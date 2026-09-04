use std::sync::{Arc, Mutex};

use tabnas::{Tabnas, Value};

#[test]
fn serialized_function_refs_cover_condition_modifier_error_and_dynamic_routing() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    let log = calls.clone();
    parser.alt_condition("@condition", move |rule, _context| {
        log.lock().unwrap().push("condition");
        rule.o.len() == 2
    });

    let log = calls.clone();
    parser.alt_modifier("@modifier", move |mut alt, _rule, _context| {
        log.lock().unwrap().push("modifier");
        alt.a = vec!["@modified-action".into()];
        alt
    });

    let log = calls.clone();
    parser.alt_error("@error", move |_rule, _context| {
        log.lock().unwrap().push("error");
        None
    });

    let log = calls.clone();
    parser.alt_backtrack("@backtrack", move |_rule, _context| {
        log.lock().unwrap().push("backtrack");
        1
    });

    let log = calls.clone();
    parser.alt_push("@push", move |_rule, _context| {
        log.lock().unwrap().push("push");
        Some("child".into())
    });

    for (name, event) in [
        ("@original-action", "original-action"),
        ("@modified-action", "modified-action"),
        ("@child-action", "child-action"),
    ] {
        let log = calls.clone();
        parser.action(name, move |_rule| log.lock().unwrap().push(event));
    }

    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#TA":"a","#TB":"b"}}
              },
              "rule":{
                "top":{"open":[{
                  "s":"#TA #TB",
                  "c":"@condition",
                  "h":"@modifier",
                  "e":"@error",
                  "b":"@backtrack",
                  "p":"@push",
                  "a":"@original-action"
                }]},
                "child":{"open":[{"s":"#TB","a":"@child-action"}]}
              }
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("ab").unwrap(), Value::Null);
    assert_eq!(
        *calls.lock().unwrap(),
        [
            "condition",
            "modifier",
            "error",
            "backtrack",
            "push",
            "modified-action",
            "child-action",
        ]
    );
}

#[test]
fn dynamic_replace_is_resolved_before_the_matching_action() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    let log = calls.clone();
    parser.action("@choose", move |_rule| {
        log.lock().unwrap().push("action");
    });
    let log = calls.clone();
    parser.alt_replace("@replace", move |_rule, _context| {
        log.lock().unwrap().push("replace");
        Some("tail".into())
    });
    let log = calls.clone();
    parser.action("@tail", move |_rule| log.lock().unwrap().push("tail"));

    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{
                "top":{"open":[{"s":"#NR","a":"@choose","r":"@replace"}]},
                "tail":{"open":[{"s":"#ZZ","a":"@tail"}]}
              }
            }"##,
        )
        .unwrap();

    parser.parse("1").unwrap();
    assert_eq!(*calls.lock().unwrap(), ["replace", "action", "tail"]);
}

#[test]
fn alternate_error_uses_raise_site_and_skips_mutation() {
    let mut parser = Tabnas::new();
    parser.alt_error("@boom", |rule, _context| {
        let mut token = rule.o0()?.clone();
        token.bad("boom_code");
        Some(token)
    });
    parser.action("@mutate", |rule| {
        *rule.node.borrow_mut() = Value::String("after-error".into());
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR","e":"@boom","a":"@mutate"}]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("42").unwrap_err();
    assert_eq!(error.code, "boom_code");
    assert_eq!(error.pos, 0);
    assert_eq!(error.rule, "top");
    assert_eq!(error.rule_stack, ["top"]);
    assert_eq!(error.expected, ["#NR"]);
}

#[test]
fn missing_typed_function_refs_fail_transactionally() {
    let documents = [
        r#"{"rule":{"top":{"open":[{"b":"@missing"}]}}}"#,
        r#"{"rule":{"top":{"open":[{"p":"@missing"}]}}}"#,
        r#"{"rule":{"top":{"open":[{"r":"@missing"}]}}}"#,
        r#"{"rule":{"top":{"open":[{"c":"@missing"}]}}}"#,
        r#"{"rule":{"top":{"open":[{"h":"@missing"}]}}}"#,
        r#"{"rule":{"top":{"open":[{"e":"@missing"}]}}}"#,
    ];

    for document in documents {
        let mut parser = Tabnas::new();
        parser
            .grammar_json(r#"{"options":{"tag":"before"},"rule":{"kept":{"open":[]}}}"#)
            .unwrap();
        assert!(
            parser.grammar_json(document).is_err(),
            "accepted {document}"
        );
        assert_eq!(parser.options.tag, "before");
        assert!(parser.rules.contains_key("kept"));
        assert!(!parser.rules.contains_key("top"));
    }
}

#[test]
fn continuations_do_not_guess_through_dynamic_backtracking() {
    let mut parser = Tabnas::new();
    parser.alt_backtrack("@backtrack", |_rule, _context| 1);
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#TA":"a"}}
              },
              "rule":{
                "top":{"open":[{"s":"#TA","b":"@backtrack","p":"child"}]},
                "child":{"open":[{"s":"#TA"}]}
              }
            }"##,
        )
        .unwrap();

    assert_eq!(parser.continuations("a").tokens, ["#ZZ"]);
    assert!(parser.parse("aa").is_err());
}
