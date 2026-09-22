#!/usr/bin/env node

'use strict'

// A committed dependency must name a PUBLISHED package or a GitHub reference.
// Local wiring -- file:, link:, a sibling path, a packed archive, a go replace
// -- is how a change gets tested before its dependency is released, and it is
// correct right up to the commit. This gate is what stops it arriving in one.
//
// Only what git TRACKS is judged, so local wiring stays legal until staged.

const Fs = require('node:fs')
const Path = require('node:path')
const Child = require('node:child_process')

const REPO = process.env.DEP_GATE_REPO
  ? Path.resolve(process.env.DEP_GATE_REPO)
  : Path.join(__dirname, '..')

const CONFIG_PATH = Path.join(__dirname, 'dep-gate.json')

const SPEC_SECTIONS = [
  'dependencies',
  'devDependencies',
  'peerDependencies',
  'optionalDependencies',
  'resolutions',
  'overrides',
]

const ARCHIVE_RE = /\.(tgz|tar\.gz|zip)(\?|#|$)/i
const GITHUB_HOST_RE = /(^|[@/.])github\.com([:/]|$)/i
const DEFAULT_REGISTRY_RE = /^https?:\/\/registry\.npmjs\.org\//i

const ALLOWED = new Set(['registry', 'github'])

const RULE = {
  file: 'local-path-dep',
  link: 'local-path-dep',
  portal: 'local-path-dep',
  workspace: 'workspace-dep',
  catalog: 'workspace-dep',
  path: 'local-path-dep',
  archive: 'archive-dep',
  'git-other': 'non-github-git-dep',
}

const WHY = {
  'local-path-dep':
    'resolves to a filesystem path, so it means nothing on another machine',
  'workspace-dep':
    'resolves inside a workspace this repository does not publish',
  'archive-dep':
    'resolves to a packed archive rather than a published version',
  'non-github-git-dep':
    'is a git reference to a host other than github.com',
  'lockfile-local-resolution':
    'a committed lockfile resolves this from a path or a link',
  'lockfile-foreign-registry':
    'a committed lockfile resolves this from a registry other than npmjs.org',
  'go-external-path-replace':
    'a committed go.mod redirects a module to a path OUTSIDE this repository, '
    + 'so it resolves only where that sibling happens to be checked out',
  'go-module-replace':
    'a committed go.mod redirects a module to another module',
  'go-workspace':
    'a committed go.work resolves modules from sibling directories',
  'cargo-external-path-dep':
    'a Cargo.toml takes a dependency by a path outside this repository',
  'escaping-symlink':
    'a committed symlink points outside this repository or into node_modules',
  'committed-archive':
    'a packed archive is committed',
  'foreign-registry':
    'a committed .npmrc points npm at a registry other than npmjs.org',
  'stale-allow':
    'an allowlist entry matches nothing, so it is describing a state that has gone',
  'unreasoned-allow':
    'an allowlist entry carries no reason',
}


function readConfig(path) {
  try {
    return JSON.parse(Fs.readFileSync(path, 'utf8'))
  }
  catch (err) {
    if ('ENOENT' === err.code) return {}
    throw new Error('dep-gate: ' + path + ' is not readable JSON: ' + err.message)
  }
}


// npm accepts a dependency spec in a dozen shapes. The order here is the order
// npm itself disambiguates them in: an explicit protocol beats a shorthand,
// and `owner/repo` is only GitHub once every other reading is excluded.
function classify(spec) {
  if (null != spec && 'object' === typeof spec) return { source: 'nested', detail: '' }

  const s = String(null == spec ? '' : spec).trim()

  if ('' === s) return { source: 'registry', detail: '(empty, npm reads it as *)' }

  // `$name` in overrides means "whatever dependencies.name resolves to".
  if ('$' === s[0]) return { source: 'registry', detail: s }

  if (/^file:/i.test(s)) return { source: 'file', detail: s }
  if (/^link:/i.test(s)) return { source: 'link', detail: s }
  if (/^portal:/i.test(s)) return { source: 'portal', detail: s }
  if (/^workspace:/i.test(s)) return { source: 'workspace', detail: s }
  if (/^catalog:/i.test(s)) return { source: 'catalog', detail: s }

  // An alias still resolves from the registry.
  if (/^npm:/i.test(s)) return { source: 'registry', detail: s }

  if (/^github:/i.test(s)) return { source: 'github', detail: s }
  if (/^(gitlab|bitbucket|gist):/i.test(s)) return { source: 'git-other', detail: s }

  if (/^(git\+ssh|git\+https?|git|ssh|https?):\/\//i.test(s)) {
    if (GITHUB_HOST_RE.test(s)) return { source: 'github', detail: s }
    if (/^https?:\/\//i.test(s) && ARCHIVE_RE.test(s)) {
      return { source: 'archive', detail: s }
    }
    return { source: 'git-other', detail: s }
  }

  if (/^(\.{1,2}[\\/]|[\\/]|~[\\/]|[A-Za-z]:[\\/])/.test(s)) {
    return { source: ARCHIVE_RE.test(s) ? 'archive' : 'path', detail: s }
  }

  // `owner/repo` and `owner/repo#ref`. A scoped package name is the KEY in a
  // dependency map and never the value, so a leading @ here is not a name.
  if ('@' !== s[0] && /^[\w.-]+\/[\w.-]+(#.+)?$/.test(s)) {
    return { source: 'github', detail: s }
  }

  if (ARCHIVE_RE.test(s)) return { source: 'archive', detail: s }

  return { source: 'registry', detail: s }
}


// `overrides` nests arbitrarily deep, and a node can carry both a spec under
// "." and child overrides beside it.
function walkSpecs(node, section, prefix, out) {
  if (null == node || 'object' !== typeof node || Array.isArray(node)) return out

  for (const [name, value] of Object.entries(node)) {
    const label = prefix ? prefix + ' > ' + name : name
    if (null != value && 'object' === typeof value && !Array.isArray(value)) {
      walkSpecs(value, section, label, out)
    }
    else {
      out.push({ section, name: label, spec: value })
    }
  }

  return out
}


function checkManifest(file, json, config, seen) {
  const findings = []
  const allow = config.allow || {}
  const note = (k) => { if (seen) seen.add(k) }

  const entries = []
  for (const section of SPEC_SECTIONS) {
    walkSpecs(json[section], section, '', entries)
  }
  walkSpecs((json.pnpm || {}).overrides, 'pnpm.overrides', '', entries)

  for (const entry of entries) {
    const { source, detail } = classify(entry.spec)
    if (ALLOWED.has(source)) continue

    const key = file + ':' + entry.section + ':' + entry.name
    note(key)
    if (Object.prototype.hasOwnProperty.call(allow, key)) continue

    findings.push({
      rule: RULE[source] || 'unknown-dep-source',
      file,
      where: entry.section + '.' + entry.name,
      spec: detail,
      key,
    })
  }

  return findings
}


// A clean package.json over a lockfile that records a file: resolution still
// installs from that path, so the lockfile is checked on its own terms.
function checkLockfile(file, json, config, seen) {
  const findings = []
  const allow = config.allow || {}

  const add = (rule, where, spec) => {
    const key = file + ':lock:' + where
    if (seen) seen.add(key)
    if (Object.prototype.hasOwnProperty.call(allow, key)) return
    findings.push({ rule, file, where, spec, key })
  }

  for (const [where, node] of Object.entries(json.packages || {})) {
    if (null == node || 'object' !== typeof node) continue
    if (true === node.link) add('lockfile-local-resolution', where || '(root)', 'link: true')
    const res = node.resolved
    if ('string' === typeof res && '' !== res) {
      if (/^(file:|link:|\.{1,2}[\\/]|[\\/])/i.test(res)) {
        add('lockfile-local-resolution', where || '(root)', res)
      }
      else if (/^https?:\/\//i.test(res) && !DEFAULT_REGISTRY_RE.test(res) &&
               !GITHUB_HOST_RE.test(res)) {
        add('lockfile-foreign-registry', where || '(root)', res)
      }
    }
  }

  // Lockfile v1 keeps the same information under a different shape.
  const walkV1 = (node, prefix) => {
    for (const [name, dep] of Object.entries(node || {})) {
      if (null == dep || 'object' !== typeof dep) continue
      const where = prefix ? prefix + ' > ' + name : name
      for (const field of ['resolved', 'version']) {
        const v = dep[field]
        if ('string' === typeof v && /^(file:|link:)/i.test(v)) {
          add('lockfile-local-resolution', where, v)
        }
      }
      walkV1(dep.dependencies, where)
    }
  }
  if (!json.packages) walkV1(json.dependencies, '')

  return findings
}


function checkGoMod(file, text, config, root, seen) {
  const findings = []
  const allow = config.allow || {}
  const REPO_ROOT = root || REPO
  const lines = text.split(/\r?\n/)
  let inBlock = false

  const add = (rule, where, spec) => {
    const key = file + ':replace:' + where
    if (seen) seen.add(key)
    if (Object.prototype.hasOwnProperty.call(allow, key)) return
    findings.push({ rule, file, where, spec, key })
  }

  // A replace pointing INSIDE this repository is how a helper module in the
  // tree uses the module beside it, and it travels with the checkout. One
  // pointing outside needs a sibling that may not be there.
  const dir = Path.dirname(Path.join(REPO_ROOT, file))

  const one = (body) => {
    const m = /^(\S+)(?:\s+\S+)?\s*=>\s*(\S+)(?:\s+(\S+))?/.exec(body.trim())
    if (!m) return
    const target = m[2]

    if (/^(\.{1,2}[\\/]|[\\/]|[A-Za-z]:[\\/])/.test(target)) {
      const abs = Path.resolve(dir, target)
      if (abs === REPO_ROOT || abs.startsWith(REPO_ROOT + Path.sep)) return
      add('go-external-path-replace', m[1], body.trim())
      return
    }

    add('go-module-replace', m[1], body.trim())
  }

  for (const raw of lines) {
    const line = raw.replace(/\/\/.*$/, '').trim()
    if ('' === line) continue
    if (/^replace\s*\($/.test(line)) { inBlock = true; continue }
    if (inBlock) {
      if (')' === line) { inBlock = false; continue }
      one(line)
      continue
    }
    if (/^replace\s+/.test(line)) one(line.replace(/^replace\s+/, ''))
  }

  return findings
}


function checkCargoToml(file, text, config, root, seen) {
  const findings = []
  const allow = config.allow || {}
  const REPO_ROOT = root || REPO
  const dir = Path.dirname(Path.join(REPO_ROOT, file))

  const re = /path\s*=\s*"([^"]+)"/g
  let m
  while (null !== (m = re.exec(text))) {
    const rel = m[1]
    const abs = Path.resolve(dir, rel)
    if (abs === REPO_ROOT || abs.startsWith(REPO_ROOT + Path.sep)) continue

    const key = file + ':cargo-path:' + rel
    if (seen) seen.add(key)
    if (Object.prototype.hasOwnProperty.call(allow, key)) continue
    findings.push({ rule: 'cargo-external-path-dep', file, where: rel, spec: m[0], key })
  }

  return findings
}


function tracked(root) {
  const out = Child.execFileSync('git', ['-C', root || REPO, 'ls-files', '-s', '-z'], {
    encoding: 'utf8', maxBuffer: 1 << 28,
  })
  const rows = []
  for (const rec of out.split('\0')) {
    if ('' === rec) continue
    const tab = rec.indexOf('\t')
    if (0 > tab) continue
    const mode = rec.slice(0, rec.indexOf(' '))
    rows.push({ mode, path: rec.slice(tab + 1) })
  }
  return rows
}


function checkAll(config, root) {
  const cfg = config || readConfig(CONFIG_PATH)
  const REPO_ROOT = root || REPO
  const skip = new Set(cfg.skipPaths || [])
  const findings = []
  const seenKeys = new Set()

  const record = (list) => {
    for (const f of list) {
      findings.push(f)
      if (f.key) seenKeys.add(f.key)
    }
  }

  // Record every key the allowlist COULD have matched, so a stale entry is
  // detectable whether or not it is currently suppressing anything.
  const offer = (key) => seenKeys.add(key)

  for (const { mode, path: rel } of tracked(REPO_ROOT)) {
    if (skip.has(rel)) continue
    const abs = Path.join(REPO_ROOT, rel)
    const base = Path.basename(rel)

    if ('120000' === mode) {
      let target = ''
      try { target = Fs.readlinkSync(abs) }
      catch { target = '' }
      const abstarget = Path.resolve(Path.dirname(abs), target)
      const escapes = !(abstarget === REPO_ROOT || abstarget.startsWith(REPO_ROOT + Path.sep))
      const intoModules = /(^|[\\/])node_modules([\\/]|$)/.test(target)
      const key = rel + ':symlink'
      offer(key)
      if ((escapes || intoModules) &&
          !Object.prototype.hasOwnProperty.call(cfg.allow || {}, key)) {
        findings.push({
          rule: 'escaping-symlink', file: rel, where: 'symlink', spec: '-> ' + target, key,
        })
      }
      continue
    }

    if (ARCHIVE_RE.test(rel)) {
      const key = rel + ':archive'
      offer(key)
      if (!Object.prototype.hasOwnProperty.call(cfg.allow || {}, key)) {
        findings.push({ rule: 'committed-archive', file: rel, where: 'file', spec: base, key })
      }
      continue
    }

    if ('go.work' === base || 'go.work.sum' === base) {
      const key = rel + ':go-workspace'
      offer(key)
      if (!Object.prototype.hasOwnProperty.call(cfg.allow || {}, key)) {
        findings.push({ rule: 'go-workspace', file: rel, where: 'file', spec: base, key })
      }
      continue
    }

    let text = null
    const read = () => {
      if (null === text) text = Fs.readFileSync(abs, 'utf8')
      return text
    }

    try {
      if ('package.json' === base) {
        record(checkManifest(rel, JSON.parse(read()), cfg, seenKeys))
      }
      else if ('package-lock.json' === base || 'npm-shrinkwrap.json' === base) {
        record(checkLockfile(rel, JSON.parse(read()), cfg, seenKeys))
      }
      else if ('go.mod' === base) {
        record(checkGoMod(rel, read(), cfg, REPO_ROOT, seenKeys))
      }
      else if ('Cargo.toml' === base) {
        record(checkCargoToml(rel, read(), cfg, REPO_ROOT, seenKeys))
      }
      else if ('.npmrc' === base) {
        for (const line of read().split(/\r?\n/)) {
          const m = /^\s*(?:[^;#\s]*:)?registry\s*=\s*(\S+)/.exec(line)
          if (!m) continue
          if (DEFAULT_REGISTRY_RE.test(m[1])) continue
          const key = rel + ':registry'
          offer(key)
          if (!Object.prototype.hasOwnProperty.call(cfg.allow || {}, key)) {
            findings.push({ rule: 'foreign-registry', file: rel, where: 'registry', spec: m[1], key })
          }
        }
      }
    }
    catch (err) {
      findings.push({
        rule: 'unreadable', file: rel, where: 'parse', spec: err.message, key: null,
      })
    }
  }

  // An allowlist is a liability once it outlives what it excused.
  for (const [key, reason] of Object.entries(cfg.allow || {})) {
    if ('string' !== typeof reason || '' === reason.trim()) {
      findings.push({ rule: 'unreasoned-allow', file: 'tools/dep-gate.json', where: key, spec: '', key: null })
    }
    if (!seenKeys.has(key)) {
      findings.push({ rule: 'stale-allow', file: 'tools/dep-gate.json', where: key, spec: '', key: null })
    }
  }

  return findings
}


function report(findings) {
  if (0 === findings.length) return 'dependency gate: clean'

  const lines = ['dependency gate: ' + findings.length + ' finding(s)', '']
  for (const f of findings) {
    lines.push('  ' + f.file + '  ' + f.where)
    lines.push('    ' + f.rule + ': ' + (WHY[f.rule] || 'is not a published package or a GitHub reference'))
    if (f.spec) lines.push('    found: ' + f.spec)
    if (f.key) lines.push('    allow with: "' + f.key + '": "<reason>"  in tools/dep-gate.json')
    lines.push('')
  }
  lines.push('A committed dependency names a published package or a GitHub reference.')
  lines.push('Local wiring is for working; undo it before the commit.')

  return lines.join('\n')
}


if (require.main === module) {
  const findings = checkAll()
  const text = report(findings)
  if (0 === findings.length) console.log(text)
  else console.error(text)
  process.exit(0 === findings.length ? 0 : 1)
}

module.exports = {
  classify,
  checkManifest,
  checkLockfile,
  checkGoMod,
  checkCargoToml,
  checkAll,
  report,
  REPO,
  RULE,
  WHY,
}
