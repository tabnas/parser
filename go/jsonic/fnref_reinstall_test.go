package jsonic

import tabnas "github.com/tabnas/parser/go"

import "testing"

// TestFnrefNoReinstallOnSubsequentCall is the Go counterpart of the JS
// fnref-no-reinstall regression test. It verifies that a second Grammar()
// call registering an unrelated reserved handler (e.g. @pair-ao) does not
// re-install previously-registered reserved handlers (e.g. @pair-bc), and
// that map.merge fires exactly once per duplicate key.
func TestFnrefNoReinstallOnSubsequentCall(t *testing.T) {
	mergeCalls := 0
	merge := func(prev, val any, _ *tabnas.Rule, _ *tabnas.Context) any {
		mergeCalls++
		if arr, ok := prev.([]any); ok {
			return append(arr, val)
		}
		return []any{prev, val}
	}

	j := Make(tabnas.Options{Map: &tabnas.MapOptions{Merge: merge}})

	var pairRS *tabnas.RuleSpec
	j.Rule("pair", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) { pairRS = rs })
	pairBefore := len(pairRS.BC)

	// Register an unrelated @pair-ao via Grammar. This must not touch BC.
	err := j.Grammar(&tabnas.GrammarSpec{
		Rule: map[string]*tabnas.GrammarRuleSpec{"pair": {}},
		Ref: map[tabnas.FuncRef]any{
			"@pair-ao": tabnas.StateAction(func(_ *tabnas.Rule, _ *tabnas.Context) {}),
		},
	})
	if err != nil {
		t.Fatalf("Grammar returned error: %v", err)
	}

	j.Rule("pair", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) { pairRS = rs })
	pairAfter := len(pairRS.BC)
	if pairAfter != pairBefore {
		t.Fatalf("pair BC count changed: before=%d after=%d (unrelated @pair-ao must not re-install @pair-bc)",
			pairBefore, pairAfter)
	}

	mergeCalls = 0
	if _, err := j.Parse("a:{x:1},a:{y:2},a:{z:3}"); err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if mergeCalls != 2 {
		t.Fatalf("merge called %d times for 3 duplicate keys, want 2", mergeCalls)
	}
}
