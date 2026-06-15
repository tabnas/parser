// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// mustGrammar calls Grammar and fails the test on error.
func mustGrammar(t *testing.T, j *Tabnas, gs *GrammarSpec) {
	t.Helper()
	if err := j.Grammar(gs); err != nil {
		t.Fatal(err)
	}
}

// --- Skip sentinel ---

func TestSkipSentinel(t *testing.T) {
	if !IsSkip(Skip) {
		t.Error("IsSkip(Skip) should be true")
	}
	if IsSkip(nil) {
		t.Error("IsSkip(nil) should be false")
	}
	if IsSkip("@SKIP") {
		t.Error("IsSkip(string) should be false")
	}
}

func TestSkipInDeepMerge(t *testing.T) {
	base := map[string]any{"a": 1, "b": 2}
	over := map[string]any{"a": Skip, "b": 3}
	result := Deep(base, over).(map[string]any)

	if result["a"] != 1 {
		t.Errorf("Skip should preserve base: got a=%v", result["a"])
	}
	if result["b"] != 3 {
		t.Errorf("non-Skip should overwrite: got b=%v", result["b"])
	}
}

func TestSkipInDeepMergeArray(t *testing.T) {
	base := []any{"x", "y", "z"}
	over := []any{Skip, Skip, "w"}
	result := Deep(base, over).([]any)

	if result[0] != "x" || result[1] != "y" || result[2] != "w" {
		t.Errorf("expected [x y w], got %v", result)
	}
}

// --- ResolveFuncRefs ---

func TestResolveFuncRefsAtEscape(t *testing.T) {
	result := ResolveFuncRefs("@@myTag", nil)
	if result != "@myTag" {
		t.Errorf("expected @myTag, got %v", result)
	}
}

func TestResolveFuncRefsAtSkip(t *testing.T) {
	result := ResolveFuncRefs("@SKIP", nil)
	if !IsSkip(result) {
		t.Errorf("expected Skip sentinel, got %v", result)
	}
}

func TestResolveFuncRefsRegex(t *testing.T) {
	result := ResolveFuncRefs("@/^foo$/i", nil)
	re, ok := result.(*regexp.Regexp)
	if !ok {
		t.Fatalf("expected *regexp.Regexp, got %T", result)
	}
	if !re.MatchString("FOO") {
		t.Error("regex should match FOO (case insensitive)")
	}
	if re.MatchString("bar") {
		t.Error("regex should not match bar")
	}
}

func TestResolveFuncRefsRegexNoFlags(t *testing.T) {
	result := ResolveFuncRefs("@/^test$/", nil)
	re, ok := result.(*regexp.Regexp)
	if !ok {
		t.Fatalf("expected *regexp.Regexp, got %T", result)
	}
	if !re.MatchString("test") {
		t.Error("regex should match test")
	}
	if re.MatchString("TEST") {
		t.Error("regex without flags should not match TEST")
	}
}

func TestResolveFuncRefsFuncLookup(t *testing.T) {
	ref := map[FuncRef]any{
		"@myFunc": "hello",
	}
	result := ResolveFuncRefs("@myFunc", ref)
	if result != "hello" {
		t.Errorf("expected hello, got %v", result)
	}
}

func TestResolveFuncRefsNestedMap(t *testing.T) {
	ref := map[FuncRef]any{
		"@fn": "resolved",
	}
	input := map[string]any{
		"a": "@fn",
		"b": "@@literal",
		"c": "@SKIP",
		"d": map[string]any{"nested": "@fn"},
	}
	result := ResolveFuncRefs(input, ref).(map[string]any)

	if result["a"] != "resolved" {
		t.Errorf("a: expected resolved, got %v", result["a"])
	}
	if result["b"] != "@literal" {
		t.Errorf("b: expected @literal, got %v", result["b"])
	}
	if !IsSkip(result["c"]) {
		t.Errorf("c: expected Skip, got %v", result["c"])
	}
	nested := result["d"].(map[string]any)
	if nested["nested"] != "resolved" {
		t.Errorf("d.nested: expected resolved, got %v", nested["nested"])
	}
}

func TestResolveFuncRefsArray(t *testing.T) {
	ref := map[FuncRef]any{"@fn": 42}
	input := []any{"@fn", "@SKIP", "@@at"}
	result := ResolveFuncRefs(input, ref).([]any)

	if result[0] != 42 {
		t.Errorf("[0]: expected 42, got %v", result[0])
	}
	if !IsSkip(result[1]) {
		t.Errorf("[1]: expected Skip, got %v", result[1])
	}
	if result[2] != "@at" {
		t.Errorf("[2]: expected @at, got %v", result[2])
	}
}

// --- Grammar() method: options ---

// --- Grammar() method: rules ---

// --- Token string resolution ---

func TestResolveTokenSpecStatic(t *testing.T) {
	tests := []struct {
		input string
		want  [][]Tin
	}{
		{"#OB", [][]Tin{{TinOB}}},
		{"#ZZ", [][]Tin{{TinZZ}}},
		{"#OB #CB", [][]Tin{{TinOB}, {TinCB}}},
		{"#KEY #CL", [][]Tin{TinSetKEY, {TinCL}}},
		{"#VAL", [][]Tin{TinSetVAL}},
	}

	for _, tt := range tests {
		got := resolveTokenSpecStatic(tt.input)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("resolveTokenSpecStatic(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestResolveTokenFieldStaticSlice(t *testing.T) {
	// []string form: each element is a slot, space-separated names are alternatives.
	tests := []struct {
		input []string
		want  [][]Tin
	}{
		// Single slot with two alternatives: CB or CS
		{[]string{"#CB #CS"}, [][]Tin{{TinCB, TinCS}}},
		// Two slots: CA in slot 0, CS or ZZ in slot 1
		{[]string{"#CA", "#CS #ZZ"}, [][]Tin{{TinCA}, {TinCS, TinZZ}}},
		// Single slot with token set + individual tokens
		{[]string{"#CA #CS #VAL"}, [][]Tin{{TinCA, TinCS, TinTX, TinNR, TinST, TinVL}}},
		// Single token in single slot (equivalent to string form)
		{[]string{"#OB"}, [][]Tin{{TinOB}}},
	}

	for _, tt := range tests {
		got := resolveTokenFieldStatic(tt.input)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("resolveTokenFieldStatic(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- State action wiring ---

// --- Declarative conditions in grammar ---

// --- Fixed tokens via Grammar ---

// --- Parity fix: missing FuncRef returns error ---

func TestGrammarMissingFuncRefReturnsError(t *testing.T) {
	j := Make()
	err := j.Grammar(&GrammarSpec{
		Ref: map[FuncRef]any{},
		Rule: map[string]*GrammarRuleSpec{
			"val": {
				Close: []*GrammarAltSpec{
					{A: "@missing", G: "custom"},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing FuncRef, got nil")
	}
	if !strings.Contains(err.Error(), "@missing") {
		t.Errorf("error should mention @missing, got: %s", err)
	}
}

// --- Parity fix: inject modifiers ---

func TestGrammarInjectPrepend(t *testing.T) {
	// Default (no inject or Append:false) prepends new alts before existing ones.
	j := Make()

	mustGrammar(t, j, &GrammarSpec{
		Ref: map[FuncRef]any{
			"@noop": AltAction(func(r *Rule, ctx *Context) {}),
		},
		Rule: map[string]*GrammarRuleSpec{
			"val": {
				Close: []*GrammarAltSpec{
					{S: "#ZZ", A: "@noop", G: "first"},
				},
			},
		},
	})

	mustGrammar(t, j, &GrammarSpec{
		Ref: map[FuncRef]any{
			"@noop2": AltAction(func(r *Rule, ctx *Context) {}),
		},
		Rule: map[string]*GrammarRuleSpec{
			"val": {
				Close: []*GrammarAltSpec{
					{S: "#ZZ", A: "@noop2", G: "second"},
				},
			},
		},
	})

	valClose := j.RSM()["val"].CloseAlts()
	// Second prepend goes before first prepend.
	if valClose[0].G != "second" {
		t.Errorf("expected first alt group=second, got %q", valClose[0].G)
	}
	if valClose[1].G != "first" {
		t.Errorf("expected second alt group=first, got %q", valClose[1].G)
	}
}

// --- Parity fix: OptionsMap with FuncRef resolution ---

func TestGrammarOptionsMapSkip(t *testing.T) {
	j := Make()

	// First: set tag
	mustGrammar(t, j, &GrammarSpec{
		OptionsMap: map[string]any{
			"tag": "original",
		},
	})
	if j.Options().Tag != "original" {
		t.Fatalf("expected tag=original, got %v", j.Options().Tag)
	}

	// Second: @SKIP preserves existing tag
	mustGrammar(t, j, &GrammarSpec{
		OptionsMap: map[string]any{
			"tag": "@SKIP",
		},
	})
	if j.Options().Tag != "original" {
		t.Errorf("expected tag=original (preserved by @SKIP), got %v", j.Options().Tag)
	}
}

func TestGrammarOptionsMapAtEscape(t *testing.T) {
	j := Make()

	mustGrammar(t, j, &GrammarSpec{
		OptionsMap: map[string]any{
			"tag": "@@literal-at",
		},
	})
	if j.Options().Tag != "@literal-at" {
		t.Errorf("expected @literal-at, got %v", j.Options().Tag)
	}
}

// --- SetOptions preserves rule modifications (clone/inherit parity) ---

// === Regexp support tests (parity with TS grammar.test.js) ===

// TestGrammarRegexEscapeAtPrefix mirrors TS "options-escape-at-regex-like".
// @@ prevents @/…/ from being interpreted as a regex.
func TestGrammarRegexEscapeAtPrefix(t *testing.T) {
	j := Make()
	mustGrammar(t, j, &GrammarSpec{
		OptionsMap: map[string]any{
			"tag": "@@/not-a-regex/",
		},
	})

	if j.Options().Tag != "@/not-a-regex/" {
		t.Errorf("expected @/not-a-regex/, got %v", j.Options().Tag)
	}
}

// TestResolveFuncRefsRegexInNestedMap verifies @/…/ resolution in nested structures.
func TestResolveFuncRefsRegexInNestedMap(t *testing.T) {
	input := map[string]any{
		"number": map[string]any{
			"exclude": "@/^0[0-9]+/",
		},
		"value": map[string]any{
			"def": map[string]any{
				"on": map[string]any{
					"match": "@/^on$/i",
				},
			},
		},
	}
	result := ResolveFuncRefs(input, nil).(map[string]any)

	num := result["number"].(map[string]any)
	if _, ok := num["exclude"].(*regexp.Regexp); !ok {
		t.Errorf("number.exclude should be *regexp.Regexp, got %T", num["exclude"])
	}

	val := result["value"].(map[string]any)
	def := val["def"].(map[string]any)
	on := def["on"].(map[string]any)
	if _, ok := on["match"].(*regexp.Regexp); !ok {
		t.Errorf("value.def.on.match should be *regexp.Regexp, got %T", on["match"])
	}
}

// TestResolveFuncRefsRegexInSlice verifies @/…/ resolution inside slices.
func TestResolveFuncRefsRegexInSlice(t *testing.T) {
	input := []any{"@/^test$/i", "@SKIP", "@@at"}
	result := ResolveFuncRefs(input, nil).([]any)

	if _, ok := result[0].(*regexp.Regexp); !ok {
		t.Errorf("[0] should be *regexp.Regexp, got %T", result[0])
	}
	re := result[0].(*regexp.Regexp)
	if !re.MatchString("TEST") {
		t.Error("regex should match TEST (case insensitive)")
	}
	if !IsSkip(result[1]) {
		t.Errorf("[1] should be Skip, got %v", result[1])
	}
	if result[2] != "@at" {
		t.Errorf("[2] expected @at, got %v", result[2])
	}
}

// --- GrammarText: string grammar ---

func TestGrammarTextInvalidSource(t *testing.T) {
	// Empty string parses to nil (stub mirrors the grammar's empty-source
	// default) and is a no-op.
	withStubTextParser(t, func(string) (any, error) { return nil, nil })
	j := Make()
	if err := j.GrammarText(""); err != nil {
		t.Fatal(err)
	}
	// Without a registered parser, GrammarText reports the missing parser.
	RegisterTextParser(nil)
	if err := j.GrammarText("rule: {}"); err == nil {
		t.Fatal("expected no-parser error")
	}
}

// --- MapToOptions coverage for previously missing options ---

func TestMapToOptionsEnder(t *testing.T) {
	// Verify MapToOptions parses the ender option correctly.
	opts := MapToOptions(map[string]any{"ender": ";"})
	if len(opts.Ender) != 1 || opts.Ender[0] != ";" {
		t.Errorf("expected Ender=[;], got %v", opts.Ender)
	}
}

// --- SetOptions applies lex.empty ---

func TestSetOptionsLexEmpty(t *testing.T) {
	// lex.empty can be set via SetOptions after Make(), matching TS behavior.
	j := Make()

	// Default: empty source is allowed.
	result, err := j.Parse("")
	if err != nil {
		t.Fatalf("empty source should be allowed by default: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty source, got %v", result)
	}

	// Disable empty source via SetOptions.
	no := false
	j.SetOptions(Options{Lex: &LexOptions{Empty: &no}})

	_, err = j.Parse("")
	if err == nil {
		t.Fatal("expected error for empty source after disabling lex.empty")
	}

	// Re-enable via SetOptions.
	yes := true
	j.SetOptions(Options{Lex: &LexOptions{Empty: &yes}})

	result, err = j.Parse("")
	if err != nil {
		t.Fatalf("empty source should be allowed again: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// --- text.lex=false should not disable value keywords ---

func TestGroupTagsValidFormat(t *testing.T) {
	// All group tags (G fields) in grammar alts must be comma-separated
	// lowercase identifiers [a-z] only. No dots, spaces, or other chars.
	// Regression guard for the "elem.tabnas" typo (TS grammar.ts:737).
	j := Make()
	tagRe := regexp.MustCompile(`^[a-z]+$`)
	for name, rs := range j.RSM() {
		for _, alt := range rs.OpenAlts() {
			if alt.G != "" {
				for _, tag := range strings.Split(alt.G, ",") {
					if !tagRe.MatchString(tag) {
						t.Errorf("rule %s open: invalid group tag %q in G=%q "+
							"(must be comma-separated lowercase [a-z] identifiers)",
							name, tag, alt.G)
					}
				}
			}
		}
		for _, alt := range rs.CloseAlts() {
			if alt.G != "" {
				for _, tag := range strings.Split(alt.G, ",") {
					if !tagRe.MatchString(tag) {
						t.Errorf("rule %s close: invalid group tag %q in G=%q "+
							"(must be comma-separated lowercase [a-z] identifiers)",
							name, tag, alt.G)
					}
				}
			}
		}
	}
}

func TestGrammarLexEmpty(t *testing.T) {
	// lex.empty can be set via Grammar, not just Make().
	j := Make()
	no := false
	mustGrammar(t, j, &GrammarSpec{
		Options: &Options{Lex: &LexOptions{Empty: &no}},
	})

	_, err := j.Parse("")
	if err == nil {
		t.Fatal("expected error for empty source after Grammar sets lex.empty=false")
	}
}

// --- Info marker key protection ---
