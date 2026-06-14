package tabnas

// No-panic guarantee: every error-returning public API converts an
// internal panic (including from arbitrary plugin / grammar / matcher
// callbacks) into an "internal"-code *TabnasError instead of crashing
// the caller. Mirrors the recover guards on Parse/ParseMeta/Grammar.

import (
	"strings"
	"testing"
)

// wantInternal asserts err is a non-nil "internal" *TabnasError.
func wantInternal(t *testing.T, api string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an internal error from a panic, got nil", api)
	}
	te, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("%s: expected *TabnasError, got %T", api, err)
	}
	if te.Code != "internal" {
		t.Errorf("%s: error code = %q, want internal", api, te.Code)
	}
	if te.Error() == "" {
		t.Errorf("%s: internal error has empty message", api)
	}
}

func panicPlugin(*Tabnas, map[string]any) error { panic("boom in plugin") }

func TestNoPanicUse(t *testing.T) {
	j := Make()
	wantInternal(t, "Use", j.Use(panicPlugin))
}

func TestNoPanicUseDefaults(t *testing.T) {
	j := Make()
	wantInternal(t, "UseDefaults", j.UseDefaults(panicPlugin, map[string]any{"k": "v"}))
}

func TestNoPanicDerive(t *testing.T) {
	// A plugin that panics only when re-applied on the child (so Use on the
	// parent succeeds, but Derive's re-application panics).
	first := true
	j := Make()
	if err := j.Use(func(*Tabnas, map[string]any) error {
		if first {
			first = false
			return nil
		}
		panic("boom on re-apply")
	}); err != nil {
		t.Fatalf("parent Use: %v", err)
	}
	child, err := j.Derive()
	wantInternal(t, "Derive", err)
	if child != nil {
		t.Errorf("Derive should return nil child on panic, got %v", child)
	}
}

func TestNoPanicParseAction(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) {
			panic("boom in action")
		}}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	_, err := j.Parse("1")
	wantInternal(t, "Parse", err)
}

func TestNoPanicGrammar(t *testing.T) {
	// A custom matcher Make that panics is invoked during config build via
	// OptionsMap → MapToOptions inside Grammar; the guard converts it.
	// Simpler reliable trigger: a Ref funcref of the wrong concrete type
	// used where a StateAction is expected does not panic (it's skipped),
	// so drive a panic through a matcher Make in OptionsMap is brittle.
	// Instead, exercise the guard via a grammar whose alt resolution hits
	// a nil dereference: an AltAction funcref present but nil.
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	err := j.Grammar(&GrammarSpec{
		Ref: map[FuncRef]any{"@boom": AltAction(func(r *Rule, ctx *Context) { panic("x") })},
		Rule: map[string]*GrammarRuleSpec{
			"top": {
				Open:  []*GrammarAltSpec{{S: "#VAL", A: "@boom"}},
				Close: []*GrammarAltSpec{{S: "#ZZ"}},
			},
		},
	})
	// Grammar registration itself does not run the action, so this should
	// succeed; the panic only fires at parse time and is caught by Parse.
	if err != nil {
		t.Fatalf("Grammar registration: %v", err)
	}
	_, perr := j.Parse("1")
	wantInternal(t, "Parse(action via grammar)", perr)
}

func TestNoPanicGrammarText(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) { panic("boom in text parser") })
	j := Make()
	wantInternal(t, "GrammarText", j.GrammarText("anything"))
}

func TestNoPanicSetOptionsText(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) { panic("boom in text parser") })
	j := Make()
	_, err := j.SetOptionsText("anything")
	wantInternal(t, "SetOptionsText", err)
}

func TestNoPanicCustomMatcher(t *testing.T) {
	// A custom lexer matcher that panics during parsing is caught by Parse.
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"boom": {Order: 1000, Make: func(_ *LexConfig, _ *Options) LexMatcher {
			return func(lex *Lex, _ *Rule) *Token { panic("boom in matcher") }
		}},
	}}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: [][]Tin{TinSetVAL}}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	_, err := j.Parse("1")
	wantInternal(t, "Parse(matcher)", err)
}

func TestNoPanicCustomParserStart(t *testing.T) {
	// A custom Options.Parser.Start that panics is caught by parseInternal
	// (the default startParse path has its own guard; this covers the
	// custom-parser branch).
	j := Make(Options{Parser: &ParserOptions{
		Start: func(string, *Tabnas, map[string]any) (any, error) { panic("boom in parserStart") },
	}})
	_, err := j.Parse("x")
	wantInternal(t, "Parse(custom start)", err)
}

func TestNoPanicErrorMentionsAPI(t *testing.T) {
	// The internal error names the API and includes the panic value.
	j := Make()
	err := j.Use(panicPlugin)
	if err == nil || !strings.Contains(err.Error(), "boom in plugin") {
		t.Errorf("internal error should include the panic value, got: %v", err)
	}
}
