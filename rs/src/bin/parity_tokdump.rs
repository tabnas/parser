// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Token-stream dumper used by `ci/parity/run-parity.sh`.

use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use tabnas::{Tabnas, Token, Value};

fn json_string(value: &str) -> String {
    serde_json::to_string(value).expect("a Rust string is always JSON serializable")
}

fn value_repr(name: &str, value: &Value) -> String {
    if !matches!(name, "#ST" | "#TX" | "#NR" | "#VL") {
        return "-".into();
    }
    match value {
        Value::Undefined => "undef".into(),
        Value::Null => "null".into(),
        Value::Bool(value) => format!("bool:{value}"),
        Value::Number(value) => format!("num:{:016x}", value.to_bits()),
        Value::String(value) => format!("str:{}", json_string(value)),
        other => format!("other:{other}"),
    }
}

fn utf16_offset(source: &str, byte_offset: usize) -> usize {
    source
        .get(..byte_offset)
        .expect("token offsets must be UTF-8 boundaries")
        .encode_utf16()
        .count()
}

fn dump(source: &str) -> String {
    let output = Arc::new(Mutex::new((Vec::<String>::new(), false)));
    let captured = output.clone();
    let owned_source = source.to_string();
    let mut parser = Tabnas::make_json();
    parser.subscribe_tokens(move |token: &Token| {
        let mut state = captured.lock().expect("token output lock");
        if token.name == "#ZZ" {
            if state.1 {
                return;
            }
            state.1 = true;
        }
        state.0.push(format!(
            "{}\t{}\t{}\t{}\t{}",
            token.name,
            utf16_offset(&owned_source, token.si),
            token.ri,
            json_string(&token.src),
            value_repr(&token.name, &token.val),
        ));
    });
    if let Err(error) = parser.parse(source) {
        return format!("ERROR\t{}", error.code);
    }
    let joined = output.lock().expect("token output lock").0.join("\n");
    joined
}

fn input_files(target: &Path) -> Result<Vec<PathBuf>, String> {
    if target.is_file() {
        return Ok(vec![target.to_path_buf()]);
    }
    let mut files: Vec<_> = fs::read_dir(target)
        .map_err(|error| error.to_string())?
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| path.extension().is_some_and(|extension| extension == "in"))
        .collect();
    files.sort();
    Ok(files)
}

fn main() {
    let arguments: Vec<String> = env::args().collect();
    if arguments.len() != 3 || arguments[1] != "json" {
        eprintln!("usage: parity_tokdump json <input-file-or-dir>");
        std::process::exit(2);
    }
    let target = Path::new(&arguments[2]);
    let directory = target.is_dir();
    let files = input_files(target).unwrap_or_else(|error| {
        eprintln!("{error}");
        std::process::exit(2);
    });
    let mut chunks = Vec::new();
    for file in files {
        if directory {
            chunks.push(format!(
                "== {}",
                file.file_name().unwrap_or_default().to_string_lossy()
            ));
        }
        let source = fs::read_to_string(&file).unwrap_or_else(|error| {
            eprintln!("{}: {error}", file.display());
            std::process::exit(2);
        });
        chunks.push(dump(&source));
    }
    println!("{}", chunks.join("\n"));
}
