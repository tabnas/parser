/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// The first-token index (altindex.go) must be invisible: a rule with
// many alternates tries only those that can take the first token, and
// everything an author can observe stays as it was. TypeScript pins the
// same cases in ts/test/alt-index.test.js and Rust in
// rs/tests/alt_index_test.rs.

import (
	"fmt"
	"reflect"
	"testing"
)

func altIndexInstance(extra map[string]string) *Tabnas {
	a, b, c := "a", "b", "c"
	tokens := map[string]*string{"#A": &a, "#B": &b, "#C": &c}
	for name, src := range extra {
		s := src
		tokens[name] = &s
	}
	return Make(Options{
		Rule:  &RuleOptions{Start: "top"},
		Fixed: &FixedOptions{Token: tokens},
	})
}

func TestAltIndexKeepsFirstMatchWinsAmongSharedHeads(t *testing.T) {
	j := altIndexInstance(nil)
	ta, tb := j.Token("#A"), j.Token("#B")
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{ta}}, C: func(*Rule, *Context) bool { return false },
				A: func(r *Rule, _ *Context) { r.Node = "first" }},
			&AltSpec{S: [][]Tin{{tb}}, A: func(r *Rule, _ *Context) { r.Node = "b" }},
			&AltSpec{S: [][]Tin{{ta}}, A: func(r *Rule, _ *Context) { r.Node = "second" }},
			&AltSpec{S: [][]Tin{{ta}}, A: func(r *Rule, _ *Context) { r.Node = "third" }},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	for src, want := range map[string]string{"a": "second", "b": "b"} {
		out, err := j.Parse(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if out != want {
			t.Fatalf("%q: got %v, want %q", src, out, want)
		}
	}
	if _, err := j.Parse("c"); err == nil {
		t.Fatal("c should be refused: no alternate takes it")
	}
}

func TestAltIndexKeepsWildcardAndEmptyAlternatesInPlace(t *testing.T) {
	j := altIndexInstance(nil)
	ta, tb := j.Token("#A"), j.Token("#B")
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{ta}}, A: func(r *Rule, _ *Context) { r.Node = "a" }},
			&AltSpec{S: [][]Tin{{TinAA}}, A: func(r *Rule, _ *Context) { r.Node = "any" }},
			&AltSpec{S: [][]Tin{{tb}}, A: func(r *Rule, _ *Context) { r.Node = "never" }},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	for src, want := range map[string]string{"a": "a", "b": "any", "c": "any"} {
		out, err := j.Parse(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if out != want {
			t.Fatalf("%q: got %v, want %q", src, out, want)
		}
	}

	// An empty sequence before the token alternates matches without
	// consuming, exactly as it did: it is a candidate for every tin.
	k := altIndexInstance(nil)
	ka := k.Token("#A")
	k.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{P: "item"},
			&AltSpec{S: [][]Tin{{ka}}, A: func(r *Rule, _ *Context) { r.Node = "direct" }},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	k.Rule("item", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ka}}, A: func(r *Rule, _ *Context) { r.Node = "item" }})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}, A: func(r *Rule, _ *Context) { r.Parent.Node = r.Node }})
	})
	out, err := k.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	if out != "item" {
		t.Fatalf("got %v, want item", out)
	}
}

func TestAltIndexSeesATokenSetOverriddenAfterTheRule(t *testing.T) {
	// tabnas/parser#217, in the form all three runtimes pin: a rule on
	// #KEY installed first, KEY narrowed to #TX afterwards. The index is
	// built from the slots as the parsing instance resolves them.
	rules := `{"options":{"rule":{"start":"top"}},
	  "rule":{"top":{"open":[{"s":"#KEY","a":"@value$"}],"close":[{"s":"#ZZ"}]}}}`
	narrow := `{"options":{"tokenSet":{"KEY":["#TX",null,null,null]}}}`
	j := Make(Options{})
	for _, g := range []string{rules, narrow} {
		spec, err := GrammarSpecFromJSON([]byte(g))
		if err != nil {
			t.Fatal(err)
		}
		if err := j.Grammar(spec); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := j.Parse("a"); err != nil || out != "a" {
		t.Fatalf("a: got %v, %v", out, err)
	}
	for _, src := range []string{"1", `"s"`, "true"} {
		if out, err := j.Parse(src); err == nil {
			t.Fatalf("%q: accepted as %v after KEY was narrowed to #TX", src, out)
		}
	}
}

func TestAltIndexHandlesManyAlternates(t *testing.T) {
	extra := map[string]string{}
	for i := 0; i < 300; i++ {
		extra[fmt.Sprintf("#K%d", i)] = fmt.Sprintf("k%d", i)
	}
	j := altIndexInstance(extra)
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		for i := 0; i < 300; i++ {
			n := i
			rs.AddOpen(&AltSpec{
				S: [][]Tin{{j.Token(fmt.Sprintf("#K%d", n))}},
				A: func(r *Rule, _ *Context) { r.Node = n },
			})
		}
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	for _, n := range []int{0, 1, 150, 299} {
		out, err := j.Parse(fmt.Sprintf("k%d", n))
		if err != nil {
			t.Fatalf("k%d: %v", n, err)
		}
		if out != n {
			t.Fatalf("k%d: got %v", n, out)
		}
	}
	if _, err := j.Parse("a"); err == nil {
		t.Fatal("a should be refused")
	}
}

func TestAltIndexFollowsAlternatesReorderedDuringAParse(t *testing.T) {
	// `top = item item`: the first item builds the index for item's open
	// state; an action after it reorders item's alternates (same length,
	// same first pointer); the second item must be parsed by the new
	// order, not by the cached one.
	j := altIndexInstance(nil)
	ta := j.Token("#A")
	second := true
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{P: "item"})
		// The close pass runs once per child that returns: the first time
		// it reorders item and pushes the second item, the next time it
		// takes the end of the source.
		rs.AddClose(&AltSpec{
			C: func(*Rule, *Context) bool { return second },
			P: "item",
			A: func(r *Rule, ctx *Context) {
				second = false
				ctx.Inst.RSM()["item"].ModifyOpen(&AltModListOpts{Move: []int{1, 2}})
			},
		})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	var seen []string
	j.Rule("item", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{ta}}, C: func(*Rule, *Context) bool { return false }},
			&AltSpec{S: [][]Tin{{ta}}, A: func(*Rule, *Context) { seen = append(seen, "second") }},
			&AltSpec{S: [][]Tin{{ta}}, A: func(*Rule, *Context) { seen = append(seen, "third") }},
		)
		rs.AddClose(&AltSpec{})
	})
	if _, err := j.Parse("aa"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"second", "third"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("alternates run %v, want %v", seen, want)
	}
}
