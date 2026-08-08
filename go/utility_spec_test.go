// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Shared utility-*.tsv conformance fixtures.
//
// AGENTS.md states that both runtimes run the shared utility-*.tsv
// fixtures; the canonical TypeScript drivers live in
// ts/test/utility.test.js (the 'str', 'deep', 'modlist' and 'strinject'
// cases). These tests mirror those drivers row-for-row so the Go ports of
// Str/Deep/ModList/StrInject are pinned to the same shared expectations.

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
)

// tsvCol returns column i, or "" when the row is short (TS reads a missing
// trailing column as undefined and coerces it to an empty string).
func tsvCol(cols []string, i int) string {
	if i < len(cols) {
		return cols[i]
	}
	return ""
}

// jsonEqual compares a produced value against a JSON-encoded expectation,
// normalising both through JSON so map ordering and numeric types match.
func jsonEqual(t *testing.T, got any, wantJSON string) bool {
	t.Helper()

	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Errorf("cannot marshal result %#v: %v", got, err)
		return false
	}

	var gotNorm, wantNorm any
	if err := json.Unmarshal(gotBytes, &gotNorm); err != nil {
		t.Errorf("cannot normalise result %s: %v", gotBytes, err)
		return false
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantNorm); err != nil {
		t.Errorf("cannot parse expected %q: %v", wantJSON, err)
		return false
	}

	return reflect.DeepEqual(gotNorm, wantNorm)
}

// loadSpecTSV loads a shared fixture by bare name, failing the test if absent
// (an absent fixture must never pass vacuously).
func loadSpecTSV(t *testing.T, name string) []tsvRow {
	t.Helper()

	rows, err := loadTSV(specDir() + "/" + name + ".tsv")
	if err != nil {
		t.Fatalf("cannot load %s.tsv: %v", name, err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s.tsv has no rows", name)
	}
	return rows
}

// TestSpecUtilityStr mirrors ts/test/utility.test.js 'str'.
func TestSpecUtilityStr(t *testing.T) {
	for _, row := range loadSpecTSV(t, "utility-str") {
		input := tsvCol(row.cols, 0)
		maxlenCol := tsvCol(row.cols, 1)
		want := tsvCol(row.cols, 2)

		var val any
		if err := json.Unmarshal([]byte(input), &val); err != nil {
			t.Errorf("utility-str line %d: bad input %q: %v", row.lineNo, input, err)
			continue
		}

		// TS: str(val) defaults maxlen to 44.
		maxlen := 44
		if maxlenCol != "" {
			n, err := strconv.Atoi(maxlenCol)
			if err != nil {
				t.Errorf("utility-str line %d: bad maxlen %q", row.lineNo, maxlenCol)
				continue
			}
			maxlen = n
		}

		if got := Str(val, maxlen); got != want {
			t.Errorf("utility-str line %d: Str(%s, %d) = %q, want %q",
				row.lineNo, input, maxlen, got, want)
		}
	}
}

// TestSpecUtilityDeep mirrors ts/test/utility.test.js 'deep'.
func TestSpecUtilityDeep(t *testing.T) {
	for _, row := range loadSpecTSV(t, "utility-deep") {
		// TS collects args from cols 0..3, stopping at the first empty col.
		var args []any
		bad := false
		for i := 0; i < 4; i++ {
			col := tsvCol(row.cols, i)
			if col == "" {
				break
			}
			var v any
			if err := json.Unmarshal([]byte(col), &v); err != nil {
				t.Errorf("utility-deep line %d: bad arg %q: %v", row.lineNo, col, err)
				bad = true
				break
			}
			args = append(args, v)
		}
		if bad || len(args) == 0 {
			continue
		}

		want := tsvCol(row.cols, 4)
		got := Deep(args[0], args[1:]...)

		if !jsonEqual(t, got, want) {
			gotJSON, _ := json.Marshal(got)
			t.Errorf("utility-deep line %d: Deep(%v) = %s, want %s",
				row.lineNo, row.cols[:len(args)], gotJSON, want)
		}
	}
}

// TestSpecUtilityModList mirrors ts/test/utility.test.js 'modlist'.
func TestSpecUtilityModList(t *testing.T) {
	for _, row := range loadSpecTSV(t, "utility-modlist") {
		input := tsvCol(row.cols, 0)
		optsCol := tsvCol(row.cols, 1)
		want := tsvCol(row.cols, 2)

		var list []any
		if err := json.Unmarshal([]byte(input), &list); err != nil {
			t.Errorf("utility-modlist line %d: bad input %q: %v", row.lineNo, input, err)
			continue
		}

		var opts *ModListOpts
		if optsCol != "" {
			var raw struct {
				Delete []int `json:"delete"`
				Move   []int `json:"move"`
			}
			if err := json.Unmarshal([]byte(optsCol), &raw); err != nil {
				t.Errorf("utility-modlist line %d: bad opts %q: %v", row.lineNo, optsCol, err)
				continue
			}
			opts = &ModListOpts{Delete: raw.Delete, Move: raw.Move}
		}

		got := ModList(list, opts)

		if !jsonEqual(t, got, want) {
			gotJSON, _ := json.Marshal(got)
			t.Errorf("utility-modlist line %d: ModList(%s, %s) = %s, want %s",
				row.lineNo, input, optsCol, gotJSON, want)
		}
	}
}

// TestSpecUtilityStrInject mirrors ts/test/utility.test.js 'strinject'.
func TestSpecUtilityStrInject(t *testing.T) {
	for _, row := range loadSpecTSV(t, "utility-strinject") {
		template := tsvCol(row.cols, 0)
		valsCol := tsvCol(row.cols, 1)
		want := tsvCol(row.cols, 2)

		var vals any
		if valsCol != "" {
			if err := json.Unmarshal([]byte(valsCol), &vals); err != nil {
				t.Errorf("utility-strinject line %d: bad values %q: %v",
					row.lineNo, valsCol, err)
				continue
			}
		}

		if got := StrInject(template, vals); got != want {
			t.Errorf("utility-strinject line %d: StrInject(%q, %s) = %q, want %q",
				row.lineNo, template, valsCol, got, want)
		}
	}
}
