# API Reference (Go)

```go
import "github.com/tabnas/parser/go"
```

## Parsing

### `Parse(src string) (any, error)`

Parse a string using default settings. Convenience function that creates a
fresh parser for each call.

```go
result, err := tabnas.Parse("a:1, b:2")
// result: map[string]any{"a": float64(1), "b": float64(2)}
```

### `(*Tabnas) Parse(src string) (any, error)`

Parse using an instance's configuration.

```go
j := tabnas.Make()
result, err := j.Parse("a:1")
```

### `(*Tabnas) ParseMeta(src string, meta map[string]any) (any, error)`

Parse with metadata accessible in rule actions via `ctx.Meta`.

```go
result, err := j.ParseMeta("a:1", map[string]any{"filename": "config.tabnas"})
```

## Instance Management

### `Make(opts ...Options) *Tabnas`

Create a new parser instance. Unset option fields use defaults.

```go
j := tabnas.Make(tabnas.Options{
    Comment: &tabnas.CommentOptions{Lex: boolp(false)},
    Number:  &tabnas.NumberOptions{Hex: boolp(false)},
})
```

### `(*Tabnas) Derive(opts ...Options) *Tabnas`

Create a child instance inheriting the parent's configuration, plugins, custom
tokens, and subscriptions. Changes to the child do not affect the parent.

```go
child := j.Derive(tabnas.Options{
    Comment: &tabnas.CommentOptions{Lex: boolp(false)},
})
```

### `(*Tabnas) SetOptions(opts Options) *Tabnas`

Deep-merge new options into the instance and rebuild the configuration,
grammar, and plugins. Nil/zero fields in opts do not overwrite existing values,
matching the TypeScript `options()` setter behavior. Returns the instance for
chaining.

### `(*Tabnas) Options() Options`

Returns a copy of the instance's current options.

### `(*Tabnas) Decorate(name string, value any) *Tabnas`

Set a named value on the instance. This is the Go equivalent of the
TypeScript pattern where plugins add properties dynamically
(`tabnas.foo = () => 'FOO'`). Decorations are inherited by `Derive`.

```go
j.Use(func(j *tabnas.Tabnas, opts map[string]any) {
    j.Decorate("greet", func(name string) string {
        return "hello " + name
    })
})
```

### `(*Tabnas) Decoration(name string) any`

Returns a named value previously set by `Decorate`, or nil.

```go
fn := j.Decoration("greet").(func(string) string)
fmt.Println(fn("world")) // "hello world"
```

## Grammar

### `(*Tabnas) Rule(name string, definer RuleDefiner) *Tabnas`

Modify or create a grammar rule. The definer callback receives the
`*RuleSpec` and the owning `*Parser`, and can modify the rule's
`Open`/`Close` alternate lists and state actions (`BO`, `BC`, `AO`, `AC`).
The parser is available for inspecting or referencing other rules.

```go
j.Rule("val", func(rs *tabnas.RuleSpec, p *tabnas.Parser) {
    rs.Open = append([]*tabnas.AltSpec{{
        S: [][]tabnas.Tin{{myToken}},
        A: func(r *tabnas.Rule, ctx *tabnas.Context) {
            r.Node = "custom"
        },
    }}, rs.Open...)
})
```

### `(*Tabnas) RSM() map[string]*RuleSpec`

Returns the rule spec map for direct inspection.

### `(*Tabnas) Token(name string, src ...string) Tin`

Register a new token type or look up an existing one. With `src`, registers
a fixed token mapping.

```go
TL := j.Token("#TL", "~")  // register ~ as #TL token
OB := j.Token("#OB", "")   // look up existing #OB token
```

### `(*Tabnas) TokenSet(name string) []Tin`

Get a named token set:
- `"IGNORE"` -- space, line, comment tokens
- `"VAL"` -- text, number, string, value tokens
- `"KEY"` -- text, number, string, value tokens

### `(*Tabnas) TinName(tin Tin) string`

Returns the name for a token identification number.

## Plugins

### `(*Tabnas) Use(plugin Plugin, opts ...map[string]any) *Tabnas`

Register and execute a plugin. Returns the instance for chaining.

```go
j.Use(myPlugin, map[string]any{"key": "value"})
```

### `Plugin` type

```go
type Plugin func(j *Tabnas, opts map[string]any)
```

### `(*Tabnas) Plugins() []Plugin`

Returns the list of installed plugins.

## Custom Matchers

Register custom lexer matchers via `options.lex.match`, keyed by name.
This mirrors the TypeScript `tabnas.options({ lex: { match: ... } })` API.
Matchers are tried in priority order (lower first). Built-in priorities:

| Matcher | Priority |
|---|---|
| fixed | 2,000,000 |
| space | 3,000,000 |
| line | 4,000,000 |
| string | 5,000,000 |
| comment | 6,000,000 |
| number | 7,000,000 |
| text | 8,000,000 |

Use an `Order` below 2,000,000 to run before all built-ins.

```go
j := tabnas.Make()
j.SetOptions(tabnas.Options{Lex: &tabnas.LexOptions{
    Match: map[string]*tabnas.MatchSpec{
        "date": {Order: 1_000_000, Make: func(_ *tabnas.LexConfig, _ *tabnas.Options) tabnas.LexMatcher {
            return func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
                // ... read from lex.Cursor(), advance on match, return a Token
                return nil
            }
        }},
    },
}})
```

Setting a spec under an existing name replaces it.

### `LexMatcher` and `MakeLexMatcher` types

```go
type LexMatcher     func(lex *Lex, rule *Rule) *Token
type MakeLexMatcher func(cfg *LexConfig, opts *Options) LexMatcher

type MatchSpec struct {
    Order int            // lower runs first
    Make  MakeLexMatcher // factory invoked when options are applied
}
```

The matcher reads the current position via `lex.Cursor()` and must advance
the cursor if it produces a token.

## Events

### `(*Tabnas) Sub(lexSub LexSub, ruleSub RuleSub) *Tabnas`

Subscribe to lex and/or rule events. Pass `nil` for either to skip.

```go
j.Sub(func(tkn *tabnas.Token, rule *tabnas.Rule, ctx *tabnas.Context) {
    fmt.Println("token:", tkn)
}, nil)
```

### Subscriber types

```go
type LexSub func(tkn *Token, rule *Rule, ctx *Context)
type RuleSub func(rule *Rule, ctx *Context)
```

## Configuration

### `(*Tabnas) Config() *LexConfig`

Returns the parser's internal configuration for direct inspection or
modification. Prefer `Token()`, `Rule()`, and `options.lex.match` for most work.

### `(*Tabnas) Exclude(groups ...string) *Tabnas`

Remove grammar alternates tagged with the given group names.

```go
j.Exclude("tabnas") // keep only JSON-tagged rules for strict parsing
```

## Error Handling

Parse errors are returned as `*TabnasError`:

```go
type TabnasError struct {
    Code   string // "unexpected", "unterminated_string", "unterminated_comment", ...
    Detail string // Human-readable message ({key} template-injected)
    Pos    int    // 0-based character position
    Row    int    // 1-based line number
    Col    int    // 1-based column number
    Src    string // Source fragment at error
    Hint   string // Explanatory text (per-code defaults; override via Options.Hint)
}
```

```go
result, err := tabnas.Parse("{a:")
if err != nil {
    if je, ok := err.(*tabnas.TabnasError); ok {
        fmt.Println(je.Code, "at line", je.Row)
    }
}
```

## Helper Pattern

Go requires a pointer to pass `*bool` option fields. A common pattern:

```go
func boolp(b bool) *bool { return &b }

tabnas.Options{
    Comment: &tabnas.CommentOptions{Lex: boolp(false)},
}
```

## Constants

### `Version`

```go
const Version = "0.1.6"
```

The current version of the tabnas Go module.
