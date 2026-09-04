# CI harnesses (staged for review)

Everything in this folder is runnable locally today; nothing is wired
into `.github/` yet. The proposed workflows live in `workflows/` —
review them and move them to `.github/workflows/` to activate.

Layout assumption (matches the existing build.yml convention): sibling
checkouts next to this repo — `<root>/parser`, `<root>/json`,
`<root>/jsonic` (override the root with `TABNAS_ROOT`).

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

## bench/ — dual-runtime benchmark harness

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
- `run-bench.sh [quick]` — wires the downstream checkouts at this
  working tree (`ci/lib/wire.sh`), generates fixtures, runs everything.
- `abba.js` / `ab-compare.sh` — the DECISION instrument for an engine
  performance claim; see below.

Numbers are advisory: compare back-to-back runs on the same machine
(the proposed bench.yml never gates; it uploads results as an
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
  locked dependency graph, runs strict Clippy, then executes both shared TSV
  token-stream parity arms against TypeScript and Go.
- `workflows/rust.yml` is its staged PR workflow. It tests the crate's declared
  Rust 1.85 minimum and clones the strict-JSON grammar needed by the parity
  dumper. Promote it under ADR-8 to make the existing local Rust gate required
  on pull requests.

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

## workflows/ — proposed GitHub workflows

- `gate.yml` — run-gate + both parity suites + a 500-case fuzz diff on
  push/PR. Note the coupling caveat in its header (downstream clones at
  main can block engine PRs; pin refs or mark non-required if that
  bites).

  **Not promoted**, so the fuzz diff has never run in CI. That is a second,
  independent reason it caught none of the audit's items: even had the
  pools been able to emit them, nothing was running the comparison. Both
  reasons had to be true for the silence to hold, and fixing either one
  alone would not have broken it.

  **Promote it only after the Phase 1 escape repairs land** (`#123`), or it
  opens red — see the status note above.
- `bench.yml` — weekly + manual benchmark run, artifact-only.
- `rust.yml` — formatting, build, tests, strict Clippy, and the two
  TypeScript/Go/Rust shared-corpus token parity arms at the crate's MSRV.
