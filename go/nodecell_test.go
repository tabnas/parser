// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"reflect"
	"testing"
)

// Rule.NodeCell / Rule.SetNode: the container identity and in-place
// replacement a rule-done subscriber needs (the Go counterpart of Rust's
// `Rc::as_ptr(&rule.node)` and a write through the shared cell). Each
// behaviour is checked against both ways a Go grammar builds a list:
// the native-value builtins (exact ownership bookkeeping) and Go actions
// that assign r.Node directly (the strict-JSON fixture, makeJSON).

// makeBuiltinList is a list grammar built only from `$`-builtins: `top`
// allocates with @array$ and pushes `elem`, which pushes `val` and
// appends it with @push$, replacing itself per separator.
func makeBuiltinList(t *testing.T) *Tabnas {
	t.Helper()
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	err := j.Grammar(&GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"top": {
			Open:  []*GrammarAltSpec{{S: "#OS", P: "elem", A: "@array$"}},
			Close: []*GrammarAltSpec{{S: "#CS"}},
		},
		"elem": {
			Open: []*GrammarAltSpec{{P: "val"}},
			Close: []*GrammarAltSpec{
				{S: "#CA", R: "elem", A: "@push$"},
				{S: "#CS", B: 1, A: "@push$"},
			},
		},
		"val": {
			Open: []*GrammarAltSpec{{S: "#NR", A: "@value$"}},
		},
	}})
	if err != nil {
		t.Fatalf("grammar: %v", err)
	}
	return j
}

func listLen(v any) int {
	l, ok := listHeader(v)
	if !ok {
		return -1
	}
	return len(l)
}

// Every list-holding rule building one list reports the same cell, and
// that cell is the rule that owns the list.
func TestNodeCellSharedList(t *testing.T) {
	cases := []struct {
		name  string
		j     *Tabnas
		owner string
	}{
		{"builtins", makeBuiltinList(t), "top"},
		{"go-actions", makeJSON(), "list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var owner *Rule
			cells := map[*Rule]bool{}
			ownerCells := map[*Rule]bool{}
			elems := 0
			c.j.SubRuleDone(func(r *Rule, _ *Context, _ RuleDone) {
				if r.Name == c.owner {
					if owner == nil {
						owner = r
					}
					ownerCells[r.NodeCell()] = true
				}
				if r.Name == "elem" {
					elems++
					cells[r.NodeCell()] = true
				}
			})
			out, err := c.j.Parse("[1,2,3]")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(out, []any{1.0, 2.0, 3.0}) {
				t.Fatalf("result: %#v", out)
			}
			if owner == nil || 6 != elems {
				t.Fatalf("owner=%v elem events=%d", owner, elems)
			}
			if 1 != len(cells) || !cells[owner] {
				t.Errorf("elem rules report %d cells, want one: the %q rule", len(cells), c.owner)
			}
			if 1 != len(ownerCells) || !ownerCells[owner] {
				t.Errorf("the %q rule's cell is not itself", c.owner)
			}
		})
	}
}

// Nested lists are distinct containers: the inner lists get cells of
// their own, distinct from the outer list's and from each other.
func TestNodeCellNestedListsAreDistinct(t *testing.T) {
	j := makeJSON()
	byList := map[*Rule]*Rule{}
	var lists []*Rule
	j.SubRuleDone(func(r *Rule, _ *Context, _ RuleDone) {
		if r.Name == "list" {
			if _, seen := byList[r]; !seen {
				lists = append(lists, r)
			}
			byList[r] = r.NodeCell()
		}
	})
	if _, err := j.Parse("[[1],[2]]"); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if 3 != len(lists) {
		t.Fatalf("list rules: %d", len(lists))
	}
	seen := map[*Rule]bool{}
	for _, l := range lists {
		if byList[l] != l {
			t.Errorf("list rule %d: cell is rule %d, want itself", l.I, byList[l].I)
		}
		seen[byList[l]] = true
	}
	if 3 != len(seen) {
		t.Errorf("nested lists share cells: %d distinct of 3", len(seen))
	}
}

// SetNode with a truncated slice from a rule-done subscriber sticks: the
// next append grows from the truncated list, and the parse result is the
// truncated list.
func TestSetNodeTruncatesInPlace(t *testing.T) {
	cases := []struct {
		name string
		make func() *Tabnas
	}{
		{"builtins", func() *Tabnas { return makeBuiltinList(t) }},
		{"go-actions", func() *Tabnas { return makeJSON() }},
	}
	for _, c := range cases {
		t.Run(c.name+"/prune-all", func(t *testing.T) {
			j := c.make()
			var lens []int
			var streamed []any
			j.SubRuleDone(func(r *Rule, _ *Context, done RuleDone) {
				if r.Name != "elem" || CLOSE != done.State {
					return
				}
				cell := r.NodeCell()
				l, _ := cell.Node.([]any)
				lens = append(lens, len(l))
				streamed = append(streamed, l...)
				r.SetNode(l[:0])
			})
			out, err := j.Parse("[1,2,3]")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(out, []any{}) {
				t.Errorf("result: %#v, want []", out)
			}
			// Each close sees only the one element appended since the
			// last prune: the truncation reached the rule doing the next
			// append, rather than being discarded by it.
			if !reflect.DeepEqual(lens, []int{1, 1, 1}) {
				t.Errorf("lengths at each close: %v, want [1 1 1]", lens)
			}
			if !reflect.DeepEqual(streamed, []any{1.0, 2.0, 3.0}) {
				t.Errorf("streamed: %#v", streamed)
			}
		})
		t.Run(c.name+"/keep-first", func(t *testing.T) {
			j := c.make()
			j.SubRuleDone(func(r *Rule, _ *Context, done RuleDone) {
				if r.Name != "elem" || CLOSE != done.State {
					return
				}
				if l, _ := r.NodeCell().Node.([]any); 1 < len(l) {
					r.SetNode(l[:1])
				}
			})
			out, err := j.Parse("[1,2,3]")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(out, []any{1.0}) {
				t.Errorf("result: %#v, want [1]", out)
			}
		})
	}
}

// A plain assignment is NOT a write through the cell: it changes one
// rule's copy and the next append discards it. This is the behaviour
// SetNode exists to avoid, pinned so the contrast stays documented.
func TestAssigningNodeDoesNotTruncate(t *testing.T) {
	j := makeJSON()
	j.SubRuleDone(func(r *Rule, _ *Context, done RuleDone) {
		if r.Name == "elem" && CLOSE == done.State {
			if l, _ := r.NodeCell().Node.([]any); 0 < len(l) {
				r.Node = l[:0]
			}
		}
	})
	out, err := j.Parse("[1,2,3]")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if 0 == listLen(out) {
		t.Errorf("plain assignment truncated the result: %#v", out)
	}
}

// Maps already have identity: every rule building into one map reports
// the same cell, and the cell's node is the very map the parse returns.
func TestNodeCellMapIdentity(t *testing.T) {
	j := makeJSON()
	var mapRule *Rule
	cells := map[*Rule]bool{}
	ptrs := map[uintptr]bool{}
	j.SubRuleDone(func(r *Rule, _ *Context, _ RuleDone) {
		if r.Name == "map" && mapRule == nil {
			mapRule = r
		}
		if r.Name == "pair" {
			cell := r.NodeCell()
			cells[cell] = true
			ptrs[reflect.ValueOf(cell.Node).Pointer()] = true
		}
	})
	out, err := j.Parse(`{"a":1,"b":2,"c":3}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if mapRule == nil {
		t.Fatal("no map rule")
	}
	if 1 != len(cells) || !cells[mapRule] {
		t.Errorf("pair rules report %d cells, want one: the map rule", len(cells))
	}
	if 1 != len(ptrs) || !ptrs[reflect.ValueOf(out).Pointer()] {
		t.Errorf("cell node is not the returned map (%d pointers)", len(ptrs))
	}
	// SetNode on a map keeps the same container, so it is a no-op for
	// identity: the map edited in place is still the result.
	j2 := makeJSON()
	j2.SubRuleDone(func(r *Rule, _ *Context, done RuleDone) {
		if r.Name == "pair" && CLOSE == done.State {
			m := r.NodeCell().Node.(map[string]any)
			delete(m, "b")
			r.SetNode(m)
		}
	})
	out2, err := j2.Parse(`{"a":1,"b":2,"c":3}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(out2, map[string]any{"a": 1.0, "c": 3.0}) {
		t.Errorf("result: %#v", out2)
	}
}

// makeReplaceList builds its list across a replacement chain with the
// builtins: `top` allocates and pushes the first value, then REPLACES
// itself with `more` per separator. The owner is a rule the chain has
// left behind, reached through the ownership bookkeeping, not Parent.
func makeReplaceList(t *testing.T) *Tabnas {
	t.Helper()
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	closeAlts := func() []*GrammarAltSpec {
		return []*GrammarAltSpec{
			{S: "#CA", R: "more", A: "@push$"},
			{S: "#CS", A: "@push$"},
		}
	}
	err := j.Grammar(&GrammarSpec{Rule: map[string]*GrammarRuleSpec{
		"top": {
			Open:  []*GrammarAltSpec{{S: "#OS", P: "val", A: "@array$"}},
			Close: closeAlts(),
		},
		"more": {
			Open:  []*GrammarAltSpec{{P: "val"}},
			Close: closeAlts(),
		},
		"val": {
			Open: []*GrammarAltSpec{{S: "#NR", A: "@value$"}},
		},
	}})
	if err != nil {
		t.Fatalf("grammar: %v", err)
	}
	return j
}

func TestNodeCellAcrossReplacement(t *testing.T) {
	j := makeReplaceList(t)
	var top *Rule
	cells := map[*Rule]bool{}
	var lens []int
	j.SubRuleDone(func(r *Rule, _ *Context, done RuleDone) {
		if r.Name == "top" && top == nil {
			top = r
		}
		if r.Name == "top" || r.Name == "more" {
			cells[r.NodeCell()] = true
		}
		if r.Name == "more" && CLOSE == done.State {
			l, _ := r.NodeCell().Node.([]any)
			lens = append(lens, len(l))
			r.SetNode(l[:0])
		}
	})
	out, err := j.Parse("[1,2,3]")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if 1 != len(cells) || !cells[top] {
		t.Errorf("chain reports %d cells, want one: the top rule", len(cells))
	}
	// top pushes 1, which the first `more` sees under its own 2 and
	// prunes with it; the second `more` then sees only its 3, so the
	// truncation reached the owner the next push grows.
	if !reflect.DeepEqual(lens, []int{2, 1}) {
		t.Errorf("lengths at each close: %v, want [2 1]", lens)
	}
	if !reflect.DeepEqual(out, []any{}) {
		t.Errorf("result: %#v, want []", out)
	}
}

// makeImplicitList is a Go-action grammar in jsonic's shape for a
// bracket-less list (`1,2,3`): `val` reads the first value and REPLACES
// itself with `list` on a comma; `list` allocates in its before-open
// action, promotes the value already read, and writes the list back to
// the replaced `val`; `elem` appends and writes back to its parent.
func makeImplicitList(t *testing.T) *Tabnas {
	t.Helper()
	j := Make(Options{Rule: &RuleOptions{Start: "val"}})
	ref := map[FuncRef]any{
		"@val": AltAction(func(r *Rule, _ *Context) { r.Node = r.O0.Val }),
		"@list-bo": StateAction(func(r *Rule, _ *Context) {
			r.Node = make([]any, 0)
			if r.Prev != nil && r.Prev != NoRule {
				r.Node = append(r.Node.([]any), r.Prev.Node)
				r.Prev.Node = r.Node
			}
		}),
		"@elem-bc": StateAction(func(r *Rule, _ *Context) {
			r.Node = append(r.Node.([]any), r.Child.Node)
			r.Parent.Node = r.Node
		}),
		"@item": AltAction(func(r *Rule, _ *Context) { r.Node = r.O0.Val }),
	}
	err := j.Grammar(&GrammarSpec{Ref: ref, Rule: map[string]*GrammarRuleSpec{
		"val": {
			Open:  []*GrammarAltSpec{{S: "#NR", A: "@val"}},
			Close: []*GrammarAltSpec{{S: "#CA", R: "list"}, {S: "#ZZ"}},
		},
		"list": {
			Open:  []*GrammarAltSpec{{P: "elem"}},
			Close: []*GrammarAltSpec{{S: "#ZZ"}},
		},
		"elem": {
			Open:  []*GrammarAltSpec{{P: "item"}},
			Close: []*GrammarAltSpec{{S: "#CA", R: "elem"}, {S: "#ZZ", B: 1}},
		},
		"item": {
			Open: []*GrammarAltSpec{{S: "#NR", A: "@item"}},
		},
	}})
	if err != nil {
		t.Fatalf("grammar: %v", err)
	}
	return j
}

// The list is handed back to the rule it replaced, so for a moment the
// recorded owner (that rule) holds it too. The cell must still be the
// list rule throughout — the rule that allocated it — not flip to it
// only once the replaced rule's copy goes stale.
func TestNodeCellImplicitListIsStable(t *testing.T) {
	j := makeImplicitList(t)
	var list *Rule
	cells := map[*Rule]bool{}
	j.SubRuleDone(func(r *Rule, _ *Context, _ RuleDone) {
		if r.Name == "list" && list == nil {
			list = r
		}
		if r.Name == "list" || r.Name == "elem" {
			cells[r.NodeCell()] = true
		}
	})
	out, err := j.Parse("1,2,3")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(out, []any{1.0, 2.0, 3.0}) {
		t.Fatalf("result: %#v", out)
	}
	if 1 != len(cells) || !cells[list] {
		names := []string{}
		for c := range cells {
			names = append(names, c.Name)
		}
		t.Errorf("list and elem rules report cells %v, want only the list rule", names)
	}
}

func TestNodeCellNilAndNoRule(t *testing.T) {
	if NoRule.NodeCell() != NoRule {
		t.Error("NoRule.NodeCell() is not NoRule")
	}
	var r *Rule
	if r.NodeCell() != nil {
		t.Error("nil.NodeCell() is not nil")
	}
	r.SetNode(1)      // must not panic
	NoRule.SetNode(1) // must not touch the sentinel
	if !IsUndefined(NoRule.Node) {
		t.Errorf("SetNode wrote NoRule.Node: %#v", NoRule.Node)
	}
	solo := &Rule{Node: "x", Parent: NoRule, Prev: NoRule, Next: NoRule}
	if solo.NodeCell() != solo {
		t.Error("scalar rule's cell is not itself")
	}
	solo.SetNode("y")
	if solo.Node != "y" {
		t.Errorf("scalar SetNode: %#v", solo.Node)
	}
}

func TestSameNode(t *testing.T) {
	e1 := any(make([]any, 0))
	e2 := any(make([]any, 0))
	e1copy := e1
	full := []any{1, 2, 3}
	m1, m2 := map[string]any{}, map[string]any{}
	om := NewOrderedMap()
	lr := ListRef{Val: []any{1, 2}}
	emptyLR := ListRef{Val: make([]any, 0), Meta: map[string]any{}}
	cases := []struct {
		name string
		a, b any
		want bool
	}{
		{"distinct empty lists", e1, e2, false},
		{"copied empty list", e1, e1copy, true},
		{"truncated list", full, full[:1], true},
		{"truncated to empty", full, full[:0], true},
		{"distinct lists", []any{1}, []any{1}, false},
		{"same map", m1, m1, true},
		{"distinct maps", m1, m2, false},
		{"same ordered map", om, om, true},
		{"distinct ordered maps", om, NewOrderedMap(), false},
		{"same MapRef", MapRef{Val: m1}, MapRef{Val: m1}, true},
		{"ListRef views", lr, ListRef{Val: lr.Val[:1]}, true},
		{"ListRef vs slice", lr, lr.Val, false},
		{"re-wrapped empty ListRef", emptyLR, ListRef{Val: emptyLR.Val, Meta: emptyLR.Meta, Implicit: true}, true},
		{"distinct empty ListRefs", emptyLR, ListRef{Val: make([]any, 0), Meta: map[string]any{}}, false},
		{"nil lists", []any(nil), []any(nil), false},
		{"nil maps", map[string]any(nil), map[string]any(nil), false},
		{"scalars", 1, 1, false},
		{"undefined", Undefined, Undefined, false},
	}
	for _, c := range cases {
		if got := sameNode(c.a, c.b); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
