package jsonic

import (
	"reflect"
	"strings"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestGrammarTextAltFields(t *testing.T) {
	// Use a fresh (unreachable) rule name so the val grammar is untouched.
	j := Make()
	err := grammarText(j, `
		rule: {
			zzz: {
				open: [
					{ s: '#OB', p: 'map', b: 1, n: {x: 1}, u: {tag: 1}, k: {prop: 1}, g: 'mytag' }
				],
				close: {
					alts: [ { s: '#ZZ', r: 'val', g: 'closer' } ],
					inject: { append: true }
				}
			}
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	rs := j.RSM()["zzz"]
	if rs == nil {
		t.Fatal("rule zzz not created")
	}
	open := rs.Open[0]
	if !reflect.DeepEqual(open.S, [][]tabnas.Tin{{tabnas.TinOB}}) {
		t.Errorf("open S failed: %v", open.S)
	}
	if open.P != "map" || open.B != 1 || open.G != "mytag" {
		t.Errorf("open p/b/g failed: %+v", open)
	}
	if open.N["x"] != 1 || open.U["tag"] != float64(1) || open.K["prop"] != float64(1) {
		t.Errorf("open n/u/k failed: %+v", open)
	}
	cl := rs.Close[0]
	if cl.R != "val" || cl.G != "closer" {
		t.Errorf("close r/g failed: %+v", cl)
	}
}

func TestGrammarTextMissingRefError(t *testing.T) {
	// GrammarText has no Ref map, so FuncRef actions must error cleanly.
	j := Make()
	err := grammarText(j, `rule: { zzz: { open: [ { a: '@nope' } ] } }`)
	if err == nil {
		t.Fatal("expected error for unresolvable FuncRef in text grammar")
	}
	if !strings.Contains(err.Error(), "@nope") {
		t.Errorf("error should mention @nope, got: %s", err)
	}
}

func TestGrammarTextInjectDeleteMove(t *testing.T) {
	// Inject delete/move modify the existing alternates before insertion.
	j := Make()
	err := grammarText(j, `
		rule: { zzz: { open: [
			{ g: 'aa' }, { g: 'bb' }
		] } }
	`)
	if err != nil {
		t.Fatal(err)
	}
	err = grammarText(j, `
		rule: { zzz: { open: {
			alts: [ { g: 'cc' } ],
			inject: { append: true, delete: [0], move: [0, 0] }
		} } }
	`)
	if err != nil {
		t.Fatal(err)
	}
	open := j.RSM()["zzz"].Open
	// aa deleted, bb remains, cc appended.
	if len(open) != 2 || open[0].G != "bb" || open[1].G != "cc" {
		tags := []string{}
		for _, a := range open {
			tags = append(tags, a.G)
		}
		t.Errorf("expected [bb cc], got %v", tags)
	}
}

func TestGrammarTextNotMapError(t *testing.T) {
	j := Make()
	err := grammarText(j, `[1,2,3]`)
	if err == nil {
		t.Fatal("expected error for non-map grammar text")
	}
	if !strings.Contains(err.Error(), "expected map") {
		t.Errorf("error should mention expected map, got: %s", err)
	}
}

func TestGrammarTextParseError(t *testing.T) {
	j := Make()
	if err := grammarText(j, `"unterminated`); err == nil {
		t.Error("expected parse error for malformed grammar text")
	}
}
