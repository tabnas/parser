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

/// The three names the canonical deep merge will not carry, refused in
/// EVERY map whose keys a caller chooses rather than in `tokenSet` alone.
///
/// Rust has no prototype chain and could hold them happily. The code is
/// the contract across runtimes, so a grammar naming a comment
/// definition `constructor` must not load here and fault in TypeScript
/// and Go, which is what it did: the check sat inside the `tokenSet`
/// loop and every other caller-keyed map was open.
#[test]
fn a_reserved_name_is_refused_in_every_caller_keyed_map() {
    // (dotted path, an ordinary NAME for that map, a legitimate value).
    // The paths are the same list the canonical runtime declares; the
    // name varies because two of the maps constrain their keys.
    let maps = [
        ("fixed.token", "#Q", r##""~""##),
        ("match.token", "mine", r##""@/x/""##),
        ("match.value", "mine", r##"{"match":"@/x/","val":1}"##),
        ("tokenSet", "MINE", r##"["#TX"]"##),
        ("token_set", "MINE", r##"["#TX"]"##),
        ("comment.def", "mine", r##"{"line":true,"start":"%"}"##),
        ("value.def", "mine", r##"{"val":1}"##),
        ("string.escape", "q", r##""x""##),
        ("string.replace", "q", r##""x""##),
        ("error", "mine", r##""boom""##),
        ("hint", "mine", r##""try""##),
        ("parse.prepare", "mine", "null"),
        ("config.modify", "mine", "null"),
        ("lex.match", "mine", "false"),
        ("plugin", "mine", r##"{"a":1}"##),
    ];

    let nest = |path: &str, leaf: String| {
        path.split('.')
            .rev()
            .fold(leaf, |inner, segment| format!(r#"{{"{segment}":{inner}}}"#))
    };

    for (path, ordinary_name, ordinary) in maps {
        for name in ["__proto__", "constructor", "prototype"] {
            let body = nest(path, format!(r#"{{"{name}":{ordinary}}}"#));
            let mut parser = Tabnas::new();
            let result = parser.grammar_json(&format!(r#"{{"options":{body}}}"#));
            let err = match result {
                Ok(_) => panic!("{path}.{name} loaded"),
                Err(e) => e,
            };
            let msg = format!("{err:?}");
            assert!(
                msg.contains(&format!("options.{path}.{name}"))
                    && msg.contains("is a reserved name"),
                "{path}.{name}: error does not name the entry: {msg}"
            );
        }

        // An ordinary name in the same map still loads, so the refusal is
        // the three names and not the map. `config.modify` is excluded:
        // its entries are functions, no document carries one, and the
        // null a document CAN carry dies at the apply step in the
        // canonical runtime -- pre-existing, and not this test's subject.
        if "config.modify" == path {
            continue;
        }
        let body = nest(path, format!(r#"{{"{ordinary_name}":{ordinary}}}"#));
        let mut parser = Tabnas::new();
        parser
            .grammar_json(&format!(r#"{{"options":{body}}}"#))
            .unwrap_or_else(|e| panic!("{path}.{ordinary_name} was refused: {e:?}"));
    }
}
