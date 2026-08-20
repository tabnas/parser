package tabnas

// relex_test.go — negotiated lexing (Options.Lex.Relex), the Go port of
// the TypeScript `lex: { relex: true }` option.
//
// The shapes here are the ones scannerless notations actually hit, taken
// from the llama.cpp GBNF corpus: a repetition class contested by a
// following literal (arithmetic.gbnf's `ws ::= [ \t\n]*` before a literal
// "\n"), and a keyword contested by an identifier class (c.gbnf).
//
// What is asserted throughout is that renegotiation cannot WIDEN the
// accepted language: every alternate still requires exactly its own
// tokens, so a wrong re-cut makes the parse fail, never succeed.

import (
	"regexp"
	"strings"
	"testing"
)

func relexTrue() *bool  { b := true; return &b }
func relexFalse() *bool { b := false; return &b }

// contestedGrammar builds the arithmetic.gbnf shape: a whitespace class
// `[ \t\n]+` that claims a newline, and a fixed "\n" literal that an
// alternate demands. The class is eager, which is what a compiled
// scannerless class is — it must fire wherever it matches rather than only
// where the current rule position already expects it.
//
// The contest is real because the class wins the FIRST cut: the match
// matcher runs before the fixed matcher, and at a position the current
// rule does not already expect the class at, the eager pass still fires
// it. So "\n" arrives as #WS, and only a renegotiation can give an
// alternate naming #NL the token it wants.
//
// (A newline TRAILING a text term would not be contested: the text matcher
// sub-matches the fixed token at the ender and QUEUES it, so it is already
// #NL. That queue is the other half of what makes a first cut sticky — it
// just happens to pick the wanted identity there.)
func contestedGrammar(relex *bool) *Tabnas {
	nl := "\n"
	j := Make(Options{
		Lex: &LexOptions{Relex: relex},
		Match: &MatchOptions{
			Token:      map[string]*regexp.Regexp{"#WS": regexp.MustCompile(`^[ \t\n]+`)},
			TokenEager: map[string]bool{"#WS": true},
		},
		Fixed: &FixedOptions{Token: map[string]*string{"#NL": &nl}},
	})

	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#NL")}},
			A: func(r *Rule, ctx *Context) { r.Node = "NL" },
		})
	})

	return j
}

// The probe @tabnas/gbnf performs before compiling: set the option, then
// read it back off the resolved config. An option silently dropped is
// worse than an absent one — the front-end would compile a grammar the
// engine then mis-parses.
func TestRelexOptionSurvivesIntoConfig(t *testing.T) {
	if on := Make(Options{Lex: &LexOptions{Relex: relexTrue()}}); !on.Config().Relex {
		t.Error("Lex.Relex=true did not survive into the resolved config")
	}
	if off := Make(Options{Lex: &LexOptions{Relex: relexFalse()}}); off.Config().Relex {
		t.Error("Lex.Relex=false was not honoured")
	}
	if def := Make(Options{}); def.Config().Relex {
		t.Error("Relex must default to OFF")
	}

	// The map form too: a serialized grammar arrives as map[string]any and
	// goes through MapToOptions, so the key has to be decoded there as
	// well as being a struct field.
	viaMap := Make(MapToOptions(map[string]any{
		"lex": map[string]any{"relex": true},
	}))
	if !viaMap.Config().Relex {
		t.Error("lex.relex from the map form did not reach the config")
	}
}

// The canonical case: a class and a literal contest the same character.
func TestRelexRepetitionClassContestedByLiteral(t *testing.T) {
	out, err := contestedGrammar(relexTrue()).Parse("\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out != "NL" {
		t.Errorf("got %#v, want %q", out, "NL")
	}
}

// The same grammar and the same input WITHOUT the option: the close
// alternate sees #WS where it wants #NL and fails. This is the behaviour
// every existing Go grammar has today, and it must not change.
func TestRelexOffLeavesTheContestUnresolved(t *testing.T) {
	if _, err := contestedGrammar(relexFalse()).Parse("\n"); err == nil {
		t.Error("expected a parse error with Relex off — if this now " +
			"passes, the option is not inert when unset")
	}
}

// A keyword contested by an identifier class (the c.gbnf shape): `#ID`
// matches `if` perfectly well, and does so first, but an alternate naming
// the keyword can still get it.
func TestRelexKeywordContestedByIdentifierClass(t *testing.T) {
	kw := "if"
	j := Make(Options{
		Lex: &LexOptions{Relex: relexTrue()},
		Match: &MatchOptions{
			Token:      map[string]*regexp.Regexp{"#ID": regexp.MustCompile(`^[a-z]+`)},
			TokenEager: map[string]bool{"#ID": true},
		},
		Fixed: &FixedOptions{Token: map[string]*string{"#IF": &kw}},
	})
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#IF")}},
			A: func(r *Rule, ctx *Context) { r.Node = "keyword" },
		})
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#ID")}},
			A: func(r *Rule, ctx *Context) { r.Node = "ident:" + r.O0.Src },
		})
	})

	// `if` is claimed by the class first; the first alternate renegotiates
	// it into the keyword.
	if out, err := j.Parse("if"); err != nil || out != "keyword" {
		t.Errorf("Parse(%q) = %#v, %v; want %q", "if", out, err, "keyword")
	}
	// A word that is NOT the keyword still reaches the class alternate —
	// the failed renegotiation must leave the buffer as it found it.
	if out, err := j.Parse("ok"); err != nil || out != "ident:ok" {
		t.Errorf("Parse(%q) = %#v, %v; want %q", "ok", out, err, "ident:ok")
	}
}

// Renegotiation cannot over-accept. The alternate names #NL and nothing
// else, so input whose span cannot cut to #NL fails — the recut is not a
// licence to accept whatever the span happens to hold.
func TestRelexCannotAcceptAnUnwantedCut(t *testing.T) {
	// " " lexes as #WS and cannot re-cut to #NL: no fixed token matches a
	// space. The alternate must fail rather than take the #WS it was given.
	if _, err := contestedGrammar(relexTrue()).Parse(" "); err == nil {
		t.Error("a span that cannot cut to the wanted tin must fail the parse")
	}
}

// A bad token's error survives the deferral. With Relex on, Lex.Next hands
// the #BD to the parser instead of raising immediately, so an alternate
// gets the chance to re-cut it; when none can, the SAME diagnostic must
// still surface rather than a generic one.
//
// The wanted tin is #NR, and that is the whole point of the test rather than
// an incidental choice. This case used to want #TX and `"unterminated` — but
// a quote is not a text ender, so the span re-cuts to #TX perfectly well and
// the parse SUCCEEDS. It only raised here because Go stopped a text run at a
// quote char, which is the divergence removed in this commit; the test was
// pinning that divergence while claiming to test diagnostic survival. TS
// measured on the same three tins:
//
//	want #TX -> OK "\"unterminated"     (the re-cut succeeds)
//	want #NR -> ERROR unterminated_string
//	want #ST -> ERROR unterminated_string
//
// #NR keeps the property under test — no alternate CAN take this span — and
// TestRelexBadTokenRecutsToText below covers the #TX half that now succeeds.
func TestRelexBadTokenStillRaisesItsOwnError(t *testing.T) {
	j := Make(Options{Lex: &LexOptions{Relex: relexTrue()}})
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#NR")}},
			A: func(r *Rule, ctx *Context) { r.Node = r.O0.Src },
		})
	})

	_, err := j.Parse(`"unterminated`)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "unterminated_string") {
		t.Errorf("deferred bad token lost its own diagnostic: %v", err)
	}
}

// The other half: an alternate that CAN take the span does, and the parse
// succeeds. `"unterminated` re-cuts to #TX because a quote does not end a
// text run — matching TS, which returns the same string here.
func TestRelexBadTokenRecutsToText(t *testing.T) {
	j := Make(Options{Lex: &LexOptions{Relex: relexTrue()}})
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#TX")}},
			A: func(r *Rule, ctx *Context) { r.Node = r.O0.Src },
		})
	})

	v, err := j.Parse(`"unterminated`)
	if nil != err {
		t.Fatalf("expected the span to re-cut to #TX, got %v", err)
	}
	if s, _ := v.(string); s != `"unterminated` {
		t.Errorf("re-cut text: got %q, want %q", s, `"unterminated`)
	}
}

// A wildcard position must not swallow a bad token. `#AA` makes tinMatch
// true for every tin, so without an explicit test the deferral would turn
// malformed input into a successful parse — the one way this feature
// could widen the accepted language.
func TestRelexWildcardDoesNotAcceptBadToken(t *testing.T) {
	j := Make(Options{Lex: &LexOptions{Relex: relexTrue()}})
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#AA")}},
			A: func(r *Rule, ctx *Context) { r.Node = "any" },
		})
	})

	if out, err := j.Parse(`"unterminated`); err == nil {
		t.Errorf("a wildcard position accepted a bad token: %#v", out)
	}
}

// A renegotiation that an alternate then abandons must leave nothing
// behind: the cut is shared with every later alternate and every later
// rule, so an inherited one could turn a working parse into a failing one.
func TestRelexUndoRestoresTheBufferForLaterAlternates(t *testing.T) {
	nl := "\n"
	j := Make(Options{
		Lex: &LexOptions{Relex: relexTrue()},
		Match: &MatchOptions{
			Token:      map[string]*regexp.Regexp{"#WS": regexp.MustCompile(`^[ \t\n]+`)},
			TokenEager: map[string]bool{"#WS": true},
		},
		Fixed: &FixedOptions{Token: map[string]*string{"#NL": &nl}},
	})
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		// First alternate renegotiates position 0 into #NL and then fails
		// at position 1, which wants a text token the input does not have.
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#NL")}, {j.Token("#TX")}},
			A: func(r *Rule, ctx *Context) { r.Node = "nl-then-text" },
		})
		// Second alternate wants the ORIGINAL cut. It only sees it if the
		// first alternate's recut was undone.
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{j.Token("#WS")}},
			A: func(r *Rule, ctx *Context) { r.Node = "ws:" + r.O0.Src },
		})
	})

	out, err := j.Parse("\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out != "ws:\n" {
		t.Errorf("got %#v, want %q — the abandoned recut was inherited "+
			"by the next alternate", out, "ws:\n")
	}
}

// The option is inert at lex time. Turning it on changes what an
// ALTERNATE may ask for; it must not change what the lexer produces when
// nobody asks. Every gate added for negotiated lexing is guarded by a want
// that is set only inside Relex, and this pins that: the same source lexes
// to the same tokens either way.
//
// (The other half of "inert when off" is the rest of this repo's suite,
// which sets Relex nowhere and is unchanged.)
func TestRelexDoesNotChangeOrdinaryLexing(t *testing.T) {
	stream := func(relex *bool, src string) []string {
		nl := "\n"
		j := Make(Options{
			Lex: &LexOptions{Relex: relex},
			Match: &MatchOptions{
				Token:      map[string]*regexp.Regexp{"#WS": regexp.MustCompile(`^[ \t\n]+`)},
				TokenEager: map[string]bool{"#WS": true},
			},
			Fixed: &FixedOptions{Token: map[string]*string{"#NL": &nl}},
		})
		lex := NewLex(src, j.Config())
		out := []string{}
		for i := 0; i < 32; i++ {
			tkn := lex.Next()
			out = append(out, tkn.Name+":"+tkn.Src)
			if tkn.Tin == TinZZ {
				break
			}
		}
		return out
	}

	for _, src := range []string{
		"", " ", "\n", "a", "abc 123", "{a:1}", "[1, 2]",
		"\"quoted\"", "a\nb", "0x1F", "true", "# comment\nx",
	} {
		off := stream(relexFalse(), src)
		on := stream(relexTrue(), src)
		if len(off) != len(on) {
			t.Errorf("%q: %d tokens with Relex off, %d with it on\n off %v\n on  %v",
				src, len(off), len(on), off, on)
			continue
		}
		for i := range off {
			if off[i] != on[i] {
				t.Errorf("%q token %d: off %q, on %q", src, i, off[i], on[i])
			}
		}
	}
}
