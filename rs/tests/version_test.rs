use tabnas::VERSION;

#[test]
fn test_version() {
    assert_eq!(VERSION, env!("CARGO_PKG_VERSION"));
}
