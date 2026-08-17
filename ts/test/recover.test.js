/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Opt-in error recovery: options.parse.recover (see
// ts/doc/lsp-feasibility.md). With the flag on, parse() returns
// { value, errors } instead of throwing, collecting multiple errors by
// skipping to sync points derived from the live rule stack. With the
// flag off (the default), behaviour is exactly the fail-fast contract
// pinned by every other suite.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, TabnasError } = require('..')
const { json } = require('../dist-test/json-plugin')

const mk = (opts) =>
  new Tabnas({ plugins: [json], parse: { recover: { enabled: true, ...opts } } })

describe('recover', () => {
  it('is off by default: fail-fast still throws', () => {
    const tn = new Tabnas({ plugins: [json] })
    assert.throws(() => tn.parse('{"a":1,,}'), TabnasError)
  })

  it('clean parse returns { value, errors: [] }', () => {
    const tn = mk()
    const out = tn.parse('{"a":[1,2]}')
    assert.equal(JSON.stringify(out.value), '{"a":[1,2]}')
    assert.deepStrictEqual(out.errors, [])
  })

  it('single error returns partial value and one error', () => {
    const tn = mk()
    const out = tn.parse('{"a":1,}')
    assert.ok(Array.isArray(out.errors))
    assert.equal(out.errors.length, 1)
    assert.ok(out.errors[0] instanceof TabnasError)
    assert.ok(null != out.value, 'partial value present')
    assert.equal(out.value.a, 1)
  })

  it('collects multiple separated errors in one parse', () => {
    const tn = mk({ suppress: 0 })
    const out = tn.parse('{"a":true blah,"b":false blah,"c":true blah}')
    assert.ok(
      2 <= out.errors.length,
      'expected 2+ errors, got ' +
        out.errors.length +
        ': ' +
        out.errors.map((e) => e.code).join(','),
    )
    // Structure around the errors survives.
    assert.equal(out.value.a, true)
  })

  it('recovers at EOF and returns the partial structure', () => {
    const tn = mk()
    const out = tn.parse('[1,2')
    assert.equal(out.errors.length, 1)
    assert.ok(Array.isArray(out.value))
    assert.deepStrictEqual(out.value.slice(0, 2), [1, 2])
  })

  it('caps recorded errors at maxRecoveries', () => {
    const cap = 5
    const tn = mk({ maxRecoveries: cap, suppress: 0 })
    const out = tn.parse('[' + 'true blah, '.repeat(50) + ']')
    assert.ok(
      out.errors.length <= cap + 1,
      'errors ' + out.errors.length + ' exceeds cap ' + (cap + 1),
    )
  })

  it('suppresses cascades inside the suppress window', () => {
    const loose = mk({ suppress: 0 }).parse('{"a":true blah blip,"b":1}')
    const tight = mk({ suppress: 8 }).parse('{"a":true blah blip,"b":1}')
    assert.ok(
      tight.errors.length <= loose.errors.length,
      'suppression should not increase error count',
    )
    assert.ok(1 <= tight.errors.length)
  })

  it('records recovery metadata on the error', () => {
    const tn = mk()
    const out = tn.parse('{"a":true blah blah blah,"b":2}')
    assert.ok(1 <= out.errors.length)
    const rec = out.errors[0].recovered
    assert.ok(rec, 'recovered metadata present')
    assert.equal('number', typeof rec.skipped)
    assert.ok(1 <= rec.skipped)
    assert.equal(out.value.b, 2)
  })

  it('gives up but still returns { value, errors } when skip cap is hit', () => {
    const tn = mk({ maxSkip: 2 })
    const out = tn.parse('{"a": blah blah blah blah blah blah}')
    assert.ok(1 <= out.errors.length)
    assert.ok('errors' in out && 'value' in out)
  })

  it('lexer soft mode: bad tokens are recorded and skipped', () => {
    // Control characters are bad tokens for the strict-JSON lexer.
    const tn = mk()
    const out = tn.parse('{"a":1,"b": 2}')
    assert.ok(1 <= out.errors.length)
    assert.equal(out.value.a, 1)
  })

  it('empty source honours the empty option under recovery', () => {
    const tn = mk()
    const out = tn.parse('')
    assert.ok('errors' in out)
  })

  it('does not disturb subsequent parses on the same instance', () => {
    const tn = mk()
    const bad = tn.parse('{"a":1,}')
    assert.equal(bad.errors.length, 1)
    const good = tn.parse('{"a":1}')
    assert.deepStrictEqual(good.errors, [])
    assert.equal(good.value.a, 1)
  })
})
