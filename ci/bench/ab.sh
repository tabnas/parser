#!/usr/bin/env bash
# Interleaved A/B: alternate the two binaries round by round so any drift
# in the machine lands on both arms equally.
#
# A single A-then-B pass cannot tell a change from the machine getting
# busier between the two runs, which on a shared host it reliably does.
# Alternating spreads that drift across both arms instead of loading it
# onto whichever ran second.
#
# Usage: ci/bench/ab.sh <binary-a> <binary-b> [rounds]
set -euo pipefail

A=${1:?usage: ab.sh <binary-a> <binary-b> [rounds]}
B=${2:?usage: ab.sh <binary-a> <binary-b> [rounds]}
N=${3:-6}

i=1
while [ "$i" -le "$N" ]; do
  "$A" | sed "s/^/A r$i /"
  "$B" | sed "s/^/B r$i /"
  i=$((i + 1))
done
