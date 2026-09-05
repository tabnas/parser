use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, RuleSpec, Tabnas, Tin, TIN_ZZ};

fn rewind_parser(history: Option<Option<usize>>) -> (Tabnas, [Tin; 3]) {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"options":{"rule":{"start":"top"},"fixed":{"token":{"#TA":"a","#TB":"b","#TC":"c"}}}}"##,
        )
        .unwrap();
    if let Some(history) = history {
        parser.options.rewind.history = history;
    }
    let tins = [
        parser.options.token("#TA").unwrap(),
        parser.options.token("#TB").unwrap(),
        parser.options.token("#TC").unwrap(),
    ];
    (parser, tins)
}

#[test]
fn context_records_consumed_tokens_and_legacy_top_accessors() {
    let (mut parser, [ta, tb, tc]) = rewind_parser(None);
    let seen = Arc::new(Mutex::new(Vec::new()));
    let seen_action = seen.clone();
    parser.action_with_context("record", move |_rule, context| {
        let mut result = context
            .v
            .iter()
            .map(|token| token.src.clone())
            .collect::<Vec<_>>();
        result.push(context.v1().unwrap().src.clone());
        result.push(context.v2().unwrap().src.clone());
        *seen_action.lock().unwrap() = result;
        Ok(())
    });
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![ta], vec![tb], vec![tc]],
        a: vec!["record".into()],
        ..Default::default()
    });
    top.close.push(AltSpec {
        s: vec![vec![TIN_ZZ]],
        ..Default::default()
    });
    parser.rule(top);

    parser.parse("abc").unwrap();
    assert_eq!(*seen.lock().unwrap(), ["a", "b", "c", "c", "b"]);
}

#[test]
fn context_rewind_replays_consumed_tokens_before_prefetched_lookahead() {
    let (mut parser, [ta, tb, _]) = rewind_parser(None);
    parser.action_with_context("rewind", |_rule, context| context.rewind(0));

    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![ta], vec![tb]],
        b: 1,
        a: vec!["rewind".into()],
        p: Some("again".into()),
        ..Default::default()
    });
    top.close.push(AltSpec {
        s: vec![vec![TIN_ZZ]],
        ..Default::default()
    });
    parser.rule(top);

    let replayed = Arc::new(Mutex::new(Vec::new()));
    let replayed_action = replayed.clone();
    parser.action_with_context("record-replay", move |_rule, context| {
        *replayed_action.lock().unwrap() =
            context.v.iter().map(|token| token.src.clone()).collect();
        Ok(())
    });
    let mut again = RuleSpec::new("again");
    again.open.push(AltSpec {
        s: vec![vec![ta], vec![tb]],
        a: vec!["record-replay".into()],
        ..Default::default()
    });
    parser.rule(again);

    parser.parse("ab").unwrap();
    assert_eq!(*replayed.lock().unwrap(), ["a", "b"]);
}

#[test]
fn replayed_tokens_are_reannounced_to_lex_subscribers() {
    let (mut parser, [ta, tb, tc]) = rewind_parser(None);
    parser.action_with_context("rewind", |_rule, context| context.rewind(0));

    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![ta], vec![tb], vec![tc]],
        a: vec!["rewind".into()],
        p: Some("again".into()),
        ..Default::default()
    });
    top.close.push(AltSpec {
        s: vec![vec![TIN_ZZ]],
        ..Default::default()
    });
    parser.rule(top);

    let mut again = RuleSpec::new("again");
    again.open.push(AltSpec {
        s: vec![vec![ta], vec![tb], vec![tc]],
        ..Default::default()
    });
    parser.rule(again);

    let seen = Arc::new(Mutex::new(String::new()));
    let seen_subscriber = seen.clone();
    parser.subscribe_tokens(move |token| seen_subscriber.lock().unwrap().push_str(&token.src));
    parser.parse("abc").unwrap();
    assert_eq!(*seen.lock().unwrap(), "abcabc");
}

#[test]
fn bounded_history_rejects_an_evicted_mark() {
    let (mut parser, [ta, _, _]) = rewind_parser(Some(Some(2)));
    parser.action_with_context("rewind-too-far", |_rule, context| context.rewind(0));
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: (0..6).map(|_| vec![ta]).collect(),
        a: vec!["rewind-too-far".into()],
        ..Default::default()
    });
    parser.rule(top);

    let error = parser.parse("a a a a a a").unwrap_err();
    assert_eq!(error.code, "internal");
    assert!(error.detail.contains("outside the retained history"));
}

#[test]
fn serialized_rewind_history_supports_limits_and_unbounded_null() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"rewind":{"history":4}}}"#)
        .unwrap();
    assert_eq!(parser.options.rewind.history, Some(4));
    parser
        .grammar_json(r#"{"options":{"rewind":{"history":null}}}"#)
        .unwrap();
    assert_eq!(parser.options.rewind.history, None);
}
