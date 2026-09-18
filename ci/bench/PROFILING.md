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
