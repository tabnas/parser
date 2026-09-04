use serde_json::Value;
use std::fs::File;
use std::io::{BufRead, BufReader};
use std::path::Path;
use tabnas::utility::{deep, modlist, str_inject, str_value, ListMods};

fn rows(name: &str) -> Vec<(usize, Vec<String>)> {
    let path = Path::new("../test/spec").join(name);
    let reader = BufReader::new(File::open(&path).unwrap_or_else(|error| {
        panic!("cannot open {}: {error}", path.display());
    }));
    let rows: Vec<_> = reader
        .lines()
        .enumerate()
        .filter_map(|(index, line)| {
            let line = line.expect("fixture row");
            (index > 0 && !line.trim().is_empty() && !line.starts_with('#')).then(|| {
                (
                    index + 1,
                    line.split('\t').map(str::to_owned).collect::<Vec<_>>(),
                )
            })
        })
        .collect();
    assert!(!rows.is_empty(), "{} has no data rows", path.display());
    rows
}

fn col(columns: &[String], index: usize) -> &str {
    columns.get(index).map_or("", String::as_str)
}

#[test]
fn shared_str_fixture() {
    for (line, columns) in rows("utility-str.tsv") {
        let value: Value = serde_json::from_str(col(&columns, 0)).unwrap();
        let max_len = if col(&columns, 1).is_empty() {
            44
        } else {
            col(&columns, 1).parse().unwrap()
        };
        assert_eq!(
            str_value(&value, max_len),
            col(&columns, 2),
            "utility-str.tsv:{line}"
        );
    }
}

#[test]
fn shared_deep_fixture() {
    for (line, columns) in rows("utility-deep.tsv") {
        let args: Vec<Value> = (0..4)
            .map(|index| col(&columns, index))
            .take_while(|value| !value.is_empty())
            .map(|value| serde_json::from_str(value).unwrap())
            .collect();
        let expected: Value = serde_json::from_str(col(&columns, 4)).unwrap();
        assert_eq!(
            deep(args[0].clone(), args[1..].iter().cloned()),
            expected,
            "utility-deep.tsv:{line}"
        );
    }
}

#[test]
fn shared_modlist_fixture() {
    for (line, columns) in rows("utility-modlist.tsv") {
        let list: Vec<Value> = serde_json::from_str(col(&columns, 0)).unwrap();
        let raw = col(&columns, 1);
        let mods = if raw.is_empty() {
            None
        } else {
            let value: Value = serde_json::from_str(raw).unwrap();
            Some(ListMods {
                delete: value["delete"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(Value::as_i64)
                    .map(|value| value as isize)
                    .collect(),
                move_items: value["move"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(Value::as_i64)
                    .map(|value| value as isize)
                    .collect(),
                custom: None,
            })
        };
        let expected: Vec<Value> = serde_json::from_str(col(&columns, 2)).unwrap();
        assert_eq!(
            modlist(list, mods.as_ref()),
            expected,
            "utility-modlist.tsv:{line}"
        );
    }
}

#[test]
fn shared_strinject_fixture() {
    for (line, columns) in rows("utility-strinject.tsv") {
        let values =
            (!col(&columns, 1).is_empty()).then(|| serde_json::from_str(col(&columns, 1)).unwrap());
        assert_eq!(
            str_inject(col(&columns, 0), values.as_ref()),
            col(&columns, 2),
            "utility-strinject.tsv:{line}"
        );
    }
}
