/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// `consumed` is engine state fixed before the alt action runs (#122).
// It used to be computed twice, straddling the action, from two reads
// of alt.b, which the action is handed and could write: an action that
// wrote alt.b made the two disagree, and a token already moved to ctx.v
// was never shifted out of ctx.t. Go always computed it once, before
// the action; that is now the contract in every runtime.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

describe('alt-consumed', () => {
  it('an action writing alt.b does not change what the pass consumed', () => {
    const tn = new Tabnas({
      rule: { start: 'top' },
      fixed: { token: { '#A': 'a', '#B': 'b' } },
    })
    let closed = false
    tn.rule('top', (rs) =>
      rs
        .open([{ s: ['#A', '#B'], a: (r, ctx, alt) => { alt.b = 1 } }])
        .close([{ s: ['#ZZ'], a: () => { closed = true } }]),
    )
    // Both tokens were consumed before the action ran; the write to
    // alt.b must not leave #B sitting in the lookahead for the close
    // pass, which only accepts the end and would otherwise fail.
    assert.doesNotThrow(() => tn.parse('ab'))
    assert.ok(closed)
  })

  it('the numeric and function forms of b consume the same count', () => {
    for (const b of [1, () => 1]) {
      const tn = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { '#A': 'a', '#B': 'b' } },
      })
      let retained = -1
      tn.rule('top', (rs) =>
        rs
          .open([{ s: ['#A', '#B'], b, a: (r, ctx) => { retained = ctx.v.length } }])
          .close([{ s: ['#B'] }, { s: ['#ZZ'] }]),
      )
      tn.parse('ab')
      assert.equal(retained, 1)
    }
  })
})
