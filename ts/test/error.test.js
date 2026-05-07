/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama, AmagamaError } = require('..')

const je = (s) => () => Amagama(s)

const JS = (x) => JSON.stringify(x)

describe('error', function () {
  it('error-message', () => {
    let src0 = '\n\n\n\n\n\n\n\n\n\n   "\\u0000"'
    try {
      Amagama(src0)
    } catch (e) {
      assert.deepEqual(e.message, 
        `\u001b[91m[amagama/invalid_unicode]:\u001b[0m invalid unicode escape: "\\\\u0000"
  \u001b[34m-->\u001b[0m <no-file>:10:6
\u001b[34m   8 | \u001b[0m
\u001b[34m   9 | \u001b[0m
\u001b[34m  10 | \u001b[0m   "\\u0000"
             \u001b[91m^^^^^^ invalid unicode escape: "\\\\u0000"\u001b[0m
\u001b[34m  11 | \u001b[0m
\u001b[34m  12 | \u001b[0m

  The escape sequence "\\\\u0000" does not encode a valid unicode code point
  number. You may need to validate your string data manually using test
  code to see how Java script will interpret it. Also consider that your
  data may have become corrupted, or the escape sequence has not been
  generated correctly.

  \u001b[2mhttps://amagama.senecajs.org\u001b[0m
  \u001b[2m--internal: rule=val~open; token=#BD~foo; plugins=--\u001b[0m'
`,
      )
    }
  })

  it('plugin-errors', () => {
    let k = Amagama.make().use(function foo(amagama) {
      amagama.options({
        tag: 'zed',
        error: {
          foo: 'foo: {src}!',
        },
        hint: {
          foo: 'Foo hint.',
        },
        lex: {
          match: {
            foo: {
              order: 9e5,
              make: () => (lex) => {
                if (lex.src.substring(lex.pnt.sI).startsWith('FOO')) {
                  return lex.bad('foo', lex.pnt.sI, lex.pnt.sI + 4)
                }
              },
            },
          },
        },
      })
      // amagama.lex(() => (lex) => {
      //   if (lex.src.substring(lex.pnt.sI).startsWith('FOO')) {
      //     return lex.bad('foo', lex.pnt.sI, lex.pnt.sI + 4)
      //   }
      // })
    })

    let src0 = 'a:1,\nb:FOO'

    /*
    try {
      k(src0)
    }
    catch(e) {
      console.log(e)
    } 
    */

    try {
      k(src0, { xlog: -1 })
    } catch (e) {
      assert.deepEqual(e.message, 
        '\u001b[91m[amagama/foo]:\u001b[0m foo: FOO!\n' +
          '  \u001b[34m-->\u001b[0m <no-file>:2:3\n' +
          '\u001b[34m  1 | \u001b[0ma:1,\n' +
          '\u001b[34m  2 | \u001b[0mb:FOO\n' +
          '        \u001b[34m^^^ foo: FOO!\u001b[0m\n' +
          '\u001b[34m  3 | \u001b[0m\n' +
          '\u001b[34m  4 | \u001b[0m\n' +
          '\n' +
          '  Foo hint.\n' +
          '\n' +
          '  \u001b[2mhttps://amagama.senecajs.org\u001b[0m\n' +
          '  \u001b[2m--internal: tag=zed; rule=val~o; token=#BD~foo;' +
          ' plugins=foo--\u001b[0m',
      )
    }

    assert.throws(() => k('a:1,\nb:FOO'), /foo/)
  })

  it('lex-unicode', () => {
    let src0 = '\n\n\n\n\n\n\n\n\n\n   "\\uQQQQ"'
    //je(src0)()
    assert.throws(je(src0), /invalid_unicode/)

    let src1 = '\n\n\n\n\n\n\n\n\n\n   "\\u{QQQQQQ}"'
    //je(src1)()
    assert.throws(je(src0), /invalid_unicode/)
  })

  it('lex-ascii', () => {
    let src0 = '\n\n\n\n\n\n\n\n\n\n   "\\x!!"'
    // je(src0)()
    assert.throws(je(src0), /invalid_ascii/)
  })

  it('lex-unprintable', () => {
    let src0 = '"\x00"'
    assert.throws(je(src0), /unprintable/)
  })

  it('lex-unterminated', () => {
    let src0 = '"a'

    assert.throws(je(src0), /unterminated/)

    /*
    try {
      Amagama(src0)
    }
    catch(e) {
      console.log(e)
    } 
    */
  })

  it('parse-unexpected', () => {
    let src0 = '\n\n\n\n\n\n\n\n\n\n   }'

    assert.throws(je(src0), /unexpected/)

    /*
    try {
      Amagama(src0)
    }
    catch(e) {
      console.log(e)
    } 
    */
  })

  it('error-json-desc', () => {
    try {
      Amagama(']')
    } catch (e) {
      // console.log(e)
      assert.deepEqual(
        JSON.stringify(e).includes(
          '{"code":"unexpected","details":{"state":"open"},' +
            '"meta":{},"lineNumber":1,"columnNumber":1',
        ), true)
    }
  })

  it('bad-syntax', () => {
    // TODO: unexpected end of src needs own case, otherwise incorrect explanation
    // expect(je('{a')).throw(/incomplete/)

    // TODO: should all be null
    //expect(Amagama('a:')).equal({a:undefined})
    //expect(Amagama('{a:')).equal({a:undefined})
    //expect(Amagama('{a:,b:')).equal({a:undefined,b:undefined})
    //expect(Amagama('a:,b:')).equal({a:undefined,b:undefined})

    // Invalid pair.
    assert.throws(je('{]'), /unexpected/)
    assert.throws(je('[}'), /unexpected/)
    assert.throws(je(':'), /unexpected/)
    assert.throws(je(':a'), /unexpected/)
    assert.throws(je(' : '), /unexpected/)
    assert.throws(je('{,]'), /unexpected/)
    assert.throws(je('[,}'), /unexpected/)
    assert.throws(je(',:'), /unexpected/)
    assert.throws(je(',:a'), /unexpected/)
    assert.throws(je('[:'), /unexpected/)
    assert.throws(je('[:a'), /unexpected/)

    // Unexpected close
    assert.throws(je(']'), /unexpected/)
    assert.throws(je('}'), /unexpected/)
    assert.throws(je(' ]'), /unexpected/)
    assert.throws(je(' }'), /unexpected/)
    assert.throws(je(',}'), /unexpected/)
    assert.throws(je('a]'), /unexpected/)
    assert.throws(je('a}'), /unexpected/)
    assert.throws(je('{a]'), /unexpected/)
    assert.throws(je('[a}'), /unexpected/)
    assert.throws(je('{a}'), /unexpected/)
    assert.throws(je('{a:1]'), /unexpected/)

    // These are actually OK
    assert.deepEqual(Amagama(',]'), [null])
    assert.deepEqual(Amagama('{a:}'), { a: null })
    assert.deepEqual(Amagama('{a:b:}'), { a: { b: null } })

    assert.deepEqual(JS(Amagama('[a:1]')), '[]')
    assert.deepEqual(Amagama('[a:1]').a, 1)

    assert.deepEqual(JS(Amagama('[a:]')), '[]')
    assert.deepEqual(Amagama('[a:]').a, null)
  })

  it('api-error', () => {
    assert.throws(() => Amagama.make().use(null), /Amagama\.use:/)
  })
})
