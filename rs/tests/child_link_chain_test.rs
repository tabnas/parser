/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

//! `rule.child` is linked at the PUSH and never relinked.
//!
//! TypeScript sets `rule.child` in the push arm (`ts/src/rules.ts:665`) and
//! nowhere else; Go does the same (`go/rule.go:1280`). A child that REPLACES
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

/// The node was only half of the link. `rule.child.name` came from the tail
/// of the chain while `rule.child.node` came from the head, a combination
/// neither canonical runtime can produce.
///
/// `top` pushes `child`, which pushes `leaf` and then replaces itself twice.
/// Every path below is read by a declarative condition on `top`'s close pass
/// and answers the same in all three ports; the single alternate carrying
/// them all tags the node `ALL-MATCH`, and a miss on any one of them falls
/// through to `MISMATCH`. Measured on this tree: TypeScript, Go and Rust all
/// answer `ALL-MATCH`, where Rust answered `MISMATCH` before the link was
/// carried on the pushed rule.
///
/// `child.next.next` and beyond are NOT here: they are still Rust-only, and
/// they are registered in `test/spec/divergent.tsv` under "Forward traversal
/// of a replacement chain in Rust" rather than pinned as agreement.
const LINKS: &str = r##"{
  "options": {
    "rule": { "start": "top" },
    "fixed": { "token": { "Ta": "a", "Tb": "b", "Tx": "x",
                          "Tc": "c", "Td": "d", "Te": "e" } }
  },
  "rule": {
    "top": {
      "open": [ { "s": "Ta", "p": "child" } ],
      "close": [
        { "s": "Te",
          "c": { "child.name": "child",
                 "child.parent.name": "top",
                 "child.child.name": "leaf",
                 "child.child.parent.name": "child",
                 "child.next.name": "child2",
                 "next.name": "child",
                 "next.next.name": "child2" },
          "a": "@node$",
          "k": { "node$": { "init": true, "rule": "ALL-MATCH",
                            "kind": "user", "nterms": 0 } } },
        { "s": "Te", "a": "@node$",
          "k": { "node$": { "init": true, "rule": "MISMATCH",
                            "kind": "user", "nterms": 0 } } }
      ]
    },
    "child": {
      "open": [ { "s": "Tb", "p": "leaf" } ],
      "close": [ { "r": "child2" } ]
    },
    "leaf":   { "open": [ { "s": "Tx" } ], "close": [ {} ] },
    "child2": { "open": [ { "s": "Tc", "r": "child3" } ] },
    "child3": { "open": [ { "s": "Td" } ], "close": [ {} ] }
  }
}"##;

#[test]
fn the_whole_child_link_names_the_pushed_rule_not_just_its_node() {
    let mut parser = Tabnas::new();
    parser.grammar_json(LINKS).expect("links grammar installs");
    let value = parser.parse("abxcde").expect("links grammar parses");
    assert_eq!(
        value,
        node("ALL-MATCH", "", vec![]),
        "a rule-graph path on top's close pass disagrees with TypeScript and Go"
    );
}
