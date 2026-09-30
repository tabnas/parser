//! A bad token handed to the parser, and the error a recovering parse ends
//! on, as the canonical engine treats them.
//!
//! TypeScript raises a `#BD` token the moment its fetch in `parse_alts`
//! (ts/src/rules.ts) is handed one: with the token's own code at the
//! token's own position, whichever position of an alternate asked for it.
//! Under recovery it records it instead, coalesces a run of them into one
//! error, and steps the lexer past it; under relexing it leaves it in the
//! lookahead for an alternate to re-cut. This port treated only the
//! lexer's OWN faults that way. A bad token a custom matcher returned
//! (`Lexer::bad`, `Lexer::bad_span`) was buffered like a good one, so it
//! failed every alternate it met and the error named the first token of
//! the lookahead instead; under recovery nothing stepped past it.
//!
//! A recovering parse that gives up lists each error once. This port
//! listed the error it gave up on a second time, without its recovery
//! metadata, whenever the copy it had recorded carried some, or had been
//! dropped as a cascade.
//!
//! `test/spec/bad-token.tsv` holds the rows all three runtimes agree on;
//! this file adds what only the API shows, and the two cases where Go
//! parts from TypeScript (relexing with recovery; a cascade at the
//! `maxRecoveries` cap), whose TypeScript answers were measured.

use std::sync::{Arc, Mutex};

use tabnas::{
    ImperativeLexMatcher, LexMatcher, ParseRecovery, RecoveredAt, Tabnas, TabnasError, Value,
    TIN_BD, TIN_CA,
};

/// At `?`, a bad token `custom_bad` over the `?`; at `%`, a bad token
/// `custom_unterminated` over the rest of the source. The cursor is not
/// moved, as `lex.bad` in TypeScript does not move it.
fn with_bad_matcher(mut parser: Tabnas) -> Tabnas {
    let matcher: ImperativeLexMatcher = Arc::new(|lexer, _rule, _context| {
        let at = lexer.point().site.pos;
        let rest = lexer.remaining();
        if rest.starts_with('?') {
            Some(lexer.bad_span("custom_bad", at, at + 1))
        } else if rest.starts_with('%') {
            let end = at + rest.chars().count();
            Some(lexer.bad_span("custom_unterminated", at, end))
        } else {
            None
        }
    });
    parser.options.lex.matchers.insert(
        "bad".into(),
        LexMatcher {
            name: "bad".into(),
            order: 1.5e6,
            matcher: None,
            imperative: Some(matcher),
            factory: None,
        },
    );
    parser
}

fn grammar(spec: &str) -> Tabnas {
    let mut parser = Tabnas::new();
    parser.grammar_json(spec).expect("grammar installs");
    parser
}

/// The serialized strict-JSON grammar every runtime loads.
fn json() -> Tabnas {
    let mut parser = grammar(include_str!("../../ts/test/json-builder.fixture.json"));
    parser.options.rule.start = "val".into();
    parser
}

fn recovering(mut parser: Tabnas) -> Tabnas {
    parser.options.parse.recover.enabled = true;
    parser
}

fn relexing(mut parser: Tabnas) -> Tabnas {
    parser.options.lex.relex = true;
    parser
}

fn value(json: &str) -> Option<Value> {
    Some(Value::from_json(&serde_json::from_str(json).unwrap()))
}

fn summary(error: &TabnasError) -> (&str, usize, usize, Option<RecoveredAt>) {
    (
        error.code.as_str(),
        error.row,
        error.col,
        error.recovered.clone(),
    )
}

fn summaries(out: &ParseRecovery) -> Vec<(&str, usize, usize, Option<RecoveredAt>)> {
    out.errors.iter().map(summary).collect()
}

fn bad_run(skipped: usize) -> Option<RecoveredAt> {
    Some(RecoveredAt {
        skipped,
        sync: None,
        bad: true,
    })
}

const TWO_NUMBERS: &str = r##"{"options":{"rule":{"start":"top"}},
  "rule":{"top":{"open":[{"s":"#NR #NR"}]}}}"##;

const ONE_NUMBER: &str = r##"{"options":{"rule":{"start":"top"}},
  "rule":{"top":{"open":[{"s":"#NR","a":"@value$"}]}}}"##;

const NEST: &str = r##"{"options":{"rule":{"start":"top"},"fixed":{"token":{"#LB":"<","#RB":">"}}},
  "rule":{"top":{"open":[{"s":"#LB","p":"body"}],"close":[{"s":"#RB"}]},
          "body":{"open":[{"s":"#NR"}],"close":[{"s":"#RB","b":1},{"s":"#ZZ","b":1}]}}}"##;

#[test]
fn a_bad_token_behind_a_good_one_is_raised_where_it_is_fetched() {
    // The second token of an alternate, the shape tabnas-css met with an
    // unclosed comment behind a property: the error is the bad token's own,
    // at the bad token, not `unexpected` at the good token in front of it.
    let parser = with_bad_matcher(grammar(TWO_NUMBERS));
    let error = parser.parse("1 ?").expect_err("fails");
    assert_eq!(
        ("custom_bad", 1, 3),
        (error.code.as_str(), error.row, error.col)
    );
    assert_eq!(
        ("#BD", "?"),
        (error.token.name.as_str(), error.token.src.as_str())
    );
    assert_eq!(1, error.len);
    assert_eq!("top", error.rule);

    let error = with_bad_matcher(json())
        .parse(r#"{"a"?:1}"#)
        .expect_err("fails");
    assert_eq!(
        ("custom_bad", 1, 5),
        (error.code.as_str(), error.row, error.col)
    );
}

#[test]
fn recovery_records_a_custom_bad_token_and_steps_past_it() {
    let parser = recovering(with_bad_matcher(json()));

    let out = parser.parse_recover("[1,?,2]");
    assert_eq!(value("[1,2]"), out.value);
    assert_eq!(vec![("custom_bad", 1, 4, bad_run(1))], summaries(&out));

    // A run of bad tokens is one error, whose metadata counts the run.
    let out = parser.parse_recover("[1,??,2]");
    assert_eq!(value("[1,2]"), out.value);
    assert_eq!(vec![("custom_bad", 1, 4, bad_run(2))], summaries(&out));

    // One that runs to the end of the source is stepped past whole.
    let out = parser.parse_recover("[1,% 2]");
    assert_eq!(value("[1]"), out.value);
    assert_eq!(
        vec![("custom_unterminated", 1, 4, bad_run(1))],
        summaries(&out)
    );

    // The parse goes on past it, and meets what follows.
    let out = parser.parse_recover("[1]?2");
    assert_eq!(value("[1]"), out.value);
    assert_eq!(
        vec![("custom_bad", 1, 4, bad_run(1)), ("unexpected", 1, 5, None)],
        summaries(&out)
    );
}

#[test]
fn a_run_past_its_caps_is_listed_once_with_its_metadata() {
    let mut parser = recovering(with_bad_matcher(json()));
    parser.options.parse.recover.max_skip = 1;
    let out = parser.parse_recover("[1,???,2]");
    assert_eq!(value("[1]"), out.value);
    assert_eq!(vec![("custom_bad", 1, 4, bad_run(2))], summaries(&out));

    let mut parser = recovering(with_bad_matcher(json()));
    parser.options.parse.recover.max_recoveries = 0;
    let out = parser.parse_recover("[1,??,2]");
    assert_eq!(value("[1]"), out.value);
    assert_eq!(vec![("custom_bad", 1, 4, bad_run(1))], summaries(&out));
}

#[test]
fn relexing_leaves_a_custom_bad_token_for_the_alternates() {
    // Nothing here re-cuts it, so fail-fast names the first token of the
    // lookahead, as the canonical engine does, and as this port did before.
    let error = relexing(with_bad_matcher(json()))
        .parse(r#"{"a"?:1}"#)
        .expect_err("fails");
    assert_eq!(
        ("unexpected", 1, 2),
        (error.code.as_str(), error.row, error.col)
    );
}

#[test]
fn relexing_and_recovery_together_recover_from_a_custom_bad_token() {
    // The token fails the alternates and recovery skips it to the next sync
    // token, stepping the lexer past it. It is listed once. TypeScript's
    // answers; Go lists each of these errors twice, once unrecovered.
    let parser = recovering(relexing(with_bad_matcher(json())));
    let skipped = |skipped| {
        Some(RecoveredAt {
            skipped,
            sync: Some(TIN_CA),
            bad: false,
        })
    };

    let out = parser.parse_recover("[1,?,2]");
    assert_eq!(value("[1,2]"), out.value);
    assert_eq!(vec![("custom_bad", 1, 4, skipped(1))], summaries(&out));

    let out = parser.parse_recover("[1,??,2]");
    assert_eq!(value("[1,2]"), out.value);
    assert_eq!(vec![("custom_bad", 1, 4, skipped(2))], summaries(&out));

    let out = parser.parse_recover("[1,? 2,3]");
    assert_eq!(value("[1,3]"), out.value);
    assert_eq!(vec![("custom_bad", 1, 4, skipped(2))], summaries(&out));
}

#[test]
fn the_trailing_check_raises_a_bad_token_without_recording_it() {
    // A grammar that stops reading after one number: whatever follows meets
    // only the check for trailing content, which raises a bad token with
    // its own code, recovering or not, and does not step past it.
    let parser = recovering(with_bad_matcher(grammar(ONE_NUMBER)));

    let out = parser.parse_recover("1 ? 2");
    assert_eq!(value("1"), out.value);
    assert_eq!(vec![("custom_bad", 1, 3, None)], summaries(&out));

    // The lexer's own faults the same way: this one used to be recorded
    // and skipped, and the next line reported as well.
    let out = parser.parse_recover("1 \"x\n2");
    assert_eq!(value("1"), out.value);
    assert_eq!(vec![("unprintable", 1, 5, None)], summaries(&out));
}

#[test]
fn a_fault_inside_a_string_resumes_past_the_next_row() {
    // The canonical skip moves past the bad token and then past the next
    // row character after it, so the line after an unprintable newline is
    // skipped whole rather than lexed from the middle of a string.
    let out = recovering(json()).parse_recover("{\"a\":1,\"b\n\":[truex,null]}");
    assert_eq!(value(r#"{"a":1}"#), out.value);
    assert_eq!(vec![("unprintable", 1, 10, bad_run(1))], summaries(&out));
}

#[test]
fn lex_subscribers_see_the_lexers_own_faults() {
    // Every token the canonical `lex.next` returns reaches the lex
    // subscribers before the parser decides what to do with it, a fault's
    // bad token included, recovering or not.
    let seen = Arc::new(Mutex::new(Vec::new()));
    let mut parser = json();
    let log = seen.clone();
    parser.subscribe_lex(move |token, _rule, _context| {
        if token.tin == TIN_BD {
            log.lock().unwrap().push(token.why.to_string());
        }
    });
    let error = parser.parse("[\"abc").expect_err("fails");
    assert_eq!("unterminated_string", error.code);
    assert_eq!(
        vec!["unterminated_string".to_string()],
        *seen.lock().unwrap()
    );

    seen.lock().unwrap().clear();
    let error = with_bad_matcher(parser).parse("[?]").expect_err("fails");
    assert_eq!("custom_bad", error.code);
    assert_eq!(vec!["custom_bad".to_string()], *seen.lock().unwrap());
}

#[test]
fn a_recovering_parse_that_gives_up_lists_each_error_once() {
    let parser = recovering(grammar(NEST));
    let resumed_at_the_end = Some(RecoveredAt {
        skipped: 0,
        sync: Some(tabnas::TIN_ZZ),
        bad: false,
    });

    // Recovery reaches the end of the source, resumes, and fails there
    // again with nothing consumed: the second failure is a cascade of the
    // first, dropped, and the parse gives up on the error it recorded.
    let out = parser.parse_recover("<");
    assert_eq!(
        vec![("unexpected", 1, 2, resumed_at_the_end.clone())],
        summaries(&out)
    );
    let out = parser.parse_recover("<1 2");
    assert_eq!(1, out.errors.len(), "{:?}", summaries(&out));

    // A failure that is not a cascade is a second error, and is listed.
    let mut parser = recovering(grammar(NEST));
    parser.options.parse.recover.suppress = 0;
    let out = parser.parse_recover("<");
    assert_eq!(
        vec![
            ("unexpected", 1, 2, resumed_at_the_end),
            ("unexpected", 1, 2, None)
        ],
        summaries(&out)
    );
}

#[test]
fn a_cascade_at_the_recovery_cap_still_recovers() {
    // The cap is read after the error is recorded and, for a cascade,
    // dropped again: a cascade does not grow the list, so it does not stop
    // the parse at the cap. TypeScript's answer; Go reads the cap before
    // recording, and gives up here with `[1]`.
    let mut parser = recovering(json());
    parser.options.parse.recover.max_recoveries = 1;
    let out = parser.parse_recover("[1,,,2]");
    assert_eq!(value("[1,2]"), out.value);
    assert_eq!(1, out.errors.len(), "{:?}", summaries(&out));
}
