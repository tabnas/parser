# Verus experiment

Standalone [Verus](https://github.com/verus-lang/verus) copies of two
pieces of `rs/src`, with specifications and machine-checked proofs.

**Nothing here is compiled by the crate.** `rs/Cargo.toml` does not list
this directory, `cargo build` never reads it, and no gate runs it. Verus
pins its own Rust toolchain (1.98.1 at the release used), which is not
the `rust-version = "1.85"` the crate is built against, so adding it to
the gates would mean carrying a second toolchain. These files are a copy
kept in step by hand, and the write-up says what that costs.

| File | Mirrors | Proves |
|---|---|---|
| `inline_text.rs` | `rs/src/text.rs` | every constructor leaves `len <= INLINE_CAPACITY`, so the `unsafe` slice in `as_str` is in bounds, and the filled prefix is byte-for-byte its source |
| `lexer_span.rs` | `rs/src/lexer.rs` | that the span arithmetic yields a valid range into the scalar buffer, and so cannot panic, GIVEN its `requires` clauses. Those preconditions are assumed, not established: none of the five production callers is modelled, so nothing here derives them from `advance()` or from where `esc_point` is captured, and `run.sh` stays green if a caller regresses |

Run them:

```bash
VERUS=/path/to/verus rs/verus/run.sh
```

Read [`doc/rust-verus-experiment.md`](../../doc/rust-verus-experiment.md)
first: it records what was tried, what verified, what did not, and what
blocks verifying the real code in place.
