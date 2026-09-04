/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// The executable cross-port divergence register — TypeScript side.
//
// test/spec/divergent.tsv records each KNOWN split as the value each port
// actually produces. This runner asserts the `ts` column; the Go and Rust
// runners assert their columns from the same file, and the Go runner
// carries the coverage gate that ties the register to DIVERGENCE.md.
//
// The property that matters: a divergence which gets FIXED fails here as
// loudly as one that regresses, forcing the row to be deleted. Prose
// cannot do that — the sibling repo's differences doc claimed 2.e3 and
// 1e999 still diverged long after they had been aligned, which is what
// moved jsonic to an executable ledger and this repo to ADR-14.
//
// If a row fails, do not adjust it to match. Either the divergence moved
// and DIVERGENCE.md is now wrong, or it was repaired and the row (and
// the entry it registers) should go.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas, makeLex } = require('..')
const { loadTSV } = require('./utility')

const COLS = 8
const MAX_TOKENS = 64

// ---------------------------------------------------------------------
// Rendering.
//
// Deliberately NOT the runtime's own quoting or key order. Go's %q and
// JavaScript's JSON.stringify escape different characters, and the two
// runtimes sort strings by different units — both cost this repo a round
// of review already (#156). Everything below renders the same bytes as
// its Go twin by construction.

// UTF-16 code units, lowercase, dot-joined: the one rendering of a string
// value that shows the lone-surrogate split without either port having to
// spell a character it cannot hold. JS strings ARE UTF-16, so this reads
// the units directly; Go's twin encodes to UTF-16 first.
function valhex(v) {
  if ('string' !== typeof v) return 'NOT-A-STRING'
  const out = []
  for (let i = 0; i < v.length; i++) {
    out.push(v.charCodeAt(i).toString(16).padStart(4, '0'))
  }
  return 0 === out.length ? 'EMPTY' : out.join('.')
}

// Sort by UTF-16 code unit. JS string comparison is already UTF-16-ordered,
// so this is the identity here and the Go twin does the work; it is spelled
// out so the two files state the same contract.
const byUtf16 = (a, b) => (a < b ? -1 : a > b ? 1 : 0)

// A canonical value rendering shared with the Go runner. Strings render
// VERBATIM between quotes — a row must therefore not expect a value
// containing a tab, newline or carriage return — and map keys are sorted,
// because ADR-15 puts key order out of contract.
function canon(v) {
  if (null === v || undefined === v) return 'null'
  if ('boolean' === typeof v) return v ? 'true' : 'false'
  // String() IS the canonical form: ECMAScript Number::toString is what
  // the Go twin's jsNumberString reimplements, because Go's %v disagrees
  // with it (1e20, 1e-7, ...). Nothing to do here but say so.
  if ('number' === typeof v) return String(v)
  if ('bigint' === typeof v) return String(v)
  if ('string' === typeof v) return '"' + v + '"'
  if (Array.isArray(v)) return '[' + v.map(canon).join(',') + ']'
  if ('object' === typeof v) {
    const keys = Object.keys(v).sort(byUtf16)
    return '{' + keys.map((k) => '"' + k + '":' + canon(v[k])).join(',') + '}'
  }
  return 'UNRENDERABLE'
}

// ---------------------------------------------------------------------
// Probes. A closed set: an unknown probe is an error, never a skip, or a
// row could sit here asserting nothing while looking green.

function tokenField(t, field) {
  switch (field) {
    case 'name':
      return t.name
    case 'src':
      return t.src
    case 'si':
      return String(t.sI)
    case 'ri':
      return String(t.rI)
    case 'ci':
      return String(t.cI)
    case 'valhex':
      return valhex(t.val)
    default:
      throw new Error('unknown lex show field (extend both runners): ' + field)
  }
}

// Lex `input` and select ONE token. Never the whole stream: the two ports
// emit different token SEQUENCES for the same source (Go emits no #SP
// where this port does), so a stream render would go red for a reason no
// row is about.
function probeLex(arg, input) {
  const j = new Tabnas(arg.opts || {})
  const lex = makeLex({
    src: () => input,
    cfg: j.internal().config,
    opts: j.options,
    sub: {},
  })

  const tokens = []
  // The cap counts RETAINED tokens, not next() calls, and #SP is dropped
  // before it counts. Both halves are load-bearing. This port emits an
  // #SP between whitespace-separated tokens and Go emits none, so a cap
  // on calls is reached after half as many real tokens here: measured, a
  // `find` target 40 tokens in was NOT-FOUND here and found at column 79
  // in Go, under an identical cap — manufacturing the exact
  // sequence-dependent difference selecting one token is meant to avoid.
  // Dropping #SP also makes `at` mean the same token in both ports.
  //
  // The consequence, stated so nobody is surprised by it: this probe
  // cannot address an #SP token. Whether that asymmetry is itself a
  // divergence is an open question and not one a row here should
  // prejudge.
  for (let guard = 0; tokens.length < MAX_TOKENS && guard < 4 * MAX_TOKENS; guard++) {
    const t = lex.next()
    if (!t) break
    // A lex failure surfaces as a #BD token here and on lex.Err in Go.
    // The register asserts the observable — code, column, span — not the
    // channel, which is a porting difference rather than a divergence.
    if ('#BD' === t.name) {
      return 'ERROR:' + t.why + ':' + t.cI + ':' + t.src
    }
    if ('#ZZ' === t.name) break
    if ('#SP' === t.name) continue
    tokens.push(t)
  }

  let tk
  if (undefined !== arg.find) tk = tokens.find((t) => t.src === arg.find)
  else tk = tokens[undefined === arg.at ? 0 : arg.at]
  if (!tk) return 'NOT-FOUND'

  const show = arg.show || ['name']
  return show.map((f) => tokenField(tk, f)).join(':')
}

function errField(e, field) {
  switch (field) {
    case 'code':
      return String(e.code)
    case 'pos':
      return String(e.pos)
    case 'col':
      return String(e.col)
    case 'row':
      return String(e.row)
    default:
      throw new Error('unknown spec show field (extend both runners): ' + field)
  }
}

// Install a serialized GrammarSpec and parse. A spec is pure JSON, so the
// SAME text drives both ports — which is what lets a grammar-level row be
// registered here at all.
function probeSpec(arg, input, specs) {
  let spec = arg.spec
  if ('string' === typeof spec) {
    if (!(spec in specs)) {
      throw new Error('row names an undefined `# @spec`: ' + spec)
    }
    spec = specs[spec]
  }
  if (null == spec || 'object' !== typeof spec) {
    throw new Error('spec probe needs arg.spec (an object, or an `# @spec` name)')
  }

  const j = new Tabnas(arg.opts || {})
  try {
    // Deep-copy: grammar() may retain or mutate what it is handed, and a
    // named spec is shared by every row that references it.
    j.grammar(JSON.parse(JSON.stringify(spec)))
  } catch {
    return 'INSTALL_ERROR'
  }

  try {
    const v = j.parse(input)
    return arg.show ? 'OK' : 'OK:' + canon(v)
  } catch (e) {
    if (!arg.show) return 'ERROR:' + e.code
    return 'ERROR:' + arg.show.map((f) => errField(e, f)).join(':')
  }
}

function runProbe(probe, arg, input, specs) {
  switch (probe) {
    case 'lex':
      return probeLex(arg, input)
    case 'spec':
      return probeSpec(arg, input, specs)
    default:
      throw new Error('unknown probe (extend both runners): ' + probe)
  }
}

// ---------------------------------------------------------------------

// `# @spec <name> <json>` definitions, so the regex rows do not repeat a
// 160-character grammar five times.
function collectSpecs(rows) {
  const specs = {}
  for (const { cols, row } of rows) {
    if (1 !== cols.length || !cols[0].startsWith('# @spec ')) continue
    const rest = cols[0].slice('# @spec '.length)
    const sp = rest.indexOf(' ')
    assert.ok(0 < sp, `line ${row}: malformed # @spec directive`)
    const name = rest.slice(0, sp)
    specs[name] = JSON.parse(rest.slice(sp + 1))
  }
  return specs
}

// The twin of go/divergent_test.go's TestJSNumberStringMatchesJavaScript.
// Go cannot use its %v here — it disagrees with String() on 1e20, 1e-7
// and more — so it reimplements ECMAScript Number::toString, and this
// side asserts the same table against the real thing. A value added to
// one table belongs in the other.
const JS_NUMBER_CASES = [
  [0, '0'],
  [1, '1'],
  [-1, '-1'],
  [3, '3'],
  [0.1, '0.1'],
  [-0.1, '-0.1'],
  [0.5, '0.5'],
  [1.5, '1.5'],
  [100, '100'],
  [1e6, '1000000'],
  // The two Go's %v got wrong, and the reason the twin exists.
  [1e20, '100000000000000000000'],
  [1e-7, '1e-7'],
  // Either side of each threshold in the spec's case split.
  [1e21, '1e+21'],
  [1e-6, '0.000001'],
  [1e-21, '1e-21'],
  [123456789012345680000, '123456789012345680000'],
  [5e-324, '5e-324'],
  [1.7976931348623157e308, '1.7976931348623157e+308'],
  [0.30000000000000004, '0.30000000000000004'],
  [2 / 3, '0.6666666666666666'],
  [-0, '0'],
  [9007199254740993, '9007199254740992'],
  [1e100, '1e+100'],
  [1.25e-10, '1.25e-10'],
  [255, '255'],
  [1e-3, '0.001'],
]

describe('divergent canonical number rendering', () => {
  it('canon renders a number exactly as String() does', () => {
    for (const [v, want] of JS_NUMBER_CASES) {
      assert.equal(canon(v), want, 'canon(' + want + ')')
    }
  })
})

describe('divergent', () => {
  it('every register row still diverges exactly as recorded', () => {
    const rows = loadTSV('divergent')
    assert.ok(
      0 < rows.length,
      'divergent.tsv has no rows; if the register is empty, delete the file ' +
        'and its runners rather than leaving an assertion that asserts nothing',
    )

    const specs = collectSpecs(rows)
    const seen = new Set()
    let ran = 0

    for (const { cols, row } of rows) {
      if (1 === cols.length && cols[0].startsWith('#')) continue
      assert.equal(
        cols.length,
        COLS,
        `line ${row}: want ${COLS} columns ` +
          `(name probe arg input go ts rust justification), got ${cols.length}`,
      )
      const [name, probe, argRaw, input, , wantTs, , why] = cols

      assert.ok(!seen.has(name), `${name}: duplicate row name`)
      seen.add(name)
      assert.ok(why && why.trim(), `${name}: a register row must carry a justification`)

      const arg = '-' === argRaw || '' === argRaw ? {} : JSON.parse(argRaw)
      const got = runProbe(probe, arg, input, specs)
      ran++

      assert.equal(
        got,
        wantTs,
        `${name}: the TS side of the register is stale.\n` +
          `  probe: ${probe} ${argRaw}\n  input: ${JSON.stringify(input)}\n` +
          `  got:   ${got}\n  want:  ${wantTs}\n` +
          'If TS now AGREES with the go column the divergence is REPAIRED — ' +
          'delete the row, and the DIVERGENCE.md entry with it. Do not edit ' +
          'the column to match.',
      )
    }

    // A register that silently ran nothing is the failure mode this file
    // exists to prevent.
    assert.ok(0 < ran, 'divergent.tsv parsed but no data row ran')
  })
})
