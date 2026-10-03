//! A valid input parses in time linear in its length with recovery on and
//! with negotiated lexing (`lex.relex`) on, as it does with both off.
//!
//! Neither is a cost TypeScript has, and both were quadratic here:
//!
//! - Recovery named its partial value at every step by taking it: a clone
//!   of the value built so far, walked for `undefined`, and held until the
//!   next step, so the grammar's next append copied the container it went
//!   into. 14 KB of css took 8 s in a release build.
//! - A negotiated cut the lexer rejects ends in an error nobody sees, and
//!   each one copied the whole source into it, three times over.
//!
//! Ten times the input takes about ten times as long. Quadratic work takes
//! a hundred; the bound is generous so a busy machine cannot fail it. In
//! the debug build the suite runs in, the ratios below were 10 to 13, and
//! before the repairs 71 (recovery, the flat array) and 91 (relex).

use std::fmt::Write as _;
use std::time::{Duration, Instant};
use tabnas::{RecoverOptions, Tabnas, Value};

/// The fastest of a few parses of `src`, to keep a loaded machine's stalls
/// and a coarse clock out of the comparison. Every one must succeed.
fn fastest(parser: &Tabnas, src: &str) -> Duration {
    (0..3)
        .map(|_| {
            let start = Instant::now();
            let parsed = parser.parse_recover(src);
            let elapsed = start.elapsed();
            assert!(
                parsed.errors.is_empty() && parsed.fatal.is_none() && parsed.value.is_some(),
                "{:?}",
                parsed.errors.first().or(parsed.fatal.as_ref())
            );
            elapsed
        })
        .min()
        .expect("three runs")
}

fn assert_linear(what: &str, parser: &Tabnas, document: impl Fn(usize) -> String) {
    let small = fastest(parser, &document(1_000));
    let large = fastest(parser, &document(10_000));
    let ratio = large.as_secs_f64() / small.as_secs_f64().max(1e-6);
    assert!(
        ratio < 30.0,
        "{what}: 1,000 items took {small:?} and 10,000 took {large:?}: {ratio:.1}x"
    );
}

fn recovering_json() -> Tabnas {
    let mut parser = Tabnas::make_json();
    parser.options.parse.recover = RecoverOptions {
        enabled: true,
        ..Default::default()
    };
    parser
}

type Document = fn(usize) -> String;

const DOCUMENTS: [(&str, Document); 3] = [
    ("a flat array", |n| format!("[{}1]", "1,".repeat(n))),
    ("an array of arrays", |n| {
        format!("[{}[1]]", "[1],".repeat(n))
    }),
    ("an object", |n| {
        let pairs = (0..n).fold(String::new(), |mut pairs, i| {
            let _ = write!(pairs, "\"k{i}\":{{\"v\":{i}}},");
            pairs
        });
        format!("{{{pairs}\"z\":1}}")
    }),
];

#[test]
fn a_recovering_parse_of_a_valid_input_is_linear() {
    let parser = recovering_json();
    for (what, document) in DOCUMENTS {
        assert_linear(what, &parser, document);
    }
}

/// What the recovering parse returns is still the value the plain parse
/// builds, and a failure still returns the partial value the last step
/// named, container and all.
#[test]
fn a_recovering_parse_returns_the_same_values() {
    let parser = recovering_json();
    let plain = Tabnas::make_json();
    for (what, document) in DOCUMENTS {
        let src = document(100);
        assert_eq!(
            parser.parse_recover(&src).value,
            Some(plain.parse(&src).expect("valid")),
            "{what}"
        );
        // Cut short, recovery gives up at the end of the source and the
        // value is the outer container as far as it got.
        let cut = &src[..src.len() / 2];
        let recovered = parser.parse_recover(cut);
        assert!(!recovered.errors.is_empty(), "{what}: {cut}");
        match recovered.value {
            Some(Value::Array(items)) => assert!(!items.is_empty(), "{what}"),
            Some(Value::Object(entries)) => assert!(!entries.is_empty(), "{what}"),
            other => panic!("{what}: {other:?}"),
        }
    }
}

/// The letters of the tokens every item is offered to before `#A` takes it.
const REJECTED: [&str; 2] = ["B", "C"];

/// Every `a` is offered first to two alternates that want `#B` and `#C`,
/// so under relex each item is two re-cuts the lexer rejects before `#A`
/// takes it, each an `unexpected` error that nobody sees. The padding
/// makes the source long without adding steps or cuts: lexing it is
/// cheap, and a cut that costs the length of the source pays for every
/// byte of it.
fn relexing(relex: bool) -> Tabnas {
    let fixed = REJECTED.iter().fold(String::new(), |mut fixed, t| {
        let _ = write!(fixed, r##","#{t}":"{}""##, t.to_lowercase());
        fixed
    });
    let alternates = REJECTED.iter().fold(String::new(), |mut alternates, t| {
        let _ = write!(alternates, r##"{{"s":"#{t}"}},"##);
        alternates
    });
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&format!(
            r##"{{
              "clear":true,
              "options":{{
                "lex":{{"relex":{relex}}},
                "rule":{{"start":"top"}},
                "fixed":{{"token":{{"#A":"a"{fixed}}}}}
              }},
              "rule":{{"top":{{"open":[
                {alternates}
                {{"s":"#A","r":"top"}},
                {{"s":"#ZZ"}}
              ]}}}}
            }}"##
        ))
        .expect("the grammar installs");
    parser
}

fn padded(n: usize) -> String {
    format!("a{}", " ".repeat(400)).repeat(n)
}

#[test]
fn a_relexing_parse_of_a_valid_input_is_linear() {
    // The same input parses the same way without relex, so every one of
    // the re-cuts below is one the lexer rejects.
    let src = padded(3);
    assert_eq!(
        relexing(false).parse(&src).expect("parses"),
        relexing(true).parse(&src).expect("parses")
    );
    assert_linear("rejected re-cuts", &relexing(true), padded);
}

/// A rejected cut is still rejected, and an error that leaves the lexer
/// still carries the whole source, attached where it leaves.
#[test]
fn a_lexer_error_still_carries_the_source() {
    let src = "a a \"x";
    for relex in [false, true] {
        let error = relexing(relex).parse(src).expect_err("the string is open");
        assert_eq!(error.code, "unterminated_string", "relex={relex}");
        assert_eq!(error.full_source, src, "relex={relex}");
        assert_eq!((error.row, error.col), (1, 5), "relex={relex}");
    }
}
