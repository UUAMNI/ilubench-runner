#!/usr/bin/env python3
"""Summarize a shadow week: every run under shadow/, re-compared, one report.

    python3 parity/shadow_report.py            # reads ./shadow/*, writes shadow/REPORT.md, prints it
    python3 parity/shadow_report.py --dir PATH # another shadow directory

The report contains no response text and no keys: per run, the arguments,
dates, binary hashes, exit codes, row and archive counts, the reported model,
the output_language distribution per implementation, every FAIL line and the
NOTE count; then the SHADOW_WEEK.md coverage checklist derived from the runs
that were actually made, and a single readiness line for Milestone 6. Safe to
paste or commit.
"""

from __future__ import annotations

import argparse
import collections
import os
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from shadow_compare import compare  # noqa: E402

DIALECT = {"anthropic": "anthropic", "google": "google", "openai": "openai",
           "moonshot": "openai", "compatible": "compatible"}


def parse_args_txt(path: Path) -> dict:
    """The runner arguments shadow.sh recorded, one per line."""
    args = [ln for ln in path.read_text(encoding="utf-8").splitlines()]
    info = {"provider": None, "base_url": None, "model": None, "probe_set": None, "probes": None, "argv": args}
    i = 0
    while i < len(args):
        a = args[i]
        nxt = args[i + 1] if i + 1 < len(args) else None
        for flag, key in (("--provider", "provider"), ("--base-url", "base_url"), ("--model", "model"),
                          ("--probe-set", "probe_set"), ("--probes", "probes"), ("--api-key-env", "key_env")):
            if a == flag:
                info[key] = nxt
                i += 1
            elif a.startswith(flag + "="):
                info[key] = a[len(flag) + 1:]
        i += 1
    if info["base_url"] and not info["provider"]:
        info["provider"] = "compatible"
    return info


def manifest(path: Path) -> dict:
    out = {}
    if path.exists():
        for ln in path.read_text(encoding="utf-8").splitlines():
            if ": " in ln:
                k, v = ln.split(": ", 1)
                out[k] = v
    return out


def lang_dist(rows: list) -> str:
    c = collections.Counter()
    for r in rows:
        for arm in ("arm_A", "arm_B"):
            a = r.get(arm)
            if isinstance(a, dict):
                c[f"{arm[-1]}:{a.get('output_language')}"] += 1
    return " ".join(f"{k}={v}" for k, v in sorted(c.items())) or "-"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--dir", default="shadow", help="directory holding shadow runs (default: ./shadow)")
    ap.add_argument("--langidcheck", default=os.environ.get("ILUBENCH_LANGIDCHECK"))
    args = ap.parse_args(argv)
    root = Path(args.dir)
    runs = sorted(d for d in root.iterdir() if d.is_dir() and (d / "go").is_dir() and (d / "py").is_dir()) \
        if root.is_dir() else []
    if not runs:
        raise SystemExit(f"shadow_report: no shadow runs under {root}")

    lines = ["# Shadow week report", "", f"Runs found: {len(runs)} under `{root}`", ""]
    lines += ["| run | dialect / provider | model (reported) | args | dates | exit go/py | rows go/py | archives go/py | langs go | langs py | verdict |",
              "|---|---|---|---|---|---|---|---|---|---|---|"]
    details = []
    seen = []  # (dialect, kind, date, verdict)
    all_pass = True
    for d in runs:
        info = parse_args_txt(d / "args.txt")
        man = manifest(d / "manifest.txt")
        rep, codes, rows, archives = compare(d, args.langidcheck)
        verdict = "PASS" if not rep.fails else f"FAIL ({len(rep.fails)})"
        all_pass &= not rep.fails
        model = (rows["go"][0].get("model") if rows["go"] else None) or (rows["py"][0].get("model") if rows["py"] else None) or "-"
        dialect = DIALECT.get(info["provider"] or "", "?")
        kind = "listing" if not info["model"] else ("subset" if info["probes"] else ("local-set" if info["probe_set"] else "full"))
        dates = man.get("started", "?") + ("" if man.get("finished") == man.get("started") else f" to {man.get('finished', '?')}")
        argstr = " ".join(os.path.basename(a) if a.startswith("/") else a for a in info["argv"])  # no absolute paths
        lines.append(f"| {d.name} | {dialect} / {info['provider']} | {model} | `{argstr}` | {dates} | "
                     f"{codes['go'] or '?'}/{codes['py'] or '?'} | {len(rows['go'])}/{len(rows['py'])} | "
                     f"{len(archives['go'])}/{len(archives['py'])} | {lang_dist(rows['go'])} | {lang_dist(rows['py'])} | {verdict} |")
        seen.append((dialect, kind, man.get("started"), codes, not rep.fails, info))
        if rep.fails or rep.notes:
            details.append(f"### {d.name}")
            details.append(f"- go binary sha256 `{man.get('go_binary_sha256', '?')[:12]}`, runner.py sha256 "
                           f"`{man.get('runner_py_sha256', '?')[:12]}`, {man.get('python', '?')}")
            for f in rep.fails:
                details.append("- FAIL " + f.replace("\n", " / "))
            details.append(f"- {len(rep.notes)} NOTE line(s)" + (": " + "; ".join(n.split(chr(10))[0] for n in rep.notes[:4]) if rep.notes else ""))
            details.append("")

    # Coverage against SHADOW_WEEK.md.
    def have(pred):
        return any(pred(s) for s in seen)
    full_days = collections.defaultdict(set)
    for dialect, kind, date, codes, ok, info in seen:
        if kind == "full" and ok:
            full_days[dialect].add(date)
    checks = [
        ("1 Anthropic dialect, full probe set", have(lambda s: s[0] == "anthropic" and s[1] == "full" and s[4])),
        ("2 Google native dialect", have(lambda s: s[0] == "google" and s[1] in ("full", "local-set", "subset") and s[4])),
        ("3 OpenAI or Moonshot dialect", have(lambda s: s[0] == "openai" and s[4])),
        ("4 OpenAI-compatible --base-url", have(lambda s: s[0] == "compatible" and s[1] != "listing" and s[4])),
        ("5 Model listing", have(lambda s: s[1] == "listing" and s[4])),
        ("6 Deliberate failure (both exit non-zero, verdict PASS)", have(lambda s: s[1] != "listing" and s[3]["go"] not in ("0", "") and s[4])),
        ("7 Live HuggingFace fetch (a run without --probe-set)", have(lambda s: s[5]["probe_set"] is None and s[1] != "listing" and s[4])),
        ("8 Local probe set and a subset", have(lambda s: s[1] == "subset" and s[4])),
        ("Repeat: every dialect with a full-set run has one on a second day",
         bool(full_days) and all(len(days) >= 2 for days in full_days.values())),
    ]
    lines += ["", "## Coverage (SHADOW_WEEK.md)", ""]
    for name, ok in checks:
        lines.append(f"- [{'x' if ok else ' '}] {name}")
    complete = all(ok for _, ok in checks)
    lines += ["", "## Verdict", ""]
    if all_pass and complete:
        lines.append("READY FOR MILESTONE 6: every run PASS and the coverage checklist is complete.")
    else:
        if not all_pass:
            lines.append("NOT READY: at least one run has FAIL lines (see details).")
        if not complete:
            lines.append("NOT READY: coverage gaps remain (unchecked items above).")
    if details:
        lines += ["", "## Details", ""] + details
    text = "\n".join(lines) + "\n"
    (root / "REPORT.md").write_text(text, encoding="utf-8")
    print(text, end="")
    return 0 if (all_pass and complete) else 1


if __name__ == "__main__":
    sys.exit(main())
