/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

package tabnas

import (
	"strings"
	"testing"
)

// A grammar signals a domain-specific parse failure by marking a token
// bad with its own error code and halting: `ctx.ParseErr = tkn.Bad(code,
// details)` in Go, `ctx.t0.bad(code, details)` in TS. The engine must
// surface that code — not the generic "unexpected" — and must inject the
// details into the code's message and hint templates. Downstream
// grammars (e.g. @tabnas/csv's csv_extra_field / csv_missing_field)
// depend on this; a regression here silently degrades every one of them
// to a single opaque code.

func badCodeParser(t *testing.T, raise func(r *Rule, ctx *Context)) *Tabnas {
	t.Helper()
	j := Make(Options{
		Rule:  &RuleOptions{Start: "top", Exclude: "tabnas,imp"},
		Error: map[string]string{"probe_bad": "probe failed on row idx {rowidx}"},
		Hint:  map[string]string{"probe_bad": "row idx {rowidx} is the problem"},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		rs.AddBC(raise)
	})
	return j
}

func TestBadTokenCustomCodeFromAction(t *testing.T) {
	j := badCodeParser(t, func(r *Rule, ctx *Context) {
		ctx.ParseErr = ctx.T0.Bad("probe_bad", map[string]any{"rowidx": 7})
	})
	_, err := j.Parse("abc")
	if err == nil {
		t.Fatalf("expected a parse error")
	}
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T", err)
	}
	if je.Code != "probe_bad" {
		t.Errorf("expected code %q, got %q", "probe_bad", je.Code)
	}
	// Details from Bad() must reach both the message and the hint,
	// otherwise the templates ship their raw {placeholders} to the user.
	if !strings.Contains(je.Error(), "probe failed on row idx 7") {
		t.Errorf("details not injected into message:\n%s", je.Error())
	}
	if !strings.Contains(je.Error(), "row idx 7 is the problem") {
		t.Errorf("details not injected into hint:\n%s", je.Error())
	}
}

// alt.E is the other route to a bad token, and must behave identically.
func TestBadTokenCustomCodeFromAltError(t *testing.T) {
	j := Make(Options{
		Rule:  &RuleOptions{Start: "top", Exclude: "tabnas,imp"},
		Error: map[string]string{"probe_alt": "alt refused {what}"},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}}, E: func(r *Rule, ctx *Context) *Token {
			return ctx.T0.Bad("probe_alt", map[string]any{"what": "it"})
		}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	_, err := j.Parse("abc")
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T (%v)", err, err)
	}
	if je.Code != "probe_alt" {
		t.Errorf("expected code %q, got %q", "probe_alt", je.Code)
	}
	if !strings.Contains(je.Error(), "alt refused it") {
		t.Errorf("details not injected into message:\n%s", je.Error())
	}
}

// Marking a bad token must not write through to the NoToken singleton.
// ctx.T0 IS that singleton once the lookahead is empty, so before the
// copy-on-write in Token.Bad a single csv/multisource-style failure
// permanently rewrote a package-level global: every later generic
// "no alt matched" error in the process — in any instance, any grammar
// — inherited that custom code and its stale details.
func TestBadTokenDoesNotPoisonNoTokenSentinel(t *testing.T) {
	if NoToken.Err != "" || NoToken.Use != nil {
		t.Fatalf("NoToken already dirty at test start: %+v", *NoToken)
	}

	leaky := badCodeParser(t, func(r *Rule, ctx *Context) {
		ctx.ParseErr = ctx.T0.Bad("probe_bad", map[string]any{"rowidx": 7})
	})
	if _, err := leaky.Parse("abc"); err == nil {
		t.Fatalf("expected the first parse to fail")
	}

	if NoToken.Err != "" {
		t.Errorf("NoToken.Err was overwritten: %q", NoToken.Err)
	}
	if NoToken.Use != nil {
		t.Errorf("NoToken.Use was overwritten: %v", NoToken.Use)
	}

	// An unrelated instance must still report the generic code.
	plain := Make(Options{Rule: &RuleOptions{Start: "top", Exclude: "tabnas,imp"}})
	plain.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		rs.AddBC(func(r *Rule, ctx *Context) { ctx.ParseErr = ctx.T0 })
	})
	_, err := plain.Parse("abc")
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T (%v)", err, err)
	}
	if je.Code != "unexpected" {
		t.Errorf("leaked code from an earlier parse: got %q", je.Code)
	}
}

// A bad token with no code of its own still falls back to "unexpected".
func TestBadTokenWithoutCodeStaysUnexpected(t *testing.T) {
	j := badCodeParser(t, func(r *Rule, ctx *Context) {
		ctx.ParseErr = ctx.T0
	})
	_, err := j.Parse("abc")
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T (%v)", err, err)
	}
	if je.Code != "unexpected" {
		t.Errorf("expected code %q, got %q", "unexpected", je.Code)
	}
}
