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

  it('does not replay before-close actions on a resumed close pass', () => {
    // `[1 :]` errors in the elem close after @elem-bc already appended
    // the 1 — recovery resuming the same rule must not run it again.
    const tn = mk()
    const out = tn.parse('[1 :]')
    assert.ok(1 <= out.errors.length)
    if (Array.isArray(out.value)) {
      const ones = out.value.filter((v) => 1 === v).length
      assert.ok(ones <= 1, 'element duplicated by replayed bc action: ' + JSON.stringify(out.value))
    }
  })

  it('suppresses a lexer error region right after a recovery', () => {
    const zero = mk({ suppress: 0 }).parse('{"a":true blah blip,"b":1}')
    const eight = mk({ suppress: 8 }).parse('{"a":true blah blip,"b":1}')
    assert.ok(2 <= zero.errors.length, 'suppress 0 keeps both regions')
    assert.equal(eight.errors.length, 1, 'window suppresses the second region')
    assert.equal(eight.value.b, 1)
  })

  it('caps unlexable-run length at maxSkip', () => {
    const tn = mk({ maxSkip: 3 })
    const out = tn.parse('{"a": zzzzzzzzzzzzzzzzzzzz}')
    assert.ok('errors' in out, 'gives up but still returns the shape')
    assert.ok(1 <= out.errors.length)
  })

  it('replaces syncGroups rather than splicing over defaults', () => {
    // A replacement array must not retain engine defaults by index:
    // ['nope'] means ONLY 'nope' (yielding structural fallback here).
    const tn = mk({ syncGroups: ['nope'] })
    const out = tn.parse('{"a":1,}')
    assert.ok('errors' in out && 1 <= out.errors.length)
    const cfgGroups = tn.internal().config.parse.recover.syncGroups
    assert.deepStrictEqual(cfgGroups, ['nope'])
  })

  it('give-up path still returns the active partial container', () => {
    const tn = mk({ maxRecoveries: 1, suppress: 0 })
    const out = tn.parse('{"a":true blah,"b":false blah blah}')
    assert.ok(null != out.value, 'partial value present on give-up')
    assert.equal(out.value.a, true)
  })

  it('resynchronizes at line end for in-string control characters', () => {
    const tn = mk()
    const out = tn.parse('{"a":"x' + String.fromCharCode(7) + 'y",\n"b":2}')
    assert.equal(out.errors.length, 1)
    assert.equal(out.errors[0].code, 'unprintable')
    // The corrupted line is abandoned at the line end: the structure
    // survives (an object comes back) and the mis-lexed remainder of
    // the string cannot swallow the following lines into one giant
    // string value (each remaining value stays short).
    assert.equal('object', typeof out.value)
    for (const v of Object.values(out.value)) {
      if ('string' === typeof v) assert.ok(v.length < 5, JSON.stringify(v))
    }
  })

  it('does not disturb subsequent parses on the same instance', () => {
    const tn = mk()
    const bad = tn.parse('{"a":1,}')
    assert.equal(bad.errors.length, 1)
    const good = tn.parse('{"a":1}')
    assert.deepStrictEqual(good.errors, [])
    assert.equal(good.value.a, 1)
  })

  it('give-up leaves only the recorded errors: no post-loop extras', () => {
    // When recovery gives up (skip cap hit, no sync token), the
    // terminal fault is already recorded and the parse converts to
    // { value, errors } — the post-loop completeness checks never run,
    // so no error is manufactured from text recovery deliberately
    // stopped processing. Mirrors go TestRecoverGiveUpAddsNoTrailingError.
    const tn = mk({ maxSkip: 0 })
    const out = tn.parse('[1 : abc def ghi]')
    assert.deepStrictEqual(out.errors.map((e) => e.code), ['unexpected'])
  })

  it('trailing bad tokens keep their own error code', () => {
    // A bad token in trailing content carries its own code (the
    // parser.ts buffered/endtry checks raise why || unexpected): the
    // generic label must not swallow unterminated_string. Both the
    // prefetched-lookahead route (json grammar) and the no-lookahead
    // route (a rule closing on an empty alternate) are pinned.
    // Mirrors go TestRecoverTrailingBadTokenKeepsCode.
    const viaLookahead = mk().parse('1 "abc')
    assert.deepStrictEqual(
      viaLookahead.errors.map((e) => e.code), ['unterminated_string'])
    assert.equal(viaLookahead.value, 1)

    const noLookahead = new Tabnas({ parse: { recover: { enabled: true } } })
    noLookahead.grammar({
      options: { rule: { start: 'top' } },
      rule: { top: { open: [{ s: '#TX' }], close: [{}] } },
    })
    const out = noLookahead.parse('x "')
    assert.deepStrictEqual(out.errors.map((e) => e.code), ['unterminated_string'])
  })

  it('give-up still returns the partial value when the root never closed', () => {
    // Pins the canonical behaviour the Go port diverged from: giving up
    // inside a structure leaves the root rule open, and the most
    // complete partial container is still returned — which is what a
    // language server shows for a broken document. Mirrors go
    // TestRecoverGiveUpKeepsPartialValue.
    const tn = mk({ maxSkip: 0 })
    for (const [src, want] of [
      ['[1 : abc def ghi]', '[1]'],
      ['{"a":1,"b":2 : xxx}', '{"a":1,"b":2}'],
      ['{"a":1} : zzz', '{"a":1}'],
    ]) {
      const out = tn.parse(src)
      assert.ok(0 < out.errors.length, src)
      assert.equal(JSON.stringify(out.value), want, src)
    }
  })

  it('reports trailing content after a complete value', () => {
    // The completeness checks run AFTER the rule loop: a document
    // whose value parses cleanly but is followed by junk must still
    // produce a diagnostic in recovery mode, with the value kept.
    // Pinned in both runtimes (go TestRecoverTrailingContent): the Go
    // port once skipped these checks entirely under recovery and
    // silently ACCEPTED `"x" q` — an accept/reject divergence.
    const tn = mk()
    for (const [src, value] of [['"x" q', 'x'], ['1 q', 1], ['"a" "b"', 'a']]) {
      const out = tn.parse(src)
      assert.equal(out.errors.length, 1, src + ' -> ' + JSON.stringify(out.errors))
      assert.equal(out.errors[0].code, 'unexpected', src)
      assert.equal(out.value, value, src)
    }
  })
})
