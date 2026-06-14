# Tutorial: your first parser (Go)

tabnas ships no grammar, so there is nothing to "turn on" — you teach
the engine one token and one rule, watch it parse, then extend it once.
This walks you from nothing to a working parse and an error you can
read. Follow it in order — each step builds on the last.

For a recipe-style index of individual tasks, see the
[how-to guide](guide.md). For exhaustive signatures, see the
[API reference](api.md).

## 1. Install

Add the module to your project:

```bash
go get github.com/tabnas/parser/go@latest
```

You need one package: the engine
(`github.com/tabnas/parser/go`, imported as `tabnas`).

## 2. Create an instance

A parser is a `*tabnas.Tabnas` value. Create one and try to parse
something:

```go
package main

import (
	"fmt"

	tabnas "github.com/tabnas/parser/go"
)

func main() {
	j := tabnas.Make()
	_, err := j.Parse("hello")
	fmt.Println(err) // non-nil: no grammar yet
}
```

The bare instance knows how to *lex* (split text into tokens) but has
no rule that says what to *do* with them, so this returns an error. The
API never panics — every failure comes back as an `error`. Adding a
grammar is the whole point.

## 3. Define one token and one rule

A grammar is a plugin: a `func(j *tabnas.Tabnas, opts map[string]any) error`
that configures the instance. The smallest useful grammar recognises a
single word.

```go
func helloGrammar(j *tabnas.Tabnas, _ map[string]any) error {
	// Teach the lexer a fixed token: the source `hello` lexes as #HI.
	hi := "hello"
	j.SetOptions(tabnas.Options{Fixed: &tabnas.FixedOptions{
		Token: map[string]*string{"#HI": &hi},
	}})
	HI := j.Token("#HI")

	// Teach the start rule (`val`) what to do when it sees that token:
	// set the result node to the string "world".
	j.Rule("val", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.AddOpen(&tabnas.AltSpec{
			S: [][]tabnas.Tin{{HI}},
			A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = "world" },
		})
		rs.AddClose(&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}})
	})
	return nil
}
```

Two things happened here:

- The `Fixed` option registered a **fixed token** — an exact source
  string. `"hello"` in the input now lexes as the token named `#HI`.
- `j.Rule("val", ...)` modified the start rule. Each rule has an
  **open** phase holding a list of **alternates**. The one alternate
  here says: if the next token sequence (`S`) is `#HI`, run the
  **action** (`A`), which assigns the result to `r.Node`. The close
  alternate accepts end-of-input (`#ZZ`).

`val` is the default start rule. Every parse begins there.

## 4. Parse

Install the plugin with `Use`, then parse:

```go
func main() {
	j := tabnas.Make()
	if err := j.Use(helloGrammar); err != nil {
		panic(err)
	}

	result, _ := j.Parse("hello")
	fmt.Println(result) // world
}
```

Run it:

```bash
go run .
```

You wrote a parser. It accepts exactly one input, but the machinery is
the same one a full JSON grammar uses.

## 5. Extend it once

Real grammars combine tokens. Add a second word and let either match.
An alternate fires when its whole token sequence matches, so add a
second alternate:

```go
func helloGrammar(j *tabnas.Tabnas, _ map[string]any) error {
	hi, by := "hello", "bye"
	j.SetOptions(tabnas.Options{Fixed: &tabnas.FixedOptions{
		Token: map[string]*string{"#HI": &hi, "#BY": &by},
	}})
	HI, BY := j.Token("#HI"), j.Token("#BY")

	j.Rule("val", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.AddOpen(
			&tabnas.AltSpec{S: [][]tabnas.Tin{{HI}}, A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = "world" }},
			&tabnas.AltSpec{S: [][]tabnas.Tin{{BY}}, A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = "farewell" }},
		)
		rs.AddClose(&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}})
	})
	return nil
}

// j.Parse("hello") // world
// j.Parse("bye")   // farewell
```

The parser tries each open alternate in order and takes the first whose
token sequence matches. That ordering — first match wins, two-token
lookahead, no backtracking — is the model you design grammars around.

## 6. Catch an error

When the input does not match, `Parse` returns an `error` — it never
panics. Parse something the grammar rejects and read the structured
detail:

```go
import (
	"errors"
	"fmt"

	tabnas "github.com/tabnas/parser/go"
)

j := tabnas.Make()
_ = j.Use(helloGrammar)

_, err := j.Parse("nope")
var te *tabnas.TabnasError
if errors.As(err, &te) {
	fmt.Println(te.Code)        // e.g. unexpected
	fmt.Println(te.Row, te.Col) // 1 1
}
```

`err.Error()` prints a formatted, colorized message with a caret
pointing at the source location and an explanatory hint — useful for
end users. The `*tabnas.TabnasError` fields (`Code`, `Row`, `Col`,
`Hint`, …) are for your code to branch on.

## Where to go next

- [How-to guide](guide.md) — focused recipes for individual tasks.
- [Plugin guide](plugins.md) — structure a grammar plugin properly,
  including a worked strict-JSON example.
- [Options reference](options.md) — every configuration field.
- [API reference](api.md) — every type, function, and method.
- [Concepts](concepts.md) — how the engine fits together and why.
