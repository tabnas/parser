package tabnas

import (
	"regexp"
	"strings"
	"testing"
)

// --- Rule.Process: H modifier, K props, dynamic PF/RF/BF, K propagation ---

func TestProcessReplaceWithKAndModifier(t *testing.T) {
	// Static R replace with K props and an H modifier:
	// "~1" → the ~ alt replaces val with a fresh val carrying K.
	j := Make()
	TT := j.Token("#TRI", "~")
	hCalled := false
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.PrependOpen(&AltSpec{
			S: [][]Tin{{TT}},
			K: map[string]any{"kk": 1},
			R: "val",
			H: func(alt *AltSpec, r *Rule, ctx *Context) *AltSpec {
				hCalled = true
				return alt
			},
		})
	})

	kPropagated := false
	j.Sub(nil, func(r *Rule, ctx *Context) {
		if r.K["kk"] == 1 && r.I > 0 {
			kPropagated = true
		}
	})

	out, err := j.Parse("~1")
	if err != nil {
		t.Fatal(err)
	}
	if out != float64(1) {
		t.Errorf("expected 1, got %v", out)
	}
	if !hCalled {
		t.Error("H modifier should have been called")
	}
	if !kPropagated {
		t.Error("K props should propagate to the replacement rule")
	}
}

func TestProcessDynamicPushAndBacktrack(t *testing.T) {
	// Dynamic PF (push name) and BF (backtrack) with K propagation to the
	// pushed child rule: "^1" → push val, child parses 1.
	j := Make()
	TP := j.Token("#PSH", "^")
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.PrependOpen(&AltSpec{
			S:  [][]Tin{{TP}},
			K:  map[string]any{"kk": 1},
			PF: func(r *Rule, ctx *Context) string { return "val" },
			BF: func(r *Rule, ctx *Context) int { return 0 },
		})
	})

	kPropagated := false
	j.Sub(nil, func(r *Rule, ctx *Context) {
		if r.K["kk"] == 1 && r.I > 0 {
			kPropagated = true
		}
	})

	out, err := j.Parse("^1")
	if err != nil {
		t.Fatal(err)
	}
	if out != float64(1) {
		t.Errorf("expected 1, got %v", out)
	}
	if !kPropagated {
		t.Error("K props should propagate to the pushed child rule")
	}
}

func TestProcessDynamicReplace(t *testing.T) {
	// Dynamic RF (replace name): "&1" → replace val with a fresh val.
	j := Make()
	TD := j.Token("#AMP", "&")
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.PrependOpen(&AltSpec{
			S:  [][]Tin{{TD}},
			RF: func(r *Rule, ctx *Context) string { return "val" },
		})
	})
	out, err := j.Parse("&1")
	if err != nil {
		t.Fatal(err)
	}
	if out != float64(1) {
		t.Errorf("expected 1, got %v", out)
	}
}

// --- grammar.go closures: map.child branches, info marker in lists, close errors ---

func TestMapChildExtendFalse(t *testing.T) {
	mc, no := true, false
	j := Make(Options{Map: &MapOptions{Child: &mc, Extend: &no}})
	out, err := j.Parse("{:1,:2}")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["child$"] != float64(2) {
		t.Errorf("extend=false child: expected 2, got %v", out)
	}
}

func TestMapChildUndefinedValue(t *testing.T) {
	mc := true
	j := Make(Options{Map: &MapOptions{Child: &mc}})
	out, err := j.Parse("{:}")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if v, ok := m["child$"]; !ok || v != nil {
		t.Errorf("expected child$=nil, got %v", m)
	}
}

func TestInfoMarkerKeyInListPair(t *testing.T) {
	// __info__ pair keys inside lists are dropped when info.map is on.
	im := true
	j := Make(Options{Info: &InfoOptions{Map: &im}})
	if _, err := j.Parse("[__info__:1]"); err != nil {
		t.Fatal(err)
	}
}

func TestListPairUndefinedValue(t *testing.T) {
	// Pair in list with empty value: child node is Undefined → nil.
	j := Make()
	if _, err := j.Parse("[a:]"); err != nil {
		t.Fatal(err)
	}
}

func TestValCloseUnbalancedAtTop(t *testing.T) {
	// CB at depth 0 triggers @val-close-err (TS val close E function).
	j := Make()
	_, err := j.Parse("1}")
	if err == nil {
		t.Fatal("expected error for unbalanced } at top level")
	}
}

func TestElemCloseErrors(t *testing.T) {
	// CB closing a list element triggers @elem-close-err.
	j := Make()
	if _, err := j.Parse("[1}"); err == nil {
		t.Error("expected error for } in list")
	}
	// CS closing a map pair triggers the pair-side @elem-close-err.
	if _, err := j.Parse("{a:1]"); err == nil {
		t.Error("expected error for ] in map")
	}
}

// --- matchNumber: overflow and trailing-text rejection per radix ---

func TestMatchNumberOverflowAndTrailing(t *testing.T) {
	j := Make()
	tests := []string{
		"0xFFFFFFFFFFFFFFFFF", // hex overflow → NaN → text
		"0o777777777777777777777777",
		"0b11111111111111111111111111111111111111111111111111111111111111111",
		"0o17z",  // trailing text after octal digits
		"0b101z", // trailing text after binary digits
		".x",     // dot not followed by digit
		"-x",     // sign without digits
		"1ex",    // exponent without digits followed by text
		"1e999",  // float overflow → NaN → text
	}
	for _, val := range tests {
		out, err := j.Parse("a:" + val)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", val, err)
			continue
		}
		got := out.(map[string]any)["a"]
		if got != val {
			t.Errorf("%q: expected text fallback, got %v (%T)", val, got, got)
		}
	}
}

// --- isTextChar / isFollowingText: string, ender, and comment-block stops ---

func TestNumberFollowedByStops(t *testing.T) {
	// Quote stops a number (isTextChar string-char branch).
	lex := NewLex(`1"a"`, DefaultLexConfig())
	if tkn := lex.Next(); tkn.Tin != TinNR || tkn.Val != float64(1) {
		t.Errorf("quote stop: expected NR 1, got %s %v", tkn.Name, tkn.Val)
	}
	// Ender char stops a number.
	cfg := DefaultLexConfig()
	cfg.EnderChars = map[rune]bool{';': true}
	lex = NewLex("1;", cfg)
	if tkn := lex.Next(); tkn.Tin != TinNR || tkn.Val != float64(1) {
		t.Errorf("ender stop: expected NR 1, got %s %v", tkn.Name, tkn.Val)
	}
	// Comment-block start stops a number (isFollowingText branch).
	lex = NewLex("1/*c*/", DefaultLexConfig())
	if tkn := lex.Next(); tkn.Tin != TinNR || tkn.Val != float64(1) {
		t.Errorf("comment stop: expected NR 1, got %s %v", tkn.Name, tkn.Val)
	}
}

// --- nextRaw: MatchCheck present but passing, then matchMatch succeeds ---

func TestMatchCheckPassThenMatchValue(t *testing.T) {
	cfg := DefaultLexConfig()
	cfg.MatchLex = true
	cfg.MatchCheck = func(lex *Lex) *LexCheckResult { return nil }
	cfg.MatchValues = []*MatchValueEntry{{
		Name:  "pct",
		Match: regexp.MustCompile(`^%[a-z]+`),
	}}
	lex := NewLex("%foo", cfg)
	tkn := lex.Next()
	if tkn.Tin != TinVL || tkn.Val != "%foo" {
		t.Errorf("expected VL %%foo, got %s %v", tkn.Name, tkn.Val)
	}
}

// --- comment marker sorting: same-length line markers, diff-length blocks ---

func TestCommentMarkerSorting(t *testing.T) {
	j := Make(Options{Comment: &CommentOptions{Def: map[string]*CommentDef{
		"hash": {Line: true, Start: "#"},
		"semi": {Line: true, Start: ";"},
		"blk":  {Start: "/*", End: "*/"},
		"html": {Start: "<!--", End: "-->"},
	}}})
	out, err := j.Parse("a:1 #x\nb:2 ;y\nc:3 /*z*/ <!-- w --> ")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) || m["b"] != float64(2) || m["c"] != float64(3) {
		t.Errorf("expected {a:1 b:2 c:3}, got %v", m)
	}
}

// --- attachHint: hint filled in for errors raised without one ---

func TestAttachHintFillsMissingHint(t *testing.T) {
	j := Make()
	j.SetOptions(Options{Parser: &ParserOptions{
		Start: func(src string, jj *Tabnas, meta map[string]any) (any, error) {
			// Hand-built error with no hint: attachHint must fill it.
			return nil, &TabnasError{Code: "unexpected"}
		},
	}})
	_, err := j.Parse("x")
	if err == nil {
		t.Fatal("expected error from custom start")
	}
	je := err.(*TabnasError)
	if je.Hint == "" {
		t.Error("attachHint should fill in the missing hint")
	}
}

// --- Describe: custom matchers section ---

func TestDescribeCustomMatchers(t *testing.T) {
	j := Make()
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"mymatcher": {Order: 100, Make: func(cfg *LexConfig, opts *Options) LexMatcher {
			return func(lex *Lex, rule *Rule) *Token { return nil }
		}},
	}}})
	desc := Describe(j)
	if !strings.Contains(desc, "Custom Matchers") || !strings.Contains(desc, "mymatcher") {
		t.Error("Describe should list custom matchers")
	}
}

// --- Omap: nil map ---

func TestOmapNilMap(t *testing.T) {
	out := Omap(nil, nil)
	if out == nil || len(out) != 0 {
		t.Errorf("Omap(nil) should return empty map, got %v", out)
	}
}
