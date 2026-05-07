/* Copyright (c) 2021 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama } = require('..')

// Compare via JSON roundtrip to handle null-prototype objects.
function deq(actual, expected, msg) {
  assert.deepStrictEqual(
    JSON.parse(JSON.stringify(actual)),
    JSON.parse(JSON.stringify(expected)),
    msg,
  )
}
const eq = assert.strictEqual

describe('readme', function () {
  describe('quick-example', () => {
    it('parses implicit object', () => {
      deq(Amagama('a:1, b:2'), { a: 1, b: 2 })
    })

    it('parses implicit array', () => {
      deq(Amagama('x, y, z'), ['x', 'y', 'z'])
    })

    it('parses nested object', () => {
      deq(Amagama('{a: {b: 1, c: 2}}'), { a: { b: 1, c: 2 } })
    })
  })

  describe('syntax-examples', () => {
    it('unquoted keys and values', () => {
      deq(Amagama('a:1,b:B'), { a: 1, b: 'B' })
    })

    it('newline separated', () => {
      deq(Amagama('a:1\nb:B'), { a: 1, b: 'B' })
    })

    it('with comments', () => {
      deq(
        Amagama('a:1\n// a:2\n# a:3\n/* b wants\n * to B\n */\nb:B'),
        { a: 1, b: 'B' },
      )
    })

    it('mixed quote styles and number formats', () => {
      deq(Amagama('{ "a": 100e-2, \'\\u0062\':`\\x42`, }'), {
        a: 1,
        b: 'B',
      })
    })
  })

  describe('relaxation-examples', () => {
    it('unquoted keys and values', () => {
      deq(Amagama('a:1'), { a: 1 })
    })

    it('implicit top-level object', () => {
      deq(Amagama('a:1,b:2'), { a: 1, b: 2 })
    })

    it('implicit top-level array', () => {
      deq(Amagama('a,b'), ['a', 'b'])
    })

    it('trailing commas', () => {
      deq(Amagama('{a:1,b:2,}'), { a: 1, b: 2 })
    })

    it('single-quoted strings', () => {
      eq(Amagama("'hello'"), 'hello')
    })

    it('backtick strings', () => {
      eq(Amagama('`hello`'), 'hello')
    })

    it('object merging', () => {
      deq(Amagama('a:{b:1},a:{c:2}'), { a: { b: 1, c: 2 } })
    })

    it('path diving', () => {
      deq(Amagama('a:b:1,a:c:2'), { a: { b: 1, c: 2 } })
    })

    it('all number formats equivalent', () => {
      eq(Amagama('1e1'), 10)
      eq(Amagama('0xa'), 10)
      eq(Amagama('0o12'), 10)
      eq(Amagama('0b1010'), 10)
    })

    it('number separators', () => {
      eq(Amagama('1_000'), 1000)
    })

  })

  describe('options-example', () => {
    it('make with options', () => {
      const lenient = Amagama.make({
        comment: { lex: false },
        number: { hex: false },
        value: {
          def: { yes: { val: true }, no: { val: false } },
        },
      })
      eq(lenient('yes'), true)
    })
  })

  describe('plugin-example', () => {
    it('custom plugin with fixed token', () => {
      function myPlugin(amagama, options) {
        amagama.options({ fixed: { token: { '#TL': '~' } } })
        const T_TILDE = amagama.token('#TL')

        amagama.rule('val', (rs) => {
          rs.open([
            {
              s: [T_TILDE],
              a: (rule) => {
                rule.node = options.tildeValue ?? null
              },
            },
          ])
        })
      }

      const j = Amagama.make()
      j.use(myPlugin, { tildeValue: 42 })
      eq(j('~'), 42)
    })
  })

  describe('api-table-examples', () => {
    it('Amagama(src) parses a string', () => {
      deq(Amagama('a:1'), { a: 1 })
    })

    it('Amagama.make() creates a configured instance', () => {
      const j = Amagama.make()
      deq(j('a:1'), { a: 1 })
    })

    it('instance.use() registers a plugin', () => {
      const j = Amagama.make()
      let called = false
      j.use(function testPlugin() {
        called = true
      })
      eq(called, true)
    })

    it('instance.rule() modifies grammar', () => {
      const j = Amagama.make()
      const rules = j.rule()
      deq(Object.keys(rules), ['val', 'map', 'list', 'pair', 'elem'])
    })

    it('instance.token() gets or creates a token type', () => {
      const j = Amagama.make()
      eq(typeof j.token.ST, 'number')
    })

    it('instance.options returns current options', () => {
      const j = Amagama.make()
      eq(j.options.comment.lex, true)
    })
  })
})
