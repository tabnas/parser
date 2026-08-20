# Risk Register for the Approved Rust Port

Third in the series after
[`doc/rust-port-feasibility.md`](rust-port-feasibility.md) and
[`doc/rust-callback-porting-strategy.md`](rust-callback-porting-strategy.md).

Both of those recommended against a full port. **That recommendation has
been overridden by the maintainer; the port is going ahead.** This
document does not re-argue the decision and does not restate the case
against it. Its job is the opposite one: find what will block the
approved port, what will bite late, and what to do about each. Where a
finding contradicts one of the earlier documents — or an earlier draft of
this one — it says so and gives the measurement.

Citation convention as before: a bare `§3.4` is a section of the
feasibility report, `§2.3 strategy` is the callback strategy document,
and sections of *this* document are written `§4 here`.

> **Provenance.** Every figure below was re-measured against the tree at
> `a9c8c67` unless it is explicitly marked as carried over from an earlier
> sweep. Re-measurement changed six headline claims, three of them in ways
> that change what to do next: the options-merge divergence is not in the
> function the previous draft named (§2.2 here), the match-token gating
> divergence is documented rather than silent and the documentation is
> wrong (§2.1 here), and the "the utility fixtures are worthless" finding
> is backwards (§2.3 here). Figures carried over without re-measurement
> are marked *(unverified)*. Treat any number here without a file:line or
> a measurement beside it with suspicion.

## Summary

**Retire the demand question this week, before anything else, and rank
the rest after the answer.** That is the single most important piece of
advice in this document. The port's likely failure mode is not that it
cannot be built — the feasibility report already established that it can,
and the hard structural questions have answers. The likely failure mode
is that it is built correctly into a niche that is already occupied. The
incumbent is measurable: `go/clib` is 287 non-test lines
(`go/clib/core.go` 183 + `go/clib/tabnas_c.go` 104) and `py/tabnas.py` is
206 lines of `ctypes` over it, and `py/README.md:26-30` advertises
exactly v0.1's stated audience — "supply a serialized GrammarSpec — the
pure-data form a front-end compiler emits (`@tabnas/gbnf` for llama.cpp
GBNF, `@tabnas/abnf` for RFC 5234 ABNF)". So the question is not "does a
consumer exist"; it is whether v0.1's three genuine differentiators over
that incumbent — a parsed value tree, the 15-field structured diagnostic,
and no cgo — justify 15-18k non-test lines against roughly 500. That is
answerable this week, and it is the single most decision-relevant
sentence either of the earlier documents could have contained. Neither
contains it.

The three things most likely to kill this port, in order:

1. **v0.1 ships correct and irrelevant.** Zero Rust consumers exist
   across the 34-repo fleet; 31 repos peer-depend on `@tabnas/parser` and
   29 `go.mod` files require the Go module. Measured, **every fleet repo
   with a `ts/src` carries arrow functions except one** — and that one,
   `jsonc`, is still unreachable, because
   `jsonc/ts/src/jsonc.ts:63` loads its grammar as *jsonic text* through
   `new Tabnas().use(jsonic).parse(grammarText)`, which needs a jsonic
   engine to read. And the named first user, the BNF/ABNF/GBNF family,
   has a live defect on exactly the path v0.1 would consume (§5 here).
2. **The contract at the tier v0.1 targets is not written down, and the
   conformance artifact cannot see the gap.** Measured: the shared corpus
   leaves `builtins.js` and `merge.js` at **0.00% function coverage** and
   `context.js` at 9.09%, while the full TypeScript suite puts all three
   above 92%. The lexer plugin tier has one fixture in eleven; the
   advanced features have none. Where the two runtimes were measured they
   already disagree — and in one case the *porting guide itself* asserts
   there is no behavioural difference where there is one (§2.1 here).
3. **Capacity, not adjudication.** 15-18k non-test lines and 7-10
   engineer-months (§Summary feasibility) against one person who is also
   sole maintainer of 31 dependent repos at two runtimes. Adjudication
   throughput is *not* the constraint — `AGENTS.md:26` already supplies a
   standing default answer for every TS/Go disagreement, which unblocks
   most of the backlog without a single new ruling (§3 here). Nine open
   PRs is a snapshot, not a trend.

### Two things changed since the previous draft

**A function-free serialized spec joins the parity contract.** The
previous draft ranked "v0.1 as scoped cannot join the parity contract at
all" as project-killing and certain, on the ground that 65 of 265 fixture
rows run only through closure-carrying strict-JSON grammars. That is true
of the *canonical* grammars — `ts/test/json-plugin.ts` declares eight
named action closures plus `exclude: /^00+/` (`:43`) and
`result: { fail: [undefined, NaN] }` (`:55`), which has no JSON form — but
it is not true of the fixture rows. A 2,489-byte `json-core` spec built
from `ts/test/json-builder.fixture.json` plus the declarative half of that
plugin's options, with the regex serialized as `"@/^00+/"` and no `ref`
bag at all, passes **55/55** `include-json*` rows with full value
comparison in TypeScript (re-measured here) and **55/55** plus **10/10**
`diagnostic.tsv` rows in Go (`go test`, re-measured here). The blocker is
retired; it was about two hours of work. It is struck from the register
below rather than left standing next to its own refutation.

**The options-merge divergence is not in `Deep`.** The previous draft
presented `deep` and `Deep` as diverging on six fields of the same input.
Fed the identical plain `map[string]any` values, they do not:

| field | TS `deep` | Go `Deep` (maps) | Go `Deep` (typed `Options`) |
|---|---|---|---|
| `space.chars: ""` over `" \t"` | `""` | `""` | `" \t"` |
| `number.sep: ""` over `"_"` | `""` | `""` | `"_"` |
| `ender: ["z"]` over `["a","b","c"]` | `["z","b","c"]` | `["z","b","c"]` | `["z"]` |
| `comment.def.slash: {lex:false}` | keeps `start`/`line` | keeps `start`/`line` | erases both |

The divergence lives in `deepMergeStruct` (`go/utility.go:182-305`) and
its `IsZero()` absence test, which only runs on the typed tree. That is
not a pedantic correction, because the previous draft's retirement — "a
`test/spec/options-merge.tsv` fixture of ~30 rows" — would have driven
`Deep` with JSON-decoded maps and passed green in both runtimes with the
defect untouched. The risk is real and reaches serialized specs end to
end (`MapToOptions({"number":{"sep":""}})` yields `Sep:""`, and
`Deep(base, that)` then keeps `"_"` — measured), but the fixture has to
drive the *options pipeline*, not the utility function.

---

## 1. Blockers, Risks and Costs

Three categories, because they need different responses.

**Blockers** must be decided by a human before Rust code is written,
because they change a struct definition, a public signature, or the
meaning of "conformant". No amount of engineering retires one.

**Risks** may bite, with a probability and a discovery point. Tooling,
tests or a written contract retire them.

**Costs** are known work with a known shape. They belong in the estimate,
not in the register.

### 1.1 Rank on residual damage, not on unmitigated damage

The previous draft ranked by severity × likelihood and produced six
blocker-or-project-killing entries out of twelve. That is the wrong
arithmetic for a register whose own plan schedules cheap retirements for
most of them. The expected damage of a risk you have costed a retirement
for is severity × P(the retirement fails), plus the retirement's cost.
Ranked that way the top of the table empties — and the items that stay at
the top are the ones whose retirement is *not* an engineering task.

Two severity columns below. **Raw** is damage if nothing is done. **Net**
is damage given the cheapest retirement actually lands. An item whose Net
is *minor* is not a risk, it is a task; it is listed because forgetting
the task reinstates the Raw column.

| # | Item | Raw | Net | Likely | Discovered | Cheapest retirement |
|---|---|---|---|---|---|---|
| 1 | v0.1 lands in a niche already held by ~500 lines of C ABI plus binding; no Rust consumer exists in 34 repos; the named first user's default artifact is silently lossy (§5 here) | project-killing | **major** | likely | integration | Name the consumer and the exact artifact, and produce that artifact this week. Half a day. Nothing engineering can do retires the rest |
| 2 | Capacity: 15-18k non-test lines against one head who also maintains 31 dependent repos at two runtimes (§3.4 here) | project-killing | **major** | certain | mid-build | A named second maintainer, or a written support tier saying the crate may lag arbitrarily and reports the engine version it implements |
| 3 | The serialized-options surface is undefined: Go's `MapToOptions` reaches 65 of 92 `Options` leaves and drops both bounds `AGENTS.md:308-313` names against hostile input (§2.2 here) | project-killing | **moderate** | certain | early spike | One ruling naming the exact leaf set S, plus an exhaustiveness test over `Options` that fails on any leaf not in S and not handled. 1-2 days, both runtimes, no Rust. Filed as #130 |
| 4 | The lexer plugin tier and the advanced features have ~zero shared-fixture coverage; where measured the runtimes disagree, and `go/doc/differences.md:94-105` asserts "No behavioural effect" for a difference that changes the accepted language (§2.1, §2.4 here) | major | **moderate** | certain | production | A matcher-tier parity harness (~200 lines per runtime) plus one correction to `differences.md`. The harness is the only thing that can see any of it |
| 5 | The options overlay merge diverges on 29 of 92 leaves in the typed tree, silently, in the accepted language; three fleet packages already carry hand-written workarounds (§2.2 here) | major | **moderate** | certain | production | Four rulings (classes A-D), then a fixture that drives the **options pipeline**, not `deep`. Then delete the three workarounds; the diff is the proof |
| 6 | `Config` lifetime and the matcher/check-hook calling convention: rustc rejects the TS capture-then-mutate shape twice, and the fix breaks every custom-matcher signature (§2.1 here) | major | **moderate** | certain | early spike | Decide the convention on day one — built-ins as an enum, hooks as `fn(&Config, &mut Lex)`. Precedes the lexer. Half a day |
| 7 | `Token` layout: a borrowed `Token<'s>` cannot live in the untyped per-parse plugin bag and the fleet stores tokens there; `'s` would infect `Ctx`, `Rule`, `Node`, the diagnostic and `parse` (§2.1 here) | major | **minor** | certain | early spike | Decide on day one: span + one `Arc<str>`, 12 bytes, `'static`. Port `ts/src/lexer.ts:99-131`, not Go's `Src string` |
| 8 | `parse(&self)` is contested in four places — recovery mints a Tin mid-parse, subscribers are `FnMut`, `ParsePrepare` hands out the live grammar, and Go keeps *lazy* scan-spec caches on `LexConfig`. The cache half is already solved and should not be reproduced: `buildScanSpecs` (`go/scan.go:278`) is called eagerly from `options.go:1175` precisely "so the shared LexConfig is read-only while parsing", the lazy getters surviving only as a fallback for hand-built configs. Measured on the configured path, TypeScript writes nothing to `Config` across seven parses covering strings, comments, escapes, unicode, maps and nesting (deep before/after snapshot, byte-identical). **A Rust port that always builds eagerly gets `&Config` for free** | major | **minor** | possible | mid-build unless probed | Four compiled probes in Wave B, each ~100 lines. Each one surfaces only when its feature is implemented, long after the signature is fixed |
| 9 | Plugin- and engine-computed byte offsets reach `&src[a..b]`: a Go no-op becomes an *engine-raised* panic, outside any `catch_unwind`, unrecoverable under `panic=abort` (§2.1 here) | major | **minor** | likely | production | A `SrcIdx` newtype plus a `clippy::string_slice` ban, day one. Unaffordable later — 26 slicing sites in `ts/src/lexer.ts` alone |
| 10 | The conformance machinery is two-runtime by construction: a Go-resident string-scan gate a comment satisfies, a binary `goOnly` registry key, pairwise "assert the opposite" divergence pins, five TSV loaders with five escape policies (§2.4, §4.1 here) | major | **moderate** | certain | production | Generalise `nonParity` and `goOnly` to N runtimes, extract one loader spec, add `assert ran == N` to every runner. Under a week, entire, and worth doing at two runtimes |
| 11 | `configure()` has at least three raw-`TypeError` paths reachable from ordinary option input, one of them on the registration shape `@tabnas/toml` uses (§2.1, §2.2 here) | moderate | **minor** | certain | integration | Three small TypeScript fixes, plus a stated Rust invariant: no option value may produce a panic; every leaf validates into a `Fault` with a code |
| 12 | The never-free arena's only bound is `rule.maxmul`; recovery adds two reachability paths not on §3.6 feasibility's list (§2.4 here) | moderate | **minor** | likely | mid-build | Measure retained bytes per source byte on the skeleton and decide once. Recovery does not multiply it (0.27-0.41 rules/byte against 0.7 clean) *(unverified)* |

Items 1 and 2 are the only two whose Net stays at *major*, and they are
the two that engineering cannot retire. That is the register's actual
shape, and it is different from the shape a severity × likelihood sort
produces.

### 1.2 What was struck, and what was demoted

**Struck.** "v0.1 as scoped cannot join the parity contract at all",
previously ranked project-killing and certain. Retired by `json-core`,
measured 55/55 and 10/10 in both runtimes (§Summary here). The residual
true statement is narrower and worth keeping: `json-core` is a *second*
grammar equivalent to the canonical one, not a serialization of it, and
that equivalence is asserted by 65 fixture rows rather than proved.

**Struck.** "`py/` is the base rate for a third runtime joining this
repo." `py/README.md:1-5` says plainly: "This is a `ctypes` binding over
`go/clib` — nothing here reimplements the engine, so what Python accepts
is exactly what every other tabnas runtime accepts." It is 206 lines over
183, it implements no accepted language, and it is outside
`go/spec_registration_test.go`'s gate because that gate scans for
*implementations*. It also landed in one sitting, so bandwidth was
demonstrably not its constraint. `py/` is evidence about **demand** — it
is unwired because nothing needs it wired — and it belongs under item 1,
not under item 2. Using it as the base rate for a lagging third runtime
was a category error, and it was the previous draft's only base rate for
its top-ranked risk.

**Demoted to scope decisions**, on measurement rather than taste:
`Continuations` (~1.1 MB/s, quadratic under editing, **zero** call sites
across all 34 fleet repos *(unverified)*) and instance `Merge`
(`ts/src/merge.ts` 614 + `go/merge.go` 847 = 1,461 implementation lines,
zero fleet callers, and a TypeScript dedupe key — `fn.toString()` at
`ts/src/merge.ts:336-346` — with no Rust spelling).

**Demoted to edits:** `Token.ignored` (declared at
`ts/src/lexer.ts:99`, read once at `:1602-1603`, assigned nowhere in the
engine, its tests, or the fleet — and described as a live TypeScript
capability at `go/doc/differences.md:90-93`); `RegisterTextParser`; the
`@tabnas/debug` matcher model; `info.marker`; the verbatim duplicate of
`str`/`snip` in `ts/src/error.ts:698-712` and `ts/src/utility.ts:837-853`;
and the three stale doc claims in §2.4 here.

**Demoted to costs with known retirements:** versioning and crates.io, CI
ownership, the matcher cursor idiom, arena generational indices, TSV
loader policy.

**Retired outright.** The boxed matcher pipeline is not the lexer's Rust
performance risk: 13 boxed matchers, 12 declining on a first-byte test,
cost 17-22 ns per source position — about 8 ns per source byte at the
measured 0.4 lex attempts per byte, under 3% of the engine's ~300 ns/byte
*(unverified)*. Do not trade the plugin tier for a static dispatch enum
that buys 8 ns/byte. CI compute is likewise not a constraint: a fully
cold `cargo build` with `serde_json` + `regex` is 8.6 s on four cores
*(unverified)*.

---

## 2. The Unexamined Surfaces

The feasibility work concentrated on `rules.ts` and `builtins.ts`. Four
surfaces had no porting analysis at all. They are where the new material
is, and three of the four contain divergences that `ci/parity` cannot
see.

The shape of the problem, measured. Function coverage of the canonical
engine, three lanes — the 254 shared parity rows driven directly; the 55
`json-core` rows; and the full TypeScript suite:

| `ts/dist/` | shared corpus | `json-core` | full suite |
|---|---|---|---|
| `builtins.js` | **0.00%** | 38.89% | 100.00% |
| `merge.js` | **0.00%** | 5.56% | 92.50% |
| `context.js` | 9.09% | 9.09% | 100.00% |
| `rules.js` | 50.53% | 44.21% | 88.60% |
| `parser.js` | 52.63% | 52.63% | 84.21% |
| `lexer.js` | 62.12% | 62.12% | 98.48% |
| `utility.js` | 67.50% | 67.90% | 88.54% |
| `error.js` | 72.73% | 68.18% | 96.43% |

Measured with `node --test --experimental-test-coverage`. Two things fall
out. First, the shared corpus never executes a single value builtin, and
never enters a `merge` function — so "passes 254 parity rows" says nothing
about the tier `json-core` exercises, which is the tier v0.1 ships.
Second, the *node* `all files` aggregate the previous draft quoted as the
Rust coverage floor (98.56% line / 93.92% function) spans `dist-test/`
and the 42 files under `test/`, which are at or near 100% by
construction because running a test file covers it. A Rust floor must be
set per-file over `dist/` — the right column above — or it is a number a
crate can neither hit nor miss.

### 2.1 The lexer and the matcher API

Largest subsystem by port ratio — 1,878 canonical TS lines against 2,987
Go (`lexer.go` 2,558 + `scan.go` 285 + `matchers.go` 144), 1.59x, the
highest in the feasibility table (§1.1) — and the one with the least
settled contract. The scan state machine and the byte tables really do
port cleanly, and `refwd()` disappears entirely because `&src[si..]` is
free. Almost nothing above that layer is agreed between the two runtimes.

Coverage: of eleven shared fixtures, exactly one touches the lexer —
`lex-string-control.tsv`, 14 rows, string control characters. Nothing
covers custom matchers, matcher ordering, relex/unrelex, plugin-raised
bad tokens, soft mode, match-token gating or `tokenSet`'s lexer effect.
`ci/parity` and `ci/fuzz` are driven through `json` and `jsonic`, neither
of which registers a custom matcher. So none of what follows is visible
to any existing mechanical check.

One useful property: `lex-string-control.tsv` needs no grammar.
`ts/test/lex.test.js:538-560` constructs `makeLex({src, cfg, opts, sub})`
and asserts on `lexer.next()` alone. A Rust lexer can join the parity
contract before a rule engine exists. That is the basis of the skeleton
recommendation in §4.3 here.

#### The plugin surface is real, and larger than the fixtures suggest

Measured across the fleet, and independently re-verified: **12 of the 34
repos register custom lex matchers** — `c`, `chess`, `css`, `csv`, `hoover`, `ini`, `jsonic`,
`markdown`, `toml`, `xml`, `yaml`, `zon`. `@tabnas/c` alone registers 13
named matchers (`c/go/matchers.go:503-516`,
`c/ts/src/matchers.ts:498-511`) and disables **eight of the nine**
built-ins to do it: `c/ts/src/c.ts:2393-2402` sets `lex: false` on
`fixed`, `space`, `line`, `text`, `number`, `string`, `comment` and
`value`, then immediately sets `match: { lex: true }`. It lexes C from
custom matchers plus the match matcher.

There is no specification of what a `LexMatcher` may do. Reading the
code, a matcher may mutate `lex.pnt.sI/rI/cI` arbitrarily — including
backwards, since nothing checks monotonicity; push extra tokens onto the
pending queue (documented only as a code comment at
`ts/src/lexer.ts:626-632`); reach the whole Context through `lex.ctx` and
mutate `ctx.meta`/`ctx.u`; read `ctx.rule` to lex context-sensitively;
mint an arbitrary error code via `lex.bad`; throw (TypeScript converts it
to a `#BD` token at `ts/src/lexer.ts:1782-1793`, Go turns it into a fatal
`internal` error at `go/parser.go:348-352`); and re-enter `lex.next`. The
fleet uses most of that: a matcher mutates per-parse mode state
(`c/ts/src/matchers.ts:192-195`), reads a symbol table the parser's
actions write (`:298`), runs a whole sub-parser that appends AST nodes
(`markdown/ts/src/engine-inline.ts:82-125`), and reads `lex.ctx.rule`
(`hoover/ts/src/hoover.ts:215`).

A Rust port must fix one signature and one capability set before writing
any matcher, and every fleet matcher is rewritten against it. Get the
capability set wrong — for instance by adopting the strategy document's
S5 capability-restricted handle and dropping `&mut Ctx` — and `c`,
`markdown`, `hoover` and `yaml` have no port at all.

The retirement is a one-page `doc/lex-matcher-contract.md` listing the
eight capabilities, what `speculate()` does and does not roll back, and
whether the cursor may move backwards. Half a day, worth doing for
TypeScript and Go regardless of Rust.

#### The universal matcher idiom does not compile

Every matcher in the fleet holds a cursor handle and then calls a `Lex`
method: `const pnt = lex.pnt` / `pnt := lex.Cursor()`, read `lex.src`,
call `lex.token(...)` or `lex.bad(...)`, advance `pnt.sI`. In Rust the
held `&mut Point` conflicts with both the shared read of `lex.src` (E0502)
and the `&mut self` of `lex.token` (E0499) *(unverified — compiled probe
`p8_cursor.rs`)*. This is the lexer-tier twin of the action-aliasing
problem in §3.1 and it was never examined.

Site counts, re-measured, because the previous draft's "51 sites
fleet-wide" does not reproduce. Go is exact: **27** non-test sites match
`:= lex.Cursor()` across the fleet, 29 broadening to any receiver.
TypeScript does not land on the quoted 22 under any pattern I could
construct: `const pnt = lex.pnt` gives 10 and `const { pnt … } = lex`
gives 8, so 18 under the register's own idiom definition; broadening to
any `const/let X = Y.pnt` gives 29. Quote Go's 27 and TypeScript as a
range, or say "dozens across both runtimes" — the engineering conclusion
is unaffected, and it is that `Lex.Cursor() *Point` has no sound Rust
analogue a matcher can hold across another `Lex` call.

The fix already exists in the fleet, written voluntarily by the largest
consumer: `c/go/matchers.go:21-31` defines `scanResult{name, consumed,
bad}` and `:471-490` wraps pure `scan*` functions. Effects as returned
data, no aliasing at all. Adopt that shape as the Rust matcher contract —
`fn(&str, usize, &mut PluginState) -> ScanResult` plus an engine-owned
wrapper — and say so in the porting guide.

#### A borrowed token cannot live in the plugin bag

`Token.src` looks borrowable: TypeScript already models it as a span
(`#ref` + `sI` + `len`, `ts/src/lexer.ts:99-131`) and Go's `Token.Src
string` is a zero-copy subslice. But `Token<'s>` cannot be stored in
`ctx.meta`/`ctx.u`, because the only untyped bag Rust offers is
`Box<dyn Any>` and `Any: 'static` — and `@tabnas/c`'s lex subscriber
stores tokens in exactly that bag (`c/ts/src/c.ts:2530`
`m.pendingTrivia.push(tkn)`; Go twin `c/go/c.go:241`, with
`PendingTrivia []any` at `c/go/symbols.go:209`) *(unverified — compiled
probe `p2_borrow.rs`)*.

So tokens must be `'static`. Measured costs, 400k tokens ≈ 1 MB source
*(unverified)*: borrowed `&'s str` 4-8 ms and 32 bytes; owned `String`
25-27 ms and 40 bytes; span-only 3.4-4.0 ms and 12 bytes. Owning costs
about 22 ns per source byte — 7% of the engine's current throughput but
15-30% of an optimistic Rust target. Go already de-borrows deliberately
(`go/lexer.go:494-497` interns string and text values "so interned values
never pin the parsed source's backing array").

Decide on day one, in writing: `Token { tin, si: u32, len: u32 }` plus one
`Arc<str>` on the `Lex`, with `src()` materialising on demand. That is
porting `ts/src/lexer.ts:99-131`, not Go's `Src string`.

#### Two incompatible matcher-pipeline models, both used downstream

TypeScript keeps one ordered list (`cfg.lex.match`) in which built-ins are
ordinary entries, sorted by `order` with a stable sort
(`ts/src/utility.ts:429`, `:441`), so a plugin can reorder a built-in by
name or replace it by re-registering under its name. Go hardcodes the
built-in sequence in `nextRaw` and interleaves customs by integer
priority (`go/lexer.go:1017-1135`, nine hardcoded interleave loops), so
built-ins can only be enabled or disabled — a `MatchSpec` with no `Make`
is silently skipped (`go/plugin.go:266-268`).

Both capabilities are used. Reordering: `expr/ts/src/expr.ts:179-181`
sets `lex: { match: { comment: { order: 1e5 } } }` with no `make`, and a
grep of `expr/go/*.go` finds no counterpart — Go cannot express it.
Reproduced here on a default instance: the pipeline goes from
`fixed@2000000, space@3000000, line@4000000, string@5000000,
comment@6000000, number@7000000, text@8000000` to
`comment@100000, fixed@2000000, …, number@7000000, text@8000000` — the
built-in moved by name, by an option, with no factory supplied.
Replacing: `toml/ts/src/toml.ts:23` registers `string:` with a
`make: '@make-toml-string-matcher'` ref, where `toml/go/toml.go:389-399`
installs `"tomlstring"` at order 900000 and leaves the engine's string
matcher installed.

Equal-order ties also resolve differently — TypeScript by declaration
order, Go by name (`go/plugin.go:285-291`) — and
`toml/go/datematcher.go:99,105` uses 950000 and 950001 rather than a tie,
which suggests the author already met this.

`AGENTS.md:26` says TypeScript wins, which means Go's `nextRaw`
hardcoding is the side that moves. That is the ruling; the tie-break is a
one-line fix plus a fixture row.

#### The dispatch table keys on the registration name, and crashes

`buildLexDispatch` branches on the string `(mat as any).matcher` and, for
the six built-in names, derives the candidate first-char set from the
engine's own config for that built-in — quote bitmap, space bitmap,
comment starts (`ts/src/utility.ts:754-799`, with the name read at
`:767`). A third-party matcher registered under one of those names
inherits a candidate set that has nothing to do with what it matches.

The previous draft called this a silent mis-dispatch. There are **two**
behaviours, and the crashing one is the path a plugin consumer takes.
Re-measured here:

| registration | result |
|---|---|
| `options({lex:{match:{string:{order:5e6, make}}}})` on a configured instance | accepted silently, matcher inherits the stale `quoteBitmap` |
| the same registration in the **constructor** | `TypeError: Cannot read properties of undefined (reading 'check')` |
| `.make()` on an instance carrying that registration | the same `TypeError` |
| a **new** name with no `make` | `TypeError: matchspec.make is not a function` |

The crash is structural: `cfg.string` is constructed only inside the
built-in string matcher's factory (`ts/src/lexer.ts:1149+`), so replacing
that factory removes its only builder and `buildLexDispatch` then reads
`cfg.string.check` on `undefined`. `toml/ts/src/toml.ts:23` is precisely
that registration, and `.make()` is the ordinary way to derive a
configured instance.

Add `opts.result.fail is not iterable` (`ts/src/utility.ts:500-507`,
re-measured here from `tn.options({result:{fail:null}})`) and
`configure()` has at least three raw-`TypeError` paths reachable from
ordinary option input — no error code, no source position, no
`TabnasError` wrapper. That is a class, not an instance, and it is a
stronger argument for the Rust invariant "no option value may produce a
panic" than the previous draft made from its single example.

Retirement: key the candidate-set derivation on the matcher's identity,
not its name — have `MakeLexMatcher` return the byte set it can start on,
Biome's `AnalyzerPlugin::query()` shape, defaulting to "all". About 30
lines in TypeScript now, and it makes the Rust `[Vec<MatcherId>; 257]`
correct by construction.

#### Match-token position gating: documented, and the documentation is wrong

TypeScript gates a `match.token` matcher on the token column at the
**current lookahead position** (`ts/src/lexer.ts:592`,
`!rule.spec.def.tcol[oc][tI].includes(...)`, where `tI` is a fourth
argument TypeScript passes to matchers and Go's 2-arity `LexMatcher` does
not have). Go gates on **slot 0** of every alternate
(`go/lexer.go:1266-1276`) with a second pass that adds only eager
fallbacks — reading `go/lexer.go:1282-1286`, a non-eager token that is not
position-expected is skipped in *both* passes.

Measured here: grammar `match.token {'#WORD': /^[a-z]+/}`, `fixed.token
{'#AT': '@'}`, one alternate `s: ['#AT','#WORD']`, input `@abc`.
TypeScript returns `"pos1:abc"`. Go's gate cannot produce `#WORD` at slot
1 at all, so the same grammar rejects.

The previous draft called this undocumented. It is documented, and that
is worse. `go/doc/differences.md:94-105` says: "Go's match matcher carries
a two-pass `positionExpected` scan that TS has no equivalent of (TS gates
by token column instead). Under a want that scan is dead … **No
behavioural effect**". The claim is sound for the want path it was written
about and reads as global. A Rust author consulting the porting guide is
told there is nothing to decide, and ships whichever side they
transcribed.

Retirement: correct `differences.md` to scope its claim to the want path,
add one `DIVERGENCE.md` entry, and land the two-row fixture (accept and
reject at position 1). About 40 lines, and it retires the `tI` argument
question at the same time.

#### A serialized `options.lex.match` is honoured in TS and dropped in Go

The serialized tier is the one v0.1 targets and the one every BNF/ABNF/
GBNF compiler emits. A spec can carry
`options: { lex: { match: { name: { order, make: '@ref' } } } }`.
TypeScript resolves the `@`-ref out of the ref bag and installs the
matcher (`ts/src/tabnas.ts:775-778`). Go resolves the ref
(`go/grammarspec.go:236-242`) and then throws the result away, because
`MapToOptions`'s `lex` branch (`go/utility.go:1050-1062`) handles only
`empty`, `emptyResult` and `relex`. No error is raised.

A Rust loader that follows Go silently loads a TOML grammar without its
string matcher and mis-parses instead of failing; a loader that follows
TypeScript makes the serialized tier code-valued, which contradicts the
premise that a serialized spec is function-free. Both are defensible;
neither is written down. `toml/go/toml.go:389-399` exists only because the
Go path does not work.

#### `lex.bad` is two different functions

TypeScript's `lex.bad(why, pstart, pend)` (`ts/src/lexer.ts:1831-1842`)
takes a span and produces a `#BD` token whose `src` and `len` describe it.
Go's exported `Lex.Bad(why)` (`go/lexer.go:685-694`) takes only the code,
produces `Src=""`, `Len=0`, and additionally sets `Err`, which TypeScript
leaves undefined. The span-taking form exists in Go but is unexported
(`go/lexer.go:1303`). Both feed the structured diagnostic — `len` is a
required field of `schema/diagnostic.schema.json` — so every
plugin-raised lexer error reports a different `src`, `len` and caret
width in the two runtimes. Roughly 34 fleet call sites across `xml`,
`zon`, `c`, `hoover`, `csv` and `toml` depend on the difference
*(unverified)*.

Retirement: export a span-taking `Bad(why, start, end)` in Go, deprecate
the one-arg form, pin one shared fixture row asserting `src`/`len`.

#### `Lex.next` filters IGNORE in Go and does not in TypeScript

Go skips IGNORE tokens inside `Next` (`go/lexer.go:955`); TypeScript
returns them and the parser skips them in `parse_alts`
(`ts/src/rules.ts:1395`). Any plugin that drives the lexer directly must
filter or must not, depending on the runtime — and `@tabnas/c` does drive
it directly, from an alternate condition, to arbitrary depth
(`c/ts/src/c.ts:2333-2336` filters; `c/go/refs_newpath.go:188-189`
comments that it need not).

That same call exposes a worse fact: an `AltCond` re-enters the lexer and
appends to `ctx.t`, which falsifies the strategy document's
classification of `AltCond` as bucket B ("pure / inspection, a shared
reference suffices") and its census of "exercised re-entrant callback
types: 1". Re-classify `AltCond` as bucket C/D — a one-line correction
that changes the Rust `AltCond` signature from `&Ctx` to
`&mut Ctx, &mut Lex`.

#### `speculate()` rolls back the lexer, not the matcher

Under negotiated lexing the engine runs a custom matcher and restores the
cursor, pending queue and cached end token if the result is not wanted
(`ts/src/lexer.ts:1644-1670`; `go/lexer.go:790-805` snapshots
`relexPoint{pnt, tokens, end}`). Neither restores anything the matcher
wrote to the Context. `@tabnas/c`'s preprocessor matchers set
`meta.mode.inDirective` as a side effect of matching, so a grammar
combining stateful matchers with `lex.relex: true` corrupts that state —
measured `fired=2` in both runtimes *(unverified)*. A Rust port
reproduces this exactly unless someone decides otherwise, and both
alternatives (fence `&mut Ctx` out of matchers, or require purity) remove
capability the fleet uses.

One sentence in the matcher contract retires it: "a matcher may be run
speculatively and rolled back; it must not mutate Context state before it
has committed to a token."

#### Plugin byte offsets reach `&src[a..b]`

`lex.bad(why, pstart, pend)` slices the source with indices a matcher
computed; so does the recovery skip loop, which walks the source from a
bad token's plugin-supplied `len`. In Go, slicing a string at any index is
legal — the repo files invalid UTF-8 under "Not divergences" for exactly
that reason. In Rust each is `&src[a..b]`, which panics off a char
boundary, and the panic is raised by the **engine**, not the plugin, so it
lands outside whatever `catch_unwind` story the port adopts and is
unrecoverable under `panic=abort`. No malicious input is needed: a matcher
that counts characters rather than bytes produces a non-boundary index on
the first non-ASCII source. Sites to audit: 26 in `ts/src/lexer.ts`, 69
`l.Src[...]` in `go/lexer.go`, `advanceLexPast` in `ts/src/rules.ts`, its
Go twin at `go/recover.go:225-231`, and six in `ts/src/error.ts` that
build the diagnostic's source extract.

Retirement: ban bare `&str` indexing with `clippy::string_slice` plus a
`SrcIdx` newtype producible only by the scanner or
`str::floor_char_boundary`, and make `bad()` take `SrcIdx`. One afternoon
at the start.

#### The error code cannot be an enum

`lex.bad` takes an arbitrary string, and downstream grammars mint at
least 20 distinct lexer error codes that appear nowhere in
`schema/error-codes.json`, which carries ten base codes plus a `goOnly`
`internal` *(unverified — fleet census)*. A Rust port modelling the
diagnostic `code` as a closed enum — the natural, serde-friendly choice,
and the one a port written from the registry reaches for — breaks every
one of them. Type it as `Cow<'static, str>` (or
`enum Code { Base(BaseCode), Custom(Box<str>) }`) from the first commit.

#### The scan driver ports from Go, not from TypeScript

Two corrections to §2 feasibility's "direct third copy" claim. Go's
driver decodes a full UTF-8 rune for any lead byte ≥ 0x80, advancing by
`size` (`go/scan.go:82-89`), while TypeScript advances one UTF-16 unit
unconditionally (`ts/src/lexer.ts:284-297`) — so a Rust port must carry
Go's `size` handling and its `Fallback`, or column counting diverges from
both. And `Fallback` is a closure over the config maps, so it is a
dynamic call per non-ASCII byte unless replaced by a `&'g Config`
parameter.

### 2.2 The options and config tree

Smaller than it looks and more divergent than anything else measured.
Re-measured on a live default instance: the TypeScript options tree is
**28 top-level groups, 51 interior objects, 136 leaves, depth 4** — 54
strings, 44 booleans, 15 numbers, 9 functions, 6 arrays, 5 undefined, 3
nulls (walker treats an array as a leaf). Go's `Options` has **92
reachable leaf fields** across 27 top-level groups *(unverified —
reflection walk from the sweep)*. The built TypeScript `Config` is a
29-group nested tree with 53 interior nodes, 241 leaves, depth 4, of
which exactly 1 is a function, 3 are RegExps and 6 are typed arrays; Go's
`LexConfig` is a flat 92-field struct. A hand-written merge over that is
~243 lines of mechanical Rust. The cost is not code, it is policy.

*(An earlier census of this same object gave 65 strings and an implied
147 option leaves. It does not reproduce; the walker above is the one the
merge-codegen estimate should rest on, and its rule is stated.)*

#### The merge diverges in `deepMergeStruct`, not in `Deep`

This is the correction from §Summary here, restated where it matters.
Options are layered: engine defaults, then plugin defaults, caller,
grammar spec, runtime `options()` calls — and `jsonic` adds a second full
defaults tree. TypeScript merges every level uniformly with `deep`
(`ts/src/utility.ts:641-673`), iterating arrays as objects. Go has *two*
merges: `Deep` on `map[string]any`, which I measured to agree with
TypeScript on all six divergent-class fields, and `deepMergeStruct`
(`go/utility.go:182-305`) on the typed `Options` tree, which does not.

Four classes, on the typed path:

| class | fields | TypeScript | Go typed | naive Rust |
|---|---|---|---|---|
| A — plain scalars | 20 (`space.chars`, `number.sep`, `string.multiChars`, `rule.include`/`exclude`, `parse.recover.enabled`, `tag`, …) | overlay's zero wins | zero discarded (`IsZero()`, `go/utility.go:249`) | `Option<T>` → TypeScript |
| B — slices | 5 (`ender`, `result.fail`, `parse.recover.syncGroups`/`syncTokens`, `match.tokenOrder`) | index-wise merge | replace | `Vec` → Go |
| C — maps of struct | 3 (`comment.def`, `value.def`, `match.value`) | recurse into the entry | replace the entry | undecided |
| D — `tokenSet` | 1 | index-wise | replace | undecided |

29 of 92 leaves, 32%, silently, in the accepted language rather than only
in values. Rust's natural types produce a *fourth* combination: TypeScript
for class A, Go for class B, and a coin-flip for C and D.

It reaches serialized specs. Measured end to end here:
`MapToOptions({"number":{"sep":""}})` produces `Sep:""`, and
`Deep(Options{Sep:"_"}, that)` returns `"_"` — so a serialized zero is
applied in TypeScript and discarded in Go. Live fleet exposure in six
lines: `jsonc/ts/src/jsonc.ts:26-29` carries `multiChars: ''` (class A),
`sep: null`, and `comment: def: hash: { lex: false }` (class C), and the
identical text is fed to Go at `jsonc/go/jsonc.go:21-28`.

The fleet already carries three hand-written workarounds, each naming the
engine merge as the cause: `jsonic/go/jsonic.go:66-109`
`normalizeCommentDefs` ("needed because the engine's option `Deep` merge
replaces map values wholesale per key"), `jsonic/go/jsonic.go:225-236`
(strips `Include`/`Exclude` before merging), and
`json5/go/json5.go:742-751` (deletes characters from the live Config
because "Jsonic's `buildConfig` restores the default multi-line quote
set … whenever `Options.String.MultiChars` is empty").

**Retirement, corrected.** One ruling per class, written into
`AGENTS.md` as "the option overlay rule", plus a fixture that drives the
**options pipeline** — `tn.options(...)` in TypeScript, `MapToOptions` +
`Deep` in Go — and asserts on the resulting Config. A `utility-deep`-style
fixture cannot see any of it, because its columns are JSON and the Go
runner would hand `Deep` maps, on which the two runtimes already agree.
Then delete the three workarounds; the diff is the proof the rule is
real.

#### Only 65 of 92 leaves are reachable from a serialized spec in Go

`MapToOptions` is 462 hand-written lines (`go/utility.go:749-1210`)
touching 22 of 27 top-level groups and 65 of 92 leaves *(the two totals
carried from the sweep; the individual misses below are re-measured here
by grep over that line range)*. Beyond the five groups filed as #130, the
per-leaf hole is worse than group counting shows: `m["rule"]` *is*
handled, yet `rule.maxmul` appears nowhere in the function. Every one of
these returns zero occurrences:

- pure data: `Rule.MaxMul`, `Rewind.History`, `Result.Fail`,
  `Match.TokenOrder`, `Parse.Budget.CheckEveryN`, and all seven
  `Parse.Recover.*` fields.
- function-valued (14, not the previously quoted 12): **eight** `*.Check`
  hooks — `go/options.go:117, 147, 154, 163, 173, 184, 211, 237`, not six
  — plus `Lex.Match`, `Parse.Prepare`, `Parse.Budget.OnCheck`,
  `Parser.Start`, `Property.ConfigModify`, `Text.Modify`.

`rule.maxmul` and `rewind.history` are precisely the two bounds
`AGENTS.md:308-313` names against hostile input, and `go/clib` — and
therefore `py/` — accepts *only* serialized specs, so every C-ABI and
Python caller is silently on defaults for both. A Rust loader written
with `#[derive(Deserialize)]` naturally applies everything, i.e. lands on
TypeScript's side, so the Go↔Rust leg lights up on ~15 pure-data fields
from the first load for a reason that is neither Rust's nor the engine's.

Compounding it, the one bound a serialized spec might try to set is
inverted. `ts/src/rules.ts:635-638`: `if (cap !== Infinity && ctx.v.length
> 2 * cap)` — 0 means retain nothing. `go/parser.go:233-236`:
`if cap > 0 && len(ctx.V) > 2*cap`, documented at `:231-232` as "a
non-positive cap means unbounded (TS Infinity)". An operator hardening a
service by setting `history: 0` gets bounded retention on TypeScript and
unbounded on Go — the opposite of the intent, on the option the security
note is about. Rust has no `Infinity`, so `Option<usize>` with `None` =
unbounded forces the question to be answered rather than defaulted.

#### `configure()` captures Config and then mutates it underneath itself

The TypeScript pipeline builds seven or eight matchers, each capturing a
live sub-object of `cfg` by reference (`ts/src/utility.ts:429` →
`ts/src/lexer.ts:198-212` `guardedMatcher(mcfg, body)`, "`mcfg` is
captured once at matcher-build time"); then runs plugin config modifiers
that mutate `cfg` (`ts/src/utility.ts:483-487`); then builds the 257-slot
dispatch table from the mutated `cfg` (`:493`).

The ordering is load-bearing, not incidental. `ini/ts/src/ini.ts:216-219`
depends on it explicitly — "the parser has no `options.comment.check`
pass-through …, so the hook is installed directly on the built config,
which `configure()` does before `buildLexDispatch()`" — and then installs
`cfg.comment.check` (`:221`), `cfg.text.check` (`:234`) and
`cfg.string.check` (`:246`), the last of which reads
`cfg.string.quoteMap[quote]` (`:252`). That is a Config field holding a
closure that borrows Config. rustc rejects the shape twice: E0502 at the
modifier loop, and a `'static` coercion failure at the matcher store
*(unverified — compiled probes `r1_naive.rs`/`r1_fixed.rs`)*.

The working shape — built-in matchers as a `Copy` enum plus
`fn(&Config, &mut Lex)` hooks — compiles and preserves all four
behaviours, but it changes the signature of every custom matcher and every
check hook. That is a breaking public-API change for the 12 fleet repos
that register matchers, and it must be decided before the `Config` struct
is written.

Note also that the matcher factories are not factories:
`ts/src/lexer.ts:1153-1201` writes about a dozen fields onto `cfg.string`
and `:645` replaces `cfg.comment` wholesale, so `MakeLexMatcher` really
wants `&mut Config` while returning something that borrows `&Config`.

Follow Go: build every derived table eagerly at configure time, take
`&'g Config`, and make `parse(&self)` with `Box<dyn Fn>` matchers a stated
invariant. Go already fixed the same problem from the other side, for a
documented data-race reason (`go/scan.go:272-277`: "called at config build
time so the shared `LexConfig` is read-only while parsing — left to the
lazy getters above, the specs are built during the FIRST parse, which
races when concurrent parses share one instance").

#### `configure()` is documented as idempotent and is not

`ts/src/utility.ts:273-277` builds `cfg.text.modify` by concatenating the
*existing* `cfg.text.modify` with the option's modifiers, so each run
appends the whole set again. Go assigns instead
(`go/options.go:830-831`). Re-measured here — and the previous draft's
"3, 4, 5" does not reproduce in either direction:

| instance | at construction | after 1 `options({})` | after 2 | after 3 |
|---|---|---|---|---|
| default (no modifier) | 0 | 0 | 0 | 0 |
| one `text.modify` installed | 1 | 2 | 3 | 4 |

So the defect fires only when a text modifier is configured, which no
fleet package does — `xml/ts/src/xml.ts:184` explicitly declines to
install one. That is why it has never been caught, and it is why a Rust
port that rebuilds Config cleanly would silently *change* TypeScript
behaviour while a transliterating port would carry the bug. Fix it in
TypeScript (one line, assign not concat), assert the length is stable
across three calls, and note in `DIVERGENCE.md` that Go was already
correct. Under an hour, and it removes a Rust decision entirely.

#### Config is an open bag in TypeScript

`config.modify` hooks write arbitrary new properties onto the built
Config. The most common is `cfg.tokenDesc`, declared by neither
`ts/src/types.ts` nor Go's `LexConfig`, written from **nine sites across
eight fleet packages** (`css`, `csv`, `ini`, `markdown` ×2, `toml`,
`xml`, `yaml`, `zon`) and read back by `railroad/ts/src/extract.ts` to
label diagram legends — re-measured here. `grep -rn tokenDesc ts/src
go/*.go` returns **zero**: the field exists in no type declaration in
either runtime. A Rust `Config` cannot grow a field at runtime, so either
the extension point is typed or Config carries an untyped side-table.
Declare
`tokenDesc` and add one typed `extra` beside it — one hour, but before the
struct is public.

#### Post-construction Config mutation, and `SetOptions` wiping it

Five fleet Go packages write lex check hooks and ender chars onto the live
Config after construction, because the serialized path cannot carry them:
`ini/go/ini.go:799,826,841,856`, `json5/go/json5.go:757-759`,
`yaml/go/yaml.go:1298,1305,1313`, `multisource/go/plugin.go:29-30`.
`SetOptions` then rebuilds Config with `buildConfig` and copies it over
the old pointer, preserving only a hard-coded list of about eleven fields
(`go/plugin.go:807-869`) — the check hooks and `EnderChars` are not on it
*(unverified — probe measured `TextCheck set=true → false`,
`EnderChars=map[60:true] → map[]`, `samePtr=true`)*. The fleet already
knows: `ini/go/ini.go:798`, "Set after `Grammar()` to ensure it's not
overwritten by `SetOptions`."

TypeScript does not have this bug, because `config.modify` hooks are
replayed inside every `configure()` run. Rust must pick one, and the two
runtimes disagree on which is correct. Adopt TypeScript's shape: Config is
rebuildable and carries an ordered `Vec<ConfigModifier>` replayed on every
rebuild, with no public field-write path. Then add the Go regression test
that fails today — it is the cheapest thing that makes the contract
visible. Secondary: TypeScript iterates modifiers in insertion order, Go
iterates a `map[string]ConfigModifier` unordered
(`go/options.go:1165-1169`), so modifier order is nondeterministic in Go.

#### The declarative condition compiler

`COND_OPS`/`COND_PATH_ROOTS` compile object conditions into closures. In
Rust this is genuinely bucket A — an operator enum plus a compiled path
enum, matched at eval time, no closures and no allocation. But it cannot
be written until four things are ruled, because the runtimes already
differ *(unverified — carried from the sweep)*:

| condition | TypeScript | Go |
|---|---|---|
| `{'need': {$gt: 0}}` | legal path root (`ts/src/rules.ts:1900-1907`) | rejected at grammar build (`go/rule.go:646-656`) |
| `{'u.never': {$ne: null}}` | `false` (`ts/src/rules.ts:2022-2024`) | `true` (`go/rule.go:565-568`) |
| `{'name': {$lt: 5}}`, `name='val'` | `false` (JS mixed-type compare) | `true` (fails open, `go/rule.go:571-580`) |
| `{'u.x': {$eq: <map>}}` | `false` | **panic**: comparing uncomparable type (`go/rule.go:521-531`) |

Fleet usage is small — 16 `$`-operator occurrences, 10 of them `$lte`,
mostly counters in `jsonic/ts/src/grammar.ts` — so the rulings are cheap
today. The single fleet use of a graph root (`prev.u.implist` at `:444`)
means the condition evaluator must take the rule arena as a parameter, so
its signature is fixed by this decision too. The Go panic is a #119 input
worth fixing regardless.

### 2.3 Utility semantics and the value model

This is the surface the previous draft got most wrong, and the correction
changes what to do.

**`deep`, `modlist` and `strinject` are engine code, not exported
extras.** `deep` runs the whole options merge and is also called on the
parse path — `ts/src/parser.ts:134` `deep(ctx, parent_ctx)` seeds a live
Context, with the comment "deep mutates the class instance in place, so
getters/setters and methods survive" — and on the token path,
`ts/src/lexer.ts:152` `this.use = deep(this.use || {}, details)` inside
`Token.bad`, which is the exact field `@tabnas/c`'s lex subscriber writes
(`c/ts/src/c.ts:2533-2537`). `modlist` is the alternate-list mod machinery
(`ts/src/rules.ts:348`, `alts = this.def[altState] = modlist(alts,
mods)`). `strinject` renders every error message and hint
(`ts/src/error.ts:289`). Only `str`/`snip` are debug-only.

That relocates the merge risk. It is not four rulings on a config tree
that the port waits on; it is a typed-API decision on Context seeding and
`Token.use` that must be taken before `Ctx` and `Token` are typed — Wave
B, alongside the `Token` layout, not Wave A behind #130.

#### The utility fixtures are the Rust merge acceptance gate

The previous draft's stop condition — "never report the 175 utility rows
as progress" — would delete the only cross-runtime pin the port has on
its own class-A and class-B merge divergences. Measured here, scoring
mutants that a Rust port actually produces against
`test/spec/utility-deep.tsv`:

| implementation | score | killed by |
|---|---|---|
| real `deep` | 52/52 | — |
| last-arg-wins (the previous draft's stub) | 39/52 | rows 19, 20, 22, 23 |
| **`null` == absent (serde `Option`)** | **44/52** | rows 3, 8, 11, 18 |
| **arrays replaced wholesale (Go class B)** | **50/52** | rows 31, 34 |
| `IsZero`-skip (Go class A) | 34/52 | rows 3, 7, 8, 10, … |

The two middle rows are exactly the divergences ranked as item 5 in §1.1
here, and `utility-deep.tsv` is the only artifact in the repo that
discriminates them. The corpus looked weak because it was tested against
mutants no Rust port would produce.

The stop condition should read: never report utility rows as *engine
reach*; do report them as *value-model* progress, and treat
`utility-deep.tsv` as the acceptance gate for the Rust merge before any
option is parsed.

**Corollary: the planned `deepStrictEqual` sweep buys nothing where it
matters.** Measured, loose and strict scores are identical for every
mutant above (52/52, 39/39, 44/44, 50/50, 34/34). And the looseness on the
runtime that matters is deliberate: `go/utility_spec_test.go:29-51`
normalises both sides through `encoding/json` "so map ordering and
numeric types match", which is the only way a Go — or Rust — leg can
compare against a TypeScript expectation at all. Keep the tightening as
hygiene; do not budget it as a risk retirement. The value-model defence is
fixture *rows* on the axes Rust actually differs: absent vs `null` vs
`@SKIP` (`ts/src/utility.ts:1230-1232` makes `@SKIP` reachable from a
serialized spec, i.e. from v0.1's surface), array merge, and key order.
About ten new rows.

#### The floor is real, and it is 63%, not 66%

An implementation that does nothing still clears most of the corpus:
`deep` = last-arg-wins 39/52, `modlist` = identity 58/78, `strinject` =
template-unchanged 8/22 *(all unverified except `deep`, re-measured here
at 39/52)*, and `str` = never-truncate **6/23** with the plainest stub or
**10/23** with a stub that still JSON-stringifies non-strings
(re-measured here, both). So the floor is 111/175 (63%) or 115/175 (66%)
depending on which stub, and the previous draft quoted the second without
saying so. Either way: "passes all shared fixtures" is not a conformance
statement about the engine.

#### The value model decisions

- **An `Undefined` variant is required.** `deep` keeps a key whose value
  is undefined; a Rust model without the variant drops the key and changes
  enumeration of the options tree. `go/rule.go:47` already carries
  `var Undefined any = &undefinedType{}`, and `ts/src/lexer.ts:1186-1191`
  carries an explicit workaround comment about it.
- **`info.text` boxes a value as `new String(v)` with a non-enumerable
  marker** (`ts/src/builtins.ts:316-318`). Rust has no hidden-property
  mechanism, so Go's `Text`/`MapRef`/`ListRef` structs (`go/text.go:6-25`)
  are the only expressible carrier — which makes `info.marker` dead config
  in Rust, exactly as it already is in Go (assigned at
  `go/options.go:1068`, never read).
- **`deep` merges an object *into a function*.** That is how `tn.options`
  is built as a value that is simultaneously callable and indexable
  (`ts/src/tabnas.ts:290-295`, `:359`; `ts/src/utility.ts:631-640`), and
  the same construction gives `tn.token`, `tn.tokenSet` and `tn.fixed`.
  Rust function values cannot carry named properties, so the public
  options accessor must split into `options()` / `set_options()` /
  `options_view()`. A rename, not a redesign, but it must precede the
  serde work because it changes what the merge target is.
- **`deep` returns its base by identity**, and the flagship downstream
  grammar branches on that: `jsonic/ts/src/grammar.ts:201` tests
  `val === prev` to detect a deep-merged duplicate key, which can never be
  true if `deep` allocates — and Go's `Deep` allocates
  (`go/utility.go:107`). `multisource/go/plugin.go:150-157` is a nine-line
  comment explaining the same workaround. Pick the `&mut`/arena form now:
  identity becomes `nid_a == nid_b`, cheap and expressible.
- **Key order is decided by a Cargo feature flag.** Choosing `IndexMap`
  does not buy insertion order if the decoder is `serde_json` without
  `preserve_order` — the order is fixed before `IndexMap` sees a key. TS
  gives integer-like-ascending-first, Go's `*OrderedMap` gives insertion,
  a naive `HashMap` gives random, and the Rust default gives
  lexicographic: a fourth answer arrived at by a dependency default
  *(unverified)*. Either enable `preserve_order` or decode straight into
  the engine's own `Value`.

#### The rest, in one list

Each is a real TS/Go difference, none is recorded in `DIVERGENCE.md`, and
none is covered by a fixture *(all unverified — carried from the sweep,
with code citations checked)*:

- `Deep` on a cyclic value is an **uncatchable process kill** in Go
  (stack overflow inside `deepClone`, `go/utility.go:392`; `recover()`
  does not catch it) where TypeScript throws a catchable `RangeError`.
  Reachable from `go/plugin.go:132,134,672,808`. A naive Rust recursive
  merge aborts the same way.
- `ModList` writes through the caller's backing array on **both** the
  delete path (an unexported `sentinel{}` escapes into user data) and the
  move path — so it is not argument-safe on either.
- Negative `move` indices: TypeScript's `(len+m)%len`
  (`ts/src/utility.ts:1076`) keeps the dividend's sign, so `move:[-5,0]`
  on a three-list silently *deletes* an element; Go's
  `((m%n)+n)%n` (`go/utility.go:685`) rotates; a literal Rust
  transliteration panics.
- `str`'s truncation unit is a genuine three-way fork, confined to
  astral text: TypeScript slices UTF-16 units, Go slices bytes and can
  emit invalid UTF-8, Rust's natural `chars()` slices scalar values.
- Number rendering diverges on four inputs (`1e21`, `1e-7`, `-0`,
  `Infinity`); a 20-line `js_number_to_string` retires it, and Go has
  three independent formatters (`go/utility.go:446, 575, 636`).
- `strinject` is polymorphic in TypeScript (`string | string[] |
  Record<string,string>`, plus an `indent` option used by `errdesc` at
  `ts/src/error.ts:289`) and string-only in Go
  (`go/utility.go:484`); its placeholder charset differs
  (`/\{([\w_0-9.]+)}/g` versus scan-to-next-`}`); it **mutates its values
  object** on a dotted miss (`ts/src/error.ts:636` via `prop`), and that
  object holds the caller's `details` by reference; and a `null` value
  throws in TypeScript, which `errdesc`'s blanket catch
  (`ts/src/error.ts:598-601`, body: `// TODO: fix`) converts into an
  empty error description.
- Go's renderers leak struct syntax into user-facing text for
  `*OrderedMap`, `MapRef`, `ListRef`, `Text` and `Undefined`, and
  `formatCompactValue` ranges a bare map, so multi-key objects render in
  randomised order.

### 2.4 Advanced engine features

About 3,499 lines of Go and 2,054 of TypeScript *(unverified — §1035-1039
feasibility)*, added over one sitting: `git log` here shows #97-#100
landing 18:14:57 to 18:17:15 on 2026-08-17 — four TypeScript features in
2m18s — their Go twins landing 19:13:22 to 20:34:47, and the sitting's
last Go commit at 21:30:32. It is the least-defended code in the repo.
None of the eleven shared fixtures exercises recovery, rewind, budget,
continuations, `ruleDone` or `info`: grepping all eleven for those words
returns zero. Every
cross-runtime claim about this layer is prose coupling between two
independently written suites (`go/recover_test.go` 412 lines / 18 funcs
against `ts/test/recover.test.js` 184 lines / 18 cases, and so on).

The `.tsv` format also cannot express what these features return —
TypeScript recovery yields `{value, errors}` and Go yields
`(value, errs, err)` — so a fixture family here needs a runner extension,
not just rows.

Structurally the news is better than expected. Recovery adds no arena
pressure (0.27-0.41 rule passes per byte with 1,000 recoveries, against
0.7 clean), synthesises no tokens, and never touches `ctx.v`; the whole
layer compiles over the settled arena design with no `unsafe`;
`Option<T>` collapses eight of Go's `xSet` companion booleans; `VecDeque`
retires an O(n) prepend at `go/parser.go:285`; `mem::take` retires the
relex slice-header dance *(all unverified)*.

Five measured input→output divergences, none recorded in
`DIVERGENCE.md` *(unverified — carried from the sweep; code paths
checked)*:

| behaviour | TypeScript | Go |
|---|---|---|
| `maxRecoveries` cap position | checked *after* the diagnostic is constructed (`ts/src/rules.ts:1096`) | checked *before* (`go/recover.go:249`) — deliberate, per its comment |
| give-up partial value | walks `ctx.rs` for the outermost partial container (`ts/src/parser.ts:378-393`) | root replacement chain (`go/parser.go:628-647`); returns `null` where TS gives `{}` |
| budget cancel under recovery | routed through the recovery contract; partial value plus a recorded cancel | `return nil, p.finishErr(...)` at `go/parser.go:477-489`; hard failure |
| negative `checkEveryN` | hook runs every iteration (`0 === kI % -1`) | disabled (`budgetN > 0`) |
| `rewind.history <= 0` | retain nothing | unbounded (re-measured here, §2.2) |

The first three are the ones a language server branches on: "parse
succeeded with diagnostics" versus "parse failed", from the same input and
the same options.

#### Recovery mints a token on the live instance mid-parse

`parse.recover.syncTokens` names are deliberately resolved per-parse
rather than at config-build time, and the resolver falls through to
`Tabnas.Token()`, which allocates a new Tin and writes three
instance-level maps when the name is unknown:
`go/recover.go:150-152` → `go/grammarspec.go:1014-1021` →
`go/plugin.go:204-223`. Measured, an unknown sync token takes `tinByName`
from 17 to 18 and `nextTin` from 18 to 19 *(unverified)*. Under the
recommended `&self` parse signature that is E0596 against `&'g Grammar`;
under a shared instance it is a data race in Go today.

The guard already exists elsewhere and was not applied here:
`go/parser.go:164-165` refuses to resolve an unknown name in `ctx.altS`
precisely because "resolving it would have to mint a token mid-parse".
Resolve sync-token names once at parse start and treat an unknown name as
a config error. About 15 lines in Go, worth landing independently of the
port.

#### `&'g Subs` cannot host the subscribers that exist

§3.4 maps subscriber lists to `&'g Subs`. That mapping is right about the
borrow and silent about `Fn` versus `FnMut`, and **every** subscriber in
the tree and the fleet is stateful: `ci/parity/tokdump.js:76-78` pushes to
`d.out`; `ci/parity/gotokdump/main.go:115-136` appends;
`go/continuations.go:246-272` captures `atEnd`/`haveEnd`;
`c/ts/src/c.ts:2522-2540` buffers trivia *and writes `tkn.use.leading`*.
So the true signature is three-way — `Fn(&mut Ctx, &mut Token, RuleId)` —
and if the list stays on the Context, where both runtimes keep it, the
dispatch is E0502 *(unverified — three compiled probes)*.

Fix the mapping in the design document now: subscribers are
`&'g [Box<dyn Fn(&mut Ctx, …)>]`, hoisted off the Context alongside the
grammar, with subscriber state in `ctx.u` or a `RefCell` the closure owns.
Note that the parity harness itself is a stateful lex subscriber, so this
is a precondition for a Rust `tokdump`, not an ergonomics nicety.

#### The per-parse state lives in ten undeclared properties

Recovery, continuations, the bad-token absorber and the `ruleDone`
payload are built on ad-hoc `(ctx as any)._*` and `(rule as any)._*`
properties that appear nowhere in `ts/src/types.ts`: `_dalt`, `_palt`,
`_eMax`, `_contTins`, `_recoverAt`, `_recoverSI`, `_badTo`, `_badErr`,
`_skipBefores`, plus `(err as any).recovered` — 22 read/write sites
*(unverified)*. A port written from the declared canonical type surface,
which is the obvious way to start, omits every one and therefore cannot
implement any of the six features. Go declared them properly
(`go/parser.go:59-95`, `go/rule.go:929-933`) and is the only readable
reference. Declaring all ten on the TypeScript `Context`/`Rule` classes is
additive, breaks nothing, and converts the port's reference from "read
2,000 lines of Go" to "read a struct". Half a day.

#### `ctx.rewind()` is token-only

Rust invites implementing rewind as an arena checkpoint plus truncate,
which is the natural idiom and is wrong. Rewind replays consumed tokens
into the lexer's pending queue and decrements the absolute counter
(`ts/src/context.ts:168-215`, `go/parser.go:252-289`); it does **not**
undo node writes, rule pushes, `rule.o`/`rule.oN` records, or anything an
action already did. `go/rewind_test.go:67-92` pins the observable: the
asserted trace is `first:abc|after-rewind-v-len:0|second:abc` — the
rewind empties the consumed history and the rule still pushes a child that
re-consumes the same three tokens.

`@probeDecide$` is the one builtin that uses it (`go/builtins.go:195-211`,
`ts/src/builtins.ts:194-208`), and it is the re-entrant bucket the
strategy document identified. It stays in v0.1 (§5 here).

#### Three stale claims in the documents a port would read

`go/doc/differences.md` and the Go doc comments are the porting guide, and
three statements about this surface are false since #106 landed recovery
in Go:

1. `go/plugin.go:65` — "`Forced bool` … always false until Go gains
   recovery (A2)", repeated at `go/doc/differences.md:706-708`, and
   contradicted by `go/recover.go:437` `RuleDone{… Forced: true}`.
2. `go/doc/differences.md:675-676` — "Go, still fail-fast, returns it
   directly" for budget cancellation. Go has recovery, and the measured
   behaviour under it differs from TypeScript.
3. `ci/parity/tokdump.js:28` — "the Go engine's public `Sub` contract
   fires after IGNORE skipping", contradicted by `go/lexer.go:834-836`
   and `:861-865`, which deliver every raw token; `gotokdump/main.go`
   filters by hand, so the harness is symmetric but its stated rationale
   is not.

Add the match-token "No behavioural effect" claim from §2.1 here and that
is four. This is the same defect class §4.1 feasibility flagged for
diagnostic `pos`: a port that passes every fixture while implementing
what the prose says instead of what the code does. Three comment edits
and one scope correction, twenty minutes, plus a `Forced == true`
assertion so the
claim cannot go stale again.

#### Two arena reachability paths not on §3.6 feasibility's list

Recovery **resumes** the failed rule — flipping OPEN→CLOSE at
`go/recover.go:405` or setting a one-shot `skipBefores` flag at `:411`,
consumed at `go/rule.go:1085-1088`, and decrementing `ctx.RSI` at
`go/recover.go:419-426` to return a popped rule — and it hands every
force-popped ancestor to the
`ruleDone` subscribers as a synthesised close (`go/recover.go:433-441`;
TypeScript twin `ts/src/rules.ts:1190-1206`). A slot-reuse scheme that
frees on pop is therefore wrong under recovery specifically, not merely
under the five paths already listed. A two-line edit to §3.6 feasibility
changes the generational-index decision from "five paths" to "seven, two
of which are error-path-only and therefore easy to forget".

#### Cross-feature interactions, untested in both runtimes

Two cells of the {recover, rewind, relex, budget} matrix are load-bearing
and neither has a test in either runtime. `relex: true` silently disables
the lexer soft mode recovery depends on (`ts/src/rules.ts:1324`
`if (BD === tkn.tin && !RELEX)`; `go/rule.go:1420-1424`), so a grammar
enabling both gets recovery *without* bad-token absorption — correct in
both runtimes today, by identical guards nobody tests. And `ctx.rewind()`
decrements `ctx.vAbs`, which is exactly the counter recovery's
cascade-suppression and strict-progress guards key on
(`go/recover.go:274, 285, 483` against `go/parser.go:283`), so a rewind
between two faults can make the second look like a cascade of the first.
Both runtimes share the arithmetic, so it is not a divergence — it is a
semantic landmine a port must reproduce bit-for-bit with no test saying
so. Four tests, two per runtime, one day.

---

## 3. Schedule Dependencies

### 3.1 Most of the "adjudication backlog" is already ruled

The previous draft treated roughly 25 outstanding TS/Go disagreements as
blocking adjudications and sequenced the port behind them. That
double-counts. `AGENTS.md:24-28` rule 1 is a standing ruling already on
the books:

> **TypeScript is canonical.** When TS and Go disagree on engine
> behavior, TS wins; change Go (and add/extend a shared fixture when the
> behavior is expressible as input → output).

So every backlog item that is a TS/Go *disagreement* has a default answer
today. Split the backlog three ways and only two of the three gate Rust:

| kind | gates Rust? | what to do | examples |
|---|---|---|---|
| TS/Go disagreement | **no** | port TypeScript, file the fixture, do not wait | #130's dropped leaves, the four merge classes, `rewind.history <= 0`, `lex.bad`'s signature, `Lex.next` filtering, the recovery trio, the equal-order tie-break |
| TypeScript is itself wrong | **yes** | genuine ruling; historically rare | #120 (the strategy document calls TS's `alt.k` read "the bug"), `configure()`'s `text.modify` concat |
| both runtimes silent | **yes** | genuine ruling; nothing to default to | parsed key order, match-token gating (both have a rule, neither is written as contract), the matcher pipeline model, `Token` `'static`-ness |

That is roughly eight genuinely blocking items, not twenty-five, and it
means Wave A's adjudication work runs *in parallel* with Wave B's type
probes rather than in front of them. The guard is not a queue-wait; it is
a measured one — track how many Rust lines a reversal would touch and
keep that number small.

### 3.2 The sequence

1. **Drain the queue and cap WIP.** Nine PRs are open on `tabnas/parser`
   — #114, #123, #124, #125, #126, #127, #128, #129, #131 — all created
   2026-08-19, all branched from base sha `9c1903d`, and #114 is the
   document that plans this port. They conflict pairwise (`go/lexer.go` in
   #125 and #128; `go/doc/differences.md` in #126, #127 and #128;
   `go/spec_test.go` in #123 and #126) and #129 edits the two files #127
   creates — a hard ordering dependency presented as a sibling branch.
   Merge serially in dependency order, rebasing each, then **tag**. Cap
   engine WIP at one open PR for the port's duration.
2. **Rule #130 first and alone** — one sentence naming the exact leaf set
   a serialized spec may set — backed by an exhaustiveness test over
   `Options` in both runtimes. Include `rule.maxmul` and
   `rewind.history` explicitly, and settle `rewind.history <= 0` in the
   same ruling. This one genuinely blocks, because "serialized-spec
   engine" does not otherwise name something a port can implement.
3. **Rule the four merge classes**, then land the fixture that drives the
   options pipeline (§2.2 here). Then delete the three fleet workarounds.
4. **Rule the both-silent set**: key order, the matcher pipeline model,
   match-token gating, `Token` representation.
5. **Land #120 and #122**, which are ruled and unimplemented — TypeScript
   still reads `alt.k` at `ts/src/builtins.ts:127, 138, 170, 238, 249,
   263, 273, 305`.
6. Everything else defaults to TypeScript and is filed as it is met.

### 3.3 Rulings reverse, so branch from tags

Issue #120 was created 2026-08-19T13:42:20Z and its last update is
15:35:35Z — a ruling and a reversal inside about 96 minutes.
Ruled-to-landed is currently 0 for 2 (#120, #122); ruled-to-reversed is
1 for 2. At hour-scale TS→Go lag that is a virtue; on a months-long
Rust branch a
reversal is a rewrite rather than a merge. **The port consumes only
rulings that have landed on `main` as code plus a shared fixture, and the
port branch rebases on tagged engine releases, never on `main`.** Costs
nothing.

The tree makes the same point: `git log` shows three commits in the last
day, the most recent being the previous draft of this document.

### 3.4 Canonical drift, and why capacity is the process risk

The two-runtime model works because the TS→Go lag is hours and paid by one
head in one sitting: measured per feature, 0h56m (unrelex) through 9h36m
(relex), median 1h42m, every one inside a single working session. 28 of 61
commits touch both trees. Commits touching engine source (`ts/src` or
`go/*.go`): 43, of which 39 are Richard Rodger's and 4 are Claude's —
three version bumps and one doc comment. **No agent has ever authored an
engine behaviour change.**

A Rust branch that runs for months cannot be in the same sitting as
anything, and the drift it must absorb is measurable: additions since root
`22fdf19` are `ts/` **+4,501** and `go/` **+7,706** (1.71x), re-measured
at `a9c8c67`. Over a 120-180 day build that is 4,000-5,800 lines of
catch-up.

Two clarifications the previous draft muddled. The "port tax" is quoted
two ways in the feasibility report and **both are legitimate measurements
of different things**: 1.42x is the aggregate non-test line ratio (§1.1),
and "~4.5 lines of port work per line of canonical source changed" is a
per-line ratio. Neither extrapolates cleanly to a greenfield engine —
they measure *incremental* porting between two existing trees. And the
engine is not unpinned today: the Go suite covers 90.6% of statements in
`github.com/tabnas/parser/go` *(unverified)*, and the TypeScript suite
puts the nine engine files with callable functions between 73% and 100%
function coverage, seven of them above 88% (§2 here). What is missing is
*cross-runtime* pinning of a specific subset, which is a narrower and
cheaper claim than "unverifiable".

### 3.5 Release, packaging and CI ownership

- **Version.** One number is shared across four locations
  (`ts/package.json`, `ts/src/tabnas.ts`, `go/tabnas.go`,
  `schema/error-codes.json`), gated by `go/version_test.go:23`, and
  computed by `/workspace/admin/publish.sh:6-9` as
  `max(npm latest, latest go/v tag, ts const, Go const)` across a serial
  32-repo wave whose **repo 1 is `parser`**. Adding a fifth location and
  an immutable sixth registry to that machinery puts `cargo publish` on
  the wave's critical path. The practice has already failed five times in 24
  releases: 23 `go/v*` tags against 19 `ts/v*`, four asymmetric Go
  releases, and a v0.8.9 that exists in all four version locations
  (commit `a3f286b`) and in no tag and no registry. npm allows a 72-hour
  unpublish; crates.io versions are permanent and a yank does not free the
  number. **Version the crate independently from 0.1.0 and have it report
  the engine version it implements** — `py/tabnas.py:138-142` is the
  working precedent and adds zero version locations.
- **The crate name.** `cargo search tabnas` returns nothing here while
  `cargo search serde` returns results, so the registry lookup works and
  the name appears unclaimed. (The crates.io HTTP API returns **403** from
  this environment, not the 404 the previous draft cited; do not repeat
  that measurement as evidence.) Names are first-come and permanent, and
  `admin/notes/2026-08-16-clib-ffi-strategy.md:186-199` plans a separate
  FFI binding crate that would claim the same name. Claim it.
- **The published crate cannot run its own parity tests.** Every parity,
  registry and version test reads paths outside the crate root
  (`../test/spec`, `../ts/package.json`, `../schema/`), and
  `cargo package --verify` compiles the lib only — a crate whose tests
  read those paths passes `cargo test` in-repo and passes the Verifying
  step, then fails every test when unpacked *(unverified)*. The same is
  already true of the published Go module. Decide the cross-directory test
  idiom before the first `cargo package`: a `repo-tests` feature, or copy
  `test/spec` and `schema/` into `OUT_DIR` from a build script.
- **CI.** `ci/README.md:1-5` says nothing under `ci/` is wired.
  `.github/workflows/ci.yml` is a 17-line caller into `tabnas/.github`'s
  `polyglot-ci.yml`, which 31 repos call and which session credentials
  cannot push (ADR-8). Build the Rust arm as `ci/rust/` plus a repo-local
  workflow file, never as a change to the shared one, so a Rust break
  cannot redden the other 30 repos. Fix the two stale paths first:
  `ci/workflows/gate.yml:71` points at `json/ts/test/spec`, which the
  fleet moved (the corrected path yields 125 inputs against
  `ci/README.md:74`'s "84/84"), and `AGENTS.md:50` still names a
  `.github/workflows/build.yml` that no longer exists.
- **ADR-12** (`/workspace/admin/DECISIONS.md:207-216`) still says
  languages beyond TypeScript and Go get tabnas through C-ABI bindings,
  "not native ports". Its status is "proposed", the only ADR in the file
  not marked accepted. Write ADR-13 superseding clause 1 in the same
  batch.

### 3.6 What only the maintainer can answer

- Which side of each measured divergence is intended, where `AGENTS.md:26`
  does not decide it — principally the recovery cap position, where
  `go/recover.go:245-248` argues Go's change was a deliberate correction
  of TypeScript.
- Whether a second maintainer, funding or dedicated time exists. Nothing
  in this repo, the 34-repo fleet or `/workspace/admin` records a
  resourcing commitment; there is no `CONTRIBUTING.md`, `SECURITY.md` or
  `FUNDING.yml`. This governs items 1 and 2 of the register.
- Whether `strinject`'s `indent` option and its array/object template
  forms are contractual or engine-private, and whether its placeholder
  *resolution set* is a contract or an accident.
- Whether `Config` is contractually mutable after construction. The Go
  fleet mutates it from five packages; the TypeScript fleet does the same
  through `config.modify`. Which is supported determines whether Rust's
  Config is rebuildable-with-replayed-modifiers or
  frozen-with-a-typed-extension-point.
- Whether instance `Merge` has an unreleased or private consumer. It has
  zero in the fleet.
- Whether `Continuations` has a committed consumer. It has zero call
  sites and there is no `lsp` repo; `ts/doc/lsp-feasibility.md` describes
  a design nothing implements.
- Whether `RelexUndo()` returning the unexported `relexPoint`
  (`go/lexer.go:781-783`) is deliberate. It makes the save/restore pair
  unusable outside the package, which suggests relex is not intended as
  public plugin surface — and decides whether the Rust port needs a public
  relex API at all.

---

## 4. The De-risking Plan

Two waves before the walking skeleton, then the skeleton, then the
harness. Wave A produces nothing in Rust; Wave B produces nothing but
probes and a written contract.

### 4.1 Wave A — contract repairs at two runtimes

Everything here is worth doing whether or not Rust happens, which is the
test a Wave-0 item should pass.

1. **Land `test/spec/json-core.fixture.json` and a third runner in both
   suites.** Done as a probe (§Summary here); about half a day to land. It
   needs about ten lines of Go: `go/spec_test.go`'s `stripRefs` (`:102`)
   and `normalizeValue` (`:135`) have no `*OrderedMap` case, so a
   builtin-built value fails comparison — 30 of 55 rows "failed" on the
   comparator alone until values were marshalled through
   `encoding/json`.
2. **Productionise the differential lane into `ci/`** (§4.4 here).
3. **Rule #130, then the four merge classes, then the both-silent set**
   (§3.2 here). Runs in parallel with Wave B.
4. **Contract hygiene:** `assert ran == N` in every runner (265 rows:
   diagnostic 10, happy 11, include-json 34, utf8 12, errors 4,
   utf8-errors 5, lex-string-control 14, deep 52, modlist 78, str 23,
   strinject 22); one TSV loader spec reconciling `test/AGENTS.md:35-36`
   ("EVERY column") with `AGENTS.md:135-136` ("the input column");
   `nonParity` and `goOnly` generalised from binary to N runtimes; the
   `#` comment convention and `diagnostic.tsv` documented in
   `test/AGENTS.md`.
5. **The four doc corrections** from §2.1 and §2.4 here, plus the `pos`
   repair §4.1 feasibility already recommends. Twenty minutes together.
6. **Wire `py/` into CI, the Makefile and the release wave**, and apply
   the "joined the parity contract" checklist to it first. If 390 lines of
   Python cannot clear that bar, 15-18k lines of Rust will not either —
   and the attempt costs under a week.

### 4.2 Wave B — the type-fixing probes

Each of these, answered wrong, is a whole-crate refactor discovered
mid-build. Most are already compiled; the work is writing them into the
porting guide, not re-deriving them.

| # | decision | cost |
|---|---|---|
| B1 | `Token { tin, si: u32, len: u32 }` plus one `Arc<str>` on the `Lex` | decision only |
| B2 | The `Lex`/`Ctx` field split and the relex save point: `Lex{cur, want, relex_undo}` + `Ctx{pending, end}` | 2 hours |
| B3 | `SrcIdx` newtype plus `clippy::string_slice` ban | half a day, day one |
| B4 | Config built eagerly; hooks as `fn(&Config, &mut Lex)`, not captures | half a day, precedes the lexer |
| B5 | Four-state overlay `Ov<T>` (Absent / Skip / Null / Set) prototyped on `string`, `number`, `parse.recover` | 1 day, gated on the merge rulings |
| B6 | Subscribers as `&'g [Box<dyn Fn(&mut Ctx, &mut Token, RuleId)>]`, hoisted off `Ctx` | 2 hours |
| B7 | `Ctx` seeding and `Token.use`: what replaces `deep(ctx, parent_ctx)` and `deep(this.use, details)` (§2.3 here) | 1 day |
| B8 | Arena retention measured on the skeleton; decide never-free versus generational indices against a number | with the skeleton |
| B9 | The custom-matcher `ScanResult` contract — **written, not built** | half a day |

`Option<T>` with `None` = unbounded for `rewind.history`, and
`Cow<'static, str>` for the diagnostic code, are decisions not probes;
record them alongside.

### 4.3 The walking skeleton: the lexer first

The previous plan made `json-core` the skeleton on the strength of its
coverage number. That is the wrong selector, because TypeScript line
coverage measures how much of the *reference* implementation executes, not
how much *Rust-specific* risk is retired. An ASCII strict-JSON document
falsifies none of B1-B4, and four of those nine Wave-B decisions — five
with B6 — are lexer decisions.

**Slice 1: the lexer.** 1,878 of 9,846 canonical TypeScript lines (19%),
the worst measured port ratio at 1.59x, the largest file in both trees,
26 source-slicing sites where the char-boundary panic lives, and it joins
the parity contract with **no grammar at all**:
`ts/test/lex.test.js:538-560` drives `makeLex` directly, so
`lex-string-control.tsv`'s 14 rows are available before a rule engine
exists. Join at the token tier with a `tokdump` spec mode.

**Slice 2: `json-core`.** 55 `include-json*` rows with full value
comparison plus 10 `diagnostic.tsv` rows, driving the scanner, the seven
built-in matchers actually used, `tokenSet`, open/close/push/replace, the
`b`-consume, seven of sixteen builtins, the ordered-map value tree, the
15-field diagnostic, the serialized-options subset, and the `@/re/`
FuncRef lowering — verified crossing to both runtimes (`{"a":001}`
rejected in both). Critically it is the **only** lane that executes the
value builtins at all: `builtins.js` goes from 0.00% function coverage
under the entire shared corpus to 38.89% under these 65 rows (§2 here).

**Slice 3:** `probe-grammar.fixture.json` (the re-entrant bucket,
`@probeDecide$` → `ctx.rewind()`) and `eager-literal.fixture.json`, both
already dual-runtime pinned at `ts/test/builtins.test.js:360, 409` and
`go/builtins_test.go:373, 545`.

**Stage on engine reach, not row count.** The 175 utility rows are 69% of
the corpus and move engine coverage by essentially nothing — measured
here, the per-file engine numbers are identical with and without them —
so a plan that lands them first maximises the green number and retires no
engine risk. Report rows as a secondary number and the per-file coverage
delta as the primary one. (This does not contradict §2.3 here: the utility
rows are the *merge* acceptance gate, which is a different claim from
being engine-reach progress.)

### 4.4 The differential harness, running from day one

Star topology — `AGENTS.md:26` makes TypeScript canonical, so Rust needs
TS↔Rust only, and `ci/parity/run-parity.sh:61-65` already writes `ts.tok`
and `go.tok` and does one `cmp`, so a third redirect is about ten lines.

One line per input, tab-separated: `OK <canonical-json> <raw-json>` or
`ERR <code> <row> <col> <pos> <token-json> <len>`. Canonical = keys
sorted, raw = as built, **so key order is a separate bucket from value** —
the one thing an `IndexMap`-based Rust engine gets wrong by default.
Compare in severity order and bucket every mismatch: accept/reject →
value → error code → row/col → pos → token src → len, then key-order
alone. Known divergences live in a data file keyed by (bucket, class:
ascii-only / lone-surrogate / non-ascii), never in prose. The recorded
`DIVERGENCE.md` entries account for the lone-surrogate and astral buckets
exactly, so **anything ASCII-only is a bug by default**.

Two things the harness must not inherit from `ci/fuzz`. It spawns one
`node` and one Go process per input, measured at 7.2 cases/s, so the case
count cannot scale; and its generator emits only well-formed documents
(`ci/fuzz/gencorpus.js:44-64`), so the error path — where every recorded
divergence lives — has never been differentially tested. In-process
dumpers plus a seeded mutation stage measured 745 inputs/s on the
TypeScript leg and roughly 5,000/s on Go *(unverified)*, and found
mismatches on the first run.

Neither dumper needs a downstream grammar port: both hard-code their
grammar import (`ci/parity/tokdump.js:44-50`,
`ci/parity/gotokdump/main.go:20-22`), and a spec mode driven from
`json-core` reaches the engine with no `json`/`jsonic` checkout. What
genuinely cannot get a Rust leg is the relaxed jsonic corpus — 1,158
inputs, the richest in the fleet — because `jsonic` has live arrow
functions and no serialized artifact. Budget that as out of scope for
v0.1 and say so in the ADR rather than discovering it at the go/no-go.

---

## 5. What v0.1 Is, and Who Uses It

### 5.1 The scope line

**In.** Loader, schema validator and the `v` gate; the lexer with its
eight built-in matchers; the rule engine; all 16 builtins; declarative
conditions; `ctx.rewind` (the BNF/ABNF/GBNF probe grammars need it
through `@probeDecide$`, and `bnf/ts/src/compiler.ts:1801` and
`bnf/go/emit_support.go:574` carry hand-written twins); the serialized
options field set ruled by #130; structured diagnostics;
`parse(&self) -> Result<Value, Fault>`; the 79 engine rows plus the 175
utility rows.

**Out, in writing.** Custom lex matchers; ref-bag closures; subscribers;
`Merge`; `Continuations`; error recovery; budget and cancellation;
`Info`/`MapRef`/`ListRef`; `RegisterTextParser`.

**The honest limit.** v0.1 serves **none** of the 12 fleet repos that
register custom lex matchers, and it does not serve `jsonic`. It does not
serve `jsonc` either, despite `jsonc` being the one fleet grammar whose
source carries no closures, because `jsonc/ts/src/jsonc.ts:63` loads its
grammar as jsonic text. State that; do not discover it.

### 5.2 The incumbent, and the margin over it

The niche is not empty. `go/clib` is 287 non-test lines and `py/` is 206
lines of `ctypes` over it; a Rust FFI binding over the same C ABI is the
same ~200 lines. What clib does **not** return is measurable —
`go/clib/core.go:133-140` emits `{"ok": true, "accept": false, "error":
{"code", "message"}}` and nothing else: no parsed value, no row, no
column, no `pos`, no structured diagnostic.

So the decision is a margin, not a vacancy. v0.1's three genuine
differentiators over an existing ~500-line incumbent are the parsed value
tree, the 15-field structured diagnostic, and no cgo / no Go runtime. That
is a roughly 40:1 line ratio. Whether those three justify it is the
question, and it is answerable this week.

### 5.3 The first user, and the defect on its path

The named first user is the BNF/ABNF/GBNF compiler family: a Rust
build-script or CLI that validates and runs a `--full --strict` compiled
grammar artifact with no Node or Go toolchain. Make that artifact a
checked-in fixture this week or the user is notional.

The emitter half already exists — `bnf/ts/src/spec.ts:415-424`'s
`compileSpec` routes through `toRecognitionSpec` or `toPureSpec`, and both
**throw** if closures remain (`:130-138`, `:158-165`) — so only the
`--strict` CLI flag is unplumbed.

But there is a live defect on the same path, and it is worse than the gap
being fixed. The ABNF CLI's **default** output is JSON:
`abnf/ts/src/bin/tabnas-abnf-cli.ts:154` prints `JSON.stringify(spec, …)`,
where `spec` is built at `:83` by `abnfConvert(src, { start, tag })` —
with **no** `builtins: true`, which only `abnfCompile` passes
(`abnf/ts/src/compile.ts:47-49`). Neither guard runs on that path, and
`JSON.stringify` drops function-valued keys silently. So the CLI's default
JSON output is a spec that loads into any engine and accepts a *different
language*, with no error and no diagnostic — landing precisely on v0.1's
only supply chain. *(Cited from the code paths; not executed, because
`abnf` has no `node_modules` in this workspace and could not be built.
Verifying it is one `npm i && npm run build` plus one `JSON.stringify`
round-trip.)*

Two fixes, not one: plumb `--strict`, and either pass `builtins: true` at
`:83` or refuse to stringify a spec containing functions. Then add a
round-trip fixture asserting the CLI JSON and the closure form accept the
same language over a shared input set.

---

## 6. Stop Conditions

Thresholds, not vibes. Each is an observation that should trigger a
rethink rather than a push.

1. **No named Rust consumer and no checked-in artifact by the end of Wave
   A.** Publish nothing. This is item 1 of the register and the only stop
   condition that can fire before a line of Rust exists.
2. **Unruled ASCII-only mismatch classes > 0 when the Rust lexer
   starts.** The recorded divergences account for the Unicode buckets
   exactly, so an ASCII-only disagreement means the two runtimes have
   agreed on nothing and Rust has no canonical answer to implement.
3. **Ruled-but-unimplemented > 0 for 30 days after the port branch
   opens** (today: 2), or any ruling reverses while live Rust code depends
   on it (#120 already reversed once, in 96 minutes).
4. **Open PR queue on `parser` > 2 when the branch opens.** Nine today,
   with five pairwise file conflicts and one hard ordering dependency.
5. **Un-allowlisted Rust-vs-TypeScript mismatch rate not below 1% of
   corpus rows within two weeks of first end-to-end parse.**
6. **Crate past 8k non-test lines before the skeleton's engine rows are
   green.** That means the arena or borrow shape is wrong; stop and
   re-probe rather than adding code around it.
7. **Throughput below Go's measured baseline** — 3.15 MB/s, 317 ns/byte
   on a 151,801-byte strict-JSON document, against TypeScript's 1.36 MB/s
   *(unverified)*. "Beat Go" is the only defensible floor: beating
   `serde_json` was never available (§7.1 feasibility puts an optimistic
   2-4x Rust factor one to two orders below it), and Go is already 2.3x
   TypeScript on identical work.
8. **Retained arena above ~200 bytes per source byte** (0.7 rule passes
   per byte × 264-byte `Rule`) → take generational indices before more
   code lands.
9. **A fleet consumer asks for a custom matcher before v0.1 ships** →
   refuse. That is the excluded imperative tier returning through the
   back door, and admitting it converts v0.1 into the full port.
10. **The Rust leg still below slice 2 (`json-core`, the rule engine and
    `diagnostic.tsv`) after a month.** Treat that as an adjudication or
    contract problem, not an engineering one, and re-read §3 here.

And one reporting rule that is not a stop condition but prevents the
others from firing late: **never report the 175 utility rows as engine
progress.** Report them as value-model progress, where they are the merge
acceptance gate (§2.3 here), and report engine progress as the per-file
function-coverage delta against the table in §2 here.
