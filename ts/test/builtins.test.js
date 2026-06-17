/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Engine-owned tests for the `$`-builtin stdlib, array-`a` action
// composition, the `@~/` eager RegExp sentinel, the `$`-namespace
// reservation, and the builtin config-schema version gate. These do NOT
// depend on @tabnas/bnf — the engine is self-tested against hand-written
// function-free specs and direct builtin invocation.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, BUILTIN_REFS, BUILTIN_SCHEMA_VERSION } = require('..')
const builtinsSubpath = require('../dist/builtins')
const { resolveFuncRefs } = require('../dist/utility')

describe('builtins', () => {

  describe('library', () => {
    it('exports the expected frozen ref set', () => {
      assert.deepEqual(
        Object.keys(BUILTIN_REFS).sort(),
        ['@array$', '@bubble$', '@capture$', '@key$', '@node$', '@object$',
          '@probeDecide$', '@probeInit$', '@probePhase0$', '@probePhase1$',
          '@probePhase2$', '@push$', '@reset$', '@setval$', '@value$'])
      assert.equal(Object.isFrozen(BUILTIN_REFS), true)
    })

    it('is reachable from the main entry and the ./builtins subpath', () => {
      assert.equal(builtinsSubpath.BUILTIN_REFS, BUILTIN_REFS)
      assert.equal(typeof BUILTIN_SCHEMA_VERSION, 'number')
      assert.equal(builtinsSubpath.BUILTIN_SCHEMA_VERSION, BUILTIN_SCHEMA_VERSION)
    })
  })

  describe('tree builtins (function-free grammar)', () => {
    // top builds a user node, descends into `lit`, and captures it.
    function treeGrammar(topAction) {
      const j = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
      j.grammar({
        rule: {
          top: {
            open: [{ p: 'lit', a: '@node$',
              k: { node$: { init: true, rule: 'top', kind: 'user', nterms: 0 } } }],
            close: [topAction],
          },
          lit: {
            open: [{ s: ['Ta'], a: '@node$',
              k: { node$: { init: true, rule: 'lit', kind: 'user', nterms: 1 } } }],
            close: [{}],
          },
        },
      })
      return j
    }

    it('@node$ allocates and accumulates; @capture$ merges a tagged child', () => {
      const j = treeGrammar({ a: '@capture$', k: { capture$: { rule: 'top', kind: 'user' } } })
      assert.deepEqual(j.parse('a'),
        { rule: 'top', src: 'a', kids: [{ rule: 'lit', src: 'a', kids: [] }] })
    })

    it('@bubble$ lifts the child node without merging', () => {
      const j = new Tabnas({ rule: { start: 'top' }, fixed: { token: { Ta: 'a' } } })
      j.grammar({
        rule: {
          top: { open: [{ p: 'lit' }], close: [{ a: '@bubble$' }] },
          lit: {
            open: [{ s: ['Ta'], a: '@node$',
              k: { node$: { init: true, rule: 'lit', kind: 'user', nterms: 1 } } }],
            close: [{}],
          },
        },
      })
      assert.deepEqual(j.parse('a'), { rule: 'lit', src: 'a', kids: [] })
    })
  })

  describe('tree builtins (direct invocation of merge edge cases)', () => {
    const node$ = BUILTIN_REFS['@node$']
    const capture$ = BUILTIN_REFS['@capture$']
    const bubble$ = BUILTIN_REFS['@bubble$']

    it('@node$ inits and accumulates nterms src', () => {
      const r = { node: null, o: [{ src: 'x' }, { src: 'y' }, { src: 'z' }] }
      node$(r, null, { k: { node$: { init: true, rule: 'r', kind: 'user', nterms: 2 } } })
      assert.deepEqual(r.node, { rule: 'r', src: 'xy', kids: [] })
    })

    it('@capture$ flattens an untagged child (src + kids), pushes a tagged one', () => {
      // Untagged child: src concatenates, kids extend.
      const untagged = { node: { src: 'p', kids: [{ rule: 'g', src: '', kids: [] }] },
        child: { node: { src: 'q', kids: [{ rule: 'h', src: '', kids: [] }] } } }
      capture$(untagged, null, { k: {} })
      assert.deepEqual(untagged.node,
        { src: 'pq', kids: [{ rule: 'g', src: '', kids: [] }, { rule: 'h', src: '', kids: [] }] })

      // Tagged child: pushed whole into kids.
      const tagged = { node: { src: '', kids: [] },
        child: { node: { rule: 'k', src: 'z', kids: [] } } }
      capture$(tagged, null, { k: {} })
      assert.deepEqual(tagged.node, { src: 'z', kids: [{ rule: 'k', src: 'z', kids: [] }] })
    })

    it('@capture$ pushes a non-node child value and is a no-op with no child', () => {
      const prim = { node: { src: '', kids: [] }, child: { node: 42 } }
      capture$(prim, null, { k: {} })
      assert.deepEqual(prim.node, { src: '', kids: [42] })

      const none = { node: { src: 's', kids: [] }, child: { node: null } }
      capture$(none, null, { k: {} })
      assert.deepEqual(none.node, { src: 's', kids: [] })
    })

    it('@node$ accumulates onto an existing node when init is falsy', () => {
      const r = { node: { rule: 'r', src: 'pre', kids: [] }, o: [{ src: 'A' }, { src: 'B' }] }
      node$(r, null, { k: { node$: { nterms: 2 } } })
      assert.deepEqual(r.node, { rule: 'r', src: 'preAB', kids: [] })
    })

    it('@capture$ self-reference (child.node === r.node) is a no-op', () => {
      const n = { src: 'S', kids: [{ rule: 'x', src: '', kids: [] }] }
      capture$({ node: n, child: { node: n } }, null, { k: {} })
      assert.deepEqual(n, { src: 'S', kids: [{ rule: 'x', src: '', kids: [] }] })
    })

    it('@bubble$ lifts r.child.node onto r.node', () => {
      const r = { node: null, child: { node: { rule: 'c', src: 'v', kids: [] } } }
      bubble$(r)
      assert.deepEqual(r.node, { rule: 'c', src: 'v', kids: [] })
    })

    it('@bubble$ leaves r.node untouched when there is no child node', () => {
      const keep = { rule: 'orig', src: '', kids: [] }
      const noChild = { node: keep, child: { node: undefined } }
      bubble$(noChild)
      assert.equal(noChild.node, keep)
      // A null child node DOES lift (null !== undefined) — pin the choice.
      const nullChild = { node: keep, child: { node: null } }
      bubble$(nullChild)
      assert.equal(nullChild.node, null)
    })
  })

  describe('probe builtins (direct invocation)', () => {
    const probeInit$ = BUILTIN_REFS['@probeInit$']
    const probeDecide$ = BUILTIN_REFS['@probeDecide$']
    const p0 = BUILTIN_REFS['@probePhase0$']
    const p1 = BUILTIN_REFS['@probePhase1$']
    const p2 = BUILTIN_REFS['@probePhase2$']

    it('@probeInit$ resets phase to 0 and records the mark', () => {
      const r = { k: {} }
      probeInit$(r, { mark: () => 7 })
      assert.equal(r.k.pd_phase, 0)
      assert.equal(r.k.pd_mark, 7)
    })

    it('@probeDecide$ rewinds and picks phase 1 when the disambiguator is present', () => {
      let rewound = null
      const r = { k: { pd_d: '#D', pd_mark: 7 } }
      probeDecide$(r, { t: [{ name: '#D' }], rewind: (m) => { rewound = m } })
      assert.equal(rewound, 7)
      assert.equal(r.k.pd_phase, 1)
    })

    it('@probeDecide$ picks phase 2 when the disambiguator is absent', () => {
      const r = { k: { pd_d: '#D', pd_mark: 3 } }
      probeDecide$(r, { t: [{ name: '#OTHER' }], rewind: () => { } })
      assert.equal(r.k.pd_phase, 2)
    })

    it('@probePhaseN$ guards match their phase only', () => {
      assert.equal(p0({ k: { pd_phase: 0 } }), true)
      assert.equal(p1({ k: { pd_phase: 1 } }), true)
      assert.equal(p2({ k: { pd_phase: 2 } }), true)
      assert.equal(p0({ k: { pd_phase: 1 } }), false)
      assert.equal(p1({ k: { pd_phase: 2 } }), false)
      assert.equal(p2({ k: { pd_phase: 0 } }), false)
    })
  })

  describe('array-`a` action composition', () => {
    function run(actions, refs) {
      const order = []
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      const ref = {}
      for (const [name, fn] of Object.entries(refs)) ref[name] = (...x) => fn(order, ...x)
      j.grammar({ ref, rule: { t: { open: [{ s: ['Ta'], a: actions }], close: [{ s: ['#ZZ'] }] } } })
      try { j.parse('a') } catch (e) { /* error-token actions abort the parse */ }
      return order
    }

    it('runs each action in array order', () => {
      assert.deepEqual(
        run(['@one', '@two', '@three'],
          { '@one': (o) => o.push(1), '@two': (o) => o.push(2), '@three': (o) => o.push(3) }),
        [1, 2, 3])
    })

    it('short-circuits on an error-token return', () => {
      assert.deepEqual(
        run(['@err', '@after'],
          { '@err': (o) => { o.push('err'); return { isToken: true, err: true } },
            '@after': (o) => o.push('after') }),
        ['err'])
    })

    it('runs a mix of inline functions and ref strings in order', () => {
      const order = []
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      j.grammar({
        ref: { '@mid': () => order.push('mid') },
        rule: {
          t: {
            open: [{ s: ['Ta'], a: [() => order.push('fn'), '@mid', () => order.push('fn2')] }],
            close: [{ s: ['#ZZ'] }],
          },
        },
      })
      j.parse('a')
      assert.deepEqual(order, ['fn', 'mid', 'fn2'])
    })

    it('treats an empty action array as no action (no node built)', () => {
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      j.grammar({ rule: { t: { open: [{ s: ['Ta'], a: [] }], close: [{ s: ['#ZZ'] }] } } })
      assert.equal(j.parse('a'), undefined)
    })

    it('throws on an unknown ref inside the array', () => {
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      assert.throws(
        () => j.grammar({ rule: { t: { open: [{ s: ['Ta'], a: ['@nope'] }], close: [{ s: ['#ZZ'] }] } } }),
        /unknown action function reference: @nope/)
    })
  })

  describe('@~/ eager RegExp sentinel', () => {
    it('resolves `@~/src/flags` to a RegExp flagged eager$', () => {
      const re = resolveFuncRefs('@~/HI/i')
      assert.ok(re instanceof RegExp)
      assert.equal(re.source, 'HI')
      assert.equal(re.flags, 'i')
      assert.equal(re.eager$, true)
    })

    it('leaves the plain `@/src/flags` form non-eager', () => {
      const re = resolveFuncRefs('@/HI/i')
      assert.ok(re instanceof RegExp)
      assert.equal(re.eager$, undefined)
    })
  })

  describe('$-namespace reservation', () => {
    it('refuses a user ref key containing `$` (trailing or interior)', () => {
      const rule = { t: { open: [{ s: ['Ta'] }], close: [{ s: ['#ZZ'] }] } }
      for (const key of ['@bad$', '@a$b', '@node$']) {
        const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
        assert.throws(() => j.grammar({ ref: { [key]: () => { } }, rule }),
          /'\$' is reserved for engine builtins/, `key ${key} should be rejected`)
      }
    })

    it('allows the spec to override a builtin (spec wins) without `$` in the key — n/a, but builtins still resolve', () => {
      // A function-free grammar referencing a builtin loads without a ref map.
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      assert.doesNotThrow(() => j.grammar({
        rule: { t: { open: [{ s: ['Ta'], a: '@bubble$' }], close: [{ s: ['#ZZ'] }] } },
      }))
    })
  })

  describe('builtin config-schema version gate', () => {
    it('loads a grammar whose v is within the supported schema', () => {
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      assert.doesNotThrow(() => j.grammar({
        v: BUILTIN_SCHEMA_VERSION,
        rule: { t: { open: [{ s: ['Ta'] }], close: [{ s: ['#ZZ'] }] } },
      }))
    })

    it('refuses a grammar requiring a newer schema', () => {
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      assert.throws(
        () => j.grammar({ v: BUILTIN_SCHEMA_VERSION + 1,
          rule: { t: { open: [{ s: ['Ta'] }], close: [{ s: ['#ZZ'] }] } } }),
        /requires builtin schema version/)
    })

    it('refuses a malformed `v` (non-number, NaN, non-positive, non-integer)', () => {
      const mk = () => new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      const rule = { t: { open: [{ s: ['Ta'] }], close: [{ s: ['#ZZ'] }] } }
      for (const bad of ['2', NaN, 0, -1, 1.5, {}]) {
        assert.throws(() => mk().grammar({ v: bad, rule }),
          /invalid builtin schema version/, `v=${String(bad)} should be rejected`)
      }
    })
  })

  describe('always-merge / always-fnref', () => {
    it('applies a no-ref grammar (same instance) and is idempotent on re-application', () => {
      const j = new Tabnas({ rule: { start: 't' }, fixed: { token: { Ta: 'a' } } })
      const spec = { rule: { t: { open: [{ s: ['Ta'] }], close: [{ s: ['#ZZ'] }] } } }
      assert.doesNotThrow(() => { j.grammar(spec); j.grammar(spec) })
      assert.doesNotThrow(() => j.parse('a'))
    })
  })

  // End-to-end: load REAL serialized, function-free grammars (captured
  // from @tabnas/bnf, but loaded here as static JSON so the engine stays
  // self-tested) and drive the probe + eager paths through the real
  // ctx.mark/ctx.rewind and lexer machinery — not hand-stubbed.
  describe('serialized grammar round-trip (function-free fixtures)', () => {
    // Provenance: bnfCompile('top = [ X "@" ] Y\nX = 1*ALPHA\nY = 1*ALPHA',
    // {strict:true}) — the canonical optional-prefix `[X D] Y` ambiguity,
    // recognition mode. Carries @probeInit$/@probeDecide$/@probePhase* and
    // an empty ref map.
    const probeSpec = require('./probe-grammar.fixture.json')
    // Provenance: bnfCompile('g = "hi"', {strict:true}) — a case-
    // insensitive literal, which serializes its match token as the eager
    // sentinel `@~/^hi/i`.
    const eagerSpec = require('./eager-literal.fixture.json')
    const clone = (o) => JSON.parse(JSON.stringify(o))

    it('the probe fixture is pure data (no closures) and carries the probe builtins', () => {
      assert.deepEqual(probeSpec.ref || {}, {})
      const s = JSON.stringify(probeSpec)
      assert.ok(s.includes('@probeInit$') && s.includes('@probeDecide$') && s.includes('@probePhase'))
    })

    it('probe builtins drive the [X D] Y phase decision end-to-end', () => {
      const accepts = (input) => {
        const j = new Tabnas()
        j.grammar(clone(probeSpec))
        try { j.parse(input); return true } catch (e) { return false }
      }
      // disambiguator present (X "@" Y) and absent (Y) both accept...
      assert.equal(accepts('abc'), true)
      assert.equal(accepts('ab@cd'), true)
      // ...but a dangling disambiguator with no following Y is rejected,
      // proving the phase decision actually gates the parse.
      assert.equal(accepts('@'), false)
      assert.equal(accepts('ab@'), false)
    })

    it('the eager sentinel makes a case-insensitive literal recognize end-to-end', () => {
      const s = JSON.stringify(eagerSpec)
      assert.ok(s.includes('@~/'), 'fixture should carry an @~/ eager match token')
      const accepts = (input) => {
        const j = new Tabnas()
        j.grammar(clone(eagerSpec))
        try { j.parse(input); return true } catch (e) { return false }
      }
      // Eager lexing fires the literal regardless of case.
      assert.equal(accepts('hi'), true)
      assert.equal(accepts('HI'), true)
      assert.equal(accepts('Hi'), true)
      // Non-matches still rejected.
      assert.equal(accepts('ho'), false)
      assert.equal(accepts('h'), false)
    })

    it('the native-value builders build values byte-identical to JSON.parse', () => {
      // A function-free json-core grammar wired to @object$/@array$/@key$/
      // @setval$/@push$/@value$/@reset$. JSON.parse is the oracle; pinning
      // both to it pins the engine to it.
      const jsonSpec = require('./json-builder.fixture.json')
      assert.deepEqual(jsonSpec.ref || {}, {})
      assert.ok(!JSON.stringify(jsonSpec).includes('Ref'), 'v1 plain-node contract: no MapRef/ListRef')
      const build = (input) => {
        const j = new Tabnas({ rule: { start: 'val' } })
        j.grammar(clone(jsonSpec))
        return j.parse(input)
      }
      for (const input of ['1', '"x"', 'true', 'false', 'null', '{}', '[]',
        '{"a":1}', '[1,2,3]', '{"a":{"b":[true,null,"x"]}}', '{"a":1,"b":2}']) {
        assert.deepEqual(build(input), JSON.parse(input), `build(${input})`)
      }
    })
  })

  describe('native-value builders (direct invocation)', () => {
    const object$ = BUILTIN_REFS['@object$']
    const array$ = BUILTIN_REFS['@array$']
    const reset$ = BUILTIN_REFS['@reset$']
    const key$ = BUILTIN_REFS['@key$']
    const setval$ = BUILTIN_REFS['@setval$']
    const push$ = BUILTIN_REFS['@push$']
    const value$ = BUILTIN_REFS['@value$']

    it('@object$ / @array$ / @reset$ set the node', () => {
      const ro = { node: 'seed' }; object$(ro)
      assert.equal(typeof ro.node, 'object'); assert.deepEqual(ro.node, {})
      const ra = { node: 'seed' }; array$(ra)
      assert.ok(Array.isArray(ra.node)); assert.equal(ra.node.length, 0)
      const rr = { node: { a: 1 } }; reset$(rr)
      assert.equal(rr.node, undefined)
    })

    it('@key$ captures the matched token value into r.u.key', () => {
      const r = { u: {}, o: [{ val: 'name' }] }
      key$(r, null, { k: {} })
      assert.equal(r.u.key, 'name')
      // custom slot/from.
      const r2 = { u: {}, o: [{ val: 'x' }, { val: 'y' }] }
      key$(r2, null, { k: { key$: { slot: 'k2', from: 1 } } })
      assert.equal(r2.u.k2, 'y')
    })

    it('@setval$ assigns child node under the captured key', () => {
      const r = { node: {}, u: { key: 'a' }, child: { node: 42 } }
      setval$(r, null, { k: {} })
      assert.deepEqual(r.node, { a: 42 })
      // no-op when node is not an object.
      const r2 = { node: 7, u: { key: 'a' }, child: { node: 1 } }
      assert.doesNotThrow(() => setval$(r2, null, { k: {} }))
      assert.equal(r2.node, 7)
    })

    it('@push$ appends the child node (skips the no-value child)', () => {
      const r = { node: [1], child: { node: 2 } }
      push$(r); assert.deepEqual(r.node, [1, 2])
      const r2 = { node: [1], child: { node: undefined } }
      push$(r2); assert.deepEqual(r2.node, [1])
    })

    it('@value$ prefers the child node, else resolves the scalar token', () => {
      const childWins = { node: 'old', child: { node: { built: true } }, o: [{ resolveVal: () => 'scalar' }] }
      value$(childWins, {}, { k: {} })
      assert.deepEqual(childWins.node, { built: true })
      const scalar = { node: 'old', child: { node: undefined }, o: [{ resolveVal: () => 99 }] }
      value$(scalar, {}, { k: {} })
      assert.equal(scalar.node, 99)
    })
  })
})
