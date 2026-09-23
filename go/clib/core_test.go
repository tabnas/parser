// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

// The library's contract, tested where it is testable.
//
// tabnas-clib-template: v3 (stamped by admin tasks/adopt-clib.sh;
// edit the template and re-stamp, not this file).
//
// The cgo shim in tabnas_c.go cannot be unit-tested (Go forbids cgo in
// _test.go), which is exactly why the behaviour lives in core.go —
// everything below runs against the same functions the exported
// symbols call.
package main

import (
	"encoding/json"
	"sync"
	"testing"
)

const (
	validSample   = "{\"a\":[1,2],\"b\":{\"c\":true}}"
	invalidSample = "{\"a\":1,}"

	// optsSample is the tabnas_grammar argument every handle below is
	// built from: the tsv `opts` column, "" for a row that defines no
	// options (which is (NULL, 0) at the C boundary).
	optsSample = "{\"v\":2,\"options\":{\"tokenSet\":{\"KEY\":[\"#ST\"],\"VAL\":[\"#ST\",\"#NR\",\"#VL\"]}},\"rule\":{\"val\":{\"open\":[{\"s\":\"#OB\",\"p\":\"map\",\"b\":1,\"a\":\"@reset$\"},{\"s\":\"#OS\",\"p\":\"list\",\"b\":1,\"a\":\"@reset$\"},{\"s\":\"#VAL\",\"a\":\"@reset$\"}],\"close\":[{\"s\":\"#ZZ\",\"a\":\"@value$\"},{\"b\":1,\"a\":\"@value$\"}]},\"map\":{\"open\":[{\"s\":\"#OB #CB\",\"b\":1,\"a\":\"@object$\"},{\"s\":\"#OB\",\"p\":\"pair\",\"a\":\"@object$\"}],\"close\":[{\"s\":\"#CB\"}]},\"list\":{\"open\":[{\"s\":\"#OS #CS\",\"b\":1,\"a\":\"@array$\"},{\"s\":\"#OS\",\"p\":\"elem\",\"a\":\"@array$\"}],\"close\":[{\"s\":\"#CS\"}]},\"pair\":{\"open\":[{\"s\":\"#KEY #CL\",\"p\":\"val\",\"u\":{\"pair\":true},\"a\":\"@key$\"}],\"close\":[{\"s\":\"#CA\",\"r\":\"pair\",\"a\":\"@setval$\"},{\"s\":\"#CB\",\"b\":1,\"a\":\"@setval$\"}]},\"elem\":{\"open\":[{\"p\":\"val\"}],\"close\":[{\"s\":\"#CA\",\"r\":\"elem\",\"a\":\"@push$\"},{\"s\":\"#CS\",\"b\":1,\"a\":\"@push$\"}]}}}"
)

func decode(t *testing.T, doc string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("reply is not JSON: %v\n%s", err, doc)
	}
	return m
}

func loadHandle(t *testing.T) int64 {
	t.Helper()
	m := decode(t, loadGrammar(optsSample))
	if m["ok"] != true {
		t.Fatalf("loadGrammar failed: %v", m)
	}
	h, ok := m["handle"].(float64)
	if !ok || h <= 0 {
		t.Fatalf("no handle in %v", m)
	}
	return int64(h)
}

func TestVersionDoc(t *testing.T) {
	m := decode(t, versionDoc())
	if m["ok"] != true || m["lib"] != libName || m["format"] != formatName {
		t.Fatalf("bad version doc: %v", m)
	}
	if m["template"] != templateVersion {
		t.Fatalf("template marker mismatch: %v", m)
	}
}

func TestAcceptsValidSample(t *testing.T) {
	h := loadHandle(t)
	defer freeGrammar(h)
	m := decode(t, parseWith(h, validSample))
	if m["ok"] != true || m["accept"] != true {
		t.Fatalf("valid sample rejected: %v", m)
	}
	if valueOut {
		if _, has := m["value"]; !has {
			t.Fatalf("valueOut set but no value in: %v", m)
		}
	}
}

// The silent-accept trap, guarded where it is cheap: a parser that
// rejects nothing is validating nothing (see parser/go/clib/core.go's
// start-rule refusal for the engine-level twin of this check).
func TestRejectsInvalidSample(t *testing.T) {
	if invalidSample == "" {
		t.Skip("format has no rejectable sample (accepts any text)")
	}
	h := loadHandle(t)
	defer freeGrammar(h)
	m := decode(t, parseWith(h, invalidSample))
	if m["ok"] != true {
		t.Fatalf("rejection must be an answer (ok:true), got: %v", m)
	}
	if m["accept"] != false {
		t.Fatalf("invalid sample accepted: %v", m)
	}
	if _, has := m["error"]; !has {
		t.Fatalf("rejection carries no error payload: %v", m)
	}
}

func TestUnknownHandle(t *testing.T) {
	m := decode(t, parseWith(1<<40, validSample))
	if m["ok"] != false {
		t.Fatalf("unknown handle must be ok:false, got: %v", m)
	}
	e, _ := m["error"].(map[string]any)
	if e == nil || e["code"] != "handle" {
		t.Fatalf("unknown handle must carry code handle: %v", m)
	}
}

func TestOptionsReserved(t *testing.T) {
	if optsDefined {
		t.Skip("this library defines its options; see TestDefinedOptionsRefuseJunk")
	}
	if m := decode(t, loadGrammar("{}")); m["ok"] != true {
		t.Fatalf("empty options object refused: %v", m)
	}
	if m := decode(t, loadGrammar(`{"x":1}`)); m["ok"] != false {
		t.Fatalf("non-empty options accepted before being defined: %v", m)
	}
	if m := decode(t, loadGrammar("not json")); m["ok"] != false {
		t.Fatalf("junk options accepted: %v", m)
	}
	// `null` unmarshals into a nil map without error; it must not slip
	// the reservation, and non-object documents must not either.
	if m := decode(t, loadGrammar("null")); m["ok"] != false {
		t.Fatalf("null options accepted: %v", m)
	}
	if m := decode(t, loadGrammar("[1]")); m["ok"] != false {
		t.Fatalf("array options accepted: %v", m)
	}
}

// A row that defines its options owns the argument, so the reservation
// above does not apply — but the construct must still refuse a document
// it cannot read, rather than build a handle from nothing.
func TestDefinedOptionsRefuseJunk(t *testing.T) {
	if !optsDefined {
		t.Skip("options are reserved; see TestOptionsReserved")
	}
	if optsSample == "" {
		t.Fatal("a row that defines options must supply an opts sample")
	}
	for _, junk := range []string{"not json", "[1]"} {
		if m := decode(t, loadGrammar(junk)); m["ok"] != false {
			t.Fatalf("junk options %q accepted: %v", junk, m)
		}
	}
}

func TestFreedHandleIsGone(t *testing.T) {
	h := loadHandle(t)
	freeGrammar(h)
	freeGrammar(h) // double free is a no-op, not a fault
	if m := decode(t, parseWith(h, validSample)); m["ok"] != false {
		t.Fatalf("freed handle still parses: %v", m)
	}
}

// FFI callers are under no obligation to serialise; the per-instance
// mutex is load-bearing, and the -race detector holds this test to it.
func TestConcurrentParses(t *testing.T) {
	h := loadHandle(t)
	defer freeGrammar(h)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				m := map[string]any{}
				_ = json.Unmarshal([]byte(parseWith(h, validSample)), &m)
				if m["accept"] != true {
					t.Errorf("concurrent parse rejected: %v", m)
					return
				}
			}
		}()
	}
	wg.Wait()
}
