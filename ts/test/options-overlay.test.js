/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// The options overlay merges index-wise for slices, recurses into a map
// of definitions, and merges tokenSet index-wise onto the default set.
// These are TypeScript's own semantics, ruled as the contract for every
// runtime (#151) after Go was found to replace all three wholesale. The
// same overlays are asserted in go/options_overlay_test.go through the
// options pipeline, never through deep() directly, where the runtimes
// already agreed.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

describe('options-overlay', () => {
  it('tokenSet merges index-wise onto the default set', () => {
    for (const tn of [
      new Tabnas({ tokenSet: { IGNORE: ['#SP'] } }),
      new Tabnas().options({ tokenSet: { IGNORE: ['#SP'] } }) && new Tabnas(),
    ]) {
      assert.deepStrictEqual(tn.options.tokenSet.IGNORE, ['#SP', '#LN', '#CM'])
    }
    const shrunk = new Tabnas({ tokenSet: { IGNORE: ['#SP', null, null] } })
    assert.deepStrictEqual(shrunk.internal().config.tokenSet.IGNORE.length, 1)

    // Through a serialized grammar: the rule reads #TX then #LN, which
    // only matches once #LN is removed from the IGNORE set.
    for (const [set, ok] of [[['#SP'], false], [['#SP', null, null], true]]) {
      const tn = new Tabnas()
      tn.grammar({
        clear: true,
        options: { rule: { start: 'top' }, tokenSet: { IGNORE: set } },
        rule: { top: { open: [{ s: ['#TX', '#LN'] }], close: [{ s: ['#ZZ'] }] } },
      })
      let parsed = true
      try { tn.parse('a\n') } catch (e) { parsed = false }
      assert.equal(parsed, ok, 'IGNORE ' + JSON.stringify(set))
    }
  })

  it('comment.def recurses into the entry and keeps the others', () => {
    const tn = new Tabnas({ comment: { def: { hash: { start: '%' } } } })
    const def = tn.options.comment.def
    assert.equal(def.hash.line, true)
    assert.equal(def.hash.start, '%')
    assert.equal(def.slash.start, '//')
    assert.equal(def.multi.start, '/*')
    assert.doesNotThrow(() => tn.parse('% c\n'))
    const removed = new Tabnas({ comment: { def: { hash: null } } })
    assert.equal(removed.options.comment.def.hash, null)
  })

  it('value.def keeps the built-in keywords when one is added', () => {
    const tn = new Tabnas({ value: { def: { yes: { val: 1 } } } })
    assert.deepStrictEqual(Object.keys(tn.options.value.def).sort(),
      ['false', 'null', 'true', 'yes'])
  })

  it('slices merge index-wise: an overlay index wins, the rest keep the base', () => {
    const tn = new Tabnas({ ender: ['x', 'y'] })
    tn.options({ ender: ['z'] })
    assert.deepStrictEqual(tn.options.ender, ['z', 'y'])
    tn.options({ result: { fail: ['a', 'b'] } })
    tn.options({ result: { fail: ['c'] } })
    assert.deepStrictEqual(tn.options.result.fail, ['c', 'b'])
  })
})
