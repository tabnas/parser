# tabnas

A pluggable parsing engine. The runtime is a class — `Tabnas` — that
runs a rule-based parser over a configurable matcher-based lexer.
The package itself ships **no grammar**; every grammar is a plugin
that you (or another package) supply.

This package ships:

- The `Tabnas` class — engine, lexer, parser, rule machinery.

A strict-JSON grammar lives as a test fixture under `test/json-plugin.ts`
— useful as a worked example, and the engine's own conformance tests
exercise it.

Companion plugins live in their own packages:

- [`@tabnas/bnf`](../../bnf/) — ABNF / BNF grammar compiler.
- [`@tabnas/debug`](../../debug/) — tracing and `describe()` helpers.

For lenient-JSON parsing (unquoted keys, implicit objects, comments,
trailing commas, etc.), see the [Go port](../go/).

## Install

```bash
npm install tabnas
```

## Quick example — define your own grammar

```js
const { Tabnas } = require('tabnas')

// A useless-but-real grammar: parse the literal token `hello`.
function helloPlugin(am) {
  am.options({ fixed: { token: { '#HI': 'hello' } } })
  am.rule('val', (rs) => rs.open([
    { s: ['#HI'], a: (r) => { r.node = 'world' } },
  ]))
}

const am = new Tabnas({ plugins: [helloPlugin] })
am.parse('hello')                     // 'world'
```

## Quick example — BNF

The companion [`@tabnas/bnf`](../../bnf/) package compiles ABNF / BNF
into the engine's rule format:

```js
const { Tabnas } = require('tabnas')
const { bnf } = require('@tabnas/bnf')

const am = new Tabnas({ plugins: [bnf] })
am.bnf('greet = "hi" / "hello"')

am.parse('hi')                        // { rule: 'greet', src: 'hi', kids: [] }
```

## Plugins

A plugin is a function `(tabnas, options?) => void | Tabnas`. Plugins
add tokens, register matchers, modify rules, hook events, or expose
new methods on the instance:

```js
function tildePlugin(am, options) {
  am.options({ fixed: { token: { '#TL': '~' } } })
  const T_TILDE = am.token('#TL')

  am.rule('val', (rs) => {
    rs.open([
      { s: [T_TILDE], a: (rule) => { rule.node = options.tildeValue ?? null } },
    ])
  })
}

const am = new Tabnas({ plugins: [tildePlugin] })
```

`am.make()` derives a child instance with overridden options. The child
inherits and re-runs each parent plugin against its merged options, so
option-conditional alternates get re-evaluated.

## Architecture

The engine is intentionally split:

- **`Tabnas` core** (this package) — lexer, parser, rule machinery.
  No grammar of its own.
- **Plugins** — separate packages that contribute grammar
  (`@tabnas/bnf`) or developer tooling (`@tabnas/debug`).

The class never carries grammar by default; grammar is opt-in via the
`plugins` option.

## API Reference

See [doc/api.md](doc/api.md) for the full API. The essentials:

| Construct | Description |
|---|---|
| `new Tabnas(options?)` | Create a parser instance. Pass `{ plugins: [...] }` for grammar / tooling. |
| `am.parse(src, meta?, parent_ctx?)` | Parse a string. |
| `am.make(options?)` | Derive a child instance with overridden options (inherits parent plugins). |
| `am.empty(options?)` | Bare instance: no defaults, no standard tokens, no grammar. |
| `am.use(plugin, opts?)` | Apply a plugin to this instance. Returns the instance (or what the plugin returned). |
| `am.options(change?)` | Get the merged option tree, or apply a partial change. |
| `am.rule(name?, definer?)` | Read or modify a grammar rule. |
| `am.token(ref)` | Look up a token name ↔ Tin. |
| `am.sub({lex?, rule?})` | Subscribe to parse events. |

Companion plugin packages:

| Plugin | Purpose |
|---|---|
| [`@tabnas/bnf`](../../bnf/) | Adds `am.bnf(src)` — installs a grammar from a BNF / ABNF string. |
| [`@tabnas/debug`](../../debug/) | Adds `am.debug.describe()` and parser tracing. |

The `test/json-plugin.ts` test fixture is a worked example of a
non-trivial grammar plugin (strict JSON).

## Go Version

A [Go port](../go/) ships a relaxed-JSON grammar. Same engine
architecture, same [test specs](../test/spec/) for behaviours that
overlap.

```go
import "github.com/amagamajs/tabnas/go"

result, err := tabnas.Parse("a:1, b:2")
```

## License

MIT. Copyright (c) Richard Rodger.
