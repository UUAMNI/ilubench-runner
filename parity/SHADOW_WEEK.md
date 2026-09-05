# The shadow week

> Completed 2026-09-03/04; the report is in `docs/shadow-week-2026-09.md`.
> Kept as the procedure for any future re-verification (a new provider, a
> changed heuristic, a Python version bump).

Milestone 5 of `PORT_PLAN.md`. Every real run during this period is done twice
with identical arguments, Go first and then Python, and compared. This runs on
a machine with API keys and network access; the sandbox the port was built in
has neither.

## Setup (once)

```bash
git checkout claude/ilubench-python-go-port-wqkk3t
go build -o bin/ilubench ./cmd/ilubench
bash parity/shadow_selftest.sh          # proves the tooling against the mock; must print "all good"
export ANTHROPIC_API_KEY=... OPENAI_API_KEY=... GEMINI_API_KEY=...
```

## One shadow run

```bash
parity/shadow.sh -- --provider anthropic --model <model-id>
```

`shadow.sh` runs the Go binary in `shadow/<date>_run01/go/` and `runner.py` in
`shadow/<date>_run01/py/`, then runs `shadow_compare.py`, which prints a
verdict. `PASS` means no structural difference and no disagreement between
the two `output_language` implementations on any real response either
produced. Keep the `shadow/` directory: it is the evidence for the cutover
decision (it is git-ignored).

Cost: two API calls per arm, so double a normal run.

## Required coverage before the week counts

| # | What | Command | Done | Verdict |
|---|---|---|---|---|
| 1 | Anthropic dialect, full published probe set | `parity/shadow.sh --label anthropic -- --provider anthropic --model <id>` | | |
| 2 | Google native dialect | `parity/shadow.sh --label google -- --provider google --model <id>` | | |
| 3 | OpenAI or Moonshot dialect | `parity/shadow.sh --label openai -- --provider openai --model <id>` | | |
| 4 | An OpenAI-compatible `--base-url` (OpenRouter or local vLLM) | `parity/shadow.sh --label router -- --base-url https://openrouter.ai/api/v1 --api-key-env OPENROUTER_API_KEY --model <id>` | | |
| 5 | Model listing (no `--model`) | `parity/shadow.sh --label list -- --provider anthropic` | | |
| 6 | A deliberate failure (bad model id) | `parity/shadow.sh --label badmodel -- --provider anthropic --model no-such-model` | | |
| 7 | Live HuggingFace fetch (no `--probe-set`; runs 1 to 4 already do this) | covered by 1 to 4 | | |
| 8 | A local probe set and a subset | `parity/shadow.sh --label subset -- --provider anthropic --model <id> --probe-set examples/sample_probes.jsonl --probes ilu-001` | | |

The week is satisfied when every row above has a `PASS` and at least one
full probe-set run per dialect has been repeated on a different day.

## Reading a verdict

- `FAIL exit code` or `FAIL stdout differs`: the two implementations took
  different paths. Look at `go/stdout.txt` and `py/stdout.txt`; the stderr
  files hold the Go notice or the Python traceback.
- `FAIL reported model differs`: the API told the two runs different model
  ids on the same day. Re-run; if it persists, the provider is routing to a
  different version and that is worth knowing about the provider, not the
  port.
- `FAIL ... Go says X, Python says Y for a real response`: the one thing
  this week exists to find. The response text is in the archive named;
  file it against `internal/langid`.
- `NOTE`: response-dependent differences (which arm scored `mixed`, which
  optional keys the provider sent). Expected; not a parity problem.

## After the week

Follow `PORT_PLAN.md` B4 steps 3 to 5: the cutover commit, rollback by
switching the command back, retirement after one full real batch.
