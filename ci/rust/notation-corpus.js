#!/usr/bin/env node
/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Compile representative ABNF and EBNF grammars to the shared pure-data wire
// format, then require TypeScript and Rust to agree on every value/error result.
// GBNF has its own larger maintained-corpus gate in gbnf-corpus.js.

const Assert = require('node:assert')
const Fs = require('node:fs')
const Path = require('node:path')
const ChildProcess = require('node:child_process')

const PARSER_ROOT = Path.resolve(__dirname, '..', '..')
const TABNAS_ROOT = Path.resolve(process.env.TABNAS_ROOT ||
  Path.join(PARSER_ROOT, '..'))
const RUST_RUNNER = Path.join(PARSER_ROOT, 'rs', 'target', 'debug',
  process.platform === 'win32' ? 'spec_runner.exe' : 'spec_runner')

const paths = {
  parser: Path.join(PARSER_ROOT, 'ts', 'dist', 'tabnas'),
  bnf: Path.join(TABNAS_ROOT, 'bnf', 'ts', 'dist', 'bnf'),
  abnf: Path.join(TABNAS_ROOT, 'abnf', 'ts', 'dist', 'abnf'),
  ebnf: Path.join(TABNAS_ROOT, 'ebnf', 'ts', 'dist', 'ebnf'),
}
for (const path of [RUST_RUNNER, ...Object.values(paths)]) {
  if (!Fs.existsSync(path) && !Fs.existsSync(path + '.js')) {
    throw new Error(`notation parity prerequisite is missing: ${path}`)
  }
}

const { Tabnas } = require(paths.parser)
const { toPureSpec, toJsonic } = require(paths.bnf)

const SUITES = [
  {
    name: 'abnf',
    convert: require(paths.abnf).abnfConvert,
    cases: [
      {
        grammar: 'greet = "hi" / "hello"',
        accept: ['hi', 'hello'],
        reject: ['nope', 'h'],
      },
      {
        grammar: 'pair = "a" "b"',
        accept: ['ab'],
        reject: ['a', 'ba'],
      },
      {
        grammar: 'expr = term *("+" term)\n' +
          'term = "(" expr ")" / number\nnumber = 1*DIGIT',
        accept: ['1', '1+2', '(1+2)+3'],
        reject: ['1+', '(1'],
      },
      {
        grammar: 'R = [ A "@" ] A\nA = 1*ALPHA',
        accept: ['ab', 'a@b', 'a'],
        reject: ['a@', '@'],
      },
    ],
  },
  {
    name: 'ebnf',
    convert: require(paths.ebnf).ebnfConvert,
    cases: [
      {
        grammar: 'Greet ::= "hi" | "hello"',
        accept: ['hi', 'hello'],
        reject: ['nope'],
      },
      {
        grammar: 'Pair ::= "a" "b" "c"',
        accept: ['abc'],
        reject: ['ab', 'bac'],
      },
      {
        grammar: 'A ::= "x"* "end"',
        accept: ['end', 'x end', 'x x x end'],
        reject: ['y end'],
      },
      {
        grammar: 'A ::= [0-9]+',
        accept: ['1', '1234'],
        reject: ['abc'],
      },
      {
        grammar: 'A ::= ( "a" | "b" ) "c"',
        accept: ['ac', 'bc'],
        reject: ['cc'],
      },
      {
        grammar: 'A ::= #x41 #x42',
        accept: ['AB'],
        reject: ['ab'],
      },
    ],
  },
]

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

let checked = 0
const failures = []
for (const suite of SUITES) {
  let passed = 0
  let total = 0
  for (const entry of suite.cases) {
    const liveSpec = suite.convert(entry.grammar, { builtins: true })
    const spec = JSON.parse(toJsonic(toPureSpec(liveSpec), { strict: true }))
    const cases = [
      ...entry.accept.map((source) => ({ source, expected: true })),
      ...entry.reject.map((source) => ({ source, expected: false })),
    ]
    total += cases.length
    const rust = rustOutcomes(spec, cases.map((item) => item.source))
    cases.forEach((item, index) => {
      const ts = tsOutcome(spec, item.source)
      try {
        Assert.equal(ts.accepted, item.expected, 'TypeScript corpus verdict')
        Assert.equal(rust[index].accepted, item.expected, 'Rust corpus verdict')
        Assert.deepStrictEqual(rust[index], ts, 'TypeScript/Rust outcome')
        passed++
      }
      catch (error) {
        failures.push({
          notation: suite.name,
          grammar: entry.grammar,
          source: item.source,
          expected: item.expected,
          typescript: ts,
          rust: rust[index],
          error: error.message,
        })
      }
    })
  }
  checked += passed
  console.log(`${suite.name}: ${passed}/${total}`)
}

if (failures.length > 0) {
  console.error(JSON.stringify(failures, null, 2))
  process.exitCode = 1
}
else {
  console.log(`ABNF/EBNF TypeScript/Rust value+error parity: ${checked}/${checked}`)
}
