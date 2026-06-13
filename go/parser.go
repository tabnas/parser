package tabnas

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Context holds the parse state, matching the TypeScript Context type.
type Context struct {
	UI int // Unique rule ID counter (TS: uI)

	// Generalized lookahead buffer. T[i] is the token at position i,
	// or NoToken if that slot has not yet been fetched. This supersedes
	// the legacy T0 / T1 two-slot fields, which are kept in sync for
	// backward compatibility (plugins / grammars / debug.go that read
	// ctx.T0 and ctx.T1 directly continue to work unchanged).
	T []*Token

	T0 *Token // Alias of T[0] (legacy). Kept in sync with T[0].
	T1 *Token // Alias of T[1] (legacy). Kept in sync with T[1].
	V1 *Token // Previous token (TS: v1)
	V2 *Token // Previous previous token (TS: v2)

	// Consumed-token rewind history. V holds the tokens consumed (not
	// backtracked) so far, bounded by cfg.RewindHistory (a ring buffer
	// trimmed from the front). VAbs is the absolute count of consumed
	// tokens — used as the Mark() value, decoupled from len(V) so the
	// ring-buffer cap can evict old tokens without invalidating
	// outstanding marks. Mirrors TS ctx.v / ctx.vAbs.
	V    []*Token
	VAbs int
	Lex  *Lex // Attached by parser.start(); used by Rewind to re-feed tokens.

	RS       []*Rule           // Rule stack (TS: rs)
	RSI      int               // Rule stack index (TS: rsI)
	RSM      map[string]*RuleSpec // Rule spec map (TS: rsm)
	KI       int               // Iteration counter (TS: kI)
	Rule     *Rule             // Current parsing rule (TS: rule)
	Meta     map[string]any    // Parse metadata (TS: meta)
	LexSubs  []LexSub          // Lex event subscribers (TS: sub.lex)
	RuleSubs []RuleSub         // Rule event subscribers (TS: sub.rule)
	ParseErr *Token            // Error token, halts parse

	// Fields matching TS Context:
	Opts     *Options          // Tabnas instance options (TS: opts)
	Cfg      *LexConfig        // Tabnas instance config (TS: cfg)
	Src      string            // Source text being parsed (TS: src)
	Inst     *Tabnas           // Current Tabnas instance (TS: inst)
	U        map[string]any    // Custom plugin data bag (TS: u)
	Root     *Rule             // Root rule (TS: root)
	TC       int               // Token count (TS: tC)
	F        func(any) string  // Format value as string (TS: F)
	Log      func(...any)      // Debug logger (TS: log)
	NOTOKEN  *Token            // Sentinel no-token (TS: NOTOKEN)
	NORULE   *Rule             // Sentinel no-rule (TS: NORULE)
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

// Parser orchestrates the parsing process.
type Parser struct {
	Config        *LexConfig
	RSM           map[string]*RuleSpec
	MaxMul        int               // Max rule occurrence multiplier. Default: 3.
	ErrorMessages map[string]string  // Custom error message templates.
	Hints         map[string]string  // Explanatory hints per error code.
	ErrTag        string             // Custom error tag (TS: errmsg.name). Default: "tabnas".
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
		UI:       0,
		T:        []*Token{NoToken, NoToken},
		T0:       NoToken,
		T1:       NoToken,
		V1:       NoToken,
		V2:       NoToken,
		RS:       make([]*Rule, len(src)*4+100),
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
			return nil, p.finishErr(p.makeError("unexpected", tkn.Src, src, tkn.SI, tkn.RI, tkn.CI), ctx, meta, tkn)
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
func (p *Parser) makeError(code, src, fullSource string, pos, row, col int) *TabnasError {
	cfg := p.Config
	if cfg != nil && p.ErrorMessages != nil {
		// Aliased in NewParser/Make; this guards direct field assignment.
		cfg.ErrorMessages = p.ErrorMessages
	}
	je := makeTabnasError(code, src, fullSource, pos, row, col, cfg)
	if p.Hints != nil {
		if hint, ok := p.Hints[code]; ok {
			je.Hint = StrInject(hint, map[string]any{
				"code": code, "src": src, "pos": pos, "row": row, "col": col,
			})
		}
	}
	if p.ErrTag != "" {
		je.tag = p.ErrTag
	}
	return je
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
		switch {
		case ns[0] == '0' && (ns[1] == 'x' || ns[1] == 'X'):
			val, err := strconv.ParseInt(ns[2:], 16, 64)
			if err != nil {
				return math.NaN()
			}
			return sign * float64(val)
		case ns[0] == '0' && (ns[1] == 'o' || ns[1] == 'O'):
			val, err := strconv.ParseInt(ns[2:], 8, 64)
			if err != nil {
				return math.NaN()
			}
			return sign * float64(val)
		case ns[0] == '0' && (ns[1] == 'b' || ns[1] == 'B'):
			val, err := strconv.ParseInt(ns[2:], 2, 64)
			if err != nil {
				return math.NaN()
			}
			return sign * float64(val)
		}
	}

	// Remove underscores if present
	ns = strings.ReplaceAll(s, "_", "")

	val, err := strconv.ParseFloat(ns, 64)
	if err != nil {
		return math.NaN()
	}

	// Normalize -0 to 0
	if val == 0 {
		return 0
	}

	return val
}
