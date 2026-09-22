/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

//! `rule.child` is linked at the PUSH and never relinked.
//!
//! TypeScript sets `rule.child` in the push arm (`ts/src/rules.ts:665`) and
//! nowhere else; Go does the same (`go/rule.go:1266`). A child that REPLACES
//! itself therefore leaves its parent looking at the first instance of the
//! chain, and a capture on the parent's close merges that instance's node.
//! `@fold$` exists precisely because of this, and says so in its doc comment.
//!
//! Rust cannot hold a reference to a rule it has dropped, so it carries the
//! link as the pushed rule's id plus the node cell that rule ended on. This
//! used to be refreshed from whichever rule POPPED, which is the last link of
//! the chain, and `@capture$` then merged a node neither canonical runtime
//! ever sees: the ABNF corpus gate (`ci/rust/notation-corpus.js`) caught it on
//! `R = [ A "@" ] A`, where the optional-prefix probe replaces the dispatch
//! rule once per phase.

use tabnas::{Tabnas, Value};

/// `top` pushes `mid`; `mid` matches one token into its own node and then
/// replaces itself with `tail`, which matches the second token into a node of
/// its own. `top`'s `@capture$` must merge `mid`, the rule it pushed.
const CHAIN: &str = r##"{
  "options": { "rule": { "start": "top" } },
  "rule": {
    "top": {
      "open": [ { "p": "mid", "a": "@node$",
        "k": { "node$": { "init": true, "rule": "top", "kind": "user", "nterms": 0 } } } ],
      "close": [ { "a": "@capture$",
        "k": { "capture$": { "rule": "top", "kind": "user" } } } ]
    },
    "mid": {
      "open": [ { "s": "#TX", "a": "@node$",
        "k": { "node$": { "init": true, "rule": "mid", "kind": "user", "nterms": 1 } } } ],
      "close": [ { "r": "tail" } ]
    },
    "tail": {
      "open": [ { "s": "#TX", "a": "@node$",
        "k": { "node$": { "init": true, "rule": "tail", "kind": "user", "nterms": 1 } } } ],
      "close": [ {} ]
    }
  }
}"##;

fn node(rule: &str, src: &str, kids: Vec<Value>) -> Value {
    Value::object(
        [
            ("rule".into(), Value::String(rule.into())),
            ("src".into(), Value::String(src.into())),
            ("kids".into(), Value::array(kids)),
        ]
        .into_iter()
        .collect(),
    )
}

#[test]
fn capture_sees_the_pushed_rule_not_the_tail_of_its_replacement_chain() {
    let mut parser = Tabnas::new();
    parser.grammar_json(CHAIN).expect("chain grammar installs");
    let value = parser.parse("a b").expect("chain grammar parses");
    // Byte-identical to TypeScript and Go on the same serialized grammar:
    // {"rule":"top","src":"a","kids":[{"rule":"mid","src":"a","kids":[]}]}
    assert_eq!(
        value,
        node("top", "a", vec![node("mid", "a", vec![])]),
        "top captured the replacement chain's tail, not the rule it pushed"
    );
}
