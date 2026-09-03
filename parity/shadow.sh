#!/usr/bin/env bash
# parity/shadow.sh: run the Go binary and runner.py with identical arguments,
# side by side, against a real provider, then compare what they produced.
#
#   parity/shadow.sh [--bin PATH] [--python PATH] [--dir DIR] [--label NAME] -- <runner arguments>
#
#   parity/shadow.sh -- --provider anthropic --model <id>
#   parity/shadow.sh --label openrouter -- --base-url https://openrouter.ai/api/v1 --api-key-env OPENROUTER_API_KEY --model <id>
#
# Each implementation runs in its own working directory (DIR/go and DIR/py),
# so the default --out and --raw-dir apply and the two archives have identical
# relative paths. Do not pass --out or --raw-dir yourself. A relative
# --probe-set path is resolved against the directory you run this from.
# API keys come from your environment, exactly as for a normal run; both
# implementations see the same variables.
#
# Costs two API calls per arm (one per implementation). Runs the Go binary
# first, then Python, then parity/shadow_compare.py, whose exit status is
# this script's exit status: 0 means no structural difference and no
# disagreement between the two output_language implementations.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
BIN="$REPO/bin/ilubench"
PY="${PYTHON:-python3}"
DIR=""
LABEL=""

usage() { sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin) BIN="$2"; shift 2 ;;
    --python) PY="$2"; shift 2 ;;
    --dir) DIR="$2"; shift 2 ;;
    --label) LABEL="$2"; shift 2 ;;
    --) shift; break ;;
    -h|--help) usage; exit 0 ;;
    *) echo "shadow.sh: unknown option $1 (runner arguments go after --)" >&2; usage >&2; exit 2 ;;
  esac
done

for a in "$@"; do
  case "$a" in
    --out|--out=*|--raw-dir|--raw-dir=*)
      echo "shadow.sh: do not pass $a; each implementation writes runs.jsonl and runs_raw/ in its own directory" >&2
      exit 2 ;;
  esac
done

# Resolve a relative --probe-set against the invoking directory, since each
# implementation runs with a different working directory.
ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --probe-set)
      p="$2"; [[ "$p" == /* ]] || p="$PWD/$p"
      ARGS+=("--probe-set" "$p"); shift 2 ;;
    --probe-set=*)
      p="${1#--probe-set=}"; [[ "$p" == /* ]] || p="$PWD/$p"
      ARGS+=("--probe-set=$p"); shift ;;
    *) ARGS+=("$1"); shift ;;
  esac
done

if [[ ! -x "$BIN" ]]; then
  echo "shadow.sh: Go binary not found at $BIN; build it with: go build -o bin/ilubench ./cmd/ilubench" >&2
  exit 2
fi
BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"

TODAY="$(date +%F)"
if [[ -z "$DIR" ]]; then
  n=1
  while [[ -e "shadow/${TODAY}_${LABEL:-run}$(printf '%02d' "$n")" ]]; do n=$((n + 1)); done
  DIR="shadow/${TODAY}_${LABEL:-run}$(printf '%02d' "$n")"
fi
mkdir -p "$DIR/go" "$DIR/py"
DIR="$(cd "$DIR" && pwd)"
printf '%s\n' "${ARGS[@]+"${ARGS[@]}"}" > "$DIR/args.txt"

run_one() {
  local sub="$1"; shift
  (
    cd "$DIR/$sub"
    set +e
    "$@" > stdout.txt 2> stderr.txt
    echo $? > exit_code.txt
  )
}

echo "shadow: $DIR"
echo "== go      $BIN ${ARGS[*]+"${ARGS[*]}"}"
run_one go "$BIN" "${ARGS[@]+"${ARGS[@]}"}"
echo "   exit $(cat "$DIR/go/exit_code.txt")"
echo "== python  $PY $REPO/runner.py ${ARGS[*]+"${ARGS[*]}"}"
run_one py "$PY" "$REPO/runner.py" "${ARGS[@]+"${ARGS[@]}"}"
echo "   exit $(cat "$DIR/py/exit_code.txt")"

END="$(date +%F)"
{
  echo "started: $TODAY"
  echo "finished: $END"
  echo "go_binary: $BIN"
  echo "go_binary_sha256: $(sha256sum "$BIN" | cut -d' ' -f1)"
  echo "python: $("$PY" --version 2>&1)"
  echo "runner_py_sha256: $(sha256sum "$REPO/runner.py" | cut -d' ' -f1)"
  echo "invoked_from: $PWD"
} > "$DIR/manifest.txt"
if [[ "$END" != "$TODAY" ]]; then
  echo "WARNING: the date changed while the shadow run was in progress; run_ids and file names will differ by date." >&2
fi

exec "$PY" "$HERE/shadow_compare.py" "$DIR"
