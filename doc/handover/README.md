# Handover: engine and port parity work, 2026-09-30

Work stopped on the maintainer's instruction partway through a set of
fixes across `parser`, `css`, `jsonic`, `csv`, `multisource`, `toml` and
`yaml`. This page records where each piece stands, the rulings the
maintainer gave, what is left, and the order it has to land in. Nothing
listed under "Local work" was pushed anywhere: the patches in
`patches/` are the only copy.

## Pushed and open

| PR | Branch head | State |
|---|---|---|
| tabnas/css#53, the Rust port rebuilt as a plugin on the engine | `ca762db` | CI green (13 checks), mergeable, no open review threads. Waits on the maintainer: the removals are breaking for Rust callers of the crates.io 0.5.9, versions move in lockstep with `ts/package.json`, and the engine costs speed and memory (figures in the PR body). |
| tabnas/parser#256, `doc/per-rule-cost.md` | this branch | Design document, the maintainer merges. By ruling (below), the engine fixes are to be added to this same branch. |

Both use the branch `claude/alchemy-handover-continuation-bil4x7`.

## Rulings the maintainer gave in this session

1. **Engine fixes go on parser#256's branch**, not a separate PR.
2. **The Go css string fix goes into css#53.**
3. **Merge of a removed fixed token: follow TypeScript**, in Rust and in
   Go. When both instances removed the same default fixed token,
   TypeScript's merge brings the default back (it rebuilds over the
   defaults); Rust and Go must do the same, and an instance built without
   defaults (`empty()`, TypeScript's `defaults$: false`) must not get
   them back.
4. **The three Go plugins broken by the jsonic/go alignment are to be
   fixed** (csv, multisource, toml), not papered over with a fallback in
   jsonic.
5. Standing: TypeScript is canonical, and dependencies change only on
   explicit instruction (each repository's `CLAUDE.md`).

## Local work, saved in `patches/`

Every patch was made against the base named, and the parser ones were
checked to apply to this branch with `git apply --check`, in order.

| Patch | Repository, base | State |
|---|---|---|
| `parser-1-engine-fix-bad-token.patch` | parser, `eb537df` | Committed (`c32ebfa`). Reviewed: one reviewer sound, one not (see open findings). Fleet-tested against all 33 other Rust crates. |
| `parser-2-engine-perf-UNVERIFIED.patch` | parser, `eb537df` | Committed (`36d6f76`). One of two reviewers finished: sound, one minor. The second review was stopped. |
| `parser-3a-engine-merge-first-attempt.patch` | parser, `eb537df` | Committed (`f6ead37`). Both reviewers: NOT sound (two majors). Superseded by 3b. |
| `parser-3b-engine-merge-rework-WIP-uncommitted.patch` | parser, on top of 3a | Uncommitted work in progress, stopped mid-way, not reviewed. Rust and Go. |
| `css-go-string-fix.patch` | css, `fedd1cf` | Committed (`7e985a7`). Both reviewers sound. Goes into css#53. |
| `css-rs-unclosed-comment-full-span-DRAFT.patch` | css, `fedd1cf` | Draft from the fleet run, applies to css#53's head. |
| `css-rs-recovery-test-update-DRAFT.patch` | css, `fedd1cf` | Draft: the recovery test's expectations under the fixed engine. |
| `jsonic-go-actions-fix.patch` | jsonic, `997d36d` (main) | Committed (`302f045`). Both reviewers: NOT sound, on landing order and four minors. |
| `csv-`, `multisource-`, `toml-go-plugin-alloc-WIP-uncommitted.patch` | each repo's main | Uncommitted, stopped mid-way, not verified. |
| `toml-rs-divergent-tsv-DRAFT.patch`, `yaml-rs-divergence-DRAFT.patch` | each repo's main | Drafts from the fleet run: registers and tests that pinned the old Rust engine answer. |

`tools/` holds the recovery differential used throughout: `recprobe/`
(a Rust binary reading hex-encoded inputs, with absolute path
dependencies on `/home/user/css/rs` and `/home/user/parser/rs` to
adjust), `recprobe/cmp.cjs` (runs TypeScript and the binary over 12,000
inputs from `gen1.cjs` and `gen2.cjs` and counts differences; modes:
default recovery, `--position`, `--relex`, `--plain`).

## What each fix does

### Engine 1: a fetched bad token, and a give-up listed once

The Rust engine buffered a bad token (`#BD`) returned by a custom lex
matcher like a good token. TypeScript throws it when it is fetched, and
under recovery records it and steps past it. The patch makes Rust do the
same (`ensure_lookahead`, a new `Lexer::skip_bad` ported from
TypeScript's `advanceLexPast`), and stops `parse_recover` from listing its
terminal error twice (a `gave_up` flag replaces an equality test that
compared the `recovered` field). New shared fixture
`test/spec/bad-token.tsv` passes in all three runtimes.

Measured effect on css under recovery (12,000 adversarial inputs):
error-only differences from TypeScript fell from 2,902 to 1, and value
differences from 1,424 to 1,029, of which 978 are inputs where
TypeScript's value is a cycle. The remaining 51 come from css cutting the
unclosed-comment span to 32 characters (the css draft patch fixes it).

Fleet run against the patched engine: every crate passes except css (one
test that pinned the old answer), toml (three register rows) and yaml
(one register test), all moving to TypeScript's answer, and support,
which failed before the patch too.

### Engine 2: recovery and relex in linear time

Two engine defects found by the css review, both absent from TypeScript:
a recovering parse walked and cloned the whole partial value on every
step (css, 16 KB, valid input: 16 s), and each rejected re-cut under
`lex.relex` copied the whole source into an error. The patch keeps a
handle on the chosen node and reads it once at the end, and builds lexer
errors without the source until they leave the lexer. css with recovery
on 14 KB: 8.03 s before, 0.025 s after; relex on 224 KB: 68 s before,
2.5 s after. A differential over 196,000 inputs and 19 configurations
found the old and new engines byte-identical. New test
`rs/tests/linear_time_test.rs`.

### Engine 3: merge of a removed fixed token

See ruling 3. The first attempt (3a) restores removed defaults but was
found unsound: it deletes a binding that loses its source to a restored
default, where TypeScript keeps it in the option tree (later merges and
edits then differ), and two `empty()`-based instances get the six
default fixed tokens back. 3b is the rework in progress, covering both
and Go.

### Go css: string lexing off

`go/css.go` set `String: &StringOptions{Chars: ""}`, but in the Go engine
an empty `Chars` means unset (see `go/empty_chars_test.go`), so the
default quotes stayed on. It now sets `Lex: false`, as the Go engine
documents. 28 stray-quote inputs gave `unterminated_string` or
`unprintable` in Go and now give TypeScript's `unexpected`. New shared
fixture `test/spec/quotes.tsv` (55 rows) passes in all three runtimes.

### jsonic/go: TypeScript's alternates

Go now installs exactly TypeScript's grammar alternates (8 differed per
option profile, among them the `@object$` and `@array$` actions Go had
moved into before-open hooks). New fixture `test/grammar/alternates.json`
with Go and Rust tests against it. Parse results were byte-identical
over 978 inputs and 10 option profiles.

## Open findings, by item

**Engine 1** (from its two reviews and the fleet run):

- MAJOR: `continuations()` now differs from TypeScript when a matcher's
  bad token comes after a finished document. Fix: set
  `capture.failure` only when `fetch == Fetch::Rule`, in both branches of
  `ensure_lookahead`; add a continuations row such as `<1>?`.
- Go differs from TypeScript in three ways found during the fix, which
  ruling 5 says Go should follow: relex with recovery lists a bad token
  twice (`go/rule.go` near 1798 records even with recovery on);
  `maxRecoveries` is read before recording (`go/recover.go`), so
  `[1,,,2]` with a cap of 1 gives `[1]` where TypeScript gives `[1,2]`; a
  bad token with only `Err` set gets code `""` fail-fast. Fix in Go, or
  register in `DIVERGENCE.md`.
- `Lexer::skip_bad`'s `rowChars` rule is unpinned: add a lone `\r` row.
- The Rust lexer's own `unterminated_comment` fault uses a different span
  and cursor from TypeScript's.
- Docs: token subscribers no longer see custom `#BD` tokens; qualify the
  zero-difference claim (U+2028 input differs, a pre-existing gap);
  record the partial-value divergence for a key that never parsed.
- This repository's `DIVERGENCE.md`, `go/options.go` and
  `go/empty_chars_test.go` still cite css as a live empty-`Chars` site;
  after the Go css fix it is not.

**Engine 2**: the recovery give-up can now return the node as a failing
callback left it (TypeScript's value, `[1,2,3]` in the reviewer's probe,
where the old engine gave `[1,2]`); pin it with a test.

**Engine 3**: the two majors above, plus: bindings returned from the
other side are appended out of default order; the TypeScript/Go split
is unrecorded; a pre-existing rule-merge divergence shows up in zon
merges.

**jsonic/go**: MAJOR, merging it breaks csv, multisource and toml Go CI
at once (they build against jsonic main in CI), so the three plugin fixes
land first. Minors: under `Info.Map` the map before-open replaces an
already-allocated map (guard it as the list one is);
`SetOptions(list.property/list.pair)` after `Make` no longer takes
effect, as in TypeScript, but undocumented and untested; the alternates
test accepts any Go function where TypeScript writes one inline; add a
paragraph to `go/doc/plugins.md` that a plugin's own map or list
alternate must allocate.

**Go css**: nits only: the old Go also raised `invalid_unicode` and
`invalid_ascii` for a stray quote (add rows such as `a"\u`); the TS/Go
probe drops inputs containing a newline.

## Landing order

1. Engine, on this branch: apply patches 1, 2, then 3a and 3b, and
   finish them. Patches 1 and 2 conflict in five places in
   `rs/src/parser.rs` (the `ParseMode` struct, its three initialisers
   and `parse_recover`): keep both sides, `partial: Partial::None,
   gave_up: false` and `(result, mode.partial.into_value(),
   mode.gave_up)`. Resolve the open findings, re-run the fleet against
   the combined engine, then push to parser#256.
2. When parser#256 merges, three downstream Rust suites that pin the old
   answer go red on their next CI run: css (the recovery test),
   toml (`test/divergent.tsv`) and yaml (`rs/tests/divergence_test.rs`).
   Land the drafts in the same window. In css, the lex subscriber's
   lookahead drop (`ctx.t.clear()` in `rs/src/lex.rs`) becomes
   redundant, and the recovery sections of `rs/doc/concepts.md`,
   `AGENTS.md` and the README can say recovery matches TypeScript.
3. Go css fix into css#53 now: it does not depend on the engine.
4. jsonic/go: the csv, multisource and toml plugin fixes first (each must
   pass its Go suite against both jsonic Go v0.7.2 and the new jsonic),
   then the jsonic PR.

## Not started

- css TypeScript and Go ports: the linear selector-group scan. The Rust
  port scans a group once (`BraceScans` in css `rs/src/lex.rs`);
  TypeScript and Go still scan each item to the group's `{`, and take
  26 s and 19 s on 200 KB of selectors. A performance change only; the
  probes must stay clean.

## Working method that held up

- A worktree set per change, with every sibling repository checked out
  beside it, so `../../parser/rs` path dependencies resolve to the
  engine under test and nothing touches the live checkouts.
- A fleet run: every Rust crate's own tests against the patched engine,
  then a baseline run for any failure, then a TypeScript check to tell a
  test that pinned the old Rust answer from a regression.
- Two independent reviewers per change, each told to refute it, with
  mutations to show each new test fails without its fix.
- Instruction counts (`valgrind --tool=callgrind`) rather than wall time
  when the machine is loaded: timings varied fourfold under load.
