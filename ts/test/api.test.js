/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const tn = new Tabnas()
const J = (src, meta, ctx) => tn.parse(src, meta, ctx)

describe('api', function () {
  it('standard', () => {
    const { keys } = Tabnas.util

    // Ensure no accidental static-API expansion on the class itself.
    assert.deepEqual(keys(Tabnas), [
      'util',
      'S',
      'OPEN',
      'CLOSE',
      'BEFORE',
      'AFTER',
      'EMPTY',
      'SKIP',
    ])

    // Spot-check the instance shape. Plugins decorate instances
    // further; this just guards against accidental additions to the
    // core class.
    assert.deepEqual(keys(tn).sort(), [
      'fixed',
      'id',
      'options',
      'parent',
      'token',
      'tokenSet',
    ])
  })
  // The Go engine's Make() dropped options.tokenSet while its SetOptions
  // applied it. TS applies it from both paths (configure() runs on the
  // constructor and on options()); pin that so the canonical side cannot
  // drift into the same split.
  it('tokenSet applies from constructor and options alike', () => {
    const ctor = new Tabnas({ tokenSet: { KEY: ['#ST', null, null, null] } })
    const opted = new Tabnas()
    opted.options({ tokenSet: { KEY: ['#ST', null, null, null] } })

    const ST = ctor.token('#ST')
    assert.deepEqual(ctor.tokenSet('KEY'), [ST])
    assert.deepEqual(opted.tokenSet('KEY'), ctor.tokenSet('KEY'))

    // A custom set name works the same way, from both paths.
    const ctorCustom = new Tabnas({ tokenSet: { MYSET: ['#TX', '#NR'] } })
    const optedCustom = new Tabnas()
    optedCustom.options({ tokenSet: { MYSET: ['#TX', '#NR'] } })
    assert.deepEqual(ctorCustom.tokenSet('MYSET'),
      [ctorCustom.token('#TX'), ctorCustom.token('#NR')])
    assert.deepEqual(optedCustom.tokenSet('MYSET'), ctorCustom.tokenSet('MYSET'))

    // The default instance is untouched by either.
    assert.deepEqual(new Tabnas().tokenSet('KEY'),
      [tn.token('#TX'), tn.token('#NR'), tn.token('#ST'), tn.token('#VL')])
  })

  // The tag is printed verbatim in three user-visible places: the
  // instance id, the "--internal: tag=..." suffix on every formatted
  // error, and the debug plugin's `tag:` line. An unset tag therefore
  // has to resolve to a concrete string, not to the empty one — Go left
  // Options().Tag empty and so rendered `tag=` everywhere TS rendered
  // `tag=-`. TS is canonical; pin it here.
  // Mirrors go/tag_default_test.go.
  it('tag defaults to dash and reaches the error suffix', () => {
    const plain = new Tabnas()
    assert.equal(plain.options.tag, '-')
    assert.ok(plain.id.endsWith('/-'), 'instance id should end with /-: ' + plain.id)

    const tagged = new Tabnas({ tag: 'mygrammar' })
    assert.equal(tagged.options.tag, 'mygrammar')
    assert.ok(tagged.id.endsWith('/mygrammar'), tagged.id)

    // The engine ships no grammar, so give the default instance a
    // one-rule one before asking it to fail.
    plain.options({ rule: { start: 'top', exclude: 'tabnas,imp' } })
    const { ZZ } = plain.token
    const { VAL } = plain.tokenSet
    plain.rule('top', (rs) => rs.open([{ s: [VAL] }]).close([{ s: [ZZ] }]))

    let msg = ''
    try {
      plain.parse('{')
    } catch (e) {
      msg = e.message
    }
    assert.match(msg, /--internal: tag=-;/)
  })

  // A grammar signals a domain-specific failure by marking a token bad
  // with its own code: the engine must surface that code rather than the
  // generic `unexpected`, and must inject the details into the code's
  // message and hint templates. Downstream grammars (@tabnas/csv's
  // csv_extra_field / csv_missing_field, @tabnas/multisource's
  // multisource_cycle) depend on this.
  // Mirrors go/badtokencode_test.go.
  it('custom bad-token error codes propagate', () => {
    const makeParser = (raise) => {
      const p = new Tabnas({
        error: { probe_bad: 'probe failed on row idx {rowidx}' },
        hint: { probe_bad: 'row idx {rowidx} is the problem' },
      })
      p.options({ rule: { start: 'top', exclude: 'tabnas,imp' } })
      const { ZZ } = p.token
      const { VAL } = p.tokenSet
      p.rule('top', (rs) =>
        rs.open([{ s: [VAL] }]).close([{ s: [ZZ] }]).bc(raise))
      return p
    }

    let err
    try {
      makeParser((r, ctx) => ctx.t0.bad('probe_bad', { rowidx: 7 })).parse('abc')
    } catch (e) {
      err = e
    }
    assert.equal(err.code, 'probe_bad')
    assert.match(err.message, /probe failed on row idx 7/)
    assert.match(err.message, /row idx 7 is the problem/)
    assert.equal(err.details.rowidx, 7)

    // Only a token carrying a code raises: an action that returns a
    // token with no `err` is not an error signal at all (rules.ts checks
    // `out.isToken && out.err`). The `tkn.err || unexpected` fallback
    // covers the engine's own no-alt-matched path instead — Go's
    // equivalent is pinned in go/badtokencode_test.go.
    let quiet
    try {
      quiet = makeParser((r, ctx) => ctx.t0).parse('abc')
    } catch (e) {
      quiet = e
    }
    assert.equal(quiet, undefined)
  })
})
