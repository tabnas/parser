package jsonic

import (
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestGrammarTextSettingAppendsTags(t *testing.T) {
	j := Make()
	err := grammarText(j, `
		rule: {
			val: {
				close: [
					{ s: "#ZZ", g: "first" },
					{ s: "#CA", g: "second" }
				]
			}
		}
	`, &tabnas.GrammarSetting{
		Rule: &tabnas.GrammarSettingRule{Alt: &tabnas.GrammarSettingAlt{G: "common"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tags := altGTags(t, j, "val", "close")
	if !containsTagSet(tags, []string{"common", "first"}) {
		t.Errorf("missing first+common, got %v", tags)
	}
	if !containsTagSet(tags, []string{"common", "second"}) {
		t.Errorf("missing second+common, got %v", tags)
	}
}

func TestSetOptionsTextBasic(t *testing.T) {
	j := Make()
	if _, err := setOptionsFromText(j, `number: { sep: "_" }`); err != nil {
		t.Fatal(err)
	}
	result, err := j.Parse("a:1_000")
	if err != nil {
		t.Fatal(err)
	}
	if m := result.(map[string]any); m["a"] != float64(1000) {
		t.Errorf("expected a:1000, got %v", m["a"])
	}
}

func TestSetOptionsTextEmptyIsNoop(t *testing.T) {
	j := Make()
	if _, err := setOptionsFromText(j, ""); err != nil {
		t.Fatal(err)
	}
	result, err := j.Parse("a:1")
	if err != nil {
		t.Fatal(err)
	}
	if m := result.(map[string]any); m["a"] != float64(1) {
		t.Errorf("expected a:1, got %v", m["a"])
	}
}

func TestSetOptionsTextMergesWithSetOptions(t *testing.T) {
	j := Make()
	if _, err := setOptionsFromText(j, `number: { sep: "_" }`); err != nil {
		t.Fatal(err)
	}
	yes := true
	j.SetOptions(tabnas.Options{Number: &tabnas.NumberOptions{Hex: &yes}})

	// Separator from text still applies.
	result, err := j.Parse("a:1_000")
	if err != nil {
		t.Fatal(err)
	}
	if m := result.(map[string]any); m["a"] != float64(1000) {
		t.Errorf("expected a:1000 after merge, got %v", m["a"])
	}

	// Hex from later SetOptions also applies.
	result2, err := j.Parse("b:0xff")
	if err != nil {
		t.Fatal(err)
	}
	if m := result2.(map[string]any); m["b"] != float64(255) {
		t.Errorf("expected b:255, got %v", m["b"])
	}
}

func TestSetOptionsTextInvalidSource(t *testing.T) {
	// Tabnas is lenient about missing closing braces, so the error must
	// come from a lexer-level failure — here, an unterminated string.
	j := Make()
	if _, err := setOptionsFromText(j, `number: { sep: "`); err == nil {
		t.Error("expected parse error on malformed options text")
	}
}
