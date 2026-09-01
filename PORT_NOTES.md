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
- Ctrl-C exits 130 without a traceback and never leaves a partial raw file.
- `User-Agent` is `ilubench/<version>` instead of `Python-urllib/3.x`.
- Request bodies are compact JSON; Python sends spaced JSON. Semantically
  identical.
