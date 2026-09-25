/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// The first-token index (rules.ts, indexFirst / parse_alts) must be
// invisible: a rule with many alternates tries only those that can take
// the first token, and everything an author can observe stays as it
// was. Go pins the same cases in go/alt_index_test.go and Rust in
// rs/tests/alt_index_test.rs.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const tn = new Tabnas()


function make_norules(opts) {
  let j = tn.make(opts)
  let rns = j.rule()
  Object.keys(rns).map((rn) => j.rule(rn, null))
  return j
}

function instance(extra) {
  return make_norules({
    rule: { start: 'top' },
    fixed: { token: { '#A': 'a', '#B': 'b', '#C': 'c', ...(extra || {}) } },
  })
}


describe('alt-index', () => {

  it('keeps first-match-wins among alternates sharing a first token', () => {
    const j = instance()
    j.rule('top', (rs) => rs
      .open([
        { s: '#A', c: () => false, a: (r) => (r.node = 'first') },
        { s: '#B', a: (r) => (r.node = 'b') },
        { s: '#A', a: (r) => (r.node = 'second') },
        { s: '#A', a: (r) => (r.node = 'third') },
      ])
      .close([{ s: '#ZZ' }]))
    assert.equal(j.parse('a'), 'second')
    assert.equal(j.parse('b'), 'b')
    assert.throws(() => j.parse('c'), /unexpected/)
  })


  it('keeps wildcard and empty alternates in place', () => {
    const j = instance()
    j.rule('top', (rs) => rs
      .open([
        { s: '#A', a: (r) => (r.node = 'a') },
        { s: '#AA', a: (r) => (r.node = 'any') },
        { s: '#B', a: (r) => (r.node = 'never') },
      ])
      .close([{ s: '#ZZ' }]))
    assert.equal(j.parse('a'), 'a')
    assert.equal(j.parse('b'), 'any')
    assert.equal(j.parse('c'), 'any')

    // An empty sequence before the token alternates matches without
    // consuming, exactly as it did: it is a candidate for every tin.
    const k = instance()
    k.rule('top', (rs) => rs
      .open([
        { p: 'item' },
        { s: '#A', a: (r) => (r.node = 'direct') },
      ])
      .close([{ s: '#ZZ' }]))
    k.rule('item', (rs) => rs
      .open([{ s: '#A', a: (r) => (r.node = 'item') }])
      .close([{ s: '#ZZ', a: (r) => (r.parent.node = r.node) }]))
    assert.equal(k.parse('a'), 'item')
  })


  it('sees a token set overridden after the rule', () => {
    // tabnas/parser#217, in the form all three runtimes pin: a rule on
    // #KEY installed first, KEY narrowed to #TX afterwards. The index
    // is rebuilt when the rules are normalised for the new options.
    const j = new Tabnas()
    j.grammar({
      options: { rule: { start: 'top' } },
      rule: { top: { open: [{ s: '#KEY', a: '@value$' }], close: [{ s: '#ZZ' }] } },
    })
    j.grammar({ options: { tokenSet: { KEY: ['#TX', null, null, null] } } })
    assert.equal(j.parse('a'), 'a')
    for (const src of ['1', '"s"', 'true']) {
      assert.throws(() => j.parse(src), /unexpected/, src)
    }
  })


  it('handles many alternates', () => {
    const extra = {}
    for (let i = 0; i < 300; i++) extra['#K' + i] = 'k' + i
    const j = instance(extra)
    j.rule('top', (rs) => {
      const alts = []
      for (let i = 0; i < 300; i++) {
        alts.push({ s: '#K' + i, a: (r) => (r.node = i) })
      }
      return rs.open(alts).close([{ s: '#ZZ' }])
    })
    for (const n of [0, 1, 150, 299]) {
      assert.equal(j.parse('k' + n), n)
    }
    assert.throws(() => j.parse('a'), /unexpected/)
  })


  it('registers more alternates than a spread could pass', () => {
    // RuleSpec.add used to spread the new alternates into push/unshift,
    // which throws RangeError past the engine's argument limit; a
    // compiled grammar can hand over that many.
    const j = instance()
    j.rule('top', (rs) => {
      const alts = []
      for (let i = 0; i < 200000; i++) alts.push({ s: '#B' })
      alts.push({ s: '#A', a: (r) => (r.node = 'a') })
      rs.open(alts)
      rs.open([{ s: '#C', a: (r) => (r.node = 'c') }])
      return rs.close([{ s: '#ZZ' }])
    })
    assert.equal(j.parse('a'), 'a')
    assert.equal(j.parse('c'), 'c')
  })

})
