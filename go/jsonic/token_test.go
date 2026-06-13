package jsonic

import tabnas "github.com/tabnas/parser/go"

import "testing"

// Mirrors TS src/lexer.ts:115 — when Token.Val is a function, ResolveVal
// invokes it with (rule, ctx) rather than returning the function itself.

func TestResolveVal_StaticValue(t *testing.T) {
	tk := &tabnas.Token{Val: 42}
	if got := tk.ResolveVal(nil, nil); got != 42 {
		t.Errorf("expected 42, got %v", got)
	}
}

func TestResolveVal_NilValueUnchanged(t *testing.T) {
	tk := &tabnas.Token{Val: nil}
	if got := tk.ResolveVal(nil, nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestResolveVal_LazyFunction(t *testing.T) {
	calls := 0
	tk := &tabnas.Token{Val: tabnas.TokenValFunc(func(r *tabnas.Rule, ctx *tabnas.Context) any {
		calls++
		return "lazy"
	})}
	if got := tk.ResolveVal(nil, nil); got != "lazy" {
		t.Errorf("expected 'lazy', got %v", got)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestResolveVal_LazyFunctionReceivesRuleAndCtx(t *testing.T) {
	var seenRule *tabnas.Rule
	var seenCtx *tabnas.Context
	tk := &tabnas.Token{Val: tabnas.TokenValFunc(func(r *tabnas.Rule, ctx *tabnas.Context) any {
		seenRule = r
		seenCtx = ctx
		return nil
	})}
	rule := &tabnas.Rule{}
	ctx := &tabnas.Context{}
	tk.ResolveVal(rule, ctx)
	if seenRule != rule {
		t.Errorf("rule not forwarded: got %p want %p", seenRule, rule)
	}
	if seenCtx != ctx {
		t.Errorf("ctx not forwarded: got %p want %p", seenCtx, ctx)
	}
}

// Integration: a custom matcher emits a Token whose Val is a TokenValFunc.
// The parser must call it at resolution time, not store the function.
func TestResolveVal_LazyValueThroughParser(t *testing.T) {
	matcher := tabnas.LexMatcher(func(lex *tabnas.Lex, _ *tabnas.Rule) *tabnas.Token {
		if lex.Cursor().SI >= len(lex.Src) || lex.Src[lex.Cursor().SI] != '@' {
			return nil
		}
		start := lex.Cursor().SI
		lex.Cursor().SI++
		return &tabnas.Token{
			Name: "#TX",
			Tin:  tabnas.TinTX,
			Src:  lex.Src[start:lex.Cursor().SI],
			Val: tabnas.TokenValFunc(func(r *tabnas.Rule, ctx *tabnas.Context) any {
				return "resolved"
			}),
			SI: start,
			RI: lex.Cursor().RI,
			CI: lex.Cursor().CI,
		}
	})

	j := Make(tabnas.Options{
		Lex: &tabnas.LexOptions{
			Match: map[string]*tabnas.MatchSpec{
				"at": {
					Order: 1,
					Make: func(_ *tabnas.LexConfig, _ *tabnas.Options) tabnas.LexMatcher {
						return matcher
					},
				},
			},
		},
	})

	out, err := j.Parse(`@`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if out != "resolved" {
		t.Errorf("expected 'resolved', got %v", out)
	}
}
