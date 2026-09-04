use tabnas::{Tabnas, Value};

const TEXT_GRAMMAR: &str = r##"{
  "clear":true,
  "options":{
    "rule":{"start":"top"},
    "text":{"modify":["@bang","@bracket"]}
  },
  "rule":{"top":{"open":[{"s":"#TX","a":"@value$"}]}}
}"##;

#[test]
fn serialized_text_modifiers_run_in_declared_order() {
    let mut parser = Tabnas::new();
    parser.text_modifier_ref("@bang", |value| match value {
        Value::String(mut text) => {
            text.push('!');
            Value::String(text)
        }
        value => value,
    });
    parser.text_modifier_ref("@bracket", |value| match value {
        Value::String(text) => Value::String(format!("[{text}]")),
        value => value,
    });
    parser.grammar_json(TEXT_GRAMMAR).unwrap();

    assert_eq!(
        parser.parse("hello").unwrap(),
        Value::String("[hello!]".into())
    );
}

#[test]
fn text_modifier_may_change_the_value_type_without_changing_the_source() {
    let mut parser = Tabnas::new();
    parser.text_modifier_ref("@bang", |_| Value::Bool(true));
    parser.text_modifier_ref("@bracket", |value| value);
    parser.grammar_json(TEXT_GRAMMAR).unwrap();

    assert_eq!(parser.parse("hello").unwrap(), Value::Bool(true));
}

#[test]
fn unknown_text_modifier_refs_fail_transactionally() {
    let mut parser = Tabnas::make_json();
    let error = match parser.grammar_json(r#"{"options":{"text":{"modify":"@missing"}}}"#) {
        Ok(_) => panic!("missing text modifier should fail"),
        Err(error) => error,
    };

    assert!(error.0.contains("unknown text modifier"), "{error}");
    assert!(parser.options.text.modify.is_empty());
    assert_eq!(parser.parse("1").unwrap(), Value::Number(1.0));
}
