// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Deep recursively merges values (maps, slices, structs via reflection); zero/nil overlays preserve base.
// Deep(map[string]any{"a":1}, map[string]any{"b":2}) // => {"a":1,"b":2}
func Deep(base any, rest ...any) any {
	for _, over := range rest {
		base = deepMerge(base, over)
	}
	return base
}

// isDangerousMergeKey reports whether an overlay key must never be merged,
// because in a JS runtime it reaches the prototype chain (prototype
// pollution). Go maps have no prototype, so a `__proto__` key decoded from
// JSON is just an ordinary string key here — but the merge is a port of TS
// deep(), which skips these, so it is dropped identically for parity and
// defense. Mirrors the guard in TS utility.ts deep() (and prop()).
// isDangerousMergeKey("__proto__") // => true
func isDangerousMergeKey(k string) bool {
	return "__proto__" == k || "constructor" == k || "prototype" == k
}

// deepMerge merges a single overlay onto base, the recursive core of Deep.
// deepMerge(map[string]any{"a":1}, map[string]any{"a":2}) // => {"a":2}
func deepMerge(base, over any) any {
	// Match TS: undefined and Skip preserve base.
	if IsUndefined(over) || IsSkip(over) {
		return base
	}

	// Extract maps from MapRef if present. A declared map type (`type
	// Field map[string]any`) is read the same way as a plain one: TS
	// deep() sees only a plain object, so a merge here that recognised
	// three concrete types and let every other map kind REPLACE the
	// base dropped defaults the canonical keeps (#198).
	baseMap, baseIsMap := stringKeyedMap(base)
	baseMR, baseIsMR := base.(MapRef)
	if baseIsMR {
		baseMap = baseMR.Val
		baseIsMap = true
	}
	overMap, overIsMap := stringKeyedMap(over)
	overMR, overIsMR := over.(MapRef)
	if overIsMR {
		overMap = overMR.Val
		overIsMap = true
	}

	// Extract arrays from ListRef if present.
	baseArr, baseIsArr := base.([]any)
	baseLR, baseIsLR := base.(ListRef)
	if baseIsLR {
		baseArr = baseLR.Val
		baseIsArr = true
	}
	overArr, overIsArr := over.([]any)
	overLR, overIsLR := over.(ListRef)
	if overIsLR {
		overArr = overLR.Val
		overIsArr = true
	}

	// Ordered-map merge: when either side is an OrderedMap, keep key order
	// (base keys in base order, then new over keys in over order) instead
	// of dropping into an unordered map.
	_, baseIsOM := base.(*OrderedMap)
	_, overIsOM := over.(*OrderedMap)
	if baseIsOM || overIsOM {
		if bKeys, bVals, bok := mapish(base); bok {
			if oKeys, oVals, ook := mapish(over); ook {
				// If the overlay is a Sorted map the result is sorted too, so
				// build a sorted result up front — Set then inserts every key in
				// sorted position (a post-hoc Sorted flag would leave Keys in
				// insertion order while claiming to be sorted).
				result := NewOrderedMap()
				if om, ok := over.(*OrderedMap); ok && om.Sorted {
					result = NewSortedMap()
				}
				for _, k := range bKeys {
					result.Set(k, bVals[k])
				}
				for _, k := range oKeys {
					// Prototype-pollution guard (see isDangerousMergeKey).
					if isDangerousMergeKey(k) {
						continue
					}
					if existing, ok := result.Get(k); ok {
						result.Set(k, deepMerge(existing, oVals[k]))
					} else {
						result.Set(k, deepClone(oVals[k]))
					}
				}
				return result
			}
		}
	}

	if baseIsMap && overIsMap {
		// Both maps: recursively merge
		result := make(map[string]any)
		for k, v := range baseMap {
			result[k] = v
		}
		for k, v := range overMap {
			// Prototype-pollution guard (see isDangerousMergeKey).
			if isDangerousMergeKey(k) {
				continue
			}
			if existing, ok := result[k]; ok {
				result[k] = deepMerge(existing, v)
			} else {
				result[k] = deepClone(v)
			}
		}
		// Preserve MapRef wrapper if the over value was a MapRef.
		if overIsMR {
			meta := mergeMeta(baseMR.Meta, overMR.Meta)
			return MapRef{Val: result, Implicit: overMR.Implicit, Meta: meta}
		}
		return result
	}

	if baseIsArr && overIsArr {
		// Both arrays: recursively merge elements at same index
		maxLen := len(baseArr)
		if len(overArr) > maxLen {
			maxLen = len(overArr)
		}
		result := make([]any, maxLen)
		for i := 0; i < maxLen; i++ {
			if i < len(baseArr) && i < len(overArr) {
				result[i] = deepMerge(deepClone(baseArr[i]), overArr[i])
			} else if i < len(overArr) {
				result[i] = deepClone(overArr[i])
			} else if i < len(baseArr) {
				result[i] = deepClone(baseArr[i])
			}
		}
		// Preserve ListRef wrapper if the over value was a ListRef.
		if overIsLR {
			// Merge Child fields if both are ListRef.
			var child any
			if baseIsLR && baseLR.Child != nil && overLR.Child != nil {
				child = deepMerge(baseLR.Child, overLR.Child)
			} else if overLR.Child != nil {
				child = deepClone(overLR.Child)
			} else if baseIsLR {
				child = deepClone(baseLR.Child)
			}
			meta := mergeMeta(baseLR.Meta, overLR.Meta)
			return ListRef{Val: result, Implicit: overLR.Implicit, Child: child, Meta: meta}
		}
		return result
	}

	// Any other slice kind, both sides the same type: index-wise, as TS
	// deep() merges arrays. Every index the overlay reaches wins, the
	// positions beyond its length keep the base (#151).
	if merged, ok := deepMergeSlices(base, over); ok {
		return merged
	}

	// Struct handling via reflection — matches TS deep() on plain objects.
	if merged, ok := deepMergeStruct(base, over); ok {
		return merged
	}

	// Type mismatch or non-container: over wins
	// Match TS: undefined preserves base, null replaces.
	if IsUndefined(over) {
		return base
	}
	if over == nil {
		return nil
	}
	return deepClone(over)
}

// deepMergeStruct merges two same-type struct values field-by-field via reflection (zero over fields preserve base).
// Returns (merged, true) when both are structs of the same type, else (nil, false).
// deepMergeStruct(Options{Tag:"a"}, Options{}) // => (Options{Tag:"a"}, true)
func deepMergeStruct(base, over any) (any, bool) {
	if base == nil || over == nil {
		return nil, false
	}

	bv := reflect.ValueOf(base)
	ov := reflect.ValueOf(over)

	// Unwrap pointers.
	bIsPtr := bv.Kind() == reflect.Ptr
	oIsPtr := ov.Kind() == reflect.Ptr

	if bIsPtr && bv.IsNil() {
		if oIsPtr || ov.Kind() == reflect.Struct {
			return over, true
		}
		return nil, false
	}
	if oIsPtr && ov.IsNil() {
		if bIsPtr || bv.Kind() == reflect.Struct {
			return base, true
		}
		return nil, false
	}

	bElem := bv
	oElem := ov
	if bIsPtr {
		bElem = bv.Elem()
	}
	if oIsPtr {
		oElem = ov.Elem()
	}

	if bElem.Kind() != reflect.Struct || oElem.Kind() != reflect.Struct {
		return nil, false
	}
	if bElem.Type() != oElem.Type() {
		return nil, false
	}

	// A struct with no exported fields is opaque: the field-by-field
	// merge below can copy nothing out of it (an unexported field is
	// skipped, and reflect could not set it anyway), so it would hand
	// back a freshly allocated zero value. For *regexp.Regexp that is a
	// regexp whose pattern is "" — the merge silently destroyed both
	// operands. Such a value replaces the base outright, matching the TS
	// runtime, where deep() treats a class instance on the `over` side
	// as opaque and lets it win.
	if !hasExportedField(bElem.Type()) {
		return over, true
	}

	result := reflect.New(bElem.Type()).Elem()
	for i := 0; i < bElem.NumField(); i++ {
		bf := bElem.Field(i)
		of := oElem.Field(i)

		if !bf.CanInterface() {
			// Unexported field: keep base.
			continue
		}

		if of.IsZero() {
			result.Field(i).Set(bf)
			continue
		}
		if bf.IsZero() {
			result.Field(i).Set(of)
			continue
		}

		// Both non-zero: merge based on kind.
		switch of.Kind() {
		case reflect.Ptr:
			if of.Elem().Kind() == reflect.Struct {
				// Pointer to struct: recurse.
				merged, ok := deepMergeStruct(bf.Interface(), of.Interface())
				if ok {
					result.Field(i).Set(reflect.ValueOf(merged))
				} else {
					result.Field(i).Set(of)
				}
			} else {
				// Pointer to primitive (*bool, *int): over wins.
				result.Field(i).Set(of)
			}
		case reflect.Map:
			// Merge map entries, and RECURSE into an entry both sides
			// carry, as TS deep() does for `comment.def`, `value.def`
			// and `match.value`: an overlay `{hash: {start: "%"}}` keeps
			// the default hash definition's other fields, and keeps the
			// other definitions. A nil overlay entry is the delete
			// marker and replaces (#151).
			merged := reflect.MakeMap(bf.Type())
			for _, k := range bf.MapKeys() {
				merged.SetMapIndex(k, bf.MapIndex(k))
			}
			for _, k := range of.MapKeys() {
				ov := of.MapIndex(k)
				bv := bf.MapIndex(k)
				if !bv.IsValid() || isNilValue(ov) {
					merged.SetMapIndex(k, ov)
					continue
				}
				m := deepMerge(bv.Interface(), ov.Interface())
				if m == nil {
					merged.SetMapIndex(k, reflect.Zero(bf.Type().Elem()))
				} else {
					merged.SetMapIndex(k, reflect.ValueOf(m))
				}
			}
			result.Field(i).Set(merged)
		case reflect.Slice:
			// Index-wise, as TS deep() merges arrays: `tokenSet.IGNORE:
			// ["#SP"]` keeps the default set's other two entries (#151).
			merged, _ := deepMergeSlices(bf.Interface(), of.Interface())
			result.Field(i).Set(reflect.ValueOf(merged))
		default:
			// String, func, etc.: over wins.
			result.Field(i).Set(of)
		}
	}

	// Return with same pointer wrapping as inputs.
	if bIsPtr || oIsPtr {
		ptr := reflect.New(result.Type())
		ptr.Elem().Set(result)
		return ptr.Interface(), true
	}
	return result.Interface(), true
}

// hasExportedField reports whether struct type t declares at least one
// exported field, i.e. whether a field-by-field merge of two t values
// can copy anything at all.
func hasExportedField(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

// deepMergeSlices merges two slices of one type index by index, the way
// TS deep() merges arrays and the []any branch of deepMerge already did:
// every index the overlay reaches wins (its element merged onto the
// base element when both are containers), and the positions beyond the
// overlay's length keep the base. Go cannot spell TS's `undefined`
// inside a typed slice, so there is no "keep this index" element: an
// empty string in a []string is a present value and wins, which is what
// makes it the serialized `null` marker for tokenSet (a name that
// removes its position). Returns (nil, false) when the two are not
// slices of the same type.
func deepMergeSlices(base, over any) (any, bool) {
	bv, ov := reflect.ValueOf(base), reflect.ValueOf(over)
	if bv.Kind() != reflect.Slice || ov.Kind() != reflect.Slice || bv.Type() != ov.Type() {
		return nil, false
	}
	if bv.IsNil() {
		return deepClone(over), true
	}
	n := bv.Len()
	if ov.Len() > n {
		n = ov.Len()
	}
	result := reflect.MakeSlice(bv.Type(), n, n)
	for i := 0; i < n; i++ {
		switch {
		case i < bv.Len() && i < ov.Len():
			m := deepMerge(bv.Index(i).Interface(), ov.Index(i).Interface())
			if m != nil {
				result.Index(i).Set(reflect.ValueOf(m))
			}
		case i < ov.Len():
			result.Index(i).Set(cloneElem(ov.Index(i), map[uintptr]any{}))
		default:
			result.Index(i).Set(cloneElem(bv.Index(i), map[uintptr]any{}))
		}
	}
	return result.Interface(), true
}

// isNilValue reports a nil pointer, interface, map, slice or func held
// in a reflect.Value: the delete marker in an options map.
func isNilValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return v.IsNil()
	}
	return false
}

// cloneMeta returns a shallow copy of a Meta map (nil stays nil).
// cloneMeta(map[string]any{"a":1}) // => {"a":1}
func cloneMeta(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	result := make(map[string]any, len(meta))
	for k, v := range meta {
		result[k] = v
	}
	return result
}

// mergeMeta merges two Meta maps; over values win on key collision.
// mergeMeta(map[string]any{"a":1}, map[string]any{"a":2}) // => {"a":2}
func mergeMeta(base, over map[string]any) map[string]any {
	if base == nil && over == nil {
		return nil
	}
	result := make(map[string]any)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range over {
		result[k] = v
	}
	return result
}

// mapish returns an ordered key list and the underlying value map for any
// object-shaped value — an OrderedMap (its Keys/Vals), a plain
// map[string]any, or a MapRef (its Val). Plain maps and MapRefs have no
// recorded order, so their key list is in Go map-iteration order. The bool
// reports whether v was object-shaped.
func mapish(v any) (keys []string, vals map[string]any, ok bool) {
	switch m := v.(type) {
	case *OrderedMap:
		return m.Keys, m.Vals, true
	case MapRef:
		ks := make([]string, 0, len(m.Val))
		for k := range m.Val {
			ks = append(ks, k)
		}
		return ks, m.Val, true
	}
	if m, ok := stringKeyedMap(v); ok {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		return ks, m, true
	}
	return nil, nil, false
}

// stringKeyedMap reads any map KIND whose key type is string as a
// map[string]any: a plain map[string]any as itself, and a declared map
// type (`type Field map[string]any`, `map[string]int`) through a copy
// made by reflection. Go's type switch matches concrete types, so the
// declared form fell through every map branch of the merge and the
// clone, where the canonical runtime sees nothing but a plain object.
// A nil map is a map here too; MapRef and *OrderedMap are structs, not
// maps, and are handled by their own branches.
func stringKeyedMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out, true
}

// deepClone returns a recursive copy of a value (maps, slices, ListRef,
// MapRef, and any declared map or slice kind); other types are returned
// as-is.
//
// A cyclic value is cloned as a cyclic value, never walked forever: a
// container is cloned once and every later reference to it inside the
// same value resolves to that one clone. Without that memo a
// self-referencing option value took the PROCESS down with a fatal
// stack overflow inside the option merge, before any plugin ran, where
// the canonical runtime throws a RangeError a caller can catch (#197).
// The clone is what TS deep() hands out for an acyclic value; for a
// cyclic one the caller now at least gets to decide what to do with it.
//
// Declared kinds keep their type: `type Ring []any` clones to a Ring.
// deepClone([]any{1, 2}) // => [1 2] (new slice)
func deepClone(val any) any {
	return cloneWithMemo(val, map[uintptr]any{})
}

// cloneWithMemo is deepClone's recursion, carrying the seen-set. Maps
// and slices are keyed by the address of their backing store, which is
// what makes two references to one container resolve to one clone.
func cloneWithMemo(val any, memo map[uintptr]any) any {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case *OrderedMap:
		key := reflect.ValueOf(v).Pointer()
		if seen, ok := memo[key]; ok {
			return seen
		}
		// Prototype-pollution guard: cloning is TS deep()'s copy path too,
		// where the same key filter runs, so a dangerous key nested under a
		// freshly-added key is dropped here rather than copied through. See
		// isDangerousMergeKey. Iterating Keys keeps the copy ordered.
		result := &OrderedMap{
			Keys:   make([]string, 0, len(v.Keys)),
			Vals:   make(map[string]any, len(v.Vals)),
			Sorted: v.Sorted,
		}
		memo[key] = result
		for _, k := range v.Keys {
			if isDangerousMergeKey(k) {
				continue
			}
			result.Keys = append(result.Keys, k)
			result.Vals[k] = cloneWithMemo(v.Vals[k], memo)
		}
		return result
	case ListRef:
		result := make([]any, len(v.Val))
		for i, val := range v.Val {
			result[i] = cloneWithMemo(val, memo)
		}
		return ListRef{Val: result, Implicit: v.Implicit, Child: cloneWithMemo(v.Child, memo), Meta: cloneMeta(v.Meta)}
	case MapRef:
		result := make(map[string]any)
		for k, val := range v.Val {
			// Prototype-pollution guard (see isDangerousMergeKey).
			if isDangerousMergeKey(k) {
				continue
			}
			result[k] = cloneWithMemo(val, memo)
		}
		return MapRef{Val: result, Implicit: v.Implicit, Meta: cloneMeta(v.Meta)}
	}

	// Every other map or slice kind, plain or declared, by reflection.
	// The result keeps the value's own type, so a caller that declared
	// `type Ring []any` gets a Ring back.
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Map:
		if rv.IsNil() {
			return val
		}
		key := rv.Pointer()
		if seen, ok := memo[key]; ok {
			return seen
		}
		stringKeys := rv.Type().Key().Kind() == reflect.String
		result := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		memo[key] = result.Interface()
		iter := rv.MapRange()
		for iter.Next() {
			// Prototype-pollution guard (see isDangerousMergeKey).
			if stringKeys && isDangerousMergeKey(iter.Key().String()) {
				continue
			}
			result.SetMapIndex(iter.Key(), cloneElem(iter.Value(), memo))
		}
		return result.Interface()
	case reflect.Slice:
		if rv.IsNil() {
			return val
		}
		// A slice's identity for the memo is its backing array plus its
		// length: two slices over one array are distinct values, but the
		// self-reference that matters (a[0] = a) shares both.
		key := rv.Pointer() ^ uintptr(rv.Len())<<48
		if seen, ok := memo[key]; ok {
			return seen
		}
		result := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		memo[key] = result.Interface()
		for i := 0; i < rv.Len(); i++ {
			result.Index(i).Set(cloneElem(rv.Index(i), memo))
		}
		return result.Interface()
	}
	return val
}

// cloneElem clones one map entry or slice element, keeping the static
// element type of the container it came from: an `any` element is
// cloned through the type switch above, a concrete one (a string in a
// []string, a *CommentDef in its map) is copied as it is.
func cloneElem(ev reflect.Value, memo map[uintptr]any) reflect.Value {
	if ev.Kind() != reflect.Interface {
		if ev.Kind() == reflect.Map || ev.Kind() == reflect.Slice {
			return reflect.ValueOf(cloneWithMemo(ev.Interface(), memo))
		}
		return ev
	}
	if ev.IsNil() {
		return ev
	}
	cloned := cloneWithMemo(ev.Interface(), memo)
	if cloned == nil {
		return reflect.Zero(ev.Type())
	}
	return reflect.ValueOf(cloned)
}

// Snip truncates s to maxlen bytes and replaces \r, \n, \t with '.' (for debug/display output).
// Snip("a\nbcd", 3) // => "a.b"
func Snip(s string, maxlen int) string {
	if maxlen <= 0 {
		return ""
	}
	if len(s) > maxlen {
		s = s[:maxlen]
	}
	return strings.NewReplacer("\r", ".", "\n", ".", "\t", ".").Replace(s)
}

// Str renders a value to a string, truncating to maxlen ("..." marks truncation) then snipping \r\n\t.
// Returns "" when maxlen <= 0. Mirrors the TypeScript str() + snip() pipeline.
// Str("hello", 4) // => "h..."
func Str(val any, maxlen int) string {
	if maxlen <= 0 {
		return ""
	}

	var s string
	switch v := val.(type) {
	case string:
		s = v
	case float64:
		if v == float64(int64(v)) {
			s = strconv.FormatInt(int64(v), 10)
		} else {
			s = strconv.FormatFloat(v, 'f', -1, 64)
		}
	case bool:
		if v {
			s = "true"
		} else {
			s = "false"
		}
	case nil:
		// Match TS: JSON.stringify(null) === "null"
		s = "null"
	default:
		b, err := json.Marshal(val)
		if err != nil {
			s = fmt.Sprintf("%v", val)
		} else {
			s = string(b)
		}
	}

	if len(s) > maxlen {
		if maxlen >= 4 {
			s = s[:maxlen-3] + "..."
		} else {
			s = "..."[:maxlen]
		}
	}

	// Match TS: str() calls snip() which replaces \r\n\t with '.'
	return Snip(s, maxlen)
}

// StrInject substitutes {key} / {key.subkey} placeholders in template from a map or array; unknown vals returns template unchanged.
// StrInject("hi {n}", map[string]any{"n":"x"}) // => "hi x"
func StrInject(template string, vals any) string {
	if template == "" {
		return ""
	}

	valsMap, isMap := AsStringMap(vals) // handles *OrderedMap and plain map
	valsArr, isArr := vals.([]any)

	if !isMap && !isArr {
		return template
	}

	var result strings.Builder
	runes := []rune(template)
	i := 0
	for i < len(runes) {
		if runes[i] == '{' {
			// Find closing brace
			j := i + 1
			for j < len(runes) && runes[j] != '}' {
				j++
			}
			if j < len(runes) {
				path := string(runes[i+1 : j])
				// Resolve path
				resolved, ok := resolvePath(path, valsMap, valsArr, isMap)
				if ok {
					result.WriteString(formatInjectValue(resolved))
				} else {
					// Keep original placeholder
					result.WriteString(string(runes[i : j+1]))
				}
				i = j + 1
			} else {
				result.WriteRune(runes[i])
				i++
			}
		} else {
			result.WriteRune(runes[i])
			i++
		}
	}
	return result.String()
}

// resolvePath walks a dotted path like "a.b.0" through nested maps/arrays, returning (value, ok).
// resolvePath("a", map[string]any{"a":1}, nil, true) // => (1, true)
func resolvePath(path string, valsMap map[string]any, valsArr []any, isMap bool) (any, bool) {
	parts := strings.Split(path, ".")
	var current any
	if isMap {
		current = any(valsMap)
	} else {
		current = any(valsArr)
	}

	for _, part := range parts {
		switch c := current.(type) {
		case *OrderedMap:
			v, ok := c.Get(part)
			if !ok {
				return nil, false
			}
			current = v
		case map[string]any:
			v, ok := c[part]
			if !ok {
				return nil, false
			}
			current = v
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(c) {
				return nil, false
			}
			current = c[idx]
		default:
			return nil, false
		}
	}
	return current, true
}

// formatInjectValue renders a resolved value for placeholder substitution (maps/arrays use compact form).
// formatInjectValue(float64(2)) // => "2"
func formatInjectValue(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	case map[string]any:
		return formatCompactValue(v)
	case []any:
		return formatCompactValue(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatCompactValue renders maps/arrays in compact unquoted form ({key:value}) like tabnas output.
// formatCompactValue(map[string]any{"k":"v"}) // => "{k:v}"
func formatCompactValue(val any) string {
	switch v := val.(type) {
	case map[string]any:
		var sb strings.Builder
		sb.WriteRune('{')
		first := true
		for k, v := range v {
			if !first {
				sb.WriteRune(',')
			}
			first = false
			sb.WriteString(k)
			sb.WriteRune(':')
			sb.WriteString(formatCompactValue(v))
		}
		sb.WriteRune('}')
		return sb.String()
	case []any:
		var sb strings.Builder
		sb.WriteRune('[')
		for i, v := range v {
			if i > 0 {
				sb.WriteRune(',')
			}
			sb.WriteString(formatCompactValue(v))
		}
		sb.WriteRune(']')
		return sb.String()
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// List modification options for ModList (mirrors the TypeScript ListMods type).
type ModListOpts struct {
	Delete []int                  // Indices to delete (supports negative indices).
	Move   []int                  // Move pairs: [from, to, from, to, ...].
	Custom func(list []any) []any // Custom modification callback, applied last.
}

// ModList applies delete, then move, then custom operations to a list (mirrors the TypeScript modlist()).
// ModList([]any{"a","b"}, &ModListOpts{Delete: []int{0}}) // => ["b"]
func ModList(list []any, opts *ModListOpts) []any {
	if opts == nil || list == nil {
		return list
	}

	if len(list) > 0 {
		type sentinel struct{}
		deleteMarker := sentinel{}

		// Phase 1: Mark elements for deletion (before move so indexes still make sense).
		if len(opts.Delete) > 0 {
			for _, idx := range opts.Delete {
				n := len(list)
				if idx < 0 {
					if -idx <= n {
						dI := (n + idx) % n
						list[dI] = deleteMarker
					}
				} else {
					if idx < n {
						list[idx] = deleteMarker
					}
				}
			}
		}

		// Phase 2: Move operations (on array with markers still present).
		if len(opts.Move) >= 2 {
			for i := 0; i+1 < len(opts.Move); i += 2 {
				n := len(list)
				if n == 0 {
					break
				}
				fromI := ((opts.Move[i] % n) + n) % n
				toI := ((opts.Move[i+1] % n) + n) % n
				entry := list[fromI]
				list = append(list[:fromI], list[fromI+1:]...)
				newList := make([]any, len(list)+1)
				copy(newList, list[:toI])
				newList[toI] = entry
				copy(newList[toI+1:], list[toI:])
				list = newList
			}
		}

		// Phase 3: Filter out deleted entries.
		if len(opts.Delete) > 0 {
			filtered := make([]any, 0, len(list))
			for _, v := range list {
				if _, ok := v.(sentinel); !ok {
					filtered = append(filtered, v)
				}
			}
			list = filtered
		}
	}

	// Phase 4: Custom modification (matches TS mods.custom).
	if opts.Custom != nil {
		if newList := opts.Custom(list); newList != nil {
			list = newList
		}
	}

	return list
}

// IsFuncRef reports whether a string is a function reference (starts with "@").
// IsFuncRef("@foo") // => true;  IsFuncRef("foo") // => false
func IsFuncRef(s string) bool {
	return len(s) > 0 && s[0] == '@'
}

// RequireRef looks up a FuncRef by name, returning a grammar error when absent (kind labels the error).
// RequireRef(map[FuncRef]any{"@f":fn}, "@f", "val") // => (fn, nil)
func RequireRef(ref map[FuncRef]any, name string, kind string) (any, error) {
	if ref == nil {
		return nil, fmt.Errorf("Grammar: unknown %s function reference: %s (no ref map)", kind, name)
	}
	fn, ok := ref[name]
	if !ok {
		return nil, fmt.Errorf("Grammar: unknown %s function reference: %s", kind, name)
	}
	return fn, nil
}

// LookupRef returns the FuncRef value for name, or nil when absent.
// LookupRef(map[FuncRef]any{"@f":fn}, "@x") // => nil
func LookupRef(ref map[FuncRef]any, name string) any {
	if ref == nil {
		return nil
	}
	return ref[name]
}

// MapToOptions builds an Options struct from a map[string]any (FuncRefs already resolved), covering common grammar option fields.
// MapToOptions(map[string]any{"tag":"x"}) // => Options{Tag:"x"}
// mapInt reads a numeric option out of an untyped options map. JSON
// decoding produces float64; a map built in Go may hold any integer
// kind. Truncates toward zero.
func mapInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	}
	return 0, false
}

func MapToOptions(m map[string]any) Options {
	opts, _ := OptionsFromMap(m)
	return opts
}

// OptionsFromMap builds an Options struct from a map[string]any whose
// FuncRefs have already been resolved, and reports the entries it cannot
// carry rather than dropping or panicking on them.
//
// This is the checked form of MapToOptions. It exists because
// MapToOptions signalled a serialized regex that RE2 could not compile
// by PANICKING out to its caller, which Grammar() then recovered and
// mislabelled as an internal error (#119). A caller's unsupported
// regex is a user error and is reported as one here; MapToOptions keeps
// its signature and skips the entry, so prefer this function wherever
// there is an error to return.
//
// Coverage is the other half of the contract (#130): every pure-data
// leaf of Options is read here, and TestOptionsFromMapCoversEveryLeaf
// walks the struct by reflection to keep it that way, so a field added
// to Options cannot fall silently out of the serialized surface again.
// Function-valued leaves are read when the map carries a function of the
// right type (a resolved ref); a spec carrying only JSON cannot reach
// them, which is what makes them unportable rather than unread.
func OptionsFromMap(m map[string]any) (Options, error) {
	var opts Options
	var errs []string
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}
	// Type the door first (#143): an ill-typed leaf is reported, never
	// dropped, and a function reference outside a declared code slot is
	// one such leaf. The reads below then only see values of the right
	// shape, or nothing.
	if err := validateOptionsMap(m); err != nil {
		fail("%s", strings.TrimPrefix(err.Error(), "tabnas: options: "))
	}

	if v, ok := m["tag"].(string); ok {
		opts.Tag = v
	}

	// safe
	if safe, ok := m["safe"].(map[string]any); ok {
		opts.Safe = &SafeOptions{}
		if key, ok := safe["key"].(bool); ok {
			opts.Safe.Key = &key
		}
	}

	// fixed
	if fm, ok := m["fixed"].(map[string]any); ok {
		opts.Fixed = &FixedOptions{}
		if fn, ok := lexCheckOf(fm["check"]); ok {
			opts.Fixed.Check = fn
		}
		if lex, ok := fm["lex"].(bool); ok {
			opts.Fixed.Lex = &lex
		}
		// fixed.token: name → src (e.g. {"#T": "@"}). A serialized grammar
		// declares its fixed punctuation here; applying it registers the
		// token so the lexer recognizes it (a nil value removes a mapping).
		if tok, ok := fm["token"].(map[string]any); ok {
			opts.Fixed.Token = make(map[string]*string, len(tok))
			for name, v := range tok {
				if s, ok := v.(string); ok {
					sv := s
					opts.Fixed.Token[name] = &sv
				} else if v == nil {
					opts.Fixed.Token[name] = nil
				}
			}
		}
	}

	// space
	if sp, ok := m["space"].(map[string]any); ok {
		opts.Space = &SpaceOptions{}
		if fn, ok := lexCheckOf(sp["check"]); ok {
			opts.Space.Check = fn
		}
		if lex, ok := sp["lex"].(bool); ok {
			opts.Space.Lex = &lex
		}
		if chars, ok := sp["chars"].(string); ok {
			opts.Space.Chars = chars
		}
	}

	// line
	if ln, ok := m["line"].(map[string]any); ok {
		opts.Line = &LineOptions{}
		if fn, ok := lexCheckOf(ln["check"]); ok {
			opts.Line.Check = fn
		}
		if lex, ok := ln["lex"].(bool); ok {
			opts.Line.Lex = &lex
		}
		if chars, ok := ln["chars"].(string); ok {
			opts.Line.Chars = chars
		}
		if rowChars, ok := ln["rowChars"].(string); ok {
			opts.Line.RowChars = rowChars
		}
		if single, ok := ln["single"].(bool); ok {
			opts.Line.Single = &single
		}
	}

	// text
	if tm, ok := m["text"].(map[string]any); ok {
		opts.Text = &TextOptions{}
		if mods, ok := tm["modify"].([]any); ok {
			for _, v := range mods {
				switch fn := v.(type) {
				case ValModifier:
					opts.Text.Modify = append(opts.Text.Modify, fn)
				case func(val any) any:
					opts.Text.Modify = append(opts.Text.Modify, fn)
				}
			}
		}
		if fn, ok := lexCheckOf(tm["check"]); ok {
			opts.Text.Check = fn
		}
		if lex, ok := tm["lex"].(bool); ok {
			opts.Text.Lex = &lex
		}
	}

	// number
	if nm, ok := m["number"].(map[string]any); ok {
		opts.Number = &NumberOptions{}
		if fn, ok := lexCheckOf(nm["check"]); ok {
			opts.Number.Check = fn
		}
		if lex, ok := nm["lex"].(bool); ok {
			opts.Number.Lex = &lex
		}
		if hex, ok := nm["hex"].(bool); ok {
			opts.Number.Hex = &hex
		}
		if oct, ok := nm["oct"].(bool); ok {
			opts.Number.Oct = &oct
		}
		if bin, ok := nm["bin"].(bool); ok {
			opts.Number.Bin = &bin
		}
		if sep, ok := nm["sep"].(string); ok {
			opts.Number.Sep = sep
		}
		if fn, ok := nm["exclude"].(func(string) bool); ok {
			opts.Number.Exclude = fn
		} else if re, ok := nm["exclude"].(*regexp.Regexp); ok {
			opts.Number.Exclude = func(s string) bool {
				return re.MatchString(s)
			}
		}
	}

	// comment
	if cm, ok := m["comment"].(map[string]any); ok {
		opts.Comment = &CommentOptions{}
		if fn, ok := lexCheckOf(cm["check"]); ok {
			opts.Comment.Check = fn
		}
		if lex, ok := cm["lex"].(bool); ok {
			opts.Comment.Lex = &lex
		}
		if defm, ok := cm["def"].(map[string]any); ok {
			opts.Comment.Def = make(map[string]*CommentDef, len(defm))
			for k, v := range defm {
				dm, ok := v.(map[string]any)
				if !ok {
					continue
				}
				cd := &CommentDef{}
				if line, ok := dm["line"].(bool); ok {
					cd.Line = line
				}
				if start, ok := dm["start"].(string); ok {
					cd.Start = start
				}
				if end, ok := dm["end"].(string); ok {
					cd.End = end
				}
				if lex, ok := dm["lex"].(bool); ok {
					cd.Lex = &lex
				}
				if eatline, ok := dm["eatline"].(bool); ok {
					cd.EatLine = &eatline
				}
				// Suffix round-trip via text: accept string or array.
				// The LexMatcher form requires the typed Go API.
				if suffix, ok := dm["suffix"]; ok {
					switch v := suffix.(type) {
					case string:
						cd.Suffix = v
					case []any:
						strs := make([]string, 0, len(v))
						for _, el := range v {
							if s, ok := el.(string); ok {
								strs = append(strs, s)
							}
						}
						cd.Suffix = strs
					case []string:
						cd.Suffix = v
					}
				}
				opts.Comment.Def[k] = cd
			}
		}
	}

	// string
	if sm, ok := m["string"].(map[string]any); ok {
		opts.String = &StringOptions{}
		if fn, ok := lexCheckOf(sm["check"]); ok {
			opts.String.Check = fn
		}
		if lex, ok := sm["lex"].(bool); ok {
			opts.String.Lex = &lex
		}
		if chars, ok := sm["chars"].(string); ok {
			opts.String.Chars = chars
		}
		if multiChars, ok := sm["multiChars"].(string); ok {
			opts.String.MultiChars = multiChars
		}
		if escapeChar, ok := sm["escapeChar"].(string); ok {
			opts.String.EscapeChar = escapeChar
		}
		if allowUnknown, ok := sm["allowUnknown"].(bool); ok {
			opts.String.AllowUnknown = &allowUnknown
		}
		if escapeStrict, ok := sm["escapeStrict"].(bool); ok {
			opts.String.EscapeStrict = &escapeStrict
		}
		if allowControl, ok := sm["allowControl"].(bool); ok {
			opts.String.AllowControl = &allowControl
		}
		if abandon, ok := sm["abandon"].(bool); ok {
			opts.String.Abandon = &abandon
		}
		if esc, ok := sm["escape"].(map[string]any); ok {
			opts.String.Escape = make(map[string]string, len(esc))
			for k, v := range esc {
				// A null value removes a built-in escape, matching the TS
				// runtime where `escape: {v: null}` drops \v. Internally the
				// removal marker is "" (see StringOptions.Escape).
				if v == nil {
					opts.String.Escape[k] = ""
					continue
				}
				if s, ok := v.(string); ok {
					opts.String.Escape[k] = s
				}
			}
		}
		if rep, ok := sm["replace"].(map[string]any); ok {
			opts.String.Replace = make(map[rune]string, len(rep))
			for k, v := range rep {
				if len(k) > 0 {
					if s, ok := v.(string); ok {
						opts.String.Replace[rune(k[0])] = s
					}
				}
			}
		}
	}

	// map
	if mm, ok := m["map"].(map[string]any); ok {
		opts.Map = &MapOptions{}
		if ext, ok := mm["extend"].(bool); ok {
			opts.Map.Extend = &ext
		}
		if child, ok := mm["child"].(bool); ok {
			opts.Map.Child = &child
		}
		if plain, ok := mm["plain"].(bool); ok {
			opts.Map.Plain = &plain
		}
		switch fn := mm["merge"].(type) {
		case MapMergeFunc:
			opts.Map.Merge = fn
		case func(any, any, *Rule, *Context) any:
			opts.Map.Merge = fn
		}
	}

	// list
	if lm, ok := m["list"].(map[string]any); ok {
		opts.List = &ListOptions{}
		if prop, ok := lm["property"].(bool); ok {
			opts.List.Property = &prop
		}
		if pair, ok := lm["pair"].(bool); ok {
			opts.List.Pair = &pair
		}
		if child, ok := lm["child"].(bool); ok {
			opts.List.Child = &child
		}
	}

	// value
	if vm, ok := m["value"].(map[string]any); ok {
		opts.Value = &ValueOptions{}
		if lex, ok := vm["lex"].(bool); ok {
			opts.Value.Lex = &lex
		}
		if defm, ok := vm["def"].(map[string]any); ok {
			opts.Value.Def = make(map[string]*ValueDef, len(defm))
			for k, v := range defm {
				switch vv := v.(type) {
				case map[string]any:
					vd := &ValueDef{}
					if val, ok := vv["val"]; ok {
						vd.Val = val
						if fn, ok := val.(func([]string) any); ok {
							vd.ValFunc = fn
						}
					}
					if m, ok := vv["match"].(*regexp.Regexp); ok {
						vd.Match = m
					}
					if c, ok := vv["consume"].(bool); ok {
						vd.Consume = c
					}
					opts.Value.Def[k] = vd
				case nil, bool:
					// nil or false removes the value def
				}
			}
		}
	}

	// ender
	if ender, ok := m["ender"]; ok {
		switch v := ender.(type) {
		case string:
			// A string ender is its CHARACTERS, one ender each, as
			// TypeScript's `opts.ender.split('')` reads it (#201). An
			// ARRAY entry is kept whole and is one ender, so an entry of
			// more than one character is a SEQUENCE (#202) -- which is
			// the whole reason the two forms cannot share a reading.
			for _, r := range v {
				opts.Ender = append(opts.Ender, string(r))
			}
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					opts.Ender = append(opts.Ender, s)
				}
			}
		}
	}

	// rule
	if rm, ok := m["rule"].(map[string]any); ok {
		opts.Rule = &RuleOptions{}
		if start, ok := rm["start"].(string); ok {
			opts.Rule.Start = start
		}
		if finish, ok := rm["finish"].(bool); ok {
			opts.Rule.Finish = &finish
		}
		if include, ok := rm["include"].(string); ok {
			opts.Rule.Include = include
		}
		if exclude, ok := rm["exclude"].(string); ok {
			opts.Rule.Exclude = exclude
		}
		// A JSON number arrives as float64; a hand-built map may carry a
		// real int. Truncated toward zero, so a fractional multiplier —
		// which TypeScript accepts and this port's `*int` cannot hold —
		// becomes 0 and is then coerced to the default at parse time,
		// rather than being dropped without trace. See DIVERGENCE.md,
		// "Rule-iteration budget: a fractional `rule.maxmul`".
		if maxmul, ok := mapInt(rm["maxmul"]); ok {
			opts.Rule.MaxMul = &maxmul
		}
	}

	// lex
	if lx, ok := m["lex"].(map[string]any); ok {
		opts.Lex = &LexOptions{}
		if empty, ok := lx["empty"].(bool); ok {
			opts.Lex.Empty = &empty
		}
		if emptyResult, ok := lx["emptyResult"]; ok {
			opts.Lex.EmptyResult = emptyResult
		}
		if relex, ok := lx["relex"].(bool); ok {
			opts.Lex.Relex = &relex
		}
		if specs, ok := lx["match"].(map[string]any); ok {
			for name, v := range specs {
				sm, ok := v.(map[string]any)
				if !ok {
					continue
				}
				spec := &MatchSpec{}
				if n, ok := mapInt(sm["order"]); ok {
					spec.Order = n
				}
				switch mk := sm["make"].(type) {
				case MakeLexMatcher:
					spec.Make = mk
				case func(cfg *LexConfig, opts *Options) LexMatcher:
					spec.Make = mk
				}
				if spec.Make == nil {
					continue
				}
				if opts.Lex.Match == nil {
					opts.Lex.Match = make(map[string]*MatchSpec)
				}
				opts.Lex.Match[name] = spec
			}
		}
	}

	// error
	if em, ok := m["error"].(map[string]any); ok {
		opts.Error = make(map[string]string, len(em))
		for k, v := range em {
			if s, ok := v.(string); ok {
				opts.Error[k] = s
			}
		}
	}

	// hint
	if hm, ok := m["hint"].(map[string]any); ok {
		opts.Hint = make(map[string]string, len(hm))
		for k, v := range hm {
			if s, ok := v.(string); ok {
				opts.Hint[k] = s
			}
		}
	}

	// errmsg
	if em, ok := m["errmsg"].(map[string]any); ok {
		opts.ErrMsg = &ErrMsgOptions{}
		if name, ok := em["name"].(string); ok {
			opts.ErrMsg.Name = name
		}
		if suffix, ok := em["suffix"]; ok {
			// TS accepts bool | string | function; only the JSON-serialisable
			// subset round-trips through a tabnas text source, so we only
			// pass those along. Functions need the typed API.
			switch v := suffix.(type) {
			case bool, string:
				opts.ErrMsg.Suffix = v
			}
		}
		if link, ok := em["link"].(string); ok {
			opts.ErrMsg.Link = link
		}
	}

	// match
	if mm, ok := m["match"].(map[string]any); ok {
		opts.Match = &MatchOptions{}
		if lex, ok := mm["lex"].(bool); ok {
			opts.Match.Lex = &lex
		}
		if tok, ok := mm["token"].(map[string]any); ok {
			opts.Match.Token = make(map[string]*regexp.Regexp, len(tok))
			for name, v := range tok {
				switch re := v.(type) {
				case *EagerRegexp:
					opts.Match.Token[name] = re.Re
					if opts.Match.TokenEager == nil {
						opts.Match.TokenEager = make(map[string]bool)
					}
					opts.Match.TokenEager[name] = true
				case *regexp.Regexp:
					opts.Match.Token[name] = re
				case string:
					// A leftover @/…/ or @~/…/ string means its regex did
					// not compile (an unsupported RE2 construct). Report it
					// rather than silently drop the token, which would make
					// the lexer mis-recognize input; Grammar() returns it as
					// an install error. Any other non-regex string is
					// ignored, as before.
					if strings.HasPrefix(re, "@/") || strings.HasPrefix(re, "@~/") {
						fail("match token %q regex did not compile (got %q): "+
							"unsupported regex construct for Go RE2", name, re)
					}
				case LexMatcher:
					if opts.Match.TokenFn == nil {
						opts.Match.TokenFn = make(map[string]LexMatcher)
					}
					opts.Match.TokenFn[name] = re
				case func(lex *Lex, rule *Rule) *Token:
					if opts.Match.TokenFn == nil {
						opts.Match.TokenFn = make(map[string]LexMatcher)
					}
					opts.Match.TokenFn[name] = re
				}
			}
		}
		if val, ok := mm["value"].(map[string]any); ok {
			opts.Match.Value = make(map[string]*MatchValueSpec, len(val))
			for name, v := range val {
				switch spec := v.(type) {
				case map[string]any:
					mvs := &MatchValueSpec{}
					if re, ok := spec["match"].(*regexp.Regexp); ok {
						mvs.Match = re
					}
					if fn, ok := spec["val"].(func([]string) any); ok {
						mvs.Val = fn
					}
					if fn, ok := spec["fn"].(LexMatcher); ok {
						mvs.Fn = fn
					} else if fn, ok := spec["fn"].(func(lex *Lex, rule *Rule) *Token); ok {
						mvs.Fn = fn
					}
					opts.Match.Value[name] = mvs
				case LexMatcher:
					opts.Match.Value[name] = &MatchValueSpec{Fn: spec}
				case func(lex *Lex, rule *Rule) *Token:
					opts.Match.Value[name] = &MatchValueSpec{Fn: spec}
				}
			}
		}
		if fn, ok := lexCheckOf(mm["check"]); ok {
			opts.Match.Check = fn
		}
		if order, ok := stringList(mm["tokenOrder"]); ok {
			opts.Match.TokenOrder = order
		}
	}

	// tokenSet. Positions are kept: the merge is index-wise onto the
	// default set, so a JSON `null` at a position (which removes it in
	// TS) becomes the "" marker here, and a shorter array keeps the
	// default set's tail, as it does in TS (#151).
	if ts, ok := m["tokenSet"].(map[string]any); ok {
		opts.TokenSet = make(map[string][]string, len(ts))
		for name, v := range ts {
			switch arr := v.(type) {
			case []any:
				names := make([]string, len(arr))
				for i, item := range arr {
					if s, ok := item.(string); ok {
						names[i] = s
					}
				}
				opts.TokenSet[name] = names
			case []string:
				opts.TokenSet[name] = arr
			}
		}
	}

	// info
	if im, ok := m["info"].(map[string]any); ok {
		opts.Info = &InfoOptions{}
		if v, ok := im["map"].(bool); ok {
			opts.Info.Map = &v
		}
		if v, ok := im["list"].(bool); ok {
			opts.Info.List = &v
		}
		if v, ok := im["text"].(bool); ok {
			opts.Info.Text = &v
		}
		if v, ok := im["marker"].(string); ok {
			opts.Info.Marker = v
		}
	}

	// color
	if cm, ok := m["color"].(map[string]any); ok {
		opts.Color = &ColorOptions{}
		if v, ok := cm["active"].(bool); ok {
			opts.Color.Active = &v
		}
		if v, ok := cm["reset"].(string); ok {
			opts.Color.Reset = v
		}
		if v, ok := cm["hi"].(string); ok {
			opts.Color.Hi = v
		}
		if v, ok := cm["lo"].(string); ok {
			opts.Color.Lo = v
		}
		if v, ok := cm["line"].(string); ok {
			opts.Color.Line = v
		}
	}

	// rewind. The spellings are the cross-runtime contract (#144, #142):
	// absent leaves the merge alone, null is the documented default, false
	// is unbounded (a negative History here), a negative cap is 0.
	if rw, ok := m["rewind"].(map[string]any); ok {
		opts.Rewind = &RewindOptions{}
		if raw, present := rw["history"]; present {
			switch h := raw.(type) {
			case nil:
				d := DefaultRewindHistory
				opts.Rewind.History = &d
			case bool:
				if h {
					fail("rewind.history: true is not a cap; use false for unbounded")
				} else {
					unbounded := -1
					opts.Rewind.History = &unbounded
				}
			default:
				if n, ok := mapInt(raw); ok {
					if n < 0 {
						n = 0
					}
					opts.Rewind.History = &n
				} else {
					fail("rewind.history: expected an integer, null or false, got %T", raw)
				}
			}
		}
	}

	// result
	if rm, ok := m["result"].(map[string]any); ok {
		opts.Result = &ResultOptions{}
		if fail, ok := rm["fail"].([]any); ok {
			opts.Result.Fail = append([]any{}, fail...)
		}
	}

	// parse: the container of budget and recover, which is why dropping
	// it left opt-in recovery unreachable from any serialized spec.
	if pm, ok := m["parse"].(map[string]any); ok {
		opts.Parse = &ParseOptions{}
		if prep, ok := pm["prepare"].(map[string]any); ok {
			for name, v := range prep {
				if fn, ok := v.(func(ctx *Context)); ok {
					if opts.Parse.Prepare == nil {
						opts.Parse.Prepare = make(map[string]func(ctx *Context))
					}
					opts.Parse.Prepare[name] = fn
				}
			}
		}
		if bm, ok := pm["budget"].(map[string]any); ok {
			opts.Parse.Budget = &BudgetOptions{}
			if n, ok := mapInt(bm["checkEveryN"]); ok {
				opts.Parse.Budget.CheckEveryN = n
			}
			if fn, ok := bm["onCheck"].(func(ctx *Context) bool); ok {
				opts.Parse.Budget.OnCheck = fn
			}
		}
		if rm, ok := pm["recover"].(map[string]any); ok {
			opts.Parse.Recover = &RecoverOptions{}
			if enabled, ok := rm["enabled"].(bool); ok {
				opts.Parse.Recover.Enabled = enabled
			}
			if groups, ok := stringList(rm["syncGroups"]); ok {
				opts.Parse.Recover.SyncGroups = groups
			}
			if toks, ok := stringList(rm["syncTokens"]); ok {
				opts.Parse.Recover.SyncTokens = toks
			}
			if b, ok := rm["popUntilValid"].(bool); ok {
				opts.Parse.Recover.PopUntilValid = &b
			}
			if n, ok := mapInt(rm["maxSkip"]); ok {
				opts.Parse.Recover.MaxSkip = &n
			}
			if n, ok := mapInt(rm["maxRecoveries"]); ok {
				opts.Parse.Recover.MaxRecoveries = &n
			}
			if n, ok := mapInt(rm["suppress"]); ok {
				opts.Parse.Recover.Suppress = &n
			}
		}
	}

	// parser: function-valued only, so it reaches here solely through a
	// resolved ref.
	if pm, ok := m["parser"].(map[string]any); ok {
		if fn, ok := pm["start"].(func(src string, j *Tabnas, meta map[string]any) (any, error)); ok {
			opts.Parser = &ParserOptions{Start: fn}
		}
	}

	// property: Go-only, function-valued.
	if pm, ok := m["property"].(map[string]any); ok {
		if mods, ok := pm["configModify"].(map[string]any); ok {
			for name, v := range mods {
				if fn, ok := v.(func(cfg *LexConfig, opts *Options)); ok {
					if opts.Property == nil {
						opts.Property = &PropertyOptions{ConfigModify: map[string]ConfigModifier{}}
					}
					opts.Property.ConfigModify[name] = fn
				} else if fn, ok := v.(ConfigModifier); ok {
					if opts.Property == nil {
						opts.Property = &PropertyOptions{ConfigModify: map[string]ConfigModifier{}}
					}
					opts.Property.ConfigModify[name] = fn
				}
			}
		}
	}

	// An entry under a reserved name is REFUSED, and the reads above
	// converted it anyway. validateOptionsMap reports it and this
	// function returns the error with the partial value, which is its
	// contract -- but MapToOptions discards the error and keeps the
	// value, and is documented to SKIP an entry it cannot carry. Without
	// this the one door that cannot report a refusal was also the one
	// that installed it.
	pruneReservedNames(reflect.ValueOf(&opts))

	if len(errs) > 0 {
		return opts, fmt.Errorf("tabnas: options: %s", strings.Join(errs, "; "))
	}
	return opts, nil
}

// pruneReservedNames deletes every entry under a reserved name from the
// string-keyed maps of a converted Options, walking exported fields and
// following pointers.
//
// The names are refused everywhere rather than per map, because the
// engine declares no option under one of them: deleting one can only
// remove an entry validateOptionsMap has already reported.
func pruneReservedNames(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			pruneReservedNames(v.Elem())
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).IsExported() {
				pruneReservedNames(v.Field(i))
			}
		}
	case reflect.Map:
		if v.IsNil() || v.Type().Key().Kind() != reflect.String {
			return
		}
		for name := range reservedMapKeys {
			v.SetMapIndex(reflect.ValueOf(name), reflect.Value{})
		}
		for _, key := range v.MapKeys() {
			pruneReservedNames(v.MapIndex(key))
		}
	}
}

// lexCheckOf reads a LexCheck hook out of a resolved map value, in either
// the named or the bare function spelling.
func lexCheckOf(v any) (LexCheck, bool) {
	switch fn := v.(type) {
	case LexCheck:
		return fn, fn != nil
	case func(lex *Lex) *LexCheckResult:
		return fn, fn != nil
	}
	return nil, false
}

// stringList reads a []string out of a resolved map value: a JSON array
// (whose non-string entries are skipped) or a Go []string.
func stringList(v any) ([]string, bool) {
	switch arr := v.(type) {
	case []any:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	case []string:
		return append([]string{}, arr...), true
	}
	return nil, false
}

// ResolveFuncRefs recursively rewrites FuncRef strings within nested maps/slices:
//   - "@@prefix" → literal "@prefix"
//   - "@SKIP" → Skip sentinel
//   - "@/pattern/flags" → *regexp.Regexp
//   - "@name" → function from ref map
//
// ResolveFuncRefs("@SKIP", nil) // => Skip
// EagerRegexp wraps a match-token regexp flagged eager: it opts out of
// the lexer's "expected at this rule position" gating, firing whenever
// its pattern matches. Produced by the @~/pattern/flags ref form; carried
// into Options.Match.TokenEager by MapToOptions.
type EagerRegexp struct {
	Re *regexp.Regexp
}

// jsUnicodeEscape matches the JS/TS regex unicode escapes \u{H...} and
// \uHHHH (the form @tabnas/abnf emits for ABNF char classes, e.g.
// `[A-Z]`). Go's regexp (RE2) uses \x{H...} instead.
var jsUnicodeEscape = regexp.MustCompile(`\\u\{([0-9a-fA-F]+)\}|\\u([0-9a-fA-F]{4})`)

// jsRegexToGo rewrites JS/TS unicode escapes (\uHHHH, \u{H...}) in a regex
// pattern to Go RE2 syntax (\x{H...}), so a serialized grammar's regex
// match tokens compile on the Go engine the same way they do in TS. Other
// JS-only constructs (lookahead, backreferences) remain unsupported by RE2
// and will still fail to compile — surfaced loudly by MapToOptions rather
// than silently dropped.
func jsRegexToGo(pattern string) string {
	return jsUnicodeEscape.ReplaceAllStringFunc(pattern, func(m string) string {
		hex := strings.Trim(m[2:], "{}") // m is \uHHHH or \u{H...}
		return `\x{` + hex + `}`
	})
}

// jsRegexFlagsToGo lowers a serialized regex's JS flag string to RE2
// inline flags, reporting false for a flag whose meaning RE2 cannot carry.
//
// The serialized `@/pattern/flags` form is SHARED between the runtimes, so
// it holds JavaScript's flags — the notation TypeScript writes natively.
// Copying them verbatim into an inline `(?flags)` group is wrong: RE2
// rejects most of them outright, and accepts one with a completely
// different meaning.
//
//	i m s   kept — same meaning in both engines.
//	u       DROPPED, because RE2 needs no equivalent: it is natively
//	        rune-based, which is exactly what `u` asks JavaScript to be.
//	        Verified case by case (see go/doc/differences.md): `.` and
//	        `[^\n]` each consume one astral character WHOLE in RE2, as in
//	        JS+u and unlike JS without it, and `.{2}` correspondingly does
//	        NOT accept a single astral character as two.
//	g y d   DROPPED. They govern the JS matcher's statefulness and output
//	        (lastIndex, sticky, match indices), not the language matched,
//	        and the engine calls FindString once per position.
//	v       REJECTED. Unlike `u` it genuinely changes what a class MEANS
//	        (set operations, string literals inside classes), so it is not
//	        a no-op and must not be quietly dropped.
//
// An unknown flag is rejected rather than ignored. Note `U`: RE2 accepts
// it and it means "swap greedy", so passing an unrecognised letter through
// could silently change the language rather than fail.
func jsRegexFlagsToGo(flags string) (string, bool) {
	out := make([]byte, 0, len(flags))
	for i := 0; i < len(flags); i++ {
		switch flags[i] {
		case 'i', 'm', 's':
			out = append(out, flags[i])
		case 'u', 'g', 'y', 'd':
			// No RE2 counterpart is needed; see above.
		default:
			return "", false
		}
	}
	return string(out), true
}

// compileSerializedRegex builds the RE2 form of a serialized `@/…/flags`
// pattern. It returns nil when the pattern or its flags have no RE2
// equivalent, which leaves the original string in place — and a leftover
// `@/…/` string is a loud install error downstream (see MapToOptions),
// not a silently dropped token.
func compileSerializedRegex(pattern, flags string) *regexp.Regexp {
	goFlags, ok := jsRegexFlagsToGo(flags)
	if !ok {
		return nil
	}
	pattern = jsRegexToGo(pattern)
	if goFlags != "" {
		pattern = "(?" + goFlags + ")" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re
}

func ResolveFuncRefs(obj any, ref map[FuncRef]any) any {
	if obj == nil {
		return nil
	}
	if s, ok := obj.(string); ok && len(s) > 0 && s[0] == '@' {
		// Escape: @@ → literal @-prefixed string
		if len(s) > 1 && s[1] == '@' {
			return s[1:]
		}
		// Sentinel: @SKIP → Skip
		if s == "@SKIP" {
			return Skip
		}
		// Regex: @/pattern/flags → *regexp.Regexp
		if len(s) > 2 && s[1] == '/' {
			if idx := strings.LastIndex(s, "/"); idx > 1 {
				if re := compileSerializedRegex(s[2:idx], s[idx+1:]); re != nil {
					return re
				}
			}
		}
		// Eager regex: @~/pattern/flags → *EagerRegexp. The match token
		// opts out of the lexer's "expected at this rule position" gating
		// (e.g. ABNF case-insensitive literals). Disjoint from @/ above
		// (s[1] is '~', not '/').
		if len(s) > 3 && s[1] == '~' && s[2] == '/' {
			if idx := strings.LastIndex(s, "/"); idx > 2 {
				if re := compileSerializedRegex(s[3:idx], s[idx+1:]); re != nil {
					return &EagerRegexp{Re: re}
				}
			}
		}
		// FuncRef: @name → function from ref
		if ref != nil {
			if fn, ok := ref[s]; ok {
				return fn
			}
		}
		return obj
	}

	// Recurse into maps
	if m, ok := obj.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = ResolveFuncRefs(v, ref)
		}
		return out
	}

	// Recurse into slices
	if arr, ok := obj.([]any); ok {
		out := make([]any, len(arr))
		for i, v := range arr {
			out[i] = ResolveFuncRefs(v, ref)
		}
		return out
	}

	return obj
}
