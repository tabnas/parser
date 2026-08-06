/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Counter comparison semantics: an UNSET counter reads as 0.
//
// These helpers used to short-circuit to true whenever the counter was unset,
// which made `lt(k,n)` and `gt(k,n)` BOTH true — a predicate and its own
// negation. That broke trichotomy and made the natural "refuse once past the
// limit" guard fire on the very first token, before anything was counted.
// Reading an unset counter as 0 keeps the permissive direction (`lt` still
// passes when nothing has been counted) while making the strict direction
// honest. `exist()` remains the way to ask whether a counter was set at all.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

// Run `probe(rule)` inside a real parse, with counter `x` set to 1.
function probe(fn, counters = { x: 1 }) {
  let j = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
  let seen = null
  j.rule('top', (rs) =>
    rs
      .open([{ s: [j.token.Ta], n: counters, a: (r) => { seen = fn(r) } }])
      .close([{ s: ['#ZZ'] }]))
  j.parse('a')
  return seen
}

describe('counter-comparisons', () => {

  it('an unset counter reads as 0, not "true against everything"', () => {
    const s = probe((r) => ({
      eq0: r.eq('nope', 0), eq9: r.eq('nope', 9),
      lt1: r.lt('nope', 1), lt0: r.lt('nope', 0), ltneg: r.lt('nope', -1),
      gtneg: r.gt('nope', -1), gt0: r.gt('nope', 0), gt9: r.gt('nope', 9),
      lte0: r.lte('nope', 0), lteneg: r.lte('nope', -1),
      gte0: r.gte('nope', 0), gte9: r.gte('nope', 9),
    }))
    assert.equal(s.eq0, true)
    assert.equal(s.eq9, false)
    assert.equal(s.lt1, true)      // 0 < 1
    assert.equal(s.lt0, false)
    assert.equal(s.ltneg, false)
    assert.equal(s.gtneg, true)    // 0 > -1
    assert.equal(s.gt0, false)
    assert.equal(s.gt9, false)     // the trap: this used to be true
    assert.equal(s.lte0, true)
    assert.equal(s.lteneg, false)
    assert.equal(s.gte0, true)
    assert.equal(s.gte9, false)
  })

  it('trichotomy holds: exactly one of <, =, > is true', () => {
    const counts = probe((r) => {
      const out = []
      for (const counter of ['x', 'unset']) {
        for (const limit of [-1, 0, 1, 2, 3]) {
          out.push([counter, limit,
            [r.lt(counter, limit), r.eq(counter, limit), r.gt(counter, limit)]
              .filter(Boolean).length])
        }
      }
      return out
    })
    for (const [counter, limit, hits] of counts) {
      assert.equal(hits, 1,
        `${counter} vs ${limit}: ${hits} of (lt,eq,gt) true, want exactly 1`)
    }
  })

  it('a set counter is unaffected', () => {
    const s = probe((r) => ({
      eq: r.eq('x', 1), lt2: r.lt('x', 2), lt1: r.lt('x', 1),
      gt0: r.gt('x', 0), gt1: r.gt('x', 1),
    }))
    assert.deepEqual(s, { eq: true, lt2: true, lt1: false, gt0: true, gt1: false })
  })

  it('exist distinguishes never-counted from counted-zero', () => {
    const s = probe((r) => ({
      zero: r.exist('zero'), never: r.exist('never'),
      eqzero: r.eq('zero', 0), eqnever: r.eq('never', 0),
    }), { zero: 0 })
    assert.equal(s.zero, true)
    assert.equal(s.never, false)
    // Both compare equal to 0 — which is exactly why exist() is needed.
    assert.equal(s.eqzero, true)
    assert.equal(s.eqnever, true)
  })

  it('declarative $exist actually gates the alternate', () => {
    // $exist was implemented in makeRuleCond but missing from COND_OPS, so it
    // was skipped as an unknown operator — leaving `conds` empty, deleting
    // `c`, and making the alternate UNCONDITIONAL. It matched everything,
    // which is the opposite of a guard, and it was the documented migration
    // path for the counter change.
    const run = (cd, counters) => {
      let j = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
      let fired = false
      j.rule('top', (rs) =>
        rs
          .open([{ s: [j.token.Ta], n: counters, c: cd, a: () => { fired = true } }])
          .close([{ s: ['#ZZ'] }]))
      try { j.parse('a') } catch (e) { }
      return fired
    }

    // Counter never set.
    assert.equal(run({ 'n.never': { $exist: true } }, { x: 1 }), false)
    assert.equal(run({ 'n.never': { $exist: false } }, { x: 1 }), true)

    // A counter set to 0 — the case the comparisons cannot distinguish from
    // unset. `n` is applied when an alternate is ACCEPTED, while `c` is
    // evaluated while matching it, so the counter has to be set by an earlier
    // alternate (here: open) and read by a later one (close).
    const closeRun = (cd) => {
      let j = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
      let fired = false
      j.rule('top', (rs) =>
        rs
          .open([{ s: [j.token.Ta], n: { zero: 0 } }])
          .close([{ s: ['#ZZ'], c: cd, a: () => { fired = true } }, { s: ['#ZZ'] }]))
      try { j.parse('a') } catch (e) { }
      return fired
    }
    assert.equal(closeRun({ 'n.zero': { $exist: true } }), true)
    assert.equal(closeRun({ 'n.zero': { $exist: false } }), false)
    // ...and the comparisons genuinely cannot tell it from unset.
    assert.equal(closeRun({ 'n.zero': { $eq: 0 } }), true)
    assert.equal(closeRun({ 'n.never': { $eq: 0 } }), true)
  })

  it('a declarative $gte guard no longer fires before anything is counted', () => {
    // Written guard-first, this used to reject at the opening token because
    // the counter was unset there and $gte passed unconditionally.
    const s = probe((r) => ({
      gteUnset: r.gte('nope', 2),   // 0 >= 2
      gteSet: r.gte('x', 2),        // 1 >= 2
    }))
    assert.equal(s.gteUnset, false)
    assert.equal(s.gteSet, false)
  })

})
