use tabnas::{Tabnas, Value};

fn error_grammar(options: &str) -> String {
    format!(
        r##"{{
          "clear":true,
          "options":{{
            "rule":{{"start":"top"}},
            "color":{{"active":false}},
            {options}
          }},
          "rule":{{"top":{{"open":[{{"s":"#NR"}}]}}}}
        }}"##
    )
}

#[test]
fn serialized_message_hint_name_and_text_suffix_reach_errors() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&error_grammar(
            r##"
              "error":{"unexpected":"bad {src} at {row}:{col}"},
              "hint":{"unexpected":"try {src}"},
              "errmsg":{"name":"vox","suffix":"TAIL"}
            "##,
        ))
        .unwrap();

    let error = parser.parse("x").unwrap_err();
    assert_eq!(error.detail, "bad x at 1:1");
    assert_eq!(error.hint, "try x");
    assert_eq!(error.tag, "vox");

    let rendered = error.to_string();
    assert!(rendered.starts_with("[vox/unexpected]: bad x at 1:1"));
    assert!(rendered.contains("\n  try x"));
    assert!(rendered.ends_with("\nTAIL"));
    assert!(!rendered.contains("\x1b["));

    let diagnostic = serde_json::to_value(&error).unwrap();
    assert_eq!(diagnostic["message"], "bad x at 1:1");
    assert_eq!(diagnostic["hint"], "try x");
}

#[test]
fn standard_suffix_includes_link_instance_rule_and_token_context() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&error_grammar(
            r##"
              "tag":"fixture",
              "errmsg":{"suffix":true,"link":"https://docs.example/errors"}
            "##,
        ))
        .unwrap();

    let rendered = parser.parse("x").unwrap_err().to_string();
    assert!(rendered.contains("https://docs.example/errors"));
    assert!(rendered.contains("--internal: tag=fixture; rule=top~o; token=#TX; plugins=--"));
}

#[test]
fn false_suffix_suppresses_internal_diagnostics() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&error_grammar(
            r##""errmsg":{"suffix":false,"link":"not-rendered"}"##,
        ))
        .unwrap();

    let rendered = parser.parse("x").unwrap_err().to_string();
    assert!(!rendered.contains("--internal:"));
    assert!(!rendered.contains("not-rendered"));
}

#[test]
fn unknown_catalogue_entries_fall_back_and_interpolate_token_details() {
    let mut parser = Tabnas::new();
    parser.alt_error("@raise", |rule, _context| {
        let mut token = rule.o0()?.clone();
        token.bad("mystery_code");
        token
            .use_data
            .insert("what".into(), Value::String("it".into()));
        Some(token)
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "error":{"unknown":"unknown {code}: {what}"},
                "hint":{"unknown":"Details: {details}"},
                "color":{"active":false},
                "errmsg":{"suffix":false}
              },
              "rule":{"top":{"open":[{"s":"#NR","e":"@raise"}]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("42").unwrap_err();
    assert_eq!(error.code, "mystery_code");
    assert_eq!(error.detail, "unknown mystery_code: it");
    assert_eq!(error.hint, "Details: {what:it}");
    assert!(!error.hint.contains("{details}"));
}

#[test]
fn invalid_diagnostic_options_fail_transactionally() {
    for document in [
        r#"{"options":{"error":{"unexpected":1}}}"#,
        r#"{"options":{"hint":{"unexpected":[]}}}"#,
        r#"{"options":{"errmsg":{"suffix":[]}}}"#,
        r#"{"options":{"color":{"reset":1}}}"#,
    ] {
        let mut parser = Tabnas::new();
        parser
            .grammar_json(r#"{"options":{"tag":"before"}}"#)
            .unwrap();
        assert!(
            parser.grammar_json(document).is_err(),
            "accepted {document}"
        );
        assert_eq!(parser.options.tag, "before");
    }
}
