#!/usr/bin/env bash
# Rust port gate. Kept in one script so local and hosted validation cannot
# quietly drift apart.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)

cd "$ROOT/rs"
cargo fmt --all --check
cargo build --all-targets --locked
cargo test --all-targets --locked
# `--all-targets` EXCLUDES doctests, so the README examples the crate's front
# page is made of ran in no gate at all. They are the first code a consumer
# copies; compile and run them.
cargo test --doc --locked
cargo clippy --all-targets --all-features --locked -- -D warnings
# Broken intra-doc links are warnings, and a warning in a `cargo doc` run
# nobody reads is silence.
RUSTDOCFLAGS="-D warnings" cargo doc --no-deps --locked

cd "$ROOT"
ci/parity/run-parity.sh json ../json/test/spec
ci/parity/run-parity.sh json test/spec
node ci/rust/json-fuzz.js
node ci/rust/notation-corpus.js
node ci/rust/gbnf-corpus.js
