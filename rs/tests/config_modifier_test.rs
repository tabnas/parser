use tabnas::{Tabnas, Value};

#[test]
fn serialized_config_modifiers_run_in_declaration_order_and_reapply() {
    let mut parser = Tabnas::new();
    parser
        .config_modifier_ref("@first", |options| options.tag.push('A'))
        .config_modifier_ref("@second", |options| {
            options.tag.push('B');
            options.number.lex = false;
        });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "tag":"base",
                "rule":{"start":"top"},
                "config":{"modify":{
                  "second":"@second",
                  "first":"@first"
                }}
              },
              "rule":{"top":{"open":[{"s":"#TX","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.options.tag, "baseBA");
    assert!(!parser.options.number.lex);
    assert_eq!(parser.parse("12").unwrap(), Value::String("12".into()));

    parser
        .grammar_json(r#"{"options":{"tag":"next","number":{"lex":true}}}"#)
        .unwrap();
    assert_eq!(parser.options.tag, "nextBA");
    assert!(!parser.options.number.lex);

    parser
        .grammar_json(r#"{"options":{"tag":"last","config":{"modify":{"second":null}}}}"#)
        .unwrap();
    assert_eq!(parser.options.tag, "lastA");
    assert_eq!(parser.options.config_modify.len(), 1);
}

#[test]
fn invalid_config_modifier_refs_fail_transactionally() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"tag":"before"},"rule":{"kept":{"open":[]}}}"#)
        .unwrap();

    let error = parser
        .grammar_json(
            r#"{"options":{"tag":"after","config":{"modify":{"x":"@missing"}}},"rule":{"lost":{"open":[]}}}"#,
        )
        .err()
        .expect("unregistered modifier must fail");
    assert!(error
        .to_string()
        .contains("unknown config modifier function reference"));
    assert_eq!(parser.options.tag, "before");
    assert!(parser.rules.contains_key("kept"));
    assert!(!parser.rules.contains_key("lost"));
}

#[test]
fn panicking_config_modifiers_return_a_transactional_error() {
    let mut parser = Tabnas::new();
    parser.config_modifier_ref("@panic", |_| panic!("boom"));
    parser
        .grammar_json(r#"{"options":{"tag":"before"}}"#)
        .unwrap();

    let error = parser
        .grammar_json(r#"{"options":{"tag":"after","config":{"modify":{"bad":"@panic"}}}}"#)
        .err()
        .expect("panic must become a grammar error");
    assert!(error.to_string().contains("config modifier bad panicked"));
    assert_eq!(parser.options.tag, "before");
    assert!(parser.options.config_modify.is_empty());
}
