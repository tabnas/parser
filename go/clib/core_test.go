package main

// The library's contract, tested where it is testable. The cgo shim in
// tabnas_c.go cannot be unit-tested (Go forbids cgo in _test.go), which
// is exactly why the behaviour lives in core.go — everything below runs
// against the same functions the exported symbols call.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func doc(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("result is not JSON: %v (%q)", err, s)
	}
	return m
}

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(
		"..", "..", "ts", "test", "json-builder.fixture.json"))
	if err != nil {
		t.Skipf("serialized grammar fixture unavailable: %v", err)
	}
	return string(b)
}

func mustLoad(t *testing.T) int64 {
	t.Helper()
	res := doc(t, loadGrammar(fixture(t)))
	if res["ok"] != true {
		t.Fatalf("load failed: %v", res)
	}
	h := int64(res["handle"].(float64))
	t.Cleanup(func() { freeGrammar(h) })
	return h
}

func TestVersionDoc(t *testing.T) {
	got := doc(t, versionDoc())
	if got["ok"] != true || got["version"] == "" {
		t.Errorf("version: %v", got)
	}
}

func TestLoadAndParse(t *testing.T) {
	h := mustLoad(t)
	for _, c := range []struct {
		src    string
		accept bool
	}{
		{`{"a":1}`, true},
		{`{"a":1,"b":[1,2]}`, true},
		{`{"a":1,}`, false},
		{`{oops`, false},
	} {
		got := doc(t, parseWith(h, c.src))
		// A rejection is an ANSWER: ok stays true, accept goes false.
		if got["ok"] != true {
			t.Errorf("%q: ok must stay true for a decided parse: %v", c.src, got)
			continue
		}
		if got["accept"] != c.accept {
			t.Errorf("%q: accept=%v, want %v", c.src, got["accept"], c.accept)
		}
		if !c.accept {
			if _, has := got["error"]; !has {
				t.Errorf("%q: a rejection must carry an error", c.src)
			}
		}
	}
}

// The engine's diagnostics are multi-line and ANSI-coloured for a
// terminal. A JSON field is neither.
func TestRejectionMessageIsOneCleanLine(t *testing.T) {
	h := mustLoad(t)
	got := doc(t, parseWith(h, `{oops`))
	msg, _ := got["error"].(map[string]any)["message"].(string)
	if msg == "" {
		t.Fatal("a rejection must carry a message")
	}
	for _, bad := range []struct {
		what string
		has  bool
	}{
		{"newline", contains(msg, "\n")},
		{"escape byte", contains(msg, "\x1b")},
	} {
		if bad.has {
			t.Errorf("message still contains a %s: %q", bad.what, msg)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ok:false is reserved for the CALL being wrong, never for a rejection —
// the distinction a caller branches on.
func TestCallErrorsAreDistinctFromRejections(t *testing.T) {
	if got := doc(t, parseWith(99999, "anything")); got["ok"] != false {
		t.Errorf("an unknown handle must be ok:false, got %v", got)
	}
	if got := doc(t, loadGrammar("{not a spec")); got["ok"] != false {
		t.Errorf("an unparseable spec must be ok:false, got %v", got)
	}
}

// The failure this guard exists to prevent is the worst one a validator
// has: a spec that installs no start rule loads clean, and then every
// input "passes". The engine returns nil,nil for a missing start rule by
// design (TS does too), so the refusal has to happen here, before a
// handle is published — otherwise silence reads as acceptance.
func TestSpecWithNoStartRuleIsRefused(t *testing.T) {
	for _, spec := range []string{
		`{}`,
		`{"rule":123}`,
		`{"rule":{}}`,
		`{"options":{"rule":{"start":"nosuchrule"}}}`,
	} {
		res := doc(t, loadGrammar(spec))
		if res["ok"] != false {
			h := int64(res["handle"].(float64))
			verdict := doc(t, parseWith(h, "literally anything at all $$$"))
			freeGrammar(h)
			t.Errorf("spec %s was accepted as a grammar; it then answered "+
				"%v for arbitrary input", spec, verdict)
		}
	}
}

// An engine bug is not a verdict on the input. The no-panic guarantee
// converts a panic in the engine, a matcher or an action into an
// "internal" error; reporting that as accept:false would give the caller
// an authoritative-looking rejection for a question never answered.
func TestInternalErrorIsACallErrorNotARejection(t *testing.T) {
	// A grammar whose parse entrypoint panics. The engine's recover turns
	// that into an "internal" *TabnasError rather than crashing, which is
	// precisely the error parseWith must not dress up as a verdict.
	tn := tabnas.Make()
	tn.SetOptions(tabnas.Options{Parser: &tabnas.ParserOptions{
		Start: func(string, *tabnas.Tabnas, map[string]any) (any, error) {
			panic("boom inside the engine")
		},
	}})

	reg.Lock()
	nextID++
	h := nextID
	loaded[h] = &grammar{tn: tn}
	reg.Unlock()
	defer freeGrammar(h)

	got := doc(t, parseWith(h, `{"a":1}`))
	if got["ok"] != false {
		t.Fatalf("an engine panic must be ok:false, not a verdict: %v", got)
	}
	if _, isVerdict := got["accept"]; isVerdict {
		t.Errorf("an engine failure must not carry an accept field: %v", got)
	}
	if code := got["error"].(map[string]any)["code"]; code != "internal" {
		t.Errorf("error code = %v, want internal", code)
	}
}

// A freed handle must stop working rather than leave an entry that still
// parses.
func TestFreedHandleStopsWorking(t *testing.T) {
	res := doc(t, loadGrammar(fixture(t)))
	h := int64(res["handle"].(float64))
	freeGrammar(h)
	if got := doc(t, parseWith(h, `{"a":1}`)); got["ok"] != false {
		t.Errorf("a freed handle must stop working, got %v", got)
	}
}

// Handles are independent: freeing one must not disturb another.
func TestHandlesAreIndependent(t *testing.T) {
	a := mustLoad(t)
	res := doc(t, loadGrammar(fixture(t)))
	b := int64(res["handle"].(float64))
	freeGrammar(b)
	if got := doc(t, parseWith(a, `{"a":1}`)); got["accept"] != true {
		t.Errorf("freeing one handle disturbed another: %v", got)
	}
}

// The reason every grammar carries a mutex: an FFI caller is under no
// obligation to serialise, and CPython releases the GIL during a ctypes
// call. Run with -race to make this meaningful.
func TestConcurrentParseIsSafe(t *testing.T) {
	h := mustLoad(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				if got := doc(t, parseWith(h, `{"a":1}`)); got["accept"] != true {
					t.Errorf("concurrent parse disagreed: %v", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}
