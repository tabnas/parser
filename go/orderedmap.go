// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"bytes"
	"encoding/json"
	"sort"
)

// OrderedMap is the parser's object node: a string-keyed map that
// remembers the ORDER its keys were discovered while parsing (insertion
// order). It replaces the bare Go map[string]any that could not order its
// keys, bringing the Go engine into parity with the TypeScript engine,
// whose plain JS objects preserve insertion order natively.
//
// Keys holds the ordered key list; Vals holds the key→value pairs. Both
// are exported so consumers can range in order (`for _, k := range
// om.Keys { v := om.Vals[k] }`) or reach the raw map directly. Set keeps
// the two in sync and is the only mutator the parser uses.
//
// Sorting is OPT-IN: a Sorted OrderedMap (built when the object$ "sort"
// config or the Sort option is on) presents its keys in sorted order
// instead of insertion order. Insertion order is still recorded; the
// Sorted flag only changes the order Keys()/MarshalJSON expose.
type OrderedMap struct {
	Keys   []string       // key order (insertion order, or sorted when Sorted)
	Vals   map[string]any // key → value
	Sorted bool           // when true, Keys/ordered views are alphabetical
}

// NewOrderedMap returns an empty insertion-ordered map.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{Vals: map[string]any{}}
}

// NewSortedMap returns an empty map whose ordered views are sorted.
func NewSortedMap() *OrderedMap {
	return &OrderedMap{Vals: map[string]any{}, Sorted: true}
}

// Set assigns val under key. A key's position is fixed at first
// insertion; re-setting an existing key updates its value but keeps its
// place (standard JSON object semantics: last value wins, first position
// wins). A Sorted map keeps Keys sorted as it grows.
func (o *OrderedMap) Set(key string, val any) {
	if o.Vals == nil {
		o.Vals = map[string]any{}
	}
	if _, exists := o.Vals[key]; !exists {
		if o.Sorted {
			i := sort.SearchStrings(o.Keys, key)
			o.Keys = append(o.Keys, "")
			copy(o.Keys[i+1:], o.Keys[i:])
			o.Keys[i] = key
		} else {
			o.Keys = append(o.Keys, key)
		}
	}
	o.Vals[key] = val
}

// Get returns the value under key and whether it was present.
func (o *OrderedMap) Get(key string) (any, bool) { v, ok := o.Vals[key]; return v, ok }

// Has reports whether key is present.
func (o *OrderedMap) Has(key string) bool { _, ok := o.Vals[key]; return ok }

// Len returns the number of keys.
func (o *OrderedMap) Len() int { return len(o.Keys) }

// Delete removes key, preserving the order of the rest.
func (o *OrderedMap) Delete(key string) {
	if _, ok := o.Vals[key]; !ok {
		return
	}
	delete(o.Vals, key)
	for i, k := range o.Keys {
		if k == key {
			o.Keys = append(o.Keys[:i], o.Keys[i+1:]...)
			break
		}
	}
}

// Clone returns a shallow copy with its own Keys slice and Vals map
// (values are shared; deepClone handles recursive copies).
func (o *OrderedMap) Clone() *OrderedMap {
	keys := make([]string, len(o.Keys))
	copy(keys, o.Keys)
	vals := make(map[string]any, len(o.Vals))
	for k, v := range o.Vals {
		vals[k] = v
	}
	return &OrderedMap{Keys: keys, Vals: vals, Sorted: o.Sorted}
}

// MarshalJSON emits the object with keys in order (insertion order, or
// sorted when Sorted), so encoding/json round-trips preserve source
// order instead of Go's alphabetical map marshaling.
func (o *OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(o.Vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// AsStringMap returns the underlying key→value map for either an
// OrderedMap (its Vals) or a plain map[string]any, easing migration of
// consumers that only need value access (not order). The bool reports
// whether v was one of those object shapes.
func AsStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case *OrderedMap:
		return m.Vals, true
	case OrderedMap:
		return m.Vals, true
	case map[string]any:
		return m, true
	}
	return nil, false
}
