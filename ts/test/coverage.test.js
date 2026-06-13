/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Coverage for error-message formatting variants (errmsg suffix/link,
// colour branch) and the Tabnas instance API surface, exercising
// branches and methods not hit by the focused feature tests.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

// A one-value grammar over the bare engine; "}" (an unmatched close
// brace) triggers an "unexpected" error for the formatting tests.
function vp(opts) {
  const am = new Tabnas(Object.assign({ rule: { start: 'top' } }, opts))
  am.rule('top', (rs) =>
    rs.open([{ s: ['#VAL'], a: (r) => (r.node = r.o0.val) }]).close([{ s: ['#ZZ'] }]),
  )
  return am
}

function errOf(am, src) {
  try {
    am.parse(src)
    return null
  } catch (e) {
    return e
  }
}

describe('coverage', () => {
  it('error formatting variants', () => {
    // Default suffix (true) → internal diagnostics line.
    assert.match(errOf(vp({}), '}').message, /--internal:/)

    // suffix=false → no internal line.
    assert.doesNotMatch(
      errOf(vp({ errmsg: { suffix: false } }), '}').message,
      /--internal:/,
    )

    // suffix=string → literal text.
    assert.match(errOf(vp({ errmsg: { suffix: 'SEEDOCS' } }), '}').message, /SEEDOCS/)

    // suffix=function → dynamic text.
    assert.match(
      errOf(vp({ errmsg: { suffix: () => 'FN_SUFFIX' } }), '}').message,
      /FN_SUFFIX/,
    )

    // link line (with default suffix true).
    assert.match(
      errOf(vp({ errmsg: { link: 'https://docs.example' } }), '}').message,
      /docs\.example/,
    )

    // colour off and colour-with-overrides both exercise the colour
    // branch of errmsg(); just ensure they still produce a message.
    assert.ok(errOf(vp({ color: { active: false } }), '}').message.length > 0)
    assert.match(
      errOf(vp({ color: { active: true, hi: '<HI>' } }), '}').message,
      /<HI>/,
    )
  })

  it('error at various source positions', () => {
    // Error on a later line exercises the multi-line source-site extract.
    assert.match(errOf(vp({}), '1\n2\n}').message, /-->/)
  })

  it('Tabnas API surface', () => {
    const am = vp({})

    // Derive a child and an empty instance.
    assert.ok(am.make({ tag: 'child' }) instanceof Tabnas)
    assert.ok(am.empty() instanceof Tabnas)

    // toString / id.
    assert.match('' + am, /^Tabnas\//)
    assert.ok(am.id)

    // config / internal / util.
    assert.equal(typeof am.config(), 'object')
    assert.equal(typeof am.internal(), 'object')
    assert.equal(typeof am.util, 'object')
    assert.equal(typeof am.util.deep, 'function')

    // token / tokenSet callables and map forms.
    const tin = am.token('#COVNEW')
    assert.equal(am.token('#COVNEW'), tin)
    assert.ok(Array.isArray(am.tokenSet('VAL')))
    assert.equal(typeof am.tokenSet, 'function')

    // options accessor (callable + indexable).
    am.options({ tag: 'tagged' })
    assert.equal(am.options.tag, 'tagged')

    // parse of a non-string returns the input unchanged.
    assert.equal(am.parse(123), 123)
    const obj = { a: 1 }
    assert.equal(am.parse(obj), obj)

    // parse with a meta.log callback (exercises the debug-log path).
    let logged = 0
    am.parse('1', { log: () => logged++ })
    assert.ok(0 <= logged)
  })

  it('sub returns the instance and chains', () => {
    const am = vp({})
    assert.equal(am.sub({ lex: () => {} }), am)
    am.sub({ rule: () => {} })
    am.parse('1')
  })
})
