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
		{"most negative int", ip(-maxInt - 1)},

		// The top end. Every one of these overflowed before the repair;
		// only some of them landed negative, which is the point.
		//
		// Derived from maxInt rather than written out, so the file
		// compiles on a 32-bit target: `1e18` is not a valid `int`
		// there, and an untyped constant that large is a compile
		// error, not a test failure.
		{"maxInt / 8", ip(maxInt / 8)},
		{"maxInt / 2", ip(maxInt / 2)},
		{"maxInt", ip(maxInt)},
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
		// Two factors whose product overflows on any platform width.
		{maxInt/2 + 1, 4, maxInt},
	} {
		if got := satMul(c.a, c.b); got != c.want {
			t.Errorf("satMul(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// The budget scales with SOURCE LENGTH, and the two ports must measure
// that length in the same unit. They did not: UTF-16 code units in
// TypeScript (`lex.src.length`, free) and BYTES here (`len(src)`). Same
// document, different budget, for anything above U+007F.
//
// Reachable, and found by review rather than by any sweep — every sweep
// so far varied the OPTION and left the source ASCII. A grammar that
// pushes N children before consuming the source separates the two: over
// 30 astral characters it parsed to N = 2879 here and only to N = 1439
// there, so every N in between was accept-in-Go / reject-in-TypeScript
// on identical input.
//
// Aligned by counting UTF-16 units here too (`utf16Len`), which costs
// one non-decoding pass and leaves ASCII untouched. Not aligned on rune
// counts: TypeScript would need an O(n) string iteration to get those,
// on a hot path where it currently reads a field.
//
// ts/test/rule-budget.test.js asserts the same five rows with the same
// two numbers. Audit item P7.
func TestRuleBudgetMeasuresSourceInUTF16Units(t *testing.T) {
	for _, c := range []struct {
		label string
		src   string
		last  int // largest N that still parses
	}{
		// 60 UTF-16 units each, whatever their byte length: 60, 120 and
		// 180 bytes respectively. All three had different budgets here
		// before; all three have the same one now.
		{"60 ascii", strings.Repeat("a", 60), 1439},
		{"30 astral", strings.Repeat("\U0001F600", 30), 1439},
		{"60 U+20AC", strings.Repeat("\u20ac", 60), 1439},
		{"30 ascii + 15 astral",
			strings.Repeat("a", 30) + strings.Repeat("\U0001F600", 15), 1439},

		// The control: twice the units, twice the budget. Without this
		// the rows above are also satisfied by ignoring source length.
		{"120 ascii", strings.Repeat("a", 120), 2879},
	} {
		if !deepPushParses(c.last, c.src) {
			t.Errorf("%s: N=%d should parse", c.label, c.last)
		}
		if deepPushParses(c.last+1, c.src) {
			t.Errorf("%s: N=%d should exceed the budget", c.label, c.last+1)
		}
	}
}

// A grammar whose iteration count is driven by N and not by the source:
// `top` pushes a `deep` chain N levels deep before `deep` consumes the
// single text token.
func deepPushParses(n int, src string) bool {
	j := Make(Options{Rule: &RuleOptions{Start: "top", Exclude: "tabnas,imp"}})
	j.Rule("top", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{}, P: "deep"})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}},
			A: func(r *Rule, ctx *Context) { r.Node = r.Child.Node }})
	})
	j.Rule("deep", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{}, P: "deep",
			C: func(r *Rule, ctx *Context) bool { return r.D < n }})
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}},
			A: func(r *Rule, ctx *Context) { r.Node = r.O0.Val }})
		rs.AddClose(&AltSpec{A: func(r *Rule, ctx *Context) {
			if r.Node == nil && r.Child != nil {
				r.Node = r.Child.Node
			}
		}})
	})
	_, err := j.Parse(src)
	return err == nil
}

func TestUTF16Len(t *testing.T) {
	for _, c := range []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"\u00e9", 1},       // 2 bytes, 1 unit
		{"\u20ac", 1},       // 3 bytes, 1 unit
		{"\U0001F600", 2},   // 4 bytes, a surrogate PAIR
		{"a\U0001F600b", 4}, // 6 bytes
		{"\u00e9\u20ac", 2}, // 5 bytes
		// Malformed UTF-8, which this engine passes through rather
		// than rejecting. The budget only needs a finite, stable
		// number here, and these are what the rule gives: 0xFF is a
		// non-continuation byte with a lead >= 0xF0, so it counts 2;
		// a stray 0x80 is a continuation byte and counts 0.
		{"\xff", 2},
		{"a\x80b", 2},
	} {
		if got := utf16Len(c.s); got != c.want {
			t.Errorf("utf16Len(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

// `rule.maxmul` reaches this port through an untyped options map — the
// path SetOptionsText and a shared options blob take.
//
// It did not. MapToOptions handled `rule.start`, `finish`, `include` and
// `exclude` and silently dropped `maxmul`, so a shared options blob that
// set the runaway multiplier configured TypeScript and left Go on its
// default, with nothing to notice. Found by review while checking a
// claim in DIVERGENCE.md that described a conversion no code performed.
func TestMaxMulSurvivesTheOptionsMap(t *testing.T) {
	for _, c := range []struct {
		label string
		val   any
		want  int
	}{
		{"JSON number", float64(7), 7},
		{"Go int", 7, 7},
		{"zero", float64(0), 0},
		{"negative", float64(-1), -1},

		// The DIVERGENCE.md entry's claim, now true: a fractional
		// multiplier truncates to 0 here and is coerced to the default
		// at parse time. TypeScript keeps the fraction.
		{"fractional", float64(0.01), 0},
	} {
		opts := MapToOptions(map[string]any{"rule": map[string]any{"maxmul": c.val}})
		if opts.Rule == nil || opts.Rule.MaxMul == nil {
			t.Errorf("%s: maxmul dropped", c.label)
			continue
		}
		if got := *opts.Rule.MaxMul; got != c.want {
			t.Errorf("%s: maxmul = %d, want %d", c.label, got, c.want)
		}
	}

	// Absent stays absent, rather than becoming a zero that the parse
	// then reads as "coerce me".
	opts := MapToOptions(map[string]any{"rule": map[string]any{"start": "val"}})
	if opts.Rule != nil && opts.Rule.MaxMul != nil {
		t.Errorf("maxmul invented from an options map that had none: %d",
			*opts.Rule.MaxMul)
	}
}

// The multiplier must reach the guard through EVERY path that sets it, not
// only through Make.
//
// `MaxMul` lives on the Parser, and `SetOptions` rebuilds the Config, so
// `SetOptions(Options{Rule: &RuleOptions{MaxMul: ...}})` left the budget at
// whatever construction had computed. TypeScript reads
// `ctx.cfg.rule.maxmul` at parse time off a config that IS rebuilt, so the
// same call took effect there. `MapToOptions` had the same shape of gap and
// is pinned separately above.
//
// Asserted on the BUDGET, not on a parse result. Under the repaired
// semantics every valid document parses at every integer multiplier — the
// guard has an order of magnitude of headroom and the floor covers the rest
// — so "did it parse?" cannot tell whether the option arrived. What the
// value is FOR is the size of the budget, so the boundary N is what the
// test reads: doubling the multiplier must double the reachable depth.
//
// This is why no shared .tsv fixture covers this option: the fixture format
// is input -> parse result, and the parse result does not move.
func TestMaxMulTakesEffectThroughSetOptions(t *testing.T) {
	src := strings.Repeat("a", 60)

	// Boundary N at the default 3, measured by the same push-chain grammar
	// as TestRuleBudgetMeasuresSourceInUTF16Units.
	base := 1439
	if !deepPushParses(base, src) || deepPushParses(base+1, src) {
		t.Fatalf("default boundary is not %d — the other tests in this file "+
			"pin it, so fix those first", base)
	}

	for _, c := range []struct {
		label  string
		maxmul int
		want   int
	}{
		{"6 doubles it", 6, 2879},
		{"1 divides it by three", 1, 479},

		// Coerced to the default, so the boundary must not move.
		{"0 is the default", 0, base},
		{"-1 is the default", -1, base},
	} {
		if got := setOptionsBoundary(t, c.maxmul, src); got != c.want {
			t.Errorf("%s: boundary via SetOptions = %d, want %d "+
				"(default is %d — an unchanged boundary means the option "+
				"never reached the parser)", c.label, got, c.want, base)
		}
	}
}

// setOptionsBoundary returns the largest push depth that still parses, with
// maxmul applied through SetOptions after the instance exists.
func setOptionsBoundary(t *testing.T, maxmul int, src string) int {
	t.Helper()
	parses := func(n int) bool {
		j := Make(Options{Rule: &RuleOptions{Start: "top", Exclude: "tabnas,imp"}})
		j.Rule("top", func(rs *RuleSpec, p *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{}, P: "deep"})
			rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}},
				A: func(r *Rule, ctx *Context) { r.Node = r.Child.Node }})
		})
		j.Rule("deep", func(rs *RuleSpec, p *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{}, P: "deep",
				C: func(r *Rule, ctx *Context) bool { return r.D < n }})
			rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}},
				A: func(r *Rule, ctx *Context) { r.Node = r.O0.Val }})
			rs.AddClose(&AltSpec{A: func(r *Rule, ctx *Context) {
				if r.Node == nil && r.Child != nil {
					r.Node = r.Child.Node
				}
			}})
		})
		j.SetOptions(Options{Rule: &RuleOptions{MaxMul: &maxmul}})
		_, err := j.Parse(src)
		return err == nil
	}
	lo, hi := 1, 6000
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if parses(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}
