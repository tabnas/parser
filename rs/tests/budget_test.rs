use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use tabnas::{AltSpec, RuleSpec, Tabnas};

const JSON_SOURCE: &str = r#"{"a":[1,2,3,{"b":[4,5,6]},7],"c":{"d":[8,9]}}"#;

#[test]
fn parse_budget_is_off_by_default() {
    let parser = Tabnas::make_json();
    assert!(parser.parse(JSON_SOURCE).is_ok());
}

#[test]
fn parse_budget_checks_periodically_and_can_cancel() {
    let calls = Arc::new(AtomicUsize::new(0));
    let calls_for_check = calls.clone();
    let mut parser = Tabnas::make_json();
    parser.parse_budget(2, move |context| {
        assert!(context.iteration > 0);
        calls_for_check.fetch_add(1, Ordering::SeqCst) < 2
    });

    let error = parser.parse(JSON_SOURCE).unwrap_err();
    assert_eq!(error.code, "cancel");
    assert_eq!(error.full_source, JSON_SOURCE);
    assert!(!error.rule.is_empty());
    assert_eq!(error.rule_stack.last(), Some(&error.rule));
    assert_ne!(error.token.name, "#BD");
    assert_eq!(calls.load(Ordering::SeqCst), 3);
}

#[test]
fn rule_iteration_guard_still_stops_a_runaway() {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "loop".into();
    let mut looping = RuleSpec::new("loop");
    looping.open.push(AltSpec {
        p: Some("loop".into()),
        ..Default::default()
    });
    parser.rule(looping);

    let error = parser.parse("a").unwrap_err();
    assert_eq!(error.code, "unexpected");
}

#[test]
fn serialized_budget_and_rule_multiplier_are_validated() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"rule":{"maxmul":7},"parse":{"budget":{"checkEveryN":5}}}}"#)
        .unwrap();
    assert_eq!(parser.options.rule.maxmul, 7);
    assert_eq!(parser.options.parse.budget.check_every_n, 5);

    parser
        .grammar_json(r#"{"options":{"rule":{"maxmul":0}}}"#)
        .unwrap();
    assert_eq!(parser.options.rule.maxmul, 3);
    assert!(parser
        .grammar_json(r#"{"options":{"rule":{"maxmul":0.5}}}"#)
        .is_err());
    assert!(parser
        .grammar_json(r#"{"options":{"parse":{"budget":{"checkEveryN":-1}}}}"#)
        .is_err());
}
