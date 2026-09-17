// Copyright (c) 2013-2026 Richard Rodger, MIT License

use std::sync::{Arc, Mutex};
use tabnas::{AltActionBinding, AltSpec, Options, RuleSpec, Tabnas, TIN_NR};

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

#[test]
fn an_action_appended_by_a_matched_action_does_not_run_in_the_same_pass() {
    // The list an alternate's actions are taken from is fixed when the
    // sequence starts; appending to `matched.actions` from inside it has
    // never reached the loop that is already running. Pinned here because
    // the loop now walks the installed order directly for an alternate
    // nothing can observe the record through, and this is the boundary.
    let events = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    tabnas.action_with_match_ref("@append", {
        let events = events.clone();
        move |_rule, _context, matched| {
            record(&events, "append");
            matched
                .actions
                .push(AltActionBinding::Named("@late".into()));
            Ok(None)
        }
    });
    tabnas.action("@late", {
        let events = events.clone();
        move |_rule| record(&events, "late")
    });

    tabnas
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR","a":["@append"]}]}}
            }"##,
        )
        .unwrap();

    tabnas.parse("1").unwrap();
    assert_eq!(["append"], events.lock().unwrap().as_slice());
}

#[test]
fn an_action_appended_by_a_matched_route_hook_does_run() {
    // The push/replace/backtrack/error hooks all see the record before the
    // action sequence starts, so what they append to it is part of that
    // sequence. An alternate carrying one of them is therefore resolved
    // from the record, not from the installed order.
    let events = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    tabnas.alt_push_with_match("@route", {
        let events = events.clone();
        move |_rule, _context, matched| {
            record(&events, "route");
            matched
                .actions
                .push(AltActionBinding::Named("@late".into()));
            None
        }
    });
    tabnas.action("@declared", {
        let events = events.clone();
        move |_rule| record(&events, "declared")
    });
    tabnas.action("@late", {
        let events = events.clone();
        move |_rule| record(&events, "late")
    });

    tabnas
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR","p":"@route","a":["@declared"]}]}}
            }"##,
        )
        .unwrap();

    tabnas.parse("1").unwrap();
    assert_eq!(
        ["route", "declared", "late"],
        events.lock().unwrap().as_slice()
    );
}

#[test]
fn a_before_action_clearing_ao_skips_the_after_actions() {
    // `bo`/`ao`/`bc`/`ac` are run control, not a record of what the rule
    // declares: a rule carries all four set whether or not it declares an
    // action for the phase. Clearing one from a callback still has to stop
    // the phase, whatever the installed order says is in it.
    let events = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    tabnas.state_action_with_next_ref("@top-bo", {
        let events = events.clone();
        move |rule, _context, _next, _out| {
            record(&events, "bo");
            rule.ao = false;
            Ok(None)
        }
    });
    tabnas.state_action_with_next_ref("@top-ao", {
        let events = events.clone();
        move |_rule, _context, _next, _out| {
            record(&events, "ao");
            Ok(None)
        }
    });

    tabnas
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{
                "open":[{"s":"#NR"}],
                "close":[{"s":"#ZZ"}]
              }}
            }"##,
        )
        .unwrap();

    tabnas.parse("1").unwrap();
    assert_eq!(["bo"], events.lock().unwrap().as_slice());
}
