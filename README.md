# amagama

Lenient JSON parsing engine. Accepts all standard JSON plus a wide
range of relaxations (unquoted keys, implicit objects/arrays, comments,
trailing commas, multiline strings, path diving, …) and lets you
customise the grammar through plugins.

This monorepo contains:

| Path | Description |
|---|---|
| [`ts/`](ts/) | TypeScript / JavaScript implementation. The reference engine. |
| [`go/`](go/) | Go port — same syntax, same results. |
| [`test/spec/`](test/spec/) | Shared `.tsv` parser-spec fixtures, exercised by both runtimes. |

Start with [`ts/README.md`](ts/README.md) for the JS API or
[`go/README.md`](go/README.md) for Go.

## License

MIT. Copyright (c) Richard Rodger.
