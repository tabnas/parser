//! The parser's prepared per-rule table, and what keeps it honest.
//!
//! Each installed rule's static routes are resolved once, when the grammar
//! is installed, and a rule carries the slot its record sits at so the parse
//! loop can find it again without hashing its name. Both halves can go
//! wrong in ways no other test would notice: a table rebuilt for only the
//! rule just installed would hand out a replaced rule's old definition
//! forever, and a slot trusted on its own would let a rule that has been
//! rewritten under the engine run another rule's alternates.

use std::sync::{Arc, Mutex};

use tabnas::{AltSpec, Options, Parser, RuleSpec, Tabnas, TIN_NR, TIN_ZZ};

type Log = Arc<Mutex<Vec<&'static str>>>;

fn spec(name: &str, open: AltSpec, close: AltSpec) -> RuleSpec {
    let mut spec = RuleSpec::new(name);
    spec.open.push(open);
    spec.close.push(close);
    spec
}

/// `top` matches a number and routes into `leaf`, which matches the second
/// number and runs whichever action the installed `leaf` carries.
fn routing_parser() -> Parser {
    let mut options = Options::default();
    options.rule.start = "top".into();
    let mut parser = Parser::new(options);
    parser.add_rule(spec(
        "top",
        AltSpec {
            s: vec![vec![TIN_NR]],
            p: Some("leaf".into()),
            ..Default::default()
        },
        AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        },
    ));
    parser
}

fn leaf(action: &str) -> RuleSpec {
    spec(
        "leaf",
        AltSpec {
            s: vec![vec![TIN_NR]],
            a: vec![action.into()],
            ..Default::default()
        },
        AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        },
    )
}

fn recorder(log: &Log, mark: &'static str) -> tabnas::Action {
    let log = log.clone();
    Arc::new(move |_rule| log.lock().unwrap().push(mark))
}

#[test]
fn replacing_a_routed_rule_routes_into_the_replacement() {
    let log: Log = Arc::new(Mutex::new(Vec::new()));
    let mut parser = routing_parser();
    parser.add_action("@first".into(), recorder(&log, "first"));
    parser.add_action("@second".into(), recorder(&log, "second"));

    parser.add_rule(leaf("@first"));
    // `top`'s route into `leaf` was resolved against the definition above.
    // Replacing `leaf` gives it a new spec, and every route into it has to
    // start handing that one out -- which is why installing a rule rebuilds
    // the whole table and not just the entry for the rule just installed.
    parser.add_rule(leaf("@second"));

    parser.parse("1 2").expect("the grammar parses");
    assert_eq!(
        *log.lock().unwrap(),
        ["second"],
        "the route still bound the definition `leaf` used to have"
    );
}

#[test]
fn a_route_into_a_rule_installed_later_resolves() {
    let log: Log = Arc::new(Mutex::new(Vec::new()));
    let mut parser = routing_parser();
    parser.add_action("@first".into(), recorder(&log, "first"));

    // `top` names `leaf` before there is a `leaf` to name. Nothing about
    // the route can be prepared at that point; installing `leaf` is what
    // settles it, so installation order must not matter.
    parser.add_rule(leaf("@first"));

    parser.parse("1 2").expect("the grammar parses");
    assert_eq!(*log.lock().unwrap(), ["first"]);
}

/// A rule reaches its prepared state through the slot it was bound to, but
/// `name` and `spec` are both public on a rule and both writable from any
/// callback. The slot is therefore only a hint: the record it points at is
/// used only while it still describes the rule in hand. Here an action
/// makes `child` into `top` -- same name, same spec -- and the close pass
/// has to run `top`'s alternate, not the one sitting at `child`'s slot.
///
/// Both rules also declare before-close and after-close actions, and they
/// declare different ones, because the alternates are not the only thing
/// read out of the record: the lifecycle action orders are prepared there
/// too. The impersonating rule's close pass is the one step in the suite
/// that reaches those phases with the record rejected, so it is the only
/// place the fallback to the spec's own order runs with anything in it --
/// and the only place a record trusted one step too long would be caught
/// running `child`'s lifecycle actions for `top`.
#[test]
fn a_rule_that_takes_another_rules_name_and_spec_runs_that_rules_alternates() {
    fn run(impersonate: bool) -> Vec<&'static str> {
        let top_spec: Arc<Mutex<Option<Arc<RuleSpec>>>> = Arc::new(Mutex::new(None));
        let log: Log = Arc::new(Mutex::new(Vec::new()));
        let mut parser = Tabnas::new();

        let captured = top_spec.clone();
        parser.action("@capture", move |rule| {
            *captured.lock().unwrap() = Some(rule.spec.clone());
        });
        let captured = top_spec.clone();
        let events = log.clone();
        parser.action("@impersonate", move |rule| {
            assert_eq!(rule.name.as_str(), "child");
            if impersonate {
                let top = captured
                    .lock()
                    .unwrap()
                    .clone()
                    .expect("the outer rule's spec was captured on the way in");
                rule.spec = top;
                rule.name = "top".into();
                events.lock().unwrap().push("impersonate");
            }
        });
        let events = log.clone();
        parser.action("@child-close", move |_rule| {
            events.lock().unwrap().push("child-close")
        });
        let events = log.clone();
        parser.action("@top-close", move |_rule| {
            events.lock().unwrap().push("top-close")
        });
        // `@<rule>-<phase>` is how a JSON grammar declares a lifecycle
        // action: the loader wires each of these into the phase its name
        // spells out.
        for (name, mark) in [
            ("@top-bc", "top-bc"),
            ("@top-ac", "top-ac"),
            ("@child-bc", "child-bc"),
            ("@child-ac", "child-ac"),
        ] {
            let events = log.clone();
            parser.action_with_context(name, move |_rule, _context| {
                events.lock().unwrap().push(mark);
                Ok(())
            });
        }
        parser
            .grammar_json(
                r##"{
                  "clear":true,
                  "options":{
                    "rule":{"start":"top"},
                    "fixed":{"token":{"#TA":"a","#TB":"b"}}
                  },
                  "rule":{
                    "top":{
                      "open":[{"s":"#TA","a":"@capture","p":"child"}],
                      "close":[{"s":"#ZZ","a":"@top-close"}]
                    },
                    "child":{
                      "open":[{"s":"#TB","a":"@impersonate"}],
                      "close":[{"s":"#ZZ","a":"@child-close"}]
                    }
                  }
                }"##,
            )
            .unwrap();

        parser.parse("ab").expect("the parse is unaffected");
        let events = log.lock().unwrap().clone();
        events
    }

    assert_eq!(
        run(true),
        [
            "impersonate",
            "top-bc",
            "top-close",
            "top-ac",
            "top-bc",
            "top-close",
            "top-ac",
        ],
        "the impersonating rule closed on its slot's alternate or its slot's \
         lifecycle order, not on the ones it now claims"
    );
    assert_eq!(
        run(false),
        [
            "child-bc",
            "child-close",
            "child-ac",
            "top-bc",
            "top-close",
            "top-ac",
        ]
    );
}
