const fs = require('fs')
const out = { packages: [], edges: [] }
for (const f of fs.readdirSync('.').filter(f => f.includes('@') && f.endsWith('package.json')).sort()) {
  const [repo, path] = f.split('@')
  const p = JSON.parse(fs.readFileSync(f, 'utf8'))
  const dep = p.dependencies || {}, peer = p.peerDependencies || {}, opt = p.optionalDependencies || {}
  const peerMeta = p.peerDependenciesMeta || {}
  out.packages.push({ repo, file: path.replace(/_/g, '/'), name: p.name, version: p.version, private: !!p.private,
    deps: dep, peers: peer, optional: opt, peerOptional: Object.keys(peerMeta).filter(k => peerMeta[k].optional) })
}
const names = new Map(out.packages.map(p => [p.name, p]))
for (const p of out.packages) {
  for (const [kind, set] of [['dep', p.deps], ['peer', p.peers], ['optional', p.optional]]) {
    for (const [d, range] of Object.entries(set)) {
      if (d.startsWith('@tabnas/')) out.edges.push({ from: p.name, to: d, kind, range, optionalPeer: p.peerOptional.includes(d), known: names.has(d) })
    }
  }
}
fs.writeFileSync('graph.json', JSON.stringify(out, null, 1))
for (const p of out.packages) {
  const tab = e => Object.keys(e).filter(k => k.startsWith('@tabnas/')).map(k => k.replace('@tabnas/', '') + '@' + e[k])
  const ext = e => Object.keys(e).filter(k => !k.startsWith('@tabnas/')).map(k => k + '@' + e[k])
  console.log(`${p.repo.padEnd(12)} ${p.file.padEnd(28)} ${String(p.name).padEnd(26)} ${String(p.version).padEnd(8)} ${p.private ? 'PRIVATE ' : ''}` +
    `dep[${tab(p.deps).join(' ')}] peer[${tab(p.peers).join(' ')}${p.peerOptional.length ? ' optional:' + p.peerOptional.join(',') : ''}] opt[${tab(p.optional).join(' ')}] ext[${[...ext(p.deps), ...ext(p.peers), ...ext(p.optional)].join(' ')}]`)
}
console.log('edges', out.edges.length, 'unknown targets', out.edges.filter(e => !e.known).map(e => e.from + '->' + e.to).join(' '))
