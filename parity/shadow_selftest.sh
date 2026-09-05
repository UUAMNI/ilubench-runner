#!/usr/bin/env bash
# parity/shadow_selftest.sh: prove the shadow tooling against the parity mock
# before it is used with real keys. Runs shadow.sh three times (a clean run, a
# run with a failing arm, a model listing), expects PASS from each compare,
# then corrupts one row and expects the compare to FAIL. Used by CI.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
PY="${PYTHON:-python3}"
BIN="${1:-$REPO/bin/ilubench}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/ilubench-shadow-selftest.XXXXXX")"
trap 'kill "${MOCK:-}" 2>/dev/null || true; rm -rf "$WORK"' EXIT

if [[ ! -x "$BIN" ]]; then
  (cd "$REPO" && go build -o bin/ilubench ./cmd/ilubench)
  BIN="$REPO/bin/ilubench"
fi
# The compare shells out to `go run`; build the helper once so every compare is quick.
(cd "$REPO" && go build -o "$WORK/langidcheck" ./parity/langidcheck)
export ILUBENCH_LANGIDCHECK="$WORK/langidcheck"

PORT="$("$PY" -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
"$PY" "$HERE/mock_server.py" --port "$PORT" --log "$WORK/requests.log" --hf-probes "$REPO/examples/sample_probes.jsonl" &
MOCK=$!
for _ in $(seq 1 40); do
  "$PY" -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:$PORT/ok/v1/models', timeout=1)" 2>/dev/null && break
  sleep 0.25
done
MOCKURL="http://127.0.0.1:$PORT"
export OPENAI_API_KEY="sk-selftest-0001"
export TZ=UTC

expect() {
  local want="$1" name="$2"; shift 2
  set +e
  "$HERE/shadow.sh" --bin "$BIN" --python "$PY" --dir "$WORK/$name" -- "$@" > "$WORK/$name.log" 2>&1
  local got=$?
  set -e
  if [[ "$got" -ne "$want" ]]; then
    echo "selftest: $name: expected shadow.sh exit $want, got $got"; cat "$WORK/$name.log"; exit 1
  fi
  echo "selftest: $name: ok (exit $got)"
}

expect 0 clean    --base-url "$MOCKURL/ok/v1" --model mock-gpt --probe-set "$REPO/examples/sample_probes.jsonl"
expect 0 failing  --base-url "$MOCKURL/ok/v1" --model mock-gpt --probe-set "$REPO/parity/probes/edge.jsonl" --probes edge-fail-b,edge-long,edge-empty
expect 0 listing  --base-url "$MOCKURL/ok/v1" --probe-set "$REPO/examples/sample_probes.jsonl"

# Tamper with one Python row: the compare must notice the row no longer
# matches what its own heuristic computes.
"$PY" - "$WORK/clean/py/runs.jsonl" <<'EOF'
import json, sys
p = sys.argv[1]
rows = [json.loads(l) for l in open(p, encoding="utf-8") if l.strip()]
rows[0]["arm_A"]["output_language"] = "ig" if rows[0]["arm_A"]["output_language"] != "ig" else "en"
open(p, "w", encoding="utf-8").write("".join(json.dumps(r, ensure_ascii=False) + "\n" for r in rows))
EOF
set +e
"$PY" "$HERE/shadow_compare.py" "$WORK/clean" > "$WORK/tampered.log" 2>&1
got=$?
set -e
if [[ "$got" -ne 1 ]] || ! grep -q "recomputed" "$WORK/tampered.log"; then
  echo "selftest: tampered row was not detected (exit $got)"; cat "$WORK/tampered.log"; exit 1
fi
echo "selftest: tampered: ok (compare failed as it should)"
echo "selftest: all good"
