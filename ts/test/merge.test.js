/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Tests for Tabnas.merge: combining two parser instances into a new
// one with deterministically interleaved rule alternates, commutative
// option merging, and tag-prefixed named actions.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')


// Token-name sequences of a rule's open alts, via the instance's own
// tin-to-name map — the visible interleave order.
function openKeys(tn, rulename) {
  return tn.rule(rulename).def.open.map((alt) =>
    (alt.t || [])
      .map((tins) => tins.map((tin) => tn.token(tin)).sort().join(' '))
      .join(', '),
  )
}


// Grammar A of the plan's example: token AT=@, val matches TX AT.
function grammarA(opts) {
  const tn = new Tabnas({
    tag: 'A',
    fixed: { token: { '#AT': '@' } },
    ...(opts || {}),
  })
  tn.rule('val', (rs) =>
    rs.open([
      { s: ['#TX', '#AT'], a: (r) => (r.node = r.o0.val + '@'), g: 'ga' },
    ]),
  )
  return tn
}

// Grammar B of the plan's example: token PC=%, val matches TX PC.
function grammarB(opts) {
  const tn = new Tabnas({
    tag: 'B',
    fixed: { token: { '#PC': '%' } },
    ...(opts || {}),
  })
  tn.rule('val', (rs) =>
    rs.open([
      { s: ['#TX', '#PC'], a: (r) => (r.node = r.o0.val + '%'), g: 'gb' },
    ]),
  )
  return tn
}


describe('merge', () => {

  it('combines-two-grammars-commutatively', () => {
    const a = grammarA()
    const b = grammarB()

    const ab = a.merge(b)
    const ba = b.merge(a)

    // Both merged instances parse both grammars' forms.
    assert.equal(ab.parse('x@'), 'x@')
    assert.equal(ab.parse('y%'), 'y%')
    assert.equal(ba.parse('x@'), 'x@')
    assert.equal(ba.parse('y%'), 'y%')

    // Deterministic interleave: TX AT sorts before TX PC (token name
    // order at the differing position), regardless of merge direction.
    assert.deepEqual(openKeys(ab, 'val'), ['#TX, #AT', '#TX, #PC'])
    assert.deepEqual(openKeys(ba, 'val'), ['#TX, #AT', '#TX, #PC'])

    // Identity is direction-independent too.
    assert.equal(ab.options.tag, 'A~B')
    assert.equal(ba.options.tag, 'A~B')

    // Originals are unmodified.
    assert.equal(a.rule('val').def.open.length, 1)
    assert.equal(b.rule('val').def.open.length, 1)
    assert.equal(a.parse('x@'), 'x@')
    assert.equal(b.parse('y%'), 'y%')
    assert.throws(() => a.parse('y%'))
    assert.throws(() => b.parse('x@'))
    assert.equal(a.options.tag, 'A')
    assert.equal(b.options.tag, 'B')
    assert.equal(a.options.fixed.token['#PC'], undefined)
    assert.equal(b.options.fixed.token['#AT'], undefined)
  })


  it('requires-distinct-tags', () => {
    assert.throws(
      () => new Tabnas().merge(grammarB()),
      /merge: the first instance needs a tag option/,
    )
    assert.throws(
      () => grammarA().merge(new Tabnas()),
      /merge: the second instance needs a tag option/,
    )
    assert.throws(
      () => grammarA().merge(grammarA()),
      /merge: instance tags must differ/,
    )
  })


  it('throws-on-option-conflict', () => {
    const a = grammarA({ rule: { maxmul: 5 } })
    const b = grammarB({ rule: { maxmul: 7 } })
    assert.throws(
      () => a.merge(b),
      /merge: conflicting option values at rule\.maxmul/,
    )
    assert.throws(
      () => b.merge(a),
      /merge: conflicting option values at rule\.maxmul/,
    )

    // Non-default vs default is not a conflict: the non-default value
    // wins in either direction.
    const c = grammarA({ rule: { maxmul: 5 } })
    const d = grammarB()
    assert.equal(c.merge(d).options.rule.maxmul, 5)
    assert.equal(d.merge(c).options.rule.maxmul, 5)
  })


  it('deep-merges-fixed-tokens', () => {
    const ab = grammarA().merge(grammarB())
    assert.equal(ab.options.fixed.token['#AT'], '@')
    assert.equal(ab.options.fixed.token['#PC'], '%')
    assert.equal(ab.token(ab.fixed('@')), '#AT')
    assert.equal(ab.token(ab.fixed('%')), '#PC')
  })


  it('prefixes-named-actions-with-tags', () => {
    let acount = 0
    const a = new Tabnas({ tag: 'A', fixed: { token: { '#AT': '@' } } })
    a.grammar({
      ref: {
        '@doit': (r) => (r.node = r.o0.val + '@'),
        '@val-bo': () => acount++,
      },
      rule: { val: { open: [{ s: ['#TX', '#AT'], a: '@doit' }] } },
    })

    const b = new Tabnas({ tag: 'B', fixed: { token: { '#PC': '%' } } })
    b.grammar({
      ref: { '@doit': (r) => (r.node = r.o0.val + '%') },
      rule: { val: { open: [{ s: ['#TX', '#PC'], a: '@doit' }] } },
    })

    const ab = a.merge(b)
    const fnref = ab.rule('val').def.fnref

    // Each side's refs live under its tag prefix; the bare name and
    // the reserved lifecycle name are gone.
    assert.equal(typeof fnref['@A:doit'], 'function')
    assert.equal(typeof fnref['@B:doit'], 'function')
    assert.equal(fnref['@doit'], undefined)
    assert.equal(typeof fnref['@A:val-bo'], 'function')
    assert.equal(fnref['@val-bo'], undefined)

    // Engine builtins ($-refs) stay unprefixed.
    assert.equal(typeof fnref['@node$'], 'function')

    // The lifecycle handler was carried as an installed action, once.
    assert.equal(ab.rule('val').def.bo.length, 1)
    acount = 0
    assert.equal(ab.parse('x@'), 'x@')
    assert.equal(acount, 1)
  })


  it('sorts-longer-token-prefix-first', () => {
    const a = new Tabnas({ tag: 'A' })
    a.rule('val', (rs) =>
      rs.open([{ s: ['#TX'], a: (r) => (r.node = r.o0.val) }]),
    )
    const b = grammarB()

    for (const m of [a.merge(b), b.merge(a)]) {
      assert.deepEqual(openKeys(m, 'val'), ['#TX, #PC', '#TX'])
      // The longer alt must win the shared TX prefix, or 'y%' would
      // strand the % after the shorter alt matched.
      assert.equal(m.parse('y%'), 'y%')
      assert.equal(m.parse('z'), 'z')
    }
  })


  it('sorts-by-complexity-then-group-tags', () => {
    // Same token sequence on both sides; A's alt carries a condition,
    // so it sorts first — and its false condition falls through to
    // B's unconditioned alt at parse time.
    const a = new Tabnas({ tag: 'A' })
    a.rule('val', (rs) =>
      rs.open([
        { s: ['#TX'], c: () => false, a: (r) => (r.node = 'cond') },
      ]),
    )
    const b = new Tabnas({ tag: 'B' })
    b.rule('val', (rs) =>
      rs.open([{ s: ['#TX'], a: (r) => (r.node = 'plain') }]),
    )

    for (const m of [a.merge(b), b.merge(a)]) {
      const alts = m.rule('val').def.open
      assert.equal(typeof alts[0].c, 'function')
      assert.equal(alts[1].c, null)
      assert.equal(m.parse('x'), 'plain')
    }

    // Equal complexity: group tags decide.
    const c = new Tabnas({ tag: 'A' })
    c.rule('val', (rs) =>
      rs.open([{ s: ['#TX'], g: 'zz', a: (r) => (r.node = 'zz') }]),
    )
    const d = new Tabnas({ tag: 'B' })
    d.rule('val', (rs) =>
      rs.open([{ s: ['#TX'], g: 'aa', a: (r) => (r.node = 'aa') }]),
    )
    for (const m of [c.merge(d), d.merge(c)]) {
      assert.deepEqual(
        m.rule('val').def.open.map((alt) => alt.g.join(',')),
        ['aa', 'zz'],
      )
      assert.equal(m.parse('x'), 'aa')
    }
  })


  it('copies-disjoint-rules', () => {
    const a = grammarA()
    a.rule('extra', (rs) => rs.open([{ s: ['#AT'] }]))
    const b = grammarB()
    b.rule('other', (rs) => rs.open([{ s: ['#PC'] }]))

    for (const m of [a.merge(b), b.merge(a)]) {
      const rules = Object.keys(m.rule()).sort()
      assert.ok(rules.includes('extra'))
      assert.ok(rules.includes('other'))
      assert.ok(rules.includes('val'))
      assert.equal(m.rule('extra').def.open.length, 1)
      assert.equal(m.rule('other').def.open.length, 1)
    }
  })


  it('sorts-empty-sequence-alts-last', () => {
    const a = new Tabnas({ tag: 'A' })
    a.rule('val', (rs) =>
      rs.open([{ a: (r) => (r.node = 'any') }]),
    )
    const b = new Tabnas({ tag: 'B' })
    b.rule('val', (rs) =>
      rs.open([{ s: ['#TX'], a: (r) => (r.node = 'text') }]),
    )

    for (const m of [a.merge(b), b.merge(a)]) {
      const alts = m.rule('val').def.open
      assert.equal(alts.length, 2)
      assert.equal(alts[0].sN, 1)
      assert.equal(alts[1].sN, 0)
      assert.equal(m.parse('x'), 'text')
    }
  })


  it('throws-on-fixed-token-source-collision', () => {
    const a = grammarA()
    const b = new Tabnas({ tag: 'B', fixed: { token: { '#BT': '@' } } })
    b.rule('val', (rs) => rs.open([{ s: ['#TX', '#BT'] }]))
    assert.throws(() => a.merge(b), /both claim source "@"/)
    assert.throws(() => b.merge(a), /both claim source "@"/)
  })


  it('derives-children-of-merged-instances', () => {
    const ab = grammarA().merge(grammarB())
    const child = ab.make({})
    assert.equal(child.parse('x@'), 'x@')
    assert.equal(child.parse('y%'), 'y%')
    assert.deepEqual(openKeys(child, 'val'), ['#TX, #AT', '#TX, #PC'])
  })


  it('dedupes-shared-ancestor-alts', () => {
    // Both instances install the same base plugin. Each use() run
    // creates fresh closures, so the alt actions are source-equal but
    // not reference-equal — the merged rule still carries one copy,
    // and the plugin's lifecycle handler installs once.
    let count = 0
    function base(tn) {
      tn.rule('val', (rs) => {
        rs.bo(() => count++)
        rs.open([{ s: ['#TX'], a: (r) => (r.node = r.o0.val) }])
      })
    }
    const a = new Tabnas({ tag: 'A' })
    a.use(base)
    const b = new Tabnas({ tag: 'B' })
    b.use(base)

    for (const m of [a.merge(b), b.merge(a)]) {
      assert.equal(m.rule('val').def.open.length, 1)
      assert.equal(m.rule('val').def.bo.length, 1)
      count = 0
      assert.equal(m.parse('x'), 'x')
      assert.equal(count, 1)
    }

    // Conditioned alts never dedupe across references: a condition
    // closure may behave differently via its captured environment, so
    // both copies are kept and the false condition falls through.
    function closing(allow) {
      return function grammar(tn) {
        tn.rule('val', (rs) =>
          rs.open([
            {
              s: ['#TX'],
              c: () => allow,
              a: (r) => (r.node = 'allow=' + allow),
            },
          ]),
        )
      }
    }
    const c = new Tabnas({ tag: 'A' })
    c.use(closing(false))
    const d = new Tabnas({ tag: 'B' })
    d.use(closing(true))
    const m = c.merge(d)
    assert.equal(m.rule('val').def.open.length, 2)
    // A pointer- or source-based dedupe would have wrongly dropped the
    // env-differing twin this parse depends on.
    assert.equal(m.parse('x'), 'allow=true')
  })


  it('merges-extensions-of-a-shared-grammar-plugin', () => {
    // The realistic composition case: both instances install the
    // strict-JSON plugin and each extends `val` with its own token.
    // The base grammar's alts and lifecycle handlers must appear once
    // in the merge — un-deduped element handlers would append every
    // list element twice.
    const a = new Tabnas({ tag: 'A', fixed: { token: { '#AT': '@' } } })
    a.use(json)
    a.rule('val', (rs) =>
      rs.open([{ s: ['#AT'], a: (r) => (r.node = 'AT!'), g: 'json' }]),
    )

    const b = new Tabnas({ tag: 'B', fixed: { token: { '#PC': '%' } } })
    b.use(json)
    b.rule('val', (rs) =>
      rs.open([{ s: ['#PC'], a: (r) => (r.node = 'PC!'), g: 'json' }]),
    )

    const ab = a.merge(b)
    const ba = b.merge(a)
    for (const m of [ab, ba]) {
      assert.deepEqual(m.parse('{"a":[1,true,null]}'), {
        a: [1, true, null],
      })
      assert.equal(m.parse('@'), 'AT!')
      assert.equal(m.parse('%'), 'PC!')
    }
    assert.deepEqual(openKeys(ab, 'val'), openKeys(ba, 'val'))
    // A's 4 open alts (json base 3 + #AT) plus B's #PC; the base alts
    // deduped even though each side's extension shifted their position.
    assert.equal(
      ab.rule('val').def.open.length,
      a.rule('val').def.open.length + 1,
    )
  })


  it('interleaves-lex-matchers-deterministically', () => {
    // Custom tokens lexed by each side's match.token matcher both
    // work in the merged instance.
    const a = new Tabnas({
      tag: 'A',
      match: { token: { '#QQ': /^!+/ } },
    })
    a.rule('val', (rs) =>
      rs.open([{ s: ['#QQ'], a: (r) => (r.node = 'bang') }]),
    )
    const b = new Tabnas({
      tag: 'B',
      match: { token: { '#WW': /^\?+/ } },
    })
    b.rule('val', (rs) =>
      rs.open([{ s: ['#WW'], a: (r) => (r.node = 'quest') }]),
    )
    for (const m of [a.merge(b), b.merge(a)]) {
      assert.equal(m.parse('!!'), 'bang')
      assert.equal(m.parse('??'), 'quest')
    }

    // Registry entries with tied order values run in name order, in
    // both merge directions (never-matching probes; order only).
    // Distinct probe objects per entry — configure() stamps the name
    // onto the matcher function itself.
    const probe = () => undefined
    const probeMa = () => undefined
    const probeMb = () => undefined
    const c = new Tabnas({
      tag: 'A',
      lex: { match: { mb: { order: 1.5e6, make: () => probeMb } } },
    })
    const d = new Tabnas({
      tag: 'B',
      lex: { match: { ma: { order: 1.5e6, make: () => probeMa } } },
    })
    for (const m of [c.merge(d), d.merge(c)]) {
      const names = m
        .config()
        .lex.match.map((mm) => mm.matcher)
        .filter((n) => 'ma' === n || 'mb' === n)
      assert.deepEqual(names, ['ma', 'mb'])
    }

    // Same matcher name with a different factory is a conflict.
    const e = new Tabnas({
      tag: 'A',
      lex: { match: { same: { order: 1.5e6, make: () => probe } } },
    })
    const f = new Tabnas({
      tag: 'B',
      lex: { match: { same: { order: 1.5e6, make: () => probe } } },
    })
    assert.throws(
      () => e.merge(f),
      /merge: conflicting option values at lex\.match\.same\.make/,
    )

    // The identical shared entry (same references) merges to one.
    const mkshared = () => probe
    const g = new Tabnas({
      tag: 'A',
      lex: { match: { same: { order: 1.5e6, make: mkshared } } },
    })
    const h = new Tabnas({
      tag: 'B',
      lex: { match: { same: { order: 1.5e6, make: mkshared } } },
    })
    const gh = g.merge(h)
    assert.equal(
      gh.config().lex.match.filter((mm) => 'same' === mm.matcher).length,
      1,
    )
  })

})


// Four small real-world grammars — emails, urls, file paths, semvers —
// each lexing its form with a match token and building a structured
// map in its own pushed rule. Merging any 2, 3, or 4 of them must
// parse the same inputs to the same structured results regardless of
// merge order.
describe('merge-permutations', () => {

  // Shared by all four grammars (same reference, so merges dedupe it):
  // hoist the child rule's structured map into val.
  const hoist = (r) => {
    if (undefined === r.node) {
      r.node = r.child.node
    }
  }

  function makeEmailGrammar() {
    const tn = new Tabnas({
      tag: 'email',
      match: {
        token: { '#EM': /^[a-z][a-z0-9._-]*@[a-z0-9.-]+\.[a-z]{2,}/ },
      },
    })
    tn.rule('val', (rs) => {
      rs.bc(hoist)
      rs.open([{ s: ['#EM'], b: 1, p: 'email' }])
    })
    tn.rule('email', (rs) => {
      rs.bo((r) => (r.node = { kind: 'email' }))
      rs.open([
        {
          s: ['#EM'],
          a: (r) => {
            const [user, domain] = r.o0.src.split('@')
            r.node.user = user
            r.node.domain = domain
          },
        },
      ])
    })
    return tn
  }

  function makeUrlGrammar() {
    const tn = new Tabnas({
      tag: 'url',
      match: { token: { '#UR': /^[a-z][a-z0-9+.-]*:\/\/[^\s]+/ } },
    })
    tn.rule('val', (rs) => {
      rs.bc(hoist)
      rs.open([{ s: ['#UR'], b: 1, p: 'url' }])
    })
    tn.rule('url', (rs) => {
      rs.bo((r) => (r.node = { kind: 'url' }))
      rs.open([
        {
          s: ['#UR'],
          a: (r) => {
            const m = r.o0.src.match(
              /^([a-z][a-z0-9+.-]*):\/\/([^/\s]+)(\/[^\s]*)?/,
            )
            r.node.protocol = m[1]
            r.node.host = m[2]
            r.node.path = m[3] || '/'
          },
        },
      ])
    })
    return tn
  }

  function makePathGrammar() {
    const tn = new Tabnas({
      tag: 'path',
      match: {
        token: { '#FP': /^\/[a-zA-Z0-9._-]+(?:\/[a-zA-Z0-9._-]+)*/ },
      },
    })
    tn.rule('val', (rs) => {
      rs.bc(hoist)
      rs.open([{ s: ['#FP'], b: 1, p: 'path' }])
    })
    tn.rule('path', (rs) => {
      rs.bo((r) => (r.node = { kind: 'path' }))
      rs.open([
        {
          s: ['#FP'],
          a: (r) => {
            const segs = r.o0.src.split('/').filter((s) => s.length)
            r.node.base = segs[segs.length - 1]
            r.node.dir = '/' + segs.slice(0, -1).join('/')
          },
        },
      ])
    })
    return tn
  }

  function makeSemverGrammar() {
    const tn = new Tabnas({
      tag: 'semver',
      match: { token: { '#SV': /^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?/ } },
    })
    tn.rule('val', (rs) => {
      rs.bc(hoist)
      rs.open([{ s: ['#SV'], b: 1, p: 'semver' }])
    })
    tn.rule('semver', (rs) => {
      rs.bo((r) => (r.node = { kind: 'semver' }))
      rs.open([
        {
          s: ['#SV'],
          a: (r) => {
            const m = r.o0.src.match(/^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$/)
            r.node.major = +m[1]
            r.node.minor = +m[2]
            r.node.patch = +m[3]
            r.node.prerelease = m[4] || ''
          },
        },
      ])
    })
    return tn
  }

  const INPUT = {
    email: 'alice@example.com',
    url: 'https://example.com/a/b',
    path: '/usr/local/bin/node',
    semver: '1.2.3-beta.1',
  }

  const EXPECTED = {
    email: { kind: 'email', user: 'alice', domain: 'example.com' },
    url: {
      kind: 'url',
      protocol: 'https',
      host: 'example.com',
      path: '/a/b',
    },
    path: { kind: 'path', base: 'node', dir: '/usr/local/bin' },
    semver: {
      kind: 'semver',
      major: 1,
      minor: 2,
      patch: 3,
      prerelease: 'beta.1',
    },
  }

  function permutations(items) {
    if (items.length <= 1) {
      return [items]
    }
    const out = []
    for (let i = 0; i < items.length; i++) {
      const rest = [...items.slice(0, i), ...items.slice(i + 1)]
      for (const p of permutations(rest)) {
        out.push([items[i], ...p])
      }
    }
    return out
  }

  function subsets(items, k) {
    if (0 === k) {
      return [[]]
    }
    if (items.length < k) {
      return []
    }
    const [head, ...rest] = items
    return [
      ...subsets(rest, k - 1).map((s) => [head, ...s]),
      ...subsets(rest, k),
    ]
  }


  it('all-merge-orders-parse-identically', () => {
    const g = {
      email: makeEmailGrammar(),
      url: makeUrlGrammar(),
      path: makePathGrammar(),
      semver: makeSemverGrammar(),
    }
    const names = Object.keys(g)

    // Each singleton parses its own input.
    for (const n of names) {
      assert.deepStrictEqual(g[n].parse(INPUT[n]), EXPECTED[n])
    }

    let permCount = 0
    for (let k = 2; k <= names.length; k++) {
      for (const subset of subsets(names, k)) {
        let refKeys = null
        for (const perm of permutations(subset)) {
          permCount++
          // Chained merge in this permutation's order. Merge never
          // modifies its operands, so the singletons are reusable
          // across all 60 permutations.
          const merged = perm
            .map((n) => g[n])
            .reduce((m, tn) => m.merge(tn))

          // Every grammar in the subset parses to the same structured
          // map as its singleton, whatever the merge order was.
          for (const n of subset) {
            assert.deepStrictEqual(
              merged.parse(INPUT[n]),
              EXPECTED[n],
              `parse ${n} via merge order [${perm}]`,
            )
          }

          // Inputs from grammars outside the subset are rejected.
          for (const n of names.filter((x) => !subset.includes(x))) {
            assert.throws(
              () => merged.parse(INPUT[n]),
              undefined,
              `input ${n} should not parse via [${perm}]`,
            )
          }

          // The interleaved alt order is identical for every merge
          // order of the same grammar subset.
          const keys = JSON.stringify(openKeys(merged, 'val'))
          if (null === refKeys) {
            refKeys = keys
          } else {
            assert.equal(keys, refKeys, `alt order differs for [${perm}]`)
          }
        }
      }
    }

    // P(4,2) + P(4,3) + P(4,4) = 12 + 24 + 24.
    assert.equal(permCount, 60)

    // The singletons are still intact after 60 merges.
    for (const n of names) {
      assert.deepStrictEqual(g[n].parse(INPUT[n]), EXPECTED[n])
      assert.equal(g[n].rule('val').def.open.length, 1)
    }
  })

})
