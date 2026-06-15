// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import "sort"

// Bag of helper functions exposed to plugins, mirroring TS tabnas.util.
type UtilBag struct {
	Deep      func(base any, rest ...any) any                             // Recursive deep merge.
	Keys      func(m map[string]any) []string                             // Sorted map keys.
	Values    func(m map[string]any) []any                                // Values in key-sorted order.
	Entries   func(m map[string]any) []Entry                              // Key/value pairs, key-sorted.
	Omap      func(m map[string]any, fn func(Entry) []any) map[string]any // Map over object entries.
	Str       func(val any, maxlen int) string                            // Value to truncated string.
	StrInject func(template string, vals any) string                      // Fill {key} placeholders.

	// Lex scan primitives, for building matchers on the same driver.
	Scan                func(src string, startSI, startRI, startCI int, spec *ScanSpec, out *ScanOut) bool // Run the scan state machine.
	BuildCharRunSpec    func(chars map[rune]bool) *ScanSpec                                                // Spec matching a run of chars.
	BuildLineRunSpec    func(lineChars, rowChars map[rune]bool) *ScanSpec                                  // Spec matching line whitespace.
	BuildStringBodySpec func(cfg *LexConfig, q rune) *ScanSpec                                             // Spec matching a quoted string body.
}

// A (key, value) pair returned by Entries; the struct form of a TS 2-tuple.
type Entry struct {
	Key   string // The map key.
	Value any    // The value at that key.
}

// Keys returns the map's keys in sorted order; a nil map returns an empty slice.
// Keys(map[string]any{"b":2,"a":1}) // => ["a","b"];  Keys(nil) // => []
func Keys(m map[string]any) []string {
	if m == nil {
		return []string{}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Values returns the map's values in key-sorted order; a nil map returns an empty slice.
// Values(map[string]any{"b":2,"a":1}) // => [1,2];  Values(nil) // => []
func Values(m map[string]any) []any {
	if m == nil {
		return []any{}
	}
	out := make([]any, 0, len(m))
	for _, k := range Keys(m) {
		out = append(out, m[k])
	}
	return out
}

// Entries returns (key, value) pairs in key-sorted order; a nil map returns an empty slice.
// Entries(map[string]any{"a":1}) // => [{Key:"a",Value:1}];  Entries(nil) // => []
func Entries(m map[string]any) []Entry {
	if m == nil {
		return []Entry{}
	}
	out := make([]Entry, 0, len(m))
	for _, k := range Keys(m) {
		out = append(out, Entry{Key: k, Value: m[k]})
	}
	return out
}

// Omap maps over an object's entries, where fn returns alternating key/value
// items: [k,v] rewrites the pair, [k,v,k2,v2,...] adds keys, [nil,_] drops it.
// Omap(map[string]any{"a":1}, func(e Entry) []any { return []any{e.Key, 9} }) // => {"a":9}
func Omap(m map[string]any, fn func(Entry) []any) map[string]any {
	out := map[string]any{}
	if m == nil {
		return out
	}
	for _, e := range Entries(m) {
		var me []any
		if fn != nil {
			me = fn(e)
		} else {
			me = []any{e.Key, e.Value}
		}
		if len(me) < 2 || me[0] == nil {
			// drop
		} else if k, ok := me[0].(string); ok {
			out[k] = me[1]
		}
		i := 2
		for i+1 < len(me) && me[i] != nil {
			if k, ok := me[i].(string); ok {
				out[k] = me[i+1]
			}
			i += 2
		}
	}
	return out
}

// Util returns a UtilBag wiring the package-level helpers, mirroring TS tabnas.util.
// The helpers are stateless, so the bag is safe to cache and call concurrently.
func (j *Tabnas) Util() UtilBag {
	return UtilBag{
		Deep:      Deep,
		Keys:      Keys,
		Values:    Values,
		Entries:   Entries,
		Omap:      Omap,
		Str:       Str,
		StrInject: StrInject,

		Scan:                Scan,
		BuildCharRunSpec:    BuildCharRunSpec,
		BuildLineRunSpec:    BuildLineRunSpec,
		BuildStringBodySpec: BuildStringBodySpec,
	}
}
