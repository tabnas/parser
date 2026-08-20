package tabnas

// regexflags_test.go — the cross-runtime encoding of regex terminals that
// need JavaScript's `u` flag.
//
// The serialized `@/pattern/flags` form is shared between the runtimes and
// carries JS flags, because TypeScript writes them natively. Go lowers
// them to RE2: `u` is DROPPED, because RE2 is natively rune-based and so
// already is what `u` asks JavaScript to be.
//
// That equivalence is the whole decision, so it is asserted here rather
// than assumed. The four terminal shapes are the ones a real grammar
// emits — a BMP class needs no flag, but an astral class, a NEGATED class
// and `.` all carry `u` in TypeScript, which is why this is not a corner
// case. `ts/test/regex-flags.test.js` runs the same table.

import (
	"regexp"
	"testing"
)

// The shared table. `want` is what BOTH runtimes must answer; the TS side
// asserts the same rows with the `u` flag applied.
var regexFlagCases = []struct {
	name    string
	pattern string // as serialized, in JS syntax
	flags   string
	input   string
	want    bool
}{
	{"bmp class, no flag", `^[a-z]$`, "", "q", true},
	{"bmp class rejects other", `^[a-z]$`, "", "Q", false},
	{"bmp class, no flag, unaffected by u", `^[a-z]$`, "u", "q", true},

	// An astral MEMBER. JS needs `u` for \u{...} at all.
	{"astral class", `^[\u{1F600}-\u{1F64F}]$`, "u", "😀", true},
	// 😐 is U+1F610 and IS inside the range; 🚀 is U+1F680 and is not.
	{"astral class accepts another member", `^[\u{1F600}-\u{1F64F}]$`, "u", "😐", true},
	{"astral class rejects outside", `^[\u{1F600}-\u{1F64F}]$`, "u", "🚀", false},

	// A negated class: the COMPLEMENT contains astral code points, so this
	// carries `u` even though nothing astral is written in it.
	{"negated class takes an astral char whole", `^[^\n]$`, "u", "😀", true},
	{"negated class is one char, not two", `^[^\n]{2}$`, "u", "😀", false},
	{"negated class rejects its exclusion", `^[^\n]$`, "u", "\n", false},

	// `.` — any character, astral included.
	{"dot takes an astral char whole", `^.$`, "u", "😀", true},
	{"dot{2} does NOT split one astral char", `^.{2}$`, "u", "😀", false},
	{"dot{2} takes two astral chars", `^.{2}$`, "u", "😀😀", true},
	{"dot is still one BMP char", `^.$`, "u", "q", true},
}

// The `u`-flagged patterns must COMPILE and match exactly as TypeScript's
// do with the flag on. This is the claim the decision rests on: for RE2,
// `u` is a no-op rather than an error.
func TestSerializedRegexUnicodeFlag(t *testing.T) {
	for _, c := range regexFlagCases {
		re := compileSerializedRegex(c.pattern, c.flags)
		if re == nil {
			t.Errorf("%s: @/%s/%s did not compile", c.name, c.pattern, c.flags)
			continue
		}
		if got := re.MatchString(c.input); got != c.want {
			t.Errorf("%s: @/%s/%s against %q = %v, want %v",
				c.name, c.pattern, c.flags, c.input, got, c.want)
		}
	}
}

// Flags whose meaning RE2 shares are kept; the ones that only govern the
// JS matcher's statefulness are dropped; the one that genuinely changes
// what a class MEANS is refused.
func TestSerializedRegexFlagLowering(t *testing.T) {
	for _, c := range []struct {
		flags string
		want  string
		ok    bool
	}{
		{"", "", true},
		{"i", "i", true},
		{"m", "m", true},
		{"s", "s", true},
		{"ims", "ims", true},
		{"u", "", true}, // no RE2 counterpart needed
		{"g", "", true}, // matcher statefulness, not language
		{"y", "", true}, // sticky
		{"d", "", true}, // match indices
		{"gimsuy", "ims", true},
		{"v", "", false}, // unicodeSets CHANGES class meaning
		{"U", "", false}, // RE2 would accept it as "swap greedy"
		{"z", "", false}, // unknown
	} {
		got, ok := jsRegexFlagsToGo(c.flags)
		if ok != c.ok || got != c.want {
			t.Errorf("jsRegexFlagsToGo(%q) = %q,%v; want %q,%v",
				c.flags, got, ok, c.want, c.ok)
		}
	}
}

// `U` is the reason an unknown flag is refused rather than ignored: RE2
// accepts it, and it means something else entirely. Passing it through
// would silently change the language a grammar matches.
func TestSerializedRegexRefusesGreedySwapFlag(t *testing.T) {
	if re := compileSerializedRegex(`^a+`, "U"); re != nil {
		t.Errorf("flag U must be refused, not passed to RE2 (it means "+
			"swap-greedy there): got %v", re)
	}
	// Proof that it would have changed the match, had it been passed.
	greedy := regexp.MustCompile(`^a+`).FindString("aaa")
	swapped := regexp.MustCompile(`(?U)^a+`).FindString("aaa")
	if greedy == swapped {
		t.Fatalf("expected (?U) to change the match; both gave %q", greedy)
	}
}

// A serialized regex the engine cannot build leaves the original string in
// place, which MapToOptions turns into a loud install error. Silence here
// would mis-lex input instead.
func TestSerializedRegexUnsupportedStaysAString(t *testing.T) {
	for _, s := range []string{
		`@/^(?=x)/`,  // lookahead: no RE2 equivalent
		`@/^[a-z]/v`, // refused flag
	} {
		got := ResolveFuncRefs(s, nil)
		if _, isRe := got.(*regexp.Regexp); isRe {
			t.Errorf("%s should not have compiled", s)
		}
		if got != any(s) {
			t.Errorf("%s: expected the original string back, got %#v", s, got)
		}
	}
}

// End to end: a grammar whose match tokens carry `u` installs and parses.
// The four shapes above reach the lexer through the serialized form, which
// is the path a compiled GBNF grammar actually takes.
func TestSerializedRegexTokensParse(t *testing.T) {
	// The real path a serialized grammar takes: ResolveFuncRefs turns the
	// `@~/…/u` strings into regexes, then MapToOptions reads them.
	raw := map[string]any{
		"match": map[string]any{
			"token": map[string]any{
				"#EMOJI": `@~/^[\u{1F600}-\u{1F64F}]+/u`,
				"#ANY":   `@~/^[^\n]/u`,
			},
		},
	}
	j := Make(MapToOptions(ResolveFuncRefs(raw, nil).(map[string]any)))
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#EMOJI")}},
			A: func(r *Rule, ctx *Context) { r.Node = "emoji:" + r.O0.Src },
		})
	})

	out, err := j.Parse("😀😀")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out != "emoji:😀😀" {
		t.Errorf("got %#v, want %q", out, "emoji:😀😀")
	}
}

// A serialized `value.def` entry can only carry a LITERAL `val` — JSON
// has no functions, and `@REF` reaches only names the host registered.
//
// This port has always taken one: `ValueDef.Val` sits alongside
// `ValFunc`. TypeScript called `val` unconditionally, so a string threw
// a TypeError that its matcher loop turned into a #BD bad token — a
// legal shared grammar reporting `unexpected` on VALID input, naming the
// character rather than the option. Repaired there under ADR-13;
// ts/test/regex-flags.test.js asserts the same two shapes. Audit item
// P10.
//
// Pinned here so the shape this port accepts cannot quietly narrow to
// match the defect.
func TestSerializedValueDefTakesALiteralVal(t *testing.T) {
	const spec = `{"options":{"value":{"def":{"kay":{"match":"@/^k$/i","val":"KAY"}}}}}`

	tn := Make(Options{Rule: &RuleOptions{Start: "top", Exclude: "tabnas,imp"}})
	gs, err := GrammarSpecFromJSON([]byte(spec))
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if err := tn.Grammar(gs, nil); err != nil {
		t.Fatalf("grammar: %v", err)
	}
	tn.Rule("top", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinVL}},
			A: func(r *Rule, ctx *Context) { r.Node = r.O0.Val }})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})

	v, perr := tn.Parse("k")
	if perr != nil {
		t.Fatalf("literal val rejected: %v", perr)
	}
	if v != "KAY" {
		t.Errorf("got %#v, want %q", v, "KAY")
	}
}
