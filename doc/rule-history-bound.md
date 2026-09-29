# Bounding the rule history

What the engine keeps of a parse while it runs, why that grows with a
document's length rather than its nesting, and a bound that keeps what
grammars read. Background: [`architecture.md`](architecture.md) for the
rule stack and the links between rules; `DIVERGENCE.md` for the
registered splits this touches (`chain-next-*`); the transducer's
[`BENCH.md`](https://github.com/tabnas/transduce/blob/main/docs/BENCH.md)
for the measurements that raised it.

## The problem

Peak resident memory is about 65 to 90 times the input for record-shaped
documents: 1.6 GB for a 24.6 MB JSON records file, 2.8 GB for 25 MB of
API records through aless, in both the TypeScript engine and the Rust
port, with or without pruning the streamed elements from the value tree.
About 1.8 KB stays behind for each element of a flat array. The value is
not what stays; the rule history is.

Every rule that hands control on links what it leaves behind:

- A **replace** (`r`) sets the incoming rule's `prev_rule` to a snapshot
  of the rule it replaces (`rs/src/parser.rs`, the replace arm), and the
  snapshot carries its own `prev_rule`: every link holds the one it
  replaced, back to the first item of the sequence.
- A **push** (`p`) sets the child's `parent_rule` to a snapshot of the
  pusher, and freezes the pusher's `child_rule` on the child's record
  when the child stops being current; a child's snapshot carries the
  parent snapshot, and through it the parent's `prev_rule` chain.
- The links are `Rc<RuleSnapshot>` (a shared record of twenty fields,
  eight of them reference-counted handles); a snapshot lives as long as
  any link reaches it.

So a repetition compiled as a same-depth replace loop, the fleet's rule,
keeps rule depth constant and still keeps every iteration's snapshot
alive until the enclosing container closes: the current item's
`prev_rule` chain is the whole sequence, and every child pushed along
the way reaches the same chain through `parent_rule`. An experiment
that kept one step of `prev_rule` changed the peak by nothing, because
the child snapshots' `parent_rule` links reach the chain from the side.
`RuleSnapshot`'s `Drop` walks the chain iteratively for the same reason:
a flat array of 150,000 numbers builds one link per element.

## What the chain serves

A bound must keep everything a grammar or the engine reads through the
links:

1. **Condition and value paths.** `c: {"prev.n.count": 1}`,
   `"parent.name"`, `"child.node"`, `"next.name"` resolve through
   `resolve_snapshot_path`, one hop per path segment, and a path can
   chain hops. The fleet's grammars and the engine's fixtures reach at
   most three hops (`child.child.parent.name`, `next.next.name`,
   `child.next.name`, `child.parent.name`); no grammar reads
   `prev.prev`. A survey over every `rs/`, `ts/src` and `go` tree in the
   checkouts found those and nothing deeper.
2. **The forward walk the Rust port plans.** `DIVERGENCE.md` registers
   `chain-next-two-hops` and `chain-next-three-hops`: a stored forward
   pointer cannot be rewritten once shared, so item R7 of
   `rust-callback-contract-spec.md` resolves a successor by walking
   `prev_rule` backwards instead. That walk needs as many `prev` links
   as the hops a grammar asks for, three today.
3. **The debug-only consistency check.** `Context::sync_rule_stack`
   compares a buried frame's snapshot with the rule by identity of its
   links (`same_link` is `Rc::ptr_eq`). A bound that rewrites a link
   must rewrite it before the snapshot the context keeps is taken, or
   the check reports drift on a legitimate parse.
4. **Rewind** is separate: `options.rewind.history` (default 64) bounds
   the token window a rule can rewind to, not the rule links.

## The design

An option, `options.rule.history`, an integer from 1 to 16 or `null`
(today's behaviour, and the default until every runtime carries the
option): the number of predecessor snapshots a rule can reach through
`prev`. Under it, the snapshot a replace or a push links is a bounded
copy:

- It keeps `history - 1` predecessors, each a bounded copy the same
  way, and the one past them none. The copy is a twenty-field struct
  with eight `Rc` clones, `history` of them per link; the parse loop
  already snapshots a rule several times per step. Because every link
  copies up to `history` snapshots, the bound is capped at 16
  (`MAX_RULE_HISTORY` in the Rust port): a grammar that asks for more
  is refused, and a larger value set on the options directly is read as
  16. Uncapped, a bound as long as the sequence copied `1 + 2 + … + N`
  snapshots over `N` items, and a linear parse became quadratic. Sixteen
  is five times the deepest walk the fleet reads, and the largest bound
  measured faster than none (the table below).
- It keeps its `parent_rule` (the pusher and, through it, the pusher's
  own parents: a chain bounded by nesting). A replaced rule's copy
  drops its `child_rule` and `next_rule`, and so does every
  predecessor's. Those links are what defeats a cut of the predecessors
  alone: a rule's `child` is its finished child, whose `next` leads
  back to the rule's own record, whose `prev` is a copy of its
  predecessor, whose `child` does the same, a ladder that reaches back
  through the whole sequence. The first prototype measured it: with the
  predecessors cut and the child links kept, peak memory went UP (the
  copies beside the originals); with both cut it fell tenfold. Keeping
  the links on the nearest copy alone is not enough either: what a rule
  could reach then grew by thirteen snapshots per item of the array.
- A pusher's copy, the `parent_rule` a pushed child links, keeps its
  own `child_rule` and `next_rule`: they hold the child as it stood when
  pushed, before it has a `next`, so no ladder starts there. Its
  predecessors drop theirs. The engine also copies the pusher once
  before it links the child, and that copy survives as the parent of
  the child's own snapshot; it drops them too. Its `child` is the
  pusher's previous child, finished, and a rule that pushes again from
  its close phase would otherwise link every child it pushed, each
  through the one before: measured, five snapshots per item under every
  bound, past the unbounded parse.
- A copy that drops its `next_rule` drops its name too. A snapshot
  whose next has its own name reads itself as `next`, so a replacement
  loop's copy that kept the name read `prev.next` as `prev`.
- What a rule reads is kept: through `prev`, the predecessor's counters,
  values, tokens, node and name, and its own `prev` up to the bound;
  through `parent`, the pusher and its parents, `parent.child` and
  `parent.next` included. `prev.child` and `prev.next` resolve to
  nothing, as `prev` on a rule with no predecessor does today, and so
  do `parent.child.parent.child` and the like, the pusher before it
  linked the child; no grammar in the fleet reads them. A pusher's copy
  keeps `history - 1` predecessors like any other, so `history: 1` cuts
  `parent.prev` too. `history: 3` serves every path the fleet reads and
  R7's walk; `history: 1` serves a grammar that reads one hop.

What a rule can reach is then a constant: over the strict-JSON fixture
grammar, thirteen snapshots at `history: 3` whether the array has 500
or 2,000 elements, against one per element without the bound. Peak
retention becomes O(nesting × history) records instead of O(items), so
a flat document of any length is parsed in memory proportional to its
deepest container and to the value it builds (which the transducer's
pruning already bounds for a stream).

Parity: the option ships in all three runtimes, TypeScript first as the
canonical engine, with `test/spec` rows that pin what a cut chain
answers (`prev.prev.name` under `history: 1` is absent) and a fixture
that parses 10,000 items under `history: 3` while a rule subscriber
records the number of live snapshots, which must stay under a constant.
The Rust port carries it first, as this design's prototype, so the
option is registered in `DIVERGENCE.md` ("A bounded rule history in
Rust") and in `test/spec/divergent.tsv` as `rule-history-bounded` with
`rule-history-control` as its control: `prev.prev.name` read two
replacements back resolves in every runtime with the option unset, and
under `history: 1` resolves in TypeScript and Go, which do not carry the
option, and not in Rust. `rule-history-bounded-child` and
`rule-history-bounded-next`, with their controls, register
`prev.child.name` and `prev.next.name` the same way, and
`rule-history-past-the-cap` the refusal of a bound past 16, which the
other two install. The group is deleted when the other two runtimes
implement it.

## Measurements

The Rust port, release build, on a 4-core container with other builds
running (`rs/tests/rule_history_test.rs`, the ignored measurement, peak
resident memory from `VmHWM`). The strict-JSON fixture grammar in every
row; the value parsed is the same under every setting.

| Document | `rule.history` | Time | Peak RSS |
|---|---|---|---|
| 300,000 numbers in one array, 1.9 MB | unbounded | 2.8 s | 553 MB |
| the same | 3 | 1.2 s | 57 MB |
| the same | 1 | 1.0 s | 57 MB |
| the same | 8 | 1.8 s | 56 MB |
| the same | 16, the cap | 2.2 s | 56 MB |
| 312,194 API records in one array, 25 MB | unbounded | 223 s | 2,724 MB |
| the same | 3 | 16 s | 795 MB |

The array rows at no bound, 3, 8 and 16 were measured again once the
pusher's copy kept its `child` and `next` links and the cap came in;
the memory did not move. Past the cap the copies cost more time than
the bound saves: 3.3 s at 32 and 6.2 s at 64, against 2.8 s with no
bound, for the same 56 MB. That is why the cap is 16.

The records rows ran while a TypeScript suite and six audit builds
shared the four cores (load average above ten); the same file parsed
unbounded in 29 s on a quiet box earlier the same day, so read the
time column as a ratio at best. The memory column does not depend on
the load: what remains at `history: 3` is the value built (22.8 MB of
JSON text as a tree) and the bounded links.

The time falls with the memory: the unbounded parse spends a third of
its time allocating and, at the end, walking the chain to free it.
The first prototype, which cut the predecessors and kept the child
links, measured 906 MB and 2.6 s on the array row, above the unbounded
peak: the ladder above.

## Proposed decision (for admin `DECISIONS.md`)

**ADR: the engine bounds its rule history.** The rule links a parse
keeps are bounded by `options.rule.history`, the number of predecessor
snapshots a rule can reach; the default is unbounded until every runtime
implements the option, then 3. A grammar that needs a deeper walk sets
the option; nothing else observes the cut. Memory for a document is then
proportional to its nesting and its value, never to its length.
