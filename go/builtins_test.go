// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

// Go-port tests for the `$`-builtin stdlib, array-`a` composition, the
// `@~/` eager sentinel, the `$`-namespace reservation, and the builtin
// schema-version gate — mirroring ts/test/builtins.test.js. The
// cross-engine fixture tests load the SAME serialized grammars the TS
// suite captured (../ts/test/*.fixture.json) and assert identical
// accept/reject, proving TS↔Go parity on the wire format.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// --- builtin library ---

func TestBuiltinRefsLibrary(t *testing.T) {
	want := []string{"@bubble$", "@capture$", "@node$", "@probeDecide$",
		"@probeInit$", "@probePhase0$", "@probePhase1$", "@probePhase2$",
		"@object$", "@array$", "@reset$", "@key$", "@setval$", "@push$", "@value$"}
	got := make([]string, 0, len(BUILTIN_REFS))
	for k := range BUILTIN_REFS {
		got = append(got, k)
	}
	if len(got) != len(want) {
		t.Fatalf("builtin count: got %d, want %d", len(got), len(want))
	}
	for _, k := range want {
		if _, ok := BUILTIN_REFS[k]; !ok {
			t.Errorf("missing builtin %q", k)
		}
	}
	if BUILTIN_SCHEMA_VERSION != 2 {
		t.Errorf("BUILTIN_SCHEMA_VERSION = %d, want 2", BUILTIN_SCHEMA_VERSION)
	}
}

// --- tree builtins (direct invocation) ---

func TestBuiltinNode(t *testing.T) {
	// nterms as float64 to exercise parsed-JSON coercion.
	r := &Rule{
		K: map[string]any{"node$": map[string]any{
			"init": true, "rule": "r", "kind": "user", "nterms": float64(2)}},
		O: []*Token{{Src: "x"}, {Src: "y"}, {Src: "z"}},
	}
	builtinNode(r, nil)
	want := map[string]any{"rule": "r", "src": "xy", "kids": []any{}}
	if !reflect.DeepEqual(r.Node, want) {
		t.Errorf("node: got %v, want %v", r.Node, want)
	}

	// accumulate-only (no init) onto an existing node.
	r2 := &Rule{
		K: map[string]any{"node$": map[string]any{"nterms": 2}},
		O: []*Token{{Src: "A"}, {Src: "B"}},
	}
	r2.Node = map[string]any{"rule": "r", "src": "pre", "kids": []any{}}
	builtinNode(r2, nil)
	if r2.Node.(map[string]any)["src"] != "preAB" {
		t.Errorf("accumulate: got %v", r2.Node)
	}
}

func TestBuiltinCapture(t *testing.T) {
	// tagged child pushes into kids.
	r := &Rule{
		Node:  map[string]any{"src": "", "kids": []any{}},
		Child: &Rule{Node: map[string]any{"rule": "k", "src": "z", "kids": []any{}}},
	}
	builtinCapture(r, nil)
	n := r.Node.(map[string]any)
	if n["src"] != "z" || len(n["kids"].([]any)) != 1 {
		t.Errorf("tagged capture: got %v", n)
	}

	// untagged child flattens src + kids.
	child := map[string]any{"src": "q", "kids": []any{
		map[string]any{"rule": "h", "src": "", "kids": []any{}}}}
	r2 := &Rule{
		Node:  map[string]any{"src": "p", "kids": []any{}},
		Child: &Rule{Node: child},
	}
	builtinCapture(r2, nil)
	n2 := r2.Node.(map[string]any)
	if n2["src"] != "pq" || len(n2["kids"].([]any)) != 1 {
		t.Errorf("untagged flatten: got %v", n2)
	}

	// self-reference (child.node === node) is a no-op.
	self := map[string]any{"src": "S", "kids": []any{
		map[string]any{"rule": "x", "src": "", "kids": []any{}}}}
	r3 := &Rule{Node: self, Child: &Rule{Node: self}}
	builtinCapture(r3, nil)
	if self["src"] != "S" || len(self["kids"].([]any)) != 1 {
		t.Errorf("self-ref guard failed: got %v", self)
	}
}

func TestBuiltinBubble(t *testing.T) {
	child := map[string]any{"rule": "c", "src": "v", "kids": []any{}}
	r := &Rule{Child: &Rule{Node: child}}
	builtinBubble(r, nil)
	if !reflect.DeepEqual(r.Node, child) {
		t.Errorf("bubble: got %v", r.Node)
	}
}

func TestBuiltinProbeInitDecide(t *testing.T) {
	// @probeInit$ resets phase and records the mark.
	r := &Rule{K: map[string]any{}}
	builtinProbeInit(r, &Context{VAbs: 5})
	if r.K["pd_phase"] != 0 || r.K["pd_mark"] != 5 {
		t.Errorf("probeInit: phase=%v mark=%v", r.K["pd_phase"], r.K["pd_mark"])
	}

	// @probeDecide$ picks phase 1 when the disambiguator is present.
	r1 := &Rule{K: map[string]any{"pd_mark": 0, "pd_d": "#D"}}
	builtinProbeDecide(r1, &Context{VAbs: 0, T: []*Token{{Name: "#D"}}})
	if r1.K["pd_phase"] != 1 {
		t.Errorf("probeDecide present: phase=%v, want 1", r1.K["pd_phase"])
	}

	// ...and phase 2 when absent.
	r2 := &Rule{K: map[string]any{"pd_mark": 0, "pd_d": "#D"}}
	builtinProbeDecide(r2, &Context{VAbs: 0, T: []*Token{{Name: "#X"}}})
	if r2.K["pd_phase"] != 2 {
		t.Errorf("probeDecide absent: phase=%v, want 2", r2.K["pd_phase"])
	}

	// Defensive: missing pd_mark bails without touching phase.
	r3 := &Rule{K: map[string]any{"pd_d": "#D"}}
	builtinProbeDecide(r3, &Context{T: []*Token{{Name: "#D"}}})
	if _, set := r3.K["pd_phase"]; set {
		t.Error("probeDecide should bail when pd_mark is missing")
	}
}

func TestBuiltinPhaseGuards(t *testing.T) {
	p0 := BUILTIN_REFS["@probePhase0$"].(AltCond)
	p1 := BUILTIN_REFS["@probePhase1$"].(AltCond)
	p2 := BUILTIN_REFS["@probePhase2$"].(AltCond)
	if !p0(&Rule{K: map[string]any{"pd_phase": 0}}, nil) ||
		!p1(&Rule{K: map[string]any{"pd_phase": 1}}, nil) ||
		!p2(&Rule{K: map[string]any{"pd_phase": 2}}, nil) {
		t.Error("phase guard should match its phase")
	}
	if p0(&Rule{K: map[string]any{"pd_phase": 1}}, nil) {
		t.Error("phase 0 guard should not match phase 1")
	}
	// float64 phase (parsed) is coerced.
	if !p1(&Rule{K: map[string]any{"pd_phase": float64(1)}}, nil) {
		t.Error("phase guard should coerce float64")
	}
}

// --- array-`a` composition ---

func TestArrayActionComposition(t *testing.T) {
	j := Make()
	order := []int{}
	ref := map[FuncRef]any{
		"@one":   AltAction(func(r *Rule, ctx *Context) { order = append(order, 1) }),
		"@two":   AltAction(func(r *Rule, ctx *Context) { order = append(order, 2) }),
		"@three": AltAction(func(r *Rule, ctx *Context) { order = append(order, 3) }),
	}
	alt, err := j.resolveGrammarAlt(&GrammarAltSpec{A: []any{"@one", "@two", "@three"}}, ref)
	if err != nil {
		t.Fatal(err)
	}
	alt.A(&Rule{K: map[string]any{}}, &Context{})
	if !reflect.DeepEqual(order, []int{1, 2, 3}) {
		t.Errorf("order: got %v, want [1 2 3]", order)
	}
}

func TestArrayActionShortCircuit(t *testing.T) {
	j := Make()
	order := []int{}
	ref := map[FuncRef]any{
		"@err":   AltAction(func(r *Rule, ctx *Context) { order = append(order, 1); ctx.ParseErr = &Token{} }),
		"@after": AltAction(func(r *Rule, ctx *Context) { order = append(order, 2) }),
	}
	alt, err := j.resolveGrammarAlt(&GrammarAltSpec{A: []any{"@err", "@after"}}, ref)
	if err != nil {
		t.Fatal(err)
	}
	alt.A(&Rule{K: map[string]any{}}, &Context{})
	if !reflect.DeepEqual(order, []int{1}) {
		t.Errorf("short-circuit: got %v, want [1]", order)
	}
}

func TestArrayActionMixAndEmpty(t *testing.T) {
	j := Make()
	order := []int{}
	ref := map[FuncRef]any{"@mid": AltAction(func(r *Rule, ctx *Context) { order = append(order, 2) })}
	// inline func + ref string + inline func, in order.
	alt, err := j.resolveGrammarAlt(&GrammarAltSpec{A: []any{
		AltAction(func(r *Rule, ctx *Context) { order = append(order, 1) }),
		"@mid",
		AltAction(func(r *Rule, ctx *Context) { order = append(order, 3) }),
	}}, ref)
	if err != nil {
		t.Fatal(err)
	}
	alt.A(&Rule{K: map[string]any{}}, &Context{})
	if !reflect.DeepEqual(order, []int{1, 2, 3}) {
		t.Errorf("mixed order: got %v", order)
	}

	// empty array → no action.
	alt2, err := j.resolveGrammarAlt(&GrammarAltSpec{A: []any{}}, ref)
	if err != nil || alt2.A != nil {
		t.Errorf("empty array should yield no action: %v %v", alt2.A, err)
	}

	// unknown ref in array → error.
	if _, err := j.resolveGrammarAlt(&GrammarAltSpec{A: []any{"@nope"}}, ref); err == nil {
		t.Error("unknown ref in array should error")
	}
}

// --- $-reservation + version gate ---

func miniGrammar(extra func(*GrammarSpec)) *GrammarSpec {
	gs := &GrammarSpec{
		Rule: map[string]*GrammarRuleSpec{
			"val": {Close: []*GrammarAltSpec{{S: "#ZZ"}}},
		},
	}
	extra(gs)
	return gs
}

func TestDollarReservation(t *testing.T) {
	for _, key := range []string{"@bad$", "@a$b", "@node$"} {
		j := Make()
		err := j.Grammar(miniGrammar(func(gs *GrammarSpec) {
			gs.Ref = map[FuncRef]any{key: AltAction(func(r *Rule, ctx *Context) {})}
		}))
		if err == nil || !strings.Contains(err.Error(), "reserved for engine builtins") {
			t.Errorf("key %q: expected reservation error, got %v", key, err)
		}
	}
}

func TestVersionGate(t *testing.T) {
	// within schema: ok.
	if err := Make().Grammar(miniGrammar(func(gs *GrammarSpec) { gs.V = BUILTIN_SCHEMA_VERSION })); err != nil {
		t.Errorf("v=%d should load: %v", BUILTIN_SCHEMA_VERSION, err)
	}
	// newer: refused.
	err := Make().Grammar(miniGrammar(func(gs *GrammarSpec) { gs.V = BUILTIN_SCHEMA_VERSION + 1 }))
	if err == nil || !strings.Contains(err.Error(), "requires builtin schema version") {
		t.Errorf("v=%d should be refused, got %v", BUILTIN_SCHEMA_VERSION+1, err)
	}
	// negative: invalid.
	err = Make().Grammar(miniGrammar(func(gs *GrammarSpec) { gs.V = -1 }))
	if err == nil || !strings.Contains(err.Error(), "invalid builtin schema version") {
		t.Errorf("v=-1 should be invalid, got %v", err)
	}
}

// --- @~/ eager sentinel ---

func TestEagerSentinelResolve(t *testing.T) {
	v := ResolveFuncRefs("@~/HI/i", nil)
	er, ok := v.(*EagerRegexp)
	if !ok {
		t.Fatalf("@~/ should resolve to *EagerRegexp, got %T", v)
	}
	if !er.Re.MatchString("hi") || !er.Re.MatchString("HI") {
		t.Error("eager regexp should match case-insensitively")
	}
	// plain @/ stays a bare regexp.
	if _, ok := ResolveFuncRefs("@/HI/i", nil).(*regexp.Regexp); !ok {
		t.Error("@/ should resolve to *regexp.Regexp")
	}
}

func TestUnicodeRegexDialect(t *testing.T) {
	// JS/TS \uHHHH escapes (the form @tabnas/bnf emits for ABNF char
	// classes) must compile on Go's RE2 via the \x{} rewrite.
	v := ResolveFuncRefs("@/^[\\u0041-\\u005a]/", nil)
	re, ok := v.(*regexp.Regexp)
	if !ok {
		t.Fatalf("\\u char class should compile to *regexp.Regexp, got %T (%v)", v, v)
	}
	if !re.MatchString("A") || re.MatchString("a") {
		t.Error("[A-Z] char class mismatch after dialect rewrite")
	}
	// eager variant and \u{...} braced form.
	ev := ResolveFuncRefs("@~/^[\\u{61}-\\u{7a}]/", nil)
	er, ok := ev.(*EagerRegexp)
	if !ok {
		t.Fatalf("eager \\u{} should compile, got %T", ev)
	}
	if !er.Re.MatchString("a") || er.Re.MatchString("A") {
		t.Error("[a-z] char class mismatch after dialect rewrite")
	}
}

func TestUncompilableRegexFailsLoud(t *testing.T) {
	// A regex that RE2 cannot compile (lookahead) must surface as a clear
	// install error, not be silently dropped (which would leave the lexer
	// with no match token and mis-recognize input).
	j := Make()
	gs := &GrammarSpec{OptionsMap: map[string]any{
		"match": map[string]any{"token": map[string]any{"#X": "@/(?=foo)/"}}}}
	err := j.Grammar(gs)
	if err == nil || !strings.Contains(err.Error(), "did not compile") {
		t.Errorf("expected a loud compile error, got %v", err)
	}
}

// --- cross-engine fixtures (TS↔Go parity on the same serialized grammars) ---

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "ts", "test", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// fixtureSpec parses the serialized JSON grammar into a *GrammarSpec via
// the same map→spec path GrammarText uses (encoding/json instead of the
// engine's text parser, so a bare engine needs no grammar plugin).
func fixtureSpec(t *testing.T, name string) *GrammarSpec {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(loadFixture(t, name)), &m); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	gs := &GrammarSpec{}
	if om, ok := m["options"].(map[string]any); ok {
		gs.OptionsMap = om
	}
	if rm, ok := m["rule"].(map[string]any); ok {
		gs.Rule = mapToGrammarRules(rm)
	}
	if v, ok := m["v"]; ok {
		gs.V = cfgInt(v)
	}
	return gs
}

func fixtureAccepts(t *testing.T, name, input string) bool {
	t.Helper()
	j := Make()
	if err := j.Grammar(fixtureSpec(t, name)); err != nil {
		t.Fatalf("install fixture grammar: %v", err)
	}
	_, perr := j.Parse(input)
	return perr == nil
}

func TestProbeFixtureParity(t *testing.T) {
	// Full phase-retry recognition parity with TS: disambiguator present
	// (X "@" Y) and absent (Y) both accept; a dangling disambiguator with
	// no following Y rejects, proving the phase decision gates the parse.
	cases := map[string]bool{"abc": true, "ab@cd": true, "a@b": true, "@": false, "ab@": false}
	for input, want := range cases {
		if got := fixtureAccepts(t, "probe-grammar.fixture.json", input); got != want {
			t.Errorf("probe %q: got accept=%v, want %v", input, got, want)
		}
	}
}

func TestEagerFixtureParity(t *testing.T) {
	// case-insensitive literal recognizes regardless of case (eager lexing).
	cases := map[string]bool{"hi": true, "HI": true, "Hi": true, "ho": false, "h": false}
	for input, want := range cases {
		if got := fixtureAccepts(t, "eager-literal.fixture.json", input); got != want {
			t.Errorf("eager %q: got accept=%v, want %v", input, got, want)
		}
	}
}

// --- native-value builders ---

func TestNativeValueBuilders(t *testing.T) {
	// @object$ / @array$ / @reset$
	ro := &Rule{Node: "seed"}
	builtinObject(ro, nil)
	if _, ok := ro.Node.(map[string]any); !ok {
		t.Errorf("@object$: got %T", ro.Node)
	}
	ra := &Rule{Node: "seed"}
	builtinArray(ra, nil)
	if s, ok := ra.Node.([]any); !ok || len(s) != 0 {
		t.Errorf("@array$: got %v", ra.Node)
	}
	rr := &Rule{Node: map[string]any{"a": 1}}
	builtinReset(rr, nil)
	if !IsUndefined(rr.Node) {
		t.Errorf("@reset$: got %v, want Undefined", rr.Node)
	}

	// @key$ captures the token value into r.U
	rk := &Rule{K: map[string]any{}, U: map[string]any{}, O: []*Token{{Val: "name"}}}
	builtinKey(rk, nil)
	if rk.U["key"] != "name" {
		t.Errorf("@key$: got %v", rk.U["key"])
	}

	// @setval$ assigns child under captured key
	rs := &Rule{K: map[string]any{}, Node: map[string]any{}, U: map[string]any{"key": "a"},
		Child: &Rule{Node: 42}}
	builtinSetval(rs, nil)
	if m, _ := rs.Node.(map[string]any); m["a"] != 42 {
		t.Errorf("@setval$: got %v", rs.Node)
	}

	// @push$ appends and re-publishes to parent (Go slice value-type)
	parent := &Rule{}
	rp := &Rule{Node: []any{1}, Parent: parent, Child: &Rule{Node: 2}}
	builtinPush(rp, nil)
	if s, _ := rp.Node.([]any); len(s) != 2 || s[1] != 2 {
		t.Errorf("@push$: got %v", rp.Node)
	}
	if ps, _ := parent.Node.([]any); len(ps) != 2 {
		t.Errorf("@push$ parent re-publish: got %v", parent.Node)
	}
	// no-value child is skipped
	rp2 := &Rule{Node: []any{1}, Parent: parent, Child: &Rule{Node: Undefined}}
	builtinPush(rp2, nil)
	if s, _ := rp2.Node.([]any); len(s) != 1 {
		t.Errorf("@push$ skip-undef: got %v", rp2.Node)
	}

	// @value$ prefers child node, else nothing-to-resolve → child wins
	rv := &Rule{K: map[string]any{}, Node: "old", Child: &Rule{Node: map[string]any{"built": true}}}
	builtinValue(rv, &Context{})
	if m, _ := rv.Node.(map[string]any); m["built"] != true {
		t.Errorf("@value$ child-wins: got %v", rv.Node)
	}
}

func TestNativeValueLeakageFix(t *testing.T) {
	// The config-reading builders delete their own r.K key after reading,
	// so a config set on one alt can't propagate to a child and mis-fire.
	rk := &Rule{K: map[string]any{"key$": map[string]any{"slot": "k"}}, U: map[string]any{},
		O: []*Token{{Val: "v"}}}
	builtinKey(rk, nil)
	if rk.U["k"] != "v" {
		t.Errorf("@key$ custom slot: got %v", rk.U["k"])
	}
	if _, present := rk.K["key$"]; present {
		t.Error("@key$ must delete its r.K config key after reading (leakage fix)")
	}
	rv := &Rule{K: map[string]any{"value$": map[string]any{}}, Child: &Rule{Node: "x"}}
	builtinValue(rv, &Context{})
	if _, present := rv.K["value$"]; present {
		t.Error("@value$ must delete its r.K config key after reading")
	}
}

func TestJsonBuilderFixtureParity(t *testing.T) {
	// The SAME serialized function-free json-core grammar the TS suite
	// uses, here on the Go engine; built values must match encoding/json
	// (the language-neutral oracle), pinning Go↔TS value parity.
	spec := fixtureSpec(t, "json-builder.fixture.json")
	if spec.Options == nil {
		spec.Options = &Options{}
	}
	spec.Options.Rule = &RuleOptions{Start: "val"}
	for _, input := range []string{"1", `"x"`, "true", "false", "null", "{}", "[]",
		`{"a":1}`, "[1,2,3]", `{"a":{"b":[true,null,"x"]}}`, `{"a":1,"b":2}`} {
		j := Make()
		if err := j.Grammar(spec); err != nil {
			t.Fatalf("install: %v", err)
		}
		got, err := j.Parse(input)
		if err != nil {
			t.Errorf("parse %q: %v", input, err)
			continue
		}
		var oracle any
		_ = json.Unmarshal([]byte(input), &oracle)
		if !reflect.DeepEqual(UnwrapUndefined(got), oracle) {
			t.Errorf("build %q: got %#v, want %#v", input, UnwrapUndefined(got), oracle)
		}
	}
}
