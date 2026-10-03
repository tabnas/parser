/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Shared contract with rs/tests/rule_history_test.rs and
// go/rule_history_test.go: a rule-history bound cuts every retention path,
// not only the direct prev chain, without changing the parsed value.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { MAX_RULE_HISTORY, Tabnas } = require('..')
const jsonSpec = require('./json-builder.fixture.json')

const clone = (value) => JSON.parse(JSON.stringify(value))

function jsonParser(history) {
  const parser = new Tabnas({ rule: { start: 'val', history } })
  parser.grammar(clone(jsonSpec))
  return parser
}

function flatArray(items) {
  return '[' + Array.from({ length: items }, (_, index) => index).join(',') + ']'
}

// The longest prev chain and the largest snapshot graph a live rule can
// reach while parsing. Identity, rather than a blank name, identifies the
// per-parse NORULE sentinel.
function reach(parser, src) {
  let longest = 0
  let largest = 0
  parser.sub({
    rule: (rule, ctx) => {
      let chain = 0
      let prev = rule.prev
      while (prev !== ctx.NORULE) {
        chain++
        prev = prev.prev
      }
      longest = Math.max(longest, chain)

      const seen = new Set()
      // Include the original root retained by ctx.root(): measuring only the
      // current rule misses an obsolete forward replacement chain rooted
      // there.
      const pending = [
        ctx.root(), rule.parent, rule.child, rule.prev, rule.next,
      ]
      while (0 < pending.length) {
        const snapshot = pending.pop()
        if (snapshot === ctx.NORULE || seen.has(snapshot)) continue
        seen.add(snapshot)
        pending.push(
          snapshot.parent, snapshot.child, snapshot.prev, snapshot.next,
        )
      }
      largest = Math.max(largest, seen.size)
    },
  })
  const value = parser.parse(src)
  return { longest, largest, value }
}

describe('rule history', () => {
  it('validates and exports the supported bound', () => {
    assert.equal(MAX_RULE_HISTORY, 16)
    for (const history of [1, 3, MAX_RULE_HISTORY, null, false]) {
      for (const install of [
        () => new Tabnas({ rule: { history } }),
        () => new Tabnas().options({ rule: { history } }),
        () => new Tabnas().grammar({ options: { rule: { history } } }),
      ]) {
        assert.doesNotThrow(install, String(history))
      }
    }
    for (const history of [0, -3, 2.5, true, '3']) {
      assert.throws(
        () => new Tabnas({ rule: { history } }),
        /options\.rule\.history must be an integer of at least 1, null, or false/,
        String(history),
      )
    }
    assert.throws(
      () => new Tabnas({ rule: { history: MAX_RULE_HISTORY + 1 } }),
      /options\.rule\.history is outside the supported range \(at most 16\)/,
    )
  })

  it('lets null and false reset an existing bound to unbounded', () => {
    for (const history of [null, false]) {
      const parser = new Tabnas({ rule: { history: 3 } })
      parser.options({ rule: { history } })
      assert.equal(parser.options.rule.history, history)
      const measured = reach(jsonParser(history), flatArray(500))
      assert.ok(100 < measured.longest, String(history))
    }
  })

  it('bounds a 10,000-item sequence without changing its value', () => {
    const src = flatArray(10_000)
    const bounded = reach(jsonParser(3), src)
    assert.equal(bounded.longest, 3)
    assert.ok(bounded.largest <= 32, `bounded reach: ${bounded.largest}`)
    assert.equal(JSON.stringify(bounded.value), src)

    // The same constant graph is retained by a shorter sequence.
    const shorter = reach(jsonParser(3), flatArray(500))
    assert.equal(shorter.longest, 3)
    assert.equal(bounded.largest, shorter.largest)
  })

  it('history one retains only the rule it replaced', () => {
    const bounded = reach(jsonParser(1), flatArray(500))
    assert.equal(bounded.longest, 1)
    assert.ok(bounded.largest <= 16, `bounded reach: ${bounded.largest}`)
  })

  it('refreshes the live pusher without changing the frozen parent view', () => {
    const parser = new Tabnas({
      rule: { start: 'top', history: 1 },
      fixed: { token: { '#A': 'a', '#B': 'b', '#C': 'c' } },
    })
    let frozenCounter
    let completed
    parser.rule('top', (rs) => rs
      .open([{ s: '#A', p: 'child' }])
      .close([{ s: '#C', a: (rule) => {
        completed = rule.child
        rule.node = 'ok'
      } }]))
    parser.rule('child', (rs) => rs.open([{
      s: '#B', n: { done: 1 }, r: 'tail',
    }]))
    parser.rule('tail', (rs) => rs.open([{ a: (rule) => {
      frozenCounter = rule.parent.child.n.done
    } }]))

    assert.equal(parser.parse('abc'), 'ok')
    assert.equal(frozenCounter, undefined)
    assert.equal(completed.name, 'child')
    assert.equal(completed.state, 'c')
    assert.equal(completed.n.done, 1)
    assert.equal(completed.o[0].src, 'b')
  })

  it('republishes copied child state after ruleDone subscribers', () => {
    const parser = new Tabnas({
      rule: { start: 'top', history: 1 },
      fixed: { token: { '#A': 'a', '#B': 'b', '#C': 'c' } },
    })
    parser.rule('top', (rs) => rs
      .open([{ s: '#A', p: 'child' }])
      .close([{ s: '#C', a: (rule) => {
        rule.node = rule.child.u.after
      } }]))
    parser.rule('child', (rs) => rs
      .open([{ s: '#B' }])
      .close([{}]))
    parser.sub({
      ruleDone: (rule, _ctx, done) => {
        if ('child' === rule.name && 'c' === done.state) {
          rule.u.after = 'after'
        }
      },
    })

    assert.equal(parser.parse('abc'), 'after')
  })

  it('keeps original-root result semantics when history is bounded', () => {
    const parse = (history) => {
      const parser = new Tabnas({
        rule: { start: 'top', history },
        fixed: { token: { '#A': 'a' } },
      })
      parser.rule('top', (rs) => rs.open([{
        s: '#A', r: 'tail', a: (rule) => { rule.node = 'old' },
      }]))
      parser.rule('tail', (rs) => rs.open([{
        a: (rule) => { rule.node = 'new' },
      }]))
      return parser.parse('a')
    }

    assert.equal(parse(null), 'old')
    assert.equal(parse(1), 'old')
  })

  it('merges on the effective history setting', () => {
    const left = new Tabnas({ tag: 'L', rule: { history: 3 } })
    const right = new Tabnas({ tag: 'R' })
    assert.equal(left.merge(right).options.rule.history, 3)
    assert.equal(right.merge(left).options.rule.history, 3)
    const disabled = new Tabnas({ tag: 'U', rule: { history: false } })
    assert.equal(left.merge(disabled).options.rule.history, 3)
    assert.equal(disabled.merge(left).options.rule.history, 3)
    const unset = new Tabnas({ tag: 'N', rule: { history: null } })
    assert.equal(disabled.merge(unset).options.rule.history, null)
    assert.throws(
      () => left.merge(new Tabnas({ tag: 'R', rule: { history: 4 } })),
      /conflicting option values at rule\.history/,
    )
  })
})
