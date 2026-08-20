/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

// The TypeScript twin of go/token_offset_test.go. Together they pin a
// DIVERGENCE, not a contract: a token's `sI` counts UTF-16 code units
// here and UTF-8 bytes in Go, so every token after a non-ASCII one sits
// at a different index in the two ports.
//
// Pinned because DIVERGENCE.md used to say the scan-unit difference was
// "visible only in error positions, never in parsed values", and that was
// false. `sI` is part of the plugin API; @tabnas/c records it in its CST,
// so the difference lands straight in a parsed value — measured there as
// 5 disagreements of 23 probe inputs, every one containing a non-ASCII
// character.
//
// Pinned on BOTH sides so the record cannot outlive what it records:
// repairing the scan unit turns that port's test red and forces the pair
// to be revisited together.
describe('token offset', () => {
  it('counts UTF-16 code units, where Go counts bytes', () => {
    // Observed the way a PLUGIN observes it — through a rule action
    // reading the open token — because that is the exposure the
    // DIVERGENCE.md entry is about.
    const seen = []
    const tn = new Tabnas({ plugins: [json] })
    tn.rule('val', (rs) => {
      rs.ao((r) => {
        if (r.o0 && !r.o0.isNoToken?.()) seen.push(r.o0.sI)
      })
    })

    tn.parse('["\u{1F600}",1]')

    assert.notEqual(seen.length, 0,
      'no token offsets observed, so this test proves nothing')

    // `["😀",1]` — this port sees the `1` at index 6 (the astral
    // character is 2 UTF-16 units); Go sees it at 8 (4 UTF-8 bytes).
    // Both measured, not reasoned: an earlier draft of this comment
    // carried the figures for a different input and was wrong by one.
    const max = Math.max(...seen)
    assert.ok(max < 7,
      'highest observed sI is ' + max + ', at or above the byte index — ' +
      'so this port is no longer counting UTF-16 units. If the scan unit ' +
      'has been repaired, delete this test, its Go twin in ' +
      'go/token_offset_test.go, and the DIVERGENCE.md entry together')
  })
})
