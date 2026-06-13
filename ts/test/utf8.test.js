/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// UTF-8 / Unicode handling: multi-byte characters of all sizes (2/3/4
// byte UTF-8; BMP and astral planes) in strings, keys, escapes, and as
// configured matcher chars. The Go port mirrors these in
// go/jsonic/utf8_test.go and via the shared include-json-utf8 fixtures.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

describe('utf8', function () {
  it('escapes', () => {
    const j = new Tabnas({ plugins: [json] })

    // 4-digit form, including surrogate pairs (the JSON encoding of
    // astral characters — pairing is implicit in UTF-16 strings).
    assert.equal(j.parse('"\\u0041"'), 'A')
    assert.equal(j.parse('"\\u00e9"'), 'é')
    assert.equal(j.parse('"\\u4e2d"'), '中')
    assert.equal(j.parse('"\\ud83d\\ude00"'), '😀')
    assert.equal(j.parse('"\\uD834\\uDD1E"'), '𝄞')

    // Braced form: 1-6 hex digits, any code point.
    assert.equal(j.parse('"\\u{41}"'), 'A')
    assert.equal(j.parse('"\\u{e9}"'), 'é')
    assert.equal(j.parse('"\\u{4E2D}"'), '中')
    assert.equal(j.parse('"\\u{1F600}"'), '😀')
    assert.equal(j.parse('"\\u{10FFFF}"'), '\u{10FFFF}')
    assert.equal(j.parse('"\\u{41}B"'), 'AB')

    // Invalid forms produce invalid_unicode, never a raw RangeError.
    for (const bad of [
      '"\\u{110000}"', '"\\u{}"', '"\\u{GG}"', '"\\u{1234567}"', '"\\u{41"',
      '"\\uZZZZ"',
    ]) {
      try {
        j.parse(bad)
        assert.fail(bad + ' should error')
      } catch (e) {
        assert.equal(e.code, 'invalid_unicode', bad + ' -> ' + e.message)
      }
    }
  })

  it('multibyte-content', () => {
    const j = new Tabnas({ plugins: [json] })
    assert.deepEqual(j.parse('{"é":"中"}'), { é: '中' })
    assert.deepEqual(j.parse('{"😀":"🎈"}'), { '😀': '🎈' })
    assert.deepEqual(j.parse('["é","中","😀","𝄞","☃"]'), [
      'é', '中', '😀', '𝄞', '☃',
    ])
    assert.equal(j.parse('"héllo wörld"'), 'héllo wörld')
  })

  it('configured-chars', () => {
    // NBSP (U+00A0) and ideographic space (U+3000) as space chars —
    // the fallback class function handles any char code >= 256.
    const j = new Tabnas({ plugins: [json] })
    j.options({ space: { chars: ' \t 　' } })
    assert.deepEqual(j.parse('{"a": 1,　"b": 2}'), {
      a: 1,
      b: 2,
    })

    // U+2028 LINE SEPARATOR as a line + row char: error rows count it.
    const j2 = new Tabnas({ plugins: [json] })
    j2.options({ line: { chars: '\r\n ', rowChars: '\n ' } })
    assert.deepEqual(j2.parse('{"a":1, "b":2}'), { a: 1, b: 2 })
    try {
      j2.parse('{"a":1, "b": "x')
      assert.fail('should error')
    } catch (e) {
      assert.equal(e.lineNumber, 3, 'U+2028 should advance the row count')
    }

    // Curly double quote (U+201C) as a string delimiter.
    const j3 = new Tabnas({ plugins: [json] })
    j3.options({ string: { chars: '"“' } })
    assert.equal(j3.parse('“hello world“'), 'hello world')
  })
})
