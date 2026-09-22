#!/usr/bin/env node

'use strict'

// Contents come from the git INDEX (`git show :path`), never the working tree:
// the rule the gate enforces changes state at staging, so reading the file on
// disk would both fail unstaged wiring and miss a staged violation the working
// copy has since had reverted.

const Path = require('node:path')
const Child = require('node:child_process')

const REPO = process.env.DEP_GATE_REPO
  ? Path.resolve(process.env.DEP_GATE_REPO)
  : Path.join(__dirname, '..')

// Relative to the repository being judged, not to this file: the gate is
// parameterised by root, and the allowlist that applies is the one that root
// tracks.
const CONFIG_REL = 'tools/dep-gate.json'

const SPEC_SECTIONS = [
  'dependencies',
  'devDependencies',
  'peerDependencies',
  'optionalDependencies',
  'resolutions',
  'overrides',
]

const ARCHIVE_RE =
  /\.(tgz|tbz2?|txz|tzst|zip|tar|tar\.(gz|bz2|xz|zst))(\?|#|$)/i

const NPM_REGISTRY_HOST = 'registry.npmjs.org'
const GITHUB_GIT_HOSTS = new Set(['github.com', 'www.github.com', 'codeload.github.com'])

const MANIFEST_BASES = new Set([
  'package.json',
  'package-lock.json',
  'npm-shrinkwrap.json',
  'go.mod',
  'Cargo.toml',
  '.npmrc',
  '.gitmodules',
])

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
  'unchecked-lockfile':
    'a committed lockfile in a format this gate cannot read, so its sources are unjudged',
  'go-external-path-replace':
    'a committed go.mod redirects a module to a path OUTSIDE this repository, '
    + 'so it resolves only where that sibling happens to be checked out',
  'go-absolute-path-replace':
    'a committed go.mod redirects a module to an ABSOLUTE path, which names one machine',
  'go-untracked-path-replace':
    'a committed go.mod redirects a module to a path inside this repository that '
    + 'git does not track, so a fresh checkout will not contain it',
  'go-module-replace':
    'a committed go.mod redirects a module to another module',
  'go-workspace':
    'a go.work is committed, and a workspace belongs outside every repository it wires',
  'cargo-external-path-dep':
    'a Cargo.toml takes a dependency by a path outside this repository',
  'cargo-absolute-path-dep':
    'a Cargo.toml takes a dependency by an ABSOLUTE path, which names one machine',
  'cargo-non-github-git-dep':
    'a Cargo.toml takes a git dependency from a host other than github.com',
  'submodule-local-url':
    'a committed submodule resolves from a filesystem path',
  'submodule-non-github-url':
    'a committed submodule resolves from a host other than github.com',
  'escaping-symlink':
    'a committed symlink points outside this repository or into node_modules',
  'absolute-symlink':
    'a committed symlink names an ABSOLUTE path, which resolves only on one machine',
  'committed-archive':
    'a packed archive is committed',
  'foreign-registry':
    'a committed .npmrc points npm at a registry other than npmjs.org',
  'stale-allow':
    'an allowlist entry matches nothing, so it is describing a state that has gone',
  'unreasoned-allow':
    'an allowlist entry carries no reason',
  unreadable:
    'this gate could not read or parse the file',
}


// From the index, like every other input. Read from disk, the allowlist would be
// the one file a staged violation could excuse itself with while the entry
// excusing it is still unstaged -- the asymmetry that reading manifests from the
// index exists to close. An untracked allowlist excuses nothing.
function readConfig(root, trackedPaths) {
  if (!trackedPaths.has(CONFIG_REL)) return {}
  try {
    return JSON.parse(indexBlob(root, CONFIG_REL))
  }
  catch (err) {
    throw new Error(
      'dep-gate: ' + CONFIG_REL + ' is not readable JSON in the index: ' + err.message)
  }
}


// A path is machine-specific the moment it is absolute, whatever it currently
// happens to resolve to: `~`, a POSIX root, a Windows drive, a UNC share. The
// Windows forms are recognised on every platform, because the gate usually runs
// on a different one from the machine that wrote the file.
function absoluteish(p) {
  return /^(~[\\/]?$|~[\\/]|[\\/]{2}|[\\/]|[A-Za-z]:[\\/])/.test(p)
}


function localish(p) {
  return absoluteish(p) || /^\.{1,2}[\\/]/.test(p)
}


// The host, not a substring of the whole spec: `https://evil.example/github.com/o/r`
// has `github.com` in its PATH and is not a GitHub reference. The authority is
// read directly rather than through `new URL`, which REJECTS
// `git+ssh://git@github.com:npm/cli.git` -- one of npm's own documented forms,
// where what follows the colon is a path and not a port number.
function hostOf(spec) {
  const s = String(spec || '').replace(/^git\+/i, '')
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) {
    // scp-style, `user@host:owner/repo.git`, which git accepts and npm passes on.
    const m = /^[^@\s/]*@([^:\s/]+):/.exec(s)
    return m ? m[1].toLowerCase() : null
  }
  const m = /^[a-z][a-z0-9+.-]*:\/\/(?:[^/@]*@)?([^/:?#\s]+)/i.exec(s)
  return m ? m[1].toLowerCase() : null
}


function githubHost(host) {
  return null != host && GITHUB_GIT_HOSTS.has(host)
}


// npm accepts a dependency spec in a dozen shapes. The order here is the order
// npm itself disambiguates them in: an explicit protocol beats a shorthand, and
// `owner/repo` is only GitHub once every other reading is excluded.
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

  // `git+file:` is a git dependency on a directory: local, whatever the scheme.
  if (/^git\+file:/i.test(s)) return { source: 'file', detail: s }

  // An alias still resolves from the registry.
  if (/^npm:/i.test(s)) return { source: 'registry', detail: s }

  if (/^github:/i.test(s)) return { source: 'github', detail: s }
  if (/^(gitlab|bitbucket|gist):/i.test(s)) return { source: 'git-other', detail: s }

  if (/^(git\+ssh|git\+https?|git|ssh|https?):\/\//i.test(s)) {
    if (githubHost(hostOf(s))) return { source: 'github', detail: s }
    if (/^https?:\/\//i.test(s) && ARCHIVE_RE.test(s)) {
      return { source: 'archive', detail: s }
    }
    return { source: 'git-other', detail: s }
  }

  // scp-style git, which has no scheme at all.
  if (/^[^@\s/]+@[^:\s/]+:/.test(s)) {
    return githubHost(hostOf(s))
      ? { source: 'github', detail: s }
      : { source: 'git-other', detail: s }
  }

  if (localish(s)) {
    return { source: ARCHIVE_RE.test(s) ? 'archive' : 'path', detail: s }
  }

  // `owner/repo` and `owner/repo#ref`. A scoped package name is the KEY in a
  // dependency map and never the value, so a leading @ here is not a name.
  if ('@' !== s[0] && /^[\w.-]+\/[\w.-]+(#.+)?$/.test(s)) {
    return { source: 'github', detail: s }
  }

  if (ARCHIVE_RE.test(s)) return { source: 'archive', detail: s }

  // No version range contains a slash, so anything left that does is a path npm
  // will save as `file:` -- `vendor/packages/foo` and the like.
  if (s.includes('/')) return { source: 'path', detail: s }

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


function resolutionFinding(res) {
  if ('string' !== typeof res || '' === res) return null
  if (/^(file:|link:|portal:|git\+file:)/i.test(res) || localish(res)) {
    return 'lockfile-local-resolution'
  }
  if (/^[^@\s/]+@[^:\s/]+:/.test(res) && !githubHost(hostOf(res))) {
    return 'lockfile-foreign-registry'
  }
  if (/:\/\//.test(res)) {
    const host = hostOf(res)
    if (null == host) return null
    if (NPM_REGISTRY_HOST === host) return null
    if (githubHost(host)) return null
    return 'lockfile-foreign-registry'
  }
  return null
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
    const rule = resolutionFinding(node.resolved)
    if (rule) add(rule, where || '(root)', node.resolved)
  }

  // Lockfile v1 keeps the same information under a different shape, and gets
  // the same judgement rather than a laxer one.
  const walkV1 = (node, prefix) => {
    for (const [name, dep] of Object.entries(node || {})) {
      if (null == dep || 'object' !== typeof dep) continue
      const where = prefix ? prefix + ' > ' + name : name
      for (const field of ['resolved', 'version']) {
        const rule = resolutionFinding(dep[field])
        if (rule) add(rule, where, dep[field])
      }
      walkV1(dep.dependencies, where)
    }
  }
  if (!json.packages) walkV1(json.dependencies, '')

  return findings
}


// yarn and pnpm lockfiles are read as text rather than parsed: the gate reports
// the forbidden markers it can see, so one of them cannot be a silent bypass.
function checkTextLockfile(file, text, config, seen) {
  const findings = []
  const allow = config.allow || {}
  const lines = text.split(/\r?\n/)

  const add = (rule, where, spec) => {
    const key = file + ':lock:' + where
    if (seen) seen.add(key)
    if (Object.prototype.hasOwnProperty.call(allow, key)) return
    findings.push({ rule, file, where, spec, key })
  }

  lines.forEach((raw, i) => {
    const line = raw.trim()
    if ('' === line || '#' === line[0]) return
    const local = /(^|[\s"':@,{[])(file|link|portal|git\+file):/i.exec(line)
    if (local) { add('lockfile-local-resolution', 'line ' + (i + 1), line.slice(0, 160)); return }
    const url = /\b[a-z+]+:\/\/\S+/i.exec(line)
    if (url) {
      const host = hostOf(url[0])
      if (null != host && NPM_REGISTRY_HOST !== host && !githubHost(host)) {
        add('lockfile-foreign-registry', 'line ' + (i + 1), line.slice(0, 160))
      }
    }
  })

  return findings
}


// Go quotes a replacement target only when it has to; strip the quotes before
// asking whether it is a path, or a valid `=> "./dir with spaces"` reads as a
// module name.
function unquoteGo(tok) {
  if (2 <= tok.length && '"' === tok[0] && '"' === tok[tok.length - 1]) {
    try { return JSON.parse(tok) } catch { return tok.slice(1, -1) }
  }
  return tok
}


function insideRepo(abs, root) {
  return abs === root || abs.startsWith(root + Path.sep)
}


function checkGoMod(file, text, config, root, seen, trackedPaths) {
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
  // tree uses the module beside it, and it travels with the checkout -- but
  // only if git tracks what it points at.
  const dir = Path.dirname(Path.join(REPO_ROOT, file))
  const relOf = (abs) => Path.relative(REPO_ROOT, abs).split(Path.sep).join('/')
  const tracksUnder = (abs) => {
    if (!trackedPaths) return true
    const prefix = '' === relOf(abs) ? '' : relOf(abs) + '/'
    for (const p of trackedPaths) if ('' === prefix || p.startsWith(prefix)) return true
    return false
  }

  const one = (body) => {
    const m = /^("(?:[^"\\]|\\.)*"|\S+)(?:\s+\S+)?\s*=>\s*("(?:[^"\\]|\\.)*"|\S+)(?:\s+(\S+))?/
      .exec(body.trim())
    if (!m) return
    const from = unquoteGo(m[1])
    const target = unquoteGo(m[2])

    if (absoluteish(target)) { add('go-absolute-path-replace', from, body.trim()); return }

    if (/^\.{1,2}[\\/]/.test(target)) {
      const abs = Path.resolve(dir, target)
      if (!insideRepo(abs, REPO_ROOT)) { add('go-external-path-replace', from, body.trim()); return }
      if (!tracksUnder(abs)) { add('go-untracked-path-replace', from, body.trim()); return }
      return
    }

    add('go-module-replace', from, body.trim())
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


function goWorkTargets(text) {
  const targets = []
  let inUse = false
  let inReplace = false

  for (const raw of text.split(/\r?\n/)) {
    const line = raw.replace(/\/\/.*$/, '').trim()
    if ('' === line) continue
    if (/^use\s*\($/.test(line)) { inUse = true; continue }
    if (/^replace\s*\($/.test(line)) { inReplace = true; continue }
    if ((inUse || inReplace) && ')' === line) { inUse = false; inReplace = false; continue }
    if (inUse) { targets.push(unquoteGo(line)); continue }
    if (inReplace) {
      const m = /=>\s*("(?:[^"\\]|\\.)*"|\S+)/.exec(line)
      if (m) targets.push(unquoteGo(m[1]))
      continue
    }
    const use = /^use\s+("(?:[^"\\]|\\.)*"|\S+)/.exec(line)
    if (use) { targets.push(unquoteGo(use[1])); continue }
    const rep = /^replace\s+.*=>\s*("(?:[^"\\]|\\.)*"|\S+)/.exec(line)
    if (rep) targets.push(unquoteGo(rep[1]))
  }

  return targets
}


// The FILE is the finding, whatever its members resolve to: a workspace belongs
// one level above the repositories it wires together, precisely so that no
// repository can track it. Its members are reported with it, because an
// all-internal one reads as harmless until you see what it names.
function checkGoWork(file, text, config, root, seen) {
  const allow = config.allow || {}
  const key = file + ':go-workspace'
  if (seen) seen.add(key)
  if (Object.prototype.hasOwnProperty.call(allow, key)) return []

  const targets = goWorkTargets(text)
  const spec = targets.length ? 'use ' + targets.join(' ') : 'a workspace with no members'
  return [{ rule: 'go-workspace', file, where: file, spec, key }]
}


// `[patch.*]` and `[replace]` redirect a dependency as surely as a `path` key in
// `[dependencies]` does, so they are dependency tables for this gate's purposes.
const CARGO_DEP_SECTION_RE =
  /^\[\s*(?:workspace\s*\.\s*)?(?:(?:target\s*\.\s*(?:"[^"]*"|'[^']*'|[^.\]]+)\s*\.\s*)?(?:dev-|build-)?dependencies(?:\s*\.\s*[^\]]+)?|patch(?:\s*\.\s*[^\]]+)?|replace)\s*\]$/

// Only dependency tables, and never a commented-out line: a `path` key in
// `[package]` or behind a `#` is not a dependency.
function checkCargoToml(file, text, config, root, seen) {
  const findings = []
  const allow = config.allow || {}
  const REPO_ROOT = root || REPO
  const dir = Path.dirname(Path.join(REPO_ROOT, file))
  let inDeps = false

  const add = (rule, where, spec) => {
    const key = file + ':cargo:' + where
    if (seen) seen.add(key)
    if (Object.prototype.hasOwnProperty.call(allow, key)) return
    findings.push({ rule, file, where, spec, key })
  }

  const str = (raw) => {
    const m = /^"((?:[^"\\]|\\.)*)"|^'([^']*)'/.exec(raw)
    if (!m) return null
    return undefined === m[1] ? m[2] : m[1].replace(/\\(.)/g, '$1')
  }

  for (const raw of text.split(/\r?\n/)) {
    const line = raw.replace(/(^|\s)#.*$/, '').trim()
    if ('' === line) continue
    if ('[' === line[0]) { inDeps = CARGO_DEP_SECTION_RE.test(line); continue }
    if (!inDeps) continue

    const pathRe = /\bpath\s*=\s*("(?:[^"\\]|\\.)*"|'[^']*')/g
    let m
    while (null !== (m = pathRe.exec(line))) {
      const rel = str(m[1])
      if (null == rel) continue
      if (absoluteish(rel)) { add('cargo-absolute-path-dep', rel, m[0]); continue }
      if (insideRepo(Path.resolve(dir, rel), REPO_ROOT)) continue
      add('cargo-external-path-dep', rel, m[0])
    }

    const gitRe = /\bgit\s*=\s*("(?:[^"\\]|\\.)*"|'[^']*')/g
    while (null !== (m = gitRe.exec(line))) {
      const url = str(m[1])
      if (null == url) continue
      if (githubHost(hostOf(url))) continue
      add('cargo-non-github-git-dep', url, m[0])
    }
  }

  return findings
}


function checkGitmodules(file, text, config, seen) {
  const findings = []
  const allow = config.allow || {}

  const add = (rule, where, spec) => {
    const key = file + ':submodule:' + where
    if (seen) seen.add(key)
    if (Object.prototype.hasOwnProperty.call(allow, key)) return
    findings.push({ rule, file, where, spec, key })
  }

  let name = ''
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.replace(/(^|\s)[;#].*$/, '').trim()
    if ('' === line) continue
    const sec = /^\[submodule\s+"?([^"\]]+)"?\]$/.exec(line)
    if (sec) { name = sec[1]; continue }
    const m = /^url\s*=\s*(.+)$/.exec(line)
    if (!m) continue
    const url = m[1].trim().replace(/^["']|["']$/g, '')
    const where = name || url
    if (/^file:/i.test(url) || localish(url)) { add('submodule-local-url', where, url); continue }
    if (!githubHost(hostOf(url))) add('submodule-non-github-url', where, url)
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


function indexBlob(root, rel) {
  return Child.execFileSync('git', ['-C', root, 'show', ':' + rel], {
    encoding: 'utf8', maxBuffer: 1 << 28,
  })
}


function checkAll(config, root) {
  const REPO_ROOT = root || REPO
  const findings = []
  const seenKeys = new Set()

  const rows = tracked(REPO_ROOT)
  const trackedPaths = new Set(rows.map((r) => r.path))
  const modeOf = new Map(rows.map((r) => [r.path, r.mode]))

  const cfg = config || readConfig(REPO_ROOT, trackedPaths)
  const allow = cfg.allow || {}
  const has = (key) => Object.prototype.hasOwnProperty.call(allow, key)

  const record = (list) => {
    for (const f of list) {
      findings.push(f)
      if (f.key) seenKeys.add(f.key)
    }
  }

  // Record every key the allowlist COULD have matched, so a stale entry is
  // detectable -- but only where the condition it excuses is actually present.
  const offer = (key) => seenKeys.add(key)

  const judge = (rel, base, text) => {
    if ('package.json' === base) return checkManifest(rel, JSON.parse(text), cfg, seenKeys)
    if ('package-lock.json' === base || 'npm-shrinkwrap.json' === base) {
      return checkLockfile(rel, JSON.parse(text), cfg, seenKeys)
    }
    if ('go.mod' === base) return checkGoMod(rel, text, cfg, REPO_ROOT, seenKeys, trackedPaths)
    if ('Cargo.toml' === base) return checkCargoToml(rel, text, cfg, REPO_ROOT, seenKeys)
    if ('.gitmodules' === base) return checkGitmodules(rel, text, cfg, seenKeys)
    if ('yarn.lock' === base || 'pnpm-lock.yaml' === base) {
      return checkTextLockfile(rel, text, cfg, seenKeys)
    }
    if ('.npmrc' === base) return checkNpmrc(rel, text, cfg, seenKeys)
    return []
  }

  function checkNpmrc(rel, text, conf, seen) {
    const out = []
    for (const line of text.split(/\r?\n/)) {
      const m = /^\s*(?:(@?[^;#\s:]*):)?registry\s*=\s*(.+?)\s*$/.exec(line)
      if (!m) continue
      const scope = m[1] || ''
      const value = m[2].replace(/^["']|["']$/g, '')
      if (NPM_REGISTRY_HOST === hostOf(value)) continue
      const key = rel + ':registry:' + (scope || '(default)')
      if (seen) seen.add(key)
      if (has(key)) continue
      out.push({
        rule: 'foreign-registry', file: rel, where: 'registry ' + (scope || '(default)'),
        spec: value, key,
      })
    }
    return out
  }

  for (const { mode, path: rel } of rows) {
    const base = Path.basename(rel)

    if ('120000' === mode) {
      let target = ''
      try { target = indexBlob(REPO_ROOT, rel).trim() }
      catch { target = '' }
      const key = rel + ':symlink'
      const abstarget = Path.resolve(Path.dirname(Path.join(REPO_ROOT, rel)), target)
      const escapes = !insideRepo(abstarget, REPO_ROOT)
      const intoModules = /(^|[\\/])node_modules([\\/]|$)/.test(target)

      if (absoluteish(target)) {
        offer(key)
        if (!has(key)) {
          findings.push({ rule: 'absolute-symlink', file: rel, where: 'symlink', spec: '-> ' + target, key })
        }
        continue
      }
      if (escapes || intoModules) {
        offer(key)
        if (!has(key)) {
          findings.push({ rule: 'escaping-symlink', file: rel, where: 'symlink', spec: '-> ' + target, key })
        }
        continue
      }

      // An in-repo link is fine, and its TARGET is still a dependency source if
      // the link is named like a manifest: npm reads through it.
      if (!MANIFEST_BASES.has(base) && 'yarn.lock' !== base && 'pnpm-lock.yaml' !== base) continue
      const targetRel = Path.relative(REPO_ROOT, abstarget).split(Path.sep).join('/')
      if (!trackedPaths.has(targetRel)) continue
      try { record(judge(rel, base, indexBlob(REPO_ROOT, targetRel))) }
      catch (err) {
        findings.push({ rule: 'unreadable', file: rel, where: 'parse', spec: err.message, key: null })
      }
      continue
    }

    if (ARCHIVE_RE.test(rel)) {
      const key = rel + ':archive'
      offer(key)
      if (!has(key)) {
        findings.push({ rule: 'committed-archive', file: rel, where: 'file', spec: base, key })
      }
      continue
    }

    if ('bun.lockb' === base) {
      const key = rel + ':lockfile'
      offer(key)
      if (!has(key)) {
        findings.push({ rule: 'unchecked-lockfile', file: rel, where: 'file', spec: base, key })
      }
      continue
    }

    try {
      if ('go.work' === base) record(checkGoWork(rel, indexBlob(REPO_ROOT, rel), cfg, REPO_ROOT, seenKeys))
      else if ('go.work.sum' === base) continue
      else if (MANIFEST_BASES.has(base) || 'yarn.lock' === base || 'pnpm-lock.yaml' === base) {
        record(judge(rel, base, indexBlob(REPO_ROOT, rel)))
      }
    }
    catch (err) {
      findings.push({
        rule: 'unreadable', file: rel, where: 'parse', spec: err.message, key: null,
      })
    }
  }

  // An allowlist is a liability once it outlives what it excused.
  for (const [key, reason] of Object.entries(allow)) {
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
  hostOf,
  checkManifest,
  checkLockfile,
  checkTextLockfile,
  checkGoMod,
  checkGoWork,
  checkCargoToml,
  checkGitmodules,
  checkAll,
  report,
  REPO,
  RULE,
  WHY,
}
