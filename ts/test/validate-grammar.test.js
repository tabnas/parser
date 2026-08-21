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

  it('a null entry REMOVES a rule the instance already had', () => {
    // The spec deletes `gone`, so pushing to it dangles even though the
    // caller listed it as known.
    const spec = { rule: { gone: null, a: { open: [{ p: 'gone' }] } } }
    assert.deepEqual(validateGrammar(spec, ['gone']), [
      'a.open alt[0]: unknown rule in p: "gone"',
    ])
  })

  it('clear wipes every known rule before the spec is applied', () => {
    const spec = { clear: true, rule: { a: { open: [{ p: 'base' }] } } }
    assert.deepEqual(validateGrammar(spec, ['base']), [
      'a.open alt[0]: unknown rule in p: "base"',
    ])
    // Without clear, the same known rule is legitimately referenced.
    assert.deepEqual(validateGrammar({ rule: spec.rule }, ['base']), [])
  })

  it('a rule name is quoted verbatim, not escaped', () => {
    // Go's %q would render this `\"`; TypeScript is canonical and inserts
    // the name as-is. go/validate_grammar_test.go asserts the same string.
    const spec = { rule: { a: { open: [{ p: 'bad"name' }] } } }
    assert.deepEqual(validateGrammar(spec), [
      'a.open alt[0]: unknown rule in p: "bad"name"',
    ])
  })

  it('sorts by UTF-16 code unit, so a non-BMP rule name sorts first', () => {
    // The ordering contract. U+10000 is a surrogate pair whose lead unit is
    // 0xD800, so UTF-16 puts it BEFORE U+E000 — while UTF-8 bytes put it
    // after. Go reproduces this ordering deliberately (utf16Less).
    const astral = '\u{10000}', priv = '\u{E000}'
    const spec = {
      rule: {
        [priv]: { open: [{ p: 'nope' }] },
        [astral]: { open: [{ p: 'nope' }] },
      },
    }
    assert.deepEqual(validateGrammar(spec), [
      astral + '.open alt[0]: unknown rule in p: "nope"',
      priv + '.open alt[0]: unknown rule in p: "nope"',
    ])
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
