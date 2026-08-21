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

# The null must be measured the way the comparisons are, or it bounds
# the wrong thing. abba.js loads each slot with require(), which caches
# by resolved path: pass the same directory twice and BOTH slots get the
# SAME module object — verified, `require(p) === require(p)` — so the null
# would run one module graph where forward and reverse run two. Every
# artifact that exists only because there ARE two graphs (separate inline
# caches, separate JIT tier-up histories, load order, code-cache pressure)
# would then be absent from the null, making the band too narrow and a
# false EFFECT ESTABLISHED easier to reach. abba.js's own header claims
# two graphs; this is what makes that true for all three runs.
#
# A COPY, never a symlink: require() keys on the real path, so a symlink
# resolves straight back to one graph. It lives under the baseline tree so
# bare specifiers still resolve through that tree's node_modules.
NULLDIR="$BASEDIR/.abba-null"
rm -rf "$NULLDIR"
mkdir -p "$NULLDIR"
cp -R "$BASEDIR/dist" "$NULLDIR/dist"
cp -R "$BASEDIR/dist-test" "$NULLDIR/dist-test"
trap 'rm -rf "$NULLDIR"; [ -n "$CLEANUP" ] && rm -rf "$CLEANUP"; [ -n "$CLEANUP" ] && git -C "$PARSER_ROOT" worktree prune || true' EXIT

FWD="$(run "$BASEDIR" "$CAND" forward)"
REV="$(run "$CAND" "$BASEDIR" reverse)"
NUL="$(run "$BASEDIR" "$NULLDIR" null)"

printf '%s\n%s\n%s\n' "$FWD" "$REV" "$NUL" | python3 -c '
import json, sys, math
rows = [json.loads(l) for l in sys.stdin if l.strip()]
by = {r["label"]: r for r in rows}
f, r, n = by["forward"], by["reverse"], by["null"]

for k in ("forward", "reverse", "null"):
    d = by[k]
    print("  %-8s d_min=%+7.2f%%  d_total=%+7.2f%%  B wins %2d/%d"
          % (k, d["d_min_median"], d["d_total_median"], d["b_wins"], d["rounds"]))

# Both metrics get a verdict. Deciding from d_min alone was wrong in the
# one case the rig most needs to get right: min-of-inner EXCLUDES GC
# pauses by construction, so a change that moves ALLOCATION can be
# invisible in d_min and plain in d_total -- or, worse, can collect an
# EFFECT ESTABLISHED from d_min noise while d_total says nothing.
def verdict(key):
    fd, rd = f[key], r[key]
    nd = abs(n[key])
    flip = (fd > 0) != (rd > 0)
    geo = math.copysign(math.sqrt(abs(fd) * abs(rd)), fd)
    band = max(nd, 1.0)
    if not flip:
        return ("UNRESOLVED", geo, nd, band,
                "no sign flip (%+.2f%% then %+.2f%%)" % (fd, rd))
    if abs(geo) <= band:
        why = ("this session null of %.2f%%" % nd) if nd >= 1.0 else (
            "the %.2f%% floor (session null was %.2f%%)" % (band, nd))
        return ("UNRESOLVED", geo, nd, band,
                "signs flip but the estimate (%+.2f%%) does not clear %s" % (geo, why))
    return ("ESTABLISHED", geo, nd, band,
            "sign reverses with the slots (%+.2f%% then %+.2f%%), clearing a "
            "null of %.2f%%" % (fd, rd, nd))

vmin = verdict("d_min_median")
vtot = verdict("d_total_median")
print()
for name, note, v in (("d_min  ", "excludes GC pauses", vmin),
                      ("d_total", "includes GC pauses", vtot)):
    state, geo, nd, band, why = v
    if state == "ESTABLISHED":
        print("  %s (%s): EFFECT ESTABLISHED - candidate is %s, %+.2f%%"
              % (name, note, "FASTER" if geo < 0 else "SLOWER", geo))
    else:
        print("  %s (%s): UNRESOLVED" % (name, note))
    print("           %s." % why)

print()
if vmin[0] == vtot[0] == "ESTABLISHED" and (vmin[1] > 0) == (vtot[1] > 0):
    print("  Both metrics agree. Report the pair, not one of them.")
elif vmin[0] == vtot[0] == "UNRESOLVED":
    print("  Neither metric resolves. Do not report either number as a")
    print("  result -- not the point estimates, not the direction.")
else:
    print("  THE TWO METRICS DISAGREE, so the rig has not decided. The usual")
    print("  cause is an ALLOCATION change: d_total sees the collection")
    print("  pauses and d_min cannot. Say which metric moved and which did")
    print("  not; do not quote the one that resolved as the result.")
'
