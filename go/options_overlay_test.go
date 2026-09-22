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
	if o.Comment == nil || o.Comment.Def["hash"] == nil || !o.Comment.Def["hash"].Line {
		t.Error("default comment.def.hash missing")
	}
	if o.Value == nil || o.Value.Def["null"] == nil {
		t.Error("default value.def.null missing")
	}
}
