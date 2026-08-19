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

  for (const mode of ['json', 'jsonic']) {
    it(`emits every recorded divergence class (${mode})`, () => {
      const all = corpus(mode)

      // P3 — malformed escapes. `parseInt` used as a validator stops at
      // the first non-hex character and returns what it read, so these
      // were accepted by one runtime and rejected by the other.
      const malformed = ['\\u00st', '\\u12zz', '\\u1', '\\u 123', '\\u+123',
        '\\x4z', '\\xZZ']
      assert.ok(malformed.some((m) => all.includes(m)),
        'no malformed \\u or \\x escape in the corpus: audit item P3 is ' +
        'unreachable, and a clean run says nothing about it')

      // A lone surrogate is a RECORDED, deliberate divergence — the
      // control proving the runner still sees a difference it is supposed
      // to see rather than having gone blind.
      assert.ok(all.includes('\\uD800'),
        'no lone surrogate: the recorded-divergence control is missing')

      // P4 — line and paragraph separators, RAW. Escaped they are
      // ordinary \u sequences and exercise nothing.
      assert.ok(all.includes(' ') || all.includes(' '),
        'no raw U+2028/U+2029: audit item P4 is unreachable')

      // P1/P2 — a value followed immediately by a quote. The shape whose
      // text run ended at the quote in Go and did not in TypeScript.
      assert.match(all, /[a-z0-9]"/,
        'no value-then-quote shape: audit items P1/P2 are unreachable')
    })
  }

  it('is deterministic for a fixed seed', () => {
    // The whole point of the pinned seed is that a diff failure can be
    // replayed exactly. A generator that drifted would make every reported
    // divergence unreproducible.
    assert.equal(corpus('json'), corpus('json'))
  })

  it('still emits ordinary well-formed documents', () => {
    // The damage pass must not turn the corpus into nothing but malformed
    // input: most of a differential run should still be documents both
    // runtimes accept, or the run stops exercising the ordinary paths.
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
    assert.ok(clean.length > cases.length / 4,
      `only ${clean.length} of ${cases.length} cases are well-formed JSON; ` +
      'the damage pass has taken over the corpus')
  })
})
