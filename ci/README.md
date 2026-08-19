# CI harnesses (staged for review)

Everything in this folder is runnable locally today; nothing is wired
into `.github/` yet. The two proposed workflows live in `workflows/` —
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
- `fixture-sync.sh` — verifies the shared TSV corpus has not drifted
  between `parser/test/spec` and `jsonic/ts/test/spec` (name map:
  `tabnas-*` ⇄ `jsonic-*`). Known divergences live in
  `fixture-sync-allow.txt` (currently: three UTF-8 fixtures that exist
  only in the parser repo — with a TODO to decide their fate).

## bench/ — dual-runtime benchmark harness

- `genfixture.js` — deterministic fixture matrix (pinned seed, NOT
  checked in): key-repetitive records (16KB/1MB), escape-dense strings,
  number-heavy, deep nesting, whitespace-padded tiny, and a relaxed
  unquoted-text jsonic shape.
- `bench.js` — one parser × one fixture per process (fresh V8 state);
  median/p5/p95 and MB/s. Parsers: `json`, `jsonic`, `native`
  (JSON.parse baseline).
- `gobench/` — `go test -bench` module (tabnas json + jsonic vs
  encoding/json, with -benchmem).
- `run-bench.sh [quick]` — generates fixtures and runs everything.

Numbers are advisory: compare back-to-back runs on the same machine
(the proposed bench.yml never gates; it uploads results as an
artifact).

## parity/ — cross-runtime token-stream parity

Both lexers must emit identical consumed-token streams for identical
input; the value-level TSV suites cannot see token-boundary or position
drift. `tokdump.js` and `gotokdump/` dump one flat record per consumed
token via the public lex-subscriber API; `run-parity.sh [grammar]
[spec-dir] [unescape|raw]` feeds every input column of every TSV
fixture through both and diffs (one process per runtime; per-file
sections).

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
- The TSV input column is unescaped per the corpus's own loader
  convention (parser/jsonic corpora escape `\n`; json's is raw).

Status at time of writing: `jsonic` over parser/test/spec — 1158/1158
identical; `json` over json/ts/test/spec — 84/84 identical.

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

Status at time of writing: with the extended pools, **300 cases give 13
divergences and 600 give 22** (seeds 979899 and 4242), every one a
malformed escape that TypeScript accepts and Go rejects — audit item P3,
reproduced by this fuzzer for the first time. The previous "500/500 agree"
was measured with pools that could not emit the class.

Those divergences are the ones `#123` repairs. Until it lands, this runner
is red by design on main, which matters for the promotion note below.

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
