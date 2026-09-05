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
The complete condition tier receives the shared live `AltMatch` and lexer in
one callback, so plugins can perform canonical arbitrary-depth lookahead
without losing match-state mutations.
Reserved lifecycle references (`@rule-bo`, `-ao`, `-bc`, `-ac`, with
`/prepend`, `/append`, and `/replace`) bind through `state_action_ref` and are
wired when that serialized rule is installed.

Function token matchers use `match_token_ref`. They receive the remaining
source and return a non-empty owned source prefix plus its value. This
effect-based interface supports eager and parser-slot-gated matchers without
giving callbacks mutable access to the lexer cursor. Native plugins that need
the mature callback surface use `imperative_lex_match_ref`; those callbacks
receive the live lexer, rule, and context and may construct native tokens.
High-priority `options.match.value` entries support serialized regexps and
typed `match_value_ref` callbacks. Regexp entries can bind their capture
transform with `value_transform_ref`; function entries return the owned source
prefix and `#VL` value directly.

Regexp-backed value definitions can bind a named transformer with
`value_transform_ref`. It receives the whole match followed by capture groups
and returns the token value without direct cursor access.
Unquoted-text modifier pipelines use `text_modifier_ref` and retain serialized
declaration order. `imperative_text_modifier_ref` exposes the live lexer, rule,
context, and resolved options.
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
`config_modifier_with_options_ref` additionally exposes the immutable raw
option input for the configure pass. Raw options and resolved configuration are
kept separate so non-idempotent modifiers do not compound during overlays or
derivation.
Replacement `options.parser.start` entry points bind through typed simple,
instance-aware, and parent-context-aware registrations; they bypass the rule
engine like the TypeScript/Go option, and callback panics are returned as
structured internal errors.
Native plugins, grammar-wide group settings, plugin option bags, derived
instances, `empty`, and commutative instance `merge` are also available as
typed Rust APIs. Lazy token values, full pre-parse hooks, live lexer checks,
function comment suffixes, and setup-time matcher factories preserve their
canonical callback timing.

The strict-JSON grammar exposed by `Tabnas::make_json()` is a compatibility
fixture while the Rust plugin API is stabilized. It is not a built-in default
grammar: `Tabnas::new()` has no rules.

## Parity status

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
modifiers, errors, dynamic routing/backtracking, lifecycle state, and
effect-based or fully imperative token matchers. The live native surface
includes the resolved `AltMatch`, next-rule snapshots, live lexer access,
rule-spec inspection, per-rule lifecycle gates, parent-context seeding, token
detail bags, ignored trivia, negotiated re-lex checkpoints, and ordered
subscriber events. Callback panics become structured `internal` diagnostics at
public parser and lexer boundaries, retaining the active rule, complete rule
stack, token, and source position when a parse context exists. Debug
configuration, stable value formatting, instance descriptions, and opt-in
lexer/rule tracing are built in. Opt-in typed `MapRef`, `ListRef`, and `Text`
results expose parse metadata while serializing to the same plain JSON shape as
TypeScript. Rust directly executes every non-exempt shared TSV fixture; the
fixture paths are not copied into the crate. The repository gate requires every
non-exempt shared fixture to have a TypeScript, Go, and Rust runner, and the
strict-JSON differential gate compares token streams over every data row in
both JSON and parser corpora. Additional compiler-consumer gates compare Rust
against TypeScript over pure-data grammars emitted by the current ABNF, EBNF,
and GBNF compilers.

The portable serialized contract and native imperative tier have completed
their TypeScript/Go surface audit. Rust ownership is expressed explicitly:
grammar and next-rule views are immutable snapshots, live mutation is limited
to the `&mut Rule`, `&mut Context`, `&mut Lexer`, and `&mut AltMatch` arguments
supplied to a callback, and JavaScript function references are registered as
typed Rust callbacks before loading serialized JSON. Unsupported or mistyped
serialized callback forms fail at install time instead of being silently
ignored. See
`../doc/rust-port-implementation-plan.md` for the original architecture and
gates; the implementation has intentionally advanced beyond that document's
v0.1 scope.

## Development

```sh
cargo build --all-targets
cargo test --all-targets
cargo clippy --all-targets --all-features -- -D warnings
```

The crate declares Rust 1.85 as its minimum supported toolchain. From the
repository root, `./ci/rust/run.sh` also runs the shared cross-runtime and
compiler-consumer parity gates.
