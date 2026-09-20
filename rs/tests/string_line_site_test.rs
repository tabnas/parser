// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! An embedded line character in a single-line string is reported at its
//! own position, as TypeScript (`pnt.sI = sI; pnt.cI = cI` before the
//! `bad()` call in `ts/src/lexer.ts`) and Go (`go/lexer.go`, "Position the
//! token ON the offending character") both do. The Rust lexer's other
//! control-character branch already did; the line-character branch used
//! the opening quote's point, so every embedded newline reported at the
//! start of its string.

use tabnas::Tabnas;

#[test]
fn embedded_line_character_is_sited_on_itself() {
    let parser = Tabnas::make_json();
    for (source, pos, col) in [
        ("\"a\nb\"", 2, 3),
        ("\"ab\rc\"", 3, 4),
        ("[1,\"x\ny\"]", 5, 6),
        // Control: the non-line control branch, which was already right.
        ("\"a\tb\"", 2, 3),
    ] {
        let error = parser.parse(source).unwrap_err();
        assert_eq!(error.code, "unprintable", "{source:?}");
        assert_eq!(
            (error.pos, error.row, error.col, error.len),
            (pos, 1, col, 1),
            "{source:?}: {error:?}"
        );
        assert_eq!(error.token.src, source[pos..=pos].to_string(), "{source:?}");
    }
}
