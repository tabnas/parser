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

use tabnas::{RuleSnapshot, Tabnas, Value};

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
