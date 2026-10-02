/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// test/spec/bad-token.tsv: what the parser does with a bad token the lexer
// hands it, fail-fast, under recovery and under relexing, and the error a
// recovering parse ends on. The Go (go/bad_token_spec_test.go) and Rust
// (rs/tests/bad_token_spec_test.rs) runners assert the same rows; the
// fixture's header says what every column means, and what the custom
// matcher all three build must do.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { loadTSV } = require('./utility')

const JSON_GRAMMAR = require('./json-builder.fixture.json')

// The custom matcher the fixture header specifies: a bad token at `?`, and
// one running to the end of the source at `%`, neither moving the cursor.
const bad = {
  order: 1.5e6,
  make: () => (lex) => {
    const sI = lex.pnt.sI
    const c = lex.src[sI]
    if ('?' === c) return lex.bad('custom_bad', sI, sI + 1)
    if ('%' === c) return lex.bad('custom_unterminated', sI, lex.src.length)
    return undefined
  },
}

// The `opts` cell that asks for `continuations(input)` instead of a parse.
const CONTINUATIONS = 'continuations'

function make(grammar, grammars, opts) {
  const tn = new Tabnas({ lex: { match: { bad } } })
  if ('json' === grammar) {
    tn.grammar(JSON_GRAMMAR)
    tn.grammar({ options: { rule: { start: 'val' } } })
  } else {
    assert.ok(null != grammars[grammar], 'no @grammar named ' + grammar)
    tn.grammar(grammars[grammar])
  }
  if ('-' !== opts && CONTINUATIONS !== opts) {
    tn.grammar({ options: JSON.parse(opts) })
  }
  return tn
}

// The shared canonical value: sorted keys, and an absent value as null.
function canon(v) {
  if (undefined === v || null === v) return 'null'
  if (Array.isArray(v)) return '[' + v.map(canon).join(',') + ']'
  if ('object' === typeof v) {
    return (
      '{' +
      Object.keys(v)
        .sort()
        .map((k) => JSON.stringify(k) + ':' + canon(v[k]))
        .join(',') +
      '}'
    )
  }
  return JSON.stringify(v)
}

function renderError(e) {
  const d = e.toJSON()
  const r = e.recovered
  const meta =
    null == r ? '' : (r.bad ? '+bad' : '+skip') + r.skipped
  return d.code + '@' + d.row + ':' + d.col + meta
}

function run(tn, input, opts) {
  if (CONTINUATIONS === opts) {
    return [tn.continuations(input).tokens.join(','), '-']
  }
  const recovering = tn.internal().config.parse.recover.enabled
  try {
    const out = tn.parse(input)
    if (!recovering) return [canon(out), '-']
    const errors = out.errors.map(renderError)
    return [canon(out.value), 0 === errors.length ? '-' : errors.join(',')]
  } catch (e) {
    if (recovering) throw e
    return ['-', renderError(e)]
  }
}

describe('bad-token', () => {
  it('spec-fixture', () => {
    const grammars = {}
    let ran = 0
    for (const { cols, row } of loadTSV('bad-token')) {
      const at = /^#\s*@grammar\s+(\S+)\s+(.*)$/.exec(cols[0])
      if (null != at) {
        grammars[at[1]] = JSON.parse(at[2])
        continue
      }
      if (cols[0].startsWith('#') || cols.length < 5) continue
      const [grammar, opts, input, value, errors] = cols
      const [gotValue, gotErrors] = run(make(grammar, grammars, opts), input, opts)
      const where = 'bad-token.tsv row ' + row + ' ' + JSON.stringify(input) + ' ' + opts
      assert.equal(gotErrors, errors, where + ': errors')
      assert.equal(gotValue, value, where + ': value')
      ran++
    }
    assert.ok(20 < ran, 'bad-token.tsv ran only ' + ran + ' rows')
  })

  // Two answers Go does not give, so they are not in the shared fixture:
  // with relexing and recovery together Go lists a bad token no alternate
  // re-cuts twice, once unrecovered, and Go reads the maxRecoveries cap
  // before recording an error, so a cascade at the cap ends its parse.
  // rs/tests/bad_token_fetch_test.rs pins the same answers in Rust.
  it('relexing-with-recovery-and-a-cascade-at-the-cap', () => {
    const answer = (opts, input) => run(make('json', {}, JSON.stringify(opts)), input)
    const both = { lex: { relex: true }, parse: { recover: { enabled: true } } }
    assert.deepEqual(answer(both, '[1,?,2]'), ['[1,2]', 'custom_bad@1:4+skip1'])
    assert.deepEqual(answer(both, '[1,??,2]'), ['[1,2]', 'custom_bad@1:4+skip2'])
    assert.deepEqual(answer(both, '[1,? 2,3]'), ['[1,3]', 'custom_bad@1:4+skip2'])

    const capped = { parse: { recover: { enabled: true, maxRecoveries: 1 } } }
    assert.deepEqual(answer(capped, '[1,,,2]'), ['[1,2]', 'unexpected@1:4+skip0'])
  })

  // The third answer Go does not give. After a complete document, a bad
  // token (or any trailing content) is met by the trailing-content check,
  // which has no rule: continuations() answers with the start rule's
  // openers, as it does for a prefix no rule ever ran on. At a bad token a
  // rule fetched, nothing is recorded, and the answer is computed from the
  // buffer at the first position of that rule's alternates: `[?` offers
  // what `[` hands over to, not the `]` an alternate two tokens long still
  // wanted. rs/tests/bad_token_fetch_test.rs pins the same answers in Rust.
  it('continuations-after-a-complete-document-and-at-a-fetched-bad-token', () => {
    const nest = {
      options: { rule: { start: 'top' }, fixed: { token: { '#LB': '<', '#RB': '>' } } },
      rule: {
        top: { open: [{ s: '#LB', p: 'body' }], close: [{ s: '#RB' }] },
        body: { open: [{ s: '#NR' }], close: [{ s: '#RB', b: 1 }, { s: '#ZZ', b: 1 }] },
      },
    }
    const after = (src) => make('nest', { nest }, '-').continuations(src).tokens
    assert.deepEqual(after('<1>?'), ['#LB'])
    assert.deepEqual(after('<1> ?'), ['#LB'])
    assert.deepEqual(after('<1>>'), ['#LB'])
    assert.deepEqual(after('<1>'), ['#ZZ'])

    const json = (src) => make('json', {}, '-').continuations(src).tokens
    assert.deepEqual(json('[?'), ['#NR', '#ST', '#VL', '#OB', '#OS'])
    assert.deepEqual(json('{"a"?'), ['#NR', '#ST', '#VL', '#OB', '#OS'])
  })
})
