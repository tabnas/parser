#!/usr/bin/env node
/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Seeded value/error differential over the shared function-free strict-JSON
// grammar. The maintained TSVs pin named edge cases; this arm combines them
// into larger trees and catches both emergent drift and accidental
// superlinear work in the Rust parser.

const Assert = require('node:assert')
const ChildProcess = require('node:child_process')
const Fs = require('node:fs')
const Os = require('node:os')
const Path = require('node:path')

const PARSER_ROOT = Path.resolve(__dirname, '..', '..')
const COUNT = Number(process.argv[2] || 50)
const SEED = Number(process.argv[3] || 2551599)
const GENERATOR = Path.join(PARSER_ROOT, 'ci', 'fuzz', 'gencorpus.js')
const FIXTURE = Path.join(PARSER_ROOT, 'ts', 'test',
  'json-builder.fixture.json')
const RUNNER = Path.join(PARSER_ROOT, 'rs', 'target', 'debug',
  process.platform === 'win32' ? 'spec_runner.exe' : 'spec_runner')

for (const path of [GENERATOR, FIXTURE, RUNNER]) {
  if (!Fs.existsSync(path)) {
    throw new Error(`Rust JSON fuzz prerequisite is missing: ${path}`)
  }
}

const { Tabnas } = require(Path.join(PARSER_ROOT, 'ts', 'dist', 'tabnas'))

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical)
  if (value && 'object' === typeof value) {
    return Object.fromEntries(Object.keys(value).sort()
      .map((key) => [key, canonical(value[key])]))
  }
  return value
}

const work = Fs.mkdtempSync(Path.join(Os.tmpdir(), 'tabnas-rust-json-'))
const corpus = Path.join(work, 'corpus')

try {
  const generated = ChildProcess.spawnSync(process.execPath,
    [GENERATOR, corpus, String(COUNT), 'json', String(SEED)], {
      encoding: 'utf8',
    })
  if (generated.status !== 0) {
    throw new Error(`JSON corpus generation failed: ${generated.stderr}`)
  }

  const spec = JSON.parse(Fs.readFileSync(FIXTURE, 'utf8'))
  const files = Fs.readdirSync(corpus)
    .filter((name) => name.endsWith('.in')).sort()
  const sources = files.map((name) =>
    Fs.readFileSync(Path.join(corpus, name), 'utf8'))

  const parser = new Tabnas()
  parser.grammar(JSON.parse(JSON.stringify(spec)))
  const typescript = sources.map((source) => {
    try {
      return { accepted: true, value: parser.parse(source) }
    }
    catch (error) {
      return { accepted: false, code: String(error.code) }
    }
  })

  const rust = ChildProcess.spawnSync(RUNNER, [], {
    input: JSON.stringify({ grammar: spec, sources }),
    encoding: 'utf8',
    maxBuffer: 128 * 1024 * 1024,
    timeout: 30000,
  })
  if (rust.status !== 0) {
    const why = rust.error ? String(rust.error) : rust.stderr
    throw new Error(`Rust spec runner failed (${rust.status}): ${why}`)
  }

  const outcomes = JSON.parse(rust.stdout)
  const failures = []
  sources.forEach((source, index) => {
    try {
      Assert.deepStrictEqual(canonical(outcomes[index]),
        canonical(typescript[index]))
    }
    catch (error) {
      failures.push({
        file: files[index],
        source: source.slice(0, 500),
        size: source.length,
        typescript: typescript[index],
        rust: outcomes[index],
        error: error.message,
      })
    }
  })

  if (failures.length > 0) {
    console.error(JSON.stringify(failures.slice(0, 10), null, 2))
    process.exitCode = 1
  }
  else {
    console.log(`strict JSON TypeScript/Rust fuzz parity: ` +
      `${COUNT}/${COUNT} (seed ${SEED})`)
  }
}
finally {
  Fs.rmSync(work, { recursive: true, force: true })
}
