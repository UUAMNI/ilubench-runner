# PORT_PLAN.md — ilubench-runner, Python → Go

Status: **approved 2026-09-03. Milestones 0 and 1 complete; Milestone 2 next.** `runner.py` is untouched and
stays runnable until the cutover in Milestone 6 is verified.

This document has two parts. Part A is the Phase 1 assessment (what the runner
actually does, measured, not assumed). Part B is the plan. Decisions that need
your sign-off are marked **[SIGN-OFF]**.

---

## Part A — Assessment

### A1. What the repo is

| Item | Fact |
|---|---|
| Files | `runner.py` (443 lines, 329 of code, 13 functions), `README.md`, `examples/sample_probes.jsonl` (2 probes), `.gitignore`, `LICENSE` |
| Dependencies | Python 3.10+ standard library only: `argparse`, `json`, `os`, `re`, `sys`, `unicodedata`, `urllib`, `datetime`, `pathlib` |
| Tests | **None.** No test files, no CI workflow. |
| Entry point | `python runner.py …` → `main()` → `sys.exit(int)` |
| History | One commit (v0.1, 2026-07-24) |

**Verdict: this is a good first Go port, not a poor one.** It is one binary with
a narrow, fully observable contract (CLI in, stdout + two file formats out, four
HTTP dialects). Nothing in it fights Go. The one thing Go lacks in its standard
library is Unicode NFC normalization, which `golang.org/x/text` (maintained by
the Go team) provides. The port is small enough to finish and large enough to
exercise the Go surfaces that matter in interviews: `net/http`, `encoding/json`
edge cases, `flag`, `os`/`filepath`, Unicode handling, `httptest`, table-driven
tests, `context`, and exit-code discipline.

### A2. Where this runner sits in the wider UUAMNI pipeline

I read the two repos you attached. This matters for what "real use" means:

- **`UUAMNI/ilubench` (dataset repo).** Holds `probe_set_v0.jsonl` (5 probes),
  `runs_v0.jsonl` (25 hand-scored rows), `rubric.md`, and
  `scripts/run_probes.py`, the *predecessor* of this runner. **All 22 API rows in
  the published matrix were produced by `run_probes.py`, not by `runner.py`**:
  their `run_id`s are `run-DATE-api-PROVIDER-PROBE` and their evidence paths
  are `runs_api_raw/…`, whereas `runner.py` writes
  `run-DATE-PROVIDER-MODELSLUG-PROBE` and `runs_raw/…`. `run_probes.py` also has
  a retry policy (4 attempts, backoff, on 401/408/429/5xx/529) that `runner.py`
  dropped.
- **`UUAMNI/uuamni-uche` (platform).** Has its own in-platform executor
  (`backend/services/benchmark.py`, "ported verbatim from ilubench-runner") that
  writes to Supabase tables (`benchmark_runs`, `benchmark_scores`, migration
  039), a v0.2 language-id (`services/langid.py`, adds `other_lang:yo`), and
  `scripts/ilubench_import.py`, which loads `runs_v0.jsonl` rows into
  production. Uche's Google adapter uses Gemini's OpenAI-compatible endpoint;
  `runner.py` uses the native `generateContent` API.
- **"uuamni-agents state conventions."** `runner.py` shares nothing with them.
  The only references anywhere are in Uche docs pointing at
  `~/Postman/UUAMNI/uuamni-agents/state/…` and `uuamni-agents/outputs/…` for
  proverb curation. The runner has no log files, no state files, no config
  files, no lock files. Its only state is `runs.jsonl` (append-only, meant to
  be committed) and `runs_raw/` (local evidence, git-ignored).

So the honest picture: `runner.py` is the *public reproduction path* and the
superset of `run_probes.py`, but no published row has come from it yet. The
plan defines "real use" accordingly (see Milestone 6 and Open Question 1).

### A3. External surfaces

**CLI flags** (all optional at parse time; validation after):
`--provider {anthropic,compatible,google,moonshot,openai}`, `--model`,
`--base-url`, `--api-key-env`, `--probe-set PATH`, `--probes ID,ID`,
`--out PATH` (default `runs.jsonl`), `--raw-dir DIR` (default `runs_raw`),
`--dry-run`, `-h/--help`.

**Environment read:** the key variable (default per provider:
`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `MOONSHOT_API_KEY`;
`compatible` defaults to `OPENAI_API_KEY`; `--api-key-env` overrides). Also,
implicitly via `urllib`: `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` and the
locale (affects stdout encoding).

**Network:**

| Call | Method + URL | Auth header |
|---|---|---|
| Probe set (when no `--probe-set`) | GET `https://huggingface.co/datasets/UUAMNI/ilubench/resolve/main/probe_set_v0.jsonl` (timeout 60s) | none |
| Anthropic message | POST `https://api.anthropic.com/v1/messages` (`max_tokens: 2048`) | `x-api-key`, `anthropic-version: 2023-06-01` |
| Anthropic models | GET `https://api.anthropic.com/v1/models` | same |
| Google message | POST `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` | `x-goog-api-key` |
| Google models | GET `…/v1beta/models` (strips `models/` prefix) | same |
| OpenAI-compatible message | POST `{base_url}/chat/completions` (no `max_tokens`) | `Authorization: Bearer` (omitted when no key) |
| OpenAI-compatible models | GET `{base_url}/models` | same |

All calls: `Content-Type: application/json`, socket timeout 120s, no retries,
redirects followed, `User-Agent: Python-urllib/3.x`. No system prompt, no
sampling parameters. The request body is Python's spaced JSON
(`{"model": …, "messages": […]}`); semantically identical to compact JSON.

**Files written:**

1. `{raw_dir}/.gitignore` containing `*\n` — created (and overwritten) on every
   non-dry run *before* any API call, but *after* model listing.
2. `{raw_dir}/{today}_{provider}_{slug(model)}_{probe}_{arm}.json` — one per
   successful arm, `indent=2`, `ensure_ascii=False`, keys in this order:
   `date_utc, provider, endpoint, requested_model, reported_model, probe_id,
   arm, prompt, response_text, raw_api_response`. Overwritten on same-day
   re-runs. Written for arm A even when arm B then fails.
3. `{out}` — one JSON line per fully successful probe, appended, Python's
   default separators (`", "` and `": "`), `ensure_ascii=False`, 12 keys in the
   README's order. Parent directory must already exist (else an uncaught
   traceback).

**Stdout** carries everything, including `ERROR:` lines. **Stderr** carries
only argparse usage errors and uncaught tracebacks.

**Exit codes:** `0` success or dry run · `1` any handled error (bad probe set,
unknown probe ids, no key, model-listing mode or listing failure) · `2` at
least one probe failed during the run, *or* an argparse error · `1` with a
traceback for uncaught exceptions · SIGINT on Ctrl-C.

### A4. Dependency classification

| Python | Go | Class |
|---|---|---|
| `argparse` | `flag` | stdlib, with two behavioral gaps (abbreviations, usage text) — see A5 |
| `json` | `encoding/json` | stdlib, but byte-exact parity needs a ~60-line custom writer (`internal/pyjson`) — see A5 |
| `urllib.request/error` | `net/http` | stdlib; timeout semantics differ — see A5 |
| `re` (Unicode `\w`) | `regexp` (RE2) | stdlib; `[^\W\d_]` must be spelled `[\p{L}\p{Nl}\p{No}]` — verified equivalent |
| `unicodedata.normalize("NFC")` | `golang.org/x/text/unicode/norm` | **needs a Go library** (the only one). Reachable from the module proxy here. |
| `str.lower()`, `str.split()`, `str.strip()` | `strings.ToLower`, custom `pyIsSpace` | stdlib; Python treats U+001C–U+001F as whitespace, Go's `unicode.IsSpace` does not — a 10-line predicate closes the gap |
| `pathlib.Path` | `path/filepath` + small normalizer | stdlib; Python keeps `..` segments, `filepath.Clean` removes them — see A5 |
| `datetime.isoformat()` | `time.Format` + zero-microsecond special case | stdlib |
| `os.environ`, `sys.exit` | `os` | stdlib |

Nothing is awkward in Go. No SDKs, no async, no dynamic typing tricks, no
metaprogramming.

### A5. The implicit contract, and what a port could silently change

Everything below was measured against `runner.py` in this session (Python
3.11.15), not inferred. Items marked ⚠ are where a naive Go port would drift.

| # | Behavior | Measured |
|---|---|---|
| 1 | ⚠ `runs.jsonl` row bytes | `{"run_id": "…", "date": "…", …}` — space after every comma and colon, non-ASCII unescaped, `<>&` unescaped. Go's `encoding/json` is compact and HTML-escapes by default. |
| 2 | ⚠ Raw archive bytes | Python `indent=2` layout equals Go's `MarshalIndent("", "  ")` layout. But Python **re-serializes** the provider's JSON: `1E5`→`100000.0`, `1e-7`→`1e-07`, `0.10`→`0.1`; key order preserved as received. |
| 3 | ⚠ Timestamps | `date_utc` = `2026-09-01T23:52:22.645591+00:00` (microseconds, `+00:00`, and the fraction is **omitted** when microsecond == 0). `date`/`today` = **local** date, computed once per run. |
| 4 | ⚠ Language heuristic | `\w` is Unicode; combining marks split words (`pụ́tara` → `pụ`, `tara`); only ASCII `'` joins `n'otu`; `ga-` in the stopword list can never match; the English article **`a` is in the Igbo list**, so plain English with ≥1 `a` per 20 words scores `mixed` (this is why so many published `arm_A` rows read `mixed`). Must be preserved bit-for-bit for row comparability. |
| 5 | ⚠ `notes` field | `~N words` uses Python's Unicode `split()`; `Opens:` is the first **90 code points** (not bytes) of whitespace-collapsed text. |
| 6 | ⚠ Errors to stdout | All `ERROR:`/`FAIL` lines go to stdout, exit 1/2. Go idiom would be stderr. |
| 7 | ⚠ argparse | Exit 2 + usage on stderr for missing `--provider`, bad choice, `compatible` without `--base-url`. Accepts prefix abbreviations (`--prov`). Go `flag` prints a different usage text and has no abbreviations. |
| 8 | ⚠ Timeout semantics | 120s is a per-socket-operation idle timeout, not a total deadline. A slow-but-streaming server never times out in Python; a Go `Client.Timeout` would. |
| 9 | ⚠ Error text | HTTP errors: `HTTP 500: <first 300 chars of body, key redacted>` — reproducible. Non-HTTP errors embed Python exception names (`KeyError: 'choices'`, `TimeoutError: timed out`, `network error: …`) — **not reproducible** in Go. |
| 10 | `--probes` parsing | Split on `,`, strip, drop blanks. Duplicates are kept: `ilu-001,ilu-001` runs the probe twice, appends two rows with the same `run_id`, and overwrites the raw files. `--probes ""` means all. |
| 11 | Probe file validation | Presence of `id`, `prompt_en`, `prompt_ig` only; values may be any JSON type. Duplicate ids: first occurrence's position, last occurrence's content. Blank lines skipped. Invalid JSON → `ERROR: bad probe set:` exit 1. |
| 12 | Probe file errors | Missing file → `ERROR: probe set file not found`. A **directory** or unreadable file → the misleading `could not fetch the probe set from HuggingFace` message (both exit 1). |
| 13 | Arm failure semantics | Arm A fails → arm B is not called. Arm B fails → arm A's raw file stays, no row, probe counts as failed. Exit 2 if any failed; rows for successful probes are still appended. |
| 14 | Row `model` | The `model` the API reported on **arm B** (falls back to the requested id if the response lacks one). Filenames use `slug(requested)`; raw files record both. |
| 15 | Model listing | No `--model` (or `--model ""`) → lists ids sorted by code point (uppercase first), exit 1, nothing written. |
| 16 | `--base-url` with `anthropic`/`google` | Ignored for the actual calls (hardcoded hosts) but printed as the endpoint and recorded in raw files. A quiet bug. |
| 17 | Path handling | `runs_raw/`→`runs_raw`, `./x`→`x`, `a//b`→`a/b`, `""`→`.`, but `a/../b` stays `a/../b`. Paths in outputs use forward slashes (`as_posix`). |
| 18 | Redaction | Only the exact key string is replaced with `[redacted]`, only in HTTP-body and generic error text. Never printed elsewhere; never written to files. |
| 19 | Dry run | Still fetches the probe set from HuggingFace when no `--probe-set` (network, not a write). Prints plan, key status, exits 0. |
| 20 | Key check order | The plan prints first, then the key check fails (exit 1). `compatible` proceeds without a key with a `Note:` line. |
| 21 | Uncaught paths | `--out` in a missing directory → traceback, exit 1, **after** all API calls ran and raw files were written. Non-string `content` from a provider → traceback. |
| 22 | Ctrl-C | Traceback, SIGINT exit; a raw file mid-write can be left truncated (`write_text` is not atomic). |
| 23 | Output buffering / encoding | Python block-buffers stdout when piped; stdout encoding follows the locale (a non-UTF-8 locale crashes on the em-dash in the `Model:` line). Go always writes UTF-8, unbuffered. |
| 24 | Wire details | UA `Python-urllib/3.x`; `Accept-Encoding: identity`; proxies from env; system CA roots. |

### A6. Test coverage

Zero. Parity **cannot** be verified from existing tests. Per your rule, a
characterization suite against the Python runner is Milestone 0 and the first
thing built. Uche's tests cover Uche's own port, not this file.

### A7. Size

| Measure | Number |
|---|---|
| Python | 443 lines (329 code), 13 functions, 4 HTTP dialects, 3 output artifacts |
| Distinct behaviors in the contract (A3 + A5) | ~45 |
| Estimated Go | 900–1,200 lines of production code across 7 packages; 700–1,000 lines of tests; ~300 lines of parity harness (Python mock server + shim + compare script) |
| Estimated effort | **7 focused sessions** plus **one calendar week** of shadow runs on your machine (this sandbox has no API keys and cannot reach HuggingFace or api.openai.com) |

---

## Part B — Plan

### B1. Target layout

```
ilubench-runner/
├── runner.py                    # untouched reference implementation
├── go.mod                       # module github.com/UUAMNI/ilubench-runner, go 1.24
├── cmd/ilubench/main.go         # 10 lines: os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
├── internal/
│   ├── cli/                     # flag parsing, argparse-compatible error semantics, Config
│   ├── probes/                  # load + validate JSONL from a path or URL
│   ├── provider/                # Client interface; anthropic.go, openai.go, google.go, http.go (redaction, error detail)
│   ├── langid/                  # detect_output_language, factual_notes, Python-compatible whitespace/word rules
│   ├── pyjson/                  # Python-compatible JSON writer for runs.jsonl rows and raw archives
│   └── run/                     # orchestration: plan printing, dry run, model listing, arm loop, archive, append rows
├── parity/                      # Milestone 0: mock server, Python shim, golden fixtures, compare script
├── .github/workflows/go.yml     # vet, test, parity suite (ubuntu runner has python3)
├── PORT_PLAN.md
└── PORT_NOTES.md
```

Why this shape (each of these is an interview answer, expanded in PORT_NOTES):

- `cmd/ilubench` is the only `main` package and does nothing but call into
  `internal` and exit. Everything testable lives behind a function that takes
  arguments and writers and returns an exit code, so tests never call
  `os.Exit`.
- `internal/` is enforced by the Go toolchain: no other module can import
  these packages. This is a binary, not a library, and the directory says so.
- Packages follow dependency direction, not file size. `langid` and `pyjson`
  import nothing of ours and can be tested in isolation against Python;
  `provider` knows HTTP but not probes; `run` ties them together; `cli` knows
  only flags. The binary name is `ilubench` because that is what you will type.
- Endpoints are fields on the provider clients, set by `run` from constants.
  The parity tests point them at an `httptest` server in-process. There is
  no hidden environment variable for this, so the shipped binary has no
  undocumented surface.

### B2. Milestones (riskiest unknowns first, each independently runnable)

**M0 — Characterization harness against Python (step one, per your rule).**
Deliverable: `parity/` with (a) a mock provider server speaking all three
dialects with canned responses, including deliberate failures, an empty
response, HTML characters, odd floats, and a key in an error body; (b)
`py_shim.py`, which imports `runner.py` unmodified and redirects the hardcoded
Anthropic/Google/HuggingFace hosts to the mock; (c) ~20 scenarios covering every
row in A5 that is observable without real keys; (d) `parity/harness.py capture`, which
runs Python and stores goldens (stdout, stderr, exit code, `runs.jsonl` bytes,
raw files with `date_utc` masked, plus a manifest recording the Python
version). Exit criterion: Python-vs-Python is green, so the harness itself is
proven before any Go exists.

**M1 — `langid` and `pyjson` with differential tests.** The riskiest port
work is Unicode and JSON bytes, so it lands first with no CLI around it.
`langid` gets table tests from A5 row 4 plus a differential test that shells
out to `python3` and compares classifications over a corpus (the sample
texts, the 25 published rows' notes, and every `response_text` in any
`runs_raw/` directory you point it at). `pyjson` gets byte-for-byte tests
against `json.dumps` output for the row struct and the archive struct.
Exit criterion: zero disagreements.

**M2 — CLI, probes, dry run, listing.** `ilubench --provider … --dry-run`
passes every non-network golden from M0 (usage errors, bad probe files,
unknown ids, missing key, dry-run plan text). CI workflow added.
Exit criterion: those goldens green.

**M3 — Providers against the mock.** All three dialects, model listing,
redaction, arm-failure semantics, raw archive layout, row append. The
OpenAI-compatible path also runs as a *built binary* via `--base-url` (no shim
needed on either side), which proves the real executable, not just the
in-process test. Exit criterion: **every M0 golden green with Go.**

**M4 — Go-specific improvements. [SIGN-OFF, each separately]**
- **M4a `context` + SIGINT.** Ctrl-C cancels the in-flight request, no
  traceback, exit 130, and raw files are written via temp-file-and-rename so
  a truncated archive can never exist. Changes: stderr noise and file
  atomicity only.
- **M4b Timeout.** Use `Transport.ResponseHeaderTimeout = 120s` (closest to
  urllib's socket timeout for a non-streaming API) rather than
  `Client.Timeout`. Recommended: yes.
- **M4c User-Agent** `ilubench/<version>`. Deviation from
  `Python-urllib/3.x`; some gateways treat UAs differently. Recommended: yes,
  and the shadow week will tell us if any provider cares.
- **M4d Concurrency — NOT in v1.** Goroutines per probe would change output
  ordering and the "arm B is skipped when arm A fails" rule. Recommended:
  ship v1 strictly sequential; add `--concurrency N` (default 1, identical
  behavior) as a post-cutover milestone if you want it.
- **M4e Retries — NOT in v1.** `run_probes.py` had them; `runner.py` does
  not. Recommended: post-cutover `--retries N` (default 0), mirroring the
  older policy, since a retry changes which response gets archived.

**M5 — Real-key parity tooling.** `parity/shadow.sh` runs both runners on the
same day against the same model into separate outputs;
`parity/shadow_compare.py` checks structure (`run_id`, `date`, `provider`,
`model`, `probe_id`, `interface`, all `pending_*` stamps, `evidence` pattern,
raw-file naming), and the M1 differential test is re-run over the union of
both raw archives so the two `output_language` implementations are compared
on real model text, not just canned text. The live HuggingFace fetch path is
tested here too. This milestone runs on **your machine** (keys, network).

**M6 — Cutover.** See B4.

### B3. Parity strategy (kept green through the whole port)

- **Golden tests run both runners.** Every scenario in `parity/` executes
  `python3 runner.py` (via the shim when a hardcoded host is involved) and the
  Go binary against the same mock, then diffs: stdout byte-exact except lines
  tagged `prefix` (A5 row 9: the `FAIL <probe> <arm>:` prefix is exact, the
  Python exception text after it is not reproducible); stderr compared for
  emptiness/non-emptiness and exit code only for argparse errors (usage text
  differs by design); `runs.jsonl` byte-exact; raw files byte-exact after
  masking `date_utc`; file listings exact.
- **Same-day rule.** `date` is the local date, so the harness sets `TZ` and
  refuses to run across midnight instead of masking dates.
- **Goldens are regenerated from Python, never hand-edited.** The manifest
  records the Python version; you regenerate once on your machine so drift
  between 3.11 here and whatever you run is caught, not hidden.
- **CI runs the suite on every push.** `go vet`, `go test ./...`, then
  `parity/harness.py check --impl go`.
- **Deviations are a list, not a surprise.** `PORT_NOTES.md` carries a
  "Known deviations" section; the parity suite has one test that asserts the
  list is complete (each deviation has a scenario that exercises it).

### B4. Cutover plan

Your real pipeline today is: you run the runner by hand against a model →
rows go into `runs.jsonl` → rows are committed to the dataset repo
(`runs_v0.jsonl`) → `scripts/ilubench_import.py` posts them into Uche. The
entry point is the command you type, so cutover is one command changing.

1. **Install.** `go install github.com/UUAMNI/ilubench-runner/cmd/ilubench@<tag>`
   or a release binary from GitHub Actions (linux/darwin, amd64/arm64).
2. **Shadow week (7 calendar days, on your machine).** Every real run is done
   twice with identical arguments: `ilubench … --out runs.jsonl --raw-dir
   runs_raw` and `python runner.py … --out runs.shadow.jsonl --raw-dir
   runs_raw_py`. Minimum coverage before the week counts: one run per dialect
   (anthropic, google, openai or moonshot), one OpenRouter or local
   `--base-url` run, one model-listing invocation, one deliberate failure
   (bad model id) to compare exit codes, one run without `--probe-set` (live
   HuggingFace fetch). `shadow_compare.py` must report zero structural diffs
   and zero `output_language` disagreements across the union corpus.
3. **Cutover commit.** README quickstart switches to `ilubench`; the Python
   section is retitled "reference implementation"; `runner.py` stays in the
   tree. The Go rows from the shadow week are the ones committed to the
   dataset repo (they are byte-compatible with what Python would have
   written).
4. **Rollback.** Run `python runner.py` with the same arguments. Formats are
   identical, so Python rows can be appended to a Go-written `runs.jsonl`
   and vice versa. Nothing to migrate, nothing to uninstall.
5. **Retirement.** After the shadow week *and* one full real batch committed
   from Go output, Python is frozen: README marks it legacy and CI stops
   running the Python side of the parity suite by default (it stays runnable
   with one flag). Deleting `runner.py` is a separate decision for you, later.

### B5. Risks and open questions (with recommended answers)

1. **Which Python runner is "in real use"?** No published row came from
   `runner.py`; `run_probes.py` produced the matrix and Uche now has its own
   executor. **Recommend:** port `runner.py` (public, superset, the one you
   asked for); define "real use" as *the next evidence batch for the dataset
   is produced by the Go binary with Python shadowing*, and leave Uche's
   executor alone. If you instead want the Go binary to feed Uche directly,
   that is a new feature (HTTP POST to `/admin/benchmark/…`), not a port, and
   should be a milestone after cutover.
2. **Byte-exact or semantic parity for `runs.jsonl`?** Rows are committed and
   reviewed in PRs, so whitespace shows up in diffs. **Recommend:** byte-exact,
   via `internal/pyjson`. For raw archives, byte-exact after masking
   `date_utc`; the float re-serialization quirk (A5 row 2) is matched by
   decoding numbers to Go floats and printing them the way Python does, which
   the M1 tests pin down.
3. **Timeout semantics** (A5 row 8). **Recommend:** M4b as written.
4. **argparse abbreviations and usage text** (A5 row 7). **Recommend:** drop
   abbreviations, keep exit code 2 and stderr, accept different usage text.
   Nobody scripts against usage text; abbreviations are a footgun.
5. **Non-reproducible error text** (A5 row 9). **Recommend:** exact prefix,
   Go's own error text after it, tagged in goldens.
6. **Uncaught-exception paths** (A5 rows 21–22). **Recommend:** Go prints
   `ERROR: …` and exits 1 with the same side effects (raw files already
   written). Cleaner than a traceback, same code, listed as a deviation.
7. **The misleading "could not fetch from HuggingFace" message for a local
   directory** (A5 row 12) and **`--base-url` ignored for anthropic/google**
   (row 16). **Recommend:** preserve both exactly through cutover (parity
   first), fix both in Go immediately after retirement, and never touch
   Python.
8. **The `a` stopword makes English text score `mixed`** (A5 row 4). This is
   a finding about your published data, not a port question. **Recommend:**
   preserve bit-for-bit (rows must stay comparable), and raise it with the
   rubric v0.2 work in Uche, where `langid.py` already supersedes this
   heuristic.
9. **Concurrency and retries** (M4d, M4e). **Recommend:** not in v1.
10. **Distribution.** **Recommend:** `go install` plus GitHub release binaries
    built by Actions on tag; no Homebrew, no Docker.
11. **Python version drift.** Goldens captured here are from 3.11.15.
    **Recommend:** regenerate once on your machine in M0; the manifest makes
    any difference visible.
12. **This sandbox cannot reach HuggingFace or api.openai.com and has no
    keys.** M0–M4 run fully here against the mock; M5 and the shadow week
    need your machine. **Recommend:** the harness is one script so that cost
    is a few minutes per run.
13. **The one external dependency** (`golang.org/x/text`). **Recommend:**
    accept it; hand-rolling NFC tables would be worse engineering and a
    worse interview answer.
14. **Windows.** `runner.py` is technically portable. **Recommend:** target
    Linux and macOS; use `filepath.ToSlash` so outputs are identical if
    someone does run it on Windows, but do not test there.

### B6. What I will not do without asking

Write any Go before you approve this plan. Modify or delete `runner.py`,
`README.md`'s Python instructions, or `examples/`. Add flags, subcommands,
environment variables, or output fields beyond the Python contract. Change
the protocol invariants (fresh call per arm, no system prompt, default
sampling, `max_tokens: 2048` on Anthropic only).
