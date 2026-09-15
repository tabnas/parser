// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Short shared text.
//!
//! Token text is small and copied constantly, and used to be a
//! `String` that allocated on every clone. Text of `INLINE_CAPACITY`
//! bytes or less now lives in the value itself; anything longer sits
//! behind an `Arc`.
//!
//! This is not the right shape for every short string in the engine.
//! `RuleName` was tried on it and measured consistently *worse*, by
//! one to two percent on every case: a rule name lives inside
//! `RuleSnapshot`, which the copy-on-write path copies whole, so a
//! 24-byte inline value costs more to carry than an 8-byte shared
//! handle. It pays here because a token's text is the thing being
//! copied, not a passenger inside something else.
//!
//! `Arc` rather than `Rc` because token subscribers are `Send + Sync`
//! closures and may capture what they are given. It is only the long
//! arm, which is why the atomic refcount does not show up: an atomic
//! read-modify-write costs more than glibc spends on a short-string
//! allocation, so using one for every clone measured *slower* than the
//! `String` it replaced even while running 11% fewer instructions.
//!
//! This is the only place that reads bytes back as UTF-8 without
//! checking them, so it is the only place that has to be right about
//! it: `Inline` is written whole from a `&str` and nowhere else.

use std::sync::Arc;

const INLINE_CAPACITY: usize = 22;

#[derive(Clone)]
pub(crate) enum InlineText {
    Inline {
        len: u8,
        bytes: [u8; INLINE_CAPACITY],
    },
    Shared(Arc<str>),
}

impl InlineText {
    pub(crate) fn new(text: &str) -> Self {
        if text.len() <= INLINE_CAPACITY {
            let mut bytes = [0u8; INLINE_CAPACITY];
            bytes[..text.len()].copy_from_slice(text.as_bytes());
            InlineText::Inline {
                len: text.len() as u8,
                bytes,
            }
        } else {
            InlineText::Shared(Arc::from(text))
        }
    }

    pub(crate) fn shared(text: Arc<str>) -> Self {
        InlineText::Shared(text)
    }

    pub(crate) fn as_str(&self) -> &str {
        match self {
            // SAFETY: `bytes[..len]` is only ever written by `new`,
            // which copies it whole out of a `&str`, so it is valid
            // UTF-8. `len` cannot exceed the buffer: `new` takes this
            // arm only when it does not.
            InlineText::Inline { len, bytes } => unsafe {
                std::str::from_utf8_unchecked(&bytes[..*len as usize])
            },
            InlineText::Shared(text) => text,
        }
    }
}

impl Default for InlineText {
    fn default() -> Self {
        InlineText::Inline {
            len: 0,
            bytes: [0u8; INLINE_CAPACITY],
        }
    }
}
