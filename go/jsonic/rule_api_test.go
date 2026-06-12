package jsonic

import (
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestGrammarCondOpDeclarativeCondition(t *testing.T) {
	// CondOp in a typed Grammar CD condition: alt fires only at depth >= 1,
	// matching the TS declarative form c: { d: { $gte: 1 } }.
	j := Make()
	depths := []int{}
	mustGrammar(t, j, &tabnas.GrammarSpec{
		Ref: map[tabnas.FuncRef]any{
			"@rec": tabnas.AltAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
				depths = append(depths, r.D)
			}),
		},
		Rule: map[string]*tabnas.GrammarRuleSpec{
			"val": {
				Close: []*tabnas.GrammarAltSpec{
					{C: map[string]any{"d": tabnas.CGte(1)}, A: "@rec", G: "custom"},
				},
			},
		},
	})

	_, err := j.Parse("a:{b:1}")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range depths {
		if d < 1 {
			t.Errorf("action fired at depth %d, expected only >= 1", d)
		}
	}
	if len(depths) == 0 {
		t.Error("expected action to fire at least once for nested value")
	}
}
