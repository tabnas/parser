# Options Reference (Go)

Options are passed to `Make()` to configure a parser instance. All fields use
pointer types -- `nil` means "use default".

```go
j := tabnas.Make(tabnas.Options{
    Comment: &tabnas.CommentOptions{Lex: boolp(false)},
    Number:  &tabnas.NumberOptions{Hex: boolp(false)},
})
```

Option fields use pointer types so that `nil` means "use default". Define
small helpers to create pointer values:

```go
func boolp(b bool) *bool { return &b }
func intp(i int) *int    { return &i }
```

## `Fixed`

Controls fixed structural tokens (`{`, `}`, `[`, `]`, `:`, `,`).

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable fixed token recognition |

## `Space`

Controls whitespace handling.

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable space recognition |
| `Chars` | `string` | `" \t"` | Characters treated as space |

## `Line`

Controls line ending handling.

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable line recognition |
| `Chars` | `string` | `"\r\n"` | Line ending characters |
| `RowChars` | `string` | `"\n"` | Characters that increment the row counter |
| `Single` | `*bool` | `false` | Separate token per newline |

## `Text`

Controls unquoted text lexing.

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable text matching |
| `Modify` | `[]ValModifier` | `nil` | Pipeline of value transformers |

`ValModifier` signature: `func(val any) any`

## `Number`

Controls numeric literal parsing.

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable number matching |
| `Hex` | `*bool` | `true` | Support `0x` hexadecimal |
| `Oct` | `*bool` | `true` | Support `0o` octal |
| `Bin` | `*bool` | `true` | Support `0b` binary |
| `Sep` | `string` | `"_"` | Separator character (empty to disable) |
| `Exclude` | `func(string) bool` | `nil` | Return true to reject a number-like string |

## `Comment`

Controls comment handling.

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable all comment lexing |
| `Def` | `map[string]*CommentDef` | (see below) | Comment type definitions |

Default definitions:

```go
map[string]*CommentDef{
    "hash":  {Line: true, Start: "#"},
    "slash": {Line: true, Start: "//"},
    "block": {Line: false, Start: "/*", End: "*/"},
}
```

### `CommentDef`

| Field | Type | Description |
|---|---|---|
| `Line` | `bool` | `true` for line comments, `false` for block |
| `Start` | `string` | Start marker |
| `End` | `string` | End marker (block only) |
| `Lex` | `*bool` | Enable this definition (default: true) |
| `EatLine` | `*bool` | Consume trailing newline (default: false) |

## `String`

Controls quoted string parsing.

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable string matching |
| `Chars` | `string` | `"'\"\`` | Quote characters |
| `MultiChars` | `string` | `` "`" `` | Multiline quote characters |
| `EscapeChar` | `string` | `"\\"` | Escape character |
| `Escape` | `map[string]string` | (standard) | Escape sequence mappings. Map a key to `""` to remove a built-in escape (e.g. `{"v": ""}` rejects `\v`) |
| `AllowUnknown` | `*bool` | `true` | Allow unknown escape sequences |
| `EscapeStrict` | `*bool` | `false` | Restrict escapes to the standard set: disable the non-standard `\xHH` and `\u{…}` structural escapes (`\uXXXX` stays). With escape-map removals + `AllowUnknown: false`, yields JSON-conformant escapes |
| `AllowControl` | `*bool` | `false` | Admit raw control characters (code point below `0x20`) inside a string body instead of failing with `unprintable`. Line-end characters are excluded — they stay governed by `MultiChars`, so a raw newline in a single-line string is still an error. For grammars whose source-character rule permits them (e.g. JSON5's `JSON5SourceCharacter`, which admits a literal tab) |
| `Abandon` | `*bool` | `false` | On error, return nil to let next matcher try |
| `Replace` | `map[rune]string` | `nil` | Character replacements during scanning |

## `Map`

Controls object/map behavior.

| Field | Type | Default | Description |
|---|---|---|---|
| `Extend` | `*bool` | `true` | Deep-merge duplicate keys |
| `Merge` | `MapMergeFunc` | `nil` | Custom merge: `func(prev, val any, r *Rule, ctx *Context) any` |
| `Child` | `*bool` | `false` | Parse bare colon as `child$` key |

## `List`

Controls array/list behavior.

| Field | Type | Default | Description |
|---|---|---|---|
| `Property` | `*bool` | `true` | Allow key-value pairs in arrays |
| `Pair` | `*bool` | `false` | Push pairs as object elements |
| `Child` | `*bool` | `false` | Parse bare colon as child value |

## `Value`

Controls keyword recognition.

| Field | Type | Default | Description |
|---|---|---|---|
| `Lex` | `*bool` | `true` | Enable value matching |
| `Def` | `map[string]*ValueDef` | (see below) | Keyword definitions |

Default definitions:

```go
map[string]*ValueDef{
    "true":  {Val: true},
    "false": {Val: false},
    "null":  {Val: nil},
}
```

## `Rule`

Controls parser rule behavior.

| Field | Type | Default | Description |
|---|---|---|---|
| `Start` | `string` | `"val"` | Starting rule name |
| `Finish` | `*bool` | `true` | Auto-close at EOF |
| `MaxMul` | `*int` | `3` | Rule occurrence multiplier |
| `Include` | `string` | `""` | Comma-separated group tags to keep (applied first; drops untagged alts when set) |
| `Exclude` | `string` | `""` | Comma-separated group tags to remove (applied after `Include`) |

## `Rewind`

Bounds the consumed-token history retained for `ctx.Rewind`.

| Field | Type | Default | Description |
|---|---|---|---|
| `History` | `*int` | `64` | Consumed tokens retained for `ctx.Rewind`; a non-positive value retains all (TS `Infinity`). `ctx.Rewind` returns an error if its target mark has been evicted |

## `Lex`

Controls global lexer behavior.

| Field | Type | Default | Description |
|---|---|---|---|
| `Empty` | `*bool` | `true` | Allow empty source |
| `EmptyResult` | `any` | `nil` | Value for empty source |
| `Match` | `map[string]*MatchSpec` | `nil` | Custom lexer matchers, keyed by name (see [API reference](api.md#custom-matchers)) |

## `Parser`

Custom parser override.

| Field | Type | Description |
|---|---|---|
| `Start` | `func(src string, j *Tabnas, meta map[string]any) (any, error)` | Replace the entire parse |

## `Safe`

Controls security features.

| Field | Type | Default | Description |
|---|---|---|---|
| `Key` | `*bool` | `true` | Block prototype-pollution keys (e.g. `__proto__`) |

## Go-Only Options (`Info`)

These options are specific to the Go version, under `Options.Info`
(`InfoOptions`). They give Go clients typed access to parse metadata.

| Field | Type | Default | Description |
|---|---|---|---|
| `Info.Text` | `*bool` | `false` | Wrap string/text values in `Text{Quote, Str}` structs |
| `Info.List` | `*bool` | `false` | Wrap arrays in `ListRef{Val, Implicit, ...}` structs (auto-enabled when `List.Child` is true) |
| `Info.Map` | `*bool` | `false` | Wrap objects in `MapRef{Val, Implicit}` structs |
| `Info.Marker` | `string` | `"__info__"` | Key under which info metadata is stored |

## `ErrMsg`

Controls error message formatting (TS: `options.errmsg`).

| Field | Type | Default | Description |
|---|---|---|---|
| `Name` | `string` | `"tabnas"` | Header tag: `[name/code]: ...` |
| `Suffix` | `any` | `true` | `bool` (standard internal block on/off), `string` (literal), or `func(code, src string) string` |
| `Link` | `string` | `""` | Optional "see also" line (e.g. docs URL) in the standard suffix |

## `Match`

Custom token and value matchers (TS: `options.match`, where each entry
is `RegExp | LexMatcher`).

| Field | Type | Description |
|---|---|---|
| `Lex` | `*bool` | Enable custom matching. Default: `true` |
| `Token` | `map[string]*regexp.Regexp` | `"#NAME"` → regexp token matcher |
| `TokenFn` | `map[string]LexMatcher` | `"#NAME"` → function token matcher |
| `Value` | `map[string]*MatchValueSpec` | name → `{Match, Val}` or `{Fn}` value matcher |
| `Check` | `LexCheck` | Hook before the match matcher runs |

## `Color`

Controls ANSI color codes in formatted error messages (TS:
`options.color`). All codes default to standard escapes.

| Field | Type | Default | Description |
|---|---|---|---|
| `Active` | `*bool` | `true` | Toggle color output (set `false` to disable) |
| `Reset` | `string` | `ESC[0m` | Reset all attributes |
| `Hi` | `string` | `ESC[91m` | Highlight the error header |
| `Lo` | `string` | `ESC[2m` | Dim the trailing suffix |
| `Line` | `string` | `ESC[34m` | Color the source-location arrow and gutter |

## Other Fields

| Field | Type | Description |
|---|---|---|
| `Ender` | `[]string` | Additional characters that end text tokens |
| `TokenSet` | `map[string][]string` | Customize named token sets (e.g. `VAL`, `KEY`); values are token names |
| `Error` | `map[string]string` | Error message templates by code; `{key}` placeholders are injected (e.g. `{src}`, `{code}`, `{row}`, `{col}`). Merged over defaults |
| `Hint` | `map[string]string` | Error hint templates by code; same `{key}` injection. Merged over defaults |
| `Parse` | `*ParseOptions` | Parse-time hooks: `Prepare` is a name-keyed map of `func(ctx *Context)` run at the start of every parse; `Budget` is the opt-in cancellation hook and `Recover` the opt-in error recovery (both below) |
| `Result` | `*ResultOptions` | `Fail []any` lists result values treated as parse failures |
| `Property` | `*PropertyOptions` | Go-only: `ConfigModify map[string]ConfigModifier` post-config callbacks |
| `Tag` | `string` | Instance identifier tag, appended to the instance id and shown in the error suffix internal line. Unset defaults to `DefaultTag` (`"-"`), matching TS. `Merge` treats `"-"` as "no tag chosen" and rejects it |

### `Parse.Recover` — multi-error recovery

Off by default. With `Enabled`, a parse error no longer ends the parse:
it is recorded, the lexer skips forward to a sync token, the rule stack
pops to a rule that can accept it, and parsing continues. Read the
results with `ParseRecover`.

```go
j := tabnas.Make(tabnas.Options{
    Parse: &tabnas.ParseOptions{
        Recover: &tabnas.RecoverOptions{Enabled: true},
    },
})

value, errs, err := j.ParseRecover(`{"a":1,}`)
// value = map[a:1], len(errs) = 1, err = nil
```

`Parse` keeps its two-value signature and returns the partial value
with a nil error, so existing callers see a result rather than a
failure; `ParseRecover` is how you learn there were errors at all.

Sync points are derived from the grammar, not hard-coded: the leading
tokens of close alternates whose `AltSpec.G` tags intersect
`SyncGroups` (default `close`, `comma`, `end`). A grammar with no such
tags falls back to the leading token of every close alternate on the
rule stack, so recovery still works — less precisely. Tagging a
grammar's close alternates is what sharpens it.

| Field | Default | Meaning |
|---|---|---|
| `Enabled` | `false` | Turn recovery on |
| `SyncGroups` | `["close","comma","end"]` | Group tags marking sync edges; a supplied slice REPLACES the default |
| `SyncTokens` | none | Extra sync token names, e.g. `[]string{"#CA"}`; additive |
| `PopUntilValid` | `true` | Pop until a rule accepts the sync token, else pop exactly one |
| `MaxSkip` | `64` | Cap tokens skipped per recovery |
| `MaxRecoveries` | `32` | Cap recorded errors per parse |
| `Suppress` | `4` | Drop errors within this many consumed tokens of the last recovery |

Each recovered error carries `Recovered` (tokens skipped, and the sync
token resumed on) for a consumer that wants to show the abandoned
region. `Recovered.Bad` marks a diagnostic for an unlexable RUN
absorbed at fetch time rather than one raised by a resync: an
unlexable word is one diagnostic however many characters it has, and
two separate words are two. See `doc/differences.md` for the TS
comparison.

### `Parse.Budget` — cancellation

Off by default. Set both fields to enable it: the main rule loop calls
`OnCheck` every `CheckEveryN` iterations, and a `false` return cancels
the parse with the `cancel` error code. One of the two alone leaves the
hook off, so a config typo cannot half-enable it.

```go
deadline := time.Now().Add(200 * time.Millisecond)

j := tabnas.Make(tabnas.Options{
    Parse: &tabnas.ParseOptions{
        Budget: &tabnas.BudgetOptions{
            CheckEveryN: 64,
            OnCheck: func(ctx *tabnas.Context) bool {
                return time.Now().Before(deadline)
            },
        },
    },
})
```

`OnCheck` receives the live parse context, so a host can read `ctx.KI`
(the iteration count) and `ctx.Rule` to decide. The first call happens
after `CheckEveryN` iterations, never before any work is done.

Pick `CheckEveryN` for the cost of the check, not the resolution you
want: a `time.Now()` call every iteration is measurable on a hot parse,
while reading an atomic abort flag is not. Cancellation is an ordinary
parse error — it is recorded in `ctx.Errs` like any other.
