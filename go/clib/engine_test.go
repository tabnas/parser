// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package main

// The engine library's own contract, beyond the stamped core_test.go.
//
// NOT stamped: adopt-clib.sh writes a fixed file list and leaves this
// file alone, so these guarantees survive every restamp. They are the
// ones a SPEC-loading library owes and a fixed-format clib does not:
// what it must refuse to load, that the TypeScript-serialized fixture
// loads, what the engine's diagnostic and its reserved `internal` code
// look like once they cross. They run against the same core.go
// functions the exported symbols call.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plug "github.com/tabnas/parser/go"
)

// A spec that installs no start rule would load clean and then accept
// every input: the engine returns nil,nil for a missing start rule by
// design (TS agrees). The construct refuses it before a handle exists.
func TestSpecWithNoStartRuleIsRefused(t *testing.T) {
	for _, spec := range []string{
		"", "{}", "null", `{"rule":123}`, `{"rule":{}}`,
		`{"options":{"rule":{"start":"nosuchrule"}}}`,
	} {
		m := decode(t, loadGrammar(spec))
		if m["ok"] != false {
			t.Errorf("spec %q was accepted as a grammar: %v", spec, m)
			continue
		}
		if e, _ := m["error"].(map[string]any); e["code"] != "grammar" {
			t.Errorf("spec %q: code %v, want grammar", spec, e["code"])
		}
	}
}

// The serialized spec the TypeScript suite and py/ load, read from the
// file itself rather than from the copy stamped into optsSample: what
// the TS side emits must load through the C ABI and decide the samples.
func TestTheTSSerializedFixtureLoads(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(
		"..", "..", "ts", "test", "json-builder.fixture.json"))
	if err != nil {
		t.Fatalf("cannot read ts/test/json-builder.fixture.json: %v", err)
	}
	m := decode(t, loadGrammar(string(raw)))
	if m["ok"] != true {
		t.Fatalf("the TS fixture did not load: %v", m)
	}
	h := int64(m["handle"].(float64))
	defer freeGrammar(h)
	if v := decode(t, parseWith(h, validSample)); v["accept"] != true {
		t.Errorf("valid sample rejected: %v", v)
	}
	if v := decode(t, parseWith(h, invalidSample)); v["accept"] != false {
		t.Errorf("invalid sample accepted: %v", v)
	}
}

// The value crosses in the engine's insertion order (out of contract
// per ADR-15, but it is what the engine built, byte for byte).
func TestAcceptedValueIsWhatTheEngineBuilt(t *testing.T) {
	h := loadHandle(t)
	defer freeGrammar(h)
	src := `{"b":[1,2],"a":{"c":true}}`
	var probe struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal([]byte(parseWith(h, src)), &probe); err != nil {
		t.Fatal(err)
	}
	if string(probe.Value) != src {
		t.Errorf("value bytes %s, want %s", probe.Value, src)
	}
}

// A rejection carries the engine's structured diagnostic, every member
// schema/diagnostic.schema.json requires, and no value. Its message is
// one clean line: no ANSI colour, no newline. Its `version` is the
// engine's: the one place the engine version still crosses, now that
// the version document carries lib/format/template instead.
func TestRejectionIsAStructuredDiagnostic(t *testing.T) {
	h := loadHandle(t)
	defer freeGrammar(h)
	m := decode(t, parseWith(h, "{oops"))
	if m["ok"] != true || m["accept"] != false {
		t.Fatalf("not a rejection: %v", m)
	}
	if _, has := m["value"]; has {
		t.Errorf("a rejection must not carry a value: %v", m)
	}
	e, _ := m["error"].(map[string]any)
	if e["code"] != "unexpected" {
		t.Fatalf("code %v, want unexpected: %v", e["code"], m)
	}
	msg, _ := e["message"].(string)
	if msg == "" || strings.ContainsAny(msg, "\n\x1b") {
		t.Errorf("message is not one clean line: %q", msg)
	}
	if e["version"] != plug.VERSION {
		t.Errorf("diagnostic version %v, want engine VERSION %s",
			e["version"], plug.VERSION)
	}

	raw, err := os.ReadFile(filepath.Join(
		"..", "..", "schema", "diagnostic.schema.json"))
	if err != nil {
		t.Fatalf("cannot read schema/diagnostic.schema.json: %v", err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema/diagnostic.schema.json: %v", err)
	}
	if len(schema.Required) == 0 {
		t.Fatal("the diagnostic schema requires nothing; this check is off")
	}
	for _, k := range schema.Required {
		if _, has := e[k]; !has {
			t.Errorf("diagnostic lacks required member %q: %v", k, e)
		}
	}
}

// Empty input is a question, not a malformed call.
func TestEmptyInputIsAnsweredNotRefused(t *testing.T) {
	h := loadHandle(t)
	defer freeGrammar(h)
	m := decode(t, parseWith(h, ""))
	if _, answered := m["accept"]; m["ok"] != true || !answered {
		t.Errorf("empty input must get a verdict: %v", m)
	}
}

// Handles are independent: freeing one must not disturb another.
func TestHandlesAreIndependent(t *testing.T) {
	a := loadHandle(t)
	defer freeGrammar(a)
	b := loadHandle(t)
	if a == b {
		t.Fatalf("two loads returned the same handle %d", a)
	}
	freeGrammar(b)
	if m := decode(t, parseWith(a, validSample)); m["accept"] != true {
		t.Errorf("freeing one handle disturbed another: %v", m)
	}
}

// An engine bug is not a verdict on the input. The Go engine turns a
// panic in a callback, matcher or custom entry point into its reserved
// `internal` error rather than crashing, so it reaches parseWith as an
// ordinary error; reporting it as accept:false would hand the caller an
// authoritative-looking rejection for a question never answered.
//
// No serialized spec can make the engine panic, so the engine is built
// directly, with a Parser.Start that panics, and published through
// core.go's registry exactly as loadGrammar publishes a handle.
func TestInternalErrorIsACallErrorNotARejection(t *testing.T) {
	tn := plug.Make()
	tn.SetOptions(plug.Options{Parser: &plug.ParserOptions{
		Start: func(string, *plug.Tabnas, map[string]any) (any, error) {
			panic("boom inside the engine")
		},
	}})

	// The engine must have recovered it itself. A panic that escaped to
	// safeParse's recover would also come back ok:false, and this test
	// would pass without exercising the `internal` path at all.
	_, err := tn.Parse(`{"a":1}`)
	if te, ok := err.(*plug.TabnasError); !ok || te.Code != "internal" {
		t.Fatalf("precondition: the engine did not report internal: %v", err)
	}

	reg.Lock()
	nextID++
	h := nextID
	loaded[h] = &instance{parse: tn.Parse}
	reg.Unlock()
	defer freeGrammar(h)

	m := decode(t, parseWith(h, `{"a":1}`))
	if m["ok"] != false {
		t.Fatalf("an engine panic must be ok:false, not a verdict: %v", m)
	}
	if _, isVerdict := m["accept"]; isVerdict {
		t.Errorf("an engine failure must not carry an accept field: %v", m)
	}
	if e, _ := m["error"].(map[string]any); e["code"] != "internal" {
		t.Errorf("error code %v, want internal", e["code"])
	}
}
