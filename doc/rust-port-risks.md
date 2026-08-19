# Risk Register for the Approved Rust Port

Third in the series after
[`doc/rust-port-feasibility.md`](rust-port-feasibility.md) and
[`doc/rust-callback-porting-strategy.md`](rust-callback-porting-strategy.md).

Both of those recommended against a full port. **That recommendation has
been overridden by the maintainer; the port is going ahead.** This
document does not re-argue the decision and does not restate the case
against it. Its job is the opposite one: find what will block the
approved port, what will bite late, and what to do about each. Where a
finding contradicts one of the earlier documents, it says so and gives
the measurement.

Citation convention as before: a bare `§3.4` is a section of the
feasibility report, `§2.3 strategy` is the callback strategy document,
and sections of *this* document are written `§4 here`.

> **Provenance.** This register was produced by a multi-agent sweep of the
> engine, two of whose agents died mid-run — the options/config surface and
> the ranking pass. The surviving draft's headline numbers were then
> re-measured by hand and three were wrong: the `MapToOptions` counts, the
> fleet matcher count, and a "port tax" correction that conflated two
> distinct metrics. Those are fixed above and the offending claim is marked
> unverified where it could not be reproduced. Treat any figure here without
> a file:line or a measurement beside it with the same suspicion.

## Summary

**Write the serialized-spec contract down, at two runtimes, before any
Rust exists.** That is the single most important piece of advice in this
document, and it is not a process nicety — it is a measurement. v0.1 is
a serialized-spec engine, and the serialized surface is currently
undefined in a way no test can see: Go's `MapToOptions` handles **18 of
the 24** option groups `Options` declares and silently drops six —
`budget`, `parse`, `parser`, `recover`, `result` and `rewind` — three of
which are pure data with no closures to excuse them. Measured, one spec,
both runtimes (filed as
[#130](https://github.com/tabnas/parser/issues/130)):

| option in the spec | TypeScript | Go |
|---|---|---|
| `rewind.history: 7` | applied | **dropped** |
| `parse.recover.enabled: true` | applied, normalised | **dropped** |
| `result.fail: ["x"]` | applied | **dropped** |

`rewind.history` is one of the two bounds `AGENTS.md`'s untrusted-input
section names against hostile input, and `go/clib` — and therefore `py/`
— accepts *only* serialized specs, so every C-ABI and Python caller is
silently on defaults for it. A Rust
loader written with `#[derive(Deserialize)]` naturally applies
everything, which is TypeScript's answer, so the Go↔Rust leg lights up on
five option groups from the first load — for a reason that is neither
Rust's fault nor the engine's.

The three things most likely to kill this port, in order:

1. **v0.1 succeeds into an empty niche.** Below it, the C ABI over
   `go/clib` already delivers accept/reject on function-free serialized
   specs for about a week of wrapper work. Above it, the imperative
   plugin tier is out of scope, and of the 34 fleet repos **15 reference
   the custom-matcher API and at least 6 register a matcher** through a
   literal options form (`c`, `expr`, `ini`, `jsonic`, `markdown`,
   `support`) — the true figure sits between, since a matcher can also be
   wired programmatically, as `csv` does via `BuildCsvStringMatcher`. What is left as v0.1's
   differentiated capability is structured diagnostics plus tree-building
   specs — and the named first user, the BNF/ABNF/GBNF output family,
   emits a *recognition-only* grammar by default
   (`bnf/ts/src/spec.ts:418-423`, `abnf/ts/src/bin/tabnas-abnf-cli.ts:103`),
   which is accept/reject, in jsonic text a v0.1 engine cannot read
   (§6 here). This is checkable this week with three commands. It is the
   most likely technical cause of failure and it is the cheapest one to
   retire.
2. **The contract that a third runtime must implement is not written
   down at the tier the port targets.** Beyond the options surface above:
   the lexer plugin tier has one shared fixture in eleven, the advanced
   features have none, and where the two runtimes were measured they
   already disagree — sometimes with Go's divergence *pinned as contract*
   by its own tests (§3.1, §3.4 here). A Rust engine can be wrong in
   every way listed in §3 here and still pass the entire shared corpus.
3. **Availability, not throughput.** The two-runtime model works because
   the TS→Go lag is measured in hours and paid by one head in one
   sitting: six TypeScript features landed 18:12:08→18:17:15 and were
   ported to Go by 21:30:32 the same day. A Rust branch that runs for
   months cannot be in the same sitting as anything. This is not a claim
   that adjudication here is slow — measured, it is fast (median closed-PR
   lifetime 13.4 minutes across 53 closed PRs, maximum 17.0 hours, none
   ever open a full day; issue #120 went filed→ruled in 1 h 53 m). The
   risk is that one person is the only author, reviewer, merger and
   releaser, and a third runtime triples the surface only they can
   service.

One clarification and one correction. The port tax is quoted two ways in
the feasibility report and **both are legitimate measurements of
different things**: 1.76x is aggregate churn (additions since root
`22fdf19`: `ts/` +4,550, `go/` +7,971 as re-measured here, 1.75x), while
"~4.5 lines of port work per line of canonical source changed" (that
report, line 1269) is a per-line ratio, not an aggregate. Counting
line-touches rather than additions gives 4,681 and 8,443 — 1.80x. Use
whichever, but label it; treating the per-line figure as a wrong version
of the aggregate is a category error, not a correction. And the engine is
not unpinned today: the Go suite covers **90.6%** of statements in
`github.com/tabnas/parser/go` (`go test -coverpkg=github.com/tabnas/parser/go ./...`).
What is missing is *cross-runtime* pinning of a specific subset, which is
a narrower and cheaper claim than "unverifiable".

---

## 1. Blockers, Risks and Costs

Three categories, because they need different responses.

**Blockers** must be decided by a human before Rust code is written,
because they change a struct definition, a public signature, or the
meaning of "conformant". No amount of engineering retires one. There are
four.

**Risks** are things that may bite, with a probability and a discovery
point. Tooling, tests or a written contract retire them. Most of this
document is risks.

**Costs** are known work with a known shape. They belong in the estimate,
not in the register. Where the earlier documents budgeted a cost that
turns out not to exist, this document says so (§5.3 here retires one).

### 1.1 The ranked register

Severity: *project-killing* = the port does not ship or does not get
adopted; *major* = a milestone slips or a design is redone; *moderate* =
days of rework; *minor* = an edit.

| # | Item | Sev | Likely | Discovered | Cheapest retirement |
|---|---|---|---|---|---|
| 1 | The serialized-spec contract does not exist: Go's `MapToOptions` drops 5 of 27 option groups, and the proposed conformance artifact is itself an accepted-language divergence (§3.2 here) | project-killing | certain | early spike | One ruling — "the serialized options surface is exactly set S" — plus an exhaustiveness test over `Options` fields. 1-2 days |
| 2 | v0.1 lands in an empty niche between the existing C ABI and the excluded plugin tier; the named first user's default artifact is recognition-only jsonic text (§6 here) | project-killing | likely | integration | Produce the `--full --strict` JSON artifact and name its reader, in Wave 0. Half a day |
| 3 | Availability, not throughput: one collaborator with admin; the TS→Go model works only because the lag is hours (§4.4 here) | project-killing | certain | mid-build | A named second maintainer with commit access, or a written support-tier statement that Rust may lag arbitrarily |
| 4 | The lexer plugin tier and the advanced features have ~zero shared-fixture coverage, and where measured the runtimes disagree — with Go's side sometimes pinned as contract by its own tests (§3.1, §3.4 here) | major | certain | production | A matcher-tier parity harness (~200 lines per runtime) plus an audit of Go's lexer tests against TS behaviour |
| 5 | The honesty gate is blind to a third runtime, and `py/` already occupies that position (§5.1 here) | major | certain | production | Generalise `nonParity` to per-runtime and move the gate out of the Go suite. Under a week, entire |
| 6 | `Token` layout: a borrowed `Token<'s>` cannot live in the untyped per-parse plugin bag, and the fleet stores tokens there (§3.1 here) | major | certain | early spike | Decide on day one: span + one `Arc<str>`, 12 bytes, `'static`. Port `ts/src/lexer.ts:99-131`, not Go's `Src string` |
| 7 | The never-free arena's only bound is `rule.maxmul`, which permits 12 × rules × source-bytes rule passes (§3.4 here) | major | likely | mid-build | A `rule.maxrules` cap checked where `kI < maxr` already is, or generational slot reuse. Decide before `RuleId` is plugin-facing |
| 8 | Two structurally incompatible matcher-pipeline models, both used downstream (§3.1 here) | major | certain | integration | An ADR picking one. Authority rule 1 says TypeScript, which means Go's hardcoded `nextRaw` interleave is the side that moves |
| 9 | Plugin-supplied byte offsets reach `&src[a..b]`, turning a Go no-op into an engine-raised abort (§3.1 here) | major | likely | production | `clippy::string_slice` ban plus a `SrcIdx` newtype produced only by the scanner or `floor_char_boundary`. One afternoon, at the start |
| 10 | `Continuations` and stateful subscribers need shapes rustc rejects outright; `&'g Subs` works only for `Fn` and the real subscribers are `FnMut` (§3.4 here) | major | certain | early spike | Decide the subscriber calling convention before `process()` is signed; return the Context from `start_parse` rather than smuggling it out |
| 11 | Unified versioning across three registries, with `parser` first in the release wave and crates.io permanently immutable (§4.5 here) | major | likely | integration | Version the crate independently from 0.1.0 and have it report the engine version it implements, as `py/tabnas.py:138-142` already does |
| 12 | The custom-matcher contract is unwritten and the fleet's real requirements exceed both runtimes' documented surface (§3.1 here) | major | certain | early spike | `doc/lex-matcher-contract.md`, one page, from the compiled probe signature. Worth doing for TS and Go regardless — but it is an M3 artifact, not a v0.1 one |

Everything below rank 12 is a fixture row, a doc edit or a day's work,
and is described in §3 where it belongs rather than ranked here.

### 1.2 What was demoted, and why

**"Unverifiability" is not the governing risk.** The first survey for
this document ranked it first; it is now rank 5. Every component of its
remedy is known, small and already designed: generalise `nonParity`
(`go/spec_registration_test.go:27`) from `map[string]string` to
per-runtime — one line per fixture across eleven fixtures; widen the
two-directory scan at `:38-39`; add a `spec` mode to
`ci/parity/tokdump.js` and `ci/parity/gotokdump/main.go` (about fifteen
lines each); about thirty lines of corpus mutation; one
`assert_eq!(ran, N)` per runner; a `divergence.tsv` data file. A risk
that is fully understood, fully costed and under a week is a task. The
word "unverifiable" also overstates the starting position: the engine's
behaviour *is* pinned, by 499 Go test functions reaching 90.6% statement
coverage. What is not pinned is the cross-runtime subset, and the honest
statement of the risk is that a Rust tree can join the repo without
anything checking it — which is rank 5, worth a week, and worth doing in
Wave 0.

**The port tax is 1.76x aggregate / 1.80x by line-touches** (§4.4 here); the separate ~4.5 per-line figure measures something else, and conflating them deflates every
drift extrapolation by about 2.5x. Canonical drift remains the strongest
of the process risks, but it is a smaller number than the first survey
reported.

**Three of the four "blocking" adjudications already have a PR.** #123
implements the `\uXXXX`/`\x` fix with twelve new fixture rows, #124
implements #115's `pos`-as-runes change, and #127 moves #118's
regex-dialect gap into `DIVERGENCE.md`. That moves their cost out of the
adjudication budget and into the integration queue, which is a different
and cheaper problem.

**The rewind window is a positive finding, not a risk.** It ports for
free: tokens all borrow one source, so the retention shape is a plain
`Vec` with an amortised front-trim, and `mark`/`rewind` need only
`&mut Ctx` once the pending queue and end cache are hoisted off `Lex` as
§3.4 already requires. The only trap is a units one — TypeScript spells
unbounded as `Infinity` (`ts/src/utility.ts:509-511`) and Go as any
non-positive value (`go/options.go:88-93`), so a Rust `usize` where `0`
means "retain nothing" is a third answer. Use `Option<usize>`.

**`Token.ignored`, the `@tabnas/debug` matcher model, and
`RegisterTextParser` are minor.** Each is a real defect and each is an
edit: delete the dead `ignored` field and its relex branch
(`ts/src/lexer.ts:99`, `:1602-1603`) plus the row at
`go/doc/differences.md:90-93` that describes a TypeScript capability
which does not exist; make `MatchSpec` carry a registration name instead
of reflecting a function name; and do not port the package-level
`textParser` global (`go/plugin.go:934-955`) — make it an instance field,
which needs no `Send + Sync + 'static` and loses nothing, since the
driver-registration pattern buys convenience for `init()` and Rust has no
`init()`.

---

## 2. The Blockers

Four items must be ruled by a human before Rust exists. Each is a
decision, not a task; each changes a signature or the meaning of a
passing test.

**B1 — What may a serialized spec set?** §3.2 here. The measured answer
today is "22 of 27 groups in Go, all of them in TypeScript, silently".
Until this is ruled, "serialized-spec engine" does not name something a
port can implement.

**B2 — Does `Config` freeze at grammar-build time, or stay live for the
instance's life?** `go/matchers.go:15-22` records the two runtimes taking
opposite answers: TypeScript matchers close over the `Config` snapshot
handed to their factory and the factories re-run on every `configure()`;
Go's `SetOptions` does `*cfg = *newcfg` and matchers read `lex.Config`
live at match time. The feasibility report's recommended `&'g Config` is
TypeScript's model; the recommended eager derived tables
(`go/scan.go:270-277`) are Go's. They are not the same choice, and
`tn.options({...})` is public and callable at any time. This determines
whether `&'g Config` spans the instance or only a parse, which changes
every lexer and matcher signature.

**B3 — Is mid-parse grammar mutation contract or bug?** `ctx.RSM` is
handed to every `ParsePrepare` hook (`go/parser.go:431`,
`ts/src/parser.ts:153-155`) and `@tabnas/debug` — a shipped sibling this
repo dev-depends on — installs after-open/after-close state actions
through it on every rule spec (`debug/go/trace.go:164-166`, `:178-191`).
Under `&'g Grammar` that is a compile error. §3.9 dismissed the
capability on the grounds that `ctx.inst()` has zero call sites; the
route in use is not `ctx.inst()`. Same family as open item #121. If it is
contract, Rust needs a declared pre-parse install phase; if it is a bug,
fix Go first so a third runtime does not inherit it.

**B4 — Do the Go-only Info carriers bind a third runtime?** `AGENTS.md`
alignment rule 2 mandates `Info.Map`/`Info.List`/`Info.Text` and the
introspection API, but it is worded about Go. 328 `MapRef`/`ListRef`
references across the fleet make the answer expensive either way, and it
decides whether Rust's `enum Value` carries the wrappers as variants
(making the renderers exhaustive by construction — a place Rust is
strictly better than both runtimes) or omits them.

---

## 3. The Unexamined Surfaces

The feasibility work concentrated on `rules.ts` and `builtins.ts`. This
section is the material that was not covered. Every claim carries a
file:line or a measurement.

### 3.1 The lexer and the matcher API

The lexer is the largest subsystem by port ratio — 1,878 canonical TS
lines against 2,987 Go lines, 1.59x, the highest in the feasibility
table — and it has the least settled contract. The scan state machine and
the byte tables do port: `go/scan.go:70-110` is already the Rust shape,
and `refwd()` disappears entirely because `&src[si..]` is free. Almost
nothing above that layer is agreed between the two runtimes.

**Port `go/scan.go`, not `ts/src/lexer.ts`'s scan.** Two corrections to
§2's "direct third copy". Go's driver decodes a full UTF-8 rune for any
lead byte >= 0x80 and advances by `size`
(`go/scan.go:80-90`), while TypeScript's advances one UTF-16 unit
unconditionally (`ts/src/lexer.ts:283-297`); carry Go's `size` handling
or column counting silently diverges from both. And `Fallback` is a
closure over the config maps, so it is a dynamic call per non-ASCII byte
unless it becomes a `&'g Config` parameter.

**The pipeline cost is not the lexer's Rust performance risk, and
believing it is would push the design toward a static enum the plugin
tier cannot use.** Measured with a compiled probe: thirteen boxed
matchers, twelve declining on a first-byte test, cost 17-22 ns per source
position — about 1.5 ns per declining dynamic call, and about 8 ns per
source byte at the measured 0.4 lex attempts per byte. That is under 3%
of the engine's current ~300 ns/byte. Record the number in the porting
guide so nobody trades the plugin tier away for it.

#### The contract that does not exist

There is no specification of what a `LexMatcher` may do. Reading the
code, a matcher may mutate `lex.pnt.sI/rI/cI` arbitrarily including
backwards (nothing checks monotonicity); push extra tokens onto the
pending queue (documented only as a code comment at
`ts/src/lexer.ts:626-632`); reach the whole Context through `lex.ctx` and
mutate `ctx.meta`/`ctx.u`; read `ctx.rule` to lex context-sensitively;
mint an arbitrary error code via `lex.bad`; throw (TypeScript converts it
to a `#BD` token at `ts/src/lexer.ts:1782-1793`, Go turns it into a fatal
`internal` error at `go/parser.go:348-352`); and re-enter `lex.next`.

The fleet uses all of it. Twelve of the 34 repos register custom
matchers, roughly 45 implementations in total, headed by `@tabnas/c`,
which disables every built-in (`c/ts/src/c.ts:2393-2401`: fixed, space,
line, text, number, string, comment and value all `lex:false`) and lexes
C from thirteen custom matchers alone (`c/ts/src/matchers.ts:497-512`,
`c/go/matchers.go:503-518`). Those matchers mutate per-parse mode state
(`c/ts/src/matchers.ts:192-195`, `:207-215`), read a symbol table the
parser's actions write (`:298`), run a whole sub-parser that appends AST
nodes (`markdown/ts/src/engine-inline.ts:82-125`), and read `lex.ctx.rule`
(`hoover/ts/src/hoover.ts:215`). Fleet Lex-API census, verified:
`lex.src` 136, `lex.pnt` 80, `lex.token` 74, `lex.bad` 53, `lex.ctx` 27,
`lex.refwd` 12, `lex.fwd` 4, `lex.relex` 3, `lex.next` 2.

A compiled probe shows
`Box<dyn for<'s,'g> Fn(&mut Lex<'s,'g>, &mut Ctx, RuleId, Option<u32>) -> Option<Token>`
covers all of it with no `unsafe`, no `RefCell` and no `FnMut`. Adopt the
strategy document's S5 capability-restricted handle and drop `&mut Ctx`,
and `@tabnas/c`, `@tabnas/markdown`, `@tabnas/hoover` and `@tabnas/yaml`
have no port at all.

This is an M3 artifact, not a v0.1 one — v0.1 excludes custom matchers —
but write it before any Rust matcher exists, because the eight
capabilities it lists are what B2 is really about.

#### The idioms that do not compile

The universal fleet idiom is a held cursor handle: `const pnt = lex.pnt`
then a read of `lex.src` then a call to `lex.token(...)`. In Rust the
held `&mut Point` conflicts with both — a compiled probe reproducing
`c/ts/src/matchers.ts:36-63` gets `error[E0502]` and `error[E0499]`. The
count of sites holding a live cursor across another `Lex` call is **58**:
40 `:= lex.Cursor()` in fleet Go and 18 `const pnt = lex.pnt` /
`const { pnt` in fleet TS. `Lex.Cursor() *Point` has no sound Rust
analogue a matcher can hold.

The fix already exists in the fleet, written by the largest consumer,
voluntarily: `c/go/matchers.go:21-34` defines
`scanResult{name, consumed, bad}` and `:471-490` wraps pure `scan*`
functions. Effects-as-returned-data ports with no aliasing at all. Adopt
that shape as the Rust matcher contract.

**A borrowed token source is incompatible with the untyped plugin bag.**
`Token.src` looks borrowable — TypeScript already models it as a span
(`#ref` + `sI` + `len`, `ts/src/lexer.ts:99-131`) and Go's `Token.Src` is
a zero-copy subslice; 24,001 of 24,003 token `src` values on a real
workload are substrings of the input. But `Token<'s>` cannot go in
`ctx.meta`/`ctx.u`, because the only untyped bag Rust offers is
`Box<dyn Any>` and `Any: 'static` — a probe gives "`'s` must outlive
`'static`". `@tabnas/c`'s lex subscriber stores tokens in exactly that
bag (`c/ts/src/c.ts:2530`, `:2536`; Go twin `c/go/c.go:241`, `:251`, with
`PendingTrivia []any` at `c/go/symbols.go:209`). Measured cost, 400k
tokens over ~1 MB: borrowed `&'s str` 4-8 ms, owned `String` 25-27 ms,
span-only 3.4-4.0 ms; `size_of` 32 / 40 / 12 bytes. Go already
de-borrows deliberately (`go/lexer.go:494-497`, interning "so interned
values never pin the parsed source's backing array").

**Decide on day one:** `Token { tin, si: u32, len: u32 }` plus one
`Arc<str>` on the `Lex`, with `src()` materialising on demand. That keeps
tokens `'static`, keeps the plugin bag untyped, and costs 12 bytes per
token instead of 40. Choosing wrong is a whole-crate refactor, because
`'s` would infect `Ctx`, `Rule`, `Node`, the diagnostic and the public
`parse` signature.

#### Six divergences and two live defects, none of them covered

Of eleven shared fixtures only `lex-string-control.tsv` (14 data rows)
touches the lexer, and it tests string control characters. `ci/parity`
and `ci/fuzz` run through `json` and `jsonic`, neither of which registers
a custom matcher. So `ci/parity` cannot see any of the following.

**Two incompatible pipeline models.** TypeScript keeps one ordered list
(`ts/src/utility.ts:422-441`) in which built-ins are ordinary entries, so
a plugin can reorder a built-in by name or replace it by re-registering
under its name. Go hardcodes the sequence in `nextRaw` and interleaves
customs by integer priority (`go/lexer.go:1017-1135`, nine hardcoded
loops), and `go/plugin.go:266-268` silently skips a `MatchSpec` with no
`Make` — so built-ins can only be enabled or disabled.
`expr/ts/src/expr.ts:179-181` reorders `comment` with no `make`, which Go
cannot express; `toml/ts/src/toml.ts:23` replaces the string matcher by
re-registering under `string`, while `toml/go/toml.go:389-399` installs a
separate `"tomlstring"` at order 900000 and leaves the engine's string
matcher installed. Equal-order ties also resolve differently: TypeScript
yields declaration order (stable sort, `ts/src/utility.ts:440`), Go sorts
by name (`go/plugin.go:285-291`) — `toml/go/datematcher.go:99,105` uses
950000 and 950001 rather than a tie, which suggests the author met this.

**The 257-slot dispatch table keys a matcher's candidate byte set on its
registration NAME.** `ts/src/utility.ts:766-798` branches on the string
`(mat as any).matcher` and, for the six built-in names, derives the
candidate first-char set from the engine's own config. A third-party
matcher registered under one of those names inherits a candidate set that
has nothing to do with what it matches. Measured: the identical
`|`-delimited string matcher registered as `mystr` lexes `|hi there|` to
one `#ST`; registered as `string` it is absent from dispatch slot 124 and
the same input lexes to `#TX #SP #TX`. This is a live TypeScript defect
and `toml/ts/src/toml.ts:23` sits on it, surviving only because `'` and
`"` are in the default quote set. In the same function, a built-in
carrying a `check` hook is promoted to every slot, so assigning a check
hook silently disables the filter — and `ini/go/ini.go:799,826,841,856`
assigns four check hooks directly onto the built config, which a
`&'g Config` design cannot accept at all. Also measured: for
`@tabnas/c` the table filters only 72 of 3,670 slot entries (2.0%),
because every custom matcher is listed everywhere. Fix by having
`MakeLexMatcher` return the byte set it can start on — Biome's
`AnalyzerPlugin::query()` shape — which makes the Rust
`[Vec<MatcherId>; 257]` correct by construction.

**Match-token position gating changes the accepted language, and
`go/doc/differences.md` says it does not.** TypeScript gates a
`match.token` matcher on the token column at the current lookahead
position (`ts/src/lexer.ts:588-593`, using the fourth argument `tI` that
Go's `LexMatcher` does not have); Go gates on slot 0 of every alternate
with a second eager-only pass (`go/lexer.go:1257-1268`). Measured on the
same grammar and input in both runtimes — `match.token {'#WORD': /^[a-z]+/}`,
`fixed.token {'#AT': '@'}`, one alternate `s: ['#AT','#WORD']`, input
`@abc` — TypeScript gives `#AT #WORD #ZZ` and the result `"pos1:abc"`;
Go gives `#AT #TX` and `[tabnas/unexpected]: unexpected character(s): @`.
This *is* documented, at `go/doc/differences.md:94-105`, which names the
mechanism and then asserts "No behavioural effect, and it matters" before
explaining the 25-98x performance win that motivated it. The performance
argument is sound; the no-effect claim is false. That is a cheaper and
sharper finding than an undocumented divergence: the fix is a two-line
doc correction plus a two-row fixture, and it settles the `tI` argument
question at the same time. As written, a Rust author reading the
reference document is told there is nothing to decide.

**A serialized grammar's `options.lex.match` is honoured in TypeScript
and silently dropped in Go.** TypeScript resolves the `@`-ref out of the
ref bag and installs the matcher (`ts/src/tabnas.ts:775-778`); Go
resolves the ref (`go/grammarspec.go:236-242`) and then loses it, because
`MapToOptions`'s `lex` branch handles only `empty`, `emptyResult` and
`relex`. No error is raised. Measured with identical bytes: TypeScript's
pipeline becomes `boom@100000, fixed@2000000, …`; Go's
`Config().CustomMatchers` has length 0. This is one instance of the much
larger problem in §3.2 here.

**`lex.bad` is a different function in the two runtimes, and 87 fleet
call sites depend on the difference.** TypeScript's
`bad(why, pstart, pend)` takes a span and produces a `#BD` token whose
`src` and `len` describe it (`ts/src/lexer.ts:1831-1842`, leaving `err`
undefined); Go's exported `Lex.Bad(why)` takes only the code, produces
`Src=""`, `Len=0`, and additionally sets `Err`
(`go/lexer.go:685-694`) — the span-taking form is unexported at
`go/lexer.go:1303`. Both feed the structured diagnostic
(`go/lexer.go:1141`; `ts/src/rules.ts:1385-1391`), and `len` is a
required field of `schema/diagnostic.schema.json`, so every plugin-raised
lexer error reports a different `src`, `len` and caret width. Call sites,
counted across the fleet clone: **53** `lex.bad(` in TypeScript across 8
repos and **34** `lex.Bad(` in Go across 6 repos: **87** sites, every one
of which changes the diagnostic it emits when the signature is unified.

**`Lex.next` filters IGNORE tokens in Go and does not in TypeScript.**
Go skips at `go/lexer.go:953-957` (`l.ignoreDense[tin] { continue }`);
TypeScript's `Lex.next` has no IGNORE branch and the skip lives in the
parser at `ts/src/rules.ts:1396`. The fleet codes around it in opposite
directions: `c/ts/src/c.ts:2333-2336` loops
`do { tkn = lex.next(...) } while (tkn && IGNORE[tkn.tin])`, while
`c/go/refs_newpath.go:188-189` comments that Go's `Next()` already skips
them. The same call exposes something worse: an `AltCond` re-enters the
lexer and appends to `ctx.t`
(`c/go/refs_newpath_handlers.go:28` → `c/go/refs_newpath.go:190-210`;
TypeScript twin `c/ts/src/c.ts:2312-2342`, wired to four alternates at
`c/ts/c-grammar.jsonic:124-133`). That falsifies the strategy document's
classification of `AltCond` as bucket B ("pure / inspection, a shared
reference suffices") and its census of "exercised re-entrant callback
types: 1". **Re-classify `AltCond` as bucket C/D**: its Rust signature
changes from `&Ctx` to `&mut Ctx, &mut Lex`.

**The lexer is the main minting site for error codes outside the
registry.** `lex.bad` takes an arbitrary string, and fleet grammars mint
24 distinct codes, 19 of them absent from `schema/error-codes.json`'s ten
base codes: `xml_invalid_tag` (8), `pi_target_invalid` (4),
`zon_number` (3), `invalid_xml_char` (3), `invalid_text` (3), and a tail
of twos and ones. A Rust `Code` therefore cannot be a closed enum — the
natural, serde-friendly, registry-derived choice. Type it as
`Cow<'static, str>` or `enum Code { Base(BaseCode), Custom(Box<str>) }`
from the first commit. This is #116 landing on the lexer specifically.

**Two live defects.** The dispatch-table name-keying above is one. The
other: `json5/ts/src/json5.ts:472-476` deletes
the backtick entry from `cfg.string.quoteMap` after configure, but the
string matcher's Latin-1 fast path reads `cfg.string.quoteBitmap`
(`ts/src/lexer.ts:1228`), which the delete does not touch — measured, the
backtick still opens a string and `quoteBitmap[0x60] === 1`. It is the
only downstream user of post-configure config mutation, and fixing it
(call `options({string:{chars:...}})`, about ten lines) removes the last
obstacle to B2 resolving as "freeze at build time".

**One shared unspecified behaviour.** `speculate()` restores the cursor,
the pending queue and the cached end token, in both runtimes, and nothing
on the Context (`ts/src/lexer.ts:1644-1670`; `go/lexer.go:790-802`). So a
matcher's side effects survive a rolled-back speculation.
`@tabnas/c`'s preprocessor matchers set `meta.mode.inDirective` as a side
effect of matching (`c/ts/src/matchers.ts:192-195`, `:207-215`, `:233`,
`:246`), so any grammar combining stateful custom matchers with
`lex.relex: true` corrupts that state. Untested in either runtime. A Rust
port reproduces it exactly unless someone decides otherwise, and both
alternatives — fence `&mut Ctx` out of matchers, or require purity —
remove capability the fleet uses. One sentence in the matcher contract
settles it; if the contract instead promises rollback, the cheapest
implementation is the `ScanResult` shape `c/go/matchers.go` already uses.
A smaller instance of the same family: `commentSuffixFnMatch`
(`ts/src/lexer.ts:1042-1057`, `go/lexer.go:1596-1609`) restores the
cursor and not the pending queue, so a suffix matcher that queues tokens
leaks them.

**Plugin-supplied byte offsets reach `&src[..]`.** `lex.bad(why, pstart,
pend)` slices the source with indices a matcher computed, and so does the
recovery skip loop. In Go, slicing a string at any index is legal — the
repo files invalid UTF-8 under "Not divergences" for exactly that reason.
In Rust each is `&src[a..b]`, which panics off a char boundary, and the
panic is raised by the *engine*, so it lands outside whatever
`catch_unwind` story the port adopts and is unrecoverable under
`panic=abort`. A probe: `&"a é b"[2..3]` panics, `src.get(2..3)` is
`None`. No malicious input is needed — a matcher that counts characters
rather than bytes produces a non-boundary index on the first non-ASCII
source. Sites to audit: 26 in `ts/src/lexer.ts`, 69 `l.Src[...]` in
`go/lexer.go`, `advanceLexPast` in `ts/src/rules.ts` and its Go twin at
`go/recover.go:225-231`, and six in `ts/src/error.ts` that build the
diagnostic's source extract. §4.5 flags the API-shape half of this; the
plugin-index half is new.

**relex/unrelex is cheap, but its save point straddles Lex and Ctx.**
§3.4 requires the pending queue and cached end token to move onto the
Context, because `ctx.rewind()` writes both. `relex` saves and restores
exactly those two fields together with the cursor, so `relexPoint`
becomes a cross-object snapshot needing both `&mut Lex` and `&mut Ctx`;
and the call site passes a token living in `ctx.t[i]` plus a `&mut Ctx`
in the same call, which is `error[E0502]`. A probe shows the whole cycle
working once the four cursor scalars are copied out first and the queue
is a moved `Vec`: `size_of(RelexPoint) = 64` bytes, no clone, no
allocation on the restore path — structurally better than Go, whose
`relexPoint.tokens` aliases the live slice and is sound only because
`Relex` nils it immediately (`go/lexer.go:696-700`, `:745-746`). Land
that probe as the first Rust file written; it fixes the Lex/Ctx field
split for the whole port. Note also that `RelexUndo()` returns the
unexported type `relexPoint` (`go/lexer.go:781-783`), so Go's save/restore
pair is unusable from outside the package while `Relex` and `Unrelex` are
both exported — which suggests relex is not intended as public plugin
surface, and that decision determines whether Rust needs a public relex
API at all.

**The unrelex re-announce is a behaviour decision hidden in a borrow
error.** Both runtimes put a restored token back into the lookahead
buffer and re-announce it to lex subscribers
(`ts/src/rules.ts:1494-1508`, `go/rule.go:1553-1571`). In Rust
`sub(&mut ctx.t[un_i], rid, ctx)` is `error[E0499]`. The two fixes are
observably different when a subscriber mutates the token, and
`@tabnas/c`'s does (`c/ts/src/c.ts:2522-2540`): announce-then-store
leaves `use = Some("leading:0")` in the buffer; store-then-announce-a-clone
leaves `use = None`. Pick announce-before-storing — it preserves today's
semantics, where subscriber and buffer share one object — and pin it.

### 3.2 The options and config tree

This is the surface that was least examined and is now rank 1.

**Go's `MapToOptions` handles 18 of the 24 groups `Options` declares** (verified; filed as [#130](https://github.com/tabnas/parser/issues/130)).
Measured by diffing every `opts.<Field>` assignment inside
`go/utility.go:749-1237` against the field list at `go/options.go:16-44`.
Silently dropped: **`Parse`, `Parser`, `Property`, `Result`, `Rewind`**.
No error, no warning. Direct probe:

```
MapToOptions({"parse":{"recover":{"enabled":true}},
              "rewind":{"history":5},
              "result":{"fail":[null]}})
  => Parse=<nil> Rewind=<nil> Result=<nil> Parser=<nil> Property=<nil>
```

TypeScript applies all of them, because `ts/src/tabnas.ts:775-776` runs
`this.options(resolveFuncRefs(gs.options, ref))` — the generic deep-merge
setter over the whole tree. So from identical spec bytes, TypeScript
returns `{value, errors}` where Go throws; TypeScript honours
`result.fail` where Go ignores it; and `rewind.history`, one of the two
standing DoS bounds `AGENTS.md:308-313` names, is unsettable from a
serialized grammar in Go.

Two of these matter more than `lex.match`. `result.fail` is what the
strict-JSON grammar uses to reject `undefined`/`NaN` results.
`rewind.history` is a security bound. And the direction is hostile to the
port: Rust with `#[derive(Deserialize)]` naturally applies everything,
i.e. lands on TypeScript's answer, so the Go↔Rust leg diverges on five
groups from the first load — and the proposed `divergence.tsv`
classifier, seeded from `DIVERGENCE.md`, `go/doc/differences.md` and the
mutation classes, contains no options-plumbing entries, so it would fail
the build for a reason that is neither Rust nor the engine.

**The proposed conformance artifact is itself an accepted-language
divergence.** `ts/test/json-builder.fixture.json` carries
`options.tokenSet: {KEY:["#ST"], VAL:["#ST","#NR","#VL"]}`. TypeScript's
`deep` merges arrays element-by-element (`for (let k in over)` over an
array, `ts/src/utility.ts:641-673`), so a shorter user array leaves the
base's tail intact; Go's `applyTokenSets` (`go/plugin.go:381-401`) calls
`SetTokenSet(name, tins)`, which replaces. Measured from the same bytes:

| | KEY tins | `{1:2}` | `diagnostic.tsv` |
|---|---|---|---|
| default (no spec) | `[10,8,9,11]` | — | — |
| TypeScript + spec | `[9,8,9,11]` | `{"1":2}` | 4/10 † |
| Go + spec | `[9]` | `[tabnas/unexpected]` | 6/10 † |

† **Treat these two rows as unverified.** `test/spec/diagnostic.tsv` is
pinned against the strict-JSON *test grammar*
(`ts/test/json-plugin.ts` / `go/jsonplugin_test.go`), not against
`json-builder.fixture.json`. Scoring the serialized spec against it is a
synthetic pairing in which rows can fail because the grammar differs
rather than because the runtimes disagree, so the split may be an
artifact of the pairing. The claim needs a harness that controls for the
grammar before it can carry any weight.

The two rows that separate 4 from 6 are exactly the rows asserting
`expected: ["#ST"]` at a map key position. The remaining four failures
are shared (both runtimes lack `#TX` in `VAL`, because the spec does not
disable the text matcher). So the artifact both earlier documents lean on
as the runtime-neutral portable target is neither runtime-neutral nor
passing, and the 13-line options patch that takes it to 10/10 in both
(`text.lex:false`, `number.exclude`, and `tokenSet.KEY:["#ST",null,null,null]`)
works by making the array length 4 — it masks the merge divergence rather
than fixing it.

Audit the other array-valued options in the same change: `ender`,
`result.fail`, `recover.syncTokens`, `recover.syncGroups`.

**`deepMergeStruct` has no Rust spelling, and `Option<T>` changes the
rule rather than expressing it.** `go/utility.go:182-305` is 124 lines of
reflection implementing "a zero overlay field preserves base", per-Kind:
`of.IsZero()` keeps base, pointer-to-struct recurses, pointer-to-primitive
over-wins, `Map` merges entries base-then-over, everything else over-wins,
and a struct with no exported fields replaces outright (the RegExp fix at
`go/doc/differences.md:290-303`). Rust has no runtime reflection: this
becomes a derive macro or hand-written merges across 29 structs and 131
field declarations. `Option<T>` is not a drop-in — it changes what counts
as absent, so `false`, `0` and `""` stop preserving the base the way they
do in Go today, which is why Go already spells options `*bool`/`*int`
(`go/options.go:55`, `:82-84`, `:93`). Prototype the macro on the three
deepest groups before committing to the options tree design; this
interacts directly with §2's note that `#[serde(default)]` is the wrong
tool because it destroys presence information.

### 3.3 Utility semantics and the value model

This surface is not the low-risk "exported extras" the feasibility report
treats it as. Three of the four fixtured functions are engine-internal
and mandatory: `deep` runs the whole options merge
(`ts/src/tabnas.ts:257,295,350,359,381,399`), the error-details clone
(`ts/src/error.ts:75`), the token `use` merge (`ts/src/lexer.ts:152`) and
lexer config (`:1169`); `modlist` runs the alternate-list mods
(`ts/src/rules.ts:348`); `strinject` renders **every** error message and
hint (`ts/src/error.ts:289`, `:491`). Only `str`/`snip` are debug-only
inside the engine, and `debug/go/trace.go:440` calls `tabnas.Str` anyway.
So "a spec-only engine may not retain the utility surface" is false for
at least 152 of the 175 rows.

**The 175-row corpus does not discriminate a correct port from a wrong
one.** Two Rust implementations over `enum Value` + `IndexMap` were built
and run against all four fixtures. The correct one passes 175/175. A
deliberately wrong one — `format!("{}", n)` for numbers, `r"\{([\w.]+)\}"`
for the placeholder (Rust's `regex` `\w` is Unicode), and TypeScript's
sign-of-dividend modulo for `move` — **also passes 175/175**. Coverage
census: `utility-modlist.tsv` is 72 delete rows, 2 move rows and 4 no-op
rows with zero `custom`/`append`/`clear`; `utility-strinject.tsv` uses
seven distinct placeholder characters, all ASCII; `utility-str.tsv` is 23
ASCII rows with zero `\r\n\t` rows, so `snip`'s entire reason for existing
is untested; and `utility-deep.tsv` cannot see key order at all, because
both runners compare order-insensitively
(`go/utility_spec_test.go:33-50`, `ts/test/utility.test.js:206`) and 38
of its 39 multi-key objects are already alphabetical with zero
integer-like keys.

The divergences the corpus hides, all measured across three runtimes:

| behaviour | TypeScript | Go | Rust default |
|---|---|---|---|
| `str(1e21)` | `1e+21` | `1000000000000000000000` | same as Go |
| `str(Infinity)` | `Infinity` | `+Inf` | `inf` |
| `str(-0)` | `0` | `0` | `-0` |
| `str(1e-7)` | `1e-7` | `0.0000001` | same as Go |
| `deep` base | mutated, returned by identity | fresh container | choice |
| merged key order | integer-like first | insertion | sorted (`serde_json`) |
| `strinject` `{é}` | literal | substitutes | substitutes (`regex` `\w`) |
| `modlist move:[-5,0]` on 3 | `["a","c"]` — an element deleted | `["b","a","c"]` | forced choice |
| `str("ααααα", 8)` | untouched | 8 bytes, truncated `α` | panics |

Ship a single `fn js_number_to_string(f64) -> String` in the porting
guide — about twenty lines, and every renderer must route through it.
Write `[0-9A-Za-z_.]` explicitly next to §4.3's `\s`/`\d`/`\w` lowering
note; this is the same trap in a second place and §4.3 does not mention
it. And note that TypeScript's `move` path has no bounds check where the
delete path does (`ts/src/utility.ts:1066` versus `:1075-1076`), so
`move:[-5,0]` silently loses a list element — a TypeScript bug, not just
an unpinned choice.

**`deep`'s signature is a real fork, and downstream has already paid for
both sides.** TypeScript mutates its base and returns it by identity; Go
allocates (`go/utility.go:105`). `multisource/go/plugin.go:150-157`
carries a nine-line comment about exactly this, against a one-line
TypeScript counterpart at `multisource/ts/src/multisource.ts:248`; and
`jsonic/ts/src/grammar.ts:216` tests `val === prev` to detect that
`deep(prev,val)` returned the base allocation. Under the arena design the
identity observable becomes `nid_a == nid_b`, which is expressible and
cheap — so pick the `&mut`/arena form and record it.

**A recursive `Value` enum aborts on Drop.** Measured with rustc 1.94.1,
release, `panic=unwind`, 8 MB stack: a `Value` nested 51,562 deep drops
cleanly; 52,500 aborts with `fatal runtime error: stack overflow`,
SIGABRT, uncatchable by `catch_unwind`, with no user frame on the stack.
Construction and traversal survive far past that — it is `Drop` alone.
TypeScript overflows at depth 1538 with a *catchable* `RangeError`; Go
handles 200,000 fine. The exact threshold is layout- and
stack-dependent, so state the mitigation and not the number: extend the
arena decision from rules and nodes to the value model (a flat `Vec<Node>`
with `NodeId(u32)` children has no recursive Drop), and if a standalone
`Value` is exported for the utility API, give it an iterative `Drop`.

**Three Go-side defects to fix before a third runtime copies them.**
`Str`/`Deep` on a cyclic value kill the process — `fmt.Sprintf("%v", …)`
recurses forever (`go/utility.go:461-467`), `recover()` does not catch a
stack overflow, and this is on the *error* path. `formatCompactValue`
ranges a bare map (`go/utility.go:598-608`), so a multi-key object
renders in randomised order — measured, 165 of 200 calls one way and 35
the other in a single process, which makes any fixture row added for it a
flake generator. And the three renderers have no cases for `*OrderedMap`,
`MapRef`, `ListRef`, `Text` or `Undefined`, so they leak Go struct syntax
into user-facing error text: `StrInject("{a}", {a: Text{...}})` returns
`"{\" hi}"`. Rust's exhaustive `match` makes that omission a compile
error — but only if B4 puts the wrappers in the enum.

**The Go utility fixture runner unescapes nothing**, while
`ts/test/utility.js:29-32` maps `unescape` over every column. The
invariant holds today only because all four utility fixtures contain zero
escapes, and `snip`'s behaviour is precisely `\r\n\t` replacement — so
its rows cannot be written until this is fixed. Worse, the two written
specifications contradict each other: `test/AGENTS.md:36-37` says the
repo decodes escapes "in EVERY column"; `AGENTS.md:135-136` says "in the
input column". Reconcile those two files before extracting a loader spec.

### 3.4 Advanced engine features

Recovery, Continuations, the rewind window, budget/cancellation,
subscribers, the Go-only Info carriers, `RegisterTextParser`.

**The algorithms port.** A compiled model of the whole recovery path —
arena rules never freed, `&'g Grammar` and `&'g Tabnas` reached through
the Context, tokens borrowing the source, ring-buffered `v`, forced-close
dispatch, mark/rewind with eviction errors — runs with no `unsafe`, no
`Rc` and no `RefCell`. Three questions have clean answers: recovery
**never rewinds** (`go/recover.go:238-428` reads `ctx.V`/`VAbs` only for
the progress guard and never writes them), **never synthesises tokens**
(it reuses lexer tokens and the `NoToken` sentinel; `forceClose`
synthesises an *event*, `go/recover.go:433-441`), and **does re-enter
rules** — it flips `rule.State` and sets `rule.skipBefores` on a rule
already on the stack (`:402-413`) and dispatches RuleDone for rules
*after* popping them (`:419-426`), which is a second independent reason
generational indices are mandatory if arena slots are ever reused.
Budget/cancellation is trivial and does **not** force `Send + Sync`: a
probe cancels a `parse(&self)` on an `Rc`-containing, `!Send`/`!Sync`
instance from another thread via a captured `Arc<AtomicBool>`.

**The plumbing does not port.** `Continuations` requires the Context to
outlive the parse, and both runtimes reach it by a route rustc rejects
outright. Go smuggles it out through a `RuleSub` closure
(`go/continuations.go:275-278`, read at `:310-315` after `startParse`
returned at `:284`) — `error[E0521]: borrowed data escapes outside of
closure`. TypeScript hangs it off the thrown error
(`ts/src/error.ts:66`, `:79`, read at `ts/src/tabnas.ts:615`) —
`error[E0515]: cannot return value referencing local variable`. The fix
compiles: `start_parse` returns `Outcome { value, ctx, err }` and
`continuation_tins` runs on the returned Context. §3.6's "errors are
snapshots, no back-pointer" is the correct call for Go and structurally
removes what TypeScript's continuations reads, so record that
`err.internal.ctx` has no Rust spelling.

**Stateful subscribers cannot ride on the Context.** §3.4's table moves
subscriber lists to `&'g Subs`, which is sufficient only for `Fn`. Every
subscriber that matters here mutates captured state: `capture`
accumulates `atEnd`/`haveEnd` (`go/continuations.go:246-272`), `watch`
writes `failCtx` (`:275-278`), TypeScript's permanently-installed lex
subscriber writes `this.#contAtEnd` (`ts/src/tabnas.ts:532-566`),
`@tabnas/debug`'s tracer holds `*traceState` (`debug/go/trace.go:129`),
and `@tabnas/c`'s buffers trivia into `ctx.Meta` *and writes the token*
(`c/go/c.go:231-253`). A probe: `&'g [Box<dyn Fn(&mut Ctx)>]` dispatched
from `ctx` compiles; `&'g mut [Box<dyn FnMut(&mut Ctx)>]` is
`error[E0499]`. So the sub list becomes a sibling `&mut` parameter
threaded through every dispatch site — five in Go
(`go/parser.go:492`, `:510`; `go/recover.go:438`; `go/rule.go:1568`;
`go/lexer.go:862`) and seven in TypeScript — which drags it into
`Lex::next`, `Lex::relex`, `force_close` and `absorb_bad`. Neither the
`k` ruling nor the argument-3 collapse touches this; it is orthogonal and
currently unaddressed. **Decide the calling convention before `process()`
is signed:**
`fn process(g: &'g Grammar, subs: &mut Subs, ctx: &mut Ctx, lex: &mut Lex, rid: RuleId)`.

**The arena's only remaining bound is four orders of magnitude too
loose.** With rules never freed, retained memory is O(rule passes), and
the only thing bounding rule passes is
`maxr = 2 * |rsm| * len(src) * 2 * maxmul` (`ts/src/parser.ts:211`,
`go/parser.go:442`, `maxmul: 3` at `ts/src/defaults.ts:408`) — that is
**12 × rule-count × source-bytes**. `AGENTS.md:308-313` names
`rule.maxmul` and `rewind.history` as the engine's two standing bounds on
hostile input, but `rewind.history` bounds only `ctx.v` (measured peak
128 tokens at the default 64, because both runtimes grow to `2*cap` then
trim), and `maxmul` was written as a liveness guard. `unsafe.Sizeof(Rule{})`
is 264 bytes, so a 5-rule grammar on 1 MB permits 60 M passes ≈ 15.8 GB,
and a 100-rule ABNF-compiled grammar permits far more. Today the GC makes
that fine; in an arena the process dies with an OOM rather than a
`TabnasError`, losing the panic-free contract `go/doc/differences.md:474-489`
declares and `go/clib` exports. Measured density is also worse than §3.6
recorded: nesting-heavy input (`[`×5000 + `]`×5000) gives a flat **1.50
rule passes per source byte** against the 0.7 measured on flat
strict-JSON. Note PR #126 changes the budget formula, so re-derive after
it lands.

**Recovery's column bookkeeping counts bytes where the contract says
runes.** `advanceLexPast` walks the source one byte at a time and does
`lex.pnt.CI++` per byte (`go/recover.go:223-231`); its comment justifies
this on the grounds that RowChars are ASCII, which is true for *row*
detection and says nothing about the *column* counter. Measured: recovery
off, `{"x":"éééé","z":@}` gives `col=17 pos=20` — 17 is the rune column,
as `DIVERGENCE.md` contracts. Recovery on, `{"x":éééé,"z":@}` gives a
second error at `col=14 pos=13`, where the offending `,` is at rune
column 10 and byte column 14. Same engine, same instance; the unit flips
because the position was reached through `advanceLexPast`. This is a
second, independent instance of #115. Fix Go before #115 is ruled, so the
ruling has one units question to settle instead of two.

**Recovery is not in value parity, and the divergence is not a counting
question.** Measured on each runtime's own strict-JSON fixture, input
`{"a":\x01\x01,"b":\x01\x01,"c":\x01\x01,"d":\x01\x01,"e":1}` with
`suppress:0`:

| `maxRecoveries` | TS errors | TS value | Go errors | Go value |
|---|---|---|---|---|
| 1 | 2 | `{}` | 1 | `null` |
| 2 | 2 | `{}` | 2 | `{"a":null}` |
| 3 | 2 | `{}` | 2 | `{"a":null}` |
| 4 | 2 | `{}` | 2 | `{"a":null}` |

TypeScript's `ruleStack` also changes with the cap. So the proposed
`test/spec/recover.tsv` must pin the recovered **value**, not just the
error count — and this strengthens the case for keeping recovery out of
v0.1. Related: `go/recover.go:249` tests `MaxRecoveries <= len(ctx.Errs)`
*before* recording while `:502` tests `<` *after* recording; the comment
at `:245-248` documents exactly this overshoot class and says it was
fixed, but the fix landed on `attemptRecover` only, not on `absorbBad`.

**Recovery's error coalescing uses Go pointer identity.** Both the
cascade-suppression pop (`go/recover.go:275`) and the unlexable-run
coalescing (`:471`) compare `ctx.Errs[len-1] == err`. §3.6's design
stores errors as values, which has no identity, so a transliteration
silently becomes structural equality — and the engine's own notion of
"the same error" is deliberately weaker
(`alreadyRecorded` compares only `Code` and `Pos`, `go/parser.go:839-849`),
so structural comparison would collapse two genuine faults at the same
offset into one. Have `make_error_in` return the index it appended and
compare indices. Two lines if written now, archaeology later.

**`ctx.rewind()` across a recovery is undefined in both runtimes and
untested in either.** Recovery skips the lexer forward and refills
`ctx.T` (`go/recover.go:340-372`) but never touches `ctx.V`/`ctx.VAbs`,
so a mark taken before an error stays valid afterwards, and rewinding to
it re-feeds consumed tokens while the scan point has advanced past the
sync token. `grep -n 'Rewind' go/recover_test.go ts/test/recover.test.js`
and `grep -n 'recover' go/rewind_test.go ts/test/rewind.test.js` both
return nothing — the two features have never been exercised together.
Write one test in each runtime; even "the rewind errors" is a defensible
answer and pins it.

**`parse(&self)` is right, but three shipped mechanisms hold instance
state.** TypeScript's `continuations()` lazily builds and caches a
fail-fast sibling (`ts/src/tabnas.ts:213-218`, `:502-517`) and
accumulates into `#contAtEnd` from a permanent subscriber — which is
additionally racy today, since two concurrent `continuations()` calls on
one instance share it. Go's `Decorate`/`Decoration` map
(`go/options.go:408-421`) is where `@tabnas/debug` parks its state. Go's
continuations mechanism (a per-parse meta flag plus per-call subscriber
slices, `go/continuations.go:21`, `:280-282`) has none of these problems
and is the portable one; adopt it, declare TypeScript's sibling caching
an implementation detail, and the only interior mutability left is the
decoration map.

**`Continuations` costs a full parse per call, and Rust does not change
the asymptote.** Measured against the Go engine via jsonic, 200
iterations: prefix 341 B → 339 µs versus a plain parse at 325 µs (1.04x);
3,401 B → 3.57 ms versus 3.27 ms; 17,001 B → 18.6 ms versus 18.8 ms.
Typing a 3,401-byte document one character at a time with one call per
keystroke costs 5.78 s of engine time; at an optimistic 2-4x for Rust
that is 1.4-2.9 s against an LSP completion budget usually under 100 ms.
Building it into the first engine spends the port's headline argument on
the one feature where a constant-factor win is irrelevant.

---

## 4. Schedule Dependencies

### 4.1 The adjudications, sequenced

Four items genuinely block; five do not.

**Blocking, in order:**

1. **B1, the serialized options surface** (§3.2 here). New; not filed.
   Nothing else in the port means anything until this is ruled.
2. **#120**, ruled rule-scoped, unimplemented — including the five
   guarded deletes at `go/builtins.go:248,269,285,302,340` and the
   propagation fixture. Its third comment leaves a sub-question
   (`value$` read-then-delete ordering on the child-wins path) explicitly
   unpicked. TypeScript still reads `alt.k` at
   `ts/src/builtins.ts:127,138,170,238,249,263,273,305`.
3. **#122**, ruled compute-once, unimplemented —
   `ts/src/rules.ts:619` and `:737` still both evaluate
   `rule[oN|cN] - (alt.b || 0)` — plus the `alt.p`/`alt.r` post-action
   channel it does *not* close.
4. **#115** (`pos` units) and **#118** (serialized regex), both unruled
   but both with an open PR (#124, #127).

**Not blocking:** #116 (a prose count — `AGENTS.md:243-249` and
`schema/README.md:14` say nine codes, the registry carries ten), #117
(clib doc contradiction), #119 (Go panics as control flow — blocks only
if Rust must mirror the no-panic guarantee), #121 (a Go-only
grammar-mutation bug, though it should be ruled together with B3), #113
(a validator gap that blocks a Rust validator milestone, not the engine).

**Three of the four blocking items already have a PR.** #123 implements
the `\uXXXX`/`\x` fix — `ts/src/lexer.ts:1310` and `:1343` guarded with
`/^[0-9a-fA-F]{4}$/` and `{2}` — with twelve rows added to
`include-json-utf8-errors.tsv`; #124 serialises `pos` as a rune offset;
#127 moves the regex-dialect gap into `DIVERGENCE.md`. So the cost is
not adjudication, it is integration.

### 4.2 The integration queue is the current bottleneck

Seven PRs are open, all authored by the maintainer, all branched from
`base.sha = 9c1903d`, none merged: #114, #123, #124, #125, #126, #127,
#128. They will conflict with each other and each needs re-verification
after the first merge. Three add new shared fixtures
(`test/spec/lex-text-quote.tsv`, `lex-text-line-terminator.tsv`,
`rule-maxmul.tsv`), taking the corpus from 11 files to 14 — which grows
the third-runtime obligation before Rust exists, since the honesty gate
requires a runner per runtime per fixture. Drain the queue before Wave 0
starts and cap engine work-in-progress at one open PR for the duration.

One of them carries a lesson worth more than its diff. **PR #128** fixes
a Go text-run divergence and records: "That was the single largest
divergence class measured across the fleet — `a"b`, `x:a"b`, `{k:a"b}`
and `[a"b]` all parsed in TS and were parse errors here". It also shows
that two Go tests had been *pinning the divergence as if it were the
contract* (`TestMatchStringAbandon`,
`TestRelexBadTokenStillRaisesItsOwnError`). The real hazard in the lexer
tier is not an unpinned Go behaviour; it is a **pinned-but-wrong** one. A
Rust author copying Go's tests inherits it. Audit Go's lexer tests
against TypeScript behaviour as a Wave 0 task.

### 4.3 Rulings reverse

Issue #120 was ruled at 13:59:37Z and ruled the opposite way at
15:35:35Z — 96 minutes — with the second comment noting that the earlier
prototype "would have shipped the leakage bug". #115's stated fix
direction has already flipped from "correct the documents" to "change
Go" (PR #124). At two runtimes and hour-scale port lag this is a virtue.
For a months-long branch it is a hazard, because a Rust implementation
written against a reversed ruling is not a one-line fix — #120's reversal
changed *which runtime moves*, which invalidates the `enum Act`
config-scoping design the strategy document derives from it.

**Rule: the port consumes only rulings that have landed on `main` as code
plus a shared fixture, and the port branch rebases on tagged engine
releases, never on `main`.** Costs nothing; converts every reversal into
a merge instead of a rewrite.

### 4.4 Canonical drift, correctly sized

Since root `22fdf19`: 59 commits over 10 calendar days (9 with commits;
2026-08-16 is empty), 19 `feat:`, peak 16 on 2026-08-17. Churn:

| tree | added | deleted | total |
|---|---|---|---|
| `ts/src` | 1,712 | 98 | 1,810 |
| `ts/test` | 2,497 | 4 | 2,501 |
| `ts/doc` | 205 | 17 | 222 |
| **`ts/` total** | **4,550** | **131** | **4,681** |
| **`go/` total** | **7,971** | **472** | **8,443** |

**The port tax is 1.80x**, matching the feasibility report's own §6.1
aggregate. The 4.51x figure that has circulated divides Go's
implementation *plus tests plus docs* by TypeScript's implementation
alone. Single-runtime commits are 15 of 59 (11 Go-only, 4 TS-only) —
25%, not a third.

Canonical rate is ~181 line-touches per day. Even discounting the launch
burst to 10% of that, a 120-180 day build accumulates 2,200-3,300
canonical lines ≈ 4,000-5,900 lines of Rust port work at 1.80x, on top of
the base estimate. That is the number to budget as its own milestone,
measured at branch time and re-measured monthly.

### 4.5 Release and packaging

The version arithmetic in `/workspace/admin/publish.sh:6-9` computes one
number as `max(npm latest, latest go/v tag, ts/package.json, Go VERSION
const)` and applies it across a hand-ordered wave with `parser` first.
There are 23 `go/v*` tags and 19 `ts/v*` tags on origin, plus one stray
bare `v0.3.0`. **v0.8.9 does not exist anywhere**: commit `a3f286b`
rewrote all four version locations, was never tagged in either runtime,
and was never published — that release was agent-run, one failure in
three.

Adding a crate makes it five version locations plus an immutable
registry: crates.io has no unpublish at all, where npm has a 72-hour
window and the Go proxy just serves a tag. A cargo failure halts the wave
at repo 1 of 32.

Two decisions before the first crate exists. **Version the crate
independently, starting at 0.1.0, and have it report the engine version
it implements** — `py/tabnas.py:138-142` already reads the version from
the library and adds zero version locations. That keeps it out of the
lockstep wave entirely. And **decide the cross-directory test idiom
before the first `cargo package`**: measured with cargo 1.94.1, a runtime
read of `../ts/package.json` (mirroring `go/version_test.go:23`) passes
in-repo, packages with no warning, and *fails inside the published
crate*; the idiomatic `include_str!("../../ts/package.json")` fails
`cargo package --verify` outright. Gate such tests behind a feature, copy
the registry into `OUT_DIR` from a build script, and add
`cargo package --verify` — not just `cargo test` — to the pre-release
check.

**ADR-12 needs superseding.** `/workspace/admin/DECISIONS.md:207` says
languages beyond TypeScript and Go get tabnas through C-ABI bindings,
"not native ports"; its status is *proposed (2026-08-16) — merging this
entry constitutes acceptance*, and it is the only ADR in the file not
marked accepted. The same programme plans a Rust *binding* crate in its
own repo (`notes/2026-08-16-clib-ffi-strategy.md:186-199`, "One `tabnas`
crate (own repo, e.g. `tabnas/rust`)"), which claims the obvious name.
Write ADR-13 superseding clause 1 and settle the naming collision in the
same paragraph. Check crates.io availability first: the API returns 403
from an automation environment, so this needs a browser or `cargo search`
from an ordinary network, and the name is permanent.

### 4.6 CI ownership

Nothing under `ci/` is wired (`ci/README.md:1-5`). The sole gating
workflow is a 17-line caller into `tabnas/.github`'s shared
`polyglot-ci.yml`, which 22 repos use and which automation credentials
cannot write (`admin/DECISIONS.md:82-91`, ADR-8). Build the Rust arm as
`ci/rust/` plus `ci/workflows/rust.yml`, provably runnable locally, and
batch the maintainer-only activation into one rollout — never a change to
the shared workflow, so a Rust break can never redden the other 21 repos.
Also put `git config --global core.autocrlf false` in any Rust job: the
existing guard is in the ts lane only
(`admin/rollout/workflows/dot-github__polyglot-ci.yml:72-73`); the go job
never needed it because `bufio.Scanner` strips a trailing `\r`. A Rust
loader splitting on `'\n'` leaves a stray `\r` on the last column of
every fixture row.

Cost is not the constraint. Measured here: `go test ./...` 0.13 s
(0.58 s wall), `npm test` 3.0 s, and a 15k-line crate with serde_json and
regex builds cold in about 15 s on four cores. Run the whole three-way
loop every commit.

---

## 5. The De-risking Plan

### 5.1 Wave 0 — contract repairs at two runtimes, no Rust

About three weeks, and worth doing whether or not Rust happens.

1. Drain the seven open PRs.
2. **B1**: publish the 27-vs-22 options table, get one ruling, and land
   an exhaustiveness test over `Options`' fields so a future group cannot
   be added without a serialized answer. Add one fixture row per group
   asserting applied-or-refused.
3. **The tokenSet array-merge divergence** and the other array-valued
   options (`ender`, `result.fail`, `recover.syncTokens`,
   `recover.syncGroups`).
4. #120 with the five guarded deletes; #122 plus the `alt.p`/`alt.r`
   adjudication; #115; #118.
5. Correct `go/doc/differences.md:94-105` ("No behavioural effect") and
   land the two-row match-token fixture. Delete the stale
   `Forced`-is-always-false sentence at `:707-708` and the `ignored` row
   at `:90-93`.
6. Audit Go's lexer tests against TypeScript behaviour, per §4.2 here.
7. Reconcile `AGENTS.md:135-136` with `test/AGENTS.md:36-37`, extract one
   loader spec, fix the four runners, and add `preprocessEscapes` to
   `go/utility_spec_test.go`'s column reads so `snip` becomes testable.
8. Generalise `nonParity` to `map[fixture]map[runtime]reason`, move the
   gate to `ci/gate/`, and wire `py/` into it first — it will go red for
   the 220 rows it does not run, which is the point.
9. Restructure `goOnly` into per-entry `runtimes: [...]`, and land the
   `web` and `mcp` changes in the same wave (`web`'s copy has a rival
   singular `"runtime"` field and is three patch releases stale, gated by
   nothing). Fix the nine-versus-ten prose while there.
10. Add the ~25 discriminating utility rows (numbers, non-ASCII
    truncation, the placeholder charset, `move` out of range, a null
    list element, integer-like key order) and fix Go's compact renderer
    before adding any multi-key `strinject` row, or it flakes.
11. Produce the `--full --strict` ABNF artifact and check it in
    (§6 here).

### 5.2 The differential harness — build it first, and call it what it is

The four-spec corpus proposed as a "walking skeleton" contains no Rust,
so it is not a skeleton of the system being built. It is the differential
harness, and it should be built first on its own merits.

- **Three legs, one format.** Add a `spec` mode to
  `ci/parity/tokdump.js` and `ci/parity/gotokdump/main.go` (about fifteen
  lines each), driven by function-free serialized specs rather than the
  `json`/`jsonic` imports at `tokdump.js:44-50` and `gotokdump/main.go:20-22`.
  Proven: 284 input cells from this repo's own fixtures, all token
  streams byte-identical TS↔Go, with no downstream checkout. Report it
  honestly — those 284 cells are 136 distinct strings, nineteen of which
  are the fixtures' own `#` comment prose and 35 of which are bare
  scalars. It is a real downstream-free lane, not a coverage claim.
- **Compare in strictness order:** token stream (`name`/`sI`/`len`; `cI`
  already excluded) → accept/reject → the structured-diagnostic subset
  `diagnostic.tsv` already defines → canonicalized value. Put the
  diagnostic before the value: it is a fixed-shape record, so mismatches
  self-classify.
- **Key order gets its own lane.** Measured, the divergence is *not* at
  the 40% first reported: over 500 generated documents, 115 (23%)
  serialize differently and **none** of them differs in key order — every
  one is `-0` rendering (`JSON.stringify(-0)` → `0`,
  `json.Marshal(-0.0)` → `-0`). 210 of the 500 contain a multi-key
  object and all agree, because the value builders produce Go
  `*OrderedMap` with insertion-order marshalling. Key order diverges on
  the **tree-builder** path, where Go's `mkNode` (`go/builtins.go:38-43`)
  returns a plain map that `json.Marshal` sorts — measured live: the same
  tree fixture on input `a` gives TypeScript `{"src":"a","kids":[]}` and
  Go `{"kids":[],"src":"a"}`. Canonicalize for the value lane, as
  `ci/fuzz/run-diff.sh:45-61` already does, *and* run a separate
  order-sensitive assertion.
- **Mutation is mandatory.** `ci/fuzz/gencorpus.js:44-64` emits only
  well-formed documents, so the error path — where every recorded
  divergence lives — has never been differentially tested. A ~30-line
  mutation stage (delete, insert poison token, truncate, case-flip,
  duplicate) over the same seeded corpus found 44 mismatches in 500 cases
  in five classes on one run. Treat the specific counts as
  stage-dependent; the mechanism reproduces and it found the `\uXXXX`
  leniency on its first run.
- **Comparator honesty.** The valid corpus gives 500/500 agreement under
  canonicalized JSON and 490/500 once Go `nil` is distinguished from JS
  `null` — ten documents are the literal source `null`. That is a
  representational decision the Rust port must make on day one
  (`Option<Value>` versus `Value::Null`), not a fuzzer finding. Pick one
  comparator and apply it in both places.
- **Known divergences as data.** A `divergence.tsv` of
  (class → allow | expect-and-pin | fail), seeded from `DIVERGENCE.md`,
  `go/doc/differences.md`, the mutation classes **and the options-plumbing
  entries from B1**. Any unclassified mismatch fails the build and prints
  the class, the answers and the issue number.
- **Row counts.** `assert_eq!(ran, N)` per runner per runtime — 265 data
  rows total, 254 of them parity: diagnostic 10, happy 11 (TS-only),
  include-json 34, utf8 12, errors 4, utf8-errors 5, lex-string-control
  14, utility-deep 52, utility-modlist 78, utility-str 23,
  utility-strinject 22. Without it, a subtly wrong third loader passes
  vacuously; Go's own consumers already `continue` silently on short rows
  (`go/spec_test.go:238-240`, `:267-269`).
- **Pairwise labelling.** Run all three pairs. Go↔Rust needs no
  normalization at all (both UTF-8, both byte-indexed, neither can hold a
  lone surrogate) and is therefore the strictest and cheapest — and it
  must **never** be cited as conformance, because `AGENTS.md` authority
  rule 1 makes TypeScript canonical. Write that ADR before the first
  Go↔Rust green, not after.
- **Matcher-tier parity.** A fixed set of hand-written matchers — a
  stateful one, a queueing one, a bad-token one, one registered under a
  built-in name — implemented once per runtime against a shared token-dump
  format. About 200 lines per runtime, and it is the only thing that can
  see any of the divergences in §3.1 here.

### 5.3 Wave 1 — the Rust thin slice, in parallel

Run a genuine thin slice rather than four days of paper spikes:
serialized spec → lexer with the `[Vec<MatcherId>; 257]` first-char
dispatch → rule push/replace with `n`/`k` propagation → one tree builtin
performing the upward `fold$` → one structured diagnostic. That single
artifact answers most of the architecture questions as a by-product, and
it is the only thing that exercises the arena under real control flow.

The questions it must answer, and which must be settled before the
corresponding struct is typed:

| # | Question | Deliverable |
|---|---|---|
| S1 | Does the never-free arena need a `rule.maxrules` bound, and what default? | A decision plus a default, not a `size_of`. The ceiling is 12 × rules × bytes at 264 B/Rule |
| S2 | The Lex/Ctx field split | `Lex{cur, want, relex_undo}` + `Ctx{pending, end}` + `relex(&mut self, ctx, from, want)`; the compiled probe is 64 bytes and allocation-free |
| S3 | Subscriber calling convention | `subs: &mut Subs` threaded through the five Go dispatch sites; confirm `Lex::next` still compiles with two disjoint `&mut`s |
| **B2** | Does `Config` freeze at build time or stay live? | Replaces the matcher-contract spike in this wave. Determines whether `&'g Config` spans the instance or a parse |
| S5 | `Value`: arena `NodeId` or recursive enum | Removes the uncatchable stack-overflow-on-Drop |
| S6 | `SrcIdx` newtype + `clippy::string_slice` ban; `bad()` takes `SrcIdx` | The only affordable time to do it |

The matcher-contract document (`doc/lex-matcher-contract.md`) moves to
**M3**, where custom matchers actually land. It changes no v0.1 struct.

### 5.4 Wave 2 — the two go/no-go measurements

**S7, the port-rate probe, is the highest-value single item in this
document.** Port one already-shipped feature end to end to Rust and
record the wall clock. Error recovery is the right probe: TypeScript
`2b21c8d` (#96, 704 insertions), Go `308db48`+`bf50ac8`+`70773f1` (1,477
insertions). **3-5 days.** Every schedule figure in both feasibility
documents rests on line-count extrapolation and an unmeasured ratio.

**S8, the publish probe.** Publish a trivial crate — a spec loader, or
less — to crates.io before the engine work starts. **Half a day plus one
maintainer round-trip.** It answers name availability, MSRV, trusted
publishing, `cargo package --verify` and the release-checklist entry
ADR-12 clause 4 demands, while the cost of getting it wrong is a wasted
0.1.0 rather than a wasted engine. `py/` is the precedent: 435 lines
landed in a single sitting on 2026-08-12 and have not moved since,
because the wheel matrix needs a macOS runner nobody has stood up
(`py/README.md:42-45`). Distribution, not code, is where the project's
only previous third-language artifact stopped.

### 5.5 One budgeted cost that does not exist

§5.4 concludes that `ci/parity` and `ci/fuzz` "cannot take a Rust leg at
all" until `json` is ported, and tells the reader to budget that port.
That is true only because both dumpers hard-code their grammar import.
Both harnesses run on the in-repo function-free serialized spec with no
downstream checkout at all — proven above. What genuinely stays
unbudgeted is the relaxed `jsonic` leg: 1,878 TS / 1,516 Go non-test
lines, 24 arrow functions in `jsonic/ts/src/grammar.ts` including two
live actions at `:663` and `:670`, no serialized artifact and no Go CLI.
That is what actually caps a Rust leg's `ci/parity` and `ci/fuzz`
coverage.

### 5.6 Staging: gate on engine reach, not row count

The obvious staging metric is the wrong one. The 175 utility rows are 69%
of the parity corpus and reach approximately none of the engine — and
§4.7 establishes that those same four functions are the *most* divergent
and *least* adjudicated surface in the repo, none of which appears in the
175 benign ASCII rows. Staging on them simultaneously maximises the green
number, concentrates the unadjudicated behaviour and retires zero engine
risk. It is the same disease as a vacuous row count, expressed as a
vacuous pass rate.

A defensible ladder, reported with rows as a secondary number:

1. Loader + schema + the `v` gate + `strinject` (needed because it
   renders every error message).
2. The lexer with the eight built-in matchers, against
   `lex-string-control` and `include-json-utf8`.
3. The rule engine + the twelve non-tree builtins + `diagnostic.tsv`.
4. The four tree builtins.

Keep `deep`/`modlist`/`str` as exported APIs only if a consumer asks —
and adjudicate them first either way, since they are the four a Rust
author must otherwise guess at.

**Note on the tree builtins.** All four — `@node$`, `@capture$`,
`@bubble$`, `@fold$` — are 0% covered by every function-free fixture in
the tree today, including `fold$`, whose `own === p` self-fold
(`ts/src/builtins.ts:174`) is the exact aliasing case §3.6's
`get_disjoint_mut` design rests on. A ~25-line function-free tree-builder
spec closes that hole and agrees across runtimes; write it in Wave 0.

---

## 6. What v0.1 Is, and Who Uses It

**IN:** the serialized `GrammarSpec` loader, schema validation and the
`v` gate; the eight built-in matchers over `&'g Config` with every
derived table built eagerly; the rule engine; all 16 `$`-builtins with
the documented `n`/`k` propagation; structured diagnostics matching
`schema/diagnostic.schema.json`; `parse(&self)`; budget and cancellation
(trivial, LSP-shaped, and demonstrably free of `Send`/`Sync`); and
`strinject`, because it renders every error message.

**OUT, stated in the README in the same voice option B's ceiling is
stated:** custom lex matchers, subscribers, `ParsePrepare`, `Decorate`;
error recovery (about 900 Go lines, zero shared fixtures, and the two
runtimes disagree on the recovered *value*, §3.4 here); `Continuations`
(one full parse per call); the Info/`MapRef`/`ListRef`/`Text` carriers,
pending B4; `RegisterTextParser`; the imperative plugin tier.

**The first-user claim needs narrowing.** The proposed first user is the
BNF/ABNF/GBNF output family — a Rust build script or CLI validating and
running a compiled grammar artifact with no Node or Go toolchain. Three
facts sit between the compiler and that consumer:

1. `bnf/ts/src/spec.ts:418-423` — `compileSpec` defaults to
   `toRecognitionSpec`, and recognition mode drops all four tree
   builtins (`TREE_BUILTINS` at `:39`, applied at `:140`). The ABNF CLI
   wires this as `recognition: !args.full` with `full: false`
   (`abnf/ts/src/bin/tabnas-abnf-cli.ts:30`, `:103`). **The default
   `--compile` artifact is a recognition-only grammar: accept/reject** —
   which is exactly what the C ABI already delivers for about a week of
   wrapper work.
2. `abnf/ts/src/compile.ts:46` returns "pure-data tabnas grammar as
   jsonic text", and `toJsonic`'s `strict` flag is not surfaced by the
   CLI — there is no `--strict`. So a v0.1 engine must parse relaxed
   jsonic to read its own input, and jsonic is the grammar with no
   function-free artifact.
3. The whole family's function-free-ness rests on one non-default
   boolean: `bnf/ts/src/compiler.ts:2511` sets
   `refs.useBuiltins = !!opts?.builtins` (declared `false` at `:2743`),
   and `abnf/ts/src/compile.ts:48` opts in with the comment "Always" at
   `:44`, while `abnf/ts/src/abnf.ts:53` passes `false` on the actions
   path.

**So v0.1's differentiated capability over the existing C ABI is
structured diagnostics plus `--full`/`toPureSpec` tree-building specs.**
Say that in the README. Then make it an object rather than an argument,
in Wave 0 and in half a day: add `--strict` to the ABNF CLI, run
`--full --strict` on a real grammar, and check the emitted `.json` into
`test/spec` as a fixture both existing runtimes load. Pin the builtins
boolean with a test in `bnf`, `ebnf` and `gbnf` asserting the compile
path emits zero non-`$` refs, in both runtimes.

If that artifact does not exist by the end of Wave 0, the first-user
story is a hypothesis and v0.1's scope should be reopened before M2b.

**The fleet ceiling is harder than it looks.** Twelve of the 34 repos
register custom lex matchers, so those twelve cannot run on v0.1 at any
level of completeness — not "with reduced features", but at all. Zero
repos depend on a Rust engine today; 29 depend on
`github.com/tabnas/parser/go` and 31 on `@tabnas/parser`. The engine
ships no grammar by rule (`AGENTS.md:24-41`), and only `json` (14 builtin
refs, 0 closures) and `jsonl` (7/0) are function-free at the grammar
level — and `json` is not function-free at the *options* level, because
`json/ts/src/json.ts:44` uses a negative-lookahead regex neither RE2 nor
Rust's `regex` can compile, implemented in Go as a host predicate with an
extra overflow check (`json/go/json.go:46,63-69,81`). Budget `json` as
the day-one grammar: 277 TS / 759 Go lines, 3 TSVs, 28 `it()` + 20 Go
tests, and exactly one Rust predicate to hand-write.

---

## 7. Stop Conditions

Eight observations that should trigger a rethink, each with a threshold
that can actually fire. Pin them in writing before M2b starts.

1. **Port rate.** If S7 exceeds **2x** the Go leg's cost, the schedule is
   wrong and should be re-derived before M2b rather than pushed through.
2. **Divergence discovery rate.** After Wave 0 lands, if the mutational
   differential still finds **more than five new unadjudicated TS/Go
   divergences per 1,000 mutated cases**, the contract is not stable
   enough to have a third runtime written against it.
3. **Ruling implementation latency.** Not adjudication latency —
   adjudication here is measurably fast (median closed-PR lifetime 13.4
   minutes across 53 closed PRs, maximum 17.0 hours, none open a full
   day; #120 filed→ruled in 1 h 53 m). The quantity at risk is
   *ruled→landed*, currently 0-for-2 (#120 and #122 both ruled and
   unimplemented). **A ruling unimplemented for more than 5 days** is
   roughly seven times the worst observed PR lifetime and is a genuine
   anomaly.
4. **Canonical drift.** If unmerged `ts/src` churn since the branch point
   exceeds **1,000 line-touches** (about 5-6 days at the measured rate),
   freeze Rust feature work and catch up. Branch from a tag, never
   `main`.
5. **Engine reach.** If the Rust leg stalls below **stage 3** of §5.6
   here for a month — the rule engine plus `diagnostic.tsv` — treat it as an
   adjudication problem, not an engineering one. Do **not** use the
   175-row utility floor as the alarm: a crate implementing four string
   helpers clears 69% of the corpus.
6. **Exemption regression.** Any Rust milestone that needs a *new*
   per-runtime exemption for a fixture the other two runtimes run is the
   `py/` failure mode starting. Hard stop.
7. **Arena memory.** Measure against the `maxmul` ceiling on a
   high-rule-count grammar (an ABNF-compiled one, 100+ rules), not
   against the benign 112 KB benchmark. If retained bytes at the ceiling
   are not bounded by a *configured cap* — not by input shape — the
   never-free decision is not finished. This must be settled before
   `RuleId` appears in any plugin-facing type.
8. **Framing.** Any PR, badge or document citing Go↔Rust parity as the
   gate inverts canonicality (`AGENTS.md` authority rule 1). Stop and fix
   the framing before merging.

And one adoption gate, separate from the rest because it fires after
shipping. ADR-12 conditions a port on recorded inbound requests, and none
exists anywhere in this repo, the 34-repo fleet or `admin/notes` — the
issue tracker has zero non-maintainer participants. **If v0.1 ships and
nothing external depends on it in 90 days, freeze at v0.1 rather than
building the plugin tier.** Better: retire the question *before* M2b, by
producing the `--full --strict` artifact of §6 here and naming its
reader. That is a day's work against a 90-day wait.

---

## 8. Open Questions Only the Maintainer Can Answer

- **B1-B4** in §2 here. All four are rulings, not measurements.
- Which runtime is right about `maxRecoveries`, and whether the recovered
  *value* divergence in §3.4 here is contract or bug. Belongs with the
  #115-#122 batch and is not currently filed.
- Whether `DIVERGENCE.md`'s "error message text ... only the error `code`
  is contractual" covers the strinject **placeholder resolution set**
  (TypeScript spreads five live engine objects into the ref bag,
  `ts/src/error.ts:274-289`; Go builds six keys plus the `use` bag,
  `go/tabnas.go:326-350`) or only wording. The Rust error-rendering
  budget differs by a large factor.
- Whether `Continuations` is in scope for a first Rust engine. Its cost
  is measured; whether any named downstream consumer needs it is not.
  `/workspace/tabnas/mcp`, `skills` and `web` were not examined for
  LSP-shaped consumers.
- Whether `str`/`snip` are contractually exported or incidentally
  exported. If incidental, 23 of the 175 rows become optional.
- Whether the crate name `tabnas` is available, and which of the engine
  and the planned FFI binding takes it. crates.io names are first-come
  and permanent; the API 403s from an automation environment.
- Whether a second maintainer, funding or dedicated time exists. Nothing
  in the repo, the fleet or `/workspace/admin` records a resourcing
  commitment; the only signals are the commit ledger and a
  single-collaborator ACL.
- Whether `tabnas/.github`'s `polyglot-ci.yml` can express a Rust job,
  and what changing it costs the other 33 repos. That repository is not
  in the fleet clone.
