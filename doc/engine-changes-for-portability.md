# Engine Changes for Portability

Fourth in the series after
[`doc/rust-port-feasibility.md`](rust-port-feasibility.md),
[`doc/rust-callback-porting-strategy.md`](rust-callback-porting-strategy.md)
and [`doc/rust-port-risks.md`](rust-port-risks.md).

The brief has widened. The port is going ahead, and algorithmic changes
and different approaches to capabilities are in scope provided they do
not impact performance. So the question this document answers is not
"how do we reproduce this engine in Rust". It is: **what should the
canonical TypeScript engine change, so that a Rust port is cheaper, and
which of those changes cost nothing at runtime?** TypeScript is canonical
(`AGENTS.md:26`), so a change here is a change to the contract every
runtime follows.

Citation convention as before: a bare `§3.4` is a section of the
feasibility report, `§2.3 strategy` is the callback strategy document,
`§2.1 risks` is the risk register, and sections of *this* document are
written `§4 here`.

> **Provenance.** Everything below was measured or probed against the
> tree at `94c57b6`. Three classes of claim carried in from earlier
> sweeps did not survive re-measurement and are corrected in place:
> the benchmark resolution floor (§2.3 here), the state of the
> post-action redirect channel (§3.C1 here), and about a dozen
> `file:line` citations that were off by up to nine lines. Every
> number attributed to "measured here" was produced by the harness
> described in §2.3; numbers carried over from an earlier sweep are
> marked *(carried over)* and should be treated as directional until
> re-run under the sign-flip protocol. **All timings are Node
> v22.22.2, which is off-support** — `ts/package.json` declares
> `engines.node` `">=24"` and CI runs Node 24.

---

## 1. Summary

**Three changes are worth making. The rest is a tail.** The candidate
sweep behind this document produced about forty peers, which is the
wrong shape: two of the three items below are larger than the whole tail
put together, and one of them is mutually exclusive with a milestone the
strategy document already schedules.

**(1) Resolve builtin config at grammar-load time and take it out of the
keep bag.** The eight config-taking `$`-builtins read their config from a
string-keyed bag at invocation time; hoist it into a closure bound at
`normalt` and the key never enters `rule.k` at all. Measured here on
`jsonic/text-1mb.jsonic`: the parse propagates **123,174 keep-bag keys
per megabyte, every one of them `object$`, and nothing ever reads it
back**. Removing that propagation is **−10% to −13%** with the sign
flipping cleanly in both slots, against a null of ±2.5% on the same
fixture, and **exactly nothing** on a grammar that declares no builtin
config. It also dissolves candidate (d): there are not two config
lifetime regimes to unify, there are four, and they produce different
output in the two runtimes today for a function-free serializable
grammar. This is the highest-value change in the document, and §3.A2
explains why it is also the one that unblocks (3).

**(2) Make the two things a Rust engine must borrow — the parse source
and the built `Config` — immutable by construction.** `Lex.src` is a
public field and `@tabnas/json5` replaces it *mid-lex* in both runtimes
(`json5/ts/src/json5.ts:342`, `json5/go/json5.go:520`); `configure()`
re-mutates the same `Config` object on every `options()` call. Neither is
a porting nicety. They are the two facts that decide whether `&'g Config`
and a shared source handle are sound by construction or by convention,
and the first of them was not on anyone's list. The source half is one
repo and two files. The config half is a spike, not a four-line change.

**(3) Narrow the callback surface to what the fleet actually uses, in one
release wave, strictly after (1).** Hoist the post-action routing into
locals; drop argument 3 from the five non-modifier callback types; narrow
`StateAction` to `(rule, ctx)`; drop the `tI` matcher argument; make
matchers declare their capabilities instead of the engine keying on the
registration name. Fleet cost for the whole wave is **three repos and
about a dozen lines**, plus one genuine piece of judgement work in
`@tabnas/expr` for which Go already ships the answer.

### 1.1 The honest total

These changes do not move the feasibility report's 15-18k non-test lines
or its 7-10 engineer-months by much. What they remove, concretely:

| Removed | Size |
|---|---|
| Instance `Merge` — zero fleet callers in either runtime, and a dedupe key with no Rust spelling | ~2,000 Rust lines + ~1,500 test lines |
| Three `Node` enum variants and the `NodeMapSet`/`NodeListAppend` type-switch layer, by declaring `Info` out-of-band (TypeScript's semantics) | ~6 match sites, one dispatch layer |
| The `Token<'s>` lifetime question, which was never real (§4.2 here) | a design week |
| Six items off M0's adjudication list, because the canonical runtime stops having two answers | days each |

Call it **one month of the seven to ten, and most of that is `Merge`**,
which the risk register had already demoted to a scope decision. The
value is not in lines. It is that roughly a dozen "a third runtime will
invent a third answer" hazards become contract, and that the port stops
inheriting a snapshot.

### 1.2 Two of the three are not port work

Change (1) is TypeScript being behind Go: Go already avoids most of the
propagation cost because its five value builders `delete` their config
key before the push/replace `K`-copy (`go/builtins.go:248`, `:269`,
`:285`, `:302`, `:340`, with the rationale at `:233-238`). And the
divergence it fixes is ruling #120, unimplemented, with `go/builtins.go:24`
asserting "Equivalent behaviour" where there is none.

Change (2)'s source half is a shipped plugin writing a public field
mid-parse in both runtimes.

Both would be made with no Rust port at all. Billing them to the port
would be dishonest, and it would also park them behind a project that
may not happen. They should be justified and landed on their own merits.

---

## 2. The Constraint, and How It Was Tested

"No performance impact" has to mean measured. Three things about the
measuring apparatus have to be said before any number is quoted.

### 2.1 `ci/bench` cannot see an engine change

`ci/bench/run-bench.sh` does no dependency wiring at all — it computes
`ROOT` from `TABNAS_ROOT` and runs `bench.js`, which requires `json/ts`
and `jsonic/ts`. Those resolve `@tabnas/parser` from their own
`node_modules`. `ci/gate/run-gate.sh` *does* wire, at `:29-31`:

```sh
link_ts_dep() { # link_ts_dep <repo-ts-dir> <scope-name> <target-dir>
  mkdir -p "$1/node_modules/@tabnas"
  ln -snf "$3" "$1/node_modules/@tabnas/$2"
}
```

Against an existing **real directory** — which is what `npm i` leaves —
`ln -snf` does not replace it. Reproduced here in a scratch tree: the
command exits 0, creates `parser/target -> …` *inside* the directory, and
the package still resolves to the old `dist`. The failure is not
hypothetical: `/workspace/tabnas/c/ts/node_modules/@tabnas/parser/ts ->
/home/user/parser/ts` is sitting in the fleet checkout right now, which is
that `ln -snf` having fired against a real directory.

Two earlier sweeps concluded from `readlink -f` plus a `0.8.10` version
string that the harness "benchmarks the published engine". On this host
that conclusion is false today — `md5sum` of `ts/dist/rules.js` and both
fleet copies is `6ebf99d8cfec6292e8fe1334bb83c6fa`, byte-identical,
because an earlier agent copied the working tree in. Which is precisely
why `readlink`+version is not a valid test, and why the "we quantified
the drift and it is nil" finding had no discriminating power. **Report
the structural defect, not today's fact**: the harness has no wiring, and
the gate's wiring is a silent no-op. Fix: `rm -rf` the destination before
`ln -s`, verify by `md5sum`, and wire from `run-bench.sh`.

The proposed `ci/workflows/bench.yml` makes it worse — it runs `npm i` in
each package (both declare `"@tabnas/parser": "*"`) and then calls
`run-bench.sh`, which wires nothing.

### 2.2 There is no gate, and there was never meant to be one

`.github/workflows/` holds `ci.yml` and `release.yml` and no bench job.
`ci/workflows/bench.yml` is a proposed workflow on `workflow_dispatch`
plus a weekly cron, artifact-only, whose own header says it "NEVER gates
a merge". `ci/bench/run-bench.sh`'s header says numbers are advisory and
must never hard-gate absolute thresholds. So "no performance impact" has
never been an enforceable claim in this repository, by design. Every
verdict below is a human reading numbers, and the numbers have to be good
enough to read.

### 2.3 The floor is not ±0.5%, and the protocol that works is the sign-flip

Earlier sweeps built a paired in-process ABBA rig — two engine builds in
one process, `A,B,B,A` per round, min-of-N per block — calibrated its null
with *one* pair of identical build directories, and reported a resolution
floor of ±0.5%. A wider null does not support that.

Six identical-build runs measured here on `text-1mb.jsonic`, 12 rounds ×
3 inner, both slot orders:

| slot order | `d_min` | `d_TOTAL` | B wins |
|---|---|---|---|
| A/N | +0.76% | −2.26% | 8/12 |
| A/N | −0.02% | −0.62% | 5/12 |
| A/N | −2.11% | −2.53% | 9/12 |
| A/N | −0.63% | −0.37% | 5/12 |
| N/A | −0.65% | −1.19% | 8/12 |
| N/A | −2.49% | −0.77% | 8/12 |

Range 3.25 percentage points, mean −0.86%, with a visible systematic bias
toward whichever build sits in the B slot. On `records-16kb.json`, 25
rounds × min-of-20 after 60 warmups, the same null gives `d_min` +0.11 /
−0.89 / −0.70 with B-win rates of 13, 20 and 17 of 25 — better, but
not ±0.5%, and the win rate alone spans 52% to 80% with
*byte-identical builds*.

**The fix is cheap and it is what every verdict in §3 uses: run the
candidate in both slots and require the delta to reverse sign.** A real
effect flips; a slot artifact does not. Where the sign flips, the effect
is established regardless of the null's magnitude, and the honest point
estimate is the geometric mean of the two directions. Where it does not
flip, the candidate is *unresolved*, and this document says so rather
than reporting the favourable direction.

Two consequences worth stating plainly. First, every candidate previously
certified "free within a ±0.5% floor" is unresolved on the evidence that
certified it — that is C3, C4, C5, C7 and the `rewind.history`
normalisation below, all of which are still worth landing on their
*removal* argument rather than on a number. Second, the practice of
discarding large positive outliers as "known JIT artifacts" while keeping
negative ones biased every earlier mean toward the candidate.

The shipped harness is worse than either rig, and its variance is
itself unstable. Three consecutive fresh-process runs of
`ci/bench/bench.js` on `records-16kb.json` measured here gave medians
4.446 / 4.102 / 4.108 ms — an 8.4% spread between runs, and a 1.93x
spread *within* one run (p5 3.447, p95 6.656). Earlier sweeps of the
same command reported between-run spreads of 6% and 25%. Whatever the
figure on a given afternoon, it cannot resolve anything in this
document.

### 2.4 The baseline, and one correction to it

`npm test` in `ts/` is **388 tests, 386 pass, 2 fail**, and both failures
are in `ts/test/doc-examples.test.js` — `MODULE_NOT_FOUND` from a missing
sibling checkout. `ts/test/errs.test.js` is green. An earlier sweep
recorded the two failures as pre-existing `errs.test.js` failures and
costed a candidate that moves five assertions in that file against a
baseline it described as already red. It is not.

Throughput on this host, in-process, min-of-20 after warmup, working-tree
engine:

| grammar / fixture | bytes | min | MB/s |
|---|---|---|---|
| `json` / `records-16kb.json` | 16,509 | ~3.3 ms | ~5.0 |
| `json` / `records-1mb.json` | 1,048,587 | ~345 ms | ~3.0 |
| `jsonic` / `text-1mb.jsonic` | 1,048,666 | ~500 ms | ~2.1 |
| `jsonic` / same, with §3.A1 | 1,048,666 | ~447 ms | ~2.35 |

The "~3 MB/s engine throughput" anchor used in the earlier documents is
not reproducible across statistics and drivers. The same fixture on the
same host, same afternoon: 3.5-3.8 MB/s by `ci/bench`'s median, ~5.0
MB/s by min-of-20 in-process; the three earlier sweeps reported 3.03,
4.52 and 5.58 MB/s. Do not use it to normalise anything; §2.1 risks' Rc/Arc
token-handle cost of "0.3-1.4% of the engine's 300 ns/byte" moves by
about 2x depending on which figure you take.

### 2.5 The fixture matrix has a hole

`ci/bench/genfixture.js` declares seven fixtures and none is non-ASCII;
the escape-dense fixture uses escape *sequences*, which are ASCII bytes.
`ci/bench/fixtures/` currently also contains `nonascii-1mb.json` and
`asciictl-1mb.json`, which appear nowhere in the generator — they are an
earlier agent's scratch, and the fixtures directory is gitignored, so the
gap is invisible to review. This matters concretely: `ScanSpec.fallback`
fires 0.686 times per character on CJK input and zero times on ASCII
*(carried over)*, so a hot path on real non-ASCII documents cannot be
seen by the current matrix at all.

### 2.6 What could not be measured

Five items cannot be judged without being built, and saying so is the
honest answer: recovery resume-as-position (touches the main loop in both
runtimes, and recovery has zero shared-fixture coverage to change under);
the relex save point and the matcher skip-vs-speculate saving (`lex.relex`
is off in every shipped grammar and its only fleet consumer is not
buildable here); Go's `@push$` one-level republish; the `TabnasError`
snapshot-at-raise; and `configure()` purity. They are spikes, listed in
§6.

---

## 3. Recommended Changes

Four groups. **A** is the one change with a large measured number. **B**
is the group that decides whether the Rust design is sound by
construction. **C** is the signature narrowing, which is one release wave
and must come after A. **D** is the tail: cheap, mostly deletions and
declarations, individually unremarkable.

Each entry states the change in TypeScript terms, why it helps a port,
what was measured, the fleet blast radius by name, and — because two of
the three headline items are not port work — whether it stands on its own
merits with no Rust port at all.

### A. Builtin config

#### A1. Bind builtin config at grammar-load time; take it out of the keep bag

**Change.** In `normalt` (`ts/src/rules.ts`, immediately before the
action ref is resolved), hoist every `alt.k.<name>$` whose `<name>` is a
config-taking builtin that this alternate's own `a` actually runs into a
closure bound to that builtin, and delete the key from a fresh copy of
`alt.k` (null the bag if it empties). The eight config-reading builders
at `ts/src/builtins.ts:127`, `:138`, `:170`, `:238`, `:249`, `:263`,
`:273` and `:305` become factories — `mkNode$`, `mkCapture$`, `mkFold$`,
`mkObject$`, `mkArray$`, `mkKey$`, `mkSetval$`, `mkValue$` — with
`BUILTIN_REFS` keeping a default-config instance of each, so a grammar
naming a builtin with no config is unchanged. The probe family is
deliberately excluded: its `k` entries (`pd_phase`, `pd_mark`, `pd_d` —
`ts/src/builtins.ts:189-190`, `:209`, `:213-215`) are parse *state* read
on a later rule after a replace, and must keep propagating.

**Implementation note, and a trap.** Key the hoist off the **closed
builtin name set**, not off a `$` suffix. The `$` namespace is reserved
for *ref* keys only, enforced at `ts/src/tabnas.ts:733-741`
(`if (key.includes('$'))` → "'$' is reserved for engine builtins"), and
nothing validates `k`. Probed here: a grammar declaring
`k: { myTotal$: 1, plain: 2 }` on a push alternate delivers
`{"myTotal$":1,"plain":2}` to the child today. A suffix test would
silently drop state for any grammar that happens to end a `k` key with
`$`, and "byte-identical on jsonic" would not catch it, because jsonic
has no such key.

**Why it helps a port.** Config stops being a runtime lookup in a mutable
string-keyed bag and becomes static per-alternate data validated once at
load. `§Summary strategy` says a Rust `enum Act` has "no inline struct
payload either" precisely "because config is read from `r.k` at
invocation time"; after this change `enum Act` carries a typed payload —
`Node { init, rule, kind, nterms }`, `Object { implicit }`, `Value { from }`
— dispatched by `match`. No `dyn Any` on the parse path, no per-builtin
delete semantics to reproduce, and — this is the part that matters for §C
— **the tree and value builders stop needing argument 3 at all**.

**Measurement.** Instrumented here on the built engine, counting entries
to both keep-bag propagation loops (`ts/src/rules.ts:662-671` push,
`:686-695` replace):

| grammar / fixture | push+replace events | events carrying a `k` bag | keys propagated | histogram |
|---|---|---|---|---|
| `jsonic` / `text-1mb.jsonic` | 123,175 | 123,174 | 123,174 | `{ object$: 123174 }` |
| `json` / `records-1mb.json` | 132,025 | 0 | 0 | — |

One key per event, always the same key, always builtin config, and
nothing in TypeScript ever reads it back off `rule.k`. The loops
materialise the child's `k` object only when there is content
(`(nk ??= next.k)[kn] = pk[kn]`), so removing the key removes **123,174
object allocations per megabyte**, not merely 123,174 loop iterations.

A/B: two fully isolated trees (each with its own
`node_modules/@tabnas/{parser,json,jsonic}`), one process, `A,B,B,A` per
round, 12 rounds × 3 inner after 5 warmups, variant = both propagation
loops skipping the closed builtin-config name set. Sign-flipped:

| direction | `d_min` | `d_TOTAL` | B wins |
|---|---|---|---|
| forward (base in A) | −8.46% / −9.83% / −11.16% | −7.65 / −10.85 / −10.52 | 12/12 each |
| reverse (variant in A) | +14.40% / +13.54% / +15.75% | +13.11 / +11.31 / +11.44 | 0/12 each |
| null (§2.3) | −2.49% … +0.76% | −2.53% … −0.37% | 5-9/12 |

Clean reversal; geometric estimate **−12%**. Output byte-identical at
1,015,563 characters in every run. Control on `json/records-1mb.json`,
where the grammar declares no `k` at all: forward +1.65%, reverse −0.92%
— *no* sign flip, i.e. no effect, exactly as a never-executed path should
behave.

The figure is a **lower bound**. The probe still pays the per-key set
membership test, the `rule.k = Object.assign(rule.k, alt.k)` at
`ts/src/rules.ts:605` that materialises `rule.k` in the first place, and
the `alt.k` object itself. The candidate removes all three.

**Fleet.** Six TypeScript declaration sites in three repos —
`bnf/ts/src/compiler.ts:2759`, `:2764`, `:2772`;
`jsonic/ts/src/grammar.ts:303`, `:386`;
`multisource/ts/src/multisource.ts:295` — plus three Go emit sites at
`bnf/go/emit.go:471`, `:497`, `:560`. Every one is co-located with the
action that reads it. Zero fleet code reads `r.k.<builtin>$` at runtime.
`bnf` already strips these keys on round-trip (`bnf/ts/src/spec.ts:38-40`, `:84`;
`bnf/go/compile.go:41`, `:515-517`), so it already agrees with the
premise. The wire format does not change and the hoist happens on load,
so no generator output changes and none of the four generator repos needs
regenerating — they keep no checked-in grammar artifacts anyway.

**Stands alone?** Yes, unambiguously. Go already gets most of this for
its five value builders. TypeScript is behind, and the gap is a 12%
throughput difference on a jsonic-class grammar.

#### A2. Rule the builtin-config lifetime, and pin it with a shared fixture

**This is a parity bug, not an API-shape question**, and framing it as
candidate (d) framed it — "two lifetime regimes, unify them so a third
runtime cannot invent a third semantics" — understates it twice over.

There are four regimes, not two:

| builder family | TypeScript | Go |
|---|---|---|
| `@node$`, `@capture$`, `@fold$` | alternate-scoped (`alt.k`) | rule-scoped, **inherits** |
| `@object$`, `@key$`, `@setval$`, `@value$` | alternate-scoped | rule-scoped, **consumed once** |
| `@array$` | alternate-scoped | inherits or consumes, depending on the unrelated `ListRef` option (`go/builtins.go:266-273`) |

And they produce **different output for the same function-free,
serializable grammar**. The strategy document's own measurement, on a
two-rule spec where the parent carries `k: { value$: { from: 1 } }` and
pushes a child that runs `@value$` bare: TypeScript answers `3` (the
config did not reach the child), Go answers `4` (it did).

Go's source asserts the opposite, at `go/builtins.go:19-25`:

> Parity note vs TS: … Go's builtins read their config from `r.K`.
> Equivalent behaviour; the config keys (node$/capture$) ride in `r.K`
> and propagate to children, which is harmless for the bounded set the
> compiler emits.

It is not equivalent, and "harmless for the bounded set the compiler
emits" is a statement about `@tabnas/bnf`'s output, not about the
contract. There is no `DIVERGENCE.md` entry — checked, the file has three
deliberate-divergence headings and none of them is this — and no
`go/doc/differences.md` entry, and none of the eleven `test/spec/*.tsv`
fixtures touches `n`, `u` or `k`, so `ci/parity` cannot see any of it.

**Under A1 the question dissolves.** Config never enters `rule.k`, so
there is exactly one regime: the alternate's own declaration, resolved at
grammar load. That is TypeScript's current semantics, which is what
`AGENTS.md:26` requires anyway. Go's five deletes become unnecessary
rather than load-bearing, and the `@array$`/`ListRef` regime disappears
with them.

**Order of work.** Land the shared fixture *first*, on the run-then-push
and set-then-push shapes, so that A1 is proved by a fixture that was red
before it. Then A1. Then delete the Go deletes.

**Stands alone?** Yes. It is ruling #120, implemented the cheap way
instead of the expensive way.

### B. What the port must borrow

#### B1. Declare the parse source immutable; add a pre-parse transform

**Change.** `Lex.src` is a public field — `ts/src/lexer.ts:1513`
(`src = EMPTY  // Full source text being lexed.`) and `go/lexer.go:36`
(`Src string`). `@tabnas/json5` installs a `fixed.check` hook that reads
it, strips line continuations, and writes the whole rewritten string back
mid-lex, then resets `pnt.len`:

- `json5/ts/src/json5.ts:342` — `;(lex as any).src = rewritten`
- `json5/go/json5.go:520` — `lex.Src = rewritten`

Declare that the source may be replaced only by a **pre-parse transform**
(the `parse.prepare` slot already exists), make `Lex.src` read-only after
lexing begins, and move json5's rewrite there.

**Why it helps a port.** This is the change nobody proposed and it is the
cheapest of the three headline items. Every Rust design that borrows or
even pins the source is dead until it lands: a token minted before the
swap indexes a buffer that must stay alive, and a single `'s` tied to the
caller's `&str` is unsound. It is also what makes a per-token span sound
rather than merely convenient (§B2).

**How dead, exactly — the hazard is the engine's, not json5's.** Verified
here by construction rather than by argument. A custom matcher registered
at priority 5e5 that swaps `lex.Src` after two tokens have been emitted
produces this stream on `"aa bb cc"`:

```
tkn[0] = #TX "aa"          (from the original buffer)
  SWAP at SI=3: "aa bb cc" -> "aa bb ZZZZZZ"
tkn[1] = #TX "bb"
tkn[2] = #TX "ZZZZZZ"      (from the replacement buffer)
```

One token stream, two backing buffers, both live. That is precisely what
a single `Token<'s>` cannot express.

Two qualifications, because the distinction changes what this costs to
land. First, **json5 itself never produces such a stream**: its hook is
reached through the `fixed` matcher at priority 2e6, and every json5
document that can contain a line continuation passes that matcher at
`SI=0` — before any token exists — because `space`, `line`, `comment`,
`string` and `text` all sort *after* `fixed`, and the one matcher that
sorts before it (`match`, 1e6) can only win at position 0 on a top-level
scalar, which cannot then contain a continuation. Probed: json5 rejects
the implicit-top-level-list forms that would otherwise reach it. So no
shipped grammar straddles buffers today, and **B1 can land without a
behaviour change to json5** — the rewrite is already effectively a
pre-parse pass, it is merely spelled as a mid-parse hook.

Second, the coherence defect this also removes is **latent, not live**:
`refwd()` memoises on cursor position alone (`fwdSI !== pnt.sI`,
`ts/src/lexer.ts:1541-1546`) and is called *before* the check hook
(`:205-206`, `:909-910`), so a hook that rewrites the source at an
unchanged cursor leaves `lex.src` and `lex.fwd` answering differently for
the same span. json5 reaches that state on every continuation-bearing
document. It does not miscompute today only because the matchers that run
after the check read `lex.src` at `pnt` offsets rather than `fwd`
(`:1504-1505`) — a property of the current matcher set, not a guarantee
*(claim carried over; the position-only memo key, and the finding that it
is latent rather than live, are new here)*.

**Measurement.** None required of the engine: json5 already constructs
`rewritten` as a whole new string, so moving the construction earlier adds
no allocation. The open question was whether json5's hook depends on lexer
state that only exists mid-parse. **Answered here, and the answer retires
the spike**: the hook reads exactly `lex.src`, three `lex.cfg` fields
(`string.quoteMap`, `string.escChar`, `comment.def.hash.lex`) and
`ctx.u.json5_preprocessed`. The config fields are built before lexing
begins; `ctx.u` is used only as a run-once latch, which a pre-parse pass
does not need. The only other write is `pnt.len`, which a pre-parse pass
sets by construction. Half a day of spike becomes a mechanical move.

**Fleet.** One repo, two files. Grepped `\.src\s*=` / `\.Src\s*=`
across all 34 repos' `ts/src` and `go`, re-run against every receiver
rather than an assumed one. Sixteen write sites; the only writes to a
**`Lex`** are json5's two. The rest, classified:

| Site | Receiver | Span-safe? |
|---|---|---|
| `json5/ts/src/json5.ts:342`, `json5/go/json5.go:520` | `Lex` | **no — this change** |
| `xml/go/xml.go:1756` — `tkn.Src = src[from:to]` | `Token` | yes — a sub-slice of `lex.Src` |
| `expr/ts/src/expr.ts:1358`, `:1367`; `expr/go/expr.go:1811`, `:2013`, `:2026` | operator *definition* object (`.use`/`.tkn`/`.tin`) | n/a |
| `feed/ts/src/feed.ts:432`; `feed/go/feed.go:571` | `AtomContent` AST node | n/a |
| `multisource/go/{multisource.go:208,resolver.go:65,72,130,163}` | resolver result struct | n/a |

The `xml` site is the one worth naming, because it writes a **token's**
source and so bears on §B2: it assigns `src[from:to]`, a sub-slice of the
live `lex.Src`, so a token stays expressible as a span and B2 holds. Its
own comment records the reason it exists — Go's `Bad()` leaves
`Token.Src` empty where the TS lexer's `bad(code, start, end)` fills it —
which is the general case behind the `\uZZZZ` row already in
`DIVERGENCE.md:90`.

**Stands alone?** Yes. A public field that a plugin mutates in flight is
a contract hole in any runtime; it is only *fatal* in Rust.

#### B2. Candidate (c) resolved: a token is already a span

**The brief asks to resolve "borrowed `Token<'s>` versus `'static` plugin
bag" by measurement. There is no tension to resolve, because the
canonical engine already models a token as indices.** At
`ts/src/lexer.ts:87-137`, `Token` carries public `sI` and `len` plus
private `#src`/`#ref`, and

```ts
get src(): string {
  let s = this.#src
  if (undefined === s) {
    const ref = this.#ref
    s = this.#src =
      undefined === ref ? EMPTY : ref.substring(this.sI, this.sI + this.len)
  }
  return s as string
}
```

The class comment at `:78-86` states the design outright — "matchers that
only know the token's span … defer the substring until someone actually
reads it, so ignored tokens never allocate one". So
`Token<'s> { src: &'s str }` was never the shape to port;
`{ tin, si, len }` plus a shared backing handle is, which is what
`§2.1 risks` already prescribes. A borrowed token is independently
unsound for B1's reason.

**What is worth changing** is the reverse of what was proposed: the
contract does not *state* the span invariant, so a port is free to invent
`Option<String>`. State it, drop the `set src` accessor
(`ts/src/lexer.ts:139-141`) so the invariant is enforced rather than
observed, and fix the two engine sites that mint a token from a literal
instead of a span — `ts/src/lexer.ts:1719` (the `#ZZ` end sentinel,
`this.token('#ZZ', undefined, '', pnt)`) and `makeNoToken` at `:187`.

**Fleet.** Zero. Nothing in any of the 34 repos writes `Token.src` —
grepped. Note the token must stay otherwise mutable in the queue:
`@tabnas/c` retints already-buffered tokens after a `#define`
(`c/ts/src/c.ts:6436-6439`, `:6455`, `:6509`), so `tin` is not final at
lex time.

The measured Rust cost of the handle, carried over and worth keeping
because it settles decision B1 in `§2.1 risks`: over 400k tokens on a
1 MB source, construct + drop — span-only 0.38 ms, borrowed `&'s str`
1.22 ms, `Rc<str>` 2.12 ms, `Arc<str>` 5.56 ms, owned `String` 19.37 ms.
The handle costs 0.9-4.3 ns/byte over a borrow; owning per-token strings
costs 18 ns/byte, which is the regression the register warned about.
Recommend `Rc<str>` and treat `Send + Sync` as the separate opt-in
decision §3.7 feasibility already argues for.

#### B3. `configure()` as a pure builder, then freeze `Config` — a spike

**What is already established, and needs no change.** Candidate (b) holds
and is now stronger than "measured read-only": a deep-frozen `Config`
(excluding the token registry) parses five fixtures including escapes,
numbers, nesting and CJK with byte-identical output and no fault, and the
emitted `dist` is `"use strict"`, so an engine write would throw
*(carried over)*. The engine does not write `Config` during a parse on
the configured path.

**What fails.** The proposal to de-alias six `Config` nodes in four lines
and deep-freeze gives **210 pass / 151 fail**, not the 376/378 it claimed
*(carried over, and the mechanism re-checked here)*. The cause is that
`configure()` mutates the *same* `Config` object on every `options()`
call — `c1 === c2`, `c1.string === c2.string`,
`c1.tokenSet === c2.tokenSet` — and re-mutates it in place
(`cfg.tokenSet[name].length = 0`, `makeStringMatcher` writing
`cfg.string` on reconfigure). So the freeze is blocked on making
`configure()` a pure builder that returns a fresh `Config`. That is a
spike, and it belongs on the list in §6 next to recovery and the relex
save point.

Narrower than "configure() mutates the same Config", and worth recording:
`make()` *does* yield a fresh `Config` with unshared sub-objects, and the
parent's values are untouched by the child. The defect is intra-instance
re-mutation on `options()`.

**Two things that should land regardless.**

*Split the token registry off `Config`.* `cfg.t` / `cfg.tI`
(`ts/src/utility.ts:142-143`) is not derived config — it is a mint that
must accumulate across `configure()` runs. Freezing it fails nine suites,
every failure reading `Cannot assign to read only property 'tI'`. It
should be owned by the instance, not by `Config`, before the Rust struct
is written; then `Config` is `Arc<Config>` and immutable.

*Eager scan specs in both runtimes.* TypeScript's string matcher writes
its closure-owned `bodySpecs` map **during lexing** — verified live with a
`config.modify` hook that adds a quote character after the matcher
factories run *(carried over)*. That is the fact behind "a Rust matcher is
`FnMut`, not `Fn`", and it is what makes `&'g [Box<dyn Fn>]` matchers —
decision B4 in `§2.1 risks` — fail to typecheck. Move the construction to
the end of `configure()`, after the `config.modify` hooks. In Go, make
`buildScanSpecs` (`go/scan.go:278`, already called eagerly from
`go/options.go:1175` with the comment "so the shared LexConfig is
read-only while parsing") the only path and delete the three lazy getters
at `go/scan.go:246`, `:253`, `:260`. The exposure is exactly one live
consumer and it is a test — `DefaultLexConfig()` followed by
post-construction field mutation, `go/lexer_edge_test.go:166-175` — which
means the cost is a public rebuild entry point, not a fleet migration.
Keep the `DefaultLexConfig` symbol: `jsonic/go/engine.go:215` re-exports
it as jsonic's public API.

### C. The callback narrowing wave

These land together, after A1, with fleet patches pre-staged. The reason
they are a wave and not seven changes is measured: **only 3 of 31 TS
fleet repos have `node_modules` installed in this checkout**, and the
fleet peer-depends on the published `@tabnas/parser`, so no signature
narrowing is visible downstream until publish and then they all land at
once. That is the same failure `AGENTS.md` records for the #110 `meta`
passthrough. With one maintainer, batching is the cheaper shape.

#### C1. Hoist the post-action routing into locals

**Change.** In `RuleSpec.process` (`ts/src/rules.ts`), immediately before
the consumed-token computation at `:619`:

```ts
const _push = alt.p, _repl = alt.r, _back = alt.b || 0
const _cons = rule[is_open ? 'oN' : 'cN'] - _back
```

Then use `_push` at the push site (`:653`, `:655`, `:679`, `:682`),
`_repl` at the replace site (`:680`, `:688`, `:703`, `:706`), and
`let consumed = _cons` at `:737`. The engine then reads nothing off the
alternate after `alt.a` at `:646`.

**Two corrections to the brief, both load-bearing.**

*The channel is not dead.* `@tabnas/expr` writes `a.r` and `a.b` at
`expr/ts/src/expr.ts:1230`, `:1231`, `:1237`, `:1238`, from `implicitList`,
wired as `h:` at `:722` and `:741`. Those writes route the parse and are
guarded by hundreds of dual-runtime fixture rows. They survive only
because `h` runs at `ts/src/rules.ts:569`, *before* the action. **Scope
the change to post-action reads and leave `p`/`r`/`b` writable from the
modifier.** Deleting the whole channel, as the brief states it, breaks a
shipped grammar.

*The channel is not merely unused — it is corrupting, and that is the
better argument.* `alt.b` is read **twice**, at `:619` and again at
`:737`, with the user action in between. `:619` drives the `ctx.v`
history push; `:737` drives the lookahead-buffer shift. An action that
writes `alt.b` desynchronises `ctx.v` from `ctx.t` and silently breaks
`ctx.rewind`. So the redirect is not a capability worth keeping; it is a
hazard that happens to be unused.

**Why it helps a port.** It is the difference between `process()`
resolving its routing before it hands `&mut Ctx` to a callback and having
to re-borrow the alternate afterwards. With the hoist, the routing is
three `Copy` locals on the stack, and the action's `&mut Ctx` borrow can
end before they are used. Compiled probe (rustc 1.94.1): `let (push, repl,
back) = (alt.p, alt.r, alt.b);` before `a(ctx, rid)?` compiles with
`&'g Grammar` held across the whole pass *(carried over)*.

**Measurement — a real win, under the sign-flip protocol.** Built here as
a patched engine in an isolated tree; output verified byte-identical
against the baseline on six fixtures (`records-16kb`, `records-1mb`,
`numbers-1mb`, `nested-256`, `records-escaped-1mb`, `text-1mb.jsonic`).
`records-16kb.json`, 25 rounds × min-of-20 after 60 warmups:

| direction | `d_min` | B wins |
|---|---|---|
| null (identical builds) | +0.11% / −0.89% / −0.70% | 13, 20, 17 of 25 |
| forward (base in A) | −4.93% / −4.80% / −2.15% | 25, 25, 23 of 25 |
| reverse (C1 in A) | +1.09% / +0.22% / +2.67% | 3, 8, 0 of 25 |

The sign reverses cleanly and the win rate goes from 92-100% to 0-32%.
Correcting for the ~0.5% slot bias the null shows, the geometric estimate
is **≈ −2%**. At `records-1mb.json` three of four paired runs favour it
but the effect sits inside the noise; report it as decisive at 16 KB and
directional at 1 MB. Mechanically the change strictly removes work: three
property loads on a polymorphic object become three locals, and one
duplicated arithmetic expression is deleted.

**Loudness — a decision, not a detail.** The object handed to the action
is `ctx._palt`, the reusable per-Context scratch (`ts/src/rules.ts:1226`),
and it is writable and shipped on npm. Hoisting makes an existing write a
silent no-op for any out-of-tree user. The loud variant is to compare the
three locals against `alt.p`/`alt.r`/`alt.b` after the action and raise a
coded fault on mismatch — three comparisons at roughly a quarter of a
rule pass per source byte. That variant **needs measuring separately**;
it does not inherit C1's number.

**Fleet.** Zero. Every fleet write to an alternate's `p`/`r`/`b` is
grammar-build time on an unnormalised spec — 30 Go hits, all inside
map-to-`GrammarAltSpec` converters (`bnf/go/emit_support.go`,
`c/go/grammar_install.go`, `css`, `csv`, `toml`, `yaml`, `zon`, `ini`,
`chess`), plus `bnf/ts/src/compiler.ts:2871` — or expr's pre-action
modifier above.

#### C2. Drop argument 3 from the five non-modifier callback types

**Change.** Six call sites in `ts/src/rules.ts` — `alt.a(rule, ctx)` at
`:646`, `alt.c` at `:1481`, `alt.e` at `:1575`, `alt.p`/`alt.r`/`alt.b`
at `:1581`/`:1588`/`:1595` — plus the two internal composers and the
generated `ruleCond` leaves. Six type declarations in `ts/src/types.ts`:
`AltCond` `:706`, `NormAltCond` `:711`, `AltAction` `:724`, `AltNext`
`:734`, `AltBack` `:741`, `AltError` `:758`. **`AltModifier` (`:716`)
alone keeps the alternate**, by value, and returns it. This adopts Go's
existing contract verbatim (`go/rule.go:92-104`), so it is a declared
narrowing, not an invention.

> Re-anchored here, because the earlier sweep's citations were off:
> `AltAction` is `:724`, not `:715`, and `StateAction` is `:749`, not
> `:747`. Several of the eight pointed at the preceding comment line.
> The scratch is created at `ts/src/rules.ts:1226`, not `:1233` —
> `:1233` is `out.n = undefined`, one of the reset lines. Re-check
> every line number before any of this becomes a codemod spec; the
> `ts/src/rules.ts` citations throughout this document were verified
> individually and hold.

**Sequencing — the sharpest finding in this document.** C2 is blocked on
#120, because the eight builtin config reads at `ts/src/builtins.ts:127`
… `:305` take their config off argument 3. There are two ways to unblock
it, and they are **mutually exclusive**:

- **S0**, the route the strategy document schedules (`§5.1 strategy`):
  move the eight reads to `r.k.<name>$`. This *requires* the config key
  to keep propagating into every child rule — which is exactly the
  123,174 keep-bag copies per megabyte that A1 deletes, measured at −12%.
- **A1**: hoist the config into a closure at load. The builders stop
  needing argument 3 at all, and C2 unblocks for free.

**Do not land S0 as written.** It permanently forfeits the largest
measured win in this brief and it standardises the side of the parity
divergence that `AGENTS.md:26` says is wrong. Land A1 first, then C2.

**Why it helps a port.** It is what lets the scratch be a stack local
rather than a `Context` field, and that is a borrow-checker fact, not a
preference: a three-argument action against a scratch living on the
Context — `a(ctx, rid, &mut ctx.palt)` — is `error[E0499]` twice,
exactly the shape `ts/src/rules.ts:1226` + `:646` has today. With two
arguments, `parse_alts` returns `AltMatch` by value, `process()` owns it
on the stack, and the action gets `&mut Ctx` *(compiled probes, carried over)*.

**Fleet.** One genuine break, in one repo.
`expr/ts/src/expr.ts:1279 implicitTernaryAction(r, _ctx, a: AltMatch)`
reads `a.r` at `:1283` (`if ('elem' === a.r)`), wired as `a:` at `:948`
and `:966`. It is the only argument-3 reader in 31 TS packages. The
migration is **judgement work, not a mechanical rewrite**: TypeScript uses
function-form `r:`/`b:` on those alternates, so the routing value is only
knowable at action time. Go already ships the answer
(`expr/go/expr.go:1385-1461`): it splits the alternate into a static
`R: "elem"` plus action pair and a separate `B: 1` paren-close alternate,
so the read is subsumed by the split. 552 of expr's 1,261 shared fixture
rows exercise that path in both runtimes and guard the port. A cheaper
escape also exists — have the `r:` function stash its result in `r.u`.

Secondary and type-only: `bnf/ts/src/spec.ts:252` publishes
`type ActionFn = (r, ctx, alt) => any` as `ActionsMap`, re-exported by
`bnf` and `abnf`. It reaches the engine through an untyped ref bag, so
nothing fails to compile, but the published type should narrow in
lockstep.

#### C3. Narrow `StateAction` to `(rule, ctx)`; drop the `this` binding

**Change.** `ts/src/rules.ts:553-559` becomes
`for (…) { const bout = befores[bI](rule, ctx); … }` and `:718-726` the
same for afters; the `bout`/`aout` threading variables disappear.
`ts/src/types.ts:749-754` drops parameters 3 and 4. Adopts Go's existing
`type StateAction func(r *Rule, ctx *Context)` (`go/rule.go:104`).

Separately, `ts/src/rules.ts:556` is
`befores[bI].call(this, rule, ctx, next, bout)` while `:720` is
`afters[aI](rule, ctx, next, aout)` — before-actions receive `this` = the
`RuleSpec`, after-actions receive `undefined`. The `StateAction` type
declares no `this` parameter, Go has no equivalent, and Rust has no
spelling for it. Make `:556` a plain call.

**Argument 3 is exactly derivable in all four slots** — probed on the
shipped build: `bo` receives `rule` (argument 1); `bc` receives
`ctx.NORULE`; `ao` and `ac` receive `rule.next`, assigned at
`ts/src/rules.ts:711` before the afters loop runs at `:718`. The
derivation site is `:530`, `let next = is_open ? rule : ctx.NORULE`.
Argument 4 has zero readers anywhere: the engine uses it only for its own
`isToken && err` short-circuit.

**Why it helps a port.** The four lifecycle slots are what
`§3.1 strategy` calls the canonical E0499 site — `(rule, ctx, next, out)`
hands out two rule handles plus the Context that owns both. Two arguments
makes the whole imperative tier one signature instead of two, and the
arena needs no second `&mut Rule`.

**Measurement.** Earlier sweeps report free-to-favourable; under the
corrected floor (§2.3) that is **unresolved**. It strictly removes two
argument slots per lifecycle call and two loop-carried locals, so land it
on the removal argument, or re-measure under the sign-flip. Do not quote
the old number.

**Fleet.** Two repos, three lines. `toml/ts/src/toml.ts:312`
(`'@table-ac': (_r, _ctx, next) => { next.n.table_dive = 0; … }`) reaches
the engine through an untyped `refs: Record<string, any>`, so it fails at
*runtime*, not compile time; the fix is `next` → `r.next`, one line.
`directive/ts/src/directive.ts:115-126` declares a five-parameter `bc`
(`this: RuleSpec, rule, ctx, next, tkn?`) and forwards both via
`action.call(this, rule, ctx, next, tkn)` at `:122` — a loud compile
break, and its own Go twin is *already* two-argument
(`directive/go/directive.go:21`).

One correction to the fleet census: `bnf`'s `composeActions` and
`seqActions` (`bnf/ts/src/spec.ts:258-273`) are not "type-only". They
forward the third argument at runtime under the parameter name `alt`,
but `PHASES` (`:255`) is `{bo, ao, bc, ac}` — these are *state* actions,
whose third engine argument is `next: Rule`. The published type has been
misdescribing what it forwards. Fix the name in the same wave, and note
that `composeActions` already drops argument 4, so bnf users never had it.

#### C4. Matchers declare `starts` and `tins`; the engine stops keying on the name

**Change.** `MakeLexMatcher` returns a matcher carrying two optional
declarations — `starts` (leading char codes it can fire on; default all)
and `tins` (token ids it can produce; default unknown). `buildLexDispatch`
reads `starts` instead of branching on `(mat as any).matcher` at
`ts/src/utility.ts:766-767`; the negotiated-lexing want filter reads
`tins` instead of its hard-coded name switch at `ts/src/lexer.ts:1738`.
Built-ins declare the same sets the name-derived branches compute today,
so the default table is byte-identical. Go's `MatchSpec` gains the same
two fields.

**It fixes a live, reachable crash.** Reproduced here against
`ts/dist`:

```
new Tabnas({lex:{match:{string:{order:5e6, make}}}})
  → TypeError: Cannot read properties of undefined (reading 'check')
options({lex:{match:{string:{…}}}}) then .make()
  → the identical TypeError
```

`toml/ts/src/toml.ts:23` performs exactly that registration
(`options: lex: { match: string: make: '@make-toml-string-matcher' }`).
It survives today only because the registration arrives through
`options()` on an already-configured instance, where the stale
`cfg.string.quoteBitmap` from the deleted built-in silently supplies the
candidate set — so `.make()` on a TOML instance crashes. Census: 376
`.make(` call sites across 11 fleet repos.

**Why it helps a port.** It makes the feasibility report's
`[Vec<MatcherId>; 257]` dispatch table correct *by construction* rather
than by audit — with name-keying it cannot be built correctly at all for
a third-party matcher. It also removes two string-keyed lookups
(`ts/src/lexer.ts:1656`, `:1767`) from the Rust hot path, and it lets the
relex path *skip* a matcher whose tins are not wanted instead of running
it under `speculate()` and rolling back, which is the only place the
un-rollbackable "matcher wrote Ctx state" hazard can fire.

**Measurement.** Removing the per-attempt name lookup measured as a small
win *(carried over: −0.45 / −0.40 / −1.22% at 16 KB)*; unresolved under
the corrected floor, but it strictly removes a string-keyed lookup from
a path that runs 0.17 matcher attempts per source byte. The relex saving
**needs measuring** and cannot be, because `lex.relex` is off in every
shipped grammar.

**Fleet.** Zero: purely additive with defaults that reproduce today's
behaviour, so 0 of 34 repos change.

#### C5. Drop the `tI` matcher argument; keep `pnt.token` engine-internal

`LexMatcher` (`ts/src/types.ts:660-664`) takes a third `tI` argument
threaded through `ts/src/lexer.ts:202`, `:210`, `:1647`, `:1659`, `:1755`,
`:1770`. **Zero of 34 repos declare it** — the single `(lex, rule, tI?)`
occurrence in the fleet is a comment at `c/ts/src/matchers.ts:10` — and
Go's `LexMatcher` is already 2-arity, which is the documented,
wrongly-scoped difference. The engine's own match matcher, its only
reader (`ts/src/lexer.ts:526`, `:592`), can take it from a `Lex` field
written once per `next()`.

Separately: stop documenting `pnt.token` as a matcher output channel (the
NOTE at `ts/src/lexer.ts:626-632`). Zero fleet writers; the engine writes
it at exactly one site (`:1495`); `@tabnas/c` *reads* it to retint
buffered tokens (`c/ts/src/c.ts:6439`, `:6455`, `:6509`), so it must stay
reachable and mutable — it just is not a matcher capability. The real
multi-token need is elsewhere and is real: `@tabnas/yaml` keeps its own
`pendingTokens` array in a closure and shifts from it
(`yaml/ts/src/yaml.ts:1702`), a userland reimplementation of the
capability the engine documents and nobody uses through the sanctioned
path. The Rust answer is that `Match` is a list — for yaml's reason, not
for the queue's.

#### C6. One normalising span constructor

Add a single total span constructor — clamp into `[0, src.length]`, swap
if reversed, snap outward to a code-point boundary — and make it the only
way a plugin-supplied offset reaches the source: `lex.bad(why, pstart,
pend)` (`ts/src/lexer.ts:1831`), the explicit-`len` form of `lex.token`,
and the recovery walk `advanceLexPast` (`ts/src/rules.ts:825-851`), whose
target is `t.sI + Math.max(1, t.len|0)` with a plugin-supplied `len`.

This converts `§2.1 risks`' "audit 26 slicing sites in `ts/src/lexer.ts`,
69 in `go/lexer.go`" into a type-level invariant — the Rust `SrcSpan`
newtype of decision B3. It matters more than an ordinary bug class
because the panic is raised by the *engine*, not the plugin, so it lands
outside whatever `catch_unwind` story the port adopts and is
unrecoverable under `panic=abort`.

**Fleet: negative churn.** The 53 TS and 35 Go `lex.bad` call sites keep
their signatures, and three Go repos get to *delete* hand-rolled
clampers that exist only because Go's exported `Bad(why)` takes no span —
`chess/go/chess.go:859 badToken`, `zon/go/zon.go:405 zonBad`
(byte-identical copy-paste) and `xml/go/xml.go:1746 badSpan`, whose
comment names the missing span form as the reason. None of the three
snaps to a character boundary. Export Go's span-taking form in the same
change.

#### C7. Freeze the alternate's `n`/`u`/`k`/`g` at grammar-build time

Four `Object.freeze` calls at the tail of `normalt`, plus one supporting
edit: the `g` normalisation must start from a fresh array, because
`normalt` can run twice on one alternate and `.sort()` on a frozen array
throws. Leave the *scratch's* scalar routing fields writable — a modifier
is allowed to redirect this pass.

It converts the live-grammar-corruption hazard from a runtime problem
into the thing `&'g Grammar` already expresses. The defect is real in
both runtimes: an action writing `alt.k.INJECTED` / `alt.g.push()` leaves
the installed grammar altered for every later parse in TypeScript, and
Go's `H` does the same through the live `*AltSpec` *(carried over)*.
C2 closes the action route structurally; the freeze closes the modifier
route.

**It only reaches one level**, which is why C7 and A1 are complementary
rather than alternatives: `ts/src/rules.ts:605` is
`rule.k = Object.assign(rule.k, alt.k)`, a shallow copy, so
`rule.k.array$ === alt.k.array$`. Under A1 the `k` half is moot; the
`n`/`u`/`g` half still matters.

**Measurement.** Build-time only, nothing on the parse path. The earlier
"free at a ±0.5% floor" verdict is unresolved under §2.3; there is
nothing on the hot path for it to regress.

**Fleet.** Zero. All ten fleet reads of an alternate's `g` are read-only,
and `expr`'s `tagExpr` builds a fresh object rather than mutating
(`expr/ts/src/expr.ts:198`). `ts/test/grammar-immutable.test.js` already
establishes that `grammar()` deep-copies the caller's spec, so the frozen
objects are never the caller's.

### D. The tail

Individually unremarkable; collectively they are most of what a third
runtime would otherwise have to guess. All are zero-fleet unless noted.

| # | Change | Why a port cares | Fleet |
|---|---|---|---|
| D1 | Delete `need` — `ts/src/rules.ts:73` and the `need: 1` entry in `COND_PATH_ROOTS` at `:1903` | One fewer arena field, and one fewer never-exercised arm in the declarative-condition match | 0 |
| D2 | Delete `closeInfoCache` (`ts/src/rules.ts:808`, read `:855`, **written mid-parse** `:876`) | The engine's only process-global mutable cache; falsifies §2 feasibility's "no mutexes anywhere" | 0 |
| D3 | Delete instance `Merge` (`ts/src/merge.ts` 614 + `go/merge.go` 847; tests 708 + 834) | Its correctness rests on function equality, and there are already two answers with no third | 0 callers |
| D4 | Assign, do not concatenate, `cfg.text.modify` (`ts/src/utility.ts:273-278`) | `configure()` is currently non-idempotent; Go already assigns (`go/options.go:830-831`) | 0 |
| D5 | Declare bag propagation a **shallow snapshot** taken at push/replace | Licenses `k: Rc<Map>` + `make_mut` instead of an eager clone per push | 0 |
| D6 | Publish the grammar's `max(alt.sN)` and pin that `rule.o`/`rule.c` never exceed it | Turns `o: Vec<Token>` into an inline array; ~250k `Vec` allocations per MB removed | 0 |
| D7 | Pin the `pid == nid` node-seeding invariant with a shared fixture | `get_disjoint_mut([pid, nid])` returns `Err` on duplicate indices — the guard is load-bearing on ordinary input | 0 (a test) |
| D8 | Keep three bags, and say **why** | Tells the porter `u` can be a plain owned map and `n`/`k` need the shared form | 0 |
| D9 | Declare `undefined`/`@SKIP` as the presence marker (`ts/src/utility.ts:663-665`) | `Option<T>` is TypeScript's semantics exactly; without the ruling Rust invents a fourth | 0 |
| D10 | `schema/options.json` — a generated, tiered option-slot manifest, with a Go reflection gate and a TS Proxy read-oracle | Turns "which options may a portable spec contain" from folklore into a file a loader reads | 0 |
| D11 | Restrict FuncRef resolution to declared code slots (`ts/src/tabnas.ts:775`) | An untyped ref bag resolved positionally has no Rust spelling | 0 |
| D12 | Normalise `rewind.history` and `rule.maxmul` to a finite integer with an explicit unbounded sentinel | Rust has no `Infinity` for an integer cap; `Option<usize>` forces the answer to be written down | 0 |
| D13 | Declare `Info` metadata **out of band** — TypeScript's semantics — in `doc/value-builtins.md` and `go/doc/differences.md` | Saves the Rust `Node` enum three variants plus the `NodeMapSet`/`NodeListAppend` dispatch layer | 0 (contract only) |
| D14 | `ScanSpec.fallback` becomes data, not a closure over live config | Removes a dynamic call per non-ASCII byte and makes `ScanSpec` a plain `&'g` value | 0 |

Five of these deserve a sentence more.

**D1 is already a silent load-time divergence.** `COND_PATH_ROOTS`
(`ts/src/rules.ts:1900-1907`) contains `need`; Go's `condPathRoots`
(`go/rule.go:646-655`) does not, and the comment above it at `:641-642`
says "Matches the TS port's set, so the same declarative grammar is
expressible in either runtime." It does not. A `need`-rooted declarative
condition builds in TypeScript and is rejected at grammar-build in Go —
on the serialized-spec path v0.1 targets. Zero `\.need\b` hits across all
34 repos in either runtime. Add a shared assertion that the two root sets
are equal so the next entry cannot drift the same way.

**D2 is what makes `&'g Grammar` sound on the recovery path.** The cache
is keyed on the identity of a grammar object and written from
`computeSyncTins` *during* a parse — a `HashMap<*const [Alt], _>` with no
sound idiomatic Rust spelling. Go already made this decision and
documented it in the code, at `go/recover.go:46-49`: "Unlike TS this is
not cached: Go's `AltSpec.S` is read directly … so the walk is a few
slice reads over a handful of alternates, on an error path only." All
four TypeScript call sites are error-path or continuations-path, so a
parse that raises no error never calls `closeInfo` at all and the delta
is exactly zero; the upper bound on a deliberately pathological document
(4,000 recoveries in 86 KB) is ~3% *(carried over)*. It is mentioned in
none of the three previous documents, and it is invisible to the brief's
item (b), because that verification snapshotted `Config` and this cache is
not on `Config`.

**D3 is the largest single deletion available.** Zero fleet callers in
either runtime — every TypeScript `.merge(` hit is `cfg.map.merge`, a
different feature. Its dedupe key is `u.toString() === v.toString()` on
closure source in TypeScript and `reflect.ValueOf(fn).Pointer()` in Go;
those already disagree, and Rust can produce neither, because
`Box<dyn Fn>`'s data pointer is a fresh allocation per closure and its
vtable pointer is shared by every closure of that type — so a naive port
gets both false negatives and false positives. Keep
`deshareMatchTokens` (`ts/src/merge.ts:166-198`), which `grammar()` uses;
move it to `utility.ts`. `Derive()`/`make()` are a different, cheap
capability and stay.

**D9 is a ruling the fleet has already made, in two directions.**
`jsonc/ts/src/jsonc.ts:26-29` carries `multiChars: ''` and `sep: null` —
zero values that must override. `csv/ts/src/csv.ts:233-239` writes
`IGNORE: [strict ? null : undefined, null, undefined]`, using `undefined`
*positionally* to mean "keep the default at this index". Ruling against
TypeScript breaks both; ruling for it lets three hand-written Go
workarounds be deleted (`jsonic/go/jsonic.go:66-109`, `:225-236`;
`json5/go/json5.go:742-751`), each of which names the engine merge as its
cause — and that diff is the proof the rule is real. Drive the acceptance
fixture through the *options pipeline*, not through `deep`/`Deep`, where
the two runtimes already agree and a fixture would pass green with the
defect untouched.

**D14 cannot currently be judged.** Both runtimes snapshot the ASCII
class table into a fixed array and leave the non-ASCII class to a closure
over live config (`ts/src/lexer.ts:251`, `:285`, `:323`; `go/scan.go:54`,
`:140`, `:169`, `:231`). That is also a genuine incoherence: one spec
answers ASCII from a snapshot and non-ASCII from live config, so a
post-configure change to a quote or space set is visible to astral input
and invisible to ASCII input. The replacement is a snapshot lookup where
there is a closure call plus a map lookup today, so it cannot be slower
— but that is an argument, not a number, and §2.5 explains why no
fixture in the matrix can produce one. Add a non-ASCII fixture to
`genfixture.js` first.

---

## 4. Rejected

A proposal document that silently omits its failures is worthless. A
dozen candidates were considered and dropped. One of them was dropped
after the measurement came back *favourable*, which is the more
interesting case.

### 4.1 The per-pass scratch `AltMatch` — dropped, but not as a regression

The candidate was to replace `ctx._palt` (`ts/src/rules.ts:1226`) with a
fresh `makeAltMatch()` per pass. An earlier sweep reported it faster at
16 KB and slower at 1 MB (+1.69 / +0.93 / +0.68%) and rejected it as a
performance regression. **That is wrong and the record should be
corrected.** Re-measured on a GC-inclusive statistic, it is
neutral-to-faster at every size including the allocation-heaviest fixture:
`records-1mb` `d_TOTAL` −2.52 / −2.11 / −0.35 / +0.13 against a +0.88%
null, `numbers-1mb` `d_TOTAL` −1.04 / −2.22 / −6.02 *(carried over)*.
The GC counter confirms the mechanism and refutes the conclusion —
scavenges rise 9.5% while wall time falls, because nursery objects that
die young cost scavenge *count*, not scavenge *time*, and the removed
write barrier on the long-lived Context field pays for it. `min`-of-N
was the wrong statistic: it systematically excludes GC pauses, which is
the one channel an allocation candidate moves.

**Drop it anyway, for the reason the candidate itself gives**: making the
scratch per-pass in TypeScript buys the port nothing. Rust's `AltMatch` is
a plain struct that lives on the stack either way. What Rust actually
needs is *permission* not to keep it on `Ctx`, and that permission is a
consequence of C2, not of TypeScript's allocation strategy. After C2 the
scratch is reachable only from `alt.h` and from `ctx.log`, whose sole
consumer reads it synchronously inside `parse_alts` and never retains it
(`debug/ts/src/debug.ts:1093-1128`) — so its lifetime becomes
unobservable, and each runtime picks its own storage with no cross-runtime
difference to declare.

### 4.2 Borrowed `Token<'s> { src: &'s str }` — a false dilemma

Covered at §3.B2. The canonical engine already models a token as
`(ref, sI, len)` with a lazy getter, so there was never a borrowed-versus-
`'static` trade to resolve by measurement; and a borrow is independently
unsound because the engine lets a matcher hook replace the backing source
between tokens (§B1, demonstrated by construction) — `@tabnas/json5` is
the proof the capability is used at all, though its own grammar always
lands the swap before the first token.
The action item — "resolve the tension with measurement" — should be
struck, and replaced with "state the span invariant and drop the setter".

### 4.3 Trim the exported `util` bag from 39 members to 12

The motivation is sound: the bag is a JavaScript standard-library shim
exported as engine contract, and three of the seven unadjudicated
cross-runtime behaviours in §4.7 feasibility live inside members with
near-zero fleet consumers (`str(1e21)`, `str(Infinity)`, `strinject`,
`modlist`'s move semantics). But the change breaks the relaxed reference
grammar loudly and does not achieve its goal.

`jsonic` destructures `keyOrder` and `recordKeyOrder` — both on the drop
list — at `jsonic/ts/src/grammar.ts:30` (used at `:211`, `:224-227`,
`:628`), imports `parserwrap` (also on the list) at
`jsonic/ts/src/jsonic.ts:42`, and **re-exports `keyOrder` as its own
public API** at `jsonic/ts/src/jsonic.ts:405` and `:452`. And the goal is
not met: the package declares five entry points, and the `/utility`
subpath alone exports 34 names, 12 of them not in the bag — including
`modlist`, one of the three divergences the candidate cites as its
motivation. Union of publicly reachable utility names: 51, plus `/lexer`
(25), `/error` (8) and `/builtins` (2).

**Reformulate as "close the subpaths or mark them internal".** Trimming
the bag alone is churn for nothing.

### 4.4 A hard type-level split of code-valued from data-valued options

`TabnasOptions` plus `TabnasHooks` would make the serialized surface a
type rather than a policy, and it would give Rust its struct boundary for
free. It is disqualified on fleet cost *(carried over)*: **15 of the 31 TS
fleet repos pass a code value in an options literal** — 6 register a
custom lex matcher, 9 install a `config.modify` hook, 7 pass a `check`
hook, 6 pass `parse.prepare`, 1 passes `map.merge`. Roughly half the
fleet edits source for a benefit fully obtainable from D10's tier tags,
which cost nothing.

### 4.5 Reformulating subscribers as event-record-and-drain

It fails, and the measurement says exactly why. `@tabnas/c` buffers trivia
tokens and writes `tkn.use.leading` onto the *next* non-trivia token, in
both runtimes (`c/ts/src/c.ts:2522-2540`, `c/go/c.go:232-254`) — an
annotation the rule machine must see when it consumes that token, so a
drain-after-dispatch delivers it too late and silently drops every comment
from the C CST.

But the same instrumentation buys something better than record-and-drain
would have. All seven dispatch sites were patched to snapshot and compare
own-enumerable properties of `ctx`, `rule` and `token` one level into the
`use`/`meta`/`u`/`n`/`k` bags. Across 388 engine tests and 133
`@tabnas/c` tests the *only* recorded writes are `token.use` (8) and
`token.use.leading` (8): **zero Context writes and zero Rule writes**
*(carried over)*. That retires `§2.4 risks`' claim that the true
subscriber signature is `Fn(&mut Ctx, &mut Token, RuleId)`. It is
`(&Ctx, &mut Token, RuleId)`, and with the list hoisted off the Context
the dispatch compiles with no `RefCell` and no split-borrow gymnastics.

### 4.6 The rest, briefly

- **Replace the upward parent write with a return value or a deferred
  edit queue.** Costs a per-close allocation and rewrites four fleet
  repos that write `r.parent.node` / `r.parent.parent.node`
  (`csv/ts/src/csv.ts:355`, `:590`; `expr/ts/src/expr.ts:804`,
  `:873-874`, `:1013`, `:1183`; `multisource/ts/src/multisource.ts:240`,
  `:248`). The observed depth is two ancestors, which is the number a
  Rust `get_disjoint_mut` call has to accommodate — record that instead.
- **Collapse `n` into `k`.** Deletes an algorithm. `@tabnas/expr` writes
  the same key into *both* bags and later compares them for equality —
  `r.u[pd] = r.n[pd] = 1` at `expr/ts/src/expr.ts:1077` against
  `if (r.u[pd] === r.n[pd])` at `:1094`, with
  `pd = 'expr_paren_depth_' + op.name`. The equality holds only because
  `n` was inherited through the push chain and `u` was not, so it answers
  "is the paren closing here the one that opened here". Nothing errors if
  you collapse them; the value simply starts appearing in child rules and
  the failure surfaces in a downstream grammar months later.
- **Intern counter names into `u32` slots.** `expr` computes counter names
  at runtime (`'expr_paren_depth_' + op.name`), so they are not statically
  enumerable.
- **Replace `AstNode.src` concatenation with a source span.** A real Rust
  win that changes output: the concatenation excludes IGNOREd tokens a
  span would include, and the builtins are contractually byte-identical to
  `@tabnas/abnf`'s `mkAstNode`.
- **Delete `fwd`/`refwd`.** Reformulated rather than rejected. Deleting it
  is 2 repos and 16 sites (`yaml` 14, `toml` 2); **keeping the name as a
  non-memoised accessor is 0 of 34 and measures identically**, because
  V8's `substring` on a long string is an O(1) `SlicedString`. Do that.
  The memo is worth removing regardless: `guardedMatcher` calls
  `lex.refwd()` *before* the check hook (`ts/src/lexer.ts:204-206`, with
  the comment "Check hooks are user code and may read `lex.fwd`
  directly"), so a hook that rewrites the source at the same cursor leaves
  `lex.src` and `lex.fwd` giving two different answers for the same span —
  and `@tabnas/json5` performs exactly that rewrite in production.
- **The strategy document's S0 milestone as written** (move the eight
  builtin config reads to `r.k.<name>$`). See §3.C2: it is the expensive
  resolution of #120 and it forfeits A1's −12%.
- **De-alias `Config` in four lines and deep-freeze it.** See §3.B3: a
  genuine deep freeze is 210 pass / 151 fail. The perf claim holds; the
  scope claim does not.

---

## 5. Changes That Need a Maintainer Decision

Eight. Each is a judgement call with the evidence attached, not an
engineering task. Six of the eight are free *today* and stop being free
the moment a consumer exists — which is the argument for deciding them
now rather than by whoever writes the Rust engine first.

**5.1 The builtin-config route (§3.A1 / §3.C2).** Confirm that #120 is
implemented by hoisting config to load time and out of `k`, not by moving
the eight reads to `r.k`. Evidence: 123,174 keep-bag copies per megabyte
that nothing reads, −12% measured with a clean sign flip, and a route that
otherwise locks that cost in permanently. This is the sequencing decision
the whole of §3.C depends on.

**5.2 The option-merge classes.** Rule for TypeScript's index-wise merge
with `undefined` losing. Measured on the built engine here: the default
`tokenSet.KEY` is `[10,8,9,11]`; `{KEY:['#TX']}` yields `[10,8,9,11]` — a
**complete no-op**; `{KEY:['#ST',null,null,null]}` yields `[9]`. So
`css/ts/src/css.ts:296` and `zon/ts/src/zon.ts:171-174` are dead
declarations whose Go twins (`css/go/css.go:296-298`,
`zon/go/zon.go:191-194`) write the same intent as a *replace* and get
`{#TX}` — two cross-runtime `Config` divergences, latent because css also
disables the affected matchers. Of the seven fleet declarations that
write an *existing* set name, five are written to TypeScript's index-wise
semantics (`json/ts/src/json.ts:64`, `jsonl/ts/src/jsonl.ts:59`,
`jsonic/ts/src/grammar.ts:754`, `toml/ts/src/toml.ts:39-41`,
`csv/ts/src/csv.ts:233-239`) and two are the dead replace form. Ruling
TS-canonical is what `AGENTS.md:26` requires anyway, and it exposes those
two plus `json5`'s three-element write (`json5/ts/src/json5.ts:106`) —
which yields `[10,9,11,11]`, a duplicate from the retained default at
index 3 — as declarations that then need fixing.

**5.3 `Lex.next` and IGNORE — the only *silent* break in the set.** Go's
exported `Next` skips IGNORE tokens in its own loop
(`go/lexer.go:955-958`); TypeScript's returns them and the parser skips
them (`ts/src/rules.ts:1395`). `AGENTS.md:26` makes Go the side that
moves. But `c/go/refs_newpath.go:187-188` carries the comment "The Go
lexer's `Next()` already skips IGNORE tokens internally" and calls
`ctx.Lex.Next(ctx.Rule)` unfiltered at `:204`, pushing straight onto
`ctx.T`; its TypeScript twin filters explicitly at
`c/ts/src/c.ts:2333-2336`. Making Go match TypeScript starts feeding
whitespace and comments into C's lookahead **with no compile error**.
The fix is three lines mirroring `c.ts`, and it must land in the same
commit. Worth correcting alongside: `ci/README.md`'s parity contract says
Go's `Sub` fires after IGNORE skipping; it does not — Go fires lex
subscribers before filtering, deliberately, and `@tabnas/c`'s
trivia-preserving subscriber depends on it.

**5.4 Should C1's removal be loud or silent?** Comparing the three
hoisted locals against `alt.p`/`alt.r`/`alt.b` after the action and
raising a coded fault converts a silent narrowing of a published API into
a diagnosable one, at three comparisons per rule pass. Needs its own
measurement; it does not inherit C1's number.

**5.5 The `RuleDone` payload: resolved or static?** TypeScript reports the
*resolved* routing (`ts/src/rules.ts:1576-1580` writes `out.p = alt.p(rule,
ctx, out)` into the scratch, read at `ts/src/parser.ts:264`); Go reports
the *static* grammar field (`ruleDoneAlt()` reads `ctx.dalt.B/G/P/R` while
`PF`/`RF`/`BF` resolve into locals that are never written back). **Zero
RuleDone subscribers exist across all 34 repos in either runtime**, so
decide on merit. Note that C1 *alone* introduces a new inconsistency — the
engine would push the static value while the payload reports the
post-action write — and C1 plus C2 together close it with no extra code.
Reporting the static field instead costs two extra property loads per pass
and would need measuring.

**5.6 The `e`-versus-`h` ordering.** TypeScript resolves `e` inside
`parse_alts` (`ts/src/rules.ts:1575`) and runs `h` outside it (`:569`);
Go runs `H` then `E`. **No alternate anywhere declares both** — `h:`
appears only at `expr/ts/src/expr.ts:722` and `:741`, `e:` only in `ini`
and `jsonic`, with zero overlap — so the ordering is unobservable in every
shipped grammar. TypeScript pins its own side in a unit test
(`ts/test/cover-engine.test.js:442` asserts `['e','h','child']`), but the
shared corpus does not, and a unit test in one runtime does not pin a
two-runtime contract. Adopting Go's order makes the pass a straight line
with exactly one mutation point, which is what lets `parse_alts` return
`AltMatch` by value; it costs one assertion and needs a shared fixture
plus a `DIVERGENCE.md` row.

**5.7 A sanctioned per-parse state slot for matchers.** Declaring matcher
closures stateless — with per-parse state in `ctx.u` — is what makes
`&'g [Box<dyn Fn>]` matchers hold at the lexer tier and `parse(&self)`
sound for a stateful plugin. Four of twelve matcher-registering repos
already comply (`xml`, `markdown`, `c`, `json5`). One does not, and it is
expensive: `@tabnas/yaml` holds **13** per-parse variables in its
matcher-factory closure (`yaml/ts/src/yaml.ts:283-302` — `anchors`,
`pendingAnchors`, `pendingExplicitCL`, `skipNumberMatch`, `pendingTokens`,
`tagHandles`, `yamlStreamDocs`, `yamlStreamMeta`, `yamlStreamCurMeta`,
`_flowDepth`, `_flowScanPos`, `_inSingleQuote`, `_inDoubleQuote`) across
about 102 reference lines, with a Go twin that threads the same state as
parameters. Worth it only if concurrent `parse(&self)` is a real
requirement.

**5.8 The continuations divergence.** Same grammar
(`top: open([{s:['#A']}]).close([{s:[]}])`), input `"a"`: TypeScript
answers `['#ZZ']`, Go answers `['#A']`. Cause: Go's capture returns early
on `rule == NoRule` (`go/continuations.go:247`) so `haveEnd` stays false;
TypeScript's capture tests only the token (`ts/src/tabnas.ts:541`,
`if (ZZ === tkn.tin)`), so the end-of-source fetch records an empty set,
which TypeScript deliberately treats as "only the end is legal"
(pinned at `ts/test/continuations.test.js:235-245`). Recorded in neither
`DIVERGENCE.md` nor `go/doc/differences.md`, and no shared fixture touches
continuations at all. **Zero fleet callers in either runtime**, so the
cost of choosing is zero today and permanent once a consumer exists. This
is exactly the defect class §4.1 feasibility flags for diagnostic `pos`,
sitting on the one capability the LSP story is built on.

---

## 6. Sequencing

### 6.1 Before anything else: fix the measuring apparatus

Nothing below can be judged honestly until this lands, and it is under a
day.

1. `rm -rf` the destination before `ln -s` in `ci/gate/run-gate.sh:29-31`,
   verify by `md5sum`, and **wire from `ci/bench/run-bench.sh`** rather
   than relying on the gate. Remove the `npm i` step from the proposed
   `ci/workflows/bench.yml`, or accept that it measures npm.
2. Add a non-ASCII fixture to `ci/bench/genfixture.js`, and remove the two
   scratch fixtures sitting in `ci/bench/fixtures/` that the generator
   does not know about.
3. Adopt the paired in-process ABBA rig with the **sign-flip** as the
   decision protocol, and record the null on the *same fixture*
   immediately adjacent to each A/B run. Fresh-process `ci/bench` remains
   fine for tracking; it cannot resolve anything in this document.
4. Re-run the decision set on Node 24. Everything here is Node 22, which
   is off-support, and a candidate whose whole claim is "hoisting three
   property reads is worth 2%" is precisely the kind that can invert
   across a V8 major.

### 6.2 Land before the port starts — canonical repairs, no Rust

In order:

1. **The builtin-config shared fixture** (§3.A2) — on the run-then-push
   and set-then-push shapes, red before A1 and green after. Then the
   `DIVERGENCE.md` entry, then the correction to `go/builtins.go:19-25`.
2. **A1**, the load-time config hoist. Then delete Go's five
   `delete(r.K, …)` calls, which become unnecessary rather than
   load-bearing.
3. **The tail's deletions**: `need`, `closeInfoCache`, instance `Merge`,
   the `text.modify` concatenation. Each is independent and none touches
   the parse path except by removing work.
4. **The tail's declarations**: shallow-snapshot propagation, the
   lookahead bound, the `pid == nid` seeding fixture, three-bags-and-why,
   `@SKIP` as the presence marker, `Info` out-of-band. These are prose
   plus fixtures; they are the cheapest porting-cost reduction available
   and they are worth more than several of the code changes.
5. **`schema/options.json`** (D10) with both exhaustiveness gates. It is
   what stops the manifest going stale the first time someone adds an
   option, and it is the precondition for D11.
6. **Rule §5.1, §5.2, §5.3, §5.5, §5.6 and §5.8.** Five of the six are
   free today.

### 6.3 Land alongside — one release wave, fleet patches pre-staged

C1 through C7, plus D11 and D12, go out together in a single engine major
with the three fleet patches staged and merged the same day: `expr`
(§3.C2 — port `expr/go/expr.go:1385-1461`'s alternate split), `toml`
(§3.C3, one line, plus §3.C4 which fixes its live crash) and `directive`
(§3.C3, about ten lines). Narrow `bnf`/`abnf`'s re-exported `ActionsMap`
in lockstep and rename its third parameter to `next`.

The wave shape is forced by the fleet's build topology, not by taste: the
fleet peer-depends on published npm, only 3 of 31 repos have
`node_modules` installed in this checkout, so none of the other 28 can
even be typechecked against an engine change locally. Every narrowing is
invisible until publish and then lands on all of them at once.

**B1** (the source-immutability hook) can land in the same wave or
earlier; it is one repo and two files, and it is the cheapest of the three
headline changes.

### 6.4 Spikes — cannot be judged without being built

Five, and saying so is the honest answer:

| Spike | Why it cannot be measured yet |
|---|---|
| `configure()` as a pure builder, then freeze `Config` (§3.B3) | A genuine deep freeze is 210/151 today; the freeze is blocked on the rewrite, and the rewrite is the spike |
| Recovery resume-as-position | Touches the main loop in both runtimes, and recovery has **zero** shared-fixture coverage to change under. Ship only the free half — declare the ten `_*` properties as real fields; Go already does |
| The relex save point as a value, and matcher skip-vs-speculate | `lex.relex` is off in every shipped grammar and its only fleet consumer is not buildable in this checkout, so neither path executes |
| Go's `@push$` one-level republish (`go/builtins.go:329`) | Code-cited but not demonstrated: no grammar exists that puts the container two rules above the pushing rule. Unreachable in the fleet today, so the cheap fix is a contract sentence |
| `TabnasError` snapshot-at-raise | The reported +7.2% is an upper bound that includes rendering the candidate keeps lazy; the win is real and large (14,028 bytes retained per held error against 711) |

### 6.5 Must not land mid-port

Two categories.

**Anything that changes the accepted language**, once the Rust leg has
fixtures: the merge-class ruling (§5.2), the IGNORE ruling (§5.3), the
`e`/`h` order (§5.6), the continuations guard (§5.8). Each of these
changes what a conformant runtime must accept, and a port that has
already transcribed one answer will silently keep it — the shared corpus
cannot see any of them today.

**The callback narrowing wave**, because it is a contract change every
runtime must make in the same commit. Landing it mid-port means the Rust
crate implements one signature and the fleet another, with no fixture
able to tell.

`§3.3 risks` already says rulings reverse, so branch from tags. That
applies with particular force to §5.1: A1 and S0 are alternative
resolutions of the same ruling, and only one of them is free.
