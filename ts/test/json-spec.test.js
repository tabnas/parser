/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Runs the shared strict-JSON spec fixtures (test/spec/include-json*.tsv)
// against the strict-JSON grammar plugin (test/json-plugin.ts). The Go
// port runs the same fixtures (go/alignment_test.go TestIncludeJSON*),
// keeping the two runtimes coupled on the strict-JSON surface.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')
const { loadTSV } = require('./utility')

describe('json-spec', function () {
  it('include-json', () => {
    const j = new Tabnas({ plugins: [json] })
    for (const { cols, row } of loadTSV('include-json')) {
      const [input, expected] = cols
      assert.deepEqual(
        j.parse(input),
        JSON.parse(expected),
        'include-json.tsv row ' + row + ': ' + input,
      )
    }
  })

  it('include-json-errors', () => {
    const j = new Tabnas({ plugins: [json] })
    for (const { cols, row } of loadTSV('include-json-errors')) {
      const [input, expected] = cols
      assert.ok(
        expected.startsWith('ERROR:'),
        'include-json-errors.tsv row ' + row + ': expected must be ERROR:<code>',
      )
      const code = expected.slice('ERROR:'.length)
      try {
        j.parse(input)
        assert.fail(
          'include-json-errors.tsv row ' + row + ': ' + input +
          ' should error with ' + code,
        )
      } catch (e) {
        assert.equal(
          e.code,
          code,
          'include-json-errors.tsv row ' + row + ': ' + input,
        )
      }
    }
  })
})
