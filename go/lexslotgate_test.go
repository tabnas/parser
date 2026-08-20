/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

package tabnas

import (
	"regexp"
	"strings"
	"testing"
)

// The lexer's match-token gate must ask about the slot it is FILLING,
// not slot 0.
//
// TS passes the slot index into the matcher as `tI` and gates on
// `spec.def.tcol[oc][tI]`. Go has no tcol table, and the gate here used
// to read `altS[0]` for every fetch — which asks "could this token START
// an alternate of this rule", a different question from "is it
// acceptable where we actually are". Any token that opens the rule then
// lexed anywhere inside it.
//
// Found in @tabnas/chess. Its tag rule is `#TGN #ST` (a tag name then a
// quoted value), so on `[a b]` slot 1 wants #ST. Under slot-0 gating the
// tag-name matcher still ran at slot 1, `b` lexed as #TGN, the alternate
// failed on a tin mismatch, and the error was attributed to the fetch's
// first token: `a` at 1:2. TypeScript refused to lex `b` at all and
// reported `b` at 1:4. Same rule, same input, different token blamed.
//
// This reproduces the shape with the engine alone: two match tokens, one
// legal at slot 0 and one at slot 1, and input that puts the slot-0
// token where only the slot-1 token belongs.

func TestLexerGatesOnTheSlotBeingFilled(t *testing.T) {
	no := false
	// As @tabnas/chess does: with the generic matchers off, a character
	// no match token claims is a lex failure rather than falling through
	// to #TX. That is what makes the gate observable — with #TX on, the
	// text matcher claims the character at any slot and both ports blame
	// the fetch's first token.
	j := Make(Options{
		Rule:    &RuleOptions{Start: "top", Exclude: "tabnas,imp"},
		Text:    &TextOptions{Lex: &no},
		Number:  &NumberOptions{Lex: &no},
		Comment: &CommentOptions{Lex: &no},
	})
	tinAAA := j.Token("#AAA")
	tinBBB := j.Token("#BBB")
	j.SetOptions(Options{Match: &MatchOptions{Token: map[string]*regexp.Regexp{
		"#AAA": regexp.MustCompile(`^a+`),
		"#BBB": regexp.MustCompile(`^b+`),
	}}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		// Slot 0 takes #AAA, slot 1 takes #BBB. Nothing takes #AAA at
		// slot 1 — which is exactly what the gate must notice.
		rs.AddOpen(&AltSpec{S: [][]Tin{{tinAAA}, {tinBBB}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})

	// The happy path still parses, so the gate has not simply been
	// tightened into refusing everything.
	if _, err := j.Parse("a b"); err != nil {
		t.Fatalf("`a b` should parse: %v", err)
	}

	// `a a`: slot 1 holds an #AAA. Gating on slot 0 would let the
	// #AAA matcher run there and blame the FIRST token; gating on the
	// slot being filled refuses to lex it and blames the second.
	_, err := j.Parse("a a")
	if err == nil {
		t.Fatal("`a a` must not parse: slot 1 takes #BBB only")
	}
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T", err)
	}
	// Column 3 is the SECOND `a`. Column 1 is the first, which is what
	// slot-0 gating reported and what this test exists to catch.
	if 3 != je.Col {
		t.Errorf("error column = %d, want 3 (the second `a`, not the first)\n%s",
			je.Col, je.Error())
	}
	if !strings.Contains(je.Error(), ":1:3") {
		t.Errorf("expected the error to point at 1:3:\n%s", je.Error())
	}
}
