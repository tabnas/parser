// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! A field that says when it has been written to.
//!
//! `Tabnas::parse` assembles a whole `Parser` for every call, and most
//! of that work depends only on configuration that does not change
//! between parses. Reusing it needs a way to know when it *has*
//! changed, and `options` and the grammar maps are public fields that
//! callers write to directly.
//!
//! So the field keeps a counter and bumps it whenever anything takes a
//! mutable path to the value. Reads go through `Deref` and writes
//! through `DerefMut`, which means `tabnas.options.rule.start = ..`
//! and `tabnas.options.rule.start` both still compile and mean what
//! they did.
//!
//! The counter over-counts rather than under-counts: taking `&mut` and
//! then not writing still bumps it. That costs a rebuild, never a
//! stale answer.

use std::ops::{Deref, DerefMut};
use std::sync::atomic::{AtomicU64, Ordering};

/// Atomic rather than a `Cell` so the value stays `Sync`, which
/// `Tabnas` is and should remain.
#[derive(Debug, Default)]
pub struct Tracked<T> {
    value: T,
    generation: AtomicU64,
}

impl<T> Tracked<T> {
    pub fn new(value: T) -> Self {
        Tracked {
            value,
            generation: AtomicU64::new(0),
        }
    }

    /// How many times a mutable path to the value has been handed out.
    pub(crate) fn generation(&self) -> u64 {
        self.generation.load(Ordering::Relaxed)
    }

    /// Read without counting it as a write. For engine code that knows
    /// it is only looking.
    pub(crate) fn peek(&self) -> &T {
        &self.value
    }
}

impl<T> Deref for Tracked<T> {
    type Target = T;

    fn deref(&self) -> &T {
        &self.value
    }
}

impl<T> DerefMut for Tracked<T> {
    fn deref_mut(&mut self) -> &mut T {
        self.generation.fetch_add(1, Ordering::Relaxed);
        &mut self.value
    }
}

impl<T: Clone> Clone for Tracked<T> {
    fn clone(&self) -> Self {
        // A clone starts its own count: whatever the original had
        // prepared, this one has not.
        Tracked::new(self.value.clone())
    }
}
