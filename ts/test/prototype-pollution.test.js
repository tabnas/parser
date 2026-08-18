/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// PROTOTYPE POLLUTION: the rule-spec map is keyed by rule name.
//
// Before the fix `Parser.rsm` was a plain `{}` literal, so a rule named
// __proto__ was not an ordinary key. `this.rsm[name]` read back
// Object.prototype, the `||` guard adopted it as the RuleSpec, and the very
// next line — `rs.name = name` — wrote onto Object.prototype, where it is
// visible to every object and array in the process:
//
//   rs = this.rsm[name] = this.rsm[name] || makeRuleSpec(...)
//   rs.name = name
//
// `constructor` and `toString` hit the same read and threw instead ("Cannot
// assign to read only property 'name' of function ...").
//
// Rule names are not always developer-typed: @tabnas/abnf and @tabnas/bnf
// compile grammar TEXT into rules, so a user-supplied grammar reaches here.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

const BASE = Object.getOwnPropertyNames(Object.prototype).slice()

function assertNoPollution() {
  const added = Object.getOwnPropertyNames(Object.prototype)
    .filter((k) => !BASE.includes(k))
  for (const k of added) delete Object.prototype[k]
  assert.deepEqual(added, [], 'Object.prototype gained: ' + added)
}

describe('prototype-pollution', () => {

  it('a rule named __proto__ does not write Object.prototype', () => {
    const tn = new Tabnas()
    tn.rule('__proto__', (rs) => rs)
    assertNoPollution()

    // And it is a real entry, not a lost write.
    const rsm = tn.internal().parser.rule()
    assert.equal(
      Object.prototype.hasOwnProperty.call(rsm, '__proto__'), true,
      'rule __proto__ should be an own key of the rule-spec map')
  })

  it('rules named constructor and toString register without throwing', () => {
    for (const name of ['constructor', 'toString', 'hasOwnProperty']) {
      const tn = new Tabnas()
      tn.rule(name, (rs) => rs)
      const rsm = tn.internal().parser.rule()
      assert.equal(
        Object.prototype.hasOwnProperty.call(rsm, name), true,
        'rule ' + name + ' should be an own key of the rule-spec map')
    }
    assertNoPollution()
  })

  it('an ordinary rule name is unaffected', () => {
    const tn = new Tabnas()
    tn.rule('myrule', (rs) => rs)
    const rsm = tn.internal().parser.rule()
    assert.equal(Object.prototype.hasOwnProperty.call(rsm, 'myrule'), true)
    assertNoPollution()
  })

})
