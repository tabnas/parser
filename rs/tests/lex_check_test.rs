use tabnas::{lexer::Lexer, LexCheckResult, LexCheckToken, Tabnas, Value, TIN_TX};

#[test]
fn serialized_checks_bind_for_every_builtin_matcher_family() {
    let mut parser = Tabnas::new();
    parser.lex_check_ref("@continue", |_| LexCheckResult::Continue);
    parser
        .grammar_json(
            r##"{"options":{
              "match":{"check":"@continue"},
              "fixed":{"check":"@continue"},
              "space":{"check":"@continue"},
              "line":{"check":"@continue"},
              "string":{"check":"@continue"},
              "comment":{"check":"@continue"},
              "number":{"check":"@continue"},
              "text":{"check":"@continue"}
            }}"##,
        )
        .unwrap();

    assert!(parser.options.match_check.is_some());
    assert!(parser.options.fixed.check.is_some());
    assert!(parser.options.space.check.is_some());
    assert!(parser.options.line.check.is_some());
    assert!(parser.options.string.check.is_some());
    assert!(parser.options.comment.check.is_some());
    assert!(parser.options.number.check.is_some());
    assert!(parser.options.text.check.is_some());
}

#[test]
fn check_can_emit_an_owned_prefix_token() {
    let mut parser = Tabnas::new();
    parser.lex_check_ref("@bang", |source| {
        if source.starts_with('!') {
            LexCheckResult::token(LexCheckToken::new(
                "#TX",
                TIN_TX,
                "!",
                Value::String("bang".into()),
            ))
        } else {
            LexCheckResult::Continue
        }
    });
    parser
        .grammar_json(
            r##"{
              "options":{"rule":{"start":"top"},"space":{"check":"@bang"}},
              "rule":{"top":{"open":[{"s":"#TX","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("!").unwrap(), Value::String("bang".into()));
}

#[test]
fn skip_bypasses_only_its_matcher_and_invalid_token_effects_do_not_advance() {
    let mut parser = Tabnas::new();
    parser.lex_check_ref("@skip", |_| LexCheckResult::Skip);
    parser
        .grammar_json(
            r##"{"options":{
              "string":{"check":"@skip"},
              "comment":{"def":{"quote":{"line":true,"start":"\"","lex":true}}}
            }}"##,
        )
        .unwrap();
    let token = Lexer::new(r#""comment"#, parser.options.clone())
        .next_raw_token()
        .unwrap();
    assert_eq!(
        (token.name.as_str(), token.src.as_str()),
        ("#CM", r#""comment"#)
    );

    let mut invalid = Tabnas::new();
    invalid.lex_check_ref("@invalid", |_| {
        LexCheckResult::token(LexCheckToken::new(
            "#TX",
            TIN_TX,
            "not-a-prefix",
            Value::String("wrong".into()),
        ))
    });
    invalid
        .grammar_json(r#"{"options":{"space":{"check":"@invalid"}}}"#)
        .unwrap();
    let token = Lexer::new("!", invalid.options).next_raw_token().unwrap();
    assert_eq!((token.name.as_str(), token.src.as_str()), ("#TX", "!"));
}

#[test]
fn unknown_check_ref_fails_transactionally() {
    let mut parser = Tabnas::make_json();
    let error = match parser.grammar_json(r#"{"options":{"fixed":{"check":"@missing"}}}"#) {
        Ok(_) => panic!("missing lexer check should fail"),
        Err(error) => error,
    };

    assert!(error.0.contains("unknown lexer check"), "{error}");
    assert!(parser.options.fixed.check.is_none());
    assert_eq!(parser.parse("1").unwrap(), Value::Number(1.0));
}
