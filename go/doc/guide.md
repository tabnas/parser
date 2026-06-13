# How-to guide (Go)

Task-focused recipes. Each is self-contained. For a guided
introduction start with the [tutorial](tutorial.md); for complete
field and signature lists see the [API reference](api.md) and
[options reference](options.md).

All recipes assume these imports and the pointer helper:

```go
import (
	tabnas "github.com/tabnas/parser/go"
	"github.com/tabnas/parser/go/jsonic"
)

func boolp(b bool) *bool { return &b }
```

## Parse strict JSON

Use `jsonic.MakeJSON`, which returns an instance that rejects every
relaxation (unquoted keys, comments, trailing commas, hex/octal/binary
numbers, single/backtick quotes, empty input):

```go
j := jsonic.MakeJSON()

j.Parse(`{"a":1}`) // ok
j.Parse("a:1")      // *TabnasError — unquoted key rejected
```

Under the hood this filters the grammar to alternates tagged `json`
via the `Rule.Include` option. To get the same filtering on a custom
configuration, pass `Rule: &tabnas.RuleOptions{Include: "json"}` to
`jsonic.Make`.

## Keep numbers as strings

Turn the number matcher off so numeric-looking values lex as text:

```go
j := jsonic.Make(tabnas.Options{
	Number: &tabnas.NumberOptions{Lex: boolp(false)},
})

result, _ := j.Parse("a:1, b:2")
// map[string]any{"a": "1", "b": "2"}
```

To keep numbers but drop a specific format, set `Hex`, `Oct`, or `Bin`
to `boolp(false)` instead.

## Handle errors

Every parse failure is a `*tabnas.TabnasError`. Type-assert (or use
`errors.As`) to read its structured fields:

```go
_, err := jsonic.Parse(`"abc`)
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
j := jsonic.Make(tabnas.Options{Info: &tabnas.InfoOptions{
	Text: boolp(true), // strings → tabnas.Text{Quote, Str}
	List: boolp(true), // arrays  → tabnas.ListRef{Val, Implicit, ...}
	Map:  boolp(true), // objects → tabnas.MapRef{Val, Implicit, ...}
}})

result, _ := j.Parse("a:'x'")
mr := result.(tabnas.MapRef)
fmt.Println(mr.Implicit)          // true (no braces in source)
tx := mr.Val["a"].(tabnas.Text)
fmt.Println(tx.Quote, tx.Str)     // ' x
```

`Text.Quote` is the quote character (`""` for unquoted text).
`ListRef.Implicit` / `MapRef.Implicit` report whether brackets/braces
were present. See the [syntax reference](syntax.md#extended-result-types)
for the full struct fields.

## Use the bare engine with your own grammar

`tabnas.Make()` returns an engine with no grammar. Install the
relaxed-JSON grammar explicitly as a plugin:

```go
j := tabnas.Make()
if err := j.Use(jsonic.Plugin); err != nil {
	// jsonic.Plugin never fails, but Use returns an error in general
}
result, _ := j.Parse("a:1")
```

For a different language, register tokens and rules yourself with
`Token`, `Rule`, or the declarative `Grammar`. See the
[plugin guide](plugins.md) for the grammar-authoring details.

## Add a custom matcher

To recognize syntax beyond the built-in matchers, register one under
`Options.Lex.Match`, keyed by name. The factory is invoked when the
options are applied; the matcher it returns reads from `lex.Cursor()`
and must advance the cursor when it produces a token:

```go
j := jsonic.Make(tabnas.Options{Lex: &tabnas.LexOptions{
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
