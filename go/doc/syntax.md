# Syntax reference (Go)

The Go version accepts the same relaxed-JSON syntax as the TypeScript
version. The canonical, language-neutral grammar is the
[top-level syntax reference](../../doc/syntax.md).

This page is the reference for how the Go runtime represents parsed
values. For the full TypeScript ↔ Go comparison see
[differences.md](differences.md).

## Result types

tabnas maps parsed values to Go types:

| Value             | Go type        |
|-------------------|----------------|
| Object            | `map[string]any` |
| Array             | `[]any`        |
| String            | `string`       |
| Number (any form) | `float64`      |
| Boolean           | `bool`         |
| Null / empty input| `nil`          |

All numbers are returned as `float64`, matching `encoding/json`
conventions. A number must be followed by a terminator (whitespace, a
structural character, or end of input), so `123abc` is a single text
value, not a number followed by text — the same behavior as the
TypeScript runtime.

## Extended result types

With `Info` options enabled, richer types are returned in place of the
plain values above:

| Option        | Wrapper type | Replaces |
|---------------|--------------|----------|
| `Info.Text`   | `tabnas.Text{Quote string, Str string}` | `string` |
| `Info.List`   | `tabnas.ListRef{Val []any, Implicit bool, Child any, Meta map[string]any}` | `[]any` |
| `Info.Map`    | `tabnas.MapRef{Val map[string]any, Implicit bool, Meta map[string]any}` | `map[string]any` |

- `Text.Quote` is the quote character used in the source (`""` for
  unquoted text).
- `ListRef.Implicit` / `MapRef.Implicit` are `true` when the array /
  object was written without brackets / braces.
- `ListRef.Child` holds the bare-colon child value (`[:1]`) when
  `List.Child` is enabled.

See the [how-to recipe](guide.md#get-quote--implicit-metadata) for
usage.
