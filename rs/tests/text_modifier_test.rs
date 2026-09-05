use std::sync::{Arc, Mutex};
use tabnas::{Lexer, Tabnas, Value};

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

#[test]
fn text_modifiers_also_transform_values_produced_by_the_text_matcher() {
    let mut parser = Tabnas::make_json();
    parser.text_modifier_ref("@tag", |value| Value::String(format!("{value:?}:modified")));
    parser
        .grammar_json(r#"{"options":{"text":{"modify":"@tag"}}}"#)
        .unwrap();

    assert_eq!(
        parser.parse("true").unwrap(),
        Value::String("Bool(true):modified".into())
    );
}

#[test]
fn imperative_text_modifiers_receive_live_lexer_rule_context_and_options() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let callback_seen = seen.clone();
    let mut parser = Tabnas::make_json();
    parser.imperative_text_modifier_ref("@inspect", move |value, lexer, rule, context, options| {
        callback_seen.lock().unwrap().push((
            rule.name.clone(),
            context.source.clone(),
            lexer.remaining().to_string(),
            options.value.lex,
        ));
        Value::String(format!("{value:?}@{}", rule.name))
    });
    parser
        .grammar_json(r#"{"options":{"text":{"modify":"@inspect"}}}"#)
        .unwrap();

    assert_eq!(
        parser.parse("true").unwrap(),
        Value::String("Bool(true)@val".into())
    );

    let mut lexer = Lexer::new("true", parser.options.clone());
    assert_eq!(
        lexer.next_raw_token().unwrap().val,
        Value::String("Bool(true)@#NORULE".into())
    );
    assert_eq!(
        *seen.lock().unwrap(),
        [
            ("val".into(), "true".into(), "".into(), true),
            ("#NORULE".into(), "true".into(), "".into(), true),
        ]
    );
}
