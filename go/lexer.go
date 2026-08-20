// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// A resolved match.value matcher entry, sorted by Name for deterministic iteration.
type MatchValueEntry struct {
	Name  string             // User-supplied name; the sort key.
	Match *regexp.Regexp     // Regexp form; submatch groups fed to Val.
	Val   func([]string) any // Builds the value from the regexp submatches.
	Fn    LexMatcher         // Function-form matcher; takes precedence over Match.
}

// A name-tagged regex value definition, sorted by Name for deterministic lexing (TS: cfg.value.defre).
type ValueDefEntry struct {
	Name string    // The sort key.
	Def  *ValueDef // The regex-based value definition.
}

// A Tin paired with its matcher (regexp or function), sorted by Tin for deterministic lexing.
type MatchTokenEntry struct {
	Tin   Tin            // The token type this matcher produces; the sort key.
	Match *regexp.Regexp // Regexp form.
	Fn    LexMatcher     // Function-form matcher; takes precedence over Match.
	Eager bool           // When true, fire regardless of the rule-position gate.
}

// The lexer: produces tokens from source text.
type Lex struct {
	Src    string     // Source text being lexed.
	Ctx    *Context   // Parse context (includes Ctx.Rule for context-sensitive lexing).
	pnt    Point      // Current scan position (offset, row, column).
	end    *Token     // End-of-source token (cached once reached).
	tokens []*Token   // Lookahead token queue.
	Config *LexConfig // Resolved lexer configuration.
	Err    error      // First error encountered during lexing.

	// Dense Tin-indexed snapshot of Config.IgnoreSet, taken at NewLex so
	// the per-token IGNORE check is an array load instead of a map probe.
	// The exported map stays authoritative: mutations (SetTokenSet, tests
	// replacing the map) are picked up by the next parse's Lex.
	ignoreDense []bool

	// Per-parse intern pool for short string token values (see
	// internAny). Lazily created on first interned value.
	intern map[string]any

	// Negotiated-lexing constraint (LexConfig.Relex). When non-nil, only
	// matchers that can produce one of these tins run. Set only inside
	// Relex, and cleared before it returns.
	want []Tin

	// Where the scan stood immediately before the last recut Relex
	// COMMITTED. One reusable record: the parser copies out of it at the
	// moment of the commit (see ParseAlts), because a later recut
	// overwrites it.
	relexUndo relexPoint

	// Byte-indexed dispatch tables (see lexTables). Engine-built configs
	// carry a shared prebuilt set (LexConfig.tables, rebuilt by
	// buildConfig / SortFixedTokens / Derive); hand-built configs get a
	// fresh per-Lex build in NewLex so direct config mutation before a
	// parse is always honored.
	tables *lexTables
}

// lexTables bundles the byte-indexed lexer dispatch tables derived from
// a LexConfig:
//   - text classifies a byte for the unquoted-text scan: textContinue
//     (plain text), textStop (definitely ends text), textVerify (might
//     end text — run the full checks).
//   - fixedFirst marks first bytes of fixed tokens; fixedTin holds the
//     tin when that byte alone IS a fixed token and no longer token
//     shares the byte (the common all-single-char case), making the
//     fixed-token match one array load. Both are populated only when
//     FixedSorted is non-empty — the standalone-lexer fallback paths
//     keep their original behavior.
//   - start holds one bit per matcher class marking bytes that could
//     START that token kind, so each matcher body rejects a
//     non-candidate position with a single array load instead of rune
//     decoding, map probes, or prefix scans. When a class's config set
//     contains non-ASCII chars, its bit is set on all bytes >= 0x80.
type lexTables struct {
	text       [256]uint8
	fixedFirst [256]bool
	fixedTin   [256]int32
	start      [256]uint8
}

// startTable matcher-class bits.
const (
	startSpace   uint8 = 1 << 0
	startLine    uint8 = 1 << 1
	startString  uint8 = 1 << 2
	startComment uint8 = 1 << 3
)

// textTable byte classes (see Lex.textTable).
const (
	textContinue uint8 = 0
	textStop     uint8 = 1
	textVerify   uint8 = 2
)

// Single-byte strings indexed by byte value, built once. Used as the
// Src of fast-path fixed tokens so no conversion runs per token.
var asciiByteStr [256]string

func init() {
	for i := range asciiByteStr {
		asciiByteStr[i] = string([]byte{byte(i)})
	}
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

	// Relex enables negotiated lexing (TS cfg.lex.relex, Go
	// Options.Lex.Relex). Default false. See Lex.Relex for the mechanism
	// and LexOptions.Relex for why it is opt-in.
	Relex bool

	StringChars        map[rune]bool     // Quote characters.
	MultiChars         map[rune]bool     // Multiline quote characters.
	EscapeChar         rune              // String escape lead character (backslash).
	EscapeMap          map[string]string // Custom escape mappings, e.g. {"n": "\n"}.
	EscapeRemoved      map[string]bool   // Built-in escapes removed via {"v": ""}; consulted before the hardcoded switch.
	EscapeStrict       bool              // Disable the non-standard \xHH and \u{...} structural escapes.
	AllowControl       bool              // Permit raw control chars (< 0x20) in a string body instead of erroring with "unprintable". Line chars are excluded — they stay governed by MultiChars.
	RewindHistory      int               // Max consumed tokens retained for ctx.Rewind. <=0 means unbounded. Default 64.
	SpaceChars         map[rune]bool     // Characters lexed as space (#SP).
	LineChars          map[rune]bool     // Characters lexed as line endings (#LN).
	RowChars           map[rune]bool     // Line characters that increment the row counter.
	CommentLine        []string          // Line comment starters: "#", "//".
	CommentBlock       [][2]string       // Block comment [start, end] pairs.
	NumberHex          bool              // Recognize hex literals (0x...).
	NumberOct          bool              // Recognize octal literals (0o...).
	NumberBin          bool              // Recognize binary literals (0b...).
	NumberSep          rune              // Digit separator char (underscore).
	AllowUnknownEscape bool              // Keep an unknown escape's char rather than erroring.
	StringAbandon      bool              // On string error, return nil instead of bad token.
	StringReplace      map[rune]string   // Character replacements during string scanning.

	// Value definitions: keyword → value (e.g. "true" → true)
	// If nil, uses built-in defaults (true, false, null).
	ValueDef map[string]any

	// ValueDefRe holds regex-processed value definitions (TS: cfg.value.defre).
	// Sorted by name at configure time for deterministic iteration.
	ValueDefRe []*ValueDefEntry

	// Match options (TS: cfg.match)
	MatchLex          bool                   // Enable custom matching. Default: false.
	MatchTokens       map[Tin]*regexp.Regexp // Custom token tin → regexp (storage).
	MatchTokensEager  map[Tin]bool           // Custom token tin → eager (skips the rule-position gate).
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

	// Shared byte-indexed lexer dispatch tables (see lexTables), built
	// by refreshLexTables from buildConfig, SortFixedTokens, and Derive
	// so every parse reads them without a per-parse rebuild. Nil for
	// hand-built configs — NewLex then builds a fresh per-Lex set.
	tables *lexTables

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

	// PlainMap builds a plain unordered map[string]any object node instead
	// of the insertion-ordered OrderedMap (for consumers that track order
	// themselves or don't need it).
	PlainMap bool

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

	// Opt-in parse budget / cancellation (TS cfg.parse.budget). The main
	// rule loop calls ParseBudgetCheck every ParseBudgetN iterations; a
	// false return cancels the parse with a "cancel" error.
	ParseBudgetN     int
	ParseBudgetCheck func(ctx *Context) bool

	// Resolved opt-in recovery settings (TS cfg.parse.recover). Sync
	// token NAMES are resolved to tins here, at config time, so
	// recovery never does a name lookup at error time.
	Recover RecoverConfig

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

// Resolved ANSI-escape palette for error formatting; when Active is false all codes emit as empty strings.
type ColorConfig struct {
	Active bool   // When false, Codes returns empty strings.
	Reset  string // Reset/clear escape sequence.
	Hi     string // High-emphasis (highlight) escape sequence.
	Lo     string // Low-emphasis (dim) escape sequence.
	Line   string // Line-number escape sequence.
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

// Hook that can intercept a matcher before it runs; nil continues normal matching, a result overrides.
type LexCheck func(lex *Lex) *LexCheckResult

// Outcome of a LexCheck hook controlling matcher behavior.
type LexCheckResult struct {
	Done  bool   // If true, use Token as the match result (even if nil).
	Token *Token // The token to return (nil means "no match").
}

// DefaultLexConfig returns the default lexer configuration matching tabnas
// defaults. Scan specs are NOT pre-built here: direct consumers (NewParser,
// standalone lexing, tests) may still mutate the char sets before first
// use, so the lazy spec getters handle this path. Engine-built configs go
// through buildConfig, which does pre-build them.
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
	cfg.refreshLexTables()
}

// refreshLexTables rebuilds the shared byte-indexed dispatch tables.
// Called wherever the engine mutates table-relevant config (fixed
// tokens via SortFixedTokens, buildConfig, Derive's ender-char merge).
func (cfg *LexConfig) refreshLexTables() {
	cfg.tables = buildLexTables(cfg)
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
		sorted = append(sorted, &MatchTokenEntry{
			Tin: tin, Match: re, Eager: cfg.MatchTokensEager[tin]})
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
	var ignoreDense []bool
	if len(cfg.IgnoreSet) > 0 {
		max := 0
		for tin, on := range cfg.IgnoreSet {
			if on && max < tin {
				max = tin
			}
		}
		ignoreDense = make([]bool, max+1)
		for tin, on := range cfg.IgnoreSet {
			if on && 0 <= tin {
				ignoreDense[tin] = true
			}
		}
	}
	tables := cfg.tables
	if tables == nil {
		tables = buildLexTables(cfg)
	}
	return &Lex{
		Src:         src,
		pnt:         Point{Len: len(src), SI: 0, RI: 1, CI: 1},
		Config:      cfg,
		ignoreDense: ignoreDense,
		tables:      tables,
	}
}

// internMaxLen caps the length of string values that go through the
// per-parse intern pool: long strings are rarely repeated, so they keep
// the zero-copy slice instead of paying a lookup.
const internMaxLen = 32

// internAny returns a canonical boxed value for the string s, cloning
// the backing bytes on first sight. Repeated occurrences (JSON object
// keys, enum-like values) then share one string and one boxed interface
// value — no per-occurrence allocation — and, because the pool stores
// clones, interned values never pin the parsed source's backing array.
func (l *Lex) internAny(s string) any {
	if v, ok := l.intern[s]; ok {
		return v
	}
	c := strings.Clone(s)
	if l.intern == nil {
		l.intern = make(map[string]any, 8)
	}
	var v any = c
	l.intern[c] = v
	return v
}

// buildLexTables derives the byte-indexed dispatch tables from a config
// (see lexTables). Engine paths cache the result on the config
// (refreshLexTables); NewLex falls back to a fresh build for hand-built
// configs.
func buildLexTables(cfg *LexConfig) *lexTables {
	l := &lexTables{}

	// Non-ASCII stop chars (or multi-byte-leading comment/fixed starters
	// outside ASCII) force bytes >= 0x80 through the verify path so the
	// scan advances rune-wise exactly like the original loop.
	wide := false

	stops := func(on bool, m map[rune]bool) {
		if !on {
			return
		}
		for r, ok := range m {
			if !ok {
				continue
			}
			if r < 128 {
				l.text[r] = textStop
			} else {
				wide = true
			}
		}
	}
	stops(cfg.SpaceLex, cfg.SpaceChars)
	stops(cfg.LineLex, cfg.LineChars)
	stops(cfg.StringLex, cfg.StringChars)
	stops(true, cfg.EnderChars)

	if len(cfg.FixedSorted) > 0 {
		var multi [256]bool
		for _, fs := range cfg.FixedSorted {
			if 0 == len(fs) {
				continue
			}
			b := fs[0]
			l.fixedFirst[b] = true
			if 1 < len(fs) {
				multi[b] = true
			}
			if b >= 128 {
				wide = true
			}
		}
		for _, fs := range cfg.FixedSorted {
			if 0 == len(fs) {
				continue
			}
			b := fs[0]
			if 1 == len(fs) {
				// A single-byte token always terminates text.
				if b < 128 {
					l.text[b] = textStop
				}
				// Direct-emit only when no longer token shares the byte
				// (longest-match-first must win otherwise).
				if !multi[b] && b < 128 {
					l.fixedTin[b] = int32(cfg.FixedTokens[fs])
				}
			} else if b < 128 && l.text[b] == textContinue {
				// Multi-byte token: might terminate text — verify.
				l.text[b] = textVerify
			}
		}
	} else {
		// Standalone-lexer fallback: matchText hardcodes these six stop
		// chars when no sorted fixed list exists (fixedFirst/fixedTin
		// stay empty so the fixed matcher keeps its map-lookup fallback).
		for _, c := range "{}[]:," {
			l.text[c] = textStop
		}
	}

	if cfg.CommentLex {
		mark := func(s string) {
			if 0 == len(s) {
				return
			}
			if b := s[0]; b >= 128 {
				wide = true
			} else if l.text[b] == textContinue {
				l.text[b] = textVerify
			}
		}
		for _, cs := range cfg.CommentLine {
			mark(cs)
		}
		for _, cb := range cfg.CommentBlock {
			mark(cb[0])
		}
	}

	if wide {
		for c := 128; c < 256; c++ {
			if l.text[c] == textContinue {
				l.text[c] = textVerify
			}
		}
	}

	// Matcher-class start bits (see startTable). Class flags (SpaceLex
	// etc.) are NOT baked in — guardedMatch consults them per call.
	starts := func(bit uint8, m map[rune]bool) {
		wideSet := false
		for r, ok := range m {
			if !ok {
				continue
			}
			if r < 128 {
				l.start[r] |= bit
			} else if !wideSet {
				wideSet = true
				for c := 128; c < 256; c++ {
					l.start[c] |= bit
				}
			}
		}
	}
	starts(startSpace, cfg.SpaceChars)
	starts(startLine, cfg.LineChars)
	starts(startString, cfg.StringChars)
	markComment := func(s string) {
		if 0 == len(s) {
			return
		}
		if b := s[0]; b < 128 {
			l.start[b] |= startComment
		} else {
			for c := 128; c < 256; c++ {
				l.start[c] |= startComment
			}
		}
	}
	for _, cs := range cfg.CommentLine {
		markComment(cs)
	}
	for _, cb := range cfg.CommentBlock {
		markComment(cb[0])
	}
	return l
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

// relexPoint is everything Relex must put back to undo a recut: the scan
// position, the lookahead queue and the cached end token. Together these
// are the Go equivalent of the fields TS saves off its Point (which
// carries token and end inline).
type relexPoint struct {
	pnt    Point
	tokens []*Token
	end    *Token
}

// wants reports whether tin can serve the current negotiated-lexing
// request. With no request in flight every tin is wanted, so matcher
// bodies can call this unconditionally.
func (l *Lex) wants(tin Tin) bool {
	if l.want == nil {
		return true
	}
	for _, t := range l.want {
		if t == tin {
			return true
		}
	}
	return false
}

// Relex re-cuts the source span of an already-lexed token, constrained to
// the tins the requesting alternate wants (LexConfig.Relex). It returns
// the recut token — the cursor then sits after it, and any pending
// lookahead is discarded — or nil, with all lexer state restored.
//
// Why this exists: one character can be claimable by several matchers ('"'
// by an escape class and by a fixed quote; '\n' by a whitespace class and
// by a fixed literal). The first fetch commits to one identity by matcher
// order, and the pushback buffer makes that identity sticky across rule
// boundaries — so an alternate wanting the other, equally valid, identity
// fails. Scannerless notations (GBNF) hit this constantly; the parser
// knows which tins it wants, so it is allowed to renegotiate the cut.
//
// This cannot widen the accepted language. The recut token is returned
// only when its tin is in want, which is the alternate's OWN token list,
// so every position still requires exactly what it always required. A
// wrong re-cut yields nil (or a token a later position rejects) and the
// parse fails; it can never satisfy a position the original cut would not
// have satisfied had the lexer produced it first.
func (l *Lex) Relex(from *Token, want []Tin, rule *Rule) *Token {
	if from == nil || len(from.Src) <= 0 || from.SI < 0 || len(want) == 0 {
		return nil
	}

	saved := relexPoint{pnt: l.pnt, tokens: l.tokens, end: l.end}

	l.pnt.SI = from.SI
	l.pnt.RI = from.RI
	l.pnt.CI = from.CI
	l.tokens = nil
	l.end = nil

	l.want = want
	tkn := l.nextUnfiltered(rule)
	l.want = nil

	if tkn == nil || !tinIn(tkn.Tin, want) {
		l.pnt = saved.pnt
		l.tokens = saved.tokens
		l.end = saved.end
		return nil
	}

	// Commit. The cursor now sits after the recut token; queued lookahead
	// derived from the old cut is stale and stays dropped. Record where
	// the scan was, so the caller can undo this commit if the alternate
	// that asked for it goes on to fail. See Unrelex.
	l.relexUndo = saved

	return tkn
}

// Unrelex undoes a committed recut: it puts the scan back where Relex
// found it. The caller passes its own copy of the snapshot rather than
// reading relexUndo directly, because that field is overwritten by any
// further recut and the undo must reach back to the FIRST one.
func (l *Lex) Unrelex(saved relexPoint) {
	l.pnt = saved.pnt
	l.tokens = saved.tokens
	l.end = saved.end
}

// RelexUndo returns the snapshot of the last committed recut, for a
// caller that intends to undo it later.
func (l *Lex) RelexUndo() relexPoint {
	return l.relexUndo
}

// speculate runs one matcher under a want constraint, keeping its token
// only when the tin is wanted and restoring the scan otherwise. Used for
// custom matchers during negotiated lexing, where the produced tin is not
// knowable in advance. A matcher is free to move the cursor and queue
// tokens, so the restore covers the same fields Relex saves.
func (l *Lex) speculate(match func() *Token) *Token {
	saved := relexPoint{pnt: l.pnt, tokens: l.tokens, end: l.end}

	tkn := match()
	if tkn != nil && tinIn(tkn.Tin, l.want) {
		return tkn
	}

	l.pnt = saved.pnt
	l.tokens = saved.tokens
	l.end = saved.end
	return nil
}

// tinIn reports whether tin appears in tins. Unlike tinMatch this does NOT
// treat #AA as a wildcard: a want list is a request for specific cuts, and
// "any token" is not a cut the lexer can aim at.
func tinIn(tin Tin, tins []Tin) bool {
	for _, t := range tins {
		if t == tin {
			return true
		}
	}
	return false
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

// nextUnfiltered2 produces the next raw token and reports whether a
// matcher actually claimed the input. When none did it synthesizes the
// #BD token TS synthesizes at the same point, so the position is never
// silently lost — but it says so through `claimed`, because the two cases
// raise DIFFERENT errors (an unclaimed position reports token `#UK`, a bad
// token reports its own name and why).
//
// Lex subscribers see every token this returns, ignored ones included,
// which is what TS's Lex.next does — a plugin watching trivia only sees a
// comment or line continuation if the ignored tokens are delivered.
//
// It performs no IGNORE skipping and raises no error: Next adds both.
// Relex needs it without either, since a recut may legitimately produce
// an ignored token (the want list decides) and must never set l.Err for a
// speculative cut that is about to be thrown away.
func (l *Lex) nextUnfiltered2(r *Rule) (*Token, bool) {
	tkn := l.nextRaw(r)
	claimed := tkn != nil

	if !claimed {
		bad := ""
		if l.pnt.SI < len(l.Src) {
			// Take the whole rune: indexing yields the first UTF-8
			// BYTE, and string(byte) widens it to a mangled U+00xx
			// character for any multi-byte (e.g. astral) input.
			ch, _ := utf8.DecodeRuneInString(l.Src[l.pnt.SI:])
			bad = string(ch)
		}
		tkn = &Token{
			Name: "#BD", Tin: TinBD, Src: bad, Why: "unexpected",
			SI: l.pnt.SI, RI: l.pnt.RI, CI: l.pnt.CI,
		}
	}

	if l.Ctx != nil && 0 < len(l.Ctx.LexSubs) {
		for _, sub := range l.Ctx.LexSubs {
			sub(tkn, r, l.Ctx)
		}
	}

	return tkn, claimed
}

// nextUnfiltered is nextUnfiltered2 without the claimed flag, for Relex —
// which treats an unclaimed position and a bad token alike: neither is in
// any want list, so both simply fail the renegotiation.
func (l *Lex) nextUnfiltered(r *Rule) *Token {
	tkn, _ := l.nextUnfiltered2(r)
	return tkn
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

		tkn, claimed := l.nextUnfiltered2(r)

		// Negotiated lexing defers both failures below, and ONLY those:
		// the IGNORE skipping past them is unconditional. A rule's token
		// column may simply be unable to produce this character — a user
		// rule whose only viable alternate is empty, sitting before a
		// class-matched token, has nothing to gate the class in with — so
		// the bad token is handed to the parser instead, where an
		// alternate here or in a later rule may re-cut it. If none ever
		// can, ParseAlts raises the very same error, at this same token.
		relex := l.Config != nil && l.Config.Relex

		// Opt-in recovery defers the same two failures for the same
		// reason relex does: the parser is the only thing that can decide
		// what to do with a bad token. Latching Err and answering #ZZ
		// tells the parse the source ENDED, which is a lie while source
		// remains — recovery then syncs on EOF and abandons the rest of
		// the document. Handing the #BD token over instead lets the skip
		// loop step past the unlexable run token by token, which is also
		// what makes Go and TS agree on how many faults a run contains.
		// TS defers on exactly this condition (rules.ts: the RELEX throw
		// is guarded by !cfg.parse.recover.enabled).
		defer_ := relex || (l.Config != nil && l.Config.Recover.Enabled)

		if !claimed {
			if defer_ {
				return tkn
			}
			src := ""
			if l.pnt.SI < len(l.Src) {
				// Take the whole rune: indexing yields the first UTF-8
				// BYTE, and string(byte) widens it to a mangled U+00xx
				// character for any multi-byte (e.g. astral) input.
				ch, _ := utf8.DecodeRuneInString(l.Src[l.pnt.SI:])
				src = string(ch)
			}
			je := makeTabnasError("unexpected", src, l.Src, l.pnt.SI, l.pnt.RI, l.pnt.CI, l.Config)
			// The unclaimed position is reported as a #BD (bad) token,
			// matching the #BD token TS synthesizes at the same point
			// (ts/src/lexer.ts next; nextUnfiltered2 below does the same).
			l.attachErrContext(je, r, "#BD", "")
			l.Ctx.recordErr(je)
			l.Err = je
			return &Token{Name: "#ZZ", Tin: TinZZ, Val: Undefined, SI: l.pnt.SI, RI: l.pnt.RI, CI: l.pnt.CI}
		}
		// Bad token → store error and return end-of-source
		if tkn.Tin == TinBD {
			if defer_ {
				return tkn
			}
			je := makeTabnasError(tkn.Why, tkn.Src, l.Src, tkn.SI, tkn.RI, tkn.CI, l.Config)
			l.attachErrContext(je, r, tkn.Name, tkn.Why)
			l.Ctx.recordErr(je)
			l.Err = je
			return &Token{Name: "#ZZ", Tin: TinZZ, Val: Undefined, SI: tkn.SI, RI: tkn.RI, CI: tkn.CI}
		}
		// Skip IGNORE tokens (per-instance set, matching TS
		// cfg.tokenSetTins.IGNORE; dense snapshot built in NewLex)
		if tin := tkn.Tin; 0 <= tin && tin < len(l.ignoreDense) && l.ignoreDense[tin] {
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

// runCustom runs one custom matcher, speculatively when a
// negotiated-lexing request is in flight. A custom matcher's token
// identity is opaque — the only way to learn whether it can serve the
// request is to run it — so it runs and the cursor goes back when what it
// produced is not wanted. Skipping custom matchers outright under a want
// would deny their tokens the renegotiation that match and fixed tokens
// get.
func (l *Lex) runCustom(cm *MatcherEntry, rule *Rule) *Token {
	if l.want == nil {
		return cm.Match(l, rule)
	}
	return l.speculate(func() *Token { return cm.Match(l, rule) })
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
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
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
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if tkn := l.guardedMatch(l.Config.FixedLex, l.Config.FixedCheck, l.matchFixed); tkn != nil {
		return tkn
	}

	// Run custom matchers with priority before space (< 3e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 3000000 {
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if l.wants(TinSP) {
		if tkn := l.guardedMatch(l.Config.SpaceLex, l.Config.SpaceCheck, l.matchSpace); tkn != nil {
			return tkn
		}
	}

	// Run custom matchers with priority before line (< 4e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 4000000 {
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if l.wants(TinLN) {
		if tkn := l.guardedMatch(l.Config.LineLex, l.Config.LineCheck, l.matchLine); tkn != nil {
			return tkn
		}
	}

	// Run custom matchers with priority before string (< 5e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 5000000 {
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if l.wants(TinST) {
		if tkn := l.guardedMatch(l.Config.StringLex, l.Config.StringCheck, l.matchString); tkn != nil {
			return tkn
		}
	}

	// Run custom matchers with priority before comment (< 6e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 6000000 {
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if l.wants(TinCM) {
		if tkn := l.guardedMatch(l.Config.CommentLex, l.Config.CommentCheck, l.matchComment); tkn != nil {
			return tkn
		}
	}

	// Run custom matchers with priority before number (< 7e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 7000000 {
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if l.wants(TinNR) {
		if tkn := l.guardedMatch(l.Config.NumberLex, l.Config.NumberCheck, l.matchNumber); tkn != nil {
			return tkn
		}
	}

	// Run custom matchers with priority before text (< 8e6).
	for cI < len(l.Config.CustomMatchers) && l.Config.CustomMatchers[cI].Priority < 8000000 {
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	if l.wants(TinTX) {
		if tkn := l.guardedMatch(l.Config.TextLex || l.Config.ValueLex,
			l.Config.TextCheck, l.matchText); tkn != nil {
			return tkn
		}
	}

	// Run remaining custom matchers (priority >= 8e6).
	for cI < len(l.Config.CustomMatchers) {
		if tkn := l.runCustom(l.Config.CustomMatchers[cI], rule); tkn != nil {
			return tkn
		}
		cI++
	}

	// No matcher matched
	return nil
}

// matchMatch implements the match matcher (TS: makeMatchMatcher at priority 1e6).
// Handles both match.value (regexp → #VL) and match.token (regexp → custom token).
//
// PERFORMANCE. This runs at every lex attempt, so its inner loop is the
// engine's hottest path on grammars with many match tokens. The gating
// work is O(match tokens x alternates x tins-per-slot) and it is easy to
// pay it for nothing:
//
//   - Under a negotiated-lexing want, the rule-position scan is dead —
//     l.wants(tin) is the gate, and positionExpected is never read.
//     Computing it anyway cost 25-46x on real grammars (the llama.cpp
//     GBNF corpus via @tabnas/gbnf: json_arr.gbnf went 73ms -> 1.6ms per
//     parse of an 8-character input once it was skipped).
//   - Pass 1 under a want skips every candidate, so the second pass is
//     pure loop overhead.
//
// Keep gating work inside the branch that reads it. A profile that shows
// tinMatch high and regexp matching low means this has regressed.
func (l *Lex) matchMatch(rule *Rule) *Token {
	if l.pnt.SI >= l.pnt.Len {
		return nil
	}

	fwd := l.Src[l.pnt.SI:]

	// Under a negotiated-lexing constraint, value matchers are skipped
	// outright: they produce value tokens (#VL) by CONTENT, not by the
	// requesting alternate's tin list, so they cannot serve a want.
	// Match values first (TS: valueMatchers loop).
	for _, vm := range l.Config.MatchValues {
		if l.want != nil {
			break
		}
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

		// Two passes so a token EXPECTED at this rule position wins over a
		// merely-eager token that happens to match the same text. When
		// several eager match tokens overlap on the same input (e.g. an
		// ABNF case-insensitive literal `#A` and a range regex `#RX[a-z]`
		// both matching "a"), the position-expected one must be produced
		// so the active rule's alt can advance — mirroring the TS lexer's
		// tcol gating, which only emits tokens listed at the current slot.
		// Pass 0 considers only position-0-expected tokens; pass 1 adds
		// the eager-only fallbacks.
		runMatch := func(mt *MatchTokenEntry, tin Tin, re *regexp.Regexp) *Token {
			if mt.Fn != nil {
				return mt.Fn(l, rule)
			}
			res := re.FindString(fwd)
			if res != "" {
				name := l.tinNameFor(tin)
				tkn := l.Token(name, tin, res, res)
				l.pnt.SI += len(res)
				l.pnt.CI += utf8.RuneCountInString(res)
				return tkn
			}
			return nil
		}
		for pass := 0; pass < 2; pass++ {
			for _, mt := range l.Config.MatchTokensSorted {
				tin, re := mt.Tin, mt.Match
				if re == nil && mt.Fn == nil {
					continue
				}
				if l.want != nil {
					// Negotiated lexing: the alternate's own tin list
					// REPLACES the rule-position gating below — it is the
					// sharper gate, being one alternate's tokens rather
					// than the union over all of them. One filtered pass
					// covers it, so pass 1 is skipped (see the break at the
					// end of the pass loop) to keep each candidate tried
					// once.
					//
					// The position-expected scan below is deliberately NOT
					// run on this path: nothing here reads it, and it costs
					// a walk of every alternate's slot-0 tins — with a
					// ctx.altS lookup each — for every candidate token. See
					// the performance note above matchMatch.
					if !l.wants(tin) {
						continue
					}
				} else {
					// Is this token expected at the current rule position?
					// Read the alt's slots through the context so a token
					// set overridden on this instance (options.tokenSet) is
					// honoured here too. Gating on the registration-time
					// tins would refuse to produce a custom token the
					// override just added to #VAL / #KEY, and the parser
					// would then reject the text the override was meant to
					// admit. Falls back to alt.S outside a parse.
					positionExpected := false
					for _, alt := range alts {
						altS := alt.S
						if l.Ctx != nil {
							altS = l.Ctx.altS(alt)
						}
						if len(altS) > 0 && tinMatch(tin, altS[0]) {
							positionExpected = true
							break
						}
					}
					if pass == 0 {
						// Pass 0: only tokens expected at this rule position.
						if !positionExpected {
							continue
						}
					} else if positionExpected || !mt.Eager {
						// Pass 1: eager-only fallbacks (position-expected
						// ones were already tried in pass 0).
						continue
					}
				}
				if tkn := runMatch(mt, tin, re); tkn != nil {
					return tkn
				}
			}
			// Under a want, pass 1 would skip every candidate — one
			// filtered pass is the whole search.
			if l.want != nil {
				break
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

	// Use sorted list for longest-match-first. Fall back to single-char lookup
	// if no sorted list (e.g. standalone lexer without Tabnas).
	// Negotiated lexing: only candidates the requesting alternate wants
	// may match. Longest-match-wins still holds among those, so a shorter
	// wanted token can win over a longer unwanted one — which is the
	// point: the alternate knows which cut it needs.
	if len(l.Config.FixedSorted) > 0 {
		c := l.Src[l.pnt.SI]
		// First-byte reject: most positions are not fixed tokens.
		if !l.tables.fixedFirst[c] {
			return nil
		}
		// Single-byte token with no longer competitor: direct emit. The
		// dispatch table is a whole-config summary, so under a want the
		// scan falls through to the candidate list rather than trusting
		// it — the summarized token may not be one this alternate names.
		if tin := Tin(l.tables.fixedTin[c]); tin != 0 && l.want == nil {
			tkn := l.Token(l.tinNameFor(tin), tin, nil, asciiByteStr[c])
			l.pnt.SI++
			l.pnt.CI++
			return tkn
		}
		remaining := l.Src[l.pnt.SI:]
		for _, fs := range l.Config.FixedSorted {
			if !l.wants(l.Config.FixedTokens[fs]) {
				continue
			}
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
	if !ok || !l.wants(tin) {
		return nil
	}
	tkn := l.Token(l.tinNameFor(tin), tin, nil, src)
	l.pnt.SI++
	l.pnt.CI++
	return tkn
}

// matchSpace matches space and tab characters.
func (l *Lex) matchSpace() *Token {
	if l.pnt.SI >= l.pnt.Len || l.tables.start[l.Src[l.pnt.SI]]&startSpace == 0 {
		return nil
	}
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
	if l.tables.start[l.Src[sI]]&startLine == 0 {
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
	if l.pnt.SI >= l.pnt.Len || l.tables.start[l.Src[l.pnt.SI]]&startComment == 0 {
		return nil
	}
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
	if l.tables.start[l.Src[l.pnt.SI]]&startString == 0 {
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

	// Escape-free fast path: until the first escape or replace is
	// processed the value is the contiguous source run [valStart, valEnd)
	// and the segment Builder is never touched, so a clean quoted string
	// costs no copy beyond the token itself (a source view, like #TX).
	valStart := sI
	valEnd := -1
	dirty := false

	// UTF-16 surrogate pairing, held ACROSS escapes rather than inside one
	// escape branch. TS strings are UTF-16 so pairing is implicit there; Go
	// decodes to runes and must pair explicitly (doc/differences.md, "\uXXXX
	// surrogate pairs ... Explicitly combined").
	//
	// This used to be a lookahead inside the 4-hex branch that recognised
	// only another 4-hex escape, and the braced branch wrote its rune
	// immediately with no lookahead at all. So `😀` paired, but
	// `\u{d83d}\u{de00}`, `\ud83d\u{de00}` and `\u{d83d}\ude00` all produced
	// two U+FFFD — three of the four standard spellings of an astral
	// character were destroyed, and Go rejected documents TS accepts.
	//
	// Pairing on the decoded CODE UNIT sequence instead makes the spelling of
	// each half irrelevant, which is what the ports actually have to agree
	// on. A high surrogate is withheld until the next unit decides it; a
	// pending high that nothing pairs with is emitted as U+FFFD, preserving
	// the documented lone-surrogate behaviour ("U+FFFD (matches
	// encoding/json)") unchanged.
	pendingHi := -1

	// flushHi resolves a withheld high surrogate that turned out to be
	// unpaired. Call before appending anything that is not a code unit.
	flushHi := func() {
		if pendingHi >= 0 {
			sb.WriteRune(utf8.RuneError)
			pendingHi = -1
		}
	}

	// emitUnit appends one decoded UTF-16 code unit, pairing it with a
	// withheld high surrogate when it is a low one. Both escape spellings
	// funnel through here, which is what makes mixed spellings work.
	emitUnit := func(cc int) {
		if 0xD800 <= cc && cc <= 0xDBFF {
			flushHi() // high after unpaired high: the first one is lone
			pendingHi = cc
			return
		}
		if 0xDC00 <= cc && cc <= 0xDFFF {
			if pendingHi >= 0 {
				sb.WriteRune(rune(0x10000 + (pendingHi-0xD800)<<10 + (cc - 0xDC00)))
				pendingHi = -1
				return
			}
			sb.WriteRune(utf8.RuneError) // lone low surrogate
			return
		}
		flushHi()
		sb.WriteRune(rune(cc))
	}

	// Per-quote body spec (TS buildStringBodySpec): consumes plain body
	// chars and, for multi-line quotes, line chars (advancing rI and
	// resetting cI). Stops on the quote, the escape char, replace chars,
	// and disallowed control chars — dispatched below.
	bodySpec := l.Config.stringBodySpec(q)
	var out ScanOut

	for sI < srclen {
		if Scan(src, sI, rI, cI, bodySpec, &out) {
			if dirty {
				flushHi() // literal text ends any pending pair
				sb.WriteString(src[sI:out.SI])
			}
			sI, rI, cI = out.SI, out.RI, out.CI
			if sI >= srclen {
				break
			}
		}

		cI++
		c, csize := utf8.DecodeRuneInString(src[sI:])

		// End quote
		if c == q {
			valEnd = sI
			sI += csize
			foundClose = true
			break
		}

		// Escape character (all string types process escapes)
		if c == l.Config.EscapeChar {
			if !dirty {
				dirty = true
				sb.WriteString(src[valStart:sI])
			}
			sI += csize
			cI++
			if sI >= srclen {
				break
			}
			esc := src[sI]

			// Only \u can continue a surrogate pair; every other escape is
			// literal content and resolves a withheld high surrogate first.
			if esc != 'u' {
				flushHi()
			}

			// Check custom escape map first.
			if l.Config.EscapeMap != nil {
				if rep, ok := l.Config.EscapeMap[string(esc)]; ok {
					flushHi() // a remapped \u is literal text, not a code unit
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
				// Same positioning as the default unknown-escape branch
				// below — control never reaches that one from here, so
				// without this the strict-\x and escape-removed paths
				// still reported column 1. Span the whole RUNE: a
				// non-ASCII escape char is more than one byte, and half
				// of one is not valid UTF-8 in the diagnostic.
				_, escSize := utf8.DecodeRuneInString(src[sI:])
				l.pnt.SI = sI
				l.pnt.CI = cI - 1
				return l.bad("unexpected", sI, sI+escSize)
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
						emitUnit(cc)
						sI += 1 // loop will increment
						cI += 2
					} else {
						if l.Config.StringAbandon {
							return nil
						}
						// TS positions the escape errors at the BACKSLASH
						// (`sI -= 2` from just past the escape letter) and
						// spans the escape's nominal width. Go left the point
						// at the opening quote. `cI - 2` because this matcher's
						// counter runs one ahead of the TS one (see the
						// unprintable site) and TS itself steps back one here.
						l.pnt.SI = sI - 2
						l.pnt.CI = cI - 2
						return l.bad("invalid_ascii", sI-2, sI+2)
					}
				} else {
					if l.Config.StringAbandon {
						return nil
					}
					// To srclen, not sI: sI is the START of the partial
					// digits, so ending there drops them from the token.
					// TS reports `\x4` for `"\x4`; this reported `\x`.
					l.pnt.SI = sI - 2
					l.pnt.CI = cI - 2
					return l.bad("invalid_ascii", sI-2, srclen)
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
							// Pairs across spellings via emitUnit; an
							// unpaired surrogate still lands as U+FFFD.
							emitUnit(cc)
							sI += endI // skip past digits, loop handles +1
							cI += endI + 2
						} else {
							if l.Config.StringAbandon {
								return nil
							}
							// SI steps back 3 (digit, `{`, `u`) to the
							// backslash, but CI only 2: this matcher does
							// not advance cI over the `{`, so a third step
							// would land the column on the opening quote.
							// Measured — it did, at 1:1 against TS's 1:2.
							l.pnt.SI = sI - 3
							l.pnt.CI = cI - 2
							return l.bad("invalid_unicode", sI-3, sI+endI+1)
						}
					} else {
						if l.Config.StringAbandon {
							return nil
						}
						// No closing brace: the token runs to the end of
						// source, not to the first digit.
						end := srclen
						if endI >= 0 {
							end = sI + endI + 1
						}
						l.pnt.SI = sI - 3
						l.pnt.CI = cI - 2
						return l.bad("invalid_unicode", sI-3, end)
					}
				} else if sI+4 <= srclen {
					cc := parseHexInt(src[sI : sI+4])
					if cc >= 0 {
						// Pairing is handled by emitUnit on the code unit
						// sequence, so this branch no longer looks ahead for
						// a second 4-hex escape. The old lookahead matched
						// only its own spelling, so a braced low half never
						// paired and a mixed pair produced two U+FFFD.
						emitUnit(cc)
						sI += 3
						cI += 4
					} else {
						if l.Config.StringAbandon {
							return nil
						}
						l.pnt.SI = sI - 2
						l.pnt.CI = cI - 2
						return l.bad("invalid_unicode", sI-2, sI+4)
					}
				} else {
					if l.Config.StringAbandon {
						return nil
					}
					// To srclen — same reason as the \x EOF branch above.
					l.pnt.SI = sI - 2
					l.pnt.CI = cI - 2
					return l.bad("invalid_unicode", sI-2, srclen)
				}
			default:
				if l.Config.AllowUnknownEscape {
					sb.WriteByte(esc)
				} else {
					if l.Config.StringAbandon {
						return nil
					}
					// TS positions this ON the escape character, with no
					// step back to the backslash. Span the whole RUNE —
					// `sI+1` takes one byte of a multi-byte character and
					// leaves invalid UTF-8 in the diagnostic.
					_, escSize := utf8.DecodeRuneInString(src[sI:])
					l.pnt.SI = sI
					l.pnt.CI = cI - 1
					return l.bad("unexpected", sI, sI+escSize)
				}
			}
			sI++
			continue
		}

		// Disallowed control char (multi-line line chars are consumed
		// by the body spec, so any control char reaching here is bad).
		// Reported as unprintable, matching the TS runtime — an earlier
		// version fell through to unterminated_string (caught by the
		// cross-runtime token parity harness, ci/parity).
		// Under string.allowControl the non-line control chars are
		// classified as plain body by BuildStringBodySpec and never
		// reach here, so only the embedded-line case remains.
		if c < 32 {
			if l.Config.StringAbandon {
				return nil
			}
			// Position the token ON the offending character, as TS does
			// (`pnt.sI = sI; pnt.cI = cI` before its bad() call). Go left
			// the point at the opening quote, so every string error in
			// this matcher reported 1:1.
			//
			// `cI - 1`, not `cI`: this matcher seeds `cI := l.pnt.CI + 1`
			// to step over the opening quote, so it runs one ahead of the
			// TS counter at the same character. Measured across four
			// offsets of the same input — Go was +1 at every one.
			l.pnt.SI = sI
			l.pnt.CI = cI - 1
			return l.bad("unprintable", sI, sI+1)
		}

		// Replace chars stop the body run; emit the replacement.
		if rep, ok := l.Config.StringReplace[c]; ok {
			if !dirty {
				dirty = true
				sb.WriteString(src[valStart:sI])
			}
			flushHi() // replacement text ends any pending pair
			sb.WriteString(rep)
			sI += csize
			continue
		}

		// Unreachable: every stop class is dispatched above.
		break
	}

	// A high surrogate still withheld at the end of the string had nothing
	// to pair with.
	flushHi()

	// Check for unterminated string
	if !foundClose {
		if l.Config.StringAbandon {
			return nil
		}
		return l.bad("unterminated_string", l.pnt.SI, sI)
	}

	var val any
	if dirty {
		val = sb.String()
	} else if s := src[valStart:valEnd]; len(s) <= internMaxLen {
		// Clean short strings (typically object keys) are interned:
		// deduped across the parse and unpinned from the source.
		val = l.internAny(s)
	} else {
		val = s
	}
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
		if sep := l.Config.NumberSep; sep != 0 && strings.ContainsRune(nstr, sep) {
			nstr = strings.ReplaceAll(nstr, string(sep), "")
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
		if sep := l.Config.NumberSep; sep != 0 && strings.ContainsRune(nstr, sep) {
			nstr = strings.ReplaceAll(nstr, string(sep), "")
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
		if sep := l.Config.NumberSep; sep != 0 && strings.ContainsRune(nstr, sep) {
			nstr = strings.ReplaceAll(nstr, string(sep), "")
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
		var edge bool
		sI, hasDigits, edge = scanDigitRun(src, sI, l.Config.NumberSep)
		if edge {
			return nil // ".5_" style — separator at a run edge
		}
	} else {
		// Integer part
		var edge bool
		sI, hasDigits, edge = scanDigitRun(src, sI, l.Config.NumberSep)
		if edge {
			return nil // "+_1", "1_" — separator at a run edge
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
			var edge bool
			sI, _, edge = scanDigitRun(src, sI, l.Config.NumberSep)
			if edge {
				return nil // "1.5_" — separator at a run edge
			}
		} else if sI+1 < len(src) && l.isFollowingText(sI+1) && src[sI+1] != '.' &&
			!isExponentStart(src, sI+1) {
			// "0.a" → not a number, let text handle it. An exponent is
			// excepted: TS's fraction group makes the digit optional
			// (\.[0-9]?...), so "2.e3" is a number there and the check
			// below must not steal the 'e' as trailing text.
			return nil
		} else {
			// Trailing dot: "0." at end or before delimiter, or "2.e3"
			// where the exponent below consumes the rest.
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
		var expEdge bool
		sI, _, expEdge = scanDigitRun(src, sI, l.Config.NumberSep)
		if expEdge {
			// "1e_2" / "1e2_" — separator at a run edge. Reset to the
			// no-exponent-digits state so the existing following-text
			// check below declines the whole run to text.
			sI = expStart
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
	if sep := l.Config.NumberSep; sep != 0 && strings.ContainsRune(nstr, sep) {
		nstr = strings.ReplaceAll(nstr, string(sep), "")
	}

	num := parseNumericString(nstr)
	if num != num { // NaN
		return nil
	}

	tkn := l.Token("#NR", TinNR, num, msrc)
	l.pnt.SI = sI
	l.pnt.CI += sI - start

	// subMatchFixed: push trailing fixed token as lookahead (matching TS)
	if l.pnt.SI < l.pnt.Len && len(l.Config.FixedSorted) > 0 {
		if c := src[l.pnt.SI]; l.tables.fixedFirst[c] {
			if tin := Tin(l.tables.fixedTin[c]); tin != 0 {
				fixTkn := l.Token(l.tinNameFor(tin), tin, nil, asciiByteStr[c])
				l.pnt.SI++
				l.pnt.CI++
				l.tokens = append(l.tokens, fixTkn)
			} else {
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

	// Table-driven scan: one byte load and branch per plain text byte
	// (the dominant case), with the full per-position checks reserved
	// for bytes that might terminate the run (multi-byte fixed tokens,
	// comment starters, non-ASCII when the config has non-ASCII stops).
	for sI < len(src) {
		switch l.tables.text[src[sI]] {
		case textContinue:
			sI++
			continue
		case textStop:
			// Definite terminator (enabled space/line/string char, ender
			// char, or single-byte fixed token).
		default: // textVerify
			if !l.textStopBase(sI) && !l.textStopComment(sI) {
				_, chSize := utf8.DecodeRuneInString(src[sI:])
				sI += chSize
				continue
			}
		}
		break
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

	// Short unquoted text (typically keys and enum-like values) is
	// interned: deduped across the parse and unpinned from the source.
	var textVal any
	if len(msrc) <= internMaxLen {
		textVal = l.internAny(msrc)
	} else {
		textVal = msrc
	}
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
		if len(l.Config.FixedSorted) > 0 {
			if c := src[l.pnt.SI]; l.tables.fixedFirst[c] {
				if tin := Tin(l.tables.fixedTin[c]); tin != 0 {
					fixTkn := l.Token(l.tinNameFor(tin), tin, nil, asciiByteStr[c])
					l.pnt.SI++
					l.pnt.CI++
					l.tokens = append(l.tokens, fixTkn)
				} else {
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
			}
		} else {
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

// isExponentStart reports whether a well-formed exponent begins at pos:
// [eE], an optional sign, then at least one digit. Mirrors the exponent
// group of the TS number regexp, ([eE][-+]?[0-9]+([0-9_]*[0-9])?), which
// likewise requires a digit before the separator run — so a bare "2.e"
// stays text in both runtimes.
// scanDigitRun consumes a run of digits and separators from i, returning
// the end index, whether any real digit was consumed, and whether the run
// begins or ends with a separator. Separators are legal only BETWEEN the
// digits of a run (TS parity: the number regexp writes every digit run as
// `[0-9]+([0-9_]*[0-9])?` — digit-led and digit-ended, interior separators
// free). A run with a separator at either edge is not a number at all and
// the whole span must fall to the lenient text matcher: the scanner
// previously swallowed edge separators, so `+_1` parsed as 1, `1_` as 1
// and `[1_,2]` as [1,2], silently changing user data.
func scanDigitRun(src string, i int, sep rune) (end int, sawDigit bool, edgeSep bool) {
	start := i
	lastSep := false
	for i < len(src) {
		if isDigit(src[i]) {
			sawDigit = true
			lastSep = false
			i++
		} else if sep != 0 && rune(src[i]) == sep {
			if i == start {
				edgeSep = true
			}
			lastSep = true
			i++
		} else {
			break
		}
	}
	if lastSep {
		edgeSep = true
	}
	return i, sawDigit, edgeSep
}

func isExponentStart(src string, pos int) bool {
	if pos >= len(src) {
		return false
	}
	if src[pos] != 'e' && src[pos] != 'E' {
		return false
	}
	i := pos + 1
	if i < len(src) && (src[i] == '+' || src[i] == '-') {
		i++
	}
	return i < len(src) && isDigit(src[i])
}

// textStopBase reports whether the rune at pos terminates a text run for
// reasons other than a comment starter: enabled space/line/string chars,
// ender chars, and fixed tokens (with the standalone-lexer fallback).
func (l *Lex) textStopBase(pos int) bool {
	src := l.Src
	ch, _ := utf8.DecodeRuneInString(src[pos:])
	// Stop at characters whose lexers are enabled, plus ender chars.
	// When a lexer is disabled, its characters are consumed as text
	// (matching TS behavior where enderRE is built conditionally).
	if (l.Config.SpaceLex && l.Config.SpaceChars[ch]) ||
		(l.Config.LineLex && l.Config.LineChars[ch]) ||
		(l.Config.StringLex && l.Config.StringChars[ch]) ||
		l.Config.EnderChars[ch] {
		return true
	}
	// Stop at fixed tokens (longest-first sorted list).
	rest := src[pos:]
	for _, fs := range l.Config.FixedSorted {
		if strings.HasPrefix(rest, fs) {
			return true
		}
	}
	// Fallback for standalone lexer without sorted list.
	if len(l.Config.FixedSorted) == 0 {
		if ch == '{' || ch == '}' || ch == '[' || ch == ']' ||
			ch == ':' || ch == ',' {
			return true
		}
	}
	return false
}

// textStopComment reports whether a comment starts at pos (only when
// comment lexing is enabled).
func (l *Lex) textStopComment(pos int) bool {
	if !l.Config.CommentLex {
		return false
	}
	rest := l.Src[pos:]
	for _, cs := range l.Config.CommentLine {
		if strings.HasPrefix(rest, cs) {
			return true
		}
	}
	for _, cb := range l.Config.CommentBlock {
		if strings.HasPrefix(rest, cb[0]) {
			return true
		}
	}
	return false
}

// isTextChar returns true if the character can continue a text token,
// checking against the config's fixed tokens, ender chars, and string chars.
func (l *Lex) isTextChar(pos int) bool {
	if pos >= len(l.Src) {
		return false
	}
	switch l.tables.text[l.Src[pos]] {
	case textContinue:
		return true
	case textStop:
		return false
	}
	// textVerify: comment starters do NOT stop isTextChar (only
	// isFollowingText considers them), so check the base stops only.
	return !l.textStopBase(pos)
}

// isFollowingText returns true if the character at pos would continue a text token,
// taking into account fixed tokens, ender chars, and comment starters.
func (l *Lex) isFollowingText(pos int) bool {
	if pos >= len(l.Src) {
		return false
	}
	switch l.tables.text[l.Src[pos]] {
	case textContinue:
		return true
	case textStop:
		return false
	}
	return !l.textStopBase(pos) && !l.textStopComment(pos)
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
