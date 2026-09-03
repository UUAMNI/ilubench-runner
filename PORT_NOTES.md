# PORT_NOTES.md — Go decisions in this codebase, explained

Appended throughout the port. Each entry is two or three plain sentences: what
was chosen, why, and what the alternative was. Written so the codebase can be
explained in an interview without the person who wrote it.

Entries below were decided during planning (see PORT_PLAN.md). Entries added
during implementation will be dated.

---

## Layout and process

**`cmd/` and `internal/`.** `cmd/ilubench/main.go` is the only `package main`
and only converts `os.Args` and the standard streams into a call to
`cli.Main`, then exits with its return value. Everything under `internal/` is
unimportable from outside this module; the Go toolchain enforces that, so it
is the standard way to say "this is a program, not a library."

**`Main(args []string, stdout, stderr io.Writer) int` instead of `os.Exit`
everywhere.** Python's `runner.py` returns an int from `main()` and lets
`sys.exit` handle it, and the Go version keeps that shape. Tests call `Main`
in-process with a `bytes.Buffer` for stdout and assert on bytes and exit code;
nothing in the tree calls `os.Exit` except the ten-line `main`.

**Packages by dependency direction, not by size.** `langid` and `pyjson`
import only the standard library (plus `x/text` for NFC) so they can be tested
against Python in isolation; `provider` knows HTTP but nothing about probes or
files; `run` composes them; `cli` knows only flags. A single package would
compile fine, but the boundaries are what make each piece testable without
the network.

**One external dependency: `golang.org/x/text/unicode/norm`.** The Python
runner calls `unicodedata.normalize("NFC", …)`, and Go's standard library has
no Unicode normalization. `x/text` is maintained by the Go team and is the
conventional answer; the alternative, vendoring composition tables by hand,
is more code and more risk for the same result.

## Parity-driven choices

**A custom JSON writer (`internal/pyjson`).** Python's `json.dumps` emits
`", "` and `": "` separators and leaves `<`, `>`, `&` and non-ASCII alone;
Go's `encoding/json` emits compact output and HTML-escapes by default. Since
`runs.jsonl` rows are committed and diffed by humans, the port writes rows
with a small hand-written encoder over a fixed struct rather than
post-processing `encoding/json` output.

**Regex character classes are spelled differently.** Python's `\w` in a `str`
pattern is Unicode-aware, so `[^\W\d_]` means "letters and non-decimal
numerics"; Go's RE2 `\w` is ASCII-only. The Go pattern uses
`[\p{L}\p{Nl}\p{No}]` explicitly, and a differential test against `python3`
proves the two tokenizers agree on a corpus.

**A `pyIsSpace` predicate instead of `unicode.IsSpace`.** Python's
`str.split()` and `strip()` treat U+001C through U+001F as whitespace; Go's
`unicode.IsSpace` does not. The word count and the 90-character opening in
`notes` depend on it, so a ten-line predicate reproduces Python's set exactly.

**Runes, not bytes.** Python slices strings by code point (`text[:90]`,
`body[:300]`); Go strings are bytes. Anywhere the Python runner truncates,
the Go code converts to `[]rune` first so an Igbo diacritic cannot be cut in
half.

**Timestamps are formatted by hand.** Python's `isoformat()` prints
microseconds and `+00:00`, and drops the fraction entirely when the
microsecond field is zero. Go's `time.RFC3339Nano` prints nanoseconds and `Z`,
so the raw archive uses a custom layout with a special case for the
one-in-a-million zero-microsecond instant.

**Paths are normalized the Python way.** `pathlib.Path` collapses `//` and
strips `./` and trailing `/`, but keeps `..` segments; Go's `filepath.Clean`
also resolves `..`. A small normalizer matches Python so the `evidence` and
`notes` strings are byte-identical, and `filepath.ToSlash` keeps forward
slashes everywhere.

**Errors go to stdout.** This is not Go convention, but it is the Python
runner's contract and anything parsing its output relies on it. Moving
`ERROR:` lines to stderr is listed as a candidate post-cutover change, not a
silent one.

**Exit codes are the contract.** `0` success or dry run, `1` handled errors
and the model-listing mode, `2` when any probe failed or on a usage error,
mirroring argparse. The `flag` package is configured with `ContinueOnError`
so the CLI layer, not the library, decides the exit code.

## Go-specific design (each a sign-off item in PORT_PLAN.md M4)

**`context.Context` on every request.** The root context is cancelled by
SIGINT via `signal.NotifyContext`, which aborts the in-flight HTTP call
cleanly instead of Python's traceback. Contexts are passed as the first
parameter by convention and never stored in structs.

**`Transport.ResponseHeaderTimeout` rather than `Client.Timeout`.** Python's
`urlopen(timeout=120)` is a per-socket-operation timeout, so a server that
takes 100 seconds to answer succeeds. `Client.Timeout` is a total deadline
and would break that; a header timeout on the transport is the closest
equivalent for a non-streaming API.

**Atomic archive writes.** Raw responses are written to a temp file in the
same directory and renamed into place, because `rename(2)` is atomic on POSIX
filesystems. Python's `write_text` can leave a truncated file on Ctrl-C; the
Go version cannot.

**Interfaces at the point of use.** `run` declares a small `Provider`
interface with `Complete` and `ListModels`; the three clients satisfy it
implicitly. This is how Go does dependency injection without a framework, and
it is what lets the parity tests swap in an `httptest` server.

**`httptest.Server` for provider tests.** Go's standard library can start a
real HTTP server on a random port inside a test, so the provider clients are
tested end-to-end over real sockets with canned responses and no mocking
library.

**Table-driven tests.** Each behavior in the contract becomes a row in a
`[]struct{ name string; in …; want … }` slice with `t.Run(tc.name, …)`.
This is the idiomatic Go shape and it maps one-to-one onto the
characterization scenarios captured from Python.

## Known deviations from `runner.py` (kept current; each has a parity scenario)

- Usage text on argparse-style errors differs; exit code 2 and stderr do not.
- Long-option prefix abbreviations (`--prov`) are not accepted.
- After a `FAIL <probe> <arm>:` prefix, non-HTTP error text is Go's, not
  Python's exception repr.
- Paths that Python would crash on with a traceback (missing `--out`
  directory, non-string provider content) produce an `ERROR:` line, exit 1.
- Ctrl-C exits 130 with a one-line notice on stderr instead of a traceback, and never leaves a partial raw file.
- `User-Agent` is `ilubench/<version>` instead of `Python-urllib/3.x`.
- Request bodies are compact JSON; Python sends spaced JSON. Semantically
  identical.

---

## Milestone 0 (2026-09-03): the characterization harness

**The harness is Python, not Go.** It exists to record what `runner.py` does,
so it lives next to the reference implementation and shares its language. The
Go module will stay free of test-tooling concerns; Go tests consume the
goldens as plain files.

**Wrap, never modify.** `parity/py_shim.py` imports `runner.py` and replaces
two functions at runtime (`runner._request_json` and
`urllib.request.urlopen`) to redirect the hardcoded Anthropic, Google and
HuggingFace hosts to a local mock. This is monkeypatching, which is
acceptable in test tooling and never in production code; the equivalent in Go
is a struct field holding the endpoint, set by `main` and overridden by tests.

**Determinism comes from the mock, masking, and a scrubbed environment.**
Canned replies are keyed by the exact prompt, so both implementations see the
same bytes; the mock also logs every request so the wire shape (no system
prompt, no sampling overrides, `max_tokens` only for Anthropic) is part of the
record. Volatile values (date, temp dir, repo path, port, `date_utc`) are
masked to placeholders, and each run gets `TZ=UTC`, a UTF-8 locale and no
`*_API_KEY` or proxy variables.

**Unreproducible tails are normalized, never ignored wholesale.** A handful of
stdout lines end in CPython exception text (`KeyError: 'choices'`, the JSON
decoder's message). Comparison keeps the stable prefix exact, including
`HTTP <code>: <body>`, and replaces only the tail with `<ERR>` on both sides.
This keeps redaction and HTTP error reporting under test while admitting the
one thing Go cannot copy.

**Goldens are flattened because of `.gitignore`.** The repo ignores any
directory named `runs_raw` and every `*.json`. Git cannot re-include files
inside an ignored directory, so written files are stored as
`tree/runs_raw__<name>.json` and a single negation rule re-includes JSON under
`parity/goldens/`.

**The edge fixtures exercise what the Go encoder will get wrong by default.**
The mock's raw responses carry `<b>&</b>` (Go HTML-escapes these), a U+2028
line separator (Go escapes it, Python does not), a U+001C control character
(both escape it, differently spelled), floats like `1E5` and `0.10` (Python
re-serializes them as `100000.0` and `0.1`), a 20-digit integer, empty
containers, and NFD-decomposed Igbo text with tone marks. Every one of these
is now a failing test waiting for Milestone 1.

**Known deviation noted during capture:** a probe file whose `id` is not a
string (for example a number) is accepted by Python and printed with Python's
repr; the Go port will reject it with `ERROR: bad probe set:` and exit 1. No
scenario pins this because Python's behavior is not a contract anyone relies on.

---

## Milestone 1 (2026-09-03): `langid` and `pyjson`

**The Go module pins `go 1.24` and one dependency.** `golang.org/x/text`
v0.30.0 is the last line that builds on Go 1.24 without a toolchain download,
and `go mod tidy` wrote the `toolchain` line automatically. Tests run with
`GOTOOLCHAIN=local` so a machine with the pinned Go never downloads another.

**`internal/pyref` is a test-only helper that lives in a normal package.** Go
has no notion of a test-support package; the convention is a small internal
package imported only from `_test.go` files. It shells out to `python3` and
skips the test when Python is absent, so `go test ./...` passes on a bare Go
machine while CI, which has Python, runs the differential tests for real.

**Differential tests generate their corpus in Go and ask CPython for the
answer.** Fixed table tests pin the cases already known; the generated corpus
(1,500 strings for `langid`, 600 values for `pyjson`, deterministic seed)
finds the cases nobody thought of. The reference is `runner.py` itself, not
a re-derivation, so a wrong understanding of Python cannot pass its own test.

**`langid` matched on the first run.** The three Unicode decisions made in
planning (RE2 `[\p{L}\p{Nl}\p{No}]` for Python's `[^\W\d_]`, the U+001C to
U+001F whitespace gap, and U+0130 in `lower()`) were sufficient: zero
mismatches over the generated corpus. `Lower` pre-expands U+0130 to
"i" + U+0307 because Python's `str.lower()` uses full case mapping and Go's
`strings.ToLower` uses the simple mapping; the combining mark then splits
the word in both tokenizers.

**`pyjson` keeps number literals, not float64s.** Python's `json` decides
int versus float by the literal's spelling: anything with `.`, `e` or `E` is
a float printed with `repr()`, everything else is an arbitrary-precision int
printed verbatim. `Number` is therefore a string; `formatNumber` parses it
with `math/big` or `strconv.ParseFloat` only at output time, and `1E5`
becomes `100000.0` exactly as the raw archives show.

**`FloatRepr` is Python's `repr(float)` layout over Go's shortest digits.**
`strconv.FormatFloat(f, 'e', -1, 64)` yields the same shortest round-trip
digits that CPython's `dtoa` produces; the difference is layout. Python
switches to scientific notation when the decimal point position is below
-3 or above 16, Go's `%g` switches at a different threshold, so the layout
is applied by hand. The differential test covers subnormals, `MaxFloat64`,
`-0.0` and out-of-range literals that become `Infinity`.

**Go's `-0.0` is `+0`.** A constant expression like `-0.0` is evaluated
exactly at compile time, and exact zero has no sign, so the test uses
`math.Copysign(0, -1)`. The kind of thing an interviewer asks.

**`Parse` uses `encoding/json` as a tokenizer.** `json.Decoder.Token()` with
`UseNumber()` returns delimiters, strings, literals and numbers one at a
time, which is enough to rebuild objects in insertion order with Python's
duplicate-key rule (first position, last value) and to reject trailing data
as `json.loads` does. Writing a JSON lexer by hand would be more code and
less trustworthy.

**The 68 golden documents are a test.** `TestGoldensRoundTrip` parses every
raw archive and `runs.jsonl` row the Python runner wrote in Milestone 0 and
requires this package to reproduce the bytes. It needs no Python, so it is
the byte-format regression test that runs everywhere.

---

## Milestone 2 (2026-09-03): CLI, probes, dry run, model listing

**`pystr` became its own package.** Milestone 1 kept the Python-string helpers
inside `langid`; Milestone 2 needed them in `probes` and `provider` too, and a
package that imports `langid` just to split lines would be the wrong
dependency direction. Moving five functions into `internal/pystr` early is
cheaper than living with the odd import; Go makes such moves mechanical
because every use is a qualified identifier.

**`flag` with `ContinueOnError` and a silenced `Usage`.** The default
`ExitOnError` mode calls `os.Exit` inside the library, which would make the
exit code untestable and the usage text unreadable from tests. Parsing
returns an error instead; `cli.Parse` decides that `-h` writes help to stdout
with exit 0 and everything else writes usage plus `ilubench: error: ...` to
stderr with exit 2, in the same order argparse checks things (invalid choice
first, then the inferred `compatible`, then the two custom checks).

**Injected dependencies live in one `Options` struct, not globals.** `run.Main`
takes stdout, stderr, an environment lookup, an HTTP client and the three
endpoint overrides. Tests fill them with buffers, a map and a mock; `main`
fills them with the real things in three lines. The overrides change where a
request goes and never what is printed or archived, so `Probe set:` still
names the published HuggingFace URL when the fetch was redirected to a mock,
exactly as the Python shim behaves.

**A sentinel error type per failure class, not error strings.** `runner.py`
routes probe-loading failures by exception type (`FileNotFoundError`,
`OSError`, `ValueError`). Go has no exception hierarchy, so `probes.Error`
carries a `Kind` and `errors.As` recovers it at the print site. Comparing
error strings would have worked today and broken on the first reworded
message.

**HTTP errors keep the body, transport errors keep the cause.** `HTTPError`
renders itself as `HTTP <code>: <first 300 code points>` and `NetError`
wraps the underlying `net` error, mirroring `urllib.error.HTTPError` versus
`URLError`. `provider.Detail` is the one place that turns either into the
line the user sees, with the key redacted, so no call site can forget.

**Python's `repr()` is a small package.** Two diagnostics print a Python list
literal, and Go's `%q` is not the same thing (double quotes, `\x1c` style
escapes). `pyrepr` implements the quote-selection and escaping rules and is
proven against CPython by a differential test; U+2028 turned out to be
non-printable in Python, so it is escaped, which the test caught before the
table did.

**Goldens are replayed in-process by `go test`.** `internal/run/golden_test.go`
reads `parity/goldens/scenarios.json`, starts the same Python mock the
harness uses, and calls `run.Main` per scenario with the endpoint overrides
standing in for the Python shim. The same masks and the same tail
normalization live in both `harness.py` and this test, on purpose: the Go
side proves the in-process path and the shim scenarios, the harness proves
the built binary. `t.Chdir` (Go 1.24) gives each scenario its own working
directory without touching the process for other packages.

**Scenarios carry a milestone number.** A scenario above the current
milestone is skipped, not failed, in both CI jobs. The number is raised in
one place per milestone (`golden_test.go` and the `--milestone` flag in
`go.yml`), which keeps CI honest about what the port claims to do today.

**Deviations added this milestone.** Non-string `id`, `prompt_en` or
`prompt_ig` values are rejected with `ERROR: bad probe set:` (Python accepts
them and fails later or sends null to the API). Python's `json.loads` accepts
`NaN` and `Infinity` literals; `pyjson.Parse` does not, so a probe file
containing them is refused. Neither occurs in any published probe set.

---

## Milestone 3 (2026-09-03): probe execution, raw archive, rows

**Every golden is green.** All 40 characterization scenarios pass in-process
(`go test ./internal/run`), and the 31 that do not need host redirection
pass through the built binary (`parity/harness.py check --impl go
--milestone 3`). `runs.jsonl` rows and raw archives are byte-identical to
Python's after date masking; the wire log shows the same requests in the
same order with the same auth headers.

**Response parsing copies Python's leniency per field, not a schema.**
runner.py reads some fields with `.get(key, default)` and indexes others
directly, so a missing `content` on Anthropic is an empty response while a
missing `choices` on OpenAI is a failed arm. `complete.go` reproduces that
distinction case by case and `TestTextExtraction` pins each one, including
the bad-shape fixture that is a failure on one dialect and an empty success
on the other two. A JSON schema would have been tidier and wrong.

**One struct per arm result.** `provider.Completion` carries the text, the
model id the API reported, and the raw value for the archive. Python returns
a three-tuple; a named struct is Go's equivalent and lets a field be added
later without touching every caller.

**The arm loop is a method on a `job`.** Once the plan has printed, the
remaining state (config, endpoint, probe set, client, secrets, clock) is
bundled into one struct so `execute` reads like the Python loop instead of
a function with nine parameters. Small structs used this way are how Go
code stays flat without classes.

**`pathlib` semantics needed their own function.** `filepath.Clean` resolves
`..` and pathlib does not, and `Path(".") / name` drops the dot while
`f"{raw_dir.as_posix()}/{name}"` keeps it, so `notes` and `evidence` can
differ for the same directory. `pyPath` and `pyJoin` reproduce exactly that,
proven by a differential test against `PurePosixPath` over generated paths.

**`isoformat()` needs a special case.** Python omits the fractional part
when the microsecond field is zero, which Go's fixed layouts cannot express,
so the timestamp is built in two steps. The parity goldens mask `date_utc`,
so this format is pinned by its own differential test instead.

**Archives are written atomically now (Milestone 4a, part one).** A temp
file in the same directory plus `rename(2)` means a reader can never see a
half-written archive. Parity does not observe this, which is why it could
land early; the signal handling half of 4a is still to come.

**Deviations added this milestone.** A provider returning a non-string
`model` (say `null`) yields the requested id in the row, where Python would
write `null`. Filesystem failures during the run (unwritable archive
directory, missing `--out` directory) print an `ERROR:` line and exit 1
instead of a traceback; the side effects before the failure are identical.
The `"content"` on the OpenAI dialect must be a string or null; Python would
carry a list through and crash later.

---

## Milestone 4 (2026-09-03): the approved Go-specific improvements

**Context first, as a parameter.** `run.Main(ctx, args, opts)` takes a
`context.Context` as its first argument, the Go convention, and passes it to
every HTTP request. `main` creates it with `signal.NotifyContext` for SIGINT
and SIGTERM. Nothing else in the program knows about signals; it only knows
that a context can end.

**Cancellation is checked at the boundary, not everywhere.** After any
request fails, the code asks `ctx.Err()` before deciding what the failure
means. A cancelled context becomes a one-line notice on stderr and exit code
130 (128 plus SIGINT's number, which is also what a shell reports for
runner.py's uncaught KeyboardInterrupt); anything else is reported as the
Python runner would. No FAIL line is printed for an abandoned request, and
nothing is appended to the rows file, so a pipeline reading stdout sees only
real results.

**Second Ctrl-C kills at once.** `signal.NotifyContext` keeps capturing the
signal after the first delivery, so a goroutine waits for the context to end
and then calls `stop()`, restoring the default handler. The first Ctrl-C is
graceful; the second is immediate. Three lines, worth knowing.

**Testing cancellation needs a server that notices.** The interrupt test
blocks a handler until the client goes away. Go's server only watches the
connection for that once the request body has been consumed, so a handler
that never reads the body never sees the disconnect and the test hung for
the full timeout. Reading the body first (and bounding the wait) fixed it,
and the test now proves the in-flight request is abandoned in well under a
second.

**The rest of Milestone 4 had already landed.** Response-header timeout
(4b) and the `ilubench/<version>` User-Agent (4c) were part of the HTTP
layer in Milestone 2; atomic archive writes (the file half of 4a) in
Milestone 3. Concurrency (4d) and retries (4e) stay out of v1 as agreed.
