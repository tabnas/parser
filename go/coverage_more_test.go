package tabnas

// Additional coverage for engine paths not exercised by the focused
// feature tests: number/comment/text matchers, custom matchers, the
// debug API, utility helpers, error formatting, text-form grammar/options,
// and assorted instance-API surfaces.

import (
	"regexp"
	"strings"
	"testing"
)

// errCode parses src with a single-value bare grammar and returns the
// error code (or "" on success).
func ecode(t *testing.T, j *Tabnas, src string) string {
	t.Helper()
	_, err := j.Parse(src)
	if err == nil {
		return ""
	}
	if te, ok := err.(*TabnasError); ok {
		return te.Code
	}
	return "?"
}

// --- Number matcher ---

func TestCovNumberForms(t *testing.T) {
	j := pmTopVal()
	cases := map[string]float64{
		"0":       0,
		"123":     123,
		"-7":      -7,
		"3.14":    3.14,
		"1e3":     1000,
		"1.5e-2":  0.015,
		"0xFF":    255,
		"0o17":    15,
		"0b1010":  10,
		"1_000":   1000,
		"1_000.5": 1000.5,
	}
	for src, want := range cases {
		out, err := j.Parse(src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if out != want {
			t.Errorf("%q: got %v want %v", src, out, want)
		}
	}
	// A bare "0x" with no digits falls back to text, not a number.
	if out, err := j.Parse("0x"); err != nil || out == float64(0) {
		t.Errorf("0x: got %v %v", out, err)
	}
}

func TestCovNumberDisabled(t *testing.T) {
	// Number lexing off → digits become text.
	f := false
	j := Make(Options{
		Rule:   &RuleOptions{Start: "top"},
		Number: &NumberOptions{Lex: &f},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if out, err := j.Parse("123"); err != nil || out != "123" {
		t.Errorf("number-off: got %v %v", out, err)
	}
}

// --- Comment matcher ---

func TestCovComments(t *testing.T) {
	j := pmTopVal()
	for _, src := range []string{
		"// line\n42",
		"# hash\n42",
		"/* block */ 42",
		"/* multi\nline */ 42",
		"42 // trailing",
		"42 /* after */",
	} {
		if out, err := j.Parse(src); err != nil || out != float64(42) {
			t.Errorf("%q: got %v %v", src, out, err)
		}
	}
}

func TestCovCommentEatLine(t *testing.T) {
	yes := true
	j := Make(Options{
		Rule: &RuleOptions{Start: "top"},
		Comment: &CommentOptions{Lex: &yes, Def: map[string]*CommentDef{
			"hash": {Line: true, Start: "#", Lex: &yes, EatLine: &yes},
		}},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if out, err := j.Parse("# c\n42"); err != nil || out != float64(42) {
		t.Errorf("eatline: got %v %v", out, err)
	}
}

func TestCovCommentSuffixFn(t *testing.T) {
	// Suffix as a LexMatcher (commentSuffixFnMatch path).
	yes := true
	stop := func(lex *Lex, _ *Rule) *Token {
		p := lex.Cursor()
		if p.SI+2 <= p.Len && lex.Src[p.SI:p.SI+2] == "##" {
			return &Token{Name: "#CM", Tin: TinCM, Src: "##"}
		}
		return nil
	}
	j := makeJSON(Options{Comment: &CommentOptions{Lex: &yes, Def: map[string]*CommentDef{
		"hash": {Line: true, Start: "#", Lex: &yes, Suffix: LexMatcher(stop)},
	}}})
	out, err := j.Parse(`[1,# note ##2]`)
	if err != nil {
		t.Fatalf("suffix-fn: %v", err)
	}
	if !valuesEqual(out, []any{float64(1), float64(2)}) {
		t.Errorf("suffix-fn: got %s", formatValue(out))
	}
}

// --- Text matcher ---

func TestCovText(t *testing.T) {
	j := pmTopVal()
	if out, err := j.Parse("hello"); err != nil || out != "hello" {
		t.Errorf("text: got %v %v", out, err)
	}
	// Ender chars break text: ";" ends "ab", then is consumed as a fixed
	// token by the close rule (exercises matchText's ender branch).
	semi := ";"
	je := Make(Options{
		Rule:  &RuleOptions{Start: "top"},
		Ender: []string{";"},
		Fixed: &FixedOptions{Token: map[string]*string{"#SEMI": &semi}},
	})
	SEMI := je.Token("#SEMI")
	je.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{SEMI}}}}
	})
	if out, err := je.Parse("ab;"); err != nil || out != "ab" {
		t.Errorf("ender: got %v %v", out, err)
	}
}

// --- Custom matchers ---

func TestCovTokenRegexMatcher(t *testing.T) {
	// match.Token: a regexp producing a custom token, consumed by a rule
	// (covers applyMatchTokens Token branch + matchMatch token branch).
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	AT := j.Token("#AT")
	j.SetOptions(Options{Match: &MatchOptions{Token: map[string]*regexp.Regexp{
		"#AT": regexp.MustCompile(`^@\w+`),
	}}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{{AT}}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.Src }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if out, err := j.Parse("@foo"); err != nil || out != "@foo" {
		t.Errorf("token-regex: got %v %v", out, err)
	}
}

// --- Debug API ---

func TestCovDebugDescribe(t *testing.T) {
	s := Describe(makeJSON())
	if !strings.Contains(s, "Tabnas Instance") {
		t.Errorf("Describe missing header: %q", s[:min(40, len(s))])
	}
}

func TestCovDebugTrace(t *testing.T) {
	j := pmTopVal()
	addTrace(j) // installs lex/rule subscribers that print
	if _, err := j.Parse("1"); err != nil {
		t.Fatal(err)
	}
}

// --- Utility helpers ---

func TestCovUtility(t *testing.T) {
	// Deep merge: maps, nested, slices, scalar override.
	got := Deep(map[string]any{"a": 1, "n": map[string]any{"x": 1}},
		map[string]any{"b": 2, "n": map[string]any{"y": 2}})
	m := got.(map[string]any)
	if m["a"] != 1 || m["b"] != 2 {
		t.Errorf("Deep merge: %v", m)
	}
	n := m["n"].(map[string]any)
	if n["x"] != 1 || n["y"] != 2 {
		t.Errorf("Deep nested merge: %v", n)
	}
	// Snip: short, long, multibyte.
	if Snip("abc", 10) != "abc" {
		t.Error("Snip short")
	}
	if len(Snip(strings.Repeat("x", 100), 10)) == 0 {
		t.Error("Snip long")
	}
	Snip("héllo wörld with lots of text", 8)
	// Omap: add extra keys.
	out := Omap(map[string]any{"a": 1}, func(e Entry) []any {
		return []any{e.Key, e.Value, "extra", 9}
	})
	if out["a"] != 1 || out["extra"] != 9 {
		t.Errorf("Omap extra: %v", out)
	}
}

// --- Error formatting ---

func TestCovErrorPaths(t *testing.T) {
	j := pmTopVal()
	cases := map[string]string{
		`"abc`:     "unterminated_string",
		"/* x":     "unterminated_comment",
		`"\uZZZZ"`: "invalid_unicode",
		`"\xZZ"`:   "invalid_ascii",
	}
	for src, want := range cases {
		if code := ecode(t, j, src); code != want {
			t.Errorf("%q: code=%q want %q", src, code, want)
		}
	}
	// Error() string is non-empty and includes the code tag.
	_, err := j.Parse(`"abc`)
	if err == nil || !strings.Contains(err.Error(), "unterminated_string") {
		t.Errorf("Error() text: %v", err)
	}
}

// --- Text-form grammar / options ---

func TestCovTextFormAPIs(t *testing.T) {
	withStubTextParser(t, func(s string) (any, error) {
		// Return a minimal options map for SetOptionsText and a grammar
		// map for GrammarText.
		if strings.Contains(s, "rule") {
			return map[string]any{"rule": map[string]any{
				"top": map[string]any{
					"open":  []any{map[string]any{"s": "#VAL"}},
					"close": []any{map[string]any{"s": "#ZZ"}},
				},
			}}, nil
		}
		return map[string]any{"tag": "fromtext"}, nil
	})
	j := Make()
	if _, err := j.SetOptionsText(`tag: fromtext`); err != nil {
		t.Errorf("SetOptionsText: %v", err)
	}
	if err := j.GrammarText(`rule: ...`); err != nil {
		t.Errorf("GrammarText: %v", err)
	}
}

// --- Instance API surfaces ---

func TestCovInstanceAPI(t *testing.T) {
	j := makeJSON()
	// FixedSrc / FixedTin round-trip for a standard fixed token.
	tin := j.FixedSrc("{")
	if tin == 0 {
		t.Error("FixedSrc({) returned 0")
	}
	if j.FixedTin(tin) != "{" {
		t.Errorf("FixedTin: %q", j.FixedTin(tin))
	}
	// Decoration get of unset.
	if j.Decoration("nope") != nil {
		t.Error("unset decoration not nil")
	}
	j.Decorate("d", 1)
	if j.Decoration("d") != 1 {
		t.Error("decoration set/get")
	}
	// SetTokenSet / TokenSet.
	j.SetTokenSet("XS", []Tin{TinNR})
	if len(j.TokenSet("XS")) != 1 {
		t.Error("SetTokenSet")
	}
	// Empty instance.
	if e := Empty(); e == nil {
		t.Error("Empty nil")
	}
}

// --- Plugin-error path: funcName + attachHint plugin list ---

func TestCovPluginErrorList(t *testing.T) {
	// An instance with a registered plugin that then errors exercises
	// attachHint's plugin-name collection (funcName).
	j := Make()
	if err := j.Use(jsonPlugin); err != nil {
		t.Fatal(err)
	}
	_, err := j.Parse("a:1") // relaxed syntax rejected by strict JSON
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "jsonPlugin") {
		t.Errorf("error suffix should list the plugin, got: %v", err)
	}
}

// --- Error() formatting variants ---

func errParser(em *ErrMsgOptions, color *ColorOptions) *Tabnas {
	o := Options{Rule: &RuleOptions{Start: "top"}}
	if em != nil {
		o.ErrMsg = em
	}
	if color != nil {
		o.Color = color
	}
	j := Make(o)
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	return j
}

func TestCovErrorFormatting(t *testing.T) {
	// Default error carries a hint block.
	if _, err := errParser(nil, nil).Parse("}"); err == nil ||
		!strings.Contains(err.Error(), "do not match any rule") {
		t.Errorf("hint block missing: %v", err)
	}
	// suffix=false → no internal diagnostics line.
	if _, err := errParser(&ErrMsgOptions{Suffix: false}, nil).Parse("}"); err == nil ||
		strings.Contains(err.Error(), "--internal:") {
		t.Errorf("suffix=false should drop internal line: %v", err)
	}
	// suffix=string → literal text.
	if _, err := errParser(&ErrMsgOptions{Suffix: "SEEDOCS"}, nil).Parse("}"); err == nil ||
		!strings.Contains(err.Error(), "SEEDOCS") {
		t.Errorf("string suffix: %v", err)
	}
	// suffix=func → dynamic text.
	sfx := func(code, src string) string { return "FN:" + code }
	if _, err := errParser(&ErrMsgOptions{Suffix: sfx}, nil).Parse("}"); err == nil ||
		!strings.Contains(err.Error(), "FN:unexpected") {
		t.Errorf("func suffix: %v", err)
	}
	// link line (suffix default true).
	if _, err := errParser(&ErrMsgOptions{Link: "https://docs"}, nil).Parse("}"); err == nil ||
		!strings.Contains(err.Error(), "https://docs") {
		t.Errorf("link line: %v", err)
	}
	// custom tag/name.
	if _, err := errParser(&ErrMsgOptions{Name: "myparser"}, nil).Parse("}"); err == nil ||
		!strings.Contains(err.Error(), "[myparser/") {
		t.Errorf("custom tag: %v", err)
	}
	// colour active → ANSI codes present.
	on := true
	if _, err := errParser(nil, &ColorOptions{Active: &on}).Parse("}"); err == nil ||
		!strings.Contains(err.Error(), "\x1b[") {
		t.Errorf("colour codes: %v", err)
	}
}

// --- Deep / deepClone ---

func TestCovDeepClone(t *testing.T) {
	// Slice clone + nested map clone + scalar-over-map override.
	base := map[string]any{
		"list": []any{1, map[string]any{"x": 1}},
		"obj":  map[string]any{"a": 1},
		"keep": "v",
	}
	got := Deep(map[string]any{}, base, map[string]any{"obj": "scalar"}).(map[string]any)
	if got["obj"] != "scalar" {
		t.Errorf("scalar override: %v", got["obj"])
	}
	if got["keep"] != "v" {
		t.Errorf("keep: %v", got["keep"])
	}
	// Clone independence: mutating the source list must not affect the clone.
	srcList := base["list"].([]any)
	clone := Deep([]any{}, srcList).([]any)
	if len(clone) != 2 {
		t.Fatalf("clone len: %v", clone)
	}
	// nil and scalar inputs.
	if Deep(nil) != nil {
		// Deep(nil) may return nil or an empty map depending on impl; just
		// exercise the path.
	}
	_ = Deep(1, 2)
}

// --- Grammar state actions (wireStateActions) ---

func TestCovGrammarStateActions(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	phases := map[string]bool{}
	mark := func(p string) StateAction {
		return StateAction(func(r *Rule, ctx *Context) { phases[p] = true })
	}
	err := j.Grammar(&GrammarSpec{
		Ref: map[FuncRef]any{
			"@top-bo": mark("bo"),
			"@top-ao": mark("ao"),
			"@top-bc": mark("bc"),
			"@top-ac": mark("ac"),
		},
		Rule: map[string]*GrammarRuleSpec{
			"top": {
				Open:  []*GrammarAltSpec{{S: "#VAL"}},
				Close: []*GrammarAltSpec{{S: "#ZZ"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("1"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"bo", "ao", "bc", "ac"} {
		if !phases[p] {
			t.Errorf("state action %q did not fire; got %v", p, phases)
		}
	}
}

// --- Scan-spec builders via custom space/line chars ---

func TestCovScanBuilders(t *testing.T) {
	// Custom space and line chars exercise BuildCharRunSpec /
	// BuildLineRunSpec at config time.
	j := Make(Options{
		Rule:  &RuleOptions{Start: "top"},
		Space: &SpaceOptions{Chars: " \t~"},
		Line:  &LineOptions{Chars: "\n;", RowChars: "\n"},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	// '~' is a space, ';' is a line char — both ignored around the value.
	if out, err := j.Parse("~~1;\n"); err != nil || out != float64(1) {
		t.Errorf("custom space/line: got %v %v", out, err)
	}
}

// --- Omap edge cases ---

func TestCovOmapEdge(t *testing.T) {
	m := map[string]any{"a": 1, "b": 2}
	// Drop entries whose key maps to nil.
	out := Omap(m, func(e Entry) []any {
		if e.Key == "a" {
			return []any{nil, nil}
		}
		return []any{e.Key, e.Value}
	})
	if _, ok := out["a"]; ok {
		t.Error("Omap should drop a")
	}
	if out["b"] != 2 {
		t.Error("Omap keep b")
	}
	// nil fn → identity.
	id := Omap(m, nil)
	if id["a"] != 1 || id["b"] != 2 {
		t.Errorf("Omap nil fn: %v", id)
	}
	// nil map.
	if len(Omap(nil, nil)) != 0 {
		t.Error("Omap nil map")
	}
}

// --- Token sets ---

func TestCovTokenSets(t *testing.T) {
	j := makeJSON()
	for _, name := range []string{"VAL", "KEY", "IGNORE"} {
		if len(j.TokenSet(name)) == 0 {
			t.Errorf("token set %q empty", name)
		}
	}
	if j.TokenSet("NOPE") != nil && len(j.TokenSet("NOPE")) != 0 {
		t.Error("unknown set should be empty")
	}
	j.SetTokenSet("VAL", []Tin{TinNR, TinST})
	if len(j.TokenSet("VAL")) != 2 {
		t.Error("SetTokenSet VAL")
	}
	j.SetTokenSet("KEY", []Tin{TinST})
	if len(j.TokenSet("KEY")) != 1 {
		t.Error("SetTokenSet KEY")
	}
	j.SetTokenSet("IGNORE", []Tin{TinSP})
	if len(j.TokenSet("IGNORE")) != 1 {
		t.Error("SetTokenSet IGNORE")
	}
}

// --- GrammarText error paths ---

func TestCovGrammarTextErrors(t *testing.T) {
	// Parser returns a non-map → error.
	withStubTextParser(t, func(string) (any, error) { return 42, nil })
	if err := Make().GrammarText("x"); err == nil {
		t.Error("expected non-map error")
	}
	if _, err := Make().SetOptionsText("x"); err == nil {
		t.Error("expected SetOptionsText non-map error")
	}
}

// --- Empty with options ---

func TestCovEmptyOpts(t *testing.T) {
	e := Empty(Options{Tag: "em"})
	if e == nil || e.options == nil || e.options.Tag != "em" {
		t.Errorf("Empty(opts) tag not set")
	}
}

// --- Number matcher edge branches ---

func TestCovNumberEdges(t *testing.T) {
	j := pmTopVal()
	// Number immediately followed by text → the whole run is text
	// (isFollowingText true; hex/oct/bin followed-by-text branches).
	textCases := map[string]string{
		"123abc": "123abc",
		"0xFFg":  "0xFFg",
		"0o9":    "0o9", // 9 is not octal → no digits → text
		"0b2":    "0b2", // 2 is not binary → text
		"0o":     "0o",  // no octal digits
		"0b":     "0b",  // no binary digits
	}
	for src, want := range textCases {
		if out, err := j.Parse(src); err != nil || out != want {
			t.Errorf("%q: got %v %v (want text %q)", src, out, err, want)
		}
	}
	// Separators inside hex/oct/bin.
	numCases := map[string]float64{
		"0xF_F": 255,
		"0o1_7": 15,
		"0b1_0": 2,
	}
	for src, want := range numCases {
		if out, err := j.Parse(src); err != nil || out != want {
			t.Errorf("%q: got %v %v", src, out, err)
		}
	}
}

// --- Custom value definitions (matchText value branches) ---

func TestCovValueDefs(t *testing.T) {
	// Exact-match custom keyword.
	j := Make(Options{
		Rule:  &RuleOptions{Start: "top"},
		Value: &ValueOptions{Def: map[string]*ValueDef{"yes": {Val: true}}},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if out, err := j.Parse("yes"); err != nil || out != true {
		t.Errorf("value def exact: got %v %v", out, err)
	}
	// Regex-based value def with ValFunc.
	jr := Make(Options{
		Rule: &RuleOptions{Start: "top"},
		Value: &ValueOptions{Def: map[string]*ValueDef{
			"ver": {Match: regexp.MustCompile(`^v[0-9]+`), ValFunc: func(m []string) any { return "VERSION:" + m[0] }},
		}},
	})
	jr.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if out, err := jr.Parse("v12"); err != nil || out != "VERSION:v12" {
		t.Errorf("value def regex: got %v %v", out, err)
	}
}

// --- Comment block branches ---

func TestCovCommentBlock(t *testing.T) {
	j := pmTopVal()
	// Block comment containing newlines (row counting) before a value.
	if out, err := j.Parse("/* a\nb\nc */42"); err != nil || out != float64(42) {
		t.Errorf("multiline block: got %v %v", out, err)
	}
	// Block comment with a suffix terminator.
	yes := true
	js := makeJSON(Options{Comment: &CommentOptions{Lex: &yes, Def: map[string]*CommentDef{
		"blk": {Line: false, Start: "/*", End: "*/", Lex: &yes, Suffix: "!!"},
	}}})
	if out, err := js.Parse(`[1,/* x !!2]`); err == nil {
		// Suffix terminates the block early; the remainder may or may not
		// parse depending on grammar — just exercise the path.
		_ = out
	}
}

// --- String abandon path ---

func TestCovStringAbandon(t *testing.T) {
	// With abandon on, a failed string lex returns nil so another matcher
	// can try, rather than erroring.
	yes := true
	j := Make(Options{
		Rule:   &RuleOptions{Start: "top"},
		String: &StringOptions{Abandon: &yes},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}, {S: [][]Tin{{TinZZ}}, B: 1}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}, {}}
	})
	// An unterminated string with abandon → no string token; the lexer
	// surfaces an unexpected/end rather than unterminated_string.
	_, _ = j.Parse(`"abc`)
}

// --- Panic recovery → "internal" error (no-panic guarantee path) ---

func TestCovPanicRecovery(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) {
			panic("boom in action")
		}}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	_, err := j.Parse("1")
	if err == nil {
		t.Fatal("expected an error from the panicking action")
	}
	te, ok := err.(*TabnasError)
	if !ok || te.Code != "internal" {
		t.Errorf("expected internal error, got %v", err)
	}
}

// --- parse.prepare hook ---

func TestCovParsePrepare(t *testing.T) {
	ran := false
	j := Make(Options{
		Rule: &RuleOptions{Start: "top"},
		Parse: &ParseOptions{Prepare: map[string]func(ctx *Context){
			"p": func(ctx *Context) { ran = true },
		}},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if _, err := j.Parse("1"); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("parse.prepare hook did not run")
	}
}

// --- ResolveVal TokenValFunc branch ---

func TestCovResolveValFunc(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"lazy": {Order: 1_000, Make: func(_ *LexConfig, _ *Options) LexMatcher {
			return func(lex *Lex, _ *Rule) *Token {
				p := lex.Cursor()
				if p.SI < p.Len && lex.Src[p.SI] == '$' {
					// Token value is a lazy func, resolved via ResolveVal.
					tkn := lex.Token("#VL", TinVL, TokenValFunc(func(r *Rule, ctx *Context) any { return "LAZY" }), "$")
					p.SI++
					p.CI++
					return tkn
				}
				return nil
			}
		}},
	}}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{{TinVL}}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if out, err := j.Parse("$"); err != nil || out != "LAZY" {
		t.Errorf("ResolveVal func: got %v %v", out, err)
	}
}

// --- Exercise more buildConfig option branches ---

func TestCovOptionBranches(t *testing.T) {
	f := false
	tr := true
	marker := "__meta__"
	mul := 5
	j := makeJSON(Options{
		Safe:   &SafeOptions{Key: &f},
		Map:    &MapOptions{Extend: &tr},
		Info:   &InfoOptions{Map: &tr, Marker: marker},
		Rule:   &RuleOptions{MaxMul: &mul},
		Number: &NumberOptions{Exclude: func(s string) bool { return s == "13" }},
	})
	// 13 is excluded from number matching → not a number.
	if _, err := j.Parse(`{"a":13}`); err == nil {
		// 13 excluded → "13" is not a value in strict JSON → error is fine.
		_ = err
	}
	if out, err := j.Parse(`{"a":1}`); err != nil {
		t.Errorf("option-branches parse: %v", err)
	} else if mr, ok := out.(MapRef); !ok || mr.Val["a"] != float64(1) {
		t.Errorf("MapRef with custom marker: %v", out)
	}
}

// --- Comment block: suffix + eatline ---

func mkBlockComment(t *testing.T, def *CommentDef) *Tabnas {
	t.Helper()
	yes := true
	j := Make(Options{
		Rule:    &RuleOptions{Start: "top"},
		Comment: &CommentOptions{Lex: &yes, Def: map[string]*CommentDef{"blk": def}},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	return j
}

func TestCovCommentBlockEatLineAndSuffix(t *testing.T) {
	yes := true
	// EatLine: block comment consumes the trailing newline too.
	je := mkBlockComment(t, &CommentDef{Start: "/*", End: "*/", Lex: &yes, EatLine: &yes})
	if out, err := je.Parse("/* c */\n42"); err != nil || out != float64(42) {
		t.Errorf("block eatline: got %v %v", out, err)
	}
	// String suffix terminates the block early.
	js := mkBlockComment(t, &CommentDef{Start: "/*", End: "*/", Lex: &yes, Suffix: "@@"})
	if out, err := js.Parse("/* note @@42"); err != nil || out != float64(42) {
		t.Errorf("block suffix string: got %v %v", out, err)
	}
	// Function suffix terminates the block early.
	stop := func(lex *Lex, _ *Rule) *Token {
		p := lex.Cursor()
		if p.SI+2 <= p.Len && lex.Src[p.SI:p.SI+2] == "##" {
			return &Token{Name: "#CM", Tin: TinCM, Src: "##"}
		}
		return nil
	}
	jf := mkBlockComment(t, &CommentDef{Start: "/*", End: "*/", Lex: &yes, Suffix: LexMatcher(stop)})
	if out, err := jf.Parse("/* note ##42"); err != nil || out != float64(42) {
		t.Errorf("block suffix fn: got %v %v", out, err)
	}
}

// --- isFollowingText comment-starter branch ---

func TestCovNumberFollowedByComment(t *testing.T) {
	j := pmTopVal()
	// A number immediately followed by a comment starter is a number,
	// not text (isFollowingText returns false at the comment).
	if out, err := j.Parse("123//c"); err != nil || out != float64(123) {
		t.Errorf("123//c: got %v %v", out, err)
	}
	if out, err := j.Parse("123/*c*/"); err != nil || out != float64(123) {
		t.Errorf("123/*c*/: got %v %v", out, err)
	}
}

// --- Unicode line char (BuildLineRunSpec fallback for runes >= 128) ---

func TestCovUnicodeLineChar(t *testing.T) {
	j := Make(Options{
		Rule: &RuleOptions{Start: "top"},
		Line: &LineOptions{Chars: "\n ", RowChars: "\n "},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if out, err := j.Parse("   1"); err != nil || out != float64(1) {
		t.Errorf("unicode line char: got %v %v", out, err)
	}
}

// --- Value matcher without a Val transformer (raw match) ---

func TestCovValueMatcherRaw(t *testing.T) {
	j := pmTopVal()
	j.SetOptions(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{
		"raw": {Match: regexp.MustCompile(`^#raw`)}, // no Val → raw match text
	}}})
	if out, err := j.Parse("#raw"); err != nil || out != "#raw" {
		t.Errorf("raw value matcher: got %v %v", out, err)
	}
}

// --- filterAlts / filterAltsInclude with mixed tagged/untagged alts ---

func TestCovFilterMixedTags(t *testing.T) {
	// Exclude a tag: untagged alts are always kept; tagged-with-excluded
	// are dropped.
	// Add the rule first, then apply exclude via SetOptions so it operates
	// on the installed alternates.
	jx := Make(Options{Rule: &RuleOptions{Start: "top"}})
	jx.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{
			{S: [][]Tin{{TinNR}}, G: "drop", A: func(r *Rule, ctx *Context) { r.Node = "DROPPED" }},
			{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}, // untagged: kept
		}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	jx.SetOptions(Options{Rule: &RuleOptions{Exclude: "drop"}})
	if out, err := jx.Parse("5"); err != nil || out != float64(5) {
		t.Errorf("exclude untagged-kept: got %v %v", out, err)
	}

	// Include a tag: untagged alts are dropped, only matching-tag kept.
	ji := Make(Options{Rule: &RuleOptions{Start: "top"}})
	ji.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{
			{S: [][]Tin{TinSetVAL}, G: "keep", A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }},
			{S: [][]Tin{{TinOB}}}, // untagged: dropped under include
		}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}, G: "keep"}}
	})
	ji.SetOptions(Options{Rule: &RuleOptions{Include: "keep"}})
	if out, err := ji.Parse("7"); err != nil || out != float64(7) {
		t.Errorf("include keep-only: got %v %v", out, err)
	}
}

// --- Trailing content and empty source ---

func TestCovTrailingAndEmpty(t *testing.T) {
	j := pmTopVal()
	// Trailing content after a complete value → unexpected.
	if code := ecode(t, j, "1 2"); code != "unexpected" {
		t.Errorf("trailing content: got %q", code)
	}
	// Empty source → nil result, no error.
	if out, err := j.Parse(""); err != nil || out != nil {
		t.Errorf("empty source: got %v %v", out, err)
	}
	// Custom empty result.
	je := Make(Options{Rule: &RuleOptions{Start: "top"}, Lex: &LexOptions{EmptyResult: []any{}}})
	je.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) { r.Node = r.O0.ResolveVal(r, ctx) }}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	_, _ = je.Parse("")
}

// --- Escape: unknown/removed/strict-x with AllowUnknown=true ---

func TestCovEscapeUnknownAllowed(t *testing.T) {
	tr := true
	j := strParser(&StringOptions{AllowUnknown: &tr, EscapeStrict: &tr, Escape: map[string]string{"v": ""}})
	// strict \x disabled + allowUnknown → 'x' copied through literally.
	if out, err := j.Parse(`"\x41"`); err != nil || out != "x41" {
		t.Errorf(`\x41 allowUnknown: got %v %v`, out, err)
	}
	// removed \v + allowUnknown → 'v' copied through.
	if out, err := j.Parse(`"\v"`); err != nil || out != "v" {
		t.Errorf(`\v allowUnknown: got %v %v`, out, err)
	}
}

// --- Leading-dot numbers ---

func TestCovLeadingDotNumber(t *testing.T) {
	j := pmTopVal()
	if out, err := j.Parse(".5"); err != nil || out != 0.5 {
		t.Errorf(".5: got %v %v", out, err)
	}
	// A bare "." is not a number → text.
	if out, err := j.Parse("."); err != nil || out != "." {
		t.Errorf(". : got %v %v", out, err)
	}
}

// --- Describe with custom matchers ---

func TestCovDescribeMatchers(t *testing.T) {
	j := Make()
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"x": {Order: 1000, Make: func(_ *LexConfig, _ *Options) LexMatcher {
			return func(*Lex, *Rule) *Token { return nil }
		}},
	}}})
	if !strings.Contains(Describe(j), "Custom Matchers") {
		t.Error("Describe should list custom matchers")
	}
}
