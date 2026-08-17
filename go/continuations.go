// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import "sort"

// Continuations — the Go port of TS tn.continuations(src), the
// completion primitive of the unified-LSP design.
//
// The structural difference from TS is what it reads. TS collates a
// per-rule `tcol` lookahead table at normalize time and answers from
// that; Go has no tcol, so every set here is derived directly from
// AltSpec.S, which already holds the per-position tins. That is the
// same information without the intermediate table — closeInfoOf does
// the same thing for recovery.

// contNoRecoverMeta forces a single parse fail-fast regardless of the
// instance's recovery setting. Continuations must stop AT the query
// point; a recovering parse would skip past it and answer about
// somewhere else entirely.
const contNoRecoverMeta = "tabnas$contNoRecover"

// openLeadTins is the set of tokens a rule can open on: the leading
// tins of its open alternates. TS reads spec.def.tcol[0][0]; Go reads
// the alternates.
func openLeadTins(ctx *Context, spec *RuleSpec) []Tin {
	if spec == nil {
		return nil
	}
	out := []Tin{}
	for _, alt := range spec.open {
		if alt == nil {
			continue
		}
		slots := alt.S
		if ctx != nil {
			slots = ctx.altS(alt)
		}
		if 0 == len(slots) {
			continue
		}
		out = append(out, slots[0]...)
	}
	return out
}

// altMatchDepth counts how many leading positions of an alternate match
// the currently buffered lookahead, using the same rules as ParseAlts:
// an empty slot is a wildcard, and #AA matches anything.
func altMatchDepth(ctx *Context, alt *AltSpec) (depth int, total int) {
	slots := ctx.altS(alt)
	total = len(slots)
	k := 0
	for k < total {
		if k >= len(ctx.T) {
			break
		}
		tk := ctx.T[k]
		if tk == nil || tk.IsNoToken() {
			break
		}
		if 0 != len(slots[k]) && !tinMatch(tk.Tin, slots[k]) {
			break
		}
		k++
	}
	return k, total
}

// hasEmptyClose reports whether a spec can close on ANY token, which is
// what makes the pop closure legal: while a rule's close state has an
// empty-sequence catch-all, its parent's close continuations are legal
// at this point too.
func hasEmptyClose(ctx *Context, spec *RuleSpec) bool {
	if spec == nil {
		return false
	}
	for _, alt := range spec.close {
		if alt != nil && 0 == len(ctx.altS(alt)) {
			return true
		}
	}
	return false
}

// closeLeadTins is the leading tins of every close alternate.
func closeLeadTins(ctx *Context, spec *RuleSpec) []Tin {
	if spec == nil {
		return nil
	}
	out := []Tin{}
	for _, alt := range spec.close {
		if alt == nil {
			continue
		}
		slots := ctx.altS(alt)
		if 0 == len(slots) {
			continue
		}
		out = append(out, slots[0]...)
	}
	return out
}

// continuationTins is the set of tokens that could legally follow the
// buffered lookahead, asked of `rule` at buffer position atPos.
func continuationTins(ctx *Context, rule *Rule, atPos int) []Tin {
	out := map[Tin]bool{}
	if ctx == nil || rule == nil || rule == NoRule || rule.Spec == nil {
		return nil
	}

	stateAlts := rule.Spec.open
	if CLOSE == rule.State {
		stateAlts = rule.Spec.close
	}

	// The base set. A failed pass recorded a path-aware set at the
	// moment it failed, while its own lookahead was still buffered —
	// prefer that. Otherwise the prefix PARSED, and the same set is
	// computed from the live buffer instead.
	if 0 < len(ctx.contTins) {
		for _, tin := range ctx.contTins {
			out[tin] = true
		}
	} else {
		// Per alternate, only the position it is actually WAITING on
		// contributes. An alternate that stopped matching before atPos
		// mismatched a token already in the buffer, so what it wants
		// next would have to replace that token rather than follow it.
		for _, alt := range stateAlts {
			if alt == nil {
				continue
			}
			k, total := altMatchDepth(ctx, alt)
			if k == atPos && k < total {
				for _, tin := range ctx.altS(alt)[k] {
					out[tin] = true
				}
			}
		}
	}

	// Pop closure: while a rule can close on anything, its parent's
	// close continuations are legal here too. Bounded by the rule stack.
	r := rule
	d := ctx.RSI - 1
	for {
		if !hasEmptyClose(ctx, r.Spec) {
			break
		}
		if d < 0 {
			break
		}
		parent := ctx.RS[d]
		d--
		if parent == nil || parent == NoRule {
			break
		}
		for _, tin := range closeLeadTins(ctx, parent.Spec) {
			out[tin] = true
		}
		r = parent
	}

	// Push closure: an alternate FULLY matched here is about to push (or
	// become) another rule, so that rule's opening tokens are legal at
	// this point too — this is what makes `[1,` offer the next element's
	// value starters and not just `]`. Partially matched alternates are
	// excluded: they still want their own next token first.
	opened := map[string]bool{}
	var addOpeners func(name string)
	addOpeners = func(name string) {
		if "" == name || opened[name] {
			return
		}
		opened[name] = true
		spec := ctx.RSM[name]
		if spec == nil {
			return
		}
		for _, tin := range openLeadTins(ctx, spec) {
			out[tin] = true
		}
		// A rule that opens with an empty-sequence alternate hands over
		// to its own target immediately: follow that through.
		for _, alt := range spec.open {
			if alt != nil && 0 == len(ctx.altS(alt)) {
				addOpeners(alt.P)
				addOpeners(alt.R)
			}
		}
	}

	for _, alt := range stateAlts {
		if alt == nil {
			continue
		}
		k, total := altMatchDepth(ctx, alt)
		if k != total {
			continue
		}
		// Backtracking moves the handover point: an alternate matching
		// total tokens and pushing back B leaves the target rule
		// starting at total-B, so its openers are legal at the QUERY
		// position only when those coincide.
		if total-alt.B != atPos {
			continue
		}
		addOpeners(alt.P)
		addOpeners(alt.R)
	}

	tins := make([]Tin, 0, len(out))
	for tin := range out {
		tins = append(tins, tin)
	}
	sort.Slice(tins, func(i, j int) bool { return tins[i] < tins[j] })
	return tins
}

// Continuations returns the tokens that could legally continue src,
// treating it as a PREFIX — the completion primitive of the unified-LSP
// design. Returns the tins and their `#`-names, both sorted by tin.
//
// The set is an over-approximation: conditions and counters may still
// reject a listed token.
//
// A prefix that parses completely still answers. Permissive grammars
// read most mid-edit input as a complete document, and reporting
// nothing there would leave completion empty exactly where an editor
// asks for it. #ZZ is included whenever the prefix parses, meaning
// "stopping here is legal" — a completion provider should drop that
// sentinel from its item list rather than offer it.
func (j *Tabnas) Continuations(src string) ([]Tin, []string) {
	var atEnd []Tin
	haveEnd := false
	var failCtx *Context
	var failRule *Rule

	// Capture the continuation set at each end-of-source fetch, while
	// the rule stack is still live — after the parse returns it has
	// unwound. Every such fetch contributes: the first can belong to an
	// alternate that is later rejected, so pinning it would drop the
	// continuations of the branch that actually made the prefix parse.
	capture := func(tkn *Token, rule *Rule, ctx *Context) {
		if tkn == nil || TinZZ != tkn.Tin || rule == nil || rule == NoRule {
			return
		}
		// The lookahead position being fetched: the count of slots
		// already filled. A rule that matched a separator and is peeking
		// past it must be asked what is legal at THAT position.
		pos := 0
		for pos < len(ctx.T) && ctx.T[pos] != nil && !ctx.T[pos].IsNoToken() {
			pos++
		}
		got := continuationTins(ctx, rule, pos)
		if !haveEnd {
			atEnd = got
			haveEnd = true
			return
		}
		seen := map[Tin]bool{}
		for _, t := range atEnd {
			seen[t] = true
		}
		for _, t := range got {
			if !seen[t] {
				atEnd = append(atEnd, t)
			}
		}
	}

	// Remember the failing rule and context for the error path.
	watch := func(rule *Rule, ctx *Context) {
		failCtx = ctx
		failRule = rule
	}

	subs := append(append([]LexSub{}, j.lexSubs...), capture)
	rules := append(append([]RuleSub{}, j.ruleSubs...), watch)
	meta := map[string]any{contNoRecoverMeta: true}

	_, err := j.parser.startParse(src, meta, subs, rules, j)

	var tins []Tin
	if err == nil {
		if haveEnd {
			// The prefix parsed, so stopping here is legal. An EMPTY
			// capture is a real answer — "only the end may follow" — and
			// must not fall through to the start rule's openers, which
			// would offer tokens that restart the document.
			seen := map[Tin]bool{}
			for _, t := range atEnd {
				seen[t] = true
			}
			tins = append([]Tin{}, atEnd...)
			if !seen[TinZZ] {
				tins = append(tins, TinZZ)
			}
		} else {
			// No end-of-source event fired at all: an empty source that
			// the instance accepts never runs a lexer. Answer with the
			// start rule's own openers.
			tins = j.startOpeners()
		}
	} else {
		// The failing pass named its own rule; the pre-process
		// subscriber's last sighting is the fallback.
		if failCtx != nil {
			r := failCtx.contRule
			if r == nil {
				r = failRule
			}
			tins = continuationTins(failCtx, r, 0)
		}
		if 0 == len(tins) {
			tins = j.startOpeners()
		}
	}

	sort.Slice(tins, func(i, k int) bool { return tins[i] < tins[k] })
	names := make([]string, 0, len(tins))
	for _, tin := range tins {
		names = append(names, j.tokenName(tin))
	}
	return tins, names
}

// tokenName resolves a tin to its `#`-name.
func (j *Tabnas) tokenName(tin Tin) string {
	if name, ok := j.nameByTin[tin]; ok {
		return name
	}
	return ""
}

// startOpeners is the start rule's own opening tokens — the answer for
// an empty or unparseable prefix, where no rule ever became live.
func (j *Tabnas) startOpeners() []Tin {
	name := j.parser.Config.RuleStart
	if "" == name {
		name = "val"
	}
	spec := j.parser.RSM[name]
	if spec == nil {
		return nil
	}
	seen := map[Tin]bool{}
	out := []Tin{}
	for _, tin := range openLeadTins(nil, spec) {
		if !seen[tin] {
			seen[tin] = true
			out = append(out, tin)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i] < out[k] })
	return out
}

// recordContTins captures the path-aware continuation set at a failed
// rule pass, while that pass's lookahead is still buffered. Called from
// Rule.Process; Continuations reads it back.
func recordContTins(ctx *Context, rule *Rule, alts []*AltSpec) {
	if ctx == nil {
		return
	}
	seen := map[Tin]bool{}
	out := []Tin{}
	for _, alt := range alts {
		if alt == nil {
			continue
		}
		k, total := altMatchDepth(ctx, alt)
		if k >= total {
			continue
		}
		for _, tin := range ctx.altS(alt)[k] {
			if !seen[tin] {
				seen[tin] = true
				out = append(out, tin)
			}
		}
	}
	ctx.contTins = out
	ctx.contRule = rule
}
