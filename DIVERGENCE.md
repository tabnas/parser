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
positions, never in parsed values. The `pos` field of the structured
diagnostic (`schema/diagnostic.schema.json`) carries the same divergence —
a 0-based offset in UTF-16 units (TS) versus runes (Go). The diagnostic's
`len` deliberately counts Unicode code points OF THE TOKEN SOURCE, so the
string-unit arithmetic never diverges; `len` can still differ where the
two lexers cut different token SPANS (next entry).

The same scan unit shows in the error token synthesized for an UNCLAIMED
astral character (one no matcher can produce): both ports name it `#BD`
with `len` 1, but its `src` is one UTF-16 unit in TS (a lone high
surrogate) and one rune in Go (the whole character). Pinned with opposite
assertions by `ts/test/diagnostic.test.js` ('unclaimed-char-token') and
`go/diagnostic_test.go` (`TestDiagnosticUnclaimedCharToken`).

### Bad-token spans for invalid string escapes

The two lexers cut a DIFFERENT bad token for the same invalid escape:
TypeScript's string matcher reports the offending escape sequence itself,
Go's reports the string from its opening quote up to the escape. The token
source, and therefore the diagnostic's `len`/`pos`/`col`, diverge even for
pure-ASCII input:

| input | TypeScript | Go |
|---|---|---|
| `"\uZZZZ"` | code `invalid_unicode`, token src `\uZZZZ`, pos 1, col 2, len 6 | code `invalid_unicode`, token src `"\uZZZZ`, pos 0, col 1, len 7 |

The mechanism is one the ports state differently rather than compute
differently. TypeScript moves the lex point onto the offending construct
before raising (`pnt.sI = sI; pnt.cI = cI` at each raise site in its
string matcher); Go leaves the point at the opening quote and spans from
`l.pnt.SI`. Five raise sites, one behaviour.

**And the code does NOT always agree.** An earlier version of this entry
said "same error `code` — the contract holds" and told consumers to
anchor on it. That is false for a truncated escape at end of input, where
the two ports disagree about which failure they are even looking at:

| input | TypeScript | Go |
|---|---|---|
| `"\x4` | **`unterminated_string`** | **`invalid_ascii`** |
| `"\u4` | **`unterminated_string`** | **`invalid_unicode`** |

TypeScript sees a string that ran out before its closing quote; Go sees an
escape that ran out of hex digits. Both are defensible readings and only
one can be the contract. Measured across the whole family — `"\x`,
`"\u`, `"\u{42`, `"\xZZ"`, `"\uZZZZ"` all agree on the code; only the
two shapes above, where SOME hex digits are present but not enough,
disagree.

So: `row` agrees everywhere. `len`/`pos`/`col` are advisory on this path.
`code` is reliable EXCEPT for a truncated escape at end of input, which is
the one case a consumer branching on `code` must not assume.

Pinned by `ts/test/divergence.test.js` and `go/divergence_test.go`
(`TestDivergenceBadEscapeSpanIncludesQuote`), which assert **opposite**
spans on purpose, so changing either side fails loudly.

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
- **The `u` flag on a serialized regex terminal.** A shared `@/…/u` carries
  a JavaScript flag; Go drops it, because RE2 is natively rune-based and so
  already behaves as `u` asks JavaScript to behave. Verified case by case
  and pinned in BOTH runtimes — see "Serialized Regex Flags" in
  [`go/doc/differences.md`](go/doc/differences.md). The flags that are not
  no-ops (`v`, and anything unrecognised) are refused rather than dropped.
