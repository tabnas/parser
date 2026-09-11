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
	want := []string{"@bubble$", "@capture$", "@fold$", "@node$", "@probeDecide$",
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
	if BUILTIN_SCHEMA_VERSION != 5 {
		t.Errorf("BUILTIN_SCHEMA_VERSION = %d, want 5", BUILTIN_SCHEMA_VERSION)
	}
}

// --- tree builtins (direct invocation) ---

func TestBuiltinNode(t *testing.T) {
	// Config is bound at grammar load (A1, #120), so a direct invocation
	// builds a configured instance. nterms as float64 to exercise
	// parsed-JSON coercion.
	r := &Rule{O: []*Token{{Src: "x"}, {Src: "y"}, {Src: "z"}}}
	makeBuiltinNode(map[string]any{
		"init": true, "rule": "r", "kind": "user", "nterms": float64(2)})(r, nil)
	want := map[string]any{"rule": "r", "src": "xy", "kids": []any{}}
	if !reflect.DeepEqual(r.Node, want) {
		t.Errorf("node: got %v, want %v", r.Node, want)
	}

	// accumulate-only (no init) onto an existing node.
	r2 := &Rule{O: []*Token{{Src: "A"}, {Src: "B"}}}
	r2.Node = map[string]any{"rule": "r", "src": "pre", "kids": []any{}}
	makeBuiltinNode(map[string]any{"nterms": 2})(r2, nil)
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
	// JS/TS \uHHHH escapes (the form @tabnas/abnf emits for ABNF char
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

func TestReplacedChildPublishesFinalNode(t *testing.T) {
	j := Make()
	if err := j.Grammar(fixtureSpec(t, "replace-child.fixture.json")); err != nil {
		t.Fatalf("install fixture grammar: %v", err)
	}
	got, err := j.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kids":[{"kids":[],"rule":"leaf","src":"a"}],"rule":"top","src":"a"}`
	if string(encoded) != want {
		t.Fatalf("replaced child node:\n  got  %s\n  want %s", encoded, want)
	}
}

// The contract Child carries: it is the rule this rule PUSHED, and it stays
// that rule even when that rule replaces itself. makeFold$'s own comment
// depends on it ("the parent's r.child pointer stays on the FIRST
// iteration"), and so does every precedence-climbing grammar.
//
// Publishing the replacement instead handed a parent the node of a rule it
// never pushed: @tabnas/expr read `1+2*3` back as ["*",2,3] -- left operand
// and operator dropped -- and 16 of its own tests plus 25 of @tabnas/c's went
// red against an engine whose own suite stayed green. A rule that must
// deliver a node upward across a replacement uses @fold$, which is what
// replace-child.fixture.json now does.
//
// Shared with ts/test/builtins.test.js: same fixture, same expectation.
func TestReplacementIsNotThePushersChild(t *testing.T) {
	j := Make()
	if err := j.Grammar(fixtureSpec(t, "child-pusher.fixture.json")); err != nil {
		t.Fatalf("install fixture grammar: %v", err)
	}
	got, err := j.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kids":[{"kids":[],"rule":"mid","src":""}],"rule":"top","src":""}`
	if string(encoded) != want {
		t.Fatalf("pusher's child:\n  got  %s\n  want %s", encoded, want)
	}
}

func TestFailedRelexRestoresEarlierLookahead(t *testing.T) {
	j := Make()
	if err := j.Grammar(fixtureSpec(t, "relex-rollback.fixture.json")); err != nil {
		t.Fatalf("install relex rollback fixture: %v", err)
	}
	got, err := j.Parse("abc")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kids":[],"rule":"top","src":"abc"}`
	if string(encoded) != want {
		t.Fatalf("relex rollback node:\n  got  %s\n  want %s", encoded, want)
	}
}

// --- native-value builders ---

func TestNativeValueBuilders(t *testing.T) {
	// @object$ / @array$ / @reset$
	ro := &Rule{Node: "seed"}
	builtinObject(ro, nil)
	if om, ok := ro.Node.(*OrderedMap); !ok || om.Len() != 0 {
		t.Errorf("@object$: got %T (%v)", ro.Node, ro.Node)
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

	// @key$ {lit} takes the key from config, not from a token: `lit` wins
	// outright even with a token present, honours `slot`, treats "" as a
	// real key, and falls back to the token for any non-string (matching
	// the TS typeof guard — a deserialized grammar can carry a null).
	rkl := &Rule{K: map[string]any{}, U: map[string]any{}, O: []*Token{{Val: "fromToken"}}}
	builtinKeyCfg(rkl, nil, map[string]any{"lit": "fromConfig"})
	if rkl.U["key"] != "fromConfig" {
		t.Errorf("@key$ lit: got %v, want fromConfig", rkl.U["key"])
	}
	rks := &Rule{K: map[string]any{}, U: map[string]any{}}
	builtinKeyCfg(rks, nil, map[string]any{"lit": "major", "slot": "k2"})
	if rks.U["k2"] != "major" {
		t.Errorf("@key$ lit+slot: got %v, want major", rks.U["k2"])
	}
	rke := &Rule{K: map[string]any{}, U: map[string]any{}, O: []*Token{{Val: "fromToken"}}}
	builtinKeyCfg(rke, nil, map[string]any{"lit": ""})
	if rke.U["key"] != "" {
		t.Errorf(`@key$ lit "": got %v, want ""`, rke.U["key"])
	}
	for _, bad := range []any{nil, 7, map[string]any{}} {
		rb := &Rule{K: map[string]any{}, U: map[string]any{}, O: []*Token{{Val: "fromToken"}}}
		builtinKeyCfg(rb, nil, map[string]any{"lit": bad})
		if rb.U["key"] != "fromToken" {
			t.Errorf("@key$ lit %#v: got %v, want the token value", bad, rb.U["key"])
		}
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
	// Config is BOUND AT GRAMMAR LOAD (A1, #120), so a direct invocation
	// builds a configured instance from the factory. This replaces the
	// delete-after-read assertions: the five value builders used to
	// remove their own r.K key to stop config leaking into a child, and
	// that containment measure is unnecessary now the config never
	// enters r.K at all.
	rk := &Rule{U: map[string]any{}, O: []*Token{{Val: "v"}}}
	makeBuiltinKey(map[string]any{"slot": "k"})(rk, nil)
	if rk.U["k"] != "v" {
		t.Errorf("@key$ custom slot: got %v", rk.U["k"])
	}

	// And a config left in r.K by a PARENT must now be inert: reading it
	// is what made the same serialized grammar answer 4 here and 3 in
	// TypeScript. A bare builtin takes its defaults, whatever the bag
	// holds.
	rleak := &Rule{K: map[string]any{"key$": map[string]any{"slot": "leaked"}},
		U: map[string]any{}, O: []*Token{{Val: "v"}}}
	builtinKey(rleak, nil)
	if _, leaked := rleak.U["leaked"]; leaked {
		t.Error("@key$ must IGNORE a config left in r.K — that leak is the " +
			"divergence A1 repairs, not a feature to preserve")
	}
	if rleak.U["key"] != "v" {
		t.Errorf("@key$ bare must use its default slot: got %v", rleak.U)
	}
}

func TestNativeValueBuildersInfo(t *testing.T) {
	// With the info options on, the builders allocate the engine's info
	// carriers (MapRef / ListRef / Text) — the Go counterpart of the TS
	// hidden marker property. Info-off behaviour is covered above.

	// @object$ → MapRef; implicit defaults false and is static alt config.
	mapCtx := &Context{Cfg: &LexConfig{MapRef: true}}
	ro := &Rule{Node: "seed"}
	builtinObject(ro, mapCtx)
	mr, ok := ro.Node.(MapRef)
	if !ok {
		t.Fatalf("@object$ info: got %T, want MapRef", ro.Node)
	}
	if mr.Implicit || mr.Val == nil || mr.Meta == nil {
		t.Errorf("@object$ info: implicit=%v val=%v meta=%v", mr.Implicit, mr.Val, mr.Meta)
	}
	ro2 := &Rule{}
	makeBuiltinObject(map[string]any{"implicit": true})(ro2, mapCtx)
	if mr2, _ := ro2.Node.(MapRef); !mr2.Implicit {
		t.Error("@object$ info: bound implicit config not honoured")
	}

	// @array$ → ListRef.
	listCtx := &Context{Cfg: &LexConfig{ListRef: true}}
	ra := &Rule{Node: "seed"}
	builtinArray(ra, listCtx)
	if lr, ok := ra.Node.(ListRef); !ok || lr.Val == nil || lr.Meta == nil {
		t.Errorf("@array$ info: got %T", ra.Node)
	}

	// @setval$ writes into a MapRef via NodeMapSet; node stays a MapRef.
	rs := &Rule{K: map[string]any{}, U: map[string]any{"key": "a"},
		Node:  MapRef{Val: map[string]any{}, Meta: map[string]any{}},
		Child: &Rule{Node: 42}}
	builtinSetval(rs, nil)
	srm, ok := rs.Node.(MapRef)
	if !ok || srm.Val["a"] != 42 {
		t.Errorf("@setval$ info: got %#v", rs.Node)
	}

	// @push$ appends into a ListRef via NodeListAppend and re-publishes.
	parent := &Rule{}
	rp := &Rule{Node: ListRef{Val: []any{1}, Meta: map[string]any{}}, Parent: parent,
		Child: &Rule{Node: 2}}
	builtinPush(rp, nil)
	prl, ok := rp.Node.(ListRef)
	if !ok || len(prl.Val) != 2 || prl.Val[1] != 2 {
		t.Errorf("@push$ info: got %#v", rp.Node)
	}
	if ppl, _ := parent.Node.(ListRef); len(ppl.Val) != 2 {
		t.Errorf("@push$ info parent re-publish: got %#v", parent.Node)
	}

	// @value$ wraps a string token in a Text carrying the source quote.
	textCtx := &Context{Cfg: &LexConfig{TextInfo: true}}
	rv := &Rule{K: map[string]any{}, Child: &Rule{Node: Undefined},
		O: []*Token{{Tin: TinST, Val: "hi", Src: `"hi"`}}}
	builtinValue(rv, textCtx)
	if tx, ok := rv.Node.(Text); !ok || tx.Str != "hi" || tx.Quote != `"` {
		t.Errorf("@value$ info: got %#v", rv.Node)
	}
	// A non-string-token value (number) is left bare.
	rv2 := &Rule{K: map[string]any{}, Child: &Rule{Node: Undefined},
		O: []*Token{{Tin: TinNR, Val: 5, Src: "5"}}}
	builtinValue(rv2, textCtx)
	if rv2.Node != 5 {
		t.Errorf("@value$ info number: got %#v", rv2.Node)
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
		// Objects are OrderedMaps now; compare VALUES against the (unordered)
		// encoding/json oracle by flattening to plain maps. Order parity is
		// pinned separately (TestObjectInsertionOrder).
		if !reflect.DeepEqual(omPlainify(UnwrapUndefined(got)), oracle) {
			t.Errorf("build %q: got %#v, want %#v", input, UnwrapUndefined(got), oracle)
		}
	}
}

// TestLiteralKeyFixtureParity: the key side of @setval$ used to be
// reachable only from a TOKEN, which suits `{"a":1}` and suits nothing
// that DECLARES its shape: in `ver = major "," minor` the part names are
// in the grammar, not in the input, so there was no token for @key$ to
// read and the whole grammar could build no object at all.
//
// Same serialized fixture the TS suite runs, so a port that drops `lit`
// fails on one side and is caught.
func TestLiteralKeyFixtureParity(t *testing.T) {
	spec := fixtureSpec(t, "literal-key.fixture.json")
	if spec.Options == nil {
		spec.Options = &Options{}
	}
	spec.Options.Rule = &RuleOptions{Start: "ver"}
	j := Make()
	if err := j.Grammar(spec); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := j.Parse("1,2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]any{"major": 1.0, "minor": 2.0}
	if !reflect.DeepEqual(omPlainify(UnwrapUndefined(got)), want) {
		t.Errorf("build: got %#v, want %#v", UnwrapUndefined(got), want)
	}
}

// TestSrcValueFixtureParity: schema v5. A member whose value IS its
// matched text had no way to get it -- the tree builders accumulate the
// text into node.src, and no builtin could read it back out, so @setval$
// could only assign the whole {rule, src, kids} node.
//
// One fixture covers all three cases the compiler has to emit:
//   - major/minor: scalar members, flattened to their src by setval src
//   - tags:        a member that built its OWN value, assigned whole
//     (no src) -- this is what makes nesting work
//   - the tags elements: flattened by push src, the array counterpart
//
// Run by the TS and Rust suites too, so a port that drops src is caught.
func TestSrcValueFixtureParity(t *testing.T) {
	spec := fixtureSpec(t, "src-value.fixture.json")
	j := Make()
	if err := j.Grammar(spec); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := j.Parse("1,2,3,4")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]any{
		"major": "1",
		"minor": "2",
		"tags":  []any{"3", "4"},
	}
	if !reflect.DeepEqual(omPlainify(UnwrapUndefined(got)), want) {
		t.Errorf("build: got %#v, want %#v", UnwrapUndefined(got), want)
	}
}

// TestPushSurvivesReplacementFixtureParity: a rule that allocates a list,
// pushes into it, and REPLACES itself to carry the chain on. The parent's
// Child pointer still refers to the rule that was replaced, so a parent
// reading the result — here @bubble$ on __start__ — reads THAT rule's
// node.
//
// Free in TypeScript and Rust, which hand the replacement the same list
// and mutate it in place. Here a slice is a value, so the grown header
// has to be carried back along the Prev chain; without that this returned
// ["1"], dropping every element appended after the replacement.
//
// Same fixture in all three suites, which is the only reason this was
// ever noticed: the json-builder array oracle uses the OTHER idiom, where
// the allocating and pushing rules are different rules in a parent/child
// relationship, so the parent write-back already covered it.
func TestPushSurvivesReplacementFixtureParity(t *testing.T) {
	spec := fixtureSpec(t, "push-replace.fixture.json")
	j := Make()
	if err := j.Grammar(spec); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := j.Parse("1,2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := []any{"1", "2"}; !reflect.DeepEqual(omPlainify(UnwrapUndefined(got)), want) {
		t.Errorf("build: got %#v, want %#v", UnwrapUndefined(got), want)
	}
}

// A list grown arbitrarily deep below the rule that allocated it, through
// the same serialized grammar in both ports.
//
// Free in TypeScript and Rust, which hand every holder the same list
// object. Here a slice is a value and the grown header was re-published
// one hop, so only the innermost push landed anywhere the owner could
// see: __start__'s @bubble$ read top's node and got the empty original.
//
// Same fixture as the TypeScript case of the same name.
func TestPushReachesTheListsOwnerFixtureParity(t *testing.T) {
	spec := fixtureSpec(t, "deep-push.fixture.json")
	j := Make()
	if err := j.Grammar(spec); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := j.Parse("1,2,3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := []any{"1", "2", "3"}; !reflect.DeepEqual(omPlainify(UnwrapUndefined(got)), want) {
		t.Errorf("build: got %#v, want %#v", UnwrapUndefined(got), want)
	}
}

// TestPushSurvivesReplacementWithListRef: the same replacement case, with
// Info.List on so the node is a ListRef wrapper rather than a bare slice.
//
// Go-only surface, so no shared fixture can reach it and nothing else
// would notice if the propagation only understood `[]any` — it would
// skip every list in info mode and quietly drop elements again.
func TestPushSurvivesReplacementWithListRef(t *testing.T) {
	grow := func(r *Rule) {
		builtinPushCfg(r, nil, nil)
	}

	// top holds the list; step replaced it and appends the second element.
	seed := ListRef{Val: []any{"1"}, Meta: map[string]any{}}
	top := &Rule{Node: seed}
	step := &Rule{Node: seed, Prev: top, Child: &Rule{Node: "2"}}
	grow(step)

	got, ok := listHeader(top.Node)
	if !ok {
		t.Fatalf("the replaced rule no longer holds a list: %#v", top.Node)
	}
	if want := []any{"1", "2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("replaced rule's list: got %#v, want %#v", got, want)
	}
	if _, isRef := top.Node.(ListRef); !isRef {
		t.Errorf("the ListRef wrapper must be preserved, got %T", top.Node)
	}
}

// A list grown ARBITRARILY DEEP below the rule that allocated it reaches
// that rule.
//
// The parent write-back covers the json idiom, where the pushing rule sits
// one level under the list's owner. A right-recursive repetition helper
// inherits the list and pushes from a new depth on every iteration, so a
// three-element list grows at three different depths — and before this,
// only the innermost push landed anywhere the owner could see. The owner
// kept the empty original, which is what `; @array` on an ABNF list idiom
// read back.
//
// The chain below is that shape: `owner` allocates and replaces itself
// with `step` (an `r:`, so `step.Prev` is the owner and its Parent is the
// owner's parent — following Parent alone walks straight past the owner),
// `step` pushes a helper, and the helper recurses.
func TestPushReachesTheListsOwnerFromAnyDepth(t *testing.T) {
	owner := &Rule{Name: "owner", Node: []any{}, Parent: NoRule, Prev: NoRule}
	// `r:` — seeded from the rule it replaced, parented above it.
	step := &Rule{Name: "step", Node: owner.Node, Parent: NoRule, Prev: owner,
		nodeOwner: owner}
	prev := step
	for i, val := range []any{"1", "2", "3"} {
		// Each iteration is a push: it inherits the same owner.
		iter := &Rule{Name: "iter", Node: prev.Node, Parent: prev, Prev: NoRule,
			nodeOwner: owner, Child: &Rule{Node: val}}
		builtinPushCfg(iter, nil, nil)
		if got, _ := listHeader(owner.Node); i+1 != len(got) {
			t.Fatalf("after %d pushes the owner holds %#v", i+1, got)
		}
		prev = iter
	}
	if got, _ := listHeader(owner.Node); !reflect.DeepEqual(got, []any{"1", "2", "3"}) {
		t.Errorf("owner's list: got %#v, want [1 2 3]", got)
	}
}

// The growth reaches the rule that ALLOCATED the list and stops there: a
// rule holding an unrelated list above it is not holding this one.
func TestPushStopsAtTheAllocatingRule(t *testing.T) {
	outer := &Rule{Name: "outer", Node: []any{"untouched"}, Parent: NoRule,
		Prev: NoRule}
	// owner ran @array$: its own list, so it is not holding outer's.
	owner := &Rule{Name: "owner", Node: []any{}, Parent: outer, Prev: NoRule}
	iter := &Rule{Name: "iter", Node: owner.Node, Parent: owner, Prev: NoRule,
		nodeOwner: owner, Child: &Rule{Node: "x"}}
	builtinPushCfg(iter, nil, nil)

	if got, _ := listHeader(owner.Node); !reflect.DeepEqual(got, []any{"x"}) {
		t.Errorf("owner's list: got %#v, want [x]", got)
	}
	if got, _ := listHeader(outer.Node); !reflect.DeepEqual(got, []any{"untouched"}) {
		t.Errorf("an unrelated list above the owner was clobbered: %#v", got)
	}
}

// @bubble$ lifting a child that is STILL CARRYING the inherited container
// must keep that container's owner, not claim ownership. Claiming it
// strands the rule that allocated the list, and a later push from deeper
// in the chain leaves that rule with a stale header where TypeScript's
// shared array stays complete. @value$ takes the same branch.
func TestLiftingAnInheritedListKeepsItsOwner(t *testing.T) {
	owner := &Rule{Name: "owner", Node: []any{}, Parent: NoRule, Prev: NoRule}
	mid := &Rule{Name: "mid", Node: owner.Node, Parent: owner, Prev: NoRule,
		nodeOwner: owner}
	// mid's child is carrying the very same inherited list.
	mid.Child = &Rule{Name: "kid", Node: owner.Node, Parent: mid,
		Prev: NoRule, nodeOwner: owner}

	builtinBubble(mid, nil)
	if mid.nodeHolder() != owner {
		t.Fatalf("bubble claimed ownership: holder is %v", mid.nodeHolder().Name)
	}

	deep := &Rule{Name: "deep", Node: mid.Node, Parent: mid, Prev: NoRule,
		nodeOwner: mid.nodeHolder(), Child: &Rule{Node: "y"}}
	builtinPushCfg(deep, nil, nil)
	if got, _ := listHeader(owner.Node); !reflect.DeepEqual(got, []any{"y"}) {
		t.Errorf("the allocating rule was stranded: got %#v, want [y]", got)
	}
}

// A replacement that allocated its OWN list must not overwrite the list of
// the rule it replaced — TypeScript would not, because the fresh
// allocation is a different object. Two distinct EMPTY lists cannot be
// told apart in Go, so the propagation declines rather than guesses.
func TestPushDoesNotClobberAFreshContainer(t *testing.T) {
	held := []any{}
	top := &Rule{Node: held}
	// step ran @array$ before its first push: its own, distinct, empty list.
	step := &Rule{Node: []any{}, Prev: top, Child: &Rule{Node: "x"}}
	builtinPushCfg(step, nil, nil)

	if got, _ := listHeader(top.Node); 0 != len(got) {
		t.Errorf("a fresh container clobbered the one it replaced: got %#v", got)
	}
	if got, _ := listHeader(step.Node); 1 != len(got) {
		t.Errorf("the replacement's own list should have grown: got %#v", got)
	}
}

// omPlainify recursively converts OrderedMap nodes to plain map[string]any
// (dropping order) so value-only comparisons against encoding/json can use
// reflect.DeepEqual.
func omPlainify(v any) any {
	switch m := v.(type) {
	case *OrderedMap:
		out := make(map[string]any, len(m.Vals))
		for k, val := range m.Vals {
			out[k] = omPlainify(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[k] = omPlainify(val)
		}
		return out
	case []any:
		out := make([]any, len(m))
		for i, val := range m {
			out[i] = omPlainify(val)
		}
		return out
	}
	return v
}

// TestObjectInsertionOrder pins the headline new behaviour: the default
// object node preserves the order keys are discovered while parsing, and a
// sort-configured builder sorts instead.
func TestObjectInsertionOrder(t *testing.T) {
	spec := fixtureSpec(t, "json-builder.fixture.json")
	if spec.Options == nil {
		spec.Options = &Options{}
	}
	spec.Options.Rule = &RuleOptions{Start: "val"}
	j := Make()
	if err := j.Grammar(spec); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := j.Parse(`{"zebra":1,"alpha":2,"mango":3}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	om, ok := got.(*OrderedMap)
	if !ok {
		t.Fatalf("want *OrderedMap, got %T", got)
	}
	if want := []string{"zebra", "alpha", "mango"}; !reflect.DeepEqual(om.Keys, want) {
		t.Errorf("insertion order: got %v, want %v", om.Keys, want)
	}
	// MarshalJSON round-trips in source order (not alphabetical).
	if b, _ := json.Marshal(om); string(b) != `{"zebra":1,"alpha":2,"mango":3}` {
		t.Errorf("MarshalJSON: got %s", b)
	}
}

// --- @fold$ (tail-repeat delivery) ---

// The shape @tabnas/abnf emits for `X = NR [ PL X ]`: a same-depth
// repeat where each iteration folds itself into the parent. Mirrors the
// TS test "@fold$ delivers a tail-repeat run to the parent as siblings".
func TestBuiltinFoldTailRepeat(t *testing.T) {
	tn := Make()
	err := tn.Grammar(&GrammarSpec{
		OptionsMap: map[string]any{
			"rule":  map[string]any{"start": "val"},
			"fixed": map[string]any{"token": map[string]any{"#PL": "+"}},
		},
		Rule: map[string]*GrammarRuleSpec{
			"val": {
				Open: []*GrammarAltSpec{{P: "add", A: "@node$",
					K: map[string]any{"node$": map[string]any{
						"init": true, "rule": "val", "kind": "user"}}}},
				Close: []*GrammarAltSpec{{A: "@capture$",
					K: map[string]any{"capture$": map[string]any{
						"rule": "val", "kind": "user"}}}},
			},
			"add": {
				Open: []*GrammarAltSpec{{S: []string{"#NR"}, A: "@node$",
					K: map[string]any{"node$": map[string]any{
						"init": true, "rule": "add", "kind": "user",
						"nterms": 1}}}},
				Close: []*GrammarAltSpec{
					{S: []string{"#PL"}, R: "add", A: "@fold$",
						K: map[string]any{"fold$": map[string]any{"cN": 1}}},
					{A: "@fold$"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tn.Parse("1+2+3")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := out.(map[string]any)
	if n == nil || n["src"] != "1+2+3" {
		t.Fatalf("root: got %v", out)
	}
	kids, _ := n["kids"].([]any)
	if len(kids) != 3 {
		t.Fatalf("kids: got %d, want 3: %v", len(kids), n)
	}
	for i, want := range []string{"1", "2", "3"} {
		k, _ := kids[i].(map[string]any)
		if k == nil || k["rule"] != "add" || k["src"] != want ||
			len(asAnySlice(k["kids"])) != 0 {
			t.Errorf("kid %d: got %v, want add(%s)", i, kids[i], want)
		}
	}
	// Single element: one iteration, no separator.
	out7, err := tn.Parse("7")
	if err != nil {
		t.Fatal(err)
	}
	n7, _ := out7.(map[string]any)
	if n7 == nil || n7["src"] != "7" || len(asAnySlice(n7["kids"])) != 1 {
		t.Fatalf("single: got %v", out7)
	}
}
