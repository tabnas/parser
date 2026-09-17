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

// Literal non-ASCII, NOT \uXXXX escapes — an escape sequence is ASCII
// bytes and exercises the escape decoder, not the non-ASCII scan path.
// Every fixture in this matrix was ASCII, which left that path unmeasured:
// ScanSpec.fallback runs per non-ASCII character and never fires on ASCII.
// Three bytes per character in UTF-8, two UTF-16 units for none of these
// (all BMP), so this isolates "not ASCII" from "not BMP".
const CJK_WORDS = ['設定', '名前', 'サーバ', '接続', '値', '重み',
  '有効', '記録', '配列', '文字列', '数値', '状態']

function str(r, escapes, cjk) {
  const n = 3 + Math.floor(r() * 12)
  let s = ''
  for (let i = 0; i < n; i++) {
    if (escapes && r() < 0.08) {
      s += ['\\n', '\\t', '\\"', '\\\\', '\\u00e9', '\\ud83d\\ude00'][Math.floor(r() * 6)]
    } else if (cjk) {
      s += CJK_WORDS[Math.floor(r() * CJK_WORDS.length)]
        + Math.floor(r() * 100)
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
function record(r, i, escapes, cjk) {
  return `{"id":${i},"name":"${str(r, escapes, cjk)}","host":"h-${i % 64}.example.com",` +
    `"port":${1024 + (i % 40000)},"active":${0 === i % 3},"weight":${num(r)},` +
    `"tags":["${str(r, escapes, cjk)}","${str(r, escapes, cjk)}"],"note":null}`
}

function genRecords(sizeBytes, escapes, seedAdj, cjk) {
  const r = prng(SEED + seedAdj)
  const parts = []
  let n = 0, i = 0
  while (n < sizeBytes) {
    const rec = record(r, i++, escapes, cjk)
    parts.push(rec)
    // Bytes, not UTF-16 units: a CJK record is ~3x its .length, so
    // counting characters would emit a ~3 MB "1 MB" fixture. Identical
    // to .length for the ASCII fixtures, which are unchanged.
    n += Buffer.byteLength(rec) + 1
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

// ---------------------------------------------------------------------
// Pathology fixtures.
//
// The matrix above is all "ordinary document, made large". These are the
// shapes that historically break parsers, and the two quadratics found in
// this engine were both of this kind: a copy per element (Go `@push$`) and
// a copy per nesting level (Rust value containers). Neither showed up on an
// ordinary document, because ordinary documents are wide AND shallow AND
// have small tokens all at once, so no single axis gets far enough to
// separate O(n) from O(n^2).
//
// Each fixture below takes ONE axis a long way and leaves the rest small.

// One object with very many keys. `records-*` nests many SMALL maps, so a
// cost per key paid against the whole map -- a copy-on-write container
// that turns out to be shared, say -- averages out to nothing there.
function genWideObject(keys) {
  const parts = []
  for (let i = 0; i < keys; i++) parts.push(`"k${i}":${i}`)
  return '{' + parts.join(',') + '}'
}

// One enormous token. Exercises the lexer's per-character accumulation on
// its own: no structure to build, one value to return.
function genLongString(chars) {
  return '["' + 'a'.repeat(chars) + '"]'
}

// Numbers built to be awkward rather than large: long digit runs, long
// fraction tails, and exponents genuinely outside f64, which a parser has
// to scan in full before it can saturate them.
//
// UNDERFLOW rather than overflow, deliberately, and the asymmetry is not
// this file's. `1e400` saturates to Infinity in the TypeScript and Rust
// arms and is REJECTED in the Go one: tabnas/json holds each runtime to
// its own platform oracle, and `encoding/json` fails on an overflowing
// literal where `JSON.parse` saturates, so the Go plugin excludes it by
// design. One fixture feeds all three arms, so nothing the Go arm rejects
// can be in it. Underflow all three accept, and it reaches the same
// saturating conversion.
function genNumericEdge(sizeBytes) {
  const parts = []
  let n = 0, i = 0
  while (n < sizeBytes) {
    const v = [
      '9'.repeat(40 + (i % 60)),
      '0.' + '1'.repeat(30 + (i % 40)),
      // Far below the smallest subnormal (~4.9e-324), so the conversion
      // has to consume the whole literal to arrive at zero.
      '1e-' + (400 + (i % 9) * 400),
      // Straddling the subnormal boundary: -1e-318 is representable,
      // -1e-324 and below are not, so consecutive elements take the two
      // different paths out of the same conversion.
      '-1e-' + (318 + (i % 9)),
      '1' + '0'.repeat(25 + (i % 30)),
    ][i % 5]
    parts.push(v)
    n += v.length + 1
    i++
  }
  return '[' + parts.join(',') + ']'
}

// A tiny document buried in separators. `padded-tiny` pads the END, which
// the lexer reaches once; this puts the run BETWEEN two tokens, where the
// skip loop runs with a rule half-matched and the stack live.
function genSeparators(sizeBytes) {
  const run = ' \n\t'.repeat(Math.ceil(sizeBytes / 3))
  return '{"a":' + run + '1}'
}

// The smallest useful document. `padded-tiny` is 10 KB of padding, so it
// measures the skip loop; this measures per-parse FIXED cost and nothing
// else -- grammar install is amortised by the harness, so what is left is
// what every call pays however small the input.
const TINY = '{"a":1}'

// Relaxed-jsonic input that keeps the alternate selector guessing: every
// line opens looking like an implicit map and only resolves at its end.
// Ambiguity is the classic parser pathology and nothing else in this
// matrix has any -- strict JSON is unambiguous by construction, so the
// alternate retry and relex paths are unmeasured without this.
function genAmbiguous(sizeBytes) {
  const parts = []
  let n = 0, i = 0
  while (n < sizeBytes) {
    // `a b c: 1` reads as an implicit map only once the colon arrives;
    // `a b c d` is an implicit list. Alternating the two keeps any
    // first-match cache cold.
    const line = 0 === i % 2
      ? `k${i} v${i} w${i}: ${i}`
      : `k${i} v${i} w${i} x${i}`
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
  // The non-ASCII arm of the matrix. Everything else here is ASCII, so
  // the per-character scan fallback never ran under any measurement.
  'records-cjk-1mb.json': genRecords(1 * MB, false, 5, true),
  'text-1mb.jsonic': genJsonicText(1 * MB),

  // Pathologies: one axis each, everything else small. See above.
  'wide-40k.json': genWideObject(40000),
  'longstring-1mb.json': genLongString(1 * MB),
  'numeric-edge-1mb.json': genNumericEdge(1 * MB),
  'separators-1mb.json': genSeparators(1 * MB),
  'tiny.json': TINY,
  'ambiguous-1mb.jsonic': genAmbiguous(1 * MB),
}

for (const [name, content] of Object.entries(fixtures)) {
  fs.writeFileSync(path.join(outDir, name), content)
  // Byte length, not .length: they differ for the non-ASCII fixture, and
  // bytes are what the throughput numbers are per.
  console.log(`${name}\t${Buffer.byteLength(content)} bytes`)
}
