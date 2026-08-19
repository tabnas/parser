# Feasibility Report: A Rust Port of the Tabnas Engine

## Summary

A Rust port of the engine is **technically feasible and strategically a
bad idea at this stage**. Feasible, because the two hardest structural
questions already have answers in the tree: the main loop is iterative
over an explicit index stack, so an arena-and-indices design is what the
code already *is* rather than a concession Rust extracts; and the Go port
has already paid, and written down, almost every semantic cost of leaving
JavaScript. A bad idea, because the callback API — the thing plugins are
written against — cannot be ported, only redesigned, and every downstream
grammar in the fleet is written against it.

Concretely:

- **Size.** The port is ~15-18k lines of non-test Rust plus ~12-15k lines
  of test — call it **28-33k lines, 7-10 engineer-months** for one
  experienced engineer, against measured baselines of 9,846 canonical TS
  lines and 13,936 Go lines. That excludes the design work on the
  blockers, and excludes the `json` and `jsonic` grammar ports that
  `ci/parity` and `ci/fuzz` require before a Rust leg can run at all.
- **The blocker is the API, not the language.** The engine hands a
  callback `&mut Rule` and `&mut Context` where `ctx.rule` *is* that rule
  (`ts/src/parser.ts:228`, `ts/src/rules.ts:530`), plus a scratch
  `AltMatch` the Context also owns (`ts/src/rules.ts:1226`). Compiled
  probes confirm this is E0499 and that `Rc<RefCell<Rule>>` aborts at
  runtime on *every* OPEN pass carrying a before-action. The fix — an
  arena with `u32` ids — is sound and idiomatic, and it means
  `r.node[r.u.key] = r.child.node` (`ts/test/json-plugin.ts:142`) has no
  transliteration. Every plugin is rewritten by hand.
- **Divergence risk is low and mostly already settled** — Rust lands on
  Go's already-recorded side of every string/Unicode row for free. But two
  contract documents actively misdescribe the Go runtime's diagnostic
  `pos`, and a port written from them ships a genuine third answer that
  passes every shared fixture. Repairing that is a day's work now and an
  unfixable three-way argument later (§4).
- **Demand does not survive measurement.** The engine runs at ~3 MB/s,
  ~11-12x slower than `encoding/json`; a Rust port at an optimistic 2-4x
  over Go lands one to two orders of magnitude below `serde_json`. The
  cost is in the rule machine, not the runtime. And the one domain where a
  native Rust engine would have a real argument — GBNF constrained
  decoding — is out of scope **by written policy**
  (`ts/doc/gbnf-feasibility.md` §8: "Staying out of the token-masking
  business keeps the scope honest"), not merely blocked by the
  `Continuations` re-parse.

**Recommendation: ship a Rust FFI binding over `go/clib` (option B) now;
gate a serialized-spec-only Rust engine (option C) on a named downstream
consumer; reject the full port (option A).** Be honest about B's ceiling:
the C ABI returns accept/reject plus an error code and a one-line message,
and nothing else — no structured diagnostic, no continuations, no
recovery, no subscribers, no options, and no grammar with custom actions.
That is a real limit, and it is the comparison a port has to beat.

---

## 1. What a Port Must Reproduce

All counts below were re-measured against the tree at the time of
writing (`wc -l`, `grep -c`, `git log --numstat`), not taken from prose.

### 1.1 Implementation

| Subsystem | Canonical TS | Go port | Go/TS |
|---|---|---|---|
| Rule engine | `rules.ts` 2,064 + `parser.ts` 426 + `context.ts` 216 = **2,706** | `rule.go` 1,605 + `parser.go` 1,001 + `recover.go` 513 + `continuations.go` 387 = **3,506** | 1.30x |
| Lexer | `lexer.ts` **1,878** | `lexer.go` 2,558 + `scan.go` 285 + `matchers.go` 144 = **2,987** | 1.59x |
| Utility / merge / builtins / defaults | `utility.ts` 1,358 + `merge.ts` 614 + `builtins.ts` 345 + `defaults.ts` 447 = **2,764** | `utility.go` 1,367 + `merge.go` 847 + `builtins.go` 399 + `options.go` 1,245 + `orderedmap.go` 236 + `text.go` 61 = **4,155** | 1.50x |
| Errors / diagnostics | `error.ts` **727** | `tabnas.go` **525** | 0.72x |
| Public surface / instance | `tabnas.ts` 901 + `types.ts` 870 = **1,771** | `grammarspec.go` 1,296 + `plugin.go` 1,088 + `token.go` 133 + `debug.go` 128 + `util.go` 118 = **2,763** | 1.56x |
| **Total non-test** | **9,846** (11 files) | **13,936** (19 files) | **1.42x** |

The 1.42x expansion is the defensible budget anchor. Rust lands in the
same band or slightly above: arena plumbing, explicit error types, a
hand-written `Value` enum in place of `any`, and `Result` threading are
all net-additive. **15-18k lines non-test.**

### 1.2 Tests and conformance

| Artifact | Measured |
|---|---|
| TS test files / lines / `it()` cases | 42 / 8,967 / **374** |
| Go test files / lines / `func Test` | 57 / 14,342 / **499** (+1 `func Fuzz`) |
| Shared TSV fixtures | 11 files, 295 lines, **265 data rows** |
| — of which cross-runtime parity rows | **254** (`happy.tsv`'s 11 rows are TS-only, exempted in `go/spec_registration_test.go:27-30`) |
| Go test files naming a `ts/test/` counterpart | **30 of 57** — prose coupling, not machinery |

The 254 shared rows are the only thing that transfers mechanically. The
23k lines of existing test code do not: Go's 499 test functions were
already original work written against a prose spec, not translated from
`ts/test/`, and Rust's would be the same. That is a real cost, but it is
not a *Rust-specific* penalty — it is the cost the project already
absorbed once.

### 1.3 Public API and contract surface

| Item | Measured |
|---|---|
| Go exported items | 64 package funcs + 101 methods + 90 types = **255** |
| Go `LexConfig` fields | 88 exported + 4 private caches |
| `TabnasOptions` | 33 top-level groups, 142 field declarations at all depths |
| Base error codes in `schema/error-codes.json` | **10** (`unknown`, `unexpected`, `invalid_unicode`, `invalid_ascii`, `unprintable`, `unterminated_string`, `unterminated_comment`, `unknown_rule`, `end_of_source`, `cancel`) plus a `goOnly` section holding `internal` |
| Structured diagnostic | 15 fields, 14 required, `additionalProperties:false` |
| `$`-builtins | 16, at `BUILTIN_SCHEMA_VERSION = 3` |
| Version locations today | 4 (`ts/package.json`, `ts/src/tabnas.ts`, `go/tabnas.go`, `schema/error-codes.json`) |
| `recover()` sites in non-test Go | **9** (`go/plugin.go` ×4, `go/grammarspec.go` ×2, `go/merge.go`, `go/options.go`, `go/parser.go:349`) |

**Note a documentation defect found while counting.** `AGENTS.md:198-204`
says the base set is *nine* codes and omits `cancel`; so do
`ts/doc/options.md` and `go/doc/api.md`. The registry — the
machine-readable file, gated in both runtimes — carries **ten**. A port
written from the prose ships nine codes and silently lacks the one the
budget/cancellation feature raises (`ts/src/parser.ts:232`). Separately,
`ts/src/utility.ts:110` declares a string constant
`invalid_lex_state: 'invalid_lex_state'` that appears nowhere else in
either runtime — a dead eleventh code name a third port would transcribe.

---

## 2. What Ports Cleanly to Rust

This is a longer list than the blockers, and several items are outright
improvements over both existing runtimes.

**The scan state machine is already written as if for Rust.**
`ts/src/lexer.ts:240-307` and `go/scan.go:33-110` are near-transliterations
of each other: packed `i32` actions (CONSUME / IS_ROW / CI_RESET / STOP
plus a `0xffff` state mask), a `[u8;256]` class table, an `&[i32]`
transition table, a caller-owned scratch output, no allocation and no
dynamic dispatch per byte.

```rust
fn scan(src: &[u8], si: usize, ri: usize, ci: usize,
        spec: &ScanSpec, out: &mut ScanOut) -> bool
```

is a direct third copy, and the three spec builders
(`BuildCharRunSpec` / `BuildLineRunSpec` / `BuildStringBodySpec`,
`go/scan.go:113-221`) port unchanged.

**The matching hot path is pure integer arithmetic.** `parse_alts` tests
tokens against bit-packed 31-bit-per-partition sets —
`hit = 0 !== (Si[part] & ((1 << ((tin % 31) - 1)) | aaBit))`
(`ts/src/rules.ts:1409-1417`) — over tables built once at
grammar-normalisation time and only read during a parse. `Vec<u32>` /
`&[u32]` beats `number[]` with no design work.

**Every dispatch table is a fixed-size array.** `go/lexer.go:89-96`
(`text:[256]uint8`, `fixedFirst:[256]bool`, `fixedTin:[256]int32`,
`start:[256]uint8`) and TS's 257-entry first-char matcher table
(`ts/src/utility.ts:754-802`) are `[u8;256]` / `[i32;256]` with bitflag
start classes — idiomatic Rust and faster than either original.

**`refwd()` becomes free.** `ts/src/lexer.ts:1541-1547` allocates a
remainder substring and memoises it on the cursor; Go already replaced it
with `fwd := l.Src[l.pnt.SI:]`. In Rust `&src[si..]` is a zero-cost
slice, so the entire memoisation apparatus disappears — including the
`BUILTIN_MATCHER` table at `ts/src/lexer.ts:1506-1510` that exists only to
decide who needs it.

**Both internal regexes are already proven removable.** Go's `matchNumber`
(`go/lexer.go:1948-2199`) and `matchText` (`go/lexer.go:2201-2360`) are
hand-written scanners with no regex at all. A Rust port can be regex-free
for every built-in matcher and depend on the `regex` crate only for
user-supplied `match.token` / `match.value` / `value.def` /
`number.exclude`.

**`Tin` wants to be a newtype and already is one in spirit.**
`ts/src/types.ts:627` brands it (`number & { readonly __brand: 'Tin' }`)
with an explicit `asTin` escape hatch; Go weakens it to `type Tin = int`.
`struct Tin(u32)` gives the TS intent with real enforcement.

**Grammar construction and parsing are already separated.** All
normalisation happens in `RuleSpec.norm()` / `normalt` at registration and
clone time; `parse_alts` reads `alt.S` / `alt.t` / `alt.sN` without
writing them. That is what makes `&'g Grammar` tractable (§3).

**The error model needs no exceptions.** The Go lexer never throws:
matchers return `*Token`/nil, errors latch on `l.Err`, and a `#ZZ` winds
the parse down. `Option<Token>` plus a latched `Option<LexError>` is a
direct fit and better than TS's throw-based path.

**The error snapshot design is already built and is exactly what Rust
requires.** TS's `TabnasError` captures the live Context and Rule
(`ts/src/error.ts:79-82`) and pushes itself onto `ctx.errs` — a cycle Rust
cannot express. Go replaced it with `diagSnapshot` / `captureDiag`
(`go/parser.go`), freezing the rule stack, failing rule name and sorted
expected-token names at the raise site. The port target for this
subsystem is the Go file, not `ts/src/error.ts`, and the design work is
already paid for.

**`len` was deliberately defined so it cannot diverge.**
`ts/src/error.ts:167` is `Array.from(tsrc).length`, `go/tabnas.go:516` is
`utf8.RuneCountInString(e.Src)`, and `schema/diagnostic.schema.json:37-39`
spells out why. Rust's `s.chars().count()` is a one-line match. This is
the single best-designed thing in the diagnostic area.

**`serde` is strictly better than what either port has for options.**
`go/utility.go`'s `MapToOptions` is ~450 lines of hand-written
`if v, ok := m["x"].(map[string]any); ok { … }` plumbing, one branch per
option field, and visibly incomplete (function-form fields are commented
as needing the typed Go API). `#[derive(Deserialize)]` on a 29-struct /
131-field options tree replaces all of it, with `#[serde(default)]` giving
the nil-means-default semantics Go emulates with pointer fields.

**`#[derive(Serialize)]` gives byte-identical diagnostics for free.** The
diagnostic is a flat closed struct of `String` / `i64` / `Vec<String>`
plus one two-field nested object. Serde preserves declaration order
exactly as Go's anonymous struct does and TS's object literal does. No
custom serialiser needed.

**`IndexMap<String, Value>` reproduces Go's `OrderedMap` contract** —
insertion order, first-insertion-wins position on re-set — with zero
bespoke code, replacing `go/orderedmap.go`'s 236 lines including its
hand-written `MarshalJSON` / `UnmarshalJSON`.

**Rust's enums model the polymorphic option fields better than Go's
`any`.** `GrammarAltSpec.S/B/A/C`, `CommentDef.Suffix`
(`string | []string | LexMatcher`) and `ErrMsgOptions.Suffix`
(`bool | string | func`) are all runtime type switches in Go and become
tagged enums with exhaustive matching. Same for `CondOp`.

**`relex` / `unrelex` / `speculate` get cheaper.** Go models the save
point as a value struct (`relexPoint{pnt, tokens, end}`,
`go/lexer.go:696-700`) whose `tokens` field aliases the live slice — sound
only because `Relex` nils it immediately at `go/lexer.go:746`. Rust's
`mem::take` / reassign is the same three words with the aliasing hazard
structurally absent, and the restore is infallible by construction rather
than by five hand-matched field assignments
(`ts/src/lexer.ts:1530-1536`, `:1619-1643`).

**Number parsing maps onto std, with one dependency.** `i64::from_str_radix`
and `f64::from_str` cover the decimal and small-base cases, and
`f64::from_str` matches JS coercion on every edge the engine relies on
(measured: `1e999`→inf, `-1e999`→-inf, `1e-999`→0, `1.`→1.0, and `-0`
preserved with `is_sign_negative()` true — the sign bit `ci/parity` once
caught Go losing). The one caveat is real and is covered in §4.4.

**No concurrency inside the lexer, and no mutexes anywhere.**
`grep -n 'sync.Mutex\|sync.RWMutex\|sync.Once' go/*.go` returns nothing
outside tests; the engine's only synchronisation is two atomic counters
(`go/options.go:385`, `go/rule.go:171`), which map to `AtomicI64` with
identical semantics. The "does Rust need a poisoned-lock error code"
question resolves to no, with evidence.

**A whole class of Go bug is unrepresentable.** `go/errormessages_race_test.go`
exists to catch `makeError` writing shared config across concurrent use —
a data race Go needed `-race` to find. In Rust, sharing `&Tabnas` across
threads while mutating its config is a compile error; the test does not
need porting because the failure cannot occur.

---

## 3. What Does Not — Ownership and Aliasing

This section is the reason the answer is "feasible but a redesign". Every
claim here is backed by a compiled probe, not by reasoning about the
borrow checker.

### 3.1 The core aliasing

`ts/src/parser.ts:228` sets `ctx.rule = rule` immediately before
`ts/src/parser.ts:245` calls `rule.process(ctx, lex)`. Inside,
`ts/src/rules.ts:530` computes `let next = is_open ? rule : ctx.NORULE`,
so on the OPEN pass `next === rule`, and `ts/src/rules.ts:556` invokes:

```js
bout = befores[bI].call(this, rule, ctx, next, bout)
```

The same `Rule` arrives as argument 1 and argument 3, alongside a Context
whose `.rule` field is also it. The alternate action at
`ts/src/rules.ts:646` is `alt.a(rule, ctx, alt)`, where `alt` is
`ctx._palt` — a reusable scratch `AltMatch` owned by the Context
(`ts/src/rules.ts:1226`). So an action receives **three mutable handles
into one object graph**, and the engine reads back through them after the
call returns (`ts/src/rules.ts:737`:
`let consumed = rule[is_open ? 'oN' : 'cN'] - (alt.b || 0)`).

One correction worth stating, because it changes what a port must
reproduce: **the arg1==arg3 aliasing is TypeScript-only.** Go's
lifecycle hook is `type StateAction func(r *Rule, ctx *Context)`
(`go/rule.go:104`) — two parameters, no `next`, no `bout` accumulator, no
return value — and its `AltAction` (`go/rule.go:95`) is the same shape.
Go nonetheless reaches the same rule four ways: `ctx.Rule = rule`
(`go/parser.go:473`), `ctx.RS[ctx.RSI] = r` (`go/rule.go:1217`),
`r.Child = next` (`:1223`), `next.Parent = r` (`:1224`). So a Rust port
targeting even Go's narrower callback shape still hits E0499, for the
`ctx.rule` aliasing reason alone.

Two designs that look like workarounds and are not:

```rust
// Probe f2 — the TS callback contract, transliterated.
fn action(rule: &mut Rule, ctx: &mut Ctx);
// call site: action(&mut ctx.rules[i], &mut ctx)
// rustc: error[E0499]: cannot borrow `*ctx` as mutable more than once
```

```rust
// Probe f3 — Rc<RefCell<Rule>>, keeping the shape.
let rule = ...; let next = Rc::clone(&rule);      // OPEN pass: next IS rule
before(&rule.borrow_mut(), &ctx, &next.borrow_mut());
// runtime: thread 'main' panicked: RefCell already borrowed
```

The `RefCell` abort is not a corner case: `next === rule` is unconditional
on OPEN, so it fires on **every** open pass carrying a before-action.

### 3.2 The rule graph is cyclic and every node is on the stack

`ts/src/rules.ts:654-658` pushes the rule onto `ctx.rs` and then makes it
`next.parent`; `ts/src/rules.ts:707` sets `rule.next = next`; replace does
the same with `next.prev = rule`. The `Rule` struct declares all four
links, all seeded to a `NORULE` sentinel and never null. `Rc` cycles leak;
`Box` ownership is impossible when a rule is reachable four ways.

### 3.3 `rule.node` is a shared, in-place-mutated tree

`makeRule(rulespec, ctx, rule.node)` (`ts/src/rules.ts:657`, `:683`) seeds
the child's node with the **parent's node object** — the same allocation.
Actions then write through whichever handle they hold, including
*upward*: `ts/src/builtins.ts:171-177` (`fold$`) does
`const p = r.parent.node; … p.src += own.src; p.kids.push(own)`.
`ts/test/json-plugin.ts:141-142` writes `r.node[r.u.key] = r.child.node`.
Any ownership-moves-to-parent design dies on that upward write.

### 3.4 The grammar must come off the Context — and it is one of four splits

TS stores the grammar on the Context (`ctx.rsm`, read at
`ts/src/rules.ts:655`, `:681`, `:1024`); Go does too (`ctx.RSM`,
`go/rule.go:1214`, `:1245`). That shape is fatal:

```rust
// Probe f1
for alt in &ctx.g.alts { (alt.a)(ctx) }
// rustc: error[E0502]: cannot borrow `*ctx` as mutable because it is
//        also borrowed as immutable
```

Splitting the grammar out resolves it with no `Rc`, no `RefCell`, and no
per-iteration `Arc::clone`:

```rust
fn process(g: &Grammar, ctx: &mut Ctx, rid: RuleId) -> Result<RuleId, Fault>
// `for alt in &spec.open { (alt.a)(ctx, rid, &mut m)? }` compiles as written
```

But the grammar is only the first instance of a general rule: **nothing
the engine iterates, or hands to a callback, may live on the object that
callback also receives.** Four things have to move:

| What | Where it lives today | Where it goes |
|---|---|---|
| Grammar (`rsm`) | `ctx.rsm` (`ts/src/rules.ts:655`), `ctx.RSM` (`go/rule.go:1214`) | `&'g Grammar` parameter |
| Scratch `AltMatch` | `ctx._palt` (`ts/src/rules.ts:1226`), passed at `:646` | a local in `process`, passed `&mut` disjointly |
| Subscriber lists | `ctx.sub.rule` / `ctx.sub.lex`, iterated at `ts/src/parser.ts:239`, `:269`, `:366`, `ts/src/rules.ts:1192`, `:1204`, `:1507`, `ts/src/lexer.ts:1819` | `&'g Subs` |
| Lexer pending queue | `lex.pnt.token`, reached from `ctx.rewind()` at `ts/src/context.ts:178`, `:214` | `ctx.pending: Vec<Token>` |

### 3.5 `Lex` must stay a peer — merging it into `Ctx` does not compile

The obvious move — make `Lex` a field of `Ctx` so `&mut self` covers both
— fails. `Lex` holds the Context as a field (`ts/src/lexer.ts:1514`) and
calls back out with the whole thing:

```js
// ts/src/lexer.ts:1818-1819
if (this.ctx.sub.lex) {
  this.ctx.sub.lex.map((sub) => sub(tkn as Token, rule, this.ctx))
}
```

So the callee needs the *whole* `Ctx`, not a disjoint field, and field
split-borrows do not rescue it:

```rust
ctx.lex.next(&mut ctx)
// rustc: error[E0499]: cannot borrow `ctx.lex` as mutable more than once
//        error[E0499]: cannot borrow `ctx` as mutable more than once
```

Keep `Lex` a peer and pass two disjoint `&mut`s —
`lex.next(ctx: &mut Ctx, rid: RuleId)` — then dissolve the cycle properly
by hoisting the pushback queue and end-token cache onto the Context, as
in the table above. That makes `ctx.rewind()` need only `&mut Ctx`, which
matters because rewind is called from *inside* an action
(`ts/src/builtins.ts:208`, `probeDecide$`) and mutates the exact buffers
`process()` is midway through consuming.

### 3.6 The recommended design

```rust
// Rules and nodes both live in arenas on the Context.
struct Ctx {
    rules:   Vec<Rule>,          // never freed mid-parse; see the cost note
    rs:      Vec<RuleId>,        // the explicit stack ctx.rs already is
    nodes:   Vec<Node>,
    pending: Vec<Token>,         // hoisted off Lex
    end:     Option<Token>,      // hoisted off Lex
    v:       Vec<Token>,         // consumed history
    t:       [Token; LOOKAHEAD],
    errs:    Vec<TabnasError>,   // snapshots, no back-pointer
    // ...
}

#[derive(Copy, Clone, PartialEq, Eq)] struct RuleId(u32);
#[derive(Copy, Clone, PartialEq, Eq)] struct NodeId(u32);

enum Node {
    Undef, Null, Skip, Bool(bool), Num(f64),
    Str(Arc<str>),
    Text { quote: Arc<str>, str: Arc<str> },   // Go's Text wrapper
    List(Vec<NodeId>),                          // children are ids
    Map(Vec<(Arc<str>, NodeId)>),               // ordered, Go-compatible
    Custom(Box<dyn Any>),                       // the plugin escape hatch
}

pub type Action =
    Box<dyn Fn(&mut Ctx, RuleId, &mut AltMatch) -> Result<(), Fault>>;

fn process(g: &Grammar, ctx: &mut Ctx, lex: &mut Lex, rid: RuleId)
    -> Result<RuleId, Fault>;
```

Three notes on this that a casual read gets wrong.

**The callback must carry the `AltMatch`.** Six public callback types take
the matched alternate — `AltAction` (`ts/src/types.ts:724`), `AltCond`
(`:706`), `AltModifier` (`:716`), `AltNext` (`:734`), `AltBack` (`:741`),
`AltError` (`:757`) — and it is not vestigial: every built-in tree/value
builder reads per-alternate config out of it (`ts/src/builtins.ts:127`,
`:138`, `:170`, `:238`, `:249`, `:263`, `:273`, `:305`). A signature of
`Fn(&mut Ctx, RuleId)` silently amputates it. Separately, `alt.h` can
*replace* the whole match object mid-pass — `ts/src/rules.ts:569`
(`alt = alt.h(rule, ctx, alt, next) || alt`), mirrored at
`go/rule.go:1126` — which no such signature can express at all. Budget
`AltModifier` explicitly: either drop it from the Rust surface, or make it
mutate in place rather than return a replacement. Note that Go's
`AltModifier` is handed the **live grammar spec**
(`go/rule.go:1075-1079`), so a Go `H` that mutates its argument corrupts
the grammar for every subsequent parse on that instance — a bug worth
filing independently of the Rust question. `&'g Grammar` makes it
statically impossible.

**Two-node operations still need ceremony.** Id-for-id moves are free —
`r.node[key] = r.child.node` becomes moving a `u32` — but the builtins
that touch two nodes are E0502 on a bare `Vec<Node>`:

```rust
// fold$ (ts/src/builtins.ts:171-177), transliterated: E0502.
// The fix, stable since Rust 1.86:
let [p, own] = ctx.nodes.get_disjoint_mut([pid, nid]).unwrap();
p.src.push_str(&own.src);
p.kids.append(&mut own.kids);
```

Routine, but it is exactly the action that proves the design, so say it
out loud. Note also that the arena needs **no** parent write-back hack:
Go's `builtinPush` has to re-publish the grown slice header with
`r.Parent.Node = r.Node` (`go/builtins.go:329`, one level only) because Go
slices are value types. TS needs none; nor does the arena.

**"Nothing is freed" is a memory decision, not a free lunch.** Rules are
logically freed constantly — pushed at `ts/src/rules.ts:654`, popped at
`:705` — and TS and Go reclaim them by GC. An arena that never frees turns
a working set of O(stack depth) into a retained set of O(input size).
Measured on the repo's own Go strict-JSON fixture: `sizeof(Rule)` is 264
bytes, and 91 / 1,081 / 12,781 / 72,781-byte inputs produce
104 / 1,004 / 10,004 / 50,004 rule passes — a flat **~0.7 rule passes per
source byte**, i.e. ~6.6 MB of never-freed arena for a 73 KB input and
~90 MB for 1 MB, against a live set of a few hundred bytes today. Either
accept that (fine for config files, a hard regression against Go on the
112 KB benchmark), or reuse popped slots — in which case you **do** need
generational indices, because popped rules stay reachable through
`next.parent`, `rule.prev`, `rule.child`, the `ctx.rs` stack, and the
`RuleDone` event that hands `prev` to subscribers after the pass
(`ts/src/parser.ts:269`). Dismissing generations and dismissing the memory
cost cannot both be done at once.

### 3.7 Thread safety: pick `&self`, and do not conflate it with `Sync`

The repo states opposite contracts. `go/options.go:469-471` says "Each
`*Tabnas` instance is itself NOT safe for concurrent Parse calls — one
instance per goroutine, or serialize." But `go/errormessages_race_test.go`
opens with "A single parser instance is shared across concurrent parses
(`Tabnas.Parse` reuses `j.parser`)", and `go/parser.go:865-868` repeats
that rationale to justify a real race fix. (That test exercises
`p.makeError` directly, not `Parse`, so it is a rationale conflict rather
than a test of concurrent parse. `go/concurrency_test.go` covers only
concurrent `Make()`.) Measured behaviour settles it: one `makeJSON()`
instance, 16 goroutines × 100 parses each, mixing successful and failing
parses, is clean under `go test -race`.

The important Rust point is that **receiver mutability and `Sync` are
orthogonal axes.** A compiled probe runs
`struct Tabnas { acts: Vec<Box<dyn Fn(&mut u32)>> }` — no `Send`, no
`Sync` — with `fn parse(&self, s: &str)`, invoked with a closure capturing
`Rc<RefCell<u32>>`, green. So:

- Take `parse(&self)`. It matches what the code already is: the instance
  is read-only during a parse (`ts/src/parser.ts:124` builds the Context
  from `this.rsm` and never writes back), and it does not foreclose the
  shared-parse shape Go already supports in practice.
- Treat `Send + Sync` on plugin closures as a **separate, later, opt-in**
  decision — a `ParallelTabnas` newtype or a feature flag — not a
  consequence of the receiver. The bound is a real tax when imposed:
  a probe confirms `Rc<RefCell<_>>` captures are rejected under it, and
  `ts/test/nlookahead.test.js:142-147` is exactly that shape (an alt
  condition mutating two captured outer `let`s). tree-sitter's `Parser`
  is `Send` but not `Sync`; pest, nom and chumsky put no thread bounds on
  user closures at all.

### 3.8 Panic safety: weaker than Go's promise, but not by much

Go converts plugin faults into a reserved `internal` error code at nine
`recover()` sites, declares the guarantee in
`go/doc/differences.md:474-489`, fuzzes it (`go/fuzz_test.go`), and has
leaked it into the C ABI (`go/clib/core.go:121-125`, consumed by `py/`).

Rust's `catch_unwind` requires `UnwindSafe` (a `&mut Ctx` is not, forcing
`AssertUnwindSafe`), still runs the default panic hook, and — measured —
does nothing under `-C panic=abort`: a probe built that way dies with
SIGABRT, shell exit 134, with `catch_unwind` visibly on the backtrace and
not catching. That is a per-profile choice a *consumer* makes, not
something a library crate ships, and it is not the default.

Two things temper this. First, **Go's guarantee has the identical hole**:
an unbounded recursion inside a plugin callback — the exact surface
`go/plugin.go:101`'s recover guard covers — does not become an `internal`
error; Go dies with `fatal error: stack overflow`. `recover()` catches
neither stack exhaustion, nor OOM, nor concurrent map writes. So
`go/doc/differences.md`'s "never panics" is already an unwind-only
guarantee in substance. Second, the idiomatic Rust answer is *better*:
make every callback return `Result<_, Fault>` and let `?` propagate — free
at runtime, sound, and errors become values rather than control flow.

**Recommendation:** `Result` is the primary channel; `catch_unwind` is a
documented second layer; the guarantee is stated as
"converts panics to errors in `panic=unwind` builds; fatal runtime aborts
— stack exhaustion, allocation failure — are not recoverable, as they are
not in Go either." Two consequences follow: unbounded recursive walks
(`go/rule.go:66-86` `UnwrapUndefined`, `resolveRulePath`, the deep merge)
must be rewritten as explicit worklists, ~40 lines; and the engine's own
code must never index a `&str` directly — `get()` / `char_indices` only —
or the guarantee is lost to the engine rather than to a plugin.

### 3.9 The remaining un-portable JS-isms

These have no Rust spelling and must be declared dead, with tests saying
so. In every case Go has already made the call.

| JS mechanism | Where | Rust answer |
|---|---|---|
| `deep(ctx, parent_ctx)` — deep-merge an arbitrary object into a live class instance | `ts/src/parser.ts:133-134` | Drop it. Go has zero occurrences of `parent_ctx`; no fixture depends on it. |
| `(ctx as any)._field` dynamic stashes (18 in `rules.ts`, 2 in `parser.ts`) | `ts/src/rules.ts:577`, `:631`, `:968`, `:1226`, … | Real struct fields. Go already did exactly this (`go/parser.go:52-118`) at zero cost. |
| Function identity / `fn.toString()` source equality for fnref and alt dedupe | `ts/src/rules.ts:279-310`, `ts/src/merge.ts:336-345` | Key dedupe on the FuncRef **name** (`@node$`, `@pairkey`). `Box<dyn Fn>` has no usable identity: the data half is a fresh allocation per closure, the vtable half is shared by every closure of that type — false negatives *and* false positives. A false negative doubles a grammar's actions; a false positive drops an alternate. |
| Hidden non-enumerable info markers (`Object.defineProperty`) | `ts/src/builtins.ts:57-61` | Go's `MapRef` / `ListRef` / `Text` wrapper structs — sanctioned as a representation split by `doc/value-builtins.md`. |
| `Object.create(null)` prototype-pollution defences | `ts/src/parser.ts:43-50`, `ts/src/utility.ts:295-305` | No-ops in Rust — **but keep the key-dropping half.** `deep()` skips `__proto__` / `constructor` / `prototype` and Go copies the skip deliberately (`isDangerousMergeKey`, `go/utility.go:26-33`). `deep({}, {"constructor":1})` returns `{}` in both runtimes; a Rust port reasoning "HashMap has no prototype" returns `{"constructor":1}` — a third answer, on a path attacker-supplied JSON reaches, and **no fixture covers it**. Three string comparisons. Add a fixture row. |
| Callable-and-indexable members (`tn.token`, `tn.tokenSet`, `tn.fixed`, `tn.options`) | `ts/src/tabnas.ts:197-202` | Split into methods, exactly as Go already did. Zero capability lost. |
| `use()` returning a Proxy-wrapped instance | `ts/src/tabnas.ts:416` | Not offered. Go's `Decorate` / `Decoration` map is the answer. |
| `trimstk` stack-string surgery | `ts/src/error.ts:293-301` | Drop it. Rust errors carry no stack. |
| `mesc()` — boxed `String` with an `esc` flag | `ts/src/utility.ts:570-573` | `enum RePart { Literal(String), Raw(String) }`, keeping `regexp()`'s per-part escaping. |

One hazard the survey work flagged that **deflates on inspection**:
grammar mutation mid-parse via `ctx.inst()`.
`grep -rn 'inst()\|\.Inst()'` returns **zero** matches repo-wide, and Go's
`ctx.Inst` field is used only for read-only token-name resolution
(`go/parser.go:174-190`, `:775`) and to reach `makeErrorIn` during
recovery (`go/recover.go:147`, `:151`, `:268`, `:454`, `:466`) — none of
which mutates the grammar. Freezing the grammar as `&'g Grammar` costs a
capability nothing exercises. It does mean the `&'g` back-reference has to
stay reachable from the recovery path.

---

## 4. Divergence Risk

The repo treats a divergence as a bug until argued otherwise
(`DIVERGENCE.md`), so this section is load-bearing. The verdict per item:

| Item | Verdict | Why |
|---|---|---|
| Lone surrogates in quoted strings | **matches Go** | `char::from_u32(0xD800)` is `None`; `String::from_utf16_lossy(&[0xD800]) == "\u{FFFD}"` exactly. Rust reaches Go's answer for free and cannot reach TS's without WTF-8. |
| Astral-character column positions (`col`) | **matches Go** | `chars().count()` is the natural Rust choice and equals Go's `utf8.RuneCountInString`. |
| Diagnostic `pos` | **matches Go — but only if written from the code** | See §4.1. This is the sharpest trap in the report. |
| Diagnostic `len` | **clean** | Defined in code points on purpose; `chars().count()` is a literal match. |
| Unclaimed-astral `#BD` token `src` | **matches Go**, unavoidably | TS's value is a lone high surrogate (`ts/test/diagnostic.test.js:197-201`); Rust cannot hold it. |
| Bad-token spans for invalid escapes | **matches Go by default; a free choice** | See §4.2. |
| Numbers (decimal, base-prefixed, `-0`, `1e999`) | **clean** — with `num-bigint` | See §4.4. `u128` alone is a silent regression. |
| Serialized regex flags (`i m s` keep, `u g y d` drop, `v` refuse) | **clean** | Rust answers identically to Go and to JS-with-`u` on all 13 rows of the shared table in `go/regexflags_test.go` / `ts/test/regex-flags.test.js`. |
| `\uHHHH` / `\u{…}` / `\x{…}` in serialized patterns | **clean, and cheaper than Go** | Rust's regex accepts all three natively, so `go/utility.go:1238` `jsRegexToGo` has no Rust counterpart. Verified on the bare 4-hex form the real fixture uses (`ts/test/probe-grammar.fixture.json` carries `@/^[\u0041-\u005a]/`, not `[A-Z]`). |
| `\s` / `\d` / `\w` in user regexes | **would add rows — fix at lowering instead** | See §4.3. |
| Invalid UTF-8 input bytes | **promotes a "not a divergence" into one** | See §4.5. |
| Parsed-object key order | **unadjudicated today** | See §4.6. |
| `str()`, `strinject`, `modlist` | **unadjudicated today** | See §4.7. |

### 4.1 `pos` — two contract documents describe a Go that does not exist

Measured against both built engines with the shared strict-JSON grammar:

| Input | TS `pos` | TS `col` | Go `pos` | Go `col` |
|---|---|---|---|---|
| `["é" 2]` | 5 | 6 | **6** | 6 |
| `["😀" 2]` | 6 | 7 | **8** | 6 |

Go's `pos` is a **UTF-8 byte offset** — it is `Point.SI`
(`go/lexer.go:482` initialises `Point{Len: len(src), SI: 0, …}` with a
byte length, and `l.Src[l.pnt.SI:]` slices with it). It is not a rune
count. But `schema/diagnostic.schema.json:35` says "TypeScript counts
UTF-16 units, Go counts runes — the same divergence class as col", and
`DIVERGENCE.md` repeats it: "a 0-based offset in UTF-16 units (TS) versus
runes (Go)". Both are wrong. (`go/doc/differences.md:464` says "Error
columns | UTF-16 units | Runes", which is about `col` and is correct; it
says nothing about `pos` at all, so it needs an *addition*, not a
correction.)

Two consequences. First, `pos` diverges for **any** non-ASCII character,
not only astral ones — the `é` case diverges on `pos` while agreeing on
`col`, which the recorded divergence explicitly does not predict. Second,
and worse: a Rust port written from the documents uses `chars().count()`
for `pos`, giving **5** for `["😀" 2]` where TS gives 6 and Go gives 8 — a
genuine third answer — and it **passes all 10 rows of
`test/spec/diagnostic.tsv`**, because that fixture's only astral row
deliberately places the character *inside* the failing token, as its own
comment at lines 15-17 states.

**Fix before any Rust exists:** correct the two documents to say bytes,
add a `pos` note to `go/doc/differences.md`, and add one fixture row with
a BMP non-ASCII character *before* the failing token (`["é" 2]`, where
`col` agrees at 6 and `pos` differs 5-vs-6) with opposite assertions in
`ts/test/divergence.test.js` and `go/divergence_test.go`, mirroring the
pattern already used for `TestDivergenceBadEscapeSpanIncludesQuote`. That
is roughly 30 lines now.

### 4.2 Bad-token spans — the one genuinely free choice

TS's string matcher writes the cursor back to the escape before raising:

```js
// ts/src/lexer.ts:1346-1350
sI = sI - 2; cI -= 1; pnt.sI = sI; pnt.cI = cI
return lex.bad(S.invalid_unicode, sI, sI + 6)
```

Go never moves the point during the body scan — every `l.bad(...)` site in
the string matcher starts at `l.pnt.SI`, the opening quote
(`go/lexer.go:1806`, `:1812`, `:1836`, `:1846`, `:1863`, `:1869`, `:1878`,
`:1897`, `:1925` — nine sites, all identical in that respect). Neither is
forced by the string model; both are one line in Rust. But the port's
natural shape lands on Go's side, because the scan driver already carries
a local `si` cursor and leaves `Lex.pnt` alone — reproducing TS needs a
deliberate write-back. Follow Go and the table stays 2-vs-1
(`TS | Go+Rust`). Honest note: this is the entry a third runtime should
force a *fix* on rather than triple, since the current argument
("display-only gain") weakens with each runtime that carries it.

### 4.3 `\s` / `\d` / `\w` — avoidable at lowering, not a blocking precondition

Measured across all three engines:

| Pattern | Subject | JS (`u`) | Go RE2 | Rust `regex` |
|---|---|---|---|---|
| `^\s$` | U+00A0, U+2028, U+2029, U+3000, VT | true | **false** | **true** |
| `^\s$` | BOM (U+FEFF) | true | false | false |
| `^\d$` | U+0663 | false | false | **true** |
| `^\w$` | U+00E9 | false | false | **true** |

So Rust sides with TS on `\s` (where `go/doc/differences.md:444-447`
already records the non-equivalence) and agrees with **neither** runtime
on `\d` and `\w` (which are recorded nowhere). Also unrecorded: `(?m)`
line boundaries (JS honours `\r`, U+2028, U+2029; Go and Rust honour only
`\n`) and bare `.` (JS excludes `\r`, U+2028, U+2029; Go and Rust do not).

This is a property of the port's **lowering**, which the port controls,
not an inherent third answer — and there is in-tree precedent for
normalising at lowering time (`jsRegexToGo`, `go/utility.go:1238`,
rewrites JS `\uHHHH`/`\u{…}` escapes on the serialized-regex path). Rewrite
`\d`→`[0-9]`, `\w`→`[0-9A-Za-z_]`, `\s`→`[\t\n\f\r ]` in ~15 lines and
Rust matches Go exactly. One trap worth writing into the porting guide:
the tempting POSIX shortcut `[[:space:]]` does **not** work — measured,
Go RE2's `\s` does not match U+000B while Rust's `[[:space:]]` does, so
the class must be spelled out.

Also mandatory for the same reason Go documents: **refuse unknown flags.**
Rust reproduces the `(?U)` swap-greedy trap exactly — `(?U)^a+` compiles
and matches `"a"` where plain `^a+` matches `"aaa"` — while `(?v)`,
`(?g)`, `(?y)`, `(?d)` all fail to compile. And Rust's regex, like RE2,
rejects lookaround and backreferences; the engine and its fixtures use
none (`grep -nE '\(\?=|\(\?!|\(\?<=|\(\?<!|\\[1-9]' ts/src/*.ts` returns
zero hits across all 11 files, and the only repo occurrences are two Go
tests asserting refusal).

### 4.4 Numbers — clean, but `u128` is not enough

TS and Go agree exactly on every measured case, including well past
`int64`, thanks to commit 500a497 and `exactBaseFloat`
(`go/parser.go:902-918`, `big.Int` → `big.Float` → `Float64`). Rust
reproduces all of it with `num_bigint::BigUint::parse_bytes(...).to_f64()`:
9.223372036854776e18, 1.8446744073709552e19, 3.402823669209385e38,
1.461501637330903e48 and 1.6069380442589903e60, all identical to both
runtimes, saturating to `inf` on a 400-digit literal like JS.

**`u128::from_str_radix` returns `None` at 40 hex digits.** An `i64`+`u128`
port silently drops those literals to `#TX` — a new divergence, and
exactly the class of bug #74 fixed. Take the arbitrary-precision
dependency.

The `"inf"` / `"nan"`-parse-successfully hazard is **not reachable**
through the built-in matcher: the number candidate is capture group 1 of
the ender regex built at `ts/src/lexer.ts:1044-1060`, whose alternation
admits only sign, base prefix, digits, dot and exponent; Go's hand-written
`matchNumber` starts only on a digit or sign. No alphabetic-only run
reaches the converter.

While here: `DIVERGENCE.md:113-115` ("Native integer type. Go returns
`int64`, TS a `number` or `bigint` depending on magnitude") is **stale on
both halves** — `grep -rc 'bigint\|BigInt' ts/src/*.ts` is zero for every
file, and Go's `Token.Val` for `0xFF` is `float64`
(`go/doc/differences.md:499` agrees). Delete or rewrite it before a third
port builds on it.

### 4.5 Invalid UTF-8 — a Rust port promotes a non-divergence into a divergence

`DIVERGENCE.md`'s "Not divergences" section files invalid UTF-8 input
under *not* a divergence, on the grounds that Go passes it through
byte-for-byte and never panics while "the question does not arise in TS".
`go/fuzz_test.go:15` seeds the corpus with `"a:\xff\xfe"` and
`"\xed\xa0\x80"`.

Rust cannot represent such input in a `String` at all: `from_utf8` rejects
it, `from_utf8_lossy` rewrites it. Worse, every `&src[si..]` panics if
`si` is not a char boundary, and a plugin matcher that advances the cursor
by a byte count — which the matcher contract permits — can put it there,
converting a Go no-op into a Rust abort.

The options are (a) scan `&[u8]` throughout and carry `Token.src`/`val` as
`Cow<'a,[u8]>`, preserving Go's acceptance set and making boundary panics
impossible but handing callers bytes; or (b) take `&str` and reject
invalid UTF-8 at the door — clean, idiomatic, and a documented behaviour
break plus a hole in the fuzz guarantee. Either way, **the mere existence
of a Rust port retroactively invalidates an existing parity
classification.** That is the structural finding: adding a runtime is not
additive to `DIVERGENCE.md`; it re-partitions it.

### 4.6 Parsed-object key order — unadjudicated, and unpinned

Measured with the *same* shared cross-engine grammar
(`ts/test/json-builder.fixture.json`), input `{"b":1,"2":2,"a":3,"10":4}`:

- TS: `{"2":2,"10":4,"b":1,"a":3}` — JS own-property order, integer-like
  keys ascending first.
- Go: `{"b":1,"2":2,"a":3,"10":4}` — insertion order via `*OrderedMap`.

`doc/value-builtins.md` calls that fixture "byte-identical to
`JSON.parse` / `encoding/json`, pinning TS↔Go value parity", but the Go
runner flattens through `omPlainify` to compare against the unordered
oracle, and the fixture's own inputs are alphabetical non-integer keys, so
the divergence never fires. `DIVERGENCE.md` has no key-order entry.

Rust's natural choice (`IndexMap`) gives insertion order — Go's answer,
not the canonical runtime's. Emulating TS exactly requires implementing
the ECMAScript array-index partition, for a behaviour the engine's own
author added a `map.ordered` option to work around. **Adjudicate this
before a third port, not after.**

### 4.7 The rest of the unpinned surface

Four more behaviours are already split between TS and Go, unrecorded, and
uncovered by the 175 utility fixture rows. Each forces a Rust author to
guess:

- **`str()`**: `str(1e21)` → TS `"1e+21"`, Go `"1000000000000000000000"`,
  naive Rust the same as Go; `Infinity` → TS `"Infinity"`, Go `"+Inf"`,
  Rust `"inf"` — three answers to one input. Truncation unit: TS slices
  UTF-16 units, Go slices **bytes** and can emit invalid UTF-8
  (`Str("ααααα", 8)` returns a string with a truncated multi-byte
  sequence); Rust literally cannot do that. `utility-str.tsv`'s 23 rows
  are ASCII-only.
- **`strinject`**: six measured TS/Go differences in placeholder charset
  and object rendering, none in `utility-strinject.tsv`'s 22 rows. TS
  renders objects by string-hacking `JSON.stringify` output; Go carries
  *two* renderers and picks the TS-compatible lossy one only for
  `{details}`.
- **`modlist`** move semantics: `modlist(['a','b','c'], {move:[-5,0]})` →
  TS `["a","c"]`, Go `["b","a","c"]`, because JS `%` keeps the dividend's
  sign and Go uses a true modulo. Rust's `%` truncates like JS, so a
  literal transliteration reproduces the TS bug *or* panics.
- **The `util` bag**: Go's `Keys`/`Values`/`Entries` sort
  (`go/util.go:31-56`); TS's are thin wrappers over JS property order.

None of these is hard. All of them land on one person's desk before a
third runtime can be written honestly.

---

## 5. The Parity Contract and CI

### 5.1 What a third runtime must implement

Joining `test/spec` is cheap; joining the *contract* is not, because
almost none of it is shared code.

| Obligation | Cost |
|---|---|
| TSV loader | ~40 lines (`std::fs`, `split('\t')`, a 4-arm unescape, `serde_json`) |
| Per-fixture runners (11 fixtures, 3 column conventions, 2 comment conventions) | ~200 lines |
| Strict-JSON grammar test fixture | ~250 lines, mirroring `ts/test/json-plugin.ts` / `go/jsonplugin_test.go` |
| Three opposite-assertion divergence pins | 3 test files |
| Registry staleness test | ~40 lines, mirroring `go/schema_test.go:118-213` |
| Version drift test | ~40 lines, mirroring `go/version_test.go` |
| Alt-key gate | Rust has no reflection; a hand-maintained `const ALT_KEYS: &[&str]` plus a serde-field assertion, ~20 lines |
| `tokdump` binary for `ci/parity` | ~150 lines |

**There is no shared loader — there are four decoders with three escape
policies.** TS unescapes *every* column at load (`ts/test/utility.js:30`).
Go's `loadTSV` unescapes nothing; each runner decides — `runParserTSV`
does column 0 only, `go/lexer_optionplumbing_test.go` does columns 1 and 2
but not 0, `go/utility_spec_test.go` never unescapes at all. `py/` is a
fourth. `test/AGENTS.md` asserts "this repo decodes `\n`, `\r`, `\r\n`
and `\t` in EVERY column" — true of TS only. The invariant holds today
only because no fixture happens to put an escape in a column Go does not
decode. A Rust author reading the prose implements the TS policy and is
silently wrong against Go's utility runner the first time anyone adds a
`\t` to a `utility-strinject` template. **Extract one loader spec before
porting**, and fix the Go runners to match — it is a small change and no
fixture moves.

Related: nothing guards row counts. `go/utility_spec_test.go:63-66`
catches an *empty* fixture and `go/diagnostic_spec_test.go:69-71` catches
`ran == 0`, but nothing catches "ran 4 rows instead of 10", which is
precisely what a subtly-wrong third loader produces. Every runner in every
runtime should assert the exact expected row count. One line each; worth
doing whether or not Rust happens.

### 5.2 The honesty gate is binary and lives in the wrong suite

`go/spec_registration_test.go:32-62` globs `test/spec/*.tsv` and greps the
Go and TS test sources for each fixture's name, with a `nonParity`
`map[string]string` exemption (one reason per fixture). Three things break
at once with a third runtime:

1. The enforcement point for Rust's obligations would live in the **Go**
   test suite — `go test ./...` goes red because a Rust file is missing.
2. Adding `inRust` fails all 10 parity fixtures simultaneously; there is
   no staging set, so the port is all-or-nothing on the corpus.
3. The exemption model is per-fixture, not per-runtime. `happy.tsv` is
   exempt because *Go* cannot parse it; with three runtimes you need
   "exempt from Rust, required in TS and Go", which `map[string]string`
   cannot express, and the `inGo && inTS` honesty condition has no obvious
   three-way generalisation.

Generalise `nonParity` to `map[string]map[string]string` **before** Rust
exists, or move the gate out of both languages into `ci/gate/`.

### 5.3 The registry and version machinery are hard-coded to two runtimes

`schema/error-codes.json` carries a literal `goOnly` section holding
`internal`, emitted by a hard-coded literal in the TS generator
(`ts/tools/gen-error-codes.js:33`) — the generator runs on the TS engine,
which has no such code, so the entry is hand-transcribed from Go source.
`go/schema_test.go:185-188` asserts no code appears in two sections. Adding
Rust means either doubling that transcription pattern with a `rustOnly`
key, or restructuring to a per-entry `runtimes: ["ts","go","rust"]` array
— roughly 150 lines across the generator, the registry, both schema tests
and `schema/README.md`, and a breaking change to a file the README calls a
contract.

The version coupling is worse. `AGENTS.md:100-114` documents four
locations; Rust makes six (`Cargo.toml` plus a `pub const VERSION`), and
the registry embeds **one** engine version, so a Rust-only patch — a
clippy fix, an MSRV bump — requires bumping `ts/package.json`,
`ts/src/tabnas.ts`, `go/tabnas.go` and regenerating the registry, i.e. a
full npm OIDC publish and a Go tag for a change in neither runtime. That
is already three package registries with one version number, and
crates.io versions are **immutable** — where npm has a 72-hour unpublish
window. The project already burns numbers: `v0.8.9` was committed
(`a3f286b`) and tagged in neither runtime; remote tags jump `go/v0.8.8` →
`go/v0.8.10`. Tag sets are already asymmetric (23 `go/v*` vs 19 `ts/v*`).

`Makefile` has `publish-ts` (:28-30) and `publish-go` (:42-55) and no
third lane; `.github/workflows/release.yml` fires only on `ts/v*`. And
release plumbing is not editable from this repo: `.github/workflows/ci.yml`
is a 17-line caller into `tabnas/.github/.github/workflows/polyglot-ci.yml@main`
shared across the fleet (`README.md:354-375` lists 22 packages, 21 of them
downstream consumers), and its own header records that "session credentials
cannot write `.github/workflows/*`; admin DECISIONS.md ADR-8". The
version-rewriting orchestrator (`admin/publish.sh`, cited by
`go/version_test.go:41` and `ts/test/version.test.js:24`) is in a third
repo, and its file discovery already missed a constant once — which is why
those two tests exist.

### 5.4 The differential harnesses cannot take a Rust leg at all

This is the hard one. The engine ships no grammar, so every token- and
value-level differential harness is driven through a downstream grammar:

- `ci/parity/gotokdump/main.go:20-22` imports
  `github.com/tabnas/json/go` and `github.com/tabnas/jsonic/go`;
  `ci/parity/tokdump.js:44-50` requires `json/ts` and `jsonic/ts`.
- `ci/fuzz/run-diff.sh:30` builds `./cmd/tabnas-json` from `$ROOT/json/go`.
- `ci/bench/gobench/bench_test.go` and `ci/bench/bench.js` likewise.
- `ci/gate/run-gate.sh` runs six suites: parser/ts, parser/go, json/ts,
  json/go, jsonic/ts, jsonic/go.

Neither `json` nor `jsonic` exists in Rust, and running
`ci/parity/run-parity.sh` against this repo's own corpus is not a
substitute (it always takes column 0 as the input, but
`lex-string-control.tsv` column 0 is an option value). So the tier that
found the three real engine bugs during bring-up — the `__proto__`
prototype-chain leak, Go normalising `-0` to `+0`, and
`unterminated_string` vs `unprintable` — is unavailable to a Rust port
until at least `json` (a grammar plus a CLI) is also ported. **Budget the
`json` Rust port as part of the estimate, not after it.**

Note the topology is favourable in one respect: parity is a **star**, not
a mesh. `AGENTS.md` makes TS canonical and Go a follower, so Rust needs
TS-vs-Rust comparisons only — `ci/parity/run-parity.sh` already writes
`ts.tok` and `go.tok` and does one `cmp -s`; a third redirect and a second
`cmp` is a ~10-line change. And `ci/gate/fixture-sync.sh` needs **zero**
change: it compares corpora, not runtimes.

### 5.5 What is genuinely cheap

- The suites are fast: `npm test` ~2.8s (388 tests), `go test ./...` 0.6s
  cold. A `cargo test` over a ~15k-line crate lands in the same class. CI
  wall time is dominated by npm install and the downstream closure build,
  and a Rust job adds nothing to that path.
- There is **no downstream Rust closure**, so a Rust job cannot suffer the
  red-main trap `AGENTS.md:116-131` documents (a sibling failing to
  compile against an unpublished engine). Its redness would always mean
  the engine is actually broken — the only unambiguous CI signal in the
  repo.
- `ci/` is staged, not wired: `ci/README.md` opens with "nothing is wired
  into `.github/` yet". A Rust arm can follow the same pattern
  (`ci/rust/`, `ci/workflows/rust.yml`) and be proven locally long before
  anyone must edit the fleet-wide workflow.
- The bench harness imposes no obligation — `ci/workflows/bench.yml` is
  self-described as PROPOSED, advisory, artifact-only, and never gates.
  Worth noting the decision hazard anyway: a Rust bench arm would very
  plausibly be several times faster than Go and an order of magnitude
  faster than TS, and once those numbers exist as an artifact there is
  organisational pressure to treat Rust as the reference — which
  `AGENTS.md` authority rule 1 forbids and which would invert the whole
  canonicality structure. If the bench arm is built, write the ADR first.

---

## 6. The Maintenance Tax

### 6.1 The measured per-feature port cost

Six features landed in TypeScript in a five-minute window on 2026-08-17
(18:12:08–18:17:15) and were ported to Go over the following ~3.3 hours
(19:13:22–21:30:32) — the same person, the same sitting, a clean
controlled measurement.

| Feature | TS commit (ins) | Go commit(s) (ins) | Go/TS |
|---|---|---|---|
| `ctx.errs` | `40e7712` #94, 202 | `c4038d9` #102, Go side 230 | 1.14x |
| error recovery | `2b21c8d` #96, 704 | `308db48` #106 1,173 + `bf50ac8` #108 99 + `70773f1` #109 205 = 1,477 | 2.10x |
| budget / cancel | `af5c1ae` #97, 143 | `da8c10e` #104, 319 | 2.23x |
| ruleDone sub | `ed5355a` #98, 295 | `29cf7df` #105, 559 | 1.89x |
| unrelex re-announce | `c239b61` #99, 141 | `9081646` #103, 197 | 1.40x |
| continuations | `ac6deba` #100 279 + `05afc97` #101 290 = 569 | `72e19e2` #107, 717 | 1.26x |
| **total** | **2,054** | **3,499** | **1.70x** |

Aggregated over the whole post-import history (51 commits on top of the
45,115-line squashed root `22fdf19`): `ts/` +4,523/−122, `go/`
+7,942/−465 — **1.76x**. Broken down, 1,706 lines of canonical `ts/src`
churn produced 2,984 lines of Go implementation, 3,983 lines of Go test
and 716 lines of `go/doc/differences.md`: **every line of canonical engine
source that changes costs ~4.5 lines of port work.** 26 of the 51
post-import commits (51%) touch both trees.

**Be honest about what these numbers do and do not show.** The six-feature
ratio of 1.70x is *below* the 1.76x historical aggregate — so on this
metric, the six most recent `Rule`/`Context`-touching features were
marginally *cheaper* to port than average. The "recent features are the
expensive ones" story is not supported by the data. What the data does
support is the 4.5x figure and the observation that the project keeps the
ports in step by brute force, inside hours, by one person in one sitting.
That works at two.

### 6.2 The catch-up pattern

`feat(go):` commits read as if Go is ahead. It is not. `70773f1` (#109)
replaced a section of `go/doc/differences.md` titled "Known gap: per-run
vs per-recovery diagnostics" with "Aligned", and `git log -S "lexer soft
mode" -- ts/src/rules.ts` shows the TS side arrived in `2b21c8d` (#96). So
#106, #108 and #109 are three commits and 1,477 lines closing **one** TS
feature, with a named gap test (`TestRecoverCascadeParityGap`) sitting in
the tree meanwhile. The genuinely Go-only work is `94c2cf1` (the C ABI +
Python binding), `83e8e23` (perf) and one Go-specific data race.

### 6.3 Who pays it

`git log --format='%an'` over all 52 commits: **49 by Richard Rodger, 3 by
"Claude" (release bumps)**. Bus factor 1. The throughput is
agent-amplified — 52 commits and ~12,500 lines of tracked churn in nine
days — and that is exactly what makes the current two-runtime cadence
survivable.

But agents accelerate **typing**, not **adjudication**. §4.6, §4.7 and
§4.1 enumerate roughly a dozen currently-unpinned behaviours a third port
forces someone to decide first — parsed key order, `strinject` rendering,
`modlist` move, `str()` units, diagnostic `pos` units, the nine-vs-ten
code set. Every one lands on the same person, and none of them is
mechanical: `93b3426` (#78) shows the adjudication going the "wrong" way
against the stated authority rule, fixing the *canonical* runtime because
"Go is the reference" for base-prefixed integer runs.

`go/doc/differences.md` is 780 lines and is touched by 23 of 52 commits.
Under `AGENTS.md` alignment rule 4, a Rust port needs its own equivalent,
maintained at the same cadence.

---

## 7. Options and Costs

| | **A — Full Rust port** | **B — Rust FFI binding over `go/clib`** | **C — Serialized-spec-only Rust engine** | **D — Wasm** |
|---|---|---|---|---|
| **Delivered capability** | Everything: plugins, custom matchers, subscribers, options, recovery, continuations, structured diagnostics, `no_std`/wasm targets, native speed | Accept/reject on precompiled function-free specs, plus an error `code` and a one-line message. Nothing else crosses the boundary. | The full engine minus code-valued extension points: all 16 `$`-builtins, declarative conditions, serialized regex terminals, options, diagnostics, recovery. The whole BNF/ABNF/GBNF front-end closure works. | Same as B, in a sandbox |
| **Cost** | ~28-33k lines, **7-10 engineer-months**, plus the `json` (and ideally `jsonic`) Rust ports before `ci/parity` and `ci/fuzz` can run | **~1 week.** A working binding is ~94 lines over `libtabnas`; `py/` (206 + 184 lines) is the shipped precedent, delivered in a single commit (`94c2cf1`) | ~10-14k lines, **~4-6 engineer-months**. Cuts Go's 255 exported items to ~70 but only ~10% of the implementation — the lexer, rule engine, config resolution, error rendering and deep-merge all survive | Days, on top of B's work |
| **Measured evidence** | Baselines: ts/src 9,846, go non-test 13,936 (1.42x), go tests 14,342 | 55 shared rows pass accept/reject; 2.72 MB/s vs ~3.1 MB/s in-process Go (~15% overhead); `-buildmode=c-archive` links statically into a 3.6 MB self-contained Rust binary with only libc/libgcc dynamic | A function-free serialized strict-JSON spec (`number.exclude` as `"@/^00/"`) passes include-json 34/34, include-json-utf8 12/12, include-json-errors 4/4, include-json-utf8-errors 5/5 and diagnostic **10/10 with full value and structured-diagnostic comparison**. `probe-grammar` and `eager-literal` fixtures — real BNF-compiler output with probe dispatch, regex terminals and eager matchers — parse with zero closures | `GOOS=wasip1`/`GOOS=js go build ./...` compile the engine; **`go/clib` fails, because cgo is unavailable under wasm**. A `//go:wasmexport` reactor builds at 4.7 MB |
| **Failure modes** | Plugin API is a redesign, not a port; every downstream grammar rewritten by hand. Inherits every blocker in §3. Forces `DIVERGENCE.md`, the registry and the honesty gate to three runtimes. Triples the adjudication load on a bus-factor-1 maintainer | Go runtime in every consumer binary; no `no_std`; no wasm; darwin needs a native build host; the `os.fork` hazard `py/README.md:38-40` documents; two format clibs cannot be statically linked into one binary. **And the capability ceiling above is the real limit** | Still needs the string/number/regex adjudications (§4). The 175 utility fixture rows exercise `deep`/`modlist`/`str`/`strinject` — exported *utility* APIs a trimmed surface may not retain, so "passes all 254 rows" is asserted, not measured. Still cannot join `ci/parity`/`ci/fuzz` without the `json` port | Drags a multi-MB wasm runtime into the Rust binary, marshals through linear memory, keeps the Go GC, and gives up the native-speed argument that was the point |

Two clib defects worth fixing regardless, both cheap:

1. `go/clib/core.go:115` is literally `_, err := g.tn.Parse(src)` — the
   parse result is discarded — even though the canonical ADR-12 header at
   `go/clib/include/tabnas.h:17-18` specifies that accepted input returns
   `{"ok":true,"accept":true}` *plus* a `"value"` field where the parse
   result is JSON-representable. Adding it is a few lines and lifts the
   validation-only limit for free. (`*OrderedMap` already carries an
   insertion-order `MarshalJSON`, `go/orderedmap.go:99-110`.) Caveat: it
   creates a new, currently-unpinned cross-runtime surface, since TS's
   `JSON.stringify` orders integer-like keys first.
2. The version document shape diverges from the header:
   `go/clib/core.go:57-59` returns `{"ok":true,"version":…}` where
   `tabnas.h:35` specifies `{"ok":true,"lib":…,"format":…,"template":…}`,
   and the artifact manifest's `lib` pattern (`^libtabnas[a-z0-9]+$`) does
   not admit the bare `libtabnas` name. (The `tabnas_grammar_json` vs
   `tabnas_grammar` symbol difference is **not** a defect: the header
   describes the per-format libraries, and `go/clib` is deliberately
   grammar-agnostic because this repo ships no grammar, so it needs a
   spec-taking entry point the per-format template has no slot for.)

### 7.1 On demand

The demand side does not survive measurement. The engine runs at ~3 MB/s
(~300 ns/byte, linear from 2.6 KB to 180 KB), **~11-12x slower than
`encoding/json`**. A Rust port at an optimistic 2-4x over Go lands near
10 MB/s — still one to two orders of magnitude below `serde_json`. "Rust
for speed" is not an argument here; the cost is in the rule machine.

The strongest demand-side case would be GBNF constrained decoding, and it
is **out of scope by written policy**. `ts/doc/gbnf-feasibility.md` §8,
under "Skip, deliberately": *"Sampler integration. Tabnas constrains
nothing at inference time; it authors, validates, and exports the grammars
that samplers consume. Staying out of the token-masking business keeps the
scope honest."* The documented GBNF demand (§5 of that report) is offline
validation and authoring — "Nothing exists in Go" — and §10 step 3(b)
records that the GBNF *text* compiler is TS-only, so Go users cannot even
supply `.gbnf` text yet. A Rust engine serves none of that; a Rust GBNF
story would need a third front-end compiler port on top of the engine
port.

The architecture agrees. `Continuations` re-parses the whole prefix per
call (`ts/src/tabnas.ts:501`, `go/continuations.go:235`) — measured at
52 µs for a 20-byte prefix, 357 µs at 200 bytes, **1.37 ms at 2 KB**, i.e.
O(prefix) per call and quadratic across a generation — and returns token
*names* (`#CS`, `#CA`), not a vocabulary mask over a scannerless byte
alphabet. A Rust port of this architecture would be equally unusable. If
constrained decoding ever becomes the goal, the right project is an
incremental/resumable parse API with a byte-level acceptance mask — **in
TypeScript first, since TS is canonical** — and that work is worth doing
regardless of whether Rust ever happens.

One honest counterpoint: `ts/doc/gbnf-feasibility.md` §7.7 names astral
`\UXXXXXXXX` character classes as "the one place GBNF support may need an
engine-side change", because the serialized-regex loader copies flags
verbatim and RE2 rejects `(?u)`. Rust's regex accepts `\x{…}` and
`\u{…}` natively and is Unicode-mode by default. That is a small, real
advantage — and the only GBNF-shaped argument for Rust anywhere in the
repo.

One argument that should **not** be made: "there are no Rust plugins, so
there is nothing to validate against." It is circular and proves too much.
Go's own downstream closure is exactly two grammars (`json`, `jsonic`) out
of 22 packages in `README.md:354-375` (21 of them downstream consumers),
all TS+Go; the repo has one issue, opened by the maintainer, and zero
external contributors across nine days of public history. The same
argument at Go's adoption would have blocked Go. The sound version is an
**option-value** argument: a third runtime multiplies the cost of every
future change against demand that is currently unmeasured for every
runtime, including Go's.

---

## 8. Recommendation

**Do B now. Gate C on evidence. Reject A. Do not pursue D except as a
sandboxing story.**

### 8.1 Immediately (~1 week): the Rust crate over `go/clib`

Ship it exactly as `py/` is shipped. It adds **zero** version locations
(read the version from the library, as `py/tabnas.py:138-142` does), zero
`DIVERGENCE.md` columns, zero registry sections, zero release artifacts in
this repo, and zero parity obligations — while giving Rust callers a real
Rust API. `go/clib/README.md` states the design intent outright: the C ABI
exists "so languages with no tabnas port can still use it", and
`tabnas.h:24-26` notes that C, C++, Zig, Swift, Nim and D need no binding
layer at all. Rust and Python are precisely the two languages that need a
thin one. This is the architecture executing as designed, not a
workaround.

Before it lands, fix the two clib items in §7, and **document B's ceiling
honestly**: accept/reject plus a code and a one-line message; no
structured diagnostic, continuations, recovery, subscribers or options;
serialized function-free specs only. If a Rust consumer needs the LSP-shaped
surface or wants to author grammars in Rust, B does not serve them at any
price — and that, not throughput, is the comparison C has to win.

### 8.2 Gate C on a named downstream consumer

A serialized-spec-only Rust engine is the only port worth considering, and
the evidence for it is stronger than it first appears: a function-free
spec passes all 65 parser-facing and diagnostic rows with full value and
structured-diagnostic comparison, and the entire BNF/ABNF/GBNF compiler
output family is already function-free, so C would ship with that
ecosystem working on day one. It also removes four blockers outright —
config mutation, plugin closures, closure identity, and the no-panic
guarantee (with no user callbacks, `internal` can be reserved and unused).

But it is still ~10-14k lines and ~4-6 engineer-months, and "passes all
254 rows" is not measured — the 175 utility rows exercise exported
utility APIs a trimmed surface may not even retain. Require a named
consumer first, and resolve before any code:

1. The **`pos`-units repair** (§4.1) — two documents corrected, one
   addition to `go/doc/differences.md`, one fixture row. Without it a port
   written from the documents ships a genuine third answer that passes
   every shared fixture.
2. The **`DIVERGENCE.md` reclassification** forced by invalid UTF-8 (§4.5)
   and the stale `bigint` note (§4.4).
3. **Parsed key order, `str()`, `strinject`, `modlist`** (§4.6-4.7) —
   adjudicated, with fixture rows, probably changing one existing runtime.
4. Generalising **`nonParity`** in `go/spec_registration_test.go` and the
   **`goOnly`** key in `schema/error-codes.json` to N runtimes, and
   decoupling the registry's `version` from the engine version.
5. **A second maintainer**, or an explicit acceptance that a third runtime
   is unmaintained the first week the maintainer is unavailable.

The regex `\s`/`\d`/`\w` split is **not** on that list: it is a lowering
choice the port controls, fixable in ~15 lines with in-tree precedent
(§4.3). It belongs in the porting guide, not in the preconditions.

### 8.3 Reject A

28-33k lines and 7-10 engineer-months; the plugin API is a redesign, not a
port, so every downstream grammar is rewritten by hand; `ci/parity` and
`ci/fuzz` — the tier that caught the real bugs — require porting `json`
and ideally `jsonic` first; and the recurring tax lands on one person who
is already paying 1.76x aggregate and ~4.5 lines of port work per line of
canonical source changed.

### 8.4 What would change this call

- **A named consumer that needs the engine in-process in Rust** — an
  embedded target, a wasm build where a Go runtime is unacceptable, or a
  Rust-native tool that must author grammars rather than consume compiled
  specs. That moves C from "gated" to "scheduled". It does not move A.
- **A second maintainer with Rust ownership.** The tax is survivable at
  two runtimes because one person can port a feature in a sitting; it is
  not survivable at three by the same person. A committed second owner
  changes the maintenance arithmetic more than any technical fact in this
  report.
- **The parity record being repaired first.** If §4.1, §4.4, §4.6 and
  §4.7 are adjudicated and pinned — which is worth doing on its own
  merits, because two contract documents currently misdescribe the Go
  runtime — then a third port stops being a decision-forcing event and
  becomes a typing exercise. That is the single cheapest thing anyone
  could do to make this question easier to answer later.
- **A resumable/incremental parse API landing in TypeScript.** If
  constrained decoding ever becomes a goal, that API is the prerequisite,
  and it is the point at which per-token latency starts to matter enough
  that a native implementation has an argument. Today it does not.

The engine's architecture is genuinely portable — Go proves it, with
13,936 lines, 254 shared fixture rows, and exactly three recorded
divergences, **all** of them lexer/Unicode and **none** in the rule
engine. What is not portable is the callback API, and what is not
affordable is a third column in every parity artefact the project
maintains. The C ABI already exists for exactly this situation. Use it.
