// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! The alternate's action order is walked where it lies, straight out of
//! the record built when the rule was installed, whenever nothing the step
//! runs can receive the live `AltMatch`. That gate is a disjunction of
//! eight terms plus a ninth condition on named bindings, and each term is
//! there because one kind of callback CAN receive the record. A term that
//! no test reaches is a term a later change can delete with the suite
//! green, so there is one test here per term.
//!
//! Five of the hooks run after the step publishes the order into the
//! record and before the action sequence starts (`parser.rs:2345` then
//! `:2372`, `:2406`, `:2444`, `:2481`, `:2500`), so for those the question
//! the test asks is simply whether the hook sees the published list. The
//! two matched conditions run earlier, during alternate selection, so for
//! those the question is whether what the condition writes into the record
//! survives into the sequence.

use std::sync::{Arc, Mutex};
use tabnas::{AltActionBinding, AltSpec, Tabnas, TIN_NR};

/// How many bindings the hook saw in the record when it ran. `None` means
/// the hook never ran at all, which is a different failure and worth
/// telling apart.
type Seen = Arc<Mutex<Option<usize>>>;

fn seen() -> Seen {
    Arc::new(Mutex::new(None))
}

/// A grammar whose single alternate declares one action and carries
/// whatever hook the caller registered under `key`.
fn grammar_with(tabnas: &mut Tabnas, key: &str, reference: &str) {
    let src = format!(
        r##"{{
          "clear":true,
          "options":{{"rule":{{"start":"top"}}}},
          "rule":{{"top":{{"open":[{{"s":"#NR","{key}":"{reference}","a":["@declared"]}}]}}}}
        }}"##
    );
    tabnas.grammar_json(&src).unwrap();
}

fn declare_action(tabnas: &mut Tabnas) {
    tabnas.action("@declared", |_rule| {});
}

fn assert_saw_the_list(what: &str, saw: &Seen) {
    match *saw.lock().unwrap() {
        None => panic!("the {what} hook never ran, so this test proves nothing"),
        Some(0) => panic!(
            "the {what} hook saw an empty action list: the step took the \
             in-place path for an alternate that can read the record"
        ),
        Some(n) => assert_eq!(n, 1, "the {what} hook should see the one declared action"),
    }
}

#[test]
fn the_error_hook_sees_the_published_action_list() {
    let mut tabnas = Tabnas::new();
    let saw = seen();
    let sink = saw.clone();
    tabnas.alt_error_with_match("@err", move |_rule, _context, matched| {
        *sink.lock().unwrap() = Some(matched.actions.len());
        None
    });
    declare_action(&mut tabnas);
    grammar_with(&mut tabnas, "e", "@err");
    tabnas.parse("1").unwrap();
    assert_saw_the_list("error", &saw);
}

#[test]
fn the_push_hook_sees_the_published_action_list() {
    let mut tabnas = Tabnas::new();
    let saw = seen();
    let sink = saw.clone();
    tabnas.alt_push_with_match("@route", move |_rule, _context, matched| {
        *sink.lock().unwrap() = Some(matched.actions.len());
        None
    });
    declare_action(&mut tabnas);
    grammar_with(&mut tabnas, "p", "@route");
    tabnas.parse("1").unwrap();
    assert_saw_the_list("push", &saw);
}

#[test]
fn the_replace_hook_sees_the_published_action_list() {
    let mut tabnas = Tabnas::new();
    let saw = seen();
    let sink = saw.clone();
    tabnas.alt_replace_with_match("@repl", move |_rule, _context, matched| {
        *sink.lock().unwrap() = Some(matched.actions.len());
        None
    });
    declare_action(&mut tabnas);
    grammar_with(&mut tabnas, "r", "@repl");
    tabnas.parse("1").unwrap();
    assert_saw_the_list("replace", &saw);
}

#[test]
fn the_backtrack_hook_sees_the_published_action_list() {
    let mut tabnas = Tabnas::new();
    let saw = seen();
    let sink = saw.clone();
    tabnas.alt_backtrack_with_match("@back", move |_rule, _context, matched| {
        *sink.lock().unwrap() = Some(matched.actions.len());
        0
    });
    declare_action(&mut tabnas);
    grammar_with(&mut tabnas, "b", "@back");
    tabnas.parse("1").unwrap();
    assert_saw_the_list("backtrack", &saw);
}

#[test]
fn the_modifier_sees_the_published_action_list() {
    let mut tabnas = Tabnas::new();
    let saw = seen();
    let sink = saw.clone();
    tabnas.alt_modifier_with_match("@mod", move |matched, _rule, _context, _next| {
        *sink.lock().unwrap() = Some(matched.actions.len());
        matched
    });
    declare_action(&mut tabnas);
    grammar_with(&mut tabnas, "h", "@mod");
    tabnas.parse("1").unwrap();
    assert_saw_the_list("modifier", &saw);
}

#[test]
fn a_named_matched_action_sees_the_published_action_list() {
    // A name in `a` is a `Named` binding whatever it resolves to, and it
    // resolves against the public `matched_actions` map, which an embedder
    // may write after `add_rule`. The step therefore asks per rule step
    // whether that map could answer, and fills the record when it could.
    let mut tabnas = Tabnas::new();
    let saw = seen();
    let sink = saw.clone();
    tabnas.action_with_match_ref("@m", move |_rule, _context, matched| {
        *sink.lock().unwrap() = Some(matched.actions.len());
        Ok(None)
    });
    tabnas
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR","a":["@m"]}]}}
            }"##,
        )
        .unwrap();
    tabnas.parse("1").unwrap();
    assert_saw_the_list("named matched action", &saw);
}

#[test]
fn a_direct_matched_action_sees_the_published_action_list() {
    // A matched action attached to the alternate as a function rather than
    // a name is a `Matched` binding, and it receives the record too.
    let saw = seen();
    let sink = saw.clone();
    let mut tabnas = Tabnas::new();
    tabnas.options.rule.start = "top".into();
    tabnas.define_rule("top", move |rule| {
        let mut open = AltSpec {
            s: vec![vec![TIN_NR]],
            ..Default::default()
        };
        let sink = sink.clone();
        open.add_action_with_match(move |_rule, _context, matched| {
            *sink.lock().unwrap() = Some(matched.actions.len());
            None
        });
        rule.open.push(open);
    });
    tabnas.parse("1").unwrap();
    assert_saw_the_list("direct matched action", &saw);
}

#[test]
fn what_a_matched_condition_writes_into_the_action_list_still_runs() {
    // A matched condition runs while alternates are still being tried, so
    // it writes into the record BEFORE the step would publish the declared
    // order into it. This alternate declares no order, so there is nothing
    // to publish and what the condition wrote is what the sequence runs.
    // Walking the installed order in place instead would discard it.
    let ran = Arc::new(Mutex::new(false));
    let mut tabnas = Tabnas::new();
    tabnas.alt_condition_with_match("@cond", |_rule, _context, matched| {
        matched
            .actions
            .push(AltActionBinding::Named("@late".into()));
        true
    });
    let flag = ran.clone();
    tabnas.action("@late", move |_rule| *flag.lock().unwrap() = true);
    tabnas
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR","c":"@cond"}]}}
            }"##,
        )
        .unwrap();
    tabnas.parse("1").unwrap();
    assert!(
        *ran.lock().unwrap(),
        "an action the matched condition pushed onto the record did not run"
    );
}

#[test]
fn what_a_matched_lexer_condition_writes_into_the_action_list_still_runs() {
    // The same, for the condition kind that is also handed the lexer.
    let ran = Arc::new(Mutex::new(false));
    let mut tabnas = Tabnas::new();
    tabnas.alt_condition_with_lexer_and_match("@lexcond", |_rule, _context, matched, _lexer| {
        matched
            .actions
            .push(AltActionBinding::Named("@late".into()));
        true
    });
    let flag = ran.clone();
    tabnas.action("@late", move |_rule| *flag.lock().unwrap() = true);
    tabnas
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{"s":"#NR","c":"@lexcond"}]}}
            }"##,
        )
        .unwrap();
    tabnas.parse("1").unwrap();
    assert!(
        *ran.lock().unwrap(),
        "an action the matched lexer condition pushed onto the record did not run"
    );
}
