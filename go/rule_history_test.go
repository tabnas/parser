// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func historyJSONParser(t *testing.T, history *int) *Tabnas {
	t.Helper()
	spec := fixtureSpec(t, "json-builder.fixture.json")
	if spec.Options == nil {
		spec.Options = &Options{}
	}
	spec.Options.Rule = &RuleOptions{Start: "val", History: history}
	parser := Make()
	if err := parser.Grammar(spec); err != nil {
		t.Fatalf("install JSON grammar: %v", err)
	}
	return parser
}

func historyFlatArray(items int) string {
	var src strings.Builder
	src.WriteByte('[')
	for i := 0; i < items; i++ {
		if i != 0 {
			src.WriteByte(',')
		}
		fmt.Fprint(&src, i)
	}
	src.WriteByte(']')
	return src.String()
}

func historyReach(t *testing.T, parser *Tabnas, src string) (int, int, any) {
	t.Helper()
	longest, largest := 0, 0
	parser.Sub(nil, func(rule *Rule, _ *Context) {
		chain := 0
		for prev := rule.Prev; prev != nil && prev != NoRule; prev = prev.Prev {
			chain++
		}
		if chain > longest {
			longest = chain
		}

		seen := map[*Rule]bool{}
		pending := []*Rule{rule.Parent, rule.Child, rule.Prev, rule.Next}
		for 0 < len(pending) {
			last := len(pending) - 1
			snapshot := pending[last]
			pending = pending[:last]
			if snapshot == nil || snapshot == NoRule || seen[snapshot] {
				continue
			}
			seen[snapshot] = true
			pending = append(pending,
				snapshot.Parent, snapshot.Child, snapshot.Prev, snapshot.Next)
		}
		if len(seen) > largest {
			largest = len(seen)
		}
	})
	value, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return longest, largest, value
}

func TestRuleHistorySerializedAndDirectBounds(t *testing.T) {
	for _, value := range []any{float64(1), float64(3), float64(MaxRuleHistory), nil, false} {
		opts, err := OptionsFromMap(map[string]any{
			"rule": map[string]any{"history": value},
		})
		if err != nil {
			t.Fatalf("history %v: %v", value, err)
		}
		if value == nil || value == false {
			if opts.Rule == nil || opts.Rule.History != nil {
				t.Fatalf("null history = %#v", opts.Rule)
			}
		} else if opts.Rule == nil || opts.Rule.History == nil ||
			*opts.Rule.History != int(value.(float64)) {
			t.Fatalf("history %v = %#v", value, opts.Rule)
		}
	}
	for _, value := range []any{float64(0), float64(-3), 2.5, true, "3"} {
		if _, err := OptionsFromMap(map[string]any{
			"rule": map[string]any{"history": value},
		}); err == nil || !strings.Contains(err.Error(), "options.rule.history") {
			t.Fatalf("history %v: got %v", value, err)
		}
	}
	if _, err := OptionsFromMap(map[string]any{
		"rule": map[string]any{"history": float64(MaxRuleHistory + 1)},
	}); err == nil || !strings.Contains(err.Error(), "at most 16") {
		t.Fatalf("past cap: %v", err)
	}

	zero, huge := 0, int(^uint(0)>>1)
	if got := Make(Options{Rule: &RuleOptions{History: &zero}}).Config().RuleHistory; got != 1 {
		t.Fatalf("direct zero clamps to %d, want 1", got)
	}
	if got := Make(Options{Rule: &RuleOptions{History: &huge}}).Config().RuleHistory; got != MaxRuleHistory {
		t.Fatalf("direct huge clamps to %d, want %d", got, MaxRuleHistory)
	}
}

func TestRuleHistoryNullAndFalseResetAnExistingBound(t *testing.T) {
	for _, value := range []any{nil, false} {
		three := 3
		parser := historyJSONParser(t, &three)
		opts, err := OptionsFromMap(map[string]any{
			"rule": map[string]any{"history": value},
		})
		if err != nil {
			t.Fatalf("history %v: %v", value, err)
		}
		if err := parser.ApplyOptions(opts); err != nil {
			t.Fatalf("apply history %v: %v", value, err)
		}
		if parser.Config().RuleHistory != 0 {
			t.Fatalf("history %v left bound %d", value, parser.Config().RuleHistory)
		}
		chain, _, _ := historyReach(t, parser, historyFlatArray(500))
		if chain < 100 {
			t.Fatalf("history %v retained only %d predecessors", value, chain)
		}
	}
}

func TestRuleHistoryTypedNilResetsAnExistingBound(t *testing.T) {
	three := 3
	parser := historyJSONParser(t, &three)
	if err := parser.ApplyOptions(Options{Rule: &RuleOptions{HistorySet: true}}); err != nil {
		t.Fatalf("apply typed unbounded history: %v", err)
	}
	if parser.Config().RuleHistory != 0 {
		t.Fatalf("typed nil history left bound %d", parser.Config().RuleHistory)
	}
}

func TestRuleHistoryBoundsTenThousandItemsWithoutChangingValue(t *testing.T) {
	history := 3
	src := historyFlatArray(10_000)
	chain, reachable, value := historyReach(t, historyJSONParser(t, &history), src)
	if chain != history {
		t.Fatalf("prev chain = %d, want %d", chain, history)
	}
	if reachable > 32 {
		t.Fatalf("bounded reach = %d, want at most 32", reachable)
	}
	oracle := make([]any, 10_000)
	for i := range oracle {
		oracle[i] = float64(i)
	}
	if !reflect.DeepEqual(value, oracle) {
		t.Fatalf("parsed value differs: got %T len %d", value, len(value.([]any)))
	}

	_, shorter, _ := historyReach(t,
		historyJSONParser(t, &history), historyFlatArray(500))
	if reachable != shorter {
		t.Fatalf("reach grew with sequence: 500=%d, 10000=%d", shorter, reachable)
	}
}

func TestRuleHistoryOneRetainsOnePredecessor(t *testing.T) {
	history := 1
	chain, reachable, _ := historyReach(t,
		historyJSONParser(t, &history), historyFlatArray(500))
	if chain != 1 {
		t.Fatalf("prev chain = %d, want 1", chain)
	}
	if reachable > 16 {
		t.Fatalf("bounded reach = %d, want at most 16", reachable)
	}
}

func TestRuleHistoryRefreshesLivePusherButKeepsFrozenParent(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	var frozenCounter int
	var completed *Rule
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
			completed = rule.Child
			rule.Node = "ok"
		}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, N: map[string]int{"done": 1}, R: "tail"})
	})
	parser.Rule("tail", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{A: func(rule *Rule, _ *Context) {
			frozenCounter = rule.Parent.Child.N["done"]
		}})
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "ok" {
		t.Fatalf("parse = %v, %v", value, err)
	}
	if frozenCounter != 0 {
		t.Fatalf("frozen parent child counter = %d, want 0", frozenCounter)
	}
	if completed == nil || completed.Name != "child" || completed.State != CLOSE ||
		completed.N["done"] != 1 || len(completed.O) == 0 || completed.O[0].Src != "b" {
		t.Fatalf("live pusher child was not completed: %#v", completed)
	}
}

func TestRuleHistoryRepublishesChildAfterRuleDoneSetNode(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
			rule.Node = rule.Child.Node
		}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, _ *Context) {
			rule.Node = []any{"before"}
		}})
		rs.AddClose(&AltSpec{})
	})
	parser.SubRuleDone(func(rule *Rule, _ *Context, done RuleDone) {
		if rule.Name == "child" && done.State == CLOSE {
			rule.SetNode([]any{"after"})
		}
	})

	value, err := parser.Parse("abc")
	if err != nil || !reflect.DeepEqual(value, []any{"after"}) {
		t.Fatalf("parse = %#v, %v; want [after]", value, err)
	}
}

func TestRuleHistorySyncsParentNodeAfterRuleDone(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}})
		rs.AddClose(&AltSpec{})
	})
	parser.SubRuleDone(func(rule *Rule, _ *Context, done RuleDone) {
		if rule.Name == "child" && done.State == CLOSE {
			rule.Parent.SetNode("after")
		}
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "after" {
		t.Fatalf("bounded parent ruleDone write = %v, %v; want after", value, err)
	}
}

func TestRuleHistoryParentNodeWriteIsImmediateThroughRoot(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
					rule.Node = rule.Child.Node
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, ctx *Context) {
					rule.Parent.SetNode("after")
					rule.Node = ctx.Root.Node
				}})
				rs.AddClose(&AltSpec{})
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "after" {
				t.Fatalf("immediate parent/root write = %v, %v; want after", value, err)
			}
		})
	}
}

func TestRuleHistoryRootRemainsTheLiveRootAfterRuleDone(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
					rule.Node = rule.Child.Node
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, ctx *Context) {
					rule.Node = ctx.Root.U["late"]
				}})
				rs.AddClose(&AltSpec{})
			})
			parser.SubRuleDone(func(rule *Rule, _ *Context, done RuleDone) {
				if rule.Name == "top" && done.State == OPEN {
					rule.EnsureU()["late"] = "seen"
				}
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "seen" {
				t.Fatalf("live root state = %v, %v; want seen", value, err)
			}
		})
	}
}

func TestRuleHistoryParentNodeWriteIsImmediateThroughLiveStack(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
					rule.Node = rule.Child.Node
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, ctx *Context) {
					rule.Parent.SetNode("after")
					rule.Node = ctx.RS[ctx.RSI-1].Node
				}})
				rs.AddClose(&AltSpec{})
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "after" {
				t.Fatalf("live stack parent state = %v, %v; want after", value, err)
			}
		})
	}
}

func TestRuleHistorySharesAfterPushChildNode(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
				rs.AddAO(func(rule *Rule, _ *Context) { rule.Child.Node = "seed" })
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
					rule.Node = rule.Child.U["seen"]
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, _ *Context) {
					rule.EnsureU()["seen"] = rule.Node
				}})
				rs.AddClose(&AltSpec{})
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "seed" {
				t.Fatalf("after-push child seed = %v, %v; want seed", value, err)
			}
		})
	}
}

func TestRuleHistorySharesPushNodeWritesAfterRuleDone(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
			rule.Node = rule.Child.U["seen"]
		}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, ctx *Context) {
			rule.EnsureU()["seen"] = fmt.Sprintf("%v/%v", rule.Node, ctx.Root.Node)
		}})
		rs.AddClose(&AltSpec{})
	})
	parser.SubRuleDone(func(rule *Rule, _ *Context, done RuleDone) {
		if rule.Name == "top" && done.State == OPEN {
			rule.Child.Node = "child"
			rule.Node = "parent"
		}
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "child/parent" {
		t.Fatalf("post-push ruleDone nodes = %v, %v; want child/parent", value, err)
	}
}

func TestRuleHistoryAfterPushActionsSeeOnlyTheFrozenChild(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	var snapshot *Rule
	liveSawSnapshotWrite := false
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddAO(func(rule *Rule, _ *Context) {
			snapshot = rule.Child
			rule.Child.EnsureU()["snapshot-only"] = true
		})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
			rule.Node = "ok"
		}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, _ *Context) {
			_, liveSawSnapshotWrite = rule.U["snapshot-only"]
		}})
		rs.AddClose(&AltSpec{})
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "ok" {
		t.Fatalf("parse = %v, %v", value, err)
	}
	if snapshot == nil || snapshot.U["snapshot-only"] != true {
		t.Fatalf("after-push action did not receive the snapshot: %#v", snapshot)
	}
	if liveSawSnapshotWrite {
		t.Fatal("after-push action mutated the live child")
	}
}

func TestRuleHistoryPushSnapshotIncludesInheritedKeeps(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	var inherited any
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child", K: map[string]any{"x": "kept"}})
		rs.AddAO(func(rule *Rule, _ *Context) { inherited = rule.Child.K["x"] })
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
			rule.Node = "ok"
		}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}})
		rs.AddClose(&AltSpec{})
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "ok" {
		t.Fatalf("parse = %v, %v", value, err)
	}
	if inherited != "kept" {
		t.Fatalf("frozen child inherited K[x] = %v, want kept", inherited)
	}
}

func TestRuleHistorySnapshotNodeWritesReachTheLiveParent(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, _ *Context) {
			rule.Parent.SetNode("from-child")
		}})
		rs.AddClose(&AltSpec{})
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "from-child" {
		t.Fatalf("bounded parent node write = %v, %v; want from-child", value, err)
	}
}

func TestRuleHistoryKeepsTheFirstChildNodeAcrossReplacements(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
					rule.Node = rule.Child.Node
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, R: "tail", A: func(rule *Rule, _ *Context) {
					rule.Node = "first"
				}})
			})
			parser.Rule("tail", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{A: func(rule *Rule, _ *Context) {
					rule.Node = "replacement"
				}})
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "first" {
				t.Fatalf("first child node = %v, %v; want first", value, err)
			}
		})
	}
}

func TestRuleHistorySharesTheFirstChildNodeWithSuccessorPrev(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
					rule.Node = rule.Child.Node
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, R: "tail", A: func(rule *Rule, _ *Context) {
					rule.Node = "first"
				}})
			})
			parser.Rule("tail", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{A: func(rule *Rule, _ *Context) {
					rule.Prev.SetNode("updated")
				}})
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "updated" {
				t.Fatalf("shared child/prev node = %v, %v; want updated", value, err)
			}
		})
	}
}

func TestRuleHistoryParentRecordIsFrozenButNodeCellIsShared(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
			_, hasU := rule.U["snapshot"]
			_, hasN := rule.N["snapshot"]
			_, hasK := rule.K["snapshot"]
			rule.Node = fmt.Sprintf("%v/%v/%v/%v", rule.Node, hasU, hasN, hasK)
		}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, _ *Context) {
			rule.Parent.EnsureU()["snapshot"] = true
			rule.Parent.EnsureN()["snapshot"] = 1
			rule.Parent.EnsureK()["snapshot"] = true
			rule.Parent.SetNode("shared")
		}})
		rs.AddClose(&AltSpec{})
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "shared/false/false/false" {
		t.Fatalf("frozen parent = %v, %v; want shared/false/false/false", value, err)
	}
}

func TestRuleHistoryMergesPublishedAndLiveRuleDoneWrites(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
		rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(rule *Rule, _ *Context) {
			rule.Node = fmt.Sprintf("%v/%v", rule.Child.U["live"], rule.Child.U["published"])
		}})
	})
	parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}})
		rs.AddClose(&AltSpec{})
	})
	parser.SubRuleDone(func(rule *Rule, ctx *Context, done RuleDone) {
		if rule.Name == "child" && done.State == CLOSE {
			rule.EnsureU()["live"] = "yes"
			ctx.Root.Child.EnsureU()["published"] = "yes"
		}
	})

	value, err := parser.Parse("abc")
	if err != nil || value != "yes/yes" {
		t.Fatalf("merged ruleDone writes = %v, %v; want yes/yes", value, err)
	}
}

func TestRuleHistoryRecoveryPublishesAbandonedChildren(t *testing.T) {
	for _, popUntilValid := range []bool{false, true} {
		t.Run(fmt.Sprintf("pop-until-valid-%v", popUntilValid), func(t *testing.T) {
			history := 1
			parser := Make(Options{
				Rule: &RuleOptions{Start: "top", History: &history},
				Parse: &ParseOptions{Recover: &RecoverOptions{
					Enabled: true, PopUntilValid: &popUntilValid,
				}},
			})
			ta := parser.Token("#A", "a")
			tb := parser.Token("#B", "b")
			tc := parser.Token("#C", "c")
			td := parser.Token("#D", "d")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child"})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, G: "sync", A: func(rule *Rule, _ *Context) {
					rule.Node = rule.Child.U["x"]
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(rule *Rule, _ *Context) {
					rule.EnsureU()["x"] = "kept"
				}})
				// The child cannot close on C, so recovery abandons it and
				// resumes the parent at its tagged C sync point.
				rs.AddClose(&AltSpec{S: [][]Tin{{td}}})
			})

			value, errs, err := parser.ParseRecover("abc")
			if err != nil || len(errs) != 1 || value != "kept" {
				t.Fatalf("recovered parse = %v, errs=%d, err=%v; want kept and one error",
					value, len(errs), err)
			}
		})
	}
}

func TestRuleHistoryInvalidMapEntryDoesNotResetBound(t *testing.T) {
	history := 3
	parser := Make(Options{Rule: &RuleOptions{History: &history}})
	if err := parser.ApplyOptions(MapToOptions(map[string]any{
		"rule": map[string]any{"history": "bad"},
	})); err != nil {
		t.Fatalf("apply decoded subset: %v", err)
	}
	if parser.Config().RuleHistory != history {
		t.Fatalf("invalid history reset bound to %d", parser.Config().RuleHistory)
	}
}

func TestRuleHistoryKeepsRootReplacementResult(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta := parser.Token("#A", "a")
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, R: "tail", A: func(rule *Rule, _ *Context) {
			rule.Node = "old"
		}})
	})
	parser.Rule("tail", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{A: func(rule *Rule, _ *Context) { rule.Node = "new" }})
	})
	value, err := parser.Parse("a")
	if err != nil || value != "new" {
		t.Fatalf("bounded root replacement = %v, %v; want new", value, err)
	}
}

func TestRuleHistoryCutsTheLiveRootReplacementChain(t *testing.T) {
	history := 1
	parser := Make(Options{Rule: &RuleOptions{Start: "top", History: &history}})
	ta := parser.Token("#A", "a")
	var root *Rule
	parser.Sub(nil, func(rule *Rule, _ *Context) {
		if root == nil {
			root = rule
		}
	})
	parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, R: "top"})
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinZZ}}, A: func(rule *Rule, _ *Context) {
			rule.Node = "done"
		}})
		rs.AddClose(&AltSpec{})
	})

	value, err := parser.Parse(strings.Repeat("a", 1000))
	if err != nil || value != "done" {
		t.Fatalf("replacement parse = %v, %v; want done", value, err)
	}
	if root == nil || (root.Next != nil && root.Next != NoRule) {
		t.Fatalf("original root retained a live forward chain: %#v", root.Next)
	}
}

func TestRuleHistoryMerge(t *testing.T) {
	three, four := 3, 4
	left := Make(Options{Tag: "L", Rule: &RuleOptions{History: &three}})
	right := Make(Options{Tag: "R"})
	for _, pair := range [][2]*Tabnas{{left, right}, {right, left}} {
		merged, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		if merged.Options().Rule == nil || merged.Options().Rule.History == nil ||
			*merged.Options().Rule.History != three {
			t.Fatalf("merged history = %#v", merged.Options().Rule)
		}
	}
	_, err := left.Merge(Make(Options{
		Tag: "R", Rule: &RuleOptions{History: &four},
	}))
	if err == nil || !strings.Contains(err.Error(), "rule.history") {
		t.Fatalf("history conflict: %v", err)
	}
	zero, one := 0, 1
	seventeen, sixteen := 17, MaxRuleHistory
	for _, bounds := range [][2]*int{{&zero, &one}, {&seventeen, &sixteen}} {
		left := Make(Options{Tag: "L", Rule: &RuleOptions{History: bounds[0]}})
		right := Make(Options{Tag: "R", Rule: &RuleOptions{History: bounds[1]}})
		merged, err := left.Merge(right)
		if err != nil {
			t.Fatalf("effective bounds %d and %d conflict: %v", *bounds[0], *bounds[1], err)
		}
		if merged.Config().RuleHistory != *effectiveRuleHistory(bounds[0]) {
			t.Fatalf("merged effective history = %d", merged.Config().RuleHistory)
		}
	}
}

// directWriteListParser builds a list as jsonic's Go grammar does: an
// element appends its child's value to its own node and writes the grown
// list to its parent's node directly (`r.Parent.Node = r.Node`), and the
// list reads its own node once the elements end. Under a bounded rule
// history the parent an element links is a copy, so the writes landed on
// the copy and the list kept none of them (#325).
func directWriteListParser(history *int) *Tabnas {
	parser := Make(Options{Rule: &RuleOptions{Start: "list", History: history}})
	parser.Rule("list", func(rs *RuleSpec, _ *Parser) {
		rs.AddBO(func(r *Rule, _ *Context) { r.Node = make([]any, 0) })
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinOS}}, P: "elem"})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinCS}}})
	})
	parser.Rule("elem", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{P: "val"})
		rs.AddBC(func(r *Rule, _ *Context) {
			r.Node = append(r.Node.([]any), r.Child.Node)
			r.Parent.Node = r.Node
		})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinCA}}, R: "elem"})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinCS}}, B: 1})
	})
	parser.Rule("val", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinNR}}, A: func(r *Rule, _ *Context) {
			r.Node = r.O0.Val
		}})
	})
	return parser
}

func TestRuleHistoryKeepsAGrammarsDirectWritesThroughParent(t *testing.T) {
	for _, items := range []int{1, 2, 1_000, 10_000} {
		oracle := make([]any, items)
		for i := range oracle {
			oracle[i] = float64(i)
		}
		src := historyFlatArray(items)
		one, three := 1, 3
		for _, history := range []*int{nil, &one, &three} {
			name := "unbounded"
			if history != nil {
				name = fmt.Sprint(*history)
			}
			value, err := directWriteListParser(history).Parse(src)
			if err != nil {
				t.Fatalf("%d items, history %s: %v", items, name, err)
			}
			if !reflect.DeepEqual(value, oracle) {
				got := -1
				if list, ok := value.([]any); ok {
					got = len(list)
				}
				t.Fatalf("%d items, history %s: the list holds %d items (%T)", items, name, got, value)
			}
		}
	}
}

func TestRuleHistoryKeepsAGrammarsDirectWritesThroughPrev(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, R: "tail", A: func(r *Rule, _ *Context) {
					r.Node = "first"
				}})
			})
			parser.Rule("tail", func(rs *RuleSpec, _ *Parser) {
				// The rule this one replaced is the root: a write through
				// Prev is the root's, as jsonic's implicit list writes the
				// value it promotes (`r.Prev.Node = r.Node`).
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(r *Rule, _ *Context) {
					r.Prev.Node = "written"
				}})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(r *Rule, ctx *Context) {
					r.Node = ctx.Root.Node
				}})
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "written" {
				t.Fatalf("root after a write through prev = %v, %v; want written", value, err)
			}
		})
	}
}

func TestRuleHistoryParentLinkReadsTheLiveParentsNode(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprintf("bounded-%v", bounded), func(t *testing.T) {
			var history *int
			if bounded {
				one := 1
				history = &one
			}
			parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
			ta, tb, tc := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c")
			parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
				rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "child", A: func(r *Rule, _ *Context) {
					r.Node = "before"
				}})
				rs.AddClose(&AltSpec{S: [][]Tin{{tc}}, A: func(r *Rule, _ *Context) {
					r.Node = r.Child.Node
				}})
			})
			parser.Rule("child", func(rs *RuleSpec, _ *Parser) {
				// The parent's node changes by the live rule, not through
				// the link; the link reads it on the child's next pass.
				rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, A: func(_ *Rule, ctx *Context) {
					ctx.Root.Node = "live"
				}})
				rs.AddClose(&AltSpec{A: func(r *Rule, _ *Context) {
					r.Node = r.Parent.Node
				}})
			})

			value, err := parser.Parse("abc")
			if err != nil || value != "live" {
				t.Fatalf("parent read through the link = %v, %v; want live", value, err)
			}
		})
	}
}

// historyReachAll counts what a rule can reach through every link it holds,
// the engine's own included: a snapshot's record of the live rule whose node
// it shares (historyOwner) must not lead back along the sequence either.
func historyReachAll(t *testing.T, parser *Tabnas, src string) (int, any) {
	t.Helper()
	largest := 0
	parser.Sub(nil, func(rule *Rule, _ *Context) {
		seen := map[*Rule]bool{}
		pending := []*Rule{rule}
		for 0 < len(pending) {
			last := len(pending) - 1
			r := pending[last]
			pending = pending[:last]
			if r == nil || r == NoRule || seen[r] {
				continue
			}
			seen[r] = true
			pending = append(pending, r.Parent, r.Child, r.Prev, r.Next,
				r.historyOwner, r.historyPusher, r.snapshotNodeOwner, r.nodeOwner)
		}
		if len(seen) > largest {
			largest = len(seen)
		}
	})
	value, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return largest, value
}

func TestRuleHistoryKeepsDirectWritesWithoutGrowingWithTheSequence(t *testing.T) {
	three := 3
	short, _ := historyReachAll(t, directWriteListParser(&three), historyFlatArray(200))
	long, value := historyReachAll(t, directWriteListParser(&three), historyFlatArray(2_000))
	if short != long {
		t.Fatalf("reach grew with the sequence: 200=%d, 2000=%d", short, long)
	}
	if list, ok := value.([]any); !ok || len(list) != 2_000 {
		t.Fatalf("the list = %T", value)
	}
	short, _ = historyReachAll(t, historyJSONParser(t, &three), historyFlatArray(200))
	long, _ = historyReachAll(t, historyJSONParser(t, &three), historyFlatArray(2_000))
	if short != long {
		t.Fatalf("JSON reach grew with the sequence: 200=%d, 2000=%d", short, long)
	}
}

func TestRuleHistoryKeepsWritesAlongThePrevChain(t *testing.T) {
	// A value that starts with fixed tokens replaces its rule once a token,
	// and the last rule writes the value back along the chain to its head,
	// the rule the parent pushed and reads (as ini's Go grammar does for
	// `a = ==x`): within the bound, every link's write reaches its rule.
	three := 3
	for _, history := range []*int{nil, &three} {
		name := "unbounded"
		if history != nil {
			name = fmt.Sprint(*history)
		}
		parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
		ta, tx, tz := parser.Token("#A", "a"), parser.Token("#X", "x"), parser.Token("#Z", "z")
		parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{P: "v"})
			rs.AddClose(&AltSpec{S: [][]Tin{{tz}}, A: func(r *Rule, _ *Context) {
				r.Node = r.Child.Node
			}})
		})
		parser.Rule("v", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, R: "v", A: func(r *Rule, _ *Context) {
				r.Node = "a"
			}})
			rs.AddOpen(&AltSpec{S: [][]Tin{{tx}}, A: func(r *Rule, _ *Context) {
				r.Node = "x"
			}})
			rs.AddAC(func(r *Rule, _ *Context) {
				for p := r.Prev; p != nil && p != NoRule; p = p.Prev {
					r.Node = "a" + r.Node.(string)
					p.Node = r.Node
				}
			})
		})
		value, err := parser.Parse("aaxz")
		if err != nil || value != "aax" {
			t.Fatalf("history %s: %v, %v; want aax", name, value, err)
		}
	}
}

func TestRuleHistoryKeepsWritesThroughTheParentsParent(t *testing.T) {
	one := 1
	for _, history := range []*int{nil, &one} {
		parser := Make(Options{Rule: &RuleOptions{Start: "top", History: history}})
		ta, tb, tc, tz := parser.Token("#A", "a"), parser.Token("#B", "b"), parser.Token("#C", "c"), parser.Token("#Z", "z")
		parser.Rule("top", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{{ta}}, P: "mid"})
			rs.AddClose(&AltSpec{S: [][]Tin{{tz}}})
		})
		parser.Rule("mid", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{{tb}}, P: "leaf"})
			rs.AddClose(&AltSpec{})
		})
		parser.Rule("leaf", func(rs *RuleSpec, _ *Parser) {
			// As expr's Go grammar hands a paren's result up two rules.
			rs.AddOpen(&AltSpec{S: [][]Tin{{tc}}, A: func(r *Rule, _ *Context) {
				r.Parent.Parent.Node = "deep"
			}})
		})
		value, err := parser.Parse("abcz")
		if err != nil || value != "deep" {
			t.Fatalf("history %v: %v, %v; want deep", history != nil, value, err)
		}
	}
}
