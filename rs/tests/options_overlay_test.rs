//! The options overlay merges tokenSet index-wise onto the default set,
//! as TypeScript's deep merge treats the array (#151): a `null` removes a
//! position and a shorter array keeps the tail. Twin of
//! go/options_overlay_test.go and ts/test/options-overlay.test.js.

use tabnas::Tabnas;

fn parses(set: &str) -> bool {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&format!(
            r##"{{"clear":true,
              "options":{{"rule":{{"start":"top"}},"tokenSet":{{"IGNORE":{set}}}}},
              "rule":{{"top":{{"open":[{{"s":["#TX","#LN"]}}],"close":[{{"s":["#ZZ"]}}]}}}}}}"##
        ))
        .unwrap();
    parser.parse("a\n").is_ok()
}

#[test]
fn token_set_overlay_is_index_wise() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(r##"{"options":{"tokenSet":{"IGNORE":["#SP"]}}}"##)
        .unwrap();
    assert_eq!(
        parser.options.token_set["IGNORE"].len(),
        3,
        "a shorter array keeps the tail"
    );
    parser
        .grammar_json(r##"{"options":{"tokenSet":{"IGNORE":["#SP",null,null]}}}"##)
        .unwrap();
    assert_eq!(
        parser.options.token_set["IGNORE"].len(),
        1,
        "null removes a position"
    );

    // The rule reads #TX then #LN, which only matches once #LN has left
    // the IGNORE set.
    assert!(!parses(r##"["#SP"]"##));
    assert!(parses(r##"["#SP",null,null]"##));
}
