// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Recovery keeps a live handle to the best partial node.
//!
//! Taking an owned `Value` snapshot at every rule step kept an extra `Arc`
//! handle to the array under construction. The next append then paid
//! `Arc::make_mut`'s copy-on-write cost for the whole accumulated array. A
//! clean document with recovery enabled therefore copied its prefix once per
//! element and allocated quadratically even though no recovery was needed.
//!
//! Allocation bytes measure that exact defect without depending on the host's
//! clock. Four times the input should allocate about four times the memory;
//! the old snapshot path allocated about sixteen times as much.

use std::alloc::{GlobalAlloc, Layout, System};
use std::cell::Cell;

use tabnas::{RecoverOptions, Tabnas, Value};

thread_local! {
    static ALLOCATED: Cell<u64> = const { Cell::new(0) };
}

struct CountingAllocator;

// SAFETY: allocation is forwarded to `System` unchanged. The thread-local
// counter uses a const initializer and allocates nothing itself.
unsafe impl GlobalAlloc for CountingAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        ALLOCATED.with(|total| total.set(total.get().wrapping_add(layout.size() as u64)));
        unsafe { System.alloc(layout) }
    }

    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        ALLOCATED.with(|total| total.set(total.get().wrapping_add(layout.size() as u64)));
        unsafe { System.alloc_zeroed(layout) }
    }

    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        ALLOCATED.with(|total| total.set(total.get().wrapping_add(new_size as u64)));
        unsafe { System.realloc(ptr, layout, new_size) }
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { System.dealloc(ptr, layout) }
    }
}

#[global_allocator]
static ALLOCATOR: CountingAllocator = CountingAllocator;

fn source(count: usize) -> String {
    let mut source = String::with_capacity(count * 2 + 1);
    source.push('[');
    for index in 0..count {
        if index > 0 {
            source.push(',');
        }
        source.push('1');
    }
    source.push(']');
    source
}

fn recovering_parser() -> Tabnas {
    let mut parser = Tabnas::make_json();
    parser.options.parse.recover = RecoverOptions {
        enabled: true,
        ..Default::default()
    };
    parser
}

fn allocated_to_recover(parser: &Tabnas, source: &str, count: usize) -> u64 {
    let before = ALLOCATED.with(Cell::get);
    let recovered = parser.parse_recover(source);
    let after = ALLOCATED.with(Cell::get);
    assert!(recovered.errors.is_empty(), "{:?}", recovered.errors);
    let Some(Value::Array(values)) = recovered.value else {
        panic!("recovery did not return an array")
    };
    assert_eq!(count, values.len());
    std::hint::black_box(values);
    after - before
}

fn recovered_len(parser: &Tabnas, source: &str) -> usize {
    match parser.parse_recover(source).value {
        Some(Value::Array(values)) => values.len(),
        other => panic!("recovery did not return an array: {other:?}"),
    }
}

#[test]
fn clean_recovery_allocates_linearly_in_the_document_size() {
    const SMALL: usize = 500;
    const LARGE: usize = 2_000;
    const GATE: f64 = 8.0;

    let parser = recovering_parser();
    let small = source(SMALL);
    let large = source(LARGE);

    // Warm the prepared grammar and lexer paths before measuring either size.
    assert_eq!(recovered_len(&parser, &small), SMALL);
    assert_eq!(recovered_len(&parser, &large), LARGE);

    let small_bytes = allocated_to_recover(&parser, &small, SMALL);
    let large_bytes = allocated_to_recover(&parser, &large, LARGE);
    assert!(small_bytes > 0, "the allocation counter did not move");

    let ratio = large_bytes as f64 / small_bytes as f64;
    assert!(
        ratio < GATE,
        "{SMALL} elements allocated {small_bytes} bytes and {LARGE} allocated \
         {large_bytes}: ratio {ratio:.1}; linear is about 4 and quadratic about 16"
    );
}
