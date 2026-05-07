/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama, AmagamaError } = require('..')
const { Debug } = require('../dist/debug')

describe('debug', function () {
  it('plugin', () => {
    let jd = Amagama.make().use(Debug)
    assert.ok(jd.debug.describe() != null)
  })
})
