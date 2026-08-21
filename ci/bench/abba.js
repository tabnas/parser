// abba.js — paired A/B/B/A comparison of two engine builds in ONE process.
//
// The decision instrument for engine performance claims. It exists because
// the obvious approach — time build A, then time build B — cannot tell an
// effect from a slot artifact: measured on BYTE-IDENTICAL builds, whichever
// build sat in the second slot won 5-9 rounds out of 12 and the "delta"
// ranged over 3.25 percentage points. See doc/rust-port-implementation-plan.md
// Phase 1 and doc/engine-changes-for-portability.md section 2.3.
//
// So this reports a comparison; it does NOT pronounce a verdict. The verdict
// needs this run PLUS the same run with the builds swapped: a real effect
// reverses sign when the slots reverse, a slot artifact does not.
// ab-compare.sh drives all three runs (forward, reverse, null) and decides.
//
// Both builds are loaded in one process, from different paths, so V8 sees
// two distinct module graphs. Each build supplies its own strict-JSON test
// grammar (dist-test/json-plugin.js), so no downstream checkout is involved
// and nothing but the engine differs between the two sides.
//
// Usage:
//   node abba.js --a <ts-dir> --b <ts-dir> --fixture <path>
//                [--rounds 12] [--inner 3] [--warmup 5] [--label name]
// Emits one JSON line.
'use strict'
const fs = require('fs')
const path = require('path')

function arg(name, dflt) {
  const i = process.argv.indexOf('--' + name)
  return i < 0 ? dflt : process.argv[i + 1]
}

const aDir = arg('a'), bDir = arg('b'), fixture = arg('fixture')
const rounds = Number(arg('rounds', 12))
const inner = Number(arg('inner', 3))
const warmup = Number(arg('warmup', 5))
const label = arg('label', 'ab')

if (!aDir || !bDir || !fixture) {
  console.error('usage: node abba.js --a <ts-dir> --b <ts-dir> --fixture <path>')
  process.exit(2)
}

const src = fs.readFileSync(fixture, 'utf8')

// One parse function per build: its engine, its grammar, nothing shared.
function load(tsDir) {
  const enginePath = path.resolve(tsDir, 'dist/tabnas.js')
  const pluginPath = path.resolve(tsDir, 'dist-test/json-plugin.js')
  for (const p of [enginePath, pluginPath]) {
    if (!fs.existsSync(p)) {
      console.error('abba: missing ' + p + ' — build that tree first')
      process.exit(2)
    }
  }
  const { Tabnas } = require(enginePath)
  const plugin = require(pluginPath)
  const make = plugin.json || plugin.default || Object.values(plugin)[0]
  const inst = new Tabnas().use(make)
  return (s) => inst.parse(s)
}

const parseA = load(aDir)
const parseB = load(bDir)

// Guard: the two builds must agree on the answer, or the comparison is
// between two different programs and the timing is meaningless.
const outA = JSON.stringify(parseA(src))
const outB = JSON.stringify(parseB(src))
if (outA !== outB) {
  console.error('abba: builds disagree on the parse result — refusing to time them')
  process.exit(3)
}

// min-of-inner: the fastest run is the one least disturbed by the machine.
// Reported alongside a GC-inclusive total, because min systematically
// EXCLUDES collection pauses — which is the one channel an allocation
// change moves, and reporting min alone once inverted a verdict.
function block(parse) {
  let best = Infinity, total = 0
  for (let i = 0; i < inner; i++) {
    const t0 = process.hrtime.bigint()
    parse(src)
    const ms = Number(process.hrtime.bigint() - t0) / 1e6
    if (ms < best) best = ms
    total += ms
  }
  return { best, total }
}

for (let i = 0; i < warmup; i++) { parseA(src); parseB(src) }

const dMin = [], dTot = []
let bWins = 0
for (let r = 0; r < rounds; r++) {
  // A,B,B,A — B sits in both an early and a late slot, and so does A, so
  // any drift over the round cancels instead of accruing to one side.
  const a1 = block(parseA)
  const b1 = block(parseB)
  const b2 = block(parseB)
  const a2 = block(parseA)

  const aBest = Math.min(a1.best, a2.best)
  const bBest = Math.min(b1.best, b2.best)
  const aTotal = a1.total + a2.total
  const bTotal = b1.total + b2.total

  const dm = (bBest - aBest) / aBest * 100
  const dt = (bTotal - aTotal) / aTotal * 100
  dMin.push(dm)
  dTot.push(dt)
  if (dm < 0) bWins++
}

const median = (xs) => {
  const s = [...xs].sort((x, y) => x - y)
  const m = s.length >> 1
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2
}
const round2 = (x) => Math.round(x * 100) / 100

console.log(JSON.stringify({
  label,
  fixture: path.basename(fixture),
  rounds, inner,
  d_min_median: round2(median(dMin)),
  d_total_median: round2(median(dTot)),
  b_wins: bWins,
  // Negative means B (the --b tree) was faster.
  d_min_each: dMin.map(round2),
}))
