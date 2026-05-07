# amagama

A pluggable parsing engine. The runtime is a class — `Amagama` — that
runs a rule-based parser over a configurable matcher-based lexer.
The package itself ships **no grammar**; every grammar is a plugin
that you (or another package) supply.

This package ships:

- The `Amagama` class — engine, lexer, parser, rule machinery.
- A `bnf` plugin that compiles ABNF / BNF source into the engine's
  rule format and installs it on the instance.
- A `Debug` plugin for tracing.

A strict-JSON grammar lives as a test fixture under `test/json-plugin.ts`
— useful as a worked example, and the engine's own conformance tests
exercise it.

For lenient-JSON parsing (unquoted keys, implicit objects, comments,
trailing commas, etc.), see the [Go port](../go/).

## Install

```bash
npm install amagama
```

## Quick example — define your own grammar

```js
const { Amagama } = require('amagama')

// A useless-but-real grammar: parse the literal token `hello`.
function helloPlugin(am) {
  am.options({ fixed: { token: { '#HI': 'hello' } } })
  am.rule('val', (rs) => rs.open([
    { s: ['#HI'], a: (r) => { r.node = 'world' } },
  ]))
}

const am = new Amagama({ plugins: [helloPlugin] })
am.parse('hello')                     // 'world'
```

## Quick example — BNF

The bundled `bnf` plugin compiles ABNF / BNF into the engine's rule
format:

```js
const { Amagama, bnf } = require('amagama')

const am = new Amagama({ plugins: [bnf] })
am.bnf('greet = "hi" / "hello"')

am.parse('hi')                        // { rule: 'greet', src: 'hi', kids: [] }
```

`am.bnf.toSpec(source)` returns the GrammarSpec without installing — useful
for inspecting or saving for later.

## Plugins

A plugin is a function `(amagama, options?) => void | Amagama`. Plugins
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

const am = new Amagama({ plugins: [tildePlugin] })
```

`am.make()` derives a child instance with overridden options. The child
inherits and re-runs each parent plugin against its merged options, so
option-conditional alternates get re-evaluated.

## Architecture

The engine is intentionally split:

- **`Amagama` core** — lexer, parser, rule machinery. No grammar of
  its own.
- **Plugins** in `src/plugins/<name>/` — each contributes a piece of
  the runtime: a converter (`bnf`), developer tooling (`debug`).

The class never carries grammar by default; grammar is opt-in via the
`plugins` option.

## API Reference

See [doc/api.md](doc/api.md) for the full API. The essentials:

| Construct | Description |
|---|---|
| `new Amagama(options?)` | Create a parser instance. Pass `{ plugins: [...] }` for grammar / tooling. |
| `am.parse(src, meta?, parent_ctx?)` | Parse a string. |
| `am.make(options?)` | Derive a child instance with overridden options (inherits parent plugins). |
| `am.empty(options?)` | Bare instance: no defaults, no standard tokens, no grammar. |
| `am.use(plugin, opts?)` | Apply a plugin to this instance. Returns the instance (or what the plugin returned). |
| `am.options(change?)` | Get the merged option tree, or apply a partial change. |
| `am.rule(name?, definer?)` | Read or modify a grammar rule. |
| `am.token(ref)` | Look up a token name ↔ Tin. |
| `am.sub({lex?, rule?})` | Subscribe to parse events. |

Plugins shipped in this package:

| Plugin | Purpose |
|---|---|
| `bnf` | Adds `am.bnf(src)` — installs a grammar from a BNF string. |
| `Debug` | Adds `am.debug.describe()` and parser tracing. |

The `test/json-plugin.ts` test fixture is a worked example of a
non-trivial grammar plugin (strict JSON).

## Go Version

A [Go port](../go/) ships a relaxed-JSON grammar. Same engine
architecture, same [test specs](../test/spec/) for behaviours that
overlap.

```go
import "github.com/amagamajs/amagama/go"

result, err := amagama.Parse("a:1, b:2")
```

## License

MIT. Copyright (c) Richard Rodger.
