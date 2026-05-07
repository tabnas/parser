/* Copyright (c) 2013-2022 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Amagama, jsonic } = require('..')
const am = new Amagama({ plugins: [jsonic] })
const J = (src, meta, ctx) => am.parse(src, meta, ctx)
const { Debug } = require('../dist/debug')

describe('api', function () {
  it('standard', () => {
    const { keys } = Amagama.util

    // Ensure no accidental API expansion
    assert.deepEqual(keys(Amagama), [
      'empty',
      'parse',
      'sub',
      'id',
      'toString',
      'Amagama',
      'AmagamaError',
      'makeLex',
      'makeParser',
      'makeToken',
      'makePoint',
      'makeRule',
      'makeRuleSpec',
      'makeFixedMatcher',
      'makeSpaceMatcher',
      'makeLineMatcher',
      'makeStringMatcher',
      'makeCommentMatcher',
      'makeNumberMatcher',
      'makeTextMatcher',
      'OPEN',
      'CLOSE',
      'BEFORE',
      'AFTER',
      'EMPTY',
      'SKIP',
      'util',
      'make',
      'S',
    ])

    assert.ok(Debug != null)
  })
})
