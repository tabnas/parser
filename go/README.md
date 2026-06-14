# tabnas (Go)

Version: 0.1.22

A pluggable parsing engine: a rule-based parser over a configurable
matcher-based lexer, exposed as the `tabnas.Tabnas` type. The package
ships **no grammar** of its own — every grammar is a plugin that you
(or another package) supply, matching the canonical TypeScript package.

This is a Go port of the [TypeScript reference](../ts/); both runtimes
share the spec fixtures under [`../test/spec/`](../test/spec/) to stay
aligned.

## Install

```bash
go get github.com/tabnas/parser/go@latest
```

## A taste

A tiny one-token grammar defined inline as a plugin:

```go
package main

import (
	"fmt"

	tabnas "github.com/tabnas/parser/go"
)

func main() {
	j := tabnas.Make(tabnas.Options{Rule: &tabnas.RuleOptions{Start: "val"}})
	_ = j.Use(func(j *tabnas.Tabnas, _ map[string]any) error {
		hi := "hello"
		j.SetOptions(tabnas.Options{Fixed: &tabnas.FixedOptions{
			Token: map[string]*string{"#HI": &hi},
		}})
		HI := j.Token("#HI")
		j.Rule("val", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
			rs.AddOpen(&tabnas.AltSpec{
				S: [][]tabnas.Tin{{HI}},
				A: func(r *tabnas.Rule, _ *tabnas.Context) { r.Node = "world" },
			})
			rs.AddClose(&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinZZ}}})
		})
		return nil
	})

	result, _ := j.Parse("hello")
	fmt.Println(result) // world
}
```

Full UTF-8 support (keys, values, escapes — including astral-plane
characters and JSON surrogate pairs), and the API never panics: every
failure is a returned `error`, even for arbitrary malformed byte input.
For a complete, non-trivial grammar, see the strict-JSON test fixture at
[`jsonplugin_test.go`](jsonplugin_test.go).

## Documentation

Learning and reference, by purpose:

- [Tutorial](doc/tutorial.md) — start here: `go get` to a working
  parse and one customization.
- [How-to guide](doc/guide.md) — focused recipes for individual tasks.
- [API reference](doc/api.md) — every type, function, and method.
- [Options reference](doc/options.md) — every configuration field.
- [Syntax reference](doc/syntax.md) — Go result types.
- [Plugin guide](doc/plugins.md) — authoring grammars and matchers.
- [Concepts](doc/concepts.md) — how the engine fits together and the
  no-panic guarantee.
- [Differences from TypeScript](doc/differences.md) — for those who
  use both runtimes.

Shared, language-neutral docs:

- [Syntax specification](../doc/syntax.md) — the syntax reference.
- [Architecture](../doc/architecture.md) — the engine design.

## License

MIT. Copyright (c) Richard Rodger.
