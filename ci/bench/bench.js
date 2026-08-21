// bench.js — benchmark one parser against one fixture in a dedicated
// process (fresh V8 state per measurement; no cross-parser JIT pollution).
//
// Usage: node bench.js <parser> <fixture-path> [iters] [warmup]
//   parser: json | jsonic | native
// Emits one JSON line: median/p5/p95 ms, MB/s.
//
// Sibling layout: json and jsonic checkouts next to this repo's root
// (override with TABNAS_ROOT).
'use strict'
const fs = require('fs')
const path = require('path')

const which = process.argv[2]
const fixture = process.argv[3]
const iters = Number(process.argv[4] || 30)
const warmup = Number(process.argv[5] || 15)

const ROOT = process.env.TABNAS_ROOT || path.resolve(__dirname, '../../..')
const src = fs.readFileSync(fixture, 'utf8')
const byteLen = Buffer.byteLength(src, 'utf8')

let parse
if ('native' === which) {
  parse = (s) => JSON.parse(s)
} else if ('json' === which) {
  const j = require(path.join(ROOT, 'json/ts'))
  const inst = j.make()
  parse = (s) => inst.parse(s)
} else if ('jsonic' === which) {
  const { Jsonic } = require(path.join(ROOT, 'jsonic/ts'))
  const inst = Jsonic.make()
  parse = (s) => inst(s)
} else {
  console.error('unknown parser: ' + which)
  process.exit(2)
}

for (let i = 0; i < warmup; i++) parse(src)

const times = []
for (let i = 0; i < iters; i++) {
  const t0 = process.hrtime.bigint()
  parse(src)
  times.push(Number(process.hrtime.bigint() - t0) / 1e6)
}
times.sort((a, b) => a - b)
const q = (p) => times[Math.min(times.length - 1, Math.floor(p * times.length))]
const median = q(0.5)

console.log(JSON.stringify({
  parser: which,
  fixture: path.basename(fixture),
  // BYTES, not UTF-16 code units. `src.length` is the same number for an
  // ASCII fixture, which is why this went unnoticed while every fixture
  // was ASCII — and is 33% short on records-cjk-1mb.json, whose 1,048,816
  // bytes are 706,226 code units. The Go harness reports MB/s from a byte
  // count, so leaving this would make the two runtimes' throughput on the
  // one non-ASCII arm silently incomparable.
  bytes: byteLen,
  iters,
  median_ms: +median.toFixed(3),
  p5_ms: +q(0.05).toFixed(3),
  p95_ms: +q(0.95).toFixed(3),
  mb_per_s: +(byteLen / 1048576 / (median / 1000)).toFixed(2),
}))
