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

`divergent.tsv` is the exception to everything in this section: it is the
ADR-14 divergence register, not a conformance fixture, and it has its own
seven-column shape with a `probe` column. Its own header documents it.
See "The divergence register" below.

Every other file here is tab-separated, one case per line, with a header
row (`input` `expected` or, for list-child fixtures, a third column). The
`expected` column is either:

- a JSON value (the parse result), or
- `ERROR:<code>` for inputs that must fail with that error code.

**Escapes: this repo decodes `\n`, `\r`, `\r\n` and `\t` in EVERY
column.** `ts/test/utility.js::loadTSV` maps `unescape` over all columns,
and the Go `lex-string-control` runner deliberately calls
`preprocessEscapes` on both of its columns to match. Write an `expected`
value accordingly: a JSON string that must contain a real newline cannot
be spelled here, because `"a\nb"` is decoded to a raw control character
before `JSON.parse` sees it.

Note that `tabnas/jsonic` decodes the **input column only** (both of its
runtimes). The two repos therefore read a shared fixture differently if
it puts an escape outside the input column — none of the three files
shared with jsonic does today, and `ci/gate/fixture-sync.sh` compares
those files byte-for-byte, so a divergence would show up there. Aligning
the two loaders is worth doing; until then, keep escapes in the `input`
column of any fixture that also lives in jsonic.

## Who runs what

Every fixture here, and the runner that executes it:

| Fixture | TypeScript runner | Go runner |
|---|---|---|
| `include-json.tsv`, `include-json-errors.tsv`, `include-json-utf8.tsv`, `include-json-utf8-errors.tsv` | `ts/test/json-spec.test.js` | `go/spec_test.go` |
| `utility-str.tsv`, `utility-deep.tsv`, `utility-modlist.tsv`, `utility-strinject.tsv` | `ts/test/utility.test.js` | `go/utility_spec_test.go` |
| `lex-string-control.tsv` | `ts/test/lex.test.js` | `go/lexer_optionplumbing_test.go` |
| `happy.tsv` | `ts/test/spec.test.js` — a `loadTSV` smoke test only, not a conformance run | — |
| `divergent.tsv` | `ts/test/divergent.test.js` (the `ts` column) | `go/divergent_test.go` (the `go` column) |

Both strict-JSON runners go through the strict-JSON grammar that lives as
a test fixture in each runtime (`ts/test/json-plugin.ts`,
`go/jsonplugin_test.go`) — the engine itself ships no grammar.

Both loaders (`ts/test/utility.js`, `go/spec_test.go`) must stay in step
on escape handling and on what counts as a row; a divergence there makes
the two runtimes read the same file differently, which is worse than not
sharing it at all.

## Rules

- **Every file here must have a runner.** Adding a `.tsv` is half the
  change; the runners take explicit filenames, not a directory glob, so
  an unregistered fixture is silently dead. Check it runs by breaking a
  row on purpose and confirming the suite goes red — **with
  `go test -count=1`**. Plain `go test` will lie to you here: the test
  cache keys a fixture read on the file's `stat`, not its contents, so a
  row edited within the filesystem's mtime granularity is served from
  cache and reports green having measured the OLD file. Measured in this
  repo: a same-size edit to `test/spec/divergent.tsv` with the mtime
  pinned stays `(cached)` even when the size changes too. The trap is
  precisely this verification step, which is why it is called out here
  and not left to be rediscovered.
- Prefer adding a fixture here over a one-off in-language assertion when
  a case is expressible as input → output **and** the engine alone can
  check it. If it needs a grammar, it belongs in that grammar's repo.
- A new case must pass in BOTH runtimes: run `go test ./...` (from `go/`)
  and `npm test` (from `ts/`) before considering it done.
- Keep `expected` JSON canonical (sorted-key-independent comparison is
  the loaders' job, but write it readably).

## The divergence register

`divergent.tsv` is not a conformance fixture. It records where the two
ports **disagree**, one column per runtime, and both suites assert their
own column — so a divergence that gets REPAIRED fails it as loudly as one
that regresses, and the row must then be deleted. That is the property
prose cannot have: `go/doc/differences.md` claimed things about `2.e3`
and `1e999` that had stopped being true, which is what ADR-14 is a
reaction to.

Three things about it differ from every other file here:

- **It has a `probe` column.** This engine ships no grammar, so there is
  nothing to parse an input against by default, and its divergences show
  up at several layers — token columns, token spans, decoded values,
  whether a grammar loads at all. The probe names which observation the
  row makes. The set is closed and shared: adding a probe means adding it
  to both runners, or the row cannot be asserted on both sides.
- **Rows render through a shared canonical form, never a runtime's own.**
  Go's `%q` and JavaScript's `JSON.stringify` escape different
  characters; the two runtimes sort strings by different units; and Go's
  `%v` disagrees with `String(number)` on ordinary values (`1e+20` vs
  `100000000000000000000`, `1e-07` vs `1e-7`), so the Go runner
  reimplements ECMAScript `Number::toString` and pins it against the real
  thing. All three traps were paid for in review. Values render verbatim,
  map keys sort by UTF-16 code unit, and numbers render as JavaScript
  writes them — so the renderer cannot manufacture a difference of its
  own.
- **Every group carries a control row** — an adjacent case where the
  ports AGREE. Without one, a repair to the divergent row and unrelated
  breakage look identical. A group must also keep at least one row where
  the two columns actually DIFFER; the gate fails otherwise, or a
  repaired divergence could keep its `DIVERGENCE.md` entry with only
  control rows left behind it.
- **A `lex` row selects one token from a comparable list.** The cap
  counts retained tokens, not `next()` calls, and `#SP` — which
  TypeScript emits between whitespace-separated tokens and Go does not —
  is dropped before it counts. Without both, an identical cap reached a
  different token in each port: measured, a target 40 tokens in was
  `NOT-FOUND` in TypeScript and found in Go. The cost is that no row here
  can address an `#SP` token, and whether that asymmetry is itself a
  divergence is an open question.

`go/divergent_test.go` also gates coverage: every `### ` heading in
`DIVERGENCE.md` must be either an `# @divergence:` group in the register
or an entry in that file's `notRegistered` map, with a reason and where
the entry IS pinned instead. An exemption is a declared gap, not an
excuse — the probe set is meant to grow until the map is empty.
