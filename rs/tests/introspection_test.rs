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
                snapshot.name.clone(),
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
