//! `test/spec/bad-token.tsv`, the Rust runner: what the parser does with a
//! bad token the lexer hands it, fail-fast, under recovery and under
//! relexing, and the error a recovering parse ends on. The TypeScript
//! (`ts/test/bad-token.test.js`) and Go (`go/bad_token_spec_test.go`)
//! runners assert the same rows; the fixture's header says what each column
//! means and what the custom matcher all three build must do.

use std::collections::HashMap;
use std::sync::Arc;

use tabnas::{
    ImperativeLexMatcher, LexMatcher, Tabnas, TabnasError, Token, TokenCode, Value, TIN_BD,
};

const FIXTURE: &str = "../test/spec/bad-token.tsv";

/// The `opts` cell that asks for `continuations(input)` instead of a parse.
const CONTINUATIONS: &str = "continuations";

fn decode(field: &str) -> String {
    field
        .replace("\\r\\n", "\r\n")
        .replace("\\n", "\n")
        .replace("\\r", "\r")
        .replace("\\t", "\t")
}

/// The custom matcher the fixture header specifies: a bad token at `?`, one
/// running to the end of the source at `%`, and one at `!` with only its
/// `err` set, none moving the cursor.
fn bad_matcher() -> LexMatcher {
    let matcher: ImperativeLexMatcher = Arc::new(|lexer, _rule, _context| {
        let at = lexer.point().site.pos;
        let rest = lexer.remaining();
        if rest.starts_with('?') {
            Some(lexer.bad_span("custom_bad", at, at + 1))
        } else if rest.starts_with('%') {
            let end = at + rest.chars().count();
            Some(lexer.bad_span("custom_unterminated", at, end))
        } else if rest.starts_with('!') {
            let mut token = Token::new("#BD", TIN_BD, Value::Undefined, "!", lexer.point());
            token.err = TokenCode::from("custom_err");
            Some(token)
        } else {
            None
        }
    });
    LexMatcher {
        name: "bad".into(),
        order: 1.5e6,
        matcher: None,
        imperative: Some(matcher),
        factory: None,
    }
}

fn make(grammar: &str, grammars: &HashMap<String, String>, opts: &str) -> Tabnas {
    let mut parser = Tabnas::new();
    if grammar == "json" {
        let source = std::fs::read_to_string("../ts/test/json-builder.fixture.json")
            .expect("json builder fixture");
        parser.grammar_json(&source).expect("install json");
        parser
            .grammar_json(r#"{"options":{"rule":{"start":"val"}}}"#)
            .expect("start at val");
    } else {
        let spec = grammars
            .get(grammar)
            .unwrap_or_else(|| panic!("no @grammar named {grammar}"));
        parser.grammar_json(spec).expect("install grammar");
    }
    if opts != "-" && opts != CONTINUATIONS {
        parser
            .grammar_json(&format!(r#"{{"options":{opts}}}"#))
            .expect("install options");
    }
    parser
        .options
        .lex
        .matchers
        .insert("bad".into(), bad_matcher());
    parser
}

/// The shared canonical value: sorted keys, integral numbers without a
/// fraction, and an absent value as `null`.
fn canon(value: &serde_json::Value) -> String {
    match value {
        serde_json::Value::Array(items) => {
            let items: Vec<String> = items.iter().map(canon).collect();
            format!("[{}]", items.join(","))
        }
        serde_json::Value::Object(map) => {
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort();
            let members: Vec<String> = keys
                .into_iter()
                .map(|key| {
                    format!(
                        "{}:{}",
                        serde_json::Value::from(key.as_str()),
                        canon(&map[key])
                    )
                })
                .collect();
            format!("{{{}}}", members.join(","))
        }
        serde_json::Value::Number(number) => match number.as_f64() {
            Some(float) if float.fract() == 0.0 && float.abs() < 1e15 => {
                format!("{}", float as i64)
            }
            _ => number.to_string(),
        },
        other => other.to_string(),
    }
}

fn render_value(value: Option<&Value>) -> String {
    match value {
        None | Some(Value::Undefined) => "null".into(),
        Some(value) => canon(&value.to_json()),
    }
}

fn render_error(error: &TabnasError) -> String {
    let meta = match &error.recovered {
        None => String::new(),
        Some(at) if at.bad => format!("+bad{}", at.skipped),
        Some(at) => format!("+skip{}", at.skipped),
    };
    format!("{}@{}:{}{}", error.code, error.row, error.col, meta)
}

fn run(parser: &Tabnas, input: &str, opts: &str) -> (String, String) {
    if opts == CONTINUATIONS {
        return (parser.continuations(input).tokens.join(","), "-".into());
    }
    if parser.options.parse.recover.enabled {
        let out = parser.parse_recover(input);
        assert!(out.fatal.is_none(), "recovery returned a fatal error");
        let errors: Vec<String> = out.errors.iter().map(render_error).collect();
        let errors = if errors.is_empty() {
            "-".to_string()
        } else {
            errors.join(",")
        };
        (render_value(out.value.as_ref()), errors)
    } else {
        match parser.parse(input) {
            Ok(value) => (render_value(Some(&value)), "-".into()),
            Err(error) => ("-".into(), render_error(&error)),
        }
    }
}

#[test]
fn bad_token_fixture() {
    let text = std::fs::read_to_string(FIXTURE).expect("bad-token.tsv");
    let mut grammars = HashMap::new();
    let mut ran = 0;
    let mut failures = Vec::new();
    for (index, line) in text.lines().enumerate().skip(1) {
        let row = index + 1;
        if let Some(definition) = line.strip_prefix("# @grammar ") {
            let (name, spec) = definition
                .split_once(' ')
                .unwrap_or_else(|| panic!("row {row}: malformed @grammar"));
            grammars.insert(name.to_string(), spec.trim().to_string());
            continue;
        }
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let cols: Vec<String> = line.split('\t').map(decode).collect();
        let [grammar, opts, input, value, errors] = cols.as_slice() else {
            panic!("row {row}: expected five columns, got {}", cols.len());
        };
        let (got_value, got_errors) = run(&make(grammar, &grammars, opts), input, opts);
        if (&got_value, &got_errors) != (value, errors) {
            failures.push(format!(
                "row {row} {input:?} {opts}\n  value:  got {got_value}, want {value}\n  errors: got {got_errors}, want {errors}"
            ));
        }
        ran += 1;
    }
    assert!(ran > 20, "bad-token.tsv ran only {ran} rows");
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}
