/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// tn.continuations(src): legal-continuation tokens after parsing src
// as a prefix — the completion surface of the unified-LSP design.
// Position-aware (deepest matched lookahead, not position 0) and
// widened by the pop-closure over empty-close ancestors.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

describe('continuations', () => {
  const tn = new Tabnas({ plugins: [json] })

  it('is position-aware: a key wants its colon', () => {
    // The review defect: position-0 expected[] lists key starters and
    // misses the one useful completion. The deepest-position read
    // yields exactly the colon.
    assert.deepStrictEqual(tn.continuations('{"a"').tokens, ['#CL'])
  })

  it('offers closers and separators after a complete pair', () => {
    const t = tn.continuations('{"a":1').tokens
    assert.ok(t.includes('#CB'), '} offered: ' + t)
    assert.ok(t.includes('#CA'), ', offered: ' + t)
  })

  it('pop-closure includes parent-level closers after a separator', () => {
    const t = tn.continuations('[1,').tokens
    assert.ok(t.includes('#NR'), 'value starters offered: ' + t)
    assert.ok(t.includes('#CS'), 'parent ] via pop-closure: ' + t)
  })

  it('a complete prefix has no continuations', () => {
    assert.deepStrictEqual(tn.continuations('{"a":1}').tokens, [])
  })

  it('uses a fail-fast sibling even when recovery is on', () => {
    const rn = new Tabnas({
      plugins: [json],
      parse: { recover: { enabled: true } },
    })
    assert.deepStrictEqual(rn.continuations('{"a"').tokens, ['#CL'])
    // And the instance's own parse still recovers.
    const out = rn.parse('{"a":1,}')
    assert.ok(Array.isArray(out.errors))
  })

  it('returns tins alongside names', () => {
    const c = tn.continuations('{"a"')
    assert.equal(c.tins.length, c.tokens.length)
    assert.equal(tn.token('#CL'), c.tins[0])
  })
})
