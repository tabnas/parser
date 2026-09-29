// Copyright (c) 2026 Richard Rodger, MIT License

//! `options.rule.history`: how many predecessor snapshots a rule can
//! reach through `prev` (`doc/rule-history-bound.md`). The chain a
//! replace loop links is what keeps every iteration of a sequence alive
//! until its container closes; under a bound, the chain a rule carries
//! stays the bound long whatever the sequence's length, and the parse
//! builds the same value.

use std::collections::HashSet;
use std::rc::Rc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tabnas::{RuleSnapshot, Tabnas, Value, MAX_RULE_HISTORY};

/// The serialized strict-JSON builder grammar, with the history set as
/// `spelling` says (`null` for the default, an integer for a bound).
fn json_parser(spelling: &str) -> Tabnas {
    let source = std::fs::read_to_string("../ts/test/json-builder.fixture.json")
        .expect("json builder fixture");
    let mut parser = Tabnas::new();
    parser.options.rule.start = "val".into();
    parser.grammar_json(&source).expect("install JSON grammar");
    parser
        .grammar_json(&format!(
            r#"{{"options":{{"rule":{{"history":{spelling}}}}}}}"#
        ))
        .expect("set the history");
    parser
}

/// The longest `prev_rule` chain any rule carried while `src` parsed,
/// and the most snapshots any rule could reach through its four links
/// (`parent_rule`, `child_rule`, `prev_rule`, `next_rule`), which is
/// what the parse keeps alive.
fn reach(parser: &mut Tabnas, src: &str) -> (usize, usize) {
    let chain = Arc::new(AtomicUsize::new(0));
    let reachable = Arc::new(AtomicUsize::new(0));
    let (chain_sink, reachable_sink) = (Arc::clone(&chain), Arc::clone(&reachable));
    parser.subscribe_rules(move |rule, _context| {
        let mut length = 0;
        let mut link = rule.prev_rule.as_ref();
        while let Some(prev) = link {
            length += 1;
            link = prev.prev_rule.as_ref();
        }
        chain_sink.fetch_max(length, Ordering::Relaxed);

        let mut seen: HashSet<*const RuleSnapshot> = HashSet::new();
        let mut pending: Vec<Rc<RuleSnapshot>> = Vec::new();
        for link in [
            &rule.parent_rule,
            &rule.child_rule,
            &rule.prev_rule,
            &rule.next_rule,
        ]
        .into_iter()
        .flatten()
        {
            pending.push(Rc::clone(link));
        }
        while let Some(snapshot) = pending.pop() {
            if !seen.insert(Rc::as_ptr(&snapshot)) {
                continue;
            }
            for link in [
                &snapshot.parent_rule,
                &snapshot.child_rule,
                &snapshot.prev_rule,
                &snapshot.next_rule,
            ]
            .into_iter()
            .flatten()
            {
                pending.push(Rc::clone(link));
            }
        }
        reachable_sink.fetch_max(seen.len(), Ordering::Relaxed);
    });
    parser.parse(src).expect("parses");
    (
        chain.load(Ordering::Relaxed),
        reachable.load(Ordering::Relaxed),
    )
}

fn flat_array(items: usize) -> String {
    let items: Vec<String> = (0..items).map(|i| i.to_string()).collect();
    format!("[{}]", items.join(","))
}

#[test]
fn serialized_rule_history_spellings() {
    let mut parser = Tabnas::new();
    assert_eq!(parser.options.rule.history, None);
    parser
        .grammar_json(r#"{"options":{"rule":{"history":3}}}"#)
        .unwrap();
    assert_eq!(parser.options.rule.history, Some(3));
    parser
        .grammar_json(r#"{"options":{"rule":{"history":null}}}"#)
        .unwrap();
    assert_eq!(parser.options.rule.history, None);
    parser
        .grammar_json(r#"{"options":{"rule":{"history":1}}}"#)
        .unwrap();
    assert_eq!(parser.options.rule.history, Some(1));
    parser
        .grammar_json(r#"{"options":{"rule":{"history":false}}}"#)
        .unwrap();
    assert_eq!(parser.options.rule.history, None);
    parser
        .grammar_json(&format!(
            r#"{{"options":{{"rule":{{"history":{MAX_RULE_HISTORY}}}}}}}"#
        ))
        .unwrap();
    assert_eq!(parser.options.rule.history, Some(MAX_RULE_HISTORY));
    let error = parser
        .grammar_json(&format!(
            r#"{{"options":{{"rule":{{"history":{}}}}}}}"#,
            MAX_RULE_HISTORY + 1
        ))
        .err()
        .expect("a bound past the cap is refused");
    assert!(
        error
            .to_string()
            .contains("options.rule.history is outside the supported range (at most 16)"),
        "{error}"
    );
    for bad in ["0", "-3", "2.5", "true", "\"3\""] {
        let error = parser
            .grammar_json(&format!(r#"{{"options":{{"rule":{{"history":{bad}}}}}}}"#))
            .err()
            .unwrap_or_else(|| panic!("{bad} was accepted"));
        assert!(
            error
                .to_string()
                .contains("options.rule.history must be an integer of at least 1"),
            "{bad}: {error}"
        );
    }
}

#[test]
fn the_history_bounds_the_chain_a_sequence_links_and_changes_no_value() {
    let (short, long) = (flat_array(500), flat_array(2000));
    // Every iteration of the element loop is linked to the one before
    // it, back to the first: the chain is the sequence, and so is what
    // a rule can reach.
    let (chain, reachable) = reach(&mut json_parser("null"), &short);
    assert!(chain >= 499, "unbounded chain: {chain}");
    assert!(reachable >= 500, "unbounded reach: {reachable}");
    // A bound of three lets a rule reach three predecessors, whatever
    // the sequence's length, and the same few snapshots in all over
    // 500 items as over 2,000; a bound of one, the one it replaced.
    let (chain, over_short) = reach(&mut json_parser("3"), &short);
    assert_eq!(chain, 3);
    let (chain, over_long) = reach(&mut json_parser("3"), &long);
    assert_eq!(chain, 3);
    assert_eq!(over_short, over_long, "reach grew with the sequence");
    assert!(over_long <= 32, "bounded reach: {over_long}");
    let (chain, over_one) = reach(&mut json_parser("1"), &long);
    assert_eq!(chain, 1);
    assert!(
        over_one <= over_long,
        "history 1 reaches {over_one}, history 3 {over_long}"
    );
    // The value is the same either way.
    let bounded = json_parser("3").parse(&long).unwrap();
    let plain = json_parser("null").parse(&long).unwrap();
    assert_eq!(bounded, plain);
    assert_eq!(bounded.to_string().len(), long.len());
}

/// What a pushed child reads through `parent`, for every rule a parse
/// runs: its name, and the names its pusher's copy links as `child` and
/// `next`.
fn parent_links(parser: &mut Tabnas, src: &str) -> Vec<(String, Option<String>, Option<String>)> {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let sink = Arc::clone(&seen);
    parser.subscribe_rules(move |rule, _context| {
        if let Some(parent) = rule.parent_rule.as_deref() {
            let name = |link: &Option<Rc<RuleSnapshot>>| link.as_ref().map(|s| s.name.to_string());
            sink.lock().unwrap().push((
                rule.name.to_string(),
                name(&parent.child_rule),
                name(&parent.next_rule),
            ));
        }
    });
    parser.parse(src).expect("parses");
    let links = seen.lock().unwrap().clone();
    links
}

/// A pushed child reads its pusher's `child` and `next` under a bound as
/// it does without one: the pusher's copy keeps its own links, since
/// they hold the child as it stood when pushed and reach no further
/// back. Cut with the rest, `parent.child` and `parent.next` resolved to
/// nothing in Rust alone. Only a replaced rule's copy drops them, where
/// they are a ladder through the sequence (`rule-history-bounded-child`
/// registers `prev.child`).
#[test]
fn a_pushed_child_reads_its_parents_child_and_next_under_a_bound() {
    let src = r#"{"a":[1,[2,3],{"b":4}],"c":[5]}"#;
    let plain = parent_links(&mut json_parser("null"), src);
    assert!(
        plain
            .iter()
            .any(|(_, child, next)| child.is_some() && next.is_some()),
        "no parent links to compare: {plain:?}"
    );
    for bound in ["1", "3"] {
        assert_eq!(
            parent_links(&mut json_parser(bound), src),
            plain,
            "history {bound}"
        );
    }
}

/// `list` pushes every item from its close phase: `b` peeks the next
/// item's token and pushes `item` again, `e` ends the list. Rule depth
/// stays constant, and each item is linked to its pusher as it stood
/// just after the item before it popped.
fn close_push_parser(history: &str) -> Tabnas {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&format!(
            r#"{{"options":{{"rule":{{"start":"list","history":{history}}},
                "fixed":{{"token":{{"Ta":"a","Tb":"b","Te":"e"}}}}}},
              "rule":{{
                "list":{{"open":[{{"s":"Ta"}}],
                         "close":[{{"s":"Tb","b":1,"p":"item"}},{{"s":"Te"}}]}},
                "item":{{"open":[{{"s":"Tb"}}],"close":[{{}}]}}}}}}"#
        ))
        .expect("install the close-push grammar");
    parser
}

/// A rule that pushes again from its close phase keeps the bound too.
/// The pusher's copy made before it links the new child holds the child
/// before, finished; kept, that copy linked each item to the one before
/// it, through the new child's own snapshot, and what a rule could reach
/// grew five snapshots per item under every bound, past the unbounded
/// parse. The pusher's `parent.child` and `parent.next` still read as
/// unbounded.
#[test]
fn a_close_phase_push_loop_keeps_the_bound() {
    let items = |count: usize| format!("a{}e", "b".repeat(count));
    let (short, long) = (items(500), items(2000));
    for bound in ["1", "3"] {
        let (_, over_short) = reach(&mut close_push_parser(bound), &short);
        let (_, over_long) = reach(&mut close_push_parser(bound), &long);
        assert_eq!(over_short, over_long, "history {bound}: reach grew");
        assert!(over_long <= 32, "history {bound}: reach {over_long}");
        assert_eq!(
            parent_links(&mut close_push_parser(bound), &short),
            parent_links(&mut close_push_parser("null"), &short),
            "history {bound}"
        );
    }
}

/// A copy that cuts its `next` cuts the name with it. A snapshot whose
/// `next` has its own name reads itself as `next`, so under a bound a
/// replacement loop's `prev.next` read the predecessor itself, not
/// nothing, and `prev.next.state` its state. Every copy a bounded parse
/// links as `prev` carries neither.
#[test]
fn a_cut_next_takes_its_name_with_it() {
    let cut = Arc::new(AtomicUsize::new(0));
    let named = Arc::new(AtomicUsize::new(0));
    let (cut_sink, named_sink) = (Arc::clone(&cut), Arc::clone(&named));
    let mut parser = json_parser("3");
    parser.subscribe_rules(move |rule, _context| {
        if let Some(prev) = rule.prev_rule.as_deref() {
            cut_sink.fetch_add(1, Ordering::Relaxed);
            if prev.next_rule.is_some() || prev.next_rule_name.is_some() {
                named_sink.fetch_add(1, Ordering::Relaxed);
            }
        }
    });
    parser.parse(&flat_array(50)).expect("parses");
    assert!(cut.load(Ordering::Relaxed) > 0, "no replacement ran");
    assert_eq!(
        named.load(Ordering::Relaxed),
        0,
        "a prev copy kept its next"
    );
}

/// A bound past [`MAX_RULE_HISTORY`] set on the options directly, where
/// no grammar refuses it, is read as the cap: each link copies at most
/// that many snapshots, so a bound as long as the sequence costs a
/// constant per link rather than one copy per item before it.
#[test]
fn a_bound_past_the_cap_is_read_as_the_cap() {
    let (short, long) = (flat_array(500), flat_array(2000));
    let capped = |src: &str| {
        let mut parser = json_parser("null");
        parser.options.rule.history = Some(usize::MAX);
        reach(&mut parser, src)
    };
    let (chain, over_short) = capped(&short);
    assert_eq!(chain, MAX_RULE_HISTORY);
    let (chain, over_long) = capped(&long);
    assert_eq!(chain, MAX_RULE_HISTORY);
    assert_eq!(over_short, over_long, "reach grew with the sequence");
    // And `Some(0)`, which no grammar can set, as 1: a copy keeps the
    // snapshot itself at least, where it copied nothing and failed every
    // push and replace with an internal error.
    let mut parser = json_parser("null");
    parser.options.rule.history = Some(0);
    let (chain, _) = reach(&mut parser, &short);
    assert_eq!(chain, 1);
}

/// Peak resident memory (`VmHWM`, so Linux only) and time over a flat
/// array, for the design document's table. Run by hand, in a release
/// build:
///
///     TABNAS_HISTORY=null TABNAS_ITEMS=300000 \
///     cargo test --release --test rule_history_test -- --ignored --nocapture
///
/// `TABNAS_FILE=<path>` parses that JSON file instead. A line every
/// thirty seconds says how far the parse is through the source.
#[test]
#[ignore]
fn measure_peak_memory_over_a_flat_array() {
    let spelling = std::env::var("TABNAS_HISTORY").unwrap_or_else(|_| "null".into());
    let items: usize = std::env::var("TABNAS_ITEMS")
        .ok()
        .and_then(|n| n.parse().ok())
        .unwrap_or(300_000);
    // `TABNAS_FILE` parses that JSON file instead of the generated array.
    let src = match std::env::var("TABNAS_FILE") {
        Ok(file) => std::fs::read_to_string(&file).expect("the file to parse"),
        Err(_) => flat_array(items),
    };
    let mut parser = json_parser(&spelling);
    let started = Instant::now();
    let last_report = Mutex::new(started);
    let len = src.len();
    parser.subscribe_rules(move |rule, _context| {
        let mut last = last_report.lock().unwrap();
        if last.elapsed() < Duration::from_secs(30) {
            return;
        }
        *last = Instant::now();
        let at = rule.o0().map_or(0, |token| token.site.si);
        println!(
            "parsing: {at} of {len} bytes ({}%), {:.0}s",
            at * 100 / len.max(1),
            started.elapsed().as_secs_f64()
        );
    });
    let value = parser.parse(&src).expect("parses");
    let elapsed = started.elapsed();
    // The count reported is the parsed array's, so a file's is its own.
    let items = match &value {
        Value::Array(elements) => elements.len(),
        _ => items,
    };
    println!(
        "history={spelling} items={items} bytes={len} elapsed={:.2}s peak={} MB value_len={}",
        elapsed.as_secs_f64(),
        peak_rss_mb(),
        value.to_string().len()
    );
}

/// Peak resident memory of this process in MB, from `VmHWM`.
#[cfg(target_os = "linux")]
fn peak_rss_mb() -> usize {
    let status = std::fs::read_to_string("/proc/self/status").expect("/proc/self/status");
    status
        .lines()
        .find_map(|line| line.strip_prefix("VmHWM:"))
        .and_then(|rest| {
            rest.trim()
                .trim_end_matches(" kB")
                .trim()
                .parse::<usize>()
                .ok()
        })
        .expect("VmHWM in /proc/self/status")
        / 1024
}

/// The measurement reads Linux's `/proc`; elsewhere it stops rather than
/// report a peak it did not measure.
#[cfg(not(target_os = "linux"))]
fn peak_rss_mb() -> usize {
    panic!("peak resident memory is read from /proc/self/status, which needs Linux");
}
