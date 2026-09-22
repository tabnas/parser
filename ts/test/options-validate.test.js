/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// The options door is typed (#143): an ill-typed leaf is a load fault
// with a plain Error naming the leaf, on every door (constructor,
// options(), a serialized grammar), where it used to be a raw TypeError
// from deep inside configure() here and a silent drop in Go. A function
// reference resolved into a slot that holds data is the same fault, which
// is what restricts refs to declared code slots. go/options_validate_test.go
// drives the same leaves through the Go door.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

describe('options-validate', () => {
  it('names an ill-typed leaf on every door', () => {
    const cases = [
      [{ line: { chars: { a: 1 } } }, /options\.line\.chars: expected string, got object/],
      [{ tokenSet: { VAL: 'str' } }, /options\.tokenSet\.VAL: expected array, got string/],
      [{ string: { escapeChar: [] } }, /options\.string\.escapeChar: expected string, got array/],
      [{ rule: { maxmul: 'three' } }, /options\.rule\.maxmul: expected number, got string/],
      [{ comment: { def: { hash: { line: 'yes' } } } }, /options\.comment\.def\.hash\.line: expected boolean/],
      [{ rewind: { history: true } }, /options\.rewind\.history/],
    ]
    for (const [opts, re] of cases) {
      assert.throws(() => new Tabnas(opts), re, 'constructor ' + JSON.stringify(opts))
      assert.throws(() => new Tabnas().options(opts), re, 'options() ' + JSON.stringify(opts))
      assert.throws(() => new Tabnas().grammar({ options: opts }), re, 'grammar() ' + JSON.stringify(opts))
    }
    // None of those is a TypeError: the fault carries the engine's own
    // message, not a stack from inside configure().
    assert.throws(() => new Tabnas({ line: { chars: { a: 1 } } }), (e) => !(e instanceof TypeError))
  })

  it('refuses a function reference in a data slot, and resolves one in a code slot', () => {
    for (const opts of [
      { options: { line: { chars: '@node$' } } },
      { options: { rule: { start: '@value$' } } },
      { options: { tokenSet: { IGNORE: ['@node$'] } } },
    ]) {
      assert.throws(
        () => new Tabnas().grammar(opts),
        /a function reference is only allowed in a declared code slot|expected string/,
        JSON.stringify(opts),
      )
    }
    // A declared code slot still takes a ref.
    const tn = new Tabnas()
    let ran = 0
    tn.grammar({
      ref: { '@prep': () => { ran++ } },
      options: { parse: { prepare: { count: '@prep' } } },
    })
    tn.parse('1')
    assert.equal(ran, 1)
  })

  it('still accepts the documented idioms', () => {
    assert.doesNotThrow(() => new Tabnas({
      rewind: { history: false },
      tokenSet: { IGNORE: [undefined, null, undefined] },
      comment: { def: { hash: null, slash: false } },
      value: { def: { yes: { val: 1 }, no: false } },
      fixed: { token: { '#CA': null, '#X': 'x' } },
      errmsg: { suffix: 'because' },
      lex: { emptyResult: [] },
      number: { exclude: /^0/ },
      match: { token: { '#A': /^a/ } },
      string: { escape: { v: null } },
      plugin: { anything: { goes: true } },
      csv: { field: { separator: '|' } },
    }))
  })
})
