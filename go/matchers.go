// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// matchers.go — exported constructors for the engine's built-in lexer
// matchers and lexing primitives, mirroring the TS public exports
// (makeFixedMatcher, makeSpaceMatcher, ..., makeLex, makePoint,
// makeRuleSpec in ts/src/lexer.ts and ts/src/parser.ts) so plugin
// authors can compose, reorder, or wrap the standard matchers.
//
// Each MakeXMatcher factory has the MakeLexMatcher shape
// (func(*LexConfig, *Options) LexMatcher), so a factory can be handed
// directly to LexOptions.Match as a MatchSpec.Make value, or invoked to
// obtain a standalone LexMatcher.
//
// Unlike TS — where a matcher closes over the Config snapshot passed to
// its factory and the factories are re-run on every configure() — the
// Go engine mutates a single *LexConfig in place (SetOptions does
// `*cfg = *newcfg`). The returned matchers therefore read the LEXER's
// live Config (lex.Config) at match time rather than capturing the cfg
// argument, so they always agree with the engine's own dispatch and
// stay current across SetOptions calls. The cfg and opts parameters
// exist for MakeLexMatcher signature compatibility and may be nil.
//
// TS exports with no separate Go factory seam (the equivalent already
// exists under another name):
//   - makeToken  → MakeToken (token.go)
//   - makeRule   → MakeRule (rule.go)
//   - makeParser → NewParser (parser.go)
//   - makeMatchMatcher → the match matcher is not separately buildable;
//     it is driven by LexConfig.MatchTokens / MatchValues / MatchTokenFns
//     (set via Options.Match), which is the Go seam for custom regexp
//     and function token matching.

// MakeFixedMatcher builds the engine's fixed-token matcher (single- and
// multi-character fixed tokens, longest match first) as a standalone
// LexMatcher (TS: makeFixedMatcher). The matcher honours the live
// FixedLex flag and FixedCheck hook of the lexer it runs against.
func MakeFixedMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		return lex.guardedMatch(lex.Config.FixedLex, lex.Config.FixedCheck, lex.matchFixed)
	}
}

// MakeSpaceMatcher builds the engine's space matcher (runs of the
// configured space chars → #SP) as a standalone LexMatcher
// (TS: makeSpaceMatcher). Honours the live SpaceLex flag and SpaceCheck
// hook of the lexer it runs against.
func MakeSpaceMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		return lex.guardedMatch(lex.Config.SpaceLex, lex.Config.SpaceCheck, lex.matchSpace)
	}
}

// MakeLineMatcher builds the engine's line matcher (runs of the
// configured line chars → #LN, advancing the row counter for row chars)
// as a standalone LexMatcher (TS: makeLineMatcher). Honours the live
// LineLex flag and LineCheck hook of the lexer it runs against.
func MakeLineMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		return lex.guardedMatch(lex.Config.LineLex, lex.Config.LineCheck, lex.matchLine)
	}
}

// MakeStringMatcher builds the engine's quoted-string matcher (quote
// chars, escapes, multiline quotes, replacements → #ST) as a standalone
// LexMatcher (TS: makeStringMatcher). Honours the live StringLex flag
// and StringCheck hook of the lexer it runs against.
func MakeStringMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		return lex.guardedMatch(lex.Config.StringLex, lex.Config.StringCheck, lex.matchString)
	}
}

// MakeCommentMatcher builds the engine's comment matcher (line and
// block comments, eatline and suffix handling → #CM) as a standalone
// LexMatcher (TS: makeCommentMatcher). Honours the live CommentLex flag
// and CommentCheck hook of the lexer it runs against.
func MakeCommentMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		return lex.guardedMatch(lex.Config.CommentLex, lex.Config.CommentCheck, lex.matchComment)
	}
}

// MakeNumberMatcher builds the engine's number matcher (decimal, hex,
// octal, binary literals with separators → #NR) as a standalone
// LexMatcher (TS: makeNumberMatcher). Honours the live NumberLex flag
// and NumberCheck hook of the lexer it runs against.
func MakeNumberMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		return lex.guardedMatch(lex.Config.NumberLex, lex.Config.NumberCheck, lex.matchNumber)
	}
}

// MakeTextMatcher builds the engine's unquoted-text matcher (text runs
// → #TX, with value-keyword recognition → #VL) as a standalone
// LexMatcher (TS: makeTextMatcher). As in the engine's own dispatch,
// the matcher runs when either TextLex or ValueLex is enabled (value
// keywords are recognised inside text runs), and honours the live
// TextCheck hook of the lexer it runs against.
func MakeTextMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		return lex.guardedMatch(lex.Config.TextLex || lex.Config.ValueLex,
			lex.Config.TextCheck, lex.matchText)
	}
}

// MakeLex creates a lexer for src using cfg (TS: makeLex). Parity alias
// for NewLex: the TS factory wraps `new Lex(ctx)`, whose Go equivalent
// is the existing NewLex constructor.
func MakeLex(src string, cfg *LexConfig) *Lex {
	return NewLex(src, cfg)
}

// MakePoint creates a source-cursor Point (TS: makePoint). The optional
// trailing values are sI, rI, cI in that order; omitted values take the
// TS constructor defaults (sI=0, rI=1, cI=1).
func MakePoint(srclen int, pos ...int) Point {
	pnt := Point{Len: srclen, SI: 0, RI: 1, CI: 1}
	if len(pos) > 0 {
		pnt.SI = pos[0]
	}
	if len(pos) > 1 {
		pnt.RI = pos[1]
	}
	if len(pos) > 2 {
		pnt.CI = pos[2]
	}
	return pnt
}

// MakeRuleSpec creates an empty named RuleSpec (TS: makeRuleSpec).
// Alternates and lifecycle actions are added via the RuleSpec methods
// (AddOpen, AddClose, AddBO, ...). Most callers should prefer
// (*Tabnas).Rule, which creates and registers the RuleSpec in one step;
// this constructor exists for plugin code that builds specs standalone
// (e.g. before installing them via RSM()).
func MakeRuleSpec(name string) *RuleSpec {
	return &RuleSpec{Name: name}
}
