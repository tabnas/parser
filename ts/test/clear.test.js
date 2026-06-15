/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Replacement of a rule's alternates and lifecycle actions by a later
// plugin. Mirrored by go/clear_test.go. Three mechanisms, all opt-in and
// backwards compatible:
//   - imperative: rs.open(alts, { clear: true }), rs.clearOpen/clearClose,
//     rs.clearActions(...phases)
//   - declarative alternates: open: { alts, inject: { clear: true } }
//   - declarative lifecycle: the '@<rule>-<phase>/replace' fnref suffix

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

function tryParse(tn, src) {
  try {
    return { ok: true, val: tn.parse(src) }
  } catch (e) {
    return { ok: false, code: e.code }
  }
}

describe('clear', () => {
  it('mods.clear replaces open alternates (imperative)', () => {
    const tn = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a', Tb: 'b' } } })
    const { Ta, Tb } = tn.token
    // Plugin A.
    tn.rule('top', (rs) =>
      rs.open([{ s: [Ta], a: (r) => (r.node = 'A') }]).close([{ s: ['#ZZ'] }]),
    )
    // Plugin B replaces A's open alternates.
    tn.rule('top', (rs) => rs.open([{ s: [Tb], a: (r) => (r.node = 'B') }], { clear: true }))

    assert.deepEqual(tryParse(tn, 'a'), { ok: false, code: 'unexpected' })
    assert.deepEqual(tryParse(tn, 'b'), { ok: true, val: 'B' })
  })

  it('clearOpen / clearClose remove alternates without touching the other phase', () => {
    const tn = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a', Tb: 'b' } } })
    const { Ta, Tb } = tn.token
    tn.rule('top', (rs) =>
      rs.open([{ s: [Ta], a: (r) => (r.node = 'A') }]).close([{ s: ['#ZZ'] }]),
    )
    tn.rule('top', (rs) =>
      rs.clearOpen().open([{ s: [Tb], a: (r) => (r.node = 'B') }]),
    )
    assert.deepEqual(tryParse(tn, 'a'), { ok: false, code: 'unexpected' })
    assert.deepEqual(tryParse(tn, 'b'), { ok: true, val: 'B' })
    // Close untouched: 'b' still requires #ZZ to finish (it parsed fine).
  })

  it('clearActions replaces lifecycle actions', () => {
    const tn = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
    const { Ta } = tn.token
    const log = []
    tn.rule('top', (rs) =>
      rs.bo(() => log.push('A')).open([{ s: [Ta] }]).close([{ s: ['#ZZ'] }]),
    )
    tn.rule('top', (rs) => rs.clearActions('bo').bo(() => log.push('B')))
    tn.parse('a')
    assert.deepEqual(log, ['B'])
  })

  it('inject.clear replaces alternates (declarative grammar)', () => {
    const tn = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a', Tb: 'b' } } })
    tn.grammar({
      ref: { '@a': (r) => (r.node = 'A'), '@b': (r) => (r.node = 'B') },
      rule: { top: { open: [{ s: 'Ta', a: '@a' }], close: [{ s: '#ZZ' }] } },
    })
    tn.grammar({
      ref: { '@b': (r) => (r.node = 'B') },
      rule: { top: { open: { alts: [{ s: 'Tb', a: '@b' }], inject: { clear: true } } } },
    })
    assert.deepEqual(tryParse(tn, 'a'), { ok: false, code: 'unexpected' })
    assert.deepEqual(tryParse(tn, 'b'), { ok: true, val: 'B' })
  })

  it('@<rule>-<phase>/replace fnref replaces lifecycle actions', () => {
    const tn = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
    const log = []
    tn.grammar({
      ref: { '@top-bo': () => log.push('A') },
      rule: { top: { open: [{ s: 'Ta' }], close: [{ s: '#ZZ' }] } },
    })
    tn.grammar({
      ref: { '@top-bo/replace': () => log.push('B') },
      rule: { top: {} },
    })
    tn.parse('a')
    assert.deepEqual(log, ['B'])
  })

  it('/replace wins deterministically across make() re-derivation', () => {
    const log = []
    const pluginA = (tn) =>
      tn.rule('top', (rs) =>
        rs.fnref({ '@top-bo': () => log.push('A') })
          .open([{ s: ['Ta'] }])
          .close([{ s: ['#ZZ'] }]),
      )
    const pluginB = (tn) =>
      tn.rule('top', (rs) => rs.fnref({ '@top-bo/replace': () => log.push('B') }))
    const tn = new Tabnas({
      rule: { start: 'top' },
      fixed: { token: { Ta: 'a' } },
      plugins: [pluginA, pluginB],
    })
    log.length = 0
    tn.parse('a')
    assert.deepEqual(log, ['B'])

    const child = tn.make()
    log.length = 0
    child.parse('a')
    assert.deepEqual(log, ['B'], 'replace must survive re-derivation')
  })

  it('is backwards compatible: no clear/replace leaves behavior unchanged', () => {
    // Two plugins both contribute lifecycle actions and alternates; with
    // no clear/replace, all fire in registration order (the documented
    // append behavior).
    const tn = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
    const { Ta } = tn.token
    const log = []
    tn.rule('top', (rs) =>
      rs.bo(() => log.push('A')).open([{ s: [Ta] }]).close([{ s: ['#ZZ'] }]),
    )
    tn.rule('top', (rs) => rs.bo(() => log.push('B')))
    tn.parse('a')
    assert.deepEqual(log, ['A', 'B'])
  })
})
