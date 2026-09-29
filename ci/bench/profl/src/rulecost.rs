//! The per-rule cost workload of `doc/per-rule-cost.md`, shared by the
//! `rulecost*` binaries: the strict-JSON builder parser exactly as
//! `rs/tests/rule_history_test.rs` `json_parser()` builds it, argument
//! handling, one timed parse, and `/proc` readings.
//!
//! `rulecost` measures in the configuration `rs/README.md` names (fat LTO,
//! mimalloc); `rulecost_sys` is the same parse on the system allocator,
//! which is what a binary gets when it chooses none; `rulecost_alloc`
//! counts allocations. The inputs are `ci/bench/rulecost/gen.py`'s.

#![allow(dead_code)]

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Instant;

use tabnas::{RuleState, Tabnas, Value};

const FIXTURE: &str = include_str!(concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../../../ts/test/json-builder.fixture.json"
));

/// `history`: `None` is the default (unbounded, spelled `null`), `Some(n)`
/// a bound, spelled as the integer.
pub fn json_parser(history: Option<usize>) -> Tabnas {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "val".into();
    parser.grammar_json(FIXTURE).expect("install JSON grammar");
    let spelling = history.map_or("null".to_string(), |n| n.to_string());
    parser
        .grammar_json(&format!(
            r#"{{"options":{{"rule":{{"history":{spelling}}}}}}}"#
        ))
        .expect("set the history");
    assert_eq!(parser.options.rule.history, history);
    parser
}

pub struct Args {
    pub input: String,
    pub history: Option<usize>,
    pub count: bool,
}

/// `BIN [--count] INPUT [HISTORY]`: HISTORY is `unset` (the default,
/// unbounded) or an integer.
pub fn args() -> Args {
    let mut count = false;
    let mut positional = Vec::new();
    for arg in std::env::args().skip(1) {
        match arg.as_str() {
            "--count" => count = true,
            _ => positional.push(arg),
        }
    }
    let history = match positional.get(1).map(String::as_str) {
        None | Some("unset") | Some("null") => None,
        Some(n) => Some(n.parse().expect("history is unset or an integer")),
    };
    Args {
        input: positional.first().cloned().expect("an INPUT argument"),
        history,
        count,
    }
}

pub fn elements(value: &Value) -> usize {
    match value {
        Value::Array(items) => items.len(),
        Value::Object(members) => members.len(),
        Value::ListRef(list) => list.value.len(),
        _ => 0,
    }
}

pub fn history_label(history: Option<usize>) -> String {
    history.map_or("unset".to_string(), |n| n.to_string())
}

/// `(VmHWM, VmRSS)` in kB.
fn vm_status() -> (u64, u64) {
    let status = std::fs::read_to_string("/proc/self/status").unwrap_or_default();
    let field = |name: &str| {
        status
            .lines()
            .find(|l| l.starts_with(name))
            .and_then(|l| l.split_whitespace().nth(1))
            .and_then(|v| v.parse().ok())
            .unwrap_or(0)
    };
    (field("VmHWM:"), field("VmRSS:"))
}

/// `(utime_s, stime_s, minflt)` for this process, from `/proc/self/stat`.
fn proc_times() -> (f64, f64, u64) {
    let stat = std::fs::read_to_string("/proc/self/stat").unwrap_or_default();
    // Fields after the `(comm)`: minflt is field 10, utime 14, stime 15.
    let rest = stat.rsplit_once(')').map(|(_, r)| r).unwrap_or("");
    let f: Vec<&str> = rest.split_whitespace().collect();
    let get = |i: usize| {
        f.get(i - 3)
            .and_then(|v| v.parse::<u64>().ok())
            .unwrap_or(0)
    };
    let tick = 100.0; // USER_HZ
    (get(14) as f64 / tick, get(15) as f64 / tick, get(10))
}

/// Steal ticks for the whole machine, from `/proc/stat`.
fn steal_ticks() -> u64 {
    std::fs::read_to_string("/proc/stat")
        .unwrap_or_default()
        .lines()
        .next()
        .and_then(|l| l.split_whitespace().nth(8))
        .and_then(|v| v.parse().ok())
        .unwrap_or(0)
}

/// Time one parse of INPUT under HISTORY and print one line of
/// `key=value` pairs. `--count` subscribes a rule and a token subscriber
/// and reports steps, rules and tokens per element; its timing includes
/// the subscribers and is not the timing run.
pub fn timed_parse() {
    let args = args();
    let mut parser = json_parser(args.history);
    let src = std::fs::read_to_string(&args.input).expect("read input");

    let steps = Arc::new(AtomicU64::new(0));
    let max_id = Arc::new(AtomicUsize::new(0));
    let max_depth = Arc::new(AtomicUsize::new(0));
    let tokens = Arc::new(AtomicU64::new(0));
    let by_name: Arc<Mutex<HashMap<(String, &'static str), u64>>> =
        Arc::new(Mutex::new(HashMap::new()));
    if args.count {
        let (steps, max_id, max_depth, by_name) = (
            Arc::clone(&steps),
            Arc::clone(&max_id),
            Arc::clone(&max_depth),
            Arc::clone(&by_name),
        );
        parser.subscribe_rules(move |rule, _context| {
            steps.fetch_add(1, Ordering::Relaxed);
            max_id.fetch_max(rule.i, Ordering::Relaxed);
            max_depth.fetch_max(rule.d, Ordering::Relaxed);
            let state = match rule.state {
                RuleState::Open => "open",
                RuleState::Close => "close",
            };
            *by_name
                .lock()
                .unwrap()
                .entry((rule.name.to_string(), state))
                .or_insert(0) += 1;
        });
        let tokens = Arc::clone(&tokens);
        parser.subscribe_tokens(move |_token| {
            tokens.fetch_add(1, Ordering::Relaxed);
        });
    }

    let (u0, s0, f0) = proc_times();
    let steal0 = steal_ticks();
    let t = Instant::now();
    let value = parser.parse(&src).expect("parses");
    let parse_s = t.elapsed().as_secs_f64();
    let (u1, s1, f1) = proc_times();
    let steal = steal_ticks() - steal0;
    let (hwm_kb, _) = vm_status();
    let n = elements(&value);
    drop(value);

    let mut line = format!(
        "input={} bytes={} history={} elements={} parse_s={:.4} mb_per_s={:.3} \
         us_per_elem={:.3} vmhwm_kb={} user_s={:.2} sys_s={:.2} minflt={} steal_ticks={}",
        args.input,
        src.len(),
        history_label(args.history),
        n,
        parse_s,
        src.len() as f64 / 1e6 / parse_s,
        parse_s * 1e6 / n as f64,
        hwm_kb,
        u1 - u0,
        s1 - s0,
        f1 - f0,
        steal,
    );
    if args.count {
        let steps = steps.load(Ordering::Relaxed);
        let rules = max_id.load(Ordering::Relaxed) + 1;
        let tokens = tokens.load(Ordering::Relaxed);
        line.push_str(&format!(
            " steps={} rules={} tokens={} max_d={} rules_per_elem={:.3} tokens_per_elem={:.3}",
            steps,
            rules,
            tokens,
            max_depth.load(Ordering::Relaxed),
            rules as f64 / n as f64,
            tokens as f64 / n as f64,
        ));
        let mut names: Vec<_> = by_name.lock().unwrap().clone().into_iter().collect();
        names.sort();
        let per: Vec<String> = names
            .iter()
            .map(|((name, state), c)| format!("{name}/{state}:{c}"))
            .collect();
        line.push_str(&format!(" by_rule_state={}", per.join(",")));
    }
    println!("{line}");
}
