# Options Reference

Options are passed to `new Tabnas(options)` (or to `am.make(options)`
to derive a child) to configure a parser instance. All fields are
optional — unset fields use defaults. The complete option type is
`TabnasOptions` (see `src/types.ts`); the default values are in
`src/defaults.ts`.

```js
const { Tabnas } = require('tabnas')

const am = new Tabnas({
  plugins: [myGrammarPlugin],
  comment: { lex: false },
  number: { hex: false },
})
```

## `plugins`

Plugins to apply at construction time. Equivalent to calling
`am.use(plugin)` in order after construction. Children of this
instance (`am.make({ … })`) re-run every plugin in this list against
their own merged options.

```js
new Tabnas({ plugins: [myGrammarPlugin, anotherPlugin] })
```

`plugins` is consumed by the constructor — it's not stored back into
`am.options.plugins`. Per-plugin options live under `options.plugin`
(below).

## `plugin`

Per-plugin option bag, namespaced by plugin name (lowercased).
`am.use(myPlugin, { x: 1 })` stores `{ x: 1 }` under
`options.plugin.myplugin`. Plugins read their own settings from there.

```js
am.options.plugin.foo                 // foo's merged options
```

## `tag`

A short label appended to `am.id` and shown in error diagnostics.
Default `'-'`.

## `fixed`

Controls recognition of fixed structural tokens (`{`, `}`, `[`, `]`,
`:`, `,`).

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable fixed token recognition |
| `token` | object | (built-in) | Map of token name to source string |

## `tokenSet`

Named groups of tokens, used by grammars and by `am.tokenSet(name)`.

| Set | Default members |
|---|---|
| `IGNORE` | `#SP`, `#LN`, `#CM` |
| `VAL` | `#TX`, `#NR`, `#ST`, `#VL` |
| `KEY` | `#TX`, `#NR`, `#ST`, `#VL` |

## `space`

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable space recognition |
| `chars` | string | `" \t"` | Characters treated as space |

## `line`

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable line recognition |
| `chars` | string | `"\r\n"` | Characters treated as line endings |
| `rowChars` | string | `"\n"` | Characters that increment the row counter |
| `single` | boolean | `false` | Generate a separate token per newline |

## `text`

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable unquoted-text matching |
| `modify` | function[] | `[]` | Value transformers applied after matching |

## `number`

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable number matching |
| `hex` | boolean | `true` | Support `0x` hexadecimal |
| `oct` | boolean | `true` | Support `0o` octal |
| `bin` | boolean | `true` | Support `0b` binary |
| `sep` | string\|null | `"_"` | Digit separator character (null to disable) |
| `exclude` | RegExp | — | Pattern to exclude from number matching |

## `comment`

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable all comment lexing |
| `def` | object | (see below) | Comment-type definitions |

Default comment definitions:

```js
{
  hash:  { line: true, start: '#',  lex: true, eatline: false },
  slash: { line: true, start: '//', lex: true, eatline: false },
  multi: { line: false, start: '/*', end: '*/', lex: true, eatline: false },
}
```

Each definition has:

| Field | Type | Description |
|---|---|---|
| `line` | boolean | `true` for line comments, `false` for block |
| `start` | string | Start marker |
| `end` | string | End marker (block comments only) |
| `lex` | boolean | Enable this definition |
| `eatline` | boolean | Consume the trailing newline after the comment |
| `suffix` | string\|string[]\|matcher | Optional body terminators |

Set a definition to `false`/`null` to remove it.

## `string`

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable string matching |
| `chars` | string | `` "'\"`" `` | Quote characters |
| `multiChars` | string | `` "`" `` | Quote characters that allow multiline strings |
| `escapeChar` | string | `"\\"` | Escape character |
| `escape` | object | (standard) | Escape-sequence mappings. Map a key to `null` or `''` to remove a built-in escape (e.g. `{ v: null }` rejects `\v`) |
| `allowUnknown` | boolean | `true` | Copy unknown escape sequences through (`\w` → `w`) |
| `escapeStrict` | boolean | `false` | Restrict escapes to the standard set: disable the non-standard `\xHH` and `\u{…}` structural escapes (`\uXXXX` stays). With escape-map removals + `allowUnknown: false`, yields JSON.parse-conformant escapes |
| `replace` | object | — | Character replacement map during scanning |
| `abandon` | boolean | `false` | On error, let the next matcher try instead of failing |

## `map`

| Field | Type | Default | Description |
|---|---|---|---|
| `extend` | boolean | `true` | Deep-merge duplicate keys |
| `merge` | function | — | Custom merge for duplicates: `(prev, curr, rule, ctx) => result` |
| `child` | boolean | `false` | Parse bare colon as a `child$` key |

## `list`

| Field | Type | Default | Description |
|---|---|---|---|
| `property` | boolean | `true` | Allow key-value pairs in arrays |
| `pair` | boolean | `false` | Parse pairs as object elements (takes precedence over `property`) |
| `child` | boolean | `false` | Parse bare colon as a child value |

## `info`

When enabled, a non-enumerable marker property is attached to parsed
nodes carrying metadata (implicit flag, quote info, etc.).

| Field | Type | Default | Description |
|---|---|---|---|
| `map` | boolean | `false` | Attach marker to map nodes |
| `list` | boolean | `false` | Attach marker to list nodes |
| `text` | boolean | `false` | Wrap string values as `String` objects with marker |
| `marker` | string | `"__info__"` | Property name for the marker |

## `value`

Keyword values — source words that resolve to fixed JS values.

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable value matching |
| `def` | object | (see below) | Keyword definitions |

Default value definitions:

```js
{
  true:  { val: true },
  false: { val: false },
  null:  { val: null },
}
```

Each definition is `{ val, match?, consume? }`. A `match` RegExp gives
a pattern-based value (matched by the text matcher, so lower priority
than pure tokens). For high-priority pattern values, use `match`
(below) instead.

```js
am.make({ value: { def: { yes: { val: true }, no: { val: false } } } })
```

## `match`

Custom matcher tokens and pattern values.

| Field | Type | Default | Description |
|---|---|---|---|
| `lex` | boolean | `true` | Enable custom matchers |
| `token` | object | `{}` | Map of token name to RegExp or matcher function |
| `value` | object | — | Map of value name to `{ match, val? }` (RegExp must start with `^`) |

```js
new Tabnas({
  match: {
    value: { date: { match: /^\d{4}-\d{2}-\d{2}/, val: (m) => new Date(m[0]) } },
  },
})
```

## `ender`

Additional text-ending characters. String or string array. Default
`[]`.

## `rule`

| Field | Type | Default | Description |
|---|---|---|---|
| `start` | string | `"val"` | Name of the starting rule |
| `finish` | boolean | `true` | Auto-close unclosed structures at EOF |
| `maxmul` | number | `3` | Rule-occurrence multiplier limit |
| `include` | string | `""` | Include only alternates with these group tags (comma-separated) |
| `exclude` | string | `""` | Exclude alternates with these group tags (comma-separated) |

## `lex`

| Field | Type | Default | Description |
|---|---|---|---|
| `empty` | boolean | `true` | Allow empty source input |
| `emptyResult` | any | `undefined` | Value returned for empty input |
| `match` | object | (built-in) | Matcher registry: `{ <name>: { order, make } }` |

The `match` registry maps each matcher name to its priority `order`
and a `make` factory. Lower `order` runs first. See `src/defaults.ts`
for the built-in registry.

## `parse`

| Field | Type | Default | Description |
|---|---|---|---|
| `prepare` | object | `{}` | Named functions run to prepare the parse `Context` |

## `result`

| Field | Type | Default | Description |
|---|---|---|---|
| `fail` | any[] | `[]` | Fail the parse if the result matches any of these |

## `rewind`

| Field | Type | Default | Description |
|---|---|---|---|
| `history` | number | `64` | Consumed tokens retained for `ctx.rewind()`; `Infinity` to retain all |

## `safe`

| Field | Type | Default | Description |
|---|---|---|---|
| `key` | boolean | `true` | Block prototype-polluting keys (`__proto__`, …) |

## `error`

Custom error-message templates, keyed by error code. Templates use
`{key}` placeholders.

```js
new Tabnas({ error: { unexpected: 'bad character: {src}' } })
```

The built-in codes are `unknown`, `unexpected`, `invalid_unicode`,
`invalid_ascii`, `unprintable`, `unterminated_string`,
`unterminated_comment`, `unknown_rule`, `end_of_source`.

## `hint`

Additional explanatory text per error code, appended below the
message. Same `{key}` template syntax.

## `errmsg`

Controls error-message framing.

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | `"tabnas"` | Prefix shown as `[name/code]` |
| `suffix` | boolean\|string\|function | `true` | Append the internal-diagnostics line (`true`), or a custom suffix |
| `link` | string | — | A "see also" line (e.g. a docs URL) shown when `suffix` is `true` |

## `color`

ANSI colouring for error output. All fields off by default.

| Field | Type | Default | Description |
|---|---|---|---|
| `active` | boolean | `false` | Enable colour |
| `reset` / `hi` / `lo` / `line` | string | `""` | ANSI escape codes for each role |

## `debug`

| Field | Type | Default | Description |
|---|---|---|---|
| `get_console` | function | `() => console` | Returns the console used for logging |
| `maxlen` | number | `99` | Max length of a parse value when printed |
| `print` | object | `{ config: false }` | `{ config?, src? }` print options |

## Construction-only flags

These meta-flags only matter at instance construction; changing them
afterwards has no effect.

| Field | Type | Default | Description |
|---|---|---|---|
| `defaults$` | boolean | `true` | If `false`, skip merging the built-in defaults — start from a blank options bag |
| `standard$` | boolean | `true` | If `false`, skip registering the standard tokens (read by `configure`) |
| `grammar$` | boolean | — | Reserved for plugins to opt out of registering grammar |

`am.empty(opts)` is shorthand for `new Tabnas({ defaults$: false,
standard$: false, grammar$: false, ...opts })`.

## Advanced

| Field | Type | Default | Description |
|---|---|---|---|
| `config.modify` | object | `{}` | Named `(config, options) => void` hooks run after `configure` |
| `parser.start` | function | — | Supply a custom parser entry point, replacing the built-in parser |
