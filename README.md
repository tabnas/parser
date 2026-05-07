# amagama

A pluggable parsing engine — a configurable rule-based parser over a
configurable matcher-based lexer. Grammar comes from plugins; the
runtime ships only the engine plus a small set of bundled plugins.

This monorepo contains:

| Path | Description |
|---|---|
| [`ts/`](ts/) | TypeScript / JavaScript implementation. The reference engine — ships the BNF and Debug plugins; bring your own grammar. A strict-JSON grammar lives as a test fixture under `ts/test/json-plugin.ts`. |
| [`go/`](go/) | Go port. Ships the engine plus a relaxed-JSON grammar (unquoted keys, implicit objects/arrays, comments, trailing commas, multiline strings, path diving). |
| [`test/spec/`](test/spec/) | Shared `.tsv` parser-spec fixtures, exercised by both runtimes. |

Start with [`ts/README.md`](ts/README.md) for the JS API or
[`go/README.md`](go/README.md) for Go.

## License

MIT. Copyright (c) Richard Rodger.
