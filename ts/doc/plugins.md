# Writing Plugins

A plugin is how grammar (and tooling) gets into tabnas — the package
itself is grammar-free. A plugin adds tokens, registers matchers,
modifies rules, hooks events, or decorates the instance. This is a
how-to: for the full method and option signatures, follow the links
into the [API reference](api.md) and [options reference](options.md).

The strict-JSON fixture at [`test/json-plugin.ts`](../test/json-plugin.ts)
is a complete, non-trivial example to read alongside this guide.
Companion plugins (`@tabnas/bnf`, `@tabnas/debug`) live in their own
packages.

## Plugin Structure

A plugin is a function that receives a `Tabnas` instance and an
optional options object, and configures the instance:

```js
function myPlugin(tabnas, options) {
  // configure the instance here
}

const { Tabnas } = require('@tabnas/parser')
const tn = new Tabnas()
tn.use(myPlugin, { key: 'value' })
```

`use()` returns the instance, so the idiomatic way to assemble a parser
is to chain registrations off the constructor:

```js
const p = new Tabnas()
  .use(jsonGrammar)
  .use(csvGrammar, { delimiter: ';' })
  .use(debugPlugin)
```

Or pass plugins at construction time — same effect, applied in order:

```js
const p = new Tabnas({ plugins: [jsonGrammar, csvGrammar, debugPlugin] })
```

Plugins should be idempotent (or guard against re-application) because
`tn.make()` derives a child by re-running every plugin the parent has
registered, against the child's merged options. That re-run is what
makes option-conditional alternates (e.g. `list.child`) work — the
plugin's grammar registration sees the child's settings, not the
parent's.

### Grammar dependencies and order

Plugins are applied in registration order, and a grammar plugin may
build on tokens, rules, or token sets that an earlier plugin
registered. Order is therefore significant: **register a grammar's
dependencies before the grammar itself.** For example, a CSV grammar
that reuses a relaxed-JSON value grammar to parse each cell depends on
that grammar being installed first:

```js
const p = new Tabnas()
  .use(jsonic)   // dependency: provides the cell-value rules/tokens
  .use(csv)      // builds on what jsonic registered
```

A plugin can fail fast if a required dependency is missing — inspect the
instance (e.g. `tn.token('#…')` or the rule set) in its body and throw a
clear error rather than producing a confusing parse failure later.

### TypeScript signature

```ts
import type { Plugin, Tabnas } from '@tabnas/parser'

const myPlugin: Plugin = function myPlugin(tn: Tabnas, options?: any) {
  // …
}
// Optionally attach plugin-author defaults; they merge with any options
// passed to tn.use(myPlugin, opts).
myPlugin.defaults = { key: 'default' }
```

### Returning a wrapped instance

`tn.use(plugin)` returns whatever the plugin returns, falling back to
the instance. This lets a plugin wrap or proxy the instance:

```js
function wrapping(tn) {
  // Tabnas uses ES #private state; the wrapper Proxy must bind methods
  // to the underlying target so private-field access resolves to the
  // real instance.
  return new Proxy(tn, {
    get(target, prop) {
      const v = target[prop]
      return 'function' === typeof v ? v.bind(target) : v
    },
  })
}

const wrapped = tn.use(wrapping)
```

## Adding Tokens

Register a fixed token by name; the first lookup mints a new Tin (an
opaque token id):

```js
function tildePlugin(tn) {
  tn.options({ fixed: { token: { '#TL': '~' } } })
  const T_TILDE = tn.token('#TL')
}
```

Token names conventionally use `#XX` form. Standard tokens:

| Name | Src | Description |
|---|---|---|
| `#OB` | `{` | Open brace |
| `#CB` | `}` | Close brace |
| `#OS` | `[` | Open square |
| `#CS` | `]` | Close square |
| `#CL` | `:` | Colon |
| `#CA` | `,` | Comma |
| `#NR` | — | Number |
| `#ST` | — | String |
| `#TX` | — | Text (unquoted) |
| `#VL` | — | Value (keyword) |
| `#SP` | — | Space |
| `#LN` | — | Line |
| `#CM` | — | Comment |
| `#BD` | — | Bad (error) |
| `#ZZ` | — | End |
| `#AA` | — | Any (wildcard) |

## Modifying Rules

The parser drives off named rules, each with `open` and `close`
alternate lists. An alternate matches a short token pattern (up to two
tokens of lookahead) and fires actions.

```js
function myPlugin(tn) {
  tn.options({ fixed: { token: { '#TL': '~' } } })

  tn.rule('val', (rs) => {
    rs.open([
      { s: ['#TL'], a: (rule) => { rule.node = 42 } },
    ])
  })
}
```

`rs.open(alts, mods?)` and `rs.close(alts, mods?)` append to the
existing alternate list. Pass `mods` to control merge behaviour:

| Mod | Effect |
|---|---|
| `{ append: true }` | Append at the end (default). |
| `{ clear: true }` | Empty the existing alternates first, then add the new ones — a later plugin can replace a rule's alternates outright. |
| `{ delete: [i, …] }` | Remove the listed indices before appending. |
| `{ move: [from, to, …] }` | Reorder existing alternates. |

### Replacing rules and actions

By default a later plugin's alternates and lifecycle actions are
**appended** to earlier ones (see [State Actions](#state-actions)). To
instead replace what earlier plugins contributed:

- **Alternates** — pass `{ clear: true }` to `rs.open` / `rs.close`, or
  use `rs.clearOpen()` / `rs.clearClose()` then re-add:

  ```js
  tn.rule('val', (rs) => rs.open([{ s: ['#OB'], p: 'map' }], { clear: true }))
  ```

  Declaratively, the same via the alt-list `inject`:

  ```js
  tn.grammar({ rule: { val: { open: { alts: [...], inject: { clear: true } } } } })
  ```

- **Lifecycle actions** — `rs.clearActions('bo', 'ao', …)` (no args clears
  all four phases) removes earlier actions for those phases; then register
  fresh ones. Declaratively, append `/replace` to the funcref name:

  ```js
  // drops every previously-registered `map` before-open action, installs this one
  tn.grammar({ ref: { '@map-bo/replace': resetMap }, rule: { map: {} } })
  ```

`/replace` takes ownership of the phase: once a phase is replaced, plain
/ `/prepend` / `/append` funcrefs for it are ignored, and the replacement
wins deterministically across `tn.make()` re-derivation. All of this is
opt-in — existing grammars that use neither `clear` nor `/replace` keep
the append behavior unchanged.

### Alternate Spec Fields

| Field | Description |
|---|---|
| `s` | Token pattern to match — array of token-name strings (or arrays of names for OR-of-tokens), or a space-separated string. Up to two-token lookahead. |
| `a` | Action: `(rule, ctx) => void` (also accepts a `@funcref` string). |
| `p` | Push a new rule onto the stack by name (creates a child). |
| `r` | Replace the current rule with another by name (creates a sibling). |
| `b` | Backtrack: number of tokens to put back. |
| `g` | Group tag(s). Used by `rule.include` / `rule.exclude` filtering. |
| `c` | Condition: function returning true to allow the alt, or an object matched against `rule.n` counters. |
| `n` | Increment named counters by these amounts. |
| `u` | Custom data attached to the rule's `u` bag. |
| `k` | Custom data attached to `k` (propagates via push / replace). |
| `h` | Modifier: `(rule, ctx, alt, next) => alt`. |
| `e` | Error: `(rule, ctx, alt) => Token | undefined`. |

### State Actions

Each rule has four hook points:

| Hook | When |
|---|---|
| `bo` | Before-open — before open alternates are tried. |
| `ao` | After-open — after an open alternate matches. |
| `bc` | Before-close — before close alternates are tried. |
| `ac` | After-close — after a close alternate matches. |

Register via the chainable API:

```js
tn.rule('map', (rs) => {
  rs.bo((rule, ctx) => {
    // runs once per map rule, before its open phase
  })
})
```

## Custom Matchers

For syntax that doesn't fit the built-in matchers, add a custom lexer
matcher via the `match` option. A regex value is the simplest form:

```js
const tn = new Tabnas({
  plugins: [myGrammarPlugin],
  match: {
    lex: true,
    value: {
      date: { match: /^\d{4}-\d{2}-\d{2}/, val: (m) => new Date(m[0]) },
    },
  },
})

tn.parse('2024-01-15')              // Date(2024-01-15)
```

The regex must be anchored with `^`. For full control, register a
matcher function under `match.token`, or build one on the shared
scan-spec primitives exposed via `Tabnas.util` (`scan`,
`guardedMatcher`, `buildCharRunSpec`, …) — see the
[utility reference](api.md#tabnasutil-static) and the matchers in
`src/lexer.ts`.

## Subscribing to Events

A plugin can observe the parse without modifying it:

```js
function loggingPlugin(tn) {
  tn.sub({
    lex: (token, rule, ctx) => { console.log('lexed:', token.toString()) },
    rule: (rule, ctx) => { console.log('rule:', rule.name, rule.state) },
  })
}
```

## Token Sets

Access groups of tokens by name (callable or as a map):

```js
const ignoreTins = tn.tokenSet('IGNORE')   // [#SP, #LN, #CM]
const { VAL, KEY } = tn.tokenSet           // map form
```

Define your own with the `tokenSet` option:

```js
tn.options({ tokenSet: { MYSET: ['#TX', '#NR'] } })
```

## Declarative grammar

Instead of registering rules imperatively, a plugin can describe them
as data via `tn.grammar(spec)`. Function fields are supplied as
`@funcref` strings resolved against `spec.ref`. This is how the
strict-JSON fixture is written — see
[`test/json-plugin.ts`](../test/json-plugin.ts) and
[`tn.grammar`](api.md#tngrammarspec-settings).

## Example: a tiny CSV plugin

Build on the bare engine — a CSV grammar replaces the standard rules
entirely. A complete, runnable version of this grammar (with the
`csvcont` / `rowcont` continuations filled in) lives in
[`test/csv-grammar.test.js`](../test/csv-grammar.test.js); the plugin
entry points shown above — `use`, custom tokens, rule modification,
custom matchers, and `sub` — are unit-tested in
[`test/plugin.test.js`](../test/plugin.test.js).

```js
function csvPlugin(tn, opts) {
  // Drop newlines from IGNORE so they survive into the rule stream.
  tn.options({
    tokenSet: { IGNORE: [undefined, null, undefined] },
    rule: { start: 'csv' },
    lex: { emptyResult: [] },
  })

  const { CA, LN, ZZ } = tn.token
  const { VAL } = tn.tokenSet

  tn.rule('csv', (rs) => rs
    .bo((r) => { r.node = [] })
    .open([
      { s: [VAL], p: 'row', b: 1 },
      { s: [LN], r: 'csv' },
      { s: [ZZ] },
    ])
  )

  tn.rule('row', (rs) => rs
    .bo((r) => { r.node = [] })
    .open([
      { s: [VAL], a: (r) => r.node.push(r.o0.val) },
    ])
    .close([
      { s: [CA], r: 'rowcont' },
      { s: [LN], b: 1 },
      { s: [ZZ], b: 1 },
    ])
  )

  // ... rowcont continuation here
}
```

Apply with `tn.use(csvPlugin)` on a bare instance, or via the
`plugins` array at construction time.

## Packaging

If you ship a plugin as its own npm package, the convention is
`tabnas-<name>` or `@<scope>/tabnas-<name>` (as the companion
`@tabnas/bnf` and `@tabnas/debug` packages do). The package itself
holds no grammar — yours is the grammar.
