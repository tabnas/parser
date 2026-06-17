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
// Parity note vs TS: the TS AltAction takes a 3rd `alt` argument and the
// tree builtins read per-alt config from `alt.k` (avoiding `r.k`
// pollution). Go's AltAction is `func(*Rule, *Context)`, and the engine
// merges `alt.K` into `r.K` *before* running the action (rule.go), so
// the Go builtins read their config from `r.K`. Equivalent behaviour;
// the config keys (node$/capture$) ride in `r.K` and propagate to
// children, which is harmless for the bounded set the compiler emits.

import "reflect"

// BUILTIN_SCHEMA_VERSION is the config-schema version these builtins
// implement. A serialized grammar declaring GrammarSpec.V greater than
// this is refused at load. Absent (zero) ⇒ treated as version 1.
const BUILTIN_SCHEMA_VERSION = 1

// mkNode builds the AST node shape produced by the tree builtins:
// `{rule?, src, kids}`. `user` rules carry a `rule` tag; others omit it
// so they flatten into the enclosing user node. MUST stay byte-identical
// to @tabnas/bnf's mkAstNode (the cross-package AST-shape contract).
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

func mapConfig(r *Rule, key string) map[string]any {
	m, _ := r.K[key].(map[string]any)
	return m
}

// @node$ — allocate (when init) and/or accumulate matched terminals' src.
// Config in r.K["node$"] = {init?, rule?, kind?, nterms?}.
func builtinNode(r *Rule, _ *Context) {
	cfg := mapConfig(r, "node$")
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
func builtinCapture(r *Rule, _ *Context) {
	cfg := mapConfig(r, "capture$")
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

func asAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{}
}

// @probeInit$ — phase-0 open: mark the position and reset phase.
func builtinProbeInit(r *Rule, ctx *Context) {
	r.K["pd_phase"] = 0
	r.K["pd_mark"] = ctx.Mark()
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
		r.K["pd_phase"] = 1
	} else {
		r.K["pd_phase"] = 2
	}
}

func builtinProbePhase0(r *Rule, _ *Context) bool { return cfgInt(r.K["pd_phase"]) == 0 }
func builtinProbePhase1(r *Rule, _ *Context) bool { return cfgInt(r.K["pd_phase"]) == 1 }
func builtinProbePhase2(r *Rule, _ *Context) bool { return cfgInt(r.K["pd_phase"]) == 2 }

// BUILTIN_REFS is the standard builtin library. Tree/probe actions are
// registered as AltAction; the phase guards as AltCond — the resolver
// type-asserts the concrete type per field.
var BUILTIN_REFS = map[FuncRef]any{
	"@node$":        AltAction(builtinNode),
	"@capture$":     AltAction(builtinCapture),
	"@bubble$":      AltAction(builtinBubble),
	"@probeInit$":   AltAction(builtinProbeInit),
	"@probeDecide$": AltAction(builtinProbeDecide),
	"@probePhase0$": AltCond(builtinProbePhase0),
	"@probePhase1$": AltCond(builtinProbePhase1),
	"@probePhase2$": AltCond(builtinProbePhase2),
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
