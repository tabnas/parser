/* Copyright (c) 2026 tabnas, MIT License */

// A base-prefixed run (0x…, 0o…, 0b…) is a COMPLETE integer. It ends at a
// registered fixed token, and no fraction or exponent tail may attach to it.
//
// WHY THIS FILE EXISTS, AND WHY IT LOOKS REDUNDANT
//
// The fraction and exponent groups used to sit outside the number regex's
// alternation, so they attached to base-prefixed runs as well as decimal
// ones. `0xFF.5` then produced the run `0xFF.5`, which coerces to NaN, so
// the matcher declined the whole span and the text matcher took `0xFF` as
// #TX — while Go, whose base-prefix branch carries no tail at all, read
// #NR("0xFF") and left `.5` behind. The two ports disagreed on the token
// stream for the same input.
//
// The follow character decides whether that is observable:
//
//   0xFF.5   fraction attaches -> NaN -> declined  -> #TX   DIVERGES
//   0xFF.a   `.a` cannot open a fraction, so the regex backtracks to
//            `0xFF` and `.` serves as the ender      -> #NR AGREES
//
// So a test that probes only `0xFF.x` or `0xFF.5x` passes whether or not
// the bug is present — which is exactly how this survived: a downstream
// corpus covered only agreeing shapes, and one port's stream was
// byte-identical before and after. Every prefix below therefore carries at
// least one DIVERGING follow character and at least one AGREEING one. If
// you trim this file, keep both halves or it stops testing anything.
//
// Mirrored by go/number_base_boundary_test.go over the same table.

const { test, describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeLex } = require('..')

const tn = new Tabnas()

// `.` as a fixed token is the whole point: it is the boundary the run must
// end at. Without it there is no boundary and nothing to disagree about.
const DOT = { fixed: { token: { '#DT': '.' } } }

// NAME("src") per token, which is what the two ports are compared on — the
// token STREAM, not just the decoded value. A divergence that changes only
// the split would be invisible in a value-only assertion.
function stream(src) {
  const j = tn.make(DOT)
  const lex = makeLex({
    src: () => src,
    cfg: j.internal().config,
    opts: j.options,
    sub: {},
  })
  const out = []
  for (;;) {
    const t = lex.next()
    if (!t || '#ZZ' === t.name || '#BD' === t.name) break
    out.push(t.name + '(' + JSON.stringify(t.src) + ')')
    if (20 < out.length) break
  }
  return out.join(' ')
}

describe('number-base-boundary', () => {
  it('base-prefixed run ends at a fixed token, diverging follows', () => {
    // These are the shapes that used to decline in TS and lex in Go.
    const rows = [
      ['0xFF.5', '#NR("0xFF") #DT(".") #NR("5")'],
      ['0xFF.0', '#NR("0xFF") #DT(".") #NR("0")'],
      ['0xFF.', '#NR("0xFF") #DT(".")'],
      ['0xFF.e5', '#NR("0xFF") #DT(".") #TX("e5")'],
      ['0xFF.5e3', '#NR("0xFF") #DT(".") #NR("5e3")'],
      ['0x1_F.5', '#NR("0x1_F") #DT(".") #NR("5")'],
      ['0xFF.5_0', '#NR("0xFF") #DT(".") #NR("5_0")'],

      ['0o17.5', '#NR("0o17") #DT(".") #NR("5")'],
      ['0o17.0', '#NR("0o17") #DT(".") #NR("0")'],
      ['0o17.', '#NR("0o17") #DT(".")'],
      ['0o17.e5', '#NR("0o17") #DT(".") #TX("e5")'],

      ['0b101.5', '#NR("0b101") #DT(".") #NR("5")'],
      ['0b101.0', '#NR("0b101") #DT(".") #NR("0")'],
      ['0b101.', '#NR("0b101") #DT(".")'],
      ['0b101.e5', '#NR("0b101") #DT(".") #TX("e5")'],
    ]
    for (const [src, want] of rows) {
      assert.equal(stream(src), want, 'diverging follow: ' + src)
    }
  })

  it('base-prefixed run ends at a fixed token, agreeing follows', () => {
    // These agreed even with the bug present, by regex backtracking rather
    // than by intent. They are here to pin that the fix did not change them,
    // NOT as evidence the fix works — on their own they prove nothing.
    const rows = [
      ['0xFF.a', '#NR("0xFF") #DT(".") #TX("a")'],
      ['0xFF.F', '#NR("0xFF") #DT(".") #TX("F")'],
      ['0xFF.x', '#NR("0xFF") #DT(".") #TX("x")'],
      ['0xFF.z', '#NR("0xFF") #DT(".") #TX("z")'],
      ['0xFF.5x', '#NR("0xFF") #DT(".") #TX("5x")'],
      ['0xFF.a5', '#NR("0xFF") #DT(".") #TX("a5")'],

      ['0o17.x', '#NR("0o17") #DT(".") #TX("x")'],
      ['0o17.5x', '#NR("0o17") #DT(".") #TX("5x")'],

      ['0b101.x', '#NR("0b101") #DT(".") #TX("x")'],
      ['0b101.5x', '#NR("0b101") #DT(".") #TX("5x")'],
    ]
    for (const [src, want] of rows) {
      assert.equal(stream(src), want, 'agreeing follow: ' + src)
    }
  })

  it('decimal fractions and exponents still carry their tail', () => {
    // The hazard of scoping the tail to the decimal alternative is scoping
    // it away entirely. `.` is a fixed token here, so if the fraction group
    // stopped applying to decimals these would split into three tokens.
    const rows = [
      ['1.5', '#NR("1.5")'],
      ['1.5e3', '#NR("1.5e3")'],
      ['00.5', '#NR("00.5")'],
      ['1.', '#NR("1.")'],
      ['1e3', '#NR("1e3")'],
      ['1_0.5', '#NR("1_0.5")'],
      ['-1.5', '#NR("-1.5")'],
      ['+1.5e-3', '#NR("+1.5e-3")'],
    ]
    for (const [src, want] of rows) {
      assert.equal(stream(src), want, 'decimal tail: ' + src)
    }
  })

  it('non-numbers around the boundary stay text', () => {
    // `0x` with no digits is not a number and must stay text in both ports;
    // so must a run that merely looks prefix-like.
    const rows = [
      ['0x.5', '#TX("0x") #DT(".") #NR("5")'],
      ['0z.5', '#TX("0z") #DT(".") #NR("5")'],
      ['x0.5', '#TX("x0") #DT(".") #NR("5")'],
      ['abc.def', '#TX("abc") #DT(".") #TX("def")'],
      ['0xFF', '#NR("0xFF")'],
    ]
    for (const [src, want] of rows) {
      assert.equal(stream(src), want, 'text boundary: ' + src)
    }
  })

  it('base-prefix marker letter is case-insensitive', () => {
    // Go's matchNumber accepts either case for the marker; TS accepted only
    // lowercase, so `0XFF` was #NR there and #TX here. Widened to match Go —
    // hex DIGITS were already case-insensitive in both ports (0xAB == 0xab),
    // so only the marker was asymmetric, and JS `Number()` already coerces
    // "0XFF" to 255.
    const rows = [
      ['0XFF', '#NR("0XFF")'],
      ['0O17', '#NR("0O17")'],
      ['0B101', '#NR("0B101")'],
      ['0Xff', '#NR("0Xff")'],
      // Case-insensitive marker must not weaken the boundary rule above.
      ['0XFF.5', '#NR("0XFF") #DT(".") #NR("5")'],
      ['0O17.5', '#NR("0O17") #DT(".") #NR("5")'],
      ['0B101.5', '#NR("0B101") #DT(".") #NR("5")'],
      // Still not a number without digits, in either case.
      ['0X.5', '#TX("0X") #DT(".") #NR("5")'],
    ]
    for (const [src, want] of rows) {
      assert.equal(stream(src), want, 'uppercase marker: ' + src)
    }

    // The decoded values must match the lowercase forms exactly.
    const j = tn.make(DOT)
    const lex = (s) => {
      const l = makeLex({
        src: () => s,
        cfg: j.internal().config,
        opts: j.options,
        sub: {},
      })
      return l.next().val
    }
    assert.equal(lex('0XFF'), lex('0xFF'))
    assert.equal(lex('0O17'), lex('0o17'))
    assert.equal(lex('0B101'), lex('0b101'))
  })

  it('declining a run does not fabricate elements', () => {
    // Guards the earlier fix in the same matcher: a run that matches the
    // shape but fails numeric conversion must decline the WHOLE span, so the
    // text matcher takes it intact. Emitting the ender while declining moved
    // the point INSIDE the unconsumed run, and `[0xFF.5]` parsed as
    // [[],"xFF.5"] — a fabricated empty element.
    assert.equal(stream('0b_,'), '#TX("0b_") #CA(",")')
  })
})
