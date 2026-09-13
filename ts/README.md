# tabnas

A pluggable parsing engine. The runtime is a class (`Tabnas`) that
runs a rule-based parser over a configurable matcher-based lexer. The
package ships **no grammar** of its own: every grammar is a plugin
that you (or another package) supply.

```bash
npm install @tabnas/parser
```

A tiny taste, a one-token grammar defined inline:

```js
const { Tabnas } = require('@tabnas/parser')

const tn = new Tabnas({ plugins: [(tn) => {
  tn.options({ fixed: { token: { '#HI': 'hello' } } })
  tn.rule('val', (rs) => rs.open([
    { s: ['#HI'], a: (r) => { r.node = 'world' } },
  ]))
}] })

tn.parse('hello')                     // 'world'
```

That grammar as a railroad/syntax diagram, generated from the live parser
with [`@tabnas/railroad`](https://github.com/tabnas/railroad):

![tabnas taste-grammar railroad diagram](doc/taste.svg)

A vertical ASCII version is in [`doc/taste.txt`](doc/taste.txt).

## Documentation

- [Tutorial](doc/tutorial.md). Build your first parser, step by step.
- [How-to guides](doc/guide.md). Short recipes for common tasks.
- [Writing plugins](doc/plugins.md). Author a grammar or tooling
  plugin.
- [API reference](doc/api.md). Every public method, property, and
  export.
- [Options reference](doc/options.md). Every option field and its
  default.
- [Concepts](doc/concepts.md). How the TypeScript engine is put
  together, and why.

Shared design docs for both runtimes live at the top of the repo:

- [Architecture](../doc/architecture.md). The engine model.
- [Syntax](../doc/syntax.md). The relaxed-JSON syntax reference.

### Explanation / design notes

- [LSP feasibility](doc/lsp-feasibility.md). Language-server angles.
- [GBNF feasibility](doc/gbnf-feasibility.md). Llama.cpp grammar
  (constrained LLM decoding) angles.

## Companion packages

Grammars and tooling ship as separate packages, not in this one:

- `@tabnas/abnf`. Compiles ABNF into engine rules and adds
  `tn.bnf(src)`.
- `@tabnas/debug`. Tracing and `describe()` helpers.

The strict-JSON grammar used by the conformance tests lives as a test
fixture at [`test/json-plugin.ts`](test/json-plugin.ts): a worked
example of a non-trivial grammar plugin.

The [Go port](../go/) follows the same grammar-free design, with its
strict-JSON grammar kept as a test fixture too.

## License

MIT. Copyright (c) Richard Rodger.
