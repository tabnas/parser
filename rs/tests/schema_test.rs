use serde_json::Value;
use std::collections::BTreeSet;
use std::fs;
use tabnas::VERSION;

#[test]
fn error_registry_matches_the_rust_contract() {
    let registry: Value = serde_json::from_str(
        &fs::read_to_string("../schema/error-codes.json").expect("error-code registry"),
    )
    .expect("valid error-code registry");
    assert_eq!(registry["version"], VERSION);

    let actual: BTreeSet<&str> = registry["codes"]
        .as_object()
        .expect("registry codes")
        .keys()
        .map(String::as_str)
        .collect();
    let expected = BTreeSet::from([
        "cancel",
        "end_of_source",
        "invalid_ascii",
        "invalid_unicode",
        "unexpected",
        "unknown",
        "unknown_rule",
        "unprintable",
        "unterminated_comment",
        "unterminated_string",
    ]);
    assert_eq!(actual, expected);
}

#[test]
fn serialized_diagnostic_has_the_required_schema_fields() {
    let schema: Value = serde_json::from_str(
        &fs::read_to_string("../schema/diagnostic.schema.json").expect("diagnostic schema"),
    )
    .expect("valid diagnostic schema");
    let error = tabnas::Tabnas::make_json()
        .parse("{")
        .expect_err("malformed JSON");
    let diagnostic = serde_json::to_value(error).expect("serialized diagnostic");
    for field in schema["required"].as_array().expect("required fields") {
        let field = field.as_str().expect("field name");
        assert!(diagnostic.get(field).is_some(), "missing field {field}");
    }
    assert_eq!(diagnostic["status"], "failure");
    let allowed: BTreeSet<&str> = schema["properties"]
        .as_object()
        .expect("schema properties")
        .keys()
        .map(String::as_str)
        .collect();
    let actual: BTreeSet<&str> = diagnostic
        .as_object()
        .expect("diagnostic object")
        .keys()
        .map(String::as_str)
        .collect();
    assert!(
        actual.is_subset(&allowed),
        "unexpected fields: {:?}",
        actual.difference(&allowed)
    );
}
