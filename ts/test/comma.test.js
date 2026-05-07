/* Copyright (c) 2013-2023 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama, jsonic } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { loadTSV } = require('./utility')

function tsvTest(name) {
  const entries = loadTSV(name)
  for (const { cols: [input, expected], row } of entries) {
    try {
      assert.deepEqual(J(input), JSON.parse(expected))
    } catch (err) {
      err.message = `${name} row ${row}: input=${input} expected=${expected}\n${err.message}`
      throw err
    }
  }
}

describe('comman', function () {
  it('implicit-comma', () => {
    tsvTest('comma-implicit-comma')
  })

  it('optional-comma', () => {
    tsvTest('comma-optional-comma')
  })
})
