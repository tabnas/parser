/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Drift tripwires for the machine-readable contract files in ../schema:
//
// - schema/grammar.schema.json (A2): the serialized-GrammarSpec schema. The
//   alt property names must equal the alt keys the runtime accepts, a
//   known-good serialized grammar must pass a minimal structural walk of the
//   schema, and a bogus alt key must fail it. go/schema_test.go asserts the
//   same alt-key set against the Go GrammarAltSpec struct, closing the
//   cross-runtime loop.
//
// - schema/error-codes.json (A3): the generated error-code registry. It is
//   regenerated IN MEMORY here (via ts/tools/gen-error-codes.js) and
//   deep-compared against the committed file, so a catalogue change that
//   forgets `npm run gen-registry` fails this test rather than shipping a
//   stale registry. go/schema_test.go byte-compares the same file against
//   the Go catalogues.
//
// Deliberately dependency-free: no JSON Schema validator package. The walk
// below covers only the keywords this schema uses (type, anyOf, $ref into
// $defs, properties/additionalProperties, items, required, pattern,
// minimum) — enough to prove the schema describes real specs, without
// pulling a dependency into the engine's dev tree.

const { describe, it } = require('node:test')
const assert = require('node:assert')
const Fs = require('node:fs')
const Path = require('node:path')

const { buildRegistry } = require('../tools/gen-error-codes.js')

const SCHEMA_DIR = Path.join(__dirname, '..', '..', 'schema')

// The alt keys the runtime accepts. There is no literal key list in the
// engine source to import: ts/src/rules.ts normalt()/validateAlt() handle
// the fields individually (g, s, p, r, b, a, h, e, c) and carry n/u/k
// through untouched, and the GrammarAltSpec type (ts/src/types.ts) is the
// written form of the same set. So the list is maintained HERE: if you add
// an alt key to rules.ts/types.ts, add it here AND to
// schema/grammar.schema.json ($defs.alt.properties) — and to the Go
// GrammarAltSpec struct (go/grammarspec.go), which go/schema_test.go
// checks against the same schema file.
const ALT_KEYS = ['s', 'b', 'p', 'r', 'a', 'e', 'h', 'c', 'n', 'u', 'k', 'g']

function loadSchema() {
  return JSON.parse(
    Fs.readFileSync(Path.join(SCHEMA_DIR, 'grammar.schema.json'), 'utf8'),
  )
}

// Minimal structural walk: validate `data` against `schema` (a subschema of
// the grammar schema), returning a list of "path: problem" strings. Covers
// exactly the keywords grammar.schema.json uses.
function walk(data, schema, root, path, out) {
  path = path || '$'
  out = out || []

  if (schema.$ref) {
    const m = /^#\/\$defs\/(.+)$/.exec(schema.$ref)
    if (!m || !root.$defs || !root.$defs[m[1]]) {
      out.push(path + ': unresolvable $ref ' + schema.$ref)
      return out
    }
    // A $ref may sit beside description etc.; the ref wins (2020-12 allows
    // siblings, but this schema only pairs $ref with annotations).
    return walk(data, root.$defs[m[1]], root, path, out)
  }

  if (schema.anyOf) {
    const failures = []
    for (const sub of schema.anyOf) {
      const errs = walk(data, sub, root, path, [])
      if (0 === errs.length) {
        return out
      }
      failures.push(errs)
    }
    out.push(
      path + ': no anyOf branch matched (' +
      failures.map((errs) => errs.join('; ')).join(' | ') + ')',
    )
    return out
  }

  if (schema.type) {
    const t = Array.isArray(data)
      ? 'array'
      : null === data
        ? 'null'
        : typeof data
    const want = schema.type
    const ok =
      'integer' === want
        ? 'number' === t && Number.isInteger(data)
        : want === t
    if (!ok) {
      out.push(path + ': expected ' + want + ', got ' + t)
      return out
    }
  }

  if ('string' === typeof data && schema.pattern) {
    if (!new RegExp(schema.pattern).test(data)) {
      out.push(path + ': does not match pattern ' + schema.pattern)
    }
  }

  if ('number' === typeof data && undefined !== schema.minimum) {
    if (data < schema.minimum) {
      out.push(path + ': below minimum ' + schema.minimum)
    }
  }

  if (Array.isArray(data) && schema.items) {
    data.forEach((item, i) =>
      walk(item, schema.items, root, path + '[' + i + ']', out))
  }

  if (null !== data && 'object' === typeof data && !Array.isArray(data)) {
    const props = schema.properties || {}
    for (const req of schema.required || []) {
      if (!(req in data)) {
        out.push(path + ': missing required property "' + req + '"')
      }
    }
    for (const key of Object.keys(data)) {
      if (key in props) {
        walk(data[key], props[key], root, path + '.' + key, out)
      } else if (false === schema.additionalProperties) {
        out.push(path + ': unknown property "' + key + '"')
      } else if (
        schema.additionalProperties &&
        'object' === typeof schema.additionalProperties
      ) {
        walk(data[key], schema.additionalProperties, root,
          path + '.' + key, out)
      }
    }
  }

  return out
}

describe('schema', function () {
  it('grammar-schema-parses', () => {
    const schema = loadSchema()
    assert.equal(schema.$id, 'https://tabnas.dev/schema/grammar.schema.json')
    assert.equal(schema.type, 'object')
    // `ref` holds live functions and is deliberately not serializable —
    // the schema must neither list it nor admit it.
    assert.ok(!('ref' in schema.properties),
      'ref must not be a schema property (functions are not JSON)')
    assert.equal(schema.additionalProperties, false)
  })

  it('alt-keys-match-runtime', () => {
    const schema = loadSchema()
    const alt = schema.$defs && schema.$defs.alt
    assert.ok(alt && alt.properties,
      'grammar.schema.json must define $defs.alt.properties ' +
      '(go/schema_test.go reads the same path)')
    assert.deepStrictEqual(
      Object.keys(alt.properties).sort(),
      [...ALT_KEYS].sort(),
      'schema alt keys drifted from the runtime alt keys ' +
      '(ts/src/rules.ts normalt/validateAlt, ts/src/types.ts GrammarAltSpec)',
    )
    assert.equal(alt.additionalProperties, false,
      'alt objects must reject unknown keys in the schema')
  })

  it('known-good-spec-passes-walk', () => {
    const schema = loadSchema()
    // A real serialized grammar used by the engine's own tests
    // (ts/test/builtins.test.js): v + options + rule with open/close alts
    // exercising s, b, p, r, a, u keys.
    const spec = JSON.parse(Fs.readFileSync(
      Path.join(__dirname, 'json-builder.fixture.json'), 'utf8'))
    const errs = walk(spec, schema, schema)
    assert.deepStrictEqual(errs, [],
      'json-builder.fixture.json should satisfy grammar.schema.json')
  })

  // The two grammars the engine's own probe/eager tests load were CAPTURED
  // from @tabnas/abnf, so they are the fleet's only committed samples of a
  // real front-end's serialized output. Walking them is what would have
  // caught abnf emitting an `m` key the schema forbids: this test used to
  // walk only json-builder.fixture.json, which is hand-written and never
  // had one.
  it('captured-abnf-grammars-pass-walk', () => {
    const schema = loadSchema()
    for (const name of ['probe-grammar.fixture.json', 'eager-literal.fixture.json']) {
      const spec = JSON.parse(Fs.readFileSync(Path.join(__dirname, name), 'utf8'))
      const errs = walk(spec, schema, schema)
      assert.deepStrictEqual(errs, [],
        `${name} should satisfy grammar.schema.json — a captured grammar ` +
        'that fails it means a front-end is emitting keys the engine rejects')
    }
  })

  it('inject-and-null-rule-forms-pass-walk', () => {
    // The alts+inject object form (including the now-declared `clear`)
    // and a null rule entry (rule removal) are valid serialized shapes —
    // both runtimes honor them through the serialized door
    // (ts/test/serialized-grammar.test.js, go/grammarspec_json_test.go).
    const schema = loadSchema()
    const spec = JSON.parse(
      '{"v":1,"rule":{' +
      '"top":{"open":{"alts":[{"s":["#NR #ST"],"g":["custom"],"n":{"k":1}}],' +
      '"inject":{"append":true,"clear":true,"delete":[0,-1],"move":[1,0]}}},' +
      '"gone":null}}')
    assert.deepStrictEqual(walk(spec, schema, schema), [],
      'alts+inject object form and rule:null must satisfy the schema')
  })

  it('bogus-alt-key-fails-walk', () => {
    const schema = loadSchema()
    const spec = JSON.parse(Fs.readFileSync(
      Path.join(__dirname, 'json-builder.fixture.json'), 'utf8'))
    spec.rule.val.open[0].zz = true
    const errs = walk(spec, schema, schema)
    assert.ok(
      errs.some((e) => e.includes('"zz"')),
      'a bogus alt key must fail the walk; got: ' + JSON.stringify(errs),
    )

    // The deliberately-absent top-level `ref` must fail too.
    const spec2 = { v: 1, ref: {}, rule: {} }
    const errs2 = walk(spec2, schema, schema)
    assert.ok(
      errs2.some((e) => e.includes('"ref"')),
      'top-level ref must fail the walk; got: ' + JSON.stringify(errs2),
    )
  })

  it('error-code-registry-not-stale', () => {
    const committed = JSON.parse(Fs.readFileSync(
      Path.join(SCHEMA_DIR, 'error-codes.json'), 'utf8'))
    assert.deepStrictEqual(
      committed,
      buildRegistry(),
      'schema/error-codes.json is stale: run npm run gen-registry ' +
      '(from ts/, after npm run build) and commit the result',
    )
  })
})
