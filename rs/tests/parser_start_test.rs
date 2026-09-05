use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use tabnas::{ContextSeed, Tabnas, TabnasError, Value};

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

#[test]
fn parser_start_receives_the_optional_parent_context_seed() {
    let mut parser = Tabnas::new();
    parser.parser_start_with_context_ref("@context", |source, instance, meta, parent| {
        let parent_value = parent
            .and_then(|seed| seed.u.get("parent"))
            .cloned()
            .unwrap_or(Value::Undefined);
        Ok(Value::Array(vec![
            Value::String(source.into()),
            Value::String(instance.id.clone()),
            meta.clone(),
            parent_value,
        ]))
    });
    parser
        .grammar_json(r#"{"options":{"parser":{"start":"@context"}}}"#)
        .unwrap();

    let meta = Value::Object(
        [("request".into(), Value::Number(7.0))]
            .into_iter()
            .collect(),
    );
    let parent = ContextSeed {
        meta: None,
        u: [("parent".into(), Value::String("seed".into()))]
            .into_iter()
            .collect(),
    };
    let result = parser
        .parse_with_context("input", meta.clone(), &parent)
        .unwrap();
    assert_eq!(
        result,
        Value::Array(vec![
            Value::String("input".into()),
            Value::String(parser.id.clone()),
            meta,
            Value::String("seed".into()),
        ])
    );

    let result = parser.parse("plain").unwrap();
    let Value::Array(result) = result else {
        panic!("custom parser must return an array")
    };
    assert_eq!(Value::Undefined, result[3]);
}
