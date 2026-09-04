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
fn declarative_conditions_resolve_nested_values_tokens_and_rule_graph() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    parser.action("@seed-node", |rule| {
        *rule.node.borrow_mut() = Value::from_json(&serde_json::json!({"kind": "root"}));
    });
    for name in ["@opened", "@parent", "@child", "@prev"] {
        let calls = calls.clone();
        parser.action(name, move |_| calls.lock().unwrap().push(name));
    }
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"},"fixed":{"token":{"#TA":"a","#TB":"b"}}},
              "rule":{
                "top":{
                  "open":[{
                    "s":"#TA",
                    "c":{"o.0.src":"a","o0.src":"a","spec.name":"top","need":0},
                    "u":{"mode":{"kind":"strict"}},
                    "a":["@seed-node","@opened"],
                    "p":"child"
                  }],
                  "close":[{
                    "c":{
                      "u.mode.kind":"strict",
                      "node.kind":"root",
                      "child.name":"child",
                      "child.parent.name":"top",
                      "next.name":"child",
                      "next.node.kind":"root"
                    },
                    "a":"@child",
                    "r":"next"
                  }]
                },
                "child":{
                  "open":[{"s":"#TB","c":{"parent.name":"top"},"a":"@parent"}]
                },
                "next":{
                  "open":[{"s":"#ZZ","c":{"prev.name":"top"},"a":"@prev"}]
                }
              }
            }"##,
        )
        .unwrap();

    parser.parse("ab").unwrap();
    assert_eq!(
        *calls.lock().unwrap(),
        ["@opened", "@parent", "@child", "@prev"]
    );
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
fn serialized_match_lex_switch_disables_custom_tokens() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r##"{"options":{"match":{"lex":false,"token":{"#HI":"@/^hi/"}}}}"##)
        .unwrap();
    let mut lexer = tabnas::lexer::Lexer::new("hi", parser.options);
    assert_eq!(lexer.next_raw_token().unwrap().name, "#TX");
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
fn serialized_strict_escape_option_reaches_the_lexer() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"string":{"allowUnknown":false,"escapeStrict":true}}}"#)
        .unwrap();
    let mut lexer = tabnas::lexer::Lexer::new(r#""\x41""#, parser.options);
    assert_eq!(lexer.next_raw_token().unwrap_err().code, "unexpected");
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

#[test]
fn serialized_tree_builtins_bind_config_at_load() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"}},"rule":{
              "top":{
                "open":[{"a":"@node$","p":"leaf","k":{"node$":{"init":true,"rule":"top","kind":"user"}}}],
                "close":[{"a":"@capture$","k":{"capture$":{"rule":"top","kind":"user"}}}]
              },
              "leaf":{"open":[{"s":"#TX","a":"@node$","k":{"node$":{"init":true,"rule":"leaf","kind":"user","nterms":1}}}]}
            }}"##,
        )
        .unwrap();

    assert!(!parser.rules["top"].open[0].k.contains_key("node$"));
    assert!(parser.rules["top"].open[0]
        .action_configs
        .contains_key("@node$"));
    let expected = Value::from_json(&serde_json::json!({
        "rule": "top",
        "src": "abc",
        "kids": [{"rule": "leaf", "src": "abc", "kids": []}]
    }));
    assert!(parser.parse("abc").unwrap().deep_equal(&expected));
}

#[test]
fn serialized_bubble_and_fold_builtins_preserve_tree_shape() {
    let mut bubble = Tabnas::new();
    bubble
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"}},"rule":{
              "top":{"open":[{"p":"leaf"}],"close":[{"a":"@bubble$"}]},
              "leaf":{"open":[{"s":"#NR","a":"@value$"}]}
            }}"##,
        )
        .unwrap();
    assert_eq!(bubble.parse("7").unwrap(), Value::Number(7.0));

    let mut fold = Tabnas::new();
    fold.grammar_json(
        r##"{"clear":true,"options":{"rule":{"start":"top"},"fixed":{"token":{"#CA":","}}},"rule":{
          "top":{
            "open":[{"a":"@node$","p":"item","k":{"node$":{"init":true,"rule":"top","kind":"user"}}}],
            "close":[{}]
          },
          "item":{
            "open":[{"s":"#TX","a":"@node$","k":{"node$":{"init":true,"rule":"item","kind":"user","nterms":1}}}],
            "close":[
              {"s":"#CA","r":"item","a":"@fold$","k":{"fold$":{"cN":1}}},
              {"a":"@fold$"}
            ]
          }
        }}"##,
    ).unwrap();
    let expected = Value::from_json(&serde_json::json!({
        "rule": "top",
        "src": "a,b",
        "kids": [
            {"rule": "item", "src": "a", "kids": []},
            {"rule": "item", "src": "b", "kids": []}
        ]
    }));
    let actual = fold.parse("a,b").unwrap();
    assert!(actual.deep_equal(&expected), "actual: {actual}");
}

#[test]
fn builtin_config_is_scoped_to_its_declaring_alternate() {
    for parent_runs in [false, true] {
        let action = if parent_runs {
            ",\"a\":\"@value$\"".to_string()
        } else {
            String::new()
        };
        let source = format!(
            r##"{{"clear":true,"options":{{"rule":{{"start":"top"}}}},"rule":{{
              "top":{{
                "open":[{{"s":"#NR #NR","k":{{"value$":{{"from":1}}}},"p":"leaf"{action}}}],
                "close":[{{"a":"@value$"}}]
              }},
              "leaf":{{"open":[{{"s":"#NR #NR","a":"@value$"}}],"close":[{{}}]}}
            }}}}"##
        );
        let mut parser = Tabnas::new();
        parser.grammar_json(&source).unwrap();
        assert_eq!(
            parser.parse("1 2 3 4").unwrap(),
            Value::Number(3.0),
            "parent_runs={parent_runs}"
        );
    }
}

#[test]
fn serialized_empty_result_and_result_fail_options_are_applied() {
    let mut empty = Tabnas::new();
    empty
        .grammar_json(r#"{"options":{"lex":{"empty":true,"emptyResult":"none"}}}"#)
        .unwrap();
    assert_eq!(empty.parse("").unwrap(), Value::String("none".into()));

    let mut rejected = Tabnas::make_json();
    rejected
        .grammar_json(r#"{"options":{"result":{"fail":[1]}}}"#)
        .unwrap();
    assert_eq!(rejected.parse("1").unwrap_err().code, "unexpected");

    assert!(Tabnas::new()
        .grammar_json(r#"{"options":{"result":{"fail":1}}}"#)
        .is_err());
    assert!(Tabnas::new()
        .grammar_json(r#"{"options":{"parse":{"prepare":{"x":"@x"}}}}"#)
        .is_err());
}

#[test]
fn serialized_builtin_lexer_switches_reach_the_runtime() {
    for (option, source) in [
        (r#""number":{"lex":false}"#, "12"),
        (r#""string":{"lex":false}"#, r#""x""#),
        (r#""value":{"lex":false}"#, "true"),
        (r#""space":{"lex":false}"#, "a b"),
    ] {
        let grammar = format!(
            r##"{{"clear":true,"options":{{"rule":{{"start":"top"}},{option}}},"rule":{{"top":{{"open":[{{"s":"#TX"}}]}}}}}}"##
        );
        let mut parser = Tabnas::new();
        parser.grammar_json(&grammar).unwrap();
        assert!(parser.parse(source).is_ok(), "{option}: {source:?}");
    }

    let mut custom_space = Tabnas::new();
    custom_space
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"},"space":{"chars":"_"},"tokenSet":{"IGNORE":[]}},"rule":{"top":{"open":[{"s":"#SP"}]}}}"##,
        )
        .unwrap();
    assert!(custom_space.parse("_").is_ok());

    let mut custom_line = Tabnas::new();
    custom_line
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"},"line":{"chars":";","rowChars":";","single":true},"tokenSet":{"IGNORE":[]}},"rule":{"top":{"open":[{"s":"#LN"}]}}}"##,
        )
        .unwrap();
    assert_eq!(custom_line.options.line.chars, ";");
    assert_eq!(custom_line.options.line.row_chars, ";");
    assert!(custom_line.options.line.single);
    assert!(custom_line.parse(";").is_ok());
}

#[test]
fn serialized_string_behavior_options_reach_the_runtime() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"},"string":{"escapeChar":"~","escape":{"q":"Q","n":null},"replace":{"x":"yz"},"multiChars":"\"","allowUnknown":false,"abandon":false}},"rule":{"top":{"open":[{"s":"#ST","a":"@value$"}]}}}"##,
        )
        .unwrap();
    assert_eq!(
        parser.parse(r#""x~q""#).unwrap(),
        Value::String("yzQ".into())
    );
    assert_eq!(parser.parse(r#""~n""#).unwrap_err().code, "unexpected");
    assert_eq!(
        parser.parse("\"a\nb\"").unwrap(),
        Value::String("a\nb".into())
    );

    assert!(Tabnas::new()
        .grammar_json(r#"{"options":{"string":{"escapeChar":""}}}"#)
        .is_err());
    assert!(Tabnas::new()
        .grammar_json(r#"{"options":{"string":{"replace":{"xx":"x"}}}}"#)
        .is_err());
}

#[test]
fn serialized_comment_definitions_reach_the_runtime() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"},"comment":{"def":{"hash":null,"semi":{"line":true,"start":";","lex":true,"suffix":["!!","!"],"eatline":false}}},"tokenSet":{"IGNORE":[]}},"rule":{"top":{"open":[{"s":"#CM"}]}}}"##,
        )
        .unwrap();
    assert!(!parser.options.comment.definitions.contains_key("hash"));
    assert_eq!(
        parser.options.comment.definitions["semi"].suffixes,
        ["!!", "!"]
    );
    assert!(parser.parse("; note!!").is_ok());

    assert!(Tabnas::new()
        .grammar_json(r#"{"options":{"comment":{"def":{"x":{"suffix":[1]}}}}}"#)
        .is_err());
}

#[test]
fn serialized_value_definitions_and_enders_reach_the_runtime() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"clear":true,"options":{"rule":{"start":"top"},"value":{"def":{"kay":{"match":"@/^k$/i","val":"KAY"},"raw":{"match":"@/^r$/","val":null},"true":null}},"ender":["END"]},"rule":{"top":{"open":[{"s":"#VL","a":"@value$"}]}}}"##,
        )
        .unwrap();
    assert_eq!(parser.parse("k").unwrap(), Value::String("KAY".into()));
    assert_eq!(parser.parse("r").unwrap(), Value::String("r".into()));
    assert!(parser.parse("true").is_err());
    assert_eq!(parser.options.ender, ["END"]);

    assert!(Tabnas::new()
        .grammar_json(r#"{"options":{"value":{"def":{"x":{"match":"nope"}}}}}"#)
        .is_err());
    assert!(Tabnas::new()
        .grammar_json(r#"{"options":{"ender":[1]}}"#)
        .is_err());
}
