// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Go-only introspection features: MapRef, ListRef and Text. These have no
// TypeScript equivalent and are mandated by the project authority rules,
// so they are exercised against the strict-JSON fixture with the info
// options enabled.

import "testing"

func TestInfoMapRef(t *testing.T) {
	j := makeJSON(Options{Info: &InfoOptions{Map: boolPtr(true)}})
	out, err := j.Parse(`{"a":1,"b":2}`)
	if err != nil {
		t.Fatal(err)
	}
	mr, ok := out.(MapRef)
	if !ok {
		t.Fatalf("expected MapRef, got %T", out)
	}
	if mr.Implicit {
		t.Error("brace map should be explicit (Implicit=false)")
	}
	if mr.Val["a"] != float64(1) || mr.Val["b"] != float64(2) {
		t.Errorf("MapRef.Val = %v", mr.Val)
	}
	if mr.Meta == nil {
		t.Error("MapRef.Meta should be initialised")
	}
}

func TestInfoMapRefNested(t *testing.T) {
	j := makeJSON(Options{Info: &InfoOptions{Map: boolPtr(true)}})
	out, err := j.Parse(`{"a":{"b":2}}`)
	if err != nil {
		t.Fatal(err)
	}
	mr := out.(MapRef)
	inner, ok := mr.Val["a"].(MapRef)
	if !ok {
		t.Fatalf("nested value should be MapRef, got %T", mr.Val["a"])
	}
	if inner.Val["b"] != float64(2) {
		t.Errorf("inner.Val = %v", inner.Val)
	}
}

func TestInfoListRef(t *testing.T) {
	j := makeJSON(Options{Info: &InfoOptions{List: boolPtr(true)}})
	out, err := j.Parse(`[1,2,3]`)
	if err != nil {
		t.Fatal(err)
	}
	lr, ok := out.(ListRef)
	if !ok {
		t.Fatalf("expected ListRef, got %T", out)
	}
	if lr.Implicit {
		t.Error("bracket list should be explicit (Implicit=false)")
	}
	if len(lr.Val) != 3 || lr.Val[0] != float64(1) {
		t.Errorf("ListRef.Val = %v", lr.Val)
	}
	if lr.Meta == nil {
		t.Error("ListRef.Meta should be initialised")
	}
}

func TestInfoText(t *testing.T) {
	j := makeJSON(Options{Info: &InfoOptions{Text: boolPtr(true)}})
	out, err := j.Parse(`"hello"`)
	if err != nil {
		t.Fatal(err)
	}
	tx, ok := out.(Text)
	if !ok {
		t.Fatalf("expected Text, got %T", out)
	}
	if tx.Str != "hello" {
		t.Errorf("Text.Str = %q", tx.Str)
	}
	if tx.Quote != `"` {
		t.Errorf("Text.Quote = %q, want double-quote", tx.Quote)
	}
}

func TestInfoTextInMap(t *testing.T) {
	j := makeJSON(Options{Info: &InfoOptions{Text: boolPtr(true)}})
	out, err := j.Parse(`{"k":"v"}`)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	// Map string values are wrapped in Text too.
	v, ok := m["k"].(Text)
	if !ok {
		t.Fatalf("map string value should be Text, got %T", m["k"])
	}
	if v.Str != "v" {
		t.Errorf("Text.Str = %q", v.Str)
	}
}

func TestInfoStripRefsRoundTrip(t *testing.T) {
	// stripRefs unwraps all three wrappers back to plain Go values.
	j := makeJSON(Options{Info: &InfoOptions{
		Map: boolPtr(true), List: boolPtr(true), Text: boolPtr(true),
	}})
	out, err := j.Parse(`{"a":[1,"x"]}`)
	if err != nil {
		t.Fatal(err)
	}
	plain := stripRefs(out)
	want := map[string]any{"a": []any{float64(1), "x"}}
	if !valuesEqual(plain, want) {
		t.Errorf("stripRefs = %s want %s", formatValue(plain), formatValue(want))
	}
}
