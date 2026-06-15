# tabnas

[![build](https://github.com/tabnas/parser/actions/workflows/build.yml/badge.svg)](https://github.com/tabnas/parser/actions/workflows/build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A **pluggable parsing engine**: a configurable rule-based parser running
over a configurable matcher-based lexer. The engine ships **no grammar**
of its own — you bring the grammar as a plugin. tabnas grew out of the
[jsonic](https://github.com/tabnas/jsonic) plugin: lenient JSON for
humans (unquoted keys, implicit objects, comments, trailing commas, path
diving), which is now just one grammar built on this engine.

```
a:1, b:2          →  {"a": 1, "b": 2}
[x y z]           →  ["x", "y", "z"]
a:b:c:1           →  {"a": {"b": {"c": 1}}}
```

## What kind of parser is this?

tabnas is a **rule-based parser** driven by a **matcher-based lexer**, and
it is **grammar-agnostic**:

- **Lexer** — tokens are produced by an ordered list of matchers (fixed
  strings, spaces, lines, strings, comments, numbers, free text). Add or
  replace matchers to recognise new tokens.
- **Parser** — a small rule machine. Each rule has `open` and `close`
  alternatives that match token sequences, push child rules, and build the
  result node. There is no fixed grammar baked in.
- **Plugins** — a grammar is a plugin that registers tokens and rules.
  Compose several plugins to parse a dialect.

**Benefits & features**

- **Grammar-free core** — one engine, many grammars; nothing JSON-specific
  is hard-wired.
- **Pluggable & composable** — mix grammar plugins (JSON, JSONC, JSON5,
  CSV, YAML, TOML, XML, INI, …) and tooling plugins (debug, BNF) on one
  instance.
- **Lenient by design** — the relaxed-JSON heritage makes human-friendly
  input (unquoted keys, implicit structure, comments, trailing commas) a
  first-class capability rather than an afterthought.
- **Configurable end to end** — tokens, matchers, rules, and a deep options
  tree are all overridable per instance.
- **Good errors** — structured, located parse errors with source context.
- **Two runtimes, one behaviour** — a canonical TypeScript engine and a Go
  port that share the same `.tsv` conformance fixtures.

## TypeScript is canonical; Go follows

The **TypeScript implementation is the original and canonical** engine —
it defines the behaviour, the API shape, and the conformance fixtures. The
**Go port follows that functionality**: same engine model, same grammar-free
design, same layout, validated against the same shared fixtures. When the
two ever differ, the TypeScript behaviour is authoritative.

## Choose your runtime

| Runtime | Package / module | Start here |
|---|---|---|
| **TypeScript / JavaScript** — original & canonical | `tabnas` (npm) | [`ts/README.md`](ts/README.md) |
| **Go** — port that follows the TS engine | `github.com/tabnas/parser/go` | [`go/README.md`](go/README.md) |

## Documentation

The docs are organised by what you are trying to do, symmetrically for both
runtimes:

- **Learning the basics** — tutorials walk you from an empty file to a
  working parse: [TypeScript tutorial](ts/doc/tutorial.md) ·
  [Go tutorial](go/doc/tutorial.md).
- **Getting a specific job done** — how-to recipes:
  [TypeScript guides](ts/doc/guide.md) · [Go guides](go/doc/guide.md);
  plugin authoring [TS](ts/doc/plugins.md) · [Go](go/doc/plugins.md).
- **Looking something up** — reference for every option, method, and rule:
  the shared [syntax reference](doc/syntax.md), plus per-language API
  ([TS](ts/doc/api.md) · [Go](go/doc/api.md)) and options
  ([TS](ts/doc/options.md) · [Go](go/doc/options.md)).
- **Understanding how it works** — the shared
  [architecture](doc/architecture.md), and per-language concept notes
  ([TS](ts/doc/concepts.md) · [Go](go/doc/concepts.md)). Porting from TS to
  Go? See [differences](go/doc/differences.md).

## Repository layout

| Path | What it is |
|---|---|
| [`ts/`](ts/) | The canonical TypeScript engine (the `tabnas` npm package). |
| [`go/`](go/) | The Go port (`github.com/tabnas/parser/go`) — grammar-free, same layout as TS. |
| [`test/spec/`](test/spec/) | Shared `.tsv` conformance fixtures, run by both runtimes. |
| [`doc/`](doc/) | Language-neutral docs: the [syntax reference](doc/syntax.md) and the [architecture explanation](doc/architecture.md). |

Working on the codebase itself? Each directory has an `AGENTS.md` with
build, test, and contribution notes; start with [`AGENTS.md`](AGENTS.md).

## Legacy version

The original project was [`jsonicjs/jsonic`](https://github.com/jsonicjs/jsonic)
— now the **legacy version**. Its engine has been generalised into tabnas,
and the relaxed-JSON grammar lives on as the
[jsonic](https://github.com/tabnas/jsonic) plugin. New work should target
tabnas and the `@tabnas/*` plugins.

## Sponsored by

This open source module is sponsored and supported by
[Voxgig](https://www.voxgig.com).

## License

MIT. Copyright (c) 2013-2026 Richard Rodger.
