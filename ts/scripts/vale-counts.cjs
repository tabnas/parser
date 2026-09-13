
const Fs = require('node:fs')
const Path = require('node:path')
const { execFileSync } = require('node:child_process')

const { gatedDocs } = require('./gated-docs.cjs')

const REPO = Path.join(__dirname, '..', '..')
const INI = Path.join(REPO, '.vale.ini')
const GUIDE = Path.join(REPO, 'docs', 'STYLE-GUIDE.md')
const SCRATCH = Path.join(REPO, '.vale-counts.ini')

const HITS = /\b(\d+)\s+hits?\b/g
const SPAN = /\b(\d+)\s+alerts?\s+across\s+(\d+)\s+files?\b/


function vale(config, files) {
  const bin = process.env.VALE || 'vale'
  const out = execFileSync(bin,
    ['--config', config, '--no-exit', '--output=JSON',
      '--minAlertLevel=suggestion', ...files],
    { cwd: REPO, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 })
  const byRule = new Map()
  let total = 0
  for (const alerts of Object.values(JSON.parse(out))) {
    for (const alert of alerts) {
      byRule.set(alert.Check, 1 + (byRule.get(alert.Check) || 0))
      total++
    }
  }
  return { byRule, total }
}


// A rule switched off still carries a count, because the count is the
// evidence for switching it off. Measuring it needs a second run with
// those rules on, and the copy has to sit beside .vale.ini: StylesPath
// resolves against the config file.
function measure(ini) {
  const files = gatedDocs()
  const live = vale(INI, files)
  const off = [...ini.matchAll(/^([\w.]+)\s*=\s*NO\s*$/gm)].map((m) => m[1])
  if (0 < off.length) {
    Fs.writeFileSync(SCRATCH, ini.replace(/^([\w.]+)(\s*=\s*)NO\s*$/gm, '$1$2suggestion'))
    try {
      const all = vale(SCRATCH, files)
      for (const rule of off) live.byRule.set(rule, all.byRule.get(rule) || 0)
    }
    finally { Fs.rmSync(SCRATCH, { force: true }) }
  }
  return { byRule: live.byRule, total: live.total, files: files.length }
}


// A claim can wrap: `61\n# hits.` is one. The block is matched as joined
// prose and each character kept pointing at the line it came from, so
// the rewrite lands on the digits rather than on a reflowed paragraph.
function join(entries) {
  let text = ''
  const at = []
  for (const entry of entries) {
    const body = entry.line.replace(/^#[ \t]?/, '')
    const pad = entry.line.length - body.length
    if (0 < text.length) { text += ' '; at.push(null) }
    for (let i = 0; i < body.length; i++) at.push([entry.index, pad + i])
    text += body
  }
  return { text, at }
}


function blocks(ini) {
  const found = []
  let block = []
  ini.split('\n').forEach((line, index) => {
    if (line.startsWith('#')) return block.push({ line, index })
    const rule = line.match(/^([\w.]+)\s*=/)
    if (rule && 0 < block.length) found.push({ rule: rule[1], ...join(block) })
    else if (0 < block.length) found.push({ rule: null, ...join(block) })
    block = []
  })
  if (0 < block.length) found.push({ rule: null, ...join(block) })
  return found
}


function edit(lines, edits) {
  for (const e of [...edits].sort((a, b) => b.line - a.line || b.col - a.col)) {
    const line = lines[e.line]
    lines[e.line] = line.slice(0, e.col) + e.text + line.slice(e.col + e.was.length)
  }
}


function report(write) {
  let ini = Fs.readFileSync(INI, 'utf8')
  const { byRule, total, files } = measure(ini)
  const wrong = []
  const lines = ini.split('\n')
  const edits = []

  for (const block of blocks(ini)) {
    for (const found of block.text.matchAll(HITS)) {
      if (null == block.rule) continue
      const claimed = Number(found[1])
      const actual = byRule.get(block.rule) || 0
      if (actual === claimed) continue
      wrong.push(`${block.rule}: .vale.ini claims ${claimed} hits, Vale reports ${actual}`)
      const [line, col] = block.at[found.index]
      edits.push({ line, col, was: found[1], text: String(actual) })
    }
    for (const found of block.text.matchAll(new RegExp(SPAN, 'g'))) {
      if (Number(found[1]) === total && Number(found[2]) === files) continue
      wrong.push(`.vale.ini: claims ${found[1]} alerts across ${found[2]} files, Vale reports ${total} across ${files}`)
      const [aLine, aCol] = block.at[found.index]
      edits.push({ line: aLine, col: aCol, was: found[1], text: String(total) })
      const offset = found.index + found[0].lastIndexOf(found[2])
      const [fLine, fCol] = block.at[offset]
      edits.push({ line: fLine, col: fCol, was: found[2], text: String(files) })
    }
  }
  edit(lines, edits)
  ini = lines.join('\n')

  // The guide repeats the header total in prose, which is how the two
  // came to disagree.
  let guide = Fs.existsSync(GUIDE) ? Fs.readFileSync(GUIDE, 'utf8') : null
  if (null != guide) {
    for (const found of guide.match(new RegExp(SPAN, 'g')) || []) {
      const parts = found.match(SPAN)
      if (Number(parts[1]) === total && Number(parts[2]) === files) continue
      wrong.push(`docs/STYLE-GUIDE.md: claims ${parts[1]} alerts across ${parts[2]} files, Vale reports ${total} across ${files}`)
      guide = guide.split(found).join(
        found.replace(SPAN, (m, a, f) => m.replace(a, String(total)).replace(` ${f} `, ` ${files} `)))
    }
  }

  if (write) {
    Fs.writeFileSync(INI, ini)
    if (null != guide) Fs.writeFileSync(GUIDE, guide)
  }
  return { wrong, total, files }
}


if (require.main === module) {
  const write = process.argv.includes('--write')
  const { wrong, total, files } = report(write)
  if (0 === wrong.length) {
    process.stdout.write(`vale-counts: ${total} alerts across ${files} files, as recorded\n`)
  }
  else if (write) {
    process.stdout.write('vale-counts: re-measured\n  ' + wrong.join('\n  ') +
      '\n\nCheck the wrapping of any comment the numbers changed length in.\n')
  }
  else {
    process.stderr.write('vale-counts: the recorded counts are not what Vale reports.\n  ' +
      wrong.join('\n  ') +
      '\n\nRe-measure with: node ts/scripts/vale-counts.cjs --write\n')
    process.exitCode = 1
  }
}

module.exports = { report, blocks, measure }
