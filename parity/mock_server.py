#!/usr/bin/env python3
"""Deterministic mock of every HTTP surface runner.py talks to.

Serves the three provider dialects (Anthropic Messages, Google generateContent,
OpenAI-compatible chat.completions), their model-listing endpoints, and a
stand-in for the HuggingFace probe-set URL. Replies are canned and keyed by the
exact prompt, so both runners see identical bytes and the goldens are stable.

URL layout:  /<profile>/<dialect path>
  profile   ok        everything succeeds
            listfail  model listing returns 500 (with the caller's key echoed
                      into the body, to prove redaction)
            hf404     the probe-set URL returns 404
  dialects  /v1/chat/completions, /v1/models                (OpenAI-compatible)
            /anthropic/v1/messages, /anthropic/v1/models
            /google/v1beta/models/<model>:generateContent, /google/v1beta/models
            /hf/probe_set_v0.jsonl

Prompt directives (placed inside a probe's prompt text) force failure modes:
  [[http:NNN]]   respond with status NNN and a JSON error body
  [[nonjson]]    respond 200 with a non-JSON body
  [[badshape]]   respond 200 with JSON of the wrong shape
  [[nomodel]]    omit the reported model field from a normal reply

Every request is appended to --log as one line: method, path, auth headers,
content type, and the body re-serialized as compact JSON (key order kept), so
the wire shape of each implementation can be diffed without caring about
whitespace. User-Agent is deliberately not logged (a known deviation).

Test tooling only. Not used by runner.py.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import unicodedata
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


def _nfd(s: str) -> str:
    return unicodedata.normalize("NFD", s)


# --- canned replies, keyed by exact prompt ---------------------------------

REPLIES: dict[str, str] = {
    # examples/sample_probes.jsonl
    "Explain this proverb: Gidi gidi bụ ugwu eze.":
        "This proverb means that unity is strength. A king is powerful because "
        "of the people around him, and a crowd cannot be pushed aside.",
    "Kọwaa ilu a: Gidi gidi bụ ugwu eze.":
        "Ilu a pụtara na ịdị n'otu bụ ike. Eze na-enwe ike site n'aka ndị ya. "
        "Igwe bụ ike; ọ bụ ya mere anyị ji ekwu na umunna bụ ike.",
    "Explain this proverb: Igwe bụ ike.":
        "This proverb means unity is strength. Many hands make light work.",
    "Kọwaa ilu a: Igwe bụ ike.":
        "Igwe bụ ike pụtara na ndị mmadụ nwere ike ma ha dị n'otu.",
    # parity/probes/edge.jsonl
    "EDGE empty reply": "",
    "EDGE digits reply": "123 456 -- 789",
    "EDGE long english":
        "  Leading spaces, then a \"quoted\" phrase, a back\\slash, a <b>bold &amp;</b> tag,\n\n"
        "a tab\there, a line separator, a unitseparator, an emoji \U0001F30D, and "
        "enough words after the ninetieth character that the opening excerpt must be "
        "truncated by the runner.  ",
    "EDGE long igbo":
        _nfd("Ilu a pụ́tara na ịdị́ n'otu bụ́ ike. ")
        + "Eze na-enwe ike site n’aka ndị ya.Ọ bụ eziokwu na "
        "‘igwe bụ ike’ ma ọ bụkwa ihe ndị Igbo kwuru site n'ala ha.",
    "EDGE nomodel [[nomodel]]": "A plain reply whose response omits the model field.",
    "EDGE nomodel ig [[nomodel]]": "Nzaghachi nke na-enweghị mkpụrụ ọkọlọtọ.",
}

_DIRECTIVE = re.compile(r"\[\[(http:(\d{3})|nonjson|badshape|nomodel)\]\]")

# Odd-but-legal JSON values a real provider could return. They make the raw
# archive exercise float re-serialization, big integers, empty containers,
# HTML characters and a U+2028 inside a string.
ODD_EXTRAS = {
    "usage": {"prompt_tokens": 10, "completion_tokens": 20, "ratio": 0.10,
              "tiny": 1e-7, "sci": 1E5, "big": 12345678901234567890},
    "empty_list": [],
    "empty_obj": {},
    "html": "<b>&</b> \"q\" \\ /",
    "sep": "line sep",
    "flag": True,
    "nothing": None,
}


def reply_for(prompt: str) -> str:
    return REPLIES.get(prompt, f"Mock reply for: {prompt}")


class Handler(BaseHTTPRequestHandler):
    server_version = "ilubench-parity-mock/0"
    hf_probes: bytes = b""
    log_path: Path | None = None

    # -- plumbing ---------------------------------------------------------
    def log_message(self, fmt, *args):  # silence default stderr chatter
        pass

    def _log_request(self, body: bytes) -> None:
        if self.log_path is None:
            return
        auth = []
        for h in ("Authorization", "x-api-key", "anthropic-version", "x-goog-api-key"):
            v = self.headers.get(h)
            if v is not None:
                auth.append(f"{h}={v}")
        try:
            shown = json.dumps(json.loads(body), ensure_ascii=False, separators=(",", ":")) if body else "-"
        except ValueError:
            shown = body.decode("utf-8", "replace")
        line = (f"{self.command} {self.path} auth=[{', '.join(auth)}] "
                f"ct={self.headers.get('Content-Type', '-')} body={shown}\n")
        with open(self.log_path, "a", encoding="utf-8") as f:
            f.write(line)

    def _send(self, code: int, payload, raw: bytes | None = None, ctype="application/json"):
        data = raw if raw is not None else json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _key(self) -> str:
        for h in ("Authorization", "x-api-key", "x-goog-api-key"):
            v = self.headers.get(h)
            if v:
                return v.removeprefix("Bearer ").strip()
        return "none"

    def _fail(self, code: int):
        self._send(code, {"error": {"type": "mock_failure", "code": code,
                                    "message": f"mock failure; caller key was {self._key()}"}})

    def _split(self):
        parts = self.path.split("/", 2)  # "", profile, rest
        profile = parts[1] if len(parts) > 1 else ""
        rest = "/" + parts[2] if len(parts) > 2 else "/"
        return profile, rest

    # -- routes -----------------------------------------------------------
    def do_GET(self):
        self._log_request(b"")
        profile, rest = self._split()
        if rest == "/hf/probe_set_v0.jsonl":
            if profile == "hf404":
                return self._send(404, None, raw=b"Entry not found", ctype="text/plain")
            return self._send(200, None, raw=self.hf_probes, ctype="text/plain; charset=utf-8")
        if profile == "listfail":
            return self._fail(500)
        if rest == "/v1/models":
            return self._send(200, {"object": "list", "data": [
                {"id": "zeta", "object": "model"}, {"id": "alpha"}, {"id": "Beta"},
                {"id": "ọmodel-with-diacritic"}, {"object": "model-without-id"}]})
        if rest == "/anthropic/v1/models":
            return self._send(200, {"data": [{"id": "claude-mock-b", "type": "model"},
                                             {"id": "claude-mock-a", "type": "model"}],
                                    "has_more": False})
        if rest == "/google/v1beta/models":
            return self._send(200, {"models": [{"name": "models/gemini-mock-b"},
                                               {"name": "models/gemini-mock-a"},
                                               {"name": "no-prefix-name"}]})
        self._send(404, {"error": "no such route"})

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n)
        self._log_request(body)
        profile, rest = self._split()
        try:
            req = json.loads(body)
        except ValueError:
            return self._fail(400)

        if rest == "/v1/chat/completions":
            prompt = req["messages"][0]["content"]
            return self._reply(prompt, "openai")
        if rest == "/anthropic/v1/messages":
            prompt = req["messages"][0]["content"]
            return self._reply(prompt, "anthropic")
        if re.fullmatch(r"/google/v1beta/models/(.+):generateContent", rest):
            prompt = req["contents"][0]["parts"][0]["text"]
            return self._reply(prompt, "google")
        self._send(404, {"error": "no such route"})

    def _reply(self, prompt: str, dialect: str):
        d = _DIRECTIVE.search(prompt)
        directive = d.group(1) if d else ""
        if directive.startswith("http:"):
            return self._fail(int(d.group(2)))
        if directive == "nonjson":
            return self._send(200, None, raw=b"this is not json", ctype="text/plain")
        if directive == "badshape":
            return self._send(200, {"unexpected": True, "choices": "not a list"})
        text = reply_for(prompt)
        # Two text parts where the dialect allows it, so the join is exercised.
        cut = len(text) // 2
        part1, part2 = text[:cut], text[cut:]
        if dialect == "openai":
            payload = {"id": "chatcmpl-mock", "object": "chat.completion", "created": 1700000000,
                       "model": "mock-gpt-1.5-reported",
                       "choices": [{"index": 0, "message": {"role": "assistant", "content": text},
                                    "finish_reason": "stop"}], **ODD_EXTRAS}
            if directive == "nomodel":
                del payload["model"]
        elif dialect == "anthropic":
            payload = {"id": "msg_mock", "type": "message", "role": "assistant",
                       "model": "claude-mock-reported",
                       "content": [{"type": "text", "text": part1},
                                   {"type": "tool_use", "id": "tu_1", "name": "ignored", "input": {}},
                                   {"type": "text", "text": part2}],
                       "stop_reason": "end_turn", **ODD_EXTRAS}
            if directive == "nomodel":
                del payload["model"]
        else:
            payload = {"candidates": [{"content": {"parts": [{"text": part1}, {"text": part2}],
                                                   "role": "model"}, "finishReason": "STOP"}],
                       "modelVersion": "gemini-mock-reported", **ODD_EXTRAS}
            if directive == "nomodel":
                del payload["modelVersion"]
        self._send(200, payload)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--log", required=True, help="request log file (appended)")
    ap.add_argument("--hf-probes", required=True, help="JSONL served as the HuggingFace probe set")
    args = ap.parse_args()
    Handler.hf_probes = Path(args.hf_probes).read_bytes()
    Handler.log_path = Path(args.log)
    srv = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
