use std::sync::{Arc, Mutex};

use tabnas::{Tabnas, Value};

#[test]
fn canonical_callbacks_share_the_resolved_alt_match_in_canonical_order() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    let log = calls.clone();
    parser.alt_condition_with_match("@condition", move |_rule, _context, matched| {
        assert!(matched.p.is_none());
        assert!(matched.g.is_empty());
        matched.u.insert("condition".into(), Value::Bool(true));
        log.lock().unwrap().push("condition");
        true
    });
    let log = calls.clone();
    parser.alt_error_with_match("@error", move |_rule, _context, matched| {
        assert_eq!(matched.g, ["source"]);
        assert_eq!(matched.u.get("condition"), Some(&Value::Bool(true)));
        log.lock().unwrap().push("error");
        None
    });
    let log = calls.clone();
    parser.alt_push_with_match("@push", move |_rule, _context, matched| {
        assert!(matched.e.is_none());
        log.lock().unwrap().push("push");
        Some("child".into())
    });
    let log = calls.clone();
    parser.alt_backtrack_with_match("@backtrack", move |_rule, _context, matched| {
        assert_eq!(matched.p.as_deref(), Some("child"));
        log.lock().unwrap().push("backtrack");
        1
    });
    let log = calls.clone();
    parser.alt_modifier_with_match("@modifier", move |mut matched, rule, _context, next| {
        assert_eq!(matched.b, 1);
        assert!(matched.h.is_some());
        assert_eq!(
            next.map(|next| next.name.as_str()),
            Some(rule.name.as_str())
        );
        matched.u.insert("modifier".into(), Value::Bool(true));
        log.lock().unwrap().push("modifier");
        matched
    });
    let log = calls.clone();
    parser.action_with_match_ref("@action", move |rule, _context, matched| {
        assert_eq!(matched.p.as_deref(), Some("child"));
        assert_eq!(matched.b, 1);
        assert_eq!(matched.g, ["source"]);
        assert!(matched.h.is_some());
        assert_eq!(rule.u.get("condition"), Some(&Value::Bool(true)));
        assert_eq!(rule.u.get("modifier"), Some(&Value::Bool(true)));
        log.lock().unwrap().push("action");
        Ok(None)
    });

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
                  "s":"#TA #TB", "c":"@condition", "h":"@modifier",
                  "e":"@error", "b":"@backtrack", "p":"@push",
                  "a":"@action", "g":"source"
                }]},
                "child":{"open":[{"s":"#TB"}]}
              }
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("ab").unwrap(), Value::Null);
    assert_eq!(
        *calls.lock().unwrap(),
        [
            "condition",
            "error",
            "push",
            "backtrack",
            "modifier",
            "action"
        ]
    );
}

#[test]
fn lifecycle_callbacks_receive_next_and_chain_token_output() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    let log = calls.clone();
    parser.state_action_with_next_ref("@top-bo/prepend", move |rule, _context, next, out| {
        assert_eq!(
            next.map(|next| next.name.as_str()),
            Some(rule.name.as_str())
        );
        assert!(out.is_none());
        let token = tabnas::Token {
            val: Value::String("first".into()),
            ..tabnas::Token::default()
        };
        log.lock().unwrap().push("bo-first");
        Ok(Some(token))
    });
    let log = calls.clone();
    parser.state_action_with_next_ref("@top-bo", move |_rule, _context, next, out| {
        assert_eq!(next.map(|next| next.name.as_str()), Some("top"));
        assert_eq!(
            out.as_ref().map(|token| &token.val),
            Some(&Value::String("first".into()))
        );
        log.lock().unwrap().push("bo-second");
        Ok(None)
    });
    let log = calls.clone();
    parser.state_action_with_next_ref("@top-ao", move |_rule, _context, next, out| {
        assert_eq!(next.map(|next| next.name.as_str()), Some("tail"));
        assert!(out.is_none());
        log.lock().unwrap().push("ao");
        Ok(None)
    });

    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{
                "top":{"open":[{"s":"#NR","p":"tail"}]},
                "tail":{"open":[{"s":"#ZZ"}]}
              }
            }"##,
        )
        .unwrap();

    parser.parse("1").unwrap();
    assert_eq!(*calls.lock().unwrap(), ["bo-first", "bo-second", "ao"]);
}

#[test]
fn live_rule_spec_and_lifecycle_gates_match_the_canonical_plugin_surface() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    for (name, event) in [
        ("@top-bo", "bo"),
        ("@top-ao", "ao"),
        ("@top-bc", "bc"),
        ("@top-ac", "ac"),
    ] {
        let log = calls.clone();
        parser.state_action_with_next_ref(name, move |_rule, _context, _next, _out| {
            log.lock().unwrap().push(event);
            Ok(None)
        });
    }
    parser.alt_modifier_with_match("@disable", |matched, rule, _context, _next| {
        assert_eq!(rule.spec.name, "top");
        assert_eq!(rule.spec.open.len(), 1);
        assert_eq!(rule.spec.close.len(), 1);
        assert!(rule.bo);
        rule.ao = false;
        rule.bc = false;
        rule.ac = false;
        matched
    });

    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{
                "open":[{"s":"#NR","h":"@disable"}],
                "close":[{"s":"#ZZ"}]
              }}
            }"##,
        )
        .unwrap();

    parser.parse("1").unwrap();
    assert_eq!(*calls.lock().unwrap(), ["bo"]);
}

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
            "push",
            "backtrack",
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
fn callback_generated_unknown_routes_fail_before_lifecycle_after_actions() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    parser.alt_push("@missing", |_rule, _context| Some("ghost".into()));
    let log = calls.clone();
    parser.action("@matched", move |_rule| {
        log.lock().unwrap().push("matched");
    });
    let log = calls.clone();
    parser.state_action_with_next_ref("@top-ao", move |_rule, _context, _next, _out| {
        log.lock().unwrap().push("ao");
        Ok(None)
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{
                "s":"#NR", "a":"@matched", "p":"@missing"
              }]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("1").unwrap_err();
    assert_eq!(error.code, "unknown_rule");
    assert_eq!(error.rule, "top");
    assert!(error.detail.contains("ghost"), "{}", error.detail);
    assert_eq!(*calls.lock().unwrap(), ["matched"]);
}

#[test]
fn a_valid_push_ignores_an_unused_unknown_replace_route() {
    let mut parser = Tabnas::new();
    parser.action_with_match_ref("@routes", |_rule, _context, matched| {
        matched.p = Some("child".into());
        matched.r = Some("ghost".into());
        Ok(None)
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{
                "top":{"open":[{"s":"#NR","a":"@routes"}]},
                "child":{"open":[{"s":"#ZZ"}]}
              }
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("1").unwrap(), Value::Null);
}

#[test]
fn matched_action_can_redirect_the_live_alternate() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    let log = calls.clone();
    parser.action_with_match_ref("@redirect", move |_rule, _context, matched| {
        assert_eq!(matched.p.as_deref(), Some("wrong"));
        matched.p = Some("right".into());
        matched.g.push("redirected".into());
        log.lock().unwrap().push("redirect");
        Ok(None)
    });
    let log = calls.clone();
    parser.action("@right", move |_rule| log.lock().unwrap().push("right"));
    parser.action("@wrong", move |_rule| panic!("the original route ran"));

    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{
                "top":{"open":[{"s":"#NR","p":"wrong","a":"@redirect"}]},
                "wrong":{"open":[{"s":"#ZZ","a":"@wrong"}]},
                "right":{"open":[{"s":"#ZZ","a":"@right"}]}
              }
            }"##,
        )
        .unwrap();

    parser.parse("1").unwrap();
    assert_eq!(*calls.lock().unwrap(), ["redirect", "right"]);
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
