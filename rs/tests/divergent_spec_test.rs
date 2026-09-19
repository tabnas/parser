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
            "name" => token.name.to_string(),
            "src" => token.src.to_string(),
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
    let mut lexer = Lexer::new(input, tabnas.config());
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
        // Without `show`, the probe renders the VALUE (tsv header, "spec"):
        // this lane returned a bare "OK" for every success, which happened
        // to match every row that existed because all of them passed
        // `show: ["code"]`. The first row to compare parsed values would
        // have gone red against a lane that never rendered one.
        Ok(value) => match args.get("show") {
            Some(_) => "OK".into(),
            None => format!("OK:{}", canon(&value)),
        },
        Err(error) => format!("ERROR:{}", error.code),
    }
}

/// The canonical value rendering the Go and TypeScript lanes already
/// share (`divergentCanon`, `canon`). Map keys sort by UTF-16 code unit
/// because key order is out of contract and UTF-8 order disagrees.
fn canon(value: &Value) -> String {
    match value {
        Value::Undefined | Value::Null => "null".into(),
        Value::Bool(true) => "true".into(),
        Value::Bool(false) => "false".into(),
        Value::String(text) => format!("\"{text}\""),
        Value::Text(text) => format!("\"{}\"", text.string),
        // ECMAScript Number::toString is what the Go lane reimplements as
        // `jsNumberString`; JavaScript gets it from `String()`. It is NOT
        // ported here, so a row whose value carries a number fails with
        // this marker rather than silently rendering a third spelling and
        // recording a divergence that is only a formatting difference.
        Value::Number(_) => "UNRENDERABLE_NUMBER".into(),
        Value::Array(items) => {
            let parts: Vec<String> = items.iter().map(canon).collect();
            format!("[{}]", parts.join(","))
        }
        Value::ListRef(list) => {
            let parts: Vec<String> = list.value.iter().map(canon).collect();
            format!("[{}]", parts.join(","))
        }
        Value::Object(map) => canon_entries(map.iter()),
        Value::MapRef(map) => canon_entries(map.value.iter()),
    }
}

fn canon_entries<'a>(entries: impl Iterator<Item = (&'a String, &'a Value)>) -> String {
    let mut pairs: Vec<(&String, &Value)> = entries.collect();
    pairs.sort_by(|a, b| utf16_units(a.0).cmp(&utf16_units(b.0)));
    let parts: Vec<String> = pairs
        .iter()
        .map(|(key, value)| format!("\"{key}\":{}", canon(value)))
        .collect();
    format!("{{{}}}", parts.join(","))
}

fn utf16_units(text: &str) -> Vec<u16> {
    text.encode_utf16().collect()
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
