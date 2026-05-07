/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const Util = require('util')
const I = Util.inspect

const { Amagama, jsonic, AmagamaError, RuleSpec } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { loadTSV } = require('./utility')

const j = am

const JS = (x) => JSON.stringify(x)
const JP = (x) => JSON.parse(JSON.stringify(x))

function tsvTest(name) {
  const entries = loadTSV(name)
  for (const { cols: [input, expected], row } of entries) {
    try {
      assert.deepEqual(JP(J(input)), JSON.parse(expected))
    } catch (err) {
      err.message = `${name} row ${row}: input=${input} expected=${expected}\n${err.message}`
      throw err
    }
  }
}

function tsvTestWith(name, opts) {
  const instance = am.make(opts)
  const entries = loadTSV(name)
  for (const { cols: [input, expected], row } of entries) {
    try {
      assert.deepEqual(JP(instance.parse(input)), JSON.parse(expected))
    } catch (err) {
      err.message = `${name} row ${row}: input=${input} expected=${expected}\n${err.message}`
      throw err
    }
  }
}

function tsvTestListChild(name, opts) {
  const instance = am.make(opts)
  const entries = loadTSV(name)
  for (const { cols, row } of entries) {
    const [input, expectedArray, expectedChild] = cols
    try {
      let result = instance.parse(input)
      assert.deepEqual(JS(result), expectedArray)
      if (expectedChild !== undefined && expectedChild !== '') {
        assert.deepEqual(JP(result['child$']), JSON.parse(expectedChild))
      } else {
        assert.deepEqual(result['child$'], undefined)
      }
    } catch (err) {
      err.message = `${name} row ${row}: input=${input} expected_array=${expectedArray} expected_child=${expectedChild}\n${err.message}`
      throw err
    }
  }
}

describe('feature', function () {
  it('test-util-match', () => {
    assert.equal(match(1, 1), undefined)
    assert.deepEqual(match([], [1]), '$[0]/val:undefined!=1')
    assert.equal(match([], []), undefined)
    assert.equal(match([1], [1]), undefined)
    assert.equal(match([[]], [[]]), undefined)
    assert.deepEqual(match([1], [2]), '$[0]/val:1!=2')
    assert.deepEqual(match([[1]], [[2]]), '$[0][0]/val:1!=2')
    assert.equal(match({}, {}), undefined)
    assert.equal(match({ a: 1 }, { a: 1 }), undefined)
    assert.deepEqual(match({ a: 1 }, { a: 2 }), '$.a/val:1!=2')
    assert.equal(match({ a: { b: 1 } }, { a: { b: 1 } }), undefined)
    assert.deepEqual(match({ a: 1 }, { a: 1, b: 2 }), '$.b/val:undefined!=2')
    assert.deepEqual(match({ a: 1 }, { b: 1 }), '$.b/val:undefined!=1')
    assert.deepEqual(match({ a: { b: 1 } }, { a: { b: 2 } }), '$.a.b/val:1!=2')
    assert.equal(match({ a: 1, b: 2 }, { a: 1 }), undefined)
    assert.deepEqual(match({ a: 1, b: 2 }, { a: 1 }, { miss: false }), 
      '$/key:{a,b}!={a}',
    )
    assert.equal(match([1], []), undefined)
    assert.deepEqual(match([], [1]), '$[0]/val:undefined!=1')
    assert.deepEqual(match([2, 1], [undefined, 1], { miss: false }), 
      '$[0]/val:2!=undefined',
    )
    assert.equal(match([2, 1], [undefined, 1]), undefined)
  })

  it('single-char', () => {
    assert.deepEqual(j.parse(), undefined)
    assert.deepEqual(j.parse(''), undefined)
    assert.deepEqual(j.parse('À'), 'À') // #192 verbatim text
    assert.deepEqual(j.parse(' '), ' ') // #160 non-breaking space, verbatim text
    assert.deepEqual(j.parse('{'), {}) // auto-close
    assert.deepEqual(j.parse('a'), 'a') // verbatim text
    assert.deepEqual(j.parse('['), []) // auto-close
    assert.deepEqual(j.parse(','), [null]) // implict list, prefixing-comma means null element
    assert.deepEqual(j.parse('#'), undefined) // comment
    assert.deepEqual(j.parse(' '), undefined) // ignored space
    assert.deepEqual(j.parse('\u0010'), '\x10') // verbatim text
    assert.deepEqual(j.parse('\b'), '\b') // verbatim
    assert.deepEqual(j.parse('\t'), undefined) // ignored space
    assert.deepEqual(j.parse('\n'), undefined) // ignored newline
    assert.deepEqual(j.parse('\f'), '\f') // verbatim
    assert.deepEqual(j.parse('\r'), undefined) // ignored newline

    assert.throws(() => j.parse('"'), /unterminated/)
    assert.throws(() => j.parse("'"), /unterminated/)
    assert.throws(() => j.parse(':'), /unexpected/)
    assert.throws(() => j.parse(']'), /unexpected/)
    assert.throws(() => j.parse('`'), /unterminated/)
    assert.throws(() => j.parse('}'), /unexpected/)
  })

  it('number', () => {
    assert.deepEqual(j.parse('1'), 1)
    assert.deepEqual(j.parse('-1'), -1)
    assert.deepEqual(j.parse('+1'), 1)
    assert.deepEqual(j.parse('0'), 0)

    assert.deepEqual(j.parse('1.'), 1)
    assert.deepEqual(j.parse('-1.'), -1)
    assert.deepEqual(j.parse('+1.'), 1)
    assert.deepEqual(j.parse('0.'), 0)

    assert.deepEqual(j.parse('.1'), 0.1)
    assert.deepEqual(j.parse('-.1'), -0.1)
    assert.deepEqual(j.parse('+.1'), 0.1)
    assert.deepEqual(j.parse('.0'), 0)

    assert.deepEqual(j.parse('0.9'), 0.9)
    assert.deepEqual(j.parse('-0.9'), -0.9)
    assert.deepEqual(j.parse('[1]'), [1])
    assert.deepEqual(j.parse('a:1'), { a: 1 })
    assert.deepEqual(j.parse('1:a'), { 1: 'a' })
    assert.deepEqual(j.parse('{a:1}'), { a: 1 })
    assert.deepEqual(j.parse('{1:a}'), { 1: 'a' })
    assert.deepEqual(j.parse('1.2'), 1.2)
    assert.deepEqual(j.parse('1e2'), 100)
    assert.deepEqual(j.parse('10_0'), 100)
    assert.deepEqual(j.parse('-1.2'), -1.2)
    assert.deepEqual(j.parse('-1e2'), -100)
    assert.deepEqual(j.parse('-10_0'), -100)
    assert.deepEqual(j.parse('1e+2'), 100)
    assert.deepEqual(j.parse('1e-2'), 0.01)

    assert.deepEqual(j.parse('0xA'), 10)
    assert.deepEqual(j.parse('0xa'), 10)
    assert.deepEqual(j.parse('+0xA'), 10)
    assert.deepEqual(j.parse('+0xa'), 10)
    assert.deepEqual(j.parse('-0xA'), -10)
    assert.deepEqual(j.parse('-0xa'), -10)

    assert.deepEqual(j.parse('0o12'), 10)
    assert.deepEqual(j.parse('0b1010'), 10)
    assert.deepEqual(j.parse('0x_A'), 10)
    assert.deepEqual(j.parse('0x_a'), 10)
    assert.deepEqual(j.parse('0o_12'), 10)
    assert.deepEqual(j.parse('0b_1010'), 10)
    assert.deepEqual(j.parse('1e6:a'), { '1e6': 'a' }) // NOTE: "1e6" not "1000000"
    assert.deepEqual(j.parse('01'), 1)
    assert.deepEqual(j.parse('-01'), -1)
    assert.deepEqual(j.parse('0099'), 99)
    assert.deepEqual(j.parse('-0099'), -99)

    assert.deepEqual(j.parse('a:1'), { a: 1 })
    assert.deepEqual(j.parse('a:-1'), { a: -1 })
    assert.deepEqual(j.parse('a:+1'), { a: 1 })
    assert.deepEqual(j.parse('a:0'), { a: 0 })
    assert.deepEqual(j.parse('a:0.1'), { a: 0.1 })
    assert.deepEqual(j.parse('a:[1]'), { a: [1] })
    assert.deepEqual(j.parse('a:a:1'), { a: { a: 1 } })
    assert.deepEqual(j.parse('a:1:a'), { a: { 1: 'a' } })
    assert.deepEqual(j.parse('a:{a:1}'), { a: { a: 1 } })
    assert.deepEqual(j.parse('a:{1:a}'), { a: { 1: 'a' } })
    assert.deepEqual(j.parse('a:1.2'), { a: 1.2 })
    assert.deepEqual(j.parse('a:1e2'), { a: 100 })
    assert.deepEqual(j.parse('a:10_0'), { a: 100 })
    assert.deepEqual(j.parse('a:-1.2'), { a: -1.2 })
    assert.deepEqual(j.parse('a:-1e2'), { a: -100 })
    assert.deepEqual(j.parse('a:-10_0'), { a: -100 })
    assert.deepEqual(j.parse('a:1e+2'), { a: 100 })
    assert.deepEqual(j.parse('a:1e-2'), { a: 0.01 })
    assert.deepEqual(j.parse('a:0xA'), { a: 10 })
    assert.deepEqual(j.parse('a:0xa'), { a: 10 })
    assert.deepEqual(j.parse('a:0o12'), { a: 10 })
    assert.deepEqual(j.parse('a:0b1010'), { a: 10 })
    assert.deepEqual(j.parse('a:0x_A'), { a: 10 })
    assert.deepEqual(j.parse('a:0x_a'), { a: 10 })
    assert.deepEqual(j.parse('a:0o_12'), { a: 10 })
    assert.deepEqual(j.parse('a:0b_1010'), { a: 10 })
    assert.deepEqual(j.parse('a:1e6:a'), { a: { '1e6': 'a' } }) // NOTE: "1e6" not "1000000"
    assert.deepEqual(j.parse('[1,0]'), [1, 0])
    assert.deepEqual(j.parse('[1,0.5]'), [1, 0.5])

    // text as +- not value enders
    assert.deepEqual(j.parse('1+'), '1+')
    assert.deepEqual(j.parse('1-'), '1-')
    assert.deepEqual(j.parse('1-+'), '1-+')

    // partial numbers are converted to text
    assert.deepEqual(j.parse('-'), '-')
    assert.deepEqual(j.parse('+'), '+')
    assert.deepEqual(j.parse('1a'), '1a')

    let jn = j.make({ number: { lex: false } })
    assert.deepEqual(jn.parse('1'), '1') // Now it's a string.
    assert.deepEqual(j.parse('1'), 1)
    assert.deepEqual(jn.parse('a:1'), { a: '1' })
    assert.deepEqual(j.parse('a:1'), { a: 1 })

    let jh = j.make({ number: { hex: false } })
    assert.deepEqual(jh.parse('1'), 1)
    assert.deepEqual(jh.parse('0x10'), '0x10')
    assert.deepEqual(jh.parse('0o20'), 16)
    assert.deepEqual(jh.parse('0b10000'), 16)
    assert.deepEqual(j.parse('1'), 1)
    assert.deepEqual(j.parse('0x10'), 16)
    assert.deepEqual(j.parse('0o20'), 16)
    assert.deepEqual(j.parse('0b10000'), 16)

    let jo = j.make({ number: { oct: false } })
    assert.deepEqual(jo.parse('1'), 1)
    assert.deepEqual(jo.parse('0x10'), 16)
    assert.deepEqual(jo.parse('0o20'), '0o20')
    assert.deepEqual(jo.parse('0b10000'), 16)
    assert.deepEqual(j.parse('1'), 1)
    assert.deepEqual(j.parse('0x10'), 16)
    assert.deepEqual(j.parse('0o20'), 16)
    assert.deepEqual(j.parse('0b10000'), 16)

    let jb = j.make({ number: { bin: false } })
    assert.deepEqual(jb.parse('1'), 1)
    assert.deepEqual(jb.parse('0x10'), 16)
    assert.deepEqual(jb.parse('0o20'), 16)
    assert.deepEqual(jb.parse('0b10000'), '0b10000')
    assert.deepEqual(j.parse('1'), 1)
    assert.deepEqual(j.parse('0x10'), 16)
    assert.deepEqual(j.parse('0o20'), 16)
    assert.deepEqual(j.parse('0b10000'), 16)

    let js0 = j.make({ number: { sep: null } })
    assert.deepEqual(js0.parse('1_0'), '1_0')
    assert.deepEqual(j.parse('1_0'), 10)

    let js1 = j.make({ number: { sep: ' ' } })
    assert.deepEqual(js1.parse('1 0'), 10)
    assert.deepEqual(js1.parse('a:1 0'), { a: 10 })
    assert.deepEqual(js1.parse('a:1 0, b : 2 000 '), { a: 10, b: 2000 })
    assert.deepEqual(j.parse('1_0'), 10)
  })

  it('value-standard', () => {
    assert.deepEqual(j.parse(''), undefined)

    assert.deepEqual(j.parse('true'), true)
    assert.deepEqual(j.parse('false'), false)
    assert.deepEqual(j.parse('null'), null)

    assert.deepEqual(j.parse('true\n'), true)
    assert.deepEqual(j.parse('false\n'), false)
    assert.deepEqual(j.parse('null\n'), null)

    assert.deepEqual(j.parse('true#'), true)
    assert.deepEqual(j.parse('false#'), false)
    assert.deepEqual(j.parse('null#'), null)

    assert.deepEqual(j.parse('true//'), true)
    assert.deepEqual(j.parse('false//'), false)
    assert.deepEqual(j.parse('null//'), null)

    assert.deepEqual(j.parse('{a:true}'), { a: true })
    assert.deepEqual(j.parse('{a:false}'), { a: false })
    assert.deepEqual(j.parse('{a:null}'), { a: null })

    assert.deepEqual(j.parse('{true:1}'), { true: 1 })
    assert.deepEqual(j.parse('{false:1}'), { false: 1 })
    assert.deepEqual(j.parse('{null:1}'), { null: 1 })

    assert.deepEqual(j.parse('a:true'), { a: true })
    assert.deepEqual(j.parse('a:false'), { a: false })
    assert.deepEqual(j.parse('a:null'), { a: null })
    assert.deepEqual(j.parse('a:'), { a: null })

    assert.deepEqual(j.parse('true,'), [true])
    assert.deepEqual(j.parse('false,'), [false])
    assert.deepEqual(j.parse('null,'), [null])

    assert.deepEqual(
      j.parse('a:true,b:false,c:null,d:{e:true,f:false,g:null},h:[true,false,null]'), {
      a: true,
      b: false,
      c: null,
      d: { e: true, f: false, g: null },
      h: [true, false, null],
    })
  })

  it('value-custom', () => {
    let jv0 = j.make({
      number: { lex: false }, // needed for commadigits
      value: {
        def: {
          foo: { val: 99 },
          bar: { val: { x: 1 } },
          zed: {
            match: /Z(\d)/,
            val: (res) => +res[1],
          },
          qaz: {
            match: /HEX<(.+)>/,
            val: (res) => {
              let val = parseInt(res[1], 16)
              if (isNaN(val)) {
                let e = new Error('Bad hex: ' + res[0])
                e.code = 'badhex'
                throw e
              }
              return val
            },
          },

          // Stops at tokens
          cap: {
            match: /[A-Z]+/,
            val: (res) => res[0].toLowerCase(),
          },

          // Does not stop at tokens
          commadigits: {
            match: /^\d+(,\d+)+/,
            val: (res) => 20 * +res[0].replace(/,/g, ''),
            consume: true,
          },
        },
      },
    })

    assert.deepEqual(jv0.parse(''), undefined)
    assert.deepEqual(jv0.parse('foo'), 99)
    assert.deepEqual(jv0.parse('bar'), { x: 1 })
    assert.deepEqual(jv0.parse('a:foo'), { a: 99 })
    assert.deepEqual(jv0.parse('a:bar'), { a: { x: 1 } })

    assert.deepEqual(jv0.parse('a:Z1'), { a: 1 })
    assert.deepEqual(jv0.parse('a:Zx'), { a: 'Zx' })

    assert.deepEqual(jv0.parse('a:HEX<>'), { a: 'HEX<>' })
    assert.deepEqual(jv0.parse('a:HEX<a>'), { a: 10 })
    assert.throws(() => jv0.parse('a:HEX<x>'), /badhex/)

    assert.deepEqual(jv0.parse('[A,B]'), ['a', 'b'])
    assert.deepEqual(jv0.parse('[1 2,3] '), ['1', 460])
  })

  it('match-custom', () => {
    let jv0 = j
      .make({
        match: {
          value: {
            foobar: {
              match: /foobar(\d)/,
              val: (res) => +res[1],
            },

            // no need to turn of number lexing
            commadigits: {
              match: /^\d+(,\d+)+/,
              val: (res) => 20 * +res[0].replace(/,/g, ''),
            },
          },
          token: {
            FOO: /foo/,
          },
        },
      })
      .rule('val', (rs, p) => {
        rs.open({ s: [p.cfg.t.FOO], a: (r) => (r.node = 'Foo') })
      })

    assert.deepEqual(jv0.parse('foo'), 'Foo')
    assert.deepEqual(jv0.parse('foobar1'), 1)

    // Still parses numbers
    assert.deepEqual(jv0.parse('[1 2,3 4]'), [1, 460, 4])
  })

  it('null-or-undefined', () => {
    // All ignored, so undefined
    assert.deepEqual(j.parse(''), undefined)
    assert.deepEqual(j.parse(' '), undefined)
    assert.deepEqual(j.parse('\n'), undefined)
    assert.deepEqual(j.parse('#'), undefined)
    assert.deepEqual(j.parse('//'), undefined)
    assert.deepEqual(j.parse('/**/'), undefined)

    // JSON only has nulls
    assert.deepEqual(j.parse('null'), null)
    assert.deepEqual(j.parse('a:null'), { a: null })

    assert.deepEqual(JS(j.parse('[a:1]')), '[]')
    assert.deepEqual(j.parse('[a:1]').a, 1)

    assert.deepEqual(j.parse('[{a:null}]'), [{ a: null }])

    assert.deepEqual(JS(j.parse('[a:null]')), '[]')
    assert.deepEqual(j.parse('[a:null]').a, null)

    assert.deepEqual(j.parse('a:null,b:null'), { a: null, b: null })
    assert.deepEqual(j.parse('{a:null,b:null}'), { a: null, b: null })

    assert.deepEqual(JS(j.parse('[a:]')), '[]')
    assert.deepEqual(j.parse('[a:]').a, null)

    assert.deepEqual(JS(j.parse('[a:,]')), '[]')
    assert.deepEqual(j.parse('[a:,]').a, null)

    assert.deepEqual(JS(j.parse('[a:,b:]')), '[]')
    assert.deepEqual({ ...j.parse('[a:,b:]') }, { a: null, b: null })

    assert.deepEqual(JS(j.parse('[a:,b:c:]')), '[]')
    assert.deepEqual({ ...j.parse('[a:,b:c:]') }, { a: null, b: { c: null } })

    assert.deepEqual(j.parse('a:'), { a: null })
    assert.deepEqual(j.parse('a:,b:'), { a: null, b: null })
    assert.deepEqual(j.parse('a:,b:c:'), { a: null, b: { c: null } })

    assert.deepEqual(j.parse('{a:}'), { a: null })
    assert.deepEqual(j.parse('{a:,b:}'), { a: null, b: null })
    assert.deepEqual(j.parse('{a:,b:c:}'), { a: null, b: { c: null } })
  })

  it('value-text', () => {
    assert.deepEqual(j.parse('a'), 'a')
    assert.deepEqual(j.parse('1a'), '1a') // NOTE: not a number!
    assert.deepEqual(j.parse('a/b'), 'a/b')
    assert.deepEqual(j.parse('a#b'), 'a')

    assert.deepEqual(j.parse('a//b'), 'a')
    assert.deepEqual(j.parse('a/*b*/'), 'a')
    assert.deepEqual(j.parse('a\\n'), 'a\\n')
    assert.deepEqual(j.parse('\\s+'), '\\s+')

    assert.deepEqual(j.parse('x:a'), { x: 'a' })
    assert.deepEqual(j.parse('x:a/b'), { x: 'a/b' })
    assert.deepEqual(j.parse('x:a#b'), { x: 'a' })
    assert.deepEqual(j.parse('x:a//b'), { x: 'a' })
    assert.deepEqual(j.parse('x:a/*b*/'), { x: 'a' })
    assert.deepEqual(j.parse('x:a\\n'), { x: 'a\\n' })
    assert.deepEqual(j.parse('x:\\s+'), { x: '\\s+' })

    assert.deepEqual(j.parse('[a]'), ['a'])
    assert.deepEqual(j.parse('[a/b]'), ['a/b'])
    assert.deepEqual(j.parse('[a#b]'), ['a'])
    assert.deepEqual(j.parse('[a//b]'), ['a'])
    assert.deepEqual(j.parse('[a/*b*/]'), ['a'])
    assert.deepEqual(j.parse('[a\\n]'), ['a\\n'])
    assert.deepEqual(j.parse('[\\s+]'), ['\\s+'])

    // TODO: REVIEW
    // // Force text re to fail (also tests infinite loop protection).
    // let j0 = j.make()
    // j0.internal().config.re.te =
    //   new RegExp(j0.internal().config.re.te.source.replace('#','#a'))
    // expect(()=>j0.parse('a')).throw(/unexpected/)
  })

  it('value-string', () => {
    assert.deepEqual(j.parse("''"), '')
    assert.deepEqual(j.parse('""'), '')
    assert.deepEqual(j.parse('``'), '')

    assert.deepEqual(j.parse("'a'"), 'a')
    assert.deepEqual(j.parse('"a"'), 'a')
    assert.deepEqual(j.parse('`a`'), 'a')

    assert.deepEqual(j.parse("'a b'"), 'a b')
    assert.deepEqual(j.parse('"a b"'), 'a b')
    assert.deepEqual(j.parse('`a b`'), 'a b')

    assert.deepEqual(j.parse("'a\\tb'"), 'a\tb')
    assert.deepEqual(j.parse('"a\\tb"'), 'a\tb')
    assert.deepEqual(j.parse('`a\\tb`'), 'a\tb')

    // NOTE: backslash inside string is always removed
    assert.deepEqual(j.parse('`a\\qb`'), 'aqb')

    assert.deepEqual(j.parse("'a\\'b\"`c'"), 'a\'b"`c')
    assert.deepEqual(j.parse('"a\\"b`\'c"'), 'a"b`\'c')
    assert.deepEqual(j.parse('`a\\`b"\'c`'), 'a`b"\'c')

    assert.deepEqual(j.parse('"\\u0061"'), 'a')
    assert.deepEqual(j.parse('"\\x61"'), 'a')

    assert.deepEqual(j.parse('`\n`'), '\n')
    assert.throws(() => j.parse('"\n"'), /unprintable]/)
    assert.throws(() => j.parse('"\t"'), /unprintable]/)
    assert.throws(() => j.parse('"\f"'), /unprintable]/)
    assert.throws(() => j.parse('"\b"'), /unprintable]/)
    assert.throws(() => j.parse('"\v"'), /unprintable]/)
    assert.throws(() => j.parse('"\0"'), /unprintable]/)

    assert.deepEqual(j.parse('"\\n"'), '\n')
    assert.deepEqual(j.parse('"\\t"'), '\t')
    assert.deepEqual(j.parse('"\\f"'), '\f')
    assert.deepEqual(j.parse('"\\b"'), '\b')
    assert.deepEqual(j.parse('"\\v"'), '\v')
    assert.deepEqual(j.parse('"\\""'), '"')
    assert.deepEqual(j.parse('"\\\'"'), "'")
    assert.deepEqual(j.parse('"\\`"'), '`')

    assert.deepEqual(j.parse('"\\w"'), 'w')
    assert.deepEqual(j.parse('"\\0"'), '0')

    assert.throws(() => j.parse('`\x1a`'), /unprintable]/)
    assert.throws(() => j.parse('"\x1a"'), /unprintable]/)

    assert.throws(() => j.parse('"x'), /unterminated_string].*:1:1/s)
    assert.throws(() => j.parse(' "x'), /unterminated_string].*:1:2/s)
    assert.throws(() => j.parse('  "x'), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse('a:"x'), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse('aa:"x'), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse('aaa:"x'), /unterminated_string].*:1:5/s)
    assert.throws(() => j.parse(' a:"x'), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse(' a :"x'), /unterminated_string].*:1:5/s)

    assert.throws(() => j.parse("'x"), /unterminated_string].*:1:1/s)
    assert.throws(() => j.parse(" 'x"), /unterminated_string].*:1:2/s)
    assert.throws(() => j.parse("  'x"), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse("a:'x"), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse("aa:'x"), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse("aaa:'x"), /unterminated_string].*:1:5/s)
    assert.throws(() => j.parse(" a:'x"), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse(" a :'x"), /unterminated_string].*:1:5/s)

    assert.throws(() => j.parse('`x'), /unterminated_string].*:1:1/s)
    assert.throws(() => j.parse(' `x'), /unterminated_string].*:1:2/s)
    assert.throws(() => j.parse('  `x'), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse('a:`x'), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse('aa:`x'), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse('aaa:`x'), /unterminated_string].*:1:5/s)
    assert.throws(() => j.parse(' a:`x'), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse(' a :`x'), /unterminated_string].*:1:5/s)

    assert.throws(() => j.parse('`\nx'), /unterminated_string].*:1:1/s)
    assert.throws(() => j.parse(' `\nx'), /unterminated_string].*:1:2/s)
    assert.throws(() => j.parse('  `\nx'), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse('a:`\nx'), /unterminated_string].*:1:3/s)
    assert.throws(() => j.parse('aa:`\nx'), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse('aaa:`\nx'), /unterminated_string].*:1:5/s)
    assert.throws(() => j.parse(' a:`\nx'), /unterminated_string].*:1:4/s)
    assert.throws(() => j.parse(' a :`\nx'), /unterminated_string].*:1:5/s)

    assert.throws(() => j.parse('\n\n"x'), /unterminated_string].*:3:1/s)
    assert.throws(() => j.parse('\n\n "x'), /unterminated_string].*:3:2/s)
    assert.throws(() => j.parse('\n\n  "x'), /unterminated_string].*:3:3/s)
    assert.throws(() => j.parse('\n\na:"x'), /unterminated_string].*:3:3/s)
    assert.throws(() => j.parse('\n\naa:"x'), /unterminated_string].*:3:4/s)
    assert.throws(() => j.parse('\n\naaa:"x'), /unterminated_string].*:3:5/s)
    assert.throws(() => j.parse('\n\n a:"x'), /unterminated_string].*:3:4/s)
    assert.throws(() => j.parse('\n\n a :"x'), /unterminated_string].*:3:5/s)

    // string.escape.allowUnknown:false
    let j1 = j.make({ string: { allowUnknown: false } })
    assert.deepEqual(j1.parse('"\\n"'), '\n')
    assert.deepEqual(j1.parse('"\\t"'), '\t')
    assert.deepEqual(j1.parse('"\\f"'), '\f')
    assert.deepEqual(j1.parse('"\\b"'), '\b')
    assert.deepEqual(j1.parse('"\\v"'), '\v')
    assert.deepEqual(j1.parse('"\\""'), '"')
    assert.deepEqual(j1.parse('"\\\\"'), '\\')
    assert.throws(() => j1.parse('"\\w"'), /unexpected].*:1:3/s)
    assert.throws(() => j1.parse('"\\0"'), /unexpected].*:1:3/s)

    // TODO: PLUGIN csv
    // let k = j.make({string:{escapedouble:true}})
    // expect(k.parse('"a""b"')).equal('a"b')
    // expect(k.parse('`a``b`')).equal('a`b')
    // expect(k.parse('\'a\'\'b\'')).equal('a\'b')
  })

  it('multiline-string', () => {
    assert.deepEqual(j.parse('`a`'), 'a')
    assert.deepEqual(j.parse('`\na`'), '\na')
    assert.deepEqual(j.parse('`\na\n`'), '\na\n')
    assert.deepEqual(j.parse('`a\nb`'), 'a\nb')
    assert.deepEqual(j.parse('`a\n\nb`'), 'a\n\nb')
    assert.deepEqual(j.parse('`a\nc\nb`'), 'a\nc\nb')
    assert.deepEqual(j.parse('`a\r\n\r\nb`'), 'a\r\n\r\nb')

    assert.throws(() => j.parse('`\n'), /unterminated_string.*:1:1/s)
    assert.throws(() => j.parse(' `\n'), /unterminated_string.*:1:2/s)
    assert.throws(() => j.parse('\n `\n'), /unterminated_string.*:2:2/s)

    assert.throws(() => j.parse('`a``b'), /unterminated_string.*:1:4/s)
    assert.throws(() => j.parse('\n`a``b'), /unterminated_string.*:2:4/s)
    assert.throws(() => j.parse('\n`a`\n`b'), /unterminated_string.*:3:1/s)
    assert.throws(() => j.parse('\n`\na`\n`b'), /unterminated_string.*:4:1/s)
    assert.throws(() => j.parse('\n`\na`\n`\nb'), /unterminated_string.*:4:1/s)

    assert.throws(() => j.parse('`a` `b'), /unterminated_string.*:1:5/s)
    assert.throws(() => j.parse('`a`\n `b'), /unterminated_string.*:2:2/s)

    assert.throws(() => j.parse('`a\n` `b'), /unterminated_string.*:2:3/s)
    assert.throws(() => j.parse('`a\n`,`b'), /unterminated_string.*:2:3/s)
    assert.throws(() => j.parse('[`a\n` `b'), /unterminated_string.*:2:3/s)
    assert.throws(() => j.parse('[`a\n`,`b'), /unterminated_string.*:2:3/s)
    assert.throws(() => j.parse('1\n `b'), /unterminated_string.*:2:2/s)
    assert.throws(() => j.parse('[1\n,`b'), /unterminated_string.*:2:2/s)

    // TODO: PLUGIN
    // expect(j.parse("'''a\nb'''")).equal('a\nb')
    // expect(j.parse("'''\na\nb'''")).equal('a\nb')
    // expect(j.parse("'''\na\nb\n'''")).equal('a\nb')
    // expect(j.parse("\n'''\na\nb\n'''\n")).equal('a\nb')
    // expect(j.parse(" '''\na\nb\n''' ")).equal('a\nb')

    // expect(j.parse("''' a\nb\n'''")).equal(' a\nb')
    // expect(j.parse(" '''a\n b\n'''")).equal('a\nb')
    // expect(j.parse(" ''' \na\n b\n'''")).equal('a\nb')
    // expect(j.parse(" ''' \na\n  b\n'''")).equal('a\n b')
    // expect(j.parse(" ''' \na\nb\n'''")).equal('a\nb')
    // expect(j.parse(" ''' a\n b\n'''")).equal('a\nb')
    // expect(j.parse(" ''' a\nb\n'''")).equal('a\nb')

    //     expect(j.parse(`{
    //   md:
    //     '''
    //     First line.
    //     Second line.
    //       This line is indented by two spaces.
    //     '''
    // }`)).equal({
    //   md: "First line.\nSecond line.\n  This line is indented by two spaces.",
    // })

    // expect(j.parse("'''\na\nb\n'''")).equal('a\nb')
    // expect(j.parse("'''a\nb'''")).equal('a\nb')
  })

  it('implicit-object', () => {
    tsvTest('feature-implicit-object')
  })

  it('implicit-list', () => {
    // implicit null element preceeds empty comma
    assert.deepEqual(j.parse(','), [null])
    assert.deepEqual(j.parse(',a'), [null, 'a'])
    assert.deepEqual(j.parse(',"a"'), [null, 'a'])
    assert.deepEqual(j.parse(',1'), [null, 1])
    assert.deepEqual(j.parse(',true'), [null, true])
    assert.deepEqual(j.parse(',[]'), [null, []])
    assert.deepEqual(j.parse(',{}'), [null, {}])
    assert.deepEqual(j.parse(',[1]'), [null, [1]])
    assert.deepEqual(j.parse(',{a:1}'), [null, { a: 1 }])

    assert.deepEqual(JS(j.parse(',a:1')), '[null]')
    assert.deepEqual(j.parse(',a:1').a, 1)

    // Top level comma imlies list; ignore trailing comma
    assert.deepEqual(j.parse('a,'), ['a'])
    assert.deepEqual(j.parse('"a",'), ['a'])
    assert.deepEqual(j.parse('1,'), [1])
    assert.deepEqual(j.parse('1,,'), [1, null])
    assert.deepEqual(j.parse('1,,,'), [1, null, null])
    assert.deepEqual(j.parse('1,null'), [1, null])
    assert.deepEqual(j.parse('1,null,'), [1, null])
    assert.deepEqual(j.parse('1,null,null'), [1, null, null])
    assert.deepEqual(j.parse('1,null,null,'), [1, null, null])
    assert.deepEqual(j.parse('true,'), [true])
    assert.deepEqual(j.parse('[],'), [[]])
    assert.deepEqual(j.parse('{},'), [{}])
    assert.deepEqual(j.parse('[1],'), [[1]])
    assert.deepEqual(j.parse('{a:1},'), [{ a: 1 }])

    // NOTE: special case, this is considered a map pair
    assert.deepEqual(j.parse('a:1,'), { a: 1 })

    assert.deepEqual(j.parse('a,'), ['a'])
    assert.deepEqual(j.parse('"a",'), ['a'])
    assert.deepEqual(j.parse('true,'), [true])
    assert.deepEqual(j.parse('1,'), [1])
    assert.deepEqual(j.parse('a,1'), ['a', 1])
    assert.deepEqual(j.parse('"a",1'), ['a', 1])
    assert.deepEqual(j.parse('true,1'), [true, 1])
    assert.deepEqual(j.parse('1,1'), [1, 1])

    assert.deepEqual(j.parse('a,b'), ['a', 'b'])
    assert.deepEqual(j.parse('a,b,c'), ['a', 'b', 'c'])
    assert.deepEqual(j.parse('a,b,c,d'), ['a', 'b', 'c', 'd'])

    assert.deepEqual(j.parse('a b'), ['a', 'b'])
    assert.deepEqual(j.parse('a b c'), ['a', 'b', 'c'])
    assert.deepEqual(j.parse('a b c d'), ['a', 'b', 'c', 'd'])

    assert.deepEqual(j.parse('[a],[b]'), [['a'], ['b']])
    assert.deepEqual(j.parse('[a],[b],[c]'), [['a'], ['b'], ['c']])
    assert.deepEqual(j.parse('[a],[b],[c],[d]'), [['a'], ['b'], ['c'], ['d']])

    assert.deepEqual(j.parse('[a] [b]'), [['a'], ['b']])
    assert.deepEqual(j.parse('[a] [b] [c]'), [['a'], ['b'], ['c']])
    assert.deepEqual(j.parse('[a] [b] [c] [d]'), [['a'], ['b'], ['c'], ['d']])

    // TODO: note this in docs as it enables parsing of JSON logs/records
    assert.deepEqual(j.parse('{a:1} {b:1}'), [{ a: 1 }, { b: 1 }])
    assert.deepEqual(j.parse('{a:1} {b:1} {c:1}'), [{ a: 1 }, { b: 1 }, { c: 1 }])
    assert.deepEqual(j.parse('{a:1} {b:1} {c:1} {d:1}'), [
      { a: 1 },
      { b: 1 },
      { c: 1 },
      { d: 1 },
    ])
    assert.deepEqual(j.parse('\n{a:1}\n{b:1}\r\n{c:1}\n{d:1}\r\n'), [
      { a: 1 },
      { b: 1 },
      { c: 1 },
      { d: 1 },
    ])

    assert.deepEqual(j.parse('{a:1},'), [{ a: 1 }])
    assert.deepEqual(j.parse('[1],'), [[1]])

    assert.deepEqual(JS(j.parse('[a:1]')), '[]')
    assert.deepEqual({ ...j.parse('[a:1]') }, { a: 1 })

    assert.deepEqual(JS(j.parse('[a:1,b:2]')), '[]')
    assert.deepEqual({ ...j.parse('[a:1,b:2]') }, { a: 1, b: 2 })

    assert.deepEqual(JS(j.parse('[a:1,b:2,c:3]')), '[]')
    assert.deepEqual({ ...j.parse('[a:1,b:2,c:3]') }, { a: 1, b: 2, c: 3 })

    assert.deepEqual(JS(j.parse('[a:1,b:2,c:3,d:4]')), '[]')
    assert.deepEqual({ ...j.parse('[a:1,b:2,c:3,d:4]') }, { a: 1, b: 2, c: 3, d: 4 })
  })

  it('implicit-map', () => {
    tsvTest('feature-implicit-map')
  })

  it('nested-space-pairs', () => {
    tsvTest('feature-nested-space-pairs')
  })

  it('extension', () => {
    assert.deepEqual(j.parse('a:{b:1,c:2},a:{c:3,e:4}'), { a: { b: 1, c: 3, e: 4 } })

    assert.deepEqual(j.parse('a:{b:1,x:1},a:{b:2,y:2},a:{b:3,z:3}'), {
      a: { b: 3, x: 1, y: 2, z: 3 },
    })

    assert.deepEqual(j.parse('a:[{b:1,x:1}],a:[{b:2,y:2}],a:[{b:3,z:3}]'), {
      a: [{ b: 3, x: 1, y: 2, z: 3 }],
    })

    assert.deepEqual(j.parse('a:[{b:1},{x:1}],a:[{b:2},{y:2}],a:[{b:3},{z:3}]'), {
      a: [{ b: 3 }, { x: 1, y: 2, z: 3 }],
    })

    let k = j.make({ map: { extend: false } })
    assert.deepEqual(k.parse('a:{b:1,c:2},a:{c:3,e:4}'), { a: { c: 3, e: 4 } })
  })

  it('finish', () => {
    assert.deepEqual(j.parse('a:{b:'), { a: { b: null } })
    assert.deepEqual(j.parse('{a:{b:{c:1}'), { a: { b: { c: 1 } } })
    assert.deepEqual(j.parse('[[1'), [[1]])

    let k = j.make({ rule: { finish: false } })
    assert.throws(() => k.parse('a:{b:'), /end_of_source/)
    assert.throws(() => k.parse('{a:{b:{c:1}'), /end_of_source/)
    assert.throws(() => k.parse('[[1'), /end_of_source/)
  })

  it('property-dive', () => {
    assert.deepEqual(j.parse('{a:1,b:2}'), { a: 1, b: 2 })
    assert.deepEqual(j.parse('{a:1,b:{c:2}}'), { a: 1, b: { c: 2 } })
    assert.deepEqual(j.parse('{a:1,b:{c:2},d:3}'), { a: 1, b: { c: 2 }, d: 3 })
    assert.deepEqual(j.parse('{b:{c:2,e:4},d:3}'), { b: { c: 2, e: 4 }, d: 3 })

    assert.deepEqual(j.parse('{a:{b:{c:1,d:2},e:3},f:4}'), {
      a: { b: { c: 1, d: 2 }, e: 3 },
      f: 4,
    })
    assert.deepEqual(j.parse('a:b:c'), { a: { b: 'c' } })
    assert.deepEqual(j.parse('a:b:c, d:e:f'), { a: { b: 'c' }, d: { e: 'f' } })
    assert.deepEqual(j.parse('a:b:c\nd:e:f'), { a: { b: 'c' }, d: { e: 'f' } })

    assert.deepEqual(j.parse('a:b:c,d:e'), { a: { b: 'c' }, d: 'e' })
    assert.deepEqual(j.parse('a:b:c:1,d:e'), { a: { b: { c: 1 } }, d: 'e' })
    assert.deepEqual(j.parse('a:b:c:f:{g:1},d:e'), {
      a: { b: { c: { f: { g: 1 } } } },
      d: 'e',
    })
    assert.deepEqual(j.parse('c:f:{g:1,h:2},d:e'), { c: { f: { g: 1, h: 2 } }, d: 'e' })
    assert.deepEqual(j.parse('c:f:[{g:1,h:2}],d:e'), {
      c: { f: [{ g: 1, h: 2 }] },
      d: 'e',
    })

    assert.deepEqual(j.parse('a:b:c:1\nd:e'), { a: { b: { c: 1 } }, d: 'e' })

    assert.deepEqual(j.parse('[{a:1,b:2}]'), [{ a: 1, b: 2 }])
    assert.deepEqual(j.parse('[{a:1,b:{c:2}}]'), [{ a: 1, b: { c: 2 } }])
    assert.deepEqual(j.parse('[{a:1,b:{c:2},d:3}]'), [{ a: 1, b: { c: 2 }, d: 3 }])
    assert.deepEqual(j.parse('[{b:{c:2,e:4},d:3}]'), [{ b: { c: 2, e: 4 }, d: 3 }])

    assert.deepEqual(j.parse('[{a:{b:{c:1,d:2},e:3},f:4}]'), [
      { a: { b: { c: 1, d: 2 }, e: 3 }, f: 4 },
    ])

    assert.deepEqual(JS(j.parse('[a:b:c]')), '[]')
    assert.deepEqual(j.parse('[a:b:c]').a, { b: 'c' })

    // NOTE: this validates that array props also merge!
    assert.deepEqual(JS(j.parse('[a:b:c, a:d:e]')), '[]')
    assert.deepEqual({ ...j.parse('[a:b:c, a:d:e]') }, { a: { b: 'c', d: 'e' } })

    assert.deepEqual(JS(j.parse('[a:b:c, d:e:f]')), '[]')
    assert.deepEqual({ ...j.parse('[a:b:c, d:e:f]') }, { a: { b: 'c' }, d: { e: 'f' } })

    assert.deepEqual(JS(j.parse('[a:b:c\nd:e:f]')), '[]')
    assert.deepEqual({ ...j.parse('[a:b:c\nd:e:f]') }, { a: { b: 'c' }, d: { e: 'f' } })

    assert.deepEqual(JS(j.parse('[a:b:c,d:e]')), '[]')
    assert.deepEqual({ ...j.parse('[a:b:c,d:e]') }, { a: { b: 'c' }, d: 'e' })

    assert.deepEqual(JS(j.parse('[a:b:c:1,d:e]')), '[]')
    assert.deepEqual({ ...j.parse('[a:b:c:1,d:e]') }, { a: { b: { c: 1 } }, d: 'e' })

    assert.deepEqual(JS(j.parse('[a:b:c:f:{g:1},d:e]')), '[]')
    assert.deepEqual({ ...j.parse('[a:b:c:f:{g:1},d:e]') }, {
      a: { b: { c: { f: { g: 1 } } } },
      d: 'e',
    })

    assert.deepEqual(JS(j.parse('[c:f:{g:1,h:2},d:e]')), '[]')
    assert.deepEqual({ ...j.parse('[c:f:{g:1,h:2},d:e]') }, {
      c: { f: { g: 1, h: 2 } },
      d: 'e',
    })

    assert.deepEqual(JS(j.parse('[c:f:[{g:1,h:2}],d:e]')), '[]')
    assert.deepEqual({ ...j.parse('[c:f:[{g:1,h:2}],d:e]') }, {
      c: { f: [{ g: 1, h: 2 }] },
      d: 'e',
    })

    assert.deepEqual(JS(j.parse('[a:b:c:1\nd:e]')), '[]')
    assert.deepEqual({ ...j.parse('[a:b:c:1\nd:e]') }, { a: { b: { c: 1 } }, d: 'e' })

    assert.deepEqual(j.parse('a:b:{x:1},a:b:{y:2}'), { a: { b: { x: 1, y: 2 } } })
    assert.deepEqual(j.parse('a:b:{x:1},a:b:{y:2},a:b:{z:3}'), {
      a: { b: { x: 1, y: 2, z: 3 } },
    })

    assert.deepEqual(j.parse('a:b:c:{x:1},a:b:c:{y:2}'), {
      a: { b: { c: { x: 1, y: 2 } } },
    })
    assert.deepEqual(j.parse('a:b:c:{x:1},a:b:c:{y:2},a:b:c:{z:3}'), {
      a: { b: { c: { x: 1, y: 2, z: 3 } } },
    })
  })

  it('list-property', () => {
    assert.deepEqual(j.parse('[a:1]').a, 1)

    let k = j.make({ list: { property: false } })
    assert.throws(() => k.parse('[a:1]'), /unexpected/)
  })

  it('list-pair', () => {
    tsvTestWith('feature-list-pair', { list: { pair: true } })
  })

  it('list-pair-interaction', () => {
    // === pair=false, property=true (default behavior) ===
    // Pairs become properties on the array object, not elements
    assert.deepEqual(JS(j.parse('[a:1]')), '[]')
    assert.deepEqual(j.parse('[a:1]').a, 1)
    assert.deepEqual(JS(j.parse('[a:1,b:2]')), '[]')
    assert.deepEqual({ ...j.parse('[a:1,b:2]') }, { a: 1, b: 2 })
    // Mixed: properties on array, values as elements
    assert.deepEqual(JS(j.parse('[a:1,2,b:3]')), '[2]')
    assert.deepEqual({ ...j.parse('[a:1,2,b:3]') }, { 0: 2, a: 1, b: 3 })

    // === pair=false, property=false ===
    // Pairs in lists are errors
    let pp_ff = j.make({ list: { pair: false, property: false } })
    assert.throws(() => pp_ff.parse('[a:1]'), /unexpected/)
    assert.throws(() => pp_ff.parse('[a:1,b:2]'), /unexpected/)
    // Plain list values still work
    assert.deepEqual(pp_ff.parse('[1,2,3]'), [1, 2, 3])
    assert.deepEqual(pp_ff.parse('[]'), [])
    // Explicit maps inside lists still work
    assert.deepEqual(pp_ff.parse('[{a:1}]'), [{ a: 1 }])
    assert.deepEqual(pp_ff.parse('[{a:1},{b:2}]'), [{ a: 1 }, { b: 2 }])

    // === pair=true, property=true ===
    // pair takes precedence: pairs become elements, not properties
    let pp_tt = j.make({ list: { pair: true, property: true } })
    assert.deepEqual(pp_tt.parse('[a:1]'), [{ a: 1 }])
    assert.deepEqual(pp_tt.parse('[a:1,b:2]'), [{ a: 1 }, { b: 2 }])
    assert.deepEqual(pp_tt.parse('[a:1,2,b:3]'), [{ a: 1 }, 2, { b: 3 }])
    assert.deepEqual(pp_tt.parse('[a:1,a:2]'), [{ a: 1 }, { a: 2 }])
    // Verify elements exist at numeric indices
    assert.deepEqual(pp_tt.parse('[a:1]')[0], { a: 1 })
    assert.deepEqual(JS(pp_tt.parse('[a:1]')), '[{"a":1}]')

    // === pair=true, property=false ===
    // pair takes precedence: pairs become elements (property disabled doesn't matter)
    let pp_tf = j.make({ list: { pair: true, property: false } })
    assert.deepEqual(pp_tf.parse('[a:1]'), [{ a: 1 }])
    assert.deepEqual(pp_tf.parse('[a:1,b:2]'), [{ a: 1 }, { b: 2 }])
    assert.deepEqual(pp_tf.parse('[a:1,2,b:3]'), [{ a: 1 }, 2, { b: 3 }])
    assert.deepEqual(pp_tf.parse('[a:b:c]'), [{ a: { b: 'c' } }])

    // === Braces: explicit maps interact correctly with list.pair ===
    let lp = j.make({ list: { pair: true } })
    // Explicit map as list element (no pairs involved)
    assert.deepEqual(lp.parse('[{a:1}]'), [{ a: 1 }])
    assert.deepEqual(lp.parse('[{a:1,b:2}]'), [{ a: 1, b: 2 }])
    // Mix of explicit maps and implicit pair objects
    assert.deepEqual(lp.parse('[{a:1},b:2]'), [{ a: 1 }, { b: 2 }])
    assert.deepEqual(lp.parse('[a:1,{b:2}]'), [{ a: 1 }, { b: 2 }])
    assert.deepEqual(lp.parse('[{a:1},b:2,{c:3}]'), [{ a: 1 }, { b: 2 }, { c: 3 }])
    // Pair value is an explicit map
    assert.deepEqual(lp.parse('[a:{x:1,y:2}]'), [{ a: { x: 1, y: 2 } }])
    assert.deepEqual(lp.parse('[a:{x:{y:1}}]'), [{ a: { x: { y: 1 } } }])
    // Nested map with pair inside
    assert.deepEqual(lp.parse('[{a:{b:1,c:2}},d:3]'), [{ a: { b: 1, c: 2 } }, { d: 3 }])

    // === Square brackets: nested lists interact correctly with list.pair ===
    // Pair value is a list
    assert.deepEqual(lp.parse('[a:[1,2,3]]'), [{ a: [1, 2, 3] }])
    assert.deepEqual(lp.parse('[a:[[1],[2]]]'), [{ a: [[1], [2]] }])
    // List element is a list (no pairs)
    assert.deepEqual(lp.parse('[[1,2],a:3]'), [[1, 2], { a: 3 }])
    assert.deepEqual(lp.parse('[a:1,[2,3],b:4]'), [{ a: 1 }, [2, 3], { b: 4 }])
    // Nested list.pair applies recursively to inner lists
    assert.deepEqual(lp.parse('[a:[b:1]]'), [{ a: [{ b: 1 }] }])
    assert.deepEqual(lp.parse('[a:[b:1,c:2]]'), [{ a: [{ b: 1 }, { c: 2 }] }])
    assert.deepEqual(lp.parse('[a:{b:[c:1,d:2]}]'), [{ a: { b: [{ c: 1 }, { d: 2 }] } }])
    // Deeply nested
    assert.deepEqual(lp.parse('[a:[b:[c:1]]]'), [{ a: [{ b: [{ c: 1 }] }] }])

    // === Maps outside lists are unaffected by list.pair ===
    assert.deepEqual(lp.parse('{a:1}'), { a: 1 })
    assert.deepEqual(lp.parse('{a:1,b:2}'), { a: 1, b: 2 })
    assert.deepEqual(lp.parse('a:1'), { a: 1 })
    assert.deepEqual(lp.parse('a:1,b:2'), { a: 1, b: 2 })
    assert.deepEqual(lp.parse('a:b:c'), { a: { b: 'c' } })

    // === Plain values in lists still work with list.pair ===
    assert.deepEqual(lp.parse('[1,2,3]'), [1, 2, 3])
    assert.deepEqual(lp.parse('[]'), [])
    assert.deepEqual(lp.parse('[true,false,null]'), [true, false, null])
    assert.deepEqual(lp.parse('["a","b"]'), ['a', 'b'])
  })

  it('list-child', () => {
    tsvTestListChild('feature-list-child', { list: { child: true } })
  })

  it('list-child-pair', () => {
    tsvTestListChild('feature-list-child-pair', { list: { child: true, pair: true } })
  })

  it('list-child-interaction', () => {
    // === child=false (default): bare colon in list is an error ===
    assert.throws(() => j.parse('[:1]'), /unexpected/)
    assert.throws(() => j.parse('[1,:2]'), /unexpected/)

    // === child=true, property=true (default): both work independently ===
    let lc = j.make({ list: { child: true } })
    // child$ is set as property, key:val is set as property
    assert.deepEqual(JS(lc.parse('[a:1,:2]')), '[]')
    assert.deepEqual(lc.parse('[a:1,:2]').a, 1)
    assert.deepEqual(lc.parse('[a:1,:2]')['child$'], 2)

    // === child=true, property=false: child works, key:val errors ===
    let lc_noprop = j.make({ list: { child: true, property: false } })
    assert.deepEqual(lc_noprop.parse('[:1]')['child$'], 1)
    assert.throws(() => lc_noprop.parse('[a:1]'), /unexpected/)

    // === child=true, pair=true: pairs become elements, child$ still property ===
    let lc_pair = j.make({ list: { child: true, pair: true } })
    assert.deepEqual(JP(lc_pair.parse('[a:1,:2]')), [{ a: 1 }])
    assert.deepEqual(lc_pair.parse('[a:1,:2]')['child$'], 2)
    assert.deepEqual(JP(lc_pair.parse('[:1,a:2]')), [{ a: 2 }])
    assert.deepEqual(lc_pair.parse('[:1,a:2]')['child$'], 1)

    // === child=true, pair=true, property=false: all three options ===
    let lc_all = j.make({ list: { child: true, pair: true, property: false } })
    assert.deepEqual(JP(lc_all.parse('[a:1,:2]')), [{ a: 1 }])
    assert.deepEqual(lc_all.parse('[a:1,:2]')['child$'], 2)

    // === Nested lists: inner list child$ is independent ===
    assert.deepEqual(JS(lc.parse('[[:1]]')), '[[]]')
    assert.deepEqual(lc.parse('[[:1]]')[0]['child$'], 1)
    assert.deepEqual(lc.parse('[[:1]]')['child$'], undefined)

    // === child$ merges with map.extend (default) ===
    assert.deepEqual(lc.parse('[:{a:1},:{b:2}]')['child$'], { a: 1, b: 2 })
    assert.deepEqual(lc.parse('[:{a:{x:1}},:{a:{y:2}}]')['child$'], { a: { x: 1, y: 2 } })

    // === child$ without map.extend: last value wins ===
    let lc_noext = j.make({ list: { child: true }, map: { extend: false } })
    assert.deepEqual(lc_noext.parse('[:{a:1},:{b:2}]')['child$'], { b: 2 })
    assert.deepEqual(lc_noext.parse('[:1,:2]')['child$'], 2)

    // === Maps outside lists are unaffected by list.child ===
    assert.deepEqual(lc.parse('{a:1}'), { a: 1 })
    assert.deepEqual(lc.parse('a:1,b:2'), { a: 1, b: 2 })
  })

  it('list-child-deep', () => {
    tsvTestListChild('feature-list-child-deep', { list: { child: true } })
  })

  it('list-child-pair-deep', () => {
    tsvTestListChild('feature-list-child-pair-deep', { list: { child: true, pair: true } })
  })

  it('list-child-deep-multilevel', () => {
    let lc = j.make({ list: { child: true } })
    let lcp = j.make({ list: { child: true, pair: true } })

    // === 2-level nesting: child at inner level only ===
    let r1 = lc.parse('[[:1]]')
    assert.deepEqual(JS(r1), '[[]]')
    assert.deepEqual(r1['child$'], undefined)
    assert.deepEqual(r1[0]['child$'], 1)

    // === 2-level: sibling lists with different child values ===
    let r2 = lc.parse('[[:1],[:2]]')
    assert.deepEqual(JS(r2), '[[],[]]')
    assert.deepEqual(r2['child$'], undefined)
    assert.deepEqual(r2[0]['child$'], 1)
    assert.deepEqual(r2[1]['child$'], 2)

    // === 3-level nesting: child only at deepest level ===
    let r3 = lc.parse('[[[:1]]]')
    assert.deepEqual(JS(r3), '[[[]]]')
    assert.deepEqual(r3['child$'], undefined)
    assert.deepEqual(r3[0]['child$'], undefined)
    assert.deepEqual(r3[0][0]['child$'], 1)

    // === 2-level: child at both levels ===
    let r4 = lc.parse('[[:1],:2]')
    assert.deepEqual(JS(r4), '[[]]')
    assert.deepEqual(r4['child$'], 2)
    assert.deepEqual(r4[0]['child$'], 1)

    // === 3-level: child at every level ===
    let r5 = lc.parse('[[[:1],:2],:3]')
    assert.deepEqual(JS(r5), '[[[]]]')
    assert.deepEqual(r5['child$'], 3)
    assert.deepEqual(r5[0]['child$'], 2)
    assert.deepEqual(r5[0][0]['child$'], 1)

    // === child value is a list which itself has child ===
    let r6 = lc.parse('[:[:1]]')
    assert.deepEqual(JS(r6), '[]')
    assert.deepEqual(JS(r6['child$']), '[]')
    assert.deepEqual(r6['child$']['child$'], 1)

    // === 3 levels deep via child-as-value chaining ===
    let r7 = lc.parse('[:[:[:1]]]')
    assert.deepEqual(JS(r7), '[]')
    assert.deepEqual(JS(r7['child$']), '[]')
    assert.deepEqual(JS(r7['child$']['child$']), '[]')
    assert.deepEqual(r7['child$']['child$']['child$'], 1)

    // === child value is list with elements and child ===
    let r8 = lc.parse('[1,:[:2,3]]')
    assert.deepEqual(JS(r8), '[1]')
    assert.deepEqual(JS(r8['child$']), '[3]')
    assert.deepEqual(r8['child$']['child$'], 2)

    // === Mixed elements with deep child ===
    let r9 = lc.parse('[1,[2,[3,:4]]]')
    assert.deepEqual(JS(r9), '[1,[2,[3]]]')
    assert.deepEqual(r9['child$'], undefined)
    assert.deepEqual(r9[1]['child$'], undefined)
    assert.deepEqual(r9[1][1]['child$'], 4)

    // === Deep with multiple children at inner level (last wins) ===
    let r10 = lc.parse('[1,[:2,:3],4,:5]')
    assert.deepEqual(JS(r10), '[1,[],4]')
    assert.deepEqual(r10['child$'], 5)
    assert.deepEqual(r10[1]['child$'], 3)

    // === Sibling lists: one with child, one without ===
    let r11 = lc.parse('[[1,2],[:3]]')
    assert.deepEqual(JS(r11), '[[1,2],[]]')
    assert.deepEqual(r11['child$'], undefined)
    assert.deepEqual(r11[0]['child$'], undefined)
    assert.deepEqual(r11[1]['child$'], 3)

    // === Inner child$ merges objects ===
    let r12 = lc.parse('[[:{a:1},:{b:2}]]')
    assert.deepEqual(JS(r12), '[[]]')
    assert.deepEqual(r12['child$'], undefined)
    assert.deepEqual(r12[0]['child$'], { a: 1, b: 2 })

    // === Map containing list with child$ ===
    let r13 = lc.parse('{x:[:1,2]}')
    assert.deepEqual(JS(r13), '{"x":[2]}')
    assert.deepEqual(r13.x['child$'], 1)

    // === Nested maps containing lists with child$ ===
    let r14 = lc.parse('{x:{y:[:1,2]}}')
    assert.deepEqual(JS(r14), '{"x":{"y":[2]}}')
    assert.deepEqual(r14.x.y['child$'], 1)

    // === Array of maps each containing lists with child$ ===
    let r15 = lc.parse('[{a:[:1]},{b:[:2]}]')
    assert.deepEqual(JS(r15), '[{"a":[]},{"b":[]}]')
    assert.deepEqual(r15[0].a['child$'], 1)
    assert.deepEqual(r15[1].b['child$'], 2)

    // === pair+child at multiple levels ===
    let r16 = lcp.parse('[[a:1,:2]]')
    assert.deepEqual(JS(r16), '[[{"a":1}]]')
    assert.deepEqual(r16['child$'], undefined)
    assert.deepEqual(r16[0]['child$'], 2)

    // === pair+child: child at both levels ===
    let r17 = lcp.parse('[a:1,[b:2,:3],:4]')
    assert.deepEqual(JS(r17), '[{"a":1},[{"b":2}]]')
    assert.deepEqual(r17['child$'], 4)
    assert.deepEqual(r17[1]['child$'], 3)

    // === pair+child: 3-level child at every level ===
    let r18 = lcp.parse('[[[:5],:6],:7]')
    assert.deepEqual(JS(r18), '[[[]]]')
    assert.deepEqual(r18['child$'], 7)
    assert.deepEqual(r18[0]['child$'], 6)
    assert.deepEqual(r18[0][0]['child$'], 5)

    // === pair+child: sibling inner lists with independent child values ===
    let r19 = lcp.parse('[[a:1,:2],[b:3,:4]]')
    assert.deepEqual(JS(r19), '[[{"a":1}],[{"b":3}]]')
    assert.deepEqual(r19['child$'], undefined)
    assert.deepEqual(r19[0]['child$'], 2)
    assert.deepEqual(r19[1]['child$'], 4)

    // === pair+child: inner list with multiple pairs and child ===
    let r20 = lcp.parse('[a:1,[b:2,c:3,:4]]')
    assert.deepEqual(JS(r20), '[{"a":1},[{"b":2},{"c":3}]]')
    assert.deepEqual(r20['child$'], undefined)
    assert.deepEqual(r20[1]['child$'], 4)
  })

  it('map-child', () => {
    tsvTestWith('feature-map-child', { map: { child: true } })
  })

  it('map-child-deep', () => {
    tsvTestWith('feature-map-child-deep', { map: { child: true }, list: { child: true } })
  })

  it('map-child-interaction', () => {
    // === child=false (default): bare colon in map is an error ===
    assert.throws(() => j.parse('{:1}'), /unexpected/)
    assert.throws(() => j.parse('{:1,a:2}'), /unexpected/)

    // === child=true: bare colon stores child$ ===
    let mc = j.make({ map: { child: true } })
    assert.deepEqual(mc.parse('{:1,a:2}'), { child$: 1, a: 2 })
    assert.deepEqual(mc.parse('{:1,a:2}')['child$'], 1)

    // === child$ merge: objects deep-merge, primitives last-wins ===
    assert.deepEqual(mc.parse('{:1,:2}')['child$'], 2)
    assert.deepEqual(mc.parse('{:{a:1},:{b:2}}')['child$'], { a: 1, b: 2 })
    assert.deepEqual(mc.parse('{:{a:{x:1}},:{a:{y:2}}}')['child$'], { a: { x: 1, y: 2 } })

    // === child$ without map.extend: last value wins ===
    let mc_noext = j.make({ map: { child: true, extend: false } })
    assert.deepEqual(mc_noext.parse('{:{a:1},:{b:2}}')['child$'], { b: 2 })
    assert.deepEqual(mc_noext.parse('{:1,:2}')['child$'], 2)

    // === Lists outside maps are unaffected by map.child ===
    assert.deepEqual(mc.parse('[1,2,3]'), [1, 2, 3])

    // === Nested maps: child$ at each level ===
    let r1 = mc.parse('{:1,a:{:2,b:{:3}}}')
    assert.deepEqual(r1['child$'], 1)
    assert.deepEqual(r1.a['child$'], 2)
    assert.deepEqual(r1.a.b['child$'], 3)

    // === map.child + list.child both enabled ===
    let both = j.make({ map: { child: true }, list: { child: true } })

    // Map with child, containing list with child
    let r2 = both.parse('{a:[:1,2],:3}')
    assert.deepEqual(r2['child$'], 3)
    assert.deepEqual(r2.a['child$'], 1)
    assert.deepEqual(JS(r2.a), '[2]')

    // List with child, containing map with child
    let r3 = both.parse('[{:1,a:2},:3]')
    assert.deepEqual(r3['child$'], 3)
    assert.deepEqual(r3[0]['child$'], 1)
    assert.deepEqual(JS(r3[0]), '{"child$":1,"a":2}')

    // Deep: map -> list -> map, each with child$
    let r4 = both.parse('{a:[{:1}],:2}')
    assert.deepEqual(r4['child$'], 2)
    assert.deepEqual(r4.a[0]['child$'], 1)

    // List child value is a map with child$
    let r5 = both.parse('[:{ a: 1 }]')
    assert.deepEqual(JS(r5), '[]')
    assert.deepEqual(r5['child$'], { a: 1 })

    // 3-level: map -> map -> list -> map with child at each
    let r6 = both.parse('{:1,x:{:2,y:[{:3}]}}')
    assert.deepEqual(r6['child$'], 1)
    assert.deepEqual(r6.x['child$'], 2)
    assert.deepEqual(r6.x.y[0]['child$'], 3)

    // Array of maps, each with their own child$
    let r7 = both.parse('[{:1,a:10},{:2,b:20}]')
    assert.deepEqual(r7[0]['child$'], 1)
    assert.deepEqual(r7[1]['child$'], 2)

    // map.child only (no list.child): list bare colon still errors
    assert.throws(() => mc.parse('[:1]'), /unexpected/)

    // list.child only (no map.child): map bare colon still errors
    let lc = j.make({ list: { child: true } })
    assert.throws(() => lc.parse('{:1}'), /unexpected/)

    // Implicit map with child$
    assert.deepEqual(mc.parse('a:1,:2'), { a: 1, child$: 2 })
    assert.deepEqual(mc.parse('a:1,:2,b:3'), { a: 1, child$: 2, b: 3 })
  })

  // Test derived from debug sessions using quick.js
  it('debug-cases', () => {
    tsvTest('feature-debug-cases')
  })
})

function match(src, pat, ctx) {
  ctx = ctx || {}
  ctx.loc = ctx.loc || '$'

  if (src === pat) return
  if (false !== ctx.miss && undefined === pat) return

  if (Array.isArray(src) && Array.isArray(pat)) {
    if (false === ctx.miss && src.length !== pat.length) {
      return ctx.loc + '/len:' + src.length + '!=' + pat.length
    }

    let m = undefined
    for (let i = 0; i < pat.length; i++) {
      m = match(src[i], pat[i], { ...ctx, loc: ctx.loc + '[' + i + ']' })
      if (m) {
        return m
      }
    }

    return
  } else if ('object' === typeof src && 'object' === typeof pat) {
    let ksrc = Object.keys(src).sort()
    let kpat = Object.keys(pat).sort()

    if (false === ctx.miss && ksrc.length !== kpat.length) {
      return ctx.loc + '/key:{' + ksrc + '}!={' + kpat + '}'
    }

    for (let i = 0; i < kpat.length; i++) {
      if (false === ctx.miss && ksrc[i] !== kpat[i])
        return ctx.loc + '/key:' + kpat[i]

      let m = match(src[kpat[i]], pat[kpat[i]], {
        ...ctx,
        loc: ctx.loc + '.' + kpat[i],
      })
      if (m) {
        return m
      }
    }

    return
  }

  return ctx.loc + '/val:' + src + '!=' + pat
}
