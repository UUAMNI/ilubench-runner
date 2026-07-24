#!/usr/bin/env python3
"""IlùBench runner — reproduce the IlùBench elicitation protocol with your own API keys.

IlùBench measures *cultural register switching*: ask a model to explain an
Igbo proverb in English (arm A), then ask the same model the same question in
Igbo (arm B), and compare the two answers. The interesting question is not
which language the reply comes back in — it is whether the two replies are
the same explanation.

Protocol invariants (do not change these if you want comparable rows):

  * One fresh API call per arm; no state is shared between arms.
  * No system prompt.
  * Provider default sampling — no temperature or top_p overrides.
  * Both arms of every probe are always run (arm A = prompt_en, arm B = prompt_ig).
  * Full raw API responses are archived locally under runs_raw/ (git-ignored).
  * The script never scores a rubric axis. epistemic_frame, anchor_source,
    register_delta, and reading are stamped "pending_human_score";
    cultural_correctness is stamped "pending_native_review". Only
    output_language is auto-filled, by a conservative diacritic/stopword
    heuristic. Scoring is human by design — see README.md.

API keys come from environment variables only (ANTHROPIC_API_KEY,
OPENAI_API_KEY, GEMINI_API_KEY, MOONSHOT_API_KEY, or any variable you name
with --api-key-env). Keys are never printed and never written to any output.

Usage:
    python runner.py --provider anthropic --dry-run
    python runner.py --provider anthropic                # lists models; you pick
    python runner.py --provider anthropic --model <model-id>
    python runner.py --provider google --model <model-id>
    python runner.py --base-url http://localhost:8000/v1 --model <model-id>
    python runner.py --probe-set examples/sample_probes.jsonl --provider openai --model <model-id>

Stdlib only. Python 3.10+.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import unicodedata
import urllib.error
import urllib.request
from datetime import date, datetime, timezone
from pathlib import Path

PROBE_SET_URL = "https://huggingface.co/datasets/UUAMNI/ilubench/resolve/main/probe_set_v0.jsonl"

# Anthropic's Messages API requires an explicit output cap; 2048 is the cap
# used for all IlùBench runs. It is not sent to providers that default it.
MAX_TOKENS = 2048
TIMEOUT_S = 120

PROVIDERS = {
    "anthropic": {"key_env": "ANTHROPIC_API_KEY", "base_url": None},
    "openai": {"key_env": "OPENAI_API_KEY", "base_url": "https://api.openai.com/v1"},
    "google": {"key_env": "GEMINI_API_KEY", "base_url": None},
    "moonshot": {"key_env": "MOONSHOT_API_KEY", "base_url": "https://api.moonshot.ai/v1"},
    # Any OpenAI-compatible endpoint (OpenRouter, local vLLM, ...) via --base-url.
    "compatible": {"key_env": "OPENAI_API_KEY", "base_url": None},
}

# Providers whose chat endpoint speaks the OpenAI chat-completions dialect.
OPENAI_COMPATIBLE = {"openai", "moonshot", "compatible"}


# ---------------------------------------------------------------------------
# HTTP helpers (plain HTTPS, no SDK dependencies)
# ---------------------------------------------------------------------------


def _request_json(url: str, headers: dict, payload: dict | None = None) -> dict:
    body = json.dumps(payload).encode("utf-8") if payload is not None else None
    req = urllib.request.Request(url, data=body, method="POST" if body else "GET")
    if body:
        req.add_header("Content-Type", "application/json")
    for k, v in headers.items():
        req.add_header(k, v)
    with urllib.request.urlopen(req, timeout=TIMEOUT_S) as resp:
        return json.load(resp)


def _redact(text: str, secrets: list[str]) -> str:
    for s in secrets:
        if s:
            text = text.replace(s, "[redacted]")
    return text


def _http_error_detail(e: Exception, secrets: list[str]) -> str:
    """A short, readable error line. Never echoes a key."""
    if isinstance(e, urllib.error.HTTPError):
        try:
            body = e.read().decode("utf-8", errors="replace")[:300]
        except Exception:
            body = ""
        return _redact(f"HTTP {e.code}: {body}".strip(), secrets)
    if isinstance(e, urllib.error.URLError):
        return f"network error: {e.reason}"
    return _redact(f"{type(e).__name__}: {e}", secrets)


# ---------------------------------------------------------------------------
# Provider calls
# ---------------------------------------------------------------------------


def call_anthropic(key: str, model: str, prompt: str) -> tuple[str, str, dict]:
    raw = _request_json(
        "https://api.anthropic.com/v1/messages",
        {"x-api-key": key, "anthropic-version": "2023-06-01"},
        {
            "model": model,
            "max_tokens": MAX_TOKENS,
            "messages": [{"role": "user", "content": prompt}],
        },
    )
    text = "".join(b.get("text", "") for b in raw.get("content", []) if b.get("type") == "text")
    return text, raw.get("model", model), raw


def call_openai_compatible(base_url: str, key: str | None, model: str, prompt: str) -> tuple[str, str, dict]:
    headers = {"Authorization": f"Bearer {key}"} if key else {}
    raw = _request_json(
        f"{base_url}/chat/completions",
        headers,
        {"model": model, "messages": [{"role": "user", "content": prompt}]},
    )
    text = raw["choices"][0]["message"]["content"] or ""
    return text, raw.get("model", model), raw


def call_google(key: str, model: str, prompt: str) -> tuple[str, str, dict]:
    raw = _request_json(
        f"https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent",
        {"x-goog-api-key": key},
        {"contents": [{"parts": [{"text": prompt}]}]},
    )
    parts = raw.get("candidates", [{}])[0].get("content", {}).get("parts", [])
    text = "".join(p.get("text", "") for p in parts)
    return text, raw.get("modelVersion", model), raw


def call_provider(provider: str, base_url: str | None, key: str | None, model: str, prompt: str) -> tuple[str, str, dict]:
    if provider == "anthropic":
        return call_anthropic(key, model, prompt)
    if provider == "google":
        return call_google(key, model, prompt)
    if provider in OPENAI_COMPATIBLE:
        return call_openai_compatible(base_url, key, model, prompt)
    raise ValueError(provider)


def list_models(provider: str, base_url: str | None, key: str | None) -> list[str]:
    """Model ids the endpoint reports. Used when --model is omitted: the
    runner deliberately has no default model ids — you pick."""
    if provider == "anthropic":
        raw = _request_json(
            "https://api.anthropic.com/v1/models",
            {"x-api-key": key, "anthropic-version": "2023-06-01"},
        )
        return [m.get("id", "") for m in raw.get("data", [])]
    if provider == "google":
        raw = _request_json(
            "https://generativelanguage.googleapis.com/v1beta/models",
            {"x-goog-api-key": key},
        )
        return [m.get("name", "").removeprefix("models/") for m in raw.get("models", [])]
    headers = {"Authorization": f"Bearer {key}"} if key else {}
    raw = _request_json(f"{base_url}/models", headers)
    return [m.get("id", "") for m in raw.get("data", [])]


# ---------------------------------------------------------------------------
# Output-language heuristic (the ONLY auto-filled field; everything else is
# human-scored — see README.md)
# ---------------------------------------------------------------------------

_IGBO_MARKERS = re.compile(r"[ịọụṅỊỌỤṄ]")
_IGBO_WORDS = {
    "na", "bụ", "nke", "ya", "a", "ilu", "ihe", "ndị", "n'ala", "mmadụ",
    "igbo", "anyị", "gị", "ha", "dị", "ka", "ma", "ga-", "kwuru", "pụtara",
}


def detect_output_language(text: str) -> str:
    """'ig' / 'en' / 'mixed' via diacritic + stopword density. Conservative:
    anything genuinely bilingual lands on 'mixed'."""
    if not text.strip():
        return "empty"
    words = re.findall(r"[^\W\d_]+(?:'[^\W\d_]+)?", unicodedata.normalize("NFC", text.lower()))
    if not words:
        return "empty"
    igbo_hits = sum(1 for w in words if _IGBO_MARKERS.search(w) or w in _IGBO_WORDS)
    ratio = igbo_hits / len(words)
    if ratio >= 0.35:
        return "ig"
    if ratio <= 0.05:
        return "en"
    return "mixed"


def factual_notes(text: str, raw_path: Path) -> str:
    """Short factual description of the response. No rubric judgment."""
    words = len(text.split())
    opening = " ".join(text.strip().split())[:90]
    return (
        f"API run, auto-captured. ~{words} words. Opens: \"{opening}...\". "
        f"Full raw response: {raw_path.as_posix()}. "
        "Rubric axes pending human score."
    )


# ---------------------------------------------------------------------------
# Probe set
# ---------------------------------------------------------------------------


def load_probes(path: str | None) -> tuple[list[dict], str]:
    """Probes from a local JSONL file, or fetched from the HuggingFace dataset."""
    if path:
        text = Path(path).read_text(encoding="utf-8")
        source = path
    else:
        with urllib.request.urlopen(PROBE_SET_URL, timeout=60) as resp:
            text = resp.read().decode("utf-8")
        source = PROBE_SET_URL
    probes = []
    for line_no, line in enumerate(text.splitlines(), 1):
        line = line.strip()
        if not line:
            continue
        d = json.loads(line)
        for field in ("id", "prompt_en", "prompt_ig"):
            if field not in d:
                raise ValueError(f"probe on line {line_no} of {source} is missing {field!r}")
        probes.append(d)
    if not probes:
        raise ValueError(f"no probes found in {source}")
    return probes, source


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def slug(model: str) -> str:
    return re.sub(r"[^A-Za-z0-9._-]+", "-", model).strip("-")


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Run the IlùBench two-arm elicitation protocol against a model API.",
        epilog="Keys are read from environment variables only and are never printed or written.",
    )
    ap.add_argument("--provider", choices=sorted(PROVIDERS),
                    help="API dialect. Inferred as 'compatible' when --base-url is set.")
    ap.add_argument("--model",
                    help="Model id to test — you pick; there are no defaults. "
                         "Omit to list the endpoint's available models and exit.")
    ap.add_argument("--base-url",
                    help="OpenAI-compatible endpoint base URL, e.g. "
                         "https://openrouter.ai/api/v1 or http://localhost:8000/v1.")
    ap.add_argument("--api-key-env",
                    help="Name of the environment variable holding the API key "
                         "(default depends on --provider, e.g. ANTHROPIC_API_KEY).")
    ap.add_argument("--probe-set", metavar="PATH",
                    help="Local probe JSONL. Default: fetch the published set from "
                         "huggingface.co/datasets/UUAMNI/ilubench.")
    ap.add_argument("--probes", metavar="ID,ID",
                    help="Comma-separated probe ids to run (default: all in the set).")
    ap.add_argument("--out", default="runs.jsonl", metavar="PATH",
                    help="Structured rows are appended here (default: runs.jsonl).")
    ap.add_argument("--raw-dir", default="runs_raw", metavar="DIR",
                    help="Raw API responses are archived here (default: runs_raw/).")
    ap.add_argument("--dry-run", action="store_true",
                    help="Print the call plan and key status; no API calls, no writes.")
    args = ap.parse_args()

    if args.base_url and not args.provider:
        args.provider = "compatible"
    if not args.provider:
        ap.error("--provider is required (or pass --base-url for an OpenAI-compatible endpoint)")
    if args.provider == "compatible" and not args.base_url:
        ap.error("--provider compatible requires --base-url")

    base_url = (args.base_url or PROVIDERS[args.provider]["base_url"] or "").rstrip("/") or None
    key_env = args.api_key_env or PROVIDERS[args.provider]["key_env"]
    key = os.environ.get(key_env) or None

    try:
        probes, probe_source = load_probes(args.probe_set)
    except FileNotFoundError:
        print(f"ERROR: probe set file not found: {args.probe_set}")
        return 1
    except (urllib.error.URLError, OSError) as e:
        print(f"ERROR: could not fetch the probe set from HuggingFace ({e}).")
        print("       Offline? Point --probe-set at a local file, e.g. "
              "--probe-set examples/sample_probes.jsonl")
        return 1
    except ValueError as e:
        print(f"ERROR: bad probe set: {e}")
        return 1

    by_id = {p["id"]: p for p in probes}
    probe_ids = ([p.strip() for p in args.probes.split(",") if p.strip()]
                 if args.probes else list(by_id))
    missing = [p for p in probe_ids if p not in by_id]
    if missing:
        print(f"ERROR: probe ids not in {probe_source}: {missing}")
        print(f"       Available: {list(by_id)}")
        return 1

    endpoint = base_url or {"anthropic": "https://api.anthropic.com",
                            "google": "https://generativelanguage.googleapis.com"}[args.provider]
    plan_calls = len(probe_ids) * 2

    print(f"Probe set: {probe_source} ({len(by_id)} probes; running {len(probe_ids)})")
    print(f"Provider:  {args.provider} ({endpoint})")
    print(f"Model:     {args.model or '(not set — pass --model; omit to list available models)'}")
    print(f"Key:       {key_env} is {'set' if key else 'NOT set'}")
    print(f"Plan:      {len(probe_ids)} probes x 2 arms = {plan_calls} API calls, "
          "no system prompt, provider default sampling")
    for pid in probe_ids:
        print(f"  {pid}  arm_A (English prompt) + arm_B (Igbo prompt)")

    if args.dry_run:
        print("\nDry run complete. No API calls made, nothing written.")
        if not key:
            print(f"Before a real run: export {key_env}=<your key>")
        return 0

    if not key and args.provider != "compatible":
        print(f"\nERROR: no API key found. Set {key_env} (or point --api-key-env at "
              "the variable that holds your key).")
        return 1
    if not key:
        print(f"\nNote: {key_env} is not set; sending unauthenticated requests "
              "(fine for local servers such as vLLM).")
    secrets = [key] if key else []

    if not args.model:
        try:
            ids = list_models(args.provider, base_url, key)
        except Exception as e:
            print(f"\nERROR: could not list models: {_http_error_detail(e, secrets)}")
            return 1
        print(f"\nNo --model given. {endpoint} reports {len(ids)} models:")
        for i in sorted(ids):
            print(f"  {i}")
        print("\nPick one and re-run with --model <id>. "
              "(IlùBench has no default model — the choice is the experiment.)")
        return 1

    raw_dir = Path(args.raw_dir)
    raw_dir.mkdir(parents=True, exist_ok=True)
    (raw_dir / ".gitignore").write_text("*\n")  # belt and braces: raw archives never enter git

    today = str(date.today())
    model_slug = slug(args.model)
    rows = []
    failures = 0
    for pid in probe_ids:
        probe = by_id[pid]
        arms = {}
        reported_model = args.model
        for arm_name, prompt_field in (("arm_A", "prompt_en"), ("arm_B", "prompt_ig")):
            prompt = probe[prompt_field]
            try:
                text, reported_model, raw = call_provider(
                    args.provider, base_url, key, args.model, prompt)
            except Exception as e:
                print(f"  FAIL {pid} {arm_name}: {_http_error_detail(e, secrets)}")
                arms = {}
                break
            raw_path = raw_dir / f"{today}_{args.provider}_{model_slug}_{pid}_{arm_name}.json"
            raw_path.write_text(
                json.dumps(
                    {
                        "date_utc": datetime.now(timezone.utc).isoformat(),
                        "provider": args.provider,
                        "endpoint": endpoint,
                        "requested_model": args.model,
                        "reported_model": reported_model,
                        "probe_id": pid,
                        "arm": arm_name,
                        "prompt": prompt,
                        "response_text": text,
                        "raw_api_response": raw,
                    },
                    ensure_ascii=False,
                    indent=2,
                ),
                encoding="utf-8",
            )
            arms[arm_name] = {
                "output_language": detect_output_language(text),
                "epistemic_frame": "pending_human_score",
                "anchor_source": "pending_human_score",
                "notes": factual_notes(text, raw_path),
            }
            print(f"  ok {pid} {arm_name}: output_language={arms[arm_name]['output_language']}")
        if not arms:
            failures += 1
            continue

        rows.append(
            {
                "run_id": f"run-{today}-{args.provider}-{model_slug}-{pid}",
                "date": today,
                "provider": args.provider,
                "model": reported_model,
                "interface": "API",
                "probe_id": pid,
                "arm_A": arms["arm_A"],
                "arm_B": arms["arm_B"],
                "register_delta": "pending_human_score",
                "reading": "pending_human_score",
                "cultural_correctness": "pending_native_review",
                "evidence": f"{raw_dir.as_posix()}/{today}_{args.provider}_{model_slug}_{pid}_*.json "
                            "(local archive, not committed)",
            }
        )

    if rows:
        with open(args.out, "a", encoding="utf-8") as f:
            for row in rows:
                f.write(json.dumps(row, ensure_ascii=False) + "\n")
    print(f"\nAppended {len(rows)} row(s) to {args.out}"
          + (f"; {failures} probe(s) failed." if failures else "."))
    if rows:
        print("Next: human-score the pending axes against rubric.md "
              "(https://huggingface.co/datasets/UUAMNI/ilubench).")
    return 0 if not failures else 2


if __name__ == "__main__":
    sys.exit(main())
