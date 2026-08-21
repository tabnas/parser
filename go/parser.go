// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// Mutable parse state threaded through every rule, matching the TS Context type.
type Context struct {
	UI int // Unique rule ID counter (TS: uI).

	// Generalized lookahead buffer: T[i] is the token at position i, or NoToken
	// if that slot is not yet fetched. Supersedes the legacy T0/T1 two-slot
	// fields, which are kept in sync for backward compatibility.
	T []*Token

	T0 *Token // Alias of T[0] (legacy), kept in sync with T[0].
	T1 *Token // Alias of T[1] (legacy), kept in sync with T[1].
	V1 *Token // Previous token (TS: v1).
	V2 *Token // Previous-previous token (TS: v2).

	// Consumed-token rewind history: V holds the tokens consumed so far, bounded
	// by cfg.RewindHistory (a ring buffer trimmed from the front). VAbs is the
	// absolute consumed count used as the Mark() value, decoupled from len(V) so
	// the ring-buffer cap can evict old tokens without invalidating live marks.
	V    []*Token // Retained consumed-token ring buffer (TS: v).
	VAbs int      // Absolute count of consumed tokens; the Mark() value (TS: vAbs).
	Lex  *Lex     // Attached by startParse; used by Rewind to re-feed tokens.

	RS       []*Rule              // Rule stack (TS: rs).
	RSI      int                  // Rule stack index (TS: rsI).
	RSM      map[string]*RuleSpec // Rule spec map (TS: rsm).
	KI       int                  // Iteration counter (TS: kI).
	Rule     *Rule                // Current parsing rule (TS: rule).
	Meta     map[string]any       // Parse metadata (TS: meta).
	LexSubs  []LexSub             // Lex event subscribers (TS: sub.lex).
	RuleSubs []RuleSub            // Rule event subscribers (TS: sub.rule).

	// Post-process rule event subscribers (TS: sub.ruleDone). Populated
	// from the instance by startParse; see Tabnas.SubRuleDone.
	RuleDoneSubs []RuleDoneSub

	// The alternate the current rule pass resolved to, recorded by
	// Rule.Process for the post-process event (TS: ctx._dalt). daltAny
	// records whether the state had ANY alternates, which is how a rule
	// with no alternates at all is told apart from one whose alternates
	// all failed; daltErr carries the failure token in the latter case.
	dalt    *AltSpec
	daltAny bool
	daltErr *Token

	// Recovery bookkeeping (TS: ctx._recoverAt / _recoverSI). recoverAt
	// is the consumed-token count at the last recovery, for cascade
	// suppression; recoverSI is the source offset it resumed at, for
	// the strict-progress guard. The *Set flags distinguish "zero" from
	// "never recovered".
	recoverAt    int
	recoverAtSet bool
	recoverSI    int
	recoverSISet bool
	// Sync token names resolved once per parse (see Context.syncTins).
	recoverSyncTins []Tin
	syncTinsDone    bool

	// Path-aware continuation record, written at the failure point by
	// Rule.Process (TS: ctx._contTins / the failing rule). The collated
	// answer is position-blind: after matching A of [A,B], a sibling
	// [C,D] must not contribute D, and only the failing pass knows how
	// far each alternate actually got.
	contTins []Tin
	contRule *Rule

	// Unlexable-run coalescing (TS: ctx._badTo / _badErr). badTo is the
	// source offset just past the last absorbed bad token, so a token
	// starting at or before it belongs to the SAME run and grows the
	// existing diagnostic instead of recording another.
	badTo    int
	badToSet bool
	badErr   *TabnasError
	ParseErr *Token // Error token; when set, halts the parse.

	// Errors recorded during this parse (TS: ctx.errs). Appended at
	// each error's CONSTRUCTION site, so the error returned by Parse is
	// also the last entry. Distinct from ParseErr, which is the
	// grammar-facing error TOKEN that halts the parse and stays a
	// single slot (documented plugin API — see doc/plugins.md).
	// Groundwork for opt-in multi-error recovery.
	Errs []*TabnasError

	Opts    *Options         // Tabnas instance options (TS: opts).
	Cfg     *LexConfig       // Tabnas instance config (TS: cfg).
	Src     string           // Source text being parsed (TS: src).
	Inst    *Tabnas          // Current Tabnas instance (TS: inst).
	U       map[string]any   // Custom plugin data bag (TS: u).
	Root    *Rule            // Root rule (TS: root).
	TC      int              // Token count (TS: tC).
	F       func(any) string // Format a value as a string (TS: F).
	Log     func(...any)     // Debug logger (TS: log).
	NOTOKEN *Token           // Sentinel no-token (TS: NOTOKEN).
	NORULE  *Rule            // Sentinel no-rule (TS: NORULE).

	// tokenSetDyn is set when the parsing instance carries custom token sets,
	// so alts that name a token set must be re-resolved against it rather
	// than matched on the tins frozen when the alt was registered.
	// altSlots memoizes that re-resolution for the duration of one parse;
	// it is per-Context, so concurrent parses on one instance stay race-free.
	tokenSetDyn bool
	altSlots    map[*AltSpec][][]Tin

	// parseErrDiag freezes the structured-diagnostic context (rule stack,
	// failing rule, expected tokens) at the moment ctx.ParseErr is set —
	// rule.Process keeps mutating RS/RSI and flips the rule state after
	// recording the error, while the TS engine throws at the raise site,
	// so finishErr must not read those fields live for grammar errors.
	parseErrDiag *diagSnapshot
}

// altS returns the effective per-position Tin sets for an alt, re-resolving
// any position that names a token set against the parsing instance. Alts
// without recorded names, and instances without custom token sets, use
// AltSpec.S unchanged. The result is memoized per parse, so the walk below
// runs at most once per alt.
// setT writes the lookahead buffer slot i, keeping the legacy T0 / T1
// aliases in sync so grammar and plugin code reading them observes the
// same values as ctx.T[0] / ctx.T[1].
func (ctx *Context) setT(i int, tkn *Token) {
	for len(ctx.T) <= i {
		ctx.T = append(ctx.T, NoToken)
	}
	ctx.T[i] = tkn
	if i == 0 {
		ctx.T0 = tkn
	} else if i == 1 {
		ctx.T1 = tkn
	}
}

// dropT clears lookahead slots from i onward, so they are re-fetched on
// demand. Used after a negotiated re-cut, where everything buffered beyond
// the recut token was lexed from positions that may no longer exist.
func (ctx *Context) dropT(i int) {
	for j := i; j < len(ctx.T); j++ {
		ctx.setT(j, NoToken)
	}
}

func (ctx *Context) altS(alt *AltSpec) [][]Tin {
	if !ctx.tokenSetDyn || alt.SNames == nil || ctx.Inst == nil {
		return alt.S
	}
	if slots, ok := ctx.altSlots[alt]; ok {
		return slots
	}
	slots := alt.S
	// Only positions that actually name a token set need re-resolving;
	// everything else keeps the tins already resolved for it. A position
	// naming anything this instance does not know is left alone too —
	// resolving it would have to mint a token mid-parse.
	var rebuilt [][]Tin
	for i, names := range alt.SNames {
		if i >= len(alt.S) {
			break
		}
		usesSet, resolvable := false, true
		for _, name := range names {
			switch {
			case ctx.Inst.hasTokenSet(strings.TrimPrefix(name, "#")):
				usesSet = true
			case ctx.Inst.hasToken(name):
			default:
				resolvable = false
			}
		}
		if !usesSet || !resolvable {
			continue
		}
		if rebuilt == nil {
			rebuilt = make([][]Tin, len(alt.S))
			copy(rebuilt, alt.S)
		}
		var tins []Tin
		for _, name := range names {
			tins = append(tins, ctx.Inst.resolveTokenName(name)...)
		}
		rebuilt[i] = tins
	}
	if rebuilt != nil {
		slots = rebuilt
	}
	if ctx.altSlots == nil {
		ctx.altSlots = make(map[*AltSpec][][]Tin)
	}
	ctx.altSlots[alt] = slots
	return slots
}

// recordConsumed appends the leading `consumed` lookahead tokens to the
// rewind history, advances VAbs, and applies the ring-buffer cap. Called
// before an alt's action runs so a ctx.Rewind() inside that action sees
// the just-matched tokens. Mirrors the TS rules.ts v-history push.
func (ctx *Context) recordConsumed(consumed int) {
	if consumed <= 0 {
		return
	}
	// Maintain the legacy 2-slot history (V1 = last consumed, V2 = prior)
	// from the matched tokens before their tbuf slots are cleared below.
	if consumed == 1 {
		ctx.V2 = ctx.V1
		ctx.V1 = ctx.T[0]
	} else if consumed >= 2 {
		ctx.V2 = ctx.T[consumed-2]
		ctx.V1 = ctx.T[consumed-1]
	}
	// Move consumed tokens into the rewind history and clear their tbuf
	// slots, so a ctx.Rewind() inside the action distinguishes "already
	// in V" (NoToken slot, replayed from V) from genuine pre-lexed
	// lookahead past the consumed position (a real token still in T).
	for i := 0; i < consumed && i < len(ctx.T); i++ {
		ctx.V = append(ctx.V, ctx.T[i])
		ctx.T[i] = NoToken
	}
	ctx.VAbs += consumed
	// Amortised-O(1) ring buffer: let V grow to twice the cap, then trim
	// its front back down to the cap. A non-positive cap means unbounded
	// (TS Infinity).
	cap := ctx.Cfg.RewindHistory
	if cap > 0 && len(ctx.V) > 2*cap {
		ctx.V = append([]*Token(nil), ctx.V[len(ctx.V)-cap:]...)
	}
}

// Mark records a rewind point at the current parse position. The returned
// value can be passed to Rewind to replay the tokens consumed since the
// mark. Mirrors TS ctx.mark().
func (ctx *Context) Mark() int {
	return ctx.VAbs
}

// Rewind replays the tokens consumed since the given mark, re-feeding them
// through the lexer's pending-token queue so the parser re-reads them.
// Returns an error if the mark has been evicted from the retained history
// window (cfg.RewindHistory was too small for the grammar). Mirrors TS
// ctx.rewind(), which throws in that case; Go reports it as an error to
// preserve the no-panic guarantee.
func (ctx *Context) Rewind(mark int) error {
	k := ctx.VAbs - mark
	if k <= 0 {
		return nil
	}
	if k > len(ctx.V) {
		return fmt.Errorf(
			"tabnas: ctx.Rewind target %d is outside the retained history "+
				"window (oldest mark available is %d, current is %d); "+
				"increase options.rewind.history",
			mark, ctx.VAbs-len(ctx.V), ctx.VAbs)
	}

	// Preserve the lookahead buffer (tokens the lexer already produced
	// past the consumed position): collect them in order, clear the
	// slots, and re-queue them BEHIND the rewound consumed tokens so the
	// next fetch serves consumed-then-lookahead in original order.
	var lookahead []*Token
	for i := 0; i < len(ctx.T); i++ {
		if ctx.T[i] != nil && ctx.T[i] != NoToken {
			lookahead = append(lookahead, ctx.T[i])
		}
		ctx.T[i] = NoToken
	}
	ctx.T0 = NoToken
	ctx.T1 = NoToken

	// The rewound consumed tokens are the last k of V, oldest-first.
	rewound := make([]*Token, k)
	copy(rewound, ctx.V[len(ctx.V)-k:])
	ctx.V = ctx.V[:len(ctx.V)-k]
	ctx.VAbs -= k

	if ctx.Lex != nil {
		prefix := append(rewound, lookahead...)
		ctx.Lex.tokens = append(prefix, ctx.Lex.tokens...)
		// Clear any cached end-of-source token so the lexer serves from
		// the replenished queue rather than short-circuiting to #ZZ.
		ctx.Lex.end = nil
	}
	return nil
}

// Parser orchestrates the parsing process for one grammar configuration.
type Parser struct {
	Config        *LexConfig           // Lexer/parser configuration.
	RSM           map[string]*RuleSpec // Rule spec map, keyed by rule name.
	MaxMul        int                  // Max rule occurrence multiplier. Default: 3.
	ErrorMessages map[string]string    // Custom error message templates.
	Hints         map[string]string    // Explanatory hints per error code.
	ErrTag        string               // Custom error tag (TS: errmsg.name). Default: "tabnas".
}

// NewParser creates a parser with default configuration.
func NewParser() *Parser {
	cfg := DefaultLexConfig()
	rsm := make(map[string]*RuleSpec)
	// Copy global error messages and hints as defaults; parser fields
	// alias the config maps (see makeError / makeTabnasError).
	cfg.ErrorMessages = make(map[string]string, len(errorMessages))
	for k, v := range errorMessages {
		cfg.ErrorMessages[k] = v
	}
	cfg.Hints = make(map[string]string, len(defaultHints))
	for k, v := range defaultHints {
		cfg.Hints[k] = v
	}
	return &Parser{Config: cfg, RSM: rsm, MaxMul: 3,
		ErrorMessages: cfg.ErrorMessages, Hints: cfg.Hints}
}

// Start parses the source string and returns the result.
// Returns a *TabnasError if parsing fails.
func (p *Parser) Start(src string) (any, error) {
	return p.startParse(src, nil, nil, nil, nil)
}

// StartMeta parses the source string with metadata, subscriptions, and
// an optional Tabnas instance reference (for Context.Inst).
func (p *Parser) StartMeta(src string, meta map[string]any, lexSubs []LexSub, ruleSubs []RuleSub) (any, error) {
	return p.startParse(src, meta, lexSubs, ruleSubs, nil)
}

// startParse is the internal entry point that populates the full Context.
// A recover guard converts any panic (from plugin callbacks, custom
// matchers, or engine bugs) into an "internal" TabnasError, upholding the
// package guarantee that parsing never panics, whatever the input.
func (p *Parser) startParse(src string, meta map[string]any, lexSubs []LexSub, ruleSubs []RuleSub, inst *Tabnas) (result any, err error) {
	// Bound to the parse context as soon as one exists, so a panic
	// raised after that point (plugin action, custom matcher, engine
	// bug) still records into ctx.Errs. Without it a subscriber that
	// captured the context sees an empty Errs while Parse returns an
	// error, breaking the invariant that the returned error is the last
	// entry. recordErr is nil-safe, so a panic raised before the
	// context is built needs no extra guard.
	var pctx *Context
	defer func() {
		if r := recover(); r != nil {
			result = nil
			// A *TabnasError keeps its own code. A grammar action that
			// raises one is REPORTING A DEFECT IN THE INPUT — a TOML key
			// conflict, say — and relabelling it "internal" turns a
			// diagnosis the plugin author wrote into an engine bug.
			//
			// TypeScript already does this: tabnas.ts's catch is
			// `if (e instanceof TabnasError) err = e; else throw e`, so a
			// thrown TabnasError reaches the caller with its code intact.
			// Go flattened every panic alike, which is why @tabnas/toml's
			// key-conflict check exists in the TypeScript port and not in
			// the Go one — there was no way to say it.
			//
			// Everything else still becomes "internal". Go cannot take
			// TypeScript's other branch and re-panic: this package
			// promises never to panic on a caller, which is what
			// nopanic_test.go pins. So the two ports agree on the case
			// that carries meaning and differ only where Go has a
			// stronger guarantee to keep.
			// `ok && te != nil` — a typed NIL is not a usable error.
			// `var te *TabnasError; panic(te)` satisfies the assertion
			// with a nil pointer, and recording it puts a non-nil error
			// INTERFACE holding a nil pointer into the named result:
			// Parser.Start then returns something that is != nil and
			// panics on .Error(). Measured, and entrypoint-dependent —
			// Tabnas.Parse only degraded to "internal" because a later
			// nil dereference tripped another recover.
			if te, ok := r.(*TabnasError); ok && te != nil {
				// Rebuilt through the normal funnel so a preserved error
				// arrives with the context every other error carries:
				// the full source (for the excerpt), the instance tag,
				// the hint, and the rule/token names finishErr adds.
				// A plugin can only populate the EXPORTED fields, so
				// without this the code survived and the diagnostic did
				// not — an error that renders no source excerpt and
				// serialises empty rule context.
				re := p.makeError(te.Code, te.Src, src, te.Pos, te.Row, te.Col)
				if te.Detail != "" {
					re.Detail = te.Detail
				}
				if te.Hint != "" {
					re.Hint = te.Hint
				}
				err = pctx.recordErr(
					p.finishErr(re, pctx, meta, nil).(*TabnasError))
			} else {
				err = pctx.recordErr(
					p.makeError("internal", fmt.Sprint(r), src, 0, 1, 1))
			}
		}
	}()

	if src == "" {
		return nil, nil
	}

	lex := NewLex(src, p.Config)

	var opts *Options
	if inst != nil {
		opts = inst.options
	}

	ctx := &Context{
		UI: 0,
		T:  []*Token{NoToken, NoToken},
		T0: NoToken,
		T1: NoToken,
		V1: NoToken,
		V2: NoToken,
		// Rule stack depth is bounded by grammar nesting (typically tens),
		// not source length; start small and let the push path's append
		// grow it (matches the TS runtime, whose stack starts empty).
		RS:       make([]*Rule, 0, 64),
		RSI:      0,
		RSM:      p.RSM,
		Meta:     meta,
		LexSubs:  lexSubs,
		RuleSubs: ruleSubs,
		Opts:     opts,
		// Read off the instance rather than taken as a parameter:
		// startParse's signature is mirrored by the exported StartMeta,
		// and widening that would break every caller for an event they
		// may not use.
		RuleDoneSubs: ruleDoneSubsOf(inst),
		Cfg:          p.Config,
		Src:          src,
		Inst:         inst,
		U:            make(map[string]any),
		TC:           0,
		NOTOKEN:      NoToken,
		NORULE:       NoRule,
		F:            func(v any) string { return Str(v, 44) },
	}

	// Late-bind token-set positions only when this instance actually
	// overrides a set — the default instance pays nothing.
	ctx.tokenSetDyn = inst != nil && len(inst.customTokenSets) > 0

	lex.Ctx = ctx
	ctx.Lex = lex
	pctx = ctx

	// Publish the per-parse error list back to ParseRecover on EVERY
	// return path, including a panic. startParse has many exits, and a
	// caller that asked for the recovered errors must not lose them
	// because the parse left by one this forgot about.
	if meta != nil {
		if slot, ok := meta[recoverErrsMeta].(*[]*TabnasError); ok && slot != nil {
			defer func() { *slot = ctx.Errs }()
		}
	}

	startName := p.Config.RuleStart
	if startName == "" {
		startName = "val"
	}
	startSpec := p.RSM[startName]
	if startSpec == nil {
		return nil, nil
	}

	rule := MakeRule(startSpec, ctx, nil)
	root := rule
	ctx.Root = root

	// Run parse.prepare hooks
	if len(p.Config.ParsePrepare) > 0 {
		for _, prep := range p.Config.ParsePrepare {
			prep(ctx)
		}
	}

	// Maximum iterations: 2 * numRules * srcLen * 2 * maxmul
	//
	// Multiplied with saturation, not with wrapping. A runaway guard
	// whose budget can go NEGATIVE is worse than no guard: the floor
	// below turns any negative product into 100, so a large MaxMul made
	// the guard STRICTER and rejected valid input with `unexpected` — a
	// syntax code for what is purely a configuration value. Measured at
	// MaxMul = 2^63-1, which rejected a 61-element array that the
	// default 3 accepts. Audit item P7.
	maxmul := p.MaxMul
	if maxmul <= 0 {
		maxmul = 3
	}
	maxr := satMul(satMul(2*len(p.RSM)*2, utf16Len(src)), maxmul)
	if maxr < 100 {
		maxr = 100
	}

	// Opt-in parse budget: call OnCheck every budgetN rule iterations;
	// a false return cancels. Off by default, costing one integer test
	// per iteration. Hoisted out of the loop for the same reason TS
	// hoists it — this loop is the engine's hot path.
	budgetN := p.Config.ParseBudgetN
	budgetCheck := p.Config.ParseBudgetCheck

	// gaveUp marks a parse that stopped early because recovery could
	// not continue. The post-loop completeness checks (trailing
	// content, latched lexer error) are skipped for it: the parse is
	// KNOWN incomplete, and reporting that as a fresh error would
	// replace the recovered diagnostics with a misleading one.
	gaveUp := false

	// Continuations forces a single parse fail-fast whatever the
	// instance's recovery setting: it must stop AT the query point, and
	// a recovering parse would skip past it and answer about somewhere
	// else entirely.
	noRecover := false
	if meta != nil {
		noRecover, _ = meta[contNoRecoverMeta].(bool)
	}

	kI := 0
	for rule != NoRule && kI < maxr {
		ctx.KI = kI
		ctx.Rule = rule

		// kI > 0 so the check never fires before any work is done: an
		// interval of N means "every N iterations", not "immediately
		// and then every N".
		if budgetN > 0 && budgetCheck != nil && kI > 0 && kI%budgetN == 0 {
			if !budgetCheck(ctx) {
				tkn := ctx.T0
				if tkn == nil {
					tkn = NoToken
				}
				je := p.makeErrorIn(
					ctx, "cancel", tkn.Src, src, tkn.SI, tkn.RI, tkn.CI)
				return nil, p.finishErr(je, ctx, meta, tkn)
			}
		}

		// Fire rule subscribers BEFORE process (matching TS).
		if len(ctx.RuleSubs) > 0 {
			for _, sub := range ctx.RuleSubs {
				sub(rule, ctx)
			}
		}

		prev := rule
		prevState := rule.State

		rule = rule.Process(ctx, lex)

		// Post-process event: fires with the pass's matched tokens
		// recorded on prev and the state transition applied, so unlike
		// the pre-process event above it can report what the pass
		// actually DID. Dispatched here rather than inside Process for
		// the same reason TS dispatches from its loop: Process has
		// several return points, and one dispatch site cannot miss one.
		if len(ctx.RuleDoneSubs) > 0 {
			done := RuleDone{State: prevState, Alt: ctx.ruleDoneAlt()}
			for _, sub := range ctx.RuleDoneSubs {
				sub(prev, ctx, done)
			}
		}

		// Check for parse error from alt.E or actions.
		if ctx.ParseErr != nil {
			// Opt-in recovery: record the error and resume from a sync
			// point instead of ending the parse. This ParseErr test is
			// Go's throw funnel — the one place a grammar error is
			// observed — so it is where recovery hooks, mirroring the
			// single raise site TS recovers at inside bad().
			if p.Config.Recover.Enabled && !noRecover {
				if resumed := attemptRecover(
					ctx.ParseErr, prev, ctx, OPEN == prevState); resumed != nil {
					rule = resumed
					kI++
					continue
				}
				// Recovery gave up — the skip cap was hit, or no rule on
				// the stack could accept the sync token. The parse still
				// has a partial value, and that is more useful to a
				// language server than nothing, so end the loop and
				// return what was built rather than failing. This is the
				// give-up half of TS's { value, errors } contract: TS
				// returns the shape there too, never throwing once
				// recovery is on.
				gaveUp = true
				ctx.ParseErr = nil
				ctx.parseErrDiag = nil
				lex.Err = nil
				break
			}

			// Prefer lexer errors (e.g. unterminated_string) over generic
			// "unexpected" from alt matching, since the lex error is more
			// specific about what went wrong. Matches TS behavior where
			// lex errors propagate through #ZZ tokens.
			if lex.Err != nil {
				return nil, p.finishErr(lex.Err, ctx, meta, nil)
			}
			tkn := ctx.ParseErr
			// Use the token's own error code when set (TS parity:
			// rules.ts bad() throws TabnasError(tkn.err || unexpected))
			// and inject its Use details (e.g. rulename) into the
			// message and hint templates.
			code := "unexpected"
			if tkn.Err != "" {
				code = tkn.Err
			}
			// tkn.Use goes IN to the single injection pass rather than being
			// applied over the already-rendered text: a detail whose name
			// collides with a location key (csv's {row}) would otherwise find
			// its placeholder already consumed.
			je := p.makeErrorIn(ctx, code, tkn.Src, src, tkn.SI, tkn.RI, tkn.CI, tkn.Use)
			return nil, p.finishErr(je, ctx, meta, tkn)
		}

		kI++
	}

	// With recovery on the parse never fails outright: a completeness
	// failure past this point is recorded as one more diagnostic and
	// the partial value still goes back, matching TS, where parse()
	// with recovery enabled always returns { value, errors } and never
	// throws. `soft` therefore disables every hard return below —
	// gaveUp implies it, and so does simply having recovered earlier.
	soft := (p.Config.Recover.Enabled && !noRecover) || gaveUp

	// Check for lexer errors (unterminated strings, comments, etc.)
	if lex.Err != nil && !soft {
		return nil, p.finishErr(lex.Err, ctx, meta, nil)
	}
	if lex.Err != nil && soft {
		// Record it so the caller still learns the source was not fully
		// lexable, then stop it failing the finished parse.
		//
		// Unless recovery already reported this exact fault: the latched
		// lexer error is the SAME one recovery skipped past, so
		// recording it again would show a consumer two squiggles at one
		// offset for one mistake.
		if je, ok := lex.Err.(*TabnasError); ok && !ctx.alreadyRecorded(je) {
			ctx.recordErr(je)
		}
		lex.Err = nil
	}

	// Check for unconsumed tokens (syntax error) - explicit trailing content check.
	// First check tokens already in the lookahead buffer.
	//
	// In soft mode the trailing-content error is RECORDED, not
	// returned: TS raises the same TabnasError — whose constructor
	// records it on ctx.errs — and its recovery catch converts the
	// throw into the { value, errors } result, keeping the partial
	// value. Skipping the check entirely (as this path once did) made
	// the Go port silently ACCEPT `"x" q` under recovery while TS
	// reported `unexpected` — an accept/reject divergence, not a span
	// one. One recorded error, then straight to the result, mirroring
	// the single throw: the second lookahead below runs only when the
	// buffer held nothing.
	//
	// NOT on the give-up path: when recovery gave up, TS threw mid-loop
	// and its catch converts — the post-loop checks never run, and the
	// terminal fault is already recorded. Running them here fetched a
	// LATER unrelated token and reported it as a second trailing error
	// (strict JSON, MaxSkip 0, `[1 : abc def ghi]`: errors at `:` AND
	// `a` where TS stops at `:`).
	//
	// A #BD token in trailing content carries its own error code —
	// unterminated_string, invalid_unicode, … — and TS raises
	// `why || unexpected` with the token's use details (parser.ts
	// buffered/endtry checks). The absorbBad precedence (Why, Err,
	// "unexpected") is the Go form of that.
	bdCode := func(tkn *Token) string {
		if TinBD == tkn.Tin {
			if "" != tkn.Why {
				return tkn.Why
			}
			if "" != tkn.Err {
				return tkn.Err
			}
		}
		return "unexpected"
	}
	trailing := !gaveUp && ctx.T0 != nil && !ctx.T0.IsNoToken() && ctx.T0.Tin != TinZZ
	if trailing && !soft {
		// Prefer lex errors over generic unexpected for unconsumed tokens too.
		if lex.Err != nil {
			return nil, p.finishErr(lex.Err, ctx, meta, nil)
		}
		return nil, p.finishErr(p.makeErrorIn(ctx, bdCode(ctx.T0), ctx.T0.Src, src, ctx.T0.SI, ctx.T0.RI, ctx.T0.CI, ctx.T0.Use), ctx, meta, ctx.T0)
	}
	if trailing && soft {
		je := p.makeError(bdCode(ctx.T0), ctx.T0.Src, src, ctx.T0.SI, ctx.T0.RI, ctx.T0.CI, ctx.T0.Use)
		if !ctx.alreadyRecorded(je) {
			ctx.recordErr(je)
			_ = p.finishErr(je, ctx, meta, ctx.T0)
		}
	}
	if !trailing && !gaveUp {
		// Also explicitly ask lexer for more (matching TS parser.ts:187-189).
		// `rule` is NoRule here (the loop has ended) and Lex.Next assigns its
		// rule argument to ctx.Rule — which would clobber the last REAL rule
		// before finishErr reads it for the diagnostic (TS lex.next performs
		// no such assignment, so its ctx.rule keeps the last processed rule).
		// Save/restore rather than passing ctx.Rule so the lex call itself
		// still sees NoRule, exactly as before.
		curRule := ctx.Rule
		endTkn := lex.Next(rule)
		ctx.Rule = curRule
		if endTkn.Tin != TinZZ {
			if !soft {
				if lex.Err != nil {
					return nil, p.finishErr(lex.Err, ctx, meta, nil)
				}
				return nil, p.finishErr(p.makeErrorIn(ctx, bdCode(endTkn), endTkn.Src, src, endTkn.SI, endTkn.RI, endTkn.CI, endTkn.Use), ctx, meta, endTkn)
			}
			var je *TabnasError
			if e, ok := lex.Err.(*TabnasError); ok && e != nil {
				je = e
			} else {
				je = p.makeError(bdCode(endTkn), endTkn.Src, src, endTkn.SI, endTkn.RI, endTkn.CI, endTkn.Use)
			}
			if !ctx.alreadyRecorded(je) {
				ctx.recordErr(je)
				_ = p.finishErr(je, ctx, meta, endTkn)
			}
			lex.Err = nil
		}
		// Check lexer errors from that final Next() call.
		if lex.Err != nil {
			if !soft {
				return nil, p.finishErr(lex.Err, ctx, meta, nil)
			}
			if je, ok := lex.Err.(*TabnasError); ok && !ctx.alreadyRecorded(je) {
				ctx.recordErr(je)
			}
			lex.Err = nil
		}
	}

	// Follow replacement chain: when val is replaced by list (implicit list),
	// root.Node is stale. Follow Next/Prev links to find the actual result.
	resRule := root
	for resRule.Next != NoRule && resRule.Next != nil && resRule.Next.Prev == resRule {
		resRule = resRule.Next
	}

	if IsUndefined(resRule.Node) {
		return nil, nil
	}

	// Check result.fail
	if len(p.Config.ResultFail) > 0 {
		for _, fail := range p.Config.ResultFail {
			if resRule.Node == fail {
				return nil, p.finishErr(p.makeErrorIn(ctx, "unexpected", "", src, 0, 1, 1), ctx, meta, nil)
			}
		}
	}

	return resRule.Node, nil
}

// finishErr enriches a parse error with context for the formatted output:
// the source file name (meta["fileName"], TS: meta.fileName), the active
// rule, and the failing token — feeding the "--internal: ..." suffix.
func (p *Parser) finishErr(err error, ctx *Context, meta map[string]any, tkn *Token) error {
	je, ok := err.(*TabnasError)
	if !ok || je == nil {
		return err
	}
	if meta != nil {
		if fn, ok := meta["fileName"].(string); ok && je.fileName == "" {
			je.fileName = fn
		}
	}
	if ctx != nil && ctx.Rule != nil && je.ruleName == "" {
		je.ruleName = ctx.Rule.Name
		je.ruleState = ctx.Rule.State
	}
	if tkn != nil && je.tokenName == "" {
		je.tokenName = tkn.Name
		je.why = tkn.Why
	}

	// Structured-diagnostic fields (TabnasError.MarshalJSON). Grammar
	// errors recorded via ctx.ParseErr snapshot their context at the
	// moment the error is raised (rule.Process keeps running — pops,
	// pushes and the state flip all happen after — while TS throws
	// immediately at the raise site); everything else is captured live
	// here, where the context has not moved since the error.
	if ctx != nil && je.ruleStack == nil {
		snap := ctx.parseErrDiag
		if snap == nil {
			snap = captureDiag(ctx)
		}
		je.ruleStack = snap.ruleStack
		je.expected = snap.expected
		je.diagRule = snap.rule
	}

	return je
}

// diagSnapshot freezes the structured-diagnostic view of a Context: the
// rule stack root-first including the failing rule, the failing rule's
// name, and the sorted expected-token names for its current state.
type diagSnapshot struct {
	ruleStack []string
	rule      string
	expected  []string
}

// captureDiag builds a diagSnapshot from the context AS IT IS NOW. Called
// at the moment a grammar error is recorded (rule.go ctx.ParseErr sites),
// and by finishErr as the live fallback for every other error path. The
// rule stack is the names root-first including the failing rule; ctx.RS
// must be cut at ctx.RSI (pops move the index without truncating) and the
// NoRule sentinel is skipped by IDENTITY — its Name is "norule" and must
// not leak into the stack.
func captureDiag(ctx *Context) *diagSnapshot {
	snap := &diagSnapshot{}

	rsI := ctx.RSI
	if rsI > len(ctx.RS) {
		rsI = len(ctx.RS)
	}
	if rsI < 0 {
		rsI = 0
	}
	stack := make([]string, 0, rsI+1)
	for _, r := range ctx.RS[:rsI] {
		if r != nil && r != NoRule {
			stack = append(stack, r.Name)
		}
	}

	failing := ctx.Rule
	if failing == nil || failing == NoRule {
		failing = nil
	}
	if failing != nil {
		stack = append(stack, failing.Name)
		snap.rule = failing.Name

		// expected: token names some alternate of the failing rule
		// names at lookahead position 0 for the current state —
		// deduplicated and SORTED, so the emitted array is
		// byte-identical to the TS tcol-derived one. Names resolve
		// via the instance (ctx.Inst.TinName knows custom tokens;
		// the package-level tinName does not).
		if failing.Spec != nil {
			alts := failing.Spec.OpenAlts()
			if failing.State == CLOSE {
				alts = failing.Spec.CloseAlts()
			}
			seen := map[string]bool{}
			exp := []string{}
			for _, alt := range alts {
				if alt == nil {
					continue
				}
				s := ctx.altS(alt)
				if len(s) < 1 || len(s[0]) < 1 {
					continue
				}
				for _, tin := range s[0] {
					name := diagTinName(ctx, tin)
					if !seen[name] {
						seen[name] = true
						exp = append(exp, name)
					}
				}
			}
			sort.Strings(exp)
			snap.expected = exp
		}
	}
	snap.ruleStack = stack
	return snap
}

// diagTinName resolves a Tin to its name for the structured diagnostic.
// The instance mapping knows custom tokens; without an instance, fall
// back to the config's TinNames (the package-level tinName only knows
// the fixed punctuation tins and answers #UK for everything else).
func diagTinName(ctx *Context, tin Tin) string {
	if ctx.Inst != nil {
		return ctx.Inst.TinName(tin)
	}
	if ctx.Cfg != nil && ctx.Cfg.TinNames != nil {
		if name, ok := ctx.Cfg.TinNames[tin]; ok {
			return name
		}
	}
	return tinName(tin)
}

// makeError creates a TabnasError using this parser's error messages.
// Parser-level fields (ErrorMessages, Hints, ErrTag) take precedence over
// the config when set directly; normally they alias the config values.
// The optional `use` bag is a grammar's own error details; see
// makeTabnasError for why its keys override the built-in location keys.
// recordErr appends an error to the per-parse list and returns it, so
// call sites can wrap construction in place. The receiver is
// deliberately nil-safe: a Lex built without a Context (NewLex) has no
// list to record into, and recording must never be the thing that
// breaks a parse.
func (ctx *Context) recordErr(je *TabnasError) *TabnasError {
	if ctx != nil && je != nil {
		ctx.Errs = append(ctx.Errs, je)
	}
	return je
}

// ruleDoneSubsOf reads the post-process subscribers off the parsing
// instance. Nil-safe: Parser.Start and StartMeta parse without one.
func ruleDoneSubsOf(inst *Tabnas) []RuleDoneSub {
	if inst == nil {
		return nil
	}
	return inst.ruleDoneSubs
}

// ruleDoneAlt renders the pass's recorded outcome as the event
// payload. Nil when the rule state had no alternates at all; non-nil
// with only Err set when it had some and none matched — the same
// distinction TS draws between a null _dalt and a failed one.
//
// The group tags are split into a fresh slice on every call: AltSpec.G
// is live grammar configuration, and a consumer must not be able to
// reach it through the event.
func (ctx *Context) ruleDoneAlt() *RuleDoneAlt {
	if ctx == nil || !ctx.daltAny {
		return nil
	}
	if ctx.dalt == nil {
		return &RuleDoneAlt{Err: ctx.daltErr}
	}
	return &RuleDoneAlt{
		B:   ctx.dalt.B,
		G:   splitGroupTags(ctx.dalt.G),
		P:   ctx.dalt.P,
		R:   ctx.dalt.R,
		Err: ctx.daltErr,
	}
}

// alreadyRecorded reports whether an equivalent diagnostic — same code
// at the same offset — is already in the per-parse list. Recovery and
// the post-loop completeness checks can both observe one fault, and a
// consumer should see it once.
func (ctx *Context) alreadyRecorded(je *TabnasError) bool {
	if ctx == nil || je == nil {
		return false
	}
	for _, prev := range ctx.Errs {
		if prev != nil && prev.Code == je.Code && prev.Pos == je.Pos {
			return true
		}
	}
	return false
}

// makeErrorIn is makeError plus recording on the parse context.
func (p *Parser) makeErrorIn(
	ctx *Context, code, src, fullSource string, pos, row, col int,
	use ...map[string]any,
) *TabnasError {
	return ctx.recordErr(p.makeError(code, src, fullSource, pos, row, col, use...))
}

func (p *Parser) makeError(code, src, fullSource string, pos, row, col int, use ...map[string]any) *TabnasError {
	cfg := p.Config
	if cfg != nil && p.ErrorMessages != nil {
		// Parser-level ErrorMessages override the config's (they are aliased in
		// NewParser/Make, but a caller may reassign p.ErrorMessages directly).
		// Apply the override on a SHALLOW COPY of the config rather than writing
		// it back into the shared *LexConfig: makeError is the funnel for every
		// failing parse, and one instance's Config is shared across concurrent
		// parses (Tabnas.Parse reuses j.parser), so the in-place write was a
		// data race — two goroutines writing cfg.ErrorMessages at once — even
		// though it usually re-assigned the already-aliased map. makeTabnasError
		// only reads values out of cfg and never retains the pointer, so a
		// per-error copy is safe and behaviourally identical.
		c := *cfg
		c.ErrorMessages = p.ErrorMessages
		cfg = &c
	}
	je := makeTabnasError(code, src, fullSource, pos, row, col, cfg, use...)
	if p.Hints != nil {
		// Re-render with the parser-level hints, using the SAME bag
		// makeTabnasError injects from (errInjectRef) — this re-render
		// used to build a bag without {details}, so every error raised
		// through a real parse (this is the funnel Parser.startParse
		// uses) shipped the raw placeholder even though the direct
		// makeTabnasError path rendered it. Same fallback as hintFor:
		// a code with no hint of its own gets the "unknown" hint (TS
		// error.ts errdesc behavior).
		if hint := hintFor(p.Hints, code); hint != "" {
			je.Hint = StrInject(hint, errInjectRef(code, src, pos, row, col, use))
		}
	}
	if p.ErrTag != "" {
		je.tag = p.ErrTag
	}
	return je
}

// exactBaseFloat evaluates a base-prefixed integer's digit string to the
// float64 JS Number() would produce: the exact mathematical value, rounded
// once to nearest. Used only when the literal exceeds int64, so the hot
// in-range path stays on strconv.ParseInt.
//
// big.Float.SetInt carries enough precision to hold the integer exactly,
// so Float64() performs a single correctly-rounded conversion — no double
// rounding, and no clamping.
func exactBaseFloat(digits string, base int) (float64, bool) {
	if digits == "" {
		return 0, false
	}
	// Defensive: the lexer strips separators before calling, but big.Int
	// only accepts them with base 0.
	if strings.IndexByte(digits, '_') >= 0 {
		digits = strings.ReplaceAll(digits, "_", "")
	}
	bi, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return 0, false
	}
	f, _ := new(big.Float).SetInt(bi).Float64()
	return f, true
}

// parseNumericString converts a numeric string to float64.
// Handles standard decimals, hex (0x), octal (0o), binary (0b), and signs.
func parseNumericString(s string) float64 {
	if len(s) == 0 {
		return math.NaN()
	}

	// Handle sign prefix for special formats
	sign := 1.0
	ns := s
	if ns[0] == '-' {
		sign = -1.0
		ns = ns[1:]
	} else if ns[0] == '+' {
		sign = 1.0
		ns = ns[1:]
	}

	if len(ns) >= 2 {
		base := 0
		switch {
		case ns[0] == '0' && (ns[1] == 'x' || ns[1] == 'X'):
			base = 16
		case ns[0] == '0' && (ns[1] == 'o' || ns[1] == 'O'):
			base = 8
		case ns[0] == '0' && (ns[1] == 'b' || ns[1] == 'B'):
			base = 2
		}
		if base != 0 {
			digits := ns[2:]
			val, err := strconv.ParseInt(digits, base, 64)
			if err == nil {
				return sign * float64(val)
			}
			// Out of int64 range: JS Number("0x...") computes the exact
			// mathematical value of the digit string and rounds ONCE to
			// the nearest float64, so match that rather than failing.
			//
			// This is the base-prefixed sibling of the decimal ErrRange
			// tolerance below, and it must NOT be written as "keep what
			// ParseInt returned": ParseInt CLAMPS to MaxInt64, which is
			// a different number. 0x8000000000000000 survives the clamp
			// only by coincidence (float64(MaxInt64) and float64(2^63)
			// round to the same double); 0xFFFFFFFFFFFFFFFF would come
			// back 9.2e18 instead of 1.8446744073709552e19 — silently
			// off by 2x, and by an arbitrary factor further out.
			if f, ok := exactBaseFloat(digits, base); ok {
				return sign * f
			}
			// A real parse failure (bad digit for the base, empty body):
			// NaN drops the token to the lenient text matcher.
			return math.NaN()
		}
	}

	// Remove underscores, scanning first: most numbers have none and
	// skip the ReplaceAll machinery entirely.
	ns = s
	if strings.IndexByte(s, '_') >= 0 {
		ns = strings.ReplaceAll(s, "_", "")
	}

	val, err := strconv.ParseFloat(ns, 64)
	if err != nil {
		// TS coerces with unary +, which saturates rather than failing:
		// 1e999 -> Infinity, -1e999 -> -Infinity, 1e-999 -> 0. ParseFloat
		// reports ErrRange for those but still returns the saturated
		// value, so keep it. Any other error is a real parse failure and
		// still yields NaN, which drops the token to the text matcher.
		var ne *strconv.NumError
		if !errors.As(err, &ne) || ne.Err != strconv.ErrRange {
			return math.NaN()
		}
	}

	// NOTE: negative zero is preserved. An earlier version normalized
	// -0 to 0 here, silently diverging from both the TS runtime (whose
	// unary + preserves -0) and encoding/json — caught by the
	// cross-runtime token parity harness (ci/parity).
	return val
}

// maxInt is the largest value of the platform int.
const maxInt = int(^uint(0) >> 1)

// utf16Len counts s in UTF-16 code units — the unit ts/src/parser.ts gets
// for free from `lex.src.length`.
//
// The rule-iteration budget scales with source length, and the two ports
// used different units for it: UTF-16 there, BYTES here. Same document,
// different budget, and reachable: a grammar that pushes N children
// before consuming a 30-emoji source parsed for every N up to 2879 here
// and only to 1439 there. Bytes are the wrong unit twice over — they
// agree with UTF-16 only for ASCII, and they are not what any other
// length in this engine counts. Audit item P7.
//
// One pass, no decoding: a byte that is not a UTF-8 continuation byte
// starts a rune, and a rune whose lead byte is >= 0xF0 is outside the
// BMP and so costs TWO UTF-16 units. Exact for well-formed input and
// stable for malformed input, which this engine passes through rather
// than rejecting.
func utf16Len(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if b := s[i]; b&0xC0 != 0x80 {
			n++
			if 0xF0 <= b {
				n++
			}
		}
	}
	return n
}

// satMul multiplies two non-negative ints, saturating at maxInt rather
// than wrapping. Used for the rule-iteration budget, where a wrapped
// (negative) product would be silently converted into the smallest legal
// budget by the floor that follows it. See ts/src/parser.ts, which needs
// no equivalent: its arithmetic is float64 and saturates at Infinity.
func satMul(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > maxInt/b {
		return maxInt
	}
	return a * b
}
