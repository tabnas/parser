# schema/ — machine-readable contract files

Cross-runtime contract files shared by the TypeScript (canonical) and Go
engines. One rule governs all of them: **only the error `code` is
contractual across runtimes** — message, hint and other rendered text are
informative and may differ (see `DIVERGENCE.md` "Not divergences" and
`AGENTS.md` "Error codes").

| File | What it is |
|---|---|
| [`diagnostic.schema.json`](diagnostic.schema.json) | JSON Schema (draft 2020-12) for the structured diagnostic a serialized parse error emits: TS `JSON.stringify(err)` via `TabnasError.toJSON`, Go `json.Marshal(err)` via `MarshalJSON`. Hand-maintained; pinned by the shared fixture `test/spec/diagnostic.tsv`. |
| [`grammar.schema.json`](grammar.schema.json) | JSON Schema (draft 2020-12) for the serialized `GrammarSpec` — the pure-JSON grammar form fed to `tn.grammar()` (TS) and `GrammarSpecFromJSON` (Go). Models the portable JSON form only: no `ref` (functions are not JSON), no Go-only `OptionsMap`/`RuleOrder`. Hand-maintained; drift gates below. |
| [`clib-artifacts-manifest.schema.json`](clib-artifacts-manifest.schema.json) | JSON Schema (draft 2020-12) for the per-release `manifest.json` the shared clib-release workflow attaches alongside prebuilt clib artifacts (ADR-12 clause 5 in `tabnas/admin`). Bindings' loaders parse it, so it is a contract: absence is explicit (`present:false`), and `sha256` is required — and must be verified — for every present artifact. Hand-maintained. |
| [`error-codes.json`](error-codes.json) | **Generated** registry of the engine's error codes with their message and hint templates: the ten base codes from the TS merged catalogues, plus the Go-only `internal` code under `goOnly`. Do not edit by hand. |

## Drift gates (`grammar.schema.json`)

The schema is hand-maintained, and two different kinds of test keep it
honest — they are not symmetrical, so know which is which:

- **Alt-key NAMES.** On the Go side the check is runtime-derived:
  `go/schema_test.go` reflects over the `GrammarAltSpec` struct
  (`go/grammarspec.go`) and compares its keys to the schema's
  `$defs.alt.properties`. On the TS side there is no literal key list in
  the runtime to reflect over, so `ts/test/schema.test.js` maintains the
  list by hand, pointing at the runtime's normalizers
  (`ts/src/rules.ts` `normalt`/`validateAlt`) and the `GrammarAltSpec`
  type (`ts/src/types.ts`) as its sources.
- **Key SHAPES.** The value forms (array-form `s` and `g`,
  `inject.clear`, rule-removal `null`) are enforced by the cross-runtime
  serialized-door tests — `go/grammarspec_json_test.go` (`TestFromJSON*`)
  and `ts/test/serialized-grammar.test.js` load the same pure-JSON specs
  and must accept/reject identically — not by reflection.

## Known emitter and runtime leniencies

The schema is deliberately strict; the engines and one real emitter are
looser in places. Recorded here so strict-validation failures can be told
apart from real bugs:

- **`m` alt key (dead, emitter-only).** Real `@tabnas/abnf` compiler
  output (e.g. the captured fixture `ts/test/probe-grammar.fixture.json`)
  carries an alt key `m` that both runtimes silently ignore. Strict
  validation rejects it, and the schema keeps rejecting it on purpose;
  cleaning that emitter is a follow-up in the abnf repo.
- **Nested-array `s`.** TS's normalizer happens to flatten nested arrays
  inside the array form of `s` (`rules.ts` tinsify). Undeclared leniency:
  the declared forms are `string` and `string[]` only, the schema does
  not admit nesting, and Go's JSON door does not implement it.
- **Non-integer `n` values.** TS tolerates float counter increments; Go's
  JSON door truncates them to int. Integers are the contract, and the
  schema says `integer`.

## Regeneration (`error-codes.json`)

```bash
cd ts && npm run build && npm run gen-registry
```

The generator (`ts/tools/gen-error-codes.js`) reads a default instance's
merged catalogues from the built dist and rewrites the file; commit the
result. Two tests keep it honest, so CI fails on drift:

- `ts/test/schema.test.js` regenerates the registry in memory and
  deep-compares it against the committed file (staleness gate).
- `go/schema_test.go` byte-compares every entry against the Go catalogues
  in `go/tabnas.go`, in both directions — including the `goOnly.internal`
  literal the generator carries.

The two `.schema.json` files are hand-maintained: edit them together with
the code they describe, and let the drift tests above (plus
`test/spec/diagnostic.tsv`) prove the edit landed on both sides.
