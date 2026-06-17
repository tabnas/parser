// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Text wraps a string value with its source quoting, used when the TextInfo option is enabled.
type Text struct {
	Quote string // Source quote char (`"`, `'`, or backtick); empty for unquoted text.
	Str   string // String value, with escapes processed for quoted strings.
}

// ListRef wraps a list value with creation metadata, used when the ListRef option is enabled.
type ListRef struct {
	Val      []any          // List contents.
	Implicit bool           // True if created implicitly (no brackets), false if brackets were explicit.
	Child    any            // Child value from bare colon syntax (:value); merged if multiple, nil if absent.
	Meta     map[string]any // Custom-parser scratch map, initialized when created in the BO (before-open) phase.
}

// MapRef wraps a map value with creation metadata, used when the MapRef option is enabled.
type MapRef struct {
	Val      map[string]any // Map contents.
	Implicit bool           // True if created implicitly (no braces), false if braces were explicit.
	Meta     map[string]any // Custom-parser scratch map, initialized when created in the BO (before-open) phase.
}

// NodeMapSet assigns val under key into node — either a MapRef wrapper
// (info mode) or a plain map[string]any — and returns the node. Promoted
// from @tabnas/json so the info-aware native-value builders (@setval$)
// operate on the engine's own value model. The map contents are a
// reference type, so the assignment is visible through any copy of the
// MapRef value.
func NodeMapSet(node any, key string, val any) any {
	if mr, ok := node.(MapRef); ok {
		mr.Val[key] = val
		return mr
	}
	if m, ok := node.(map[string]any); ok {
		m[key] = val
		return m
	}
	return node
}

// NodeListAppend appends val to node — either a ListRef wrapper (info
// mode) or a plain []any — and returns the (possibly reallocated) node.
// Promoted from @tabnas/json. Go slices are value types, so the caller
// must use the returned node (and re-publish it to the parent rule).
func NodeListAppend(node any, val any) any {
	if lr, ok := node.(ListRef); ok {
		lr.Val = append(lr.Val, val)
		return lr
	}
	if s, ok := node.([]any); ok {
		return append(s, val)
	}
	return node
}
