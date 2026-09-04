use tabnas::{Tabnas, Value};

fn parser_with_matcher(pattern: &str, sequence: &str) -> Tabnas {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&format!(
            r##"{{
              "clear":true,
              "options":{{
                "rule":{{"start":"top"}},
                "match":{{"token":{{"#CUSTOM":"{pattern}"}}}}
              }},
              "rule":{{"top":{{"open":[{{"s":"{sequence}","a":"@value$"}}]}}}}
            }}"##
        ))
        .unwrap();
    parser
}

#[test]
fn non_eager_matcher_only_runs_when_the_rule_slot_names_it() {
    let text = parser_with_matcher("@/^a/", "#TX");
    assert_eq!(text.parse("a").unwrap(), Value::String("a".into()));

    let custom = parser_with_matcher("@/^a/", "#CUSTOM");
    assert_eq!(custom.parse("a").unwrap(), Value::String("a".into()));
}

#[test]
fn eager_matcher_bypasses_the_rule_slot_gate() {
    let parser = parser_with_matcher("@~/^a/", "#TX");
    assert_eq!(parser.parse("a").unwrap_err().code, "unexpected");
}

#[test]
fn matcher_gate_uses_the_lookahead_position_being_filled() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#FIRST":"@/^a/","#SECOND":"@/^b/"}}
              },
              "rule":{"top":{"open":[{"s":"#FIRST #SECOND","a":"@value$"}]}}
            }"##,
        )
        .unwrap();
    assert!(parser.parse("a b").is_ok());
}

#[test]
fn matcher_precedence_is_tin_order_not_map_order() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#FIRST":"@~/^a/","#SECOND":"@~/^a/"}}
              },
              "rule":{"top":{"open":[{"s":"#FIRST","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    parser.options.match_tokens.swap_indices(0, 1);
    assert!(parser.parse("a").is_ok());
}
