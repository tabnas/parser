//! The parse loop keeps one `AltMatch` record for the whole parse and
//! resets it at the head of each rule step. These tests pin the
//! boundaries that reuse has to respect: a rejected alternate's writes
//! must not reach the alternate that wins, a step that leaves by the
//! error path must not leave its error, its routes, its backtrack or its
//! groups behind for the next step, what a step publishes into the record
//! for its own callbacks -- its modifier and its action order -- must not
//! be visible to the next step's, and the dynamic route callbacks must
//! still drive the routing block.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};

use tabnas::{Tabnas, Value};

/// A rejected alternate is handed the live record, so what it wrote has to
/// be gone before the next alternate is offered one.
#[test]
fn a_rejected_alternates_match_writes_do_not_reach_the_winner() {
    let mut parser = Tabnas::new();
    let rejected = Arc::new(AtomicUsize::new(0));
    let seen = Arc::new(Mutex::new(Vec::new()));

    let count = rejected.clone();
    parser.alt_condition_with_match("@reject", move |_rule, _context, matched| {
        matched.u.insert("leak".into(), Value::Bool(true));
        matched.k.insert("leak".into(), Value::Bool(true));
        matched.n.insert("leak".into(), 1);
        matched.g.push("leak".into());
        matched.b = 7;
        matched.p = Some("nowhere".into());
        count.fetch_add(1, Ordering::SeqCst);
        false
    });
    parser.alt_condition_with_match("@accept", |_rule, _context, _matched| true);
    let log = seen.clone();
    parser.action_with_match_ref("@observe", move |rule, _context, matched| {
        log.lock().unwrap().push((
            matched.u.contains_key("leak"),
            matched.k.contains_key("leak"),
            matched.n.contains_key("leak"),
            matched.g.clone(),
            matched.b,
            matched.p.clone(),
            rule.u.contains_key("leak"),
        ));
        Ok(None)
    });

    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#TA":"a"}}
              },
              "rule":{"top":{"open":[
                {"s":"#TA","c":"@reject"},
                {"s":"#TA","c":"@accept","a":"@observe"}
              ]}}
            }"##,
        )
        .unwrap();

    parser.parse("a").unwrap();
    assert_eq!(rejected.load(Ordering::SeqCst), 1);
    assert_eq!(
        *seen.lock().unwrap(),
        [(false, false, false, Vec::<String>::new(), 0, None, false)]
    );
}

/// The alternate-error exit is the one step ending that takes nothing out
/// of the record: `e`, `p`, `r`, `b` and `g` are all resolved before the
/// error is raised, and the step leaves with all five still in it. The
/// erroring alternate therefore carries a push route AND a replace route,
/// so the `r` this asserts on is a route the step really resolved rather
/// than a field that was never written.
#[test]
fn a_step_that_leaves_by_the_error_path_leaves_no_record_behind() {
    let mut parser = Tabnas::new();
    let observed = Arc::new(Mutex::new(Vec::new()));

    parser.alt_error_with_match("@boom", |_rule, context, _matched| {
        let mut token = context.t0().cloned().unwrap_or_default();
        token.bad("boom_code");
        Some(token)
    });
    let log = observed.clone();
    parser.alt_condition_with_match("@observe", move |_rule, _context, matched| {
        log.lock().unwrap().push((
            matched.e.is_some(),
            matched.p.clone(),
            matched.r.clone(),
            matched.b,
            matched.g.clone(),
        ));
        true
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
                  "open":[{"s":"#TA","p":"child"}],
                  "close":[{"s":"","c":"@observe"}]
                },
                "child":{"open":[
                  {"s":"#TB","e":"@boom","p":"grand","r":"grand","b":1,"g":"first"}
                ]},
                "grand":{"open":[{"s":""}]}
              }
            }"##,
        )
        .unwrap();
    parser.options.parse.recover.enabled = true;

    let recovered = parser.parse_recover("ab");
    assert!(recovered.fatal.is_none(), "fatal: {:?}", recovered.fatal);
    assert!(!recovered.errors.is_empty());
    assert!(recovered
        .errors
        .iter()
        .all(|error| error.code == "boom_code"));
    let observed = observed.lock().unwrap();
    assert!(!observed.is_empty(), "the later step never ran");
    for step in observed.iter() {
        assert_eq!(*step, (false, None, None, 0, Vec::<String>::new()));
    }
}

/// Two of the record's fields are published by the step for its own
/// callbacks to read and are never taken back out of it: `h`, the matched
/// modifier, which is assigned on every step that resolves an alternate,
/// and `actions`, the resolved action order, which is assigned only when
/// the order is non-empty. A step that publishes either and a following
/// step that reads the record before it has published its own -- which is
/// every matched condition, since those run while alternates are still
/// being tried -- is the pair that a reset missing `h` or `actions` would
/// let through.
#[test]
fn what_a_step_publishes_into_the_record_is_not_visible_to_the_next_step() {
    let mut parser = Tabnas::new();
    let seen = Arc::new(Mutex::new(Vec::new()));

    // Taking the record by value and handing it straight back is what a
    // modifier that only wants to look does; it leaves `h` in the record.
    parser.alt_modifier_with_match("@mod", |matched, _rule, _context, _next| matched);
    parser.action("@act", |_rule| {});
    let log = seen.clone();
    parser.alt_condition_with_match("@observe", move |_rule, _context, matched| {
        log.lock()
            .unwrap()
            .push((matched.h.is_some(), matched.actions.len()));
        true
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
                "top":{"open":[{"s":"#TA","h":"@mod","a":"@act","p":"child"}]},
                "child":{"open":[{"s":"#TB","c":"@observe"}]}
              }
            }"##,
        )
        .unwrap();

    parser.parse("ab").unwrap();
    let seen = seen.lock().unwrap();
    assert!(!seen.is_empty(), "the step after the modifier never ran");
    for step in seen.iter() {
        assert_eq!(
            *step,
            (false, 0),
            "the previous step's modifier or action order was still in the record"
        );
    }
}

/// The routing block reads `p` and `r` out of the record, so the dynamic
/// route callbacks that write them have to keep driving it.
#[test]
fn dynamic_push_and_replace_callbacks_still_drive_the_routing_block() {
    let mut parser = Tabnas::new();
    let routes = Arc::new(Mutex::new(Vec::new()));

    let log = routes.clone();
    parser.alt_push("@push", move |_rule, _context| {
        log.lock().unwrap().push("push");
        Some("child".into())
    });
    let log = routes.clone();
    parser.alt_replace("@replace", move |_rule, _context| {
        log.lock().unwrap().push("replace");
        Some("tail".into())
    });
    let log = routes.clone();
    parser.action("@in-child", move |_rule| {
        log.lock().unwrap().push("child");
    });
    let log = routes.clone();
    parser.action("@in-tail", move |_rule| {
        log.lock().unwrap().push("tail");
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
                "top":{"open":[{"s":"#TA","p":"@push"}]},
                "child":{"open":[{"s":"#TB","a":"@in-child","r":"@replace"}]},
                "tail":{"open":[{"s":"","a":"@in-tail"}]}
              }
            }"##,
        )
        .unwrap();

    parser.parse("ab").unwrap();
    assert_eq!(
        *routes.lock().unwrap(),
        ["push", "replace", "child", "tail"]
    );
}
