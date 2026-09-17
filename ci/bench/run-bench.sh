#!/usr/bin/env bash
# run-bench.sh — the three-runtime benchmark harness.
#
# Generates the deterministic fixture matrix (pinned seed — TS and Go
# read identical bytes), then runs the TS and Rust benchmarks (each in
# its own process) and the Go benchmarks (-benchmem). Numbers are advisory:
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
if [ "$MODE" = quick ]; then
  ITERS=10 WARMUP=5 RUST_ITERS=3 RUST_WARMUP=1 BENCHTIME=5x
else
  ITERS=30 WARMUP=15 RUST_ITERS=10 RUST_WARMUP=5 BENCHTIME=2s
fi

# --- TS wiring: measure the WORKING TREE, not whatever npm installed ---
# Without this the harness benchmarks the PUBLISHED @tabnas/parser that
# json/ and jsonic/ resolve from their own node_modules, so an engine
# change under test contributes nothing to the numbers and the run looks
# entirely normal. Relying on run-gate.sh to have wired it first is not
# enough: it is a separate script a bench run need not have executed.
. "$DIR/../lib/wire.sh"
link_ts_dep "$ROOT/json/ts" parser "$PARSER_ROOT/ts"
link_ts_dep "$ROOT/jsonic/ts" parser "$PARSER_ROOT/ts"
link_ts_dep "$ROOT/jsonic/ts" json "$ROOT/json/ts"
if [ -d "$ROOT/debug/ts" ]; then
  link_ts_dep "$ROOT/jsonic/ts" debug "$ROOT/debug/ts"
fi
echo "=== wired @tabnas/parser -> $PARSER_ROOT/ts ==="

echo "=== generate fixtures (pinned seed) ==="
node "$DIR/genfixture.js" "$FIX"

echo
echo "=== TS benchmarks ==="
# records-cjk-1mb.json is the non-ASCII arm: without it every fixture
# here is ASCII (the escape-dense one included, since escape SEQUENCES
# are ASCII bytes) and the per-character fallback scan path is never
# measured. Generating it and not benchmarking it is worse than not
# having it — it reads as coverage and produces no timing data.
for f in records-1mb.json records-escaped-1mb.json numbers-1mb.json records-16kb.json records-cjk-1mb.json; do
  node "$DIR/bench.js" json "$FIX/$f" "$ITERS" "$WARMUP"
  node "$DIR/bench.js" native "$FIX/$f" "$ITERS" "$WARMUP"
done
# Pathologies: one axis each, everything else small. An ordinary document
# is wide AND shallow AND small-tokened at once, so no single axis gets far
# enough on it to separate O(n) from O(n^2) -- which is where both of this
# engine's quadratics hid. No `native` arm: these are about THIS parser's
# shape, and encoding/json has nothing to say about the jsonic one.
for f in wide-40k.json longstring-1mb.json numeric-edge-1mb.json separators-1mb.json tiny.json; do
  node "$DIR/bench.js" json "$FIX/$f" "$ITERS" "$WARMUP"
done
node "$DIR/bench.js" jsonic "$FIX/records-1mb.json" "$ITERS" "$WARMUP"
node "$DIR/bench.js" jsonic "$FIX/text-1mb.jsonic" "$ITERS" "$WARMUP"
# Ambiguity is the classic parser pathology and strict JSON cannot express
# it, so it only reaches the relaxed grammar.
node "$DIR/bench.js" jsonic "$FIX/ambiguous-1mb.jsonic" "$ITERS" "$WARMUP"

echo
echo "=== Rust benchmarks ==="
# Built in the configuration rs/README.md documents (lto = "fat",
# codegen-units = 1, mimalloc), which is also the configuration #179's
# instruction counts were taken in. A default release build is a
# different binary and is not what these rows should report.
cargo build --release --manifest-path "$DIR/rustbench/Cargo.toml"
RUST_BENCH="$DIR/rustbench/target/release/tabnas-rustbench"
# numbers-1mb.json overflows the default 8 MiB main-thread stack on the
# Rust port. The fixture is a FLAT array -- nesting depth 1 -- so this is
# not deep input, and TS and Go parse the same bytes without complaint.
# 16 MiB is enough. The limit is raised here rather than inside the
# benchmark so that running the binary directly still shows the real
# behaviour; this is a defect to fix in rs/, not a harness convenience.
# `ulimit -s` is in KiB, so this asks for the 16 MiB measured to be
# enough, not 64. If the host's hard limit forbids it, say so and keep
# going: the other four fixtures are still worth having, and silently
# dropping to 8 MiB would abort the Rust arm mid-run and take the Go
# benchmarks below down with it under `set -e`.
(
  if ! ulimit -s 16384 2>/dev/null; then
    echo "WARNING: could not raise the stack limit to 16 MiB (hard limit" \
         "$(ulimit -Hs) KiB); skipping numbers-1mb.json, which needs it." >&2
    RUST_FIXTURES="records-1mb.json records-escaped-1mb.json records-16kb.json records-cjk-1mb.json"
  else
    RUST_FIXTURES="records-1mb.json records-escaped-1mb.json numbers-1mb.json records-16kb.json records-cjk-1mb.json"
  fi
  # Pathologies, on whichever branch above applied. No jsonic row: the
  # relaxed grammar is not ported to Rust yet.
  RUST_FIXTURES="$RUST_FIXTURES wide-40k.json longstring-1mb.json"
  RUST_FIXTURES="$RUST_FIXTURES numeric-edge-1mb.json separators-1mb.json tiny.json"
  for f in $RUST_FIXTURES; do
    "$RUST_BENCH" "$FIX/$f" "$RUST_ITERS" "$RUST_WARMUP"
  done
)

echo
echo "=== Go benchmarks ==="
GOWORK_DIR="$(mktemp -d)"
trap 'rm -rf "$GOWORK_DIR"' EXIT
( cd "$GOWORK_DIR" && go work init \
    "$DIR/gobench" "$PARSER_ROOT/go" "$ROOT/json/go" "$ROOT/jsonic/go" >/dev/null )
( cd "$DIR/gobench" && \
  GOWORK="$GOWORK_DIR/go.work" BENCH_FIXTURE_DIR="$FIX" \
  go test -run='^$' -bench=. -benchmem -benchtime="$BENCHTIME" -count=1 )
