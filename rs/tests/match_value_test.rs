use tabnas::{lexer::Lexer, MatchTokenResult, Tabnas, Value};

fn value_rule(options: &str) -> String {
    format!(
        r##"{{
          "clear":true,
          "options":{{"rule":{{"start":"top"}},"match":{options}}},
          "rule":{{"top":{{"open":[{{"s":"#VL","a":"@value$"}}]}}}}
        }}"##
    )
}

#[test]
fn regexp_value_matchers_run_before_eager_named_tokens() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&value_rule(
            r##"{
              "token":{"#WORD":"@~/^yes/"},
              "value":{"truth":{"match":"@/^yes/","val":true}}
            }"##,
        ))
        .unwrap();

    assert_eq!(parser.parse("yes").unwrap(), Value::Bool(true));
}

#[test]
fn regexp_value_transform_receives_whole_match_and_capture_groups() {
    let mut parser = Tabnas::new();
    parser.value_transform_ref("@tag", |groups| {
        Value::String(format!("{}:{}:{}", groups[0], groups[1], groups[2]))
    });
    parser
        .grammar_json(&value_rule(
            r##"{
              "value":{"tag":{"match":"@/^([a-z]+):([0-9]+)/","val":"@tag"}}
            }"##,
        ))
        .unwrap();

    assert_eq!(
        parser.parse("alpha:42").unwrap(),
        Value::String("alpha:42:alpha:42".into())
    );
}

#[test]
fn function_value_matcher_can_emit_an_owned_prefix_value() {
    let mut parser = Tabnas::new();
    parser.match_value_ref("@money", |source| {
        let amount = source.strip_prefix('$')?;
        let digits = amount
            .chars()
            .take_while(char::is_ascii_digit)
            .collect::<String>();
        (!digits.is_empty()).then(|| {
            MatchTokenResult::new(
                format!("${digits}"),
                Value::Number(digits.parse::<f64>().unwrap()),
            )
        })
    });
    parser
        .grammar_json(&value_rule(r##"{"value":{"money":{"match":"@money"}}}"##))
        .unwrap();

    assert_eq!(parser.parse("$42").unwrap(), Value::Number(42.0));
}

#[test]
fn invalid_function_match_span_is_ignored_without_advancing() {
    let mut parser = Tabnas::new();
    parser.match_value_ref("@invalid", |_| {
        Some(MatchTokenResult::new(
            "not-a-prefix",
            Value::String("wrong".into()),
        ))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"value":{"invalid":{"match":"@invalid"}}}
              },
              "rule":{"top":{"open":[{"s":"#TX","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    let token = Lexer::new("x", parser.options.clone())
        .next_raw_token()
        .unwrap();
    assert_eq!((token.name.as_str(), token.src.as_str()), ("#TX", "x"));
    assert_eq!(parser.parse("x").unwrap(), Value::String("x".into()));
}

#[test]
fn missing_value_transform_defaults_to_the_match_source() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&value_rule(
            r##"{"value":{"raw":{"match":"@/^&[a-z]+/"}}}"##,
        ))
        .unwrap();

    assert_eq!(parser.parse("&raw").unwrap(), Value::String("&raw".into()));
}

#[test]
fn unknown_function_matcher_ref_fails_transactionally() {
    let mut parser = Tabnas::make_json();
    let error = match parser
        .grammar_json(r##"{"options":{"match":{"value":{"x":{"match":"@missing"}}}}}"##)
    {
        Ok(_) => panic!("missing value matcher should fail"),
        Err(error) => error,
    };

    assert!(error.0.contains("unknown value matcher"), "{error}");
    assert!(parser.options.match_values.is_empty());
    assert_eq!(parser.parse("1").unwrap(), Value::Number(1.0));
}
