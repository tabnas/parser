// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"reflect"
	"testing"
)

// A grammar spec can name a rule that does not exist. ValidateAlt cannot
// catch it — it sees one alternate and does not know the rule set — so the
// typo survived validation and surfaced only at parse time, as an
// unknown_rule error, and only once an input reached the alternate carrying
// it. ValidateGrammar is the check that has the rule map in scope. See
// tabnas/parser#113.
//
// The message wording and ordering here are the CROSS-RUNTIME contract:
// ts/test/validate-grammar.test.js asserts the identical strings.

func eqProblems(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestValidateGrammarReportsUnknownPushRule(t *testing.T) {
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"val": {Open: []*GrammarAltSpec{
			{S: "#OB", P: "mapp", B: 1, A: "@reset$"},
			{S: "#OS", P: "list"},
		}},
		"list": {Open: []*GrammarAltSpec{}},
	}}
	eqProblems(t, ValidateGrammar(gs, nil), []string{
		`val.open alt[0]: unknown rule in p: "mapp"`,
	})
}

func TestValidateGrammarReportsReplaceAndListForm(t *testing.T) {
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"a": {Close: &GrammarAltListSpec{
			Alts:   []*GrammarAltSpec{{S: "#CS", R: "nope"}},
			Inject: &GrammarInjectSpec{Append: true},
		}},
	}}
	eqProblems(t, ValidateGrammar(gs, nil), []string{
		`a.close alt[0]: unknown rule in r: "nope"`,
	})
}

func TestValidateGrammarNilEntryRemovesTheRule(t *testing.T) {
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"gone": nil,
		"a":    {Open: []*GrammarAltSpec{{P: "gone"}}},
	}}
	eqProblems(t, ValidateGrammar(gs, nil), []string{
		`a.open alt[0]: unknown rule in p: "gone"`,
	})
}

func TestValidateGrammarSkipsFuncRefsAndAbsentSlots(t *testing.T) {
	// A FuncRef yields its rule name at parse time, so no static check can
	// follow it. Go has no `p: false` form — its P is a string, and absent
	// is "".
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"a": {Open: []*GrammarAltSpec{
			{P: "@pickNext"},
			{R: "@pickOther"},
			{S: "#TX"},
		}},
	}}
	eqProblems(t, ValidateGrammar(gs, nil), nil)
}

func TestValidateGrammarKnownRulesAllowExtensionSpecs(t *testing.T) {
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"a": {Open: []*GrammarAltSpec{{P: "base"}}},
	}}
	eqProblems(t, ValidateGrammar(gs, nil), []string{
		`a.open alt[0]: unknown rule in p: "base"`,
	})
	eqProblems(t, ValidateGrammar(gs, []string{"base"}), nil)
}

func TestValidateGrammarReportsEveryDanglingReferenceSorted(t *testing.T) {
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"val":      {Open: []*GrammarAltSpec{{P: "mapp"}}},
		"list":     {Close: &GrammarAltListSpec{Alts: []*GrammarAltSpec{{R: "nope"}}}},
		"gone":     nil,
		"dangling": {Open: []*GrammarAltSpec{{P: "gone"}}},
	}}
	// Sorted, so map iteration order cannot make this flap.
	for i := 0; i < 8; i++ {
		eqProblems(t, ValidateGrammar(gs, nil), []string{
			`dangling.open alt[0]: unknown rule in p: "gone"`,
			`list.close alt[0]: unknown rule in r: "nope"`,
			`val.open alt[0]: unknown rule in p: "mapp"`,
		})
	}
}

func TestValidateGrammarCleanGrammarReportsNothing(t *testing.T) {
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"a": {
			Open:  []*GrammarAltSpec{{P: "b"}},
			Close: []*GrammarAltSpec{{R: "a"}},
		},
		"b": {Open: []*GrammarAltSpec{}},
	}}
	eqProblems(t, ValidateGrammar(gs, nil), nil)
}

func TestValidateGrammarMalformedInputYieldsNoProblems(t *testing.T) {
	eqProblems(t, ValidateGrammar(nil, nil), nil)
	eqProblems(t, ValidateGrammar(&GrammarSpec{}, nil), nil)
	eqProblems(t, ValidateGrammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{"a": {Open: "not-alts"}},
	}, nil), nil)
	eqProblems(t, ValidateGrammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{"a": {Open: []*GrammarAltSpec{nil}}},
	}, nil), nil)
	eqProblems(t, ValidateGrammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{"a": {Close: (*GrammarAltListSpec)(nil)}},
	}, nil), nil)
}

func TestValidateGrammarReportsRuleReferencesOnly(t *testing.T) {
	// The two runtimes word their group-tag message differently; keeping
	// this surface to rule references is what makes it identical in both.
	gs := &GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"a": {Open: []*GrammarAltSpec{{G: "bad tag!", P: "nope"}}},
	}}
	eqProblems(t, ValidateGrammar(gs, nil), []string{
		`a.open alt[0]: unknown rule in p: "nope"`,
	})
}
