/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

// Alignment tests validate that TypeScript and Go produce identical results.
// Shared TSV files in test/spec/alignment-*.tsv are run by both this TS runner
// and the Go runner (go/alignment_test.go).

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama, jsonic, AmagamaError } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { loadTSV } = require('./utility')

const j = am
const JS = (x) => JSON.stringify(x)

// deepEqual compares values via JSON roundtrip to handle null-prototype objects.
function deepEqual(actual, expected, msg) {
  assert.deepStrictEqual(JSON.parse(JS(actual)), JSON.parse(JS(expected)), msg)
}

// --- Shared TSV test helpers ---

function tsvTest(name, parser) {
  parser = parser || am
  const entries = loadTSV(name)
  for (const { cols: [input, expected], row } of entries) {
    const result = parser.parse(input)
    deepEqual(result, JSON.parse(expected),
      `${name} row ${row}: input=${input} expected=${expected}`)
  }
}

function tsvErrorTest(name, parser) {
  parser = parser || am
  const entries = loadTSV(name)
  for (const { cols: [input, expected], row } of entries) {
    if (!expected.startsWith('ERROR:')) {
      throw new Error(`${name} row ${row}: expected column must start with ERROR:`)
    }
    const code = expected.slice(6)
    assert.throws(() => parser.parse(input), (err) => {
      return err instanceof AmagamaError && err.code === code
    }, `${name} row ${row}: input=${input} expected=${expected}`)
  }
}

function tsvNullTest(name) {
  const entries = loadTSV(name)
  for (const { cols: [input, expected], row } of entries) {
    const result = J(input)
    // TS returns undefined for empty/comment-only input, which is equivalent
    // to Go's nil. The TSV says "null" but in TS this is actually undefined.
    assert.ok(result === undefined || result === null,
      `${name} row ${row}: input=${input} expected null/undefined, got ${JS(result)}`)
  }
}

describe('alignment', function () {
  // --- Shared TSV tests ---

  it('alignment-values', () => {
    tsvTest('alignment-values')
  })

  it('alignment-safe-key', () => {
    // TS uses Object.create(null) so __proto__ is a normal key on objects.
    // safe.key only blocks __proto__ on arrays (which have real prototypes).
    const entries = loadTSV('alignment-safe-key')
    for (const { cols: [input, expected], row } of entries) {
      const result = J(input)
      const exp = JSON.parse(expected)
      deepEqual(result, exp,
        `alignment-safe-key row ${row}: input=${input} expected=${expected}`)
    }
  })

  it('alignment-map-merge', () => {
    tsvTest('alignment-map-merge')
  })

  it('alignment-number-text', () => {
    tsvTest('alignment-number-text')
  })

  it('alignment-structure', () => {
    tsvTest('alignment-structure')
  })

  it('alignment-empty', () => {
    tsvNullTest('alignment-empty')
  })

  it('alignment-errors', () => {
    tsvErrorTest('alignment-errors')
  })

  // --- Exclude group TSV tests ---

  it('exclude-strict-json', () => {
    const jj = am.make({ rule: { exclude: 'amagama,imp' } })
    tsvTest('exclude-strict-json', jj)
  })

  it('exclude-strict-json-errors', () => {
    const jj = am.make({ rule: { exclude: 'amagama,imp' } })
    tsvErrorTest('exclude-strict-json-errors', jj)
  })

  it('exclude-comma', () => {
    const jj = am.make({ rule: { exclude: 'comma' } })
    tsvTest('exclude-comma', jj)
  })

  it('exclude-comma-errors', () => {
    const jj = am.make({ rule: { exclude: 'comma' } })
    tsvErrorTest('exclude-comma-errors', jj)
  })

  // --- Include group TSV tests (parity for rule.include) ---

  it('include-json', () => {
    // include="json" keeps only json-tagged alts, producing a parser that
    // behaves like strict-JSON. Shared TSV ensures TS and Go agree.
    const jj = am.make({ rule: { include: 'json' } })
    tsvTest('include-json', jj)
  })

  it('include-json-errors', () => {
    const jj = am.make({ rule: { include: 'json' } })
    tsvErrorTest('include-json-errors', jj)
  })

  // --- Comment suffix TSV tests (parity for comment.def.suffix) ---

  it('feature-comment-suffix-line', () => {
    // Line comment with a custom suffix terminator — suffix is consumed.
    // NOTE: TS's makeCommentMatcher requires lex:true explicitly on every
    // def (Go defaults it to true); setting it here keeps both runners
    // configured identically.
    const jj = am.make({
      comment: {
        def: {
          hash: { line: true, start: '#', lex: true, suffix: '@@' },
          line: { line: true, start: '//', lex: true },
          block: { line: false, start: '/*', end: '*/', lex: true },
        },
      },
    })
    tsvTest('feature-comment-suffix-line', jj)
  })

  it('feature-comment-suffix-block', () => {
    // Block comment with a custom suffix terminator — suffix is consumed
    // and short-circuits the usual End-required behaviour.
    const jj = am.make({
      comment: {
        def: {
          hash: { line: true, start: '#', lex: true },
          line: { line: true, start: '//', lex: true },
          block: { line: false, start: '/*', end: '*/', lex: true, suffix: '!!' },
        },
      },
    })
    tsvTest('feature-comment-suffix-block', jj)
  })

  // --- Lex error propagation tests ---
  // Verifies that lex-level errors (unterminated_string, unterminated_comment)
  // are not masked by generic "unexpected" in any parser state.

  it('lex-errors-default', () => {
    tsvErrorTest('lex-errors')
  })

  it('lex-errors-exclude-amagama-imp', () => {
    const jj = am.make({ rule: { exclude: 'amagama,imp' } })
    tsvErrorTest('lex-errors', jj)
  })

  it('lex-errors-exclude-amagama-imp-comma', () => {
    const jj = am.make({ rule: { exclude: 'amagama,imp,comma' } })
    tsvErrorTest('lex-errors', jj)
  })

  // --- Direct TS tests for option-dependent features ---

  it('map-extend-false', () => {
    const ji = am.make({ map: { extend: false } })
    deepEqual(ji.parse('{a:{b:1},a:{c:2}}'), { a: { c: 2 } })
  })

  it('map-merge-func', () => {
    const ji = am.make({
      map: {
        merge: (prev, val) => prev,
      },
    })
    deepEqual(ji.parse('{a:1,a:2}'), { a: 1 })
  })

  it('safe-key-objects', () => {
    const result = J('{__proto__:1,a:2}')
    assert.strictEqual(result.__proto__, 1)
    assert.strictEqual(result.a, 2)
  })

  it('safe-key-arrays', () => {
    const result = J('[1,2,__proto__:3]')
    deepEqual(result, [1, 2])
    assert.notStrictEqual(result.__proto__.toString, '3')
  })

  it('safe-key-false', () => {
    const ji = am.make({ safe: { key: false } })
    const result = ji.parse('[1,2,__proto__:{toString:FAIL}]')
    assert.ok(('' + result.toString).startsWith('FAIL'))
  })

  it('string-escape-errors', () => {
    const ji = am.make({ string: { allowUnknown: false } })
    assert.throws(() => ji.parse('"\\w"'))
  })

  it('string-abandon', () => {
    const ji = am.make({ string: { abandon: true } })
    const result = ji.parse('"abc')
    assert.ok(result !== undefined && result !== null)
  })

  it('string-replace', () => {
    const ji = am.make({
      string: { replace: { A: 'B', D: '' } },
    })
    assert.strictEqual(ji.parse('"aAc"'), 'aBc')
    assert.strictEqual(ji.parse('"aAcDe"'), 'aBce')
  })

  it('number-exclude', () => {
    const ji = am.make({
      number: {
        exclude: /^00/,
      },
    })
    assert.strictEqual(ji.parse('0099'), '0099')
    assert.strictEqual(ji.parse('99'), 99)
  })

  it('line-single', () => {
    const ji = am.make({ line: { single: true } })
    deepEqual(ji.parse('a\n\nb'), ['a', 'b'])
  })

  it('comment-eatline', () => {
    const ji = am.make({
      comment: {
        def: {
          hash: { line: true, start: '#', eatline: true },
          line: { line: true, start: '//' },
          block: { line: false, start: '/*', end: '*/' },
        },
      },
    })
    deepEqual(ji.parse('a:1#x\nb:2'), { a: 1, b: 2 })
  })

  it('text-modify', () => {
    const ji = am.make({
      text: {
        modify: [(val) => (typeof val === 'string' ? val.toUpperCase() : val)],
      },
    })
    assert.strictEqual(ji.parse('hello'), 'HELLO')
    assert.strictEqual(ji.parse('"hello"'), 'hello')
  })

  it('list-property-guard', () => {
    const ji = am.make({ list: { property: false, pair: false } })
    assert.throws(() => ji.parse('[a:1]'))
  })

  it('exclude-amagama', () => {
    const ji = am.make()

    let openBefore, closeBefore
    ji.rule('val', (rs) => {
      openBefore = rs.def.open.length
      closeBefore = rs.def.close.length
    })

    ji.rule('val', (rs) => {
      rs.def.open = rs.def.open.filter((a) => !a.g || !a.g.includes('amagama'))
      rs.def.close = rs.def.close.filter((a) => !a.g || !a.g.includes('amagama'))
      assert.ok(rs.def.open.length < openBefore)
      assert.ok(rs.def.close.length < closeBefore)
      for (const alt of rs.def.open) {
        const g = typeof alt.g === 'string' ? alt.g : ''
        assert.ok(!g.split(',').map(s => s.trim()).includes('amagama'),
          `val.open alt still has amagama tag: ${alt.g}`)
      }
    })
  })

  it('result-fail', () => {
    const ji = am.make({ result: { fail: ['FAIL'] } })
    assert.throws(() => ji.parse('FAIL'))
    assert.strictEqual(ji.parse('OK'), 'OK')
  })

  it('finish-rule-false', () => {
    const ji = am.make({ rule: { finish: false } })
    assert.throws(() => ji.parse('{a:1'))
  })

  it('empty-disabled', () => {
    const ji = am.make({ lex: { empty: false } })
    assert.throws(() => ji.parse(''))
  })

  it('custom-values', () => {
    const ji = am.make({
      value: {
        def: {
          true: { val: true },
          false: { val: false },
          null: { val: null },
          NaN: { val: 'NaN-custom' },
        },
      },
    })
    assert.strictEqual(ji.parse('NaN'), 'NaN-custom')
    assert.strictEqual(ji.parse('true'), true)
  })

  it('deep-undefined', () => {
    const { deep } = Amagama.util
    const base = { a: 1, b: 2 }
    const over = { a: undefined, b: 3 }
    const result = deep(base, over)
    assert.strictEqual(result.a, 1)
    assert.strictEqual(result.b, 3)
  })

  it('error-propagation', () => {
    assert.throws(() => j.parse('}'), /unexpected/)
    assert.throws(() => j.parse(']'), /unexpected/)
  })

  it('trailing-content', () => {
    assert.throws(() => j.parse('a:1,2'), /unexpected/)
  })

  it('lex-subscriber', () => {
    const ji = am.make()
    const tokens = []
    ji.sub({ lex: (tkn) => {
      tokens.push(tkn.tin)
    }})
    ji.parse('a:1')
    assert.ok(tokens.length > 0)
  })
})
