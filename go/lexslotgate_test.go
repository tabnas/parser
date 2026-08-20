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

// A WILDCARD slot must not open the lexer gate.
//
// TS gates on `tcol[oc][tI].includes(tin$)` — plain list membership —
// and leaves `#AA` to the parser. Go's `tinMatch` treats `#AA` as
// matching every tin, so gating with it made a wildcard slot vote for
// every registered match token and the lexer emitted tokens TS never
// produces. A wildcard says the PARSER will accept any tin, not that the
// LEXER should invent one.
//
// Both slots are covered because the two failed differently. Slot 1 was
// broken by the slot fix above (it had no wildcard reachable before);
// slot 0 was ALREADY wrong on main, since the old gate used tinMatch
// there too. Measured in TypeScript: both inputs are rejected.
func TestLexerGateIgnoresWildcardSlots(t *testing.T) {
	no := false
	mk := func() *Tabnas {
		return Make(Options{
			Rule:    &RuleOptions{Start: "top", Exclude: "tabnas,imp"},
			Text:    &TextOptions{Lex: &no},
			Number:  &NumberOptions{Lex: &no},
			Comment: &CommentOptions{Lex: &no},
		})
	}

	// Slot 1 is `#AA`. `x` is claimable only by the #X match token, and
	// nothing asks for #X here.
	j := mk()
	a := j.Token("#A")
	j.SetOptions(Options{Match: &MatchOptions{Token: map[string]*regexp.Regexp{
		"#A": regexp.MustCompile(`^a`),
		"#X": regexp.MustCompile(`^x`),
	}}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{a}, {TinAA}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	if _, err := j.Parse("ax"); err == nil {
		t.Error("`ax` must not parse: a wildcard slot is not a licence " +
			"for the lexer to produce #X (TypeScript rejects it)")
	}

	// Slot 0 is `#AA`. Same rule, one token earlier.
	j2 := mk()
	j2.SetOptions(Options{Match: &MatchOptions{Token: map[string]*regexp.Regexp{
		"#X": regexp.MustCompile(`^x`),
	}}})
	j2.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinAA}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	if _, err := j2.Parse("x"); err == nil {
		t.Error("`x` must not parse: same reason, at slot 0 " +
			"(TypeScript rejects it)")
	}
}

// DIVERGENCE REGISTER — eager precedence. This pins what Go DOES, not
// what it should do, so it goes red the moment the divergence is
// repaired and cannot outlive what it records (admin ADR-14).
//
// TS makes ONE tin-ordered pass over its match tokens in which eagerness
// only bypasses the column gate, so an eager matcher earlier in that
// order wins over a position-expected one later. Go makes two passes,
// position-expected first, so the position-expected matcher always wins.
//
// With rule `#A #X`, an eager `#E`, and both `#E` and `#X` matching `q`:
//
//	TypeScript  `aq` -> rejected (it lexes #E at slot 1, the alternate
//	                    fails, and the error lands on #A at 1:1)
//	Go          `aq` -> accepted, slot 1 holding #X
//
// Collapsing Go's two passes is the honest repair and is deliberately
// NOT done here. Go's tins come from map iteration order, so a single
// tin-ordered pass makes the winner non-deterministic where TS's
// object-key order is stable — measured: it broke
// TestSerializedRegexTokensParse, which TS passes. The ordering has to
// be made deterministic first, and that is its own change.
func TestEagerPrecedenceDivergesFromTS(t *testing.T) {
	no := false
	j := Make(Options{
		Rule:    &RuleOptions{Start: "top", Exclude: "tabnas,imp"},
		Text:    &TextOptions{Lex: &no},
		Number:  &NumberOptions{Lex: &no},
		Comment: &CommentOptions{Lex: &no},
	})
	a := j.Token("#A")
	j.Token("#E")
	x := j.Token("#X")
	j.SetOptions(Options{Match: &MatchOptions{
		Token: map[string]*regexp.Regexp{
			"#A": regexp.MustCompile(`^a`),
			"#E": regexp.MustCompile(`^q`),
			"#X": regexp.MustCompile(`^q`),
		},
		TokenEager: map[string]bool{"#E": true},
	}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{a}, {x}},
			A: func(r *Rule, ctx *Context) { r.Node = r.O1.Name },
		})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	out, err := j.Parse("aq")
	if err != nil || out != "#X" {
		t.Fatalf("the recorded divergence no longer reproduces "+
			"(got out=%v err=%v). If Go now REJECTS `aq`, it has been "+
			"repaired to match TypeScript — delete this test and the "+
			"register entry it belongs to.", out, err)
	}
}
