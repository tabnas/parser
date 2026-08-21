#!/usr/bin/env bash
# ab-compare.sh — decide whether an engine change is a real effect.
#
# Runs the paired A/B/B/A rig THREE times and reads the pattern:
#
#   forward   baseline in slot A, candidate in slot B
#   reverse   the same two builds, slots swapped
#   null      baseline against ITSELF, same fixture, same session
#
# A real effect REVERSES SIGN between forward and reverse. A slot artifact
# does not — and there is always a slot artifact: on byte-identical builds
# whichever tree sits in slot B tends to lose, by up to a couple of percent.
# The null run measures that bias adjacent to the comparison rather than
# from memory, because it moves between machines and between sessions.
#
# Usage:
#   ci/bench/ab-compare.sh [--base <git-ref|ts-dir>] [--fixture <name>]
#                          [--rounds N] [--inner N]
#
#   --base     ref to build as the baseline, or a path to an already-built
#              ts/ tree. Default: HEAD (i.e. compare the working tree's
#              build against the last commit's).
#   --fixture  a name in ci/bench/fixtures. Default records-16kb.json.
#
# The candidate is always this checkout's ts/ — build it first.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"

BASE="HEAD"
FIXTURE="records-16kb.json"
ROUNDS=12
INNER=3
while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="$2"; shift 2 ;;
    --fixture) FIXTURE="$2"; shift 2 ;;
    --rounds) ROUNDS="$2"; shift 2 ;;
    --inner) INNER="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

FIX="$DIR/fixtures/$FIXTURE"
[ -f "$FIX" ] || node "$DIR/genfixture.js" "$DIR/fixtures" >/dev/null
[ -f "$FIX" ] || { echo "no such fixture: $FIXTURE" >&2; exit 2; }

CAND="$PARSER_ROOT/ts"
[ -f "$CAND/dist/tabnas.js" ] || { echo "build the working tree first: (cd ts && npm run build)" >&2; exit 2; }

CLEANUP=""
trap '[ -n "$CLEANUP" ] && rm -rf "$CLEANUP"; [ -n "$CLEANUP" ] && git -C "$PARSER_ROOT" worktree prune || true' EXIT

if [ -d "$BASE/dist" ]; then
  BASEDIR="$BASE"
  echo "baseline: prebuilt tree $BASEDIR"
else
  WT="$(mktemp -d)"
  CLEANUP="$WT"
  echo "baseline: building $BASE in a throwaway worktree (this takes a minute)"
  git -C "$PARSER_ROOT" worktree add --detach "$WT" "$BASE" >/dev/null 2>&1
  ( cd "$WT/ts" && npm i --silent >/dev/null 2>&1 && npm run build >/dev/null 2>&1 )
  BASEDIR="$WT/ts"
fi

echo "candidate: $CAND"
echo "fixture:   $FIXTURE   rounds: $ROUNDS   inner: $INNER"
echo

run() { node "$DIR/abba.js" --a "$1" --b "$2" --fixture "$FIX" \
          --rounds "$ROUNDS" --inner "$INNER" --label "$3"; }

FWD="$(run "$BASEDIR" "$CAND" forward)"
REV="$(run "$CAND" "$BASEDIR" reverse)"
NUL="$(run "$BASEDIR" "$BASEDIR" null)"

printf '%s\n%s\n%s\n' "$FWD" "$REV" "$NUL" | python3 -c '
import json, sys, math
rows = [json.loads(l) for l in sys.stdin if l.strip()]
by = {r["label"]: r for r in rows}
f, r, n = by["forward"], by["reverse"], by["null"]

for k in ("forward", "reverse", "null"):
    d = by[k]
    dmin, dtot = d["d_min_median"], d["d_total_median"]
    wins, rnds = d["b_wins"], d["rounds"]
    print("  %-8s d_min=%+7.2f%%  d_total=%+7.2f%%  B wins %2d/%d"
          % (k, dmin, dtot, wins, rnds))

fd, rd, nd = f["d_min_median"], r["d_min_median"], abs(n["d_min_median"])
flip = (fd > 0) != (rd > 0)
geo = math.copysign(math.sqrt(abs(fd) * abs(rd)), fd)
band = max(nd, 1.0)
print()
if not flip:
    print("  VERDICT: UNRESOLVED - no sign flip (%+.2f%% then %+.2f%%)." % (fd, rd))
    print("           Consistent with a slot artifact, or an effect too small")
    print("           to see here. Do not report either number as a result.")
elif abs(geo) <= band:
    why = ("this session null of %.2f%%" % nd) if nd >= 1.0 else (
        "the %.2f%% floor (session null was %.2f%%)" % (band, nd))
    print("  VERDICT: UNRESOLVED - signs flip but the estimate (%+.2f%%)" % geo)
    print("           does not clear %s." % why)
else:
    print("  VERDICT: EFFECT ESTABLISHED - the candidate is %s."
          % ("FASTER" if geo < 0 else "SLOWER"))
    print("           Sign reverses with the slots (%+.2f%% then %+.2f%%)," % (fd, rd))
    print("           clearing a null of %.2f%%. Point estimate %+.2f%%" % (nd, geo))
    print("           (geometric mean of the two directions).")
print()
print("  d_min excludes GC pauses by construction; if the change moves")
print("  ALLOCATION, read d_total, which does not.")
'
