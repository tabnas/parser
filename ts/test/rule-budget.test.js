/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */
'use strict'

// The rule-iteration guard, at both ends of `rule.maxmul`.
//
// The guard's budget is `2 * ruleCount * srcLen * 2 * maxmul`, floored at
// 100, in both ports. Its job is to stop a runaway grammar. It must never
// stop a NON-runaway one, because when it does the parse ends incomplete
// and reports `unexpected` — a SYNTAX code, on a valid document, caused
// by a configuration value the document knows nothing about.
//
// Both ports did exactly that, at opposite ends, which is why this file
// and go/rule_budget_test.go exist and assert the same table:
//
//   - This port, at the bottom. `rule.maxmul: 0` was honoured literally,
//     yielding a zero budget; negatives yielded a negative one; NaN made
//     every comparison false. All three rejected `[1,2,3]`. Repaired by
//     falling back to the default, which Go already did.
//
//   - Go, at the top. Its product wrapped, and the floor turned the
//     negative result into 100, so `MaxMul` at 2^63-1 rejected input the
//     default accepts — while 1e18 overflowed to a large POSITIVE value
//     and parsed fine. Noise, not a ceiling. Repaired with a saturating
//     multiply; float64 needs none, saturating at Infinity instead.
//
// Audit item P7. Recorded before this in go/doc/differences.md under
// "Rule-Iteration Budget", in a section headed "These affect parse output
// for the same input" — DIVERGENCE.md's own definition of a divergence,
// in a file DIVERGENCE.md cites as a porting guide. Filed as a porting
// note, it was never pinned, and neither end had a test.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

// A valid document that needs well over 100 rule iterations, so a budget
// collapsed to the floor cannot complete it.
const BIG = '[' + '1,'.repeat(60) + '1]'

describe('rule-budget', () => {
  it('never rejects a valid parse, at either end of maxmul', () => {
    const cases = [
      ['unset (default 3)', undefined],
      ['1', 1],

      // Non-positive and NaN: coerced to the default. Each of these
      // rejected BIG before the repair.
      ['0', 0],
      ['-1', -1],
      ['-1e308', -1e308],
      ['NaN', NaN],

      // The top end. Go overflowed on all of these and rejected at the
      // last one; float64 has always been fine here, and the row is kept
      // so the two files assert the same table.
      ['1e9', 1e9],
      ['1e18', 1e18],
      ['2^63-1', 9223372036854775807],
      ['Number.MAX_SAFE_INTEGER', Number.MAX_SAFE_INTEGER],
      ['Infinity', Infinity],
    ]

    for (const [label, maxmul] of cases) {
      const opts = { plugins: [json] }
      if (undefined !== maxmul) opts.rule = { maxmul }
      const got = new Tabnas(opts).parse(BIG)
      assert.equal(got.length, 61, 'maxmul ' + label)
    }
  })

  it('still stops a runaway', () => {
    // Without this, the test above is satisfied by deleting the budget
    // entirely — the failure mode of every repair that makes something
    // stop rejecting.
    const j = new Tabnas({ rule: { start: 'loop', exclude: 'tabnas,imp' } })
    j.rule('loop', (rs) => {
      // Pushes itself without consuming a token: unbounded.
      rs.open({ s: [], p: 'loop' })
    })
    assert.throws(() => j.parse('a'), (e) => 'unexpected' === e.code)

    // And it is the BUDGET stopping it, not the grammar: the same shape
    // with a token to consume terminates on its own.
    //
    // Deliberately NOT asserted at a large maxmul. A large multiplier
    // means a large budget in both ports now, so the runaway simply runs
    // longer — that is what asking for it means. What it must not do is
    // what Go did before the repair, which was to stop EARLY.
    const ok = new Tabnas({ rule: { start: 'one', exclude: 'tabnas,imp' } })
    ok.rule('one', (rs) => {
      rs.open({ s: [ok.token('#TX')], a: (r) => { r.node = r.o0.val } })
      rs.close({ s: [ok.token('#ZZ')] })
    })
    assert.equal(ok.parse('a'), 'a')
  })

  it('measures the source in UTF-16 units, as Go now does', () => {
    // The budget scales with SOURCE LENGTH, and the two ports must
    // measure that length in the same unit. They did not: UTF-16 code
    // units here (`lex.src.length`, free) and BYTES in Go
    // (`len(src)`). Same document, different budget, for anything above
    // U+007F.
    //
    // Reachable, and found by review rather than by any sweep — every
    // sweep so far varied the OPTION and left the source ASCII. The
    // grammar below pushes N children before consuming the source,
    // which separates the two: over 30 astral characters Go parsed to
    // N = 2879 and this port only to N = 1439, so every N in between
    // was accept-in-Go / reject-here on identical input.
    //
    // Aligned by counting UTF-16 units in Go too (`utf16Len`), which
    // costs one non-decoding pass there and leaves ASCII untouched.
    // NOT aligned on rune counts: this port would need an O(n) string
    // iteration to get those, on a hot path where it reads a field.
    //
    // go/rule_budget_test.go asserts the same five rows with the same
    // two numbers. Audit item P7.
    const deepPushParses = (n, src) => {
      const j = new Tabnas({ rule: { start: 'top', exclude: 'tabnas,imp' } })
      j.rule('top', (rs) => rs
        .open([{ s: [], p: 'deep' }])
        .close([{ s: ['#ZZ'], a: (r) => { r.node = r.child.node } }]))
      j.rule('deep', (rs) => rs
        .open([
          { s: [], p: 'deep', c: (r) => r.d < n },
          { s: ['#TX'], a: (r) => { r.node = r.o0.val } },
        ])
        .close([{ a: (r) => { if (undefined === r.node) r.node = r.child.node } }]))
      try {
        j.parse(src)
        return true
      } catch (e) {
        return false
      }
    }

    const EM = '\u{1F600}'
    const cases = [
      // 60 UTF-16 units each, whatever their byte length: 60, 120 and
      // 180 bytes respectively. All three had different budgets in Go
      // before; all three have the same one now.
      ['60 ascii', 'a'.repeat(60), 1439],
      ['30 astral', EM.repeat(30), 1439],
      ['60 U+20AC', '\u20ac'.repeat(60), 1439],
      ['30 ascii + 15 astral', 'a'.repeat(30) + EM.repeat(15), 1439],

      // The control: twice the units, twice the budget. Without this
      // the rows above are also satisfied by ignoring source length.
      ['120 ascii', 'a'.repeat(120), 2879],
    ]

    for (const [label, src, last] of cases) {
      assert.equal(deepPushParses(last, src), true, label + ' N=' + last)
      assert.equal(deepPushParses(last + 1, src), false,
        label + ' N=' + (last + 1) + ' should exceed the budget')
    }
  })

  it('a fractional maxmul is expressible here and not in Go', () => {
    // `rule.maxmul` is a `number` in TypeScript and a `*int` in Go, so a
    // multiplier between 0 and 1 shrinks the budget here and cannot be
    // written there at all. Not repaired: it is the option TYPE, and
    // narrowing TypeScript's to an integer would break callers for a
    // setting nobody tunes fractionally. Recorded in DIVERGENCE.md under
    // "Rule-iteration budget: fractional maxmul".
    //
    // The floor still protects short parses, which is why the assertion
    // is about BIG and not about a small document.
    assert.throws(
      () => new Tabnas({ plugins: [json], rule: { maxmul: 0.01 } }).parse(BIG),
      (e) => 'unexpected' === e.code,
    )
    assert.equal(
      new Tabnas({ plugins: [json], rule: { maxmul: 0.01 } }).parse('[1]').length,
      1,
      'the 100 floor still completes a short parse',
    )
  })
})
