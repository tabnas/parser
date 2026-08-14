/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it, afterEach } = require('node:test')
const assert = require('node:assert')

const { util, Tabnas } = require('..')
const { deep } = util

// A JSON object gives `__proto__` (and `constructor`/`prototype`) as OWN
// enumerable keys — unlike a JS object literal, where `__proto__:` sets the
// prototype. These payloads are therefore built with JSON.parse to reproduce
// the attacker-supplied-JSON shape the merge functions must defend against.
const P = (s) => JSON.parse(s)

// The dangerous keys, in case a fix regresses.
const DANGER = ['__proto__', 'constructor', 'prototype']

describe('prototype-pollution', () => {

  // Belt-and-braces: if any assertion regresses and pollution leaks, remove
  // the sentinels so the rest of the suite is not poisoned.
  afterEach(() => {
    for (const sentinel of [
      'pwn', 'pwnOpts', 'pwnGram', 'pwnAlt', 'pwnNest', 'pwnDeep',
    ]) {
      delete Object.prototype[sentinel]
    }
  })

  it('options() with a JSON __proto__ payload does not pollute', () => {
    new Tabnas().options(P('{"__proto__":{"pwnOpts":1}}'))
    assert.strictEqual({}.pwnOpts, undefined)
    assert.strictEqual(
      Object.prototype.hasOwnProperty.call(Object.prototype, 'pwnOpts'),
      false,
    )
  })

  it('grammar({options:{__proto__:...}}) does not pollute', () => {
    new Tabnas().grammar(P('{"options":{"__proto__":{"pwnGram":1}}}'))
    assert.strictEqual({}.pwnGram, undefined)
  })

  it('grammar with __proto__ nested in alt data does not pollute', () => {
    const spec = P(
      '{"rule":{"top":{"open":[{"s":[1],"a":{"__proto__":{"pwnAlt":1}}}]}}}',
    )
    new Tabnas().grammar(spec)
    assert.strictEqual({}.pwnAlt, undefined)
  })

  it('all three dangerous keys, nested one level deep, are neutralized', () => {
    for (const k of DANGER) {
      const payload = P(`{"outer":{"${k}":{"pwnNest":1}}}`)
      new Tabnas().options(payload)
      assert.strictEqual(
        {}.pwnNest,
        undefined,
        `nested "${k}" polluted the prototype`,
      )
    }
  })

  it('deep() drops dangerous keys but keeps legitimate data', () => {
    // Direct unit-level proof on the merge primitive itself.
    for (const k of DANGER) {
      const base = {}
      deep(base, P(`{"${k}":{"pwnDeep":1}}`))
      assert.strictEqual({}.pwnDeep, undefined, `deep() merged "${k}"`)
      assert.strictEqual(
        Object.prototype.hasOwnProperty.call(base, k),
        false,
        `deep() wrote own key "${k}" onto base`,
      )
    }

    // A legitimate key that merely resembles a dangerous one must still merge.
    const merged = deep({ a: 1 }, { proto_thing: 2, constructor_name: 3 })
    assert.strictEqual(merged.a, 1)
    assert.strictEqual(merged.proto_thing, 2)
    assert.strictEqual(merged.constructor_name, 3)
  })

  it('a legitimate option named proto_thing still merges via options()', () => {
    const out = new Tabnas().options(P('{"proto_thing":7}'))
    assert.strictEqual(out.proto_thing, 7)
  })

})
