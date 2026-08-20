// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package tabnas

import "testing"

// TestTokenOffsetIsAByteOffset pins a DIVERGENCE, not a contract:
// Token.SI counts UTF-8 bytes here and UTF-16 code units in TypeScript,
// so every token after a non-ASCII one sits at a different index in the
// two ports.
//
// It is pinned because DIVERGENCE.md used to say the scan-unit difference
// was "visible only in error positions, never in parsed values" — and
// that was false. Token.SI is part of the plugin API; @tabnas/c records
// it in its CST, so the difference lands straight in a parsed value.
// Measured there: 5 disagreements of 23 probe inputs, every one an input
// with a non-ASCII character.
//
// Pinned on BOTH sides, with the TypeScript twin in
// ts/test/token-offset.test.js, so the record cannot outlive the
// divergence: repairing the scan unit turns that port's test red and
// forces the pair to be revisited together.
func TestTokenOffsetIsAByteOffset(t *testing.T) {
	// `["😀" 1]` — the astral character is 4 UTF-8 bytes and 2 UTF-16
	// code units, so the tokens after it are displaced by two.
	// Observed the way a PLUGIN observes it — through a rule action
	// reading Token.SI — because that is the exposure the DIVERGENCE.md
	// entry is about.
	j := makeJSON()
	var seen []int
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddAO(func(r *Rule, ctx *Context) {
			if !r.O0.IsNoToken() {
				seen = append(seen, r.O0.SI)
			}
		})
	})

	if _, err := j.Parse("[\"\U0001F600\",1]"); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if 0 == len(seen) {
		t.Fatal("no token offsets observed, so this test proves nothing")
	}

	// `["😀",1]` — the astral character is 4 UTF-8 bytes here and 2
	// UTF-16 code units in TypeScript, so the `1` sits at 8 here and 6
	// there. Both measured, not reasoned: an earlier draft of this
	// comment carried the figures for a different input and was wrong by
	// one.
	//
	// The EXACT sequence, not a bound. A `max >= 7` bound would still
	// pass if this port moved to code-point offsets (the `1` would land
	// at 5... which is below 7, so that one failed open the other way) —
	// and, more to the point, the TypeScript twin's `max < 7` passed for
	// UTF-16 offsets AND for code-point offsets alike, so it did not pin
	// the unit it names. Review caught that; a bound is not a pin when
	// two candidate units fall on the same side of it.
	want := []int{0, 1, 8}
	if len(seen) != len(want) {
		t.Fatalf("observed %d token offsets %v, want %d — the sequence "+
			"changed shape, so the comparison below would not mean what "+
			"it says", len(seen), seen, len(want))
	}
	for i, w := range want {
		if seen[i] != w {
			t.Errorf("Token.SI sequence is %v, want %v (UTF-8 byte "+
				"offsets). If the scan unit has been repaired, delete "+
				"this test, its TypeScript twin in "+
				"ts/test/token-offset.test.js, and the DIVERGENCE.md "+
				"entry together", seen, want)
			break
		}
	}
}
