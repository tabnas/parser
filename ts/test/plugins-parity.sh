#!/usr/bin/env bash
# plugins-parity.sh
#
# Builds this amagama checkout (TS + Go), then for every sibling plugin
# project in ~/Projects/amagamajs with a Makefile (excluding amagama itself),
# temporarily links the plugin against this local amagama and runs
# `make test` so both the TS and Go sides exercise the current fix.
#
# TS side: node_modules/amagama is replaced with a symlink to $AMAGAMA_DIR.
# Go side: a `replace github.com/amagamajs/amagama/go => $AMAGAMA_DIR/go`
# directive is added via `go mod edit` to each plugin's go.mod.
#
# Both overrides are reverted after each plugin runs, whether the test
# passed, failed, or was interrupted. Plugins without an installed
# node_modules get `npm install` run on demand.
#
# Usage:
#   bash test/plugins-parity.sh              # run all plugins
#   bash test/plugins-parity.sh expr path    # run named plugins only
#   bash test/plugins-parity.sh --list       # list discovered plugins
#
# Exit status: 0 if every plugin's `make test` succeeded, 1 otherwise.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AMAGAMA_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
AMAGAMAJS_DIR="$(cd "$AMAGAMA_DIR/.." && pwd)"

AMAGAMA_MOD="github.com/amagamajs/amagama/go"

log()  { printf '\033[1;34m[parity]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[parity]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[parity]\033[0m %s\n' "$*"; }

# Discover every sibling project with a Makefile (excluding amagama itself).
discover_plugins() {
  local d name
  for d in "$AMAGAMAJS_DIR"/*/; do
    name="$(basename "$d")"
    [ "$name" = "amagama" ] && continue
    [ -f "$d/Makefile" ] || continue
    printf '%s\n' "$name"
  done
}

ensure_amagama_built() {
  log "building amagama (TS)"
  ( cd "$AMAGAMA_DIR" && npm run build ) >/dev/null || {
    fail "amagama TS build failed"
    return 1
  }
  log "building amagama (Go)"
  ( cd "$AMAGAMA_DIR/go" && go build ./... ) || {
    fail "amagama Go build failed"
    return 1
  }
}

# Replace node_modules/amagama with a symlink to our local checkout.
# Saves a non-link original under .amagama.parity-bak so it can be restored.
link_ts() {
  local dir=$1
  local nm="$dir/node_modules"
  if [ ! -d "$nm" ]; then
    log "  npm install (no node_modules)"
    ( cd "$dir" && npm install --no-audit --no-fund --silent ) || {
      warn "  npm install failed — skipping ts link"
      return 1
    }
  fi
  if [ -e "$nm/amagama" ] && [ ! -L "$nm/amagama" ]; then
    mv "$nm/amagama" "$nm/.amagama.parity-bak"
  else
    rm -rf "$nm/amagama"
  fi
  ln -s "$AMAGAMA_DIR" "$nm/amagama"
}

unlink_ts() {
  local dir=$1
  local nm="$dir/node_modules"
  [ -d "$nm" ] || return 0
  if [ -L "$nm/amagama" ]; then
    rm -f "$nm/amagama"
  fi
  if [ -e "$nm/.amagama.parity-bak" ]; then
    mv "$nm/.amagama.parity-bak" "$nm/amagama"
  fi
}

# Add a `replace` for the local amagama Go module in the plugin's go.mod.
link_go() {
  local dir=$1
  [ -f "$dir/go/go.mod" ] || return 0
  ( cd "$dir/go" && go mod edit "-replace=$AMAGAMA_MOD=$AMAGAMA_DIR/go" )
}

unlink_go() {
  local dir=$1
  [ -f "$dir/go/go.mod" ] || return 0
  ( cd "$dir/go" && go mod edit "-dropreplace=$AMAGAMA_MOD" ) 2>/dev/null || true
}

# Called on EXIT/INT/TERM. Restores every plugin we've touched.
TOUCHED=()
cleanup() {
  local p
  for p in "${TOUCHED[@]+"${TOUCHED[@]}"}"; do
    unlink_ts "$AMAGAMAJS_DIR/$p"
    unlink_go "$AMAGAMAJS_DIR/$p"
  done
}
trap cleanup EXIT INT TERM

# Run `make test` for one plugin under the linked local amagama.
# Echoes " pass" or " fail <rc>" on the accumulator FD (3) so the caller
# can collect results without parsing stdout.
run_plugin() {
  local plugin=$1
  local dir="$AMAGAMAJS_DIR/$plugin"
  log "=== $plugin ==="
  TOUCHED+=("$plugin")

  link_ts "$dir" || { printf '%s fail link-ts\n' "$plugin" >&3; return 1; }
  link_go "$dir"

  local rc=0
  ( cd "$dir" && make test ) || rc=$?

  unlink_ts "$dir"
  unlink_go "$dir"

  # Drop from TOUCHED once cleanly unlinked so cleanup doesn't double-unlink.
  local i new=()
  for i in "${TOUCHED[@]}"; do
    [ "$i" = "$plugin" ] || new+=("$i")
  done
  TOUCHED=("${new[@]+"${new[@]}"}")

  if [ "$rc" -eq 0 ]; then
    printf '%s pass\n' "$plugin" >&3
  else
    printf '%s fail %d\n' "$plugin" "$rc" >&3
  fi
  return "$rc"
}

main() {
  if [ "${1-}" = "--list" ]; then
    discover_plugins
    exit 0
  fi

  local plugins=()
  if [ "$#" -gt 0 ]; then
    plugins=("$@")
  else
    while IFS= read -r p; do plugins+=("$p"); done < <(discover_plugins)
  fi

  if [ "${#plugins[@]}" -eq 0 ]; then
    fail "no plugins discovered in $AMAGAMAJS_DIR"
    exit 1
  fi

  ensure_amagama_built || exit 1

  local results_file
  results_file="$(mktemp)"
  exec 3>"$results_file"

  local overall=0
  local p
  for p in "${plugins[@]}"; do
    run_plugin "$p" || overall=1
  done

  exec 3>&-

  echo
  log "summary"
  printf '  %-14s  %s\n' PLUGIN RESULT
  printf '  %-14s  %s\n' -------------- ------
  while read -r line; do
    set -- $line
    printf '  %-14s  %s\n' "$1" "${2}${3:+ ($3)}"
  done <"$results_file"
  rm -f "$results_file"

  exit "$overall"
}

main "$@"
