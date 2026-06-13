/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Strict-escape mode (string.escapeStrict) and escape-map removal.
// Mirrored byte-for-byte by go/strict_escape_test.go — the two runtimes
// must reject the same inputs with the same error codes, since the
// downstream strict-JSON grammar plugins assert on those shared codes.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

// A one-string grammar over the bare engine, configured with the given
// string options, so each case exercises only the escape handling.
function strParser(stringOpts) {
  const am = new Tabnas({ rule: { start: 'top' }, string: stringOpts })
  am.rule('top', (rs) =>
    rs.open([{ s: ['#ST'], a: (r) => (r.node = r.o0.val) }]).close([{ s: ['#ZZ'] }]),
  )
  return am
}

function parseCode(am, src) {
  try {
    return { ok: true, val: am.parse(src) }
  } catch (e) {
    return { ok: false, code: e.code }
  }
}

describe('strict-escape', () => {
  // Strict config: disable \x and \u{...}, reject unknown escapes, and
  // drop \v \' \` from the escape map — i.e. JSON.parse's escape set.
  const strict = {
    allowUnknown: false,
    escapeStrict: true,
    escape: { v: '', "'": '', '`': '' },
  }

  it('rejects non-standard escapes with shared codes', () => {
    const am = strParser(strict)
    // \x ASCII escape: 'x' is no longer a recognised escape → unexpected.
    assert.deepEqual(parseCode(am, '"\\x41"'), { ok: false, code: 'unexpected' })
    // braced unicode: falls into the \uXXXX path, '{' is not hex → invalid_unicode.
    assert.deepEqual(parseCode(am, '"\\u{41}"'), { ok: false, code: 'invalid_unicode' })
    // removed built-ins → unknown escape → unexpected.
    assert.deepEqual(parseCode(am, '"\\v"'), { ok: false, code: 'unexpected' })
    assert.deepEqual(parseCode(am, '"\\\'"'), { ok: false, code: 'unexpected' })
    assert.deepEqual(parseCode(am, '"\\`"'), { ok: false, code: 'unexpected' })
  })

  it('still accepts standard escapes and \\uXXXX surrogate pairs', () => {
    const am = strParser(strict)
    // Surrogate pair for U+1F600 😀.
    assert.deepEqual(parseCode(am, '"\\uD83D\\uDE00"'), { ok: true, val: '😀' })
    // The standard JSON escapes.
    assert.deepEqual(parseCode(am, '"\\n\\t\\"\\\\\\/\\b\\f\\r"'), {
      ok: true,
      val: '\n\t"\\/\b\f\r',
    })
    // Plain \uXXXX.
    assert.deepEqual(parseCode(am, '"\\u0041"'), { ok: true, val: 'A' })
  })

  it('leaves default (non-strict) behaviour unchanged', () => {
    const am = strParser({}) // engine defaults
    assert.deepEqual(parseCode(am, '"\\x41"'), { ok: true, val: 'A' })
    assert.deepEqual(parseCode(am, '"\\u{41}"'), { ok: true, val: 'A' })
    assert.deepEqual(parseCode(am, '"\\v"'), { ok: true, val: '\v' })
    assert.deepEqual(parseCode(am, '"\\\'"'), { ok: true, val: "'" })
    assert.deepEqual(parseCode(am, '"\\`"'), { ok: true, val: '`' })
  })

  it('escape-map removal works without strict mode', () => {
    // Dropping \v via the escape map (mapped to '') rejects it even when
    // strict mode is off, as long as unknown escapes are disallowed.
    const am = strParser({ allowUnknown: false, escape: { v: '' } })
    assert.deepEqual(parseCode(am, '"\\v"'), { ok: false, code: 'unexpected' })
    // \x and \u{ remain enabled (strict is off).
    assert.deepEqual(parseCode(am, '"\\x41"'), { ok: true, val: 'A' })
  })
})
