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
    assert_eq!(error.code, "internal");
    assert!(error.src.contains("token matcher exploded"));
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
    assert_eq!(calls.load(Ordering::SeqCst), 2);
}
