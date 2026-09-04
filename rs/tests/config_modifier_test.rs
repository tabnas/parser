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
fn config_modifiers_rebuild_from_raw_options_without_compounding() {
    let mut parser = Tabnas::new();
    parser.config_modifier_ref("@suffix", |options| options.tag.push('A'));
    parser
        .grammar_json(r#"{"options":{"tag":"base","config":{"modify":{"suffix":"@suffix"}}}}"#)
        .unwrap();
    assert_eq!(parser.options.tag, "baseA");

    // An unrelated overlay performs a fresh configure. The previous
    // modifier result is not itself input to the next configure.
    parser
        .grammar_json(r#"{"options":{"number":{"lex":false}}}"#)
        .unwrap();
    assert_eq!(parser.options.tag, "baseA");

    let child = parser
        .derive(|options| options.string.allow_control = true)
        .unwrap();
    assert_eq!(child.options.tag, "baseA");
    assert!(child.options.string.allow_control);
}

#[test]
fn complete_config_modifier_receives_stable_raw_options() {
    let seen = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
    let callback_seen = seen.clone();
    let mut parser = Tabnas::new();
    parser.config_modifier_with_options_ref("@full", move |config, options| {
        callback_seen
            .lock()
            .unwrap()
            .push((config.tag.clone(), options.tag.clone()));
        config.tag.push_str("-resolved");
    });
    parser
        .grammar_json(r#"{"options":{"tag":"raw","config":{"modify":{"full":"@full"}}}}"#)
        .unwrap();
    parser
        .grammar_json(r#"{"options":{"number":{"lex":false}}}"#)
        .unwrap();

    assert_eq!(parser.options.tag, "raw-resolved");
    assert_eq!(
        *seen.lock().unwrap(),
        [("raw".into(), "raw".into()), ("raw".into(), "raw".into())]
    );
}

#[test]
fn derived_instances_retain_typed_function_reference_registries() {
    let mut parent = Tabnas::new();
    parent.config_modifier_ref("@child", |options| options.number.lex = false);
    let mut child = parent.derive(|_| {}).unwrap();
    child
        .grammar_json(r#"{"options":{"config":{"modify":{"child":"@child"}}}}"#)
        .unwrap();
    assert!(!child.options.number.lex);
}

#[test]
fn set_options_reconfigures_without_dropping_rules_or_compounding_modifiers() {
    let mut parser = Tabnas::new();
    parser.config_modifier_ref("@suffix", |options| options.tag.push('A'));
    parser
        .grammar_json(
            r#"{"options":{"tag":"base","config":{"modify":{"suffix":"@suffix"}}},"rule":{"kept":{"open":[]}}}"#,
        )
        .unwrap();

    parser
        .set_options(|options| options.number.lex = false)
        .unwrap();
    assert_eq!(parser.options.tag, "baseA");
    assert!(!parser.options.number.lex);
    assert!(parser.rules.contains_key("kept"));
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
