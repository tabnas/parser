/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Post-process rule event: tn.sub({ ruleDone }). Fires AFTER each
// rule pass with matched tokens recorded on rule.o / rule.c, unlike
// the pre-process `rule` sub which sees an empty rule. This is the
// span-bearing structural stream the LSP outline builds on.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

function collect(tn) {
  const events = []
  tn.sub({
    ruleDone: (rule, ctx, done) => {
      events.push({
        i: rule.i,
        name: rule.name,
        state: done.state,
        forced: !!done.forced,
        alt: done.alt,
        o0: rule.o0 && rule.o0.sI,
        c0: rule.c0 && rule.c0.sI,
        oN: rule.oN,
        cN: rule.cN,
      })
    },
  })
  return events
}

describe('ruledone', () => {
  it('fires after each pass with matched tokens visible', () => {
    const tn = new Tabnas({ plugins: [json] })
    const events = collect(tn)
    const src = '{"a":1}'
    tn.parse(src)
    assert.ok(0 < events.length)

    // The map rule's open pass matched the brace at index 0, and its
    // close pass the brace at the end — the exact spans an outline
    // needs, which the pre-process `rule` event cannot see.
    const mapOpen = events.find((e) => 'map' === e.name && 'o' === e.state)
    const mapClose = events.find((e) => 'map' === e.name && 'c' === e.state)
    assert.ok(mapOpen, 'map open event')
    assert.ok(mapClose, 'map close event')
    assert.equal(mapOpen.o0, src.indexOf('{'))
    assert.equal(mapClose.c0, src.indexOf('}'))
    assert.equal(mapOpen.i, mapClose.i, 'same rule instance')
  })

  it('reconstructs balanced nested spans', () => {
    const tn = new Tabnas({ plugins: [json] })
    const events = collect(tn)
    const src = '{"a":[1,{"b":2}]}'
    tn.parse(src)

    // Pair open/close events by rule instance id for structural rules.
    const opens = new Map()
    const spans = []
    for (const e of events) {
      if ('map' !== e.name && 'list' !== e.name) continue
      if ('o' === e.state && 0 < e.oN) opens.set(e.i, e)
      if ('c' === e.state && 0 < e.cN) {
        const open = opens.get(e.i)
        if (open) spans.push({ name: e.name, start: open.o0, end: e.c0 })
      }
    }

    const byStart = spans.sort((a, b) => a.start - b.start)
    assert.deepStrictEqual(
      byStart.map((s) => [s.name, s.start, s.end]),
      [
        ['map', 0, src.length - 1],
        ['list', src.indexOf('['), src.indexOf(']')],
        ['map', src.indexOf('{', 1), src.indexOf('}', 1)],
      ],
    )
  })

  it('exposes the matched alternate routing (push/replace)', () => {
    const tn = new Tabnas({ plugins: [json] })
    const events = collect(tn)
    tn.parse('{"a":1,"b":2}')
    // Some pass pushed or replaced a rule — the event carries the name.
    assert.ok(
      events.some((e) => e.alt && ('' !== e.alt.p || '' !== e.alt.r)),
      'at least one push/replace routing observed',
    )
  })

  it('synthesizes forced close events for rules popped by recovery', () => {
    const tn = new Tabnas({
      plugins: [json],
      parse: { recover: { enabled: true } },
    })
    const events = collect(tn)
    // The list cannot close on '}' — recovery pops it to reach the map.
    const out = tn.parse('{"a":[1,2}')
    assert.ok(1 <= out.errors.length)
    const forced = events.filter((e) => e.forced)
    assert.ok(
      forced.some((e) => 'list' === e.name && 'c' === e.state),
      'forced close for the abandoned list; got: ' +
        JSON.stringify(forced.map((e) => e.name + ':' + e.state)),
    )
  })

  it('does not fire when not subscribed', () => {
    const tn = new Tabnas({ plugins: [json] })
    const out = tn.parse('{"a":[1,2]}')
    assert.equal(JSON.stringify(out), '{"a":[1,2]}')
  })
})
