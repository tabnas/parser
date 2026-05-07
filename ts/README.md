# amagama

JSON is great. JSON parsers are not. They punish you for every missing
quote and misplaced comma. You're a professional -- you know what you
meant. amagama knows too.

```
a:1,foo:bar  →  {"a": 1, "foo": "bar"}
```

It's a JSON parser that isn't strict. And it's very, very extensible.

Available for [TypeScript/JavaScript](#install) and [Go](../go/).

## Install

```bash
npm install amagama
```

## Quick Example

amagama is a class. Create an instance with the grammar plugin you
want, then call `parse`:

```js
const { Amagama, jsonic } = require('amagama')

const am = new Amagama({ plugins: [jsonic] })

am.parse('a:1, b:2')              // {"a": 1, "b": 2}
am.parse('x, y, z')               // ["x", "y", "z"]
am.parse('{a: {b: 1, c: 2}}')     // {"a": {"b": 1, "c": 2}}
```

```ts
import { Amagama, jsonic } from 'amagama'

const am = new Amagama({ plugins: [jsonic] })
am.parse('a:1, b:2')              // {"a": 1, "b": 2}
```

For strict JSON (no relaxations), swap the plugin:

```js
const { Amagama, json } = require('amagama')
const strict = new Amagama({ plugins: [json] })
strict.parse('{"a":1}')           // {"a": 1}
strict.parse('{a:1}')             // throws — JSON.parse-equivalent
```

## What Syntax Does the jsonic Plugin Accept?

More than you'd expect. All of the following parse to `{"a": 1, "b": "B"}`:

```
a:1,b:B
```

```
a:1
b:B
```

```
a:1
// a:2
# a:3
/* b wants
 * to B
 */
b:B
```

```
{ "a": 100e-2, 'b':`\x42`, }
```

That last one mixes double quotes, single quotes, backticks, unicode
escapes, hex escapes, and scientific notation. It doesn't matter. amagama
handles it.

Here's the full set of relaxations:

- **Unquoted keys and values**: `a:1` &rarr; `{"a": 1}`
- **Implicit top-level object**: `a:1,b:2` &rarr; `{"a": 1, "b": 2}`
- **Implicit top-level array**: `a,b` &rarr; `["a", "b"]`
- **Trailing commas**: `{a:1,b:2,}` &rarr; `{"a": 1, "b": 2}`
- **Single-quoted strings**: `'hello'` works like `"hello"`
- **Backtick strings**: `` `hello` `` works like `"hello"`
- **Multiline strings**: backtick strings preserve newlines
- **Indent-adjusted strings**: `'''...\n'''` trims leading indent
- **Comments**: `//`, `#` (line), `/* */` (block)
- **Object merging**: `a:{b:1},a:{c:2}` &rarr; `{"a": {"b": 1, "c": 2}}`
- **Path diving**: `a:b:1,a:c:2` &rarr; `{"a": {"b": 1, "c": 2}}`
- **All number formats**: `1e1 === 0xa === 0o12 === 0b1010`, plus `1_000` separators
- **Auto-close at EOF**: unclosed `{` or `[` close automatically

For the full syntax reference, see [doc/syntax.md](doc/syntax.md).

## Architecture

The engine is intentionally split:

- **`Amagama` core** — lexer, parser, rule machinery. No grammar of
  its own.
- **Plugins** in `src/plugins/<name>/` — each contributes a piece of
  the runtime: a grammar (`json`, `jsonic`), a converter (`bnf`),
  developer tooling (`debug`).

The class never carries grammar by default; everything is opt-in via
`plugins`. To embed BNF-defined grammar:

```js
const { Amagama, jsonic, bnf } = require('amagama')

const am = new Amagama({ plugins: [jsonic, bnf] })
am.bnf('greet = "hi" / "hello"')
am.parse('hi')
```

## Customization

You might be tempted to think a lenient parser is a simple thing. It
isn't. amagama is built around a rule-based parser and a matcher-based
lexer. Both are fully customizable through options and plugins. You can
change almost anything about how parsing works -- and you don't have to
understand the internals to do it.

### Options

Tweak parser/lexer behaviour at construction time, or via
`am.options(...)` afterwards. A child instance can override anything:

```js
const am = new Amagama({ plugins: [jsonic] })

const lenient = am.make({
  comment: { lex: false },         // disable comments
  number: { hex: false },          // disable hex numbers
  value: {
    def: { yes: { val: true }, no: { val: false } }
  }
})

lenient.parse('yes')               // true
```

Options compose. You turn things off, you turn things on, you define new
value tokens. That's it.

See [doc/options.md](doc/options.md) for the full options reference.

### Plugins

When options aren't enough, plugins let you reach deeper. They can
modify the grammar, add matchers, or hook into parse events:

```js
function myPlugin(amagama, options) {
  // Register a custom fixed token
  amagama.options({ fixed: { token: { '#TL': '~' } } })
  const T_TILDE = amagama.token('#TL')

  // Modify grammar rules
  amagama.rule('val', (rs) => {
    rs.open([{
      s: [T_TILDE],
      a: (rule) => { rule.node = options.tildeValue ?? null }
    }])
  })
}

const am = new Amagama({ plugins: [jsonic] })
am.use(myPlugin, { tildeValue: 42 })
am.parse('~')                      // 42
```

Consider what just happened: we invented a new syntax element (`~`),
told the parser what to do when it encounters one, and wired it up with
a configurable value. The parser itself doesn't care what symbols you
use. It only cares about rules.

See [doc/plugins.md](doc/plugins.md) for the plugin authoring guide.

## API Reference

See [doc/api.md](doc/api.md) for the full API.

The essentials:

| Construct | Description |
|---|---|
| `new Amagama(options?)` | Create a parser instance. Pass `{ plugins: [...] }` for grammar. |
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
| `json` | Pure JSON grammar (`JSON.parse`-equivalent). |
| `jsonic` | The relaxed-JSON grammar shown above. Layered on top of `json`. |
| `bnf` | Adds `am.bnf(src)` — installs a grammar from a BNF string. |
| `Debug` | Adds `am.debug.describe()` and parser tracing. |

## Go Version

There's a Go port with the same core parsing behavior. Same syntax,
same relaxations, same results. See the [Go documentation](../go/) for
installation and usage.

```go
import "github.com/amagamajs/amagama/go"

result, err := amagama.Parse("a:1, b:2")
```

## License

MIT. Copyright (c) Richard Rodger.
