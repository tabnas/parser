# tabnas

A pluggable parsing engine — a configurable rule-based parser over a
configurable matcher-based lexer. tabnas grew out of the jsonic use
case: lenient JSON for humans (unquoted keys, implicit objects,
comments, trailing commas, path diving). Grammar comes from plugins;
the TypeScript runtime ships only the engine.

This monorepo contains:

| Path | Description |
|---|---|
| [`ts/`](ts/) | TypeScript / JavaScript implementation. The canonical engine — bring your own grammar (companion BNF and Debug plugins live in separate repos). A strict-JSON grammar lives as a test fixture under `ts/test/json-plugin.ts`. |
| [`go/`](go/) | Go port. Ships the engine plus the relaxed-JSON (jsonic-style) grammar built in, so `tabnas.Parse` works out of the box. |
| [`test/spec/`](test/spec/) | Shared `.tsv` parser-spec fixtures, exercised by both runtimes. |

Start with [`ts/README.md`](ts/README.md) for the JS API or
[`go/README.md`](go/README.md) for Go. Working on the codebase?
See [`AGENTS.md`](AGENTS.md).

## License

MIT. Copyright (c) Richard Rodger.
