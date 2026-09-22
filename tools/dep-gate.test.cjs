'use strict'

// A check that cannot fail is not a check. Every rule below is driven by a
// case that breaks exactly one thing and requires that rule to go red.

const { test } = require('node:test')
const assert = require('node:assert/strict')
const Fs = require('node:fs')
const Path = require('node:path')
const Os = require('node:os')
const Child = require('node:child_process')

const gate = require('./dep-gate.cjs')
const CONFIG = (() => {
  try { return JSON.parse(Fs.readFileSync(Path.join(__dirname, 'dep-gate.json'), 'utf8')) }
  catch { return {} }
})()

const EMPTY = { allow: {} }
const rules = (list) => list.map((f) => f.rule).sort()


test('this repository satisfies the policy', () => {
  const findings = gate.checkAll()
  assert.deepEqual(
    findings.map((f) => f.file + ' ' + f.where + ' ' + f.rule), [],
    'dependency gate findings on the committed tree',
  )
})


test('every allowlist entry carries a reason and still matches something', () => {
  for (const [key, reason] of Object.entries(CONFIG.allow || {})) {
    assert.equal(typeof reason, 'string', key + ' has no reason')
    assert.ok(0 < reason.trim().length, key + ' has an empty reason')
  }
  // A stale entry is reported by checkAll, which the first test asserts clean.
})


test('a registry range and a GitHub reference are the only allowed sources', () => {
  for (const spec of [
    '1.2.3', '^1.2', '~1', '>=1 <2', '>=0.8.0 <0.11.0 || >=0.11.1', '*', '',
    'latest', 'next', 'npm:other@^1', '$other',
  ]) assert.equal(gate.classify(spec).source, 'registry', JSON.stringify(spec))

  for (const spec of [
    'github:owner/repo', 'github:owner/repo#semver:^1', 'owner/repo',
    'owner/repo#main', 'git+https://github.com/o/r.git#v1',
    'git+ssh://git@github.com/o/r.git', 'git://github.com/o/r',
    'https://github.com/o/r/tarball/main',
  ]) assert.equal(gate.classify(spec).source, 'github', JSON.stringify(spec))
})


test('each rejected source is classified as itself', () => {
  for (const [spec, source] of [
    ['file:../sibling', 'file'],
    ['file:./packed.tgz', 'file'],
    ['link:../sibling', 'link'],
    ['portal:../sibling', 'portal'],
    ['workspace:*', 'workspace'],
    ['workspace:^', 'workspace'],
    ['catalog:', 'catalog'],
    ['catalog:default', 'catalog'],
    ['./relative', 'path'],
    ['../sibling', 'path'],
    ['/absolute', 'path'],
    ['~/home', 'path'],
    ['C:/windows', 'path'],
    ['../packed.tgz', 'archive'],
    ['packed-1.0.0.tgz', 'archive'],
    ['https://example.com/packed.tgz', 'archive'],
    ['gitlab:owner/repo', 'git-other'],
    ['bitbucket:owner/repo', 'git-other'],
    ['git+https://gitlab.com/o/r.git', 'git-other'],
    ['git+ssh://git@git.example.com/o/r.git', 'git-other'],
  ]) assert.equal(gate.classify(spec).source, source, JSON.stringify(spec))
})


test('a manifest is red for each rejected source and green for the allowed ones', () => {
  const manifest = (deps) => gate.checkManifest('package.json', deps, EMPTY)

  assert.deepEqual(manifest({
    dependencies: { a: '^1.0.0' },
    devDependencies: { b: 'github:o/r#v2' },
    peerDependencies: { c: '>=0.8.0 <0.11.0 || >=0.11.1' },
    optionalDependencies: { d: 'npm:e@^3' },
  }), [], 'published and GitHub sources must pass')

  for (const [rule, deps] of [
    ['local-path-dep', { dependencies: { a: 'file:../sibling' } }],
    ['local-path-dep', { devDependencies: { a: 'link:../sibling' } }],
    ['local-path-dep', { dependencies: { a: '../sibling' } }],
    ['workspace-dep', { dependencies: { a: 'workspace:*' } }],
    ['archive-dep', { devDependencies: { a: '../packed.tgz' } }],
    ['non-github-git-dep', { dependencies: { a: 'gitlab:o/r' } }],
  ]) {
    const found = rules(manifest(deps))
    assert.ok(found.includes(rule), rule + ' not raised for ' + JSON.stringify(deps))
  }
})


test('every spec section is read, including nested overrides', () => {
  for (const section of [
    'dependencies', 'devDependencies', 'peerDependencies',
    'optionalDependencies', 'resolutions', 'overrides',
  ]) {
    const found = gate.checkManifest('package.json', { [section]: { a: 'file:../x' } }, EMPTY)
    assert.equal(found.length, 1, section + ' was not read')
    assert.equal(found[0].rule, 'local-path-dep', section)
  }

  // A nested override is the shape most likely to be missed.
  const nested = gate.checkManifest('package.json', {
    overrides: { outer: { inner: 'file:../x' } },
  }, EMPTY)
  assert.equal(nested.length, 1, 'nested override was not read')
  assert.match(nested[0].where, /outer > inner/)

  const pnpm = gate.checkManifest('package.json', {
    pnpm: { overrides: { a: 'link:../x' } },
  }, EMPTY)
  assert.equal(pnpm.length, 1, 'pnpm.overrides was not read')
})


test('a clean manifest over a dirty lockfile is still caught', () => {
  const v3 = gate.checkLockfile('package-lock.json', {
    packages: {
      '': { name: 'root' },
      'node_modules/a': { resolved: 'file:../sibling', version: '1.0.0' },
      'node_modules/b': { link: true },
      'node_modules/c': { resolved: 'https://registry.example.com/c/-/c-1.0.0.tgz' },
      'node_modules/d': { resolved: 'https://registry.npmjs.org/d/-/d-1.0.0.tgz' },
    },
  }, EMPTY)
  const found = rules(v3)
  assert.ok(found.includes('lockfile-local-resolution'), 'file: resolution missed')
  assert.equal(found.filter((r) => 'lockfile-local-resolution' === r).length, 2, 'link: true missed')
  assert.ok(found.includes('lockfile-foreign-registry'), 'foreign registry missed')
  assert.equal(v3.length, 3, 'the npmjs.org resolution must not be a finding')

  const v1 = gate.checkLockfile('package-lock.json', {
    dependencies: { a: { version: 'file:../sibling' } },
  }, EMPTY)
  assert.deepEqual(rules(v1), ['lockfile-local-resolution'], 'lockfile v1 shape missed')
})


test('a go replace inside the repository passes and one outside it fails', () => {
  // The depths mirror the real case: a helper module three levels down, whose
  // ../../../go IS the repository's own module and whose ../../../../x is not.
  const root = '/repo'
  const mod = 'ci/parity/helper/go.mod'

  const inside = gate.checkGoMod(mod, 'replace example.com/m => ../../../go\n', EMPTY, root)
  assert.deepEqual(inside, [], 'an in-repo replace travels with the checkout and must pass')

  const outside = gate.checkGoMod(mod, 'replace example.com/m => ../../../../sibling/go\n', EMPTY, root)
  assert.deepEqual(rules(outside), ['go-external-path-replace'], 'escaping replace missed')

  const block = gate.checkGoMod(mod,
    'replace (\n\texample.com/m => ../../../../sibling/go\n\texample.com/n => ../../../go\n)\n',
    EMPTY, root)
  assert.deepEqual(rules(block), ['go-external-path-replace'], 'block form missed')

  const toModule = gate.checkGoMod(mod, 'replace example.com/m => example.com/fork v1.2.3\n', EMPTY, root)
  assert.deepEqual(rules(toModule), ['go-module-replace'], 'module replace missed')

  const commented = gate.checkGoMod(mod, '// replace example.com/m => ../../../../sibling/go\n', EMPTY, root)
  assert.deepEqual(commented, [], 'a commented-out replace is not a replace')
})


test('a cargo path dependency is judged by whether it leaves the repository', () => {
  const root = '/repo'
  const inside = gate.checkCargoToml('rs/Cargo.toml', 'dep = { path = "../other" }\n', EMPTY, root)
  assert.deepEqual(inside, [], 'a workspace-internal path dep must pass')

  const outside = gate.checkCargoToml('rs/Cargo.toml', 'dep = { path = "../../sibling" }\n', EMPTY, root)
  assert.deepEqual(rules(outside), ['cargo-external-path-dep'], 'escaping cargo path missed')
})


// The tree-level rules need a real git index, so they get a real one.
function synthetic(build) {
  const dir = Fs.mkdtempSync(Path.join(Os.tmpdir(), 'dep-gate-'))
  const git = (...args) => Child.execFileSync('git', ['-C', dir, ...args], { stdio: 'pipe' })
  git('init', '-q')
  git('config', 'user.email', 'gate@example.com')
  git('config', 'user.name', 'gate')
  build(dir)
  git('add', '-A')
  return { dir, cleanup: () => Fs.rmSync(dir, { recursive: true, force: true }) }
}


test('the tree-level rules each go red on a real git index', () => {
  const cases = [
    ['committed-archive', (d) => Fs.writeFileSync(Path.join(d, 'packed-1.0.0.tgz'), 'x')],
    ['go-workspace', (d) => Fs.writeFileSync(Path.join(d, 'go.work'), 'go 1.24\nuse ./go\n')],
    ['foreign-registry', (d) => Fs.writeFileSync(Path.join(d, '.npmrc'), 'registry=https://registry.example.com/\n')],
    ['escaping-symlink', (d) => Fs.symlinkSync('../outside', Path.join(d, 'escape'))],
    ['escaping-symlink', (d) => {
      Fs.mkdirSync(Path.join(d, 'sub'))
      Fs.symlinkSync('node_modules/pkg', Path.join(d, 'sub', 'linked'))
    }],
    ['local-path-dep', (d) => Fs.writeFileSync(Path.join(d, 'package.json'),
      JSON.stringify({ name: 'p', dependencies: { a: 'file:../x' } }))],
  ]

  for (const [rule, build] of cases) {
    const { dir, cleanup } = synthetic(build)
    try {
      const found = rules(gate.checkAll(EMPTY, dir))
      assert.ok(found.includes(rule), rule + ' was not raised; got ' + JSON.stringify(found))
    }
    finally { cleanup() }
  }
})


test('an in-repo symlink and an npmjs registry are not findings', () => {
  const { dir, cleanup } = synthetic((d) => {
    Fs.writeFileSync(Path.join(d, 'AGENTS.md'), '# guide\n')
    Fs.symlinkSync('AGENTS.md', Path.join(d, 'CLAUDE.md'))
    Fs.writeFileSync(Path.join(d, '.npmrc'), 'registry=https://registry.npmjs.org/\n')
    Fs.writeFileSync(Path.join(d, 'package.json'),
      JSON.stringify({ name: 'p', dependencies: { a: '^1.0.0', b: 'github:o/r' } }))
  })
  try {
    assert.deepEqual(gate.checkAll(EMPTY, dir), [],
      'the CLAUDE.md symlink convention and a registry dep must both pass')
  }
  finally { cleanup() }
})


test('an allowlist entry suppresses exactly its own finding, and a stale one is reported', () => {
  const { dir, cleanup } = synthetic((d) => {
    Fs.writeFileSync(Path.join(d, 'package.json'),
      JSON.stringify({ name: 'p', dependencies: { a: 'file:../x', b: 'link:../y' } }))
  })
  try {
    const both = gate.checkAll(EMPTY, dir)
    assert.equal(both.length, 2)

    const one = gate.checkAll({
      allow: { 'package.json:dependencies:a': 'reason' },
    }, dir)
    assert.equal(one.length, 1, 'the allowlist must suppress only its own key')
    assert.match(one[0].where, /\.b$/)

    const stale = gate.checkAll({
      allow: {
        'package.json:dependencies:a': 'reason',
        'package.json:dependencies:b': 'reason',
        'package.json:dependencies:gone': 'reason',
      },
    }, dir)
    assert.ok(rules(stale).includes('stale-allow'), 'a stale allowlist entry must be reported')

    const unreasoned = gate.checkAll({
      allow: { 'package.json:dependencies:a': '', 'package.json:dependencies:b': 'reason' },
    }, dir)
    assert.ok(rules(unreasoned).includes('unreasoned-allow'), 'an unreasoned allow must be reported')
  }
  finally { cleanup() }
})


test('the report names the file, the rule and the way to allow it', () => {
  const text = gate.report(gate.checkManifest('package.json', {
    dependencies: { a: 'file:../sibling' },
  }, EMPTY))
  assert.match(text, /package\.json/)
  assert.match(text, /local-path-dep/)
  assert.match(text, /file:\.\.\/sibling/)
  assert.match(text, /tools\/dep-gate\.json/)
  assert.equal(gate.report([]), 'dependency gate: clean')
})
