// Copyright (c) 2013-2026 Richard Rodger, MIT License

use regex::Regex;
use std::sync::{Arc, Mutex};
use tabnas::{
    AltSpec, ContextAction, FixedToken, MatchToken, MatchTokenMatcher, Options, RuleSpec, Tabnas,
    Value, TIN_TX,
};

fn fixed_grammar(tag: &str, token_name: &str, source: &str, result_suffix: &'static str) -> Tabnas {
    let mut options = Options {
        tag: tag.into(),
        ..Default::default()
    };
    let tin = options.register_token(token_name);
    options.fixed.tokens.insert(
        token_name.into(),
        FixedToken {
            name: token_name.into(),
            tin,
            source: source.into(),
        },
    );
    let mut tabnas = Tabnas::with_options(options);
    let mut rule = RuleSpec::new("val");
    let mut alt = AltSpec {
        s: vec![vec![TIN_TX], vec![tin]],
        ..Default::default()
    };
    alt.add_action(move |rule, _| {
        let text = rule.o0().map_or("", |token| token.src.as_str());
        *rule.node.borrow_mut() = Value::String(format!("{text}{result_suffix}"));
    });
    rule.open.push(alt);
    tabnas.rule(rule);
    tabnas
}

fn open_keys(tabnas: &Tabnas) -> Vec<String> {
    tabnas.rules["val"]
        .open
        .iter()
        .map(|alt| {
            alt.s
                .iter()
                .map(|slot| {
                    let mut names = slot
                        .iter()
                        .map(|tin| tabnas.options.token_name(*tin))
                        .collect::<Vec<_>>();
                    names.sort();
                    names.join(" ")
                })
                .collect::<Vec<_>>()
                .join(", ")
        })
        .collect()
}

#[test]
fn merge_is_commutative_and_keeps_sources_unchanged() {
    let left = fixed_grammar("A", "#AT", "@", "@");
    let right = fixed_grammar("B", "#PC", "%", "%");

    let ab = left.merge(&right).unwrap();
    let ba = right.merge(&left).unwrap();
    for merged in [&ab, &ba] {
        assert_eq!(Value::String("x@".into()), merged.parse("x@").unwrap());
        assert_eq!(Value::String("y%".into()), merged.parse("y%").unwrap());
        assert_eq!(vec!["#TX, #AT", "#TX, #PC"], open_keys(merged));
        assert_eq!("A~B", merged.options.tag);
    }

    assert_eq!(1, left.rules["val"].open.len());
    assert_eq!(1, right.rules["val"].open.len());
    assert!(left.parse("y%").is_err());
    assert!(right.parse("x@").is_err());
    assert_eq!("A", left.options.tag);
    assert_eq!("B", right.options.tag);
}

#[test]
fn merge_requires_distinct_non_default_tags() {
    let untagged = Tabnas::new();
    let tagged = fixed_grammar("B", "#PC", "%", "%");
    assert!(untagged
        .merge(&tagged)
        .err()
        .unwrap()
        .to_string()
        .contains("first instance needs a tag"));
    assert!(tagged
        .merge(&untagged)
        .err()
        .unwrap()
        .to_string()
        .contains("second instance needs a tag"));
    assert!(tagged
        .merge(&tagged)
        .err()
        .unwrap()
        .to_string()
        .contains("instance tags must differ"));
}

#[test]
fn merge_uses_non_default_options_and_rejects_real_conflicts() {
    let mut left = fixed_grammar("A", "#AT", "@", "@");
    let mut right = fixed_grammar("B", "#PC", "%", "%");
    left.options.rule.maxmul = 5;
    right.options.rule.maxmul = 7;
    assert_eq!(
        "merge: conflicting option values at rule.maxmul",
        left.merge(&right).err().unwrap().to_string()
    );
    assert_eq!(
        "merge: conflicting option values at rule.maxmul",
        right.merge(&left).err().unwrap().to_string()
    );

    right.options.rule.maxmul = Options::default().rule.maxmul;
    assert_eq!(5, left.merge(&right).unwrap().options.rule.maxmul);
    assert_eq!(5, right.merge(&left).unwrap().options.rule.maxmul);
}

#[test]
fn merge_rejects_two_fixed_names_for_one_source() {
    let left = fixed_grammar("A", "#AT", "@", "@");
    let right = fixed_grammar("B", "#BT", "@", "@");
    let error = left.merge(&right).err().unwrap().to_string();
    assert!(error.contains("#AT"));
    assert!(error.contains("#BT"));
    assert!(error.contains("both claim source \"@\""));
}

#[test]
fn longer_prefix_and_more_complex_alternates_sort_first() {
    let short_options = Options {
        tag: "A".into(),
        ..Default::default()
    };
    let mut short = Tabnas::with_options(short_options);
    let mut short_rule = RuleSpec::new("val");
    let mut short_alt = AltSpec {
        s: vec![vec![TIN_TX]],
        ..Default::default()
    };
    short_alt.add_action(|rule, _| *rule.node.borrow_mut() = Value::String("short".into()));
    short_rule.open.push(short_alt);
    short.rule(short_rule);

    let long = fixed_grammar("B", "#PC", "%", "%");
    for merged in [short.merge(&long).unwrap(), long.merge(&short).unwrap()] {
        assert_eq!(vec!["#TX, #PC", "#TX"], open_keys(&merged));
        assert_eq!(Value::String("z%".into()), merged.parse("z%").unwrap());
        assert_eq!(Value::String("short".into()), merged.parse("z").unwrap());
    }

    let condition_options = Options {
        tag: "A".into(),
        ..Default::default()
    };
    let mut conditioned = Tabnas::with_options(condition_options);
    let mut rule = RuleSpec::new("val");
    let mut alt = AltSpec {
        s: vec![vec![TIN_TX]],
        c_fn: Some(Arc::new(|_, _| false)),
        ..Default::default()
    };
    alt.add_action(|rule, _| *rule.node.borrow_mut() = Value::String("conditioned".into()));
    rule.open.push(alt);
    conditioned.rule(rule);

    let plain_options = Options {
        tag: "B".into(),
        ..Default::default()
    };
    let mut plain = Tabnas::with_options(plain_options);
    let mut rule = RuleSpec::new("val");
    let mut alt = AltSpec {
        s: vec![vec![TIN_TX]],
        ..Default::default()
    };
    alt.add_action(|rule, _| *rule.node.borrow_mut() = Value::String("plain".into()));
    rule.open.push(alt);
    plain.rule(rule);

    let merged = conditioned.merge(&plain).unwrap();
    assert!(merged.rules["val"].open[0].c_fn.is_some());
    assert_eq!(Value::String("plain".into()), merged.parse("x").unwrap());
}

#[test]
fn named_actions_are_namespaced_and_merged_children_rebuild() {
    let mut left = fixed_grammar("A", "#AT", "@", "unused");
    left.context_actions.insert(
        "@doit".into(),
        Arc::new(|rule, _| {
            *rule.node.borrow_mut() = Value::String("left".into());
            Ok(())
        }),
    );
    left.rules["val"].open[0].action_fns.clear();
    left.rules["val"].open[0].a = vec!["@doit".into()];

    let mut right = fixed_grammar("B", "#PC", "%", "unused");
    right.context_actions.insert(
        "@doit".into(),
        Arc::new(|rule, _| {
            *rule.node.borrow_mut() = Value::String("right".into());
            Ok(())
        }),
    );
    right.rules["val"].open[0].action_fns.clear();
    right.rules["val"].open[0].a = vec!["@doit".into()];

    let merged = left.merge(&right).unwrap();
    assert!(merged.context_actions.contains_key("@A:doit"));
    assert!(merged.context_actions.contains_key("@B:doit"));
    assert!(!merged.context_actions.contains_key("@doit"));
    assert_eq!(Value::String("left".into()), merged.parse("x@").unwrap());
    assert_eq!(Value::String("right".into()), merged.parse("x%").unwrap());

    let child = merged.derive(|options| options.rule.maxmul = 9).unwrap();
    assert_eq!(9, child.options.rule.maxmul);
    assert_eq!(open_keys(&merged), open_keys(&child));
    assert_eq!(Value::String("left".into()), child.parse("x@").unwrap());
    assert_eq!(Value::String("right".into()), child.parse("x%").unwrap());
}

#[test]
fn match_tokens_and_lex_matcher_order_survive_merge() {
    fn matcher_grammar(tag: &str, name: &str, pattern: &str, result: &'static str) -> Tabnas {
        let mut options = Options {
            tag: tag.into(),
            ..Default::default()
        };
        let tin = options.register_token(name);
        options.match_tokens.insert(
            name.into(),
            MatchToken {
                name: name.into(),
                tin,
                matcher: MatchTokenMatcher::Regex(Regex::new(pattern).unwrap()),
                eager: false,
            },
        );
        let mut tabnas = Tabnas::with_options(options);
        let mut rule = RuleSpec::new("val");
        let mut alt = AltSpec {
            s: vec![vec![tin]],
            ..Default::default()
        };
        alt.add_action(move |rule, _| *rule.node.borrow_mut() = Value::String(result.into()));
        rule.open.push(alt);
        tabnas.rule(rule);
        tabnas
    }

    let left = matcher_grammar("A", "#QQ", r"^!+", "bang");
    let right = matcher_grammar("B", "#WW", r"^\?+", "question");
    for merged in [left.merge(&right).unwrap(), right.merge(&left).unwrap()] {
        assert_eq!(Value::String("bang".into()), merged.parse("!!").unwrap());
        assert_eq!(
            Value::String("question".into()),
            merged.parse("??").unwrap()
        );
    }
}

#[test]
fn subscribers_append_in_canonical_tag_order_without_deduplication() {
    let count = Arc::new(Mutex::new(0));
    let callback: tabnas::RuleDoneSubscriber = {
        let count = count.clone();
        Arc::new(move |_, _, _| *count.lock().unwrap() += 1)
    };
    let mut left = fixed_grammar("A", "#AT", "@", "@");
    let mut right = fixed_grammar("B", "#PC", "%", "%");
    left.rule_done_subscribers.push(callback.clone());
    right.rule_done_subscribers.push(callback);

    let merged = right.merge(&left).unwrap();
    merged.parse("x@").unwrap();
    // One open and one close notification, delivered to both registrations.
    assert_eq!(4, *count.lock().unwrap());
}

#[test]
fn identical_shared_arc_alternates_and_lifecycle_callbacks_dedupe() {
    let action: ContextAction = Arc::new(|rule, _| {
        *rule.node.borrow_mut() = Value::String("shared".into());
        Ok(())
    });
    let lifecycle: ContextAction = Arc::new(|_, _| Ok(()));
    let make = |tag: &str| {
        let options = Options {
            tag: tag.into(),
            ..Default::default()
        };
        let mut tabnas = Tabnas::with_options(options);
        let mut rule = RuleSpec::new("val");
        rule.bo_fns.push(lifecycle.clone());
        rule.open.push(AltSpec {
            s: vec![vec![TIN_TX]],
            action_fns: vec![action.clone()],
            ..Default::default()
        });
        tabnas.rule(rule);
        tabnas
    };
    let merged = make("A").merge(&make("B")).unwrap();
    assert_eq!(1, merged.rules["val"].open.len());
    assert_eq!(1, merged.rules["val"].bo_fns.len());
    assert_eq!(Value::String("shared".into()), merged.parse("x").unwrap());
}

#[test]
fn empty_is_fresh_and_has_no_standard_token_producers() {
    let mut source = fixed_grammar("A", "#AT", "@", "@");
    source.subscribe_rules(|_, _| {});
    source
        .use_plugin(tabnas::Plugin::new("decorator", |_, _| Ok(())), None)
        .unwrap();

    let empty = source.empty();
    assert!(empty.rules.is_empty());
    assert!(empty.plugins.is_empty());
    assert!(empty.rule_subscribers.is_empty());
    assert!(empty.options.fixed.tokens.is_empty());
    assert!(empty.options.token_set.is_empty());
    assert!(!empty.options.text.lex);

    let mut options = Options::empty();
    options.tag = "empty".into();
    let configured = source.empty_with_options(options);
    assert_eq!("empty", configured.options.tag);
    assert!(configured.rules.is_empty());
}

#[test]
fn merge_unions_compatible_decorations_and_rejects_conflicts() {
    let mut left = fixed_grammar("A", "#AT", "@", "@");
    left.decorate("left", Value::Number(1.0));
    left.decorate("shared", Value::String("same".into()));
    let mut right = fixed_grammar("B", "#BT", "!", "!");
    right.decorate("right", Value::Number(2.0));
    right.decorate("shared", Value::String("same".into()));

    let merged = left.merge(&right).unwrap();
    assert_eq!(merged.decoration("left"), Some(&Value::Number(1.0)));
    assert_eq!(merged.decoration("right"), Some(&Value::Number(2.0)));

    right.decorate("shared", Value::String("different".into()));
    assert!(left
        .merge(&right)
        .err()
        .unwrap()
        .0
        .contains("decoration.shared"));
}

#[test]
fn matched_and_state_action_refs_are_namespaced_across_merge() {
    fn callback_grammar(tag: &str, source: &str, result: &'static str) -> Tabnas {
        let mut parser = Tabnas::with_options(Options {
            tag: tag.into(),
            ..Default::default()
        });
        let tin = parser.token_with_source(format!("#{tag}T"), source);
        let action_group = format!("g{}", tag.to_lowercase());
        parser.action_with_match_ref("@set", move |rule, _context, matched| {
            assert_eq!(matched.g, [action_group.as_str()]);
            *rule.node.borrow_mut() = Value::String(result.into());
            Ok(None)
        });
        parser.state_action_with_next_ref("@val-bo", |_rule, _context, next, out| {
            assert_eq!(next.map(|rule| rule.name.as_str()), Some("val"));
            assert!(out.is_none());
            Ok(None)
        });
        parser
            .grammar_json(&format!(
                r##"{{
                  "options":{{"rule":{{"start":"val"}}}},
                  "rule":{{"val":{{"open":[{{"s":"#{tag}T","a":"@set","g":"{}"}}]}}}}
                }}"##,
                format_args!("g{}", tag.to_lowercase())
            ))
            .unwrap();
        assert_eq!(parser.options.token(&format!("#{tag}T")), Some(tin));
        parser
    }

    let left = callback_grammar("A", "a", "left");
    let right = callback_grammar("B", "b", "right");
    for merged in [left.merge(&right).unwrap(), right.merge(&left).unwrap()] {
        assert!(merged.matched_actions.contains_key("@A:set"));
        assert!(merged.matched_actions.contains_key("@B:set"));
        assert!(merged.state_actions.contains_key("@A:val-bo"));
        assert!(merged.state_actions.contains_key("@B:val-bo"));
        assert_eq!(merged.parse("a").unwrap(), Value::String("left".into()));
        assert_eq!(merged.parse("b").unwrap(), Value::String("right".into()));
        let child = merged.derive(|_| {}).unwrap();
        assert_eq!(child.parse("a").unwrap(), Value::String("left".into()));
        assert_eq!(child.parse("b").unwrap(), Value::String("right".into()));
    }
}
