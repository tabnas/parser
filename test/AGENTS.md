# Agents Guide — shared spec fixtures

`spec/*.tsv` holds this repo's cross-runtime conformance fixtures: the
strict-JSON surface and the utility-function cases, run by **both**
runtimes. Every file here is executed by a runner in this repo — if you
add one, wire it up, or it will pin nothing.

**Relaxed-JSON fixtures do not belong here.** The engine ships no grammar,
so it has nothing to check them against; they belong in the repo whose
grammar they describe, next to the suite that runs them:

| Grammar | Repo |
|---|---|
| relaxed JSON (implicit maps/lists, optional commas, comments, unquoted keys, single/backtick strings, path diving) | [`tabnas/jsonic`](https://github.com/tabnas/jsonic) — `test/spec/`, run by `ts/test/*.test.js` and `go/*_test.go` |
| a dialect built on jsonic (csv, toml, yaml, json5, jsonc, ini, zon, …) | that dialect's own repo |

This is not a style preference. This directory used to carry ~48
relaxed-grammar fixtures "for downstream packages"; nothing in this repo
loaded them, `test/spec` is not in the published npm package (`ts/package.json`
`files` is `LICENSE`, `dist`, `dist-test`), and jsonic never read them — it
keeps its own copies and runs those. The result was two sets of the same
files, one live and one dormant, which drifted: the dormant copies fell
behind on whitespace rows, and a fixture added here in good faith
(tabnas/parser#72) pinned nothing at all because no suite loaded the file.
Those fixtures now live in `tabnas/jsonic` only.

## Format

Tab-separated, one case per line, with a header row (`input` `expected`
or, for list-child fixtures, a third column). Loaders unescape `\n`,
`\r`, `\t` in the **`input` column only** — the `expected` column is raw
JSON, so JSON's own escape rules apply there. The `expected` column is
either:

- a JSON value (the parse result), or
- `ERROR:<code>` for inputs that must fail with that error code.

## Who runs what

Every fixture here, and the runner that executes it:

| Fixture | Runner |
|---|---|
| `include-json.tsv`, `include-json-errors.tsv`, `include-json-utf8.tsv`, `include-json-utf8-errors.tsv` | Go `go/spec_test.go` (through the strict-JSON test fixture in `go/jsonplugin_test.go`); TS `ts/test/json-spec.test.js` (through the strict-JSON test grammar) |
| `utility-str.tsv`, `utility-deep.tsv`, `utility-modlist.tsv`, `utility-strinject.tsv` | TS `ts/test/utility.test.js` |
| `lex-string-control.tsv` | Go `go/lexer_optionplumbing_test.go` |
| `happy.tsv` | TS `ts/test/spec.test.js` — a `loadTSV` smoke test only, not a conformance run |

Both loaders (`ts/test/utility.js`, `go/spec_test.go`) must stay in step
on escape handling and on what counts as a row; a divergence there makes
the two runtimes read the same file differently, which is worse than not
sharing it at all.

## Rules

- **Every file here must have a runner.** Adding a `.tsv` is half the
  change; the runners take explicit filenames, not a directory glob, so
  an unregistered fixture is silently dead. Check it runs by breaking a
  row on purpose and confirming the suite goes red.
- Prefer adding a fixture here over a one-off in-language assertion when
  a case is expressible as input → output **and** the engine alone can
  check it. If it needs a grammar, it belongs in that grammar's repo.
- A new case must pass in BOTH runtimes: run `go test ./...` (from `go/`)
  and `npm test` (from `ts/`) before considering it done.
- Keep `expected` JSON canonical (sorted-key-independent comparison is
  the loaders' job, but write it readably).
