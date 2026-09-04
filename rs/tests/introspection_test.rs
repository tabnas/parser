// Copyright (c) 2013-2026 Richard Rodger, MIT License

use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, ContextSeed, Options, Plugin, RuleSpec, Tabnas, Value, TIN_NR};

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
