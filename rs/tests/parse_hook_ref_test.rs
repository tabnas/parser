use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use tabnas::{Tabnas, Value};

const NUMBER_GRAMMAR: &str = r##"{
  "clear":true,
  "options":{"rule":{"start":"top"}},
  "rule":{"top":{"open":[{"s":"#NR","a":"@value$"}]}}
}"##;

#[test]
fn serialized_prepare_refs_run_by_name_before_each_parse() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    for (name, event) in [("@z", "z"), ("@a", "a")] {
        let calls = calls.clone();
        parser.parse_prepare_ref(name, move |_context| {
            calls.lock().unwrap().push(event);
        });
    }
    parser.grammar_json(NUMBER_GRAMMAR).unwrap();
    parser
        .grammar_json(r##"{"options":{"parse":{"prepare":{"z":"@z","a":"@a"}}}}"##)
        .unwrap();

    assert_eq!(parser.parse("1").unwrap(), Value::Number(1.0));
    assert_eq!(parser.parse("2").unwrap(), Value::Number(2.0));
    assert_eq!(*calls.lock().unwrap(), ["a", "z", "a", "z"]);

    parser
        .grammar_json(r##"{"options":{"parse":{"prepare":{"a":null}}}}"##)
        .unwrap();
    parser.parse("3").unwrap();
    assert_eq!(*calls.lock().unwrap(), ["a", "z", "a", "z", "z"]);
}

#[test]
fn serialized_budget_ref_can_cancel_and_be_cleared() {
    let calls = Arc::new(AtomicUsize::new(0));
    let checked = calls.clone();
    let mut parser = Tabnas::new();
    parser.parse_budget_ref("@stop", move |_context| {
        checked.fetch_add(1, Ordering::SeqCst);
        false
    });
    parser.grammar_json(NUMBER_GRAMMAR).unwrap();
    parser
        .grammar_json(r##"{"options":{"parse":{"budget":{"checkEveryN":1,"onCheck":"@stop"}}}}"##)
        .unwrap();

    assert_eq!(parser.parse("42").unwrap_err().code, "cancel");
    assert!(calls.load(Ordering::SeqCst) > 0);

    parser
        .grammar_json(r##"{"options":{"parse":{"budget":{"onCheck":null}}}}"##)
        .unwrap();
    assert_eq!(parser.parse("42").unwrap(), Value::Number(42.0));
}

#[test]
fn unknown_parse_hook_refs_fail_transactionally() {
    for document in [
        r##"{"options":{"parse":{"prepare":{"x":"@missing"}}}}"##,
        r##"{"options":{"parse":{"budget":{"onCheck":"@missing"}}}}"##,
    ] {
        let mut parser = Tabnas::new();
        parser
            .grammar_json(r#"{"options":{"tag":"before"}}"#)
            .unwrap();
        assert!(
            parser.grammar_json(document).is_err(),
            "accepted {document}"
        );
        assert_eq!(parser.options.tag, "before");
        assert!(parser.options.parse.named_prepare.is_empty());
        assert!(parser.options.parse.budget.on_check.is_none());
    }
}
