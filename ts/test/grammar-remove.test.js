/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

// A GrammarSpec can remove as well as add: `rule[name] = null` prunes one
// rule, and `clear: true` wipes the instance's grammar entirely. Both are
// declarative, so a serialized spec expresses them and tn.grammar(spec)
// and any plugin that installs one agree about what the spec means.
describe('grammar-remove', () => {
  it('removes a single rule', () => {
    const tn = new Tabnas({ plugins: [json] })
    assert.deepEqual(
      Object.keys(tn.rule()), ['val', 'map', 'list', 'pair', 'elem'])

    tn.grammar({ rule: { list: null } })

    assert.deepEqual(Object.keys(tn.rule()), ['val', 'map', 'pair', 'elem'])
    assert.deepEqual(tn.parse('{"a":1}'), { a: 1 })
    assert.throws(() => tn.parse('[1,2]'), (e) => 'unknown_rule' === e.code)
  })

  it('removing an absent rule is a no-op', () => {
    // So a spec can prune defensively without knowing what is installed.
    const tn = new Tabnas({ plugins: [json] })
    tn.grammar({ rule: { nosuchrule: null } })
    assert.deepEqual(tn.parse('{"a":1}'), { a: 1 })
  })

  it('clear wipes rules and fixed tokens', () => {
    const tn = new Tabnas({ plugins: [json] })
    assert.ok(0 < Object.keys(tn.rule()).length)
    assert.ok(0 < Object.keys(tn.internal().config.fixed.token).length)

    tn.grammar({ clear: true })

    assert.deepEqual(Object.keys(tn.rule()), [])
    assert.deepEqual(tn.internal().config.fixed.token, {})
  })

  it('clear leaves the lexer matchers alone', () => {
    // Matchers are not grammar. A cleared instance still lexes numbers
    // and strings — without them there would be no tokens to write the
    // replacement rules against, and a grammar cannot put them back.
    // Observed through a minimal replacement grammar, because a wiped
    // instance has no start rule and so never reaches the lexer.
    const tn = new Tabnas({ plugins: [json] })
    tn.grammar({
      clear: true,
      options: { rule: { start: 'top' } },
      rule: {
        top: {
          open: [
            { s: '#NR', a: (r) => { r.node = ['number', r.o[0].val] } },
            { s: '#ST', a: (r) => { r.node = ['string', r.o[0].val] } },
          ],
        },
      },
    })

    assert.deepEqual(tn.parse('42'), ['number', 42])
    assert.deepEqual(tn.parse('"s"'), ['string', 's'])
  })

  it('clears and rebuilds in one spec', () => {
    const tn = new Tabnas({ plugins: [json] })
    tn.grammar({
      clear: true,
      options: {
        fixed: { token: { '#HI': 'hi' } },
        rule: { start: 'top' },
      },
      rule: {
        top: { open: [{ s: '#HI', a: (r) => { r.node = 'world' } }] },
      },
    })

    assert.deepEqual(Object.keys(tn.rule()), ['top'])
    assert.equal(tn.parse('hi'), 'world')
    // The JSON grammar really is gone.
    assert.throws(() => tn.parse('{"a":1}'))
  })
})
