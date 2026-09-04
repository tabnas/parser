use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use tabnas::{LexCheckToken, Tabnas, Value, TIN_TX, TIN_VL};

fn value_token(source: &str, value: &str) -> LexCheckToken {
    LexCheckToken::new("#VL", TIN_VL, source, Value::String(value.to_string()))
}

#[test]
fn serialized_custom_matchers_interleave_with_builtin_priority_bands() {
    let late_calls = Arc::new(AtomicUsize::new(0));
    let seen_late = late_calls.clone();
    let mut parser = Tabnas::new();
    parser
        .lex_match_ref("@early", |source| {
            source.starts_with(':').then(|| value_token(":", "custom"))
        })
        .lex_match_ref("@late", move |source| {
            seen_late.fetch_add(1, Ordering::SeqCst);
            source.starts_with(',').then(|| value_token(",", "late"))
        });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#COLON":":","#COMMA":","}},
                "lex":{"match":{
                  "early":{"order":1500000,"make":"@early"},
                  "late":{"order":2500000,"make":"@late"}
                }}
              },
              "rule":{"top":{"open":[
                {"s":"#VL","a":"@value$"},
                {"s":"#COMMA","a":"@value$"}
              ]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse(":").unwrap(), Value::String("custom".into()));
    assert_eq!(parser.parse(",").unwrap(), Value::String(",".into()));
    assert_eq!(late_calls.load(Ordering::SeqCst), 0);
}

#[test]
fn equal_priority_custom_matchers_are_tie_broken_by_name() {
    let mut parser = Tabnas::new();
    parser
        .lex_match_ref("@zed", |source| {
            source.starts_with('x').then(|| value_token("x", "zed"))
        })
        .lex_match_ref("@alpha", |source| {
            source.starts_with('x').then(|| value_token("x", "alpha"))
        });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "lex":{"match":{
                  "zed":{"order":500000,"make":"@zed"},
                  "alpha":{"order":500000,"make":"@alpha"}
                }}
              },
              "rule":{"top":{"open":[{"s":"#VL","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.options.lex.matchers.get_index(0).unwrap().0, "alpha");
    assert_eq!(parser.parse("x").unwrap(), Value::String("alpha".into()));
}

#[test]
fn negotiated_lexing_runs_custom_matchers_speculatively() {
    let wrong_calls = Arc::new(AtomicUsize::new(0));
    let right_calls = Arc::new(AtomicUsize::new(0));
    let seen_wrong = wrong_calls.clone();
    let seen_right = right_calls.clone();
    let mut parser = Tabnas::new();
    parser
        .lex_match_ref("@wrong", move |source| {
            seen_wrong.fetch_add(1, Ordering::SeqCst);
            source
                .starts_with('x')
                .then(|| LexCheckToken::new("#TX", TIN_TX, "x", Value::String("wrong".into())))
        })
        .lex_match_ref("@right", move |source| {
            seen_right.fetch_add(1, Ordering::SeqCst);
            source.starts_with('x').then(|| value_token("x", "right"))
        });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "lex":{"relex":true,"match":{
                  "wrong":{"order":1500000,"make":"@wrong"},
                  "right":{"order":1600000,"make":"@right"}
                }},
                "rule":{"start":"top"},
                "match":{"token":{"#WORD":"@~/^x/"}}
              },
              "rule":{"top":{"open":[{"s":"#VL","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("x").unwrap(), Value::String("right".into()));
    assert_eq!(wrong_calls.load(Ordering::SeqCst), 1);
    assert_eq!(right_calls.load(Ordering::SeqCst), 1);
}

#[test]
fn invalid_effects_and_disabled_entries_do_not_move_the_cursor() {
    let calls = Arc::new(AtomicUsize::new(0));
    let seen = calls.clone();
    let mut parser = Tabnas::new();
    parser.lex_match_ref("@invalid", move |_| {
        seen.fetch_add(1, Ordering::SeqCst);
        Some(value_token("not-a-prefix", "bad"))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#COLON":":"}},
                "lex":{"match":{"invalid":{"order":0,"make":"@invalid"}}}
              },
              "rule":{"top":{"open":[{"s":"#COLON","a":"@value$"}]}}
            }"##,
        )
        .unwrap();
    assert_eq!(parser.parse(":").unwrap(), Value::String(":".into()));
    assert_eq!(calls.load(Ordering::SeqCst), 1);

    parser
        .grammar_json(r#"{"options":{"lex":{"match":{"invalid":{"order":-1}}}}}"#)
        .unwrap();
    assert!(parser.options.lex.matchers.is_empty());
    assert_eq!(parser.parse(":").unwrap(), Value::String(":".into()));
    assert_eq!(calls.load(Ordering::SeqCst), 1);
}

#[test]
fn unknown_custom_matcher_refs_fail_transactionally() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"tag":"before"},"rule":{"kept":{"open":[]}}}"#)
        .unwrap();

    let error = parser
        .grammar_json(
            r#"{"options":{"tag":"after","lex":{"match":{"word":{"order":1,"make":"@missing"}}}},"rule":{"lost":{"open":[]}}}"#,
        )
        .err()
        .expect("unregistered matcher must fail");
    assert!(error
        .to_string()
        .contains("unknown custom lexer matcher function reference"));
    assert_eq!(parser.options.tag, "before");
    assert!(parser.rules.contains_key("kept"));
    assert!(!parser.rules.contains_key("lost"));
}

#[test]
fn custom_matchers_emit_tokens_declared_by_serialized_rules() {
    let mut parser = Tabnas::new();
    parser.lex_match_ref("@word", |source| {
        source
            .starts_with("word")
            .then(|| LexCheckToken::named("#CUSTOM", "word", Value::String("named".into())))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "lex":{"match":{"word":{"order":0,"make":"@word"}}}
              },
              "rule":{"top":{"open":[{"s":"#CUSTOM","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    let custom = parser
        .options
        .token("#CUSTOM")
        .expect("rule slot must declare its token");
    assert_eq!(parser.options.token_name(custom), "#CUSTOM");
    assert_eq!(parser.parse("word").unwrap(), Value::String("named".into()));
}

#[test]
fn token_sets_allocate_custom_members_before_rules_are_loaded() {
    let mut parser = Tabnas::new();
    parser.lex_match_ref("@second", |source| {
        source
            .starts_with('b')
            .then(|| LexCheckToken::named("#SECOND", "b", Value::String("set".into())))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "lex":{"match":{"second":{"order":0,"make":"@second"}}},
                "tokenSet":{"CUSTOM":["#FIRST","#SECOND"]}
              },
              "rule":{"top":{"open":[{"s":"#CUSTOM","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    let members = &parser.options.token_set["CUSTOM"];
    assert_eq!(members.len(), 2);
    assert_eq!(parser.options.token_name(members[0]), "#FIRST");
    assert_eq!(parser.options.token_name(members[1]), "#SECOND");
    assert_eq!(parser.parse("b").unwrap(), Value::String("set".into()));
}

#[test]
fn explicit_token_registration_is_reused_by_match_definitions() {
    let mut parser = Tabnas::new();
    let tin = parser.token("#WORD");
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#WORD":"@/^word/"}}
              },
              "rule":{"top":{"open":[{"s":"#WORD","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.options.token("#WORD"), Some(tin));
    assert_eq!(parser.parse("word").unwrap(), Value::String("word".into()));
}
