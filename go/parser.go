// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"errors"
	"fmt"
	"math"
	"math/big"
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
	ParseErr *Token               // Error token; when set, halts the parse.

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
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = p.makeError("internal", fmt.Sprint(r), src, 0, 1, 1)
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
		Cfg:      p.Config,
		Src:      src,
		Inst:     inst,
		U:        make(map[string]any),
		TC:       0,
		NOTOKEN:  NoToken,
		NORULE:   NoRule,
		F:        func(v any) string { return Str(v, 44) },
	}

	// Late-bind token-set positions only when this instance actually
	// overrides a set — the default instance pays nothing.
	ctx.tokenSetDyn = inst != nil && len(inst.customTokenSets) > 0

	lex.Ctx = ctx
	ctx.Lex = lex

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
	maxmul := p.MaxMul
	if maxmul <= 0 {
		maxmul = 3
	}
	maxr := 2 * len(p.RSM) * len(src) * 2 * maxmul
	if maxr < 100 {
		maxr = 100
	}

	kI := 0
	for rule != NoRule && kI < maxr {
		ctx.KI = kI
		ctx.Rule = rule

		// Fire rule subscribers BEFORE process (matching TS).
		if len(ctx.RuleSubs) > 0 {
			for _, sub := range ctx.RuleSubs {
				sub(rule, ctx)
			}
		}

		rule = rule.Process(ctx, lex)

		// Check for parse error from alt.E or actions.
		if ctx.ParseErr != nil {
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
			je := p.makeError(code, tkn.Src, src, tkn.SI, tkn.RI, tkn.CI, tkn.Use)
			return nil, p.finishErr(je, ctx, meta, tkn)
		}

		kI++
	}

	// Check for lexer errors (unterminated strings, comments, etc.)
	if lex.Err != nil {
		return nil, p.finishErr(lex.Err, ctx, meta, nil)
	}

	// Check for unconsumed tokens (syntax error) - explicit trailing content check.
	// First check tokens already in the lookahead buffer.
	if ctx.T0 != nil && !ctx.T0.IsNoToken() && ctx.T0.Tin != TinZZ {
		// Prefer lex errors over generic unexpected for unconsumed tokens too.
		if lex.Err != nil {
			return nil, p.finishErr(lex.Err, ctx, meta, nil)
		}
		return nil, p.finishErr(p.makeError("unexpected", ctx.T0.Src, src, ctx.T0.SI, ctx.T0.RI, ctx.T0.CI), ctx, meta, ctx.T0)
	}
	// Also explicitly ask lexer for more (matching TS parser.ts:187-189).
	endTkn := lex.Next(rule)
	if endTkn.Tin != TinZZ {
		if lex.Err != nil {
			return nil, p.finishErr(lex.Err, ctx, meta, nil)
		}
		return nil, p.finishErr(p.makeError("unexpected", endTkn.Src, src, endTkn.SI, endTkn.RI, endTkn.CI), ctx, meta, endTkn)
	}
	// Check lexer errors from that final Next() call.
	if lex.Err != nil {
		return nil, p.finishErr(lex.Err, ctx, meta, nil)
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
				return nil, p.finishErr(p.makeError("unexpected", "", src, 0, 1, 1), ctx, meta, nil)
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
	return je
}

// makeError creates a TabnasError using this parser's error messages.
// Parser-level fields (ErrorMessages, Hints, ErrTag) take precedence over
// the config when set directly; normally they alias the config values.
// The optional `use` bag is a grammar's own error details; see
// makeTabnasError for why its keys override the built-in location keys.
func (p *Parser) makeError(code, src, fullSource string, pos, row, col int, use ...map[string]any) *TabnasError {
	cfg := p.Config
	if cfg != nil && p.ErrorMessages != nil {
		// Aliased in NewParser/Make; this guards direct field assignment.
		cfg.ErrorMessages = p.ErrorMessages
	}
	je := makeTabnasError(code, src, fullSource, pos, row, col, cfg, use...)
	if p.Hints != nil {
		if hint, ok := p.Hints[code]; ok {
			ref := map[string]any{
				"code": code, "src": src, "pos": pos, "row": row, "col": col,
			}
			for _, u := range use {
				for k, v := range u {
					ref[k] = v
				}
			}
			je.Hint = StrInject(hint, ref)
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
