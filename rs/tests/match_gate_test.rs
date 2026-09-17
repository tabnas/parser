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

#[test]
fn expected_matchers_win_over_earlier_eager_ones() {
    // Two passes over the match tokens, position-expected first, as
    // go/lexer.go matchMatch and ts/src/lexer.ts makeMatchMatcher both
    // make. `#EA` and `#XA` both match an `a`; `#EA` is registered first
    // (lower tin) and eager, `#XA` is what the rule expects. One
    // tin-ordered pass in which eagerness merely bypassed the gate
    // produced `#EA`, and the rule failed on a token it never asked for
    // — the shape a character class inside a character class takes
    // (`p = %x31-39` beside `d = %x30-39`, on `12`).
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#EA":"@~/^a/","#XA":"@~/^[a-z]/"}}
              },
              "rule":{"top":{"open":[{"s":"#XA","a":"@value$"}]}}
            }"##,
        )
        .unwrap();
    assert_eq!(parser.parse("a").unwrap(), Value::String("a".into()));
}

#[test]
fn eager_matchers_still_fire_at_a_slot_the_column_does_not_cover() {
    // The second pass is what keeps eagerness useful: `#X` is not named
    // at the slot the two-token alternate peeks, so without it the `b`
    // of `ab` would be a fatal bad token instead of letting the
    // one-token alternate carry on.
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "fixed":{"token":{"#BANG":"!"}},
                "match":{"token":{"#X":"@~/^[a-z]/"}}
              },
              "rule":{"top":{"open":[
                {"s":"#X #BANG","a":"@value$"},
                {"s":"#X","p":"rest","a":"@value$"}
              ]},
              "rest":{"open":[{"s":"#X","a":"@value$"}]}}
            }"##,
        )
        .unwrap();
    assert!(parser.parse("a!").is_ok());
    assert!(parser.parse("ab").is_ok());
}

fn keyword_parser(sequence: &str) -> Tabnas {
    // `#IF` is a literal the grammar names; `#I` and `#ID` are eager
    // classes that contain it. `#I` cuts SHORTER than the literal, `#ID`
    // cuts the same or further, so in the eager pass the two matchers
    // weigh the same expected literal in turn.
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&format!(
            r##"{{
              "clear":true,
              "options":{{
                "rule":{{"start":"top"}},
                "fixed":{{"token":{{"#IF":"if"}}}},
                "match":{{"token":{{"#I":"@~/^i/","#ID":"@~/^[a-z]+/"}}}}
              }},
              "rule":{{"top":{{"open":[{{"s":"{sequence}","a":"@value$"}}]}}}}
            }}"##
        ))
        .unwrap();
    parser
}

#[test]
fn eager_pass_yields_to_an_expected_literal_only_where_it_cannot_out_cut_it() {
    // The slot names `#IF` and neither class, so both classes reach the
    // eager pass and are held to the literal's length there: `if` is a
    // tie and goes to the literal, `iffy` is cut further by `#ID` and
    // stays a word -- the keyword cannot truncate it. `#I` is held back
    // both times, so the length the fixed table answers is consulted
    // twice in one fetch and must answer the same.
    let parser = keyword_parser("#IF");
    assert!(parser.parse("if").is_ok());
    let error = parser.parse("iffy").unwrap_err();
    assert_eq!(error.code, "unexpected");
    assert_eq!(error.token.name, "#ID");
}

#[test]
fn a_literal_the_slot_does_not_name_holds_nothing_back_in_the_eager_pass() {
    // Same characters, but `#IF` is not what this slot expects, so no
    // literal outranks the classes and the first eager one in tin order
    // takes the `i`.
    let parser = keyword_parser("#TX");
    let error = parser.parse("if").unwrap_err();
    assert_eq!(error.code, "unexpected");
    assert_eq!(error.token.name, "#I");
}
