# tabnas (Go)

Version: 0.1.22

A grammar-free parsing engine plus a relaxed-JSON grammar, in the
jsonic tradition: lenient JSON for humans. It parses what you meant,
not just what you typed — unquoted keys, implicit objects and arrays,
comments, trailing commas, single-quoted strings, path diving, and
more.

The module ships two packages:

- the engine, `github.com/tabnas/parser/go` (imported as `tabnas`),
  which ships **no grammar** — grammar comes from plugins;
- the relaxed-JSON grammar, `github.com/tabnas/parser/go/jsonic`,
  which is what most Go clients want.

This is a Go port of the [TypeScript reference](../ts/); both runtimes
share the spec fixtures under [`../test/spec/`](../test/spec/) to stay
aligned.

## Install

```bash
go get github.com/tabnas/parser/go@latest
```

## A taste

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

No schema, no struct tags, no ceremony. Full UTF-8 support (keys,
values, escapes — including astral-plane characters and JSON surrogate
pairs), and the API never panics: every failure is a returned `error`,
even for arbitrary malformed byte input.

## Documentation

Learning and reference, by purpose:

- [Tutorial](doc/tutorial.md) — start here: `go get` to a working
  parse and one customization.
- [How-to guide](doc/guide.md) — focused recipes for individual tasks.
- [API reference](doc/api.md) — every type, function, and method.
- [Options reference](doc/options.md) — every configuration field.
- [Syntax reference](doc/syntax.md) — Go result types.
- [Plugin guide](doc/plugins.md) — authoring grammars and matchers.
- [Concepts](doc/concepts.md) — how the Go packages fit together and
  the no-panic guarantee.
- [Differences from TypeScript](doc/differences.md) — for those who
  use both runtimes.

Shared, language-neutral docs:

- [Syntax specification](../doc/syntax.md) — the canonical relaxed-JSON
  grammar.
- [Architecture](../doc/architecture.md) — the engine design.

## License

MIT. Copyright (c) Richard Rodger.
