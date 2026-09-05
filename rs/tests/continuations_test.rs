use tabnas::token::TIN_AA;
use tabnas::{AltSpec, RuleSpec, Tabnas, Tin, TIN_ZZ};

fn tokens(parser: &Tabnas, src: &str) -> Vec<String> {
    parser.continuations(src).tokens
}

fn has(tokens: &[String], name: &str) -> bool {
    tokens.iter().any(|token| token == name)
}

fn bare(fixed: &[(&str, &str)]) -> Tabnas {
    let fixed = fixed
        .iter()
        .map(|(name, source)| ((*name).to_owned(), serde_json::json!(source)))
        .collect::<serde_json::Map<_, _>>();
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            &serde_json::json!({
                "options": {
                    "rule": { "start": "top" },
                    "fixed": { "token": fixed }
                }
            })
            .to_string(),
        )
        .unwrap();
    parser
}

fn alt(slots: &[&[Tin]]) -> AltSpec {
    AltSpec {
        s: slots.iter().map(|slot| slot.to_vec()).collect(),
        ..Default::default()
    }
}

#[test]
fn strict_json_continuations_match_the_canonical_contract() {
    let parser = Tabnas::make_json();

    assert_eq!(tokens(&parser, r#"{"a""#), ["#CL"]);
    assert_eq!(tokens(&parser, r#"{"a":1"#), ["#CB", "#CA"]);
    assert_eq!(tokens(&parser, r#"{"a":1}"#), ["#ZZ"]);
    assert_eq!(tokens(&parser, "{"), ["#ST"]);

    let after_colon = tokens(&parser, r#"{"a":"#);
    for wanted in ["#ST", "#NR", "#OB", "#OS"] {
        assert!(
            has(&after_colon, wanted),
            "{wanted} missing: {after_colon:?}"
        );
    }

    let after_separator = tokens(&parser, "[1,");
    for wanted in ["#NR", "#ST", "#OB", "#OS", "#CS"] {
        assert!(
            has(&after_separator, wanted),
            "{wanted} missing: {after_separator:?}"
        );
    }

    let empty = tokens(&parser, "");
    for wanted in ["#OB", "#OS", "#ST", "#NR"] {
        assert!(has(&empty, wanted), "{wanted} missing: {empty:?}");
    }
}

#[test]
fn continuations_are_path_aware_on_failure_and_success() {
    let mut parser = bare(&[("#A", "a"), ("#B", "b"), ("#C", "c"), ("#D", "d")]);
    let a = parser.options.token("#A").unwrap();
    let b = parser.options.token("#B").unwrap();
    let c = parser.options.token("#C").unwrap();
    let d = parser.options.token("#D").unwrap();
    let mut top = RuleSpec::new("top");
    top.open.push(alt(&[&[a], &[b]]));
    top.open.push(alt(&[&[c], &[d]]));
    top.rule_close_on_end();
    parser.rule(top);

    let failed = tokens(&parser, "a");
    assert!(has(&failed, "#B"), "#B missing: {failed:?}");
    assert!(
        !has(&failed, "#D"),
        "#D came from a sibling path: {failed:?}"
    );

    let mut top = RuleSpec::new("top");
    top.open.push(alt(&[&[a], &[b]]));
    top.open.push(alt(&[&[c], &[d]]));
    top.open.push(alt(&[&[a]]));
    top.rule_close_on_end();
    parser.rule(top);
    assert_eq!(tokens(&parser, "a"), ["#ZZ", "#B"]);
    assert!(parser.parse("ab").is_ok());
    assert!(parser.parse("ac").is_err());
}

#[test]
fn push_closure_respects_backtracking_handover_position() {
    let mut parser = bare(&[("#A", "a"), ("#X", "x")]);
    let a = parser.options.token("#A").unwrap();
    let x = parser.options.token("#X").unwrap();
    let mut top = RuleSpec::new("top");
    top.open.push(alt(&[&[a], &[x]]));
    top.open.push(AltSpec {
        s: vec![vec![a]],
        b: 1,
        p: Some("child".into()),
        ..Default::default()
    });
    top.rule_close_on_end();
    parser.rule(top);
    let mut child = RuleSpec::new("child");
    child.open.push(alt(&[&[a]]));
    child.rule_close_on_end();
    parser.rule(child);

    let got = tokens(&parser, "a");
    assert!(has(&got, "#X"), "#X missing: {got:?}");
    assert!(!has(&got, "#A"), "backtracked #A was offered: {got:?}");
}

#[test]
fn an_empty_end_capture_means_only_end_is_legal() {
    let mut parser = bare(&[("#A", "a")]);
    let a = parser.options.token("#A").unwrap();
    let mut top = RuleSpec::new("top");
    top.open.push(alt(&[&[a]]));
    top.close.push(AltSpec::default());
    parser.rule(top);

    assert_eq!(tokens(&parser, "a"), ["#ZZ"]);
    assert!(parser.parse("aa").is_err());
}

#[test]
fn any_token_slots_match_and_report_the_any_token() {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "top".into();
    let mut top = RuleSpec::new("top");
    top.open.push(alt(&[&[TIN_AA]]));
    top.rule_close_on_end();
    parser.rule(top);

    assert!(parser.parse("word").is_ok());
    assert_eq!(tokens(&parser, ""), ["#AA"]);
}

trait CloseOnEnd {
    fn rule_close_on_end(&mut self);
}

impl CloseOnEnd for RuleSpec {
    fn rule_close_on_end(&mut self) {
        self.close.push(AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        });
    }
}
