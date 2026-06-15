// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"reflect"
	"strings"
	"testing"
)

// --- resolveGrammarAlt: full field resolution (typed Grammar path) ---

func TestResolveGrammarAltAllFields(t *testing.T) {
	j := Make()
	ref := map[FuncRef]any{
		"@bf": func(r *Rule, ctx *Context) int { return 1 },
		"@pf": func(r *Rule, ctx *Context) string { return "map" },
		"@rf": func(r *Rule, ctx *Context) string { return "list" },
		"@a":  AltAction(func(r *Rule, ctx *Context) {}),
		"@e":  AltError(func(r *Rule, ctx *Context) *Token { return nil }),
		"@h":  AltModifier(func(alt *AltSpec, r *Rule, ctx *Context) *AltSpec { return alt }),
		"@c":  AltCond(func(r *Rule, ctx *Context) bool { return true }),
	}

	ga := &GrammarAltSpec{
		S: "#OB #CL",
		B: "@bf",
		P: "@pf",
		R: "@rf",
		A: "@a",
		E: "@e",
		H: "@h",
		C: "@c",
		N: map[string]int{"x": 1},
		U: map[string]any{"u": 1},
		K: map[string]any{"k": 1},
		G: "mytag",
	}
	alt, err := j.resolveGrammarAlt(ga, ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(alt.S, [][]Tin{{TinOB}, {TinCL}}) {
		t.Errorf("S resolution failed: %v", alt.S)
	}
	if alt.BF == nil || alt.PF == nil || alt.RF == nil {
		t.Error("BF/PF/RF should be set from FuncRefs")
	}
	if alt.A == nil || alt.E == nil || alt.H == nil || alt.C == nil {
		t.Error("A/E/H/C should be set from FuncRefs")
	}
	if alt.N["x"] != 1 || alt.U["u"] != 1 || alt.K["k"] != 1 {
		t.Error("N/U/K maps should be copied")
	}
	if alt.G != "mytag" {
		t.Errorf("G should be copied, got %q", alt.G)
	}
}

func TestResolveGrammarAltBacktrackForms(t *testing.T) {
	j := Make()
	// int form.
	alt, err := j.resolveGrammarAlt(&GrammarAltSpec{B: 2}, nil)
	if err != nil || alt.B != 2 {
		t.Errorf("int B failed: %v %v", alt, err)
	}
	// float64 form (from parsed text).
	alt, err = j.resolveGrammarAlt(&GrammarAltSpec{B: float64(3)}, nil)
	if err != nil || alt.B != 3 {
		t.Errorf("float64 B failed: %v %v", alt, err)
	}
	// Plain rule names for P / R.
	alt, err = j.resolveGrammarAlt(&GrammarAltSpec{P: "map", R: "list"}, nil)
	if err != nil || alt.P != "map" || alt.R != "list" {
		t.Errorf("plain P/R failed: %v %v", alt, err)
	}
}

// Each FuncRef field rejects refs of the wrong type with a specific error.
func TestResolveGrammarAltWrongTypeErrors(t *testing.T) {
	j := Make()
	ref := map[FuncRef]any{"@x": "not-a-func"}

	tests := []struct {
		ga   *GrammarAltSpec
		want string
	}{
		{&GrammarAltSpec{B: "@x"}, "not a backtrack function"},
		{&GrammarAltSpec{P: "@x"}, "not a push function"},
		{&GrammarAltSpec{R: "@x"}, "not a replace function"},
		{&GrammarAltSpec{A: "@x"}, "not an AltAction"},
		{&GrammarAltSpec{E: "@x"}, "not an AltError"},
		{&GrammarAltSpec{H: "@x"}, "not an AltModifier"},
		{&GrammarAltSpec{C: "@x"}, "not an AltCond"},
	}
	for _, tt := range tests {
		_, err := j.resolveGrammarAlt(tt.ga, ref)
		if err == nil {
			t.Errorf("expected error %q, got nil", tt.want)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("expected error containing %q, got: %s", tt.want, err)
		}
	}
}

// Each FuncRef field reports missing references.
func TestResolveGrammarAltMissingRefErrors(t *testing.T) {
	j := Make()
	tests := []*GrammarAltSpec{
		{B: "@missing"},
		{P: "@missing"},
		{R: "@missing"},
		{A: "@missing"},
		{E: "@missing"},
		{H: "@missing"},
		{C: "@missing"},
	}
	for i, ga := range tests {
		if _, err := j.resolveGrammarAlt(ga, nil); err == nil {
			t.Errorf("case %d: expected missing ref error", i)
		}
	}
}

func TestResolveGrammarAltDeclarativeCondition(t *testing.T) {
	j := Make()
	alt, err := j.resolveGrammarAlt(&GrammarAltSpec{
		C: map[string]any{"d": CEq(0)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// NormAlt converts CD → C.
	if alt.C == nil {
		t.Fatal("expected declarative condition converted to C")
	}
	if !alt.C(&Rule{D: 0}, nil) || alt.C(&Rule{D: 1}, nil) {
		t.Error("declarative $eq condition behavior wrong")
	}
}

// --- resolveTokenField: string, []string, and unsupported forms ---

func TestResolveTokenFieldForms(t *testing.T) {
	j := Make()
	// Empty string → nil.
	if got := j.resolveTokenField(""); got != nil {
		t.Errorf("empty string should resolve to nil, got %v", got)
	}
	// String form: each space-separated name is a slot.
	got := j.resolveTokenField("#OB #CL")
	if !reflect.DeepEqual(got, [][]Tin{{TinOB}, {TinCL}}) {
		t.Errorf("string form failed: %v", got)
	}
	// []string form: alternatives within a slot.
	got = j.resolveTokenField([]string{"#CB #CS", "#OB"})
	if !reflect.DeepEqual(got, [][]Tin{{TinCB, TinCS}, {TinOB}}) {
		t.Errorf("[]string form failed: %v", got)
	}
	// Token set names expand within slots.
	got = j.resolveTokenField("#VAL")
	if len(got) != 1 || len(got[0]) != 4 {
		t.Errorf("token set expansion failed: %v", got)
	}
	// Unsupported type → nil.
	if got := j.resolveTokenField(42); got != nil {
		t.Errorf("unsupported type should resolve to nil, got %v", got)
	}
}

// --- ResolveGrammarAltStatic: built-in token resolution path ---

func TestResolveGrammarAltStaticAllFields(t *testing.T) {
	ref := map[FuncRef]any{
		"@bf": func(r *Rule, ctx *Context) int { return 1 },
		"@pf": func(r *Rule, ctx *Context) string { return "map" },
		"@rf": func(r *Rule, ctx *Context) string { return "list" },
		"@a":  AltAction(func(r *Rule, ctx *Context) {}),
		"@e":  AltError(func(r *Rule, ctx *Context) *Token { return nil }),
		"@h":  AltModifier(func(alt *AltSpec, r *Rule, ctx *Context) *AltSpec { return alt }),
		"@c":  AltCond(func(r *Rule, ctx *Context) bool { return true }),
	}
	ga := &GrammarAltSpec{
		S: "#OB",
		B: "@bf",
		P: "@pf",
		R: "@rf",
		A: "@a",
		E: "@e",
		H: "@h",
		C: "@c",
		N: map[string]int{"n": 1},
		U: map[string]any{"u": 1},
		K: map[string]any{"k": 1},
		G: "tag",
	}
	alt := ResolveGrammarAltStatic(ga, ref)
	if !reflect.DeepEqual(alt.S, [][]Tin{{TinOB}}) {
		t.Errorf("static S failed: %v", alt.S)
	}
	if alt.BF == nil || alt.PF == nil || alt.RF == nil ||
		alt.A == nil || alt.E == nil || alt.H == nil || alt.C == nil {
		t.Error("static funcref fields should resolve")
	}
	if alt.N == nil || alt.U == nil || alt.K == nil || alt.G != "tag" {
		t.Error("static simple fields should copy")
	}
}

func TestResolveGrammarAltStaticVariants(t *testing.T) {
	// int / float64 backtrack and plain P / R names.
	alt := ResolveGrammarAltStatic(&GrammarAltSpec{B: 1, P: "map", R: "list"}, nil)
	if alt.B != 1 || alt.P != "map" || alt.R != "list" {
		t.Errorf("static int/plain failed: %+v", alt)
	}
	alt = ResolveGrammarAltStatic(&GrammarAltSpec{B: float64(2)}, nil)
	if alt.B != 2 {
		t.Errorf("static float64 B failed: %d", alt.B)
	}
	// Missing refs are ignored (best-effort).
	alt = ResolveGrammarAltStatic(&GrammarAltSpec{
		B: "@nope", P: "@nope", R: "@nope",
	}, nil)
	if alt.BF != nil || alt.PF != nil || alt.RF != nil {
		t.Error("missing static refs should leave fields nil")
	}
	// Wrong-typed B/P/R refs are also ignored (no panic).
	ref := map[FuncRef]any{"@x": 42}
	alt = ResolveGrammarAltStatic(&GrammarAltSpec{B: "@x", P: "@x", R: "@x"}, ref)
	if alt.BF != nil || alt.PF != nil || alt.RF != nil {
		t.Error("wrong-typed static refs should leave fields nil")
	}
	// Declarative condition map → CD.
	alt = ResolveGrammarAltStatic(&GrammarAltSpec{C: map[string]any{"d": 0}}, nil)
	if alt.C == nil {
		t.Error("static declarative condition should normalize to C")
	}
}

func TestResolveTokenFieldStaticForms(t *testing.T) {
	if got := resolveTokenFieldStatic(""); got != nil {
		t.Errorf("empty string → nil, got %v", got)
	}
	if got := resolveTokenFieldStatic(42); got != nil {
		t.Errorf("unsupported type → nil, got %v", got)
	}
	// Unknown token name in static context → empty slice (no panic).
	got := resolveTokenNameStatic("#NOPE")
	if got != nil {
		t.Errorf("unknown static token → nil, got %v", got)
	}
}

// --- mapToGrammarAltSpec: parsed map → GrammarAltSpec ---

func TestMapToGrammarAltSpecAllKeys(t *testing.T) {
	m := map[string]any{
		"s": "#OB",
		"b": float64(1),
		"p": "map",
		"r": "list",
		"a": "@act",
		"e": "@err",
		"h": "@mod",
		"c": "@cond",
		"n": map[string]any{"x": float64(2), "bad": "skip"},
		"u": map[string]any{"u": 1},
		"k": map[string]any{"k": 1},
		"g": "tag",
	}
	alt := mapToGrammarAltSpec(m)
	if alt.S != "#OB" || alt.B != float64(1) || alt.P != "map" || alt.R != "list" {
		t.Errorf("s/b/p/r failed: %+v", alt)
	}
	if alt.A != "@act" || alt.E != "@err" || alt.H != "@mod" || alt.C != "@cond" {
		t.Errorf("a/e/h/c failed: %+v", alt)
	}
	if alt.N["x"] != 2 {
		t.Errorf("n failed: %v", alt.N)
	}
	if _, ok := alt.N["bad"]; ok {
		t.Error("non-float n entries should be skipped")
	}
	if alt.U == nil || alt.K == nil || alt.G != "tag" {
		t.Errorf("u/k/g failed: %+v", alt)
	}
}

// --- parseGrammarAltsOrSpec: array form, map form with inject, bad forms ---

func TestParseGrammarAltsOrSpecForms(t *testing.T) {
	// Plain array form (non-map entries skipped).
	out := parseGrammarAltsOrSpec([]any{
		map[string]any{"g": "one"},
		"not-a-map",
	})
	alts, ok := out.([]*GrammarAltSpec)
	if !ok || len(alts) != 1 || alts[0].G != "one" {
		t.Errorf("array form failed: %v", out)
	}

	// Map form with alts + inject.
	out = parseGrammarAltsOrSpec(map[string]any{
		"alts": []any{map[string]any{"g": "two"}},
		"inject": map[string]any{
			"append": true,
			"delete": []any{float64(0), "skip"},
			"move":   []any{float64(1), float64(0)},
		},
	})
	spec, ok := out.(*GrammarAltListSpec)
	if !ok {
		t.Fatalf("expected *GrammarAltListSpec, got %T", out)
	}
	if len(spec.Alts) != 1 || spec.Alts[0].G != "two" {
		t.Errorf("alts failed: %v", spec.Alts)
	}
	if spec.Inject == nil || !spec.Inject.Append {
		t.Error("inject.append failed")
	}
	if !reflect.DeepEqual(spec.Inject.Delete, []int{0}) {
		t.Errorf("inject.delete failed: %v", spec.Inject.Delete)
	}
	if !reflect.DeepEqual(spec.Inject.Move, []int{1, 0}) {
		t.Errorf("inject.move failed: %v", spec.Inject.Move)
	}

	// Map form without alts → nil.
	if out := parseGrammarAltsOrSpec(map[string]any{"inject": map[string]any{}}); out != nil {
		t.Errorf("map without alts → nil, got %v", out)
	}
	// alts not an array → nil.
	if out := parseGrammarAltsOrSpec(map[string]any{"alts": "x"}); out != nil {
		t.Errorf("non-array alts → nil, got %v", out)
	}
	// Unsupported type → nil.
	if out := parseGrammarAltsOrSpec("zzz"); out != nil {
		t.Errorf("unsupported form → nil, got %v", out)
	}
}

// --- GrammarText: text-form rules exercising more alt fields ---

func TestGrammarTextCommentOnly(t *testing.T) {
	// Comment-only text parses to nil → no-op.
	withStubTextParser(t, func(string) (any, error) { return nil, nil })
	j := Make()
	if err := j.GrammarText("# nothing here"); err != nil {
		t.Errorf("comment-only grammar text should be a no-op: %v", err)
	}
}

// --- GrammarSetting alt.g tag merging ([]string form + mergeG) ---

func TestGrammarSettingAltGSlice(t *testing.T) {
	j := Make()
	err := j.Grammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{
			"zzz": {
				Open: []*GrammarAltSpec{{G: "base"}},
			},
		},
	}, &GrammarSetting{Rule: &GrammarSettingRule{Alt: &GrammarSettingAlt{
		G: []string{" extra ", ""},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	open := j.RSM()["zzz"].OpenAlts()
	if open[0].G != "base,extra" {
		t.Errorf("expected base,extra, got %q", open[0].G)
	}
}

func TestGrammarSettingNilEntries(t *testing.T) {
	// nil settings and settings without alt.g are skipped.
	j := Make()
	err := j.Grammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{
			"zzz": {Open: []*GrammarAltSpec{{G: "solo"}}},
		},
	}, nil, &GrammarSetting{}, &GrammarSetting{Rule: &GrammarSettingRule{}})
	if err != nil {
		t.Fatal(err)
	}
	if j.RSM()["zzz"].OpenAlts()[0].G != "solo" {
		t.Error("tags should be unchanged when no setting alt.g supplied")
	}
}

func TestMergeGEmptyExtra(t *testing.T) {
	if got := mergeG("aa,bb", nil); got != "aa,bb" {
		t.Errorf("empty extra should return existing, got %q", got)
	}
	if got := mergeG("", []string{"xx"}); got != "xx" {
		t.Errorf("empty existing should return extra, got %q", got)
	}
}

func TestSplitGroupTags(t *testing.T) {
	if got := splitGroupTags(""); got != nil {
		t.Errorf("empty → nil, got %v", got)
	}
	got := splitGroupTags(" aa , , bb ")
	if !reflect.DeepEqual(got, []string{"aa", "bb"}) {
		t.Errorf("expected [aa bb], got %v", got)
	}
}

// --- applyGrammarAlts: tag merging and unsupported spec types ---
// Note: a nil *GrammarAltSpec entry is not exercised — the nil-copy branch
// in applyGrammarAlts would panic downstream in resolveGrammarAlt, so it is
// unreachable from any successful public path.

func TestApplyGrammarAltsTagMerge(t *testing.T) {
	j := Make()
	rs := &RuleSpec{Name: "tmp"}
	err := applyGrammarAlts(j, rs, []*GrammarAltSpec{{G: "gg"}}, nil, true, []string{"tt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.OpenAlts()) != 1 {
		t.Fatalf("expected 1 alt, got %d", len(rs.OpenAlts()))
	}
	if rs.OpenAlts()[0].G != "gg,tt" {
		t.Errorf("expected gg,tt, got %q", rs.OpenAlts()[0].G)
	}
}

func TestApplyGrammarAltsUnsupportedSpec(t *testing.T) {
	j := Make()
	rs := &RuleSpec{Name: "tmp"}
	if err := applyGrammarAlts(j, rs, 42, nil, true, nil); err != nil {
		t.Errorf("unsupported spec type should be a no-op: %v", err)
	}
	if len(rs.OpenAlts()) != 0 {
		t.Error("unsupported spec type should not add alts")
	}
}
