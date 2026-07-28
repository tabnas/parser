package tabnas

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestOrderedMapUnmarshalRoundTrip pins that json.Unmarshal into an OrderedMap
// preserves source key order (top-level and nested) and round-trips.
func TestOrderedMapUnmarshalRoundTrip(t *testing.T) {
	src := `{"zebra":1,"alpha":2,"nested":{"y":3,"x":4}}`
	var om OrderedMap
	if err := json.Unmarshal([]byte(src), &om); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := []string{"zebra", "alpha", "nested"}; !reflect.DeepEqual(om.Keys, want) {
		t.Errorf("keys = %v, want %v", om.Keys, want)
	}
	nested, _ := om.Get("nested")
	nom, ok := nested.(*OrderedMap)
	if !ok {
		t.Fatalf("nested type = %T, want *OrderedMap", nested)
	}
	if want := []string{"y", "x"}; !reflect.DeepEqual(nom.Keys, want) {
		t.Errorf("nested keys = %v, want %v", nom.Keys, want)
	}
	if b, _ := json.Marshal(&om); string(b) != src {
		t.Errorf("round-trip = %s, want %s", b, src)
	}
}

// TestDeepMergeSorted pins that merging into a Sorted overlay yields sorted
// keys (not insertion order with a bogus Sorted flag).
func TestDeepMergeSorted(t *testing.T) {
	base := NewOrderedMap()
	base.Set("z", 1)
	over := NewSortedMap()
	over.Set("a", 3)
	over.Set("m", 2)
	res, ok := Deep(base, over).(*OrderedMap)
	if !ok {
		t.Fatalf("Deep = %T, want *OrderedMap", Deep(base, over))
	}
	if !res.Sorted {
		t.Errorf("merged result not marked Sorted")
	}
	if want := []string{"a", "m", "z"}; !reflect.DeepEqual(res.Keys, want) {
		t.Errorf("merged sorted keys = %v, want %v", res.Keys, want)
	}
}

// TestStrInjectOrderedMap pins that {key}/{key.sub} placeholders resolve from
// an OrderedMap at the top level and through a nested OrderedMap path.
func TestStrInjectOrderedMap(t *testing.T) {
	inner := NewOrderedMap()
	inner.Set("s", "x")
	om := NewOrderedMap()
	om.Set("n", "hi")
	om.Set("d", inner)
	if got := StrInject("{n} {d.s}", om); got != "hi x" {
		t.Errorf("StrInject = %q, want %q", got, "hi x")
	}
}

// TestUnwrapUndefinedOrderedMap pins that Undefined inside an OrderedMap is
// normalised to nil.
func TestUnwrapUndefinedOrderedMap(t *testing.T) {
	om := NewOrderedMap()
	om.Set("a", Undefined)
	om.Set("b", 1)
	UnwrapUndefined(om)
	if v, _ := om.Get("a"); v != nil {
		t.Errorf("a = %v, want nil", v)
	}
}

// TestMapToOptionsPlain pins that the Map.Plain opt-out is read from map/text
// configuration (not silently dropped).
func TestMapToOptionsPlain(t *testing.T) {
	opts := MapToOptions(map[string]any{"map": map[string]any{"plain": true}})
	if opts.Map == nil || opts.Map.Plain == nil || !*opts.Map.Plain {
		t.Errorf("Map.Plain not parsed from config: %+v", opts.Map)
	}
}

// TestSetOptionsTextOrderedNode pins the P1 fix: a text parser using the
// native builders returns an *OrderedMap top level, and SetOptionsText accepts
// it (via Plainify) rather than rejecting a non-map[string]any.
func TestSetOptionsTextOrderedNode(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) {
		mm := NewOrderedMap()
		mm.Set("extend", false)
		om := NewOrderedMap()
		om.Set("map", mm)
		return om, nil
	})
	if _, err := Make().SetOptionsText("map:{extend:false}"); err != nil {
		t.Errorf("SetOptionsText(ordered node): %v", err)
	}
}
