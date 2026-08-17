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

  it('a complete prefix offers only end-of-source', () => {
    // A finished document can still be *continued* only by ending: the
    // set is computed at the end-of-source fetch, so it is precise
    // rather than empty.
    assert.deepStrictEqual(tn.continuations('{"a":1}').tokens, ['#ZZ'])
  })

  it('answers for prefixes that parse successfully', () => {
    // Permissive grammars parse most mid-edit prefixes (jsonic reads
    // `{a:` as an implicit null). Reporting nothing for a successful
    // parse would leave completion empty exactly where an editor asks
    // for it, so the set is captured at end-of-source instead.
    const afterColon = tn.continuations('{"a":').tokens
    assert.ok(afterColon.includes('#NR'), 'value starters offered: ' + afterColon)
    assert.ok(afterColon.includes('#ST'), 'value starters offered: ' + afterColon)
  })

  it('offers the next element after a separator (push closure)', () => {
    // `[1,` has fully matched the separator alternate, so the element
    // rule it pushes contributes its opening tokens here.
    const t = tn.continuations('[1,').tokens
    assert.ok(t.includes('#NR'), 'next element starters: ' + t)
    assert.ok(t.includes('#CS'), 'and the list closer: ' + t)
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

describe('continuations-review', () => {
  const { Tabnas: T } = require('..')
  const { json: J } = require('../dist-test/json-plugin')

  it('is path-aware across alternates', () => {
    // Alternates [A B] and [C D]: after matching A, only B may follow.
    const tn = new T({
      fixed: { token: { '#A': 'a', '#B': 'b', '#C': 'c', '#D': 'd' } },
    })
    const j = tn.make()
    for (const rn of Object.keys(j.rule())) j.rule(rn, null)
    j.rule('top', (rs) =>
      rs.open([{ s: ['#A', '#B'] }, { s: ['#C', '#D'] }]).close([{ s: ['#ZZ'] }]),
    )
    j.options({ rule: { start: 'top' } })
    const t = j.continuations('a').tokens
    assert.ok(t.includes('#B'), '#B offered: ' + t)
    assert.ok(!t.includes('#D'), '#D must NOT be offered (its C prefix did not match): ' + t)
  })

  it('sees rule()-installed grammars and later changes', () => {
    const tn = new T({ fixed: { token: { '#X': 'x', '#Y': 'y' } } })
    const j = tn.make()
    for (const rn of Object.keys(j.rule())) j.rule(rn, null)
    j.rule('top', (rs) => rs.open([{ s: ['#X', '#Y'] }]).close([{ s: ['#ZZ'] }]))
    j.options({ rule: { start: 'top' } })
    assert.deepStrictEqual(j.continuations('x').tokens, ['#Y'])
    // A later grammar change is picked up (generation invalidation).
    j.rule('top', (rs) => { rs.clearOpen(); rs.open([{ s: ['#Y', '#X'] }]) })
    assert.deepStrictEqual(j.continuations('y').tokens, ['#X'])
  })

  it('empty prefix offers the start rule openers', () => {
    const tn = new T({ plugins: [J] })
    const t = tn.continuations('').tokens
    assert.ok(t.includes('#OB'), '{ offered on empty doc: ' + t)
  })
})

describe('continuations-eof-review', () => {
  const { Tabnas: T } = require('..')

  // Grammar helper: bare instance, caller supplies rules.
  function bare(tokens, rules, start) {
    const tn = new T({ fixed: { token: tokens } })
    const j = tn.make()
    for (const rn of Object.keys(j.rule())) j.rule(rn, null)
    for (const [name, def] of Object.entries(rules)) j.rule(name, def)
    j.options({ rule: { start: start || 'top' } })
    return j
  }

  it('is path-aware on the success path too', () => {
    // Alternates [A B], [C D], [A]: "a" PARSES (third alternate), so
    // this exercises the end-of-source path. Only B may follow — D
    // belongs to an alternate whose own C prefix never matched.
    const j = bare(
      { '#A': 'a', '#B': 'b', '#C': 'c', '#D': 'd' },
      {
        top: (rs) =>
          rs
            .open([
              { s: ['#A', '#B'] },
              { s: ['#C', '#D'] },
              { s: ['#A'] },
            ])
            .close([{ s: ['#ZZ'] }]),
      },
    )
    const t = j.continuations('a').tokens
    assert.ok(t.includes('#B'), '#B offered: ' + t)
    assert.ok(!t.includes('#D'), '#D must not be offered: ' + t)
  })

  it('does not offer openers a backtracking alternate re-consumes', () => {
    // top: [A X] | [A]{b:1, p:child}; child opens on A. "a" parses by
    // letting child re-consume the backed-up A, so another A is NOT a
    // legal continuation here (only X is).
    const j = bare(
      { '#A': 'a', '#X': 'x' },
      {
        top: (rs) =>
          rs
            .open([{ s: ['#A', '#X'] }, { s: ['#A'], b: 1, p: 'child' }])
            .close([{ s: ['#ZZ'] }]),
        child: (rs) => rs.open([{ s: ['#A'] }]).close([{ s: ['#ZZ'] }]),
      },
    )
    const t = j.continuations('a').tokens
    assert.ok(t.includes('#X'), '#X offered: ' + t)
    assert.ok(!t.includes('#A'), '#A must not be offered (child re-consumes it): ' + t)
  })

  it('empty source answers even when lex.empty lets it parse', () => {
    // With lex.empty (the default) an empty document parses and no
    // lexer ever runs, so there is no end-of-source event to capture:
    // the start rule's own openers are the answer.
    const j = bare(
      { '#A': 'a', '#B': 'b' },
      { top: (rs) => rs.open([{ s: ['#A'] }, { s: ['#B'] }]).close([{ s: ['#ZZ'] }]) },
    )
    const t = j.continuations('').tokens
    assert.ok(t.includes('#A') && t.includes('#B'), 'openers offered: ' + t)
  })
})
