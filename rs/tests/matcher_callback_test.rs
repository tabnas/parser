use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use tabnas::{MatchTokenResult, Tabnas, Value};

#[test]
fn serialized_match_token_refs_produce_owned_spans_and_values() {
    let mut parser = Tabnas::new();
    parser.match_token_ref("@upper-word", false, |remaining| {
        let source: String = remaining
            .chars()
            .take_while(|character| character.is_ascii_alphabetic())
            .collect();
        (!source.is_empty()).then(|| {
            MatchTokenResult::new(source.clone(), Value::String(source.to_ascii_uppercase()))
        })
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#WORD":"@upper-word"}}
              },
              "rule":{"top":{"open":[{"s":"#WORD","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(
        parser.parse("hello").unwrap(),
        Value::String("HELLO".into())
    );
}

#[test]
fn function_matchers_obey_the_parser_slot_gate_unless_eager() {
    for (eager, expected_calls) in [(false, 0), (true, 1)] {
        let calls = Arc::new(AtomicUsize::new(0));
        let seen = calls.clone();
        let mut parser = Tabnas::new();
        parser.match_token_ref("@word", eager, move |remaining| {
            seen.fetch_add(1, Ordering::SeqCst);
            Some(MatchTokenResult::new(
                remaining.chars().next()?.to_string(),
                Value::String("word".into()),
            ))
        });
        parser
            .grammar_json(
                r##"{
                  "clear":true,
                  "options":{
                    "rule":{"start":"top"},
                    "match":{"token":{"#WORD":"@word"}}
                  },
                  "rule":{"top":{"open":[{"s":"#NR"}]}}
                }"##,
            )
            .unwrap();

        assert!(parser.parse("x").is_err());
        assert_eq!(
            calls.load(Ordering::SeqCst),
            expected_calls,
            "eager={eager}"
        );
    }
}

#[test]
fn invalid_function_matcher_spans_are_ignored_without_moving_the_cursor() {
    let mut parser = Tabnas::new();
    parser.match_token_ref("@invalid", false, |_remaining| {
        Some(MatchTokenResult::new(
            "not-a-prefix",
            Value::String("bad".into()),
        ))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#INVALID":"@invalid"}}
              },
              "rule":{"top":{"open":[
                {"s":"#INVALID"},
                {"s":"#TX","a":"@value$"}
              ]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("ok").unwrap(), Value::String("ok".into()));
}

#[test]
fn unknown_function_matcher_refs_fail_transactionally() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"tag":"before"},"rule":{"kept":{"open":[]}}}"#)
        .unwrap();

    let error = parser
        .grammar_json(
            r##"{"options":{"match":{"token":{"#WORD":"@missing"}}},"rule":{"lost":{"open":[]}}}"##,
        )
        .err()
        .expect("unregistered matcher must fail");
    assert!(error
        .to_string()
        .contains("unknown token matcher function reference"));
    assert_eq!(parser.options.tag, "before");
    assert!(parser.rules.contains_key("kept"));
    assert!(!parser.rules.contains_key("lost"));
}
