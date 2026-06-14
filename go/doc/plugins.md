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

### Registering several plugins

`Use` returns an `error`, not the instance, so Go does not chain
registrations the way the TypeScript API does
(`new Tabnas().use(a).use(b)`). The idiomatic equivalent is sequential
calls with error checks:

```go
j := tabnas.Make()
if err := j.Use(jsonGrammar); err != nil {
	return nil, err
}
if err := j.Use(csvGrammar, map[string]any{"delimiter": ";"}); err != nil {
	return nil, err
}
```

For a uniform list with no per-plugin options, a loop reads cleanly:

```go
j := tabnas.Make()
for _, p := range []tabnas.Plugin{jsonGrammar, csvGrammar, debugPlugin} {
	if err := j.Use(p); err != nil {
		return nil, err
	}
}
```

### Grammar dependencies and order

Plugins are applied in registration order, and a grammar plugin may
build on tokens, rules, or token sets that an earlier plugin
registered. Order is therefore significant: **register a grammar's
dependencies before the grammar itself.** For example, a CSV grammar
that reuses a relaxed-JSON value grammar to parse each cell depends on
that grammar being installed first:

```go
j := tabnas.Make()
_ = j.Use(jsonic) // dependency: provides the cell-value rules/tokens
_ = j.Use(csv)    // builds on what jsonic registered
```

A plugin can fail fast if a required dependency is missing — inspect the
instance in its body (e.g. look up an expected token, or check
`j.RSM()` for a required rule) and return a clear `error` rather than
producing a confusing parse failure later.

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

Each rule has open/close alternate lists and bo/ao/bc/ac lifecycle action
lists. These are **mutated through methods**, not by direct field access —
the lists are unexported on `RuleSpec`, matching the TypeScript runtime
(where direct array assignment is likewise impossible). The methods:

| Method | Effect |
|---|---|
| `AddOpen(alts…)` / `AddClose(alts…)` | Append alternates |
| `PrependOpen(alts…)` / `PrependClose(alts…)` | Prepend alternates |
| `ModifyOpen(mods)` / `ModifyClose(mods)` | Clear / delete / move / custom (see [Replacing rules and actions](#replacing-rules-and-actions)) |
| `ClearOpen()` / `ClearClose()` | Remove all alternates for that phase |
| `AddBO/AddAO/AddBC/AddAC(fn)` | Append a lifecycle action |
| `PrependBO/PrependAO/PrependBC/PrependAC(fn)` | Prepend a lifecycle action |
| `ClearActions(phases…)` | Remove lifecycle actions (no args = all) |
| `Fnref(ref)` | Install lifecycle actions by `@<rule>-<phase>` funcref |
| `OpenAlts()` / `CloseAlts()` / `Actions(phase)` | Read accessors |
| `HasBO()/HasAO()/HasBC()/HasAC()` | Lifecycle presence flags |

```go
func myPlugin(j *tabnas.Tabnas, opts map[string]any) error {
	TL := j.Token("#TL", "~")
	j.Rule("val", func(rs *tabnas.RuleSpec, p *tabnas.Parser) {
		rs.PrependOpen(&tabnas.AltSpec{
			S: [][]tabnas.Tin{{TL}},
			A: func(r *tabnas.Rule, ctx *tabnas.Context) { r.Node = 42 },
		})
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
	rs.AddAO(func(r *tabnas.Rule, ctx *tabnas.Context) {
		fmt.Println("opened a map")
	})
})
```

A `StateAction` returns nothing. To halt the parse with an error from
within an action, set `ctx.ParseErr` to an error token (the TS
equivalent of returning an error `Token`); the parse stops and that
error is returned.

### Replacing rules and actions

By default a later plugin's alternates and state actions are **appended**
to earlier ones. To instead replace what earlier plugins contributed:

- **Alternates** — `ModifyOpen` / `ModifyClose` with `Clear: true`, or
  `ClearOpen()` / `ClearClose()`, then re-add:

  ```go
  rs.ClearOpen().AddOpen(&tabnas.AltSpec{ /* ... */ })
  // or, equivalently: rs.ModifyOpen(&tabnas.AltModListOpts{Clear: true}); rs.AddOpen(...)
  ```

  Declaratively, set `Clear` on the alt-list inject:

  ```go
  "val": {Open: &tabnas.GrammarAltListSpec{
      Alts:   []*tabnas.GrammarAltSpec{ /* ... */ },
      Inject: &tabnas.GrammarInjectSpec{Clear: true},
  }}
  ```

- **State actions** — `rs.ClearActions("bo", "ao", …)` (no args clears all
  four phases) removes earlier actions; then register fresh ones.
  Declaratively, append `/replace` to the funcref name:

  ```go
  // drops every previously-registered `map` before-open action, installs this one
  j.Grammar(&tabnas.GrammarSpec{
      Ref:  map[tabnas.FuncRef]any{"@map-bo/replace": tabnas.StateAction(resetMap)},
      Rule: map[string]*tabnas.GrammarRuleSpec{"map": {}},
  })
  ```

`/replace` takes ownership of the phase: once replaced, the plain /
`/prepend` / `/append` funcrefs for it are ignored, and the replacement
wins deterministically across `Derive()`. All of this is opt-in —
existing grammars that use neither `Clear` nor `/replace` keep the append
behavior unchanged.

### Funcref slot and dedup semantics

The funcref suffixes match the TypeScript runtime:

- `@<rule>-<phase>/append` and the plain `@<rule>-<phase>` are the **same
  slot** — providing both installs one action (the `/append` entry wins),
  exactly as TS resolves `fr[base+'/append'] ?? fr[base]`. Use `/prepend`
  for a distinct front-inserted action.
- A funcref already installed for a phase is **not** re-installed on a
  later `Grammar()` / `Fnref()` call (dedup). TS dedups by function
  identity; Go dedups by function pointer (`reflect.ValueOf(fn).Pointer()`).
  Go has no per-closure identity, so two *distinct* closures created from
  the same function literal share a code pointer and dedup as one. Register
  **stable, reused** `StateAction` / ref values across calls (as grammars
  do) — the dependable pattern in both runtimes.

`RuleSpec.Fnref(ref)` installs lifecycle actions by funcref imperatively,
mirroring the TS `rs.fnref(frm)` method, for code-built grammars that don't
go through `Grammar()`.

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
