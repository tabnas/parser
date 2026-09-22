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
    r##"{"rule":{"top":{"open":[{"s":"#NR","c":"@positive"}]}}}"##,
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

Cost, not only answers, is tracked against the canonical runtime. A rule
with N elements is flattened by the tree builders once per level of the
repetition that produced it, in every runtime: TypeScript spreads each
level's kids into the level above, and so does this crate, so both curves
are quadratic in the element count of one rule. This crate's constant was
an order of magnitude worse (`tabnas/proto` measured 46 s for 800 fields
against a tenth of a second) until a level whose own kids were still empty
stopped cloning the level below element by element and took its array by
handle, and `Rule::accept_child_node` stopped holding a second handle on a
parent's own accumulator. Measured on `list = item *( "," item )` compiled
with builtins, release profile, 4000 and 8000 items: TypeScript 0.38 s and
2.1 s, this crate 0.7 s and 6.8 s, down from 2.7 s at 4000 and beyond the
60 s budget at 8000. What remains is the algorithm the canonical runtime
also runs, at this crate's per-element cost for a refcounted value.

Two answers are deliberately this runtime's own, and both are recorded in
the repository's divergence record. Object key order is out of the
parsed-value contract (ADR-15): `Value::Object` keeps insertion order,
TypeScript's plain object puts integer-like keys first, and this crate must
never emulate that. A parse that sets no value answers `Value::Null`, where
TypeScript answers `undefined`; the engine's `Value::Undefined` is unwrapped
at the parse boundary on purpose.

The portable serialized contract and native imperative tier have been audited
against the TypeScript and Go surfaces, and the audit is executable rather
than asserted: the fixture registration gate, the token-stream differential
and the compiler-consumer gates are what hold it, and what the prose claims
is what those gates run. The claim is not that no difference remains. The
ABNF arm of the compiler-consumer gate found one in September 2026, on the
optional-prefix shape `R = [ A "@" ] A`: a parent read its child link from
the rule that popped, which is the last link of a replacement chain, where
TypeScript and Go both keep the link on the rule they pushed. That link is
now the pushed rule in whole, name and node together, and one narrower split
survives it: walking `rule.child.next.next` and beyond reaches the rest of a
replacement chain in TypeScript and Go and reaches nothing here. It is
registered, with a control row, as "Forward traversal of a replacement chain
in Rust". Known remaining API differences are listed under "Gaps against the
canonical surface" below.

Rust ownership is expressed explicitly:
grammar and next-rule views are immutable snapshots, live mutation is limited
to the `&mut Rule`, `&mut Context`, `&mut Lexer`, and `&mut AltMatch` arguments
supplied to a callback, and JavaScript function references are registered as
typed Rust callbacks before loading serialized JSON. Unsupported or mistyped
serialized callback forms fail at install time instead of being silently
ignored. See
`../doc/rust-port-implementation-plan.md` for the original architecture and
gates; the implementation has intentionally advanced beyond that document's
v0.1 scope.

## Gaps against the canonical surface

Audited against `ts/src` (`tabnas.ts`, `parser.ts`, `lexer.ts`, `rules.ts`,
`context.ts`, `utility.ts`) in September 2026. Parse behavior is not on this
list: where a behavior differs it is a defect, it is repaired, and until it is
repaired it lives in the repository's `DIVERGENCE.md` and its executable
register, `test/spec/divergent.tsv`, whose `rust` column this crate's own
suite asserts. One entry there is Rust's alone today, the chain walk named above.
What follows is public API surface that TypeScript offers a plugin author and
this crate does not, or offers in another shape.

- **The shared utility bag.** TypeScript exports a `util` object and this
  crate's `tabnas::utility` carries four of its members: `deep`, `modlist`,
  `str_value` and `str_inject`, which are the ones the shared `utility-*.tsv`
  fixtures pin. `escre`, `mesc`, `regexp`, `getpath`, `charset`,
  `charsBitmap`, `snip`, `srcfmt`, `clean`, `configure`, `filterRules`,
  `makelog`, `badlex`, `findTokenSet`, `keyOrder`, `recordKeyOrder` and
  `KEY_ORDER` have no public Rust equivalent. Several exist inside the crate
  and are simply not exported; the rest are internal to how the TypeScript
  engine is built. The members that only shim JavaScript (`isarr`, `omap`,
  `keys`, `values`, `entries`, `assign`, `clone`, `str`) have no meaning here
  and are not gaps.
- **The matcher factories.** TypeScript exports `makeCommentMatcher`,
  `makeFixedMatcher`, `makeLineMatcher`, `makeNumberMatcher`,
  `makeSpaceMatcher`, `makeStringMatcher`, `makeTextMatcher` and `makeLex`, so
  a plugin can build a matcher from config and install it beside the built-in
  bands. Here the eight bands are internal and a plugin reaches the same
  outcome through `lex_match_ref`, `imperative_lex_match_ref` and
  `lex_match_factory_ref`, which compose rather than construct. The
  capability is present; the shape is not the same.
- **Constructors exported for plugins.** `makeRule`, `makeRuleSpec`,
  `makeToken`, `makePoint` and `makeParser` are public in TypeScript. Here
  `Rule`, `Token` and `Parser` are constructed by the engine, and a plugin
  receives them.
- **Per-alternate validation of a grammar held as data.** TypeScript exports
  `validateAlt` and `validateAlts` alongside `validateGrammar`, and Go exports
  `ValidateAlt`/`ValidateAlts`, so a generator or an editor can check one
  alternate before any parser exists. This crate exposes
  `grammar::validate_grammar` over a whole rule table and nothing finer.
- **The builtin reference set is not public.** TypeScript exports
  `BUILTIN_REFS` and `BUILTIN_SCHEMA_VERSION`, and Go exports both under the
  same names, so a compiler emitting pure-data grammars can ask the engine
  which builtin references it honours. Here `BUILTIN_SCHEMA_VERSION` is public
  as `tabnas::grammar::BUILTIN_SCHEMA_VERSION` and is not re-exported at the
  crate root, and the name set is `pub(crate)`. The three sets are not
  identical between the runtimes, so exposing one here is a contract decision
  rather than a transcription, and it is left to the maintainer.
- **`internal()`.** TypeScript hands back one bag of engine internals. The
  same material is reachable here through named accessors (`config`,
  `rule_names`, `rule_specs`, `token_set`, `token_name`, `installed_plugins`,
  `describe`), which is a deliberate difference and not an absence.

## Building a release that uses this

The engine is a separate crate from whatever links it, so nothing in it
can be inlined into your code unless your binary asks for that. Put this
in the release profile of the crate that builds the final artefact:

```toml
[profile.release]
opt-level = 3
codegen-units = 1
lto = "fat"
```

`lto` is the one that matters and the one that is not on by default.
Without it, `tabnas/measure` records this port as **1.10x to 1.15x
slower on every case**: more than all but two of the twenty
optimisations in the engine's own history are worth individually. Go's
compiler inlines across packages within a binary by default and V8
inlines across module boundaries at runtime, so this is the flag that
puts a Rust consumer on the same footing rather than an unusual
tuning.

### Pick an allocator

The other two ports of this engine bring their own: Go has the
runtime's and TypeScript has V8's. A Rust binary takes whatever libc
supplies unless it says otherwise, and this engine allocates heavily
enough for that to matter. On glibc, `tabnas/measure` records
**1.09x to 1.56x** between the system allocator and `mimalloc`, the
larger end on the deeply nesting grammars.

```toml
[dependencies]
mimalloc = "0.1"
```

```rust,ignore
#[global_allocator]
static GLOBAL: mimalloc::MiMalloc = mimalloc::MiMalloc;
```

`mimalloc` is what the measurement harness uses; `jemalloc` and others
are reasonable too. The point is the choice, not the crate: leaving it
to the platform is itself a choice, and on glibc it is an expensive
one.

A library cannot set a profile or an allocator for its consumers,
which is why both of these are written down rather than configured.

## Development

```sh
cargo build --all-targets
cargo test --all-targets
cargo test --doc
cargo clippy --all-targets --all-features -- -D warnings
RUSTDOCFLAGS="-D warnings" cargo doc --no-deps
```

The crate declares Rust 1.85 as its minimum supported toolchain. From the
repository root, `./ci/rust/run.sh` also runs the shared cross-runtime and
compiler-consumer parity gates.

### Formal verification (experiment, not a gate)

`rs/verus/` holds standalone [Verus](https://github.com/verus-lang/verus)
copies of two pieces of `src/`, with specifications and machine-checked
proofs: the `InlineText` length invariant behind the crate's only
`unsafe` block, and the lexer's scalar-index span arithmetic.

Nothing there is compiled by the crate. Verus pins its own Rust
toolchain, which is not the 1.85 above, so no gate runs it and it is not
a build dependency. Run it by hand with `VERUS=/path/to/verus
rs/verus/run.sh`. `doc/rust-verus-experiment.md` records what verified,
what did not, and why the crate was not adopted onto Verus.
