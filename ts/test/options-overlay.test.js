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

  // The twin of TestMatchValueOverlayOnTheSettingPath (#237): options()
  // rebuilds match.value from the merged options, so repeated calls keep
  // one matcher and a replaced or removed value stops matching. Go carried
  // the live matchers forward on top of the rebuilt ones.
  it('match.value is rebuilt, not carried forward, by options()', () => {
    const hexVal = (m) => 'HEX:' + m[0]
    const make = () => {
      const tn = new Tabnas({
        rule: { start: 'top' },
        match: { value: { hex: { match: /^0x[0-9a-f]+/, val: hexVal } } },
      })
      const { ZZ } = tn.token
      const { VAL } = tn.tokenSet
      tn.rule('top', (rs) =>
        rs.open([{ s: [VAL], a: (r) => (r.node = r.o0.val) }]).close([{ s: [ZZ] }]))
      return tn
    }

    const repeated = make()
    repeated.options({})
    repeated.options({})
    repeated.options({})
    assert.equal(repeated.parse('0xff'), 'HEX:0xff')

    const replaced = make()
    replaced.options({ match: { value: { hex: { match: /^0x[0-9]+/, val: hexVal } } } })
    assert.equal(replaced.parse('0xff'), 255)

    const removed = make()
    removed.options({ match: { value: { hex: null } } })
    assert.equal(removed.parse('0xff'), 255)
  })

  it('text.modify is rebuilt, not carried forward, by options()', () => {
    const tn = new Tabnas({ text: { modify: (v) => v + '!' } })
    tn.grammar({
      options: { rule: { start: 'top' } },
      rule: { top: { open: [{ s: '#TX', a: '@value$' }] } },
    })
    tn.options({})
    tn.options({})
    tn.options({})

    // One modifier, however many calls rebuilt the config: configure()
    // once concatenated onto the list it had, so each call added the
    // modifiers again, and a modifier that is not idempotent ran once more
    // per text token (#243).
    assert.equal(tn.internal().config.text.modify.length, 1)
    assert.equal(tn.parse('abc'), 'abc!')

    // A call that names text.modify leaves the merged options holding what
    // it passed, and the config is built from that alone.
    tn.options({ text: { modify: (v) => '[' + v + ']' } })
    assert.equal(tn.internal().config.text.modify.length, 1)
    assert.equal(tn.parse('abc'), '[abc]')
  })
})
