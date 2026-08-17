// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Opt-in panic-mode error recovery — the Go port of the TS engine's
// options.parse.recover (ts/src/rules.ts). Everything here is reached
// only from the error path, and only when the flag is on: the match
// hot path is untouched.
//
// The shape of the port differs from TS in one structural way. TS
// recovers at its single throw funnel, inside bad(). Go has no throw
// funnel — it signals a grammar error by setting ctx.ParseErr and
// letting Rule.Process return — so recovery hooks the ONE place the
// parse loop tests ParseErr. That is the same single point in
// practice, reached by a different route.

// RecoverConfig is the resolved form of RecoverOptions, with sync
// token names already turned into tins.
type RecoverConfig struct {
	Enabled    bool
	SyncGroups []string
	// SyncTokens stays as NAMES here rather than tins: token names are
	// registered on the Tabnas instance, not on the config, so they
	// cannot be resolved at config-build time. They are resolved once
	// per parse into ctx.recoverSyncTins.
	SyncTokens    []string
	PopUntilValid bool
	MaxSkip       int
	MaxRecoveries int
	Suppress      int
}

// defaultSyncGroups is the engine's own set of close-alternate group
// tags that mark a sync edge. A grammar tags its close alternates with
// these (see toml's g: end / g: comma / g: close) to say where a parse
// can safely resume.
var defaultSyncGroups = []string{"close", "comma", "end"}

// closeInfo summarizes a rule spec's close alternates for recovery.
type closeInfo struct {
	sync map[Tin]bool // leading tins of close alts tagged with a sync group
	all  map[Tin]bool // leading tins of every close alt (structural fallback)
	any  bool         // spec has an empty-S close alt, so it closes on anything
}

// closeInfoOf summarizes spec's close alternates against the given sync
// groups. Unlike TS this is not cached: Go's AltSpec.S is read
// directly (TS needs its collated alt.t table), so the walk is a few
// slice reads over a handful of alternates, on an error path only.
func closeInfoOf(ctx *Context, spec *RuleSpec, groups []string) closeInfo {
	info := closeInfo{sync: map[Tin]bool{}, all: map[Tin]bool{}}
	if spec == nil {
		return info
	}
	for _, alt := range spec.close {
		if alt == nil {
			continue
		}
		slots := alt.S
		if ctx != nil {
			slots = ctx.altS(alt)
		}
		if len(slots) == 0 {
			// An empty-S close alternate matches any token.
			info.any = true
			continue
		}
		lead := slots[0]
		for _, tin := range lead {
			info.all[tin] = true
		}
		tagged := false
		for _, tag := range splitGroupTags(alt.G) {
			for _, want := range groups {
				if tag == want {
					tagged = true
					break
				}
			}
			if tagged {
				break
			}
		}
		if tagged {
			for _, tin := range lead {
				info.sync[tin] = true
			}
		}
	}
	return info
}

// computeSyncTins is the sync set for the current error, derived from
// the LIVE rule stack: leading tins of close alternates whose group
// tags intersect the configured sync groups, plus any explicit
// SyncTins.
//
// When the grammar carries no such tags the set comes back empty, and
// the structural fallback applies: the leading tin of every close
// alternate on the stack. That is what lets recovery work at all on
// the fleet's untagged grammars, at the cost of resuming at more
// places than a tagged grammar would.
func computeSyncTins(ctx *Context, rule *Rule, rec RecoverConfig) map[Tin]bool {
	out := map[Tin]bool{}

	addFrom := func(r *Rule, pick func(closeInfo) map[Tin]bool) {
		if r == nil || r == NoRule || r.Spec == nil {
			return
		}
		for tin := range pick(closeInfoOf(ctx, r.Spec, rec.SyncGroups)) {
			out[tin] = true
		}
	}
	sync := func(i closeInfo) map[Tin]bool { return i.sync }
	all := func(i closeInfo) map[Tin]bool { return i.all }

	addFrom(rule, sync)
	for d := ctx.RSI - 1; 0 <= d; d-- {
		addFrom(ctx.RS[d], sync)
	}

	// The fallback is decided on the GRAMMAR-derived set alone.
	// Explicit sync tokens are extras and must not disable it merely by
	// being present — otherwise adding one token to a tagged-free
	// grammar would silently narrow recovery to that one token.
	if 0 == len(out) {
		addFrom(rule, all)
		for d := ctx.RSI - 1; 0 <= d; d-- {
			addFrom(ctx.RS[d], all)
		}
	}

	for _, tin := range ctx.syncTins(rec) {
		out[tin] = true
	}
	return out
}

// syncTins resolves the configured sync token names against the
// parsing instance, once per parse. Recovery can fire up to
// MaxRecoveries times, so the names are not re-looked-up per error.
func (ctx *Context) syncTins(rec RecoverConfig) []Tin {
	if ctx.syncTinsDone {
		return ctx.recoverSyncTins
	}
	ctx.syncTinsDone = true
	if ctx.Inst == nil {
		return nil
	}
	for _, name := range rec.SyncTokens {
		ctx.recoverSyncTins = append(ctx.recoverSyncTins, ctx.Inst.resolveTokenName(name)...)
	}
	return ctx.recoverSyncTins
}

// acceptsClose reports whether spec's close state can consume tin. An
// empty-S close alternate accepts anything, which over-approximates:
// conditions and counters may still reject it. Cascade suppression
// absorbs the resulting follow-on error.
func acceptsClose(ctx *Context, spec *RuleSpec, tin Tin, groups []string) bool {
	info := closeInfoOf(ctx, spec, groups)
	return info.any || info.all[tin]
}

// midConstruct lists bad-token codes raised from INSIDE a compound
// construct — string-body escapes and control characters. Resuming the
// lexer just past the offending character would restart lexing
// mid-construct and mis-tokenize the remainder, since a stray quote
// would open a "new" string. For these, recovery advances to the next
// line end instead, a boundary no construct spans.
var midConstruct = map[string]bool{
	"unprintable":     true,
	"invalid_unicode": true,
	"invalid_ascii":   true,
}

// advanceLexPast moves the lexer's scan point past a bad token. A bad
// token is produced WITHOUT advancing the point (the matchers declined
// it), so skipping one means moving the point by hand, keeping the row
// and column counters honest as we go.
//
// Positions here are byte offsets, Go's unit throughout; the TS
// original counts UTF-16 units. Row detection walks bytes, which is
// safe because every RowChar is ASCII and no UTF-8 continuation byte
// can collide with one.
func advanceLexPast(lex *Lex, t *Token, toLineEnd bool) {
	if lex == nil || t == nil {
		return
	}
	src := lex.Src
	isRow := func(i int) bool {
		if i < 0 || i >= len(src) {
			return false
		}
		if lex.Config != nil && lex.Config.RowChars != nil {
			return lex.Config.RowChars[rune(src[i])]
		}
		return '\n' == src[i]
	}

	span := len(t.Src)
	if span < 1 {
		span = 1
	}
	target := t.SI + span
	if lex.pnt.SI > target {
		target = lex.pnt.SI
	}
	if toLineEnd {
		e := target
		for e < len(src) && !isRow(e) {
			e++
		}
		// Include the line end itself, so lexing resumes on a fresh row.
		if e < len(src) {
			e++
		}
		if e > target {
			target = e
		}
	}

	for lex.pnt.SI < target && lex.pnt.SI < len(src) {
		if isRow(lex.pnt.SI) {
			lex.pnt.RI++
			lex.pnt.CI = 1
		} else {
			lex.pnt.CI++
		}
		lex.pnt.SI++
	}
}

// attemptRecover runs one panic-mode recovery: record the error, skip
// forward to a sync token, pop the rule stack to a rule that can
// consume it, and return that rule so the parse loop continues. Nil
// means give up — the caller then fails the parse as before.
func attemptRecover(tkn *Token, rule *Rule, ctx *Context, isOpen bool) *Rule {
	rec := ctx.Cfg.Recover
	lex := ctx.Lex
	if lex == nil || tkn == nil {
		return nil
	}

	code := tkn.Err
	if "" == code {
		code = tkn.Why
	}
	if "" == code {
		code = "unexpected"
	}
	state := "close"
	if isOpen {
		state = "open"
	}
	use := map[string]any{"state": state}
	for k, v := range tkn.Use {
		use[k] = v
	}
	err := ctx.Inst.parser.makeErrorIn(
		ctx, code, tkn.Src, ctx.Src, tkn.SI, tkn.RI, tkn.CI, use)

	// Cascade suppression: an error within Suppress consumed tokens of
	// the previous recovery is a follow-on of the same fault, not news.
	// Drop it rather than report the same mistake several ways.
	suppressed := ctx.recoverAtSet && ctx.VAbs-ctx.recoverAt < rec.Suppress
	if suppressed && 0 < len(ctx.Errs) && ctx.Errs[len(ctx.Errs)-1] == err {
		ctx.Errs = ctx.Errs[:len(ctx.Errs)-1]
	}

	if rec.MaxRecoveries < len(ctx.Errs) {
		return nil
	}

	// Strict-progress guard. When nothing has been consumed since the
	// last recovery, the previous resume point failed to advance the
	// parse — every accepting alternate's condition rejected, say.
	// Requiring the next sync candidate to sit strictly beyond the
	// previous one bounds total recoveries by source length, so a
	// recovery can never loop in place.
	noProgress := ctx.recoverAtSet && ctx.recoverAt == ctx.VAbs
	lastSI := ctx.recoverSI
	if !ctx.recoverSISet {
		lastSI = -1
	}
	ctx.recoverAt = ctx.VAbs
	ctx.recoverAtSet = true

	sync := computeSyncTins(ctx, rule, rec)

	// Drain already-fetched lookahead first: those tokens advanced the
	// lexer and would be lost if we only pulled fresh ones.
	pending := []*Token{}
	for i := 0; i < len(ctx.T); i++ {
		if t := ctx.T[i]; t != nil && !t.IsNoToken() {
			pending = append(pending, t)
		}
		ctx.setT(i, NoToken)
	}

	fetch := func() *Token {
		for {
			// Defensive: with recovery on the lexer no longer latches
			// Err for unlexable input (it hands the #BD token over
			// instead), but Lex.Err is exported and a custom matcher can
			// still set it — and while latched the lexer answers #ZZ and
			// CACHES that in lex.end, so clearing only the error would
			// leave every later fetch reporting end-of-source. Clear
			// both. The end cache is dropped only while source remains,
			// so a genuine end still answers from the cache.
			lex.Err = nil
			if lex.pnt.SI < len(lex.Src) {
				lex.end = nil
			}
			t := lex.Next(rule)
			if t == nil {
				return NoToken
			}

			if lex.Config != nil && lex.Config.IgnoreSet[t.Tin] {
				continue
			}
			return t
		}
	}

	next := func() *Token {
		if 0 < len(pending) {
			t := pending[0]
			pending = pending[1:]
			return t
		}
		return fetch()
	}

	cand := next()
	skipped := 0
	for cand != nil && TinZZ != cand.Tin &&
		(!sync[cand.Tin] || (noProgress && cand.SI <= lastSI)) {
		if TinBD == cand.Tin {
			advanceLexPast(lex, cand, midConstruct[cand.Why])
		}
		if rec.MaxSkip <= skipped {
			return nil
		}
		skipped++
		cand = next()
	}
	if cand == nil {
		return nil
	}

	// End of source with no progress since the last recovery: the
	// grammar has already had its chance to close on the end token, so
	// give up rather than spin on the pinned one.
	if TinZZ == cand.Tin && noProgress && lastSI >= cand.SI {
		return nil
	}

	ctx.recoverSI = cand.SI
	ctx.recoverSISet = true

	// Restore the buffer: the sync token first, then whatever
	// pre-fetched lookahead is left, in its original order.
	ctx.setT(0, cand)
	for i := 0; i < len(pending); i++ {
		ctx.setT(i+1, pending[i])
	}

	// The skipped region, for diagnostics consumers.
	if err != nil {
		err.Recovered = &RecoveredAt{Skipped: skipped, Sync: cand.Tin}
	}

	// The parse continues, so the halt signals must be cleared. Leaving
	// ParseErr set would fail the loop on its very next iteration; and
	// the lexer LATCHES its first error, which the parse loop re-checks
	// after it ends — so a bad token recovery skipped past would still
	// fail the whole parse at the finish line, having been recovered
	// from perfectly. This latch is the one place Go's error channel
	// needs more clearing than the TS original, which throws instead.
	// The error is already recorded in ctx.Errs.
	ctx.ParseErr = nil
	ctx.parseErrDiag = nil
	lex.Err = nil

	if !rec.PopUntilValid {
		// Fixed-depth pop: exactly one rule.
		if 0 < ctx.RSI {
			ctx.RSI--
			return ctx.RS[ctx.RSI]
		}
		return nil
	}

	// Resume with the erroring rule itself when its close state accepts
	// the sync token, else pop ancestors until one does.
	if rule != nil && rule != NoRule &&
		acceptsClose(ctx, rule.Spec, cand.Tin, rec.SyncGroups) {
		if OPEN == rule.State {
			rule.State = CLOSE
		} else if !isOpen {
			// The failed pass was this rule's OWN close, so its
			// before-close actions have already run. Re-processing the
			// close would run them a second time and corrupt any that
			// are not idempotent — an element append, say.
			rule.skipBefores = true
		}
		return rule
	}

	// The erroring rule is being abandoned: synthesize its close event
	// first, so a structural consumer still sees a balanced stream.
	forceClose(ctx, rule)
	for 0 < ctx.RSI {
		ctx.RSI--
		r := ctx.RS[ctx.RSI]
		if r != nil && acceptsClose(ctx, r.Spec, cand.Tin, rec.SyncGroups) {
			return r
		}
		forceClose(ctx, r)
	}
	return nil
}

// forceClose emits the synthesized close event for a rule popped
// without a close pass, so outline and folding consumers see open and
// close paired even through recovery.
func forceClose(ctx *Context, r *Rule) {
	if r == nil || r == NoRule || 0 == len(ctx.RuleDoneSubs) {
		return
	}
	done := RuleDone{State: CLOSE, Alt: nil, Forced: true}
	for _, sub := range ctx.RuleDoneSubs {
		sub(r, ctx, done)
	}
}
