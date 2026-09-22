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
    'https://github.com/o/r/tarball/main', 'git@github.com:o/r.git',
    'https://codeload.github.com/o/r/tar.gz/main',
  ]) assert.equal(gate.classify(spec).source, 'github', JSON.stringify(spec))
})


test('each rejected source is classified as itself', () => {
  for (const [spec, source] of [
    ['file:../sibling', 'file'],
    ['file:./packed.tgz', 'file'],
    ['git+file:///tmp/checkout', 'file'],
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
    ['\\\\share\\pkg', 'path'],
    ['vendor/packages/foo', 'path'],
    ['../packed.tgz', 'archive'],
    ['packed-1.0.0.tgz', 'archive'],
    ['packed.tar', 'archive'],
    ['packed.tar.xz', 'archive'],
    ['packed.tar.bz2', 'archive'],
    ['https://example.com/packed.tgz', 'archive'],
    ['gitlab:owner/repo', 'git-other'],
    ['bitbucket:owner/repo', 'git-other'],
    ['git+https://gitlab.com/o/r.git', 'git-other'],
    ['git+ssh://git@git.example.com/o/r.git', 'git-other'],
    ['git@gitlab.com:o/r.git', 'git-other'],
  ]) assert.equal(gate.classify(spec).source, source, JSON.stringify(spec))
})


test('github.com in the PATH of another host is not a GitHub reference', () => {
  for (const spec of [
    'https://evil.example/github.com/o/r.git',
    'git+https://evil.example/github.com/o/r.git',
    'git+ssh://git@evil.example/github.com/o/r.git',
  ]) assert.equal(gate.classify(spec).source, 'git-other', JSON.stringify(spec))

  assert.equal(gate.hostOf('git+https://evil.example/github.com/o/r.git'), 'evil.example')
  assert.equal(gate.hostOf('git@github.com:o/r.git'), 'github.com')
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
    ['local-path-dep', { dependencies: { a: 'git+file:///tmp/x' } }],
    ['local-path-dep', { dependencies: { a: 'vendor/packages/foo' } }],
    ['workspace-dep', { dependencies: { a: 'workspace:*' } }],
    ['archive-dep', { devDependencies: { a: '../packed.tgz' } }],
    ['archive-dep', { devDependencies: { a: 'packed.tar' } }],
    ['non-github-git-dep', { dependencies: { a: 'gitlab:o/r' } }],
    ['non-github-git-dep', { dependencies: { a: 'git@gitlab.com:o/r.git' } }],
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


test('a clean manifest over a dirty lockfile is still caught, in either shape', () => {
  const v3 = gate.checkLockfile('package-lock.json', {
    packages: {
      '': { name: 'root' },
      'node_modules/a': { resolved: 'file:../sibling', version: '1.0.0' },
      'node_modules/b': { link: true },
      'node_modules/c': { resolved: 'https://registry.example.com/c/-/c-1.0.0.tgz' },
      'node_modules/d': { resolved: 'https://registry.npmjs.org/d/-/d-1.0.0.tgz' },
      'node_modules/e': { resolved: 'git+ssh://git@github.com/o/r.git#abc' },
    },
  }, EMPTY)
  const found = rules(v3)
  assert.ok(found.includes('lockfile-local-resolution'), 'file: resolution missed')
  assert.equal(found.filter((r) => 'lockfile-local-resolution' === r).length, 2, 'link: true missed')
  assert.ok(found.includes('lockfile-foreign-registry'), 'foreign registry missed')
  assert.equal(v3.length, 3, 'npmjs.org and a GitHub git resolution must not be findings')

  const v1local = gate.checkLockfile('package-lock.json', {
    dependencies: { a: { version: 'file:../sibling' } },
  }, EMPTY)
  assert.deepEqual(rules(v1local), ['lockfile-local-resolution'], 'lockfile v1 shape missed')

  // v1 gets the SAME judgement as v3, not a laxer one.
  const v1foreign = gate.checkLockfile('package-lock.json', {
    dependencies: { a: { resolved: 'https://registry.example.com/a/-/a-1.0.0.tgz', version: '1.0.0' } },
  }, EMPTY)
  assert.deepEqual(rules(v1foreign), ['lockfile-foreign-registry'],
    'a foreign registry in a v1 lockfile must be caught too')

  const v1clean = gate.checkLockfile('package-lock.json', {
    dependencies: { a: { resolved: 'https://registry.npmjs.org/a/-/a-1.0.0.tgz', version: '1.0.0' } },
  }, EMPTY)
  assert.deepEqual(v1clean, [], 'a v1 npmjs.org resolution must pass')
})


test('a yarn or pnpm lockfile is read as text rather than waved through', () => {
  const dirty = gate.checkTextLockfile('yarn.lock',
    '"a@file:../sibling":\n  version "1.0.0"\n', EMPTY)
  assert.ok(rules(dirty).includes('lockfile-local-resolution'), 'file: in yarn.lock missed')

  const foreign = gate.checkTextLockfile('pnpm-lock.yaml',
    "  a:\n    resolution: {tarball: https://registry.example.com/a.tgz}\n", EMPTY)
  assert.ok(rules(foreign).includes('lockfile-foreign-registry'), 'foreign registry missed')

  const clean = gate.checkTextLockfile('yarn.lock',
    '# comment\n"a@^1.0.0":\n  version "1.0.0"\n  resolved "https://registry.npmjs.org/a/-/a-1.0.0.tgz"\n',
    EMPTY)
  assert.deepEqual(clean, [], 'an npmjs.org yarn lockfile must pass')
})


test('a go replace is judged on where it points, and quoting does not hide it', () => {
  // The depths mirror the real case: a helper module three levels down, whose
  // ../../../go IS the repository's own module and whose ../../../../x is not.
  // Path.resolve makes the synthetic root native, so the containment checks
  // mean the same thing on a Windows runner as on a POSIX one.
  const root = Path.resolve('/repo')
  const mod = 'ci/parity/helper/go.mod'

  const inside = gate.checkGoMod(mod, 'replace example.com/m => ../../../go\n', EMPTY, root)
  assert.deepEqual(inside, [], 'an in-repo replace travels with the checkout and must pass')

  const quoted = gate.checkGoMod(mod, 'replace example.com/m => "../../../go dir"\n', EMPTY, root)
  assert.deepEqual(quoted, [], 'a quoted in-repo path is still a path')

  const outside = gate.checkGoMod(mod, 'replace example.com/m => ../../../../sibling/go\n', EMPTY, root)
  assert.deepEqual(rules(outside), ['go-external-path-replace'], 'escaping replace missed')

  for (const target of ['/abs/go', 'C:/outside/mod', '\\\\share\\mod', '~/go']) {
    const abs = gate.checkGoMod(mod, 'replace example.com/m => ' + target + '\n', EMPTY, root)
    assert.deepEqual(rules(abs), ['go-absolute-path-replace'], 'absolute target ' + target + ' missed')
  }

  const block = gate.checkGoMod(mod,
    'replace (\n\texample.com/m => ../../../../sibling/go\n\texample.com/n => ../../../go\n)\n',
    EMPTY, root)
  assert.deepEqual(rules(block), ['go-external-path-replace'], 'block form missed')

  const toModule = gate.checkGoMod(mod, 'replace example.com/m => example.com/fork v1.2.3\n', EMPTY, root)
  assert.deepEqual(rules(toModule), ['go-module-replace'], 'module replace missed')

  const commented = gate.checkGoMod(mod, '// replace example.com/m => ../../../../sibling/go\n', EMPTY, root)
  assert.deepEqual(commented, [], 'a commented-out replace is not a replace')

  // An in-repo target git does not track will not be in a fresh checkout.
  const tracked = new Set(['go/go.mod', 'ci/parity/helper/go.mod'])
  assert.deepEqual(
    gate.checkGoMod(mod, 'replace example.com/m => ../../../go\n', EMPTY, root, null, tracked),
    [], 'a tracked in-repo target must pass')
  assert.deepEqual(
    rules(gate.checkGoMod(mod, 'replace example.com/m => ../../../scratch\n', EMPTY, root, null, tracked)),
    ['go-untracked-path-replace'], 'an untracked in-repo target must be reported')
})


test('a committed go.work is a finding by name, whatever it wires', () => {
  // Path.resolve makes the synthetic root native, so the containment checks
  // mean the same thing on a Windows runner as on a POSIX one.
  const root = Path.resolve('/repo')
  const work = (text) => gate.checkGoWork('go.work', text, EMPTY, root)

  // The all-internal case is the one a path rule waves through, and the one the
  // guides forbid outright: a workspace lives above the repositories it wires.
  assert.deepEqual(rules(work('go 1.24\nuse ./go\nuse (\n\t./ci/helper\n)\n')),
    ['go-workspace'], 'an internal workspace must still be a finding')
  assert.deepEqual(work('go 1.24\nuse ./go\n')[0].spec, 'use ./go',
    'the report must name what the workspace wires')

  assert.deepEqual(rules(work('go 1.24\nuse (\n\t./go\n\t../sibling/go\n)\n')),
    ['go-workspace'], 'an escaping use missed')
  assert.deepEqual(rules(work('go 1.24\nuse /elsewhere/go\n')),
    ['go-workspace'], 'an absolute use missed')
  assert.deepEqual(rules(work('go 1.24\n')),
    ['go-workspace'], 'a memberless workspace missed')

  const allowed = gate.checkGoWork('go.work', 'go 1.24\nuse ./go\n',
    { allow: { 'go.work:go-workspace': 'reason' } }, root)
  assert.deepEqual(allowed, [], 'the allowlist key must suppress it')
})


test('a git host is read from the authority, in every spelling git accepts', () => {
  // npm documents `git+ssh://git@github.com:npm/cli.git#v1.0.27`, where what
  // follows the colon is a path. `new URL` rejects it outright.
  for (const spec of [
    'git+ssh://git@github.com:npm/cli.git#v1.0.27',
    'git+ssh://git@github.com/npm/cli.git#v1.0.27',
    'git+https://github.com/o/r.git',
    'github:o/r',
    'git@github.com:o/r.git',
  ]) assert.equal(gate.classify(spec).source, 'github', JSON.stringify(spec))

  for (const spec of [
    'git+ssh://git@gitlab.com:o/r.git',
    'git+ssh://git@gitlab.com/o/r.git',
    'git+https://evil.example/github.com/o/r.git',
  ]) assert.equal(gate.classify(spec).source, 'git-other', JSON.stringify(spec))

  assert.equal(gate.hostOf('git+ssh://git@github.com:npm/cli.git'), 'github.com',
    'the colon form must yield a host, not null')
})


test('a cargo dependency is judged by path, by host, and only inside a dependency table', () => {
  // Path.resolve makes the synthetic root native, so the containment checks
  // mean the same thing on a Windows runner as on a POSIX one.
  const root = Path.resolve('/repo')
  const cargo = (text) => gate.checkCargoToml('rs/Cargo.toml', text, EMPTY, root)

  assert.deepEqual(cargo('[dependencies]\ndep = { path = "../other" }\n'), [],
    'a workspace-internal path dep must pass')
  assert.deepEqual(rules(cargo('[dependencies]\ndep = { path = "../../sibling" }\n')),
    ['cargo-external-path-dep'], 'escaping cargo path missed')
  assert.deepEqual(rules(cargo("[dependencies]\ndep = { path = '../../sibling' }\n")),
    ['cargo-external-path-dep'], 'a TOML literal string hid an escaping path')
  assert.deepEqual(rules(cargo('[dependencies]\ndep = { path = "/elsewhere" }\n')),
    ['cargo-absolute-path-dep'], 'an absolute cargo path missed')
  assert.deepEqual(rules(cargo('[dependencies]\ndep = { git = "https://gitlab.com/o/r.git" }\n')),
    ['cargo-non-github-git-dep'], 'a non-GitHub cargo git dep missed')
  assert.deepEqual(cargo('[dependencies]\ndep = { git = "https://github.com/o/r.git" }\n'), [],
    'a GitHub cargo git dep must pass')

  // Not a dependency: a comment, and a `path` key in another table.
  assert.deepEqual(cargo('[dependencies]\n# dep = { path = "../../sibling" }\n'), [],
    'a commented-out cargo path is not a dependency')
  assert.deepEqual(cargo('[package]\npath = "../../sibling"\n'), [],
    'a path outside a dependency table is not a dependency')
  assert.deepEqual(rules(cargo('[target."cfg(unix)".dev-dependencies]\ndep = { path = "../../s" }\n')),
    ['cargo-external-path-dep'], 'a target dev-dependency table was not read')

  // `[patch.*]` and `[replace]` redirect a resolved dependency, so they are
  // dependency tables here however Cargo names them.
  assert.deepEqual(rules(cargo('[patch.crates-io]\ndep = { path = "../../sibling" }\n')),
    ['cargo-external-path-dep'], 'a patch table was not read')
  assert.deepEqual(rules(cargo('[patch."https://github.com/o/r"]\ndep = { path = "/elsewhere" }\n')),
    ['cargo-absolute-path-dep'], 'a patch table keyed by URL was not read')
  assert.deepEqual(rules(cargo('[replace]\n"dep:0.1.0" = { path = "../../sibling" }\n')),
    ['cargo-external-path-dep'], 'a replace table was not read')
  assert.deepEqual(rules(cargo('[workspace.patch.crates-io]\ndep = { git = "https://gitlab.com/o/r" }\n')),
    ['cargo-non-github-git-dep'], 'a workspace patch table was not read')
  assert.deepEqual(cargo('[patch.crates-io]\ndep = { git = "https://github.com/o/r.git" }\n'), [],
    'a GitHub patch must pass')
  assert.deepEqual(cargo('[workspace.package]\npath = "../../sibling"\n'), [],
    'a workspace package table is not a dependency table')

  // A DOTTED key has no table header to be inside of, so section state alone
  // never sees it. Cargo accepts all of these.
  assert.deepEqual(rules(cargo('dependencies.dep.path = "../../sibling"\n')),
    ['cargo-external-path-dep'], 'a dotted dependency path at the root was not read')
  assert.deepEqual(rules(cargo('dependencies.dep.git = "https://gitlab.com/o/r"\n')),
    ['cargo-non-github-git-dep'], 'a dotted dependency git at the root was not read')
  assert.deepEqual(rules(cargo('patch.crates-io.dep.path = "/elsewhere"\n')),
    ['cargo-absolute-path-dep'], 'a dotted patch path was not read')
  assert.deepEqual(rules(cargo('workspace.dependencies.dep.path = "../../sibling"\n')),
    ['cargo-external-path-dep'], 'a dotted workspace dependency path was not read')
  assert.deepEqual(rules(cargo('[workspace]\ndependencies.dep.path = "../../sibling"\n')),
    ['cargo-external-path-dep'], 'a header prefixes a dotted key, and did not')
  assert.deepEqual(rules(cargo('[dependencies]\ndep.path = "../../sibling"\n')),
    ['cargo-external-path-dep'], 'a dotted key under a dependency header was not read')
  assert.deepEqual(rules(cargo('dependencies.dep = { path = "../../sibling" }\n')),
    ['cargo-external-path-dep'], 'a dotted key holding an inline table was not read')
  assert.deepEqual(rules(cargo('target."cfg(unix)".dependencies.dep.path = "../../s"\n')),
    ['cargo-external-path-dep'], 'a dotted target dependency path was not read')

  // The same spelling outside a dependency table still is not a dependency.
  assert.deepEqual(cargo('package.path = "../../sibling"\n'), [],
    'a dotted package path is not a dependency')
  assert.deepEqual(cargo('workspace.package.path = "../../sibling"\n'), [],
    'a dotted workspace package path is not a dependency')
  assert.deepEqual(cargo('dependencies.dep.version = "1.2.3"\n'), [],
    'a dotted version is not a path or a git reference')
  assert.deepEqual(cargo('# dependencies.dep.path = "../../sibling"\n'), [],
    'a commented-out dotted dependency path is not a dependency')
  assert.deepEqual(cargo('dependencies.dep.path = "../other"\n'), [],
    'a dotted path inside the repository must pass')
  assert.deepEqual(cargo('patch.crates-io.dep.git = "https://github.com/o/r.git"\n'), [],
    'a dotted GitHub patch must pass')

  // One report, not two: the dotted branch handles the line and stops.
  assert.equal(cargo('dependencies.dep.path = "../../sibling"\n').length, 1,
    'the dotted key was judged twice')
})


test('a committed submodule URL is judged like any other source', () => {
  const mods = (text) => gate.checkGitmodules('.gitmodules', text, EMPTY)
  assert.deepEqual(mods('[submodule "x"]\n\tpath = x\n\turl = https://github.com/o/r.git\n'), [],
    'a GitHub submodule must pass')
  assert.deepEqual(rules(mods('[submodule "x"]\n\turl = file:///home/me/dep\n')),
    ['submodule-local-url'], 'a file: submodule URL missed')
  assert.deepEqual(rules(mods('[submodule "x"]\n\turl = ../sibling\n')),
    ['submodule-local-url'], 'a relative submodule URL missed')
  assert.deepEqual(rules(mods('[submodule "x"]\n\turl = https://gitlab.com/o/r.git\n')),
    ['submodule-non-github-url'], 'a non-GitHub submodule URL missed')
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
  return { dir, git, cleanup: () => Fs.rmSync(dir, { recursive: true, force: true }) }
}


test('the tree-level rules each go red on a real git index', () => {
  const cases = [
    ['committed-archive', (d) => Fs.writeFileSync(Path.join(d, 'packed-1.0.0.tgz'), 'x')],
    ['committed-archive', (d) => Fs.writeFileSync(Path.join(d, 'packed.tar'), 'x')],
    ['go-workspace', (d) => Fs.writeFileSync(Path.join(d, 'go.work'), 'go 1.24\nuse ../sibling/go\n')],
    ['go-workspace', (d) => {
      Fs.mkdirSync(Path.join(d, 'go'))
      Fs.writeFileSync(Path.join(d, 'go', 'go.mod'), 'module example.com/m\n\ngo 1.24\n')
      Fs.writeFileSync(Path.join(d, 'go.work'), 'go 1.24\nuse ./go\n')
    }],
    ['foreign-registry', (d) => Fs.writeFileSync(Path.join(d, '.npmrc'), 'registry=https://registry.example.com/\n')],
    ['escaping-symlink', (d) => Fs.symlinkSync('../outside', Path.join(d, 'escape'))],
    ['absolute-symlink', (d) => Fs.symlinkSync('/etc/hosts', Path.join(d, 'abs'))],
    ['escaping-symlink', (d) => {
      Fs.mkdirSync(Path.join(d, 'sub'))
      Fs.symlinkSync('node_modules/pkg', Path.join(d, 'sub', 'linked'))
    }],
    ['local-path-dep', (d) => Fs.writeFileSync(Path.join(d, 'package.json'),
      JSON.stringify({ name: 'p', dependencies: { a: 'file:../x' } }))],
    ['unchecked-lockfile', (d) => Fs.writeFileSync(Path.join(d, 'bun.lockb'), 'binary')],
    ['submodule-local-url', (d) => Fs.writeFileSync(Path.join(d, '.gitmodules'),
      '[submodule "x"]\n\turl = /home/me/dep\n')],
    ['lockfile-local-resolution', (d) => Fs.writeFileSync(Path.join(d, 'yarn.lock'),
      '"a@file:../x":\n  version "1.0.0"\n')],
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


test('an in-repo symlink, a go.mod without a replace and an npmjs registry are not findings', () => {
  const { dir, cleanup } = synthetic((d) => {
    Fs.writeFileSync(Path.join(d, 'AGENTS.md'), '# guide\n')
    Fs.symlinkSync('AGENTS.md', Path.join(d, 'CLAUDE.md'))
    Fs.writeFileSync(Path.join(d, '.npmrc'), 'registry=https://registry.npmjs.org/\n')
    Fs.mkdirSync(Path.join(d, 'go'))
    Fs.writeFileSync(Path.join(d, 'go', 'go.mod'), 'module example.com/m\n\ngo 1.24\n')
    Fs.writeFileSync(Path.join(d, 'package.json'),
      JSON.stringify({ name: 'p', dependencies: { a: '^1.0.0', b: 'github:o/r' } }))
  })
  try {
    assert.deepEqual(gate.checkAll(EMPTY, dir), [],
      'the CLAUDE.md convention, a plain go.mod and a registry dep must all pass')
  }
  finally { cleanup() }
})


test('the allowlist is read from the index too, so it cannot excuse from the worktree', () => {
  const pkg = JSON.stringify({ name: 'p', dependencies: { a: 'file:../x' } })
  const key = 'package.json:dependencies:a'
  const allow = JSON.stringify({ allow: { [key]: 'measuring something' } }, null, 2)

  const { dir, git, cleanup } = synthetic((d) => {
    Fs.writeFileSync(Path.join(d, 'package.json'), pkg)
  })
  try {
    // Writing the allowance without staging it must change nothing: read from
    // disk it would excuse a violation that is already staged.
    Fs.mkdirSync(Path.join(dir, 'tools'))
    Fs.writeFileSync(Path.join(dir, 'tools', 'dep-gate.json'), allow)
    assert.deepEqual(rules(gate.checkAll(null, dir)), ['local-path-dep'],
      'an unstaged allowlist entry excused a staged violation')

    git('add', '-A')
    assert.deepEqual(gate.checkAll(null, dir), [],
      'a staged allowlist entry did not take effect')

    // And once staged, reverting it on disk must not bring the finding back.
    Fs.writeFileSync(Path.join(dir, 'tools', 'dep-gate.json'), '{"allow":{}}\n')
    assert.deepEqual(gate.checkAll(null, dir), [],
      'the worktree copy of the allowlist was read instead of the index')
  }
  finally { cleanup() }
})


test('the npm registry is accepted in every spelling npm accepts', () => {
  for (const line of [
    'registry=https://registry.npmjs.org/',
    'registry=https://registry.npmjs.org',
    'registry="https://registry.npmjs.org/"',
    "registry='https://registry.npmjs.org'",
    '@scope:registry=https://registry.npmjs.org/',
  ]) {
    const { dir, cleanup } = synthetic((d) => Fs.writeFileSync(Path.join(d, '.npmrc'), line + '\n'))
    try {
      assert.deepEqual(gate.checkAll(EMPTY, dir), [], JSON.stringify(line) + ' must pass')
    }
    finally { cleanup() }
  }
})


test('one allowed registry directive does not suppress another in the same file', () => {
  const { dir, cleanup } = synthetic((d) => Fs.writeFileSync(Path.join(d, '.npmrc'),
    '@corp:registry=https://npm.corp.example/\nregistry=https://evil.example/\n'))
  try {
    assert.equal(gate.checkAll(EMPTY, dir).length, 2, 'both directives must be findings')
    const one = gate.checkAll({ allow: { '.npmrc:registry:@corp': 'the corp registry is deliberate' } }, dir)
    assert.equal(one.length, 1, 'allowing the scoped registry must leave the global one reported')
    assert.match(one[0].spec, /evil\.example/)
  }
  finally { cleanup() }
})


test('the gate judges the INDEX, so wiring is legal until it is staged', () => {
  const { dir, git, cleanup } = synthetic((d) => {
    Fs.writeFileSync(Path.join(d, 'package.json'),
      JSON.stringify({ name: 'p', dependencies: { a: '^1.0.0' } }))
  })
  try {
    const file = Path.join(dir, 'package.json')
    assert.deepEqual(gate.checkAll(EMPTY, dir), [], 'the clean index must pass')

    // Unstaged local wiring: legal, because it has not been committed to.
    Fs.writeFileSync(file, JSON.stringify({ name: 'p', dependencies: { a: 'file:../x' } }))
    assert.deepEqual(gate.checkAll(EMPTY, dir), [],
      'an UNSTAGED file: dependency must not fail the gate')

    // Staged: now it is on its way into a commit.
    git('add', 'package.json')
    assert.deepEqual(rules(gate.checkAll(EMPTY, dir)), ['local-path-dep'],
      'a STAGED file: dependency must fail the gate')

    // Staged bad, working copy reverted: the index still carries it.
    Fs.writeFileSync(file, JSON.stringify({ name: 'p', dependencies: { a: '^1.0.0' } }))
    assert.deepEqual(rules(gate.checkAll(EMPTY, dir)), ['local-path-dep'],
      'reverting only the working copy must not clear a staged violation')
  }
  finally { cleanup() }
})


test('a manifest reached through an in-repo symlink is still read', () => {
  const { dir, cleanup } = synthetic((d) => {
    Fs.mkdirSync(Path.join(d, 'real'))
    Fs.writeFileSync(Path.join(d, 'real', 'manifest.json'),
      JSON.stringify({ name: 'p', dependencies: { a: 'file:../x' } }))
    Fs.symlinkSync('real/manifest.json', Path.join(d, 'package.json'))
  })
  try {
    assert.ok(rules(gate.checkAll(EMPTY, dir)).includes('local-path-dep'),
      'npm reads through the link, so the gate must too')
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


test('a symlink allowance goes stale once the symlink is repaired', () => {
  const allow = { 'link:symlink': 'the escaping link was deliberate' }

  const bad = synthetic((d) => Fs.symlinkSync('../outside', Path.join(d, 'link')))
  try {
    assert.deepEqual(gate.checkAll({ allow }, bad.dir), [],
      'the allowance must suppress the escaping link')
  }
  finally { bad.cleanup() }

  const good = synthetic((d) => {
    Fs.writeFileSync(Path.join(d, 'inside.md'), 'x\n')
    Fs.symlinkSync('inside.md', Path.join(d, 'link'))
  })
  try {
    assert.deepEqual(rules(gate.checkAll({ allow }, good.dir)), ['stale-allow'],
      'once the link points inside, its allowance describes a state that has gone')
  }
  finally { good.cleanup() }
})


test('there is no whole-file bypass', () => {
  const { dir, cleanup } = synthetic((d) => {
    Fs.writeFileSync(Path.join(d, 'package.json'),
      JSON.stringify({ name: 'p', dependencies: { a: 'file:../x' } }))
  })
  try {
    // An unreasoned, unchecked skip list is the one exception shape this gate
    // must not have: every exception carries a reason and expires.
    const withSkip = gate.checkAll({ allow: {}, skipPaths: ['package.json'] }, dir)
    assert.deepEqual(rules(withSkip), ['local-path-dep'],
      'skipPaths must not suppress anything')
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
