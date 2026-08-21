# Rust Port: Design Review and Implementation Plan

Fifth in the series after
[`doc/rust-port-feasibility.md`](rust-port-feasibility.md),
[`doc/rust-callback-porting-strategy.md`](rust-callback-porting-strategy.md),
[`doc/rust-port-risks.md`](rust-port-risks.md) and
[`doc/engine-changes-for-portability.md`](engine-changes-for-portability.md).

Those four were written in five days against a brief that moved twice
(recommend → approved → widen to canonical-engine changes), and each has
its own sequencing section. This document does two things: **Part 1
reviews the series** — what stands, what one document supersedes in
another, and what the tree has already done since they were measured —
and **Part 2 is the consolidated implementation plan**, one sequence,
with the open decisions isolated at the front where they gate everything
behind them.

Citation convention as before: a bare `§3.4` is a section of the
feasibility report, `§2.3 strategy` the callback strategy document,
`§2.1 risks` the risk register, `§3.A1 changes` the engine-changes
document, and sections of *this* document are written `§1.2 here`.

> **Provenance.** Part 1's "verified" claims were re-checked against the
> tree at `abfed2c` (engine v0.8.11, 2026-08-21) — 79 commits after
> `94c57b6`, the newest measurement point in the series. Everything else
> cites the document that measured it. The plan targets **v0.1 as scoped
> by `§5.1 risks`** (the serialized-spec engine); the imperative plugin
> tier stays unscheduled per `§5.6 strategy`.

---

## Part 1 — Design Review

### 1.1 Verdict

**The design is sound and adoptable.** The load-bearing structural
answers — arena + `u32` ids, `&'g Grammar`, the four Context hoists, the
two-argument action, span tokens over one shared source handle, effects
as returned data for matchers — are each backed by a compiled probe or an
external precedent, usually both, and no later document overturns an
earlier one's *structural* finding. What the series does not do is agree
with itself on sequencing and on one implementation route, because each
document was written against a different brief. Three genuine conflicts
survive to the end (§1.4 here); one of them — the builtin-config route —
is the single decision that gates the largest measured win and the
callback-narrowing wave behind it, and it is a maintainer call, not an
engineering one.

The series' weakest point is not internal: it is that the tree and the
org moved while it was being written. ADR-13/14/15 (accepted 2026-08-19)
change the adjudication rule, the recording mechanism, and one of the
open questions the documents carry forward; the parity wave repaired two
of the divergences the feasibility report lists as preconditions; and
the `tabnas-clib` crate that the strategy document schedules as
milestone M1 already exists, verified, in `admin/staging/bindings/rust/`.
A plan built by reading the four documents in isolation would re-decide
settled questions and re-build shipped code. §1.3 here is the ledger.

### 1.2 How to read the series

| Doc | Brief | Standing |
|---|---|---|
| Feasibility | "Should this be ported?" | Recommendation (reject A, gate C) **overridden by the maintainer**; its measurements, blocker analysis (§3) and divergence table (§4) remain the reference. §3.6's "the callback must carry the `AltMatch`" is superseded by `§5.5 strategy`. §4.1/§4.2 preconditions are done in-tree (§1.3 here). §4.6 is answered by ADR-15. |
| Strategy | "How, if ever — and what did everyone else do?" | The milestone skeleton (M0 → M1 → gate → M2a → M2b; M3 never) is adopted by this plan. Its M0 route for #120 (S0: move reads to `r.k` + five deletes) is **contested by `§3.A1 changes`** — decision D1 below. Its `AltCond` bucket-B classification is falsified by `§2.1 risks` (an `AltCond` re-enters the lexer in `@tabnas/c`): the Rust `AltCond` takes `&mut Ctx, &mut Lex`. |
| Risks | "The port is approved; what kills it?" | The register's shape (rank on residual damage), Wave A/B structure, walking skeleton (lexer first), differential harness, v0.1 scope line and stop conditions are adopted wholesale. Its snapshot of the tree is the most stale: the nine-PR queue is drained, and `tabnas-clib` exists. |
| Engine changes | "What should canonical TS change so the port is cheaper?" | The newest and the one that redirects: A1 (load-time builtin config) at a measured −12%, B1/B2 (source immutability, span invariant), the C1–C7 narrowing wave, the D-tail, eight maintainer decisions (§5), and the measurement-apparatus repairs (§2/§6.1) that everything perf-justified now depends on. |

Where two documents disagree on a *fact*, the later measurement wins and
says so in place (each carries its own corrections honestly — e.g. the
Go-adoption figure was corrected twice, ending at 31 modules). Where they
disagree on a *route*, this plan resolves it in §1.4 and Phase 0.

### 1.3 What the tree has already done (verified at `abfed2c`)

Done since the series was measured — do **not** re-plan these:

| Item | Series status | Verified now |
|---|---|---|
| Diagnostic `pos` units (#115, §4.1) | "sharpest trap; fix before any Rust" | **Repaired** — Go emits runes (`go/tabnas.go:488-504`), schema rewritten, opposite-assertion pins in both runtimes (`ts/test/divergence.test.js:235`, `go/divergence_test.go:343` `TestDiagnosticPosCountsRunes`). Note the repair went the *opposite* way to §4.1's advice (docs-say-bytes): Go moved to runes. Close the issue. |
| Serialized regex dialect (#118, §4.3) | "adjudicate the `\s` and `.` rows first" | **Adjudicated and recorded** — `DIVERGENCE.md` "Regex dialect in serialized terminals": recorded-not-fixed, with the translation-layer cost stated, a portable explicit-class workaround pinned in both runtimes (`TestDivergenceRegexDialect` + TS twin). Rust consequence in §2.5 here. |
| Bad-token spans (§4.2) | "the one genuinely free choice" | **Moot — repaired in both directions**: TS escape decode made strict, Go string errors moved onto the offending construct; kept as parity tests (`TestEscapeDecodeIsStrict`, `TestStringErrorsPointAtTheConstruct`). |
| Parsed key order (§4.6) | "adjudicate before a third port" | **ADR-15**: map key order is **out of the value contract**; signed zero is **in** (`deepEqual` gains strict numeric compare). The Rust engine may use `IndexMap`/insertion order freely; no ECMAScript integer-key emulation, ever. |
| Canonicality rule | All four docs treat `AGENTS.md` rule 1 as the decision rule | **ADR-13** (2026-08-19): repair direction is **defect-level, not port-level** — "canonical names the language, not the winner". `§3.1 risks`' default-to-TS unblocking survives as the *default*; every adjudication in Phase 0/2 states its direction per defect. |
| Divergence recording | Docs recommend fixtures ad hoc | **ADR-14**: every unrepaired divergence lives in an **executable register both runtimes run** (precedent: `jsonic`'s `test/spec/divergent.tsv`); prose-only records are defects. This upgrades several plan items from "write a fixture" to "this is org law". |
| Class-A option merge (empty string vs unset) | `§2.2 risks` item 5, unrecorded | **Recorded** in `DIVERGENCE.md` ("An explicitly empty option cannot be expressed in Go") as *deferred, not deliberate*, repair named (`Chars *string`), adoption cost counted (15 fleet call sites), pinned by `TestEmptyCharsMeansUnset` which **fails when the repair lands**. Classes B/C/D still need rulings (D3 below). |
| The nine-PR queue (`§3.2 risks` step 1) | "merge serially, then tag; cap WIP at one" | **Done** — 0 open PRs on `parser`; #123–#135, #145–#147 merged; v0.8.11 released with unified TS+Go versions. Stop-condition 4 is clear. |
| `rule.maxmul` plumbing (part of #130's surface) | dropped by `MapToOptions` | **Plumbed and pinned** (`TestMaxMulSurvivesTheOptionsMap`); the *rest* of #130's leaf set is still open, now joined by #142/#143/#144. |
| Lexer-tier fixtures | "one fixture in eleven touches the lexer" | Now **three**: `lex-string-control.tsv` plus `lex-text-line-terminator.tsv` and `lex-text-quote.tsv`, all grammar-free, driven from `ts/test/lex.test.js` — the Rust lexer's day-one parity surface grew while nobody was looking. |
| M1, the Rust FFI crate (`§5.2 strategy`) | "~1 week for the wrapper" | **Built and verified**: `admin/staging/bindings/rust/tabnas-clib` — generic loader for the per-format clibs under the uniform ADR-12 ABI, returning the parsed value as JSON; `cargo test` conformance + shared-handle threading green against a built `libtabnasjson`. Awaiting maintainer repo seeding (`seed-repos.sh --apply`, org-admin gated). What it does **not** cover: `parser`'s own grammar-agnostic `go/clib`, whose `core.go:115` still discards the parse value (#117 open). |

Still outstanding, exactly as the series says (Phase 2 is built from
these): #120 and #122 **ruled but unimplemented** (`ts/src/builtins.ts`
still reads `alt.k` at all eight sites; `ts/src/rules.ts:737` still
re-reads `alt.b` post-action); the `alt.p`/`alt.r` post-action channel
unadjudicated; #130/#142/#143/#144 open; `nonParity` still
`map[string]string` and the registry still carries a binary `goOnly`
key; the registry version still coupled to the engine version; no
propagation fixture and no options-pipeline fixture; #113, #116, #117,
#119, #121 open; and `AGENTS.md` still documents a deleted
`.github/workflows/build.yml`. (Two `AGENTS.md` items are brought
current by this change itself, so they are done rather than
outstanding: the `doc/` index now names the full series, and authority
rule 1 carries the ADR-13 amendment.)

### 1.4 The three conflicts, resolved

**(1) The builtin-config route — S0 versus A1.** `§5.1 strategy` item 1
implements ruling #120 (rule-scoped, per the final comment on the issue)
by moving the eight builtin reads to `r.k` plus copying Go's five
consume-once deletes. `§3.A1 changes` implements it by hoisting config
into closures at grammar load, so the key never enters any bag — and
measures S0's route as permanently forfeiting **123,174 keep-bag copies
per megabyte, −12% throughput** on a jsonic-class grammar with a clean
sign flip. The two are mutually exclusive (`§3.C2 changes`).

**This plan recommends A1**, with one honesty requirement: A1 is not
"#120 implemented the cheap way", it is a **partial re-ruling**, and the
amendment must be recorded on the issue before the code moves. Under the
ruling as written, a parent alternate that *sets* `k: {value$: …}`
without running the builtin and pushes a child that runs `@value$` bare
answers `4` (config descends); under A1 it answers `3` (config is bound
to the declaring alternate; a bare builtin gets defaults). The measured
support for A1's side: `3` is what TypeScript answers **today**, the
#120 downstream scan found all four fleet declaration sites pair the
config with its action on the same alternate (so no fleet code
exercises inheritance), the run-then-push shape — the one `jsonic` and
`json` actually rely on (`json/ts/src/json.ts:83-84` documents relying
on the key's *absence*) — answers `3` under both routes, and A1
dissolves the four-regime table of `§3.A2 changes` into one regime
instead of standardising two. The probe family (`pd_*` state) is
excluded from the hoist by design and keeps propagating, so the BNF/ABNF/
GBNF closure is untouched — the same carve-out-free property the final
#120 comment valued. **Decided 2026-08-21: A1.** The amendment is
recorded on issue #120; Phase 2a implements it.

**(2) The action signature.** §3.6's "the callback must carry the
`AltMatch`" is superseded by `§5.5 strategy` (two arguments, Go's
contract, declared as a narrowing) — and `§3.C1/C2 changes` turn that
from a Rust-side decision into a TypeScript-first engine change, which
is the right order: the canonical engine stops reading the alternate
after the action (C1), then stops passing it (C2), and the Rust surface
is then a port of what ships rather than a divergence from it. The
`expr` migration (`§3.C2 changes`: port Go's alternate split at
`expr/go/expr.go:1385-1461`) and `AltModifier`'s take-and-return
exception ride with the wave. C2 is sequenced strictly after D1 lands as
A1 — under A1 the builtins stop needing argument 3 at all, which is what
unblocks C2 for free.

**(3) `AltCond`'s bucket.** `§2.1 risks` measured `@tabnas/c` driving
`lex.next` from an alternate condition to arbitrary depth, which
falsifies `§3.1 strategy`'s bucket-B classification and its "one
exercised re-entrant type" census. Adopt the risks correction: the Rust
`AltCond` is `Fn(&mut Ctx, &mut Lex, RuleId) -> Result<bool, Fault>`,
and the porting guide's re-entrancy census reads "two" (actions via
`rewind`, conditions via `lex.next`).

Two smaller resolutions worth recording. The **scan driver ports from
Go, not TypeScript** (`§2.1 risks`): Go's rune-`size` advance and
`Fallback` are what a Rust `&[u8]` walker needs; §2's "direct third
copy" claim is kept for the table shapes only. And the **regex lowering**
question (§4.3) is now bounded by the #118 adjudication: the recorded
contract is that `\s`/`.`/`(?i)` are dialect-divergent, deliberately,
with the portable spelling being explicit classes. One correction to the
series while re-verifying: Rust's native `\s` (`\p{White_Space}`) is a
genuine **third set**, not TS's side — the feasibility report's own §4.3
table already shows it rejecting U+FEFF where JS accepts it, and the
Unicode `White_Space` property admits U+0085 where JS's class does not.
So the port either lowers `\s` (and the other dialect-divergent
constructs the entry records) to the JS class at grammar load — the
~15-line lowering §4.3 describes, now mandatory rather than optional —
or refuses it in serialized terminals; explicit classes compile
identically in all three engines and stay the recommended portable form.
The port refuses unknown flags exactly as Go does, and the
`DIVERGENCE.md` entry gains a Rust column recording the third set rather
than forcing a three-way re-adjudication.

### 1.5 Review findings the series does not carry

**(F1) The port decision contradicts a standing ADR, in writing.**
`admin/DECISIONS.md` ADR-12's governing premise (status: *proposed*):
"Languages beyond TypeScript and Go get tabnas through C-ABI bindings
over the Go implementation, **not native ports** … a port is
reconsidered only on Phase-6-style evidence." (The premise, not ADR-12's
numbered clause 1 — that is the uniform-symbol contract, which stands.) The risk register's request for an ADR
superseding that clause (`§3.5 risks`) was overtaken — ADR-13/14/15/16
were spent on other decisions. Nothing currently records the approval
the risk register's own title asserts. **D2 below writes it.** Until it
exists, the port is a verbal decision contradicted by the org's decision
log, which is precisely the state ADR-14 exists to forbid.

**(F2) The crate-name plan has an internal collision.** `§3.5 risks`
says "claim the name" for a binding crate;
`admin/notes/2026-08-16-clib-ffi-strategy.md:186-199` plans `tabnas/rust`
as the FFI binding's repo; the staged crate is named **`tabnas-clib`**.
So the bare `tabnas` crates.io name is currently claimed by nobody and
planned by two documents for two different artifacts. D2 assigns the
namespace once: `tabnas-clib` (the staged binding), `tabnas-spec` (M2a
loader), and the engine crate's name decided and registered before any
code — recommend reserving `tabnas` for the engine, since the binding
already ships under its own name.

**(F3) The LSP design is a named consumer for the least-pinned
capability.** `admin/notes/2026-08-17-unified-lsp-design.md` §4.4 makes
a real continuation API the *completion blocker* for the unified LSP,
and plans `tabnas_expected` across the C ABI. The engine-changes
document flags the continuations TS/Go divergence (`§5.8 changes`:
`['#ZZ']` vs `['#A']` for the same grammar and input, recorded nowhere)
as "zero fleet callers, so the cost of choosing is zero today". That is
no longer quite true: a design document now builds on the surface. The
adjudication moves up — it must land **before** the LSP work consumes
either answer, independent of the Rust schedule (D3.7).

**(F4) The demand question is narrowed, not retired.** `§Summary risks`
("retire the demand question this week") still has no in-tree answer:
no checked-in compiled-grammar artifact, and the named first user's
supply chain still carries the unverified abnf-CLI silent-lossy-JSON
defect (`§5.3 risks`). But the incumbent's shape moved: `tabnas-clib`
returns parsed values (per its staged description and conformance),
so v0.1's differentiators over the FFI route reduce to **the structured
diagnostic, wasm/no-cgo targets, and in-process embedding without a Go
runtime**. That is a thinner margin than the register priced. Phase 3
makes producing the consumer artifact — and verifying/fixing the abnf
defect — the demand test, and stop-condition 1 keeps its teeth.

**(F5) Citation staleness is now a mechanical hazard.** 79 commits have
landed since the newest document's measurement point, and the org's own
tooling (`ax-phantom-gates`) flags doc-named symbols that do not exist —
the series itself records two rounds of that class of repair. The
load-bearing citations were re-verified for this plan (§1.3 here), but
the four documents' `file:line` references should be treated as
*of-their-commit*; Phase 2h schedules one refresh pass, and no codemod
should be driven from a document's line numbers without re-anchoring
(the warning `§3.C2 changes` already carries).

---

## Part 2 — Implementation Plan

The sequence is: **Phase 0** (decisions, days) unblocks **Phase 1**
(apparatus, ≤1 day) and **Phase 2** (canonical repairs, TS/Go only,
~2–4 weeks alongside normal maintenance); **Phase 3** ships the already-
built FFI crate and the consumer artifact; **Gate G** then decides
whether Rust engine code is written at all; **Phases 4–6** are the
crates. Nothing in Phases 4–6 starts before Gate G passes. Every phase-2
item stands on its own merits with no Rust port — that is the test
`§4.1 risks` sets, and it is what makes the plan safe under a reversal.

### Phase 0 — Decisions (maintainer; days, not weeks)

The decision register. Each entry names its evidence and a recommended
answer; per ADR-13 each states its repair *direction* explicitly rather
than defaulting to a port.

| # | Decision | Evidence | Recommendation |
|---|---|---|---|
| D1 | **The #120 route: A1 or S0.** Amend the ruling on issue #120 accordingly (the set-without-run row flips from the ruled `4` to today's `3` under A1 — §1.4 here). | `§3.A1/A2 changes` (−12%, four regimes → one, zero fleet inheritance); `§5.1 strategy` (the S0 shape) | **DECIDED 2026-08-21: A1**, amendment recorded on #120. TypeScript hoists at `normalt`; Go follows at spec load; Go's five deletes are then deleted as unnecessary. |
| D2 | **Write the port ADR; assign the crate namespace.** Supersede ADR-12's no-native-ports premise for the *engine* (its numbered clauses, including the uniform-symbol contract, stand); record v0.1's scope line (`§5.1 risks`) and stop conditions as the ADR's reconsideration triggers; claim `tabnas`, `tabnas-spec` on crates.io (`tabnas-clib` per its staged plan), with checklist rows landing per ADR-12 clause 4; crate versions **independent from 0.1.0** — engine-implementing crates report the engine version they implement (`§3.5 risks`, `py/` precedent), the engine-agnostic loader reports its ABI template version — and the four-location version machinery gains no fifth member. | §1.5 F1/F2 here; `§5.3 risks` | Do it in one `admin` PR. |
| D3 | **The engine-changes §5 set**, each direction stated per ADR-13: (1) = D1; (2) option-merge classes B/C/D (class A is recorded with its `*string` repair — schedule the breaking bump per the DIVERGENCE.md note); (3) `Lex.next` IGNORE filtering — direction: Go moves, **with the `@tabnas/c` three-line filter landing in the same commit** (`§5.3 changes`); (4) C1 loud-or-silent; (5) `RuleDone` payload resolved-vs-static (zero subscribers exist; pick TS's resolved and pin); (6) `e`/`h` order (adopt Go's straight-line order, one TS assertion moves, shared fixture + DIVERGENCE row); (7) **continuations divergence — now LSP-blocking** (§1.5 F3 here); (8) matcher state slot (defer unless concurrent `parse(&self)` is a stated requirement — `yaml`'s 13 closure variables are the cost). | `§5 changes` | Rule (2)(3)(5)(6)(7) now — five of the six are free today and stop being free the moment a consumer exists. |
| D4 | **#130: the serialized-options leaf set S**, folding #142 (`rewind.history <= 0`: TS retain-nothing vs Go unbounded — on the DoS bound itself), #143 (ill-typed leaves: TS crashes, Go drops silently), #144 (`history: null` → `Infinity`). One ruling naming S, an exhaustiveness test over `Options` in both runtimes failing on any leaf not in S and not handled, and `Option<usize>`/`None`-is-unbounded as the Rust shape. | `§2.2 risks`; issues #130/#142/#143/#144 | Rule now; 1–2 days to land; genuinely blocks M2a. |
| D5 | **Resourcing.** A named second maintainer with Rust ownership, or a written support tier ("the crate may lag arbitrarily; it reports the engine version it implements"). | `§8.4 feasibility`; `§1.1 risks` item 2 | Must be answered in the D2 ADR — it is the only register item engineering cannot retire. |

*Status, 2026-08-21.* D1 **decided** (A1; amendment on #120). D2
**drafted** as ADR-17, proposed on admin#71 — merging that entry
constitutes acceptance, per the ADR-12 convention; it also carries D5 as
the written support tier, upgraded when a second maintainer is named.
D3's five rule-now items are **filed with proposed rulings**: the
overlay classes (#151), `Lex.next`/IGNORE (#152), the `RuleDone` payload
(#153), the `e`/`h` order (#154), continuations (#155); items (4) and
(8) stay deferred as the table says. D4's leaf-set ruling is **proposed
on #130**, folding #142/#143/#144. Each proposal awaits the maintainer's
confirmation on its own thread; nothing is implemented until confirmed.

### Phase 1 — Fix the measuring apparatus (≤1 day, before any perf-justified change)

From `§6.1 changes`, with one correction to it: (1) `rm -rf` before
`ln -s` in `ci/gate/run-gate.sh:29-31`, verify by `md5sum`, and wire the
same working-tree linking from `ci/bench/run-bench.sh`. The proposed
bench workflow **keeps its `npm i`** — a clean Actions checkout has no
`node_modules` and no built `dist`, so dropping the install (as §6.1
suggests) yields a workflow that fails before measuring anything — and
then overwrites the installed `@tabnas/parser` with the built
working-tree engine via that fixed linking, `md5sum`-verified;
installing and then silently measuring the *published* engine was the
defect, not installing. (2) Add a non-ASCII fixture to `ci/bench/genfixture.js` and
delete the two orphan scratch fixtures; (3) adopt the paired ABBA rig
with the **sign-flip protocol** and an adjacent same-fixture null as the
only decision-grade measurement; (4) re-run the decision set on
**Node 24** — every number in the engine-changes document is Node 22,
off-support. A1's −12% and C1's −2% must reproduce under (3)+(4) before
they are cited in a release note.

*Status, 2026-08-21.* (1) **Done** — `ci/lib/wire.sh` is now shared by
`run-gate.sh` and `run-bench.sh` so the two cannot drift; it removes the
destination before linking and proves the swap took. The `ln -snf`
no-op was reproduced first (exit 0, link buried inside the directory,
package still resolving to the published engine), as was the hole in the
first version of the guard — `readlink -f` normalises a dangling link and
a missing target to the same string, so a missing target passed silently
until an explicit check was added. The staged `bench.yml` **keeps** its
`npm i`, correcting `§6.1 changes`: a clean runner has no `node_modules`
and no `dist`, so dropping the install yields a workflow that dies before
measuring. (2) **Done** — `records-cjk-1mb.json`, literal UTF-8, zero
astral characters, so it isolates "not ASCII" from "not BMP"; sizing
moved to bytes, with all seven existing ASCII fixtures verified
byte-identical. **Review caught that the fixture was generated and never
run**: neither `run-bench.sh`'s TS loop nor `gobench` named it, so the
non-ASCII arm produced no timing data at all — coverage in appearance
only, which is the failure mode this repo cares most about. Both runners
now benchmark it. Wiring it also exposed that `bench.js` measured
`src.length`, i.e. UTF-16 code units: identical to the byte count for
every ASCII fixture and 33% short on this one, against a Go harness
reporting true bytes. Fixed, so the two runtimes' throughput on the one
non-ASCII arm is comparable. (3) **Done** — `ci/bench/abba.js` plus
`ci/bench/ab-compare.sh` implement the sign-flip protocol and refuse to
pronounce without a reversal clearing the session null. Each side loads
its own strict-JSON test grammar from `dist-test`, so the rig needs no
downstream checkout — a simplification over the isolated-trees approach
`§2.3 changes` describes.

**Two defects in the first version, both found in review, both real.**
The null ran the baseline against ITSELF *at the same path*, and
`abba.js` loads a slot with `require()`, which caches by resolved path —
so both null slots got the same module object where forward and reverse
get two. The null therefore excluded every artifact that exists only
because there ARE two graphs, and a band that is too narrow makes a
false EFFECT ESTABLISHED easier to reach. It now runs against a
byte-identical copy at a distinct path, and the difference is not
theoretical: on identical builds the same-path null read −0.41% on
`d_min` where the two-graph null on the same machine read **+1.42%**.
Second, the verdict was computed from `d_min` alone while `d_total` was
merely printed — so an allocation change, the one thing `d_min` cannot
see, could take a verdict from min-time noise. Both metrics now get a
verdict, and the rig says so plainly when they disagree instead of
picking.

Re-validated after the repairs, on `records-16kb.json`, 8 rounds:

| case | `d_min` fwd / rev / null | verdict |
|---|---|---|
| identical builds | +1.75 / +4.82 / +1.42 | UNRESOLVED both metrics, no flip |
| baseline slowed ~20% | −35.94 / +61.69 / −0.39 | EFFECT ESTABLISHED both metrics, candidate FASTER (−47.09% `d_min`, −39.61% `d_total`) |
| builds disagree on the parse result | — | refuses to time, exit 3 |

(4) **Partly** — the suite and the rig both run clean on Node
24 (the same two pre-existing `doc-examples` failures as Node 22). What cannot be done
yet is the part that matters: A1 and C1 do not exist as code, so there is
nothing to re-measure. That step belongs to Phase 2a/2b, and the rig is
what it must be measured with.

### Phase 2 — Canonical repairs (TypeScript and Go; no Rust)

Ordered; items within a letter-group are independent. Sources in
parentheses; fleet blast radius is as measured in the series unless
marked re-verify.

**2a — The config route (after D1).**
0. **Done** — this repo's ADR-14 executable register now exists:
   `test/spec/divergent.tsv` plus `ts/test/divergent.test.js` and
   `go/divergent_test.go`, fifteen rows over the five `DIVERGENCE.md`
   entries that a probe can reach. It departs from the `jsonic`
   precedent in one way that mattered more than expected: jsonic's
   `input → parse result` shape assumes a grammar, and this engine ships
   none, so rows carry a **probe** column naming which observation they
   make — `lex` (one selected token's fields) or `spec` (install a
   serialized `GrammarSpec`, then parse). The `spec` probe is what lets
   step 1 below stage a builtin-config row at all; a lex-only register
   would have served 2i and not 2a.

   Three properties were worth the extra work. Rows render through a
   **shared canonical form** — verbatim values, keys sorted by UTF-16
   code unit — because a renderer built on `%q` or `JSON.stringify`
   manufactures differences of its own, which is what #156 cost a review
   round over. Every group carries a **control row** where the ports
   agree, so a repair cannot be confused with unrelated breakage. And a
   **coverage gate** requires every `### ` heading in `DIVERGENCE.md` to
   be either a register group or a declared `notRegistered` exemption
   (one today: the fractional `rule.maxmul`, which needs a full value
   grammar no probe builds), failing in both directions so neither file
   can drift from the other.

   Validated by breaking it thirteen ways — a stale `ts` column, a stale
   `go` column, a *repaired* divergence, an unregistered entry, a new
   prose heading, a renamed heading, a missing justification, a
   duplicate name, an unknown probe, a short row, an undefined `@spec`
   reference and an unknown `show` field — each confirmed red on the
   runner it should be red on and green on the other.

   **A repo-wide trap fell out of that validation, and it defeats
   `test/AGENTS.md`'s own instruction to "check it runs by breaking a
   row on purpose".** `go test` caches a fixture read against the file's
   `stat`, not its contents: with the mtime pinned, a corrupted
   `divergent.tsv` stays `(cached)` and green even when the file's size
   changes. Every shared-fixture Go runner in this repo has the same
   exposure, and every `go test` invocation in the tree is cache-enabled.
   CI is unaffected — a fresh runner has an empty cache — so this bites
   exactly the person editing a fixture and re-running immediately.
   `test/AGENTS.md` now says `-count=1` at that step.
1. **Done** — both shapes are now staged in the register, and the
   divergence is **reproduced rather than predicted**. A two-rule
   function-free serialized spec (`top` matches `#NR #NR`, carries
   `k: {value$: {from: 1}}` and pushes `leaf`, which runs `@value$`
   bare) over the input `1 2 3 4` answers exactly what `§3.A2 changes`
   said it would:

   | grammar shape | TypeScript | Go |
   |---|---|---|
   | parent sets `k`, **runs** the builtin, pushes | `3` | `3` |
   | parent sets `k`, does **not** run it, pushes | `3` | **`4`** |

   The second row is the divergence, the first its control — and the
   control agrees for a reason that makes the split worse rather than
   better, so the row carries that in its justification: Go's five value
   builders delete their config key right after reading it, so *running*
   the builtin consumes the config. Remove the delete and the control
   row moves too.

   Three things this turned up. The divergence had **no
   `DIVERGENCE.md` entry** (`§3.A2 changes` said so; confirmed against
   the file), so one is written — as a defect with a ruling and a slot,
   not a deliberate split, since the coverage gate from step 0 requires
   every register group to name a real entry. `go/builtins.go:19-25`
   **asserted the opposite** — "Equivalent behaviour" — and now points
   at the entry instead; the plan had that rewrite in step 3, but
   leaving a knowingly false parity claim in the source while writing a
   divergence entry about it was not defensible, and it is comment-only.
   And the value renders identically through the register's canonical
   form despite Go producing a `float64` and TypeScript a `number`,
   which is what that renderer is for.

   Verified live in both directions: with the row edited to say Go
   answers `3` (what A1 will make true) the Go runner goes red and tells
   the reader to delete the row and the entry; with it edited to say TS
   answers `4` the TypeScript runner goes red. So when A1 lands in step
   3 the register forces its own cleanup rather than needing to be
   remembered.
2. **Done** — A1 in TypeScript. The eight config-reading builtins are
   factories; `normalt` binds each alternate's config into its action at
   load and takes the key out of `alt.k`, keyed on the closed builtin
   name set. The `k: {myTotal$: 1}` trap is avoided by construction and
   pinned. The probe family needed no carve-out at all: it reads and
   writes `r.k` (`pd_phase`, `pd_mark`) rather than per-alternate
   config, so it is simply absent from the bound set.

   One correction to the design as written: the "shadowed ref" guard it
   implies is not reachable. `grammar()` reserves the whole `$` ref
   namespace and throws on a user ref key containing `$`, so nothing can
   shadow a builtin. The check is kept as a cheap assertion against that
   reservation being relaxed, and says so rather than claiming a defence
   it does not provide.
3. **Done** — A1 in Go, and it closes the split. The same eight builtins
   take bound config; `bindBuiltinConfig` in `grammarspec.go` binds at
   spec load and `copyAltK` drops the consumed keys, nilling a bag that
   ends up empty. **All five `delete(r.K, …)` sites are gone** — they
   were containment for a design that no longer exists, and were
   themselves a third scoping regime (consumed-once against
   alternate-scoped), which is why the run-then-push shape used to agree
   for the wrong reason. `mapConfig` went with them, its last caller
   removed.

   Two resolvers needed it, not one: `ResolveGrammarAltStatic` is
   exported, bypasses `resolveGrammarAlt` entirely, and would have left
   any caller silently on the pre-A1 semantics.

   **The register did exactly what it was built for.** The moment Go's
   half landed, `a2-config-set-then-push` went red — Go now answers `3`
   where the row records `4` — with the message instructing deletion of
   the row *and* its `DIVERGENCE.md` entry. Both are gone; the entry is
   replaced by a forwarding address in "Repaired, and what replaced
   them", and the two shapes moved to
   `TestBuiltinConfigIsAlternateScoped` in both ports, as the plan said
   they should. That is step 0's mechanism forcing its own cleanup on
   its first real repair, without anyone having to remember.

   `go/builtins.go`'s header and `doc/value-builtins.md`'s config
   section are rewritten. The doc's note still cited #120's *original*
   rule-scoped ruling, which A1 amends — corrected, with the amendment
   named. Close #120.

   Fleet verified: the gate is green across parser, json and jsonic in
   both runtimes, including jsonic — the only repo with live builtin
   config declarations (`ts/src/grammar.ts:303` `array$ implicit`,
   `:386` `object$ implicit`).
4. The propagation fixture `test/spec/propagate.{fixture.json,tsv}`
   (`§5.1 strategy` item 5): `n`/`k` on push and replace, `u`
   exclusion — with the `@setval$` missing-key coercion divergence fixed
   or registered first, and the `test/AGENTS.md` carve-out sentence for
   engine-probe fixtures so the next reader does not delete it.

**2b — The post-action channel (#122 + the `alt.p`/`alt.r` ruling).**
C1's hoist of `_push`/`_repl`/`_back`/`_cons` into pre-action locals
(`§3.C1 changes` — decisive at 16 KB, ≈ −2%), the D3.4 loud-or-silent
choice applied, the function-form `b` resolution folded in, and the
`alt.p`/`alt.r` adjudication (Go's shape: the alternate is not a
post-action channel) with its fixture — the item `§5.1 strategy` item 3
insists must precede any signature change. Close #122. Correct
`go/rule.go:1176-1177`'s "Mirrors the TS ordering" comment when it
becomes true.

**2c — The narrowing wave (one engine major, fleet patches pre-staged;
after 2a+2b).** C2 (drop argument 3 from the five non-modifier types;
`AltModifier` keeps take-and-return), C3 (`StateAction` to
`(rule, ctx)`; drop the `this` binding), C4 (matchers declare
`starts`/`tins` — also fixes the live `toml` `.make()` crash), C5 (drop
`tI`; `pnt.token` de-documented as matcher surface), C6 (the normalising
span constructor; export Go's span-taking `Bad` and delete three fleet
clampers), C7 (freeze alternates' `n`/`u`/`k`/`g` at build; closes the
grammar-corruption route #121 from the modifier side), D11, D12. Fleet:
`expr` (port Go's alternate split), `toml` (one line + C4), `directive`
(~ten lines), `bnf`/`abnf` (`ActionsMap` narrowed, third parameter
renamed `next`). The wave ships together because the fleet peer-depends
on published npm and cannot be typechecked incrementally
(`§6.3 changes`); it must **not** land mid-port (`§6.5 changes`).

**2d — What the port must borrow.** B1: `Lex.src` read-only after lexing
begins; `json5`'s rewrite moves to the pre-parse slot (mechanical — the
spike was retired by measurement, `§3.B1 changes`); B2: state the token
span invariant, drop the `set src` accessor, fix the two literal-minted
tokens; B3's two free halves: split the token registry (`cfg.t`/`tI`)
off `Config` onto the instance, and make scan specs eager in both
runtimes (delete Go's three lazy getters, keep `DefaultLexConfig` for
`jsonic`). The full `configure()`-as-pure-builder rewrite stays a spike
(§2f).

**2e — The tail (`§3.D changes`).** The deletions: `need` (D1 — also a
live load-time divergence with Go's root set; add the shared root-set
assertion), `closeInfoCache` (D2 — the engine's only process-global
mutable cache), instance `Merge` (D3 — 1,461 lines, zero fleet callers,
keep `deshareMatchTokens`), the `text.modify` concat (D4 — one line,
plus the idempotence assertion). The declarations: shallow-snapshot
propagation (D5), lookahead bound (D6), the `pid == nid` seeding fixture
(D7 — the guard `get_disjoint_mut` depends on), three-bags-and-why (D8),
`@SKIP`/undefined as the presence marker (D9 — with the fixture driven
through the options pipeline), `Info` out-of-band (D13), `schema/options.json`
(D10, with both exhaustiveness gates) and FuncRef slot restriction (D11,
with the wave). D14 (`ScanSpec.fallback` as data) lands after Phase 1's
non-ASCII fixture exists to measure it.

**2f — Spikes (cannot be judged without being built; `§6.4 changes`).**
`configure()` as a pure builder then freeze `Config`; recovery
resume-as-position; the relex save-point value; `@push$` republish
contract sentence; `TabnasError` snapshot-at-raise. None gates the port;
each is a standing card.

**2g — Conformance machinery to N runtimes (`§5.2/5.3 feasibility`,
`§2.4 risks`).** Generalise `nonParity` (`go/spec_registration_test.go:27`
is still `map[string]string` — verified) to per-runtime exemptions or
move the gate to `ci/gate/`; restructure `schema/error-codes.json`'s
`goOnly` to a per-entry `runtimes` array (~150 lines across generator,
registry, both schema tests, README — a declared breaking change to the
registry file); decouple the registry's embedded version from the engine
version (the largest single item; sequence last and alone,
`§5.1 strategy` item 4); extract the one TSV loader spec and fix the Go
runners to match (`§5.1 feasibility`); add `assert ran == N` row-count
checks to every runner in both runtimes — with N taken from a **fresh
census at implementation time**, never from `§4.1 risks`' 265-row
table, which is already stale: the corpus stands at 297 data rows today
(diagnostic grew to 13, the UTF-8 error set to 11, and the two
`lex-text-*` fixtures added 23), and the assert values move with every
fixture change by design.

**2h — Docs and registers.** The four stale porting-guide claims
(`§2.4 risks`: `Forced`, budget-under-recovery, tokdump's Sub rationale,
match-token "No behavioural effect" — with a `Forced == true` assertion
so the first cannot regress); #116 (nine-vs-ten codes in `AGENTS.md`,
plus the dead `invalid_lex_state` constant); the `AGENTS.md`
`build.yml` reference; the one-page `doc/lex-matcher-contract.md`
(`§2.1 risks` — the eight capabilities, speculate's rollback boundary,
cursor monotonicity, the "no Context writes before commitment"
sentence); the match-token gating DIVERGENCE entry + two-row fixture;
`Lex.next`/IGNORE per D3.3; declare the ten `_*` per-parse properties as
real fields on `Context`/`Rule` (`§2.4 risks` — half a day, converts the
Go-source archaeology into a struct read); a citation-refresh pass over
the five Rust documents (§1.5 F5 here); align `DIVERGENCE.md`'s preamble
with ADR-13 (its opening still states the pre-ADR-13 law form that the
ADR amends); and scope `schema/diagnostic.schema.json`'s `version`
parenthetical ("equals the package version of the runtime") to the
lockstep runtimes — see Phase 6's versioning note.

**2i — Options rulings landed (after D3/D4).** The B/C/D merge-class
fixtures driven through the **options pipeline** (never bare
`deep`/`Deep` — `§2.2 risks`' correction), the three fleet workarounds
deleted as proof; the class-A `*string` repair scheduled as the breaking
bump the DIVERGENCE entry calls for; #119 (panicking validators → error
values) and #113 (validate `p`/`r` rule references at build) closed; the
serialized `lex.match` TS-honours/Go-drops split adjudicated under D4's
ruling; recovery sync-token minting moved to parse start
(`§2.4 risks` — ~15 lines, also a data-race fix); modifier-order
determinism in Go (`map` → ordered), with the post-construction Config
mutation question ruled per D3's Config-mutability decision.

**2j — clib closure.** #117: implement the header's `value` field in
`parser`'s grammar-agnostic `go/clib/core.go:115` (the `*OrderedMap`
marshal exists; the key-order surface it creates is now covered by
ADR-15), keeping the `version` key `py/tabnas.py:141` reads
(`§7 feasibility`'s paired-change warning).

### Phase 3 — Ship the FFI lane and the demand test

1. Seed `tabnas/rust` from the staged `tabnas-clib` (maintainer,
   `seed-repos.sh --apply`), wire its registry presence into the release
   checklist per ADR-12 clause 4.
2. **Produce the consumer artifact**: a checked-in, `--full --strict`
   compiled grammar from the BNF/ABNF/GBNF family, loaded by
   `tabnas-clib` in a conformance test — after verifying and fixing the
   abnf CLI default-output defect (`§5.3 risks`: `JSON.stringify`
   silently dropping function values on the path with neither guard;
   one build to verify, two small fixes, one round-trip fixture).
3. Write down B's ceiling as shipped (structured diagnostics,
   continuations, recovery, subscribers and custom actions do not cross;
   values now do).

This phase is the demand experiment `§Summary risks` demands: it serves
the named first user with ~500 lines instead of ~10–14k. What it cannot
serve — wasm, no-Go-runtime, in-process embedding, the structured
diagnostic — is exactly Gate G's condition 1.

### Gate G — before any Rust engine code

All five, in writing, per `§5.3 strategy` — current status:

| # | Condition | Status at `abfed2c` |
|---|---|---|
| 1 | A named consumer `libtabnas` + a serialized spec provably cannot serve (wasm / no-Go-runtime / in-Rust authoring — the third reopens the plugin question and must say so) | **Open** — Phase 3 is the experiment; the LSP design names the C ABI, not Rust, for its engine access |
| 2 | The differential-tier entry cost paid or waived in writing (`json` leg free via `json-core`; the relaxed `jsonic` leg has no function-free artifact — waive it explicitly or produce one) | **Open** |
| 3 | The two-runtime machinery generalised (Phase 2g) | **Not started** — `nonParity`/`goOnly` verified still binary |
| 4 | The unpinned surface **landed**: #120-as-D1, #122, the `p`/`r`-channel fixture, M0.2 orderings, the propagation fixture, #130's exhaustiveness test. (`pos` ✅, regex dialect ✅, key order ✅ via ADR-15 — already done, §1.3 here) | **Partial** |
| 5 | D5 answered in the ADR | **Open** |

### Phase 4 — M2a: `tabnas-spec` (~1–2k lines, zero parity obligations)

Loader + validator per `§5.4 strategy`: `schema/grammar.schema.json`
validation, `@name$` resolution against the 16 builtins, the `v` gate
against `BUILTIN_SCHEMA_VERSION`. Under D1=A1 the `enum Act` carries
**typed payloads** bound at load (`Node {…}`, `Object {implicit}`,
`Value {from}` — `§3.A1 changes`), which is simpler than the strategy
document's payload-free variant and validates config shape at load, a
capability S0 forecloses (`§5.4 strategy`'s "no static validator can
associate them" paragraph dissolves with the bag). Record the two
honesty notes that survive: the `@ref`-bag narrowing against the two
serialized-`e` tests, and that `Cow<'static, str>` — never a closed
enum — is the diagnostic `code` type (`§2.1 risks`).

### Phase 5 — M2b: the engine, by walking skeleton

**Porting guide first.** Write `doc/rust-porting-guide.md` from the
Wave-B decisions (`§4.2 risks`), all of which are settled by probes in
the series — restated with the Part-1 corrections: span token +
`Arc<str>` (B1); the `Lex`/`Ctx` split with `pending`/`end` hoisted
(B2); `SrcIdx` + `clippy::string_slice` from day one (B3); eager Config,
hooks as `fn(&Config, &mut Lex)` (B4); the four-state `Ov<T>` overlay
(B5, gated on D3.2); subscribers `&'g [Box<dyn Fn(&mut Ctx, &mut Token,
RuleId)>]` hoisted off Ctx (B6 — the parity dumper itself needs this);
Context seeding / `Token.use` typed API (B7); arena retention measured
then decided, generational indices if reuse — remembering recovery's
two extra reachability paths (B8, `§2.4 risks`); the matcher
`ScanResult` contract written not built (B9); `AltCond` re-entrant
(§1.4 here); actions `Fn(&mut Ctx, RuleId) -> Result<Effect, Fault>`
with `Effect` = the S4 error/backtrack channel; `Result` primary +
`catch_unwind` documented second layer, worklist rewrites for the
recursive walks, `parse(&self)`, `Send + Sync` behind a feature-gated
trait alias (§3.7/§3.8); numbers via `num-bigint` (§4.4); UTF-8 policy
decided at the door per §4.5 (recommend (b): `&str` in, invalid UTF-8
rejected — the DIVERGENCE "Not divergences" entry then stands
untouched).

**Slices, staged on engine reach (`§4.3 risks`):**
1. **The lexer** — joins the parity contract grammar-free through the
   three `lex-*` fixtures (§1.3 here) and a `tokdump` spec mode;
   retires B1–B4/B9.
2. **`json-core`** — land `test/spec/json-core.fixture.json` + third
   runners in both existing suites first (Wave A item 1, `§4.1 risks`,
   including the ten-line Go comparator fix); then the Rust leg: 55
   value rows + 10 diagnostic rows, the only lane that executes the
   value builtins.
3. **`probe-grammar` + `eager-literal`** — the re-entrant bucket
   (`@probeDecide$` → rewind) and eager matchers, already dual-pinned.

**The differential harness runs from day one** (`§4.4 risks`): star
topology (TS↔Rust only), the third redirect in `run-parity.sh` (~10
lines), in-process dumpers, seeded mutation including **error-path**
corpus, mismatches bucketed by severity with known divergences in a
data file — ASCII-only mismatches are bugs by default. The `jsonic`
corpus is out of scope for v0.1 unless Gate G condition 2 produced an
artifact; say so in the D2 ADR.

**Rules of conduct for the branch:** consume only rulings landed as
code + fixture on a tagged release; rebase on tags, never `main`
(`§3.3 risks`); no language-changing engine ruling lands mid-port
(`§6.5 changes`); report per-file function coverage against the
`§2 risks` table as the primary progress number, utility rows as
value-model progress only.

### Phase 6 — Integration

CI as `ci/rust/` + a repo-local workflow, staged for maintainer
application per ADR-8, never a change to the shared `polyglot-ci.yml`;
the two stale harness paths fixed first (`§3.5 risks`). Registry and
honesty-gate entries flow from Phase 2g's generalisation. Release: the
crate lane per D2 (independent version, engine-version report,
`repo-tests` feature or `OUT_DIR` copy so the packaged crate's tests
survive unpacking — decided before the first `cargo package`). One
distinction D2's independence needs stated, because
`schema/diagnostic.schema.json` defines the diagnostic `version` as "the
engine version (equals the package version of the runtime that produced
the diagnostic)" — an equality that holds only while every runtime
versions in lockstep: the Rust engine emits the **engine-contract
version it implements** in diagnostics (keeping cross-runtime
diagnostics comparable and the registry gate meaningful), its package
version stays independent, and the schema's parenthetical is scoped to
the lockstep runtimes in the 2h doc pass. The crate's engine-contract
constant is a compatibility declaration that lags deliberately — the
crate's registry/version tests compare *it* against
`schema/error-codes.json`, not the package version — and it is not a
fifth member of the synchronized version set, which is exactly the
difference between declaring compatibility and joining the bump
machinery. The bench arm ships **only after** its ADR
(`§5.5 feasibility`'s canonicality-inversion hazard).

### Out of scope, restated

The imperative plugin tier (M3) is not scheduled, and `§6 strategy`'s
"what not to do" list is adopted as-is — no `Rc<RefCell>`
transliteration, no branding lifetimes, no unversioned serialized ABI,
no in-place `AltModifier`, no early bench arm. Custom lex matchers stay
out of v0.1; a consumer request for one before v0.1 ships is
stop-condition 9, answered "no". wasm-via-cgo remains a non-goal
(ADR-12); a wasm need is Gate G condition 1 evidence for the *native*
engine instead.

### Stop conditions

`§6 risks`' ten are adopted with the baseline updated to `abfed2c`:
condition 4's PR queue is at 0 (re-arm the threshold, don't retire it);
condition 3's ruled-but-unimplemented count is 2 (#120, #122) and its
30-day clock starts when the port branch opens, so Phase 2a/2b are the
fuse; conditions 1 and 5–10 unchanged. One addition: **G-regression** —
if any Gate G condition later becomes false (the named consumer
withdraws, the second maintainer leaves), the port pauses at its current
slice rather than coasting.

### Estimates, honestly

From the series' own measurements, unchanged where nothing moved:
Phase 0 days; Phase 1 ≤1 day; Phase 2 roughly two to four weeks of
maintainer-plus-agent time at the measured per-item costs (A1 and the
wave are the two large items; most of 2e/2h are hours each); Phase 3
about a week (the crate exists; the artifact and the abnf verification
are the work); M2a 1–2k lines; M2b **10–14k lines, 4–6 engineer-months
solo** (`§7 feasibility`, option C's band — v0.1 excludes `Merge` and
recovery, so `§1.1 changes`' "one month back" mostly does not apply to
this scope). The maintenance tax after shipping: `§6 feasibility`'s
measured 1.76x aggregate at two runtimes is the floor the third one
raises; D5 is what makes that number survivable or not, and no estimate
here changes it.
