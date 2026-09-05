// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Test-harness runner for serialized grammar corpora.
//!
//! Reads one JSON request from stdin:
//! `{ "grammar": <GrammarSpec>, "sources": ["..."] }`
//! and writes one outcome per source. This keeps compiler-consumer parity
//! checks independent of a Rust implementation of the source notation.

use serde::{Deserialize, Serialize};
use std::io::{self, Read};
use tabnas::{Tabnas, Value};

#[derive(Deserialize)]
struct Request {
    grammar: serde_json::Value,
    sources: Vec<String>,
}

#[derive(Serialize)]
struct Outcome {
    accepted: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    value: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    code: Option<String>,
}

fn main() {
    if let Err(error) = run() {
        eprintln!("{error}");
        std::process::exit(2);
    }
}

fn run() -> Result<(), String> {
    let mut input = String::new();
    io::stdin()
        .read_to_string(&mut input)
        .map_err(|error| format!("cannot read request: {error}"))?;
    let request: Request =
        serde_json::from_str(&input).map_err(|error| format!("invalid request: {error}"))?;
    let grammar = serde_json::to_string(&request.grammar)
        .map_err(|error| format!("cannot encode grammar: {error}"))?;
    let mut parser = Tabnas::new();
    parser
        .grammar_json(&grammar)
        .map_err(|error| format!("cannot install grammar: {error}"))?;

    let outcomes: Vec<Outcome> = request
        .sources
        .iter()
        .map(|source| match parser.parse(source) {
            Ok(value) => Outcome {
                accepted: true,
                value: Some(value),
                code: None,
            },
            Err(error) => Outcome {
                accepted: false,
                value: None,
                code: Some(error.code),
            },
        })
        .collect();
    serde_json::to_writer(io::stdout(), &outcomes)
        .map_err(|error| format!("cannot encode outcomes: {error}"))?;
    Ok(())
}
