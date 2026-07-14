// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package tabnas

// Coverage for `#AA` as a true ANY-token wildcard in alt S lists,
// porting ts/test/aa-wildcard.test.js. The TS engine stores alt tin
// constraints as per-partition bitsets and had two bugs around #AA
// (high-tin tokens failing the bitset AND; bitAA leaking into other
// partitions). The Go engine stores constraints as plain []Tin slices,
// so the partition mechanics don't exist here — but the SEMANTICS the
// TS tests pin down do: a slot listing TinAA must match any token
// (low- or high-tin), and unrelated tokens must never leak through an
// alt that doesn't list them.

import (
	"fmt"
	"strings"
	"testing"
)

// aaTop builds an instance whose start rule `top` opens on the given
// alt slot and closes on end-of-source.
func aaTop(t *testing.T, j *Tabnas, slot []Tin) {
	t.Helper()
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{slot}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
}

func TestAAWildcardMatchesLowTinFixedToken(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	// A built-in low-tin token: `{` (#OB, tin 12).
	aaTop(t, j, []Tin{TinAA})
	if _, err := j.Parse("{"); err != nil {
		t.Errorf("#AA should match a low-tin fixed token: %v", err)
	}

	// And a freshly registered custom fixed token.
	j2 := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j2.Token("#Ta", "a")
	aaTop(t, j2, []Tin{TinAA})
	if _, err := j2.Parse("a"); err != nil {
		t.Errorf("#AA should match a custom fixed token: %v", err)
	}
}

func TestAAWildcardMatchesHighTinFixedToken(t *testing.T) {
	// Push the test token's tin above 31 (the TS partition-0 boundary)
	// by registering filler literals first. The Go engine has no
	// partitions, but the ported test still proves #AA is tin-magnitude
	// independent.
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	for n := 0; n < 40; n++ {
		j.Token(fmt.Sprintf("#F%d", n), fmt.Sprintf("f%d", n))
	}
	tx := j.Token("#Tx", "x")
	if tx < 31 {
		t.Fatalf("test setup bug: Tx tin %d expected >= 31", tx)
	}

	aaTop(t, j, []Tin{TinAA})
	if _, err := j.Parse("x"); err != nil {
		t.Errorf("#AA should match a high-tin fixed token: %v", err)
	}
}

func TestAAWildcardViaGrammarSpecName(t *testing.T) {
	// The declarative grammar path: `S: "#AA"` resolves through
	// resolveTokenName and must behave as the same wildcard.
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Token("#Ta", "a")
	err := j.Grammar(&GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"top": {
			Open:  []*GrammarAltSpec{{S: "#AA"}},
			Close: []*GrammarAltSpec{{S: "#ZZ"}},
		},
	}})
	if err != nil {
		t.Fatalf("Grammar: %v", err)
	}
	if _, err := j.Parse("a"); err != nil {
		t.Errorf("#AA via grammar spec should match: %v", err)
	}
}

func TestAAWildcardNoUnrelatedTokenLeak(t *testing.T) {
	// Sibling regression (TS: "unrelated token bits no longer leak via
	// bitAA"): an alt that lists ONLY one specific token must reject a
	// different token, including when both tins sit in the same TS
	// partition (>= 31) at different bit offsets.
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	// Fill up tins so the two test tokens land at tins >= 31.
	for n := 0; n < 34; n++ {
		j.Token(fmt.Sprintf("#F%d", n), fmt.Sprintf("f%d", n))
	}
	pa35 := j.Token("#Pa35", "p")
	pa44 := j.Token("#Pa44", "q")
	if pa35 == pa44 {
		t.Fatalf("test setup bug: tins must stay distinct")
	}

	aaTop(t, j, []Tin{pa35}) // only accept the first one
	if _, err := j.Parse("p"); err != nil {
		t.Errorf("listed token should match: %v", err)
	}
	_, err := j.Parse("q")
	if err == nil {
		t.Fatal("unlisted token must not match an alt that does not list it")
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("expected 'unexpected' error, got: %v", err)
	}
}
