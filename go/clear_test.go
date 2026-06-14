package tabnas

// Replacement of a rule's alternates and lifecycle actions by a later
// plugin. Mirrors ts/test/clear.test.js. All mechanisms are opt-in and
// backwards compatible:
//   - imperative: ModifyOpen/ModifyClose with Clear, ClearOpen/ClearClose,
//     ClearActions(...phases)
//   - declarative alternates: Open: &GrammarAltListSpec{Inject:{Clear:true}}
//   - declarative lifecycle: the "@<rule>-<phase>/replace" fnref suffix

import "testing"

func clearTryParse(j *Tabnas, src string) (any, string) {
	out, err := j.Parse(src)
	if err != nil {
		if te, ok := err.(*TabnasError); ok {
			return nil, te.Code
		}
		return nil, "?"
	}
	return out, ""
}

func TestClearModifyOpenReplaces(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	Ta := j.Token("#Ta", "a")
	Tb := j.Token("#Tb", "b")
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{{Ta}}, A: func(r *Rule, _ *Context) { r.Node = "A" }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	// Plugin B replaces A's open alternates: clear, then add.
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.ModifyOpen(&AltModListOpts{Clear: true})
		rs.Open = append(rs.Open, &AltSpec{S: [][]Tin{{Tb}}, A: func(r *Rule, _ *Context) { r.Node = "B" }})
	})
	if _, code := clearTryParse(j, "a"); code != "unexpected" {
		t.Errorf("a after clear: code=%q want unexpected", code)
	}
	if out, code := clearTryParse(j, "b"); code != "" || out != "B" {
		t.Errorf("b: out=%v code=%q", out, code)
	}
}

func TestClearOpenClose(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	Ta := j.Token("#Ta", "a")
	Tb := j.Token("#Tb", "b")
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{{Ta}}, A: func(r *Rule, _ *Context) { r.Node = "A" }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.ClearOpen()
		rs.Open = append(rs.Open, &AltSpec{S: [][]Tin{{Tb}}, A: func(r *Rule, _ *Context) { r.Node = "B" }})
	})
	if _, code := clearTryParse(j, "a"); code != "unexpected" {
		t.Errorf("a after ClearOpen: code=%q want unexpected", code)
	}
	if out, _ := clearTryParse(j, "b"); out != "B" {
		t.Errorf("b: out=%v", out)
	}
}

func TestClearActionsReplaces(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	Ta := j.Token("#Ta", "a")
	var log []string
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddBO(func(r *Rule, _ *Context) { log = append(log, "A") })
		rs.Open = []*AltSpec{{S: [][]Tin{{Ta}}}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.ClearActions("bo")
		rs.AddBO(func(r *Rule, _ *Context) { log = append(log, "B") })
	})
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "B" {
		t.Errorf("ClearActions: log=%v want [B]", log)
	}
}

func TestClearInjectReplaces(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Token("#Ta", "a")
	j.Token("#Tb", "b")
	if err := j.Grammar(&GrammarSpec{
		Ref: map[FuncRef]any{"@a": AltAction(func(r *Rule, _ *Context) { r.Node = "A" })},
		Rule: map[string]*GrammarRuleSpec{
			"top": {Open: []*GrammarAltSpec{{S: "#Ta", A: "@a"}}, Close: []*GrammarAltSpec{{S: "#ZZ"}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := j.Grammar(&GrammarSpec{
		Ref: map[FuncRef]any{"@b": AltAction(func(r *Rule, _ *Context) { r.Node = "B" })},
		Rule: map[string]*GrammarRuleSpec{
			"top": {Open: &GrammarAltListSpec{
				Alts:   []*GrammarAltSpec{{S: "#Tb", A: "@b"}},
				Inject: &GrammarInjectSpec{Clear: true},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, code := clearTryParse(j, "a"); code != "unexpected" {
		t.Errorf("a after inject.Clear: code=%q want unexpected", code)
	}
	if out, _ := clearTryParse(j, "b"); out != "B" {
		t.Errorf("b: out=%v", out)
	}
}

func TestClearFnrefReplace(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Token("#Ta", "a")
	var log []string
	mustG := func(gs *GrammarSpec) {
		if err := j.Grammar(gs); err != nil {
			t.Fatal(err)
		}
	}
	mustG(&GrammarSpec{
		Ref: map[FuncRef]any{"@top-bo": StateAction(func(r *Rule, _ *Context) { log = append(log, "A") })},
		Rule: map[string]*GrammarRuleSpec{
			"top": {Open: []*GrammarAltSpec{{S: "#Ta"}}, Close: []*GrammarAltSpec{{S: "#ZZ"}}},
		},
	})
	mustG(&GrammarSpec{
		Ref:  map[FuncRef]any{"@top-bo/replace": StateAction(func(r *Rule, _ *Context) { log = append(log, "B") })},
		Rule: map[string]*GrammarRuleSpec{"top": {}},
	})
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "B" {
		t.Errorf("fnref /replace: log=%v want [B]", log)
	}
}

func TestClearFnrefReplaceSurvivesDerive(t *testing.T) {
	var log []string
	pluginA := func(j *Tabnas, _ map[string]any) error {
		return j.Grammar(&GrammarSpec{
			Ref: map[FuncRef]any{"@top-bo": StateAction(func(r *Rule, _ *Context) { log = append(log, "A") })},
			Rule: map[string]*GrammarRuleSpec{
				"top": {Open: []*GrammarAltSpec{{S: "#Ta"}}, Close: []*GrammarAltSpec{{S: "#ZZ"}}},
			},
		})
	}
	pluginB := func(j *Tabnas, _ map[string]any) error {
		return j.Grammar(&GrammarSpec{
			Ref:  map[FuncRef]any{"@top-bo/replace": StateAction(func(r *Rule, _ *Context) { log = append(log, "B") })},
			Rule: map[string]*GrammarRuleSpec{"top": {}},
		})
	}
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Token("#Ta", "a")
	if err := j.Use(pluginA); err != nil {
		t.Fatal(err)
	}
	if err := j.Use(pluginB); err != nil {
		t.Fatal(err)
	}
	log = nil
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "B" {
		t.Errorf("parent: log=%v want [B]", log)
	}
	// Derive inherits the parent's options (incl. Rule.Start); the point
	// under test is that /replace survives the plugin re-application
	// during derivation.
	child, err := j.Derive()
	if err != nil {
		t.Fatal(err)
	}
	log = nil
	if _, err := child.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0] != "B" {
		t.Errorf("derived child: log=%v want [B] (replace must survive derive)", log)
	}
}

func TestClearBackwardsCompatAppend(t *testing.T) {
	// With no clear/replace, lifecycle actions from two plugins all fire
	// in registration order (the documented append behavior).
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	Ta := j.Token("#Ta", "a")
	var log []string
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddBO(func(r *Rule, _ *Context) { log = append(log, "A") })
		rs.Open = []*AltSpec{{S: [][]Tin{{Ta}}}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddBO(func(r *Rule, _ *Context) { log = append(log, "B") })
	})
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0] != "A" || log[1] != "B" {
		t.Errorf("append order: log=%v want [A B]", log)
	}
}
