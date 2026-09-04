use indexmap::IndexMap;
use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, Plugin, Rule, RuleSpec, Tabnas, Value, TIN_NR, TIN_ZZ};

fn object(entries: impl IntoIterator<Item = (&'static str, Value)>) -> Value {
    Value::Object(
        entries
            .into_iter()
            .map(|(key, value)| (key.to_string(), value))
            .collect::<IndexMap<_, _>>(),
    )
}

#[test]
fn plugin_defaults_options_and_application_order_match_the_mature_engines() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let plugin_seen = seen.clone();
    let plugin = Plugin::new("Demo", move |_parser, options| {
        plugin_seen.lock().unwrap().push(options.clone());
        Ok(())
    })
    .with_defaults(object([
        ("sep", Value::String(",".into())),
        ("trim", Value::Bool(true)),
    ]));

    let mut parser = Tabnas::new();
    parser
        .use_plugin(plugin.clone(), Some(object([("trim", Value::Bool(false))])))
        .unwrap();
    parser
        .use_plugin(
            Plugin::new("Second", {
                let seen = seen.clone();
                move |_parser, _options| {
                    seen.lock().unwrap().push(Value::String("second".into()));
                    Ok(())
                }
            }),
            None,
        )
        .unwrap();

    assert_eq!(parser.plugins.len(), 2);
    assert_eq!(
        parser.plugin_options("DEMO"),
        Some(&object([
            ("sep", Value::String(",".into())),
            ("trim", Value::Bool(false)),
        ]))
    );
    assert_eq!(seen.lock().unwrap().len(), 2);
}

#[test]
fn serialized_plugin_options_deep_merge_and_survive_derivation() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r#"{"options":{"plugin":{"Demo":{"format":{"indent":2},"keep":true}}}}"#)
        .unwrap();
    parser
        .grammar_json(r#"{"options":{"plugin":{"demo":{"format":{"tabs":false}}}}}"#)
        .unwrap();

    let expected = object([
        (
            "format",
            object([("indent", Value::Number(2.0)), ("tabs", Value::Bool(false))]),
        ),
        ("keep", Value::Bool(true)),
    ]);
    assert_eq!(parser.plugin_options("DEMO"), Some(&expected));
    assert_eq!(parser.options.plugin["demo"], expected);

    parser.set_plugin_options("demo", object([("extra", Value::String("x".into()))]));
    let child = parser.derive(|_| {}).unwrap();
    assert_eq!(
        child.plugin_options("demo").and_then(|value| match value {
            Value::Object(map) => map.get("extra"),
            _ => None,
        }),
        Some(&Value::String("x".into()))
    );
}

#[test]
fn derived_instances_rerun_plugins_against_child_options() {
    let runs = Arc::new(Mutex::new(Vec::new()));
    let plugin_runs = runs.clone();
    let plugin = Plugin::new("conditional", move |parser, _options| {
        plugin_runs.lock().unwrap().push(parser.options.tag.clone());
        parser.options.rule.start = "top".into();
        parser.define_rule("top", |rule| {
            rule.clear();
            let mut open = AltSpec {
                s: vec![vec![TIN_NR]],
                ..Default::default()
            };
            open.add_action(|rule, _context| {
                *rule.node.borrow_mut() = Value::String("plugin".into());
            });
            rule.add_open(open).add_close(AltSpec {
                s: vec![vec![TIN_ZZ]],
                ..Default::default()
            });
        });
        Ok(())
    });

    let mut parent = Tabnas::new();
    parent.options.tag = "parent".into();
    parent.use_plugin(plugin, None).unwrap();
    let child = parent
        .derive(|options| options.tag = "child".into())
        .unwrap();

    assert_eq!(parent.parse("1").unwrap(), Value::String("plugin".into()));
    assert_eq!(child.parse("1").unwrap(), Value::String("plugin".into()));
    assert_eq!(*runs.lock().unwrap(), ["parent", "child"]);
    assert_eq!(child.plugins.len(), 1);
}

#[test]
fn direct_alternate_and_lifecycle_actions_are_first_class() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    parser.options.rule.start = "top".into();
    parser.define_rule("top", |rule| {
        rule.clear();
        let log = events.clone();
        rule.add_bo(move |_rule, _context| log.lock().unwrap().push("bo"));
        let log = events.clone();
        rule.add_ao(move |_rule, _context| log.lock().unwrap().push("ao"));
        let log = events.clone();
        rule.add_bc(move |_rule, _context| log.lock().unwrap().push("bc"));
        let log = events.clone();
        rule.add_ac(move |_rule, _context| log.lock().unwrap().push("ac"));

        let mut open = AltSpec {
            s: vec![vec![TIN_NR]],
            ..Default::default()
        };
        let log = events.clone();
        open.add_action(move |rule, _context| {
            log.lock().unwrap().push("alt");
            *rule.node.borrow_mut() = Value::Number(7.0);
        });
        rule.add_open(open).add_close(AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        });
    });

    assert_eq!(parser.parse("1").unwrap(), Value::Number(7.0));
    assert_eq!(*events.lock().unwrap(), ["bo", "alt", "ao", "bc", "ac"]);
}

#[test]
fn rule_helpers_cover_second_tokens_counters_and_mutation() {
    let mut rule = Rule::new("top", Value::Undefined);
    assert!(rule.eq("missing", 0));
    assert!(rule.lt("missing", 1));
    assert!(!rule.gt("missing", 0));
    assert!(rule.lte("missing", 0));
    assert!(rule.gte("missing", 0));
    assert!(!rule.exist("missing"));
    rule.n.insert("count".into(), 2);
    assert!(rule.eq("count", 2));
    assert!(rule.gt("count", 1));
    assert!(rule.exist("count"));

    let mut spec = RuleSpec::new("top");
    spec.add_open(AltSpec::new()).prepend_open(AltSpec::new());
    spec.add_close(AltSpec::new()).prepend_close(AltSpec::new());
    assert_eq!(spec.open.len(), 2);
    assert_eq!(spec.close.len(), 2);
    spec.clear_open().clear_close();
    assert!(spec.open.is_empty());
    assert!(spec.close.is_empty());
}

#[test]
fn complete_map_list_and_safe_options_load_with_typed_merge_callbacks() {
    let mut parser = Tabnas::new();
    parser.map_merge_ref("@sum", |previous, current, _rule, _context| {
        match (previous, current) {
            (Value::Number(left), Value::Number(right)) => Value::Number(left + right),
            (_, current) => current,
        }
    });
    parser
        .grammar_json(
            r##"{
              "options":{
                "safe":{"key":false},
                "map":{"extend":false,"child":true,"ordered":true,"merge":"@sum"},
                "list":{"property":false,"pair":true,"child":true},
                "rule":{"start":"top"}
              }
            }"##,
        )
        .unwrap();

    assert!(!parser.options.safe.key);
    assert!(!parser.options.map.extend);
    assert!(parser.options.map.child);
    assert!(parser.options.map.ordered);
    assert!(!parser.options.list.property);
    assert!(parser.options.list.pair);
    assert!(parser.options.list.child);

    let merge = parser.options.map.merge.clone().unwrap();
    parser.define_rule("top", |rule| {
        let mut open = AltSpec {
            s: vec![vec![TIN_NR]],
            ..Default::default()
        };
        open.add_action(move |rule, context| {
            let value = merge(Value::Number(2.0), Value::Number(3.0), rule, context);
            *rule.node.borrow_mut() = value;
        });
        rule.add_open(open).add_close(AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        });
    });
    assert_eq!(parser.parse("1").unwrap(), Value::Number(5.0));

    let mut missing = Tabnas::new();
    assert!(missing
        .grammar_json(r#"{"options":{"map":{"merge":"@missing"}}}"#)
        .is_err());
}

#[test]
fn parse_context_exposes_source_meta_options_and_mutable_plugin_state() {
    let observed = Arc::new(Mutex::new(None));
    let mut parser = Tabnas::new();
    parser.options.tag = "context-test".into();
    parser.options.rule.start = "top".into();
    let capture = observed.clone();
    parser.parse_prepare(move |context| {
        context.u.insert("prepared".into(), Value::Bool(true));
        *capture.lock().unwrap() = Some((
            context.source.clone(),
            context.meta.clone(),
            context.options.tag.clone(),
            context.errs.len(),
        ));
    });
    parser.define_rule("top", |rule| {
        let mut open = AltSpec {
            s: vec![vec![TIN_NR]],
            ..Default::default()
        };
        open.add_action(|rule, context| {
            assert_eq!(context.v1().map(|token| token.src.as_str()), Some("1"));
            assert_eq!(context.u.get("prepared"), Some(&Value::Bool(true)));
            *rule.node.borrow_mut() = context.meta.clone();
        });
        rule.add_open(open).add_close(AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        });
    });

    let meta = object([("request", Value::String("abc".into()))]);
    assert_eq!(parser.parse_with_meta("1", meta.clone()).unwrap(), meta);
    assert_eq!(
        *observed.lock().unwrap(),
        Some(("1".into(), meta, "context-test".into(), 0))
    );
}

#[test]
fn recovery_errors_are_visible_to_later_plugin_callbacks() {
    let visible = Arc::new(Mutex::new(false));
    let mut parser = Tabnas::make_json();
    parser.options.parse.recover.enabled = true;
    parser.options.parse.recover.suppress = 0;
    let seen = visible.clone();
    parser.subscribe_rules(move |_rule, context| {
        if !context.errs.is_empty() {
            *seen.lock().unwrap() = true;
        }
    });

    let recovered = parser.parse_recover(r#"{"a":true blah,"b":1}"#);
    assert!(!recovered.errors.is_empty());
    assert!(*visible.lock().unwrap());
}

#[test]
fn imperative_lexer_matchers_receive_live_lexer_rule_and_context() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::make_json();
    let value_tin = parser.options.token("#VL").unwrap();
    let seen = calls.clone();
    parser.imperative_lex_match_ref("@dollar", move |lexer, rule, context| {
        seen.lock()
            .unwrap()
            .push((rule.name.clone(), context.source.clone(), lexer.point().pos));
        if !lexer.remaining().starts_with("$$") {
            return None;
        }
        let point = lexer.point();
        assert!(lexer.advance_chars(2));
        Some(lexer.token(
            "#VL",
            value_tin,
            Value::String("DOLLAR".into()),
            "$$",
            point,
        ))
    });
    parser
        .grammar_json(
            r##"{
              "options":{"lex":{"match":{"dollar":{"order":1500000,"make":"@dollar"}}}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("$$").unwrap(), Value::String("DOLLAR".into()));
    assert_eq!(
        parser.parse(r#"{"a":$$}"#).unwrap(),
        Value::from_json(&serde_json::json!({"a": "DOLLAR"}))
    );
    let calls = calls.lock().unwrap();
    assert!(calls
        .iter()
        .any(|(rule, source, pos)| rule == "val" && source == "$$" && *pos == 0));
    assert!(calls.iter().any(|(_, source, _)| source == r#"{"a":$$}"#));
}

#[test]
fn alternate_conditions_can_reenter_the_live_lexer() {
    let mut parser = Tabnas::new();
    parser.alt_condition_with_lexer("@peek", |rule, context, lexer| {
        let mut token = lexer.next_raw_for_rule(rule, context).unwrap();
        while context.options.is_ignored(token.tin) {
            token = lexer.next_raw_for_rule(rule, context).unwrap();
        }
        context.u.insert("peeked".into(), token.val);
        token.name == "#TB"
    });
    parser.action_with_context("@use-peek", |rule, context| {
        *rule.node.borrow_mut() = context.u.get("peeked").cloned().unwrap_or(Value::Undefined);
        Ok(())
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#TA":"a","#TB":"b"}}
              },
              "rule":{"top":{
                "open":[{"s":"#TA","c":"@peek","a":"@use-peek"}],
                "close":[{"s":"#ZZ"}]
              }}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("a b").unwrap(), Value::String("b".into()));
}
