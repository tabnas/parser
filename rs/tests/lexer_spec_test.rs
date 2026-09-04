use std::fs::File;
use std::io::{BufRead, BufReader};
use std::path::Path;
use tabnas::lexer::Lexer;
use tabnas::options::Options;
use tabnas::{Value, TIN_ZZ};

fn unescape(input: &str) -> String {
    let mut output = String::with_capacity(input.len());
    let mut chars = input.chars().peekable();
    while let Some(ch) = chars.next() {
        if ch == '\\' {
            match chars.peek() {
                Some('n') => {
                    chars.next();
                    output.push('\n');
                }
                Some('r') => {
                    chars.next();
                    output.push('\r');
                }
                Some('t') => {
                    chars.next();
                    output.push('\t');
                }
                _ => output.push(ch),
            }
        } else {
            output.push(ch);
        }
    }
    output
}

fn rows(name: &str) -> Vec<Vec<String>> {
    let path = Path::new("../test/spec").join(name);
    BufReader::new(File::open(&path).unwrap_or_else(|error| panic!("{path:?}: {error}")))
        .lines()
        .skip(1)
        .map(|line| line.expect("fixture line must be readable"))
        .filter(|line| !line.trim().is_empty() && !line.starts_with('#'))
        .map(|line| line.split('\t').map(unescape).collect())
        .collect()
}

fn lex(input: &str, options: Options) -> Result<String, String> {
    let mut lexer = Lexer::new(input, options);
    let mut first = None;
    loop {
        let token = lexer.next_token().map_err(|error| error.code)?;
        if token.tin == TIN_ZZ {
            break;
        }
        if first.is_none() {
            let value = match token.val {
                Value::String(value) => value,
                value => value.to_string(),
            };
            first = Some(format!("{}:{value}", token.name));
        }
    }
    Ok(first.unwrap_or_else(|| "#ZZ:".to_string()))
}

#[test]
fn string_control_fixture() {
    for row in rows("lex-string-control.tsv") {
        let mut options = Options::default();
        options.string.allow_control = row[0] == "true";
        let actual = lex(&row[1], options).unwrap_or_else(|code| format!("ERROR:{code}"));
        assert_eq!(actual, row[2], "input {:?}", row[1]);
    }
}

#[test]
fn strict_escape_option_matches_json_escape_surface() {
    let mut options = Options::default();
    options.string.allow_unknown = false;
    options.string.escape_strict = true;

    for (source, expected) in [
        (r#""\x41""#, "ERROR:unexpected"),
        (r#""\u{41}""#, "ERROR:invalid_unicode"),
        (r#""\q""#, "ERROR:unexpected"),
        (r#""\u0041""#, "#ST:A"),
    ] {
        let actual = lex(source, options.clone()).unwrap_or_else(|code| format!("ERROR:{code}"));
        assert_eq!(actual, expected, "source {source:?}");
    }
}

#[test]
fn text_line_terminator_fixture() {
    for row in rows("lex-text-line-terminator.tsv") {
        let mut options = Options::default();
        options.line.lex = row[0] == "true";
        if row[1] == "true" {
            options.line.fixed.push('\u{2028}');
        }
        let actual = lex(&row[2], options).unwrap_or_else(|code| format!("ERROR:{code}"));
        assert_eq!(actual, row[3], "input {:?}", row[2]);
    }
}

#[test]
fn text_quote_fixture() {
    for row in rows("lex-text-quote.tsv") {
        let actual =
            lex(&row[0], Options::default()).unwrap_or_else(|code| format!("ERROR:{code}"));
        assert_eq!(actual, row[1], "input {:?}", row[0]);
    }
}

#[test]
fn token_offsets_are_bytes_and_diagnostic_positions_are_scalars() {
    let mut lexer = Lexer::new("é a true", Options::default());
    let first = lexer.next_token().expect("first text token");
    let second = lexer.next_token().expect("second text token");
    let third = lexer.next_token().expect("value token");
    assert_eq!((first.si, first.pos, first.ci), (0, 0, 1));
    assert_eq!((second.si, second.pos, second.ci), (3, 2, 3));
    assert_eq!((third.si, third.pos, third.ci), (5, 4, 5));
}

#[test]
fn configured_number_forms_are_honored() {
    for (source, expected) in [
        ("0xFF", 255.0),
        ("0o17", 15.0),
        ("0b1010", 10.0),
        ("1_000.5", 1000.5),
        (".25", 0.25),
    ] {
        let mut lexer = Lexer::new(source, Options::default());
        let token = lexer.next_token().expect("number token");
        assert_eq!(token.val, Value::Number(expected), "source {source}");
    }
}

#[test]
fn negative_number_boundaries_do_not_panic_and_keep_the_sign() {
    let mut lexer = Lexer::new("-", Options::default());
    let token = lexer.next_raw_token().expect("bare minus falls through");
    assert_eq!(token.name, "#TX");

    let mut lexer = Lexer::new("-0x10", Options::default());
    let token = lexer.next_raw_token().unwrap();
    assert_eq!(token.val, Value::Number(-16.0));

    let mut lexer = Lexer::new("-0", Options::default());
    let token = lexer.next_raw_token().unwrap();
    let Value::Number(number) = token.val else {
        panic!("expected number")
    };
    assert!(number.is_sign_negative());
}
