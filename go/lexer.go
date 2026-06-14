package tabnas

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// MatchValueEntry holds a resolved match.value matcher entry.
// Name carries the user-supplied name so the slice can be sorted
// deterministically at configure time (independent of the map iteration
// order that built it).
type MatchValueEntry struct {
	Name  string
	Match *regexp.Regexp
	Val   func([]string) any
	Fn    LexMatcher // Function-form matcher (TS LexMatcher branch); takes precedence over Match.
}

// ValueDefEntry is a name-tagged wrapper around *ValueDef used to sort
// regex-based value definitions at configure time so iteration during
// lexing is deterministic (mirrors TS cfg.value.defre being a sorted array).
type ValueDefEntry struct {
	Name string
	Def  *ValueDef
}

// MatchTokenEntry pairs a Tin with its matcher — regexp or function form
// (TS match.token: RegExp | LexMatcher) — pre-sorted by Tin at configure
// time so MatchTokens iteration during lexing is deterministic.
type MatchTokenEntry struct {
	Tin   Tin
	Match *regexp.Regexp
	Fn    LexMatcher // Function-form matcher; takes precedence over Match.
}

// Lex is the lexer that produces tokens from source text.
type Lex struct {
	Src    string
	Ctx    *Context // Parse context (includes Ctx.Rule for context-sensitive lexing)
	pnt    Point
	end    *Token   // End-of-source token (cached)
	tokens []*Token // Lookahead token queue
	Config *LexConfig
	Err    error // First error encountered during lexing
}

// LexConfig holds lexer configuration.
type LexConfig struct {
	// Lex enable/disable flags (matching TS options.*.lex)
	FixedLex   bool // Enable fixed token recognition. Default: true.
	SpaceLex   bool // Enable space lexing. Default: true.
	LineLex    bool // Enable line lexing. Default: true.
	TextLex    bool // Enable text matching. Default: true.
	NumberLex  bool // Enable number matching. Default: true.
	CommentLex bool // Enable comment matching. Default: true.
	StringLex  bool // Enable string matching. Default: true.
	ValueLex   bool // Enable value keyword matching. Default: true.

	StringChars        map[rune]bool // Quote characters
	MultiChars         map[rune]bool // Multiline quote characters
	EscapeChar         rune
	EscapeMap          map[string]string // Custom escape mappings, e.g. {"n": "\n"}.
	EscapeRemoved      map[string]bool   // Built-in escapes removed via {"v": ""}; consulted before the hardcoded switch.
	EscapeStrict       bool              // Disable the non-standard \xHH and \u{...} structural escapes.
	RewindHistory      int               // Max consumed tokens retained for ctx.Rewind. <=0 means unbounded. Default 64.
	SpaceChars         map[rune]bool
	LineChars          map[rune]bool
	RowChars           map[rune]bool
	CommentLine        []string    // Line comment starters: "#", "//"
	CommentBlock       [][2]string // Block comment: [start, end] pairs
	NumberHex          bool
	NumberOct          bool
	NumberBin          bool
	NumberSep          rune // Separator char (underscore)
	AllowUnknownEscape bool
	StringAbandon      bool            // On string error, return nil instead of bad token.
	StringReplace      map[rune]string // Character replacements during string scanning.

	// Value definitions: keyword → value (e.g. "true" → true)
	// If nil, uses built-in defaults (true, false, null).
	ValueDef map[string]any

	// ValueDefRe holds regex-processed value definitions (TS: cfg.value.defre).
	// Sorted by name at configure time for deterministic iteration.
	ValueDefRe []*ValueDefEntry

	// Match options (TS: cfg.match)
	MatchLex          bool                   // Enable custom matching. Default: false.
	MatchTokens       map[Tin]*regexp.Regexp // Custom token tin → regexp (storage).
	MatchTokenFns     map[Tin]LexMatcher     // Custom token tin → function matcher (storage).
	MatchTokensSorted []*MatchTokenEntry     // Sorted-by-tin view for deterministic iteration.
	MatchValues       []*MatchValueEntry     // Custom value matchers, sorted by name.

	// Number options
	NumberExclude func(string) bool // Exclude certain number-like strings.

	// Line options
	LineSingle bool // Generate separate tokens per newline.

	// Text options
	TextModify []ValModifier // Pipeline of text value modifiers.

	// Comment options (per-def eatline stored on CommentDef)
	CommentLineEatLine  map[string]bool // Line comment starter → eatline flag.
	CommentBlockEatLine map[string]bool // Block comment start → eatline flag.

	// Comment suffix terminators — optional, non-consuming.
	// Per start-marker, a list of string prefixes that terminate the
	// comment body when encountered. The suffix remains in the input.
	CommentLineSuffixes  map[string][]string
	CommentBlockSuffixes map[string][]string

	// LexMatcher-form suffix terminators. Invoked at each candidate
	// position inside the comment body; a non-nil returned token signals
	// termination at that offset. The returned token is discarded.
	CommentLineSuffixFuncs  map[string]LexMatcher
	CommentBlockSuffixFuncs map[string]LexMatcher

	// Color carries the resolved ANSI escape codes for error formatting.
	// Populated from Options.Color by buildConfig.
	Color ColorConfig

	// ErrorMessages holds error message templates by code (TS: cfg.error).
	// Options.Error entries are merged over the package defaults by
	// buildConfig. Templates may use {key} placeholders (code, src, ...).
	ErrorMessages map[string]string

	// Hints holds explanatory hint templates by code (TS: cfg.hint).
	// Options.Hint entries are merged over the package defaults.
	Hints map[string]string

	// ErrTag is the error header name (TS: errmsg.name). "" → "tabnas".
	ErrTag string

	// ErrSuffix mirrors TS errmsg.suffix: nil/true renders the standard
	// internal suffix block, false suppresses it, string/func replace it.
	ErrSuffix any

	// ErrLink is an optional "see also" line (TS: errmsg.link) rendered
	// inside the standard suffix block.
	ErrLink string

	// Tag is the instance tag (TS: options.tag), shown in the internal
	// error suffix.
	Tag string

	// Lazily built scan specs (see scan.go). Cleared whenever SetOptions
	// replaces the config contents.
	spaceSpec   *ScanSpec
	lineSpec    *ScanSpec
	stringSpecs map[rune]*ScanSpec

	// Map/List options
	MapExtend    bool         // Deep-merge duplicate keys. Default: true.
	MapMerge     MapMergeFunc // Custom merge function for duplicate keys.
	MapChild     bool         // Parse bare colon in maps as child$ key. Default: false.
	ListProperty bool         // Allow named properties in arrays. Default: true.
	ListPair     bool         // Push pairs as object elements in arrays. Default: false.

	// Safe options
	SafeKey bool // Prevent __proto__ keys. Default: true.

	// Rule options
	FinishRule bool   // Auto-close unclosed structures at EOF
	RuleStart  string // Starting rule name. Default: "val".

	// EnderChars lists additional characters that end text and number tokens.
	EnderChars map[rune]bool

	// Per-instance fixed token map (cloned from global FixedTokens).
	// Plugins can add custom fixed tokens here. Supports multi-char keys.
	FixedTokens map[string]Tin

	// FixedSorted is the list of fixed token strings sorted by length (longest first).
	// Rebuilt by SortFixedTokens() after adding custom tokens.
	FixedSorted []string

	// Custom token names: Tin → name for plugin-defined tokens.
	TinNames map[Tin]string

	// Custom lexer matchers added by plugins, sorted by priority.
	CustomMatchers []*MatcherEntry

	// TextInfo wraps string/text output values in Text structs.
	TextInfo bool

	// ListRef wraps list output values in ListRef structs.
	ListRef bool

	// ListChild enables bare colon (:value) syntax in lists to set a child value.
	ListChild bool

	// MapRef wraps map output values in MapRef structs.
	MapRef bool

	// InfoMarker is the key name to protect from user data when info options
	// are enabled. Keys matching this value are dropped during parsing.
	// Matches TS cfg.info.marker. Empty string means no protection.
	InfoMarker string

	// IgnoreSet is the per-instance set of token Tins to skip during lexing.
	// Matches TS cfg.tokenSetTins.IGNORE. Plugins can customize this per-instance.
	IgnoreSet map[Tin]bool

	// ValSet is the per-instance VAL token set (text, number, string, value).
	// Matches TS cfg.tokenSet.VAL. Plugins can customize this per-instance.
	ValSet []Tin

	// KeySet is the per-instance KEY token set (text, number, string, value).
	// Matches TS cfg.tokenSet.KEY. Plugins can customize this per-instance.
	KeySet []Tin

	// ParsePrepare hooks called before parsing begins.
	ParsePrepare []func(ctx *Context)

	// ResultFail is a list of values that are treated as parse failures.
	ResultFail []any

	// LexCheck callbacks allow plugins to intercept and override matchers.
	// Each returns nil to continue normal matching, or a LexCheckResult to short-circuit.
	FixedCheck   LexCheck
	SpaceCheck   LexCheck
	LineCheck    LexCheck
	StringCheck  LexCheck
	CommentCheck LexCheck
	NumberCheck  LexCheck
	TextCheck    LexCheck
	MatchCheck   LexCheck
}

// ColorConfig is the resolved ANSI-escape palette used by the error
// formatter. Active false means all codes are emitted as empty strings.
type ColorConfig struct {
	Active bool
	Reset  string
	Hi     string
	Lo     string
	Line   string
}

// Codes returns the four escape sequences the formatter needs as plain
// strings. When Active is false they are all empty, so the formatter
// can append them unconditionally.
func (c ColorConfig) Codes() (hi, lo, line, reset string) {
	if !c.Active {
		return "", "", "", ""
	}
	return c.Hi, c.Lo, c.Line, c.Reset
}

// LexCheck is a function that can intercept a matcher before it runs.
// Return nil to continue normal matching, or a LexCheckResult to override.
type LexCheck func(lex *Lex) *LexCheckResult

// LexCheckResult controls matcher behavior from a LexCheck callback.
type LexCheckResult struct {
	Done  bool   // If true, use Token as the match result (even if nil).
	Token *Token // The token to return (nil means "no match").
}

// DefaultLexConfig returns the default lexer configuration matching tabnas defaults.
func DefaultLexConfig() *LexConfig {
	return &LexConfig{
		FixedLex:   true,
		SpaceLex:   true,
		LineLex:    true,
		TextLex:    true,
		NumberLex:  true,
		CommentLex: true,
		StringLex:  true,
		ValueLex:   true,

		StringChars:        map[rune]bool{'\'': true, '"': true, '`': true},
		MultiChars:         map[rune]bool{'`': true},
		EscapeChar:         '\\',
		SpaceChars:         map[rune]bool{' ': true, '\t': true},
		LineChars:          map[rune]bool{'\r': true, '\n': true},
		RowChars:           map[rune]bool{'\n': true},
		CommentLine:        []string{"#", "//"},
		CommentBlock:       [][2]string{{"/*", "*/"}},
		NumberHex:          true,
		NumberOct:          true,
		NumberBin:          true,
		NumberSep:          '_',
		AllowUnknownEscape: true,

		MapExtend:    true,
		ListProperty: true,
		SafeKey:      true,

		FinishRule: true,
		RuleStart:  "val",

		IgnoreSet: map[Tin]bool{TinSP: true, TinLN: true, TinCM: true},
		ValSet:    []Tin{TinTX, TinNR, TinST, TinVL},
		KeySet:    []Tin{TinTX, TinNR, TinST, TinVL},

		FixedTokens: map[string]Tin{
			"{": TinOB, "}": TinCB,
			"[": TinOS, "]": TinCS,
			":": TinCL, ",": TinCA,
		},
		FixedSorted: []string{"{", "}", "[", "]", ":", ","},
	}
}

// SortFixedTokens rebuilds FixedSorted from FixedTokens, sorted by length descending.
// Call this after adding multi-char fixed tokens to ensure longest-match-first behavior.
func (cfg *LexConfig) SortFixedTokens() {
	sorted := make([]string, 0, len(cfg.FixedTokens))
	for k := range cfg.FixedTokens {
		sorted = append(sorted, k)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i]) != len(sorted[j]) {
			return len(sorted[i]) > len(sorted[j]) // longer first
		}
		return sorted[i] < sorted[j] // stable tie-break
	})
	cfg.FixedSorted = sorted
}

// RebuildMatchTokensSorted projects MatchTokens and MatchTokenFns into
// MatchTokensSorted ([]*MatchTokenEntry) in ascending Tin order. Call this
// after any mutation of either map so the lexer can iterate deterministically
// without sorting during parse. A function matcher takes precedence over a
// regexp registered for the same tin.
func (cfg *LexConfig) RebuildMatchTokensSorted() {
	sorted := make([]*MatchTokenEntry, 0, len(cfg.MatchTokens)+len(cfg.MatchTokenFns))
	for tin, re := range cfg.MatchTokens {
		if cfg.MatchTokenFns != nil && cfg.MatchTokenFns[tin] != nil {
			continue
		}
		sorted = append(sorted, &MatchTokenEntry{Tin: tin, Match: re})
	}
	for tin, fn := range cfg.MatchTokenFns {
		sorted = append(sorted, &MatchTokenEntry{Tin: tin, Fn: fn})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Tin < sorted[j].Tin
	})
	cfg.MatchTokensSorted = sorted
}

// NewLex creates a new lexer for the given source.
func NewLex(src string, cfg *LexConfig) *Lex {
	return &Lex{
		Src:    src,
		pnt:    Point{Len: len(src), SI: 0, RI: 1, CI: 1},
		Config: cfg,
	}
}

// Cursor returns a pointer to the lexer's current position.
// Custom matchers use this to read and advance the position.
func (l *Lex) Cursor() *Point {
	return &l.pnt
}

// Token creates a new token at the current point.
func (l *Lex) Token(name string, tin Tin, val any, src string) *Token {
	return MakeToken(name, tin, val, src, l.pnt)
}

// Fwd returns a forward-looking substring from the current position.
// maxlen limits the length of the returned string.
// Matches TS lex.fwd.
func (l *Lex) Fwd(maxlen int) string {
	si := l.pnt.SI
	end := si + maxlen
	if end > l.pnt.Len {
		end = l.pnt.Len
	}
	if si >= end {
		return ""
	}
	return l.Src[si:end]
}

// Bad creates an error token at the current position.
// Matches TS lex.bad(why, pstart, pend).
func (l *Lex) Bad(why string) *Token {
	tkn := MakeToken("#BD", TinBD, nil, "", l.pnt)
	tkn.Why = why
	tkn.Err = why
	return tkn
}

// attachErrContext records the active rule and token on an error for the
// standard "--internal: ..." suffix block (TS errdesc suffix).
func (l *Lex) attachErrContext(je *TabnasError, r *Rule, tokenName, why string) {
	if r != nil {
		je.ruleName = r.Name
		je.ruleState = r.State
	}
	je.tokenName = tokenName
	je.why = why
}

// Next returns the next non-IGNORE token, passing the current parsing rule
// to custom matchers for context-sensitive lexing.
// On error (unterminated string, unterminated comment, unexpected character),
// the error is stored in l.Err and a ZZ (end) token is returned to allow
// the parser to wind down gracefully.
func (l *Lex) Next(rule ...*Rule) *Token {
	var r *Rule
	if len(rule) > 0 {
		r = rule[0]
	}
	if l.Ctx != nil && r != nil {
		l.Ctx.Rule = r
	}
	for {
		// If an error has already occurred, return end-of-source to stop parsing
		if l.Err != nil {
			return &Token{Name: "#ZZ", Tin: TinZZ, Val: Undefined, SI: l.pnt.SI, RI: l.pnt.RI, CI: l.pnt.CI}
		}

		tkn := l.nextRaw(r)
		if tkn == nil {
			src := ""
			if l.pnt.SI < len(l.Src) {
				src = string(l.Src[l.pnt.SI])
			}
			je := makeTabnasError("unexpected", src, l.Src, l.pnt.SI, l.pnt.RI, l.pnt.CI, l.Config)
			l.attachErrContext(je, r, "#UK", "")
			l.Err = je
			return &Token{Name: "#ZZ", Tin: TinZZ, Val: Undefined, SI: l.pnt.SI, RI: l.pnt.RI, CI: l.pnt.CI}
		}
		// Bad token → store error and return end-of-source
		if tkn.Tin == TinBD {
			je := makeTabnasError(tkn.Why, tkn.Src, l.Src, tkn.SI, tkn.RI, tkn.CI, l.Config)
			l.attachErrContext(je, r, tkn.Name, tkn.Why)
			l.Err = je
			return &Token{Name: "#ZZ", Tin: TinZZ, Val: Undefined, SI: tkn.SI, RI: tkn.RI, CI: tkn.CI}
		}
		// Skip IGNORE tokens (per-instance set, matching TS cfg.tokenSetTins.IGNORE)
		if l.Config.IgnoreSet[tkn.Tin] {
			continue
		}
		return tkn
	}
}

// guardedMatch wraps a matcher body in the standard entry guards
// (TS: guardedMatcher): skip when the matcher's lex flag is off, and
// consult the optional check hook, which may short-circuit by returning
// {Done: true, Token: t} — a nil hook token suppresses the matcher.
func (l *Lex) guardedMatch(enabled bool, check LexCheck, body func() *Token) *Token {
	if !enabled {
		return nil
	}
	if check != nil {
		if cr := check(l); cr != nil && cr.Done {
			return cr.Token
		}
	}
	return body()
}

// nextRaw returns the next raw token (including IGNORE tokens).
// The rule parameter is passed to custom matchers for context-sensitive lexing.
func (l *Lex) nextRaw(rule *Rule) *Token {
	// Return cached end token
	if l.end != nil {
		return l.end
	}

	// Return queued lookahead tokens
	if len(l.tokens) > 0 {
		tkn := l.tokens[0]
		l.tokens = l.tokens[1:]
		return tkn
	}

	// End of source
	if l.pnt.SI >= l.pnt.Len {
		l.end = l.Token("#ZZ", TinZZ, Undefined, "")
		return l.end
	}

	// Try matchers in order (matching TS lex.match ordering):
	// match(1e6), fixed(2e6), space(3e6), line(4e6), string(5e6), comment(6e6), number(7e6), text(8e6)
	// Custom matchers are interleaved by priority (already sorted).

	cI := 0 // index into CustomMatchers

	// Run custom matchers with priority before match (< 1e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 1000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	// Match matcher (priority 1e6) — regexp-based token and value matching.
	if tkn := l.guardedMatch(l.Config.MatchLex, l.Config.MatchCheck,
		func() *Token { return l.matchMatch(rule) }); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before fixed (< 2e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 2000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.FixedLex, l.Config.FixedCheck, l.matchFixed); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before space (< 3e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 3000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.SpaceLex, l.Config.SpaceCheck, l.matchSpace); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before line (< 4e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 4000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.LineLex, l.Config.LineCheck, l.matchLine); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before string (< 5e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 5000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.StringLex, l.Config.StringCheck, l.matchString); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before comment (< 6e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 6000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.CommentLex, l.Config.CommentCheck, l.matchComment); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before number (< 7e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 7000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.NumberLex, l.Config.NumberCheck, l.matchNumber); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before text (< 8e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 8000000 {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.TextLex || l.Config.ValueLex,
		l.Config.TextCheck, l.matchText); tkn != nil {
		return tkn
	}

	// Run remaining custom matchers (priority >= 8e6).
	for cI < len(l.Config.CustomMatchers) {
		if tkn := l.Config.CustomMatchers[cI].Match(l, rule); tkn != nil {
			return tkn
		}
		cI++
	}

	// No matcher matched
	return nil
}

// matchMatch implements the match matcher (TS: makeMatchMatcher at priority 1e6).
// Handles both match.value (regexp → #VL) and match.token (regexp → custom token).
func (l *Lex) matchMatch(rule *Rule) *Token {
	if l.pnt.SI >= l.pnt.Len {
		return nil
	}

	fwd := l.Src[l.pnt.SI:]

	// Match values first (TS: valueMatchers loop).
	for _, vm := range l.Config.MatchValues {
		// Function-form matcher (TS: LexMatcher branch).
		if vm.Fn != nil {
			if tkn := vm.Fn(l, rule); tkn != nil {
				return tkn
			}
			continue
		}
		if vm.Match != nil {
			res := vm.Match.FindStringSubmatch(fwd)
			if res != nil && len(res[0]) > 0 {
				msrc := res[0]
				mlen := len(msrc)
				var val any
				if vm.Val != nil {
					val = vm.Val(res)
				} else {
					val = msrc
				}
				tkn := l.Token("#VL", TinVL, val, msrc)
				l.pnt.SI += mlen
				l.pnt.CI += utf8.RuneCountInString(msrc)
				return tkn
			}
		}
	}

	// Match tokens (TS: tokenMatchers loop).
	// Only match if the token type is expected in the current rule position.
	if rule != nil && rule.Spec != nil {
		var alts []*AltSpec
		if rule.State == OPEN {
			alts = rule.Spec.open
		} else {
			alts = rule.Spec.close
		}

		for _, mt := range l.Config.MatchTokensSorted {
			tin, re := mt.Tin, mt.Match
			if re == nil && mt.Fn == nil {
				continue
			}
			// Check if this Tin is expected at position 0 in any alt.
			expected := false
			for _, alt := range alts {
				if len(alt.S) > 0 && tinMatch(tin, alt.S[0]) {
					expected = true
					break
				}
			}
			if !expected {
				continue
			}

			// Function-form matcher (TS: LexMatcher branch).
			if mt.Fn != nil {
				if tkn := mt.Fn(l, rule); tkn != nil {
					return tkn
				}
				continue
			}

			res := re.FindString(fwd)
			if res != "" {
				name := l.tinNameFor(tin)
				tkn := l.Token(name, tin, res, res)
				l.pnt.SI += len(res)
				l.pnt.CI += utf8.RuneCountInString(res)
				return tkn
			}
		}
	}

	return nil
}

func (l *Lex) bad(why string, pstart, pend int) *Token {
	src := ""
	if pstart >= 0 && pstart < len(l.Src) && pend <= len(l.Src) {
		src = l.Src[pstart:pend]
	} else if l.pnt.SI < len(l.Src) {
		src = string(l.Src[l.pnt.SI])
	}
	tkn := l.Token("#BD", TinBD, nil, src)
	tkn.Why = why
	return tkn
}

// matchFixed matches fixed tokens, including multi-character tokens.
// Tokens are tried longest-first to ensure greedy matching (e.g. "=>" before "=").
func (l *Lex) matchFixed() *Token {
	if l.pnt.SI >= l.pnt.Len {
		return nil
	}
	remaining := l.Src[l.pnt.SI:]

	// Use sorted list for longest-match-first. Fall back to single-char lookup
	// if no sorted list (e.g. standalone lexer without Tabnas).
	if len(l.Config.FixedSorted) > 0 {
		for _, fs := range l.Config.FixedSorted {
			if strings.HasPrefix(remaining, fs) {
				tin := l.Config.FixedTokens[fs]
				tkn := l.Token(l.tinNameFor(tin), tin, nil, fs)
				l.pnt.SI += len(fs)
				l.pnt.CI += utf8.RuneCountInString(fs)
				return tkn
			}
		}
		return nil
	}

	// Fallback: single-char lookup.
	src := string(l.Src[l.pnt.SI])
	tin, ok := l.Config.FixedTokens[src]
	if !ok {
		return nil
	}
	tkn := l.Token(l.tinNameFor(tin), tin, nil, src)
	l.pnt.SI++
	l.pnt.CI++
	return tkn
}

// matchSpace matches space and tab characters.
func (l *Lex) matchSpace() *Token {
	var out ScanOut
	if !Scan(l.Src, l.pnt.SI, l.pnt.RI, l.pnt.CI, l.Config.spaceRunSpec(), &out) {
		return nil
	}
	src := l.Src[l.pnt.SI:out.SI]
	tkn := l.Token("#SP", TinSP, nil, src)
	l.pnt.SI = out.SI
	l.pnt.CI = out.CI
	return tkn
}

// matchLine matches line ending characters (\r, \n).
// When LineSingle is true, generates separate tokens for each newline sequence.
func (l *Lex) matchLine() *Token {
	sI := l.pnt.SI
	if sI >= l.pnt.Len {
		return nil
	}
	ch, chSize := utf8.DecodeRuneInString(l.Src[sI:])
	if !l.Config.LineChars[ch] {
		return nil
	}

	rI := l.pnt.RI

	if l.Config.LineSingle {
		// Single mode: consume one newline sequence (\r\n or \n or \r)
		sI += chSize
		if l.Config.RowChars[ch] {
			rI++
		}
		// Handle \r\n as a single sequence
		if ch == '\r' && sI < l.pnt.Len && l.Src[sI] == '\n' {
			if l.Config.RowChars['\n'] {
				// \r\n counts as one row
			}
			sI++
		}
		src := l.Src[l.pnt.SI:sI]
		tkn := l.Token("#LN", TinLN, nil, src)
		l.pnt.SI = sI
		l.pnt.RI = rI
		l.pnt.CI = 1
		return tkn
	}

	// Default: consume all consecutive line characters into one token
	var out ScanOut
	Scan(l.Src, sI, rI, l.pnt.CI, l.Config.lineRunSpec(), &out)
	src := l.Src[l.pnt.SI:out.SI]
	tkn := l.Token("#LN", TinLN, nil, src)
	l.pnt.SI = out.SI
	l.pnt.RI = out.RI
	l.pnt.CI = 1
	return tkn
}

// matchComment matches line comments (# //) and block comments (/* */).
func (l *Lex) matchComment() *Token {
	fwd := l.Src[l.pnt.SI:]

	// Line comments
	for _, start := range l.Config.CommentLine {
		if strings.HasPrefix(fwd, start) {
			suffixes := l.Config.CommentLineSuffixes[start]
			suffixFn := l.Config.CommentLineSuffixFuncs[start]
			fI := len(start)
			cI := l.pnt.CI + len(start)
			suffixLen := 0
			for fI < len(fwd) {
				r, rsize := utf8.DecodeRuneInString(fwd[fI:])
				if l.Config.LineChars[r] {
					break
				}
				if n := commentSuffixMatch(fwd, fI, suffixes); n > 0 {
					suffixLen = n
					break
				}
				if n := commentSuffixFnMatch(l, fI, suffixFn); n > 0 {
					suffixLen = n
					break
				}
				cI++
				fI += rsize
			}
			if suffixLen > 0 {
				// Consume the suffix as part of the comment body.
				fI += suffixLen
				cI += suffixLen
				src := fwd[:fI]
				tkn := l.Token("#CM", TinCM, nil, src)
				l.pnt.SI += len(src)
				l.pnt.CI = cI
				return tkn
			}
			// EatLine: also consume trailing line characters, only when
			// the comment terminated at a line-char (not via suffix).
			atLineChar := false
			if fI < len(fwd) {
				r, _ := utf8.DecodeRuneInString(fwd[fI:])
				atLineChar = l.Config.LineChars[r]
			}
			if atLineChar && l.Config.CommentLineEatLine[start] {
				var out ScanOut
				Scan(fwd, fI, l.pnt.RI, 1, l.Config.lineRunSpec(), &out)
				fI = out.SI
				rI := out.RI
				src := fwd[:fI]
				tkn := l.Token("#CM", TinCM, nil, src)
				l.pnt.SI += len(src)
				l.pnt.RI = rI
				l.pnt.CI = 1
				return tkn
			}
			src := fwd[:fI]
			tkn := l.Token("#CM", TinCM, nil, src)
			l.pnt.SI += len(src)
			l.pnt.CI = cI
			return tkn
		}
	}

	// Block comments
	for _, pair := range l.Config.CommentBlock {
		start, end := pair[0], pair[1]
		if strings.HasPrefix(fwd, start) {
			suffixes := l.Config.CommentBlockSuffixes[start]
			suffixFn := l.Config.CommentBlockSuffixFuncs[start]
			rI := l.pnt.RI
			cI := l.pnt.CI + len(start)
			fI := len(start)
			suffixLen := 0
			for fI < len(fwd) && !strings.HasPrefix(fwd[fI:], end) {
				if n := commentSuffixMatch(fwd, fI, suffixes); n > 0 {
					suffixLen = n
					break
				}
				if n := commentSuffixFnMatch(l, fI, suffixFn); n > 0 {
					suffixLen = n
					break
				}
				r, rsize := utf8.DecodeRuneInString(fwd[fI:])
				if l.Config.RowChars[r] {
					rI++
					cI = 0
				}
				cI++
				fI += rsize
			}
			if suffixLen > 0 {
				// Consume the suffix, advancing column/row like the End path.
				for k := 0; k < suffixLen; k++ {
					if l.Config.RowChars[rune(fwd[fI+k])] {
						rI++
						cI = 0
					}
					cI++
				}
				fI += suffixLen
				src := fwd[:fI]
				tkn := l.Token("#CM", TinCM, nil, src)
				l.pnt.SI += len(src)
				l.pnt.RI = rI
				l.pnt.CI = cI
				return tkn
			}
			if strings.HasPrefix(fwd[fI:], end) {
				cI += len(end)
				fI += len(end)
				// EatLine: also consume trailing line characters
				if l.Config.CommentBlockEatLine[start] {
					var out ScanOut
					Scan(fwd, fI, rI, 1, l.Config.lineRunSpec(), &out)
					fI = out.SI
					rI = out.RI
					cI = 1
				}
				src := fwd[:fI]
				tkn := l.Token("#CM", TinCM, nil, src)
				l.pnt.SI += len(src)
				l.pnt.RI = rI
				l.pnt.CI = cI
				return tkn
			}
			// Unterminated comment - return bad token
			return l.bad("unterminated_comment", l.pnt.SI, l.pnt.SI+len(start)*9)
		}
	}

	return nil
}

// commentSuffixMatch returns the length of the suffix match at fwd[fI:]
// or zero if no suffix matches. Suffixes are pre-sorted longest-first,
// so the first match is the longest.
func commentSuffixMatch(fwd string, fI int, suffixes []string) int {
	if len(suffixes) == 0 {
		return 0
	}
	rest := fwd[fI:]
	for _, s := range suffixes {
		if strings.HasPrefix(rest, s) {
			return len(s)
		}
	}
	return 0
}

// commentSuffixFnMatch probes the LexMatcher-form suffix terminator at
// offset fI. Returns the length of the returned token's Src (to be
// consumed as part of the comment body) or zero if the matcher passes.
// The lexer point is snapshotted and restored so the matcher cannot
// advance the stream itself.
func commentSuffixFnMatch(lex *Lex, fI int, fn LexMatcher) int {
	if fn == nil {
		return 0
	}
	saved := lex.pnt
	// Position the lexer at the candidate offset so the matcher sees
	// the current body-scan location, not the comment start.
	lex.pnt.SI = saved.SI + fI
	tkn := fn(lex, nil)
	lex.pnt = saved
	if tkn == nil {
		return 0
	}
	return len(tkn.Src)
}

// matchString matches quoted strings: "...", '...', `...`
func (l *Lex) matchString() *Token {
	if l.pnt.SI >= l.pnt.Len {
		return nil
	}
	q, qlen := utf8.DecodeRuneInString(l.Src[l.pnt.SI:])
	if !l.Config.StringChars[q] {
		return nil
	}

	src := l.Src
	sI := l.pnt.SI + qlen
	rI := l.pnt.RI
	cI := l.pnt.CI + 1

	var sb strings.Builder
	srclen := len(src)
	foundClose := false

	// Per-quote body spec (TS buildStringBodySpec): consumes plain body
	// chars and, for multi-line quotes, line chars (advancing rI and
	// resetting cI). Stops on the quote, the escape char, replace chars,
	// and disallowed control chars — dispatched below.
	bodySpec := l.Config.stringBodySpec(q)
	var out ScanOut

	for sI < srclen {
		if Scan(src, sI, rI, cI, bodySpec, &out) {
			sb.WriteString(src[sI:out.SI])
			sI, rI, cI = out.SI, out.RI, out.CI
			if sI >= srclen {
				break
			}
		}

		cI++
		c, csize := utf8.DecodeRuneInString(src[sI:])

		// End quote
		if c == q {
			sI += csize
			foundClose = true
			break
		}

		// Escape character (all string types process escapes)
		if c == l.Config.EscapeChar {
			sI += csize
			cI++
			if sI >= srclen {
				break
			}
			esc := src[sI]

			// Check custom escape map first.
			if l.Config.EscapeMap != nil {
				if rep, ok := l.Config.EscapeMap[string(esc)]; ok {
					sb.WriteString(rep)
					sI++
					continue
				}
			}

			// An escape removed via {"esc": ""}, or the \xHH ASCII escape
			// when strict mode is on, is treated as an unknown escape (so
			// the hardcoded switch below never fires for it). Mirrors the
			// TS runtime: \v/\'/\` can be dropped from the escape map, and
			// strict mode disables \x.
			if l.Config.EscapeRemoved[string(esc)] ||
				(l.Config.EscapeStrict && esc == 'x') {
				if l.Config.AllowUnknownEscape {
					sb.WriteByte(esc)
					sI++
					continue
				}
				if l.Config.StringAbandon {
					return nil
				}
				return l.bad("unexpected", l.pnt.SI, sI+1)
			}

			switch esc {
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'v':
				sb.WriteByte('\v')
			case '"':
				sb.WriteByte('"')
			case '\'':
				sb.WriteByte('\'')
			case '`':
				sb.WriteByte('`')
			case '\\':
				sb.WriteByte('\\')
			case '/':
				sb.WriteByte('/')
			case 'x':
				// ASCII escape \x**
				sI++
				if sI+2 <= srclen {
					cc := parseHexInt(src[sI : sI+2])
					if cc >= 0 {
						sb.WriteRune(rune(cc))
						sI += 1 // loop will increment
						cI += 2
					} else {
						if l.Config.StringAbandon {
							return nil
						}
						return l.bad("invalid_ascii", l.pnt.SI, sI+2)
					}
				} else {
					if l.Config.StringAbandon {
						return nil
					}
					return l.bad("invalid_ascii", l.pnt.SI, sI)
				}
			case 'u':
				// Unicode escape \u**** or \u{*****}. Strict mode disables
				// the braced \u{...} form (plain \uXXXX stays), so a braced
				// escape falls into the 4-hex-digit path below and fails as
				// invalid_unicode (the '{' is not a hex digit).
				sI++
				if !l.Config.EscapeStrict && sI < srclen && src[sI] == '{' {
					sI++
					endI := strings.IndexByte(src[sI:], '}')
					// 1-6 hex digits, any valid Unicode code point.
					if endI >= 1 && endI <= 6 {
						cc := parseHexInt(src[sI : sI+endI])
						if cc >= 0 && cc <= 0x10FFFF {
							// Surrogate code points are not scalar values;
							// WriteRune substitutes U+FFFD, matching
							// encoding/json's handling of lone surrogates.
							sb.WriteRune(rune(cc))
							sI += endI // skip past digits, loop handles +1
							cI += endI + 2
						} else {
							if l.Config.StringAbandon {
								return nil
							}
							return l.bad("invalid_unicode", l.pnt.SI, sI+endI+1)
						}
					} else {
						if l.Config.StringAbandon {
							return nil
						}
						end := sI
						if endI >= 0 {
							end = sI + endI + 1
						}
						return l.bad("invalid_unicode", l.pnt.SI, end)
					}
				} else if sI+4 <= srclen {
					cc := parseHexInt(src[sI : sI+4])
					if cc >= 0 {
						// Combine UTF-16 surrogate pairs split across two
						// \uXXXX escapes (the JSON encoding of astral
						// chars). TS strings are UTF-16 so pairing happens
						// implicitly there; Go must pair explicitly. A
						// lone surrogate becomes U+FFFD via WriteRune,
						// matching encoding/json.
						if 0xD800 <= cc && cc <= 0xDBFF &&
							sI+10 <= srclen && src[sI+4] == '\\' && src[sI+5] == 'u' {
							lo := parseHexInt(src[sI+6 : sI+10])
							if 0xDC00 <= lo && lo <= 0xDFFF {
								full := 0x10000 + (cc-0xD800)<<10 + (lo - 0xDC00)
								sb.WriteRune(rune(full))
								sI += 9
								cI += 10
								break
							}
						}
						sb.WriteRune(rune(cc))
						sI += 3
						cI += 4
					} else {
						if l.Config.StringAbandon {
							return nil
						}
						return l.bad("invalid_unicode", l.pnt.SI, sI+4)
					}
				} else {
					if l.Config.StringAbandon {
						return nil
					}
					return l.bad("invalid_unicode", l.pnt.SI, sI)
				}
			default:
				if l.Config.AllowUnknownEscape {
					sb.WriteByte(esc)
				} else {
					if l.Config.StringAbandon {
						return nil
					}
					return l.bad("unexpected", l.pnt.SI, sI+1)
				}
			}
			sI++
			continue
		}

		// Disallowed control char (multi-line line chars are consumed
		// by the body spec, so any control char reaching here is bad).
		if c < 32 {
			if l.Config.StringAbandon {
				return nil
			}
			break
		}

		// Replace chars stop the body run; emit the replacement.
		if rep, ok := l.Config.StringReplace[c]; ok {
			sb.WriteString(rep)
			sI += csize
			continue
		}

		// Unreachable: every stop class is dispatched above.
		break
	}

	// Check for unterminated string
	if !foundClose {
		if l.Config.StringAbandon {
			return nil
		}
		return l.bad("unterminated_string", l.pnt.SI, sI)
	}

	val := sb.String()
	ssrc := src[l.pnt.SI:sI]
	tkn := l.Token("#ST", TinST, val, ssrc)
	l.pnt.SI = sI
	l.pnt.RI = rI
	l.pnt.CI = cI
	return tkn
}

// matchNumber matches numeric literals: decimal, hex (0x), octal (0o), binary (0b).
// Returns nil if the text at current position is not a valid number (lets text matcher try).
func (l *Lex) matchNumber() *Token {
	if l.pnt.SI >= l.pnt.Len {
		return nil
	}

	src := l.Src
	sI := l.pnt.SI
	ch := src[sI]

	// Must start with digit, +, -, or .
	if !isDigit(ch) && ch != '-' && ch != '+' && ch != '.' {
		return nil
	}

	// Save start position for backtracking
	start := sI

	// Handle sign
	hasSign := false
	if ch == '-' || ch == '+' {
		hasSign = true
		sI++
		if sI >= len(src) {
			return nil
		}
		ch = src[sI]
	}

	// Hex: 0x...
	if ch == '0' && sI+1 < len(src) && (src[sI+1] == 'x' || src[sI+1] == 'X') && l.Config.NumberHex {
		sI += 2
		hexStart := sI
		for sI < len(src) && (isHexDigitByte(src[sI]) || (l.Config.NumberSep != 0 && rune(src[sI]) == l.Config.NumberSep)) {
			sI++
		}
		if sI == hexStart {
			// "0x" with no hex digits → let text matcher handle
			return nil
		}
		// Check trailing text
		if l.isFollowingText(sI) {
			return nil
		}
		msrc := src[start:sI]
		nstr := msrc
		if l.Config.NumberSep != 0 {
			nstr = strings.ReplaceAll(nstr, string(l.Config.NumberSep), "")
		}
		num := parseNumericString(nstr)
		if num != num { // NaN check
			return nil
		}
		tkn := l.Token("#NR", TinNR, num, msrc)
		l.pnt.SI = sI
		l.pnt.CI += sI - start
		return tkn
	}

	// Octal: 0o...
	if ch == '0' && sI+1 < len(src) && (src[sI+1] == 'o' || src[sI+1] == 'O') && l.Config.NumberOct {
		sI += 2
		octStart := sI
		for sI < len(src) && ((src[sI] >= '0' && src[sI] <= '7') || (l.Config.NumberSep != 0 && rune(src[sI]) == l.Config.NumberSep)) {
			sI++
		}
		if sI == octStart {
			return nil
		}
		if l.isFollowingText(sI) {
			return nil
		}
		msrc := src[start:sI]
		nstr := msrc
		if l.Config.NumberSep != 0 {
			nstr = strings.ReplaceAll(nstr, string(l.Config.NumberSep), "")
		}
		num := parseNumericString(nstr)
		if num != num {
			return nil
		}
		tkn := l.Token("#NR", TinNR, num, msrc)
		l.pnt.SI = sI
		l.pnt.CI += sI - start
		return tkn
	}

	// Binary: 0b...
	if ch == '0' && sI+1 < len(src) && (src[sI+1] == 'b' || src[sI+1] == 'B') && l.Config.NumberBin {
		sI += 2
		binStart := sI
		for sI < len(src) && ((src[sI] == '0' || src[sI] == '1') || (l.Config.NumberSep != 0 && rune(src[sI]) == l.Config.NumberSep)) {
			sI++
		}
		if sI == binStart {
			return nil
		}
		if l.isFollowingText(sI) {
			return nil
		}
		msrc := src[start:sI]
		nstr := msrc
		if l.Config.NumberSep != 0 {
			nstr = strings.ReplaceAll(nstr, string(l.Config.NumberSep), "")
		}
		num := parseNumericString(nstr)
		if num != num {
			return nil
		}
		tkn := l.Token("#NR", TinNR, num, msrc)
		l.pnt.SI = sI
		l.pnt.CI += sI - start
		return tkn
	}

	// Decimal number: optional leading dot, digits, decimal, exponent
	// Pattern: \.?[0-9]+([0-9_]*[0-9])? (\.[0-9]?([0-9_]*[0-9])?)? ([eE][-+]?[0-9]+([0-9_]*[0-9])?)?
	hasDigits := false

	// Leading dot
	if ch == '.' {
		if sI+1 >= len(src) || !isDigit(src[sI+1]) {
			return nil // Just a dot, not a number
		}
		sI++ // consume dot
		for sI < len(src) && (isDigit(src[sI]) || (l.Config.NumberSep != 0 && rune(src[sI]) == l.Config.NumberSep)) {
			sI++
			hasDigits = true
		}
	} else {
		// Integer part
		for sI < len(src) && (isDigit(src[sI]) || (l.Config.NumberSep != 0 && rune(src[sI]) == l.Config.NumberSep)) {
			hasDigits = true
			sI++
		}
	}

	if !hasDigits {
		return nil
	}

	// Decimal point
	if sI < len(src) && src[sI] == '.' {
		// Check what follows the dot
		if sI+1 < len(src) && isDigit(src[sI+1]) {
			sI++ // consume dot
			for sI < len(src) && (isDigit(src[sI]) || (l.Config.NumberSep != 0 && rune(src[sI]) == l.Config.NumberSep)) {
				sI++
			}
		} else if sI+1 < len(src) && l.isFollowingText(sI+1) && src[sI+1] != '.' {
			// "0.a" → not a number, let text handle it
			return nil
		} else {
			// Trailing dot: "0." at end or before delimiter
			sI++ // consume dot
		}
	}

	// Exponent
	if sI < len(src) && (src[sI] == 'e' || src[sI] == 'E') {
		eSI := sI
		sI++ // consume e
		if sI < len(src) && (src[sI] == '+' || src[sI] == '-') {
			sI++
		}
		expStart := sI
		for sI < len(src) && (isDigit(src[sI]) || (l.Config.NumberSep != 0 && rune(src[sI]) == l.Config.NumberSep)) {
			sI++
		}
		if sI == expStart {
			// No exponent digits - check if trailing makes it text
			if l.isFollowingText(sI) {
				return nil
			}
			sI = eSI // backtrack, 'e' is not part of number
		}
		// Check for trailing text after exponent
		if l.isFollowingText(sI) {
			return nil
		}
	}

	// Check for trailing alpha/text that would make this text
	if l.isFollowingText(sI) {
		return nil
	}

	msrc := src[start:sI]
	if len(msrc) == 0 || (hasSign && len(msrc) == 1) {
		return nil
	}

	// Check number.exclude
	if l.Config.NumberExclude != nil && l.Config.NumberExclude(msrc) {
		return nil
	}

	nstr := msrc
	if l.Config.NumberSep != 0 {
		nstr = strings.ReplaceAll(nstr, string(l.Config.NumberSep), "")
	}

	num := parseNumericString(nstr)
	if num != num { // NaN
		return nil
	}

	tkn := l.Token("#NR", TinNR, num, msrc)
	l.pnt.SI = sI
	l.pnt.CI += sI - start

	// subMatchFixed: push trailing fixed token as lookahead (matching TS)
	if l.pnt.SI < l.pnt.Len {
		remaining := src[l.pnt.SI:]
		for _, fs := range l.Config.FixedSorted {
			if strings.HasPrefix(remaining, fs) {
				tin := l.Config.FixedTokens[fs]
				fixTkn := l.Token(l.tinNameFor(tin), tin, nil, fs)
				l.pnt.SI += len(fs)
				l.pnt.CI += utf8.RuneCountInString(fs)
				l.tokens = append(l.tokens, fixTkn)
				break
			}
		}
	}

	return tkn
}

// matchText matches unquoted text and checks for value keywords (true, false, null).
// Text is terminated by fixed tokens, whitespace, quotes, and comment starters.
func (l *Lex) matchText() *Token {
	if l.pnt.SI >= l.pnt.Len {
		return nil
	}

	src := l.Src
	sI := l.pnt.SI
	start := sI

	for sI < len(src) {
		ch, chSize := utf8.DecodeRuneInString(src[sI:])
		// Stop at characters whose lexers are enabled, plus ender chars.
		// When a lexer is disabled, its characters are consumed as text
		// (matching TS behavior where enderRE is built conditionally).
		if (l.Config.SpaceLex && l.Config.SpaceChars[ch]) ||
			(l.Config.LineLex && l.Config.LineChars[ch]) ||
			(l.Config.StringLex && l.Config.StringChars[ch]) ||
			l.Config.EnderChars[ch] {
			break
		}
		// Stop at fixed tokens (check multi-char first, then single-char)
		rest := src[sI:]
		isFixed := false
		for _, fs := range l.Config.FixedSorted {
			if strings.HasPrefix(rest, fs) {
				isFixed = true
				break
			}
		}
		if !isFixed && len(l.Config.FixedSorted) == 0 {
			// Fallback for standalone lexer without sorted list
			if ch == '{' || ch == '}' || ch == '[' || ch == ']' ||
				ch == ':' || ch == ',' {
				isFixed = true
			}
		}
		if isFixed {
			break
		}
		// Comment starters (only stop text if comment lexing is enabled)
		if l.Config.CommentLex {
			isComment := false
			for _, cs := range l.Config.CommentLine {
				if strings.HasPrefix(rest, cs) {
					isComment = true
					break
				}
			}
			if !isComment {
				for _, cb := range l.Config.CommentBlock {
					if strings.HasPrefix(rest, cb[0]) {
						isComment = true
						break
					}
				}
			}
			if isComment {
				break
			}
		}
		sI += chSize
	}

	if sI == start {
		return nil
	}

	msrc := src[start:sI]
	mlen := len(msrc)

	// Check for value keywords
	if l.Config.ValueLex {
		if l.Config.ValueDef != nil {
			// Custom value definitions (exact match)
			if val, ok := l.Config.ValueDef[msrc]; ok {
				tkn := l.Token("#VL", TinVL, val, msrc)
				l.pnt.SI += mlen
				l.pnt.CI += utf8.RuneCountInString(msrc)
				return tkn
			}

			// Regex-processed value definitions (TS: cfg.value.defre).
			// Iterated in configure-time name-sorted order for determinism.
			if len(l.Config.ValueDefRe) > 0 {
				for _, entry := range l.Config.ValueDefRe {
					vspec := entry.Def
					if vspec.Match != nil {
						var matchSrc string
						if vspec.Consume {
							matchSrc = src[start:]
						} else {
							matchSrc = msrc
						}
						res := vspec.Match.FindStringSubmatch(matchSrc)
						if res != nil && (vspec.Consume || len(res[0]) == len(msrc)) {
							remsrc := res[0]
							var val any
							if vspec.ValFunc != nil {
								val = vspec.ValFunc(res)
							} else if vspec.Val != nil {
								val = vspec.Val
							} else {
								val = remsrc
							}
							tkn := l.Token("#VL", TinVL, val, remsrc)
							l.pnt.SI = start + len(remsrc)
							l.pnt.CI += utf8.RuneCountInString(remsrc)
							return tkn
						}
					}
				}
			}
		} else {
			// Default value keywords (matching TS: true, false, null only)
			switch msrc {
			case "true":
				tkn := l.Token("#VL", TinVL, true, msrc)
				l.pnt.SI += mlen
				l.pnt.CI += utf8.RuneCountInString(msrc)
				return tkn
			case "false":
				tkn := l.Token("#VL", TinVL, false, msrc)
				l.pnt.SI += mlen
				l.pnt.CI += utf8.RuneCountInString(msrc)
				return tkn
			case "null":
				tkn := l.Token("#VL", TinVL, nil, msrc)
				l.pnt.SI += mlen
				l.pnt.CI += utf8.RuneCountInString(msrc)
				return tkn
			}
		}
	}

	// Plain text — only emit #TX when text lexing is enabled.
	// When text.lex is false but value.lex is true, we still reach here
	// to check value keywords (above), but unmatched text is not emitted.
	// This matches TS behavior (lexer.ts line 506: `if (null == out && mcfg.lex)`).
	if !l.Config.TextLex {
		return nil
	}

	var textVal any = msrc
	// Run text.modify pipeline
	if len(l.Config.TextModify) > 0 {
		for _, mod := range l.Config.TextModify {
			textVal = mod(textVal)
		}
	}
	tkn := l.Token("#TX", TinTX, textVal, msrc)
	l.pnt.SI += mlen
	l.pnt.CI += utf8.RuneCountInString(msrc)

	// Check if next chars are a fixed token - push as lookahead (subMatchFixed)
	if l.pnt.SI < l.pnt.Len {
		remaining := src[l.pnt.SI:]
		matched := false
		for _, fs := range l.Config.FixedSorted {
			if strings.HasPrefix(remaining, fs) {
				tin := l.Config.FixedTokens[fs]
				fixTkn := l.Token(l.tinNameFor(tin), tin, nil, fs)
				l.pnt.SI += len(fs)
				l.pnt.CI += utf8.RuneCountInString(fs)
				l.tokens = append(l.tokens, fixTkn)
				matched = true
				break
			}
		}
		if !matched && len(l.Config.FixedSorted) == 0 {
			// Fallback for standalone lexer
			nextCh := string(src[l.pnt.SI])
			if tin, ok := l.Config.FixedTokens[nextCh]; ok {
				fixTkn := l.Token(l.tinNameFor(tin), tin, nil, nextCh)
				l.pnt.SI++
				l.pnt.CI++
				l.tokens = append(l.tokens, fixTkn)
			}
		}
	}

	return tkn
}

// Helper functions

// tinNameFor returns the name for a Tin, checking custom names first.
func (l *Lex) tinNameFor(tin Tin) string {
	if l.Config.TinNames != nil {
		if name, ok := l.Config.TinNames[tin]; ok {
			return name
		}
	}
	return tinName(tin)
}

func tinName(tin Tin) string {
	switch tin {
	case TinOB:
		return "#OB"
	case TinCB:
		return "#CB"
	case TinOS:
		return "#OS"
	case TinCS:
		return "#CS"
	case TinCL:
		return "#CL"
	case TinCA:
		return "#CA"
	default:
		return "#UK"
	}
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isHexDigitByte(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// isTextChar returns true if the character can continue a text token,
// checking against the config's fixed tokens, ender chars, and string chars.
func (l *Lex) isTextChar(pos int) bool {
	if pos >= len(l.Src) {
		return false
	}
	ch := l.Src[pos]
	r, _ := utf8.DecodeRuneInString(l.Src[pos:])
	// Only treat whitespace as non-text if the corresponding lexer is enabled.
	if l.Config.SpaceLex && l.Config.SpaceChars[r] {
		return false
	}
	if l.Config.LineLex && l.Config.LineChars[r] {
		return false
	}
	// Check string chars (only when string lexing is enabled)
	if l.Config.StringLex && l.Config.StringChars[r] {
		return false
	}
	// Check ender chars
	if l.Config.EnderChars[r] {
		return false
	}
	// Check fixed tokens (multi-char: check if any fixed token starts here)
	rest := l.Src[pos:]
	for _, fs := range l.Config.FixedSorted {
		if strings.HasPrefix(rest, fs) {
			return false
		}
	}
	// Fallback for standalone lexer without sorted list
	if len(l.Config.FixedSorted) == 0 {
		if ch == '{' || ch == '}' || ch == '[' || ch == ']' ||
			ch == ':' || ch == ',' {
			return false
		}
	}
	return true
}

// isFollowingText returns true if the character at pos would continue a text token,
// taking into account fixed tokens, ender chars, and comment starters.
func (l *Lex) isFollowingText(pos int) bool {
	if !l.isTextChar(pos) {
		return false
	}
	// Comment starters are not text continuation (only when comment lexing is enabled)
	if l.Config.CommentLex {
		rest := l.Src[pos:]
		for _, cs := range l.Config.CommentLine {
			if strings.HasPrefix(rest, cs) {
				return false
			}
		}
		for _, cb := range l.Config.CommentBlock {
			if strings.HasPrefix(rest, cb[0]) {
				return false
			}
		}
	}
	return true
}

func parseHexInt(s string) int {
	val := 0
	for _, ch := range s {
		val <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			val |= int(ch - '0')
		case ch >= 'a' && ch <= 'f':
			val |= int(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			val |= int(ch-'A') + 10
		default:
			return -1
		}
	}
	return val
}
