/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// The options overlay merges as TypeScript's does (#151): slices are
// merged index-wise, maps of definitions recurse into an entry both
// sides carry, and tokenSet is index-wise onto the default set. This
// port replaced all three wholesale, and the split was invisible to any
// fixture that drove Deep directly, so these drive the OPTIONS PIPELINE
// (Make, SetOptions, a serialized spec) and assert on what the instance
// then does. ts/test/options-overlay.test.js asserts the same behaviour
// with the same overlays.

import (
	"reflect"
	"regexp"
	"sort"
	"testing"
)

func tinNames(j *Tabnas, tins []Tin) []string {
	out := make([]string, 0, len(tins))
	for _, t := range tins {
		out = append(out, j.tokenName(t))
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Class D: tokenSet merges index-wise onto the default set. A shorter
// slice keeps the default tail; an empty name removes its position, as
// `null` does in TypeScript.
func TestTokenSetOverlayIsIndexWise(t *testing.T) {
	for label, j := range map[string]*Tabnas{
		"Make":       Make(Options{TokenSet: map[string][]string{"IGNORE": {"#SP"}}}),
		"SetOptions": Make().SetOptions(Options{TokenSet: map[string][]string{"IGNORE": {"#SP"}}}),
	} {
		if got := tinNames(j, j.TokenSet("IGNORE")); !reflect.DeepEqual(got, []string{"#SP", "#LN", "#CM"}) {
			t.Errorf("%s: IGNORE [#SP] over the default gave %v, want the default set unchanged", label, got)
		}
	}
	j := Make(Options{TokenSet: map[string][]string{"IGNORE": {"#SP", "", ""}}})
	if got := tinNames(j, j.TokenSet("IGNORE")); !reflect.DeepEqual(got, []string{"#SP"}) {
		t.Errorf("IGNORE [#SP \"\" \"\"] gave %v, want [#SP]", got)
	}

	// Through a serialized spec, where the removal is a JSON null. The
	// grammar reads #TX then #LN: with the default IGNORE set intact the
	// line token is skipped and `a\n` cannot match; with #LN removed it
	// can.
	for _, c := range []struct {
		set  string
		want bool
	}{
		{`["#SP"]`, false},
		{`["#SP", null, null]`, true},
	} {
		gs, err := GrammarSpecFromJSON([]byte(`{"clear":true,
			"options":{"rule":{"start":"top"},"tokenSet":{"IGNORE":` + c.set + `}},
			"rule":{"top":{"open":[{"s":["#TX","#LN"]}],"close":[{"s":["#ZZ"]}]}}}`))
		if err != nil {
			t.Fatal(err)
		}
		j := Make()
		if err := j.Grammar(gs); err != nil {
			t.Fatal(err)
		}
		_, perr := j.Parse("a\n")
		if (perr == nil) != c.want {
			t.Errorf("IGNORE %s: parse of \"a\\n\" err=%v, want ok=%v", c.set, perr, c.want)
		}
	}
}

// Class C: a map of definitions recurses into the entry. Overriding one
// field of the hash comment keeps its other fields and keeps the other
// two definitions; a nil entry removes one.
func TestCommentDefOverlayRecursesIntoTheEntry(t *testing.T) {
	j := Make(Options{Comment: &CommentOptions{Def: map[string]*CommentDef{
		"hash": {Start: "%"},
	}}})
	cfg := j.Config()
	if got := cfg.CommentLine; !reflect.DeepEqual(got, []string{"//", "%"}) {
		t.Errorf("line comment markers %v, want [// %%]: hash kept line:true and slash survived", got)
	}
	if len(cfg.CommentBlock) != 1 || cfg.CommentBlock[0][0] != "/*" {
		t.Errorf("block comment definition lost: %v", cfg.CommentBlock)
	}
	if _, err := j.Parse("% c\n"); err != nil {
		t.Errorf("%% should now open a line comment: %v", err)
	}

	removed := Make(Options{Comment: &CommentOptions{Def: map[string]*CommentDef{"hash": nil}}})
	if got := removed.Config().CommentLine; !reflect.DeepEqual(got, []string{"//"}) {
		t.Errorf("nil entry did not remove hash: %v", got)
	}
}

// Class C, second site: value.def keeps the built-in keywords when one
// is added.
func TestValueDefOverlayKeepsTheDefaults(t *testing.T) {
	j := Make(Options{Value: &ValueOptions{Def: map[string]*ValueDef{"yes": {Val: 1}}}})
	if got := sortedKeys(j.Config().ValueDef); !reflect.DeepEqual(got, []string{"false", "null", "true", "yes"}) {
		t.Errorf("value.def = %v, want the defaults plus yes", got)
	}
}

// Class B: slices merge index-wise; an overlay index wins, positions
// beyond it keep the base.
func TestSliceOverlayIsIndexWise(t *testing.T) {
	j := Make(Options{Ender: []string{"x", "y"}})
	j.SetOptions(Options{Ender: []string{"z"}})
	if got := j.Options().Ender; !reflect.DeepEqual(got, []string{"z", "y"}) {
		t.Errorf("ender = %v, want [z y]", got)
	}
	if got := j.Config().EnderChars; !got['z'] || !got['y'] || got['x'] {
		t.Errorf("ender chars = %v, want z and y", got)
	}
	j.SetOptions(Options{Result: &ResultOptions{Fail: []any{"a", "b"}}})
	j.SetOptions(Options{Result: &ResultOptions{Fail: []any{"c"}}})
	if got := j.Options().Result.Fail; !reflect.DeepEqual(got, []any{"c", "b"}) {
		t.Errorf("result.fail = %v, want [c b]", got)
	}
}

// The defaults an overlay merges onto are visible on the instance, as
// they are on tn.options in TypeScript.
func TestDefaultOptionsAreTheMergeBase(t *testing.T) {
	o := Make().Options()
	if got := o.TokenSet["IGNORE"]; !reflect.DeepEqual(got, []string{"#SP", "#LN", "#CM"}) {
		t.Errorf("default IGNORE = %v", got)
	}
	if o.Comment == nil || o.Comment.Def["hash"] == nil || !boolVal(o.Comment.Def["hash"].Line, false) {
		t.Error("default comment.def.hash missing")
	}
	if o.Value == nil || o.Value.Def["null"] == nil {
		t.Error("default value.def.null missing")
	}
}

// An explicit `false` in a nested definition must survive the overlay.
//
// The overlay keeps the base wherever an overlay FIELD is zero, which is
// right where zero means "not supplied" and wrong for a tri-state typed
// as a plain `bool`. `CommentDef.Line` was one, so `Line: false` was
// dropped and a caller could not redefine a default line comment as a
// block comment. Its siblings `Lex` and `EatLine` are `*bool` for exactly
// this reason; `Line` is now too, and `Bool` is the way to write one.
//
// Note what the failure was NOT: the override was not ignored. `End`
// is non-zero, so it merged, while `Line: false` did not -- the def that
// reached the lexer was a record no caller wrote, a line comment
// carrying a block terminator.
func TestCommentDefLineFalseSurvivesTheOverlay(t *testing.T) {
	j := Make(Options{Comment: &CommentOptions{
		Def: map[string]*CommentDef{
			"hash": {Line: Bool(false), Start: "#", End: "@@"},
		},
	}})
	cfg := j.Config()
	for _, start := range cfg.CommentLine {
		if "#" == start {
			t.Errorf("# is still a line comment: CommentLine = %v", cfg.CommentLine)
		}
	}
	found := false
	for _, pair := range cfg.CommentBlock {
		if "#" == pair[0] && "@@" == pair[1] {
			found = true
		}
	}
	if !found {
		t.Errorf("# @@ is not a block comment: CommentBlock = %v", cfg.CommentBlock)
	}
	// The other defaults are untouched by the override.
	if len(cfg.CommentLine) != 1 || "//" != cfg.CommentLine[0] {
		t.Errorf("the slash def was disturbed: CommentLine = %v", cfg.CommentLine)
	}

	// A field the caller did NOT supply still falls back, which is the
	// measurement that rules out merging a supplied def as a whole
	// record: canonical TypeScript keeps `line`, `start`, `lex` and
	// `eatline` for a caller who writes only `end`.
	only := Make(Options{Comment: &CommentOptions{
		Def: map[string]*CommentDef{"hash": {End: "@@"}},
	}}).Config()
	hash := false
	for _, start := range only.CommentLine {
		if "#" == start {
			hash = true
		}
	}
	if !hash {
		t.Errorf("an unsupplied Line did not fall back to the default: CommentLine = %v", only.CommentLine)
	}

	// The SERIALIZED door, which is where a grammar carries it, and
	// which reads the same field: `"line": false` in a document has to
	// arrive as a pointer to false rather than as an absent key.
	gs, err := GrammarSpecFromJSON([]byte(
		`{"options":{"comment":{"def":{"hash":{"line":false,"start":"#","end":"@@"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := Make()
	if err := doc.Grammar(gs); err != nil {
		t.Fatal(err)
	}
	for _, start := range doc.Config().CommentLine {
		if "#" == start {
			t.Errorf("serialized: # is still a line comment: %v", doc.Config().CommentLine)
		}
	}

	// And nil is a BLOCK comment for a name with no default behind it,
	// as an absent `line` is in the canonical runtime.
	novel := Make(Options{Comment: &CommentOptions{
		Def: map[string]*CommentDef{
			"novel": {Start: "%%", End: "%%", Lex: Bool(true)},
		},
	}}).Config()
	block := false
	for _, pair := range novel.CommentBlock {
		if "%%" == pair[0] {
			block = true
		}
	}
	if !block {
		t.Errorf("a def with no Line is not a block comment: CommentBlock = %v", novel.CommentBlock)
	}
}

// Match.Value on the setting path (#237). buildConfig rebuilds MatchValues
// in full from the merged options, and SetOptions used to append the live
// entries on top, so every call added another copy of every matcher. The
// copies were not inert: sort.Slice is unstable, so a replaced or removed
// value kept firing through a stale copy. TypeScript and Rust rebuild from
// the options alone.
func TestMatchValueOverlayOnTheSettingPath(t *testing.T) {
	hexVal := func(m []string) any { return "HEX:" + m[0] }
	hex := func() map[string]*MatchValueSpec {
		return map[string]*MatchValueSpec{
			"hex": {Match: regexp.MustCompile(`^0x[0-9a-f]+`), Val: hexVal},
		}
	}
	withHex := func() *Tabnas {
		j := pmTopVal()
		j.SetOptions(Options{Match: &MatchOptions{Value: hex()}})
		return j
	}
	names := func(j *Tabnas) []string {
		out := []string{}
		for _, mv := range j.Config().MatchValues {
			out = append(out, mv.Name)
		}
		return out
	}
	parses := func(t *testing.T, j *Tabnas, src string, want any) {
		t.Helper()
		if got, err := j.Parse(src); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Parse(%q) = %#v, %v; want %#v", src, got, err, want)
		}
	}

	t.Run("repeated calls keep one copy", func(t *testing.T) {
		j := withHex()
		for i := 0; i < 3; i++ {
			j.SetOptions(Options{})
		}
		if got := names(j); !reflect.DeepEqual(got, []string{"hex"}) {
			t.Errorf("after three empty SetOptions calls: %v, want [hex]", got)
		}
		parses(t, j, "0xff", "HEX:0xff")
	})

	// The new pattern needs a digit after 0x, so 0xff falls through to the
	// number matcher's hex form. A stale copy of the old pattern caught it.
	t.Run("a replaced value stops matching", func(t *testing.T) {
		j := withHex()
		j.SetOptions(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{
			"hex": {Match: regexp.MustCompile(`^0x[0-9]+`), Val: hexVal},
		}}})
		if got := names(j); !reflect.DeepEqual(got, []string{"hex"}) {
			t.Errorf("after replacing hex: %v, want [hex]", got)
		}
		parses(t, j, "0xff", float64(255))
	})

	t.Run("a removed value stops matching", func(t *testing.T) {
		j := withHex()
		j.SetOptions(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{"hex": nil}}})
		if got := names(j); len(got) != 0 {
			t.Errorf("after removing hex: %v, want none", got)
		}
		parses(t, j, "0xff", float64(255))
	})

	t.Run("an added value joins in name order", func(t *testing.T) {
		j := withHex()
		j.SetOptions(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{
			"b64": {Match: regexp.MustCompile(`^b64:[A-Za-z0-9+/=]+`)},
		}}})
		if got := names(j); !reflect.DeepEqual(got, []string{"b64", "hex"}) {
			t.Errorf("after adding b64: %v, want [b64 hex]", got)
		}
		parses(t, j, "0xff", "HEX:0xff")
	})

	t.Run("the same grammar applied twice", func(t *testing.T) {
		j := pmTopVal()
		gs := &GrammarSpec{OptionsMap: map[string]any{
			"match": map[string]any{"value": map[string]any{
				"hex": map[string]any{"match": "@/^0x[0-9a-f]+/"},
			}},
		}}
		for i := 0; i < 2; i++ {
			if err := j.Grammar(gs); err != nil {
				t.Fatal(err)
			}
		}
		if got := names(j); !reflect.DeepEqual(got, []string{"hex"}) {
			t.Errorf("after applying the grammar twice: %v, want [hex]", got)
		}
	})

	t.Run("a derived child", func(t *testing.T) {
		j := withHex()
		for i := 0; i < 3; i++ {
			j.SetOptions(Options{})
		}
		child, err := j.Derive()
		if err != nil {
			t.Fatal(err)
		}
		if got := names(child); !reflect.DeepEqual(got, []string{"hex"}) {
			t.Errorf("derived child: %v, want [hex]", got)
		}
	})

	// A config modifier adds a value outside Match.Value, and it re-runs
	// on every rebuild, so it is kept exactly once rather than carried
	// forward from the live config. The empty Match options switch the
	// match lexer on.
	t.Run("a value a config modifier adds", func(t *testing.T) {
		j := pmTopVal()
		j.SetOptions(Options{Match: &MatchOptions{}, Property: &PropertyOptions{ConfigModify: map[string]ConfigModifier{
			"add-hex": func(cfg *LexConfig, _ *Options) {
				cfg.MatchValues = append(cfg.MatchValues, &MatchValueEntry{
					Name: "hex", Match: regexp.MustCompile(`^0x[0-9a-f]+`), Val: hexVal,
				})
			},
		}}})
		for i := 0; i < 3; i++ {
			j.SetOptions(Options{})
		}
		if got := names(j); !reflect.DeepEqual(got, []string{"hex"}) {
			t.Errorf("after three empty SetOptions calls: %v, want [hex]", got)
		}
		parses(t, j, "0xff", "HEX:0xff")
	})

	// Config() is open to modification, so a value appended to the live
	// config is one the options know nothing about and buildConfig cannot
	// rebuild. SetOptions carries it forward, once, however often it runs.
	t.Run("a value registered on the live config", func(t *testing.T) {
		j := pmTopVal()
		j.SetOptions(Options{Match: &MatchOptions{}})
		cfg := j.Config()
		cfg.MatchValues = append(cfg.MatchValues, &MatchValueEntry{
			Name: "hex", Match: regexp.MustCompile(`^0x[0-9a-f]+`), Val: hexVal,
		})
		for i := 0; i < 3; i++ {
			j.SetOptions(Options{Tag: "x"})
		}
		if got := names(j); !reflect.DeepEqual(got, []string{"hex"}) {
			t.Errorf("after three unrelated SetOptions calls: %v, want [hex]", got)
		}
		parses(t, j, "0xff", "HEX:0xff")
	})

	// What SetOptions carries is told apart from what it rebuilds by the
	// entry itself, not its name, so removing the option value leaves the
	// live one alone.
	t.Run("a live value beside an option value", func(t *testing.T) {
		j := withHex()
		cfg := j.Config()
		cfg.MatchValues = append(cfg.MatchValues, &MatchValueEntry{
			Name: "b64", Match: regexp.MustCompile(`^b64:[A-Za-z0-9+/=]+`),
		})
		for i := 0; i < 3; i++ {
			j.SetOptions(Options{})
		}
		if got := names(j); !reflect.DeepEqual(got, []string{"b64", "hex"}) {
			t.Errorf("after three empty SetOptions calls: %v, want [b64 hex]", got)
		}
		j.SetOptions(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{"hex": nil}}})
		if got := names(j); !reflect.DeepEqual(got, []string{"b64"}) {
			t.Errorf("after removing hex: %v, want [b64]", got)
		}
		parses(t, j, "0xff", float64(255))
	})

	// Modifiers run in map order, so two that each add a value landed in
	// either order, and the lexer takes the first value that matches:
	// both of these match 0xff. Every build puts them in name order.
	t.Run("values modifiers add come out in name order", func(t *testing.T) {
		add := func(name string) ConfigModifier {
			return func(cfg *LexConfig, _ *Options) {
				cfg.MatchValues = append(cfg.MatchValues, &MatchValueEntry{
					Name: name, Match: regexp.MustCompile(`^0x[0-9a-f]+`),
					Val: func(m []string) any { return name + ":" + m[0] },
				})
			}
		}
		mods := Options{Match: &MatchOptions{}, Property: &PropertyOptions{
			ConfigModify: map[string]ConfigModifier{"z": add("z"), "a": add("a")},
		}}
		for i := 0; i < 32; i++ {
			if got := names(Make(mods)); !reflect.DeepEqual(got, []string{"a", "z"}) {
				t.Fatalf("Make, build %d: %v, want [a z]", i+1, got)
			}
		}
		j := pmTopVal()
		j.SetOptions(mods)
		for i := 0; i < 32; i++ {
			j.SetOptions(Options{})
			if got := names(j); !reflect.DeepEqual(got, []string{"a", "z"}) {
				t.Fatalf("SetOptions call %d: %v, want [a z]", i+1, got)
			}
		}
		parses(t, j, "0xff", "a:0xff")
	})
}
