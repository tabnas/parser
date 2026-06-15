# How-to guides

Short, task-focused recipes. Each assumes you have a `Tabnas`
instance and know the basics (see the [tutorial](tutorial.md)). For
full field lists and signatures, follow the links into the
[API reference](api.md) and [options reference](options.md).

## Define your own grammar

Wrap your grammar in a plugin function and apply it. A grammar
registers tokens, then registers rules that act on them.

```js
const { Tabnas } = require('@tabnas/parser')

function myGrammar(tn) {
  tn.options({ fixed: { token: { '#HI': 'hello' } } })
  tn.rule('val', (rs) => rs.open([
    { s: ['#HI'], a: (r) => { r.node = 'world' } },
  ]))
}

const tn = new Tabnas({ plugins: [myGrammar] })
tn.parse('hello')                     // 'world'
```

Apply at construction with `{ plugins: [...] }`, or afterwards with
`tn.use(myGrammar)`. See [Writing plugins](plugins.md) for structure
and conventions.

## Add a keyword (literal value)

Keywords are source words that resolve to fixed JS values. Register
them under the `value.def` option; a grammar that matches the `#VL`
value token will then resolve them:

```js
const tn = new Tabnas({
  plugins: [myGrammar],
  value: { def: { yes: { val: true }, no: { val: false } } },
})

tn.parse('yes')                       // true
tn.parse('no')                        // false
```

The built-in keywords are `true`, `false`, and `null`. See the
[`value` option](options.md#value).

## Add a custom matcher (regex value)

For values that need a pattern rather than an exact word, register a
regex under `match.value`. The `match` regex must be anchored with
`^`; `val` maps the match array to the parsed value:

```js
const tn = new Tabnas({
  plugins: [myGrammar],
  match: {
    lex: true,
    value: {
      date: { match: /^\d{4}-\d{2}-\d{2}/, val: (m) => new Date(m[0]) },
    },
  },
})

tn.parse('2024-01-15')                // Date(2024-01-15)
```

Regex values are matched by the text matcher, so pure-token alternates
always win over them. For higher priority, register a full matcher
function under `match.token`. See the [`match` option](options.md#match)
and [custom matchers](plugins.md#custom-matchers).

## Handle parse errors

A failed parse throws a `TabnasError`. Catch it and read its fields:

```js
const { Tabnas, TabnasError } = require('@tabnas/parser')

try {
  tn.parse(brokenSource)
} catch (err) {
  if (err instanceof TabnasError) {
    err.code                          // e.g. 'unexpected'
    err.message                       // formatted, multi-line, with source context
    err.details                       // structured details, e.g. { state: 'open' }
    err.lineNumber                    // row of the offending token (1-based)
    err.columnNumber                  // column of the offending token (1-based)
  } else {
    throw err
  }
}
```

`TabnasError` extends `SyntaxError`. See the
[error reference](api.md#error-handling) for the full field list and
for customising messages via the `error` / `hint` options.

## Derive a configured child instance

`tn.make(options)` forks an instance: the child inherits the parent's
config, plugins, and rules, then merges your overrides on top. The
child re-runs each parent plugin against its own merged options, so
option-conditional grammar is re-evaluated for the child.

```js
const strict = tn.make({ comment: { lex: false }, number: { hex: false } })

strict.parse(src)                     // parses with comments and hex disabled
tn.parse(src)                         // parent is unchanged
```

Use `rule.exclude` / `rule.include` to strip or keep grammar
alternates by group tag:

```js
const trimmed = tn.make({ rule: { exclude: 'experimental' } })
```

See [`make()`](api.md#tnmakeoptions).

## Subscribe to lex / rule events

`tn.sub({ lex, rule })` registers observers that fire as the parse
runs. Multiple subscriptions are allowed and fire in registration
order. Observers cannot change the parse — they just watch.

```js
tn.sub({
  lex: (token, rule, ctx) => { /* a token was produced */ },
  rule: (rule, ctx) => { /* a rule state was processed */ },
})

tn.parse(src)
```

See [`sub()`](api.md#tnsub-lex-rule-) and, for plugin-side logging,
[subscribing to events](plugins.md#subscribing-to-events).
