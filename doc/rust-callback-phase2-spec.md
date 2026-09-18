# Callback-contract implementation spec (Phase 2): split the borrow — revision 5

Engine: `/home/user/parser/rs` at `main` = `55fb49c`. Every line number below is against that commit. Nothing in this spec was built or run; the numbers marked *verified* are from `$S/BRIEFING.md`, everything else is arithmetic on them and is labelled as such. Revision 2 closed the eighteen required changes of the first refutation round (cited **[Rn]**). Revision 3 closed the twenty required changes of round r1 — six parity (**[P1]**–**[P6]**), fourteen soundness (**[S1]**–**[S14]**). **Revision 4 closes the thirteen required changes of round r2 — seven from the parity lens (**[Q1]**–**[Q7]**) and six from the soundness lens (**[T1]**–**[T6]**)** — each cited at the place it lands, plus three accuracy corrections the r2 refuters flagged as notes rather than as required changes (marked **[n]**). **Revision 5 closes the eight required changes of round r3 — four from the soundness lens (**[U1]**–**[U4]**) and four from the parity lens (**[V1]**–**[V4]**)** — each cited at the place it lands, plus five accuracy notes the r3 refuters raised without requiring them (also marked **[n]**). Nothing else is changed.

Both r2 refuters returned `refuted: true` on details, not on the design: the `Box`-tree acyclicity argument, the disjointness of `rule` and `context` on every engine path, the five verified copy sites becoming field stores, `error.rule_stack` byte-identity, the `RuleCursor` walker, `materialise_next`, `RuleDone<'a>`'s HRTB behaviour and all three plugin migrations were each independently compiled or traced and survive unchanged. What follows fixes one behavioural regression the spec's own tables contradicted each other about (**[T1]/[Q5]**), one place where the specified recovery code could not produce the value four other sections promise (**[Q1]**), two false statements about rustc and about clippy attributes already in the tree (**[T3]**, **[T5]**), and six smaller precision gaps. The list of r2 findings that needed no change is Appendix C.

Round r3 returned `refuted: true` on details again, and again not on the design. The soundness refuter compiled the whole of §3.3 — `materialise_next`, the rewritten `run_after_actions` loop, all three transition arms, the entire `recover_error_pass` `done_next` match with its five coexisting borrow sources, the three `attempt_recover` arms and the recursive `RuleCursor` walker including `Chain { tail: &tail }` on a stack local — under rustc 1.94.1 / edition 2021, and every one borrow-checks; **[T3]**, **[T6]**'s two error codes and §0.3's withdrawal are now empirically confirmed rather than argued. The parity refuter re-derived `ancestors()`, the live-rule reads, the head-of-chain `child`/`next`, the ruleDone post-transition stack, the `done.next` tag table, test 7's per-port rows and both plugin migrations against `ts/src/rules.ts` and `go/rule.go` and found them right. What revision 5 fixes is: one behaviour change the spec recorded for the after phase and denied for the before phase, although one edit causes both (**[U1]**); one place where the specified POP arm would let a ruleDone subscriber change what `parse()` returns, under a stated parse-result freeze (**[U2]**); one capability the changelog claims survives and which does not, for one callback kind (**[V2]**); two recovery events whose specified code cannot produce the value §2 documents (**[V1]**, **[V3]**); an argument miscount inside the table that exists to correct an argument miscount (**[U3]**); a stage-A expectation resting on a holder that an `Rc::make_mut` has already detached (**[U4]**); and the changelog's opening claim that no parse result changes, contradicted by its own next clause (**[V4]**). The r3 findings that needed no change are Appendix D.

---

## 0. Disposition of rounds r1, r2 and r3, and what they changed

### 0.1 Round r1 required changes, one row each

| id | lens / subject | where it lands | what changed |
|---|---|---|---|
| **P1** | pending rule must stay reachable from EVERY after-action | §2 (`Context::pending`), §3.1, §3.3 (`run_after_actions` protocol, `NextOf`), §6 test 1, §7, §10 | New `pub(crate) pending: Option<Box<Rule>>` on `Context` + `pub fn Context::pending(&self) -> Option<&Rule>`. The engine moves the pending push/replace rule into it for the duration of the after-actions. §10 item (2) — the capability removal — is **deleted**; the capability survives, by a new path. **NARROWED BY [V2]: not for a named plain `Action`, which receives no `Context` at all; §0.4 R9.** |
| **P2** | failed after-action: `done.next` and the unrun child | §3.3 (`recover_error_pass`, `attempt_recover`), §6 tests 3/6, §10 item (3) | `recover_error_pass` takes the pending out of the context, hands it to `attempt_recover` as `&mut Option<Box<Rule>>`, and reports it as `done.next`. On the `accepts_close` path after a failed **push** after-action the unrun child is retained (`current_rule.child_rule = Some(..)`, tag stays `Child`). §10 item (3) now covers the **replace** case only. |
| **P3** | link stored BEFORE the ruleDone event | §2 (`retire_in_place`), §3.3 (replace and pop arms), §6 test 3 | The replace/pop arms store the completed rule into `prev_rule`/`child_rule` first, fire the event, then strip in place. This **restores today's ordering** (:2536 and :2602 already precede the notify at :2610); revision 2's per-arm rewrite had inverted it. Test 3 now asserts `prev.next.prev === prev` and the pop's `done.parent.child_rule` instead of exempting them. |
| **P4** | `rule.child()` = head of the completed chain | §2, §3.2, §3.3 (forced-close loop), §4, §6 tests 6/7, §7 | New `pub fn Rule::child(&self) -> Option<&Rule>`, the rule this one pushed (TS `rule.child`, Go `r.Child`). `done.next` in the forced-close loop is `current_rule.child()`, the head, not `child_rule.as_deref()`, the tail. The migration table points at `child()` instead of a hand-walk. |
| **P5** | `u_mut` copy counter vs the shared empty map | §3.2, §6 test 11 | The counter fires only when the map is not the thread-local empty sentinel. `empty_values()` (rule.rs:1283-1290) hands every rule one shared `Rc`, so `strong_count > 1` is true on every rule's first write. |
| **P6** | changelog: `Rule: Clone` removal is a silent type change; `bo`/modifier `next` unchanged | §10 | Both recorded. **The `bo` half is SUPERSEDED BY [U1]: the before-open `next` copy moves exactly as the after-phase one does, and the "unchanged" claim is deleted. Only the `AltModifierWithMatch` half survives.** |
| **S1** | `materialise_next`'s `Parent` arm does not compile | §3.3 | Written as a `match`. Generalised into a rule (§0.3). |
| **S2** | the :1540 lazy rewrite does not compile | §3.3 | Written as `if is_open { Self::materialise_next(..) } else { None }`. |
| **S3** | `debug_u_copies` counts the empty sentinel | §3.2, §6 test 11 | Same fix as **P5**; one implementation serves both. |
| **S4** | `Index for Ancestors` on a by-value view is E0716 | §2, §7 | `Index` kept (the panic surface is part of the design's claim) with the one-expression restriction documented; the migration row now says `[i]` → bind the view first, or use `get(i)`/`last()`. **SUPERSEDED BY [T3]: the E0716 premise is false and the restriction does not exist.** |
| **S5** | `ancestors_for` cannot slice a private field | §2, §3.3 | `Ancestors::without_last(self)`, `pub(crate)`, specified in context.rs. |
| **S6** | `notify_rule_done` trips `clippy::too_many_arguments` | §3.3 | `#[allow(clippy::too_many_arguments)]`. **The citation is CORRECTED BY [T5]: `attempt_recover` carries no such attribute and needs none; `recover_error_pass` (:799) and `recover_after_actions` (:848) are the two that do.** |
| **S7** | wrong CI citations | §3.7, §8 A3 | Makefile target is `test-rs` (Makefile:46-48); the test line is `ci/rust/run.sh:11`. |
| **S8** | the "live `next`" claim is false for self/parent | §9, §10 | Restricted and re-derived under **P1** (see §0.2); the after-action ordering change is recorded. |
| **S9** | unrecorded ruleDone link differences | §10 | Superseded by **P3** for replace and pop (see §0.2); the residual — the pusher's `child_rule` is `None` on a push event — is recorded. |
| **S10** | §10 item (3) must also cover `child.*` | §10 | Superseded by **P2** for push (the unrun child is retained, so `child.*` resolves to it); recorded for replace. |
| **S11** | migration: E0502 on a held ancestor; `rule_stack[rule.d - 1]` in a ruleDone | §7 | Both rows added. |
| **S12** | `Replaced` on a chain tail has no successor | §4 | The walker terminates at the tail and yields `None`. |
| **S13** | test 10's retention count only holds for a linear nest | §6 test 10 | Grammar shape stated; a child dropped by a later push is asserted unreachable. |
| **S14** | "read-only by type" over-promises | §2 | Reworded: no `&mut Rule` is reachable; a frame's `node` cell is still writable through `borrow_mut()`, as `rule_stack[i].node` was and as TypeScript allows. |

### 0.1b Round r2 required changes, one row each

| id | lens / subject | where it lands | what changed |
|---|---|---|---|
| **T1** | the `next` hop by the `Child` tag must route through the LIVE descendant, not `Rule::child()` | §4 (link table, hop table), §6 test 7, §10 | With `child_rule` cleared at the push (§3.3), a buried frame's `Rule::child()` is `None` for the whole life of its child, so `{"parent.next.name": ..}` would have resolved to nothing where today's Rust, TypeScript and Go all resolve it to the pushed child. The `next`-by-`Child` row now runs the SAME cursor `child` hop the row above it already specifies (live descendant → chain head), at zero cost. |
| **T2** | test 7 has no row evaluated while a child is live | §6 test 7, `ts/test/rule-links.fixture.json` | Four rows added on `child`'s open and close passes (`parent.next.name`, `parent.next.state`, `parent.child.name`), per port. Without them the **T1** regression is invisible to the whole plan (grep of `rs/`, `ts/`, `go/`, `test/spec/*.tsv`: no existing case uses `parent.next`). |
| **T3** | the E0716 claim is false | §2 (`Index` doc), §7 (migration row), §10 | `let frame = &context.ancestors()[i];` **compiles** — temporary lifetime extension covers the operand of an index expression in a `let` initializer, and `frame` stays usable afterwards. The real restrictions are E0515 (returning the reference out of a function) and E0502 (holding it across a `&mut` use). **[S4]** was closed on a false premise; the spec no longer tells plugin authors to rewrite working code. |
| **T4** | §10 item (3)'s "not a divergence for a PUSH" is true only on one recovery arm | §0.4 R5, §3.3 (`attempt_recover`), §6 test 6, §10 items (3) and (7) | The `match (next_rule, pending)` normalisation now covers the `!pop_until_valid` arm (parser.rs:768-770) and the forced-pop loop (:784-796) as well as `accepts_close` (:775-782); where nothing can own the unrun rule the tag is reset to `NoRule` so it cannot dangle, and the residue is recorded. Test 6 gains a third case driving `!pop_until_valid`. |
| **T5** | wrong `#[allow(clippy::too_many_arguments)]` citation | §3.3 | `attempt_recover` (parser.rs:634) carries **no** such attribute — only `recover_error_pass` (:799) and `recover_after_actions` (:848) do — and it is 7 arguments today and 7 after the `stack`→`pending` swap, so it needs none. `notify_forced_close` at 6 arguments needs none either. Only the 8-argument `notify_rule_done` gains one. |
| **T6** | the two `compile_fail` doctests pass on any compile error | §2, §3.7 | Annotated ```compile_fail,E0502``` and ```compile_fail,E0596```. They are the entire replacement for the capability `introspection_test.rs:495-560` loses under §0.4 R1, and §3.7 adds `cargo test --doc` specifically so they run. |
| **Q1** | the specified `accepts_close` arm makes `done.next` `None` for a failed REPLACE | §3.3 (`attempt_recover`, `recover_error_pass`), §2 (`RuleDone::next`), §6 test 6 | `match (current_rule.next_rule, pending.take())` emptied the caller's `Option` on **every** arm and dropped the unrun replacement inside the arm, so `recover_error_pass` could not report it. The match is now on `pending.as_deref()`, and `pending.take()` happens only in the arm that retains it. Option (a) of the refuter's two; option (b) stays on the R5 ballot. |
| **Q2** | `done.next` for a failed SELF or POP pass | §2 (`RuleDone::next`), §3.3 (`recover_error_pass`), §0.4 R8, §10 item (7) | `done.next` is now resolved from the failed rule's own `next_rule` tag — which is literally what TypeScript's `prev.next` is — so a failed self pass reports the rule itself and a failed close pass reports the resuming parent. The residual `None` sub-cases are listed in §10 item (7) instead of being silent. |
| **Q3** | the changelog files the capability removal in the wrong bucket | §10 | "a callback can no longer write the ancestor stack" moves out of the "toward the canonical engine" paragraph and becomes differs-list item 6, with the citations §0.4 R1 already carries. |
| **Q4** | differs-list item 5 records only the ruleDone-push case | §10 item 5, §7 (`rule.child_rule` row) | Stated generally: a frame's `child_rule` is `None` for the WHOLE lifetime of its live child, where TypeScript's `rule.child` and Go's `r.Child` are the live child object from push until the next push. The migration row now names `context.ancestors()[rule.d + 1]` and the callback's own `rule` as well as `context.pending()`. |
| **Q5** | the `next` row's `self.child()`/`self.parent()` are ambiguous | §4 hop table | They mean the CURSOR's `child`/`parent` hops from the rows above, not the `Rule` accessors. Same edit as **T1**, from the other lens. |
| **Q6** | tag hygiene on the two non-`accepts_close` recovery paths | §3.3, §10 item (3) | Same edit as **T4**, from the other lens. |
| **Q7** | test 11's second case reads 0, not 1 | §3.2, §6 test 11 | The grammar must write `rule.u_mut()` **before** the `bo` `to_snapshot()`, because a rule whose `u` is still the `empty_values()` sentinel is not counted under either keying. The counter also moves to the `!Rc::ptr_eq(&self.u, &empty_values())` form, which additionally catches a map that was written and then emptied. |
| **[n]** | three accuracy notes the refuters raised without requiring | §0.3, §6 gates, §9 | The generalised closure rule (§0.3) is empirically false as stated and is narrowed; the profile gate names `drop_in_place<Rule>`, not `drop_in_place<RuleSnapshot>`; §9's palindrome `u_mut()` line no longer claims the first write stops copying. |

### 0.1c Round r3 required changes, one row each

| id | lens / subject | where it lands | what changed |
|---|---|---|---|
| **U1** | the **before**-open `next` copy moves when the after-phase copy moves | §3.3 (the `:1540` bullet), §10 (the `[S8]` paragraph), §0.2 item 13, §7 (last migration row) | The claim that the before-open `next` argument is "unchanged by this release" is **deleted**. `rs/src/parser.rs:1540` evaluates `let next = is_open.then(\|\| current_rule.snapshot());` **once**, before the binding loop at :1546, so every before-action of the phase sees the rule as it stood before the first of them ran; the lazy replacement takes the copy at the first **bound state action**, so a `bo` `ContextAction` declared before a `bo` state action in `bo_order` now changes what that state action sees in `next`. That is the identical change **[S8]** already records for `ao`/`ac`, and it is a move toward TypeScript (`let next = is_open ? rule : ctx.NORULE`, ts/src/rules.ts:535, passed live at :564). The `AltModifierWithMatch` half of the old sentence is correct and is kept: parser.rs:2089's `then()` sits immediately before the single modifier call at :2092. |
| **U2** | the POP arm must capture the root's result **before** the ruleDone event | §3.3 (the POP arm's `None` branch and its empty-alternatives twin), §6 test 3 | Revision 4 wrote the notify first and `completed_value = Some(current_rule.node.borrow().clone())` second, inverting parser.rs:2607 → :2610 (twin :2698 → :2701). `rule.node` is an `Rc<RefCell<Value>>` that **[S14]** says is still writable through a `&Rule`, so under the inverted order a ruleDone subscriber on the root's final pass could change what `parse()` returns — a parse-result change under §10's freeze. The two statements are swapped back to today's order on both arms. |
| **U3** | the `[T5]` attribute table miscounts `recover_error_pass` | §3.3 (the `[T5]` table) | 11 → **10**, not 11 → 11. Counted from parser.rs:800-811: `&self`, `error`, `state`, `alt`, `fallback_error_token`, `src`, `current_rule`, `stack`, `context`, `lexer`, `mode` = 11 today, 10 after `−stack`. The conclusion is unchanged (10 > 7, so the `#[allow]` at :799 stays), but a table whose purpose is to correct a miscount must not contain one. |
| **U4** | stage A removes `:1743` as well, so its expectation was too low and its gate was disarmed | §8 (Measure A), §3.3 (the per-site table row for `:1743`), §9 (per-stage split) | The "second holder" revision 4 credited to `:1743` — the forward link the retired predecessor stores at :2503 — does not hold the record `:1743` writes: `:2536`'s `deref_mut` on `next` runs while that `Rc`'s count is 2 and **copies**, leaving the predecessor pointing at the old record. That is the same detachment the spec already credits to `:2484` in the push case; the two arms are symmetric, which is also why `:1743` and `:1745` are ~10,240 copies each (they are the two branches of one `if is_open`). Stage A therefore removes `:1743`, `:1745` **and** `:2540` on adder as well as on palindrome. The expectation is restated (~6-7.5M, not ~4.5-5.5M) and the gate is made **two-sided**. |
| **V1** | `done.parent` is `None` on exactly the arms where `done.next` names the resumed parent | §2 (`RuleDone::parent`), §3.3 (`recover_error_pass`), §6 test 6, §10 item (7), §0.4 **R8** | `done_parent` is read from `context.ancestors().get(event_rule.d - 1)` **after** `attempt_recover` has run; on the `!pop_until_valid` arm and in the forced-pop loop the stack is already shorter, so that read is `None` while arm 5 of `done_next` deliberately resolves the same rule through `parent_i`. The event then reported `done.next = <the parent>` and `done.parent = None` for one rule. TypeScript's `prev.parent` is the pusher object always (`next.parent = rule`, rules.ts:665/:666, never reassigned; `bad()`/`attemptRecover` leave it intact, :1183-1193). Fixed by the same `parent_i` normalisation, written as a `match` per §0.3; the residue — the forced-pop loop popping **past** the immediate parent, where neither `done.next` nor `done.parent` can name it — is added to §10 item (7). |
| **V2** | a plain named `Action` after-action cannot reach `context.pending()` | §3.3 (the pending protocol), §6 test 1, §7 (new migration row), §10 differs-list item 2, §0.4 **R9** | §3.3 and §10 item 2 both claimed **every** after-action still reaches the rule that will run next. `Action = Arc<dyn Fn(&mut Rule) + Send + Sync>` (lib.rs:144, registered by `Tabnas::action`, lib.rs:839-846) receives **no `Context`**: `run_action_with_config` (parser.rs:424-446) tries builtins, then `self.actions.get(name)` and calls `action(rule)`, and only then `self.context_actions`. Such an action reads the pending rule today through `rule.child_rule`/`rule.next_rule`, which :2450-2451/:2503 set before the after-actions. **That is a capability removal for one callback kind**; the two false sentences are fixed, the loss is recorded, a migration row points at `action_with_context` (lib.rs:882-888) or a state action, and it is put up for ratification as **R9**. |
| **V3** | the forced close of the rule that failed reports `done.next == None` where TypeScript reports its live link | §2 (`RuleDone::next`), §3.3 (the forced-pop loop), §6 test 6, §10 item (7) | TypeScript fires that synthesized event with the live rule (`ctx.sub.ruleDone.map((s) => s(rule, ctx, done))`, ts/src/rules.ts:1197-1201), so a subscriber reads `prev.next` = the unrun pending rule when the failure was in a push or replace after-action, and the tag's target otherwise. The spec hard-coded `None` although `pending` is in scope on that path. The first forced close now resolves **the pending rule, then the failed rule's own tag**, through one helper; the loop's later events keep the ancestor's own `child()`. The same paragraph now states the forced-close event's `ancestors()` shape, which §2 did not cover. |
| **V4** | "no parse result changes" is contradicted by its own next clause | §10 (opening paragraph) | A declarative condition decides whether an alternate matches, so moving a close-pass `next.state` from the frozen `"o"` to the live `"c"` (test 8) and a parent's `child.*`/`next.*` from the chain tail to the pushed head (test 7) change routing, and therefore the parsed value, for any grammar that declares them. Restated: no parse-result change for any grammar that declares no `next.*`/`child.*` condition — which includes both benchmark grammars and the shipped JSON grammar — and, for grammars that do, Rust moves to the TypeScript/Go value. The `DIVERGENCE.md` argument that follows is unaffected and stays. |
| **[n]** | five accuracy notes the r3 refuters raised without requiring | §2, §7, §9, §10 | (1) `Rule` gains a public `Debug` impl — additive, but it belongs in the changelog beside `Rule: Clone`'s removal, and the `#[derive(Debug)]` on `Context` and on `RuleDone<'a>` does not compile without it. (2) §9's new-cost table gains the pointer indirection `Vec<Box<Rule>>` adds to the stack scans. (3) §7 gains a migration row for `parent_rule` read on a rule reached **through a link**. (4) §10's "toward the canonical engine" list gains the `!pop_until_valid` arm's new child retention, which is an ADD relative to today. (5) §0.4 R8 records that three arms of `done_next`, the `done_parent` normalisation and `ancestors_for` all key on `rule.i`, a `pub` field a plugin can write. |

### 0.2 Where two required changes interact, and how they are reconciled

1. **P1 × S8 — `next` for a state action is now a copy in all three cases.** S8 asked that the changelog restrict the "live next rule" claim to `NextOf::Pending`. P1 moves the pending rule into `Context`, and a `&Rule` borrowed from `context` cannot coexist with the `&mut Context` the same call passes — so `NextOf::Pending` must materialise a copy too, exactly as `Current` and `Parent` do. The reconciliation is that the claim is dropped for **all three** variants and replaced by the true one: **no after-action can mutate the pending rule or an ancestor** (both are reachable only as `&Rule`), so for `Pending` and `Parent` a copy is observationally identical to the live rule; only for `Current` — where `next` is the rule the callback also holds as `&mut Rule` — is the copy observable, and there the copy is taken at the FIRST bound state action of the phase instead of before the first after-action (today, parser.rs:257). That ordering change is recorded in §10. The cost is one `to_snapshot()` per push/replace pass **that binds a lifecycle state action** — zero on both benchmark grammars and zero on the shipped JSON grammar (§9).
2. **P3 × S9 — fix beats document.** S9 asked the changelog to record that `done.next.prev_rule` is `None` during a replace event and `done.parent.child_rule` is `None` during a pop event. P3 removes both by storing the link before the event, which is also what today's code does. The changelog therefore records only what survives: on a **push** event the pusher's `child_rule` is `None` where TypeScript's `prev.child` is the pushed child — structural, because the child is the loop's live rule and cannot also be owned by the pusher — with `done.next` as the replacement.
3. **P2 × S10 — fix beats document, for push.** S10 asked §10 item (3) to record that `child.*` resolves to nothing after a failed after-action that recovery survives. P2 retains the unrun child on that path, so for a **push** both `child.*` and `next.*` resolve to the unrun rule, as TypeScript does. Item (3) shrinks to the **replace** case: there is no owning field for "the replacement that never ran", so `next_rule` is reset to `NoRule` and `next.*` resolves to nothing (`next_rule_name` still names it). See the ratification item R5 below.
4. **P2 × P3 — one ordering, three sites.** The recovery ruleDone (:840) now also reports a link that may already have been stored: if `attempt_recover` retained the pending child, `done.next` is read back from `current_rule.child_rule`; otherwise it is the still-owned local. §3.3 gives the exact shape.
5. **P4 × S12 — head and tail are the same walk.** `Rule::child()` walks `prev_rule` to the head; the walker's `Replaced` resolution walks the same chain forward. S12's case is the terminating condition of that forward walk: when `self.rule` IS the tail, no member has `prev_rule` pointing at it and the answer is `None`.
6. **P5 × S3 — one counter.** Identical requirement from two lenses; implemented once in `u_mut`.
7. **P1 × the "signatures textually unchanged" promise.** The refuter's alternative — passing `NextOf` into `run_context_callback`/`run_action_with_config` — cannot reach a `ContextAction`, whose type (`Fn(&mut Rule, &mut Context) -> Result<(), ActionError>`, lib.rs:145) must stay textually unchanged for the external break to remain five lines. `Context::pending()` delivers the same rule to every after-action kind **that receives a `Context`** — three of the four; the fourth is **[V2]**, §0.2 item 16 — without touching one callback type.

**Round r2.**

8. **T1 × Q5 — one edit, two lenses, and it is the edit the `child` row already made.** Both lenses land on the same cell of §4's hop table: the `next` hop resolving the `Child` tag. The soundness lens says the resolution must go through the live descendant (`Frame{index+1}`, or `Current` when `index+1 == len`, then walked to the chain head) because §3.3 clears `child_rule` at the push; the parity lens says the table's `self.child()` must be read as the CURSOR's `child` hop rather than the `Rule::child()` accessor. Those are the same sentence from two directions, and the reconciliation is to write the `next` row so that every column delegates to the `child`/`parent` rows above it — which already specify the live descendant for a `Frame` — instead of naming an accessor. Cost is zero instructions: it is the code path the `child` hop already runs, on grammars that declare a `child.*`/`next.*` condition, of which there are none in either benchmark or the shipped JSON grammar.
9. **T4 × Q6 — one normalisation, three recovery arms, and one thing that still cannot be owned.** The soundness lens asks that §10's "not a divergence for a PUSH" be restricted to the `accepts_close` arm or that the other two arms be given the same normalisation; the parity lens asks for tag hygiene on those same two arms. Fixing beats documenting where an owner exists, so the normalisation is applied to all three arms: on `!pop_until_valid` (parser.rs:768-770) and inside the forced-pop loop (:784-796) the ABANDONED rule is retained as the resuming rule's `child_rule` exactly as before, and the rule that failed has its `next_rule` tag reset to `NoRule` when that tag named a pending rule nothing can own any more. What cannot be fixed on those two arms is retaining the unrun rule itself: the resuming rule's `child_rule` is already taken by the abandoned rule, so the unrun child of a failed push is reported once as `done.next` and dropped there, as the unrun replacement is. §10 item (3) now says exactly that, and test 6 gains a third case that drives it.
10. **Q1 × Q2 × P2 — one rewrite of `recover_error_pass`.** Q1 says the pending must not be consumed on every arm; Q2 says `done.next` must be TypeScript's `prev.next` for a failed self or pop pass; P2 (round r1) says the unrun rule must be reported as `done.next`. All three are one code block, and the reconciliation is a single resolution order: **pending first, then the failed rule's own `next_rule` tag.** The tag is not a substitute for the pending — it names the pending for exactly the two routing cases — but for every other failed pass the tag IS what TypeScript reads, because TypeScript's `rule.next` is assigned at rules.ts:720 before the after handlers at :728 and `bad()`/`attemptRecover` return the rule with that link intact (rules.ts:1183-1193). Resolving the tag also answers the six `recover_error_pass` call sites that are **not** after-action failures (parser.rs:1618, :2125, :2342, :2410, :2764, :2820), which a threaded `NextOf` — the mechanism the required change suggested — would have had to invent a value for: at those sites the pass failed before routing, and TypeScript likewise reports whatever `rule.next` already held. The `NextOf` parameter is therefore **not** added to `recover_error_pass`/`recover_after_actions`; the requirement is met by the tag, and §0.4 R8 puts the resulting resolution table up for ratification because it defines new public API.
11. **Q7 × P5/S3 — the counter keying does not rescue the test; the write order does.** The required change offers two fixes for test 11's second case. Only one of them works. Keying the counter on `!Rc::ptr_eq(&self.u, &empty_values())` does **not** make an empty-but-shared map count when that map IS the sentinel, because the snapshot holds a clone of the same sentinel `Rc` — `ptr_eq` is true and the write is skipped, exactly as `!is_empty()` skips it. The fix that works is the one the required change names first: the grammar's `bo` action writes `rule.u_mut()` **before** taking the `to_snapshot()`, so the shared map is the rule's own. Both are adopted anyway — the write order because it is necessary, the `ptr_eq` keying because it is strictly more precise than `!is_empty()` (it also counts a map that was written and then emptied, which `is_empty()` would miss) — and §3.2 records why neither alone is sufficient.
12. **T3 × S11 — one migration row survives, the other goes.** S4 (round r1) and S11 (round r1) added two adjacent migration rows about borrowing an ancestor. T3 deletes the first (the E0716 claim is false) and leaves the second untouched: E0502 when a yielded `&Rule` is held across a `&mut` use of the same `Context` is real, is the code the refuter verified, and is the one a plugin author will actually hit.

**Round r3.**

13. **U1 × S8/P6 — one edit, three phases, one changelog paragraph.** [S8] recorded that the after-phase `next` copy moves from "once, before the first after-action" to "at the first bound state action", and [P6] recorded the before phase and the modifier as unchanged. The lazy materialisation is the **same edit** at both sites (§3.3's `:1540` bullet is explicitly "lazy, through the same helper"), so the before phase moves for the same reason and by the same amount. The reconciliation is that [P6]'s sentence splits: the `AltModifierWithMatch` half survives verbatim (parser.rs:2089's `then()` is immediately before the single call at :2092 — nothing can run in between, so nothing can be observed), and the before-open half is deleted and folded into the [S8] paragraph, which now names `bo`/`bc` alongside `ao`/`ac`. The migration table's "unchanged, listed so it is not mistaken for new" row shrinks to the modifier. What is NOT claimed: that either phase reaches TypeScript. TypeScript passes the live rule to every handler; Rust now passes a copy taken later than before. Both phases move the same distance toward it.
14. **V1 × Q2 arm 5 — the normalisation makes `done.next` and `done.parent` the same rule, deliberately.** Arm 5 of `done_next` resolves a failed close pass to the resuming parent, and on the two arms where recovery **popped** to that parent it finds it as `&**current_rule` rather than as a stack frame. [V1] gives `done_parent` the same second chance, so on those arms both fields name the resumed parent and `ptr::eq(done.next.unwrap(), done.parent.unwrap())` holds — which is exactly what test 3's ordinary (non-recovery) pop case already asserts, and what TypeScript gives (`prev.next` is `ctx.rs[--ctx.rsI]` and `prev.parent` is the pusher, the same object). Test 6's third `[Q2]` sub-case gains that assertion. The normalisation is written as a `match`, not `.or_else(\|\| ..)`, for the reason §0.3 gives.
15. **V3 × Q2 × §2 — one resolution order, now used at three event sites.** [Q2] established the order **pending first, then the failed rule's own `next_rule` tag** for the error-pass event. [V3] observes that the forced-close event of the rule that failed is the same question about the same rule, one call earlier, with the same two sources in scope. The reconciliation is to hoist that order into one helper (`Self::forced_next`, §3.3) and call it from the forced close as well, so §2's `RuleDone::next` table describes one rule of resolution rather than two. The loop's later forced closes do **not** consult the pending: they fire on ancestors whose `Child` tag names the child they really pushed, which the loop has just retired into `child_rule`, and reporting the dropped unrun rule for each of them would be wrong in a new way.
16. **V2 × P1 — "every after-action" narrows to "every after-action that receives a `Context`".** [P1]'s mechanism is sound and its window is right; what was wrong is its reach. Three of the four after-action kinds get a `&mut Context` and can call `context.pending()`: `ActionBinding::Callback` (a `ContextAction`), `ActionBinding::State`, and a `Named` binding that resolves through `self.context_actions` (registered by `action_with_context` or `state_action_ref`). The fourth — a `Named` binding that resolves through `self.actions`, the plain `Action` of lib.rs:144 — takes only `&mut Rule` and cannot. Note the resolution order in `run_action_with_config` (parser.rs:424-446): builtins, then `actions`, then `context_actions`, so a name registered both ways resolves to the context-less one. §3.3, §10 item 2 and test 1 are corrected to the narrower claim, §7 gains the migration row, and §0.4 **R9** puts the removal itself up for ratification rather than deciding it here.
17. **U4 × §9 — the gross saving is unchanged; only the stage boundary moves.** [U4] does not add or remove a single copy from the totals: `:1743`'s 1.84M was always counted in the gross ~14.8-15.3M and in the committed 11-12M. What changes is which stage collects it, so §8's Measure A expectation rises (~6-7.5M on adder) and §9's per-stage line moves the same amount out of stage B. The committed claim, the arithmetic and the ≥11M sanity floor are untouched.
18. **U2 × P3 — two orderings, no conflict.** [P3] requires the completed rule to be **stored into its holder's link before** the ruleDone event fires, on the replace and pop arms. [U2] requires the root's **result value to be read before** the ruleDone event fires, on the pop arm's `None` branch. They are different statements about different arms of the same `match` (the `None` branch stores nothing — there is no holder left), and both restore an order today's code already has (:2536/:2602 precede :2610; :2607 precedes :2610). Neither edit touches the other.

### 0.3 The compile rule behind the two rewritten sites **[S1, S2, n]**

Revision 3 stated this as a general law: *"a closure that captures a `&mut` and returns a reference derived from it is rejected, because the closure kind is inferred from the upvar use, not from the `FnOnce` bound"*, and concluded that **every** `owned.get_or_insert_with(..)` must be called outside any closure capturing `owned`. **That generalisation is empirically false** and is withdrawn **[n]**: the r2 soundness refuter compiled both `is_open.then(|| &*owned.get_or_insert_with(|| rule.to_snapshot()))` and `is_open.then(|| materialise_next(owned, rule)).flatten()` under rustc 1.94.1 / edition 2021, and both are accepted — `bool::then` takes `FnOnce`, the closure infers `FnOnce`, the `&'o mut` upvar is moved in, and the derived reborrow escapes legally.

What is true, and all that [S1] and [S2] ever established, is narrower: **the two specific shapes revision 2 wrote were rejected**, for reasons local to those shapes (an inference that landed on `FnMut` because the same upvar was used twice in the surrounding expression). The forms this spec now uses — a `match` in `materialise_next` (§3.3) and a plain `if` at the :1540 site (§3.3) — compile, and the closure forms would very likely compile too. **The rule to carry into review is therefore: these two sites are written as `match`/`if` deliberately, and a reviewer must not "simplify" them back to `.map(..)`/`.then(..)` without compiling** — not that the closure form is illegal. Closures that capture only shared references (for example `context.rs.len().checked_sub(2).map(|k| &*context.rs[k])`) were never in question and are left as written.

### 0.4 MAINTAINER RATIFICATION REQUIRED

These are contract decisions, not implementation choices. They are listed here and repeated in §8 Step 0; the branch is not opened until each has a yes or a different answer.

- **R1 — `introspection_test.rs:495-560` can only be re-expressed.** The test writes `context.rule_stack.reverse()` from a callback. The stack is the engine's own data in this design, so the write cannot exist. Re-expressed as `a_callback_mutating_everything_it_can_reach_does_not_derail_the_parse` (§6), which asserts strictly more about the behaviour that matters; the "cannot write" half becomes two `compile_fail` doctests. **This removes a capability** (a callback writing the published stack) that TypeScript and Go both allow on `ctx.rs`/`ctx.RS`.
- **R2 — `api_test.rs:311-348` tests a mechanism that ceases to exist.** Copy-on-write of `Rule::snapshot()`. Re-expressed as `a_rule_is_one_object_for_its_whole_life` plus the layout invariants of test 12 (§6).
- **R3 — ruleDone ordering.** A ruleDone subscriber sees the ancestor stack AFTER the transition (TypeScript's `ctx.rs[0..rsI]`), with `done.next`/`done.parent` carrying the moved rules. Today's Rust gave the pre-transition stack on push and pop. Pinned by test 3.
- **R4 — after-action stack order stays Rust's.** Inside `ao` of a pushing pass and `ac` of a popping pass, `context.ancestors()` is the PRE-transition stack and `depth() == rule.d`; TypeScript and Go have already moved `ctx.rs`. Reasons in §10 item (1). Pinned by tests 1 and 5.
- **R5 — the unrun rule after a failed after-action (restated for r2, **[T4, Q1, Q6]**).** **P2** retains the unrun **child** of a failed push — **but only on the `accepts_close` recovery arm** (parser.rs:775-782), which is the only arm where the failed rule stays current and its `child_rule` is free. Revision 3 asserted this generally and was wrong. The full picture, and what needs ratifying:
  - **`accepts_close` arm, failed PUSH:** the unrun child is retained as `current_rule.child_rule`, tag stays `Child`, so `child.*` and `next.*` both resolve to it, as TypeScript does. **Not a divergence.**
  - **`accepts_close` arm, failed REPLACE:** nothing can own the unrun replacement (the rule stays current; `prev_rule` belongs to a rule that completed). The tag is reset to `NoRule`, `next.*` resolves to nothing, `next_rule_name` still names it, and the error-pass ruleDone reports it once as `done.next`. **Divergence**, §10 item (3).
  - **`!pop_until_valid` arm (parser.rs:768-770) and the forced-pop loop (:784-796), failed PUSH *or* REPLACE:** the resuming rule's `child_rule` is taken by the ABANDONED rule, so the unrun rule cannot be retained either. Its tag is reset to `NoRule` on the abandoned rule before it is retired, so `parent.child.next.*` cannot name a rule that no longer exists. The unrun rule is reported once as `done.next` and dropped. **Divergence**, §10 item (3), pinned by test 6's third case.

  Options for the two divergent cases: (a) accept them, recorded in §10 item (3) — what this spec assumes; (b) add a fifth owning link (`unrun: Option<Box<Rule>>`, cleared on the next pass) to carry the unrun rule on every arm. (b) costs a field and a retention rule for a path that only runs under recovery, and would close all three divergent cases at once.
- **R6 — `Context::pending()` is new public API (new this round).** It is how **P1** keeps the pending rule reachable. It is `Some` ONLY inside an after-action of a pass that pushes or replaces, and `None` everywhere else, including inside the recovery that follows a failed after-action. A narrower accessor (`pending_name()`) or a wider one (making it `Some` for the whole pass) are the alternatives.
- **R7 — `child`/`next` after a replaced child popped move to TypeScript's head-of-chain (**P4**).** Today's Rust resolves them to the last replacement. Test 7 states every value per port and requires the TypeScript fixture to be run before the pin lands.
- **R8 — the `done.next` resolution table on an error-pass event (new in r2, **[Q2]**).** `RuleDone::next` is new public API, and on the error pass (`recover_error_pass`) there is no single obviously-right value. This spec resolves it as **the pending rule if one is still reachable, otherwise the failed rule's own `next_rule` tag** — which is literally what a TypeScript ruleDone subscriber reads as `prev.next` (assigned at rules.ts:720 before the afters at :728, left intact by `bad()`/`attemptRecover`, rules.ts:1183-1193). The full table is in §2 (`RuleDone::next`) and §3.3. Consequences the maintainer is ratifying: a failed **self** pass reports **the rule itself**, by pointer-identity with the `rule` argument the subscriber receives (both are `&event_rule`, the link-free recovery copy of §10 item 4); a failed **close** pass reports **the resuming parent**, whether recovery left it on the stack or popped to it; and three sub-cases report `None` where TypeScript would still name a rule (§10 item 7). The alternative the refuter offered is to resolve nothing and record `done.next == None` for every failed self/pop pass as a divergence; that is one line of code less and one more divergence.

  **Extended in r3 (**[V1]**, **[V3]**, **[n]**). Three more consequences ride on the same table and are ratified with it:**
  - **`done.parent` on the two arms where recovery popped to the parent [V1].** `done_parent` is read from the stack *after* `attempt_recover` has run, so on the `!pop_until_valid` arm and in the forced-pop loop the frame at `d - 1` is gone and the read is `None` — while arm 5 of `done_next` resolves that very rule through `parent_i` and reports it as `done.next`. This spec applies the same normalisation to `done_parent` (§3.3), so the two fields name the same rule and match TypeScript, where `prev.parent` is the pusher object always (rules.ts:665-666, never reassigned; `bad()`/`attemptRecover` leave it intact, :1183-1193). The alternative is to leave the code and amend the `RuleDone::parent` doc to say `None` there; that is one line less and one more divergence, and it makes the doc's "the resuming rule on a pop" false on the recovery path.
  - **The forced close of the rule that failed [V3].** Its `done.next` is now resolved by the same order (pending, then tag) instead of being hard-coded `None`, which is what a TypeScript subscriber reads there (`s(rule, ctx, done)` with the rule's links live, rules.ts:1197-1201). The alternative is `None` plus a fourth entry in §10 item (7).
  - **The resolution keys on `rule.i`, which a plugin can write [n].** Arms 2, 3 and 5 of `done_next`, the `done_parent` normalisation and `ancestors_for` all compare `i` fields. That is pre-existing practice in the engine (context.rs:86, parser.rs:559), but this is the first time a plugin-writable `pub` field decides the value of a **new public API field**. A grammar that rewrites `rule.i` mid-parse can make `done.next`/`done.parent` name the wrong rule or nothing; it cannot make them dangle (every candidate is a rule the engine still owns). Recorded rather than defended: the alternative is a private monotonic id, which is a larger change than this PR.

- **R9 — a named plain `Action` after-action loses the pending rule (new in r3, **[V2]**). CAPABILITY REMOVAL.** `Action = Arc<dyn Fn(&mut Rule) + Send + Sync>` (lib.rs:144, registered by `Tabnas::action`, lib.rs:839-846) receives no `Context`, so `context.pending()` — the mechanism **P1** uses to keep the pending rule reachable — is unreachable from it. Today such an action reads the rule that will run next through `rule.child_rule` / `rule.next_rule`, which parser.rs:2450-2451 (push) and :2503 (replace) set before the after-actions at :2452/:2504. After this PR `child_rule` is `None` at a push and `next_rule` is a one-byte tag, so for this one callback kind the capability is gone. Nothing in the tree catches it: `rs/tests/api_test.rs:108-114` registers exactly this shape in `ao` but reads only `next_rule_name`, which survives. Options: **(a)** accept it, record it in §10 item (2), and tell plugin authors to re-register the action with `action_with_context` (lib.rs:882-888) or as a state action — what this spec assumes, and the migration is one line at the registration site with no change to the action body beyond the extra parameter and the `Result` return; **(b)** widen `Action` to `Fn(&mut Rule, &Context)`, which changes a public callback type and takes the external break past its five one-line edits; **(c)** keep a push-time copy in `child_rule` for this callback kind alone, which reinstates the second holder the whole design exists to remove. Like **R1**, this removes something the three ports allow, so it is the maintainer's call, not the implementer's.

---

### 0.5 MAINTAINER DECISIONS (2026-09-18)

Four of the nine §0.4 items were put to the maintainer and answered. **R2,
R3, R4, R6 and R7 were not asked in this round and remain unratified.**

- **R1 — NOT accepted as a removal.** The instruction is to find a way to
  keep the capability in Rust that is not as damaging. Direction taken:
  publish a PERMUTATION rather than the stack. `Context` carries
  `view: Option<Vec<usize>>`, indices into `rs`, materialised on first use
  in a pass; a callback may permute, truncate or reorder the view exactly
  as it writes `rule_stack` today, and reads resolve index to `&rs[i]`, a
  live rule. A callback that never asks pays nothing, which is strictly
  cheaper than today (one `Rc<RuleSnapshot>` per push whether or not
  anyone looks); a callback that does pays one `usize` per frame.
  Carried-forward semantics survive, and `rule_stack_shadow` goes away
  because the engine never consults the view. **One narrowing to record in
  §10:** a callback can permute and drop frames but can no longer push a
  synthesised entry, which the `Vec<Rc<RuleSnapshot>>` allowed and the
  engine ignored.
- **R5 — option (b).** Add the fifth owning link,
  `unrun: Option<Box<Rule>>`, cleared on the next pass, carrying the unrun
  rule on every arm and closing all three divergent cases at once. Open
  items 2 and 3 exist only to document the loss and dissolve with it.
- **R8 — the spec's resolution order stands.** Pending first, then the
  failed rule's own `next_rule` tag, carrying the three r3 extensions: the
  `done.parent` normalisation, the forced-close event, and the fact that
  resolution keys on the plugin-writable `rule.i`.
- **R9 — NOT accepted as a removal.** A Rust-only API change is
  authorised; the bar is perf and memory, not API churn. Direction taken:
  option (b), `Action = Arc<dyn Fn(&mut Rule, &Context) + Send + Sync>`.
  One extra pointer argument at a `dyn` call, no memory on `Rule` or
  `Context`, no hot-path structural change. The spec had rejected this
  only because it takes the external break past its five one-line edits,
  which is the objection the maintainer has overruled. Option (c) stays
  rejected on its merits: it reinstates the second holder the design
  exists to remove.

Neither R1 nor R9 is specified below yet. §2, §3.3, §6, §7 and §10 still
carry the revision-5 text that assumed both removals, and the twelve
editorial items in `rust-callback-phase2-open-items.md` are still open.
The branch is not opened until those are folded in and the refutation
cycle has been re-run against them.

---

## 1. The chosen design, and why it won

**Design 1, "Split the borrow", with the judges' grafts.** The copy a callback pays today is not the price of `&mut Rule` — it is the price of a *second handle* to the same rule: `context.rule` (published by `Context::set_rule`, context.rs:360, from `set_active` at parser.rs:1428 and 16 mid-step `set_rule` calls) and the four `Option<Rc<RuleSnapshot>>` link fields (rule.rs:1258-1261). `Rule::deref_mut` = `Rc::make_mut` (rule.rs:1241) copies whenever either kind of handle is alive, and the same `Rc` is why #177's aliasing attempt leaked (`next_rule = self`, parent↔child). The design removes the second handle rather than freezing it: `Context` loses `rule`, `rule_stack` and `rule_stack_shadow`; the engine's own ancestor stack (the loop local `stack: Vec<Rule>`, parser.rs:1407) moves into `Context` as `pub(crate) rs: Vec<Box<Rule>>` — TS `ctx.rs`, Go `ctx.RS` — and is handed out only as a read-only borrowed view `context.ancestors()`. The current rule is the `&mut Rule` argument and nothing else; it is never in `rs`, so `&mut Rule` and `&mut Context` are disjoint and the borrow checker enforces the contract the copy used to buy. `Rule` holds its state record inline (`data: RuleSnapshot`, no `Rc`), so `DerefMut` is a field projection and cannot copy. Links become an owned `Box<Rule>` of a rule that has *completed* (`child_rule`, `prev_rule`) or a one-byte tag resolved against the live stack (`next_rule: NextRule`); `parent_rule` disappears because the parent *is* `context.parent()`. A `Box` ownership graph is a tree by construction: the #177 cycles are unrepresentable, there is no `Weak`, no `upgrade()`, no `RefCell` on rule state, no `unsafe`.

The one rule that is neither the callback's own nor an ancestor — the rule about to be pushed or to replace the current one — is parked on the `Context` for exactly the window in which the canonical engines expose it (`rule.child`/`rule.next` during the after-actions): `context.pending()` **[P1]**. It is borrowed, never copied, and read-only, so it does not reintroduce a second *writable* handle.

It won on every axis the three judges scored (49/53/49 against 41/43/40 for RuleMut, 34/33/28 for Edits, 34/42/32 for Weak): all seven `(&mut Rule, &mut Context)` callback types keep their signatures textually, so the external break is five one-line edits in three repos; it is pure safe Rust, and **its panic surface is unchanged rather than absent** **[R7]**: `impl Index<usize> for Ancestors` panics out of range exactly as today's `context.rule_stack[i]` did, and every callback still runs under `catch_unwind` into an `internal` error (no_panic_test.rs pins that for the other callback kinds); and it is the only design that removes *both* the publish-caused and the link-caused copies without leaving a residual per-write check or adding an upgrade to any traversal — which is why its expected saving sits at the top of, and may exceed, the verified 11-12M bound (§9). Grafts taken: `BudgetCheck` widened to `Fn(&Rule, &Context)` now (§2); ruleDone `ancestors()` ordering decided by comparison with TS and pinned (§3.3, §6); the declarative `child` hop and the `Child` tag resolve to the **head** of a completed replacement chain, as TS and Go do, and `Rule::child()` names it **[R1, R10, P4]**, while a child that is still LIVE resolves through the live descendant on the stack rather than through that accessor **[T1, Q5]** (§4); a ruleDone subscriber receives the transition target and the completed rule's parent live, through `RuleDone` **[R13]** (§2, §3.3); `Rule::set_node` (§2); the link-walk, identity, lex-path, standalone-lexer, recovery and retention tests (§6); `Rule::detached()` for parser.rs:817 (§3.3); the **lazy** `next` materialisation for bound state actions **[R2]** (§3.3); two green landing stages with a measurement after each (§8); miri once, `md5sum`, cache/branch sim and the RSS gate (§8).

---

## 2. New public types and signatures (verbatim Rust)

### `rs/src/context.rs`

```rust
/// Mutable state for one parse run.
#[derive(Debug)]
pub struct Context {
    pub iteration: usize,
    pub source: String,
    pub meta: Value,
    pub u: IndexMap<String, Value>,
    pub errs: Vec<TabnasError>,
    pub options: Arc<Options>,
    pub instance: InstanceInfo,
    /// The live ancestor stack, root first: TS `ctx.rs`, Go `ctx.RS`. The
    /// rule being processed is never in it — it is the `&mut Rule` the
    /// callback holds — so a callback's `rule` and `context` are disjoint.
    /// Read through [`Context::ancestors`]; the engine is the only writer.
    pub(crate) rs: Vec<Box<Rule>>,
    /// The rule the engine is about to run, parked here for the duration of
    /// a pushing or replacing pass's after-actions — the window in which
    /// TypeScript's `rule.child`/`rule.next` name it (ts/src/rules.ts:665,
    /// :720, both before the afters at :728). It is moved in and moved back
    /// out; nothing copies it, and no callback can reach it as `&mut`.
    /// `None` at every other moment of the parse. Read through
    /// [`Context::pending`].
    pub(crate) pending: Option<Box<Rule>>,
    pub v: VecDeque<Token>,
    pub v_abs: usize,
    pub t: Vec<Token>,
    replay: VecDeque<Token>,
    history_limit: Option<usize>,
    root: Option<Rc<RefCell<Value>>>,
    pub(crate) recover_at: Option<usize>,
    pub(crate) recover_si: Option<usize>,
    pub(crate) bad_to: Option<usize>,
    pub(crate) bad_error: Option<usize>,
}

impl Context {
    /// The live ancestor stack, innermost last. It is the engine's own
    /// stack, borrowed: what an ancestor's `u` reads is what that ancestor
    /// holds now.
    ///
    /// No `&mut Rule` is reachable through it, so an ancestor's own fields
    /// cannot be written from here. That is not a promise of immutability:
    /// a frame's `node` is an `Rc<RefCell<Value>>` and `borrow_mut()` on it
    /// still writes the shared cell, exactly as `rule_stack[i].node` did and
    /// as TypeScript allows. **[S14]**
    ///
    /// What it contains depends on which callback holds it:
    ///
    /// * In a callback that receives `&mut Rule` — alternate conditions,
    ///   routes, errors and modifiers, actions of every kind, rule and lex
    ///   subscribers, imperative matchers and text modifiers, lazy token
    ///   values — and in the budget check, it is the stack the rule sits
    ///   on: `len() == rule.d` on every engine path, including recovery,
    ///   and including the after-actions of a pass that pushes, replaces
    ///   or pops (the stack moves after them; see `RuleDoneSubscriber`
    ///   for why that is not the TypeScript order).
    /// * In a `RuleDoneSubscriber` it is the stack after the transition,
    ///   as TypeScript's `ctx.rs[0..rsI]` is at parser.ts:285: after a
    ///   push the completed rule is `ancestors().last()` and
    ///   `len() == rule.d + 1`; after a pop the parent is gone and
    ///   `len() == rule.d - 1`; after a replace or an open pass with no
    ///   route `len() == rule.d`. `done.next` and `done.parent` carry the
    ///   rules the transition moved.
    /// * In a `RuleDoneSubscriber` reached from a **forced close**
    ///   (`done.forced`, synthesized by recovery at parser.rs:601) it is
    ///   again the stack the rule sits on, `len() == rule.d`, for both
    ///   kinds of forced-close event: the first fires on the rule that
    ///   failed, before any frame has been popped, and each later one
    ///   fires on an ancestor the forced-pop loop has ALREADY popped
    ///   (`context.rs.pop()` precedes the call, exactly as TypeScript's
    ///   `ctx.rs[--ctx.rsI]` precedes `s(r, ctx, done)`, rules.ts:1203-1211).
    ///   `done.parent` is `ancestors().last()` and `done.next` is resolved
    ///   as [`RuleDone::next`] describes. **[V3]**
    ///
    /// The error codes are part of the doctest, not decoration: an
    /// unannotated `compile_fail` passes on ANY compile error, so a renamed
    /// method or a changed signature would keep these two green while the
    /// property they exist to prove had stopped holding — and these two are
    /// the entire replacement for what `introspection_test.rs:495-560`
    /// loses (§0.4 R1). Both codes were verified against the shapes below.
    /// **[T6]**
    ///
    /// ```compile_fail,E0502
    /// fn f(context: &mut tabnas::Context) {
    ///     let ancestors = context.ancestors();
    ///     context.u.insert("k".into(), tabnas::Value::Null); // E0502
    ///     let _ = ancestors.len();
    /// }
    /// ```
    /// ```compile_fail,E0596
    /// fn g(context: &mut tabnas::Context) {
    ///     context.ancestors()[0].u_mut(); // E0596: not writable through the view
    /// }
    /// ```
    pub fn ancestors(&self) -> Ancestors<'_> { Ancestors(&self.rs) }

    /// `ancestors().last()`. In a callback that receives `&mut Rule` it is
    /// the rule that pushed the one the callback holds (`None` for the
    /// root). In a `RuleDoneSubscriber` it is the top of the
    /// post-transition stack — the completed rule itself after a push, the
    /// grandparent after a pop — so a ruleDone subscriber that wants the
    /// completed rule's parent reads `done.parent`.
    pub fn parent(&self) -> Option<&Rule> { self.rs.last().map(|rule| &**rule) }

    /// The rule the engine will run after this pass: the child about to be
    /// pushed, or the rule about to replace the current one. **[P1]**
    ///
    /// `Some` ONLY inside an after-action of a pass that pushes or replaces
    /// — which is the window in which TypeScript exposes the same rule as
    /// `rule.child` (ts/src/rules.ts:665) and `rule.next` (:720), before it
    /// hands it to every after handler as the `next` argument (:728). It is
    /// `None` in every other callback, including a `RuleDoneSubscriber`
    /// (which reads `done.next`) and including the recovery that follows a
    /// failed after-action (the pending rule has been taken back by then;
    /// the recovery event reports it as `done.next` instead).
    ///
    /// Read-only: the pending rule has not run, and nothing may write to it
    /// before it does.
    pub fn pending(&self) -> Option<&Rule> { self.pending.as_deref() }

    /// `ancestors().len()`: equal to the held rule's `d` in every callback
    /// that receives `&mut Rule`; `d + 1`, `d` or `d - 1` in a
    /// `RuleDoneSubscriber` after a push, replace/self or pop respectively.
    pub fn depth(&self) -> usize { self.rs.len() }
}
```
**[R6, R16]** The `parent()`/`depth()` doc comments above replace the revision-1 claims ("the rule that pushed the one the callback holds", "equals the current rule's `d` on every engine path"), which were false inside a `RuleDoneSubscriber` by the spec's own test 3. The ruleDone case is also why `ancestors_for` (§3.3) stays for error attachment.

```rust
/// A read-only view of the live ancestor stack. `Copy`; lives no longer
/// than the shared borrow of the `Context` it came from.
#[derive(Clone, Copy, Debug)]
pub struct Ancestors<'a>(&'a [Box<Rule>]);

impl<'a> Ancestors<'a> {
    pub fn len(self) -> usize;
    pub fn is_empty(self) -> bool;
    pub fn get(self, index: usize) -> Option<&'a Rule>;
    pub fn first(self) -> Option<&'a Rule>;
    pub fn last(self) -> Option<&'a Rule>;
    pub fn iter(self) -> AncestorsIter<'a>;
    /// The view without its innermost frame. Used by the engine to strip a
    /// rule that is both the error site and the top of the stack
    /// (`Parser::ancestors_for`, parser.rs:558); `Ancestors` owns its slice
    /// field privately, so the slicing has to live here. **[S5]**
    pub(crate) fn without_last(self) -> Ancestors<'a> {
        Ancestors(self.0.split_last().map_or(&[][..], |(_, rest)| rest))
    }
}
impl<'a> IntoIterator for Ancestors<'a> { type Item = &'a Rule; type IntoIter = AncestorsIter<'a>; }
/// Panics when `index >= len()`, exactly as `context.rule_stack[index]`
/// did; inside a callback the panic is caught into an `internal` error.
/// Use `get` for a fallible read.
///
/// `Ancestors` is returned BY VALUE and `Index::index` borrows it, but that
/// costs the caller nothing at the usual site: `let frame =
/// &context.ancestors()[i];` **compiles**, because temporary lifetime
/// extension covers the operand of an index expression in a `let`
/// initializer, and `frame` stays usable for the rest of the block exactly
/// as `&context.rule_stack[i]` did. **[T3]** The two restrictions that are
/// real: returning that reference out of a function is **E0515** (the
/// extended temporary is still a local), and holding it across a `&mut`
/// use of the same `Context` is **E0502** — which is the borrow the design
/// intends and which `context.rule_stack[i]` did not have, because it
/// yielded an `Rc` clone. `get(i)`/`first()`/`last()` take `self` by value
/// and return `&'a Rule`, so they have neither restriction.
impl std::ops::Index<usize> for Ancestors<'_> { type Output = Rule; }

pub struct AncestorsIter<'a>(std::slice::Iter<'a, Box<Rule>>);
impl<'a> Iterator for AncestorsIter<'a> { type Item = &'a Rule; }
impl DoubleEndedIterator for AncestorsIter<'_> {}
impl ExactSizeIterator for AncestorsIter<'_> {}
```

**Removed from `Context`:** `pub rule`, `pub rule_stack`, `rule_stack_shadow` (:183-191, init :228-231), `set_active` (:307-310), `sync_rule_stack` (:312-358), `set_rule` (:360-362), `follow_stack` (:67-73), `same_rule`/`same_values`/`same_tokens`/`same_link` (:75-155), the `RuleSnapshot` import (:5).

### `rs/src/rule.rs`

```rust
/// A rule as the parse loop sees it. Its state record is inline: there is
/// no shared handle to a live rule anywhere in the engine, so a write is a
/// write. Reads and writes go through `Deref`/`DerefMut`, so `rule.state`
/// and `rule.state = ..` are unchanged at every call site.
pub struct Rule {                       // NOT Clone; Debug by hand (below)
    data: RuleSnapshot,
    pub parent_node: Option<Rc<RefCell<Value>>>,
    pub child_node: Value,
    /// The completed child, moved in at pop and cleared at push. The box
    /// holds the LAST rule of the child's replacement chain — the one that
    /// popped — and its `prev_rule` chain walks back to the rule this one
    /// pushed, which is the child in the TypeScript and Go sense (`rule.child`
    /// is assigned once, at push, ts/src/rules.ts:665, go/rule.go:1266, and
    /// never relinked at pop, rules.ts:713, rule.go:1312-1318). Use
    /// [`Rule::child`] for that rule; the declarative `child` hop and the
    /// `Child` tag resolve to it. `None` while a child is running: the live
    /// child is then `ancestors()[d+1]` or the callback's own `rule`, and
    /// during the pushing pass's after-actions it is `context.pending()`.
    pub child_rule: Option<Box<Rule>>,
    /// The completed rule this one replaced (TS `rule.prev`, rules.ts:693).
    pub prev_rule: Option<Box<Rule>>,
    /// Which rule runs after this pass, resolved against the live stack.
    pub next_rule: NextRule,
    pub(crate) skip_befores: bool,
    pub(crate) child_node_is_self: bool,
}

/// TS `rule.next` (ts/src/rules.ts:720) as a tag instead of a pointer.
/// `Same`, `Child` and `Parent` name rules the engine owns elsewhere (the
/// rule itself; `child()`, or the head of the live child's chain;
/// `ancestors().last()`); `Replaced` names the rule that now holds this one
/// as `prev_rule`. A tag cannot own, so no cycle can be written through it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum NextRule {
    #[default]
    NoRule,
    Same,
    Child,
    Replaced,
    Parent,
}

impl std::ops::Deref for Rule { type Target = RuleSnapshot; fn deref(&self) -> &RuleSnapshot { &self.data } }
impl std::ops::DerefMut for Rule { fn deref_mut(&mut self) -> &mut RuleSnapshot { &mut self.data } }

#[derive(Debug, Clone)]
pub struct RuleSnapshot {
    pub i: usize,
    pub d: usize,
    pub name: RuleName,
    pub spec: Arc<RuleSpec>,
    pub state: RuleState,
    pub bo: bool, pub ao: bool, pub bc: bool, pub ac: bool,
    pub need: i32,
    pub node: Rc<RefCell<Value>>,
    // parent_rule / child_rule / prev_rule / next_rule REMOVED (were :1258-1261)
    pub next_rule_name: Option<RuleName>,
    pub n: Rc<HashMap<String, i32>>,
    pub u: Rc<HashMap<String, Value>>,
    pub k: Rc<HashMap<String, Value>>,
    pub o: Rc<Vec<Token>>,
    pub c: Rc<Vec<Token>>,
}

/// What a ruleDone subscriber receives about the pass that just completed.
/// TypeScript hands the subscriber the completed rule with its links live
/// (`sub(prev, ctx, done)`, parser.ts:285: `prev.next`, `prev.parent`);
/// the links here are the same rules, borrowed from the engine for the
/// duration of the call.
#[derive(Debug, Clone)]
pub struct RuleDone<'a> {
    /// Rule state before the completed pass.
    pub state: RuleState,
    /// `None` only when that state declared no alternatives.
    pub alt: Option<RuleDoneAlt>,
    /// True only for a close synthesized by recovery.
    pub forced: bool,
    /// The rule the engine runs next — TS `prev.next`: the pushed child,
    /// the replacement, the rule itself (open pass with no route) or the
    /// parent that resumes (pop). `None` for the root's final pass.
    ///
    /// On the **error pass** of a FAILED pass (`recover_error_pass`) it is
    /// resolved in this order — pending first, then the failed rule's own
    /// `next_rule` tag, which is literally what TypeScript's `prev.next`
    /// holds there (assigned at rules.ts:720 before the after handlers at
    /// :728, and left intact by `bad()`/`attemptRecover`, rules.ts:1183-1193).
    /// **[P2, Q1, Q2]** Ratification item §0.4 **R8**.
    ///
    /// | the failed pass | `done.next` | same as TypeScript? |
    /// |---|---|---|
    /// | pushed or replaced, unrun rule still owned by the error pass | the unrun rule | yes |
    /// | pushed, unrun child retained by recovery (`accepts_close`) | that child, through `current_rule.child()` | yes |
    /// | routed nowhere on an open pass (tag `Same`) | the rule itself, by pointer identity with the `rule` argument | yes |
    /// | closed with no route (tag `Parent`) | the resuming parent — the frame at `d - 1`, or the rule recovery popped to | yes |
    /// | tag `Child` and the child had already completed | that child, when recovery kept the rule current; else `None` | partly, §10 item (7) |
    /// | tag `Replaced`, or the unrun rule was dropped by recovery | `None` | no, §10 items (3) and (7) |
    /// | tag `NoRule` (the pass failed before the rule ever routed) | `None` | yes — TS reads `NORULE` there |
    ///
    /// On a **forced close** (`forced == true`) the same order applies, and
    /// it applies to the rule that FAILED as well — not just to the
    /// resumed ancestors. **[V3]** TypeScript fires that synthesized event
    /// with the live rule and its links intact
    /// (`ctx.sub.ruleDone.map((s) => s(rule, ctx, done))`, ts/src/rules.ts:1197-1201),
    /// so a subscriber there reads `prev.next` = the unrun pending rule
    /// when the failure was in a push or replace after-action, and the
    /// tag's target otherwise:
    ///
    /// | the forced-closed rule | `done.next` |
    /// |---|---|
    /// | the rule that failed, and the failed pass had parked a pending rule | that unrun rule |
    /// | the rule that failed, tag `Same` / `Child` / `Parent` | itself / its completed `child()` / `ancestors().last()` |
    /// | an ancestor the forced-pop loop retires (tag `Child`) | that ancestor's `child()` — the **head** of the chain it pushed, not the tail **[P4]** |
    /// | tag `NoRule`, or tag `Replaced` with nothing left to name | `None` |
    ///
    /// The loop's later events deliberately do NOT consult the pending
    /// rule: they fire on ancestors whose `Child` tag names the child they
    /// really pushed, which the loop has just retired into `child_rule`.
    pub next: Option<&'a Rule>,
    /// The completed rule's parent — TS `prev.parent`, which is the pusher
    /// object for the whole life of the rule (`next.parent = rule`,
    /// ts/src/rules.ts:665-666; never reassigned, and `bad()`/`attemptRecover`
    /// leave it intact, :1183-1193).
    ///
    /// It is the frame at `d - 1` while that frame is on the stack, and the
    /// resuming rule itself once a pop — ordinary or forced by recovery —
    /// has taken that frame off. On the error pass the second case is
    /// reached through the same `parent_i` capture that resolves
    /// `done.next` (§3.3), so a failed close pass that recovery popped out
    /// of reports the same rule in both fields. **[V1]** Without that
    /// normalisation the specified code could not produce the value this
    /// doc promises: `context.ancestors().get(d - 1)` is read AFTER
    /// `attempt_recover` has popped, and would be `None` on exactly the
    /// arms where `done.next` names the parent.
    ///
    /// `None` for the root, and on one residual path: when the forced-pop
    /// loop popped PAST the immediate parent, nothing the event can reach
    /// is that parent any more, so `parent` and `next` are both `None`
    /// where TypeScript still names it (§10 item (7)).
    pub parent: Option<&'a Rule>,
}
/// `state`, `alt` and `forced` by value; `next` and `parent` by identity.
impl PartialEq for RuleDone<'_> { /* ptr::eq on the two links */ }
```
**[R13]** `RuleDone` gains the two links as borrowed fields rather than as extra subscriber arguments so the debug plugin's closure (`|rule, context, done|`, trace.rs:211) and every test closure stay textually unchanged; no test constructs a `RuleDone` literal or compares one (grep of `rs/tests`, json, debug, directive: none), so the derived `PartialEq` becoming a manual identity-based one breaks nothing.

```rust
impl Rule {
    pub fn new(name: impl Into<RuleName>, initial_node: Value) -> Self;            // unchanged signature
    pub fn with_shared_node(name: impl Into<RuleName>, node: Rc<RefCell<Value>>) -> Self; // unchanged
    pub(crate) fn bound(name: RuleName, node: Rc<RefCell<Value>>, installed: Option<&Arc<RuleSpec>>) -> Self; // unchanged
    pub fn n_mut(&mut self) -> &mut HashMap<String, i32>;   // unchanged
    pub fn u_mut(&mut self) -> &mut HashMap<String, Value>; // unchanged (+ debug counter, §3.2/§6)
    pub fn k_mut(&mut self) -> &mut HashMap<String, Value>; // unchanged
    pub fn resolve_open_value(&mut self, index: usize, context: &mut Context) -> Value;  // unchanged
    pub fn resolve_close_value(&mut self, index: usize, context: &mut Context) -> Value; // unchanged

    /// The rule this one pushed: TypeScript `rule.child` (ts/src/rules.ts:665),
    /// Go `r.Child` (go/rule.go:1266), assigned once at the push and never
    /// relinked when the child pops or replaces itself. **[P4]**
    ///
    /// `child_rule` holds the LAST rule of that child's replacement chain
    /// (the one that popped), so this walks `prev_rule` back to the head.
    /// The walk is an iterative loop, O(chain length), and runs only where a
    /// caller asks for the child — `done.next` on a forced close, the
    /// recovery reader, and the declarative `child` hop and `Child` tag
    /// **when the child has completed**.
    ///
    /// **`None` for the whole lifetime of a LIVE child** — not just during
    /// the pushing pass. TypeScript's `rule.child` (ts/src/rules.ts:665) and
    /// Go's `r.Child` (go/rule.go:1266) are the live child object from the
    /// push until the rule pushes again; today's Rust holds a frozen
    /// push-time snapshot there. This accessor cannot, because the live
    /// child is the engine's `&mut Rule` and cannot also be owned by its
    /// parent. Read the live child as `context.ancestors()[d + 1]`, as the
    /// callback's own `rule` when the callback IS the child, or as
    /// `context.pending()` during the pushing pass's after-actions.
    /// **[Q4]**, recorded in §10 differs-list item 5.
    ///
    /// The declarative `child` and `next` hops do NOT have this hole: the
    /// walker resolves them through the live descendant (§4), so
    /// `{"parent.child.name": ..}` and `{"parent.next.name": ..}` name the
    /// live child exactly as TypeScript and Go do. **[T1, Q5]**
    pub fn child(&self) -> Option<&Rule> {
        let mut cursor = self.child_rule.as_deref()?;
        while let Some(previous) = cursor.prev_rule.as_deref() {
            cursor = previous;
        }
        Some(cursor)
    }

    /// An owned copy of the state record: the explicit replacement for the
    /// removed `snapshot()`. A real copy (~120 B plus nine refcount bumps,
    /// two of them atomic); use it only to keep a value past the callback.
    pub fn to_snapshot(&self) -> RuleSnapshot { self.data.clone() }
    /// Install a fresh node cell: what `rule.node = v` means in TypeScript.
    /// A pushed rule shares its parent's cell, so `*rule.node.borrow_mut()
    /// = v` would overwrite the parent's node as well.
    pub fn set_node(&mut self, value: Value) { self.node = Rc::new(RefCell::new(value)); }
    /// Strip what a retained rule must not carry: the completed child's node
    /// copy and the parent-node handle. Used where the rule is retained with
    /// no event in between.
    pub(crate) fn retire(mut self: Box<Rule>) -> Box<Rule> {
        self.retire_in_place();
        self
    }
    /// `retire()` for a rule that is already stored in its holder's link,
    /// because a ruleDone event fires between the store and the strip
    /// (§3.3 replace/pop arms). **[P3]**
    pub(crate) fn retire_in_place(&mut self) {
        self.child_node = Value::Undefined;
        self.parent_node = None;
    }
    /// A link-free copy for the recovery/error paths that need a rule as it
    /// stood before recovery rewrote `current_rule` (parser.rs:817). It
    /// carries `data`, `parent_node`, `child_node`, `child_node_is_self` and
    /// the `next_rule` tag (it is `Copy`); `child_rule` and `prev_rule` are
    /// `None`, where today's `Rule::clone` carried their snapshots. Error
    /// path only.
    pub(crate) fn detached(&self) -> Rule;
    /// `detached()` with `state` overridden: the error site for a ruleDone
    /// subscriber failure (parser.rs:583/:616), built only on the error path.
    pub(crate) fn detached_with_state(&self, state: RuleState) -> Rule;
    pub(crate) fn accept_child_node(&mut self, child: &Rule);   // unchanged
    // REMOVED: pub fn snapshot(&self) -> Rc<RuleSnapshot>   (:1484-1486)
}

impl fmt::Debug for Rule { /* name~i, d, state, links printed as "name~i"/"-", never recursing */ }
impl fmt::Display for Rule { /* unchanged */ }
/// Unlinks `prev_rule`/`child_rule` chains iteratively so a 16K-long
/// replace chain does not drop recursively.
impl Drop for Rule { fn drop(&mut self) { /* §4 */ } }
```
**[R18]** `detached()` is specified above: tag carried, boxes `None`.

**Callback types — textually UNCHANGED:** `RuleSubscriber`, `LexSubscriber`, `ContextAction`, `Action` (lib.rs:144-149); `AltCondition`, `AltConditionWithMatch`, `AltConditionWithLexer`, `AltConditionWithLexerAndMatch`, `AltNext(WithMatch)`, `AltBack(WithMatch)`, `AltError(WithMatch)`, `AltModifier`, `AltModifierWithMatch` (with its `Option<&RuleSnapshot>`), `AltAction`, `StateAction` (with its `Option<&RuleSnapshot>`) (rule.rs:15-100); `ImperativeLexMatcher`, `ImperativeTextModifier`, `MapMerge`, `ContextParsePrepare` (options.rs); `TokenValFunc::new/call`, `Token::with_lazy_value`, `Token::resolve_val` (token.rs); every registration function in lib.rs except the two below.

**Changed:**

```rust
// rs/src/lib.rs:150 — the record gains a lifetime; closures infer it
pub type RuleDoneSubscriber = Arc<dyn Fn(&Rule, &Context, &RuleDone<'_>) + Send + Sync>;
// rs/src/options.rs:953 (graft)
pub type BudgetCheck = Arc<dyn Fn(&Rule, &Context) -> bool + Send + Sync>;
// rs/src/lib.rs:1332 / :1344
pub fn parse_budget(&mut self, check_every_n: usize,
    check: impl Fn(&Rule, &Context) -> bool + Send + Sync + 'static) -> &mut Self;
pub fn parse_budget_ref(&mut self, name: impl Into<String>,
    check: impl Fn(&Rule, &Context) -> bool + Send + Sync + 'static) -> &mut Self;
```

**Re-exports** (lib.rs:23, :43): add `Ancestors`, `AncestorsIter` from `context`, `NextRule` from `rule`; `RuleDone` is already exported.

**Engine-internal (parser.rs):**

```rust
#[derive(Clone, Copy)]
struct ParseSite<'a> { source: &'a str, alts: &'a [AltSpec] }   // `stack` field removed (:59-64)

/// TS `rule.next` for the after-actions of this pass, before the transition.
/// No lifetime: the pending rule is reached through `context.pending()`
/// **[P1]**, the parent through `context.ancestors().last()`, the rule
/// itself through the `&Rule` the caller already has.
#[derive(Clone, Copy, PartialEq, Eq)]
enum NextOf {
    /// A pass that pushes or replaces: `context.pending()`.
    Pending,
    /// Open pass with no route: `next` is the rule itself.
    Current,
    /// Close pass: `next` is `context.ancestors().last()` (None for the root).
    Parent,
}
```

---

## 3. File-by-file change list

### 3.1 `rs/src/context.rs`

| lines | change |
|---|---|
| :5 | `use crate::rule::Rule;` (drop `RuleSnapshot`) |
| :67-73 `follow_stack` | delete |
| :75-155 `same_*` | delete — the property they checked ("a buried frame does not move", :340-344) is definitional once the frames are the engine's objects |
| :183-191 fields | replace with `pub(crate) rs: Vec<Box<Rule>>` and `pub(crate) pending: Option<Box<Rule>>` **[P1]**, with the doc comments in §2 |
| :228-231 init | `rs: Vec::new(), pending: None` |
| :307-362 `set_active`, `sync_rule_stack`, `set_rule` | delete; add `ancestors()`, `parent()`, `pending()`, `depth()`, `Ancestors` (incl. `without_last` **[S5]**), `AncestorsIter` (§2) |
| :163 `#[derive(Debug)]` | stays; now needs `Rule: Debug` (manual impl, rule.rs) |

`ContextSeed`, `apply_seed`, `rewind`, `record_consumed`, the replay queue and `Context::new`'s signature are untouched. `Context::new` stays `pub(crate)`; nothing outside the engine constructs one (verified: no `Context::new`/`Context {` in json, debug, directive).

### 3.2 `rs/src/rule.rs`

| lines | change |
|---|---|
| :15-100 callback types | unchanged |
| :143-151 `RuleDone` | gains `'a`, `next`, `parent`; `PartialEq` by hand (§2) |
| :1201-1214 `Rule` | struct in §2; `#[derive(Clone)]` removed |
| :1216-1243 `Deref`/`DerefMut` | field projections; rewrite the doc comment (:1224-1238) to record the outcome: the 1.3x was blocked by *holders*, not by copy-on-write, and the holders are gone |
| :1245-1271 `RuleSnapshot` | delete :1258-1261 |
| :1315-1347 `new`, :1362-1400 `bound` | build `data: RuleSnapshot { .. }` without the four links; `child_rule: None, prev_rule: None, next_rule: NextRule::NoRule` |
| :1301-1313 `n_mut/u_mut/k_mut` | unchanged except the `u_mut` counter below |
| :1441-1454 `resolve_*_value` | unchanged |
| :1482-1486 `snapshot()` | delete; add `to_snapshot`, `child` **[P4]**, `set_node`, `retire`, `retire_in_place` **[P3]**, `detached`, `detached_with_state` |
| :1494-1498 `Display` | unchanged; add manual `Debug`, `impl Drop` (§4), `NextRule` with the acyclicity doc comment (§4) |

**The `u_mut` copy counter [P5, S3, Q7].** `empty_values()` (rule.rs:1283-1290) hands **every** rule one thread-local `Rc<HashMap<String, Value>>`, so `Rc::strong_count(&self.u) > 1` is true on the first write of every rule that was ever created — a counter keyed on that alone can never read zero, and the palindrome grammar (which writes `rule.u_mut()`) would fail test 11 as revision 2 specified it. Count a copy only when the map is not that sentinel:

```rust
pub fn u_mut(&mut self) -> &mut HashMap<String, Value> {
    #[cfg(debug_assertions)]
    if Rc::strong_count(&self.u) > 1 && !Rc::ptr_eq(&self.u, &empty_values()) {
        // `u` — unlike `n` and `k` (parser.rs:2446-2447/:2500-2501) — is
        // never handed to a pushed child, so after this PR the only thing
        // that can share a rule's own map is a `to_snapshot()` the plugin
        // chose to keep. The sentinel is excluded because every rule starts
        // out sharing it and copying an empty map is not the copy this
        // counter is about.
        debug_u_copies_inc();
    }
    Rc::make_mut(&mut self.u)
}
```
**Keying [Q7].** Revision 3 used `!self.u.is_empty()` and called `!Rc::ptr_eq(&self.u, &empty_values())` an equivalent alternative. They are not equivalent, and the `ptr_eq` form is the one to use: a map the grammar wrote and then emptied is a rule's OWN map, is shared with any outstanding `to_snapshot()`, and does copy — `is_empty()` misses that, `ptr_eq` counts it.

**What neither form fixes, and what test 11's second case must therefore do.** A rule whose `u` is still the sentinel when a `to_snapshot()` is taken shares the sentinel *with that snapshot*, so its first `u_mut()` really is a `make_mut` copy — of an empty map — and **both** keyings skip it (`is_empty()` because it is empty, `ptr_eq` because it IS the sentinel). Test 11's second case therefore cannot assert `debug_u_copies() == 1` unless the grammar writes `rule.u_mut()` **before** taking the snapshot, so that the shared map is the rule's own. §6 test 11 states that order; §0.2 item 11 records why the alternative offered by the required change does not work on its own.

The counter is `#[cfg(debug_assertions)]` on both sides, so release builds carry nothing. `empty_values()` costs a thread-local read and one `Rc` clone/drop per `u_mut` in debug builds only; if that ever shows up in a debug-build profile, hoist the sentinel `Rc` into a `thread_local!` `Cell<*const _>` and compare raw pointers.

Size arithmetic (informational; the invariants a test pins are in §6 test 12 **[R9]**): `RuleSnapshot` 152 → 120 B; `Rule` ≈ 120 + 8 + 72 + 8 + 8 + 1 + 2 → 224 B, one `Box` allocation per rule (today: one `Rc` allocation of 168 B). Still exactly one allocation per rule (#177 step 24 preserved).

### 3.3 `rs/src/parser.rs`

**Runners and helpers**

- `run_context_callback` (:324-351): delete :331. `run_state_callback` (:353-382): delete :362. `run_action_with_config` (:424-462): delete :431. Nothing else in them changes; the `map_err` bodies read `rule`, not the context.
- `run_after_actions` (:241-322): signature becomes `(&self, spec, is_open, rule: &mut Rule, context: &mut Context, next_of: NextOf, site: ParseSite<'_>)`. Line :257 `let next = rule.next_rule.clone();` becomes a **lazy** materialisation **[R2]** — no pre-scan of `actions`, no second hash lookup:
  ```rust
  let mut owned: Option<RuleSnapshot> = None;
  for binding in resolved_action_order(actions, callbacks, states, order) {
      output = match binding {
          ActionBinding::Named(action) => {
              if let Some(callback) = self.state_actions.get(&action) {      // the one lookup the arm already does (:262)
                  let next = Self::materialise_next(&mut owned, next_of, rule, context);
                  self.run_state_callback("named lifecycle after action", callback, rule, context, next, output)
                      .map_err(|error| self.attach_action_error(error, site.source, rule, context.ancestors(), site.alts))?
              } else {
                  self.run_action(&action, rule, context)
                      .map_err(|error| self.attach_action_error(error, site.source, rule, context.ancestors(), site.alts))?;
                  None
              }
          }
          ActionBinding::Callback(callback) => { /* as today, ancestors() in map_err */ None }
          ActionBinding::State(callback) => {
              let next = Self::materialise_next(&mut owned, next_of, rule, context);
              self.run_state_callback("lifecycle after action", &callback, rule, context, next, output)
                  .map_err(|error| self.attach_action_error(error, site.source, rule, context.ancestors(), site.alts))?
          }
      };
      output = self.check_lifecycle_output(output, rule, context.ancestors(), site)?;
  }
  ```
  with the helper — **written as `match`, not `.map(..)`** **[S1, §0.3]**:
  ```rust
  /// `next` for a state action: a copy of the pending rule, of the rule
  /// itself, or of its parent, made on the first state action of the pass
  /// and reused by the rest. `rule` and `context` are only read here, so the
  /// caller's `&mut` borrows are free again when it returns, and the result
  /// borrows `owned` alone.
  fn materialise_next<'o>(
      owned: &'o mut Option<RuleSnapshot>, next_of: NextOf, rule: &Rule, context: &Context,
  ) -> Option<&'o RuleSnapshot> {
      match next_of {
          NextOf::Pending => match context.pending() {
              Some(pending) => Some(&*owned.get_or_insert_with(|| pending.to_snapshot())),
              // The engine always parks a rule before a pushing or replacing
              // pass's after-actions; `pending` is `pub(crate)`, so no
              // callback can have removed it.
              None => { debug_assert!(false, "a pushing or replacing pass ran its after-actions with no pending rule"); None }
          },
          NextOf::Current => Some(&*owned.get_or_insert_with(|| rule.to_snapshot())),
          NextOf::Parent => match context.ancestors().last() {
              Some(parent) => Some(&*owned.get_or_insert_with(|| parent.to_snapshot())),
              None => None,
          },
      }
  }
  ```
  `next` borrows `owned` (a local), never `rule` and never `context`, so both go to the callback as `&mut`. The `Named` arm's `state_actions.get` is the single SipHash it performs today; a miss (every builtin lifecycle name — `@val-bo`, `@map-bo`, `@list-bo`, `@val-bc`, `@pair-bc`, `@elem-bc` resolve through builtins.rs:28-34, not `state_actions`) materialises nothing. **Do not** cache a `needs_next` bool on `RuleSpec`: `bo`/`ao`/`bc`/`ac` are public mutable fields, and a cache derived from a public mutable field is a cache with a second writer. Every `site.stack` in the `map_err` closures becomes `context.ancestors()`; the closure runs after the `&mut context` borrow of the call has ended, so it compiles as today's `&stack` does.

  **The pending protocol [P1, V2].** A pass that pushes or replaces moves the rule it built into `context.pending` before calling `run_after_actions(.., NextOf::Pending, ..)` and takes it back immediately after, so that **every after-action that receives a `Context`** — `ActionBinding::Callback` (a `ContextAction` from `add_action`/`ao_fns`/`ac_fns`), `ActionBinding::State`, a `Named` binding that resolves through `self.context_actions` (registered by `action_with_context`, lib.rs:882-888, or `state_action_ref`, lib.rs:908-913), and anything they call — can read it as `context.pending()`. This is what today's `:2450`/`:2451`/`:2503` snapshot stores gave those callbacks (they precede the after-actions at :2452/:2504), and what TypeScript gives every after handler (`rule.child`/`rule.next` at rules.ts:665/:720, the `next` argument at :728). A `StateAction` additionally receives it as its `next` argument, as a copy: a `&Rule` borrowed from `context` cannot coexist with the `&mut Context` of the same call. The copy is not observable — the pending rule is reachable only as `&Rule`, so no after-action can mutate it (§0.2 item 1) — and it costs one `to_snapshot()` per push/replace pass that binds a lifecycle state action, which is zero on both benchmarks and on the shipped JSON grammar (§9).

  **The one after-action kind that cannot [V2], and it is a capability removal.** `run_action_with_config` (parser.rs:424-446) resolves a `Named` binding in three steps: builtins first (they get `context`), then `self.actions.get(name)` — which calls `action(rule)` — and only then `self.context_actions`. `Action = Arc<dyn Fn(&mut Rule) + Send + Sync>` (lib.rs:144, registered by `Tabnas::action`, lib.rs:839-846) receives **no `Context` at all**, so `context.pending()` is unreachable from it, and today it reads the pending rule through `rule.child_rule` / `rule.next_rule`, which :2450-2451 (push) and :2503 (replace) set before the after-actions at :2452/:2504. Revision 4 claimed here, and in §10 item 2, that every after-action keeps the capability; for this kind it does not. Note the resolution order: a name registered both ways resolves to the context-less one. `rs/tests/api_test.rs:108-114` is exactly this registration in `ao` and stays green only because it reads `next_rule_name`, which survives. §7 carries the migration (re-register with `action_with_context` or as a state action), §10 item 2 records the loss, and **§0.4 R9 puts the removal itself to the maintainer** rather than deciding it here.

- `check_lifecycle_output` (:384-394) and `raised_token_error` (:396-406) **[R5]**: both read `site.stack` today; each gains `ancestors: Ancestors<'_>` after `rule`: `check_lifecycle_output(&self, output, rule: &Rule, ancestors: Ancestors<'_>, site: ParseSite<'_>)` and `raised_token_error(&self, token: &Token, rule: &Rule, ancestors: Ancestors<'_>, site: ParseSite<'_>)`; :405 becomes `self.attach_error(error, rule, ancestors, site.alts, Some(token))`. Callers: run_after_actions (:319, shown above, fed `context.ancestors()` after the callback has returned) and the unknown-route site (:2401): `self.raised_token_error(&token, &current_rule, context.ancestors(), ParseSite { source: src, alts })`.
- `attach_error` (:485), `attach_action_error` (:408), `attach_active_error` (:531): `stack: &[Rule]` → `ancestors: Ancestors<'_>`; body :493 becomes `ancestors.iter().map(|item| item.name.to_string()).collect()`. `error.rule_stack` output is byte-identical.
- `ancestors_for` (:558-564) **[S5]**:
  ```rust
  fn ancestors_for<'a>(rule: &Rule, ancestors: Ancestors<'a>) -> Ancestors<'a> {
      if ancestors.last().is_some_and(|ancestor| ancestor.i == rule.i) { ancestors.without_last() } else { ancestors }
  }
  ```
  `Ancestors` owns its slice privately, so the strip is `Ancestors::without_last` (§2), `pub(crate)` in context.rs. It is used **only** for error attachment, exactly as today, and it must stay: after a push the completed rule *is* `rs.last()` when its ruleDone fires (§2), and an error raised from that subscriber must not list it twice.
- `notify_rule_done` (:566-599) **[R13, S6]**:
  ```rust
  #[allow(clippy::too_many_arguments)]   // 8 params: rule, context, state, alt, src, next, parent (+ &self)
  fn notify_rule_done(&self, rule: &Rule, context: &Context, state: RuleState,
      alt: Option<RuleDoneAlt>, src: &str, next: Option<&Rule>, parent: Option<&Rule>) -> Result<(), TabnasError>
  ```
  builds `RuleDone { state, alt, forced: false, next, parent }`; drops the `stack` parameter (reads `context.ancestors()`); :583-584 `let mut site_rule = rule.clone(); site_rule.state = state;` is deleted and the site rule is built **inside** `map_err` as `rule.detached_with_state(state)` — error path only, so the debug plugin's per-completion cost does not grow. `notify_forced_close` (:601-632) likewise takes `next: Option<&Rule>, parent: Option<&Rule>`, which brings it to 6 arguments including `&self` — **under clippy's threshold of 7, so it gets no `#[allow]`** **[T5]**.

  **The attribute inventory, corrected [T5], with the `recover_error_pass` row corrected again [U3].** Counted from parser.rs:800-811, `recover_error_pass` takes 11 parameters today (`&self`, `error`, `state`, `alt`, `fallback_error_token`, `src`, `current_rule`, `stack`, `context`, `lexer`, `mode`) and **10** after `−stack` — revision 4's table said "11 → 11". The conclusion is unchanged (10 > clippy's 7, so the `#[allow]` at :799 stays), but a table that exists to correct a miscount must not contain one. Revision 3 said "`attempt_recover`/`recover_error_pass` already carry it (parser.rs:633, :798)". Verified against `main`: **`attempt_recover` (fn at parser.rs:634) carries no such attribute** — it is 7 arguments (`&self`, `error`, `current_rule`, `stack`, `context`, `lexer`, `mode`), exactly at the threshold — and the two that do carry it are `recover_error_pass` (attribute at :799) and `recover_after_actions` (attribute at :848). After this PR:

  | fn | args before | args after | `#[allow(clippy::too_many_arguments)]` |
  |---|---:|---:|---|
  | `notify_rule_done` | 7 | 8 (`+next`, `+parent`, `−stack`… net +1) | **added by this PR** |
  | `notify_forced_close` | 5 | 6 | no — do not add one |
  | `attempt_recover` | 7 | 7 (`−stack`, `+pending`) | no — it never had one, and must not gain a dead one |
  | `recover_error_pass` | 11 | **10** (`−stack`; no `NextOf` is added, §0.2 item 10) | keeps the one at :799 |
  | `recover_after_actions` | 10 | 9 (`−stack`) | keeps the one at :848; it could now be dropped, but leave it — removing an `#[allow]` that is no longer needed is noise in a diff this size |
- `attempt_recover` (:634-797), `recover_error_pass` (:800-846), `recover_after_actions` (:849-877): `current_rule: &mut Box<Rule>`, drop `stack: &mut Vec<Rule>` (use `context.rs`). :656 becomes `let sync = compute_sync_tins(current_rule, context.ancestors(), &self.rules, &self.options);` — a shared reborrow of `current_rule` and a shared reborrow of the same `&mut Context` in one call; both shared, result owned, so it compiles **[R5]**.

  **The failed-after-action path [P2, Q1, Q2, T4, Q6].** `recover_after_actions` and `recover_error_pass` gain nothing in their signatures — in particular **no `NextOf` parameter**, see §0.2 item 10; `recover_error_pass` takes the pending rule out of the context itself, and `attempt_recover` gains one parameter `pending: &mut Option<Box<Rule>>` (7 arguments before, 7 after, so no new `#[allow]`, **[T5]**):
  ```rust
  // recover_error_pass, replacing :817-840
  let mut pending = context.pending.take();               // None unless the failed pass pushed or replaced
  let pending_i = pending.as_deref().map(|rule| rule.i);
  let event_rule = current_rule.detached();               // fatal flaw 4, closed; [R18]: link-free, tag carried
  // Captured as plain `usize`es BEFORE recovery runs, so nothing is
  // borrowed across the `&mut` it takes. [Q2]
  let failed_i = current_rule.i;
  let parent_i = context.ancestors().last().map(|frame| frame.i);
  let recovered = mode.recovering
      && self.attempt_recover(error.clone(), current_rule, context, lexer, mode, &mut pending)?;
  /* the fallback_error_token block is unchanged */
  // [V1] The frame at `d - 1` if recovery left it on the stack; otherwise
  // the rule recovery popped TO, which is that same parent, and which is
  // what TypeScript's `prev.parent` holds throughout (rules.ts:665-666,
  // never reassigned; `bad()`/`attemptRecover` leave it intact, :1183-1193).
  // Without the second arm this read is `None` on exactly the arms where
  // `done_next` arm 5 names the parent, and the event reports
  // `next = <the parent>` with `parent = None` for one rule. Written as a
  // `match`, not `.or_else(|| ..)`, per §0.3; `parent_i` is a plain `usize`
  // captured before `attempt_recover` ran, so nothing is held across it.
  let done_parent: Option<&Rule> =
      match event_rule.d.checked_sub(1).and_then(|k| context.ancestors().get(k)) {
          Some(frame) => Some(frame),
          None if Some(current_rule.i) == parent_i => Some(&**current_rule),
          None => None,
      };
  // TS reads `prev.next`, which `bad()` leaves exactly as the pass set it
  // (rules.ts:720 before the afters at :728; :1183-1193). Resolution order:
  // the pending rule if it is still reachable, then the failed rule's own
  // tag. [P2, Q1, Q2]  Ratification item R8.
  let done_next: Option<&Rule> = match (pending.as_deref(), event_rule.next_rule) {
      // 1. A failed push or replace whose unrun rule nobody retained: the
      //    caller still owns it, so it can be reported before it is dropped.
      (Some(unrun), _) => Some(unrun),
      // 2. A failed push whose unrun child `attempt_recover` retained
      //    (the `accepts_close` arm).
      (None, NextRule::Child) if pending_i.is_some() => {
          current_rule.child().filter(|child| Some(child.i) == pending_i)
      }
      // 3. A failed pass on a rule that had already completed a child, and
      //    which recovery kept current: TS's `prev.next` is that child.
      (None, NextRule::Child) if current_rule.i == failed_i => current_rule.child(),
      // 4. A failed open pass with no route: TS `prev.next === prev`. The
      //    subscriber's `rule` argument is `&event_rule` too, so this is
      //    pointer-identical to the rule it is told completed.
      (None, NextRule::Same) => Some(&event_rule),
      // 5. A failed close pass: TS's `prev.next` is the popped-to parent.
      //    Recovery either left it on the stack (`accepts_close`) or popped
      //    to it (`!pop_until_valid`, forced pop), and `parent_i` tells the
      //    two apart without holding a borrow across `attempt_recover`.
      (None, NextRule::Parent) => match done_parent.filter(|frame| Some(frame.i) == parent_i) {
          Some(frame) => Some(frame),
          None if Some(current_rule.i) == parent_i => Some(&**current_rule),
          None => None,
      },
      // 6. `Replaced` with the replacement gone, `Child` on a rule recovery
      //    moved away from, `NoRule`: nothing that still exists to name.
      (None, _) => None,
  };
  self.notify_rule_done(&event_rule, context, state, alt, src, done_next, done_parent)?;
  ```
  **After the [V1] normalisation, arm 5's two branches converge.** `done_parent` may now itself be `&**current_rule`, so arm 5's first branch (`done_parent.filter(..)`) returns what its second branch used to, and `ptr::eq(done.next.unwrap(), done.parent.unwrap())` holds on the recovery-popped close — which is what the ordinary pop already gives (test 3) and what TypeScript gives, `prev.next` being `ctx.rs[--ctx.rsI]` and `prev.parent` the pusher, the same object. The second branch is kept rather than deleted: it is the branch that states the intent, it costs nothing, and it keeps arm 5 correct if the normalisation is ever taken back out under §0.4 R8. Arms 1 and 2 are **[P2]** as revision 3 had them, with the consumption bug of **[Q1]** fixed in `attempt_recover` below. Arms 3-5 are **[Q2]**: revision 3 reported `None` for a failed self or pop pass, where TypeScript reports the rule itself and the resuming parent. Arm 6 is the residue, listed as §10 item (7) rather than left silent. `done.parent` is the frame at `d - 1` if recovery left it on the stack.

  Borrows: `done_parent` borrows `context` shared on its first arm and, after **[V1]**, `current_rule` shared on its second; arm 5's first branch inherits whichever of the two that was; arms 2, 3 and 5's second branch borrow `current_rule` shared; arm 4 borrows the local `event_rule`. Every one of them is shared, and every one stays live until `notify_rule_done`, which takes `&Rule`, `&Context`, `Option<&Rule>`, `Option<&Rule>` — so they coexist. The constraint the [V1] arm adds, and which a later edit must not break: **no `&mut current_rule` and no `&mut context` use may be inserted between the `done_parent` binding and the notify.** `NextRule` is `Copy`, so matching on `event_rule.next_rule` borrows nothing. Arm 5 is written as a nested `match` rather than `.or_else(|| ..)` for the reason §0.3 gives: a closure returning a reference derived from a capture is a shape worth not depending on here.

  Inside `attempt_recover`, with one shared helper for the tag hygiene the two non-`accepts_close` arms need **[T4, Q6]**:
  ```rust
  /// A rule that failed a pass which had parked a pending rule keeps a tag
  /// naming that rule. Recovery can only retain the pending on the
  /// `accepts_close` PUSH arm; everywhere else `recover_error_pass` reports
  /// it as `done.next` and drops it, and a tag left behind would make
  /// `next.*` resolve to nothing, or worse, to an older child of the same
  /// rule. `next_rule_name` is deliberately left alone: it is a name, not a
  /// link, and it is what TypeScript's equivalent survives as.
  fn clear_unrun_link(rule: &mut Rule, unrun: bool) {
      if unrun && matches!(rule.next_rule, NextRule::Child | NextRule::Replaced) {
          rule.next_rule = NextRule::NoRule;
      }
  }

  /// `done.next` for a forced close, in the order **[Q2]** fixed for the
  /// error pass: the unrun pending rule if the caller still owns one, then
  /// the rule's own tag. Every source is a shared borrow and the result
  /// unifies their lifetimes, so it coexists with the `&Rule` and
  /// `&Context` the notify takes. **[V3]**
  fn forced_next<'a>(rule: &'a Rule, ancestors: Ancestors<'a>, pending: Option<&'a Rule>)
      -> Option<&'a Rule>
  {
      if let Some(unrun) = pending {
          return Some(unrun);
      }
      match rule.next_rule {
          NextRule::Same => Some(rule),
          NextRule::Child => rule.child(),          // the chain HEAD [P4]
          NextRule::Parent => ancestors.last(),
          NextRule::Replaced | NextRule::NoRule => None,
      }
  }
  ```
  - :768-770 (`pop_until_valid == false`) becomes
    ```rust
    if let Some(parent) = context.rs.pop() {
        let mut abandoned = std::mem::replace(current_rule, parent);
        clear_unrun_link(&mut abandoned, pending.is_some());     // [T4, Q6]
        current_rule.child_rule = Some(abandoned.retire());
        return Ok(true);
    }
    return Ok(false);
    ```
    so the abandoned child stays reachable through `child()`/`child.*`, as TS's `rule.child` is. The resuming rule's `child_rule` is taken by the abandoned rule, so the **pending** rule cannot be retained here: it is left for `recover_error_pass` to report as `done.next` and drop, and the abandoned rule's tag is reset so it does not name it. §0.4 R5, §10 item (3).
  - :775-782 (`accepts_close`: recovery keeps the rule current) **[P2, S10, Q1]**:
    ```rust
    if accepts_close(current_rule, candidate.tin, &self.rules, &self.options) {
        if current_rule.state == RuleState::Open { current_rule.state = RuleState::Close; }
        else { current_rule.skip_befores = true; }
        // Read the tag and the pending FLAG into locals first. Revision 3
        // wrote `match (current_rule.next_rule, pending.take())`, which
        // emptied the caller's Option on EVERY arm and dropped the unrun
        // replacement inside the `Replaced` arm, so `recover_error_pass`
        // could not report it [Q1]. `match (.., pending.as_deref())` does
        // not fix it either: a match scrutinee temporary lives to the end
        // of the match, so the shared borrow of `pending` would still be
        // live when the `Child` arm calls `pending.take()` (E0502). A
        // copied tag and a `bool` borrow nothing.
        let tag = current_rule.next_rule;                        // NextRule: Copy
        let unrun = pending.is_some();
        if unrun {
            match tag {
                // A PUSH whose after-action failed: keep the unrun child, as TS does.
                NextRule::Child => {
                    let child = pending.take().expect("checked by `unrun`");
                    current_rule.child_rule = Some(child.retire());
                }
                // A REPLACE whose after-action failed: nothing can own the unrun
                // replacement (§0.4 R5). Leave it with the CALLER so the error
                // pass can report it as `done.next`, and clear the tag so it
                // does not dangle.
                NextRule::Replaced => { current_rule.next_rule = NextRule::NoRule; }
                // Any other failed pass: the tag named a rule that still exists
                // (Same / Parent / a completed Child) or none at all.
                _ => {}
            }
        }
        return Ok(true);
    }
    ```
    `next_rule_name` is left as today in both cases. For the push case `next.*` and `child.*` both resolve to the unrun child (tag `Child` → the cursor's `child` hop → `child()`, the chain head, §4); for the replace case `next.*` resolves to nothing — §10 item (3).
  - :784-796 (the forced-pop loop) becomes, with `done.next` as the **head** of the chain **[P4]** and the same tag hygiene on the rule that failed **[T4, Q6]**:
    ```rust
    let unrun = pending.is_some();   // nothing on this path can own the pending rule
    // [V3] The rule that FAILED gets the same `done.next` the error pass
    // will give it one call later: the unrun pending rule if the failed
    // pass parked one, else its own tag. TypeScript fires this synthesized
    // event with the live rule and its links intact (rules.ts:1197-1201),
    // so hard-coding `None` here reported less than any port does. `rs` has
    // not been touched yet, so `ancestors().len() == current_rule.d` and
    // `ancestors().last()` is its parent.
    let forced_next = Self::forced_next(&**current_rule, context.ancestors(), pending.as_deref());
    self.notify_forced_close(current_rule, context, &src, forced_next, context.ancestors().last())?;
    let mut failed = true;
    while let Some(parent) = context.rs.pop() {
        let mut child = std::mem::replace(current_rule, parent);
        if failed { clear_unrun_link(&mut child, unrun); failed = false; }   // [T4, Q6]
        current_rule.accept_child_node(&child);
        current_rule.child_rule = Some(child.retire());        // next_rule is already NextRule::Child
        if accepts_close(current_rule, candidate.tin, &self.rules, &self.options) { return Ok(true); }
        // The `pop()` above already happened, so `ancestors().len() ==
        // current_rule.d` here too — TypeScript likewise decrements
        // `ctx.rsI` before `s(r, ctx, done)` (rules.ts:1203-1211).
        // Deliberately NOT `pending.as_deref()`: this rule's `Child` tag
        // names the child it really pushed, which the loop has just retired
        // into `child_rule`, and `forced_next` resolves that to the chain
        // HEAD, not the tail **[P4]**.
        let forced_next = Self::forced_next(&**current_rule, context.ancestors(), None);
        self.notify_forced_close(current_rule, context, &src, forced_next, context.ancestors().last())?;
    }
    Ok(false)
    ```
    `clear_unrun_link` runs on the FIRST rule the loop moves, which is the rule that failed; every later one is an ancestor whose `Child` tag names the child it really pushed and which this loop has just retired into its `child_rule`. Four shared reborrows of `current_rule`/`context` now reach the `notify_forced_close` call — `&**current_rule` and `context.ancestors()` through `forced_next`, then `current_rule` and `&*context` in the call itself, plus `context.ancestors().last()` — and every one of them is shared, so they coexist; `forced_next` returns an owned `Option<&Rule>` that holds no `&mut`. `child()` is the iterative `prev_rule` walk of §2. **[V3]**
- `ensure_lookahead` (:1203-…): `site.stack` → `context.ancestors()` in the `map_err`s and at :1246; the two `continuation_tins` calls (:1284, :1310) become `continuation_tins(context, rule, context.ancestors(), &self.rules, &self.options, context.t.len(), None)` — three shared reborrows of the same `&mut Context` (`&*context` by coercion, `context.ancestors()`, `context.t.len()`) in one expression, no `&mut` among them, so it compiles **[R5]**.
- `update_partial`/`best_partial_value` (:2934-…), `compute_sync_tins` (:3154), `continuation_tins` (:3223), `conditions_match` (:3346): `stack: &[Rule]` → `Ancestors<'_>`.
- The four walkers (:3387-:3534) collapse into one (§4).

**The parse loop**

- :1396 `let mut current_rule = Box::new(Rule::new(..));` :1407 `let mut stack` deleted. Every `&stack` / `&mut stack` (120 uses) becomes `context.ancestors()` / `context.rs`.
- :1428 `context.set_active(&current_rule, &stack);` deleted. **No `debug_assert!(current_rule.d == context.rs.len())` replaces it**: `d` is a plugin-writable field, and a debug panic on legal plugin behaviour is exactly what the judges refused in Design 4. The invariant is pinned by tests instead (§6).
- :1446 `check(&context)` → `check(&current_rule, &context)`.
- :1540 `let next = is_open.then(|| current_rule.snapshot());` → lazy, through the same helper **[R2, R4, S2, U1]**. Write it as a plain `if`, deliberately — §0.3 withdrew the claim that the closure form is *rejected* (it very likely compiles); what stands is that this site is written as an `if` on purpose and a reviewer must not "simplify" it back without compiling:
  ```rust
  let mut owned: Option<RuleSnapshot> = None;            // before the binding loop
  // ... inside the `Named`-hit arm (:1551) and the `State` arm, before the call:
  let next = if is_open {
      Self::materialise_next(&mut owned, NextOf::Current, &current_rule, &context)
  } else {
      None
  };
  self.run_state_callback(label, callback, &mut current_rule, &mut context, next, output)
  ```
  The `Named`-miss arm (:1551 else) and the `Callback` arm touch neither. A copy happens only on a pass with a bound before-open **state** action (callback_test.rs:101-123 needs it; neither benchmark nor the shipped JSON grammar binds one — its `@val-bo` is a builtin and misses `state_actions`). Today this line is an `Rc` bump that turns the builtin's first write (builtins.rs:310) into a full-record copy; after the change there is no holder and no copy.

  **When the copy is taken changes here too, and it is observable [U1].** Today `:1540` is evaluated **once**, before the `for binding in resolved_action_order(actions, callbacks, states, order)` loop at :1546, so every before-action of the phase sees the rule as it stood before the first of them ran. The replacement materialises `owned` at the **first bound state action** — inside the `Named`-hit arm (:1551) and the `State` arm. `resolved_action_order` (rs/src/rule.rs:1054-1071) returns `order.to_vec()` whenever `order_matches`, and `bo_order` interleaves kinds in declaration order (`ActionBinding::Callback` pushed at rule.rs:340 and :464, `ActionBinding::State` at :512), so a grammar that declares a `bo` `ContextAction` before a `bo` state action now hands that state action a `next` reflecting the ContextAction's writes, where today it does not. On the fallback ordering (when `order_matches` fails, rule.rs:1062-1070) *every* callback precedes *every* state action, so the change is observable there whenever a phase binds both. This is the identical change **[S8]** records for `ao`/`ac`, and like it, it is a move **toward** TypeScript, which passes the live rule (`let next = is_open ? rule : ctx.NORULE`, ts/src/rules.ts:535, handed to `befores[bI].call(this, rule, ctx, next, bout)` at :564) and therefore reflects every earlier write. Revision 4 claimed here and in §10 that the before-open `next` is "unchanged by this release"; that claim is **deleted** from both places, and §10's `[S8]` paragraph now names `bo`/`bc` alongside `ao`/`ac`. What remains true, and is still claimed: the `AltModifierWithMatch` `next` at :2089 does not move — its `then()` sits immediately before the single modifier call at :2092, so nothing can run between them (see the :2089 bullet below).
- :1729-1740 the `candidate` clone becomes an in-place swap:
  ```rust
  if alt.c_ref.is_some() || !alt.c.is_empty() {
      let saved = if is_open { std::mem::replace(&mut current_rule.o, Rc::clone(&tokens)) }
                  else       { std::mem::replace(&mut current_rule.c, Rc::clone(&tokens)) };
      if !builtin_condition_matches(alt.c_ref.as_deref(), &current_rule)
          || !conditions_match(&alt.c, &current_rule, context.ancestors())
      {
          if is_open { current_rule.o = saved } else { current_rule.c = saved }
          alt_matches = false;
      }
  }
  ```
  (:1743/:1745 then re-store the same `Rc` on success: one redundant refcount bump, only on grammars with declarative conditions.)
- The 16 mid-step publishes — :1748, :1764, :1786, :1812, :1887, :1943, :1960, :1977, :1994, :2015, :2032, :2053, :2069, :2088, :2187, :2228 — each lose their `context.set_rule(&current_rule);` line and nothing else; their `map_err` `&stack` becomes `context.ancestors()`.
- :2089 `let next = is_open.then(|| current_rule.snapshot());` → `let next = is_open.then(|| current_rule.to_snapshot());` and the call at :2092 becomes `modifier(matched, &mut current_rule, &mut context, next.as_ref())` **[R4]** — `RuleSnapshot` is not `Deref`, so `as_deref()` would not compile; `next` here owns a `RuleSnapshot` and captures only `&current_rule`, so `bool::then` is legal at this site (§0.3). Already inside `if let Some(modifier) = alt.h_match`; a borrow is impossible because `next` *is* the current rule that the modifier receives as `&mut`. TypeScript passes the live rule to the modifier too (rules.ts:577); the copy is pre-existing and unchanged **[P6]**.

**The five transition arms** (push :2435-2487, replace :2488-2538, self :2539-2568, pop :2569-2609, and their empty-alternatives twins :2632-2661 / :2662-2700). `notify_rule_done` moves out of the shared tail (:2610, :2701) and into each arm, because the completed rule is now a moved `Box` rather than a clone, and because each arm knows `next` and `parent` **[R13]**. **In the replace and pop arms the link is stored BEFORE the event and stripped after it** **[P3]** — which is the order today's code already has (:2536 and :2602 precede the notify at :2610), so a subscriber keeps seeing `done.next.prev_rule` / `done.parent.child_rule` set, as TypeScript's `prev.next.prev === prev` (rules.ts:693) does. The strip is not behind the `?`, so the retention invariant holds even when a subscriber errors:

```rust
// PUSH
let mut child = Box::new(Rule::bound(push_shared.clone(), current_rule.node.clone(), self.rules.get(push_name)));
child.i = next_rule_id; next_rule_id += 1;
child.d = context.rs.len() + 1;
child.parent_node = Some(current_rule.node.clone());
child.n = Rc::clone(&current_rule.n);
child.k = Rc::clone(&current_rule.k);
// :2448 deleted — the child's parent is context.rs.last() once it is current
current_rule.next_rule_name = Some(push_shared);
current_rule.child_rule = None;                 // :2450 — TS overwrites rule.child here (rules.ts:665);
                                                //         the pending child is context.pending() [P1]
current_rule.next_rule = NextRule::Child;       // :2451
context.pending = Some(child);                  // [P1] an 8-byte move, not a copy
let after = self.run_after_actions(&spec, is_open, &mut current_rule, &mut context,
    NextOf::Pending, ParseSite { source: src, alts });
update_partial(mode, &root_node, &current_rule, context.ancestors());
if self.recover_after_actions(after, state, done_alt.clone(), src, &mut current_rule, &mut context, &mut lexer, mode)? { continue 'parse; }
//   `recover_after_actions` returning Ok(false) means the after-actions did NOT fail, so
//   `recover_error_pass` never ran and never took the pending rule. On the failing path it
//   takes it, reports it as `done.next`, and either retires it onto `child_rule` or drops it.
let child = context.pending.take().expect("after-actions that did not fail leave the pending rule");
if is_open { current_rule.state = RuleState::Close; }
// :2484 deleted
context.rs.push(std::mem::replace(&mut current_rule, child));          // :2485-2487: an 8-byte pointer move
let pusher = &**context.rs.last().expect("pushed");
let grandparent = context.rs.len().checked_sub(2).map(|k| &*context.rs[k]);
self.notify_rule_done(pusher, &context, state, done_alt, src, Some(&current_rule), grandparent)?;
//   the subscriber sees rule.child_rule == None (cleared at push; §10 differs-list item 5); done.next IS the child

// REPLACE
let mut next = Box::new(Rule::bound(replace_shared.clone(), current_rule.node.clone(), self.rules.get(replace_name)));
next.i = next_rule_id; next_rule_id += 1;
next.d = current_rule.d;
next.parent_node = current_rule.parent_node.clone();
// :2499 deleted — the parent is structural: the same ancestors
next.n = Rc::clone(&current_rule.n);
next.k = Rc::clone(&current_rule.k);
current_rule.next_rule_name = Some(replace_shared);
current_rule.next_rule = NextRule::Replaced;    // :2503 — was Some(next.snapshot()): the second holder of `next`
context.pending = Some(next);                   // [P1]
let after = self.run_after_actions(&spec, is_open, &mut current_rule, &mut context,
    NextOf::Pending, ParseSite { source: src, alts });
/* update_partial / recover_after_actions as above */
let next = context.pending.take().expect("after-actions that did not fail leave the pending rule");
if is_open { current_rule.state = RuleState::Close; }
let completed = std::mem::replace(&mut current_rule, next);            // :2537-2538
current_rule.prev_rule = Some(completed);                              // :2536 — stored BEFORE the event [P3]
let notified = self.notify_rule_done(
    current_rule.prev_rule.as_deref().expect("just stored"),           // the completed rule, by identity
    &context, state, done_alt, src,
    Some(&*current_rule),                                              // done.next = the replacement (TS prev.next)
    context.ancestors().last());                                       // done.parent (TS prev.parent)
current_rule.prev_rule.as_mut().expect("just stored").retire_in_place();
notified?;

// SELF (open pass, no route)
current_rule.next_rule_name = Some(current_rule.name.clone());          // :2540 — a plain store now
current_rule.next_rule = NextRule::Same;                                // :2541
let after = self.run_after_actions(&spec, true, &mut current_rule, &mut context, NextOf::Current, ParseSite { source: src, alts });
/* ... */
current_rule.state = RuleState::Close;
self.notify_rule_done(&current_rule, &context, RuleState::Open, done_alt, src, Some(&current_rule), context.ancestors().last())?;

// POP (close pass, no route)
current_rule.next_rule_name = context.rs.last().map(|rule| rule.name.clone());
current_rule.next_rule = NextRule::Parent;                              // :2572
let after = self.run_after_actions(&spec, false, &mut current_rule, &mut context, NextOf::Parent, ParseSite { source: src, alts });
/* ... */
match context.rs.pop() {                                                 // :2598
    Some(parent) => {
        let completed = std::mem::replace(&mut current_rule, parent);
        current_rule.accept_child_node(&completed);                     // :2601 (reads the chain TAIL's node — pre-existing, §4)
        current_rule.child_rule = Some(completed);                      // :2602 — stored BEFORE the event [P3]
        let notified = self.notify_rule_done(
            current_rule.child_rule.as_deref().expect("just stored"),
            &context, RuleState::Close, done_alt, src,
            Some(&*current_rule), Some(&*current_rule));                // ancestors() = stack after the pop (TS); next == parent
        current_rule.child_rule.as_mut().expect("just stored").retire_in_place();
        notified?;
        // next_rule is still Child from the push (:2603); `child()` resolves to the chain HEAD (§4)
    }
    None => {
        // [U2] The root popped: the parse result is read BEFORE the event,
        // which is today's order (parser.rs:2607 precedes :2610; the
        // empty-alternatives twin is :2698 before :2701). `rule.node` is an
        // `Rc<RefCell<Value>>` that a subscriber can still write through
        // `borrow_mut()` (§2 **[S14]**), so the other order would let a
        // ruleDone subscriber on the root's final pass change what
        // `parse()` returns — a parse-result change under §10's freeze.
        // The `?` behaves the same either way: a failing subscriber
        // propagates before `final_value` is read.
        completed_value = Some(current_rule.node.borrow().clone());
        self.notify_rule_done(&current_rule, &context, RuleState::Close, done_alt, src, None, None)?;
    }
}
```

Borrow notes for the two rewritten arms: every argument of those `notify_rule_done` calls is a shared borrow — `current_rule.prev_rule.as_deref()` and `&*current_rule` are two shared reborrows of the same `Box<Rule>`, `&context` and `context.ancestors().last()` two of the same `Context` — so they coexist. `notified` is an owned `Result`, holding no borrow, which is what lets the strip run before the `?`.

The empty-alternatives twins (:2632-2661, :2662-2700) take the SELF and POP shapes verbatim with `done_alt = None` — **including the POP `None` branch's read-before-notify order** (today: :2698 precedes :2701) **[U2]**. The no-match branch (:2718 onward) only changes `&stack` → `context.ancestors()`.

**Per copying site — the exact new shape and why it no longer copies**

| site (verified Ir) | today | after | why 0 copies |
|---|---|---|---|
| :1743 `current_rule.o = Rc::clone(&tokens)` (1.84M) | `deref_mut` = `make_mut`, count 2, and the second holder is `context.rule` from :1428 **alone** **[U4]** — on every path the transition's own `deref_mut` has already detached the record the new current rule carries: :2536 copies `next` while :2503 still holds the old record (replace), :2484 copies the child while :2450/:2451 hold the old one (push), :2602 copies the resumed parent while the retired child's `parent_rule` holds the old one (pop). Revision 4 credited this row with a surviving forward link from :2503; that link names the record :2536 left behind, not the one :1743 writes | same text; `current_rule: Box<Rule>`, `deref_mut` = `&mut self.data` | no `context.rule` exists; the forward link is now the tag `Replaced`/`Child` on the *other* rule; nothing can hold `data` |
| :1745 `current_rule.c = ...` (2.01M) | count 2 from :1428 — the same single holder, which is why the two rows are ~10,240 copies each: they are the two branches of one `if is_open` **[U4]** | same text | as above |
| :2540 `next_rule_name = Some(name.clone())` (1.81M) | count 2 from the action's re-publish at :431 | same text; `name.clone()` is one `Arc<str>` bump as today | no publish anywhere |
| :2536 `next.prev_rule = Some(current_rule.snapshot())` (2.14M, link) | LHS `deref_mut(next)` copies `next`, held since :2503 | `current_rule.prev_rule = Some(completed)` after `mem::replace`, then `retire_in_place()` after the event | an 8-byte `Box` move into a field of a `Box<Rule>` that was a loop local nobody pointed at; the retired rule is moved, not cloned |
| :2541 `current_rule.next_rule = Some(current_rule.snapshot())` (2.17M, link) | RHS bumps to 2, LHS copies | `current_rule.next_rule = NextRule::Same` | a 1-byte store with no right-hand side; `Same` resolves to the rule itself, live (TS `rule.next = rule`) |

Also gone with them: the `set_rule` `Rc` clone + drop-previous at all 17 publish sites; `follow_stack` per step; the `make_mut` fast-path check on the ~102,700 non-copying `deref_mut` calls (2.47M — there is no count to test); the `candidate` `Rule` clone at :1729 for declarative-condition grammars; :1870/:1872 `current_rule.o = matched_tokens` (not in the briefing's five, same mechanism, same fix); the `u_mut()` map copies on palindrome (nothing shares a live rule's `u` any more); and on the shipped JSON grammar the :1540 holder that made `@val-bo`'s first write copy.

### 3.4 `rs/src/lib.rs`

- :144-149 callback types: unchanged; :150 `RuleDoneSubscriber` takes `&RuleDone<'_>` (§2). :23/:43 re-exports: add `Ancestors`, `AncestorsIter`, `NextRule`.
- :1332 `parse_budget`, :1344 `parse_budget_ref`: the widened bound (§2). `budget_check_refs` (lib.rs:257, merge.rs:1675, grammar.rs:44) follows the alias with no edit.
- Nothing else: `subscribe_lex/rules/rule_done`, `state_action_ref`, `state_action_with_next_ref`, `alt_*`, `imperative_*`, `map_merge_ref` keep their `impl Fn(&mut Rule, &mut Context, ..)` bounds; `subscribe_rule_done`'s bound becomes `impl Fn(&Rule, &Context, &RuleDone<'_>) + Send + Sync + 'static`, which a closure passed directly infers.

### 3.5 `rs/src/lexer.rs` — no signature changes

The lexer never published (`grep set_rule src/lexer.rs` is empty) and already passes the live pair straight through: `next_raw_for_rule/next_for_rule/relex_for_rule(&mut Rule, &mut Context)` (:470-507), `run_custom_matchers(.., plugin: &mut Option<(&mut Rule, &mut Context)>)` (:342, calling the `ImperativeLexMatcher` at :360), `modify_text_value` (:645-662, `ImperativeTextModifier::run` at :659), `next_raw_with` (:664). `LexSubscriber` is invoked from parser.rs (:680, :713, :1254, :1274, :1301) with `current_rule`/`rule` and `context` — unchanged text. The `standalone: Option<(Rule, Context)>` pair (:28, :627-641) keeps its shape: `Rule` no longer being `Clone` does not matter (it is moved in and out; `Lexer<'a>` does not derive `Clone` — only `LexerState` (:31) and `RelexCheckpoint` (:42) do), and the standalone `Context` has an empty `rs` and `pending: None`, which is what `context.rule == None` meant there today. What *does* change on the lex path is semantics, toward TS: inside a lex subscriber, an imperative matcher or a text modifier, `context.ancestors()` is the live stack (today `rule_stack` was synced at the loop top and was correct there, but `context.rule` was a stale copy after any earlier write in the step). The `ImperativeLexMatcher`/`ImperativeTextModifier`/`MapMerge` aliases in options.rs are untouched.

### 3.6 `rs/src/builtins.rs`, `rs/src/token.rs`, `rs/src/grammar.rs`, `rs/src/merge.rs`

No source edits. `run_builtin_action` (:230) builds its own `Context::new` (empty `rs`, `pending: None`); `token_value` (:180) and `Token::resolve_val` (:617) keep `(&mut Rule, &mut Context)`. grammar.rs/merge.rs carry `BudgetCheck` by alias.

### 3.7 CI **[S7]**

`ci/rust/run.sh:11` runs `cargo test --all-targets --locked`, which **skips doctests**; the two `compile_fail` doctests on `Context::ancestors` need `cargo test --doc --locked` added on the next line (:12, before the clippy line). The Makefile target is **`test-rs`** (Makefile:46-48: `cd rs && cargo test --all-targets` then the clippy line); add `cd rs && cargo test --doc` to it. No new dependency. Revision 2 cited `run.sh:8` and a `rs-test` target; both were wrong.

---

## 4. The cycle plan

There is no `Rc<Rule>`, no `Weak`, and no `upgrade()` anywhere. Every rule-to-rule edge is either an owned `Box` of a rule that will never be written again, or a tag. Link by link:

| edge | representation | set at | owner / direction | cycle? |
|---|---|---|---|---|
| parent → completed child | `child_rule: Option<Box<Rule>>`, holding the **tail** of the child's replacement chain; the pushed child is the chain's head, reached by `Rule::child()` **[P4]** | pop :2602/:2694, forced pops :787, `!pop_until_valid` :768, failed-push recovery :775-782 **[P2]** | the parent owns; set only with a child that has completed, or (on the recovery path) one that never ran | the child cannot own its parent: the parent is `current_rule` or a frame in `context.rs`, neither of which is inside anything the child owns |
| parent → live child | *none stored* (`child_rule = None` at push :2450) | — | the live child is `ancestors()[d+1]`, the callback's `rule`, or `context.pending()` during the pushing pass's after-actions **[P1]**; the walker resolves it and walks its chain to the head (table below) | — |
| child → parent | *removed* (`parent_rule` :2448/:2484/:2499 deleted) | — | `context.parent()` = `rs.last()`; for a retained child, structural: the holder (walker `Via::Child`) | the #177 parent↔child two-cycle is unrepresentable |
| replacement → replaced | `prev_rule: Option<Box<Rule>>` | :2536, before the ruleDone event **[P3]** | the incoming rule owns the retired outgoing one | the outgoing rule's `next_rule` is the tag `Replaced`, never a pointer |
| rule → self (open, no route) | `NextRule::Same` | :2541, :2634 | 1 byte | the #177 `next_rule = self` cycle is unrepresentable |
| rule → parent (pop) | `NextRule::Parent` | :2572, :2664 | resolved to `rs.last()` / the holder's parent | no pointer |
| rule → child (push) | `NextRule::Child` | :2451; the tag is never rewritten at the pop. **It resolves through the cursor's `child` hop (the row below), not through `Rule::child()`** **[T1, Q5]**: when the child has completed that is `child()`, the **head** of its chain; while the child is still LIVE it is the live descendant (`Frame{index+1}`, or `Current`), walked to its chain head; during the pushing pass's after-actions it is `context.pending()`. All three are the rule TS's `rule.next` and `rule.child` name for the pusher (both set at rules.ts:665/:720 to the pushed rule and never relinked; Go rule.go:1266/:1329 likewise) **[R11, P4]** | see the hop table | no pointer |
| outgoing → replacement | `NextRule::Replaced` | :2503 | reachable only through `Prev`/`Chain`: resolves to the holder or the chain successor, and to `None` on a tail **[S12]** | no pointer |
| engine → pending rule | `Context.pending: Option<Box<Rule>>` **[P1]** | moved in before a pushing/replacing pass's after-actions, moved back out after | the `Context` owns it for that window only; the rule holds no link to the `Context` | a `Context` is not a `Rule` and no rule can own one |

`Rc` survives only where it is not a rule: `node: Rc<RefCell<Value>>` (shared parent/child by design, TS parity, no back edge), `o`/`c: Rc<Vec<Token>>`, `n`/`u`/`k: Rc<HashMap>` COW, `name`/`spec`/`next_rule_name: Arc`. None can point at a `Rule` (`Value` has no variant that can hold one, value.rs:96-111).

**Acyclicity by completion order** (doc comment on `child_rule`/`prev_rule`, graft from Design 4): an owning edge is created from a rule R to a rule S only at the moment S completes (or, on the recovery path, at the moment S is abandoned without ever running), while R is still live (R = the parent resuming, or the replacement about to run). Order rules by completion time: every owning edge points at a rule that completed strictly earlier than its holder will. A cycle would need a rule that completed before itself. Tags own nothing. A stronger form of the same argument: a `Box` graph cannot be cyclic without `unsafe`, because building a cycle needs a value to be moved into itself.

**Where a lookup runs on the hot path:** `context.parent()` is `rs.last()` (one bounds check); `context.pending()` is one `Option` read; tag resolution and the chain walk run only in the declarative walker, in `Rule::child()` and in `NextOf` materialisation for bound state actions — none of which runs per token on the benchmark grammars, and the shipped JSON grammar declares no `child.*`/`next.*`/`prev.*` condition (grep of rs/src/lib.rs: none). **Zero upgrades**, so the `DerefMut` doc comment's warning (rule.rs:1236-1238) does not apply.

**Retention bound.** The retained set is {every live rule's completed child chain, transitively} ∪ {every rule's prev chain} — the set today's frozen tree retains — but one 224-byte object per rule instead of a 152-byte snapshot *plus every copy-on-write duplicate*, and `retire()`/`retire_in_place()` strips `child_node` (a deep `Value` clone) and `parent_node` before, or immediately after, retention. A parent that pops a second child **drops the first child's chain**, because `child_rule = Some(..)` overwrites and `child_rule = None` at the next push clears — the same overwrite TypeScript's `rule.child` has; test 10 pins it **[S13]**. Expected peak RSS at or below today's; gated in §8.

**Iterative `Drop`** (a 16K-term adder retains a 16K-long `prev_rule` chain; today the equivalent `Rc` chain drops recursively too — not a new risk, but cheap insurance):

```rust
impl Drop for Rule {
    fn drop(&mut self) {
        let mut pending: Vec<Box<Rule>> = Vec::new();   // no allocation when both links are None
        pending.extend(self.prev_rule.take());
        pending.extend(self.child_rule.take());
        while let Some(mut rule) = pending.pop() {
            pending.extend(rule.prev_rule.take());
            pending.extend(rule.child_rule.take());
        }
    }
}
```

**The walker** (replaces `condition_exists`/`resolve_condition_path`/`snapshot_condition_exists`/`resolve_snapshot_path`, :3387-3534, which exist in duplicate only because `Rule` and `RuleSnapshot` had different link types) **[R1, R10]**:

```rust
#[derive(Clone, Copy)]
struct RuleCursor<'a, 'h> { rule: &'a Rule, via: Via<'a, 'h> }
#[derive(Clone, Copy)]
enum Via<'a, 'h> {
    /// The rule the condition is evaluated on.
    Current { ancestors: Ancestors<'a> },
    /// A frame of the live stack.
    Frame   { ancestors: Ancestors<'a>, index: usize, current: &'a Rule },
    /// `holder.rule.child_rule`: the TAIL of a completed child's chain.
    Child   { holder: &'h RuleCursor<'a, 'h> },
    /// `holder.rule.prev_rule`.
    Prev    { holder: &'h RuleCursor<'a, 'h> },
    /// A member of the `prev_rule` chain whose tail is `tail.rule`: what the
    /// `child` hop yields (the head) and what `next` on a member yields
    /// (its successor).
    Chain   { tail: &'h RuleCursor<'a, 'h> },
}
```

| hop | `Current` | `Frame{index}` | `Child{holder}` | `Prev{holder}` | `Chain{tail}` |
|---|---|---|---|---|---|
| `parent` | `ancestors.last()` as `Frame{len-1}` | `index == 0 ? None : Frame{index-1}` | `holder` | `holder.parent()` (a replacement shares its predecessor's parent, rules.ts:692) | `tail.parent()` |
| `child` | **head of** `rule.child_rule` — `Rule::child()`'s walk **[P4]**: build `tail = Child{holder: self}` on the tail, walk `prev_rule` iteratively to the oldest rule; if that is the tail itself the cursor is `tail`, else `Chain{tail: &tail}` on the head | the live descendant — `Frame{index+1}`, or `Current` when `index+1 == len` — walked to its head the same way (the descendant is the tail of a chain that may still be growing) | as `Current` | as `Current` | as `Current` |
| `prev` | `rule.prev_rule` as `Prev{self}` | same | same | same | same |
| `next` by tag | `Same`→self; `Child`→**this cursor's `child` hop** (the row above), NOT `Rule::child()`; `Parent`→**this cursor's `parent` hop**; `NoRule`→None; `Replaced`→None | same — and for `Child` that means the row above's `Frame` column: the **live descendant** at `Frame{index+1}` (or `Current` when `index+1 == len`), walked to its chain head | same | same, plus `Replaced`→`holder` | same, plus `Replaced`→the chain member whose `prev_rule` is `ptr::eq` to `self.rule`, found by walking forward from `tail.rule`; when that member is `tail.rule` the result is `*tail` (its own tag then resolves as a tail's does); **when `self.rule` IS the tail, no member has `prev_rule` pointing at it and the result is `None` — the forward walk terminates at the tail rather than assuming a successor exists** **[S12]** |

**[T1, Q5] `child()` and `parent()` in the `next` row mean the CURSOR's hops, not the `Rule` accessors, and getting that wrong is a regression.** §3.3 clears `current_rule.child_rule` at the push (replacing parser.rs:2450), so a buried frame's `Rule::child()` is `None` for the entire life of its child (§2 documents exactly that). Read literally as the accessor, `{"parent.next.name": ..}` evaluated on a descendant would resolve to nothing — while `{"parent.child.name": ..}`, one row above, resolves to the live child — an asymmetry no port has. Today's Rust resolves `parent.next` to the pushed child: parser.rs:2451 stores `current_rule.next_rule = current_rule.child_rule.clone()` before `stack.push(current_rule)` at :2486, and `resolve_condition_path`'s `"parent"` arm (parser.rs:3414-3417) hands the live `&Rule` frame to itself, whose `"next"` arm (:3450-3458) reads that push-time snapshot. TypeScript resolves it to the live child (`next = rule.child = makeRule(..)`, rules.ts:665, `rule.next = next` at :720, never relinked at :713); Go likewise (go/rule.go:1266-1267). Routing `Child` through the `child` hop restores all three to the same answer at **zero cost** — it is the code path the `child` hop already runs, and neither benchmark grammar nor the shipped JSON grammar declares a `child.*`/`next.*` condition. Pinned by the live-child rows added to test 7 **[T2]**.

Two cases of that row spelled out, because they are the ones that are easy to get wrong:

* **`Current` with tag `Child` and `child_rule == None`.** Reachable only inside the after-actions of a pushing pass (the tag is set at :2451, the child is parked as `context.pending()`) or after a recovery that dropped an unrun child (§3.3). The declarative walker is not reachable from an after-action today — conditions are evaluated during alternate matching, before routing — but if it ever becomes reachable there, the `Child` resolution must read `context.pending()`, which is the only place the rule exists. Everywhere else `Current` + `Child` means the child has completed, and `child()` is correct.
* **`Frame{index}` with tag `Child`.** The ordinary case for every ancestor of the live rule: the child is live, `child_rule` is `None`, and the answer is the live descendant, exactly as for the `child` hop.

**[S12]** The tail-with-a-`Replaced`-tag case is reachable: a rule retired by recovery mid-replace (`attempt_recover` :768, or the forced-pop loop) carries the tag from a replacement that never ran. `None` is the honest answer there — the named rule does not exist — and it matches the `NoRule` reading a fresh rule gives.

The holder references point into the caller's stack frame (the `'h` lifetime; covariance lets a `RuleCursor<'a,'h>` be held by a shorter-lived child cursor). Recursion depth equals the path length written in the grammar; the chain walks are iterative loops over `prev_rule`, so a `child.*` lookup on a parent whose child replaced itself 16K times is a 16K-step loop, not 16K stack frames. **Cost: O(replace-chain length) per `child.*` hop and per `Replaced` resolution from a chain member, in the declarative walker and in `Rule::child()` only** — no benchmark grammar and no shipped grammar declares one. The existing "next names self" fallback (:3407/:3453/:3482/:3525) becomes `NextRule::Same`, which also removes today's ambiguity when a rule pushes a rule of its own name; a fresh rule (tag `NoRule`) resolves `next.*` to `None` as today.

Why the head and not the tail: TS assigns `rule.child` once, at push (`next = rule.child = makeRule(..)`, rules.ts:665) and never at pop (:713 only reads `ctx.rs`); Go likewise (`r.Child = next`, rule.go:1266; the pop at :1312-1318 relinks nothing); `ts/test/child-pusher.fixture.json` with `ts/test/builtins.test.js:595` ("a replacement does not become the pusher's child") pins it, and `go/doc/differences.md:318-322` records "making a parent's `Child` follow the replacement chain forward" as a fix that was tried and is WRONG. Today's Rust (parser.rs:2602/:2603) stores the last replacement — an undocumented divergence — and this PR ends it for the declarative `child`/`next` views and for `Rule::child()`.

**What this PR does not change in that area (recorded, not silently traded):** `accept_child_node(&completed)` at :2601 copies the chain **tail**'s node into `child_node`, which `@capture$` (builtins.rs:283-287) reads, whereas TS's `@capture$` reads `rule.child.node`, the head's. That is a same-input-different-output divergence in Rust today on `child-pusher.fixture.json` (TS: a `mid` kid; Rust: an `alt` kid), unpinned because the Rust suite runs only `push-replace.fixture.json` (grammar_spec_test.rs:505). This PR freezes parse results, so it does not touch `accept_child_node`; it makes the repair a one-liner (`self.child().unwrap_or(child).node`, now that the head is retained and named) and the follow-up is: add `child-pusher.fixture.json` to grammar_spec_test.rs and fix `accept_child_node`, or register the divergence per AGENTS.md:434-444 (`DIVERGENCE.md` entry plus a `notRegistered` exemption in `go/divergent_test.go`, since a fixture case cannot be a `divergent.tsv` row). Cited so the "ports agree after the change" claim in §10 is scoped exactly.

grammar_spec_test.rs:190-230 resolves identically to today: `child.name`/`child.parent.name` on `top`'s close pass go `Current → child (chain of length 1, head == tail) → parent = top`; `next.name`/`next.node.kind` on `top` resolve `Child` → the completed `child` (which shares `top`'s node cell); `prev.name` on `next` is the retired `top`; `parent.name` on `child` is `ancestors.last()`.

---

## 5. The `resolve_val` plan

#177 found that dropping the top-of-loop publish was unsafe because `Token::resolve_val` (token.rs:617-621 → `TokenValFunc::call`, :128) runs a plugin callback with no `set_rule` on its path: from `builtins::token_value` (builtins.rs:180-184, reached through `run_action_with_config` at parser.rs:431, whose publish precedes the builtin's own `rule.node = ..` writes at builtins.rs:264/278/291, so the published copy was already stale when the lazy value ran), and from the public `Rule::resolve_open_value/resolve_close_value` (rule.rs:1441-1454), callable from any callback. That is a hazard of *having a published copy that some path forgets to refresh*.

This design has no published copy, so the hazard class does not exist rather than being patched. On every one of those paths the lazy callback receives (a) `rule`: the very `&mut Rule` its caller holds, which is the engine's current rule whenever the caller is a callback that received it — there is no other `Rule` it could be; and (b) `context.ancestors()`: the engine's stack, which cannot be out of date because it is not a copy — there is no sync step a path could omit. A lazy value resolved from inside an after-action of a pushing or replacing pass additionally sees `context.pending()` **[P1]**, for the same reason and with the same read-only guarantee. The standalone lexer path (lexer.rs:627-641) passes its own `#NORULE` rule and a fresh `Context` with `rs` empty and `pending: None`, which is what it does today; the public `run_builtin_action` (builtins.rs:230) builds its own `Context` likewise. Signatures of `resolve_val`, `TokenValFunc`, `with_lazy_value`, `token_value` and `resolve_*_value` are unchanged.

Pinned by three assertions in `callback_surface_test.rs` (§6): the lazy callback's `rule.u_mut()` write is visible to an `ac` state action on the same rule (the write went to the engine's rule); a lazy value three rules deep records `context.ancestors()` names equal to the `error.rule_stack[..len-1]` the engine reports for an error raised from the same callback (the callback's view and the engine's bookkeeping are the same data — the check #177 could only do by audit); and `ancestors().len() == rule.d` inside the lazy callback.

---

## 6. The test plan

Parity contract: all 270 `#[test]`s in `rs/tests` (37 files) and every `test/spec/*.tsv` runner pass at every commit; json (21), debug (42) and directive suites pass against the branch with the §7 edits. No test is skipped or weakened; the four below are re-expressed and every behaviour they protect is re-asserted. **Maintainer ratification is required before the branch is opened** for the two re-expressions marked ★ (fatal flaw 3, §0.4 R1/R2) and for the **seven** contract decisions listed in §0.4 / §8 Step 0 (R3-R9; R8 is new in revision 4 **[Q2]** and extended in revision 5 **[V1, V3]**, R9 is new in revision 5 **[V2]** and is a second capability removal).

**Every `rs/tests` site that reads `context.rule` or `rule_stack`, and what happens to it:**

| file:line | today | change |
|---|---|---|
| `callback_surface_test.rs:32` | `context.rule.as_ref().map(\|r\| r.name.clone())` in the lazy callback | tuple's fourth element becomes `(rule.i, context.ancestors().len())`, expected `(0, 0)` (`val` is the start rule); add `assert_eq!(context.ancestors().len(), rule.d)`; add an `ac` state action on `val` asserting `rule.u["lazy-ran"] == Bool(true)`. Lines 24-31 and the `LAZY` result unchanged |
| `callback_surface_test.rs:84` | `error.rule_stack` | unchanged (built from the live stack at parser.rs:493) |
| `introspection_test.rs:263-273` | `context.rule` name / `o.first()` | `rule.name.to_string()` / `rule.o.first()` — the same six observations, values unchanged |
| `introspection_test.rs:275` | `context.rule_stack.iter()` | `context.ancestors().iter()` |
| `introspection_test.rs:369-457` | `rule_stack` frames' `u["mark"]` down and up | `context.ancestors()`; assertions unchanged; doc comment now says it holds by construction |
| `introspection_test.rs:495-560` ★ | callback **writes** `context.rule_stack.reverse()` | Cannot be expressed: the stack is the engine's own data. Re-expressed as `a_callback_mutating_everything_it_can_reach_does_not_derail_the_parse`: the action writes `rule.u_mut()`, `rule.n_mut()`, `rule.k_mut()`, `context.u`, `context.meta`, `context.t`, `*rule.node.borrow_mut()` and `rule.set_node(..)`; assert the parse succeeds, `seen.len() == 3`, and the ancestry on *each* call is exactly `["top"]`, `["top","node"]`, `["top","node","node"]` — strictly stronger than :559's `seen[0] == "top"`, which the old test could not assert because the view was writable. The "cannot write" half becomes the two `compile_fail` doctests on `Context::ancestors` (§2, run via `cargo test --doc`, §3.7). **§0.4 R1** |
| `budget_test.rs:18` | `parse_budget(2, move \|context\| ..)` | `move \|_rule, context\|` (BudgetCheck widening); :27 `error.rule_stack` unchanged |
| `parse_hook_ref_test.rs:74` | `parse_budget_ref("@stop", move \|_context\| ..)` | `move \|_rule, _context\|` |
| `no_panic_test.rs:269` | `parse_budget(1, \|_\| panic!(..))` | `\|_, _\|`; all its `error.rule_stack` assertions (:29, :44, :168, :237, :296, :311) unchanged |
| `state_action_ref_test.rs:104/:131`, `callback_test.rs:406` | `error.rule_stack` | unchanged |
| `api_test.rs:311-348` ★ | copy-on-write of `Rule::snapshot()` | Tests a mechanism that no longer exists. Re-expressed as `a_rule_is_one_object_for_its_whole_life` — the invariant, not the symptom: a grammar with bo/ao/bc/ac state actions, a `c_fn`, a rule subscriber, a ruleDone subscriber and a pushed child; every callback records `rule as *const Rule as usize` with `rule.i`; the child's action records `context.ancestors().last()` as `*const Rule`; assert one address per `i` across bo, c_fn, ao, subscriber, the child's ancestor view, bc, ac and ruleDone; plus a `bo` write `rule.u_mut().insert("stamp", i)` read back identically in `c_fn`, `ao`, the child's `ancestors().last().u`, and `ac`. The representation invariants move to test 12. **§0.4 R2** |

**Unchanged tests that exercise the new representation without edits:** `callback_test.rs:101-130` (`next.name == rule.name` in bo via `Named → state_actions` — the lazy `to_snapshot()`; `"tail"` in ao via `NextOf::Pending` materialised from `context.pending()`), `callback_test.rs:166-175` (`rule.ao = false` through `DerefMut`), `imperative_plugin_test.rs:160-170` and `introspection_test.rs:133-135` (`Rule::new` + `n_mut`), `grammar_spec_test.rs:190-230` (walker paths, §4), `rewind_test.rs` (all), `imperative_plugin_test.rs:185` (`MapMerge`), all 17 `*rule.node.borrow_mut() = ..` sites.

**New tests** (a new `rs/tests/callback_contract_test.rs` unless noted):

1. `every_mut_rule_callback_sees_the_stack_its_rule_sits_on` (graft, Design 3) **[R17]**: one grammar registering a rule subscriber, a lex subscriber, an `ImperativeLexMatcher` (lexer.rs:360 path), an `alt_condition`, an `add_action`, all four `state_action_with_next_ref` phases, an `AltModifierWithMatch`, a lazy token value, a budget check (`check_every_n = 1`) and a ruleDone subscriber; each `&mut Rule` callback asserts on every invocation `context.ancestors().len() == rule.d` and `ancestors().iter().enumerate().all(|(k, f)| f.d == k)` — **including the `ao` of the pushing pass and the `ac` of the popping pass**, which is Rust's contract, not TypeScript's (TS moves `ctx.rs` before the after-actions, rules.ts:662/:713 vs :724-733; Go rule.go:1258-1263/:1312-1314 vs :1332-1339; see §10) — records `rule as *const Rule`, and the test asserts one address per `i` across every kind. The ruleDone subscriber asserts test 3's post-transition shape instead.

   **[P1, V2]** The same test pins the pending rule for **every after-action kind that receives a `Context`**, which is the capability revision 2 dropped: on the pushing pass the grammar binds (a) an `ao` `ContextAction` via `add_action`, (b) an `ao` `Named` action registered with `action_with_context` (lib.rs:882-888), so it resolves through `self.context_actions` in `run_action_with_config`, and (c) an `ao` state action. (a) and (b) assert `context.pending().unwrap().name == "child"` and `.d == rule.d + 1`; (c) asserts the same through `context.pending()` **and** that its `next` argument carries the same `name`/`d`/`i`. On the replacing pass the three assert `context.pending().unwrap().name == "<replacement>"` and `.d == rule.d`. Every other callback in the test asserts `context.pending().is_none()` — including the `bo`/`bc` phases, the conditions, the ruleDone subscriber and the lazy value — which pins the window. A further `ao` `ContextAction`, registered before (c) in `ao_order`, writes `rule.u_mut()` and asserts it cannot reach the pending rule as `&mut` (it has only `&Rule`), which is the compile-level half.

   **The fourth kind cannot, and the test records that rather than asserting it away [V2].** A fifth binding — an `ao` `Named` action registered with the plain `tabnas.action(name, |rule| ..)` (`Action`, lib.rs:144), which resolves through `self.actions` and receives **no `Context`** — has nothing to call `context.pending()` on. It asserts what it can still see, `rule.next_rule_name == Some("child")` and `rule.next_rule == NextRule::Child`, and its doc comment names **§0.4 R9** and the migration row in §7. Revision 4's version of this test asserted `pending()` from "an `ao` `Named` action that is NOT a state action", which is unwritable for this registration. The same test also pins the resolution ORDER `run_action_with_config` uses (parser.rs:424-446: builtins, then `actions`, then `context_actions`) by registering one name both ways and asserting the context-less one runs — that order is what decides whether a plugin's migration to `action_with_context` takes effect at all. If the maintainer takes option (b) of R9, this binding becomes a fourth `pending()` assertion instead and the order case stays.
2. `a_lex_subscriber_write_is_visible_to_the_matcher_and_condition_of_the_same_step` (graft, Design 3): the lex subscriber writes `rule.u_mut().insert("lexed", ..)`; the imperative matcher on the *next* token and the alternate's `c_fn` in the same step read it back — the staleness class that `context.rule` had on the lex path.
3. `rule_done_sees_the_stack_after_the_transition_and_the_rules_it_moved` (graft 2, matching TS `ctx.rs[0..rsI]` after `ctx.rs[ctx.rsI++] = rule` / `ctx.rs[--ctx.rsI]`, parser.ts:267-285) **[R13, P3]**:
   - **push**: `ptr::eq(context.ancestors().last().unwrap(), rule)`, `ancestors().len() == rule.d + 1`, `rule.child_rule.is_none()` (structural: the child is the live rule — §10 differs-list item 5), `done.next.unwrap().name == <pushed name>` and `done.next.unwrap().d == rule.d + 1`, `ptr::eq(done.parent.unwrap(), &ancestors()[rule.d - 1])` (root: `done.parent == None`); the child's first rule-subscriber call then sees the address recorded from `done.next`.
   - **replace**: `ancestors().len() == rule.d`, `done.next.unwrap().name == <replacement>`, and — now asserted, not exempted **[P3]** — `ptr::eq(done.next.unwrap().prev_rule.as_deref().unwrap(), rule)`, which is TS's `prev.next.prev === prev` (rules.ts:693). Also `done.next.unwrap().prev_rule.as_deref().unwrap().child_node` is still the completed rule's own (`retire_in_place` runs after the event).
   - **pop**: `ancestors().len() == rule.d - 1`, `ptr::eq(done.next.unwrap(), done.parent.unwrap())`, and — now asserted **[P3]** — `ptr::eq(done.parent.unwrap().child_rule.as_deref().unwrap(), rule)`; the resumed parent's next rule-subscriber call sees that address.
   - **self**: `ptr::eq(done.next.unwrap(), rule)`. **root's final pass**: `done.next == None && done.parent == None`.
   - **root's final pass, and the parse-result freeze [U2]**: that same subscriber writes `*rule.node.borrow_mut() = Value::String("tampered".into())`, and the test asserts `parse()` still returns the value the root held when the pass completed. The POP arm's `None` branch reads the result **before** the event (§3.3), as parser.rs:2607 does before :2610, so the write lands on a cell nothing reads again. With the two statements in the other order this assertion fails and §10's "no parse-result change" would be false for any grammar with a ruleDone subscriber.
   Today's `rule_stack` (synced only at the loop top) gave the pre-transition stack in the push and pop cases; the change is toward TS and is noted in the changelog (**§0.4 R3**). `error.rule_stack` for an error raised *from* a ruleDone subscriber is unchanged (`ancestors_for` still strips the frame).
4. `a_lazy_value_three_rules_deep_sees_the_engines_own_stack` (§5).
5. `after_close_actions_run_before_the_pop_and_see_the_parent_as_next` **[R17]**: an `ac` state action asserts `next.name` is the parent's name **and** `context.ancestors().last().name` is the same — the pop happens after the after-actions (:2573 → :2598). This pins Rust's order; TS pops first (`next = ctx.rs[--ctx.rsI]`, rules.ts:713) so its `ctx.rs[0..rsI]` in the same `ac` no longer holds the parent. The test's doc comment says so and points at §10 (**§0.4 R4**).
6. `the_invariant_holds_through_forced_close_recovery` (graft, Design 3): a recovery-enabled grammar driving the multi-frame forced pop (parser.rs:784-796; nesting shape from no_panic_test.rs:217-237); a rule subscriber asserts `ancestors().len() == rule.d` and root-first `d == k` on every step including the resumed parent; after recovery `child.name` on the resumed parent resolves to the abandoned child (retired at :768/:787) and `next.name` reads the same (tag `Child` → `child()`); the forced-close ruleDone events carry `done.forced`; `done.next` for the **failing** rule is what `forced_next` resolves — the unrun pending rule when the failed pass had parked one, else its own tag (`Same` → itself, `Child` → its completed `child()`, `Parent` → `context.ancestors().last()`), and `None` only for `NoRule`/`Replaced` **[V3]**, where revision 4 hard-coded `None` for every case; and — **[P4]** — `ptr::eq(done.next.unwrap(), rule.child().unwrap())` for a resumed parent (the **head** of the chain, where revision 2 asserted `child_rule.as_deref()`, the tail; the two differ as soon as the abandoned child had replaced itself once, which this grammar arranges). Every forced-close event additionally asserts `context.ancestors().len() == rule.d` and `ptr::eq(done.parent.unwrap(), context.ancestors().last().unwrap())` — the first event fires before any pop, each later one after its own pop, and both land on the same invariant (§2) **[V3]**.

   **[P2, Q1, T4, Q6, Q2]** **Five** cases are added to the same file. The first four are driven by an after-action that returns an error token on a pass that routes; the fifth pins the tag-based half of the `done.next` table:
   - a failed **push** after-action with `accepts_close` true (parser.rs:775-782): the error-pass ruleDone carries `done.next.unwrap().name == <pushed name>` and `done.next.unwrap().i` equal to the id the pushing pass allocated; after recovery, on the resumed rule, `child.name` and `next.name` both resolve to that unrun rule, and `rule.child().unwrap().o.is_empty()` (it never ran). This is TS's behaviour (rules.ts:720 before :728; :1183-1193 returns the same rule with its links intact).
   - a failed **replace** after-action with `accepts_close` true: `done.next` is the unrun replacement — **the assertion revision 3's code could not satisfy [Q1]**, because `match (current_rule.next_rule, pending.take())` emptied the caller's `Option` on every arm and dropped the box inside the `(Replaced, Some(_))` arm, so `recover_error_pass` saw `None` in both `pending` and `current_rule.child()`. §3.3 now takes the pending only in the arm that retains it. After recovery `rule.next_rule == NextRule::NoRule`, `next.*` resolves to nothing, `rule.next_rule_name` still names it, and `child.*` is untouched (whatever the rule had pushed earlier). §10 item (3); **§0.4 R5**.
   - **[T4, Q6]** a failed **push** after-action with `recover.pop_until_valid = false`, so recovery takes the `!pop_until_valid` arm (parser.rs:768-770) instead: the parent is popped and becomes current, the failed rule is retired onto its `child_rule`, and the unrun child **cannot** be retained because that field is taken. Assert: the error-pass ruleDone still carries `done.next.unwrap().name == <pushed name>` (reported once, from the still-owned local) **and `done.parent.unwrap().i` equal to the resumed parent's** — which only the **[V1]** normalisation can produce, because the frame at `d - 1` is already off the stack by the time `done_parent` is read; and after recovery the abandoned rule — reachable as `rule.child()` on the resumed parent — has `next_rule == NextRule::NoRule` and `next_rule_name == Some(<pushed name>)`, so `child.next.*` resolves to nothing rather than to a rule that no longer exists or to an older child of the same rule. Revision 3 asserted the general claim that a failed PUSH is never a divergence; this case is the counter-example, and §10 item (3) now records it. **§0.4 R5**.
   - **[T4, Q6]** the same shape driven into the **forced-pop loop** (`pop_until_valid = true`, `accepts_close` false on every frame): assert the first rule the loop moves — the one that failed — carries `next_rule == NextRule::NoRule` after `clear_unrun_link`, while each ancestor the loop retires keeps `NextRule::Child` naming the child it really pushed, so `done.next` on each forced close is that ancestor's `child()` and not the dropped unrun rule. The event for the rule that FAILED carries `done.next` = the unrun pending rule **[V3]**. A fourth assertion pins the residue: when the loop pops **past** the immediate parent, the error-pass event that follows has `done.next == None` **and** `done.parent == None`, because `parent_i` matches neither the (gone) frame at `d - 1` nor the rule the loop stopped on — §10 item (7), and the one case in this area where TypeScript still names a rule and Rust cannot.
   - **[Q2]** two cases that fail a pass which routes **nowhere**, so the tag half of §2's `done.next` table is pinned: (a) an `ao` action returning an error token on an **open pass with no route** — `ptr::eq(done.next.unwrap(), rule)` in the error-pass ruleDone, since both are `&event_rule` and TS gives `prev.next === prev`; (b) an `ac` action returning an error token on a **close pass with no route**, with `accepts_close` true so the rule stays current — `done.next` is the resuming parent and `ptr::eq(done.next.unwrap(), done.parent.unwrap())`, which is TS's `prev.next` after `ctx.rs[--ctx.rsI]`. A third sub-case runs (b) with `pop_until_valid = false` so recovery pops to the parent: `done.next` is then `&**current_rule`, still the same rule by `i`, which is what the `parent_i` capture in §3.3 exists for. Revision 3 reported `None` for all three. **[V1]** All three sub-cases also assert `done.parent`, and the third asserts `ptr::eq(done.next.unwrap(), done.parent.unwrap())` — under revision 4's code that sub-case reported `done.next = <the parent>` with `done.parent = None` for the same rule, which no port does and which §2's own doc forbade.
7. `rs/tests/rule_links_test.rs` (graft, Design 2) **[R1, R10, R12, P4]**: the grammar below (fixed tokens `a`–`e`, `x`) exercises push, a nested push, a two-step replace chain and both pops; every expected value is stated **per port**, derived from ts/src/rules.ts:665-666 (push), :692-693 (replace), :713 (pop), :720 (`rule.next`) and go/rule.go:1266-1267/:1292-1293/:1329 — and, before the pin lands, confirmed by running the same grammar once in the TS suite as `ts/test/rule-links.fixture.json` + a case in `ts/test/builtins.test.js` next to `:595` (not `counter.test.js:217`, which only validates path *names*). Rust-after must equal TS on every row; "Rust today" is quoted to show what the test pins.

   **[T2] Rows evaluated while a child is still running are mandatory, not optional.** A grep of `rs/`, `ts/`, `go/` and `test/spec/*.tsv` finds **no existing case anywhere that uses `parent.next`**, and revision 3's table contained no row where the named rule was still live — so the `Child`-tag regression of **[T1]** was invisible to the entire plan. The `child` open/close rows below are what make it visible: under revision 3's wording they resolve to nothing, under §4's corrected wording they resolve to the live child, and TypeScript and Go resolve to the live child. They must be in `ts/test/rule-links.fixture.json` as well, because the fixture is what establishes the TS column.

   ```text
   top   open : s a, p child                     close: s e, c {…}     (then pop → root done)
   child open : s b, p leaf                      close: r child2       (empty s: matches on the way out)
   leaf  open : s x                              close: (none → pop)
   child2 open: s c, r child3
   child3 open: s d                              close: c {…}          (no route → pop)
   ```
   Input `abxcde`. Conditions and expected strings:

   | evaluated on | path | TS / Go | Rust after | Rust today |
   |---|---|---|---|---|
   | `child3` close pass | `parent.name` | `top` | `top` (`ancestors.last()`) | `top` |
   | | `prev.name` | `child2` | `child2` | `child2` |
   | | `prev.prev.name` | `child` | `child` | `child` |
   | | `prev.next.name` | `child3` (`child2.next` = its replacement, :720) | `child3` (`Replaced` on `Prev` → holder) | `child3` |
   | `child` **open** pass (its child `leaf` is LIVE) **[T2]** | `parent.next.name` | **`child`** (`top.next` is the live child object, :665/:720) | **`child`** (`Frame{0}` + tag `Child` → live descendant → `Current`) | `child` (the push-time snapshot) |
   | | `parent.next.state` | `o` (live) | `o` (live) | `o` (frozen at creation — agrees here by luck) |
   | | `parent.child.name` | `child` | `child` (`Frame{0}` `child` hop → live descendant) | `child` |
   | `child` **close** pass (`leaf` has popped, `child` is about to replace itself) **[T2]** | `parent.next.name` | `child` | `child` | `child` |
   | | `parent.next.state` | **`c`** (live) | **`c`** (live) | **`o`** (frozen at creation) — the same divergence test 8 pins for `next.state`, seen one hop away |
   | | `parent.child.name` | `child` | `child` | `child` |
   | | `prev.prev.next.name` | `child2` | `child2` | `child2` |
   | | `prev.parent.name` | `top` | `top` (`Prev.parent` = holder's parent) | `top` |
   | | `prev.prev.child.name` | `leaf` (`child.child`, set at push :665) | `leaf` | `leaf` |
   | | `prev.prev.child.parent.name` | `child` | `child` (`Child.parent` = holder) | `child` |
   | | `next.name` | `child3` (`child3.next = child3` from the open pass, :720) | `child3` (`Same`) | `child3` |
   | | `next.state` | `c` (live) | `c` (live) | `o` (frozen) — test 8 |
   | `top` close pass | `child.name` | **`child`** (assigned once at push, :665) | **`child`** (head of the chain) | `child3` (tail, :2602) |
   | | `child.parent.name` | `top` | `top` | `top` |
   | | `child.child.name` | `leaf` | `leaf` | *(none: `child3.child_rule` is `None`)* |
   | | `child.child.parent.name` | `child` | `child` | *(none)* |
   | | `child.next.name` | `child2` | `child2` (`Chain` successor) | *(none: tail's tag is `Parent` → `top`, i.e. `top`)* |
   | | `child.next.next.name` | `child3` | `child3` | *(none)* |
   | | `child.next.next.next.name` | `top` (`child3.next` = the popped-to parent, :713/:720) | `top` (tail's `Parent` → `Child.parent` = holder) | *(none)* |
   | | `next.name` | **`child`** (`top.next` set at :720 during the push pass, never relinked) | **`child`** (`Child` → `child()`) | `child3` (:2603) |
   | | `next.next.name` | `child2` | `child2` | *(none)* |
   | `ao` state action, `top` open (push) | `next.name` | `child` | `child` (`NextOf::Pending` ← `context.pending()`) | `child` |
   | `ao` `ContextAction`, `top` open (push) **[P1]** | `context.pending().name` | `rule.child.name` = `child` | `child` | `rule.child_rule.name` = `child` |
   | `ao` state action, `child2` open (replace) | `next.name` | `child3` | `child3` | `child3` |
   | `ac` state action, `child3` close (pop) | `next.name` | `top` | `top` (`NextOf::Parent`) | `top` |

   **[P4]** The same file asserts `Rule::child()` directly alongside the declarative rows, in a ruleDone subscriber on `top`'s final pass: `top.child().unwrap().name == "child"`, `ptr::eq(top.child().unwrap(), <the head of the chain reached by hand from child_rule through prev_rule>)`, and `top.child_rule.as_deref().unwrap().name == "child3"` (the tail — the field is not the accessor, and the migration table says so). The "replaced-successor-itself-replaced" case is the `prev.prev.*` and `child.next.next.*` rows. Note for TS readers: TS's `child.state` on `top`'s close pass is the head's state (`c`) and Rust-after gives the same; the head's `u`/`n`/`o` are the head's own, retained in the chain.
8. `a_close_pass_next_state_condition_reads_the_live_rule`: `{"next.state": "c"}` on a close alternate of a rule that went open→close with no route — true in TS (`rule.next === rule`, live; `getpath`, ts/src/utility.ts:1207) and Go (`r.Next`, go/rule.go:713-714), false in Rust today (frozen at :2541), true after. **Not a TSV fixture**: `test/spec/*.tsv` rows are `input → expected` under the strict-JSON grammar and cannot carry a grammar; the pin lives in `grammar_spec_test.rs` with sibling assertions added to the TS suite (in the `rule-links` fixture/test of test 7 — `ts/test/counter.test.js:217` only validates condition-path names and is not the place) and to `go/` so all three ports assert it. No `DIVERGENCE.md` entry: after the change the three ports agree on this value (the one parse-result divergence that remains in this area is `accept_child_node`'s, §4, untouched).
9. `a_directly_driven_lexer_with_an_imperative_matcher_still_works` (graft, Design 3): the standalone `Lexer` path (lexer.rs:627-641) with an `ImperativeLexMatcher` that writes `context.u`; a second `next_raw_token` call sees it. It also asserts `context.pending().is_none()` and `context.ancestors().is_empty()` there.
10. `completed_rules_stay_reachable_and_everything_is_freed` (graft, Designs 3/4) **[R3, S13]**: `Value` (value.rs:96-111) has no variant that can host a user `Drop` type, so retention and release are observed through the node cells instead. A `bo` `ContextAction` on every rule calls `rule.set_node(Value::Number(rule.i as f64))` so each rule owns a distinct cell (a pushed rule otherwise shares its parent's), then records `(rule.i, Rc::downgrade(&rule.node))` into a `thread_local! { static CELLS: RefCell<Vec<(usize, Weak<RefCell<Value>>)>> }` (`Weak<RefCell<Value>>` is `!Send`, so it cannot ride in the `Send + Sync` closure; a thread-local is per test thread and is cleared at the start of the test).

    **The grammar is a linear nest: each rule pushes AT MOST ONE child, N of them replace themselves, M of them push.** That shape is required, because `current_rule.child_rule = Some(..)` at each pop and `child_rule = None` at each push **overwrite** — a parent that pops a second child drops the first child's chain, exactly as TypeScript's `rule.child` does. A counted retention assertion over an arbitrary grammar would fail on that, not on a bug. **[S13]**

    In the ruleDone subscriber for the root's final pass (`done.next.is_none() && context.ancestors().is_empty()`) — the only external handle to the completed tree, since `context.rs` is `pub(crate)` and nothing survives `parse()` — walk `child_rule` and then every `prev_rule` chain from the `rule` argument, count N retired-by-replace and M retired-by-pop rules, and assert every recorded `Weak` for those rules `upgrade().is_some()` (reachable and alive). The grammar additionally contains **one rule that pushes twice**; inside that same ruleDone, assert `upgrade().is_none()` for the first child it pushed — so the test pins the overwrite rule instead of failing on it **[S13]**. After `parse()` returns and the result `Value` is dropped, assert every recorded `Weak` (root included) `upgrade().is_none()`. A node cell is held only by the `Rule`s that own it (`data.node`, and a live child's `parent_node`, stripped by `retire()`/`retire_in_place()`) and by `context.root`, which dies with the context; a `Value` cannot hold one. So a cell that is gone is a rule that was dropped, and **"no `Rc` cycle retains a rule" means exactly this: every rule's node cell is unreachable once the parse is over, asserted without `Rc<Rule>` counts because there is no `Rc<Rule>`.**
11. `writing_u_never_copies_the_map_when_nothing_shares_it` (graft, Design 2's counter idea, `u` only — `n`/`k` are legitimately shared with a pushed child, parser.rs:2446-2447/:2500-2501) **[P5, S3]**: `#[cfg(debug_assertions)] #[doc(hidden)] pub fn debug_u_copies() -> usize` backed by a thread-local incremented in `u_mut()` **only when `Rc::strong_count(&self.u) > 1 && !Rc::ptr_eq(&self.u, &empty_values())`** (§3.2, re-keyed by **[Q7]**); a palindrome-shaped grammar parses with the counter at 0. The test's doc comment records why the second clause is there: `empty_values()` (rule.rs:1283-1290) hands every rule one thread-local `Rc`, so the count alone is `> 1` on the first `u_mut()` of every rule ever created, and the palindrome grammar writes `rule.u_mut()` on every step — revision 2's form could not read 0 and would have failed for a reason that has nothing to do with the design. A second case in the same test asserts the counter still detects a real sharer, and **the order of its three steps is load-bearing [Q7]**: the `bo` action (1) writes `rule.u_mut().insert(..)` FIRST — an uncounted copy off the shared empty sentinel, skipped under either keying — then (2) takes and keeps a `to_snapshot()`, which now shares the rule's OWN map; the matching `ao` then (3) writes `rule.u_mut()` again, which is the counted copy, and the test asserts `debug_u_copies() == 1`. Revision 3 specified steps (2) and (3) only, so the map was still `empty_values()` at snapshot time, the write took the skipped branch, and the case read 0 — failing for a reason that has nothing to do with the design, which is exactly what the first case was written to avoid. §0.2 item 11 records why re-keying the counter alone does not rescue it: the snapshot holds a clone of the same sentinel `Rc`, so `!Rc::ptr_eq(&self.u, &empty_values())` skips that write too.
12. `api_test.rs` **[R9]**: `a_rule_is_one_object_for_its_whole_life` (above) and `a_rule_embeds_its_record_and_links_are_a_box_or_a_byte`, which asserts the invariants the record layout is supposed to have, not a slack constant: `size_of::<Rule>() >= size_of::<RuleSnapshot>()` (the record is inline; `ptr::eq(&*rule, ..)` against the record is not expressible, this is the expressible half), `size_of::<Option<Box<Rule>>>() == 8` (the niche: a link is one pointer), `size_of::<NextRule>() == 1`; and that `RuleSnapshot` carries no link field, pinned as an exhaustive destructuring with no `..` — `let RuleSnapshot { i, d, name, spec, state, bo, ao, bc, ac, need, node, next_rule_name, n, u, k, o, c } = rule.to_snapshot();` — which stops compiling the day a field is added or a link comes back. The revision-1 `+ 64` would have broken silently the first time `Value` changed width and protected nothing.

13. `the_next_argument_is_materialised_at_the_first_bound_state_action` **[U1, S8]** (new in revision 5). The `[S8]` ordering change for `ao`/`ac`, and the same change for `bo`/`bc` that **[U1]** uncovered, are announced in §10 and pinned by nothing in revisions 1-4. One grammar, four phases: in each of `bo`, `ao`, `bc`, `ac` the rule binds a `ContextAction` **declared first** and a `StateAction` **declared second**, so `resolved_action_order` (rule.rs:1054-1071) keeps that order on the `order_matches` path and the fallback path puts the callback first anyway (rule.rs:1062-1070). The `ContextAction` writes `rule.need = 7` and `rule.u_mut().insert("mark", Value::Number(1.0))`; the `StateAction` asserts on its `next` argument:
    - `bo` and `ao` of an **open pass with no route** (`NextOf::Current`): `next.unwrap().need == 7` and `next.unwrap().u["mark"] == 1` — today both read the pre-action values, because :1540 and :257 take the copy before the phase's first action. This is the assertion that fails on `main` and passes on the branch, in both phases.
    - `ao` of a **pushing** pass (`NextOf::Pending`) and `ac` of a **popping** pass (`NextOf::Parent`): the same `ContextAction` writes, and `next` must be **unchanged** by them — it describes the pending rule or the parent, neither of which the action can reach as `&mut` (§0.2 item 1). This is the half of the change that is *not* observable, asserted so that a later "optimisation" that made `next` alias the current rule would fail.
    TypeScript is the reference for the before phase and reflects every earlier write, because it passes the live rule (`let next = is_open ? rule : ctx.NORULE`, ts/src/rules.ts:535, handed to `befores[bI].call(this, rule, ctx, next, bout)` at :564); **Go has no `next` argument for before actions at all** (`action(r, ctx)`, go/rule.go:1116-1121), so there is no Go column for the before rows. Rust after this PR is closer to TypeScript than before and still not equal to it — the doc comment says exactly that and points at §10.

**Plugin suites:** json 21, debug 42, directive suite, unchanged in outcome after §7 (the debug ruleDone closure reads only `rule.d/name/i/node` and `done.state/alt/forced`, so its output is unaffected by the ruleDone ordering change and by the new `RuleDone` fields).

**Non-unit gates** (§8): callgrind on a copy of `$S/profl` pointed at the worktree (`Rule::deref_mut` absent from the flat profile; **`drop_in_place<Rule>` ≈ one per rule created, and `drop_in_place<RuleSnapshot>` absent or near-zero** **[n]** — the record is inline in `Rule` after this change, so the symbol is renamed, and under `lto = "fat"` + `codegen-units = 1` it may be inlined away entirely; reading the gate against the old symbol name would look like a failure when it is a rename; zero `Rc<RuleSnapshot>` allocations), `md5sum` differ check, `$S/ab.sh` ≥ 6 rounds, peak RSS on palindrome-32768 and adder-16384, miri once over tests 1-13 (`cargo +nightly miri test`, if available in the environment; there is no `unsafe`, so this is a courtesy pass over the `Rc<RefCell<Value>>` node paths).

---

## 7. Plugin migration (concrete diffs)

**`/home/user/json/rs/src/lib.rs`** (BudgetCheck widening + `rule_stack`):

```diff
-fn depth(context: &Context) -> usize {
-    context
-        .rule_stack
-        .iter()
+fn depth(context: &Context) -> usize {
+    context
+        .ancestors()
+        .iter()
         .filter(|rule| rule.name == "map" || rule.name == "list")
         .count()
 }
@@
-fn within_depth_limit(context: &Context) -> bool {
+fn within_depth_limit(_rule: &Rule, context: &Context) -> bool {
     depth(context) < DEPTH_LIMIT
 }
```
plus `Rule` added to the `use tabnas::{..}` import; the doc comment at :154 (`rule_stack.len()`) reads `ancestors().len()`; `parser.parse_budget(1, within_depth_limit)` at :318 is unchanged text (a fn item satisfies the HRTB bound). The DEPTH_LIMIT boundary test in `json/rs/tests/json_test.rs` is unaffected (the check still runs at the loop top before the push).

**`/home/user/debug/rs/src/trace.rs:194-200`:**

```diff
             if kinds.stack {
                 let stack = context
-                    .rule_stack
+                    .ancestors()
                     .iter()
                     .map(|frame| format!("{}~{}", frame.name, frame.i))
```
The `subscribe_lex` (:159), the `rule` branch (:182-192) and `subscribe_rule_done` (:211) closures read only the `rule` parameter, `done.state/alt/forced` and `context.options`/`context.instance`; unchanged.

**`/home/user/directive/rs/tests/common/mini_grammar.rs:111-115`** (an alternate condition: the rule is the engine's current rule, so `context.parent()` is its parent):

```diff
-        implicit.c_fn = Some(Arc::new(|rule: &mut Rule, _context: &mut Context| {
-            let parent = rule.parent_rule.as_ref().map(|parent| parent.name.as_str());
+        implicit.c_fn = Some(Arc::new(|rule: &mut Rule, context: &mut Context| {
+            let parent = context.parent().map(|parent| parent.name.as_str());
             rule.n.get("dlist").copied().unwrap_or(0) != 1
                 && !matches!(parent, Some("elem") | Some("pair") | Some("ilist"))
         }));
```

**`/home/user/directive/rs/tests/doc_examples_test.rs:234-237`** and **`/home/user/directive/rs/tests/directive_test.rs:195-206`** (alternate actions, same reasoning; bind the ignored context parameter):

```diff
-            .with_action(move |rule, _context| {
+            .with_action(move |rule, context| {
@@
-                let at_pair = rule
-                    .parent_rule
-                    .as_ref()
-                    .is_some_and(|parent| parent.name == "pair");
+                let at_pair = context.parent().is_some_and(|parent| parent.name == "pair");
```

**Optional, non-breaking, in `/home/user/directive/rs/src/lib.rs:140-142` and `mini_grammar.rs:49-51`:** `pub fn set_node(rule: &mut Rule, value: Value) { rule.set_node(value) }` — the engine now carries the shared-cell footgun's fix (graft 9); directive's helper and doc comment can stay as a thin wrapper.

**Verified not a break:** directive `lib.rs:48` (`RuleSnapshot` import) and `:560-566` (`_next: Option<&RuleSnapshot>` on `state_action_with_next_ref`) compile unchanged; `ActionFn`/`TokenActionFn`/`ConditionFn` (:101/:108/:111), `DirectiveAction::run` (:184), every `(&mut Rule, &mut Context)` closure, `rule.resolve_open_value(0, context)` (mini_grammar.rs:78), `rule.node = Rc::new(..)` writes, `rule.child_node`/`rule.parent_node` reads: unchanged. The measure harness's `profl/src/parsers.rs` (both benchmark grammars: `add_action`, `c_fn`, `rule.u_mut()`, `rule.parent_node`, `context.v_abs`, `context.source`) compiles unchanged, so the profiling binary is a like-for-like build. Nothing in the three repos calls `Rule::snapshot()`, `Rule::clone()`, `Context::new`, constructs or compares a `RuleDone`, or reads `child_rule`/`prev_rule`/`next_rule`.

**Sequencing:** directive builds the engine by path (`tabnas = { path = "../../parser/rs" }`) so its three test edits land after the engine merges, as with #177's `TokenText` note; json and debug likewise.

**Migration table for the changelog** (plugin authors) **[R14, R15, P1, P4, P6, S4, S11]**:

| was | now |
|---|---|
| `context.rule` | the `rule` argument you already hold — it is the same object (TS `ctx.rule === rule`) |
| `context.rule_stack` (`.iter()`, `.len()`, `.get(i)`, `.last()`) | `context.ancestors()`, same calls, yielding `&Rule`; `context.depth()`; not writable |
| `context.rule_stack[i]` | `context.ancestors()[i]` — it still exists and still panics out of range. **No rewrite is needed: `let frame = &context.ancestors()[i];` compiles** and `frame` stays usable, because temporary lifetime extension covers the operand of an index expression in a `let` initializer **[T3]**. Revision 3 claimed this was E0716 and told you to bind the view first; that was wrong, and the row is corrected rather than deleted so the claim does not survive in anyone's notes. The two restrictions that are real: **E0515** if you return that reference out of a function (bind `.get(i)`/`.last()`, which take `self` by value and return `&'a Rule`, or take a `to_snapshot()`), and **E0502** as in the row below |
| `let parent = context.rule_stack.last().cloned(); context.u.insert(..); /* use parent */` | the yielded `&Rule` **borrows `context`**, so the same shape is now **E0502**. Read what you need before writing (`let name = context.ancestors().last().map(\|p\| p.name.clone());`) or take an owned copy (`parent.to_snapshot()`) **[S11]** |
| `context.rule_stack[rule.d - 1]` inside a `RuleDoneSubscriber` | **panics after a pop**: the stack was synced pre-transition and is now post-transition, so `depth() == rule.d - 1` there and `rule.d - 1` is out of range. Use `done.parent` **[S11]** |
| `rule.parent_rule` | `context.parent()` (`Option<&Rule>`) **in a callback that receives `&mut Rule`** — conditions, routes, actions of every kind, subscribers, matchers, lazy values, the budget check — where the rule is the engine's current rule. **In a `RuleDoneSubscriber` use `done.parent`**: there `context.parent()` is the top of the post-transition stack, which is the completed rule itself after a push and the grandparent after a pop |
| `context.rule_stack.len()` as the rule's depth | `context.depth()` — equals `rule.d` in every `&mut Rule` callback; in a `RuleDoneSubscriber` it is `d + 1` / `d` / `d - 1` after a push / replace-or-self / pop |
| `rule.child_rule` (the rule this one pushed) | **`rule.child()`** — `Option<&Rule>`, the rule that was pushed, TS `rule.child` **[P4]**. The FIELD `child_rule` is still there, still `Option<..>`, but it is now `Option<Box<Rule>>` holding the **last** rule of the child's replacement chain; `child()` walks `prev_rule` back to the head for you. **Both the field and the accessor are `None` for the WHOLE lifetime of a live child** **[Q4]** — not only during the pushing pass — where TypeScript's `rule.child` (rules.ts:665) and Go's `r.Child` (rule.go:1266) are the live child object from the push until the rule pushes again, and where today's Rust hands you a frozen push-time snapshot. For the live child read **`context.ancestors()[rule.d + 1]`** (or `.get(rule.d + 1)`), or the **callback's own `rule`** when the callback is running on that child, or **`context.pending()`** during the pushing pass's after-actions **[P1]**. The declarative `child.*`/`next.*` conditions do not have this hole: the walker resolves them through the live descendant (§4) |
| `rule.prev_rule` | still a field, now `Option<Box<Rule>>`; `.as_deref()` reads work |
| `rule.next_rule` | a `NextRule` tag (`NoRule`/`Same`/`Child`/`Replaced`/`Parent`), not a rule. The next rule as an object reaches: **every after-action of a pushing or replacing pass that receives a `Context`, through `context.pending()`** **[P1, V2]** — `ContextAction` (`add_action`, `ao_fns`/`ac_fns`), a `Named` binding registered with `action_with_context`/`state_action_ref`, and state actions alike, but **not** a `Named` binding registered with the plain `Tabnas::action` (see the `Action` row above); a `StateAction` and an `AltModifierWithMatch` additionally through their `next` argument; a `RuleDoneSubscriber` through `done.next`. Outside that window: `rule.next_rule_name`, the tag, `rule.child()`, `context.parent()` |
| `rule.next_rule`/`child_rule`/`parent_rule` read on the rule passed to a `RuleDoneSubscriber` | `done.next` / `done.parent` (live `&Rule`, TS `prev.next`/`prev.parent`); `rule.child_rule` is `None` on the push event (the child is the engine's live rule) |
| `rule.snapshot()` | `rule.to_snapshot()` — an owned copy, when you must keep it past the callback |
| `Rule: Clone` | gone; `RuleSnapshot: Clone` stays. **`rule.clone()` no longer fails to compile — it changes type** **[P6]**: on a `&mut Rule` it resolves through `Deref` to `RuleSnapshot::clone()` and yields a `RuleSnapshot`; on a `&Rule` it resolves to `<&Rule as Clone>::clone` and yields another `&Rule` (clippy's `clone_on_copy`/`clone_double_ref` may or may not flag it). Either way the error surfaces at the *use* site, not at the clone, so grep for `.clone()` on a rule binding rather than relying on the compiler to point at it |
| `parse_budget(n, \|context\| ..)` | `parse_budget(n, \|rule, context\| ..)` |
| `rule.node = Rc::new(RefCell::new(v))` | still works; `rule.set_node(v)` is the same thing |
| `RuleDone` (a `'static` record) | `RuleDone<'a>` with two more fields; closures are unchanged; `==` compares the links by identity |
| a named after-action registered with `Tabnas::action` (`Action = Fn(&mut Rule)`) that read the rule about to run, through `rule.child_rule` / `rule.next_rule` | **this callback kind has no replacement for that read** **[V2]**: it receives no `Context`, so `context.pending()` is unreachable from it. Re-register it with `action_with_context` (lib.rs:882-888) or as a state action (`state_action_ref`, lib.rs:908-913) — both take `&mut Context` — and read `context.pending()`; or keep the plain `Action` if `rule.next_rule_name` and the `NextRule` tag are enough. `run_action_with_config` resolves builtins, then `actions`, then `context_actions`, so **remove** the old `action(..)` registration rather than adding the new one alongside it. Ratification item §0.4 **R9** |
| `parent_rule` read on a rule you reached **through a link** — `done.next.parent_rule`, `rule.child_rule.as_deref().unwrap().parent_rule` | gone, with no accessor **[n]**. `context.parent()` answers the question only for the engine's current rule. The parent of a rule you reached through a link is the **holder you reached it from**: your own `rule` argument for `done.next` on a push event, and the rule whose `child_rule`/`prev_rule` you walked otherwise. The declarative walker does exactly that (`Via::Child` → holder, `Via::Prev` → holder's parent, §4). Verified unused in json, debug and directive |
| *(changed in a way that is easy to miss)* | the `next` argument of a **before**-open action is still an owned copy of the current rule, but the copy is now taken at the **first bound state action of the phase** instead of before the phase's first action — so a `bo` `ContextAction` declared before a `bo` state action now shows up in that state action's `next`, where it did not **[U1]**. Same edit and same reason as the `ao`/`ac` change **[S8]**; TypeScript passes the live rule (rules.ts:535/:564) and reflects every earlier write, so both moves are toward it and neither arrives |
| *(unchanged, listed so it is not mistaken for new)* | the `next` argument of an `AltModifierWithMatch` is, and was, an owned copy of the current rule, taken immediately before the single call that consumes it (parser.rs:2089 → :2092, nothing can run in between) — TypeScript passes the live rule at rules.ts:577. Pre-existing, and this PR does not change it **[P6]** |

---

## 8. Landing sequence

Every commit keeps `cd rs && cargo test --all-targets && cargo test --doc && cargo clippy --all-targets --all-features -- -D warnings && cargo fmt --check` green, and `ci/parity/run-parity.sh` where the script runs it. Measurement after each **stage** (not each commit): copy `$S/profl` to `$S/profl-p2`, point its `Cargo.toml` `path =` at the worktree, build in the shipped config, `md5sum` against `$S/bin-profl-main` (must differ), then `valgrind --tool=callgrind --cache-sim=yes --branch-sim=yes` on `adder` and `palindrome` (20 parses each, ~1 min per run, one at a time on the shared 4 CPUs); report total Ir, D1 misses, mispredicts, and the `deref_mut`/`drop_in_place<RuleSnapshot>`/`drop_slow` lines from `callgrind_annotate --inclusive=yes`. Worktree: `git -C /home/user/parser worktree add $S/wt-phase2 main`. The PR is opened **ready for review, never as a draft** (CLAUDE.md), with the attribution lines from the session reminder on every commit.

**Step 0 — ratification (no code).** Post to the maintainer and get a yes before the branch is opened. The **nine** items are stated in full in **§0.4**; in short: **R1** the re-expression of `introspection_test.rs:495-560` (a capability removal: a callback can no longer write the published stack); **R2** the re-expression of `api_test.rs:311-348`; **R3** the ruleDone post-transition stack plus `done.next`/`done.parent`; **R4** keeping Rust's after-action stack order; **R5** the unrun rule of a failed push or replace after-action, which is retained on one recovery arm and cannot be retained on the other two without a fifth owning link **[T4, Q1, Q6]**; **R6** the new public `Context::pending()` and its window; **R7** `child`/`next` moving to TypeScript's head-of-chain; **R8** the `done.next` resolution table on an error-pass event **[Q2]**, now also covering `done.parent` on the arms recovery popped out of and the forced close of the rule that failed **[V1, V3]**; **R9** the named plain `Action` after-action losing the pending rule, which `context.pending()` cannot reach because that callback kind receives no `Context` **[V2]**. The briefing's own wording (`rule_stack_shadow` is "evidence the field should not be writable from a callback at all") supports R1; all nine are contract decisions, and **three** of them (R1, R5, R9) remove or fail to preserve a capability.

**Stage A — the `Context` API (keeps `Rc<RuleSnapshot>` and `make_mut`).**

- A1 `rs: widen BudgetCheck to (&Rule, &Context); add Rule::set_node` — options.rs:953, lib.rs:1332/:1344, parser.rs:1446; budget_test.rs:18, parse_hook_ref_test.rs:74, no_panic_test.rs:269. Green. No measurement (no hot-path change).
- A2 `rs: the ancestor stack lives on Context; callbacks read it through ancestors()` — `stack` → `context.rs: Vec<Rule>` (not yet boxed), `Ancestors` (with `without_last`), `parent()`, `depth()`, `ParseSite` loses `stack`, every helper signature (§3.3, including `check_lifecycle_output`/`raised_token_error`/`ancestors_for`), the 16 mid-step `set_rule` lines and :1428 `set_active` deleted, `follow_stack`/`same_*`/shadow deleted, `Context.rule`/`rule_stack` deleted, `RuleDone<'a>` with `next`/`parent` — at this stage the arms pass them from the loop locals they already hold (`Some(&child)` / `Some(&next)` before the move, `context.rs.last()` for the parent), and the replace/pop arms keep today's store-before-notify order **[P3]**, which they already have (:2536/:2602 precede :2610); introspection_test ×3 and callback_surface_test:32 re-expressed; tests 1 (minus the address identity and the `pending()` assertions, which stage A cannot give), 3, 4, 5, 6, 9 added. **What the recovery events can carry at this stage:** `done.parent` gets its full **[V1]** shape here, including the `parent_i` normalisation, which needs nothing but two `usize`s and the current rule; `done.next` on the error pass and on a forced close needs the `NextRule` tag to resolve arms 3-6 and `forced_next`, so at stage A those two events carry only what is still owned locally (the pending rule where the caller holds it, `None` otherwise) and the tag-driven arms — with test 6's `[Q2]` and `[V3]` cases — land with B2. Test 3's `[U2]` root-result assertion lands here: the POP arm's read-before-notify order is today's order and does not depend on the representation. The `Rc<RuleSnapshot>` links, `Rule::snapshot()`, `Rule: Clone` and the four walkers are untouched; the walkers still resolve `child` to the tail at this stage, so test 7 and `Rule::child()` wait for B2.
- A3 `rs: ci runs the doctests` — `ci/rust/run.sh` (add `cargo test --doc --locked` after :11) and the Makefile `test-rs` target (Makefile:46-48) **[S7]**.
- **Measure A [U4].** Expected on adder-512 (arithmetic, §9): **~6-7.5M Ir removed, centred on ~6.8M**. Stage A removes every copy whose only holder was `context.rule`, and on **both** grammars that is the whole `:1743`/`:1745`/`:2540` family: each transition's own `deref_mut` has already detached the record the incoming current rule carries — `:2536` copies `next` while `:2503` holds the old record (replace), `:2484` copies the child while `:2450`/`:2451` hold the old one (push), `:2602` copies the resumed parent while the retired child's `parent_rule` holds the old one (pop) — so `context.rule` is the second holder and the only one. Revision 4 credited `:1743` on adder with a surviving forward link from `:2503` and expected ~4.5-5.5M; that link names the record `:2536` left behind, not the one `:1743` writes, and the two arms are symmetric (which is also why `:1743` and `:1745` are ~10,240 copies each — they are the two branches of one `if is_open`). The arithmetic: 5.66M inclusive for the three sites, **minus** the ~30,720 × 24 Ir `make_mut` fast-path checks that survive stage A (`DerefMut` is still `Rc::make_mut` until B2) ≈ 4.9M; plus ~0.7M of `set_rule` clone/drop pairs; plus ~0.2M of `follow_stack`/`sync_rule_stack`; plus the drop side for 30,720 of the 51,280 copies, ~0.9-1.2M. The two link-caused sites (`:2536`, `:2541`, 4.31M) and the 2.47M of fast-path checks stay for stage B. Palindrome: the same family, **~5-6.5M** (derived; the briefing has no per-site palindrome numbers).

  **The gate is two-sided.** Revision 4's gate fired only when a number came in "under half its expectation", so an under-prediction silently disarmed the check it exists to perform — which is exactly what happened to it. Stop and diagnose before stage B if the measured removal on either grammar is **below the band** — a publish survived; grep `set_rule|snapshot\(\)`, and confirm `:1743`/`:1745` are single-holder in the new build — **or above it**: a copy stage A cannot account for has gone, which means the holder model above is wrong, and stage B's expectation *and the 11-12M committed claim that rests on the same arithmetic* have to be re-derived **before** B2 lands rather than after. Coming in over the band is not good news.

**Stage B — the `Rule` representation.**

- B1 `rs: the pending rule reaches every context-taking after-action; next for state actions is materialised lazily` — `Context.pending` + `Context::pending()` **[P1]**, `NextOf`, `materialise_next` **[S1]**, the :1540 lazy `owned` **[S2]**, :2089 `as_ref()`, the pending protocol in the push and replace arms, `recover_error_pass`/`attempt_recover` taking the pending **[P2]**, and the removal of the :2450/:2451/:2503 pending-snapshot stores that the capability rode on until now. At this stage `Rule` is still the `Rc`-backed struct, so the pending rule is boxed for the window and unboxed on the way out — one allocation per push/replace that exists only between B1 and B2, and never appears in a measurement (Measure B runs after B2). Green: callback_test.rs:101-130 pins `next`; test 1's `pending()` assertions (and its `[V2]` recording of the one kind that cannot), test 13's four phases **[U1, S8]** — this is the commit that moves when the `next` copy is taken, in all four — and test 6's two recovery cases land here.
- B2 `rs: a rule holds its state inline; links are owned boxes or tags` — `Rule { data, child_rule: Option<Box<Rule>>, prev_rule, next_rule: NextRule }`, `Box<Rule>` in the loop, in `rs` and in `pending`, the five arms rewritten (§3.3), `retire()`/`retire_in_place()` **[P3]**, `child()` **[P4]**, `detached()`, `to_snapshot()`, `snapshot()` removed, `Rule: Clone` removed, the candidate swap at :1729, the walker collapse with the head-of-chain `child` and the tail `Replaced` rule **[S12]**, manual `Debug`, iterative `Drop`, the `u_mut` counter **[P5, S3]**; api_test:311-348 re-expressed; the tag-driven `done.next` resolution — `recover_error_pass`'s `done_next` arms 3-6 **[Q2]** and `forced_next` on both forced-close events **[V3]** — lands here with the tag it reads; tests 1 (full), 7 (with the TS fixture and Go case landing in the same commit), 8, 10, 11, 12 added, and test 6's `[Q2]`/`[V3]` cases complete. This is the large commit; it is one commit because the link representation and the arms cannot be split green.
- B3 `rs: record the outcome in the DerefMut and Context docs; README parity paragraph; changelog note` (§10).
- **Measure B.** Expected cumulative on adder-512: 11-12M committed, 12-14M by arithmetic; palindrome-512: 9-10M committed. Profile must show no `<Rule as DerefMut>::deref_mut` symbol, `drop_in_place<RuleSnapshot>` ≈ 10,250 calls, no `Rc<RuleSnapshot>` allocation. Then: `/usr/bin/time -v` peak RSS on palindrome-32768 and adder-16384 from a copy of `$S/alloc` built against the worktree versus the same bench at `main` (the #177 failure signature was 71 MB → 1214 MB; the gate is "not above main"); `$S/ab.sh <main> <branch> 6` interleaved, reported per the ≥3% rule ("removed N instructions with no visible regression" unless the clock clears 3%); D1/mispredict deltas reported as secondaries.

**Downstream (after merge):** json one line + `_rule` parameter, debug one line, directive three test lines (§7), measure harness provenance manifest pin (invariant 11) if the harness re-records.

---

## 9. Expected Ir removed and every new hot-path cost

All arithmetic is against the verified adder-512 profile (155,148,899 Ir, 20 parses, ~20,500 steps, 10,240 completions).

**Removed (adder):**

| item | verified / derived | Ir |
|---|---|---|
| the five copying sites, inclusive | verified: 1.84 + 2.01 + 1.81 + 2.14 + 2.17 | 9.97M |
| `make_mut` fast-path checks on the non-copying `deref_mut` calls | verified: ~102,700 × 24 | 2.47M |
| `set_active`/`set_rule` clone + drop-previous pairs | derived: ~41,000 × ~17 | ~0.7M |
| `follow_stack` + `sync_rule_stack` per step | derived | ~0.2M |
| drop side: `RuleSnapshot` drops fall from ~61,520 to ~10,250 (the ~20,520 `Rc<Vec<Token>>` frees stay) | derived from the verified 3.29M | ~1.5-2.0M |
| **gross** | | **~14.8-15.3M** |

**New hot-path costs** **[R2, P1]**:

| item | adder / palindrome | shipped JSON grammar (`test/spec/*.tsv`, json plugin) |
|---|---|---|
| `Box::new` of a 224 B rule vs `Rc::new` of 168 B | +~0.1M | same per rule |
| `retire()`/`retire_in_place()` two stores ×10,240; `Drop` take()s ×10,250 | ~0.15M | same per rule |
| `NextRule` byte stores replacing `Rc` stores; `Vec<Box<Rule>>` push/pop moving 8 B instead of 96 B | cheaper | cheaper |
| the pending move: two 8-byte `Option<Box<Rule>>` moves per push/replace **[P1]** | ~10,240 × ~6 Ir ≈ 0.06M | same per transition |
| `NextOf` construction (now a fieldless enum) | register-sized | register-sized |
| the `candidate` swap | 0 (no declarative conditions) | two `Rc` moves per alternate with a declarative condition; today a whole-rule clone |
| lazy `next` for before/after state actions, **including `NextOf::Pending`** | **0** — neither grammar binds a lifecycle action | **0 new** — `@val-bo`/`@map-bo`/`@list-bo`/`@val-bc`/`@pair-bc`/`@elem-bc` are builtins (builtins.rs:28-34): the `Named` arm's single `state_actions.get` misses (one SipHash today, one after — no pre-scan, no second lookup) and `owned` never materialises. Today's :1540 `snapshot()` was an `Rc` bump that made the builtin's first write (builtins.rs:310) a 152 B copy; both go. A grammar that binds a lifecycle **state** action pays one `to_snapshot()` (~120 B + 9 refcount bumps, 2 atomic) on the first bound state action of that pass — for a self, a parent **or a pending** `next` **[P1]** — where today it paid an `Rc` bump plus a full-record copy on the next write |
| the `child`-hop chain walk in the declarative walker and in `Rule::child()` | 0 (no `child.*`/`next.*` condition; `child()` is called only from the walker, the forced-close loop and the recovery reader) | 0 (none declared) |
| `Vec<Box<Rule>>` on the `Context` where a `Vec<Rule>` loop local used to be: one pointer hop per frame on the stack scans, and a hot local becomes a field load **[n]** | not measured, and not claimed to be zero. The scans are `best_partial_value`, `compute_sync_tins`, `continuation_tins` and `conditions_match`; all of them are recovery or continuation paths except `update_partial`, which is gated on `mode.recovering`. Neither benchmark recovers, so this should not appear in either profile — if stage A comes in **under** its band, this is the second thing to look at after a surviving publish | same |
| `debug_u_copies` counter | debug builds only | debug builds only |
| **Total new (adder)** | **~0.35M** | |

**Net by arithmetic ~14.5M. Committed claim: 11-12M** (the verified bound), with 12-14M the expected reading. The excess over the bound is legitimate and specific: the bound was derived for full aliasing *with `Weak`* and reserved 0.5-1M for a residual `deref_mut` check (there is no check: `DerefMut` is a projection), 0.7M for a surviving `set_rule` pair (there is no publish), and an unquantified `Weak::upgrade` per traversal (there is no `Weak`). If measurement lands *below* 11M, look for a surviving `Rc<RuleSnapshot>` clone or a `to_snapshot()` on the loop before doubting the design.

**Palindrome-512** (97,288,347 Ir; `deref_mut` 9,156,062 inclusive, 41,220 copies over 8 sites including `u_mut()` and the :2484 child copy): `deref_mut` inclusive all removed (9.16M); publish pairs incl. the four `c_fn` publishes per step (~1.0M); `u_mut()` `HashMap<String, Value>` copies, which sit under `u_mut`'s own inclusive cost and vanish **for every write after the first on each rule**, because nothing shares a live rule's `u` any more (0.5-1.5M). **[n]** The FIRST `u_mut()` on each rule still copies, and still will: `empty_values()` (rule.rs:1283-1290) hands every rule a clone of one shared empty map, so `Rc::make_mut` duplicates it on that first write exactly as it does today. That copy is of an empty `HashMap` and is cheap, it is unchanged by this PR, and it is why test 11's counter skips the sentinel (§3.2) — the saving claimed here is the repeat copies, not the first one; drop side (~0.9M); minus ~0.35M new. **Committed 9M, expected 10-11M.**

**Per stage [U4]:** A ≈ **6-7.5M** adder / **5-6.5M** palindrome (§8); B the remainder (≈ 7.5-9M on adder). Revision 4 put stage A at 4.5-5.5M on adder because it credited `:1743` with a second holder that the `Rc::make_mut` at `:2536` has already detached; the gross total, the committed 11-12M and the ≥11M sanity floor are unchanged — only the boundary between the two stages moves. B1's transient `Box::new`/free per push/replace is not measured (Measure B follows B2).

**Wall clock:** the removed work is ~51,280 `Rc` allocations and frees plus 152-byte memcpys per profile (the category #177 found converts at or above its Ir) and refcount/check computation (converts at a fraction); the added work is register moves. Expected above the 3% floor, but the claim is the callgrind delta; the clock is confirmed, not established, over ≥6 interleaved rounds after the `md5sum` check.

---

## 10. Version and changelog note

This is a **breaking change to the Rust crate's public API**, and in one narrow respect to parse results **[V4]**: **no parse-result change for any grammar that declares no `next.*` or `child.*` condition — which includes both benchmark grammars, the shipped JSON grammar and every `test/spec/*.tsv` fixture — and, for a grammar that declares one, Rust moves to the TypeScript/Go value.** Two declarative-condition views move to those canonical values — a close-pass `next.state` (test 8) and a parent's `child.*`/`next.*` after a replaced child popped (test 7) — and since a declarative condition decides whether an alternate matches, changing what it reads changes routing and therefore the parsed value for any grammar that declares one. Revision 4 opened with "no parse result changes for the same grammar and input" and announced those two changes in the same sentence; the restatement above is the honest form of the same claim, and it is the form the PR body and the release note must carry. Both are fixed, previously unrecorded divergences that get no `DIVERGENCE.md` entry because the three ports agree on them afterwards (`DIVERGENCE.md` is for same-input-different-output, AGENTS.md:434-444, and needs a `divergent.tsv` row per case). The one parse-result divergence in the same area — `@capture$` reading the chain tail's node where TS reads the head's (§4, `child-pusher.fixture.json`) — is pre-existing, unpinned in the Rust suite, **not changed here**, and named in the PR body as the follow-up. The after-action, ruleDone and `next`-materialisation orderings below are callback-observable ordering differences, not parse results; they are recorded in the changelog note and in `rs/README.md`'s parity paragraph, not in `DIVERGENCE.md`. `go/doc/differences.md` is not touched (Rust-only API). The repository ships one version across all seven sites (AGENTS.md "Releasing": `ts/package.json`, `ts/src/tabnas.ts`, `go/tabnas.go`, `rs/Cargo.toml`, `rs/src/lib.rs`, `rs/Cargo.lock` via `cargo update --workspace`, and the TS changelog), rewritten by the release orchestrator — **this PR does not hand-edit a version**. The release that carries it is `0.9.7 → 0.10.0`, cargo's semver-breaking boundary for a `0.x` crate. The PR title and merge commit use the conventional form the generated changelog reads: `feat(rs)!: callbacks receive the engine's own rule and ancestor stack` with a `BREAKING CHANGE:` footer carrying the note below; the same text goes into the PR body's "API notes" section (the #177 precedent) and the "Parity status" paragraph of `rs/README.md:129-136` is rewritten to say that the rule and its ancestors are live, aliased engine objects; that grammar and next-rule views remain immutable snapshots; and that the ancestor stack moves *after* a pass's after-actions (TypeScript moves it before), so `depth() == rule.d` holds in every `&mut Rule` callback.

> **BREAKING CHANGE (Rust):** parse-time callbacks now receive the engine's own rule and ancestor stack instead of frozen copies, as the TypeScript and Go engines do (`ctx.rule = rule`, `ctx.rs`). `Context.rule` and `Context.rule_stack` are removed: read the `rule` argument, and `context.ancestors()` / `context.parent()` / `context.depth()`. The ancestor view exposes no `&mut Rule`, so an ancestor's own fields cannot be written through it (its `node` cell still can, through `borrow_mut()`, as before); `[i]` still panics out of range, and `let frame = &context.ancestors()[i];` compiles and behaves as `&context.rule_stack[i]` did (temporary lifetime extension) — you only need `get(i)`/`last()` if you must RETURN the reference out of a function. `Context::pending()` is new: inside an after-action of a pass that pushes or replaces, it is the rule that will run next — what TypeScript exposes there as `rule.child`/`rule.next` — and it is `None` everywhere else. `RuleSnapshot` loses its `parent_rule`/`child_rule`/`prev_rule`/`next_rule` fields; `Rule` gains `child_rule: Option<Box<Rule>>` (the last rule of the completed child's replacement chain) with `Rule::child()` returning the rule that was actually pushed, `prev_rule: Option<Box<Rule>>` and `next_rule: NextRule` (a tag), and `parent_rule` is gone (`context.parent()`). `Rule` is no longer `Clone` — note that `rule.clone()` does not stop compiling, it changes type (through `Deref` to `RuleSnapshot` on a `&mut Rule`, to another `&Rule` on a `&Rule`), so the error appears at the use site; `Rule::snapshot()` is replaced by `Rule::to_snapshot() -> RuleSnapshot` (an owned copy). `Rule` gains a public `Debug` impl — hand-written, printing links as `name~i` and never recursing — which is additive and also required, since `#[derive(Debug)]` on `Context` now covers a `Vec<Box<Rule>>` and on `RuleDone<'a>` an `Option<&Rule>` **[n]**. `BudgetCheck` is `Fn(&Rule, &Context) -> bool` so a budget check can see the current rule as TypeScript's `onCheck(ctx)` can. `RuleDone` is `RuleDone<'a>` and gains `next: Option<&Rule>` and `parent: Option<&Rule>` — the transition target and the completed rule's parent, live, which TypeScript exposes as `prev.next`/`prev.parent` and Rust exposed as snapshots on the rule; its `==` compares those by identity. `Rule::set_node(value)` installs a fresh node cell. Every `(&mut Rule, &mut Context)` callback signature is unchanged.
>
> Behaviour changes toward the canonical engine: declarative `next.*` conditions read the live next rule (a close-pass `next.state` is `"c"`, not the frozen `"o"`); a parent's `child.*` and `next.*` resolve to the rule it pushed even after that rule replaced itself, as TypeScript's `rule.child`/`rule.next` do (previously the last replacement), and resolve to the LIVE child while that child is still running, as all three engines do; a ruleDone subscriber sees the ancestor stack *after* the transition (a pushed parent is on it, a popped one is not; `done.next`/`done.parent` carry the moved rules); and a rule abandoned by a fixed-depth recovery pop (`recover.pop_until_valid = false`, parser.rs:768-770) is retained as the resuming rule's `child_rule`, so `child.*` still names it as TypeScript's `rule.child` does **[n]** — today's Rust drops the abandoned rule there and leaves the parent's push-time snapshot in place, and this is an ADD relative to today rather than a repair of something the new representation broke.
>
> The `next` **argument** of a lifecycle state action is still an owned copy in every case, and this release changes *when* the copy is taken — **in all four phases, `bo`/`bc` as well as `ao`/`ac`** **[S8, U1]**. It used to be taken once per phase, before the phase's first action (`rule.next_rule.clone()` at parser.rs:257 for the after phases, `is_open.then(|| current_rule.snapshot())` at :1540 for the before ones); it is now taken at the first *bound state action* of the phase. For a pass that pushes or replaces, and for the `next` of a closing pass (the parent), the difference is not observable: the pending rule and the ancestors are reachable only as `&Rule`, so no after-action can change what the copy would have held. Where `next` is the rule itself — an open pass with no route, and **every before-open phase, where `next` is always the rule itself** — it is observable: a `ContextAction`, or a named action that is not a state action, running *before* a state action in `bo`/`ao`/`bc`/`ac` order now changes what that state action sees in `next`, where previously the copy predated every action of the phase. On the fallback ordering (`resolved_action_order`, rs/src/rule.rs:1062-1070) every callback precedes every state action, so a phase that binds both is affected there by construction. TypeScript passes the live rule to both the before handlers (`let next = is_open ? rule : ctx.NORULE`, ts/src/rules.ts:535, used at :564) and the after handlers (:720, used at :728), so it reflects every earlier write; this is a move toward TypeScript in all four phases, not all the way to it, and it is pinned by test 13. Revision 4 recorded this for `ao`/`ac` and asserted the before phases were "unchanged by this release"; one edit causes both, so that assertion is withdrawn **[U1]**. What IS unchanged is the `next` argument of an `AltModifierWithMatch`: it was and remains an owned copy of the current rule, taken immediately before the single call that consumes it (parser.rs:2089 → :2092, with nothing able to run in between), where TypeScript passes the live rule at rules.ts:577 **[P6]**.
>
> Behaviour that differs from the canonical engine, deliberately:
> 1. The ancestor stack moves **after** a pass's after-actions — inside `ao` of a pushing pass and `ac` of a popping pass, `context.ancestors()` is the pre-transition stack and `depth() == rule.d`, where TypeScript and Go have already pushed/popped `ctx.rs` (rules.ts:662/:713 before :724-733; rule.go:1258-1263/:1312-1314 before :1332-1339). The push case cannot be TypeScript-ordered without the rule being both the callback's `&mut Rule` and a frame of `rs`; the pop case could, at the price of `error.rule_stack` no longer listing the parent for an error raised from `ac` (TypeScript's does not, and it lists the rule twice for an error raised from a push's `ao`, error.ts:208-216 — its own debug plugin slices `ctx.rs` to `rule.d` to undo the early move, debug/ts/src/debug.ts:1093-1097). `error.rule_stack` is byte-identical to before on every path.
> 2. *(was: after-actions lose the pending rule — **narrowed**, not withdrawn **[P1, V2]**.)* Every after-action of a pushing or replacing pass **that receives a `Context`** still reaches the rule that will run next, now through `context.pending()` rather than through `rule.child_rule`/`rule.next_rule`: a `ContextAction` (`add_action`, `ao_fns`/`ac_fns`), a state action, and a `Named` binding registered with `action_with_context` (lib.rs:882-888) or `state_action_ref` (lib.rs:908-913). What changed for them is the path and the window: the field `rule.child_rule` is `None` during the pushing pass (TypeScript's `rule.child` is the pending child there) and `rule.next_rule` is a tag, so a plugin that read either must read `context.pending()`, which is `Some` only inside those after-actions. **The fourth kind loses the capability outright.** A named action registered with `Tabnas::action` is an `Action = Arc<dyn Fn(&mut Rule) + Send + Sync>` (lib.rs:144) and receives no `Context` at all, so `context.pending()` is unreachable from it and there is no replacement for the read; it keeps `rule.next_rule_name` and the `NextRule` tag. Migrate it to `action_with_context` or to a state action, and **remove** the old registration — `run_action_with_config` (parser.rs:424-446) resolves builtins, then `self.actions`, then `self.context_actions`, so a name registered both ways still resolves to the context-less one. Revision 4 claimed the capability survived for every after-action; for this kind it does not. Ratification item §0.4 **R9**.
> 3. When an after-action fails and recovery survives it, the rule that was about to run is reported once as `done.next` on the error-pass ruleDone and then dropped, unless there is a field free to own it. Where it is dropped, `next_rule` is reset to `NoRule` so the tag cannot dangle, `next.*` resolves to nothing, and `next_rule_name` still names it — where TypeScript resolves `rule.next` to the live unrun rule for as long as the rule lives (rules.ts:720 assigns before the afters at :728; `bad()` returns the same rule, :1183-1193). **Which case is which [T4, Q6]:**
>    * `accepts_close` (parser.rs:775-782), failed **push** — **not a divergence**: the unrun child is retained as `child_rule` with the tag `Child`, so both `child.*` and `next.*` resolve to it, as TypeScript does. `child.*` on the failing rule is otherwise untouched — it still names whatever child that rule had pushed earlier.
>    * `accepts_close`, failed **replace** — divergence as above. Nothing can own a replacement that never ran and is not the current rule.
>    * `!pop_until_valid` (parser.rs:768-770) and the forced-pop loop (:784-796), failed **push or replace** — divergence as above, **for a push as well**. The resuming rule's `child_rule` is taken by the ABANDONED rule, so the unrun rule has no owner on these two paths either. Revision 3 of this spec asserted that a failed push was never a divergence; that was true only of the `accepts_close` arm.
> 4. The rule handed to a ruleDone subscriber on the recovery path (parser.rs:817) is link-free: `child_rule` and `prev_rule` are `None` and `next_rule` is the tag, where the previous clone carried the link snapshots; `done.parent` is the frame at `d - 1` if recovery left it on the stack, and `done.next` is the rule the failed pass never ran **[P2]**.
> 5. `rule.child_rule` is `None` for the **whole lifetime of a live child**, not only in the ruleDone for a push **[Q4]**: where TypeScript's `rule.child` (rules.ts:665) and Go's `r.Child` (rule.go:1266) are the live child object from the push until the rule pushes again, and where today's Rust holds a frozen push-time snapshot there, the new field holds `None` until that child completes. It is structural — the live child is the engine's `&mut Rule` and cannot also be owned by the rule that pushed it — and it is a widening of an existing divergence (the snapshot was already not the live object), not a new one. The live child is reachable as `context.ancestors()[rule.d + 1]`, as the callback's own `rule`, as `context.pending()` during the pushing pass's after-actions, and as `done.next` in the push ruleDone; the declarative `child.*`/`next.*` conditions resolve to it directly. The two cases the r1 review asked about are **not** divergences: during a replace event `done.next.prev_rule` IS the completed rule (TypeScript's `prev.next.prev === prev`, rules.ts:693) and during a pop event `done.parent.child_rule` IS it, because both links are stored before the event and stripped after it **[P3]** — which is the order today's engine already has (parser.rs:2536/:2602 precede :2610).
> 6. **A callback can no longer write the ancestor stack** **[Q3]**. `context.rule_stack` was a public `Vec`, and a callback could reorder or rewrite it; `Context::sync_rule_stack` (context.rs:332-335) deliberately carried a same-length callback write forward, and `rs/tests/introspection_test.rs:495-501` documents that as three-port behaviour — TypeScript's `ctx.rs` and Go's `ctx.RS` are both writable from a callback and none of the three engines sanitizes what a callback leaves there. `context.ancestors()` yields `&Rule` only, so that capability is gone. This is a move **away** from the canonical engines, not toward them, and it is deliberate: the stack is now the engine's own working data rather than a published copy, which is what removes the per-step snapshot cost. A frame's `node` cell is still writable through `borrow_mut()`, as it was. Ratification item §0.4 R1; the re-expressed test asserts strictly more about the ancestry a callback observes.
> 7. **`done.next` on the error pass of a failed pass** reports `None` in four cases where TypeScript's `prev.next` still names a rule **[Q2, V1]**: (a) the unrun rule of a failed push or replace that recovery could not retain (item 3 above) — reported once as `done.next` on that event and `None` for every later read; (b) tag `Child` on a rule recovery moved away from, where the completed child is owned by a rule that is no longer current; (c) tag `Replaced` where the replacement is gone; (d) **a failed close pass whose parent the forced-pop loop popped *past*** **[V1]** — `parent_i` then matches neither the frame at `d - 1` (already off the stack) nor the rule the loop stopped on, so **both** `done.next` and `done.parent` are `None`, where TypeScript names the parent in both (`prev.parent` is the pusher object, rules.ts:665-666, and `prev.next` is whatever the pass assigned at :720). Everything else matches TypeScript, including the two cases revision 3 got wrong: a failed pass that routed nowhere on an **open** pass reports the rule itself (TS `prev.next === prev`), and on a **close** pass reports the resuming parent — **and `done.parent` names that same parent, on the arms where recovery popped to it as well, through the `parent_i` normalisation of §3.3 [V1]**. Revision 4's code could not produce that: it read `done_parent` from the stack after `attempt_recover` had popped, so the event reported `next = <the parent>` with `parent = None` for one rule, contradicting `RuleDone::parent`'s own doc. **The forced close of the rule that failed is likewise no longer `None` by construction [V3]**: it resolves the pending rule, then the rule's own tag, which is what TypeScript's synthesized event carries (`s(rule, ctx, done)` with the links live, rules.ts:1197-1201); revision 4 hard-coded `None` there and this list did not record it. The full tables are on `RuleDone::next` and `RuleDone::parent`. Ratification item §0.4 R8.
>
> Migration: json one line plus a `_rule` parameter on its budget check, debug one line, directive three test lines. Beyond those three repos: a named after-action registered with `Tabnas::action` that read the rule about to run, through `rule.child_rule`/`rule.next_rule`, must be re-registered with `action_with_context` or as a state action and read `context.pending()` — the plain `Action` gets no `Context` **[V2]**. Copy-on-write of rules is gone entirely: `Rule::deref_mut` no longer exists in the profile, and the 51,280 rule copies per 20 parses of a 512-term adder are zero.

**[R8, R17]** Items 1–4 above were introduced by revision 1 without being listed; item 5 and the `next`-argument paragraph close the round-r1 findings **[S8, S9]**. Items 6 and 7 were added by revision 4 **[Q2, Q3]**. Revision 5 restates the opening **[V4]**, narrows item 2 **[V2]**, extends item 7 **[V1, V3]**, widens the `next`-argument paragraph to the before phases **[U1]**, and adds the `Debug` impl and the fixed-depth-pop child retention to the lists above **[n]**.

---

## Appendix A: disposition of every fatal flaw the judges raised

| flaw | disposition |
|---|---|
| Design 4: `unsafe` in `deref_mut` sound only by an enumerated `lent` discipline; lex-path handoffs (:666/:680/:704) not on the list; `catch_unwind` defeats the panic; `debug_assert!(weak_count == 0)` panics on legal plugin behaviour; strong>1 branch silently detaches Weak links in release | Not chosen. The winning design has no `unsafe`, no `Weak`, no new panic reachable from a callback (the surface is unchanged: `Ancestors[i]` out of range panics as `rule_stack[i]` did, into an `internal` error under `catch_unwind` **[R7]**), and no per-site discipline: disjointness of `rule` and `context` is enforced by the type system on every path including the lex path (§3.5) and `resolve_val` (§5). |
| Design 3: deferred writes diverge from TS's immediate mutation; widest break | Not chosen. Writes through `&mut Rule` are immediate and visible on the next line and to a nested lazy value (test 4). |
| Designs 1/2/3: `introspection_test.rs:495-560` re-expressible only as "does not compile"; `api_test.rs:311-348` tests a deleted mechanism; needs ratification before the branch opens | Closed by §6 (★ rows) and §0.4 R1/R2: both re-expressions are specified, both assert strictly more than the originals about the behaviour that matters (true ancestry on every call; one object per rule), and ratification is the first step of the sequence. |
| Design 1: parser.rs:817 `event_rule = current_rule.clone()` not enumerated; `:583/:616` site-rule clones | Closed: `Rule::detached()` at :817 (tag carried, boxes `None`, documented **[R18]**) and `detached_with_state()` built inside `map_err` at :583/:616 (§2, §3.3), so the debug plugin's per-completion path does not pay a copy. |
| Design 1: the `to_snapshot()` guard must cover `ActionBinding::Named` resolving through `self.state_actions` (:262) | Closed without a pre-scan: `next` is materialised lazily inside the `Named`-hit and `State` arms from the lookup the `Named` arm already does, so the shipped JSON grammar's builtin lifecycle names cost no second hash and no copy **[R2]**; callback_test.rs:101-130 exercises exactly that binding. |
| Design 1: ruleDone `ancestors()` ordering after a push/pop | Decided to match TS `ctx.rs[0..rsI]`: the live stack at notification time, plus `done.next`/`done.parent` for the rules the transition moved **[R13]**; pinned by test 3; `ancestors_for` stays for error attachment only so `error.rule_stack` is byte-identical; `parent()`/`depth()` docs state the ruleDone cases **[R6, R16]**. |
| Refuters r0: after-action stack order presented as parity | Recorded as Rust's contract, with reasons, in §10 item 1 and in tests 1/5's wording **[R17]**; not adopted from TS because the push case is unrepresentable without aliasing and the pop case would change `error.rule_stack`. |
| Refuters r0: `child`/`next` after a replaced child pops resolve to the last replacement, unlike TS/Go | Closed: the walker resolves the `child` hop and the `Child` tag to the head of the completed chain (`Via::Chain`), `Rule::child()` names it **[P4]**, and buried frames go via the live descendant's chain; test 7 states every value per port and requires the TS fixture run before the pin lands; the pre-existing `accept_child_node` node divergence is named and left for a follow-up **[R1, R10, R11, R12]**. |
| Refuters r0: `Option<RuleSnapshot>::as_deref()` does not compile | `next.as_ref()` at :2089 and the helper-based form at :1540 **[R4, S2]**. |
| Refuters r0: `raised_token_error`/`check_lifecycle_output`/:2400 lose `site.stack` | Each takes `ancestors: Ancestors<'_>`; `compute_sync_tins` and both `continuation_tins` calls listed with their reborrow shapes **[R5]**. |
| Refuters r0: test 10 unwritable (`Value` hosts no `Drop` type) | Rewritten on `Weak<RefCell<Value>>` node cells recorded through a thread-local, reachability asserted inside the final ruleDone, release asserted after `parse()` **[R3]**, grammar shape fixed **[S13]**. |
| Refuters r0: `+ 64` slack in test 12 | Replaced by the layout invariants and an exhaustive destructuring of `RuleSnapshot` **[R9]**. |
| Design 1: `BudgetCheck` cannot see the current rule | Closed by the widening (§2, A1). |
| Design 1: `parent.child_rule == None` while the child runs | Closed in the declarative walker (`Frame` → live descendant → head of its chain, §4), through `context.pending()` during the pushing pass's after-actions **[P1]**, and documented for direct field reads (`ancestors()[d+1]` or the callback's `rule`). |
| Design 1: recursive drop of long `prev_rule` chains; RSS on 32K palindrome / 16K adder | Iterative `Drop` (§4); the chain walks in the walker and in `child()` are loops, not recursion; RSS gate in §8; `retire()`/`retire_in_place()` mandatory on every retention path. |
| Design 1: a `debug_assert` on `current_rule.d == rs.len()` could fire on a plugin write to `d` | Not added (§3.3); the invariant is pinned by tests 1 and 6 instead. (The one `debug_assert!` this spec does add — `materialise_next`'s "a pushing pass ran with no pending rule" — is on `pub(crate)` engine bookkeeping no callback can reach.) |
| Design 1: `compile_fail` doctests would not run under `cargo test --all-targets` | Closed: `cargo test --doc --locked` added to `ci/rust/run.sh` and the Makefile `test-rs` target (§3.7, A3) **[S7]**. |

## Appendix B: round-r1 findings that needed no change

*(Read with Appendix C: two claims listed here as verified in r1 were narrowed in r2 — "the walker table reproducing every test 7 row" held only for the rows r1's test 7 contained, none of which was evaluated while a child was live **[T1, T2]**, and "the `Box`-tree acyclicity argument" is untouched but the `Child` tag's RESOLUTION is not the same thing as the tag's ownership and was wrong **[T1]**.)*

Both refuters verified, and this spec keeps unchanged: the `Box`-tree acyclicity argument (strengthened in §4 with the refuter's stronger form — a `Box` graph cannot be cyclic without `unsafe`); `error.rule_stack` byte-identical on every path, including `no_panic_test.rs:296`'s `["top","top"]` through the new PUSH arm; `depth() == rule.d` in every `&mut Rule` callback including the recovery paths; the walker table reproducing every test 7 row; test 8's frozen-`o` claim (the :2541 snapshot precedes the :2567 state write); json's `within_depth_limit` fn item satisfying the HRTB bound at :318 unchanged; debug's `|rule, context, done|` closure inferring `RuleDone<'_>`; directive's three sites being places where `context.parent()` equals today's `parent_rule`; `Lexer` deriving nothing, so `Rule: !Clone` is safe at lexer.rs:28; `Value` being unable to hold an `Rc<RefCell<Value>>`, so test 10's premise holds; exactly 19 `context.set_rule` call sites (3 runners + 16 mid-step); and the pre-existing `prev.*`-on-a-fresh-rule and `@capture$`-reads-the-tail behaviours, both named and scoped as follow-ups rather than traded silently.

---

## Appendix C: round-r2 findings that needed no change

*(Read with Appendix D: three things listed here as verified in r2 were corrected in r3 — the `:1540` bullet's "unchanged by this release" sentence **[U1]**, the POP arm's statement order **[U2]**, and the stage-A holder model **[U4]**. None of the three is a borrow-checking claim, which is what the r2 soundness refuter compiled these arms for; an arm can borrow-check and still be in the wrong order.)*

Both r2 refuters compiled or traced these and found them sound. They are recorded so a later round does not re-litigate them, and so a reviewer knows which parts of this spec have been independently checked rather than merely argued.

**Compiled by the soundness refuter** (standalone models, rustc 1.94.1 / edition 2021): `Rule`/`RuleSnapshot`/`Ancestors`/`Context`/`RuleDone<'a>`/`NextRule`; `materialise_next` exactly as §3.3 writes it (`next` borrows `owned` alone, so `&mut rule` and `&mut context` are free for `run_state_callback`, and the `&mut owned` reborrow is re-taken each iteration under NLL); the `run_after_actions` loop including its `map_err(|e| .. context.ancestors())`; the :1540 lazy site; the :2089 `next.as_ref()` modifier call; the :1729 candidate swap; **all five transition arms**, including the shared-reborrow soup of the rewritten replace and pop arms (`current_rule.prev_rule.as_deref()` + `&*current_rule` + `&context` + `context.ancestors().last()` in one call) and the `notified?`-after-`retire_in_place()` ordering; `recover_error_pass`'s `done_next`/`done_parent`; `ancestors_for`/`without_last`; the `RuleCursor<'a,'h>` walker including `Chain { tail: &tail }` built on a local, provided the walker is a recursion that keeps the tail cursor in the calling frame (which §4 already requires); the widened `BudgetCheck` with json's `within_depth_limit` **fn item**; debug's `subscribe_rules` stack line; directive's three migrated closures; and the one HRTB-inference risk this spec never named — the explicitly annotated `let callback: tabnas::RuleDoneSubscriber = Arc::new(move |_, _, _| ..)` at `rs/tests/merge_test.rs:274`, which compiles with `RuleDone<'a>`. Both `compile_fail` doctests fail with the intended codes, which is why **[T6]** annotates them.

**Disjointness** holds on every path either refuter could find: `current_rule` is never in `context.rs` and is never `context.pending`; the only writes to `rs` are the `push`/`pop` in the five arms and in `attempt_recover`; no `&mut` into a buried frame is ever handed to a callback today (parser.rs:784-796 pops before mutating) and cannot after. The lex path (lexer.rs:342/:360/:470-507/:645-662) and `resolve_val` (token.rs:617 → :128, rule.rs:1441-1454, builtins.rs:180-184 via parser.rs:431) all carry `current_rule`, so §5's "the hazard class does not exist" is correct.

**The copy sites.** :1743/:1745 become plain field stores once `deref_mut` is `&mut self.data`; :2540 keeps only the `Arc<str>` bump and still borrow-checks; :2536 becomes an 8-byte `Box` move after `mem::replace`; :2541 becomes a 1-byte tag store; and :2448/:2484/:2450/:2451/:2503/:2602/:2603/:787/:788 all lose their `snapshot()` too. The `Box`-tree acyclicity argument holds and there is no `Weak` and no `upgrade()`.

**Parity properties re-verified:** `context.rule` is never read by the engine itself (only `set_rule` writes), so its removal is internally free; `depth() == rule.d` on every engine path including `attempt_recover`'s `!pop_until_valid` pop and the forced-pop loop; `error.rule_stack` is byte-identical including `no_panic_test.rs:296`'s `["top","top"]` through the rewritten PUSH arm and `introspection_test.rs:369-457`'s open/close ancestry strings; the ruleDone `stack` argument at :2610 is ALREADY post-transition today, so only `context.rule_stack` changes (R3); `grammar_spec_test.rs:190-230` resolves exactly as §4 claims; test 8's premise (the :2541 snapshot precedes the :2567 state write) is correct; the `next` argument being an owned copy in all cases is a preserved property, not a new restriction, since today's `next` is `Option<&RuleSnapshot>` and no Rust after-action can write the pending rule now either; the :1729 candidate swap is observationally identical; `api_test.rs:112`/`:151` read `rule.next_rule_name`, which survives, and the POP arm keeps `:151`'s `Some("top")` green; the standalone lexer really does move its `(Rule, Context)` pair in and out (lexer.rs:627-641) and only `LexerState` and `RelexCheckpoint` derive `Clone`, so `Rule: !Clone` is safe there; `impl Drop for Rule` is safe (nothing moves a field out of a `Rule` by value, so E0509 is never triggered) and allocates nothing when both links are `None`; the CI citations (`ci/rust/run.sh:11`, Makefile `test-rs` at :46-48) and the API line numbers (`parse_budget` lib.rs:1332/:1344, `BudgetCheck` options.rs:953) are right; and the `BudgetCheck` widening is genuinely needed, because today's `check(&context)` at parser.rs:1446 runs right after `set_active` at :1428 and so can read `context.rule`.

**One cost note the soundness refuter volunteered:** fixing **[T1]** costs **zero instructions** on the benchmark grammars and on the shipped JSON grammar — neither declares a `child.*`/`next.*` condition — so §9's arithmetic and the 11-12M committed claim are undisturbed by it.

---

## Appendix D: round-r3 findings that needed no change

Recorded, like Appendix C, so a later round does not re-litigate them and so a reviewer knows which parts were independently compiled or traced rather than argued. Both r3 refuters returned `refuted: true` on the four findings each that §0.1c lists; everything below is what they checked and could not break.

**Compiled by the r3 soundness refuter** (standalone models, rustc 1.94.1 / edition 2021, no repo build):

- `let frame = &context.ancestors()[i];` compiles with `Ancestors<'a>(&'a [Box<Rule>])` returned **by value** plus `impl Index<usize>`, and `frame` stays usable afterwards — **[T3]** is right and round r1's **[S4]** was wrong. The migration row stands as revision 4 rewrote it.
- The two `compile_fail` doctests really do produce **E0502** ("cannot borrow `context.u` as mutable because it is also borrowed as immutable") and **E0596** ("cannot borrow data in an index of `Ancestors<'_>` as mutable"). **[T6]**'s annotations are correct, which matters because an unannotated `compile_fail` passes on any compile error.
- §0.3's withdrawal is correct: **both** `is_open.then(|| &*owned.get_or_insert_with(|| rule.to_snapshot()))` and `is_open.then(|| materialise_next(owned, rule)).flatten()` compile. The generalised closure law is empirically false, and the surviving rule is the narrow one §0.3 states.
- `materialise_next` and the rewritten `run_after_actions` loop borrow-check exactly as claimed: `next: Option<&'o RuleSnapshot>` borrows only `owned`, both `&mut rule` and `&mut context` reach the callback, and the `map_err` closure's `context.ancestors()` compiles after the `&mut` call has returned.
- All three rewritten transition arms compile, including `current_rule.prev_rule.as_deref().expect(..)` alongside `Some(&*current_rule)` and `context.ancestors().last()` in one call, and the post-event `as_mut().retire_in_place()` after `notified` is bound.
- `recover_error_pass`'s `done_next` match compiles with all five borrow sources coexisting (the `pending` local, a shared reborrow of `current_rule`, `context` via `done_parent`, the local `event_rule`), and so do the three rewritten `attempt_recover` arms, including `notify_forced_close(current_rule, context, &src, current_rule.child(), context.ancestors().last())`. The **[V1]** and **[V3]** edits add only further shared borrows of the same three places and one owned `Option<&Rule>` local each.
- The `RuleCursor<'a,'h>` / `Via` walker compiles in its intended inline-recursive form, **including `Chain { tail: &tail }` on a stack local** — covariance in `'h` works. **Implementer's note the r3 refuter volunteered and this spec had only implied:** `child`/`parent` cannot be written as methods that return a cursor containing `&tail`; the head-of-chain hop must be inlined into the recursive resolver, or CPS'd.
- `RuleDone<'a>` with `RuleDoneSubscriber = Arc<dyn Fn(&Rule, &Context, &RuleDone<'_>) + Send + Sync>`: both the `impl Fn(..) + 'static` registration form with an unannotated three-parameter closure (the debug plugin's shape, trace.rs:211) and `rs/tests/merge_test.rs:274`'s explicitly annotated `let callback: RuleDoneSubscriber = Arc::new(move |_, _, _| ..)` infer and run correctly.
- clippy counts `self` in `decl.inputs`, confirmed by `attempt_recover` (7 parameters, no `#[allow]`) against `recover_error_pass` / `recover_after_actions` (11 and 10, both carrying one). **[T5]**'s central claim is right; only its `recover_error_pass` arithmetic was wrong, which **[U3]** fixes.
- The CI citations are right: `ci/rust/run.sh:11` is `cargo test --all-targets --locked` (doctests skipped), clippy at :12, `Makefile:46-48` is `test-rs`. There is no `RUSTFLAGS=-D warnings` on the test run, so the new passing doctest's dead `fn f` cannot fail the build.
- The engine reads `context.rule` nowhere outside context.rs, so removing it is internally free.
- `error.rule_stack` byte-identity holds on every arm traced: on a push `ancestors_for` strips the pusher, which is `rs.last()` after the push exactly as `stack.last()` is today; on a pop today's `stack` at notify time is **already** popped at parser.rs:2598, so the post-transition `ancestors()` matches.
- The cycle argument holds, and the r3 refuter identified the mechanism it replaces: today's `Rc` cycle between a replacement and its predecessor's record is broken **only** by the `Rc::make_mut` copy at :2536 — which is precisely why that site is one of the five, and is the fact **[U4]** turns on. Under `Box`/tag a cycle is unrepresentable, and a plugin cannot build one either (it can corrupt the tree through the still-public `child_rule`/`prev_rule`, but a `Box` cycle needs a value moved into itself).
- Retention is consistent: every rule is stripped exactly once, when it is retired (`retire`/`retire_in_place` on the chain TAIL; deeper members were stripped when they were retired), and `Drop for Rule` allocates at most one `Vec` per drop-root.
- The `u_mut` counter's short-circuit order is right: `Rc::strong_count(&self.u) > 1` is evaluated before `empty_values()` bumps the sentinel.
- The three plugin migrations type-check semantically: `rule.parent_rule` and `context.parent()` name the same rule on every engine path (a pushed rule's `parent_rule` is the pushing rule = `rs.last()`; a replacement inherits it and shares the frame), and the directive closures hold no borrow across a `&mut` use.
- `resolve_val`: every call site (`Rule::resolve_open_value`/`resolve_close_value`, rule.rs:1441/:1449, and `builtins::token_value`, builtins.rs:184) passes the caller's own `&mut Rule`, so §5's "there is no other `Rule` it could be" is sound; the standalone lexer's `Option<(Rule, Context)>` (lexer.rs:28, :622-641) moves the pair in and out and never clones, so `Rule: !Clone` is fine there.
- The `NextOf` materialisation is value-equal to today's `rule.next_rule.clone()` for `Pending`, `Parent` and `Current` in every arm checked — nothing writes the pending or the parent between the routing store and the after-actions.

**Re-verified by the r3 parity refuter, and not re-raised:** json's `rule_stack` → `ancestors()` swap is exact (`parse_budget` fires at parser.rs:1446, immediately after `set_active` at :1428, so today's `rule_stack` is byte-for-byte the ancestor stack excluding `current_rule`), and the `< DEPTH_LIMIT` boundary test is unaffected; debug's is exact (`subscribe_rules` fires at :1497, after the same sync) and its ruleDone closure reads only rule/done fields, so `RuleDone<'a>` infers; directive's three `parent_rule` sites are alternate conditions and actions on the engine's current rule, where `parent_rule` is by construction `ancestors().last()`, `None` at the root included; the `rs/tests` grep matches §6's table, with the two sites it does not list covered in Appendix C; "TypeScript never relinks the pusher's `child`/`next` at a pop" is right (rules.ts:713 assigns only the popped rule's `next`, :720; go/rule.go:1309-1316 likewise), so the head-of-chain move **R7** is toward the canonical ports and `grammar_spec_test.rs:190-230` resolves identically under the new walker; test 7's added live-child rows **[T2]** are correct per port; `error.rule_stack` byte-identity holds including `no_panic_test.rs:296`'s `["top","top"]` and after-action errors during a push; `len() == rule.d` holds on the lex path (all five `ensure_lookahead` call sites — parser.rs:1656, :2283, :2721, :2778, :2844 — pass `current_rule`); and `BudgetCheck` is the only `Context`-without-`Rule` callback that could read `context.rule` today (options.rs:953), so the widening covers the surface.
