// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! The `child_node` park, and the invariant it rests on.
//!
//! A pushed child is seeded with its PARENT's node cell. A child that
//! never installs a cell of its own still has that cell when it closes,
//! so `Rule::accept_child_node` records `child_node_is_self` and leaves
//! the parent's `child_node` holding a second `Value` handle on the very
//! container the parent's next child will write into. `Value` containers
//! are copy-on-write, so with that second handle in place every append
//! copies the whole container, and a rule with one such child per
//! element copies its accumulated node once per element.
//!
//! The shipped grammars do not reach it. `jsonic`, `json`, `csv` and
//! `yaml` all iterate by having `elem` or `pair` REPLACE itself, so the
//! container rule pushes once and never re-pushes. The grammar below
//! iterates the other legal way, with the container rule pushing a fresh
//! child per element from its close state, and that is the shape that
//! was quadratic.
//!
//! The scaling here is measured in BYTES ALLOCATED rather than in time,
//! through the counting allocator below. The defect is a copy of the
//! accumulated container per element, and a copy is an allocation of a
//! known size, so counting them answers the question exactly and gives
//! the same answer on a fast machine, a slow one and a loaded one.

use std::alloc::{GlobalAlloc, Layout, System};
use std::cell::Cell;

use tabnas::{RecoverOptions, Tabnas, Value};

thread_local! {
    /// Bytes requested on THIS thread. Thread-local because the test
    /// harness runs tests in parallel threads, and a shared counter
    /// would report another test's allocations as this one's.
    ///
    /// `const`-initialised so the slot itself never allocates: a lazily
    /// initialised thread-local would allocate from inside the allocator
    /// that is trying to record the allocation.
    static ALLOCATED: Cell<u64> = const { Cell::new(0) };
}

struct CountingAllocator;

// SAFETY: every method forwards to `System` unchanged; the only addition
// is a counter increment on a `Cell<u64>` that allocates nothing.
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

/// `top` pushes one `elem` per element; `elem` installs no node of its
/// own and appends into `top`'s through `@push$`. No `"clear"`, so the
/// engine's default fixed tokens (`[`, `]`, `,`) survive.
const PUSH_PER_ELEMENT: &str = r##"{
  "v": 2,
  "options": {
    "rule": { "start": "top" },
    "text": { "lex": false },
    "comment": { "lex": false }
  },
  "rule": {
    "top": {
      "open":  [ { "s": "#OS", "a": "@array$", "p": "elem" } ],
      "close": [ { "s": "#CA", "p": "elem" }, { "s": "#CS" } ]
    },
    "elem": {
      "open":  [ { "p": "val" } ],
      "close": [ { "s": ["#CA #CS"], "b": 1, "a": "@push$" } ]
    },
    "val": { "open": [ { "s": "#VAL", "a": "@value$" } ] }
  }
}"##;

fn pushing_parser() -> Tabnas {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(PUSH_PER_ELEMENT)
        .expect("the grammar installs");
    parser
}

fn source(count: usize) -> String {
    let mut out = String::with_capacity(count * 6 + 2);
    out.push('[');
    for index in 0..count {
        if index > 0 {
            out.push(',');
        }
        out.push_str(&index.to_string());
    }
    out.push(']');
    out
}

fn numbers(value: &Value) -> Vec<f64> {
    match value {
        Value::Array(items) => items
            .iter()
            .map(|item| match item {
                Value::Number(number) => *number,
                other => panic!("expected a number, got {other:?}"),
            })
            .collect(),
        other => panic!("expected an array, got {other:?}"),
    }
}

/// Bytes this thread requests from the allocator while parsing `text`.
///
/// The parse's own result is dropped after the reading, so the count
/// covers the build and not the teardown. It is not a peak-memory
/// figure: a copy that is freed at once still counts, which is the
/// point, because a copy per element is exactly the defect.
fn bytes_to_parse(parser: &Tabnas, text: &str) -> u64 {
    let before = ALLOCATED.with(Cell::get);
    let value = parser.parse(text).expect("parses");
    let after = ALLOCATED.with(Cell::get);
    std::hint::black_box(&value);
    after - before
}

/// The scaling claim, as a RATIO of allocated bytes, so it means the
/// same on every machine and does not depend on the clock at all.
///
/// Quadruple the input and linear work allocates about four times as
/// much. A copy of the accumulated container per element allocates
/// about SIXTEEN times as much, because the copies themselves are the
/// bulk of it. Measured here, before the park: 75,831,361 bytes at
/// 1,000 elements and 1,167,418,489 at 4,000, a ratio of 15.4. After:
/// 3,961,225 and 15,939,265, a ratio of 4.0.
///
/// The gate is 8: clear of linear, and far enough under quadratic that
/// the defect cannot hide beneath it.
#[test]
fn a_rule_that_pushes_one_child_per_element_allocates_linearly() {
    const SMALL: usize = 1_000;
    const LARGE: usize = 4_000;
    const GATE: f64 = 8.0;

    let parser = pushing_parser();
    let small = source(SMALL);
    let large = source(LARGE);

    // Warm the prepared-rule tables and any one-time lexer state, so the
    // first measured parse is not paying for the second one's setup too.
    assert_eq!(numbers(&parser.parse(&small).expect("parses")).len(), SMALL);
    assert_eq!(numbers(&parser.parse(&large).expect("parses")).len(), LARGE);

    let small_bytes = bytes_to_parse(&parser, &small);
    let large_bytes = bytes_to_parse(&parser, &large);
    assert!(small_bytes > 0, "the allocator counter did not move");

    let ratio = large_bytes as f64 / small_bytes as f64;
    assert!(
        ratio < GATE,
        "{SMALL} elements allocated {small_bytes} bytes and {LARGE} allocated \
         {large_bytes}: a ratio of {ratio:.1} for four times the input, where \
         linear is about 4 and a copy of the node per element is about 16"
    );
}

/// The value this grammar produces, element for element. `@push$` reads
/// `child_node` and nothing else, so a park that dropped the field at
/// the wrong moment, or put back something that was never there, shows
/// up here as a missing, duplicated or self-nested element.
#[test]
fn pushing_one_child_per_element_builds_every_element() {
    let parser = pushing_parser();

    // `top` pushes an `elem` unconditionally, so the empty list is not in
    // this grammar's language. Pinned so a change to the grammar above
    // cannot quietly turn this case into a pass.
    assert!(
        parser.parse("[]").is_err(),
        "empty list unexpectedly parsed"
    );

    for count in [1_usize, 2, 3, 17, 500] {
        let value = parser.parse(&source(count)).expect("parses");
        assert_eq!(
            numbers(&value),
            (0..count).map(|index| index as f64).collect::<Vec<_>>(),
            "{count} elements"
        );
    }
}

/// `recover.pop_until_valid = false` takes the one pop that resumes a
/// parent WITHOUT accepting a child node over the top of it, which is
/// why the park is skipped for a parse configured that way. Both
/// settings must answer the same on a clean parse, and the fixed-depth
/// setting must still recover rather than panic, hang or lose what it
/// had already built.
#[test]
fn fixed_depth_recovery_sees_the_same_tree_as_the_default() {
    let mut fixed_depth = pushing_parser();
    fixed_depth.options.parse.recover = RecoverOptions {
        enabled: true,
        pop_until_valid: false,
        ..Default::default()
    };
    let mut pop_until_valid = pushing_parser();
    pop_until_valid.options.parse.recover = RecoverOptions {
        enabled: true,
        ..Default::default()
    };

    for count in [1_usize, 2, 3, 40, 500] {
        let text = source(count);
        assert_eq!(
            fixed_depth.parse(&text).expect("parses"),
            pop_until_valid.parse(&text).expect("parses"),
            "{count} elements"
        );
    }

    // A fault after several elements, recovered both ways. The two
    // settings are allowed to resume at different places, which is what
    // the option selects, but each must report the fault and keep the
    // elements it had already built.
    for (name, parser) in [
        ("fixed depth", &fixed_depth),
        ("pop until valid", &pop_until_valid),
    ] {
        let recovered = parser.parse_recover("[0,1,2,%,4]");
        assert!(
            !recovered.errors.is_empty(),
            "{name}: no error was reported"
        );
        let Some(Value::Array(items)) = recovered.value else {
            continue;
        };
        let built: Vec<f64> = items
            .iter()
            .filter_map(|item| match item {
                Value::Number(number) => Some(*number),
                _ => None,
            })
            .collect();
        assert!(
            built.starts_with(&[0.0, 1.0, 2.0]),
            "{name}: the elements built before the fault were lost: {items:?}"
        );
    }
}
