/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Runs the shared strict-JSON spec fixtures (test/spec/include-json*.tsv)
// against the strict-JSON grammar plugin (test/json-plugin.ts). The Go
// port runs the same fixtures (go/spec_test.go TestSpecIncludeJSON and
// TestSpecIncludeJSONErrors), keeping the two runtimes coupled on the
// strict-JSON surface.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')
const { loadTSV } = require('./utility')

describe('json-spec', function () {
  // Shared cross-runtime fixture for rule.maxmul, the runaway-guard
  // multiplier (the Go counterpart is TestSpecRuleMaxMul in
  // go/spec_test.go). Columns: maxmul | via | input | expected, where
  // expected is a JSON value (the parse result) or ERROR:<code>, the format
  // test/AGENTS.md defines for every shared fixture in this repo.
  //
  // `via` is the half that matters as much as the value. Go honoured
  // rule.maxmul at CONSTRUCTION and silently ignored it via SetOptions
  // (MaxMul lives on the Parser, and only the Config was rebuilt), while
  // TS reads it off the config at parse time and honoured both. A fixture
  // that only ever constructed would have passed on a half-fixed port.
  //
  // maxmul 0 and -1 are the boundary the two runtimes disagreed on: TS
  // honours a non-positive value literally, which is a zero budget — the
  // rule loop never runs and the parse fails as `unexpected`. Go coerced
  // it to the default 3 and parsed happily, so a guard you had explicitly
  // disarmed silently rearmed itself.
  it('rule-maxmul-spec', () => {
    for (const { cols, row } of loadTSV('rule-maxmul')) {
      const [maxmulStr, via, input, expected] = cols
      const maxmul = parseInt(maxmulStr, 10)
      try {
        let j
        if ('construct' === via) {
          j = new Tabnas({ plugins: [json], rule: { maxmul } })
        } else {
          // The public IN-PLACE setter, not make(). make() returns a child
          // instance, so it exercises construction a second time — the Go
          // runner calls SetOptions on the parser that already exists, and a
          // fixture whose two runners take different paths compares nothing
          // on the path it claims to cover.
          j = new Tabnas({ plugins: [json] })
          j.options({ rule: { maxmul } })
        }

        if (expected.startsWith('ERROR:')) {
          assert.throws(
            () => j.parse(input),
            (e) => 'ERROR:' + (e.code || e.message) === expected,
            expected,
          )
        } else {
          assert.deepEqual(j.parse(input), JSON.parse(expected))
        }
      } catch (err) {
        err.message =
          `rule-maxmul row ${row}: maxmul=${maxmulStr} via=${via}` +
          ` input=${JSON.stringify(input)}\n` + err.message
        throw err
      }
    }
  })


  it('include-json', () => {
    const j = new Tabnas({ plugins: [json] })
    for (const name of ['include-json', 'include-json-utf8']) {
      for (const { cols, row } of loadTSV(name)) {
        const [input, expected] = cols
        assert.deepEqual(
          j.parse(input),
          JSON.parse(expected),
          name + '.tsv row ' + row + ': ' + input,
        )
      }
    }
  })

  it('include-json-errors', () => {
    const j = new Tabnas({ plugins: [json] })
    for (const name of ['include-json-errors', 'include-json-utf8-errors'])
    for (const { cols, row } of loadTSV(name)) {
      const [input, expected] = cols
      assert.ok(
        expected.startsWith('ERROR:'),
        name + ".tsv row " + row + ': expected must be ERROR:<code>',
      )
      const code = expected.slice('ERROR:'.length)
      try {
        j.parse(input)
        assert.fail(
          name + ".tsv row " + row + ': ' + input +
          ' should error with ' + code,
        )
      } catch (e) {
        assert.equal(
          e.code,
          code,
          name + ".tsv row " + row + ': ' + input,
        )
      }
    }
  })
})
