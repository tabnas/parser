#!/usr/bin/env bash
# Rust port gate. Kept in one script so local and hosted validation cannot
# quietly drift apart.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)

cd "$ROOT/rs"
cargo fmt --all --check
cargo build --all-targets --locked
cargo test --all-targets --locked
cargo clippy --all-targets --all-features --locked -- -D warnings

cd "$ROOT"
ci/parity/run-parity.sh json ../json/test/spec
ci/parity/run-parity.sh json test/spec
node ci/rust/gbnf-corpus.js
