// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Lexer-feature coverage: number formats (hex / octal / binary /
// separators / exclusions), comments (line, block and suffix
// termination), plus a few small instance-API surfaces. Exercised over
// the bare engine (which lexes these by default) and the strict-JSON
// fixture.

import "testing"

func TestLexNumberFormats(t *testing.T) {
	j := pmTopVal()
	cases := map[string]float64{
		"0xff":  255,
		"0o17":  15,
		"0b101": 5,
		"1_000": 1000,
		"3.14":  3.14,
		"-42":   -42,
		"1e3":   1000,
	}
	for src, want := range cases {
		out, err := j.Parse(src)
		if err != nil {
			t.Errorf("%q: error %v", src, err)
			continue
		}
		if out != want {
			t.Errorf("%q: got %v want %v", src, out, want)
		}
	}
}

func TestLexNumberHexDisabled(t *testing.T) {
	// With hex disabled, 0xff is no longer a number. Over the bare
	// value grammar it lexes as text instead.
	j := Make(Options{
		Rule:   &RuleOptions{Start: "top"},
		Number: &NumberOptions{Hex: boolPtr(false)},
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{TinSetVAL}, A: func(r *Rule, ctx *Context) {
			r.Node = r.O0.ResolveVal(r, ctx)
		}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	out, err := j.Parse("0xff")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out == float64(255) {
		t.Errorf("hex should be disabled, but parsed as 255")
	}
}

func TestLexNumberExclude(t *testing.T) {
	// Leading double-zero is excluded from number matching (strict JSON
	// rejects it as a number).
	if _, err := makeJSON().Parse("00"); err == nil {
		t.Error("expected error for leading-zero number in strict JSON")
	}
}

func TestLexLineComment(t *testing.T) {
	j := pmTopVal()
	out, err := j.Parse("// leading\n42")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != float64(42) {
		t.Errorf("line comment not skipped: got %v", out)
	}
}

func TestLexBlockComment(t *testing.T) {
	j := pmTopVal()
	out, err := j.Parse("/* block */ 42")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if out != float64(42) {
		t.Errorf("block comment not skipped: got %v", out)
	}
}

func TestLexCommentSuffixString(t *testing.T) {
	// Re-enable comments on the strict fixture with a suffix terminator;
	// the suffix is consumed, so the value after it parses.
	yes := true
	j := makeJSON(Options{Comment: &CommentOptions{
		Lex: &yes,
		Def: map[string]*CommentDef{
			"hash": {Line: true, Start: "#", Lex: &yes, Suffix: "@@"},
		},
	}})
	out, err := j.Parse(`[1,# noise @@2]`)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	want := []any{float64(1), float64(2)}
	if !valuesEqual(out, want) {
		t.Errorf("got %s want %s", formatValue(out), formatValue(want))
	}
}

func TestLexCommentSuffixMultiple(t *testing.T) {
	yes := true
	j := makeJSON(Options{Comment: &CommentOptions{
		Lex: &yes,
		Def: map[string]*CommentDef{
			"hash": {Line: true, Start: "#", Lex: &yes, Suffix: []string{"END", "STOP"}},
		},
	}})
	out, err := j.Parse(`[1,# noise STOP2]`)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !valuesEqual(out, []any{float64(1), float64(2)}) {
		t.Errorf("suffix list did not terminate: %s", formatValue(out))
	}
}

// --- Small instance-API surfaces ---

func TestLexEmptyInstance(t *testing.T) {
	e := Empty()
	if e == nil {
		t.Fatal("Empty() returned nil")
	}
	// An empty instance has no grammar: every rule's alternates are cleared.
	for name, rs := range e.RSM() {
		if len(rs.OpenAlts()) != 0 || len(rs.CloseAlts()) != 0 {
			t.Errorf("rule %q should be cleared in an empty instance", name)
		}
	}
}

func TestLexInstanceID(t *testing.T) {
	j := Make()
	if j.Id() == "" {
		t.Error("instance Id should be non-empty")
	}
}

func TestLexErrorMessage(t *testing.T) {
	_, err := makeJSON().Parse("a:1") // relaxed syntax — rejected
	if err == nil {
		t.Fatal("expected error")
	}
	te, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T", err)
	}
	if te.Error() == "" {
		t.Error("TabnasError.Error() should be non-empty")
	}
	if te.Code == "" {
		t.Error("TabnasError.Code should be set")
	}
}
