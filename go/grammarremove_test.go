package tabnas

import "testing"

// A GrammarSpec can remove as well as add: a nil Rule entry prunes one
// rule, and Clear wipes the instance's grammar entirely. Mirrors the TS
// grammar-remove suite.
func TestGrammarSpecRemovesASingleRule(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {})
	j.Rule("other", func(rs *RuleSpec, _ *Parser) {})

	if err := j.Grammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{"other": nil},
	}); err != nil {
		t.Fatalf("Grammar: %v", err)
	}

	if _, ok := j.parser.RSM["other"]; ok {
		t.Error("rule 'other' should have been removed")
	}
	if _, ok := j.parser.RSM["top"]; !ok {
		t.Error("rule 'top' should survive")
	}
}

func TestGrammarSpecRemovingAbsentRuleIsNoOp(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {})

	if err := j.Grammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{"nosuchrule": nil},
	}); err != nil {
		t.Fatalf("Grammar: %v", err)
	}
	if _, ok := j.parser.RSM["top"]; !ok {
		t.Error("rule 'top' should survive")
	}
}

func TestGrammarSpecClearWipesRulesAndFixedTokens(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {})

	if len(j.parser.Config.FixedTokens) == 0 {
		t.Fatal("expected default fixed tokens before clear")
	}

	if err := j.Grammar(&GrammarSpec{Clear: true}); err != nil {
		t.Fatalf("Grammar: %v", err)
	}

	if len(j.parser.RSM) != 0 {
		t.Errorf("expected no rules after clear, got %d", len(j.parser.RSM))
	}
	if len(j.parser.Config.FixedTokens) != 0 {
		t.Errorf("expected no fixed tokens after clear, got %d",
			len(j.parser.Config.FixedTokens))
	}
}
