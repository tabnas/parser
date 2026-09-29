# Lowering the per-rule cost

What the Rust port spends on each rule it creates, where that goes, and a
set of changes that remove most of it without changing anything a
grammar reads. This is the second throughput change. The first,
[`rule-history-bound.md`](rule-history-bound.md), made peak memory follow
a document's nesting instead of its length; it left the time per rule
where it was. Background:
[`rust-callback-contract-spec.md`](rust-callback-contract-spec.md)
("split the borrow"), which removes some of the same copies by a larger
route and waits on the maintainer; `DIVERGENCE.md` for the parity
register; `ci/bench/PROFILING.md` for the harnesses this repository
already carries.

Every figure names its input, its build, its `rule.history` setting and
the load it ran under; the conditions are under "Conditions" below. Line
numbers are against `main` at `e58c4bf`. Nothing here was measured in
TypeScript or Go, and no comparison with them is claimed.

## The problem

With the history bounded, the Rust port parses compact JSON records at
2.4 to 2.8 MB/s. A record of eight members takes 31 rules, so the rule is
the unit of cost: about 2.3 µs, 20,400 instructions and 32 heap
allocations per rule. Nearly half of those allocations, and 60% of the
bytes, serve the engine's own bookkeeping of rule records, not the value
it builds.

The profile's timings, release build, median of three runs (sitting P):

| Input | `rule.history` | Median | Per rule | Per step | Peak (`VmHWM`) | Load (1, 5, 15 min) |
|---|---|---|---|---|---|---|
| records, 2 MB | 3 | 0.714 s | 2,303 ns | 1,152 ns | 56.7 MiB | 0.61 0.64 0.46 |
| records, 2 MB | unset | 0.759 s | 2,450 ns | 1,225 ns | 118.6 MiB | 0.61 0.64 0.46 |
| records, 25 MB | 3 | 10.60 s | 2,715 ns | 1,357 ns | 665.6 MiB | 0.72 0.67 0.47 |
| records, 25 MB | unset | 13.36 s | 3,422 ns | 1,711 ns | 1,445.6 MiB | 0.94 0.73 0.51 |
| flat, 2 MB | 3 | 1.075 s | 1,791 ns | 895 ns | 47.3 MiB | 0.94 0.73 0.51 |
| flat, 2 MB | 1 | 0.978 s | 1,630 ns | 815 ns | 47.1 MiB | 0.94 0.73 0.51 |
| flat, 2 MB | unset | 2.37 s | 3,948 ns | 1,974 ns | 552.8 MiB | 0.96 0.75 0.52 |

A record costs 30.98 rules and 61.97 steps (an open and a close pass per
rule): 14 `val`, 11 `pair`, 3 `elem`, 2 `map` and 1 `list`, over 49.98
tokens, at a deepest rule depth of 9. An element of the flat array costs
2 rules (`elem` and `val`), 4 steps and 2 tokens. The counts do not
change with the history setting.

The 25 MB runs spread widely: 9.45 to 12.74 s at history 3 and 12.1 to
18.1 s unbounded. User time was 9.1 to 10.9 s at history 3 and 9.9 to
14.4 s unbounded, system time 0.3 to 3.7 s; minor faults were 163,000 at
history 3 and 363,000 unbounded. A fat-LTO build, as aless ships it,
measured at or below the low end of the release spread (25 MB records at
history 3: 9.13 s median, runs down to 8.89 s, load 0.97 0.85 0.59).

Instructions and allocations do not depend on the load. Over the parse
of the 2 MB records at history 3:

- callgrind counted 6.31 G instructions: 20,371 per rule and 631,000 per
  record. The flat array at history 3 took 16,310 per rule. The
  first-level data-cache miss rate was 0.9%, so the parse is bound by the
  instructions it executes, not by memory.
- A counting allocator active only during `parse()` counted 994.31
  allocations and 94,120 bytes requested per record: 32.1 allocations and
  3,038 B per rule, plus 11.43 reallocations per record. Unbounded, the
  same input took 27.4 allocations and 2,532 B per rule. The flat array
  took 23.5 allocations per rule at history 3, 20.5 at history 1 and 17.5
  unbounded.
- The allocator's own code (glibc `malloc.c` and the Rust shims around
  it) is 31.2% of all instructions, about 6,400 per rule. `memcpy` is
  another 6.8%.

Over the whole program, the bounded copies of history 3 cost about 1,500
instructions per rule (20,556 against 19,052 unbounded, records 2 MB).
History 3 is still the faster setting on large inputs, because it
touches far less memory.

### Conditions

The engine is `rs/` at `e58c4bf`, unedited. The harness, the input
generator and the prototype patches are in `ci/bench/rulecost/` and
`ci/bench/profl/`, and `ci/bench/PROFILING.md` says how to run them.
They were rebuilt from the repository and reproduce the baseline on the
2 MB records at history 3: 20,401 instructions per rule against 20,397,
994.31 allocations per record exactly, and 16,092 instructions per rule
under mimalloc against 16,053. The
parser is built as `rs/tests/rule_history_test.rs` builds
`json_parser()`: the grammar in `ts/test/json-builder.fixture.json`,
`options.rule.start` set to `"val"`, then `options.rule.history` set to
the row's value. The build is the release profile at opt-level 3 with
debug info, `codegen-units = 1`, no LTO, and the glibc allocator: what
a binary gets when it chooses neither, aless among them. A second
profile with fat LTO was used for one set of timings, marked "fat LTO".
A third, fat LTO with mimalloc, is the configuration `rs/README.md`
recommends measuring in and every other harness in `ci/bench` uses; its
counts and timings are marked "mimalloc". Type sizes in the first build: `RuleSnapshot` 152 B (168 B with
its `Rc` header), `Token` 192 B, `Rule` 96 B, `AltMatch` 320 B,
`Context` 520 B.

The inputs come from one generator script with one seed:

| Input | Bytes | Content |
|---|---|---|
| records, 2 MB | 1,959,364 | one compact array of 10,000 records; each has an integer id, two strings, a float, a boolean, a null, an object of three members and an array of one to three strings |
| records, 25 MB | 25,004,970 | 126,000 records from the same generator and seed; its first 10,000 are the 2 MB file's |
| flat, 2 MB | 1,988,891 | the integers 0 to 299,999 in one compact array |

Timings are of `parser.parse(src)` alone, on a 4-core container, in
round-robin runs of one process each, with `/proc/loadavg` read before
and after every run. The harness occupies one core itself. Steal time
was zero, or one tick in a handful of runs. Six sittings produced the
timings:

| Sitting | What it timed | Runs | 1-minute load before each run |
|---|---|---|---|
| P | the profile's baseline, release, and one fat-LTO set | 3 per row | 0.61 to 0.98 |
| E | the baseline and the three single-change builds, release, before the combined build existed | 3 per row, 84 in all | 0.78 to 1.00 |
| A | the baseline and the four prototype builds, release | 3 per row, 105 in all | 0.47 to 1.39 (5-minute 1.24 to 1.63) |
| U | the two unbounded cases again, release | 5 per row, 50 in all | 0.42 to 1.03 |
| L | the baseline and the combined prototype, fat LTO | 3 per row | 0.50 to 0.97 |
| M | the baseline and the four prototype builds, mimalloc | 3 per row, 105 in all | 0.88 to 2.03 |

Where two sittings disagree on the sign of a wall-clock difference, the
difference is unresolved and the instruction and allocation counts are
the evidence. The profile's first pass, a smaller set run while the
harness was built, is not reported.

Instruction counts are callgrind's `Ir` over the parse call. They do not
depend on the load, but they are not exact: two counts of one binary
differed by up to 1.3%, so a saving under about 4% counted once is not a
claim on its own. Allocation counts come from the counting allocator, and
allocation sites and stacks from dhat; those repeat exactly. The
harness, its scripts and the raw outputs are kept in the same scratch
area as the prototypes (`prof/harness`, `prof/out`, `prof/out-ab`), not
committed.

## Where the cost goes

Inclusive instruction shares, records 2 MB, history 3 (callgrind). The
shares overlap, because an inclusive cost counts its callees.

| Where | Site | Share | What |
|---|---|---|---|
| Freeing rule records | `RuleSnapshot`'s `Drop`, `rs/src/rule.rs:1347-1411` | 18 to 23% | 1.2 M `Rc::drop_slow` calls; the range depends on how inlined frames are charged |
| Lexing | `ensure_lookahead`, `rs/src/parser.rs:1795-1931` | 14.6% | about 1,860 instructions per token |
| Actions | `run_action_with_config`, `rs/src/parser.rs:928-966` | 13.7% | 3.6 points of it free the record that `set_rule` (`parser.rs:935`) displaced |
| Parse loop, own code | `Parser::parse_inner`, `rs/src/parser.rs` | 10.9% (self) | 2,246 instructions per rule |
| Copying rule records | `impl DerefMut for Rule`, `rs/src/rule.rs:1438` | 9.3% | 1,799,108 copies of about 5.8 M calls, 5.8 copies per rule |
| Bounded links | `bounded_history`, `rs/src/parser.rs:4374-4409` | 7.5% | 509,757 calls, 1.65 per rule |
| Tokens | `rs/src/parser.rs:2409` clone, `2459` collect, and their drops | about 9% | 3.3, 3.6 and 1.7 points (8.6%), plus 2.3 to free the matched vectors |
| First-token index | `AltIndex::named`, `rs/src/parser.rs:397` | 1.3% | a SipHash of an `i32` per step |
| The allocator | glibc and the Rust allocation shims | 31.2% (self) | about 6,400 instructions per rule |
| `memcpy` | | 6.8% (self) | moves of 152-byte records, 192-byte tokens and 320-byte match records |

Allocation sites, records 2 MB, history 3 (dhat):

| Cause | Site | Per record | Per rule | Share | Bytes per record |
|---|---|---|---|---|---|
| Rule record copy-on-write | `rs/src/rule.rs:1438` | 179.91 | 5.81 | 17.9% | 30,225 |
| Matched-token vectors | `rs/src/parser.rs:2459` | 157.94 | 5.10 | 15.7% | 13,376 |
| Building the value | `rs/src/builtins.rs` (`map_insert` at `:410` and others) | 110.95 | 3.58 | 11.0% | 5,751 |
| Bounded link copies | `rs/src/parser.rs:4406` | 103.96 | 3.36 | 10.3% | 17,465 |
| Scratch vector in `Drop` | `rs/src/rule.rs:1375` | 92.61 | 2.99 | 9.2% | 2,964 |
| `u` maps | `rs/src/rule.rs:1505`, `rs/src/parser.rs:2656`, `:2933` | 88.00 | 2.84 | 8.7% | 11,704 |
| Token text and values | `rs/src/lexer.rs` | 81.40 | 2.63 | 8.1% | 571 |
| Lookahead token clone | `rs/src/parser.rs:2409` | 76.97 | 2.48 | 7.7% | 234 |
| Scratch vector per bounded link | `rs/src/parser.rs:4387` | 50.98 | 1.65 | 5.1% | 1,223 |
| The new rule itself | `rs/src/rule.rs:1574` | 30.98 | 1.00 | 3.1% | 5,205 |
| Route names | `rs/src/parser.rs:2743`, `:2781` | 30.98 | 1.00 | 3.1% | 108 |

Building the value, the work the parse exists to do, is 11% of the
allocations. The rule-record machinery (copy-on-write, bounded copies,
both scratch vectors and the new rule) is 458 of dhat's 1,006 blocks per
record (46%) and 60% of the bytes. On the flat array at history 3 it is
66% of allocations and 79% of bytes.

### Why rule records are copied

A rule keeps its state in an `Rc<RuleSnapshot>`, and every write goes
through `impl DerefMut for Rule` (`rs/src/rule.rs:1436-1440`), which is
`Rc::make_mut`. When any other handle on the record exists, the write
copies the whole record first. The engine makes those handles itself:

- At the head of every step, `context.set_active`
  (`rs/src/parser.rs:2037`, `rs/src/context.rs:305-360`) publishes
  `context.rule = Some(rule.snapshot())`.
- Before every callback, one of 19 `set_rule` sites publishes again,
  including one before every named action (`rs/src/parser.rs:935`),
  builtins included.
- A published record is held until the next publish, so the engine's
  next write to the rule copies it.
- The four links (`parent_rule`, `child_rule`, `prev_rule`, `next_rule`)
  and the published `rule_stack` hold the other handles.

Which writes copy, per record (dhat callers, records 2 MB, history 3):

| Write | Site | Copies per record | Second handle |
|---|---|---|---|
| matched tokens into `o` | `rs/src/parser.rs:2482` | 30.98 | the step-head publish |
| matched tokens into `c` | `rs/src/parser.rs:2484` | 30.98 | the step-head publish |
| `@reset$` writes `rule.node` | `rs/src/builtins.rs:368` | 13.99 | the action's publish |
| `@key$` writes `u` | `rs/src/builtins.rs:393` | 11.00 | the action's publish |
| `@value$` writes `rule.node` | `rs/src/builtins.rs:352` | 10.99 | the action's publish |
| `next_rule_name`, replace arm | `rs/src/parser.rs:3339` | 10.99 | a publish |
| `next_rule_name`, pop arm | `rs/src/parser.rs:3426` | 3.00 | a publish |
| other builtins writing `rule.node` | `rs/src/builtins.rs:347`, `:356`, `:362` | 6.00 | the action's publish |
| the pushed child's `parent_rule` | `rs/src/parser.rs:3304` | 19.99 | the pusher's `child_rule` |
| `accept_child` on the resumed parent | `rs/src/rule.rs:1797`, from `parser.rs:3457` | 19.99 | the published stack and the popped rule's `next_rule` |
| the replacement's `prev_rule` | `rs/src/parser.rs:3374` | 10.99 | the replaced rule's `next_rule` |
| a rule linked to itself as `next` | `rs/src/parser.rs:3395` | 10.99 | its own snapshot |

The first eight rows, 117.93 copies per record, exist only because a
publish is still held when the engine writes. The last four, 61.96, come
from links that conditions and Rust callbacks can read.

Tokens are copied to be looked at. `rs/src/parser.rs:2409` clones a whole
192-byte `Token`, with the `String` of a string value, for every slot of
every alternate tried, to read one `i32`. Every matched alternate then
collects cloned tokens into a new `Rc<Vec<Token>>`
(`rs/src/parser.rs:2458-2459`), including the 17 steps per record that
match no token and allocate an empty one.

Two scratch vectors are allocated on the heap for walks that need none.
`bounded_history` allocates `Vec::with_capacity(history)` for every link
(`rs/src/parser.rs:4387`), and a push makes two links that copy the same
predecessors (`:3262`, `:3304`). `RuleSnapshot`'s `Drop` pushes to a heap
vector (`rs/src/rule.rs:1375`) for any record holding more than one link,
even when every one of them is still held elsewhere.

Peak memory at history 3 is no longer the rule history. At dhat's peak on
the 2 MB records, 25.2 of 51.1 MB live (49%) are the lexer's
per-character tables (`Lexer::with_shared`, `rs/src/lexer.rs:97-127`: a
`Vec<char>` and a `Vec<usize>`, 12 B per character, grown by doubling).
On the 25 MB records the counting allocator's peak live heap was 700 MB,
of which about 400 MB are those tables and 270 MB the value tree.
`Context::new` copies the source once more (`rs/src/context.rs:220`).

TypeScript and Go have no counterpart to any of these copies. They store
the current rule by reference (`ctx.rule = rule`, `ts/src/parser.ts:244`;
`ctx.Rule = rule`, `go/parser.go:564`), read lookahead tokens by
reference (`ts/src/rules.ts:1460`; `go/rule.go:1596-1643`), and reuse a
rule's `o` and `c` arrays.

## The proposed changes

Ranked by the measured instruction saving at the default setting
(history unset) on the 2 MB records, then by estimate at history 3, with
the one observable change last. Changes 1 to 3
were prototyped; the figures in this table are the instruction and
allocation counts. Changes 4 to 8 were not, and their figures are
estimates from the profile's per-site costs.

| Rank | Change | Instructions per rule, records 2 MB | Allocations per record, history 3 | Observable | TypeScript and Go |
|---|---|---|---|---|---|
| 1 | Publish the rule only while a callback runs | -14.7% unset, -14.0% at 3 (measured) | -22.0% (measured) | no | nothing |
| 2 | Test the lookahead tin in place | -8.4% unset, -7.8% at 3 (measured) | -10.1% (measured) | no | nothing |
| 3 | Bounded links without scratch vectors, one shared tail | -3.1% unset, -8.4% at 3 (measured) | -17.8% (measured) | no | nothing |
| 4 | Leave the match record unfilled when nothing reads it | about -2.4% (estimate) | about -6% (estimate) | no | nothing |
| 5 | Lexer dispatch tables and a number fast path | about -2.3%, flat about -4.2% (estimate) | about one per number (estimate) | no | nothing |
| 6 | Skip an empty after phase and the idle ruleDone plumbing | about -2.3%, flat about -2.9% (estimate) | none | no | nothing |
| 7 | No per-character tables for an ASCII source | about -0.8% (estimate) | peak memory, see below | no | nothing |
| 8 | A fixed token carries no value | about -2.3% (estimate) | about -6% (estimate) | yes, to grammars | a shared spec row first |

The three measured savings came close to their predictions: about 14% for
change 1 (measured 14.0%), 6 to 7% for change 2 (7.8%), and 5.7 to 8.9%
for change 3 (8.4%), all at history 3. That is some evidence for the
method behind the estimates, not a measurement of them.

### 1. Publish the rule only while a callback runs

The parse computes one flag, `lazy_rule`: it is set when nothing but a
callback that publishes for itself can read `context.rule`. That is, no
lex subscriber, no ruleDone subscriber, no imperative or factory lexer
matcher and no imperative text modifier; those four are handed the
context with no publish of their own. Under the flag:

- the step head still publishes for parse guards, the budget check and
  rule subscribers, and lets go once the subscriber loop ends (after
  `rs/src/parser.rs:2152`);
- every callback that `set_rule` publishes for lets go when it returns;
- `run_action_with_config` (`rs/src/parser.rs:935`) publishes nothing
  for a builtin: builtins never read `context.rule`, a plain action is
  handed the rule alone, and a context action publishes its own record
  in `run_context_callback` (`:835`);
- `token_value` (`rs/src/builtins.rs:199-203`) publishes just before a
  token's lazy `val_fn`, the one piece of user code a builtin reaches;
- the before-phase `next` snapshot (`rs/src/parser.rs:2191`) is taken
  only when a binding in the phase is a state action, the only kind that
  receives it.

The engine never reads `context.rule`; the only other `.rule` read in
`rs/src` is `TabnasError`'s own field. With nothing held between
callbacks, the writes in the first eight rows of the table above land in
place. The prototype is about 100 changed lines, and adds 8 B to
`Context`.

It saves 2,851 instructions per rule on the 2 MB records at history 3
(-14.0%), -14.7% unbounded, -13.3% on the flat array at history 3 and
-15.8% on it unbounded. Allocations fall by 218.9 per record (-22.0%):
the 117.93 record copies, exactly the drop in the 129-to-256-byte size
class; 33 `u`-map allocations behind `@key$`'s copy; and 68 of the `Drop`
scratch vectors, which the displaced copies needed because they carried
more than one link. Wall clock in sittings A and E: -12.9% and -13.0% on
the 2 MB records at history 3, -13.8% and -12.0% on the 25 MB records at
history 3, -14.3% and -16.8% on the flat array at history 3, -14.7% and
-9.4% at history 1.

No grammar can see it, and no register row changes. Every callback still
enters with `context.rule` holding a snapshot of the live rule. A
differential probe logged `context.rule` and the live rule at every
condition, context action, matched action, state action, rule
subscriber, parse guard and lazy token value, over five inputs at
histories unset, 1 and 3; its 3,996 lines are byte-identical to the
baseline's. A `debug_assert` that nothing is published where the engine
writes `o` or `c` held across the whole test suite. What remains is the
lifetime of an `Rc`: a `Weak` taken from `context.rule` now expires at
the rule's next write instead of at the next publish. That belongs in a
line of the Rust changelog.

The flag has a reach. A parser that installs any of the four readers
keeps today's cost. A text search of `rs/src` in the fleet's checkouts
on this machine, not a run, finds the flag set for json, jsonc, json5,
jsonic, jsonl, expr, css, abnf, bnf, ebnf, gbnf and directive, and
cleared for alchemy, c, chess, csv, hoover, ini, markdown, toml, xml,
yaml and zon (imperative matchers, matcher factories or lex
subscribers), for the debug tracer and the lsp crate, and for the
transducer's rule-event source (`subscribe_rule_done` in
`transduce/rs/src/source/rule_events.rs`), which is the streaming path.
Widening it is an API question, under "What waits on the maintainer".

TypeScript and Go need nothing: their `ctx.rule` and `ctx.Rule` are
references, and nothing is copied. The risk is a missed publish, which
would hand a callback `None` where it expects a record. The entries are
enumerable: the 35 `catch_callback` sites in `rs/src/parser.rs`, the
action maps in `run_action_with_config`, and `Token::resolve_val`. If
ruling R9's `Action` signature, which hands a plain action the context,
lands while `context.rule` still exists, `run_action_with_config` must
publish for plain actions too (an `Rc` clone and drop, no copy). Two
gaps to close before landing: the step head publishes and lets go on
every step even with no step-head reader (an `Rc` clone and drop, no
copy), and a callback that returns an error through `?` leaves its record
published until the next publish (time only).

### 2. Test the lookahead tin in place

`rs/src/parser.rs:2409` reads `.tin` instead of cloning the token, and
clones `context.t[pos]` only inside the relex branch (`:2414-2454`),
before relex overwrites it; that branch is the copy's one reader. An
alternate that matches no token shares the empty list every new rule
already starts with (`empty_tokens` in `rs/src/rule.rs`, made
`pub(crate)`), at `:2458-2459` and `:2611-2613`. `token_value`
(`rs/src/builtins.rs:199-203`) borrows the token through its own handle
on `rule.o` instead of cloning it. The first-token index (`AltIndex`,
`rs/src/parser.rs:366-400`) keeps tins below 1,024 in a vector, with the
map as the fallback above. The prototype is about 90 changed lines.

It saves 1,582 instructions per rule on the 2 MB records at history 3
(-7.8%), -8.4% unbounded, -5.9% on the flat array at history 3 and -9.7%
on it unbounded. Allocations fall by 99.94 per record (-10.1%, against
99.95 predicted) and by 4 per element on the flat array. Wall clock in
sittings A and E: -4.9% and -6.8% on the 2 MB records at history 3,
-3.0% and -5.8% on the 25 MB records at history 3, -6.3% and -9.0% on
the flat array at history 3, -10.0% and -2.1% on the 2 MB records
unbounded. On the flat array at history 1 it measured -6.9% in sitting A
and +3.8% in sitting E, and unbounded -20.1%, -7.7% and +25.4% in
sittings A, U and E: unresolved, so the instruction counts are the
evidence there. Peak
memory falls slightly when unbounded, because the records a parse keeps
share the empty list: 552.8 to 525.4 MiB on the flat array.

Nothing is observable. Only the tin is read outside the relex branch,
and that branch clones the same token as before, so its undo still
restores the original. The shared empty list differs from a fresh one
only under `Rc::ptr_eq`. The dense index keeps first-match-wins order
(the named and wildcard lists merged as before) and gives an unknown tin
no alternates, as a missing key does now.

TypeScript and Go need nothing. They read the lookahead by reference and
test a bitset (`ts/src/rules.ts:1565-1577`) or a pointer
(`go/rule.go:1596-1643`). Their first-token indexes are map lookups
(`ts/src/rules.ts:1391-1438`, `go/altindex.go:58-82`); a dense table
would be internal there too, and nothing requires it. The risk is low;
the relex tests pass.

### 3. Bounded links without scratch vectors, and one shared tail

`bounded_history` (`rs/src/parser.rs:4374-4409`) collects the links in a
stack array of `MAX_RULE_HISTORY` (16) slots instead of a vector, and
builds each copy field by field instead of cloning it and clearing its
links. A push copies the pusher's predecessors once and keeps them on the
rule, beside the `prev_rule` handle they came from. The push's two links
(`:3262` and `:3304`) and the replace that ends the rule (`:3374`) share
that tail while `prev_rule` is still the same handle (`Rc::ptr_eq`), and
copy as before otherwise. `RuleSnapshot`'s `Drop`
(`rs/src/rule.rs:1347-1411`) lets go of a link that another strong handle
also holds where it stands, which is a decrement and cannot recurse. It
walks only the links it owns last, on a four-slot inline stack that
spills to a vector. The prototype is about 210 changed lines, and grows
`Rule` from 96 to 120 B.

At history 3 it saves 1,704 instructions per rule on the 2 MB records
(-8.4%) and 2,293 on the flat array (-14.1%), which takes the flat array
at history 3 (14,017) below the same array unbounded (14,857).
Allocations fall by 177.24 per record (-17.8%): 92.61 `Drop` scratch
vectors, 50.98 link vectors and 33.66 shared copies. On the flat array
they fall by 27.7% at history 3 and 22.0% at history 1. Unbounded, where
only the `Drop` change applies, it saves 3.1% and 3.5% of instructions
and 9.5% and 14.3% of allocations. Wall clock in sittings A and E:
-10.4% and -8.3% on the 2 MB records at history 3, -12.0% and -14.3% on
the flat array at history 3, -6.9% and -5.5% at history 1.

The 25 MB records at history 3 measured +5.9% at the median in sitting A,
with runs of 11.08, 9.69 and 8.28 s against the baseline's 8.91, 9.14
and 9.95 s; median user time fell 7.6% while median system time rose
from 0.30 to 1.02 s. Sitting E measured -0.2%. That is page-fault time
on this machine, and the instruction count on the 2 MB input is the
signal. Unbounded wall clock is within noise: +0.4% and -2.9% on the
2 MB records (sittings A and E), +6.2%, -0.9% and +19.9% on the flat
array (sittings A, U and E), +23.4%, +2.4% and -1.9% on the 25 MB
records.

Nothing is observable. Every link reads the same values at every depth;
the `rule-history-*` register rows and `rs/tests/rule_history_test.rs`
pass, and the probe, which prints `prev` and `prev.prev`, is
byte-identical. Only `Rc::ptr_eq` and `Rc::strong_count` can tell a
shared tail from equal copies. A callback that rewrites `prev_rule`
falls back to a full copy: the cache holds the source handle, so a write
through it copies and the identity test fails. The walk still never
drops a link it owns alone inline. The flat array unbounded, the one
unbroken chain, completed in every release and fat-LTO run, and a
1,000,000-element unbounded chain dropped without overflow in a fat-LTO
build on an 8 MiB stack (through the combined build; load not recorded).

TypeScript and Go need nothing. `rule.history` is Rust-only today
(`DIVERGENCE.md`, "A bounded rule history in Rust"), and when the other
two carry it they will cut live links, with no copies to share. The
change pays where the bound is set, and nothing in the fleet's
checkouts sets it yet; until then its effect is the `Drop` change.

### 4. Leave the match record unfilled when nothing reads it

The engine already knows when nothing can read the match record: a
prepared alternate that is not observed and has no named binding that
could resolve to a matched action (`rs/src/parser.rs:2681-2692`). It
uses that test to skip publishing the action order. With the same test,
and also no modifier, no `p_fn` or `r_fn` and no ruleDone subscriber, the
step can read `alt.n`, `alt.u` and `alt.k` from the alternate where the
counters are applied today (`:2923-2939`) instead of cloning them into
`matched` at the fill (`:2651-2660`), so a plain `e` hook or a `b_fn`
the test allows sees the counters no earlier than it does now. It can
route from `PreparedRoute::Static` instead of cloning the rule's name
into `matched.p` or `matched.r` (`:2743`, `:2781`); a `ByName` route
carries no name, and is also what an alternate gets whose static route
names a rule that is not installed, so it keeps reading `alt.p` or
`alt.r`, by borrow, and still raises `unknown_rule` with the name.
`@setval$` can look up its slot by `&str` instead of allocating `"key"`
(`rs/src/builtins.rs:396-405`). The `matched.reset()` at
`rs/src/parser.rs:2295` stays: a step that hands the record out to
nothing still writes `g` (`:2675`), `action_configs` (`:2706`), `b`
(`:2787`, `:2817`) and `e` (`:2854`) into it, and the next step's
matched conditions (`:2503`, `:2522`) and its `e` check (`:2885`) read
the record before anything refills it.

Estimated from the profile, records 2 MB at history 3: the route names
cost 67.4 M instructions and 30.98 allocations per record, the `u` clone
of the pair alternate 59.5 M and 22, and the `"key"` string about 23 M
and 11. Together that is about 2.4% of instructions (about 150 M, less
the `ByName` routes that keep their borrow) and about 64 allocations per
record (6.4%). On the flat array, the route names are about 1% of
instructions and 2 allocations per element.

Nothing is observable where the test holds: nothing receives the record
between the fill and the counters and transition, and the reset keeps
one step's writes out of the next. A test of the change needs a step
whose record goes to nothing but which writes `g`, `b` or `e`,
followed by a step with a matched condition. Wherever a matched
hook, a matched action or a ruleDone subscriber exists, the record is
filled as today, so `matched.p` and `matched.r` stay the writable
routing channel. TypeScript and Go need nothing: they store references
(`out.u = alt.u`, `ts/src/rules.ts:1750-1767`) or read the alternate
directly (`go/rule.go:1251-1277`). The risk is a reader the test misses,
including recovery; `matched_actions` can change after `add_rule`, so
the test stays per step, as it is now.

### 5. Lexer dispatch tables and a number fast path

Built with the lexer: fixed tokens indexed by first byte, with their
character counts, in place of the scan of every fixed token per token
(`rs/src/lexer.rs:959-1018`); delimiters indexed by first character for
`is_text_delimiter_at` (`:451-477`); and a flag per check family, so
`run_check` is not called with a cloned `None` for every family on every
token. For numbers, `scan_number_digits` (`:1711-1745`) reports whether
it saw a separator, so the text is parsed directly when it did not, and
whether any `value.definitions` key can be spelled as a number is
decided once per grammar instead of hashing every number's text into
that map (`:1666`).

Estimated from the profile: about 146 M instructions (2.3%) on the 2 MB
records at history 3, and about 420 M (4.2%) on the flat array, where
`match_number` alone is 6.6% of the parse. The separator-free path saves
one allocation per number. The dense first-token index, which the same
proposal named, is already in change 2.

Nothing is observable provided the order and tie-breaks hold: first
match wins, and among fixed tokens the longest wins, the last of equals
as `max_by_key` picks it. TypeScript and Go need nothing; they already
index fixed tokens by first character (`ts/src/lexer.ts:481-505`) or
first byte (`go/lexer.go:1545-1573`).

### 6. Skip an empty after phase and the idle ruleDone plumbing

Each transition arm calls `run_after_actions` and `recover_after_actions`
(`rs/src/parser.rs:3271` and `:3284`, `:3341` and `:3354`, `:3396` and
`:3409`, `:3428` and `:3441`), passing a `Result<(), TabnasError>` by
value between them. Where the step's prepared rule is valid and its
after-action order is empty, both calls can be skipped, as can
`notify_rule_done` and the drop of an always-empty `done_alt` when there
are no ruleDone subscribers (`:3465-3483`); `update_partial` can be
inlined at its call sites. Estimated from the profile: 145.5 M
instructions (2.3%) on the 2 MB records at history 3 and about 288 M
(2.9%) on the flat array; about 61 M of the records' figure is the
`Result` moved by value. Nothing is observable: each skipped call returns
at once today. TypeScript and Go need nothing.

### 7. No per-character tables for an ASCII source

When `src.is_ascii()`, `Lexer::with_shared` (`rs/src/lexer.rs:97-127`)
builds neither table: character `i` is byte `i` and its offset is `i`.
The dozen readers in `lexer.rs` go through two inline helpers, and a
source with any other character keeps the tables.

This is a memory change. Estimated for the 25 MB records at history 3:
about 400 MB of the 700 MB peak live heap and about 73,000 of the
163,000 minor faults go, taking `VmHWM` from 665.6 MiB to about
380 MiB and system time down by about 0.6 s. On the 2 MB records, dhat's
peak live heap would fall from 51.1 MB to about 26 MB. Instructions fall
by about 0.8% (`with_shared` is 49.3 M on the 2 MB records). Nothing is
observable: in an ASCII source a byte offset and a character offset are
the same number, so the registered units (`si` in bytes, `pos` in
characters) hold. TypeScript and Go need nothing; neither builds
per-character tables.

### 8. A fixed token carries no value

`rs/src/lexer.rs:1014` gives a fixed token (`#CL`, `#CA`, `#OB` and the
rest) `Value::String` of its source. TypeScript gives it `undefined`
(`ts/src/lexer.ts:499`) and Go `nil` (`go/lexer.go:1556`, `:1568`). The
same split exists for `#SP` and `#LN` (`rs/src/lexer.rs:1026-1098`) and
`#CM` (`:1464-1470`), and should move in the same change.

Estimated on top of change 2, records 2 MB at history 3: 27.99
allocations per record at `lexer.rs:1014`, and about 34 more where those
tokens are cloned into matched vectors; about 62 per record and about
460 instructions per rule (6.2% and 2.3% of the unmodified baseline,
6.9% and 2.4% of the change-2 build). The records input has no
whitespace, so nothing is claimed for the whitespace tokens.

This one is observable. A grammar reads a fixed token's value through an
`o0.val` condition, `@value$` or `@key$` from a fixed token, a lex
subscriber or `rule.o` in an action, and the token's `Display` loses its
`=value` suffix. It repairs an unregistered Rust-only split toward the
canonical answer, so it lands TypeScript first: a shared spec row that
pins TypeScript's answer, which TypeScript and Go already pass, then the
Rust change in the same pull request or a register row until it lands.
The fleet's Rust crates need a read-only search for reads of a fixed
token's `val` first.

## What the prototypes measured

The three prototypes and their combination were each applied to a copy
of `e58c4bf`. The patches are `ci/bench/rulecost/patches/{publish,tin,
links,all}.diff`, and they apply unchanged to `main` at `595e954`, which
moved only version sites. Each build
passed `cargo test --release --locked --offline` in `rs/`: 59 test
binaries, 396 passed, none failed, 2 ignored, the same as the baseline,
both as built and with `CARGO_PROFILE_RELEASE_DEBUG_ASSERTIONS=true`,
which runs the engine's own self-checks and change 1's assertion. The
differential probe's output is byte-identical across all five builds.

Instructions per rule (callgrind, the parse alone, the first of two
counts per build). The baseline here, 20,397 on the 2 MB records at
history 3, is built from the same source as the profile's 20,371; the
difference is within callgrind's run-to-run variation. A second count of
every build, rebuilt from the same patches, moved each figure by 0.0 to
1.1% (the flat baseline at history 3 moved most) and each saving by at
most 1.4 points. The smallest savings held: change 3 unbounded measured
2.9% and 3.2% the second time, against 3.1% and 3.5% below.

| Input | `rule.history` | Baseline | 1. Publish | 2. Tin | 3. Links | All three |
|---|---|---|---|---|---|---|
| records, 2 MB | 3 | 20,397 | 17,546 (-14.0%) | 18,815 (-7.8%) | 18,693 (-8.4%) | 14,840 (-27.2%) |
| records, 2 MB | unset | 18,838 | 16,063 (-14.7%) | 17,255 (-8.4%) | 18,251 (-3.1%) | 14,501 (-23.0%) |
| flat, 2 MB | 3 | 16,310 | 14,137 (-13.3%) | 15,350 (-5.9%) | 14,017 (-14.1%) | 10,978 (-32.7%) |
| flat, 2 MB | unset | 14,857 | 12,505 (-15.8%) | 13,422 (-9.7%) | 14,332 (-3.5%) | 11,136 (-25.0%) |

Allocations per element (the counting allocator, the parse alone;
deterministic):

| Input | `rule.history` | Baseline | 1. Publish | 2. Tin | 3. Links | All three |
|---|---|---|---|---|---|---|
| records, 2 MB | 3 | 994.31 | 775.40 (-22.0%) | 894.37 (-10.1%) | 817.07 (-17.8%) | 566.19 (-43.1%) |
| records, 2 MB | unset | 848.38 | 621.14 (-26.8%) | 748.44 (-11.8%) | 767.76 (-9.5%) | 516.88 (-39.1%) |
| flat, 2 MB | 3 | 47.0 | 36.0 (-23.4%) | 43.0 (-8.5%) | 34.0 (-27.7%) | 23.0 (-51.1%) |
| flat, 2 MB | 1 | 41.0 | 30.0 (-26.8%) | 37.0 (-9.8%) | 32.0 (-22.0%) | 21.0 (-48.8%) |
| flat, 2 MB | unset | 35.0 | 23.0 (-34.3%) | 31.0 (-11.4%) | 30.0 (-14.3%) | 19.0 (-45.7%) |

Bytes requested per record on the 2 MB records at history 3 went from
94,120 to 59,354 (-36.9%) with all three. Peak live bytes during the
parse (4,911 per record) and the value kept afterwards (2,176 per
record) did not move.

Wall-clock medians in seconds, release, sitting A (three round-robin
runs per build; 1-minute load 0.47 to 1.39 before each run):

| Input | `rule.history` | Baseline | 1. Publish | 2. Tin | 3. Links | All three |
|---|---|---|---|---|---|---|
| records, 2 MB | 3 | 0.721 | 0.628 (-12.9%) | 0.685 (-4.9%) | 0.646 (-10.4%) | 0.537 (-25.4%) |
| records, 2 MB | unset | 0.750 | 0.721 (-3.8%) | 0.675 (-10.0%) | 0.753 (+0.4%) | 0.568 (-24.3%) |
| records, 25 MB | 3 | 9.142 | 7.879 (-13.8%) | 8.863 (-3.0%) | 9.685 (+5.9%) | 6.759 (-26.1%) |
| records, 25 MB | unset | 13.734 | 12.627 (-8.1%) | 13.067 (-4.9%) | 16.948 (+23.4%) | 14.237 (+3.7%) |
| flat, 2 MB | 3 | 1.051 | 0.901 (-14.3%) | 0.984 (-6.3%) | 0.925 (-12.0%) | 0.732 (-30.4%) |
| flat, 2 MB | 1 | 0.929 | 0.793 (-14.7%) | 0.864 (-6.9%) | 0.864 (-6.9%) | 0.701 (-24.5%) |
| flat, 2 MB | unset | 1.480 | 1.354 (-8.5%) | 1.183 (-20.1%) | 1.572 (+6.2%) | 1.148 (-22.4%) |

The two unbounded cases again, sitting U (five runs per build; 1-minute
load 0.42 to 1.03): on the 25 MB records the baseline took 11.854 s and
the builds +6.0%, +25.0%, +2.4% and -0.2%; on the flat array the
baseline took 1.327 s and the builds -3.3%, -7.7%, -0.9% and -18.1%. The
25 MB unbounded case is not resolvable on this machine. Its runs spread
from 9.9 to 18.8 s whatever the build, with system time from 1.8 to
5.9 s, and pooled over both sittings the eight-run medians of the four
builds lie between -4.9% and +3.2% of the baseline's. The instruction
counts on the 2 MB records are the signal for that mode.

Fat LTO, sitting L (three runs per build; 1-minute load 0.50 to 0.97),
baseline against all three:

| Input | `rule.history` | Baseline | All three |
|---|---|---|---|
| records, 2 MB | 3 | 0.644 | 0.564 (-12.5%) |
| records, 2 MB | unset | 0.663 | 0.624 (-5.9%) |
| records, 25 MB | 3 | 9.250 | 7.139 (-22.8%) |
| records, 25 MB | unset | 12.097 | 7.908 (-34.6%; +3.7% and -0.2% in sittings A and U, so not resolvable) |
| flat, 2 MB | 3 | 0.953 | 0.698 (-26.8%) |
| flat, 2 MB | 1 | 0.850 | 0.683 (-19.6%) |
| flat, 2 MB | unset | 1.254 | 1.052 (-16.1%) |

Mimalloc, fat LTO: instructions per rule (callgrind, one count per
build; the parse's count is the total less the same binary's count on
a two-byte input, because fat LTO inlines the parse into `main`. That
also counts reading the input and dropping the value, which put the
glibc baseline 0.8% above its parse alone where both could be read, so
these figures run slightly high, alike for every build):

| Input | `rule.history` | Baseline | 1. Publish | 2. Tin | 3. Links | All three |
|---|---|---|---|---|---|---|
| records, 2 MB | 3 | 16,053 | 13,965 (-13.0%) | 14,762 (-8.0%) | 15,071 (-6.1%) | 12,050 (-24.9%) |
| records, 2 MB | unset | 14,687 | 12,456 (-15.2%) | 13,370 (-9.0%) | 14,377 (-2.1%) | 11,242 (-23.5%) |
| flat, 2 MB | 3 | 13,141 | 11,489 (-12.6%) | 12,093 (-8.0%) | 11,774 (-10.4%) | 9,417 (-28.3%) |
| flat, 2 MB | unset | 11,483 | 9,612 (-16.3%) | 10,424 (-9.2%) | 11,196 (-2.5%) | 8,669 (-24.5%) |

The baseline runs 18% to 23% fewer instructions than under glibc without
LTO, and the changes keep most of their saving: all three together save
23.5% to 28.3% here against 23.0% to 32.7% there. Change 3 loses the most
(on the records at history 3, 6.1% against 8.4%), because a larger share
of what it removes is allocator work, which mimalloc does in fewer
instructions. The allocation counts above do not depend on the allocator.

Mimalloc, fat LTO: wall-clock medians in seconds, sitting M (three
round-robin runs per build; 1-minute load 0.88 to 2.03 before each run),
baseline against all three:

| Input | `rule.history` | Baseline | All three |
|---|---|---|---|
| records, 2 MB | 3 | 0.614 | 0.458 (-25.4%) |
| records, 2 MB | unset | 0.641 | 0.436 (-32.0%) |
| records, 25 MB | 3 | 11.094 | 7.881 (-29.0%) |
| records, 25 MB | unset | 10.082 | 7.093 (-29.6%) |
| flat, 2 MB | 3 | 0.943 | 0.707 (-25.0%) |
| flat, 2 MB | 1 | 0.850 | 0.712 (-16.2%) |
| flat, 2 MB | unset | 1.094 | 0.871 (-20.5%) |

The single changes are not resolved in this sitting: under the higher
load their medians change sign from one input to the next (change 1
measured -14.2% on the 2 MB records at history 3 and +23.1% unbounded),
so their instruction counts above are the evidence for them.

The three compose. Together they save 5,557 instructions per rule on the
2 MB records at history 3, against 6,139 for the three measured alone.
The overlap is in the frees both changes 1 and 3 touch: the 68 `Drop`
scratch vectors per record that either one removes are the whole gap
between the combined allocation saving (428 per record) and the sum of
the three (496). At history 3 the combination takes the 2 MB records
from 2.72 to 3.65 MB/s
and the 25 MB records from 2.74 to 3.70 MB/s (sitting A), with no change
to peak memory (`VmHWM` 56.5 MiB and 664.8 MiB against 56.5 MiB and
665.5 MiB).

## What is left out, and why

- The copies that come from links. The four rows of the copy table
  that remain after change 1 (`rs/src/parser.rs:3304`, `:3374`, `:3395`
  and `rs/src/rule.rs:1797`), 61.96 per record or 2.0 per rule, exist
  because conditions and Rust callbacks can read the links. At the
  profile's unit costs (about 240 to 330 instructions to make a copy and
  about 410 to free it) they are worth an estimated 1,300 to 1,500
  instructions per rule, 6 to 7%. Removing them changes link types or
  link semantics near registered splits (`chain-next-*`,
  `pusher-through-child`). That is the split-the-borrow design's work, and
  it waits on items R2 to R7 of that spec and on folding in the R1 and R9
  rulings of 2026-09-18.
- Linking a replaced rule's successor in place, by setting `next_rule`
  to `None` before the `prev` link at `rs/src/parser.rs:3374` when the
  record is uniquely held. It depends on change 1 and touches link
  semantics beside the `rule-history-next-control` row; estimated at
  about 0.9%. It was not attempted.
- Recovery. With `parse.recover.enabled`, each of the about six
  `update_partial` calls on an ordinary step (15 sites in all) walks the
  whole partial value (`rs/src/parser.rs:3800-3827`; the clone is a
  shallow `Arc` bump, the walk is `unwrap_undefined` into
  `contains_undefined`), which makes a recovering parse
  quadratic. The benchmark does not recover, so this profile cannot size
  it; it is a separate change with its own measurement. The same holds
  for the error path, where `TabnasError::new` copies the whole source
  per error.
- Costs this grammar barely exercises. These are the key clone on
  every `n`, `u` or `k` write, action names resolved per call instead of
  at install, declarative conditions evaluated on a cloned rule, and
  custom matchers cloned per token. The strict-JSON grammar has little
  or none of these, so a grammar that has them needs its own profile
  first.
- The allocator. The glibc allocator is 31% of the instructions. A
  library does not choose its embedder's global allocator; a binary such
  as aless could, as a dependency change on the maintainer's instruction.
  Under mimalloc with fat LTO the baseline runs 18% to 23% fewer
  instructions per rule, and the changes still save 23% to 28% of what
  remains (under
  "What the prototypes measured"): they cut the allocation count, which
  helps under either allocator, rather than the cost of one allocation.
- Downstream suites. Only `rs/` was tested against the prototypes.
  `ci/fleet/run-fleet.sh` runs with each change as it lands.

## Landing order, and what waits on the maintainer

Changes 1 to 7 are internal to the Rust port, so each lands in Rust
alone, with no register row and nothing to port. Change 8 is observable
and lands TypeScript first. The order, by risk and by what depends on
what:

1. Change 2, the tin. The lowest risk, and independent of the others.
2. Change 3, the links. Its `Drop` half pays at every setting and its
   history half where the bound is set. Before landing, shrink what it
   adds to `Rule` (24 B) without giving up the strong handle on the
   source: the tail is fresh copies, so the source cannot be read back
   from it, and without the handle a `prev_rule` held by the rule alone
   can be changed in place (same pointer, new content) and pass the
   `ptr_eq` test with a stale tail. Under change 1 a plain after-open
   action between `:3262` and `:3304` reaches exactly that. Keeping the
   pair behind one pointer (an `Option<Box<..>>`, 8 B on `Rule`, one
   allocation per push, only when a bound is set) keeps both.
3. Change 1, the publish, with the two gaps named in its section closed
   and a line in the Rust changelog about the `Weak` lifetime.
4. Changes 4, 6 and 5, each prototyped and measured before it lands, the
   same way.
5. Change 7, measured on the 25 MB records, where its effect is memory.
6. Change 8: the shared spec row in TypeScript first, then Rust, with the
   whitespace and comment token values in the same spec change.

Each lands with the repository's own gates, the `rule-history-*` and
spec register runners, `ci/fleet/run-fleet.sh`, and a before-and-after
measurement on this document's workload (`ci/bench/rulecost/` and the
`rulecost` binaries of `ci/bench/profl`) as well as the existing ones:
`ci/bench/profl` for instructions, `ci/bench/alloc` for peak memory, and
`ci/bench/ab.sh` for wall clock, where a difference under 3% is not a
claim on its own.

Four questions wait on the maintainer:

- The reach of change 1. Under the flag, the saving goes to parsers
  with no lex subscriber, ruleDone subscriber, imperative matcher or
  imperative text modifier: the JSON family and the BNF family on the
  survey above, but not yaml, toml, xml, csv, ini or markdown, and not the
  transducer's rule-event source. Publishing for each of those readers at
  its call, and letting go after, would extend the saving to all of them
  at the cost of an `Rc` clone per call, not a copy. It would also change
  what they read: the rule as it stands at the call, as TypeScript and Go
  give it, instead of the record from the last publish, which predates
  this step's `o` or `c` write and any builtin's write. That is a Rust
  API change. Ruling R9 of 2026-09-18 authorised a Rust-only API change
  for the plain `Action` type, with performance and memory as the bar;
  this is a different item and needs its own answer. The saving on
  those grammars was not measured.
- Change 1 against split the borrow. Change 1 is a subset of that
  design with no type change, and neither blocks the other; split the
  borrow would later remove `context.rule` and the published stack
  altogether. Whether change 1 lands first or is folded in is the
  maintainer's call.
- Change 8, because it is observable to grammars.
- The size of `Rule`, if the maintainer holds it fixed. Change 3 adds
  24 B per live rule as prototyped.

## Proposed decision (for admin `DECISIONS.md`)

**ADR: the Rust engine copies nothing for its own bookkeeping.** A rule
record, a token or a scratch buffer that the Rust engine copies only for
its own use, and that no grammar or callback reads, is a cost defect,
not behaviour: it is removed in the Rust port alone, with no register
row, and each removal is measured in instructions and allocations per
rule on the committed benchmarks. The engine holds `context.rule` only
while user code that can read it runs. A change to what a Rust callback
reads, such as a fresher `context.rule` for lexer-time and ruleDone
readers, is a Rust API change for the maintainer, judged on performance
and memory. A change a grammar can read, such as the value of a fixed
token, lands TypeScript first with a shared spec row.
