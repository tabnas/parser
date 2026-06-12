# Differences from TypeScript

The TypeScript version is the authoritative implementation. The Go version is
a faithful port of the engine behavior, with deliberate Go-only additions for
Go client code, and one deliberate packaging difference described first.

## Packaging: Grammar Bundling

The TypeScript package is a grammar-free engine: it ships no grammar at all,
and every grammar (including strict JSON) arrives via a plugin. The strict
JSON grammar lives as a TS test fixture (`ts/test/json-plugin.ts`).

The Go module deliberately keeps the original tabnas (jsonic-style) relaxed
JSON grammar built in: `tabnas.Parse("a:1")` works out of the box, which is
the primary Go client use case. Use `Empty()` for a grammar-free instance, or
`MakeJSON()` / `Rule.Include: "json"` for the strict-JSON subset.

Both runtimes run the shared fixtures under `test/spec/`: Go runs all of
them against its bundled grammar; TypeScript runs the strict-JSON and
utility fixtures (`include-json*.tsv`, `utility-*.tsv`) via the json-plugin
fixture.

## Behavioral Differences

These affect parse output for the same input.

### Number + Text Tokenization

Input like `123abc` produces separate number and text tokens in the
TypeScript lexer but is rejected as not-a-number in Go (treated as text).
This keeps the original jsonic behavior of `a:123abc` → `{"a": "123abc"}`.

```
// TypeScript lexer: 123abc → number(123) + text("abc")
// Go:               123abc → text("123abc")
```

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
exception; Go returns `*TabnasError` as an `error` value.

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
| State actions | Can return error tokens | No return value |
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

### `MakeJSON()`

Constructs an instance restricted to strict JSON (rejects all tabnas
relaxations). Mirrors what the TS json-plugin test fixture provides.

## Internal Structure

The TypeScript lexer was refactored into a declarative scan-spec design
(`ScanSpec`, `scan()` state-machine driver, `guardedMatcher`, char-class
bitmaps, scan primitives exposed via the util bag). The Go lexer predates
this refactor and keeps the direct per-matcher structure. This is
behavior-neutral — both lexers produce the same tokens — but means the two
lexer sources no longer correspond function-for-function. Port the scan-spec
design if/when matcher-level extension parity is needed in Go.

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
