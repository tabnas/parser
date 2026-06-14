package tabnas

import (
	"errors"
	"strings"
	"testing"
)

// --- list.pair mode (TS list.pair: pairs in lists become {key:val} objects) ---

// --- map.child merging across multiple child entries ---

// --- duplicate keys: extend off and custom merge in pair/list contexts ---

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

// --- custom space / line characters ---

// --- custom string multiChars and escapeChar ---

// --- value.def: consume mode and val fallback ---

// --- match.token regexp success and match.value without Val ---

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
	// The engine is grammar-free now, so seed a small tagged rule
	// inline (previously this relied on the bundled grammar's "val").
	j := Make()
	if err := j.Grammar(&GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{
			"val": {Open: []*GrammarAltSpec{
				{S: "#NR", G: "one"},
				{S: "#TX", G: "two"},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	before := len(j.RSM()["val"].open)
	if before == 0 {
		t.Fatal("expected seeded alts")
	}
	j.include("")
	j.include(" , ")
	if len(j.RSM()["val"].open) != before {
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
	child, _ := parent.Derive()
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

// --- implicit list with leading comma (undefined first element) ---
