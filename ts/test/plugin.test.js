/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Dedicated unit tests for the plugin surface, mirroring the Go
// jsonic/plugin_test.go cases so both runtimes exercise the same
// mechanics: use()/plugins-array invocation and ordering, plugin-author
// defaults, custom fixed tokens, rule modification, custom lexer matchers
// (including priority ordering), and event subscription. Grammar-level
// behaviour lives in the json fixture and csv-grammar tests; this file
// stays focused on the plugin entry points themselves.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

describe('plugin', () => {
  describe('use', () => {
    it('invokes-plugin-with-options', () => {
      let invoked = false
      let seen = null
      const tn = new Tabnas()
      tn.use((tn, opts) => {
        invoked = true
        seen = opts
      }, { key: 'value' })
      assert.equal(invoked, true)
      assert.deepEqual(seen, { key: 'value' })
    })

    it('requires-a-function', () => {
      const tn = new Tabnas()
      assert.throws(() => tn.use(123), /must be a function/)
    })

    it('applies-plugins-in-order', () => {
      const order = []
      new Tabnas({
        plugins: [() => order.push('first'), () => order.push('second')],
      })
      assert.deepEqual(order, ['first', 'second'])
    })

    it('use-chains-in-order', () => {
      const order = []
      const tn = new Tabnas()
      tn.use(() => order.push('a'))
      tn.use(() => order.push('b'))
      assert.deepEqual(order, ['a', 'b'])
    })

    it('returns-plugin-wrapped-instance', () => {
      const tn = new Tabnas({ plugins: [json] })
      // A plugin can return a proxy; use() returns whatever it returns.
      // Methods must bind to the target so ES #private state resolves.
      const wrapped = tn.use((tn) =>
        new Proxy(tn, {
          get(target, prop) {
            const v = target[prop]
            return 'function' === typeof v ? v.bind(target) : v
          },
        }),
      )
      assert.notEqual(wrapped, tn)
      assert.deepEqual(wrapped.parse('[1,2]'), [1, 2])
    })

    it('merges-plugin-author-defaults', () => {
      let seen = null
      const myPlugin = (tn, opts) => {
        seen = opts
      }
      // Author defaults merge under the caller's options.
      myPlugin.defaults = { sep: ',', trim: true }
      new Tabnas().use(myPlugin, { trim: false })
      assert.deepEqual(seen, { sep: ',', trim: false })
    })
  })

  describe('tokens', () => {
    it('registers-a-new-fixed-token', () => {
      const tn = new Tabnas()
      const tin = tn.token('#QQ')
      assert.equal(typeof tin, 'number')
      assert.equal(tn.token.QQ, tin)
      assert.equal(tn.token('#QQ'), tin)
    })

    it('rebinds-comma-to-tilde-as-separator', () => {
      // Reusing the #CA token id for '~' makes tilde a separator.
      const tn = new Tabnas({ plugins: [json] })
      tn.use((tn) => tn.options({ fixed: { token: { '#CA': '~' } } }))
      assert.deepEqual(tn.parse('[1 ~ 2 ~ 3]'), [1, 2, 3])
    })

    it('adds-a-new-token-used-in-a-rule', () => {
      // A custom '~' token drives a val-rule alternate that yields 42.
      const tn = new Tabnas({ plugins: [json] })
      tn.use((tn) => {
        tn.options({ fixed: { token: { '#TL': '~' } } })
        const TL = tn.token('#TL')
        tn.rule('val', (rs) => {
          rs.open([{ s: [TL], a: (rule) => (rule.node = 42) }])
        })
      })
      assert.equal(tn.parse('~'), 42)
    })
  })

  describe('rule-modification', () => {
    it('uppercases-strings-via-after-close', () => {
      const tn = new Tabnas({ plugins: [json] })
      tn.rule('val', (rs) =>
        rs.ac((r) => {
          if ('string' === typeof r.node) r.node = r.node.toUpperCase()
        }),
      )
      assert.deepEqual(tn.parse('["hello","World"]'), ['HELLO', 'WORLD'])
    })

    it('adds-an-alternate-that-pushes-a-new-rule', () => {
      const tn = new Tabnas({ plugins: [json] })
      tn.use((tn) => {
        tn.options({ fixed: { token: { '#TH': 'H' } } })
        const TH = tn.token('#TH')
        tn.rule('hundred', (rs) => rs.ao((r) => (r.node = 100)))
        tn.rule('val', (rs) => rs.open([{ s: [TH], p: 'hundred' }]))
      })
      assert.equal(tn.parse('H'), 100)
    })
  })

  describe('custom-matchers', () => {
    // Build a custom lexer matcher under lex.match. `order` controls
    // priority; built-ins run match=1e6 … text=8e6, so an order below
    // 1e6 fires before every built-in.
    const matchPlugin = (name, order, fn) => (tn) =>
      tn.options({ lex: { match: { [name]: { order, make: () => fn } } } })

    it('matches-a-bareword-value-token', () => {
      const tn = new Tabnas({ plugins: [json] })
      tn.use(
        matchPlugin('dollar', 1.5e6, (lex) => {
          const pnt = lex.pnt
          if (pnt.sI + 2 <= pnt.len && '$$' === lex.src.substr(pnt.sI, 2)) {
            const tkn = lex.token('#VL', 'DOLLAR', '$$', pnt)
            pnt.sI += 2
            pnt.cI += 2
            return tkn
          }
          return undefined
        }),
      )
      assert.equal(tn.parse('$$'), 'DOLLAR')
      assert.deepEqual(tn.parse('{"a":$$}'), { a: 'DOLLAR' })
    })

    it('runs-early-matchers-before-built-ins', () => {
      // An early matcher sees '4' before the number matcher consumes it.
      let earlySaw = false
      const tn = new Tabnas({ plugins: [json] })
      tn.use(
        matchPlugin('early', 1e3, (lex) => {
          const pnt = lex.pnt
          if (pnt.sI < pnt.len && '4' === lex.src[pnt.sI]) earlySaw = true
          return undefined // pass through to the built-in number matcher
        }),
      )
      tn.parse('42')
      assert.equal(earlySaw, true)
    })

    it('lets-an-early-matcher-capture-before-the-number-matcher', () => {
      const tn = new Tabnas({ plugins: [json] })
      tn.use(
        matchPlugin('cap42', 1e3, (lex) => {
          const pnt = lex.pnt
          if (pnt.sI + 2 <= pnt.len && '42' === lex.src.substr(pnt.sI, 2)) {
            const tkn = lex.token('#VL', 'FORTY_TWO', '42', pnt)
            pnt.sI += 2
            pnt.cI += 2
            return tkn
          }
          return undefined
        }),
      )
      assert.equal(tn.parse('42'), 'FORTY_TWO')
    })
  })

  describe('subscription', () => {
    it('observes-lexed-tokens-and-processed-rules', () => {
      const tn = new Tabnas({ plugins: [json] })
      const lexed = []
      const ruled = []
      tn.sub({ lex: (tkn) => lexed.push(tkn.src) })
      // A second sub call extends the existing subscriber lists.
      tn.sub({ rule: (rule) => ruled.push(rule.name) })
      tn.parse('{"a":1}')
      assert.ok(lexed.includes('{'))
      assert.ok(ruled.includes('val'))
    })
  })
})

describe('plugin application', () => {
  it('applies a plugin once per use, even if it calls options()', () => {
    // A plugin whose body calls tn.options() must not be re-applied by
    // the options() setter (TS #setOptions does not re-run plugins; only
    // make()/derive does). Parity guard with the Go engine.
    const { Tabnas } = require('..')
    let runs = 0
    const tn = new Tabnas()
    tn.use((tn) => {
      runs++
      tn.options({ tag: 'x' })
    })
    assert.equal(runs, 1, 'plugin should run exactly once per use')
    tn.options({ tag: 'y' })
    assert.equal(runs, 1, 'options() must not re-run the plugin')
  })
})
