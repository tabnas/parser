/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// Two defects on one code path, both found by tabnas/csv while sweeping
// its option handling (#198, #197): deepMerge recognised three concrete
// map types and let a DECLARED map type replace the defaults instead of
// merging onto them, and deepClone walked a cyclic value until the
// process died with a fatal stack overflow. The canonical runtime sees
// a plain object in the first case and throws a catchable RangeError in
// the second.

import (
	"reflect"
	"testing"
)

// Field stands in for csv's declared option map.
type Field map[string]any

// Ring stands in for csv's declared self-referencing container.
type Ring []any

func TestDeepMergeDeclaredMapTypeMergesOntoDefaults(t *testing.T) {
	defaults := map[string]any{
		"string": map[string]any{"quote": `"`, "csv": false},
	}
	got := Deep(map[string]any{}, defaults, map[string]any{
		"string": Field{"csv": true},
	}).(map[string]any)

	str := got["string"].(map[string]any)
	if str["quote"] != `"` {
		t.Fatalf("declared map replaced the defaults: quote lost, got %#v", str)
	}
	if str["csv"] != true {
		t.Fatalf("overlay value lost: %#v", str)
	}

	// The same overlay as a plain map is the control: both spellings
	// must land on the same merged value.
	plain := Deep(map[string]any{}, defaults, map[string]any{
		"string": map[string]any{"csv": true},
	}).(map[string]any)
	if !reflect.DeepEqual(plain, got) {
		t.Fatalf("plain and declared overlays diverge:\n plain    %#v\n declared %#v", plain, got)
	}
}

func TestDeepMergeDeclaredMapAsBase(t *testing.T) {
	base := Field{"a": 1, "nested": map[string]any{"x": 1}}
	got := Deep(base, map[string]any{"b": 2, "nested": map[string]any{"y": 2}}).(map[string]any)
	want := map[string]any{"a": 1, "b": 2, "nested": map[string]any{"x": 1, "y": 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDeepMergeDeclaredMapInsideOrderedMap(t *testing.T) {
	om := NewOrderedMap()
	om.Set("string", map[string]any{"quote": `"`})
	got := Deep(om, map[string]any{"string": Field{"csv": true}}).(*OrderedMap)
	str, _ := got.Get("string")
	m := str.(map[string]any)
	if m["quote"] != `"` || m["csv"] != true {
		t.Fatalf("got %#v", m)
	}
}

func TestDeepCloneKeepsDeclaredKinds(t *testing.T) {
	ring := Ring{1, "two"}
	cloned := deepClone(ring)
	got, ok := cloned.(Ring)
	if !ok {
		t.Fatalf("clone of a Ring is a %T", cloned)
	}
	if &got[0] == &ring[0] {
		t.Fatal("clone shares the backing array")
	}
	if !reflect.DeepEqual(got, ring) {
		t.Fatalf("got %#v, want %#v", got, ring)
	}

	field := Field{"k": []any{1}}
	fc := deepClone(field).(Field)
	fc["k"].([]any)[0] = 2
	if field["k"].([]any)[0] != 1 {
		t.Fatal("clone of a declared map shares its nested slice")
	}

	typed := map[string]int{"a": 1}
	tc := deepClone(typed).(map[string]int)
	tc["a"] = 2
	if typed["a"] != 1 {
		t.Fatal("clone of a typed map aliases the original")
	}
}

// A cyclic value must not take the process down. Every one of these
// died in deepClone before the fix; the declared kind died later, in
// whatever plugin code walked it. Each is spelled the way a caller
// would hand it to UseDefaults, and the assertion is that the merge
// RETURNS. The clone mirrors the cycle rather than erroring, so the
// caller can decide what to do with it, as it can in TypeScript after
// catching the RangeError.
func TestDeepCloneSurvivesCyclicValues(t *testing.T) {
	cases := map[string]func() any{
		"slice": func() any {
			a := make([]any, 1)
			a[0] = a
			return a
		},
		"map": func() any {
			m := map[string]any{}
			m["self"] = m
			return m
		},
		"orderedmap": func() any {
			om := NewOrderedMap()
			om.Set("self", om)
			return om
		},
		"declared-slice": func() any {
			r := make(Ring, 1)
			r[0] = r
			return r
		},
		"declared-map": func() any {
			f := Field{}
			f["self"] = f
			return f
		},
		"mutual": func() any {
			a := map[string]any{}
			b := map[string]any{"a": a}
			a["b"] = b
			return a
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			val := mk()
			j := Make()
			var seen any
			err := j.UseDefaults(func(_ *Tabnas, opts map[string]any) error {
				seen = opts["field"].(map[string]any)["empty"]
				return nil
			}, map[string]any{"field": map[string]any{"empty": nil}},
				map[string]any{"field": map[string]any{"empty": val}})
			if err != nil {
				t.Fatalf("UseDefaults: %v", err)
			}
			if seen == nil {
				t.Fatal("the cyclic value did not reach the plugin")
			}
			if reflect.ValueOf(seen).Pointer() == reflect.ValueOf(val).Pointer() {
				t.Fatal("the plugin was handed the caller's own container, not a clone")
			}
		})
	}

	// The clone of a self-referencing slice is itself self-referencing:
	// the memo resolves the inner reference to the outer clone.
	a := make([]any, 1)
	a[0] = a
	c := deepClone(a).([]any)
	if inner, ok := c[0].([]any); !ok || &inner[0] != &c[0] {
		t.Fatalf("clone does not mirror the cycle: %T", c[0])
	}
}

// Two references to ONE container inside an acyclic value resolve to one
// clone, so shared structure stays shared rather than being duplicated.
func TestDeepCloneSharesRepeatedContainers(t *testing.T) {
	shared := map[string]any{"x": 1}
	val := map[string]any{"a": shared, "b": shared}
	c := deepClone(val).(map[string]any)
	if reflect.ValueOf(c["a"]).Pointer() != reflect.ValueOf(c["b"]).Pointer() {
		t.Fatal("one container was cloned twice")
	}
	if reflect.ValueOf(c["a"]).Pointer() == reflect.ValueOf(shared).Pointer() {
		t.Fatal("clone aliases the original")
	}
}
