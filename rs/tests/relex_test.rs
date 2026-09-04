use tabnas::{Tabnas, Value};

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
