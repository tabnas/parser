/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// The serialized options door is typed (#143): an ill-typed leaf is a
// load fault naming the leaf, never a silent drop, and a function
// reference outside a declared code slot is one such leaf. Twin of
// ts/test/options-validate.test.js, over the same leaves.

import (
	"regexp"
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
		// A null WHOLE VALUE. It used to load here and leave the set
		// untouched, while the same document was a fault in TypeScript
		// and a deletion in Rust -- three answers for one JSON grammar.
		// A null MEMBER is unaffected and still clears its position;
		// the `["#SP",null,null]` case elsewhere in this file covers it.
		{`{"options":{"tokenSet":{"KEY":null}}}`, "options.tokenSet.KEY: expected array, got null"},
		{`{"options":{"tokenSet":{"CUSTOM":null}}}`, "options.tokenSet.CUSTOM: expected array, got null"},
		// A null CONTAINER, as distinct from a null name. It used to load
		// here as a no-op while TypeScript's constructor replaced the map
		// and built NO sets at all -- one document, two answers on one
		// runtime and a third here.
		{`{"options":{"tokenSet":null}}`, "options.tokenSet: expected object, got null"},
		// The three names TypeScript's deep merge refuses, because merging
		// one reaches the prototype chain. Go has no such hazard, but the
		// code is the contract: a grammar naming a set `constructor` must
		// not load here and fault there.
		{`{"options":{"tokenSet":{"constructor":["#TX"]}}}`,
			"options.tokenSet.constructor: \"constructor\" is a reserved name"},
		{`{"options":{"tokenSet":{"__proto__":["#TX"]}}}`,
			"options.tokenSet.__proto__: \"__proto__\" is a reserved name"},
		{`{"options":{"tokenSet":{"prototype":["#TX"]}}}`,
			"options.tokenSet.prototype: \"prototype\" is a reserved name"},
		// Every OTHER inherited name is an ordinary set name, and the rule
		// is the three deep() skips rather than "anything on the prototype".
		{`{"options":{"comment":{"def":{"constructor":{"line":true}}}}}`,
			"options.comment.def.constructor: \"constructor\" is a reserved name"},
		// `options.plugin` is the one caller-keyed map this port's
		// Options does not hold -- TypeScript keeps a plugin's namespace
		// in its options and this port keeps it on the engine -- so the
		// reflective walk cannot reach it and it used to load here while
		// faulting there.
		{`{"options":{"plugin":{"prototype":{"a":1}}}}`,
			"options.plugin.prototype: \"prototype\" is a reserved name"},
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
		"tokenSet":{"toString":["#TX"],"valueOf":["#NR"]},
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

// A null entry in a map of definitions DELETES that definition, in every
// runtime, and so does a false comment or value definition: TypeScript's
// makeCommentMatcher and configure skip an entry that is `null == om ||
// false === om`, so the default is gone from the config. The serialized
// door here read a definition only when it was an object and dropped
// anything else, so the default survived and a grammar document could
// not turn one off in Go (#240; tabnas/ini#77 and the abnf port carried
// typed-nil workarounds). The typed door was never affected: Deep already
// treats a nil *Def entry as the delete marker, so the reader's job is to
// produce that entry rather than skip it.
//
// A match value takes null only. TypeScript's validator accepts a regexp,
// a function, an object or null there and refuses false, so a false match
// value is a load fault here too, rather than a deletion in one runtime
// and a refusal in the other.
func TestOptionsFromMapNullDefinitionDeletes(t *testing.T) {
	gs, err := GrammarSpecFromJSON([]byte(`{"options":{
		"comment":{"def":{"slash":null,"multi":false}},
		"value":{"def":{"null":null}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatal(err)
	}
	cfg := j.Config()
	if len(cfg.CommentLine) != 1 || cfg.CommentLine[0] != "#" {
		t.Errorf("comment.def.slash: null left the line comments at %v, want [#]", cfg.CommentLine)
	}
	if len(cfg.CommentBlock) != 0 {
		t.Errorf("comment.def.multi: false left the block comments at %v, want none", cfg.CommentBlock)
	}
	if _, has := cfg.ValueDef["null"]; has {
		t.Errorf("value.def.null: null left the keyword in %v", cfg.ValueDef)
	}
	if _, has := cfg.ValueDef["true"]; !has {
		t.Errorf("value.def.true should survive an unrelated deletion: %v", cfg.ValueDef)
	}

	// The reader's own output is the typed delete marker: the key is
	// present and its definition nil, which is what Deep removes.
	opts, err := OptionsFromMap(map[string]any{
		"comment": map[string]any{"def": map[string]any{"slash": nil, "multi": false}},
		"value":   map[string]any{"def": map[string]any{"null": nil, "no": false}},
		"match":   map[string]any{"value": map[string]any{"x": nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"slash": true, "multi": true} {
		if cd, ok := opts.Comment.Def[name]; !ok || cd != nil || !want {
			t.Errorf("comment.def.%s: want a present nil entry, got present=%v value=%v", name, ok, cd)
		}
	}
	for _, name := range []string{"null", "no"} {
		if vd, ok := opts.Value.Def[name]; !ok || vd != nil {
			t.Errorf("value.def.%s: want a present nil entry, got present=%v value=%v", name, ok, vd)
		}
	}
	if mv, ok := opts.Match.Value["x"]; !ok || mv != nil {
		t.Errorf("match.value.x: want a present nil entry, got present=%v value=%v", ok, mv)
	}

	// match.value has no defaults, so the deletion is of an entry set
	// earlier: a typed matcher value, then nulled through the serialized
	// door, is gone from the config.
	k := Make(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{
		"at": {Match: regexp.MustCompile(`^@\w+`)},
	}}})
	if len(k.Config().MatchValues) != 1 {
		t.Fatalf("setup: want one match value, got %d", len(k.Config().MatchValues))
	}
	if err := k.Grammar(&GrammarSpec{OptionsMap: map[string]any{
		"match": map[string]any{"value": map[string]any{"at": nil}},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := len(k.Config().MatchValues); n != 0 {
		t.Errorf("match.value.at: null left %d match values", n)
	}

	// false is not a second spelling of that deletion. The door refuses
	// it with the shapes the slot takes, as TypeScript's validator does,
	// and the matcher it would have deleted survives: one grammar must
	// not delete a matcher in one runtime and fail to load in the other.
	k = Make(Options{Match: &MatchOptions{Value: map[string]*MatchValueSpec{
		"at": {Match: regexp.MustCompile(`^@\w+`)},
	}}})
	want := "options.match.value.at: expected a serialized regex, " +
		"a matcher function or an object, got boolean"
	err = k.Grammar(&GrammarSpec{OptionsMap: map[string]any{
		"match": map[string]any{"value": map[string]any{"at": false}},
	}})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("match.value.at: false: want an error naming %q, got %v", want, err)
	}
	if n := len(k.Config().MatchValues); n != 1 {
		t.Errorf("match.value.at: false deleted the matcher: %d match values left", n)
	}
	// And the reader produces no entry for it, so the value a caller
	// ignores the error of carries no delete marker either.
	refused, err := OptionsFromMap(map[string]any{
		"match": map[string]any{"value": map[string]any{"at": false}},
	})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("OptionsFromMap match.value.at: false: want an error naming %q, got %v", want, err)
	}
	if refused.Match != nil && refused.Match.Value != nil {
		if mv, has := refused.Match.Value["at"]; has {
			t.Errorf("match.value.at: false was read as the entry %v", mv)
		}
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
		// The general definition-map rule, falseDeletes rather than an
		// optionWidenings entry.
		{"options.lex.match.number", false},
		{"options.comment.def.hash", false},
		{"options.value.def.no", false},
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
		// false deletes a definition in the three maps falseDeletes
		// names and nowhere else. A match value, a match token and a
		// fixed token take their own shapes, TypeScript refuses false in
		// each, and the readers here used to drop it in silence.
		{"options.match.value.x", false},
		{"options.match.token.#X", false},
		{"options.fixed.token.#X", false},
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

// MapToOptions is the legacy door: it calls OptionsFromMap and DISCARDS
// the error, and is documented to skip an entry it cannot carry. A
// reserved name was reported by the validator and converted anyway, so
// the one door that cannot report a refusal was also the one that
// installed it.
func TestMapToOptionsDropsARefusedEntry(t *testing.T) {
	opts := MapToOptions(map[string]any{
		"tokenSet": map[string]any{"constructor": []any{"#TX"}, "MINE": []any{"#TX"}},
		"comment":  map[string]any{"def": map[string]any{"prototype": map[string]any{"line": true}}},
		"fixed":    map[string]any{"token": map[string]any{"__proto__": "x", "#Q": "q"}},
	})
	if _, ok := opts.TokenSet["constructor"]; ok {
		t.Error("tokenSet.constructor was refused and carried")
	}
	if opts.Comment != nil && opts.Comment.Def != nil {
		if _, ok := opts.Comment.Def["prototype"]; ok {
			t.Error("comment.def.prototype was refused and carried")
		}
	}
	if opts.Fixed != nil && opts.Fixed.Token != nil {
		if _, ok := opts.Fixed.Token["__proto__"]; ok {
			t.Error("fixed.token.__proto__ was refused and carried")
		}
	}
	// The legitimate neighbours in the same maps are untouched: the prune
	// removes the three names, not the map.
	if len(opts.TokenSet["MINE"]) != 1 {
		t.Errorf("tokenSet.MINE = %v, want one member", opts.TokenSet["MINE"])
	}
	if opts.Fixed == nil || opts.Fixed.Token["#Q"] == nil {
		t.Error("fixed.token.#Q was dropped with the refused entry")
	}
	// AND the caller's own data is untouched. The converter stores an
	// opaque `any` -- a value.def entry's Val -- by reference, and a Go
	// map is a reference type, so a prune that walked into one deleted
	// the key from the CALLER'S map. A reserved name in opaque data is
	// DATA, not an option-map entry.
	inner := map[string]any{"constructor": 1, "keep": 2}
	MapToOptions(map[string]any{
		"value": map[string]any{"def": map[string]any{
			"mine": map[string]any{"val": inner},
		}},
	})
	if _, ok := inner["constructor"]; !ok {
		t.Error("MapToOptions deleted a key from the caller's own opaque map")
	}
	if len(inner) != 2 {
		t.Errorf("the caller's opaque map was modified: %v", inner)
	}

	// And the checked door still reports what the legacy one swallows.
	if _, err := OptionsFromMap(map[string]any{
		"tokenSet": map[string]any{"constructor": []any{"#TX"}},
	}); err == nil {
		t.Error("OptionsFromMap accepted a reserved name")
	}
}
