use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, RuleSpec, RuleState, Tabnas, Value, TIN_NR, TIN_TX};

#[test]
fn lex_subscribers_see_ignored_trivia_and_the_active_rule() {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "top".into();
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![TIN_TX]],
        ..Default::default()
    });
    parser.rule(top);

    let seen = Arc::new(Mutex::new(Vec::new()));
    let seen_subscriber = seen.clone();
    parser.subscribe_lex(move |token, rule, _context| {
        seen_subscriber
            .lock()
            .unwrap()
            .push((token.name.clone(), rule.name.clone()));
    });
    assert_eq!(parser.parse("a b").unwrap_err().code, "unexpected");
    assert!(seen
        .lock()
        .unwrap()
        .contains(&("#SP".to_string(), "top".to_string())));
}

#[test]
fn lex_subscriber_token_changes_reach_the_parser() {
    let mut parser = Tabnas::make_json();
    parser.subscribe_lex(|token, _rule, _context| {
        if token.name == "#NR" {
            token.val = Value::Number(2.0);
        }
    });
    assert_eq!(parser.parse("1").unwrap(), Value::Number(2.0));
}

#[test]
fn rule_subscribers_wrap_each_successful_rule_pass() {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "top".into();
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![TIN_TX]],
        g: "word,entry".into(),
        ..Default::default()
    });
    parser.rule(top);

    let before = Arc::new(Mutex::new(Vec::new()));
    let before_subscriber = before.clone();
    parser.subscribe_rules(move |rule, context| {
        before_subscriber
            .lock()
            .unwrap()
            .push((rule.name.clone(), rule.state, context.iteration));
    });

    let done = Arc::new(Mutex::new(Vec::new()));
    let done_subscriber = done.clone();
    parser.subscribe_rule_done(move |rule, _context, event| {
        done_subscriber.lock().unwrap().push((
            rule.name.clone(),
            event.state,
            event.alt.clone(),
            rule.o.first().map(|token| token.src.clone()),
        ));
    });

    parser.parse("hello").unwrap();
    assert_eq!(
        *before.lock().unwrap(),
        [
            ("top".to_string(), RuleState::Open, 0),
            ("top".to_string(), RuleState::Close, 1),
        ]
    );
    let done = done.lock().unwrap();
    assert_eq!(done.len(), 2);
    assert_eq!(done[0].1, RuleState::Open);
    assert_eq!(done[0].2.as_ref().unwrap().g, ["word", "entry"]);
    assert_eq!(done[0].3.as_deref(), Some("hello"));
    assert_eq!(done[1].1, RuleState::Close);
    assert!(done[1].2.is_none(), "no close alternatives means no alt");
}

#[test]
fn rule_done_reports_the_failure_token_when_alternatives_exist() {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "top".into();
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    });
    parser.rule(top);

    let failure = Arc::new(Mutex::new(None));
    let failure_subscriber = failure.clone();
    parser.subscribe_rule_done(move |_rule, _context, event| {
        *failure_subscriber.lock().unwrap() = Some(event.clone());
    });

    assert_eq!(parser.parse("word").unwrap_err().code, "unexpected");
    let event = failure.lock().unwrap().clone().expect("failure event");
    assert_eq!(event.state, RuleState::Open);
    assert_eq!(event.alt.unwrap().err.unwrap().name, "#TX");
}

#[test]
fn rule_done_exposes_push_and_backtrack_routing() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r##"{"options":{"rule":{"start":"top"},"fixed":{"token":{"#TA":"a"}}}}"##)
        .unwrap();
    let ta = parser.options.token("#TA").unwrap();
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![ta]],
        b: 1,
        p: Some("child".into()),
        ..Default::default()
    });
    parser.rule(top);
    let mut child = RuleSpec::new("child");
    child.open.push(AltSpec {
        s: vec![vec![ta]],
        ..Default::default()
    });
    parser.rule(child);

    let routing = Arc::new(Mutex::new(None));
    let routing_subscriber = routing.clone();
    parser.subscribe_rule_done(move |rule, _context, event| {
        if rule.name == "top" && event.state == RuleState::Open {
            *routing_subscriber.lock().unwrap() = event.alt.clone();
        }
    });
    parser.parse("a").unwrap();
    let route = routing.lock().unwrap().clone().unwrap();
    assert_eq!(route.b, 1);
    assert_eq!(route.p, "child");
    assert_eq!(route.r, "");
}
