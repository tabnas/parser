// Copyright (c) 2013-2026 Richard Rodger, MIT License
//
// `number.exclude` is compiled once on the parser and shared by the lexer
// of every parse. These tests pin what that sharing must preserve: the
// pattern still excludes what it excluded, on every parse, through both
// the parser's lexer and a lexer built directly with `Lexer::new`; a
// changed pattern is recompiled; and an invalid pattern still excludes
// nothing rather than failing the parse.

use serde_json::json;
use std::sync::Arc;
use tabnas::lexer::Lexer;
use tabnas::options::Options;
use tabnas::{AltSpec, Parser, RuleSpec, Tabnas, Value, TIN_NR, TIN_TX, TIN_ZZ};

/// The JSON preset with its exclude pattern swapped for the one under
/// test, and text lexing turned back on so that an excluded number can
/// surface as text rather than as an error.
fn with_exclude(exclude: Option<&str>) -> Tabnas {
    let mut parser = Tabnas::make_json();
    parser
        .set_options(|options| {
            options.number.exclude = exclude.map(str::to_owned);
            options.text.lex = true;
            options.string.allow_unknown = true;
        })
        .expect("number.exclude is a plain option");
    parser
}

fn parsed(parser: &Tabnas, src: &str) -> serde_json::Value {
    parser
        .parse(src)
        .unwrap_or_else(|error| panic!("{src}: {error:?}"))
        .to_json()
}

#[test]
fn exclude_applies_on_every_parse_of_the_same_parser() {
    let parser = with_exclude(Some(r"^0\d"));

    // `01` matches the pattern, so it is not a number and lexes as text;
    // `1` does not match and stays a number. A second parse must see the
    // same compiled pattern as the first, not a lexer that lost it.
    for _ in 0..2 {
        assert_eq!(
            parsed(&parser, "[01]"),
            json!(["01"]),
            "an excluded number lexes as text"
        );
        assert_eq!(parsed(&parser, "[1]"), json!([1.0]));
    }
}

#[test]
fn json_preset_excludes_leading_zero_on_repeated_parses() {
    let parser = Tabnas::make_json();
    let document = r#"{"a":[1,2.5,"x",true,null],"b":{"c":"d"}}"#;
    let expected = json!({"a": [1.0, 2.5, "x", true, null], "b": {"c": "d"}});
    assert_eq!(parsed(&parser, document), expected);
    assert_eq!(parsed(&parser, document), expected);

    // The preset's pattern rejects a leading zero; it has to keep doing
    // so on the second parse as much as the first.
    for _ in 0..2 {
        assert!(parser.parse("[01]").is_err(), "01 is not a JSON number");
        assert_eq!(parsed(&parser, "[10]"), json!([10.0]));
    }
}

#[test]
fn exclude_changed_through_set_options_is_recompiled() {
    let mut parser = with_exclude(Some(r"^0\d"));
    assert_eq!(parsed(&parser, "[01]"), json!(["01"]));

    parser
        .set_options(|options| options.number.exclude = Some(r"^1".to_string()))
        .unwrap();
    assert_eq!(
        parsed(&parser, "[01]"),
        json!([1.0]),
        "the old pattern no longer applies"
    );
    assert_eq!(
        parsed(&parser, "[12]"),
        json!(["12"]),
        "the new pattern does"
    );

    parser
        .set_options(|options| options.number.exclude = None)
        .unwrap();
    assert_eq!(parsed(&parser, "[01]"), json!([1.0]));
}

#[test]
fn invalid_exclude_pattern_excludes_nothing() {
    let parser = with_exclude(Some("("));
    assert_eq!(
        parsed(&parser, "[01]"),
        json!([1.0]),
        "a pattern that does not compile is ignored, not an error"
    );
}

#[test]
fn lexer_built_directly_compiles_its_own_exclude() {
    let mut options = Options::default();
    options.number.exclude = Some(r"^0\d".to_string());

    let mut lexer = Lexer::new("01 1", options.clone());
    let excluded = lexer.next_raw_token().unwrap();
    assert_eq!(excluded.tin, TIN_TX, "01 is excluded from being a number");
    assert_eq!(excluded.src, "01");

    let mut lexer = Lexer::new("1", options);
    let kept = lexer.next_raw_token().unwrap();
    assert_eq!(kept.tin, TIN_NR);
    assert_eq!(kept.val, Value::Number(1.0));

    let mut lexer = Lexer::new("01", Options::default());
    let no_pattern = lexer.next_raw_token().unwrap();
    assert_eq!(no_pattern.tin, TIN_NR, "without a pattern 01 is a number");
}

/// `Parser.options` is public, so a caller on the low-level API can
/// replace it between parses. The pattern in force must be the one in
/// `self.options` at the parse, not the one the parser happened to be
/// built from: a cache derived from a public mutable field is a cache
/// with a second writer, and this is the writer.
#[test]
fn exclude_follows_options_replaced_directly_on_the_parser() {
    fn options_with(exclude: &str) -> Arc<Options> {
        let mut options = Options::default();
        options.number.exclude = Some(exclude.to_string());
        options.text.lex = true;
        options.string.allow_unknown = true;
        options.rule.start = "val".into();
        Arc::new(options)
    }

    fn val_rule() -> RuleSpec {
        let mut val = RuleSpec::new("val");
        // A hand-built rule carries no value into its node on its own;
        // the action is what makes the matched token's value the result,
        // and so what makes a number-versus-text outcome observable.
        let take_value = |tin| {
            let mut alt = AltSpec {
                s: vec![vec![tin]],
                ..Default::default()
            };
            alt.add_action(|rule, _context| {
                if let Some(token) = rule.o0() {
                    *rule.node.borrow_mut() = token.val.clone();
                }
            });
            alt
        };
        val.open.push(take_value(TIN_NR));
        val.open.push(take_value(TIN_TX));
        val.close.push(AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        });
        val
    }

    let mut parser = Parser::from_shared(options_with(r"^0\d"));
    parser.add_rule(val_rule());
    assert_eq!(
        parser.parse("01").expect("01 parses"),
        Value::String("01".into()),
        "the pattern the parser was built with excludes 01"
    );

    parser.options = options_with(r"^9");
    assert_eq!(
        parser.parse("01").expect("01 parses"),
        Value::Number(1.0),
        "the replaced options drop the old pattern"
    );
    assert_eq!(
        parser.parse("99").expect("99 parses"),
        Value::String("99".into()),
        "and bring the new one"
    );
}
