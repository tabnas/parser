// Copyright (c) 2013-2026 Richard Rodger, MIT License

// Package tabnas provides a lenient JSON parser that supports relaxed syntax
// including unquoted keys, implicit objects/arrays, comments, trailing commas,
// single-quoted strings, path diving (nested object shorthand), and more.
//
// It is a Go port of the tabnas TypeScript library, faithfully implementing
// the same matcher-based lexer and rule-based parser architecture.
package tabnas

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// VERSION is this module's version. It MUST equal ts/package.json
// "version": the release orchestrator rewrites both, and
// TestVersionMatchesPackageJSON fails the build if they drift.
const VERSION = "0.8.9"

// Error message templates matching TypeScript defaults (ts/src/defaults.ts).
// Values are injected with StrInject using {key} placeholders.
var errorMessages = map[string]string{
	"unknown":              "unknown error: {code}",
	"unexpected":           "unexpected character(s): {src}",
	"invalid_unicode":      "invalid unicode escape: {src}",
	"invalid_ascii":        "invalid ascii escape: {src}",
	"unprintable":          "unprintable character: {src}",
	"unterminated_string":  "unterminated string: {src}",
	"unterminated_comment": "unterminated comment: {src}",
	"unknown_rule":         "unknown rule: {rulename}",
	"end_of_source":        "unexpected end of source",
	"cancel":               "parse cancelled",
	"internal":             "internal error: {src}",
}

// Error hint templates matching TypeScript defaults (ts/src/defaults.ts).
// Injected with StrInject like errorMessages.
var defaultHints = map[string]string{
	"unknown": "Unknown error code: {code}\nDetails:\n{details}",

	"unexpected": "The character(s) {src} do not match any rule alternative active at\nthis position.",

	"invalid_unicode": "The escape sequence {src} does not encode a valid unicode code point.",

	"invalid_ascii": "The escape sequence {src} does not encode a valid ASCII character.",

	"unprintable": "The character {src} (code point below 32) is not allowed inside a\nstring literal.",

	"unterminated_string": "This string has no end quote.",

	"unterminated_comment": "This comment is never closed.",

	"unknown_rule": "No rule named {rulename} is defined.",

	"end_of_source": "Unexpected end of source.",

	"cancel": "The parse was cancelled by the caller's parse.budget.onCheck callback\n(or exceeded its configured budget) before completing.",

	"internal": "The parser failed unexpectedly; this is a bug in tabnas\nor a plugin, not in your input.",
}

// Structured error returned by Parse, carrying the failure location, cause, and formatting context.
// RecoveredAt records what one panic-mode recovery did, for
// diagnostics consumers that want to show the skipped region.
type RecoveredAt struct {
	Skipped int // Tokens skipped before reaching the sync token.
	Sync    Tin // The token the parse resumed on.

	// Bad marks a diagnostic for an unlexable RUN absorbed at fetch
	// time, rather than one raised by a panic-mode resync. Skipped is
	// then the run's length in tokens and Sync is unset.
	Bad bool
}

type TabnasError struct {
	Code   string // Error code keying errorMessages/defaultHints, e.g. "unexpected", "unterminated_string".
	Detail string // Human-readable detail message (e.g. "unterminated string: \"abc")
	Pos    int    // 0-based character position in source
	Row    int    // 1-based line number
	Col    int    // 1-based column number
	Src    string // Source fragment at the error (the token text)
	Hint   string // Additional explanatory text for this error code

	// Recovered is set when this error was recovered from rather than
	// fatal (options.parse.recover): how far the parse skipped and the
	// sync token it resumed on. Nil on a fail-fast error.
	Recovered *RecoveredAt

	fullSource string      // Complete input source (for generating site extract)
	tag        string      // Custom error tag name (TS: errmsg.name), defaults to "tabnas"
	instTag    string      // Instance tag (TS: options.tag) shown in the internal suffix
	color      ColorConfig // ANSI palette applied by Error(); zero value disables colour
	fileName   string      // Source file name (TS: meta.fileName); "" → "<no-file>"
	suffix     any         // errmsg.suffix: nil/true → standard internal block, false → none, string/func → custom
	link       string      // errmsg.link: optional "see also" line in the standard suffix
	ruleName   string      // Rule active when the error was raised (for the internal suffix)
	ruleState  string      // Rule state ("o"/"c") when the error was raised
	tokenName  string      // Token name (e.g. "#BD") at the error
	why        string      // Token why annotation, if any
	plugins    []string    // Registered plugin names (for the internal suffix)

	// Structured-diagnostic fields (MarshalJSON), populated by
	// Parser.finishErr — the one funnel every parse-error path passes
	// through with a Context in hand. Nil-safe: paths without a Context
	// (empty source, internalError) leave them empty.
	ruleStack []string // Rule names root-first incl. the failing rule; NoRule skipped by identity.
	expected  []string // Sorted token names some alternate of the failing rule accepts next.
	diagRule  string   // Failing rule name; "" when only the NoRule sentinel was available.
}

// Error returns a formatted error message matching the TypeScript TabnasError format:
//
//	[tabnas/<code>]: <message>
//	  --> <file>:<row>:<col>
//	 <line-2> | <source>
//	 <line-1> | <source>
//	    <line> | <source with error>
//	             ^^^^ <message>
//	 <line+1> | <source>
//	 <line+2> | <source>
//
//	  <hint, indented>
//
//	  <errmsg.link, when configured>
//	  --internal: tag=...; rule=...; token=...; plugins=...--
//
// When e.color.Active is true the header, arrow, caret, line-number
// gutter, and suffix are wrapped in ANSI escapes — matching TS error.ts
// output. The trailing suffix block follows TS `errmsg.suffix`: rendered
// by default (or when true), suppressed when false, and replaced when a
// string or func(code, src string) string is configured.
func (e *TabnasError) Error() string {
	msg := e.Detail

	hi, lo, line, reset := e.color.Codes()

	var b strings.Builder

	// Line 1: [tag/<code>]: <message>
	tag := e.tag
	if tag == "" {
		tag = "tabnas"
	}
	b.WriteString(hi)
	b.WriteString("[")
	b.WriteString(tag)
	b.WriteString("/")
	b.WriteString(e.Code)
	b.WriteString("]:")
	b.WriteString(reset)
	b.WriteString(" ")
	b.WriteString(msg)

	// Line 2: --> <file>:<row>:<col> (TS prints "<no-file>" when unnamed)
	file := e.fileName
	if file == "" {
		file = "<no-file>"
	}
	b.WriteString("\n  ")
	b.WriteString(line)
	b.WriteString("-->")
	b.WriteString(reset)
	b.WriteString(" ")
	b.WriteString(file)
	b.WriteString(":")
	b.WriteString(strconv.Itoa(e.Row))
	b.WriteString(":")
	b.WriteString(strconv.Itoa(e.Col))

	// Source site extract
	if e.fullSource != "" {
		site := errsite(e.fullSource, e.Src, msg, e.Row, e.Col, e.color)
		if site != "" {
			b.WriteString("\n")
			b.WriteString(site)
		}
	}

	// Hint: blank line, then hint indented two spaces per line (TS errdesc).
	if e.Hint != "" {
		b.WriteString("\n\n")
		for i, hline := range strings.Split(strings.TrimSpace(e.Hint), "\n") {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("  ")
			b.WriteString(hline)
		}
	}

	// Suffix (TS errmsg.suffix). nil defaults to true.
	switch sfx := e.suffix.(type) {
	case nil, bool:
		if v, isBool := sfx.(bool); isBool && !v {
			break
		}
		b.WriteString("\n")
		if e.link != "" {
			b.WriteString("\n  ")
			b.WriteString(lo)
			b.WriteString(e.link)
			b.WriteString(reset)
		}
		b.WriteString("\n  ")
		b.WriteString(lo)
		b.WriteString("--internal: tag=")
		b.WriteString(e.instTag)
		b.WriteString("; rule=")
		b.WriteString(e.ruleName)
		b.WriteString("~")
		b.WriteString(e.ruleState)
		b.WriteString("; token=")
		b.WriteString(e.tokenName)
		if e.why != "" {
			b.WriteString("~")
			b.WriteString(e.why)
		}
		b.WriteString("; plugins=")
		b.WriteString(strings.Join(e.plugins, ","))
		b.WriteString("--")
		b.WriteString(reset)
	case string:
		b.WriteString("\n")
		b.WriteString(sfx)
	case func(code, src string) string:
		b.WriteString("\n")
		b.WriteString(sfx(e.Code, e.Src))
	}

	return b.String()
}

// errsite generates a source code extract showing the error location,
// matching the TypeScript errsite() function output format.  When color
// is active, line-number gutters and the caret row are wrapped in the
// configured ANSI Line/Reset codes.
func errsite(src, sub, msg string, row, col int, color ColorConfig) string {
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}

	_, _, line, reset := color.Codes()

	lines := strings.Split(src, "\n")

	// row is 1-based, convert to 0-based index
	lineIdx := row - 1
	if lineIdx >= len(lines) {
		lineIdx = len(lines) - 1
	}

	// Determine padding width based on largest line number shown
	maxLineNum := row + 2
	pad := len(strconv.Itoa(maxLineNum)) + 2

	// Build context lines: 2 before, error line, caret line, 2 after
	var result []string

	ln := func(num int, text string) string {
		numStr := strconv.Itoa(num)
		return line + strings.Repeat(" ", pad-len(numStr)) + numStr + " | " + reset + text
	}

	// 2 lines before
	if lineIdx-2 >= 0 {
		result = append(result, ln(row-2, lines[lineIdx-2]))
	}
	if lineIdx-1 >= 0 {
		result = append(result, ln(row-1, lines[lineIdx-1]))
	}

	// Error line
	if lineIdx >= 0 && lineIdx < len(lines) {
		result = append(result, ln(row, lines[lineIdx]))
	}

	// Caret line
	caretCount := len(sub)
	if caretCount < 1 {
		caretCount = 1
	}
	indent := strings.Repeat(" ", pad) + "   " + strings.Repeat(" ", col-1)
	result = append(result, indent+line+strings.Repeat("^", caretCount)+" "+msg+reset)

	// 2 lines after
	if lineIdx+1 < len(lines) {
		result = append(result, ln(row+1, lines[lineIdx+1]))
	}
	if lineIdx+2 < len(lines) {
		result = append(result, ln(row+2, lines[lineIdx+2]))
	}

	return strings.Join(result, "\n")
}

// detailQuoteRE strips JSON string quotes for {details} injection, exactly
// as TS does (ts/src/error.ts strinject): every quote preceded by a
// non-quote character is dropped, so {"a":"b"} renders as {a:b}.
var detailQuoteRE = regexp.MustCompile(`([^"])"`)

// errInjectRef builds the StrInject reference bag used for EVERY error
// template render — makeTabnasError and Parser.makeError must inject from
// the same bag, or a placeholder only one of them supplies (as happened
// with {details}) reaches the user raw from the other. It carries the
// built-in location keys, the grammar's own `use` details (whose keys
// override the built-ins — see makeTabnasError), and the rendered
// {details} value.
//
// {details} (used by the "unknown" hint) renders the whole detail bag —
// the merged `use` maps, TS's `details` parameter — the way TS errinject
// renders an object injection (ts/src/error.ts strinject): JSON.stringify,
// then strip every quote that follows a non-quote character. Set AFTER the
// use merge, mirroring TS where the literal `details` key is spread last
// and so wins over a use key of the same name. json.Marshal sorts map keys
// where JSON.stringify preserves insertion order, and TS folds an extra
// `state` key into its details on the bad-token path — residual,
// non-contractual text differences (DIVERGENCE.md "Not divergences":
// error message text).
func errInjectRef(code, src string, pos, row, col int, use []map[string]any) map[string]any {
	ref := map[string]any{
		"code": code,
		"src":  src,
		"pos":  pos,
		"row":  row,
		"col":  col,
	}
	detailBag := map[string]any{}
	for _, u := range use {
		for k, v := range u {
			ref[k] = v
			detailBag[k] = v
		}
	}
	if raw, jerr := json.Marshal(detailBag); jerr == nil {
		ref["details"] = detailQuoteRE.ReplaceAllString(string(raw), "$1")
	} else {
		// Unmarshalable detail values (funcs, channels, NaN) — fall back
		// to fmt, which at least names them; fmt sorts map keys too.
		ref["details"] = fmt.Sprintf("%v", detailBag)
	}
	return ref
}

// hintFor selects the hint template for a code: the code's own entry, or
// the "unknown" hint as the fallback for a code with no hint of its own —
// TS behavior (ts/src/error.ts errdesc: cfg.hint[code] || ... ||
// cfg.hint.unknown), whose {details} placeholder then names the
// unrecognized code and shows the raised details. The package-default
// guard mirrors the message-template fallback in makeTabnasError.
func hintFor(hints map[string]string, code string) string {
	if hint, ok := hints[code]; ok {
		return hint
	}
	if hint, ok := hints["unknown"]; ok {
		return hint
	}
	return defaultHints["unknown"]
}

// makeTabnasError creates a TabnasError with the proper Detail message.
// Message and hint templates come from cfg (which merges Options.Error /
// Options.Hint over the package defaults); cfg also supplies the colour
// palette, errmsg name/suffix/link, and instance tag. A nil cfg uses the
// package defaults with colour disabled. Template {key} placeholders are
// injected with StrInject, matching TS errinject (ts/src/error.ts).
// The optional `use` bag carries a grammar's own error details (a bad token's
// tkn.Use). Its keys OVERRIDE the built-in location keys below, because a
// grammar that names a detail `row` means its own row — csv's
// "Row {row} has too many fields" means the CSV row, not the source line. The
// built-in value used to win, so that message rendered as "Row -1 ..." in Go
// while TS rendered "Row 2". Merging once, with `use` last, also avoids a
// second injection pass re-expanding a placeholder that appeared inside an
// injected value.
func makeTabnasError(code, src, fullSource string, pos, row, col int, cfg *LexConfig, use ...map[string]any) *TabnasError {
	msgs := errorMessages
	hints := defaultHints

	// Normalise an unset location. A grammar can legitimately raise on the
	// NoToken sentinel (token.go: SI/RI/CI = -1) — csv does, via
	// ctx.T0.Bad() when the lookahead slot has been consumed — and that used
	// to surface to the reader as "--> file:-1:-1" and inject {row}/{col} as
	// -1. TS reports 1:1 for the same case. Rows and columns are 1-based and
	// pos is a 0-based offset, so anything below those floors is "unknown",
	// not a position.
	if pos < 0 {
		pos = 0
	}
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}

	je := &TabnasError{
		Code:       code,
		Pos:        pos,
		Row:        row,
		Col:        col,
		Src:        src,
		fullSource: fullSource,
	}

	if cfg != nil {
		if cfg.ErrorMessages != nil {
			msgs = cfg.ErrorMessages
		}
		if cfg.Hints != nil {
			hints = cfg.Hints
		}
		je.color = cfg.Color
		je.tag = cfg.ErrTag
		je.instTag = cfg.Tag
		je.suffix = cfg.ErrSuffix
		je.link = cfg.ErrLink
	}

	tmpl, ok := msgs[code]
	if !ok {
		tmpl = msgs["unknown"]
		if tmpl == "" {
			tmpl = errorMessages["unknown"]
		}
	}

	// Injection reference bag, matching the heuristic spirit of TS
	// errinject: code, src, location details, the `use` bag, and the
	// rendered {details} value (see errInjectRef).
	ref := errInjectRef(code, src, pos, row, col, use)

	je.Detail = StrInject(tmpl, ref)
	if hint := hintFor(hints, code); hint != "" {
		je.Hint = StrInject(hint, ref)
	}

	return je
}

// MarshalJSON emits the structured diagnostic documented in
// schema/diagnostic.schema.json, semantically identical to the TS
// TabnasError.toJSON output. Only `code` is contractual across runtimes;
// message/hint/src are informative text. Arrays are always emitted as []
// (never null), and `len` counts Unicode code points of the token source.
//
// VALUE receiver on purpose: the method then belongs to the method set of
// both TabnasError and *TabnasError, so json.Marshal emits the diagnostic
// for values and pointers alike, and a nil *TabnasError still encodes as
// null (encoding/json short-circuits the nil pointer before invoking the
// method).
func (e TabnasError) MarshalJSON() ([]byte, error) {
	code := e.Code
	if code == "" {
		code = "unknown"
	}

	ruleStack := e.ruleStack
	if ruleStack == nil {
		ruleStack = []string{}
	}
	expected := e.expected
	if expected == nil {
		expected = []string{}
	}
	plugins := e.plugins
	if plugins == nil {
		plugins = []string{}
	}

	// The full source LINE containing the error: line row-1 of the
	// source split on '\n', with one trailing '\r' stripped. "" when the
	// row is invalid or the source is unavailable.
	srcLine := ""
	if e.fullSource != "" && e.Row >= 1 {
		lines := strings.Split(e.fullSource, "\n")
		if e.Row-1 < len(lines) {
			srcLine = strings.TrimSuffix(lines[e.Row-1], "\r")
		}
	}

	type tokenJSON struct {
		Name string `json:"name"`
		Src  string `json:"src"`
	}
	return json.Marshal(struct {
		Status    string    `json:"status"`
		Code      string    `json:"code"`
		Message   string    `json:"message"`
		Hint      string    `json:"hint"`
		Row       int       `json:"row"`
		Col       int       `json:"col"`
		Pos       int       `json:"pos"`
		Len       int       `json:"len"`
		Rule      string    `json:"rule"`
		RuleStack []string  `json:"ruleStack"`
		Token     tokenJSON `json:"token"`
		Expected  []string  `json:"expected"`
		Src       string    `json:"src"`
		Plugins   []string  `json:"plugins"`
		Version   string    `json:"version"`
	}{
		Status:    "failure",
		Code:      code,
		Message:   e.Detail,
		Hint:      e.Hint,
		Row:       e.Row,
		Col:       e.Col,
		Pos:       e.Pos,
		Len:       utf8.RuneCountInString(e.Src),
		Rule:      e.diagRule,
		RuleStack: ruleStack,
		Token:     tokenJSON{Name: e.tokenName, Src: e.Src},
		Expected:  expected,
		Src:       srcLine,
		Plugins:   plugins,
		Version:   VERSION,
	})
}
