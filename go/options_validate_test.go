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
