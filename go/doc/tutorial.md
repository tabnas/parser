# Tutorial: your first tabnas parse (Go)

This walks you from nothing to a working parse, then through one
customization. Follow it in order — each step builds on the last.
When you finish you will have parsed a string, inspected the result,
configured a parser instance, and handled an error.

For a recipe-style index of individual tasks, see the
[how-to guide](guide.md). For exhaustive signatures, see the
[API reference](api.md).

## 1. Install

Add the module to your project:

```bash
go get github.com/tabnas/parser/go@latest
```

You will use two packages: the engine
(`github.com/tabnas/parser/go`, imported as `tabnas`) and the
relaxed-JSON grammar (`github.com/tabnas/parser/go/jsonic`). For a
first parse you only need `jsonic`.

## 2. Parse a string

Create `main.go`:

```go
package main

import (
	"fmt"

	"github.com/tabnas/parser/go/jsonic"
)

func main() {
	result, err := jsonic.Parse("a:1, b:2")
	if err != nil {
		panic(err)
	}
	fmt.Println(result) // map[a:1 b:2]
}
```

Run it:

```bash
go run .
```

You wrote `a:1, b:2` — no braces, no quotes around the keys — and got
back an object. That is the point of the relaxed grammar: it parses
what you meant. `jsonic.Parse` is the zero-config convenience
function; it builds a fresh parser for each call.

## 3. Inspect the result

`jsonic.Parse` returns `any`. For relaxed-JSON input the concrete
types are predictable:

- objects → `map[string]any`
- arrays → `[]any`
- numbers → `float64`
- strings → `string`
- booleans → `bool`
- `null` / empty input → `nil`

So type-assert and read fields directly:

```go
result, _ := jsonic.Parse("a:1, b:2")
m := result.(map[string]any)
fmt.Println(m["a"]) // 1   (a float64)
```

Numbers come back as `float64`, matching `encoding/json`. The full
list of result types lives in the [syntax reference](syntax.md).

## 4. Make a configured instance

The defaults are not the only option. `jsonic.Make` returns a
configured parser instance — the engine plus the relaxed-JSON grammar
— that you can reuse across many parses. It takes a `tabnas.Options`
value.

Option fields are pointers, so `nil` means "use the default". Define
a tiny helper to take the address of a literal:

```go
func boolp(b bool) *bool { return &b }
```

Now change one behavior — turn number lexing off so numeric-looking
values stay as strings:

```go
import (
	tabnas "github.com/tabnas/parser/go"
	"github.com/tabnas/parser/go/jsonic"
)

j := jsonic.Make(tabnas.Options{
	Number: &tabnas.NumberOptions{Lex: boolp(false)},
})

result, _ := j.Parse("a:1, b:2")
m := result.(map[string]any)
fmt.Println(m["a"]) // 1   (now a string, not a float64)
```

The same instance `j` can parse as many strings as you like. Every
option is documented in the [options reference](options.md).

## 5. Catch an error

When the input is malformed, `Parse` returns an `error` — it never
panics. Parse an unterminated string and look at the structured
detail:

```go
import (
	"errors"
	"fmt"

	tabnas "github.com/tabnas/parser/go"
	"github.com/tabnas/parser/go/jsonic"
)

_, err := jsonic.Parse(`"abc`)
var te *tabnas.TabnasError
if errors.As(err, &te) {
	fmt.Println(te.Code) // unterminated_string
	fmt.Println(te.Row, te.Col) // 1 1
}
```

`err.Error()` prints a formatted, colorized message with a caret
pointing at the source location and an explanatory hint — useful for
end users. The `*tabnas.TabnasError` fields (`Code`, `Row`, `Col`,
`Hint`, …) are for your code to branch on.

## Where to go next

- [How-to guide](guide.md) — focused recipes for individual tasks.
- [Options reference](options.md) — every configuration field.
- [API reference](api.md) — every type, function, and method.
- [Concepts](concepts.md) — how the Go packages fit together and why.
