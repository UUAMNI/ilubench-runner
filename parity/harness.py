#!/usr/bin/env python3
"""Characterization harness: record what runner.py does, then hold any
implementation to it.

    python3 parity/harness.py capture                 # goldens from runner.py
    python3 parity/harness.py check --impl python     # runner.py vs goldens
    python3 parity/harness.py check --impl go --bin ./bin/ilubench
    python3 parity/harness.py list

Each scenario (parity/scenarios.py) runs in a fresh temporary working
directory against a local mock (parity/mock_server.py) with a scrubbed
environment: no *_API_KEY variables, no proxies, TZ=UTC, UTF-8 locale.
Recorded per scenario: exit code, stdout, stderr, every file written under the
working directory, and the mock's request log.

Masking makes the record stable across days, machines and ports: today's date
becomes <TODAY>, the working directory <WORK>, the repository root <REPO>, the
mock base URL <MOCK>, and every date_utc value <DATE_UTC>. Goldens are stored
masked; the same masking is applied to the implementation under test, and the
harness re-runs a scenario if the UTC date changed while it ran.

A few stdout lines end in text that cannot be reproduced outside CPython (an
exception repr after "FAIL <probe> <arm>:", the JSON decoder's message after
"ERROR: bad probe set:"). Those tails are normalized to <ERR> on BOTH sides at
comparison time; the stable prefix, including "HTTP <code>: <body>", stays
exact. See normalize_stdout().

Goldens live in parity/goldens/<scenario>/ as plain files; written files are
stored flattened under tree/ with "/" replaced by "__" (the repo's .gitignore
ignores any directory named runs_raw, so the real tree cannot be committed).

Test tooling only. Exit status: 0 all green, 1 differences, 2 harness error.
"""

from __future__ import annotations

import argparse
import difflib
import hashlib
import json
import os
import platform
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
GOLDENS = HERE / "goldens"
sys.path.insert(0, str(HERE))
from scenarios import SCENARIOS  # noqa: E402

ARTIFACTS = ("exit_code.txt", "stdout.txt", "stderr.txt", "requests.txt", "files.txt")


# --- mock lifecycle ---------------------------------------------------------

class Mock:
    def __init__(self, log: Path):
        self.log = log
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            self.port = s.getsockname()[1]
        self.base = f"http://127.0.0.1:{self.port}"
        self.proc = None

    def __enter__(self):
        self.log.write_text("")
        self.proc = subprocess.Popen(
            [sys.executable, str(HERE / "mock_server.py"), "--port", str(self.port),
             "--log", str(self.log), "--hf-probes", str(REPO / "examples" / "sample_probes.jsonl")],
            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        deadline = time.time() + 10
        while time.time() < deadline:
            try:
                urllib.request.urlopen(f"{self.base}/ok/v1/models", timeout=1).read()
                return self
            except Exception:
                if self.proc.poll() is not None:
                    raise RuntimeError("mock server exited: " + self.proc.stderr.read().decode())
                time.sleep(0.05)
        raise RuntimeError("mock server did not come up")

    def __exit__(self, *exc):
        self.proc.terminate()
        self.proc.wait(5)

    def take_log(self) -> str:
        text = self.log.read_text(encoding="utf-8")
        self.log.write_text("")
        return text


# --- running one scenario ---------------------------------------------------

def today_utc() -> str:
    return datetime.now(timezone.utc).date().isoformat()


def expand(value: str, mock: Mock, work: Path) -> str:
    return value.replace("{REPO}", str(REPO)).replace("{MOCK}", mock.base).replace("{WORK}", str(work))


def command_for(impl: str, scenario: dict, args: list[str], go_bin: str | None) -> list[str] | None:
    if impl == "python":
        script = HERE / "py_shim.py" if scenario["shim"] else REPO / "runner.py"
        return [sys.executable, str(script), *args]
    if impl == "go":
        if scenario["shim"]:
            return None  # covered by the Go test suite with in-process endpoint injection
        if not go_bin:
            raise SystemExit("--bin is required for --impl go")
        return [go_bin, *args]
    raise SystemExit(f"unknown impl {impl!r}")


def run_once(impl: str, scenario: dict, mock: Mock, go_bin: str | None) -> dict:
    """Run a scenario once. Returns the masked record, or None if the UTC date
    rolled over during the run (caller retries)."""
    work = Path(tempfile.mkdtemp(prefix=f"ilubench-parity-{scenario['name']}-"))
    try:
        for d in scenario["pre"]:
            (work / d).mkdir(parents=True, exist_ok=True)
        args = [expand(a, mock, work) for a in scenario["args"]]
        cmd = command_for(impl, scenario, args, go_bin)
        if cmd is None:
            return {"skipped": "shim scenario; run by the Go test suite"}
        env = {
            "PATH": os.environ.get("PATH", ""),
            "HOME": str(work),
            "TZ": "UTC",
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
            "PYTHONDONTWRITEBYTECODE": "1",
            "PYTHONHASHSEED": "0",
            "ILUBENCH_PARITY_MOCK": mock.base,
            "ILUBENCH_PARITY_MOCK_PROFILE": scenario["profile"],
        }
        for k, v in scenario["env"].items():
            env[k] = expand(v, mock, work)
        mock.take_log()
        before = today_utc()
        proc = subprocess.run(cmd, cwd=work, env=env, capture_output=True, timeout=120)
        after = today_utc()
        if before != after:
            return None
        requests_log = mock.take_log()

        tree = {}
        for p in sorted(work.rglob("*")):
            if p.is_file():
                tree[p.relative_to(work).as_posix()] = p.read_bytes()

        masks = [(str(work), "<WORK>"), (str(REPO), "<REPO>"), (mock.base, "<MOCK>"), (before, "<TODAY>")]

        def mask(text: str) -> str:
            for old, new in masks:
                text = text.replace(old, new)
            return re.sub(r'"date_utc": "[^"]+"', '"date_utc": "<DATE_UTC>"', text)

        record = {
            "exit_code.txt": f"{proc.returncode}\n",
            "stdout.txt": mask(proc.stdout.decode("utf-8", "replace")),
            "stderr.txt": mask(proc.stderr.decode("utf-8", "replace")),
            "requests.txt": mask(requests_log),
            "files.txt": "".join(mask(name) + "\n" for name in tree),
            "tree": {mask(name): mask(data.decode("utf-8", "replace")) for name, data in tree.items()},
        }
        return record
    finally:
        shutil.rmtree(work, ignore_errors=True)


def run_scenario(impl, scenario, mock, go_bin):
    for _ in range(3):
        rec = run_once(impl, scenario, mock, go_bin)
        if rec is not None:
            return rec
    raise RuntimeError("UTC date kept changing; try again")


# --- goldens on disk --------------------------------------------------------

def flat(name: str) -> str:
    return name.replace("/", "__")


def write_golden(name: str, record: dict) -> None:
    d = GOLDENS / name
    if d.exists():
        shutil.rmtree(d)
    (d / "tree").mkdir(parents=True)
    for k in ARTIFACTS:
        (d / k).write_text(record[k], encoding="utf-8")
    for fname, content in record["tree"].items():
        (d / "tree" / flat(fname)).write_text(content, encoding="utf-8")


def read_golden(name: str) -> dict | None:
    d = GOLDENS / name
    if not d.exists():
        return None
    rec = {k: (d / k).read_text(encoding="utf-8") for k in ARTIFACTS}
    names = [ln for ln in rec["files.txt"].splitlines() if ln]
    rec["tree"] = {n: (d / "tree" / flat(n)).read_text(encoding="utf-8") for n in names}
    return rec


# --- comparison -------------------------------------------------------------

_TAILS = [
    # FAIL lines whose error is not an HTTP status carry a Python exception repr.
    (re.compile(r"^(  FAIL \S+ arm_[AB]: )(?!HTTP \d{3}).*$", re.M), r"\1<ERR>"),
    (re.compile(r"^(ERROR: could not list models: )(?!HTTP \d{3}).*$", re.M), r"\1<ERR>"),
    (re.compile(r"^(ERROR: could not fetch the probe set from HuggingFace \().*(\)\.)$", re.M), r"\1<ERR>\2"),
    (re.compile(r"^(ERROR: bad probe set: )(?!probe on line |no probes found).*$", re.M), r"\1<ERR>"),
]


def normalize_stdout(text: str) -> str:
    for pat, repl in _TAILS:
        text = pat.sub(repl, text)
    return text


def udiff(a: str, b: str, label: str) -> str:
    return "".join(difflib.unified_diff(a.splitlines(True), b.splitlines(True),
                                        f"golden/{label}", f"actual/{label}", n=2))


def compare(scenario: dict, golden: dict, actual: dict, impl: str) -> list[str]:
    problems = []
    if golden["exit_code.txt"] != actual["exit_code.txt"]:
        problems.append(f"exit code: golden {golden['exit_code.txt'].strip()} actual {actual['exit_code.txt'].strip()}")

    mode = scenario["stdout"]
    g, a = normalize_stdout(golden["stdout.txt"]), normalize_stdout(actual["stdout.txt"])
    if mode == "exact" and g != a:
        problems.append("stdout differs:\n" + udiff(g, a, "stdout"))
    elif mode == "prefix" and not a.startswith(g):
        problems.append("stdout does not start with golden:\n" + udiff(g, a, "stdout"))

    smode, err = scenario["stderr"], actual["stderr.txt"]
    if smode == "empty" and err.strip():
        problems.append("stderr should be empty:\n" + err)
    elif smode == "usage" and "error" not in err.lower():
        problems.append("stderr should be a usage error:\n" + err)
    elif smode == "python_traceback":
        if impl == "python" and "Traceback" not in err:
            problems.append("expected a Python traceback on stderr")
        if impl != "python" and err.strip():
            problems.append("stderr should be empty for this implementation:\n" + err)

    if golden["requests.txt"] != actual["requests.txt"]:
        problems.append("wire requests differ:\n" + udiff(golden["requests.txt"], actual["requests.txt"], "requests"))
    if golden["files.txt"] != actual["files.txt"]:
        problems.append("files written differ:\n" + udiff(golden["files.txt"], actual["files.txt"], "files"))
    for name, content in golden["tree"].items():
        if name in actual["tree"] and actual["tree"][name] != content:
            problems.append(f"file {name} differs:\n" + udiff(content, actual["tree"][name], name))
    return problems


# --- commands ---------------------------------------------------------------

def select(names: list[str]) -> list[dict]:
    if not names:
        return SCENARIOS
    by = {s["name"]: s for s in SCENARIOS}
    unknown = [n for n in names if n not in by]
    if unknown:
        raise SystemExit(f"unknown scenario(s): {unknown}")
    return [by[n] for n in names]


def write_scenarios_json() -> None:
    """parity/goldens/scenarios.json: the scenario list as data, so the Go
    golden test can replay it without importing Python."""
    GOLDENS.mkdir(exist_ok=True)
    (GOLDENS / "scenarios.json").write_text(
        json.dumps(SCENARIOS, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")


def cmd_export(args) -> int:
    write_scenarios_json()
    print(f"wrote {GOLDENS / 'scenarios.json'} ({len(SCENARIOS)} scenarios)")
    return 0


def cmd_capture(args) -> int:
    scenarios = select(args.scenario)
    write_scenarios_json()
    with Mock(HERE / ".mock_requests.log") as mock:
        for s in scenarios:
            rec = run_scenario("python", s, mock, None)
            write_golden(s["name"], rec)
            print(f"captured {s['name']:40} exit={rec['exit_code.txt'].strip()} files={len(rec['tree'])}")
    manifest = (
        f"captured_at_utc: {datetime.now(timezone.utc).isoformat(timespec='seconds')}\n"
        f"python: {platform.python_version()} ({platform.python_implementation()})\n"
        f"platform: {platform.system()} {platform.machine()}\n"
        f"runner.py_sha256: {hashlib.sha256((REPO / 'runner.py').read_bytes()).hexdigest()}\n"
        f"scenarios: {len(SCENARIOS)}\n"
    )
    (GOLDENS / "MANIFEST.txt").write_text(manifest, encoding="utf-8")
    print(manifest, end="")
    return 0


def cmd_check(args) -> int:
    scenarios = [s for s in select(args.scenario) if s["milestone"] <= args.milestone]
    if args.bin:
        args.bin = os.path.abspath(args.bin)  # scenarios run with cwd set to a temp dir
    failed = skipped = passed = 0
    with Mock(HERE / ".mock_requests.log") as mock:
        for s in scenarios:
            golden = read_golden(s["name"])
            if golden is None:
                print(f"MISSING  {s['name']} (run capture first)")
                failed += 1
                continue
            actual = run_scenario(args.impl, s, mock, args.bin)
            if "skipped" in actual:
                print(f"SKIP     {s['name']}: {actual['skipped']}")
                skipped += 1
                continue
            problems = compare(s, golden, actual, args.impl)
            if problems:
                failed += 1
                print(f"FAIL     {s['name']}")
                for p in problems:
                    print("    " + p.replace("\n", "\n    "))
            else:
                passed += 1
                print(f"ok       {s['name']}")
    print(f"\n{passed} passed, {failed} failed, {skipped} skipped ({args.impl})")
    return 1 if failed else 0


def cmd_list(args) -> int:
    for s in SCENARIOS:
        tag = " [shim]" if s["shim"] else ""
        print(f"M{s['milestone']} {s['name']:40}{tag}  {s['note']}")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("capture", help="record goldens from runner.py")
    p.add_argument("scenario", nargs="*")
    p.set_defaults(fn=cmd_capture)
    p = sub.add_parser("check", help="compare an implementation against the goldens")
    p.add_argument("--impl", choices=["python", "go"], required=True)
    p.add_argument("--bin", help="path to the Go binary (for --impl go)")
    p.add_argument("--milestone", type=int, default=99,
                   help="only scenarios at or below this PORT_PLAN.md milestone")
    p.add_argument("scenario", nargs="*")
    p.set_defaults(fn=cmd_check)
    p = sub.add_parser("export", help="write goldens/scenarios.json for the Go golden test")
    p.set_defaults(fn=cmd_export)
    p = sub.add_parser("list", help="list scenarios")
    p.set_defaults(fn=cmd_list)
    args = ap.parse_args()
    try:
        return args.fn(args)
    except RuntimeError as e:
        print(f"harness error: {e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
