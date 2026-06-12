# Agents Guide — tabnas (Go)

Go port of the tabnas engine, serving the original **jsonic use
case** directly: lenient JSON for humans — `tabnas.Parse("a:1, b:2")`
just works. Unlike the canonical TypeScript package (a grammar-free
engine), this module deliberately bundles the relaxed-JSON grammar
(`grammar.go`, `grammarspec.go`) so Go clients need no plugin setup.
That bundling is a feature, not drift — see `doc/differences.md`.

Module path: `github.com/tabnas/parser/go`. Single Go package.

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
- `MakeJSON()` strict-JSON constructor; tests in `variant_test.go`.
- Introspection API (`RSM()`, `Plugins()`, `Decorate()`, ...).

## Layout

- `tabnas.go` — `TabnasError`, error/hint templates (mirrors TS
  defaults), `Error()` formatting (mirrors TS `errmsg`/`errsite`).
- `lexer.go` — matchers and `LexConfig` (the resolved option tree;
  TS `cfg`). Note: predates the TS scan-spec lexer refactor;
  behavior-aligned but not structure-aligned.
- `parser.go`, `rule.go` — rule machinery.
- `options.go` — `Options` tree, `Make`/`Empty`/`MakeJSON`,
  `buildConfig` (Options → LexConfig, merging defaults).
- `plugin.go` — `Use`, `SetOptions`, `Grammar`, match registration.
- `grammar.go`, `grammarspec.go` — the bundled relaxed-JSON grammar.
- `utility.go` — `Deep`, `StrInject`, text-form option parsing.

## Commands

```bash
go build ./... && go vet ./...
go test ./...            # includes all ../test/spec fixtures
go test -cover ./...
go test -run TestName -v ./...
```

## Testing conventions

- Shared behavior: add a fixture under `../test/spec/` and run it via
  `runParserTSV` / `runErrorTSV` (`alignment_test.go`).
- Go-specific API: plain `_test.go` files; mirror the TS test name in
  a comment when porting a TS test.
- Error-output assertions: ANSI color is on by default — disable via
  `Options{Color: &ColorOptions{Active: &off}}` or assert on
  substrings that avoid escape-code boundaries.
