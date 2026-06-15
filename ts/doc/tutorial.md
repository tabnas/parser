# Tutorial — your first parser

This walks you from an empty project to a working parser you wrote
yourself. tabnas ships no grammar, so there is nothing to "turn on" —
you teach the engine one token and one rule, watch it parse, then
extend it once. Follow it top to bottom; every step builds on the
last.

## Install

```bash
npm install tabnas
```

## Create an instance

A parser is an instance of the `Tabnas` class. Create one and try to
parse something:

```js
const { Tabnas } = require('tabnas')

const tn = new Tabnas()
tn.parse('hello')                     // throws — no grammar yet
```

The bare instance knows how to *lex* (split text into tokens) but has
no rule that says what to *do* with them, so this throws. That is the
whole point: a grammar is something you add.

## Define one token and one rule

A grammar is a plugin — a function that receives the instance and
configures it. The smallest useful grammar recognises a single word.

```js
function helloPlugin(tn) {
  // Teach the lexer a fixed token: the source `hello` becomes `#HI`.
  tn.options({ fixed: { token: { '#HI': 'hello' } } })

  // Teach the parser what to do when it opens the start rule (`val`)
  // and sees that token: set the result node to the string 'world'.
  tn.rule('val', (rs) => rs.open([
    { s: ['#HI'], a: (r) => { r.node = 'world' } },
  ]))
}
```

Two things happened here:

- `tn.options({ fixed: { token: ... } })` registered a **fixed token**.
  Fixed tokens are exact source strings; `'hello'` in the input now
  lexes as the token named `#HI`.
- `tn.rule('val', ...)` modified the start rule. Each rule has an
  **open** phase holding a list of **alternates**. The one alternate
  here says: if the next token sequence (`s`) is `['#HI']`, run the
  **action** (`a`), which assigns the result to `r.node`.

`val` is the default start rule (see the `rule.start` option). Every
parse begins there.

## Parse

Apply the plugin at construction time and parse:

```js
const tn = new Tabnas({ plugins: [helloPlugin] })
tn.parse('hello')                     // 'world'
```

You wrote a parser. It accepts exactly one input, but the machinery is
the same one a full JSON grammar uses.

## Extend it once

Real grammars combine tokens. Add a second word and let either match.
A rule alternate's `s` can hold a list of token names; an alternate
fires when the whole sequence matches, so add a second alternate:

```js
function helloPlugin(tn) {
  tn.options({ fixed: { token: { '#HI': 'hello', '#BY': 'bye' } } })

  tn.rule('val', (rs) => rs.open([
    { s: ['#HI'], a: (r) => { r.node = 'world' } },
    { s: ['#BY'], a: (r) => { r.node = 'farewell' } },
  ]))
}

const tn = new Tabnas({ plugins: [helloPlugin] })
tn.parse('hello')                     // 'world'
tn.parse('bye')                       // 'farewell'
```

The parser tries each open alternate in order and takes the first whose
token sequence matches. That ordering — first match wins, two-token
lookahead, no backtracking — is the model you design grammars around.

## Where to go next

- [How-to guides](guide.md) — recipes for keywords, custom matchers,
  error handling, child instances, and events.
- [Writing plugins](plugins.md) — structure a grammar plugin properly.
- [API reference](api.md) — every method and property.
- [Concepts](concepts.md) — why the engine is split this way.
