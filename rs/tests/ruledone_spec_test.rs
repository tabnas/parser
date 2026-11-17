//! The shared `RuleDone` contract, `test/spec/ruledone.tsv`: what a
//! `rule_done` subscriber sees of each pass, in order, as
//! `rule:state[groups]`, with `!` when the pass failed and `rule:state`
//! alone when its state had no alternates. A pass on which no alternate
//! matches names no groups (tabnas/parser#340). Every runtime runs it.

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
fn shared_rule_done_contract() {
    const COLUMNS: usize = 4;
    let source = fs::read_to_string("../test/spec/ruledone.tsv").expect("ruledone.tsv");
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
        assert_eq!(columns.len(), COLUMNS, "ruledone.tsv:{}", index + 1);
        let [name, args, input, expected]: &[String; COLUMNS] =
            columns.as_slice().try_into().expect("checked column count");
        let args: JsonValue = serde_json::from_str(args).expect("row arguments");
        let spec = args["spec"].as_str().expect("a named spec");
        let document = specs.get(spec).expect("a defined # @spec");

        let mut parser = Tabnas::new();
        parser.grammar_json(document).expect("the spec installs");
        let passes: Arc<Mutex<Vec<String>>> = Arc::default();
        let seen = passes.clone();
        parser.subscribe_rule_done(move |rule, _context, done| {
            let state = match done.state {
                RuleState::Open => "open",
                RuleState::Close => "close",
            };
            let mut pass = format!("{}:{state}", rule.name.as_str());
            if let Some(alt) = &done.alt {
                pass.push_str(&format!("[{}]", alt.g.join(",")));
                if alt.err.is_some() {
                    pass.push('!');
                }
            }
            seen.lock().unwrap().push(pass);
        });
        let error = match parser.parse(input) {
            Ok(_) => JsonValue::Null,
            Err(error) => JsonValue::String(error.code),
        };
        let passes = passes.lock().unwrap().clone();
        let actual = json!({ "passes": passes, "error": error });
        let expected: JsonValue = serde_json::from_str(expected).expect("expected JSON");
        assert_eq!(actual, expected, "ruledone row {name}");
        ran += 1;
    }
    assert!(ran > 0, "ruledone.tsv ran no rows");
}
