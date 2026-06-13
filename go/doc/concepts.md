# Concepts (Go)

Background reading for the Go packages: how they are split, the
guarantees they make, and the mechanics that are specific to Go. For
the language-neutral engine model — lexer, parser, rules, alternates —
read the shared [architecture](../../doc/architecture.md) first; this
document only covers what is Go-specific. For the behavioral
TypeScript ↔ Go comparison, see [differences](differences.md).

## Two packages: engine and grammar

The module ships two packages:

- `github.com/tabnas/parser/go` (package `tabnas`) — the parsing
  *engine*. It ships **no grammar**. On its own it lexes and runs
  rules, but a fresh `tabnas.Make()` has no rules to run.
- `github.com/tabnas/parser/go/jsonic` — the relaxed-JSON grammar,
  supplied as a plugin (`jsonic.Plugin`) plus convenience
  constructors (`Parse`, `Make`, `MakeJSON`).

This mirrors the canonical TypeScript package, where every grammar —
even strict JSON — arrives as a plugin rather than being baked in. The
split keeps the engine reusable: the same lexer and rule machinery
drive relaxed JSON, strict JSON, or any grammar you write, depending
only on which plugin is installed. The relaxed-JSON behavior that
gives the project its name is just the most common plugin, not a
privileged built-in.

A practical consequence: most Go clients import `jsonic` and never
touch the engine directly. You reach for the bare `tabnas` package
only when you are authoring a grammar (see the [plugin
guide](plugins.md)).

One Go-specific wrinkle of the split: the engine's text-form
convenience APIs (`SetOptionsText`, `GrammarText`) need a parser for
their text argument, but the engine has no grammar. A grammar package
registers one via `tabnas.RegisterTextParser`; importing `jsonic` does
this automatically in its `init`, in the manner of database/sql
drivers.

## The no-panic guarantee

The Go API never panics. Every failure — malformed input, a buggy
plugin, an internal engine fault — is delivered as a returned `error`,
never as a panic that crosses the package boundary. This is a
deliberate guarantee, enforced in several places:

- **Parsing** wraps a `recover` guard. Any panic raised during a
  parse — including panics thrown from plugin callbacks or custom
  matchers — is converted into a `*TabnasError` with code `internal`,
  and returned. A `FuzzParse` fuzz target exercises this against
  arbitrary byte input.
- **`Grammar`** has the same guard: a malformed declarative spec
  produces an `internal` error rather than a panic.
- **APIs that could fail return errors instead of panicking.**
  `Derive` returns `(*Tabnas, error)` — a plugin that fails while the
  child is being derived surfaces as that error (mirroring TypeScript
  `make()` throwing). `MakeRuleCond` returns `(AltCond, error)` for
  unknown operators. The `Plugin` type itself returns `error`, and
  `Use` / `UseDefaults` propagate it.

When you see an `internal`-code error, it signals a bug in tabnas or a
plugin, not in your input — the hint text says as much.

## UTF-8 and column semantics

Input is treated as UTF-8. All character sizes (1–4 byte sequences;
both the BMP and the astral planes) work in keys, values, strings,
comments, and escapes. Any Unicode character may also be used as a
configured matcher character — a space char, line char, quote char,
or ender — not just ASCII.

Two mechanical points specific to the Go runtime:

- **Columns count runes, not bytes or UTF-16 units.** An error column
  (`TabnasError.Col`) is a 1-based rune offset, so an astral-plane
  character counts as one column. (TypeScript counts UTF-16 units, so
  the same character counts as two there — the one place columns
  disagree between the runtimes.)
- **Invalid UTF-8 input bytes are passed through byte-for-byte** and
  never trigger a panic, upholding the no-panic guarantee even on
  arbitrary binary input. Lone surrogates from `\uXXXX` escapes become
  U+FFFD, matching `encoding/json`.

See [differences](differences.md#unicode--utf-8) for the full
TypeScript ↔ Go Unicode comparison.

## The scan-spec lexer

The simpler matchers — space, line, comment eat-line tails, and the
string body — do not each hand-roll their byte walk. They share a
small **scan-spec driver** (`scan.go`): a table-driven state machine
that walks bytes, classifies each one into a small set of classes, and
emits packed position-tracking actions (consume, advance row, advance
column, stop). The space/line/string specs are built by
`BuildCharRunSpec`, `BuildLineRunSpec`, and `BuildStringBodySpec`, and
cached per configuration.

ASCII bytes classify through a fast 256-entry table; bytes ≥ 0x80
decode a full rune and classify it through a fallback function,
consuming the whole rune as a single column — which is how the
rune-based column counting above is achieved on the hot path. These
primitives are exported (see the [API reference](api.md#scan-primitives))
so grammar authors can build their own matchers on the same machine.
This design is shared with the TypeScript lexer; the engine difference
is the scan unit (UTF-8 bytes here, UTF-16 code units there).
