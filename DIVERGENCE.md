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

**That last sentence has been false at least twice, in the same way.**
`go/doc/differences.md` has a section headed *"Behavioral Differences —
These affect parse output for the same input"*, which is this file's own
definition of a divergence, and things that belonged here were filed
there instead: the rule-iteration budget (repaired, P7) and the `\s` /
`(?i)` regex non-equivalences (permanent, P8, and now recorded below).
Both were accurately DESCRIBED — and neither was pinned, because that
file is prose and this one is backed by tests in both ports. When adding
to either, the test is not "is this about the Go port?" but "can the two
engines produce a different result for the same input?" If yes, it
belongs here, whatever else is true about it.

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
Go's reports the string from its opening quote up to the escape. Same
error `code` — the contract holds — but the token source, and therefore
the diagnostic's `len`/`pos`/`col`, diverge even for pure-ASCII input:

| input | TypeScript | Go |
|---|---|---|
| `"\uZZZZ"` | code `invalid_unicode`, token src `\uZZZZ`, pos 1, col 2, len 6 | code `invalid_unicode`, token src `"\uZZZZ`, pos 0, col 1, len 7 |

This is span metadata on an already-agreed failure, not a disagreement
about the input's value: both ports reject the document with the same
code, and no parsed value exists to differ. Aligning the spans would mean
rewriting one lexer's error recovery to match the other's internal
matcher structure, for display-only gain. Consumers should treat
`len`/`pos`/`col` on bad-token errors as advisory and anchor on `code`
(and `row`, which agrees).

Pinned by `ts/test/divergence.test.js` and `go/divergence_test.go`
(`TestDivergenceBadEscapeSpanIncludesQuote`), which assert **opposite**
spans on purpose, so changing either side fails loudly.

### Rule-iteration budget: a fractional `rule.maxmul`

The runaway guard's multiplier is a `number` in TypeScript and a `*int` in
Go, so a value between 0 and 1 shrinks the budget in one port and cannot
be written in the other.

| options | TypeScript | Go |
|---|---|---|
| `rule.maxmul: 0.01`, 61-element array | `ERROR unexpected` | not expressible; a shared options blob reaches `*int` as `0`, which coerces to the default `3`, and parses |

Everything else about this guard is aligned, and was not: both ports used
to reject valid input at one end of `maxmul` — TypeScript by honouring a
zero or negative multiplier literally, Go by wrapping the product and
meeting its own floor of 100. Repaired; see "Rule-Iteration Budget" in
[`go/doc/differences.md`](go/doc/differences.md) and audit item P7.

Not repaired here, because the fix is to the option's TYPE. Narrowing
TypeScript's `maxmul` to an integer would break callers for a setting
nobody tunes fractionally, and widening Go's would put a float in a
loop counter. The floor of 100 bounds the damage: a fractional multiplier
cannot make a SHORT parse fail in either port.

Pinned by `ts/test/rule-budget.test.js` ('a fractional maxmul is
expressible here and not in Go') alongside the aligned cases, so the two
are read together.

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
