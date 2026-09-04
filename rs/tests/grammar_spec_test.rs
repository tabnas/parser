use std::sync::{Arc, Mutex};

use tabnas::grammar::{validate_grammar, BUILTIN_SCHEMA_VERSION};
use tabnas::{GrammarSpec, Tabnas};

#[test]
fn loads_ordered_serialized_grammar_and_start_rule() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "clear": true,
              "options": {"rule": {"start": "top"}},
              "meta": {"provenance": {"top$step1": "top"}},
              "rule": {
                "top": {"open": [{"s": ["#NR #ST"]}]},
                "unused": {"open": [{"s": "#TX"}]}
              }
            }"##,
        )
        .unwrap();

    assert!(parser.parse("42").is_ok());
    assert!(parser.parse(r#""text""#).is_ok());
    assert!(parser.parse("bare").is_err());
    assert_eq!(parser.rules.keys().collect::<Vec<_>>(), ["top", "unused"]);
}

#[test]
fn grammar_document_is_preserved_and_versions_are_gated() {
    let source = r#"{"v":1,"clear":true,"meta":{"source":"bnf"}}"#;
    let grammar = GrammarSpec::from_json(source).unwrap();
    assert!(grammar.clear);
    assert_eq!(grammar.version, Some(1));
    assert_eq!(grammar.meta.as_ref().unwrap()["source"], "bnf");

    for bad in [r#"{"v":"3"}"#, r#"{"v":2.5}"#, r#"{"v":0}"#] {
        assert!(GrammarSpec::from_json(bad).is_err(), "accepted {bad}");
    }
    let future = format!(r#"{{"v":{}}}"#, BUILTIN_SCHEMA_VERSION + 1);
    assert!(GrammarSpec::from_json(&future).is_err());
}

#[test]
fn removes_rules_and_supports_injection_modes() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"}},"rule":{
              "top":{"open":[{"s":"#NR"}]},
              "other":{"open":[{"s":"#ST"}]}
            }}"##,
        )
        .unwrap();
    parser
        .grammar_json(
            r##"{"rule":{
              "other":null,
              "top":{"open":{"alts":[{"s":"#ST"}],"inject":{"clear":true}}}
            }}"##,
        )
        .unwrap();

    assert!(!parser.rules.contains_key("other"));
    assert!(parser.parse(r#""yes""#).is_ok());
    assert!(parser.parse("42").is_err());
}

#[test]
fn serialized_action_arrays_run_in_order() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    for name in ["@first", "@second"] {
        let calls = calls.clone();
        parser.action(name, move |_| calls.lock().unwrap().push(name));
    }
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"}},"rule":{
              "top":{"open":[{"s":"#NR","a":["@first","@second"]}]}
            }}"##,
        )
        .unwrap();
    parser.parse("1").unwrap();
    assert_eq!(*calls.lock().unwrap(), ["@first", "@second"]);
}

#[test]
fn validation_reports_dangling_static_rule_references() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"clear":true,"rule":{"top":{"open":[{"p":"missing"}]}}}"#)
        .unwrap();
    assert_eq!(
        validate_grammar(&parser.rules),
        ["top.open alt[0]: unknown rule: missing"]
    );
}

#[test]
fn unsupported_dynamic_fields_fail_during_installation() {
    let mut parser = Tabnas::new();
    let error = parser
        .grammar_json(r#"{"rule":{"top":{"open":[{"c":"@condition"}]}}}"#)
        .err()
        .expect("condition must be rejected");
    assert!(error.to_string().contains("not supported"));
}

#[test]
fn injection_delete_and_move_match_cross_runtime_indexing() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"clear":true,"rule":{"top":{"open":[
              {"s":"#NR"},{"s":"#ST"},{"s":"#TX"}
            ]}}}"##,
        )
        .unwrap();
    parser
        .grammar_json(
            r#"{"rule":{"top":{"open":{"alts":[],"inject":{"delete":[-1],"move":[1,0]}}}}}"#,
        )
        .unwrap();

    let alts = &parser.rules["top"].open;
    assert_eq!(alts.len(), 2);
    assert_eq!(alts[0].s[0][0], tabnas::TIN_ST);
    assert_eq!(alts[1].s[0][0], tabnas::TIN_NR);
}
