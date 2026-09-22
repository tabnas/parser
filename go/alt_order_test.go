/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// The order in which a matched alternate's hooks run is contract in
// every runtime (#154): the routing forms resolve, then the modifier,
// then the error hook, then the action. TypeScript ran the error hook
// before the modifier, and no shipped grammar declared both, so nothing
// observed the split until a third runtime had to transcribe one side.
// ts/test/cover-engine.test.js ('fnref-strings-for-h-e-p-r-b') and
// rs/tests/callback_test.rs pin the same grammar.

import (
	"reflect"
	"testing"
)

func TestAltModifierRunsBeforeTheErrorHook(t *testing.T) {
	a, b := "a", "b"
	j := Make(Options{
		Rule:  &RuleOptions{Start: "top"},
		Fixed: &FixedOptions{Token: map[string]*string{"#A": &a, "#B": &b}},
	})
	ta, tb := j.Token("#A"), j.Token("#B")
	var used []string
	j.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(r *Rule, ctx *Context) { used = append(used, "child") }})
	})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{ta}},
			H: func(alt *AltSpec, r *Rule, ctx *Context) *AltSpec {
				used = append(used, "h")
				return alt
			},
			E: func(r *Rule, ctx *Context) *Token {
				used = append(used, "e")
				return nil
			},
			PF: func(r *Rule, ctx *Context) string { used = append(used, "p"); return "child" },
			BF: func(r *Rule, ctx *Context) int { used = append(used, "b"); return 0 },
		})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	if _, err := j.Parse("ab"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"p", "b", "h", "e", "child"}; !reflect.DeepEqual(used, want) {
		t.Fatalf("hook order %v, want %v", used, want)
	}
}
