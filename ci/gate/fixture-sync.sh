#!/usr/bin/env bash
# fixture-sync.sh — verify the fixtures this repo SHARES with jsonic have
# not drifted.
#
# The two repos no longer hold mirrored corpora. This repo owns the
# engine's own surface (strict-JSON, utility, lexer-option fixtures);
# jsonic owns the relaxed-grammar corpus and runs it. Only the files
# present in BOTH are required to match, and those are compared
# byte-for-byte. A file that exists on one side only is that side's to
# own and is reported as INFO, not a failure.
#
# (This script previously required the two corpora to be byte-identical
# copies in both directions. It also pointed at jsonic's old
# ts/test/spec path, so once jsonic moved its fixtures to the repo root
# the script exited 1 on every run and stopped detecting anything —
# which is how the shared include-json.tsv came to differ by five rows
# without anyone noticing.)
#
# One naming convention difference is preserved: files named tabnas-*.tsv
# here are named jsonic-*.tsv there.
#
# Usage: ci/gate/fixture-sync.sh [path-to-jsonic-repo]
#   default jsonic location: ../jsonic relative to this repo's root.
# Exit code: 0 = shared fixtures agree, 1 = drift.
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"
JSONIC_ROOT="${1:-$PARSER_ROOT/../jsonic}"

A="$PARSER_ROOT/test/spec"
# jsonic keeps its fixtures at the repo root so both its runtimes sit
# below them; older checkouts had them under ts/test/spec.
B="$JSONIC_ROOT/test/spec"
if [ ! -d "$B" ] && [ -d "$JSONIC_ROOT/ts/test/spec" ]; then
  B="$JSONIC_ROOT/ts/test/spec"
fi
ALLOW="$DIR/fixture-sync-allow.txt"

if [ ! -d "$B" ]; then
  echo "fixture-sync: jsonic spec dir not found: $B" >&2
  exit 1
fi

allowed() {
  grep -qxF "$1" <(grep -v '^#' "$ALLOW" 2>/dev/null || true)
}

# Map a parser-side fixture name to its jsonic-side name.
map_name() {
  case "$1" in
    tabnas-*) echo "jsonic-${1#tabnas-}" ;;
    *) echo "$1" ;;
  esac
}

fail=0
checked=0
only_here=0

for f in "$A"/*.tsv; do
  base="$(basename "$f")"
  mapped="$(map_name "$base")"
  if [ ! -f "$B/$mapped" ]; then
    # Owned by this repo alone. The allowlist predates the split and is
    # kept only so an entry there stays quiet rather than noisy.
    only_here=$((only_here + 1))
    if ! allowed "$base"; then
      echo "INFO (parser-owned, not in jsonic): $base"
    fi
    continue
  fi
  if ! cmp -s "$f" "$B/$mapped"; then
    fail=1
    echo "FAIL (content drift): $base != jsonic:$mapped"
    diff -u "$f" "$B/$mapped" | head -10
  else
    checked=$((checked + 1))
  fi
done

# The reverse direction is deliberately NOT checked: jsonic owns the
# relaxed-grammar corpus, and requiring this repo to carry a copy of it
# is what produced two sets of the same files, one of them dead.

echo "fixture-sync: $checked shared and identical, $only_here parser-owned, fail=$fail"
exit $fail
