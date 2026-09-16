//! How a route name becomes a rule.
//!
//! Every transition resolves the name it routes to against the installed
//! rules: it has to say whether the rule exists, hand the new rule the
//! shared name handle and bind it to the spec. These pin the behaviour of
//! that resolution independently of how many lookups it takes -- a static
//! route that names nothing still fails at the canonical point, and a rule
//! whose own spec or name is rewritten under it is still resolved by the
//! name it carries.

use std::sync::{Arc, Mutex};

use tabnas::{RuleSpec, Tabnas};

#[test]
fn a_static_route_naming_no_installed_rule_fails_after_the_matched_action() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();

    let log = calls.clone();
    parser.action("@matched", move |_rule| {
        log.lock().unwrap().push("matched");
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#TA":"a"}}
              },
              "rule":{"top":{"open":[{"s":"#TA","a":"@matched","p":"ghost"}]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("a").unwrap_err();
    assert_eq!(error.code, "unknown_rule");
    assert_eq!(error.rule, "top");
    assert!(error.detail.contains("ghost"), "{}", error.detail);
    assert_eq!(
        *calls.lock().unwrap(),
        ["matched"],
        "the route is checked after the matched action, not before it"
    );
}

#[test]
fn a_rule_whose_spec_an_action_swapped_is_still_resolved_by_its_name() {
    fn run(swap: bool) -> (Vec<&'static str>, tabnas::Value) {
        let top_spec: Arc<Mutex<Option<Arc<RuleSpec>>>> = Arc::new(Mutex::new(None));
        let calls = Arc::new(Mutex::new(Vec::new()));
        let mut parser = Tabnas::new();

        let captured = top_spec.clone();
        let log = calls.clone();
        parser.action("@capture", move |rule| {
            assert_eq!(rule.name.as_str(), "top");
            *captured.lock().unwrap() = Some(rule.spec.clone());
            log.lock().unwrap().push("capture");
        });
        let captured = top_spec.clone();
        let log = calls.clone();
        parser.action("@swap", move |rule| {
            assert_eq!(rule.name.as_str(), "child");
            if swap {
                let other = captured
                    .lock()
                    .unwrap()
                    .clone()
                    .expect("the outer rule's spec was captured on the way in");
                assert_eq!(other.name, "top");
                // The rule now carries another installed rule's spec while
                // still calling itself `child`. Its close phase has to go
                // back to the name to find out what `child` is; were the
                // spec trusted, the close below would run `top`'s alternate.
                rule.spec = other;
                log.lock().unwrap().push("swap");
            }
        });
        let log = calls.clone();
        parser.action("@child-close", move |_rule| {
            log.lock().unwrap().push("child-close");
        });
        let log = calls.clone();
        parser.action("@top-close", move |_rule| {
            log.lock().unwrap().push("top-close");
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
                    "top":{
                      "open":[{"s":"#TA","a":"@capture","p":"child"}],
                      "close":[{"s":"#ZZ","a":"@top-close"}]
                    },
                    "child":{
                      "open":[{"s":"#TB","a":"@swap"}],
                      "close":[{"s":"#ZZ","a":"@child-close"}]
                    }
                  }
                }"##,
            )
            .unwrap();

        let value = parser.parse("ab").expect("the parse is unaffected");
        let calls = calls.lock().unwrap().clone();
        (calls, value)
    }

    let (swapped, swapped_value) = run(true);
    let (untouched, untouched_value) = run(false);
    assert_eq!(
        swapped,
        ["capture", "swap", "child-close", "top-close"],
        "the swapped rule still closed on `child`'s alternate"
    );
    assert_eq!(untouched, ["capture", "child-close", "top-close"]);
    assert_eq!(swapped_value, untouched_value);
}

#[test]
fn a_rule_renamed_to_an_uninstalled_name_fails_by_that_name() {
    let mut parser = Tabnas::new();

    parser.action("@rename", move |rule| {
        // The rule keeps `top`'s spec but answers to a name no rule claims.
        rule.name = "ghost".into();
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#TA":"a"}}
              },
              "rule":{"top":{"open":[{"s":"#TA","a":"@rename"}]}}
            }"##,
        )
        .unwrap();

    let error = parser.parse("a").unwrap_err();
    assert_eq!(
        error.code, "unknown_rule",
        "the next step resolves the rule by the name it carries, not by the spec it holds"
    );
}
