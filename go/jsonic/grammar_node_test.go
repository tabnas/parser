package jsonic

// Tests for the unexported node list/map helpers in grammar.go, and for
// the preprocessEscapes TSV test helper. Moved from the engine package
// with the grammar (engine/grammar split).

import (
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

// --- node list helpers (grammar.go) ---

func TestNodeListHelpers(t *testing.T) {
	// ListRef form.
	lr := tabnas.ListRef{Val: []any{1}}
	out := nodeListAppend(lr, 2).(tabnas.ListRef)
	if len(out.Val) != 2 || out.Val[1] != 2 {
		t.Errorf("ListRef append failed: %v", out.Val)
	}
	v, ok := nodeListVal(out)
	if !ok || len(v) != 2 {
		t.Error("nodeListVal on ListRef failed")
	}

	// Plain []any form.
	arr := nodeListAppend([]any{1}, 2).([]any)
	if len(arr) != 2 {
		t.Errorf("array append failed: %v", arr)
	}
	if _, ok := nodeListVal(arr); !ok {
		t.Error("nodeListVal on []any failed")
	}

	// Non-list node passes through unchanged.
	if nodeListAppend("x", 1) != "x" {
		t.Error("non-list node should pass through")
	}
	if _, ok := nodeListVal("x"); ok {
		t.Error("nodeListVal on non-list should be false")
	}
}

func TestNodeMapHelpers(t *testing.T) {
	// MapRef form.
	mr := tabnas.MapRef{Val: map[string]any{}}
	nodeMapSet(mr, "k", 1)
	if v, ok := nodeMapGet(mr, "k"); !ok || v != 1 {
		t.Error("MapRef set/get failed")
	}
	if nodeMapGetVal(mr, "k") != 1 {
		t.Error("nodeMapGetVal on MapRef failed")
	}
	// Non-map node.
	if _, ok := nodeMapGet("x", "k"); ok {
		t.Error("nodeMapGet on non-map should be false")
	}
}

// --- preprocessEscapes (TSV fixture helper, see helpers_test.go) ---

func TestPreprocessEscapes(t *testing.T) {
	if preprocessEscapes("") != "" {
		t.Error("empty passthrough")
	}
	got := preprocessEscapes(`a\nb\rc\td\qe\`)
	if got != "a\nb\rc\td\\qe\\" {
		t.Errorf("escape processing failed: %q", got)
	}
}
