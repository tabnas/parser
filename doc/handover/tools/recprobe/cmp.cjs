// usage: node cmp.cjs <tag> [--position] [--relex]; corpus = gen1(6000)+gen2(6000)
const { spawnSync } = require('child_process')
const fs = require('fs')
const S = '/tmp/claude-0/-home-user/b2a7b8ed-3a32-5059-b784-facb325d2e7b/scratchpad'
const NM = '/home/user/css/ts/node_modules/'
const { Tabnas } = require(NM + '@tabnas/parser')
const { jsonic } = require(NM + '@tabnas/jsonic')
const { Css } = require('/home/user/css/ts/dist/css.js')
const pos = process.argv.includes('--position'), plainm = process.argv.includes('--plain'), relex = process.argv.includes('--relex') || plainm
const inputs = require(S + '/hunt/gen1.cjs')(6000).concat(require(S + '/hunt/gen2.cjs')(6000))
function plain(v, seen = []) {
  if (null === v || 'object' !== typeof v) return v
  if (seen.includes(v)) return '[CIRCULAR]'
  const s2 = seen.concat([v])
  if (Array.isArray(v)) return v.map((x) => plain(x, s2))
  const out = {}
  for (const k of Object.keys(v)) if (undefined !== v[k]) out[k] = plain(v[k], s2)
  return out
}
const opts = plainm ? {} : relex ? { lex: { relex: true } } : { parse: { recover: { enabled: true } } }
const p = new Tabnas(opts).use(jsonic).use(Css, { position: pos })
console.error('ts: parsing ' + inputs.length)
const ts = inputs.map((src) => {
  try {
    const r = p.parse(src)
    if (relex) return { value: plain(r) ?? null, errors: [] }
    return { value: plain(r.value) ?? null, errors: (r.errors || []).map((e) => [e.code, e.lineNumber, e.columnNumber]) }
  } catch (e) {
    return { value: null, errors: [[e.code, e.lineNumber, e.columnNumber]] }
  }
})
console.error('rs: parsing')
const args = [pos ? '--position' : '', plainm ? '--plain' : relex ? '--relex' : ''].filter(Boolean)
const r = spawnSync(S + '/recprobe/target/release/recprobe', args, { input: inputs.map((s) => Buffer.from(s, 'utf8').toString('hex')).join('\n') + '\n', maxBuffer: 1 << 30 })
const rs = r.stdout.toString().split('\n')
let vd = 0, ed = 0, cyc = 0, dup = 0
const ex = { value: [], errors: [] }
for (let i = 0; i < inputs.length; i++) {
  const t = ts[i], q = JSON.parse(rs[i])
  const tv = JSON.stringify(t.value), qv = JSON.stringify(q.value)
  if (tv !== qv) { vd++; if (tv.includes('[CIRCULAR]')) cyc++; else if (ex.value.length < 12) ex.value.push({ src: inputs[i], ts: tv, rs: qv }) }
  else if (JSON.stringify(t.errors) !== JSON.stringify(q.errors)) {
    ed++
    const qe = q.errors.filter((e, j) => 0 === j || JSON.stringify(e) !== JSON.stringify(q.errors[j - 1]))
    if (JSON.stringify(t.errors) === JSON.stringify(qe)) dup++
    else if (ex.errors.length < 12) ex.errors.push({ src: inputs[i], ts: t.errors, rs: q.errors })
  }
}
console.log(JSON.stringify({ mode: args.join(' ') || 'default', inputs: inputs.length, valueDiffs: vd, ofWhichTsCycle: cyc, errorOnlyDiffs: ed, ofWhichOnlyAdjacentDuplicates: dup }))
fs.writeFileSync(S + '/recprobe/ex-' + process.argv[2] + '.json', JSON.stringify(ex, null, 1))
