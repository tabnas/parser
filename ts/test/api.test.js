/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const am = new Tabnas()
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { Debug } = require('../dist/plugins/debug')

describe('api', function () {
  it('standard', () => {
    const { keys } = Tabnas.util

    // Ensure no accidental static-API expansion on the class itself.
    assert.deepEqual(keys(Tabnas), [
      'util',
      'S',
      'OPEN',
      'CLOSE',
      'BEFORE',
      'AFTER',
      'EMPTY',
      'SKIP',
    ])

    // Spot-check the instance shape. Plugins decorate instances
    // further; this just guards against accidental additions to the
    // core class.
    assert.deepEqual(keys(am).sort(), [
      'fixed',
      'id',
      'options',
      'parent',
      'token',
      'tokenSet',
    ])

    assert.ok(Debug != null)
  })
})
