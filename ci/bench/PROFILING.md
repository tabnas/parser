# Rust profiling harnesses

Two single-purpose crates and one runner, used by the gates in
`doc/rust-callback-contract-spec.md` and by any Rust engine change that
has to show where its instructions went.

They are here rather than in a scratch directory because a gate whose
tools live in a session's temporary space is a gate nobody can re-run:
the spec's acceptance bands cited four such paths and none of them
survived the session that produced them.

## `profl/` — instruction counts

One parse workload per invocation, built in the configuration `rs/README.md`
names as the one to measure in (`lto = "fat"`, `codegen-units = 1`,
mimalloc), plus `debug = 1` so callgrind can resolve symbols.

```sh
cargo build --release --manifest-path ci/bench/profl/Cargo.toml
valgrind --tool=callgrind --cache-sim=yes --branch-sim=yes \
  ci/bench/profl/target/release/profl adder
callgrind_annotate --inclusive=yes callgrind.out.*
```

`adder` and `palindrome` are the two workloads: a 512-term adder and a
512-character palindrome, 20 parses each. They are rule-step bound, which
is what makes them sensitive to per-step engine work; the 1 MB document
fixtures in `run-bench.sh` are lexer and value-construction bound and
answer a different question.

## `alloc/` — peak resident memory

The same two grammars at sizes chosen to make allocation visible
(palindrome-32768, adder-16384). Run under `/usr/bin/time -v` and read
"Maximum resident set size". The gate is "not above `main`": tabnas/parser#177
regressed 71 MB to 1214 MB and passed every unit test.

## `ab.sh` — interleaved wall clock

Alternates two binaries round by round. Read the result against the
null-change floor recorded in tabnas/measure (a change that alters nothing
moves this suite by -2.49% to +2.92%), so a wall-clock difference under
3% is not a claim on its own.

## `rulecost/` — the per-rule cost of `doc/per-rule-cost.md`

The strict-JSON builder grammar over three generated inputs, one parse per
run. It measures what the engine spends per rule it creates, which the
two `profl` workloads do not isolate: they are 512 rules deep and 20 parses
long, where these are a few rules deep and 300,000 to 3.9 million rules
long. The document's figures came from these tools; its "Conditions"
section says which build and sitting produced each one.

```sh
python3 ci/bench/rulecost/gen.py                  # the inputs, into rulecost/
(cd ci/bench/rulecost && sha256sum -c SHA256SUMS) # they must match
cargo build --release --manifest-path ci/bench/profl/Cargo.toml \
  --bin rulecost --bin rulecost_sys --bin rulecost_alloc
B=ci/bench/profl/target/release
$B/rulecost       ci/bench/rulecost/records-10000.json 3   # mimalloc, fat LTO
$B/rulecost_sys   ci/bench/rulecost/records-10000.json 3   # the system allocator
$B/rulecost_alloc ci/bench/rulecost/records-10000.json 3   # allocation counts
```

`HISTORY` (the second argument) is `options.rule.history`: an integer, or
`unset` for the default, which is unbounded. `--count` before the input
adds rule, step and token counts; its timing includes the subscribers
that count them, so it is not the timing run.

- **Instructions.** Run a binary under
  `valgrind --tool=callgrind`. Fat LTO can inline the parse into `main`,
  so take the parse's count as the total minus the same binary's total on
  a two-byte input (`[]`). That also counts reading the input and
  dropping the value, about 0.8% more than the parse alone; where the
  parse is not inlined (`rulecost_sys` without LTO), read
  `tabnas::Tabnas::parse`'s inclusive count from `callgrind_annotate`
  instead. Two counts of one build differ by up to about 1%, so count a
  variant twice before quoting a saving under 4%.
- **The system allocator without LTO** is the document's main
  configuration, because it is what a binary gets when it chooses nothing
  (aless, for one): build with `CARGO_PROFILE_RELEASE_LTO=false` and run
  `rulecost_sys`.
- **The prototypes.** `rulecost/patches/*.diff` are the document's three
  prototype changes and their combination, against `rs/`. Apply one with
  `git apply`, build, measure, and `git apply -R` it. Build each variant
  into its own `CARGO_TARGET_DIR` when timing them against each other.
- **Wall clock.** Alternate the builds run by run (`ab.sh` does two) and
  read `/proc/loadavg` around each run; the document reports the load
  with every sitting. The null-change floor above applies here too.
