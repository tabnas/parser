/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// RuleSpec.add (behind open() and close()) injects into the alternate
// array IN PLACE. process() reads def.open before the before-actions
// run, so an alternate a `bo` hook installs for the pass about to run
// has to land in the array that pass holds; and it lands there without
// a spread, which a compiled grammar with more alternates than the
// argument limit overflows (ts/test/alt-index.test.js pins that count).

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const tn = new Tabnas()

function instance() {
  const j = tn.make({
    rule: { start: 'top' },
    fixed: { token: { '#A': 'a', '#B': 'b' } },
  })
  Object.keys(j.rule()).map((rn) => j.rule(rn, null))
  return j
}

describe('rule-add', () => {

  it('an alternate a before-open hook installs is seen by that pass', () => {
    const j = instance()
    j.rule('top', (rs) => rs
      .bo((r) => {
        if (0 === r.spec.def.open.length) {
          r.spec.open([{ s: '#A', a: (r) => (r.node = 'hooked') }])
        }
      })
      .close([{ s: '#ZZ' }]))
    assert.equal(j.parse('a'), 'hooked')
  })

  it('a hook can prepend, append and clear for the pass it runs before', () => {
    const prepend = instance()
    prepend.rule('top', (rs) => rs
      .open([{ s: '#A', a: (r) => (r.node = 'original') }])
      .bo((r) => {
        if (1 === r.spec.def.open.length) {
          r.spec.open([{ s: '#A', a: (r) => (r.node = 'prepended') }])
        }
      })
      .close([{ s: '#ZZ' }]))
    assert.equal(prepend.parse('a'), 'prepended')

    const append = instance()
    append.rule('top', (rs) => rs
      .open([{ s: '#A', a: (r) => (r.node = 'original') }])
      .bo((r) => {
        if (1 === r.spec.def.open.length) {
          r.spec.open([{ s: '#B', a: (r) => (r.node = 'appended') }], { append: true })
        }
      })
      .close([{ s: '#ZZ' }]))
    assert.equal(append.parse('b'), 'appended')
    assert.equal(append.parse('a'), 'original')

    const clear = instance()
    clear.rule('top', (rs) => rs
      .open([{ s: '#A', a: (r) => (r.node = 'original') }])
      .bo((r) => {
        if (1 === r.spec.def.open.length) {
          r.spec.open([{ s: '#A', a: (r) => (r.node = 'replacement') }], { clear: true })
        }
      })
      .close([{ s: '#ZZ' }]))
    assert.equal(clear.parse('a'), 'replacement')
  })

  it('a hook whose modifier returns a fresh array leaves its pass scanning the one it holds', () => {
    // The pass read def.open before the hook ran. A custom modifier
    // replaces the array, so that pass scans the array it holds (with the
    // appended alternate in it) and the next pass scans the replacement.
    // The rebuilt first-token index describes the replacement, so the
    // pass holding the old array must not consult it.
    const j = instance()
    let hooked = false
    j.rule('top', (rs) => rs
      .open([{ s: '#A', a: (r) => (r.node = 'a') }])
      .bo((r) => {
        if (!hooked) {
          hooked = true
          r.spec.open([{ s: '#B', a: (r) => (r.node = 'b') }], {
            append: true,
            custom: (alts) => alts.slice().reverse(),
          })
        }
      })
      .close([{ s: '#ZZ' }]))
    assert.equal(j.parse('b'), 'b')
    assert.deepEqual(j.rule('top').def.open.map((alt) => alt.s[0]), ['#B', '#A'])
    assert.equal(j.parse('a'), 'a')
    assert.equal(j.parse('b'), 'b')
  })

})
