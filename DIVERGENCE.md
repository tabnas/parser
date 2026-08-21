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

**This file is prose, and prose rots.** Every entry below that carries
its own `###` heading is therefore also REGISTERED, per ADR-14, in
[`test/spec/divergent.tsv`](test/spec/divergent.tsv): a row per case, a
column per runtime, asserted by BOTH suites. A divergence that gets
repaired fails that register as loudly as one that regresses, so the row
— and the entry here — must then be deleted. Where an entry cannot be
registered yet it is declared, with a reason, in the `notRegistered` map
in `go/divergent_test.go`; today that is one entry, the fractional
`rule.maxmul` below, which needs a full value grammar no probe builds
yet. A gate in the same file fails if an entry here gains no row and no
exemption, or if an exemption outlives the entry it exempts.

When this file and the register disagree, **the register is what runs**.
Fix this file to match it, never the other way round.

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
UTF-16 code units, Go scans UTF-8 bytes. The `pos` field of the structured
diagnostic (`schema/diagnostic.schema.json`) carries the same divergence —
a 0-based offset in UTF-16 units (TS) versus runes (Go).

That sentence was aspirational until recently: Go emitted a BYTE offset,
so `pos` diverged for every character above U+007F rather than only above
the BMP. Both this file and the schema described runes, which told a
BMP-only consumer that `pos` was as safe as `col`. Repaired at the
marshal boundary — where `len` had always converted for the same reason —
so the description above is now what the code does. Audit item P5.

The diagnostic's `len` deliberately counts Unicode code points OF THE
TOKEN SOURCE, so the string-unit arithmetic never diverges; `len` can
still differ where the two lexers cut different token SPANS (next entry).

The same scan unit shows in the error token synthesized for an UNCLAIMED
astral character (one no matcher can produce): both ports name it `#BD`
with `len` 1, but its `src` is one UTF-16 unit in TS (a lone high
surrogate) and one rune in Go (the whole character). Pinned with opposite
assertions by `ts/test/diagnostic.test.js` ('unclaimed-char-token') and
`go/diagnostic_test.go` (`TestDiagnosticUnclaimedCharToken`).

### Token offsets reach parsed values, not only diagnostics

**This entry exists because the one above used to end "and visible only
in error positions, never in parsed values". That was false.** `Token.SI`
is part of the plugin API, and a plugin that records it lands the scan
unit directly in its output.

Measured through `@tabnas/c`, whose CST carries a `span` built from
`tkn.SI`, on the input `["\u{1F600}" 1]`:

| token | TypeScript | Go |
| --- | --- | --- |
| `PUNC_LBRACKET` | 0→1 | 0→1 |
| `LIT_STRING` | 1→**5** | 1→**7** |
| `LIT_INT` | **6**→7 | **8**→9 |
| `PUNC_RBRACKET` | 7→8 | 9→10 |

TypeScript counts the astral character as 2 UTF-16 units; Go counts its 4
UTF-8 bytes. **Go's `Token.SI` is a byte offset — not a rune offset**,
which is what the column entry above describes for error positions after
conversion. Every token after a non-ASCII one is displaced.

Found by `tasks/ax-parity-probe` in tabnas/admin, once `@tabnas/c` gained
the `pluginKind: "grammar"` descriptor field that had been keeping it out
of the probe: 5 disagreements of 23 inputs, every one an input containing
a non-ASCII character, and `@tabnas/expr` is exposed the same way.

Not repairable in a plugin: converting offsets there would need the
source and would still leave `tkn.SI` itself divergent for anything else
reading it. The repair is the engine's scan unit, which is the same
change the column entry above defers. Recorded rather than fixed, and the
scope sentence corrected so the next reader is not told this cannot reach
a parsed value.

## Repaired, and what replaced them

An entry that leaves this file should leave a forwarding address: a
reader who remembers one and cannot find it needs to know whether it was
fixed or quietly dropped.

- **An alt action running after that alt's own error.** Go's `Process`
  keeps going past an `alt.E` raise — deliberately, since the parser
  loop only observes `ctx.ParseErr` after `Process` returns — so the
  alt's `A` still ran and its mutations stuck. TS throws at the raise
  site and never reaches the action. Harmless while a raised error
  always discarded the value; visible as soon as recovery began
  returning a partial one:

  | input | grammar | TypeScript | Go |
  |---|---|---|---|
  | `42` | `top: {s:'#NR', e:@boom, a:@mutate}` | `undefined` | `"after-error"` |

  The action is now skipped when THAT alt's own `E` raised. Only its own
  raise counts: an error already standing from elsewhere does not
  suppress it, because TS would have thrown before reaching the alt at
  all, so there is no canonical behaviour to match and skipping would
  silence actions that legitimately run. The diagnostic context was
  already snapshotted at the raise site for the same underlying reason;
  this extends that compensation to the node. Pinned by
  `TestAltActionSkippedAfterItsOwnError` and the TS case "an alt action
  does not run after its own error".

- **The value kept when recovery gives up.** Two defects, same fallback,
  both Go-only and both repaired. (1) A give-up inside a structure
  leaves the root open, and Go returned `nil` where TS returns the most
  complete partial container — `[1 : abc def ghi]` gave `[1]` in TS and
  `null` in Go. (2) The fallback then consulted only Go's
  replacement-chain `resRule`, which is the right answer for a parse
  that COMPLETED but is not TS's `ctx.root()`: the latter is the rule
  the parse started with, set once and never followed through
  replacement. A start rule whose node was set before it was replaced
  therefore survived in TS and was skipped in Go —
  `rule.start "top"`, node `"old"`, replaced by `val`, on the same
  input: TS `"old"`, Go `[1]`. Go now prefers the original root, then
  the outermost active rule, then `ctx.rule`, matching TS's order, and
  tests both missing-node cases with a nullish predicate rather than
  `IsUndefined`, since TS's `null ==` covers null and undefined alike.

  Pinned in both ports (`TestRecoverGiveUpKeepsPartialValue`,
  `TestRecoverGiveUpWithReplacedStartRule`,
  `TestRecoverGiveUpTreatsNilRootAsMissing` and their TS mirrors).
  A note for whoever writes the next fixture here: `rule.start` is
  load-bearing in the replacement cases. Leave it at the default and
  the custom start rule is never entered, its before-open action never
  runs, and the case silently degenerates into an ordinary parse that
  agrees in both ports — which is how the second defect was first
  measured as a non-divergence and nearly dismissed.
- **Builtin config reaching a child rule in Go.** Carried a table
  showing that a parent declaring `k: {value$: {from: 1}}` WITHOUT
  running the builtin handed it to a child running `@value$` bare — `4`
  in Go against TypeScript's `3` for the same function-free serialized
  grammar. Go's builtins read `r.K`, and `k` propagates on push and
  replace; TypeScript's read the matched alternate.

  Repaired by ruling #120's A1: config is bound when the GRAMMAR LOADS,
  so it never enters `r.K` and there is one regime — the alternate that
  declares the config is the alternate that gets it. Go's five
  delete-after-read calls went with it; they were containment for a
  design that no longer exists, and were themselves a third scoping
  semantics (consumed-once here, alternate-scoped there), which is why
  the run-then-push shape used to agree for the wrong reason.

  Kept as PARITY tests rather than deleted —
  `TestBuiltinConfigIsAlternateScoped` in both ports, over the same two
  shapes. Both now answer `3`. The pair is worth keeping because the
  regression is silent: restoring an `r.K` read would leave every
  fleet grammar working, since all four declaration sites pair a config
  with its action on the same alternate.

- **Bad-token spans and codes for invalid string escapes.** Carried a
  table of `len`/`pos`/`col` differences and, at one point, the claim
  that the error `code` always agreed. Both halves are repaired: the
  TypeScript escape decode now requires the full fixed-width hex run
  (it accepted any prefix, so `"\x4Z"` parsed as U+0004 with the `Z`
  discarded), and the Go string matcher now positions its errors on the
  offending construct rather than the opening quote. Swept 32 inputs for
  the first and 19 for the second: 0 diverge.

  Kept as PARITY tests rather than deleted —
  `TestEscapeDecodeIsStrict` and `TestStringErrorsPointAtTheConstruct`
  in both ports. Both defects are easy to reintroduce and silent when
  they are: a plain `parseInt` is the obvious way to write the decode,
  and dropping the point-move leaves the codes right and only the
  positions wrong.

### Rule-iteration budget: a fractional `rule.maxmul`

The runaway guard's multiplier is a `number` in TypeScript and a `*int` in
Go, so a value between 0 and 1 shrinks the budget in one port and cannot
be written in the other.

| options | TypeScript | Go |
|---|---|---|
| `rule.maxmul: 0.01`, 61-element array | `ERROR unexpected` | not expressible; through an options map it truncates to `0`, which coerces to the default `3`, and parses |

That Go column was not true when first written: `MapToOptions` handled
`rule.start`, `finish`, `include` and `exclude` and dropped `maxmul`
entirely, so a shared options blob set the multiplier in TypeScript and
left Go on its default with nothing to notice. Plumbed, and pinned by
`go/rule_budget_test.go` `TestMaxMulSurvivesTheOptionsMap`. `maxmul` is
the only numeric option that path carries; the others (`rewind.history`,
the `error.recover` caps, `parse.budget.checkEveryN`) are still dropped,
which is an API gap rather than a divergence and is noted in
[`go/doc/differences.md`](go/doc/differences.md).

Everything else about this guard is aligned, and was not. Three separate
ways it produced a different result for the same input, all repaired:
TypeScript honoured a zero or negative multiplier literally; Go wrapped
the product and met its own floor of 100, so a LARGER multiplier was a
STRICTER guard; and the two ports measured source length in different
units — UTF-16 code units in TypeScript, bytes in Go — so any source
above U+007F got a different budget in each. See "Rule-Iteration Budget"
in [`go/doc/differences.md`](go/doc/differences.md) and audit item P7.

Not repaired here, because the fix is to the option's TYPE. Narrowing
TypeScript's `maxmul` to an integer would break callers for a setting
nobody tunes fractionally, and widening Go's would put a float in a
loop counter. The floor of 100 bounds the damage: a fractional multiplier
cannot make a SHORT parse fail in either port.

Pinned by `ts/test/rule-budget.test.js` ('a fractional maxmul is
expressible here and not in Go') alongside the aligned cases, so the two
are read together.

### Regex dialect in serialized terminals

A grammar spec can carry a match token as a serialized regex
(`"#WS": "@/^\\s+/"`). Each runtime compiles it with its own engine — JS
`RegExp` in TypeScript, RE2 in Go — and the two dialects disagree on two
constructs. **It diverges in both directions.**

| pattern | input | TypeScript | Go |
|---|---|---|---|
| `@/^\s+/` | U+00A0 NBSP | accepted | **rejected** |
| `@/^\s+/` | U+2028, U+2000, U+3000, U+FEFF | accepted | **rejected** |
| `@/^\s+/` | U+0020, U+0009 | accepted | accepted |
| `@/^k/i` | `k`, `K` | accepted | accepted |
| `@/^k/i` | U+212A KELVIN SIGN | **rejected** | accepted |

JS `\s` is Unicode-aware; RE2's is the Perl class `[\t\n\f\r ]`. JS `/i`
without `u` does not fold U+212A to `k`; RE2 case-folds by Unicode rules
and does.

**And two constructs that do not diverge in the result but in whether the
grammar loads at all**, which for a grammar author is worse:

| pattern | TypeScript | Go |
|---|---|---|
| `@/^(?=x)x/` (lookahead) | installs, matches `x` | **install error** |
| `@/^(a)\1/` (backreference) | installs, matches `aa` | **install error** |

RE2 implements neither, by design — both need backtracking. `go/utility.go`
refuses them at compile time and `Grammar()` reports it, which is the right
failure mode, but it means a spec written and tested against TypeScript can
be unloadable in Go. The `v` flag is the same story. Treat "compiles in JS"
as no evidence that a serialized terminal is portable.

**This is recorded rather than fixed, and the reason is worth stating
plainly, because the two halves are not equally hard.**

`\s` is mechanically repairable: a compile-time rewrite could expand it to
the explicit JS class before handing the pattern to RE2. That is not free —
it means this engine ships a regex-dialect translation layer, which has to
parse enough of the pattern to know a `\s` inside a character class from a
`\\s` that is a literal backslash, and it then owns that translation for
every downstream grammar in both runtimes. `(?i)` is not repairable the same
way: RE2 has no ASCII-only case-folding flag, so matching JS would mean
rewriting the pattern into explicit alternations.

Adding the layer for one of the two is a decision for the maintainer, not a
mechanical fix, so it is written down here with the cost attached instead of
being taken unilaterally.

**The workaround, measured rather than assumed — and spelled the JS way.**
An explicit class in the serialized terminal makes the two agree exactly:

```
@/^[\t\n\v\f\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]+/
```

**The `\uXXXX` spelling is load-bearing.** A serialized pattern is JS source:
TypeScript hands it to `new RegExp`, and Go lowers it to RE2. Writing the
class the RE2 way (`\x{00a0}`) makes it a *SyntaxError in TypeScript* — a
"portable" workaround that only works in one runtime, which is the very
defect this entry records. The first draft of this entry had it that way
round; it was caught in review, which is why the workaround is now pinned in
both runtimes rather than only written down.

With the pattern above both runtimes accept U+0020, U+0009, U+00A0, U+2028,
U+2000, U+3000 and U+FEFF and both reject `A` — the verdicts TS gives for
`\s`. Prefer it over `\s` in any serialized terminal a Go runtime will
compile.

Pinned by `go/divergence_test.go` `TestDivergenceRegexDialect` and the
matching case in `ts/test/divergence.test.js`, which assert **opposite**
results on purpose. Both drive a real `GrammarSpec` through
`grammar()` / `Grammar()` and parse: the gap was previously known only at
the regex-engine layer, so "a shared grammar that depends on either will
differ" was a prediction until this was wired.

### An explicitly empty option cannot be expressed in Go

**Deferred, not deliberate** — this is a defect awaiting a breaking
change, recorded here so consumers are not told it does not exist.

`String.Chars`, `String.MultiChars`, `String.EscapeChar`, `Space.Chars`,
`Line.Chars` and `Line.RowChars` are plain `string` in the Go options, so
`""` is their zero value and an explicitly empty value is
indistinguishable from an unset one. Each config branch tests `!= ""` and
restores the default when empty.

TypeScript distinguishes `''` from `undefined` and honours it; Go cannot,
so the defaults stay in force.

Six fields, not four: `Line.RowChars` and `String.EscapeChar` were missed
on the first pass, and they are not cosmetic — `rowChars: ''` changes
reported POSITIONS and `escapeChar: ''` changes string-token CONTENT.

The consequence is a **different result for the same input** whenever a
plugin configures its lexer that way. `@tabnas/css` declared
`string: { chars: '' }` in both ports:

| input | TypeScript | Go |
| --- | --- | --- |
| `a"b` | `jsonic/unexpected` | `jsonic/unterminated_string` |

The repair is `Chars *string`, matching `Lex`, `AllowUnknown` and
`EscapeStrict`, which are pointers for exactly this reason. It is a
breaking change across a published module, so it is outstanding rather
than done — and the blocking constraint is specific enough to write down.

The adoption cost, counted across the fleet excluding tests:

| repo | affected call sites |
| --- | --- |
| `parser` | its own config branches |
| `jsonic` | 3 |
| `ini` | 3 |
| `json`, `chess`, `zon` | 2 each |
| `css`, `csv`, `yaml` | 1 each |

**Consumers are not broken by the merge.** They pin the engine — `ini`,
`zon`, `csv` and `yaml` all require `parser/go v0.8.10` — so a type change
on `main` reaches them only when someone bumps that pin. The fifteen call
sites are an ADOPTION cost paid at upgrade, not a coordination cost paid
at merge.

That makes this a **release-policy decision** rather than a scheduling
puzzle: whether to spend a breaking bump on it, and when. Not a call to
make as a side effect of a parity sweep, which is why it is recorded here
instead of done.

A non-breaking half-measure exists and is deliberately not taken: an
additive `CharsSet *string` preferred when non-nil would give Go a way to
SAY "no quote characters" without changing any existing caller. It closes
the capability gap and leaves the divergence — `Chars: ""` would still
mean two different things in the two ports — so it trades a recorded
defect for an unrecorded one plus a second way to spell the same option. `TestEmptyCharsMeansUnset` pins the current
behaviour and **fails when the repair lands** — the signal to delete this
entry along with it.

Sibling ports are not all exposed: of the four call sites in the fleet
that set an empty value, only css's was live. `csv` sets `Lex: false`
alongside; `json` and `chess` set `MultiChars: ""` where the backtick was
never a quote character to begin with.

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
