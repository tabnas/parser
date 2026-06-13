# Differences from TypeScript

The TypeScript version is the authoritative implementation. The Go version is
a faithful port of the engine behavior — same packaging (grammar-free
engine), same lexer structure, same error model — with deliberate Go-only
additions for Go client code, listed below.

## Packaging: Aligned (Grammar-Free Engine)

Both packages are grammar-free engines. In TypeScript every grammar
(including strict JSON) arrives via a plugin, and the strict-JSON grammar
lives as a test fixture (`ts/test/json-plugin.ts`). In Go the engine is
`github.com/tabnas/parser/go` and the relaxed-JSON (jsonic-style) grammar
is the separate plugin package `github.com/tabnas/parser/go/jsonic`:

| Need | Use |
|---|---|
| Relaxed JSON out of the box | `jsonic.Parse(src)` / `jsonic.Make(opts...)` |
| Strict JSON | `jsonic.MakeJSON()` or `Rule.Include: "json"` |
| Bare engine, own grammar | `tabnas.Make()` + `Rule`/`Grammar` |
| Grammar as a plugin | `j.Use(jsonic.Plugin)` |

The engine's text-form convenience APIs (`SetOptionsText`, `GrammarText`)
need a parser for their text argument; grammar packages register one via
`tabnas.RegisterTextParser` (importing `jsonic` does this automatically,
in the manner of database/sql drivers).

Both runtimes run the shared fixtures under `test/spec/`: the Go jsonic
package runs all of them; TypeScript runs the strict-JSON and utility
fixtures (`include-json*.tsv`, `utility-*.tsv`) via the json-plugin
fixture.

## Behavioral Differences

These affect parse output for the same input.

### Number + Text Tokenization

Aligned. Both lexers require an ender character after a number, so
`123abc` lexes as a single text token in both (TS via the ender-anchored
number regexp, Go via its not-a-number check). The shared fixture
`alignment-number-text.tsv` pins this behavior.

### Empty / Whitespace Input

Both implementations short-circuit exact empty-string input (`""`).
Whitespace/comment-only input is processed through the normal parse flow in both
implementations and resolves to `null`/`nil` by grammar behavior.

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

## Plugin Differences

| Area | TypeScript | Go |
|---|---|---|
| Plugin signature | `(tabnas, opts?) => void \| Tabnas` | `func(j *Tabnas, opts map[string]any) error` |
| Plugin failure | throw | returned `error` |
| Rule definer | `(rs: RuleSpec, p: Parser) => void \| RuleSpec` | `func(rs *RuleSpec, p *Parser)` (no replacement return) |
| State actions raising errors | Return an error `Token` | Set `ctx.ParseErr` (same effect: parse halts with the error) |
| Plugin defaults | `.defaults` property on the function | `UseDefaults(plugin, defaults)` |
| Option namespacing | Plugin options merged by name | `PluginOptions` / `SetPluginOptions` |

## Go-Specific Features

These are available only in the Go version. They exist for Go client code
(typed access to parse metadata) and are intentionally kept.

### `Info.Text` Option (`TextInfo`)

Wraps string and text values in a `Text` struct that preserves the quote
character used:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{Text: boolp(true)}})
result, _ := j.Parse(`'hello'`)
// result: tabnas.Text{Quote: "'", Str: "hello"}
```

### `Info.List` Option (`ListRef`)

Wraps arrays in a `ListRef` struct with metadata:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{List: boolp(true)}})
result, _ := j.Parse("a, b, c")
// result: tabnas.ListRef{Val: []any{"a", "b", "c"}, Implicit: true}
```

### `Info.Map` Option (`MapRef`)

Wraps objects in a `MapRef` struct with metadata:

```go
j := tabnas.Make(tabnas.Options{Info: &tabnas.InfoOptions{Map: boolp(true)}})
result, _ := j.Parse("a:1")
// result: tabnas.MapRef{Val: map[string]any{"a": 1.0}, Implicit: true}
```

### `jsonic.MakeJSON()`

Constructs an instance restricted to strict JSON (rejects all tabnas
relaxations). Mirrors what the TS json-plugin test fixture provides.

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
| `\uXXXX` surrogate pairs | Implicit (UTF-16 strings) | Explicitly combined |
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
- `go test -fuzz=FuzzParse ./jsonic/` exercises the guarantee against
  arbitrary byte input.

## Type System

TypeScript returns untyped `any`. Go returns `any` but the concrete types are
predictable:

| Value | Go Type |
|---|---|
| Objects | `map[string]any` (or `MapRef` with option) |
| Arrays | `[]any` (or `ListRef` with option) |
| Strings | `string` (or `Text` with option) |
| Numbers | `float64` |
| Booleans | `bool` |
| Null | `nil` |
