/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// RuleDone.probe: a pass made while a probe is open (Context.probeOpen,
// which `@probeInit$` calls) is the probe's, not the document's, since the
// grammar rewinds past it. A transducer listening to ruleDone drops those
// passes; the shared fixture test/spec/probe.tsv pins which passes they
// are, and every runtime runs it.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { Context } = require('../dist/context')
const { json } = require('../dist-test/json-plugin')
const { loadTSV } = require('./utility')

const STATE = { o: 'open', c: 'close' }

function collectSpecs(rows) {
  const specs = {}
  for (const { cols, row } of rows) {
    if (1 !== cols.length || !cols[0].startsWith('# @spec ')) continue
    const rest = cols[0].slice('# @spec '.length)
    const sp = rest.indexOf(' ')
    assert.ok(0 < sp, `line ${row}: malformed # @spec directive`)
    specs[rest.slice(0, sp)] = JSON.parse(rest.slice(sp + 1))
  }
  return specs
}

describe('probe', () => {
  it('runs the shared probe contract (probe.tsv)', () => {
    const rows = loadTSV('probe')
    const specs = collectSpecs(rows)
    let ran = 0
    for (const { cols, row } of rows) {
      if (1 === cols.length && cols[0].startsWith('#')) continue
      assert.equal(cols.length, 4, `probe.tsv:${row}: expected 4 columns`)
      const [name, argRaw, input, want] = cols
      const spec = specs[JSON.parse(argRaw).spec]
      assert.ok(spec, `${name}: unknown spec`)
      const tn = new Tabnas()
      tn.grammar(JSON.parse(JSON.stringify(spec)))
      const probe = []
      let kept = 0
      tn.sub({
        ruleDone: (rule, _ctx, done) => {
          if (done.probe) probe.push(`${rule.name}:${STATE[done.state]}`)
          else kept++
        },
      })
      tn.parse(input)
      assert.equal(JSON.stringify({ probe, kept }), want, name)
      ran++
    }
    assert.ok(0 < ran, 'probe.tsv ran no cases')
  })

  it('flags no pass of a grammar that never probes', () => {
    const tn = new Tabnas({ plugins: [json] })
    let passes = 0
    tn.sub({
      ruleDone: (_rule, _ctx, done) => {
        passes++
        assert.equal(done.probe, false)
      },
    })
    tn.parse('{"a":[1,2,{"b":null}]}')
    assert.ok(0 < passes)
  })

  it('counts nested probes, and flags the pass that closes the last', () => {
    // The probe methods need no parse: a bare context with their state.
    const ctx = Object.assign(Object.create(Context.prototype), {
      probe: 0,
      probePass: false,
    })
    assert.equal(ctx.probeTake(), false)
    ctx.probeOpen()
    assert.equal(ctx.probeTake(), true, 'the pass that opens a probe')
    ctx.probeOpen()
    ctx.probeClose()
    assert.equal(ctx.probeTake(), true, 'still inside the outer probe')
    ctx.probeClose()
    assert.equal(ctx.probe, 0)
    assert.equal(ctx.probeTake(), true, 'the pass that closes the outer probe')
    assert.equal(ctx.probeTake(), false, 'the next pass is the document again')
    ctx.probeClose()
    assert.equal(ctx.probe, 0, 'an unmatched close does not go negative')
  })
})
