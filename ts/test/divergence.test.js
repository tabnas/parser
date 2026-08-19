/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */
'use strict'

// One test per entry in DIVERGENCE.md, the record of deliberate TS/Go
// non-parity. Mirrors go/divergence_test.go, which asserts the OPPOSITE
// where the ports differ — that pairing IS the test. A divergence pinned
// on only one side is half an assertion: it cannot tell "the other port
// still differs" from "the other port was quietly changed".
//
// WHY EVERY PERMITTED DIVERGENCE NEEDS A TEST
//
// A divergence that is written down but not executed can move in either
// direction unnoticed — regress further, or be FIXED and leave the
// document lying. Both have happened here: jsonic's prose claimed `2.e3`
// and `1e999` still diverged after they had been aligned, and claimed
// base-prefixed overflow was aligned before it was. That is what moved
// jsonic to an executable ledger.
//
// If one of these fails, do not adjust the test to match. Either the
// divergence moved and DIVERGENCE.md is now wrong, or it was resolved and
// the entry should be deleted.
//
// Lone surrogates — the third entry — are pinned in
// surrogate-pairing.test.js rather than duplicated here, next to the
// pairing cases they are easily confused with.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeLex } = require('..')

describe('divergence', () => {
  it('astral characters advance the column by TWO here, one in Go', () => {
    // DIVERGENCE.md: "Error columns count UTF-16 units in TypeScript (an
    // astral character is 2) and runes in Go (any character is 1)."
    //
    // Measured on the token AFTER the character. The ports emit different
    // token sequences for the same source (Go emits no #SP here), so this
    // finds the `y` token by source rather than by index — comparing "the
    // third token" would compare different tokens in each port.
    const colOfY = (src) => {
      const j = new Tabnas()
      const lex = makeLex({
        src: () => src,
        cfg: j.internal().config,
        opts: j.options,
        sub: {},
      })
      for (let i = 0; i < 6; i++) {
        const t = lex.next()
        if (!t || '#ZZ' === t.name) break
        if ('y' === t.src) return t.cI
      }
      assert.fail('no `y` token in ' + JSON.stringify(src))
    }

    // Control: one ASCII char then a space. Both ports agree, so a change
    // here means something other than the divergence broke.
    assert.equal(colOfY('x y'), 3, 'ascii control')

    // The divergence: an astral character is TWO UTF-16 units, so `y`
    // lands in column 4. Go counts runes and reports 3.
    assert.equal(
      colOfY('\u{1F600} y'),
      4,
      'astral: TS counts UTF-16 units; Go reports 3 — see DIVERGENCE.md',
    )
  })

  // NOT a divergence. Negotiated lexing is PORTED, and this pair of tests
  // used to say otherwise on both sides while agreeing with neither.
  //
  // This test was titled 'lex.relex exists here and does not in Go' and
  // cited DIVERGENCE.md: "Negotiated lexing (lex.relex) — TypeScript
  // only". That entry does not exist — `grep -in relex DIVERGENCE.md`
  // returns nothing — and Go's LexConfig has carried Relex for some time.
  // Its Go mirror had already been rewritten to assert the field IS
  // present; only its header comment still described the old absence
  // assertion. So the two halves asserted opposite things, and this half
  // asserted nothing about Go at all, which is why it kept passing: it is
  // exactly the failure both files' headers warn about — a divergence
  // pinned on one side is half an assertion.
  //
  // Kept rather than deleted, because the two ports agreeing is worth an
  // assertion of its own: relex is settable in both and OFF by default in
  // both, which is why every grammar in the fleet behaves identically
  // despite the feature existing. Measured 2026-08-19, both ports.
  //
  // Go mirror: go/divergence_test.go TestRelexIsPortedAndDefaultsOff.
  it('lex.relex is ported, and defaults off, in both ports', () => {
    const j = new Tabnas({ lex: { relex: true } })
    assert.equal(
      j.options.lex.relex,
      true,
      'relex must be settable here, as it is in Go (Options.Lex.Relex)',
    )

    const d = new Tabnas()
    assert.equal(
      d.options.lex.relex,
      false,
      'relex must default OFF, as it does in Go. If either port changes ' +
      'this default, the two diverge for every grammar in the fleet ' +
      'rather than only for scannerless front-ends',
    )
  })

  it('bad-escape token spans the ESCAPE here, quote-to-escape in Go', () => {
    // DIVERGENCE.md: "Bad-token spans for invalid string escapes". This
    // port's string matcher reports the offending escape sequence itself;
    // Go reports the string from its opening quote up to the escape. Same
    // code (the contract), different span — so the structured diagnostic's
    // len/pos/col diverge on this path even for pure-ASCII input.
    // go/divergence_test.go TestDivergenceBadEscapeSpanIncludesQuote
    // asserts the OPPOSITE values on purpose.
    const j = new Tabnas()
    const lex = makeLex({
      src: () => '"\\uZZZZ"',
      cfg: j.internal().config,
      opts: j.options,
      sub: {},
    })
    const t = lex.next()
    assert.equal(t.name, '#BD')
    assert.equal(t.why, 'invalid_unicode', 'the code is shared — only the span diverges')
    assert.equal(t.src, '\\uZZZZ', 'escape only — Go includes the opening quote')
    assert.equal(t.sI, 1, 'pos: escape start — Go reports 0 (the quote)')
    assert.equal(t.cI, 2, 'col: escape start — Go reports 1 (the quote)')
    assert.equal(
      Array.from(t.src).length,
      6,
      'diagnostic len (code points of token src) — Go reports 7',
    )
  })
})
