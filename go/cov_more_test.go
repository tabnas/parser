package tabnas

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// --- list.pair mode (TS list.pair: pairs in lists become {key:val} objects) ---

func TestListPairMode(t *testing.T) {
	lp := true
	j := Make(Options{List: &ListOptions{Pair: &lp}})
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

// --- map.child merging across multiple child entries ---

func TestMapChildMultipleEntries(t *testing.T) {
	mc := true
	j := Make(Options{Map: &MapOptions{Child: &mc}})
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
	j := Make(Options{
		Map: &MapOptions{
			Child: &mc,
			Merge: func(prev, curr any, r *Rule, ctx *Context) any {
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

// --- duplicate keys: extend off and custom merge in pair/list contexts ---

func TestMapExtendFalseLastWins(t *testing.T) {
	no := false
	j := Make(Options{Map: &MapOptions{Extend: &no}})
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

func TestListPropertyDuplicateKeys(t *testing.T) {
	// Pairs inside a list (list.property) with duplicate keys exercise
	// the elem-bc-pair prev/merge branches.
	j := Make()
	if _, err := j.Parse("[a:1,a:2]"); err != nil {
		t.Fatal(err)
	}
	// With a custom merge function.
	j2 := Make(Options{Map: &MapOptions{
		Merge: func(prev, curr any, r *Rule, ctx *Context) any { return curr },
	}})
	if _, err := j2.Parse("[a:1,a:2]"); err != nil {
		t.Fatal(err)
	}
	// With extend disabled.
	no := false
	j3 := Make(Options{Map: &MapOptions{Extend: &no}})
	if _, err := j3.Parse("[a:1,a:2]"); err != nil {
		t.Fatal(err)
	}
}

func TestListProtoKeySafe(t *testing.T) {
	// __proto__ keys in list-property pairs are skipped by safe.key.
	j := Make()
	if _, err := j.Parse("[__proto__:1,a:2]"); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("[constructor:1]"); err != nil {
		t.Fatal(err)
	}
}

// --- comment block suffix terminators (string, fn) and eatline ---

func TestCommentBlockSuffixString(t *testing.T) {
	// A "\n" suffix terminates the block comment early (and covers the
	// row-advance inside the suffix consumption).
	j := Make(Options{Comment: &CommentOptions{Def: map[string]*CommentDef{
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
	fn := LexMatcher(func(lex *Lex, rule *Rule) *Token {
		if strings.HasPrefix(lex.Fwd(1), "!") {
			return lex.Token("#CM", TinCM, nil, "!")
		}
		return nil
	})
	j := Make(Options{Comment: &CommentOptions{Def: map[string]*CommentDef{
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
	j := Make(Options{Comment: &CommentOptions{Def: map[string]*CommentDef{
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

// --- custom space / line characters ---

func TestCustomSpaceChars(t *testing.T) {
	j := Make(Options{Space: &SpaceOptions{Chars: "~ "}})
	out, err := j.Parse("a:~1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected ~ treated as space, got %v", out)
	}
}

func TestCustomLineChars(t *testing.T) {
	j := Make(Options{Line: &LineOptions{Chars: "\r\n;", RowChars: "\n;"}})
	out, err := j.Parse("a:1;b:2")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) || m["b"] != float64(2) {
		t.Errorf("expected {a:1 b:2} via ; as line char, got %v", m)
	}
}

// --- custom string multiChars and escapeChar ---

func TestCustomStringMultiChars(t *testing.T) {
	j := Make(Options{String: &StringOptions{MultiChars: `"`}})
	out, err := j.Parse("\"a\nb\"")
	if err != nil {
		t.Fatal(err)
	}
	if out != "a\nb" {
		t.Errorf("expected multiline double-quoted string, got %v", out)
	}
}

func TestCustomEscapeChar(t *testing.T) {
	j := Make(Options{String: &StringOptions{EscapeChar: "/"}})
	out, err := j.Parse(`"a/nb"`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a\nb" {
		t.Errorf("expected / as escape char, got %q", out)
	}
}

// --- value.def: consume mode and val fallback ---

func TestValueDefConsume(t *testing.T) {
	lex := true
	j := Make(Options{Value: &ValueOptions{
		Lex: &lex,
		Def: map[string]*ValueDef{
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

// --- match.token regexp success and match.value without Val ---

func TestMatchTokenRegexpSuccess(t *testing.T) {
	// Register the regexp under #ST so it is expected in KEY/VAL positions.
	j := Make(Options{Match: &MatchOptions{
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
	j := Make(Options{Match: &MatchOptions{
		Value: map[string]*MatchValueSpec{
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

// --- matcher end-of-source guards (defensive, called directly) ---

func TestMatcherEndOfSourceGuards(t *testing.T) {
	lex := NewLex("x", DefaultLexConfig())
	p := lex.Cursor()
	p.SI = p.Len
	if lex.matchMatch(nil) != nil {
		t.Error("matchMatch at end should be nil")
	}
	if lex.matchFixed() != nil {
		t.Error("matchFixed at end should be nil")
	}
	if lex.matchString() != nil {
		t.Error("matchString at end should be nil")
	}
	if lex.matchNumber() != nil {
		t.Error("matchNumber at end should be nil")
	}
	if lex.matchText() != nil {
		t.Error("matchText at end should be nil")
	}
}

// --- custom matchers returning tokens in each priority band ---

func TestCustomMatcherReturnsPerBand(t *testing.T) {
	bands := []int{500000, 1500000, 2500000, 3500000, 4500000, 5500000, 6500000, 7500000}
	for _, prio := range bands {
		cfg := DefaultLexConfig()
		cfg.MatchLex = true
		cfg.CustomMatchers = []*MatcherEntry{{
			Name:     "claim",
			Priority: prio,
			Match: func(lex *Lex, rule *Rule) *Token {
				tkn := lex.Token("#VL", TinVL, "claimed", "x")
				p := lex.Cursor()
				p.SI++
				p.CI++
				return tkn
			},
		}}
		lex := NewLex("x", cfg)
		tkn := lex.Next()
		if tkn.Val != "claimed" {
			t.Errorf("priority %d: expected claimed, got %v (%s)", prio, tkn.Val, tkn.Name)
		}
	}

	// >= 8e6 band: a nil matcher followed by a matching one (covers loop
	// continuation in the final band).
	cfg := DefaultLexConfig()
	cfg.TextLex = false
	cfg.ValueLex = false
	cfg.CustomMatchers = []*MatcherEntry{
		{Name: "no", Priority: 8500000, Match: func(lex *Lex, rule *Rule) *Token { return nil }},
		{Name: "yes", Priority: 9000000, Match: func(lex *Lex, rule *Rule) *Token {
			tkn := lex.Token("#VL", TinVL, "fin", "q")
			p := lex.Cursor()
			p.SI++
			p.CI++
			return tkn
		}},
	}
	lex := NewLex("q", cfg)
	if tkn := lex.Next(); tkn.Val != "fin" {
		t.Errorf("expected fin from final band, got %v", tkn.Val)
	}
}

// --- include: empty group set is a no-op ---

func TestIncludeEmptyGroups(t *testing.T) {
	j := Make()
	before := len(j.RSM()["val"].Open)
	j.include("")
	j.include(" , ")
	if len(j.RSM()["val"].Open) != before {
		t.Error("empty include set should not filter alts")
	}
}

// --- MaxMul handling ---

func TestMakeRuleMaxMulOption(t *testing.T) {
	mm := 5
	j := Make(Options{Rule: &RuleOptions{MaxMul: &mm}})
	if _, err := j.Parse("a:1"); err != nil {
		t.Fatal(err)
	}
}

func TestParserMaxMulNonPositive(t *testing.T) {
	p := NewParser()
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
	p := NewParser()
	p.Config.RuleStart = "" // falls back to "val"
	out, err := p.Start("a:1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", out)
	}
}

// --- finishErr pass-through ---

func TestFinishErrPassThrough(t *testing.T) {
	p := NewParser()
	plain := errors.New("plain")
	if got := p.finishErr(plain, nil, nil, nil); got != plain {
		t.Error("non-TabnasError should pass through finishErr")
	}
}

// --- errsite clamps and context lines ---

func TestErrsiteClampsAndContext(t *testing.T) {
	color := ColorConfig{} // inactive: empty codes

	// row/col below 1 are clamped; empty sub still gets one caret.
	out := errsite("line1\nline2", "", "msg", 0, 0, color)
	if !strings.Contains(out, "^ msg") {
		t.Errorf("expected single caret, got %q", out)
	}

	// row beyond the source clamps to the last line.
	out = errsite("only", "x", "m", 99, 1, color)
	if !strings.Contains(out, "only") {
		t.Errorf("expected last line shown, got %q", out)
	}

	// Lines before and after the error line are included.
	out = errsite("l1\nl2\nl3\nl4\nl5", "x", "m", 3, 1, color)
	for _, want := range []string{"l1", "l2", "l3", "l4", "l5"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected context line %s in %q", want, out)
		}
	}
}

// --- makeTabnasError unknown code fallbacks ---

func TestMakeTabnasErrorUnknownCode(t *testing.T) {
	// Unknown code with default messages uses the "unknown" template.
	je := makeTabnasError("no_such_code", "x", "x", 0, 1, 1, nil)
	if je.Detail == "" {
		t.Error("expected non-empty detail from unknown template")
	}

	// Config message map without an "unknown" entry falls back to the
	// package-level default.
	cfg := &LexConfig{ErrorMessages: map[string]string{}}
	je = makeTabnasError("no_such_code", "x", "x", 0, 1, 1, cfg)
	if je.Detail == "" {
		t.Error("expected fallback to package-level unknown template")
	}
}

// --- preprocessEscapes ---

func TestPreprocessEscapes(t *testing.T) {
	if preprocessEscapes("") != "" {
		t.Error("empty passthrough")
	}
	got := preprocessEscapes(`a\nb\rc\td\qe\`)
	if got != "a\nb\rc\td\\qe\\" {
		t.Errorf("escape processing failed: %q", got)
	}
}

// --- Deep with struct values (deepMerge struct branch) ---

func TestDeepWithStructs(t *testing.T) {
	out := Deep(covInner{A: "x"}, covInner{A: "y"})
	if out.(covInner).A != "y" {
		t.Errorf("expected struct merge via Deep, got %v", out)
	}
}

// --- deepClone of ListRef / MapRef values ---

func TestDeepCloneRefWrappers(t *testing.T) {
	over := map[string]any{
		"l": ListRef{Val: []any{1}, Child: "c", Meta: map[string]any{"m": 1}},
		"m": MapRef{Val: map[string]any{"a": 1}, Meta: map[string]any{"m": 2}},
	}
	out := Deep(map[string]any{}, over).(map[string]any)
	lr := out["l"].(ListRef)
	if len(lr.Val) != 1 || lr.Child != "c" || lr.Meta["m"] != 1 {
		t.Errorf("ListRef clone failed: %+v", lr)
	}
	mr := out["m"].(MapRef)
	if mr.Val["a"] != 1 || mr.Meta["m"] != 2 {
		t.Errorf("MapRef clone failed: %+v", mr)
	}
}

// --- formatCompactValue: multi-entry map comma separator ---

func TestFormatCompactValueMultiKeyMap(t *testing.T) {
	got := formatCompactValue(map[string]any{"a": float64(1), "b": float64(2)})
	if !strings.Contains(got, ",") || !strings.Contains(got, "a:1") || !strings.Contains(got, "b:2") {
		t.Errorf("multi-key compact format failed: %q", got)
	}
}

// --- ResolveFuncRefs corner cases ---

func TestResolveFuncRefsCorners(t *testing.T) {
	if ResolveFuncRefs(nil, nil) != nil {
		t.Error("nil passthrough")
	}
	// Unknown @ref with a non-nil ref map returns the original string.
	ref := map[FuncRef]any{"@known": 1}
	if got := ResolveFuncRefs("@unknown", ref); got != "@unknown" {
		t.Errorf("unknown ref should pass through, got %v", got)
	}
	// Invalid regex pattern passes through as the original string.
	if got := ResolveFuncRefs("@/[unclosed/", nil); got != "@/[unclosed/" {
		t.Errorf("invalid regex should pass through, got %v", got)
	}
}

// --- wireStateActions: /prepend variant and non-StateAction refs ---

func TestWireStateActionsPrependAndWrongType(t *testing.T) {
	j := Make()
	order := []string{}
	mustGrammar(t, j, &GrammarSpec{
		Ref: map[FuncRef]any{
			"@val-bo": StateAction(func(r *Rule, ctx *Context) {
				order = append(order, "appended")
			}),
		},
		Rule: map[string]*GrammarRuleSpec{"val": {}},
	})
	mustGrammar(t, j, &GrammarSpec{
		Ref: map[FuncRef]any{
			"@val-bo/prepend": StateAction(func(r *Rule, ctx *Context) {
				order = append(order, "prepended")
			}),
			"@val-ac": "not-a-state-action", // wrong type: silently ignored
		},
		Rule: map[string]*GrammarRuleSpec{"val": {}},
	})

	if _, err := j.Parse("1"); err != nil {
		t.Fatal(err)
	}
	if len(order) < 2 || order[0] != "prepended" || order[1] != "appended" {
		t.Errorf("expected prepended before appended, got %v", order)
	}
}

// --- mapToGrammarRules: non-map rule values skipped ---

func TestMapToGrammarRulesNonMapSkipped(t *testing.T) {
	rules := mapToGrammarRules(map[string]any{
		"good": map[string]any{"open": []any{}},
		"bad":  "not-a-map",
	})
	if _, ok := rules["good"]; !ok {
		t.Error("good rule should be present")
	}
	if _, ok := rules["bad"]; ok {
		t.Error("non-map rule value should be skipped")
	}
}

// --- resolveTokenSpec: whitespace-only specs ---

func TestResolveTokenSpecWhitespaceOnly(t *testing.T) {
	j := Make()
	if got := j.resolveTokenField(" "); got != nil {
		t.Errorf("whitespace-only spec → nil, got %v", got)
	}
	if got := resolveTokenFieldStatic(" "); got != nil {
		t.Errorf("whitespace-only static spec → nil, got %v", got)
	}
}

// --- Derive copies custom matchers ---

func TestDeriveCopiesCustomMatchers(t *testing.T) {
	parent := Make()
	parent.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"pm": {Order: 100, Make: func(cfg *LexConfig, opts *Options) LexMatcher {
			return func(lex *Lex, rule *Rule) *Token { return nil }
		}},
	}}})
	child := parent.Derive()
	found := false
	for _, m := range child.Config().CustomMatchers {
		if m.Name == "pm" {
			found = true
		}
	}
	if !found {
		t.Error("child should inherit parent's custom matchers")
	}
}

// --- SetOptions preserves match token regexps and fns ---

func TestSetOptionsPreservesMatchTokens(t *testing.T) {
	j := Make(Options{Match: &MatchOptions{
		Token: map[string]*regexp.Regexp{
			"#ST": regexp.MustCompile(`^@[a-z]+`),
		},
		TokenFn: map[string]LexMatcher{
			"#VL": func(lex *Lex, rule *Rule) *Token { return nil },
		},
	}})
	sep := "_"
	j.SetOptions(Options{Number: &NumberOptions{Sep: sep}})

	out, err := j.Parse("a:@abc")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != "@abc" {
		t.Errorf("match.token lost after SetOptions: %v", out)
	}
}

// --- registerMatchSpecs: multiple matchers sorted by priority ---

func TestRegisterMatchSpecsSorted(t *testing.T) {
	j := Make()
	mk := func(cfg *LexConfig, opts *Options) LexMatcher {
		return func(lex *Lex, rule *Rule) *Token { return nil }
	}
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"later":   {Order: 9000000, Make: mk},
		"earlier": {Order: 100, Make: mk},
	}}})
	ms := j.Config().CustomMatchers
	if len(ms) < 2 {
		t.Fatalf("expected 2 matchers, got %d", len(ms))
	}
	for i := 1; i < len(ms); i++ {
		if ms[i-1].Priority > ms[i].Priority {
			t.Errorf("matchers not sorted by priority: %v > %v", ms[i-1].Priority, ms[i].Priority)
		}
	}
}

// --- Token: nil FixedTokens map (defensive re-init) ---

func TestTokenNilFixedTokens(t *testing.T) {
	j := Make()
	j.Config().FixedTokens = nil
	if tin := j.Token("#CL", ";"); tin != TinCL {
		t.Errorf("existing token with nil FixedTokens: got %d", tin)
	}
	j2 := Make()
	j2.Config().FixedTokens = nil
	if tin := j2.Token("#CARET", "^"); tin == 0 {
		t.Error("new token with nil FixedTokens should allocate")
	}
}

// --- trailing token error at depth 0 (val-close-err) ---

func TestTrailingTokenError(t *testing.T) {
	j := Make()
	_, err := j.Parse("a:1 b")
	if err == nil {
		t.Fatal("expected error for trailing token after map pair")
	}
	if je, ok := err.(*TabnasError); ok && je.Code != "unexpected" {
		t.Errorf("expected unexpected, got %s", je.Code)
	}
}

// --- implicit list with leading comma (undefined first element) ---

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
