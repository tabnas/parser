/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// A minimal CSV grammar built directly on the bare tabnas engine — the
// worked example from doc/plugins.md, and the canonical counterpart to
// the Go jsonic/csv_grammar_test.go suite (which names this file as its
// mirror). It demonstrates a plugin replacing the standard rules
// entirely: comma-separated tokens are cells, newline-separated rows are
// records, and single tokens (text, number, string, keyword) are cell
// values. Empty cells become ''; empty rows are dropped.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

function makeCSV() {
  const am = new Tabnas()

  // Drop #LN from IGNORE so newlines survive into the rule stream;
  // keep #SP and #CM ignored. Select the custom start rule and exclude
  // the engine's implicit-structure groups.
  am.options({
    rule: { start: 'csv', exclude: 'tabnas,imp' },
    lex: { emptyResult: [] },
    tokenSet: { IGNORE: ['#SP', null, '#CM'] },
  })

  const { CA, LN, ZZ } = am.token
  const { VAL } = am.tokenSet

  // csv: outer list of rows. A fresh bo resets the node per parse.
  // After each row closes, bc appends its (non-empty) cells.
  const collectRow = (r) => {
    const cells = r.child && r.child.node
    if (Array.isArray(cells) && 0 < cells.length) r.node.push(cells)
  }

  am.rule('csv', (rs) =>
    rs
      .bo((r) => (r.node = []))
      .open([{ s: [ZZ] }, { p: 'row' }])
      .close([
        { s: [LN, ZZ] },
        { s: [LN], r: 'csvcont' },
        { s: [ZZ] },
      ])
      .bc(collectRow),
  )

  // csvcont: tail-call sibling of csv; inherits the outer-list node so
  // the replace chain carries the rows accumulated so far.
  am.rule('csvcont', (rs) =>
    rs
      .open([{ s: [ZZ] }, { p: 'row' }])
      .close([
        { s: [LN, ZZ] },
        { s: [LN], r: 'csvcont' },
        { s: [ZZ] },
      ])
      .bc(collectRow),
  )

  // row: initialises the row from its first cell, then hands the
  // continuation to rowcont. Row-ending tokens at open produce an empty
  // row, which csv.bc drops.
  am.rule('row', (rs) =>
    rs
      .open([
        { s: [VAL], a: (r) => (r.node = [r.o0.val]) },
        { s: [CA], b: 1, a: (r) => (r.node = ['']) },
        { s: [LN], b: 1, a: (r) => (r.node = []) },
        { s: [ZZ], b: 1, a: (r) => (r.node = []) },
      ])
      .close([
        { s: [CA], r: 'rowcont' },
        { s: [LN], b: 1 },
        { s: [ZZ], b: 1 },
      ]),
  )

  // rowcont: appends further cells into the row. JS arrays are shared by
  // reference, so push mutates the node the parent rule reads.
  am.rule('rowcont', (rs) =>
    rs
      .open([
        { s: [VAL], a: (r) => r.node.push(r.o0.val) },
        { s: [CA], b: 1, a: (r) => r.node.push('') },
        { s: [LN], b: 1, a: (r) => r.node.push('') },
        { s: [ZZ], b: 1, a: (r) => r.node.push('') },
      ])
      .close([
        { s: [CA], r: 'rowcont' },
        { s: [LN], b: 1 },
        { s: [ZZ], b: 1 },
      ]),
  )

  return am
}

describe('csv-grammar', () => {
  const cases = [
    ['empty-input', '', []],
    ['single-row', 'a,b,c', [['a', 'b', 'c']]],
    ['multiple-rows', 'a,b\nc,d', [['a', 'b'], ['c', 'd']]],
    ['trailing-newline', 'a,b,c\n', [['a', 'b', 'c']]],
    ['blank-lines-skipped', 'a,b\n\nc,d\n', [['a', 'b'], ['c', 'd']]],
    ['numbers-parsed', '1,2,3', [[1, 2, 3]]],
    ['quoted-strings', '"hello","world"', [['hello', 'world']]],
    ['mixed-types', 'a,1,"x",true', [['a', 1, 'x', true]]],
    ['empty-leading-field', ',a,b', [['', 'a', 'b']]],
    ['empty-middle-field', 'a,,b', [['a', '', 'b']]],
    ['empty-trailing-field', 'a,b,', [['a', 'b', '']]],
    ['single-cell-rows', 'x\ny', [['x'], ['y']]],
    ['keywords', 'true,false,null', [[true, false, null]]],
  ]

  for (const [name, src, want] of cases) {
    it(name, () => {
      assert.deepEqual(makeCSV().parse(src), want)
    })
  }
})
