# API reference (Go)

Complete reference for the exported API. For a guided introduction see
the [tutorial](tutorial.md); for task recipes see the
[how-to guide](guide.md); for the option tree see the
[options reference](options.md).

```go
import (
	tabnas "github.com/tabnas/parser/go" // engine (ships no grammar)
	"github.com/tabnas/parser/go/jsonic" // relaxed-JSON grammar + conveniences
)
```

The engine package is grammar-free: it has no package-level `Parse`.
Top-level parsing convenience lives in `jsonic`. `(*Tabnas).Parse` and
`ParseMeta` are instance methods on a configured instance.

## Package `jsonic`

### `Parse(src string) (any, error)`

Parse a relaxed-JSON string with default settings. Builds a fresh
parser per call. Returns `*tabnas.TabnasError` on a syntax error.

```go
result, err := jsonic.Parse("a:1, b:2")
// result: map[string]any{"a": float64(1), "b": float64(2)}
```

### `Make(opts ...tabnas.Options) *tabnas.Tabnas`

Create a reusable instance with the relaxed-JSON grammar installed.
Options are applied as in `tabnas.Make`; `Rule.Include` / `Rule.Exclude`
group filters are applied after the grammar exists, so they operate on
its group tags (`"json"`, `"tabnas"`).

### `MakeJSON() *tabnas.Tabnas`

Create an instance restricted to strict JSON. Rejects unquoted
keys/values, comments, hex/octal/binary numbers, trailing commas,
leading-zero numbers, single/backtick strings, and empty input.

### `Plugin(j *tabnas.Tabnas, opts map[string]any) error`

The relaxed-JSON grammar as a standard plugin, usable with
`(*Tabnas).Use`. Always returns `nil`.

```go
j := tabnas.Make()
_ = j.Use(jsonic.Plugin)
```

## Parsing (instance methods)

### `(*Tabnas) Parse(src string) (any, error)`

Parse with this instance's configuration.

### `(*Tabnas) ParseMeta(src string, meta map[string]any) (any, error)`

Parse with metadata accessible in rule actions/conditions via
`ctx.Meta`. A `"fileName"` key surfaces in formatted errors as
`--> file:row:col`.

## Result types

For relaxed-JSON input the concrete types behind the returned `any`
are: `map[string]any` (objects), `[]any` (arrays), `float64`
(numbers), `string` (strings), `bool` (booleans), `nil` (null / empty
input). With `Info` options enabled, values are wrapped in `Text`,
`ListRef`, and `MapRef`. See the [syntax reference](syntax.md).

## Instance management

### `Make(opts ...Options) *Tabnas`

Create a bare engine instance (no grammar). Unset option fields use
defaults.

### `Empty(opts ...Options) *Tabnas`

Like `Make`, but also clears any grammar rules contributed by plugins
in `opts`. Mirrors TS `tabnas.empty()`.

### `(*Tabnas) Derive(opts ...Options) (*Tabnas, error)`

Create a child instance inheriting the parent's config, rules,
plugins, custom tokens, decorations, and subscriptions. The parent's
plugins are re-applied against the child's merged options; if a plugin
fails during derivation, the error is returned (never a panic).
Changes to the child do not affect the parent.

```go
child, err := j.Derive(tabnas.Options{
	Comment: &tabnas.CommentOptions{Lex: boolp(false)},
})
```

### `(*Tabnas) SetOptions(opts Options) *Tabnas`

Deep-merge `opts` into the instance and rebuild the configuration.
Nil/zero fields do not overwrite existing values. Existing grammar
rules (including plugin modifications) are preserved. Returns the
instance for chaining.

### `(*Tabnas) SetOptionsText(text string) (*Tabnas, error)`

As `SetOptions`, but parses a tabnas-format options string. Requires a
registered text parser (importing `jsonic` registers one).

### `(*Tabnas) Options() Options`

Return a copy of the instance's current options.

### `(*Tabnas) Id() string`

Return the unique instance identifier.

### `(*Tabnas) Decorate(name string, value any) *Tabnas`

Attach a named value to the instance (the Go analogue of plugins
adding properties dynamically). Inherited by `Derive`.

### `(*Tabnas) Decoration(name string) any`

Return a value set by `Decorate`, or nil.

## Grammar

### `(*Tabnas) Rule(name string, definer RuleDefiner) *Tabnas`

Modify or create a grammar rule. The definer receives the `*RuleSpec`
and owning `*Parser` and can modify the rule's `Open`/`Close` alternate
lists and state actions (`BO`, `BC`, `AO`, `AC`). A new empty rule is
created if the name is unknown.

```go
type RuleDefiner func(rs *RuleSpec, p *Parser)
```

### `(*Tabnas) Grammar(gs *GrammarSpec, setting ...*GrammarSetting) error`

Apply a declarative grammar spec. Resolves `"@name"` function refs via
`gs.Ref`. Returns an error for missing/mistyped refs; malformed specs
produce an error, never a panic. See the [plugin guide](plugins.md).

### `(*Tabnas) GrammarText(text string, setting ...*GrammarSetting) error`

As `Grammar`, but parses a tabnas-format grammar string. Requires a
registered text parser.

### `(*Tabnas) RSM() map[string]*RuleSpec`

Return the rule spec map for direct inspection or modification.

### `(*Tabnas) Token(name string, src ...string) Tin`

Register a token type or look up an existing one. With `src`, registers
a fixed-token mapping.

```go
TL := j.Token("#TL", "~") // register ~ as #TL
OB := j.Token("#OB", "")  // look up existing #OB
```

### `(*Tabnas) FixedSrc(src string) Tin` / `(*Tabnas) FixedTin(tin Tin) string`

Map between a fixed-token source string and its `Tin` (`"{"` ↔ `TinOB`).
Return the zero value / `""` when not a fixed token.

### `(*Tabnas) TokenSet(name string) []Tin` / `(*Tabnas) SetTokenSet(name string, tins []Tin)`

Read or define a named token set. Built-in sets: `"IGNORE"` (space,
line, comment), `"VAL"` and `"KEY"` (text, number, string, value).

### `(*Tabnas) TinName(tin Tin) string`

Return the name for a `Tin`, covering built-in and custom tokens.

### Tokens

`Tin` is a token id (`type Tin = int`). Built-in `Tin` constants:
`TinBD` (#BD bad), `TinZZ` (#ZZ end), `TinUK` (#UK unknown), `TinAA`
(#AA any), `TinSP` (#SP space), `TinLN` (#LN line), `TinCM` (#CM
comment), `TinNR` (#NR number), `TinST` (#ST string), `TinTX` (#TX
text), `TinVL` (#VL value), `TinOB` `{`, `TinCB` `}`, `TinOS` `[`,
`TinCS` `]`, `TinCL` `:`, `TinCA` `,`.

## Plugins

### `Plugin` type

```go
type Plugin func(j *Tabnas, opts map[string]any) error
```

A plugin returns an `error`; a non-nil return aborts `Use`/`UseDefaults`.

### `(*Tabnas) Use(plugin Plugin, opts ...map[string]any) error`

Register and invoke a plugin. Returns the plugin's error.

```go
err := j.Use(myPlugin, map[string]any{"key": "value"})
```

### `(*Tabnas) UseDefaults(plugin Plugin, defaults map[string]any, opts ...map[string]any) error`

Register and invoke a plugin, deep-merging `defaults` under any
user-provided `opts` before calling it (the Go analogue of a TS
plugin's `.defaults` property). Returns the plugin's error.

### `(*Tabnas) Plugins() []Plugin`

Return the installed plugins.

### `(*Tabnas) PluginOptions(name string) map[string]any` / `(*Tabnas) SetPluginOptions(name string, opts map[string]any)`

Read or store options under a plugin namespace (the Go analogue of TS
`tabnas.options.plugin[name]`).

## Custom matchers

Register matchers under `Options.Lex.Match`, keyed by name. Matchers
run in priority order (lower first). Built-in priorities:

| Matcher | Priority  |
|---------|-----------|
| fixed   | 2,000,000 |
| space   | 3,000,000 |
| line    | 4,000,000 |
| string  | 5,000,000 |
| comment | 6,000,000 |
| number  | 7,000,000 |
| text    | 8,000,000 |

Use `Order` below 2,000,000 to run before all built-ins. Setting a
spec under an existing name replaces it.

```go
type MatchSpec struct {
	Order int            // lower runs first
	Make  MakeLexMatcher // factory invoked when options are applied
}

type LexMatcher     func(lex *Lex, rule *Rule) *Token
type MakeLexMatcher func(cfg *LexConfig, opts *Options) LexMatcher
```

A matcher reads the position via `lex.Cursor()` and must advance the
cursor if it produces a token. The `rule` argument enables
context-sensitive lexing. See the [recipe](guide.md#add-a-custom-matcher).

### `Lex` helper methods

| Method | Returns | Purpose |
|--------|---------|---------|
| `(*Lex) Cursor() *Point` | current `*Point` | read/advance position (`SI`, `RI`, `CI`) |
| `(*Lex) Fwd(maxlen int) string` | substring | look ahead up to `maxlen` bytes from the cursor |
| `(*Lex) Token(name string, tin Tin, val any, src string) *Token` | new token | build a token at the current point |
| `(*Lex) Bad(why string) *Token` | error token | signal a lex error (`why` is an error code) |
| `(*Lex) Next(rule ...*Rule) *Token` | next token | next non-IGNORE token |

### Scan primitives

The simpler matchers run on a table-driven byte-walk driver, exported
so plugin authors can build their own:

```go
func Scan(src string, startSI, startRI, startCI int, spec *ScanSpec, out *ScanOut) bool
func BuildCharRunSpec(chars map[rune]bool) *ScanSpec
func BuildLineRunSpec(lineChars, rowChars map[rune]bool) *ScanSpec
func BuildStringBodySpec(cfg *LexConfig, q rune) *ScanSpec
```

`ScanSpec` declares the byte-class table and a `Fallback` classifier
for non-ASCII runes; `ScanOut` receives the reached `SI`/`RI`/`CI`.
The packed action flags (`ScanConsume`, `ScanIsRow`, `ScanCIReset`,
`ScanStop`, `ScanStateMask`) are exported for hand-built specs. See
[concepts](concepts.md#the-scan-spec-lexer).

## Events

### `(*Tabnas) Sub(lexSub LexSub, ruleSub RuleSub) *Tabnas`

Subscribe to lex and/or rule events; pass `nil` to skip either.

```go
type LexSub  func(tkn *Token, rule *Rule, ctx *Context)
type RuleSub func(rule *Rule, ctx *Context)
```

## Configuration

### `(*Tabnas) Config() *LexConfig`

Return the resolved lexer config for direct inspection. Prefer
`Token`, `Rule`, and `Options.Lex.Match` for most work.

## Error handling

Parse errors are `*TabnasError`:

```go
type TabnasError struct {
	Code   string // error code (see below)
	Detail string // human-readable message ({key}-injected)
	Pos    int    // 0-based character position
	Row    int    // 1-based line number
	Col    int    // 1-based column (rune offset)
	Src    string // source fragment at the error
	Hint   string // explanatory text (per-code default; override via Options.Hint)
}
```

`Error()` renders the formatted message: a `[tag/code]:` header, a
`--> file:row:col` line, a source-context extract with a caret, the
hint, and an internal-diagnostics suffix. ANSI color is on by default;
disable via `Options.Color`.

```go
_, err := jsonic.Parse(`"abc`)
if te, ok := err.(*tabnas.TabnasError); ok {
	fmt.Println(te.Code, "at", te.Row, te.Col) // unterminated_string at 1 1
}
```

Common error codes: `unexpected`, `unterminated_string`,
`unterminated_comment`, `invalid_unicode`, `invalid_ascii`,
`unprintable`, `unknown_rule`, `end_of_source`, `internal`. The
`internal` code marks a bug in tabnas or a plugin (a panic caught by
the recover guard), not bad input — see
[concepts](concepts.md#the-no-panic-guarantee).

## Helper pattern

Option fields are pointers, so `nil` means "use default". A common
helper takes the address of a literal:

```go
func boolp(b bool) *bool { return &b }

tabnas.Options{Comment: &tabnas.CommentOptions{Lex: boolp(false)}}
```

## Constants

### `Version`

```go
const Version = "0.1.22"
```
