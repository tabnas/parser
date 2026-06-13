package tabnas

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- Parser.StartMeta (public API entry with meta + subscriptions) ---

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

// --- SetOptionsText error paths ---
//
// The engine ships no grammar, so SetOptionsText/GrammarText require a
// registered text parser (RegisterTextParser; the jsonic package
// registers one in its init). These tests register a stub parser to
// exercise every path, restoring the unregistered state on cleanup.

// withStubTextParser registers p for the duration of the test.
func withStubTextParser(t *testing.T, p func(string) (any, error)) {
	t.Helper()
	RegisterTextParser(p)
	t.Cleanup(func() { RegisterTextParser(nil) })
}

func TestSetOptionsTextEmpty(t *testing.T) {
	// Empty text short-circuits before the parser lookup.
	j := Make()
	if _, err := j.SetOptionsText(""); err != nil {
		t.Errorf("empty text should be a no-op: %v", err)
	}
}

func TestSetOptionsTextUnregistered(t *testing.T) {
	j := Make()
	if _, err := j.SetOptionsText(`tag: "x"`); err == nil ||
		!strings.Contains(err.Error(), "no text parser registered") {
		t.Errorf("expected no-parser error, got: %v", err)
	}
}

func TestSetOptionsTextParseError(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) {
		return nil, fmt.Errorf("boom")
	})
	j := Make()
	if _, err := j.SetOptionsText(`"unterminated`); err == nil || err.Error() != "boom" {
		t.Errorf("parser error should propagate, got: %v", err)
	}
}

func TestSetOptionsTextNotMap(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) {
		return []any{1.0, 2.0}, nil
	})
	j := Make()
	if _, err := j.SetOptionsText(`[1,2]`); err == nil ||
		!strings.Contains(err.Error(), "expected map") {
		t.Errorf("expected not-a-map error, got: %v", err)
	}
}

func TestSetOptionsTextNilParse(t *testing.T) {
	// Comment-only source parses to nil — SetOptionsText is a no-op.
	withStubTextParser(t, func(string) (any, error) { return nil, nil })
	j := Make()
	if _, err := j.SetOptionsText("# just a comment"); err != nil {
		t.Errorf("comment-only text should be a no-op: %v", err)
	}
}

func TestSetOptionsTextApplies(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) {
		return map[string]any{"number": map[string]any{"sep": "_"}}, nil
	})
	j := Make()
	before := j.Options()
	if _, err := j.SetOptionsText(`number: { sep: "_" }`); err != nil {
		t.Fatal(err)
	}
	after := j.Options()
	if after.Number == nil || after.Number.Sep != "_" {
		t.Errorf("SetOptionsText should apply parsed options (before %+v), got %+v", before.Number, after.Number)
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

	child, _ := parent.Derive()

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

func TestDerivePluginErrorReturned(t *testing.T) {
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
	// Mirrors TS make(): a plugin failing during child derivation
	// surfaces as an error (never a panic).
	if _, err := parent.Derive(); err == nil ||
		!strings.Contains(err.Error(), "boom") {
		t.Errorf("expected plugin error from Derive, got %v", err)
	}
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
