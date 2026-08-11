/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */
'use strict'

// UTF-16 surrogate pairing inside quoted strings, across BOTH escape
// spellings. Mirrors go/surrogate_pairing_test.go.
//
// TypeScript needed no fix here: JS strings ARE UTF-16, so a high unit
// followed by a low unit is one astral character with no work from the
// lexer. This file exists because that made the divergence invisible from
// this side — every spelling below already passed, so the TS suite could
// not have caught Go decoding three of them to two U+FFFD each
// (rjrodger/aontu#24). It pins the shared contract so the ports can be
// compared, rather than each being checked against its own expectation.
//
// The lone-surrogate block at the end asserts the OPPOSITE of the Go file,
// deliberately. That divergence is documented in
// go/doc/differences.md line 327 and is a language-design question, not a
// bug — see aontu#24. If it is ever resolved, both files change together.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeLex } = require('..')
const { json } = require('../dist-test/json-plugin')

// U+1F600 GRINNING FACE, the character every spelling below denotes.
const ASTRAL = '\u{1F600}'

describe('surrogate-pairing', () => {
  it('all spellings of an astral character agree', () => {
    const j = new Tabnas({ plugins: [json] })

    // Four standard spellings plus the two mixed pairs. In Go these went
    // through two different escape branches; three of the six produced
    // two U+FFFD until the pairing moved onto the code unit sequence.
    const cases = [
      ['literal', '"' + ASTRAL + '"'],
      ['braced whole code point', '"\\u{1F600}"'],
      ['4-hex pair', '"\\ud83d\\ude00"'],
      ['braced pair', '"\\u{d83d}\\u{de00}"'],
      ['mixed: 4-hex high, braced low', '"\\ud83d\\u{de00}"'],
      ['mixed: braced high, 4-hex low', '"\\u{d83d}\\ude00"'],
    ]
    for (const [name, src] of cases) {
      const got = j.parse(src)
      assert.equal(got, ASTRAL, name + ' (' + src + ')')
      assert.equal(got.codePointAt(0), 0x1f600, name + ' code point')
      assert.equal(got.length, 2, name + ' is one astral char (2 UTF-16 units)')
    }
  })

  it('pairing survives surrounding text and repeats', () => {
    const j = new Tabnas({ plugins: [json] })
    const cases = [
      ['"a\\u{d83d}\\u{de00}b"', 'a' + ASTRAL + 'b'],
      ['"\\u{d83d}\\u{de00}\\u{d83d}\\u{de00}"', ASTRAL + ASTRAL],
      ['"\\ud83d\\ude00\\u{d83d}\\u{de00}"', ASTRAL + ASTRAL],
      ['"x\\ud83d\\u{de00}"', 'x' + ASTRAL],
      ['"\\u{d83d}\\ude00y"', ASTRAL + 'y'],
    ]
    for (const [src, want] of cases) {
      assert.equal(j.parse(src), want, src)
    }
  })

  it('lone surrogates are PRESERVED here — Go folds them to U+FFFD', () => {
    // DELIBERATE DIVERGENCE, not an oversight. go/doc/differences.md line
    // 327: "Lone surrogates | Preserved (JS strings allow them) | U+FFFD
    // (matches encoding/json)". Asserted so that folding them here would
    // fail loudly rather than look like an improvement — it would remove a
    // working capability, which is exactly why aontu#24 treats it as a
    // decision needing sign-off rather than a patch.
    const j = new Tabnas({ plugins: [json] })

    const lone = j.parse('"\\ud800"')
    assert.equal(lone.length, 1)
    assert.equal(lone.charCodeAt(0), 0xd800)
    assert.equal(lone.isWellFormed(), false, 'a lone surrogate survives')

    // Distinctness is the property that matters downstream: folding maps
    // these onto one value, which lets two different sources unify.
    assert.notEqual(j.parse('"\\ud800"'), j.parse('"\\ud801"'))
    assert.notEqual(j.parse('"\\ud800"'), j.parse('"\\ufffd"'))

    // The awkward cases, mirroring the Go table one-for-one.
    assert.equal(j.parse('"\\udc00"').charCodeAt(0), 0xdc00)
    assert.equal(j.parse('"\\u{d800}"').charCodeAt(0), 0xd800)
    assert.equal(j.parse('"\\ud800\\ud801"').length, 2)
    assert.equal(j.parse('"\\udc00\\ud800"').length, 2)
    assert.equal(j.parse('"\\ud800z"'), '\ud800z')
    assert.equal(j.parse('"a\\ud800"'), 'a\ud800')
    assert.equal(j.parse('"\\ud800\\n"'), '\ud800\n')
    assert.equal(j.parse('"\\ud800\\x41"'), '\ud800A')
    assert.equal(j.parse('"\\ud800\\u{41}"'), '\ud800A')
    assert.equal(j.parse('"\\ud83d\\ude00\\ud800"'), ASTRAL + '\ud800')
    assert.equal(j.parse('"\\ud800\\ud83d\\ude00"'), '\ud800' + ASTRAL)
  })

  it('bare text keeps the backslash literal', () => {
    // No escape processing happens outside quotes, so a change to string
    // escape handling must not reach here. This agreed across ports before
    // and must still agree (aontu#24).
    //
    // Checked at the LEXER, on the token itself: the bare engine has no
    // grammar to parse a bare run with, and the token is the precise claim
    // anyway — the text matcher never decodes an escape. Mirrors the Go
    // test, which hit the same wall.
    const j = new Tabnas()
    const lex = makeLex({
      src: () => 'a\\ud800',
      cfg: j.internal().config,
      opts: j.options,
      sub: {},
    })
    const tkn = lex.next()
    assert.equal(tkn.name, '#TX')
    assert.equal(tkn.val, 'a\\ud800')
    assert.equal(tkn.src, 'a\\ud800')
  })
})
