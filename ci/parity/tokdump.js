// tokdump.js — dump the token stream the parser consumes, one flat
// record per line, for cross-runtime differential testing (the Go
// counterpart is ./gotokdump). Byte-identical output from both dumpers
// for the same input pins the lexer contract across runtimes.
//
// Record: NAME \t sI \t rI \t json(src) \t VALREP
//   (cI is deliberately NOT recorded: column semantics differ between
//   the runtimes by documented design — TS counts UTF-16 units, Go
//   counts runes; see parser/go/doc/differences.md. sI, normalized to
//   UTF-16 units on the Go side, pins positions exactly.)
//   VALREP for value-bearing kinds (#ST #TX #NR #VL):
//     numbers  -> num:<16-hex float64 bit pattern>  (exact, no float
//                 formatting divergence between JS and Go)
//     strings  -> str:<json>
//     booleans -> bool:true|false
//     null     -> null, undefined -> undef
//   all other kinds -> "-"
// A FAILED parse emits exactly one record: ERROR \t <code>. Token
// delivery on the error path differs between the engines by documented
// design in three ways (TS delivers the #BD token then throws where Go
// substitutes #ZZ and returns; the end token is re-delivered during
// rule-stack wind-down; TS's trailing-content probe delivers one token
// past the last consumed one) — the engines' cross-runtime contract for
// errored inputs is the error CODE, which the fixture suites also pin.
// Successful parses are compared token-for-token.
//
// Only tokens the parser CONSUMES are dumped (space/line/comment are
// filtered): the Go engine's public Sub contract fires after IGNORE
// skipping, so consumed tokens are the cross-runtime comparable stream.
// The end token is recorded once (re-delivery count during rule-stack
// wind-down is engine-internal).
//
// Usage: node tokdump.js <json|jsonic> <input-file-or-dir>
//   Directory mode processes every *.in file (sorted), separating
//   sections with "== <basename>" lines.
'use strict'
const fs = require('fs')
const path = require('path')

const grammar = process.argv[2]
const target = process.argv[3]
const ROOT = process.env.TABNAS_ROOT || path.resolve(__dirname, '../../..')

let inst
if ('json' === grammar) {
  inst = require(path.join(ROOT, 'json/ts')).make()
} else if ('jsonic' === grammar) {
  inst = require(path.join(ROOT, 'jsonic/ts')).Jsonic.make()
} else {
  console.error('usage: node tokdump.js <json|jsonic> <input-file-or-dir>')
  process.exit(2)
}

const IGNORE = { '#SP': 1, '#LN': 1, '#CM': 1 }
const VALKIND = { '#ST': 1, '#TX': 1, '#NR': 1, '#VL': 1 }

const f64 = new DataView(new ArrayBuffer(8))
function valrep(name, val) {
  if (1 !== VALKIND[name]) return '-'
  if (undefined === val) return 'undef'
  if (null === val) return 'null'
  const t = typeof val
  if ('number' === t) {
    f64.setFloat64(0, val)
    const hi = f64.getUint32(0).toString(16).padStart(8, '0')
    const lo = f64.getUint32(4).toString(16).padStart(8, '0')
    return 'num:' + hi + lo
  }
  if ('string' === t) return 'str:' + JSON.stringify(val)
  if ('boolean' === t) return 'bool:' + val
  return 'other:' + String(val)
}

let out = []
let ended = false
inst.sub({
  lex: (tkn) => {
    if (1 === IGNORE[tkn.name]) return
    if ('#ZZ' === tkn.name) {
      if (ended) return
      ended = true
    }
    out.push([
      tkn.name, tkn.sI, tkn.rI,
      JSON.stringify(tkn.src), valrep(tkn.name, tkn.val),
    ].join('\t'))
  },
})

function dump(src) {
  out = []
  ended = false
  try {
    'json' === grammar ? inst.parse(src) : inst(src)
  } catch (e) {
    out = ['ERROR\t' + (e.code || 'unknown')]
  }
  return out.join('\n')
}

const chunks = []
if (fs.statSync(target).isDirectory()) {
  for (const f of fs.readdirSync(target).filter((f) => f.endsWith('.in')).sort()) {
    chunks.push('== ' + f)
    chunks.push(dump(fs.readFileSync(path.join(target, f), 'utf8')))
  }
} else {
  chunks.push(dump(fs.readFileSync(target, 'utf8')))
}
process.stdout.write(chunks.join('\n') + '\n')
