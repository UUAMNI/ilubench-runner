# parity/ — characterization harness for `runner.py`

This directory is Milestone 0 of `PORT_PLAN.md`: a record of exactly what the
Python runner does, and a tool that holds any implementation to that record.
Nothing here is used by `runner.py` itself.

```bash
python3 parity/harness.py list                       # the scenarios and why each exists
python3 parity/harness.py capture                    # re-record goldens from runner.py
python3 parity/harness.py check --impl python        # runner.py against the goldens (must be green)
python3 parity/harness.py check --impl go --bin ./bin/ilubench
```

| File | Role |
|---|---|
| `mock_server.py` | Deterministic stand-in for the Anthropic, Google and OpenAI-compatible APIs and for the HuggingFace probe-set URL. Canned replies keyed by prompt; failure modes via `[[http:500]]`-style directives in probe prompts; logs every request. |
| `py_shim.py` | Runs `runner.py` unmodified with its three hardcoded hosts redirected to the mock. Only needed for the Anthropic/Google/HuggingFace paths; `--base-url` scenarios run `runner.py` directly. |
| `scenarios.py` | The 40 scenarios, each mapped to a row of `PORT_PLAN.md` section A5. |
| `harness.py` | Runs scenarios in scrubbed temp directories, masks volatile values, stores and diffs goldens. |
| `probes/` | Probe files for edge cases: failure directives, duplicate ids, bad JSON, missing fields, blank file. |
| `goldens/<scenario>/` | `exit_code.txt`, `stdout.txt`, `stderr.txt`, `requests.txt` (wire log), `files.txt`, and `tree/` (every file the run wrote, path flattened with `__`). `MANIFEST.txt` records the Python version and the `runner.py` hash the goldens came from. |

What is compared, per scenario: exit code (always exact); stdout (byte-exact
after masking, except tails that are CPython exception text, which are
normalized to `<ERR>` on both sides); stderr by mode (empty, usage error, or
Python traceback); the mock's request log (method, path, auth headers, compact
body: this pins "no system prompt, no sampling overrides, `max_tokens` only
for Anthropic"); the set of files written; and each file's masked contents.

Masks: `<TODAY>` (local date under `TZ=UTC`), `<DATE_UTC>`, `<WORK>`, `<REPO>`,
`<MOCK>`. Goldens are regenerated from Python, never hand-edited. If you run
`capture` on a different Python version, expect `MANIFEST.txt` to change and
review the diff before committing it.

Scenarios marked `[shim]` are skipped for `--impl go` by this harness; the Go
test suite runs them in-process with the provider endpoints pointed at the
same mock, reading the same goldens (Milestone 3).
