// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package tabnas

// Tests for the TS-parity API surface: the Empty instance method and
// String (TS: tabnas.empty / toString, see ts/test/cover-engine.test.js),
// and the exported matcher/lex/point/rulespec constructors (TS: the
// makeXMatcher / makeLex / makePoint / makeRuleSpec exports).

import (
	"fmt"
	"strings"
	"testing"
)

func TestEmptyMethod(t *testing.T) {
	j := Make(Options{Tag: "parent"})
	j.Decorate("mark", "yes")

	e := j.Empty()
	if e == nil {
		t.Fatal("Empty() returned nil")
	}
	// Fresh standalone instance: no receiver state inherited.
	if e.Decoration("mark") != nil {
		t.Error("Empty() must not inherit receiver decorations")
	}
	if strings.Contains(e.Id(), "parent") {
		t.Errorf("Empty() must not inherit receiver tag: %s", e.Id())
	}
	if len(e.RSM()) != 0 {
		t.Errorf("Empty() should have no grammar rules, got %d", len(e.RSM()))
	}

	// Options pass through (TS: tn.empty({ tag: 'em' })).
	e2 := j.Empty(Options{Tag: "em"})
	if !strings.HasSuffix(e2.Id(), "/em") {
		t.Errorf("Empty(opts) tag not set: %s", e2.Id())
	}
}

func TestStringIsId(t *testing.T) {
	j := Make(Options{Tag: "st"})
	if j.String() != j.Id() {
		t.Errorf("String() = %q, want Id() = %q", j.String(), j.Id())
	}
	if !strings.HasPrefix(j.String(), "Tabnas/") {
		t.Errorf("String() should start with Tabnas/: %q", j.String())
	}
	// fmt formatting picks up the Stringer (TS: '' + tn === tn.id).
	if fmt.Sprintf("%v", j) != j.Id() {
		t.Errorf("fmt %%v = %q, want %q", fmt.Sprintf("%v", j), j.Id())
	}
}

func TestExportedMatcherFactories(t *testing.T) {
	cfg := DefaultLexConfig()

	// Walk a source that exercises each matcher in turn, calling the
	// factory-built matchers directly (no engine dispatch).
	lex := MakeLex("{ ab 12 'q' #c\n", cfg)

	if tkn := MakeFixedMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinOB {
		t.Fatalf("fixed: got %v", tkn)
	}
	if tkn := MakeSpaceMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinSP {
		t.Fatalf("space: got %v", tkn)
	}
	if tkn := MakeTextMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinTX || tkn.Val != "ab" {
		t.Fatalf("text: got %v", tkn)
	}
	if tkn := MakeSpaceMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinSP {
		t.Fatalf("space2: got %v", tkn)
	}
	if tkn := MakeNumberMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinNR || tkn.Val != float64(12) {
		t.Fatalf("number: got %v", tkn)
	}
	if tkn := MakeSpaceMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinSP {
		t.Fatalf("space3: got %v", tkn)
	}
	if tkn := MakeStringMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinST || tkn.Val != "q" {
		t.Fatalf("string: got %v", tkn)
	}
	if tkn := MakeSpaceMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinSP {
		t.Fatalf("space4: got %v", tkn)
	}
	if tkn := MakeCommentMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinCM || tkn.Src != "#c" {
		t.Fatalf("comment: got %v", tkn)
	}
	if tkn := MakeLineMatcher(cfg, nil)(lex, nil); tkn == nil || tkn.Tin != TinLN {
		t.Fatalf("line: got %v", tkn)
	}
}

func TestExportedMatcherFactoriesHonourLexFlags(t *testing.T) {
	// The returned matchers read the lexer's live config, so disabling a
	// matcher's lex flag suppresses it (the guardedMatcher gate).
	cfg := DefaultLexConfig()
	cfg.SpaceLex = false
	cfg.NumberLex = false
	cfg.CommentLex = false

	if tkn := MakeSpaceMatcher(cfg, nil)(MakeLex("  x", cfg), nil); tkn != nil {
		t.Errorf("space matcher should be gated off, got %v", tkn)
	}
	if tkn := MakeNumberMatcher(cfg, nil)(MakeLex("12", cfg), nil); tkn != nil {
		t.Errorf("number matcher should be gated off, got %v", tkn)
	}
	if tkn := MakeCommentMatcher(cfg, nil)(MakeLex("#c", cfg), nil); tkn != nil {
		t.Errorf("comment matcher should be gated off, got %v", tkn)
	}
}

func TestExportedMatcherFactoryCheckHook(t *testing.T) {
	// The check hook short-circuit (TS guardedMatcher check contract):
	// Done + Token overrides the matcher body.
	cfg := DefaultLexConfig()
	forced := MakeToken("#SP", TinSP, nil, "!", MakePoint(1))
	cfg.SpaceCheck = func(lex *Lex) *LexCheckResult {
		return &LexCheckResult{Done: true, Token: forced}
	}
	tkn := MakeSpaceMatcher(cfg, nil)(MakeLex("zz", cfg), nil)
	if tkn != forced {
		t.Errorf("check hook should short-circuit, got %v", tkn)
	}
}

func TestMakePointDefaults(t *testing.T) {
	p := MakePoint(10)
	if p.Len != 10 || p.SI != 0 || p.RI != 1 || p.CI != 1 {
		t.Errorf("MakePoint defaults wrong: %+v", p)
	}
	p2 := MakePoint(10, 3, 2, 5)
	if p2.SI != 3 || p2.RI != 2 || p2.CI != 5 {
		t.Errorf("MakePoint positional args wrong: %+v", p2)
	}
}

func TestMakeLexAliasesNewLex(t *testing.T) {
	cfg := DefaultLexConfig()
	lex := MakeLex("a", cfg)
	if lex == nil || lex.Src != "a" || lex.Config != cfg {
		t.Fatalf("MakeLex wrong: %+v", lex)
	}
	if tkn := lex.Next(); tkn.Tin != TinTX || tkn.Val != "a" {
		t.Errorf("MakeLex lexing: got %v", tkn)
	}
}

func TestMakeRuleSpecStandalone(t *testing.T) {
	rs := MakeRuleSpec("top")
	if rs.Name != "top" {
		t.Fatalf("MakeRuleSpec name: %q", rs.Name)
	}
	rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}}, A: func(r *Rule, _ *Context) {
		r.Node = r.O0.Val
	}})
	rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})

	// Install the standalone spec and parse with it.
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.RSM()["top"] = rs
	out, err := j.Parse("hello")
	if err != nil || out != "hello" {
		t.Errorf("parse via MakeRuleSpec rule: out=%v err=%v", out, err)
	}
}

func TestMatcherFactoryAsMatchSpec(t *testing.T) {
	// A factory can be handed to LexOptions.Match as a MatchSpec.Make,
	// the plugin-author registration seam. Registering the text matcher
	// ahead of all built-ins makes 'true' lex as #TX text instead of the
	// #VL keyword.
	f := false
	j := Make(Options{
		Value: &ValueOptions{Lex: &f},
		Lex: &LexOptions{Match: map[string]*MatchSpec{
			"pretext": {Order: 500000, Make: MakeTextMatcher},
		}},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}}, A: func(r *Rule, _ *Context) {
			r.Node = r.O0.Val
		}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	j.SetOptions(Options{Rule: &RuleOptions{Start: "top"}})
	out, err := j.Parse("true")
	if err != nil || out != "true" {
		t.Errorf("factory as MatchSpec: out=%v err=%v", out, err)
	}
}
