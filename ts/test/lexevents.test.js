/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Lex-event reconciliation: the LexSub stream is a trace of lexer
// activity, not a token list — re-serves and speculative recuts fire
// events too. The documented consumer contract is position-keyed
// last-write-wins: keep the latest event per source position. This
// suite pins the one case that used to break it: a speculative relex
// recut fired an event, and its undo (unrelex) did not, so the
// abandoned recut "won". The undo now re-announces the restored token.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

function make(relex, rules) {
  const tn = new Tabnas({
    lex: { relex },
    fixed: {
      token: { '#LONG': 'ab', '#SHORT': 'a', '#TB': 'b', '#TC': 'c' },
    },
  })
  const j = tn.make()
  for (const rn of Object.keys(j.rule())) j.rule(rn, null)
  for (const [name, def] of Object.entries(rules)) j.rule(name, def)
  j.options({ rule: { start: 'top' } })
  return j
}

// Reconstruction per the documented contract: newest event wins per
// position, and each kept token's span shadows older events inside
// [sI, sI + len).
function reconstruct(j) {
  const events = []
  j.sub({
    lex: (tkn) => {
      if (0 <= tkn.sI) events.push(tkn)
    },
  })
  const byPos = new Map()
  byPos.finish = () => {
    const out = new Map()
    for (let i = events.length - 1; 0 <= i; i--) {
      const t = events[i]
      let shadowed = false
      for (const kept of out.values()) {
        if (kept.sI <= t.sI && t.sI < kept.sI + Math.max(1, kept.len)) {
          shadowed = true
          break
        }
      }
      if (!shadowed && !out.has(t.sI)) out.set(t.sI, t)
    }
    return out
  }
  return byPos
}

describe('lexevents', () => {
  it('an abandoned recut is overwritten by the restored token', () => {
    // The first alternate recuts 'ab' -> #SHORT 'a', then fails at its
    // second position; the fallback takes the original #LONG cut. The
    // reconstruction must show #LONG at position 0 — the token the
    // parse proceeded with — not the abandoned #SHORT.
    const RULES = {
      top: (rs) =>
        rs
          .open([
            { s: ['#SHORT', '#TC'] },
            { s: ['#AA'] },
          ])
          .close([{ s: ['#ZZ'] }]),
    }
    const j = make(true, RULES)
    const LONG = j.token('#LONG')
    const byPos = reconstruct(j)
    j.parse('ab')
    const final = byPos.finish()
    const at0 = final.get(0)
    assert.ok(at0, 'token recorded at position 0')
    assert.equal(
      at0.tin,
      LONG,
      'restored #LONG wins over the abandoned #SHORT recut',
    )
    // Span shadowing: the stale event fired at position 1 during the
    // abandoned speculation is covered by the restored #LONG's span.
    assert.equal(undefined, final.get(1), 'interior stale event shadowed')
  })

  it('a committed recut stays the latest event at its position', () => {
    // Here the recut alternate succeeds — the reconstruction must show
    // the recut token, not the original cut.
    const RULES = {
      top: (rs) =>
        rs
          .open([
            { s: ['#SHORT', '#TB'] },
            { s: ['#AA'] },
          ])
          .close([{ s: ['#ZZ'] }]),
    }
    const j = make(true, RULES)
    const SHORT = j.token('#SHORT')
    const TB = j.token('#TB')
    const byPos = reconstruct(j)
    j.parse('ab')
    const final = byPos.finish()
    assert.equal(final.get(0) && final.get(0).tin, SHORT, 'committed recut wins')
    assert.equal(final.get(1) && final.get(1).tin, TB, 'following token kept')
  })
})
