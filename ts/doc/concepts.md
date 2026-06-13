# Concepts

Background on how the TypeScript engine is put together, and why. This
is understanding-oriented reading, not a task list — for steps see the
[tutorial](tutorial.md) and [how-to guides](guide.md), and for exact
signatures see the [API](api.md) and [options](options.md) references.

The engine model — grammar-as-plugin, the lexer/parser split, the
open/close rule machinery, two-token lookahead, instance derivation —
is shared by both runtimes and described once in
[../../doc/architecture.md](../../doc/architecture.md). This document
covers only what is specific to the TypeScript port.

## A parser is a class instance

In TypeScript the engine is a class, `Tabnas`. A parser is an
instance, created with `new Tabnas(options)` and configured by plugins.
Methods (`parse`, `use`, `rule`, `make`, …) hang off the instance, and
plugins may decorate instances with extra properties — the class
carries an index signature so TypeScript tolerates that.

This is the main shape difference from the Go port, which is
function-and-struct based. Most other differences follow from it: the
class can hold genuinely private state, and accessors can be both
callable and indexable at once.

## Dual-shape accessors

`options`, `token`, and `tokenSet` are each two things at once: a
function and a map.

- `am.options(change)` applies a partial change; `am.options()` returns
  a fresh snapshot; `am.options.comment.lex` reads a single setting
  from the indexable view. The map view is refreshed after every set.
- `am.token('#OB')` looks up or mints a Tin; `am.token.OB` reads it
  from the map. The map is keyed by both `#XX` and bare `XX` forms, so
  grammar code can destructure `const { ST, TX } = am.token`.
- `am.tokenSet('VAL')` returns a set's Tin array; `am.tokenSet.VAL` is
  the map form.

The dual shape exists so plugin code can read configuration ergonomically
(`am.options.list.child`) while still having a setter, without two
separate members to keep in sync.

## `make()` re-runs plugins

`am.make(options)` derives a child by constructing a new instance with
the current one as parent. The child does not copy the parent's rules
verbatim — it inherits the merged options, then **re-runs every plugin
the parent registered** against the child's options, and finally
applies `rule.include` / `rule.exclude` filtering.

Re-running matters because grammar can be option-conditional: an
alternate that only exists when `list.child` is set must be
re-evaluated for the child, not copied from a parent that had the flag
off. This is why plugins must be idempotent — they run again on every
derived instance.

## The scan-spec lexer

The lexer is matcher-based (one matcher per token kind, run in priority
order). The simpler matchers — space, line, string bodies, comment
tails — share a small **scan-spec driver**: a table-driven state
machine that walks bytes, classifies each, and emits position-tracking
actions. The driver (`scan`), the spec builders (`buildCharRunSpec`,
`buildLineRunSpec`, `buildStringBodySpec`), the `guardedMatcher`
wrapper, and the action constants (`CONSUME`, `IS_ROW`, `CI_RESET`,
`STOP`, `STATE_MASK`) are re-exposed through `Tabnas.util` so plugin
authors can build custom matchers on the same primitives. The types
`ScanSpec` and `ScanOut` are exported for typing them.

## Hash-private internals

Each instance keeps its working state — parser, compiled config,
plugin list, subscriptions — in a single ECMAScript hash-private field,
`#internal`. Being hash-private (not merely conventionally private), it
is invisible to `for...in`, `Object.keys`, `JSON.stringify`, and
tests: the instance presents only its public surface
(`fixed`, `id`, `options`, `parent`, `token`, `tokenSet`). The single
public reader is `am.internal()`, which plugins use for the rare cases
the public API doesn't cover.

Errors lean on the platform too: `TabnasError` extends the built-in
`SyntaxError`, and its message is assembled by `{key}` template
injection (`strinject`) so it can be customised or localised through
the `error` / `hint` / `errmsg` options.

## Design notes

Longer-form explorations live alongside this doc:

- [BNF feasibility](bnf-to-tabnas-feasibility.md) — mapping BNF / ABNF
  onto the engine's rule model.
- [LSP feasibility](lsp-feasibility.md) — language-server angles on the
  engine.

For the shared engine rationale, see
[../../doc/architecture.md](../../doc/architecture.md).
