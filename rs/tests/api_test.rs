use std::collections::HashMap;
use std::rc::Rc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use tabnas::{
    AltSpec, Rule, RuleName, RuleSpec, RuleState, Tabnas, TokenText, Value, TIN_VL, TIN_ZZ,
};

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
    tn.options.rule.start = "root".into();
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
fn lifecycle_after_actions_run_after_next_resolution_on_implicit_states() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let mut tn = Tabnas::new();
    tn.options.rule.start = "top".into();

    let mut top = RuleSpec::new("top");
    top.ao.push("top-after-open".into());
    top.close.push(AltSpec {
        s: vec![vec![TIN_VL]],
        ..Default::default()
    });
    tn.rule(top);

    let open_seen = seen.clone();
    tn.action("top-after-open", move |rule| {
        open_seen.lock().unwrap().push((
            rule.name.clone(),
            rule.state,
            rule.next_rule_name.clone(),
        ));
    });

    tn.parse("true").unwrap();
    assert_eq!(
        *seen.lock().unwrap(),
        [("top".into(), tabnas::RuleState::Open, Some("top".into()))]
    );
}

#[test]
fn pushed_child_after_close_action_can_see_its_parent_as_next() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let mut tn = Tabnas::new();
    tn.options.rule.start = "top".into();

    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![TIN_VL]],
        p: Some("child".into()),
        b: 1,
        ..Default::default()
    });
    tn.rule(top);

    let mut child = RuleSpec::new("child");
    child.open.push(AltSpec {
        s: vec![vec![TIN_VL]],
        ..Default::default()
    });
    child.ac.push("child-after-close".into());
    tn.rule(child);

    let close_seen = seen.clone();
    tn.action("child-after-close", move |rule| {
        close_seen.lock().unwrap().push((
            rule.name.clone(),
            rule.state,
            rule.next_rule_name.clone(),
        ));
    });

    tn.parse("true").unwrap();
    assert_eq!(
        *seen.lock().unwrap(),
        [("child".into(), tabnas::RuleState::Close, Some("top".into()))]
    );
}

#[test]
fn unknown_actions_fail_loudly() {
    let mut tn = Tabnas::new();
    tn.options.rule.start = "root".into();
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
fn missing_configured_start_rule_returns_undefined() {
    let mut tn = Tabnas::new();
    tn.options.rule.start = "missing".into();
    let mut first = RuleSpec::new("first");
    first.open.push(AltSpec {
        s: vec![vec![TIN_VL]],
        ..Default::default()
    });
    tn.rule(first);

    assert_eq!(tn.parse("true").unwrap(), Value::Undefined);
    assert!(tn.continuations("true").tokens.is_empty());
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

#[test]
fn parse_prepare_empty_result_and_result_fail_are_honored() {
    let calls = Arc::new(AtomicUsize::new(0));
    let called = calls.clone();
    let mut empty = Tabnas::new();
    empty.options.lex.empty_result = Value::String("empty".into());
    empty.parse_prepare(move |_context| {
        called.fetch_add(1, Ordering::SeqCst);
    });
    assert_eq!(empty.parse("").unwrap(), Value::String("empty".into()));
    assert_eq!(calls.load(Ordering::SeqCst), 1);

    let mut rejected = Tabnas::make_json();
    rejected.options.result.fail.push(Value::Number(1.0));
    assert_eq!(rejected.parse("1").unwrap_err().code, "unexpected");

    rejected.options.parse.recover.enabled = true;
    let recovered = rejected.parse_recover("1");
    assert_eq!(recovered.value, Some(Value::Number(1.0)));
    assert_eq!(recovered.errors.len(), 1);
    assert!(recovered.fatal.is_none());
}

/// `Rule.name` shares one handle per installed rule rather than holding
/// its own `String`. That is meant to be invisible: a plugin compares it
/// with a literal, prints it, or takes a `&str` from it exactly as
/// before. This pins that surface, because losing any of it would break
/// plugin sources for a change they should never have to see.
#[test]
fn a_rule_name_still_behaves_like_the_string_it_replaced() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let mut tn = Tabnas::make_json();
    let subscriber_seen = seen.clone();
    tn.subscribe_rules(move |rule, _context| {
        assert_eq!(rule.name, "val");
        assert_eq!(rule.name, "val".to_string());
        assert_eq!("val", rule.name);
        assert_ne!(rule.name, "map");
        assert_eq!(rule.name.as_str(), "val");
        assert_eq!(&rule.name[..1], "v");
        assert_eq!(format!("{}", rule.name), "val");
        assert_eq!(format!("{:?}", rule.name), "\"val\"");
        assert_eq!(rule.name.to_string(), "val");
        assert_eq!(String::from(rule.name.clone()), "val");
        fn takes_a_str(name: &str) -> usize {
            name.len()
        }
        assert_eq!(takes_a_str(&rule.name), 3);
        subscriber_seen.lock().unwrap().push(rule.name.clone());
    });

    tn.parse("1").unwrap();
    assert_eq!(*seen.lock().unwrap(), ["val", "val"]);

    // Borrowed as a `str`, so it keys a map the same way the name does.
    let mut by_name: HashMap<RuleName, usize> = HashMap::new();
    by_name.insert(RuleName::from("val"), 7);
    assert_eq!(by_name.get("val"), Some(&7));
}

/// The sharing is the point: two rules of the same name must reach the
/// same allocation, or pushing a rule is still copying its name.
#[test]
fn rules_of_one_name_share_a_single_copy_of_it() {
    let names = Arc::new(Mutex::new(Vec::new()));
    let mut tn = Tabnas::make_json();
    let collected = names.clone();
    tn.subscribe_rules(move |rule, _context| {
        if rule.name == "val" {
            collected.lock().unwrap().push(rule.name.clone());
        }
    });

    tn.parse("[1,2,3]").unwrap();
    let names = names.lock().unwrap();
    assert!(names.len() > 2, "expected several val rules, got {names:?}");
    assert!(
        names
            .windows(2)
            .all(|pair| pair[0].as_str().as_ptr() == pair[1].as_str().as_ptr()),
        "every val rule should point at the same name"
    );
}

/// A rule shares its state with every snapshot taken of it, and copies
/// on the next write instead — but only while a snapshot is still
/// holding the current value. The parse loop snapshots a rule about
/// seven times per input construct, so the difference between copying
/// per snapshot and copying per write-after-snapshot is most of what
/// the rule machinery costs.
///
/// A snapshot still has to keep the value it was taken at. Both halves
/// are pinned here because either one alone is easy to get right.
#[test]
fn a_snapshot_shares_the_rule_until_the_rule_is_written_to() {
    let mut rule = Rule::new("top", Value::Undefined);

    let first = rule.snapshot();
    let second = rule.snapshot();
    assert!(
        Rc::ptr_eq(&first, &second),
        "snapshots with no write between them should be one allocation"
    );

    rule.state = RuleState::Close;
    let third = rule.snapshot();
    assert!(
        !Rc::ptr_eq(&second, &third),
        "a write while a snapshot is outstanding has to copy"
    );
    assert_eq!(
        second.state,
        RuleState::Open,
        "the outstanding snapshot keeps the value it was taken at"
    );
    assert_eq!(third.state, RuleState::Close);

    drop(first);
    drop(second);
    drop(third);
    let held = rule.snapshot();
    let before = Rc::as_ptr(&held);
    drop(held);
    rule.need = 3;
    assert_eq!(
        Rc::as_ptr(&rule.snapshot()),
        before,
        "with no snapshot outstanding, a write should not copy at all"
    );
    assert_eq!(rule.need, 3);
}

/// A token's text is held inline when it is short enough, which is
/// what makes cloning a token cheap. The inline form reads its bytes
/// back as UTF-8 without checking them, so the boundary between the
/// two forms is pinned here: either side of the inline capacity, and
/// multi-byte characters on both sides of it.
#[test]
fn token_text_reads_back_whatever_it_was_given() {
    let cases = [
        String::new(),
        "1".to_string(),
        "#NR".to_string(),
        "a".repeat(21),
        "a".repeat(22),
        "a".repeat(23),
        "a".repeat(200),
        // Multi-byte, just inside and just outside the inline capacity.
        "é".repeat(11),
        "é".repeat(12),
        "🙂".repeat(5),
        "🙂".repeat(6),
        "mixed é 🙂 text".to_string(),
    ];
    for case in cases {
        let text = TokenText::from(case.as_str());
        assert_eq!(text.as_str(), case, "as_str");
        assert_eq!(&*text, case.as_str(), "deref");
        assert_eq!(text.len(), case.len(), "byte length");
        assert_eq!(text.is_empty(), case.is_empty(), "is_empty");
        assert_eq!(text.to_string(), case, "display");
        assert_eq!(format!("{text:?}"), format!("{case:?}"), "debug");
        assert_eq!(text, case, "eq against String");
        assert_eq!(text.clone(), text, "clone round-trips");
        assert_eq!(text.chars().count(), case.chars().count(), "chars");
    }

    // Equality and hashing agree across the two representations.
    let short = TokenText::from("x");
    let long = TokenText::from("x".repeat(40).as_str());
    assert_ne!(short, long);
    let mut by_text: HashMap<TokenText, usize> = HashMap::new();
    by_text.insert(short.clone(), 1);
    by_text.insert(long.clone(), 2);
    assert_eq!(by_text.get("x"), Some(&1));
    assert_eq!(by_text.get("x".repeat(40).as_str()), Some(&2));
}

/// The configuration a parse runs against is prepared once and reused,
/// so the thing that has to be right is noticing when it changes.
/// `options` is a public field callers write to directly, and a stale
/// prepared copy would silently parse against the old configuration.
#[test]
fn configuration_changed_between_parses_takes_effect() {
    let mut tn = Tabnas::make_json();
    assert_eq!(tn.parse("1").unwrap(), Value::Number(1.0));

    // A start rule that does not exist yields undefined rather than a
    // parse, which is an unmistakable signal that the change was seen.
    tn.options.rule.start = "no-such-rule".into();
    assert_eq!(
        tn.parse("1").unwrap(),
        Value::Undefined,
        "a start rule set after the first parse must be honoured"
    );

    tn.options.rule.start = "val".into();
    assert_eq!(
        tn.parse("1").unwrap(),
        Value::Number(1.0),
        "and changing it back must be honoured too"
    );

    // The lexer's configuration comes from the same prepared copy,
    // so it has to notice too.
    let mut lexing = Tabnas::make_json();
    assert!(lexing.parse("[1,2]").is_ok());
    lexing.options.rule.start = "".into();
    assert_eq!(
        lexing.parse("[1,2]").unwrap(),
        Value::Undefined,
        "clearing the start rule after a parse must be honoured"
    );
}
