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

	// Missing counter → always true (matching TS null == this.n[name] || ...).
	if !r.Eq("missing", 99) {
		t.Error("Eq on missing counter should be true")
	}
	if !r.Lt("missing", -1) {
		t.Error("Lt on missing counter should be true")
	}
	if !r.Gt("missing", 99) {
		t.Error("Gt on missing counter should be true")
	}
	if !r.Lte("missing", -1) {
		t.Error("Lte on missing counter should be true")
	}
	if !r.Gte("missing", 99) {
		t.Error("Gte on missing counter should be true")
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
		// Missing property → condition true (matching TS getRuleProp).
		{"$eq", "n", "missing", 99, true},
		{"$lt", "n", "missing", -1, true},
		{"$ne", "n", "missing", 0, true},
		{"$lte", "n", "missing", -1, true},
		{"$gt", "n", "missing", 99, true},
		{"$gte", "n", "missing", 99, true},
		// Unknown prop → not found → true.
		{"$eq", "z", "", 99, true},
		// "n" without subprop → not found → true.
		{"$eq", "n", "", 99, true},
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
	// getRuleProp(nil) → not found → condition true.
	cond, err := MakeRuleCond("$eq", "d", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !cond(nil, nil) {
		t.Error("condition on nil rule should be true")
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
	spec := &RuleSpec{
		Name:  "x",
		Open:  []*AltSpec{{CD: map[string]any{"d": 0}}},
		Close: []*AltSpec{{CD: map[string]any{"d": CGt(0)}}},
	}
	if err := NormAlts(spec); err != nil {
		t.Fatal(err)
	}
	if spec.Open[0].C == nil || spec.Close[0].C == nil {
		t.Error("NormAlts should convert CD to C in both Open and Close")
	}
}

func TestNormAltsOpenError(t *testing.T) {
	spec := &RuleSpec{Open: []*AltSpec{{G: "BAD!"}}}
	if err := NormAlts(spec); err == nil {
		t.Error("expected error for invalid Open group tag")
	}
}

func TestNormAltsCloseError(t *testing.T) {
	spec := &RuleSpec{Close: []*AltSpec{{G: "BAD!"}}}
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
	rs := &RuleSpec{Close: []*AltSpec{a, b, c}}

	// Delete index 0, move last to front (TS rs.close(alts, {delete, move})).
	rs.ModifyClose(&AltModListOpts{Delete: []int{0}, Move: []int{-1, 0}})
	if len(rs.Close) != 2 {
		t.Fatalf("expected 2 alts, got %d", len(rs.Close))
	}
	if rs.Close[0].G != "cc" || rs.Close[1].G != "bb" {
		t.Errorf("expected [cc bb], got [%s %s]", rs.Close[0].G, rs.Close[1].G)
	}

	// Custom modification callback.
	rs.ModifyClose(&AltModListOpts{Custom: func(list []*AltSpec) []*AltSpec {
		return list[:1]
	}})
	if len(rs.Close) != 1 || rs.Close[0].G != "cc" {
		t.Errorf("custom should keep first only, got %v", rs.Close)
	}

	// nil mods → unchanged.
	rs.ModifyClose(nil)
	if len(rs.Close) != 1 {
		t.Error("nil mods should leave list unchanged")
	}
}

func TestModifyCloseCustomReturningNil(t *testing.T) {
	rs := &RuleSpec{Close: []*AltSpec{{G: "aa"}}}
	rs.ModifyClose(&AltModListOpts{Custom: func(list []*AltSpec) []*AltSpec {
		return nil
	}})
	if len(rs.Close) != 1 {
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
