# Divergences

TypeScript is the canonical implementation; the Go and Rust ports track it. This
file is the single record of where the runtimes produce a **different
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
column per runtime, asserted by every runtime suite. A divergence that gets
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

| input | TypeScript | Go | Rust |
|---|---|---|---|
| `"\ud800"` | preserved, code unit `d800`, `isWellFormed()` false | `U+FFFD` | `U+FFFD` |

TypeScript preserves it because JS strings are UTF-16 and permit it. Go and
Rust fold it to `U+FFFD`, matching their Unicode-scalar string models. Each port does the correct
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
and scalar values in Go and Rust (any character is 1). Forced by the scan unit — TS scans
UTF-16 code units, while Go and Rust scan UTF-8. The `pos` field of the structured
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

| token | TypeScript | Go | Rust |
| --- | --- | --- | --- |
| `PUNC_LBRACKET` | 0→1 | 0→1 | 0→1 |
| `LIT_STRING` | 1→**5** | 1→**7** | 1→**7** |
| `LIT_INT` | **6**→7 | **8**→9 | **8**→9 |
| `PUNC_RBRACKET` | 7→8 | 9→10 | 9→10 |

TypeScript counts the astral character as 2 UTF-16 units; Go and Rust count its 4
UTF-8 bytes. **`Token.SI` is a byte offset in Go and Rust — not a scalar offset**,
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

### Key order in parsed objects

Map key order is **out of the parsed-value contract** (ADR-15, admin
`DECISIONS.md`, accepted 2026-08-19). Each runtime builds its result in
its own native container and keeps whatever order that container keeps:

| input | TypeScript | Go | Rust |
| --- | --- | --- | --- |
| `{"2":"b","1":"a"}` | `{"1":"a","2":"b"}` | `{"2":"b","1":"a"}` | `{"2":"b","1":"a"}` |
| `{"10":"j","9":"i","2":"b"}` | `{"2":"b","9":"i","10":"j"}` | `{"10":"j","9":"i","2":"b"}` | `{"10":"j","9":"i","2":"b"}` |
| `{"b":1,"2":"two","a":2,"0":"zero"}` | `{"0":"zero","2":"two","b":1,"a":2}` | `{"b":1,"2":"two","a":2,"0":"zero"}` | `{"b":1,"2":"two","a":2,"0":"zero"}` |

TypeScript builds a plain object, so `[[OwnPropertyKeys]]` yields
canonical array-index keys first in ascending numeric order and the
remaining string keys in creation order. Go's `*OrderedMap` and Rust's
`Value::Object` (`Arc<IndexMap<String, Value>>`, `rs/src/value.rs`) keep
insertion order. All three are intended.

**The Rust consequence, stated in as many words** (from
`doc/rust-port-implementation-plan.md`): the Rust engine may use
`IndexMap` and insertion order freely; **no ECMAScript integer-key
emulation, ever.** The same holds for Go. A port that reorders keys to
match JavaScript is implementing a defect, not parity. This has been
rediscovered three times as a suspected fleet-wide defect (jsonic U6,
then `ini`, then `jsonic-cli`), and one emulation was written and
reverted before ADR-15 was found, which is why the prohibition is
written here where the next reader will look first.

**No shared fixture can detect this**, and none should try. The fixture
loaders compare objects by key membership, and the register's `spec`
probe renders map keys sorted by UTF-16 code unit in every runtime for
exactly this reason, so the difference is invisible to every suite in
the fleet. Prose is the only place it can live; the pins below assert
each runtime's own order so a port that starts emulating another's
fails loudly. Pinned by `ts/test/divergence.test.js` ('integer-like keys
sort first here, and stay in source order in Go and Rust'),
`go/divergence_test.go` `TestKeyOrderIsInsertionOrder` and
`rs/tests/divergent_spec_test.rs` `key_order_is_insertion_order`.

### A parse that sets no value

A parse whose rules match the whole source but never set a node
answers with each runtime's native absent value:

| input | grammar | TypeScript | Go | Rust |
| --- | --- | --- | --- | --- |
| `a` | `top: {s:'#A'}` and no action | `undefined` | `nil` | `Value::Null` |

Surfaced by `tabnas/json5` as `# c` under `{hashComment: true,
requireValue: false}` (#196). It is not `lex.emptyResult`, which is the
answer for an EMPTY source and agrees in all three; it is what an
unset node becomes at the parse boundary.

Deliberate, on both sides. TypeScript's `undefined` is JavaScript's
absent value. Go's public value model is `any` over the JSON shapes,
and Rust's public `Value` has no absent member once the engine is done
with it: both ports carry an engine-internal `Undefined` sentinel and
both **unwrap it at the parse boundary, recursively**
(`UnwrapUndefined` in `go/rule.go`, `Value::unwrap_undefined` in
`rs/src/value.rs`), so an unset element inside a container becomes
`null` there exactly as `JSON.stringify` renders it from TypeScript.
Returning the sentinel from `Parse` alone would make the top level
disagree with every nested level, and Go's sentinel cannot pass
through `encoding/json` at all. The repository's own parity rendering
already folds `undefined` and `null` into one value (`divergentCanon`
in both register runners), so the shared corpus cannot express the
difference and does not need to.

The cost: a grammar that distinguishes "parsed a null" from "parsed
nothing" can do so in TypeScript and not in the ports, which is why
`json5` normalises the former to `null` in all three plugins rather
than reading the engine's answer. Pinned with opposite assertions by
`ts/test/divergence.test.js` ('a parse that sets no value is undefined
here, nil in Go, Null in Rust'), `go/divergence_test.go`
`TestNoValueParseIsNil` and `rs/tests/divergent_spec_test.rs`
`no_value_parse_is_null`.

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

- **The serialized options door was untyped.** An ill-typed leaf in a
  spec's `options` (`line.chars: {}`, `tokenSet.VAL: "str"`,
  `string.escapeChar: []`) crashed TypeScript with a raw `TypeError`
  from inside `configure()` and was dropped in silence by Go, and a
  FuncRef resolved in ANY slot, so `@node$` could land in `rule.start`
  (#143). Ruled, per the D4 proposal on #130: an ill-typed leaf is a
  load fault naming the leaf, in every runtime, on every door (the
  constructor, `options()`, and a serialized grammar), and a function
  reference resolves only in a declared code slot; in a data slot it is
  the same fault. TypeScript validates the overlay against the shape of
  its defaults before merging; Go validates the map against `Options` by
  reflection inside `OptionsFromMap`; Rust, whose door already refused
  ill-typed leaves, now refuses a resolvable reference in a data slot
  and applies the `@@` escape door-wide. Pinned by
  `ts/test/options-validate.test.js`, `go/options_validate_test.go` and
  `rs/tests/grammar_spec_test.rs` (`a_reference_in_a_data_slot_is_a_load_fault`).

- **The options overlay: slices, definition maps and `tokenSet`.** On
  Go's typed options path, a slice (`ender`, `result.fail`, the
  recovery sync lists, `match.tokenOrder`), a map of definitions
  (`comment.def`, `value.def`, `match.value`) and `tokenSet` each
  REPLACED the default, where TypeScript merges index-wise, recurses
  into the entry, and merges the set index-wise (#151). Invisible to
  any fixture that drove `deep`/`Deep` directly, where the two agree;
  measured on the options pipeline as `css` and `zon` carrying dead
  `tokenSet` declarations whose Go twins worked as a replace, and three
  Go fleet packages carrying workarounds naming the engine merge. Ruled
  as TypeScript's semantics for all three, and Go moves: its instances
  now start from `DefaultOptions()` so the overlays have the same base
  to merge onto, an empty name in a `TokenSet` slice is the removed
  position TypeScript spells `null`, and the strict-JSON fixture spells
  its `KEY` replacement with explicit removals in both runtimes. Rust's
  serialized `tokenSet` door replaced too and now merges index-wise.
  Pinned by `go/options_overlay_test.go`,
  `ts/test/options-overlay.test.js` and
  `rs/tests/options_overlay_test.rs`, which drive the options pipeline.

- **The order of a matched alternate's hooks, and when `consumed` is
  read.** Two both-silent splits ruled before a third runtime
  transcribed one side. TypeScript ran the alternate's error hook `e`
  inside `parse_alts`, before the modifier `h`; Go ran `H` then `E`;
  Rust ran one modifier form before `e` and the other after it. No
  shipped grammar declares both, so nothing observed it (#154). Ruled
  as Go's order, the straight line: the routing forms `p`/`r`/`b`
  resolve, then the modifier, then the error hook, then counters and
  the action. TypeScript's hook now runs in `process()` after `h`, and
  the modifier sees resolved routing in every runtime. Pinned by the
  same grammar in `ts/test/cover-engine.test.js`
  ('fnref-strings-for-h-e-p-r-b'), `go/alt_order_test.go` and
  `rs/tests/callback_test.rs`.

  TypeScript also computed `consumed` (matched tokens minus `alt.b`)
  twice, straddling the action, from two reads of a field the action is
  handed; an action writing `alt.b` made the two disagree and left a
  token in the lookahead that had already moved to the history. Go
  computed it once, before the action, and that is the contract now
  (#122): `consumed` is engine state fixed before the action, and
  `alt.b` is an input to the match, not a channel. Pinned by
  `ts/test/alt-consumed.test.js`.

- **`rewind.history` at its edges.** Three spellings of the retained
  rewind window meant different things in different ports, and none of
  it was recorded: an explicit `null` resolved to `Infinity` in
  TypeScript and to unbounded in Rust, against a documented default of
  64 (#144); a cap of `0` retained nothing in TypeScript and everything
  in Go and Rust, which inverted an operator's hardening intent on
  exactly the bound `AGENTS.md` names against hostile input (#142).
  Both sides moved, per defect: TypeScript reads `null` as 64, Go and
  Rust read `0` as retain-nothing. The contract is now one table for
  every runtime: absent or `null` is 64; `n >= 0` caps at `n`; a
  negative cap is `0`; `false` is unbounded, the one spelling every
  runtime can read (`Infinity` still works in TypeScript, and a
  negative `History` in Go's typed struct, as each port's own way of
  writing it). Pinned by `ts/test/rewind.test.js` ('a null history is
  the documented default'), `go/rewind_test.go`
  (`TestRewindZeroHistoryRetainsNothing`) and
  `rs/tests/rewind_test.rs` (`serialized_rewind_history_spellings`).

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
`go/rule_budget_test.go` `TestMaxMulSurvivesTheOptionsMap`. The other
numeric options that path once dropped (`rewind.history`, the
`parse.recover` caps, `parse.budget.checkEveryN`) are carried now, and
a reflection gate keeps every data leaf on the surface; see "The
serialized options surface" in
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
`RegExp` in TypeScript, RE2 in Go, and `regex` in Rust — and the dialects disagree on two
constructs. **It diverges in both directions.**

| pattern | input | TypeScript | Go | Rust |
|---|---|---|---|---|
| `@/^\s+/` | U+00A0 NBSP | accepted | **rejected** | accepted |
| `@/^\s+/` | U+2028, U+2000, U+3000 | accepted | **rejected** | accepted |
| `@/^\s+/` | U+FEFF | accepted | **rejected** | **rejected** |
| `@/^\s+/` | U+0020, U+0009 | accepted | accepted | accepted |
| `@/^k/i` | `k`, `K` | accepted | accepted | accepted |
| `@/^k/i` | U+212A KELVIN SIGN | **rejected** | accepted | accepted |

JS `\s` uses its own Unicode-derived set; RE2's is the Perl class
`[\t\n\f\r ]`; Rust uses Unicode `White_Space`, which excludes U+FEFF. JS
`/i` without `u` does not fold U+212A to `k`; RE2 and Rust case-fold by
Unicode rules and do.

**And two constructs that do not diverge in the result but in whether the
grammar loads at all**, which for a grammar author is worse:

| pattern | TypeScript | Go | Rust |
|---|---|---|---|
| `@/^(?=x)x/` (lookahead) | installs, matches `x` | **install error** | **install error** |
| `@/^(a)\1/` (backreference) | installs, matches `aa` | **install error** | **install error** |

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

Pinned by `go/divergence_test.go` `TestDivergenceRegexDialect`, the
matching case in `ts/test/divergence.test.js`, and the Rust divergence
register runner, which assert the recorded
results on purpose. Both drive a real `GrammarSpec` through
`grammar()` / `Grammar()` and parse: the gap was previously known only at
the regex-engine layer, so "a shared grammar that depends on either will
differ" was a prediction until this was wired.

### `@push$` with `chain: false` and a grammar that breaks its promise

`@push$` takes an opt-in `chain: false` (schema v5 config, alongside
`src`). It tells the engine that the grammar never reads a rule that has
been replaced, so the grown list need not be re-published back along the
replacement chain. TypeScript and Rust hand out one array object, so
every holder sees every element and the key is a no-op in both. A Go
slice is a value, so in that port the key is the difference between
O(elements) and O(elements^2) on a grammar whose element rule replaces
itself per separator, which is the shape `@tabnas/json` uses.

| grammar | input | TypeScript | Go | Rust |
|---|---|---|---|---|
| `chain: false`, parent reads the replaced rule | `1,2` | `["1","2"]` | **`["1"]`** | `["1","2"]` |
| same grammar, key absent | `1,2` | `["1","2"]` | `["1","2"]` | `["1","2"]` |

The divergent row is a grammar declaring the key and then doing the one
thing the key promises not to do: a rule lifts the list with `@bubble$`
and replaces itself, and the parent reads the rule it replaced. **The
loader cannot check that promise** -- it is a claim about which paths the
grammar will resolve, not about the spec's shape -- so the different
result is reachable for the same serialized grammar and input, and is
registered rather than argued away.

Repairing it means one of two things, and both are worse than recording
it. Removing the opt-out puts `@push$` back to quadratic on the shape the
shipped JSON grammar uses: 15,127,750 walk steps for 5,500 elements,
11.9 seconds on `numeric-edge-1mb` against 230 ms with the key. Making
the walk unconditional but cheap is the open problem
`go/doc/differences.md` records three blocked attempts at.

Registered as `push-chain-off`, with `push-chain-on` as its control.

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

### Forward traversal of a replacement chain in Rust

**Deferred, not deliberate** — a Rust defect with a designed repair
already scheduled, recorded here so nobody is told the rule graph is at
parity when a grammar can still see the difference.

`rule.child` names the rule its parent PUSHED in all three ports, and a
rule that replaces itself leaves the parent on the first link of the
chain. That is what `@fold$` exists to work around, and all three ports
now agree on it, on the link's node, and on its immediate `next` — the
replacement that took its place.

They part company on the SECOND hop. TypeScript and Go hold a replaced
rule as a live object, so its `next` goes on being written long after the
parent stopped looking, and `child.next.next` reaches the chain's third
link. Rust cannot: it holds a frozen record, taken when each link stopped
being the current rule, and that record's `next_rule` is its successor as
the successor stood at its own creation — before it had a successor of
its own.

| grammar | path read on the parent's close | TypeScript | Go | Rust |
|---|---|---|---|---|
| `child` replaced by `child2`, `child2` by `child3` | `child.next.name` | `child2` | `child2` | `child2` |
| same | `child.next.next.name` | `child3` | `child3` | **nothing** |
| same | `child.next.next.next.name` | `top` | `top` | **nothing** |

Repair direction: **Rust changes.** TypeScript defines the language and
Go already agrees with it.

Not repaired here because the forward pointer cannot be patched in place
once it is stored: by the time the grandchild exists, the successor's
record is shared through an `Rc` and the link that would have to be
rewritten sits in the middle of the chain. The chain IS still reachable,
backwards, through `prev_rule` — every link holds the one it replaced —
and resolving a successor from that walk instead of from a stored
forward pointer is item **R7** of
[`doc/rust-callback-contract-spec.md`](doc/rust-callback-contract-spec.md),
announced by its `[V4]` release note. Writing a second mechanism here
would collide with that one and add a cost to every pop, so the split is
registered until R7 lands.

Registered as `chain-next-two-hops` and `chain-next-three-hops`, with
`chain-next-one-hop` as their control. **The control row is the load-bearing
one:** it is what tells a later reader that the child link itself is at
parity and only the walk past it is not.

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
