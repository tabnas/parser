#!/usr/bin/env bash
# run-diff.sh — value-level cross-runtime differential testing via the
# json repo's behaviorally-mirrored CLIs: both runtimes must make the
# same accept/reject decision on every input, and accepted values must
# be deep-equal. This is the scaling path toward article-sized case
# counts (tens of thousands of generated inputs per CI week) and catches
# the class of bug the no-panic fuzzer is blind to: both runtimes
# accepting an input with DIFFERENT values.
#
# Outputs are canonicalized through one JSON parse/stringify (in node)
# before comparison, so JS-vs-Go number formatting differences don't
# drown real divergences.
#
# Usage: ci/fuzz/run-diff.sh [count] [seed]
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"
ROOT="${TABNAS_ROOT:-$(cd "$PARSER_ROOT/.." && pwd)}"
COUNT="${1:-200}"
SEED="${2:-979899}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "=== build go CLI ==="
GOWORK_DIR="$(mktemp -d)"
( cd "$GOWORK_DIR" && go work init "$PARSER_ROOT/go" "$ROOT/json/go" >/dev/null )
( cd "$ROOT/json/go" && GOWORK="$GOWORK_DIR/go.work" \
  go build -o "$WORK/tabnas-json" ./cmd/tabnas-json )
rm -rf "$GOWORK_DIR"

echo "=== generate corpus ==="
node "$DIR/gencorpus.js" "$WORK/corpus" "$COUNT" json "$SEED"

TSCLI="$ROOT/json/ts/bin/json"
[ -f "$ROOT/json/ts/dist/json-cli.js" ] || { echo "build json/ts first (dist/json-cli.js missing)" >&2; exit 2; }

# Compare two JSON documents for deep equality, canonicalized with
# recursively SORTED object keys (Go json.Marshal sorts keys, JS
# preserves insertion order — key order is not part of JSON equality)
# and one parse/re-stringify (so JS-vs-Go number formatting cannot
# cause false positives). File-based end to end: shell $(...) capture
# of large node output truncates (async pipe writes).
CMP_JS='
const fs = require("fs")
const sortk = (v) => {
  if (Array.isArray(v)) return v.map(sortk)
  if (v && "object" === typeof v) {
    const o = {}
    for (const k of Object.keys(v).sort()) o[k] = sortk(v[k])
    return o
  }
  return v
}
const canon = (f) => {
  try { return JSON.stringify(sortk(JSON.parse(fs.readFileSync(f, "utf8")))) }
  catch { return "UNPARSEABLE:" + f }
}
process.exit(canon(process.argv[1]) === canon(process.argv[2]) ? 0 : 1)
'

pass=0
fail=0
for input in "$WORK"/corpus/*.in; do
  # Both CLIs take the JSON source as their argument (argv joined),
  # mirroring each other — pass the file content, not the path.
  # Outputs are captured via file redirection, NOT $(...): Node's
  # stdout writes to a PIPE are asynchronous and the TS CLI's
  # process.exit can truncate large outputs mid-flush; writes to a
  # file descriptor are synchronous and complete.
  doc="$(cat "$input")"
  set +e
  node "$TSCLI" "$doc" > "$WORK/ts.out" 2>/dev/null; ts_code=$?
  "$WORK/tabnas-json" "$doc" > "$WORK/go.out" 2>/dev/null; go_code=$?
  set -e

  ok=1
  if [ "$ts_code" != "$go_code" ] && { [ "$ts_code" = 0 ] || [ "$go_code" = 0 ]; }; then
    ok=0
    why="exit codes differ: ts=$ts_code go=$go_code"
  elif [ "$ts_code" = 0 ]; then
    if ! node -e "$CMP_JS" "$WORK/ts.out" "$WORK/go.out"; then
      ok=0
      why="values differ"
    fi
  fi

  if [ "$ok" = 1 ]; then
    pass=$((pass + 1))
  else
    fail=$((fail + 1))
    echo "DIFF FAIL: $(basename "$input") ($why)"
    echo "  input: $(head -c 120 "$input" | tr '\n' ' ')"
  fi
done

echo
echo "fuzz-diff: $pass agree, $fail divergent (count=$COUNT seed=$SEED)"
[ "$fail" = 0 ]
