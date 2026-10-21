// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! A parse from a reader, a window of the source at a time, against the
//! same text parsed whole (`doc/chunked-source.md`, "Proving it"): every
//! row of the spec register's JSON and default-grammar files, handed over
//! in pieces of 1, 2, 3, 7, 64 and 4096 bytes, splitting characters past
//! ASCII between pieces, under a window whose margin and block are a few
//! characters, so that every matcher meets the window's edge. The value,
//! the tokens the parser takes, the rule passes and an error's code,
//! position, row and column must be the same. A prototype: an error's
//! excerpt is left out of a parse from a reader.

use std::fs::File;
use std::io::{BufRead, BufReader, Read};
use std::path::Path;
use std::sync::{Arc, Mutex};

use tabnas::{Tabnas, WindowSizes};

/// A reader that hands its bytes over at most `size` at a time.
struct Pieces<'a> {
    bytes: &'a [u8],
    at: usize,
    size: usize,
}

impl Read for Pieces<'_> {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        let n = self.size.min(buf.len()).min(self.bytes.len() - self.at);
        buf[..n].copy_from_slice(&self.bytes[self.at..self.at + n]);
        self.at += n;
        Ok(n)
    }
}

fn unescape(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut chars = s.chars().peekable();
    while let Some(c) = chars.next() {
        if c == '\\' {
            match chars.peek() {
                Some('n') => {
                    chars.next();
                    out.push('\n');
                }
                Some('r') => {
                    chars.next();
                    out.push('\r');
                }
                Some('t') => {
                    chars.next();
                    out.push('\t');
                }
                _ => out.push(c),
            }
        } else {
            out.push(c);
        }
    }
    out
}

/// The inputs of a spec file whose first column is the input.
fn inputs(file: &str) -> Vec<(usize, String)> {
    let path = Path::new("../test/spec").join(file);
    let reader = BufReader::new(File::open(&path).unwrap_or_else(|e| panic!("{file}: {e}")));
    reader
        .lines()
        .enumerate()
        .filter_map(|(i, line)| {
            let line = line.expect("a line");
            if i == 0 || line.trim().is_empty() || line.starts_with('#') {
                return None;
            }
            let input = line.split('\t').next()?.to_string();
            Some((i + 1, unescape(&input)))
        })
        .collect()
}

/// What a parse shows: its value or its error, the tokens the parser took
/// and its rule passes.
#[derive(Debug, PartialEq)]
struct Seen {
    outcome: String,
    tokens: Vec<String>,
    rules: Vec<String>,
}

/// A grammar with `lex.window` set, recording the tokens and rule passes
/// of each parse into `log`.
fn windowed(make: fn() -> Tabnas, log: &Arc<Mutex<(Vec<String>, Vec<String>)>>) -> Tabnas {
    let mut parser = make();
    parser
        .set_options(|options| options.lex.window = true)
        .expect("lex.window sets");
    let tokens = Arc::clone(log);
    parser.subscribe_tokens(move |token| {
        tokens.lock().unwrap().0.push(format!(
            "{} {:?} {} {}:{}",
            token.tin, token.src, token.site.pos, token.site.ri, token.site.ci
        ));
    });
    let rules = Arc::clone(log);
    parser.subscribe_rules(move |rule, _| {
        rules
            .lock()
            .unwrap()
            .1
            .push(format!("{} {:?}", rule.name, rule.state));
    });
    parser
}

/// What a parse showed, its tokens and passes taken from `log`, which is
/// empty again after.
fn seen(
    log: &Arc<Mutex<(Vec<String>, Vec<String>)>>,
    parsed: Result<tabnas::Value, tabnas::TabnasError>,
) -> Seen {
    let outcome = match parsed {
        Ok(value) => format!("ok {value}"),
        Err(e) => format!(
            "err {} at {} {}:{} {:?}",
            e.code, e.pos, e.row, e.col, e.src
        ),
    };
    let (tokens, rules) = std::mem::take(&mut *log.lock().unwrap());
    Seen {
        outcome,
        tokens,
        rules,
    }
}

fn differential(file: &str, make: fn() -> Tabnas) -> usize {
    let log = Arc::new(Mutex::new((Vec::new(), Vec::new())));
    let parser = windowed(make, &log);
    let mut checked = 0;
    for (line, input) in inputs(file) {
        let whole = seen(&log, parser.parse(&input));
        for size in [1, 2, 3, 7, 64, 4096] {
            for (margin, block) in [(1, 1), (3, 2), (64, 64)] {
                let reader = Pieces {
                    bytes: input.as_bytes(),
                    at: 0,
                    size,
                };
                let sizes = WindowSizes {
                    margin,
                    block,
                    chunk: size,
                };
                let read = seen(&log, parser.parse_reader_with(reader, sizes));
                assert_eq!(
                    read, whole,
                    "{file}:{line} {input:?} in pieces of {size}, margin {margin}, block {block}"
                );
                checked += 1;
            }
        }
    }
    checked
}

#[test]
fn json_rows_parse_the_same_from_a_reader() {
    let mut checked = 0;
    for file in [
        "include-json.tsv",
        "include-json-errors.tsv",
        "include-json-utf8.tsv",
        "include-json-utf8-errors.tsv",
    ] {
        checked += differential(file, Tabnas::make_json);
    }
    assert!(checked > 1000, "{checked}");
}

#[test]
fn the_default_grammars_rows_parse_the_same_from_a_reader() {
    assert!(differential("happy.tsv", Tabnas::new) > 100);
}

/// A long source is held a window at a time: a parse from a reader never
/// holds much more than its margin and block of the source, however long
/// the source.
#[test]
fn a_long_source_is_held_a_window_at_a_time() {
    let records: String = (0..20_000)
        .map(|i| format!("{{\"id\":{i},\"name\":\"n{i}\",\"tags\":[\"a\",\"b\"]}}"))
        .collect::<Vec<_>>()
        .join(",");
    let text = format!("[{records}]");
    let mut parser = Tabnas::make_json();
    parser
        .set_options(|options| options.lex.window = true)
        .expect("lex.window sets");
    let whole = parser.parse(&text).expect("the text parses");
    let read = parser
        .parse_reader(text.as_bytes())
        .expect("the reader parses");
    assert!(read.deep_equal(&whole));
}
