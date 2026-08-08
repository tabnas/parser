// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Options.TokenSet: Make and SetOptions must be equivalent
// ---------------------------------------------------------------------------

// tinSet renders a []Tin as a comparable, order-insensitive key.
func tinSet(tins []Tin) map[Tin]bool {
	out := map[Tin]bool{}
	for _, t := range tins {
		out[t] = true
	}
	return out
}

// Make(opts) used to build the config but never apply Options.TokenSet, so a
// declarative token-set override silently did nothing while the identical
// SetOptions(opts) worked. The two construction paths must agree.
func TestOptionsTokenSetMakeEqualsSetOptions(t *testing.T) {
	opts := Options{TokenSet: map[string][]string{
		"KEY":   {"#ST"},
		"VAL":   {"#ST", "#NR"},
		"MYSET": {"#TX", "", "#NR"}, // empty names are dropped (TS `null` filter)
	}}

	made := Make(opts)
	set := Make().SetOptions(opts)

	for _, name := range []string{"KEY", "VAL", "MYSET"} {
		got, want := made.TokenSet(name), set.TokenSet(name)
		if !reflect.DeepEqual(tinSet(got), tinSet(want)) {
			t.Errorf("TokenSet(%q): Make gave %v, SetOptions gave %v", name, got, want)
		}
	}

	if got := tinSet(made.TokenSet("KEY")); !reflect.DeepEqual(got, tinSet([]Tin{TinST})) {
		t.Errorf(`Make(tokenSet KEY=[#ST]).TokenSet("KEY") = %v, want [#ST]`,
			made.TokenSet("KEY"))
	}
	if got := tinSet(made.TokenSet("MYSET")); !reflect.DeepEqual(got, tinSet([]Tin{TinTX, TinNR})) {
		t.Errorf(`empty names should be skipped, got %v`, made.TokenSet("MYSET"))
	}

	// The config sets the lexer and grammar read must track, not just the
	// customTokenSets map that TokenSet() consults first.
	if got := tinSet(made.Config().KeySet); !reflect.DeepEqual(got, tinSet([]Tin{TinST})) {
		t.Errorf("Make did not update Config().KeySet: %v", made.Config().KeySet)
	}
}

// A token set applied at Make() must survive Derive.
func TestOptionsTokenSetSurvivesDerive(t *testing.T) {
	j := Make(Options{TokenSet: map[string][]string{"KEY": {"#ST"}}})
	child, err := j.Derive()
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if got := tinSet(child.TokenSet("KEY")); !reflect.DeepEqual(got, tinSet([]Tin{TinST})) {
		t.Errorf("derived TokenSet(KEY) = %v, want [#ST]", child.TokenSet("KEY"))
	}
}

// ---------------------------------------------------------------------------
// A custom KEY set must actually change parsing (defects 1 + 2 together)
// ---------------------------------------------------------------------------

// jsonOptionsNoKeySet is jsonOptions with the KEY restriction removed: the
// control case, where the engine's default KEY set (#TX #NR #ST #VL) applies.
func jsonOptionsNoKeySet() Options {
	o := jsonOptions()
	o.TokenSet = nil
	return o
}

// End-to-end pair check for defect (1)+(2): jsonOptions declares
// tokenSet KEY = [#ST], and the strict-JSON grammar's pair rule opens on
// "#KEY #CL". A number-keyed object is therefore admitted by the default KEY
// set and rejected by the custom one — so it distinguishes "the override was
// applied" from "the override was silently dropped".
func TestCustomKeyTokenSetRestrictsParsing(t *testing.T) {
	// Control: without the override, a number key parses.
	loose := Make(jsonOptionsNoKeySet())
	if err := registerJSONGrammar(loose); err != nil {
		t.Fatalf("registerJSONGrammar: %v", err)
	}
	if out, err := loose.Parse(`{1:2}`); err != nil {
		t.Fatalf("default KEY set should admit a number key, got error: %v", err)
	} else if out == nil {
		t.Fatal("default KEY set: expected a parsed map")
	}

	// With tokenSet KEY = [#ST] declared through Options at Make() time, the
	// same input must be rejected, and quoted keys must still work.
	strict := makeJSON()
	if _, err := strict.Parse(`{1:2}`); err == nil {
		t.Error(`tokenSet KEY=[#ST] must reject the number key in {1:2}`)
	}
	out, err := strict.Parse(`{"a":1}`)
	if err != nil {
		t.Fatalf(`{"a":1} should still parse: %v`, err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["a"] == nil {
		t.Errorf(`{"a":1} = %#v, want map with key "a"`, out)
	}
}

// Same end-to-end check, but for alternates resolved WITHOUT an instance —
// the ResolveGrammarAltStatic path grammar packages use when they build rule
// alternates by hand. Static resolution can only see the package-level token
// sets, so the alt's tins are frozen to the default KEY; the declared names it
// records on AltSpec.SNames are what let the instance's override still apply.
func TestStaticAltHonoursInstanceTokenSet(t *testing.T) {
	j := makeJSON() // Options declare tokenSet KEY = [#ST]

	// Overwrite the pair-open alt's token sequence with the statically
	// resolved form, exactly as a hand-built grammar would install it.
	static := ResolveGrammarAltStatic(&GrammarAltSpec{S: "#KEY #CL"}, nil)
	if len(static.S) != 2 {
		t.Fatalf("static resolve of %q gave %d positions, want 2", "#KEY #CL", len(static.S))
	}
	if !tinSet(static.S[0])[TinNR] {
		t.Fatal("precondition: static #KEY should resolve to the package-level default set")
	}

	pair := j.RSM()["pair"]
	if pair == nil {
		t.Fatal("pair rule missing")
	}
	patched := false
	for _, alt := range pair.OpenAlts() {
		if len(alt.S) == 2 && tinSet(alt.S[1])[TinCL] {
			alt.S = static.S
			alt.SNames = static.SNames
			patched = true
		}
	}
	if !patched {
		t.Fatal("did not find the pair-open alt to patch")
	}

	if _, err := j.Parse(`{1:2}`); err == nil {
		t.Error("statically resolved #KEY must still honour the instance's tokenSet override")
	}
	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Errorf(`{"a":1} should still parse: %v`, err)
	}
}

// A statically resolved alt on an instance with no token-set override must
// behave exactly as before: the late-binding path is opt-in on custom sets.
func TestStaticAltUnchangedWithoutOverride(t *testing.T) {
	j := Make(jsonOptionsNoKeySet())
	if err := registerJSONGrammar(j); err != nil {
		t.Fatalf("registerJSONGrammar: %v", err)
	}
	static := ResolveGrammarAltStatic(&GrammarAltSpec{S: "#KEY #CL"}, nil)
	for _, alt := range j.RSM()["pair"].OpenAlts() {
		if len(alt.S) == 2 && tinSet(alt.S[1])[TinCL] {
			alt.S, alt.SNames = static.S, static.SNames
		}
	}
	if _, err := j.Parse(`{1:2}`); err != nil {
		t.Errorf("no override: number key should still parse, got %v", err)
	}
}

// The instance-aware resolver is the recommended replacement for
// ResolveGrammarAltStatic where a *Tabnas is in hand: it resolves the set
// against the instance up front rather than relying on late binding.
func TestResolveGrammarAltUsesInstanceTokenSet(t *testing.T) {
	j := Make(Options{TokenSet: map[string][]string{"KEY": {"#ST"}}})

	alt, err := j.ResolveGrammarAlt(&GrammarAltSpec{S: "#KEY #CL"}, nil)
	if err != nil {
		t.Fatalf("ResolveGrammarAlt: %v", err)
	}
	if got := tinSet(alt.S[0]); !reflect.DeepEqual(got, tinSet([]Tin{TinST})) {
		t.Errorf("instance-aware #KEY = %v, want [#ST]", alt.S[0])
	}

	alts, err := j.ResolveGrammarAlts([]*GrammarAltSpec{{S: "#KEY"}, {S: "#VAL"}}, nil)
	if err != nil {
		t.Fatalf("ResolveGrammarAlts: %v", err)
	}
	if len(alts) != 2 {
		t.Fatalf("ResolveGrammarAlts gave %d alts, want 2", len(alts))
	}
	if got := tinSet(alts[0].S[0]); !reflect.DeepEqual(got, tinSet([]Tin{TinST})) {
		t.Errorf("instance-aware #KEY = %v, want [#ST]", alts[0].S[0])
	}
}

// ---------------------------------------------------------------------------
// Rule declaration order
// ---------------------------------------------------------------------------

// Rules registered imperatively report their definition order, which is NOT
// the alphabetical order an RSM() map walk yields.
func TestRuleNamesFollowDeclarationOrder(t *testing.T) {
	j := Make()
	for _, name := range []string{"zebra", "alpha", "mid"} {
		j.Rule(name, func(rs *RuleSpec, _ *Parser) {})
	}

	want := []string{"zebra", "alpha", "mid"}
	if got := j.RuleNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("RuleNames() = %v, want %v", got, want)
	}

	rules := j.Rules()
	if len(rules) != 3 {
		t.Fatalf("Rules() gave %d specs, want 3", len(rules))
	}
	for i := 1; i < len(rules); i++ {
		if rules[i-1].Def >= rules[i].Def {
			t.Errorf("Def not increasing: %s=%d then %s=%d",
				rules[i-1].Name, rules[i-1].Def, rules[i].Name, rules[i].Def)
		}
	}

	// Re-registering an existing rule must not renumber it.
	before := j.RSM()["zebra"].Def
	j.Rule("zebra", func(rs *RuleSpec, _ *Parser) {})
	if j.RSM()["zebra"].Def != before {
		t.Errorf("redefining a rule changed its Def: %d -> %d", before, j.RSM()["zebra"].Def)
	}
	if got := j.RuleNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("after redefinition RuleNames() = %v, want %v", got, want)
	}
}

// Specs built as bare struct literals carry no order; they must not be
// interleaved arbitrarily with stamped ones — they sort last, by name.
func TestRuleNamesUnstampedSpecsSortLast(t *testing.T) {
	j := Make()
	j.Rule("zebra", func(rs *RuleSpec, _ *Parser) {})
	j.RSM()["bare"] = &RuleSpec{Name: "bare"}
	j.RSM()["also"] = &RuleSpec{Name: "also"}

	want := []string{"zebra", "also", "bare"}
	if got := j.RuleNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("RuleNames() = %v, want %v", got, want)
	}
}

// A declarative GrammarSpec's Rule map has no order, so RuleOrder declares it.
func TestGrammarRuleOrder(t *testing.T) {
	j := Make()
	err := j.Grammar(&GrammarSpec{
		RuleOrder: []string{"zebra", "alpha", "mid"},
		Rule: map[string]*GrammarRuleSpec{
			"alpha": {Open: []*GrammarAltSpec{{S: "#ST"}}},
			"mid":   {Open: []*GrammarAltSpec{{S: "#ST"}}},
			"zebra": {Open: []*GrammarAltSpec{{S: "#ST"}}},
		},
	})
	if err != nil {
		t.Fatalf("Grammar: %v", err)
	}
	want := []string{"zebra", "alpha", "mid"}
	if got := j.RuleNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("RuleNames() = %v, want %v", got, want)
	}
}

// Without RuleOrder the engine must still be deterministic — Go map iteration
// is randomised, so an unordered spec has to fall back to sorted names.
func TestGrammarRuleOrderDefaultsToSorted(t *testing.T) {
	var first []string
	for i := 0; i < 20; i++ {
		j := Make()
		if err := j.Grammar(&GrammarSpec{
			Rule: map[string]*GrammarRuleSpec{
				"alpha": {Open: []*GrammarAltSpec{{S: "#ST"}}},
				"mid":   {Open: []*GrammarAltSpec{{S: "#ST"}}},
				"zebra": {Open: []*GrammarAltSpec{{S: "#ST"}}},
			},
		}); err != nil {
			t.Fatalf("Grammar: %v", err)
		}
		got := j.RuleNames()
		if first == nil {
			first = got
		} else if !reflect.DeepEqual(got, first) {
			t.Fatalf("rule order not deterministic: %v then %v", first, got)
		}
	}
	if want := []string{"alpha", "mid", "zebra"}; !reflect.DeepEqual(first, want) {
		t.Errorf("unordered spec = %v, want sorted %v", first, want)
	}
}

// RuleOrder naming rules not present in the map is harmless; rules missing
// from RuleOrder follow the listed ones, sorted.
func TestGrammarRuleOrderPartial(t *testing.T) {
	j := Make()
	err := j.Grammar(&GrammarSpec{
		RuleOrder: []string{"zebra", "ghost", "zebra"},
		Rule: map[string]*GrammarRuleSpec{
			"alpha": {Open: []*GrammarAltSpec{{S: "#ST"}}},
			"mid":   {Open: []*GrammarAltSpec{{S: "#ST"}}},
			"zebra": {Open: []*GrammarAltSpec{{S: "#ST"}}},
		},
	})
	if err != nil {
		t.Fatalf("Grammar: %v", err)
	}
	want := []string{"zebra", "alpha", "mid"}
	if got := j.RuleNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("RuleNames() = %v, want %v", got, want)
	}
}

// GrammarText recovers declaration order from the source text's key order,
// so a text grammar needs no explicit RuleOrder.
func TestGrammarTextPreservesDeclarationOrder(t *testing.T) {
	// Stub parser returning the ordered node a real grammar package would.
	withStubTextParser(t, func(src string) (any, error) {
		names := strings.Fields(src)
		rules := NewOrderedMap()
		for _, n := range names {
			rule := NewOrderedMap()
			rule.Set("open", []any{map[string]any{"s": "#ST"}})
			rules.Set(n, rule)
		}
		top := NewOrderedMap()
		top.Set("rule", rules)
		return top, nil
	})

	j := Make()
	if err := j.GrammarText("zebra alpha mid"); err != nil {
		t.Fatalf("GrammarText: %v", err)
	}
	want := []string{"zebra", "alpha", "mid"}
	if got := j.RuleNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("RuleNames() = %v, want %v", got, want)
	}
}

// The lexer only produces a custom match.token when that token is expected
// at the current rule position. That gate reads the alt's token slots, so it
// has to see the same tokenSet override the parser does — otherwise a token
// the override just added to #KEY / #VAL is never lexed, and parsing fails on
// the very input the override was meant to admit (the TS jsonic
// "options-regex-match-token" case, which broke in Go through statically
// resolved alts).
func TestMatchTokenGateHonoursTokenSetOverride(t *testing.T) {
	f := false
	// lexIdent lexes "a:1" against a rule whose open alt was resolved WITHOUT
	// an instance — #KEY frozen to the package-level default set, which has no
	// #ID — and returns the name of the first token.
	lexIdent := func(t *testing.T, tokenSet map[string][]string) string {
		t.Helper()
		j := Make(Options{
			// Text lexing off, so "a" is either #ID or nothing.
			Text: &TextOptions{Lex: &f},
			Match: &MatchOptions{Token: map[string]*regexp.Regexp{
				"#ID": regexp.MustCompile(`^[a-zA-Z_][a-zA-Z_0-9]*`),
			}},
			TokenSet: tokenSet,
		})
		spec := MakeRuleSpec("val")
		spec.AddOpen(ResolveGrammarAltStatic(&GrammarAltSpec{S: "#KEY #CL"}, nil))
		j.RSM()["val"] = spec

		if _, err := j.Parse("a:1"); err == nil {
			// The stub grammar cannot complete a parse; only the lexing
			// decision is under test, and Parse is how the engine wires the
			// Context the gate reads.
			t.Fatal("stub grammar unexpectedly completed a parse")
		}

		lex := NewLex("a:1", j.Config())
		ctx := &Context{Inst: j, Cfg: j.Config(), RSM: j.RSM(), T: []*Token{NoToken}}
		ctx.tokenSetDyn = len(j.customTokenSets) > 0
		lex.Ctx = ctx
		return lex.Next(MakeRule(spec, ctx, nil)).Name
	}

	// Control: no override, so #ID is in no set the alt names and the gate
	// correctly refuses to produce it.
	if got := lexIdent(t, nil); got == "#ID" {
		t.Error("without an override #ID is in no token set and must not be produced")
	}

	// With #ID added to KEY, the alt's #KEY position expects it — even though
	// the alt itself was resolved against the default set.
	if got := lexIdent(t, map[string][]string{"KEY": {"#ST", "#ID"}}); got != "#ID" {
		t.Errorf("first token = %s, want #ID (tokenSet KEY override adds it)", got)
	}
}

// A position that names a token set alongside a name this instance does not
// know must be left exactly as resolved: re-resolving it would have to mint a
// token during the parse.
func TestUnknownNameLeavesSlotAlone(t *testing.T) {
	j := Make(Options{TokenSet: map[string][]string{"KEY": {"#ST"}}})
	if err := registerJSONGrammar(j); err != nil {
		t.Fatalf("registerJSONGrammar: %v", err)
	}
	alt := ResolveGrammarAltStatic(&GrammarAltSpec{S: []string{"#KEY #NOPE", "#CL"}}, nil)
	frozen := append([][]Tin{}, alt.S...)
	for _, a := range j.RSM()["pair"].OpenAlts() {
		if len(a.S) == 2 && tinSet(a.S[1])[TinCL] {
			a.S, a.SNames = alt.S, alt.SNames
		}
	}

	knownTokens := len(j.tinByName)
	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Fatalf(`{"a":1}: %v`, err)
	}
	if got := len(j.tinByName); got != knownTokens {
		t.Errorf("parse minted %d new token(s) for an unknown name", got-knownTokens)
	}
	if !reflect.DeepEqual(alt.S, frozen) {
		t.Errorf("alt.S mutated: %v -> %v", frozen, alt.S)
	}
}
