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

  // Where the sentinel is actually wanted is a set's MEMBERS: `@SKIP`
  // preserves the default at that position and `null` clears it, which is
  // how a grammar narrows a built-in set. tabnas/jsonic's
  // `skip-in-grammar-options-tokenset` is the case that drove it.
  it('takes SKIP and null as members', () => {
    const base = new Tabnas({}).internal().config.tokenSet.KEY.length
    assert.equal(base, 4)
    // SKIP keeps position 0's default, the nulls clear the rest.
    assert.deepEqual(
      new Tabnas({ tokenSet: { KEY: [SKIP, null, null, null] } })
        .internal().config.tokenSet.KEY.length,
      1,
    )
    assert.equal(
      new Tabnas({ tokenSet: { KEY: ['#ST', null, null, null] } })
        .internal().config.tokenSet.KEY.length,
      1,
    )
  })

  // The whole VALUE takes only an array, or the two spellings of "not
  // supplied". A name with nothing behind it is dropped rather than
  // carried through as a set with no members: `.filter()` on the absent
  // value is a raw TypeError, and `deep()` creates the own property even
  // for a value it did not merge.
  it('ignores an absent whole value for a name with no default', () => {
    for (const members of [undefined, SKIP]) {
      const label = 'CUSTOM = ' + String(members)
      assert.doesNotThrow(() => new Tabnas({ tokenSet: { CUSTOM: members } }), label)
      assert.equal(
        'CUSTOM' in new Tabnas({ tokenSet: { CUSTOM: members } }).internal().config.tokenSet,
        false,
        label + ': the name reached the config',
      )
    }
  })

  it('leaves a name that DOES have a default alone', () => {
    // `deep()` keeps the base for both, so the default array is still there
    // when configure() runs and the guard never sees the name.
    const base = new Tabnas({}).internal().config.tokenSet.KEY.length
    for (const members of [undefined, SKIP]) {
      assert.equal(
        new Tabnas({ tokenSet: { KEY: members } }).internal().config.tokenSet.KEY.length,
        base,
        'KEY = ' + String(members),
      )
    }
  })

  // An explicit null whole value is a load fault, not a silent delete.
  // `deep()` replaces the base for it, so accepting it would clear a
  // built-in set, and the clearing would be quiet: a rule naming `#KEY`
  // afterwards mints a single token of that name rather than failing,
  // since Rule.parse resolves a set name as `tokenSet(n) ?? token(n)`.
  //
  // Both doors are asserted because they reach configure() differently:
  // the constructor builds a fresh config and `options()` hands the
  // existing one back, so a guard that only dropped the name would clear
  // the set in one and keep it in the other.
  it('refuses an explicit null whole value, on every door', () => {
    for (const name of ['KEY', 'CUSTOM']) {
      const opts = { tokenSet: { [name]: null } }
      const re = new RegExp('options\\.tokenSet\\.' + name + ': expected array')
      assert.throws(() => new Tabnas(opts), re, 'constructor ' + name)
      assert.throws(() => new Tabnas().options(opts), re, 'options() ' + name)
      assert.throws(() => new Tabnas().grammar({ options: opts }), re, 'grammar() ' + name)
    }
  })

  it('still rejects an ill-typed member', () => {
    assert.throws(
      () => new Tabnas({ tokenSet: { KEY: [SKIP, 7] } }),
      /options\.tokenSet\.KEY\[1\]: expected string, got number/,
    )
    assert.throws(
      () => new Tabnas({ tokenSet: { KEY: 7 } }),
      /options\.tokenSet\.KEY: expected array, got number/,
    )
  })

  // The spelling that empties a set while keeping the name. An empty array
  // does not do it: an array overlays index by index, so overlaying nothing
  // leaves every default standing.
  it('empties a set by clearing every position, not with an empty array', () => {
    const emptied = new Tabnas({ tokenSet: { KEY: [null, null, null, null] } })
      .internal().config.tokenSet
    assert.equal('KEY' in emptied, true)
    assert.equal(emptied.KEY.length, 0)

    const notEmptied = new Tabnas({ tokenSet: { KEY: [] } }).internal().config.tokenSet
    assert.equal(notEmptied.KEY.length, 4, 'an empty array emptied the set')
  })

  // `deep()` reads the base as `base[k]`, which walks the prototype chain,
  // so a name Object.prototype also carries merged the inherited METHOD in
  // as an own property whenever the overlay said "keep the base". The
  // reduce below then called `.filter()` on a function:
  //
  //   TypeError: members.filter is not a function
  //
  // which is the exact failure this suite exists to stop, surviving for
  // one class of name. The validator cannot catch it: it sees the SKIP the
  // caller wrote, and the function only exists after the merge.
  // Shared by the two cases below: inherited names that are ORDINARY set
  // names, as distinct from the three `deep()` refuses to merge.
  const PROTO = [
    'toString', 'valueOf', 'hasOwnProperty', 'isPrototypeOf', 'propertyIsEnumerable',
  ]

  it('does not crash on a name inherited from Object.prototype', () => {
    for (const name of PROTO) {
      for (const members of [SKIP, undefined]) {
        const cfg = new Tabnas({ tokenSet: { [name]: members } }).internal().config
        // hasOwn, not `in`: every one of these names IS on the prototype
        // of any plain object, so `in` answers true whatever the config
        // holds -- which is the same prototype-chain read that made the
        // engine crash here in the first place.
        assert.equal(
          Object.hasOwn(cfg.tokenSet, name),
          false,
          name + ' = ' + String(members) + ' reached the config',
        )
      }
    }

    // An array under such a name is an ordinary set and still installs.
    for (const name of PROTO) {
      const cfg = new Tabnas({ tokenSet: { [name]: ['#TX'] } }).internal().config
      assert.equal(Object.hasOwn(cfg.tokenSet, name), true, name + ' as a real set')
      assert.equal(cfg.tokenSet[name].length, 1, name + ' set size')
      assert.equal(cfg.tokenSetTins[name][cfg.t.TX], true, name + ' tin lookup')
    }

  })

  // RESERVED, not dropped. `deep()` refuses to merge `__proto__`,
  // `constructor` and `prototype` on purpose -- merging them reaches the
  // prototype chain, which is prototype pollution -- and the cost used to
  // be that a set under one of those names vanished without a word, while
  // the option reference says an undeclared name installs as written. The
  // door says so now, on every spelling and every map with caller-chosen
  // keys.
  it('refuses the three names deep() will not merge', () => {
    const re = /reserved name/
    for (const name of ['__proto__', 'constructor', 'prototype']) {
      // Through JSON, because that is the only way `__proto__` becomes an
      // OWN key: `{__proto__: v}` in a literal is a prototype assignment.
      const opts = JSON.parse(`{"tokenSet":{${JSON.stringify(name)}:["#TX"]}}`)
      assert.equal(Object.hasOwn(opts.tokenSet, name), true, name + ' is an own key')
      assert.throws(() => new Tabnas(opts), re, 'constructor ' + name)
      assert.throws(() => new Tabnas({}).options(opts), re, 'options() ' + name)
    }

    // Every other inherited name is an ordinary set and still installs --
    // the refusal is the three `deep()` skips, not "anything on the
    // prototype". The table that decides this is null-prototype for
    // exactly the reason this whole suite exists: keyed by caller-chosen
    // names, a plain object answers `toString` with the inherited method.
    for (const name of PROTO) {
      const cfg = new Tabnas({ tokenSet: { [name]: ['#TX'] } }).internal().config
      assert.equal(Object.hasOwn(cfg.tokenSet, name), true, name + ' still installs')
    }

    // The same rule on another caller-keyed map.
    assert.throws(
      () => new Tabnas(JSON.parse('{"comment":{"def":{"constructor":{"line":true}}}}')),
      re,
      'comment.def.constructor',
    )

    // And Object.prototype is untouched by any of it.
    assert.equal(undefined, {}.line)
  })

  // A null CONTAINER. The constructor let `deep()` replace the whole map
  // and built NO sets at all, while `options()` kept all three, so one
  // input had two answers depending on the door. Refused on both now, for
  // the reason a null NAME is: there is no map a null could name.
  it('refuses a null tokenSet container on both doors', () => {
    const re = /options\.tokenSet: expected object, got null/
    assert.throws(() => new Tabnas({ tokenSet: null }), re, 'constructor')
    assert.throws(() => new Tabnas({}).options({ tokenSet: null }), re, 'options()')
    assert.throws(
      () => new Tabnas(JSON.parse('{"tokenSet":null}')),
      re,
      'serialized',
    )

    // SKIP and undefined are still the two spellings of "not supplied",
    // and a null DEFINITION map is still an ordinary "none of these".
    for (const whole of [SKIP, undefined]) {
      const cfg = new Tabnas({ tokenSet: whole }).internal().config
      assert.deepEqual(Object.keys(cfg.tokenSet), ['IGNORE', 'VAL', 'KEY'], String(whole))
    }
    assert.doesNotThrow(() => new Tabnas({ comment: { def: null } }), 'comment.def: null')
  })

  // `undefined` is the third spelling of "keep what the base holds", and
  // it reads identically to SKIP at both levels. It matters for the TYPE
  // as much as the runtime: `tokenSet?:` makes the property optional and
  // says nothing about the index signature's values, so `undefined` has
  // to appear in both unions for a strict caller to write what these
  // assertions prove the engine accepts.
  it('treats an undefined member as SKIP does', () => {
    const base = new Tabnas({}).internal().config.tokenSet.KEY
    const withUndef = new Tabnas({ tokenSet: { KEY: [null, undefined, null, null] } })
      .internal().config.tokenSet.KEY
    const withSkip = new Tabnas({ tokenSet: { KEY: [null, SKIP, null, null] } })
      .internal().config.tokenSet.KEY
    assert.deepEqual(withUndef, [base[1]], 'undefined did not preserve position 1')
    assert.deepEqual(withUndef, withSkip, 'undefined and SKIP disagree')
  })
})
