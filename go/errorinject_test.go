/* Copyright (c) 2026 Richard Rodger, MIT License */

// Error-template injection and location normalisation.
//
// Both cases here are cross-runtime parity fixes driven by @tabnas/csv, whose
// message reads "Row {row} has too many fields" and whose raise site is
// ctx.T0.Bad() on a consumed lookahead slot.

package tabnas

import (
	"strings"
	"testing"
)

// A grammar detail must WIN over the built-in location key of the same name.
// csv's {row} means the CSV record, not the source line; the built-in value
// used to be injected first, so the placeholder was already gone and Go
// rendered the source row where TS rendered the record.
func TestErrorDetailOverridesLocationKey(t *testing.T) {
	p := NewParser()
	p.ErrorMessages = map[string]string{"custom": "Row {row} col {col} says {what}"}
	p.Hints = map[string]string{"custom": "hint: row {row}, what {what}"}

	je := p.makeError("custom", "s", "s", 5, 9, 4,
		map[string]any{"row": 2, "what": "boom"})

	if !strings.Contains(je.Detail, "Row 2 ") {
		t.Errorf("detail should take row from the detail bag, got %q", je.Detail)
	}
	if !strings.Contains(je.Detail, "col 4") {
		t.Errorf("an un-shadowed location key must still inject, got %q", je.Detail)
	}
	if !strings.Contains(je.Hint, "row 2") {
		t.Errorf("hint should take row from the detail bag, got %q", je.Hint)
	}
	// The error's own Row field reports the SOURCE position; only the rendered
	// template is overridden.
	if je.Row != 9 {
		t.Errorf("je.Row should stay the source row 9, got %d", je.Row)
	}
}

// The NoToken sentinel carries SI/RI/CI = -1. A grammar raising on it must not
// surface "-->  file:-1:-1", nor inject -1 for {row}/{col}. TS reports 1:1.
func TestErrorLocationNormalisedFromSentinel(t *testing.T) {
	p := NewParser()
	je := p.makeError("unexpected", "", "a,b\n1,2,3",
		NoToken.SI, NoToken.RI, NoToken.CI)

	if je.Pos != 0 || je.Row != 1 || je.Col != 1 {
		t.Errorf("sentinel position should normalise to 0/1/1, got %d/%d/%d",
			je.Pos, je.Row, je.Col)
	}
	if strings.Contains(je.Error(), ":-1:-1") {
		t.Errorf("rendered error still shows a negative location:\n%s", je.Error())
	}
}

// A real position must pass through untouched — the normalisation is a floor
// for "unknown", not a rewrite.
func TestErrorLocationRealPositionUnchanged(t *testing.T) {
	p := NewParser()
	je := p.makeError("unexpected", "x", "abc\nxyz", 4, 2, 1)
	if je.Pos != 4 || je.Row != 2 || je.Col != 1 {
		t.Errorf("real position altered: got %d/%d/%d", je.Pos, je.Row, je.Col)
	}
}
