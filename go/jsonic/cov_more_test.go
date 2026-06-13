package jsonic

import (
	"regexp"
	"strings"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestListPairMode(t *testing.T) {
	lp := true
	j := Make(tabnas.Options{List: &tabnas.ListOptions{Pair: &lp}})
	out, err := j.Parse("[a:1]")
	if err != nil {
		t.Fatal(err)
	}
	arr := out.([]any)
	if len(arr) != 1 {
		t.Fatalf("expected one element, got %v", arr)
	}
	pair := arr[0].(map[string]any)
	if pair["a"] != float64(1) {
		t.Errorf("expected [{a:1}], got %v", out)
	}
}

func TestMapChildMultipleEntries(t *testing.T) {
	mc := true
	j := Make(tabnas.Options{Map: &tabnas.MapOptions{Child: &mc}})
	out, err := j.Parse("{:1,:2,a:3}")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	// Scalar children: later overrides (Deep merge of scalars).
	if m["child$"] != float64(2) || m["a"] != float64(3) {
		t.Errorf("expected child$=2 a=3, got %v", m)
	}
}

func TestMapChildWithMerge(t *testing.T) {
	mc := true
	j := Make(tabnas.Options{
		Map: &tabnas.MapOptions{
			Child: &mc,
			Merge: func(prev, curr any, r *tabnas.Rule, ctx *tabnas.Context) any {
				pf, _ := prev.(float64)
				cf, _ := curr.(float64)
				return pf + cf
			},
		},
	})
	out, err := j.Parse("{:1,:2}")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["child$"] != float64(3) {
		t.Errorf("expected merged child$=3, got %v", m)
	}
}

func TestMapExtendFalseLastWins(t *testing.T) {
	no := false
	j := Make(tabnas.Options{Map: &tabnas.MapOptions{Extend: &no}})
	out, err := j.Parse("a:{x:1},a:{y:2}")
	if err != nil {
		t.Fatal(err)
	}
	inner := out.(map[string]any)["a"].(map[string]any)
	if inner["y"] != float64(2) {
		t.Errorf("expected last value to win, got %v", inner)
	}
	if _, exists := inner["x"]; exists {
		t.Errorf("extend=false should not merge, got %v", inner)
	}
}

func TestCommentBlockSuffixString(t *testing.T) {
	// A "\n" suffix terminates the block comment early (and covers the
	// row-advance inside the suffix consumption).
	j := Make(tabnas.Options{Comment: &tabnas.CommentOptions{Def: map[string]*tabnas.CommentDef{
		"blk":  {Start: "/*", End: "*/", Suffix: "\n"},
		"blk2": {Start: "/+", End: "+/"},
		"ln":   {Line: true, Start: "#"},
		"ln2":  {Line: true, Start: "//"},
	}}})
	out, err := j.Parse("/*comment\n8")
	if err != nil {
		t.Fatal(err)
	}
	if out != float64(8) {
		t.Errorf("expected 8 after suffix-terminated comment, got %v", out)
	}
}

func TestCommentBlockSuffixFn(t *testing.T) {
	// LexMatcher-form suffix probe on a block comment.
	fn := tabnas.LexMatcher(func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
		if strings.HasPrefix(lex.Fwd(1), "!") {
			return lex.Token("#CM", tabnas.TinCM, nil, "!")
		}
		return nil
	})
	j := Make(tabnas.Options{Comment: &tabnas.CommentOptions{Def: map[string]*tabnas.CommentDef{
		"blk": {Start: "/*", End: "*/", Suffix: fn},
	}}})
	out, err := j.Parse("/*c! 7")
	if err != nil {
		t.Fatal(err)
	}
	if out != float64(7) {
		t.Errorf("expected 7 after fn-suffix comment, got %v", out)
	}
}

func TestCommentBlockEatLine(t *testing.T) {
	el := true
	j := Make(tabnas.Options{Comment: &tabnas.CommentOptions{Def: map[string]*tabnas.CommentDef{
		"blk": {Start: "/*", End: "*/", EatLine: &el},
	}}})
	out, err := j.Parse("/*c*/\n\n9")
	if err != nil {
		t.Fatal(err)
	}
	if out != float64(9) {
		t.Errorf("expected 9 after eatline comment, got %v", out)
	}
}

func TestCustomSpaceChars(t *testing.T) {
	j := Make(tabnas.Options{Space: &tabnas.SpaceOptions{Chars: "~ "}})
	out, err := j.Parse("a:~1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected ~ treated as space, got %v", out)
	}
}

func TestCustomLineChars(t *testing.T) {
	j := Make(tabnas.Options{Line: &tabnas.LineOptions{Chars: "\r\n;", RowChars: "\n;"}})
	out, err := j.Parse("a:1;b:2")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) || m["b"] != float64(2) {
		t.Errorf("expected {a:1 b:2} via ; as line char, got %v", m)
	}
}

func TestCustomStringMultiChars(t *testing.T) {
	j := Make(tabnas.Options{String: &tabnas.StringOptions{MultiChars: `"`}})
	out, err := j.Parse("\"a\nb\"")
	if err != nil {
		t.Fatal(err)
	}
	if out != "a\nb" {
		t.Errorf("expected multiline double-quoted string, got %v", out)
	}
}

func TestCustomEscapeChar(t *testing.T) {
	j := Make(tabnas.Options{String: &tabnas.StringOptions{EscapeChar: "/"}})
	out, err := j.Parse(`"a/nb"`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a\nb" {
		t.Errorf("expected / as escape char, got %q", out)
	}
}

func TestValueDefConsume(t *testing.T) {
	lex := true
	j := Make(tabnas.Options{Value: &tabnas.ValueOptions{
		Lex: &lex,
		Def: map[string]*tabnas.ValueDef{
			// No Val / ValFunc: the matched source is the value.
			"w":    {Match: regexp.MustCompile(`^win+`), Consume: true},
			"gone": nil, // nil defs are skipped by buildConfig
		},
	}})
	out, err := j.Parse("a:winnn")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "winnn" {
		t.Errorf("expected consumed value winnn, got %v", out)
	}
}

func TestMatchTokenRegexpSuccess(t *testing.T) {
	// Register the regexp under #ST so it is expected in KEY/VAL positions.
	j := Make(tabnas.Options{Match: &tabnas.MatchOptions{
		Token: map[string]*regexp.Regexp{
			"#ST": regexp.MustCompile(`^@[a-z]+`),
		},
	}})
	out, err := j.Parse("a:@abc")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "@abc" {
		t.Errorf("expected a:@abc via match.token, got %v", out)
	}
}

func TestMatchValueNoValFn(t *testing.T) {
	// match.value without a Val transformer uses the matched source.
	j := Make(tabnas.Options{Match: &tabnas.MatchOptions{
		Value: map[string]*tabnas.MatchValueSpec{
			"pct": {Match: regexp.MustCompile(`^%[a-z]+`)},
		},
	}})
	out, err := j.Parse("a:%foo")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "%foo" {
		t.Errorf("expected a:%%foo, got %v", out)
	}
}

func TestParserMaxMulNonPositive(t *testing.T) {
	p := newGrammarParser()
	p.MaxMul = 0 // falls back to 3 in startParse
	out, err := p.Start("a:1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", out)
	}
}

func TestParserRuleStartEmptyFallback(t *testing.T) {
	p := newGrammarParser()
	p.Config.RuleStart = "" // falls back to "val"
	out, err := p.Start("a:1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", out)
	}
}

func TestWireStateActionsPrependAndWrongType(t *testing.T) {
	j := Make()
	order := []string{}
	mustGrammar(t, j, &tabnas.GrammarSpec{
		Ref: map[tabnas.FuncRef]any{
			"@val-bo": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
				order = append(order, "appended")
			}),
		},
		Rule: map[string]*tabnas.GrammarRuleSpec{"val": {}},
	})
	mustGrammar(t, j, &tabnas.GrammarSpec{
		Ref: map[tabnas.FuncRef]any{
			"@val-bo/prepend": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
				order = append(order, "prepended")
			}),
			"@val-ac": "not-a-state-action", // wrong type: silently ignored
		},
		Rule: map[string]*tabnas.GrammarRuleSpec{"val": {}},
	})

	if _, err := j.Parse("1"); err != nil {
		t.Fatal(err)
	}
	if len(order) < 2 || order[0] != "prepended" || order[1] != "appended" {
		t.Errorf("expected prepended before appended, got %v", order)
	}
}

func TestSetOptionsPreservesMatchTokens(t *testing.T) {
	j := Make(tabnas.Options{Match: &tabnas.MatchOptions{
		Token: map[string]*regexp.Regexp{
			"#ST": regexp.MustCompile(`^@[a-z]+`),
		},
		TokenFn: map[string]tabnas.LexMatcher{
			"#VL": func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token { return nil },
		},
	}})
	sep := "_"
	j.SetOptions(tabnas.Options{Number: &tabnas.NumberOptions{Sep: sep}})

	out, err := j.Parse("a:@abc")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "@abc" {
		t.Errorf("match.token lost after SetOptions: %v", out)
	}
}

func TestTrailingTokenError(t *testing.T) {
	j := Make()
	_, err := j.Parse("a:1 b")
	if err == nil {
		t.Fatal("expected error for trailing token after map pair")
	}
	if je, ok := err.(*tabnas.TabnasError); ok && je.Code != "unexpected" {
		t.Errorf("expected unexpected, got %s", je.Code)
	}
}

func TestImplicitListLeadingComma(t *testing.T) {
	j := Make()
	out, err := j.Parse(",1")
	if err != nil {
		t.Fatal(err)
	}
	arr := out.([]any)
	if len(arr) != 2 || arr[0] != nil || arr[1] != float64(1) {
		t.Errorf("expected [nil 1], got %v", arr)
	}
	// Lone comma → [nil].
	out, err = j.Parse(",")
	if err != nil {
		t.Fatal(err)
	}
	arr = out.([]any)
	if len(arr) != 1 || arr[0] != nil {
		t.Errorf("expected [nil], got %v", arr)
	}
}
