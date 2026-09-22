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

/// A null WHOLE VALUE is a load error, not a deletion.
///
/// It used to remove the named set here and load cleanly, while the same
/// JSON grammar was a fault in TypeScript and a silent no-op in Go --
/// three answers for one document on the door that is meant to be the
/// portable one. TypeScript defines the language (ADR-13), so all three
/// now refuse it. A null MEMBER is untouched and still clears its
/// position, which the test above pins.
#[test]
fn a_null_whole_value_is_a_load_error() {
    for name in ["IGNORE", "KEY", "CUSTOM"] {
        let mut parser = Tabnas::new();
        let before = parser.options.token_set.get(name).map(|v| v.len());
        let result = parser.grammar_json(&format!(
            r##"{{"options":{{"tokenSet":{{"{name}":null}}}}}}"##
        ));
        let err = match result {
            Ok(_) => panic!("tokenSet.{name} = null loaded"),
            Err(e) => e,
        };
        let msg = format!("{err:?}");
        assert!(
            msg.contains(&format!("options.tokenSet.{name}")) && msg.contains("must be an array"),
            "{name}: error does not name the leaf: {msg}"
        );
        assert_eq!(
            parser.options.token_set.get(name).map(|v| v.len()),
            before,
            "{name}: the set changed although the load failed"
        );
    }
}
