/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeLex, TabnasError } = require('..')
const { loadTSV } = require('./utility')
const tn = new Tabnas()
const J = (src, meta, ctx) => tn.parse(src, meta, ctx)

describe('lex', function () {
  let j, t, config

  function lexall(src) {
    let lex = lexstart(src)
    let out = []
    do {
      // console.log(out[out.length-1])
      // NOTE: tokens are collected directly (not spread-copied):
      // Token.src is a prototype accessor (materialized lazily from the
      // token's span), so it is not an own enumerable property and a
      // {...tkn} copy would lose it.
      out.push(lex())
    } while (t.ZZ != out[out.length - 1].tin && t.BD != out[out.length - 1].tin)
    return out.map((t) => st(t))
  }

  function alleq(ta) {
    for (let i = 0; i < ta.length; i += 2) {
      let suffix = ' CASE:' + i / 2 + ' [' + ta[i] + ']'
      assert.deepEqual(lexall(ta[i]) + suffix, ta[i + 1] + suffix)
    }
  }

  function lexstart(src) {
    j = tn.make()
    config = j.internal().config
    t = j.token

    let lex = makeLex({ src: () => src, cfg: config, opts: j.options, sub: {} })
    return lex.next.bind(lex)
  }

  it('tabnas-token', () => {
    lexstart('')
    assert.ok(j.token.OB != null)
    assert.ok(t.CB != null)
  })

  it('specials', () => {
    let lex0 = lexstart(' {123 ')
    assert.deepEqual('' + lex0(), 'Token[#SP=5   0,1,1]')
    assert.deepEqual('' + lex0(), 'Token[#OB=12 { 1,1,2]')
    assert.deepEqual('' + lex0(), 'Token[#NR=8 123=123 2,1,3]')
    assert.deepEqual('' + lex0(), 'Token[#SP=5   5,1,6]')
    assert.deepEqual('' + lex0(), 'Token[#ZZ=2  6,1,7]')
    assert.deepEqual('' + lex0(), 'Token[#ZZ=2  6,1,7]')
    assert.deepEqual('' + lex0(), 'Token[#ZZ=2  6,1,7]')

    let lex1 = lexstart('"\\u0040\\u{012345}"')
    let t0 = lex1()
    assert.deepEqual(t0.val, '\u0040\u{012345}')
    assert.deepEqual(t0.len, 18)
    assert.deepEqual('' + t0, 'Token[#ST=9 "\\u00 0,1,1]') // NOTE: truncated!

    assert.deepEqual(lexall(' {123'), [
      '#SP;0;1;1x1',
      '#OB;1;1;1x2',
      '#NR;2;3;1x3;123',
      '#ZZ;5;0;1x6',
    ])

    assert.deepEqual(lexall(' {123%'), [
      '#SP;0;1;1x1',
      '#OB;1;1;1x2',
      '#TX;2;4;1x3;123%',
      '#ZZ;6;0;1x7',
    ])

    alleq(['', ['#ZZ;0;0;1x1'], '0', ['#NR;0;1;1x1;0', '#ZZ;1;0;1x2']])
  })

  it('space', () => {
    let lex0 = lexstart(' \t')
    assert.deepEqual('' + lex0(), 'Token[#SP=5  . 0,1,1]')

    alleq([
      ' ',
      ['#SP;0;1;1x1', '#ZZ;1;0;1x2'],
      '  ',
      ['#SP;0;2;1x1', '#ZZ;2;0;1x3'],
      ' \t',
      ['#SP;0;2;1x1', '#ZZ;2;0;1x3'],
      ' \t ',
      ['#SP;0;3;1x1', '#ZZ;3;0;1x4'],
      '\t \t',
      ['#SP;0;3;1x1', '#ZZ;3;0;1x4'],
      '\t ',
      ['#SP;0;2;1x1', '#ZZ;2;0;1x3'],
      '\t\t',
      ['#SP;0;2;1x1', '#ZZ;2;0;1x3'],
      '\t',
      ['#SP;0;1;1x1', '#ZZ;1;0;1x2'],
    ])
  })

  it('brace', () => {
    alleq([
      '{',
      ['#OB;0;1;1x1', '#ZZ;1;0;1x2'],
      '{{',
      ['#OB;0;1;1x1', '#OB;1;1;1x2', '#ZZ;2;0;1x3'],
      '}',
      ['#CB;0;1;1x1', '#ZZ;1;0;1x2'],
      '}}',
      ['#CB;0;1;1x1', '#CB;1;1;1x2', '#ZZ;2;0;1x3'],
    ])
  })

  it('square', () => {
    alleq([
      '[',
      ['#OS;0;1;1x1', '#ZZ;1;0;1x2'],
      '[[',
      ['#OS;0;1;1x1', '#OS;1;1;1x2', '#ZZ;2;0;1x3'],
      ']',
      ['#CS;0;1;1x1', '#ZZ;1;0;1x2'],
      ']]',
      ['#CS;0;1;1x1', '#CS;1;1;1x2', '#ZZ;2;0;1x3'],
    ])
  })

  it('colon', () => {
    alleq([
      ':',
      ['#CL;0;1;1x1', '#ZZ;1;0;1x2'],
      '::',
      ['#CL;0;1;1x1', '#CL;1;1;1x2', '#ZZ;2;0;1x3'],
    ])
  })

  it('comma', () => {
    alleq([
      ',',
      ['#CA;0;1;1x1', '#ZZ;1;0;1x2'],
      ',,',
      ['#CA;0;1;1x1', '#CA;1;1;1x2', '#ZZ;2;0;1x3'],
    ])
  })

  it('comment', () => {
    alleq([
      'a#b',
      ['#TX;0;1;1x1;a', '#CM;1;2;1x2', '#ZZ;3;0;1x4'],
      'a/*x*/b',
      ['#TX;0;1;1x1;a', '#CM;1;5;1x2', '#TX;6;1;1x7;b', '#ZZ;7;0;1x8'],
      'a#b\nc',
      [
        '#TX;0;1;1x1;a',
        '#CM;1;2;1x2',
        '#LN;3;1;1x4',
        '#TX;4;1;2x1;c',
        '#ZZ;5;0;2x2',
      ],
      'a#b\r\nc',
      [
        '#TX;0;1;1x1;a',
        '#CM;1;2;1x2',
        '#LN;3;2;1x4',
        '#TX;5;1;2x1;c',
        '#ZZ;6;0;2x2',
      ],
    ])
  })

  it('boolean', () => {
    alleq([
      'true',
      ['#VL;0;4;1x1;true', '#ZZ;4;0;1x5'],
      'true ',
      ['#VL;0;4;1x1;true', '#SP;4;1;1x5', '#ZZ;5;0;1x6'],
      ' true',
      ['#SP;0;1;1x1', '#VL;1;4;1x2;true', '#ZZ;5;0;1x6'],
      'truex',
      ['#TX;0;5;1x1;truex', '#ZZ;5;0;1x6'],
      'truex ',
      ['#TX;0;5;1x1;truex', '#SP;5;1;1x6', '#ZZ;6;0;1x7'],
      'false',
      ['#VL;0;5;1x1;false', '#ZZ;5;0;1x6'],
      'false ',
      ['#VL;0;5;1x1;false', '#SP;5;1;1x6', '#ZZ;6;0;1x7'],
      ' false',
      ['#SP;0;1;1x1', '#VL;1;5;1x2;false', '#ZZ;6;0;1x7'],
      'falsex',
      ['#TX;0;6;1x1;falsex', '#ZZ;6;0;1x7'],
      'falsex ',
      ['#TX;0;6;1x1;falsex', '#SP;6;1;1x7', '#ZZ;7;0;1x8'],
    ])
  })

  it('null', () => {
    alleq([
      'null',
      ['#VL;0;4;1x1;null', '#ZZ;4;0;1x5'],
      'null ',
      ['#VL;0;4;1x1;null', '#SP;4;1;1x5', '#ZZ;5;0;1x6'],
      ' null',
      ['#SP;0;1;1x1', '#VL;1;4;1x2;null', '#ZZ;5;0;1x6'],
      'nullx',
      ['#TX;0;5;1x1;nullx', '#ZZ;5;0;1x6'],
      'nullx ',
      ['#TX;0;5;1x1;nullx', '#SP;5;1;1x6', '#ZZ;6;0;1x7'],
      'nulx ',
      ['#TX;0;4;1x1;nulx', '#SP;4;1;1x5', '#ZZ;5;0;1x6'],
      'nulx',
      ['#TX;0;4;1x1;nulx', '#ZZ;4;0;1x5'],
    ])
  })

  it('number', () => {
    let lex0 = lexstart('321')
    assert.deepEqual('' + lex0(), 'Token[#NR=8 321=321 0,1,1]')

    alleq([
      '0',
      ['#NR;0;1;1x1;0', '#ZZ;1;0;1x2'],
      '0.',
      ['#NR;0;2;1x1;0', '#ZZ;2;0;1x3'],
      '.0',
      ['#NR;0;2;1x1;0', '#ZZ;2;0;1x3'],
      '-0',
      ['#NR;0;2;1x1;0', '#ZZ;2;0;1x3'],
      '-.0',
      ['#NR;0;3;1x1;0', '#ZZ;3;0;1x4'],
      '1.2',
      ['#NR;0;3;1x1;1.2', '#ZZ;3;0;1x4'],
      '-1.2',
      ['#NR;0;4;1x1;-1.2', '#ZZ;4;0;1x5'],
      '0xA',
      ['#NR;0;3;1x1;10', '#ZZ;3;0;1x4'],
      '1e2',
      ['#NR;0;3;1x1;100', '#ZZ;3;0;1x4'],
      '0e0',
      ['#NR;0;3;1x1;0', '#ZZ;3;0;1x4'],
      '-1.5E2',
      ['#NR;0;6;1x1;-150', '#ZZ;6;0;1x7'],
      '0x',
      ['#TX;0;2;1x1;0x', '#ZZ;2;0;1x3'],
      '-0xA',
      ['#NR;0;4;1x1;-10', '#ZZ;4;0;1x5'],
      '01',
      ['#NR;0;2;1x1;1', '#ZZ;2;0;1x3'],
      '1x',
      ['#TX;0;2;1x1;1x', '#ZZ;2;0;1x3'],
      '12x',
      ['#TX;0;3;1x1;12x', '#ZZ;3;0;1x4'],
      '1%',
      ['#TX;0;2;1x1;1%', '#ZZ;2;0;1x3'],
      '12%',
      ['#TX;0;3;1x1;12%', '#ZZ;3;0;1x4'],
      '123%',
      ['#TX;0;4;1x1;123%', '#ZZ;4;0;1x5'],
      '1_0_0',
      ['#NR;0;5;1x1;100', '#ZZ;5;0;1x6'],
    ])
  })

  // A trailing dot before an exponent is a number: the fraction group
  // makes the digit optional, so `2.` then `e3` both match. Go's
  // matchNumber originally rejected these (the 'e' looked like trailing
  // text), so keep the two runtimes pinned here. Without exponent digits
  // (`2.e`) it stays text in both.
  it('number-exponent-trailing-dot', () => {
    alleq([
      '2.e3',
      ['#NR;0;4;1x1;2000', '#ZZ;4;0;1x5'],
      '2.e+3',
      ['#NR;0;5;1x1;2000', '#ZZ;5;0;1x6'],
      '2.e-3',
      ['#NR;0;5;1x1;0.002', '#ZZ;5;0;1x6'],
      '0.e1',
      ['#NR;0;4;1x1;0', '#ZZ;4;0;1x5'],
      '2.e',
      ['#TX;0;3;1x1;2.e', '#ZZ;3;0;1x4'],
      '2.a',
      ['#TX;0;3;1x1;2.a', '#ZZ;3;0;1x4'],
    ])
  })

  // Unary + saturates rather than failing, so an out-of-range exponent is
  // still a number. Go's parseNumericString used to treat ParseFloat's
  // ErrRange as a hard failure and drop these to text; keep both runtimes
  // pinned. `1e` (no exponent digits) is text in both.
  it('number-exponent-range', () => {
    alleq([
      '1e999',
      ['#NR;0;5;1x1;Infinity', '#ZZ;5;0;1x6'],
      '-1e999',
      ['#NR;0;6;1x1;-Infinity', '#ZZ;6;0;1x7'],
      '1e+999',
      ['#NR;0;6;1x1;Infinity', '#ZZ;6;0;1x7'],
      '1e309',
      ['#NR;0;5;1x1;Infinity', '#ZZ;5;0;1x6'],
      '2.e999',
      ['#NR;0;6;1x1;Infinity', '#ZZ;6;0;1x7'],
      '1e-999',
      ['#NR;0;6;1x1;0', '#ZZ;6;0;1x7'],
      '1e',
      ['#TX;0;2;1x1;1e', '#ZZ;2;0;1x3'],
    ])
  })

  // Negative zero survives lexing. `alleq` stringifies token values, and
  // -0 stringifies as "0", so this case has to compare with Object.is —
  // which is also why a Go regression here (parseNumericString once
  // normalized -0 to +0) went unnoticed for so long: JSON serialization
  // erases the sign too, so the shared .tsv fixtures cannot see it.
  // Mirrors go/lexer_edge_test.go TestMatchNumberNegativeZero.
  it('number-negative-zero', () => {
    const cases = [
      ['-0', -0],
      ['-0.0', -0],
      ['-0e0', -0],
      ['0', 0],
      ['0.0', 0],
      ['+0', 0],
    ]
    for (const [src, want] of cases) {
      const tkn = lexstart(src)()
      assert.equal(tkn.tin, t.NR, src + ': expected a number token')
      assert.ok(
        Object.is(tkn.val, want),
        src + ': expected ' + (Object.is(want, -0) ? '-0' : '0') +
        ', got ' + (Object.is(tkn.val, -0) ? '-0' : tkn.val),
      )
    }
  })

  it('double-quote', () => {
    // NOTE: col for unterminated is final col
    alleq([
      '""',
      ['#ST;0;2;1x1;', '#ZZ;2;0;1x3'],
      '"a"',
      ['#ST;0;3;1x1;a', '#ZZ;3;0;1x4'],
      '"ab"',
      ['#ST;0;4;1x1;ab', '#ZZ;4;0;1x5'],
      '"abc"',
      ['#ST;0;5;1x1;abc', '#ZZ;5;0;1x6'],
      '"a b"',
      ['#ST;0;5;1x1;a b', '#ZZ;5;0;1x6'],
      ' "a"',
      ['#SP;0;1;1x1', '#ST;1;3;1x2;a', '#ZZ;4;0;1x5'],
      '"a" ',
      ['#ST;0;3;1x1;a', '#SP;3;1;1x4', '#ZZ;4;0;1x5'],
      ' "a" ',
      ['#SP;0;1;1x1', '#ST;1;3;1x2;a', '#SP;4;1;1x5', '#ZZ;5;0;1x6'],
      '"',
      ['#BD;0;1;1x1;"~unterminated_string'],
      '"a',
      ['#BD;0;2;1x1;"a~unterminated_string'],
      '"ab',
      ['#BD;0;3;1x1;"ab~unterminated_string'],
      ' "',
      ['#SP;0;1;1x1', '#BD;1;1;1x2;"~unterminated_string'],
      ' "a',
      ['#SP;0;1;1x1', '#BD;1;2;1x2;"a~unterminated_string'],
      ' "ab',
      ['#SP;0;1;1x1', '#BD;1;3;1x2;"ab~unterminated_string'],
      '"a\'b"',
      ["#ST;0;5;1x1;a'b", '#ZZ;5;0;1x6'],
      '"\'a\'b"',
      ["#ST;0;6;1x1;'a'b", '#ZZ;6;0;1x7'],
      "\"'a'b'\"",
      ["#ST;0;7;1x1;'a'b'", '#ZZ;7;0;1x8'],
      '"\\t"',
      ['#ST;0;4;1x1;\t', '#ZZ;4;0;1x5'],
      '"\\r"',
      ['#ST;0;4;1x1;\r', '#ZZ;4;0;1x5'],
      '"\\n"',
      ['#ST;0;4;1x1;\n', '#ZZ;4;0;1x5'],
      '"\\""',
      ['#ST;0;4;1x1;"', '#ZZ;4;0;1x5'],
      '"\\\'"',
      ["#ST;0;4;1x1;'", '#ZZ;4;0;1x5'],
      '"\\q"',
      ['#ST;0;4;1x1;q', '#ZZ;4;0;1x5'],
      '"\\\'"',
      ["#ST;0;4;1x1;'", '#ZZ;4;0;1x5'],
      '"\\\\"',
      ['#ST;0;4;1x1;\\', '#ZZ;4;0;1x5'],
      '"\\u0040"',
      ['#ST;0;8;1x1;@', '#ZZ;8;0;1x9'],
      '"\\uQQQQ"',
      ['#BD;1;6;1x2;\\uQQQQ~invalid_unicode'],
      '"\\u{QQQQQQ}"',
      ['#BD;1;10;1x2;\\u{QQQQQQ}~invalid_unicode'],
      '"\\xQQ"',
      ['#BD;1;4;1x2;\\xQQ~invalid_ascii'],
      '"[{}]:,"',
      ['#ST;0;8;1x1;[{}]:,', '#ZZ;8;0;1x9'],
      '"a\\""',
      ['#ST;0;5;1x1;a"', '#ZZ;5;0;1x6'],
      '"a\\"a"',
      ['#ST;0;6;1x1;a"a', '#ZZ;6;0;1x7'],
      '"a\\"a\'a"',
      ['#ST;0;8;1x1;a"a\'a', '#ZZ;8;0;1x9'],
    ])
  })

  it('single-quote', () => {
    alleq([
      "''",
      ['#ST;0;2;1x1;', '#ZZ;2;0;1x3'],
      "'a'",
      ['#ST;0;3;1x1;a', '#ZZ;3;0;1x4'],
      "'ab'",
      ['#ST;0;4;1x1;ab', '#ZZ;4;0;1x5'],
      "'abc'",
      ['#ST;0;5;1x1;abc', '#ZZ;5;0;1x6'],
      "'a b'",
      ['#ST;0;5;1x1;a b', '#ZZ;5;0;1x6'],
      " 'a'",
      ['#SP;0;1;1x1', '#ST;1;3;1x2;a', '#ZZ;4;0;1x5'],
      "'a' ",
      ['#ST;0;3;1x1;a', '#SP;3;1;1x4', '#ZZ;4;0;1x5'],
      " 'a' ",
      ['#SP;0;1;1x1', '#ST;1;3;1x2;a', '#SP;4;1;1x5', '#ZZ;5;0;1x6'],
      "'",
      ["#BD;0;1;1x1;'~unterminated_string"],
      "'a",
      ["#BD;0;2;1x1;'a~unterminated_string"],
      "'ab",
      ["#BD;0;3;1x1;'ab~unterminated_string"],
      " '",
      ['#SP;0;1;1x1', "#BD;1;1;1x2;'~unterminated_string"],
      " 'a",
      ['#SP;0;1;1x1', "#BD;1;2;1x2;'a~unterminated_string"],
      " 'ab",
      ['#SP;0;1;1x1', "#BD;1;3;1x2;'ab~unterminated_string"],
      "'a\"b'",
      ['#ST;0;5;1x1;a"b', '#ZZ;5;0;1x6'],
      '\'"a"b\'',
      ['#ST;0;6;1x1;"a"b', '#ZZ;6;0;1x7'],
      '\'"a"b"\'',
      ['#ST;0;7;1x1;"a"b"', '#ZZ;7;0;1x8'],
      "'\\t'",
      ['#ST;0;4;1x1;\t', '#ZZ;4;0;1x5'],
      "'\\r'",
      ['#ST;0;4;1x1;\r', '#ZZ;4;0;1x5'],
      "'\\n'",
      ['#ST;0;4;1x1;\n', '#ZZ;4;0;1x5'],
      "'\\''",
      ["#ST;0;4;1x1;'", '#ZZ;4;0;1x5'],
      "'\\\"'",
      ['#ST;0;4;1x1;"', '#ZZ;4;0;1x5'],
      "'\\q'",
      ['#ST;0;4;1x1;q', '#ZZ;4;0;1x5'],
      "'\\\"'",
      ['#ST;0;4;1x1;"', '#ZZ;4;0;1x5'],
      "'\\\\'",
      ['#ST;0;4;1x1;\\', '#ZZ;4;0;1x5'],
      "'\\u0040'",
      ['#ST;0;8;1x1;@', '#ZZ;8;0;1x9'],
      "'\\uQQQQ'",
      ['#BD;1;6;1x2;\\uQQQQ~invalid_unicode'],
      "'\\u{QQQQQQ}'",
      ['#BD;1;10;1x2;\\u{QQQQQQ}~invalid_unicode'],
      "'\\xQQ'",
      ['#BD;1;4;1x2;\\xQQ~invalid_ascii'],
      "'[{}]:,'",
      ['#ST;0;8;1x1;[{}]:,', '#ZZ;8;0;1x9'],
      "'a\\''",
      ["#ST;0;5;1x1;a'", '#ZZ;5;0;1x6'],
      "'a\\'a'",
      ["#ST;0;6;1x1;a'a", '#ZZ;6;0;1x7'],
      "'a\\'a\"a'",
      ['#ST;0;8;1x1;a\'a"a', '#ZZ;8;0;1x9'],
    ])
  })

  it('text', () => {
    alleq([
      'a-b',
      ['#TX;0;3;1x1;a-b', '#ZZ;3;0;1x4'],
      '$a_',
      ['#TX;0;3;1x1;$a_', '#ZZ;3;0;1x4'],
      '!%~',
      ['#TX;0;3;1x1;!%~', '#ZZ;3;0;1x4'],
      'a"b',
      ['#TX;0;3;1x1;a"b', '#ZZ;3;0;1x4'],
      "a'b",
      ["#TX;0;3;1x1;a'b", '#ZZ;3;0;1x4'],
      ' a b ',
      [
        '#SP;0;1;1x1',
        '#TX;1;1;1x2;a',
        '#SP;2;1;1x3',
        '#TX;3;1;1x4;b',
        '#SP;4;1;1x5',
        '#ZZ;5;0;1x6',
      ],
      'a:',
      ['#TX;0;1;1x1;a', '#CL;1;1;1x2', '#ZZ;2;0;1x3'],
    ])
  })

  it('line', () => {
    alleq([
      '{a:1,\nb:2}',
      [
        '#OB;0;1;1x1',

        '#TX;1;1;1x2;a',
        '#CL;2;1;1x3',
        '#NR;3;1;1x4;1',

        '#CA;4;1;1x5',
        '#LN;5;1;1x6',

        '#TX;6;1;2x1;b',
        '#CL;7;1;2x2',
        '#NR;8;1;2x3;2',

        '#CB;9;1;2x4',
        '#ZZ;10;0;2x5',
      ],
    ])
  })


  // Shared cross-runtime fixture for string.allowControl (the Go
  // counterpart is TestSpecLexStringControl in go/utility_spec_test.go).
  // Columns: allowControl | input | expected, where expected is either
  // ERROR:<code> or #ST:<string value>. \t \n \r are unescaped by loadTSV
  // in BOTH columns, so the input carries a real control char.
  it('string-allow-control-spec', () => {
    for (const { cols, row } of loadTSV('lex-string-control')) {
      const [allowControl, src, expected] = cols
      try {
        const inst = new Tabnas({
          string: { allowControl: 'true' === allowControl },
        }).make()
        const lexer = makeLex({
          src: () => src,
          cfg: inst.internal().config,
          opts: inst.options,
          sub: {},
        })
        const tkn = lexer.next()

        const actual =
          inst.token.BD === tkn.tin
            ? 'ERROR:' + tkn.why
            : tkn.name + ':' + tkn.val

        assert.equal(actual, expected)
      } catch (err) {
        err.message =
          `lex-string-control row ${row}: allowControl=${allowControl}` +
          ` input=${JSON.stringify(src)} expected=${JSON.stringify(expected)}\n` +
          err.message
        throw err
      }
    }
  })

  // Shared cross-runtime fixture for what terminates an unquoted text run
  // at a line terminator (the Go counterpart is
  // TestSpecLexTextLineTerminator in go/lexer_optionplumbing_test.go).
  // Columns: lineLex | fixedSep | input | expected, same ERROR:<code> /
  // <name>:<value> contract as lex-string-control above. `fixedSep` registers
  // U+2028 as a fixed token, which is the case where the separator DOES get
  // an ender alternative and the run ends normally instead of failing.
  //
  // The rule under test comes from the REGEX DIALECT, not the ender set:
  // the TS ender is built as `cfg.line.lex ? 'y' : 'ys'`, so with line
  // lexing on `.` cannot cross a JS line terminator. \n and \r are also
  // enders, so they END the run; U+2028 and U+2029 are not, so the match
  // FAILS and no text token is produced at all. Go, whose RE2 `.` excludes
  // only \n, ran straight through both and made `a<U+2028>b` one token.
  //
  // The NUMBER matcher is covered here too. Its ender regex has its own
  // alternatives and does not contain these separators either, so
  // `1<U+2028>b` is `unexpected` rather than `#NR:1` — and `1\nb` is the
  // control that keeps the rule from being read as "reject everything".
  // Getting this wrong is easy in the Go port, where one predicate answers
  // both "can text continue?" and "can a number end here?".
  //
  // The U+2028/U+2029 cells hold the RAW code point: the shared escape
  // codec is deliberately minimal (\n \r \t \\ only, see
  // support/ts/src/escape.ts) and has no \u form. The guard below is why
  // that is safe to rely on — an editor or tool that normalised them away
  // would otherwise leave these rows passing while testing nothing.
  it('text-line-terminator-spec', () => {
    const rows = [...loadTSV('lex-text-line-terminator')]

    // The fixture must still contain the characters it is about.
    const raw = rows.map(({ cols }) => cols.join('')).join('')
    assert.ok(
      raw.includes('\u2028') && raw.includes('\u2029'),
      'lex-text-line-terminator.tsv no longer contains U+2028/U+2029 — the ' +
      'raw code points were normalised away and these rows now test nothing',
    )

    for (const { cols, row } of rows) {
      const [lineLex, fixedSep, src, expected] = cols
      try {
        const opts = { line: { lex: 'true' === lineLex } }
        if ('true' === fixedSep) {
          opts.fixed = { token: { '#SEP': '\u2028' } }
        }
        const inst = new Tabnas(opts).make()
        const lexer = makeLex({
          src: () => src,
          cfg: inst.internal().config,
          opts: inst.options,
          sub: {},
        })
        const tkn = lexer.next()

        const actual =
          inst.token.BD === tkn.tin
            ? 'ERROR:' + tkn.why
            : tkn.name + ':' + tkn.val

        assert.equal(actual, expected)
      } catch (err) {
        err.message =
          `lex-text-line-terminator row ${row}: lineLex=${lineLex}` +
          ` fixedSep=${fixedSep} input=${JSON.stringify(src)}` +
          ` expected=${JSON.stringify(expected)}\n` +
          err.message
        throw err
      }
    }
  })


  // options.string.check and options.comment.check were declared and
  // consulted by the lexer but never copied into the config, so the
  // hooks were dead. text.check (which always worked) is the control.
  it('string-comment-check-hooks-are-wired', () => {
    const seen = []
    const hook = (name) => (lex) => {
      seen.push(name + '@' + lex.pnt.sI)
      return undefined
    }

    const inst = new Tabnas({
      string: { check: hook('string') },
      comment: {
        lex: true,
        def: { hash: { line: true, start: '#' } },
        check: hook('comment'),
      },
      text: { check: hook('text') },
    }).make()

    const cfg = inst.internal().config
    assert.equal(typeof cfg.string.check, 'function')
    assert.equal(typeof cfg.comment.check, 'function')
    assert.equal(typeof cfg.text.check, 'function')

    // A check hook that claims the match short-circuits its matcher.
    const claim = new Tabnas({
      string: {
        check: (lex) => {
          const p = lex.pnt
          const tkn = lex.token('#VL', 'CLAIMED', undefined, p)
          p.sI = lex.src.length
          return { done: true, token: tkn }
        },
      },
    }).make()
    const lexer = makeLex({
      src: () => '"abc"',
      cfg: claim.internal().config,
      opts: claim.options,
      sub: {},
    })
    assert.equal(lexer.next().val, 'CLAIMED')
  })

  // A check hook must also survive the first-char dispatch table: the
  // hook has to run for chars the matcher would otherwise never see.
  it('string-comment-check-hooks-bypass-dispatch', () => {
    for (const which of ['string', 'comment']) {
      const seen = []
      const inst = new Tabnas({
        comment: { lex: true, def: { hash: { line: true, start: '#' } } },
        [which]: {
          ...(which === 'comment'
            ? { lex: true, def: { hash: { line: true, start: '#' } } }
            : {}),
          check: (lex) => {
            seen.push(lex.pnt.sI)
            return undefined
          },
        },
      }).make()

      const lexer = makeLex({
        src: () => 'abc',
        cfg: inst.internal().config,
        opts: inst.options,
        sub: {},
      })
      lexer.next()

      // 'a' is neither a quote nor a comment start, so without the
      // dispatch-table opt-out the hook would never have been called.
      assert.ok(0 < seen.length, which + ' check hook was not called')
    }
  })


  function st(tkn) {
    let out = []

    function m(s, v, t) {
      return [
        s.substring(0, 3),
        t.sI,
        t.len,
        t.rI + 'x' + t.cI,
        v ? '' + t.val : null,
      ]
    }

    switch (tkn.tin) {
      case t.SP:
        out = m('#SP', 0, tkn)
        break

      case t.LN:
        out = m('#LN', 0, tkn)
        break

      case t.OB:
        out = m('#OB{', 0, tkn)
        break

      case t.CB:
        out = m('#CB}', 0, tkn)
        break

      case t.OS:
        out = m('#OS[', 0, tkn)
        break

      case t.CS:
        out = m('#CS]', 0, tkn)
        break

      case t.CL:
        out = m('#CL:', 0, tkn)
        break

      case t.CA:
        out = m('#CA,', 0, tkn)
        break

      case t.NR:
        out = m('#NR', 1, tkn)
        break

      case t.ST:
        out = m('#ST', 1, tkn)
        break

      case t.TX:
        out = m('#TX', 1, tkn)
        break

      case t.VL:
        out = m('#VL', 1, tkn)
        break

      case t.CM:
        out = m('#CM', 0, tkn)
        break

      case t.BD:
        tkn.val =
          (undefined === tkn.val
            ? undefined === tkn.src
              ? ''
              : tkn.src
            : tkn.val) +
          '~' +
          tkn.why
        out = m('#BD', 1, tkn)
        break

      case t.ZZ:
        out = m('#ZZ', 0, tkn)
        break
    }

    return out.filter((x) => null != x).join(';')
  }
})
