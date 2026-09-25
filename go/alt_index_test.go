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

func TestAltIndexIgnoresAListReplacedBeforeTheScan(t *testing.T) {
	// `top = item item`. On its first visit item's before-open action
	// reorders item's own alternates, and ModifyOpen builds a fresh list
	// of the same length. That visit still scans the list it read before
	// the action ran, so an index built from it must not be kept as the
	// index for the new list: the second visit would then look for `b`
	// where the old order had it.
	j := altIndexInstance(nil)
	ta, tb, tc := j.Token("#A"), j.Token("#B"), j.Token("#C")
	second := true
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{P: "item"})
		rs.AddClose(&AltSpec{
			C: func(*Rule, *Context) bool { return second },
			P: "item",
			A: func(*Rule, *Context) { second = false },
		})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	var seen []string
	reordered := false
	j.Rule("item", func(rs *RuleSpec, _ *Parser) {
		rs.AddBO(func(r *Rule, _ *Context) {
			if !reordered {
				reordered = true
				r.Spec.ModifyOpen(&AltModListOpts{Move: []int{1, 2}})
			}
		})
		// The `#C` alternate comes first and fetches the token, so the
		// scan consults the index for everything after it.
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{tc}}},
			&AltSpec{S: [][]Tin{{ta}}, A: func(*Rule, *Context) { seen = append(seen, "a") }},
			&AltSpec{S: [][]Tin{{tb}}, A: func(*Rule, *Context) { seen = append(seen, "b") }},
		)
		rs.AddClose(&AltSpec{})
	})
	if _, err := j.Parse("ab"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("alternates run %v, want %v", seen, want)
	}
}

func TestAltIndexSelectsAgainWhenAConditionRetagsTheFirstToken(t *testing.T) {
	// A condition may change the buffered token's tin and reject. The
	// full scan then tests every later alternate against the token as it
	// is now, so the candidates have to follow it: the second `#A`
	// candidate retags `a` as `#B`, the third can no longer take it, and
	// the `#B` alternate after them does.
	j := altIndexInstance(nil)
	ta, tb := j.Token("#A"), j.Token("#B")
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{ta}}, C: func(*Rule, *Context) bool { return false },
				A: func(r *Rule, _ *Context) { r.Node = "first" }},
			&AltSpec{S: [][]Tin{{ta}}, C: func(_ *Rule, ctx *Context) bool {
				ctx.T[0].Tin = tb
				return false
			}, A: func(r *Rule, _ *Context) { r.Node = "second" }},
			&AltSpec{S: [][]Tin{{ta}}, A: func(r *Rule, _ *Context) { r.Node = "third" }},
			&AltSpec{S: [][]Tin{{tb}}, A: func(r *Rule, _ *Context) { r.Node = "b" }},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	for _, src := range []string{"a", "b"} {
		out, err := j.Parse(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if out != "b" {
			t.Fatalf("%q: got %v, want %q", src, out, "b")
		}
	}
}

func TestAltIndexScansInFullOnceAConditionEditsTheAlternates(t *testing.T) {
	// A rejecting condition may edit the rule's alternates through
	// ModifyOpen: here it turns the `#B` alternate after it into an `#A`
	// one, in place. The index the scan selected from no longer describes
	// them, and the full scan would reach the edited alternate, so the rest
	// of this one does.
	j := altIndexInstance(nil)
	ta, tb := j.Token("#A"), j.Token("#B")
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{ta}}, C: func(r *Rule, _ *Context) bool {
				r.Spec.ModifyOpen(&AltModListOpts{Custom: func(list []*AltSpec) []*AltSpec {
					list[1].S = [][]Tin{{ta}}
					return list
				}})
				return false
			}},
			&AltSpec{S: [][]Tin{{tb}}, A: func(r *Rule, _ *Context) { r.Node = "edited" }},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	out, err := j.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	if out != "edited" {
		t.Fatalf("got %v, want %q", out, "edited")
	}
}

func TestAltIndexScanRereadsSlotsAConditionEditsInPlace(t *testing.T) {
	// With a token set on the instance, slots declared by name are resolved
	// once per parse and remembered (Context.altS), and building the index
	// resolves every alternate of the rule at once. A rejecting condition
	// that edits a later alternate's slot and names in place must still be
	// seen by the rest of the scan, as it was when each alternate was
	// resolved only as the scan reached it.
	a, b, c := "a", "b", "c"
	j := Make(Options{
		Rule:     &RuleOptions{Start: "top"},
		Fixed:    &FixedOptions{Token: map[string]*string{"#A": &a, "#B": &b, "#C": &c}},
		TokenSet: map[string][]string{"SET": {"#C"}},
	})
	ta, tb := j.Token("#A"), j.Token("#B")
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{ta}}, SNames: [][]string{{"#A"}}, C: func(r *Rule, _ *Context) bool {
				r.Spec.ModifyOpen(&AltModListOpts{Custom: func(list []*AltSpec) []*AltSpec {
					list[1].S = [][]Tin{{ta}}
					list[1].SNames = [][]string{{"#A"}}
					return list
				}})
				return false
			}},
			&AltSpec{S: [][]Tin{{tb}}, SNames: [][]string{{"#B"}}, A: func(r *Rule, _ *Context) { r.Node = "edited" }},
		)
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	out, err := j.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	if out != "edited" {
		t.Fatalf("got %v, want %q", out, "edited")
	}
}

func TestAltIndexGateColumnsAreSparse(t *testing.T) {
	// A column is the tins a slot names, ascending and without repeats:
	// a slot set by hand to a tin far beyond the registered ones must not
	// size a column by it, and membership answers exactly, whether the
	// column is scanned (short) or searched (long).
	high := Tin(1 << 30)
	idx := buildAltIndex(&Context{}, []*AltSpec{
		{S: [][]Tin{{high}, {3, 1}}},
		{S: [][]Tin{{1, 3, 3}}},
	})
	if got := idx.cols[0]; !reflect.DeepEqual(got, []Tin{1, 3, high}) {
		t.Fatalf("slot 0 column: %v", got)
	}
	if got := idx.cols[1]; !reflect.DeepEqual(got, []Tin{1, 3}) {
		t.Fatalf("slot 1 column: %v", got)
	}
	for _, tin := range []Tin{1, 3, high} {
		if !idx.expects(0, tin) {
			t.Fatalf("slot 0 should expect %d", tin)
		}
	}
	for _, tin := range []Tin{0, 2, 4, high - 1, -1} {
		if idx.expects(0, tin) {
			t.Fatalf("slot 0 should not expect %d", tin)
		}
	}
	if idx.expects(1, high) || idx.expects(2, 1) || idx.expects(-1, 1) {
		t.Fatal("a tin outside its slot, or a slot outside the index, is not expected")
	}
	if got := idx.named(3); !reflect.DeepEqual(got, []int32{1}) {
		t.Fatalf("named(3): %v", got)
	}

	long := &AltSpec{S: [][]Tin{{}}}
	for i := 19; 0 <= i; i-- {
		long.S[0] = append(long.S[0], Tin(5*i+2))
	}
	idx = buildAltIndex(&Context{}, []*AltSpec{long})
	if len(idx.cols[0]) != 20 || idx.cols[0][0] != 2 || idx.cols[0][19] != 97 {
		t.Fatalf("long column: %v", idx.cols[0])
	}
	for i := 0; i < 20; i++ {
		if !idx.expects(0, Tin(5*i+2)) {
			t.Fatalf("long column should expect %d", 5*i+2)
		}
		if idx.expects(0, Tin(5*i+3)) {
			t.Fatalf("long column should not expect %d", 5*i+3)
		}
	}
}
