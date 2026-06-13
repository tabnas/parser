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

const { Tabnas } = require('tabnas')
const am = new Tabnas()
am.use(myPlugin, { key: 'value' })
```

Pass plugins at construction time too, applied in order:

```js
const am = new Tabnas({ plugins: [myPlugin, anotherPlugin] })
```

Plugins should be idempotent (or guard against re-application) because
`am.make()` derives a child by re-running every plugin the parent has
registered, against the child's merged options. That re-run is what
makes option-conditional alternates (e.g. `list.child`) work — the
plugin's grammar registration sees the child's settings, not the
parent's.

### TypeScript signature

```ts
import type { Plugin, Tabnas } from 'tabnas'

const myPlugin: Plugin = function myPlugin(am: Tabnas, options?: any) {
  // …
}
// Optionally attach plugin-author defaults; they merge with any options
// passed to am.use(myPlugin, opts).
myPlugin.defaults = { key: 'default' }
```

### Returning a wrapped instance

`am.use(plugin)` returns whatever the plugin returns, falling back to
the instance. This lets a plugin wrap or proxy the instance:

```js
function wrapping(am) {
  // Tabnas uses ES #private state; the wrapper Proxy must bind methods
  // to the underlying target so private-field access resolves to the
  // real instance.
  return new Proxy(am, {
    get(target, prop) {
      const v = target[prop]
      return 'function' === typeof v ? v.bind(target) : v
    },
  })
}

const wrapped = am.use(wrapping)
```

## Adding Tokens

Register a fixed token by name; the first lookup mints a new Tin (an
opaque token id):

```js
function tildePlugin(am) {
  am.options({ fixed: { token: { '#TL': '~' } } })
  const T_TILDE = am.token('#TL')
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
function myPlugin(am) {
  am.options({ fixed: { token: { '#TL': '~' } } })

  am.rule('val', (rs) => {
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
| `{ delete: [i, …] }` | Remove the listed indices before appending. |
| `{ move: [from, to, …] }` | Reorder existing alternates. |

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
am.rule('map', (rs) => {
  rs.bo((rule, ctx) => {
    // runs once per map rule, before its open phase
  })
})
```

## Custom Matchers

For syntax that doesn't fit the built-in matchers, add a custom lexer
matcher via the `match` option. A regex value is the simplest form:

```js
const am = new Tabnas({
  plugins: [myGrammarPlugin],
  match: {
    lex: true,
    value: {
      date: { match: /^\d{4}-\d{2}-\d{2}/, val: (m) => new Date(m[0]) },
    },
  },
})

am.parse('2024-01-15')              // Date(2024-01-15)
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
function loggingPlugin(am) {
  am.sub({
    lex: (token, rule, ctx) => { console.log('lexed:', token.toString()) },
    rule: (rule, ctx) => { console.log('rule:', rule.name, rule.state) },
  })
}
```

## Token Sets

Access groups of tokens by name (callable or as a map):

```js
const ignoreTins = am.tokenSet('IGNORE')   // [#SP, #LN, #CM]
const { VAL, KEY } = am.tokenSet           // map form
```

Define your own with the `tokenSet` option:

```js
am.options({ tokenSet: { MYSET: ['#TX', '#NR'] } })
```

## Declarative grammar

Instead of registering rules imperatively, a plugin can describe them
as data via `am.grammar(spec)`. Function fields are supplied as
`@funcref` strings resolved against `spec.ref`. This is how the
strict-JSON fixture is written — see
[`test/json-plugin.ts`](../test/json-plugin.ts) and
[`am.grammar`](api.md#amgrammarspec-settings).

## Example: a tiny CSV plugin

Build on the bare engine — a CSV grammar replaces the standard rules
entirely. A complete, runnable version of this grammar (with the
`csvcont` / `rowcont` continuations filled in) lives in
[`test/csv-grammar.test.js`](../test/csv-grammar.test.js); the plugin
entry points shown above — `use`, custom tokens, rule modification,
custom matchers, and `sub` — are unit-tested in
[`test/plugin.test.js`](../test/plugin.test.js).

```js
function csvPlugin(am, opts) {
  // Drop newlines from IGNORE so they survive into the rule stream.
  am.options({
    tokenSet: { IGNORE: [undefined, null, undefined] },
    rule: { start: 'csv' },
    lex: { emptyResult: [] },
  })

  const { CA, LN, ZZ } = am.token
  const { VAL } = am.tokenSet

  am.rule('csv', (rs) => rs
    .bo((r) => { r.node = [] })
    .open([
      { s: [VAL], p: 'row', b: 1 },
      { s: [LN], r: 'csv' },
      { s: [ZZ] },
    ])
  )

  am.rule('row', (rs) => rs
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

Apply with `am.use(csvPlugin)` on a bare instance, or via the
`plugins` array at construction time.

## Packaging

If you ship a plugin as its own npm package, the convention is
`tabnas-<name>` or `@<scope>/tabnas-<name>` (as the companion
`@tabnas/bnf` and `@tabnas/debug` packages do). The package itself
holds no grammar — yours is the grammar.
