use std::sync::{Arc, Mutex};

use tabnas::grammar::{validate_grammar, BUILTIN_SCHEMA_VERSION};
use tabnas::{GrammarSpec, Tabnas, Value};

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
    assert!(error.to_string().contains("unknown condition function"));
}

#[test]
fn declarative_conditions_gate_alternates_and_counters() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    for name in ["@under", "@done"] {
        let calls = calls.clone();
        parser.action(name, move |_| calls.lock().unwrap().push(name));
    }
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"}},"rule":{
              "top":{"open":[
                {"s":"#NR","c":{"n.count":{"$lt":2}},"n":{"count":1},"r":"top","a":"@under"},
                {"s":"#NR","c":{"n.count":{"$gte":2}},"a":"@done"}
              ]}
            }}"##,
        )
        .unwrap();

    parser.parse("1 2 3").unwrap();
    assert_eq!(*calls.lock().unwrap(), ["@under", "@under", "@done"]);
}

#[test]
fn condition_validation_and_group_filters_fail_closed() {
    for (document, message) in [
        (
            r##"{"rule":{"top":{"open":[{"c":{"bad.x":{"$eq":1}}}]}}}"##,
            "unknown condition path",
        ),
        (
            r##"{"rule":{"top":{"open":[{"c":{"n.x":{"$wat":1}}}]}}}"##,
            "unknown condition operator",
        ),
        (
            r##"{"rule":{"top":{"open":[{"g":"Bad Tag"}]}}}"##,
            "invalid group tag",
        ),
    ] {
        let mut parser = Tabnas::new();
        let error = parser
            .grammar_json(document)
            .err()
            .expect("invalid grammar");
        assert!(error.to_string().contains(message), "{error}");
    }

    let seen = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    for name in ["@drop", "@keep"] {
        let seen = seen.clone();
        parser.action(name, move |_| seen.lock().unwrap().push(name));
    }
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top","include":"keep"}},"rule":{
              "top":{"open":[
                {"s":"#NR","g":"drop","a":"@drop"},
                {"s":"#NR","g":"keep","a":"@keep"}
              ]}
            }}"##,
        )
        .unwrap();
    parser.parse("1").unwrap();
    assert_eq!(*seen.lock().unwrap(), ["@keep"]);
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

#[test]
fn serialized_regex_tokens_lex_and_parse() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"clear":true,"options":{
              "rule":{"start":"top"},
              "match":{"token":{"#WS":"@/^\\s+/"}}
            },"rule":{"top":{
              "open":[{"s":["#WS"],"a":"@value$"}],"close":[{}]
            }}}"##,
        )
        .unwrap();

    assert_eq!(parser.parse(" ").unwrap(), Value::String(" ".into()));
    assert_eq!(
        parser.parse("\u{00a0}").unwrap(),
        Value::String("\u{00a0}".into())
    );
    assert_eq!(
        parser.parse("\u{3000}").unwrap(),
        Value::String("\u{3000}".into())
    );
    assert_eq!(parser.rules["top"].open[0].s[0].len(), 1);
}

#[test]
fn unsupported_javascript_regex_constructs_fail_at_installation() {
    for expression in ["@/^(?=x)x/", "@/^(a)\\1/"] {
        let mut parser = Tabnas::new();
        let source = format!(r##"{{"options":{{"match":{{"token":{{"#X":"{expression}"}}}}}}}}"##);
        assert!(
            parser.grammar_json(&source).is_err(),
            "accepted {expression}"
        );
    }
}

#[test]
fn explicitly_empty_string_chars_are_not_treated_as_unset() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"string":{"chars":""}}}"#)
        .unwrap();
    let mut lexer = tabnas::lexer::Lexer::new(r#""ab""#, parser.options);
    let token = lexer.next_raw_token().unwrap();
    assert_eq!(token.name, "#TX");
    assert_eq!(token.src, r#""ab""#);
}

#[test]
fn serialized_fixed_tokens_rebind_remove_and_clear() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"options":{"fixed":{"token":{"#CA":";","#AT":"@"}},"match":{"token":{"#HI":"@/^hi/"}}}}"##,
        )
        .unwrap();

    let mut lexer = tabnas::lexer::Lexer::new(";@,", parser.options.clone());
    assert_eq!(lexer.next_raw_token().unwrap().name, "#CA");
    assert_eq!(lexer.next_raw_token().unwrap().name, "#AT");
    assert_eq!(lexer.next_raw_token().unwrap().name, "#TX");

    parser
        .grammar_json(r##"{"options":{"fixed":{"token":{"#AT":null}}}}"##)
        .unwrap();
    assert!(!parser.options.fixed.tokens.contains_key("#AT"));

    parser.grammar_json(r#"{"clear":true}"#).unwrap();
    assert!(parser.options.fixed.tokens.is_empty());
    assert!(parser.options.match_tokens.contains_key("#HI"));
    assert!(parser.rules.is_empty());
}

#[test]
fn shared_eager_literal_grammar_fixture_executes_in_rust() {
    let source = include_str!("../../ts/test/eager-literal.fixture.json");
    for (input, accepted) in [
        ("hi", true),
        ("HI", true),
        ("Hi", true),
        ("ho", false),
        ("h", false),
    ] {
        let mut parser = Tabnas::new();
        parser.grammar_json(source).unwrap();
        assert_eq!(parser.parse(input).is_ok(), accepted, "input {input:?}");
    }
}

#[test]
fn shared_probe_grammar_fixture_executes_in_rust() {
    let source = include_str!("../../ts/test/probe-grammar.fixture.json");
    for (input, accepted) in [
        ("abc", true),
        ("ab@cd", true),
        ("a@b", true),
        ("@", false),
        ("ab@", false),
    ] {
        let mut parser = Tabnas::new();
        parser.grammar_json(source).unwrap();
        assert_eq!(parser.parse(input).is_ok(), accepted, "input {input:?}");
    }
}
