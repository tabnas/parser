// Builds the tabnas peer-dependency page from graph.json, shas.txt and read-at.txt, with the
// changes since the previous read from prev/ and changes.json, and npm's own manifests for the
// same versions from npm-manifests.json.
'use strict'
const fs = require('fs')

const g = JSON.parse(fs.readFileSync('graph.json', 'utf8'))
const prevG = JSON.parse(fs.readFileSync('prev/graph.json', 'utf8'))
const npmManifests = JSON.parse(fs.readFileSync('npm-manifests.json', 'utf8'))
const changes = JSON.parse(fs.readFileSync('changes.json', 'utf8'))
// Every version comes from the package's own package.json on main, like every edge.
const ver = Object.fromEntries(g.packages.filter((p) => p.name).map((p) => [p.name, p.version]))
const prevVer = Object.fromEntries(prevG.packages.filter((p) => p.name).map((p) => [p.name, p.version]))
const readShas = (f) => Object.fromEntries(fs.readFileSync(f, 'utf8').trim().split('\n').map((l) => l.split(' ')))
const shas = readShas('shas.txt')

const short = (n) => n.replace('@tabnas/', '')
const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
const floor = (r) => (r && r !== '>=0' ? r.replace('>=', '≥') : '')
const joinNames = (xs) => (xs.length < 2 ? xs.join('') : xs.slice(0, -1).join(', ') + ' and ' + xs[xs.length - 1])
const code = (x) => `<code>${esc(x)}</code>`
const WORDS = ['no', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten', 'eleven', 'twelve']
const word = (n) => WORDS[n] || String(n)
const Word = (n) => { const w = word(n); return w[0].toUpperCase() + w.slice(1) }

const PARSER = '@tabnas/parser'
const tabnasPeers = (p) => Object.keys(p.peers).filter((d) => d.startsWith('@tabnas/'))
const isOpt = (p, d) => p.peerOptional.includes(d)

// The graph a set of package.json files describes. Depth counts required peers only: the engine
// is depth 0, and a package is one deeper than its deepest required peer, so a package with no
// required peer but the engine, or none at all, is depth 1. An optional peer is not installed
// with the package, so it adds no depth.
function model(gr) {
  const published = gr.packages.filter((p) => p.name && p.name.startsWith('@tabnas/') && !p.private)
  const inGraph = published.filter((p) => p.name !== PARSER && tabnasPeers(p).length)
  const byShort = new Map(inGraph.map((p) => [short(p.name), p]))
  const req = new Map(), opt = new Map(), engine = new Map()
  for (const p of inGraph) {
    const n = short(p.name)
    for (const d of tabnasPeers(p)) if (d !== PARSER && !byShort.has(short(d))) throw new Error(`${n} peers on ${d}, which is outside the graph`)
    req.set(n, tabnasPeers(p).filter((d) => d !== PARSER && !isOpt(p, d)).map(short))
    opt.set(n, tabnasPeers(p).filter((d) => d !== PARSER && isOpt(p, d)).map(short))
    engine.set(n, !p.peers[PARSER] ? 'none' : isOpt(p, PARSER) ? 'optional' : 'direct')
  }
  const dMemo = new Map()
  const depth = (n) => {
    if (dMemo.has(n)) return dMemo.get(n)
    const ds = req.get(n) || []
    const v = 1 + (ds.length ? Math.max(...ds.map(depth)) : 0)
    dMemo.set(n, v)
    return v
  }
  // Whether installing the package installs the engine: it peers on it, or a required peer does.
  const nMemo = new Map()
  const needsEngine = (n) => {
    if (nMemo.has(n)) return nMemo.get(n)
    const v = engine.get(n) === 'direct' || (req.get(n) || []).some(needsEngine)
    nMemo.set(n, v)
    return v
  }
  return { published, inGraph, byShort, req, opt, engine, depth, needsEngine }
}

const M = model(g), P = model(prevG)
const { published, inGraph, byShort, depth } = M
const names = [...byShort.keys()]
const maxDepth = Math.max(...names.map(depth))
const seedOrder = ['alchemy', 'render', 'transduce', 'json', 'jsonl', 'jsonic', 'csv', 'xml', 'feed', 'expr', 'c', 'css', 'json5', 'jsonc',
  'toml', 'yaml', 'zon', 'hoover', 'ini', 'debug', 'jsonic-cli', 'directive', 'path', 'multisource', 'bnf', 'abnf', 'proto', 'semver',
  'ebnf', 'gbnf', 'support', 'mcp', 'railroad', 'markdown', 'chess', 'lsp']
const bySeed = (a, b) => (seedOrder.indexOf(a) + 1 || 999) - (seedOrder.indexOf(b) + 1 || 999) || a.localeCompare(b)
// The deepest required chain from a package down to the engine.
const chainFrom = (mdl, n) => { const out = [n]; let c = n; while ((mdl.req.get(c) || []).length) { const ds = mdl.req.get(c).slice().sort(bySeed); c = ds.reduce((b, d) => (mdl.depth(d) > mdl.depth(b) ? d : b), ds[0]); out.push(c) } return out }

// Layers, with a dummy node wherever an edge skips a layer, so long edges get a lane of their own.
const layers = Array.from({ length: maxDepth + 1 }, () => [])
const kind = new Map() // id -> 'real' | 'dummy'
for (const n of names) { layers[depth(n)].push(n); kind.set(n, 'real') }
// An edge's chain runs from the box whose left side its path leaves, the deeper end, to the box
// whose right side it enters. An optional peer can sit deeper than its dependent, since it adds
// no depth: that edge's chain is reversed and its arrowhead drawn at the start, and an optional
// peer at the dependent's own depth is a short loop beside the two boxes instead (same).
const edges = [] // { from, to, optional, range, chain, reversed, same }
function laneChain(a, b) {
  const chain = [a]
  for (let k = depth(a) - 1; k > depth(b); k--) {
    const id = `~${a}>${b}@${k}`
    layers[k].push(id); kind.set(id, 'dummy'); chain.push(id)
  }
  chain.push(b)
  return chain
}
for (const n of names) {
  const p = byShort.get(n)
  for (const d of M.req.get(n)) edges.push({ from: n, to: d, optional: false, range: p.peers['@tabnas/' + d], chain: laneChain(n, d) })
  for (const d of M.opt.get(n)) {
    const e = { from: n, to: d, optional: true, range: p.peers['@tabnas/' + d] }
    if (depth(n) > depth(d)) e.chain = laneChain(n, d)
    else if (depth(n) < depth(d)) { e.chain = laneChain(d, n); e.reversed = true }
    else { e.chain = [n, d]; e.same = true }
    edges.push(e)
  }
}
const drawnRequired = edges.filter((e) => !e.optional)
const drawnOptional = edges.filter((e) => e.optional)
// Segment adjacency between consecutive layers.
const left = new Map(), right = new Map() // left = towards parser (lower depth)
const addAdj = (m, k, v) => { if (!m.has(k)) m.set(k, []); m.get(k).push(v) }
for (const e of edges) if (!e.same) for (let i = 0; i + 1 < e.chain.length; i++) { addAdj(left, e.chain[i], e.chain[i + 1]); addAdj(right, e.chain[i + 1], e.chain[i]) }

// Seed order by family, then barycentre sweeps, keeping the order with the fewest crossings.
const seedRank = (id) => { const base = id.startsWith('~') ? id.slice(1).split('>')[0] : id; const i = seedOrder.indexOf(base); return i < 0 ? 999 : i + (id.startsWith('~') ? 0.5 : 0) }
for (const L of layers) L.sort((a, b) => seedRank(a) - seedRank(b))

const pos = () => { const m = new Map(); for (const L of layers) L.forEach((id, i) => m.set(id, i)); return m }
function crossings() {
  const p = pos(); let c = 0
  for (let d = 2; d <= maxDepth; d++) {
    const segs = []
    for (const u of layers[d]) for (const v of left.get(u) || []) segs.push([p.get(u), p.get(v)])
    for (let i = 0; i < segs.length; i++) for (let j = i + 1; j < segs.length; j++) if ((segs[i][0] - segs[j][0]) * (segs[i][1] - segs[j][1]) < 0) c++
  }
  return c
}
let best = layers.map((L) => [...L]), bestC = crossings()
for (let it = 0; it < 40; it++) {
  const downward = it % 2 === 0
  const range = downward ? [...Array(maxDepth - 1).keys()].map((i) => i + 2) : [...Array(maxDepth - 1).keys()].map((i) => maxDepth - 1 - i)
  for (const d of range) {
    const p = pos()
    const nb = downward ? left : right
    const bc = new Map(layers[d].map((id) => {
      const ns = nb.get(id) || []
      return [id, ns.length ? ns.reduce((s, x) => s + p.get(x), 0) / ns.length : p.get(id)]
    }))
    layers[d].sort((a, b) => bc.get(a) - bc.get(b) || seedRank(a) - seedRank(b))
  }
  const c = crossings()
  if (c < bestC) { bestC = c; best = layers.map((L) => [...L]) }
}
for (let d = 0; d <= maxDepth; d++) layers[d] = best[d]
// Nothing passes through depth 1 on its way to a node, so the packages nothing peers on, and
// the support–mcp pair, can fill the free band under json instead of hanging below the rest.
const moveAfter = (d, ids, afterId) => { const L = layers[d].filter((x) => !ids.includes(x)); L.splice(L.indexOf(afterId) + 1, 0, ...ids); layers[d] = L }
moveAfter(1, ['railroad', 'markdown', 'chess', 'lsp', 'support'].filter((n) => layers[1].includes(n)), 'json')
if (layers[2].includes('mcp')) moveAfter(2, ['mcp'], 'jsonic')
// A loop between two boxes of one column is shortest with the boxes side by side, on whichever
// side of the peer crosses fewer edges.
for (const e of drawnOptional.filter((x) => x.same)) {
  const d = depth(e.to)
  const trial = (before) => { const L = layers[d].filter((x) => x !== e.from); L.splice(L.indexOf(e.to) + (before ? 0 : 1), 0, e.from); return L }
  const options = [trial(true), trial(false)].map((L) => { layers[d] = L; return { L, c: crossings() } })
  layers[d] = options[0].c <= options[1].c ? options[0].L : options[1].L
}
bestC = crossings()

// Vertical placement: each node moves towards its neighbours' mean height, subject to its
// layer's order and minimum gaps (isotonic regression by pool-adjacent-violators).
const H = 26
const h = (id) => (kind.get(id) === 'real' ? H : 0)
const gapBetween = (a, b) => (kind.get(a) === 'real' && kind.get(b) === 'real' ? 14 : kind.get(a) === 'real' || kind.get(b) === 'real' ? 9 : 7)
const cy = new Map()
for (const L of layers) { let y = 0; L.forEach((id, i) => { if (i) y += h(L[i - 1]) / 2 + gapBetween(L[i - 1], id) + h(id) / 2; cy.set(id, y) }) }
function pav(values, weights) {
  const blocks = []
  values.forEach((v, i) => {
    blocks.push({ v, w: weights[i], n: 1 })
    while (blocks.length > 1 && blocks[blocks.length - 2].v > blocks[blocks.length - 1].v) {
      const b = blocks.pop(), a = blocks.pop()
      blocks.push({ v: (a.v * a.w + b.v * b.w) / (a.w + b.w), w: a.w + b.w, n: a.n + b.n })
    }
  })
  return blocks.flatMap((b) => Array(b.n).fill(b.v))
}
// Place one layer: each node aims at the mean height of the given neighbours, and a node with
// none follows the node above it and pulls on nothing.
function placeLayer(d, neighbours) {
  const L = layers[d]
  const offs = []; let o = 0
  L.forEach((id, i) => { if (i) o += h(L[i - 1]) / 2 + gapBetween(L[i - 1], id) + h(id) / 2; offs.push(o) })
  const want = [], weight = []
  L.forEach((id, i) => {
    const ns = neighbours(id)
    if (ns.length) { want.push(ns.reduce((s, x) => s + cy.get(x), 0) / ns.length); weight.push(kind.get(id) === 'real' ? 1 : 0.6) }
    else { want.push(i ? want[i - 1] - offs[i - 1] + offs[i] : cy.get(id)); weight.push(0.02) }
  })
  const z = pav(want.map((v, i) => v - offs[i]), weight)
  L.forEach((id, i) => cy.set(id, z[i] + offs[i]))
}
// Seed outwards from the tallest column, which keeps its compact stacking.
const ref = layers.reduce((bi, L, i) => (i && L.length > layers[bi].length ? i : bi), 1)
for (let d = ref - 1; d >= 1; d--) placeLayer(d, (id) => right.get(id) || [])
for (let d = ref + 1; d <= maxDepth; d++) placeLayer(d, (id) => left.get(id) || [])
// Refine with both sides, holding the reference column's mean fixed so the layout cannot drift.
const refMean = () => { const r = layers[ref].filter((id) => kind.get(id) === 'real'); return r.reduce((s, id) => s + cy.get(id), 0) / r.length }
const anchor = refMean()
for (let it = 0; it < 60; it++) {
  for (let d = 1; d <= maxDepth; d++) placeLayer(d, (id) => [...(left.get(id) || []), ...(right.get(id) || [])])
  const shift = refMean() - anchor
  for (const [id, y] of cy) cy.set(id, y - shift)
}
// Compact the smaller connected components (the support–mcp pair and the packages nothing
// peers on): lift each one, top first, until a node meets the node above it in its column.
{
  const comp = new Map(); let nComp = 0
  for (const id of cy.keys()) {
    if (comp.has(id)) continue
    const stack = [id]; comp.set(id, nComp)
    while (stack.length) { const x = stack.pop(); for (const y of [...(left.get(x) || []), ...(right.get(x) || [])]) if (!comp.has(y)) { comp.set(y, nComp); stack.push(y) } }
    nComp++
  }
  const members = Array.from({ length: nComp }, () => [])
  for (const [id, c] of comp) members[c].push(id)
  const main = members.reduce((bi, m, i) => (m.length > members[bi].length ? i : bi), 0)
  const layerOf = new Map(); layers.forEach((L, d) => L.forEach((id) => layerOf.set(id, d)))
  const order = members.map((m, i) => i).filter((i) => i !== main).sort((a, b) => Math.min(...members[a].map((id) => cy.get(id))) - Math.min(...members[b].map((id) => cy.get(id))))
  for (const c of order) {
    let up = Infinity
    for (const id of members[c]) {
      const L = layers[layerOf.get(id)], i = L.indexOf(id)
      if (i === 0) continue
      const above = L[i - 1]
      if (comp.get(above) === c) continue
      up = Math.min(up, cy.get(id) - (cy.get(above) + h(above) / 2 + gapBetween(above, id) + h(id) / 2))
    }
    if (up > 0 && up < Infinity) for (const id of members[c]) cy.set(id, cy.get(id) - up)
  }
}
const TOP = 58
let minTop = Infinity, maxBot = -Infinity
for (const [id, y] of cy) { minTop = Math.min(minTop, y - h(id) / 2); maxBot = Math.max(maxBot, y + h(id) / 2) }
for (const [id, y] of cy) cy.set(id, y - minTop + TOP)
const graphBottom = maxBot - minTop + TOP

// Geometry.
const W = 120, PITCH = 178, GAP = PITCH - W
const BAR_X = 18, BAR_W = 40, X1 = BAR_X + BAR_W + 46
const colX = (d) => X1 + (d - 1) * PITCH
const VBW = colX(maxDepth) + W + 22
const VBH = Math.ceil(graphBottom + 26)

// Ports: spread the edges leaving a box's left side, and those reaching its right side, over its
// height, ordered by where they go. A same-column loop takes a right-side port at both ends.
const outPorts = new Map(), inPorts = new Map(), loopPorts = new Map()
const startBox = (e) => e.chain[0], endBox = (e) => e.chain[e.chain.length - 1]
const firstHop = (e) => e.chain[1], lastHop = (e) => e.chain[e.chain.length - 2]
for (const n of names) {
  const outs = edges.filter((e) => !e.same && startBox(e) === n).sort((a, b) => cy.get(firstHop(a)) - cy.get(firstHop(b)))
  outs.forEach((e, i) => outPorts.set(e, cy.get(n) + (outs.length > 1 ? (i - (outs.length - 1) / 2) * Math.min(5, 18 / (outs.length - 1)) : 0)))
  const far = (e) => (e.same ? cy.get(e.from === n ? e.to : e.from) : cy.get(lastHop(e)))
  const ins = edges.filter((e) => (e.same ? e.from === n || e.to === n : endBox(e) === n)).sort((a, b) => far(a) - far(b))
  ins.forEach((e, i) => {
    const y = cy.get(n) + (ins.length > 1 ? (i - (ins.length - 1) / 2) * Math.min(3, 20 / (ins.length - 1)) : 0)
    if (!e.same) inPorts.set(e, y)
    else { if (!loopPorts.has(e)) loopPorts.set(e, {}); loopPorts.get(e)[n === e.from ? 'from' : 'to'] = y }
  })
}

const f = (v) => Math.round(v * 10) / 10
const paths = []
for (const e of edges) {
  let dpath
  if (e.same) {
    const x = colX(depth(e.from)) + W, { from: y1, to: y2 } = loopPorts.get(e), out = GAP * 0.5
    dpath = `M${f(x)} ${f(y1)} C${f(x + out)} ${f(y1)} ${f(x + out)} ${f(y2)} ${f(x)} ${f(y2)}`
  } else {
    const dStart = depth(startBox(e))
    let x = colX(dStart), y = outPorts.get(e)
    dpath = `M${f(x)} ${f(y)}`
    for (let i = 1; i < e.chain.length; i++) {
      const id = e.chain[i]
      const k = dStart - i
      const ty = i === e.chain.length - 1 ? inPorts.get(e) : cy.get(id)
      const tx = colX(k) + W
      dpath += ` C${f(x - GAP * 0.55)} ${f(y)} ${f(tx + GAP * 0.55)} ${f(ty)} ${f(tx)} ${f(ty)}`
      if (i < e.chain.length - 1) { dpath += ` H${f(colX(k))}`; x = colX(k) } else { x = tx }
      y = ty
    }
  }
  paths.push({ e, d: dpath, fl: floor(e.range) })
}

// Facts the text states, all read from the data.
const direct = names.filter((n) => M.engine.get(n) === 'direct')
const engineFree = names.filter((n) => !M.needsEngine(n)).sort()
const viaPeers = names.filter((n) => M.engine.get(n) !== 'direct' && M.needsEngine(n)).sort()
const optionalEngine = names.filter((n) => M.engine.get(n) === 'optional').sort()
const jsonicIn = drawnRequired.filter((e) => e.to === 'jsonic').length
const deepest = names.filter((n) => depth(n) === maxDepth).sort(bySeed)
const deepChains = deepest.map((n) => chainFrom(M, n))
const chainText = (c) => joinNames(c.slice(1).map(code))
const deepestText = deepest.length === 1
  ? `${code(deepest[0])}, ${word(maxDepth)} steps up through ${chainText(deepChains[0])}, sits furthest right`
  : `${joinNames(deepChains.map((c) => `${code(c[0])} (through ${chainText(c)})`))} sit furthest right, ${word(maxDepth)} steps up`
const ariaChain = [...deepChains[0], 'parser'].join(', ')

let svg = ''
svg += `<svg viewBox="0 0 ${VBW} ${VBH}" style="min-width:${VBW}px;max-width:${Math.round(VBW * 1.25)}px" role="img" aria-label="Peer dependencies between ${inGraph.length + 1} tabnas npm packages: ${direct.length} of the ${inGraph.length} packages peer on @tabnas/parser directly; jsonic is the second hub with ${jsonicIn} dependents; the deepest chain is ${ariaChain}; dashed arrows are optional peers, and dashed boxes install without the engine." xmlns="http://www.w3.org/2000/svg">`
svg += `<defs><marker id="ah" viewBox="0 0 8 8" refX="7.2" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path class="ah" d="M0 0.6 L7.4 4 L0 7.4 z"/></marker>`
svg += `<marker id="ahs" viewBox="0 0 8 8" refX="7.2" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path class="ahs" d="M0 0.6 L7.4 4 L0 7.4 z"/></marker></defs>`
// Column headers.
svg += `<text class="colhead" x="${BAR_X + BAR_W / 2}" y="24" text-anchor="middle">engine</text>`
for (let d = 1; d <= maxDepth; d++) svg += `<text class="colhead" x="${colX(d) + W / 2}" y="24" text-anchor="middle">depth ${d}</text>`
svg += `<line class="rule" x1="${BAR_X}" y1="36" x2="${VBW - 22}" y2="36"/>`
// The engine bar.
const barTop = TOP - 4, barH = graphBottom - TOP + 8
svg += `<g class="bar"><title>@tabnas/parser ${ver[PARSER]}: ${direct.length} of the ${inGraph.length} packages in this graph peer on it directly</title><rect x="${BAR_X}" y="${f(barTop)}" width="${BAR_W}" height="${f(barH)}" rx="8"/>`
svg += `<text transform="translate(${BAR_X + BAR_W / 2 + 4.5} ${f(barTop + barH / 2)}) rotate(-90)" text-anchor="middle">parser ${ver[PARSER]} · ${direct.length} of ${inGraph.length} packages here peer on it</text></g>`
// Edges: optional ones first, then light, then floored.
const layerRank = (p) => (p.e.optional ? 0 : p.fl ? 2 : 1)
for (const p of paths.sort((a, b) => layerRank(a) - layerRank(b))) {
  const marker = `url(#${p.fl ? 'ahs' : 'ah'})`
  svg += `<path class="edge${p.fl ? ' floored' : ''}${p.e.optional ? ' optional' : ''}" d="${p.d}" ${p.e.reversed ? 'marker-start' : 'marker-end'}="${marker}"><title>${esc(p.e.from)} peers on ${esc(p.e.to)} ${esc(p.e.range)}${p.e.optional ? ', optional' : ''}</title></path>`
}
// Nodes.
for (const n of names) {
  const p = byShort.get(n)
  const x = colX(depth(n)), y = cy.get(n) - H / 2
  const ext = [...Object.keys(p.deps), ...Object.keys(p.peers), ...Object.keys(p.optional)].filter((d) => !d.startsWith('@tabnas/'))
  const peerList = tabnasPeers(p).map((d) => `${short(d)} ${p.peers[d]}${isOpt(p, d) ? ' optional' : ''}`).join(', ')
  const cls = (n === 'jsonic' ? ' hub' : '') + (M.needsEngine(n) ? '' : ' free')
  svg += `<g class="node${cls}"><title>${esc(p.name)} ${esc(ver[p.name])} · peers: ${esc(peerList)}${M.needsEngine(n) ? '' : ' · installs without the engine'}${ext.length ? ' · third-party: ' + esc(ext.join(', ')) : ''}</title>`
  svg += `<rect x="${f(x)}" y="${f(y)}" width="${W}" height="${H}" rx="5"/>`
  svg += `<text x="${f(x + 10)}" y="${f(y + H / 2 + 4.2)}">${esc(n)}</text>`
  if (ext.length) svg += `<text class="ext" x="${f(x + W - 8)}" y="${f(y + H / 2 + 4)}" text-anchor="end">+${ext.length} npm</text>`
  svg += `</g>`
}
svg += `</svg>`

// What npm serves for the same version, where main declares different peers.
const sortedJson = (o) => JSON.stringify(Object.entries(o || {}).sort(([a], [b]) => a.localeCompare(b)))
const optKeys = (meta) => Object.keys(meta || {}).filter((k) => meta[k] && meta[k].optional).sort()
const parserFirst = (a, b) => (a === PARSER ? -1 : b === PARSER ? 1 : a.localeCompare(b))
function npmDiffers(p) {
  const n = npmManifests[p.name + '@' + p.version]
  if (!n) return null
  const same = sortedJson(n.peerDependencies) === sortedJson(p.peers) && sortedJson(n.dependencies) === sortedJson(p.deps) &&
    JSON.stringify(optKeys(n.peerDependenciesMeta)) === JSON.stringify([...p.peerOptional].sort())
  return same ? null : n
}
const unreleased = published.filter((p) => npmDiffers(p)).map((p) => short(p.name)).sort()
for (const p of published) if (!(p.name + '@' + p.version in npmManifests) || npmManifests[p.name + '@' + p.version] === null) throw new Error(`no npm manifest read for ${p.name}@${p.version}`)

// Table rows.
const rows = inGraph.slice().sort((a, b) => depth(short(a.name)) - depth(short(b.name)) || short(a.name).localeCompare(short(b.name)))
const parserRow = published.find((p) => p.name === PARSER)
function peerCell(p) {
  const opt = new Set(p.peerOptional)
  const cell = tabnasPeers(p).sort(parserFirst)
    .map((d) => `<code>${esc(short(d))}</code>${floor(p.peers[d]) ? ` <span class="floor">${esc(floor(p.peers[d]))}</span>` : ''}${opt.has(d) ? ' <span class="floor">optional</span>' : ''}`).join(', ')
  const n = npmDiffers(p)
  if (!n) return cell
  const nOpt = new Set(optKeys(n.peerDependenciesMeta))
  const was = Object.keys(n.peerDependencies || {}).filter((d) => d.startsWith('@tabnas/')).sort(parserFirst)
    .map((d) => `<code>${esc(short(d))}</code>${nOpt.has(d) ? ' optional' : ''}`).join(', ')
  return `${cell}<div class="was"><span class="stale">unreleased</span> npm's ${esc(p.version)} still declares ${was}${nOpt.size ? '' : ', all required'}</div>`
}
function extCell(p) {
  const ext = Object.entries({ ...p.deps, ...p.peers, ...p.optional }).filter(([d]) => !d.startsWith('@tabnas/'))
  return ext.length ? ext.map(([d, r]) => `<code>${esc(d)}</code> <span class="floor">${esc(r)}</span>`).join('<br>') : '<span class="none">none</span>'
}
const tr = (p, depthLabel) => `<tr><td><code class="pkg">${esc(short(p.name))}</code></td><td class="num">${esc(ver[p.name])}</td><td class="num">${depthLabel}</td><td>${peerCell(p) || '<span class="none">none</span>'}</td><td>${extCell(p)}</td><td><code class="sha">${esc(p.repo)}@${esc(shas[p.repo])}</code></td></tr>`
let table = tr(parserRow, '0')
for (const p of rows) table += tr(p, String(depth(short(p.name))))

// Outside the graph.
const outsidePublished = published.filter((p) => p.name !== PARSER && !byShort.has(short(p.name))).map((p) => p.name)
if (outsidePublished.join() !== '@tabnas/chess-view') throw new Error('outside the graph: ' + outsidePublished.join(', ') + '; the page names only chess-view')
const behindOf = (gr, versions) => { const w = gr.packages.find((p) => p.repo === 'web'); const pins = Object.keys(w.deps).filter((d) => d.startsWith('@tabnas/')); return { pins, behind: pins.filter((d) => versions[d] !== w.deps[d]) } }
const web = g.packages.find((p) => p.repo === 'web')
const webRows = Object.entries(web.deps).map(([d, pin]) => {
  const latest = ver[d] || '—'
  const behind = d.startsWith('@tabnas/') && latest !== pin
  return `<tr><td><code>${esc(d)}</code></td><td class="num">${esc(pin)}</td><td class="num">${esc(d.startsWith('@tabnas/') ? latest : '')}</td><td>${d.startsWith('@tabnas/') ? (behind ? '<span class="stale">behind</span>' : '<span class="ok">current</span>') : '<span class="none">third-party</span>'}</td></tr>`
}).join('')
const webNow = behindOf(g, ver), webPrev = behindOf(prevG, prevVer)
const webFinding = webNow.behind.length
  ? `${Word(webNow.behind.length)} of its ${word(webNow.pins.length)} tabnas pins are behind the versions on <code>main</code>.`
  : `All ${word(webNow.pins.length)} of its tabnas pins match the versions on <code>main</code>.`

const lspExt = g.packages.find((p) => p.name === 'tabnas-lsp-vscode')
const chessView = g.packages.find((p) => p.name === '@tabnas/chess-view')
const peerEdgeCount = g.edges.filter((e) => e.kind === 'peer').length
const optPeerEdgeCount = g.edges.filter((e) => e.kind === 'peer' && e.optionalPeer).length
const depEdgesPublished = g.edges.filter((e) => e.kind === 'dep' && published.some((p) => p.name === e.from)).length
const thirdParty = inGraph.filter((p) => [...Object.keys(p.deps), ...Object.keys(p.peers), ...Object.keys(p.optional)].some((d) => !d.startsWith('@tabnas/'))).map((p) => short(p.name)).sort()

// Version floors, spelled out for the caption instead of labelled on crowded arrows.
const floorGroups = new Map()
for (const e of edges) { const fl = floor(e.range); if (!fl) continue; const k = e.to + ' ' + fl; if (!floorGroups.has(k)) floorGroups.set(k, { to: e.to, fl, from: [] }); floorGroups.get(k).from.push(e.from) }
const edgeFloors = [...floorGroups.values()].map((gr) => `${joinNames(gr.from.sort().map(code))} need${gr.from.length === 1 ? 's' : ''} ${code(gr.to)} ${esc(gr.fl)}`).join('; ')
const parserFloorGroups = new Map()
for (const p of inGraph) { const fl = floor(p.peers[PARSER]); if (!fl) continue; if (!parserFloorGroups.has(fl)) parserFloorGroups.set(fl, []); parserFloorGroups.get(fl).push(short(p.name)) }
const parserFloors = [...parserFloorGroups.entries()].sort((a, b) => b[1].length - a[1].length).map(([fl, xs]) => `${joinNames(xs.sort().map(code))} ${esc(fl)}`).join(', and ')
// Optional peers, grouped by the package that declares them.
const optByFrom = new Map()
for (const n of names) { const o = M.opt.get(n); if (o.length) optByFrom.set(n, o) }
const optionalText = joinNames([...optByFrom.entries()].sort(([a], [b]) => bySeed(a, b)).map(([n, o]) => `${code(n)}'s ${joinNames(o.map(code))}`))

// The engine, read from the data.
const engineParts = []
if (viaPeers.length) engineParts.push(`${joinNames(viaPeers.map(code))} ${viaPeers.length === 1 ? 'gets' : 'get'} it through ${viaPeers.length === 1 ? 'its' : 'their'} peers`)
if (engineFree.length) engineParts.push(`${joinNames(engineFree.map(code))}, the dashed boxes, install without it`)
const engineFinding = `${Word(direct.length)} of the ${word(inGraph.length)} packages in the graph peer on <code>@tabnas/parser</code> directly.` +
  (engineParts.length ? ' ' + engineParts.join(', and ') + '.' : '')
const engineCaption = `The bar on the left is the engine, <code>@tabnas/parser</code>: the ${word(direct.length)} packages that peer on it directly have no arrow to it` +
  (optionalEngine.length ? `, and neither ${optionalEngine.length === 1 ? 'does' : 'do'} ${joinNames(optionalEngine.map(code))}, whose peer on it is optional` : '') + '.'
// Optional peers drawn against the flow: to the right of the package that names them, or beside it.
const backward = drawnOptional.filter((e) => e.reversed || e.same)
const backwardBy = new Map()
for (const e of backward) { if (!backwardBy.has(e.from)) backwardBy.set(e.from, []); backwardBy.get(e.from).push(e.to) }
const backwardList = [...backwardBy.entries()].map(([n, ts]) => `${code(n)}'s ${joinNames(ts.sort(bySeed).map(code))}`)
const backwardCount = backward.length
const backwardNote = backwardCount
  ? `Because an optional peer adds no depth, it can sit to the right of the package that names it, or in its column, as ${joinNames(backwardList)} ${backwardCount === 1 ? 'does' : 'do'}.`
  : ''

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']
const dayOf = (d) => `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`

// Since the previous read.
const prevReadAt = new Date(fs.readFileSync('prev/read-at.txt', 'utf8').trim())
const hhmm = (d) => `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}`
const peerState = (mdl, n) => { const p = mdl.byShort.get(n); return p ? Object.fromEntries(tabnasPeers(p).map((d) => [short(d), isOpt(p, d) ? 'optional' : 'required'])) : {} }
const prLink = (repo, pr) => `<a href="https://github.com/tabnas/${esc(repo)}/pull/${pr}">#${pr}</a>`
const prsOf = (repo) => ((changes[repo] || {}).commits || []).filter((c) => c.pr).map((c) => prLink(repo, c.pr))
const sinceRows = []
for (const n of [...new Set([...P.byShort.keys(), ...M.byShort.keys()])]) {
  const a = peerState(P, n), b = peerState(M, n)
  const dropped = Object.keys(a).filter((d) => !(d in b)).sort(bySeed)
  const added = Object.keys(b).filter((d) => !(d in a)).sort(bySeed)
  const madeOpt = Object.keys(b).filter((d) => a[d] === 'required' && b[d] === 'optional').sort(bySeed)
  const madeReq = Object.keys(b).filter((d) => a[d] === 'optional' && b[d] === 'required').sort(bySeed)
  const d0 = P.byShort.has(n) ? P.depth(n) : null, d1 = M.byShort.has(n) ? M.depth(n) : null
  const own = [dropped.length && `drops ${joinNames(dropped.map(code))}`, added.length && `adds ${joinNames(added.map(code))}`,
    madeOpt.length && `makes ${joinNames(madeOpt.map(code))} optional`, madeReq.length && `makes ${joinNames(madeReq.map(code))} required`].filter(Boolean)
  if (!own.length && d0 === d1) continue
  const moved = (M.req.get(n) || []).filter((d) => P.byShort.has(d) && P.depth(d) !== M.depth(d)).sort(bySeed)
  const change = own.length ? joinNames(own) : `none of its own; its peer${moved.length === 1 ? '' : 's'} ${joinNames(moved.map(code))} moved`
  const repo = (M.byShort.get(n) || P.byShort.get(n)).repo
  sinceRows.push({ n, d0, d1, html: `<tr><td><code class="pkg">${esc(n)}</code></td><td>${change}</td><td class="num">${d0 === d1 ? d1 : `${d0 == null ? 'new' : d0} → ${d1}`}</td><td>${own.length ? prsOf(repo).join(', ') || '' : '<span class="none">—</span>'}</td></tr>` })
}
// The largest depth change first, and within it from the bottom of the chain up.
sinceRows.sort((x, y) => (y.d0 - y.d1) - (x.d0 - x.d1) || x.d1 - y.d1 || bySeed(x.n, y.n))
const prevMax = Math.max(...[...P.byShort.keys()].map(P.depth))
const mergedCount = Object.values(changes).reduce((s, c) => s + c.commits.filter((x) => x.pr).length, 0)
const sinceLead = `${Word(mergedCount)} pull requests merged since the read on ${dayOf(prevReadAt)} at ${hhmm(prevReadAt)} UTC moved ${word(Object.keys(changes).length)} <code>main</code> branches. ` +
  (prevMax !== maxDepth ? `The deepest chain went from ${word(prevMax)} steps to ${word(maxDepth)}. ` : '') +
  `Only these packages changed peers or depth.`
const webSince = (webPrev.behind.length !== webNow.behind.length)
  ? `<p class="lead"><code>web</code> ${prsOf('web').join(', ')} pins ${webNow.behind.length ? `all but ${word(webNow.behind.length)}` : `all ${word(webNow.pins.length)}`} of its tabnas packages at their versions on <code>main</code>; at the last read, ${word(webPrev.behind.length)} ${webPrev.behind.length === 1 ? 'was' : 'were'} behind.</p>`
  : ''
const unreleasedFinding = unreleased.length
  ? `<li>The peer changes in ${joinNames(unreleased.map(code))} are on <code>main</code> and not yet on npm. npm's releases of those versions still declare the old peers, so an install gets this graph only from each package's next release.</li>`
  : ''

// When the package.json files were read: read-at.txt, written as the survey fetched each main.
const readAt = new Date(fs.readFileSync('read-at.txt', 'utf8').trim())
const readDay = `${readAt.getUTCDate()} ${MONTHS[readAt.getUTCMonth()]} ${readAt.getUTCFullYear()}`
const html = fs.readFileSync('template.html', 'utf8')
  .replace('{{SVG}}', svg)
  .replace('{{TABLE}}', table)
  .replace('{{WEBROWS}}', webRows)
  .replace('{{SINCEROWS}}', sinceRows.map((r) => r.html).join(''))
  .replace('{{SINCE_LEAD}}', sinceLead)
  .replace('{{WEB_SINCE_P}}', webSince)
  .replace('{{BACKWARD_NOTE}}', backwardNote)
  .replace('{{ENGINE_FINDING}}', engineFinding)
  .replace('{{ENGINE_CAPTION}}', engineCaption)
  .replace('{{UNRELEASED_LI}}', unreleasedFinding)
  .replace('{{WEB_FINDING}}', webFinding)
  .replace('{{DEEPEST}}', deepestText)
  .replace('{{OPTIONAL_PEERS}}', optionalText)
  .replace('{{THIRD_PARTY}}', joinNames(thirdParty.map(code)))
  .replace(/{{N_REPOS}}/g, String(Object.keys(shas).length))
  .replace(/{{N_PUBLISHED}}/g, String(published.length))
  .replace(/{{N_GRAPH}}/g, String(inGraph.length))
  .replace(/{{N_PEER_EDGES}}/g, String(peerEdgeCount))
  .replace(/{{N_OPT_PEER_EDGES}}/g, word(optPeerEdgeCount))
  .replace(/{{N_DEP_PUBLISHED}}/g, String(depEdgesPublished))
  .replace(/{{JSONIC_IN}}/g, String(jsonicIn))
  .replace(/{{PARSER_V}}/g, ver[PARSER])
  .replace(/{{CHESSVIEW_V}}/g, chessView.version)
  .replace(/{{LSPEXT_DEP}}/g, Object.entries(lspExt.deps).map(([d, r]) => `<code>${esc(d)}</code> ${esc(r)}`).join(', '))
  .replace('{{EDGE_FLOORS}}', edgeFloors)
  .replace('{{PARSER_FLOORS}}', parserFloors)
  .replace(/{{READ_DAY}}/g, readDay)
  .replace(/{{READ_TIME}}/g, hhmm(readAt))
const left_ = html.match(/{{[A-Z_]+}}/g)
if (left_) throw new Error('unfilled placeholders: ' + [...new Set(left_)].join(' '))
fs.writeFileSync('site/tabnas-peers.html', html)
console.log(`nodes ${names.length}, drawn edges ${edges.length} (${drawnOptional.length} optional: ${drawnOptional.map((e) => e.from + '>' + e.to + (e.reversed ? ' reversed' : e.same ? ' same' : '')).join(', ')}), depth ${maxDepth} (was ${prevMax}), crossings ${bestC}, viewBox ${VBW}x${VBH}`)
console.log(`direct ${direct.length}/${inGraph.length}, via peers [${viaPeers}], engine-free [${engineFree}], optional engine [${optionalEngine}], jsonic in ${jsonicIn}, unreleased [${unreleased}], web behind ${webNow.behind.length}/${webNow.pins.length} (was ${webPrev.behind.length}), third-party [${thirdParty}]`)
console.log('deepest:', deepChains.map((c) => c.join(' > ')).join(' | '))
for (let d = 1; d <= maxDepth; d++) console.log(d, layers[d].filter((id) => kind.get(id) === 'real').join(' '))
for (const r of sinceRows) console.log('since:', r.n, r.d0, '->', r.d1)
