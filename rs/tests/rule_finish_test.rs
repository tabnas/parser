//! `rule.finish` is the grammar's switch, not the engine's (tabnas/parser#250).
//!
//! The option is stored, merged, serialized and handed to the grammar,
//! whose `@finish` alternates read it: jsonic closes an unterminated
//! structure at the end of the source when it is true and refuses it with
//! `end_of_source` when it is false. The engine never reads it, so
//! trailing content is refused whatever it says. The shared rows are in
//! test/spec/bad-token.tsv; this pins the Rust-only routes the issue
//! named: the typed option, a serialized grammar document, and `derive`.

use tabnas::Tabnas;

#[test]
fn rule_finish_is_kept_for_the_grammar() {
    // The strict-JSON preset turns it off, as @tabnas/json does.
    let parser = Tabnas::make_json();
    assert!(!parser.options.rule.finish);

    // A serialized grammar document sets it either way.
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r##"{"options":{"rule":{"finish":false}}}"##)
        .unwrap();
    assert!(!parser.options.rule.finish);
    parser
        .grammar_json(r##"{"options":{"rule":{"finish":true}}}"##)
        .unwrap();
    assert!(parser.options.rule.finish);

    // A derived child carries its own change and leaves the parent's.
    let parent = Tabnas::make_json();
    let child = parent.derive(|options| options.rule.finish = true).unwrap();
    assert!(child.options.rule.finish);
    assert!(!parent.options.rule.finish);
}

#[test]
fn trailing_content_is_refused_whatever_rule_finish_says() {
    for finish in [false, true] {
        let mut parser = Tabnas::make_json();
        parser
            .grammar_json(&format!(
                r##"{{"options":{{"rule":{{"finish":{finish}}}}}}}"##
            ))
            .unwrap();
        assert_eq!(parser.options.rule.finish, finish);
        let error = parser.parse("[1] 2").expect_err("trailing content");
        assert_eq!(error.code, "unexpected", "finish {finish}");
        assert_eq!((error.row, error.col), (1, 5), "finish {finish}");
        assert_eq!(
            parser.parse("[1]").unwrap().to_string(),
            "[1]",
            "finish {finish}"
        );
    }
}
