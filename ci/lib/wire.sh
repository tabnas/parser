#!/usr/bin/env bash
# wire.sh — point a downstream checkout's @tabnas/* deps at sibling
# WORKING TREES. Sourced by ci/gate/run-gate.sh and ci/bench/run-bench.sh
# so the two cannot drift: measuring or gating against the wrong engine is
# the failure this file exists to make impossible.
#
# Usage:  . "$(dirname "$0")/../lib/wire.sh"
#         link_ts_dep <repo-ts-dir> <scope-name> <target-dir>
#         unlink_ts_deps <repo-ts-dir>

# link_ts_dep replaces <repo-ts-dir>/node_modules/@tabnas/<scope-name>
# with a symlink to <target-dir>, and proves the swap took.
link_ts_dep() {
  local dest="$1/node_modules/@tabnas/$2"
  local target="$3"

  # Without this, mkdir -p below would fabricate a node_modules tree
  # inside a repo that is not checked out.
  if [ ! -d "$1" ]; then
    echo "wire: $1 does not exist — is the sibling repo checked out?" >&2
    return 1
  fi

  # A missing target must fail here rather than leave a dangling symlink
  # behind: `readlink -f` normalises a dangling link and a nonexistent
  # path to the SAME string, so the check below cannot see that case.
  if [ ! -d "$target" ]; then
    echo "wire: target $target is not a directory — refusing to link $dest" >&2
    return 1
  fi

  mkdir -p "$1/node_modules/@tabnas"

  # `ln -snf` against a REAL directory — which is exactly what `npm i`
  # leaves behind — does NOT replace it. It exits 0, creates the link
  # INSIDE the directory, and the package goes on resolving to the
  # PUBLISHED copy. Nothing warns; the run simply measures the wrong
  # engine. Remove the destination first so the link is the only thing
  # that can answer.
  rm -rf "$dest"
  ln -s "$target" "$dest"

  # Structural proof, because the failure above is silent by nature: a
  # no-op leaves $dest resolving somewhere other than $target.
  if [ ! -d "$dest" ] || \
     [ "$(readlink -f "$dest")" != "$(readlink -f "$target")" ]; then
    echo "wire: $dest does not resolve to $target — refusing to continue" >&2
    return 1
  fi
}

# unlink_ts_deps removes the symlinks link_ts_dep left under
# <repo-ts-dir>/node_modules/@tabnas, and a package-lock.json git does not
# track, so that `npm install` in a reused checkout installs what the
# manifest declares. Over a symlinked package npm writes a link instead
# ("link": true in the lockfile it makes) and installs nothing of that
# package's own: the build then depends on whatever the link's target
# happens to hold. Wire the links again after the install.
unlink_ts_deps() {
  local dir="$1" link
  for link in "$dir"/node_modules/@tabnas/*; do
    if [ -L "$link" ]; then rm -f "$link"; fi
  done
  # A tracked lockfile is the repository's own, and the checkout restores
  # it; an untracked one was written by an earlier install.
  if [ -f "$dir/package-lock.json" ] &&
     ! git -C "$dir" ls-files --error-unmatch package-lock.json >/dev/null 2>&1; then
    rm -f "$dir/package-lock.json"
  fi
  return 0
}
