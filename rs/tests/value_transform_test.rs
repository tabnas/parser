use tabnas::{Tabnas, Value};

fn value_grammar(value: &str) -> String {
    format!(
        r##"{{
          "clear":true,
          "options":{{
            "rule":{{"start":"top"}},
            "value":{{"def":{{"tag":{{"match":"@/^([a-z]+)-([0-9]+)$/","val":{value}}}}}}}
          }},
          "rule":{{"top":{{"open":[{{"s":"#VL","a":"@value$"}}]}}}}
        }}"##
    )
}

#[test]
fn serialized_value_transform_receives_whole_match_and_capture_groups() {
    let mut parser = Tabnas::new();
    parser.value_transform_ref("@tag", |groups| {
        Value::String(format!("{}:{}:{}", groups[0], groups[1], groups[2]))
    });
    parser.grammar_json(&value_grammar(r#""@tag""#)).unwrap();

    assert_eq!(
        parser.parse("alpha-42").unwrap(),
        Value::String("alpha-42:alpha:42".into())
    );
}

#[test]
fn optional_unmatched_capture_is_an_empty_string() {
    let mut parser = Tabnas::new();
    parser.value_transform_ref("@tag", |groups| {
        Value::String(groups.get(2).cloned().unwrap_or_default())
    });
    let grammar =
        value_grammar(r#""@tag""#).replace("^([a-z]+)-([0-9]+)$", "^([a-z]+)(?:-([0-9]+))?$");
    parser.grammar_json(&grammar).unwrap();

    assert_eq!(parser.parse("alpha").unwrap(), Value::String(String::new()));
}

#[test]
fn unknown_refs_remain_literal_and_double_at_escapes_one_prefix() {
    let mut literal = Tabnas::new();
    literal
        .grammar_json(&value_grammar(r#""@not-registered""#))
        .unwrap();
    assert_eq!(
        literal.parse("alpha-42").unwrap(),
        Value::String("@not-registered".into())
    );

    let mut escaped = Tabnas::new();
    escaped
        .grammar_json(&value_grammar(r#""@@literal""#))
        .unwrap();
    assert_eq!(
        escaped.parse("alpha-42").unwrap(),
        Value::String("@literal".into())
    );
}
