/* Copyright (c) 2021 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

// const Util = require('util')

// let Lab = require('@hapi/lab')
// Lab = null != Lab.script ? Lab : require('hapi-lab-shim')

// 
// const lab = (exports.lab = Lab.script())
// const describe = lab.describe
// const it = lab.it
// 
const {
  Amagama,
  Parser,
  AmagamaError,
  OPEN,
  CLOSE,
  BEFORE,
  AFTER,
  make,
} = require('..')

describe('doc', function () {
  it('method-amagama', () => {
    let earth = Amagama('name: Terra, moons: [{name: Luna}]')
    assert.deepEqual(earth, {
      name: 'Terra',
      moons: [
        {
          name: 'Luna',
        },
      ],
    })
  })

  // TODO: test without actually writing to STDOUT
  // it('method-amagama-log', () => {
  //   let one = Amagama('1', {log:-1}) // one === 1
  //   expect(one).equal(1)
  // })

  it('method-make', () => {
    let array_of_numbers = Amagama('1,2,3')
    // array_of_numbers === [1, 2, 3]
    assert.deepEqual(array_of_numbers, [1, 2, 3])

    let no_numbers_please = Amagama.make({ number: { lex: false } })
    let array_of_strings = no_numbers_please('1,2,3')
    // array_of_strings === ['1', '2', '3']
    assert.deepEqual(array_of_strings, ['1', '2', '3'])
  })

  it('method-make-inherit', () => {
    let no_numbers_please = Amagama.make({ number: { lex: false } })
    let out = no_numbers_please('1,2,3') // === ['1', '2', '3'] as before
    assert.deepEqual(out, ['1', '2', '3'])

    let pipe_separated = no_numbers_please.make({
      fixed: { token: { '#CA': '|' } },
    })
    out = pipe_separated('1|2|3') // === ['1', '2', '3'], but:
    assert.deepEqual(out, ['1', '2', '3'])
    out = pipe_separated('1,2,3') // === '1,2,3' !!!
    assert.deepEqual(out, '1,2,3')
  })

  it('method-options', () => {
    let amagama = Amagama.make()

    let options = amagama.options()
    assert.deepEqual(options.comment.lex, true)
    assert.deepEqual(amagama.options.comment.lex, true)

    let no_comment = Amagama.make()
    no_comment.options({ comment: { lex: false } })
    assert.deepEqual(no_comment.options().comment.lex, false)
    assert.deepEqual(no_comment.options.comment.lex, false)

    // Returns {"a": 1, "#b": 2}
    let out = no_comment(`
   a: 1
   #b: 2
 `)
    assert.deepEqual(out, { a: 1, '#b': 2 })

    // Whereas this returns only {"a": 1} as # starts a one line comment
    out = Amagama(`
  a: 1
  #b: 2
`)
    assert.deepEqual(out, { a: 1 })
  })

  it('method-use', () => {
    let amagama = Amagama.make().use(function piper(amagama) {
      amagama.options({ fixed: { token: { '#CA': '~' } } })
    })

    assert.deepEqual(amagama.options.fixed.token['#CA'], '~')
    assert.deepEqual(amagama.internal().config.fixed.token['~'], 17)

    let out = amagama('a~b~c') // === ['a', 'b', 'c']
    assert.deepEqual(out, ['a', 'b', 'c'])
  })

  it('method-use-options', () => {
    function sepper(amagama) {
      let sep = amagama.options.plugin.sepper.sep
      amagama.options({ fixed: { token: { '#CA': sep } } })
    }
    let amagama = Amagama.make().use(sepper, { sep: ';' })
    let out = amagama('a;b;c') // === ['a', 'b', 'c']
    assert.deepEqual(out, ['a', 'b', 'c'])
  })

  it('method-use-chaining', () => {
    function foo(amagama) {
      amagama.foo = function () {
        return 1
      }
    }
    function bar(amagama) {
      amagama.bar = function () {
        return this.foo() * 2
      }
    }
    let amagama = Amagama.make().use(foo).use(bar)
    assert.deepEqual(amagama.foo(), 1)
    assert.deepEqual(amagama.bar(), 2)
  })

  it('method-rule', () => {
    let concat = Amagama.make()
    assert.deepEqual(Object.keys(concat.rule()), [
      'val',
      'map',
      'list',
      'pair',
      'elem',
    ])

    assert.deepEqual(concat.rule('val').name, 'val')

    let ST = concat.token.ST
    concat.rule('val', (rulespec) => {
      //rulespec.def.open.unshift({
      rulespec.open([
        {
          s: [ST, ST],
          a: (rule, ctx) => (rule.node = rule.o0.val + rule.o1.val),
        },
      ])
    })

    assert.deepEqual(concat('"a" "b"', { xlog: -1 }), 'ab')
    assert.deepEqual(concat('["a" "b"]', { xlog: -1 }), ['ab'])
    assert.deepEqual(concat('{x:"a" "b",y:1}', { xlog: -1 }), { x: 'ab', y: 1 })

    concat.options({
      fixed: { token: { '#HH': '%' } },
    })

    let HH = concat.token.HH

    concat.rule('hundred', (rs) => rs.ao((rule) => (rule.node = 100)))

    concat.rule('val', (rulespec) => {
      rulespec.open([{ s: [HH], p: 'hundred' }])
    })

    assert.deepEqual(concat('{x:1, y:%}', { xlog: -1 }), { x: 1, y: 100 })
  })

  /* METHOD REMOVED FROM API
  it('method-lex', () => {
    let tens = Amagama.make()

    tens.lex((cfg, opts) => (lex, rule) => {
      let pnt = lex.pnt
      let marks = lex.src.substring(pnt.sI).match(/^%+/)
      if (marks) {
        let len = marks[0].length
        let tkn = lex.token('#VL', 10 * marks[0].length, marks, lex.pnt)
        pnt.sI += len
        pnt.cI += len
        return tkn
      }
    })

    assert.deepEqual(tens('a:1,b:%%,c:[%%%%]'), { a: 1, b: 20, c: [40] })
  })
  */

  it('method-token', () => {
    let amagama = Amagama.make()
    amagama.token.ST // === 11, String token identification number
    amagama.token(11) // === '#ST', String token name
    amagama.token('#ST') // === 11, String token name
  })

  it('property-id', () => {
    assert.deepEqual(null != Amagama.id.match(/Amagama.*/), true)
    assert.deepEqual(null != Amagama.make({ tag: 'foo' }).id.match(/Amagama.*foo/), true)
  })
})
