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
porting first is not, today, semantically closed.** TypeScript builtins
read their per-alternate config from `alt.k.<name>` — the per-pass
scratch `AltMatch`, reset every pass. Go builtins read it from `r.K`,
which is a rule field and is *inherited* by every pushed or replaced
child (`ts/src/rules.ts:670`, `:694`; `go/rule.go:1231-1235`). Measured
on one pure-JSON grammar with no closures and only `$`-refs, input
`ab`:

| runtime | output |
|---|---|
| TypeScript | `{"rule":"P","src":"a","kids":[]}` |
| Go | `{"kids":[{"kids":[],"rule":"P","src":"b"}],"rule":"P","src":"ab"}` |

`doc/value-builtins.md:17` records the two spellings — "`alt.k.<name>`
(TS, 3rd action arg) / `r.K` (Go, merged before the action)" — as
though they were equivalent. They are not. There is no `DIVERGENCE.md`
entry, no `go/doc/differences.md` entry, and no shared fixture, so
`ci/parity` cannot see it. A Rust `enum Act` with inline struct payloads
must pick one scoping rule and will ship a third answer against
whichever runtime it does not copy — §4.1 of the feasibility report,
happening on the one tier the strategy depends on.

Five further corrections to the shape a port should take, each
compiled or measured:

- **`&mut Ctx` stays.** A shared-receiver action signature is E0596
  against the arena design it would sit on: 11 of the 13 builtin
  actions write the node arena, and `ctx.nodes[nid].src.push(..)` from
  `ctx: &Ctx` does not compile. Deferring the in-action rewind removes
  one reason an action needs `&mut Ctx`; the node arena is the reason.
- **The read-back at `ts/src/rules.ts:737` *is* callback-dependent.**
  Not through `rule.oN`/`cN`, which are engine-written before the
  action, but through `alt.b` — argument 3, which the action can write
  and the engine re-reads. Go computes the same quantity once,
  pre-action (`go/rule.go:1178-1192`), and its `AltAction` cannot see
  the alternate at all. That is a fourth unrecorded divergence.
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

### 1.1 The `:737` read-back is a data dependency on the callback

§3.1 cites `ts/src/rules.ts:737` —
`let consumed = rule[is_open ? 'oN' : 'cN'] - (alt.b || 0)` — as the
engine reading back through the handles it passed in. Half of that is
wrong in a way that matters and half is right in a way the report
understates.

`rule.oN` and `rule.cN` are written at exactly two sites,
`ts/src/rules.ts:1468` and `:1474`, both inside `parse_alts`, which
completes before the action runs at `:646`. No builtin, plugin or test
writes them. So the `oN` half is engine state.

`alt.b` is not. It is a field of the scratch `AltMatch` handed to the
action as argument 3. The same expression is evaluated twice — once at
`ts/src/rules.ts:619` as `_cons`, before the action, to push consumed
tokens onto the rewind history; and again at `:737`, after the action,
to shift the lookahead buffer. An action that writes `alt.b` makes the
two disagree, and a token that was moved to `ctx.v` is never shifted
out of `ctx.t`.

Go computes it once, pre-action, and comments that this "Mirrors the TS
rules.ts ordering" (`go/rule.go:1178-1192`). It does not. Go's
`AltAction func(r *Rule, ctx *Context)` cannot reach the alternate, so
the divergence is currently unobservable from Go — which is exactly the
condition under which a port settles it by accident.

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

### 1.3 The closed builtin tier is not semantically closed

Stated in the summary and measured there. Two consequences for the
port. First, `enum Act` cannot be designed until the scoping rule is
adjudicated, because the config source is part of the variant's
meaning, not an implementation detail. Second, Go's `builtinValue`
already carries the workaround — `delete(r.K, "value$")`
(`go/builtins.go:340`) — which is evidence that `r.K` persistence was
felt and patched locally rather than fixed.

There are two further ordering divergences of the same family, both
currently latent:

| what | TypeScript | Go | pinned? |
|---|---|---|---|
| function-form `p` / `r` | resolved in `parse_alts`, `ts/src/rules.ts:1577-1589` — **before** the action | resolved at `go/rule.go:1203-1210` — **after** the action | TS side only, `ts/test/cover-engine.test.js:436`, `:455`; no shared fixture |
| `e` relative to `h` | `e` evaluated in `parse_alts` (`ts/src/rules.ts:1575`), i.e. **before** `h` runs at `:569` | `H` at `go/rule.go:1126`, then `E` at `:1137` | no; `h` appears in zero shipped fixtures |
| `alt.b` re-read after the action | yes, `ts/src/rules.ts:737` | no, computed once at `go/rule.go:1178` | no |

Function-form `b` matches (both pre-action). None of these is a Rust
question. All three are decided, permanently, by whoever writes the
Rust engine first.

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
present, and that the engine performs the `:737` read-back *after* the
callback loop with `alt.b` action-writable:

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

type PluginAct = Box<dyn Fn(&mut PluginCtx, &mut AltMatch) -> Result<(), Fault>>;

fn run_actions(ctx: &mut Ctx, rid: RuleId, alt: &mut AltMatch,
               acts: &[PluginAct]) -> Result<(), Fault> {
    for a in acts {
        let mut pc = PluginCtx { c: ctx, rid };   // fresh reborrow per call
        a(&mut pc, alt)?;
    }
    let consumed = ctx.rules[rid.0 as usize].on.saturating_sub(alt.b);
    // ... the ts/src/rules.ts:737 read, after the actions
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

The second is a design constraint, not a defect: the action list must
live in the grammar (`&'g [Act]`), never on the scratch, which is what
TypeScript already does — `composedAction` closes over its function
list at grammar-build time (`ts/src/rules.ts:1845-1864`) and the
per-pass `out.a` merely points at it.

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
| `AltAction` `a` | `(rule, ctx, alt) => any` / `func(r *Rule, ctx *Context)` | `ts/src/rules.ts:646`; `go/rule.go:1197` | **D** (one member); C otherwise | yes — `a`, scalar or array |
| `AltCond` `c` | `(rule, ctx, alt) => bool` \| `Record` / `AltCond` + `CD` | `ts/src/rules.ts:1481`; `go/rule.go:1544` | B (by contract, not by construction) | yes — funcRef **or** declarative object |
| `AltModifier` `h` | `(rule, ctx, alt, next) => AltMatch` / `func(alt *AltSpec, r, ctx) *AltSpec` | `ts/src/rules.ts:569`; `go/rule.go:1126` | C — and in Go it is handed the **live grammar** | yes |
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
| **S0 — Adjudicate first** | Settle `alt.k` vs `r.K`, the `p`/`r` and `e` orderings, and the `alt.b` re-read, in TS and Go, with fixtures | The feasibility report's own §4.1 argument about `pos` | Days. One Go change (give `AltAction` the alternate, or snapshot the alt's own `k` per pass), three fixtures, one `DIVERGENCE.md` row if any is declared deliberate | **Prerequisite.** Cheap now, unfixable three-way argument later |
| **S1 — Transliterate the callback API** | `Rc<RefCell<Rule>>`, weak-capture macros, keep the signatures | gtk-rs `glib::clone!` — the largest callback-based Rust GUI codebase, whose most-used ergonomic tool exists to make capture-and-upgrade bearable, and whose users largely migrated to Relm4 to stop writing it | Every action becomes a `'static` closure over `Rc<RefCell<Ctx>>`, every body opens with upgrade-or-bail, and the runtime abort is replaced by a silent no-op. `clone!` manages capture *lifetime*, not *aliasing*, so arg1==arg3 is untouched | **Reject.** Boa #663→#5337 is six years of the same panic |
| **S2 — Arena + `RuleId`/`NodeId`** | The §3.6 design: `Copy` ids, `&'g Grammar`, `node_parent` side table, one checked two-node accessor | Unanimous: rust-analyzer `Idx<T>`, oxc `parent_ids`, wasmtime `StoreId`, mlua `ValueRef`, starlark `Value<'v>` | Solves the aliasing, the cycles, and the two-node writes. Does **not** solve memory retention (~0.7 rule passes per source byte) or the API break. Every downstream plugin is rewritten by hand | **Mandatory** for any in-process engine |
| **S3 — Closed `enum Act` for the 16 builtins** | Bucket A as `#[repr(u8)] enum Act` with struct payloads, dispatched by `match` inside `process()` | Biome `declare_lint_rule!` + GritQL; Ruff's in-tree rule set; chalk's closed `RustIrDatabase` | Zero `dyn` for specs loaded from JSON, zero aliasing on that path, and the dedupe key becomes an enum discriminant. Closes the world: a third party needs a new variant or a `Custom` arm that reinstates the problem. **Blocked on S0** | **Take**, after S0 |
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

**On S3's blocker.** `enum Act` is not designable until S0 lands,
because the config source — per-pass alternate versus inherited rule
field — is part of each variant's meaning. Picking one now means
picking a runtime to disagree with, silently, on a path no fixture
covers.

---

## 5. The Recommended Strategy, in Milestones

### 5.1 M0 — Adjudicate (days, TypeScript and Go, no Rust)

Four things, all of which are cheap at two runtimes and expensive at
three. **All four were adjudicated on 2026-08-19; the decisions are
recorded below and are the input to the work, not open questions.**

1. **`alt.k` vs `r.K` — DECIDED: Go moves to alternate-scoped.**
   TypeScript is canonical (`AGENTS.md` rule 1), so Go changes: either
   give `AltAction` the alternate, or snapshot the alternate's own `k`
   into a per-pass slot the builtins read. Config becomes local to the
   alternate that declares it, and a parent can no longer silently
   reconfigure a child's builtin. Pin it with a shared fixture using the
   two-rule grammar in the Summary, plus its control (the same grammar
   with the parent's `k` removed, where the runtimes already agree).
   Correct `doc/value-builtins.md:17`, which records the two spellings
   as equivalent, and `go/builtins.go:22-24`, which asserts "Equivalent
   behaviour" outright. Tracked as #120.

   *Known risk, accepted:* nothing currently tests inherited config, so
   if a downstream Go grammar relies on a parent configuring a subtree,
   this removes that capability silently. The fixture should land before
   the behaviour change so the break is visible in one commit.

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
4. **The unpinned surface adjudicated** — M0.1-M0.3, plus the feasibility report's §4.1 `pos`
   units, where two contract documents already misdescribe the Go
   runtime and a port written from them ships a third answer that
   passes every fixture.
5. **A second maintainer with Rust ownership**, or written acceptance
   that the third runtime is unmaintained.

### 5.4 M2a — `tabnas-spec`: loader and validator, no engine

Validates against `schema/grammar.schema.json`, resolves `@name$`
against the 16 builtins as a `#[repr(u8)] enum`, checks `v` against
`BUILTIN_SCHEMA_VERSION`. Useful standalone — a build script validating
a compiled grammar artifact — at roughly 1-2k lines and zero parity
obligations. Its value is that it forces the `enum Act` decision, and
therefore M0.1, before any engine exists.

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
pub type Action =
    Box<dyn Fn(&mut Ctx, RuleId, &mut AltMatch) -> Result<Effect, Fault>>;

fn process(g: &Grammar, ctx: &mut Ctx, lex: &mut Lex, rid: RuleId)
    -> Result<RuleId, Fault>;
```

`&mut Ctx`, not `&Ctx` (E0596). `Effect`, not `()`, because the return
channel is free and the error-token short-circuit already uses it. Ids
by value; the action list borrowed from the grammar as `&'g [Act]`, not
owned by the scratch (E0502). `Send + Sync` behind a RustPython-style
trait alias gated on a feature, never as a consequence of the receiver.
Dedupe keyed on ref *name* for the closed tier and on `Rc::ptr_eq` for
the open one — `merge.ts`'s `toString()` fallback has no spelling and
should be declared dead.

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
rewrite outlived the company that started it. The fleet here is 21
downstream TypeScript packages, of which exactly two — `json` and
`jsonic` — have Go ports. The second runtime attracted 2/21 of the
ecosystem; a third will attract less, and every one of those 21 is
rewritten by hand against an API that does not exist yet.

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
`AltMatch` *through* the token is E0499, verified — it is a capability
fence, which S5 provides more cheaply.

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
