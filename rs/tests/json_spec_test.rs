use std::fs::File;
use std::io::{BufRead, BufReader};
use std::path::Path;
use tabnas::{Tabnas, Value};

fn preprocess_escapes(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let chars: Vec<char> = s.chars().collect();
    let mut i = 0;
    while i < chars.len() {
        if chars[i] == '\\' && i + 1 < chars.len() {
            match chars[i + 1] {
                'n' => {
                    out.push('\n');
                    i += 2;
                }
                'r' => {
                    out.push('\r');
                    i += 2;
                }
                't' => {
                    out.push('\t');
                    i += 2;
                }
                _ => {
                    out.push(chars[i]);
                    i += 1;
                }
            }
        } else {
            out.push(chars[i]);
            i += 1;
        }
    }
    out
}

fn load_tsv(path: &Path) -> Vec<(usize, String, String)> {
    let file = File::open(path).unwrap_or_else(|e| panic!("failed to open {:?}: {}", path, e));
    let reader = BufReader::new(file);
    let mut rows = Vec::new();

    for (line_idx, line) in reader.lines().enumerate() {
        let line_num = line_idx + 1;
        let line = line.expect("failed to read line");
        if line_num == 1 || line.trim().is_empty() || line.starts_with('#') {
            continue;
        }
        let cols: Vec<&str> = line.split('\t').collect();
        if cols.len() >= 2 {
            rows.push((line_num, cols[0].to_string(), cols[1].to_string()));
        }
    }
    rows
}

fn run_parser_tsv(file_name: &str) {
    let spec_path = Path::new("../test/spec").join(file_name);
    let rows = load_tsv(&spec_path);
    let parser = Tabnas::make_json();

    for (line_no, raw_input, raw_expected) in rows {
        let input = preprocess_escapes(&raw_input);
        let expected_json: serde_json::Value =
            serde_json::from_str(&raw_expected).expect("valid expected json");
        let expected = Value::from_json(&expected_json);

        match parser.parse(&input) {
            Ok(got) => {
                if !got.deep_equal(&expected) {
                    panic!(
                        "{}:{} Parse({:?})\n  got:      {}\n  expected: {}",
                        file_name, line_no, input, got, expected
                    );
                }
            }
            Err(e) => {
                panic!(
                    "{}:{} Parse({:?}) returned error: {}",
                    file_name, line_no, input, e
                );
            }
        }
    }
}

fn run_error_tsv(file_name: &str) {
    let spec_path = Path::new("../test/spec").join(file_name);
    let rows = load_tsv(&spec_path);
    let parser = Tabnas::make_json();

    for (line_no, raw_input, expected_err) in rows {
        let input = preprocess_escapes(&raw_input);
        assert!(
            expected_err.starts_with("ERROR:"),
            "{}:{} expected must start with ERROR:, got {}",
            file_name,
            line_no,
            expected_err
        );
        let want_code = &expected_err["ERROR:".len()..];

        match parser.parse(&input) {
            Ok(val) => {
                panic!(
                    "{}:{} Parse({:?}) should error (want {}), but succeeded with {}",
                    file_name, line_no, input, want_code, val
                );
            }
            Err(e) => {
                assert_eq!(
                    e.code, want_code,
                    "{}:{} Parse({:?}) error code got {:?}, want {:?}",
                    file_name, line_no, input, e.code, want_code
                );
            }
        }
    }
}

#[test]
fn test_spec_include_json() {
    run_parser_tsv("include-json.tsv");
}

#[test]
fn test_spec_include_json_utf8() {
    run_parser_tsv("include-json-utf8.tsv");
}

#[test]
fn test_spec_include_json_errors() {
    run_error_tsv("include-json-errors.tsv");
}

#[test]
fn test_spec_include_json_utf8_errors() {
    run_error_tsv("include-json-utf8-errors.tsv");
}

#[test]
fn serialized_json_builder_fixture_builds_native_values() {
    let source = std::fs::read_to_string("../ts/test/json-builder.fixture.json")
        .expect("json builder fixture");
    let mut parser = Tabnas::new();
    parser.options.rule.start = "val".into();
    parser.grammar_json(&source).expect("install JSON grammar");

    for input in [
        "1",
        r#""x""#,
        "true",
        "false",
        "null",
        "{}",
        "[]",
        r#"{"a":1}"#,
        "[1,2,3]",
        r#"{"a":{"b":[true,null,"x"]}}"#,
        r#"{"a":1,"b":2}"#,
    ] {
        let expected = Value::from_json(&serde_json::from_str(input).unwrap());
        let actual = parser
            .parse(input)
            .unwrap_or_else(|error| panic!("serialized JSON grammar failed for {input}: {error}"));
        assert!(actual.deep_equal(&expected), "input {input}: {actual}");
    }
}
