//! A custom matcher that moves the cursor and then declines, as the xml
//! plugin's does: it steps over a byte-order mark at the start of the
//! source and goes on to look for a tag. TypeScript's matchers each start
//! at the live `lex.pnt`, and its first-character dispatch, read once when
//! a fetch begins, lists every built-in for a character from U+0100 up;
//! Go's matchers each start at the live `l.pnt`. Every answer below was
//! measured in both. `test/spec/bad-token.tsv` holds the byte-order-mark
//! rows that all three runtimes assert.

use std::sync::Arc;

use tabnas::lexer::Lexer;
use tabnas::{LexMatcher, Tabnas, TabnasError, Value};

const BYTE_ORDER_MARK: char = '\u{feff}';

/// One number or one text token: `text` in `test/spec/bad-token.tsv`.
const TEXT_GRAMMAR: &str = r##"{"options":{"rule":{"start":"top"}},"rule":{"top":{"open":[{"s":"#NR","a":"@value$"},{"s":"#TX","a":"@value$"}]}}}"##;

const TEXT_OFF: &str = r#"{"text":{"lex":false}}"#;

/// A matcher that steps over `skip` at the start of the source, one
/// character and one column, and declines; with `None` it only declines.
fn stepping_matcher(skip: Option<char>) -> LexMatcher {
    LexMatcher {
        name: "step".into(),
        order: 100_000.0,
        matcher: None,
        imperative: Some(Arc::new(move |lexer, _rule, _context| {
            if let Some(skip) = skip {
                if lexer.point().site.pos == 0 && lexer.remaining().starts_with(skip) {
                    lexer.advance_chars(1);
                }
            }
            None
        })),
        factory: None,
    }
}

fn parser(skip: Option<char>, options: &str) -> Tabnas {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(TEXT_GRAMMAR)
        .expect("install the grammar");
    parser
        .grammar_json(&format!(r#"{{"options":{options}}}"#))
        .expect("install the options");
    parser
        .options
        .lex
        .matchers
        .insert("step".into(), stepping_matcher(skip));
    parser
}

fn site(error: &TabnasError) -> (&str, usize, usize, usize, &str) {
    (
        error.code.as_str(),
        error.pos,
        error.row,
        error.col,
        error.token.src.as_str(),
    )
}

/// A matcher that steps over the last character leaves no character to
/// name: the error is `unexpected` at the end of the source, naming none,
/// in TypeScript and Go. Rust took a character there unconditionally and
/// panicked, which the engine reported as `internal`: in the line
/// matcher's branch for U+2028 and U+2029, in the string matcher for a
/// quote, and in the fallback for an unclaimed character, which a lone
/// byte-order mark under the xml plugin reached. A digit, a space and a
/// line feed did not panic, but they sent the number, space and line
/// matchers after a character that was no longer there.
///
/// The error stands where the matcher left the cursor: `advance_chars`
/// counts a row over a line feed, so that one is at 2:1, as it is in
/// TypeScript and Go when their matcher counts the row too.
#[test]
fn a_matcher_that_steps_over_the_last_character_leaves_unexpected_at_the_end() {
    for (skip, row, col) in [
        (BYTE_ORDER_MARK, 1, 2),
        ('\u{2028}', 1, 2),
        ('\u{2029}', 1, 2),
        ('"', 1, 2),
        ('1', 1, 2),
        (' ', 1, 2),
        ('\n', 2, 1),
    ] {
        for options in ["{}", TEXT_OFF] {
            let error = parser(Some(skip), options)
                .parse(&skip.to_string())
                .expect_err("nothing is left to parse");
            assert_eq!(
                site(&error),
                ("unexpected", 1, row, col, ""),
                "{skip:?} under {options}"
            );
        }
    }
}

/// The same at the lexer, which the register's `lex` probe drives:
/// TypeScript's `lex.next` gives a `#BD` with why `unexpected` at sI 1,
/// 1:2, naming nothing, and the Rust lexer gives that error.
#[test]
fn the_lexer_names_nothing_after_a_matcher_steps_over_the_last_character() {
    for options in ["{}", TEXT_OFF] {
        let parser = parser(Some(BYTE_ORDER_MARK), options);
        let mut lexer = Lexer::new("\u{feff}", parser.config());
        let error = lexer.next_raw_token().expect_err("nothing is left to lex");
        assert_eq!(
            (
                error.code.as_str(),
                error.pos,
                error.row,
                error.col,
                error.src.as_str()
            ),
            ("unexpected", 1, 1, 2, ""),
            "under {options}"
        );
    }
}

/// The matchers after the one that moved the cursor read the character it
/// moved to, and their tokens start there, as in TypeScript and Go. Rust
/// tested the character the fetch began on, so it lexed none of these,
/// and the one token it did make, `#TX` under text, started at the mark.
#[test]
fn matchers_after_a_moved_cursor_start_where_it_left_off() {
    for (input, options, name, source) in [
        ("\u{feff}1", "{}", "#NR", "1"),
        ("\u{feff}1", TEXT_OFF, "#NR", "1"),
        ("\u{feff} 1", "{}", "#SP", " "),
        ("\u{feff}\"a\"", "{}", "#ST", "\"a\""),
        ("\u{feff}\n1", "{}", "#LN", "\n"),
    ] {
        let parser = parser(Some(BYTE_ORDER_MARK), options);
        let mut lexer = Lexer::new(input, parser.config());
        let token = lexer.next_raw_token().expect("a token after the mark");
        assert_eq!(
            (
                token.name.as_str(),
                token.src.as_str(),
                token.site.pos,
                token.site.ri,
                token.site.ci
            ),
            (name, source, 1, 1, 2),
            "{input:?} under {options}"
        );
    }

    // A character no matcher claims is named where it stands, after the
    // mark. Rust named it at the mark.
    let error = parser(Some(BYTE_ORDER_MARK), TEXT_OFF)
        .parse("\u{feff}x")
        .expect_err("x is unclaimed with text off");
    assert_eq!(site(&error), ("unexpected", 1, 1, 2, "x"));
}

/// TypeScript chooses the matchers a fetch tries by the character it
/// begins on (`buildLexDispatch`), and a built-in that cannot start a
/// token with a Latin-1 character is not tried, even after a custom matcher
/// has moved the cursor onto a character it could match: `~1` is the text
/// `1`, `~"a"` is text, and `~ 1` meets no matcher at the space. Rust
/// follows TypeScript. Go tries every matcher and answers the number 1,
/// the string `a` and 1: a split between those two that this does not
/// settle.
#[test]
fn a_moved_cursor_keeps_the_matchers_the_first_character_chose() {
    let parser = parser(Some('~'), "{}");
    assert_eq!(parser.parse("~1").expect("text"), Value::String("1".into()));
    assert_eq!(
        parser.parse("~\"a\"").expect("text"),
        Value::String("\"a\"".into())
    );
    let error = parser.parse("~ 1").expect_err("the space is unclaimed");
    assert_eq!(site(&error), ("unexpected", 1, 1, 2, " "));
}

/// A custom matcher that declines during a negotiated re-cut leaves the
/// request standing for the matchers after it, as TypeScript's
/// `Lex.speculate` does. Rust ended the negotiation when it put the cursor
/// back, so the string matcher cut the `#ST` that the re-cut, asking for
/// `#TX`, was there to avoid, and `"a"` failed where TypeScript and Go
/// give the text `"a"`.
#[test]
fn a_custom_matcher_declining_during_a_recut_keeps_the_request() {
    let parser = parser(None, r#"{"lex":{"relex":true}}"#);
    assert_eq!(
        parser.parse("\"a\"").expect("re-cut as text"),
        Value::String("\"a\"".into())
    );
}
