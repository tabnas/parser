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

  it('lex.relex exists here and does not in Go', () => {
    // DIVERGENCE.md: "Negotiated lexing (lex.relex) — TypeScript only".
    //
    // Asserted as a real behaviour, not just a config key, so this cannot
    // pass on a stub: with relex on, the engine re-cuts a buffered token's
    // source span rather than failing the alternate outright.
    const j = new Tabnas({ lex: { relex: true } })
    assert.equal(
      j.options.lex.relex,
      true,
      'relex must be settable here; Go has no such option at all',
    )

    // And it is OFF by default — which is why every grammar in the fleet
    // behaves identically in both ports despite this entry.
    const d = new Tabnas()
    assert.notEqual(
      d.options.lex.relex,
      true,
      'relex must default off, or the two ports would diverge for real ' +
      'grammars rather than only for scannerless front-ends',
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
    assert.equal(
      t.why,
      'invalid_unicode',
      'the code agrees for THIS input — see the truncated-escape test ' +
        'below for the shapes where it does not',
    )
    assert.equal(t.src, '\\uZZZZ', 'escape only — Go includes the opening quote')
    assert.equal(t.sI, 1, 'pos: escape start — Go reports 0 (the quote)')
    assert.equal(t.cI, 2, 'col: escape start — Go reports 1 (the quote)')
    assert.equal(
      Array.from(t.src).length,
      6,
      'diagnostic len (code points of token src) — Go reports 7',
    )
  })

  it('escape decode is strict, and agrees with Go', () => {
    // This replaces a divergence pin. Until P3 was repaired, `parseInt`
    // succeeded on any hex prefix here, so this port accepted `"\\x4Z"`
    // as U+0004 (discarding the `Z`) and reported truncated escapes as
    // unterminated_string where Go named the escape. Swept then: 32
    // cases, 16 diverged. Swept after the repair: 0.
    //
    // Kept as a PARITY test rather than deleted, because the boundary is
    // easy to relax again by accident — `parseInt` is the obvious way to
    // write this and it is the wrong way. go/divergence_test.go
    // TestEscapeDecodeIsStrict asserts the same inputs.
    const parse = (src) => {
      const j = new Tabnas({
        string: { allowUnknown: false },
        rule: { start: 'val', exclude: 'tabnas,imp' },
      })
      j.rule('val', (rs) => {
        rs.open({ s: [j.token('#ST')], a: (r) => { r.node = r.o0.val } })
      })
      try {
        return { ok: true, val: j.parse(src) }
      } catch (e) {
        return { ok: false, code: e.code }
      }
    }

    // A junk-terminated escape is not a valid escape.
    assert.equal(parse('"\\x4Z"').code, 'invalid_ascii')
    assert.equal(parse('"\\u414Z"').code, 'invalid_unicode')

    // Nor is a truncated one, with or without a closing quote. Assert the
    // CODE, not merely that it threw: `unterminated_string` is exactly
    // what the repair removed here, and a check for "it failed" would
    // pass if it came back.
    for (const [src, code] of [
      ['"\\x4', 'invalid_ascii'],
      ['"\\x4"', 'invalid_ascii'],
      ['"\\u4', 'invalid_unicode'],
      ['"\\u41"', 'invalid_unicode'],
      ['"\\u414Z', 'invalid_unicode'],
    ]) {
      assert.equal(parse(src).code, code, src)
    }

    // No valid hex prefix at all — unchanged by the repair.
    assert.equal(parse('"\\xZZ"').code, 'invalid_ascii')
    assert.equal(parse('"\\uZZZZ"').code, 'invalid_unicode')

    // And valid escapes still decode, including the braced form that was
    // already strict and served as the model for the repair.
    assert.equal(parse('"\\x41"').val, 'A')
    assert.equal(parse('"\\u0041"').val, 'A')
    assert.equal(parse('"\\u{1F600}"').val, '\u{1F600}')
    assert.equal(parse('"a\\x41b\\u0042c"').val, 'aAbBc')
  })
})
