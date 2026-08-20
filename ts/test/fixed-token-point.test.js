/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Audit item P9: a fixed token must not be queued at a point the lexer
// never reached.
//
// A CONSUMING value regexp (`value.def` with `consume: true`) matches
// against the forward source rather than against the text run `msrc`, so
// it may take a shorter prefix and leave the lex point mid-run.
// `subMatchFixed` then builds its token AT THE POINT, while `tsrc` was
// found at the END of `msrc` — two different offsets whenever the regexp
// stopped short. It fabricated a delimiter where the source had
// something else, consumed that something, and left the real delimiter
// to be emitted a second time:
//
//   value.def {at: {match: /^@\w+/, consume: true}}, src `@abc-rest,`
//     was:  #VL"@abc"  #CA","@4  #TX"rest"  #CA","@9   (the `-` is gone)
//     now:  #VL"@abc"  #TX"-rest"  #CA","@9
//
// `go/lexer.go` returns straight after a consuming value match and
// re-enters the matcher, so the following fixed token is found at a point
// that actually exists — that port never had this. Every `want` below is
// Go's stream, measured, and `go/fixed_token_point_test.go` asserts the
// same table.
//
// Found by review on the P4 change (parser#140) and reproduced on `main`
// with no line terminator involved, so it predates P4 and is independent
// of how P4 is settled. The two P4-dependent rows from that PR are NOT
// here on purpose — what a text run does at a line terminator is P4's
// question, and pinning it here would couple this file to that decision.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeLex } = require('..')

describe('fixed-token-point', () => {
  it('does not queue a fixed token the point never reached', () => {
    const spans = (src) => {
      const j = new Tabnas({
        value: {
          def: {
            at: { match: /^@\w+/, consume: true, val: (res) => ({ at: res[0] }) },
          },
        },
      })
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

    // Controls. When the point DOES reach the ender, the fixed token is
    // still queued in the same call — the guard must not disable it. A
    // repair that dropped subMatchFixed outright would pass the three
    // rows above and fail these two.
    assert.deepEqual(spans('@abc,rest'),
      ['#VL:@abc', '#CA:,', '#TX:rest', '#ZZ:'])
    assert.deepEqual(spans('@abc rest,'),
      ['#VL:@abc', '#TX:rest', '#CA:,', '#ZZ:'])
  })
})
