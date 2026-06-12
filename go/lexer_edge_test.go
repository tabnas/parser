package tabnas

import (
	"strings"
	"testing"
)

// --- matchFixed fallback: standalone lexer without FixedSorted ---

func TestMatchFixedFallbackSingleChar(t *testing.T) {
	// A hand-built LexConfig with empty FixedSorted exercises the
	// single-char fallback lookup in matchFixed (and matchText/isTextChar).
	cfg := DefaultLexConfig()
	cfg.FixedSorted = nil

	lex := NewLex("{a}", cfg)
	t1 := lex.Next()
	if t1.Tin != TinOB {
		t.Fatalf("expected OB, got %s", t1.Name)
	}
	// "a" is not fixed → matchFixed fallback returns nil, text matches;
	// the trailing "}" is pushed as lookahead via the fallback subMatchFixed.
	t2 := lex.Next()
	if t2.Tin != TinTX || t2.Val != "a" {
		t.Fatalf("expected TX a, got %s %v", t2.Name, t2.Val)
	}
	t3 := lex.Next()
	if t3.Tin != TinCB {
		t.Fatalf("expected CB, got %s", t3.Name)
	}
	if lex.Next().Tin != TinZZ {
		t.Error("expected ZZ at end")
	}
}

func TestIsFollowingTextFallback(t *testing.T) {
	// Number followed by a structural char with no FixedSorted exercises
	// the isTextChar single-char fallback.
	cfg := DefaultLexConfig()
	cfg.FixedSorted = nil
	lex := NewLex("1}", cfg)
	tkn := lex.Next()
	if tkn.Tin != TinNR || tkn.Val != float64(1) {
		t.Fatalf("expected NR 1, got %s %v", tkn.Name, tkn.Val)
	}
}

// --- matchString escape sequences ---

func TestMatchStringEscapes(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{`"\x41"`, "A"},                       // ASCII escape
		{"\"\\u0042\"", "B"},                  // 4-digit unicode
		{`"\u{1F600}"`, "\U0001F600"},         // braced unicode
		{`"a\x41b"`, "aAb"},                   // embedded ascii
		{`"\b\f\n\r\t\v"`, "\b\f\n\r\t\v"},    // control escapes
		{`"\"\'\` + "`" + `\\\/"`, "\"'`\\/"}, // quote/backslash/slash escapes
		{`"\q"`, "q"},                         // unknown escape (allowed by default)
	}
	for _, tt := range tests {
		lex := NewLex(tt.src, DefaultLexConfig())
		tkn := lex.Next()
		if lex.Err != nil {
			t.Errorf("%q: unexpected lex error: %v", tt.src, lex.Err)
			continue
		}
		if tkn.Tin != TinST || tkn.Val != tt.want {
			t.Errorf("%q: expected %q, got %v (%s)", tt.src, tt.want, tkn.Val, tkn.Name)
		}
	}
}

func TestMatchStringEscapeErrors(t *testing.T) {
	tests := []struct {
		src  string
		code string
	}{
		{`"\xZZ"`, "invalid_ascii"},                 // bad hex digits
		{`"\x4`, "invalid_ascii"},                   // truncated at end
		{`"\uZZZZ"`, "invalid_unicode"},             // bad 4-digit hex
		{`"\u4`, "invalid_unicode"},                 // truncated 4-digit form
		{`"\u{GG}"`, "invalid_unicode"},             // bad braced hex
		{`"\u{42`, "invalid_unicode"},               // unterminated brace
		{`"abc`, "unterminated_string"},             // no closing quote
		{`"a` + "\n" + `b"`, "unterminated_string"}, // control char in non-multiline
		{`"\`, "unterminated_string"},               // escape at end of source
	}
	for _, tt := range tests {
		lex := NewLex(tt.src, DefaultLexConfig())
		tkn := lex.Next()
		if tkn.Tin != TinZZ {
			t.Errorf("%q: expected ZZ on error, got %s", tt.src, tkn.Name)
		}
		je, ok := lex.Err.(*TabnasError)
		if !ok {
			t.Errorf("%q: expected TabnasError, got %v", tt.src, lex.Err)
			continue
		}
		if je.Code != tt.code {
			t.Errorf("%q: expected code %s, got %s", tt.src, tt.code, je.Code)
		}
	}
}

func TestMatchStringUnknownEscapeRejected(t *testing.T) {
	cfg := DefaultLexConfig()
	cfg.AllowUnknownEscape = false
	lex := NewLex(`"\q"`, cfg)
	lex.Next()
	je, ok := lex.Err.(*TabnasError)
	if !ok || je.Code != "unexpected" {
		t.Errorf("expected unexpected error for unknown escape, got %v", lex.Err)
	}
}

func TestMatchStringAbandon(t *testing.T) {
	// string.abandon: matchString returns nil instead of a bad token,
	// allowing other matchers to try; with nothing else matching, an
	// "unexpected" error results (TS string.abandon behavior).
	srcs := []string{
		`"\xZZ"`,           // invalid ascii
		`"\x4`,             // truncated ascii
		`"\uZZZZ"`,         // invalid unicode
		`"\u4`,             // truncated unicode
		`"\u{GG}"`,         // invalid braced unicode
		`"\u{42`,           // unterminated brace
		`"abc`,             // unterminated string
		`"a` + "\n" + `b"`, // control char
		`"\q"`,             // unknown escape (with AllowUnknownEscape off)
	}
	for _, src := range srcs {
		cfg := DefaultLexConfig()
		cfg.StringAbandon = true
		cfg.AllowUnknownEscape = false
		lex := NewLex(src, cfg)
		tkn := lex.Next()
		if tkn.Tin != TinZZ {
			t.Errorf("%q: expected ZZ, got %s", src, tkn.Name)
		}
		je, ok := lex.Err.(*TabnasError)
		if !ok || je.Code != "unexpected" {
			t.Errorf("%q: expected unexpected after abandon, got %v", src, lex.Err)
		}
	}
}

func TestMatchStringEscapeMap(t *testing.T) {
	// Custom escape map takes precedence over built-in escapes.
	cfg := DefaultLexConfig()
	cfg.EscapeMap = map[string]string{"z": "ZED"}
	lex := NewLex(`"a\zb"`, cfg)
	tkn := lex.Next()
	if tkn.Val != "aZEDb" {
		t.Errorf("expected aZEDb, got %v", tkn.Val)
	}
}

func TestMatchStringReplace(t *testing.T) {
	// string.replace substitutes body characters during the fast scan.
	cfg := DefaultLexConfig()
	cfg.StringReplace = map[rune]string{'o': "0"}
	lex := NewLex(`"foo bar"`, cfg)
	tkn := lex.Next()
	if tkn.Val != "f00 bar" {
		t.Errorf("expected f00 bar, got %v", tkn.Val)
	}
}

func TestMatchStringMultiline(t *testing.T) {
	// Backtick strings accept raw newlines (MultiChars).
	lex := NewLex("`ab\ncd`", DefaultLexConfig())
	tkn := lex.Next()
	if tkn.Tin != TinST || tkn.Val != "ab\ncd" {
		t.Errorf("expected multiline string, got %v (%s)", tkn.Val, tkn.Name)
	}
}

// --- parseHexInt ---

func TestParseHexInt(t *testing.T) {
	if got := parseHexInt("ff"); got != 255 {
		t.Errorf("ff → %d, want 255", got)
	}
	if got := parseHexInt("FF"); got != 255 {
		t.Errorf("FF → %d, want 255", got)
	}
	if got := parseHexInt("10"); got != 16 {
		t.Errorf("10 → %d, want 16", got)
	}
	if got := parseHexInt("xy"); got != -1 {
		t.Errorf("xy → %d, want -1", got)
	}
}

// --- matchLine single mode ---

func TestMatchLineSingle(t *testing.T) {
	cfg := DefaultLexConfig()
	cfg.LineSingle = true
	cfg.IgnoreSet = map[Tin]bool{} // surface LN tokens
	lex := NewLex("\r\n\n", cfg)

	t1 := lex.Next()
	if t1.Tin != TinLN || t1.Src != "\r\n" {
		t.Errorf("expected single LN for \\r\\n, got %s %q", t1.Name, t1.Src)
	}
	t2 := lex.Next()
	if t2.Tin != TinLN || t2.Src != "\n" {
		t.Errorf("expected single LN for \\n, got %s %q", t2.Name, t2.Src)
	}
	if lex.Next().Tin != TinZZ {
		t.Error("expected ZZ at end")
	}
}

// --- LexCheck hooks: intercept each built-in matcher ---

func TestLexCheckHooks(t *testing.T) {
	// Each Check hook can (a) return a replacement token, (b) suppress the
	// matcher (Done with nil Token), or (c) return nil to continue normally.
	assign := func(cfg *LexConfig, which string, chk LexCheck) {
		switch which {
		case "fixed":
			cfg.FixedCheck = chk
		case "space":
			cfg.SpaceCheck = chk
		case "line":
			cfg.LineCheck = chk
		case "string":
			cfg.StringCheck = chk
		case "comment":
			cfg.CommentCheck = chk
		case "number":
			cfg.NumberCheck = chk
		case "text":
			cfg.TextCheck = chk
		}
	}

	cases := []struct {
		which string
		src   string
	}{
		{"fixed", "{"},
		{"space", " "},
		{"line", "\n"},
		{"string", `"s"`},
		{"comment", "# c"},
		{"number", "1"},
		{"text", "abc"},
	}

	for _, c := range cases {
		// (a) Replacement token.
		cfg := DefaultLexConfig()
		cfg.IgnoreSet = map[Tin]bool{}
		assign(cfg, c.which, func(lex *Lex) *LexCheckResult {
			tkn := lex.Token("#VL", TinVL, "hooked", lex.Fwd(1))
			p := lex.Cursor()
			p.SI = p.Len // consume everything
			return &LexCheckResult{Done: true, Token: tkn}
		})
		lex := NewLex(c.src, cfg)
		tkn := lex.Next()
		if tkn.Val != "hooked" {
			t.Errorf("%s check: expected hooked token, got %v (%s)", c.which, tkn.Val, tkn.Name)
		}

		// (b) Done with nil token: matcher suppressed, lexing continues.
		cfg = DefaultLexConfig()
		cfg.IgnoreSet = map[Tin]bool{}
		assign(cfg, c.which, func(lex *Lex) *LexCheckResult {
			return &LexCheckResult{Done: true}
		})
		lex = NewLex(c.src, cfg)
		tkn = lex.Next()
		// The original matcher must not have produced its usual token kind
		// (either another matcher claims the source or an error occurs).
		if c.which == "fixed" && tkn.Tin == TinOB {
			t.Errorf("fixed check suppression failed: got %s", tkn.Name)
		}

		// (c) nil result: normal matching continues.
		cfg = DefaultLexConfig()
		cfg.IgnoreSet = map[Tin]bool{}
		assign(cfg, c.which, func(lex *Lex) *LexCheckResult { return nil })
		lex = NewLex(c.src, cfg)
		tkn = lex.Next()
		if tkn.Tin == TinZZ && lex.Err != nil {
			t.Errorf("%s check nil: unexpected error %v", c.which, lex.Err)
		}
	}
}

func TestMatchCheckHook(t *testing.T) {
	// MatchCheck guards the match matcher (requires MatchLex on).
	cfg := DefaultLexConfig()
	cfg.MatchLex = true
	cfg.MatchCheck = func(lex *Lex) *LexCheckResult {
		if strings.HasPrefix(lex.Fwd(2), "%%") {
			tkn := lex.Token("#VL", TinVL, "pct", "%%")
			p := lex.Cursor()
			p.SI += 2
			p.CI += 2
			return &LexCheckResult{Done: true, Token: tkn}
		}
		return nil
	}
	lex := NewLex("%%", cfg)
	tkn := lex.Next()
	if tkn.Val != "pct" {
		t.Errorf("expected pct from MatchCheck, got %v", tkn.Val)
	}

	// Done with nil token suppresses matchMatch.
	cfg = DefaultLexConfig()
	cfg.MatchLex = true
	cfg.MatchCheck = func(lex *Lex) *LexCheckResult {
		return &LexCheckResult{Done: true}
	}
	lex = NewLex("abc", cfg)
	tkn = lex.Next()
	if tkn.Tin != TinTX {
		t.Errorf("expected TX via fall-through, got %s", tkn.Name)
	}
}

// --- Custom matchers at every priority band of nextRaw ---

func TestCustomMatcherPriorityBands(t *testing.T) {
	cfg := DefaultLexConfig()
	cfg.MatchLex = true
	cfg.TextLex = false
	cfg.ValueLex = false

	seen := []int{}
	noop := func(order int) *MatcherEntry {
		return &MatcherEntry{
			Name:     "band",
			Priority: order,
			Match: func(lex *Lex, rule *Rule) *Token {
				seen = append(seen, order)
				return nil
			},
		}
	}
	// One matcher in each band, plus a final one (>= 8e6) that matches.
	cfg.CustomMatchers = []*MatcherEntry{
		noop(500000),
		noop(1500000),
		noop(2500000),
		noop(3500000),
		noop(4500000),
		noop(5500000),
		noop(6500000),
		noop(7500000),
		{
			Name:     "final",
			Priority: 9000000,
			Match: func(lex *Lex, rule *Rule) *Token {
				tkn := lex.Token("#VL", TinVL, "fin", "q")
				p := lex.Cursor()
				p.SI++
				p.CI++
				return tkn
			},
		},
	}

	lex := NewLex("q", cfg)
	tkn := lex.Next()
	if tkn.Val != "fin" {
		t.Fatalf("expected final matcher to claim 'q', got %v (%s)", tkn.Val, tkn.Name)
	}
	if len(seen) != 8 {
		t.Errorf("expected all 8 priority bands visited, got %v", seen)
	}
}

// --- matchMatch: function-form value and token matchers ---

func TestMatchValueFnForm(t *testing.T) {
	j := Make(Options{Match: &MatchOptions{
		Value: map[string]*MatchValueSpec{
			"pct": {Fn: func(lex *Lex, rule *Rule) *Token {
				if strings.HasPrefix(lex.Fwd(2), "%%") {
					tkn := lex.Token("#VL", TinVL, "PCT", "%%")
					p := lex.Cursor()
					p.SI += 2
					p.CI += 2
					return tkn
				}
				return nil
			}},
		},
	}})
	out, err := j.Parse("a:%%")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "PCT" {
		t.Errorf("expected a:PCT, got %v", out)
	}
}

func TestMatchTokenFnForm(t *testing.T) {
	// Register the function-form matcher under #ST, which is already
	// expected in the KEY/VAL alt positions (grammar alt sets are resolved
	// statically, so brand-new tokens are not "expected" at match time).
	j := Make(Options{
		Match: &MatchOptions{
			TokenFn: map[string]LexMatcher{
				"#ST": func(lex *Lex, rule *Rule) *Token {
					fwd := lex.Fwd(3)
					if strings.HasPrefix(fwd, "@@@") {
						tkn := lex.Token("#ST", TinST, "ID!", "@@@")
						p := lex.Cursor()
						p.SI += 3
						p.CI += 3
						return tkn
					}
					return nil
				},
			},
		},
	})
	out, err := j.Parse("a:@@@")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "ID!" {
		t.Errorf("expected a:ID!, got %v", out)
	}
}

func TestMatchTokenNotExpectedSkipped(t *testing.T) {
	// A match token whose Tin is never expected at position 0 is skipped
	// (the expected=false branch of matchMatch).
	j := Make()
	mustGrammar(t, j, &GrammarSpec{
		OptionsMap: map[string]any{
			"match": map[string]any{
				"token": map[string]any{
					"#NEVER": "@/^zzz/",
				},
			},
		},
	})
	out, err := j.Parse("a:1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", out)
	}
}

// --- matchNumber edge cases ---

func TestMatchNumberEdgeCases(t *testing.T) {
	j := Make()
	tests := []struct {
		src  string
		want any
	}{
		{"a:0x", "0x"},   // hex prefix without digits → text
		{"a:0o8", "0o8"}, // octal prefix without octal digits → text
		{"a:0b2", "0b2"}, // binary prefix without binary digits → text
		{"a:0o17", float64(15)},
		{"a:0b101", float64(5)},
		{"a:0xFFg", "0xFFg"}, // trailing text after hex → text
		{"a:1e", "1e"},       // bare exponent backtracks → text? (e is following text)
		{"a:1e2", float64(100)},
		{"a:1e+2", float64(100)},
		{"a:5.", float64(5)}, // trailing dot
		{"a:.5", float64(0.5)},
		{"a:5.x", "5.x"}, // dot followed by text → text
		{"a:+1", float64(1)},
		{"a:-1", float64(-1)},
	}
	for _, tt := range tests {
		out, err := j.Parse(tt.src)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tt.src, err)
			continue
		}
		got := out.(map[string]any)["a"]
		if got != tt.want {
			t.Errorf("%q: expected %v (%T), got %v (%T)", tt.src, tt.want, tt.want, got, got)
		}
	}
}

func TestMatchNumberSignOnly(t *testing.T) {
	// A bare sign is not a number; "+" alone lexes as text.
	j := Make()
	out, err := j.Parse("a:+")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "+" {
		t.Errorf("expected '+', got %v", out)
	}
}

func TestMatchNumberSignAtEnd(t *testing.T) {
	// Standalone lexer: source ending in a sign exercises the
	// sign-at-end-of-source early return.
	lex := NewLex("-", DefaultLexConfig())
	tkn := lex.Next()
	if tkn.Tin != TinTX || tkn.Val != "-" {
		t.Errorf("expected TX '-', got %s %v", tkn.Name, tkn.Val)
	}
}

// --- matchComment: suffix terminators and eatline ---

func TestCommentBlockUnterminated(t *testing.T) {
	j := Make()
	_, err := j.Parse("a:1 /* never closed")
	if err == nil {
		t.Fatal("expected unterminated_comment error")
	}
	if je, ok := err.(*TabnasError); ok {
		if je.Code != "unterminated_comment" {
			t.Errorf("expected unterminated_comment, got %s", je.Code)
		}
	}
}

// --- Fwd ---

func TestFwd(t *testing.T) {
	lex := NewLex("abcdef", DefaultLexConfig())
	if got := lex.Fwd(3); got != "abc" {
		t.Errorf("Fwd(3) = %q", got)
	}
	if got := lex.Fwd(100); got != "abcdef" {
		t.Errorf("Fwd(100) = %q", got)
	}
	// Exhausted source → empty string.
	p := lex.Cursor()
	p.SI = p.Len
	if got := lex.Fwd(3); got != "" {
		t.Errorf("Fwd at end = %q, want empty", got)
	}
}

// --- Lex.Bad ---

func TestLexBadToken(t *testing.T) {
	lex := NewLex("x", DefaultLexConfig())
	tkn := lex.Bad("custom_reason")
	if tkn.Tin != TinBD || tkn.Why != "custom_reason" {
		t.Errorf("Bad token malformed: %+v", tkn)
	}
}
