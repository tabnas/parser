use serde_json::json;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use tabnas::utility::{modlist, ListMods};
use tabnas::{AltSpec, ImperativeLexMatcher, LexCheckResult, RuleSpec, Tabnas, Token, Value};

#[test]
fn lazy_token_values_receive_and_mutate_the_live_rule_and_context() {
    let seen = Arc::new(Mutex::new(Vec::new()));
    let callback_seen = seen.clone();
    let mut parser = Tabnas::make_json();
    let value_tin = parser.options.token("#VL").unwrap();
    parser.imperative_lex_match_ref("@lazy", move |lexer, _rule, _context| {
        if !lexer.remaining().starts_with('$') {
            return None;
        }
        let point = lexer.point();
        assert!(lexer.advance_chars(1));
        let seen = callback_seen.clone();
        Some(
            lexer
                .token("#VL", value_tin, Value::String("EAGER".into()), "$", point)
                .with_lazy_value(move |rule, context| {
                    rule.u.insert("lazy-ran".into(), Value::Bool(true));
                    context
                        .u
                        .insert("lazy-ran".into(), Value::String(rule.name.clone()));
                    seen.lock().unwrap().push((
                        rule.name.clone(),
                        context.source.clone(),
                        context.meta.clone(),
                        context.rule.as_ref().map(|rule| rule.name.clone()),
                    ));
                    Value::String("LAZY".into())
                }),
        )
    });
    parser
        .grammar_json(r#"{"options":{"lex":{"match":{"lazy":{"order":1000,"make":"@lazy"}}}}}"#)
        .unwrap();

    assert_eq!(
        parser
            .parse_with_meta("$", Value::from_json(&json!({"request": 1})))
            .unwrap(),
        Value::String("LAZY".into())
    );
    assert_eq!(
        *seen.lock().unwrap(),
        vec![(
            "val".into(),
            "$".into(),
            Value::from_json(&json!({"request": 1})),
            Some("val".into()),
        )]
    );
}

#[test]
fn lazy_token_value_panics_become_active_action_errors() {
    let mut parser = Tabnas::make_json();
    let value_tin = parser.options.token("#VL").unwrap();
    parser.imperative_lex_match_ref("@lazy-panic", move |lexer, _rule, _context| {
        if !lexer.remaining().starts_with('$') {
            return None;
        }
        let point = lexer.point();
        assert!(lexer.advance_chars(1));
        Some(
            lexer
                .token("#VL", value_tin, Value::Undefined, "$", point)
                .with_lazy_value(|_rule, _context| panic!("lazy value exploded")),
        )
    });
    parser
        .grammar_json(
            r#"{"options":{"lex":{"match":{"lazy":{"order":1000,"make":"@lazy-panic"}}}}}"#,
        )
        .unwrap();

    let error = parser.parse("$").unwrap_err();
    assert_eq!(error.code, "internal");
    assert_eq!(error.rule, "val");
    assert_eq!(error.rule_stack, ["val"]);
    assert!(error.detail.contains("lazy value exploded"));
    assert!(error.detail.contains("@val-bc"));
}

#[test]
fn list_custom_runs_after_delete_and_move_and_can_replace() {
    let mods = ListMods::<i32> {
        delete: vec![1],
        move_items: vec![-1, 0],
        custom: None,
    }
    .with_custom(|list| {
        assert_eq!(list, &[4, 1, 3]);
        list.push(5);
        Some(list.iter().rev().copied().collect())
    });

    assert_eq!(modlist(vec![1, 2, 3, 4], Some(&mods)), [5, 3, 1, 4]);
}

#[test]
fn list_custom_none_retains_in_place_changes_and_runs_on_empty_lists() {
    let mods = ListMods::<i32>::default().with_custom(|list| {
        list.push(7);
        None
    });
    assert_eq!(modlist(Vec::new(), Some(&mods)), [7]);

    let mut rule = RuleSpec::new("top");
    rule.add_open(AltSpec {
        g: "first".into(),
        ..Default::default()
    });
    rule.add_open(AltSpec {
        g: "second".into(),
        ..Default::default()
    });
    let mods = ListMods::<AltSpec>::default().with_custom(|alts| {
        alts.reverse();
        None
    });
    rule.modify_open(&mods);
    assert_eq!(
        rule.open
            .iter()
            .map(|alternate| alternate.g.as_str())
            .collect::<Vec<_>>(),
        ["second", "first"]
    );
}

#[test]
fn live_lexer_checks_can_advance_and_return_native_tokens() {
    let calls = Arc::new(Mutex::new(Vec::new()));
    let seen = calls.clone();
    let mut parser = Tabnas::make_json();
    let value_tin = parser.options.token("#VL").unwrap();
    parser.imperative_lex_check_ref("@claim", move |lexer| {
        seen.lock()
            .unwrap()
            .push((lexer.remaining().to_string(), lexer.point().pos));
        if !lexer.remaining().starts_with('$') {
            return LexCheckResult::Continue;
        }
        let point = lexer.point();
        assert!(lexer.advance_chars(1));
        LexCheckResult::native_token(lexer.token(
            "#VL",
            value_tin,
            Value::String("CLAIMED".into()),
            "$",
            point,
        ))
    });
    parser
        .grammar_json(r#"{"options":{"fixed":{"check":"@claim"}}}"#)
        .unwrap();

    assert_eq!(parser.parse("$").unwrap(), Value::String("CLAIMED".into()));
    assert_eq!(*calls.lock().unwrap(), [("$".into(), 0)]);
}

#[test]
fn canonical_conditions_receive_the_live_match_and_lexer_together() {
    let mut parser = Tabnas::new();
    parser.alt_condition_with_lexer_and_match("@inspect-both", |rule, context, matched, lexer| {
        assert_eq!(rule.o0().map(|token| token.src.as_str()), Some("1"));
        assert_eq!(context.source, "1");
        assert_eq!(lexer.remaining(), "");
        matched
            .u
            .insert("condition".into(), Value::String("seen".into()));
        true
    });
    parser.action_with_match_ref("@verify", |rule, _context, matched| {
        assert_eq!(matched.u.get("condition"), rule.u.get("condition"));
        *rule.node.borrow_mut() = matched.u["condition"].clone();
        Ok(None)
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{
                "s":"#NR", "c":"@inspect-both", "a":"@verify"
              }]}}
            }"##,
        )
        .unwrap();

    assert_eq!(parser.parse("1").unwrap(), Value::String("seen".into()));
    let mut child = parser.derive(|_| {}).unwrap();
    child
        .grammar_json(
            r##"{
              "clear":true,
              "options":{"rule":{"start":"top"}},
              "rule":{"top":{"open":[{
                "s":"#NR", "c":"@inspect-both", "a":"@verify"
              }]}}
            }"##,
        )
        .unwrap();
    assert_eq!(child.parse("1").unwrap(), Value::String("seen".into()));
}

#[test]
fn matcher_factories_see_final_options_and_persist_across_parses() {
    let builds = Arc::new(AtomicUsize::new(0));
    let calls = Arc::new(AtomicUsize::new(0));
    let seen_options = Arc::new(Mutex::new(Vec::new()));
    let mut parser = Tabnas::new();
    parser.config_modifier_ref("@late", |options| options.space.chars = "~".into());
    let factory_builds = builds.clone();
    let factory_calls = calls.clone();
    let factory_seen = seen_options.clone();
    parser.lex_match_factory_ref("@factory", move |options| {
        factory_builds.fetch_add(1, Ordering::SeqCst);
        factory_seen
            .lock()
            .unwrap()
            .push(options.space.chars.clone());
        if options.tag == "disabled" {
            return None;
        }
        let calls = factory_calls.clone();
        let matcher: ImperativeLexMatcher = Arc::new(move |lexer, _rule, _context| {
            if !lexer.remaining().starts_with('$') {
                return None;
            }
            calls.fetch_add(1, Ordering::SeqCst);
            let point = lexer.point();
            assert!(lexer.advance_chars(1));
            Some(lexer.token("#FACT", -1, Value::String("FACTORY".into()), "$", point))
        });
        Some(matcher)
    });
    parser
        .grammar_json(
            r##"{
              "clear":true,
              "options":{
                "rule":{"start":"top"},
                "lex":{"match":{"factory":{"order":1000,"make":"@factory"}}},
                "config":{"modify":{"late":"@late"}}
              },
              "rule":{"top":{"open":[{"s":"#FACT","a":"@value$"}]}}
            }"##,
        )
        .unwrap();

    assert_eq!(builds.load(Ordering::SeqCst), 1);
    assert_eq!(*seen_options.lock().unwrap(), ["~"]);
    assert_eq!(parser.parse("$").unwrap(), Value::String("FACTORY".into()));
    assert_eq!(parser.parse("$").unwrap(), Value::String("FACTORY".into()));
    assert_eq!(builds.load(Ordering::SeqCst), 1);
    assert_eq!(calls.load(Ordering::SeqCst), 2);

    let child = parser
        .derive(|options| {
            options.tag = "disabled".into();
            options.space.chars = "overridden-before-modifier".into();
        })
        .unwrap();
    assert_eq!(builds.load(Ordering::SeqCst), 2);
    assert_eq!(*seen_options.lock().unwrap(), ["~", "~"]);
    assert!(child.options.lex.matchers["factory"].imperative.is_none());
}

#[test]
fn live_lexer_helpers_support_inspection_bad_tokens_and_relex_rollback() {
    let mut parser = Tabnas::new();
    let fixed = parser.token_with_source("#FIXED_A", "a");
    parser.alt_condition_with_lexer("@inspect", move |rule, context, lexer| {
        assert_eq!(lexer.forward(1), "");
        assert_eq!(lexer.forward(99), "");

        let extra = lexer.token_tin("#EXTRA");
        assert_eq!(lexer.token_name(extra), "#EXTRA");
        let bad = lexer.bad_span("probe", 0, 1);
        assert_eq!(bad.src, "a");
        assert_eq!(bad.err, "probe");

        let mut original = rule.o0().expect("matched token").clone();
        original.ignored = Some(Box::new(Token::new(
            "#SP",
            tabnas::TIN_SP,
            Value::Undefined,
            " ",
            tabnas::Point {
                len: 1,
                si: 0,
                pos: 0,
                ri: 1,
                ci: 1,
            },
        )));
        let (recut, checkpoint) = lexer
            .relex_for_rule(&original, &[fixed], rule, context)
            .expect("the fixed matcher can claim the same span");
        assert_eq!(recut.tin, fixed);
        assert_eq!(recut.src, "a");
        assert_eq!(
            recut.ignored.as_ref().map(|token| token.src.as_str()),
            Some(" ")
        );
        lexer.unrelex(checkpoint, context);
        assert_eq!(lexer.point().pos, 1);
        true
    });
    parser
        .grammar_json(
            r##"{
              "options":{
                "rule":{"start":"top"},
                "match":{"token":{"#MATCH_A":"@/^a/"}}
              },
              "rule":{"top":{"open":[{"s":"#MATCH_A","c":"@inspect"}]}}
            }"##,
        )
        .unwrap();

    parser.parse("a").unwrap();
}

#[test]
fn panicking_matcher_factory_fails_grammar_transactionally() {
    let mut parser = Tabnas::new();
    parser.lex_match_factory_ref("@boom", |_options| panic!("factory exploded"));
    let error = match parser
        .grammar_json(r#"{"options":{"lex":{"match":{"boom":{"order":1,"make":"@boom"}}}}}"#)
    {
        Ok(_) => panic!("panicking factory should fail"),
        Err(error) => error,
    };

    assert!(error.0.contains("factory boom panicked"), "{error}");
    assert!(parser.options.lex.matchers.is_empty());
}
