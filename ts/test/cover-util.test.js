/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Coverage tests for utility helpers (makelog/srcfmt/str/clone/
// modlist/parserwrap/resolveFuncRefs) and error helpers (trimstk,
// errmsg prefix/suffix forms, errdesc failure path, prop guards).

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, util, SKIP, makePoint } = require('..')
const { resolveFuncRefs } = require('../dist/utility')
const { errmsg } = require('../dist/error')
const { loadTSV } = require('./utility')

const {
  makelog,
  srcfmt,
  str,
  clone,
  prop,
  trimstk,
  errdesc,
  omap,
} = util

const { modlist } = require('../dist/utility')

describe('cover-util', () => {
  it('loadTSV-unescapes-line-escapes', () => {
    // The TSV loader converts \r\n, \n and \r escapes into real
    // line-ending characters. Fixture lives next to this test.
    const rows = loadTSV('../../ts/test/cover-escapes')
    assert.deepEqual(rows[0].cols, ['a\r\nb', 'x\ny'])
    assert.deepEqual(rows[1].cols, ['c\rd', 'e\r\nf'])
  })

  it('makelog-function-meta', () => {
    let f = () => {}
    let ctx = { opts: {} }
    assert.equal(makelog(ctx, { log: f }), f)
    assert.equal(ctx.log, f)
  })

  it('srcfmt-array-props-and-null', () => {
    let F = srcfmt({ debug: { maxlen: 99, print: {} } })
    assert.equal(F(null), '')
    let arr = [1, 2]
    arr.x = 9
    assert.equal(F(arr), '[1,2, x: 9]')
  })

  it('str-handles-unstringifiable', () => {
    let c = {}
    c.self = c
    assert.equal(str(c), '[object Object]')
  })

  it('clone-preserves-prototype', () => {
    let p = makePoint(4, 3, 2, 1)
    let q = clone(p)
    assert.notEqual(p, q)
    assert.equal(q.sI, 3)
    assert.equal('' + q, 'Point[3/4,2,1]')
  })

  it('modlist-custom', () => {
    assert.deepEqual(
      modlist([1, 2], { custom: (list) => [...list, 3] }),
      [1, 2, 3],
    )
    // Custom returning null keeps the original list.
    assert.deepEqual(modlist([1, 2], { custom: () => null }), [1, 2])
    // Custom also runs for empty lists.
    assert.deepEqual(modlist([], { custom: () => [7] }), [7])
  })

  it('parserwrap-custom-parser', () => {
    // Normal result passthrough.
    let j = new Tabnas({ parser: { start: (src) => ({ ok: src }) } })
    assert.deepEqual(j.parse('x'), { ok: 'x' })

    // SyntaxError with parseable location info.
    let j2 = new Tabnas({
      parser: {
        start: () => {
          throw new SyntaxError('Unexpected token z in JSON at position 5')
        },
      },
    })
    assert.throws(
      () => j2.parse('abc\ndefgh'),
      (e) => 'json' === e.code && 1 === e.lineNumber && 2 === e.columnNumber,
    )

    // SyntaxError without location info.
    let j3 = new Tabnas({
      parser: {
        start: () => {
          throw new SyntaxError('plain bad')
        },
      },
    })
    assert.throws(() => j3.parse('abc'), (e) => 'json' === e.code)

    // Non-SyntaxError exceptions pass through unchanged.
    let j4 = new Tabnas({
      parser: {
        start: () => {
          throw new RangeError('nope')
        },
      },
    })
    assert.throws(() => j4.parse('abc'), RangeError)
  })

  it('resolveFuncRefs-forms', () => {
    const fn = () => {}

    // '@@' escape produces a literal '@' string.
    assert.equal(resolveFuncRefs('@@x'), '@x')
    // '@SKIP' resolves to the SKIP sentinel.
    assert.equal(resolveFuncRefs('@SKIP'), SKIP)
    // '@/pattern/flags' builds a RegExp.
    assert.deepEqual(resolveFuncRefs('@/ab/i'), /ab/i)
    // Function reference lookup.
    assert.equal(resolveFuncRefs('@f', { '@f': fn }), fn)
    // Unknown reference stays a string.
    assert.equal(resolveFuncRefs('@missing', {}), '@missing')
    // '@'-ref without a ref map stays a string.
    assert.equal(resolveFuncRefs('@nofref'), '@nofref')
    // Plain values pass through.
    assert.equal(resolveFuncRefs('plain'), 'plain')
    assert.equal(resolveFuncRefs(5), 5)
    assert.equal(resolveFuncRefs(null), null)
    // Arrays recurse.
    assert.deepEqual(resolveFuncRefs([1, '@@y']), [1, '@y'])
    // Non-plain objects are preserved without recursion.
    let re = /q/
    assert.equal(resolveFuncRefs(re), re)
    // Plain objects recurse.
    assert.deepEqual(resolveFuncRefs({ a: '@@b', c: { d: '@/x/' } }), {
      a: '@b',
      c: { d: /x/ },
    })
  })

  it('omap-no-mapper', () => {
    assert.deepEqual(omap({ a: 1 }), { a: 1 })
    assert.deepEqual(omap(null), {})
  })

  it('trimstk-filters-internal-frames', () => {
    let err = {
      stack: [
        'Error: x',
        '    at inner (/p/tabnas/tabnas.js:1:1)',
        '    at outer (/p/app.js:2:2)',
      ].join('\n'),
    }
    trimstk(err)
    assert.equal(err.stack, 'Error: x\nat outer (/p/app.js:2:2)')
    // No stack at all is tolerated.
    trimstk({})
  })

  it('errmsg-prefix-forms', () => {
    let m1 = errmsg({
      prefix: (color, spec) => 'P:' + spec.code,
      code: 'c0',
      txts: { msg: 'm0' },
    })
    assert.ok(m1.startsWith('P:c0\n'))

    let m2 = errmsg({ prefix: 'PRE', code: 'c0', txts: { msg: 'm0' } })
    assert.ok(m2.startsWith('PRE\n'))
  })

  it('errmsg-suffix-options', () => {
    // String suffix configured via options.errmsg.
    let j = new Tabnas({
      errmsg: { suffix: 'MY-SUFFIX' },
      lex: { empty: false },
    })
    assert.throws(
      () => j.parse(''),
      (e) => e.message.includes('MY-SUFFIX'),
    )

    // Function suffix.
    let j2 = new Tabnas({
      errmsg: { suffix: () => 'FN-SUFFIX' },
      lex: { empty: false },
    })
    assert.throws(
      () => j2.parse(''),
      (e) => e.message.includes('FN-SUFFIX'),
    )
  })

  it('errdesc-internal-failure-returns-empty', () => {
    // ctx.src throwing makes errdesc fall into its catch-all.
    let d = errdesc(
      'code0',
      {},
      { tin: 1 },
      {},
      {
        src: () => {
          throw new Error('errdesc-internal-fail (expected test noise)')
        },
      },
    )
    assert.deepEqual(d, {})
  })

  it('prop-guards-and-error-message', () => {
    // __proto__ paths are rejected.
    assert.throws(() => prop({}, '__proto__', 1), /Cannot set path/)
    assert.throws(() => prop({}, '__proto__.x', 1), /Cannot set path/)
    assert.throws(() => prop({}, 'a.__proto__', 1), /Cannot set path/)
    assert.throws(() => prop({}, '__proto__'), /Cannot get path/)

    // Unstringifiable root objects still produce an error message.
    let c = {}
    c.self = c
    assert.throws(
      () => prop(c, '__proto__'),
      /Cannot get path __proto__ on object: \[object Object\]/,
    )
  })
})
