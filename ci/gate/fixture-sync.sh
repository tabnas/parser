#!/usr/bin/env bash
# fixture-sync.sh — verify the shared TSV fixture corpus has not drifted
# between this repo (test/spec/) and jsonic (ts/test/spec/).
#
# The two corpora are meant to be byte-identical copies, with one naming
# convention difference: files named tabnas-*.tsv here are named
# jsonic-*.tsv in the jsonic repo. Files listed in
# fixture-sync-allow.txt are known, deliberate divergences (see the
# comments in that file) and are reported as SKIP instead of FAIL.
#
# Usage: ci/gate/fixture-sync.sh [path-to-jsonic-repo]
#   default jsonic location: ../jsonic relative to this repo's root.
# Exit code: 0 = in sync (allowlisted divergences only), 1 = drift.
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"
JSONIC_ROOT="${1:-$PARSER_ROOT/../jsonic}"

A="$PARSER_ROOT/test/spec"
B="$JSONIC_ROOT/ts/test/spec"
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
skipped=0

for f in "$A"/*.tsv; do
  base="$(basename "$f")"
  mapped="$(map_name "$base")"
  if [ ! -f "$B/$mapped" ]; then
    if allowed "$base"; then
      skipped=$((skipped + 1))
      echo "SKIP (allowlisted, missing in jsonic): $base"
    else
      fail=1
      echo "FAIL (missing in jsonic): $base (expected $mapped)"
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

# Reverse direction: jsonic fixtures that do not exist here.
for f in "$B"/*.tsv; do
  base="$(basename "$f")"
  case "$base" in
    jsonic-*) unmapped="tabnas-${base#jsonic-}" ;;
    *) unmapped="$base" ;;
  esac
  if [ ! -f "$A/$unmapped" ]; then
    if allowed "$base"; then
      skipped=$((skipped + 1))
      echo "SKIP (allowlisted, missing in parser): $base"
    else
      fail=1
      echo "FAIL (missing in parser): jsonic:$base (expected $unmapped)"
    fi
  fi
done

echo "fixture-sync: $checked identical, $skipped allowlisted, fail=$fail"
exit $fail
