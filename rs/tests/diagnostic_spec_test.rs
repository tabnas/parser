use serde_json::Value;
use std::fs::File;
use std::io::{BufRead, BufReader};
use std::path::Path;
use tabnas::Tabnas;

fn unescape(input: &str) -> String {
    input
        .replace("\\r\\n", "\r\n")
        .replace("\\n", "\n")
        .replace("\\r", "\r")
        .replace("\\t", "\t")
}

fn assert_subset(actual: &Value, expected: &Value, path: &str) {
    match expected {
        Value::Object(fields) => {
            for (key, value) in fields {
                let child = actual
                    .get(key)
                    .unwrap_or_else(|| panic!("{path}.{key} is missing from {actual}"));
                assert_subset(child, value, &format!("{path}.{key}"));
            }
        }
        _ => assert_eq!(actual, expected, "diagnostic mismatch at {path}"),
    }
}

#[test]
fn structured_diagnostic_fixture() {
    let path = Path::new("../test/spec/diagnostic.tsv");
    let parser = Tabnas::make_json();
    for (index, line) in BufReader::new(File::open(path).expect("diagnostic fixture"))
        .lines()
        .enumerate()
        .skip(1)
    {
        let line = line.expect("fixture line must be readable");
        if line.trim().is_empty() || line.starts_with('#') {
            continue;
        }
        let (input, expected) = line
            .split_once('\t')
            .unwrap_or_else(|| panic!("diagnostic.tsv:{} malformed row", index + 1));
        let error = parser
            .parse(&unescape(input))
            .unwrap_err_or_else(|| panic!("diagnostic.tsv:{} should fail", index + 1));
        let actual = serde_json::to_value(error).expect("serialize diagnostic");
        let expected: Value = serde_json::from_str(expected).expect("diagnostic subset JSON");
        assert_subset(&actual, &expected, &format!("diagnostic.tsv:{}", index + 1));
    }
}

trait ResultErrorExt<T, E> {
    fn unwrap_err_or_else(self, failure: impl FnOnce() -> E) -> E;
}

impl<T, E> ResultErrorExt<T, E> for Result<T, E> {
    fn unwrap_err_or_else(self, failure: impl FnOnce() -> E) -> E {
        match self {
            Ok(_) => failure(),
            Err(error) => error,
        }
    }
}
