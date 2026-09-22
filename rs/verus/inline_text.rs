// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Verus copy of the `InlineText` inline-buffer invariant.
//!
//! This mirrors `rs/src/text.rs`, which holds the Rust port's only
//! `unsafe` block: `str::from_utf8_unchecked(&bytes[..*len as usize])`.
//! It is a STANDALONE COPY, not the shipped code: nothing in the crate
//! compiles this file, and it is kept in step by hand. See
//! `doc/rust-verus-experiment.md`.
//!
//! The shipped SAFETY comment makes two claims. This file proves the
//! one a machine can check and leaves the other explicitly assumed:
//!
//! 1. "`len` cannot exceed the buffer" is PROVED here, for every
//!    constructor, which is what makes `&bytes[..len]` in bounds.
//! 2. "`bytes[..len]` is valid UTF-8" is NOT proved here. Verus models
//!    `str` opaquely, so UTF-8 validity of a byte buffer is not
//!    expressible against `from_utf8_unchecked`. What is proved instead
//!    is the property the real argument rests on: the filled prefix is
//!    byte-for-byte the source the constructor was handed.
//!
//! The shipped comment says `bytes[..len]` "is only ever written by
//! `new`". `Default` also constructs the `Inline` arm. That is sound,
//! because its `len` is 0 and the empty slice is valid UTF-8, but the
//! comment as written does not cover it. `default_is_wf` below is the
//! case the prose misses.

use vstd::prelude::*;

verus! {

pub const INLINE_CAPACITY: usize = 22;

/// `rs/src/text.rs`'s `InlineText`, with the `Arc<str>` long arm
/// modelled as a byte vector. The long arm carries no proof obligation:
/// it is a safe `Deref`, and it is here only so the case split matches
/// the shipped enum.
pub enum InlineText {
    Inline { len: u8, bytes: [u8; 22] },
    Shared(Vec<u8>),
}

impl InlineText {
    /// The type invariant the `unsafe` block depends on.
    pub open spec fn len_in_bounds(self) -> bool {
        match self {
            InlineText::Inline { len, bytes: _ } => len as int <= INLINE_CAPACITY as int,
            InlineText::Shared(_) => true,
        }
    }

    /// The bytes the value denotes.
    pub open spec fn view_bytes(self) -> Seq<u8> {
        match self {
            InlineText::Inline { len, bytes } => bytes@.subrange(0, len as int),
            InlineText::Shared(shared) => shared@,
        }
    }
}

/// `InlineText::new`. The shipped code fills the buffer with one
/// `copy_from_slice`; Verus has no specification for that on a fixed
/// array, so the copy is written as a loop with the invariant spelled
/// out. That expansion is the cost this experiment is measuring, and it
/// is recorded in the write-up rather than hidden.
pub fn new(text: &[u8]) -> (result: InlineText)
    ensures
        result.len_in_bounds(),
        result.view_bytes() =~= text@,
{
    if text.len() <= INLINE_CAPACITY {
        let mut bytes: [u8; 22] = [0u8; 22];
        let mut i: usize = 0;
        while i < text.len()
            invariant
                i <= text.len(),
                text.len() <= INLINE_CAPACITY,
                bytes@.len() == INLINE_CAPACITY as int,
                forall|j: int| 0 <= j < i ==> bytes@[j] == text@[j],
            decreases text.len() - i,
        {
            let byte = text[i];
            bytes[i] = byte;
            i = i + 1;
        }
        InlineText::Inline { len: text.len() as u8, bytes }
    } else {
        // `text.to_vec()` has no Verus specification, so the long arm's
        // copy is also written out. See the write-up: the alternative
        // is `assume_specification`, which would put the property being
        // proved back into an assumption.
        let mut shared: Vec<u8> = Vec::new();
        let mut i: usize = 0;
        while i < text.len()
            invariant
                i <= text.len(),
                shared@.len() == i as int,
                forall|j: int| 0 <= j < i ==> shared@[j] == text@[j],
            decreases text.len() - i,
        {
            shared.push(text[i]);
            i = i + 1;
        }
        InlineText::Shared(shared)
    }
}

/// `impl Default for InlineText`. The arm the shipped SAFETY comment
/// does not mention.
pub fn default() -> (result: InlineText)
    ensures
        result.len_in_bounds(),
        result.view_bytes() =~= Seq::<u8>::empty(),
{
    InlineText::Inline { len: 0, bytes: [0u8; 22] }
}

/// `InlineText::shared`.
pub fn shared(text: Vec<u8>) -> (result: InlineText)
    ensures
        result.len_in_bounds(),
{
    InlineText::Shared(text)
}

/// The index `as_str` slices with. The shipped code writes
/// `&bytes[..*len as usize]` inside an `unsafe` block; the claim that
/// makes that in bounds is this postcondition.
pub fn as_str_end(value: &InlineText) -> (end: usize)
    requires
        value.len_in_bounds(),
        value is Inline,
    ensures
        end <= INLINE_CAPACITY,
        end == value->Inline_len,
{
    match value {
        InlineText::Inline { len, bytes: _ } => *len as usize,
        InlineText::Shared(_) => 0,
    }
}

/// Every constructor lands inside the buffer. This is the whole of the
/// checkable half of the SAFETY comment, stated once.
pub fn constructors_are_wf(text: &[u8], owned: Vec<u8>) {
    let a = new(text);
    assert(a.len_in_bounds());
    let b = default();
    assert(b.len_in_bounds());
    let c = shared(owned);
    assert(c.len_in_bounds());
}

fn main() {}

} // verus!
