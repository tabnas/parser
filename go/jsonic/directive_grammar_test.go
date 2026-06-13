package jsonic

import tabnas "github.com/tabnas/parser/go"

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Minimal directive-style plugin defined inline for the test. A directive
// binds a fixed OPEN token to a named rule that reads the following val
// and replaces it with the result of an action callback. Mirrors the
// essential shape of the JS @tabnas/directive plugin — token + rule +
// transform — without the close-token, rule-filtering, or error-plumbing
// surface area. Kept in-test so the core repo carries no plugin dependency.
func defineDirective(j *tabnas.Tabnas, name, open string, action func(any) any) {
	OPEN := j.Token("#OD_"+name, open)

	j.Rule(name, func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.BO = []tabnas.StateAction{func(r *tabnas.Rule, ctx *tabnas.Context) {
			r.Node = nil
		}}
		rs.Open = []*tabnas.AltSpec{
			{P: "val"},
		}
		rs.BC = []tabnas.StateAction{func(r *tabnas.Rule, ctx *tabnas.Context) {
			var childNode any
			if r.Child != nil && r.Child != tabnas.NoRule {
				childNode = r.Child.Node
			}
			r.Node = action(childNode)
		}}
	})

	j.Rule("val", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.Open = append([]*tabnas.AltSpec{{
			S: [][]tabnas.Tin{{OPEN}},
			P: name,
		}}, rs.Open...)
	})
}

func makeDirectiveJ() *tabnas.Tabnas {
	j := Make()
	defineDirective(j, "upper", "@up", func(val any) any {
		return strings.ToUpper(fmt.Sprintf("%v", val))
	})
	defineDirective(j, "wrap", "@wrap", func(val any) any {
		return map[string]any{"wrapped": val}
	})
	return j
}

func runDirective(t *testing.T, name, src string, want any) {
	t.Helper()
	j := makeDirectiveJ()
	got, err := j.Parse(src)
	if err != nil {
		t.Fatalf("%s: Parse(%q) error: %v", name, src, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: Parse(%q)\n  got:      %#v\n  expected: %#v",
			name, src, got, want)
	}
}

func TestDirectiveUpperString(t *testing.T) {
	runDirective(t, "upper-string", `@up "hello"`, "HELLO")
}

func TestDirectiveUpperBare(t *testing.T) {
	runDirective(t, "upper-bare", `@up hello`, "HELLO")
}

func TestDirectiveUpperNumber(t *testing.T) {
	runDirective(t, "upper-number", `@up 42`, "42")
}

func TestDirectiveWrapNumber(t *testing.T) {
	runDirective(t, "wrap-number", `@wrap 42`,
		map[string]any{"wrapped": float64(42)})
}

func TestDirectiveWrapKeyword(t *testing.T) {
	runDirective(t, "wrap-keyword", `@wrap true`,
		map[string]any{"wrapped": true})
}

func TestDirectiveInList(t *testing.T) {
	runDirective(t, "in-list", `[1, @up "x", 2]`,
		[]any{float64(1), "X", float64(2)})
}

func TestDirectiveInMap(t *testing.T) {
	runDirective(t, "in-map", `{a: @up "v", b: @wrap 3}`,
		map[string]any{
			"a": "V",
			"b": map[string]any{"wrapped": float64(3)},
		})
}

func TestDirectiveNested(t *testing.T) {
	runDirective(t, "nested", `@wrap @up "hi"`,
		map[string]any{"wrapped": "HI"})
}

func TestDirectiveWrappingList(t *testing.T) {
	runDirective(t, "wrapping-list", `@wrap [1, @up "x"]`,
		map[string]any{"wrapped": []any{float64(1), "X"}})
}

func TestDirectiveWrappingMap(t *testing.T) {
	runDirective(t, "wrapping-map", `@wrap {k: @up "v"}`,
		map[string]any{"wrapped": map[string]any{"k": "V"}})
}
