/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// A grammar spec can name a rule that does not exist. `validateAlt` cannot
// catch it — it sees one alternate and does not know the rule set — so the
// typo survived validation and surfaced only at parse time, as an
// `unknown_rule` error, and only once an input reached the alternate
// carrying it. `validateGrammar` is the check that has the rule map in
// scope. See tabnas/parser#113.
//
// The message wording and ordering here are the CROSS-RUNTIME contract:
// go/validate_grammar_test.go asserts the identical strings.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { validateGrammar } = require('..')

describe('validate-grammar', () => {

  it('reports a p that names no rule — the #113 repro', () => {
    const spec = {
      rule: {
        val: {
          open: [
            { s: '#OB', p: 'mapp', b: 1, a: '@reset$' },
            { s: '#OS', p: 'list' },
          ],
        },
        list: { open: [] },
      },
    }
    assert.deepEqual(validateGrammar(spec), [
      'val.open alt[0]: unknown rule in p: "mapp"',
    ])
  })

  it('reports r as well as p, and reads the {alts,inject} list form', () => {
    const spec = {
      rule: {
        a: { close: { alts: [{ s: '#CS', r: 'nope' }], inject: { append: true } } },
      },
    }
    assert.deepEqual(validateGrammar(spec), [
      'a.close alt[0]: unknown rule in r: "nope"',
    ])
  })

  it('a null entry removes a rule, so referencing it dangles', () => {
    const spec = { rule: { gone: null, a: { open: [{ p: 'gone' }] } } }
    assert.deepEqual(validateGrammar(spec), [
      'a.open alt[0]: unknown rule in p: "gone"',
    ])
  })

  it('skips what cannot be checked statically: FuncRef, false, absent', () => {
    const spec = {
      rule: {
        a: {
          open: [
            { p: '@pickNext' },   // resolves to a function at load
            { r: '@pickOther' },
            { p: false },         // deliberately disabled
            { s: '#TX' },         // neither slot
          ],
        },
      },
    }
    assert.deepEqual(validateGrammar(spec), [])
  })

  it('known rules let an EXTENSION spec push to a rule it does not define', () => {
    const spec = { rule: { a: { open: [{ p: 'base' }] } } }
    assert.deepEqual(validateGrammar(spec), [
      'a.open alt[0]: unknown rule in p: "base"',
    ])
    assert.deepEqual(validateGrammar(spec, ['base']), [])
    assert.deepEqual(validateGrammar(spec, new Set(['base'])), [])
  })

  it('reports every dangling reference, sorted', () => {
    const spec = {
      rule: {
        val: { open: [{ p: 'mapp' }] },
        list: { close: { alts: [{ r: 'nope' }] } },
        gone: null,
        dangling: { open: [{ p: 'gone' }] },
      },
    }
    assert.deepEqual(validateGrammar(spec), [
      'dangling.open alt[0]: unknown rule in p: "gone"',
      'list.close alt[0]: unknown rule in r: "nope"',
      'val.open alt[0]: unknown rule in p: "mapp"',
    ])
  })

  it('a grammar that resolves reports nothing', () => {
    const spec = {
      rule: { a: { open: [{ p: 'b' }], close: [{ r: 'a' }] }, b: { open: [] } },
    }
    assert.deepEqual(validateGrammar(spec), [])
  })

  it('malformed input yields no problems rather than throwing', () => {
    for (const bad of [null, undefined, {}, { rule: null }, { rule: 'x' },
      { rule: { a: 'x' } }, { rule: { a: { open: 'x' } } },
      { rule: { a: { open: [null] } } }]) {
      assert.deepEqual(validateGrammar(bad), [], JSON.stringify(bad))
    }
  })

  it('reports rule references only — per-alt checks stay in validateAlts', () => {
    // The two runtimes word their group-tag message differently; keeping
    // this surface to rule references is what makes it identical in both.
    const spec = { rule: { a: { open: [{ g: 'bad tag!', p: 'nope' }] } } }
    assert.deepEqual(validateGrammar(spec), [
      'a.open alt[0]: unknown rule in p: "nope"',
    ])
  })

})
