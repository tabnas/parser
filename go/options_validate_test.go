/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// The serialized options door is typed (#143): an ill-typed leaf is a
// load fault naming the leaf, never a silent drop, and a function
// reference outside a declared code slot is one such leaf. Twin of
// ts/test/options-validate.test.js, over the same leaves.

import (
	"strings"
	"testing"
)

func TestOptionsFromMapNamesAnIllTypedLeaf(t *testing.T) {
	for _, c := range []struct {
		spec string
		want string
	}{
		{`{"options":{"line":{"chars":{"a":1}}}}`, "options.line.chars: expected string, got object"},
		{`{"options":{"tokenSet":{"VAL":"str"}}}`, "options.tokenSet.VAL: expected array, got string"},
		{`{"options":{"string":{"escapeChar":[]}}}`, "options.string.escapeChar: expected string, got array"},
		{`{"options":{"rule":{"maxmul":"three"}}}`, "options.rule.maxmul: expected number, got string"},
		{`{"options":{"comment":{"def":{"hash":{"line":"yes"}}}}}`, "options.comment.def.hash.line: expected boolean"},
		{`{"options":{"rewind":{"history":true}}}`, "options.rewind.history"},
		{`{"options":{"tokenSet":{"IGNORE":["#SP",7]}}}`, "options.tokenSet.IGNORE[1]: expected string"},
	} {
		gs, err := GrammarSpecFromJSON([]byte(c.spec))
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		err = Make().Grammar(gs)
		if err == nil {
			t.Errorf("%s: loaded; the ill-typed leaf was dropped in silence", c.spec)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not name the leaf as %q", c.spec, err, c.want)
		}
		if strings.Contains(err.Error(), "internal") {
			t.Errorf("%s: a caller's mistake is reported as internal: %v", c.spec, err)
		}
	}
}

func TestOptionsFromMapRefusesARefInADataSlot(t *testing.T) {
	for _, spec := range []string{
		`{"options":{"line":{"chars":"@node$"}}}`,
		`{"options":{"rule":{"start":"@value$"}}}`,
		`{"options":{"tokenSet":{"IGNORE":["@node$"]}}}`,
	} {
		gs, err := GrammarSpecFromJSON([]byte(spec))
		if err != nil {
			t.Fatal(err)
		}
		err = Make().Grammar(gs)
		if err == nil || !strings.Contains(err.Error(), "function reference") {
			t.Errorf("%s: want a code-slot error, got %v", spec, err)
		}
	}

	// A code slot given a function of the wrong kind is reported as such,
	// where it used to be ignored: parser.start is the parse entry point.
	gs, err := GrammarSpecFromJSON([]byte(`{"options":{"parser":{"start":"@node$"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	err = Make().Grammar(gs)
	if err == nil || !strings.Contains(err.Error(), "wrong type") {
		t.Errorf("parser.start with an action ref: want a wrong-type error, got %v", err)
	}

	// And a declared code slot still takes a ref of the right kind.
	gs, err = GrammarSpecFromJSON([]byte(`{"options":{"rule":{"start":"top"},"parse":{"prepare":{"count":"@prep"}}},
		"rule":{"top":{"open":[{"s":"#NR"}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	gs.Ref = map[FuncRef]any{"@prep": func(ctx *Context) { ran++ }}
	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("1"); err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Errorf("parse.prepare ref did not run: %d", ran)
	}
}

func TestOptionsFromMapAcceptsTheDocumentedIdioms(t *testing.T) {
	gs, err := GrammarSpecFromJSON([]byte(`{"options":{
		"rewind":{"history":false},
		"tokenSet":{"IGNORE":["#SP",null,null]},
		"comment":{"def":{"hash":null,"slash":false}},
		"value":{"def":{"yes":{"val":1},"no":false}},
		"fixed":{"token":{"#CA":null,"#X":"x"}},
		"errmsg":{"suffix":"because"},
		"ender":":",
		"lex":{"match":{"number":false}},
		"lex":{"emptyResult":[]},
		"match":{"token":{"#A":"@/^a/"}},
		"string":{"escape":{"v":null}},
		"plugin":{"anything":{"goes":true}},
		"csv":{"field":{"separator":"|"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Make().Grammar(gs); err != nil {
		t.Fatalf("documented idioms refused: %v", err)
	}
}

// #143's validator was stricter than the readers it described, and
// 0.11.0 shipped that in every runtime: a string `ender` is read by
// OptionsFromMap (and passed by @tabnas/yaml), and the door refused it.
func TestOptionsFromMapAcceptsAStringEnder(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{"ender": ";|"})
	if err != nil {
		t.Fatalf("string ender refused: %v", err)
	}
	// Split per character, exactly as the array form of the same
	// characters: `';|'` is two enders, `[';|']` is one (#202). Reading
	// the string into a single entry made the two forms indistinguishable
	// downstream, which is why this reader, not buildConfig, splits it.
	if len(opts.Ender) != 2 || opts.Ender[0] != ";" || opts.Ender[1] != "|" {
		t.Fatalf("string ender read as %v, want [; |]", opts.Ender)
	}
	// Both characters end text, which is what the string form means.
	j := Make(opts)
	chars := j.Config().EnderChars
	for _, r := range []rune{';', '|'} {
		if !chars[r] {
			t.Errorf("ender char %q missing from %v", r, chars)
		}
	}
	if seqs := j.Config().EnderSeqs; len(seqs) != 0 {
		t.Errorf("string ender contributed sequences %v, want none", seqs)
	}
	// The discriminator: the same two characters as ONE array entry are
	// one two-character ender, and neither character ends text alone.
	arr, err := OptionsFromMap(map[string]any{"ender": []any{";|"}})
	if err != nil {
		t.Fatalf("array ender refused: %v", err)
	}
	cfg := Make(arr).Config()
	if len(cfg.EnderChars) != 0 {
		t.Errorf("array entry %q split into characters %v", ";|", cfg.EnderChars)
	}
	if len(cfg.EnderSeqs) != 1 || cfg.EnderSeqs[0] != ";|" {
		t.Errorf("array entry sequences = %v, want [;|]", cfg.EnderSeqs)
	}
	// And through the serialized door, as a grammar carries it.
	gs, err := GrammarSpecFromJSON([]byte(`{"options":{"ender":":"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Make().Grammar(gs); err != nil {
		t.Fatalf("string ender refused through the grammar door: %v", err)
	}
}

// The regression test for the class: every declared widening is accepted,
// and the table is the only place a widening is declared.
func TestOptionWideningsAreAccepted(t *testing.T) {
	for _, c := range []struct {
		path string
		val  any
	}{
		{"options.ender", ";"},
		{"options.rewind.history", false},
		// The general definition-map rule, stated in optionWidenings'
		// comment rather than as an entry.
		{"options.lex.match.number", false},
		{"options.comment.def.hash", false},
	} {
		var errs []string
		validateLeafAt(c.path, c.val, &errs)
		if len(errs) > 0 {
			t.Errorf("%s = %v: refused: %v", c.path, c.val, errs)
		}
	}
	// A widening is not a blanket pass: the same leaves still refuse a
	// shape no reader takes.
	for _, c := range []struct {
		path string
		val  any
	}{
		{"options.ender", 7.0},
		{"options.rewind.history", true},
		{"options.lex.match.number", "off"},
	} {
		var errs []string
		validateLeafAt(c.path, c.val, &errs)
		if len(errs) == 0 {
			t.Errorf("%s = %v: accepted, but no reader takes it", c.path, c.val)
		}
	}
}

// validateLeafAt drives validateOptionsMap through the nested map that
// `path` names, so a case reads as the leaf it is about.
func validateLeafAt(path string, val any, errs *[]string) {
	parts := strings.Split(strings.TrimPrefix(path, "options."), ".")
	var node any = val
	for i := len(parts) - 1; i >= 0; i-- {
		node = map[string]any{parts[i]: node}
	}
	if err := validateOptionsMap(node.(map[string]any)); err != nil {
		*errs = append(*errs, err.Error())
	}
}
