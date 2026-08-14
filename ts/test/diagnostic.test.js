/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Structured diagnostics: TabnasError.toJSON (reached via
// JSON.stringify(err)) emits the shape documented in
// schema/diagnostic.schema.json. The shared parity fixture
// test/spec/diagnostic.tsv is run here and by the Go port
// (go/diagnostic_spec_test.go); each row asserts the JSON SUBSET both
// runtimes must emit byte-identically. Only the error `code` is
// contractual across runtimes; the subset rows additionally pin the new
// structural fields (rule/ruleStack/expected/token/len/...) that are
// designed to be runtime-identical.

const { readFileSync } = require('fs')
const { join } = require('path')
const { describe, it } = require('node:test')
const assert = require('node:assert')

const pkg = require('../package.json')
const { Tabnas, TabnasError } = require('..')
const { json } = require('../dist-test/json-plugin')
const { loadTSV } = require('./utility')

// Serialize-then-parse an expected parse failure.
function diag(j, src) {
  try {
    j.parse(src)
  } catch (e) {
    return JSON.parse(JSON.stringify(e))
  }
  assert.fail('parse should fail: ' + JSON.stringify(src))
}

// Deep-compare only the given keys of a diagnostic.
function assertSubset(got, want, label) {
  for (const key of Object.keys(want)) {
    assert.deepStrictEqual(got[key], want[key], label + ' key ' + key)
  }
}

describe('diagnostic', function () {
  it('spec-fixture', () => {
    const j = new Tabnas({ plugins: [json] })
    let ran = 0
    for (const { cols, row } of loadTSV('diagnostic')) {
      // Comment lines start with '#' and contain no tab.
      if (cols.length < 2 || cols[0].startsWith('#')) {
        continue
      }
      ran++
      const [input, expectedJSON] = cols
      const want = JSON.parse(expectedJSON)
      const got = diag(j, input)
      for (const key of Object.keys(want)) {
        assert.deepStrictEqual(
          got[key],
          want[key],
          'diagnostic.tsv row ' + row + ' key ' + key + ': ' + input,
        )
      }
    }
    assert.ok(0 < ran, 'diagnostic.tsv contained no runnable rows')
  })

  it('degenerate-never-throws', () => {
    // An error whose errdesc failed (its catch path returns {}) has no
    // code/txts/internal at all — toJSON must still produce the shape.
    const bare = Object.create(TabnasError.prototype)
    const out = bare.toJSON()
    assert.equal(out.status, 'failure')
    assert.equal(out.code, 'unknown')
    assert.equal(out.message, '')
    assert.deepStrictEqual(out.ruleStack, [])
    assert.deepStrictEqual(out.expected, [])
    assert.deepStrictEqual(out.token, { name: '', src: '' })
    // JSON.stringify must not throw either.
    JSON.parse(JSON.stringify(bare))
  })

  it('status-and-version', () => {
    const j = new Tabnas({ plugins: [json] })
    const d = diag(j, '{"a": }')
    assert.equal(d.status, 'failure')
    assert.equal(d.version, pkg.version)
  })

  it('hint-has-no-display-indent', () => {
    const j = new Tabnas({ plugins: [json] })
    const d = diag(j, '{"a": }')
    assert.ok(0 < d.hint.length, 'hint expected for unexpected code')
    for (const line of d.hint.split('\n')) {
      assert.ok(
        !line.startsWith('  '),
        'hint line still carries display indent: ' + JSON.stringify(line),
      )
    }
    assert.equal(d.hint, d.hint.trim())
  })

  it('src-is-the-failing-line', () => {
    const j = new Tabnas({ plugins: [json] })
    assert.equal(diag(j, '{"a": }').src, '{"a": }')
    // Multi-line: only the line containing the error, row advances.
    const d = diag(j, '{"a":1,\n"b":]}')
    assert.equal(d.row, 2)
    assert.equal(d.src, '"b":]}')
    // Windows line ending: the trailing \r is stripped from the line
    // (the error is on line 1, whose split-on-\n remainder ends in \r).
    assert.equal(diag(j, '{"a":]\r\n}').src, '{"a":]')
  })

  it('alt-e-raise-site-context', () => {
    // Grammar `e` errors report the context AT THE RAISE SITE — the rule
    // stack, the rule's state (which picks the expected-token column),
    // and the pre-consumption lookahead token — not the post-mutation
    // world after pops/pushes/the state flip. Values below are pinned
    // IDENTICALLY in go/diagnostic_test.go
    // (TestDiagnosticAltERaiseSiteContext); the Go engine records the
    // error and lets Process keep running, so it must snapshot at the
    // moment of the raise.

    // Close-state e: expected comes from the CLOSE alts.
    {
      const j = new Tabnas({ rule: { start: 'top' } })
      j.rule('top', (rs) => rs
        .open([{ s: ['#NR'] }])
        .close([{ s: ['#ZZ'] }, { e: (r, ctx) => ctx.t0 }]))
      assertSubset(diag(j, '1 1'), {
        code: 'unexpected', row: 1, col: 3, pos: 2, len: 1,
        rule: 'top', ruleStack: ['top'], expected: ['#ZZ'],
        token: { name: '#NR', src: '1' },
      }, 'close-state e')
    }

    // Open-state e: expected comes from the OPEN alts (empty here — the
    // close alts DO name #ZZ, so a late state-flip would leak it).
    {
      const j = new Tabnas({ rule: { start: 'top' } })
      j.rule('top', (rs) => rs
        .open([{ e: (r, ctx) => ctx.t0 }])
        .close([{ s: ['#ZZ'] }]))
      assertSubset(diag(j, '1'), {
        code: 'unexpected', row: 1, col: 1, pos: 0, len: 0,
        rule: 'top', ruleStack: ['top'], expected: [],
        token: { name: '', src: '' },
      }, 'open-state e')
    }

    // Open-state e on an alt that also pushes: the push has NOT happened
    // at the raise site, so the pushed child must not appear in the
    // stack (a late snapshot would report ["top","top"-child frames]).
    {
      const j = new Tabnas({ rule: { start: 'top' } })
      j.rule('top', (rs) => rs
        .open([{ s: ['#NR'], p: 'child', e: (r, ctx) => ctx.t0 }])
        .close([{ s: ['#ZZ'] }]))
      j.rule('child', (rs) => rs
        .open([{ s: ['#NR'] }])
        .close([{ s: ['#ZZ'] }]))
      assertSubset(diag(j, '1 1'), {
        code: 'unexpected', row: 1, col: 1, pos: 0, len: 1,
        rule: 'top', ruleStack: ['top'], expected: ['#NR'],
        token: { name: '#NR', src: '1' },
      }, 'open-state e with push')
    }
  })

  it('trailing-content-final-fetch', () => {
    // Trailing content discovered by the parser's FINAL lexer fetch
    // (empty lookahead buffer): the error reports the real trailing
    // token — not a stale NOTOKEN at 1:1 — and trailing ignorable
    // trivia is not trailing content. Pinned identically in
    // go/diagnostic_test.go (TestDiagnosticTrailingContentFinalFetch).
    const j = new Tabnas({ rule: { start: 'top' } })
    j.rule('top', (rs) => rs.open([{ s: ['#NR'] }]))
    assertSubset(diag(j, '1 2'), {
      code: 'unexpected', row: 1, col: 3, pos: 2, len: 1,
      rule: 'top', ruleStack: ['top'], expected: [],
      token: { name: '#NR', src: '2' },
    }, 'trailing content')
    // A trailing space is consumed silently, exactly as Go's Lex.Next
    // skips ignorables.
    assert.doesNotThrow(() => j.parse('1 '))
  })

  it('unclaimed-char-token', () => {
    // A character no matcher can produce is reported as a #BD token in
    // both runtimes (Go used to attach #UK on this path). The BMP case
    // is fully pinned in the shared fixture ('x' row); the ASTRAL case
    // has a KNOWN token.src divergence of the recorded scan-unit class
    // (DIVERGENCE.md "Column positions for astral characters"): this
    // port's lexer takes one UTF-16 unit (a lone high surrogate), Go
    // takes one rune (the whole character) — len is 1 in both.
    // go/diagnostic_test.go (TestDiagnosticUnclaimedCharToken) asserts
    // the OPPOSITE src on purpose.
    const j = new Tabnas({ plugins: [json] })
    assertSubset(diag(j, '\u{1F600}'), {
      code: 'unexpected', row: 1, col: 1, pos: 0, len: 1,
      rule: 'val', ruleStack: ['val'],
      token: { name: '#BD', src: '\ud83d' }, // Go: '😀'
    }, 'unclaimed astral')
  })

  it('plugins-lists-registered-plugins', () => {
    // Compose with a trivial inline plugin; plugins lists names in
    // registration order (a list, not a single attributed "plugin").
    const marker = function markerplug(_tn) {}
    const j = new Tabnas({ plugins: [json, marker] })
    const d = diag(j, '{"a": }')
    assert.deepStrictEqual(d.plugins, ['json', 'markerplug'])
  })

  it('matches-schema', () => {
    // Dependency-free shape check against schema/diagnostic.schema.json:
    // required keys present, declared types respected, no unknown keys.
    const schema = JSON.parse(
      readFileSync(
        join(__dirname, '..', '..', 'schema', 'diagnostic.schema.json'),
        'utf8',
      ),
    )

    function checkType(val, prop, path) {
      if ('string' === prop.type) {
        assert.equal(typeof val, 'string', path + ' must be a string')
      } else if ('integer' === prop.type) {
        assert.ok(Number.isInteger(val), path + ' must be an integer')
      } else if ('array' === prop.type) {
        assert.ok(Array.isArray(val), path + ' must be an array')
        if (prop.items) {
          val.forEach((v, i) => checkType(v, prop.items, path + '[' + i + ']'))
        }
      } else if ('object' === prop.type) {
        assert.ok(
          null != val && 'object' === typeof val && !Array.isArray(val),
          path + ' must be an object',
        )
        for (const k of prop.required || []) {
          assert.ok(k in val, path + ' missing required key ' + k)
        }
        for (const k of Object.keys(val)) {
          if (false === prop.additionalProperties) {
            assert.ok(
              prop.properties && k in prop.properties,
              path + ' has unknown key ' + k,
            )
          }
          if (prop.properties && k in prop.properties) {
            checkType(val[k], prop.properties[k], path + '.' + k)
          }
        }
      }
      if (undefined !== prop.const) {
        assert.deepStrictEqual(val, prop.const, path + ' const mismatch')
      }
    }

    const j = new Tabnas({ plugins: [json] })
    for (const src of ['{"a": }', '"abc', '{', '[1,2,]']) {
      checkType(diag(j, src), schema, 'diagnostic(' + JSON.stringify(src) + ')')
    }
  })
})
