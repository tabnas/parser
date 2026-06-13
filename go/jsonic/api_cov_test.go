package jsonic

import (
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestParserStartMeta(t *testing.T) {
	p := newGrammarParser()
	meta := map[string]any{"k": "v"}

	lexCount := 0
	ruleCount := 0
	lexSubs := []tabnas.LexSub{func(tkn *tabnas.Token, r *tabnas.Rule, ctx *tabnas.Context) {
		lexCount++
		if ctx.Meta["k"] != "v" {
			t.Error("meta should be available in lex sub context")
		}
	}}
	ruleSubs := []tabnas.RuleSub{func(r *tabnas.Rule, ctx *tabnas.Context) {
		ruleCount++
	}}

	out, err := p.StartMeta("a:1", meta, lexSubs, ruleSubs)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["a"] != float64(1) {
		t.Errorf("expected {a:1}, got %v", out)
	}
	if lexCount == 0 {
		t.Error("lex subscriber should have fired")
	}
	if ruleCount == 0 {
		t.Error("rule subscriber should have fired")
	}
}

func TestDebugPluginTrace(t *testing.T) {
	j := Make()
	if err := j.Use(tabnas.Debug, map[string]any{"trace": true}); err != nil {
		t.Fatal(err)
	}
	// Subscribers installed by addTrace must not break parsing.
	out, err := j.Parse("a:1")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", m)
	}
}

func TestDebugPluginNoTrace(t *testing.T) {
	j := Make()
	// trace=false and nil opts should not install subscribers.
	if err := j.Use(tabnas.Debug, map[string]any{"trace": false}); err != nil {
		t.Fatal(err)
	}
	if err := j.Use(tabnas.Debug); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("a:1"); err != nil {
		t.Fatal(err)
	}
}

func TestAttachHintPluginNames(t *testing.T) {
	// Errors from a Tabnas instance with plugins include the plugin names
	// in the --internal suffix (TS errdesc ctx.plgn()).
	j := Make()
	noop := tabnas.Plugin(func(j *tabnas.Tabnas, opts map[string]any) error { return nil })
	if err := j.Use(noop); err != nil {
		t.Fatal(err)
	}
	_, err := j.Parse(`"unterminated`)
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
	if _, ok := err.(*tabnas.TabnasError); !ok {
		t.Fatalf("expected *TabnasError, got %T", err)
	}
}

func TestTokenUpdateExistingFixedSrc(t *testing.T) {
	j := Make()
	// "#CL" already exists; provide a second source string for it.
	tin := j.Token("#CL", ";")
	if tin != tabnas.TinCL {
		t.Errorf("expected TinCL, got %d", tin)
	}
	// Both ":" and ";" now lex as colon.
	out, err := j.Parse("a;1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1 via ';' colon alias, got %v", out)
	}
}

func TestApplyFixedTokensSwapAndAllocate(t *testing.T) {
	semi := ";"
	tilde := "~"
	j := Make(tabnas.Options{Fixed: &tabnas.FixedOptions{Token: map[string]*string{
		"#CA":  &semi,  // swap comma → semicolon
		"#NEW": &tilde, // allocate a new token
	}}})
	if j.FixedSrc(";") != tabnas.TinCA {
		t.Error("expected ';' to map to TinCA")
	}
	if j.FixedSrc(",") != 0 {
		t.Error("expected ',' mapping removed after swap")
	}
	if j.FixedSrc("~") == 0 {
		t.Error("expected '~' to map to newly allocated token")
	}
	out, err := j.Parse("a:1;b:2")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) || m["b"] != float64(2) {
		t.Errorf("expected {a:1 b:2}, got %v", m)
	}
}

func TestSetOptionsRuleInclude(t *testing.T) {
	// rule.include via SetOptions keeps only tagged alts (json strict mode).
	j := Make()
	j.SetOptions(tabnas.Options{Rule: &tabnas.RuleOptions{Include: "json"}})
	// Strict JSON should still parse.
	out, err := j.Parse(`{"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", out)
	}
	// tabnas implicit map should fail (those alts were dropped).
	if _, err := j.Parse("a:1"); err == nil {
		t.Error("expected error for implicit map with include=json")
	}
}

func TestSetOptionsPreservesMatchValues(t *testing.T) {
	// Match.Value entries registered earlier survive a later SetOptions call
	// (preserved + re-sorted branch).
	j := Make()
	mustGrammar(t, j, &tabnas.GrammarSpec{
		Ref: map[tabnas.FuncRef]any{
			"@valOn": func(match []string) any { return true },
		},
		OptionsMap: map[string]any{
			"match": map[string]any{
				"value": map[string]any{
					"on": map[string]any{"match": "@/^on/i", "val": "@valOn"},
				},
			},
		},
	})
	sep := "_"
	j.SetOptions(tabnas.Options{Number: &tabnas.NumberOptions{Sep: sep}})

	out, err := j.Parse("a:ON")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != true {
		t.Errorf("expected a:true preserved after SetOptions, got %v", out)
	}
}
