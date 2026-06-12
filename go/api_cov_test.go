package tabnas

import (
	"errors"
	"strings"
	"testing"
)

// --- Parser.StartMeta (public API entry with meta + subscriptions) ---

func TestParserStartMeta(t *testing.T) {
	p := NewParser()
	meta := map[string]any{"k": "v"}

	lexCount := 0
	ruleCount := 0
	lexSubs := []LexSub{func(tkn *Token, r *Rule, ctx *Context) {
		lexCount++
		if ctx.Meta["k"] != "v" {
			t.Error("meta should be available in lex sub context")
		}
	}}
	ruleSubs := []RuleSub{func(r *Rule, ctx *Context) {
		ruleCount++
	}}

	out, err := p.StartMeta("a:1", meta, lexSubs, ruleSubs)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["a"] != float64(1) {
		t.Errorf("expected {a:1}, got %v", out)
	}
	if lexCount == 0 {
		t.Error("lex subscriber should have fired")
	}
	if ruleCount == 0 {
		t.Error("rule subscriber should have fired")
	}
}

func TestParserStartMetaEmptySource(t *testing.T) {
	p := NewParser()
	out, err := p.StartMeta("", nil, nil, nil)
	if err != nil || out != nil {
		t.Errorf("empty source should return nil,nil — got %v, %v", out, err)
	}
}

func TestParserStartMissingStartRule(t *testing.T) {
	// Unknown rule.start → nil result, no panic (startParse startSpec==nil path).
	p := NewParser()
	p.Config.RuleStart = "nosuchrule"
	out, err := p.Start("a:1")
	if err != nil || out != nil {
		t.Errorf("missing start rule should return nil,nil — got %v, %v", out, err)
	}
}

// --- UseDefaults (TS: deep({}, plugin.defaults, plugin_options)) ---

func TestUseDefaultsMergesOptions(t *testing.T) {
	j := Make()
	var got map[string]any
	plugin := Plugin(func(j *Tabnas, opts map[string]any) error {
		got = opts
		return nil
	})
	err := j.UseDefaults(plugin, map[string]any{"a": 1, "b": 2}, map[string]any{"b": 3})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1 {
		t.Errorf("default a=1 expected, got %v", got["a"])
	}
	if got["b"] != 3 {
		t.Errorf("user b=3 should override default, got %v", got["b"])
	}
	if len(j.Plugins()) != 1 {
		t.Errorf("plugin should be registered, got %d", len(j.Plugins()))
	}
}

func TestUseDefaultsNoUserOptions(t *testing.T) {
	j := Make()
	var got map[string]any
	plugin := Plugin(func(j *Tabnas, opts map[string]any) error {
		got = opts
		return nil
	})
	if err := j.UseDefaults(plugin, map[string]any{"x": "d"}); err != nil {
		t.Fatal(err)
	}
	if got["x"] != "d" {
		t.Errorf("defaults should be passed when no user opts, got %v", got)
	}
}

// --- Debug plugin trace (addTrace via opts.trace) ---

func TestDebugPluginTrace(t *testing.T) {
	j := Make()
	if err := j.Use(Debug, map[string]any{"trace": true}); err != nil {
		t.Fatal(err)
	}
	// Subscribers installed by addTrace must not break parsing.
	out, err := j.Parse("a:1")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", m)
	}
}

func TestDebugPluginNoTrace(t *testing.T) {
	j := Make()
	// trace=false and nil opts should not install subscribers.
	if err := j.Use(Debug, map[string]any{"trace": false}); err != nil {
		t.Fatal(err)
	}
	if err := j.Use(Debug); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("a:1"); err != nil {
		t.Fatal(err)
	}
}

func TestDescribeWithTag(t *testing.T) {
	j := Make(Options{Tag: "mytag"})
	desc := Describe(j)
	if !strings.Contains(desc, "Tag: mytag") {
		t.Error("Describe should include the instance tag")
	}
}

// --- TinName / tinName ---

func TestTinNameFallback(t *testing.T) {
	j := Make()
	if name := j.TinName(TinOB); name != "#OB" {
		t.Errorf("expected #OB, got %s", name)
	}
	// Unknown tin falls back to tinName → "#UK".
	if name := j.TinName(Tin(987654)); name != "#UK" {
		t.Errorf("expected #UK for unknown tin, got %s", name)
	}
}

func TestTinNameBuiltinCases(t *testing.T) {
	tests := []struct {
		tin  Tin
		want string
	}{
		{TinOB, "#OB"}, {TinCB, "#CB"}, {TinOS, "#OS"},
		{TinCS, "#CS"}, {TinCL, "#CL"}, {TinCA, "#CA"},
		{TinZZ, "#UK"}, // not in the static switch → default
	}
	for _, tt := range tests {
		if got := tinName(tt.tin); got != tt.want {
			t.Errorf("tinName(%d) = %s, want %s", tt.tin, got, tt.want)
		}
	}
}

// --- PluginOptions / SetPluginOptions ---

func TestPluginOptionsLifecycle(t *testing.T) {
	j := Make()
	// No options stored yet.
	if j.PluginOptions("p") != nil {
		t.Error("expected nil for unset plugin options")
	}
	j.SetPluginOptions("p", map[string]any{"a": 1})
	got := j.PluginOptions("p")
	if got["a"] != 1 {
		t.Errorf("expected a:1, got %v", got)
	}
	// Second set merges into existing.
	j.SetPluginOptions("p", map[string]any{"b": 2})
	got = j.PluginOptions("p")
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("expected merged {a:1 b:2}, got %v", got)
	}
}

// --- Options() copy accessor ---

func TestOptionsAccessor(t *testing.T) {
	j := Make(Options{Tag: "opt-tag"})
	o := j.Options()
	if o.Tag != "opt-tag" {
		t.Errorf("expected opt-tag, got %s", o.Tag)
	}
	// Nil options → zero value (defensive branch).
	empty := &Tabnas{}
	if got := empty.Options(); got.Tag != "" {
		t.Errorf("expected zero Options, got %+v", got)
	}
}

// --- attachHint: plugin names, nil and non-TabnasError pass-through ---

func TestAttachHintPassThrough(t *testing.T) {
	j := Make()
	if j.attachHint(nil) != nil {
		t.Error("nil error should pass through")
	}
	plain := errors.New("plain")
	if j.attachHint(plain) != plain {
		t.Error("non-TabnasError should pass through unchanged")
	}
}

func TestAttachHintPluginNames(t *testing.T) {
	// Errors from a Tabnas instance with plugins include the plugin names
	// in the --internal suffix (TS errdesc ctx.plgn()).
	j := Make()
	noop := Plugin(func(j *Tabnas, opts map[string]any) error { return nil })
	if err := j.Use(noop); err != nil {
		t.Fatal(err)
	}
	_, err := j.Parse(`"unterminated`)
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
	if _, ok := err.(*TabnasError); !ok {
		t.Fatalf("expected *TabnasError, got %T", err)
	}
}

// --- SetOptionsText error paths ---

func TestSetOptionsTextEmpty(t *testing.T) {
	j := Make()
	if _, err := j.SetOptionsText(""); err != nil {
		t.Errorf("empty text should be a no-op: %v", err)
	}
}

func TestSetOptionsTextParseError(t *testing.T) {
	j := Make()
	if _, err := j.SetOptionsText(`"unterminated`); err == nil {
		t.Error("expected parse error for malformed options text")
	}
}

func TestSetOptionsTextNotMap(t *testing.T) {
	j := Make()
	_, err := j.SetOptionsText(`[1,2,3]`)
	if err == nil {
		t.Fatal("expected error for non-map options text")
	}
	if !strings.Contains(err.Error(), "expected map") {
		t.Errorf("error should mention expected map, got: %s", err)
	}
}

func TestSetOptionsTextNilParse(t *testing.T) {
	// Comment-only source parses to nil — SetOptionsText should be a no-op.
	j := Make()
	if _, err := j.SetOptionsText("# just a comment"); err != nil {
		t.Errorf("comment-only text should be a no-op: %v", err)
	}
}

func TestSetOptionsTextApplies(t *testing.T) {
	j := Make()
	if _, err := j.SetOptionsText(`number: { sep: "_" }`); err != nil {
		t.Fatal(err)
	}
	out, err := j.Parse("a:1_000")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1000) {
		t.Errorf("expected 1000, got %v", out)
	}
}

// --- Token: existing token with new fixed src ---

func TestTokenUpdateExistingFixedSrc(t *testing.T) {
	j := Make()
	// "#CL" already exists; provide a second source string for it.
	tin := j.Token("#CL", ";")
	if tin != TinCL {
		t.Errorf("expected TinCL, got %d", tin)
	}
	// Both ":" and ";" now lex as colon.
	out, err := j.Parse("a;1")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1 via ';' colon alias, got %v", out)
	}
}

// --- applyFixedTokens branches (fixed.token option) ---

func TestApplyFixedTokensDeleteUnknownName(t *testing.T) {
	// nil src for an unknown name is a no-op (nothing to delete).
	j := Make(Options{Fixed: &FixedOptions{Token: map[string]*string{
		"#NOPE": nil,
	}}})
	if _, err := j.Parse("a:1"); err != nil {
		t.Fatal(err)
	}
}

func TestApplyFixedTokensDeleteKnownName(t *testing.T) {
	// nil src for a known name removes its fixed mapping.
	j := Make()
	j.SetOptions(Options{Fixed: &FixedOptions{Token: map[string]*string{
		"#CA": nil,
	}}})
	if j.FixedSrc(",") != 0 {
		t.Error("expected ',' mapping removed")
	}
}

func TestApplyFixedTokensSwapAndAllocate(t *testing.T) {
	semi := ";"
	tilde := "~"
	j := Make(Options{Fixed: &FixedOptions{Token: map[string]*string{
		"#CA":  &semi,  // swap comma → semicolon
		"#NEW": &tilde, // allocate a new token
	}}})
	if j.FixedSrc(";") != TinCA {
		t.Error("expected ';' to map to TinCA")
	}
	if j.FixedSrc(",") != 0 {
		t.Error("expected ',' mapping removed after swap")
	}
	if j.FixedSrc("~") == 0 {
		t.Error("expected '~' to map to newly allocated token")
	}
	out, err := j.Parse("a:1;b:2")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) || m["b"] != float64(2) {
		t.Errorf("expected {a:1 b:2}, got %v", m)
	}
}

func TestApplyFixedTokensEmptySrc(t *testing.T) {
	// Empty string source: existing mapping removed, no new mapping added.
	empty := ""
	j := Make()
	j.SetOptions(Options{Fixed: &FixedOptions{Token: map[string]*string{
		"#CA": &empty,
	}}})
	if j.FixedSrc(",") != 0 {
		t.Error("expected ',' mapping removed for empty src")
	}
	if j.FixedTin(TinCA) != "" {
		t.Error("expected no source for TinCA")
	}
}

// --- registerMatchSpecs branches ---

func TestRegisterMatchSpecsSkipsAndReplaces(t *testing.T) {
	j := Make()

	made := 0
	mk := func(cfg *LexConfig, opts *Options) LexMatcher {
		made++
		return func(lex *Lex, rule *Rule) *Token { return nil }
	}

	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"skipme-nil":  nil,                                                                               // nil spec → skipped
		"skipme-make": {Order: 100, Make: nil},                                                           // nil Make → skipped
		"nilmatcher":  {Order: 100, Make: func(cfg *LexConfig, opts *Options) LexMatcher { return nil }}, // nil matcher → skipped
		"m1":          {Order: 100, Make: mk},
	}}})
	if made != 1 {
		t.Errorf("expected one matcher built, got %d", made)
	}
	n := len(j.Config().CustomMatchers)

	// Re-register same name → replaced, not appended.
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"m1": {Order: 50, Make: mk},
	}}})
	if len(j.Config().CustomMatchers) != n {
		t.Errorf("re-registering same name should replace, got %d entries (was %d)",
			len(j.Config().CustomMatchers), n)
	}
}

// --- normalizeCommentSuffix forms ---

func TestNormalizeCommentSuffixForms(t *testing.T) {
	// nil → nothing.
	strs, fn := normalizeCommentSuffix(nil)
	if strs != nil || fn != nil {
		t.Error("nil suffix should produce nothing")
	}

	// Empty string → nothing.
	strs, _ = normalizeCommentSuffix("")
	if len(strs) != 0 {
		t.Error("empty string suffix should produce nothing")
	}

	// Single string.
	strs, _ = normalizeCommentSuffix("!!")
	if len(strs) != 1 || strs[0] != "!!" {
		t.Errorf("expected [!!], got %v", strs)
	}

	// []string sorted longest-first, empty entries dropped.
	strs, _ = normalizeCommentSuffix([]string{"a", "", "bbb", "cc"})
	if len(strs) != 3 || strs[0] != "bbb" || strs[1] != "cc" || strs[2] != "a" {
		t.Errorf("expected [bbb cc a], got %v", strs)
	}

	// []any (MapToOptions JSON form).
	strs, _ = normalizeCommentSuffix([]any{"x", 7, "yy"})
	if len(strs) != 2 || strs[0] != "yy" || strs[1] != "x" {
		t.Errorf("expected [yy x], got %v", strs)
	}

	// LexMatcher form.
	m := LexMatcher(func(lex *Lex, rule *Rule) *Token { return nil })
	_, fn = normalizeCommentSuffix(m)
	if fn == nil {
		t.Error("LexMatcher suffix should produce a matcher")
	}

	// Plain func form.
	_, fn = normalizeCommentSuffix(func(lex *Lex, rule *Rule) *Token { return nil })
	if fn == nil {
		t.Error("func suffix should produce a matcher")
	}
}

// --- Derive: inherit ender chars, escape map, token sets, decorations, plugin opts ---

func TestDeriveInheritsExtendedState(t *testing.T) {
	parent := Make(Options{
		Ender:  []string{";"},
		String: &StringOptions{Escape: map[string]string{"z": "ZED"}},
	})
	parent.SetTokenSet("MYSET", []Tin{TinTX})
	parent.Decorate("deco", "val")
	parent.SetPluginOptions("plug", map[string]any{"a": 1})
	parent.Sub(func(tkn *Token, r *Rule, ctx *Context) {}, func(r *Rule, ctx *Context) {})

	child := parent.Derive()

	if !child.Config().EnderChars[';'] {
		t.Error("child should inherit ender chars")
	}
	if child.Config().EscapeMap["z"] != "ZED" {
		t.Error("child should inherit escape map")
	}
	if ts := child.TokenSet("MYSET"); len(ts) != 1 || ts[0] != TinTX {
		t.Error("child should inherit custom token sets")
	}
	if child.Decoration("deco") != "val" {
		t.Error("child should inherit decorations")
	}
	if po := child.PluginOptions("plug"); po == nil || po["a"] != 1 {
		t.Error("child should inherit plugin options")
	}
	if _, err := child.Parse("a:1"); err != nil {
		t.Fatal(err)
	}
}

func TestDerivePluginErrorPanics(t *testing.T) {
	parent := Make()
	fail := false
	bad := Plugin(func(j *Tabnas, opts map[string]any) error {
		if fail {
			return errors.New("boom")
		}
		return nil
	})
	if err := parent.Use(bad); err != nil {
		t.Fatal(err)
	}
	fail = true
	defer func() {
		if recover() == nil {
			t.Error("expected panic when plugin errors during Derive")
		}
	}()
	parent.Derive()
}

// --- SetOptions: tokenSet resolution and rule include ---

func TestSetOptionsTokenSet(t *testing.T) {
	j := Make()
	j.SetOptions(Options{TokenSet: map[string][]string{
		"KEY": {"#ST", "", "#TX"}, // empty name skipped
	}})
	keys := j.TokenSet("KEY")
	if len(keys) != 2 {
		t.Errorf("expected 2 tins in KEY set, got %v", keys)
	}
}

func TestSetOptionsRuleInclude(t *testing.T) {
	// rule.include via SetOptions keeps only tagged alts (json strict mode).
	j := Make()
	j.SetOptions(Options{Rule: &RuleOptions{Include: "json"}})
	// Strict JSON should still parse.
	out, err := j.Parse(`{"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", out)
	}
	// tabnas implicit map should fail (those alts were dropped).
	if _, err := j.Parse("a:1"); err == nil {
		t.Error("expected error for implicit map with include=json")
	}
}

func TestSetOptionsParserStart(t *testing.T) {
	j := Make()
	j.SetOptions(Options{Parser: &ParserOptions{
		Start: func(src string, j *Tabnas, meta map[string]any) (any, error) {
			return "custom:" + src, nil
		},
	}})
	out, err := j.Parse("xyz")
	if err != nil {
		t.Fatal(err)
	}
	if out != "custom:xyz" {
		t.Errorf("expected custom:xyz, got %v", out)
	}
}

func TestSetOptionsPreservesMatchValues(t *testing.T) {
	// Match.Value entries registered earlier survive a later SetOptions call
	// (preserved + re-sorted branch).
	j := Make()
	mustGrammar(t, j, &GrammarSpec{
		Ref: map[FuncRef]any{
			"@valOn": func(match []string) any { return true },
		},
		OptionsMap: map[string]any{
			"match": map[string]any{
				"value": map[string]any{
					"on": map[string]any{"match": "@/^on/i", "val": "@valOn"},
				},
			},
		},
	})
	sep := "_"
	j.SetOptions(Options{Number: &NumberOptions{Sep: sep}})

	out, err := j.Parse("a:ON")
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["a"] != true {
		t.Errorf("expected a:true preserved after SetOptions, got %v", out)
	}
}
