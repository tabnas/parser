#!/usr/bin/env bash
# run-gate.sh — the engine conformance gate: run this repo's suites AND
# the downstream json + jsonic suites, in both runtimes, against the
# working-tree engine. An engine change that breaks a downstream grammar
# fails here instead of after merge.
#
# Layout assumption (the org-wide sibling-checkout convention, which the
# shared CI workflow also follows): the sibling repos are checked out next
# to this one:
#   <root>/parser   (this repo)
#   <root>/json
#   <root>/jsonic
# and, when present, wired and built too (CI clones all three):
#   <root>/debug    (jsonic's api/custom/debug tests require it)
#   <root>/bnf      (abnf needs it)
#   <root>/abnf     (this repo's README doc-examples require it)
#
# Usage: ci/gate/run-gate.sh [root-dir]
#   default root: the parent of this repo.
#
# TS wiring: each downstream ts/ gets node_modules/@tabnas/* symlinked
# to the sibling checkouts (idempotent; matches the file:../../parser/ts
# dependency the repos use in CI).
# Go wiring: a throwaway go.work (in a temp dir, via GOWORK) points every
# module at the sibling checkouts — no repo files are modified.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"
ROOT="${1:-$(cd "$PARSER_ROOT/.." && pwd)}"

step() { printf '\n=== %s ===\n' "$*"; }

# link_ts_dep lives in ci/lib/wire.sh, shared with run-bench.sh so the
# two wirings cannot drift apart.
. "$DIR/../lib/wire.sh"

# --- TS wiring ---
link_ts_dep "$ROOT/json/ts" parser "$PARSER_ROOT/ts"
link_ts_dep "$ROOT/jsonic/ts" parser "$PARSER_ROOT/ts"
link_ts_dep "$ROOT/jsonic/ts" json "$ROOT/json/ts"
# jsonic's debug/api/custom tests require @tabnas/debug; wire it when
# the sibling exists (CI clones it — see .github/workflows/gate.yml).
# Without it those three test files fail with MODULE_NOT_FOUND.
if [ -d "$ROOT/debug/ts" ]; then
  link_ts_dep "$ROOT/debug/ts" parser "$PARSER_ROOT/ts"
  link_ts_dep "$ROOT/jsonic/ts" debug "$ROOT/debug/ts"
fi
# This repo's README doc-examples require @tabnas/abnf, which
# ts/test/doc-examples.test.js resolves from the sibling <root>/abnf/ts,
# and abnf requires @tabnas/bnf. Each is wired to the engine under test,
# so the examples cannot load a second, published engine through them.
if [ -d "$ROOT/bnf/ts" ]; then
  link_ts_dep "$ROOT/bnf/ts" parser "$PARSER_ROOT/ts"
fi
if [ -d "$ROOT/abnf/ts" ]; then
  link_ts_dep "$ROOT/abnf/ts" parser "$PARSER_ROOT/ts"
  link_ts_dep "$ROOT/abnf/ts" bnf "$ROOT/bnf/ts"
fi

# --- Go wiring: hermetic go.work over the sibling modules ---
GOWORK_DIR="$(mktemp -d)"
trap 'rm -rf "$GOWORK_DIR"' EXIT
( cd "$GOWORK_DIR" && go work init \
    "$PARSER_ROOT/go" "$ROOT/json/go" "$ROOT/jsonic/go" >/dev/null )
export GOWORK="$GOWORK_DIR/go.work"

fail=0
run() { # run <name> <dir> <cmd...>
  local name="$1" dir="$2"
  shift 2
  step "$name"
  if ( cd "$dir" && "$@" ); then
    echo "PASS: $name"
  else
    echo "FAIL: $name"
    fail=1
  fi
}

step "build TS (engine first, then downstreams)"
( cd "$PARSER_ROOT/ts" && npx tsc --build src && npx tsc --build test )
# A linked sibling is its WORKING TREE, which has no dist/ until built:
# the same order as ci.yml's build-order (parser bnf debug abnf).
for sib in bnf debug abnf; do
  if [ -d "$ROOT/$sib/ts" ]; then ( cd "$ROOT/$sib/ts" && npx tsc --build src ); fi
done
( cd "$ROOT/json/ts" && npx tsc --build src )
( cd "$ROOT/jsonic/ts" && npx tsc --build src )

run "parser/ts test" "$PARSER_ROOT/ts" npm test --silent
run "parser/go test" "$PARSER_ROOT/go" go test ./...
run "json/ts test" "$ROOT/json/ts" npm test --silent
run "json/go test" "$ROOT/json/go" go test ./...
run "jsonic/ts test" "$ROOT/jsonic/ts" npm test --silent
run "jsonic/go test" "$ROOT/jsonic/go" go test ./...

step "fixture corpus sync"
if "$DIR/fixture-sync.sh" "$ROOT/jsonic"; then
  echo "PASS: fixture-sync"
else
  echo "FAIL: fixture-sync"
  fail=1
fi

step "gate result"
if [ "$fail" = 0 ]; then echo "GATE PASS"; else echo "GATE FAIL"; fi
exit $fail
