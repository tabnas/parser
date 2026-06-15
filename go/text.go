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
