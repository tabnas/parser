# Agents Guide — parser

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
| `ts/` | **Canonical** TypeScript implementation. The grammar-free engine package (`@tabnas/parser` on npm, currently `2.28.0`). Source in `src/` (`tabnas.ts`, `lexer.ts`, `rules.ts`, `parser.ts`, `context.ts`, `defaults.ts`, `error.ts`, `utility.ts`, `types.ts`). Strict-JSON grammar lives as a test fixture (`ts/test/json-plugin.ts`). BNF and Debug plugins live in separate repos. |
| `go/` | Go port of the engine — grammar-free like TS. Module: `github.com/tabnas/parser/go`; the package's `const Version` lives in `go/tabnas.go` (currently `0.1.22`). Strict-JSON grammar lives as a test fixture (`go/jsonplugin_test.go`), mirroring the TS fixture. Grammar packages are shipped separately, not in this repo. |
| `test/spec/` | Shared `.tsv` fixtures (input → expected pairs, or `ERROR:<code>`). Both runtimes run the strict-JSON (`include-json*.tsv`) and `utility-*.tsv` ones; the relaxed-grammar fixtures are kept for downstream grammar packages. |

## Authority and alignment rules

1. **TypeScript is canonical.** When TS and Go disagree on engine
   behavior, TS wins; change Go (and add/extend a shared fixture when
   the behavior is expressible as input → output).
2. **Go-only features are intentional** and must be kept and tested:
   `Info.Map` (`MapRef`), `Info.List` (`ListRef`), `Info.Text`
   (`Text`), and the introspection API. They exist for typed Go client
   code and are exercised in `go/feature_info_test.go`.
3. The Go layout mirrors TS: the engine package ships no grammar. The
   strict-JSON grammar lives as a test fixture (`go/jsonplugin_test.go`),
   not in the engine. Don't fold a grammar back into the engine.
4. Known, accepted behavior differences are documented in
   `go/doc/differences.md`. Update that file whenever you change
   either side's behavior or feature surface.
5. When you add a TS feature, port it to Go in the same change when
   feasible, or record it in `go/doc/differences.md` if not.

## Dev dependencies & CI

The engine has **no runtime tabnas dependencies** — it is the bottom of
the stack. Its only `@tabnas` deps are **dev-only** `file:` siblings in
`ts/package.json`: `@tabnas/debug` and `@tabnas/railroad` (used to
regenerate `ts/doc/grammar.{svg,txt}` and the README diagrams; debug is
not a runtime peer here). `engines.node` is `">=24"`.

CI (`.github/workflows/build.yml`) does not publish to npm. Both jobs
git-clone the downstream tabnas closure (`debug json abnf railroad`) as
siblings so the dependents can build against this engine:

- **build** (Ubuntu/Windows/macOS, Node 24): sets
  `git config --global core.autocrlf false` (CRLF corrupts the `.tsv`
  fixtures), then `npm i && npm run build --if-present` for `parser` and
  each sibling in order, then `npm test` here.
- **build-go** (Ubuntu/macOS, Go 1.24): creates `vendor/` symlinks for
  any `../vendor/` replaces and a `go work` over every non-vendor-replaced
  module, then `go build ./...` / `go test -v ./...` here.

## Build / test / coverage

From `ts/` (see `ts/Makefile`, or the repo-root [`Makefile`](Makefile)
for combined targets):

```bash
npm install && npm run build   # tsc --build src test
npm test                       # node --test, includes shared fixtures
node --test --experimental-test-coverage test/**/*.test.js
```

From `go/`:

```bash
go build ./... && go vet ./...
go test ./...                  # engine + strict-JSON fixture; shared fixtures
go test -coverpkg=./... -cover ./...
```

The repo-root [`Makefile`](Makefile) (adapted from voxgig/util) wraps
both halves: `make build|test|clean` run the TS and Go sides,
`make reset` rebuilds from clean, and `make publish-go V=x.y.z` injects
`V` into the `const Version` in `go/tabnas.go`, commits, and tags
`go/vX.Y.Z`. `make publish-ts` publishes the TS package at its
`package.json` version. (`make -C ts test` runs the TS suite alone.)

## Shared spec fixtures (`test/spec/*.tsv`)

Tab-separated, header row first, one case per line. `\n`, `\r`, `\t`
in the input column are unescaped by the loaders. The expected column
is JSON, or `ERROR:<code>` for error cases. Loaders:
`ts/test/utility.js` (`loadTSV`) and `go/spec_test.go`
(`runParserTSV` / `runErrorTSV`; `specDir` resolves `../test/spec`).

## Documentation structure

Docs are split by purpose, and that split is intentional — keep each
file to one job:

- **Tutorials** (`ts/doc/tutorial.md`, `go/doc/tutorial.md`) teach a
  newcomer step by step.
- **How-to guides** (`{ts,go}/doc/guide.md`, `{ts,go}/doc/plugins.md`) are
  task recipes.
- **Reference** (`{ts,go}/doc/api.md`, `{ts,go}/doc/options.md`, and the
  language-neutral top-level [`doc/syntax.md`](doc/syntax.md)) is dry and
  complete.
- **Explanation** (top-level [`doc/architecture.md`](doc/architecture.md),
  `{ts,go}/doc/concepts.md`, `go/doc/differences.md`, the
  `ts/doc/{bnf-to-tabnas,lsp}-feasibility.md` reports) covers design and
  rationale.

The per-runtime `api/options/guide/plugins/concepts/tutorial` docs live in
`ts/doc/` and `go/doc/`; the top-level [`doc/`](doc/) holds only the two
language-neutral files shared by both runtimes — `syntax.md` (syntax spec)
and `architecture.md`.

READMEs are orientation hubs that route to the four types — don't grow
them into manuals. When you change behavior or signatures, update the
matching reference doc; when you add a capability, consider whether it
needs a how-to.
