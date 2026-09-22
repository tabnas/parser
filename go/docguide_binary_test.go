// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

// The binary-format example from doc/guide.md, compiled and run.
//
// ts/test/doc-examples.test.js executes fenced `js` blocks from every
// doc in the repo, but not `go` ones, so a Go example in the docs is
// pinned by a test that mirrors it or it is pinned by nothing. Edit the
// two together: this file is the doc's "Parse a binary format" block
// verbatim, plus the assertion its `// =>` comment states.
//
// An external test package on purpose: the doc block is written for a
// reader importing the module, so it carries `tabnas.` prefixes, and
// compiling it that way is part of what this checks.
package tabnas_test

import (
	"reflect"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestDocGuideBinaryExample(t *testing.T) {
	// `[u8 length][that many bytes]`, repeated to end of input.
	no := false
	j := tabnas.Make(tabnas.Options{
		Rule:     &tabnas.RuleOptions{Start: "blobs", Exclude: "tabnas,imp"},
		TokenSet: map[string][]string{"IGNORE": {}},

		// Switch the text-oriented built-ins off. Unlike TypeScript, Go
		// cannot remove them from the pipeline: the order is fixed in
		// Lex.Next and each is gated by its own Config flag, so a disabled
		// one costs a boolean test rather than a call.
		Fixed:   &tabnas.FixedOptions{Lex: &no},
		Space:   &tabnas.SpaceOptions{Lex: &no},
		Line:    &tabnas.LineOptions{Lex: &no},
		Text:    &tabnas.TextOptions{Lex: &no},
		Number:  &tabnas.NumberOptions{Lex: &no},
		Comment: &tabnas.CommentOptions{Lex: &no},
		String:  &tabnas.StringOptions{Lex: &no},
		Value:   &tabnas.ValueOptions{Lex: &no},
	})

	tinLEN := j.Token("#LEN")
	tinBODY := j.Token("#BODY")

	j.SetOptions(tabnas.Options{Match: &tabnas.MatchOptions{
		// Declaration order is the tie-break inside a column; TokenOrder
		// pins it, because Go map iteration is randomised.
		TokenOrder: []string{"#LEN", "#BODY"},
		TokenFn: map[string]tabnas.LexMatcher{
			"#LEN": func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
				pnt := lex.Cursor()
				if pnt.Len <= pnt.SI {
					return nil
				}
				tkn := lex.Token("#LEN", tinLEN, int(lex.Src[pnt.SI]),
					lex.Src[pnt.SI:pnt.SI+1])
				pnt.SI++
				pnt.CI++
				return tkn
			},

			// Length from the RULE, not from the bytes.
			"#BODY": func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
				if rule == nil {
					return nil
				}
				n, ok := rule.K["len"].(int)
				if !ok || n < 0 {
					return nil
				}
				pnt := lex.Cursor()
				if pnt.Len < pnt.SI+n {
					return nil
				}
				tkn := lex.Token("#BODY", tinBODY, nil, lex.Src[pnt.SI:pnt.SI+n])
				pnt.SI += n
				pnt.CI += n
				return tkn
			},
		},
	}})

	j.Rule("blobs", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.AddBO(func(r *tabnas.Rule, _ *tabnas.Context) {
			if r.Prev != nil && r.Prev != tabnas.NoRule && r.Prev.Node != nil {
				r.Node = r.Prev.Node
				return
			}
			r.Node = []any{}
		})
		rs.AddOpen(
			&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
			&tabnas.AltSpec{
				S: [][]tabnas.Tin{{tinLEN}},
				A: func(r *tabnas.Rule, _ *tabnas.Context) {
					r.EnsureK()["len"] = r.O[0].Val
				},
				P: "body",
			},
		)
		rs.AddClose(
			&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
			&tabnas.AltSpec{S: [][]tabnas.Tin{{tinLEN}}, B: 1, R: "blobs"},
		)
		rs.AddBC(func(r *tabnas.Rule, _ *tabnas.Context) {
			if r.Child == nil || r.Child == tabnas.NoRule || r.Child.Node == nil {
				return
			}
			r.Node = append(r.Node.([]any), r.Child.Node)
		})
	})

	j.Rule("body", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.AddOpen(&tabnas.AltSpec{
			S: [][]tabnas.Tin{{tinBODY}},
			A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = r.O[0].Src },
		})
	})

	out, _ := j.Parse(string([]byte{3, 'a', 'b', 'c', 2, 'd', 'e'}))

	if !reflect.DeepEqual(out, []any{"abc", "de"}) {
		t.Errorf("got %#v", out)
	}
}
