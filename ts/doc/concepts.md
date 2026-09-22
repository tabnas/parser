# Concepts

Background on how the TypeScript engine is put together, and why. This
is understanding-oriented reading, not a task list. For steps see the
[tutorial](tutorial.md) and [how-to guides](guide.md), and for exact
signatures see the [API](api.md) and [options](options.md) references.

The engine model (grammar-as-plugin, the lexer/parser split, the
open/close rule machinery, grammar-declared lookahead, instance derivation)
is shared by both runtimes and described once in
[../../doc/architecture.md](../../doc/architecture.md). This document
covers only what is specific to the TypeScript port.

## A parser is a class instance

In TypeScript the engine is a class, `Tabnas`. A parser is an
instance, created with `new Tabnas(options)` and configured by plugins.
Methods (`parse`, `use`, `rule`, `make`, …) hang off the instance, and
plugins may decorate instances with extra properties: the class
carries an index signature so TypeScript tolerates that.

This is the main shape difference from the Go port, which is
function-and-struct based. Most other differences follow from it: the
class can hold genuinely private state, and accessors can be both
callable and indexable at once.

## Dual-shape accessors

`options`, `token`, and `tokenSet` are each two things at once: a
function and a map.

- `tn.options(change)` applies a partial change; `tn.options()` returns
  a fresh snapshot; `tn.options.comment.lex` reads a single setting
  from the indexable view. The map view is refreshed after every set.
- `tn.token('#OB')` looks up or mints a Tin; `tn.token.OB` reads it
  from the map. The map is keyed by both `#XX` and bare `XX` forms, so
  grammar code can destructure `const { ST, TX } = tn.token`.
- `tn.tokenSet('VAL')` returns a set's Tin array; `tn.tokenSet.VAL` is
  the map form.

The dual shape exists so plugin code can read configuration ergonomically
(`tn.options.list.child`) while still having a setter, without two
separate members to keep in sync.

## `make()` re-runs plugins

`tn.make(options)` derives a child by constructing a new instance with
the current one as parent. The child does not copy the parent's rules
verbatim: it inherits the merged options, then **re-runs every plugin
the parent registered** against the child's options, and finally
applies `rule.include` / `rule.exclude` filtering.

Re-running matters because grammar can be option-conditional: an
alternate that only exists when `list.child` is set must be
re-evaluated for the child, not copied from a parent that had the flag
off. This is why plugins must be idempotent: they run again on every
derived instance.

## The scan-spec lexer

The lexer is matcher-based (one matcher per token kind, run in priority
order). The simpler matchers (space, line, string bodies, comment
tails) share a small **scan-spec driver**: a table-driven state
machine that walks bytes, classifies each, and emits position-tracking
actions. The driver (`scan`), the spec builders (`buildCharRunSpec`,
`buildLineRunSpec`, `buildStringBodySpec`), the `guardedMatcher`
wrapper, and the action constants (`CONSUME`, `IS_ROW`, `CI_RESET`,
`STOP`, `STATE_MASK`) are re-exposed through `Tabnas.util` so plugin
authors can build custom matchers on the same primitives. The types
`ScanSpec` and `ScanOut` are exported for typing them.

## Hash-private internals

Each instance keeps its working state (parser, compiled config,
plugin list, subscriptions) in a single ECMAScript hash-private field,
`#internal`. Being hash-private (not merely conventionally private), it
is invisible to `for...in`, `Object.keys`, `JSON.stringify`, and
tests: the instance presents only its public surface
(`fixed`, `id`, `options`, `parent`, `token`, `tokenSet`). The single
public reader is `tn.internal()`, which plugins use for the rare cases
the public API doesn't cover.

Errors lean on the platform too: `TabnasError` extends the built-in
`SyntaxError`, and its message is assembled by `{key}` template
injection (`strinject`) so it can be customised or localised through
the `error` / `hint` / `errmsg` options.

## Binary input

The engine never inspects a character as text. It indexes, compares code
units, and hands spans to matchers, so a binary format is a grammar
question rather than an engine question. The only crossing that needs
care is getting bytes into a JavaScript string: latin1 maps bytes 0 to
255 onto code units 0 to 255 one for one, so `buf.toString('latin1')` is
lossless and every position the engine tracks stays a byte offset.

What makes binary tractable is that the lexer is parser-directed. A
matcher registered under `match.token` runs only where the active rule's
expected-token column names its token (`rspec.def.tcol[oc][tI]`), and it
is handed the live rule. Text formats rarely need either property,
because a quote or a digit announces its own token kind. Binary formats
have no such signal: the same byte is a length here and a payload two
offsets later, and only the grammar knows which. Registering the same
matchers under `lex.match` instead puts them in an ungated
priority-ordered pipeline, where the first matcher that can consume a
byte takes it, and a fixed-width integer matcher swallows the first four
bytes of every variable-length payload.

Throughput is governed by tokens rather than by bytes. Holding total
input at 4 MB and varying only the payload size of a length-prefixed
frame (so, the token count) gives a flat rate of roughly 0.7 to 1.4
million lex calls per second on one development machine, while the byte
rate moves across three orders of magnitude:

| payload | lex calls | MB/s | M lex/s |
|---|---|---|---|
| 8 B | 699,052 | 3 | 0.55 |
| 128 B | 63,552 | 46 | 0.73 |
| 512 B | 16,258 | 198 | 0.81 |
| 8 KB | 1,024 | 5530 | 1.42 |

The design question is therefore how many tokens the grammar cuts, not
how many bytes it reads. Folding a whole record into one matcher, where
the grammar does not need to see inside it, measured about five times
faster than splitting the same record into four tokens across three
rules. Dropping the built-in matchers out of the pipeline rather than
only disabling them measured about 1.3 times faster again. Both of those
are grammar choices; the engine cost per token is what it is.

A token built with `src` undefined and an explicit `len` is a bare
`(sI, len)` span, and its `src` accessor materialises the substring on
first read. A payload that the grammar only needs to address, never to
inspect, can stay a span: keep the original `Buffer` on `ctx.meta` and
slice it with `ctx.meta.buf.subarray(tkn.sI, tkn.sI + tkn.len)`, which
shares memory rather than copying.

Two limits are structural. There is no streaming entry point, so a parse
holds its whole source as one string, and V8 caps that at 512 MB
(`require('buffer').constants.MAX_STRING_LENGTH`). And `Point` counts
bytes with no bit position, so a sub-byte field has to carry its own bit
offset on `ctx.u`.

The Go and Rust ports reach the same place by different routes, and the
differences are recorded in
[`go/doc/differences.md`](../../go/doc/differences.md) and
[`rs/README.md`](../../rs/README.md).

## Design notes

Longer-form explorations live alongside this doc:

- [LSP feasibility](lsp-feasibility.md). Language-server angles on the
  engine.
- [GBNF feasibility](gbnf-feasibility.md). Llama.cpp grammar
  (constrained LLM decoding) angles on the engine.

For the shared engine rationale, see
[../../doc/architecture.md](../../doc/architecture.md).
