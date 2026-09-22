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
const { readFileSync } = require('node:fs')
const { join } = require('node:path')

const { Tabnas } = require('..')
const { validateOptions, WIDENINGS } = require('../dist/utility')
const { defaults } = require('../dist/defaults')

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
      lex: { emptyResult: [], match: { number: false } },
      number: { exclude: /^0/ },
      match: { token: { '#A': /^a/ } },
      string: { escape: { v: null } },
      ender: ':',
      plugin: { anything: { goes: true } },
      csv: { field: { separator: '|' } },
    }))
  })

  // #143's validator was stricter than the readers it was meant to
  // describe, and 0.11.0 shipped that: a string `ender` is documented,
  // read and split into characters by configure(), and the validator
  // refused it, so every fresh install of the published chain threw at
  // module load (@tabnas/yaml passes `ender: ':'`).
  it('accepts a string ender, and splits it exactly as the array form', () => {
    assert.doesNotThrow(() => new Tabnas({ ender: ':' }))
    assert.doesNotThrow(() => new Tabnas().options({ ender: ':' }))
    assert.doesNotThrow(() => new Tabnas().grammar({ options: { ender: ':' } }))

    // The string is split per character, so it is the array form.
    const src = (o) => new Tabnas(o).internal().config.re.ender.source
    assert.equal(src({ ender: ':' }), src({ ender: [':'] }))
    assert.equal(src({ ender: ';|' }), src({ ender: [';', '|'] }))

    // And the characters really are enders: a two-char string contributes
    // both, which is what a single array entry ';|' would NOT do.
    assert.match(src({ ender: ';|' }), /\|;\|\\\|/)
  })

  // A matcher set to false is dropped, exactly as null drops it. Go's
  // door already accepted this; only TypeScript refused it.
  it('accepts false for a lex matcher, as null already was', () => {
    const matchers = (o) =>
      new Tabnas(o).internal().config.lex.match.map((m) => m.matcher)
    assert.ok(matchers({}).includes('number'))
    for (const off of [false, null]) {
      assert.doesNotThrow(() => new Tabnas({ lex: { match: { number: off } } }))
      assert.deepEqual(
        matchers({ lex: { match: { number: off } } }),
        matchers({ lex: { match: { number: null } } }),
      )
      assert.ok(!matchers({ lex: { match: { number: off } } }).includes('number'))
    }
  })

  // The regression test for the CLASS, not the instance: every widening
  // the table declares must be accepted by the validator AND survive the
  // reader, on every door. A widening declared but not read, or read but
  // not declared, fails here.
  it('the validator accepts what the readers accept', () => {
    const SAMPLES = {
      'options.ender': [':', ';|'],
      'options.lex.match.*': [false],
      'options.rewind.history': [false],
      'options.errmsg.suffix': ['because', () => 'because'],
    }
    // Every declared widening carries a sample, and vice versa.
    assert.deepEqual(Object.keys(SAMPLES).sort(), Object.keys(WIDENINGS).sort())

    for (const [path, samples] of Object.entries(SAMPLES)) {
      for (const sample of samples) {
        // The table itself agrees this is a widening of that leaf.
        const at = path.endsWith('.*') ? path.slice(0, -1) + 'number' : path
        assert.doesNotThrow(
          () => validateOptions(optsFor(at, sample), defaults),
          at + ' = ' + String(sample) + ' must validate',
        )
        // ... and the reader takes it, on every door.
        const opts = optsFor(at, sample)
        assert.doesNotThrow(() => new Tabnas(opts), at + ' via constructor')
        assert.doesNotThrow(() => new Tabnas().options(opts), at + ' via options()')
      }
    }

    // A widening is narrow, not a blanket pass: the door stays typed at
    // the same leaves, and at the map a widened entry lives in.
    for (const [opts, re] of [
      [{ ender: 7 }, /options\.ender: expected array, got number/],
      [{ ender: { a: 1 } }, /options\.ender: expected array, got object/],
      [{ lex: { match: { number: true } } }, /options\.lex\.match\.number: expected object/],
      [{ lex: { match: { number: 'off' } } }, /options\.lex\.match\.number: expected object/],
      // the MAP is not widened, only an entry of it
      [{ lex: { match: false } }, /options\.lex\.match: expected object, got boolean/],
      [{ rewind: { history: 'lots' } }, /options\.rewind\.history: expected number/],
      // and the values a reader only fails to crash on stay refused
      [{ string: { escape: { n: false } } }, /options\.string\.escape\.n: expected string/],
      [{ result: { fail: 'x' } }, /options\.result\.fail: expected array/],
    ]) {
      assert.throws(() => new Tabnas(opts), re, JSON.stringify(opts))
    }
  })

  // The other half of the contract, and the half that would have caught
  // #143 before it shipped: a reader that TYPE-TESTS an option value
  // accepts more than one shape there, so that option must be declared
  // in WIDENINGS. Source-level on purpose — the divergence is between
  // the reader's text and the table, and nothing else can see it.
  it('every option a reader type-tests is declared as a widening', () => {
    const declared = new Set(
      Object.keys(WIDENINGS).map((p) => p.replace(/^options\./, '').replace(/\.\*$/, '')),
    )
    const found = new Set()
    for (const file of ['utility.ts', 'lexer.ts']) {
      const src = readFileSync(join(__dirname, '..', 'src', file), 'utf8')
      for (const m of src.matchAll(/typeof\s+opts[.?]+([A-Za-z0-9_.?]+)/g)) {
        found.add(m[1].replace(/\?/g, ''))
      }
      for (const m of src.matchAll(/Array\.isArray\(\s*opts[.?]+([A-Za-z0-9_.?]+)\s*\)/g)) {
        found.add(m[1].replace(/\?/g, ''))
      }
    }
    // The readers do type-test something — a silent zero here would make
    // this test vacuous.
    assert.ok(0 < found.size, 'found no reader type tests at all')
    for (const path of found) {
      assert.ok(
        declared.has(path),
        'options.' + path + ' is type-tested by a reader but not declared in ' +
        'WIDENINGS: the validator will refuse what the reader accepts',
      )
    }
  })
  // Parser's CI clones `bnf debug abnf` and builds them against this
  // engine, so the jsonic/yaml/json5/toml/... grammars — the packages a
  // fresh install of the published chain actually loads — have NO
  // coverage here, and that is what let #143 out: the validator refused
  // an overlay that @tabnas/yaml has always passed, and nothing in this
  // repo loads yaml.
  //
  // A lane that installs the published grammars cannot run here (they
  // depend on this engine, so npm resolves a NESTED published copy and
  // the lane would not exercise the local build without committed local
  // wiring). What is hermetic is the OVERLAY each one passes, recorded
  // from the published package, driven through every door. A check hook
  // stands in as a bare function: the door validates shapes, not bodies.
  it('accepts the overlays the published grammars pass', () => {
    const hook = () => undefined
    const OVERLAYS = {
      // @tabnas/yaml 0.5.7, yaml.js: tabnas.options({...})
      '@tabnas/yaml': {
        fixed: { token: { '#CL': null } },
        ender: ':',
        string: { chars: '' },
        number: { check: hook },
        text: { check: hook },
      },
      // @tabnas/jsonic 0.6.7, jsonic.js: one namespace per plugin.
      '@tabnas/jsonic': { plugin: { yaml: { safe: true } } },
    }
    for (const [pkg, opts] of Object.entries(OVERLAYS)) {
      assert.doesNotThrow(() => new Tabnas(opts), pkg + ' via constructor')
      assert.doesNotThrow(() => new Tabnas().options(opts), pkg + ' via options()')
    }
  })
})

// Build an options object with `val` at a dotted path.
function optsFor(at, val) {
  const parts = at.replace(/^options\./, '').split('.')
  const out = {}
  let node = out
  for (let i = 0; i < parts.length - 1; i++) {
    node = node[parts[i]] = {}
  }
  node[parts[parts.length - 1]] = val
  return out
}

describe('options-validate SKIP', () => {
  // `@SKIP` resolves to the SKIP symbol BEFORE validation runs, so a
  // grammar that writes it hands these validators a symbol. The generic
  // walk has always accepted that; the three leaves with a validator of
  // their own rejected it, which made the sentinel usable everywhere
  // except the three places it is most useful. Reported from
  // tabnas/jsonic, whose `skip-in-grammar-options-tokenset` and
  // `skip-in-grammar-options-value-def` suites this failed.
  const { SKIP } = require('..')

  it('accepts the SKIP sentinel in tokenSet, value.def and comment.def', () => {
    const cases = [
      { tokenSet: { KEY: [SKIP, null, null, null] } },
      { tokenSet: { KEY: SKIP } },
      { value: { def: { yes: SKIP } } },
      { comment: { def: { hash: SKIP } } },
    ]
    for (const opts of cases) {
      assert.doesNotThrow(() => new Tabnas(opts), 'constructor ' + Object.keys(opts)[0])
      assert.doesNotThrow(() => new Tabnas().options(opts), 'options() ' + Object.keys(opts)[0])
    }
  })

  it('still rejects an ill-typed leaf beside a SKIP', () => {
    // The fix must not turn the validator off: a real type fault in the
    // same shape is still a load fault.
    assert.throws(
      () => new Tabnas({ tokenSet: { KEY: [SKIP, 7] } }),
      /options\.tokenSet\.KEY\[1\]: expected string, got number/,
    )
    assert.throws(
      () => new Tabnas({ tokenSet: { KEY: 7 } }),
      /options\.tokenSet\.KEY: expected array, got number/,
    )
  })
})

describe('options-validate absent token sets', () => {
  const { SKIP } = require('..')

  // A caller-defined name with no default has nothing to leave alone, so
  // it must be dropped rather than carried through as a set with no
  // members. `deep()` assigns `base[k] = deep(base[k], over[k])`
  // unconditionally, so it leaves an own property holding the absent
  // value, and `configure()` then called `.filter()` on it. All three
  // spellings of "leave this alone" reached that: `null` and `undefined`
  // always did, and SKIP joined them when the validator learned to accept
  // it. Each died with a raw TypeError rather than a load fault or a
  // quiet no-op.
  it('ignores a token set whose members are absent', () => {
    for (const members of [null, undefined, SKIP]) {
      const label = 'CUSTOM = ' + String(members)
      assert.doesNotThrow(() => new Tabnas({ tokenSet: { CUSTOM: members } }), label)
      const parser = new Tabnas({ tokenSet: { CUSTOM: members } })
      assert.equal(
        'CUSTOM' in parser.internal().config.tokenSet,
        false,
        label + ': the name reached the config',
      )
    }
  })

  it('leaves a name that DOES have a default alone', () => {
    // The other half of the same rule: SKIP over a set that exists is a
    // no-op, not a deletion.
    const base = new Tabnas({}).internal().config.tokenSet.KEY.length
    assert.equal(new Tabnas({ tokenSet: { KEY: SKIP } }).internal().config.tokenSet.KEY.length, base)
    // ...and a real list still replaces it.
    assert.equal(
      new Tabnas({ tokenSet: { KEY: ['#ST', null, null, null] } })
        .internal().config.tokenSet.KEY.length,
      1,
    )
  })
})
