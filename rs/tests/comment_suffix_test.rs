use tabnas::{lexer::Lexer, Tabnas};

#[test]
fn serialized_comment_suffix_callback_consumes_only_its_owned_prefix() {
    let mut parser = Tabnas::new();
    parser.comment_suffix_ref("@end", |source| {
        source.starts_with("!END").then(|| "!END".to_string())
    });
    parser
        .grammar_json(
            r##"{"options":{"comment":{"def":{"hash":{
              "line":true,"start":"#","lex":true,"suffix":"@end"
            }}}}}"##,
        )
        .unwrap();

    let mut lexer = Lexer::new("# body !ENDtail\n", parser.options);
    assert_eq!(lexer.next_raw_token().unwrap().src, "# body !END");
    assert_eq!(lexer.next_raw_token().unwrap().src, "tail");
}

#[test]
fn invalid_callback_spans_are_ignored_without_cursor_mutation() {
    let mut parser = Tabnas::new();
    parser.comment_suffix_ref("@bad", |_| Some("not-a-prefix".into()));
    parser
        .grammar_json(
            r##"{"options":{"comment":{"def":{"hash":{
              "line":true,"start":"#","lex":true,"suffix":"@bad"
            }}}}}"##,
        )
        .unwrap();

    let mut lexer = Lexer::new("# body\n", parser.options);
    assert_eq!(lexer.next_raw_token().unwrap().src, "# body");
    assert_eq!(lexer.next_raw_token().unwrap().name, "#LN");
}

#[test]
fn unknown_at_prefixed_suffix_remains_a_static_literal() {
    let mut parser = Tabnas::new();
    parser
        .grammar_json(
            r##"{"options":{"comment":{"def":{"hash":{
              "line":true,"start":"#","lex":true,"suffix":"@literal"
            }}}}}"##,
        )
        .unwrap();

    let mut lexer = Lexer::new("# body @literaltail\n", parser.options);
    assert_eq!(lexer.next_raw_token().unwrap().src, "# body @literal");
    assert_eq!(lexer.next_raw_token().unwrap().src, "tail");
}
