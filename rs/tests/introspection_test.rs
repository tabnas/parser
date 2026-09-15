// Copyright (c) 2013-2026 Richard Rodger, MIT License

use std::sync::{Arc, Mutex};
use tabnas::{
    AltSpec, ContextSeed, Options, Plugin, Point, Rule, RuleSpec, Tabnas, Token, Value, TIN_NR,
};

#[test]
fn instance_introspection_is_ordered_and_independent() {
    let mut tabnas = Tabnas::new();
    tabnas.rule(RuleSpec::new("first"));
    tabnas.rule(RuleSpec::new("second"));
    tabnas
        .use_plugin(Plugin::new("demo", |_, _| Ok(())), None)
        .unwrap();

    assert!(tabnas.id.starts_with("Tabnas/"));
    assert_eq!(tabnas.id, tabnas.to_string());
    assert_eq!(["first", "second"], tabnas.rule_names().as_slice());
    assert_eq!(2, tabnas.rule_specs().len());
    assert_eq!("demo", tabnas.installed_plugins()[0].name);
    assert_eq!(
        tabnas.options.token_set["VAL"],
        tabnas.token_set("#VAL").unwrap()
    );
    assert_eq!(tabnas.options.token("#OB"), tabnas.fixed("{"));

    let mut config = tabnas.config();
    config.rule.maxmul = 99;
    assert_ne!(99, tabnas.options.rule.maxmul);

    let description = tabnas.describe();
    assert!(description.contains("=== Tabnas Instance ==="));
    assert!(description.contains("first: open=0 close=0"));
    assert!(description.contains("--- Plugins: 1 ---"));
    assert!(description.contains("RuleStart: val"));
}

#[test]
fn debug_trace_uses_the_configured_formatter_and_reports_lex_and_rule_events() {
    let lines = Arc::new(Mutex::new(Vec::<String>::new()));
    let mut parser = Tabnas::make_json();
    parser.options.debug.maxlen = 3;
    let captured = lines.clone();
    parser.enable_trace_with(move |line| captured.lock().unwrap().push(line.into()));

    assert_eq!(Value::Number(1234.0), parser.parse("1234").unwrap());
    let lines = lines.lock().unwrap();
    assert!(lines.iter().any(|line| line.starts_with("[lex] #NR")));
    assert!(lines.iter().any(|line| line.starts_with("[rule] val")));
    assert!(lines.iter().any(|line| line.contains("val=123...")));
}

#[test]
fn debug_options_load_from_serialized_grammar() {
    let output = Arc::new(Mutex::new(Vec::<String>::new()));
    let mut options = Options::default();
    let captured = output.clone();
    options.debug.output = Some(Arc::new(move |line| {
        captured.lock().unwrap().push(line.into());
    }));
    let mut parser = Tabnas::with_options(options);
    parser
        .grammar_json(r#"{"options":{"debug":{"maxlen":7,"print":{"config":true}}}}"#)
        .unwrap();
    assert_eq!(7, parser.options.debug.maxlen);
    assert!(parser.options.debug.print.config);
    assert!(output
        .lock()
        .unwrap()
        .iter()
        .any(|line| line.contains("maxlen: 7")));

    parser.options.debug.maxlen = 6;
    assert_eq!(
        "\"a\\\"bc...",
        parser
            .options
            .debug
            .format_source(&Value::String("a\"bcdef".into()))
    );
    assert_eq!("", parser.options.debug.format_source(&Value::Null));
}

#[test]
fn core_runtime_values_have_stable_human_readable_forms() {
    let point = Point {
        len: 3,
        si: 1,
        pos: 1,
        ri: 2,
        ci: 4,
    };
    assert_eq!(point.to_string(), "Point[1/3,2,4]");
    let no_token = Token::no_token();
    assert_eq!(no_token.name, "");
    assert!(no_token.is_no_token());

    let mut token = Token::new("#NR", TIN_NR, Value::Number(1.0), "1", point);
    token.bad_with_details(
        "unexpected",
        [
            ("x".into(), Value::Number(1.0)),
            (
                "nested".into(),
                Value::Object([("a".into(), Value::Number(1.0))].into_iter().collect()),
            ),
        ],
    );
    token.bad_with_details(
        "unexpected",
        [(
            "nested".into(),
            Value::Object([("b".into(), Value::Number(2.0))].into_iter().collect()),
        )],
    );
    assert_eq!(
        token.use_data["nested"],
        Value::Object(
            [
                ("a".into(), Value::Number(1.0)),
                ("b".into(), Value::Number(2.0)),
            ]
            .into_iter()
            .collect()
        )
    );
    assert_eq!(
        token.to_string(),
        "Token[#NR=8 1=1 1,2,4 {nested:{a:1,b:2},x:1} unexpected]"
    );

    let mut rule = Rule::new("value", Value::Null);
    rule.i = 7;
    assert_eq!(rule.to_string(), "[Rule value~7]");
}

#[test]
fn decorations_are_named_and_inherited_by_value() {
    let mut parent = Tabnas::new();
    parent.decorate("answer", Value::Number(42.0));
    assert_eq!(parent.decoration("answer"), Some(&Value::Number(42.0)));
    assert!(parent.decoration::<Value>("missing").is_none());

    let mut child = parent.derive(|_| {}).unwrap();
    assert_eq!(child.decoration("answer"), Some(&Value::Number(42.0)));
    child.decorate("answer", Value::Number(7.0));
    assert_eq!(parent.decoration("answer"), Some(&Value::Number(42.0)));
}

#[test]
fn decorations_support_native_values_and_expose_parent_identity() {
    let mut parent = Tabnas::new();
    parent.decorate("labels", vec!["one".to_string(), "two".to_string()]);
    parent.decorate_opaque(
        "callable",
        Arc::new(|value: i32| value + 1) as Arc<dyn Fn(i32) -> i32 + Send + Sync>,
    );

    let mut child = parent.derive(|_| {}).unwrap();
    assert_eq!(child.parent_id.as_deref(), Some(parent.id.as_str()));
    assert_eq!(
        child.decoration::<Vec<String>>("labels").unwrap(),
        &["one".to_string(), "two".to_string()]
    );
    let callable = child
        .decoration::<Arc<dyn Fn(i32) -> i32 + Send + Sync>>("callable")
        .unwrap();
    assert_eq!(callable(4), 5);

    let seen_parent = Arc::new(Mutex::new(None));
    let capture = seen_parent.clone();
    child.parse_prepare(move |context| {
        *capture.lock().unwrap() = context.instance.parent_id.clone();
    });
    child.parse("1").unwrap();
    assert_eq!(
        seen_parent.lock().unwrap().as_deref(),
        Some(parent.id.as_str())
    );
}

#[test]
fn native_token_and_rule_definer_helpers_expose_the_complete_instance_view() {
    let mut tabnas = Tabnas::new();
    let custom = tabnas.token_with_source("#CUSTOM", "!");
    assert_eq!(tabnas.fixed("!"), Some(custom));
    assert_eq!(tabnas.fixed_source(custom), Some("!"));
    assert_eq!(tabnas.token_name(custom), "#CUSTOM");
    tabnas.set_token_set("#CUSTOMS", vec![custom]);
    assert_eq!(tabnas.token_set("CUSTOMS"), Some(vec![custom]));

    tabnas.define_rule_with_parser("top", |rule, parser| {
        assert_eq!(parser.options.token("#CUSTOM"), Some(custom));
        assert!(parser.rules.contains_key("top"));
        rule.open.push(AltSpec {
            s: vec![vec![custom]],
            ..Default::default()
        });
    });
    tabnas
        .set_options(|options| options.rule.start = "top".into())
        .unwrap();
    assert!(tabnas.parse("!").is_ok());
}

#[test]
fn metadata_aware_parser_start_receives_the_owning_instance() {
    let options = Options {
        tag: "owner".into(),
        ..Default::default()
    };
    let mut tabnas = Tabnas::with_options(options);
    tabnas.parser_start_with_instance_ref("@custom", |src, instance, meta| {
        let Value::Object(meta) = meta else {
            panic!("metadata was not passed through")
        };
        Ok(Value::String(format!(
            "{src}:{}:{}",
            instance.options.tag,
            meta["request"].to_json()
        )))
    });
    tabnas
        .grammar_json(r#"{"options":{"parser":{"start":"@custom"}}}"#)
        .unwrap();

    let meta = Value::Object(
        [("request".into(), Value::Number(7.0))]
            .into_iter()
            .collect(),
    );
    assert_eq!(
        Value::String("input:owner:7.0".into()),
        tabnas.parse_with_meta("input", meta).unwrap()
    );
}

#[test]
fn context_exposes_live_rule_instance_plugins_and_ancestor_stack() {
    let observed = Arc::new(Mutex::new(Vec::<String>::new()));
    let mut tabnas = Tabnas::new();
    tabnas
        .use_plugin(Plugin::new("observer", |_, _| Ok(())), None)
        .unwrap();

    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![],
        p: Some("child".into()),
        ..Default::default()
    });
    tabnas.rule(top);

    let mut child = RuleSpec::new("child");
    let mut alt = AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    };
    alt.add_action({
        let observed = observed.clone();
        move |rule, context| {
            let snapshot = context.rule.as_ref().expect("current rule snapshot");
            observed.lock().unwrap().extend([
                context.instance.id.clone(),
                context.instance.plugins.join(","),
                context.instance.rule_names.join(","),
                snapshot.name.to_string(),
                snapshot
                    .o
                    .first()
                    .map_or("none", |token| token.name.as_str())
                    .into(),
                context
                    .rule_stack
                    .iter()
                    .map(|rule| rule.name.as_str())
                    .collect::<Vec<_>>()
                    .join(","),
            ]);
            *rule.node.borrow_mut() = Value::String("seen".into());
        }
    });
    child.open.push(alt);
    tabnas.rule(child);
    tabnas.options.rule.start = "top".into();

    assert_eq!(Value::String("seen".into()), tabnas.parse("1").unwrap());
    let observed = observed.lock().unwrap();
    assert_eq!(tabnas.id, observed[0]);
    assert_eq!("observer", observed[1]);
    assert_eq!("top,child", observed[2]);
    assert_eq!("child", observed[3]);
    assert_eq!("#NR", observed[4]);
    assert_eq!("top", observed[5]);
}

#[test]
fn diagnostics_include_installed_plugin_names() {
    let mut tabnas = Tabnas::new();
    tabnas
        .use_plugin(Plugin::new("diagnostic", |_, _| Ok(())), None)
        .unwrap();
    let mut rule = RuleSpec::new("val");
    rule.open.push(AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    });
    tabnas.rule(rule);

    let error = tabnas.parse("x").unwrap_err();
    assert_eq!(["diagnostic"], error.plugins.as_slice());
    assert!(error.to_string().contains("plugins=diagnostic"));
}

#[test]
fn typed_parent_context_seeds_meta_and_plugin_state_only() {
    let observed = Arc::new(Mutex::new(None));
    let mut tabnas = Tabnas::new();
    let mut rule = RuleSpec::new("val");
    let mut alt = AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    };
    alt.add_action({
        let observed = observed.clone();
        move |_, context| {
            *observed.lock().unwrap() =
                Some((context.meta.clone(), context.u.clone(), context.errs.len()));
        }
    });
    rule.open.push(alt);
    tabnas.rule(rule);

    let meta = Value::Object(
        [(
            "request".into(),
            Value::Object([("a".into(), Value::Number(1.0))].into_iter().collect()),
        )]
        .into_iter()
        .collect(),
    );
    let seed = ContextSeed {
        meta: Some(Value::Object(
            [(
                "request".into(),
                Value::Object([("b".into(), Value::Number(2.0))].into_iter().collect()),
            )]
            .into_iter()
            .collect(),
        )),
        u: [("seeded".into(), Value::Bool(true))].into_iter().collect(),
    };
    tabnas.parse_with_context("1", meta, &seed).unwrap();

    let (meta, u, errors) = observed.lock().unwrap().clone().unwrap();
    let Value::Object(meta) = meta else {
        panic!("meta must remain an object")
    };
    let Value::Object(request) = &meta["request"] else {
        panic!("nested request metadata must remain an object")
    };
    assert_eq!(Value::Number(1.0), request["a"]);
    assert_eq!(Value::Number(2.0), request["b"]);
    assert_eq!(Some(&Value::Bool(true)), u.get("seeded"));
    assert_eq!(0, errors);
}

/// `context.rule_stack` has to describe the ancestors as they are now, at
/// every depth, on the way down and on the way back up. The engine keeps
/// that stack incrementally rather than re-snapshotting every ancestor on
/// every parse step, so this walks a grammar that nests once per token and
/// reads each frame's own `u` — the per-rule state a stale ancestor
/// snapshot would report from an earlier step.
#[test]
fn the_rule_stack_describes_every_ancestor_at_its_current_state() {
    let seen: Arc<Mutex<Vec<String>>> = Arc::new(Mutex::new(Vec::new()));

    let record = |seen: Arc<Mutex<Vec<String>>>, phase: &'static str| {
        move |_rule: &mut Rule, context: &mut tabnas::Context| {
            let frames = context
                .rule_stack
                .iter()
                .map(|frame| {
                    let mark = match frame.u.get("mark") {
                        Some(Value::Number(number)) => number.to_string(),
                        _ => "-".to_string(),
                    };
                    format!("{}:{mark}", frame.name)
                })
                .collect::<Vec<_>>()
                .join(" ");
            seen.lock().unwrap().push(format!("{phase} {frames}"));
        }
    };

    let mut tabnas = Tabnas::new();
    tabnas.options.rule.start = "top".into();
    tabnas.define_rule("top", |rule| {
        rule.add_open(AltSpec {
            p: Some("node".into()),
            ..Default::default()
        })
        .add_close(AltSpec {
            s: vec![vec![tabnas::TIN_ZZ]],
            ..Default::default()
        });
    });
    tabnas.define_rule("node", {
        let seen = seen.clone();
        move |rule| {
            let mut descend = AltSpec {
                s: vec![vec![TIN_NR]],
                p: Some("node".into()),
                ..Default::default()
            };
            // Each frame stamps its own token value, so a frame reported
            // with another frame's mark is a stale snapshot.
            descend.add_action(|rule, _context| {
                let mark = rule.o.first().map_or(Value::Undefined, |o| o.val.clone());
                rule.u_mut().insert("mark".into(), mark);
            });
            descend.add_action(record(seen.clone(), "open"));
            let mut close = AltSpec::default();
            close.add_action(record(seen.clone(), "close"));
            rule.add_open(descend)
                .add_open(AltSpec::default())
                .add_close(close);
        }
    });

    tabnas.parse("1 2 3").expect("nested parse");

    let seen = seen.lock().unwrap();
    assert_eq!(
        seen.iter()
            .filter(|entry| entry.starts_with("open"))
            .collect::<Vec<_>>(),
        [
            "open top:-",
            "open top:- node:1",
            "open top:- node:1 node:2"
        ],
    );
    // Unwinding reports the same frames, still carrying their own marks.
    assert_eq!(
        seen.iter()
            .filter(|entry| entry.starts_with("close"))
            .collect::<Vec<_>>(),
        [
            "close top:- node:1 node:2 node:3",
            "close top:- node:1 node:2",
            "close top:- node:1",
            "close top:-",
        ],
    );
}

/// A grammar may legitimately compute a NaN and stash it on a rule. The
/// engine's own bookkeeping must not treat that as the rule having changed
/// underneath it: `Value` derives `PartialEq`, and NaN is not equal to
/// itself, so a check written with `==` reports a frame as drifted when it
/// is byte-identical.
#[test]
fn a_not_a_number_on_an_ancestor_does_not_look_like_drift() {
    let mut tabnas = Tabnas::new();
    tabnas.options.rule.start = "top".into();
    tabnas.define_rule("top", |rule| {
        rule.add_open(AltSpec {
            p: Some("node".into()),
            ..Default::default()
        })
        .add_close(AltSpec {
            s: vec![vec![tabnas::TIN_ZZ]],
            ..Default::default()
        });
    });
    tabnas.define_rule("node", |rule| {
        let mut descend = AltSpec {
            s: vec![vec![TIN_NR]],
            p: Some("node".into()),
            ..Default::default()
        };
        descend.add_action(|rule, _context| {
            rule.u_mut().insert("mark".into(), Value::Number(f64::NAN));
        });
        rule.add_open(descend)
            .add_open(AltSpec::default())
            .add_close(AltSpec::default());
    });

    assert!(tabnas.parse("1 2 3").is_ok());
}

/// `context.rule_stack` is a public field, so a callback can write to it, as
/// it can to `ctx.rs` in the TypeScript engine and `ctx.RS` in the Go one.
/// None of the three sanitizes what a callback leaves there. What all three
/// do guarantee is that the engine's own parse is unaffected, and a debug
/// build must not panic over it either: the assertion that the stack tracks
/// the parse loop is about the engine's bookkeeping, and a callback cannot
/// reach that.
#[test]
fn a_callback_writing_to_the_published_rule_stack_does_not_derail_the_parse() {
    let seen: Arc<Mutex<Vec<String>>> = Arc::new(Mutex::new(Vec::new()));

    let mut tabnas = Tabnas::new();
    tabnas.options.rule.start = "top".into();
    tabnas.define_rule("top", |rule| {
        rule.add_open(AltSpec {
            p: Some("node".into()),
            ..Default::default()
        })
        .add_close(AltSpec {
            s: vec![vec![tabnas::TIN_ZZ]],
            ..Default::default()
        });
    });
    tabnas.define_rule("node", {
        let seen = seen.clone();
        move |rule| {
            let mut descend = AltSpec {
                s: vec![vec![TIN_NR]],
                p: Some("node".into()),
                ..Default::default()
            };
            let log = seen.clone();
            descend.add_action(move |rule, context| {
                let mark = rule.o.first().map_or(Value::Undefined, |o| o.val.clone());
                rule.u_mut().insert("mark".into(), mark);
                log.lock().unwrap().push(
                    context
                        .rule_stack
                        .iter()
                        .map(|frame| match frame.u.get("mark") {
                            Some(Value::Number(number)) => number.to_string(),
                            _ => frame.name.to_string(),
                        })
                        .collect::<Vec<_>>()
                        .join(" "),
                );
                // Same length, different contents: the reverse leaves the
                // engine no size change to notice.
                context.rule_stack.reverse();
            });
            rule.add_open(descend)
                .add_open(AltSpec::default())
                .add_close(AltSpec::default());
        }
    });

    // The parse result is the engine's own bookkeeping: three terms, each
    // rule opened and closed in order, whatever the callback did to the
    // published view on its way past.
    assert!(tabnas.parse("1 2 3").is_ok());
    assert_eq!(seen.lock().unwrap().len(), 3);
    // The first callback runs before anything has been written, so it sees
    // the real ancestry. The later ones inherit what their predecessor left,
    // which is the same thing `ctx.rs` gives a TypeScript callback.
    assert_eq!(seen.lock().unwrap()[0], "top");
}
