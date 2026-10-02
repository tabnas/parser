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
	for _, value := range []any{float64(1), float64(3), float64(MaxRuleHistory), nil} {
		opts, err := OptionsFromMap(map[string]any{
			"rule": map[string]any{"history": value},
		})
		if err != nil {
			t.Fatalf("history %v: %v", value, err)
		}
		if value == nil {
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
}
