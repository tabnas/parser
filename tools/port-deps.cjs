#!/usr/bin/env node
// tabnas-port-deps-template v1: stamped by tabnas/admin tasks/adopt-port-deps.js. Change the template and restamp, never this file.
/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

/* port-deps.cjs: do this repository's ports depend on the same tabnas repos?
 *
 * A tabnas repo ships the same library up to three times: TypeScript (ts/),
 * Go (go/) and Rust (rs/). Each port declares its dependencies in its own
 * ecosystem's manifest, and nothing compared them, so a port could gain or
 * lose a dependency on another tabnas repo without the others noticing.
 * This file reads the three ports and compares them (ADR-24 in
 * tabnas/admin DECISIONS.md).
 *
 * Only tabnas-to-tabnas edges can be compared across ecosystems, and a
 * dependency is named by the repo it lives in:
 *   ts   @tabnas/<id>                 ts/package.json
 *   go   github.com/tabnas/<id>/go    go/go.mod, and the imports under go/
 *   rs   tabnas-<id>                  rs/Cargo.toml
 * The repo's own id comes from the same three names. A port's reference to
 * its own repo is dropped.
 *
 * What is compared is the RUNTIME dependency set of each port:
 *   ts   dependencies + peerDependencies + optionalDependencies
 *   rs   [dependencies] + [build-dependencies], and their [target.*] forms
 *   go   the tabnas modules that the non-test .go files under go/ import
 * Go is read from imports because go.mod cannot say which require is for
 * tests only: `support/go` is required by modules whose library never
 * imports it. devDependencies, [dev-dependencies] and imports made only by
 * _test.go files are reported and not compared. Whether a dependency is
 * optional is shown and not compared.
 *
 * A difference that exists today cannot be repaired here: changing a
 * dependency needs the maintainer's instruction (ADR-22). So each repo owns
 * a register, tools/port-deps.json, in the ADR-14 shape:
 *   { "note": "...",
 *     "differ": { "<id>": { "ports": ["rs", "ts"],
 *                           "reason": "...", "repair": "..." } } }
 * `ports` are the ports that have the dependency today, sorted. The check
 * fails on a difference the register does not record, and fails as loudly
 * on an entry the manifests no longer support, so a repaired difference
 * forces its entry's deletion.
 *
 * The readers fail closed. A manifest shape this file does not understand
 * throws an Error that names the file and line; it is never read as "no
 * dependency". The one lenient spot is go.mod, where a directive other
 * than `module` and `require` is skipped: Go's dependencies are read from
 * imports, so no go.mod directive can hide one.
 *
 * Zero dependencies, node: built-ins only, and nothing newer than Node 24:
 * the per-repo test runs this file wherever the repo's own suite runs,
 * Windows runners included, before anything is installed.
 *
 * Usage:
 *   node tools/port-deps.cjs [--json] [DIR]
 *
 *   DIR      the repository to read (default: the one holding this file)
 *   --json   print the result as JSON rather than as a table
 *   -h, --help
 *
 * Exits 0 when the ports agree or every difference is registered, 1 when
 * there is a problem, 2 when a manifest or the register cannot be read or
 * DIR is not a directory.
 */

'use strict'

const Fs = require('node:fs')
const Path = require('node:path')

const ORG = 'tabnas'
const PORTS = ['ts', 'go', 'rs']
const REGISTER = 'tools/port-deps.json'

const MANIFEST = { ts: 'ts/package.json', go: 'go/go.mod', rs: 'rs/Cargo.toml' }
const NAMING = {
  ts: `@${ORG}/<id>`,
  go: `github.com/${ORG}/<id>/go`,
  rs: `${ORG}-<id>`,
}

// An id is whatever stands in the <id> place: every name in the org's
// namespace is a tabnas dependency. A stricter pattern would read a name it
// did not expect as somebody else's package, and drop it from the comparison.
const ID = '[^/\\s]+'
const NPM_RE = new RegExp(`^@${ORG}/(${ID})$`)
const CRATE_RE = new RegExp(`^${ORG}-(${ID})$`)
const GO_ORG = `github.com/${ORG}/`
const GO_MODULE_RE = new RegExp(`^github\\.com/${ORG}/(${ID})/go$`)
const GO_IMPORT_RE = new RegExp(`^github\\.com/${ORG}/(${ID})/go(?:/|$)`)

// vendor and testdata: the go tool never descends into them. node_modules:
// skipped by decision (ADR-24), although the go command would build Go code
// placed there; no fleet repo has any.
const GO_SKIP_DIRS = new Set(['vendor', 'node_modules', 'testdata'])

// ---------------------------------------------------------------------------
// Shared helpers

// Every reader error carries the file (relative to the repo, with forward
// slashes, so the message is the same on every machine) and the line.
function unreadable(file, line, message) {
  const err = new Error(`${file}:${line}: ${message}`)
  err.code = 'PORT_DEPS_UNREADABLE'
  err.file = file
  err.line = line
  return err
}

// Text as the parsers want it: no byte order mark, LF line endings. A
// Windows checkout hands over CRLF, and none of the three formats gives a
// carriage return before a newline any meaning.
function readText(dir, rel) {
  const text = Fs.readFileSync(Path.join(dir, ...rel.split('/')), 'utf8')
  return (0xfeff === text.charCodeAt(0) ? text.slice(1) : text).replace(/\r\n/g, '\n')
}

function exists(dir, rel) {
  return Fs.existsSync(Path.join(dir, ...rel.split('/')))
}

// Sorted by UTF-16 code unit, which no locale can change.
function uniq(list) {
  return [...new Set(list)].sort()
}

function isObject(val) {
  return null !== val && 'object' === typeof val && !Array.isArray(val)
}

function own(obj, key) {
  return Object.prototype.hasOwnProperty.call(obj, key)
}

function idOf(re, name) {
  const m = re.exec(name)
  return m ? m[1] : null
}

// Fold a port's declarations into its dependency lists.
//   runtime  [{ name, optional }]   dev  [name]
// A dependency is optional only when every runtime declaration of it is.
function fold(port, re, runtime, dev) {
  const ids = new Map()
  const external = []
  for (const decl of runtime) {
    const id = idOf(re, decl.name)
    if (null === id) external.push(decl.name)
    else if (id !== port.id) {
      ids.set(id, (ids.has(id) ? ids.get(id) : true) && decl.optional)
    }
  }
  port.runtime = uniq(ids.keys())
  port.optional = port.runtime.filter((id) => ids.get(id))
  port.dev = uniq(dev.map((name) => idOf(re, name))
    .filter((id) => null !== id && id !== port.id && !ids.has(id)))
  port.external = uniq(external)
  return port
}

// ---------------------------------------------------------------------------
// ts: ts/package.json

function jsonLine(text, err) {
  const msg = String(err && err.message)
  const at = /\(line (\d+) column \d+\)/.exec(msg)
  if (at) return Number(at[1])
  const pos = /position (\d+)/.exec(msg)
  return pos ? text.slice(0, Number(pos[1])).split('\n').length : 1
}

// The line a top-level key sits on, for an error message. A guess is good
// enough: the message also names the key.
function keyLine(text, key) {
  const at = text.search(new RegExp(`"${key}"\\s*:`))
  return at < 0 ? 1 : text.slice(0, at).split('\n').length
}

// `"alias": "npm:@scope/real@^1"` installs @scope/real under another name.
// The dependency is the real package.
function npmName(key, spec) {
  if (!spec.startsWith('npm:')) return key
  const rest = spec.slice(4)
  const at = rest.lastIndexOf('@')
  return at > 0 ? rest.slice(0, at) : rest
}

function readTs(dir) {
  const file = MANIFEST.ts
  const text = readText(dir, file)
  let pkg
  try {
    pkg = JSON.parse(text)
  } catch (err) {
    throw unreadable(file, jsonLine(text, err), `not JSON: ${err.message}`)
  }
  if (!isObject(pkg)) throw unreadable(file, 1, 'the manifest is not a JSON object')
  if ('string' !== typeof pkg.name || '' === pkg.name) {
    throw unreadable(file, keyLine(text, 'name'),
      'no "name", so the port cannot be named')
  }

  const section = (key) => {
    const val = pkg[key]
    if (undefined === val) return {}
    if (!isObject(val)) {
      throw unreadable(file, keyLine(text, key),
        `"${key}" is not an object of package names, so its dependencies cannot be read`)
    }
    for (const name of Object.keys(val)) {
      if ('string' !== typeof val[name]) {
        throw unreadable(file, keyLine(text, key),
          `"${key}" gives "${name}" something other than a version string`)
      }
    }
    return val
  }
  const deps = section('dependencies')
  const peer = section('peerDependencies')
  const opt = section('optionalDependencies')
  const dev = section('devDependencies')

  const meta = undefined === pkg.peerDependenciesMeta ? {} : pkg.peerDependenciesMeta
  if (!isObject(meta) || Object.keys(meta).some((name) => !isObject(meta[name]))) {
    throw unreadable(file, keyLine(text, 'peerDependenciesMeta'),
      '"peerDependenciesMeta" is not an object of objects, so optional peers cannot be read')
  }
  // npm takes any truthy "optional" as true. Rather than guess at what
  // "yes" or 1 was meant to say, ask for the boolean.
  for (const name of Object.keys(meta)) {
    if (own(meta[name], 'optional') && 'boolean' !== typeof meta[name].optional) {
      throw unreadable(file, keyLine(text, 'peerDependenciesMeta'),
        `"peerDependenciesMeta" gives "${name}" an "optional" that is not true or false`)
    }
  }

  // npm installs an optional dependency if it can and carries on if it
  // cannot. A peer marked optional is the same, unless "dependencies" also
  // names it, which installs it regardless.
  const optional = (name) => own(opt, name) ||
    (own(peer, name) && true === meta[name]?.optional && !own(deps, name))

  const runtime = []
  for (const sec of [deps, peer, opt]) {
    for (const name of Object.keys(sec)) {
      runtime.push({ name: npmName(name, sec[name]), optional: optional(name) })
    }
  }
  const port = { manifest: file, name: pkg.name, id: idOf(NPM_RE, pkg.name) }
  return fold(port, NPM_RE, runtime,
    Object.keys(dev).map((name) => npmName(name, dev[name])))
}

// ---------------------------------------------------------------------------
// rs: rs/Cargo.toml
//
// A real TOML reader rather than a line matcher, because Cargo accepts a
// dependency in five spellings ([dependencies] with a string or an inline
// table, [dependencies.<name>], a dotted key, and all of those again under
// [target.<cfg>]) and a line matcher reads the ones it was not written for
// as "no dependency". Scalars other than strings and booleans are kept as
// raw text: nothing here needs a number or a date.
//
// Nodes: { t: 'table', line, map, defined, inline }   { t: 'aot', line, items }
//        { t: 'array', line, items }   { t: 'string' | 'bool' | 'other', line, v }

function parseToml(text, file) {
  let i = 0
  let line = 1
  const n = text.length
  const fail = (message, at) => { throw unreadable(file, at || line, message) }
  const table = (at, flags) =>
    ({ t: 'table', line: at, map: new Map(), defined: false, inline: false, ...flags })

  const blank = () => { while (i < n && (' ' === text[i] || '\t' === text[i])) i++ }
  const comment = () => { if ('#' === text[i]) while (i < n && '\n' !== text[i]) i++ }
  // Whitespace, comments and newlines: between statements, inside arrays.
  const gap = () => {
    for (;;) {
      blank()
      comment()
      if (i < n && '\n' === text[i]) { i++; line++ } else return
    }
  }
  const endOfLine = () => {
    blank()
    comment()
    if (i >= n) return
    if ('\n' !== text[i]) fail(`unexpected ${JSON.stringify(text[i])} after a complete statement`)
    i++
    line++
  }

  const escape = () => {
    // text[i] is the character after the backslash.
    const c = text[i++]
    const simple = { b: '\b', t: '\t', n: '\n', f: '\f', r: '\r', e: '\x1b', '"': '"', '\\': '\\' }
    if (own(simple, c)) return simple[c]
    const width = { x: 2, u: 4, U: 8 }[c]
    if (width) {
      const hex = text.slice(i, i + width)
      if (!new RegExp(`^[0-9A-Fa-f]{${width}}$`).test(hex)) fail(`a malformed \\${c} escape`)
      i += width
      const code = parseInt(hex, 16)
      if (code > 0x10ffff) fail(`a \\${c} escape beyond Unicode`)
      return String.fromCodePoint(code)
    }
    return fail(`an unknown string escape \\${c}`)
  }

  const string = () => {
    const start = line
    const quote = text[i]
    if (text.startsWith(quote.repeat(3), i)) {
      i += 3
      if ('\n' === text[i]) { i++; line++ }
      let out = ''
      for (;;) {
        if (i >= n) fail('a multi-line string that never closes', start)
        if (text.startsWith(quote.repeat(3), i)) {
          i += 3
          // Up to two more quotes belong to the string itself.
          for (let extra = 0; extra < 2 && text[i] === quote; extra++) { out += quote; i++ }
          return { t: 'string', line: start, v: out }
        }
        const c = text[i++]
        if ('\n' === c) { line++; out += c }
        else if ('\\' === c && '"' === quote) {
          // A backslash at a line end swallows the newline and what follows.
          const rest = /^[ \t]*\n/.exec(text.slice(i, i + 200))
          if (rest) {
            while (i < n && /\s/.test(text[i])) { if ('\n' === text[i]) line++; i++ }
          } else out += escape()
        } else out += c
      }
    }
    i++
    let out = ''
    for (;;) {
      if (i >= n || '\n' === text[i]) fail('a string that does not close on its line', start)
      const c = text[i++]
      if (c === quote) return { t: 'string', line: start, v: out }
      out += '\\' === c && '"' === quote ? escape() : c
    }
  }

  const key = () => {
    const parts = []
    for (;;) {
      blank()
      if ('"' === text[i] || "'" === text[i]) {
        if (text.startsWith(text[i].repeat(3), i)) fail('a multi-line string used as a key')
        parts.push(string().v)
      } else {
        const m = /^[A-Za-z0-9_-]+/.exec(text.slice(i, i + 256))
        if (!m) fail(i < n ? `expected a key, found ${JSON.stringify(text[i])}` : 'expected a key, found the end of the file')
        parts.push(m[0])
        i += m[0].length
      }
      blank()
      if ('.' !== text[i]) return parts
      i++
    }
  }

  // Walk `parts` down from `from`, creating tables on the way, and set the
  // last one to `node`.
  const assign = (from, parts, node) => {
    let at = from
    for (const part of parts.slice(0, -1)) {
      let next = at.map.get(part)
      if (!next) {
        next = table(node.line, { defined: true })
        at.map.set(part, next)
      } else if ('table' !== next.t || next.inline) {
        fail(`the key ${JSON.stringify(part)} is already a value, so it cannot hold ${JSON.stringify(parts[parts.length - 1])}`, node.line)
      }
      at = next
    }
    const last = parts[parts.length - 1]
    if (at.map.has(last)) fail(`the key ${JSON.stringify(parts.join('.'))} is set twice`, node.line)
    at.map.set(last, node)
  }

  const value = () => {
    const start = line
    const c = text[i]
    if ('"' === c || "'" === c) return string()
    if ('[' === c) {
      i++
      const items = []
      for (;;) {
        gap()
        if (i >= n) fail('an array that never closes', start)
        if (']' === text[i]) { i++; return { t: 'array', line: start, items } }
        items.push(value())
        gap()
        if (',' === text[i]) i++
        else if (i < n && ']' !== text[i]) fail('expected "," or "]" in an array')
      }
    }
    if ('{' === c) {
      i++
      const node = table(start, { defined: true, inline: true })
      for (;;) {
        gap()
        if (i >= n) fail('an inline table that never closes', start)
        if ('}' === text[i]) { i++; return node }
        const parts = key()
        if ('=' !== text[i]) fail('expected "=" after a key in an inline table')
        i++
        blank()
        assign(node, parts, value())
        gap()
        if (',' === text[i]) i++
        else if (i < n && '}' !== text[i]) fail('expected "," or "}" in an inline table')
      }
    }
    const m = /^[A-Za-z0-9+\-_.:]+/.exec(text.slice(i, i + 256))
    if (!m) fail(i < n && '\n' !== c ? `a value starting with ${JSON.stringify(c)} is not understood` : 'a key with no value')
    i += m[0].length
    let raw = m[0]
    // A date and a time may be separated by one space.
    if (/^\d{4}-\d\d-\d\d$/.test(raw)) {
      const time = /^ \d\d:[0-9:.+\-Zz]+/.exec(text.slice(i, i + 64))
      if (time) { raw += time[0]; i += time[0].length }
    }
    if ('true' === raw || 'false' === raw) return { t: 'bool', line: start, v: 'true' === raw }
    return { t: 'other', line: start, v: raw }
  }

  const root = table(1, { defined: true })
  let current = root

  const header = () => {
    const start = line
    const array = text.startsWith('[[', i)
    i += array ? 2 : 1
    const parts = key()
    if (!text.startsWith(array ? ']]' : ']', i)) fail('a table header that does not close')
    i += array ? 2 : 1
    endOfLine()

    let at = root
    for (const part of parts.slice(0, -1)) {
      let next = at.map.get(part)
      if (!next) {
        next = table(start)
        at.map.set(part, next)
      }
      if ('aot' === next.t) next = next.items[next.items.length - 1]
      if ('table' !== next.t || next.inline) {
        fail(`[${parts.join('.')}]: ${JSON.stringify(part)} is already a value, not a table`, start)
      }
      at = next
    }
    const last = parts[parts.length - 1]
    const seen = at.map.get(last)
    if (array) {
      const item = table(start, { defined: true })
      if (!seen) at.map.set(last, { t: 'aot', line: start, items: [item] })
      else if ('aot' === seen.t) seen.items.push(item)
      else fail(`[[${parts.join('.')}]] is already defined as something else`, start)
      current = item
    } else if (!seen) {
      current = table(start, { defined: true })
      at.map.set(last, current)
    } else if ('table' === seen.t && !seen.defined) {
      // Made on the way to a deeper header; this is its own header.
      seen.defined = true
      current = seen
    } else {
      fail(`the table [${parts.join('.')}] is defined twice`, start)
    }
  }

  for (;;) {
    gap()
    if (i >= n) return root
    if ('[' === text[i]) { header(); continue }
    const start = line
    const parts = key()
    if ('=' !== text[i]) fail(`expected "=" after the key ${JSON.stringify(parts.join('.'))}`)
    i++
    blank()
    const val = value()
    val.line = start
    assign(current, parts, val)
    endOfLine()
  }
}

// Each kind of dependency table, by its spelling and, where there is one,
// the underscore spelling that Cargo still reads before the 2024 edition:
// only when the hyphen table of the same kind is absent from that scope.
// The 2024 edition refuses the underscore spelling.
const CARGO_TABLES = [
  { key: 'dependencies', dev: false },
  { key: 'build-dependencies', old: 'build_dependencies', dev: false },
  { key: 'dev-dependencies', old: 'dev_dependencies', dev: true },
]

function readRs(dir) {
  const file = MANIFEST.rs
  const doc = parseToml(readText(dir, file), file)
  const fail = (line, message) => { throw unreadable(file, line, message) }

  const pkg = doc.map.get('package')
  if (!pkg || 'table' !== pkg.t) {
    fail(1, 'no [package] table: a manifest that is only a workspace is not a port this reader understands')
  }
  const name = pkg.map.get('name')
  if (!name || 'string' !== name.t || '' === name.v) {
    fail(name ? name.line : pkg.line, '[package] has no name string, so the port cannot be named')
  }

  const runtime = []
  const dev = []

  // One dependency table: `<name> = "1"`, `<name> = { ... }`, or the
  // [<table>.<name>] and dotted-key forms, which the parser has already
  // turned into the same table node.
  const entries = (node, where, isDev) => {
    if ('table' !== node.t) fail(node.line, `${where} is not a table, so its dependencies cannot be read`)
    for (const [key, dep] of node.map) {
      let crate = key
      let optional = false
      if ('table' === dep.t) {
        const inherit = dep.map.get('workspace')
        if (inherit) {
          fail(inherit.line, `${where}: "${key}" is inherited from a workspace manifest, which this reader does not follow`)
        }
        const rename = dep.map.get('package')
        if (rename) {
          if ('string' !== rename.t) fail(rename.line, `${where}: "${key}" has a package that is not a string`)
          crate = rename.v
        }
        const flag = dep.map.get('optional')
        if (flag) {
          if ('bool' !== flag.t) fail(flag.line, `${where}: "${key}" has an optional that is not true or false`)
          optional = flag.v
        }
      } else if ('string' !== dep.t) {
        fail(dep.line, `${where}: "${key}" is neither a version string nor a table`)
      }
      if (isDev) dev.push(crate)
      else runtime.push({ name: crate, optional })
    }
  }
  const edition = pkg.map.get('edition')
  const modern = edition && 'string' === edition.t && edition.v >= '2024'
  const sections = (node, prefix) => {
    for (const { key, old, dev: isDev } of CARGO_TABLES) {
      if (old && node.map.has(old) && modern) {
        fail(node.map.get(old).line, `[${prefix}${old}] is refused by Cargo from the 2024 edition: it is spelled [${prefix}${key}]`)
      }
      const use = node.map.has(key) ? key : old && node.map.has(old) ? old : null
      if (null !== use) entries(node.map.get(use), `[${prefix}${use}]`, isDev)
    }
  }

  sections(doc, '')
  const target = doc.map.get('target')
  if (target) {
    if ('table' !== target.t) fail(target.line, '"target" is not a table, so its dependencies cannot be read')
    for (const [cfg, node] of target.map) {
      if ('table' !== node.t) fail(node.line, `[target.${cfg}] is not a table, so its dependencies cannot be read`)
      sections(node, `target.${cfg}.`)
    }
  }

  const port = { manifest: file, name: name.v, id: idOf(CRATE_RE, name.v) }
  return fold(port, CRATE_RE, runtime, dev)
}

// ---------------------------------------------------------------------------
// go: go/go.mod and the import declarations under go/

function parseGoMod(text, file) {
  let module = null
  let block = null
  let opened = 0
  const requires = []
  const lines = text.split('\n')

  for (let n = 0; n < lines.length; n++) {
    const at = n + 1
    const fail = (message) => { throw unreadable(file, at, message) }
    const src = lines[n]
    const tokens = []
    let note = ''
    for (let i = 0; i < src.length;) {
      const c = src[i]
      if (' ' === c || '\t' === c || '\r' === c) { i++; continue }
      if (src.startsWith('//', i)) { note = src.slice(i + 2).trim(); break }
      if ('(' === c || ')' === c) { tokens.push(c); i++; continue }
      if ('"' === c || '`' === c) {
        const end = src.indexOf(c, i + 1)
        if (end < 0) fail('a quoted string that does not close on its line')
        const str = src.slice(i + 1, end)
        if ('"' === c && str.includes('\\')) fail('an escaped string, which this reader does not decode')
        tokens.push(str)
        i = end + 1
        continue
      }
      let j = i
      while (j < src.length && !/[\s()]/.test(src[j]) && !src.startsWith('//', j)) j++
      tokens.push(src.slice(i, j))
      i = j
    }
    if (0 === tokens.length) continue

    let verb = block
    let args = tokens
    if (null !== block) {
      if (')' === tokens[0]) {
        if (1 !== tokens.length) fail('text after the ")" that closes a block')
        block = null
        continue
      }
    } else {
      verb = tokens[0]
      args = tokens.slice(1)
      if ('(' === args[0]) {
        if (1 !== args.length) fail(`text after "${verb} ("`)
        block = verb
        opened = at
        continue
      }
    }

    if ('module' === verb) {
      if (1 !== args.length) fail('a module directive that is not "module <path>"')
      if (null !== module) fail('a second module directive')
      module = args[0]
    } else if ('require' === verb) {
      if (2 !== args.length) fail('a require that is not "<module path> <version>"')
      requires.push({ path: args[0], indirect: /^indirect(\s*;.*)?$/.test(note) })
    }
    // Any other directive (go, toolchain, replace, exclude, retract, tool,
    // godebug, ignore, or one a later Go adds) is not read: runtime
    // dependencies come from imports, so none of them can hide one.
  }
  if (null !== block) throw unreadable(file, opened, `the "${block} (" block never closes`)
  if (null === module) throw unreadable(file, 1, 'no module directive, so the port cannot be named')
  return { module, requires }
}

// Is the file's build constraint exactly `ignore`? Decided line by line, as
// go/build does it (parseFileHeader and shouldBuild), because a looser
// reading skips files that Go compiles and so hides their imports:
//   - The header is the lines before the first non-comment text, with /* */
//     comments tracked across lines.
//   - A `//go:build` line counts when the line itself starts with it,
//     outside a block comment, anywhere in the header. When there is one,
//     it alone decides, and `// +build` lines are not consulted. Two of
//     them is a file Go refuses, so it is refused here too.
//   - Otherwise a `// +build` line counts only in the run of `//` comments
//     and blank lines that ends with the last blank line before the first
//     line that is not a `//` comment: a `+build` line in the package's doc
//     comment, or after a block comment, is just text.
function goIgnored(text, file) {
  const lines = text.split('\n')
  const trim = (s) => s.replace(/^[ \t\r\v\f]+|[ \t\r\v\f]+$/g, '')
  let end = 0
  let ended = false
  let inStar = false
  let goBuild = null
  header: for (let k = 0; k < lines.length; k++) {
    let line = trim(lines[k])
    if ('' === line && !ended) { end = k + 1; continue }
    if (!line.startsWith('//')) ended = true
    if (!inStar && /^\/\/go:build(?:[ \t]|$)/.test(line)) {
      if (null !== goBuild) throw unreadable(file, k + 1, 'a second //go:build line, which Go refuses')
      goBuild = line
    }
    while ('' !== line) {
      if (inStar) {
        const close = line.indexOf('*/')
        if (close < 0) continue header
        inStar = false
        line = trim(line.slice(close + 2))
        continue
      }
      if (line.startsWith('//')) continue header
      if (line.startsWith('/*')) {
        inStar = true
        line = trim(line.slice(2))
        continue
      }
      break header
    }
  }
  if (null !== goBuild) return 'ignore' === trim(goBuild.slice('//go:build'.length))
  for (const raw of lines.slice(0, end)) {
    const line = trim(raw)
    if (!line.startsWith('//')) continue
    const m = /^\+build(?:[ \t](.*))?$/.exec(trim(line.slice(2)))
    if (m && 'ignore' === trim(m[1] || '')) return true
  }
  return false
}

// The import paths of one Go file. Import declarations come after the
// package clause and before every other declaration, so this reads tokens
// up to the first one that is neither and stops: the rest of the file,
// whatever strings and comments it holds, is never looked at.
function goImports(text, file) {
  let i = 0
  let line = 1
  const n = text.length
  const fail = (message, at) => { throw unreadable(file, at || line, message) }

  const next = () => {
    for (;;) {
      const c = text[i]
      if (i >= n) return { t: 'eof', line }
      if ('\n' === c) { line++; i++; continue }
      if (' ' === c || '\t' === c || '\r' === c) { i++; continue }
      if (text.startsWith('//', i)) {
        while (i < n && '\n' !== text[i]) i++
        continue
      }
      if (text.startsWith('/*', i)) {
        const start = line
        const end = text.indexOf('*/', i + 2)
        if (end < 0) fail('a comment that never closes', start)
        for (let j = i; j < end; j++) if ('\n' === text[j]) line++
        i = end + 2
        continue
      }
      break
    }
    const start = line
    const c = text[i]
    if ('"' === c) {
      let j = i + 1
      while (j < n && '"' !== text[j] && '\n' !== text[j]) {
        if ('\\' === text[j]) fail('an import path with an escape, which this reader does not decode')
        j++
      }
      if ('"' !== text[j]) fail('a string that does not close on its line')
      const v = text.slice(i + 1, j)
      i = j + 1
      return { t: 'string', v, line: start }
    }
    if ('`' === c) {
      const end = text.indexOf('`', i + 1)
      if (end < 0) fail('a raw string that never closes')
      const v = text.slice(i + 1, end)
      for (const ch of v) if ('\n' === ch) line++
      i = end + 1
      return { t: 'string', v, line: start }
    }
    const word = /^[\p{L}_][\p{L}\p{Nd}_]*/u.exec(text.slice(i, i + 256))
    if (word) {
      i += word[0].length
      return { t: 'word', v: word[0], line: start }
    }
    i++
    return { t: 'punct', v: c, line: start }
  }
  const is = (tok, t, v) => tok.t === t && (undefined === v || tok.v === v)

  let tok = next()
  if (!is(tok, 'word', 'package')) fail('a Go file that does not start with a package clause', tok.line)
  tok = next()
  if (!is(tok, 'word')) fail('a package clause with no name', tok.line)

  const out = []
  const spec = (first) => {
    let at = first
    // An alias: a name, `_` (blank) or `.` (dot).
    if (is(at, 'word') || is(at, 'punct', '.')) at = next()
    if (!is(at, 'string')) fail('an import declaration this reader does not understand', at.line)
    out.push({ path: at.v, line: at.line })
  }
  for (;;) {
    tok = next()
    if (is(tok, 'punct', ';')) continue
    if (!is(tok, 'word', 'import')) return out
    tok = next()
    if (!is(tok, 'punct', '(')) { spec(tok); continue }
    for (;;) {
      tok = next()
      if (is(tok, 'punct', ')')) break
      if (is(tok, 'punct', ';')) continue
      if (is(tok, 'eof')) fail('an import group that never closes', tok.line)
      spec(tok)
    }
  }
}

// The .go files of the module rooted at go/, close to how the go tool sees
// them: every file and directory whose name starts with "." or "_" is
// ignored, and the walk stops at vendor, testdata, node_modules and another
// module. No symlink is followed. The go command does not walk a symlinked
// directory, but it does read a symlinked .go file; a published module zip
// holds no symlink at all, so this is the module its consumers get.
function goFiles(dir, rel, out) {
  const entries = Fs.readdirSync(Path.join(dir, ...rel.split('/')), { withFileTypes: true })
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
  for (const entry of entries) {
    const name = entry.name
    const child = `${rel}/${name}`
    if (entry.isSymbolicLink()) continue
    if (name.startsWith('.') || name.startsWith('_')) continue
    if (entry.isDirectory()) {
      if (GO_SKIP_DIRS.has(name)) continue
      // A directory with its own go.mod is another module.
      if (exists(dir, `${child}/go.mod`)) continue
      goFiles(dir, child, out)
    } else if (entry.isFile() && name.endsWith('.go')) {
      out.push(child)
    }
  }
  return out
}

function readGo(dir) {
  const file = MANIFEST.go
  const mod = parseGoMod(readText(dir, file), file)
  const port = { manifest: file, name: mod.module, id: idOf(GO_MODULE_RE, mod.module) }

  const runtime = new Set()
  const tests = new Set()
  const external = []
  for (const rel of goFiles(dir, 'go', [])) {
    const text = readText(dir, rel)
    if (goIgnored(text, rel)) continue
    const isTest = rel.endsWith('_test.go')
    for (const imp of goImports(text, rel)) {
      const path = imp.path
      // The module's own packages: a reference to the repo itself.
      if (path === mod.module || path.startsWith(mod.module + '/')) continue
      if (path.startsWith(GO_ORG)) {
        const m = GO_IMPORT_RE.exec(path)
        if (!m) {
          throw unreadable(rel, imp.line,
            `the import "${path}" is in the ${ORG} org but is not under ${NAMING.go}, so this reader cannot name its repo`)
        }
        ;(isTest ? tests : runtime).add(m[1])
        continue
      }
      if (isTest) continue
      // The standard library has no dot in its first element.
      if (!path.split('/')[0].includes('.')) continue
      let best = null
      for (const req of mod.requires) {
        if ((path === req.path || path.startsWith(req.path + '/')) &&
          (null === best || req.path.length > best.path.length)) best = req
      }
      if (best && !best.indirect) external.push(best.path)
    }
  }

  port.runtime = uniq(runtime)
  port.optional = []
  port.dev = uniq(tests).filter((id) => !runtime.has(id))
  port.external = uniq(external)
  return port
}

// ---------------------------------------------------------------------------
// The repo, its register, and the comparison

const READERS = { ts: readTs, go: readGo, rs: readRs }

function readRepo(dir) {
  const ports = {}
  for (const port of PORTS) {
    ports[port] = exists(dir, MANIFEST[port]) ? READERS[port](dir) : null
  }
  const ids = uniq(PORTS.filter((p) => ports[p]).map((p) => ports[p].id))
  return { id: 1 === ids.length ? ids[0] : null, ports }
}

function readRegister(dir) {
  let text
  try {
    text = readText(dir, REGISTER)
  } catch (err) {
    if ('ENOENT' === err.code) return { differ: {} }
    throw err
  }
  try {
    return JSON.parse(text)
  } catch (err) {
    throw unreadable(REGISTER, jsonLine(text, err), `not JSON: ${err.message}`)
  }
}

const list = (ports) => JSON.stringify(ports)

function compare(read, register) {
  const ports = PORTS.filter((p) => read.ports[p])
  const sorted = [...ports].sort()
  const problems = []

  // The names. Each port must name a repo, and the same one.
  for (const p of ports) {
    const port = read.ports[p]
    if (null === port.id) {
      problems.push(`${port.manifest}: the name "${port.name}" is not ${NAMING[p]}, ` +
        `so the port names no repo. Name it ${NAMING[p]}.`)
    }
  }
  const named = ports.filter((p) => null !== read.ports[p].id)
  if (new Set(named.map((p) => read.ports[p].id)).size > 1) {
    problems.push('the ports disagree on the repo id: ' +
      named.map((p) => `${read.ports[p].manifest} says "${read.ports[p].id}" (${read.ports[p].name})`).join(', ') +
      `. Name every port after the one repo: ${PORTS.map((p) => NAMING[p]).join(', ')}.`)
  }

  // The register's shape. A register that cannot be trusted records nothing.
  let differ = {}
  if (!isObject(register)) {
    problems.push(`${REGISTER}: not a JSON object. Write { "note": "...", "differ": { ... } }.`)
  } else {
    for (const key of Object.keys(register).sort()) {
      if ('note' !== key && 'differ' !== key) {
        problems.push(`${REGISTER}: the key "${key}" is not part of the register. Keep only "note" and "differ".`)
      }
    }
    if (own(register, 'note') && 'string' !== typeof register.note) {
      problems.push(`${REGISTER}: "note" is not a string. Make it one, or delete it.`)
    }
    if (!isObject(register.differ)) {
      problems.push(`${REGISTER}: "differ" is not an object. Write "differ": {} when no difference is recorded.`)
    } else {
      differ = register.differ
    }
  }

  const ids = uniq(ports.flatMap((p) => read.ports[p].runtime))
  const rows = ids.map((id) => ({
    id,
    in: sorted.filter((p) => read.ports[p].runtime.includes(id)),
    optional: sorted.filter((p) => read.ports[p].runtime.includes(id) &&
      read.ports[p].optional.includes(id)),
    registered: own(differ, id),
  }))
  // With one port every id is in "all" of them, so nothing differs.
  const differences = rows.filter((row) => row.in.length !== ports.length)

  for (const id of uniq([...ids, ...Object.keys(differ)])) {
    const row = rows.find((r) => r.id === id)
    const have = row ? row.in : []
    const differs = differences.includes(row)

    if (!own(differ, id)) {
      if (differs) {
        const lack = sorted.filter((p) => !have.includes(p))
        problems.push(`${id}: a runtime dependency of ${have.join(', ')} but not of ${lack.join(', ')}, ` +
          `and ${REGISTER} does not record it. Make the ports agree (a dependency change, which needs ` +
          `the maintainer's instruction: ADR-22), or add "${id}" to "differ" with "ports": ${list(have)}, ` +
          'a "reason" and a "repair".')
      }
      continue
    }

    const entry = differ[id]
    if (!differs) {
      const why = ports.length < 2
        ? `this repo has ${1 === ports.length ? 'one port' : 'no port'}, so nothing is compared`
        : 0 === have.length ? 'no port has it as a runtime dependency now'
          : `every port (${ports.join(', ')}) has it now: the difference is repaired`
      problems.push(`${id}: ${REGISTER} records a difference, but ${why}. Delete the entry.`)
      continue
    }
    if (!isObject(entry)) {
      problems.push(`${id}: the ${REGISTER} entry is not an object. ` +
        `Write { "ports": ${list(have)}, "reason": "...", "repair": "..." }.`)
      continue
    }
    for (const key of Object.keys(entry).sort()) {
      if (!['ports', 'reason', 'repair'].includes(key)) {
        problems.push(`${id}: the ${REGISTER} entry has a key "${key}" that is not part of the register. ` +
          'Keep only "ports", "reason" and "repair".')
      }
    }
    if (list(entry.ports) !== list(have)) {
      problems.push(`${id}: ${REGISTER} records it in ${own(entry, 'ports') ? list(entry.ports) : 'no ports'}, ` +
        `but the manifests have it in ${list(have)}. Set "ports" to ${list(have)} ` +
        'and check that the reason and the repair still hold.')
    }
    if ('string' !== typeof entry.reason || '' === entry.reason.trim()) {
      problems.push(`${id}: the ${REGISTER} entry has no "reason". Say why the ports differ, as a non-empty string.`)
    }
    if ('string' !== typeof entry.repair || '' === entry.repair.trim()) {
      problems.push(`${id}: the ${REGISTER} entry has no "repair". State the repair direction (ADR-13): ` +
        'which port changes, or "undecided: awaits the maintainer\'s ruling".')
    }
  }

  return { ports, rows, differences, problems }
}

function render(read, out) {
  const lines = []
  const ports = out.ports
  lines.push(ports.length
    ? `port dependencies of ${read.id || '(no single repo id)'}: ` +
      ports.map((p) => `${p} ${read.ports[p].name}`).join(', ')
    : `port dependencies: no port here (${PORTS.map((p) => MANIFEST[p]).join(', ')})`)
  lines.push('')

  if (out.rows.length) {
    const table = [['dependency', ...ports, '']]
    for (const row of out.rows) {
      const cells = ports.map((p) => !row.in.includes(p) ? '-'
        : row.optional.includes(p) ? 'optional' : 'yes')
      const note = !out.differences.includes(row) ? ''
        : row.registered ? 'differs, registered' : 'differs, NOT registered'
      table.push([row.id, ...cells, note])
    }
    const width = table[0].map((_, col) => Math.max(...table.map((r) => r[col].length)))
    for (const r of table) {
      lines.push(('  ' + r.map((cell, col) => cell.padEnd(width[col])).join('  ')).trimEnd())
    }
    lines.push('')
  } else if (ports.length) {
    lines.push(`  no runtime dependency on another ${ORG} repo in any port`)
    lines.push('')
  }

  const dev = ports.filter((p) => read.ports[p].dev.length)
  if (dev.length) {
    lines.push('  test and development only, not compared:')
    for (const p of dev) lines.push(`    ${p}  ${read.ports[p].dev.join(' ')}`)
    lines.push('')
  }

  if (out.problems.length) {
    lines.push(`${out.problems.length} problem${1 === out.problems.length ? '' : 's'}:`)
    for (const problem of out.problems) lines.push(`  - ${problem}`)
  } else if (ports.length < 2) {
    lines.push(`ok: ${1 === ports.length ? 'one port' : 'no port'}, so there is nothing to compare`)
  } else if (out.differences.length) {
    const count = out.differences.length
    lines.push(`ok: ${count} difference${1 === count ? '' : 's'}, each recorded in ${REGISTER}`)
  } else {
    lines.push(`ok: ${ports.join(', ')} have the same ${ORG} runtime dependencies`)
  }
  return lines.join('\n') + '\n'
}

function check(dir) {
  const read = readRepo(dir)
  const register = readRegister(dir)
  const out = compare(read, register)
  return { read, register, ...out, ok: 0 === out.problems.length, report: render(read, out) }
}

module.exports = { ORG, PORTS, REGISTER, MANIFEST, readRepo, readRegister, compare, check }

// ---------------------------------------------------------------------------
// Run as a program

const USAGE = `Usage: node tools/port-deps.cjs [--json] [DIR]

Compare the ${ORG} runtime dependencies of this repository's ports
(${PORTS.map((p) => MANIFEST[p]).join(', ')}).

  DIR         the repository to read (default: the one holding this file)
  --json      print the result as JSON rather than as a table
  -h, --help  print this

Exits 0 when the ports agree or every difference is recorded in
${REGISTER}, 1 when there is a problem, 2 when a manifest or
the register cannot be read or DIR is not a directory.
`

function main(argv) {
  let json = false
  let dir = null
  for (const arg of argv) {
    if ('-h' === arg || '--help' === arg) {
      process.stdout.write(USAGE)
      return 0
    }
    if ('--json' === arg) json = true
    else if (arg.startsWith('-') || null !== dir) {
      process.stderr.write(`port-deps: unexpected argument ${arg}\n\n${USAGE}`)
      return 2
    } else dir = arg
  }
  const root = null === dir ? Path.join(__dirname, '..') : dir
  // A mistyped DIR has no manifest, and would otherwise pass as "no port".
  if (!Fs.existsSync(root) || !Fs.statSync(root).isDirectory()) {
    process.stderr.write(`port-deps: ${root} is not a directory\n`)
    return 2
  }
  let result
  try {
    result = check(root)
  } catch (err) {
    process.stderr.write(`port-deps: ${err.message}\n`)
    return 2
  }
  process.stdout.write(json ? JSON.stringify(result, null, 2) + '\n' : result.report)
  return result.ok ? 0 : 1
}

if (require.main === module) process.exitCode = main(process.argv.slice(2))
