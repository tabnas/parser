// gencorpus.js — seeded random-document generator biased toward grammar
// edges: escape sequences (incl. surrogate pairs and \u{...}), exotic
// number forms, deep nesting, and (in jsonic mode) comments, unquoted
// keys, and trailing commas. Feeds the cross-runtime diff runner
// (run-diff.sh); the pinned seed makes corpora reproducible, so a diff
// failure can be replayed exactly.
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

function jstr(r, depthBias) {
  let s = '"'
  const n = Math.floor(r() * 10)
  for (let i = 0; i < n; i++) {
    s += r() < 0.35 ? ESCS[Math.floor(r() * ESCS.length)]
      : String.fromCharCode(97 + Math.floor(r() * 26))
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
  let doc = value(r, 0)
  if ('jsonic' === mode) doc = relax(r, doc)
  fs.writeFileSync(path.join(outDir, `case-${String(i).padStart(5, '0')}.in`), doc)
}
console.log(`gencorpus: ${count} ${mode} cases (seed ${seed}) -> ${outDir}`)
