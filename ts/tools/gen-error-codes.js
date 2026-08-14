#!/usr/bin/env node
/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// Generates schema/error-codes.json — the machine-readable registry of the
// engine's error codes with their message and hint templates. Run from ts/:
//
//   npm run gen-registry        (or: node tools/gen-error-codes.js)
//
// Requires the built dist (npm run build first): the registry is read from a
// default instance's MERGED catalogues, not from source text, so it records
// exactly what the engine ships. ts/test/schema.test.js regenerates the
// registry in memory and fails on any drift from the committed file, and
// go/schema_test.go asserts the Go catalogues match it byte-for-byte.
//
// Only the CODES are contractual across runtimes (AGENTS.md "Error codes");
// the registry pins the templates anyway so that drift is a deliberate,
// visible act rather than an accident.

const Fs = require('node:fs')
const Path = require('node:path')

// The package main (ts/package.json "main": "dist/tabnas.js").
const { Tabnas, VERSION } = require('../dist/tabnas.js')

const REGISTRY_PATH = Path.join(__dirname, '..', '..', 'schema', 'error-codes.json')

// The Go-only `internal` code (go/tabnas.go errorMessages/defaultHints),
// recorded here as a literal because this generator runs on the TS engine,
// which has no such code. The Go half of the drift gate (go/schema_test.go)
// is what keeps this block honest: it compares these bytes against the Go
// catalogues, so an edit on either side without the other fails the build.
const GO_ONLY = {
  internal: {
    $comment:
      'Go-only: raised when the Go engine recovers a panic from a plugin ' +
      'callback or matcher (go/parser.go, go/plugin.go). TypeScript has no ' +
      'counterpart (see AGENTS.md "Error codes"). Recorded verbatim from ' +
      'go/tabnas.go; go/schema_test.go keeps this literal honest.',
    message: 'internal error: {src}',
    hint:
      'The parser failed unexpectedly; this is a bug in tabnas\n' +
      'or a plugin, not in your input.',
  },
}

// The hint templates in ts/src/defaults.ts are template literals whose text
// starts on the line AFTER the opening backtick, so every merged value
// carries one leading newline that is source formatting, not hint text (the
// display path indents/reflows the hint anyway). The registry records the
// authored text itself, which is also what the Go catalogue declares.
function hintText(hint) {
  return hint.startsWith('\n') ? hint.substring(1) : hint
}

// Build the registry object from a default instance's merged catalogues.
// Exported so ts/test/schema.test.js can regenerate in memory and compare.
function buildRegistry() {
  const tn = new Tabnas()
  const opts = tn.options()
  const error = opts.error
  const hint = opts.hint

  const codes = {}
  for (const code of Object.keys(error)) {
    if ('string' !== typeof hint[code]) {
      throw new Error(
        'gen-error-codes: code "' + code + '" has a message but no hint ' +
        '(ts/src/defaults.ts error/hint blocks must declare the same codes)',
      )
    }
    codes[code] = {
      message: error[code],
      hint: hintText(hint[code]),
    }
  }
  for (const code of Object.keys(hint)) {
    if (!(code in codes)) {
      throw new Error(
        'gen-error-codes: code "' + code + '" has a hint but no message ' +
        '(ts/src/defaults.ts error/hint blocks must declare the same codes)',
      )
    }
  }

  return {
    $comment:
      "Generated from the engine's merged catalogues. Regenerate with: " +
      'cd ts && npm run gen-registry (or node tools/gen-error-codes.js). ' +
      'CI fails on drift.',
    version: VERSION,
    codes,
    goOnly: GO_ONLY,
  }
}

function writeRegistry() {
  const registry = buildRegistry()
  Fs.writeFileSync(REGISTRY_PATH, JSON.stringify(registry, null, 2) + '\n')
  console.log(
    'wrote ' + REGISTRY_PATH + ' (' +
    Object.keys(registry.codes).length + ' codes + ' +
    Object.keys(registry.goOnly).length + ' Go-only)',
  )
}

if (require.main === module) {
  writeRegistry()
}

module.exports = { buildRegistry, REGISTRY_PATH }
