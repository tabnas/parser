package tabnas

// Engine-mechanics tests, adapted from the former jsonic/plugin_test.go
// to run against the bare engine and the strict-JSON fixture instead of
// the relaxed-JSON grammar. Covers the plugin entry points (Use /
// UseDefaults / Decorate / Sub / PluginOptions), token registration, the
// RuleSpec mutation API, group include/exclude filtering, custom lexer
// matchers, ParseMeta, and the Util helpers.

import (
	"regexp"
	"strings"
	"testing"
)

// pmKeyword builds a one-token grammar over the bare engine: the start
// rule "top" matches a single value token, used to exercise engine
// mechanics without a full grammar.
func pmTopVal() *Tabnas {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Open = []*AltSpec{{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) {
			r.Node = r.O0.ResolveVal(r, ctx)
		}}}
		rs.Close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	return j
}

// --- Use / UseDefaults ---

func TestPMUseInvokes(t *testing.T) {
	invoked := false
	var seen map[string]any
	j := Make()
	if err := j.Use(func(j *Tabnas, opts map[string]any) error {
		invoked = true
		seen = opts
		return nil
	}, map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if !invoked || seen["k"] != "v" {
		t.Errorf("plugin not invoked with options: %v %v", invoked, seen)
	}
}

func TestPMUseChaining(t *testing.T) {
	order := []string{}
	j := Make()
	_ = j.Use(func(*Tabnas, map[string]any) error { order = append(order, "a"); return nil })
	_ = j.Use(func(*Tabnas, map[string]any) error { order = append(order, "b"); return nil })
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("expected [a b], got %v", order)
	}
}

func TestPMUseNilOptions(t *testing.T) {
	j := Make()
	if err := j.Use(func(j *Tabnas, opts map[string]any) error {
		if opts != nil {
			t.Errorf("expected nil opts, got %v", opts)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPMUseDefaults(t *testing.T) {
	var seen map[string]any
	j := Make()
	err := j.UseDefaults(func(j *Tabnas, opts map[string]any) error {
		seen = opts
		return nil
	}, map[string]any{"sep": ",", "trim": true}, map[string]any{"trim": false})
	if err != nil {
		t.Fatal(err)
	}
	if seen["sep"] != "," || seen["trim"] != false {
		t.Errorf("defaults not merged under caller opts: %v", seen)
	}
}

// --- Tokens ---

func TestPMTokenRegisterAndName(t *testing.T) {
	j := Make()
	tin := j.Token("#QQ")
	if tin < TinMAX {
		t.Errorf("expected new tin >= %d, got %d", TinMAX, tin)
	}
	if j.Token("#QQ") != tin {
		t.Error("re-lookup returned a different tin")
	}
	if j.TinName(tin) != "#QQ" {
		t.Errorf("TinName(%d) = %q, want #QQ", tin, j.TinName(tin))
	}
}

func TestPMCustomFixedToken(t *testing.T) {
	// Rebind comma to '~' so it separates array elements in strict JSON.
	j := makeJSON()
	tilde := "~"
	j.SetOptions(Options{Fixed: &FixedOptions{Token: map[string]*string{"#CA": &tilde}}})
	got, err := j.Parse("[1 ~ 2 ~ 3]")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	want := []any{float64(1), float64(2), float64(3)}
	if !valuesEqual(got, want) {
		t.Errorf("got %s want %s", formatValue(got), formatValue(want))
	}
}

// --- RuleSpec mutation API ---

func TestPMRuleSpecAPI(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	var phases []string
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		// Start from a clean slate, then build up via the mutators.
		rs.Clear()
		rs.AddOpen(&AltSpec{S: [][]Tin{TinSetVAL}})
		rs.PrependOpen(&AltSpec{S: [][]Tin{{TinOS}}, P: "never"}) // index 0
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		rs.PrependClose(&AltSpec{S: [][]Tin{{TinCB}}, B: 1}) // index 0
		// Drop the unreachable open[0] we just prepended.
		rs.ModifyOpen(&AltModListOpts{Delete: []int{0}})
		rs.AddBO(func(r *Rule, ctx *Context) { phases = append(phases, "bo") })
		rs.AddAO(func(r *Rule, ctx *Context) { phases = append(phases, "ao") })
		rs.AddBC(func(r *Rule, ctx *Context) { phases = append(phases, "bc") })
		rs.AddAC(func(r *Rule, ctx *Context) { phases = append(phases, "ac") })
	})
	if _, err := j.Parse("1"); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, want := range []string{"bo", "ao", "bc", "ac"} {
		found := false
		for _, p := range phases {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("phase %q not fired; got %v", want, phases)
		}
	}
	if len(j.RSM()["top"].Open) != 1 {
		t.Errorf("expected 1 open alt after delete, got %d", len(j.RSM()["top"].Open))
	}
}

func TestPMModifyMove(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.Clear()
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{TinNR}}, A: func(r *Rule, ctx *Context) { r.Node = "num" }},
			&AltSpec{S: [][]Tin{{TinST}}, A: func(r *Rule, ctx *Context) { r.Node = "str" }},
		)
		// Swap the two open alts.
		rs.ModifyOpen(&AltModListOpts{Move: []int{0, 1, 1, 0}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	out, err := j.Parse(`"x"`)
	if err != nil || out != "str" {
		t.Errorf("got %v, %v", out, err)
	}
}

// --- Rule modification on the strict fixture ---

func TestPMRuleUppercase(t *testing.T) {
	j := makeJSON()
	j.Rule("val", func(rs *RuleSpec, _ *Parser) {
		rs.AddAC(func(r *Rule, ctx *Context) {
			if s, ok := r.Node.(string); ok {
				r.Node = strings.ToUpper(s)
			}
		})
	})
	got, err := j.Parse(`["hello","World"]`)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(got, []any{"HELLO", "WORLD"}) {
		t.Errorf("got %s", formatValue(got))
	}
}

// --- Group include / exclude ---

func TestPMExcludeJSONGroup(t *testing.T) {
	// Excluding the "json" group strips every strict alternate, so even
	// a trivial value no longer parses.
	j := makeJSON(Options{Rule: &RuleOptions{Exclude: "json"}})
	if _, err := j.Parse("1"); err == nil {
		t.Error("expected error after excluding the json group")
	}
}

func TestPMIncludeMapOnly(t *testing.T) {
	// Including only json keeps the strict grammar working.
	j := makeJSON(Options{Rule: &RuleOptions{Include: "json"}})
	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- ParseMeta ---

func TestPMParseMeta(t *testing.T) {
	calls := 0
	j := makeJSON()
	_, err := j.ParseMeta(`{"a":1}`, map[string]any{"log": func(...any) { calls++ }})
	if err != nil {
		t.Fatal(err)
	}
	// The meta map is accepted; a successful parse is the assertion.
}

func TestPMParseMetaNil(t *testing.T) {
	j := makeJSON()
	if _, err := j.ParseMeta("1", nil); err != nil {
		t.Fatalf("ParseMeta nil meta: %v", err)
	}
}

// --- Custom matchers ---

func TestPMCustomMatcherLexMake(t *testing.T) {
	// A "$$" matcher producing a VL token, via the Lex.Match make API.
	j := makeJSON()
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"dollar": {Order: 1_500_000, Make: func(_ *LexConfig, _ *Options) LexMatcher {
			return func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				if pnt.SI+2 <= pnt.Len && lex.Src[pnt.SI:pnt.SI+2] == "$$" {
					tkn := lex.Token("#VL", TinVL, "DOLLAR", "$$")
					pnt.SI += 2
					pnt.CI += 2
					return tkn
				}
				return nil
			}
		}},
	}}})
	if out, err := j.Parse("$$"); err != nil || out != "DOLLAR" {
		t.Errorf("got %v, %v", out, err)
	}
}

func TestPMMatcherPriorityCaptures(t *testing.T) {
	// An early matcher (order < built-in number) captures "42" first.
	j := makeJSON()
	j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"cap42": {Order: 1_000, Make: func(_ *LexConfig, _ *Options) LexMatcher {
			return func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				if pnt.SI+2 <= pnt.Len && lex.Src[pnt.SI:pnt.SI+2] == "42" {
					tkn := lex.Token("#VL", TinVL, "FORTY_TWO", "42")
					pnt.SI += 2
					pnt.CI += 2
					return tkn
				}
				return nil
			}
		}},
	}}})
	if out, err := j.Parse("42"); err != nil || out != "FORTY_TWO" {
		t.Errorf("got %v, %v", out, err)
	}
}

func TestPMValueMatcherRegex(t *testing.T) {
	// A regexp value matcher producing a transformed value.
	j := pmTopVal()
	j.SetOptions(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{
		"hash": {
			Match: regexp.MustCompile(`^#[0-9a-f]{3}`),
			Val:   func(m []string) any { return "COLOR:" + m[0] },
		},
	}}})
	if out, err := j.Parse("#f0a"); err != nil || out != "COLOR:#f0a" {
		t.Errorf("got %v, %v", out, err)
	}
}

func TestPMTokenFnMatcher(t *testing.T) {
	// Function-form token matcher (applyMatchTokens TokenFn branch).
	j := pmTopVal()
	j.SetOptions(Options{Match: &MatchOptions{TokenFn: map[string]LexMatcher{
		"#VL": func(lex *Lex, rule *Rule) *Token {
			pnt := lex.Cursor()
			if pnt.SI < pnt.Len && lex.Src[pnt.SI] == '@' {
				tkn := lex.Token("#VL", TinVL, "AT", "@")
				pnt.SI++
				pnt.CI++
				return tkn
			}
			return nil
		},
	}}})
	if out, err := j.Parse("@"); err != nil || out != "AT" {
		t.Errorf("got %v, %v", out, err)
	}
}

// --- Subscription ---

func TestPMSubLexAndRule(t *testing.T) {
	j := makeJSON()
	var toks, rules []string
	j.Sub(
		func(tkn *Token, rule *Rule, ctx *Context) { toks = append(toks, tkn.Src) },
		func(rule *Rule, ctx *Context) { rules = append(rules, rule.Name) },
	)
	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Fatal(err)
	}
	if !contains(toks, "{") {
		t.Errorf("lex sub missing '{': %v", toks)
	}
	if !contains(rules, "val") {
		t.Errorf("rule sub missing 'val': %v", rules)
	}
}

// --- Decorate ---

func TestPMDecorate(t *testing.T) {
	j := Make()
	j.Decorate("greet", "hi")
	if j.Decoration("greet") != "hi" {
		t.Errorf("decoration not stored: %v", j.Decoration("greet"))
	}
	if j.Decoration("missing") != nil {
		t.Error("unset decoration should be nil")
	}
}

// --- PluginOptions ---

func TestPMPluginOptions(t *testing.T) {
	j := Make()
	j.SetPluginOptions("myplug", map[string]any{"x": 1})
	got := j.PluginOptions("myplug")
	if got == nil || got["x"] != 1 {
		t.Errorf("plugin options not stored: %v", got)
	}
}

// --- Token sets ---

func TestPMTokenSets(t *testing.T) {
	j := Make()
	ignore := j.TokenSet("IGNORE")
	if len(ignore) == 0 {
		t.Error("IGNORE token set empty")
	}
	j.SetTokenSet("MYSET", []Tin{TinNR, TinST})
	if got := j.TokenSet("MYSET"); len(got) != 2 {
		t.Errorf("custom token set: %v", got)
	}
}

// --- Derive ---

func TestPMDeriveInheritsPlugins(t *testing.T) {
	count := 0
	j := Make()
	_ = j.Use(func(*Tabnas, map[string]any) error { count++; return nil })
	if count != 1 {
		t.Fatalf("plugin should run once on parent, got %d", count)
	}
	child, err := j.Derive()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("plugin should re-run on derive, got %d", count)
	}
	if child == j {
		t.Error("derive should return a new instance")
	}
}

// --- Token.Bad / Lex.Bad ---

func TestPMTokenBad(t *testing.T) {
	tkn := MakeToken("#TX", TinTX, "x", "x", Point{})
	out := tkn.Bad("oops", map[string]any{"why": "test"})
	if out.Err != "oops" {
		t.Errorf("Bad did not set Err: %q", out.Err)
	}
}

// --- Util helpers ---

func TestPMUtil(t *testing.T) {
	j := Make()
	u := j.Util()
	if u.Deep == nil || u.Keys == nil {
		t.Fatal("Util bag missing functions")
	}
	m := map[string]any{"b": 2, "a": 1}
	if got := Keys(m); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Keys sorted: %v", got)
	}
	if got := Values(m); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("Values: %v", got)
	}
	if got := Entries(m); len(got) != 2 || got[0].Key != "a" {
		t.Errorf("Entries: %v", got)
	}
	// Omap: rename "a"→"A", drop "b".
	out := Omap(m, func(e Entry) []any {
		if e.Key == "b" {
			return []any{nil, nil}
		}
		return []any{strings.ToUpper(e.Key), e.Value}
	})
	if out["A"] != 1 || len(out) != 1 {
		t.Errorf("Omap: %v", out)
	}
	// Nil-safety.
	if len(Keys(nil)) != 0 || len(Values(nil)) != 0 || len(Entries(nil)) != 0 {
		t.Error("nil-map helpers should return empty")
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
