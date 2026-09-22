/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

// The dependency-source gate, inside the suite `npm test` runs.
//
// tools/dep-gate.cjs is repo-wide -- npm manifests and lockfiles, go.mod,
// Cargo.toml, .npmrc, committed symlinks and archives -- and `make deps`
// runs it directly. It is also the half of AGENTS.md "Never commit the
// local wiring" that a machine can check, so it runs here too rather than
// only from a target somebody has to remember: a green `npm test` over a
// committed `replace` pointing at a sibling nobody has is the failure this
// exists to stop. Needs nothing but node and git.

const Assert = require('node:assert')
const { describe, test } = require('node:test')

const Gate = require('../../tools/dep-gate.cjs')

describe('deps', () => {
  test('every committed dependency names a published package or GitHub', () => {
    const findings = Gate.checkAll()
    Assert.deepEqual(
      findings.map((f) => f.file + ' ' + f.where + ' ' + f.rule), [],
      '\n' + Gate.report(findings),
    )
  })
})
