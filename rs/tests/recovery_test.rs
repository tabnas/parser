use std::sync::{
    atomic::{AtomicBool, AtomicUsize, Ordering},
    Arc, Mutex,
};
use tabnas::{RecoverOptions, Tabnas, Value};

fn recovering() -> Tabnas {
    let mut parser = Tabnas::make_json();
    parser.options.parse.recover = RecoverOptions {
        enabled: true,
        ..Default::default()
    };
    parser
}

#[test]
fn recovery_is_off_by_default_and_clean_parses_stay_clean() {
    let plain = Tabnas::make_json();
    assert!(plain.parse(r#"{"a":1,}"#).is_err());
    let failed = plain.parse_recover(r#"{"a":1,}"#);
    assert!(failed.value.is_none());
    assert!(failed.fatal.is_some());
    assert_eq!(failed.errors.len(), 1);

    let recovered = recovering().parse_recover(r#"{"a":1}"#);
    assert!(recovered.fatal.is_none());
    assert!(recovered.errors.is_empty());
    assert_eq!(
        recovered.value,
        Some(Value::from_json(&serde_json::json!({"a": 1})))
    );
}

#[test]
fn recovery_keeps_the_partial_value_after_a_trailing_comma() {
    let parser = recovering();
    let recovered = parser.parse_recover(r#"{"a":1,}"#);
    assert!(recovered.fatal.is_none(), "fatal: {:?}", recovered.fatal);
    assert_eq!(recovered.errors.len(), 1);
    assert_eq!(
        recovered.value,
        Some(Value::from_json(&serde_json::json!({"a": 1})))
    );
    assert_eq!(
        parser.parse(r#"{"a":1,}"#).unwrap(),
        Value::from_json(&serde_json::json!({"a": 1}))
    );
}

#[test]
fn recovery_coalesces_unlexable_runs_and_preserves_later_values() {
    let mut parser = recovering();
    parser.options.parse.recover.suppress = 0;
    let recovered = parser.parse_recover(r#"{"a":true blah blip,"b":1}"#);
    assert!(recovered.fatal.is_none(), "fatal: {:?}", recovered.fatal);
    assert_eq!(recovered.errors.len(), 2);
    assert!(recovered
        .errors
        .iter()
        .all(|error| error.recovered.as_ref().is_some_and(|at| at.bad)));
    assert_eq!(
        recovered.value,
        Some(Value::from_json(&serde_json::json!({"a": true, "b": 1})))
    );
}

#[test]
fn recovery_at_end_of_source_keeps_the_outer_structure() {
    let recovered = recovering().parse_recover(r#"{"a":[1,2"#);
    assert!(!recovered.errors.is_empty());
    let value = recovered.value.expect("partial value");
    assert!(
        matches!(value, Value::Object(ref map) if map.contains_key("a")),
        "partial value: {value:?}; fatal: {:?}",
        recovered.fatal
    );
}

#[test]
fn serialized_recovery_options_are_validated_and_applied() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{
              "options":{"parse":{"recover":{
                "enabled":true,
                "syncGroups":["custom"],
                "syncTokens":["#CA"],
                "popUntilValid":false,
                "maxSkip":3,
                "maxRecoveries":2,
                "suppress":0
              }}}
            }"##,
        )
        .unwrap();
    let recover = &parser.options.parse.recover;
    assert!(recover.enabled);
    assert_eq!(recover.sync_groups, ["custom"]);
    assert_eq!(recover.sync_tokens, ["#CA"]);
    assert!(!recover.pop_until_valid);
    assert_eq!(recover.max_skip, 3);
    assert_eq!(recover.max_recoveries, 2);
    assert_eq!(recover.suppress, 0);

    for bad in [
        r#"{"options":{"parse":{"recover":{"syncGroups":1}}}}"#,
        r#"{"options":{"parse":{"recover":{"syncTokens":[1]}}}}"#,
        r#"{"options":{"parse":{"recover":{"maxSkip":-1}}}}"#,
    ] {
        assert!(Tabnas::new().grammar_json(bad).is_err(), "accepted {bad}");
    }
}

#[test]
fn recovery_caps_errors_and_suppresses_nearby_cascades() {
    let source = r#"{"a":true blah blip,"b":1}"#;

    let mut unsuppressed = recovering();
    unsuppressed.options.parse.recover.suppress = 0;
    assert_eq!(unsuppressed.parse_recover(source).errors.len(), 2);

    let mut suppressed = recovering();
    suppressed.options.parse.recover.suppress = 8;
    let result = suppressed.parse_recover(source);
    assert_eq!(result.errors.len(), 1);
    assert_eq!(
        result.value,
        Some(Value::from_json(&serde_json::json!({"a": true, "b": 1})))
    );

    let mut capped = recovering();
    capped.options.parse.recover.max_recoveries = 2;
    capped.options.parse.recover.suppress = 0;
    let result = capped.parse_recover(r#"{"a":true x,"b":true y,"c":true z}"#);
    assert!(
        result.errors.len() <= 3,
        "cap overshot: {:?}",
        result.errors
    );
}

#[test]
fn recovery_metadata_and_forced_close_events_are_exposed() {
    let mut parser = recovering();
    let forced = Arc::new(Mutex::new(Vec::new()));
    let seen = forced.clone();
    parser.subscribe_rule_done(move |rule, _context, done| {
        if done.forced {
            seen.lock().unwrap().push(rule.name.clone());
        }
    });

    let result = parser.parse_recover(r#"[{"a":1 ]"#);
    assert!(result.errors.iter().any(|error| error
        .recovered
        .as_ref()
        .is_some_and(|recovered| recovered.sync.is_some())));
    assert!(!forced.lock().unwrap().is_empty());
}

#[test]
fn forced_close_events_precede_the_recovered_pass_completion() {
    let mut parser = recovering();
    let events = Arc::new(Mutex::new(Vec::new()));
    let seen = events.clone();
    parser.subscribe_rule_done(move |rule, _context, done| {
        seen.lock().unwrap().push((
            rule.name.clone(),
            done.forced,
            done.alt.as_ref().is_some_and(|alt| alt.err.is_some()),
        ));
    });

    let result = parser.parse_recover(r#"{"a":[1,2}"#);
    assert!(!result.errors.is_empty());
    let events = events.lock().unwrap();
    let first_forced = events
        .iter()
        .position(|(_, forced, _)| *forced)
        .expect("recovery must force-close a nested rule");
    let recovered_pass = events
        .iter()
        .enumerate()
        .skip(first_forced + 1)
        .find(|(_, (_, forced, error))| !forced && *error)
        .map(|(index, _)| index)
        .expect("the recovered failing pass must still emit ruleDone");
    assert!(first_forced < recovered_pass);
}

#[test]
fn retrying_a_failed_close_does_not_repeat_before_close_actions() {
    let result = recovering().parse_recover("[1 :]");
    assert!(!result.errors.is_empty());
    if let Some(Value::Array(items)) = result.value {
        let ones = items
            .iter()
            .filter(|item| matches!(item, Value::Number(value) if *value == 1.0))
            .count();
        assert!(ones <= 1, "before-close action appended twice: {items:?}");
    }
}

#[test]
fn fixed_depth_recovery_and_mid_string_faults_terminate_safely() {
    let mut fixed_depth = recovering();
    fixed_depth.options.parse.recover.pop_until_valid = false;
    let _ = fixed_depth.parse_recover(r#"{"a":[1,2}"#);

    let source = "{\"a\":\"x\x07y\",\n\"b\":2}";
    let result = recovering().parse_recover(source);
    assert!(!result.errors.is_empty());
    if let Some(Value::Object(map)) = result.value {
        assert!(map
            .values()
            .all(|value| { !matches!(value, Value::String(text) if text.chars().count() >= 5) }));
    }
}

#[test]
fn recovery_state_does_not_leak_between_parse_runs() {
    let parser = recovering();
    assert!(!parser.parse_recover(r#"{"a":1,}"#).errors.is_empty());
    let clean = parser.parse_recover(r#"{"a":1}"#);
    assert!(clean.errors.is_empty());
    assert!(clean.fatal.is_none());
    assert_eq!(
        clean.value,
        Some(Value::from_json(&serde_json::json!({"a": 1})))
    );
}

#[test]
fn recovery_give_up_keeps_the_partial_value_without_a_fatal_result() {
    let mut parser = recovering();
    parser.options.parse.recover.max_skip = 0;

    let recovered = parser.parse_recover("[1 : abc def ghi]");
    assert!(recovered.fatal.is_none());
    assert_eq!(
        recovered
            .errors
            .iter()
            .map(|error| error.code.as_str())
            .collect::<Vec<_>>(),
        ["unexpected"]
    );
    assert_eq!(
        recovered.value,
        Some(Value::from_json(&serde_json::json!([1])))
    );
    assert_eq!(
        parser.parse("[1 : abc def ghi]").unwrap(),
        Value::from_json(&serde_json::json!([1]))
    );
}

#[test]
fn recovery_reports_trailing_content_and_keeps_the_completed_value() {
    for (source, expected) in [
        (r#""x" q"#, serde_json::json!("x")),
        ("1 q", serde_json::json!(1)),
        (r#""a" "b""#, serde_json::json!("a")),
    ] {
        let recovered = recovering().parse_recover(source);
        assert!(recovered.fatal.is_none(), "{source}: {:?}", recovered.fatal);
        assert_eq!(
            recovered
                .errors
                .iter()
                .map(|error| error.code.as_str())
                .collect::<Vec<_>>(),
            ["unexpected"],
            "{source}"
        );
        assert_eq!(
            recovered.value,
            Some(Value::from_json(&expected)),
            "{source}"
        );
    }

    let bad = recovering().parse_recover(r#"1 "abc"#);
    assert_eq!(
        bad.errors
            .iter()
            .map(|error| error.code.as_str())
            .collect::<Vec<_>>(),
        ["unterminated_string"]
    );
    assert_eq!(bad.value, Some(Value::Number(1.0)));
}

#[test]
fn matched_action_error_tokens_recover_and_stop_the_action_chain() {
    let mut parser = Tabnas::new();
    let later_calls = Arc::new(AtomicUsize::new(0));
    let done_errors = Arc::new(Mutex::new(Vec::new()));

    parser.action_with_match_ref("@boom", |_rule, context, _matched| {
        let mut token = context.t0().cloned().unwrap_or_default();
        token.bad("boom_code");
        Ok(Some(token))
    });
    let calls = later_calls.clone();
    parser.action("@later", move |_rule| {
        calls.fetch_add(1, Ordering::SeqCst);
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{
                "open":[{"s":"#NR","a":["@boom","@later"]}],
                "close":[{"s":"#ZZ"}]
              }}
            }"##,
        )
        .unwrap();
    parser.options.parse.recover.enabled = true;
    let seen_done_errors = done_errors.clone();
    parser.subscribe_rule_done(move |_rule, _context, done| {
        seen_done_errors.lock().unwrap().push(
            done.alt
                .as_ref()
                .is_some_and(|alternate| alternate.err.is_some()),
        );
    });

    let recovered = parser.parse_recover("42");
    assert!(recovered.fatal.is_none(), "fatal: {:?}", recovered.fatal);
    assert_eq!(
        recovered
            .errors
            .iter()
            .map(|error| error.code.as_str())
            .collect::<Vec<_>>(),
        ["boom_code"]
    );
    assert!(recovered.errors[0].recovered.is_some());
    assert_eq!(later_calls.load(Ordering::SeqCst), 0);
    assert_eq!(
        Some(&false),
        done_errors.lock().unwrap().first(),
        "a recovered action-token error is not the alternate's own e field"
    );
}

#[test]
fn lifecycle_error_tokens_recover_in_every_rule_phase() {
    for phase in ["bo", "ao", "bc", "ac"] {
        let mut parser = Tabnas::new();
        let raised = Arc::new(AtomicBool::new(false));
        let calls = Arc::new(AtomicUsize::new(0));
        let did_raise = raised.clone();
        let phase_calls = calls.clone();
        parser.state_action_with_next_ref(
            format!("@top-{phase}"),
            move |rule, context, _next, _out| {
                phase_calls.fetch_add(1, Ordering::SeqCst);
                if did_raise.swap(true, Ordering::SeqCst) {
                    return Ok(None);
                }
                let mut token = context
                    .t0()
                    .or_else(|| rule.o0())
                    .or_else(|| rule.c0())
                    .cloned()
                    .unwrap_or_default();
                token.bad("phase_code");
                Ok(Some(token))
            },
        );
        parser
            .grammar_json(
                r##"{
                  "clear":true,
                  "options":{"rule":{"start":"top"}},
                  "rule":{"top":{
                    "open":[{"s":"#NR"}],
                    "close":[{"s":"#ZZ"}]
                  }}
                }"##,
            )
            .unwrap();
        parser.options.parse.recover.enabled = true;

        let recovered = parser.parse_recover("42");
        assert!(
            recovered.fatal.is_none(),
            "{phase}: fatal: {:?}",
            recovered.fatal
        );
        assert_eq!(
            recovered
                .errors
                .iter()
                .map(|error| error.code.as_str())
                .collect::<Vec<_>>(),
            ["phase_code"],
            "{phase}"
        );
        assert!(recovered.errors[0].recovered.is_some(), "{phase}");
        let expected_calls = if phase == "ac" { 2 } else { 1 };
        assert_eq!(calls.load(Ordering::SeqCst), expected_calls, "{phase}");
    }
}
