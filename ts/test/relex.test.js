/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Negotiated lexing (`lex.relex`) — see Lex.relex and parse_alts.
//
// The option's contract is narrow and worth pinning precisely: enabling
// it may turn a FAILED alternate into a match, and may do nothing else.
// It must never make a working parse fail, and it must never let
// malformed input through. Both are easy to break, because a recut
// mutates state (the shared token buffer and the lexer point) that
// outlives the alternate that asked for it.
//
// Every case below therefore runs the SAME grammar and input twice,
// with `relex` off and on, and asserts the outcomes agree except where
// renegotiation is the whole point.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')


// A grammar over two overlapping fixed literals: `#LONG` ("ab") wins
// the first cut by longest-match, but an alternate may want the shorter
// `#SHORT` ("a"). Rules are supplied by the caller.
function make(relex, rules) {
  const tn = new Tabnas({
    lex: { relex },
    fixed: {
      token: { '#LONG': 'ab', '#SHORT': 'a', '#TB': 'b', '#TC': 'c' },
    },
  })
  const j = tn.make()
  for (const rn of Object.keys(j.rule())) j.rule(rn, null)
  for (const [name, def] of Object.entries(rules)) j.rule(name, def)
  j.options({ rule: { start: 'top' } })
  return j
}

const run = (j, src) => {
  try {
    j.parse(src)
    return 'PARSED'
  } catch (e) {
    return 'FAILED'
  }
}


describe('relex', () => {

  describe('a failed alternate leaves no trace', () => {

    // The first alternate needs `ab` re-cut to `#SHORT`, then fails at
    // its SECOND position. The fallback alternate must still see the
    // original `#LONG` cut — not the shortened one chosen for an
    // alternate that was rejected.
    const RULES = {
      top: (rs) => rs
        .open([
          { s: ['#SHORT', '#TC'] },   // recuts 'ab' -> 'a', then wants 'c'
          { s: ['#AA'] },             // fallback: takes whatever is there
        ])
        .close([{ s: ['#ZZ'] }]),
    }

    it('does not fail a parse that succeeds without relex', () => {
      assert.equal(run(make(false, RULES), 'ab'), 'PARSED')
      assert.equal(run(make(true, RULES), 'ab'), 'PARSED')
    })


    // Same shape, but the alternate fails in its `c` condition — after
    // every position matched, which is the later of the two undo paths.
    const COND_RULES = {
      top: (rs) => rs
        .open([
          { s: ['#SHORT'], c: () => false },
          { s: ['#AA'] },
        ])
        .close([{ s: ['#ZZ'] }]),
    }

    it('undoes a recut rejected by the alternate condition', () => {
      assert.equal(run(make(false, COND_RULES), 'ab'), 'PARSED')
      assert.equal(run(make(true, COND_RULES), 'ab'), 'PARSED')
    })


    // The stale cut used to leak past the rule that made it: the token
    // buffer is shared, so the NEXT rule inherited it too.
    const NEXT_RULE = {
      top: (rs) => rs
        .open([
          { s: ['#SHORT', '#TC'] },
          { p: 'val' },
        ])
        .close([{ s: ['#ZZ'] }]),
      val: (rs) => rs.open([{ s: ['#AA'] }]),
    }

    it('undoes a recut before the next rule sees the buffer', () => {
      assert.equal(run(make(false, NEXT_RULE), 'ab'), 'PARSED')
      assert.equal(run(make(true, NEXT_RULE), 'ab'), 'PARSED')
    })

  })


  describe('bad tokens', () => {

    // A `#BD` token's throw is deferred under relex so an alternate can
    // try to re-cut the span. That deferral must not become acceptance:
    // a wildcard position compiles to no tin constraint at all, and
    // would otherwise swallow the bad token and parse malformed input.
    function badGrammar(relex) {
      const tn = new Tabnas({ lex: { relex } })
      const j = tn.make()
      for (const rn of Object.keys(j.rule())) j.rule(rn, null)
      j.rule('top', (rs) => rs
        .open([{ s: ['#AA', '#AA'] }])
        .close([{ s: ['#ZZ'] }]))
      j.options({ rule: { start: 'top' } })
      return j
    }

    it('are rejected at a wildcard position, with or without relex', () => {
      // An invalid unicode escape lexes to #BD.
      const src = '"\\uZZZZ"'
      for (const relex of [false, true]) {
        assert.throws(
          () => badGrammar(relex).parse(src),
          /invalid unicode/,
          `relex: ${relex}`)
      }
    })

  })


  describe('what relex is for', () => {

    // The positive case, so the tests above cannot be satisfied by
    // simply disabling renegotiation: an alternate that names only the
    // shorter token DOES get it, which is what the option buys.
    const WANT_SHORT = {
      top: (rs) => rs
        .open([{ s: ['#SHORT', '#TB'] }])
        .close([{ s: ['#ZZ'] }]),
    }

    it('re-cuts a token an alternate asks for by name', () => {
      assert.equal(run(make(false, WANT_SHORT), 'ab'), 'FAILED')
      assert.equal(run(make(true, WANT_SHORT), 'ab'), 'PARSED')
    })


    // Renegotiation reaches custom `lex.match` matchers too. The
    // matcher below is registered under a name the lexer does not know,
    // which is exactly the case that used to be skipped outright.
    it('re-cuts through a custom lex.match matcher', () => {
      const mk = (relex) => {
        const tn = new Tabnas({
          lex: {
            relex,
            match: {
              // Ordered after `fixed` (2e6), so `#TB` wins the first cut
              // and only renegotiation can reach this matcher.
              custom: {
                order: 2.5e6,
                make: () => {
                  const m = (lex) => {
                    const pnt = lex.pnt
                    if ('x' !== lex.src[pnt.sI]) return undefined
                    const tkn = lex.token('#TA', 'x', 'x', pnt)
                    pnt.sI += 1
                    pnt.cI += 1
                    return tkn
                  }
                  m.matcher = 'custom'
                  return m
                },
              },
            },
          },
          fixed: { token: { '#TB': 'x' } },
          tokenSet: { IGNORE: [] },
        })
        const j = tn.make()
        for (const rn of Object.keys(j.rule())) j.rule(rn, null)
        j.rule('top', (rs) => rs
          .open([{ s: ['#TA'] }])
          .close([{ s: ['#ZZ'] }]))
        j.options({ rule: { start: 'top' } })
        return j
      }

      assert.equal(run(mk(false), 'x'), 'FAILED')
      assert.equal(run(mk(true), 'x'), 'PARSED')
    })

  })


  it('is off by default', () => {
    const tn = new Tabnas()
    assert.equal(tn.internal().config.lex.relex, false)
  })

})
