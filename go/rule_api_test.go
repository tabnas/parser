// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"strings"
	"testing"
)

// --- UnwrapUndefined (TS: undefined → null conversion in results) ---

func TestUnwrapUndefinedScalar(t *testing.T) {
	if UnwrapUndefined(Undefined) != nil {
		t.Error("Undefined should unwrap to nil")
	}
	if UnwrapUndefined("x") != "x" {
		t.Error("plain string should pass through")
	}
	if UnwrapUndefined(nil) != nil {
		t.Error("nil should pass through")
	}
}

func TestUnwrapUndefinedMap(t *testing.T) {
	m := map[string]any{"a": Undefined, "b": 1, "c": map[string]any{"d": Undefined}}
	out := UnwrapUndefined(m).(map[string]any)
	if out["a"] != nil {
		t.Errorf("a: expected nil, got %v", out["a"])
	}
	if out["b"] != 1 {
		t.Errorf("b: expected 1, got %v", out["b"])
	}
	inner := out["c"].(map[string]any)
	if inner["d"] != nil {
		t.Errorf("c.d: expected nil, got %v", inner["d"])
	}
}

func TestUnwrapUndefinedArray(t *testing.T) {
	arr := []any{Undefined, "x", []any{Undefined}}
	out := UnwrapUndefined(arr).([]any)
	if out[0] != nil {
		t.Errorf("[0]: expected nil, got %v", out[0])
	}
	if out[1] != "x" {
		t.Errorf("[1]: expected x, got %v", out[1])
	}
	inner := out[2].([]any)
	if inner[0] != nil {
		t.Errorf("[2][0]: expected nil, got %v", inner[0])
	}
}

// --- Rule counter comparisons (TS: rule.eq/lt/gt/lte/gte) ---

func TestRuleCounterComparisons(t *testing.T) {
	r := &Rule{N: map[string]int{"x": 2}}

	// Present counter: normal comparison semantics.
	if !r.Eq("x", 2) || r.Eq("x", 3) {
		t.Error("Eq failed for present counter")
	}
	if !r.Lt("x", 3) || r.Lt("x", 2) {
		t.Error("Lt failed for present counter")
	}
	if !r.Gt("x", 1) || r.Gt("x", 2) {
		t.Error("Gt failed for present counter")
	}
	if !r.Lte("x", 2) || r.Lte("x", 1) {
		t.Error("Lte failed for present counter")
	}
	if !r.Gte("x", 2) || r.Gte("x", 3) {
		t.Error("Gte failed for present counter")
	}

	// An unset counter reads as 0: it has counted nothing. It is NOT "true
	// against everything" — that made Lt and Gt both pass at once, so a guard
	// written the natural way fired before anything had been counted.
	if !r.Eq("missing", 0) || r.Eq("missing", 99) {
		t.Error("Eq on an unset counter should compare against 0")
	}
	if !r.Lt("missing", 1) || r.Lt("missing", -1) || r.Lt("missing", 0) {
		t.Error("Lt on an unset counter should compare against 0")
	}
	if !r.Gt("missing", -1) || r.Gt("missing", 99) || r.Gt("missing", 0) {
		t.Error("Gt on an unset counter should compare against 0")
	}
	if !r.Lte("missing", 0) || r.Lte("missing", -1) {
		t.Error("Lte on an unset counter should compare against 0")
	}
	if !r.Gte("missing", 0) || r.Gte("missing", 99) {
		t.Error("Gte on an unset counter should compare against 0")
	}

	// Trichotomy: exactly one of <, =, > holds — the property the old
	// "unset is true against everything" behaviour violated.
	for _, limit := range []int{-1, 0, 1, 99} {
		n := 0
		if r.Lt("missing", limit) {
			n++
		}
		if r.Eq("missing", limit) {
			n++
		}
		if r.Gt("missing", limit) {
			n++
		}
		if n != 1 {
			t.Errorf("unset counter vs %d: %d of (Lt,Eq,Gt) true, want exactly 1", limit, n)
		}
	}

	// Exist still distinguishes "never counted" from "counted zero".
	if r.Exist("missing") {
		t.Error("Exist should be false for a counter that was never set")
	}
	zero := &Rule{N: map[string]int{"z": 0}}
	if !zero.Exist("z") {
		t.Error("Exist should be true for a counter explicitly set to 0")
	}
	if !zero.Eq("z", 0) || !zero.Eq("missing", 0) {
		t.Error("a set-to-0 counter and an unset counter both compare equal to 0")
	}
}

// --- CondOp constructors (TS: c: { 'n.pk': { $lte: 0 } } declarative form) ---

func TestCondOpConstructors(t *testing.T) {
	tests := []struct {
		op   CondOp
		name string
		val  int
	}{
		{CEq(1), "$eq", 1},
		{CNe(2), "$ne", 2},
		{CLt(3), "$lt", 3},
		{CLte(4), "$lte", 4},
		{CGt(5), "$gt", 5},
		{CGte(6), "$gte", 6},
	}
	for _, tt := range tests {
		if tt.op.Op != tt.name || tt.op.Val != tt.val {
			t.Errorf("expected {%s %d}, got %+v", tt.name, tt.val, tt.op)
		}
	}
}

// --- MakeRuleCond: all comparison operators ---

func TestMakeRuleCondOperators(t *testing.T) {
	r := &Rule{D: 2, N: map[string]int{"pk": 1}}

	tests := []struct {
		op      string
		prop    string
		subprop string
		val     int
		want    bool
	}{
		{"$eq", "d", "", 2, true},
		{"$eq", "d", "", 3, false},
		{"$ne", "d", "", 3, true},
		{"$ne", "d", "", 2, false},
		{"$lt", "d", "", 3, true},
		{"$lt", "d", "", 2, false},
		{"$lte", "d", "", 2, true},
		{"$lte", "d", "", 1, false},
		{"$gt", "d", "", 1, true},
		{"$gt", "d", "", 2, false},
		{"$gte", "d", "", 2, true},
		{"$gte", "d", "", 3, false},
		// Counter subprop access (n.pk).
		{"$eq", "n", "pk", 1, true},
		{"$lte", "n", "pk", 0, false},
		// An unset COUNTER reads as 0 — it has counted nothing — so the
		// comparison is a real comparison, not an automatic pass.
		{"$eq", "n", "missing", 0, true},
		{"$eq", "n", "missing", 99, false},
		{"$ne", "n", "missing", 0, false},
		{"$ne", "n", "missing", 99, true},
		{"$lt", "n", "missing", 1, true},
		{"$lt", "n", "missing", -1, false},
		{"$lte", "n", "missing", 0, true},
		{"$lte", "n", "missing", -1, false},
		{"$gt", "n", "missing", -1, true},
		{"$gt", "n", "missing", 99, false},
		{"$gte", "n", "missing", 0, true},
		{"$gte", "n", "missing", 99, false},
		// A path that does not RESOLVE is not a zero — no counter exists to
		// read. The ORDERED operators stay permissive there (they answer a
		// question the rule cannot answer); $eq fails CLOSED, because "equals
		// 99" cannot be satisfied by a value that is not there. This matches
		// the TS port exactly.
		{"$lt", "z", "", 99, true},
		{"$gte", "z", "", 99, true},
		{"$eq", "z", "", 99, false},
		// "n" without a counter named → does not resolve.
		{"$eq", "n", "", 99, false},
		{"$lt", "n", "", 99, true},
	}
	for _, tt := range tests {
		cond, err := MakeRuleCond(tt.op, tt.prop, tt.subprop, tt.val)
		if err != nil {
			t.Fatalf("MakeRuleCond(%s): %v", tt.op, err)
		}
		if got := cond(r, nil); got != tt.want {
			t.Errorf("MakeRuleCond(%s,%s,%s,%d) = %v, want %v",
				tt.op, tt.prop, tt.subprop, tt.val, got, tt.want)
		}
	}
}

func TestMakeRuleCondNilRule(t *testing.T) {
	// Nothing resolves on a nil rule. The ordered operators stay permissive;
	// $eq fails closed. Same split as the TS port, where the path walk yields
	// undefined and `undefined === 5` is false.
	ordered, err := MakeRuleCond("$lt", "d", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !ordered(nil, nil) {
		t.Error("an ordered condition on a nil rule should stay permissive")
	}

	eq, err := MakeRuleCond("$eq", "d", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if eq(nil, nil) {
		t.Error("$eq on a nil rule should fail closed")
	}
}

func TestMakeRuleCondUnknownOpError(t *testing.T) {
	if _, err := MakeRuleCond("$bogus", "d", "", 0); err == nil {
		t.Error("expected error for unknown comparison operator")
	}
}

// --- NormAlt / NormAlts ---

func TestNormAltNil(t *testing.T) {
	if err := NormAlt(nil); err != nil {
		t.Errorf("NormAlt(nil) should not error: %v", err)
	}
}

func TestNormAltInvalidGroupTag(t *testing.T) {
	alt := &AltSpec{G: "Bad Tag"}
	if err := NormAlt(alt); err == nil {
		t.Error("expected error for invalid group tag")
	}
}

func TestNormAltCDIntCondition(t *testing.T) {
	// CD with plain int → $eq condition.
	alt := &AltSpec{CD: map[string]any{"d": 0}}
	if err := NormAlt(alt); err != nil {
		t.Fatal(err)
	}
	if alt.C == nil {
		t.Fatal("expected C to be set from CD")
	}
	if !alt.C(&Rule{D: 0}, nil) {
		t.Error("d=0 should match CD {d:0}")
	}
	if alt.C(&Rule{D: 1}, nil) {
		t.Error("d=1 should not match CD {d:0}")
	}
}

func TestNormAltCDCondOpCondition(t *testing.T) {
	// CD with CondOp values, including subprop path "n.pk" (matching TS
	// c: { 'n.pk': { $lte: 0 } }).
	alt := &AltSpec{CD: map[string]any{"n.pk": CLte(0)}}
	if err := NormAlt(alt); err != nil {
		t.Fatal(err)
	}
	if alt.C == nil {
		t.Fatal("expected C to be set from CD")
	}
	if !alt.C(&Rule{N: map[string]int{"pk": 0}}, nil) {
		t.Error("pk=0 should satisfy $lte 0")
	}
	if alt.C(&Rule{N: map[string]int{"pk": 1}}, nil) {
		t.Error("pk=1 should not satisfy $lte 0")
	}
}

func TestNormAltCDMultipleConditions(t *testing.T) {
	// Multiple CD entries combine with AND.
	alt := &AltSpec{CD: map[string]any{"d": CGte(1), "n.pk": CLt(2)}}
	if err := NormAlt(alt); err != nil {
		t.Fatal(err)
	}
	if alt.C == nil {
		t.Fatal("expected combined C")
	}
	if !alt.C(&Rule{D: 1, N: map[string]int{"pk": 1}}, nil) {
		t.Error("both conditions hold → true")
	}
	if alt.C(&Rule{D: 0, N: map[string]int{"pk": 1}}, nil) {
		t.Error("first condition fails → false")
	}
	if alt.C(&Rule{D: 1, N: map[string]int{"pk": 3}}, nil) {
		t.Error("second condition fails → false")
	}
}

func TestNormAltCDIgnoredWhenCSet(t *testing.T) {
	// Explicit C takes precedence over CD (CD conversion skipped).
	called := false
	c := AltCond(func(r *Rule, ctx *Context) bool { called = true; return true })
	alt := &AltSpec{C: c, CD: map[string]any{"d": 99}}
	if err := NormAlt(alt); err != nil {
		t.Fatal(err)
	}
	alt.C(&Rule{}, nil)
	if !called {
		t.Error("explicit C should be preserved")
	}
}

func TestNormAlts(t *testing.T) {
	spec := &RuleSpec{Name: "x"}
	spec.AddOpen(&AltSpec{CD: map[string]any{"d": 0}})
	spec.AddClose(&AltSpec{CD: map[string]any{"d": CGt(0)}})
	if err := NormAlts(spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAlts()[0].C == nil || spec.CloseAlts()[0].C == nil {
		t.Error("NormAlts should convert CD to C in both Open and Close")
	}
}

func TestNormAltsOpenError(t *testing.T) {
	spec := &RuleSpec{}
	spec.AddOpen(&AltSpec{G: "BAD!"})
	if err := NormAlts(spec); err == nil {
		t.Error("expected error for invalid Open group tag")
	}
}

func TestNormAltsCloseError(t *testing.T) {
	spec := &RuleSpec{}
	spec.AddClose(&AltSpec{G: "BAD!"})
	if err := NormAlts(spec); err == nil {
		t.Error("expected error for invalid Close group tag")
	}
}

// --- ValidateGroupTags ---

func TestValidateGroupTags(t *testing.T) {
	if err := ValidateGroupTags(""); err != nil {
		t.Errorf("empty string should be valid: %v", err)
	}
	// Empty entries between commas are skipped.
	if err := ValidateGroupTags("ab, ,cd"); err != nil {
		t.Errorf("empty entries should be skipped: %v", err)
	}
	if err := ValidateGroupTags("ab,X"); err == nil {
		t.Error("uppercase tag should be invalid")
	}
	if err := ValidateGroupTags("a"); err == nil {
		t.Error("single-char tag should be invalid (regex requires 2+ chars)")
	}
}

// --- ModifyClose / ModifyOpen ---

func TestModifyClose(t *testing.T) {
	a := &AltSpec{G: "aa"}
	b := &AltSpec{G: "bb"}
	c := &AltSpec{G: "cc"}
	rs := &RuleSpec{}
	rs.AddClose(a, b, c)

	// Delete index 0, move last to front (TS rs.close(alts, {delete, move})).
	rs.ModifyClose(&AltModListOpts{Delete: []int{0}, Move: []int{-1, 0}})
	if len(rs.CloseAlts()) != 2 {
		t.Fatalf("expected 2 alts, got %d", len(rs.CloseAlts()))
	}
	if rs.CloseAlts()[0].G != "cc" || rs.CloseAlts()[1].G != "bb" {
		t.Errorf("expected [cc bb], got [%s %s]", rs.CloseAlts()[0].G, rs.CloseAlts()[1].G)
	}

	// Custom modification callback.
	rs.ModifyClose(&AltModListOpts{Custom: func(list []*AltSpec) []*AltSpec {
		return list[:1]
	}})
	if len(rs.CloseAlts()) != 1 || rs.CloseAlts()[0].G != "cc" {
		t.Errorf("custom should keep first only, got %v", rs.CloseAlts())
	}

	// nil mods → unchanged.
	rs.ModifyClose(nil)
	if len(rs.CloseAlts()) != 1 {
		t.Error("nil mods should leave list unchanged")
	}
}

func TestModifyCloseCustomReturningNil(t *testing.T) {
	rs := &RuleSpec{}
	rs.AddClose(&AltSpec{G: "aa"})
	rs.ModifyClose(&AltModListOpts{Custom: func(list []*AltSpec) []*AltSpec {
		return nil
	}})
	if len(rs.CloseAlts()) != 1 {
		t.Error("custom returning nil should leave list unchanged")
	}
}

// --- Declarative CD via Grammar (CondOp consumed by NormAlt) ---

// --- Group tag validation surfaces through Grammar (error path) ---

func TestGrammarInvalidGroupTagError(t *testing.T) {
	j := Make()
	err := j.Grammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{
			"val": {
				Close: []*GrammarAltSpec{
					{G: "Not A Valid Tag"},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid group tag in Grammar")
	}
	if !strings.Contains(err.Error(), "invalid group tag") {
		t.Errorf("error should mention invalid group tag, got: %s", err)
	}
}

// $exist asks whether the counter was SET, which the comparisons cannot: they
// read an unset counter as 0, so a counter set to 0 and one never set compare
// identically. Go had no $exist at all, while TS implemented it (but never
// listed it as a known operator) — so the documented escape hatch worked in
// neither runtime.
func TestMakeRuleCondExist(t *testing.T) {
	r := &Rule{N: map[string]int{"zero": 0, "two": 2}}

	tests := []struct {
		subprop string
		want    bool // for CExist(true)
	}{
		{"zero", true}, // set to 0 — exists
		{"two", true},
		{"never", false},
	}
	for _, tt := range tests {
		yes, err := MakeRuleCond("$exist", "n", tt.subprop, CExist(true).Val)
		if err != nil {
			t.Fatalf("MakeRuleCond($exist): %v", err)
		}
		if got := yes(r, nil); got != tt.want {
			t.Errorf("$exist:true on n.%s = %v, want %v", tt.subprop, got, tt.want)
		}
		no, err := MakeRuleCond("$exist", "n", tt.subprop, CExist(false).Val)
		if err != nil {
			t.Fatalf("MakeRuleCond($exist): %v", err)
		}
		if got := no(r, nil); got != !tt.want {
			t.Errorf("$exist:false on n.%s = %v, want %v", tt.subprop, got, !tt.want)
		}
	}

	// The distinction the comparisons cannot make.
	if !r.Eq("zero", 0) || !r.Eq("never", 0) {
		t.Error("both a zero counter and an unset counter compare equal to 0")
	}
	if !r.Exist("zero") || r.Exist("never") {
		t.Error("Exist must separate counted-zero from never-counted")
	}
}

// Declarative grammar parts are validated while the grammar is BUILT, so a
// bad spec can never surface during a parse. ValidateAlt reports every problem
// at once rather than stopping at the first, so a grammar held as data can be
// checked before any parser exists.
func TestValidateAltDeclarativeParts(t *testing.T) {
	// Unknown operator, unresolvable path root, unusable value, bad group tag.
	problems := ValidateAlt(&AltSpec{
		CD: map[string]any{
			"zz.depth": CLt(3),               // no such rule property
			"n.ok":     CondOp{Op: "$bogus"}, // unknown operator
			"d":        []int{1},             // unusable value type
		},
		G: "Bad Tag",
	})
	if len(problems) != 4 {
		t.Errorf("want 4 problems, got %d: %v", len(problems), problems)
	}

	// A well-formed alt reports nothing. Plain values are the $eq shorthand,
	// and — matching TS — may be any scalar, not just an int.
	if got := ValidateAlt(&AltSpec{
		CD: map[string]any{"n.pk": CLte(0), "d": 1, "name": "val", "u.on": true},
		G:  "json,map",
	}); len(got) != 0 {
		t.Errorf("valid alt reported problems: %v", got)
	}

	// nil is not a problem.
	if got := ValidateAlt(nil); len(got) != 0 {
		t.Errorf("nil alt reported problems: %v", got)
	}

	// $exist is a known operator (it had none in Go at all before).
	if got := ValidateAlt(&AltSpec{CD: map[string]any{"n.k": CExist(true)}}); len(got) != 0 {
		t.Errorf("$exist reported problems: %v", got)
	}
}

func TestValidateAltsLabelsLocation(t *testing.T) {
	problems := ValidateAlts([]*AltSpec{
		{CD: map[string]any{"n.ok": CLt(1)}},   // fine
		{CD: map[string]any{"nope.x": CLt(1)}}, // bad
	}, "val.open")
	if len(problems) != 1 {
		t.Fatalf("want 1 problem, got %d: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "val.open alt[1]:") {
		t.Errorf("problem should name its location, got %q", problems[0])
	}
}

// NormAlt must REJECT what ValidateAlt reports, rather than skipping it and
// leaving the alternate with fewer conditions than it reads as having.
func TestNormAltRejectsInvalidDeclarative(t *testing.T) {
	if err := NormAlt(&AltSpec{CD: map[string]any{"nope.x": CLt(1)}}); err == nil {
		t.Error("NormAlt must reject an unresolvable condition path")
	}
	if err := NormAlt(&AltSpec{CD: map[string]any{"n.x": CondOp{Op: "$bogus"}}}); err == nil {
		t.Error("NormAlt must reject an unknown condition operator")
	}
}

// Negative coverage for the validation surface: empty input is not a problem,
// valid roots are not rejected (guarding against over-tightening), and every
// bad entry in one alternate is reported rather than just the first.
func TestValidateAltNegativeEdges(t *testing.T) {
	// Empty / nil inputs report nothing.
	if got := ValidateAlts(nil, "x"); len(got) != 0 {
		t.Errorf("nil alts reported %v", got)
	}
	if got := ValidateAlts([]*AltSpec{}, ""); len(got) != 0 {
		t.Errorf("empty alts reported %v", got)
	}
	if got := ValidateAlt(&AltSpec{}); len(got) != 0 {
		t.Errorf("bare alt reported %v", got)
	}

	// The roots Go can actually resolve are accepted.
	for _, prop := range []string{"d", "n.pk", "n.depth"} {
		if got := ValidateAlt(&AltSpec{CD: map[string]any{prop: CLt(1)}}); len(got) != 0 {
			t.Errorf("%s should be valid, got %v", prop, got)
		}
	}

	// Every bad entry is reported, not just the first.
	problems := ValidateAlt(&AltSpec{CD: map[string]any{
		"aa.x": CLt(1),
		"bb.y": CLt(1),
		"n.z":  CondOp{Op: "$nope"},
	}})
	if len(problems) != 3 {
		t.Errorf("want 3 problems, got %d: %v", len(problems), problems)
	}

	// A function condition is opaque, and does not stop CD being checked.
	if got := ValidateAlt(&AltSpec{C: func(r *Rule, ctx *Context) bool { return true }}); len(got) != 0 {
		t.Errorf("function condition should be opaque, got %v", got)
	}
}

// A grammar built through the public path must reject an invalid declarative
// condition, so it can never reach a parse.
func TestNormAltsRejectsInvalidAlternate(t *testing.T) {
	rs := &RuleSpec{Name: "val"}
	rs.AddOpen(&AltSpec{CD: map[string]any{"n.ok": CLt(1)}}) // valid
	rs.AddOpen(&AltSpec{CD: map[string]any{"bad.x": CLt(1)}})

	if err := NormAlts(rs); err == nil {
		t.Error("NormAlts must reject an invalid alternate even after a valid one")
	}
}

// Cross-port alignment: these are the exact cases the TS port produces, so a
// change to either runtime that breaks the pairing shows up here. Every row
// was run through both implementations and confirmed identical.
//
// Note the asymmetry: $eq fails CLOSED on a path that does not resolve
// ("equals x" cannot be satisfied by a value that is not there), while the
// ordered operators fail OPEN (they answer a question the rule cannot
// answer). A named counter always resolves — unset reads as 0.
func TestConditionParityWithTS(t *testing.T) {
	rule := func() *Rule {
		return &Rule{
			Name: "top",
			N:    map[string]int{"set": 2, "zero": 0},
			U:    map[string]any{"mode": "strict"},
			K:    map[string]any{"kept": 7},
			O0:   &Token{Tin: 5},
		}
	}

	cases := []struct {
		path string
		op   string
		val  any
		want bool
	}{
		{"n.set", "$eq", 2, true},
		{"n.set", "$lt", 3, true},
		{"n.set", "$gt", 2, false},
		{"n.unset", "$eq", 0, true},   // unset counter reads as 0
		{"n.unset", "$gt", 99, false}, // ...so it is NOT past a limit
		{"n.unset", "$lt", 1, true},
		{"n.unset", "$exist", true, false},
		{"n.zero", "$exist", true, true}, // set to 0 is not unset
		{"d", "$eq", 0, true},
		{"name", "$eq", "top", true},
		{"name", "$eq", "other", false},
		{"u.mode", "$eq", "strict", true},
		{"u.mode", "$ne", "loose", true},
		{"u.missing", "$eq", "x", false}, // $eq fails CLOSED
		{"u.missing", "$lt", 5, true},    // ordered ops fail OPEN
		{"k.kept", "$eq", 7, true},
		{"o0.tin", "$gt", 0, true},
	}

	for _, tc := range cases {
		prop, sub := tc.path, ""
		if dot := strings.SplitN(tc.path, ".", 2); len(dot) == 2 {
			prop, sub = dot[0], dot[1]
		}
		cond, err := MakeRuleCond(tc.op, prop, sub, tc.val)
		if err != nil {
			t.Fatalf("%s %s %v: %v", tc.path, tc.op, tc.val, err)
		}
		if got := cond(rule(), nil); got != tc.want {
			t.Errorf("%s %s %v = %v, want %v (TS produces %v)",
				tc.path, tc.op, tc.val, got, tc.want, tc.want)
		}
	}
}
