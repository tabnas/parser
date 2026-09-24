# CI harnesses

Everything in this folder is runnable locally today: the harnesses and
scripts that the workflows in `.github/workflows/` run. No workflow is
staged here.

To change CI, edit `.github/workflows/` in a reviewed pull request.
Session credentials push workflow files (admin `DECISIONS.md` ADR-8, as
amended 2026-09-24), so nothing needs staging here first. Sessions still
cannot push tags, so a maintainer pushes any tag that a tag-triggered
workflow needs.

Some of the workflows are maintained in admin as well, and an edit made
only in this repository does not last:

- A workflow with a template in admin `rollout/workflows/`, named
  `parser__<file>`, changes in that template too, in a pull request to
  admin. Today that is `ci.yml`, `release.yml`, `crates-release.yml`,
  `notify-status.yml` and `scorecard.yml`, and `deps-gate.yml` joins
  them once it is deployed (see "Workflows" below). Admin
  `scripts/verify.sh` reports a deployed copy that differs from its
  template, and the next `rollout/apply-workflows.sh --apply` writes the
  template back over it.
- `clib.yml` and `clib-release.yml` are stamped from admin
  `tasks/clib-template/`, together with `go/clib/`. Change the template
  and restamp with admin `tasks/adopt-clib.sh`, which writes the two
  workflows to `ci/`, then move them over the copies in
  `.github/workflows/` in the same pull request. Admin `scripts/verify.sh`
  reports a `ci/*.yml` left behind as a promotion still owed.

The prose gate (Vale over the reader-facing pages, see
`docs/STYLE-GUIDE.md`) was staged here and has been promoted: it runs
from `.github/workflows/docs.yml`. It needs no sibling checkouts and no
secrets; `make prose` runs the same check locally.

Layout assumption (the org-wide sibling-checkout convention): sibling
checkouts next to this repo — `<root>/parser`, `<root>/json`,
`<root>/jsonic` (override the root with `TABNAS_ROOT`).

`fleet/` is the exception: it fetches the checkouts it needs itself, at
each package's released version, so it needs nothing beside this repo.

## fleet/ — the downstream regression gate

Nothing else here answers the question an engine change actually raises:
**does every published grammar still work on it?** `gate/` runs json and
jsonic from whatever sibling checkouts are on the machine — two of thirty
packages, at whatever revision the operator happens to have. This one
checks out ALL of them at the version users are installing, and runs each
repo's own suites against the working-tree engine.

```bash
ci/fleet/run-fleet.sh                       # the fleet, both runtimes
ci/fleet/run-fleet.sh --only expr,jsonic    # just these (bases pulled in)
ci/fleet/run-fleet.sh --runtime ts          # one runtime
ci/fleet/run-fleet.sh --offline             # reuse .work, no network
ci/fleet/run-fleet.sh --update-lock         # record the versions tested
ci/fleet/run-fleet.sh --record-timings      # append to timings.tsv
```

It is deliberately not part of `make test`: thirty clones and two
toolchains is minutes, not seconds, and it reaches the network. Run it on
demand, and in CI — `.github/workflows/fleet.yml` is the workflow, a small
required arm on pull requests and the whole fleet nightly.

**This gate is not speculative.** `@tabnas/parser` 0.9.1 shipped a
regression in expression precedence: on it, with expr at *any* published
version, `1+2*3` parses to `["*",2,3]` — the `1 +` is dropped. Run against
the two engine trees:

```
ts/v0.9.0 →  expr/ts PASS   →  FLEET PASS
ts/v0.9.1 →  expr/ts FAIL   →  FLEET FAIL      (16 of expr's own tests)
```

Sixteen failing tests in a published package, and every check in this
repository was green. That is the hole.

- `fleet.json` — the fleet. `base` is the grammar each one extends, used
  for build order only (jsonic cannot build before json); naming a package
  with `--only` pulls in its base chain. `suites: false` means wire it but
  do not test it, and only `@tabnas/support` is set that way, because it is
  the shared fixture loader rather than something that parses.
- `expect-fail.txt` — known-broken entries, as
  `<package>/<runtime> :: <signature> :: <reason>`. All three fields are
  required. Three rules; the first two come from
  `gate/fixture-sync-allow.txt`, whose design problem was the same one:
  a line with **no reason is rejected**, so a package cannot be quieted by
  adding a bare name; an entry that **passes fails the gate**, so an
  exemption cannot outlive the breakage it was written for; and an entry
  that **fails without its signature fails the gate**, because "this
  package fails" is not a claim worth recording — an exemption accepting
  any non-zero exit turns regression detection off for that package
  entirely, and a second, newer break would ride in behind the first. The
  file ships empty: the expr break on 0.9.1 is real and is meant to block.
- `fleet.lock` — the versions the last `--update-lock` run recorded. The
  script prints what moved since; it never fails on a difference, because a
  new release is the thing being tested. Not committed until a full run has
  produced one — a lock listing versions nobody ran would be a record of
  nothing.

**Both wirings prove themselves.** TypeScript goes through
`link_ts_dep` (`lib/wire.sh`), shared with `run-gate.sh` and
`run-bench.sh`, and then the harness asks node where it actually resolved
`@tabnas/parser` from — a nested `node_modules` can shadow a correct
symlink. Go has no symlink to inspect at all, so after `go work init` the
harness asks `go list -m` which directory the engine resolved to. Either
check failing aborts the run rather than reporting a result. A fleet gate
that silently tests the PUBLISHED engine is worse than no gate: it is
thirty green rows that mean nothing.

Checkouts land in `ci/fleet/.work/` (gitignored) at each package's
`ts/vX.Y.Z` release tag, cleaned of untracked files so a previous
release's build output cannot stand in for one that no longer compiles. A
package with no such tag is still run, off its default branch, and the log
says so — "we tested the release" and "we tested main" are different
claims.

**Go is versioned separately.** A package's Go module is tagged
`go/vX.Y.Z` on the module proxy, independently of its npm release. They
usually match, and nothing enforces that, so both are resolved and the Go
arm gets its own checkout whenever they differ — otherwise the Go suites
would run off the npm release's tree and silently test the wrong source
the first time the two diverge.

**Nothing green happens without a run.** Every route to a pass that did
not actually exercise the working-tree engine fails instead: a missing
checkout (so `--offline` against an empty `.work` aborts rather than
reporting a clean sweep of skips), a failed `npm install`, a failed
downstream build (which also takes that package's suites out of the run,
rather than letting them pass off stale output), or an engine that
resolves anywhere but here. The engine is built before anything resolves
it, too: `npm i` does not create `ts/dist`, so on a clean checkout the
resolution probe would otherwise fail against a tree that is merely
unbuilt.

Per-suite output is kept in `.work/.logs/` and the last 40 lines of every
failing one are printed at the end of the run — a CI log reading
`expr/ts FAIL` and nothing else cannot be acted on, and the checkout it
came from is gone by the time anyone looks.

### Timings

**Every run times each suite and prints the durations. Only
`--record-timings` writes them down.** A timing record is a measurement
somebody decided to take, not a side effect of pushing: an ordinary run
leaves `timings.tsv` untouched, so the file stays a series of comparable
runs rather than a log of every branch that happened to run the gate.
`.github/workflows/fleet.yml` follows the same rule — the nightly and PR
arms record nothing, and `workflow_dispatch` carries a `record_timings`
input, defaulting to false, for when a person is deliberately measuring.

`seconds` covers the **suite only**. Clone, `npm install` and build are
excluded: they are dominated by the network and by whatever npm had
cached, and a number that moves with the weather is not one to write down.
What is left is the part an engine change can actually move.

**Compare rows from the same `host` and toolchain, and prefer runs taken
back to back.** These are wall-clock times on whatever machine ran them;
across two machines, or across a busy one and an idle one, the difference
between two rows says more about the machine than about the engine. That
is the same caveat `bench/` carries, and for the same reason — but the
purpose here is narrower. This file is for noticing that something has
become slow. Deciding whether a performance change is *real* is what
`bench/ab-compare.sh` and its A/B/B/A protocol are for; two rows in a TSV
cannot separate an effect from noise and should not be quoted as if they
could.

## gate/ — the engine conformance gate

The engine's own CI currently exercises ~50 strict-JSON fixture rows
and never runs downstream tests; jsonic (the grammar the engine was
built for, with the richest corpus) is not even cloned.

- `run-gate.sh` — runs the parser, json, and jsonic suites in BOTH
  runtimes against the working-tree engine (~2,300 shared TSV rows plus
  ~1,500 unit tests, including the error-code parity contract). TS deps
  are wired via `node_modules/@tabnas` symlinks; Go via a throwaway
  `go.work` (GOWORK env) — no repo files are modified.
- `fixture-sync.sh` — verifies the fixtures the two repos SHARE have not
  drifted, between `parser/test/spec` and `jsonic/test/spec` (name map:
  `tabnas-*` ⇄ `jsonic-*`; older jsonic checkouts under `ts/test/spec`
  are still found). The corpora are no longer mirrors — this repo owns
  the engine's own surface, jsonic owns the relaxed-grammar corpus — so a
  file on one side only is INFO, not a failure, and only files present on
  both are compared byte-for-byte.

  `fixture-sync-allow.txt` carries two kinds of line, and they are not
  interchangeable:

  - A **plain filename** only keeps a parser-owned file from being
    reported as INFO. It does NOT permit drift.
  - A **`not-shared: <name>`** line declares that a filename exists on
    both sides and names two DIFFERENT fixtures, so comparing them is a
    category error; the comparison is then skipped. Today that is
    `divergent.tsv`, the ADR-14 divergence register, which both repos
    keep at the same name deliberately and in two different shapes.
    A `not-shared:` entry naming a file no longer present on both sides
    FAILS — an exemption that outlives its purpose is one that will
    eventually hide a real drift.

  Nothing in that file can silence drift in a genuinely shared fixture.
  That is the one thing this gate exists to catch: the shared
  `include-json.tsv` drifted by five rows while an earlier version of the
  script was pointing at a path that no longer existed and exiting 1 on
  every run.

## bench/ — three-runtime benchmark harness

- `genfixture.js` — deterministic fixture matrix (pinned seed, NOT
  checked in): key-repetitive records (16KB/1MB), escape-dense strings,
  number-heavy, deep nesting, whitespace-padded tiny, a CJK
  (literal non-ASCII) records shape, and a relaxed unquoted-text jsonic
  shape. The CJK arm exists because everything else here is ASCII — the
  escape-dense fixture included, since escape SEQUENCES are ASCII bytes —
  which left the per-character non-ASCII scan path unmeasured.
- `bench.js` — one parser × one fixture per process (fresh V8 state);
  median/p5/p95 and MB/s. Parsers: `json`, `jsonic`, `native`
  (JSON.parse baseline).
- `gobench/` — `go test -bench` module (tabnas json + jsonic vs
  encoding/json, with -benchmem).
- `rustbench/` — one fixture per process, median/min/max and MiB/s, in
  the same JSON line shape `bench.js` emits so rows compare directly.
  Built with `lto = "fat"`, `codegen-units = 1` and mimalloc, which is
  what rs/README.md documents as the configuration to measure in: `lto`
  is off by default and is the setting that matters, and the allocator
  alone is worth 1.09x to 1.56x here, so a default release build reports
  a binary nobody ships. The parser is built once and reused across
  iterations, as the TS and Go arms do.
- `run-bench.sh [quick]` — wires the downstream checkouts at this
  working tree (`ci/lib/wire.sh`), generates fixtures, runs everything.
- `abba.js` / `ab-compare.sh` — the DECISION instrument for an engine
  performance claim; see below.

Numbers are advisory: compare back-to-back runs on the same machine
(`.github/workflows/bench.yml` never gates; it uploads results as an
artifact).

### Deciding whether a change is real: `ab-compare.sh`

`run-bench.sh` tracks throughput. It cannot tell an effect from noise,
and the noise here is bigger than the effects usually being argued
about: measured on BYTE-IDENTICAL builds, whichever tree sat in the
second slot won 5-9 rounds of 12, with the "delta" ranging over 3.25
percentage points. Any single A-then-B run can therefore report a
couple of percent in either direction from nothing at all.

So a claim needs the paired protocol:

```bash
(cd ts && npm run build)                 # build the candidate
ci/bench/ab-compare.sh --base main       # or --base <a-built-ts-dir>
```

It runs the A/B/B/A rig three times — forward, slots reversed, and a
null of the baseline against a byte-identical COPY of itself on the same
fixture in the same session — and reports EFFECT ESTABLISHED only when
the sign REVERSES with the slots and the estimate clears that session's
null. Otherwise it says UNRESOLVED and tells you not to quote either
number. Both builds supply their own strict-JSON test grammar, so no
downstream checkout is involved, and the rig refuses to time two builds
that disagree on the parse result.

**The null runs against a copy at a different path, not the same path
twice.** `abba.js` loads each slot with `require()`, which caches by
resolved path, so passing one directory twice hands both slots the same
module object — one module graph where forward and reverse have two.
Every artifact that exists only because there are two graphs (separate
inline caches, separate JIT tier-up histories, load order) would then be
missing from the null, making the band too narrow. Measured on identical
builds: the same-path null read −0.41% on `d_min` where the two-graph
null on the same machine read +1.42%. The copy is made under the
baseline tree (so bare specifiers still resolve) and removed on exit.

**Both metrics get a verdict**, not just `d_min`. `min`-of-N excludes GC
pauses by construction, which is the one channel an allocation change
moves, so deciding from `d_min` alone can miss a real allocation effect
or promote min-time noise. When the two disagree the rig says so and
declines to pick — read `d_min` for compute, `d_total` for allocation,
and report the pair.

## parity/ — cross-runtime token-stream parity

All available lexers must emit identical consumed-token streams for identical
input; the value-level TSV suites cannot see token-boundary or position
drift. `tokdump.js`, `gotokdump/`, and Rust's `parity_tokdump` dump one flat
record per consumed token via the public lex-subscriber API;
`run-parity.sh [grammar] [spec-dir] [unescape|raw]` feeds every input column
of every TSV fixture through them and diffs the streams (one process per
runtime; per-file sections). The function-free strict-JSON arm runs all three
runtimes; jsonic remains TypeScript/Go until it has a serialized grammar
artifact Rust can load.

Comparison contract (each normalization is documented in the dumpers):

- Consumed tokens only — Go's `Sub` fires after IGNORE skipping.
- `sI` is normalized to UTF-16 code units on the Go side (TS counts
  UTF-16 units, Go counts bytes — documented difference).
- `cI` is NOT compared (documented astral-plane divergence:
  UTF-16 units vs runes; `sI` pins positions exactly).
- Number values compare as float64 BIT PATTERNS (hex), sidestepping
  JS-vs-Go float formatting.
- The end token is recorded once (wind-down re-delivery counts are
  engine-internal).
- FAILED parses compare by error CODE only — token delivery on the
  error path differs three documented ways (TS delivers #BD then
  throws / Go substitutes #ZZ; wind-down re-delivery; TS's
  trailing-content probe delivers one extra token).
- The TSV input column is decoded with the shared fixture codec
  (`\n`, `\r`, `\t`, and `\\`) for every tabnas corpus; comments and
  headers are excluded exactly as the shared loader excludes them.

Status at time of writing: `jsonic` over parser/test/spec — 312/312 data
rows identical in TypeScript and Go; `json` — 119/119 data rows from the JSON
corpus and 312/312 data rows from parser/test/spec identical in TypeScript,
Go, and Rust.

## rust/ — native-port gate

- `run.sh` checks formatting, builds and tests every Rust target at the
  locked dependency graph, runs the README doctests (`--all-targets` does not
  include them), runs strict Clippy and a warning-free `cargo doc`, then
  executes both shared TSV token-stream parity arms against TypeScript and
  Go. It then compiles the
  shared strict-JSON grammar against a seeded generated value/error corpus,
  checks 35 acceptance/rejection cases produced by the sibling ABNF and EBNF
  compilers, then compiles the sibling GBNF repo's explicit llama.cpp corpus
  to schema-v3 pure data and requires Rust to match TypeScript values and
  error codes for all 96 samples.
  The GBNF compiler-consumer gate intentionally reads that corpus's maintained
  census rather than copying cases into this repo; sibling `bnf`, `abnf`,
  `ebnf`, and `gbnf` checkouts must therefore be present (or located via
  `TABNAS_ROOT`).
- `.github/workflows/rust.yml` is its PR workflow. It tests the crate's
  declared Rust 1.85 minimum and clones the strict-JSON and BNF-family
  compiler dependencies needed by the parity gates.

This harness found three real engine divergences during bring-up, all
fixed in the engine alongside it: TS lexed unquoted `__proto__` /
`constructor` as value keywords via a prototype-chain leak in the
value.def lookup (visibly wrong values: `a:__proto__` → `{"a":null}`);
Go normalized `-0` to `+0` where TS and encoding/json preserve it; and
Go reported `unterminated_string` where TS reports `unprintable` for a
control character inside a single-line string.

## fuzz/ — cross-runtime value-level differential testing

The scaling path toward large generated case counts: both runtimes'
strict-JSON CLIs must make the same accept/reject decision on every
input, with deep-equal values on accept.

- `gencorpus.js` — seeded generator biased toward grammar edges
  (escape sequences incl. surrogate pairs, exotic number forms, deep
  nesting; jsonic mode adds comments/unquoted keys/trailing commas),
  plus **malformed** `\u`/`\x` escapes, raw U+2028/U+2029, and a
  value-then-quote damage pass. See the blind-spot note below.

### The blind spot this generator had, and what remains

The 2026-08 fleet audit found four recorded accept/reject divergences in
this engine that the generator was **structurally incapable of
producing**: malformed `\u`/`\x` escapes (P3), a value followed
immediately by a quote (P1/P2), and U+2028/U+2029 (P4). Its `ESCS` pool
held only well-formed escapes, it assembled well-formed documents, and it
had no line separators anywhere. A generator that cannot emit a class
cannot find a bug in it, however many cases it runs — its clean runs were
evidence about the generator, not about the engine.

The pools now cover all four, and `ts/test/fuzz-corpus.test.js` asserts
that each class actually appears in a generated corpus, so trimming a pool
cannot restore the blindness silently.

**Two limits remain, and neither is fixed by the pools.**

1. `run-diff.sh` compares the **json** CLIs, and JSON has no unquoted
   text. So P1/P2 and P4 can now be *emitted* but not *observed* here:
   with the malformed pool disabled, 600 cases carrying separators and
   value-then-quote shapes produced **0** divergences. Observing those
   needs a jsonic-mode CLI (`jsonic-cli`). Until then the generator's
   coverage of them is latent, and this file says so rather than letting a
   green run imply otherwise.

2. `run-diff.sh` hardcodes `json` mode, so the jsonic relaxations the
   generator can produce are never exercised by the differential runner at
   all.
- `run-diff.sh [count] [seed]` — runs both CLIs per input and compares
  exit codes + values. Values are canonicalized with recursively
  sorted keys (Go json.Marshal sorts keys; JS preserves insertion
  order) and compared file-to-file (shell `$(...)` capture of node
  output truncates on large documents — the TS CLI's `process.exit`
  races async pipe writes, which is also why outputs are captured via
  file redirection).

Status at time of writing: with the extended pools, **300 cases give 12
divergences** (seed 979899), in two classes:

```
  9  both reject, codes differ: ts=unexpected go=invalid_unicode
  3  exit codes differ: ts=0 go=1
```

The previous "500/500 agree" was measured with pools that could not emit
either class.

The second class is audit item P3 — TypeScript accepting a malformed escape
that Go rejects — reproduced by this fuzzer for the first time.

The first class was **invisible to this runner until now**. It compared
exit codes only when one side succeeded, and discarded stderr, so two
runtimes rejecting the same input for *different reasons* counted as
agreement. Agreeing that an input is invalid is not agreeing; AGENTS.md
makes the code part of the contract. The runner now extracts and compares
the rejection code whenever both sides reject.

Both classes are what `#123` repairs. Until it lands, this runner is red by
design on main, which matters for the promotion note below.

**No `\uD800` in the corpus, deliberately.** The lone surrogate is a
recorded, permanent divergence, so it looks like the ideal control — but
this is a zero-difference gate, and a permanent difference in its corpus
makes it permanently red. Measured: with `\uD800` as the only malformed
entry, **103 of 300** cases diverge, and no engine repair would ever bring
that to zero. A control belongs where a difference is the expected answer:
the divergence register (ADR-14). Putting one in a gate whose contract is
"these must agree" does not test the gate, it disables it.

## Workflows

The workflows this folder staged were promoted on 2026-09-22 and live in
`.github/workflows/`. The one that was not, `deps.yml`, was retired
instead: it ran checks that CI already runs (see the dependency gates
below).

- `gate.yml`, live in `.github/workflows/` — run-gate + both parity
  suites + a 500-case fuzz diff on push/PR. Note the coupling caveat in
  its header (downstream clones at main can block engine PRs; pin refs or
  mark non-required if that bites).

  **Live since 2026-09-22.** Before then it was not promoted, so the fuzz
  diff never ran in CI. That was a second, independent reason it caught
  none of the audit's items: even had the pools been able to emit them,
  nothing was running the comparison. Both reasons had to be true for the
  silence to hold, and fixing either one alone would not have broken it.

  It clones `bnf debug abnf json jsonic`, not only the two suites it runs:
  jsonic's api, custom and debug tests require `@tabnas/debug`, and this
  repo's README doc-examples require `@tabnas/abnf`, which requires
  `@tabnas/bnf`. `gate/run-gate.sh` links each one to its working tree
  and builds it in `ci.yml`'s build-order. The parity and fuzz steps run
  whenever the gate step ran, pass or fail, so a red downstream test
  cannot hide them.
- `fleet.yml`, live in `.github/workflows/` — the downstream regression
  gate: a small required arm on every PR, the whole fleet nightly and on
  demand. See `fleet/` above for what it catches and why it is separate
  from `gate.yml`.
- The per-repo dependency gate, `tools/dep-gate.cjs`, has no workflow of
  its own: it and its suite run inside `npm test`. `ts/test/deps.test.js`
  requires `tools/dep-gate.test.cjs`, whose first case runs the gate over
  this repository's index. `ci.yml`, through the shared `polyglot-ci.yml`,
  runs `npm test` on every pull request and every push to `main`, so both
  halves run on every change that reaches `main`, and a branch pushed
  without a pull request is checked once one is opened. `make deps` and
  `make deps-test` run the same two halves locally.

  The org-wide npm floor (admin ADR-18), which checks less than
  `tools/dep-gate.cjs` does, is `deps-gate.yml` once it is deployed: a
  caller of the reusable workflow of the same name in `tabnas/.github`. A
  maintainer adds the reusable workflow there first, because a caller of
  a missing reusable workflow fails its run, and the caller follows in a
  reviewed pull request.

  A proposed `deps.yml` for the per-repo gate used to sit in this folder,
  under `workflows/`. It was deleted on 2026-09-24 instead of being
  promoted, because it ran the same two checks that `npm test` already
  runs in CI.
- `bench.yml`, live in `.github/workflows/` — weekly + manual benchmark
  run, artifact-only.
- `rust.yml`, live in `.github/workflows/` — formatting, build, tests,
  strict Clippy, and the two TypeScript/Go/Rust shared-corpus token parity
  arms at the crate's MSRV.
- crates.io publishing is **no longer staged here**. The disabled copy of
  `release.yml` that used to sit in this folder is gone. Promoting it
  meant hand-merging a second `release.yml` against the live one, and the
  bulk promotion of 2026-09-22 made it live too early. What replaces it is
  already in `.github/workflows/` and inert: `crates-release.yml`, which
  `release.yml`'s `crates` job calls with the release tag. Both skip
  unless the repository or organisation variable `TABNAS_CRATES` is `on`
  (admin ADR-21, proposed).

  The trusted publisher on crates.io still names `release.yml`. crates.io
  matches the token's `workflow_ref`, which names the calling workflow, so
  renaming `release.yml` breaks crates.io publishing exactly as it breaks
  npm.

  Admin's `docs/crates-maintainer-steps.md` lists the steps that come
  before the switch: the first manual publish of `tabnas` (RFC 3691 allows
  a trusted publisher only on a crate that exists), its trusted publisher,
  and then the other crates in dependency order.

  The org actions policy admits `rust-lang/crates-io-auth-action`. A probe
  run on 2026-09-23 downloaded and ran it, and a skipped job that uses it
  did not fail its run at startup.
