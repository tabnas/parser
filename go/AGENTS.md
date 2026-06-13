# Agents Guide — tabnas (Go)

Go port of the tabnas engine, serving the original **jsonic use
case**: lenient JSON for humans — `jsonic.Parse("a:1, b:2")` just
works. The layout mirrors the canonical TypeScript package: this
engine package ships **no grammar**; the relaxed-JSON grammar is the
plugin sub-package `jsonic/` (`jsonic.Plugin`, plus `Make`/`Parse`/
`MakeJSON` conveniences). `tabnas.Make()` returns a bare engine.

Module path: `github.com/tabnas/parser/go`. Two packages:
`tabnas` (engine, this directory) and `jsonic` (grammar, `jsonic/`).

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
  `mapref_test.go`, `listref_test.go`, `textinfo_test.go`.
- `jsonic.MakeJSON()` strict-JSON constructor; tests in `jsonic/variant_test.go`.
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
- `jsonic/grammar.go`, `jsonic/jsonic.go` — the relaxed-JSON grammar
  plugin and its convenience API.
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
go test ./...            # engine + jsonic (../test/spec fixtures run in jsonic)
go test -coverpkg=./... -cover ./...
go test -run TestName -v ./...
```

## Testing conventions

- Shared behavior: add a fixture under `../test/spec/` and run it via
  `runParserTSV` / `runErrorTSV` (`jsonic/alignment_test.go`).
- Engine tests must not import jsonic (cycle); drive the lexer
  standalone or install a small inline grammar.
- Go-specific API: plain `_test.go` files; mirror the TS test name in
  a comment when porting a TS test.
- Error-output assertions: ANSI color is on by default — disable via
  `Options{Color: &ColorOptions{Active: &off}}` or assert on
  substrings that avoid escape-code boundaries.
- **No panics**: public APIs return errors, never panic — parsing has
  a recover guard (panics become `"internal"` TabnasErrors), and
  `jsonic/utf8_test.go` carries a `FuzzParse` fuzz target. Don't add
  `panic(...)` to production code; thread an error instead.
- Unicode: any UTF-8 char works in data and as configured matcher
  chars; columns count runes. See `doc/differences.md` ("Unicode").
