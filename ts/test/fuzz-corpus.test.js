/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

/* fuzz-corpus.test.js — what the differential corpus generator can EMIT.
 *
 * The 2026-08 fleet audit found four recorded accept/reject divergences in
 * this engine that `ci/fuzz/gencorpus.js` was STRUCTURALLY incapable of
 * producing:
 *
 *   P3  malformed \u / \x escapes — ESCS held only well-formed ones.
 *   P1  a value followed immediately by a quote (`a"b`) — documents were
 *   P2  assembled well-formed, so the shape never arose.
 *   P4  U+2028 / U+2029 — absent from every pool.
 *
 * A generator that cannot emit a class cannot find a bug in it, however
 * many cases it runs. Its clean runs were evidence about the generator,
 * not about the engine.
 *
 * So this suite asserts COVERAGE OF THE GENERATOR, not of the engine:
 * every class the audit named must appear in a generated corpus. Without
 * it, trimming a pool restores the blindness silently — and the symptom of
 * that blindness is a green run, which is indistinguishable from success.
 */

const { describe, it } = require('node:test')
const assert = require('node:assert')
const Fs = require('node:fs')
const Os = require('node:os')
const Path = require('node:path')
const { execFileSync } = require('node:child_process')


const GEN = Path.join(__dirname, '..', '..', 'ci', 'fuzz', 'gencorpus.js')

// Enough cases that a class appearing at a few percent is present with
// overwhelming probability, and the seed is fixed, so this is not flaky:
// it is one deterministic corpus, asserted.
const COUNT = 400
const SEED = 979899

function corpus(mode) {
  const dir = Fs.mkdtempSync(Path.join(Os.tmpdir(), 'tabnas-fuzz-'))
  try {
    execFileSync(process.execPath, [GEN, dir, String(COUNT), mode, String(SEED)],
      { stdio: 'pipe' })
    return Fs.readdirSync(dir)
      .map((f) => Fs.readFileSync(Path.join(dir, f), 'utf8'))
      .join('\n')
  }
  finally {
    Fs.rmSync(dir, { recursive: true, force: true })
  }
}


describe('fuzz-corpus', () => {

  // U+2028 / U+2029, as a character class for building patterns.
  const SEP = '[\u2028\u2029]'

  for (const mode of ['json', 'jsonic']) {
    it(`emits every recorded divergence class (${mode})`, () => {
      const all = corpus(mode)

      // P3 — malformed escapes. `parseInt` used as a validator stops at
      // the first non-hex character and returns what it read, so these
      // were accepted by one runtime and rejected by the other.
      //
      // The two escape FAMILIES are asserted separately. One `some()` over
      // both would stay green with every malformed `\x` removed as long as
      // one malformed `\u` survived, silently restoring the blindness for
      // half of what this is meant to protect.
      assert.match(all, /\\u(?:00st|12zz|1"| 123|\+123|\{110000\})/,
        'no malformed \\u escape in the corpus: half of audit item P3 is ' +
        'unreachable, and a clean run says nothing about it')

      // Only shapes the pool alone can produce. An earlier cut also
      // accepted `\x"`, which the lone-backslash entry makes incidentally
      // whenever a random `x` follows it -- so the assertion passed with
      // the entire \x family deleted. Digits and capitals cannot occur in
      // the random-letter filler, so `4z` and `ZZ` can only come from here.
      assert.match(all, /\\x(?:4z|ZZ)/,
        'no malformed \\x escape in the corpus: half of audit item P3 is ' +
        'unreachable, and a clean run says nothing about it')

      // P4 — line and paragraph separators, raw, and OUTSIDE a string.
      //
      // `jstr` also emits them inside quoted strings, where the text
      // matcher is not the thing deciding. Asserting mere presence would
      // stay green with the damage pass removed, which is exactly the
      // context P4 is about. Pin the structure: a separator immediately
      // after a `,` or `:`, which only the damage pass produces.
      assert.match(all, new RegExp('[,:]' + SEP),
        'no raw U+2028/U+2029 BETWEEN TOKENS: audit item P4 is ' +
        'unreachable. Inside a string does not count -- that is not the ' +
        'context the text matcher decides in')

      // P1/P2 — a value followed immediately by a quote. The shape whose
      // text run ended at the quote in Go and did not in TypeScript.
      //
      // Pinned as the exact shape the damage pass produces. An earlier cut
      // asserted /[a-z0-9]"/, which every ordinary `"abc"` satisfies: the
      // guard passed with the class entirely absent.
      assert.match(all, /:ab"cd/,
        'no bare-text-then-quote shape: audit items P1/P2 are unreachable')
    })
  }

  it('is deterministic for a fixed seed', () => {
    // The whole point of the pinned seed is that a diff failure can be
    // replayed exactly. A generator that drifted would make every reported
    // divergence unreproducible.
    assert.equal(corpus('json'), corpus('json'))
  })

  it('still emits ordinary well-formed documents', () => {
    // A differential runner is at its strongest where BOTH runtimes accept
    // and the values must match, so ordinary documents have to stay the
    // BULK of the corpus, not merely a presence in it.
    //
    // json mode only. jsonic mode deliberately relaxes into things JSON
    // rejects -- unquoted keys, comments, trailing commas -- so measuring
    // it with JSON.parse would be measuring the relaxations, not the
    // damage pass.
    const cases = corpus('json').split('\n').filter(Boolean)
    const clean = cases.filter((c) => {
      try {
        JSON.parse(c)
        return true
      }
      catch {
        return false
      }
    })

    // A real majority, with margin: the pinned corpus sits near 85%.
    // Damage is chosen once per DOCUMENT rather than per character for
    // exactly this reason -- per-character injection put something
    // malformed in nearly half of these deeply nested documents.
    assert.ok(clean.length > cases.length * 0.6,
      `only ${clean.length} of ${cases.length} cases are well-formed JSON; ` +
      'the damage pass has taken over the corpus, and ordinary ' +
      'accepted-value comparison is no longer the bulk of a run')
  })
})
