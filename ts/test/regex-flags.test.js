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

const { Tabnas } = require('..')

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

// --- The two non-equivalences the flag translation does NOT fix -------
//
// `\s` and `(?i)` mean different things in RE2 and in JavaScript, with or
// without `u`, so a shared `@/…/` terminal that uses either matches a
// different LANGUAGE in each runtime. That was recorded in
// go/doc/differences.md as a porting note; it is a DIVERGENCE — same
// input, different parse result — and it now lives in DIVERGENCE.md with
// this pin and its Go twin. Audit item P8.
//
// Verified at the PARSE level, through the shared serialized door, not at
// the regex-engine layer. The engine-layer difference was already known;
// what was never wired was a demonstration that it reaches a grammar. It
// does, and it goes BOTH ways: `\s` makes this port the permissive one
// and `(?i)` makes Go's.
//
// go/regexflags_test.go asserts the OPPOSITE answers over the same table.
// That pairing is the test.
//
// NOT in this table: U+2028. It is a line terminator in the JavaScript
// regex dialect AND, separately, in this port's text matcher, so its row
// would move when the unrelated text-ender repair lands. `\s`
// disagreement is shown by four other characters that carry no such
// second meaning.

describe('regex-terminal-dialect', () => {
  const SPACE_SPEC = '{"options":{"value":{"def":{"spacey":{"match":"@/^\\\\s+$/"}}}}}'
  const KAY_SPEC = '{"options":{"value":{"def":{"kay":{"match":"@/^k$/i"}}}}}'

  // Accepts exactly one #VL. A source the terminal does not claim
  // arrives as #TX and is rejected, so accept/reject IS the answer.
  const grammar = (spec) => {
    const tn = new Tabnas({ rule: { start: 'top', exclude: 'tabnas,imp' } })
    tn.grammar(JSON.parse(spec))
    tn.rule('top', (rs) => rs
      .open([{ s: ['#VL'], a: (r) => { r.node = r.o0.val } }])
      .close([{ s: ['#ZZ'] }]))
    return tn
  }
  const accepts = (spec, src) => {
    try {
      grammar(spec).parse(src)
      return true
    } catch (e) {
      return false
    }
  }

  it('a shared `\\s` or `(?i)` terminal answers differently than Go', () => {
    const cases = [
      // `\s` is Unicode-aware here and ASCII-only in RE2
      // ([\t\n\f\r ]). Go rejects every row below.
      ['NBSP U+00A0', SPACE_SPEC, '\u00a0', true],
      ['U+2000 EN QUAD', SPACE_SPEC, '\u2000', true],
      ['U+3000 IDEOGRAPHIC SPACE', SPACE_SPEC, '\u3000', true],
      ['U+FEFF BOM', SPACE_SPEC, '\ufeff', true],

      // Control: a character neither dialect calls whitespace. Without
      // it, "this port accepts everything" would pass.
      ['plain text', SPACE_SPEC, 'abc', false],

      // `(?i)` case-folds by Unicode rules in RE2, which is JS `iu` and
      // not JS `i`. This is the direction where GO is the permissive
      // one — `/^k$/i` does not match the Kelvin sign here.
      ['U+212A KELVIN SIGN', KAY_SPEC, '\u212a', false],

      // Controls: the rows both dialects agree on.
      ['k', KAY_SPEC, 'k', true],
      ['K', KAY_SPEC, 'K', true],
    ]
    for (const [label, spec, src, want] of cases) {
      assert.equal(accepts(spec, src), want, label)
    }
  })

  it('a serialized value.def takes a LITERAL val', () => {
    // JSON has no functions, so a serialized `value.def` entry can only
    // carry a literal `val`; `@REF` reaches only names the host
    // registered. This port called `val` unconditionally, so a string
    // threw a TypeError that the matcher loop turned into a #BD bad
    // token — reporting `unexpected` on VALID input, naming the
    // character rather than the option. Go has always taken both shapes
    // (`ValueDef.Val` alongside `ValFunc`), so it built the same spec
    // and parsed it. Audit item P10.
    const spec =
      '{"options":{"value":{"def":{"kay":{"match":"@/^k$/i","val":"KAY"}}}}}'
    assert.equal(grammar(spec).parse('k'), 'KAY')

    // A function `val` still receives the match array, which is the
    // shape a host-registered @REF supplies.
    const fn = new Tabnas({ rule: { start: 'top', exclude: 'tabnas,imp' } })
    fn.options({
      value: { def: { kay: { match: /^k$/i, val: (res) => 'fn:' + res[0] } } },
    })
    fn.rule('top', (rs) => rs
      .open([{ s: ['#VL'], a: (r) => { r.node = r.o0.val } }])
      .close([{ s: ['#ZZ'] }]))
    assert.equal(fn.parse('k'), 'fn:k')
  })
})
