/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama, jsonic, json: jsonPlugin, AmagamaError } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)

const j = am

describe('variant', function () {
  it('just-json-happy', () => {
    let json = new Amagama({ plugins: [jsonPlugin] })

    assert.deepEqual(json.parse('{"a":1}'), { a: 1 })
    assert.deepEqual(
      json.parse('{"a":1,"b":"x","c":true,"d":{"e":[-1.1e2,{"f":null}]}}'), { a: 1, b: 'x', c: true, d: { e: [-1.1e2, { f: null }] } })
    assert.deepEqual(json.parse(' "a" '), 'a')
    assert.deepEqual(json.parse('\r\n\t1.0\n'), 1.0)

    // NOTE: as per JSON.parse
    assert.deepEqual(json.parse('{"a":1,"a":2}'), { a: 2 })

    // console.log(json.parse('{"a":1,}'))

    assert.throws(() => json.parse('{a:1}'), /unexpected.*:1:2/s)
    assert.throws(() => json.parse('{"a":1,}'), /unexpected.*:1:8/s)
    assert.throws(() => json.parse('[a]'), /unexpected.*:1:2/s)
    assert.throws(() => json.parse('["a",]'), /unexpected.*:1:6/s)
    assert.throws(() => json.parse('"a" # foo'), /unexpected.*:1:5/s)
    assert.throws(() => json.parse('0xA'), /unexpected.*:1:1/s)
    assert.throws(() => json.parse('`a`'), /unexpected.*:1:1/s)
    assert.throws(() => json.parse("'a'"), /unexpected.*:1:1/s)
    assert.throws(() => json.parse(''), /unexpected.*:1:1/s)
    assert.throws(() => json.parse('{"a":1'), /unexpected.*:1:7/s)
    assert.throws(() => json.parse('[,a]'), /unexpected.*:1:2/s)
    assert.throws(() => json.parse(''), /unexpected/s)
    assert.throws(() => json.parse('00'), /unexpected/s)
    assert.throws(() => json.parse('{0:1}'), /unexpected/s)
    assert.throws(() => json.parse('["a"00,"b"]'), /unexpected/s)
    assert.throws(() => json.parse('[{}00,"b"]'), /unexpected/s)
  })

  // TODO: move to plugin
  /*
  it('comment-suffix', () => {
    let js = am.make()

    let jc = am.make({
      comment: {
        def: {
          hash: { suffix: 'makeLineMatcher' },
        },
      },
    })

    let tknlogS = []
    js.sub({
      lex: (tkn) => {
        tknlogS.push(tkn)
      },
    })

    let tknlogC = []
    jc.sub({
      lex: (tkn) => {
        tknlogC.push(tkn)
      },
    })

    assert.deepEqual(js.parse('a#b \nc'), ['a', 'c'])
    assert.deepEqual(jc.parse('a#b \nc'), ['a', 'c'])

    // console.log(''+tknlogS)
    // console.log(''+tknlogC)

    assert.deepEqual('' + tknlogS, 
      'Token[#TX=10 a 0,1,1],Token[#CM=7 #b  1,1,2],Token[#LN=6 . 4,1,5],Token[#TX=10 c 5,2,1],Token[#ZZ=2  6,2,2],Token[#ZZ=2  6,2,2],Token[#ZZ=2  6,2,2],Token[#ZZ=2  6,2,2]'
    )

    assert.deepEqual('' + tknlogC, 
      'Token[#TX=10 a 0,1,1],Token[#CM=7 #b  1,1,2],Token[#TX=10 c 5,2,1],Token[#ZZ=2  6,2,2],Token[#ZZ=2  6,2,2],Token[#ZZ=2  6,2,2],Token[#ZZ=2  6,2,2]'
    )

    tknlogC.length = 0
    assert.deepEqual(jc.parse('a#b \n\n\nc'), ['a', 'c'])
    // console.log(''+tknlogC)
    assert.deepEqual('' + tknlogC, 
      'Token[#TX=10 a 0,1,1],Token[#CM=7 #b  1,1,2],Token[#TX=10 c 7,4,1],Token[#ZZ=2  8,4,2],Token[#ZZ=2  8,4,2],Token[#ZZ=2  8,4,2],Token[#ZZ=2  8,4,2]'
    )
  })
  */

  it('line-lex-single', () => {
    let j = am
    let js = am.make({
      line: { single: true },
    })

    let tknlog = []
    j.sub({
      lex: (tkn) => {
        tknlog.push(tkn)
      },
    })

    let tknlogS = []
    js.sub({
      lex: (tkn) => {
        tknlogS.push(tkn)
      },
    })

    assert.deepEqual(j.parse('a\n\nb'), ['a', 'b'])
    // console.log(''+tknlog)
    assert.deepEqual('' + tknlog, 
      'Token[#TX=10 a 0,1,1],' +
        'Token[#LN=6 .. 1,1,2],' +
        'Token[#TX=10 b 3,3,1],Token[#ZZ=2  4,3,2],Token[#ZZ=2  4,3,2],Token[#ZZ=2  4,3,2],Token[#ZZ=2  4,3,2]',
    )

    assert.deepEqual(js.parse('a\n\nb'), ['a', 'b'])
    // console.log(''+tknlogS)
    assert.deepEqual('' + tknlogS, 
      'Token[#TX=10 a 0,1,1],' +
        'Token[#LN=6 . 1,1,2],Token[#LN=6 . 2,2,1],' +
        'Token[#TX=10 b 3,3,1],Token[#ZZ=2  4,3,2],Token[#ZZ=2  4,3,2],Token[#ZZ=2  4,3,2],Token[#ZZ=2  4,3,2]',
    )
  })
})
