# tabnas for Rust

Rust port of the grammar-free Tabnas parser engine. TypeScript is the canonical
implementation; the shared fixtures in `../test/spec` define cross-port
behavior.

```rust
use tabnas::{Tabnas, Value};

let parser = Tabnas::new();
// Install rules and actions, then parse input with parser.parse(...).
```

Serialized function references are bound through typed Rust registrations
before the grammar is installed:

```rust
use tabnas::Tabnas;

let mut parser = Tabnas::new();
parser.alt_condition("@positive", |rule, _ctx| {
    rule.o0().is_some_and(|token| token.src != "0")
});
parser.grammar_json(
    r#"{"rule":{"top":{"open":[{"s":"#NR","c":"@positive"}]}}}"#,
)?;
# Ok::<(), tabnas::GrammarError>(())
```

Typed registrations cover conditions (`c`), modifiers (`h`), errors (`e`),
and dynamic push, replace, and backtrack fields (`p`, `r`, `b`). Modifiers use
an explicit take-and-return contract, and missing or mistyped references fail
transactionally during grammar installation.

Function token matchers use `match_token_ref`. They receive the remaining
source and return a non-empty owned source prefix plus its value. This
effect-based interface supports eager and parser-slot-gated matchers without
giving callbacks mutable access to the lexer cursor.
High-priority `options.match.value` entries support serialized regexps and
typed `match_value_ref` callbacks. Regexp entries can bind their capture
transform with `value_transform_ref`; function entries return the owned source
prefix and `#VL` value directly.

Regexp-backed value definitions can bind a named transformer with
`value_transform_ref`. It receives the whole match followed by capture groups
and returns the token value without direct cursor access.
Unquoted-text modifier pipelines use `text_modifier_ref` and retain serialized
declaration order.
Matcher-family `check` hooks use `lex_check_ref`; callbacks can continue,
skip that matcher, or emit an owned non-empty prefix token.
Priority-ordered custom lexer entries bind through `lex_match_ref` and are
interleaved with the eight built-in matcher bands. During negotiated lexing,
unwanted custom token effects are discarded without moving the cursor.
Custom token identities can be allocated explicitly with `Tabnas::token`, or
implicitly by a serialized rule/token-set reference; `LexCheckToken::named`
resolves the numeric identity after grammar installation.
Function-form comment terminators use `comment_suffix_ref` and likewise
consume only a validated owned source prefix.
Serialized `error`, `hint`, `errmsg`, and `color` overlays configure both
structured diagnostic text and human-readable error rendering, including
source context, links, and internal suffix suppression or replacement.
Dynamic suffix references bind through `error_suffix_ref` and receive an
owned snapshot of the rendered diagnostic and its color palette.
Named parse lifecycle hooks bind through `parse_prepare_ref` and
`parse_budget_ref`; serialized prepare maps are deterministic by callback name
and budget callbacks remain inactive until `checkEveryN` is non-zero.
Load-time `options.config.modify` callbacks bind through
`config_modifier_ref`, run in declaration order after option resolution, and
are reapplied on later grammar option overlays until removed.

The strict-JSON grammar exposed by `Tabnas::make_json()` is a compatibility
fixture while the Rust plugin API is stabilized. It is not a built-in default
grammar: `Tabnas::new()` has no rules.

## Current scope

Implemented: ordered grammar rules, open/close/push/replace transitions,
inheritable `n` and `k` state, rule-local `u` state, named actions, built-in
lexers, Unicode-scalar source positions, structured diagnostics, and the shared
strict-JSON, lexer, diagnostic, utility, and divergence-register fixtures.
Serialized grammars also support declarative conditions, group filters,
load-bound builtin configuration, tree/value builtins, token and rule
subscribers, public parse actions with bounded mark/rewind history, parse
budgets, path-aware continuation queries, and opt-in panic-mode recovery with
structured recovery diagnostics. Opt-in negotiated lexing can re-cut contested
source spans for scannerless serialized grammars, including rollback when a
candidate alternate later fails. Typed named hooks cover alternate conditions,
modifiers, errors, dynamic routing/backtracking, and effect-based token
matchers. Opt-in typed `MapRef`, `ListRef`, and `Text` results expose parse
metadata while serializing to the same plain JSON shape as TypeScript. Rust
directly executes every non-exempt shared TSV fixture; the
fixture paths are not copied into the crate. The repository gate requires every
non-exempt shared fixture to have a TypeScript, Go, and Rust runner, and the
strict-JSON differential gate compares token streams over every data row in
both JSON and parser corpora.

Not yet equivalent to the mature TypeScript/Go engines. Ordered serialized
grammar loading supports static token/rule/action fields, rule removal,
alternate injection, metadata, and schema-version gating. Function-valued
callbacks that need direct lexer/rule re-entry and complete option overlays
are not yet equivalent.
Unsupported callback and matcher forms fail at install time instead of being
silently ignored. See
`../doc/rust-port-implementation-plan.md` for the architecture and gates for
that work. Do not present this crate as a drop-in replacement until those
surfaces and the differential harness land.

## Development

```sh
cargo build --all-targets
cargo test --all-targets
cargo clippy --all-targets --all-features -- -D warnings
```
