//! `RuleDone::probe`: a pass made while a probe is open
//! (`Context::probe_open`, which `@probeInit$` calls) is the probe's, not
//! the document's, since the grammar rewinds past it. The shared fixture
//! `test/spec/probe.tsv` pins which passes they are, and every runtime
//! runs it.

use indexmap::IndexMap;
use serde_json::{json, Value as JsonValue};
use std::fs;
use std::sync::{Arc, Mutex};
use tabnas::{RuleState, Tabnas};

fn preprocess(value: &str) -> String {
    value
        .replace("\\r\\n", "\r\n")
        .replace("\\n", "\n")
        .replace("\\r", "\r")
        .replace("\\t", "\t")
}

#[test]
fn shared_probe_contract() {
    const COLUMNS: usize = 4;
    let source = fs::read_to_string("../test/spec/probe.tsv").expect("probe.tsv");
    let mut specs = IndexMap::new();
    for line in source.lines() {
        if let Some(spec) = line.strip_prefix("# @spec ") {
            let (name, document) = spec.split_once(' ').expect("named spec document");
            specs.insert(name.to_string(), document.to_string());
        }
    }

    let mut ran = 0;
    for (index, raw) in source.lines().enumerate().skip(1) {
        if raw.starts_with('#') || raw.trim().is_empty() {
            continue;
        }
        let columns: Vec<String> = raw.split('\t').map(preprocess).collect();
        assert_eq!(columns.len(), COLUMNS, "probe.tsv:{}", index + 1);
        let [name, args, input, expected]: &[String; COLUMNS] =
            columns.as_slice().try_into().expect("checked column count");
        let args: JsonValue = serde_json::from_str(args).expect("row arguments");
        let spec = args["spec"].as_str().expect("a named spec");
        let document = specs.get(spec).expect("a defined # @spec");

        let mut parser = Tabnas::new();
        parser.grammar_json(document).expect("the spec installs");
        let passes: Arc<Mutex<(Vec<String>, usize)>> = Arc::default();
        let seen = passes.clone();
        parser.subscribe_rule_done(move |rule, _context, done| {
            let mut seen = seen.lock().unwrap();
            if done.probe {
                let state = match done.state {
                    RuleState::Open => "open",
                    RuleState::Close => "close",
                };
                seen.0.push(format!("{}:{state}", rule.name.as_str()));
            } else {
                seen.1 += 1;
            }
        });
        parser.parse(input).expect("the row parses");
        let (probe, kept) = passes.lock().unwrap().clone();
        let actual = json!({ "probe": probe, "kept": kept });
        let expected: JsonValue = serde_json::from_str(expected).expect("expected JSON");
        assert_eq!(actual, expected, "probe row {name}");
        ran += 1;
    }
    assert!(ran > 0, "probe.tsv ran no rows");
}

#[test]
fn a_grammar_that_never_probes_flags_no_pass() {
    let mut parser = Tabnas::make_json();
    let passes: Arc<Mutex<usize>> = Arc::default();
    let seen = passes.clone();
    parser.subscribe_rule_done(move |_rule, _context, done| {
        assert!(!done.probe, "a pass of a grammar with no probe is flagged");
        *seen.lock().unwrap() += 1;
    });
    parser.parse(r#"{"a":[1,2,{"b":null}]}"#).unwrap();
    assert!(*passes.lock().unwrap() > 0, "no ruleDone event fired");
}
