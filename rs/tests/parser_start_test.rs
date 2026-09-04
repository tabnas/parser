use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use tabnas::{Tabnas, TabnasError, Value};

#[test]
fn serialized_parser_start_replaces_the_rule_engine() {
    let prepare_calls = Arc::new(AtomicUsize::new(0));
    let seen_prepare = prepare_calls.clone();
    let mut parser = Tabnas::new();
    parser
        .parse_prepare(move |_| {
            seen_prepare.fetch_add(1, Ordering::SeqCst);
        })
        .parser_start_ref("@custom", |source| {
            Ok(Value::String(format!("custom:{source}")))
        });
    parser
        .grammar_json(r#"{"options":{"parser":{"start":"@custom"}}}"#)
        .unwrap();

    assert_eq!(
        parser.parse("input").unwrap(),
        Value::String("custom:input".into())
    );
    assert_eq!(
        parser.parse("").unwrap(),
        Value::String("custom:".into()),
        "the TypeScript custom entry point bypasses empty-source handling"
    );
    assert_eq!(prepare_calls.load(Ordering::SeqCst), 0);

    parser
        .grammar_json(r#"{"options":{"parser":{"start":null}}}"#)
        .unwrap();
    assert_eq!(parser.parse("input").unwrap(), Value::Undefined);
}

#[test]
fn parser_start_errors_use_diagnostic_options_and_recovery_api() {
    let mut parser = Tabnas::new();
    parser.parser_start_ref("@fail", |source| {
        Err(Box::new(TabnasError::new(
            "unexpected",
            source,
            source,
            0,
            1,
            1,
        )))
    });
    parser
        .grammar_json(
            r##"{"options":{"error":{"unexpected":"override {src}"},"parser":{"start":"@fail"}}}"##,
        )
        .unwrap();

    let error = parser.parse("x").unwrap_err();
    assert_eq!(error.detail, "override x");

    let recovered = parser.parse_recover("x");
    assert!(recovered.value.is_none());
    assert!(recovered.errors.is_empty());
    assert_eq!(recovered.fatal.unwrap().detail, "override x");
}

#[test]
fn unknown_parser_start_refs_fail_transactionally() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"tag":"before"},"rule":{"kept":{"open":[]}}}"#)
        .unwrap();

    let error = parser
        .grammar_json(
            r#"{"options":{"tag":"after","parser":{"start":"@missing"}},"rule":{"lost":{"open":[]}}}"#,
        )
        .err()
        .expect("unregistered parser start must fail");
    assert!(error
        .to_string()
        .contains("unknown parser start function reference"));
    assert_eq!(parser.options.tag, "before");
    assert!(parser.rules.contains_key("kept"));
    assert!(!parser.rules.contains_key("lost"));
}

#[test]
fn panicking_parser_start_returns_an_internal_error() {
    let mut parser = Tabnas::new();
    parser.parser_start_ref("@panic", |_| panic!("boom"));
    parser
        .grammar_json(r#"{"options":{"parser":{"start":"@panic"}}}"#)
        .unwrap();

    let error = parser.parse("x").unwrap_err();
    assert_eq!(error.code, "internal");
    assert!(error.detail.contains("parser.start: boom"));
}
