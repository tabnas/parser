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

  it('string errors point at the construct, and agree with Go', () => {
    // This replaces a divergence pin. Go used to leave the lex point at
    // the opening quote and span from it, so every error from its string
    // matcher reported 1:1 and carried the whole string-so-far as its
    // token src. Repaired by mirroring what this port does — move the
    // point onto the construct before raising. Swept 19 inputs across
    // the family: 0 diverge.
    //
    // Kept as a PARITY test rather than deleted: the point-moving is easy
    // to drop when editing either matcher, and dropping it is silent —
    // the codes stay right and only the positions move.
    // go/divergence_test.go TestStringErrorsPointAtTheConstruct asserts
    // the same inputs.
    const LF = String.fromCharCode(10)
    for (const [src, why, cI, tsrc] of [
      // Escape errors sit on the BACKSLASH and span the escape.
      ['"\\uZZZZ"', 'invalid_unicode', 2, '\\uZZZZ'],
      ['"\\xZZ"', 'invalid_ascii', 2, '\\xZZ'],
      ['"\\u{GG}"', 'invalid_unicode', 2, '\\u{GG}'],
      // An unknown escape sits on the escape CHARACTER.
      ['"\\q"', 'unexpected', 3, 'q'],
      // A control char sits on the character itself.
      ['"a' + LF + 'b"', 'unprintable', 3, LF],
      // A truncated escape at EOF spans the partial digits too.
      ['"\\x4', 'invalid_ascii', 2, '\\x4'],
      ['"\\u41', 'invalid_unicode', 2, '\\u41'],
      ['"\\u{42', 'invalid_unicode', 2, '\\u{42'],
    ]) {
      const j = new Tabnas({ string: { allowUnknown: false } })
      const lex = makeLex({
        src: () => src,
        cfg: j.internal().config,
        opts: j.options,
        sub: {},
      })
      const t = lex.next()
      assert.equal(t.name, '#BD', src)
      assert.equal(t.why, why, src)
      assert.equal(t.cI, cI, src + ': the point must sit on the construct')
      assert.equal(t.src, tsrc, src + ': the span must cover the construct')
    }
  })

  it('string errors under options that change the escape set', () => {
    // The escape-removed and strict-\x branches raise from a different
    // place than the default unknown-escape branch. Reachable only under
    // these options, so a sweep over defaults cannot see them.
    // go/divergence_test.go asserts the same three.
    for (const [label, src, opts, cI, tsrc] of [
      ['strict disables \\x', '"\\x41"',
        { string: { escapeStrict: true, allowUnknown: false } }, 3, 'x'],
      ['escape map removes \\v', '"\\v"',
        { string: { escape: { v: '' }, allowUnknown: false } }, 3, 'v'],
      ['non-ASCII escape char', '"\\\u00e9"',
        { string: { allowUnknown: false } }, 3, '\u00e9'],
    ]) {
      const j = new Tabnas(opts)
      const lex = makeLex({
        src: () => src,
        cfg: j.internal().config,
        opts: j.options,
        sub: {},
      })
      const t = lex.next()
      assert.equal(t.name, '#BD', label)
      assert.equal(t.why, 'unexpected', label)
      assert.equal(t.cI, cI, label)
      assert.equal(t.src, tsrc, label)
    }
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

  it('reports `pos` in UTF-16 units, as Go reports it in runes', () => {
    // DIVERGENCE.md "Column positions for astral characters": `pos`
    // carries that divergence and nothing else, so it agrees with Go
    // throughout the BMP and differs only above it — exactly like `col`.
    //
    // This is the TypeScript half of go/divergence_test.go
    // TestDiagnosticPosCountsRunes, over the same four inputs. The pairing
    // is the test: Go emitted a BYTE offset until audit item P5, which
    // diverged for every character above U+007F while both this file's
    // subject and the schema described runes. A one-sided pin could not
    // tell "Go still counts runes" from "Go went back to bytes".
    //
    // `col` is asserted alongside `pos` because the claim is that the two
    // are now the same class; a repair that fixed one and not the other
    // would leave this green if only `pos` were checked.
    const diag = (src) => {
      const j = new Tabnas({ rule: { start: 'top', exclude: 'tabnas,imp' } })
      j.rule('top', (rs) => {
        rs.open({ s: [j.token('#ST')], a: (r) => { r.node = r.o0.val } })
        rs.close({ s: [j.token('#ZZ')] })
      })
      try {
        j.parse(src)
      } catch (e) {
        return JSON.parse(JSON.stringify(e))
      }
      assert.fail('no error for ' + JSON.stringify(src))
    }

    // src, pos, col — and the Go numbers for the same input in the
    // comment. Only the astral row differs.
    const cases = [
      ['"ab" 1', 5, 6], // pure ASCII      Go pos 5 col 6 — same
      ['"\u00e9" 1', 4, 5], // U+00E9: 2 bytes  Go pos 4 col 5 — same
      ['"\u20ac" 1', 4, 5], // U+20AC: 3 bytes  Go pos 4 col 5 — same
      ['"\u{1F600}" 1', 5, 6], // astral      Go pos 4 col 5 — DIVERGES
    ]
    for (const [src, pos, col] of cases) {
      const o = diag(src)
      assert.equal(o.pos, pos, 'pos ' + JSON.stringify(src))
      assert.equal(o.col, col, 'col ' + JSON.stringify(src))
    }

    // The divergence stated as a difference rather than as two numbers:
    // one astral character costs this port ONE more unit of pos than the
    // same character costs Go, for the same reason it costs one more
    // column. Go's half asserts 4 and 5 for these two rows.
    assert.equal(diag('"\u{1F600}" 1').pos - diag('"\u20ac" 1').pos, 1)
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

    // THE WORKAROUND, pinned rather than only written down. DIVERGENCE.md
    // recommends an explicit class instead of `\s`, and the recommendation is
    // worthless unless it works in BOTH runtimes: the first draft spelled it
    // the RE2 way (\x{00a0}), which is a SyntaxError HERE and installs fine
    // in Go. This is the same assertion as its Go counterpart — not the
    // opposite — because the whole point is that the two agree.
    const CLS =
      '[\\t\\n\\v\\f\\r \\u00a0\\u1680\\u2000-\\u200a' +
      '\\u2028\\u2029\\u202f\\u205f\\u3000\\ufeff]+'
    const SPEC_CLS = {
      options: { rule: { start: 'top' }, match: { token: { '#WS': '@/^' + CLS + '/' } } },
      rule: { top: { open: [{ s: ['#WS'], a: '@value$' }], close: [{}] } },
    }
    for (const cp of [0x20, 0x09, 0x00A0, 0x2028, 0x2000, 0x3000, 0xFEFF]) {
      const ch = String.fromCharCode(cp)
      assert.equal(
        run(SPEC_CLS, ch), 'ACCEPTED:' + ch,
        `workaround class U+${cp.toString(16)}: the class DIVERGENCE.md ` +
        'recommends must work in both runtimes',
      )
    }
    assert.equal(run(SPEC_CLS, 'A'), 'REJECTED', 'workaround class "A"')

    // A HARSHER KIND OF DIVERGENCE: not a different result, but a grammar
    // that will not load at all. RE2 implements neither lookahead nor
    // backreferences, so these install and match here and are refused at
    // install time in Go.
    for (const [name, pattern, src] of [
      ['lookahead', '(?=x)x', 'x'],
      ['backreference', '(a)\\1', 'aa'],
    ]) {
      const spec = {
        options: { rule: { start: 'top' }, match: { token: { '#WS': '@/^' + pattern + '/' } } },
        rule: { top: { open: [{ s: ['#WS'], a: '@value$' }], close: [{}] } },
      }
      assert.equal(
        run(spec, src), 'ACCEPTED:' + src,
        `${name} (${pattern}) must install and match here — Go reports an ` +
        'install error. If TS refuses it too the divergence is GONE',
      )
    }
  })

})
