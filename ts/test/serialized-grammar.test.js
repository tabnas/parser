/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// The TS half of the serialized-door parity tests: the same pure-JSON
// grammar specs (built via JSON.parse, so nothing an object literal can
// smuggle in) must accept/reject identically in both runtimes. The Go half
// lives in go/grammarspec_json_test.go (TestFromJSON*) and
// go/schema_test.go (the hint funnel pair) — each test below names its
// mirror. TS is canonical: these pin what the Go door must reproduce.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

// Apply a serialized spec (a JSON string) to a fresh default instance.
function loadJSONGrammar(spec) {
  const tn = new Tabnas()
  tn.grammar(JSON.parse(spec))
  return tn
}

function accepts(tn, src) {
  try {
    tn.parse(src)
    return true
  } catch (e) {
    return false
  }
}

describe('serialized-grammar', () => {
  // Mirror: go TestFromJSONRuleNullRemovesRule.
  it('rule-null-removes-rule', () => {
    const tn = loadJSONGrammar(
      '{"clear":true,"options":{"rule":{"start":"top"}},"rule":{' +
      '"top":{"open":[{"s":"#NR"},{"p":"other"}]},' +
      '"other":{"open":[{"s":"#ST"}]}}}')
    assert.ok(accepts(tn, '42'))
    assert.ok(accepts(tn, '"s"'))

    tn.grammar(JSON.parse('{"rule":{"other":null}}'))

    assert.deepStrictEqual(Object.keys(tn.rule()), ['top'])
    assert.throws(() => tn.parse('"s"'), (e) => 'unknown_rule' === e.code)
  })

  // Mirror: go TestFromJSONArrayTokenSpec. Array-form s: each element is
  // a slot; space-separated names within an element are alternatives for
  // that slot.
  it('array-token-spec', () => {
    const tn = loadJSONGrammar(
      '{"clear":true,"options":{"rule":{"start":"top"}},' +
      '"rule":{"top":{"open":[{"s":["#NR #ST"]}]}}}')
    assert.ok(accepts(tn, '42'))
    assert.ok(accepts(tn, '"s"'))
    assert.throws(() => tn.parse('abc'), (e) => 'unexpected' === e.code)
  })

  // Mirror: go TestFromJSONArrayGroupTagsFilterable. Array-form g must
  // land as real group tags, or rule.include/exclude filtering silently
  // stops working for serialized grammars.
  it('array-group-tags-filterable', () => {
    const spec =
      '{"clear":true,"options":{"rule":{"start":"top"}},' +
      '"rule":{"top":{"open":[' +
      '{"s":["#NR"]},' +
      '{"s":"#ST","g":["custom","special"]}]}}}'

    const plain = loadJSONGrammar(spec)
    assert.ok(accepts(plain, '42'))
    assert.ok(accepts(plain, '"s"'))
    assert.deepStrictEqual(
      plain.rule('top').def.open.map((a) => a.g),
      [[], ['custom', 'special']])

    const filtered = loadJSONGrammar(spec)
    filtered.options({ rule: { exclude: 'special' } })
    assert.ok(accepts(filtered, '42'))
    assert.throws(() => filtered.parse('"s"'), (e) => 'unexpected' === e.code)
  })

  // Mirror: go TestFromJSONPreservesMeta. `meta` is engine-ignored tool
  // metadata (schema/grammar.schema.json): a spec carrying it must parse
  // exactly as one without it, and the caller's own spec object must
  // still hold the meta after grammar() (the install works on a private
  // copy — meta is for whoever holds the spec, not for the engine).
  it('meta-is-engine-ignored-passthrough', () => {
    const spec = JSON.parse(
      '{"clear":true,"options":{"rule":{"start":"top"}},' +
      '"meta":{"provenance":{"top$step1":"top"}},' +
      '"rule":{"top":{"open":[{"s":"#NR"}]}}}')
    const tn = new Tabnas()
    tn.grammar(spec)
    assert.ok(accepts(tn, '42'))
    assert.throws(() => tn.parse('"s"'), (e) => 'unexpected' === e.code)
    assert.deepStrictEqual(spec.meta, { provenance: { top$step1: 'top' } })
  })

  // Mirror: go TestFromJSONInjectClearReplacesAlts. inject {clear:true}
  // replaces a rule's alternates outright, from pure JSON.
  it('inject-clear-replaces-alts', () => {
    const tn = loadJSONGrammar(
      '{"clear":true,"options":{"rule":{"start":"top"}},' +
      '"rule":{"top":{"open":[{"s":"#NR"}]}}}')
    assert.ok(accepts(tn, '42'))
    assert.ok(!accepts(tn, '"s"'))

    tn.grammar(JSON.parse(
      '{"rule":{"top":{"open":{"alts":[{"s":"#ST"}],"inject":{"clear":true}}}}}'))

    assert.ok(accepts(tn, '"s"'))
    assert.throws(() => tn.parse('42'), (e) => 'unexpected' === e.code)
  })

  // Mirror: go TestUncataloguedCodeGetsUnknownHintThroughParse
  // (go/schema_test.go). A grammar-raised code with no hint of its own
  // falls back to the "unknown" hint (error.ts errdesc), whose {details}
  // placeholder must render the raised details — never ship raw. The
  // ref bag carries the one function a serialized spec cannot; the rest
  // of the spec is the pure-JSON shape.
  it('uncatalogued-code-gets-unknown-hint', () => {
    const tn = new Tabnas()
    tn.grammar({
      clear: true,
      options: { rule: { start: 'top' } },
      ref: { '@mkbad': (rule, ctx) => ctx.t0.bad('mystery_code', { what: 'it' }) },
      rule: { top: { open: [{ s: '#NR', e: '@mkbad' }] } },
    })
    try {
      tn.parse('42')
      assert.fail('expected a parse error')
    } catch (e) {
      assert.equal(e.code, 'mystery_code')
      const hint = e.toJSON().hint
      // Go renders {what:it} (sorted keys, no state detail) — key order
      // and the TS-only `state` key are non-contractual text.
      assert.equal(hint,
        'Unknown error code: mystery_code\nDetails:\n{what:it,state:open}')
      assert.ok(!hint.includes('{details}'),
        'raw {details} placeholder reached the user: ' + hint)
    }
  })

  // Mirror: go TestUnknownHintRendersDetailsThroughParse.
  it('unknown-code-hint-renders-details', () => {
    const tn = new Tabnas()
    tn.grammar({
      clear: true,
      options: { rule: { start: 'top' } },
      ref: { '@mkbad': (rule, ctx) => ctx.t0.bad('unknown', { reason: 'boom' }) },
      rule: { top: { open: [{ s: '#NR', e: '@mkbad' }] } },
    })
    try {
      tn.parse('42')
      assert.fail('expected a parse error')
    } catch (e) {
      assert.equal(e.code, 'unknown')
      assert.equal(e.toJSON().hint,
        'Unknown error code: unknown\nDetails:\n{reason:boom,state:open}')
    }
  })
})
