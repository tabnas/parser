use std::fs::File;
use std::io::{BufRead, BufReader};
use std::path::Path;
use tabnas::lexer::Lexer;
use tabnas::options::Options;
use tabnas::{CommentDef, Value, ValueDef, TIN_ZZ};

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
        ("+1.5e-3", 0.0015),
        ("2.e3", 2000.0),
        ("1.", 1.0),
    ] {
        let mut lexer = Lexer::new(source, Options::default());
        let token = lexer.next_token().expect("number token");
        assert_eq!(token.val, Value::Number(expected), "source {source}");
    }
}

#[test]
fn decimal_separator_edges_fall_through_to_text() {
    for source in ["+_1", "1_", "1.5_", "1e_2", "1e2_"] {
        assert_eq!(
            lex(source, Options::default()).unwrap(),
            format!("#TX:{source}")
        );
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

#[test]
fn builtin_lexer_enable_switches_fall_through_to_text() {
    let mut number = Options::default();
    number.number.lex = false;
    assert_eq!(lex("12", number).unwrap(), "#TX:12");

    let mut string = Options::default();
    string.string.lex = false;
    assert_eq!(lex(r#""x""#, string).unwrap(), r#"#TX:"x""#);

    let mut value = Options::default();
    value.value.lex = false;
    assert_eq!(lex("true", value).unwrap(), "#TX:true");

    let mut space = Options::default();
    space.space.lex = false;
    assert_eq!(lex("a b", space).unwrap(), "#TX:a b");

    let mut custom_space = Options::default();
    custom_space.space.chars = "_".into();
    let mut lexer = Lexer::new("_", custom_space);
    assert_eq!(lexer.next_raw_token().unwrap().name, "#SP");
}

#[test]
fn configured_line_characters_rows_and_single_mode_are_honored() {
    let mut options = Options::default();
    options.line.chars = ";".into();
    options.line.row_chars = ";".into();
    let mut lexer = Lexer::new(";;x", options);
    let line = lexer.next_raw_token().unwrap();
    let text = lexer.next_raw_token().unwrap();
    assert_eq!((line.name.as_str(), line.src.as_str()), ("#LN", ";;"));
    assert_eq!((text.name.as_str(), text.ri, text.ci), ("#TX", 3, 1));

    let mut single = Options::default();
    single.line.single = true;
    let mut lexer = Lexer::new("\n\nx", single);
    assert_eq!(lexer.next_raw_token().unwrap().src, "\n");
    assert_eq!(lexer.next_raw_token().unwrap().src, "\n");
}

#[test]
fn configured_string_escape_replace_multiline_and_abandon_are_honored() {
    let mut options = Options::default();
    options.string.escape_char = '~';
    options.string.escape.insert('q', "Q".into());
    options.string.replace.insert('x', "yz".into());
    assert_eq!(lex(r#""x~q""#, options).unwrap(), "#ST:yzQ");

    let mut removed_escape = Options::default();
    removed_escape.string.escape.remove(&'n');
    removed_escape.string.allow_unknown = false;
    assert_eq!(lex(r#""\n""#, removed_escape).unwrap_err(), "unexpected");

    let mut multiline = Options::default();
    multiline.string.multi_chars = "\"".into();
    assert_eq!(lex("\"a\nb\"", multiline).unwrap(), "#ST:a\nb");

    let mut control = Options::default();
    control.string.allow_control = true;
    assert_eq!(lex("\"a\u{0007}b\"", control).unwrap(), "#ST:a\u{0007}b");

    let mut abandon = Options::default();
    abandon.string.abandon = true;
    assert_eq!(lex(r#""open"#, abandon).unwrap(), r#"#TX:"open"#);
}

#[test]
fn configurable_comment_definitions_suffixes_and_eatline_are_honored() {
    let mut default = Lexer::new("# note\nx", Options::default());
    assert_eq!(default.next_raw_token().unwrap().src, "# note");
    assert_eq!(default.next_raw_token().unwrap().name, "#LN");

    let mut options = Options::default();
    options.comment.definitions.clear();
    options.comment.definitions.insert(
        "short".into(),
        CommentDef {
            line: true,
            start: "#".into(),
            end: String::new(),
            lex: true,
            suffixes: Vec::new(),
            eat_line: false,
        },
    );
    options.comment.definitions.insert(
        "long".into(),
        CommentDef {
            line: true,
            start: "##".into(),
            end: String::new(),
            lex: true,
            suffixes: vec!["!".into()],
            eat_line: false,
        },
    );
    let mut lexer = Lexer::new("##a!x\n", options);
    assert_eq!(lexer.next_raw_token().unwrap().src, "##a!");
    assert_eq!(lexer.next_raw_token().unwrap().src, "x");

    let mut eat = Options::default();
    eat.comment.definitions.clear();
    eat.comment.definitions.insert(
        "semi".into(),
        CommentDef {
            line: true,
            start: ";".into(),
            end: String::new(),
            lex: true,
            suffixes: Vec::new(),
            eat_line: true,
        },
    );
    let mut lexer = Lexer::new("; note\nx", eat);
    assert_eq!(lexer.next_raw_token().unwrap().src, "; note\n");
    assert_eq!(lexer.next_raw_token().unwrap().src, "x");

    let mut block = Options::default();
    block.comment.definitions.clear();
    block.comment.definitions.insert(
        "block".into(),
        CommentDef {
            line: false,
            start: "<*".into(),
            end: "*>".into(),
            lex: true,
            suffixes: vec!["END".into()],
            eat_line: false,
        },
    );
    assert_eq!(
        Lexer::new("<* body ENDx", block)
            .next_raw_token()
            .unwrap()
            .src,
        "<* body END"
    );
}

#[test]
fn named_and_regex_values_and_text_enders_are_honored() {
    let mut exact = Options::default();
    exact.value.definitions.insert(
        "yes".into(),
        ValueDef {
            val: Some(Value::Bool(true)),
            matcher: None,
            transform: None,
            consume: false,
        },
    );
    exact.value.definitions.insert(
        "12".into(),
        ValueDef {
            val: Some(Value::String("dozen".into())),
            matcher: None,
            transform: None,
            consume: false,
        },
    );
    assert_eq!(lex("yes", exact.clone()).unwrap(), "#VL:true");
    assert_eq!(lex("12", exact).unwrap(), "#VL:dozen");

    let mut regex = Options::default();
    regex.value.definitions.insert(
        "at".into(),
        ValueDef {
            val: None,
            matcher: Some(regex::Regex::new(r"^@[a-z]+").unwrap()),
            transform: None,
            consume: true,
        },
    );
    let mut lexer = Lexer::new("@abc-rest,", regex);
    let value = lexer.next_raw_token().unwrap();
    let rest = lexer.next_raw_token().unwrap();
    assert_eq!((value.name.as_str(), value.src.as_str()), ("#VL", "@abc"));
    assert_eq!((rest.name.as_str(), rest.src.as_str()), ("#TX", "-rest"));

    assert_eq!(lex("12abc", Options::default()).unwrap(), "#TX:12abc");

    let ender = Options {
        ender: vec!["END".into()],
        ..Default::default()
    };
    let mut lexer = Lexer::new("abcEND", ender.clone());
    assert_eq!(lexer.next_raw_token().unwrap().src, "abc");
    assert_eq!(lex("abcEX", ender).unwrap(), "#TX:abcEX");
}
