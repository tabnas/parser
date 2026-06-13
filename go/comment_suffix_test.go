package tabnas

import (
	"testing"
)

// --- Line comment suffixes ---

// --- EatLine interaction ---

// --- Block comment suffixes ---

// --- LexMatcher-form suffix ---

// --- Text round-trip ---

// --- Config-level normalization ---

func TestNormalizeCommentSuffixStringForms(t *testing.T) {
	strs, fn := normalizeCommentSuffix("abc")
	if len(strs) != 1 || strs[0] != "abc" || fn != nil {
		t.Errorf("string form: got %v fn=%v", strs, fn != nil)
	}

	strs, fn = normalizeCommentSuffix([]string{"a", "bbb", "cc"})
	// Sorted longest-first, then lexicographic.
	if len(strs) != 3 || strs[0] != "bbb" || strs[1] != "cc" || strs[2] != "a" {
		t.Errorf("longest-first sort broken: %v", strs)
	}
	if fn != nil {
		t.Errorf("unexpected fn for string-slice form")
	}
}

func TestNormalizeCommentSuffixNilAndEmpty(t *testing.T) {
	strs, fn := normalizeCommentSuffix(nil)
	if len(strs) != 0 || fn != nil {
		t.Errorf("nil should yield empty: %v %v", strs, fn != nil)
	}
	strs, fn = normalizeCommentSuffix("")
	if len(strs) != 0 || fn != nil {
		t.Errorf("empty string should yield empty: %v %v", strs, fn != nil)
	}
}
