// Package jsonic provides the relaxed-JSON grammar for the tabnas
// engine — the original jsonic use case: lenient JSON for humans.
// Unquoted keys, implicit objects/arrays, comments, trailing commas,
// single/backtick quotes, multiline strings, and path diving.
//
// The engine package (github.com/tabnas/parser/go) ships no grammar,
// matching the canonical TypeScript package; this package supplies the
// grammar as a plugin plus convenience constructors:
//
//	result, err := jsonic.Parse("a:1, b:2")
//
//	j := jsonic.Make(tabnas.Options{...})
//	result, err := j.Parse("a:1")
package jsonic

import (
	tabnas "github.com/tabnas/parser/go"
)

func init() {
	// Make the engine's text-form convenience APIs (SetOptionsText,
	// GrammarText) work whenever this grammar package is linked in.
	tabnas.RegisterTextParser(Parse)
}

// Plugin installs the relaxed-JSON grammar on a Tabnas instance.
// Standard tabnas plugin signature, usable with (*Tabnas).Use:
//
//	j := tabnas.Make()
//	_ = j.Use(jsonic.Plugin)
func Plugin(j *tabnas.Tabnas, opts map[string]any) error {
	buildGrammar(j.RSM(), j.Config())
	return nil
}

// Make creates a Tabnas instance with the relaxed-JSON grammar
// installed. Options are applied as in tabnas.Make; rule include /
// exclude group filters are applied after the grammar is installed so
// they operate on the grammar's group tags ("json", "tabnas").
func Make(opts ...tabnas.Options) *tabnas.Tabnas {
	var o tabnas.Options
	if len(opts) > 0 {
		o = opts[0]
	}

	// Defer rule include/exclude until the grammar rules exist.
	var include, exclude string
	if o.Rule != nil {
		include, exclude = o.Rule.Include, o.Rule.Exclude
		rule := *o.Rule
		rule.Include, rule.Exclude = "", ""
		o.Rule = &rule
	}

	j := tabnas.Make(o)
	// Plugin is infallible (always returns nil): buildGrammar has no
	// failure modes, so the Use error can be discarded.
	_ = j.Use(Plugin)

	if include != "" || exclude != "" {
		j.SetOptions(tabnas.Options{
			Rule: &tabnas.RuleOptions{Include: include, Exclude: exclude},
		})
	}

	return j
}

// Parse parses a relaxed-JSON string and returns the resulting Go value:
//   - map[string]any for objects
//   - []any for arrays
//   - float64 for numbers
//   - string for strings
//   - bool for booleans
//   - nil for null or empty input
//
// Returns a *tabnas.TabnasError if the input contains a syntax error.
func Parse(src string) (any, error) {
	return Make().Parse(src)
}

// MakeJSON creates a Tabnas instance configured to accept strict JSON
// only. Mirrors the TS json-plugin test fixture semantics. Rejects
// tabnas relaxations: unquoted keys/values, comments, hex/octal/binary
// numbers, trailing commas, leading-zero numbers, single-quoted or
// backtick strings, and empty input.
func MakeJSON() *tabnas.Tabnas {
	f := false
	return Make(tabnas.Options{
		Text: &tabnas.TextOptions{Lex: &f},
		Number: &tabnas.NumberOptions{
			Hex: &f, Oct: &f, Bin: &f,
			Sep: "",
			Exclude: func(s string) bool {
				return len(s) >= 2 && s[0] == '0' && s[1] == '0'
			},
		},
		String: &tabnas.StringOptions{
			Chars:        `"`,
			MultiChars:   "",
			AllowUnknown: &f,
		},
		Comment: &tabnas.CommentOptions{Lex: &f},
		Map:     &tabnas.MapOptions{Extend: &f},
		Lex:     &tabnas.LexOptions{Empty: &f},
		Rule: &tabnas.RuleOptions{
			Finish:  &f,
			Include: "json",
		},
	})
}
