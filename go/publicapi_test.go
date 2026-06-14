package tabnas

// Verifies a working rule can be defined and introspected using ONLY the
// exported RuleSpec method/getter API — no direct field access. This is
// the contract external grammar packages rely on now that the alternate
// and lifecycle lists are unexported (aligned with the TS RuleSpec).

import "testing"

func TestPublicAPIRuleConstruction(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	Ta := j.Token("#Ta", "a")
	Tb := j.Token("#Tb", "b")

	var phases []string
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		// Append alternates and lifecycle actions via methods only.
		rs.AddOpen(&AltSpec{S: [][]Tin{{Ta}}, A: func(r *Rule, _ *Context) { r.Node = "A" }})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		rs.AddBO(func(r *Rule, _ *Context) { phases = append(phases, "bo") })
		rs.AddAC(func(r *Rule, _ *Context) { phases = append(phases, "ac") })
		rs.PrependBO(func(r *Rule, _ *Context) { phases = append(phases, "bo0") })
		// Prepend an alternate ahead of the first.
		rs.PrependOpen(&AltSpec{S: [][]Tin{{Tb}}, A: func(r *Rule, _ *Context) { r.Node = "B" }})
	})

	if out, err := j.Parse("a"); err != nil || out != "A" {
		t.Fatalf("parse a: out=%v err=%v", out, err)
	}
	if out, err := j.Parse("b"); err != nil || out != "B" {
		t.Fatalf("parse b: out=%v err=%v", out, err)
	}

	// Introspect via getters only.
	rs := j.RSM()["top"]
	if len(rs.OpenAlts()) != 2 {
		t.Errorf("OpenAlts: got %d, want 2", len(rs.OpenAlts()))
	}
	if len(rs.CloseAlts()) != 1 {
		t.Errorf("CloseAlts: got %d, want 1", len(rs.CloseAlts()))
	}
	if !rs.HasBO() || !rs.HasAC() || rs.HasAO() || rs.HasBC() {
		t.Errorf("Has* flags wrong: bo=%v ao=%v bc=%v ac=%v",
			rs.HasBO(), rs.HasAO(), rs.HasBC(), rs.HasAC())
	}
	if len(rs.Actions("bo")) != 2 { // prepended bo0 + bo
		t.Errorf("Actions(bo): got %d, want 2", len(rs.Actions("bo")))
	}
	// bo0 (prepended) fires before bo.
	if len(phases) < 2 || phases[0] != "bo0" || phases[1] != "bo" {
		t.Errorf("prepend order: %v", phases)
	}

	// Replace via methods: ClearOpen + AddOpen, ClearActions + AddBO.
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.ClearOpen().AddOpen(&AltSpec{S: [][]Tin{{Tb}}, A: func(r *Rule, _ *Context) { r.Node = "B2" }})
		rs.ClearActions("bo")
	})
	if _, err := j.Parse("a"); err == nil {
		t.Error("after ClearOpen+replace, 'a' should be rejected")
	}
	if out, _ := j.Parse("b"); out != "B2" {
		t.Errorf("after replace: got %v want B2", out)
	}
	if j.RSM()["top"].HasBO() {
		t.Error("ClearActions(bo) should have removed all bo actions")
	}
}
