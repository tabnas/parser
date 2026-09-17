// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! A rule chain as long as the input must not be dropped with the call
//! stack.
//!
//! `RuleSnapshot` owns four `Rc<RuleSnapshot>` links, so the drop glue the
//! compiler derives for it walks a chain one stack frame per link. A
//! grammar that pushes or replaces a rule per input element builds exactly
//! that, and a flat JSON array -- nesting depth ONE, nothing recursive
//! about the document -- is enough. A 1 MiB array of numbers aborted the
//! process on the default 8 MiB main-thread stack, and fat LTO, which
//! rs/README.md documents as the configuration to ship, made it worse by
//! inlining the drop cycle into itself.
//!
//! The parse's own stack use is bounded and small; only the drop scaled
//! with the input. So this runs on a deliberately small stack: big enough
//! for any amount of parsing, nowhere near enough for one frame per
//! element. With `RuleSnapshot`'s iterative `Drop` it passes; without it
//! the walk needs megabytes and dies.
//!
//! NOTE ON FAILURE MODE: a stack overflow in Rust is a guard-page fault
//! that aborts. It cannot be caught and reported as a failed assertion, so
//! on regression this binary dies and `cargo test` reports it as failed
//! rather than printing a diff. That is the best a test of this property
//! can do, and it still beats not noticing.

use tabnas::Tabnas;

/// One frame per element would need single-digit megabytes here, against
/// the 512 KiB below.
const ELEMENTS: usize = 60_000;
const STACK_BYTES: usize = 512 * 1024;

fn flat_array(elements: usize) -> String {
    let mut source = String::with_capacity(elements * 2 + 2);
    source.push('[');
    for i in 0..elements {
        if i > 0 {
            source.push(',');
        }
        source.push('1');
    }
    source.push(']');
    source
}

#[test]
fn a_rule_chain_as_long_as_the_input_drops_without_the_call_stack() {
    let handle = std::thread::Builder::new()
        .stack_size(STACK_BYTES)
        .spawn(|| {
            let source = flat_array(ELEMENTS);
            let parser = Tabnas::make_json();
            let value = parser.parse(&source).expect("a flat array should parse");
            // The drop is what is under test, so it happens here, named,
            // rather than incidentally at the end of the closure.
            drop(value);
            ELEMENTS
        })
        .expect("thread should spawn");
    assert_eq!(
        handle.join().expect("the drop must not overflow the stack"),
        ELEMENTS
    );
}
