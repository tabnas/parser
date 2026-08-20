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
