// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

// Unit tests for TabnasError.MarshalJSON beyond the shared fixture
// (diagnostic_spec_test.go), mirroring ts/test/diagnostic.test.js:
// degenerate errors marshal without failing, status/version fields, hint
// carries no display indent, src is the failing LINE, plugins lists
// registered plugins, and the emitted object satisfies
// schema/diagnostic.schema.json (dependency-free walk).

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diagJSON parses src (which must fail) and returns the decoded diagnostic.
func diagJSON(t *testing.T, j *Tabnas, src string) map[string]any {
	t.Helper()
	_, perr := j.Parse(src)
	if perr == nil {
		t.Fatalf("Parse(%q) should fail", src)
	}
	data, merr := json.Marshal(perr)
	if merr != nil {
		t.Fatalf("Marshal error for %q: %v", src, merr)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("diagnostic for %q is not JSON: %v", src, err)
	}
	return got
}

// TestDiagnosticExternalPosPreserved pins the case an in-package test cannot
// reach by accident: fullSource is unexported, so an error built by a caller
// outside this package has nothing to convert Pos against. Serialising it must
// hand back the position that was set, not clamp it to 0.
func TestDiagnosticExternalPosPreserved(t *testing.T) {
	data, err := json.Marshal(&TabnasError{Code: "unexpected", Pos: 7, Row: 1, Col: 8})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if 7.0 != got["pos"] {
		t.Errorf("pos with no source: got %v, want 7", got["pos"])
	}
}

func TestDiagnosticDegenerate(t *testing.T) {
	// A zero-value error (no code, no context, no source) must still
	// marshal to the documented shape.
	data, err := json.Marshal(&TabnasError{})
	if err != nil {
		t.Fatalf("Marshal of zero TabnasError failed: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("degenerate diagnostic is not JSON: %v", err)
	}
	if got["status"] != "failure" {
		t.Errorf("status: got %v, want failure", got["status"])
	}
	if got["code"] != "unknown" {
		t.Errorf("code fallback: got %v, want unknown", got["code"])
	}
	for _, key := range []string{"ruleStack", "expected", "plugins"} {
		arr, ok := got[key].([]any)
		if !ok || len(arr) != 0 {
			t.Errorf("%s: got %v, want []", key, got[key])
		}
	}

	// MarshalJSON has a VALUE receiver so both the value and the pointer
	// emit the diagnostic shape, and a nil pointer still encodes as null.
	vdata, verr := json.Marshal(TabnasError{})
	if verr != nil {
		t.Fatalf("Marshal of TabnasError value failed: %v", verr)
	}
	if string(vdata) != string(data) {
		t.Errorf("value vs pointer marshal differ:\n  value:   %s\n  pointer: %s", vdata, data)
	}
	var nilErr *TabnasError
	ndata, nerr := json.Marshal(nilErr)
	if nerr != nil || string(ndata) != "null" {
		t.Errorf("nil *TabnasError: got %s (err %v), want null", ndata, nerr)
	}
}

func TestDiagnosticStatusAndVersion(t *testing.T) {
	got := diagJSON(t, makeJSON(), `{"a": }`)
	if got["status"] != "failure" {
		t.Errorf("status: got %v, want failure", got["status"])
	}
	if got["version"] != VERSION {
		t.Errorf("version: got %v, want %v", got["version"], VERSION)
	}
}

func TestDiagnosticHintNoIndent(t *testing.T) {
	got := diagJSON(t, makeJSON(), `{"a": }`)
	hint, _ := got["hint"].(string)
	if hint == "" {
		t.Fatal("hint expected for the unexpected code")
	}
	for _, line := range strings.Split(hint, "\n") {
		if strings.HasPrefix(line, "  ") {
			t.Errorf("hint line carries display indent: %q", line)
		}
	}
	if hint != strings.TrimSpace(hint) {
		t.Errorf("hint not trimmed: %q", hint)
	}
}

func TestDiagnosticSrcLine(t *testing.T) {
	j := makeJSON()
	if got := diagJSON(t, j, `{"a": }`); got["src"] != `{"a": }` {
		t.Errorf("src: got %v, want the failing line", got["src"])
	}
	// Multi-line: only the line containing the error, row advances.
	got := diagJSON(t, j, "{\"a\":1,\n\"b\":]}")
	if got["row"] != float64(2) {
		t.Errorf("row: got %v, want 2", got["row"])
	}
	if got["src"] != `"b":]}` {
		t.Errorf("src: got %v, want the second line", got["src"])
	}
	// Windows line ending: the trailing \r is stripped from the line
	// (the error is on line 1, whose split-on-\n remainder ends in \r).
	if got := diagJSON(t, j, "{\"a\":]\r\n}"); got["src"] != `{"a":]` {
		t.Errorf("src: got %v, want %q", got["src"], `{"a":]`)
	}
}

// assertDiagSubset deep-compares only the keys present in wantJSON (a
// JSON object literal) against the diagnostic emitted for src.
func assertDiagSubset(t *testing.T, j *Tabnas, src, wantJSON, label string) {
	t.Helper()
	var want map[string]any
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatalf("%s: bad want literal: %v", label, err)
	}
	got := diagJSON(t, j, src)
	for key, wv := range want {
		if !deepCompare(got[key], wv) {
			t.Errorf("%s key %s:\n  got:      %s\n  expected: %s",
				label, key, formatValue(got[key]), formatValue(wv))
		}
	}
}

// TestDiagnosticAltERaiseSiteContext mirrors ts/test/diagnostic.test.js
// 'alt-e-raise-site-context'. Grammar `e` errors report the context AT
// THE RAISE SITE — the rule stack, the rule's state (which picks the
// expected-token column), and the pre-consumption lookahead token. This
// engine records the error and lets Process keep running (pops, pushes,
// the state flip), so it snapshots via ctx.parseErrDiag at the moment of
// the raise; these values are pinned IDENTICALLY on the TS side.
func TestDiagnosticAltERaiseSiteContext(t *testing.T) {
	// Close-state e: expected comes from the CLOSE alts.
	{
		j := Make(Options{Rule: &RuleOptions{Start: "top"}})
		j.Rule("top", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{{TinNR}}})
			rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
			rs.AddClose(&AltSpec{E: func(r *Rule, ctx *Context) *Token { return ctx.T0 }})
		})
		assertDiagSubset(t, j, "1 1",
			`{"code":"unexpected","row":1,"col":3,"pos":2,"len":1,
			  "rule":"top","ruleStack":["top"],"expected":["#ZZ"],
			  "token":{"name":"#NR","src":"1"}}`,
			"close-state e")
	}

	// Open-state e: expected comes from the OPEN alts (empty here — the
	// close alts DO name #ZZ, so a late state-flip would leak it).
	{
		j := Make(Options{Rule: &RuleOptions{Start: "top"}})
		j.Rule("top", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{E: func(r *Rule, ctx *Context) *Token { return ctx.T0 }})
			rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		})
		assertDiagSubset(t, j, "1",
			`{"code":"unexpected","row":1,"col":1,"pos":0,"len":0,
			  "rule":"top","ruleStack":["top"],"expected":[],
			  "token":{"name":"","src":""}}`,
			"open-state e")
	}

	// Open-state e on an alt that also pushes: the push has NOT happened
	// at the raise site, so the pushed child must not appear in the stack.
	{
		j := Make(Options{Rule: &RuleOptions{Start: "top"}})
		j.Rule("top", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{{TinNR}}, P: "child",
				E: func(r *Rule, ctx *Context) *Token { return ctx.T0 }})
			rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		})
		j.Rule("child", func(rs *RuleSpec, _ *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{{TinNR}}})
			rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		})
		assertDiagSubset(t, j, "1 1",
			`{"code":"unexpected","row":1,"col":1,"pos":0,"len":1,
			  "rule":"top","ruleStack":["top"],"expected":["#NR"],
			  "token":{"name":"#NR","src":"1"}}`,
			"open-state e with push")
	}
}

// TestDiagnosticTrailingContentFinalFetch mirrors ts/test/diagnostic.test.js
// 'trailing-content-final-fetch'. Trailing content discovered by the
// parser's FINAL lexer fetch reports the real trailing token with the
// last real rule as context (Lex.Next must not leave ctx.Rule clobbered
// to NoRule), and a trailing ignorable is not trailing content.
func TestDiagnosticTrailingContentFinalFetch(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinNR}}})
	})
	assertDiagSubset(t, j, "1 2",
		`{"code":"unexpected","row":1,"col":3,"pos":2,"len":1,
		  "rule":"top","ruleStack":["top"],"expected":[],
		  "token":{"name":"#NR","src":"2"}}`,
		"trailing content")
	if _, err := j.Parse("1 "); err != nil {
		t.Errorf("trailing space is not trailing content: %v", err)
	}
}

// TestDiagnosticUnclaimedCharToken mirrors ts/test/diagnostic.test.js
// 'unclaimed-char-token'. A character no matcher can produce is reported
// as a #BD token in both runtimes (this engine used to attach #UK on
// this path). The BMP case is fully pinned in the shared fixture ('x'
// row); the ASTRAL case has a KNOWN token.src divergence of the recorded
// scan-unit class (DIVERGENCE.md "Column positions for astral
// characters"): this engine takes one rune (the whole character), TS
// takes one UTF-16 unit (a lone high surrogate) — len is 1 in both. The
// TS test asserts the OPPOSITE src on purpose.
func TestDiagnosticUnclaimedCharToken(t *testing.T) {
	assertDiagSubset(t, makeJSON(), "\U0001F600",
		`{"code":"unexpected","row":1,"col":1,"pos":0,"len":1,
		  "rule":"val","ruleStack":["val"],
		  "token":{"name":"#BD","src":"😀"}}`,
		"unclaimed astral")
}

// diagMarkerPlugin is package-level so funcName yields a stable name.
func diagMarkerPlugin(j *Tabnas, opts map[string]any) error { return nil }

func TestDiagnosticPlugins(t *testing.T) {
	j := makeJSON()
	if err := j.Use(diagMarkerPlugin); err != nil {
		t.Fatalf("Use failed: %v", err)
	}
	got := diagJSON(t, j, `{"a": }`)
	arr, ok := got["plugins"].([]any)
	if !ok {
		t.Fatalf("plugins: got %T, want array", got["plugins"])
	}
	found := false
	for _, p := range arr {
		if s, _ := p.(string); strings.Contains(s, "diagMarkerPlugin") {
			found = true
		}
	}
	if !found {
		t.Errorf("plugins %v does not list the registered plugin", arr)
	}
}

// TestDiagnosticMatchesSchema walks schema/diagnostic.schema.json
// (dependency-free) and asserts emitted diagnostics satisfy the declared
// types, required keys, and additionalProperties: false.
func TestDiagnosticMatchesSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "schema", "diagnostic.schema.json"))
	if err != nil {
		t.Fatalf("cannot read schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}

	var check func(t *testing.T, val any, prop map[string]any, path string)
	check = func(t *testing.T, val any, prop map[string]any, path string) {
		switch prop["type"] {
		case "string":
			if _, ok := val.(string); !ok {
				t.Errorf("%s must be a string, got %T", path, val)
			}
		case "integer":
			f, ok := val.(float64)
			if !ok || f != math.Trunc(f) {
				t.Errorf("%s must be an integer, got %v", path, val)
			}
		case "array":
			arr, ok := val.([]any)
			if !ok {
				t.Errorf("%s must be an array, got %T", path, val)
				return
			}
			if items, ok := prop["items"].(map[string]any); ok {
				for i, v := range arr {
					check(t, v, items, fmt.Sprintf("%s[%d]", path, i))
				}
			}
		case "object":
			obj, ok := val.(map[string]any)
			if !ok {
				t.Errorf("%s must be an object, got %T", path, val)
				return
			}
			props, _ := prop["properties"].(map[string]any)
			if req, ok := prop["required"].([]any); ok {
				for _, k := range req {
					if _, present := obj[k.(string)]; !present {
						t.Errorf("%s missing required key %v", path, k)
					}
				}
			}
			for k, v := range obj {
				sub, known := props[k].(map[string]any)
				if !known {
					if ap, isBool := prop["additionalProperties"].(bool); isBool && !ap {
						t.Errorf("%s has unknown key %q", path, k)
					}
					continue
				}
				check(t, v, sub, path+"."+k)
			}
		}
		if c, ok := prop["const"]; ok && !deepCompare(val, c) {
			t.Errorf("%s const mismatch: got %v, want %v", path, val, c)
		}
	}

	j := makeJSON()
	for _, src := range []string{`{"a": }`, `"abc`, `{`, `[1,2,]`} {
		check(t, diagJSON(t, j, src), schema, fmt.Sprintf("diagnostic(%q)", src))
	}
}
