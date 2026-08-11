# Divergences

TypeScript is the canonical implementation; the Go port tracks it. This
file is the single record of where the two ports produce a **different
result for the same input** — and, for each, whether that is deliberate.

It is deliberately short. Most of what differs between the ports is not a
divergence at all: packaging, API shape, Go-specific helpers and the
plugin surface are covered in
[`go/doc/differences.md`](go/doc/differences.md), which is a porting guide,
not a parity record. A reader asking "will these two engines agree on my
input?" should be able to answer it from this page alone.

## Why this matters more here than elsewhere

This engine is the root of a dependency graph. A divergence here reaches
every downstream grammar, in both runtimes, and downstream cannot fix it —
the value is already decided by the time a plugin sees a token. Two
consumers have carried shims against divergences in this file, and
[`rjrodger/aontu`](https://github.com/rjrodger/aontu) recorded one as a
lattice-law breach it could not repair from its own repository.

So the bar for adding an entry here is high: a divergence is a **bug**
until someone argues otherwise and is agreed with. The default response is
to fix the engine.

## Deliberate, permanent

### Lone surrogates in quoted strings

A UTF-16 surrogate that does not form a high+low pair with its neighbour:

| input | TypeScript | Go |
|---|---|---|
| `"\ud800"` | preserved, code unit `d800`, `isWellFormed()` false | `U+FFFD` |

TypeScript preserves it because JS strings are UTF-16 and permit it. Go
folds it to `U+FFFD`, matching `encoding/json`. Each port does the correct
thing for its own string model, and neither can adopt the other's without
either lying about what it stores (Go) or losing a capability (TS).

**The cost is real and should not be glossed.** Go conflates values TS
keeps distinct, so two sources denoting different values can compare equal
there. A downstream unifier reported exactly this as a lattice-law breach
(`aontu#24`): `x:"\ud800"` and `x:"�"` unify in Go and conflict in TS,
and as map keys the two entries silently merge.

Refusing an unpaired surrogate in both ports was considered and declined
(2026-08-11): in Go a refusal removes nothing anyone can depend on, since
the value is already destroyed, but in TS it removes a working capability.
Reopen `aontu#24` rather than changing one port quietly.

Pinned by `go/surrogate_pairing_test.go` and
`ts/test/surrogate-pairing.test.js`, which assert **opposite** results on
purpose, so changing either side fails loudly.

*Not* this entry: surrogate PAIRS, which agree in every spelling —
literal, `\u{1F600}`, `😀`, `\u{d83d}\u{de00}` and both mixed
forms. Three of those were broken in Go until 0.8.4 and are now pinned.

### Column positions for astral characters

Error columns count UTF-16 units in TypeScript (an astral character is 2)
and runes in Go (any character is 1). Forced by the scan unit — TS scans
UTF-16 code units, Go scans UTF-8 bytes — and visible only in error
positions, never in parsed values.

### Negotiated lexing (`lex.relex`) — TypeScript only

TS has an opt-in `lex: { relex: true }` that re-cuts a buffered token's
source span on a token-type mismatch instead of failing the alternate. Go
has no equivalent, so a Go grammar cannot opt in.

Impact is confined to **scannerless** front-ends. Grammars written for a
tokenising lexer distinguish their terminals lexically and never contest a
character, so `relex: false` — the TS default and Go's only behaviour — is
identical in both runtimes for every ABNF and JSON-family grammar in the
fleet. See [`go/doc/differences.md`](go/doc/differences.md) for the full
description and what a Go port would require.

## Not divergences

Recorded here because they are regularly mistaken for divergences:

- **Invalid UTF-8 input bytes.** Go passes them through byte-for-byte and
  never panics; the question does not arise in TS, whose strings are
  UTF-16 by construction.
- **Error message text.** Not in parity by design. Only the error `code` is
  contractual; hint wording and source frames differ.
- **Native integer type.** Go returns `int64`, TS a `number` or `bigint`
  depending on magnitude. The serialised bytes agree; the difference is
  forced by the storage, not chosen.
