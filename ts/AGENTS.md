# Agents Guide — tabnas (TypeScript)

This is the **canonical** implementation. tabnas comes from the
jsonic use case — lenient JSON for humans (unquoted keys, implicit
objects/arrays, comments, trailing commas, path diving) — but this
package itself ships **no grammar**: it is the engine (lexer, parser,
rule machinery) and grammars are plugins. The strict-JSON grammar used
by the conformance tests is a test fixture at `test/json-plugin.ts`;
the lenient-JSON grammar ships built into the [Go port](../go/).

## Layout

- `src/tabnas.ts` — the `Tabnas` class (public API).
- `src/lexer.ts` — matcher-based lexer; declarative scan-spec design
  (`ScanSpec`, `scan()` driver, `guardedMatcher`); scan primitives are
  exported via the util bag for plugin authors.
- `src/rules.ts`, `src/parser.ts`, `src/context.ts` — rule machinery.
- `src/defaults.ts` — the option tree defaults (error/hint texts,
  matcher registry, token definitions). The Go port mirrors these.
- `src/error.ts` — `TabnasError`, `errmsg`/`errsite` formatting,
  `strinject` `{key}` template injection. Subpath export `./error`.
- `test/json-plugin.ts` — strict-JSON grammar fixture (worked example
  of a non-trivial grammar plugin).

## Commands

```bash
npm install
npm run build        # tsc --build src test (emits dist/ and dist-test/)
npm test             # node --test test/**/*.test.js
TEST_PATTERN=name npm run test-some
node --test --experimental-test-coverage test/**/*.test.js
```

Tests run against the compiled output — always `npm run build` after
editing `src/` or `test/*.ts`.

## Documentation

The docs follow a strict four-purpose split — keep each file to ONE
purpose, never mix them:

- `doc/tutorial.md` — learning: one guided happy path, no options dumps.
- `doc/guide.md` — task recipes ("how to X"), short and focused.
- `doc/api.md`, `doc/options.md` — reference: dry, complete, no teaching.
- `doc/concepts.md` — explanation: the TS-specific engine model and
  rationale; links to the shared `../../doc/architecture.md`.
- `doc/plugins.md` — the plugin-authoring how-to.
- `doc/lsp-feasibility.md`, `doc/gbnf-feasibility.md` — design-note
  explanations.

`README.md` is an **orientation hub**: what the package is, install,
one tiny example, and links out. Do not let it grow into a manual —
new detail belongs in the relevant doc above. Ground every factual
claim against `src/` and `package.json` before writing.

## Rules of the road

**`n` and `k` propagate to child rules; `u` does not.** `k` is named for
"keep" — its content is kept as the parse descends. `ts/src/rules.ts:662-671`
(push) and `:686-695` (replace) copy `rawn()` and `rawk()` into the new
rule; `rawu()` (`:94`) is never copied. `k` is also rule-scoped rather
than alternate-scoped: `rule.k = Object.assign(rule.k, alt.k)` at `:605`
runs before the alt action, so it accumulates across alternates and then
descends. Put per-rule scratch in `u` (as `@key$` does,
`ts/src/builtins.ts:264`), and anything that must reach child rules in
`k`. See the root [`AGENTS.md`](../AGENTS.md) section "Rule state" for the
full statement — it is contract, not implementation detail.


- Behavior changes here are changes to the spec: the Go port
  (`../go/`) must follow. Either port in the same change or record the
  gap in `../go/doc/differences.md`.
- Shared fixtures live in `../test/spec/`; `test/json-spec.test.js`
  runs the strict-JSON ones (`include-json*.tsv`) through the
  json-plugin, and `test/utility.test.js` runs the `utility-*.tsv`
  ones. Prefer adding a shared fixture over a one-off assertion when
  the case is expressible as input → output.
- Companion plugins (`@tabnas/abnf`, `@tabnas/debug`) live in separate
  repos — don't reintroduce grammar or debug tooling into this
  package.
