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
file is prose and this one is backed by tests in every port. When adding
to either, the test is not "is this about the Go port?" but "can the
engines produce a different result for the same input?" If yes, it
belongs here, whatever else is true about it.

**This file is prose, and prose rots.** Every entry below that carries
its own `###` heading is therefore also REGISTERED, per ADR-14, in
[`test/spec/divergent.tsv`](test/spec/divergent.tsv): a row per case, a
column per runtime, asserted by every runtime suite. A divergence that gets
repaired fails that register as loudly as one that regresses, so the row
— and the entry here — must then be deleted: the row moves to
[`test/spec/repaired.tsv`](test/spec/repaired.tsv), its three runtime
columns collapsed into the one answer every runtime now gives, where the
same runners keep asserting the repair, and the entry moves under
"Repaired, and what replaced them" below. Where an entry cannot be
registered yet it is declared, with a reason, in the `notRegistered` map
in `go/divergent_test.go`, which names each such entry, the reason, and
where it is pinned instead. A gate in the same file fails if an entry
here gains no row and no exemption, or if an exemption outlives the
entry it exempts.

When this file and the register disagree, **the register is what runs**.
Fix this file to match it, never the other way round.

## Why this matters more here than elsewhere

This engine is the root of a dependency graph. A divergence here reaches
every downstream grammar, in every runtime, and downstream cannot fix it —
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

- **A bad token a custom matcher returns, and the error a recovering
  parse gives up on, in Rust.** Never recorded here, and found by
  `tabnas/css`, whose unclosed comment is a bad token its own matcher
  returns. TypeScript's fetch in `parse_alts` raises every `#BD` token
  it is handed at once, with the token's own code at the token's own
  position; under recovery it records it, coalesces a run into one
  error and steps the lexer past it; under relexing it leaves it for an
  alternate to re-cut. Go's `Lex.Next` does the same, fail-fast and
  under recovery. Rust did it only for its lexer's own faults: a token
  from `Lexer::bad` or `Lexer::bad_span` was buffered like a good one,
  failed every alternate, and the error named the first token of the
  lookahead; under recovery nothing stepped past it, so the skip met it
  again until `maxSkip` gave up. Separately, a recovering Rust parse
  that gave up listed its error twice, once more without recovery
  metadata, where TypeScript lists what it recorded (or dropped as a
  cascade) once. With the matcher and grammars of
  `test/spec/bad-token.tsv`, which cuts `?` as a bad token:

  | grammar | input | options | TypeScript and Go | Rust before |
  |---|---|---|---|---|
  | `json` | `{"a"?:1}` | | `custom_bad` at 1:5 | `unexpected` at 1:2 |
  | `json` | `[1,?,2]` | recover | `[1,2]`, `custom_bad` 1:4, a run of 1 | `[1]`, `custom_bad` 1:4, unrecovered |
  | `nest` | `<` | recover | one `unexpected` at 1:2 | the same error twice |

  Rust now handles a fetched bad token exactly as it handles its lexer's
  own faults, and both as TypeScript does: raised, or recorded and
  coalesced, and stepped past by TypeScript's `advanceLexPast` rule,
  which moves past the token's span and, for a fault inside a string,
  past the next row character after it; the check for trailing content
  raises one without recording it first; a lexer fault reaches the lex
  subscribers in every mode. A give-up lists nothing again, the recovery
  cap is read after the error is recorded, and a cascade is dropped
  before it, all in TypeScript's order. Over 4,000 generated inputs in
  four modes (fail-fast, recovery, relexing, both), TypeScript and Rust
  now report the same errors, positions and recovery metadata on all
  16,000 runs, where 3,013 differed before. Pinned in all three runtimes
  by `test/spec/bad-token.tsv`, and in Rust by
  `rs/tests/bad_token_fetch_test.rs`.

- **Three answers Go gave differently under recovery and from
  `continuations()`.** Found while the fixture above was written, and
  kept out of it until Go followed TypeScript. With relexing and recovery
  together, Go listed a bad token no alternate re-cut twice, once
  unrecovered from the relexing pass and once from the recovery, where
  TypeScript leaves the token to its recovery and lists it once with the
  tokens skipped (#266). Go read `maxRecoveries` before recording an
  error, so a cascade at the cap ended its parse (`[1,,,2]` under a cap
  of one gave `[1]`), where TypeScript records, drops the cascade and
  reads the cap after, and goes on to `[1,2]`; and when its fetch-time
  absorber gave up at a cap, Go's recovery recorded the same token a
  second time. From `continuations()`, Go answered a bad token after a
  complete document with the last rule's closers (`<1>?` in the
  fixture's `nest` grammar gave `#RB`) and a bad token a rule fetched
  with what alternates still waiting on a later position wanted (`[?`
  gave `#CS` among its answer), where TypeScript throws at the fetch and
  computes from the buffer as it stood, for the fetching rule, and
  answers a failure of the trailing-content check, which has no rule,
  with the start rule's openers (`#LB`, and `#NR,#ST,#VL,#OB,#OS`). Go
  now does the same on every count, and the rows are in
  `test/spec/bad-token.tsv` under "Relexing with recovery", the cap rows
  and the continuations rows; ts/test/bad-token.test.js and
  rs/tests/bad_token_fetch_test.rs pin the same answers a second time.

- **A block comment with no end marker.** A comment definition with
  `line: false` and no `end`, or `end: ''`, had three answers (#218). Go
  closed the comment right after its start marker, since
  `strings.HasPrefix` with an empty end is true at once. TypeScript ran
  it to the end of the source by accident, `startsWith(undefined)`
  searching for the word "undefined", and threw a TypeError when the
  body held that word, which `guardedMatcher` turned into `unexpected`;
  with `end: ''` written out it gave Go's answer. Rust closed a block
  comment only on a non-empty end, so it never closed and reported
  `unterminated_comment`. The ruling (#275) is that a block comment that
  cannot close is a configuration error: every runtime now refuses the
  definition when the options are built, with
  `options.comment.def.<name>.end: block comments require a non-empty
  end marker` (`validateCommentDefinitions` in `ts/src/utility.ts`,
  `checkCommentDefinitions` in `go/plugin.go`,
  `Options::validate_comment_definitions` in `rs/src/options.rs`), so
  the input never reaches a lexer and the three answers, the `undefined`
  body included, cannot be produced. Pinned per runtime by
  `ts/test/options-validate.test.js`, `go/options_validate_test.go` and
  `rs/tests/options_overlay_test.rs`.

- **A row counted at a raw U+2028 or U+2029 inside a string in Rust.**
  Found by the differential run behind tabnas/json5#82 and reported as
  #263. A string body is not multi-line unless its quote is in
  `string.multiChars`, and in TypeScript and Go a line character inside
  such a body is ordinary content: `buildStringBodySpec` in
  `ts/src/lexer.ts` classifies a line character as body unless the
  string is multi-line, and Go's `BuildStringBodySpec` is its port. Rust
  counted rows in one place, `advance` in `rs/src/lexer.rs`, for every
  character in `line.rowChars` its cursor stepped over, string body or
  not, and refused any character in `line.chars` inside a single-line
  string as `unprintable`. Under json5's row characters, LF, U+2028 and
  U+2029, which all three json5 ports set:

  | input, position of `y` | `line.chars` | TypeScript | Go | Rust before |
  |---|---|---|---|---|
  | `"ab" y` | default | 1:6 | 1:6 | 1:6 |
  | `"a<U+2028>b" y` | default | 1:7 | 1:7 | 2:4 |
  | `"a<U+2028>b" y` | LF, U+2028, U+2029 | 1:7 | 1:7 | `unprintable` at column 3 |

  The Rust string matcher now follows the body classes of
  `buildStringBodySpec`: inside a multi-line string a line character
  resets the column and counts a row when `line.rowChars` holds it, and
  everywhere else in a string body a character counts a column, with
  one exception, a control character below 32, which stops the body as
  `unprintable` whether or not it is a line character. The same rule
  settles two neighbours no row had registered. A row character outside
  `line.chars` is body inside a multi-line string too, so `y` in
  `"a<U+2028>b" y` under `line.rowChars` of LF and U+2028 alone, with
  the double quote multi-line, is at 1:7 in every runtime where Rust had
  2:4. A CR inside a multi-line string resets the column without
  counting a row, as TypeScript's class LINE does and Go already did, so
  `y` in `` `a<CR>b` y `` is at 1:4 where Rust had 1:7. Two paths those
  classes do not decide still count a row in Rust, a row character an
  escape consumes and one a `string.replace` key replaces: the live
  entries "An escaped row character inside a string counts a row in
  Rust" and "A replaced row character inside a string counts a row in
  Rust". Pinned in all three runtimes by the first group of
  [`test/spec/repaired.tsv`](test/spec/repaired.tsv), `string-raw-ls`
  to `string-multi-crlf-control`, with `string-row-char-control`.

- **No row counted at a raw U+2028 or U+2029 inside a multi-line string in Go.**
  The Go half of the same split, measured while #263 was registered.
  Go's `BuildStringBodySpec` in `go/scan.go` tested
  `LineChars` and `RowChars` only for a character below code point 32,
  so under json5's line configuration, with the double quote in
  `string.multiChars`, a raw U+2028 or U+2029 inside a multi-line string
  was plain body, and the token after the string sat on row 1 where
  TypeScript and Rust put it on row 2:

  | input, position of `y` | TypeScript | Go before | Rust |
  |---|---|---|---|
  | `"a<LF>b" y` | 2:4 | 2:4 | 2:4 |
  | `"a<U+2028>b" y` | 2:4 | 1:7 | 2:4 |
  | `"a<U+2028>b<U+2029>c" y` | 3:4 | 1:9 | 3:4 |

  The test now applies to every character in `LineChars`.

  **TypeScript moves here as well, and this is the one place the repair
  moves it.** Its 256-entry table disagreed with its own fallback:
  `buildStringBodySpec` classified a line character inside a multi-line
  string at any code point past the table, and inside the table only
  below 32, so a line character from U+0020 to U+00FF was body there
  where any other line character reset the column. Measured before the
  change, with the double quote multi-line:

  | input, position of `y` | `line.chars` | `line.rowChars` | TypeScript before | Go before | Rust before |
  |---|---|---|---|---|---|
  | `"a<U+2028>b" y` | LF, U+2028 | LF | 1:4 | 1:7 | 1:7 |
  | `"a<U+0085>b" y` | LF, U+0085 | LF | 1:7 | 1:7 | 1:7 |
  | `"a<U+0085>b" y` | LF, U+0085 | LF, U+0085 | 1:7 | 1:7 | 2:4 |

  The first row is the fallback's answer and the other two the table's.
  The table now classifies a line character at any code point, as the
  fallback did, and all three runtimes answer 1:4, 1:4 and 2:4. So
  TypeScript's answer moves on the second and third rows, Go's on all
  three and Rust's on the first two. No grammar in the fleet configures
  a line character from U+0020 to U+00FF. To keep the table as it was,
  strike the hunk in `ts/src/lexer.ts` and the two rows that pin it,
  `string-multi-raw-nel` and `string-multi-line-char-nel`; Go and Rust
  would then need the same exception below U+0100, or the split it
  leaves would need registering. Pinned by the second group of
  [`test/spec/repaired.tsv`](test/spec/repaired.tsv),
  `string-multi-raw-ls` to `string-multi-line-char-ls`, with
  `string-multi-row-char-control`.

- **An escaped non-ASCII character read as one byte in Go.** The second
  half of #263. The Go string matcher read the character after the
  escape character as one byte, `esc := src[sI]` in `go/lexer.go`, and
  stepped one byte past it, so the remaining bytes of a multi-byte
  character fell to the body scan and were counted as columns of their
  own, and a `string.escape` mapping of a non-ASCII character was looked
  up by its first byte and never found. The value of an unmapped escape
  was right, since the bytes were appended in order:

  | `string.escape` | input | TypeScript | Go before | Rust |
  |---|---|---|---|---|
  | default | `"\a" y`, column of `y` | 6 | 6 | 6 |
  | default | `"\é" y`, column of `y` | 6 | 7 | 6 |
  | default | `"\€" y`, column of `y` | 6 | 8 | 6 |
  | `{a: 'X'}` | `"\a"`, value | `X` | `X` | `X` |
  | `{é: 'E'}` | `"\é"`, value | `E` | `é` | `E` |

  The matcher now decodes the rune after the escape character, copies
  its bytes whole and steps past its full width, counting one column, as
  every other branch of that matcher already did for a multi-byte
  character, and looks the mapping up by the whole character when that
  character is one UTF-16 unit, as TypeScript reads the map. A character
  above U+FFFF is not looked up. TypeScript compares a key with one code
  unit and so never matches a key of two, and looking an astral
  character up whole, as this repair first did, gave Rust's answer
  instead: a split older than #263, the live entry "A `string.escape`
  key longer than one UTF-16 unit". The column after an escaped astral
  character stays apart for its own reason: `"\😀" y` puts `y` at
  column 6 in Go, where it was 9, and in Rust, and at 7 in TypeScript,
  which counts the character's two UTF-16 units, the split recorded
  under "Column positions for astral characters". Pinned by the third
  group of [`test/spec/repaired.tsv`](test/spec/repaired.tsv),
  `escape-two-byte` to `escape-map-three-byte`, with
  `escape-ascii-control` and `escape-map-ascii-control`.

- **A non-ASCII `string.replace` key read by its first byte in Go.**
  Found while #263 was in review, and never registered. Go's serialized
  options, `OptionsFromMap` in `go/utility.go`, keyed the replace map by
  a key's first byte, `rune(k[0])`, so a non-ASCII key never applied,
  and its lead byte, read as a Latin-1 character, applied in its place.
  TypeScript keys the map by a key's first code unit (`c.charCodeAt(0)`
  in `makeStringMatcher`) and Rust by its character:

  | `string.replace` | input, value | TypeScript | Go before | Rust |
  |---|---|---|---|---|
  | `{b: 'B'}` | `"abc"` | `aBc` | `aBc` | `aBc` |
  | `{é: 'E'}` | `"aéb"` | `aEb` | `aéb` | `aEb` |
  | `{é: 'E'}` | `"aÃb"` | `aÃb` | `aEb` | `aÃb` |
  | `{€: 'EUR'}` | `"a€b"` | `aEURb` | `a€b` | `aEURb` |

  A key is now decoded to its first character, which is TypeScript's key
  for every character in the BMP. The same read had a position
  consequence once #263 widened Go's multi-line test. Under json5's line
  configuration, with the double quote multi-line and `{U+2028: 'L'}`,
  the unreplaced U+2028 became a row, and `y` in `"a<U+2028>b" y` moved
  from 1:7 to 2:4 while the PR was in review. Go now replaces it and
  answers 1:7 with TypeScript. Rust answers 2:4 there, counting a row for
  the replaced character, which is the live entry "A replaced row
  character inside a string counts a row in Rust"; a key above U+FFFF is
  the live entry "A `string.replace` key longer than one UTF-16 unit".
  Pinned by the fourth group of
  [`test/spec/repaired.tsv`](test/spec/repaired.tsv), `replace-two-byte`
  to `replace-ls-json5-value`, with `replace-ascii-control`.

- **A pusher read back through its child's snapshot in Rust.** A push
  links the child to its pusher both ways. TypeScript and Go link the
  live rules, so from the child `parent.child.parent.child` is the
  child itself. Rust links snapshots, and the one the pusher kept as
  `child` was taken before the pusher had linked it, so its `parent`
  was the pusher as it stood before the push, whose `child` was the
  child pushed before: with `list` pushing `first` from its open phase
  and `second` from its close phase, `parent.child.parent.child.name`
  read on `second` gave `second` in TypeScript and Go and `first` in
  Rust, or nothing on a pusher's first push (#259). Repaired as the
  entry said: with no history bound, the push arm links the child
  again once the pusher has linked it, from a snapshot that carries the
  pusher with the child linked (`Rule::relink_child` in
  `rs/src/rule.rs`), and the child keeps the pusher as it then stands.
  The cost is two more records kept per push without a bound, 8 for 6
  per element of a flat array, 654 MB for 554 MB at its peak over
  300,000 numbers. The pusher as it stood before the push is still
  where the snapshots end, two hops further down, past every path a
  fleet grammar reads: the survey in
  [`doc/rule-history-bound.md`](doc/rule-history-bound.md) found none
  past three hops. Under a bound nothing moves: TypeScript and Go copy
  the same two records there, so the path reads nothing in all three
  runtimes, which `rule-history-bounded-pusher` pins. Pinned in all
  three by `rule-history-pusher-control` in
  `test/spec/rule-history.tsv`, the same grammar with the option unset,
  with `rule-history-pusher-one-hop` (`parent.child.name`, the row the
  register kept as its control) beside it, and in Rust by
  `a_pusher_read_back_through_its_child_has_linked_it` in
  `rs/tests/rule_history_test.rs`. The relink runs only when the
  pushing alternate's after actions left the pusher's `child` and `next`
  as the push made them, so an action that clears or repoints them
  keeps its edit, as in TypeScript and Go
  (`an_after_action_that_clears_the_pushers_links_keeps_its_edit`).
  What the relink does not reach, a read six hops from the child and a
  read from the pusher during those after actions, is the live entry "A
  pusher read back past four hops through its child's snapshots in
  Rust".

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

### A multi-character block-comment end is cut short in Rust

**Deferred, not deliberate** — a Rust defect measured while the block
comment with no end marker was being registered (now under "Repaired").

A block comment whose end marker is longer than one character closes
one character early in Rust. Under `comment.def.hash: {line: false,
end: '@@'}`, `a # x @@ b` lexes to a `#CM` token `# x @` where
TypeScript and Go give `# x @@`, and the marker's last `@` is lexed
again, as text at column 8, before `b`; under `end: '@@@'` the token is
`# x @@`. The span is the marker less its last character, not a fixed
length, and what follows the comment differs with it.

| `comment.def.hash` | input | TypeScript | Go | Rust |
|---|---|---|---|---|
| `{line: false, end: '@'}` | `a # x @ b` | `#CM` `# x @` | `#CM` `# x @` | `#CM` `# x @` |
| `{line: false, end: '@@'}` | `a # x @@ b` | `#CM` `# x @@` | `#CM` `# x @@` | **`#CM` `# x @`**, then `@` |
| `{line: false, end: '@@@'}` | `a # x @@@ b` | `#CM` `# x @@@` | `#CM` `# x @@@` | **`#CM` `# x @@`**, then `@` |

Repair direction: **Rust changes.** TypeScript defines the language and
Go agrees with it: the token spans the whole end marker and the cursor
moves past it.

Registered as `block-comment-two-char-end` and
`block-comment-three-char-end`, with `block-comment-end-control` as
their control: a one-character end, where the three ports agree.

### A `string.escape` key longer than one UTF-16 unit

**Deferred, not deliberate**, for a ruling: measured while #263 was
repaired. Rust's split is older than that PR, which briefly moved Go to
Rust's side of it.

TypeScript looks the escape map up by `src[sI]`, the one code unit after
the escape character (`makeStringMatcher` in `ts/src/lexer.ts`), so a key
of two units or more never matches. An escaped character above U+FFFF,
two units, is then copied as an unknown escape, and a key of two
characters is never consulted at all. Go looks the map up only for an
escaped character of one UTF-16 unit, which gives TypeScript's answer by
another route, and its map, keyed by string, never matches a longer key
either. Rust keys the map by character (`HashMap<char, String>` in
`rs/src/options.rs`), so it applies a key above U+FFFF, and it refuses a
key of more than one character when the options load.

| `string.escape` | input, value | TypeScript | Go | Rust |
|---|---|---|---|---|
| `{é: 'E', 😀: 'X'}` | `"\é"` | `E` | `E` | `E` |
| same | `"\😀"` | `😀` | `😀` | **`X`** |
| `{ab: 'X'}` | `"\abc"` | `abc` | `abc` | **load fault** |

Repair direction: **the maintainer's ruling.** TypeScript's answers
follow from its UTF-16 index, not from a decision anyone made about keys.
If a key is a character, TypeScript moves, looking the map up by code
point, and Go goes with it, back to the whole-character lookup #263 first
wrote. If a key is one code unit, Rust moves and matches no key above
U+FFFF. Either ruling also settles a key of two characters: refused when
the options load, as in Rust, or accepted and never matched, as in
TypeScript and Go.

Registered as `escape-key-astral` and `escape-key-two-chars`, with
`escape-key-bmp-control` as their control: a key of one unit under the
same options, applied in every port.

### A `string.replace` key longer than one UTF-16 unit

**Deferred, not deliberate**, for a ruling: the replace-map half of the
entry above, measured with it.

TypeScript keys the replace map by the first code unit of each key
(`c.charCodeAt(0)` in `makeStringMatcher`) and reads the source one code
unit at a time. A key above U+FFFF therefore replaces its high surrogate
alone and leaves the low surrogate in the value, and a key of two
characters replaces its first character wherever that appears. Go keys
the map by a key's first character, which is TypeScript's key for every
character in the BMP. For one above U+FFFF it keys the whole character,
as its native `map[rune]string` option always has, because TypeScript's
answer there is a lone surrogate, which a Go string cannot hold. Before
#263 Go keyed by a key's first byte, so this key never applied and the
split had three answers. Rust keys by character, replaces the whole of an
astral one, and refuses a longer key when the options load.

| `string.replace` | input, value | TypeScript | Go | Rust |
|---|---|---|---|---|
| `{é: 'E', 😀: 'X'}` | `"aéb"` | `aEb` | `aEb` | `aEb` |
| same | `"a😀b"` | **`aX`, U+DE00, `b`** | `aXb` | `aXb` |
| `{ab: 'X'}` | `"abc"` | `Xbc` | `Xbc` | **load fault** |

Repair direction: **the maintainer's ruling.** TypeScript's answer
follows from its UTF-16 index, and neither port can give it, since a lone
surrogate folds to U+FFFD in Go and Rust (see "Lone surrogates in quoted
strings"). So the astral split closes only if TypeScript moves, keying
and reading the map by code point as Go and Rust do; the alternative is
to accept it as a consequence of the string models. A key of two
characters is the question the entry above asks.

Registered as `replace-key-astral` and `replace-key-two-chars`, with
`replace-key-bmp-control` as their control.

### An escaped row character inside a string counts a row in Rust

**Deferred, not deliberate**, for a ruling: measured while #263 was
repaired.

An escape whose character is in `line.rowChars`, a line continuation,
puts that character in the value in every port. TypeScript copies it as
an unknown escape and counts one column (`buf.push(ec); sI++; cI++` in
`ts/src/lexer.ts`), and Go does the same. Rust steps over it with
`advance` in `rs/src/lexer.rs`, which counts a row for every character
in `line.rowChars`, so every position after the string is a row further
on:

| input, position of `y` | options | TypeScript | Go | Rust |
|---|---|---|---|---|
| `"a\qb" y` | default | 1:8 | 1:8 | 1:8 |
| `"a\<LF>b" y` | default | 1:8 | 1:8 | **2:4** |
| `` `a\<LF>b` y `` | default | 1:8 | 1:8 | **2:4** |
| `"a\<U+2028>b" y` | json5's line characters | 1:8 | 1:8 | **2:4** |

The value is the same in every port. The last row was 1:10 in Go before
#263, which read the escaped character one byte at a time.

Repair direction: **Rust changes, by ADR-13's default.** TypeScript
defines the language, Go agrees with it, and the fix is to step over an
escaped character as one column. It is recorded rather than made because
the default may be the wrong way round: Rust's row is where `y` stands in
the source, and TypeScript's count leaves every later position a row
behind it, which is a defect of its own if a position names a source
line. If the maintainer rules so, TypeScript and Go move instead and
count a row for an escaped row character. The rows hold the split until
then.

Registered as `escape-lf`, `escape-lf-multi` and `escape-ls-json5`, with
`escape-row-char-control` as their control: an escaped ordinary
character, one column in every port.

### A replaced row character inside a string counts a row in Rust

**Deferred, not deliberate**, for the ruling the entry above asks for,
and measured with it.

TypeScript consults the replace map first after the closing quote, before
a line character can count a row, so a replaced row character becomes its
replacement and counts one column, in a single-line string and a
multi-line one alike. Rust replaces the character too, but steps over it
with `advance`, which counts a row. Go agrees with TypeScript for a row
character outside the control range, U+2028 under json5's line
characters, since #263 made it read a replace key by character; a
control one, LF, it refuses as `unprintable` before it reads the map,
which is the entry "The `string.replace` map consulted after the escape
and control checks in Go" below:

| `string.replace` | input, position of `y` | TypeScript | Go | Rust |
|---|---|---|---|---|
| `{x: 'X'}` | `"axb" y` | 1:7 | 1:7 | 1:7 |
| `{U+2028: 'L'}`, json5's line characters | `"a<U+2028>b" y` | 1:7 | 1:7 | **2:4** |
| `{U+2028: 'L'}`, json5's line characters, `"` multi-line | `"a<U+2028>b" y` | 1:7 | 1:7 | **2:4** |
| `{LF: 'N'}` | `"a<LF>b" y` | 1:7 | `unprintable` | **2:4** |
| `{LF: 'N'}` | `` `a<LF>b` y `` | 1:7 | `unprintable` | **2:4** |

Every value agrees where Go answers. Before #263 Go answered 1:7 on the
multi-line U+2028 row only because it read the key by its first byte and
never replaced the character; while #263 was in review, its wider
multi-line test made that unreplaced character a row, 2:4.

`tabnas/jsonic`'s ledger carries the same split through its grammar as
`string-replace-control-row`, where a raw CR after a replaced LF is
`unprintable` at 2:6 in TypeScript and Go and at 3:1 in Rust.

Repair direction: **Rust changes, by ADR-13's default**, stepping over a
replaced character as one column. If the maintainer rules that a row
character counts a row wherever an escape or a replacement consumes it,
TypeScript and Go move instead. One ruling settles this entry and the one
above.

Registered as `replace-ls-single` and `replace-ls-json5`, with
`replace-row-char-control` as their control: a replaced ordinary
character, one column in every port. The LF rows are not registered
here. Go's answer there is a lex error, and the `lex` probe renders the
source of a lex error, which would be the LF itself, a character no
register cell may hold; Go's refusal is pinned instead, through the
`spec` probe, under the entry below.

### The `string.replace` map consulted after the escape and control checks in Go

**Deferred, not deliberate**, and held on purpose: measured while #263 was
repaired, with the repair written, measured and taken out again. Tracked
as [#287](https://github.com/tabnas/parser/issues/287).

Inside a string body TypeScript tests the closing quote, then the replace
map, then the escape character, then the control range
(`makeStringMatcher` in `ts/src/lexer.ts`), and Rust consults the replace
map in the same place. Go's `matchString` in `go/lexer.go` tests the
escape character, then the control range, and reads the replace map
last. So a `string.replace` key for a control character is refused as
`unprintable` before the map is read, and a key for the escape character
begins an escape instead of being replaced:

| `string.replace` | input, value | TypeScript | Go | Rust |
|---|---|---|---|---|
| `{x: 'X'}` | `"axb"` | `aXb` | `aXb` | `aXb` |
| `{TAB: 'T'}` | `"a<TAB>b"` | `aTb` | **`unprintable`** | `aTb` |
| `{LF: 'N'}` | `"a<LF>b"` | `aNb` | **`unprintable`** | `aNb` |
| `{'\\': '/'}` | `"a\bc"` | `a/bc` | **`a`, U+0008, `c`** | `a/bc` |

Repair direction: **Go changes**, to TypeScript's order, reading the
replace map right after the closing quote. TypeScript defines the
language and Rust agrees with it. The repair moves one block in
`matchString`, and #287 carries it.

It is held because a downstream ledger pins Go's present answer.
`tabnas/jsonic`'s divergence ledger carries the replaced-LF case, through
its grammar, as `string-replace-control`, with Go at `unprintable`, and
that ledger fails a row whose divergence has been repaired until the row
is deleted. This repository's `gate` job runs jsonic from its `main`, and
the fleet gate runs jsonic at its latest release, so the repair lands
once the row is gone from both: delete the row in tabnas/jsonic, release
jsonic, then land the reorder and close this group in the same change.

Registered as `replace-tab`, `replace-lf-value` and `replace-escape-char`,
with `replace-order-control` as their control: a replaced ordinary
character, parsed alike in every port. The control, the TAB and the LF go
through the `spec` probe, a grammar that takes one string token, because
the `lex` probe renders the source of a lex error and Go's would be the
TAB or the LF itself; the escape character goes through the `lex` probe,
whose value Go decodes rather than refuses.

### A pusher read back past four hops through its child's snapshots in Rust

**Deferred, not deliberate**: what the repair of "A pusher read back
through its child's snapshot in Rust" (under "Repaired") does not reach.

TypeScript and Go link a pushed child to its pusher as live rules, so
from the child every `parent.child` is the child itself, however often
the path repeats it. Rust links snapshots. With no history bound the
push arm links the child a second time, from a snapshot that carries the
pusher with the child linked, and that carries the read through four
hops. Two hops further the snapshots end at the pusher as it stood
before the push, whose `child` is the child pushed before. With `list`
pushing `first` from its open phase and `second` from its close phase:

| path read on `second` | TypeScript | Go | Rust |
| --- | --- | --- | --- |
| `parent.child.parent.child.name` | `second` | `second` | `second` |
| `parent.child.parent.child.parent.child.name` | `second` | `second` | `first` |

The same model shows from the pusher's side, during the pushing
alternate's after actions. TypeScript and Go have linked the live rules
before those actions run, so a state action on the pusher's close reads
`child.parent.child` as the child just pushed. Rust relinks after the
actions, so the same action reads the child pushed before, or nothing on
the pusher's first push. No row can carry this face, because a
serialized grammar has no after action that reads a path; per-runtime
tests pin it instead.

No fleet grammar reads either path: the survey in
[`doc/rule-history-bound.md`](doc/rule-history-bound.md) found none
past three hops.

Repair direction: **Rust changes**, by a mechanism the maintainer
chooses. Relinking further costs two more records per push without a
bound for each further pair of hops. Resolving a child's `parent` as the
live rule in the path readers gives TypeScript's answer at every depth
for a condition path at no memory cost, though not to a subscriber
walking the raw links or to an after action. Linking live rules, as the
canonical engine does, is the full repair.

Registered as `pusher-six-hops`, with `pusher-four-hops-control` as its
control: the four-hop read, where the three runtimes agree. The
after-action face is pinned by `ts/test/divergence.test.js` ('a pusher's
after action reads the child just pushed here and in Go, the child
pushed before in Rust'), `go/divergence_test.go`
`TestPusherAfterActionReadsTheChildJustPushed` and
`rs/tests/rule_history_test.rs`
`an_after_action_on_the_pusher_reads_the_child_pushed_before`.

### Decorations reach a derived child after the plugins in Go

A derived child (`make()` in TypeScript, `derive` in Rust, `Derive` in
Go) is built from the parent's options and re-runs the parent's plugins,
so option-conditional grammar is rebuilt against the child's options.
It also inherits the parent's decorations: the own properties a plugin
puts on the instance in TypeScript, the `decorate` entries in Go and
Rust. The two steps run in a different order. TypeScript copies the
parent's properties onto the child before it re-runs the plugins (the
`Object.keys(parent)` loop ahead of `this.use(plugin)` in the `Tabnas`
constructor, `ts/src/tabnas.ts`), and Rust does the same
(`child.decorations = self.decorations.clone()` ahead of the
`use_plugin` loop in `Tabnas::derive`, `rs/src/lib.rs`). Go re-applies
the plugins first and copies the decorations after (`Derive` in
`go/plugin.go`).

What a plugin sees of the instance while it re-runs on the child:

| a plugin that writes a different value on each run | TypeScript | Go | Rust |
| --- | --- | --- | --- |
| during its re-run on the child, its decoration from the parent is already there | yes | no | yes |
| the child's decoration once the derive returns | what the re-run wrote | the parent's, copied over what the re-run wrote | what the re-run wrote |

Found by tabnas/yaml (#244): the Rust plugin guarded its install with a
decoration, so on a child it found its own mark, returned early, and
the child parsed `a: 1` to null with no error, where the same guard in
Go would have installed. A guard belongs on the rules, as jsonic's is
and tabnas-yaml's now is (tabnas/yaml#113). What remains is the
order, with two faces: a plugin that reads a parent decoration during
its re-run, to inherit state, finds it in TypeScript and Rust and not
in Go; and a value the re-run writes, one computed from the child's
options say, is what the child carries in TypeScript and Rust, where
Go's copy afterwards puts the parent's value over it.

Repair direction: **Go changes.** TypeScript defines the order and
Rust follows it: the child carries its parent's decorations before
any plugin runs on it.

Not registered: the observable is what a plugin sees of the instance
during its re-run, which no row-shaped probe drives. Pinned per
runtime by `ts/test/divergence.test.js` ('a derived child carries its
decorations into the plugin re-run here and in Rust, after it in Go'),
`go/divergence_test.go` `TestDerivedChildGetsDecorationsAfterThePlugins`
and `rs/tests/divergent_spec_test.rs`
`derived_child_carries_decorations_into_the_plugin_rerun`.

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
