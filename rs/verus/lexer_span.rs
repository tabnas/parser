// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Verus copy of the lexer's scalar-index span arithmetic.
//!
//! This mirrors `Lexer::source_span` and its five call sites in
//! `rs/src/lexer.rs`. It is a STANDALONE COPY, not the shipped code:
//! nothing in the crate compiles this file, and it is kept in step by
//! hand. See `doc/rust-verus-experiment.md`.
//!
//! What is being proved: every span the string lexer cuts for an
//! invalid-escape diagnostic is a valid range into the scalar buffer,
//! so the indexing cannot panic on any input.

use vstd::prelude::*;

verus! {

/// `self.chars[start..end.min(self.char_len)]` in `Lexer::source_span`.
///
/// The shipped code clips only the END. The START is not clipped and not
/// checked, so the indexing is in bounds only when the caller supplies
/// `start <= end` and `start <= char_len`. Those are exactly the two
/// `requires` clauses below, and they are the obligation the shipped
/// code leaves implicit.
pub fn source_span_range(start: usize, end: usize, char_len: usize) -> (r: (usize, usize))
    requires
        start <= char_len,
        start <= end,
    ensures
        r.0 == start,
        r.1 == if end < char_len { end } else { char_len },
        r.0 <= r.1,
        r.1 <= char_len,
{
    let clipped = if end < char_len { end } else { char_len };
    (start, clipped)
}

/// The call sites at `rs/src/lexer.rs:1837` and `:1852`:
/// `self.source_span(esc_point.site.pos - 1, self.idx)`.
///
/// `pos - 1` is an unchecked subtraction on a `usize`. Verus refuses it
/// without `1 <= pos`, which is the invariant that the escape point is
/// captured after a backslash has been consumed. That invariant holds
/// several hundred lines away from this arithmetic and is nowhere
/// written down in the shipped code.
pub fn escape_span_to_cursor(pos: usize, idx: usize, char_len: usize) -> (r: (usize, usize))
    requires
        1 <= pos,
        pos <= idx,
        idx <= char_len,
    ensures
        r.0 <= r.1,
        r.1 <= char_len,
{
    source_span_range(pos - 1, idx, char_len)
}

/// The call sites at `rs/src/lexer.rs:1887`, `:1903` and `:1938`:
/// `self.source_span(esc_point.site.pos - 1, esc_point.site.pos + N)`.
///
/// The end is `pos + N` rather than the cursor, so it can run past the
/// end of the source. That is what the clipping in `source_span_range`
/// is for, and the proof shows the clip is sufficient: no overflow is
/// possible because `pos <= char_len <= usize::MAX - N`.
pub fn escape_span_fixed(pos: usize, width: usize, char_len: usize) -> (r: (usize, usize))
    requires
        1 <= pos,
        pos <= char_len,
        width <= 8,
        char_len <= usize::MAX - 8,
    ensures
        r.0 <= r.1,
        r.1 <= char_len,
{
    source_span_range(pos - 1, pos + width, char_len)
}

/// `Lexer::advance_chars` in `rs/src/lexer.rs:186`.
///
/// The shipped code uses `saturating_add` and returns false without
/// moving when the request runs past the end. The proof records what
/// that buys: on the `true` path the cursor lands inside the buffer, so
/// the `count` calls to `advance` that follow all find a character.
pub fn advance_chars_fits(idx: usize, count: usize, char_len: usize) -> (r: bool)
    requires
        idx <= char_len,
    ensures
        r ==> idx + count <= char_len,
        !r ==> idx + count > char_len,
{
    if count > char_len - idx {
        false
    } else {
        true
    }
}

fn main() {}

} // verus!
