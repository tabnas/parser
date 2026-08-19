# Porting the Tabnas Callback API to Rust: Precedent and Strategy

Companion to [`doc/rust-port-feasibility.md`](rust-port-feasibility.md),
which concluded that the architecture ports and the callback API does
not. This document answers the two questions that report raised and did
not settle: how other projects have moved callback-style code to Rust,
and what strategy follows for tabnas.

Citation convention: a bare `§3.4` refers to a section of the
feasibility report; sections of *this* document are written `§2.3
here`.

## Summary

**Do not port the callback API. Ship the tiers separately, and
adjudicate the closed tier before porting it.** Concretely: settle four
unpinned cross-runtime questions in TypeScript and Go first (§5.1 here, days
of work); ship the Rust FFI crate over `go/clib` — the feasibility
report's option B — as the first artifact; gate a serialized-spec Rust
engine (option C) on a named consumer that `libtabnas` provably cannot
serve; and treat the imperative plugin API as a decision separate
from all of these, and most likely a permanent no.

**The finding from the precedent survey is that this is the normal
answer, not a compromise.** Every toolchain that made this move —
Rome→Biome, Ruff, SWC, oxlint, with esbuild as the non-Rust control —
converged on the same two-tier shape: a closed, in-binary action set
that grows only by upstream contribution, sitting on a substrate where
the callback holds no borrow into anything else it holds; plus an open
tier that was deferred, replaced by a declarative DSL, or pushed across
a process boundary. **None of them ported their callback API.** Biome
shipped its Rust rewrite in 2021 with no plugin API at all, shipped a
GritQL tier with exactly one effect function in June 2025, and still
has not shipped the JS/TS tier that took 80% of its own technology poll.
Ruff shipped 900+ in-tree rules and ~50 reimplemented flake8 plugins,
with the plugin system tracked but unbuilt since 2022. SWC pushed its
open tier to wasm and paid three years of ABI breakage. tabnas's
existing split — 16 `$`-builtins reachable from pure JSON versus the
imperative ref-bag API — **is already the split the field converged
on**, and the JSON tier is the tier that ports.

**The finding from this repository is that the tier everyone recommends
porting first is not, today, semantically closed.** The claim stands.
The evidence it was first written on has since been adjudicated, so it
is restated here on what remains.

*Historical, and now settled.* TypeScript builtins read their
per-alternate config from `alt.k.<name>` — the per-pass scratch
`AltMatch`, reset every pass. Go builtins read it from `r.K`, a rule
field *inherited* by every pushed or replaced child
(`ts/src/rules.ts:662-671`, `:686-695`; `go/rule.go:1224-1236`).
Measured on one pure-JSON grammar with no closures and only `$`-refs,
input `ab` — the divergence #120 closes:

| runtime | output | |
|---|---|---|
| TypeScript | `{"rule":"P","src":"a","kids":[]}` | the bug |
| Go | `{"kids":[{"kids":[],"rule":"P","src":"b"}],"rule":"P","src":"ab"}` | ruled correct |

**#120 rules builtin config RULE-SCOPED: `rule.k` is what a builtin
reads.** TypeScript is the side that moves — eight reads at
`ts/src/builtins.ts:127`, `:138`, `:170`, `:238`, `:249`, `:263`,
`:273`, `:305` — and the Go row above is the answer to copy. The
mechanism is that `rule.k = Object.assign(rule.k, alt.k)`
(`ts/src/rules.ts:605`) and its Go twin (`go/rule.go:1166-1170`) both
run *before* the action, so an alternate's own declared config is
already in `r.k` when its action fires; the propagation contract that
makes it inheritable is written down at `AGENTS.md:196-240` — `n` and
`k` descend to a child on push AND replace, `u` does not.

**The read change alone is not the fix, and shipping it alone creates a
new divergence.** Go's five *value* builders delete their config key
immediately after reading it — `go/builtins.go:248` (`object$`), `:269`
(`array$`, `ListRef` branch only), `:285` (`key$`), `:302` (`setval$`),
`:340` (`value$`) — with the rationale at `:233-238`; the three tree
builders do not. So "rule-scoped" is two regimes: config descends for
`node$`/`capture$`/`fold$` and is consumed once by the other five.
Measured on a two-rule function-free spec (`top` matches `#NR #NR`,
carries `k: { value$: { from: 1 } }`, and pushes `leaf`, which runs
`@value$` bare), input `1 2 3 4` — `3` means the config did not reach
the child, `4` means it did:

| grammar shape | TS today | Go today | TS, naive `r.k` | TS, `r.k` + the five deletes |
|---|---|---|---|---|
| parent sets `k`, **runs** the builtin, pushes | 3 | 3 | **4** | 3 |
| parent sets `k`, does **not** run it, pushes | 3 | 4 | 4 | 4 |

Row 2 is the divergence #120 closes. Row 1 is the one it opens. There
is still no `DIVERGENCE.md` entry, no `go/doc/differences.md` entry and
no shared fixture for either, and none of the eleven `test/spec/*.tsv`
files touches `n`, `u` or `k`, so `ci/parity` cannot see any of it.

*What survives the ruling.* A Rust `enum Act` no longer has a scoping
rule to pick, and — because config is read from `r.k` at invocation
time — no inline struct payload either. What it can still ship is a
*fourth* answer: Go's consume-once delete is a third scoping semantics
that #120 does not name, and an `enum Act` written from post-#120
TypeScript would not have it. Land the run-then-push fixture before
anything is ported. Three further behaviours reachable from a pure
function-free serialized spec also remain unadjudicated: diagnostic
`pos` units (#115 — `schema/diagnostic.schema.json:35` and
`DIVERGENCE.md:66-68` both say Go counts runes, while `go/lexer.go:482`
seeds `Point{Len: len(src), SI: 0}` and `go/parser.go:564` passes
`tkn.SI`, which are bytes), serialized regex terminals (#118 — recorded
only as a declared non-fix at `go/doc/differences.md:441-451`, with no
`DIVERGENCE.md` heading and no fixture), and parsed key order (§4.6,
unpinned). The tier is not closed on one axis; it is not closed on four.

Five further corrections to the shape a port should take, each
compiled or measured:

- **`&mut Ctx` stays.** A shared-receiver action signature is E0596
  against the arena design it would sit on: 11 of the 13 builtin
  actions write the node arena, and `ctx.nodes[nid].src.push(..)` from
  `ctx: &Ctx` does not compile. Deferring the in-action rewind removes
  one reason an action needs `&mut Ctx`; the node arena is the reason.
- **The engine reads back through argument 3 at three fields, and #122
  closes one.** The `:737` re-read of `alt.b` is settled: compute once,
  pre-action, Go's shape. The other two are not, and they are the ones
  that steer the parse. `alt.p` (`ts/src/rules.ts:653`) and `alt.r`
  (`:680`) are read *after* the action at `:646`, off the reusable
  per-Context scratch (`:1226`), and an action that assigns them
  redirects the push or the replace — measured. Go resolves both from
  engine state at `go/rule.go:1203-1210` and its
  `AltAction func(r *Rule, ctx *Context)` cannot reach the alternate, so
  the channel is TypeScript-only and unrecorded. Nothing in-tree or in
  the fleet writes any of the three. Dropping argument 3 closes all
  three structurally — which is the argument for doing it, and which
  makes it a declared narrowing to Go's contract rather than a free
  consequence of #120.
- **Fencing `ctx.rewind()` off a plugin handle is a capability loss,
  not a capability split** — it is documented option surface
  (`ts/doc/options.md:354-358`), budgeted in `AGENTS.md:265-267`, and
  called from eleven in-tree grammar actions. It also does not need
  fencing: a compiled probe runs a restricted `PluginCtx` that *keeps*
  `rewind`, with `Box<dyn Fn>` plugin actions, the pinned post-rewind
  observable, and the guarded upward fold, with no `unsafe` (§2.3 here).
- **Closure identity ports.** `Rc<dyn Fn>` plus `Rc::ptr_eq` reproduces
  `WeakSet<Function>` exactly — verified, no false positives, no false
  negatives. Only `merge.ts`'s `toString()` fallback is unportable.
- **Codegen is not what the fixture measurement shows.** Across the
  three shipped function-free fixtures — 27 rules, 76 alternates —
  `p`/`r`/`b` are 100% static and `h`/`e` are absent. That is static at
  *grammar-load* time, not at rustc time; a serialized spec arrives as
  runtime JSON by design. The salvage is interning rule names to a
  `u32` inside the interpreter, not a second grammar consumer.

---

## 1. The Problem, Restated

The feasibility report states it in §3 and this document does not
repeat it. In one paragraph: an action receives three mutable handles
into one object graph — `rule`, a `Context` whose `.rule` *is* that
rule, and a scratch `AltMatch` the Context owns — and on the OPEN pass
argument 1 and argument 3 are the same object (§3.1). The rule graph is
cyclic with four non-null links per node (§3.2). The value tree is
built by in-place mutation through those handles, including upward
writes into the parent (§3.3). The recommended fix is arenas with `u32`
ids, a `&'g Grammar` parameter, and four things hoisted off the Context
(§3.4, §3.6). Callbacks re-enter the engine: `probeDecide$` calls
`ctx.rewind()` from inside an action (§3.5).

Three things that section does not carry, all of which change what a
port must reproduce.

### 1.1 The alternate is a post-action channel — and #122 closes only part of it

§3.1 cites `ts/src/rules.ts:737` —
`let consumed = rule[is_open ? 'oN' : 'cN'] - (alt.b || 0)` — as the
engine reading back through the handles it passed in. Half of that is
wrong in a way that matters and half is right in a way the report
understates.

`rule.oN` and `rule.cN` are written at exactly two sites,
`ts/src/rules.ts:1468` and `:1474`, both inside `parse_alts`, which
completes before the action runs at `:646`. No builtin, plugin or test
writes them. So the `oN` half is engine state. That still holds.

`alt.b` is not — it is a field of the scratch `AltMatch` handed to the
action as argument 3, and the same expression is evaluated twice, at
`:619` before the action and at `:737` after it, so an action that
writes it desynchronises `ctx.t` and `ctx.v`. **Settled by #122:**
compute once, pre-action, Go's shape (`go/rule.go:1178-1192`). Recorded
honestly, and against this document's earlier emphasis: three separate
probes failed to make a post-action `alt.b` write produce different
output in-tree, so `alt.b` was the weakest example of the family, not
the strongest.

The strongest is `alt.p` and `alt.r`, which this document did not
carry. Both are read *after* the action — `if (alt.p)` at
`ts/src/rules.ts:653`, `else if (alt.r)` at `:680`, against the action
call at `:646` — and both read the same reusable per-Context scratch
(`:1226-1238`) the action holds as argument 3. Measured against
`ts/dist`: an action assigning `alt.p` makes the engine push the name it
wrote instead of the declared one, and a name that does not resolve
kills the parse with `unknown_rule`. Go resolves `pushName`/`replaceName`
at `go/rule.go:1203-1210` from a spec its `AltAction` cannot reach. That
is a fifth unrecorded divergence, it is live routing surface rather than
an unsupported write, and #122 does not touch it. The only thing that
closes it structurally is dropping argument 3 (§5.5 here).

One further property of argument 3 belongs here, because it bounds what
"the action may write it" means. `parse_alts` assigns `out.n/h/a/u/k/g`
straight off the matched `NormAltSpec` by reference
(`ts/src/rules.ts:1568-1573`), so the scratch's `k` and `g` *are* the
grammar's objects: an action doing `alt.k.cfg = 999` and
`alt.g.push('injected')` leaves the grammar permanently altered for
every subsequent parse on that instance — verified against `ts/dist`.
This is the same defect §3.3 files against Go's `AltModifier`
(`go/rule.go:1372`), on the TypeScript action path. It is not entirely
unrecorded: `ts/src/parser.ts:260-263` names the `g` half in a comment
("the alternate's own `g` array is live grammar configuration and must
not be mutable through the event payload") and defends the *subscriber*
with a `.slice()` — which the action path bypasses. Note also which way
#120 moves this: after it, builtins read `r.k`, a per-rule copy made by
the merge at `ts/src/rules.ts:605`, so the ruling *removes* the
builtins' route into the live grammar rather than leaving it.

### 1.2 `ctx.log` is a fifth split, not a fourth

§3.4's table moves four things off the Context. There is a fifth, on
the hot path: `ctx.log` is user-supplied (`ts/src/parser.ts:150`, from
`cfg.debug.get_console` via `ts/src/utility.ts`), stored *on* the
Context, and invoked *with* the Context and the `Lex` at
`ts/src/rules.ts:527`, `:729`, `:1601`, `ts/src/parser.ts:236`, `:272`
and `ts/src/lexer.ts:1800` — once per rule pass. It compiles as a
shared-only callback (`Fn(&Ctx, &Lex)` read from `ctx` and called with
`ctx` is green), so it is a contract narrowing rather than a blocker:
today's `log` receives a Context with an open index signature and could
mutate it.

### 1.3 The closed builtin tier is closed now — except for five deletes

Stated in the summary and measured there. The first consequence this
section used to draw is discharged: the scoping rule *is* adjudicated
(#120, rule-scoped), and the config source is no longer part of any
variant's meaning — it is a lookup in the rule's keep bag at invocation
time, so `enum Act` is designable today and carries no payload.

The second consequence inverts, and is promoted from footnote to the
live blocker. `delete(r.K, "value$")` (`go/builtins.go:340`) is not
evidence that `r.K` persistence was felt and patched locally. Under the
ruling it is the *contract*, and it is one of five —
`go/builtins.go:248` (`object$`), `:269` (`array$`, `ListRef` branch),
`:285` (`key$`), `:302` (`setval$`), `:340` (`value$`), documented at
`:233-238` as running "before the push/replace K-copy, so a config set
on one alt can never leak into a child rule". The three tree builders
do not delete; TypeScript deletes nothing. So one ruling covers two
config lifetimes, and the half TypeScript is missing is the half that
matters for a grammar that both runs a value builtin and pushes (the
measured table in the Summary). It is pinned only by
`go/builtins_test.go:449-465`, which is Go-only, direct-invocation, and
covers two of the five; and `doc/value-builtins.md`'s "Where builtin
config lives" section states the descending lifetime for all eight. A
third runtime reading that section builds five wrong builtins.

Four ordering divergences of the same family, two still latent:

| what | TypeScript | Go | pinned? |
|---|---|---|---|
| function-form `p` / `r` | resolved in `parse_alts`, `ts/src/rules.ts:1577-1589` — **before** the action | resolved at `go/rule.go:1203-1210` — **after** the action | TS side only, `ts/test/cover-engine.test.js:436`, `:455`; no shared fixture |
| `e` relative to `h` | `e` evaluated in `parse_alts` (`ts/src/rules.ts:1575`), i.e. **before** `h` runs at `:569` | `H` at `go/rule.go:1126`, then `E` at `:1137` | no; `h` appears in zero shipped fixtures |
| `alt.b` re-read after the action | yes, `ts/src/rules.ts:737` | no, computed once at `go/rule.go:1178` | **RULED (#122)** |
| `alt.p` / `alt.r` written by the action, then read | yes — `ts/src/rules.ts:653`, `:680`, off the scratch at `:1226`; measured | structurally impossible — `AltAction` cannot reach the alternate | no |
| `RuleDone.alt.p`/`r`/`b` with a function form | the **resolved** value: `_dalt` is the post-`h` scratch (`ts/src/rules.ts:1581` → `:577` → `ts/src/parser.ts:264`) | the **static** grammar field: `ctx.dalt` is a `*AltSpec` (`go/parser.go:55`, `go/rule.go:1105`) read at `go/parser.go:827-830`, while `PF` resolves into a local at `go/rule.go:1203-1210` and is never written back | no — Go's only `PF`/`RF`/`BF` tests are loader assertions (`go/grammarspec_cov_test.go:46`), and `ts/test/ruledone.test.js:90` asserts only that *some* event has a non-empty `p` |

Function-form `b` matches (both pre-action). None of these is a Rust
question. Two are now decided; the other three are decided,
permanently, by whoever writes the Rust engine first — except the
fourth, which is decided instead by whether `AltAction` keeps argument
3.

---

## 2. What Other Projects Did

Organised by technique. The projects are evidence; the techniques are
the menu. Every claim below is from a primary source — a repository
file, an ADR, a changelog, or an issue thread.

### 2.1 The technique menu

| Technique | Who ships it | What it costs |
|---|---|---|
| **Handle, not borrow.** The callback gets `Copy` ids plus one engine reference; nothing it holds is a borrow into anything else it holds. | wasmtime `Func { store: StoreId, … }` with `StoreOpaque: Index<I>`; mlua `ValueRef { lua: WeakLua, index, count }`; la-arena `Idx<T>` (`Copy`, `Eq`/`Hash` on the raw `u32`); starlark-rust `fn invoke(&self, me: Value<'v>, args, eval: &mut Evaluator)` — the receiver arrives twice, on purpose, because neither copy is a borrow into the other | Every access is an index plus a bounds check. Two-object operations need a disjoint-access API. Arena lifetime becomes an explicit decision with a number: wasmtime documents that store objects "will not be deallocated until the `Store` itself is dropped" |
| **Effects as returned data.** The callback returns a value describing what it wants; the engine applies it. | Biome `fn run(ctx: &RuleContext<Self>) -> Self::Signals`, then `fn action(…) -> Option<RuleAction<L>>` carrying a `BatchMutation`; Biome plugins return `PluginEvalResult`; Ruff `Edit { range: TextRange, content: Option<Box<str>> }` inside `Fix`; logos `Skip` / `Filter<T>` / `FilterResult<T,E>` | The effect set must be enumerable. Anything outside it is inexpressible and widening it is a breaking change. One small allocation per emitting call |
| **Parent as a query; analysis in side tables.** The back-pointer is not on the node. | oxc `AstNodes { parent_ids: IndexVec<NodeId, NodeId> }` with `parent_id(nid)` (`crates/oxc_semantic/src/node/nodes.rs:26`); rustc `TypeckResults` as ~20 `ItemLocalMap`s; rust-analyzer `ArenaMap<ExprId, …>` beside `Arena<Expr>` | Two structures to keep in step; desynchronisation is a silent mis-parenting, not a type error. rustc guards it with a wrapper that checks `hir_owner` |
| **Disjoint indexed access with an identity guard.** | `<[T]>::get_disjoint_mut` (stable 1.86), returning `Err(GetDisjointMutError)` on overlap. The Nomicon states the limit: borrowck cannot understand disjointness in tree-shaped containers "especially if distinct keys actually *do* map to the same value" | MSRV floor 1.86; the `HashMap` equivalent is still unstable. The guard is load-bearing, not decoration. duckdb-rs is the counterexample — `&self` column accessors that safe code can call twice, documented as UB and left open |
| **Declared interest, dispatch pushed into the engine.** | esbuild `OnResolveOptions { Filter string; Namespace string }`, evaluated on the Go side; Biome `AnalyzerPlugin::query() -> Vec<RawSyntaxKind>` checked in `PluginVisitor::visit` before `evaluate`; Biome's compile-time form `type Query = Ast<T>`; oxlint `RuleRunner::NODE_TYPES: Option<&AstTypesBitset>` plus `RUN_FUNCTIONS`, derived by codegen | The declaration drifts from the body. oxlint pays with a bidirectional conformance test that fails both for a missing type and for a spare one. Hand-declared variants push a correctness obligation onto plugin authors |
| **Capability-restricted handle.** Hand the callback a type that structurally lacks the methods that would break the pass. | Bevy `DeferredWorld` — "A `World` reference that disallows structural ECS changes"; rusqlite `Context<'a> { ctx, args }` with no `&Connection`; tree-sitter's `TSLexer` vtable with no route to the parser | Two parallel context types to maintain; a taxonomy decision per new operation; users hit "why can't I call this here" and must learn the phase model |
| **Shared receiver, interior mutability, uniqueness checked at run time.** | Ruff `Checker` with `RefCell` fields and `fn report_diagnostic(&self, …) -> DiagnosticGuard` (RAII, lands on `Drop`); PyO3 splitting the thread axis (static `Python<'py>`) from the uniqueness axis (`PyCell`); mlua `Lua { raw: XRc<ReentrantMutex<RawLua>> }` | Aliasing errors move from compile time to run time. Safe only on a sink nothing re-enters — never on the working object graph |
| **Fallible borrow, never a panicking one.** | mlua `Error::UserDataBorrowMutError`, whose doc comment names the cause: "This error can occur when a method on a `UserData` type calls back into Lua, which then tries to call a method on the same `UserData` type" | A runtime failure the plugin author must debug. Needs a reserved error code and a message that explains the cause, not just the symptom |
| **Metadata instead of the aliased peer.** | SWC ADR 00004 (2022-06-30) hit the identical wall — "As we mutabily borrowed something from `CallExpr`, we cannot pass `&CallExpr` to `visit_mut_callee`" — and chose to "expose the spans and kinds of the parent ast nodes", rejecting both the dummy-swap and `unsafe` | Named in the ADR itself: "It's too hard to **port** plugins, because a plugin author has to recreate logic instead of porting, if the original babel plugin uses parent node information" |
| **Externalize the callback's state.** | Boa `from_copy_closure_with_captures(closure, captures: T)`; duckdb-rs `type State: Send + Sync + 'static`; rusqlite `get_aux`/`set_aux`; tree-sitter `serialize`/`deserialize` into a 1024-byte engine-owned buffer, so the *engine* owns checkpoint and restore | Plugin authors lose ordinary closure captures. Serializable state is a hard bound and forces awkward encodings |
| **The engine drives backtracking, not the callback.** | tree-sitter: the docs say flatly "you cannot backtrack" inside `scan`; the engine rewinds and restores the scanner through `deserialize` | Some logic cannot be expressed as a return value and must be refactored. tree-sitter pays with the serialize ABI and the 1024-byte cap |
| **Closed builtin set plus a one-effect declarative tier.** | Biome: hundreds of Rust rules behind `declare_lint_rule!`, plus GritQL plugins whose entire API is one function — "Biome currently supports one extra function: `register_diagnostic()`". Ruff is the degenerate case: 900+ in-tree rules, plugin system tracked but unbuilt | The DSL is strictly less powerful and everyone knows it — Biome's own RFC poll ran TypeScript 80%, DSL 2%. The builtin set becomes a merge queue |
| **Cross-boundary protocol with a registration manifest.** | esbuild sends Go a manifest — `BuildPlugin { name, onStart, onEnd, onResolve: [{id, filter, namespace}], onLoad: […] }` — over "a very simple binary protocol … basically JSON"; SWC's `#[plugin_transform]` marshals a whole `Program` in and out per file | Coarse granularity is mandatory: a per-node round trip is unaffordable. Ownership problems become versioning problems |
| **Feature-gated `Send + Sync` behind a trait alias.** | RustPython `PyThreadingConstraint` — `Send + Sync` under the `threading` feature, empty otherwise, with `PyNativeFn` and `PyPayload` bounded on the alias rather than on `Send + Sync` directly (`crates/vm/src/object/payload.rs:10-19`) | Two build configurations to test. Downstream crates wanting the threaded profile must satisfy it, which rejects `Rc<RefCell<_>>` captures |

### 2.2 The four that matter most here

**Handle, not borrow** is unanimous and is already the feasibility
report's §3.6 recommendation. The survey adds one refinement worth
taking: wasmtime stamps an owner identity into the handle (`StoreId`, a
process-wide monotonic `NonZeroU64`, with `assert_belongs_to`) so a
handle used against the wrong engine panics deterministically instead
of reading a live slot from another parse. That is one word of storage
and one comparison, and it is cheaper than per-slot generational
indices if slots are only reused across parses.

**Parent as a query** is the direct answer to §3.3's upward write. oxc
carries no parent field on the node at all; `parent_id` is a lookup in
a parallel `IndexVec`. Applied here, `Node` carries no parent, `Ctx`
carries `node_parent: Vec<NodeId>`, and `fold$` becomes a `node_parent`
read followed by the guarded two-node accessor. This also removes Go's
`push$` write-back — `r.Parent.Node = r.Node` (`go/builtins.go:329`),
which exists only because Go slices are value types.

**Metadata instead of the aliased peer** is the closest external
statement of tabnas's §3.1 problem. SWC's ADR 00004 is the same wall
with `CallExpr`/`callee` in place of `rule`/`next`, decided by
maintainers who had to ship, and its answer — hand the callback the
peer's identity and metadata, not a second handle — is what
`Fn(&mut Ctx, RuleId, &mut AltMatch)` already is. It is the citation
that makes dropping argument 3 a precedent rather than an amputation.
Note that SWC also names the price, and it is exactly tabnas's price:
plugins get rewritten, not ported.

**Capability-restricted handle** is the one to adopt for the plugin
tier, and to adopt *without* removing `rewind` (§2.3 here). Bevy's
`DeferredWorld` is the model: it disallows *structural* changes — the
ones that would invalidate the iteration the engine is inside — and
permits everything else. The tabnas analogue is a handle with no
rule-stack push or pop, no grammar access, and no lexer cursor, which
costs nothing in-tree because nothing in-tree uses those from a
callback.

### 2.3 What a fenced handle actually looks like

Compiled and run, rustc 1.94.1, no `unsafe`, no `Rc`, no `RefCell`.
This is the shape a plugin tier should have; note that `rewind` is
present, that the handle takes one parameter (#120 leaves no builtin
reading the alternate), and that `consumed` is fixed *before* the
callback loop, per #122:

```rust
/// A newtype over &mut Ctx exposing a chosen method set.
/// No push_rule, no pop, no grammar, no lexer cursor — but rewind is here.
pub struct PluginCtx<'a> { c: &'a mut Ctx, rid: RuleId }

impl<'a> PluginCtx<'a> {
    pub fn mark(&self) -> u32 { self.c.v_abs }
    pub fn rewind(&mut self, mark: u32) { /* v -> pending, v_abs -= k */ }
    pub fn v_len(&self) -> usize { self.c.v.len() }
    pub fn node_src_push(&mut self, s: &str) { /* via rules[rid].node */ }

    /// The only two-node spelling; pid == nid is a no-op, not a panic.
    pub fn fold(&mut self) {
        let r   = &self.c.rules[self.rid.0 as usize];
        let nid = r.node;
        let pid = self.c.node_parent[nid.0 as usize];
        if pid == nid { return }
        let [p, own] = self.c.nodes
            .get_disjoint_mut([pid.0 as usize, nid.0 as usize]).unwrap();
        p.src.push_str(&own.src);
        p.kids.append(&mut own.kids);
    }
}

type PluginAct = Box<dyn Fn(&mut PluginCtx) -> Result<(), Fault>>;

fn run_actions(ctx: &mut Ctx, rid: RuleId,
               acts: &[PluginAct]) -> Result<(), Fault> {
    // #122: `consumed` is engine state, fixed before any action runs.
    let consumed = ctx.rules[rid.0 as usize].on.saturating_sub(ctx.palt.b);
    for a in acts {
        let mut pc = PluginCtx { c: ctx, rid };   // fresh reborrow per call
        a(&mut pc)?;
    }
    let _ = consumed;
    Ok(())
}
```

A user action that calls `pc.rewind(0)` and *then* reads `pc.v_len()`
prints `after-rewind-v-len:0` — the observable pinned in both runtimes
at `ts/test/rewind.test.js:104` and `go/rewind_test.go:78`. The E0499
in §3.1 is an artifact of handing out `&mut Rule` and `&mut Ctx`
simultaneously; the arena dissolves it before any tiering question
arises. Re-entrancy is not the hard problem.

What does *not* compile, and matters:

```rust
// A shared-receiver action against the arena design. E0596.
fn node_act(ctx: &Ctx, rid: RuleId, _alt: &mut AltMatch) -> Result<Effect, Fault> {
    ctx.nodes[nid.0 as usize].src.push('x');
    // error[E0596]: cannot borrow `ctx.nodes` as mutable, as it is
    //               behind a `&` reference
}

// An action list owned by the scratch AltMatch. E0502.
for a in &alt.a { run(a, ctx, alt) }
// error[E0502]: cannot borrow `*alt` as mutable because it is also
//               borrowed as immutable
```

The first still holds unconditionally: the node arena is why an action
needs `&mut Ctx`, and no ruling touches that.

The second was a design constraint only while the callback took the
alternate. Recompiled with the two-argument action —
`for a in &alt.a { a(ctx, rid)?; }` — it builds and runs (rustc 1.94.1),
because the loop's shared borrow of `alt` no longer collides with a
`&mut` handed to the callback. Keeping the action list in the grammar
(`&'g [Act]`) is still the right shape and still what TypeScript does —
`composedAction` closes over its function list at grammar-build time
(`ts/src/rules.ts:1845-1864`) and the per-pass `out.a` merely points at
it — but it is now a preference, not something the borrow checker
enforces. Say which, because the two are not the same kind of claim.

### 2.4 Antipatterns — tried, shipped, and abandoned

**Transliterating a JS object graph as `Rc<RefCell<T>>`.** The
feasibility report's probe f3 aborts on every OPEN pass carrying a
before-action. That is not a tabnas peculiarity and it is not a
transitional state. Boa shipped the equivalent — a panicking
`GcRefCell::borrow_mut` — and has filed the same class of borrow panic
continuously from issue #663 in 2020 ("calling a function that mutates
itself causes borrow panic") to #5265 and #5337 in 2026. RustPython
started at `Rc<RefCell<PyObject>>`, measured "70 Byte up to 84 Byte for
each integer … three(!) pointer indirections", and moved to
`#[repr(transparent)] PyObjectRef { ptr: NonNull<PyObject> }`.
generational-arena's README states the reason in one sentence: "The
cycles rule out reference counted types, and the required shared
mutability rules out borrows." There is also an API cost the report
does not name: because a `RefCell` guard's lifetime is the guard's,
"Learning Rust With Entirely Too Many Linked Lists" shows `peek` cannot
return `&T` and must return `Ref<T>`. The tabnas version of that is an
`AltAction` signature that leaks `RefMut<Rule>` to every plugin author
— a worse break than moving to ids, which are inert `Copy` values.

**Branding every handle with a generative lifetime.** This is the
strongest evidence against a `Caller`-style token design, and it is
directly on point because rlua's problem was tabnas's problem —
callbacks re-entering the host. rlua 0.16 shipped the fix as a "HUGE
API incompatible change: move most of the `Lua` API into `Context` and
require context callbacks to generate a branding lifetime". Then it was
undone: mlua removed `Lua::context()` entirely and its 0.10 changelog
records "Dropped `'lua` lifetime (subtypes now store a weak reference
to Lua)", and rlua 0.20 became a wrapper around mlua. rquickjs still
brands, so it is a live trade rather than a settled one — but the
closest analogue in the ecosystem adopted it, maintained it for years,
and reversed. Do not adopt it here without saying so.

**A scope-lifetime arena for handles.** PyO3's GIL Refs (`&'py PyAny`
over a thread-local "reference pool" that could not be freed until the
GIL scope ended) is the memory model §3.6 contemplates when it says an
arena that never frees turns O(stack depth) into O(input size). PyO3
measured "as much as 30% overhead" from it and paid a multi-release
migration to `Bound<'py, T>` to undo it. Wasmtime keeps the never-free
arena but confines it by documenting `Store` as short-lived and
"unsuitable for creating an unbounded number of instances". Neither is
free; pick one deliberately and write the caveat down.

**Reaching for `unsafe` to hand a callback the aliased peer.** SWC ADR
00004 considered exactly this and rejected it: "Using `unsafe` in
public API requires more discussion. We don't have good debugging api
for plugins at the moment." A tabnas port is in a worse position, since
its callbacks are user-supplied and can re-enter the engine.

**Applying the dummy-swap universally.** Also ADR 00004: doing the
take-dummy-restore dance "by hand is error-prone and doing this
automatically by codegen is costly. This is explicit `memmove`, and
`memmove` is quite costly. SWC moved from `Fold` to `VisitMut` because
of `memmove`." Use `Take`/`map_with_mut` surgically on a small scratch
value — which is what `AltMatch` is — not on the node tree.

**Take-and-return as the default visitor style.** `Fold` is the most
Babel-like option and swc_visit's own module docs deprecate it:
"WARNING: `Fold` is slow, and it's recommended to use VisitMut if you
are experienced". Relevant because `AltModifier` is exactly a `Fold` —
it returns a replacement `AltMatch` — and "just make everything return
a new value" is the redesign SWC migrated *away* from under
measurement.

**Shipping a serialized ABI without versioning or an unknown-variant
escape.** SWC's wasm plugins bound each plugin binary to an exact
`swc_core` version; the docs said "Currently, the Wasm plugins are not
backwards compatible." Three years of breakage followed — a plugin
author unable to satisfy both Next.js 13.4.3-13.4.7 and newer
(swc#8315), `RuntimeError: out of bounds memory access` on mismatch, a
dedicated Rspack error page, and a compatibility *registry* built as a
workaround — fixed only in `@swc/core` v1.15.0 (November 2025) with a
stable ABI and an `Unknown` variant added to every AST enum. If tabnas
ever serializes its `Rule`/`Node`/`AltMatch` shapes across a boundary,
version them and add unknown-variant tolerance in the first commit.

**`FnMut` callbacks in a re-entrant engine.** Wasmtime states it
flatly: "host functions always are `Fn` as opposed to `FnMut` or
`FnOnce`." mlua offers `create_function_mut` only by wrapping in a
`RefCell` and turning re-entry into `Err(Error::RecursiveMutCallback)`.
The gtk-rs book derives it from first principles: signal handlers "can
be called from inside themselves. This would lead to multiple mutable
references which the borrow checker doesn't appreciate at all. This
leaves `Fn`."

**Take-out/put-back with a runtime panic.** Zed's GPUI does exactly
what a tabnas port is tempted to do — `EntityMap::lease` removes the
entity from the arena, hands the callback `&mut T` and `&mut Context<T>`
disjointly, and `end_lease` puts it back — and it is worth recording
honestly rather than as a recommendation: `lease` calls
`double_lease_panic` on re-entry, and `Lease`'s `Drop` panics with
"Leases must be ended with EntityMap::end_lease". That is the same
failure class as the `RefCell` transliteration this section rejects. It
ships, and people live with it; it is not better than ids.

**Deferring the plugin question and assuming it stays cheap.** Rome
announced the Rust rewrite in September 2021, shipped the *formatter*
first (February 2022), reached a stable Rust release in November 2022
with no plugin API, opened the plugin RFC in 2024, shipped GritQL
plugins with one effect function in June 2025, and has not shipped the
JS/TS tier. Four-plus years, with a full-time team, and the company
that started it folded mid-migration — the project survived only
because it was forked. The feasibility report puts the tabnas port at
7-10 engineer-months for one engineer; the plugin tier is a separate
and much longer clock.

**Assuming a JSON boundary is cheap.** oxc names it as the default
mistake: serialize the AST, ship the string, `JSON.parse` it back — "But
this is extremely slow. Often the cost of these conversions is so high
that they massively outweigh the performance gain of using native code
in the first place." Even esbuild, whose protocol *is* effectively
JSON, survives only because it crosses twice per module rather than per
node.

**Letting the callback own traversal.** In every project surveyed the
engine owns the walk and the extension is a leaf. tabnas's
`probeDecide$` calling `ctx.rewind()` mid-action is the one thing in
the survey with no external precedent among *plugin* APIs — tree-sitter,
the only other backtracking parser here, forbids it outright. That is
an argument for keeping the capability in-engine and giving it a
defined drain point, not an argument that it is impossible: §2.3 here shows
it compiles once handles are ids.

---

## 3. The Callback Inventory

`ts/src/types.ts` declares 17 callback types. That is the number that
matters for a port, because each is a distinct signature. It is not the
number of *slots*: `LexCheck` alone occupies 16 declared option
positions, and at least twelve user-supplied callback slots have no
named type at all — `budget.onCheck` (`:255`), `config.modify[name]`
(`:282`), `options.parser.start` (`:283-292`), `value.defre[].val`
(`:469`), comment `suffixFn` (`:491`, a `LexMatcher` in an unnamed
slot), `comment.*.suffix` (`:129`), `map.merge` (`:164`, `:499`),
`debug.get_console` (`:518`), `debug.print.src` (`:522`),
`errmsg.suffix` (`:537`), `ListMods.custom` (`:606`), and the
`TokenValFunc` that `Token.resolveVal` dispatches to
(`ts/src/lexer.ts:143-146`; Go `token.go:97`). Treat the slot count as
a floor above 30, not as a number. `NextToken` (`:321`) is dead — its
declaration is its only occurrence — and is an exported type, so
deleting it is a trivial public-API break rather than a pure cleanup.

### 3.1 The table

Buckets: **A** = closed builtin, dispatched inside the engine; **B** =
pure / inspection, a shared reference suffices; **C** = mutating,
non-re-entrant; **D** = re-entrant; **E** = lexer tier.

| Callback | Signature (TS / Go) | Invoked at | Bucket | Serialized? |
|---|---|---|---|---|
| `AltAction` `a` | `(rule, ctx, alt) => any` / `func(r *Rule, ctx *Context)` — after #120 **argument 3 has no readers** | `ts/src/rules.ts:646`; `go/rule.go:1197` | **D** (one member); C otherwise | yes — `a`, scalar or array |
| `AltCond` `c` | `(rule, ctx, alt) => bool` \| `Record` / `AltCond` + `CD` | `ts/src/rules.ts:1481`; `go/rule.go:1544` | B (by contract, not by construction) | yes — funcRef **or** declarative object |
| `AltModifier` `h` | `(rule, ctx, alt, next) => AltMatch` / `func(alt *AltSpec, r, ctx) *AltSpec` — the two runtimes hand it **different objects**: TS the per-pass scratch `ctx._palt`, Go a grammar `*AltSpec` | `ts/src/rules.ts:569`; `go/rule.go:1126` | C — and in Go it is handed the **live grammar** | yes |
| `AltNext` `p` / `r` | `(rule, ctx, alt) => string` / `PF`, `RF` | `ts/src/rules.ts:1577-1589` (pre-action); `go/rule.go:1203-1210` (post-action) | B | yes |
| `AltBack` `b` | `(rule, ctx, alt) => number` / `BF` | `ts/src/rules.ts:1591`; `go/rule.go:1182` — both pre-action | B | yes |
| `AltError` `e` | `(rule, ctx, alt) => Token?` / `func(r, ctx) *Token` | `ts/src/rules.ts:1575` (pre-`h`); `go/rule.go:1137` (post-`H`) | C | yes |
| `StateAction` `bo`/`ao`/`bc`/`ac` | `(rule, ctx, next, out?) => Token \| void` / `func(r, ctx)` | `ts/src/rules.ts:556`, `:720` | C — **the canonical E0499 site** | not directly; reachable via reserved `@<rule>-bo\|ao\|bc\|ac` refs in a load-time ref bag (`ts/src/rules.ts:290`) |
| `LexMatcher` | `(lex, rule, tI?) => Token?` / `func(lex, rule)` — Go lacks `tI` | `ts/src/lexer.ts:1659`, `:1770` | E | no |
| `LexCheck` | `(lex) => void \| {done, token}` / `*LexCheckResult` | `ts/src/lexer.ts:207`, `:911` | E | no |
| `ValModifier` | `(val, lex, cfg, opts) => string` / `func(val any) any` | `ts/src/lexer.ts:990` | E | no |
| `MakeLexMatcher` | `(cfg, opts) => LexMatcher?` | configure(), per `make()` | E (setup) | no |
| `RuleDefiner` | `(rs, p) => void \| RuleSpec` | grammar build | setup | no |
| `Plugin` | `(tabnas, opts) => void \| Tabnas` | `use()` / make | setup | no |
| `ParsePrepare` | `(tabnas, ctx, meta)` / `func(ctx *Context)` | `ts/src/parser.ts:153-155`; `go/parser.go:431` | C | no |
| `LexSub` | `(tkn, rule, ctx)` | `ts/src/lexer.ts:1818-1819` — inside `lex.next()`, inside `parse_alts`, inside `process()` | C | no |
| `RuleSub` | `(rule, ctx)` | `ts/src/parser.ts:238-239` | C | no |
| `RuleDoneSub` | `(rule, ctx, done)` | `ts/src/parser.ts:269`, `:366` | C | no |
| `TokenValFunc` (untyped in TS) | `(rule, ctx) => any` / `func(r, ctx)` | `ts/src/lexer.ts:143-146`, reached from `@value$` at `ts/src/builtins.ts:307` | C — a user callback invoked from inside another user callback, with the same handles | no |

The 16 `$`-builtins are bucket **A**: 13 `AltAction` (`@node$`
`@capture$` `@bubble$` `@fold$` `@probeInit$` `@probeDecide$`
`@object$` `@array$` `@reset$` `@key$` `@setval$` `@push$` `@value$`)
and 3 `AltCond` (`@probePhase0$/1$/2$`), frozen at
`ts/src/builtins.ts:326-345`, `BUILTIN_SCHEMA_VERSION = 3`. They cover
**two of the seventeen declared types**. There is no builtin for
`AltNext`, `AltBack`, `AltError`, `AltModifier`, `StateAction`, or
anything in the lexer tier — `schema/grammar.schema.json:10` says
outright that lexer matchers "are not grammar".

Two facts about that bucket changed with #120, and both simplify the
port. **The action half loses its payloads.** Config is read from `r.k`
at invocation time, so no variant carries a literal: `#[repr(u8)] enum
Act` is fieldless, one byte, `Copy + Eq + Hash`, its own dedupe key, no
`Box` and no `dyn`. The lookup must stay dynamic, though — the
propagation contract (`AGENTS.md:196-240`) means the key may have been
set by an ancestor rule, so the variant resolves it from the rule bag
at run time and never inlines it. **The condition half is already a
data language.** The declarative form ships as a path DSL over 22 rule
roots — including `n`, `u`, `k`, `parent`, `child`, `prev`
(`ts/src/rules.ts:1900-1907`) — with seven operators (`:1884-1892`), and
every generated leaf is `function ruleCond(r, _c, _a)` ignoring both
context and alternate (`:2017-2052`). `COND_PATH_ROOTS` has no `alt`
root, so before the ruling the phase guards could not be expressed in
it; with `pd_phase` on `r.k` they can. A Rust engine therefore
implements one payload-free action enum plus one path evaluator it
needs anyway, not two dispatch mechanisms.

On argument 3 specifically, the load-bearing statement is a negative
one and it is what the §5.5 signature rests on: **no callback in either
runtime, or in any of the 34 plugin repositories under
`/workspace/tabnas`, reads the third argument of an action or a
condition.** The only three-argument bodies are pass-through composers —
`composedAction` (`ts/src/rules.ts:1856-1859`), `conjunctCond`
(`:1799`), and `@tabnas/bnf`'s `composeActions`/`seqActions`
(`bnf/ts/src/spec.ts:259-271`) — and the only genuine consumers are five
`AltModifier` implementations (`ts/test/cover-engine.test.js:381`,
`:427`; `jsonic/ts/test/custom.test.js:215`;
`go/grammarspec_cov_test.go:21`, `:182`), every one of which returns its
input unchanged. Zero shipped grammar sources in the fleet declare `h`
at all. One caveat worth stating rather than asserting away: `@tabnas/bnf`
*publishes* the three-argument shape as public API — `type ActionFn =
(r, ctx, alt) => any` at `bnf/ts/src/spec.ts:252`, re-exported as
`ActionsMap` from `bnf/ts/src/bnf.ts:66` and consumed by
`attachActions` (`spec.ts:333`). Zero readers, one shipped public
declaration.

### 3.2 The size of the re-entrant bucket

**One declared callback type, and one builtin.** `ctx.rewind()` is the
only engine-mutating API reachable from a callback
(`ts/src/context.ts:168-215` rewrites `ctx.t`, `ctx.v`, `ctx.vAbs`,
`lex.pnt.token`, `lex.pnt.end`). Every call site reachable from user
code is an `AltAction`. `ctx.mark()` is a pure read, so `@probeInit$`
is bucket C. `ctx.inst()` would permit a nested parse; it has zero
occurrences repo-wide.

That number is the one that decides the strategy, and it decides it in
a direction the survey does not obviously predict, so state the full
census:

| | count | where |
|---|---|---|
| Declared callback types that can re-enter, **exercised** | **1** | `AltAction` |
| Builtins that re-enter | **1** | `@probeDecide$` (`ts/src/builtins.ts:208`; Go twin `go/builtins.go:210`) |
| In-tree **user grammar actions** that call `rewind` | **11** | `ts/test/rewind.test.js:102, 145, 185, 222, 260, 332, 362`; `go/rewind_test.go:74, 104, 130, 175` |
| Other in-tree call sites | **1** | `ts/test/cover-engine.test.js:256`, a direct call on a hand-built mock |
| Declared types that *could* re-enter (receive a `Context`) | **14** | every alt/lifecycle/subscriber type, plus `budget.onCheck`, `TokenValFunc`, `map.merge`, `ParsePrepare`; `LexMatcher`/`LexCheck` transitively via `lex.ctx` |

So: one exercised type against fourteen reachable is a strong argument
for *not* designing a general re-entrant tier — Deno's op tiering
(`&OpState` / `&mut OpState` / `Rc<RefCell<OpState>>`) is the precedent
for sizing the strong form to its actual callers. But of the twelve
call sites outside the engine's own two builtins, eleven are in *user
grammar actions* — precisely the tier a plugin fence would restrict. `rewind` is
documented option surface (`ts/doc/options.md:354-358`) and a budgeted
resource (`AGENTS.md:265-267`). Removing it from the plugin handle is a
capability loss with eleven in-tree casualties, and §2.3 here shows it does
not buy anything: a fenced handle that keeps `rewind` compiles.

The fleet denominator, now that all the repositories are cloned,
strengthens the first half and weakens the second. Across the 34 plugin
repositories there are exactly **two** shipped `ctx.rewind` /
`ctx.Rewind` call sites — `bnf/ts/src/compiler.ts:1801` and
`bnf/go/emit_support.go:574` — both hand-written twins of
`@probeDecide$` on the closure path, and both already reading
rule-scoped `r.k.pd_mark` in both runtimes (which is #120's ruling,
confirmed at source in shipped downstream code). `ctx.inst()` still has
zero real call sites fleet-wide. So the shipped usage is one builtin
plus one duplicate: "do not design a general re-entrant tier" gets
stronger, and "eleven in-tree casualties" gets weaker as an argument
against fencing. The argument that survives is §2.3 here's — a fence
that keeps `rewind` compiles anyway, so nothing has to be given up.

**Fence the things nothing exercises instead** — rule-stack push and
pop, grammar access, the lexer cursor. That is a real `DeferredWorld`
restriction at zero in-tree cost.

### 3.3 Three inventory findings worth pinning as tests

- `map.merge` is declared in both runtimes and invoked by neither
  (`ts/src/types.ts:164`, `:499`; `go/options.go:242`, wired at
  `:1019`). It is a slot for downstream grammar plugins. It is also
  divergent three ways: TS's `OPTIONS` shape is `(prev, curr) => any`,
  TS's resolved `Config` shape is `(prev, curr, rule, ctx) => any`, and
  Go's `MapMergeFunc` is `func(prev, val any, r *Rule, ctx *Context)
  any`. Nothing can catch this, because nothing calls it.
- Three further callbacks have divergent arity: `LexMatcher` (TS
  `(lex, rule, tI?)` vs Go `(lex, rule)`), `ValModifier` (TS
  `(val, lex, cfg, opts)` vs Go `(val any) any`), and `ParsePrepare`
  (TS `(tabnas, ctx, meta)` vs Go `(ctx)`). A Rust port written from
  Go's shapes silently narrows the plugin contract.
- Go's `AltModifier` receives the **live** `*AltSpec` and `ParseAlts`
  returns one of its inputs (`go/rule.go:1126`, `:1372`), so a mutating `H`
  corrupts the grammar for every subsequent parse on that instance.
  That is a bug worth filing independently of the Rust question;
  `&'g Grammar` makes it statically impossible.

---

## 4. Strategy Options, Costed

The options below are not a flat menu, and ranking them as peers would
be a category error. S0 is a prerequisite. S2 is the mandatory
substrate for anything running in-process. S3, S6 and S7 are *scope*
decisions — what tier ships. S4 and S5 are refinements of the
substrate. S1 is listed in order to be rejected.

| Option | What it is | Precedent | Cost | Verdict |
|---|---|---|---|---|
| **S0 — Adjudicate first** | Settle `alt.k` vs `r.K`, the `p`/`r` and `e` orderings, the `alt.b` re-read and the `alt.p`/`alt.r` channel, in TS and Go, with fixtures | The feasibility report's own §4.1 argument about `pos` | Days. **One TypeScript change** — 8 builtin reads move `alt.k.<name>` → `r.k.<name>`, plus 5 guarded delete-after-read mirroring `go/builtins.go:248,269,285,302,340` and *not* for `node$`/`capture$`/`fold$`; Go unchanged; 4 direct-invocation tests in `ts/test/builtins.test.js` rewritten; a TS mirror of `go/builtins_test.go:449-465` covering all five; one shared run-then-push fixture, one propagation fixture, one each for the `p`/`r` and `e` orderings and the `alt.p`/`alt.r` channel; one `DIVERGENCE.md` row per deliberate difference | **Prerequisite.** Cheap now, unfixable three-way argument later |
| **S1 — Transliterate the callback API** | `Rc<RefCell<Rule>>`, weak-capture macros, keep the signatures | gtk-rs `glib::clone!` — the largest callback-based Rust GUI codebase, whose most-used ergonomic tool exists to make capture-and-upgrade bearable, and whose users largely migrated to Relm4 to stop writing it | Every action becomes a `'static` closure over `Rc<RefCell<Ctx>>`, every body opens with upgrade-or-bail, and the runtime abort is replaced by a silent no-op. `clone!` manages capture *lifetime*, not *aliasing*, so arg1==arg3 is untouched | **Reject.** Boa #663→#5337 is six years of the same panic |
| **S2 — Arena + `RuleId`/`NodeId`** | The §3.6 design: `Copy` ids, `&'g Grammar`, `node_parent` side table, one checked two-node accessor | Unanimous: rust-analyzer `Idx<T>`, oxc `parent_ids`, wasmtime `StoreId`, mlua `ValueRef`, starlark `Value<'v>` | Solves the aliasing, the cycles, and the two-node writes. Does **not** solve memory retention (~0.7 rule passes per source byte) or the API break. Every downstream plugin is rewritten by hand | **Mandatory** for any in-process engine |
| **S3 — Closed `enum Act` for the 16 builtins** | Bucket A as `#[repr(u8)] enum Act`, **no payload** — config resolved from `r.k` at invocation; the three `AltCond` builtins fold into the declarative path language that already ships | Biome `declare_lint_rule!` + GritQL; Ruff's in-tree rule set; chalk's closed `RustIrDatabase` | Zero `dyn` and zero allocation for specs loaded from JSON, zero aliasing on that path, and the dedupe key is a `u8` discriminant (`Rc::ptr_eq` needed only for the open tier). Closes the world: a third party needs a new variant or a `Custom` arm that reinstates the problem. **No longer blocked on the scoping question** — blocked only on removing Go's five consume-once deletes, or adopting them in TS | **Take now** |
| **S4 — Effects as a returned value** | Widen the action return to `Result<Effect, Fault>` with `Effect ∈ {None, Bad(Token), Backtrack(u32), Rewind(Mark)}` | Biome `RuleAction`, Ruff `Edit`, logos `FilterResult`, tree-sitter's engine-driven rewind | A strict superset of today's channel, which is used for exactly one thing (`ts/src/rules.ts:646-650`). Does **not** buy a shared receiver — E0596. Full deferral of value-tree edits is unnecessary once nodes are id-keyed | **Take the error/backtrack channel; skip full deferral** |
| **S5 — Capability-restricted plugin handle** | `PluginCtx<'_>` newtype: no rule-stack mutation, no grammar, no lexer cursor — but keep `rewind` | Bevy `DeferredWorld`, rusqlite `Context<'a>`, tree-sitter `TSLexer` | Two context types to maintain. Makes §3.4's rule a type fact rather than a convention. Compiles (§2.3 here). A `Caller`-style *token* is not this: rlua shipped one, mlua withdrew it | **Take**, if there is ever a plugin tier |
| **S6 — Out-of-process / FFI boundary** | Ship over `go/clib`; later, extend the ABI | esbuild's stdio manifest; SWC's wasm `Program` in/out; `py/tabnas.py` in this repo | Ships today. Cannot carry `AltAction` — not on throughput grounds, which the measured ~15% whole-parse overhead undercuts, but because the ABI has no representation for the live `Rule`/`Context` graph an action needs. Can carry `LexCheck`, `ParsePrepare` and subscribers | **Ship first** |
| **S7 — Compile the grammar to Rust** | Generate a specialised parser from a `GrammarSpec` | lalrpop `grammar<'ast>(arena: &'ast Arena)`; grmtools' two-tier compile/runtime split | The measurement that motivates it (27 rules, 76 alternates, `p`/`r`/`b` 100% static, `h`/`e` absent) shows the topology is static at *grammar-load* time, not at rustc time. A serialized spec arrives as runtime JSON by design. The salvage — intern rule names to a `u32` at load — is an interpreter optimisation with no second consumer | **Reject as framed; keep the interning** |

Two notes the table cannot carry.

**On S6's ceiling.** The honest limit is not speed. `go/clib`'s C ABI
returns accept/reject plus a code and a one-line message; an
`AltAction` needs `r.node`, `r.child.node`, `r.parent.node`, `r.o[i]`,
`ctx.t`, `ctx.v` and `ctx.rewind`, and exposing those means versioning
the whole `Rule`/`Context` object graph across a boundary — which is
the plugin-API redesign this document exists to avoid, plus a boundary.
Fixing `go/clib/core.go:115` so accepted input returns `"value"` (as
`go/clib/include/tabnas.h:17-18` already specifies) lifts B from
validation-only, and is worth doing regardless.

**On S3's payload.** The blocker recorded here is discharged: #120
names the config source, so `enum Act` is designable now. What replaces
it is a shape constraint. Config is not a property of the declaring
alternate — it is read from the rule's merged, *inherited* keep bag,
and for five of the eight variants it is removed from that bag after
reading. So no variant may carry a static struct payload; each resolves
its config by interned key at run time. That also gives `&mut Ctx` a
second, independent justification alongside the node arena: a value
builtin mutates the rule's keep bag, not only the value tree.

---

## 5. The Recommended Strategy, in Milestones

### 5.1 M0 — Adjudicate (days, TypeScript and Go, no Rust)

Four things, all of which are cheap at two runtimes and expensive at
three. **All four were adjudicated on 2026-08-19; the decisions are
recorded below and are the input to the work, not open questions.**

1. **`alt.k` vs `r.K` — DECIDED: builtin config is RULE-SCOPED.
   TypeScript changes; Go does not. (#120)**

   `rule.k` is what a builtin reads. *This reverses the direction
   recorded in the first draft of this section, which had Go moving to
   alternate-scoped;* it is recorded here rather than deleted because
   the two directions imply opposite fixes and opposite risks. The fix
   has two halves.

   *Half one — the read.* The eight VALUE builtins change
   `alt.k.<name>` to the rule bag: `node$`, `capture$`, `fold$`,
   `object$`, `array$`, `key$`, `setval$`, `value$`, at
   `ts/src/builtins.ts:127`, `:138`, `:170`, `:238`, `:249`, `:263`,
   `:273`, `:305`. Those eight are the *only* reads of the third
   argument anywhere in the file — `bubble$` (`:153`), `reset$`
   (`:254`), `push$` (`:287`), `probeInit$` (`:184`), `probeDecide$`
   (`:194`) and the three phase conditions already take two parameters
   or fewer. After the change **13 of the 16 builtins touch `rule.k` and
   none touches the alternate.** Read through the non-materialising
   `r.rawk()` view (`ts/src/rules.ts:95`), not the `r.k` getter
   (`:89`), which allocates an empty bag per invocation and then shows
   up as a k-copy at every push.

   *Half two — the delete, which the read alone gets wrong.* Go's value
   builders CONSUME their key: `delete(r.K, …)` at `go/builtins.go:248`
   (`object$`, unconditional), `:269` (`array$`, `ListRef` branch only),
   `:285` (`key$`), `:302` (`setval$`), `:340` (`value$`), documented at
   `:233-238`. The three tree builders do not. TypeScript must copy that
   exactly — `r.rawk() && delete r.rawk().<name>` immediately after the
   read, for those five only — or the fix ships a *new* divergence. The
   measured four-way table is in the Summary; the short form is that a
   grammar whose alternate both runs a value builtin and pushes gets
   `3` on both runtimes today, `3` on Go after the change, and `4` on
   TypeScript unless the deletes come with it. Guard the deletes: an
   unguarded `delete r.k.X` throws on the hand-built mocks in
   `ts/test/builtins.test.js` and raises the edit count from 4 to 10 for
   no benefit.

   That shape is not hypothetical. `jsonic/ts/src/grammar.ts:302-303`
   pairs `k: { array$: { implicit: true } }` with `p: 'list'`, and
   `:385-386` pairs `k: { object$: { implicit: true } }` with
   `p: 'pair'` — config on an alternate that both runs the builtin and
   pushes. `json/ts/src/json.ts:83-84` documents the reliance on the
   key's *absence*: "Strict JSON containers are always explicit, so
   @object$/@array$ take the default implicit:false (no `k` config
   needed)." Without the deletes, the pushed child inherits `implicit:
   true` and marks its own container implicit — silently, because the
   `info` options that surface it are off by default.

   *Measured cost.* The read-only prototype builds, and no
   grammar-driven test changes behaviour: against a baseline of 388
   tests / 386 passing (2 pre-existing README doc-example failures), it
   gives 382 passing / 6 failing — the same 2 plus exactly **4** new
   ones, all direct-invocation unit tests that hand-build a plain object
   as argument 3: `@node$` at `ts/test/builtins.test.js:126` and `:158`,
   `@key$` at `:448-453`, `@object$` at `:498-504`. The json-builder
   parity test at `:405` does not move. jsonic against the patched
   `dist` is 438/438 and byte-identical to Go across fifteen inputs
   exercising implicit and explicit containers.

   **There is no carve-out and no BNF risk.** The earlier direction
   required exempting the probe builtins, because `probeInit$`,
   `probeDecide$` and `probePhase0/1/2$` read rule-scoped `r.k`/`r.K` in
   both runtimes deliberately (`ts/src/builtins.ts:189-190`, `:203`,
   `:208-215`; `go/builtins.go:199`, `:211`, `:218-220`) and
   `@tabnas/bnf` emits `k: { pd_d: … }` on a phase-0 open that pushes
   (`bnf/ts/src/compiler.ts:1779-1780`; `bnf/go/emit_support.go:548`).
   With everything rule-scoped there is nothing to exempt: the probe
   builtins are already correct, and every grammar the BNF family
   generates — ABNF, EBNF, GBNF — is unaffected. The carve-out is
   deleted, not narrowed. `bnf`'s two hand-written `ctx.rewind` twins
   (`bnf/ts/src/compiler.ts:1801`, `bnf/go/emit_support.go:574`) already
   read `r.k.pd_mark` in both runtimes, which is the ruling confirmed at
   source in shipped downstream code.

   *The fleet scan is confirmation, not a risk register.* All 36
   `tabnas` repos were cloned and scanned. Value-builtin config is set
   on an alternate in exactly four places —
   `jsonic/ts/src/grammar.ts:296-304` and `:379-387`,
   `multisource/ts/src/multisource.ts:281-296`, and the `bnf` emitters
   at `compiler.ts:2759`, `:2764`, `:2772` — and every one pairs the
   config with its action on the *same* alternate. Two of them push;
   with the deletes in place, no descendant reads the inherited key.

   *Prose to correct.* Five comments in `ts/src/builtins.ts` say
   "per-alt": `:19`, `:82`, `:98`, `:121`, `:218`. `go/builtins.go:19-25`
   asserts "Equivalent behaviour" — true only once TypeScript adopts the
   deletes, so it is rewritten rather than merely re-pointed.
   `doc/value-builtins.md` has already been updated for the ruling but
   still describes one config lifetime ("kept as the parse descends")
   where there are two, and does not mention delete-after-read at all;
   fix that in the same change, and fix its `test/json-builder.fixture.json`
   path, which should be `ts/test/json-builder.fixture.json`.

2. **The `p`/`r` and `e` orderings — DECIDED: shared fixtures now.**
   The `h` → `e` → `p`/`r` ordering itself is aligned
   (`ts/src/rules.ts:568`, `:580`, `:653`; `go/rule.go:1125`, `:1136`,
   `:1203`). What is unpinned is the function form and the combination:
   TypeScript exposes one polymorphic field
   (`p?: string | AltNext | null | false | FuncRef`,
   `ts/src/types.ts:568`) with tests at `ts/test/cover-engine.test.js:436`
   and `:455`; Go splits it into `P string` plus
   `PF func(r,ctx) string` (`go/rule.go`) and has **zero** tests using
   `PF:` or `RF:`. Write cross-runtime fixtures for function-form
   `p`/`r` and for `h` combined with `e`, then adjudicate whatever they
   expose. Promoting these to the parity contract is the point: the API
   shapes already differ, so a port written from Go's shape would narrow
   the contract with nothing to catch it.

3. **The `alt.b` re-read — DECIDED: compute once, pre-action.**
   Go's shape becomes the contract. `consumed` is engine state fixed
   before the action runs, and `alt.b` is an input to the match rather
   than a channel an action may write. This removes a way for a plugin
   to desynchronise `ctx.t` and `ctx.v`: TypeScript evaluates
   `rule[oN|cN] - (alt.b || 0)` twice today, at `ts/src/rules.ts:619`
   (before the action, to move consumed tokens) and again at `:737`
   (after it, to shift the lookahead), so an action that writes `alt.b`
   makes the two disagree and a token moved to `ctx.v` is never shifted
   out of `ctx.t`. It is a behaviour change in TypeScript and needs a
   fixture. Verified: nothing in-tree writes `alt.b` —
   `grep -rnE "alt\.b\s*=[^=]" ts/src ts/test` returns nothing, and
   every occurrence in `ts/src` is a read (`merge.ts:246`, `:372`;
   `parser.ts:259`, `:359`; `rules.ts:619`, `:737`, `:1592-1595`). So
   the change should be observable only to a plugin already doing
   something unsupported; confirm against the downstream closure before
   landing, since that is the part this repository cannot check.

   One nuance the same grep surfaced, worth handling in the same change:
   `alt.b` may itself be a function, resolved at `ts/src/rules.ts:1595`
   as `alt.b(rule, ctx, out)` during normalisation, whereas Go resolves
   its `alt.BF(r, ctx)` inside the consumed computation at
   `go/rule.go:1181`. Both are pre-action, so this is a resolution-point
   difference rather than a behavioural one today — but it is a second
   place the two runtimes spell the same feature differently, and the
   fixture for this item should cover the function form as well as the
   numeric one.

   Also correct `go/rule.go:1176-1177`, whose comment claims the
   single-evaluation form "Mirrors the TS rules.ts ordering". It does
   not; after this change it will.

   **#122 closes the `b` channel; it does not close the alternate.**
   `alt.p` and `alt.r` are still read after the action, at
   `ts/src/rules.ts:653` and `:680`, and `out` is the reusable
   per-Context scratch (`:1226`) the action is holding. Measured against
   `ts/dist`: an `AltAction` writing `alt.p` makes the engine push the
   name it wrote instead of the declared one; writing `alt.r` redirects
   the replace the same way; a name that does not resolve fails the
   parse with `unknown_rule`. Go cannot do either —
   `AltAction func(r *Rule, ctx *Context)` (`go/rule.go:95`), with
   `pushName`/`replaceName` resolved from engine state at `:1203-1210`.
   The `grep`-based confidence used for `alt.b` does **not** transfer:
   an action writing `alt.p` is not obviously unsupported, because `p`
   is documented routing surface. Adjudicate it the same way — Go's
   shape wins, the alternate is not a post-action channel — and note
   that this, not the builtin-config question, is what actually licenses
   dropping the `AltMatch` parameter from `AltAction` (§5.5 here). It
   needs its own fixture in this milestone. Conversely, three attempts
   to make a post-action `alt.b` write observable in-tree all failed, so
   sequence this item as the first half of a two-part change whose
   second half is the signature, and do not lean on `b` as the
   motivating example.

4. **Generalise the two-runtime machinery to N — DECIDED: all of it,
   now.** `go/spec_registration_test.go:27-30`'s
   `nonParity map[string]string` has no per-runtime dimension;
   `schema/error-codes.json:46` carries a literal `goOnly` block whose
   key name already encodes an assumption that will not survive; and the
   registry's embedded engine version is coupled to the four version
   locations. The first two are one-line-per-fixture. The third —
   decoupling the registry version from the engine version — is the
   largest single item in M0 and is entangled with the release process
   described in `AGENTS.md`, so sequence it last and on its own.

   *Recorded for the record:* the recommendation attached to this
   question was to defer until a third runtime is funded, on the grounds
   that generalising costs about the same later and buys nothing until
   then. The decision was to do it now, which is the maintainer's call;
   the argument for it is that it converts a future decision-forcing
   event into a typing exercise, and that `goOnly` is poorly named even
   at two runtimes.

5. **The `n`/`u`/`k` propagation contract — DECIDED by documentation,
   pinned by nothing.** `n` and `k` are copied into a child on both push
   and replace (`ts/src/rules.ts:662-671`, `:686-695`;
   `go/rule.go:1224-1236`, `:1248-1261`); `u` is not — `rawu()`
   (`ts/src/rules.ts:94`) is read at neither TS site, and `EnsureU()`
   has exactly one non-definition caller, `go/rule.go:1161`, the alt
   merge. The contract is now stated in `AGENTS.md:196-240`,
   `ts/AGENTS.md`, `go/AGENTS.md`, `doc/value-builtins.md` and both
   `plugins.md` files. It is pinned by no shared fixture: `test/spec`
   holds eleven TSVs and none of them touches `n`, `u` or `k`.

   Per-runtime coverage is accidental and lopsided. Ablation, both
   runtimes: dropping the `n`/`k` copies on **push** breaks nothing —
   TypeScript stays at its 386/388 baseline and Go stays fully green.
   Dropping them on **replace** breaks three TS tests (builtins probe,
   serialized-grammar round-trip, rewind) and Go's
   `TestProbeFixtureParity`. Adding `u` propagation breaks one TS test
   (recover) and two Go tests. So the obvious half is the unpinned half,
   in both runtimes, and `ci/parity` and the honesty gate are blind to
   it. Worse, `ts/test/probe-grammar.fixture.json` *looks* like it
   covers push — `top$pd0` sets `k: { pd_d: '#T' }` on an alternate with
   `p: 'top$pd0$probe'` — but does not: `pd_d` is read by
   `@probeDecide$` on the same rule's close (`ts/src/builtins.ts:207`),
   never by the child. A Rust port loading the shared fixtures catches
   replace and the `u` exclusion and silently omits push.

   Land the fixture in the **same change as item 1**, because the
   k-propagation probe *is* the #120 divergence in miniature. Shape: a
   function-free `GrammarSpec` at `test/spec/propagate.fixture.json`
   plus `test/spec/propagate.tsv` with columns `start`, `input`,
   `expected` — both runtimes already parse from a named start rule
   (`ts/test/builtins.test.js:414`; `go/builtins_test.go:548-549`), and
   `lex-string-control.tsv` is the three-column precedent. Rows that are
   already measured today: a parent alternate carrying
   `k: { value$: { from: 1 } }` that pushes a child running `@value$`
   bare returns `4` on Go and `3` on TypeScript when the parent does
   *not* run the builtin (red on TS, green on Go before item 1, green on
   both after), and `3` on both when it does — which is the
   run-then-push row that catches the missing deletes. Two rows the
   fixture cannot yet carry: the replace edge, because neither runtime
   updates `rule.parent.child` on replace (`ts/src/rules.ts:686-695`;
   `go/rule.go:1246-1261`) so `@bubble$` reads a stale child and the
   probe must deliver its result through the seeded node instead; and
   the `u`-exclusion half, because `u` is observable only through
   `@setval$`, whose missing-key path itself diverges (TS writes the key
   `"undefined"`, Go writes `""` — `key, _ := r.U[slot].(string)` at
   `go/builtins.go:307`, with the same coercion splitting a non-string
   key into `{"2":4}` versus `{"":4}`). Fix that coercion, or give
   `@setval$` a skip-on-unset-slot guard, before adding the `u` rows;
   record it as a sixth divergence meanwhile. Wire both runners
   (`ts/test/spec.test.js` and a new `go/propagate_spec_test.go`) or
   `go/spec_registration_test.go:32` fails the build — which is the
   point. `test/AGENTS.md`'s "if it needs a grammar, it belongs in that
   grammar's repo" is aimed at dialect grammars; add a sentence carving
   out engine-probe fixtures, or the next reader deletes this one.

   The same contract constrains the Rust design, which is why it is an
   M0 item and not a documentation chore: a builtin's config may have
   been set by an ancestor rule, so an `enum Act` variant must resolve
   it from `r.k` at run time and can never inline it (§4's S3 note). And
   the descent is a *copy taken at push/replace time*, not a view — a
   later write to the source rule's bag is invisible to the already-
   created child — so a Rust port must not model it as a parent-chain
   lookup. The asymmetry is load-bearing downstream, not decorative:
   `expr/ts/src/expr.ts:1077` writes `r.u[pd] = r.n[pd] = 1` on paren
   open and `:1094` tests `r.u[pd] === r.n[pd]` on close, which means
   "am I the rule that opened this paren" and works only because `n`
   propagates and `u` does not.

Also worth doing here, and it is TypeScript-side design work where
`AGENTS.md` says design belongs: **split the `funcRef` surface into two
separately-versioned tiers on two code paths**, tree-sitter style.
`schema/grammar.schema.json`'s `$defs/funcRef` already half-draws the
line — "builtin refs end in `$`; anything else must be supplied in a
ref bag at load time" — but both spellings are the same callback type
on the same code path. The declarative tier already has a version
(`GrammarSpec.v`, `BUILTIN_SCHEMA_VERSION`); the imperative one has
none.

### 5.2 M1 — The Rust FFI crate over `go/clib`

**Named user:** a Rust caller holding a serialized `GrammarSpec` emitted
by `@tabnas/abnf` or `@tabnas/gbnf` who needs `grammar.accepts(src)`
inside a Rust process without a Node or Go toolchain in their build.
Today that user's options are: shell out to Node, embed Go themselves,
or nothing. `py/tabnas.py` (206 lines) already serves the same user in
Python, and 55 shared rows already pass accept/reject across the
boundary at ~15% overhead.

**This is Rome's formatter**: narrow, complete, useful on day one, from
the new language, and committing to no extension API. It adds zero
parity obligations, zero `DIVERGENCE.md` columns, zero registry
sections.

Scope: fix `go/clib/core.go:115` to return `"value"` for accepted
input, and pin the key-order surface with a fixture in the same commit.
Do **not** rename the version document in isolation —
`py/tabnas.py:141` reads the `version` key that
`go/clib/include/tabnas.h:35` would remove. Budget the artifact matrix
separately from the ~94-line wrapper: `go/clib/build.sh` cross-compiles
Linux and Windows via zig, and darwin needs a macOS host, which is the
same wall `py/README.md:42-45` records as unclimbed.

### 5.3 Decision gate before M2

Five conditions, checkable, in order. All five, not a majority.

1. **A named consumer that `libtabnas` plus a serialized spec provably
   cannot serve.** In practice only three qualify: a **wasm** target
   (`go/clib` cannot build for wasm at all — cgo is unavailable — and
   this is the one gap S6 can never close), a **no-Go-runtime**
   constraint, or **in-Rust grammar authoring**. The third reintroduces
   the plugin question, so if that is the consumer, the gate must also
   settle whether Rust ever gets an imperative tier. Anything else is
   an M1 ticket.
2. **The differential-tier entry cost paid or explicitly waived.** A
   Rust leg gets the `json` half nearly free —
   `ts/test/json-builder.fixture.json` is function-free, its only refs
   being `@object$ @array$ @key$ @setval$ @push$ @reset$ @value$` — and
   the `jsonic` half not at all. Either produce a function-free
   serialized `jsonic` spec (a genuine research question: does the
   relaxed grammar fit inside 16 builtins?) or write down that the Rust
   leg runs `json` parity and fuzz only, accepting reduced coverage on
   the tier that caught the real engine bugs.
3. **M0.4 landed** — `nonParity`, `goOnly` and the registry version
   generalised to N runtimes, before Rust exists rather than after.
4. **The unpinned surface LANDED, not merely adjudicated.** M0.1 and
   M0.3 are decided (#120, #122) and neither is implemented, so the
   gate asks for code and fixtures, not rulings: (i) #120 in TypeScript
   *including* the five guarded deletes and a TS mirror of
   `go/builtins_test.go:449-465` covering all five; (ii) #122
   implemented, and the `alt.p`/`alt.r` channel adjudicated with it;
   (iii) M0.2, the function-form `p`/`r` and `h`-with-`e` orderings;
   (iv) the M0.5 propagation fixture; (v) the feasibility report's §4.1
   `pos` units (#115), where two contract documents —
   `schema/diagnostic.schema.json:35` and `DIVERGENCE.md:66-68` —
   already misdescribe the Go runtime as counting runes when it counts
   bytes, so a port written from them ships a third answer that passes
   every fixture; (vi) serialized regex terminals (#118), adjudicated as
   a TS-vs-Go divergence rather than deferred as a Rust lowering choice;
   (vii) parsed key order, pinned with a non-alphabetical
   integer-like-key row; (viii) #113 and #119, which land on M2a's
   validator and on the no-panic guarantee respectively. M0.4 remains
   condition 3 and is untouched by any of this.
5. **A second maintainer with Rust ownership**, or written acceptance
   that the third runtime is unmaintained.

### 5.4 M2a — `tabnas-spec`: loader and validator, no engine

Validates against `schema/grammar.schema.json`, resolves `@name$`
against the 16 builtins as a `#[repr(u8)] enum`, checks `v` against
`BUILTIN_SCHEMA_VERSION`. Useful standalone — a build script validating
a compiled grammar artifact — at roughly 1-2k lines and zero parity
obligations.

The rationale recorded here — "its value is that it forces the
`enum Act` decision, and therefore M0.1, before any engine exists" — is
spent: #120 settled the scoping question, so M2a now forces nothing.
The honest, smaller rationale is schema validation, ref-name
resolution and the `v` gate. State the ceiling with it, because it is
permanent rather than a schema gap: `schema/grammar.schema.json:161-163`
types `k` as a bare open object with no per-builtin shape, and
rule-scoping makes that unclosable — the alternate that declares
`k.value$` and the rule whose action consumes it are different objects
at different depths, so no static validator can associate them. A
loader can check ref names and the version gate; it can never check
builtin config.

One scoping honesty note: a loader that *rejects* `@name` ref-bag refs
is narrower than what ships and narrower than option C in the
feasibility report's own table. The serialized tier is
function-*deferred*, not function-free —
`ts/test/serialized-grammar.test.js:127` and `:151` exercise a pure-JSON
spec plus `ref: { '@mkbad': (rule, ctx) => … }`, mirrored on the Go
side — and that is the only in-tree exercise of the `e` slot *from a
serialized spec*. Record the narrowing and the two tests it fails; do
not describe it as equivalent to option C.

### 5.5 M2b — The engine

On the S2 substrate, with S3, the S4 error/backtrack channel, and S5 if
a plugin tier is ever offered. The shape, restated with the corrections
this document establishes:

```rust
pub type Action = Box<dyn Fn(&mut Ctx, RuleId) -> Result<Effect, Fault>>;

fn process(g: &Grammar, ctx: &mut Ctx, lex: &mut Lex, rid: RuleId)
    -> Result<RuleId, Fault>;
```

Two arguments, not three — identical to Go's
`AltAction func(r *Rule, ctx *Context)` (`go/rule.go:95`). This
supersedes the feasibility report's §3.6 note, "The callback must carry
the `AltMatch`", whose argument was that "every built-in tree/value
builder reads per-alternate config out of it
(`ts/src/builtins.ts:127`, …, `:305`)". #120 moves every one of those
eight citations to `r.k`, and they were the only reads of the alternate
in the file, so that half of the objection is gone.

**State the price rather than presenting it as free**, because an
earlier draft that dropped the parameter was correctly refuted and the
refutation was only half-demolished. Three things are true at once.

*It is a narrowing of six public types, not one.* TypeScript still
declares `AltCond` (`ts/src/types.ts:706`, called at
`ts/src/rules.ts:1481`), `AltModifier` (`:716` / `:569`), `AltNext`
(`:734` / `:1581`, `:1588`), `AltBack` (`:741` / `:1595`) and
`AltError` (`:758` / `:1575`) as three-argument, and neither ruling
touches them. Collapsing the whole surface to `(rule, ctx)` adopts Go's
existing contract wholesale (`go/rule.go:92-104`, `:150-152`) — which
is the right destination, and is exactly the kind of silent contract
narrowing item 2 of §5.1 exists to forbid. Declare it as an M0
adjudication. The two things it buys that are worth the declaration:
it closes the `alt.p`/`alt.r` post-action channel structurally, and it
takes argument-3 pass sites in `ts/src/rules.ts` from eight to zero.

*It does not, by itself, remove the §3.4 scratch hoist.* Compiled,
rustc 1.94.1: a three-argument `AltCond` called as
`c(ctx, rid, &mut ctx.palt)` is `error[E0499]` ×2 **even when the
action is already two-argument**. The hoist follows from the imperative
tier's arity and from nothing else, in both directions. What the
collapse actually buys is that the scratch's placement becomes *free*
rather than forced: with no callback receiving it, leaving it on the
`Context` compiles, and so does hoisting it. Pick one deliberately.

*It costs no lifetime.* An argument-3 design does not force `'g` into
`Ctx`/`Grammar`/`Tabnas`; a scratch holding `Copy` config indices
reproduces TypeScript's aliasing with no borrow, and a higher-ranked
callback type compiles the borrowing spelling too. Any claim that
keeping the parameter costs a self-referential lifetime is an artifact
of one probe's modelling, not a property of the design.

`AltModifier` is the exception and stays one: it *replaces* the whole
object (`alt = alt.h(rule, ctx, alt, next) || alt`,
`ts/src/rules.ts:569`), so it keeps the alternate **by value** —
`Fn(&mut Ctx, RuleId, AltMatch, RuleId) -> AltMatch` via `mem::take`
and put-back — or, in Go's shape, as `&'g AltSpec`, which additionally
makes the `go/rule.go:1372` grammar-corruption bug a compile error. Do
not "make it mutate in place": that is E0499 against the arena and
reinstates the hoist single-handedly (§6 here). At 152 bytes and zero
shipped invocations across the fleet, §2.4's `Fold`/memmove warning
does not bite here.

**Do not delete the scratch along with the parameter**, and do not
confuse it with what the `RuleDone` payload needs. The payload is a
five-field resolved record — `b`, `g`, `p`, `r`, `err`, built at
`ts/src/parser.ts:252-266` and again on the throwing path at `:352-366`
— so it must be observable after a *failed* pass; it cannot be a value
returned through `?`, and it is not the scratch, which also carries the
callbacks and the config alias that no consumer sees. Whether that
record reports the resolved or the static value is the open M0 question
this creates: TypeScript reports resolved (`ts/src/rules.ts:1581` →
`:577` → `ts/src/parser.ts:264`), Go reports the static grammar field
(`go/parser.go:827-830`, because `PF` resolves into a local at
`go/rule.go:1203-1210` and is never written back), measured, and
unpinned on both sides. Go's answer is a divergence from canonical
TypeScript, not a simplification to copy.

The rest of the shape is unchanged. `&mut Ctx`, not `&Ctx` (E0596 —
the node arena is the reason, and it survives the collapse). `Effect`,
not `()`, because the return channel is free and the error-token
short-circuit already uses it. Ids by value. The action list borrowed
from the grammar as `&'g [Act]` — by preference now rather than by
borrow-check necessity, since the E0502 that forced it dissolves with
the third argument (§2.3 here). `Send + Sync` behind a RustPython-style
trait alias gated on a feature, never as a consequence of the receiver.
Dedupe on the `Act` discriminant for the closed tier and `Rc::ptr_eq`
for the open one — `merge.ts`'s `toString()` fallback has no spelling
and should be declared dead.

### 5.6 M3 — The imperative plugin tier

Not scheduled. See §6 here.

---

## 6. What Not to Do

**Do not attempt the imperative plugin API in M1 or M2.** Every project
in the survey either never shipped one (Ruff — the FAQ says a plugin
system is "within-scope", and issue #283 has been open since September
2022), took four-plus years to ship a single-effect DSL (Biome), paid
three years of ABI breakage (SWC), or recovered ESLint-compatible
fidelity only after months of zero-copy "raw transfer" engineering
(oxlint). tabnas is in a worse position than any of them on two counts:
it runs at ~3 MB/s with an optimistic 2-4x from Rust, so there is no
headroom to spend a boundary out of; and its plugins contribute
*grammar* — CSV, TOML, YAML, XML — so Ruff's "upstream it" substitute
is not available, because `AGENTS.md` rule 3 forbids folding a grammar
back into the engine.

**Do not attempt the full port (option A).** Rome is the precedent: the
rewrite outlived the company that started it.

An earlier draft of this paragraph argued that the second runtime had
attracted only 2 of 21 downstream packages. **That was wrong, and the
correct figure argues the same conclusion harder.** Measured across all
36 `tabnas` repositories: 31 carry a TypeScript package depending on
`@tabnas/parser`, and **31 carry a Go module depending on
`github.com/tabnas/parser/go`**. The two-runtime figure came from
`ci/parity/gotokdump/go.mod` and `ci/bench/gobench/go.mod`, which
`replace` only `json` and `jsonic` — that is the parity harness's
dependency set, not the fleet's Go coverage.

So Go adoption is near-total, not marginal. That removes the "nobody
followed the second runtime" argument and replaces it with a worse one:
every engine change is already paid for across ~31 Go modules as well as
~31 TypeScript packages, and a third runtime multiplies that base again
against a maintainer who wrote 49 of this repository's 52 commits. The
per-change tax measured in §6 of the feasibility report — 1.76x
aggregate — is the cost of keeping *two* in step across that fleet.

**Do not transliterate to `Rc<RefCell<Rule>>`.** §2.4 here. Boa filed the
same borrow panic for six years. RustPython moved off it. The
"Learning Rust" book's verdict on the safe doubly-linked list — "a
nightmare to implement, leaks implementation details, and doesn't
support several fundamental operations" — is the honest summary, and
the leak reaches the public signature.

**Do not adopt a branding-lifetime handle token.** rlua shipped one in
0.16 as its headline fix, and it was withdrawn: mlua dropped the
`'lua` lifetime, and rlua 0.20 became a wrapper around mlua. A
wasmtime-style `Caller` is also not an aliasing fix — reaching the
`AltMatch` *through* the token is E0499, verified, which after #120
applies to the `h`/`e`/`p`/`r`/`b` tier only, since no `AltAction`
reaches the alternate any more — it is a capability fence, which S5
provides more cheaply.

**Do not ship any serialized ABI without versioning and
unknown-variant tolerance in the first commit.** SWC's three years is
the price of not doing so, and the discipline already exists here on
the correct tier (`GrammarSpec.v`, `BUILTIN_SCHEMA_VERSION = 3`).

**Do not defer the value-tree edits into a two-phase apply.**
rust-analyzer's Roslyn-derived `SyntaxRewriter` was abandoned as
"awkward to use and also requires O(N) processing, as it essentially
rebuilds the whole tree" — but note the honest reading: it came *back*,
as `SyntaxEditor`, which ships today with an explicit O(changes)
pending-state query (`pub fn deleted(&self, element) -> bool`). So the
precedent is not "deferral fails"; it is "deferral needs a pending-state
query and a re-identification scheme, and you will build both". Here it
is simply unnecessary: once nodes are `NodeId`-keyed, immediate writes
are already sound.

**Do not build the Rust benchmark arm early.** §5.5 of the feasibility
report names the hazard: Rust numbers as a CI artifact create pressure
to treat Rust as the reference, which `AGENTS.md` authority rule 1
forbids and which inverts the canonicality structure. Write the ADR
first.

**Do not remove `ctx.rewind()` from a plugin handle and call it a
split.** Eleven in-tree grammar actions use it, it is documented option
surface, and §2.3 here shows a fenced handle that keeps it compiles. Fence
the rule stack, the grammar and the lexer cursor instead — nothing
in-tree reaches those from a callback, so the restriction is free.

**Do not land #120 as eight read-site edits.** A move to `r.k` without
Go's five consume-once deletes (`go/builtins.go:248`, `:269`, `:285`,
`:302`, `:340`) makes a pushed child inherit its parent's value-builtin
config — measured, and the shipped shape it hits is
`jsonic/ts/src/grammar.ts:302-303` and `:385-386`, where an explicit
container silently becomes implicit. The failure is invisible with the
`info` options off, which is the default, so the ordinary suite will
not catch it. Relatedly, **do not describe builtin config as uniformly
"kept as the parse descends"** — that is true of
`node$`/`capture$`/`fold$` and false of the other five, and
`doc/value-builtins.md` currently says the first for all eight.

**Do not make `AltModifier` mutate in place.** The feasibility report's
§3.6 floats it as an alternative to dropping `h` from the Rust surface.
It is E0499 against the arena and it single-handedly reinstates §3.4's
scratch hoist. Take-and-return is correct here precisely because it *is*
`h`'s semantics (`alt = alt.h(rule, ctx, alt, next) || alt`,
`ts/src/rules.ts:569`), the value is 152 bytes, and `h` appears in zero
shipped grammars across the fleet — the memmove SWC measured on its AST
does not apply to a per-pass scratch nothing calls.

**Do not drop the third `AltAction` parameter without writing down what
it closes.** It is the right move and the fleet cost is zero readers,
but `alt.p`/`alt.r` are a live TypeScript redirect channel (measured,
`ts/src/rules.ts:653`, `:680`) with no Go equivalent and no fixture, and
collapsing the other five alternate-taking types with it narrows five
more public contracts. A Rust engine that drops the parameter silently
ships all of that as an implementation detail — which is exactly the
§4.1 failure mode this document exists to prevent. Declare it in M0,
land a fixture, and **do not delete the scratch `AltMatch` along with
the parameter**: Go has none, reports the static grammar fields to
`RuleDone`, and that is a divergence from canonical TypeScript rather
than a simplification to copy.
