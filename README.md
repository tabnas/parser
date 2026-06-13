# tabnas

A pluggable parsing engine — a configurable rule-based parser over a
configurable matcher-based lexer. tabnas grew out of the jsonic use
case: lenient JSON for humans (unquoted keys, implicit objects,
comments, trailing commas, path diving). The engine ships **no
grammar**; grammar comes from plugins.

```
a:1, b:2          →  {"a": 1, "b": 2}
[x y z]           →  ["x", "y", "z"]
a:b:c:1           →  {"a": {"b": {"c": 1}}}
```

## Choose your runtime

| Runtime | Start here |
|---|---|
| **TypeScript / JavaScript** (canonical) | [`ts/README.md`](ts/README.md) |
| **Go** | [`go/README.md`](go/README.md) — most Go users want the [`jsonic`](go/jsonic/) package |

## Documentation

The docs are organised by what you are trying to do:

- **Learning the basics** — the tutorials walk you from an empty file to
  a working parse: [TypeScript tutorial](ts/doc/tutorial.md),
  [Go tutorial](go/doc/tutorial.md).
- **Getting a specific job done** — the how-to guides are task recipes:
  [TypeScript guides](ts/doc/guide.md), [Go guides](go/doc/guide.md),
  and plugin authoring ([TS](ts/doc/plugins.md), [Go](go/doc/plugins.md)).
- **Looking something up** — the reference describes every option, method,
  and syntax rule: the shared [syntax reference](doc/syntax.md), plus
  per-language API and options references
  ([TS api](ts/doc/api.md) / [options](ts/doc/options.md),
  [Go api](go/doc/api.md) / [options](go/doc/options.md)).
- **Understanding how it works** — the explanations cover the design:
  the shared [architecture](doc/architecture.md), and per-language concept
  notes ([TS](ts/doc/concepts.md), [Go](go/doc/concepts.md)). Go users
  porting from TS should read [differences](go/doc/differences.md).

## Repository layout

| Path | What it is |
|---|---|
| [`ts/`](ts/) | The canonical TypeScript engine (the `tabnas` npm package). |
| [`go/`](go/) | The Go port of the engine — grammar-free, same layout as TS. |
| [`go/jsonic/`](go/jsonic/) | The relaxed-JSON grammar for Go, as a plugin package plus a convenience API. |
| [`test/spec/`](test/spec/) | Shared `.tsv` conformance fixtures, run by both runtimes. |
| [`doc/`](doc/) | Language-neutral docs: the [syntax reference](doc/syntax.md) and the [architecture explanation](doc/architecture.md). |

Working on the codebase itself? Each directory has an `AGENTS.md` with
build, test, and contribution notes; start with [`AGENTS.md`](AGENTS.md).

## License

MIT. Copyright (c) Richard Rodger.
