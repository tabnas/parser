// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Funcref-installation parity with the TS runtime: the `/append` suffix
// and the plain name are the SAME slot (providing both installs one), and
// the RuleSpec.Fnref method appends lifecycle actions by funcref like the
// TS rs.fnref(frm) method.

import "testing"

func fnrefParser(t *testing.T) (*Tabnas, *RuleSpec) {
	t.Helper()
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Token("#Ta", "a")
	var spec *RuleSpec
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		spec = rs
		rs.AddOpen(&AltSpec{S: [][]Tin{{j.Token("#Ta")}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	return j, spec
}

// /append and the plain name are one slot: a single function provided
// under both installs exactly once (matches TS fr[base+'/append'] ?? fr[base]).
func TestFnrefAppendPlainSameSlot(t *testing.T) {
	j, _ := fnrefParser(t)
	var log []string
	fn := StateAction(func(r *Rule, _ *Context) { log = append(log, "X") })
	// Provide the SAME function under both @top-bo and @top-bo/append.
	if err := j.Grammar(&GrammarSpec{
		Ref:  map[FuncRef]any{"@top-bo": fn, "@top-bo/append": fn},
		Rule: map[string]*GrammarRuleSpec{"top": {}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 {
		t.Errorf("/append and plain should be one slot: fired %d times, want 1 (%v)", len(log), log)
	}
}

// /prepend installs ahead of /append; both distinct funcs install.
func TestFnrefPrependAppendOrder(t *testing.T) {
	j, _ := fnrefParser(t)
	var log []string
	if err := j.Grammar(&GrammarSpec{
		Ref: map[FuncRef]any{
			"@top-bo/append":  StateAction(func(r *Rule, _ *Context) { log = append(log, "append") }),
			"@top-bo/prepend": StateAction(func(r *Rule, _ *Context) { log = append(log, "prepend") }),
		},
		Rule: map[string]*GrammarRuleSpec{"top": {}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0] != "prepend" || log[1] != "append" {
		t.Errorf("order: got %v want [prepend append]", log)
	}
}

// RuleSpec.Fnref appends a lifecycle action by funcref (TS rs.fnref parity).
func TestFnrefMethod(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Token("#Ta", "a")
	var log []string
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{j.Token("#Ta")}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		rs.Fnref(map[FuncRef]any{"@top-bo": StateAction(func(r *Rule, _ *Context) { log = append(log, "bo") })})
	})
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "bo" {
		t.Errorf("Fnref: got %v want [bo]", log)
	}
}

// The phase of an `@<rule>-<phase>` fnref is the suffix after the LAST
// hyphen, so a hyphenated rule name — the shape every ABNF grammar's rule
// names take — wires every lifecycle phase. Go always did; the TS runtime
// read everything after the FIRST hyphen as the phase and threw from
// grammar(). Pinned in both so the two cannot drift again (TS twin:
// cover-engine.test.js "fnref-hyphenated-rule-name").
func TestFnrefHyphenatedRuleName(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "valid-sem-ver"}})
	j.Token("#Ta", "a")
	var order []string
	push := func(s string) StateAction {
		return StateAction(func(r *Rule, _ *Context) { order = append(order, s) })
	}
	if err := j.Grammar(&GrammarSpec{
		Ref: map[FuncRef]any{
			"@valid-sem-ver-bo": push("bo"),
			"@valid-sem-ver-ao": push("ao"),
			"@valid-sem-ver-bc": push("bc"),
			"@valid-sem-ver-ac": push("ac"),
		},
		Rule: map[string]*GrammarRuleSpec{"valid-sem-ver": {
			Open:  []*GrammarAltSpec{{S: "#Ta"}},
			Close: []*GrammarAltSpec{{S: "#ZZ"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	want := []string{"bo", "ao", "bc", "ac"}
	if len(order) != 4 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] || order[3] != want[3] {
		t.Errorf("hyphenated fnref phases: got %v want %v", order, want)
	}
}
