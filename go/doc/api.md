# API reference (Go)

Complete reference for the exported API. For a guided introduction see
the [tutorial](tutorial.md); for task recipes see the
[how-to guide](guide.md); for the option tree see the
[options reference](options.md).

```go
import (
	tabnas "github.com/tabnas/parser/go" // engine (ships no grammar)
)
```

The engine package is grammar-free: it has no package-level `Parse`, and
there is no convenience top-level parser. You `Make()` an engine,
`Use()` a grammar plugin to teach it a language, then call the instance
methods `(*Tabnas).Parse` / `ParseMeta`:

```go
j := tabnas.Make()
if err := j.Use(myGrammar); err != nil {
	// plugin reported a failure
}
result, err := j.Parse("hello")
```

A grammar plugin is any `func(j *tabnas.Tabnas, opts map[string]any) error`
that registers tokens and rules (see the [plugin guide](plugins.md)).
For a complete, non-trivial grammar example, see the strict-JSON test
fixture at [`jsonplugin_test.go`](../jsonplugin_test.go).

## Parsing (instance methods)

### `(*Tabnas) Parse(src string) (any, error)`

Parse with this instance's configuration.

### `(*Tabnas) ParseMeta(src string, meta map[string]any) (any, error)`

Parse with metadata accessible in rule actions/conditions via
`ctx.Meta`. A `"fileName"` key surfaces in formatted errors as
`--> file:row:col`.

## Result types

A JSON-style grammar maps the returned `any` to these concrete types:
`*OrderedMap` (objects — an insertion-ordered map preserving source key
order; `Keys`/`Vals`, ordered `MarshalJSON`/`UnmarshalJSON`, `AsStringMap`),
`[]any` (arrays), `float64` (numbers), `string` (strings), `bool`
(booleans), `nil` (null / empty input) — the exact mapping is the
grammar's choice. Set `Map.Plain: true` to get a plain unordered
`map[string]any` instead (for consumers that track order themselves).
With `Info` options enabled, values are wrapped in `Text`, `ListRef`,
and `MapRef`. See the [syntax reference](syntax.md).

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

### `(*Tabnas) Merge(other *Tabnas) (*Tabnas, error)`

Combine this instance's grammar with another's, returning a **new**
instance; neither original is modified. Commutative: `a.Merge(b)` and
`b.Merge(a)` produce instances with the same options, the same rule
alternates in the same order, and the same parse behavior. Returns an
error (never panics) on conflicts.

Both instances must carry distinct, non-empty `Tag` options; the
result's tag is the sorted join (e.g. `"A~B"`).

Options merge symmetrically — a field set on only one side (nil/zero
means "default") merges cleanly; a field set to different values on
both sides errors with the option path (`merge: conflicting option
values at rule.maxmul`). Custom tokens and fixed-token sources are
unified by name (two names claiming one source, or one name claiming
two non-default sources, error). All rules appear in the result;
alternates of a rule defined on both sides interleave
deterministically: token-name order at the first differing position,
longer sequences before their own prefix, identical sequences by
complexity (condition first) then group tags. Identical unconditioned
alts (shared-base-plugin case) are emitted once. Lifecycle actions
concatenate in tag order; lex matchers order by `(Order, Name)`.

```go
a := tabnas.Make(tabnas.Options{Tag: "A"})
at := a.Token("#AT", "@")
a.Rule("val", func(rs *tabnas.RuleSpec, p *tabnas.Parser) {
	rs.AddOpen(&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinTX}, {at}}})
})

b := tabnas.Make(tabnas.Options{Tag: "B"})
pc := b.Token("#PC", "%")
b.Rule("val", func(rs *tabnas.RuleSpec, p *tabnas.Parser) {
	rs.AddOpen(&tabnas.AltSpec{S: [][]tabnas.Tin{{tabnas.TinTX}, {pc}}})
})

ab, err := a.Merge(b)   // val: [TX AT], [TX PC] — parses both forms
```

Merge is defined over the option trees and per-instance token/rule
state; matchers appended directly to `Config.CustomMatchers` (rather
than via `Options.Lex.Match`) do not transfer.

### `(*Tabnas) SetOptions(opts Options) *Tabnas`

Deep-merge `opts` into the instance and rebuild the configuration.
Nil/zero fields do not overwrite existing values. Existing grammar
rules (including plugin modifications) are preserved. Returns the
instance for chaining.

### `(*Tabnas) SetOptionsText(text string) (*Tabnas, error)`

As `SetOptions`, but parses a tabnas-format options string. Requires a
text parser registered via `tabnas.RegisterTextParser`; the engine
ships none, so this returns an error until you register one (typically
your own grammar package does so in its `init`).

### `RegisterTextParser(p func(src string) (any, error))`

Package-level. Register the parser used by the text-form APIs
(`SetOptionsText`, `GrammarText`) to parse their string argument. The
grammar-free engine registers none by default.

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
and owning `*Parser`. A new empty rule is created if the name is unknown.

```go
type RuleDefiner func(rs *RuleSpec, p *Parser)
```

The rule's alternate and lifecycle-action lists are **unexported** and
mutated through methods (matching the TS RuleSpec, where direct array
assignment is not possible):

| Method(s) | Purpose |
|---|---|
| `AddOpen(alts…)` / `AddClose(alts…)` | append alternates |
| `PrependOpen(alts…)` / `PrependClose(alts…)` | prepend alternates |
| `ModifyOpen(mods)` / `ModifyClose(mods)` | clear / delete / move / custom |
| `ClearOpen()` / `ClearClose()` | remove all alternates for a phase |
| `AddBO/AddAO/AddBC/AddAC(fn)` | append a lifecycle action |
| `PrependBO/PrependAO/PrependBC/PrependAC(fn)` | prepend a lifecycle action |
| `ClearActions(phases…)` | remove lifecycle actions (no args = all four) |
| `Fnref(ref)` | install lifecycle actions by `@<rule>-<phase>` funcref |
| `OpenAlts()` / `CloseAlts()` | read alternates |
| `Actions(phase)` | read a phase's actions (`"bo"`/`"ao"`/`"bc"`/`"ac"`) |
| `HasBO()/HasAO()/HasBC()/HasAC()` | lifecycle presence flags |

`AltModListOpts` (the `ModifyOpen`/`ModifyClose` argument) has fields
`Clear`, `Delete`, `Move`, and `Custom`.

### `(*Tabnas) Grammar(gs *GrammarSpec, setting ...*GrammarSetting) error`

Apply a declarative grammar spec. Resolves `"@name"` function refs via
`gs.Ref`. Returns an error for missing/mistyped refs; malformed specs
produce an error, never a panic. See the [plugin guide](plugins.md).

`gs.Meta` is free-form tool metadata and is ignored here: it is neither
read nor applied, so a spec carrying it installs exactly as one without
it. `GrammarSpecFromJSON` preserves a serialised spec's top-level `meta`
object into that field, so a caller holding the spec can read it back —
the BNF-family compilers record their synthetic-rule provenance under
`meta.provenance`, which the language server reads to canonicalise
generated rule names. Declared in `schema/grammar.schema.json`.

### `(*Tabnas) GrammarText(text string, setting ...*GrammarSetting) error`

As `Grammar`, but parses a tabnas-format grammar string. Requires a
text parser registered via `RegisterTextParser` (the engine ships
none).

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

`LexSub` fires for **every** token the lexer produces, including tokens in
the IGNORE set (whitespace, comments, line breaks) — the same stream the
TypeScript runtime delivers. Filter on the token name if you only want
grammar-significant tokens.

`RuleSub` fires **before** each rule pass, so the rule it receives has
not matched anything yet. For what a pass actually did, use
`SubRuleDone`.

### `(*Tabnas) SubRuleDone(sub RuleDoneSub) *Tabnas`

Subscribe to the post-process rule event, which fires **after** each
rule pass with the matched tokens recorded on the rule and the state
transition applied.

```go
type RuleDoneSub func(rule *Rule, ctx *Context, done RuleDone)

type RuleDone struct {
	State  RuleState    // rule state BEFORE the pass: OPEN or CLOSE
	Alt    *RuleDoneAlt // resolved alternate, nil if the state had none
	Forced bool         // synthesized close during recovery
}

type RuleDoneAlt struct {
	B   int      // backtracked token count
	G   []string // matched alternate's group tags
	P   string   // pushed child rule name ("" if none)
	R   string   // replacement rule name ("" if none)
	Err *Token   // failure token when alternates existed but none matched
}
```

Because `State` is the state *before* the pass, open and close events
for one rule can be paired by `rule.I` — which is how you recover
balanced source spans:

```go
opens := map[int]*Token{}
j.SubRuleDone(func(rule *Rule, ctx *tabnas.Context, done tabnas.RuleDone) {
	if tabnas.OPEN == done.State && 0 < rule.ON {
		opens[rule.I] = rule.O0
	}
	if tabnas.CLOSE == done.State && 0 < rule.CN {
		if open, ok := opens[rule.I]; ok {
			fmt.Printf("%s spans [%d,%d]\n", rule.Name, open.SI, rule.C0.SI)
		}
	}
})
```

It is a separate method rather than a third parameter on `Sub` because
`Sub`'s signature is positional and public: widening it would break
every caller for an event most do not use.

`Alt` is nil only when the rule state had no alternates at all; when it
had some and none matched, `Alt` is non-nil with just `Err` set. The
`G` slice is a fresh copy per event, so mutating it cannot reach the
grammar.

`Forced` is always false until Go gains error recovery — only recovery
synthesizes a close.

## Completion

### `(*Tabnas) Continuations(src string) ([]Tin, []string)`

The tokens that could legally continue `src`, treating it as a
**prefix** — the completion primitive of the unified-LSP design.
Returns the tins and their `#`-names, both sorted by tin.

```go
tins, names := j.Continuations(`{"a"`)
// names = ["#CL"] — after a key, only the colon can follow
```

The computation is **path-aware**: each alternate contributes only the
position it is actually waiting on, so a sibling alternate whose own
prefix never matched adds nothing. It is widened two ways — a **pop
closure** (while a rule can close on anything, its parent's close
continuations are legal here too) and a **push closure** (an alternate
fully matched at this position is about to push another rule, so that
rule's openers are legal, which is why `[1,` offers the next element's
value starters as well as `]`).

A prefix that parses completely still answers. Permissive grammars read
most mid-edit input as a complete document, and reporting nothing there
would leave completion empty exactly where an editor asks for it. `#ZZ`
is included whenever the prefix parses, meaning "stopping here is
legal" — it is a sentinel rather than something a user types, so a
completion provider should drop it from the item list and read it as
"this document is already valid".

The query runs its own parse, forced fail-fast whatever the instance's
`Parse.Recover` setting is: it must stop AT the query point, and a
recovering parse would skip past it and answer about somewhere else.
The instance is left untouched.

Over-approximation caveat: conditions and counters may still reject a
listed token.

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
j := tabnas.Make()
_ = j.Use(myGrammar)
_, err := j.Parse(`"abc`)
if te, ok := err.(*tabnas.TabnasError); ok {
	fmt.Println(te.Code, "at", te.Row, te.Col) // unterminated_string at 1 1
}
```

Common error codes: `unexpected`, `unterminated_string`,
`unterminated_comment`, `invalid_unicode`, `invalid_ascii`,
`unprintable`, `unknown_rule`, `end_of_source`, `cancel`, `internal`. The
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

## Grammar validation

Pure functions that check a grammar held as **data** — a `*GrammarSpec` from
the `Grammar()` / GrammarText path, a generator, or an editor — before any
parser exists. They report problems instead of returning errors, so one pass
can collect everything wrong with a grammar rather than stopping at the first.

### `ValidateAlt(alt *AltSpec) []string`

Problems with one alternate: declarative (`CD`) conditions and `G` group
tags. A condition given as a function is opaque and is skipped.

### `ValidateAlts(alts []*AltSpec, label string) []string`

`ValidateAlt` across a list, each problem prefixed with where it is — `label`
names the list, e.g. `"val.open alt[0]: …"`.

### `ValidateGrammar(gs *GrammarSpec, known []string) []string`

Every **dangling rule reference** in a whole spec: an alternate whose `P` or
`R` names a rule nothing defines. This is the one check that needs the whole
rule map in scope, so `ValidateAlt` cannot make it — and the reference is a
static typo the engine can otherwise only report at parse time, as
`unknown_rule`, and only once an input reaches the alternate carrying it.

- `known` names rules that already exist on the target instance, so a spec
  that **extends** a grammar can push to a rule it does not itself define.
  Pass `nil` to check a spec as a self-contained document.
- A `nil` rule entry *removes* that rule, so referencing it dangles — even if
  it was in `known`. `Clear: true` discards `known` entirely, since it wipes
  every rule on the instance.
- A FuncRef (`"@name"`) and an empty slot are skipped: each yields its rule
  name at parse time, so no static check can follow it.

Deliberately narrow — rule references only. Run `ValidateAlts` per list for
the per-alternate checks; note those two runtimes word their group-tag
message differently, which is why they are not folded in here. Problems are
sorted **by UTF-16 code unit** (JavaScript's order, reproduced deliberately —
see `utf16Less`) and the rule name is quoted verbatim rather than with `%q`,
so this function returns byte-identical output to TypeScript's
`validateGrammar`.

```go
gs := &tabnas.GrammarSpec{Rule: map[string]*tabnas.GrammarRuleSpec{
    "val": {Open: []*tabnas.GrammarAltSpec{{S: "#OB", P: "mapp"}}},
}}
tabnas.ValidateGrammar(gs, nil)
// [val.open alt[0]: unknown rule in p: "mapp"]
```

## Constants

### `Version`

```go
// Always equal to ts/package.json "version"; asserted by
// TestVersionMatchesPackageJSON in both runtimes.
const VERSION string
```
