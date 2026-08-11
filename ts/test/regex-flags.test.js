/* Copyright (c) 2026 tabnas, MIT License */
'use strict'

/* regex-flags.test.js — the cross-runtime encoding of regex terminals
 * that need JavaScript's `u` flag.
 *
 * The serialized `@/pattern/flags` form is shared between the runtimes and
 * carries JS flags, because this runtime writes them natively. The Go
 * runtime lowers them to RE2, where `u` is DROPPED: RE2 is natively
 * rune-based, which is exactly what `u` asks JavaScript to be.
 *
 * This file is the TypeScript half of the table in `go/regexflags_test.go`
 * — the same patterns, inputs and expected answers. It is what makes the
 * "no-op" claim a pinned agreement rather than an assertion about RE2:
 * if either engine's answer moves, one of the two suites goes red.
 *
 * The four shapes are the ones a real grammar emits. Only the first needs
 * no flag; an astral class, a NEGATED class and `.` all carry `u`, which
 * is why this is not a corner case.
 */

const { describe, it } = require('node:test')
const assert = require('node:assert')

// Keep in step with regexFlagCases in go/regexflags_test.go.
const CASES = [
  ['bmp class, no flag', /^[a-z]$/, 'q', true],
  ['bmp class rejects other', /^[a-z]$/, 'Q', false],
  ['bmp class, no flag, unaffected by u', /^[a-z]$/u, 'q', true],

  // An astral MEMBER. This runtime needs `u` for \u{...} at all.
  ['astral class', /^[\u{1F600}-\u{1F64F}]$/u, '😀', true],
  // 😐 is U+1F610 and IS inside the range; 🚀 is U+1F680 and is not.
  ['astral class accepts another member', /^[\u{1F600}-\u{1F64F}]$/u, '😐', true],
  ['astral class rejects outside', /^[\u{1F600}-\u{1F64F}]$/u, '🚀', false],

  // A negated class: the COMPLEMENT contains astral code points, so this
  // carries `u` even though nothing astral is written in it.
  ['negated class takes an astral char whole', /^[^\n]$/u, '😀', true],
  ['negated class is one char, not two', /^[^\n]{2}$/u, '😀', false],
  ['negated class rejects its exclusion', /^[^\n]$/u, '\n', false],

  // `.` — any character, astral included.
  ['dot takes an astral char whole', /^.$/u, '😀', true],
  ['dot{2} does NOT split one astral char', /^.{2}$/u, '😀', false],
  ['dot{2} takes two astral chars', /^.{2}$/u, '😀😀', true],
  ['dot is still one BMP char', /^.$/u, 'q', true],
]

describe('regex-flags', () => {

  it('the shared table answers the same here as in Go', () => {
    for (const [name, re, input, want] of CASES) {
      assert.equal(re.test(input), want, `${name}: ${re} against ${JSON.stringify(input)}`)
    }
  })

  // Why the flag is needed HERE, and so why it has to survive
  // serialization at all. Each of these is the same pattern without `u`,
  // answering differently — which is the bug the Go side must not
  // reintroduce by dropping the flag AND being code-unit based.
  it('without u this runtime answers differently', () => {
    // `.{2}` accepting a single astral character as two: the real bug
    // this pins, corrected here and not to be reintroduced in Go.
    assert.equal(/^.{2}$/.test('😀'), true, 'no-u: dot{2} splits an astral char')
    assert.equal(/^.{2}$/u.test('😀'), false, 'u: dot{2} does not')

    assert.equal(/^.$/.test('😀'), false, 'no-u: dot is one code UNIT')
    assert.equal(/^[^\n]$/.test('😀'), false, 'no-u: negated class is one code unit')

    // And the astral class does not even compile without it.
    assert.throws(() => new RegExp('^[\\u{1F600}-\\u{1F64F}]$'), SyntaxError)
  })

})
