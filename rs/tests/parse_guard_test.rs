// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Parse guards: named checks the loop runs at every step, which a caller's
//! `parse_budget` does not replace.
//!
//! The grammars bound nesting with one, because a `Value` drops and
//! displays by recursion and a stack overflow ends the process. They kept
//! that bound in the budget until a caller setting a budget of its own was
//! found to remove it.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, Context, Plugin, RuleSpec, Tabnas, TIN_NR, TIN_ZZ};

fn number_rules(tabnas: &mut Tabnas) {
    tabnas.options.rule.start = "top".into();
    let mut top = RuleSpec::new("top");
    top.open.push(AltSpec {
        s: vec![vec![TIN_NR]],
        ..Default::default()
    });
    top.close.push(AltSpec {
        s: vec![vec![TIN_ZZ]],
        ..Default::default()
    });
    tabnas.rule(top);
}

fn number_parser() -> Tabnas {
    let mut parser = Tabnas::new();
    number_rules(&mut parser);
    parser
}

/// How many lists are open: the ancestors on the stack, and the rule the
/// loop is working on, which the engine hands over apart from them.
fn list_depth(context: &Context) -> usize {
    let is_list = |name: &str| name == "list";
    let ancestors = context
        .rule_stack
        .iter()
        .filter(|rule| is_list(&rule.name))
        .count();
    ancestors
        + usize::from(
            context
                .rule
                .as_ref()
                .is_some_and(|rule| is_list(&rule.name)),
        )
}

fn nested(depth: usize) -> String {
    "[".repeat(depth) + &"]".repeat(depth)
}

#[test]
fn a_guard_that_refuses_stops_the_parse_with_cancel() {
    let mut parser = Tabnas::make_json();
    let calls = Arc::new(AtomicUsize::new(0));
    let seen = calls.clone();
    parser.parse_guard("stop", move |context| {
        assert!(context.iteration > 0);
        seen.fetch_add(1, Ordering::SeqCst) < 2
    });
    let error = parser.parse(r#"{"a":[1,2,3]}"#).unwrap_err();
    assert_eq!(error.code, "cancel");
    assert!(!error.rule.is_empty());
    assert_eq!(error.rule_stack.last(), Some(&error.rule));
    assert_eq!(calls.load(Ordering::SeqCst), 3);
}

#[test]
fn guards_run_at_every_step_ahead_of_the_budget() {
    let log = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::make_json();
    let guard_log = log.clone();
    parser.parse_guard("first", move |_| {
        guard_log.lock().unwrap().push("guard");
        true
    });
    let budget_log = log.clone();
    parser.parse_budget(1, move |_| {
        budget_log.lock().unwrap().push("budget");
        true
    });
    assert!(parser.parse(r#"{"a":[1,2,3]}"#).is_ok());
    let log = log.lock().unwrap();
    assert!(!log.is_empty());
    for pair in log.chunks(2) {
        assert_eq!(pair, ["guard", "budget"]);
    }
}

#[test]
fn a_budget_set_later_does_not_remove_a_guard() {
    let mut parser = number_parser();
    parser.parse_guard("stop", |_| false);
    let budget_calls = Arc::new(AtomicUsize::new(0));
    let seen = budget_calls.clone();
    parser.parse_budget(1, move |_| {
        seen.fetch_add(1, Ordering::SeqCst);
        true
    });
    assert_eq!(parser.parse("1").unwrap_err().code, "cancel");
    // The guard refused first, so the budget never ran.
    assert_eq!(budget_calls.load(Ordering::SeqCst), 0);
}

#[test]
fn a_grammar_document_applied_later_keeps_a_guard() {
    let mut parser = number_parser();
    parser.parse_guard("stop", |_| false);
    parser
        .grammar_json(r#"{"options":{"parse":{"budget":{"checkEveryN":5}}}}"#)
        .unwrap();
    assert_eq!(parser.parse("1").unwrap_err().code, "cancel");
}

#[test]
fn a_name_in_use_is_replaced_and_new_names_accumulate() {
    let mut parser = number_parser();
    parser.parse_guard("depth", |_| false);
    parser.parse_guard("depth", |_| true);
    assert!(
        parser.parse("1").is_ok(),
        "the second `depth` replaced the first"
    );
    assert_eq!(parser.parse_guards.len(), 1);

    parser.parse_guard("other", |_| false);
    assert_eq!(parser.parse("1").unwrap_err().code, "cancel");
    parser.remove_parse_guard("other");
    assert!(parser.parse("1").is_ok());
    parser.remove_parse_guard("never-installed");
    assert_eq!(
        parser.parse_guards.keys().collect::<Vec<_>>(),
        ["depth"],
        "removing a name not in use changes nothing"
    );
}

#[test]
fn a_derived_instance_keeps_its_parents_guards() {
    // A plugin's rules come back when the child re-runs it; the guard,
    // installed apart from any plugin, comes with the child's options.
    let mut parent = Tabnas::new();
    parent
        .use_plugin(
            Plugin::new("numbers", |tabnas, _| {
                number_rules(tabnas);
                Ok(())
            }),
            None,
        )
        .unwrap();
    parent.parse_guard("stop", |_| false);
    let child = parent.derive(|_| {}).unwrap();
    assert_eq!(child.parse_guards.keys().collect::<Vec<_>>(), ["stop"]);
    assert_eq!(child.parse("1").unwrap_err().code, "cancel");
}

#[test]
fn a_plugin_re_run_by_derive_replaces_its_guard_rather_than_adding_one() {
    let calls = Arc::new(AtomicUsize::new(0));
    let seen = calls.clone();
    let mut parent = Tabnas::new();
    parent
        .use_plugin(
            Plugin::new("guarded", move |tabnas, _| {
                number_rules(tabnas);
                let seen = seen.clone();
                tabnas.parse_guard("guarded", move |_| {
                    seen.fetch_add(1, Ordering::SeqCst);
                    true
                });
                Ok(())
            }),
            None,
        )
        .unwrap();
    let child = parent.derive(|_| {}).unwrap();
    assert_eq!(child.parse_guards.len(), 1);
    assert!(child.parse("1").is_ok());
    let once = calls.load(Ordering::SeqCst);
    assert!(once > 0);
    assert!(child.parse("1").is_ok());
    assert_eq!(calls.load(Ordering::SeqCst), 2 * once, "one guard, not two");
}

#[test]
fn a_merge_keeps_both_sides_guards() {
    let tagged = |tag: &str| {
        let mut parser = number_parser();
        parser.options.tag = tag.into();
        parser
    };
    let mut left = tagged("A");
    left.parse_guard("depth", |_| true);
    let mut right = tagged("B");
    right.parse_guard("depth", |_| false);
    let merged = left.merge(&right).unwrap();
    assert_eq!(
        merged.parse_guards.keys().collect::<Vec<_>>(),
        ["A:depth", "B:depth"]
    );
    assert_eq!(merged.parse("1").unwrap_err().code, "cancel");
}

#[test]
fn a_panicking_guard_is_an_error_and_not_a_crash() {
    let mut parser = number_parser();
    parser.parse_guard("boom", |_| panic!("guard exploded"));
    let error = parser.parse("1").unwrap_err();
    assert_eq!(error.token.name, "#NR");
    assert_eq!(error.rule, "top");
}

#[test]
fn a_depth_bound_holds_whatever_budget_the_caller_sets() {
    // The case guards exist for: a grammar bounds nesting, and a caller
    // then sets a budget of its own. Before guards, the bound lived in the
    // budget, and the caller's replaced it.
    let mut parser = Tabnas::make_json();
    parser.parse_guard("depth", |context| list_depth(context) <= 32);
    parser.parse_budget(1, |_| true);
    assert!(parser.parse(&nested(32)).is_ok());
    assert_eq!(parser.parse(&nested(33)).unwrap_err().code, "cancel");
    assert_eq!(parser.parse(&nested(10_000)).unwrap_err().code, "cancel");
}
