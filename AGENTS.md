# Agents Guide — tabnas monorepo

## What this project is

tabnas grew out of the **jsonic use case**: parsing lenient,
human-written JSON — unquoted keys (`a:1`), implicit objects/arrays
(`a:1,b:2`, `x,y,z`), comments, trailing commas, single/backtick
quotes, multiline strings, and path diving (`a:b:1` → `{a:{b:1}}`).
Keep that use case in mind for every change: the engine exists so that
grammars like this can be expressed as plugins, and the shared test
fixtures encode exactly that lenient-JSON behavior.

The engine is a rule-based parser over a configurable matcher-based
lexer. Grammar is contributed by plugins.

## Repository map

| Path | What it is |
|---|---|
| `ts/` | **Canonical** TypeScript implementation. A grammar-free engine package (`tabnas` on npm). Strict-JSON grammar lives as a test fixture (`ts/test/json-plugin.ts`). BNF and Debug plugins live in separate repos. |
| `go/` | Go port of the engine — grammar-free like TS. Module: `github.com/tabnas/parser/go`. |
| `go/jsonic/` | The relaxed-JSON (jsonic-style) grammar as a plugin package plus convenience API (`jsonic.Parse`, `jsonic.Make`, `jsonic.MakeJSON`). What most Go clients import. |
| `test/spec/` | Shared `.tsv` fixtures (input → expected pairs, or `ERROR:<code>`). The Go jsonic package runs all of them; TS runs the strict-JSON (`include-json*.tsv`) and `utility-*.tsv` ones. |

## Authority and alignment rules

1. **TypeScript is canonical.** When TS and Go disagree on engine
   behavior, TS wins; change Go (and add/extend a shared fixture when
   the behavior is expressible as input → output).
2. **Go-only features are intentional** and must be kept and tested:
   `Info.Map` (`MapRef`), `Info.List` (`ListRef`), `Info.Text`
   (`Text`), `jsonic.MakeJSON()`, and the introspection API. They exist for
   typed Go client code.
3. The Go layout mirrors TS: the engine package ships no grammar; the
   relaxed-JSON grammar is the `go/jsonic` plugin package. Don't fold
   the grammar back into the engine.
4. Known, accepted behavior differences are documented in
   `go/doc/differences.md`. Update that file whenever you change
   either side's behavior or feature surface.
5. When you add a TS feature, port it to Go in the same change when
   feasible, or record it in `go/doc/differences.md` if not.

## Build / test / coverage

From `ts/` (see `ts/Makefile` for combined targets):

```bash
npm install && npm run build   # tsc --build src test
npm test                       # node --test, includes shared fixtures
node --test --experimental-test-coverage test/**/*.test.js
```

From `go/`:

```bash
go build ./... && go vet ./...
go test ./...                  # engine + jsonic; includes all shared fixtures
go test -coverpkg=./... -cover ./...
```

`make -C ts test` runs both suites.

## Shared spec fixtures (`test/spec/*.tsv`)

Tab-separated, header row first, one case per line. `\n`, `\r`, `\t`
in the input column are unescaped by the loaders. The expected column
is JSON, or `ERROR:<code>` for error cases. Loaders:
`ts/test/utility.js` (`loadTSV`) and `go/alignment_test.go`
(`runParserTSV` / `runErrorTSV`).
