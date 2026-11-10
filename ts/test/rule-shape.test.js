/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Every rule keeps fast properties. A rule's own `node` accessor is defined
// from one descriptor that every rule shares (`Rule`'s `#nodeProperty`). A
// getter and a setter made for each rule, as closures, sent every rule into
// V8's dictionary mode, so constructing one cost several times what it
// should, and the bounded rule history constructs several snapshots a step
// (tabnas/transduce#84). TypeScript only: Rust's and Go's rules are structs.

const { describe, it } = require('node:test')
const assert = require('node:assert')
const { execFileSync } = require('node:child_process')
const path = require('node:path')

const { Tabnas } = require('..')
const jsonSpec = require('./json-builder.fixture.json')

const clone = (value) => JSON.parse(JSON.stringify(value))

function jsonParser(history) {
  const parser = new Tabnas({ rule: { start: 'val', history } })
  parser.grammar(clone(jsonSpec))
  return parser
}

function records(items) {
  return '[' + Array.from({ length: items }, (_, i) => `{"a":${i},"b":[${i},"x"]}`).join(',') + ']'
}

// Every rule a parse reaches: the live ones and, under a bound, the
// snapshots their links hold.
function rulesOf(parser, src) {
  const rules = new Set()
  parser.sub({
    rule: (rule, ctx) => {
      for (const r of [rule, rule.prev, rule.parent, rule.child, rule.next]) {
        if (r !== ctx.NORULE) rules.add(r)
      }
    },
  })
  parser.parse(src)
  return rules
}

describe('rule shape', () => {
  it("defines every rule's node from one getter and one setter", () => {
    for (const history of [null, 3]) {
      const rules = rulesOf(jsonParser(history), records(50))
      assert.ok(300 < rules.size, `history ${history}: ${rules.size} rules`)
      let first
      for (const rule of rules) {
        const node = Object.getOwnPropertyDescriptor(rule, 'node')
        assert.ok(node, `history ${history}: node is an own property`)
        assert.equal(node.enumerable, true, 'enumerable, so a spread of a rule carries it')
        assert.equal(node.configurable, true)
        assert.equal(typeof node.get, 'function')
        assert.equal(typeof node.set, 'function')
        first ??= node
        assert.equal(node.get, first.get, `history ${history}: one getter for every rule`)
        assert.equal(node.set, first.set, `history ${history}: one setter for every rule`)
      }
    }
  })

  it('keeps every rule, live or a snapshot, out of dictionary mode', () => {
    // V8's own answer, from a process that may use its natives syntax.
    const root = JSON.stringify(path.join(__dirname, '..'))
    const fixture = JSON.stringify(path.join(__dirname, 'json-builder.fixture.json'))
    const script = `
      const { Tabnas } = require(${root})
      const spec = require(${fixture})
      const out = {}
      for (const history of [null, 3]) {
        const parser = new Tabnas({ rule: { start: 'val', history } })
        parser.grammar(JSON.parse(JSON.stringify(spec)))
        const rules = new Set()
        parser.sub({
          rule: (rule, ctx) => {
            for (const r of [rule, rule.prev, rule.parent, rule.child, rule.next]) {
              if (r !== ctx.NORULE) rules.add(r)
            }
          },
        })
        parser.parse(${JSON.stringify(records(500))})
        let fast = 0
        for (const r of rules) if (%HasFastProperties(r)) fast++
        out[String(history)] = { rules: rules.size, fast }
      }
      console.log(JSON.stringify(out))
    `
    const out = JSON.parse(
      execFileSync(process.execPath, ['--allow-natives-syntax', '-e', script], {
        encoding: 'utf8',
      }),
    )
    for (const history of ['null', '3']) {
      const { rules, fast } = out[history]
      assert.ok(3000 < rules, `history ${history}: ${rules} rules`)
      assert.equal(fast, rules, `history ${history}: ${fast} of ${rules} rules have fast properties`)
    }
  })
})
