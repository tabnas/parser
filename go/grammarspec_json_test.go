// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// GrammarSpecFromJSON is the cross-runtime door: every non-Go binding
// reaches the engine through it. A field it drops or coerces is not a
// cosmetic loss — it is this runtime accepting a language the canonical
// TypeScript runtime does not. These tests pin the fields where that
// divergence is possible.

import (
	"strings"
	"testing"
)

// `clear` wipes rules and fixed-token bindings before the rest of the
// spec applies. Dropping it leaves the engine's default bindings in
// place, which changes how the grammar tokenises — a silently different
// language, the one thing the house rule forbids.
func TestFromJSONPreservesClear(t *testing.T) {
	for _, c := range []struct {
		spec string
		want bool
	}{
		{`{"clear":true}`, true},
		{`{"clear":false}`, false},
		{`{}`, false},
		// TS compares against literal true (`true === gs.clear`), so a
		// non-boolean is not a clear request. Matching that exactly
		// matters more than being lenient here.
		{`{"clear":"yes"}`, false},
		{`{"clear":1}`, false},
	} {
		gs, err := GrammarSpecFromJSON([]byte(c.spec))
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		if gs.Clear != c.want {
			t.Errorf("%s: Clear = %v, want %v", c.spec, gs.Clear, c.want)
		}
	}
}

// A malformed version must be refused, not coerced. "999" silently
// becoming 0 disables the version gate altogether — the spec then loads
// on an engine that may not implement its schema at all.
func TestFromJSONRejectsMalformedVersion(t *testing.T) {
	for _, spec := range []string{
		`{"v":"999"}`, // not a number: would coerce to 0, gate skipped
		`{"v":2.5}`,   // not an integer: would truncate to a different schema
		`{"v":0}`,     // present but not positive; TS rejects
		`{"v":-1}`,
		`{"v":null}`,
		`{"v":true}`,
	} {
		gs, err := GrammarSpecFromJSON([]byte(spec))
		if err == nil {
			t.Errorf("%s: accepted, decoded V=%d; want a refusal", spec, gs.V)
			continue
		}
		if !strings.Contains(err.Error(), "invalid builtin schema version") {
			t.Errorf("%s: unhelpful error %q", spec, err)
		}
	}
}

// `meta` is engine-ignored tool metadata (schema/grammar.schema.json):
// the JSON door must preserve it on the spec — the C ABI and bindings
// load specs through here, and dropping it would silently strip a
// BNF-family compiler's provenance map — while a spec carrying it still
// parses exactly as one without it. Mirrored by
// ts/test/serialized-grammar.test.js meta-is-engine-ignored-passthrough.
func TestFromJSONPreservesMeta(t *testing.T) {
	spec := `{"clear":true,"options":{"rule":{"start":"top"}},` +
		`"meta":{"provenance":{"top$step1":"top"}},` +
		`"rule":{"top":{"open":[{"s":"#NR"}]}}}`
	gs, err := GrammarSpecFromJSON([]byte(spec))
	if err != nil {
		t.Fatalf("GrammarSpecFromJSON: %v", err)
	}
	prov, ok := gs.Meta["provenance"].(map[string]any)
	if !ok || prov["top$step1"] != "top" {
		t.Fatalf("Meta not preserved: %#v", gs.Meta)
	}
	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatalf("Grammar: %v", err)
	}
	expectVerdicts(t, j, []string{"42"}, []string{`"s"`})

	// A non-object meta is not an error at the loader (the schema is
	// where shape is enforced); it is simply not preserved.
	gs2, err := GrammarSpecFromJSON([]byte(`{"meta":"nope"}`))
	if err != nil || gs2.Meta != nil {
		t.Fatalf("non-object meta: err=%v Meta=%#v", err, gs2.Meta)
	}
}

// The text-form door builds and applies the spec internally (nothing to
// preserve for a caller), but a grammar text carrying meta must still
// load and parse exactly as one without it.
func TestGrammarTextToleratesMeta(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) {
		return map[string]any{
			"options": map[string]any{"rule": map[string]any{"start": "top"}},
			"meta":    map[string]any{"provenance": map[string]any{"top$step1": "top"}},
			"rule": map[string]any{
				"top": map[string]any{"open": []any{map[string]any{"s": "#NR"}}},
			},
		}, nil
	})
	j := Make()
	if err := j.GrammarText("(stubbed)"); err != nil {
		t.Fatalf("GrammarText: %v", err)
	}
	expectVerdicts(t, j, []string{"42"}, []string{`"s"`})
}

// loadJSONGrammar builds a default instance and applies a serialized
// spec through the JSON door, failing the test on any load error.
func loadJSONGrammar(t *testing.T, spec string) *Tabnas {
	t.Helper()
	gs, err := GrammarSpecFromJSON([]byte(spec))
	if err != nil {
		t.Fatalf("GrammarSpecFromJSON: %v (%s)", err, spec)
	}
	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatalf("Grammar: %v (%s)", err, spec)
	}
	return j
}

// expectVerdicts asserts accept/reject per input, naming the failing case.
func expectVerdicts(t *testing.T, j *Tabnas, accept []string, reject []string) {
	t.Helper()
	for _, src := range accept {
		if _, err := j.Parse(src); err != nil {
			t.Errorf("%q should be accepted, got: %v", src, err)
		}
	}
	for _, src := range reject {
		if _, err := j.Parse(src); err == nil {
			t.Errorf("%q should be rejected", src)
		}
	}
}

// A JSON null rule entry REMOVES the rule, exactly as the struct form
// (Rule[name] = nil) and TS (`{"rule":{"other":null}}` → rule(name, null))
// do. The door used to skip null entries, silently keeping the rule
// installed. Mirrored by ts/test/serialized-grammar.test.js.
func TestFromJSONRuleNullRemovesRule(t *testing.T) {
	j := loadJSONGrammar(t, `{"clear":true,`+
		`"options":{"rule":{"start":"top"}},`+
		`"rule":{`+
		`"top":{"open":[{"s":"#NR"},{"p":"other"}]},`+
		`"other":{"open":[{"s":"#ST"}]}}}`)
	expectVerdicts(t, j, []string{"42", `"s"`}, nil)

	gs, err := GrammarSpecFromJSON([]byte(`{"rule":{"other":null}}`))
	if err != nil {
		t.Fatalf("GrammarSpecFromJSON: %v", err)
	}
	if err := j.Grammar(gs); err != nil {
		t.Fatalf("Grammar: %v", err)
	}

	if _, ok := j.parser.RSM["other"]; ok {
		t.Error(`rule "other" should have been removed by the null entry`)
	}
	// The push to the now-missing rule must fail the parse, as in TS
	// (which raises unknown_rule for the same input).
	if _, err := j.Parse(`"s"`); err == nil {
		t.Error(`"s" should be rejected once rule "other" is removed`)
	}
}

// The array form of `s` (["#NR #ST"]: each element a slot, space-separated
// names alternatives within the slot) arrives from JSON as []any and was
// dropped by resolveTokenField/tokenFieldNames — the alt then matched
// nothing. Same serialized grammar must accept/reject identically in TS
// (ts/test/serialized-grammar.test.js).
func TestFromJSONArrayTokenSpec(t *testing.T) {
	j := loadJSONGrammar(t, `{"clear":true,`+
		`"options":{"rule":{"start":"top"}},`+
		`"rule":{"top":{"open":[{"s":["#NR #ST"]}]}}}`)
	expectVerdicts(t, j, []string{"42", `"s"`}, []string{"abc"})
}

// Array-form g (["custom","special"]) arrives as []any and was dropped —
// and with it rule.include/exclude filtering for serialized grammars.
// Mirrored by ts/test/serialized-grammar.test.js.
func TestFromJSONArrayGroupTagsFilterable(t *testing.T) {
	spec := `{"clear":true,` +
		`"options":{"rule":{"start":"top"}},` +
		`"rule":{"top":{"open":[` +
		`{"s":["#NR"]},` +
		`{"s":"#ST","g":["custom","special"]}]}}}`

	// Without filtering, both alts are live.
	expectVerdicts(t, loadJSONGrammar(t, spec), []string{"42", `"s"`}, nil)

	// Excluding one of the array's tags must strip the tagged alt.
	j := loadJSONGrammar(t, spec)
	j.SetOptions(Options{Rule: &RuleOptions{Exclude: "special"}})
	expectVerdicts(t, j, []string{"42"}, []string{`"s"`})
}

// inject {clear:true} empties the existing alternates before inserting —
// TS honors it from pure JSON (rules.ts add(), mods.clear), so the door
// must too. Mirrored by ts/test/serialized-grammar.test.js.
func TestFromJSONInjectClearReplacesAlts(t *testing.T) {
	j := loadJSONGrammar(t, `{"clear":true,`+
		`"options":{"rule":{"start":"top"}},`+
		`"rule":{"top":{"open":[{"s":"#NR"}]}}}`)
	expectVerdicts(t, j, []string{"42"}, []string{`"s"`})

	gs, err := GrammarSpecFromJSON([]byte(
		`{"rule":{"top":{"open":{"alts":[{"s":"#ST"}],"inject":{"clear":true}}}}}`))
	if err != nil {
		t.Fatalf("GrammarSpecFromJSON: %v", err)
	}
	if err := j.Grammar(gs); err != nil {
		t.Fatalf("Grammar: %v", err)
	}
	expectVerdicts(t, j, []string{`"s"`}, []string{"42"})
}

func TestFromJSONKeepsValidVersion(t *testing.T) {
	// Absent means "current" and must stay 0, the engine's own sentinel.
	gs, err := GrammarSpecFromJSON([]byte(`{}`))
	if err != nil || gs.V != 0 {
		t.Fatalf("absent v: V=%d err=%v, want 0 and no error", gs.V, err)
	}

	gs, err = GrammarSpecFromJSON([]byte(`{"v":1}`))
	if err != nil || gs.V != 1 {
		t.Fatalf("v:1: V=%d err=%v", gs.V, err)
	}

	// A version this engine cannot serve still decodes; refusing it is
	// Grammar()'s job, and its message is the one users already know.
	gs, err = GrammarSpecFromJSON([]byte(`{"v":999}`))
	if err != nil {
		t.Fatalf("v:999 should decode, then be refused by Grammar: %v", err)
	}
	if err := Make().Grammar(gs); err == nil ||
		!strings.Contains(err.Error(), "supports up to") {
		t.Errorf("Grammar() should refuse v:999, got %v", err)
	}
}
