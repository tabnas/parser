# Verus experiment: verifying the Rust port

A bounded experiment, run against
[Verus](https://github.com/verus-lang/verus) release
`0.2026.09.20.aef82ed` on Linux x86_64, answering three questions in
order: can Verus be installed here, can it verify anything real from
`rs/src`, and is it worth adopting. This is a record of what was tried,
not a proposal.

The short answers: yes, yes for two small pieces, and no for the crate
as it stands. The detail is below, including the measurement that
decides the third answer.

## 1. Installation

It installs, in about a minute, with no build from source.

```
curl -L -o verus.zip \
  https://github.com/verus-lang/verus/releases/download/release/0.2026.09.20.aef82ed/verus-0.2026.09.20.aef82ed-x86-linux.zip
unzip verus.zip
rustup toolchain install 1.98.1-x86_64-unknown-linux-gnu --profile minimal
./verus-x86-linux/verus --version
```

Notes that cost time to find:

- The asset is 486 MB and unzips to roughly 1.5 GB. It carries its own
  `z3` binary, so the `source/tools/get-z3.sh` step in `BUILD.md` is
  only for a source build.
- The Linux asset is named `x86-linux`, not `x86_64-linux`. The other
  spelling is a 404.
- `verus` runs under a toolchain it pins itself, `1.98.1` at this
  release, read from `rust-toolchain.toml` in the Verus repository. That
  is not the `rust-version = "1.85"` this crate is built against, and it
  is ahead of the `1.94.1` stable installed here. Verus prints the exact
  `rustup` command when the toolchain is missing.
- Behind the agent HTTPS proxy, `https://github.com/.../releases/latest`
  and the GitHub REST API are both refused with 403, so neither the
  release list nor the asset list is readable. The release asset URL
  itself resolves and downloads, and `git ls-remote --tags` works, so the
  tag can be recovered from the tag list and the asset URL built by hand.

## 2. What was verified

`rs/verus/` holds two standalone Verus copies with specifications and
proofs. Both verify:

```
$ VERUS=/path/to/verus rs/verus/run.sh
== inline_text.rs
verification results:: 9 verified, 0 errors
== lexer_span.rs
verification results:: 5 verified, 0 errors
```

### `inline_text.rs`, mirroring `rs/src/text.rs`

`rs/src/text.rs` holds the port's only `unsafe` block, and it is the only
one: a count over `rs/src` finds one `unsafe` in the whole crate, in
`InlineText::as_str`. Its SAFETY comment argues two things by hand, and
the experiment separates them.

The first is that `len` cannot exceed the buffer, which is what makes
`&bytes[..*len as usize]` in bounds. That is proved, for every
constructor. Removing the capacity guard from `new` makes Verus reject
it, so the proof rests on the guard rather than passing regardless:

```
error: postcondition not satisfied
note: recommendation not met: value may be out of range of the target type
```

The proof also covers `Default`, which constructs the `Inline` arm with
`len` 0. The shipped comment says `bytes[..len]` "is only ever written by
`new`", which does not describe that arm. The code is correct, because
the empty slice is valid UTF-8 and 0 is in bounds. The comment is
narrower than the code, and stating the invariant once as a property over
all constructors is what caught it.

The second claim is that the bytes are valid UTF-8. This is **not**
proved, and it cannot be, which is covered in section 3.

### `lexer_span.rs`, mirroring `rs/src/lexer.rs`

`Lexer::source_span` is `self.chars[start..end.min(self.char_len)]`. It
clips the end and does nothing to the start, so the indexing is in bounds
only if the caller supplies `start <= end` and `start <= char_len`.
Neither is written down anywhere in the shipped code. Stated as a
`requires`, both are discharged at all five call sites.

Four of those call sites pass `esc_point.site.pos - 1`, an unchecked
subtraction on a `usize`. Verus rejects it outright without `1 <= pos`:

```
error: possible arithmetic underflow/overflow
   |     (pos - 1, idx)
```

**This is not a live defect.** `esc_point` is captured at
`rs/src/lexer.rs:1797`, immediately after `self.advance()` has consumed
the escape character, so `pos` is at least 1 on every path that reaches
the subtraction. What the experiment produced is the precondition, not a
bug: the arithmetic is safe because of an invariant established about 40
lines earlier, and that invariant appears in no comment, no type and no
test. A refactor that captured the point before the advance would
reintroduce it silently, in release builds as a span read from a wrapped
index rather than a panic.

`advance_chars` is included for contrast. Its `saturating_add` guard is
already sufficient, and the proof adds only a statement of what the
`true` return buys the caller.

## 3. What did not verify, and why

### The `unsafe` line itself is not verifiable in place

`str::from_utf8_unchecked` has no Verus specification:

```
error: `core::str::converts::from_utf8_unchecked` is not supported
(note: you may be able to add a Verus specification to this function
with `assume_specification`)
```

So the one line in the crate that a proof exists for is the one line
Verus will not reason about. The options are `assume_specification` or
`#[verifier::external_body]`, and both put the UTF-8 claim back where it
started, as an assumption written by the same person who wrote the code.
`rs/verus/inline_text.rs` therefore proves the bound and, instead of
asserting UTF-8 validity, proves the property the hand argument actually
rests on: the filled prefix is byte-for-byte the `&str` the constructor
was handed. That is the strongest available statement, and it is weaker
than the SAFETY comment.

### Pointing Verus at the real file checks nothing

Running the unmodified `rs/src/text.rs` through Verus, with only
`use vstd::prelude::*;` added, succeeds:

```
verification results:: 0 verified, 0 errors
```

Zero verified. Verus checks only what is inside a `verus!{}` macro and
treats the rest as external. A green run on unwrapped code means nothing
was examined, which is a result worth stating plainly because the command
looks like it passed.

### Wrapping is contagious

Wrapping the real `InlineText` enum and impls verbatim in `verus!{}`,
changing nothing else, fails three ways in sequence:

1. `pub(crate) fn` inside the macro is rejected: "function is marked
   `open` but not marked `pub`". The port's chosen visibility has to
   change, or every spec has to be annotated `closed`.
2. `const INLINE_CAPACITY` declared outside the macro cannot be
   referenced from inside it. The const has to move in too.
3. `from_utf8_unchecked`, as above.

Point 2 is the general shape of the cost. Wrapping spreads to everything
the wrapped code touches, so the boundary is not where the proof is, it
is wherever the dependency graph stops.

The dependency graph here does not stop anywhere convenient. Counted over
`rs/src` at this commit: 56 uses of `dyn`, 40 `Rc<`, 28 `RefCell` and 224
`HashMap`. `rule.rs` alone has 31 `Rc`/`RefCell` sites and `lib.rs` 73
`HashMap` sites. None of those are in the Verus subset. `Arc<str>`, in
contrast, verifies fine, which is why `text.rs` was tractable: it is the
one file with an `unsafe` and no `dyn`, no `Rc`, no `RefCell` and no
`HashMap`.

### Verus cannot speak to parity at all

The contract in this repository is that Go and Rust reproduce TypeScript
exactly. Verus proves Rust against a specification written in Rust. It
has no access to TypeScript semantics, so it can say nothing about
whether `rs/` matches `ts/`. `test/spec/*.tsv` remains the only parity
contract, and a proof is assurance alongside it rather than a stronger
version of it.

## 4. Cost

| Item | Measured |
|---|---|
| Install, including the pinned toolchain | about 1 minute |
| Disk | 486 MB download, about 1.5 GB unpacked |
| Verification time, both files | about 2 seconds |
| Source expansion, `InlineText::new` | 1 line of `copy_from_slice` becomes an 11-line loop with an invariant |
| Second toolchain in CI | 1.98.1 beside `rust-version = "1.85"` |

The source expansion is the number that matters. `copy_from_slice` has no
Verus specification on a fixed-size array, and neither does
`slice::to_vec`, so both arms of `new` become explicit loops carrying
invariants. That is a 5x growth on the smallest, simplest function in the
crate, before any of it is wrapped.

## 5. Recommendation

**Do not adopt Verus for `rs/` now, and do not add it as a build
dependency.** `rs/verus/` stays as a standalone copy, run by hand, with
no gate depending on it. Two arguments, in order of weight:

1. The port is deliberately a readable mirror of `ts/`, so a divergence
   can be found by eye against the TypeScript. Macro-wrapping the engine
   destroys that property, and it is the property that makes the port
   maintainable. The contagion measured in section 3 means partial
   wrapping is not available: the boundary follows the dependency graph,
   not the proof.
2. The one place where a proof would replace a hand argument rather than
   restate one is the `unsafe` block, and Verus will not reason about the
   intrinsic at its centre. The bound is provable, the UTF-8 claim is not.

What the experiment did produce is worth keeping, and it is already
committed: the preconditions in `rs/verus/lexer_span.rs` are true
statements about `Lexer::source_span` that exist nowhere else, and the
`Default` gap in the SAFETY comment in `rs/src/text.rs` is real.

If this is revisited, the ranking of engine invariants by value is:

1. **`InlineText`'s length invariant**, as a Rust type invariant with a
   private constructor rather than a Verus proof. The bound is the
   checkable half, and Rust's own type system can carry it without a
   second toolchain.
2. **Panic freedom over the lexer's scalar index arithmetic.** This is
   where the experiment found something, and it is the area with the most
   unwritten preconditions. It does not need Verus: `proptest` over the
   inputs `test/spec/*.tsv` already describes would probe the same
   surface, at a fraction of the cost, and would find defects rather than
   prove their absence.
3. **Progress and termination in the matcher loop**, that every step
   either advances the source index or ends the token stream. `rule.maxmul`
   and the step budget approximate this at runtime. A proof would make the
   budget a backstop rather than the guarantee. This one Verus genuinely
   could not do today: the loop runs through `Rc<RefCell<...>>` rule state
   and `Box<dyn Fn>` actions, all outside the subset.

The cheaper adjacent move, if the goal is finding defects rather than
proving their absence, is `proptest` or `cargo-fuzz` over the lexer.
Neither dependency is present today, and neither needs a second
toolchain.

## Reproducing

```bash
rs/verus/run.sh          # with verus on PATH, or VERUS=/path/to/verus
```

`rs/verus/README.md` describes the two files. Neither is compiled by
`cargo`, and `ci/rust/run.sh` does not call them.
