/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
// tabnas-port-deps-template v1: stamped by tabnas/admin tasks/adopt-port-deps.js. Change the template and restamp, never this file.
'use strict'

// The TypeScript, Go and Rust ports of this repository must depend on the
// same tabnas repos at runtime.
//
// tools/port-deps.cjs reads ts/package.json, rs/Cargo.toml and the imports
// of the Go module under go/, and compares the three sets. A difference
// that is not repaired yet is recorded in tools/port-deps.json with its
// reason and its repair direction. An entry there that the manifests no
// longer support fails this test as loudly as a difference nobody recorded,
// so a repaired difference forces its entry's deletion. The rules are
// ADR-24 in tabnas/admin DECISIONS.md.
//
// The test sits in the TypeScript suite because that suite runs on every
// platform CI covers. It checks all three ports, not only this one.

const { describe, test } = require('node:test')
const assert = require('node:assert')
const { join } = require('node:path')

const PortDeps = require('../../tools/port-deps.cjs')

// This file runs from ts/test, or compiled from ts/dist-test. The
// repository root is two levels up from either.
const repoRoot = join(__dirname, '..', '..')

// An id no tabnas repo has.
const PROBE = 'zz-port-deps-probe'

describe('port-deps', () => {
  test('the ports have the same tabnas runtime dependencies', () => {
    const result = PortDeps.check(repoRoot)
    assert.ok(result.ok, '\n' + result.report)
  })

  // A comparison that cannot fail proves nothing. Give one port at a time
  // a dependency the others lack, and require the comparison to report it.
  test('the comparison is live', (t) => {
    const ports = PortDeps.readRepo(repoRoot).ports
    const present = PortDeps.PORTS.filter((port) => ports[port])
    if (present.length < 2) {
      t.skip('fewer than two ports here, so there is nothing to compare')
      return
    }
    for (const port of present) {
      const read = PortDeps.readRepo(repoRoot)
      read.ports[port].runtime.push(PROBE)
      const out = PortDeps.compare(read, PortDeps.readRegister(repoRoot))
      const row = out.differences.find((r) => r.id === PROBE)
      assert.deepStrictEqual(
        row && row.in,
        [port],
        `a dependency only the ${port} port has was not reported as a difference`,
      )
      assert.ok(
        out.problems.some((problem) => problem.includes(PROBE)),
        `a dependency only the ${port} port has raised no problem`,
      )
    }
  })
})
