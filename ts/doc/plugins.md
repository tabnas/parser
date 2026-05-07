# Writing Plugins

Plugins extend amagama by modifying the grammar, adding new token
types, registering custom matchers, or subscribing to parse events.
Most of the package itself is plugins — `json`, `jsonic`, `bnf`, and
`Debug` all ship as plugins under `src/plugins/<name>/`.

## Plugin Structure

A plugin is a function that receives an `Amagama` instance and an
optional options object. The function does whatever work it wants
against the instance:

```js
function myPlugin(amagama, options) {
  // Modify the parser here
}

const { Amagama, jsonic } = require('amagama')
const am = new Amagama({ plugins: [jsonic] })
am.use(myPlugin, { key: 'value' })
```

You can pass plugins at construction time too, in order:

```js
const am = new Amagama({ plugins: [jsonic, myPlugin] })
```

Plugins should be idempotent (or guard against re-application) because
`am.make()` derives a child by re-running every plugin the parent has
registered, against the child's merged options. That re-run is what
makes option-conditional alternates (e.g. `list.child`) work — the
plugin's grammar registration sees the child's settings, not the
parent's.

### TypeScript signature

```ts
import type { Plugin, Amagama } from 'amagama'

const myPlugin: Plugin = function myPlugin(am: Amagama, options?: any) {
  // …
}
// Optionally attach plugin-author defaults; they merge with any options
// passed to am.use(myPlugin, opts).
myPlugin.defaults = { key: 'default' }
```

### Returning a wrapped instance

`am.use(plugin)` returns whatever the plugin returns, falling back to
the instance. This lets plugins wrap or proxy the instance:

```js
function wrapping(am) {
  // Amagama uses ES #private state; the wrapper Proxy must bind
  // methods to the underlying target so private-field access resolves
  // to the real instance.
  return new Proxy(am, {
    get(target, prop) {
      const v = target[prop]
      return 'function' === typeof v ? v.bind(target) : v
    },
  })
}

const wrapped = am.use(wrapping)
wrapped.parse('a:1')              // works
```

## Adding Tokens

Register a fresh token by name. The first lookup mints a new Tin (an
opaque token id):

```js
function tildePlugin(am) {
  am.options({ fixed: { token: { '#TL': '~' } } })
  const T_TILDE = am.token('#TL')
}
```

Token names conventionally use `#XX` form. Built-in tokens:

| Name | Src | Description |
|---|---|---|
| `#OB` | `{` | Open brace |
| `#CB` | `}` | Close brace |
| `#OS` | `[` | Open square |
| `#CS` | `]` | Close square |
| `#CL` | `:` | Colon |
| `#CA` | `,` | Comma |
| `#NR` | -- | Number |
| `#ST` | -- | String |
| `#TX` | -- | Text (unquoted) |
| `#VL` | -- | Value (keyword) |
| `#SP` | -- | Space |
| `#LN` | -- | Line |
| `#CM` | -- | Comment |
| `#BD` | -- | Bad (error) |
| `#ZZ` | -- | End |
| `#AA` | -- | Any (wildcard) |

## Modifying Rules

The parser drives off named rules, each with `open` and `close`
alternate lists. Alternates match token patterns and fire actions.

```js
function myPlugin(am) {
  am.options({ fixed: { token: { '#TL': '~' } } })

  am.rule('val', (rs) => {
    rs.open([
      {
        s: ['#TL'],
        a: (rule) => { rule.node = 42 }
      }
    ])
  })
}
```

`rs.open(alts, mods?)` and `rs.close(alts, mods?)` append to the
existing alternate list. Pass `mods` to control merge behaviour:

| Mod | Effect |
|---|---|
| `{ append: true }` | Append at the end (default). |
| `{ delete: [i, …] }` | Remove the listed indices from the existing alts before appending. |
| `{ move: [from, to] }` | Reorder existing alts. |

### Alternate Spec Fields

| Field | Description |
|---|---|
| `s` | Token pattern to match — array of token-name strings (or arrays of names for OR-of-tokens). Up to two-token lookahead. |
| `a` | Action: `(rule, ctx) => void` (also accepts a `@funcref` string). |
| `p` | Push a new rule onto the stack by name (creates a child). |
| `r` | Replace current rule with another by name (creates a sibling). |
| `b` | Backtrack: number of tokens to put back. |
| `g` | Group tag string (e.g. `'json'`, `'amagama,map'`). Used by `rule.include` / `rule.exclude` filtering. |
| `c` | Condition: function that returns true to allow the alt, or an object pattern to match against `rule.n` counters. |
| `n` | Increment named counters by these amounts. |
| `u` | Custom data attached to the rule's `u` bag. |
| `k` | Custom data attached to `k` (propagates via push / replace). |
| `h` | Modifier: `(rule, ctx, alt, next) => alt`. |
| `e` | Error: `(rule, ctx, alt) => Token | undefined`. Returns a token to record an error. |

### State Actions

Each rule spec has four hook points:

| Hook | When |
|---|---|
| `bo` | Before-open — runs before open alternates are tried. |
| `ao` | After-open — runs after an open alternate matches. |
| `bc` | Before-close — runs before close alternates are tried. |
| `ac` | After-close — runs after a close alternate matches. |

Register via the chainable API:

```js
am.rule('map', (rs) => {
  rs.bo((rule, ctx) => {
    // … runs once per map rule, before its open phase
  })
})
```

## Custom Matchers

For syntax that doesn't fit the built-in matchers, add a custom lexer
matcher via the `match` option:

```js
const am = new Amagama({
  plugins: [jsonic],
  match: {
    lex: true,
    value: {
      date: {
        match: /^\d{4}-\d{2}-\d{2}/,
        val: (m) => new Date(m[0])
      }
    }
  }
})

am.parse('d: 2024-01-15')           // { d: Date('2024-01-15') }
```

## Subscribing to Events

Plugins can observe the parse without modifying it:

```js
function loggingPlugin(am) {
  am.sub({
    lex: (token, rule, ctx) => {
      console.log('lexed:', token.toString())
    },
    rule: (rule, ctx) => {
      console.log('rule:', rule.name, rule.state)
    }
  })
}
```

## Token Sets

Access groups of tokens by name:

```js
const ignoreTins = am.tokenSet('IGNORE')   // [#SP, #LN, #CM]
const valueTins  = am.tokenSet('VAL')      // [#TX, #NR, #ST, #VL]
const keyTins    = am.tokenSet('KEY')      // [#TX, #NR, #ST, #VL]
```

Define your own with the `tokenSet` option:

```js
am.options({ tokenSet: { MYSET: ['#TX', '#NR'] } })
```

## Plugin Folder Layout

Plugins shipped in this package live under `src/plugins/<name>/`:

```
src/plugins/
├── json/index.ts                    # Pure JSON grammar
├── jsonic/index.ts                  # jsonic relaxations on top of JSON
├── bnf/
│   ├── index.ts                     # Plugin entry — adds am.bnf
│   ├── converter.ts                 # BNF → GrammarSpec conversion
│   └── bin/amagama-bnf-cli.ts       # CLI entry
└── debug/index.ts                   # Tracing + describe()
```

If you ship a plugin as its own npm package, the convention is
`amagama-<name>` or `@<scope>/amagama-<name>`. The CLI's `--plugin`
flag understands both raw module names and the `@amagama/<name>`
shorthand.

## Example: a tiny CSV plugin

```js
function csvPlugin(am, opts) {
  // Drop newlines from IGNORE so they survive into the rule stream.
  am.options({
    tokenSet: { IGNORE: [undefined, null, undefined] },
    rule: { start: 'csv', exclude: 'amagama,imp' },
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

The full version lives in `test/csv-grammar.test.js`.
