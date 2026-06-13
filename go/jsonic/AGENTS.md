# Agents Guide — tabnas/jsonic (Go)

The relaxed-JSON grammar for the tabnas engine — the original jsonic
use case (unquoted keys, implicit objects/arrays, comments, trailing
commas, single/backtick quotes, multiline strings, path diving). This
is what most Go clients import; the bare engine lives one level up in
package `tabnas` (`../`).

Import path: `github.com/tabnas/parser/go/jsonic`.

## What's here

- `grammar.go` — `buildGrammar`, the declarative relaxed-JSON grammar
  (rules, alternates, state actions) installed onto a `*tabnas.Tabnas`.
  This is the bulk of the package and the thing most changes touch.
- `jsonic.go` — the public surface: `Plugin` (a `tabnas.Plugin`),
  `Make(opts...)` (engine + grammar), `Parse(src)`, `MakeJSON()`
  (strict JSON), and an `init()` that registers `Parse` as the engine's
  text-form parser via `tabnas.RegisterTextParser`.
- `*_test.go` — the behavioral test suite, including the shared-fixture
  runners and the UTF-8 / no-panic / fuzz coverage.

## How the grammar attaches

`Plugin` calls `buildGrammar(j.RSM(), j.Config())` — it populates the
engine's rule-spec map and lexer config through the engine's public
accessors. There is no private coupling: anything `buildGrammar` needs
from the engine must be an exported `tabnas` symbol.

## Authority

The TypeScript implementation (`../../ts/`) is canonical for engine
behavior. The relaxed-JSON grammar itself is a Go-side concern (TS keeps
only a strict-JSON test fixture), but lexer/parser semantics it relies
on must match TS. Accepted differences live in `../doc/differences.md`.

## Tests and fixtures

- Shared `.tsv` fixtures live in `../../test/spec/`. The runners
  (`loadTSV`, `runParserTSV`, `runErrorTSV`, `specDir`) are in
  `feature_tsv_test.go` / `helpers_test.go`; `specDir` resolves
  `../../test/spec`. Prefer adding a shared fixture over a one-off
  assertion when a case is expressible as input → output.
- `utf8_test.go` carries the `FuzzParse` fuzz target — run
  `go test -fuzz=FuzzParse -fuzztime=20s ./` after lexer/grammar
  changes. Parsing must never panic on any input.
- Error-output assertions: ANSI color is on by default — disable with
  `Options{Color: &ColorOptions{Active: &off}}` or assert on
  escape-safe substrings.

## Commands

```bash
go test ./...                         # from go/, runs engine + jsonic
go test -coverpkg=./... -cover ./...  # combined coverage
go test -run TestName -v ./jsonic/
```

## Conventions

- Keep `tabnas.` qualification on every engine symbol — this package
  imports the engine, it is not part of it.
- When porting a TS test, mirror its name in a comment.
- Don't add `panic(...)`; the no-panic guarantee is part of the
  contract (see `../doc/concepts.md` once written, or
  `../doc/differences.md`).
