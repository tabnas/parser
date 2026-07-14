#!/usr/bin/env bash
# run-bench.sh — the dual-runtime benchmark harness.
#
# Generates the deterministic fixture matrix (pinned seed — TS and Go
# read identical bytes), then runs the TS benchmarks (each parser in its
# own process) and the Go benchmarks (-benchmem). Numbers are advisory:
# compare against a baseline run on the SAME machine, back-to-back;
# never hard-gate CI on absolute thresholds.
#
# Usage: ci/bench/run-bench.sh [quick]
#   quick: fewer iterations / shorter benchtime.
# Requires the sibling layout (json/, jsonic/ next to this repo) with
# built ts/dist in all three (run ci/gate/run-gate.sh first, or npm
# build each).
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"
ROOT="${TABNAS_ROOT:-$(cd "$PARSER_ROOT/.." && pwd)}"
FIX="$DIR/fixtures"

MODE="${1:-full}"
if [ "$MODE" = quick ]; then ITERS=10 WARMUP=5 BENCHTIME=5x; else ITERS=30 WARMUP=15 BENCHTIME=2s; fi

echo "=== generate fixtures (pinned seed) ==="
node "$DIR/genfixture.js" "$FIX"

echo
echo "=== TS benchmarks ==="
for f in records-1mb.json records-escaped-1mb.json numbers-1mb.json records-16kb.json; do
  node "$DIR/bench.js" json "$FIX/$f" "$ITERS" "$WARMUP"
  node "$DIR/bench.js" native "$FIX/$f" "$ITERS" "$WARMUP"
done
node "$DIR/bench.js" jsonic "$FIX/records-1mb.json" "$ITERS" "$WARMUP"
node "$DIR/bench.js" jsonic "$FIX/text-1mb.jsonic" "$ITERS" "$WARMUP"

echo
echo "=== Go benchmarks ==="
GOWORK_DIR="$(mktemp -d)"
trap 'rm -rf "$GOWORK_DIR"' EXIT
( cd "$GOWORK_DIR" && go work init \
    "$DIR/gobench" "$PARSER_ROOT/go" "$ROOT/json/go" "$ROOT/jsonic/go" >/dev/null )
( cd "$DIR/gobench" && \
  GOWORK="$GOWORK_DIR/go.work" BENCH_FIXTURE_DIR="$FIX" \
  go test -run='^$' -bench=. -benchmem -benchtime="$BENCHTIME" -count=1 )
