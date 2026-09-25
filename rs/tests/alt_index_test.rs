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
use tabnas::{Tabnas, Value, TIN_NR, TIN_ST, TIN_TX};

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

#[test]
fn a_slot_set_by_hand_after_the_rule_is_not_overwritten_by_its_names() {
    // The names on a slot late-bind a token set, but an explicit edit of
    // `s` after the grammar was installed is the caller's decision and
    // has to survive every rebuild of the parser.
    let mut tabnas = Tabnas::new();
    tabnas.grammar_json(RULES).unwrap();
    tabnas.rules.get_mut("top").expect("top").open[0].s[0] = vec![TIN_NR];
    assert_eq!(tabnas.parse("1").unwrap(), Value::Number(1.0));
    assert!(
        tabnas.parse("a").is_err(),
        "the edit narrowed the slot to #NR"
    );
    // An options change rebuilds the parser: the edit still stands.
    tabnas.set_token_set("KEY", vec![TIN_TX]);
    assert_eq!(tabnas.parse("1").unwrap(), Value::Number(1.0));
    assert!(tabnas.parse("a").is_err());
}

#[test]
fn a_merged_slot_still_follows_a_set_overridden_on_the_merged_instance() {
    // The merge re-registers every token in the merged instance's own
    // token space, so a set member's tin can differ from the source's
    // (`right` registers two tokens of its own first). The merged slot's
    // baseline has to be the remapped tins: left at the source's, the
    // slot reads as set by hand, and a set overridden on the merged
    // instance never reaches it.
    let mut left = Tabnas::with_options(tabnas::Options {
        tag: "Z".into(),
        ..Default::default()
    });
    left.grammar_json(
        r##"{"options":{"rule":{"start":"top"},"fixed":{"token":{"#ZT":"@"}},
             "tokenSet":{"KEY":["#TX","#ZT"]}},
             "rule":{"top":{"open":[{"s":"#KEY","a":"@value$"}],"close":[{"s":"#ZZ"}]}}}"##,
    )
    .unwrap();
    let mut right = Tabnas::with_options(tabnas::Options {
        tag: "A".into(),
        ..Default::default()
    });
    right
        .grammar_json(r##"{"options":{"fixed":{"token":{"#BA":"%","#BB":"^"}}}}"##)
        .unwrap();
    let mut merged = right.merge(&left).unwrap();
    assert_ne!(
        left.options.token("#ZT"),
        merged.options.token("#ZT"),
        "the merge has to move the tin for this test to mean anything"
    );
    assert!(
        merged.parse("@").is_ok(),
        "before the override an @ is a key"
    );
    merged.set_token_set("KEY", vec![TIN_TX]);
    assert_eq!(merged.parse("a").unwrap(), Value::String("a".into()));
    assert!(
        merged.parse("@").is_err(),
        "the merged slot did not follow the set overridden on the merged instance"
    );
}

#[test]
fn a_rejecting_condition_that_retags_the_first_token_selects_again() {
    // A condition may change the buffered token's tin and reject. The full
    // scan then tests every later alternate against the token as it is
    // now, so the candidates have to follow it: the second `#A` candidate
    // retags `a` as `#B`, the third can no longer take it, and the `#B`
    // alternate after them does.
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut tabnas = Tabnas::new();
    recorder(&mut tabnas, &seen, &["first", "second", "third", "b"]);
    tabnas
        .grammar_json(&format!(
            r##"{{"options":{{"rule":{{"start":"top"}},{FIXED}}}}}"##
        ))
        .unwrap();
    let tin_b = tabnas.options.token("#B").expect("#B");
    tabnas.alt_condition("@never", |_, _| false);
    tabnas.alt_condition("@retag", move |_, context| {
        context.t[0].tin = tin_b;
        false
    });
    tabnas
        .grammar_json(
            r##"{"rule":{"top":{
              "open":[
                {"s":"#A","c":"@never","a":["@first"]},
                {"s":"#A","c":"@retag","a":["@second"]},
                {"s":"#A","a":["@third"]},
                {"s":"#B","a":["@b"]}
              ],
              "close":[{"s":"#ZZ"}]
            }}}"##,
        )
        .unwrap();
    assert_eq!(ran(&tabnas, &seen, "a"), ["b"]);
    assert_eq!(ran(&tabnas, &seen, "b"), ["b"]);
}

#[test]
fn a_slot_set_by_hand_before_a_merge_keeps_its_edit_on_the_merged_instance() {
    // The edit is the caller's decision on the source instance. The merge
    // carries the slot's tins, and must not carry the names that would
    // resolve over them when the merged instance builds its parser.
    let mut left = Tabnas::with_options(tabnas::Options {
        tag: "Z".into(),
        ..Default::default()
    });
    left.grammar_json(RULES).unwrap();
    left.rules.get_mut("top").expect("top").open[0].s[0] = vec![TIN_NR];
    let right = Tabnas::with_options(tabnas::Options {
        tag: "A".into(),
        ..Default::default()
    });
    let mut merged = right.merge(&left).unwrap();
    assert_eq!(merged.parse("1").unwrap(), Value::Number(1.0));
    assert!(
        merged.parse("a").is_err(),
        "the merged instance resolved the names over the edit"
    );
    merged.set_token_set("KEY", vec![TIN_TX]);
    assert_eq!(merged.parse("1").unwrap(), Value::Number(1.0));
    assert!(merged.parse("a").is_err());
}

#[test]
fn a_merge_keeps_alternates_whose_different_sets_resolve_alike() {
    // Two alternates that differ only in the set they name are the same
    // alternate while the sets agree, and different ones once either set
    // is overridden on the merged instance. The merge keeps both, or the
    // override would reach only the survivor.
    fn side(tag: &str, set: &str) -> Tabnas {
        let mut tabnas = Tabnas::with_options(tabnas::Options {
            tag: tag.into(),
            ..Default::default()
        });
        tabnas
            .grammar_json(&format!(
                r##"{{"options":{{"rule":{{"start":"top"}},"tokenSet":{{"{set}":["#NR"]}}}},
                     "rule":{{"top":{{"open":[{{"s":"#{set}","a":"@value$"}}],
                                      "close":[{{"s":"#ZZ"}}]}}}}}}"##
            ))
            .unwrap();
        tabnas
    }
    let mut merged = side("A", "ALPHA").merge(&side("B", "BETA")).unwrap();
    assert_eq!(merged.rules["top"].open.len(), 2);
    assert_eq!(merged.parse("1").unwrap(), Value::Number(1.0));
    merged.set_token_set("BETA", vec![TIN_ST]);
    assert_eq!(
        merged.parse("1").unwrap(),
        Value::Number(1.0),
        "ALPHA still takes a number"
    );
    assert_eq!(
        merged.parse(r#""s""#).unwrap(),
        Value::String("s".into()),
        "BETA takes a string once overridden"
    );
}
