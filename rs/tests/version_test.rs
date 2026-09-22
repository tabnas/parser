/* Copyright (c) 2026 Richard Rodger, MIT License */

//! The engine version is declared in seven places across this repository and
//! every one of them has to agree. Two checks live here.

use tabnas::VERSION;

/// The baked-in constant must equal the crate's declared version.
#[test]
fn test_version() {
    assert_eq!(VERSION, env!("CARGO_PKG_VERSION"));
}

/// ...and the crate's version must equal the canonical runtime's.
///
/// `go/version_test.go` asks exactly this of Go, and `ts/test/version.test.js`
/// of TypeScript's own export, so a Go or TypeScript bump that skipped a site
/// fails before it ships. Rust had no such check: `rs/Cargo.toml` and
/// `rs/src/lib.rs` could agree with each other on a version nothing else in
/// the repository carried, and no gate in `.github/workflows/` reads the Rust
/// side at all. `v0.9.6` already shipped with `rs/Cargo.lock` left on the
/// previous version for want of a check that read it.
///
/// Deliberately fatal, never skipped: a version check that silently does not
/// run is the failure mode it exists to prevent.
#[test]
fn version_matches_the_typescript_package() {
    let manifest = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let path = manifest.join("..").join("ts").join("package.json");
    let raw = std::fs::read_to_string(&path).unwrap_or_else(|error| {
        panic!(
            "cannot read {}, so VERSION cannot be checked: {error}",
            path.display()
        )
    });
    let package: serde_json::Value =
        serde_json::from_str(&raw).expect("ts/package.json is not readable JSON");
    let declared = package["version"]
        .as_str()
        .expect("ts/package.json has no version field");
    assert_eq!(
        VERSION, declared,
        "VERSION drift: the tabnas crate is {VERSION} but ts/package.json is \
         {declared}. Every version site moves together; see AGENTS.md, \
         \"The Rust lockfile is a version site\"."
    );
}
