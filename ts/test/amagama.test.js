/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

// const Util = require('util')

// let Lab = require('@hapi/lab')
// Lab = null != Lab.script ? Lab : require('hapi-lab-shim')

// const lab = (exports.lab = Lab.script())
// const describe = lab.describe
// const it = lab.it

// const I = Util.inspect

const { Amagama, jsonic, AmagamaError, makeRule, makeRuleSpec } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { loadTSV } = require('./utility')
const Exhaust = require('./exhaust')
const Large = require('./large')
const JsonStandard = require('./json-standard')

let j = am

function tsvTest(name) {
  const entries = loadTSV(name)
  for (const { cols: [input, expected], row } of entries) {
    try {
      assert.deepEqual(J(input), JSON.parse(expected))
    } catch (err) {
      err.message = `${name} row ${row}: input=${input} expected=${expected}\n${err.message}`
      throw err
    }
  }
}

describe('amagama', function () {
  it('happy', () => {
    tsvTest('happy')
  })

  it('options', () => {
    let j = am.make({ x: 1 })

    assert.deepEqual(j.options.x, 1)
    assert.deepEqual(Object.keys({ x: 1 }).reduce((a,k)=>(a[k]=({ ...j.options })[k],a),{}), { x: 1 })

    j.options({ x: 2 })
    assert.deepEqual(j.options.x, 2)
    assert.deepEqual(Object.keys({ x: 2 }).reduce((a,k)=>(a[k]=({ ...j.options })[k],a),{}), { x: 2 })

    j.options()
    assert.deepEqual(j.options.x, 2)

    j.options(null)
    assert.deepEqual(j.options.x, 2)

    j.options('ignored')
    assert.deepEqual(j.options.x, 2)

    assert.deepEqual(j.options.comment.lex, true)
    assert.deepEqual(j.options().comment.lex, true)
    assert.deepEqual(j.internal().config.comment.lex, true)
    j.options({ comment: { lex: false } })
    assert.deepEqual(j.options.comment.lex, false)
    assert.deepEqual(j.options().comment.lex, false)
    assert.deepEqual(j.internal().config.comment.lex, false)

    let k = am.make()
    assert.deepEqual(k.options.comment.lex, true)
    assert.deepEqual(k.options().comment.lex, true)
    assert.deepEqual(k.internal().config.comment.lex, true)
    assert.deepEqual(k.rule().val.def.open.length > 4, true)
    k.use((amagama) => {
      amagama.options({
        comment: { lex: false },
        rule: { include: 'json' },
      })
    })

    assert.deepEqual(k.options.comment.lex, false)
    assert.deepEqual(k.options().comment.lex, false)
    assert.deepEqual(k.internal().config.comment.lex, false)
    assert.deepEqual(k.rule().val.def.open.length, 3)

    let k1 = am.make()
    k1.use((amagama) => {
      amagama.options({
        rule: { exclude: 'json' },
      })
    })
    // console.log(k1.rule().val.def.open)
    assert.deepEqual(k1.rule().val.def.open.length, 6)
  })

  it('token-gen', () => {
    let j = am.make()

    let suffix = Math.random()
    let s = j.token('__' + suffix)

    let s1 = j.token('AA' + suffix)
    assert.deepEqual(s1, s + 1)
    assert.deepEqual(j.token['AA' + suffix], s + 1)
    assert.deepEqual(j.token[s + 1], 'AA' + suffix)
    assert.deepEqual(j.token('AA' + suffix), s + 1)
    assert.deepEqual(j.token(s + 1), 'AA' + suffix)

    let s1a = j.token('AA' + suffix)
    assert.deepEqual(s1a, s + 1)
    assert.deepEqual(j.token['AA' + suffix], s + 1)
    assert.deepEqual(j.token[s + 1], 'AA' + suffix)
    assert.deepEqual(j.token('AA' + suffix), s + 1)
    assert.deepEqual(j.token(s + 1), 'AA' + suffix)

    let s2 = j.token('BB' + suffix)
    assert.deepEqual(s2, s + 2)
    assert.deepEqual(j.token['BB' + suffix], s + 2)
    assert.deepEqual(j.token[s + 2], 'BB' + suffix)
    assert.deepEqual(j.token('BB' + suffix), s + 2)
    assert.deepEqual(j.token(s + 2), 'BB' + suffix)
  })

  it('token-fixed', () => {
    let j = am.make()

    assert.deepEqual({ ...j.fixed }, {
      12: '{',
      13: '}',
      14: '[',
      15: ']',
      16: ':',
      17: ',',
      '{': 12,
      '}': 13,
      '[': 14,
      ']': 15,
      ':': 16,
      ',': 17,
    })

    assert.deepEqual(j.fixed('{'), 12)
    assert.deepEqual(j.fixed('}'), 13)
    assert.deepEqual(j.fixed('['), 14)
    assert.deepEqual(j.fixed(']'), 15)
    assert.deepEqual(j.fixed(':'), 16)
    assert.deepEqual(j.fixed(','), 17)

    assert.deepEqual(j.fixed(12), '{')
    assert.deepEqual(j.fixed(13), '}')
    assert.deepEqual(j.fixed(14), '[')
    assert.deepEqual(j.fixed(15), ']')
    assert.deepEqual(j.fixed(16), ':')
    assert.deepEqual(j.fixed(17), ',')

    j.options({
      fixed: {
        token: {
          '#A': 'a',
          '#BB': 'bb',
        },
      },
    })

    assert.deepEqual({ ...j.fixed }, {
      12: '{',
      13: '}',
      14: '[',
      15: ']',
      16: ':',
      17: ',',
      18: 'a',
      19: 'bb',
      '{': 12,
      '}': 13,
      '[': 14,
      ']': 15,
      ':': 16,
      ',': 17,
      a: 18,
      bb: 19,
    })

    assert.deepEqual(j.fixed('{'), 12)
    assert.deepEqual(j.fixed('}'), 13)
    assert.deepEqual(j.fixed('['), 14)
    assert.deepEqual(j.fixed(']'), 15)
    assert.deepEqual(j.fixed(':'), 16)
    assert.deepEqual(j.fixed(','), 17)
    assert.deepEqual(j.fixed('a'), 18)
    assert.deepEqual(j.fixed('bb'), 19)

    assert.deepEqual(j.fixed(12), '{')
    assert.deepEqual(j.fixed(13), '}')
    assert.deepEqual(j.fixed(14), '[')
    assert.deepEqual(j.fixed(15), ']')
    assert.deepEqual(j.fixed(16), ':')
    assert.deepEqual(j.fixed(17), ',')
    assert.deepEqual(j.fixed(18), 'a')
    assert.deepEqual(j.fixed(19), 'bb')
  })

  it('basic-json', () => {
    tsvTest('amagama-basic-json')
  })

  it('basic-object-tree', () => {
    tsvTest('amagama-basic-object-tree')
  })

  it('basic-array-tree', () => {
    tsvTest('amagama-basic-array-tree')
  })

  it('basic-mixed-tree', () => {
    tsvTest('amagama-basic-mixed-tree')
  })

  it('syntax-errors', () => {
    // bad close
    assert.throws(() => j.parse('}'))
    assert.throws(() => j.parse(']'))

    // top level already is a map
    assert.throws(() => j.parse('a:1,2'))

    // values not valid inside map
    assert.throws(() => j.parse('x:{1,2}'))
  })

  it('process-scalars', () => {
    tsvTest('amagama-process-scalars')
  })

  it('process-text', () => {
    tsvTest('amagama-process-text')
  })

  it('process-implicit-object', () => {
    tsvTest('amagama-process-implicit-object')
  })

  it('process-object-tree', () => {
    tsvTest('amagama-process-object-tree')
  })

  it('process-array', () => {
    tsvTest('amagama-process-array')
  })

  it('process-mixed-nodes', () => {
    tsvTest('amagama-process-mixed-nodes')
  })

  it('process-comment', () => {
    assert.deepEqual(j.parse('a:q\nb:w #X\nc:r \n\nd:t\n\n#'), {
      a: 'q',
      b: 'w',
      c: 'r',
      d: 't',
    })

    let jm = j.make({ comment: { lex: false } })
    assert.deepEqual(jm.parse('a:q\nb:w#X\nc:r \n\nd:t'), {
      a: 'q',
      b: 'w#X',
      c: 'r',
      d: 't',
    })
  })

  it('process-whitespace', () => {
    tsvTest('amagama-process-whitespace')
  })

  it('funky-keys', () => {
    tsvTest('amagama-funky-keys')
  })

  it('api', () => {
    assert.deepEqual(J('a:1'), { a: 1 })
    assert.deepEqual(Amagama.parse('a:1'), { a: 1 })
  })

  it('rule-spec', () => {
    let cfg = {}

    let rs0 = j.makeRuleSpec({}, cfg, {})
    assert.deepEqual(rs0.name, '')
    assert.deepEqual(rs0.def.open, [])
    assert.deepEqual(rs0.def.close, [])

    let rs1 = j.makeRuleSpec({}, cfg, {
      open: [
        {},
        { c: () => true },
        { c: (r) => r.lte() },
        { c: {} },
      ],
    })
    
    assert.deepEqual(rs1.def.open[0].c, undefined)
    assert.deepEqual(typeof rs1.def.open[1].c === 'function', true)
    assert.deepEqual(typeof rs1.def.open[2].c === 'function', true)

    let rs2 = j.makeRuleSpec({}, cfg, {
      open: [
        { c: (r) => r.lte('a', 10) && r.lte('b', 20) },
      ],
    })
    let c0 = rs2.def.open[0].c
    let mr = (n) => {
      let r = makeRule({ name: '', def: {} }, { uI: 0 })
      r.n = n
      return r
    }
    assert.deepEqual(c0(mr({})), true)
    assert.deepEqual(c0(mr({ a: 5 })), true)
    assert.deepEqual(c0(mr({ a: 10 })), true)
    assert.deepEqual(c0(mr({ a: 15 })), false)
    assert.deepEqual(c0(mr({ b: 19 })), true)
    assert.deepEqual(c0(mr({ b: 20 })), true)
    assert.deepEqual(c0(mr({ b: 21 })), false)

    assert.deepEqual(c0(mr({ a: 10, b: 20 })), true)
    assert.deepEqual(c0(mr({ a: 10, b: 21 })), false)
    assert.deepEqual(c0(mr({ a: 11, b: 21 })), false)
    assert.deepEqual(c0(mr({ a: 11, b: 20 })), false)
  })

  it('id-string', function () {
    let s0 = '' + Amagama
    assert.ok(s0.match(/Amagama.*/) != null)
    assert.deepEqual('' + Amagama, s0)
    assert.deepEqual('' + Amagama, '' + Amagama)

    let j1 = am.make()
    let s1 = '' + j1
    assert.ok(s1.match(/Amagama.*/) != null)
    assert.deepEqual('' + j1, s1)
    assert.deepEqual('' + j1, '' + j1)
    assert.notDeepEqual(s0, s1)

    let j2 = am.make({ tag: 'foo' })
    let s2 = '' + j2
    assert.ok(s2.match(/Amagama.*foo/) != null)
    assert.deepEqual('' + j2, s2)
    assert.deepEqual('' + j2, '' + j2)
    assert.notDeepEqual(s0, s2)
    assert.notDeepEqual(s1, s2)
  })

  // Test against all combinations of chars up to `len`
  // NOTE: coverage tracing slows this down - a lot!
  it('exhaust-perf', function () {
    let len = 2

    // Use this env var for debug-code-test loop to avoid
    // slowing things down. Do run this test for builds!
    if (null == process.env.AMAGAMA_TEST_SKIP_PERF) {
      let out = Exhaust(len)

      // NOTE: if parse algo changes then these may change.
      // But if *not intended* changes here indicate unexpected effects.
      assert.deepEqual(Object.keys({
        rmc: 62734,
        emc: 2292,
        ecc: {
          unprintable: 91,
          unexpected: 1508,
          unterminated_string: 692,
          unterminated_comment: 1,
        },
      }).reduce((a,k)=>(a[k]=(out)[k],a),{}), {
        rmc: 62734,
        emc: 2292,
        ecc: {
          unprintable: 91,
          unexpected: 1508,
          unterminated_string: 692,
          unterminated_comment: 1,
        },
      })
    }
  })

  it('large-perf', function () {
    let len = 12345 // Coverage really nerfs this test sadly
    // let len = 520000 // Pretty much the V8 string length limit

    // Use this env var for debug-code-test loop to avoid
    // slowing things down. Do run this test for builds!
    if (null == process.env.AMAGAMA_TEST_SKIP_PERF) {
      let out = Large(len)

      // NOTE: if parse algo changes then these may change.
      // But if *not intended* changes here indicate unexpected effects.
      assert.deepEqual(Object.keys({
        ok: true,
        len: len * 1000,
      }).reduce((a,k)=>(a[k]=(out)[k],a),{}), {
        ok: true,
        len: len * 1000,
      })
    }
  })

  // Validate pure JSON to ensure Amagama is always a superset.
  it('json-standard', function () {
    JsonStandard(Amagama)
  })

  it('src-not-string', () => {
    assert.deepEqual(J({}), {})
    assert.deepEqual(J([]), [])
    assert.deepEqual(J(true), true)
    assert.deepEqual(J(false), false)
    assert.deepEqual(J(null), null)
    assert.deepEqual(J(undefined), undefined)
    assert.deepEqual(J(1), 1)
    assert.deepEqual(J(/a/), /a/)

    let sa = Symbol('a')
    assert.deepEqual(J(sa), sa)
  })

  it('src-empty-string', () => {
    assert.deepEqual(J(''), undefined)

    assert.throws(() => am.make({ lex: { empty: false } }).parse(''), /unexpected.*:1:1/s,)
  })
})

function make_empty(opts) {
  let j = am.make(opts)
  let rns = j.rule()
  Object.keys(rns).map((rn) => j.rule(rn, null))
  return j
}
