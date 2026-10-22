// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! A parse from a reader, a window of the source at a time, against the
//! same text parsed whole (admin ADR-30, `doc/chunked-source.md`, "Proving
//! it"). Every input of every parsing file in the spec register is parsed
//! by the default and the strict-JSON grammars, and the probe and
//! rule-history rows by their own specs, each with `lex.window` set: once
//! whole, and from a reader that hands the text over in pieces of 1, 2, 3,
//! 7, 64 and 4096 bytes, splitting characters past ASCII between pieces,
//! under windows whose margin and block are a few characters, so that
//! every matcher meets the window's edge. The value, the tokens the parser
//! takes, the rule passes and an error's code, position, row, column and
//! token must be the same. An error's excerpt must be the same while the
//! window holds the text, and a cut of it where the window has dropped the
//! line's start: the one difference a parse from a reader shows.

use std::fs;
use std::io::Read;
use std::path::Path;
use std::sync::{Arc, Mutex};

use tabnas::{RuleState, Tabnas, TabnasError, Value, WindowSizes};

/// A reader that hands its bytes over at most `size` at a time.
struct Pieces<'a> {
    bytes: &'a [u8],
    at: usize,
    size: usize,
}

impl<'a> Pieces<'a> {
    fn new(bytes: &'a [u8], size: usize) -> Self {
        Pieces { bytes, at: 0, size }
    }
}

impl Read for Pieces<'_> {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        let n = self.size.min(buf.len()).min(self.bytes.len() - self.at);
        buf[..n].copy_from_slice(&self.bytes[self.at..self.at + n]);
        self.at += n;
        Ok(n)
    }
}

/// A reader that hands over `good` bytes, a few at a time, and then fails.
struct Failing<'a> {
    bytes: &'a [u8],
    at: usize,
    good: usize,
}

impl Read for Failing<'_> {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        if self.at >= self.good {
            return Err(std::io::Error::other("the disk went away"));
        }
        let n = 3.min(buf.len()).min(self.good - self.at);
        buf[..n].copy_from_slice(&self.bytes[self.at..self.at + n]);
        self.at += n;
        Ok(n)
    }
}

/// The spec register's escapes, as its runners decode them.
fn decode(field: &str) -> String {
    field
        .replace("\\r\\n", "\r\n")
        .replace("\\n", "\n")
        .replace("\\r", "\r")
        .replace("\\t", "\t")
}

/// A spec file's rows: the line, the `input` column decoded, and the
/// named spec document the row's `spec` column asks for, if any.
fn rows(file: &str) -> Vec<(usize, String, Option<String>)> {
    let text = fs::read_to_string(Path::new("../test/spec").join(file))
        .unwrap_or_else(|e| panic!("{file}: {e}"));
    let mut specs = std::collections::HashMap::new();
    for line in text.lines() {
        if let Some(spec) = line.strip_prefix("# @spec ") {
            let (name, document) = spec.split_once(' ').expect("a named spec document");
            specs.insert(name.to_string(), document.to_string());
        }
    }
    let mut lines = text.lines().enumerate();
    let header: Vec<&str> = lines.next().expect("a header").1.split('\t').collect();
    let input = header
        .iter()
        .position(|column| *column == "input")
        .unwrap_or_else(|| panic!("{file} has no input column"));
    let spec = header.iter().position(|column| *column == "spec");
    lines
        .filter(|(_, line)| !line.trim().is_empty() && !line.starts_with('#'))
        .map(|(index, line)| {
            let columns: Vec<&str> = line.split('\t').collect();
            let document = spec.and_then(|column| {
                let args: serde_json::Value = serde_json::from_str(columns[column]).ok()?;
                let name = args["spec"].as_str()?;
                Some(specs.get(name).expect("a defined # @spec").clone())
            });
            (index + 1, decode(columns[input]), document)
        })
        .collect()
}

/// Every file of the register whose rows are parsed.
const PARSED: [&str; 15] = [
    "bad-token.tsv",
    "diagnostic.tsv",
    "divergent.tsv",
    "happy.tsv",
    "include-json.tsv",
    "include-json-errors.tsv",
    "include-json-utf8.tsv",
    "include-json-utf8-errors.tsv",
    "lex-ender-array.tsv",
    "lex-string-control.tsv",
    "lex-text-line-terminator.tsv",
    "lex-text-quote.tsv",
    "probe.tsv",
    "repaired.tsv",
    "rule-history.tsv",
];

/// The pieces a reader hands its bytes over in.
const PIECES: [usize; 6] = [1, 2, 3, 7, 64, 4096];

/// Windows whose margin and block are a few characters, and one that holds
/// every input of the register whole.
const WINDOWS: [(usize, usize); 4] = [(1, 1), (3, 2), (64, 64), (4096, 4096)];

type Log = Arc<Mutex<(Vec<String>, Vec<String>)>>;

/// `parser` with `lex.window` set, recording the tokens and rule passes of
/// each parse into `log`.
fn windowed(mut parser: Tabnas, log: &Log) -> Tabnas {
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
    parser.subscribe_rule_done(move |rule, _context, done| {
        let state = match done.state {
            RuleState::Open => "open",
            RuleState::Close => "close",
        };
        rules
            .lock()
            .unwrap()
            .1
            .push(format!("{} {state} {}", rule.name.as_str(), done.probe));
    });
    parser
}

/// What a parse showed: its value, or its error without the excerpt; its
/// tokens and passes, taken from `log`, which is empty again after; and
/// the error's excerpt, its line and its rendering.
#[derive(Debug, PartialEq)]
struct Seen {
    outcome: String,
    tokens: Vec<String>,
    rules: Vec<String>,
    line: Option<String>,
    rendered: Option<String>,
}

fn seen(log: &Log, parsed: Result<Value, TabnasError>) -> Seen {
    let (outcome, line, rendered) = match parsed {
        Ok(value) => (format!("ok {value}"), None, None),
        Err(e) => {
            let json = serde_json::to_value(&e).expect("a diagnostic");
            (
                format!(
                    "err {} at {} {}:{} {:?} {} {:?} {:?} {:?}",
                    e.code, e.pos, e.row, e.col, e.src, e.len, e.token, e.rule, e.expected
                ),
                json["src"].as_str().map(str::to_string),
                Some(e.to_string()),
            )
        }
    };
    let (tokens, rules) = std::mem::take(&mut *log.lock().unwrap());
    Seen {
        outcome,
        tokens,
        rules,
        line,
        rendered,
    }
}

/// Every input parsed whole and from a reader, under every pieces and
/// window size; the count of reader parses compared.
fn differential(file: &str, line: usize, input: &str, parser: &Tabnas, log: &Log) -> usize {
    let whole = seen(log, parser.parse(input));
    let mut checked = 0;
    for size in PIECES {
        for (margin, block) in WINDOWS {
            let sizes = WindowSizes {
                margin,
                block,
                chunk: size,
            };
            let read = seen(
                log,
                parser.parse_reader_with_sizes(Pieces::new(input.as_bytes(), size), sizes),
            );
            let at = format!(
                "{file}:{line} {input:?} in pieces of {size}, margin {margin}, block {block}"
            );
            assert_eq!(read.outcome, whole.outcome, "{at}");
            assert_eq!(read.tokens, whole.tokens, "{at}");
            assert_eq!(read.rules, whole.rules, "{at}");
            if margin >= input.len() {
                // The window holds the text whole: the same excerpt.
                assert_eq!(read.line, whole.line, "{at}");
                assert_eq!(read.rendered, whole.rendered, "{at}");
            } else if let (Some(cut), Some(full)) = (&read.line, &whole.line) {
                // Cut where the window begins, and read on past its end no
                // further than its margin.
                assert!(
                    full.contains(cut.as_str()),
                    "{at}: {cut:?} is no cut of {full:?}"
                );
            }
            checked += 1;
        }
    }
    checked
}

#[test]
fn every_parsed_row_parses_the_same_from_a_reader() {
    let log: Log = Arc::default();
    let default = windowed(Tabnas::new(), &log);
    let json = windowed(Tabnas::make_json(), &log);
    let mut checked = 0;
    for (index, file) in PARSED.iter().enumerate() {
        let rows = rows(file);
        for (line, input, spec) in &rows {
            match spec {
                Some(document) => {
                    let mut parser = Tabnas::new();
                    // A spec the register expects to refuse installing
                    // (`ERROR:install`) has no parse to compare.
                    if parser.grammar_json(document).is_err() {
                        continue;
                    }
                    let parser = windowed(parser, &log);
                    checked += differential(file, *line, input, &parser, &log);
                }
                None => {
                    checked += differential(file, *line, input, &default, &log);
                    checked += differential(file, *line, input, &json, &log);
                }
            }
        }
        println!(
            "window differential: {} of {} files ({}%), {checked} reader parses",
            index + 1,
            PARSED.len(),
            (index + 1) * 100 / PARSED.len()
        );
    }
    assert!(checked > 10_000, "{checked}");
}

/// A long source parses the same from a reader, a window at a time, under
/// the fixed window and a small one, and so does one past ASCII.
#[test]
fn a_long_source_parses_the_same_from_a_reader() {
    let log: Log = Arc::default();
    let parser = windowed(Tabnas::make_json(), &log);
    for name in ["n", "ñ"] {
        let records: Vec<String> = (0..20_000)
            .map(|i| format!("{{\"id\":{i},\"{name}\":\"v{i}\",\"tags\":[\"a\",\"b\"]}}"))
            .collect();
        let text = format!("[{}]", records.join(",\n"));
        let whole = seen(&log, parser.parse(&text));
        let read = seen(&log, parser.parse_reader(text.as_bytes()));
        assert_eq!(read, whole, "{name}");
        let small = WindowSizes {
            margin: 16,
            block: 16,
            chunk: 7,
        };
        let read = seen(
            &log,
            parser.parse_reader_with_sizes(Pieces::new(text.as_bytes(), 5), small),
        );
        assert_eq!(read.outcome, whole.outcome, "{name}");
        assert_eq!(read.tokens, whole.tokens, "{name}");
        assert_eq!(read.rules, whole.rules, "{name}");
    }
}

/// An error on a line longer than the window shows the line from where the
/// window begins, and its caret stays under the error.
#[test]
fn a_long_lines_excerpt_is_cut_where_the_window_begins() {
    let parser = {
        let mut parser = Tabnas::make_json();
        parser
            .set_options(|options| options.lex.window = true)
            .expect("lex.window sets");
        parser
    };
    let text = format!("[{}x]", "1,".repeat(200));
    let whole = parser.parse(&text).unwrap_err();
    let small = WindowSizes {
        margin: 8,
        block: 8,
        chunk: 4,
    };
    let read = parser
        .parse_reader_with_sizes(Pieces::new(text.as_bytes(), 4), small)
        .unwrap_err();
    assert_eq!(
        (&read.code, read.pos, read.row, read.col),
        (&whole.code, whole.pos, whole.row, whole.col)
    );
    let whole_line = serde_json::to_value(&whole).unwrap()["src"]
        .as_str()
        .unwrap()
        .to_string();
    let read_line = serde_json::to_value(&read).unwrap()["src"]
        .as_str()
        .unwrap()
        .to_string();
    assert_eq!(whole_line, text);
    assert!(read_line.len() < whole_line.len(), "{read_line:?}");
    assert!(whole_line.ends_with(&read_line), "{read_line:?}");
    // The caret sits under the `x`, on the line as the window shows it.
    let rendered = plain(&read.to_string());
    let lines: Vec<&str> = rendered.lines().collect();
    let shown = lines
        .iter()
        .position(|line| line.ends_with(read_line.as_str()))
        .expect("the cut line is shown");
    let x = lines[shown].len() - read_line.len() + read_line.find('x').unwrap();
    let caret = lines[shown + 1].find('^').expect("a caret");
    assert_eq!(caret, x, "{rendered}");
}

/// `text` without its terminal colour codes.
fn plain(text: &str) -> String {
    let mut out = String::new();
    let mut chars = text.chars();
    while let Some(c) = chars.next() {
        if c == '\u{1b}' {
            for c in chars.by_ref() {
                if c == 'm' {
                    break;
                }
            }
        } else {
            out.push(c);
        }
    }
    out
}

/// A reader that fails, and bytes that are not UTF-8, fail the parse with
/// `unreadable` where the source failed, whether the grammar reads a window
/// at a time or the reader whole.
#[test]
fn an_unreadable_source_fails_where_it_fails() {
    let window = {
        let mut parser = Tabnas::make_json();
        parser
            .set_options(|options| options.lex.window = true)
            .expect("lex.window sets");
        parser
    };
    let whole = Tabnas::make_json();
    let text = b"[1,\n2,\n3333,\n4]";
    for parser in [&window, &whole] {
        let failed = parser
            .parse_reader(Failing {
                bytes: text,
                at: 0,
                good: 9,
            })
            .unwrap_err();
        assert_eq!(failed.code, "unreadable");
        assert_eq!((failed.pos, failed.row, failed.col), (9, 3, 3));
        assert!(
            failed.to_string().contains("the disk went away"),
            "{failed}"
        );

        let mut bytes = b"[1,\n\"a\xffb\"]".to_vec();
        let invalid = parser
            .parse_reader_with_sizes(
                Pieces::new(&bytes, 2),
                WindowSizes {
                    margin: 2,
                    block: 2,
                    chunk: 2,
                },
            )
            .unwrap_err();
        assert_eq!(invalid.code, "unreadable");
        assert_eq!((invalid.pos, invalid.row, invalid.col), (6, 2, 3));

        // A character the source ends inside.
        bytes = "[\"ñ".as_bytes().to_vec();
        bytes.pop();
        let cut = parser.parse_reader(bytes.as_slice()).unwrap_err();
        assert_eq!(cut.code, "unreadable");
        assert_eq!((cut.pos, cut.row, cut.col), (2, 1, 3));
    }
}

/// `lex.window` survives a merge of two grammars, from either side.
#[test]
fn lex_window_survives_a_merge() {
    let tagged = |tag: &str, window: bool| {
        let mut parser = Tabnas::make_json();
        parser
            .set_options(|options| {
                options.lex.window = window;
                options.tag = tag.into();
            })
            .expect("the options set");
        parser
    };
    let merged = tagged("A", true)
        .merge(&tagged("B", false))
        .expect("the grammars merge");
    assert!(merged.options.lex.window);
    let merged = tagged("A", false)
        .merge(&tagged("B", true))
        .expect("the grammars merge");
    assert!(merged.options.lex.window);
    let merged = tagged("A", false)
        .merge(&tagged("B", false))
        .expect("the grammars merge");
    assert!(!merged.options.lex.window);
}

/// In a parse from a reader a window at a time, the length a `Point`
/// carries (`Lexer::point`, as a matcher sees it) is the length read so
/// far, where a parse of the whole text carries the whole length: the one
/// field of a token's position that differs (DIVERGENCE.md, "What a parse
/// from a reader shows of its source in Rust").
#[test]
fn a_points_length_is_the_length_read_so_far() {
    let grammar = r##"{
      "clear":true,
      "options":{
        "rule":{"start":"top"},
        "lex":{"window":true,"match":{"probe":{"order":1,"make":"@probe"}}}
      },
      "rule":{"top":{"open":[{"s":"#TX","r":"top"},{"s":"#ZZ"}]}}
    }"##;
    let probed = |lens: &Arc<Mutex<Vec<usize>>>| {
        let seen = Arc::clone(lens);
        let mut parser = Tabnas::new();
        parser.imperative_lex_match_ref("@probe", move |lexer, _rule, _context| {
            seen.lock().unwrap().push(lexer.point().len);
            None
        });
        parser.grammar_json(grammar).expect("the grammar installs");
        parser
    };
    let text = "a ".repeat(200);
    let sizes = WindowSizes {
        margin: 4,
        block: 4,
        chunk: 4,
    };
    let read: Arc<Mutex<Vec<usize>>> = Arc::default();
    probed(&read)
        .parse_reader_with_sizes(Pieces::new(text.as_bytes(), 4), sizes)
        .expect("the reader parses");
    let read = read.lock().unwrap();
    assert!(!read.is_empty());
    assert!(read[0] < text.len(), "{read:?}");
    assert!(read.windows(2).all(|pair| pair[0] <= pair[1]), "{read:?}");
    let whole: Arc<Mutex<Vec<usize>>> = Arc::default();
    probed(&whole).parse(&text).expect("the text parses");
    assert!(whole.lock().unwrap().iter().all(|len| *len == text.len()));
}

/// In a parse from a reader a window at a time, `Context::source` is empty,
/// where a parse of the whole text gives the source (DIVERGENCE.md, "What
/// a parse from a reader shows of its source in Rust"): a grammar that
/// declares `lex.window` reads the text through the lexer's window.
#[test]
fn a_windowed_contexts_source_is_empty() {
    let sources: Arc<Mutex<Vec<String>>> = Arc::default();
    let seen = Arc::clone(&sources);
    let mut parser = Tabnas::make_json();
    parser
        .set_options(|options| options.lex.window = true)
        .expect("lex.window sets");
    parser.subscribe_rules(move |_rule, context| {
        seen.lock().unwrap().push(context.source.clone());
    });
    let text = r#"{"a":[1,2,{"b":null}]}"#;
    parser
        .parse_reader(text.as_bytes())
        .expect("the reader parses");
    let read = std::mem::take(&mut *sources.lock().unwrap());
    assert!(!read.is_empty());
    assert!(read.iter().all(String::is_empty), "{read:?}");
    parser.parse(text).expect("the text parses");
    let whole = std::mem::take(&mut *sources.lock().unwrap());
    assert!(whole.iter().all(|source| source == text), "{whole:?}");
}
