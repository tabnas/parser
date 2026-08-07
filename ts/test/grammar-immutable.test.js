/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// `grammar()` must not modify the spec it is given.
//
// It used to. Normalisation is destructive by design — `normalt` rewrites
// `g` into a sorted array and nulls an empty `s`, and `resolveFunctionRef`
// swaps each `@name$` string for the resolved function — and all of it ran
// directly on the caller's object. A declarative grammar came out full of
// functions, so serialising *after* installing produced a spec with every
// action missing. That spec still installed and still parsed; it just
// returned `undefined`. The failure was silent, which is what made it
// expensive.
//
// Go never had the bug: `Grammar` leaves the caller's *GrammarSpec alone.
// These tests pin the TS side to the same contract.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

// A grammar with every ref-carrying shape: a ref array (`a`), a single ref
// string (`a`), an empty `s`, and an unsorted comma-string `g`.
const makeSpec = () => ({
  options: {
    fixed: { token: { '#EQ': '=' } },
    rule: { start: 'pair' },
  },
  rule: {
    pair: {
      open: [{ s: '#TX #EQ', p: 'val', a: ['@object$', '@key$'], g: 'zulu,alpha' }],
      close: [{ a: '@setval$' }],
    },
    val: {
      open: [{ s: '#NR', a: '@value$' }],
      close: [{}],
    },
  },
})

describe('grammar-immutable', () => {

  it('leaves the caller spec byte-identical', () => {
    const spec = makeSpec()
    const before = JSON.stringify(spec)

    new Tabnas().grammar(spec)

    assert.equal(JSON.stringify(spec), before,
      'grammar() modified the spec it was given')
  })

  it('keeps ref strings as strings', () => {
    const spec = makeSpec()
    new Tabnas().grammar(spec)

    assert.deepEqual(spec.rule.pair.open[0].a, ['@object$', '@key$'])
    assert.equal(spec.rule.pair.close[0].a, '@setval$')
    assert.equal(typeof spec.rule.val.open[0].a, 'string')
  })

  it('leaves g and s untouched', () => {
    const spec = makeSpec()
    new Tabnas().grammar(spec)

    // normalt sorts g into an array and nulls an empty s — on its own copy.
    assert.equal(spec.rule.pair.open[0].g, 'zulu,alpha')
    assert.deepEqual(spec.rule.val.close[0], {})
  })

  it('installs the same spec value twice with the same result', () => {
    const spec = makeSpec()

    const first = new Tabnas()
    first.grammar(spec)
    assert.deepEqual(first.parse('port = 8080'), { port: 8080 })

    // The bug: the second install saw a spec whose actions had been replaced
    // by resolved functions on the first pass, and quietly produced nothing.
    const second = new Tabnas()
    second.grammar(spec)
    assert.deepEqual(second.parse('port = 8080'), { port: 8080 })
  })

  it('round-trips through JSON after installing', () => {
    const spec = makeSpec()

    const tn = new Tabnas()
    tn.grammar(spec)
    assert.deepEqual(tn.parse('port = 8080'), { port: 8080 })

    // Serialise AFTER install — the order the docs used to warn against.
    const wire = JSON.stringify(spec)
    assert.ok(wire.includes('@object$'), 'refs were lost from the spec')

    const elsewhere = new Tabnas()
    elsewhere.grammar(JSON.parse(wire))
    assert.deepEqual(elsewhere.parse('port = 8080'), { port: 8080 })
  })

  it('still accepts real functions, by reference', () => {
    // The clone copies plain objects and arrays but must pass functions
    // through untouched — a spec may carry actual actions, not just refs.
    let called = 0
    const action = () => { called++ }

    const spec = makeSpec()
    spec.rule.val.open[0].a = action

    const tn = new Tabnas()
    tn.grammar(spec)
    tn.parse('port = 8080')

    assert.equal(spec.rule.val.open[0].a, action, 'function was copied, not referenced')
    assert.ok(0 < called, 'function action did not run')
  })

  // Lex matchers are the exception to "pass functions by reference".
  // configure() writes `matcher.tin$ = +tin` in place, so a shared matcher
  // is instance-specific state disguised as a plain value.
  describe('lex matchers are de-shared', () => {

    const RE_SRC = '^#[0-9a-f]{3}'
    const matcherSpec = (re) => ({
      options: { match: { token: { '#HX': re } }, rule: { start: 'val' } },
      rule: { val: { open: [{ s: '#HX', a: '@value$' }], close: [{}] } },
    })

    // Two engines that number tokens differently, sharing one spec.
    const twoEngines = (re) => {
      const a = new Tabnas()
      a.grammar(matcherSpec(re))
      const b = new Tabnas()
      b.token('#X1', 'zz1')
      b.token('#X2', 'zz2')
      b.token('#X3', 'zz3')
      b.grammar(matcherSpec(re))
      return [a, b]
    }

    const matcherOf = (tn, src) =>
      Object.values(tn.internal().config.match.token)
        .find((m) => m instanceof RegExp && m.source === src)

    it('does not annotate the caller RegExp', () => {
      const re = new RegExp(RE_SRC)
      twoEngines(re)
      assert.equal(re.tin$, undefined, 'tin$ was written onto the caller RegExp')
    })

    it('gives each engine its own matcher object', () => {
      const re = new RegExp(RE_SRC)
      const [a, b] = twoEngines(re)
      const ma = matcherOf(a, RE_SRC)
      const mb = matcherOf(b, RE_SRC)
      assert.ok(ma && mb, 'matcher missing from a config')
      assert.notEqual(ma, mb, 'both engines share one matcher object')
      assert.notEqual(ma.tin$, mb.tin$, 'expected different token numbers')
    })

    it('leaves the first engine parsing correctly after the second installs', () => {
      const re = new RegExp(RE_SRC)
      const [a, b] = twoEngines(re)
      // Before the fix, installing into b rewrote a's tin$, so a matched on
      // the wrong token and this returned undefined.
      assert.equal(a.parse('#1ab'), '#1ab')
      assert.equal(b.parse('#1ab'), '#1ab')
    })

    it('preserves the eager$ opt-out when cloning', () => {
      const re = new RegExp(RE_SRC)
      re.eager$ = true
      const [a] = twoEngines(re)
      assert.equal(matcherOf(a, RE_SRC).eager$, true)
    })

  })

})
