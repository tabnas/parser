# Writing plugins (Go)

A how-to for grammar authors. Plugins extend tabnas by modifying the
grammar, adding token types, registering custom matchers, or
subscribing to parse events. For the engine model behind these
mechanics see the shared [architecture](../../doc/architecture.md);
for exact signatures see the [API reference](api.md).

## Plugin structure

A plugin is a function returning an `error`:

```go
type Plugin func(j *Tabnas, opts map[string]any) error
```

Register it with `Use`, which invokes the plugin immediately and
returns its error:

```go
func myPlugin(j *tabnas.Tabnas, opts map[string]any) error {
	// modify the parser
	return nil
}

j := tabnas.Make()
if err := j.Use(myPlugin, map[string]any{"key": "value"}); err != nil {
	// plugin reported a failure
}
```

Return a non-nil error to abort installation. Plugins are re-applied
when `Derive()` creates a child instance; a plugin that fails during
derivation surfaces through `Derive`'s returned error.

### Default options

To ship default options that a caller can override, use `UseDefaults`.
It deep-merges your defaults under the caller's options before invoking
the plugin:

```go
defaults := map[string]any{"sep": ",", "trim": true}
err := j.UseDefaults(myPlugin, defaults, map[string]any{"trim": false})
// plugin sees {"sep": ",", "trim": false}
```

For options that belong to a plugin namespace rather than a single
call, store them with `SetPluginOptions(name, opts)` and read them back
with `PluginOptions(name)`.

## Adding tokens

Register a new fixed token:

```go
func tildePlugin(j *tabnas.Tabnas, opts map[string]any) error {
	j.Token("#TL", "~")
	return nil
}
```

Token names use the `#XX` convention. Built-in tokens:

| Name  | Src | Description        |
|-------|-----|--------------------|
| `#OB` | `{` | open brace         |
| `#CB` | `}` | close brace        |
| `#OS` | `[` | open square        |
| `#CS` | `]` | close square       |
| `#CL` | `:` | colon              |
| `#CA` | `,` | comma              |
| `#NR` | —   | number             |
| `#ST` | —   | string             |
| `#TX` | —   | text               |
| `#VL` | —   | value (keyword)    |
| `#SP` | —   | space              |
| `#LN` | —   | line ending        |
| `#CM` | —   | comment            |
| `#BD` | —   | bad (error)        |
| `#ZZ` | —   | end of input       |

## Modifying rules

Each rule has `Open` and `Close` alternate lists.

```go
func myPlugin(j *tabnas.Tabnas, opts map[string]any) error {
	TL := j.Token("#TL", "~")
	j.Rule("val", func(rs *tabnas.RuleSpec, p *tabnas.Parser) {
		rs.Open = append([]*tabnas.AltSpec{{
			S: [][]tabnas.Tin{{TL}},
			A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				r.Node = 42
			},
		}}, rs.Open...)
	})
	return nil
}
```

### AltSpec fields

| Field | Type | Description |
|-------|------|-------------|
| `S`  | `[][]Tin` | token pattern to match |
| `A`  | `AltAction` | action: `func(r *Rule, ctx *Context)` |
| `P`  | `string` | push a rule by name |
| `R`  | `string` | replace the current rule |
| `B`  | `int` | backtrack: tokens to put back |
| `C`  | `AltCond` | match condition |
| `G`  | `string` | group tags (e.g. `"json"`, `"tabnas,map"`) |
| `H`  | `AltModifier` | modifier: `func(alt *AltSpec, r *Rule, ctx *Context) *AltSpec` |
| `E`  | `AltError` | error function |
| `PF` | `func(r *Rule, ctx *Context) string` | dynamic push |
| `RF` | `func(r *Rule, ctx *Context) string` | dynamic replace |
| `BF` | `func(r *Rule, ctx *Context) int` | dynamic backtrack |

### State actions

Each `RuleSpec` has four phase-boundary hooks, each a `[]StateAction`
(`func(r *Rule, ctx *Context)`):

| Hook | When |
|------|------|
| `BO` | before open alternates are tried |
| `AO` | after an open alternate matches |
| `BC` | before close alternates are tried |
| `AC` | after a close alternate matches |

```go
j.Rule("map", func(rs *tabnas.RuleSpec, p *tabnas.Parser) {
	rs.AO = append(rs.AO, func(r *tabnas.Rule, ctx *tabnas.Context) {
		fmt.Println("opened a map")
	})
})
```

A `StateAction` returns nothing. To halt the parse with an error from
within an action, set `ctx.ParseErr` to an error token (the TS
equivalent of returning an error `Token`); the parse stops and that
error is returned.

## Custom matchers

For syntax beyond the built-in matchers, register one under
`Options.Lex.Match`:

```go
j.SetOptions(tabnas.Options{Lex: &tabnas.LexOptions{
	Match: map[string]*tabnas.MatchSpec{
		"date": {Order: 1_000_000, Make: func(_ *tabnas.LexConfig, _ *tabnas.Options) tabnas.LexMatcher {
			return func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
				// read from lex.Cursor(), advance if matched, return *Token or nil
				return nil
			}
		}},
	},
}})
```

`Order` controls priority (lower runs first; built-ins are
fixed=2M … text=8M). Setting a spec under an existing name replaces it.
For walking bytes inside a matcher, the scan-spec primitives (`Scan`,
`BuildCharRunSpec`, `BuildLineRunSpec`, `BuildStringBodySpec`) are
exported — see the [API reference](api.md#scan-primitives). Full
ordering, the `Lex` helper methods, and built-in priorities are listed
in the [API reference](api.md#custom-matchers).

## Subscribing to events

```go
j.Sub(
	func(tkn *tabnas.Token, rule *tabnas.Rule, ctx *tabnas.Context) {
		fmt.Println("lexed:", tkn)
	},
	func(rule *tabnas.Rule, ctx *tabnas.Context) {
		fmt.Println("rule:", rule.Name)
	},
)
```

Pass `nil` for either subscriber to skip it.

## Token sets

```go
ignore := j.TokenSet("IGNORE") // [#SP, #LN, #CM]
vals   := j.TokenSet("VAL")    // [#TX, #NR, #ST, #VL]
keys   := j.TokenSet("KEY")    // [#TX, #NR, #ST, #VL]
```

## Differences from TypeScript plugins

- Plugin signature is `func(j *Tabnas, opts map[string]any) error`;
  failures are returned as `error`, not thrown.
- Plugin defaults: `UseDefaults(plugin, defaults)` (TS uses a
  `.defaults` property on the function).
- Option namespacing: `PluginOptions` / `SetPluginOptions`.
- `StateAction` returns nothing; halt with an error by setting
  `ctx.ParseErr` (TS returns an error `Token`).
- Custom matchers register via `Options.Lex.Match` (same key/order
  shape as TS `lex.match`).

See [differences.md](differences.md) for the full list.
