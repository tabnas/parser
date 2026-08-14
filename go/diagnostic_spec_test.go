// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

// Runs the shared structured-diagnostic parity fixture
// (test/spec/diagnostic.tsv) against the strict-JSON grammar. Each row's
// input must fail to parse, and the JSON SUBSET in the diagnostic column
// must deep-equal the corresponding keys of json.Marshal(err). The
// canonical TypeScript suite runs the same fixture
// (ts/test/diagnostic.test.js), keeping the two runtimes coupled on the
// structural diagnostic fields (rule/ruleStack/expected/token/len/...).

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecDiagnostic(t *testing.T) {
	rows, err := loadTSV(filepath.Join(specDir(), "diagnostic.tsv"))
	if err != nil {
		t.Fatalf("failed to load diagnostic.tsv: %v", err)
	}

	j := makeJSON()
	ran := 0
	for _, row := range rows {
		// Comment lines start with '#' and contain no tab.
		if len(row.cols) < 2 || strings.HasPrefix(row.cols[0], "#") {
			continue
		}
		ran++
		input := preprocessEscapes(row.cols[0])

		var want map[string]any
		if err := json.Unmarshal([]byte(row.cols[1]), &want); err != nil {
			t.Errorf("diagnostic.tsv line %d: bad expected %q: %v",
				row.lineNo, row.cols[1], err)
			continue
		}

		_, perr := j.Parse(input)
		if perr == nil {
			t.Errorf("diagnostic.tsv line %d: Parse(%q) should fail", row.lineNo, input)
			continue
		}

		data, merr := json.Marshal(perr)
		if merr != nil {
			t.Errorf("diagnostic.tsv line %d: Marshal error: %v", row.lineNo, merr)
			continue
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("diagnostic.tsv line %d: emitted diagnostic is not JSON: %v",
				row.lineNo, err)
			continue
		}

		for key, wv := range want {
			if !deepCompare(got[key], wv) {
				t.Errorf("diagnostic.tsv line %d: Parse(%q) key %q\n  got:      %s\n  expected: %s",
					row.lineNo, input, key, formatValue(got[key]), formatValue(wv))
			}
		}
	}

	if ran == 0 {
		t.Fatal("diagnostic.tsv contained no runnable rows")
	}
}
