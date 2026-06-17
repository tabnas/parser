# Native-value `$`-builtins

The engine ships two families of `$`-suffixed builtin function references
(`src/builtins.ts` / `go/builtins.go`), merged into a grammar's ref map at
`grammar()` load so a serialized, function-free `GrammarSpec` can reference
them by name:

- **Tree builders** (`@node$`, `@capture$`, `@bubble$`) — rebuild the
  `{rule, src, kids}` *syntax* tree (used by the ABNF compiler).
- **Native-value builders** (`@object$`, `@array$`, `@key$`, `@setval$`,
  `@push$`, `@value$`, `@reset$`) — build the *parsed value itself*
  (objects / arrays / scalars), the way the `@tabnas/json` grammar does.

A grammar whose rules thread a node from parent to child (the engine seeds
a pushed child's node from the parent) can assemble plain objects/arrays
with the native-value builders as **alt actions** (`a:`). Config rides in
`alt.k.<name>` (TS, 3rd action arg) / `r.K` (Go, merged before the action).

| Builtin | Effect |
|---|---|
| `@object$` | `r.node = {}` (Object.create(null) / `map[string]any{}`) |
| `@array$`  | `r.node = []` (`[]any{}`) |
| `@reset$`  | `r.node = undefined` / `Undefined` (clears the parent-seeded node) |
| `@key$`    | `r.u.key = r.o0.val` (capture the matched key token) |
| `@setval$` | `r.node[r.u.key] = r.child.node` (object property assign) |
| `@push$`   | `r.node.push(r.child.node)` (array element append) |
| `@value$`  | child-wins-else resolve the matched scalar token |

`BUILTIN_SCHEMA_VERSION` (currently **2**) versions the config contract; a
grammar may declare `GrammarSpec.v` and the engine refuses one that needs a
newer schema.

## v1 (shipped): plain nodes

The native-value builders emit **plain** containers/scalars. The
`@tabnas/json` grammar (TS) builds on them, keeping only its *json-specific*
behaviour (the `info` introspection markers and the pair assign's
marker-key-drop + prev-value) as thin closures composed via array-`a`.

A shared cross-engine fixture (`test/json-builder.fixture.json`) is a
function-free json-core grammar wired to these builtins; both the TS and Go
suites load it and assert the built value is byte-identical to
`JSON.parse` / `encoding/json`, pinning TS↔Go value parity.

---

## v2 (this design): info-aware builders

### Goal

Make the native-value builders **info-aware** so they handle the engine's
own introspection node model, gated by `cfg.info`. This dissolves *all* of
the json plugin's info closures on **both** engines (TS and Go fully adopt
the builtins) and moves the info logic into the engine — where its types
already live — instead of every JSON-family plugin re-hand-writing it.

**Principle.** `MapRef`/`ListRef`/`Text` and `cfg.info` are **engine**
value-model features (general introspection any grammar can enable), not
json-plugin concerns. A builtin operating on a `MapRef` is exactly as
legitimate as one operating on a `map[string]any`. v2 = the builders go
through the engine's info-aware value operations.

### The engine value-model (the contract)

**Go** (`go/text.go`, `go/lexer.go`):

```go
type MapRef  struct { Val map[string]any; Implicit bool; Meta map[string]any }
type ListRef struct { Val []any;          Implicit bool; Meta map[string]any }
type Text    struct { Quote string; Str string }
// LexConfig: MapRef bool, ListRef bool, TextInfo bool, InfoMarker string
```

**TS** (`cfg.info`): `{ map, list, text: bool, marker: string = '__info__' }`.
The marker is a non-enumerable property; string values become boxed
`String` carrying the marker.

**Representation split (by design):** Go encodes info by *swapping the node
type* (a wrapper struct, metadata in struct fields); TS encodes it by *a
hidden property on the plain node* (metadata in the key namespace). v2 has
each engine's builders use *its own* carrier — `builtins.ts` and
`builtins.go` are already separate, so this is per-engine internals behind
one config gate.

### What moves into the engine

Promote the ref-aware value operations out of `@tabnas/json` into the
parser package (they operate on engine types):

```go
// go (promoted from json/go/json.go)
func NodeMapSet(node any, key string, val any) any   // MapRef.Val[k]=v | m[k]=v ; returns node
func NodeListAppend(node any, val any) any           // ListRef.Val append | []any append ; returns node
```

TS gets a small `markNode(node, marker, data)` helper (the
`Object.defineProperty` the json plugin uses today — same arg order as the
plugin's `mark`).

### Per-builtin behaviour

All gated on `cfg.info.*`; **info-off behaviour is byte-identical to v1**
(so v1 grammars and the cross-engine fixture are unaffected).

| Builtin | info OFF (= v1) | info ON |
|---|---|---|
| `@object$` | plain object | Go `MapRef{Val:{}, Implicit:cfg.implicit, Meta:{}}` (when `cfg.MapRef`); TS plain object + marker `{implicit:cfg.implicit, meta:{}}` (when `cfg.info.map`) |
| `@array$`  | plain array  | Go `ListRef{...}` (when `cfg.ListRef`); TS array + marker (when `cfg.info.list`) |
| `@setval$` | `node[key]=child` | Go `NodeMapSet(...)`; **TS only:** skip when `cfg.info.map && key === cfg.info.marker` (marker-key-drop) |
| `@push$`   | `node.push(child)` (+ Go parent republish) | Go `NodeListAppend(...)` (handles `ListRef`) + republish |
| `@value$`  | child-wins-else scalar | when `cfg.info.text` & string & token∈{ST,TX}: Go `Text{Quote,Str}`; TS boxed `String` + `{quote}` marker |
| `@key$`    | `r.u.key = r.o0.val` | unchanged |
| `@reset$`  | `r.node = undefined` | unchanged |

**New config:** `@object$`/`@array$` gain optional `implicit` (default
`false`).

### The `Implicit` flag — static config, not a close hook

Today Go computes `Implicit` at *close* (`@map-bc`: `Implicit = !(O0.Tin ==
OB)`). v2 makes it **static config on `@object$`/`@array$`**
(`k.object$ = {implicit:false}`):

- **Strict JSON** is always explicit → `implicit:false` (config omitted).
  This **eliminates `@map-bc`/`@list-bc`** for json.
- **jsonic** implicit maps (`a:1,b:2`, no braces) are a *separate* open alt
  → that alt carries `implicit:true`. Only if a single alt must be
  dynamically explicit-or-implicit would a `@containerImplicit$` close
  builder be needed; json (and jsonic's separate-alt structure) is static.

### Schema version

Stays **2**. The info-awareness is an internal enhancement active only when
the runtime `cfg.info.*` options are set; info-off behaviour (every
serialized grammar + the cross-engine fixture) is unchanged. `info` is a
runtime option, not declared in a grammar's `v`, so no bump is required.

### The json plugin after v2

- **TS json:** drop `@jsonMapMark` / `@jsonListMark` / `@jsonText` /
  `@jsonSetval`; the grammar uses the builders directly.
- **Go json:** drop `@val-bc` / `@map-bo` / `@map-bc` / `@list-bo` /
  `@list-bc` / `@pair-bc` / `@elem-bc` and `jsonMapSet`/`jsonListAppend`
  (promoted to the engine); the grammar mirrors TS.
- Both become a grammar with **zero structure closures** — only the
  strict-lexer options remain plugin config.

### Tests

- **Value parity (info OFF):** the existing `json-builder.fixture.json`
  pins TS↔Go byte-identical — unchanged.
- **Info ON:** representations differ by design, so assert **per-engine**
  structure (TS: `__info__` property + key-drop + boxed-String quote; Go:
  `MapRef.Implicit` / `ListRef` / `Text.Quote`), plus the json suite as the
  behaviour oracle.

### Risks / edge cases

1. **`info.text` scalars** have no container identity, so both engines wrap
   the scalar (`Text` / boxed `String`). `@value$` does this inline — the
   one builder whose output *type* changes under info (acceptable: a leaf,
   no children operate on it downstream).
2. **Go value semantics** — `NodeMapSet`/`NodeListAppend` return the node
   (value-copied); `@setval$`/`@push$` reassign `r.Node` and republish to
   `r.Parent`.
3. **Marker-key-drop is TS-only** — Go's field-based metadata has no key
   collision.

### Scope & sequencing

1. **Engine** — promote `NodeMapSet`/`NodeListAppend` (+ TS `markNode`);
   make `@object$`/`@array$`/`@setval$`/`@push$`/`@value$` info-aware
   (config-gated); add the `implicit` config; engine info-on tests.
2. **json TS** — drop the 4 info closures.
3. **json Go** — drop the 7 closures + helpers; full builtin adoption.
4. **jsonic** — the superset (merge/extend, implicit promotion), now with
   info handled, on both engines → pure-data jsonic + the json5/jsonc/zon
   free-riders.
