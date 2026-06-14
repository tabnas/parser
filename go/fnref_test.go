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
