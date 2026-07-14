// genfixture.js — deterministic benchmark fixture generator.
//
// Fixtures are NOT checked in: they are regenerated at bench time from a
// pinned seed, so the TS and Go benchmarks always read identical bytes
// without bloating the repo.
//
// Usage: node genfixture.js <out-dir>
'use strict'
const fs = require('fs')
const path = require('path')

const SEED = 20260714 // pinned; change deliberately only
const outDir = process.argv[2]
if (!outDir) {
  console.error('usage: node genfixture.js <out-dir>')
  process.exit(2)
}
fs.mkdirSync(outDir, { recursive: true })

// mulberry32 — small deterministic PRNG.
function prng(seed) {
  let a = seed >>> 0
  return function () {
    a |= 0; a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const WORDS = ['alpha', 'beta', 'gamma', 'delta', 'epsilon', 'zeta',
  'server', 'client', 'active', 'weight', 'value', 'config']

function str(r, escapes) {
  const n = 3 + Math.floor(r() * 12)
  let s = ''
  for (let i = 0; i < n; i++) {
    if (escapes && r() < 0.08) {
      s += ['\\n', '\\t', '\\"', '\\\\', '\\u00e9', '\\ud83d\\ude00'][Math.floor(r() * 6)]
    } else {
      s += WORDS[Math.floor(r() * WORDS.length)][0]
        + Math.floor(r() * 100)
    }
  }
  return s
}

function num(r) {
  const k = r()
  if (k < 0.4) return String(Math.floor(r() * 100000))
  if (k < 0.7) return (r() * 1000).toFixed(3)
  if (k < 0.85) return String(-Math.floor(r() * 5000))
  return (r() * 1e-4).toExponential(4).replace('e', 'E')
}

// One record with REPEATED keys — the dominant real-world JSON shape
// (arrays of records), and what the Go intern pool targets.
function record(r, i, escapes) {
  return `{"id":${i},"name":"${str(r, escapes)}","host":"h-${i % 64}.example.com",` +
    `"port":${1024 + (i % 40000)},"active":${0 === i % 3},"weight":${num(r)},` +
    `"tags":["${str(r, escapes)}","${str(r, escapes)}"],"note":null}`
}

function genRecords(sizeBytes, escapes, seedAdj) {
  const r = prng(SEED + seedAdj)
  const parts = []
  let n = 0, i = 0
  while (n < sizeBytes) {
    const rec = record(r, i++, escapes)
    parts.push(rec)
    n += rec.length + 1
  }
  return '[' + parts.join(',') + ']'
}

function genNumbers(sizeBytes) {
  const r = prng(SEED + 3)
  const parts = []
  let n = 0
  while (n < sizeBytes) {
    const v = num(r)
    parts.push(v)
    n += v.length + 1
  }
  return '[' + parts.join(',') + ']'
}

function genNested(depth) {
  let s = '1'
  for (let i = 0; i < depth; i++) s = (0 === i % 2) ? `[${s},2]` : `{"a":${s}}`
  return s
}

// Relaxed-jsonic shape: unquoted keys/values, comments — the input class
// the Go text-table scan targets. Only meaningful for the jsonic parser.
function genJsonicText(sizeBytes) {
  const r = prng(SEED + 4)
  const parts = []
  let n = 0, i = 0
  while (n < sizeBytes) {
    const line = `entry${i}: { host: server-${i}.example.com, port: ${1024 + (i % 40000)},` +
      ` active: ${0 === i % 3}, tags: [${str(r)} ${str(r)} tag-${i}] } // node ${i}`
    parts.push(line)
    n += line.length + 1
    i++
  }
  return parts.join('\n')
}

const KB = 1024, MB = 1024 * KB
const fixtures = {
  'records-16kb.json': genRecords(16 * KB, false, 1),
  'records-1mb.json': genRecords(1 * MB, false, 1),
  'records-escaped-1mb.json': genRecords(1 * MB, true, 2),
  'numbers-1mb.json': genNumbers(1 * MB),
  'nested-256.json': genNested(256),
  'padded-tiny.json': '{"a":1}' + ' '.repeat(10000),
  'text-1mb.jsonic': genJsonicText(1 * MB),
}

for (const [name, content] of Object.entries(fixtures)) {
  fs.writeFileSync(path.join(outDir, name), content)
  console.log(`${name}\t${content.length} bytes`)
}
