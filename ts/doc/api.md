# API Reference

tabnas exposes a single class — `Tabnas` — plus error and lexer
helpers. The package ships **no grammar** of its own; every grammar
arrives via a plugin.

## Construction

### `new Tabnas(options?)`

Create a parser instance. The class has no grammar by default; pass a
`plugins` array to apply one (or more) at construction time:

```js
const { Tabnas } = require('@tabnas/parser')

const tn = new Tabnas({ plugins: [myGrammarPlugin] })
tn.parse(src)
```

`options` is a [`TabnasOptions`](options.md) object — every field is
optional and merges with the defaults. The `plugins` field is the only
field that doesn't survive into `tn.options` after construction (it's
consumed by the `use()` calls the constructor makes internally).

For a bare instance with no defaults, no standard tokens, and no
grammar — useful as a base for building a parser from scratch — use
`tn.empty()`:

```js
const blank = tn.empty({ rule: { start: 'mything' } })
```

## Parsing

### `tn.parse(src, meta?, parent_ctx?)`

Parse a string and return the result.

```js
tn.parse(src)                         // depends on the active grammar
```

Non-string inputs are returned unchanged — handy when threading values
through plugin pipelines.

`meta` passes arbitrary data to plugins and rule actions (read off
`ctx.meta` inside actions). `parent_ctx` seeds the per-parse `Context`
with extra fields — used by the test harness; rarely needed in user
code.

### `ctx.errs`

Every parse `Context` carries `errs: TabnasError[]`, the per-parse
error list. Each `TabnasError` records itself there at construction,
so after a failed parse the thrown error is also
`err.internal.ctx.errs[errs.length - 1]` (today, fail-fast, always the
only entry). The list is strictly per-parse: every parse starts empty,
`parent_ctx` seeding never carries a parent's entries across, and a
clean parse leaves it empty. Recording is best-effort — a degenerate
context (missing or frozen `errs`) never prevents the error itself
from being raised.

This is the substrate for opt-in multi-error recovery
(`ts/doc/lsp-feasibility.md`): in a future recovery mode the engine
appends here and continues instead of throwing.

### `tn.continuations(src)`

Legal-continuation tokens after parsing `src` as a prefix — the
completion primitive of the unified-LSP design. Returns
`{ tins, tokens }` (numeric tins and their `#`-names, sorted).

The computation is **path-aware**: each alternate contributes only the
position it is actually waiting on, so a sibling alternate whose own
prefix never matched adds nothing (`{"a"` yields `['#CL']`, the colon,
not key-starters). It is widened two ways — a **pop closure** (while a
rule's close state has an empty catch-all alternate, the parent's
close continuations are legal too) and a **push closure** (an
alternate whose sequence is fully matched *and* whose backtrack leaves
the handover exactly here contributes its pushed rule's openers, which
is why `[1,` offers the next element's value starters as well as
`]`).

**Prefixes that parse successfully still answer.** Permissive grammars
read most mid-edit prefixes as complete documents (jsonic takes `{a:`
as an implicit null), and reporting nothing for them would leave
completion empty exactly where an editor asks for it. The set is
captured at each end-of-source fetch, while the rule stack is still
live, at the lookahead position actually being read; captures
accumulate, because the first can belong to an alternate that is later
rejected. `#ZZ` is then added unconditionally: the prefix parsed, so
stopping here is legal — and the close rule may have consumed an end
token that an earlier alternate had already buffered, which fires no
second lexer event to capture. A finished document reports `['#ZZ']` —
precisely "only the end may follow". An empty capture means the same
thing and is returned as such; only a document that produced no
end-of-source event at all (an empty source with `lex.empty` enabled
parses without ever running a lexer) falls back to the start rule's
openers.

Because `#ZZ` is a sentinel rather than something a user types, a
completion provider should drop it from the item list and read it as
"the document is valid as it stands".

Two cases are deliberately silent. An alternate that stopped matching
*before* the queried position contributes nothing — its next token
would have to replace something already typed rather than follow it.
Neither does an alternate whose backtrack uses the function form
(`b: (rule, ctx, alt) => n`): that resolves only while the alternate is
being matched, so the handover point is unknown here, and a missing
completion beats a wrong one.

Over-approximation caveat: conditions and counters may still reject a
listed token. Runs on a lazily-created fail-fast sibling, so it works
identically on recovery-enabled instances.

Note: the structured diagnostic's `expected[]` field intentionally
keeps its original position-0 semantics for now — it is pinned by the
cross-runtime `diagnostic.tsv` parity fixtures, and changes there land
with the Go parity phase.

## Instance Management

### `tn.make(options?)`

Derive a child instance with overridden options. The child inherits the
parent's plugin list and re-runs each plugin against the merged options
— so option-conditional grammar alternates get re-evaluated against the
child's settings.

```js
const restricted = tn.make({
  rule: { exclude: 'experimental' }   // strip alts tagged 'experimental'
})
```

After re-running plugins the child applies `rule.include` /
`rule.exclude` filtering on top.

### `tn.empty(options?)`

Create a bare instance with `defaults$: false`, `standard$: false`,
`grammar$: false`. No tokens, no rules, no anything. The starting
point for entirely custom parsers.

### `tn.merge(other)`

Combine this instance's grammar with another's, returning a **new**
instance; neither original is modified. The operation is commutative:
`a.merge(b)` and `b.merge(a)` produce instances with the same options,
the same rule alternates in the same order, and the same parse
behavior.

Both instances must carry distinct, non-default `tag` options (throws
otherwise). The result's tag is the sorted join, e.g. `'A~B'`.

**Options** are deep merged commutatively: a value present in only one
instance, equal in both, or differing only from the shared defaults
merges cleanly; a leaf set to *different non-default* values in both
throws naming the option path
(`merge: conflicting option values at rule.maxmul`). Two fixed-token
names claiming the same source string also throw. Lexer matchers
(`lex.match`) union by name and run in `(order, name)` order, so ties
are deterministic.

**Rules** from both instances are all present in the result. Alternates
of a rule defined on both sides are interleaved deterministically
(each side's alts keep their order relative to each other):

1. at the first differing lookahead position, token-name order decides;
2. when one token sequence is a prefix of the other, the longer sorts
   first (so empty-`s` catch-alls sort last);
3. identical sequences order by complexity — presence of `c`, `e`,
   `h`, `b`, counters, `a`, `u`, `k`, `p`, `r`, more complex first;
4. then by `g` group tags; a final tie falls to tag order.

Alts that are *identical* are emitted once — the shared-base-plugin
case, where both instances installed the same grammar plugin. Fields
compare by reference or, since each plugin run creates fresh closures,
by function source text; the source-based comparison applies only to
unconditioned alts (where the duplicate is unreachable anyway, so the
dedupe cannot change behavior). Lifecycle handlers dedupe the same way
— note a handler whose behavior differs *only* through its closure
environment (e.g. built by a shared helper factory on both sides)
dedupes to one copy. Token references are translated by name into the
merged instance's tin space; actions that captured raw tin *values*
from their source instance are not translatable and should read tokens
via `ctx.cfg` instead.

**Named actions** (`@ref` entries in each rule's fnref map) are renamed
with the source instance's tag — `@pairkey` from tag `A` becomes
`@A:pairkey` — so the two grammars' names cannot collide. `$`-suffixed
engine builtins stay unprefixed. Already-installed lifecycle handlers
(`bo`/`ao`/`bc`/`ac`) are carried as installed actions (concatenated in
tag order, deduped by identity); the renamed `@<tag>:<rule>-<phase>`
entries never re-trigger lifecycle auto-install. Note that a later
`@<rule>-<phase>/replace` fnref on a merged rule replaces the
concatenated handlers from *both* sides.

```js
const a = new Tabnas({ tag: 'A', fixed: { token: { '#AT': '@' } } })
a.rule('val', (rs) => rs.open([{ s: ['#TX', '#AT'] }]))

const b = new Tabnas({ tag: 'B', fixed: { token: { '#PC': '%' } } })
b.rule('val', (rs) => rs.open([{ s: ['#TX', '#PC'] }]))

const ab = a.merge(b)   // val: [TX AT], [TX PC] — parses both forms
```

Caveats: merge is defined over the option trees and rule maps —
grammar state injected outside options (direct config mutation,
hand-appended matchers) does not transfer. Merged instances are
runtime artifacts: alt actions are carried as resolved functions, so
the result is not re-serializable to a `GrammarSpec`.

## Configuration

### `tn.options`

Dual-shape: callable as a setter, indexable as a snapshot of the
merged option tree. Both forms work simultaneously.

```js
tn.options.comment.lex                // current setting (snapshot)
tn.options({ comment: { lex: false } })   // applies a partial change
tn.options()                          // returns a fresh copy of merged opts
```

Calling `options(change)` deep-merges into the live options, re-runs
`configure()`, and clones the parser with the new config. Reading
property paths gives the snapshot at the most recent set call.

### `tn.config()`

Returns a deep copy of the internal `Config` (the resolved, compiled
form of the options). Useful for debugging.

## Grammar

### `tn.rule(name?, definer?)`

Access or modify grammar rules.

- `tn.rule()` — returns the full `RuleSpec` map.
- `tn.rule(name)` — returns the `RuleSpec` for that rule name.
- `tn.rule(name, definer)` — calls `definer(rs, parser)` to modify or
  create the rule. Use `rs.open([...])` / `rs.close([...])` to add
  alternates, and `bo` / `ao` / `bc` / `ac` for the state-action hooks.
- `tn.rule(name, null)` — delete a rule.

```js
tn.rule('val', (rs) => {
  rs.open([
    { s: ['#OB'], p: 'map', b: 1, g: 'map,custom' },
  ])
})
```

See [Writing plugins](plugins.md) for the full alternate-spec and
state-action field lists.

### `tn.grammar(spec, settings?)`

Apply a `GrammarSpec` — a JSON-shaped declarative representation of
rule definitions, with function fields supplied as `@funcref` strings
resolved against `spec.ref`. Used by plugins that ship grammar as data
rather than code. `settings.rule.alt.g` appends group tags to every
alternate in the spec.

**The spec is not modified.** Installing works on a private copy, so the
object you pass in reads the same afterwards as before: `@funcref` strings
are still strings, `g` is still the comma-string you wrote, and the spec
still serialises to the grammar you authored. You can therefore install one
spec into several parsers, and serialise it before or after installing, in
any order.

Two consequences worth knowing:

- Function fields (real actions, conditions, errors) are carried into the
  copy **by reference**, so a spec may hold live functions and they run as
  written. Only the containing objects and arrays are copied.
- Lex matchers in `spec.options.match.token` are the exception and are
  **cloned**: a `RegExp` is rebuilt (keeping a user-set `eager$`) and a
  function matcher gets a delegating wrapper. Each engine annotates its
  matchers with its own token numbers, so sharing one matcher between
  engines would make the second corrupt the first.

Before 0.6.1 the TypeScript runtime resolved refs in place, so a spec came
back holding functions and serialising it after installing produced a
grammar with every action silently missing. Go always copied.

### `tn.token(ref)`

Dual-shape, like `options`: callable for lookup-or-create, and
indexable as a name → Tin map.

```js
tn.token('#OB')                       // Tin for the open-brace token
tn.token(12)                          // name for Tin 12
tn.token('#TL')                       // mints and returns a fresh Tin
tn.token.ST                           // map access (bare name, no '#')
```

The map is keyed by both `#XX` and bare `XX` forms.

### `tn.tokenSet(ref)`

Dual-shape: callable to look up a named set's Tin array, indexable as
a name → Tin[] map. Built-in sets:

| Name | Members |
|---|---|
| `IGNORE` | `#SP`, `#LN`, `#CM` (skipped during lex) |
| `VAL` | `#TX`, `#NR`, `#ST`, `#VL` (anything that can be a value) |
| `KEY` | `#TX`, `#NR`, `#ST`, `#VL` (anything that can be a map key) |

### `tn.fixed(ref)`

Look up the source-string ↔ Tin mapping for fixed (punctuation /
keyword) tokens.

## Plugins

### `tn.use(plugin, options?)`

Register a plugin and invoke it. The plugin receives the instance and
the merged plugin options:

```js
function foo(tn, opts) { /* … */ }
tn.use(foo, { x: 1 })
```

Plugins can return a wrapped instance (e.g. a `Proxy`) — `use()` will
return whatever the plugin returns, falling back to the instance:

```js
const wrapped = tn.use((tn) => new Proxy(tn, {
  // Tabnas uses ES #private state; bind methods to the underlying
  // instance so private-field access works through the Proxy.
  get(target, prop) {
    const v = target[prop]
    return 'function' === typeof v ? v.bind(target) : v
  }
}))
```

When `tn.make()` derives a child, the child re-runs every plugin
applied to the parent. Plugins should be idempotent (or guard
themselves against re-application).

## Events

### `tn.sub({ lex?, rule?, ruleDone? })`

Subscribe to lex and rule events. Multiple subscriptions are allowed
and fire in registration order.

```js
tn.sub({
  lex: (token, rule, ctx) => { /* … */ },
  rule: (rule, ctx)       => { /* … */ },
  ruleDone: (rule, ctx, done) => { /* … */ },
})
```

**The `lex` stream is a trace of lexer activity, not a token list.**
Re-served end tokens and rewind replays fire duplicate events, and
under negotiated lexing (`lex.relex`) a speculative recut fires an
event that a later failure retracts by re-announcing the RESTORED
token. The reconciliation contract for consumers reconstructing "the
tokens the parse used" (semantic tokens): process events in order,
keep the **newest event per source position**, and let each kept
token's **span shadow** any older events inside `[sI, sI + len)` —
a restored longer token thereby shadows stale events fired at interior
positions during the abandoned speculation.

`rule` fires **before** each rule pass — the rule has not matched
anything yet. `ruleDone` fires **after** the pass: the matched tokens
are recorded on `rule.o` / `rule.c` (so `rule.o0.sI`-style spans are
readable), the state transition has been applied, and `done`
(`RuleDone`) carries `{ state, alt, forced? }` — `state` is the pass
that ran (`'o'`/`'c'`), `alt` snapshots the matched alternate's
routing (`b` backtrack count, `g` group tags, `p` pushed rule, `r`
replacement rule, `err` failure token), and `forced: true` marks a
close notification synthesized for a rule force-popped during error
recovery. Structural consumers (outline, folding) pair open/close by `rule.i`
instance id. A rule replaced via `alt.r` **on its open pass** never
closes itself — its replacement continues. A replacement on a **close
pass** is different: that event *is* the rule's real close (the
strict-JSON `pair`/`elem` continuation loops work this way), so only
open-pass replacements are treated as non-closing.

## Identity

| Property | Description |
|---|---|
| `tn.id` | Unique-per-instance string id (`'Tabnas/<ts>/<rand>[/<tag>]'`). |
| `tn.parent` | Parent instance, if this was created via `parent.make()`. |
| `tn.toString()` | Returns `tn.id`. |

## Internals

### `tn.internal()`

Returns the internal-state record: `{ parser, config, plugins, sub,
mark, merged }`. The state itself lives in a hash-private field
(`#internal`); this method is the only public reader. Plugins use it
for things the public API doesn't surface; user code rarely needs it.

## Utilities

### `Tabnas.util` (static)

Bag of helpers for plugin authors. Also reachable per-instance via
`tn.util`. Members:

- **Object / merge** — `deep`, `clone`, `keys`, `values`, `entries`,
  `omap`, `clean`, `prop`.
- **Regex / text** — `regexp`, `escre`, `charset`, `mesc`, `srcfmt`,
  `str`, `tokenize`.
- **Config** — `configure`, `parserwrap`, `badlex`, `makelog`.
- **Error** — `errdesc`, `errinject`, `errmsg`, `errsite`,
  `strinject`, `trimstk`.
- **Lex scan primitives** — `scan`, `guardedMatcher`,
  `buildCharRunSpec`, `buildLineRunSpec`, `buildStringBodySpec`, and
  the scan-spec constants `CONSUME`, `IS_ROW`, `CI_RESET`, `STOP`,
  `STATE_MASK`. These drive the table-driven matcher state machine;
  plugin authors writing custom matchers can reuse them. See the
  matchers in `src/lexer.ts` for usage.

### Constants

`OPEN`, `CLOSE`, `BEFORE`, `AFTER`, `EMPTY`, `SKIP`, `S` — exported as
both named exports and `Tabnas.X` static members. Used in rule
definitions and state actions. `SKIP` is the deep-merge sentinel that
preserves the base value.

### Lexer factories

Lower-level matcher and machinery factories, rarely needed by users
but exported for advanced plugin authors and the engine itself:

`makeLex`, `makeParser`, `makeToken`, `makePoint`, `makeRule`,
`makeRuleSpec`, `makeFixedMatcher`, `makeSpaceMatcher`,
`makeLineMatcher`, `makeStringMatcher`, `makeCommentMatcher`,
`makeNumberMatcher`, `makeTextMatcher`.

## Types

The TypeScript types live in `src/types.ts` and are re-exported from
the main module. Notable shapes:

| Type | Description |
|---|---|
| `Tabnas` | Class type of the engine instance. |
| `TabnasOptions` | The full option shape (with `plugins?: Plugin[]`). |
| `TabnasInternal` | Shape returned by `tn.internal()`. |
| `Plugin` | `(tabnas, options?) => void \| Tabnas`, plus optional `defaults`. |
| `Tin` | Branded `number` for token ids. |
| `Token`, `Lex`, `Point` | Lexer types (also classes). |
| `Rule`, `RuleSpec`, `AltMatch`, `AltSpec` | Parser types. |
| `Config` | Internal compiled form of `TabnasOptions`. |
| `Context` | Per-parse state passed to rule actions and matchers. |
| `GrammarSpec`, `GrammarAltSpec` | Declarative-grammar JSON shapes. |
| `ScanSpec`, `ScanOut` | Scan-spec driver types for custom matchers. |

## Error Handling

A parse failure throws a `TabnasError` (extends `SyntaxError`). Its
enumerable fields:

| Property | Description |
|---|---|
| `code` | Error code (`'unexpected'`, `'unterminated_string'`, …). |
| `message` | Formatted, multi-line message including a source-context extract. |
| `details` | Structured details (e.g. `{ state: 'open' }`); may be empty. |
| `lineNumber` | Row of the offending token (1-based). |
| `columnNumber` | Column of the offending token (1-based). |
| `fileName` | From `meta.fileName`, if supplied to `parse()`. |
| `meta` | The `meta` passed to `parse()`. |
| `txts()` | Returns the resolved `{ msg, hint, site }` text parts. |

Customise messages and hints through the [`error` and `hint`
options](options.md#error). Error-code keys come from
`src/defaults.ts`.

## Module Exports

```js
const {
  Tabnas,            // the engine class
  TabnasError,       // error class (extends SyntaxError)

  // Step / state constants and the string-table
  OPEN, CLOSE, BEFORE, AFTER, EMPTY, SKIP, S,

  // Lower-level factories (rarely needed by users)
  makeLex, makeParser,
  makeToken, makePoint, makeRule, makeRuleSpec,
  makeFixedMatcher, makeSpaceMatcher, makeLineMatcher,
  makeStringMatcher, makeCommentMatcher, makeNumberMatcher, makeTextMatcher,

  // Utility bag (also Tabnas.util)
  util,
} = require('@tabnas/parser')
```

The package also exposes subpath exports for direct access to internal
modules: `@tabnas/parser/lexer`, `@tabnas/parser/utility`, and `@tabnas/parser/error`.

There is no bundled grammar export. Companion grammars and tooling are
separate packages (`@tabnas/abnf`, `@tabnas/debug`); the strict-JSON
grammar used by the tests is the fixture at `test/json-plugin.ts`.
