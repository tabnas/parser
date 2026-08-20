/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */
'use strict'

// What ends a text run, and what does NOT.
//
// The answer is supposed to be `cfg.line.chars` (plus space chars, ender
// chars, fixed tokens and comment starters) and nothing else. It was not:
// the TypeScript text matcher built its ender regex without the `s` flag
// whenever line lexing was on, which made the JS regex dialect's own
// line-terminator set — U+000A, U+000D, U+2028, U+2029 — a second,
// unconfigurable ender. Audit item P4.
//
// The two sets are not the same, and the gap was reachable in BOTH
// directions:
//
//   - U+2028 and U+2029 ended a text run although no option named them.
//     TypeScript could not lex `a<U+2028>b` at all — it produced `#BD"a"`
//     where Go produced `#TX"a<U+2028>b"`.
//   - After `line.chars` was set to something without a newline, a
//     newline still ended the run, and since it was no longer an ender
//     char there was nothing to end it WITH: `#BD"a"` again.
//
// go/text_ender_test.go asserts the same eight cases with the same
// expected spans. That pairing is the test. Neither half can tell
// "the other port agrees" from "the other port was quietly changed",
// and this defect was invisible in the shared fixtures precisely
// because no fixture used a non-ASCII separator or a custom
// `line.chars`.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeLex } = require('..')

const LS = '\u2028' // LINE SEPARATOR
const PS = '\u2029' // PARAGRAPH SEPARATOR

describe('text-ender', () => {
  // The sources of the #TX tokens, in order. Only text spans are
  // compared: Go's lexer does not emit the #LN token TypeScript emits
  // between them, which is a separate and long-standing shape
  // difference, not the property under test here.
  const textSpans = (opts, src) => {
    const j = new Tabnas(opts)
    const lex = makeLex({
      src: () => src,
      cfg: j.internal().config,
      opts: j.options,
      sub: {},
    })
    const out = []
    for (let i = 0; i < 12; i++) {
      const t = lex.next()
      if (!t) break
      if ('#TX' === t.name) out.push(t.src)
      // A bad token is the failure shape this repairs, so surface it
      // rather than letting the loop end on it silently.
      if ('#BD' === t.name) out.push('#BD:' + t.src)
      if ('#ZZ' === t.name || '#BD' === t.name) break
    }
    return out
  }

  it('ends a text run at the configured line chars, and only those', () => {
    const cases = [
      // REPAIRED. No option names U+2028 or U+2029, so neither ends a
      // text run. TypeScript produced `#BD:a` for both before P4.
      ['LS is not an ender', {}, 'a' + LS + 'b', ['a' + LS + 'b']],
      ['PS is not an ender', {}, 'a' + PS + 'b', ['a' + PS + 'b']],

      // REPAIRED. `line.chars` no longer contains a newline, so a
      // newline is ordinary text. `#BD:a` before P4.
      ['LF after line.chars is retargeted',
        { line: { chars: ';' } }, 'a\nb', ['a\nb']],

      // CONTROLS. Each of these already agreed with Go, and each would
      // catch the repair going too far — dropping the ender-char class
      // instead of the dialect's extra set.
      ['a configured line char still ends the run', {}, 'a\nb', ['a', 'b']],
      ['and so does the other default one', {}, 'a\rb', ['a', 'b']],
      ['a retargeted line char ends the run',
        { line: { chars: ';' } }, 'a;b', ['a', 'b']],
      ['line lexing off means nothing line-ish ends it',
        { line: { lex: false } }, 'a\nb', ['a\nb']],

      // CONTROL, and the point of the whole change: naming U+2028 makes
      // it an ender, exactly as naming `;` does. ts/test/utf8.test.js
      // relies on this same configuration for row counting.
      ['naming LS makes it an ender after all',
        { line: { chars: '\r\n' + LS, rowChars: '\n' + LS } },
        'a' + LS + 'b', ['a', 'b']],
    ]

    for (const [label, opts, src, want] of cases) {
      assert.deepEqual(textSpans(opts, src), want, label)
    }
  })

  it('does not queue a fixed token the point never reached', () => {
    // A CONSUMING value regexp matches against the forward source, not
    // against `msrc`, so it may take a shorter prefix and leave the lex
    // point mid-run. `subMatchFixed` builds its token at the POINT while
    // `tsrc` was found at the end of `msrc` — two different offsets. So
    // it fabricated a delimiter where the source had something else,
    // swallowed that something, and left the real delimiter to be
    // emitted a second time.
    //
    // Found by review on the P4 change and reproduced on `main`: it
    // predates P4 and is not caused by it. P4 widens the reach, since
    // `msrc` can now span a line terminator, which is why the fix lands
    // in the same change. Audit item P9.
    //
    // go/lexer.go returns straight after a consuming value match and
    // re-enters the matcher, so it never had this. Every "want" below is
    // Go's stream, measured, and go/text_ender_test.go asserts the same
    // table.
    const spans = (src, opts) => {
      const j = new Tabnas(Object.assign({
        value: {
          def: {
            at: { match: /^@\w+/, consume: true, val: (res) => ({ at: res[0] }) },
          },
        },
      }, opts))
      const lex = makeLex({
        src: () => src,
        cfg: j.internal().config,
        opts: j.options,
        sub: {},
      })
      const out = []
      for (let i = 0; i < 14; i++) {
        const t = lex.next()
        if (!t) break
        // #SP is skipped: Go emits no space token here, a separate and
        // long-standing shape difference.
        if ('#SP' !== t.name) out.push(t.name + ':' + t.src)
        if ('#ZZ' === t.name || '#BD' === t.name) break
      }
      return out
    }

    // The regexp takes `@abc`; the ender is the comma, further on. The
    // characters in between must survive as text.
    assert.deepEqual(spans('@abc-rest,'),
      ['#VL:@abc', '#TX:-rest', '#CA:,', '#ZZ:'])
    assert.deepEqual(spans('@abc-r,x'),
      ['#VL:@abc', '#TX:-r', '#CA:,', '#TX:x', '#ZZ:'])
    assert.deepEqual(spans('@abc--,'),
      ['#VL:@abc', '#TX:--', '#CA:,', '#ZZ:'])

    // The P4 widening: with `s` on, `msrc` can span a line terminator,
    // so the gap the old code swallowed could be a newline.
    assert.deepEqual(spans('@abc\nrest,', { line: { chars: ';' } }),
      ['#VL:@abc', '#TX:\nrest', '#CA:,', '#ZZ:'])
    assert.deepEqual(spans('@abc' + LS + 'rest,'),
      ['#VL:@abc', '#TX:' + LS + 'rest', '#CA:,', '#ZZ:'])

    // Controls. When the point DOES reach the ender, the fixed token is
    // still queued in the same call — the guard must not disable it.
    assert.deepEqual(spans('@abc,rest'),
      ['#VL:@abc', '#CA:,', '#TX:rest', '#ZZ:'])
    assert.deepEqual(spans('@abc rest,'),
      ['#VL:@abc', '#TX:rest', '#CA:,', '#ZZ:'])
  })
})
