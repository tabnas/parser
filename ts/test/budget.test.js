/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Opt-in parse budget / cancellation: options.parse.budget. The main
// rule loop invokes onCheck every checkEveryN iterations; a false
// return cancels the parse with a `cancel` error. Off by default.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, TabnasError } = require('..')
const { json } = require('../dist-test/json-plugin')

const SRC = '{"a":[1,2,3,{"b":[4,5,6]},7],"c":{"d":[8,9]}}'

describe('budget', () => {
  it('is off by default: no callback, parse unchanged', () => {
    const tn = new Tabnas({ plugins: [json] })
    assert.equal(JSON.stringify(tn.parse(SRC)), SRC)
  })

  it('invokes onCheck periodically during a parse', () => {
    let calls = 0
    const tn = new Tabnas({
      plugins: [json],
      parse: { budget: { checkEveryN: 2, onCheck: () => void calls++ } },
    })
    assert.equal(JSON.stringify(tn.parse(SRC)), SRC)
    assert.ok(1 <= calls, 'onCheck called at least once, got ' + calls)
  })

  it('cancels the parse when onCheck returns false', () => {
    let calls = 0
    const tn = new Tabnas({
      plugins: [json],
      parse: { budget: { checkEveryN: 2, onCheck: () => 3 > ++calls } },
    })
    try {
      tn.parse(SRC)
      assert.fail('parse should cancel')
    } catch (e) {
      assert.ok(e instanceof TabnasError)
      assert.equal(e.code, 'cancel')
      assert.ok(e.message.includes('cancel'))
    }
  })

  it('cancel in recovery mode returns { value, errors }', () => {
    let calls = 0
    const tn = new Tabnas({
      plugins: [json],
      parse: {
        recover: { enabled: true },
        budget: { checkEveryN: 2, onCheck: () => 3 > ++calls },
      },
    })
    const out = tn.parse(SRC)
    assert.ok('value' in out && 'errors' in out)
    assert.equal(out.errors.length, 1)
    assert.equal(out.errors[0].code, 'cancel')
  })

  it('a deadline-style check works via closure state', () => {
    const started = Date.now()
    const tn = new Tabnas({
      plugins: [json],
      parse: {
        budget: { checkEveryN: 1, onCheck: () => Date.now() - started < 60000 },
      },
    })
    assert.equal(JSON.stringify(tn.parse(SRC)), SRC)
  })
})
