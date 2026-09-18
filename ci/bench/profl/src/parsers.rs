// The two benchmark grammars, transcribed from the canonical TypeScript
// definitions in `ports/typescript/src/parsers.ts`. The grammars are the
// measured subject, so they stay structurally identical across ports:
// the same alternates in the same order with the same conditions, not a
// Rust-idiomatic rewrite that happens to accept the same language.

use tabnas::{AltSpec, Context, Rule, Tabnas, Value, TIN_NR, TIN_ZZ};

pub fn make_parser(benchmark_id: &str) -> Result<Tabnas, String> {
    match benchmark_id {
        "adder" => Ok(make_adder()),
        "palindrome" => Ok(make_palindrome()),
        other => Err(format!("no Rust parser implementation for benchmark {other}")),
    }
}

fn make_adder() -> Tabnas {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "val".into();
    let plus = parser.token_with_source("#PL", "+");

    parser.define_rule("val", |rule| {
        let mut open = AltSpec {
            p: Some("add".into()),
            ..Default::default()
        };
        open.add_action(|rule, _context| {
            *rule.node.borrow_mut() = Value::Number(0.0);
        });
        rule.add_open(open).add_close(AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        });
    });

    parser.define_rule("add", move |rule| {
        let mut open = AltSpec {
            s: vec![vec![TIN_NR]],
            ..Default::default()
        };
        // The accumulator lives on the parent node, so each term folds into
        // the running total rather than building a tree that a later pass
        // would have to walk.
        open.add_action(|rule, _context| {
            let Some(parent) = rule.parent_node.clone() else {
                return;
            };
            let left = match &*parent.borrow() {
                Value::Number(number) => *number,
                _ => 0.0,
            };
            let right = match rule.o.first().map(|token| &token.val) {
                Some(Value::Number(number)) => *number,
                _ => 0.0,
            };
            *parent.borrow_mut() = Value::Number(left + right);
        });
        rule.add_open(open)
            .add_close(AltSpec {
                s: vec![vec![plus]],
                r: Some("add".into()),
                ..Default::default()
            })
            .add_close(AltSpec::default());
    });

    parser
}

fn make_palindrome() -> Tabnas {
    let mut parser = Tabnas::new();
    parser.options.rule.start = "val".into();
    // The empty word is in the language, so an empty source is a result
    // rather than an error.
    parser.options.lex.empty_result = Value::Bool(true);
    let a = parser.token_with_source("#A", "a");
    let b = parser.token_with_source("#B", "b");

    parser.define_rule("val", |rule| {
        let mut open = AltSpec {
            p: Some("pal".into()),
            ..Default::default()
        };
        open.add_action(|rule, _context| {
            *rule.node.borrow_mut() = Value::Bool(true);
        });
        rule.add_open(open).add_close(AltSpec {
            s: vec![vec![TIN_ZZ]],
            ..Default::default()
        });
    });

    // Position, not lookahead, is what resolves the non-determinism: the
    // parser descends while it is in the first half and turns around at the
    // exact midpoint. `v_abs` counts tokens consumed for this parse and the
    // alphabet is one byte per symbol, so the two agree.
    fn before_midpoint(_rule: &mut Rule, context: &mut Context) -> bool {
        context.v_abs * 2 < context.source.len()
    }
    fn at_midpoint(_rule: &mut Rule, context: &mut Context) -> bool {
        context.v_abs * 2 == context.source.len()
    }

    parser.define_rule("pal", move |rule| {
        for (token, symbol) in [(a, "a"), (b, "b")] {
            let mut open = AltSpec {
                s: vec![vec![token]],
                c_fn: Some(std::sync::Arc::new(before_midpoint)),
                p: Some("pal".into()),
                ..Default::default()
            };
            open.add_action(move |rule, _context| {
                rule.u_mut().insert("expected".into(), Value::String(symbol.into()));
            });
            rule.add_open(open);
        }
        let mut midpoint = AltSpec {
            c_fn: Some(std::sync::Arc::new(at_midpoint)),
            ..Default::default()
        };
        midpoint.add_action(|rule, _context| {
            rule.u_mut().insert("midpoint".into(), Value::Bool(true));
        });
        rule.add_open(midpoint);

        for (token, symbol) in [(a, "a"), (b, "b")] {
            let expected = Value::String(symbol.into());
            rule.add_close(AltSpec {
                s: vec![vec![token]],
                c_fn: Some(std::sync::Arc::new(move |rule: &mut Rule, _: &mut Context| {
                    rule.u.get("expected") == Some(&expected)
                })),
                ..Default::default()
            });
        }
        rule.add_close(AltSpec {
            c_fn: Some(std::sync::Arc::new(|rule: &mut Rule, _: &mut Context| {
                rule.u.get("midpoint") == Some(&Value::Bool(true))
            })),
            ..Default::default()
        });
    });

    parser
}
