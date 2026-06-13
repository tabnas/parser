# API Reference

tabnas exposes a single class — `Tabnas` — plus error and lexer
helpers. The package ships **no grammar** of its own; every grammar
arrives via a plugin.

## Construction

### `new Tabnas(options?)`

Create a parser instance. The class has no grammar by default; pass a
`plugins` array to apply one (or more) at construction time:

```js
const { Tabnas } = require('tabnas')

const am = new Tabnas({ plugins: [myGrammarPlugin] })
am.parse(src)
```

`options` is a [`TabnasOptions`](options.md) object — every field is
optional and merges with the defaults. The `plugins` field is the only
field that doesn't survive into `am.options` after construction (it's
consumed by the `use()` calls the constructor makes internally).

For a bare instance with no defaults, no standard tokens, and no
grammar — useful as a base for building a parser from scratch — use
`am.empty()`:

```js
const blank = am.empty({ rule: { start: 'mything' } })
```

## Parsing

### `am.parse(src, meta?, parent_ctx?)`

Parse a string and return the result.

```js
am.parse(src)                         // depends on the active grammar
```

Non-string inputs are returned unchanged — handy when threading values
through plugin pipelines.

`meta` passes arbitrary data to plugins and rule actions (read off
`ctx.meta` inside actions). `parent_ctx` seeds the per-parse `Context`
with extra fields — used by the test harness; rarely needed in user
code.

## Instance Management

### `am.make(options?)`

Derive a child instance with overridden options. The child inherits the
parent's plugin list and re-runs each plugin against the merged options
— so option-conditional grammar alternates get re-evaluated against the
child's settings.

```js
const restricted = am.make({
  rule: { exclude: 'experimental' }   // strip alts tagged 'experimental'
})
```

After re-running plugins the child applies `rule.include` /
`rule.exclude` filtering on top.

### `am.empty(options?)`

Create a bare instance with `defaults$: false`, `standard$: false`,
`grammar$: false`. No tokens, no rules, no anything. The starting
point for entirely custom parsers.

## Configuration

### `am.options`

Dual-shape: callable as a setter, indexable as a snapshot of the
merged option tree. Both forms work simultaneously.

```js
am.options.comment.lex                // current setting (snapshot)
am.options({ comment: { lex: false } })   // applies a partial change
am.options()                          // returns a fresh copy of merged opts
```

Calling `options(change)` deep-merges into the live options, re-runs
`configure()`, and clones the parser with the new config. Reading
property paths gives the snapshot at the most recent set call.

### `am.config()`

Returns a deep copy of the internal `Config` (the resolved, compiled
form of the options). Useful for debugging.

## Grammar

### `am.rule(name?, definer?)`

Access or modify grammar rules.

- `am.rule()` — returns the full `RuleSpec` map.
- `am.rule(name)` — returns the `RuleSpec` for that rule name.
- `am.rule(name, definer)` — calls `definer(rs, parser)` to modify or
  create the rule. Use `rs.open([...])` / `rs.close([...])` to add
  alternates, and `bo` / `ao` / `bc` / `ac` for the state-action hooks.
- `am.rule(name, null)` — delete a rule.

```js
am.rule('val', (rs) => {
  rs.open([
    { s: ['#OB'], p: 'map', b: 1, g: 'map,custom' },
  ])
})
```

See [Writing plugins](plugins.md) for the full alternate-spec and
state-action field lists.

### `am.grammar(spec, settings?)`

Apply a `GrammarSpec` — a JSON-shaped declarative representation of
rule definitions, with function fields supplied as `@funcref` strings
resolved against `spec.ref`. Used by plugins that ship grammar as data
rather than code. `settings.rule.alt.g` appends group tags to every
alternate in the spec.

### `am.token(ref)`

Dual-shape, like `options`: callable for lookup-or-create, and
indexable as a name → Tin map.

```js
am.token('#OB')                       // Tin for the open-brace token
am.token(12)                          // name for Tin 12
am.token('#TL')                       // mints and returns a fresh Tin
am.token.ST                           // map access (bare name, no '#')
```

The map is keyed by both `#XX` and bare `XX` forms.

### `am.tokenSet(ref)`

Dual-shape: callable to look up a named set's Tin array, indexable as
a name → Tin[] map. Built-in sets:

| Name | Members |
|---|---|
| `IGNORE` | `#SP`, `#LN`, `#CM` (skipped during lex) |
| `VAL` | `#TX`, `#NR`, `#ST`, `#VL` (anything that can be a value) |
| `KEY` | `#TX`, `#NR`, `#ST`, `#VL` (anything that can be a map key) |

### `am.fixed(ref)`

Look up the source-string ↔ Tin mapping for fixed (punctuation /
keyword) tokens.

## Plugins

### `am.use(plugin, options?)`

Register a plugin and invoke it. The plugin receives the instance and
the merged plugin options:

```js
function foo(am, opts) { /* … */ }
am.use(foo, { x: 1 })
```

Plugins can return a wrapped instance (e.g. a `Proxy`) — `use()` will
return whatever the plugin returns, falling back to the instance:

```js
const wrapped = am.use((am) => new Proxy(am, {
  // Tabnas uses ES #private state; bind methods to the underlying
  // instance so private-field access works through the Proxy.
  get(target, prop) {
    const v = target[prop]
    return 'function' === typeof v ? v.bind(target) : v
  }
}))
```

When `am.make()` derives a child, the child re-runs every plugin
applied to the parent. Plugins should be idempotent (or guard
themselves against re-application).

## Events

### `am.sub({ lex?, rule? })`

Subscribe to lex and rule events. Multiple subscriptions are allowed
and fire in registration order.

```js
am.sub({
  lex: (token, rule, ctx) => { /* … */ },
  rule: (rule, ctx)       => { /* … */ },
})
```

## Identity

| Property | Description |
|---|---|
| `am.id` | Unique-per-instance string id (`'Tabnas/<ts>/<rand>[/<tag>]'`). |
| `am.parent` | Parent instance, if this was created via `parent.make()`. |
| `am.toString()` | Returns `am.id`. |

## Internals

### `am.internal()`

Returns the internal-state record: `{ parser, config, plugins, sub,
mark, merged }`. The state itself lives in a hash-private field
(`#internal`); this method is the only public reader. Plugins use it
for things the public API doesn't surface; user code rarely needs it.

## Utilities

### `Tabnas.util` (static)

Bag of helpers for plugin authors. Also reachable per-instance via
`am.util`. Members:

- **Object / merge** — `deep`, `clone`, `keys`, `values`, `entries`,
  `omap`, `clean`, `prop`.
- **Regex / text** — `regexp`, `escre`, `charset`, `mesc`, `srcfmt`,
  `str`, `tokenize`.
- **Config** — `configure`, `parserwrap`, `badlex`, `makelog`.
- **Error** — `errdesc`, `errinject`, `errmsg`, `errsite`,
  `strinject`, `trimstk`.
- **Lex scan primitives** — `scan`, `guardedMatcher`,
  `buildCharRunSpec`, `buildLineRunSpec`, `buildStringBodySpec`, and
  the scan-spec constants `CONSUME`, `IS_ROW`, `CI_RESET`, `STOP`,
  `STATE_MASK`. These drive the table-driven matcher state machine;
  plugin authors writing custom matchers can reuse them. See the
  matchers in `src/lexer.ts` for usage.

### Constants

`OPEN`, `CLOSE`, `BEFORE`, `AFTER`, `EMPTY`, `SKIP`, `S` — exported as
both named exports and `Tabnas.X` static members. Used in rule
definitions and state actions. `SKIP` is the deep-merge sentinel that
preserves the base value.

### Lexer factories

Lower-level matcher and machinery factories, rarely needed by users
but exported for advanced plugin authors and the engine itself:

`makeLex`, `makeParser`, `makeToken`, `makePoint`, `makeRule`,
`makeRuleSpec`, `makeFixedMatcher`, `makeSpaceMatcher`,
`makeLineMatcher`, `makeStringMatcher`, `makeCommentMatcher`,
`makeNumberMatcher`, `makeTextMatcher`.

## Types

The TypeScript types live in `src/types.ts` and are re-exported from
the main module. Notable shapes:

| Type | Description |
|---|---|
| `Tabnas` | Class type of the engine instance. |
| `TabnasOptions` | The full option shape (with `plugins?: Plugin[]`). |
| `TabnasInternal` | Shape returned by `am.internal()`. |
| `Plugin` | `(tabnas, options?) => void \| Tabnas`, plus optional `defaults`. |
| `Tin` | Branded `number` for token ids. |
| `Token`, `Lex`, `Point` | Lexer types (also classes). |
| `Rule`, `RuleSpec`, `AltMatch`, `AltSpec` | Parser types. |
| `Config` | Internal compiled form of `TabnasOptions`. |
| `Context` | Per-parse state passed to rule actions and matchers. |
| `GrammarSpec`, `GrammarAltSpec` | Declarative-grammar JSON shapes. |
| `ScanSpec`, `ScanOut` | Scan-spec driver types for custom matchers. |

## Error Handling

A parse failure throws a `TabnasError` (extends `SyntaxError`). Its
enumerable fields:

| Property | Description |
|---|---|
| `code` | Error code (`'unexpected'`, `'unterminated_string'`, …). |
| `message` | Formatted, multi-line message including a source-context extract. |
| `details` | Structured details (e.g. `{ state: 'open' }`); may be empty. |
| `lineNumber` | Row of the offending token (1-based). |
| `columnNumber` | Column of the offending token (1-based). |
| `fileName` | From `meta.fileName`, if supplied to `parse()`. |
| `meta` | The `meta` passed to `parse()`. |
| `txts()` | Returns the resolved `{ msg, hint, site }` text parts. |

Customise messages and hints through the [`error` and `hint`
options](options.md#error). Error-code keys come from
`src/defaults.ts`.

## Module Exports

```js
const {
  Tabnas,            // the engine class
  TabnasError,       // error class (extends SyntaxError)

  // Step / state constants and the string-table
  OPEN, CLOSE, BEFORE, AFTER, EMPTY, SKIP, S,

  // Lower-level factories (rarely needed by users)
  makeLex, makeParser,
  makeToken, makePoint, makeRule, makeRuleSpec,
  makeFixedMatcher, makeSpaceMatcher, makeLineMatcher,
  makeStringMatcher, makeCommentMatcher, makeNumberMatcher, makeTextMatcher,

  // Utility bag (also Tabnas.util)
  util,
} = require('tabnas')
```

The package also exposes subpath exports for direct access to internal
modules: `tabnas/lexer`, `tabnas/utility`, and `tabnas/error`.

There is no bundled grammar export. Companion grammars and tooling are
separate packages (`@tabnas/bnf`, `@tabnas/debug`); the strict-JSON
grammar used by the tests is the fixture at `test/json-plugin.ts`.
