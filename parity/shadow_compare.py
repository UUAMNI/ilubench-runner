#!/usr/bin/env python3
"""Compare a shadow run: the Go binary and runner.py, same arguments, real API.

    python3 parity/shadow_compare.py DIR        # DIR holds go/ and py/, from shadow.sh

Model output is not deterministic, so this is not a byte comparison of the
responses. It checks everything that must be identical regardless of what the
model said, and it cross-checks the one derived field, output_language (plus
the notes string), by running BOTH implementations' heuristics over BOTH
archives' real response texts:

  exit codes      equal
  stdout          equal after masking what depends on the response
                  (output_language values, HTTP error bodies) and the
                  CPython-only error tails the goldens also mask
  runs.jsonl      same row count; per row the same key order, run_id, date,
                  provider, reported model, interface, probe_id, pending
                  stamps and evidence; per arm the same key order, a valid
                  output_language, and notes in the runner's format that
                  name the right archive file
  runs_raw/       same file names; per file the same key order and the same
                  provider, endpoint, requested/reported model, probe id, arm
                  and prompt; a well-formed date_utc; a string response
  heuristic       for every archived response from either implementation,
                  Go's Detect/FactualNotes and Python's
                  detect_output_language/factual_notes agree, and the row
                  that cites the archive carries exactly those values

Anything the model itself decides (response text, token counts, request
ids, which optional keys a provider includes) is reported as a NOTE, never a
failure. Exit 0 when there is no FAIL.

Requires the Go toolchain (`go run ./parity/langidcheck`) unless
--langidcheck names a prebuilt helper. Test tooling only.
"""

from __future__ import annotations

import argparse
import difflib
import json
import os
import re
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
sys.path.insert(0, str(HERE))
sys.path.insert(0, str(REPO))
from harness import normalize_stdout  # noqa: E402
import runner  # noqa: E402  (the reference implementation, untouched)

LANGS = {"en", "ig", "mixed", "empty"}
DATE_UTC = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{6})?\+00:00$")
NOTES = re.compile(r'^API run, auto-captured\. ~\d+ words\. Opens: ".*\.\.\."\. '
                   r"Full raw response: (?P<path>.+?)\. Rubric axes pending human score\.$", re.S)
ARCHIVE_KEYS = ["date_utc", "provider", "endpoint", "requested_model", "reported_model",
                "probe_id", "arm", "prompt", "response_text", "raw_api_response"]
ROW_KEYS = ["run_id", "date", "provider", "model", "interface", "probe_id", "arm_A", "arm_B",
            "register_delta", "reading", "cultural_correctness", "evidence"]
ARM_KEYS = ["output_language", "epistemic_frame", "anchor_source", "notes"]


class Report:
    def __init__(self):
        self.fails: list[str] = []
        self.notes: list[str] = []
        self.counts: dict[str, int] = {}

    def fail(self, msg):
        self.fails.append(msg)

    def note(self, msg):
        self.notes.append(msg)

    def count(self, key, n=1):
        self.counts[key] = self.counts.get(key, 0) + n


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8") if path.exists() else ""


def read_rows(path: Path) -> list:
    rows = []
    for i, line in enumerate(read_text(path).splitlines(), 1):
        if line.strip():
            try:
                rows.append(json.loads(line))
            except ValueError as e:
                raise SystemExit(f"{path} line {i}: {e}")
    return rows


def udiff(a: str, b: str, label: str) -> str:
    return "".join(difflib.unified_diff(a.splitlines(True), b.splitlines(True),
                                        f"go/{label}", f"py/{label}", n=2))


# --- stdout -------------------------------------------------------------------

_STDOUT_MASKS = [
    (re.compile(r"output_language=\w+"), "output_language=<L>"),
    (re.compile(r"(HTTP \d{3}): .*$", re.M), r"\1: <BODY>"),
]


def mask_stdout(text: str) -> str:
    text = normalize_stdout(text)
    for pat, repl in _STDOUT_MASKS:
        text = pat.sub(repl, text)
    return text


# --- rows -----------------------------------------------------------------------

def compare_rows(go_rows, py_rows, rep: Report):
    if len(go_rows) != len(py_rows):
        rep.fail(f"row count: go {len(go_rows)}, python {len(py_rows)}")
    for i, (g, p) in enumerate(zip(go_rows, py_rows)):
        label = f"row {i} ({g.get('probe_id', '?')})"
        rep.count("rows")
        if list(g) != ROW_KEYS:
            rep.fail(f"{label}: go row keys {list(g)}")
        if list(p) != ROW_KEYS:
            rep.fail(f"{label}: python row keys {list(p)}")
        for k in ("run_id", "date", "provider", "interface", "probe_id",
                  "register_delta", "reading", "cultural_correctness", "evidence"):
            if g.get(k) != p.get(k):
                rep.fail(f"{label}: {k}: go {g.get(k)!r}, python {p.get(k)!r}")
        if g.get("model") != p.get("model"):
            rep.fail(f"{label}: reported model differs: go {g.get('model')!r}, python {p.get('model')!r} "
                     "(the API reported different ids to the two runs; re-run before reading anything into it)")
        for arm in ("arm_A", "arm_B"):
            for who, row in (("go", g), ("python", p)):
                a = row.get(arm)
                if not isinstance(a, dict) or list(a) != ARM_KEYS:
                    rep.fail(f"{label} {arm}: {who} arm keys {list(a) if isinstance(a, dict) else a}")
                    continue
                if a["output_language"] not in LANGS:
                    rep.fail(f"{label} {arm}: {who} output_language {a['output_language']!r}")
                if a["epistemic_frame"] != "pending_human_score" or a["anchor_source"] != "pending_human_score":
                    rep.fail(f"{label} {arm}: {who} rubric stamps {a['epistemic_frame']!r}, {a['anchor_source']!r}")
                m = NOTES.match(a["notes"])
                if not m:
                    rep.fail(f"{label} {arm}: {who} notes not in runner format: {a['notes'][:80]!r}")
                elif not m.group("path").endswith(f"_{row.get('probe_id')}_{arm}.json"):
                    rep.fail(f"{label} {arm}: {who} notes cite {m.group('path')!r}, expected the {arm} archive")
            ga, pa = g.get(arm, {}), p.get(arm, {})
            if isinstance(ga, dict) and isinstance(pa, dict) and ga.get("output_language") != pa.get("output_language"):
                rep.note(f"{label} {arm}: output_language go {ga.get('output_language')} vs python "
                         f"{pa.get('output_language')} (different responses; the cross-check below is the real test)")


# --- archives -------------------------------------------------------------------

def load_archives(d: Path) -> dict:
    out = {}
    raw = d / "runs_raw"
    if raw.is_dir():
        for f in sorted(raw.glob("*.json")):
            with open(f, encoding="utf-8") as fh:
                out[f.name] = json.load(fh)
    return out


def compare_archives(go_arc: dict, py_arc: dict, rep: Report):
    if set(go_arc) != set(py_arc):
        rep.fail(f"archive files differ: only go {sorted(set(go_arc) - set(py_arc))}, "
                 f"only python {sorted(set(py_arc) - set(go_arc))}")
    for name in sorted(set(go_arc) & set(py_arc)):
        g, p = go_arc[name], py_arc[name]
        rep.count("archives")
        for who, a in (("go", g), ("python", p)):
            if list(a) != ARCHIVE_KEYS:
                rep.fail(f"{name}: {who} archive keys {list(a)}")
            if not DATE_UTC.match(str(a.get("date_utc"))):
                rep.fail(f"{name}: {who} date_utc {a.get('date_utc')!r} not in isoformat()+00:00 form")
            if not isinstance(a.get("response_text"), str):
                rep.fail(f"{name}: {who} response_text is {type(a.get('response_text')).__name__}")
        for k in ("provider", "endpoint", "requested_model", "probe_id", "arm", "prompt"):
            if g.get(k) != p.get(k):
                rep.fail(f"{name}: {k}: go {g.get(k)!r}, python {p.get(k)!r}")
        if g.get("reported_model") != p.get("reported_model"):
            rep.fail(f"{name}: reported_model: go {g.get('reported_model')!r}, python {p.get('reported_model')!r}")
        gk = sorted(g.get("raw_api_response", {}).keys()) if isinstance(g.get("raw_api_response"), dict) else None
        pk = sorted(p.get("raw_api_response", {}).keys()) if isinstance(p.get("raw_api_response"), dict) else None
        if gk != pk:
            rep.note(f"{name}: raw_api_response top-level keys differ: go {gk}, python {pk}")


# --- heuristic cross-check -------------------------------------------------------

def go_langid(items: list, langidcheck: str | None) -> list:
    """Run the Go heuristic over [(text, raw_path)] via parity/langidcheck."""
    if not items:
        return []
    cmd = [langidcheck] if langidcheck else ["go", "run", "./parity/langidcheck"]
    payload = "".join(json.dumps({"text": t, "raw_path": rp}, ensure_ascii=False) + "\n" for t, rp in items)
    try:
        proc = subprocess.run(cmd, cwd=REPO, input=payload.encode("utf-8"), capture_output=True, timeout=600)
    except FileNotFoundError:
        raise SystemExit("shadow_compare: `go` not found; install the Go toolchain or pass --langidcheck PATH")
    if proc.returncode != 0:
        raise SystemExit(f"shadow_compare: langidcheck failed:\n{proc.stderr.decode('utf-8', 'replace')}")
    lines = proc.stdout.decode("utf-8").splitlines()
    if len(lines) != len(items):
        raise SystemExit(f"shadow_compare: langidcheck returned {len(lines)} lines for {len(items)} inputs")
    return [json.loads(ln) for ln in lines]


def cross_check(dirs: dict, rows: dict, archives: dict, rep: Report, langidcheck: str | None):
    items, meta = [], []
    for who in ("go", "py"):
        by_probe = {r.get("probe_id"): r for r in rows[who]}
        for name, a in archives[who].items():
            text = a.get("response_text")
            if not isinstance(text, str):
                continue
            raw_path = f"runs_raw/{name}"
            items.append((text, raw_path))
            row = by_probe.get(a.get("probe_id"))
            arm = row.get(a.get("arm")) if isinstance(row, dict) else None
            meta.append((who, name, arm))
    go_results = go_langid(items, langidcheck)
    for (text, raw_path), (who, name, arm), gres in zip(items, meta, go_results):
        rep.count("texts cross-checked")
        py_lang = runner.detect_output_language(text)
        py_notes = runner.factual_notes(text, runner.Path(raw_path))
        if gres["output_language"] != py_lang:
            rep.fail(f"{who}/{name}: Go says {gres['output_language']}, Python says {py_lang} for a real response "
                     f"(opens {text.strip()[:60]!r})")
        if gres["notes"] != py_notes:
            rep.fail(f"{who}/{name}: notes differ between implementations\n  go: {gres['notes']}\n  py: {py_notes}")
        if isinstance(arm, dict):
            # The row written by `who` must carry exactly what its own heuristic computes.
            own_lang = py_lang if who == "py" else gres["output_language"]
            own_notes = py_notes if who == "py" else gres["notes"]
            if arm.get("output_language") != own_lang:
                rep.fail(f"{who}/{name}: row says output_language={arm.get('output_language')!r}, "
                         f"recomputed {own_lang!r}")
            if arm.get("notes") != own_notes:
                rep.fail(f"{who}/{name}: row notes differ from recomputation\n  row: {arm.get('notes')}\n  now: {own_notes}")
        elif rows[who]:
            rep.note(f"{who}/{name}: no row cites this archive (the probe failed on the other arm)")


# --- main -----------------------------------------------------------------------

def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("dir", help="shadow run directory containing go/ and py/")
    ap.add_argument("--langidcheck", default=os.environ.get("ILUBENCH_LANGIDCHECK"),
                    help="prebuilt parity/langidcheck binary (default: go run ./parity/langidcheck)")
    args = ap.parse_args(argv)
    base = Path(args.dir)
    dirs = {"go": base / "go", "py": base / "py"}
    for who, d in dirs.items():
        if not d.is_dir():
            raise SystemExit(f"shadow_compare: {d} is missing; run parity/shadow.sh first")
    rep = Report()

    codes = {who: read_text(d / "exit_code.txt").strip() for who, d in dirs.items()}
    if codes["go"] != codes["py"]:
        rep.fail(f"exit code: go {codes['go'] or '?'}, python {codes['py'] or '?'}")
    g_out, p_out = mask_stdout(read_text(dirs["go"] / "stdout.txt")), mask_stdout(read_text(dirs["py"] / "stdout.txt"))
    if g_out != p_out:
        rep.fail("stdout differs (after masking):\n" + udiff(g_out, p_out, "stdout"))
    if read_text(dirs["go"] / "stderr.txt").strip():
        rep.note("go stderr:\n" + read_text(dirs["go"] / "stderr.txt"))
    if "Traceback" in read_text(dirs["py"] / "stderr.txt"):
        rep.fail("python crashed with a traceback:\n" + read_text(dirs["py"] / "stderr.txt"))

    rows = {who: read_rows(d / "runs.jsonl") for who, d in dirs.items()}
    archives = {who: load_archives(d) for who, d in dirs.items()}
    compare_rows(rows["go"], rows["py"], rep)
    compare_archives(archives["go"], archives["py"], rep)
    cross_check(dirs, rows, archives, rep, args.langidcheck)

    print(f"shadow compare: {base}")
    print(f"  exit codes      go={codes['go'] or '?'} python={codes['py'] or '?'}")
    print(f"  rows            go={len(rows['go'])} python={len(rows['py'])}")
    print(f"  archives        go={len(archives['go'])} python={len(archives['py'])}")
    print(f"  cross-checked   {rep.counts.get('texts cross-checked', 0)} real response(s) through both heuristics")
    for n in rep.notes:
        print("  NOTE " + n.replace("\n", "\n       "))
    for f in rep.fails:
        print("  FAIL " + f.replace("\n", "\n       "))
    verdict = "PASS" if not rep.fails else f"FAIL ({len(rep.fails)} problem(s))"
    print(f"  verdict         {verdict}")
    return 0 if not rep.fails else 1


if __name__ == "__main__":
    sys.exit(main())
