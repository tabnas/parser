# Agents Guide — shared spec fixtures

`spec/*.tsv` holds the cross-runtime conformance fixtures. Both runtimes
run the strict-JSON subset against their strict-JSON test grammars, so a
change to those fixtures affects both languages — edit with that in mind.
The relaxed-grammar fixtures are retained for downstream grammar packages.

## Format

Tab-separated, one case per line, with a header row (`input` `expected`
or, for list-child fixtures, a third column). Loaders unescape `\n`,
`\r`, `\t` in the `input` column. The `expected` column is either:

- a JSON value (the parse result), or
- `ERROR:<code>` for inputs that must fail with that error code.

## Who runs what

- Go: `go/spec_test.go` (`loadTSV` / `runParserTSV` / `runErrorTSV`;
  `specDir` → `../test/spec`) runs the `include-json*` fixtures through
  the strict-JSON test fixture (`go/jsonplugin_test.go`).
- TypeScript: `ts/test/utility.js` (`loadTSV`); `ts/test/json-spec.test.js`
  runs the `include-json*` fixtures through the strict-JSON test grammar,
  and `ts/test/utility.test.js` runs the `utility-*` fixtures.

## Naming families

| Prefix | Purpose |
|---|---|
| `alignment-*` | behaviors pinned identical across TS and Go |
| `include-json*` | strict-JSON surface (run by both runtimes) |
| `exclude-*` | option-restricted grammars (e.g. comma/strict-json off) |
| `feature-*` | individual relaxed-JSON features |
| `tabnas-*`, `fv-*`, `comma-*` | broader relaxed-JSON scenarios |
| `lex-errors` | lexer-level error cases |
| `utility-*` | utility-function fixtures (TS + Go) |

## Rules

- Prefer adding a fixture here over a one-off in-language assertion when
  a case is expressible as input → output — that keeps the two runtimes
  honest against each other.
- A new `alignment-*` case must pass in BOTH runtimes; run
  `go test ./...` (from `go/`) and `npm test` (from `ts/`) before
  considering it done.
- Keep `expected` JSON canonical (sorted-key-independent comparison is
  the loaders' job, but write it readably).
