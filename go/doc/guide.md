# How-to guide (Go)

Task-focused recipes. Each is self-contained. For a guided
introduction start with the [tutorial](tutorial.md); for complete
field and signature lists see the [API reference](api.md) and
[options reference](options.md).

All recipes assume this import and the pointer helper. Each builds an
engine with `tabnas.Make()` and installs a grammar plugin with `Use`;
`myGrammar` stands for whatever grammar plugin you install (see the
[tutorial](tutorial.md) for a minimal one, or the worked strict-JSON
fixture at [`jsonplugin_test.go`](../jsonplugin_test.go)).

```go
import (
	tabnas "github.com/tabnas/parser/go"
)

func boolp(b bool) *bool { return &b }
```

## Install a grammar and parse

The engine ships no grammar, so a fresh `tabnas.Make()` cannot parse
anything. Install a grammar plugin with `Use`, then parse:

```go
j := tabnas.Make()
if err := j.Use(myGrammar); err != nil {
	// the plugin reported a failure
}
result, _ := j.Parse("hello")
```

Apply options at construction (`tabnas.Make(opts...)`) or layer them on
later with `SetOptions`. To author `myGrammar`, see the
[plugin guide](plugins.md).

## Build a strict-JSON parser

A strict-JSON grammar is just a grammar plugin that registers the
`json` rule group and tightens the number/string/comment options so
every tabnas relaxation (unquoted keys, comments, trailing commas,
hex/octal/binary numbers, single/backtick quotes, empty input) is
rejected. The repository keeps a complete worked example as a test
fixture in [`jsonplugin_test.go`](../jsonplugin_test.go) — read it as a
template for your own grammar:

```go
j := tabnas.Make()
_ = j.Use(strictJSON) // your strict-JSON grammar plugin

j.Parse(`{"a":1}`) // ok
j.Parse("a:1")      // *TabnasError — unquoted key rejected
```

When a grammar tags its alternates with group names, you can restrict
an instance to one group with `Rule: &tabnas.RuleOptions{Include: "json"}`.

## Keep numbers as strings

Turn the number matcher off so numeric-looking values lex as text:

```go
j := tabnas.Make(tabnas.Options{
	Number: &tabnas.NumberOptions{Lex: boolp(false)},
})
_ = j.Use(myGrammar)

result, _ := j.Parse(`{"a":1,"b":2}`)
// numbers stay as their text form, e.g. "1", "2"
```

To keep numbers but drop a specific format, set `Hex`, `Oct`, or `Bin`
to `boolp(false)` instead.

## Handle errors

Every parse failure is a `*tabnas.TabnasError`. Type-assert (or use
`errors.As`) to read its structured fields:

```go
j := tabnas.Make()
_ = j.Use(myGrammar)

_, err := j.Parse(`"abc`)
if te, ok := err.(*tabnas.TabnasError); ok {
	fmt.Println(te.Code) // "unterminated_string"
	fmt.Println(te.Row, te.Col, te.Pos)
	fmt.Println(te.Hint) // human-readable explanation
}
```

`te.Error()` renders the full formatted message (header, source
extract with a caret, hint) for display to end users. To turn off the
ANSI colors in that output, build the instance with
`Color: &tabnas.ColorOptions{Active: boolp(false)}`. The common error
codes are listed in the [API reference](api.md#error-handling).

## Get quote / implicit metadata

The `Info` options wrap output values in typed structs that carry
extra metadata, instead of plain Go values:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{
	Text: boolp(true), // strings → tabnas.Text{Quote, Str}
	List: boolp(true), // arrays  → tabnas.ListRef{Val, Implicit, ...}
	Map:  boolp(true), // objects → tabnas.MapRef{Val, Implicit, ...}
}})
_ = j.Use(myGrammar) // a grammar that honours the Info options

result, _ := j.Parse(`{"a":"x"}`)
mr := result.(tabnas.MapRef)
fmt.Println(mr.Implicit)          // false (braces in source)
tx := mr.Val["a"].(tabnas.Text)
fmt.Println(tx.Quote, tx.Str)     // " x
```

`Text.Quote` is the quote character (`""` for unquoted text). A grammar
that creates containers implicitly (e.g. a relaxed `a:1`) reports
`Implicit: true`; braces/brackets report `false`. See the
[syntax reference](syntax.md#extended-result-types) for the full struct
fields.

## Author a grammar without a plugin function

A grammar plugin is the usual packaging, but it is only a function that
calls `Token` / `Rule`. You can drive the same instance methods inline
when you do not need a reusable plugin:

```go
j := tabnas.Make()
HI := j.Token("#HI", "hello") // register a fixed token
j.Rule("val", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
	rs.AddOpen(&tabnas.AltSpec{
		S: [][]tabnas.Tin{{HI}},
		A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = "world" },
	})
	rs.AddClose(&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}})
})
result, _ := j.Parse("hello") // "world"
```

For larger grammars, prefer the declarative `Grammar(*GrammarSpec)` API
or wrap the setup in a `Plugin` so it can be reused and re-applied on
`Derive`. See the [plugin guide](plugins.md) for the grammar-authoring
details.

## Add a custom matcher

To recognize syntax beyond the built-in matchers, register one under
`Options.Lex.Match`, keyed by name. The factory is invoked when the
options are applied; the matcher it returns reads from `lex.Cursor()`
and must advance the cursor when it produces a token:

```go
j := tabnas.Make(tabnas.Options{Lex: &tabnas.LexOptions{
	Match: map[string]*tabnas.MatchSpec{
		"at": {
			Order: 1_000_000, // < 2_000_000 runs before all built-ins
			Make: func(_ *tabnas.LexConfig, _ *tabnas.Options) tabnas.LexMatcher {
				return func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
					pnt := lex.Cursor()
					if pnt.SI < len(lex.Src) && lex.Src[pnt.SI] == '@' {
						tkn := lex.Token("#TX", tabnas.TinTX, "AT", "@")
						pnt.SI++
						pnt.CI++
						return tkn
					}
					return nil // pass to the next matcher
				}
			},
		},
	},
}})
```

`Order` controls priority (lower runs first); the built-in priorities
and the scan-spec primitives for building matchers are covered in the
[API reference](api.md#custom-matchers). Setting a spec under an
existing name replaces it.
