#!/bin/bash
# Copyright (c) 2013-2026 Richard Rodger, MIT License
#
# Verify the standalone Verus copies in this directory.
#
# This is NOT part of any gate and NOT a build dependency of the crate:
# `cargo build` never sees these files, and `ci/rust/run.sh` does not
# call this script. Verus pins its own Rust toolchain, so wiring it into
# CI would put a second toolchain beside `rust-version = "1.85"`. See
# `doc/rust-verus-experiment.md` for why that trade was not made.
#
# Usage:
#   VERUS=/path/to/verus rs/verus/run.sh
#   rs/verus/run.sh            # finds `verus` on PATH
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERUS="${VERUS:-$(command -v verus || true)}"

if [ -z "$VERUS" ] || [ ! -x "$VERUS" ]; then
  echo "verus not found. Set VERUS=/path/to/verus, or put verus on PATH."
  echo "Install: download the x86-linux release zip from"
  echo "  https://github.com/verus-lang/verus/releases"
  echo "then 'rustup toolchain install <the toolchain it names>'."
  exit 2
fi

status=0
for file in "$HERE"/*.rs; do
  echo "== $(basename "$file")"
  "$VERUS" "$file" || status=1
done
exit $status
