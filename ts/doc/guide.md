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

## Combine two grammars

`a.merge(b)` builds a new instance carrying both grammars; the
originals are untouched and the operation is commutative. Give each
instance a distinct `tag`: it identifies the grammar, prefixes its
named actions in the result, and breaks ordering ties.

```js
const a = new Tabnas({ tag: 'A', fixed: { token: { '#AT': '@' } } })
a.rule('val', (rs) => rs.open([{ s: ['#TX', '#AT'] }]))

const b = new Tabnas({ tag: 'B', fixed: { token: { '#PC': '%' } } })
b.rule('val', (rs) => rs.open([{ s: ['#TX', '#PC'] }]))

const ab = a.merge(b)
ab.parse('x@')                        // grammar A's form
ab.parse('y%')                        // grammar B's form
```

Shared rules interleave their alternates deterministically so the
combined grammar parses unambiguously; options deep-merge, and a
genuine conflict (both sides set the same option to different
non-default values) throws naming the option path.

See [`merge()`](api.md#tnmergeother) for the exact ordering rules.

## Subscribe to lex / rule events

`tn.sub({ lex, rule })` registers observers that fire as the parse
runs. Multiple subscriptions are allowed and fire in registration
order. Observers cannot change the parse: they just watch.

```js
tn.sub({
  lex: (token, rule, ctx) => { /* a token was produced */ },
  rule: (rule, ctx) => { /* a rule state was processed */ },
})

tn.parse(src)
```

See [`sub()`](api.md#tnsub-lex-rule-ruledone-) and, for plugin-side logging,
[subscribing to events](plugins.md#subscribing-to-events).

## Parse a binary format

The engine is byte-agnostic. A JavaScript string is an array of 16-bit
code units, and latin1 maps bytes 0 to 255 onto code units 0 to 255 one
for one, so `buf.toString('latin1')` hands the lexer the bytes unchanged.
`Point.sI`, `Token.sI` and `Token.len` are then byte offsets.

Register field matchers under `match.token`, not under `lex.match`. Only
`match.token` matchers are gated on the rule's expected-token column, and
that gating is what makes a binary format parsable at all: the bytes do
not say what they are, so the grammar has to. A matcher is handed the
live rule, so a field whose length came from an earlier field reads that
length off `rule.k`.

```js
const { Tabnas } = require('@tabnas/parser')

// `[u8 length][that many bytes]`, repeated to end of input.
function blobs(tn) {
  tn.options({
    rule: { start: 'blobs', exclude: 'tabnas,imp' },

    // Nothing in a binary stream is ignorable.
    tokenSet: { IGNORE: [null, null, null] },

    // Switch the text-oriented built-ins off, and drop them from the
    // matcher pipeline: `lex: false` alone leaves each one installed and
    // called once per token only to decline.
    fixed: { lex: false }, space: { lex: false }, line: { lex: false },
    text: { lex: false }, number: { lex: false }, comment: { lex: false },
    string: { lex: false }, value: { lex: false },
    lex: { match: { fixed: null, space: null, line: null, string: null,
                    comment: null, number: null, text: null } },

    match: { lex: true, token: {
      '#LEN': (lex) => {
        const pnt = lex.pnt
        if (pnt.len <= pnt.sI) return undefined
        const tkn = lex.token('#LEN', lex.src.charCodeAt(pnt.sI), undefined,
                              pnt, undefined, undefined, 1)
        pnt.sI += 1
        return tkn
      },

      // Length from the RULE, not from the bytes. No `val` and no `src`:
      // the token is a bare (sI, len) span, so nothing is copied until
      // something reads it.
      '#BODY': (lex, rule) => {
        const n = rule.rawk()?.len
        if (null == n) return undefined
        const pnt = lex.pnt
        if (pnt.len < pnt.sI + n) return undefined
        const tkn = lex.token('#BODY', undefined, undefined, pnt,
                              undefined, undefined, n)
        pnt.sI += n
        return tkn
      },
    } },
  })

  const { LEN, BODY, ZZ } = tn.token

  tn.rule('blobs', (rs) => rs
    .bo((r) => (r.node = (r.prev && r.prev.node) || []))
    .open([
      { s: [ZZ] },
      { s: [LEN], a: (r) => (r.k.len = r.o[0].val), p: 'body' },
    ])
    .close([{ s: [ZZ] }, { s: [LEN], b: 1, r: 'blobs' }])
    .bc((r) => {
      if (r.child && null != r.child.node) r.node.push(r.child.node)
    }))

  tn.rule('body', (rs) =>
    rs.open([{ s: [BODY], a: (r) => (r.node = r.o[0].src) }]))
}

const tn = new Tabnas({ plugins: [blobs] })
const bytes = Buffer.from([3, 0x61, 0x62, 0x63, 2, 0x64, 0x65])
tn.parse(bytes.toString('latin1'))    // => ['abc', 'de']
```

Column gating changes how the alternates are written:

- An alternate that leads to a fetch has to name the token it expects.
  `{ p: 'body' }` names nothing, so the column admits no matcher and the
  fetch yields `#BD`. `{ s: [LEN], b: 1, p: 'body' }` matches the byte,
  pushes it back, and hands it to the child.
- Declaration order under `match.token` fixes token order, which is the
  tie-break inside a column. Declare a narrow matcher (a two-byte
  sentinel) before a wide one (a catch-all byte).
- Put a length in `k`, not in `u`. Only `k` and `n` reach child rules.

The same shape covers the other field kinds. A self-terminating field (a
LEB128 varint) loops on its own continuation bit. A termination marker is
`src.indexOf('\u0000', pnt.sI)`, with the token's `len` covering the
terminator and its `val` not. A sub-byte field keeps a bit offset on
`ctx.u` and advances `pnt.sI` only over the bytes it fully crossed,
because the engine's cursor counts bytes and has no bit position of its
own.

For byte-oriented diagnostics, override the message template: the token
fields are in scope, so `error: { unexpected: 'bad field at byte {sI}' }`
reports an offset instead of a row and column. The source excerpt under
the message is still rendered as text lines, which is of no use for
binary input; the structured diagnostic (`JSON.stringify(err)`) carries
`pos`, `expected` and `ruleStack` instead.

A worked example covering every field kind, with the Go and Rust mirrors,
is [`test/binary-grammar.test.js`](../test/binary-grammar.test.js). The
cost model is in [Concepts](concepts.md#binary-input).
