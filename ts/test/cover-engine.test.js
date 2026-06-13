/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Coverage tests for the engine surface: Tabnas instance API
// (parse/config/use/empty/sub/grammar/util), Parser rule management
// and error paths, Context lookahead/history accessors, and RuleSpec
// definition and parse-time alt features.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeRuleSpec, makeToken, makePoint } = require('..')
const { Context } = require('../dist/context')

const am = new Tabnas()

describe('cover-engine', () => {
  describe('tabnas-api', () => {
    it('parse-non-string-returns-input', () => {
      assert.equal(am.parse(123), 123)
      let o = { a: 1 }
      assert.equal(am.parse(o), o)
    })

    it('config-accessor', () => {
      let c = am.config()
      assert.ok(c.fixed)
      assert.ok(c.t)
    })

    it('use-requires-function', () => {
      assert.throws(() => am.use(123), /must be a function/)
    })

    it('empty-instance', () => {
      let e = am.empty()
      assert.equal(e.options.defaults$, false)
      assert.equal(e.options.standard$, false)
      assert.equal(typeof e.token, 'function')

      let e2 = am.empty({ tag: 'em' })
      assert.equal(e2.options.tag, 'em')
    })

    it('toString-is-id', () => {
      assert.equal('' + am, am.id)
      assert.match('' + am, /^Tabnas\//)
    })

    it('util-getter', () => {
      assert.equal(am.util, Tabnas.util)
      assert.equal(typeof am.util.deep, 'function')
    })

    it('token-create-new', () => {
      let j = new Tabnas()
      let tin = j.token('#QQNEW')
      assert.equal(typeof tin, 'number')
      assert.equal(j.token.QQNEW, tin)
      assert.equal(j.token('#QQNEW'), tin)
    })

    it('sub-lex-and-rule', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      j.rule('top', (rs) => rs.open([{ s: [Ta] }]).close([{ s: ['#ZZ'] }]))

      let lexed = []
      let ruled = []
      j.sub({ lex: (tkn) => lexed.push(tkn.name) })
      // Second sub call extends the existing lists.
      j.sub({ rule: (rule) => ruled.push(rule.name) })

      j.parse('a')
      assert.ok(lexed.includes('Ta'))
      assert.ok(ruled.includes('top'))
    })

    it('grammar-object-form-and-alt-groups', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let done = false
      j.grammar(
        {
          options: { tag: '@@literal' },
          ref: { '@noop': () => undefined },
          rule: {
            top: {
              // Object (alts/inject) form rather than plain array.
              open: { alts: [{ s: ['Ta'], g: 'pre' }, null] },
              close: {
                alts: [{ s: ['#ZZ'], a: () => (done = true) }],
              },
            },
          },
        },
        // Group tags injected from settings, as a comma string.
        { rule: { alt: { g: 'gg1, gg2' } } },
      )

      // The '@@' escape resolved to a literal '@'-string option.
      assert.equal(j.options.tag, '@literal')
      // Injected groups merged with the alt's own (string) groups.
      assert.deepEqual(j.rule('top').def.open[0].g, ['gg1', 'gg2', 'pre'])
      j.parse('a')
      assert.equal(done, true)

      // Array form of the alt group setting.
      let j2 = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      j2.grammar(
        { rule: { top: { open: [{ s: ['Ta'] }], close: [{ s: ['#ZZ'] }] } } },
        { rule: { alt: { g: ['arr1'] } } },
      )
      assert.deepEqual(j2.rule('top').def.open[0].g, ['arr1'])
    })
  })

  describe('parser', () => {
    it('rule-get-delete-and-missing-start', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      j.rule('top', (rs) => rs.open([{ s: [Ta] }]).close([{ s: ['#ZZ'] }]))

      // Get by name.
      let rs = j.rule('top')
      assert.equal(rs.name, 'top')

      // Delete by name.
      j.rule('top', null)
      assert.equal(j.rule()['top'], undefined)

      // Parse with no start rule returns undefined.
      assert.equal(j.parse('a'), undefined)
    })

    it('meta-log-function', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      j.rule('top', (rs) => rs.open([{ s: [Ta] }]).close([{ s: ['#ZZ'] }]))
      let calls = 0
      j.parse('a', { log: () => calls++ })
      assert.ok(0 < calls)
    })

    it('trailing-content-throws', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      // Close matches without consuming, leaving trailing source.
      j.rule('top', (rs) => rs.open([{ s: [Ta] }]).close([{}]))
      assert.throws(() => j.parse('a a'), (e) => 'unexpected' === e.code)
    })

    it('result-fail-list', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
        result: { fail: ['BAD'] },
      })
      let Ta = j.token.Ta
      j.rule('top', (rs) =>
        rs
          .open([{ s: [Ta], a: (r) => (r.node = 'BAD') }])
          .close([{ s: ['#ZZ'] }]),
      )
      assert.throws(() => j.parse('a'), (e) => 'unexpected' === e.code)
    })
  })

  describe('context', () => {
    const NOTOKEN = { name: 'no-token' }
    const mkctx = () =>
      new Context({
        opts: {},
        cfg: {},
        meta: {},
        src: () => '',
        root: () => null,
        plgn: () => [],
        inst: () => null,
        sub: {},
        rsm: {},
        F: (x) => x,
        NOTOKEN,
        NORULE: { name: 'no-rule' },
      })

    it('t0-t1-aliases', () => {
      let ctx = mkctx()
      assert.equal(ctx.t0, NOTOKEN)
      assert.equal(ctx.t1, NOTOKEN)
      let a = { name: 'a' }
      let b = { name: 'b' }
      ctx.t0 = a
      ctx.t1 = b
      assert.equal(ctx.t0, a)
      assert.equal(ctx.t1, b)
    })

    it('v1-v2-setters', () => {
      let ctx = mkctx()
      let a = { name: 'a' }
      let b = { name: 'b' }
      let c = { name: 'c' }

      // v1 set on empty stack pushes.
      assert.equal(ctx.v1, NOTOKEN)
      ctx.v1 = a
      assert.deepEqual(ctx.v, [a])
      // v1 set on non-empty stack replaces the top.
      ctx.v1 = b
      assert.deepEqual(ctx.v, [b])

      // v2 set with len>1 replaces the second-from-top.
      ctx.v = [a, b]
      ctx.v2 = c
      assert.deepEqual(ctx.v, [c, b])
      // v2 set with len==1 unshifts.
      ctx.v = [a]
      ctx.v2 = c
      assert.deepEqual(ctx.v, [c, a])
      // v2 set with empty stack pushes.
      ctx.v = []
      ctx.v2 = c
      assert.deepEqual(ctx.v, [c])
      assert.equal(ctx.v2, NOTOKEN)
    })

    it('rewind-preserves-lookahead-buffer', () => {
      let ctx = mkctx()
      let t1 = { name: 't1' }
      let t2 = { name: 't2' }
      let la = { name: 'lookahead' }
      let queue = []
      ctx.lex = { pnt: { token: queue, end: { name: 'end' } } }
      ctx.t = [NOTOKEN, la]
      ctx.v = [t1, t2]
      ctx.vAbs = 2

      ctx.rewind(0)

      // Rewound consumed tokens lead, pre-lexed lookahead follows.
      assert.deepEqual(
        queue.map((t) => t.name),
        ['t1', 't2', 'lookahead'],
      )
      assert.equal(ctx.vAbs, 0)
      assert.equal(ctx.lex.pnt.end, undefined)
      assert.deepEqual(ctx.t, [NOTOKEN, NOTOKEN])
    })
  })

  describe('rules', () => {
    it('rulespec-def-normalisation-and-methods', () => {
      let j = new Tabnas()
      let cfg = j.internal().config
      let boCalls = []
      // Null alts are filtered; action specs in def install handlers.
      let rs = makeRuleSpec(j, cfg, {
        open: [null, { s: '#SP' }],
        close: [null, { s: '#ZZ' }],
        bo: [{ append: true, action: () => boCalls.push('bo') }],
      })
      rs.name = 'x'
      assert.equal(rs.def.open.length, 1)
      assert.equal(rs.def.close.length, 1)
      assert.equal(rs.def.bo.length, 2) // spec object + installed fn

      // tin lookup.
      assert.equal(rs.tin('#SP'), j.token.SP)

      // Action registration in both directions, for all four phases.
      let f = () => {}
      rs.bo(false, f) // unshift
      rs.ao(f)
      rs.bc(f)
      rs.ac(f)
      assert.equal(rs.def.bo[0], f)
      assert.equal(rs.def.ao.length, 1)
      assert.equal(rs.def.bc.length, 1)
      assert.equal(rs.def.ac.length, 1)

      // clear() resets all alt and action lists.
      rs.clear()
      assert.equal(rs.def.open.length, 0)
      assert.equal(rs.def.bo.length, 0)
      assert.equal(rs.def.ac.length, 0)
    })

    it('fnref-prepend-and-dedupe', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      let order = []
      const pre = () => order.push('pre')
      const app = () => order.push('app')

      j.rule('top', (rs) => {
        rs.fnref({ '@top-bo/prepend': pre, '@top-bo/append': app })
        // Re-registering the same functions installs nothing extra.
        rs.fnref({ '@top-bo/prepend': pre, '@top-bo/append': app })
        rs.open([{ s: [Ta] }]).close([{ s: ['#ZZ'] }])
      })

      j.parse('a')
      assert.deepEqual(order, ['pre', 'app'])
    })

    it('rule-counter-comparators-and-toString', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      let seen = null
      j.rule('top', (rs) =>
        rs
          .open([
            {
              s: [Ta],
              n: { x: 1 },
              a: (r) => {
                seen = {
                  eq0: r.eq('x', 0),
                  eq1: r.eq('x', 1),
                  lt: r.lt('x', 2),
                  gt: r.gt('x', 0),
                  lte: r.lte('x', 1),
                  gte: r.gte('x', 1),
                  missing: r.eq('nope'),
                  str: '' + r,
                }
              },
            },
          ])
          .close([{ s: ['#ZZ'] }]),
      )
      j.parse('a')
      assert.equal(seen.eq0, false)
      assert.equal(seen.eq1, true)
      assert.equal(seen.lt, true)
      assert.equal(seen.gt, true)
      assert.equal(seen.lte, true)
      assert.equal(seen.gte, true)
      assert.equal(seen.missing, true)
      assert.match(seen.str, /^\[Rule top~\d+\]$/)
    })

    it('alt-h-handler-and-k-props', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      let hCalled = false
      let kSeen = null
      j.rule('top', (rs) =>
        rs
          .open([
            {
              s: [Ta],
              k: { kk: 1 },
              h: (rule, ctx, alt) => ((hCalled = true), alt),
              a: (r) => (kSeen = r.k.kk),
            },
          ])
          .close([{ s: ['#ZZ'] }]),
      )
      j.parse('a')
      assert.equal(hCalled, true)
      assert.equal(kSeen, 1)
    })

    it('unknown-rule-error', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      j.rule('top', (rs) =>
        rs.open([{ s: [Ta], p: 'no-such-rule' }]).close([{ s: ['#ZZ'] }]),
      )
      assert.throws(() => j.parse('a'), (e) => 'unknown_rule' === e.code)

      // Replace (r) with an unknown rule also errors.
      let j2 = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta2 = j2.token.Ta
      j2.rule('top', (rs) =>
        rs.open([{ s: [Ta2], r: 'no-such-rule' }]).close([{ s: ['#ZZ'] }]),
      )
      assert.throws(() => j2.parse('a'), (e) => 'unknown_rule' === e.code)
    })

    it('fnref-strings-for-h-e-p-r-b', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a', Tb: 'b' } },
      })
      let { Ta, Tb } = j.token
      let used = []
      j.rule('child', (rs) =>
        rs.open([{ s: [Tb], a: (r) => used.push('child') }]),
      )
      j.rule('top', (rs) => {
        rs.fnref({
          '@h': (rule, ctx, alt) => (used.push('h'), alt),
          '@e': () => (used.push('e'), undefined),
        })
        rs.open([
          {
            s: [Ta],
            h: '@h',
            e: '@e',
            // Function forms of p and b.
            p: () => 'child',
            b: () => 0,
          },
        ]).close([{ s: ['#ZZ'] }])
      })
      j.parse('ab')
      assert.deepEqual(used, ['e', 'h', 'child'])

      // Function form of r.
      let j2 = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a', Tb: 'b' } },
      })
      let t2 = j2.token
      let replaced = false
      j2.rule('next', (rs) =>
        rs.open([{ s: [t2.Tb], a: () => (replaced = true) }]),
      )
      j2.rule('top', (rs) =>
        rs.open([{ s: [t2.Ta] }]).close([{ r: () => 'next' }]),
      )
      j2.parse('ab')
      assert.equal(replaced, true)
    })

    it('condition-operators', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      let matched = null
      // d (rule depth) is 0 at the root: the first six alts all fail
      // their conditions, exercising every comparison operator; the
      // final conjunction (two ops on one prop) passes.
      j.rule('top', (rs) =>
        rs
          .open([
            { s: [Ta], c: { d: { $gt: 0 } } },
            { s: [Ta], c: { d: { $ne: 0 } } },
            { s: [Ta], c: { d: { $lt: 0 } } },
            { s: [Ta], c: { d: { $lte: -1 } } },
            { s: [Ta], c: { d: { $gte: 1 } } },
            { s: [Ta], c: { d: 5 } },
            // Conjunction with a failing member short-circuits.
            { s: [Ta], c: { d: { $lte: 0, $gte: 1 } } },
            { s: [Ta], c: { d: { $lt: 1, $gt: -1 } }, a: () => (matched = 'conj') },
          ])
          .close([{ s: ['#ZZ'] }]),
      )
      j.parse('a')
      assert.equal(matched, 'conj')
    })

    it('condition-edge-shapes', () => {
      let j = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta = j.token.Ta
      let hit = null
      j.rule('top', (rs) =>
        rs
          .open([
            // Empty condition object: zero conds, condition removed.
            { s: [Ta], c: {}, a: () => (hit = 'empty') },
          ])
          .close([{ s: ['#ZZ'] }]),
      )
      j.parse('a')
      assert.equal(hit, 'empty')

      // Null prop spec and unknown ops are ignored (zero conds).
      let j2 = new Tabnas({
        rule: { start: 'top' },
        fixed: { token: { Ta: 'a' } },
      })
      let Ta2 = j2.token.Ta
      let hit2 = null
      j2.rule('top', (rs) =>
        rs
          .open([
            {
              s: [Ta2],
              c: { x: null, d: { $weird: 1 } },
              a: () => (hit2 = 'ok'),
            },
          ])
          .close([{ s: ['#ZZ'] }]),
      )
      j2.parse('a')
      assert.equal(hit2, 'ok')

      // Invalid condition type throws at definition time.
      let j3 = new Tabnas()
      assert.throws(
        () => makeRuleSpec(j3, j3.internal().config, { open: [{ c: 5 }] }),
        /invalid condition/,
      )
    })

    it('invalid-group-tag-throws', () => {
      let j = new Tabnas()
      assert.throws(
        () =>
          makeRuleSpec(j, j.internal().config, {
            open: [{ g: 'Bad_Tag' }],
          }),
        /invalid group tag/,
      )
    })
  })
})
