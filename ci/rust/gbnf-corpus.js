#!/usr/bin/env node
/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Compile the maintained llama.cpp GBNF corpus once, serialize each
// function-free grammar, and require the TypeScript and Rust engines to
// produce identical values/error codes for every sample. The corpus file is
// evaluated only up to its test declarations so its explicit census remains
// the single source of cases.

const Assert = require('node:assert')
const Fs = require('node:fs')
const Path = require('node:path')
const ChildProcess = require('node:child_process')

const PARSER_ROOT = Path.resolve(__dirname, '..', '..')
const TABNAS_ROOT = Path.resolve(process.env.TABNAS_ROOT ||
  Path.join(PARSER_ROOT, '..'))
const GBNF_ROOT = Path.join(TABNAS_ROOT, 'gbnf')
const CORPUS_TEST = Path.join(GBNF_ROOT, 'ts', 'test', 'corpus.test.js')
const CORPUS_DIR = Path.join(GBNF_ROOT, 'test', 'corpus')
const RUST_RUNNER = Path.join(PARSER_ROOT, 'rs', 'target', 'debug',
  process.platform === 'win32' ? 'spec_runner.exe' : 'spec_runner')

for (const path of [CORPUS_TEST, CORPUS_DIR, RUST_RUNNER]) {
  if (!Fs.existsSync(path)) {
    throw new Error(`GBNF parity prerequisite is missing: ${path}`)
  }
}

const { Tabnas } = require(Path.join(PARSER_ROOT, 'ts', 'dist', 'tabnas'))
const gbnfModule = require(Path.join(GBNF_ROOT, 'ts', 'dist', 'gbnf'))
const { gbnfConvert } = gbnfModule
const { toPureSpec, toJsonic } = require(
  Path.join(TABNAS_ROOT, 'bnf', 'ts', 'dist', 'bnf'))

function corpusCases() {
  const source = Fs.readFileSync(CORPUS_TEST, 'utf8')
  const end = source.indexOf("\ndescribe('corpus'")
  Assert.ok(0 < end, `cannot find corpus test boundary in ${CORPUS_TEST}`)
  const prefix = source.slice(0, end)
  const fakeRequire = (id) => {
    if (id === '@tabnas/parser') return { Tabnas }
    if (id === '..') return gbnfModule
    return require(id)
  }
  return Function('require', '__dirname',
    `${prefix}\nreturn { GRAMMARS, ACCEPT, REJECT, EXPECTED_FAILURES }`)(
      fakeRequire, Path.dirname(CORPUS_TEST))
}

function tsOutcome(spec, source) {
  const parser = new Tabnas()
  parser.grammar(JSON.parse(JSON.stringify(spec)))
  try {
    return { accepted: true, value: parser.parse(source) }
  }
  catch (error) {
    return { accepted: false, code: String(error.code) }
  }
}

function rustOutcomes(spec, sources) {
  const run = ChildProcess.spawnSync(RUST_RUNNER, [], {
    input: JSON.stringify({ grammar: spec, sources }),
    encoding: 'utf8',
    maxBuffer: 32 * 1024 * 1024,
  })
  if (run.status !== 0) {
    throw new Error(`Rust spec runner failed (${run.status}): ${run.stderr}`)
  }
  return JSON.parse(run.stdout)
}

const { GRAMMARS, ACCEPT, REJECT, EXPECTED_FAILURES } = corpusCases()
const expectedFailures = new Map()
for (const [name, source] of EXPECTED_FAILURES) {
  const sources = expectedFailures.get(name) || []
  sources.push(source)
  expectedFailures.set(name, sources)
}

let checked = 0
const failures = []
for (const name of GRAMMARS) {
  const accepted = ACCEPT[name] || []
  const rejected = [...(REJECT[name] || []), ...(expectedFailures.get(name) || [])]
  const cases = [
    ...accepted.map((source) => ({ source, expected: true })),
    ...rejected.map((source) => ({ source, expected: false })),
  ]
  const grammarSource = Fs.readFileSync(Path.join(CORPUS_DIR, `${name}.gbnf`), 'utf8')
  const liveSpec = toPureSpec(gbnfConvert(grammarSource, { builtins: true }))
  // Strict serialization is part of the contract: it converts RegExp
  // objects into the schema's @/.../ strings instead of JSON's `{}`.
  const spec = JSON.parse(toJsonic(liveSpec, { strict: true }))
  const rust = rustOutcomes(spec, cases.map((entry) => entry.source))
  let passed = 0

  cases.forEach((entry, index) => {
    const ts = tsOutcome(spec, entry.source)
    try {
      Assert.equal(ts.accepted, entry.expected, 'TypeScript corpus verdict')
      Assert.equal(rust[index].accepted, entry.expected, 'Rust corpus verdict')
      Assert.deepStrictEqual(rust[index], ts, 'TypeScript/Rust outcome')
      passed++
    }
    catch (error) {
      failures.push({
        grammar: name,
        source: entry.source,
        expected: entry.expected,
        typescript: ts,
        rust: rust[index],
        error: error.message,
      })
    }
  })
  checked += cases.length
  console.log(`${name}: ${passed}/${cases.length}`)
}

if (failures.length > 0) {
  console.error(JSON.stringify(failures, null, 2))
  process.exitCode = 1
}
else {
  console.log(`GBNF TypeScript/Rust value+error parity: ${checked}/${checked}`)
}
