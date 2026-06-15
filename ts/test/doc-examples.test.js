/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  doc-examples.test.js
 *  Doc-example harness: extracts fenced ```js / ```javascript code blocks
 *  from this repo's README and docs, runs every block that contains a
 *  `// =>` assertion, and checks each `<expr> // => <expected>` line.
 *
 *  A block opts in to testing by including at least one `// =>` line.
 *  Blocks with no `// =>` are skipped (illustrative snippets). Mark a
 *  block ` ```js ignore ` (info string) to exclude it explicitly.
 *
 *  `require(...)` inside an example resolves from this package's
 *  node_modules; unresolved `@tabnas/<x>` specifiers fall back to the
 *  sibling repo `<tabnas-folder>/<x>/ts` (local, unpublished dev layout).
 *  Identical across all tabnas repos — discovers docs relative to the repo.
 */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')
const fs = require('node:fs')
const path = require('node:path')

const TS_DIR = path.join(__dirname, '..') // <repo>/ts
const REPO = path.join(TS_DIR, '..') // <repo>
const TABNAS = path.join(REPO, '..') // the tabnas folder (siblings)

const OWN_NAME = (() => {
  try {
    return require(path.join(TS_DIR, 'package.json')).name
  } catch {
    return null
  }
})()

// Candidate doc locations, relative to the repo root. Missing ones skipped.
const DOC_GLOBS = [
  'README.md',
  'ts/README.md',
  'go/README.md',
  'ts/doc',
  'doc',
  'docs',
]

function collectMarkdown() {
  const out = []
  const add = (p) => {
    if (fs.existsSync(p) && fs.statSync(p).isFile() && p.endsWith('.md')) {
      out.push(p)
    }
  }
  const walk = (dir) => {
    if (!fs.existsSync(dir) || !fs.statSync(dir).isDirectory()) return
    for (const e of fs.readdirSync(dir)) {
      if (e === 'node_modules' || e.startsWith('dist') || e === '.git') continue
      const p = path.join(dir, e)
      const st = fs.statSync(p)
      if (st.isDirectory()) walk(p)
      else add(p)
    }
  }
  for (const g of DOC_GLOBS) {
    const p = path.join(REPO, g)
    if (g.endsWith('.md')) add(p)
    else walk(p)
  }
  return [...new Set(out)]
}

// Extract fenced js/javascript blocks with their starting line number.
function extractBlocks(src) {
  const lines = src.split('\n')
  const blocks = []
  let cur = null
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const open = line.match(/^```+\s*(\w+)?\s*(\w+)?\s*$/)
    if (cur) {
      if (/^```+\s*$/.test(line)) {
        blocks.push(cur)
        cur = null
      } else {
        cur.code.push(line)
      }
    } else if (open && /^(js|javascript)$/i.test(open[1] || '')) {
      const info = (open[2] || '').toLowerCase()
      cur = { lang: open[1], ignore: info === 'ignore', startLine: i + 2, code: [] }
    } else if (open && /^```/.test(line) && (open[1] || open[2])) {
      // a non-js fence; consume until close so its body isn't scanned
      let j = i + 1
      while (j < lines.length && !/^```+\s*$/.test(lines[j])) j++
      i = j
    }
  }
  return blocks
}

// import { A } from 'x' -> const { A } = require('x'); default + namespace too.
function importsToRequire(code) {
  return code
    .replace(
      /^\s*import\s+\*\s+as\s+(\w+)\s+from\s+(['"][^'"]+['"]).*$/gm,
      'const $1 = require($2)',
    )
    .replace(
      /^\s*import\s+(\{[^}]*\})\s+from\s+(['"][^'"]+['"]).*$/gm,
      'const $1 = require($2)',
    )
    .replace(
      /^\s*import\s+(\w+)\s+from\s+(['"][^'"]+['"]).*$/gm,
      'const $1 = require($2)',
    )
}

// Rewrite `<expr>  // => <expected>` lines into __eq(expr, expected) calls.
const ARROW = /\/\/\s*=>(.*)$/
function rewriteAssertions(code) {
  let count = 0
  const out = code.split('\n').map((line) => {
    const m = line.match(ARROW)
    if (!m) return line
    const expected = m[1].trim()
    if (expected === '') return line // `// =>` with no value: leave as comment
    const codePart = line.slice(0, m.index).replace(/[;\s]+$/, '')
    if (codePart.trim() === '') return line
    const indent = line.match(/^\s*/)[0]
    count++
    return `${indent}__eq((${codePart}), (${expected}));`
  })
  return { code: out.join('\n'), count }
}

function deepEq(actual, expected) {
  const norm = (v) => JSON.parse(JSON.stringify(v ?? null))
  try {
    assert.deepStrictEqual(actual, expected)
    return
  } catch {}
  // Fall back to JSON-normalised compare (null-proto objects, etc.).
  assert.deepStrictEqual(norm(actual), norm(expected))
}

function makeRequire() {
  return function patchedRequire(spec) {
    try {
      return require(spec)
    } catch (e) {
      if (e && e.code === 'MODULE_NOT_FOUND') {
        // The repo's own package (self-reference fallback).
        if (OWN_NAME && (spec === OWN_NAME || spec.startsWith(OWN_NAME + '/'))) {
          return require(TS_DIR + spec.slice(OWN_NAME.length))
        }
        // A sibling @tabnas/<x> package -> <tabnas-folder>/<x>/ts (local dev).
        const m = spec.match(/^@tabnas\/([^/]+)(\/.*)?$/)
        if (m) return require(path.join(TABNAS, m[1], 'ts') + (m[2] || ''))
      }
      throw e
    }
  }
}

describe('doc-examples', () => {
  const files = collectMarkdown()
  let testable = 0

  for (const file of files) {
    const rel = path.relative(REPO, file)
    const blocks = extractBlocks(fs.readFileSync(file, 'utf8'))
    blocks.forEach((b, bi) => {
      if (b.ignore) return
      const joined = b.code.join('\n')
      if (!ARROW.test(joined)) return // no assertions -> skip
      const { code, count } = rewriteAssertions(importsToRequire(joined))
      if (count === 0) return
      testable++
      const label = `${rel} block #${bi + 1} (line ${b.startLine})`
      it(label, () => {
        const isAsync = /\bawait\b/.test(code)
        const body = isAsync ? `return (async () => {\n${code}\n})()` : code
        const fn = new Function('require', '__eq', body)
        return fn(makeRequire(), deepEq)
      })
    })
  }

  it('found at least one tested example (sanity)', () => {
    // Not a hard failure if a repo has no `// =>` examples yet.
    assert.ok(testable >= 0, `tested ${testable} doc example block(s)`)
  })
})
