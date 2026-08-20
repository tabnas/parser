/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */

package tabnas

// The rule-iteration guard, at both ends of `rule.maxmul`.
//
// The guard's budget is `2 * ruleCount * srcLen * 2 * maxmul`, floored at
// 100, in both ports. Its job is to stop a runaway grammar. It must never
// stop a NON-runaway one, because when it does the parse ends incomplete
// and reports `unexpected` — a SYNTAX code, on a valid document, caused
// by a configuration value the document knows nothing about.
//
// Both ports did exactly that, at opposite ends, which is why this file
// and ts/test/rule-budget.test.js exist and assert the same table:
//
//   - Go, at the top. The product was computed with wrapping int64
//     arithmetic, and the floor below turns any negative result into 100.
//     So MaxMul = 2^63-1 rejected a 61-element array that the default 3
//     accepts. Not a usable protection either — it was noise, not a
//     ceiling: 1e18 overflowed to a large POSITIVE value and parsed
//     fine, while 2^63-1 did not. Repaired with a saturating multiply.
//
//   - TypeScript, at the bottom. `rule.maxmul: 0` (and negatives, and
//     NaN) were honoured literally, yielding a zero or negative budget
//     and the same `unexpected` on valid input. Repaired by falling back
//     to the default, which is what this port already did.
//
// Audit item P7. Recorded before this in go/doc/differences.md under
// "Rule-Iteration Budget", in a section headed "These affect parse output
// for the same input" — DIVERGENCE.md's own definition of a divergence,
// in a file DIVERGENCE.md cites as a porting guide. Filed as a porting
// note, it was never pinned, and neither end had a test.

import (
	"strings"
	"testing"
)

// A valid document that needs well over 100 rule iterations, so a budget
// collapsed to the floor cannot complete it.
func ruleBudgetBigSrc() string { return "[" + strings.Repeat("1,", 60) + "1]" }

func TestRuleBudgetNeverRejectsAValidParse(t *testing.T) {
	ip := func(i int) *int { return &i }

	for _, c := range []struct {
		label  string
		maxmul *int
	}{
		{"unset (default 3)", nil},
		{"1", ip(1)},

		// Non-positive: coerced to the default. TypeScript honoured
		// these literally and rejected the same input.
		{"0", ip(0)},
		{"-1", ip(-1)},
		{"most negative int", ip(-9223372036854775808)},

		// The top end. Every one of these overflowed before the repair;
		// only some of them landed negative, which is the point.
		{"1e9", ip(1000000000)},
		{"1e18", ip(1000000000000000000)},
		{"maxint 2^63-1", ip(9223372036854775807)},
	} {
		j := Make(Options{Rule: &RuleOptions{MaxMul: c.maxmul}})
		if err := j.Use(jsonPlugin, nil); err != nil {
			t.Fatalf("%s: plugin: %v", c.label, err)
		}
		v, err := j.Parse(ruleBudgetBigSrc())
		if err != nil {
			t.Errorf("maxmul %s: rejected a valid document: %v", c.label, err)
			continue
		}
		if list, ok := v.([]any); !ok || len(list) != 61 {
			t.Errorf("maxmul %s: got %v, want a 61-element list", c.label, v)
		}
	}
}

// The guard still exists. Without this, the test above is satisfied by
// deleting the budget entirely — the failure mode of every repair that
// makes something stop rejecting.
func TestRuleBudgetStillStopsARunaway(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "loop", Exclude: "tabnas,imp"}})
	j.Rule("loop", func(rs *RuleSpec, p *Parser) {
		// Pushes itself without consuming a token: unbounded.
		rs.AddOpen(&AltSpec{S: [][]Tin{}, P: "loop"})
	})
	if _, err := j.Parse("a"); err == nil {
		t.Fatal("a self-pushing rule parsed without hitting the budget")
	}

	// And it is the BUDGET stopping it, not the grammar: the same rule
	// with a token to consume terminates on its own.
	//
	// Deliberately NOT asserted at a large maxmul. A large multiplier
	// means a large budget in both ports now, so the runaway simply runs
	// longer — that is what asking for it means, and it is what
	// TypeScript has always done. What it must not do is what Go did
	// before the repair, which was to stop EARLY.
	satisfied := Make(Options{Rule: &RuleOptions{Start: "one", Exclude: "tabnas,imp"}})
	satisfied.Rule("one", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}},
			A: func(r *Rule, ctx *Context) { r.Node = r.O0.Val }})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	if v, err := satisfied.Parse("a"); err != nil || v != "a" {
		t.Fatalf("control grammar: %v, %v", v, err)
	}
}

// satMul is the repair itself, and its edges are not exercised by the
// parses above — they only prove no wrap reached the budget.
func TestSatMulSaturatesRatherThanWrapping(t *testing.T) {
	for _, c := range []struct{ a, b, want int }{
		{0, 5, 0},
		{5, 0, 0},
		{-1, 5, 0},
		{5, -1, 0},
		{3, 4, 12},
		{maxInt, 1, maxInt},
		{1, maxInt, maxInt},
		{maxInt, 2, maxInt},
		{2, maxInt, maxInt},
		{maxInt, maxInt, maxInt},
		{1 << 32, 1 << 32, maxInt},
	} {
		if got := satMul(c.a, c.b); got != c.want {
			t.Errorf("satMul(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
