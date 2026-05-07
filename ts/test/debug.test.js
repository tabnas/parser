/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama, jsonic, AmagamaError } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { Debug } = require('../dist/debug')

describe('debug', function () {
  it('plugin', () => {
    let jd = am.make().use(Debug)
    assert.ok(jd.debug.describe() != null)
  })
})
