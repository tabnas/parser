use tabnas::Value;

#[test]
fn json_null_deserializes_as_null_not_undefined() {
    assert_eq!(serde_json::from_str::<Value>("null").unwrap(), Value::Null);
    assert_eq!(
        serde_json::from_str::<Value>("[null]").unwrap(),
        Value::Array(vec![Value::Null])
    );
}

#[test]
fn undefined_remains_a_serializable_internal_sentinel() {
    assert_eq!(serde_json::to_string(&Value::Undefined).unwrap(), "null");
}
