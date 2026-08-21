// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

// builtins.go — the standard `$`-suffixed builtin function references,
// the Go port of ts/src/builtins.ts.
//
// A trailing `$` in a ref name marks an engine-provided builtin. The
// `$` ref-namespace is RESERVED (Grammar() rejects user refs containing
// `$`). BUILTIN_REFS is merged into the effective ref map at Grammar()
// load time, so a *serialized*, function-free GrammarSpec references
// these by name. BUILTIN_SCHEMA_VERSION versions the config contract;
// Grammar() refuses a spec whose GrammarSpec.V exceeds it.
//
// Two families: tree builders (@node$/@capture$/@bubble$) rebuild the
// `{rule, src, kids}` AST; probe dispatch (@probeInit$/@probeDecide$/
// @probePhase0$/1$/2$) resolves the optional-prefix `[X D] Y` ambiguity.
//
// CONFIG LIFETIME. A builtin's configuration is bound when the GRAMMAR
// LOADS (ruling #120's A1) and travels in the action's closure, not in
// any rule state. `bindBuiltinConfig` in grammarspec.go takes the key out
// of the alternate's `K` as it binds, so it never reaches `r.K` at all.
// There is exactly one regime: the alternate that declares a config is
// the alternate that gets it — the same rule TypeScript has always had,
// where the AltAction takes the matched alternate and reads `alt.k`.
//
// This comment used to claim the two ports were equivalent while Go read
// config from `r.K`. They were not. `r.K` propagates to children on push
// and replace, so a parent that DECLARED a config without running the
// builtin passed it to a child running one bare, and the same
// function-free serialized grammar answered 4 here and 3 in TypeScript.
// The escape clause it offered — "harmless for the bounded set the
// compiler emits" — was a claim about @tabnas/bnf's output, not about
// the contract this package offers every other grammar.
//
// The five value builders used to `delete` their own key immediately
// after reading it, to stop that leak. Those deletes are gone: they were
// containment for a design that no longer exists, and were themselves a
// THIRD scoping regime — consumed-once here against alternate-scoped in
// TypeScript — which is why the run-then-push shape used to agree for a
// different reason than the one that made it correct.
//
// Pinned by TestBuiltinConfigIsAlternateScoped and its TypeScript twin.
// See DIVERGENCE.md, "Repaired, and what replaced them".

import "reflect"

// BUILTIN_SCHEMA_VERSION is the config-schema version these builtins
// implement. A serialized grammar declaring GrammarSpec.V greater than
// this is refused at load. Absent (zero) ⇒ treated as version 1.
const BUILTIN_SCHEMA_VERSION = 3

// mkNode builds the AST node shape produced by the tree builtins:
// `{rule?, src, kids}`. `user` rules carry a `rule` tag; others omit it
// so they flatten into the enclosing user node. MUST stay byte-identical
// to @tabnas/abnf's mkAstNode (the cross-package AST-shape contract).
func mkNode(rule string, kind string) map[string]any {
	if kind == "user" {
		return map[string]any{"rule": rule, "src": "", "kids": []any{}}
	}
	return map[string]any{"src": "", "kids": []any{}}
}

// cfgInt reads a config number that may arrive as int (set at runtime) or
// float64 (parsed from a serialized JSON grammar).
func cfgInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

func cfgStr(v any) string { s, _ := v.(string); return s }
func cfgBool(v any) bool  { b, _ := v.(bool); return b }

// @node$ — allocate (when init) and/or accumulate matched terminals' src.
// Config in r.K["node$"] = {init?, rule?, kind?, nterms?}.
func builtinNodeCfg(r *Rule, _ *Context, cfg map[string]any) {
	if cfgBool(cfg["init"]) {
		r.Node = mkNode(cfgStr(cfg["rule"]), cfgStr(cfg["kind"]))
	}
	n, _ := r.Node.(map[string]any)
	if n == nil {
		return
	}
	nterms := cfgInt(cfg["nterms"])
	src, _ := n["src"].(string)
	for i := 0; i < nterms && i < len(r.O); i++ {
		src += r.O[i].Src
	}
	n["src"] = src
}

// @capture$ — merge the just-returned child node into the current node.
// Tagged children push into kids; untagged ones flatten (src + kids).
// Config in r.K["capture$"] = {rule?, kind?}.
func builtinCaptureCfg(r *Rule, _ *Context, cfg map[string]any) {
	if r.Node == nil {
		r.Node = mkNode(cfgStr(cfg["rule"]), cfgStr(cfg["kind"]))
	}
	n, _ := r.Node.(map[string]any)
	if n == nil || r.Child == nil {
		return
	}
	c := r.Child.Node
	if c == nil || c == Undefined {
		return
	}
	cm, ok := c.(map[string]any)
	if !ok {
		n["kids"] = append(asAnySlice(n["kids"]), c)
		return
	}
	if _, hasSrc := cm["src"]; !hasSrc {
		n["kids"] = append(asAnySlice(n["kids"]), c)
		return
	}
	// Self-reference guard (TS `c === n`): maps aren't ==-comparable.
	if reflect.ValueOf(cm).Pointer() == reflect.ValueOf(n).Pointer() {
		return
	}
	ns, _ := n["src"].(string)
	cs, _ := cm["src"].(string)
	n["src"] = ns + cs
	if cm["rule"] != nil && cm["rule"] != "" {
		n["kids"] = append(asAnySlice(n["kids"]), cm)
	} else if ck, ok := cm["kids"].([]any); ok {
		n["kids"] = append(asAnySlice(n["kids"]), ck...)
	}
}

// @bubble$ — lift the committed child's node straight up (no merge).
// Mirrors TS `r.child.node !== undefined` (a null child node still lifts).
func builtinBubble(r *Rule, _ *Context) {
	if r.Child != nil && r.Child.Node != Undefined {
		r.Node = r.Child.Node
	}
}

// @fold$ — fold this rule's node upward into its *parent's* node, then
// clear it. Emitted on the close alts of a tail-repeat rule
// (`X = seq [ sep X ]` compiled to a same-depth `r:` repeat): each
// iteration delivers its own node to the parent as a sibling kid, since
// the parent's Child pointer stays on the FIRST iteration and
// capture-on-close cannot see the run. Clearing Node makes the parent's
// later @capture$ a no-op on that stale pointer.
//
// cN close-phase tokens (the separator, e.g. `+`) append their src to
// the parent after the fold, so the parent's src spans the full run
// while each kid spans only its own segment.
// Config in r.K["fold$"] = {cN?}.
func builtinFoldCfg(r *Rule, _ *Context, cfg map[string]any) {
	if r.Parent == nil || r.Parent == NoRule {
		return
	}
	p, _ := r.Parent.Node.(map[string]any)
	if p == nil {
		return
	}
	if _, hasSrc := p["src"]; !hasSrc {
		return
	}
	if own, ok := r.Node.(map[string]any); ok && own != nil {
		_, ownHasSrc := own["src"]
		if ownHasSrc &&
			reflect.ValueOf(own).Pointer() != reflect.ValueOf(p).Pointer() {
			ps, _ := p["src"].(string)
			os, _ := own["src"].(string)
			p["src"] = ps + os
			if own["rule"] != nil && own["rule"] != "" {
				p["kids"] = append(asAnySlice(p["kids"]), own)
			} else if oks, okk := own["kids"].([]any); okk {
				p["kids"] = append(asAnySlice(p["kids"]), oks...)
			}
		}
	}
	cN := cfgInt(cfg["cN"])
	for i := 0; i < cN && i < len(r.C); i++ {
		if r.C[i] != nil {
			ps, _ := p["src"].(string)
			p["src"] = ps + r.C[i].Src
		}
	}
	r.Node = Undefined
}

func asAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{}
}

// @probeInit$ — phase-0 open: mark the position and reset phase.
func builtinProbeInit(r *Rule, ctx *Context) {
	rk := r.EnsureK()
	rk["pd_phase"] = 0
	rk["pd_mark"] = ctx.Mark()
}

// @probeDecide$ — phase-0 close: peek the un-consumed token, rewind, and
// commit to phase 1 (disambiguator present) or 2 (absent). The compiler
// emits a phase-0 close that consumes nothing, so ctx.T[0] is a real peek.
func builtinProbeDecide(r *Rule, ctx *Context) {
	mark, ok := r.K["pd_mark"]
	if !ok || mark == nil {
		// Defensive: phase-0 close ran without @probeInit$ (malformed
		// grammar). Bail rather than feed Rewind a bad mark and corrupt
		// the rewind window. Never fires for compiler-emitted grammars.
		return
	}
	var peek *Token
	if len(ctx.T) > 0 {
		peek = ctx.T[0]
	}
	_ = ctx.Rewind(cfgInt(mark))
	if peek != nil && peek.Name == cfgStr(r.K["pd_d"]) {
		r.EnsureK()["pd_phase"] = 1
	} else {
		r.EnsureK()["pd_phase"] = 2
	}
}

func builtinProbePhase0(r *Rule, _ *Context) bool { return cfgInt(r.K["pd_phase"]) == 0 }
func builtinProbePhase1(r *Rule, _ *Context) bool { return cfgInt(r.K["pd_phase"]) == 1 }
func builtinProbePhase2(r *Rule, _ *Context) bool { return cfgInt(r.K["pd_phase"]) == 2 }

// ---- Native-value builders ----------------------------------------
// Build NATIVE JSON values (objects/arrays/scalars), not the
// {rule,src,kids} syntax tree. Schema family v2.
//
// These are INFO-AWARE: with the info options off they emit plain
// map[string]any / []any / scalar (byte-identical to v1); with
// ctx.Cfg.MapRef / .ListRef / .TextInfo on they allocate the engine's
// MapRef / ListRef / Text wrappers (the Go info carriers — the
// counterpart of the TS marker property). The info logic lives here, in
// the engine, instead of each JSON-family plugin re-hand-writing it.
//
// Go reads config from r.K (alt.K is merged before the action), and r.K
// propagates to children — so the config-reading builders (@object$/
// @array$/@key$/@setval$/@value$) DELETE their own key right after
// reading it, before the push/replace K-copy, so a config set on one alt
// can never leak into a child rule and mis-fire. The open- and close-side
// builders use disjoint keys, so unconditional delete-after-read is safe.

// @object$ — allocate a fresh empty object. The default object node is an
// insertion-ordered OrderedMap (keys remember discovery order, matching
// the TS engine's plain-object semantics). A "sort" config on the alt
// (K:{object$:{sort:true}}) selects a Sorted node instead — the only way
// to get alphabetical keys. With MapRef info on, allocate a MapRef
// carrying the static `implicit` flag and an empty Meta bag.
func builtinObjectCfg(r *Rule, ctx *Context, cfg map[string]any) {
	if ctx != nil && ctx.Cfg != nil && ctx.Cfg.MapRef {
		r.Node = MapRef{Val: make(map[string]any), Implicit: cfgBool(cfg["implicit"]), Meta: make(map[string]any)}
		return
	}
	if cfgBool(cfg["sort"]) {
		r.Node = NewSortedMap()
		return
	}
	if ctx != nil && ctx.Cfg != nil && ctx.Cfg.PlainMap {
		r.Node = map[string]any{}
		return
	}
	r.Node = NewOrderedMap()
}

// @array$ — allocate a fresh empty array. With ListRef info on, allocate
// a ListRef carrying the static `implicit` flag and an empty Meta bag.
func builtinArrayCfg(r *Rule, ctx *Context, cfg map[string]any) {
	if ctx != nil && ctx.Cfg != nil && ctx.Cfg.ListRef {
		r.Node = ListRef{Val: make([]any, 0), Implicit: cfgBool(cfg["implicit"]), Meta: make(map[string]any)}
		return
	}
	r.Node = make([]any, 0)
}

// @reset$ — clear the parent-seeded node back to the no-value sentinel.
func builtinReset(r *Rule, _ *Context) {
	r.Node = Undefined
}

// @key$ — capture the matched key token's value into a (non-propagated)
// r.U slot for a later @setval$ on the same rule.
func builtinKeyCfg(r *Rule, _ *Context, cfg map[string]any) {
	slot := cfgStr(cfg["slot"])
	if slot == "" {
		slot = "key"
	}
	from := cfgInt(cfg["from"])
	if from >= 0 && from < len(r.O) {
		r.EnsureU()[slot] = r.O[from].Val
	}
}

// @setval$ — assign the just-returned child node under the captured key.
// Works on either a plain map[string]any or a MapRef wrapper (info mode)
// via NodeMapSet. Go's metadata lives in MapRef struct fields, so there
// is no marker-key collision to guard against (unlike the TS side).
func builtinSetvalCfg(r *Rule, _ *Context, cfg map[string]any) {
	slot := cfgStr(cfg["slot"])
	if slot == "" {
		slot = "key"
	}
	if r.Child == nil {
		return
	}
	key, _ := r.U[slot].(string)
	switch r.Node.(type) {
	case map[string]any, MapRef, *OrderedMap:
		r.Node = NodeMapSet(r.Node, key, r.Child.Node)
	}
}

// @push$ — append the child node to the array (skips the no-value child).
// Works on a plain []any or a ListRef wrapper (info mode) via
// NodeListAppend. Go slices are value types, so the grown header is
// re-published to the parent (mirrors the json plugin's parent write-back).
func builtinPush(r *Rule, _ *Context) {
	if r.Child == nil || IsUndefined(r.Child.Node) {
		return
	}
	switch r.Node.(type) {
	case []any, ListRef:
		r.Node = NodeListAppend(r.Node, r.Child.Node)
		if r.Parent != nil && r.Parent != NoRule {
			r.Parent.Node = r.Node
		}
	}
}

// @value$ — coalesce a value: a built child node wins; otherwise resolve
// the matched scalar token. With TextInfo on, a string/text token's value
// is wrapped in a Text carrying its source quote char (the leaf whose
// output type changes under info — the TS counterpart boxes a String).
func builtinValueCfg(r *Rule, ctx *Context, cfg map[string]any) {
	if r.Child != nil && !IsUndefined(r.Child.Node) {
		r.Node = r.Child.Node
		return
	}
	from := cfgInt(cfg["from"])
	if from < 0 || from >= len(r.O) {
		r.Node = Undefined
		return
	}
	tok := r.O[from]
	val := tok.ResolveVal(r, ctx)
	if ctx != nil && ctx.Cfg != nil && ctx.Cfg.TextInfo &&
		(tok.Tin == TinST || tok.Tin == TinTX) {
		quote := ""
		if tok.Tin == TinST && len(tok.Src) > 0 {
			quote = string(tok.Src[0])
		}
		str, _ := val.(string)
		val = Text{Quote: quote, Str: str}
	}
	r.Node = val
}

// ---- Config binding (A1, ruling #120) -----------------------------
//
// A builtin's configuration is bound when the GRAMMAR LOADS, not read
// from the rule's keep bag when the action runs. Each `make…` returns an
// AltAction closed over its alternate's config; BUILTIN_REFS holds the
// nil-config instance, which is what a bare `@node$` gets.
//
// This is the repair for the split registered in DIVERGENCE.md as
// "Builtin config reaches a child rule in Go and not in TypeScript".
// Config used to ride in `r.K`, which PROPAGATES to children on push and
// replace, so a parent that merely DECLARED `k: {value$: {from: 1}}`
// handed it to a child running `@value$` bare — 4 here against
// TypeScript's 3 for the same serialized grammar.
//
// The five value builders used to `delete` their key right after reading
// it, to stop exactly that leak. Those deletes are gone: they were a
// containment measure for a design that no longer exists, and they were
// themselves a THIRD scoping regime — config was consumed by running the
// builtin here and merely alternate-scoped in TypeScript, so the two
// ports agreed on the run-then-push shape for different reasons.
func makeBuiltinNode(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinNodeCfg(r, ctx, cfg) }
}
func makeBuiltinCapture(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinCaptureCfg(r, ctx, cfg) }
}
func makeBuiltinFold(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinFoldCfg(r, ctx, cfg) }
}
func makeBuiltinObject(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinObjectCfg(r, ctx, cfg) }
}
func makeBuiltinArray(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinArrayCfg(r, ctx, cfg) }
}
func makeBuiltinKey(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinKeyCfg(r, ctx, cfg) }
}
func makeBuiltinSetval(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinSetvalCfg(r, ctx, cfg) }
}
func makeBuiltinValue(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinValueCfg(r, ctx, cfg) }
}

// Default-config instances: what BUILTIN_REFS exposes.
var (
	builtinNode    = makeBuiltinNode(nil)
	builtinCapture = makeBuiltinCapture(nil)
	builtinFold    = makeBuiltinFold(nil)
	builtinObject  = makeBuiltinObject(nil)
	builtinArray   = makeBuiltinArray(nil)
	builtinKey     = makeBuiltinKey(nil)
	builtinSetval  = makeBuiltinSetval(nil)
	builtinValue   = makeBuiltinValue(nil)
)

// BUILTIN_CONFIG_FACTORY is the CLOSED set of builtins whose config is
// bound at grammar load. Keyed by the ref a spec writes — never by a `$`
// suffix test, which would also strip a grammar's own `k: {myTotal$: 1}`.
//
// The probe family is absent by construction, not by carve-out: those
// builtins read and write `r.K` (pd_phase, pd_mark), which is rule state
// that MUST propagate, and is not per-alternate configuration.
var BUILTIN_CONFIG_FACTORY = map[FuncRef]func(map[string]any) AltAction{
	"@node$":    makeBuiltinNode,
	"@capture$": makeBuiltinCapture,
	"@fold$":    makeBuiltinFold,
	"@object$":  makeBuiltinObject,
	"@array$":   makeBuiltinArray,
	"@key$":     makeBuiltinKey,
	"@setval$":  makeBuiltinSetval,
	"@value$":   makeBuiltinValue,
}

// BUILTIN_REFS is the standard builtin library. Tree/probe/value actions
// are registered as AltAction; the phase guards as AltCond — the resolver
// type-asserts the concrete type per field.
var BUILTIN_REFS = map[FuncRef]any{
	"@node$":        AltAction(builtinNode),
	"@capture$":     AltAction(builtinCapture),
	"@bubble$":      AltAction(builtinBubble),
	"@fold$":        AltAction(builtinFold),
	"@probeInit$":   AltAction(builtinProbeInit),
	"@probeDecide$": AltAction(builtinProbeDecide),
	"@probePhase0$": AltCond(builtinProbePhase0),
	"@probePhase1$": AltCond(builtinProbePhase1),
	"@probePhase2$": AltCond(builtinProbePhase2),

	// Native-value builders (schema v2).
	"@object$": AltAction(builtinObject),
	"@array$":  AltAction(builtinArray),
	"@reset$":  AltAction(builtinReset),
	"@key$":    AltAction(builtinKey),
	"@setval$": AltAction(builtinSetval),
	"@push$":   AltAction(builtinPush),
	"@value$":  AltAction(builtinValue),
}

// mergeBuiltinRefs returns BUILTIN_REFS overlaid with the spec's own refs
// (spec wins on collision, though `$` is reserved in Grammar()).
func mergeBuiltinRefs(specRef map[FuncRef]any) map[FuncRef]any {
	merged := make(map[FuncRef]any, len(BUILTIN_REFS)+len(specRef))
	for k, v := range BUILTIN_REFS {
		merged[k] = v
	}
	for k, v := range specRef {
		merged[k] = v
	}
	return merged
}
