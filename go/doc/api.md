# API Reference (Go)

```go
import "github.com/amagamajs/amagama/go"
```

## Parsing

### `Parse(src string) (any, error)`

Parse a string using default settings. Convenience function that creates a
fresh parser for each call.

```go
result, err := amagama.Parse("a:1, b:2")
// result: map[string]any{"a": float64(1), "b": float64(2)}
```

### `(*Amagama) Parse(src string) (any, error)`

Parse using an instance's configuration.

```go
j := amagama.Make()
result, err := j.Parse("a:1")
```

### `(*Amagama) ParseMeta(src string, meta map[string]any) (any, error)`

Parse with metadata accessible in rule actions via `ctx.Meta`.

```go
result, err := j.ParseMeta("a:1", map[string]any{"filename": "config.amagama"})
```

## Instance Management

### `Make(opts ...Options) *Amagama`

Create a new parser instance. Unset option fields use defaults.

```go
j := amagama.Make(amagama.Options{
    Comment: &amagama.CommentOptions{Lex: boolp(false)},
    Number:  &amagama.NumberOptions{Hex: boolp(false)},
})
```

### `(*Amagama) Derive(opts ...Options) *Amagama`

Create a child instance inheriting the parent's configuration, plugins, custom
tokens, and subscriptions. Changes to the child do not affect the parent.

```go
child := j.Derive(amagama.Options{
    Comment: &amagama.CommentOptions{Lex: boolp(false)},
})
```

### `(*Amagama) SetOptions(opts Options) *Amagama`

Deep-merge new options into the instance and rebuild the configuration,
grammar, and plugins. Nil/zero fields in opts do not overwrite existing values,
matching the TypeScript `options()` setter behavior. Returns the instance for
chaining.

### `(*Amagama) Options() Options`

Returns a copy of the instance's current options.

### `(*Amagama) Decorate(name string, value any) *Amagama`

Set a named value on the instance. This is the Go equivalent of the
TypeScript pattern where plugins add properties dynamically
(`amagama.foo = () => 'FOO'`). Decorations are inherited by `Derive`.

```go
j.Use(func(j *amagama.Amagama, opts map[string]any) {
    j.Decorate("greet", func(name string) string {
        return "hello " + name
    })
})
```

### `(*Amagama) Decoration(name string) any`

Returns a named value previously set by `Decorate`, or nil.

```go
fn := j.Decoration("greet").(func(string) string)
fmt.Println(fn("world")) // "hello world"
```

## Grammar

### `(*Amagama) Rule(name string, definer RuleDefiner) *Amagama`

Modify or create a grammar rule. The definer callback receives the
`*RuleSpec` and the owning `*Parser`, and can modify the rule's
`Open`/`Close` alternate lists and state actions (`BO`, `BC`, `AO`, `AC`).
The parser is available for inspecting or referencing other rules.

```go
j.Rule("val", func(rs *amagama.RuleSpec, p *amagama.Parser) {
    rs.Open = append([]*amagama.AltSpec{{
        S: [][]amagama.Tin{{myToken}},
        A: func(r *amagama.Rule, ctx *amagama.Context) {
            r.Node = "custom"
        },
    }}, rs.Open...)
})
```

### `(*Amagama) RSM() map[string]*RuleSpec`

Returns the rule spec map for direct inspection.

### `(*Amagama) Token(name string, src ...string) Tin`

Register a new token type or look up an existing one. With `src`, registers
a fixed token mapping.

```go
TL := j.Token("#TL", "~")  // register ~ as #TL token
OB := j.Token("#OB", "")   // look up existing #OB token
```

### `(*Amagama) TokenSet(name string) []Tin`

Get a named token set:
- `"IGNORE"` -- space, line, comment tokens
- `"VAL"` -- text, number, string, value tokens
- `"KEY"` -- text, number, string, value tokens

### `(*Amagama) TinName(tin Tin) string`

Returns the name for a token identification number.

## Plugins

### `(*Amagama) Use(plugin Plugin, opts ...map[string]any) *Amagama`

Register and execute a plugin. Returns the instance for chaining.

```go
j.Use(myPlugin, map[string]any{"key": "value"})
```

### `Plugin` type

```go
type Plugin func(j *Amagama, opts map[string]any)
```

### `(*Amagama) Plugins() []Plugin`

Returns the list of installed plugins.

## Custom Matchers

Register custom lexer matchers via `options.lex.match`, keyed by name.
This mirrors the TypeScript `amagama.options({ lex: { match: ... } })` API.
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
j := amagama.Make()
j.SetOptions(amagama.Options{Lex: &amagama.LexOptions{
    Match: map[string]*amagama.MatchSpec{
        "date": {Order: 1_000_000, Make: func(_ *amagama.LexConfig, _ *amagama.Options) amagama.LexMatcher {
            return func(lex *amagama.Lex, rule *amagama.Rule) *amagama.Token {
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

### `(*Amagama) Sub(lexSub LexSub, ruleSub RuleSub) *Amagama`

Subscribe to lex and/or rule events. Pass `nil` for either to skip.

```go
j.Sub(func(tkn *amagama.Token, rule *amagama.Rule, ctx *amagama.Context) {
    fmt.Println("token:", tkn)
}, nil)
```

### Subscriber types

```go
type LexSub func(tkn *Token, rule *Rule, ctx *Context)
type RuleSub func(rule *Rule, ctx *Context)
```

## Configuration

### `(*Amagama) Config() *LexConfig`

Returns the parser's internal configuration for direct inspection or
modification. Prefer `Token()`, `Rule()`, and `options.lex.match` for most work.

### `(*Amagama) Exclude(groups ...string) *Amagama`

Remove grammar alternates tagged with the given group names.

```go
j.Exclude("amagama") // keep only JSON-tagged rules for strict parsing
```

## Error Handling

Parse errors are returned as `*AmagamaError`:

```go
type AmagamaError struct {
    Code   string // "unexpected", "unterminated_string", "unterminated_comment"
    Detail string // Human-readable message
    Pos    int    // 0-based character position
    Row    int    // 1-based line number
    Col    int    // 1-based column number
    Src    string // Source fragment at error
    Hint   string // Additional context (if configured)
}
```

```go
result, err := amagama.Parse("{a:")
if err != nil {
    if je, ok := err.(*amagama.AmagamaError); ok {
        fmt.Println(je.Code, "at line", je.Row)
    }
}
```

## Helper Pattern

Go requires a pointer to pass `*bool` option fields. A common pattern:

```go
func boolp(b bool) *bool { return &b }

amagama.Options{
    Comment: &amagama.CommentOptions{Lex: boolp(false)},
}
```

## Constants

### `Version`

```go
const Version = "0.1.6"
```

The current version of the amagama Go module.
