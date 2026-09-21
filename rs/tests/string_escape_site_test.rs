// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! A string escape error is sited and spanned as TypeScript and Go site
//! and span it. `DIVERGENCE.md` records that the two ports once cut
//! different bad-token spans for invalid escapes, repaired it, and kept
//! the sweep as a parity test in each (`TestStringErrorsPointAtTheConstruct`
//! in `go/divergence_test.go`, the same table in
//! `ts/test/divergence.test.js`), because the defect is silent: the codes
//! stay right and only the positions move. This is that table for the
//! Rust port, which reported an unknown escape on the backslash rather
//! than on the escape character, spanned an invalid fixed-width escape
//! as `\u` or `\x` alone rather than the six or four source characters
//! TypeScript cuts, and named a `}` in the token of an unterminated
//! braced escape that the source never contained.

use tabnas::Tabnas;

#[test]
fn string_escape_errors_point_at_the_construct() {
    let mut parser = Tabnas::make_json();
    parser
        .set_options(|options| {
            // Every escape form on, as the TypeScript and Go twins run
            // it; `allow_unknown` is off there too, or `\q` is accepted
            // and there is no error to position.
            options.string.escape_strict = false;
            options.string.allow_unknown = false;
        })
        .expect("string options are plain options");

    for (source, code, col, token) in [
        // Escape errors sit on the BACKSLASH and span the escape.
        (r#""\uZZZZ""#, "invalid_unicode", 2, r"\uZZZZ"),
        (r#""\xZZ""#, "invalid_ascii", 2, r"\xZZ"),
        (r#""\u{GG}""#, "invalid_unicode", 2, r"\u{GG}"),
        // An unknown escape sits on the escape CHARACTER.
        (r#""\q""#, "unexpected", 3, "q"),
        // A control character sits on the character itself.
        ("\"a\nb\"", "unprintable", 3, "\n"),
        // A truncated escape at end of input spans the partial digits.
        (r#""\x4"#, "invalid_ascii", 2, r"\x4"),
        (r#""\u41"#, "invalid_unicode", 2, r"\u41"),
        (r#""\u{42"#, "invalid_unicode", 2, r"\u{42"),
        // The fixed-width span is cut from the source, so it takes in
        // whatever follows short digits, the closing quote included.
        (r#""\u12""#, "invalid_unicode", 2, "\\u12\""),
        (r#""\u12G4""#, "invalid_unicode", 2, r"\u12G4"),
        (r#""ab\x4G""#, "invalid_ascii", 4, r"\x4G"),
        (r#""\u{}""#, "invalid_unicode", 2, r"\u{}"),
    ] {
        let error = parser.parse(source).unwrap_err();
        assert_eq!(error.code, code, "{source:?}");
        assert_eq!(
            (error.row, error.col, error.pos),
            (1, col, col - 1),
            "{source:?}: the point must sit on the construct"
        );
        assert_eq!(
            error.token.src, token,
            "{source:?}: the span must cover the construct"
        );
        assert_eq!(error.len, token.chars().count(), "{source:?}");
    }
}

#[test]
fn strict_escapes_report_the_same_sites() {
    // `escape_strict` routes `\x` and `\u{` to the unknown-escape and
    // fixed-width paths; the sites follow the path, as in TypeScript.
    let parser = Tabnas::make_json();
    for (source, code, col, token) in [
        (r#""\x41""#, "unexpected", 3, "x"),
        (r#""\u{41}""#, "invalid_unicode", 2, r"\u{41}"),
        (r#""\v""#, "unexpected", 3, "v"),
    ] {
        let error = parser.parse(source).unwrap_err();
        assert_eq!(error.code, code, "{source:?}");
        assert_eq!((error.col, error.pos), (col, col - 1), "{source:?}");
        assert_eq!(error.token.src, token, "{source:?}");
    }
}
