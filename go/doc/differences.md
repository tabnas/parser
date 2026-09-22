# Differences from TypeScript

> **Looking for parity?** This is a PORTING guide: packaging, API shape,
> Go-specific helpers, plugin surface. Most of it describes how to write
> the same program twice, not places the two engines disagree.
>
> For "will these engines produce the same result for your input?", read
> [`DIVERGENCE.md`](../../DIVERGENCE.md) at the repository root, which is
> the single record of behavioural divergence and is deliberately short.

The TypeScript version is the authoritative implementation. The Go version is
a faithful port of the engine behavior (same packaging (grammar-free
engine), same lexer structure, same error model) with deliberate Go-only
additions for Go client code, listed below.

## Packaging: Aligned (Grammar-Free Engine)

Both runtimes are grammar-free engines that ship no grammar. In each, a
grammar (including strict JSON) arrives via a plugin, and the
strict-JSON grammar lives only as a test fixture: `ts/test/json-plugin.ts`
in TypeScript, `go/jsonplugin_test.go` (`package tabnas`, test-only) in
Go. The Go engine is `github.com/tabnas/parser/go` (package `tabnas`):

| Need | Use |
|---|---|
| Parse anything | `tabnas.Make()` + `j.Use(grammarPlugin)` |
| Bare engine, own grammar | `tabnas.Make()` + `Token`/`Rule`/`Grammar` |
| Grammar as a plugin | `j.Use(myPlugin)` |
| Restrict to a rule group | `Rule: &RuleOptions{Include: "json"}` |

A grammar plugin is a `func(j *Tabnas, opts map[string]any) error`. The
strict-JSON test fixture (`makeJSON` / `jsonPlugin` in
`go/jsonplugin_test.go`) is a worked example, but those helpers are
test-only and not importable by client code.

The engine's text-form convenience APIs (`SetOptionsText`, `GrammarText`)
need a parser for their text argument. The engine registers none; a
grammar package registers one via `tabnas.RegisterTextParser` (in the
manner of database/sql drivers), and until one is registered the
text-form APIs return an error.

Both runtimes run the same shared fixtures under `test/spec/`: the
strict-JSON set (`include-json*.tsv`) and the utility set
(`utility-*.tsv`). TypeScript runs them from `ts/test/json-spec.test.js`
and `ts/test/utility.test.js` (grammar from `ts/test/json-plugin.ts`); Go
runs them from `go/spec_test.go` and `go/utility_spec_test.go` (grammar
from `go/jsonplugin_test.go`).

Fixtures exercising *relaxed* grammar syntax (bare text, unquoted keys,
implicit structure) used to sit in `test/spec/` too, unexecuted, since
the strict-JSON test grammar rejects them by design and this engine ships
no grammar. They now live only in the grammar's own repo
([`tabnas/jsonic`](https://github.com/tabnas/jsonic), `test/spec/`),
where both of its runtimes run them.

## Behavioral Differences

These affect parse output for the same input.

### Negotiated Lexing (`lex.relex`)

Aligned. Both runtimes carry the opt-in lexer option (`lex: { relex: true }`
in TS, `Options.Lex.Relex` in Go) and it is **off by default** in both.

When set, a token-type mismatch in the alternate loop is no longer final:
the engine re-cuts the buffered token's source span constrained to the tins
the alternate itself names (`Lex.relex` / `Lex.Relex`), instead of failing
the alternate outright. Under it a `#BD` token is also a soft failure a
later alternate may renegotiate rather than an immediate throw, and when
no alternate can use it, the SAME diagnostic is raised, at the same token.

The mechanism is the same on both sides: save point + token queue, re-cut
under a wanted-tin filter across the match, fixed and builtin matcher
paths, restore on failure, plus the mismatch hook in the alternate loop.
Per-runtime notes:

- **The want filter.** `match` and `fixed` filter per candidate, so
  longest-match-wins still holds *among wanted candidates*: a shorter
  wanted token can beat a longer unwanted one, which is the point. A
  single-tin builtin (space, line, string, comment, number, text) is
  skipped when its tin is not wanted. Value matchers are skipped outright:
  they produce `#VL` by content, not by the alternate's tin list.
- **Custom matchers are speculated,** not skipped: their token identity is
  opaque, so the only way to learn whether one can serve the request is to
  run it and put the cursor back if what it produced is not wanted.
- **Differs (cosmetic).** Go's fixed-token dispatch table is a
  whole-config summary, so under a want the fixed matcher falls through to
  the candidate list rather than trusting the table's single-byte answer.
  Same result, one array load slower on the renegotiation path only.
- **Differs (cosmetic).** TS preserves a recut token's attached
  `ignored` token; Go's token carries no such field (the parser's fetch
  skips ignored tokens rather than attaching them), so there is
  nothing to preserve.
- **Both runtimes skip rule-position gating under a want,
  deliberately.** Go's match matcher makes a two-pass
  `positionExpected` scan; TS makes the same two passes over its token
  column (`ts/src/lexer.ts` makeMatchMatcher, position-expected
  matchers first, eager-only ones second). Under a want that scan is
  dead in both (the alternate's own tin list is the gate) so neither
  runs it and neither makes the second pass. No behavioural effect, and
  it matters: computing it anyway walked every alternate's slot-0 tins
  for every candidate token, costing 25–98x on scannerless grammars with
  many alternates (the llama.cpp GBNF corpus via `@tabnas/gbnf`:
  `json.gbnf` went 26.9ms → 273µs per parse of an 8-character input). Do
  not "restore parity" by reinstating the scan on the want path: there
  is nothing on either side to be parity with.

  Until the TS lexer gained its second pass, the two orders differed and
  the difference was observable: an eager matcher earlier in tin order
  beat a position-expected one later in TS but not in Go. That entry has
  been retired; `TestEagerPrecedenceMatchesTS` (`go/lexslotgate_test.go`)
  and `match-tokens-expected-at-slot-win-over-earlier-eager`
  (`ts/test/cover-lex.test.js`) now pin the shared behaviour.

  The eager pass also yields to a FIXED literal the slot expects and
  that it cannot out-cut, in all three runtimes. Ties go to the literal;
  an eager matcher that cuts further still wins, so a keyword cannot
  truncate a longer word. Without it a character class containing a
  literal the grammar also uses swallowed it: `num = "0" / posdigit
  *digit` beside `digit = %x30-39` rejected `0.0.0` in Go (whose emitter
  has always marked classes eager) and would have in TS the moment the
  bnf emitter did the same. Pinned by
  `TestExpectedLiteralBeatsAnEagerTieWithoutRelex` and
  `TestExpectedLiteralDoesNotTruncateALongerEagerMatch` (Go),
  `expected-fixed-literal-beats-an-eager-tie` (TS) and
  `expected_literal_beats_an_eager_tie_without_relex` (Rust). It also
  narrows what negotiated lexing is FOR: a tie no longer needs a recut,
  only a contest the class wins on length does.

It cannot widen the accepted language. A recut is returned only when its
tin is in the alternate's OWN list, so every position still requires
exactly what it always required; a wrong re-cut fails the parse rather
than satisfying anything. A `#BD` token never satisfies a position, not
even a wildcard one. Without that, `#AA` would make the deferred throw
into an acceptance.

Practical impact is confined to **scannerless** front-ends. Grammars
written for a tokenising lexer distinguish their terminals lexically and
never contest a character, so the default is identical in both runtimes
for every ABNF and EBNF grammar in the shared fixtures. What needs the
option is the GBNF corpus (`tabnas/gbnf`), where two terminals may claim
the same character: `arithmetic.gbnf`'s `ws ::= [ \t\n]*` against a
literal `"\n"`, `json.gbnf`'s quote inside a string body against the
closing quote, `c.gbnf`'s keywords inside identifier classes.

Pinned by `go/relex_test.go` (the contested shapes, the option probe, the
over-acceptance guards) and `ts/test/lex.test.js`.

### Number + Text Tokenization

Aligned. Both lexers require an ender character after a number, so
`123abc` lexes as a single text token in both (TS via the ender-anchored
number regexp, Go via its not-a-number check). The fixture recording this
behavior needs a relaxed grammar to run, so it lives and is exercised
downstream: `alignment-number-text.tsv` in
[`tabnas/jsonic`](https://github.com/tabnas/jsonic)'s `test/spec/`.

These number forms were previously misaligned and are now fixed in Go:

- **Trailing dot before an exponent** (`2.e3`, `0.e1`, `2.e+3`, `2.e-3`).
  TS's fraction group makes the digit optional, so these are numbers;
  `matchNumber` tested for trailing text before testing for an exponent
  and so abandoned the token. `isExponentStart` (`lexer.go`) carves the
  exponent out of that check. A bare `2.e` stays text in both, since TS's
  exponent group also requires a digit.
- **Base-prefixed integers beyond int64** (`0xFFFFFFFFFFFFFFFF`,
  `0o1000000000000000000000`). JS `Number("0x…")` evaluates the exact
  mathematical value of the digit string and rounds once to nearest
  float64; `strconv.ParseInt` fails with `ErrRange`, which used to drop
  the token to the text matcher, so the run lexed as `#TX` in Go and
  `#NR` in TS. `parseNumericString` now falls back to `exactBaseFloat`
  (big.Int → big.Float) on `ErrRange`. NOTE this is deliberately NOT
  "keep what ParseInt returned": that CLAMPS to MaxInt64, which agrees
  for `0x8000000000000000` only by coincidence and is off by 2x for
  `0xFFFFFFFFFFFFFFFF`. The in-range path stays on ParseInt.
- **Out-of-range exponents** (`1e999` → `Infinity`, `-1e999` →
  `-Infinity`). TS coerces with unary `+`, which saturates;
  `parseNumericString` treated `strconv.ParseFloat`'s `ErrRange` as a hard
  failure. It now keeps the saturated value ParseFloat already returns.
  Other `ParseFloat` errors still yield NaN and drop to the text matcher.
  Go returns no error at all for underflow, so `1e-999` → `0` and
  `-1e-999` → negative zero were already correct.
- **A malformed exponent where an ender starts at the `e`** (`12Ex` under
  `ender: []string{"E"}`, `12END` under `ender: []string{"END"}`). When
  the exponent has no digits, TS drops the whole optional exponent group
  and then requires an ender WHERE THE `e` IS. `matchNumber` asked its
  following-text question one position further on, after the `e`, which
  is a different question: it answered "text follows" for `12Ex` and
  abandoned a token TS reads as `#NR:12`. The backtrack now precedes the
  check, so both ask at the `e`. The single-character case had diverged
  since the port; a multi-character ender makes it ordinary rather than a
  corner, since an entry like `END` has text right after the `E` by
  construction.

These are pinned by `TestMatchNumberExponentTrailingDot` /
`TestMatchNumberExponentRange` (`go/lexer_edge_test.go`) and the matching
`number-exponent-trailing-dot` / `number-exponent-range` cases in
`ts/test/lex.test.js`; the malformed-exponent case is pinned by the
`["END"]` rows of the shared fixture `test/spec/lex-ender-array.tsv`.

### Raw Control Characters in Strings (`string.allowControl`)

Aligned. By default both lexers reject any control character (code point
below `0x20`) inside a string body with `unprintable`. `string.allowControl`
(`Options.String.AllowControl` in Go) relaxes that: control characters are
admitted verbatim as ordinary body text. Line-end characters are deliberately
NOT covered: they stay governed by `multiChars`, so a raw newline inside a
single-line string is still an error with the option set. The option exists
because some grammars' source-character rules admit raw control chars (JSON5's
`JSON5SourceCharacter` permits a literal tab); the default keeps the strict
behavior so no existing grammar changes.

Pinned cross-runtime by the shared fixture `test/spec/lex-string-control.tsv`
(`TestSpecLexStringControl` in `go/lexer_optionplumbing_test.go`,
`string-allow-control-spec` in `ts/test/lex.test.js`).

### Empty / Whitespace Input

Both implementations short-circuit exact empty-string input (`""`) before the
lexer or the rule loop is built, and return `lex.emptyResult` /
`Lex.EmptyResult` (default `undefined`/`nil`) or raise `unexpected` when
`lex.empty` / `Lex.Empty` is false. This is aligned, and it is the *only* path
for `""`: the rule-iteration budget (which is proportional to source length,
and so zero for a zero-length source) is never reached, so a grammar whose
empty value is not `undefined` must declare it via `lex.emptyResult` rather
than expect its start rule to run. Whitespace/comment-only input takes the
normal parse flow in both implementations and resolves by grammar behavior.

### Rule-Iteration Budget: Aligned

The runaway guard is `2 * ruleCount * len(src) * 2 * rule.maxmul`, floored
at `100`, with a non-positive multiplier coerced to the default `3`, in
**both** ports.

`srcLen` is counted in **UTF-16 code units** in both: free in
TypeScript, one non-decoding pass (`utf16Len`) here.

This subsection previously recorded a difference: Go coerced and floored,
TypeScript honoured `rule.maxmul: 0` literally and rejected valid input
with `unexpected`. It was a divergence by this document's own heading
("These affect parse output for the same input"), filed here rather than
in `DIVERGENCE.md`, and so never pinned. It also recorded only ONE of the
three: repairing it turned up a second (Go's product wrapped, and the
negative result met that same `100` floor, so a LARGER multiplier was a
STRICTER guard) and review turned up a third (source length was measured
in bytes here and UTF-16 units there, so anything above U+007F got a
different budget in each port). Audit item P7.

Pinned in `go/rule_budget_test.go` and `ts/test/rule-budget.test.js`,
which assert the same table at both ends and, separately, that the guard
still stops a runaway grammar.

One thing about `maxmul` is still not aligned, and it is the option's
TYPE rather than the guard: `number` in TypeScript, `*int` in Go, so a
fractional multiplier is expressible only in TypeScript. Recorded in
[`DIVERGENCE.md`](../../DIVERGENCE.md).

### Value-Builder Config (`src`, schema v5): Aligned

`@setval$` and `@push$` take `src` as of builtin schema v5: a member's
value becomes the source text the tree builders accumulated into
`node.src`, rather than the whole `{rule?, src, kids}` node. Omitting it
assigns the node as-is, which is how a member that built its own value
nests. Same semantics in both ports.

`@push$` is config-bound for the first time at v5. Both ports bind it
through their builtin-config factory, so `k.push$` is consumed at grammar
load and never propagates into a child rule.

The flag must be the **boolean** `true` in both, not merely truthy. Go
reads it through a `v.(bool)` assertion; TypeScript tests `true === v`
rather than truthiness for exactly this reason. A serialized
`{"src": "false"}` reaches the engine through `grammar(JSON.parse(...))`
without passing the TypeScript interface, and a truthiness test there
would have switched src ON in TypeScript and OFF in Go for the same
grammar.

One binding detail was a real TypeScript-only bug rather than a
difference, and is worth recording because the shape invites it: config
is consumed only once EVERY action on the alternate has been bound.
Binding eagerly broke an alternate naming the same configured builtin
twice (`a: ["@push$", "@push$"]`): the second bind found the key already
deleted and silently took defaults, so one array element landed as its
source text and the next as a raw tree node. Go already deferred
consumption, so this was a divergence as well as a bug.

Pinned by `ts/test/src-value.fixture.json`, which both suites run.

### `@push$` across a rule replacement: Aligned (was a Go defect)

A rule that allocates a list, pushes into it, and REPLACES itself (`r:`)
to carry the chain on, with a parent reading the result afterwards
(`@bubble$`, `@capture$`). The parent's `Child` pointer still refers to
the rule that was REPLACED, so it reads that rule's node.

TypeScript and Rust get this free: the replacement is handed the same list
OBJECT, and pushing mutates it in place, so the stale pointer sees every
element. A Go slice is a value (`NodeListAppend` returns a new header), so the replaced rule kept the list as it stood before the replacement, and
the parent read it. `["1"]` here, `["1","2"]` there, from the same
serialized grammar.

Go already re-published the grown header to the PARENT for exactly this
reason; the replacement direction was simply never covered. It now walks
the `Prev` chain too, updating only rules that actually held the list this
push grew (`sameGrownList`: same length AND same backing array), so a
replacement that allocated a fresh container of its own cannot clobber the
one it replaced, which is what `child-pusher.fixture.json` pins, and what
TypeScript does.

Info mode wraps a list in a `ListRef`, so the comparison unwraps
(`listHeader`) rather than asserting `[]any`. That surface is Go-only, so
no shared fixture can reach it; `TestPushSurvivesReplacementWithListRef`
covers it directly.

**One case is deliberately not propagated.** Go gives two distinct
zero-length slices the same (or no) data pointer, so an EMPTY list cannot
be told apart from another empty one. The propagation therefore declines
on an empty list rather than guessing: a replacement that allocated its
own empty list before its first push must not overwrite the list of the
rule it replaced, because TypeScript would not. The mirror-image case (a
rule that allocated a list, was replaced before anything went into it, and
is then read by a parent) keeps the empty list here where TypeScript
would show the elements. Recorded rather than silently traded away; no
grammar the compilers emit produces it, because a rule that allocates a
list also pushes into it before replacing itself.

**The empty-list case is no longer traded away.** It could not be settled
by comparing slices, so it is settled by asking a different question. See the next entry, which subsumes it.

A tempting fix that is WRONG, recorded so it is not tried again: making a
parent's `Child` follow the replacement chain forward. TypeScript reads
the PRE-replacement child deliberately (`child-pusher.fixture.json`
returns a `mid` kid, not the `alt` that replaced it) so following it
forward diverges from TypeScript rather than aligning with it.

Not specific to the value annotations that turned it up: `@push$` + `r:` +
a parent read is reachable by any grammar. It went unnoticed because the
json-builder fixture (the array oracle for every port) uses the OTHER
idiom, where the allocating rule and the pushing rule are different rules
in a parent/child relationship, so the parent write-back already covered
it.

Pinned by `ts/test/push-replace.fixture.json`, which all three suites run.

### `@push$` from any depth below the list's owner: Aligned (was a Go defect)

The entry above re-published a grown list header to the PARENT and back
along the replacement chain. Both are one hop from the push. A list can be
grown arbitrarily deeper than that.

A right-recursive repetition helper inherits the list from the rule that
allocated it and pushes from a NEW depth on every iteration, so a
three-element list grows at three different depths. Only the innermost
push landed anywhere the owner could see; the owner kept the empty
original. TypeScript and Rust hand out the same list object, so every
holder sees every element and there is nothing to re-publish.

Every rule now knows which rule holds the authoritative copy of the
container it is building into: `Rule.nodeOwner`, seeded down from the
parent on a push and from the REPLACED rule on an `r:`, and reset to nil
by every builtin that allocates a new container or scalar. `@push$` grows
that rule's list and writes it back there: one write, whatever the
depth.

The seeding has to follow the link the node actually came from. A rule
that replaces itself before pushing a helper (`list` → `list$step1`) has
the replacement parented ABOVE the owner, so inheriting through `Parent`
alone would skip `list`: the list would reach everything except the rule
whose value is read.

Naming the owner rather than searching for it is also what keeps this
linear. Walking the ancestors on each append is Θ(n²) in the length of
the list, which is reachable from untrusted input: measured over this
fixture, 1600 elements took 32.8 ms walking and 4.0 ms with the owner,
against 3.5 ms for the (incorrect) unfixed engine.

Slice identity could not have answered "who still holds this?": two
distinct EMPTY slices share a data pointer, and a list is empty exactly
when the first push happens. That is the case this entry's predecessor
recorded as traded away, and naming the owner removes the question rather
than answering it.

One consequence worth stating: a rule that LIFTS a child's node
(`@bubble$`, `@value$`) inherits that node's owner rather than claiming
ownership. Claiming it would strand the rule that allocated the container
whenever the child was still carrying an inherited one, and a later push
from deeper in the chain would leave the allocator with a stale header.

Go-only bookkeeping for a Go-only problem: an unexported field, no API
or spec-format change, and nothing for the other ports to mirror.

### `@push$` into a NESTED container: Aligned (was a Go defect)

The two entries above are about reaching every rule that holds the list.
This one is about the rules that hold something else.

The container a value builder grows is not always the outermost one. A
list can be a MEMBER of an object (`{a, b:[…]}`) or an element of another
list (`[[…],[…]]`). `@push$` re-published the grown header to `r.Parent`
UNCONDITIONALLY, so whatever the parent was holding was overwritten by
the list:

| grammar shape | TypeScript | Go, before |
|---|---|---|
| a list as one member of an object | `{"a":"ab","b":["1","2"]}` | `{"a":"ab"}` |
| a list inside another list | `[["1","2"],["3"]]` | `["1",["1","2"],["3"]]` |
| a list as an object's only member | `{"b":["1","2"]}` | `["1"]` |

The third is the sharpest: not a damaged map but the wrong KIND of value,
from a grammar that asked for an object.

Free in TypeScript and Rust, where the enclosing map is a different
object and nothing aliases it. The header still genuinely has to be
re-published in Go, but only to a parent building into the SAME
container, and ownership already answers which: an inherited container
gives parent and pusher the same holder, while a freshly allocated one
resets the pusher's owner to itself. The `Prev` walk beside it was
already guarded (`sameGrownList`); the parent write-back was not guarded
at all.

Not specific to the value annotations that turned it up, and not specific
to any compiler's output: any grammar nesting one value container inside
another reaches it. It went unnoticed because every earlier fixture built
ONE container: the json-builder oracle nests maps in maps and lists in
lists through `@setval$`/`@push$` on the same rule, where the parent
holds the container being grown.

Pinned by `ts/test/nested-container.fixture.json`, which both suites run.
`TestPushReachesTheListsOwnerFromAnyDepth`,
`TestPushStopsAtTheAllocatingRule` and
`TestLiftingAnInheritedListKeepsItsOwner` pin the directions separately;
`deep-push.fixture.json` runs in both suites.

Turned up by `; @array` over an ABNF list idiom
(`list = item *( "," item )`), where the emitted helpers fill the
annotated rule's array directly. Not specific to it: any grammar that
accumulates a list more than one rule below where it was allocated hits
the same thing.

### `@push$`'s replacement-chain walk is quadratic: Opt-out shipped, default unchanged

Not a divergence (every port builds the same value) but a Go-only cost,
recorded here because the three obvious repairs are each blocked for a
different reason and two of them have already been written and reverted.

The three entries above each made `@push$` publish a grown list header
somewhere a holder could see it. The last of those directions, the walk
back along the replacement chain, is unbounded: a grammar whose element
rule replaces itself per separator (`{ s: '#CA', r: 'elem', a: '@push$' }`,
which is what @tabnas/json declares) builds a `Prev` chain one rule longer
per element, and every push walks all of it.

Measured over @tabnas/json parsing a top-level array of records, counting
the walk's steps:

| records | pushes | walk steps | steps/push |
| ---: | ---: | ---: | ---: |
| 344 | 1,032 | 59,340 | 57.5 |
| 688 | 2,064 | 237,016 | 114.8 |
| 1,375 | 4,125 | 946,000 | 229.3 |
| 2,750 | 8,250 | 3,782,625 | 458.5 |
| 5,500 | 16,500 | 15,127,750 | 916.8 |

Steps per push double when the record count doubles, so the total is
O(n²). Of the 15.1 M writes at 5,500 records, 10,999 land on the rule the
parent's `Child` still points at and 15,116,751, or 99.93%, land on
replaced rules between it and the pusher.

**It is not confined to pathological input.** Per-record cost over that
same series runs 34.2, 29.2, 33.0, 48.8, 99.5 µs; @tabnas/jsonic parsing
the identical bytes stays flat at 38.2, 31.4, 30.8, 34.8, 31.1 µs. Roughly
two thirds of this port's time on a 1 MB array of records is the walk,
which read for a long while as the port simply being slow on that shape.

jsonic is flat because it does not call `@push$`. It keeps the same
`r: 'elem'` chain and supplies its own close action, which appends and
writes the result to the pusher and its parent and stops there
(`jsonic/go/grammar.go`, `@elem-bc-json`). That is the behaviour the walk
would have after dropping every non-head write.

Three repairs, each blocked:

- **Publish to the chain head only.** `prev` is a public condition path
  (`plugins.md`, the rule graph), so `$prev.prev.node` reads a replaced
  rule and a grammar can observe the skipped writes. Gating the fast path
  on the installed grammar not using a declarative `prev` path would cover
  the declarative surface, but a custom `AltCond` or `AltAction` closure
  reading `r.Prev.Node` is not detectable from the spec.
- **Hold the container by reference while building.** This is what
  TypeScript and Rust do, and it deletes `nodeOwner`, `sameGrownList` and
  the walk together. `Rule.Node` is a public field documented as `[]any`,
  so it changes the public contract.
- **Change the grammar so no chain forms.** Having `list` push a fresh
  `elem` per separator instead of `elem` replacing itself does not fit the
  rule lifecycle: once a rule has pushed a child it moves to its close
  state and its open alternates never run again. Giving @tabnas/json a
  jsonic-style close action instead is expressible in its TypeScript and
  Go halves but not its Rust one, whose grammar is a declarative spec with
  no grammar-local closures by design.

The fourth route is the one that shipped: an opt-in config on the builtin,
`k: { push$: { chain: false } }`, alongside the `src` flag it already
takes. **The default is unchanged** -- absent, the walk runs exactly as
before, which is why the two pinned tests above still pass untouched and
why nothing existing moved. A grammar that declares the key is asserting
that it never reads a replaced rule, and gives up `$prev.node` and
anything else resolving through `Rule.Prev` in this port.

The Rust engine takes the key in its config-field table and ignores it:
it hands out one `Arc`ed container, so there is nothing to re-publish,
but its loader rejects unknown builtin config fields and would otherwise
refuse a grammar the other two ports accept. TypeScript needs no change
because it does not validate config keys -- an asymmetry between those
two loaders that predates this and is untouched by it. No builtin-schema
bump: the version gate refuses a spec an old engine would MIS-handle, and
an old Go or TypeScript engine ignores this key and keeps walking, which
is slower and still correct.

`@tabnas/json` declares it on both `elem` close alternates. Go, same
host, `benchtime=3x`: `records-1mb` 577 ms to 145 ms, `records-cjk-1mb`
179 ms to 79 ms, `numeric-edge-1mb` 11901 ms to 230 ms, and `wide-40k`
flat at ~128 ms, which is the control -- one object, no long array, no
chain to skip. Allocation counts are identical on every row, so what
went is work rather than memory.

The promise is not checkable by the loader, so a grammar CAN declare the
key and then read a replaced rule. That is a reachable different result
for the same serialized grammar and input, and it is registered in
`DIVERGENCE.md` and `test/spec/divergent.tsv` as `push-chain-off`, with
`push-chain-on` as its control, rather than left in a Go-only unit test.

The three blocked repairs above stay blocked. This does not solve the
general problem -- a grammar that DOES read replaced rules still pays the
quadratic, and there is still no way to make the walk cheap without one
of those three trades.

`TestPushSurvivesReplacementAfterABubbledList` and
`TestCustomActionNodeSurvivesOwnerResolution` pin the two shapes that the
reverted attempts broke; any repair has to keep both passing.

### The serialized options surface: every data leaf

Not a divergence: an API gap, now closed, recorded so the shape of the
gap is not forgotten. `OptionsFromMap` (the path `Grammar`,
`SetOptionsText` and a shared options blob take) builds `Options` from a
map, and it used to be a hand-written extractor that named 18 of the 24
option groups: a field it did not name was dropped in silence. Six
groups fell out that way, among them `rewind.history`, `result.fail` and
all of `parse.recover`, so a caller who lowered the rewind bound in a
serialized spec to harden a service got the default instead.

The surface is now defined by type rather than by list: **every
pure-data leaf of `Options` is read, and a gate asserts it.**
`TestOptionsFromMapCoversEveryLeaf` walks `Options` by reflection,
builds a map that sets every leaf, and fails on any that does not
arrive, so a field added to `Options` cannot fall out of the serialized
surface again. Function-valued leaves (`parser.start`, `map.merge`,
the `check` hooks, `parse.prepare`, `budget.onCheck`) are read when the
map carries a function of the right type, which is what a resolved
FuncRef is; a spec carrying only JSON cannot reach them, and that is
what makes them unportable rather than unread.

`MapToOptions` keeps its signature and delegates to `OptionsFromMap`,
which also returns an error for an entry it cannot carry: a serialized
regex that RE2 cannot compile, an ill-typed leaf (`line.chars: {}`,
`tokenSet.VAL: "str"`), or a function reference resolved into a slot
that holds data. The door is typed against `Options` by reflection
before any field is read, so a mistake is named rather than dropped,
which is also what TypeScript now does against the shape of its
defaults; Rust's door was strict already. Prefer
`OptionsFromMap` wherever there is an error to return. Unmarshalling
JSON straight into `Options` is still not a substitute: it rejects a
fractional `rule.maxmul` rather than truncating it (see "Rule-Iteration
Budget" above).

### Token Consumption

When no grammar alternate matches, both implementations raise an immediate
parse error. Token consumption behavior is aligned.

### Ender Characters and Ender Sequences

Aligned.

`options.ender` has two forms and they are read differently. A STRING is
its characters, one ender each; an ARRAY entry is ONE ender, so an entry
longer than a single character is a SEQUENCE that ends a text or number
run where the whole of it starts. TypeScript makes each array entry one
alternative of `cfg.rePart.ender`, which is where that reading comes
from, and the Rust port reads the entries whole.

This port read the array by iterating the RUNES of every entry into
`LexConfig.EnderChars`, a `map[rune]bool` consulted per character, in
which a sequence has no expression at all: `ender: [";|"]` was the two
enders `;` and `|` here and the one sequence `;|` there. Since every
published grammar passes single-character enders, where the two readings
coincide, nothing downstream had caught it.

Two things make the Go representation of this option worth knowing when
porting a grammar:

- **`Options.Ender` is a `[]string` in which every entry is one ender.**
  A string on the serialized door is split into characters by the
  READER (`OptionsFromMap`), not by `buildConfig`, because the two forms
  are indistinguishable once they are in that slice.
- **The config keeps the two kinds apart.** A single-character entry
  goes to `LexConfig.EnderChars` and a longer one to
  `LexConfig.EnderSeqs`. The dispatch table routes a sequence's first
  byte to the verify path, exactly as it does for a multi-byte fixed
  token, so a config with no sequence ender pays nothing for the
  feature.
- **Set enders before the dispatch table is built, not after.** Both
  fields are read once, by `buildLexTables`, and the result is what the
  lexer consults: an ender character becomes a `textStop` entry and a
  sequence's first byte a `textVerify` entry. Adding to either field on a
  config that is already built therefore changes nothing, silently,
  because the byte's entry still says `textContinue` and the verify path
  is never reached. The rebuild is engine-internal
  (`LexConfig.refreshLexTables` is unexported), so the supported routes
  are the options door (`Options.Ender`, which `buildConfig` rebuilds
  from) and a `ConfigModifier`, which runs before the tables are built.
  `SortFixedTokens` also rebuilds them, but it is the fixed-token API and
  relying on that side effect is not the contract.

Number tokens end on the same alternatives, so a sequence ender ends
them too: under `ender: []string{";|"}`, `1;|2` is `#NR:1` and `1;2` is
`#TX:1;2`.

Pinned cross-runtime by `test/spec/lex-ender-array.tsv`.

### Unquoted Text and Quote Characters

Aligned. **A quote character does not end an unquoted text run**, in either
runtime: `a"b`, `a'b` and `` a`b `` are each ONE `#TX` token, and `ab"c"d` is
one token whose value contains both quotes.

TS builds `cfg.rePart.ender` from the space and line chars plus `opts.ender`,
the fixed tokens and the comment starters; `cfg.string.chars` is deliberately
not among them. A grammar that wants a quote to end text says so through
`opts.ender`, which is the documented extension point.

Go's `textStopBase` used to test the string chars as well, which stopped a
text run at the quote. That was the largest divergence class measured across
the fleet: `a"b`, `x:a"b`, `{k:a"b}` and `[a"b]` all parsed in TS and were
parse errors here, and `ab"c"d` came back as `["ab","c","d"]` against
`"ab\"c\"d"` there.

**The boundary, which is the part worth getting right:** a text run may
CONTAIN a quote, but a source that STARTS with one is still a string. `"ab"`
is `#ST`, and `"a` is an `unterminated_string` error, in both. A string only
begins where a text run is not already in progress.

Two consequences for callers, both aligned with TS:

- With `string.abandon`, `matchString` returns nil and the text matcher takes
  the span, so an abandoned string lexes as `#TX` rather than raising.
- With `lex.relex`, a bad string token's span can be re-cut to `#TX` by an
  alternate that wants one; when no alternate can take it, the original
  diagnostic still surfaces.

Pinned by the shared fixture `test/spec/lex-text-quote.tsv`, run by both
suites.

## Aligned Error Handling

Both implementations now share the same error model:

| Feature | TypeScript | Go |
|---|---|---|
| Message templates with `{key}` injection | `options.error` | `Options.Error` |
| Hint templates with `{key}` injection | `options.hint` | `Options.Hint` |
| Default per-code hints | yes | yes |
| Header name | `errmsg.name` | `ErrMsg.Name` |
| Suffix (bool / string / function) | `errmsg.suffix` | `ErrMsg.Suffix` |
| "See also" link line | `errmsg.link` | `ErrMsg.Link` |
| `--internal: tag=...; rule=...; token=...; plugins=...--` block | yes | yes |
| Instance tag when unset | `'-'` (`defaults.ts`) | `'-'` (`DefaultTag`, applied in `Make`) |
| Custom bad-token error code | `tkn.err` wins over `unexpected` | `tkn.Err` wins over `unexpected` |
| Source filename in `--> file:row:col` | `meta.fileName` | `ParseMeta` meta `"fileName"` |
| ANSI colors | `options.color` | `Options.Color` |
| Source site extract with caret | yes | yes |

The remaining difference is delivery: TypeScript throws `TabnasError` as an
exception; Go returns `*TabnasError` as an `error` value and never panics
(see "Error Delivery and the No-Panic Guarantee" below).

### Error template placeholders

The `{key}` vocabulary the two runtimes accept is NOT the same, because
the reference bag each builds for `strinject` is built differently.
Go's `errInjectRef` (`tabnas.go`) carries a fixed set plus the grammar's
own `use` keys; TypeScript spreads the failing token, the rule, the
context, the config and the options into the bag, so every field of each
is addressable.

| placeholder | TypeScript | Go |
|---|---|---|
| `{code}`, `{src}`, `{details}` | yes | yes |
| `{row}`, `{col}` | yes | yes |
| `{pos}` (source offset) | no | yes |
| `{sI}`, `{rI}`, `{cI}`, `{len}`, `{name}` (token fields) | yes | no |

Both injectors walk a dotted path, so `{a.b}` reaches a nested value in
either runtime wherever `a` is in the bag.

A key the grammar put in `token.use` is NOT a stable placeholder in
either runtime, and the shape it would take differs. Whether it reaches
the bag at all depends on which site raised the error: the ordinary
fetch paths in `rules.ts` pass it as `{use: tkn.use}` and Go's
`parser.go` passes it positionally, while other sites pass no `use` at
all. Where it does arrive, TypeScript leaves it nested (so `{use.foo}`,
never `{foo}`) and Go's `errInjectRef` flattens every `use` map to top
level (so `{foo}`, never `{use.foo}`). Two runtimes, opposite spellings,
neither reliable across error sites: write the value into the message
from the grammar rather than reaching for it from a template, until the
bags are aligned.

An unresolved placeholder is left in the message verbatim rather than
raising, so all of this shows up as literal `{sI}` or `{foo}` text
rather than as an error. The case that meets it in practice is a
byte-oriented template for a binary grammar, which is `{sI}` in
TypeScript and `{pos}` in Go (see "Parse a binary format" in each
runtime's guide).

This is a difference in message TEXT, which
[`DIVERGENCE.md`](../../DIVERGENCE.md) records as explicitly not in
parity: only the error `code` is contractual. Aligning the two bags
would be an improvement, not a bug fix.

### `Lex.Next` returns the raw stream: Aligned (was a Go difference)

`Lex.Next` returns every token the matchers produce, IGNORE tokens
(space, line, comment) included, exactly as TypeScript's `lex.next`
does; the parser skips the IGNORE set in its own fetch, as the
TypeScript `parse_alts` loop does around `lex.next`. This port used to
skip them inside `Next` itself, so a plugin driving the lexer directly
from an alternate condition had to filter in one runtime and must not
in the other. `@tabnas/c` carried exactly that split: its TypeScript
side filters and its Go side did not. A Go plugin that reads `Next`
directly and wants only grammar-significant tokens now filters on the
instance's IGNORE set, as the TypeScript plugin does. Lex subscribers
are unaffected: they always saw every token, before any skipping.
Pinned by `TestLexNextReturnsIgnoredTokens`.

## Custom Matchers

TS `match.token` / `match.value` accept `RegExp | LexMatcher`. Go splits the
union across fields:

| TS | Go |
|---|---|
| `match.token[name] = RegExp` | `Match.Token[name] = *regexp.Regexp` |
| `match.token[name] = LexMatcher` | `Match.TokenFn[name] = LexMatcher` |
| `match.value[name].match = RegExp` | `Match.Value[name].Match` |
| `match.value[name].match = LexMatcher` | `Match.Value[name].Fn` |

Full custom matchers (with lexer ordering control) are available in both via
`lex.match` / `Options.Lex.Match`.

### The matcher pipeline is a list in TS and fixed in Go

TypeScript keeps the active matchers in `cfg.lex.match`, an ordered list
built from the `lex.match` registry, so setting a built-in's entry to
`null` removes it from the pipeline entirely:

```js
lex: { match: { fixed: null, space: null, line: null, string: null,
                comment: null, number: null, text: null } }
```

Go has no equivalent. `Lex.Next` calls the built-ins in a hard-coded
order, interleaving custom matchers by priority band, and a `nil`
`MatchSpec` is skipped rather than removing anything. Switching a
built-in off with `Lex: &false` is the whole of what Go offers.

The two end up close in cost for opposite reasons. A disabled built-in
in Go is a boolean test inside one function; in TypeScript it is a call
into `guardedMatcher` that returns immediately, which is why removing it
from the list is worth doing there and impossible here. Measured on a
binary grammar reading 116,508 length-prefixed frames, the TypeScript
removal was about 1.3 times faster than disabling alone.

Go also has no first-char dispatch table (TS `cfg.lex.dispatch`), so
every eligible matcher is consulted at every position.

### Matcher `check` Hooks

Aligned. All eight built-in matchers accept a pre-match `check` hook:
`fixed`, `match`, `space`, `line`, `text`, `number`, `comment`, `string`
(`FixedCheck`, `MatchCheck` and the rest on the Go `LexConfig`). Returning
`{done: true, token}` / `&LexCheckResult{Done: true, Token: t}` claims the
match; returning nothing falls through to the normal matcher.

TS previously declared and consulted `string.check` and `comment.check`
but never copied them out of the options, so those two hooks were dead
there while Go honoured all eight. Both runtimes now wire all eight, and a
matcher carrying a `check` opts out of TS's first-char dispatch table so
the hook runs for every input character, rather than only the ones the matcher
would normally claim.

## Plugin Differences

| Area | TypeScript | Go |
|---|---|---|
| Plugin signature | `(tabnas, opts?) => void \| Tabnas` | `func(j *Tabnas, opts map[string]any) error` |
| Plugin failure | throw | returned `error` |
| Rule definer | `(rs: RuleSpec, p: Parser) => void \| RuleSpec` | `func(rs *RuleSpec, p *Parser)` (no replacement return) |
| RuleSpec alternate/action lists | private; mutated via methods | private; mutated via methods (`AddOpen`/`PrependOpen`/`ModifyOpen`/`ClearOpen`, `AddBO`/`PrependBO`/`ClearActions`, `Fnref`) and read via getters (`OpenAlts`/`CloseAlts`/`Actions`/`HasBO…`), aligned with TS; direct field assignment is no longer possible |
| Funcref `@x/append` vs plain `@x` | same slot (`fr['@x/append'] ?? fr['@x']`) | same slot (aligned) |
| Funcref dedup | by function identity | by function pointer (Go has no per-closure identity; reuse stable ref values) |
| State actions raising errors | Return an error `Token` | Set `ctx.ParseErr` (same effect: parse halts with the error) |
| Plugin defaults | `.defaults` property on the function | `UseDefaults(plugin, defaults)` |
| Option namespacing | Plugin options merged by name | `PluginOptions` / `SetPluginOptions` |

## Merge

Both runtimes implement instance merging (`a.merge(b)` / `a.Merge(b)`)
with the same commutative semantics: options conflict-check rather than
override, all rules carry over, shared rules interleave their
alternates deterministically, and both instances need distinct tags.
Differences:

| Area | TypeScript | Go |
|---|---|---|
| Signature / failure | `merge(other): Tabnas`, throws | `Merge(other *Tabnas) (*Tabnas, error)`, never panics |
| Named-action (fnref) renaming | fnref keys renamed `@x` → `@<tag>:x` (`$`-builtins kept) | none. Go persists no fnref map (`Grammar()` Ref maps are transient); lifecycle action slices carry the wired handlers |
| "Non-default" option detection | compared against the shared defaults tree, an explicitly set default value still merges cleanly | nil/zero field = default; a field explicitly set to the default value on both sides with different values still conflicts (indistinguishable from intent) |
| Identical-alt / lifecycle dedupe | function reference identity, falling back to source-text equality (`fn.toString()`): each plugin run creates fresh closures, so reference identity alone would miss shared base plugins | code-pointer identity (closures from one literal share a pointer), the natural Go equivalent of source equality | 
| Conditioned-alt dedupe | only when the condition is reference-equal (or absent) | never (a condition cannot be proven identical across closures); unconditioned duplicates are unreachable, so both rules are behavior-safe |
| Option conflict paths | TS option names (`lex.match.same.make`) | lowercased Go field names, which coincide for most paths (`rule.maxmul`, `lex.match.same.make`) |

## Deep Option Merge (`util.deep` / `Deep`)

Aligned on opaque values. A value that is not a plain object/array,
a `RegExp` in TS or a struct with no exported fields (`*regexp.Regexp`,
`time.Time` and so on) in Go, **replaces** the base rather than being merged
into it. Merging into such a value cannot copy anything: TS's `for..in`
over a `RegExp` yields no keys (so the parent pattern silently survived a
child override), and Go's reflective field merge skipped every unexported
field and handed back a zero value (so `Deep(reA, reB)` produced a regexp
matching the empty pattern). Both now let the overlay win, which is what
`tn.make({number: {exclude: /new/}})` has always meant.

Structs with exported fields (the `Options` tree) merge field by field
in Go, and plain objects/arrays merge key by key in TS. `undefined`/zero
on the overlay side loses in both.

Three classes of the typed overlay merge as TS does, rather than
replacing:

| class | fields | both runtimes |
|---|---|---|
| slices | `Ender`, `Result.Fail`, `Parse.Recover.SyncGroups`/`SyncTokens`, `Match.TokenOrder` | index-wise: an overlay index wins, positions beyond it keep the base |
| maps of definitions | `Comment.Def`, `Value.Def`, `Match.Value` | recurse into an entry both sides carry; a nil entry removes it |
| `TokenSet` | every set | index-wise onto the default set |

For that to hold, an instance starts from `DefaultOptions()`, which
carries the defaults those overlays merge onto (the three token sets,
the three comment definitions, the three value keywords), exactly as
`tn.options` carries them in TS. `Options()` reports them.

Go cannot spell TS's `undefined` inside a typed slice, so there is no
"keep this index" element: **an empty name in a `TokenSet` slice is the
removed position**, which is what TS's `null` does, and a serialized
`null` arrives as one. `{"KEY": {"#ST", "", "", ""}}` is therefore the
replacement the TS fixture spells `['#ST', null, null, null]`, and a
bare `{"KEY": {"#ST"}}` keeps the default set's other three entries. The
strict-JSON fixtures in both runtimes are written that way. Pinned by
`go/options_overlay_test.go` and `ts/test/options-overlay.test.js`,
which drive the options pipeline rather than `Deep`.

## Go-Specific Features

These are available only in the Go version. They exist for Go client code
(typed access to parse metadata) and are intentionally kept. The examples
below install a grammar (`myGrammar`) that honours the `Info` options and
parse strict JSON; `Implicit` is `false` for braces/brackets and would be
`true` only for a grammar that creates containers implicitly (for example, a
relaxed `a:1` → map).

### `GrammarSpecFromJSON` and the C ABI (`go/clib`)

Go-only, and needed only because Go is typed. `GrammarSpecFromJSON`
turns a serialized spec (`{"options":…, "rule":…, "v":N}`) into a
`*GrammarSpec`. TypeScript needs no equivalent: a parsed JSON object is
already structurally a `GrammarSpec` there, so `tn.grammar(JSON.parse(s))`
just works.

It is exported rather than left a test helper because it is the only way
a caller outside Go reaches the engine, which is what `go/clib` (the
C-ABI shared library) is built on. That library exists so languages
with no tabnas port can use the engine (Python via `ctypes` is the
motivating case); it stays grammar-agnostic, taking a serialized spec
and answering whether input parses. See [`../clib/README.md`](../clib/README.md).

One trap it removes: passing the whole serialized document as
`GrammarSpec{OptionsMap: …}` looks right and `Grammar()` returns no
error, but the rule block is never read, so the engine installs no rules
and every later parse silently returns nothing.

### `Info.Text` Option (`TextInfo`)

Wraps string and text values in a `Text` struct that preserves the quote
character used:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{Text: boolp(true)}})
_ = j.Use(myGrammar)
result, _ := j.Parse(`"hello"`)
// result: tabnas.Text{Quote: `"`, Str: "hello"}
```

### `Info.List` Option (`ListRef`)

Wraps arrays in a `ListRef` struct with metadata:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{List: boolp(true)}})
_ = j.Use(myGrammar)
result, _ := j.Parse(`["a","b","c"]`)
// result: tabnas.ListRef{Val: []any{"a", "b", "c"}, Implicit: false}
```

### `Info.Map` Option (`MapRef`)

Wraps objects in a `MapRef` struct with metadata:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{Map: boolp(true)}})
_ = j.Use(myGrammar)
result, _ := j.Parse(`{"a":1}`)
// result: tabnas.MapRef{Val: map[string]any{"a": 1.0}, Implicit: false}
```

## Source Position: the `Site` Type

Same data, one name for it. TypeScript spells a source location as three
loose fields, and spells them three times: on `Point`, on `Token`, and in
the `ScanOut` record its scan driver writes back (`ts/src/lexer.ts`). Go
and Rust name that triple `Site` and reuse it in all three places.

| Runtime | Shape |
|---|---|
| TypeScript | `sI` / `rI` / `cI` fields, repeated on `Point`, `Token` and `ScanOut` |
| Go | `type Site struct { SI, RI, CI int }`, embedded in `Point` and `Token`; `ScanOut` is `Site` |
| Rust | `pub struct Site { si, pos, ri, ci }`, a `site` field on `Point` and `Token` |

This is an API shape, not a parity claim. The values a parse reports are
unchanged in every runtime, which is what the token-stream parity runner
(`ci/parity/run-parity.sh`) asserts.

What the two ports do differently, and why:

- **Go embeds, Rust does not.** Go's embedding is anonymous, so `pnt.RI`
  and `tkn.CI` resolve as before and an encoded `Token` keeps its flat
  shape. Only a composite literal changes: write
  `Point{Len: n, Site: Site{SI: 0, RI: 1, CI: 1}}`. Rust has no
  embedding, so the field is named and reads are `token.site.ri`.
- **Rust's `Site` carries a fourth field, `pos`, and it is not extra
  data.** TypeScript indexes source by UTF-16 code unit and reports that
  same number in a diagnostic. Rust slices by UTF-8 byte, so `si` is what
  it slices with and `pos` is what it reports, and the two together say
  what TypeScript's single `sI` says. See the Unicode section below and
  DIVERGENCE.md "Column positions for astral characters" for what that
  costs at the edges.
- **No length.** `Point.Len` is the length of the whole source and a
  token measures its own matched text, so the two mean different things
  and only the position is shared.

Both ports keep their lexer cursor as private scalars rather than a
`Site`, since neither cursor holds a byte offset directly: Go derives one
from its scan, Rust counts characters and converts. `lex.Cursor()` (Go)
and `Lexer::point()` (Rust) are where that private state becomes a
`Point`, and a `Site` with it.

`Scan` keeps taking `startSI, startRI, startCI` as loose numbers, because
a caller such as the comment matcher tracks them as locals against a
sliced string and has no `Site` to hand. The result is a `Site`, so the
common case assigns in one statement.

## Internal Structure: Scan-Spec Lexer (Aligned)

Both lexers use the declarative scan-spec design: a packed-action state
machine driver (`Scan` / TS `scan()`), per-byte class tables built by
`BuildCharRunSpec` / `BuildLineRunSpec` / `BuildStringBodySpec`, and a
shared matcher entry guard (`guardedMatch` / TS `guardedMatcher`). The
space, line, comment-eatline, and string-body walks all run on the driver,
and the scan primitives are exposed via the util bag in both runtimes so
plugin authors can build their own matchers on it. Both use a fallback
classifier beyond the fast-path table: TS for UTF-16 code units ≥ 256,
Go for UTF-8 lead bytes ≥ 0x80 (decoding the full rune); see the
Unicode section below.

## Serialized Regex Flags (`@/…/flags`)

Aligned, by translation rather than by copying.

A serialized grammar carries a regex terminal as `@/pattern/flags` (or
`@~/pattern/flags` for the eager form). That string is **shared** between
the runtimes, and it holds **JavaScript's** flags, because TypeScript
writes them natively. Go therefore lowers them to RE2 rather than passing
them through: copying them verbatim into an inline `(?flags)` group is
wrong twice over: RE2 rejects most of them outright, and accepts one with
an entirely different meaning.

| flag | Go | why |
|---|---|---|
| `i` `m` `s` | kept | same meaning in both engines |
| `u` | **dropped** | RE2 needs no equivalent, being natively rune-based, which is what `u` asks JavaScript to be |
| `g` `y` `d` | dropped | they govern the JS matcher's statefulness and output (`lastIndex`, sticky, match indices), not the language matched; the engine calls `FindString` once per position |
| `v` | **refused** | unlike `u` it changes what a class MEANS (set operations, string literals inside classes), so it is not a no-op |
| anything else | refused | see `U` below |

### Why dropping `u` is sound

`u` is not cosmetic in TypeScript: without it a JS regex is **UTF-16
code-unit** based, and an astral character is two units. It is also not
confined to emoji grammars: a negated class (`[^\n]`) and `.` both need it,
because the complement of any set contains astral code points. The
question is only whether RE2's native behaviour already IS the flag's
behaviour. Measured, case by case:

| pattern | input | RE2 | JS `u` | JS without `u` |
|---|---|---|---|---|
| `^[a-z]$` | `q` | match | match | match |
| `^[\u{1F600}-\u{1F64F}]$` | `😀` | match | match | **does not compile** |
| `^[^\n]$` | `😀` | match | match | **no** |
| `^.$` | `😀` | match | match | **no** |
| `^.{2}$` | `😀` | no | no | **match** |
| `^.{2}$` | `😀😀` | match | match | **no** |
| `^[^\n]{2}$` | `😀` | no | no | **match** |

RE2 agrees with JS-**with**-`u` on every row and differs from JS-without
on five. In particular `.` consumes one astral character whole, and
`.{2}` correspondingly does **not** accept a single astral character as
two, a real bug once fixed on the TypeScript side, and the one this
translation must not reintroduce.

Both halves of that table are pinned, so this is an agreement between the
runtimes rather than a claim about RE2: `go/regexflags_test.go` and
`ts/test/regex-flags.test.js` assert the same patterns, inputs and
answers.

### Why an unknown flag is refused rather than ignored

RE2 accepts `(?U)`, and it means *swap greedy*. A letter passed through
because it was unrecognised could therefore change the language a grammar
matches, silently. Refusing is the safe default, and it is not silent
either: an unbuildable serialized regex leaves the original `@/…/` string
in place, which `OptionsFromMap` reports as an error naming the token,
and `Grammar` returns as an install error.

### Two related non-equivalences this does NOT fix

Both predate the flag question and are independent of `u`:

- **`\s`** is ASCII-only in RE2 (`[\t\n\f\r ]`) and Unicode-aware in
  JavaScript (it includes NBSP, U+2028, …), with or without `u`.
- **`(?i)`** case-folds by Unicode rules in RE2, which matches JS `iu`
  rather than JS `i` alone.

A shared grammar that depends on either will differ between the runtimes,
which makes these DIVERGENCES rather than porting notes: this file is a
porting guide, and a different parse result for the same input belongs in
the parity record. They are now recorded in
[`DIVERGENCE.md`](../../DIVERGENCE.md) under "Regex dialect in serialized
terminals", with a parse-level reproduction pinned in both runtimes.
Prefer an explicit class over `\s` in a serialized terminal.

## Unicode / UTF-8

Both runtimes handle UTF-8 characters of all sizes (2/3/4-byte
sequences; BMP and astral planes) in keys, values, strings, comments,
and escapes, and both accept any Unicode character as a configured
matcher char (space/line/quote/ender sets) via their fallback
classifiers. The shared `include-json-utf8*.tsv` fixtures pin the
common surface. Mechanical differences:

| Area | TypeScript | Go |
|---|---|---|
| Scan unit | UTF-16 code units | UTF-8 bytes (runes decoded on demand) |
| Error columns | UTF-16 units (astral char = 2) | Runes (any char = 1) |
| Surrogate pairs (either escape spelling) | Implicit (UTF-16 strings) | Explicitly combined, on the code unit sequence |
| Lone surrogates | Preserved (JS strings allow them) | U+FFFD (matches encoding/json) |
| `\u{...}` braced escapes | 1-6 hex digits, ≤ U+10FFFF, else `invalid_unicode` | Same |
| Invalid UTF-8 input bytes | n/a (JS strings are UTF-16) | Passed through byte-for-byte, never a panic |

Column positions agree between the runtimes except for astral-plane
characters (TS counts 2, Go counts 1).

## Error Delivery and the No-Panic Guarantee

TypeScript throws `TabnasError`; Go returns errors, and the Go API
guarantees it **never panics**:

- Parsing wraps a recover guard that converts a panic (including one
  from a plugin callback or a custom matcher) into an `"internal"`-code
  `*TabnasError`, with **one deliberate exception**. A non-nil
  `*TabnasError` keeps its own code, because an action raising one is
  reporting a defect in the INPUT, not an engine bug, and relabelling it
  would leave a grammar unable to diagnose what it parses. That mirrors
  TypeScript, whose catch is
  `if (e instanceof TabnasError) err = e; else throw e`.

  The preserved error is rebuilt through the normal error funnel, so it
  arrives with the source excerpt, tag, hint and rule context every other
  error carries: a plugin can populate only the exported fields.

  A typed **nil** (`var te *TabnasError; panic(te)`) is not a usable
  error and still becomes `"internal"`.

  Where the ports still differ: with `Options.Parse.Recover` enabled,
  TypeScript turns a terminal `TabnasError` into a `{value, errors}`
  recovery result, while Go returns it as fatal. That is true of every
  panic in this port, not only a preserved one, and predates the
  exception above.
- `Grammar` has the same guard for malformed specs. The guard is for
  panics the engine did not expect; the engine's own validators do not
  use it. A caller's mistake that the engine detects (a matcher-owned
  token bound to a fixed literal, a serialized regex RE2 cannot
  compile) comes back from `Grammar`, `SetOptionsText`, `ApplyOptions`
  and `OptionsFromMap` as a plain error naming the mistake, never as an
  `"internal"` error. The chaining doors that have no error channel,
  `Make` and `SetOptions`, panic on the same input, as the TypeScript
  guard throws; `ApplyOptions` is `SetOptions` with the error returned.
- APIs that previously panicked now return errors: `Derive` returns
  `(*Tabnas, error)` (a failing plugin during child derivation mirrors
  TS `make()` throwing), and `MakeRuleCond` returns
  `(AltCond, error)` for unknown operators.
- `go test -fuzz=FuzzParse .` exercises the guarantee against
  arbitrary byte input.

## Type System

TypeScript returns untyped `any`. Go returns `any` but the concrete types are
predictable:

| Value | Go Type |
|---|---|
| Objects | `*OrderedMap` (insertion-ordered; `Map.Plain:true` → `map[string]any`, or `MapRef` with the info option) |
| Arrays | `[]any` (or `ListRef` with option) |
| Strings | `string` (or `Text` with option) |
| Numbers | `float64` |
| Booleans | `bool` |
| Null | `nil` |

Two consequences of that table are deliberate, and both are recorded in
the repository's divergence record. Object key order is out of the
parsed-value contract (ADR-15): `*OrderedMap` keeps insertion order,
TypeScript's plain object puts integer-like keys first, and this port must
never emulate that. A parse that sets no value answers `nil`, where
TypeScript answers `undefined`; the engine's `Undefined` sentinel is
unwrapped at the parse boundary on purpose.

## `options.tokenSet`

Both runtimes accept a `tokenSet` option and apply it identically from
either construction path: `Make(opts)` and `SetOptions(opts)` are
equivalent, as are TS `new Tabnas(opts)` and `tabnas.options(opts)`.
How the value combines with the built-in set differs:

| Area | TypeScript | Go |
|---|---|---|
| Type | `{ [name: string]: (string \| null \| undefined \| typeof SKIP)[] \| typeof SKIP \| undefined }` | `map[string][]string` |
| Combination with the default set | index-wise deep merge with `defaults.tokenSet`; so `{ KEY: ['#ST'] }` keeps the tail and yields `[#ST, #NR, #ST, #VL]` | index-wise as well: `{"KEY": {"#ST"}}` yields `[#ST, #NR, #ST, #VL]`, the same shape |
| Clear one position | `null` at that index | `""` at that index (an empty name is skipped) |
| Empty a set, keeping the name | clear every position: `['#ST', null, null, null]` narrows to one, `[null, null, null, null]` empties it | the same: `{"#ST", "", "", ""}` narrows to one |
| `[]` / `{}` as the whole value | **not** an empty set: an array overlays index-wise, so overlaying nothing leaves every default standing | the same |
| Whole value "leave this name alone" | `undefined` or `SKIP`; a name with no default behind it is then dropped rather than installed empty | no spelling: Go has no sentinel here, and an absent map key is the only way to say it |
| Whole value `null` | a load fault on every door: `options.tokenSet.KEY: expected array, got null` | a load fault on the **serialized** door, `options.tokenSet.KEY: expected array, got null`. The typed door has no such value: `map[string][]string{"KEY": nil}` is an empty slice, not a null, and installs a present, empty set |

| A reserved name (`__proto__`, `constructor`, `prototype`) | a load fault: the deep merge will not carry a key that reaches the prototype chain | the same, though Go has no such hazard: the code is the contract, so a grammar naming a set `constructor` must not load here and fault there |
| `tokenSet` itself `null` | a load fault, `options.tokenSet: expected object, got null` | the same |

The last two rows are an API-shape difference and not a parity one: `SKIP`
and a JSON `null` are values a document can carry and Go's typed map
cannot, while a `nil` slice is a value Go's map can carry and a document
cannot. The one place both runtimes read the SAME input is the serialized
door, and there all three runtimes answer alike: TypeScript, Go and Rust
each refuse a null whole value.

Both runtimes late-bind token-set references in rule alternates, so an
override applies to alternates that were declared before it. In Go the
declared names are kept on `AltSpec.SNames` and re-resolved against the
parsing instance; alternates built from raw `[]Tin` carry no names and
match exactly what they were given. The lexer's `match.token` gate (a
custom match token is only produced where the current rule position
expects it) reads the same late-bound slots, so adding a custom token to
`#KEY` / `#VAL` by override is enough to have it lexed.

## Rule Declaration Order

A TS `GrammarSpec.rule` object keeps insertion order for free; a Go map
has none. Go therefore records declaration order explicitly:

- `RuleSpec.Def`. A monotonically increasing definition index stamped
  when the spec is first created (`(*Tabnas).Rule`, `Grammar`,
  `GrammarText`, `MakeRuleSpec`). Redefining an existing rule does not
  renumber it. Zero means the spec was built as a bare struct literal.
- `(*Tabnas).Rules() []*RuleSpec` and `(*Tabnas).RuleNames() []string`.
  The grammar in declaration order. Unstamped specs sort last, by name,
  so the result is always deterministic. `RSM()` remains unordered.
- `GrammarSpec.RuleOrder []string`. Declares the order of the `Rule`
  map's entries. Without it, `Grammar()` applies rules in sorted-name
  order (deterministic, but alphabetical rather than as-declared).
  `GrammarText` fills it in automatically from the source text's key
  order, so text grammars need not supply it.

## Per-parse error list: `ctx.errs` (TS) / `ctx.Errs` (Go)

Both runtimes carry a per-parse error list, appended at each error's
CONSTRUCTION site, so the error the parse reports is also the last
entry; a clean parse leaves it empty and every parse starts fresh.
Pinned by `ts/test/errs.test.js` and `go/errs_test.go`.

The shapes differ because the error channels do:

| | TypeScript | Go |
|---|---|---|
| Field | `ctx.errs: TabnasError[]` | `ctx.Errs []*TabnasError` |
| Recorded by | the `TabnasError` constructor, so every raise site (engine or plugin) records for free | `ctx.recordErr`, called at each engine construction site (`makeErrorIn`, plus the two `Lex.Next` raises and the deferred relex raise) |
| Guard | `try/catch`, since a frozen array must not mask the error | a nil-safe receiver, since a `Lex` built without a `Context` has no list |

`ctx.ParseErr` is unchanged and remains the grammar-facing error TOKEN
that halts the parse (documented in `doc/plugins.md`): it is a single
slot with set-once semantics that in-engine consumers and ~20 sibling
grammar repos rely on. `Errs` is additive and never replaces it.

One gap, deliberate: Go rejects an empty source in `parseInternal`
before any `Context` exists, so that one error cannot be recorded
(TS records it). It is unobservable today (no `Context` is reachable)
but the Go equivalent of TS's `{ value, errors }` result must
synthesize a one-element list there.

## Error recovery (`options.parse.recover`)

Both runtimes support opt-in panic-mode recovery: a parse error is
recorded, the lexer skips to a sync point derived from the live rule
stack × close-alternate group tags (with a structural fallback for
untagged grammars), the rule stack pops to a rule that accepts the sync
token, and parsing continues. Pinned by `ts/test/recover.test.js` and
`go/recover_test.go`.

| | TypeScript | Go |
|---|---|---|
| Enable | `parse: { recover: { enabled: true } }` | `Parse: &ParseOptions{Recover: &RecoverOptions{Enabled: true}}` |
| Results | `parse()` returns `{ value, errors }` | `ParseRecover()` returns `(value, errs, err)` |
| Sync tags | `syncGroups` (replaces the default set) | `SyncGroups` (same semantics) |
| Extra tokens | `syncTokens: ['#CA']` | `SyncTokens: []string{"#CA"}` |
| Caps | `maxSkip`, `maxRecoveries`, `suppress` | `MaxSkip`, `MaxRecoveries`, `Suppress` |

**Go returns a third value where TS changes the shape of the first.**
`Parse` is public and called throughout the fleet; returning a
`{value, errors}` struct through `any` would force every existing
caller into a type assertion just to learn whether they got a value or
a wrapper. `Parse` therefore keeps its signature and yields the partial
value with a nil error, and `ParseRecover` is how a caller asks what
was recovered from. Same constraint class as `Sub` and `ctx.ParseErr`.

Two Go-specific hazards the port has to handle, both stemming from Go
signalling lexer failure through a different channel than TS:

- **The lexer latches `Lex.Err`, and caches the `#ZZ` it answers while
  latched.** Clearing only the error leaves that cached end token in
  place, so every later fetch still reports end-of-source and recovery
  syncs on EOF, silently abandoning the rest of the document.
  Recovery clears both.
- **On unlexable input the lexer used to set `Lex.Err` and return
  `#ZZ`**, claiming end-of-source with source still ahead of the scan
  point, which ended recovery at the first bad character and abandoned
  the rest of the document. With recovery on, the lexer now hands the
  `#BD` token to the parser instead, exactly the deferral it already
  made for negotiated lexing, and exactly the condition TS defers on
  (`rules.ts`, where the throw is guarded by
  `!cfg.parse.recover.enabled`). The skip loop then walks the run token
  by token and counts it against `MaxSkip`.

  That deferral is what removed the duplicate diagnostics: one
  unlexable run is now one diagnostic. `{"a":true blah blip,"b":1}`
  reported three errors at a single offset before it and reports one
  after; `{"a": zzz, "b":2}` reported four.

With recovery on, neither runtime's parse fails outright: a
completeness failure after the rule loop is recorded as one more
diagnostic and the partial value still comes back.

### Unlexable runs: lexer soft mode

Aligned. With recovery on and relex off, an unlexable span is absorbed
at token-FETCH time rather than handed to the alternates, so the parse
continues as though it were not there and the text after it still
parses. Contiguous bad tokens coalesce into one diagnostic whose region
grows with the run, marked `Recovered.Bad` (TS: `recovered.bad`), and
the same `suppress` window recoveries use applies to a fresh run with
nothing consumed since the last one.

That is why an unlexable word is one squiggle rather than one per
character, and why two separate words are two.

Verified against TS on `{"a":true blah blip,"b":1}`:

| | TypeScript | Go |
|---|---|---|
| `suppress: 0` | 2 errors, `{"a":true,"b":1}` | 2 errors, `{"a":true,"b":1}` |
| `suppress: 8` | 1 error, `{"a":true,"b":1}` | 1 error, `{"a":true,"b":1}` |

Beyond `MaxSkip` the run gives up like any other over-long recovery,
and beyond `MaxRecoveries` the parse gives up: Go checks that cap
before recording rather than after, so the list does not overshoot.

The one remaining difference on these inputs is the `undefined`/`nil`
value-model split described above, not the diagnostics: a key whose
value never parsed is absent from `JSON.stringify` in TS and `null` in
Go, in both cases with the key present.

## Parse budget (`options.parse.budget`)

Both runtimes carry the opt-in cancellation/budget hook: a callback
runs every N rule-loop iterations and cancels the parse with the
`cancel` error code on a false return. Off by default in both, costing
one test per iteration. Pinned by `ts/test/budget.test.js` and
`go/budget_test.go`.

| | TypeScript | Go |
|---|---|---|
| Option | `parse: { budget: { checkEveryN, onCheck } }` | `Parse: &ParseOptions{Budget: &BudgetOptions{CheckEveryN, OnCheck}}` |
| Callback | `(ctx) => boolean \| void` | `func(ctx *Context) bool` |
| On cancel | throws `TabnasError('cancel')` | returns a `"cancel"` `*TabnasError` |

The callback signature is the one real difference. TS accepts
`boolean | void`, so a checker that only observes can return nothing;
Go has no undefined, so an observer returns `true`. Both halves of the
option are required in both runtimes: an interval with no checker, or
a checker with no interval, leaves the hook off rather than
half-enabled.

Cancellation is an ordinary error of the parse, so it is recorded in
`ctx.Errs` / `ctx.errs` as the last entry like any other. In TS's
recovery mode it surfaces through `{ value, errors }`; Go, still
fail-fast, returns it directly.

## Post-process rule event (`sub({ ruleDone })` / `SubRuleDone`)

Both runtimes carry the third subscriber kind: it fires AFTER each rule
pass, with the matched tokens recorded on the rule and the state
transition applied, so it can report what the pass actually did, which
the pre-process event cannot. This is the span-bearing structural
stream an outline provider is built from. Pinned by
`ts/test/ruledone.test.js` and `go/ruledone_test.go`.

| | TypeScript | Go |
|---|---|---|
| Subscribe | `tn.sub({ ruleDone })` | `tn.SubRuleDone(fn)` |
| Callback | `(rule, ctx, done) => void` | `func(rule *Rule, ctx *Context, done RuleDone)` |
| Payload | `{ state, alt: { b, g, p, r, err }, forced }` | `RuleDone{State, Alt: *RuleDoneAlt{B, G, P, R, Err}, Forced}` |
| Group tags | `g: string[]` | `G []string`, split from the comma-separated `AltSpec.G` |

**The subscription is its own method in Go, and that is forced.** TS's
`sub()` takes an options object, so a new event is a new key. Go's
`Sub(lexSub, ruleSub)` is positional and public, and every sibling Go
grammar repo calls it, and widening it would break all of them for an
event most do not use. Same constraint class as `ctx.ParseErr`.

`Alt` is nil only when the rule state had no alternates at all. When it
had some and none matched, `Alt` is non-nil with just `Err` set,
mirroring TS's distinction between a null `_dalt` and a failed one:
collapsing the two would make a grammar hole read as a syntax error.
The `G` slice is a fresh copy on every event: `AltSpec.G` is live
grammar configuration and a consumer must not reach it through the
payload.

One gap, and it closes with A2: `Forced` marks a close synthesized by
error recovery, so it is always false in Go until Go has recovery.

## Lex-event retraction on unrelex: both runtimes

Under negotiated lexing, both runtimes re-announce the RESTORED token
to lex subscribers when a speculative recut is undone, so a
position-keyed consumer's reconstruction ends on the token the parse
actually proceeded with. Pinned by `ts/test/lexevents.test.js` and
`go/lexevents_test.go`.

The consumer contract (documented in `ts/doc/api.md` under `tn.sub`)
is identical in both: process events in order, keep the newest per
source position, and let each kept token's span shadow older events
inside its extent, which is what retracts the interior events an
abandoned speculation fired.

**One unit difference, and it matters to consumers doing the span
arithmetic**: TS `Token.sI`/`len` count UTF-16 code units, Go
`Token.SI` is a BYTE offset and spans are byte lengths. The contract
is the same; the arithmetic is in each runtime's own unit. A host
mapping either to editor positions (an LSP server) converts as it
already must for diagnostics.

## Continuations API (`tn.continuations(src)` / `Continuations`)

Both runtimes answer what tokens could legally follow a prefix: the
completion primitive of the unified-LSP design. Pinned by
`ts/test/continuations.test.js` and `go/continuations_test.go`, whose
expectations were checked against the OTHER runtime on the same
prefixes rather than restating each engine's own output.

| | TypeScript | Go |
|---|---|---|
| Call | `tn.continuations(src)` | `tn.Continuations(src)` |
| Returns | `{ tins, tokens }` | `(tins []Tin, names []string)` |
| Source of the sets | the collated per-rule `tcol` table | `AltSpec.S` directly |

**Go has no `tcol`**, the lookahead table TS collates at normalize
time, and does not need one: `AltSpec.S` already holds the per-position
tins, so every set here (a rule's openers, its close-leading tokens,
an alternate's next position) is read straight off the alternates.
`closeInfoOf` does the same for recovery. The information is identical;
only the intermediate table is absent.

Verified equal on the strict-JSON fixture for `""`, `{`, `{"a"`,
`{"a":`, `{"a":1`, `{"a":1}`, `[`, `[1` and `[1,`.

Shared semantics:

- **Path-aware.** Each alternate contributes only the position it is
  actually waiting on, so a sibling whose own prefix never matched adds
  nothing (`{"a"` yields `['#CL']`, not key starters). At a failure the
  set is recorded by the failing pass itself, while its own lookahead
  is still buffered.
- **Pop closure.** While a rule can close on anything, its parent's
  close continuations are legal here too.
- **Push closure.** An alternate fully matched at the query position is
  about to push another rule, so that rule's openers are legal, which
  is why `[1,` offers the next element's value starters as well as `]`.
- **Prefixes that parse still answer**, with `#ZZ` included to mean
  "stopping here is legal". It is a sentinel, not something a user
  types: a completion provider should drop it and read it as "this
  document is already valid". A prefix that parsed completely and was
  never asked for more answers exactly `#ZZ`, not the start rule's
  openers: `a` against a grammar that accepts one `a` must not offer a
  second. Go answered `#A` there until its capture stopped discarding
  the parser's trailing end-of-source fetch, which arrives with no rule
  in hand; pinned in all three runtimes by the "only the end is legal"
  test.
- **Recovery never changes the answer.** The query forces its own parse
  fail-fast whatever the instance is configured for, since it must stop
  AT the query point rather than skip past it.

The set is an over-approximation in both: conditions and counters may
still reject a listed token.

The diagnostic `expected[]` field deliberately keeps its position-0
semantics in BOTH runtimes: it is pinned by the shared diagnostic.tsv
parity fixtures, and the improved computation lives in this API only.
