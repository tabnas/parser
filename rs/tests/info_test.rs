use tabnas::{ListRef, MapRef, Tabnas, Text, Value};

#[test]
fn json_map_info_wraps_nested_maps_and_serializes_plainly() {
    let mut parser = Tabnas::make_json();
    parser.options.info.map = true;

    let value = parser.parse(r#"{"a":{"b":2}}"#).unwrap();
    let Value::MapRef(root) = &value else {
        panic!("expected MapRef, got {value:?}");
    };
    assert!(!root.implicit);
    assert!(root.meta.is_empty());
    let Some(Value::MapRef(inner)) = root.value.get("a") else {
        panic!("expected nested MapRef, got {:?}", root.value.get("a"));
    };
    assert_eq!(inner.value.get("b"), Some(&Value::Number(2.0)));
    let expected = Value::from_json(&serde_json::json!({"a":{"b":2}})).to_json();
    assert_eq!(value.to_json(), expected);
    assert_eq!(serde_json::to_value(&value).unwrap(), value.to_json());
}

#[test]
fn json_list_and_text_info_preserve_metadata_and_plain_json_shape() {
    let mut parser = Tabnas::make_json();
    parser.options.info.list = true;
    parser.options.info.text = true;

    let value = parser.parse(r#"["hello",1]"#).unwrap();
    let Value::ListRef(ListRef {
        value: items,
        implicit,
        child,
        meta,
    }) = &value
    else {
        panic!("expected ListRef, got {value:?}");
    };
    assert!(!implicit);
    assert!(child.is_none());
    assert!(meta.is_empty());
    assert_eq!(
        items.first(),
        Some(&Value::Text(Text {
            quote: "\"".into(),
            string: "hello".into(),
        }))
    );
    let expected = Value::from_json(&serde_json::json!(["hello", 1])).to_json();
    assert_eq!(value.to_json(), expected);
    assert_eq!(serde_json::to_value(&value).unwrap(), value.to_json());
}

#[test]
fn serialized_info_options_and_bound_implicit_config_reach_builtins() {
    let mut grammar: serde_json::Value =
        serde_json::from_str(include_str!("../../ts/test/json-builder.fixture.json")).unwrap();
    grammar["options"]["info"] = serde_json::json!({
        "map": true,
        "list": true,
        "text": true,
        "marker": "__meta__"
    });
    grammar["rule"]["map"]["open"][0]["k"] = serde_json::json!({"object$":{"implicit":true}});

    let mut parser = Tabnas::new();
    parser.grammar_json(&grammar.to_string()).unwrap();
    assert!(parser.options.info.map);
    assert!(parser.options.info.list);
    assert!(parser.options.info.text);
    assert_eq!(parser.options.info.marker, "__meta__");

    let value = parser.parse("{}").unwrap();
    let Value::MapRef(MapRef { implicit, .. }) = value else {
        panic!("expected MapRef");
    };
    assert!(implicit);
}

#[test]
fn text_info_does_not_wrap_non_text_tokens() {
    let mut parser = Tabnas::make_json();
    parser.options.info.text = true;

    assert_eq!(parser.parse("42").unwrap(), Value::Number(42.0));
    assert_eq!(parser.parse("true").unwrap(), Value::Bool(true));
}
