// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! A document as deep as the input must not be parsed with the call
//! stack.
//!
//! The parse loop itself is iterative, but the final pass that turns
//! every `Undefined` in the result into `Null` used to recurse, one frame
//! per nesting level. A strict-JSON parse of `[[[...]]]` therefore
//! overflowed the default 8 MiB main-thread stack -- an abort, not a
//! panic -- between 8,000 and 16,000 levels in a release build and
//! between 2,000 and 4,000 in a debug build. TypeScript and Go, whose
//! parse loops are iterative, return a value for the same input.
//!
//! So this runs on a deliberately small stack, as `deep_chain_drop_test`
//! does: big enough for any amount of parsing, nowhere near enough for one
//! frame per level. The result is handed to a thread with a very large
//! stack to be dropped, because the compiler-derived drop glue of `Value`
//! still walks the tree recursively (as `serde_json::Value`'s does); that
//! is the caller's stack, not the parse's, and is not what this pins.
//!
//! NOTE ON FAILURE MODE: a stack overflow in Rust aborts rather than
//! panicking, so on regression this binary dies and `cargo test` reports
//! it as failed rather than printing a diff.

use std::sync::Arc;
use tabnas::{ListRef, MapRef, Tabnas, Value};

/// Each level of the old recursive walk needed roughly 500 bytes to 1 KiB
/// in a release build and more than 2.5 KiB in a debug build, so a parse
/// this deep needed well over half a megabyte against the 512 KiB below
/// (the same floor as `deep_chain_drop_test`: the parse loop's own frames
/// do not fit in 256 KiB in a debug build). The depth is modest because a
/// debug build checks the whole rule stack on every step
/// (`Context::sync_rule_stack`), which makes a nested parse quadratic
/// there; the direct unwrap below goes much deeper.
const PARSE_DEPTH: usize = 1_200;
/// The direct walk is linear, so this pins it at a depth whose old
/// recursive form needed tens of megabytes.
const UNWRAP_DEPTH: usize = 20_000;
const STACK_BYTES: usize = 512 * 1024;

fn on_small_stack<T: Send + 'static>(work: impl FnOnce() -> T + Send + 'static) -> T {
    std::thread::Builder::new()
        .stack_size(STACK_BYTES)
        .spawn(work)
        .expect("spawn small-stack thread")
        .join()
        .expect("small-stack thread completed")
}

fn drop_on_large_stack(value: Value) {
    std::thread::Builder::new()
        .stack_size(1 << 30)
        .spawn(move || drop(value))
        .expect("spawn large-stack thread")
        .join()
        .expect("large-stack thread completed");
}

/// Depth of a chain of single-element arrays, walked without recursion.
fn array_depth(value: &Value) -> (usize, &Value) {
    let mut depth = 0;
    let mut current = value;
    while let Value::Array(values) = current {
        depth += 1;
        match values.first() {
            Some(inner) => current = inner,
            None => break,
        }
    }
    (depth, current)
}

#[test]
fn deeply_nested_document_parses_on_a_small_stack() {
    let source = format!("{}{}", "[".repeat(PARSE_DEPTH), "]".repeat(PARSE_DEPTH));
    let value = on_small_stack(move || {
        Tabnas::make_json()
            .parse(&source)
            .expect("deeply nested arrays parse")
    });
    let (depth, leaf) = array_depth(&value);
    assert_eq!(depth, PARSE_DEPTH);
    assert_eq!(*leaf, Value::array(Vec::new()));
    drop_on_large_stack(value);
}

#[test]
fn deeply_nested_undefined_is_unwrapped_on_a_small_stack() {
    let value = on_small_stack(|| {
        let mut value = Value::array(vec![Value::Undefined]);
        for _ in 1..UNWRAP_DEPTH {
            value = Value::array(vec![value]);
        }
        value.unwrap_undefined()
    });
    let (depth, leaf) = array_depth(&value);
    assert_eq!(depth, UNWRAP_DEPTH);
    assert_eq!(*leaf, Value::Null);
    drop_on_large_stack(value);
}

#[test]
fn unwrap_undefined_rewrites_every_container_shape() {
    let mut object = indexmap::IndexMap::new();
    object.insert("a".to_string(), Value::Undefined);
    object.insert("b".to_string(), Value::Number(1.0));
    let mut meta = indexmap::IndexMap::new();
    meta.insert("m".to_string(), Value::Undefined);
    let list = Value::ListRef(Arc::new(ListRef {
        value: vec![Value::Undefined, Value::Bool(true)],
        implicit: true,
        child: Some(Box::new(Value::Undefined)),
        meta: meta.clone(),
    }));
    let map = Value::MapRef(Arc::new(MapRef {
        value: object.clone(),
        implicit: true,
        meta,
    }));
    let input = Value::array(vec![
        Value::Undefined,
        Value::object(object),
        list,
        map,
        Value::String("s".into()),
    ]);

    let Value::Array(values) = input.unwrap_undefined() else {
        panic!("array expected");
    };
    assert_eq!(values[0], Value::Null);
    let Value::Object(entries) = &values[1] else {
        panic!("object expected");
    };
    assert_eq!(
        entries.iter().collect::<Vec<_>>(),
        [
            (&"a".to_string(), &Value::Null),
            (&"b".to_string(), &Value::Number(1.0))
        ]
    );
    let Value::ListRef(list) = &values[2] else {
        panic!("list ref expected");
    };
    assert_eq!(list.value, [Value::Null, Value::Bool(true)]);
    assert!(list.implicit);
    assert_eq!(list.child.as_deref(), Some(&Value::Null));
    assert_eq!(list.meta.get("m"), Some(&Value::Null));
    let Value::MapRef(map) = &values[3] else {
        panic!("map ref expected");
    };
    assert_eq!(map.value.get("a"), Some(&Value::Null));
    assert_eq!(map.value.get("b"), Some(&Value::Number(1.0)));
    assert!(map.implicit);
    assert_eq!(map.meta.get("m"), Some(&Value::Null));
    assert_eq!(values[4], Value::String("s".into()));
}

#[test]
fn unwrap_undefined_keeps_sharing_a_tree_without_undefined() {
    let inner = Value::array(vec![Value::Number(1.0)]);
    let outer = Value::array(vec![inner]);
    let shared = outer.clone();
    let Value::Array(before) = &shared else {
        panic!("array expected");
    };
    let Value::Array(after) = outer.unwrap_undefined() else {
        panic!("array expected");
    };
    assert!(
        Arc::ptr_eq(before, &after),
        "an undefined-free tree is returned as is"
    );
}
