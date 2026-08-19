// gencorpus.js — seeded random-document generator biased toward grammar
// edges: escape sequences (incl. surrogate pairs and \u{...}), exotic
// number forms, deep nesting, and (in jsonic mode) comments, unquoted
// keys, and trailing commas. Feeds the cross-runtime diff runner
// (run-diff.sh); the pinned seed makes corpora reproducible, so a diff
// failure can be replayed exactly.
//
// WHAT IT COULD NOT GENERATE, AND WHY THAT MATTERED
//
// The 2026-08 fleet audit found four recorded accept/reject divergences in
// this engine that this generator was STRUCTURALLY incapable of producing:
//
//   P3  malformed \u / \x escapes -- ESCS held only well-formed ones.
//   P1  a value followed immediately by a quote (`a"b`) -- documents were
//   P2  assembled well-formed, so the shape never arose.
//   P4  U+2028 / U+2029 -- absent from every pool.
//
// A generator that cannot emit a class cannot find a bug in it, however
// many cases it runs. Its clean runs were evidence about the generator,
// not about the engine. The pools below now cover all four; see MALFORMED,
// SEPARATORS and the `damage` pass.
//
// These shapes are mostly INVALID input, which is the point: this is a
// differential runner, so what it asserts is that the two runtimes make
// the SAME decision, not that the decision is acceptance.
//
// Usage: node gencorpus.js <out-dir> <count> [json|jsonic] [seed]
'use strict'
const fs = require('fs')
const path = require('path')

const outDir = process.argv[2]
const count = Number(process.argv[3] || 200)
const mode = process.argv[4] || 'json'
const seed = Number(process.argv[5] || 979899)
fs.mkdirSync(outDir, { recursive: true })

function prng(s) {
  let a = s >>> 0
  return () => {
    a |= 0; a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const NUMS = ['0', '-0', '1', '123', '-45', '0.5', '1.25e3', '2E-7',
  '1e308', '9007199254740993', '0.0000001', '-1.5E+10']
const ESCS = ['\\n', '\\t', '\\r', '\\"', '\\\\', '\\/', '\\b', '\\f',
  '\\u0041', '\\u00e9', '\\u4e2d', '\\ud83d\\ude00', '\\u0000']

// MALFORMED escapes (audit P3, and jsonic U3/U4). Every one of these was
// accepted by at least one runtime and rejected by the other, or accepted
// by both with DIFFERENT values -- `parseInt` used as a validator stops at
// the first non-hex character and returns what it read, so `"\u00st"`
// decoded and silently dropped the `st`.
//
// NO LONE SURROGATE HERE, deliberately, and this is worth reading twice.
//
// `\uD800` looks like the ideal control: it is a RECORDED, permanent
// divergence, so it would prove the runner still sees a difference it is
// supposed to see. But run-diff is a ZERO-DIFFERENCE gate, and a permanent
// difference in its corpus makes it permanently red — measured: with
// `\uD800` as the only malformed entry, 103 of 300 cases diverge, and no
// engine repair would ever bring that to zero.
//
// A control belongs where a difference is the expected answer: the
// divergence register (ADR-14), or an explicit expected-difference list.
// Putting one in a gate whose contract is "these must agree" does not test
// the gate, it disables it.
const MALFORMED = ['\\u00st', '\\u12zz', '\\u1', '\\u 123', '\\u+123',
  '\\x4z', '\\xZZ', '\\x', '\\u{110000}', '\\q', '\\']

// Line and paragraph separators (audit P4). JavaScript's `.` excludes
// four line terminators; RE2's excludes one, so a text run crossing these
// two code points split differently in the two ports. Raw, not escaped --
// escaped they are ordinary \u sequences and exercise nothing.
const SEPARATORS = ['\u2028', '\u2029']

// Whether the document currently being built may carry malformed content.
//
// Decided PER DOCUMENT, not per character, and that distinction is the
// whole of it. A differential runner is at its strongest on the case where
// both runtimes accept and the values must match, so ordinary well-formed
// documents have to stay the bulk of the corpus. But these documents are
// deeply nested with many strings, so even a 5% per-character rate put a
// malformed escape somewhere in nearly half of them: measured 55% well
// formed, which is a majority only barely. Choosing once per document
// keeps roughly three quarters clean while leaving the damaged quarter
// dense enough to carry every class. fuzz-corpus.test.js pins both ends.
let damaged = false

function jstr(r, depthBias) {
  let s = '"'
  const n = Math.floor(r() * 10)
  for (let i = 0; i < n; i++) {
    const k = r()
    if (k < 0.30) {
      s += ESCS[Math.floor(r() * ESCS.length)]
    }
    else if (damaged && k < 0.45) {
      s += MALFORMED[Math.floor(r() * MALFORMED.length)]
    }
    else if (damaged && k < 0.52) {
      s += SEPARATORS[Math.floor(r() * SEPARATORS.length)]
    }
    else {
      s += String.fromCharCode(97 + Math.floor(r() * 26))
    }
  }
  return s + '"'
}

function value(r, depth) {
  const k = r()
  if (depth > 24 || k < 0.25) {
    const s = r()
    if (s < 0.35) return NUMS[Math.floor(r() * NUMS.length)]
    if (s < 0.7) return jstr(r)
    if (s < 0.8) return 'true'
    if (s < 0.9) return 'false'
    return 'null'
  }
  if (k < 0.6) {
    const n = Math.floor(r() * 4)
    const items = []
    for (let i = 0; i < n; i++) items.push(value(r, depth + 1))
    return '[' + items.join(',') + ']'
  }
  const n = Math.floor(r() * 4)
  const pairs = []
  for (let i = 0; i < n; i++) pairs.push(jstr(r) + ':' + value(r, depth + 1))
  return '{' + pairs.join(',') + '}'
}

// Damage a finished document into the shapes a well-formed generator
// cannot reach (audit P1/P2/P4). Applied to BOTH modes, because the
// divergences it targets are in the engine's lexer, not in jsonic's
// relaxations.
//
// Kept as a post-pass rather than folded into `value()` so the undamaged
// generator stays exactly what it was: a corpus of well-formed documents
// is still what most of the run should be, and a diff on a damaged case
// should be traceable to the one edit that made it.
function damage(r, doc) {
  if (!damaged) {
    return doc
  }
  let s = doc

  // A value followed immediately by a quote character -- `a"b`. This is
  // the shape whose text run ended at the quote in Go and did not in
  // TypeScript: ~2/3 of the 1,612 divergences in jsonic's own fuzz run.
  //
  // It has to be BARE text, not a number or a keyword. An earlier cut
  // appended a quote before a `,]}`, but at that position this generator
  // only ever emits numbers and true/false/null, and jsonic's relaxation
  // unquotes KEYS, which are followed by a colon. So it produced `true"q`
  // and never `a"b`, and the text matcher -- the thing under test -- was
  // never reached. The value is replaced outright instead, which is the
  // only way to be sure the text is text.
  if (r() < 0.5) {
    s = s.replace(/:\s*("(?:[^"\\]|\\.)*"|-?\d[\d.eE+-]*|true|false|null)/,
      () => ':ab"cd')
  }

  // A bare line separator OUTSIDE a string, between tokens, where the
  // text matcher decides whether a run may cross it.
  if (r() < 0.5) {
    s = s.replace(/([,:])/, (m, c) =>
      c + SEPARATORS[Math.floor(r() * SEPARATORS.length)])
  }

  return s
}

// jsonic mode: post-process valid JSON into relaxed forms.
function relax(r, doc) {
  let s = doc
  if (r() < 0.5) s = s.replace(/"([a-z]{2,8})":/g, (m, k) => (r() < 0.6 ? k + ':' : m))
  if (r() < 0.3) s = s.replace(/,/g, (m) => (r() < 0.2 ? ', // c\n' : m))
  if (r() < 0.3) s = s.replace(/\}/g, (m) => (r() < 0.3 ? ',}' : m))
  if (r() < 0.2) s = '# leading comment\n' + s
  return s
}

const r = prng(seed)
for (let i = 0; i < count; i++) {
  damaged = r() < 0.25
  let doc = value(r, 0)
  if ('jsonic' === mode) doc = relax(r, doc)
  doc = damage(r, doc)
  fs.writeFileSync(path.join(outDir, `case-${String(i).padStart(5, '0')}.in`), doc)
}
console.log(`gencorpus: ${count} ${mode} cases (seed ${seed}) -> ${outDir}`)
