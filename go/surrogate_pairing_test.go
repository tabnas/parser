// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// UTF-16 surrogate pairing inside quoted strings, across BOTH escape
// spellings. Mirrors ts/test/surrogate-pairing.test.js.
//
// An astral character has four standard spellings in this grammar, and all
// four must produce the same code point:
//
//	😀                  literal
//	\u{1F600}           braced, whole code point
//	😀        4-hex surrogate pair
//	\u{d83d}\u{de00}    braced surrogate pair
//
// plus the two mixed pairs. Only the literal, the whole-code-point braced
// form, and the 4-hex pair used to work. Pairing was a lookahead inside the
// 4-hex branch that recognised only another 4-hex escape, and the braced
// branch wrote its rune immediately with no lookahead at all — so three of
// the six paired spellings decoded to two U+FFFD, and Go REJECTED documents
// TypeScript accepts (rjrodger/aontu#24).
//
// Pairing now runs on the decoded code unit sequence, so the spelling of
// each half is irrelevant. That is the property worth testing: not "braced
// works" but "any high pairs with any low".
//
// LONE surrogates are NOT covered here beyond pinning the current Go
// behaviour. They fold to U+FFFD by deliberate design, documented in
// doc/differences.md line 327 ("U+FFFD (matches encoding/json)") — where TS
// preserves them. That divergence is a language-design question tracked in
// aontu#24, not a bug this file may quietly change.

import "testing"

// astral is U+1F600 GRINNING FACE, the character every spelling below
// denotes.
const astral = "\U0001F600"

func TestSurrogatePairingAllSpellings(t *testing.T) {
	j := strParser(nil)
	cases := []struct{ name, src string }{
		{"literal", `"` + astral + `"`},
		{"braced whole code point", `"\u{1F600}"`},
		{"4-hex pair", `"😀"`},
		{"braced pair", `"\u{d83d}\u{de00}"`},
		{"mixed: 4-hex high, braced low", `"\ud83d\u{de00}"`},
		{"mixed: braced high, 4-hex low", `"\u{d83d}\ude00"`},
	}
	for _, c := range cases {
		got, code := parseResult(t, j, c.src)
		if code != "" {
			t.Errorf("%s (%s): error %s", c.name, c.src, code)
			continue
		}
		if got != astral {
			t.Errorf("%s (%s): got %q (% x) want %q", c.name, c.src, got, []byte(got), astral)
		}
	}
}

func TestSurrogatePairingInContext(t *testing.T) {
	// The pairing state is per-string, so it must survive text on either
	// side and repeat within one string.
	j := strParser(nil)
	cases := []struct{ src, want string }{
		{`"a\u{d83d}\u{de00}b"`, "a" + astral + "b"},
		{`"\u{d83d}\u{de00}\u{d83d}\u{de00}"`, astral + astral},
		{`"😀\u{d83d}\u{de00}"`, astral + astral},
		{`"x\ud83d\u{de00}"`, "x" + astral},
		{`"\u{d83d}\ude00y"`, astral + "y"},
	}
	for _, c := range cases {
		got, code := parseResult(t, j, c.src)
		if code != "" {
			t.Errorf("%s: error %s", c.src, code)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q (% x) want %q", c.src, got, []byte(got), c.want)
		}
	}
}

func TestSurrogateLoneFoldsToReplacement(t *testing.T) {
	// Pins the DELIBERATE Go behaviour (doc/differences.md line 327). TS
	// preserves these instead; see the mirrored TS test, which asserts the
	// opposite on purpose. Changing this is a language decision, not a fix.
	//
	// The awkward cases are the point: a high surrogate is withheld to see
	// whether a low follows, so every way that wait can end has to resolve
	// it exactly once.
	const r = "�"
	j := strParser(nil)
	cases := []struct {
		name, src, want string
	}{
		{"lone high", `"\ud800"`, r},
		{"lone low", `"\udc00"`, r},
		{"lone braced high", `"\u{d800}"`, r},
		{"lone braced low", `"\u{dc00}"`, r},
		{"high then high", `"\ud800\ud801"`, r + r},
		{"low then high", `"\udc00\ud800"`, r + r},
		{"high then text", `"\ud800z"`, r + "z"},
		{"high at end of string", `"a\ud800"`, "a" + r},
		{"high then simple escape", `"\ud800\n"`, r + "\n"},
		{"high then \\x escape", `"\ud800\x41"`, r + "A"},
		{"high then whole-code-point braced", `"\ud800\u{41}"`, r + "A"},
		{"pair then lone high", `"😀\ud800"`, astral + r},
		{"lone high then pair", `"\ud800😀"`, r + astral},
	}
	for _, c := range cases {
		got, code := parseResult(t, j, c.src)
		if code != "" {
			t.Errorf("%s (%s): error %s", c.name, c.src, code)
			continue
		}
		if got != c.want {
			t.Errorf("%s (%s): got %q (% x) want %q", c.name, c.src, got, []byte(got), c.want)
		}
	}
}

func TestSurrogatePairingLeavesBareTextAlone(t *testing.T) {
	// Bare/unquoted text keeps the backslash literal — no escape processing
	// happens there at all, so a fix inside matchString must not touch it.
	// This agreed across ports before and must still agree (aontu#24).
	//
	// Checked at the LEXER, on the token itself: the bare engine has no
	// grammar to parse a bare run with, and the token is the precise claim
	// anyway — the text matcher never decodes an escape.
	j := Make(Options{})
	lex := NewLex(`a\ud800`, j.Config())
	tkn := lex.Next()
	if tkn == nil {
		t.Fatal("bare text produced no token")
	}
	if tkn.Name != "#TX" {
		t.Errorf("bare text: got token %s want #TX", tkn.Name)
	}
	if s, ok := tkn.Val.(string); !ok || s != `a\ud800` {
		t.Errorf("bare text value: got %#v want %q", tkn.Val, `a\ud800`)
	}
	if tkn.Src != `a\ud800` {
		t.Errorf("bare text src: got %q want %q", tkn.Src, `a\ud800`)
	}
}
