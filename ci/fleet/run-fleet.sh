#!/usr/bin/env bash
# run-fleet.sh — the fleet regression gate.
#
# Check out every published tabnas grammar, compiler and plugin at its
# LATEST released version, and run that repo's own test suites against the
# WORKING-TREE engine, in both runtimes. An engine change that breaks any
# downstream grammar fails here, before the engine is published.
#
# This is the gap ci/gate/run-gate.sh leaves. That gate runs json and jsonic
# from whatever sibling checkouts happen to be on the machine — two of
# thirty packages, at whatever revision the operator has. This one covers
# the fleet, and pins each package to what users are actually installing.
#
# It is not part of `make test`, deliberately: it clones thirty repositories
# and runs two toolchains over each, which is minutes, not seconds, and it
# reaches the network. Run it on demand, and in CI.
#
#   ci/fleet/run-fleet.sh                       # everything, both runtimes
#   ci/fleet/run-fleet.sh --only expr,jsonic    # just these
#   ci/fleet/run-fleet.sh --runtime ts          # one runtime
#   ci/fleet/run-fleet.sh --offline             # reuse .work, no network
#   ci/fleet/run-fleet.sh --update-lock         # rewrite fleet.lock
#   ci/fleet/run-fleet.sh --record-timings      # append to timings.tsv
#
# Every run TIMES each suite and prints the durations. Only --record-timings
# writes them down, because a timing record is a deliberate measurement, not
# a side effect of pushing: an ordinary run leaves timings.tsv alone, so the
# file stays a series of runs somebody meant to compare rather than noise
# from every branch that happened to run the gate.
#
# THE GATE ONLY MEANS SOMETHING IF IT RAN. Every way this script can produce
# a green result without having tested the working-tree engine is treated as
# a failure, not a skip: a missing checkout, a failed install, a failed
# build, an engine that resolves anywhere but here. A harness that passes
# having run nothing is worse than no harness.
#
# Exit status is the gate: 0 only when every suite that was expected to pass
# did, AND nothing in expect-fail.txt passed or failed differently than it
# said it would.
set -uo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"

WORK="$DIR/.work"
ONLY=""
RUNTIME="both"
OFFLINE=0
UPDATE_LOCK=0
RECORD_TIMINGS=0

while [ $# -gt 0 ]; do
  case "$1" in
    --only) ONLY="$2"; shift 2 ;;
    --runtime) RUNTIME="$2"; shift 2 ;;
    --work) WORK="$2"; shift 2 ;;
    --offline) OFFLINE=1; shift ;;
    --update-lock) UPDATE_LOCK=1; shift ;;
    --record-timings) RECORD_TIMINGS=1; shift ;;
    -h|--help) sed -n '2,32p' "$0"; exit 0 ;;
    *) echo "run-fleet: unknown option $1" >&2; exit 2 ;;
  esac
done

case "$RUNTIME" in ts|go|both) ;; *) echo "run-fleet: --runtime must be ts, go or both" >&2; exit 2 ;; esac

step() { printf '\n=== %s ===\n' "$*"; }
note() { printf '  %s\n' "$*"; }

# Per-suite output lives here so a CI failure can be read. Discarding it
# leaves a workflow log with a FAIL row and no diagnostic, on an ephemeral
# checkout nobody can inspect afterwards.
LOGS="$WORK/.logs"

# link_ts_dep lives in ci/lib/wire.sh, shared with run-gate.sh and
# run-bench.sh so the three wirings cannot drift apart. Wiring the wrong
# engine is the one failure that makes this whole harness lie.
. "$DIR/../lib/wire.sh"

fail=0
results=()
record() { results+=("$(printf '%-22s %s' "$1" "$2")"); }

# Durations, keyed by <package>/<runtime>, in milliseconds.
declare -A TIMING=()
TIMED_ORDER=()

# node rather than `date +%s%N` (not portable to the macOS runners) or bash 5's
# EPOCHREALTIME (whose decimal separator follows the locale). node is already
# required by the preflight, and one spawn either side of a suite that runs
# for seconds is not a measurement error worth caring about.
now_ms() { node -e 'process.stdout.write(String(Date.now()))'; }

# Milliseconds as seconds, to one decimal: the numbers being compared are
# whole seconds apart and more precision would suggest a resolution this has
# no claim to.
secs() { node -e 'process.stdout.write((Number(process.argv[1])/1000).toFixed(1))' "$1"; }

# --- preflight ---------------------------------------------------------
# Fail on a missing toolchain HERE, with a sentence, rather than thirty
# rows of identical "command not found" further down.
for tool in node npm git; do
  command -v "$tool" >/dev/null 2>&1 || { echo "run-fleet: $tool is required" >&2; exit 2; }
done
if [ "$RUNTIME" != "ts" ]; then
  command -v go >/dev/null 2>&1 || { echo "run-fleet: go is required for --runtime go/both" >&2; exit 2; }
fi

# --- the roster --------------------------------------------------------
# fleet.json says WHICH packages are in the fleet and nothing about how they
# depend on each other: that lives in each package's own ts/package.json,
# and graph.mjs reads it from the checkouts. A second copy here would be one
# more thing to keep in step, and the first full run proved a hand-kept one
# wrong — a single `base` per package built ini before hoover and c before
# expr, and both failed with "Cannot find module".
roster() {
  node -e '
    const m = require(process.argv[1])
    const only = process.argv[2] ? new Set(process.argv[2].split(",").map(s => s.trim())) : null
    const all = new Map(m.packages.map((p) => [p.name, p]))
    for (const want of only ?? []) {
      if (!all.has(want)) {
        console.error(`run-fleet: --only names ${want}, which is not in fleet.json`)
        process.exit(2)
      }
    }
    for (const p of m.packages) {
      if (only && !only.has(p.name)) continue
      console.log([p.name, p.suites ? "1" : "0"].join("\t"))
    }
  ' "$DIR/fleet.json" "$ONLY"
}

ROSTER="$(roster)" || exit 2
ASKED=()
declare -A SUITES=()
while IFS=$'\t' read -r name suites; do
  [ -n "$name" ] || continue
  ASKED+=("$name")
  SUITES["$name"]="$suites"
done <<< "$ROSTER"

# NAMES is filled in after checkout, once the real graph can be read. Until
# then only the asked-for set is known.
NAMES=("${ASKED[@]}")

step "fleet: ${#ASKED[@]} package(s) asked for"
note "${ASKED[*]}"

# --- expected failures -------------------------------------------------
# A known-broken package is recorded HERE and is still RUN. The format is
#
#     <package>/<runtime> :: <signature> :: <reason>
#
# and all three fields are required. <signature> is a substring that must
# appear in the suite's output for the exemption to apply, because "this
# package fails" is not a claim worth writing down: an entry that accepts
# ANY non-zero exit turns off regression detection for that package
# entirely, so a NEW break behind a known one would ride in unnoticed.
# Three rules, the first two taken from ci/gate/fixture-sync-allow.txt,
# whose design problem was the same one:
#   - an entry with no reason is rejected, so nobody can quiet a package
#     by adding a bare name;
#   - an entry that PASSES fails the gate, so an exemption cannot outlive
#     the breakage it was written for;
#   - an entry that fails WITHOUT its signature fails the gate, so it
#     covers the one breakage it names and nothing else.
declare -A XFAIL_SIG=()
declare -A XFAIL_WHY=()
EXPECT_FILE="$DIR/expect-fail.txt"
if [ -f "$EXPECT_FILE" ]; then
  while IFS= read -r line; do
    line="$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    # A comment is a `#` that STARTS the line. Stripping from the first `#`
    # anywhere would eat the `#` in a signature, and the most useful
    # signatures are test-runner output like `# fail 16`.
    case "$line" in ''|'#'*) continue ;; esac
    key="$(printf '%s' "$line" | awk -F' *:: *' '{print $1}')"
    sig="$(printf '%s' "$line" | awk -F' *:: *' '{print $2}')"
    why="$(printf '%s' "$line" | awk -F' *:: *' '{print $3}')"
    if [ -z "$key" ] || [ -z "$sig" ] || [ -z "$why" ]; then
      echo "run-fleet: expect-fail.txt: '$line'" >&2
      echo "run-fleet: needs '<package>/<runtime> :: <signature> :: <reason>' — refusing to run" >&2
      exit 2
    fi
    XFAIL_SIG["$key"]="$sig"
    XFAIL_WHY["$key"]="$why"
  done < "$EXPECT_FILE"
fi

# --- versions and checkout ---------------------------------------------
# "Latest" is what the registry serves as the `latest` dist-tag, which is
# what a user gets from `npm i @tabnas/<name>`. The git tag `ts/vX.Y.Z` is
# the commit that produced it (see each repo's `repo-tag` script), so it is
# the tree whose tests describe that release.
#
# Go is versioned SEPARATELY, as `go/vX.Y.Z` on the Go module proxy. The two
# usually move together and usually match, but nothing enforces that, and
# running the Go suites off the npm release's tree would silently test the
# wrong source the first time they diverge. So both are resolved, and the Go
# arm gets its own checkout whenever they differ.
declare -A VERSION=()
declare -A GOVERSION=()
declare -A LOCKED=()
declare -A DONE=()
LOCK="$DIR/fleet.lock"
if [ -f "$LOCK" ]; then
  while IFS=' ' read -r n v; do
    [ -n "${n:-}" ] || continue
    case "$n" in \#*) continue ;; esac
    LOCKED["$n"]="$v"
  done < "$LOCK"
fi

# The Go tree for a package: its own checkout when the Go release differs
# from the npm one, otherwise the same tree.
godir() {
  local name="$1"
  if [ -n "${GOVERSION[$name]:-}" ] && [ "${GOVERSION[$name]}" != "${VERSION[$name]:-}" ]; then
    printf '%s/.go/%s/go' "$WORK" "$name"
  else
    printf '%s/%s/go' "$WORK" "$name"
  fi
}

resolve_version() { # resolve_version <name>
  local name="$1" v gv was suffix
  [ -z "${VERSION[$name]:-}" ] || return 0
  v="$(npm view "@tabnas/$name" version 2>/dev/null | tail -1)"
  if [ -z "$v" ]; then
    echo "run-fleet: cannot resolve @tabnas/$name from the registry" >&2
    exit 2
  fi
  VERSION["$name"]="$v"

  gv=""
  if [ "$RUNTIME" != "ts" ]; then
    gv="$(go list -m -versions "github.com/tabnas/$name/go" 2>/dev/null | awk '{print $NF}')"
    gv="${gv#v}"
    GOVERSION["$name"]="$gv"
  fi

  was="${LOCKED[$name]:-}"
  suffix=""
  [ -n "$gv" ] && [ "$gv" != "$v" ] && suffix="  go $gv (SEPARATE CHECKOUT)"
  if [ -z "$was" ]; then
    note "$(printf '%-14s %-10s (new — not in fleet.lock)%s' "$name" "$v" "$suffix")"
  elif [ "$was" != "$v" ]; then
    note "$(printf '%-14s %-10s <- %s  UPDATED%s' "$name" "$v" "$was" "$suffix")"
  else
    note "$(printf '%-14s %-10s%s' "$name" "$v" "$suffix")"
  fi
}

# `git checkout --force` does not remove untracked files, and a previous
# release's build output left in place can let a suite pass without the
# current source ever compiling. Clean it.
checkout_at() { # checkout_at <repo-dir> <slug> <tag> <label>
  local repo="$1" slug="$2" tag="$3" label="$4"
  if [ ! -d "$repo/.git" ]; then
    rm -rf "$repo"
    git clone --quiet --filter=blob:none --no-checkout \
      "https://github.com/tabnas/$slug.git" "$repo" || {
        echo "run-fleet: clone failed for $slug" >&2; return 1; }
  fi
  if git -C "$repo" fetch --quiet --depth 1 origin "refs/tags/$tag:refs/tags/$tag" 2>/dev/null &&
     git -C "$repo" checkout --quiet --force "$tag" 2>/dev/null; then
    git -C "$repo" clean -qfdx -e node_modules
    note "$(printf '%-14s %s%s' "$slug" "$tag" "$label")"
  else
    # A missing release tag is REPORTED, never silent: the run still has to
    # happen, but "we tested the release" and "we tested the default
    # branch" are different claims and the log has to say which.
    local head
    head="$(git -C "$repo" remote show origin 2>/dev/null | sed -n 's/.*HEAD branch: //p')"
    head="${head:-main}"
    git -C "$repo" fetch --quiet --depth 1 origin "$head" &&
      git -C "$repo" checkout --quiet --force FETCH_HEAD || {
        echo "run-fleet: cannot check out $slug" >&2; return 1; }
    git -C "$repo" clean -qfdx -e node_modules
    note "$(printf '%-14s %s  (NO TAG %s — used %s)%s' \
      "$slug" "$(git -C "$repo" rev-parse --short HEAD)" "$tag" "$head" "$label")"
  fi
}

fetch_one() { # fetch_one <name>
  local name="$1" gv
  [ -z "${DONE[$name]:-}" ] || return 0
  resolve_version "$name"
  checkout_at "$WORK/$name" "$name" "ts/v${VERSION[$name]}" "" || exit 2
  gv="${GOVERSION[$name]:-}"
  if [ -n "$gv" ] && [ "$gv" != "${VERSION[$name]}" ]; then
    mkdir -p "$WORK/.go"
    checkout_at "$WORK/.go/$name" "$name" "go/v$gv" "  [go]" || exit 2
  fi
  DONE["$name"]=1
}

mkdir -p "$WORK" "$LOGS"

if [ "$OFFLINE" = 1 ]; then
  step "versions: --offline, reusing $WORK as it stands"
  mapfile -t NAMES < <(node "$DIR/graph.mjs" close "$WORK" "${ASKED[@]}")
else
  # A package's dependencies are only readable once it is checked out, so
  # this is a fixpoint rather than one pass: fetch what is selected, ask the
  # graph what that needs, fetch the difference, repeat. Naming `ini` has to
  # bring in `hoover`, and nothing knows that until ini's package.json is on
  # disk.
  step "resolve and check out"
  NAMES=("${ASKED[@]}")
  for _round in 1 2 3 4 5; do
    for name in "${NAMES[@]}"; do fetch_one "$name"; done
    mapfile -t CLOSURE < <(node "$DIR/graph.mjs" close "$WORK" "${NAMES[@]}")
    [ "${#CLOSURE[@]}" -eq "${#NAMES[@]}" ] && break
    NAMES=("${CLOSURE[@]}")
  done
  NAMES=("${CLOSURE[@]:-${NAMES[@]}}")
fi

# Build order from the real graph: every package after everything it needs.
mapfile -t NAMES < <(node "$DIR/graph.mjs" order "$WORK" "${NAMES[@]}") || exit 2

pulled=$(( ${#NAMES[@]} - ${#ASKED[@]} ))
if [ "$pulled" -gt 0 ]; then
  note "$pulled package(s) pulled in as dependencies"
fi
note "build order: ${NAMES[*]}"


# --- every selected checkout must actually be here ---------------------
# Without this, `--offline` against an empty or half-populated work
# directory records every suite as "skipped, no runtime in this repo" and
# reports FLEET PASS having run nothing at all. A checkout that is missing
# is a broken run, not an absent runtime.
step "checkouts present"
missing=()
for name in "${NAMES[@]}"; do
  [ -d "$WORK/$name/.git" ] || missing+=("$name")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "run-fleet: no checkout for: ${missing[*]}" >&2
  echo "run-fleet: re-run without --offline to fetch them" >&2
  exit 2
fi
note "${#NAMES[@]} checkout(s)"

# --- build the engine FIRST --------------------------------------------
# Before anything resolves or imports @tabnas/parser. Its package.json
# points main and every export at dist/, which `npm i` does not create, so
# a clean checkout has no dist at all — and both the resolution probe below
# and every downstream build would fail against a tree that is merely
# unbuilt rather than wrong.
if [ "$RUNTIME" != "go" ]; then
  step "build the engine"
  ( cd "$PARSER_ROOT/ts" && npx tsc --build src test ) || {
    echo "run-fleet: the engine's own TS build failed — fix that first" >&2; exit 2; }
  [ -f "$PARSER_ROOT/ts/dist/tabnas.js" ] || {
    echo "run-fleet: the engine built but $PARSER_ROOT/ts/dist/tabnas.js is absent" >&2; exit 2; }
  note "$PARSER_ROOT/ts/dist"
fi

# --- install TS toolchains ---------------------------------------------
# An install failure FAILS the gate. Reusing .work after an earlier good
# run can leave enough node_modules behind that the build and the suite
# both pass against a stale dependency graph — green, and about nothing.
if [ "$RUNTIME" != "go" ]; then
  step "npm install"
  for name in "${NAMES[@]}"; do
    [ -d "$WORK/$name/ts" ] || { note "$name: no ts/ — skipped"; continue; }
    if ( cd "$WORK/$name/ts" && npm install --no-audit --no-fund --silent \
           >"$LOGS/$name.install.log" 2>&1 ); then
      note "$name: ok"
    else
      note "$name: INSTALL FAILED (see $LOGS/$name.install.log)"
      record "$name/ts" "INSTALL FAILED"
      SUITES["$name"]=0
      fail=1
    fi
  done

  # --- wire every in-fleet dependency to the checkout, and the engine to
  # the working tree. Without this the suites run against the PUBLISHED
  # engine and the gate silently tests nothing.
  step "wiring TS deps to this working tree"
  for name in "${NAMES[@]}"; do
    tsdir="$WORK/$name/ts"
    [ -d "$tsdir" ] || continue
    link_ts_dep "$tsdir" parser "$PARSER_ROOT/ts" || exit 2
    for dep in "${NAMES[@]}"; do
      [ "$dep" != "$name" ] || continue
      [ -d "$tsdir/node_modules/@tabnas/$dep" ] || continue
      [ -d "$WORK/$dep/ts" ] || continue
      link_ts_dep "$tsdir" "$dep" "$WORK/$dep/ts" || exit 2
    done
  done

  # link_ts_dep already proves each symlink resolves where it was pointed.
  # This proves the stronger thing: that node, running in that directory,
  # LOADS the engine from here. A nested node_modules or a package export
  # map can still shadow a correct symlink, and the result would be a green
  # run that says nothing about this working tree.
  for probe in "${NAMES[@]}"; do
    [ -d "$WORK/$probe/ts/node_modules" ] || continue
    resolved="$(cd "$WORK/$probe/ts" && node -p "require.resolve('@tabnas/parser')" 2>/dev/null)"
    case "$resolved" in
      "$PARSER_ROOT/ts"/*) ;;
      *)
        echo "run-fleet: $probe/ts loads the engine from '${resolved:-<nothing>}', not $PARSER_ROOT/ts" >&2
        echo "run-fleet: refusing to report a result that would not be about this working tree" >&2
        exit 2
        ;;
    esac
    break
  done

  note "engine: $PARSER_ROOT/ts"
fi

# --- Go wiring: a throwaway go.work over the engine and every module ---
if [ "$RUNTIME" != "ts" ]; then
  step "wiring Go modules to this working tree"
  GOWORK_DIR="$(mktemp -d)"
  trap 'rm -rf "$GOWORK_DIR"' EXIT
  GOMODS=("$PARSER_ROOT/go")
  for name in "${NAMES[@]}"; do
    d="$(godir "$name")"
    [ -d "$d" ] && GOMODS+=("$d")
  done
  ( cd "$GOWORK_DIR" && go work init "${GOMODS[@]}" >/dev/null ) || {
    echo "run-fleet: go work init failed" >&2; exit 2; }
  export GOWORK="$GOWORK_DIR/go.work"

  # Structural proof, the same one link_ts_dep makes for the TypeScript
  # side. Go has no symlink to inspect: if the work file did not take, the
  # modules resolve the engine from the module cache — the PUBLISHED
  # engine — and every suite below passes while testing nothing this
  # branch changed. Ask Go which directory it actually resolved.
  for probe in "${GOMODS[@]:1}"; do
    [ -f "$probe/go.mod" ] || continue
    resolved="$(cd "$probe" && go list -m -f '{{.Dir}}' github.com/tabnas/parser/go 2>/dev/null)"
    if [ "$resolved" != "$PARSER_ROOT/go" ]; then
      echo "run-fleet: $probe resolves the engine to '${resolved:-<nothing>}', not $PARSER_ROOT/go" >&2
      echo "run-fleet: refusing to report a result that would not be about this working tree" >&2
      exit 2
    fi
    break
  done

  note "${#GOMODS[@]} module(s), engine resolves to $PARSER_ROOT/go"
fi

# --- build the fleet ----------------------------------------------------
# A build failure FAILS the gate and takes that package's suites out of the
# run. Letting it through and hoping the test script rebuilds is how a
# package whose source no longer compiles against this engine reports PASS
# off output left by the previous release.
if [ "$RUNTIME" != "go" ]; then
  step "build the fleet, in base order"
  for name in "${NAMES[@]}"; do
    [ -d "$WORK/$name/ts/src" ] || continue
    [ "${SUITES[$name]:-0}" != "0" ] || [ -d "$WORK/$name/ts/node_modules" ] || continue
    if ( cd "$WORK/$name/ts" && npx tsc --build src >"$LOGS/$name.build.log" 2>&1 ); then
      note "$name: built"
    else
      note "$name: BUILD FAILED (see $LOGS/$name.build.log)"
      record "$name/ts" "BUILD FAILED"
      SUITES["$name"]=0
      fail=1
    fi
  done
fi

# --- run ----------------------------------------------------------------
run_suite() { # run_suite <name> <runtime> <dir> <cmd...>
  local name="$1" rt="$2" dir="$3"
  shift 3
  local label="$name/$rt"
  local log="$LOGS/$name.$rt.log"

  if [ ! -d "$dir" ]; then
    record "$label" "SKIP (no $rt/ in this repo)"
    return
  fi

  local status t0 t1
  t0="$(now_ms)"
  if ( cd "$dir" && "$@" >"$log" 2>&1 ); then status=pass; else status=fail; fi
  t1="$(now_ms)"
  TIMING["$label"]=$(( t1 - t0 ))
  TIMED_ORDER+=("$label")

  local sig="${XFAIL_SIG[$label]:-}"
  if [ -n "$sig" ]; then
    if [ "$status" != fail ]; then
      # An exemption that no longer describes reality hides the next real
      # break. Passing here is a failure of the FILE, and it says so.
      record "$label" "UNEXPECTED PASS — remove from expect-fail.txt"
      fail=1
    elif grep -qF -- "$sig" "$log"; then
      record "$label" "xfail (${XFAIL_WHY[$label]})"
    else
      # It failed, but not for the reason the exemption names. That is a
      # new break wearing an old exemption's coat.
      record "$label" "FAIL — not the expected failure ($sig absent)"
      fail=1
    fi
    return
  fi

  if [ "$status" = pass ]; then
    record "$label" "PASS"
  else
    record "$label" "FAIL (see $log)"
    fail=1
  fi
}

step "suites"
for name in "${NAMES[@]}"; do
  [ "${SUITES[$name]:-0}" = "1" ] || continue
  if [ "$RUNTIME" != "go" ]; then
    printf '  %s/ts ... ' "$name"
    run_suite "$name" ts "$WORK/$name/ts" npm test --silent
    printf '%s\n' "${results[-1]#* }"
  fi
  if [ "$RUNTIME" != "ts" ]; then
    printf '  %s/go ... ' "$name"
    run_suite "$name" go "$(godir "$name")" go test ./...
    printf '%s\n' "${results[-1]#* }"
  fi
done

# --- report -------------------------------------------------------------
step "fleet result"
printf '%s\n' "${results[@]}"

# --- timings ------------------------------------------------------------
# Printed on every run. Written down only when asked.
if [ ${#TIMED_ORDER[@]} -gt 0 ]; then
  step "timings"
  ts_ms=0
  go_ms=0
  for label in "${TIMED_ORDER[@]}"; do
    ms="${TIMING[$label]}"
    case "$label" in
      */ts) ts_ms=$(( ts_ms + ms )) ;;
      */go) go_ms=$(( go_ms + ms )) ;;
    esac
    printf '  %-22s %8ss\n' "$label" "$(secs "$ms")"
  done
  printf '  %-22s %8ss\n' "--- ts total" "$(secs "$ts_ms")"
  printf '  %-22s %8ss\n' "--- go total" "$(secs "$go_ms")"
  printf '  %-22s %8ss\n' "--- all suites" "$(secs $(( ts_ms + go_ms )))"

  # These are SUITE times only: clone, npm install and build are excluded,
  # because those are dominated by the network and by whatever npm already
  # had cached, and a number that moves with the weather is not one to
  # record. What is recorded is the part an engine change can actually
  # move.
  if [ "$RECORD_TIMINGS" = 1 ]; then
    TIMINGS="$DIR/timings.tsv"
    run_id="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    engine="$(git -C "$PARSER_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
    host="$(uname -s | tr 'A-Z' 'a-z')-$(uname -m)"
    nodev="$(node --version 2>/dev/null)"
    gov="$(go version 2>/dev/null | awk '{print $3}')"
    if [ ! -f "$TIMINGS" ]; then
      {
        echo "# Fleet suite timings, one row per suite per recorded run."
        echo "#"
        echo "# Written ONLY by \`ci/fleet/run-fleet.sh --record-timings\`. An ordinary"
        echo "# run prints its timings and writes nothing, so this file is a series of"
        echo "# measurements somebody meant to take rather than a log of every push."
        echo "#"
        echo "# seconds covers the SUITE only — clone, npm install and build are"
        echo "# excluded, being dominated by the network and by whatever npm had"
        echo "# cached. What is left is the part an engine change can move."
        echo "#"
        echo "# COMPARE ROWS FROM THE SAME host AND toolchain, AND PREFER RUNS TAKEN"
        echo "# BACK TO BACK. These are wall-clock times on whatever machine ran them;"
        echo "# across machines, or across a busy and an idle one, the difference"
        echo "# between two rows says more about the machine than about the engine."
        echo "# ci/bench/ab-compare.sh is the instrument for deciding whether a"
        echo "# performance change is real; this file is for noticing that something"
        echo "# has become slow, not for proving by how much."
        printf 'run\thost\tnode\tgo\tengine\tpackage\truntime\tseconds\tstatus\n'
      } > "$TIMINGS"
    fi
    for label in "${TIMED_ORDER[@]}"; do
      pkg="${label%/*}"
      rt="${label##*/}"
      st=ok
      for entry in "${results[@]}"; do
        case "$entry" in
          "$label"*FAIL*|"$label"*UNEXPECTED*) st=fail ;;
          "$label"*xfail*) st=xfail ;;
        esac
      done
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$run_id" "$host" "$nodev" "${gov:-none}" "$engine" \
        "$pkg" "$rt" "$(secs "${TIMING[$label]}")" "$st" >> "$TIMINGS"
    done
    note "recorded ${#TIMED_ORDER[@]} row(s) in $TIMINGS"
  fi
fi

# The diagnostics, not just the verdict. A CI log showing `expr/ts FAIL`
# and nothing else cannot be acted on, and the checkout it came from is
# gone by the time anyone looks.
if [ "$fail" != 0 ]; then
  step "output of failing suites"
  for entry in "${results[@]}"; do
    case "$entry" in
      *FAIL*)
        label="${entry%% *}"
        log="$LOGS/${label%/*}.${label##*/}.log"
        [ -f "$log" ] || log="$LOGS/${label%/*}.build.log"
        [ -f "$log" ] || log="$LOGS/${label%/*}.install.log"
        [ -f "$log" ] || continue
        printf '\n--- %s (last 40 lines of %s) ---\n' "$label" "$log"
        tail -40 "$log"
        ;;
    esac
  done
fi

if [ "$UPDATE_LOCK" = 1 ] && [ "$OFFLINE" = 0 ]; then
  {
    echo "# The versions recorded by the last --update-lock run. run-fleet.sh"
    echo "# compares the registry against this and prints what moved; it never"
    echo "# FAILS on a difference, because a new release is the thing being"
    echo "# tested, not something to be pinned away from."
    echo "# Rewrite with: ci/fleet/run-fleet.sh --update-lock"
    for name in "${NAMES[@]}"; do echo "$name ${VERSION[$name]}"; done
  } > "$LOCK"
  note "wrote $LOCK"
fi

step "fleet gate"
if [ "$fail" = 0 ]; then echo "FLEET PASS"; else echo "FLEET FAIL"; fi
exit $fail
