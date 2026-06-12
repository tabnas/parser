package jsonic

import (
	"strings"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestMatchValueFnForm(t *testing.T) {
	j := Make(tabnas.Options{Match: &tabnas.MatchOptions{
		Value: map[string]*tabnas.MatchValueSpec{
			"pct": {Fn: func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
				if strings.HasPrefix(lex.Fwd(2), "%%") {
					tkn := lex.Token("#VL", tabnas.TinVL, "PCT", "%%")
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
	j := Make(tabnas.Options{
		Match: &tabnas.MatchOptions{
			TokenFn: map[string]tabnas.LexMatcher{
				"#ST": func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
					fwd := lex.Fwd(3)
					if strings.HasPrefix(fwd, "@@@") {
						tkn := lex.Token("#ST", tabnas.TinST, "ID!", "@@@")
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
	mustGrammar(t, j, &tabnas.GrammarSpec{
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

func TestCommentBlockUnterminated(t *testing.T) {
	j := Make()
	_, err := j.Parse("a:1 /* never closed")
	if err == nil {
		t.Fatal("expected unterminated_comment error")
	}
	if je, ok := err.(*tabnas.TabnasError); ok {
		if je.Code != "unterminated_comment" {
			t.Errorf("expected unterminated_comment, got %s", je.Code)
		}
	}
}
