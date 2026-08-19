# Differences from TypeScript

> **Looking for parity?** This is a PORTING guide — packaging, API shape,
> Go-specific helpers, plugin surface. Most of it describes how to write
> the same program twice, not places the two engines disagree.
>
> For "will these engines produce the same result for my input?", read
> [`DIVERGENCE.md`](../../DIVERGENCE.md) at the repository root, which is
> the single record of behavioural divergence and is deliberately short.

The TypeScript version is the authoritative implementation. The Go version is
a faithful port of the engine behavior — same packaging (grammar-free
engine), same lexer structure, same error model — with deliberate Go-only
additions for Go client code, listed below.

## Packaging: Aligned (Grammar-Free Engine)

Both runtimes are grammar-free engines that ship no grammar. In each, a
grammar (including strict JSON) arrives via a plugin, and the
strict-JSON grammar lives only as a test fixture — `ts/test/json-plugin.ts`
in TypeScript, `go/jsonplugin_test.go` (`package tabnas`, test-only) in
Go. The Go engine is `github.com/tabnas/parser/go` (package `tabnas`):

| Need | Use |
|---|---|
| Parse anything | `tabnas.Make()` + `j.Use(grammarPlugin)` |
| Bare engine, own grammar | `tabnas.Make()` + `Token`/`Rule`/`Grammar` |
| Grammar as a plugin | `j.Use(myPlugin)` |
| Restrict to a rule group | `Rule: &RuleOptions{Include: "json"}` |

A grammar plugin is a `func(j *Tabnas, opts map[string]any) error`. The
strict-JSON test fixture (`makeJSON` / `jsonPlugin` in
`go/jsonplugin_test.go`) is a worked example, but those helpers are
test-only and not importable by client code.

The engine's text-form convenience APIs (`SetOptionsText`, `GrammarText`)
need a parser for their text argument. The engine registers none; a
grammar package registers one via `tabnas.RegisterTextParser` (in the
manner of database/sql drivers), and until one is registered the
text-form APIs return an error.

Both runtimes run the same shared fixtures under `test/spec/`: the
strict-JSON set (`include-json*.tsv`) and the utility set
(`utility-*.tsv`). TypeScript runs them from `ts/test/json-spec.test.js`
and `ts/test/utility.test.js` (grammar from `ts/test/json-plugin.ts`); Go
runs them from `go/spec_test.go` and `go/utility_spec_test.go` (grammar
from `go/jsonplugin_test.go`).

Fixtures exercising *relaxed* grammar syntax — bare text, unquoted keys,
implicit structure — used to sit in `test/spec/` too, unexecuted, since
the strict-JSON test grammar rejects them by design and this engine ships
no grammar. They now live only in the grammar's own repo
([`tabnas/jsonic`](https://github.com/tabnas/jsonic), `test/spec/`),
where both of its runtimes run them.

## Behavioral Differences

These affect parse output for the same input.

### Negotiated Lexing (`lex.relex`)

Aligned. Both runtimes carry the opt-in lexer option — `lex: { relex: true }`
in TS, `Options.Lex.Relex` in Go — and it is **off by default** in both.

When set, a token-type mismatch in the alternate loop is no longer final:
the engine re-cuts the buffered token's source span constrained to the tins
the alternate itself names (`Lex.relex` / `Lex.Relex`), instead of failing
the alternate outright. Under it a `#BD` token is also a soft failure a
later alternate may renegotiate rather than an immediate throw — and when
no alternate can use it, the SAME diagnostic is raised, at the same token.

The mechanism is the same on both sides: save point + token queue, re-cut
under a wanted-tin filter across the match, fixed and builtin matcher
paths, restore on failure, plus the mismatch hook in the alternate loop.
Per-runtime notes:

- **The want filter.** `match` and `fixed` filter per candidate, so
  longest-match-wins still holds *among wanted candidates* — a shorter
  wanted token can beat a longer unwanted one, which is the point. A
  single-tin builtin (space, line, string, comment, number, text) is
  skipped when its tin is not wanted. Value matchers are skipped outright:
  they produce `#VL` by content, not by the alternate's tin list.
- **Custom matchers are speculated,** not skipped: their token identity is
  opaque, so the only way to learn whether one can serve the request is to
  run it and put the cursor back if what it produced is not wanted.
- **⚠ differs (cosmetic).** Go's fixed-token dispatch table is a
  whole-config summary, so under a want the fixed matcher falls through to
  the candidate list rather than trusting the table's single-byte answer.
  Same result, one array load slower on the renegotiation path only.
- **⚠ differs (cosmetic).** TS preserves a recut token's attached
  `ignored` token; Go's token carries no such field (its lexer skips
  ignored tokens in `Lex.Next` rather than attaching them), so there is
  nothing to preserve.
- **Go skips rule-position gating under a want, deliberately.** Go's
  match matcher carries a two-pass `positionExpected` scan that TS has
  no equivalent of (TS gates by token column instead). Under a want that
  scan is dead — `wants(tin)` is the gate — so Go does not run it, and
  skips the second pass entirely. No behavioural effect, and it matters:
  computing it anyway walked every alternate's slot-0 tins for every
  candidate token, costing 25–98x on scannerless grammars with many
  alternates (the llama.cpp GBNF corpus via `@tabnas/gbnf`: `json.gbnf`
  went 26.9ms → 273µs per parse of an 8-character input). Do not
  "restore parity" by reinstating the scan on the want path — there is
  no TS behaviour to be parity with.

It cannot widen the accepted language. A recut is returned only when its
tin is in the alternate's OWN list, so every position still requires
exactly what it always required; a wrong re-cut fails the parse rather
than satisfying anything. A `#BD` token never satisfies a position, not
even a wildcard one — without that, `#AA` would make the deferred throw
into an acceptance.

Practical impact is confined to **scannerless** front-ends. Grammars
written for a tokenising lexer distinguish their terminals lexically and
never contest a character, so the default is identical in both runtimes
for every ABNF and EBNF grammar in the shared fixtures. What needs the
option is the GBNF corpus (`tabnas/gbnf`), where two terminals may claim
the same character — `arithmetic.gbnf`'s `ws ::= [ \t\n]*` against a
literal `"\n"`, `json.gbnf`'s quote inside a string body against the
closing quote, `c.gbnf`'s keywords inside identifier classes.

Pinned by `go/relex_test.go` (the contested shapes, the option probe, the
over-acceptance guards) and `ts/test/lex.test.js`.

### Number + Text Tokenization

Aligned. Both lexers require an ender character after a number, so
`123abc` lexes as a single text token in both (TS via the ender-anchored
number regexp, Go via its not-a-number check). The fixture recording this
behavior needs a relaxed grammar to run, so it lives and is exercised
downstream: `alignment-number-text.tsv` in
[`tabnas/jsonic`](https://github.com/tabnas/jsonic)'s `test/spec/`.

Two exponent forms were previously misaligned and are now fixed in Go:

- **Trailing dot before an exponent** (`2.e3`, `0.e1`, `2.e+3`, `2.e-3`).
  TS's fraction group makes the digit optional, so these are numbers;
  `matchNumber` tested for trailing text before testing for an exponent
  and so abandoned the token. `isExponentStart` (`lexer.go`) carves the
  exponent out of that check. A bare `2.e` stays text in both, since TS's
  exponent group also requires a digit.
- **Base-prefixed integers beyond int64** (`0xFFFFFFFFFFFFFFFF`,
  `0o1000000000000000000000`). JS `Number("0x…")` evaluates the exact
  mathematical value of the digit string and rounds once to nearest
  float64; `strconv.ParseInt` fails with `ErrRange`, which used to drop
  the token to the text matcher, so the run lexed as `#TX` in Go and
  `#NR` in TS. `parseNumericString` now falls back to `exactBaseFloat`
  (big.Int → big.Float) on `ErrRange`. NOTE this is deliberately NOT
  "keep what ParseInt returned": that CLAMPS to MaxInt64, which agrees
  for `0x8000000000000000` only by coincidence and is off by 2x for
  `0xFFFFFFFFFFFFFFFF`. The in-range path stays on ParseInt.
- **Out-of-range exponents** (`1e999` → `Infinity`, `-1e999` →
  `-Infinity`). TS coerces with unary `+`, which saturates;
  `parseNumericString` treated `strconv.ParseFloat`'s `ErrRange` as a hard
  failure. It now keeps the saturated value ParseFloat already returns.
  Other `ParseFloat` errors still yield NaN and drop to the text matcher.
  Go returns no error at all for underflow, so `1e-999` → `0` and
  `-1e-999` → negative zero were already correct.

Both are pinned by `TestMatchNumberExponentTrailingDot` /
`TestMatchNumberExponentRange` (`go/lexer_edge_test.go`) and the matching
`number-exponent-trailing-dot` / `number-exponent-range` cases in
`ts/test/lex.test.js`.

### Raw Control Characters in Strings (`string.allowControl`)

Aligned. By default both lexers reject any control character (code point
below `0x20`) inside a string body with `unprintable`. `string.allowControl`
(`Options.String.AllowControl` in Go) relaxes that: control characters are
admitted verbatim as ordinary body text. Line-end characters are deliberately
NOT covered — they stay governed by `multiChars`, so a raw newline inside a
single-line string is still an error with the option set. The option exists
because some grammars' source-character rules admit raw control chars (JSON5's
`JSON5SourceCharacter` permits a literal tab); the default keeps the strict
behavior so no existing grammar changes.

Pinned cross-runtime by the shared fixture `test/spec/lex-string-control.tsv`
(`TestSpecLexStringControl` in `go/lexer_optionplumbing_test.go`,
`string-allow-control-spec` in `ts/test/lex.test.js`).

### Empty / Whitespace Input

Both implementations short-circuit exact empty-string input (`""`) before the
lexer or the rule loop is built, and return `lex.emptyResult` /
`Lex.EmptyResult` — default `undefined`/`nil` — or raise `unexpected` when
`lex.empty` / `Lex.Empty` is false. This is aligned, and it is the *only* path
for `""`: the rule-iteration budget (which is proportional to source length,
and so zero for a zero-length source) is never reached, so a grammar whose
empty value is not `undefined` must declare it via `lex.emptyResult` rather
than expect its start rule to run. Whitespace/comment-only input takes the
normal parse flow in both implementations and resolves by grammar behavior.

### Rule-Iteration Budget

The runaway guard is `2 * ruleCount * len(src) * 2 * rule.maxmul` in both.
Go additionally coerces a non-positive `MaxMul` to the default `3` and floors
the product at `100`; TS honors `rule.maxmul: 0` literally, which yields a zero
budget and an `unexpected` error. Only reachable by setting `rule.maxmul` to
zero or a negative number.

### Token Consumption

When no grammar alternate matches, both implementations raise an immediate
parse error. Token consumption behavior is aligned.

## Aligned Error Handling

Both implementations now share the same error model:

| Feature | TypeScript | Go |
|---|---|---|
| Message templates with `{key}` injection | `options.error` | `Options.Error` |
| Hint templates with `{key}` injection | `options.hint` | `Options.Hint` |
| Default per-code hints | yes | yes |
| Header name | `errmsg.name` | `ErrMsg.Name` |
| Suffix (bool / string / function) | `errmsg.suffix` | `ErrMsg.Suffix` |
| "See also" link line | `errmsg.link` | `ErrMsg.Link` |
| `--internal: tag=...; rule=...; token=...; plugins=...--` block | yes | yes |
| Instance tag when unset | `'-'` (`defaults.ts`) | `'-'` (`DefaultTag`, applied in `Make`) |
| Custom bad-token error code | `tkn.err` wins over `unexpected` | `tkn.Err` wins over `unexpected` |
| Source file name in `--> file:row:col` | `meta.fileName` | `ParseMeta` meta `"fileName"` |
| ANSI colors | `options.color` | `Options.Color` |
| Source site extract with caret | yes | yes |

The remaining difference is delivery: TypeScript throws `TabnasError` as an
exception; Go returns `*TabnasError` as an `error` value and never panics
(see "Error Delivery and the No-Panic Guarantee" below).

## Custom Matchers

TS `match.token` / `match.value` accept `RegExp | LexMatcher`. Go splits the
union across fields:

| TS | Go |
|---|---|
| `match.token[name] = RegExp` | `Match.Token[name] = *regexp.Regexp` |
| `match.token[name] = LexMatcher` | `Match.TokenFn[name] = LexMatcher` |
| `match.value[name].match = RegExp` | `Match.Value[name].Match` |
| `match.value[name].match = LexMatcher` | `Match.Value[name].Fn` |

Full custom matchers (with lexer ordering control) are available in both via
`lex.match` / `Options.Lex.Match`.

### Matcher `check` Hooks

Aligned. All eight built-in matchers accept a pre-match `check` hook —
`fixed`, `match`, `space`, `line`, `text`, `number`, `comment`, `string`
(`FixedCheck`, `MatchCheck`, ... on the Go `LexConfig`). Returning
`{done: true, token}` / `&LexCheckResult{Done: true, Token: t}` claims the
match; returning nothing falls through to the normal matcher.

TS previously declared and consulted `string.check` and `comment.check`
but never copied them out of the options, so those two hooks were dead
there while Go honoured all eight. Both runtimes now wire all eight, and a
matcher carrying a `check` opts out of TS's first-char dispatch table so
the hook runs for every input character, not just the ones the matcher
would normally claim.

## Plugin Differences

| Area | TypeScript | Go |
|---|---|---|
| Plugin signature | `(tabnas, opts?) => void \| Tabnas` | `func(j *Tabnas, opts map[string]any) error` |
| Plugin failure | throw | returned `error` |
| Rule definer | `(rs: RuleSpec, p: Parser) => void \| RuleSpec` | `func(rs *RuleSpec, p *Parser)` (no replacement return) |
| RuleSpec alternate/action lists | private; mutated via methods | private; mutated via methods (`AddOpen`/`PrependOpen`/`ModifyOpen`/`ClearOpen`, `AddBO`/`PrependBO`/`ClearActions`, `Fnref`) and read via getters (`OpenAlts`/`CloseAlts`/`Actions`/`HasBO…`) — aligned with TS; direct field assignment is no longer possible |
| Funcref `@x/append` vs plain `@x` | same slot (`fr['@x/append'] ?? fr['@x']`) | same slot (aligned) |
| Funcref dedup | by function identity | by function pointer (Go has no per-closure identity; reuse stable ref values) |
| State actions raising errors | Return an error `Token` | Set `ctx.ParseErr` (same effect: parse halts with the error) |
| Plugin defaults | `.defaults` property on the function | `UseDefaults(plugin, defaults)` |
| Option namespacing | Plugin options merged by name | `PluginOptions` / `SetPluginOptions` |

## Merge

Both runtimes implement instance merging (`a.merge(b)` / `a.Merge(b)`)
with the same commutative semantics: options conflict-check rather than
override, all rules carry over, shared rules interleave their
alternates deterministically, and both instances need distinct tags.
Differences:

| Area | TypeScript | Go |
|---|---|---|
| Signature / failure | `merge(other): Tabnas`, throws | `Merge(other *Tabnas) (*Tabnas, error)`, never panics |
| Named-action (fnref) renaming | fnref keys renamed `@x` → `@<tag>:x` (`$`-builtins kept) | none — Go persists no fnref map (`Grammar()` Ref maps are transient); lifecycle action slices carry the wired handlers |
| "Non-default" option detection | compared against the shared defaults tree — an explicitly-set default value still merges cleanly | nil/zero field = default; a field explicitly set to the default value on both sides with different values still conflicts (indistinguishable from intent) |
| Identical-alt / lifecycle dedupe | function reference identity, falling back to source-text equality (`fn.toString()`) — each plugin run creates fresh closures, so reference identity alone would miss shared base plugins | code-pointer identity (closures from one literal share a pointer) — the natural Go equivalent of source equality | 
| Conditioned-alt dedupe | only when the condition is reference-equal (or absent) | never (a condition cannot be proven identical across closures); unconditioned duplicates are unreachable, so both rules are behavior-safe |
| Option conflict paths | TS option names (`lex.match.same.make`) | lowercased Go field names, which coincide for most paths (`rule.maxmul`, `lex.match.same.make`) |

## Deep Option Merge (`util.deep` / `Deep`)

Aligned on opaque values. A value that is not a plain object/array —
a `RegExp` in TS, a struct with no exported fields (`*regexp.Regexp`,
`time.Time`, ...) in Go — **replaces** the base rather than being merged
into it. Merging into such a value cannot copy anything: TS's `for..in`
over a `RegExp` yields no keys (so the parent pattern silently survived a
child override), and Go's reflective field merge skipped every unexported
field and handed back a zero value (so `Deep(reA, reB)` produced a regexp
matching the empty pattern). Both now let the overlay win, which is what
`tn.make({number: {exclude: /new/}})` has always meant.

Structs with exported fields (the `Options` tree) still merge field by
field in Go, and plain objects/arrays still merge key by key in TS.
`undefined`/zero on the overlay side still loses in both.

## Go-Specific Features

These are available only in the Go version. They exist for Go client code
(typed access to parse metadata) and are intentionally kept. The examples
below install a grammar (`myGrammar`) that honours the `Info` options and
parse strict JSON; `Implicit` is `false` for braces/brackets and would be
`true` only for a grammar that creates containers implicitly (e.g. a
relaxed `a:1` → map).

### `GrammarSpecFromJSON` and the C ABI (`go/clib`)

Go-only, and needed only because Go is typed. `GrammarSpecFromJSON`
turns a serialized spec — `{"options":…, "rule":…, "v":N}` — into a
`*GrammarSpec`. TypeScript needs no equivalent: a parsed JSON object is
already structurally a `GrammarSpec` there, so `tn.grammar(JSON.parse(s))`
just works.

It is exported rather than left a test helper because it is the only way
a caller outside Go reaches the engine, which is what `go/clib` — the
C-ABI shared library — is built on. That library exists so languages
with no tabnas port can use the engine (Python via `ctypes` is the
motivating case); it stays grammar-agnostic, taking a serialized spec
and answering whether input parses. See [`../clib/README.md`](../clib/README.md).

One trap it removes: passing the whole serialized document as
`GrammarSpec{OptionsMap: …}` looks right and `Grammar()` returns no
error, but the rule block is never read, so the engine installs no rules
and every later parse quietly returns nothing.

### `Info.Text` Option (`TextInfo`)

Wraps string and text values in a `Text` struct that preserves the quote
character used:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{Text: boolp(true)}})
_ = j.Use(myGrammar)
result, _ := j.Parse(`"hello"`)
// result: tabnas.Text{Quote: `"`, Str: "hello"}
```

### `Info.List` Option (`ListRef`)

Wraps arrays in a `ListRef` struct with metadata:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{List: boolp(true)}})
_ = j.Use(myGrammar)
result, _ := j.Parse(`["a","b","c"]`)
// result: tabnas.ListRef{Val: []any{"a", "b", "c"}, Implicit: false}
```

### `Info.Map` Option (`MapRef`)

Wraps objects in a `MapRef` struct with metadata:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{Map: boolp(true)}})
_ = j.Use(myGrammar)
result, _ := j.Parse(`{"a":1}`)
// result: tabnas.MapRef{Val: map[string]any{"a": 1.0}, Implicit: false}
```

## Internal Structure: Scan-Spec Lexer (Aligned)

Both lexers use the declarative scan-spec design: a packed-action state
machine driver (`Scan` / TS `scan()`), per-byte class tables built by
`BuildCharRunSpec` / `BuildLineRunSpec` / `BuildStringBodySpec`, and a
shared matcher entry guard (`guardedMatch` / TS `guardedMatcher`). The
space, line, comment-eatline, and string-body walks all run on the driver,
and the scan primitives are exposed via the util bag in both runtimes so
plugin authors can build their own matchers on it. Both use a fallback
classifier beyond the fast-path table: TS for UTF-16 code units ≥ 256,
Go for UTF-8 lead bytes ≥ 0x80 (decoding the full rune); see the
Unicode section below.

## Serialized Regex Flags (`@/…/flags`)

Aligned, by translation rather than by copying.

A serialized grammar carries a regex terminal as `@/pattern/flags` (or
`@~/pattern/flags` for the eager form). That string is **shared** between
the runtimes, and it holds **JavaScript's** flags, because TypeScript
writes them natively. Go therefore lowers them to RE2 rather than passing
them through — copying them verbatim into an inline `(?flags)` group is
wrong twice over: RE2 rejects most of them outright, and accepts one with
an entirely different meaning.

| flag | Go | why |
|---|---|---|
| `i` `m` `s` | kept | same meaning in both engines |
| `u` | **dropped** | RE2 needs no equivalent — it is natively rune-based, which is what `u` asks JavaScript to be |
| `g` `y` `d` | dropped | they govern the JS matcher's statefulness and output (`lastIndex`, sticky, match indices), not the language matched; the engine calls `FindString` once per position |
| `v` | **refused** | unlike `u` it changes what a class MEANS (set operations, string literals inside classes), so it is not a no-op |
| anything else | refused | see `U` below |

### Why dropping `u` is sound

`u` is not cosmetic in TypeScript — without it a JS regex is **UTF-16
code-unit** based, and an astral character is two units. It is also not
confined to emoji grammars: a negated class (`[^\n]`) and `.` both need it,
because the complement of any set contains astral code points. The
question is only whether RE2's native behaviour already IS the flag's
behaviour. Measured, case by case:

| pattern | input | RE2 | JS `u` | JS without `u` |
|---|---|---|---|---|
| `^[a-z]$` | `q` | match | match | match |
| `^[\u{1F600}-\u{1F64F}]$` | 😀 | match | match | **does not compile** |
| `^[^\n]$` | 😀 | match | match | **no** |
| `^.$` | 😀 | match | match | **no** |
| `^.{2}$` | 😀 | no | no | **match** |
| `^.{2}$` | 😀😀 | match | match | **no** |
| `^[^\n]{2}$` | 😀 | no | no | **match** |

RE2 agrees with JS-**with**-`u` on every row and differs from JS-without
on five. In particular `.` consumes one astral character whole, and
`.{2}` correspondingly does **not** accept a single astral character as
two — a real bug once fixed on the TypeScript side, and the one this
translation must not reintroduce.

Both halves of that table are pinned, so this is an agreement between the
runtimes rather than a claim about RE2: `go/regexflags_test.go` and
`ts/test/regex-flags.test.js` assert the same patterns, inputs and
answers.

### Why an unknown flag is refused rather than ignored

RE2 accepts `(?U)` — and it means *swap greedy*. A letter passed through
because it was unrecognised could therefore change the language a grammar
matches, silently. Refusing is the safe default, and it is not silent
either: an unbuildable serialized regex leaves the original `@/…/` string
in place, which `MapToOptions` turns into an install error naming the
token.

### Two related non-equivalences this does NOT fix

Both predate the flag question and are independent of `u`:

- **`\s`** is ASCII-only in RE2 (`[\t\n\f\r ]`) and Unicode-aware in
  JavaScript (it includes NBSP, U+2028, …), with or without `u`.
- **`(?i)`** case-folds by Unicode rules in RE2, which matches JS `iu`
  rather than JS `i` alone.

A shared grammar that depends on either will differ between the runtimes,
which makes these DIVERGENCES rather than porting notes — this file is a
porting guide, and a different parse result for the same input belongs in
the parity record. They are now recorded in
[`DIVERGENCE.md`](../../DIVERGENCE.md) under "Regex dialect in serialized
terminals", with a parse-level reproduction pinned in both runtimes.
Prefer an explicit class over `\s` in a serialized terminal.

## Unicode / UTF-8

Both runtimes handle UTF-8 characters of all sizes (2/3/4-byte
sequences; BMP and astral planes) in keys, values, strings, comments,
and escapes, and both accept any Unicode character as a configured
matcher char (space/line/quote/ender sets) via their fallback
classifiers. The shared `include-json-utf8*.tsv` fixtures pin the
common surface. Mechanical differences:

| Area | TypeScript | Go |
|---|---|---|
| Scan unit | UTF-16 code units | UTF-8 bytes (runes decoded on demand) |
| Error columns | UTF-16 units (astral char = 2) | Runes (any char = 1) |
| Surrogate pairs (either escape spelling) | Implicit (UTF-16 strings) | Explicitly combined, on the code unit sequence |
| Lone surrogates | Preserved (JS strings allow them) | U+FFFD (matches encoding/json) |
| `\u{...}` braced escapes | 1-6 hex digits, ≤ U+10FFFF, else `invalid_unicode` | Same |
| Invalid UTF-8 input bytes | n/a (JS strings are UTF-16) | Passed through byte-for-byte, never a panic |

Column positions agree between the runtimes except for astral-plane
characters (TS counts 2, Go counts 1).

## Error Delivery and the No-Panic Guarantee

TypeScript throws `TabnasError`; Go returns errors — and the Go API
guarantees it **never panics**:

- Parsing wraps a recover guard that converts any panic (including
  panics thrown by plugin callbacks or custom matchers) into an
  `"internal"`-code `*TabnasError`.
- `Grammar` has the same guard for malformed specs.
- APIs that previously panicked now return errors: `Derive` returns
  `(*Tabnas, error)` (a failing plugin during child derivation mirrors
  TS `make()` throwing), and `MakeRuleCond` returns
  `(AltCond, error)` for unknown operators.
- `go test -fuzz=FuzzParse .` exercises the guarantee against
  arbitrary byte input.

## Type System

TypeScript returns untyped `any`. Go returns `any` but the concrete types are
predictable:

| Value | Go Type |
|---|---|
| Objects | `*OrderedMap` (insertion-ordered; `Map.Plain:true` → `map[string]any`, or `MapRef` with the info option) |
| Arrays | `[]any` (or `ListRef` with option) |
| Strings | `string` (or `Text` with option) |
| Numbers | `float64` |
| Booleans | `bool` |
| Null | `nil` |

## `options.tokenSet`

Both runtimes accept a `tokenSet` option and apply it identically from
either construction path — `Make(opts)` and `SetOptions(opts)` are
equivalent, as are TS `new Tabnas(opts)` and `tabnas.options(opts)`.
How the value combines with the built-in set differs:

| Area | TypeScript | Go |
|---|---|---|
| Type | `{ [name: string]: (string \| null)[] }` | `map[string][]string` |
| Combination with the default set | index-wise deep merge with `defaults.tokenSet`, so `{ KEY: ['#ST'] }` yields `[#ST, #NR, #ST, #VL]`; shortening a set needs explicit `null` padding (`['#ST', null, null, null]`) | replacement — `{"KEY": {"#ST"}}` yields `[#ST]`. Go's `Options` carries no `tokenSet` defaults to merge against (the defaults live in the config's `KeySet`/`ValSet`/`IgnoreSet`) |
| "Drop this entry" marker | `null` | `""` (an empty name is skipped) |

Both runtimes late-bind token-set references in rule alternates, so an
override applies to alternates that were declared before it. In Go the
declared names are kept on `AltSpec.SNames` and re-resolved against the
parsing instance; alternates built from raw `[]Tin` carry no names and
match exactly what they were given. The lexer's `match.token` gate (a
custom match token is only produced where the current rule position
expects it) reads the same late-bound slots, so adding a custom token to
`#KEY` / `#VAL` by override is enough to have it lexed.

## Rule Declaration Order

A TS `GrammarSpec.rule` object keeps insertion order for free; a Go map
has none. Go therefore records declaration order explicitly:

- `RuleSpec.Def` — a monotonically increasing definition index stamped
  when the spec is first created (`(*Tabnas).Rule`, `Grammar`,
  `GrammarText`, `MakeRuleSpec`). Redefining an existing rule does not
  renumber it. Zero means the spec was built as a bare struct literal.
- `(*Tabnas).Rules() []*RuleSpec` and `(*Tabnas).RuleNames() []string` —
  the grammar in declaration order. Unstamped specs sort last, by name,
  so the result is always deterministic. `RSM()` remains unordered.
- `GrammarSpec.RuleOrder []string` — declares the order of the `Rule`
  map's entries. Without it, `Grammar()` applies rules in sorted-name
  order (deterministic, but alphabetical rather than as-declared).
  `GrammarText` fills it in automatically from the source text's key
  order, so text grammars need not supply it.

## Per-parse error list — `ctx.errs` (TS) / `ctx.Errs` (Go)

Both runtimes carry a per-parse error list, appended at each error's
CONSTRUCTION site, so the error the parse reports is also the last
entry; a clean parse leaves it empty and every parse starts fresh.
Pinned by `ts/test/errs.test.js` and `go/errs_test.go`.

The shapes differ because the error channels do:

| | TypeScript | Go |
|---|---|---|
| Field | `ctx.errs: TabnasError[]` | `ctx.Errs []*TabnasError` |
| Recorded by | the `TabnasError` constructor, so every raise site — engine or plugin — records for free | `ctx.recordErr`, called at each engine construction site (`makeErrorIn`, plus the two `Lex.Next` raises and the deferred relex raise) |
| Guard | `try/catch` — a frozen array must not mask the error | a nil-safe receiver — a `Lex` built without a `Context` has no list |

`ctx.ParseErr` is unchanged and remains the grammar-facing error TOKEN
that halts the parse (documented in `doc/plugins.md`): it is a single
slot with set-once semantics that in-engine consumers and ~20 sibling
grammar repos rely on. `Errs` is additive and never replaces it.

One gap, deliberate: Go rejects an empty source in `parseInternal`
before any `Context` exists, so that one error cannot be recorded
(TS records it). It is unobservable today — no `Context` is reachable
— but the Go equivalent of TS's `{ value, errors }` result must
synthesize a one-element list there.

## Error recovery (`options.parse.recover`)

Both runtimes support opt-in panic-mode recovery: a parse error is
recorded, the lexer skips to a sync point derived from the live rule
stack × close-alternate group tags (with a structural fallback for
untagged grammars), the rule stack pops to a rule that accepts the sync
token, and parsing continues. Pinned by `ts/test/recover.test.js` and
`go/recover_test.go`.

| | TypeScript | Go |
|---|---|---|
| Enable | `parse: { recover: { enabled: true } }` | `Parse: &ParseOptions{Recover: &RecoverOptions{Enabled: true}}` |
| Results | `parse()` returns `{ value, errors }` | `ParseRecover()` returns `(value, errs, err)` |
| Sync tags | `syncGroups` (replaces the default set) | `SyncGroups` (same semantics) |
| Extra tokens | `syncTokens: ['#CA']` | `SyncTokens: []string{"#CA"}` |
| Caps | `maxSkip`, `maxRecoveries`, `suppress` | `MaxSkip`, `MaxRecoveries`, `Suppress` |

**Go returns a third value where TS changes the shape of the first.**
`Parse` is public and called throughout the fleet; returning a
`{value, errors}` struct through `any` would force every existing
caller into a type assertion just to learn whether they got a value or
a wrapper. `Parse` therefore keeps its signature and yields the partial
value with a nil error, and `ParseRecover` is how a caller asks what
was recovered from. Same constraint class as `Sub` and `ctx.ParseErr`.

Two Go-specific hazards the port has to handle, both stemming from Go
signalling lexer failure through a different channel than TS:

- **The lexer latches `Lex.Err`, and caches the `#ZZ` it answers while
  latched.** Clearing only the error leaves that cached end token in
  place, so every later fetch still reports end-of-source and recovery
  syncs on EOF — silently abandoning the rest of the document.
  Recovery clears both.
- **On unlexable input the lexer used to set `Lex.Err` and return
  `#ZZ`**, claiming end-of-source with source still ahead of the scan
  point — which ended recovery at the first bad character and abandoned
  the rest of the document. With recovery on, the lexer now hands the
  `#BD` token to the parser instead, exactly the deferral it already
  made for negotiated lexing, and exactly the condition TS defers on
  (`rules.ts`, where the throw is guarded by
  `!cfg.parse.recover.enabled`). The skip loop then walks the run token
  by token and counts it against `MaxSkip`.

  That deferral is what removed the duplicate diagnostics: one
  unlexable run is now one diagnostic. `{"a":true blah blip,"b":1}`
  reported three errors at a single offset before it and reports one
  after; `{"a": zzz, "b":2}` reported four.

With recovery on, neither runtime's parse fails outright: a
completeness failure after the rule loop is recorded as one more
diagnostic and the partial value still comes back.

### Unlexable runs: lexer soft mode

Aligned. With recovery on and relex off, an unlexable span is absorbed
at token-FETCH time rather than handed to the alternates, so the parse
continues as though it were not there and the text after it still
parses. Contiguous bad tokens coalesce into one diagnostic whose region
grows with the run, marked `Recovered.Bad` (TS: `recovered.bad`), and
the same `suppress` window recoveries use applies to a fresh run with
nothing consumed since the last one.

That is why an unlexable word is one squiggle rather than one per
character, and why two separate words are two.

Verified against TS on `{"a":true blah blip,"b":1}`:

| | TypeScript | Go |
|---|---|---|
| `suppress: 0` | 2 errors, `{"a":true,"b":1}` | 2 errors, `{"a":true,"b":1}` |
| `suppress: 8` | 1 error, `{"a":true,"b":1}` | 1 error, `{"a":true,"b":1}` |

Beyond `MaxSkip` the run gives up like any other over-long recovery,
and beyond `MaxRecoveries` the parse gives up — Go checks that cap
before recording rather than after, so the list does not overshoot.

The one remaining difference on these inputs is the `undefined`/`nil`
value-model split described above, not the diagnostics: a key whose
value never parsed is absent from `JSON.stringify` in TS and `null` in
Go, in both cases with the key present.

## Parse budget (`options.parse.budget`)

Both runtimes carry the opt-in cancellation/budget hook: a callback
runs every N rule-loop iterations and cancels the parse with the
`cancel` error code on a false return. Off by default in both, costing
one test per iteration. Pinned by `ts/test/budget.test.js` and
`go/budget_test.go`.

| | TypeScript | Go |
|---|---|---|
| Option | `parse: { budget: { checkEveryN, onCheck } }` | `Parse: &ParseOptions{Budget: &BudgetOptions{CheckEveryN, OnCheck}}` |
| Callback | `(ctx) => boolean \| void` | `func(ctx *Context) bool` |
| On cancel | throws `TabnasError('cancel')` | returns a `"cancel"` `*TabnasError` |

The callback signature is the one real difference. TS accepts
`boolean | void`, so a checker that only observes can return nothing;
Go has no undefined, so an observer returns `true`. Both halves of the
option are required in both runtimes — an interval with no checker, or
a checker with no interval, leaves the hook off rather than
half-enabled.

Cancellation is an ordinary error of the parse, so it is recorded in
`ctx.Errs` / `ctx.errs` as the last entry like any other. In TS's
recovery mode it surfaces through `{ value, errors }`; Go, still
fail-fast, returns it directly.

## Post-process rule event (`sub({ ruleDone })` / `SubRuleDone`)

Both runtimes carry the third subscriber kind: it fires AFTER each rule
pass, with the matched tokens recorded on the rule and the state
transition applied, so it can report what the pass actually did — which
the pre-process event cannot. This is the span-bearing structural
stream an outline provider is built from. Pinned by
`ts/test/ruledone.test.js` and `go/ruledone_test.go`.

| | TypeScript | Go |
|---|---|---|
| Subscribe | `tn.sub({ ruleDone })` | `tn.SubRuleDone(fn)` |
| Callback | `(rule, ctx, done) => void` | `func(rule *Rule, ctx *Context, done RuleDone)` |
| Payload | `{ state, alt: { b, g, p, r, err }, forced }` | `RuleDone{State, Alt: *RuleDoneAlt{B, G, P, R, Err}, Forced}` |
| Group tags | `g: string[]` | `G []string`, split from the comma-separated `AltSpec.G` |

**The subscription is its own method in Go, and that is forced.** TS's
`sub()` takes an options object, so a new event is a new key. Go's
`Sub(lexSub, ruleSub)` is positional and public, and every sibling Go
grammar repo calls it — widening it would break all of them for an
event most do not use. Same constraint class as `ctx.ParseErr`.

`Alt` is nil only when the rule state had no alternates at all. When it
had some and none matched, `Alt` is non-nil with just `Err` set,
mirroring TS's distinction between a null `_dalt` and a failed one —
collapsing the two would make a grammar hole read as a syntax error.
The `G` slice is a fresh copy on every event: `AltSpec.G` is live
grammar configuration and a consumer must not reach it through the
payload.

One gap, and it closes with A2: `Forced` marks a close synthesized by
error recovery, so it is always false in Go until Go has recovery.

## Lex-event retraction on unrelex — both runtimes

Under negotiated lexing, both runtimes re-announce the RESTORED token
to lex subscribers when a speculative recut is undone, so a
position-keyed consumer's reconstruction ends on the token the parse
actually proceeded with. Pinned by `ts/test/lexevents.test.js` and
`go/lexevents_test.go`.

The consumer contract (documented in `ts/doc/api.md` under `tn.sub`)
is identical in both: process events in order, keep the newest per
source position, and let each kept token's span shadow older events
inside its extent — which is what retracts the interior events an
abandoned speculation fired.

**One unit difference, and it matters to consumers doing the span
arithmetic**: TS `Token.sI`/`len` count UTF-16 code units, Go
`Token.SI` is a BYTE offset and spans are byte lengths. The contract
is the same; the arithmetic is in each runtime's own unit. A host
mapping either to editor positions (an LSP server) converts as it
already must for diagnostics.

## Continuations API (`tn.continuations(src)` / `Continuations`)

Both runtimes answer what tokens could legally follow a prefix — the
completion primitive of the unified-LSP design. Pinned by
`ts/test/continuations.test.js` and `go/continuations_test.go`, whose
expectations were checked against the OTHER runtime on the same
prefixes rather than restating each engine's own output.

| | TypeScript | Go |
|---|---|---|
| Call | `tn.continuations(src)` | `tn.Continuations(src)` |
| Returns | `{ tins, tokens }` | `(tins []Tin, names []string)` |
| Source of the sets | the collated per-rule `tcol` table | `AltSpec.S` directly |

**Go has no `tcol`**, the lookahead table TS collates at normalize
time, and does not need one: `AltSpec.S` already holds the per-position
tins, so every set here — a rule's openers, its close-leading tokens,
an alternate's next position — is read straight off the alternates.
`closeInfoOf` does the same for recovery. The information is identical;
only the intermediate table is absent.

Verified equal on the strict-JSON fixture for `""`, `{`, `{"a"`,
`{"a":`, `{"a":1`, `{"a":1}`, `[`, `[1` and `[1,`.

Shared semantics:

- **Path-aware.** Each alternate contributes only the position it is
  actually waiting on, so a sibling whose own prefix never matched adds
  nothing (`{"a"` yields `['#CL']`, not key starters). At a failure the
  set is recorded by the failing pass itself, while its own lookahead
  is still buffered.
- **Pop closure.** While a rule can close on anything, its parent's
  close continuations are legal here too.
- **Push closure.** An alternate fully matched at the query position is
  about to push another rule, so that rule's openers are legal — which
  is why `[1,` offers the next element's value starters as well as `]`.
- **Prefixes that parse still answer**, with `#ZZ` included to mean
  "stopping here is legal". It is a sentinel, not something a user
  types: a completion provider should drop it and read it as "this
  document is already valid".
- **Recovery never changes the answer.** The query forces its own parse
  fail-fast whatever the instance is configured for, since it must stop
  AT the query point rather than skip past it.

The set is an over-approximation in both: conditions and counters may
still reject a listed token.

The diagnostic `expected[]` field deliberately keeps its position-0
semantics in BOTH runtimes — it is pinned by the shared diagnostic.tsv
parity fixtures, and the improved computation lives in this API only.
