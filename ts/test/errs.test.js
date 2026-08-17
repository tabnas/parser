/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Per-parse error list: ctx.errs. Every TabnasError records itself on
// its Context's errs list at construction, so in today's fail-fast
// mode the thrown error is also the last (and only) entry. This is the
// groundwork for multi-error collection (ts/doc/lsp-feasibility.md):
// a recovery mode appends here and continues instead of throwing.
//
// Contract pinned here:
//   1. flags-off behaviour is unchanged — errors still throw;
//   2. the thrown error IS the recorded error (same object);
//   3. errs is per-parse — a fresh parse starts empty;
//   4. a clean parse records nothing.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, TabnasError } = require('..')
const { json } = require('../dist-test/json-plugin')

describe('errs', () => {
  it('records the thrown error on ctx.errs', () => {
    const tn = new Tabnas({ plugins: [json] })
    try {
      tn.parse('{"a":1,}')
      assert.fail('parse should fail')
    } catch (e) {
      assert.ok(e instanceof TabnasError)
      const ctx = e.internal.ctx
      assert.ok(Array.isArray(ctx.errs))
      assert.equal(ctx.errs.length, 1)
      assert.equal(ctx.errs[0], e)
    }
  })

  it('starts empty on every parse', () => {
    const tn = new Tabnas({ plugins: [json] })
    let first
    try {
      tn.parse('[1,')
      assert.fail('parse should fail')
    } catch (e) {
      first = e
    }
    try {
      tn.parse('{"b":')
      assert.fail('parse should fail')
    } catch (e) {
      // Second parse has its own Context and its own errs list: only
      // its own error, and not the first parse's.
      const errs = e.internal.ctx.errs
      assert.equal(errs.length, 1)
      assert.equal(errs[0], e)
      assert.notEqual(errs[0], first)
      assert.notEqual(e.internal.ctx, first.internal.ctx)
    }
  })

  it('records nothing on a clean parse', () => {
    const tn = new Tabnas({ plugins: [json] })
    let seen
    tn.sub({
      rule: (rule, ctx) => {
        seen = ctx
      },
    })
    const out = tn.parse('{"a":[1,2]}')
    // JSON round-trip: the json plugin builds null-prototype objects.
    assert.equal(JSON.stringify(out), '{"a":[1,2]}')
    assert.ok(seen, 'rule sub captured the parse context')
    assert.deepStrictEqual(seen.errs, [])
  })

  it('never throws from recording on a degenerate context', () => {
    // Plugin code can construct TabnasError with unusual ctx values —
    // construction must not depend on ctx.errs being present.
    const err = new TabnasError(
      'unexpected',
      {},
      { isToken: true, tin: 1, rI: 1, cI: 1, sI: 0, len: 0 },
      {},
      {},
    )
    assert.ok(err instanceof TabnasError)
  })
})
