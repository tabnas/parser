# tabnas (Go)

Version: 0.1.22

A Go port of [tabnas](https://github.com/tabnas/parser) — the
lenient JSON parser in the jsonic tradition. The TypeScript reference
engine lives at [`../ts/`](../ts/) in this repo; both runtimes share
the spec fixtures under [`../test/spec/`](../test/spec/), which keep
the engine behavior aligned.

Like the TypeScript package, the engine package
(`github.com/tabnas/parser/go`) ships **no grammar** — grammar comes
from plugins. The relaxed-JSON grammar lives in the
[`jsonic`](jsonic/) sub-package, which is what most Go clients want:

tabnas/jsonic accepts all standard JSON -- and then goes further.
Unquoted keys, implicit objects, comments, trailing commas,
single-quoted strings, multiline strings, path diving, and more. It
parses what you meant, not just what you typed.

## Install

```bash
go get github.com/tabnas/parser/go@latest
```

## Quick Example

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

That's it. No schema, no struct tags, no ceremony.

## Configured Instance

You don't have to accept the defaults. `jsonic.Make` gives you a
configured parser instance (engine + relaxed-JSON grammar) with
whatever behavior you need:

```go
import (
    tabnas "github.com/tabnas/parser/go"
    "github.com/tabnas/parser/go/jsonic"
)

func boolp(b bool) *bool { return &b }

j := jsonic.Make(tabnas.Options{
    Number: &tabnas.NumberOptions{Lex: boolp(false)},
})

result, err := j.Parse("a:1, b:2")
// {"a": "1", "b": "2"} — numbers are kept as strings
```

For a bare engine with your own grammar, use `tabnas.Make()` and
install rules via `Rule`/`Grammar`, or apply the jsonic grammar
explicitly with `j.Use(jsonic.Plugin)`.

Options compose. Turn things off, turn things on. You can always change
it later.

## Syntax

tabnas/jsonic accepts all standard JSON plus the relaxations listed in the
[syntax reference](doc/syntax.md). Here are the highlights:

- **Unquoted keys**: `a:1` &rarr; `{"a": 1}`
- **Implicit objects**: `a:1,b:2` &rarr; `{"a": 1, "b": 2}`
- **Implicit arrays**: `a,b,c` &rarr; `["a", "b", "c"]`
- **Comments**: `#`, `//`, `/* */`
- **Single/backtick quotes**: `'hello'`, `` `hello` ``
- **Path diving**: `a:b:1` &rarr; `{"a": {"b": 1}}`
- **Trailing commas**: `{a:1,}` &rarr; `{"a": 1}`
- **All number formats**: hex, octal, binary, separators

## Documentation

- [API Reference](doc/api.md) -- types, functions, and methods
- [Syntax Reference](doc/syntax.md) -- all supported syntax
- [Options Reference](doc/options.md) -- configuration options
- [Plugin Guide](doc/plugins.md) -- writing plugins
- [Differences from TypeScript](doc/differences.md) -- what to know if you use both

## License

MIT. Copyright (c) Richard Rodger.
