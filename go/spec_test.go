package tabnas

// Shared .tsv conformance fixtures, run against the strict-JSON fixture.
// Mirrors what the canonical TypeScript suite runs (ts/test/json-spec.test.js):
// the include-json* fixtures (ASCII and UTF-8, value and error cases).
// Relaxed-grammar fixtures are not run here — the engine ships no grammar
// and the strict fixture only accepts JSON.parse-equivalent input.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tsvRow holds a row from a TSV fixture file.
type tsvRow struct {
	cols   []string
	lineNo int
}

// loadTSV reads a TSV file and returns its rows (excluding the header).
func loadTSV(path string) ([]tsvRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []tsvRow
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo == 1 {
			continue // skip header
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		rows = append(rows, tsvRow{cols: cols, lineNo: lineNo})
	}
	return rows, scanner.Err()
}

// preprocessEscapes converts literal \n, \r and \t escape sequences in
// TSV fixture fields into their real characters.
func preprocessEscapes(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	var out []rune
	i := 0
	for i < len(runes) {
		if runes[i] == '\\' && i+1 < len(runes) {
			switch runes[i+1] {
			case 'n':
				out = append(out, '\n')
				i += 2
			case 'r':
				out = append(out, '\r')
				i += 2
			case 't':
				out = append(out, '\t')
				i += 2
			default:
				out = append(out, runes[i])
				i++
			}
		} else {
			out = append(out, runes[i])
			i++
		}
	}
	return string(out)
}

// parseExpected parses the expected JSON string into a Go value.
func parseExpected(s string) (any, error) {
	if s == "" {
		return nil, nil
	}
	var val any
	if err := json.Unmarshal([]byte(s), &val); err != nil {
		return nil, err
	}
	return val, nil
}

// stripRefs unwraps ListRef / MapRef / Text back to plain Go values so
// they compare against JSON-unmarshaled expected values.
func stripRefs(v any) any {
	switch val := v.(type) {
	case ListRef:
		out := make([]any, len(val.Val))
		for i, e := range val.Val {
			out[i] = stripRefs(e)
		}
		return out
	case MapRef:
		out := make(map[string]any)
		for k, e := range val.Val {
			out[k] = stripRefs(e)
		}
		return out
	case Text:
		return val.Str
	case map[string]any:
		out := make(map[string]any)
		for k, e := range val {
			out[k] = stripRefs(e)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = stripRefs(e)
		}
		return out
	default:
		return v
	}
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any)
		for k, e := range val {
			out[k] = normalizeValue(e)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = normalizeValue(e)
		}
		return out
	case float64:
		if val == 0 {
			return float64(0)
		}
		return val
	default:
		return v
	}
}

func valuesEqual(got, expected any) bool {
	return deepCompare(normalizeValue(got), normalizeValue(expected))
}

func deepCompare(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bVal, ok := bv[k]
			if !ok || !deepCompare(v, bVal) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !deepCompare(av[i], bv[i]) {
				return false
			}
		}
		return true
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return false
		}
		if math.IsNaN(av) && math.IsNaN(bv) {
			return true
		}
		return av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return reflect.DeepEqual(a, b)
	}
}

func formatValue(v any) string {
	if v == nil {
		return "nil"
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

// specDir returns the path to the shared spec directory, relative to the
// go/ engine package test working directory.
func specDir() string {
	return filepath.Join("..", "test", "spec")
}

// runParserTSV runs a 2-column TSV (input, expected) against j.
func runParserTSV(t *testing.T, file string, j *Tabnas) {
	t.Helper()
	rows, err := loadTSV(filepath.Join(specDir(), file))
	if err != nil {
		t.Fatalf("failed to load %s: %v", file, err)
	}
	for _, row := range rows {
		if len(row.cols) < 2 {
			continue
		}
		input := preprocessEscapes(row.cols[0])
		expected, err := parseExpected(row.cols[1])
		if err != nil {
			t.Errorf("%s line %d: bad expected %q: %v", file, row.lineNo, row.cols[1], err)
			continue
		}
		got, err := j.Parse(input)
		if err != nil {
			t.Errorf("%s line %d: Parse(%q) error: %v", file, row.lineNo, input, err)
			continue
		}
		if !valuesEqual(stripRefs(got), expected) {
			t.Errorf("%s line %d: Parse(%q)\n  got:      %s\n  expected: %s",
				file, row.lineNo, input, formatValue(stripRefs(got)), formatValue(expected))
		}
	}
}

// runErrorTSV runs a 2-column TSV (input, ERROR:<code>) against j.
func runErrorTSV(t *testing.T, file string, j *Tabnas) {
	t.Helper()
	rows, err := loadTSV(filepath.Join(specDir(), file))
	if err != nil {
		t.Fatalf("failed to load %s: %v", file, err)
	}
	for _, row := range rows {
		if len(row.cols) < 2 {
			continue
		}
		input := preprocessEscapes(row.cols[0])
		expectedStr := row.cols[1]
		if !strings.HasPrefix(expectedStr, "ERROR:") {
			t.Errorf("%s line %d: expected must start with ERROR:, got %q", file, row.lineNo, expectedStr)
			continue
		}
		want := strings.TrimPrefix(expectedStr, "ERROR:")
		_, parseErr := j.Parse(input)
		if parseErr == nil {
			t.Errorf("%s line %d: Parse(%q) should error (want %s), got nil", file, row.lineNo, input, want)
			continue
		}
		je, ok := parseErr.(*TabnasError)
		if !ok {
			t.Errorf("%s line %d: Parse(%q) error should be *TabnasError, got %T", file, row.lineNo, input, parseErr)
			continue
		}
		if je.Code != want {
			t.Errorf("%s line %d: Parse(%q) error code got %q, want %q", file, row.lineNo, input, je.Code, want)
		}
	}
}

func TestSpecIncludeJSON(t *testing.T) {
	for _, name := range []string{"include-json.tsv", "include-json-utf8.tsv"} {
		runParserTSV(t, name, makeJSON())
	}
}

func TestSpecIncludeJSONErrors(t *testing.T) {
	for _, name := range []string{"include-json-errors.tsv", "include-json-utf8-errors.tsv"} {
		runErrorTSV(t, name, makeJSON())
	}
}
