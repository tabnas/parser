/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

import (
	"strings"
	"testing"
)

// Continuations: the Go mirror of ts/test/continuations.test.js. The
// completion primitive of the unified-LSP design — what tokens could
// legally follow a prefix.
//
// Every expectation here was checked against the TS engine on the same
// prefix with the same grammar, so these are cross-runtime parity
// assertions rather than a restatement of whatever Go happens to do.

func names(j *Tabnas, src string) []string {
	_, n := j.Continuations(src)
	return n
}

func hasName(list []string, want string) bool {
	for _, n := range list {
		if n == want {
			return true
		}
	}
	return false
}

func eqNames(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestContinuationsIsPositionAware(t *testing.T) {
	// The defect this pins: reading position 0 lists key starters and
	// misses the one useful completion. After a key, only the colon can
	// follow. TS answers ['#CL'] here.
	j := makeJSON()
	if got := names(j, `{"a"`); !eqNames(got, "#CL") {
		t.Fatalf("want [#CL], got %v", got)
	}
}

func TestContinuationsAfterAColon(t *testing.T) {
	// Value starters, plus the closers the pop closure adds.
	j := makeJSON()
	got := names(j, `{"a":`)
	for _, want := range []string{"#ST", "#NR", "#OB", "#OS"} {
		if !hasName(got, want) {
			t.Fatalf("%s not offered after a colon: %v", want, got)
		}
	}
}

func TestContinuationsAfterACompletePair(t *testing.T) {
	// Either another pair or the object's end — and nothing else.
	j := makeJSON()
	if got := names(j, `{"a":1`); !eqNames(got, "#CB", "#CA") {
		t.Fatalf("want [#CB #CA], got %v", got)
	}
}

func TestContinuationsAfterASeparator(t *testing.T) {
	// The push closure at work: `[1,` offers the next element's value
	// starters as well as the closer, because the alternate that
	// matched the comma is about to push a value rule.
	j := makeJSON()
	got := names(j, `[1,`)
	for _, want := range []string{"#NR", "#ST", "#OB", "#OS", "#CS"} {
		if !hasName(got, want) {
			t.Fatalf("%s not offered after a separator: %v", want, got)
		}
	}
}

func TestContinuationsOfACompleteDocument(t *testing.T) {
	// A finished document reports exactly the end: nothing may follow.
	// #ZZ is a sentinel, not something a user types, so a completion
	// provider reads it as "this document is already valid".
	j := makeJSON()
	if got := names(j, `{"a":1}`); !eqNames(got, "#ZZ") {
		t.Fatalf("want [#ZZ], got %v", got)
	}
}

func TestContinuationsOfAnEmptyPrefix(t *testing.T) {
	// No rule ever became live, so the answer is the start rule's own
	// openers.
	j := makeJSON()
	got := names(j, "")
	for _, want := range []string{"#OB", "#OS", "#ST", "#NR"} {
		if !hasName(got, want) {
			t.Fatalf("%s not offered for an empty prefix: %v", want, got)
		}
	}
}

func TestContinuationsAfterAnOpenBrace(t *testing.T) {
	// Strict JSON wants a string key and nothing else.
	j := makeJSON()
	if got := names(j, `{`); !eqNames(got, "#ST") {
		t.Fatalf("want [#ST], got %v", got)
	}
}

func TestContinuationsIsPathAwareAcrossAlternates(t *testing.T) {
	// Alternates [A B] and [C D]: after matching A only B may follow.
	// A sibling alternate whose own prefix never matched contributes
	// nothing — the collated answer would offer D here.
	j := Make(Options{Fixed: &FixedOptions{Token: map[string]*string{
		"#A": strp("a"), "#B": strp("b"), "#C": strp("c"), "#D": strp("d"),
	}}})
	j.Rule("top", func(rs *RuleSpec, p *Parser) {
		rs.ClearOpen()
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{j.Token("#A")}, {j.Token("#B")}}},
			&AltSpec{S: [][]Tin{{j.Token("#C")}, {j.Token("#D")}}},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	j.SetOptions(Options{Rule: &RuleOptions{Start: "top"}})

	got := names(j, "a")
	if !hasName(got, "#B") {
		t.Fatalf("#B not offered: %v", got)
	}
	if hasName(got, "#D") {
		t.Fatalf("#D offered though its own C prefix never matched: %v", got)
	}
}

func TestContinuationsDropsAlternatesThatMismatchedEarlier(t *testing.T) {
	// Open alternates [A B], [C D], [A]. After `a` the third has
	// completed, so the end is legal, and the first wants B. The second
	// never matched its C: offering it would mean REPLACING the `a`
	// already typed, and `ac` does not parse.
	j := Make(Options{Fixed: &FixedOptions{Token: map[string]*string{
		"#A": strp("a"), "#B": strp("b"), "#C": strp("c"), "#D": strp("d"),
	}}})
	j.Rule("top", func(rs *RuleSpec, p *Parser) {
		rs.ClearOpen()
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{j.Token("#A")}, {j.Token("#B")}}},
			&AltSpec{S: [][]Tin{{j.Token("#C")}, {j.Token("#D")}}},
			&AltSpec{S: [][]Tin{{j.Token("#A")}}},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	j.SetOptions(Options{Rule: &RuleOptions{Start: "top"}})

	got := names(j, "a")
	if !hasName(got, "#B") {
		t.Fatalf("#B not offered: %v", got)
	}
	if hasName(got, "#C") {
		t.Fatalf("#C offered: it would replace the `a`, and `ac` does not parse: %v", got)
	}
	if !hasName(got, "#ZZ") {
		t.Fatalf("#ZZ not offered though `a` parses: %v", got)
	}
	// Corroborate the claim rather than trusting the set.
	if _, err := j.Parse("ab"); err != nil {
		t.Fatalf("`ab` should parse: %v", err)
	}
	if _, err := j.Parse("ac"); err == nil {
		t.Fatal("`ac` should not parse")
	}
}

func TestContinuationsUnaffectedByRecovery(t *testing.T) {
	// Continuations must stop AT the query point. With recovery on, a
	// recovering parse would skip past it and answer about somewhere
	// else, so the query forces fail-fast for its own parse only.
	plain := makeJSON()
	rec := makeJSON(Options{Parse: &ParseOptions{
		Recover: &RecoverOptions{Enabled: true},
	}})

	for _, src := range []string{`{"a"`, `{"a":1`, `[1,`, `{"a":1}`} {
		a := strings.Join(names(plain, src), ",")
		b := strings.Join(names(rec, src), ",")
		if a != b {
			t.Fatalf("%q: recovery changed the answer: %q vs %q", src, a, b)
		}
	}

	// And the instance's own recovery still works afterwards.
	if _, errs, err := rec.ParseRecover(`{"a":1,}`); err != nil || 0 == len(errs) {
		t.Fatalf("recovery disturbed: errs=%d err=%v", len(errs), err)
	}
}

func TestContinuationsDoesNotDisturbLaterParses(t *testing.T) {
	// The query runs a whole parse; it must leave the instance alone.
	j := makeJSON()
	names(j, `{"a"`)
	out, err := j.Parse(`{"a":1}`)
	if err != nil {
		t.Fatalf("parse after a query failed: %v", err)
	}
	if enc(out) != `{"a":1}` {
		t.Fatalf("got %s", enc(out))
	}
}

func strp(s string) *string { return &s }
