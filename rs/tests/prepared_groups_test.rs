// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Whether an alternate survives `rule.include` and `rule.exclude` is
//! worked out when the rule is installed, and `Parser::options` is public.
//!
//! That makes it the same shape as the `number.exclude` pattern cache and
//! the `expected_tins` finding on #177: a cache derived from a public
//! mutable field is a cache with a second writer. The parser records the
//! two strings the prepared answers were computed against, and a step
//! that finds the live options carrying anything else derives the answer
//! per alternate as it always did.
//!
//! These tests use the LOW-LEVEL `Parser` on purpose. `Tabnas` folds
//! `options.generation()` into its grammar generation and rebuilds the
//! whole parser when either changes, so it can never see a stale prepared
//! answer and a test written against it passes whether the guard exists
//! or not. `Parser::options` is a public field with no such rebuild, and
//! it is the surface `exclude_follows_options_replaced_directly_on_the_parser`
//! covers for the same reason.

use std::sync::{Arc, Mutex};
use tabnas::{AltSpec, Options, Parser, RuleSpec, TIN_NR};

type Seen = Arc<Mutex<Vec<&'static str>>>;

fn options_with(include: &str, exclude: &str) -> Arc<Options> {
    let mut options = Options::default();
    options.rule.start = "top".into();
    options.rule.include = include.to_string();
    options.rule.exclude = exclude.to_string();
    Arc::new(options)
}

/// Two alternates that match the same token and differ only in the group
/// they declare, so which one runs is entirely the group filter's answer.
fn top_rule(seen: &Seen) -> RuleSpec {
    let mut top = RuleSpec::new("top");
    for (group, label) in [("first", "first"), ("second", "second")] {
        let mut alt = AltSpec {
            s: vec![vec![TIN_NR]],
            g: group.to_string(),
            ..Default::default()
        };
        let seen = seen.clone();
        alt.add_action(move |_rule, _context| seen.lock().unwrap().push(label));
        top.open.push(alt);
    }
    top
}

fn ran(parser: &Parser, seen: &Seen) -> Vec<&'static str> {
    seen.lock().unwrap().clear();
    parser.parse("1").expect("should parse");
    let out = seen.lock().unwrap().clone();
    out
}

#[test]
fn an_include_list_replaced_directly_on_the_parser_still_decides() {
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Parser::from_shared(options_with("first", ""));
    parser.add_rule(top_rule(&seen));
    assert_eq!(ran(&parser, &seen), ["first"], "as installed");

    // No rule is reinstalled, so nothing rebuilds the prepared answers.
    // Without the guard the step keeps reading the answer prepared for
    // "first" and the wrong alternate runs.
    parser.options = options_with("second", "");
    assert_eq!(
        ran(&parser, &seen),
        ["second"],
        "the live include list decides, not the one installed with"
    );

    // Back again, so this cannot pass by latching in one direction.
    parser.options = options_with("first", "");
    assert_eq!(ran(&parser, &seen), ["first"], "and back");
}

#[test]
fn an_exclude_list_added_directly_on_the_parser_still_decides() {
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Parser::from_shared(options_with("first,second", ""));
    parser.add_rule(top_rule(&seen));
    assert_eq!(
        ran(&parser, &seen),
        ["first"],
        "both included, so the first alternate wins"
    );

    // Excluding the winner has to hand the match to the other alternate.
    // The include list is unchanged, so only the exclude half of the
    // recorded pair can catch this.
    parser.options = options_with("first,second", "first");
    assert_eq!(
        ran(&parser, &seen),
        ["second"],
        "an exclude list added after install must be read"
    );
}

#[test]
fn reinstalling_the_rule_rebuilds_the_prepared_answers() {
    let seen: Seen = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Parser::from_shared(options_with("first", ""));
    parser.add_rule(top_rule(&seen));
    assert_eq!(ran(&parser, &seen), ["first"]);

    // Changing the options AND reinstalling takes the fast path rather
    // than the fallback, because the recorded strings match again. The
    // answer has to be the same either way.
    parser.options = options_with("second", "");
    parser.add_rule(top_rule(&seen));
    assert_eq!(
        ran(&parser, &seen),
        ["second"],
        "the prepared answers are rebuilt with the rule"
    );
}
