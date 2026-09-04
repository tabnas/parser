use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, RuleSpec, Tabnas, Value, TIN_VL, TIN_ZZ};

#[test]
fn test_parse_primitives() {
    let tn = Tabnas::make_json();

    assert_eq!(tn.parse("true").unwrap(), Value::Bool(true));
    assert_eq!(tn.parse("false").unwrap(), Value::Bool(false));
    assert_eq!(tn.parse("null").unwrap(), Value::Null);
    assert_eq!(tn.parse("123").unwrap(), Value::Number(123.0));
    assert_eq!(tn.parse("-45.67").unwrap(), Value::Number(-45.67));
    assert_eq!(
        tn.parse("\"hello world\"").unwrap(),
        Value::String("hello world".into())
    );
    assert_eq!(tn.parse("\"\"").unwrap(), Value::String("".into()));
}

#[test]
fn test_parse_nested_containers() {
    let tn = Tabnas::make_json();

    let json_str =
        r#"{"name":"tabnas","tags":["parser","rust"],"config":{"active":true,"limit":10}}"#;
    let res = tn.parse(json_str).unwrap();

    let expected_json: serde_json::Value = serde_json::from_str(json_str).unwrap();
    let expected = Value::from_json(&expected_json);

    assert!(res.deep_equal(&expected));
}

#[test]
fn test_parse_errors() {
    let tn = Tabnas::make_json();

    let err = tn.parse("").unwrap_err();
    assert_eq!(err.code, "unexpected");

    // Unterminated string
    let err = tn.parse("\"unclosed").unwrap_err();
    assert_eq!(err.code, "unterminated_string");

    // Trailing comma
    let err = tn.parse("{\"a\":1,}").unwrap_err();
    assert_eq!(err.code, "unexpected");

    // Trailing content
    let err = tn.parse("{\"a\":1} trailing").unwrap_err();
    assert_eq!(err.code, "unexpected");

    // Unexpected character
    let err = tn.parse("@unexpected").unwrap_err();
    assert_eq!(err.code, "unexpected");
}

#[test]
fn custom_actions_run_on_the_matching_rule() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let mut tn = Tabnas::new();
    let mut root = RuleSpec::new("root");
    root.ao.push("record-open".to_string());
    root.ac.push("record-close".to_string());
    root.open.push(AltSpec {
        s: vec![vec![TIN_VL]],
        ..Default::default()
    });
    root.close.push(AltSpec {
        s: vec![vec![TIN_ZZ]],
        ..Default::default()
    });
    tn.rule(root);
    for name in ["record-open", "record-close"] {
        let seen = seen.clone();
        tn.action(name, move |rule| {
            seen.lock()
                .expect("action log lock")
                .push(rule.name.clone());
        });
    }

    tn.parse("true").expect("custom grammar should parse");
    assert_eq!(*seen.lock().expect("action log lock"), ["root", "root"]);
}

#[test]
fn unknown_actions_fail_loudly() {
    let mut tn = Tabnas::new();
    let mut root = RuleSpec::new("root");
    root.bo.push("missing-action".to_string());
    root.open.push(AltSpec {
        s: vec![vec![TIN_VL]],
        ..Default::default()
    });
    tn.rule(root);

    let error = tn.parse("true").expect_err("unknown action must fail");
    assert_eq!(error.code, "unknown");
    assert!(error.detail.contains("missing-action"));
}

#[test]
fn value_equality_preserves_signed_zero() {
    assert!(!Value::Number(-0.0).deep_equal(&Value::Number(0.0)));
}

#[test]
fn token_subscribers_observe_the_filtered_parser_stream() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let mut tn = Tabnas::make_json();
    let subscriber_seen = seen.clone();
    tn.subscribe_tokens(move |token| {
        subscriber_seen
            .lock()
            .expect("subscriber log lock")
            .push(token.name.clone());
    });

    tn.parse("[1, 2]").unwrap();
    assert_eq!(
        *seen.lock().expect("subscriber log lock"),
        ["#OS", "#NR", "#CA", "#NR", "#CS", "#ZZ", "#ZZ"]
    );
}
