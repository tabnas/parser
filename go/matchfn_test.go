package tabnas

// Tests for function-form custom matchers — the LexMatcher branch of the
// TS `match.token` / `match.value` union (TS accepts RegExp | LexMatcher;
// Go splits the union into Token/TokenFn and MatchValueSpec.Match/Fn).

import (
	"regexp"
	"strings"
	"testing"
)

// fnValueMatcher matches a leading "@" followed by word characters and
// produces a #VL token carrying the name without the "@".
func fnValueMatcher(lex *Lex, _ *Rule) *Token {
	fwd := lex.Fwd(64)
	if !strings.HasPrefix(fwd, "@") {
		return nil
	}
	n := 1
	for n < len(fwd) && (fwd[n] == '_' ||
		('a' <= fwd[n] && fwd[n] <= 'z') || ('0' <= fwd[n] && fwd[n] <= '9')) {
		n++
	}
	if n == 1 {
		return nil
	}
	src := fwd[:n]
	tkn := lex.Token("#VL", TinVL, "ref:"+src[1:], src)
	pnt := lex.Cursor()
	pnt.SI += n
	pnt.CI += n
	return tkn
}

func TestMatchValueFn(t *testing.T) {
	j := Make(Options{
		Match: &MatchOptions{
			Value: map[string]*MatchValueSpec{
				"atref": {Fn: fnValueMatcher},
			},
		},
	})

	result, err := j.Parse("a:@home,b:1")
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["a"] != "ref:home" {
		t.Errorf("expected function matcher value, got %v", m["a"])
	}
	if m["b"] != float64(1) {
		t.Errorf("expected b:1, got %v", m["b"])
	}
}

func TestMatchValueFnAndRegexpCoexist(t *testing.T) {
	j := Make(Options{
		Match: &MatchOptions{
			Value: map[string]*MatchValueSpec{
				"atref": {Fn: fnValueMatcher},
				"onoff": {
					Match: regexp.MustCompile(`^on\b`),
					Val:   func(m []string) any { return true },
				},
			},
		},
	})

	result, err := j.Parse("a:@x1,b:on")
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["a"] != "ref:x1" {
		t.Errorf("function matcher failed alongside regexp, got %v", m["a"])
	}
	if m["b"] != true {
		t.Errorf("regexp matcher failed alongside function, got %v", m["b"])
	}
}

func TestMatchTokenFn(t *testing.T) {
	// Function-form token matcher for identifiers, registered as #ID and
	// allowed in KEY/VAL token sets — mirrors TestGrammarRegexMatchTokenTyped
	// but with the LexMatcher branch.
	var idTin Tin
	idMatcher := func(lex *Lex, _ *Rule) *Token {
		fwd := lex.Fwd(64)
		n := 0
		for n < len(fwd) && (fwd[n] == '_' ||
			('a' <= fwd[n] && fwd[n] <= 'z') || ('A' <= fwd[n] && fwd[n] <= 'Z') ||
			(0 < n && '0' <= fwd[n] && fwd[n] <= '9')) {
			n++
		}
		if n == 0 {
			return nil
		}
		src := fwd[:n]
		tkn := lex.Token("#ID", idTin, src, src)
		pnt := lex.Cursor()
		pnt.SI += n
		pnt.CI += n
		return tkn
	}

	j := Make()
	err := j.Grammar(&GrammarSpec{
		Options: &Options{
			Match: &MatchOptions{
				TokenFn: map[string]LexMatcher{"#ID": idMatcher},
			},
			TokenSet: map[string][]string{
				"KEY": {"#ST", "#ID"},
				"VAL": {"#TX", "#NR", "#ST", "#VL", "#ID"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	idTin = j.Token("#ID")

	result, perr := j.Parse("foo:bar")
	if perr != nil {
		t.Fatal(perr)
	}
	m := result.(map[string]any)
	if m["foo"] != "bar" {
		t.Errorf("expected foo:bar via function token matcher, got %v", m["foo"])
	}
}

func TestMatchTokenFnAtMake(t *testing.T) {
	// match.token (both forms) registers at construction, matching TS
	// where options are applied when the instance is created.
	re := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z_0-9]*`)
	j := Make(Options{
		Match: &MatchOptions{
			Token: map[string]*regexp.Regexp{"#ID": re},
		},
		TokenSet: map[string][]string{
			"KEY": {"#ST", "#ID"},
			"VAL": {"#TX", "#NR", "#ST", "#VL", "#ID"},
		},
	})

	result, err := j.Parse("a:1")
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["a"] != float64(1) {
		t.Errorf("expected a:1 with construction-time match.token, got %v", m["a"])
	}
}
