use std::sync::{Arc, Mutex};

use tabnas::{ActionError, AltSpec, RuleSpec, Tabnas, Value, TIN_NR, TIN_ZZ};

fn record(
    parser: &mut Tabnas,
    name: &str,
    label: &'static str,
    log: &Arc<Mutex<Vec<&'static str>>>,
) {
    let seen = log.clone();
    parser.state_action_ref(name, move |_, _| {
        seen.lock().unwrap().push(label);
        Ok(())
    });
}

#[test]
fn reserved_state_action_refs_wire_all_lifecycle_phases() {
    let log = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    for (name, label) in [
        ("@top-bo", "bo"),
        ("@top-ao", "ao"),
        ("@top-bc", "bc"),
        ("@top-ac", "ac"),
    ] {
        record(&mut parser, name, label, &log);
    }
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{
                "open":[{"s":"#NR","a":"@value$"}],
                "close":[{"s":"#ZZ"}]
              }}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("1").unwrap(), Value::Number(1.0));
    assert_eq!(*log.lock().unwrap(), ["bo", "ao", "bc", "ac"]);
}

#[test]
fn prepend_append_replace_and_reinstallation_are_deterministic() {
    let log = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    record(&mut parser, "old", "old", &log);
    record(&mut parser, "@top-bo/prepend", "prepend", &log);
    record(&mut parser, "@top-bo/append", "append", &log);

    let mut top = RuleSpec::new("top");
    top.bo.push("old".into());
    top.open.push(AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    });
    top.close.push(AltSpec {
        s: vec![vec![TIN_ZZ]],
        ..Default::default()
    });
    parser.options.rule.start = "top".into();
    parser.rule(top);

    parser
        .grammar_json(r#"{"rule":{"top":{}}}"#)
        .unwrap()
        .grammar_json(r#"{"rule":{"top":{}}}"#)
        .unwrap();
    parser.parse("1").unwrap();
    assert_eq!(*log.lock().unwrap(), ["prepend", "old", "append"]);

    log.lock().unwrap().clear();
    record(&mut parser, "@top-bo/replace", "replace", &log);
    parser.grammar_json(r#"{"rule":{"top":{}}}"#).unwrap();
    parser.parse("1").unwrap();
    assert_eq!(*log.lock().unwrap(), ["replace"]);
}

#[test]
fn state_action_errors_abort_the_parse() {
    let mut parser = Tabnas::new();
    parser.state_action_ref("@top-ao", |_, _| {
        Err(ActionError::new("custom", "lifecycle failed"))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR"}]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("1").unwrap_err();
    assert_eq!(error.code, "custom");
    assert_eq!(error.detail, "lifecycle failed");
    assert_eq!(error.full_source, "1");
    assert_eq!(error.rule, "top");
    assert_eq!(error.rule_stack, ["top"]);
    assert_eq!(error.token.name, "#NR");
    assert_eq!(error.token.src, "1");
}

#[test]
fn action_errors_include_the_complete_nested_rule_stack() {
    let mut parser = Tabnas::new();
    parser.state_action_ref("@child-ac", |_, _| {
        Err(ActionError::new("nested", "child failed"))
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{
                "top":{"open":[{"s":"#NR","p":"child"}]},
                "child":{"open":[{"s":"#NR"}]}
              }
            }"##,
        )
        .unwrap();

    let error = parser.parse("1 2").unwrap_err();
    assert_eq!(error.code, "nested", "{error:?}");
    assert_eq!(error.rule, "child");
    assert_eq!(error.rule_stack, ["top", "child"]);
    assert_eq!(error.token.name, "#NR");
    assert_eq!(error.pos, 2);
}
