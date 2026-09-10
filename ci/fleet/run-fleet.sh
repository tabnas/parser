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
#
# Exit status is the gate: 0 only when every suite that was expected to
# pass did, AND nothing in expect-fail.txt passed unexpectedly.
set -uo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"

WORK="$DIR/.work"
ONLY=""
RUNTIME="both"
OFFLINE=0
UPDATE_LOCK=0

while [ $# -gt 0 ]; do
  case "$1" in
    --only) ONLY="$2"; shift 2 ;;
    --runtime) RUNTIME="$2"; shift 2 ;;
    --work) WORK="$2"; shift 2 ;;
    --offline) OFFLINE=1; shift ;;
    --update-lock) UPDATE_LOCK=1; shift ;;
    -h|--help) sed -n '2,28p' "$0"; exit 0 ;;
    *) echo "run-fleet: unknown option $1" >&2; exit 2 ;;
  esac
done

case "$RUNTIME" in ts|go|both) ;; *) echo "run-fleet: --runtime must be ts, go or both" >&2; exit 2 ;; esac

step() { printf '\n=== %s ===\n' "$*"; }
note() { printf '  %s\n' "$*"; }

# link_ts_dep lives in ci/lib/wire.sh, shared with run-gate.sh and
# run-bench.sh so the three wirings cannot drift apart. Wiring the wrong
# engine is the one failure that makes this whole harness lie.
. "$DIR/../lib/wire.sh"

# --- preflight ---------------------------------------------------------
# Fail on a missing toolchain HERE, with a sentence, rather than thirty
# rows of identical "command not found" further down.
for tool in node npm git; do
  command -v "$tool" >/dev/null 2>&1 || { echo "run-fleet: $tool is required" >&2; exit 2; }
done
if [ "$RUNTIME" != "ts" ]; then
  command -v go >/dev/null 2>&1 || { echo "run-fleet: go is required for --runtime go/both" >&2; exit 2; }
fi

# --- the manifest ------------------------------------------------------
# Read through node rather than jq: node is already a hard dependency of
# this repo and jq is not, so parsing JSON with a regex is not the trade
# being made here.
manifest() {
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

    // Build order, by base chain: jsonic cannot build before json. A
    // selection pulls in the bases it needs to build, even when they were
    // not asked for, and they are marked so the report can say why they
    // are there.
    const need = new Set()
    const pull = (name) => {
      if (need.has(name) || !all.has(name)) return
      need.add(name)
      const base = all.get(name).base
      if (base) pull(base)
    }
    for (const p of m.packages) if (!only || only.has(p.name)) pull(p.name)

    const order = []
    const emit = (name) => {
      if (order.includes(name) || !need.has(name)) return
      const base = all.get(name).base
      if (base) emit(base)
      order.push(name)
    }
    for (const name of need) emit(name)

    for (const name of order) {
      const p = all.get(name)
      const asked = !only || only.has(name)
      // name<TAB>suites<TAB>asked
      console.log([name, p.suites && asked ? "1" : "0", asked ? "1" : "0"].join("\t"))
    }
  ' "$DIR/fleet.json" "$ONLY"
}

MANIFEST="$(manifest)" || exit 2
NAMES=()
declare -A SUITES=()
while IFS=$'\t' read -r name suites _asked; do
  [ -n "$name" ] || continue
  NAMES+=("$name")
  SUITES["$name"]="$suites"
done <<< "$MANIFEST"

step "fleet: ${#NAMES[@]} package(s)"
note "${NAMES[*]}"

# --- expected failures -------------------------------------------------
# A known-broken package is recorded HERE, with a reason, and it is still
# run. Two rules, both borrowed from ci/gate/fixture-sync-allow.txt:
#   - an entry that PASSES is a failure, so an exemption cannot outlive
#     the breakage it was written for;
#   - an entry with no reason is rejected, so nobody can quiet a package
#     by adding a bare name.
declare -A EXPECT_FAIL=()
EXPECT_FILE="$DIR/expect-fail.txt"
if [ -f "$EXPECT_FILE" ]; then
  while IFS= read -r line; do
    line="${line%%#*}"
    line="$(echo "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    [ -n "$line" ] || continue
    key="${line%%:*}"
    reason="$(echo "${line#*:}" | sed 's/^[[:space:]]*//')"
    if [ "$key" = "$line" ] || [ -z "$reason" ]; then
      echo "run-fleet: expect-fail.txt: '$line' has no reason — refusing to run" >&2
      exit 2
    fi
    EXPECT_FAIL["$key"]="$reason"
  done < "$EXPECT_FILE"
fi

# --- resolve the latest published version of each package --------------
# "Latest" is what the registry serves as the `latest` dist-tag, which is
# what a user gets from `npm i @tabnas/<name>`. The git tag `ts/vX.Y.Z` is
# the commit that produced it (see each repo's `repo-tag` script), so it is
# the tree whose tests describe that release.
declare -A VERSION=()
declare -A LOCKED=()
LOCK="$DIR/fleet.lock"
if [ -f "$LOCK" ]; then
  while IFS=' ' read -r n v; do
    [ -n "${n:-}" ] || continue
    case "$n" in \#*) continue ;; esac
    LOCKED["$n"]="$v"
  done < "$LOCK"
fi

if [ "$OFFLINE" = 1 ]; then
  step "versions: --offline, reusing $WORK as it stands"
else
  step "versions: checking the registry for updates"
  for name in "${NAMES[@]}"; do
    v="$(npm view "@tabnas/$name" version 2>/dev/null | tail -1)"
    if [ -z "$v" ]; then
      echo "run-fleet: cannot resolve @tabnas/$name from the registry" >&2
      exit 2
    fi
    VERSION["$name"]="$v"
    was="${LOCKED[$name]:-}"
    if [ -z "$was" ]; then
      note "$(printf '%-14s %-10s (new — not in fleet.lock)' "$name" "$v")"
    elif [ "$was" != "$v" ]; then
      note "$(printf '%-14s %-10s <- %s  UPDATED' "$name" "$v" "$was")"
    else
      note "$(printf '%-14s %-10s' "$name" "$v")"
    fi
  done
fi

# --- check out each package at that version ----------------------------
mkdir -p "$WORK"
if [ "$OFFLINE" = 0 ]; then
  step "checkout"
  for name in "${NAMES[@]}"; do
    v="${VERSION[$name]}"
    repo="$WORK/$name"
    tag="ts/v$v"

    if [ ! -d "$repo/.git" ]; then
      rm -rf "$repo"
      git clone --quiet --filter=blob:none --no-checkout \
        "https://github.com/tabnas/$name.git" "$repo" || {
          echo "run-fleet: clone failed for $name" >&2; exit 2; }
    fi

    if git -C "$repo" fetch --quiet --depth 1 origin "refs/tags/$tag:refs/tags/$tag" 2>/dev/null &&
       git -C "$repo" checkout --quiet --force "$tag" 2>/dev/null; then
      note "$(printf '%-14s %s' "$name" "$tag")"
    else
      # A missing release tag is REPORTED, never silent: the run still has
      # to happen, but "we tested the default branch" and "we tested the
      # release" are different claims and the log has to say which.
      head="$(git -C "$repo" remote show origin 2>/dev/null | sed -n 's/.*HEAD branch: //p')"
      head="${head:-main}"
      git -C "$repo" fetch --quiet --depth 1 origin "$head" &&
        git -C "$repo" checkout --quiet --force FETCH_HEAD || {
          echo "run-fleet: cannot check out $name" >&2; exit 2; }
      note "$(printf '%-14s %s  (NO TAG %s — used %s)' "$name" "$(git -C "$repo" rev-parse --short HEAD)" "$tag" "$head")"
    fi
  done
fi

# --- install TS toolchains ---------------------------------------------
if [ "$RUNTIME" != "go" ]; then
  step "npm install"
  for name in "${NAMES[@]}"; do
    [ -d "$WORK/$name/ts" ] || { note "$name: no ts/ — skipped"; continue; }
    if ( cd "$WORK/$name/ts" && npm install --no-audit --no-fund --silent >/dev/null 2>&1 ); then
      note "$name: ok"
    else
      # An install failure is not a test result. Say so plainly rather than
      # letting it surface later as a hundred MODULE_NOT_FOUND lines.
      note "$name: INSTALL FAILED"
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
    [ -d "$WORK/$probe/ts" ] || continue
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
    [ -d "$WORK/$name/go" ] && GOMODS+=("$WORK/$name/go")
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

# --- build --------------------------------------------------------------
if [ "$RUNTIME" != "go" ]; then
  step "build TS (engine first, then the fleet in base order)"
  ( cd "$PARSER_ROOT/ts" && npx tsc --build src test ) || {
    echo "run-fleet: the engine's own TS build failed — fix that first" >&2; exit 2; }
  for name in "${NAMES[@]}"; do
    [ -d "$WORK/$name/ts/src" ] || continue
    if ( cd "$WORK/$name/ts" && npx tsc --build src >/dev/null 2>&1 ); then
      note "$name: built"
    else
      note "$name: BUILD FAILED"
    fi
  done
fi

# --- run ----------------------------------------------------------------
results=()
fail=0

record() { # record <label> <status>
  results+=("$(printf '%-22s %s' "$1" "$2")")
}

run_suite() { # run_suite <name> <runtime> <dir> <cmd...>
  local name="$1" rt="$2" dir="$3"
  shift 3
  local label="$name/$rt"

  if [ ! -d "$dir" ]; then
    record "$label" "SKIP (no $rt/ in this repo)"
    return
  fi

  local status
  if ( cd "$dir" && "$@" >/dev/null 2>&1 ); then status=pass; else status=fail; fi

  local expected="${EXPECT_FAIL[$label]:-}"
  if [ -n "$expected" ]; then
    if [ "$status" = fail ]; then
      record "$label" "xfail ($expected)"
    else
      # An exemption that no longer describes reality hides the next real
      # break. Passing here is a failure of the FILE, and it says so.
      record "$label" "UNEXPECTED PASS — remove from expect-fail.txt"
      fail=1
    fi
    return
  fi

  if [ "$status" = pass ]; then
    record "$label" "PASS"
  else
    record "$label" "FAIL"
    fail=1
  fi
}

step "suites"
for name in "${NAMES[@]}"; do
  [ "${SUITES[$name]}" = "1" ] || continue
  if [ "$RUNTIME" != "go" ]; then
    printf '  %s/ts ... ' "$name"
    run_suite "$name" ts "$WORK/$name/ts" npm test --silent
    printf '%s\n' "${results[-1]##* }"
  fi
  if [ "$RUNTIME" != "ts" ]; then
    printf '  %s/go ... ' "$name"
    run_suite "$name" go "$WORK/$name/go" go test ./...
    printf '%s\n' "${results[-1]##* }"
  fi
done

# --- report -------------------------------------------------------------
step "fleet result"
printf '%s\n' "${results[@]}"

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
