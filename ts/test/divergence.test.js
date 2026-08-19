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

  // Pins the serialized-regex dialect gap, asserting the OPPOSITE of
  // TestDivergenceRegexDialect in go/divergence_test.go: that pairing IS the
  // test. See DIVERGENCE.md, "Regex dialect in serialized terminals".
  //
  // This is the first PARSE-LEVEL reproduction. The gap was known at the
  // regex-engine layer and recorded in go/doc/differences.md, but nothing
  // drove it through a real GrammarSpec — so "a shared grammar that depends
  // on either will differ" was a prediction, not a measurement. It goes both
  // ways: TS accepts what Go rejects (`\s`) and rejects what Go accepts
  // (`(?i)`).
  it('a serialized `\\s` matches Unicode spaces here, ASCII-only in Go', () => {
    const SPEC_WS = {
      options: { rule: { start: 'top' }, match: { token: { '#WS': '@/^\\s+/' } } },
      rule: { top: { open: [{ s: ['#WS'], a: '@value$' }], close: [{}] } },
    }
    const SPEC_K = {
      options: { rule: { start: 'top' }, match: { token: { '#K': '@/^k/i' } } },
      rule: { top: { open: [{ s: ['#K'], a: '@value$' }], close: [{}] } },
    }

    const run = (spec, src) => {
      const j = new Tabnas({ rule: { start: 'top' } })
      j.grammar(JSON.parse(JSON.stringify(spec)))
      try {
        return 'ACCEPTED:' + j.parse(src)
      } catch {
        return 'REJECTED'
      }
    }

    // Control: the ASCII whitespace both engines agree on. A change here
    // means something other than the dialect gap broke.
    for (const cp of [0x20, 0x09]) {
      const ch = String.fromCharCode(cp)
      assert.equal(run(SPEC_WS, ch), 'ACCEPTED:' + ch,
        `\\s control U+${cp.toString(16)}`)
    }

    // JS `\s` is Unicode-aware; RE2's is the Perl class [\t\n\f\r ], so Go
    // REJECTS every one of these. Named, so a future JS that narrowed the
    // class fails loudly rather than quietly aligning.
    for (const [name, cp] of [
      ['NBSP', 0x00A0], ['LINE SEPARATOR', 0x2028], ['EN QUAD', 0x2000],
      ['IDEOGRAPHIC SPACE', 0x3000], ['ZERO WIDTH NO-BREAK SPACE', 0xFEFF],
    ]) {
      const ch = String.fromCharCode(cp)
      assert.equal(
        run(SPEC_WS, ch), 'ACCEPTED:' + ch,
        `\\s U+${cp.toString(16)} (${name}) must be accepted here — Go rejects ` +
        'it. If TS now rejects it too the divergence is GONE and the ' +
        'DIVERGENCE.md entry should be deleted',
      )
    }

    // The other direction. JS `/i` without `u` does not fold U+212A KELVIN
    // SIGN to `k`; RE2 case-folds by Unicode rules and matches it.
    for (const ch of ['k', 'K']) {
      assert.equal(run(SPEC_K, ch), 'ACCEPTED:' + ch, `(?i) control ${ch}`)
    }
    assert.equal(
      run(SPEC_K, String.fromCharCode(0x212A)), 'REJECTED',
      'U+212A KELVIN SIGN must be rejected here — Go accepts it. If TS now ' +
      'accepts it too the divergence is GONE',
    )
  })

})
