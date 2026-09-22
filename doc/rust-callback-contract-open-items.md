# Callback-contract spec: round-4 open items, applied

The spec is [`rust-callback-contract-spec.md`](rust-callback-contract-spec.md).
Round 4 was the last automated refutation cycle. Both refuters opened with
"the design survives": soundness could not construct a callback that fails to
compile, panics or sees stale state, and parity could not find a behaviour the
migration table cannot re-express. The twelve items they left were editorial
precision, not design defects.

Required-change counts by round: r1 18, r2 13, r3 8, r4 12. The r4 rise was
the new material revision 5 added drawing its own scrutiny, not a regression.

**All twelve were applied to the spec on 2026-09-22**, on branch
`claude/parser-rs-review`. Each was re-checked against the working tree first,
because the line numbers the items cite had themselves drifted: they match no
committed state of `rs/src/parser.rs`, and the file has moved 350 to 580 lines
since they were written. Every `rs/src/parser.rs:N` citation in the spec was
re-derived at the same time; the spec's header records what that pass did and
did not cover.

**That pass over-claimed, and the spec's header no longer makes the claim.** A
review of it found qualified citations that name the wrong construct, some of
them added by the same commit: the two `#[allow(clippy::too_many_arguments)]`
attributes, the pending-snapshot stores in the push and replace arms (which
the spec's own **R9** bullet already contradicted), the `Action` type alias and
the `Rc::make_mut` in the `DerefMut` path. Those and eight more are corrected,
and the header now states a property of the process rather than a promise
about every sentence: the numbers are carried through each change to `rs/src`
by mapping them through the diff, and a reader re-derives from the construct
each citation names.

A second thing has changed under the spec since. Part of what it schedules has
LANDED, ahead of its own §8: the parent's child link is now carried on the
rule it PUSHED, so the head of a replacement chain, its node, its parent, its
own child and the first hop of `child.next` all read what TypeScript and Go
read. What is left of that item is the rest of the forward walk, and it is
registered in `test/spec/divergent.tsv` as `chain-next-two-hops` and
`chain-next-three-hops` with a `DIVERGENCE.md` entry, so the PR that finishes
**R7** deletes a register group instead of announcing a change nobody recorded.
§4, §6 test 7 and §10's `[V4]` release note are updated to match.

No Rust engine code was written for the design this spec describes. The design
remains behind Gate G of
[`rust-port-implementation-plan.md`](rust-port-implementation-plan.md) and
§8 remains un-run, as the spec's own header requires.

## What was applied

1. **§3.3, `attempt_recover`'s new parameter — applied, with a different
   name.** The parameter is `unrun_rule: &mut Option<Box<Rule>>`, not the
   `unrun` the item suggested. `pending` was unusable as the item says:
   `attempt_recover` declares `let mut pending: std::collections::VecDeque<Token>`
   at `parser.rs:1013`, moved out at `:1122` but in scope through `:1126-1160`,
   which is the exact span the rewritten arms occupy. `unrun` was not free
   either — the `accepts_close` arm and the forced-pop loop each already bind
   `let unrun = ..` as a `bool`, which the item did not notice. Renamed at the
   signature, in all three arms, at the `clear_unrun_link` and
   `Self::forced_next(..)` call sites, in the `[Q1]` comment and in the `[T5]`
   table row.

2. **§10 item (3) third bullet and §0.2 item 3 — applied.** The third bullet
   now records that a failed PUSH on the `!pop_until_valid` arm and in the
   forced-pop loop leaves the abandoned rule with `child_rule == None` as well
   as a cleared `next_rule`, so `parent.child.child.*` resolves to nothing
   where today's push-time snapshot (`parser.rs:2988`) resolves and
   TypeScript's `rule.child` names the unrun rule. §0.2 item 3's "Item (3)
   shrinks to the **replace** case" is restricted to the `accepts_close` arm.

3. **§6 test 6 — applied.** The third and fourth cases now assert
   `rule.child().unwrap().child().is_none()` on the resuming rule, so the
   `child_rule` half of that loss is pinned rather than silent.

4. **§10's `next`-argument paragraph — applied.** `bc` is out of the list of
   phases this release changes and `ac` is out of the list where the change is
   observable. The paragraph now states the mechanism: `parser.rs:1989` is
   `let next = is_open.then(|| current_rule.snapshot());` inside the one
   before-phase block that serves both `bo` and `bc` (selection at
   `:1957-1961`, bindings at `:1966-1982`), so `next` is `None` for every
   before-close phase before and after.

5. **§8 Stage A item A2, the `done.parent` recipe — applied.** `context.rs.last()`
   is named as correct for the replace and self arms only: on a push, after
   `rs.push(current_rule)` (`parser.rs:3037`), `rs.last()` IS the completed
   rule and the grandparent is `rs[len-2]`; on a pop, after `rs.pop()`
   (`parser.rs:3165`), `rs.last()` is the grandparent and the resumed parent
   is `current_rule`.

6. **§8 Stage A item A2, test 3's `[P3]` assertions — applied.** Carved out of
   stage A the way test 1's address-identity and `pending()` assertions
   already were, with the reason: `as_deref()` on an `Option<Rc<RuleSnapshot>>`
   yields `&RuleSnapshot`, not `&Rule`, so those two `ptr::eq` calls cannot
   type-check while A2 keeps the `Rc` links. Test 3's `[U2]` root-result and
   stack-shape assertions still land at A2.

7. **§10 item (3) and §0.4 R5, the event count — applied.** Both now say the
   unrun rule is reported **twice** on the forced-pop path: once on the forced
   close of the rule that failed, through `Self::forced_next(..)` **[V3]**,
   and again on the error-pass event through `done_next` arm 1. §0.2 item 9
   carries the same correction.

8. **§3.3's `[T5]`/`[U3]` table, `recover_after_actions` row — applied.** "It
   could now be dropped" is gone. Counted from `parser.rs:1215-1226` the
   function takes 10 arguments and 9 after `−stack`, clippy's threshold is 7,
   so the `#[allow]` at `:1214` is still required and dropping it would fail
   `cargo clippy --all-targets --all-features -- -D warnings`.

9. **§6 test 7, the six orphaned rows — applied.** `prev.prev.next.name`,
   `prev.parent.name`, `prev.prev.child.name`, `prev.prev.child.parent.name`,
   `next.name` and `next.state` are back under the `child3` close-pass group,
   where they are right; under `child`'s close pass they resolved to nothing
   in all three ports. The `next.state` row's Rust annotation now cites the
   current lines (`parser.rs:3106` precedes the state change at `:3133`).

10. **§6 test 7, the three understated "Rust today" cells — applied, and the
    whole group is now measured rather than derived.** `child.next.name` is
    `top`, `child.next.next.name` is `child`, `next.next.name` is `top`. All
    eight paths of the `top` close-pass group were run against this tree, in
    all three runtimes, by installing the test-7 grammar with the path under
    test as an ordered set of `c` alternates. TypeScript and Go answer
    `child`, `child2`, `child3`, `top`, `child`, `child2`, `leaf`, `child`;
    Rust answers `child3`, `top`, `child`, *(none)*, `child3`, `top`,
    *(none)*, *(none)*. The table and the spec's new note record exactly that.

11. **§0.2 item 13, §10, §6 test 13 and §8 B1, the `bc` claim — applied.**
    Test 13 is now "three phases with an assertable case", with `bc`'s binding
    stated as asserting only `next.is_none()`. The `:1540`/`:257` citations in
    its `bo`/`ao` bullet are now `parser.rs:1989`/`:601`.

12. **The `rules.ts:535` citation — applied.** `let next = is_open ? rule : ctx.NORULE`
    is `ts/src/rules.ts:538`. Corrected at all five sites. The adjacent
    citations in the same sentences (`:564`, `:577`, `:665`, `:713`, `:720`,
    `:728`, `:1183-1193`, `:1197-1201`) were re-checked and are correct.

## What applying them turned up

- **Go's declarative conditions were a no-op through the serialized door.**
  Item 10's "TS / Go" column could not be measured until this was repaired:
  Go's probe returned the first alternate for all eight paths, because
  `NormAlt` built a condition for `int` alone while `condProblems` accepted
  `int, int64, float64, string, bool`, and `encoding/json` produces none of
  them but the last two. Fixed in `go/rule.go` with a `default` arm that
  refuses a value neither list accepts, and pinned by
  `TestNormAltBuildsEveryPlainConditionValue`.

- **§8 item A3 has already landed.** `ci/rust/run.sh` now runs
  `cargo test --doc --locked`, which A3 schedules for stage A. It was added
  independently: `cargo test --all-targets` excludes doctests, so the crate's
  README examples ran in no gate at all.

## Refuter reasons, as written in round 4

Kept verbatim for the record. Where a claim here was superseded by what
applying it showed, the numbered item above says so.

### soundness (refuted=True)

The design survives the soundness lens: I could not build a callback that fails to compile where it should be allowed, that panics, or that sees stale state through a hole the spec has not already closed. Disjointness of `rule` and `context` holds on every path I traced; `Value` really cannot host an `Rc<RefCell<Value>>`, so the Box/tag graph is acyclic; the `pending` protocol cannot leak; and the five verified copy sites plus the two link-caused ones genuinely become field stores, an 8-byte Box move and a 1-byte tag once `data` is inline. [U4]'s holder re-derivation is correct: at the push, replace and pop transitions the transition's own `deref_mut` has already detached the record the incoming current rule carries, so `context.rule` is the only surviving holder.

But six concrete things are wrong, and two of them are behaviour, not prose: the `pending` compile break (item 1), the unrecorded `child_rule` loss on two recovery arms (item 2), the `bc`/`ac` claim in §10's `next`-argument paragraph (items 4 and 11), stage A's `done.parent` recipe and test 3's carve-out (items 5 and 6), the event count [V3] changed (item 7), and the `recover_after_actions` argument arithmetic (item 8).

### parity (refuted=True)

The design survives this lens intact: I re-derived `ctx.rule`/`ctx.rs`/`rule.child`/`parent`/`prev`/`next` against the TypeScript and Go sources, and every substantive parity claim held — the ruleDone post-transition stack (R3), the after-action stack order (R4), head-of-chain child/next (R7), the `done.next`/`done.parent` resolution order (R8), the V1/V2/V3 corrections, `error.rule_stack` byte-identity, and both plugin readers. But four concrete things are wrong, and two of them sit in the one artefact the spec designates as the per-port parity contract (§6 test 7, ratification item R7, and the source of `ts/test/rule-links.fixture.json`): six rows attributed to the wrong evaluation point (item 9) and three "Rust today" cells stating an absence where the engine produces a rule (item 10). An implementer building the TS fixture from that table writes the wrong fixture.
