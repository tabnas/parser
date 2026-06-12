/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Coverage tests for the strict-JSON grammar fixture: info markers
// (map / list / text), the marker-key guard in pair handling, and
// the '@finish' end-of-source reference handler.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json, registerJsonGrammar } = require('../dist-test/json-plugin')

describe('cover-json-plugin', () => {
  it('info-markers-on-map-list-text', () => {
    const j = new Tabnas({ plugins: [json] }).make({
      info: { map: true, list: true, text: true },
    })

    const out = j.parse('{"a": [1, "b"]}')

    // Map and list nodes carry the (non-enumerable) marker.
    assert.deepEqual(out.__info__, { implicit: false, meta: {} })
    assert.deepEqual(out.a.__info__, { implicit: false, meta: {} })
    assert.ok(!Object.keys(out).includes('__info__'))

    // String values are wrapped with quote info.
    const s = out.a[1]
    assert.equal(typeof s, 'object')
    assert.equal(String(s), 'b')
    assert.deepEqual(s.__info__, { quote: '"' })

    // Non-string values are untouched.
    assert.equal(out.a[0], 1)
  })

  it('info-map-skips-marker-key-pair', () => {
    const j = new Tabnas({ plugins: [json] }).make({
      info: { map: true },
    })
    const out = j.parse('{"__info__": 1, "a": 2}')
    // The marker-named key is not assigned as a normal pair.
    assert.equal(out.a, 2)
    assert.deepEqual(out.__info__, { implicit: false, meta: {} })
  })

  it('info-text-unquoted-text-has-empty-quote', () => {
    // Default (permissive) options allow unquoted text; the JSON
    // grammar core wraps it with an empty quote marker.
    const j = new Tabnas({ info: { text: true } })
    registerJsonGrammar(j)
    const v = j.parse('hello')
    assert.equal(String(v), 'hello')
    assert.deepEqual(v.__info__, { quote: '' })
  })

  it('finish-ref-allows-or-rejects-end-of-source', () => {
    // rule.finish true (default): '@finish' yields no error.
    const j = new Tabnas()
    registerJsonGrammar(j)
    j.rule('val', (rs) =>
      rs.close([{ s: '#ZZ', e: '@finish' }], { append: false }),
    )
    assert.equal(j.parse('hello'), 'hello')

    // rule.finish false: '@finish' reports end_of_source.
    const j2 = new Tabnas({ rule: { finish: false } })
    registerJsonGrammar(j2)
    j2.rule('val', (rs) =>
      rs.close([{ s: '#ZZ', e: '@finish' }], { append: false }),
    )
    assert.throws(() => j2.parse('hello'), (e) => 'end_of_source' === e.code)
  })
})
