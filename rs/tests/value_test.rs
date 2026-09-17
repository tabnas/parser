use tabnas::{Tabnas, Value};

#[test]
fn json_null_deserializes_as_null_not_undefined() {
    assert_eq!(serde_json::from_str::<Value>("null").unwrap(), Value::Null);
    assert_eq!(
        serde_json::from_str::<Value>("[null]").unwrap(),
        Value::array(vec![Value::Null])
    );
}

#[test]
fn undefined_remains_a_serializable_internal_sentinel() {
    assert_eq!(serde_json::to_string(&Value::Undefined).unwrap(), "null");
}

/// A write through one handle on a value is invisible through another.
///
/// `Value`'s containers are shared behind an `Arc` so that folding a
/// finished rule's value into its parent costs a refcount rather than a
/// copy of everything built so far. Sharing the representation must not
/// share the VALUE: a `Value` still behaves as if each clone owned its
/// own contents, which is what `Arc::make_mut` buys and what every
/// mutating accessor has to go through.
///
/// Nothing else in the suite asks this, and it is the one invariant the
/// representation could lose silently -- reaching inside with
/// `Arc::get_mut` instead would pass every other test in the repository.
#[test]
fn writing_through_one_handle_is_invisible_through_another() {
    let mut original = Value::object(
        [
            ("keep".into(), Value::Number(1.0)),
            (
                "nested".into(),
                Value::array(vec![Value::Number(2.0), Value::Number(3.0)]),
            ),
        ]
        .into_iter()
        .collect(),
    );
    let untouched = original.clone();

    original
        .as_object_mut()
        .expect("an object")
        .insert("added".into(), Value::Bool(true));
    original
        .as_object_mut()
        .expect("an object")
        .get_mut("nested")
        .and_then(Value::as_array_mut)
        .expect("an array")
        .push(Value::Number(4.0));

    let Value::Object(after) = &untouched else {
        panic!("the clone is still an object")
    };
    assert!(!after.contains_key("added"), "a key leaked into the clone");
    let Some(Value::Array(nested)) = after.get("nested") else {
        panic!("the clone still holds its array")
    };
    assert_eq!(nested.len(), 2, "an element leaked into the clone's array");

    // And the write did land on the handle that made it.
    let Value::Object(before) = &original else {
        panic!("still an object")
    };
    assert_eq!(before.get("added"), Some(&Value::Bool(true)));
}

/// The same, reached through the parser rather than built by hand: a
/// parsed document handed to two owners is two independent values.
#[test]
fn a_parsed_value_and_its_clone_are_independent() {
    let parsed = Tabnas::make_json()
        .parse(r#"{"a":[1,2],"b":{"c":3}}"#)
        .unwrap();
    let mut mutated = parsed.clone();
    mutated
        .as_object_mut()
        .expect("an object")
        .get_mut("a")
        .and_then(Value::as_array_mut)
        .expect("an array")
        .clear();

    let expected: serde_json::Value = serde_json::from_str(r#"{"a":[1,2],"b":{"c":3}}"#).unwrap();
    assert!(
        parsed.deep_equal(&Value::from_json(&expected)),
        "clearing the clone changed the parse result"
    );
}

/// Nesting a thousand deep parses to the right value.
///
/// It also used to take 287 ms, because every rule close copied the whole
/// subtree built so far and the cost was quadratic in depth. It is 8 ms
/// now. The assertion here is correctness; the timing lives in the commit
/// message, because a wall-clock assertion in CI is a flake waiting to
/// happen.
///
/// The tree is walked rather than compared against `serde_json`, whose
/// own recursion limit gives up at 128 levels.
#[test]
fn a_thousand_levels_of_nesting_parse_to_the_right_value() {
    const DEPTH: usize = 1024;
    let mut source = String::from("1");
    for level in 0..DEPTH {
        source = if level % 2 == 0 {
            format!("[{source},2]")
        } else {
            format!("{{\"a\":{source}}}")
        };
    }

    let mut node = Tabnas::make_json().parse(&source).unwrap();
    for level in (0..DEPTH).rev() {
        node = if level % 2 == 0 {
            let Value::Array(values) = &node else {
                panic!("level {level} should be an array, found {node:?}")
            };
            assert_eq!(values.len(), 2, "level {level} lost an element");
            assert_eq!(values[1], Value::Number(2.0), "level {level} lost its tail");
            values[0].clone()
        } else {
            let Value::Object(entries) = &node else {
                panic!("level {level} should be an object, found {node:?}")
            };
            assert_eq!(entries.len(), 1, "level {level} lost a key");
            entries
                .get("a")
                .expect("level {level} keeps its key")
                .clone()
        };
    }
    assert_eq!(node, Value::Number(1.0), "the innermost value survived");
}
