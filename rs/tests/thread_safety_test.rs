// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! `Value` documents that it is `Send + Sync` and has to stay that way
//! (`src/value.rs`, the `Array` variant): `Options` carries `Value`s and is
//! shared as `Arc<Options>`, and a parsed value can be sent between
//! threads, and a `Tabnas` instance is shared between threads by the same
//! token. That is a compile-time property, so this pins it at compile
//! time; a variant that smuggles in an `Rc` or a `RefCell` fails to build.

use tabnas::{Options, Tabnas, TabnasError, Value};

fn assert_send_sync<T: Send + Sync>() {}

#[test]
fn parser_values_options_and_errors_are_send_and_sync() {
    assert_send_sync::<Tabnas>();
    assert_send_sync::<Value>();
    assert_send_sync::<Options>();
    assert_send_sync::<TabnasError>();
}
