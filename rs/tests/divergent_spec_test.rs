use indexmap::IndexMap;
use serde_json::Value as JsonValue;
use std::fs;
use tabnas::lexer::Lexer;
use tabnas::{Tabnas, Token, Value};

const COLUMNS: usize = 8;

fn preprocess(value: &str) -> String {
    value
        .replace("\\r\\n", "\r\n")
        .replace("\\n", "\n")
        .replace("\\r", "\r")
        .replace("\\t", "\t")
}

fn valhex(value: &Value) -> String {
    let Value::String(value) = value else {
        return "NOT-A-STRING".into();
    };
    value
        .encode_utf16()
        .map(|unit| format!("{unit:x}"))
        .collect::<Vec<_>>()
        .join(".")
}

fn render_token(token: &Token, fields: &[JsonValue]) -> String {
    fields
        .iter()
        .map(|field| match field.as_str().unwrap_or_default() {
            "name" => token.name.clone(),
            "src" => token.src.clone(),
            "si" => token.si.to_string(),
            "ri" => token.ri.to_string(),
            "ci" => token.ci.to_string(),
            "valhex" => valhex(&token.val),
            field => panic!("unknown token render field: {field}"),
        })
        .collect::<Vec<_>>()
        .join(":")
}

fn lex_probe(input: &str, args: &JsonValue) -> String {
    let mut tabnas = Tabnas::new();
    if let Some(options) = args.get("opts") {
        let document = serde_json::json!({"options": options});
        if tabnas.grammar_json(&document.to_string()).is_err() {
            return "INSTALL_ERROR".into();
        }
    }
    let mut lexer = Lexer::new(input, tabnas.options);
    let at = args.get("at").and_then(JsonValue::as_u64).unwrap_or(0) as usize;
    let find = args.get("find").and_then(JsonValue::as_str);
    let fields = args["show"].as_array().expect("lex probe show array");
    let mut retained = 0;
    for _ in 0..256 {
        let token = match lexer.next_raw_token() {
            Ok(token) => token,
            Err(error) => return format!("ERROR:{}:{}:{}", error.code, error.col, error.src),
        };
        if token.name == "#SP" {
            continue;
        }
        let selected = find.map_or(retained == at, |source| token.src == source);
        if selected {
            return render_token(&token, fields);
        }
        if token.name == "#ZZ" {
            break;
        }
        retained += 1;
    }
    "NO_TOKEN".into()
}

fn spec_probe(input: &str, args: &JsonValue, specs: &IndexMap<String, String>) -> String {
    let requested = args.get("spec").expect("spec probe grammar");
    let source = if let Some(name) = requested.as_str() {
        specs.get(name).cloned().unwrap_or_else(|| name.to_string())
    } else {
        requested.to_string()
    };
    let mut parser = Tabnas::new();
    if parser.grammar_json(&source).is_err() {
        return "INSTALL_ERROR".into();
    }
    match parser.parse(input) {
        Ok(_) => "OK".into(),
        Err(error) => format!("ERROR:{}", error.code),
    }
}

#[test]
fn shared_divergence_register_has_a_live_rust_lane() {
    let source = fs::read_to_string("../test/spec/divergent.tsv").expect("divergent.tsv");
    let mut specs = IndexMap::new();
    for line in source.lines() {
        if let Some(spec) = line.strip_prefix("# @spec ") {
            let (name, document) = spec.split_once(' ').expect("named spec document");
            specs.insert(name.to_string(), document.to_string());
        }
    }

    let mut seen = std::collections::HashSet::new();
    let mut ran = 0;
    for (index, raw) in source.lines().enumerate().skip(1) {
        if raw.starts_with('#') || raw.trim().is_empty() {
            continue;
        }
        let columns: Vec<String> = raw.split('\t').map(preprocess).collect();
        assert_eq!(columns.len(), COLUMNS, "divergent.tsv:{}", index + 1);
        let [name, probe, args, input, _, _, expected, why]: &[String; COLUMNS] =
            columns.as_slice().try_into().expect("checked column count");
        assert!(seen.insert(name.clone()), "duplicate divergence row {name}");
        assert!(!why.trim().is_empty(), "{name} has no justification");
        let args: JsonValue = serde_json::from_str(args).expect("probe arguments");
        let actual = match probe.as_str() {
            "lex" => lex_probe(input, &args),
            "spec" => spec_probe(input, &args, &specs),
            value => panic!("unknown probe {value}"),
        };
        assert_eq!(actual, *expected, "divergence row {name}");
        ran += 1;
    }
    assert!(ran > 0, "divergence register ran no rows");
}
