package tabnas

import (
	"math"
	"regexp"
	"strings"
	"testing"
)

// --- cloneMeta / mergeMeta ---

func TestCloneMeta(t *testing.T) {
	if cloneMeta(nil) != nil {
		t.Error("cloneMeta(nil) should be nil")
	}
	src := map[string]any{"a": 1}
	cp := cloneMeta(src)
	cp["a"] = 2
	if src["a"] != 1 {
		t.Error("cloneMeta should produce an independent copy")
	}
}

func TestMergeMeta(t *testing.T) {
	if mergeMeta(nil, nil) != nil {
		t.Error("mergeMeta(nil,nil) should be nil")
	}
	got := mergeMeta(map[string]any{"a": 1, "b": 1}, map[string]any{"b": 2})
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("expected {a:1 b:2}, got %v", got)
	}
	// One-sided merges.
	if got := mergeMeta(map[string]any{"x": 1}, nil); got["x"] != 1 {
		t.Errorf("base-only merge failed: %v", got)
	}
	if got := mergeMeta(nil, map[string]any{"y": 2}); got["y"] != 2 {
		t.Errorf("over-only merge failed: %v", got)
	}
}

func TestDeepMergeMapRefMeta(t *testing.T) {
	// MapRef merge preserves the wrapper and merges Meta maps.
	base := MapRef{Val: map[string]any{"a": 1}, Meta: map[string]any{"m1": 1}}
	over := MapRef{Val: map[string]any{"b": 2}, Meta: map[string]any{"m2": 2}}
	out := Deep(base, over).(MapRef)
	if out.Val["a"] != 1 || out.Val["b"] != 2 {
		t.Errorf("MapRef val merge failed: %v", out.Val)
	}
	if out.Meta["m1"] != 1 || out.Meta["m2"] != 2 {
		t.Errorf("MapRef meta merge failed: %v", out.Meta)
	}
}

func TestDeepMergeListRefChild(t *testing.T) {
	// ListRef merge: Child fields merge across base and over.
	base := ListRef{Val: []any{1}, Child: map[string]any{"a": 1}, Meta: map[string]any{"m": 1}}
	over := ListRef{Val: []any{2, 3}, Child: map[string]any{"b": 2}}
	out := Deep(base, over).(ListRef)
	if len(out.Val) != 2 {
		t.Errorf("ListRef val merge failed: %v", out.Val)
	}
	child := out.Child.(map[string]any)
	if child["a"] != 1 || child["b"] != 2 {
		t.Errorf("ListRef child merge failed: %v", child)
	}

	// Over-only child.
	out = Deep(ListRef{Val: []any{1}}, ListRef{Val: []any{2}, Child: "c"}).(ListRef)
	if out.Child != "c" {
		t.Errorf("over-only child failed: %v", out.Child)
	}
	// Base-only child.
	out = Deep(ListRef{Val: []any{1}, Child: "b"}, ListRef{Val: []any{2}}).(ListRef)
	if out.Child != "b" {
		t.Errorf("base-only child failed: %v", out.Child)
	}
}

func TestDeepMergeArrayLengths(t *testing.T) {
	// Base longer than over: tail preserved from base.
	out := Deep([]any{1, 2, 3}, []any{9}).([]any)
	if out[0] != 9 || out[1] != 2 || out[2] != 3 {
		t.Errorf("expected [9 2 3], got %v", out)
	}
	// Over nil replaces.
	if got := deepMerge(1, nil); got != nil {
		t.Errorf("nil over should replace, got %v", got)
	}
	// Undefined over preserves base.
	if got := deepMerge(1, Undefined); got != 1 {
		t.Errorf("Undefined over should preserve base, got %v", got)
	}
}

// --- deepMergeStruct branches ---

type covInner struct{ A string }
type covOuter struct {
	S   string
	P   *covInner
	B   *bool
	M   map[string]int
	Sl  []int
	unx int //nolint:unused — exercises the unexported-field skip branch
}

func TestDeepMergeStructBranches(t *testing.T) {
	yes := true
	base := covOuter{S: "base", P: &covInner{A: "pa"}, M: map[string]int{"a": 1}, Sl: []int{1}}
	over := covOuter{S: "over", P: &covInner{A: "po"}, B: &yes, M: map[string]int{"b": 2}, Sl: []int{2, 3}}

	merged, ok := deepMergeStruct(base, over)
	if !ok {
		t.Fatal("expected struct merge to apply")
	}
	m := merged.(covOuter)
	if m.S != "over" {
		t.Errorf("string field: over wins, got %q", m.S)
	}
	if m.P.A != "po" {
		t.Errorf("ptr-to-struct recurse: got %q", m.P.A)
	}
	if m.B == nil || !*m.B {
		t.Error("ptr-to-primitive: over wins")
	}
	if m.M["a"] != 1 || m.M["b"] != 2 {
		t.Errorf("map field merge failed: %v", m.M)
	}
	if len(m.Sl) != 2 {
		t.Errorf("slice: over wins, got %v", m.Sl)
	}
}

func TestDeepMergeStructPointerForms(t *testing.T) {
	// Pointer inputs round-trip as pointers.
	merged, ok := deepMergeStruct(&covInner{A: "x"}, &covInner{A: "y"})
	if !ok {
		t.Fatal("pointer merge should apply")
	}
	if merged.(*covInner).A != "y" {
		t.Errorf("expected y, got %v", merged)
	}

	// Nil base pointer → over wins.
	var nilInner *covInner
	merged, ok = deepMergeStruct(nilInner, &covInner{A: "o"})
	if !ok || merged.(*covInner).A != "o" {
		t.Errorf("nil base ptr: expected over, got %v ok=%v", merged, ok)
	}
	// Nil over pointer → base wins.
	merged, ok = deepMergeStruct(&covInner{A: "b"}, nilInner)
	if !ok || merged.(*covInner).A != "b" {
		t.Errorf("nil over ptr: expected base, got %v ok=%v", merged, ok)
	}

	// Non-struct values → not applicable.
	if _, ok := deepMergeStruct(1, 2); ok {
		t.Error("ints are not structs")
	}
	// Different struct types → not applicable.
	if _, ok := deepMergeStruct(covInner{}, covOuter{}); ok {
		t.Error("different struct types should not merge")
	}
	// Nil values → not applicable.
	if _, ok := deepMergeStruct(nil, covInner{}); ok {
		t.Error("nil base should not merge")
	}
	// Nil base pointer with non-struct over → not applicable.
	if _, ok := deepMergeStruct(nilInner, 5); ok {
		t.Error("nil ptr base with int over should not merge")
	}
	// Nil over pointer with non-struct base → not applicable.
	if _, ok := deepMergeStruct(5, nilInner); ok {
		t.Error("int base with nil ptr over should not merge")
	}
}

// --- Str ---

func TestStrBranches(t *testing.T) {
	if Str("x", 0) != "" {
		t.Error("maxlen<=0 → empty")
	}
	if Str(true, 10) != "true" || Str(false, 10) != "false" {
		t.Error("bool formatting failed")
	}
	if Str(nil, 10) != "null" {
		t.Error("nil → null")
	}
	if Str(float64(3), 10) != "3" {
		t.Error("integral float → int form")
	}
	if Str(float64(3.5), 10) != "3.5" {
		t.Error("fractional float failed")
	}
	// Default: JSON marshalling.
	if got := Str(map[string]any{"a": 1}, 20); got != `{"a":1}` {
		t.Errorf("map → JSON, got %q", got)
	}
	// JSON-unmarshalable (func) falls back to %v formatting.
	if got := Str(func() {}, 30); got == "" {
		t.Error("func value should produce non-empty fallback")
	}
	// Truncation with ellipsis.
	if got := Str("abcdefghij", 6); got != "abc..." {
		t.Errorf("truncation failed: %q", got)
	}
	// Tiny maxlen truncation.
	if got := Str("abcdefghij", 2); got != ".." {
		t.Errorf("tiny truncation failed: %q", got)
	}
	// Newlines snipped to dots.
	if got := Str("a\nb\tc", 10); got != "a.b.c" {
		t.Errorf("snip failed: %q", got)
	}
}

func TestSnip(t *testing.T) {
	if Snip("abc", 0) != "" {
		t.Error("maxlen<=0 → empty")
	}
	if Snip("a\r\n\tb", 10) != "a...b" {
		t.Errorf("snip replace failed: %q", Snip("a\r\n\tb", 10))
	}
}

// --- StrInject / formatInjectValue / formatCompactValue ---

func TestStrInjectValueKinds(t *testing.T) {
	vals := map[string]any{
		"s":   "str",
		"i":   float64(3),
		"f":   float64(3.5),
		"bt":  true,
		"bf":  false,
		"nil": nil,
		"m":   map[string]any{"k": float64(1)},
		"arr": []any{float64(1), "x", true, nil, float64(2.5), []any{}},
		"oth": 42, // non-float numeric → default %v branch
	}
	out := StrInject("{s}|{i}|{f}|{bt}|{bf}|{nil}|{oth}", vals)
	if out != "str|3|3.5|true|false|null|42" {
		t.Errorf("scalar injection failed: %q", out)
	}

	out = StrInject("{m}", vals)
	if out != "{k:1}" {
		t.Errorf("map injection failed: %q", out)
	}

	out = StrInject("{arr}", vals)
	if out != "[1,x,true,null,2.5,[]]" {
		t.Errorf("array injection failed: %q", out)
	}

	// Nested compact map.
	out = StrInject("{n}", map[string]any{
		"n": map[string]any{"a": map[string]any{"b": false}},
	})
	if out != "{a:{b:false}}" {
		t.Errorf("nested compact failed: %q", out)
	}

	// Default branch of formatCompactValue (non-float numeric).
	out = StrInject("{x}", map[string]any{"x": []any{7}})
	if out != "[7]" {
		t.Errorf("compact default failed: %q", out)
	}
}

func TestStrInjectPaths(t *testing.T) {
	// Array vals with index paths.
	out := StrInject("{0}-{1.k}", []any{"a", map[string]any{"k": "b"}})
	if out != "a-b" {
		t.Errorf("array path injection failed: %q", out)
	}
	// Missing key keeps the placeholder.
	out = StrInject("{missing}", map[string]any{"a": 1})
	if out != "{missing}" {
		t.Errorf("missing key should keep placeholder: %q", out)
	}
	// Bad array index keeps the placeholder.
	out = StrInject("{9}", []any{"a"})
	if out != "{9}" {
		t.Errorf("bad index should keep placeholder: %q", out)
	}
	// Path into a scalar keeps the placeholder.
	out = StrInject("{a.b}", map[string]any{"a": 1})
	if out != "{a.b}" {
		t.Errorf("scalar path should keep placeholder: %q", out)
	}
	// Unclosed brace is emitted verbatim.
	out = StrInject("x{abc", map[string]any{})
	if out != "x{abc" {
		t.Errorf("unclosed brace failed: %q", out)
	}
	// Empty template / non-container vals.
	if StrInject("", map[string]any{}) != "" {
		t.Error("empty template → empty")
	}
	if StrInject("{a}", "notmap") != "{a}" {
		t.Error("non-container vals → unchanged template")
	}
}

// --- LookupRef ---

func TestLookupRef(t *testing.T) {
	if LookupRef(nil, "@x") != nil {
		t.Error("nil ref map → nil")
	}
	ref := map[FuncRef]any{"@x": 1}
	if LookupRef(ref, "@x") != 1 {
		t.Error("hit failed")
	}
	if LookupRef(ref, "@y") != nil {
		t.Error("miss should be nil")
	}
}

// --- MapToOptions: remaining option keys ---

func TestMapToOptionsAllKeys(t *testing.T) {
	re := regexp.MustCompile(`^x`)
	mergeFn := func(prev, curr any, r *Rule, ctx *Context) any { return curr }
	valFn := func(m []string) any { return true }

	opts := MapToOptions(map[string]any{
		"tag":  "t",
		"safe": map[string]any{"key": false},
		"fixed": map[string]any{
			"lex": true,
		},
		"space": map[string]any{"lex": true, "chars": " "},
		"line": map[string]any{
			"lex": true, "chars": "\n", "rowChars": "\n", "single": true,
		},
		"text": map[string]any{"lex": true},
		"number": map[string]any{
			"lex": true, "hex": true, "oct": false, "bin": false,
			"sep": "_", "exclude": re,
		},
		"comment": map[string]any{
			"lex": true,
			"def": map[string]any{
				"slash": map[string]any{
					"line": true, "start": "//", "end": "\n",
					"lex": true, "eatline": false,
					"suffix": []any{"!!", 5},
				},
				"hash": map[string]any{"start": "#", "suffix": "??"},
				"str":  map[string]any{"start": ";", "suffix": []string{"s1", "s2"}},
				"bad":  "not-a-map",
			},
		},
		"string": map[string]any{
			"lex": true, "chars": `"'`, "multiChars": "`",
			"escapeChar": "\\", "allowUnknown": true, "abandon": false,
			"escape":  map[string]any{"z": "Z", "n": 7},
			"replace": map[string]any{"o": "0", "": "skip"},
		},
		"map":  map[string]any{"extend": true, "child": false, "merge": mergeFn},
		"list": map[string]any{"property": true, "pair": false, "child": true},
		"value": map[string]any{
			"lex": true,
			"def": map[string]any{
				"yes":  map[string]any{"val": true, "match": re, "consume": true},
				"fn":   map[string]any{"val": valFn},
				"gone": nil,
				"off":  false,
			},
		},
		"ender": []any{";", 9},
		"rule": map[string]any{
			"start": "val", "finish": true, "include": "json", "exclude": "tabnas",
		},
		"lex":   map[string]any{"empty": true, "emptyResult": "E"},
		"error": map[string]any{"unexpected": "boom {src}", "n": 5},
		"hint":  map[string]any{"unexpected": "try again", "n": 5},
		"errmsg": map[string]any{
			"name": "myparser", "suffix": false, "link": "http://x",
		},
		"match": map[string]any{
			"lex": true,
			"token": map[string]any{
				"#A":   re,
				"#BAD": "not-a-regexp",
			},
			"value": map[string]any{
				"v":    map[string]any{"match": re, "val": valFn},
				"vbad": "not-a-map",
			},
		},
		"tokenSet": map[string]any{
			"S1": []any{"#TX", 5},
			"S2": []string{"#NR"},
		},
		"info": map[string]any{
			"map": true, "list": false, "text": true, "marker": "__m__",
		},
		"color": map[string]any{
			"active": false, "reset": "r", "hi": "h", "lo": "l", "line": "n",
		},
	})

	if opts.Tag != "t" {
		t.Error("tag")
	}
	if opts.Safe == nil || *opts.Safe.Key != false {
		t.Error("safe.key")
	}
	if opts.Fixed == nil || !*opts.Fixed.Lex {
		t.Error("fixed.lex")
	}
	if opts.Space == nil || opts.Space.Chars != " " {
		t.Error("space")
	}
	if opts.Line == nil || opts.Line.Chars != "\n" || opts.Line.RowChars != "\n" || !*opts.Line.Single {
		t.Error("line")
	}
	if opts.Text == nil || !*opts.Text.Lex {
		t.Error("text")
	}
	if opts.Number == nil || !*opts.Number.Hex || *opts.Number.Oct || opts.Number.Sep != "_" || opts.Number.Exclude == nil {
		t.Error("number")
	}
	if !opts.Number.Exclude("x1") || opts.Number.Exclude("y") {
		t.Error("number.exclude regexp wrapping")
	}
	if opts.Comment == nil || len(opts.Comment.Def) != 3 {
		t.Errorf("comment.def: %v", opts.Comment.Def)
	}
	slash := opts.Comment.Def["slash"]
	if !slash.Line || slash.Start != "//" || slash.End != "\n" || !*slash.Lex || *slash.EatLine {
		t.Errorf("comment.def.slash: %+v", slash)
	}
	if sfx, ok := slash.Suffix.([]string); !ok || len(sfx) != 1 || sfx[0] != "!!" {
		t.Errorf("comment suffix []any: %v", slash.Suffix)
	}
	if opts.Comment.Def["hash"].Suffix != "??" {
		t.Error("comment suffix string")
	}
	if sfx, ok := opts.Comment.Def["str"].Suffix.([]string); !ok || len(sfx) != 2 {
		t.Error("comment suffix []string")
	}
	if opts.String == nil || opts.String.Chars != `"'` || opts.String.MultiChars != "`" ||
		opts.String.EscapeChar != "\\" || !*opts.String.AllowUnknown || *opts.String.Abandon {
		t.Error("string")
	}
	if opts.String.Escape["z"] != "Z" || len(opts.String.Escape) != 1 {
		t.Errorf("string.escape: %v", opts.String.Escape)
	}
	if opts.String.Replace['o'] != "0" || len(opts.String.Replace) != 1 {
		t.Errorf("string.replace: %v", opts.String.Replace)
	}
	if opts.Map == nil || !*opts.Map.Extend || *opts.Map.Child || opts.Map.Merge == nil {
		t.Error("map")
	}
	if opts.List == nil || !*opts.List.Property || *opts.List.Pair || !*opts.List.Child {
		t.Error("list")
	}
	if opts.Value == nil || len(opts.Value.Def) != 2 {
		t.Errorf("value.def: %v", opts.Value.Def)
	}
	yes := opts.Value.Def["yes"]
	if yes.Val != true || yes.Match == nil || !yes.Consume {
		t.Errorf("value.def.yes: %+v", yes)
	}
	if opts.Value.Def["fn"].ValFunc == nil {
		t.Error("value.def.fn ValFunc")
	}
	if len(opts.Ender) != 1 || opts.Ender[0] != ";" {
		t.Errorf("ender: %v", opts.Ender)
	}
	if opts.Rule == nil || opts.Rule.Start != "val" || !*opts.Rule.Finish ||
		opts.Rule.Include != "json" || opts.Rule.Exclude != "tabnas" {
		t.Error("rule")
	}
	if opts.Lex == nil || !*opts.Lex.Empty || opts.Lex.EmptyResult != "E" {
		t.Error("lex")
	}
	if opts.Error["unexpected"] != "boom {src}" || len(opts.Error) != 1 {
		t.Errorf("error: %v", opts.Error)
	}
	if opts.Hint["unexpected"] != "try again" || len(opts.Hint) != 1 {
		t.Errorf("hint: %v", opts.Hint)
	}
	if opts.ErrMsg == nil || opts.ErrMsg.Name != "myparser" ||
		opts.ErrMsg.Suffix != false || opts.ErrMsg.Link != "http://x" {
		t.Error("errmsg")
	}
	if opts.Match == nil || !*opts.Match.Lex {
		t.Error("match.lex")
	}
	if len(opts.Match.Token) != 1 || opts.Match.Token["#A"] == nil {
		t.Errorf("match.token: %v", opts.Match.Token)
	}
	if len(opts.Match.Value) != 1 || opts.Match.Value["v"].Match == nil || opts.Match.Value["v"].Val == nil {
		t.Errorf("match.value: %v", opts.Match.Value)
	}
	if len(opts.TokenSet["S1"]) != 1 || opts.TokenSet["S1"][0] != "#TX" {
		t.Errorf("tokenSet S1: %v", opts.TokenSet)
	}
	if len(opts.TokenSet["S2"]) != 1 || opts.TokenSet["S2"][0] != "#NR" {
		t.Errorf("tokenSet S2: %v", opts.TokenSet)
	}
	if opts.Info == nil || !*opts.Info.Map || *opts.Info.List || !*opts.Info.Text || opts.Info.Marker != "__m__" {
		t.Error("info")
	}
	if opts.Color == nil || *opts.Color.Active || opts.Color.Reset != "r" ||
		opts.Color.Hi != "h" || opts.Color.Lo != "l" || opts.Color.Line != "n" {
		t.Error("color")
	}
}

func TestMapToOptionsNumberExcludeFunc(t *testing.T) {
	fn := func(s string) bool { return strings.HasPrefix(s, "0") }
	opts := MapToOptions(map[string]any{
		"number": map[string]any{"exclude": fn},
	})
	if opts.Number.Exclude == nil || !opts.Number.Exclude("01") {
		t.Error("function-form number.exclude not passed through")
	}
}

func TestMapToOptionsEnderString(t *testing.T) {
	opts := MapToOptions(map[string]any{"ender": ";"})
	if len(opts.Ender) != 1 || opts.Ender[0] != ";" {
		t.Errorf("string ender: %v", opts.Ender)
	}
}

// --- ModList edge cases ---

func TestModListEdgeCases(t *testing.T) {
	// nil opts / nil list pass through.
	if got := ModList(nil, &ModListOpts{}); got != nil {
		t.Error("nil list passthrough")
	}
	list := []any{"a"}
	if got := ModList(list, nil); len(got) != 1 {
		t.Error("nil opts passthrough")
	}

	// Negative and out-of-range deletes.
	got := ModList([]any{"a", "b", "c"}, &ModListOpts{Delete: []int{-1, 99, -99}})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("negative delete failed: %v", got)
	}

	// Move with negative indices.
	got = ModList([]any{"a", "b", "c"}, &ModListOpts{Move: []int{-1, 0}})
	if got[0] != "c" || got[1] != "a" || got[2] != "b" {
		t.Errorf("negative move failed: %v", got)
	}

	// Custom returning nil keeps the list.
	got = ModList([]any{"a"}, &ModListOpts{Custom: func(l []any) []any { return nil }})
	if len(got) != 1 {
		t.Errorf("custom nil failed: %v", got)
	}
	// Custom replacing the list.
	got = ModList([]any{"a"}, &ModListOpts{Custom: func(l []any) []any { return []any{"z"} }})
	if len(got) != 1 || got[0] != "z" {
		t.Errorf("custom replace failed: %v", got)
	}
}

// --- parseNumericString ---

func TestParseNumericString(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"1", 1},
		{"-1", -1},
		{"+2", 2},
		{"0xff", 255},
		{"-0x10", -16},
		{"0o17", 15},
		{"0b101", 5},
		{"1_000", 1000},
		{"-0", 0},
		{"1.5e2", 150},
	}
	for _, tt := range tests {
		if got := parseNumericString(tt.in); got != tt.want {
			t.Errorf("parseNumericString(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
	// NaN cases.
	for _, in := range []string{"", "xyz", "0xzz", "0o9", "0b3"} {
		if got := parseNumericString(in); !math.IsNaN(got) {
			t.Errorf("parseNumericString(%q) = %v, want NaN", in, got)
		}
	}
}

// --- RequireRef ---

func TestRequireRefNoMap(t *testing.T) {
	_, err := RequireRef(nil, "@x", "action")
	if err == nil || !strings.Contains(err.Error(), "no ref map") {
		t.Errorf("expected no-ref-map error, got %v", err)
	}
	ref := map[FuncRef]any{"@x": 1}
	v, err := RequireRef(ref, "@x", "action")
	if err != nil || v != 1 {
		t.Errorf("hit failed: %v %v", v, err)
	}
	_, err = RequireRef(ref, "@y", "action")
	if err == nil {
		t.Error("miss should error")
	}
}

// --- RebuildMatchTokensSorted: fn precedence over regexp ---

func TestRebuildMatchTokensSortedFnPrecedence(t *testing.T) {
	cfg := DefaultLexConfig()
	cfg.MatchTokens = map[Tin]*regexp.Regexp{
		TinST: regexp.MustCompile("a"),
		TinTX: regexp.MustCompile("b"),
	}
	cfg.MatchTokenFns = map[Tin]LexMatcher{
		TinST: func(lex *Lex, rule *Rule) *Token { return nil },
	}
	cfg.RebuildMatchTokensSorted()
	if len(cfg.MatchTokensSorted) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(cfg.MatchTokensSorted))
	}
	for _, e := range cfg.MatchTokensSorted {
		if e.Tin == TinST && e.Fn == nil {
			t.Error("fn matcher should take precedence over regexp for same tin")
		}
	}
}
