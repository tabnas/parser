// Copyright (c) 2013-2026 Richard Rodger, MIT License

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
	"strconv"
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

// TestSpecRuleMaxMul runs the shared rule-maxmul.tsv fixture (the TS
// counterpart is 'rule-maxmul-spec' in ts/test/json-spec.test.js).
// Columns: maxmul | via | input | expected, where expected is a JSON value
// (the parse result) or ERROR:<code> — the format test/AGENTS.md defines for
// every shared fixture in this repo.
//
// `via` is the half that matters as much as the value. Go honoured
// rule.maxmul at CONSTRUCTION and silently ignored it via SetOptions (MaxMul
// lives on the Parser, and only the Config was rebuilt), while TS reads it
// off the config at parse time and honoured both. A fixture that only ever
// constructed would have passed on a half-fixed port.
//
// maxmul 0 and -1 are the boundary the two runtimes disagreed on: TS honours
// a non-positive value literally, which is a zero budget — the rule loop
// never runs and the parse fails as `unexpected`. Go coerced it to the
// default 3 and parsed happily, so a guard you had explicitly disarmed
// silently rearmed itself.
func TestSpecRuleMaxMul(t *testing.T) {
	for _, row := range loadSpecTSV(t, "rule-maxmul") {
		maxmul, err := strconv.Atoi(tsvCol(row.cols, 0))
		if nil != err {
			t.Errorf("rule-maxmul line %d: bad maxmul %q", row.lineNo, tsvCol(row.cols, 0))
			continue
		}
		via := tsvCol(row.cols, 1)
		input := preprocessEscapes(tsvCol(row.cols, 2))
		want := tsvCol(row.cols, 3)

		var j *Tabnas
		switch via {
		case "construct":
			o := jsonOptions()
			o.Rule = &RuleOptions{MaxMul: &maxmul}
			j = Make(o)
			if err := registerJSONGrammar(j); nil != err {
				t.Fatalf("rule-maxmul line %d: %v", row.lineNo, err)
			}
		case "setoptions":
			j = makeJSON(Options{Rule: &RuleOptions{MaxMul: &maxmul}})
		default:
			t.Errorf("rule-maxmul line %d: unknown via %q", row.lineNo, via)
			continue
		}

		v, perr := j.Parse(input)

		if strings.HasPrefix(want, "ERROR:") {
			got := "<no error>"
			if nil != perr {
				got = "ERROR:" + perr.Error()
				if te, ok := perr.(*TabnasError); ok {
					got = "ERROR:" + te.Code
				}
			}
			if got != want {
				t.Errorf("rule-maxmul line %d: maxmul=%d via=%s input=%q: got %q, want %q",
					row.lineNo, maxmul, via, input, got, want)
			}
			continue
		}

		if nil != perr {
			t.Errorf("rule-maxmul line %d: maxmul=%d via=%s Parse(%q) error: %v",
				row.lineNo, maxmul, via, input, perr)
			continue
		}
		expected, eerr := parseExpected(want)
		if nil != eerr {
			t.Errorf("rule-maxmul line %d: bad expected %q: %v", row.lineNo, want, eerr)
			continue
		}
		if !valuesEqual(stripRefs(v), expected) {
			t.Errorf("rule-maxmul line %d: maxmul=%d via=%s Parse(%q)\n  got:      %s\n  expected: %s",
				row.lineNo, maxmul, via, input, formatValue(stripRefs(v)), formatValue(expected))
		}
	}
}

// TestUTF16Len pins the source-length unit the rule budget is scaled by.
// TS scales by `lex.src.length` — UTF-16 code units — and Go scaled by
// len(src), which is BYTES, so the same text bought a non-ASCII source up to
// three times the budget. Not observable through the grammars available here
// (the budget is never the binding constraint for them), so it is pinned
// directly rather than left to an integration test that cannot reach it.
func TestUTF16Len(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{string(rune(0x00E9)), 1},  // 2 bytes, 1 unit
		{string(rune(0x65E5)), 1},  // 3 bytes, 1 unit
		{string(rune(0x1F600)), 2}, // 4 bytes, 1 rune, 2 units (surrogate pair)
		{"a" + string(rune(0x1F600)) + "b", 4},
	}
	for _, c := range cases {
		if got := utf16Len(c.s); got != c.want {
			t.Errorf("utf16Len(%q) = %d, want %d (len=%d)", c.s, got, c.want, len(c.s))
		}
	}
}

// TestBudgetOverflow pins the two ends of the multiplier range, where Go's
// int arithmetic and TS's float64 arithmetic part company. The shared fixture
// carries 9007199254740991 (JS Number.MAX_SAFE_INTEGER, the largest value
// both runtimes can spell exactly); these are the values only Go has, so they
// are asserted here rather than left unpinned.
//
// Before satMul the product WRAPPED, in both directions: MaxMul=MaxInt gave a
// negative budget and refused a document TS accepts, while MaxMul=-MaxInt
// wrapped positive and accepted one TS refuses. Exactly inverted, which is
// the worst kind of "only reachable at the boundary".
func TestBudgetOverflow(t *testing.T) {
	cases := []struct {
		maxmul  int
		wantErr bool
	}{
		{math.MaxInt, false}, // enormous budget: parses, as in TS
		{-math.MaxInt, true}, // negative budget: no iterations, as in TS
		{math.MinInt, true},  // ditto, and the value satMul must not touch
	}
	for _, c := range cases {
		m := c.maxmul
		o := jsonOptions()
		o.Rule = &RuleOptions{MaxMul: &m}
		j := Make(o)
		if err := registerJSONGrammar(j); nil != err {
			t.Fatal(err)
		}
		_, err := j.Parse(`{"a":1}`)
		if c.wantErr && nil == err {
			t.Errorf("maxmul=%d: parsed, want an error", c.maxmul)
		}
		if !c.wantErr && nil != err {
			t.Errorf("maxmul=%d: %v, want a successful parse", c.maxmul, err)
		}
	}
}

// TestSatMul pins the saturating multiply itself, including the case a plain
// `a*b` gets wrong.
func TestSatMul(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{0, 5, 0},
		{5, 0, 0},
		{3, 4, 12},
		{math.MaxInt, 1, math.MaxInt},
		{math.MaxInt, 2, math.MaxInt},
		{1 << 40, 1 << 40, math.MaxInt},
	}
	for _, c := range cases {
		if got := satMul(c.a, c.b); got != c.want {
			t.Errorf("satMul(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSpecIncludeJSONErrors(t *testing.T) {
	for _, name := range []string{"include-json-errors.tsv", "include-json-utf8-errors.tsv"} {
		runErrorTSV(t, name, makeJSON())
	}
}
