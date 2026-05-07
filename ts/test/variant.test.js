/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama } = require('..')
const { json } = require('../dist-test/json-plugin')

describe('variant', function () {
  it('json-strict', () => {
    // The `json` plugin (test fixture; lives under test/json-plugin.ts)
    // gives JSON.parse-equivalent semantics: only standard JSON
    // tokens, no relaxations.
    const j = new Amagama({ plugins: [json] })

    assert.deepEqual(j.parse('{"a":1}'), { a: 1 })
    assert.deepEqual(
      j.parse('{"a":1,"b":"x","c":true,"d":{"e":[-1.1e2,{"f":null}]}}'),
      { a: 1, b: 'x', c: true, d: { e: [-1.1e2, { f: null }] } },
    )
    assert.deepEqual(j.parse(' "a" '), 'a')
    assert.deepEqual(j.parse('\r\n\t1.0\n'), 1.0)

    // Per JSON.parse — duplicate keys: last value wins.
    assert.deepEqual(j.parse('{"a":1,"a":2}'), { a: 2 })

    // Relaxations are rejected.
    assert.throws(() => j.parse('{a:1}'), /unexpected.*:1:2/s)
    assert.throws(() => j.parse('{"a":1,}'), /unexpected.*:1:8/s)
    assert.throws(() => j.parse('[a]'), /unexpected.*:1:2/s)
    assert.throws(() => j.parse('["a",]'), /unexpected.*:1:6/s)
    assert.throws(() => j.parse('"a" # foo'), /unexpected.*:1:5/s)
    assert.throws(() => j.parse('0xA'), /unexpected.*:1:1/s)
    assert.throws(() => j.parse('`a`'), /unexpected.*:1:1/s)
    assert.throws(() => j.parse("'a'"), /unexpected.*:1:1/s)
    assert.throws(() => j.parse(''), /unexpected.*:1:1/s)
    assert.throws(() => j.parse('{"a":1'), /unexpected.*:1:7/s)
    assert.throws(() => j.parse('[,a]'), /unexpected.*:1:2/s)
    assert.throws(() => j.parse(''), /unexpected/s)
    assert.throws(() => j.parse('00'), /unexpected/s)
    assert.throws(() => j.parse('{0:1}'), /unexpected/s)
    assert.throws(() => j.parse('["a"00,"b"]'), /unexpected/s)
    assert.throws(() => j.parse('[{}00,"b"]'), /unexpected/s)
  })
})
