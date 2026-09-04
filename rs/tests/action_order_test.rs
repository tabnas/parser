// Copyright (c) 2013-2026 Richard Rodger, MIT License

use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, Options, RuleSpec, Tabnas, TIN_NR};

fn record(events: &Arc<Mutex<Vec<&'static str>>>, event: &'static str) {
    events.lock().unwrap().push(event);
}

#[test]
fn named_and_native_actions_keep_exact_prepend_append_order() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    tabnas.action_with_context("@named-bo", {
        let events = events.clone();
        move |_, _| {
            record(&events, "named-bo");
            Ok(())
        }
    });
    tabnas.action_with_context("@named-alt", {
        let events = events.clone();
        move |_, _| {
            record(&events, "named-alt");
            Ok(())
        }
    });

    let mut rule = RuleSpec::new("val");
    rule.bo.push("@named-bo".into());
    rule.prepend_bo({
        let events = events.clone();
        move |_, _| record(&events, "pre-bo")
    });
    rule.add_bo({
        let events = events.clone();
        move |_, _| record(&events, "post-bo")
    });

    let mut alt = AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    };
    alt.a.push("@named-alt".into());
    alt.prepend_action({
        let events = events.clone();
        move |_, _| record(&events, "pre-alt")
    });
    alt.add_action({
        let events = events.clone();
        move |_, _| record(&events, "post-alt")
    });
    rule.open.push(alt);
    tabnas.rule(rule);

    tabnas.parse("1").unwrap();
    assert_eq!(
        [
            "pre-bo",
            "named-bo",
            "post-bo",
            "pre-alt",
            "named-alt",
            "post-alt"
        ],
        events.lock().unwrap().as_slice()
    );
}

#[test]
fn merge_preserves_each_sources_mixed_lifecycle_sequence() {
    fn grammar(tag: &'static str, events: Arc<Mutex<Vec<&'static str>>>) -> Tabnas {
        let options = Options {
            tag: tag.into(),
            ..Default::default()
        };
        let mut tabnas = Tabnas::with_options(options);
        let named = format!("@{tag}-named");
        tabnas.action_with_context(named.clone(), {
            let events = events.clone();
            move |_, _| {
                record(&events, if tag == "A" { "A-name" } else { "B-name" });
                Ok(())
            }
        });
        let mut rule = RuleSpec::new("val");
        rule.bo.push(named);
        rule.prepend_bo({
            let events = events.clone();
            move |_, _| record(&events, if tag == "A" { "A-pre" } else { "B-pre" })
        });
        rule.add_bo({
            let events = events.clone();
            move |_, _| record(&events, if tag == "A" { "A-post" } else { "B-post" })
        });
        rule.open.push(AltSpec {
            s: vec![vec![TIN_NR]],
            ..Default::default()
        });
        tabnas.rule(rule);
        tabnas
    }

    let events = Arc::new(Mutex::new(Vec::new()));
    let left = grammar("A", events.clone());
    let right = grammar("B", events.clone());
    right.merge(&left).unwrap().parse("1").unwrap();
    assert_eq!(
        ["A-pre", "A-name", "A-post", "B-pre", "B-name", "B-post"],
        events.lock().unwrap().as_slice()
    );
}
