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
const BUILTIN_SCHEMA_VERSION = 5

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

// srcVal returns the accumulated source text of a tree node -- the
// {rule?, src, kids} shape mkNode builds -- or the node unchanged when it
// is not one.
//
// "src" is how a member whose value IS its matched text gets that text:
// the tree builders already accumulate it, and nothing else could read it
// back out. A compiler emits "src" only where it already knows the member
// is a scalar, so this never has to guess which it is: the fall-through
// exists so asking for src where no tree node was built passes the value
// along rather than erasing it.
func srcVal(node any) any {
	if m, ok := node.(map[string]any); ok {
		if s, ok := m["src"].(string); ok {
			return s
		}
	}
	return node
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

// cfgFlag reads a config key that defaults to something other than the
// zero value. `cfgBool` cannot express that: an absent key and an
// explicit `false` both read as false, which is right for a flag that
// switches something ON and wrong for one that switches something OFF.
func cfgFlag(cfg map[string]any, key string, def bool) bool {
	v, ok := cfg[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

// @node$ — allocate (when init) and/or accumulate matched terminals' src.
// Config in r.K["node$"] = {init?, rule?, kind?, nterms?}.
func builtinNodeCfg(r *Rule, _ *Context, cfg map[string]any) {
	if cfgBool(cfg["init"]) {
		r.Node = mkNode(cfgStr(cfg["rule"]), cfgStr(cfg["kind"]))
		r.nodeOwner = nil
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
		r.nodeOwner = nil
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
		// The lifted node keeps its OWNER. Claiming ownership here would
		// strand the rule that actually allocated the container when the
		// child is still carrying one handed down to it, and a later push
		// from deeper in the chain would leave that rule with a stale
		// header.
		r.nodeOwner = r.Child.nodeHolder()
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
	r.nodeOwner = nil
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
		r.nodeOwner = nil
		return
	}
	if cfgBool(cfg["sort"]) {
		r.Node = NewSortedMap()
		r.nodeOwner = nil
		return
	}
	if ctx != nil && ctx.Cfg != nil && ctx.Cfg.PlainMap {
		r.Node = map[string]any{}
		r.nodeOwner = nil
		return
	}
	r.Node = NewOrderedMap()
	r.nodeOwner = nil
}

// @array$ — allocate a fresh empty array. With ListRef info on, allocate
// a ListRef carrying the static `implicit` flag and an empty Meta bag.
func builtinArrayCfg(r *Rule, ctx *Context, cfg map[string]any) {
	if ctx != nil && ctx.Cfg != nil && ctx.Cfg.ListRef {
		r.Node = ListRef{Val: make([]any, 0), Implicit: cfgBool(cfg["implicit"]), Meta: make(map[string]any)}
		r.nodeOwner = nil
		return
	}
	r.Node = make([]any, 0)
	r.nodeOwner = nil
}

// @reset$ — clear the parent-seeded node back to the no-value sentinel.
func builtinReset(r *Rule, _ *Context) {
	r.Node = Undefined
	r.nodeOwner = nil
}

// @key$ — capture the matched key token's value into a (non-propagated)
// r.U slot for a later @setval$ on the same rule.
//
// "lit" supplies the key as a CONSTANT instead of reading it from a
// token. A grammar whose structure is declared rather than delimited —
// `ver = maj "." min`, where `maj` names a part but no token carries the
// text "maj" — has no token for @key$ to read, so without this the key
// side of @setval$ is unreachable for it. The type assertion (rather
// than a nil test) keeps this port agreeing with the TS one on every
// input: both take "lit" only when it is actually a string.
func builtinKeyCfg(r *Rule, _ *Context, cfg map[string]any) {
	slot := cfgStr(cfg["slot"])
	if slot == "" {
		slot = "key"
	}
	if lit, ok := cfg["lit"].(string); ok {
		r.EnsureU()[slot] = lit
		return
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
	val := r.Child.Node
	if cfgBool(cfg["src"]) {
		val = srcVal(val)
	}
	switch r.Node.(type) {
	case map[string]any, MapRef, *OrderedMap:
		r.Node = NodeMapSet(r.Node, key, val)
	}
}

// listHeader is the slice inside a node that holds a list, and whether
// the node was one. Info mode wraps the slice in a ListRef, so a bare
// `[]any` assertion would silently skip every list in that mode — which
// is Go-only surface, so nothing else would have caught it.
func listHeader(v any) ([]any, bool) {
	switch l := v.(type) {
	case []any:
		return l, true
	case ListRef:
		return l.Val, true
	}
	return nil, false
}

// sameGrownList reports whether `held` is the very list that `before`
// was: same length, and the same backing array.
//
// EMPTY lists are deliberately NOT matched. Go gives two distinct
// zero-length slices the same (or no) data pointer, so an empty list
// cannot be told apart from another empty one — and guessing the wrong
// way is worse than not propagating. A replacement that allocated its
// OWN empty list before its first push must not overwrite the list of
// the rule it replaced, because TypeScript would not: there the fresh
// allocation is a different object and the replaced rule keeps its own.
// Declining to propagate keeps that guarantee, at the cost of the
// mirror-image case — a rule that allocated a list, was replaced before
// anything went into it, and is then read by a parent — which is
// recorded in go/doc/differences.md rather than silently traded away.
func sameGrownList(held, before any) bool {
	hs, hok := listHeader(held)
	bs, bok := listHeader(before)
	if !hok || !bok || len(hs) != len(bs) || 0 == len(bs) {
		return false
	}
	return &hs[0] == &bs[0]
}

// @push$ — append the child node to the array (skips the no-value child).
// Works on a plain []any or a ListRef wrapper (info mode) via
// NodeListAppend.
//
// Go slices are value types, so the grown header has to be re-published
// to every rule that was holding the same list — otherwise those rules
// keep a shorter one. TypeScript needs none of this: it hands out the
// same array OBJECT, and `push` mutates it in place.
//
// Two directions, and they are not the same one:
//
//   - the PARENT, which pushed this rule and reads its list afterwards
//     (mirrors the json plugin's parent write-back);
//   - the rules this one REPLACED (`r:`), which is the direction that was
//     missing. A replacement is seeded with the replaced rule's node and
//     carries the chain on, but the PARENT'S Child pointer still refers
//     to the rule that was replaced — so a parent reading the result
//     through `@bubble$` or `@capture$` got the list as it stood before
//     the replacement, dropping every element the rest of the chain
//     appended. TypeScript hides this behind the shared object; here the
//     header has to be carried back.
//
// Only rules that actually held the pre-append list are updated, so a
// replacement that allocated a fresh container of its own cannot clobber
// the one it replaced.
func builtinPushCfg(r *Rule, _ *Context, cfg map[string]any) {
	if r.Child == nil || IsUndefined(r.Child.Node) {
		return
	}
	val := r.Child.Node
	if cfgBool(cfg["src"]) {
		val = srcVal(val)
	}
	// The rule holding the authoritative container. A list can be grown
	// many rules below the one that allocated it — a right-recursive
	// repetition helper inherits it and pushes from a new depth on every
	// iteration — and a Go slice is a value, so the grown header has to
	// reach that rule. Naming the owner makes it one write; walking the
	// ancestors instead was quadratic in the length of the list.
	owner := r.nodeHolder()
	switch owner.Node.(type) {
	case []any, ListRef:
		before := owner.Node
		owner.Node = NodeListAppend(owner.Node, val)
		r.Node = owner.Node
		// Only a parent BUILDING INTO THE SAME CONTAINER gets the grown
		// header. An unconditional write overwrote whatever the parent
		// held — the enclosing `@object$`'s map when a list is a member,
		// or the enclosing list when one array nests in another — and a
		// Go slice being a value made that silent rather than aliased.
		// Ownership answers it where slice identity cannot: an inherited
		// container gives parent and pusher the same holder, while a
		// freshly allocated one resets the pusher's owner to itself.
		if r.Parent != nil && r.Parent != NoRule &&
			r.Parent.nodeHolder() == owner {
			r.Parent.Node = owner.Node
		}
		// ...and back along the replacement chain. A rule replaced via
		// `r:` carries the chain on under a new Rule, and the parent's
		// Child still refers to the rule that was REPLACED — so a parent
		// reading the result (`@bubble$`, `@capture$`) reads that rule's
		// node. In TypeScript the replacement is handed the same array
		// OBJECT and pushing mutates it, so either pointer sees every
		// element; here a slice is a value and the replaced rule would
		// keep a shorter one.
		//
		// Only rules still holding the list this push grew are updated,
		// so a replacement that allocated a container of its own cannot
		// clobber the one it replaced — which is what TypeScript does,
		// and what child-pusher.fixture.json pins.
		//
		// `chain: false` says the grammar never reads a replaced rule,
		// and skips the walk. It is opt-in because the walk is correct
		// and this is not: a grammar declaring it gives up `$prev.node`
		// and anything else that resolves through `Rule.Prev`, which
		// `plugins.md` documents as part of the rule graph. What it buys
		// is the difference between O(n^2) and O(n) on a list built by a
		// rule that replaces itself per separator -- the shape
		// @tabnas/json's `elem` uses, and the one measured in
		// `doc/differences.md` at 15.1 M walk steps for 5,500 elements.
		//
		// TypeScript and Rust ignore the key, and that is the right
		// behaviour rather than a gap: they hand out the same list
		// object, so there is no republication to skip and nothing an
		// unread flag could change. An engine older than this one
		// ignores it too and keeps walking, which is slower and still
		// correct -- so this needs no builtin-schema bump, whose job is
		// to refuse a spec an old engine would MIS-handle.
		if cfgFlag(cfg, "chain", true) {
			for p := r.Prev; p != nil && p != NoRule && p != r; p = p.Prev {
				if !sameGrownList(p.Node, before) {
					break
				}
				p.Node = owner.Node
			}
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
		// Same as @bubble$: a lifted container keeps its owner.
		r.nodeOwner = r.Child.nodeHolder()
		return
	}
	from := cfgInt(cfg["from"])
	if from < 0 || from >= len(r.O) {
		r.Node = Undefined
		r.nodeOwner = nil
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
	r.nodeOwner = nil
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
func makeBuiltinPush(cfg map[string]any) AltAction {
	return func(r *Rule, ctx *Context) { builtinPushCfg(r, ctx, cfg) }
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
	builtinPush    = makeBuiltinPush(nil)
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
	"@push$":    makeBuiltinPush,
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
