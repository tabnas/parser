# Agents Guide — tabnas (Go)

Go port of the tabnas engine. The layout mirrors the canonical
TypeScript package: this engine package ships **no grammar**.
`tabnas.Make()` returns a bare engine; callers bring their own grammar
plugin via `Use`. A strict-JSON grammar is kept as a test fixture
(`jsonplugin_test.go` — `makeJSON`, `registerJSONGrammar`, `jsonPlugin`),
mirroring `ts/test/json-plugin.ts`, so the engine always has a
non-trivial grammar to test against.

Module path: `github.com/tabnas/parser/go`. One package: `tabnas`
(engine, this directory).

## Authority

The TypeScript implementation (`../ts/`) is canonical for engine
behavior. When porting or fixing, read the TS source first
(`ts/src/defaults.ts` for option/error/hint defaults, `ts/src/error.ts`
for error formatting, `ts/src/lexer.ts` for matcher semantics) and
mirror it. Accepted differences are documented in
`doc/differences.md` — keep that file current.

## Go-only client features (keep, and keep tested)

- `Info.Map` → `MapRef`, `Info.List` → `ListRef`, `Info.Text` →
  `Text` wrappers (typed metadata for Go clients); tests in
  `feature_info_test.go`.
- Introspection API (`RSM()`, `Plugins()`, `Decorate()`, ...).

## Layout

- `tabnas.go` — `TabnasError`, error/hint templates (mirrors TS
  defaults), `Error()` formatting (mirrors TS `errmsg`/`errsite`).
- `lexer.go` — matchers and `LexConfig` (the resolved option tree;
  TS `cfg`). Simple matchers run on the scan-spec driver (scan.go),
  matching the TS lexer structure.
- `parser.go`, `rule.go` — rule machinery.
- `options.go` — `Options` tree, `Make`/`Empty`,
  `buildConfig` (Options → LexConfig, merging defaults).
- `plugin.go` — `Use`, `SetOptions`, `Grammar`, match registration.
- `grammarspec.go` — declarative grammar-spec machinery (engine).
- `scan.go` — scan-spec driver and builders (mirrors TS lexer scan).
- `jsonplugin_test.go` — strict-JSON grammar test fixture
  (`makeJSON`/`registerJSONGrammar`/`jsonPlugin`); `spec_test.go` runs
  the shared `include-json*` fixtures against it.
- `utility.go` — `Deep`, `StrInject`, text-form option parsing.

## Documentation

`README.md` is an orientation hub: what the module is, `go get`, one
taste example, and links out. The `doc/` files are organized by a
single purpose each — keep them unmixed:

- `doc/tutorial.md` — learning-oriented, one sequential happy path.
- `doc/guide.md` — task-oriented how-to recipes.
- `doc/api.md`, `doc/options.md`, `doc/syntax.md` — reference (dry,
  complete; do not teach).
- `doc/concepts.md`, `doc/differences.md` — explanation (background,
  rationale, TS↔Go comparison).
- `doc/plugins.md` — focused grammar-authoring how-to.

When editing, verify every signature against the source and keep
examples compiling. Don't turn the tutorial into an options dump or
the reference into a tutorial.

## Commands

```bash
go build ./... && go vet ./...
go test ./...            # engine + strict-JSON fixture (../test/spec fixtures)
go test -coverpkg=./... -cover ./...
go test -run TestName -v ./...
```

## Testing conventions

- Shared behavior: add a fixture under `../test/spec/` and run it via
  `runParserTSV` / `runErrorTSV` (`spec_test.go`).
- Engine tests drive the lexer standalone, install a small inline
  grammar, or build on the strict-JSON fixture (`makeJSON`).
- Go-specific API: plain `_test.go` files; mirror the TS test name in
  a comment when porting a TS test.
- Error-output assertions: ANSI color is on by default — disable via
  `Options{Color: &ColorOptions{Active: &off}}` or assert on
  substrings that avoid escape-code boundaries.
- **No panics**: public APIs return errors, never panic — parsing has
  a recover guard (panics become `"internal"` TabnasErrors). Don't add
  `panic(...)` to production code; thread an error instead.
- Unicode: any UTF-8 char works in data and as configured matcher
  chars; columns count runes. See `doc/differences.md` ("Unicode").
