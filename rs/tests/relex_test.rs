use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use tabnas::{LexCheckResult, LexCheckToken, Tabnas, Value, TIN_VL};

fn fixed_parser(relex: bool, alternatives: &str) -> Tabnas {
    let mut parser = Tabnas::new();
    let grammar = format!(
        r##"{{
          "clear":true,
          "options":{{
            "lex":{{"relex":{relex}}},
            "rule":{{"start":"top"}},
            "fixed":{{"token":{{"#LONG":"ab","#SHORT":"a","#TB":"b","#TC":"c"}}}}
          }},
          "rule":{{"top":{{"open":{alternatives}}}}}
        }}"##
    );
    parser.grammar_json(&grammar).unwrap();
    parser
}

#[test]
fn negotiated_lexing_recuts_a_longer_fixed_token() {
    let alternatives = r##"[{"s":"#SHORT #TB"}]"##;
    assert!(fixed_parser(false, alternatives).parse("ab").is_err());
    assert!(fixed_parser(true, alternatives).parse("ab").is_ok());
}

#[test]
fn failed_alternate_restores_the_original_cut() {
    let alternatives = r##"[{"s":"#SHORT #TC"},{"s":"#AA"}]"##;
    assert!(fixed_parser(false, alternatives).parse("ab").is_ok());
    assert!(fixed_parser(true, alternatives).parse("ab").is_ok());
}

#[test]
fn eager_regex_class_can_be_recut_as_a_literal() {
    for (relex, accepted) in [(false, false), (true, true)] {
        let mut parser = Tabnas::new();
        let grammar = format!(
            r##"{{
              "clear":true,
              "options":{{
                "lex":{{"relex":{relex}}},
                "rule":{{"start":"top"}},
                "match":{{"token":{{"#WS":"@~/^[ \\t\\n]+/"}}}},
                "fixed":{{"token":{{"#NL":"\n"}}}},
                "tokenSet":{{"IGNORE":[]}}
              }},
              "rule":{{"top":{{"open":[{{"s":"#NL"}}]}}}}
            }}"##
        );
        parser.grammar_json(&grammar).unwrap();
        let result = parser.parse("\n");
        assert_eq!(result.is_ok(), accepted, "relex={relex}: {result:?}");
    }
}

#[test]
fn deferred_bad_token_can_only_be_recut_to_a_named_token() {
    let mut text = Tabnas::new();
    text.grammar_json(
        r##"{"clear":true,"options":{"lex":{"relex":true},"rule":{"start":"top"}},"rule":{"top":{"open":[{"s":"#TX","a":"@value$"}]}}}"##,
    )
    .unwrap();
    assert_eq!(
        text.parse(r#""unterminated"#).unwrap(),
        Value::String(r#""unterminated"#.into())
    );

    let mut wildcard = Tabnas::new();
    wildcard
        .grammar_json(
            r##"{"clear":true,"options":{"lex":{"relex":true},"rule":{"start":"top"}},"rule":{"top":{"open":[{"s":"#AA"}]}}}"##,
        )
        .unwrap();
    let error = wildcard.parse(r#""\uZZZZ""#).unwrap_err();
    assert_eq!(error.code, "invalid_unicode");
}

#[test]
fn serialized_relex_option_defaults_off_and_is_applied() {
    assert!(!Tabnas::new().options.lex.relex);
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"lex":{"relex":true}}}"#)
        .unwrap();
    assert!(parser.options.lex.relex);
}

#[test]
fn negotiated_lexing_does_not_enter_text_to_produce_a_value() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "lex":{"relex":true},
                "rule":{"start":"top"},
                "match":{"token":{"#WORD":"@~/^@abc/"}},
                "value":{"def":{"tag":{"match":"@/^@[a-z]+/","consume":true}}}
              },
              "rule":{"top":{"open":[{"s":"#VL","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    // The first cut is the eager #WORD. TS and Go do not enter the text
    // matcher when re-lexing asks only for #VL, even though that matcher can
    // produce #VL through a value definition.
    assert!(parser.parse("@abc").is_err());
}

#[test]
fn negotiated_lexing_gates_number_checks_by_number_identity() {
    let calls = Arc::new(AtomicUsize::new(0));
    let checked = calls.clone();
    let mut parser = Tabnas::new();
    parser.lex_check_ref("@as-value", move |_| {
        checked.fetch_add(1, Ordering::SeqCst);
        LexCheckResult::token(LexCheckToken::new(
            "#VL",
            TIN_VL,
            "42",
            Value::String("number-check".into()),
        ))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "lex":{"relex":true},
                "rule":{"start":"top"},
                "match":{"token":{"#DIGITS":"@~/^42/"}},
                "number":{"check":"@as-value"}
              },
              "rule":{"top":{"open":[{"s":"#VL","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert!(parser.parse("42").is_err());
    assert_eq!(calls.load(Ordering::SeqCst), 0);
}

#[test]
fn fixed_check_can_satisfy_a_negotiated_non_fixed_token() {
    let mut parser = Tabnas::new();
    parser.lex_check_ref("@as-value", |source| {
        if source.starts_with('x') {
            LexCheckResult::token(LexCheckToken::new(
                "#VL",
                TIN_VL,
                "x",
                Value::String("fixed-check".into()),
            ))
        } else {
            LexCheckResult::Continue
        }
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "lex":{"relex":true},
                "rule":{"start":"top"},
                "match":{"token":{"#WORD":"@~/^x/"}},
                "fixed":{"check":"@as-value"}
              },
              "rule":{"top":{"open":[{"s":"#VL","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(
        parser.parse("x").unwrap(),
        Value::String("fixed-check".into())
    );
}
