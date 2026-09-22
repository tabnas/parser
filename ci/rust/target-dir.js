#!/usr/bin/env node
/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Where cargo actually put the Rust gate binaries.
//
// The gates used to hardcode `rs/target/debug/<bin>`, which is only where
// cargo writes when nothing has redirected it. `CARGO_TARGET_DIR`, or a
// `[build] target-dir` in any cargo config that applies to this checkout,
// moves the whole directory, and a shared target dir is the normal setup on
// a machine that builds many crates. The gates then died with ENOENT on a
// build that had in fact just succeeded, which reads as a broken gate rather
// than a misresolved path. Ask cargo where it is instead of guessing.

const ChildProcess = require('node:child_process')
const Path = require('node:path')

const PARSER_ROOT = Path.resolve(__dirname, '..', '..')
const CRATE = Path.join(PARSER_ROOT, 'rs')
const MANIFEST = Path.join(CRATE, 'Cargo.toml')

function targetDir() {
  // Asked from rs/, not from wherever the caller happens to stand.
  // `--manifest-path` does not set the directory cargo resolves from, and
  // two things depend on that directory rather than on the manifest: a
  // relative `CARGO_TARGET_DIR` is taken relative to it, and cargo's config
  // search starts there, so an `rs/.cargo/config.toml` is invisible from the
  // repository root. `ci/rust/run.sh` builds from rs/ (it cds there) and
  // then runs these gates from the root, so asking from the root answers
  // for a different build: with `CARGO_TARGET_DIR=cache` the binaries are
  // written to rs/cache/debug and this reported <root>/cache/debug, which
  // is the missing-runner-after-a-successful-build failure all over again.
  const out = ChildProcess.execFileSync(
    'cargo',
    ['metadata', '--format-version', '1', '--no-deps',
      '--manifest-path', MANIFEST],
    { cwd: CRATE, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 },
  )
  const meta = JSON.parse(out)
  if (!meta.target_directory) {
    throw new Error('cargo metadata reported no target_directory')
  }
  return meta.target_directory
}

// Path of a debug-profile binary built from rs/.
function binary(name) {
  return Path.join(targetDir(), 'debug',
    'win32' === process.platform ? name + '.exe' : name)
}

module.exports = { targetDir, binary }

if (require.main === module) {
  process.stdout.write(binary(process.argv[2] || '') + '\n')
}
