// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

// The library's contract, tested where it is testable.
//
// tabnas-clib-template: v6 (stamped by admin tasks/adopt-clib.sh;
// edit the template and re-stamp, not this file).
//
// The cgo shim in tabnas_c.go cannot be unit-tested (Go forbids cgo in
// _test.go), which is exactly why the behaviour lives in core.go —
// everything below runs against the same functions the exported
// symbols call.
package main

import (
	"encoding/json"
	"math/big"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const (
	validSample   = "{\"a\":[1,2],\"b\":{\"c\":true}}"
	invalidSample = "{\"a\":1,}"

	// optsSample is the tabnas_grammar argument every handle below is
	// built from: the tsv `opts` column, "" for a row that defines no
	// options (which is (NULL, 0) at the C boundary).
	optsSample = "{\"v\":2,\"options\":{\"tokenSet\":{\"KEY\":[\"#ST\"],\"VAL\":[\"#ST\",\"#NR\",\"#VL\"]}},\"rule\":{\"val\":{\"open\":[{\"s\":\"#OB\",\"p\":\"map\",\"b\":1,\"a\":\"@reset$\"},{\"s\":\"#OS\",\"p\":\"list\",\"b\":1,\"a\":\"@reset$\"},{\"s\":\"#VAL\",\"a\":\"@reset$\"}],\"close\":[{\"s\":\"#ZZ\",\"a\":\"@value$\"},{\"b\":1,\"a\":\"@value$\"}]},\"map\":{\"open\":[{\"s\":\"#OB #CB\",\"b\":1,\"a\":\"@object$\"},{\"s\":\"#OB\",\"p\":\"pair\",\"a\":\"@object$\"}],\"close\":[{\"s\":\"#CB\"}]},\"list\":{\"open\":[{\"s\":\"#OS #CS\",\"b\":1,\"a\":\"@array$\"},{\"s\":\"#OS\",\"p\":\"elem\",\"a\":\"@array$\"}],\"close\":[{\"s\":\"#CS\"}]},\"pair\":{\"open\":[{\"s\":\"#KEY #CL\",\"p\":\"val\",\"u\":{\"pair\":true},\"a\":\"@key$\"}],\"close\":[{\"s\":\"#CA\",\"r\":\"pair\",\"a\":\"@setval$\"},{\"s\":\"#CB\",\"b\":1,\"a\":\"@setval$\"}]},\"elem\":{\"open\":[{\"p\":\"val\"}],\"close\":[{\"s\":\"#CA\",\"r\":\"elem\",\"a\":\"@push$\"},{\"s\":\"#CS\",\"b\":1,\"a\":\"@push$\"}]}}}"

	// Inputs the format accepts whose parsed value holds a byte that is
	// not UTF-8 in an object member's key, in an object member's value,
	// and inside an array: the tsv utf8_key, utf8_value and utf8_array
	// columns, "" where the format cannot express the case.
	utf8KeySample   = "{\"\xff\":1}"
	utf8ValueSample = "{\"a\":\"\xff\"}"
	utf8ArraySample = "[\"\xff\"]"
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

// encoding/json folds a byte that is not UTF-8 to U+FFFD without an
// error, so a value holding one is withheld rather than corrupted: the
// reply is an accept with valueError and no value. The samples put the
// byte where this format's own parse result keeps it: an object member's
// key, its value, and inside an array. That is the regression this
// guards: a check that missed a container (the engine's ordered object
// node, a plugin's struct) let such a value through, altered.
func TestInvalidUTF8IsWithheld(t *testing.T) {
	if !valueOut {
		t.Skip("accept/reject only: this library returns no value")
	}
	for _, c := range []struct{ where, src string }{
		{"object member key", utf8KeySample},
		{"object member value", utf8ValueSample},
		{"array", utf8ArraySample},
	} {
		t.Run(c.where, func(t *testing.T) {
			if c.src == "" {
				t.Skip("the format cannot express invalid UTF-8 here (recorded, not hidden)")
			}
			if utf8.ValidString(c.src) {
				t.Fatalf("sample %q holds no invalid UTF-8", c.src)
			}
			h := loadHandle(t)
			defer freeGrammar(h)
			m := decode(t, parseWith(h, c.src))
			if m["ok"] != true || m["accept"] != true {
				t.Fatalf("sample %q must be accepted: %v", c.src, m)
			}
			if v, has := m["value"]; has {
				t.Fatalf("sample %q: value emitted with its bytes replaced: %v", c.src, v)
			}
			if ve, _ := m["valueError"].(string); !strings.Contains(ve, "UTF-8") {
				t.Fatalf("sample %q: no UTF-8 valueError: %v", c.src, m)
			}
		})
	}
}

// The value check walks every container a parse result can hold, built
// here directly so that each library holds the walk to the whole set,
// whatever its own format returns. The engine's object node is not built
// here (this package does not import the engine); the samples above
// reach it through the formats that return it. The local types have the
// shapes of the engine's MapRef, ListRef and Text wrappers, and of the
// structs format plugins return.
func TestJSONUnsafeWalksEveryContainer(t *testing.T) {
	const bad = "\xff"
	type text struct{ Quote, Str string }
	type listRef struct {
		Val      []any
		Implicit bool
		Child    any
		Meta     map[string]any
	}
	type mapRef struct {
		Val      map[string]any
		Implicit bool
		Meta     map[string]any
	}
	type Line struct {
		Moves []*text `json:"moves"`
	}
	type game struct {
		Tags map[string]string `json:"tags"`
		Line
		note   string
		Hidden string `json:"-"`
	}
	for _, c := range []struct {
		name string
		val  any
		want string
	}{
		{"string", bad, unsafeUTF8},
		{"map key", map[string]any{bad: 1.0}, unsafeUTF8},
		{"map value", map[string]any{"a": bad}, unsafeUTF8},
		{"array element", []any{"x", bad}, unsafeUTF8},
		{"nested", map[string]any{"a": []any{map[string]any{"b": bad}}}, unsafeUTF8},
		{"typed map key", map[string]int{bad: 1}, unsafeUTF8},
		{"typed map value", map[string]string{"a": bad}, unsafeUTF8},
		{"typed array", []string{"x", bad}, unsafeUTF8},
		{"Text field", text{Quote: `"`, Str: bad}, unsafeUTF8},
		{"ListRef element", listRef{Val: []any{bad}}, unsafeUTF8},
		{"ListRef child", &listRef{Child: bad}, unsafeUTF8},
		{"MapRef key", mapRef{Val: map[string]any{bad: 1.0}}, unsafeUTF8},
		{"struct map value", []*game{{Tags: map[string]string{"Event": bad}}}, unsafeUTF8},
		{"embedded struct field", &game{Line: Line{Moves: []*text{{Str: bad}}}}, unsafeUTF8},
		{"unexported field, never emitted", game{note: bad}, ""},
		{`json:"-" field, never emitted`, game{Hidden: bad}, ""},
		{"raw JSON", json.RawMessage(`["` + bad + `"]`), unsafeUTF8},
		{"bytes, emitted as base64", []byte(bad), ""},
		{"big number in a struct", struct{ N *big.Int }{big.NewInt(1)}, unsafeNumber},
		{"clean", map[string]any{"a": []any{"é", 1.0, true, nil, text{Str: "x"}}}, ""},
	} {
		if got := jsonUnsafe(c.val); got != c.want {
			t.Errorf("%s: jsonUnsafe = %q, want %q", c.name, got, c.want)
		}
	}

	// A value that contains itself is refused, not walked forever; one
	// that only shares a part is not a cycle.
	self := []any{nil}
	self[0] = self
	loop := map[string]any{}
	loop["loop"] = []any{loop}
	ref := &listRef{}
	ref.Child = ref
	shared := []any{"x"}
	for name, c := range map[string]struct {
		val  any
		want string
	}{
		"array":           {self, unsafeCycle},
		"map":             {loop, unsafeCycle},
		"pointer":         {ref, unsafeCycle},
		"shared, acyclic": {[]any{shared, map[string]any{"a": shared}}, ""},
	} {
		if got := jsonUnsafe(c.val); got != c.want {
			t.Errorf("%s: jsonUnsafe = %q, want %q", name, got, c.want)
		}
	}

	// Through the reply: withheld with a reason, never emitted altered.
	if valueOut {
		m := decode(t, acceptDoc(map[string]any{"a": []any{bad}}))
		ve, _ := m["valueError"].(string)
		if _, has := m["value"]; has || !strings.Contains(ve, "UTF-8") {
			t.Fatalf("value with invalid UTF-8 was not withheld: %v", m)
		}
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
