# API Reference

amagama exposes a single class — `Amagama` — plus a handful of plugins
that add grammars and tooling.

## Construction

### `new Amagama(options?)`

Create a parser instance. The class has no grammar by default; pass a
`plugins` array to load one (or more) at construction time:

```js
const { Amagama, json } = require('amagama')

const am = new Amagama({ plugins: [json] })
am.parse('{"a":1}')                   // {"a": 1}
am.parse('{a:1}')                     // throws — JSON-strict
```

`options` is an [`AmagamaOptions`](options.md) object — every field is
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
am.parse('a:1')                       // {"a": 1}
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
— so option-conditional grammar alternatives (e.g. `list.child`) get
re-evaluated against the child's settings.

```js
const json_only = am.make({
  rule: { include: 'json' }           // strip amagama-tagged alts
})
```

After re-running plugins the child applies `rule.include` /
`rule.exclude` filtering on top, matching the original
`parser.clone()` semantics.

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

```js
am.rule('val', (rs) => {
  rs.open([
    { s: ['#OB'], p: 'map', b: 1, g: 'map,custom' },
  ])
})
```

### `am.token(ref)`

Look up a token's name from its Tin, or its Tin from its name. Creates
a new Tin if the name is previously unseen:

```js
am.token('#OB')                       // 12 (or whatever the assignment was)
am.token(12)                          // '#OB'
am.token('#TL')                       // creates and returns a fresh Tin
```

### `am.tokenSet(ref)`

Look up a named token set. Built-in sets:

| Name | Members |
|---|---|
| `IGNORE` | `#SP`, `#LN`, `#CM` (skipped during lex) |
| `VAL` | `#TX`, `#NR`, `#ST`, `#VL` (anything that can be a value) |
| `KEY` | `#TX`, `#NR`, `#ST`, `#VL` (anything that can be a map key) |

### `am.fixed(ref)`

Lookup the source string ↔ Tin mapping for fixed (punctuation /
keyword) tokens.

### `am.grammar(spec, settings?)`

Apply a [`GrammarSpec`](#grammarspec) — a JSON-shaped declarative
representation of rule definitions. Used by the BNF plugin and by
plugins that ship grammar as data rather than code.

## Plugins

### `am.use(plugin, options?)`

Register a plugin and invoke it. The plugin receives the instance and
the merged plugin options:

```js
function foo(am, opts) { /* …​ */ }
am.use(foo, { x: 1 })
```

Plugins can return a wrapped instance (e.g. a `Proxy`) — `use()` will
return whatever the plugin returns, falling back to the instance:

```js
const wrapped = am.use((am) => new Proxy(am, {
  // Amagama uses ES #private state; bind methods to the underlying
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

Subscribe to lex and rule events. Multiple subscriptions are allowed.

```js
am.sub({
  lex: (token, rule, ctx) => { /* … */ },
  rule: (rule, ctx)       => { /* … */ },
})
```

## Identity

| Property | Description |
|---|---|
| `am.id` | Unique-per-instance string id (`'Amagama/<ts>/<rand>[/<tag>]'`). |
| `am.parent` | Parent instance, if this was created via `parent.make()`. |
| `am.toString()` | Returns `am.id`. |

## Internals

### `am.internal()`

Returns the internal-state record: `{ parser, config, plugins, sub,
mark, merged }`. The state itself lives in a hash-private field
(`#internal`); this method is the only public reader. Plugins use it
for things the public API doesn't surface; user code rarely needs it.

## Utilities

### `Amagama.util` (static)

Bag of helpers for plugin authors:

- `tokenize` — convert token names to Tin numbers.
- `deep`, `clone` — deep merge / clone.
- `regexp`, `escre` — safe regex construction.
- `srcfmt` — format source strings for display.
- `charset` — build character sets.
- `errmsg`, `strinject` — error-message helpers.
- `prop`, `keys`, `values`, `entries`, `omap`, `clean` — object utilities.
- `trimstk`, `makelog`, `str`, `mesc` — misc helpers.

### Constants

`OPEN`, `CLOSE`, `BEFORE`, `AFTER`, `EMPTY`, `SKIP` — exported as both
named exports and `Amagama.X` static members. Used in rule definitions
and state actions.

## Plugins shipped with this package

| Module path | Purpose |
|---|---|
| `amagama` | re-exports `json`, `bnf`, `Debug` for ergonomic destructuring. |
| `amagama/dist/plugins/json` | Strict JSON grammar (`json` plugin). |
| `amagama/dist/plugins/bnf` | BNF → grammar plugin + `bnfConvert` / `parseBnf` / `BnfParseError` exports. |
| `amagama/dist/plugins/debug` | Debug plugin (`Debug`) + tracing hooks. |

For plugin authors writing plugins of their own, see
[plugins.md](plugins.md).

## Types

The TypeScript types are kept in `src/types.ts` and re-exported via the
main module. Notable shapes:

| Type | Description |
|---|---|
| `Amagama` | Class type of the engine instance. |
| `AmagamaOptions` | The full option shape (with `plugins?: Plugin[]`). |
| `AmagamaInternal` | Shape returned by `am.internal()`. |
| `Plugin` | `(amagama, options?) => void \| Amagama`, plus optional `defaults`. |
| `Tin` | Branded `number` for token ids. |
| `Token`, `Lex`, `Point` | Lexer types (also classes). |
| `Rule`, `RuleSpec`, `AltMatch`, `AltSpec` | Parser types. |
| `Config` | Internal compiled form of `AmagamaOptions`. |
| `Context` | Per-parse state passed to rule actions and matchers. |
| `GrammarSpec`, `GrammarAltSpec` | Declarative-grammar JSON shapes. |
| `BnfConvertOptions` | BNF plugin's optional input. |

## Error Handling

Parse failures throw an `AmagamaError`:

| Property | Description |
|---|---|
| `code` | Error code (`'unexpected'`, `'unterminated_string'`, …). |
| `message` | Formatted multi-line error including source context. |
| `details` | Structured details (optional). |
| `token` | The token that caused the error (with `sI` / `rI` / `cI` location info). |

## Module Exports

```js
const {
  Amagama,            // the engine class
  AmagamaError,       // error class

  // Grammar plugin
  json,

  // Other plugins
  bnf,
  Debug,

  // Step / state constants
  OPEN, CLOSE, BEFORE, AFTER, EMPTY, SKIP, S,

  // Lower-level factories (rarely needed by users)
  makeLex, makeParser,
  makeToken, makePoint, makeRule, makeRuleSpec,
  makeFixedMatcher, makeSpaceMatcher, makeLineMatcher,
  makeStringMatcher, makeCommentMatcher, makeNumberMatcher, makeTextMatcher,

  // Utility bag (also Amagama.util)
  util,
} = require('amagama')
```
