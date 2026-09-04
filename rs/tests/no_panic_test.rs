use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use tabnas::lexer::Lexer;
use tabnas::{
    AltSpec, ErrorSuffixContext, LexCheckToken, MatchTokenResult, RuleSpec, Tabnas, TabnasError,
    Value, TIN_NR, TIN_ZZ,
};

fn number_parser() -> Tabnas {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "top".into();
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    });
    top.close.push(AltSpec {
        s: vec![vec![TIN_ZZ]],
        ..Default::default()
    });
    parser.rule(top);
    parser
}

fn assert_top_site(error: &TabnasError, source: &str) {
    assert_eq!(error.code, "internal", "{error:?}");
    assert_eq!(error.full_source, source, "{error:?}");
    assert_eq!(error.rule, "top", "{error:?}");
    assert_eq!(error.rule_stack, ["top"], "{error:?}");
}

#[test]
fn parse_callbacks_cannot_unwind_through_public_entry_points() {
    let mut parser = number_parser();
    parser.action("boom", |_| panic!("action exploded"));
    parser.rules.get_mut("top").unwrap().open[0]
        .a
        .push("boom".into());

    let error = parser.parse("1").unwrap_err();
    assert_eq!(error.code, "internal");
    assert!(error.src.contains("action exploded"));
    assert_eq!(error.rule, "top");
    assert_eq!(error.rule_stack, ["top"]);
    assert_eq!(error.token.name, "#NR");

    let recovered = parser.parse_recover("1");
    assert!(recovered.value.is_none());
    assert_eq!(recovered.fatal.unwrap().code, "internal");

    let continuations = parser.continuations("1");
    assert_eq!(continuations.tokens, ["#NR"]);
}

#[test]
fn lexer_callbacks_cannot_unwind_through_standalone_lexer() {
    let mut parser = Tabnas::new();
    parser.lex_match_ref("@boom", |_remaining| -> Option<LexCheckToken> {
        panic!("matcher exploded")
    });
    parser
        .grammar_json(r#"{"options":{"lex":{"match":{"boom":{"order":0,"make":"@boom"}}}}}"#)
        .unwrap();

    let mut lexer = Lexer::new("x", parser.options);
    let error = lexer.next_raw_token().unwrap_err();
    assert_eq!(error.code, "internal");
    assert!(error.src.contains("matcher exploded"));
    assert_eq!(error.pos, 0);
}

#[test]
fn typed_error_panics_keep_their_diagnostic_code() {
    let mut parser = number_parser();
    parser.action("boom", |_| {
        std::panic::panic_any(TabnasError::new("demo", "bad value", "", 0, 1, 1))
    });
    parser.rules.get_mut("top").unwrap().open[0]
        .a
        .push("boom".into());

    let error = parser.parse("1").unwrap_err();
    assert_eq!(error.code, "demo");
    assert_eq!(error.full_source, "1");
}

#[test]
fn panicking_error_suffix_renders_a_safe_fallback() {
    let mut parser = number_parser();
    parser.error_suffix_ref("@boom", |_context: &ErrorSuffixContext| {
        panic!("suffix exploded")
    });
    parser
        .grammar_json(r#"{"options":{"errmsg":{"suffix":"@boom"}}}"#)
        .unwrap();

    let mut error = parser.parse("x").unwrap_err();
    let rendered = error.to_string();
    assert!(rendered.contains("errmsg.suffix callback panicked"));

    // Re-rendering must remain safe and deterministic.
    assert_eq!(rendered, error.to_string());
    error.detail = "still usable".into();
}

#[test]
fn panicking_match_token_callback_is_an_internal_error() {
    let mut parser = Tabnas::new();
    parser.match_token_ref("@boom", true, |_remaining| -> Option<MatchTokenResult> {
        panic!("token matcher exploded")
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#WORD":"@boom"}}
              },
              "rule":{"top":{"open":[{"s":"#WORD"}]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("x").unwrap_err();
    assert_top_site(&error, "x");
    assert!(error.src.contains("token matcher exploded"));
    assert_eq!(error.expected, ["#WORD"]);
    assert_eq!(Value::Undefined, parser.parse("").unwrap());
}

#[test]
fn callback_panics_during_negotiated_relex_are_not_hidden() {
    let calls = Arc::new(AtomicUsize::new(0));
    let seen = calls.clone();
    let mut parser = Tabnas::new();
    parser.lex_match_ref("@cut", move |_remaining| {
        if seen.fetch_add(1, Ordering::SeqCst) == 0 {
            Some(LexCheckToken::named(
                "#WORD",
                "x",
                Value::String("x".into()),
            ))
        } else {
            panic!("relex exploded")
        }
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "lex":{
                  "relex":true,
                  "match":{"cut":{"order":0,"make":"@cut"}}
                },
                "rule":{"start":"top"}
              },
              "rule":{"top":{"open":[{"s":"#VL"}]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("x").unwrap_err();
    assert_eq!(error.code, "internal");
    assert!(error.src.contains("relex exploded"));
    assert_eq!(error.rule, "top");
    assert_eq!(error.rule_stack, ["top"]);
    assert_eq!(error.token.name, "#TX");
    assert_eq!(calls.load(Ordering::SeqCst), 2);
}

#[test]
fn panicking_alternate_callbacks_keep_the_match_site() {
    for field in ["c", "h", "e", "b", "p", "r"] {
        let mut parser = Tabnas::new();
        match field {
            "c" => {
                parser.alt_condition("@boom", |_, _| panic!("condition exploded"));
            }
            "h" => {
                parser.alt_modifier("@boom", |_, _, _| panic!("modifier exploded"));
            }
            "e" => {
                parser.alt_error("@boom", |_, _| panic!("error hook exploded"));
            }
            "b" => {
                parser.alt_backtrack("@boom", |_, _| panic!("backtrack exploded"));
            }
            "p" => {
                parser.alt_push("@boom", |_, _| panic!("push exploded"));
            }
            "r" => {
                parser.alt_replace("@boom", |_, _| panic!("replace exploded"));
            }
            _ => unreachable!(),
        }
        parser
            .grammar_json(&format!(
                r##"{{
                  "clear":true,
                  "options":{{"rule":{{"start":"top"}}}},
                  "rule":{{"top":{{"open":[{{"s":"#NR","{field}":"@boom"}}]}}}}
                }}"##
            ))
            .unwrap();

        let error = parser.parse("1").unwrap_err();
        assert_top_site(&error, "1");
        assert_eq!(error.token.name, "#NR", "field {field}: {error:?}");
        assert_eq!(error.expected, ["#NR"], "field {field}: {error:?}");
        assert!(error.src.contains("exploded"), "field {field}: {error:?}");
    }
}

#[test]
fn panicking_nested_callback_keeps_the_complete_rule_stack() {
    let mut parser = Tabnas::new();
    parser.alt_condition("@boom", |_, _| panic!("nested condition exploded"));
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{
                "top":{"open":[{"s":"#NR","p":"child"}]},
                "child":{"open":[{"s":"#NR","c":"@boom"}]}
              }
            }"##,
        )
        .unwrap();

    let error = parser.parse("1 2").unwrap_err();
    assert_eq!(error.code, "internal", "{error:?}");
    assert_eq!(error.full_source, "1 2");
    assert_eq!(error.rule, "child");
    assert_eq!(error.rule_stack, ["top", "child"]);
    assert_eq!(error.token.name, "#NR");
    assert_eq!(error.pos, 2);
}

#[test]
fn panicking_subscribers_and_budget_checks_keep_parse_context() {
    let mut rule_parser = number_parser();
    rule_parser.subscribe_rules(|_, _| panic!("rule subscriber exploded"));
    let error = rule_parser.parse("1").unwrap_err();
    assert_top_site(&error, "1");
    assert!(error.src.contains("rule subscriber exploded"));

    let mut lex_parser = number_parser();
    lex_parser.subscribe_lex(|_, _, _| panic!("lex subscriber exploded"));
    let error = lex_parser.parse("1").unwrap_err();
    assert_top_site(&error, "1");
    assert_eq!(error.token.name, "#NR");

    let mut token_parser = number_parser();
    token_parser.subscribe_tokens(|_| panic!("token subscriber exploded"));
    let error = token_parser.parse("1").unwrap_err();
    assert_top_site(&error, "1");
    assert_eq!(error.token.name, "#NR");

    let mut done_parser = number_parser();
    done_parser.subscribe_rule_done(|_, _, _| panic!("ruleDone subscriber exploded"));
    let error = done_parser.parse("1").unwrap_err();
    assert_top_site(&error, "1");
    assert_eq!(error.token.name, "#NR");

    let mut budget_parser = number_parser();
    budget_parser.parse_budget(1, |_| panic!("budget check exploded"));
    let error = budget_parser.parse("1").unwrap_err();
    assert_top_site(&error, "1");
    assert_eq!(error.token.name, "#NR");
}

#[test]
fn recursive_rule_done_panics_preserve_repeated_rule_names() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR","p":"top"}]}}
            }"##,
        )
        .unwrap();
    parser.subscribe_rule_done(|rule, _, _| {
        if rule.d == 1 {
            panic!("recursive ruleDone exploded");
        }
    });

    let error = parser.parse("1 2").unwrap_err();
    assert_eq!(error.code, "internal", "{error:?}");
    assert_eq!(error.rule, "top");
    assert_eq!(error.rule_stack, ["top", "top"]);
    assert_eq!(error.token.name, "#NR");
    assert_eq!(error.pos, 2);
}

#[test]
fn panicking_prepare_is_identified_before_a_rule_exists() {
    let mut parser = number_parser();
    parser.parse_prepare(|_| panic!("prepare exploded"));

    let error = parser.parse("1").unwrap_err();
    assert_eq!(error.code, "internal", "{error:?}");
    assert_eq!(error.full_source, "1");
    assert!(error.src.contains("parse.prepare: prepare exploded"));
    assert!(error.rule.is_empty());
    assert!(error.rule_stack.is_empty());
}
