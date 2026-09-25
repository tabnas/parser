// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

//! The first-token index (`AltIndex` in `parser.rs`) must be invisible: a
//! rule with many alternates tries only those that can take the first
//! token, and everything an author can observe stays as it was.
//! TypeScript pins the same cases in `ts/test/alt-index.test.js` and Go
//! in `go/alt_index_test.go`.
//!
//! The last test is tabnas/parser#217: a slot naming a token set keeps
//! its names and is resolved again when the parser is rebuilt, so a set
//! overridden after the alternate was installed reaches it.

use std::sync::{Arc, Mutex};
use tabnas::{Tabnas, Value, TIN_TX};

type Seen = Arc<Mutex<Vec<String>>>;

fn recorder(tabnas: &mut Tabnas, seen: &Seen, names: &[&str]) {
    for name in names {
        let seen = seen.clone();
        let label = name.to_string();
        tabnas.action(format!("@{name}"), move |_rule| {
            seen.lock().unwrap().push(label.clone());
        });
    }
}

fn ran(tabnas: &Tabnas, seen: &Seen, src: &str) -> Vec<String> {
    seen.lock().unwrap().clear();
    tabnas.parse(src).unwrap_or_else(|e| panic!("{src:?}: {e}"));
    let out = seen.lock().unwrap().clone();
    out
}

const FIXED: &str = r##""fixed":{"token":{"#A":"a","#B":"b","#C":"c"}}"##;

#[test]
fn first_match_wins_is_kept_among_alternates_sharing_a_first_token() {
    // The excluded alternate is a candidate the step must skip and go on
    // from, exactly as a scan of every alternate would.
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    recorder(&mut tabnas, &seen, &["first", "b", "second", "third"]);
    tabnas
        .grammar_json(&format!(
            r##"{{
              "options":{{"rule":{{"start":"top","exclude":"skip"}},{FIXED}}},
              "rule":{{"top":{{
                "open":[
                  {{"s":"#A","g":"skip","a":["@first"]}},
                  {{"s":"#B","a":["@b"]}},
                  {{"s":"#A","a":["@second"]}},
                  {{"s":"#A","a":["@third"]}}
                ],
                "close":[{{"s":"#ZZ"}}]
              }}}}
            }}"##
        ))
        .unwrap();
    assert_eq!(ran(&tabnas, &seen, "a"), ["second"]);
    assert_eq!(ran(&tabnas, &seen, "b"), ["b"]);
    assert!(tabnas.parse("c").is_err(), "no alternate takes c");
}

#[test]
fn wildcard_and_empty_alternates_stay_in_place() {
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    recorder(&mut tabnas, &seen, &["a", "any", "never"]);
    tabnas
        .grammar_json(&format!(
            r##"{{
              "options":{{"rule":{{"start":"top"}},{FIXED}}},
              "rule":{{"top":{{
                "open":[
                  {{"s":"#A","a":["@a"]}},
                  {{"s":"#AA","a":["@any"]}},
                  {{"s":"#B","a":["@never"]}}
                ],
                "close":[{{"s":"#ZZ"}}]
              }}}}
            }}"##
        ))
        .unwrap();
    assert_eq!(ran(&tabnas, &seen, "a"), ["a"]);
    assert_eq!(ran(&tabnas, &seen, "b"), ["any"]);
    assert_eq!(ran(&tabnas, &seen, "c"), ["any"]);

    // An empty sequence before the token alternates matches without
    // consuming, exactly as it did: it is a candidate for every tin.
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    recorder(&mut tabnas, &seen, &["item", "direct"]);
    tabnas
        .grammar_json(&format!(
            r##"{{
              "options":{{"rule":{{"start":"top"}},{FIXED}}},
              "rule":{{
                "top":{{
                  "open":[{{"p":"item"}},{{"s":"#A","a":["@direct"]}}],
                  "close":[{{"s":"#ZZ"}}]
                }},
                "item":{{
                  "open":[{{"s":"#A","a":["@item"]}}],
                  "close":[{{"s":"#ZZ"}}]
                }}
              }}
            }}"##
        ))
        .unwrap();
    assert_eq!(ran(&tabnas, &seen, "a"), ["item"]);
}

#[test]
fn many_alternates_dispatch_to_the_right_one() {
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    let names: Vec<String> = (0..300).map(|i| format!("k{i}")).collect();
    let name_refs: Vec<&str> = names.iter().map(String::as_str).collect();
    recorder(&mut tabnas, &seen, &name_refs);
    let tokens = names
        .iter()
        .map(|n| format!(r##""#{}":"{n}""##, n.to_uppercase()))
        .collect::<Vec<_>>()
        .join(",");
    let alts = names
        .iter()
        .map(|n| format!(r##"{{"s":"#{}","a":["@{n}"]}}"##, n.to_uppercase()))
        .collect::<Vec<_>>()
        .join(",");
    tabnas
        .grammar_json(&format!(
            r##"{{
              "options":{{"rule":{{"start":"top"}},"fixed":{{"token":{{{tokens}}}}}}},
              "rule":{{"top":{{"open":[{alts}],"close":[{{"s":"#ZZ"}}]}}}}
            }}"##
        ))
        .unwrap();
    for n in ["k0", "k1", "k150", "k299"] {
        assert_eq!(ran(&tabnas, &seen, n), [n]);
    }
    assert!(tabnas.parse("a").is_err());
}

const RULES: &str = r##"{"options":{"rule":{"start":"top"}},
  "rule":{"top":{"open":[{"s":"#KEY","a":"@value$"}],"close":[{"s":"#ZZ"}]}}}"##;
const NARROW: &str = r##"{"options":{"tokenSet":{"KEY":["#TX",null,null,null]}}}"##;

fn only_text_is_a_key(tabnas: &Tabnas, how: &str) {
    assert_eq!(
        tabnas
            .parse("a")
            .unwrap_or_else(|e| panic!("{how}: a: {e}")),
        Value::String("a".into()),
        "{how}"
    );
    for src in ["1", r#""s""#, "true"] {
        assert!(
            tabnas.parse(src).is_err(),
            "{how}: {src} accepted after KEY was narrowed to #TX"
        );
    }
}

#[test]
fn a_token_set_overridden_after_the_rule_reaches_the_rule() {
    // tabnas/parser#217, in the form all three runtimes pin.
    let mut tabnas = Tabnas::new();
    tabnas.grammar_json(RULES).unwrap();
    tabnas.grammar_json(NARROW).unwrap();
    only_text_is_a_key(&tabnas, "narrowed by a later grammar");

    // Through the options API as well.
    let mut tabnas = Tabnas::new();
    tabnas.grammar_json(RULES).unwrap();
    tabnas.set_token_set("KEY", vec![TIN_TX]);
    only_text_is_a_key(&tabnas, "narrowed by set_token_set");

    // Control: narrowed before the rule was installed.
    let mut tabnas = Tabnas::new();
    tabnas.grammar_json(NARROW).unwrap();
    tabnas.grammar_json(RULES).unwrap();
    only_text_is_a_key(&tabnas, "narrowed first");

    // And widened again: the binding is live in both directions.
    let mut tabnas = Tabnas::new();
    tabnas.grammar_json(RULES).unwrap();
    tabnas.grammar_json(NARROW).unwrap();
    tabnas
        .grammar_json(r##"{"options":{"tokenSet":{"KEY":["#TX","#NR","#ST","#VL"]}}}"##)
        .unwrap();
    assert_eq!(tabnas.parse("1").unwrap(), Value::Number(1.0));
}
