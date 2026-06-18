// Copyright (c) 2013-2026 Richard Rodger, MIT License

// Package tabnas provides a lenient JSON parser that supports relaxed syntax
// including unquoted keys, implicit objects/arrays, comments, trailing commas,
// single-quoted strings, path diving (nested object shorthand), and more.
//
// It is a Go port of the tabnas TypeScript library, faithfully implementing
// the same matcher-based lexer and rule-based parser architecture.
package tabnas

import (
	"strconv"
	"strings"
)

// Version is the current version of the tabnas Go module.
const Version = "0.2.0"

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
	"internal":             "internal error: {src}",
}

// Error hint templates matching TypeScript defaults (ts/src/defaults.ts).
// Injected with StrInject like errorMessages.
var defaultHints = map[string]string{
	"unknown": "Unknown error code: {code}",

	"unexpected": "The character(s) {src} do not match any rule alternative active at\nthis position.",

	"invalid_unicode": "The escape sequence {src} does not encode a valid unicode code point.",

	"invalid_ascii": "The escape sequence {src} does not encode a valid ASCII character.",

	"unprintable": "The character {src} (code point below 32) is not allowed inside a\nstring literal.",

	"unterminated_string": "This string has no end quote.",

	"unterminated_comment": "This comment is never closed.",

	"unknown_rule": "No rule named {rulename} is defined.",

	"end_of_source": "Unexpected end of source.",

	"internal": "The parser failed unexpectedly; this is a bug in tabnas\nor a plugin, not in your input.",
}

// Structured error returned by Parse, carrying the failure location, cause, and formatting context.
type TabnasError struct {
	Code   string // Error code keying errorMessages/defaultHints, e.g. "unexpected", "unterminated_string".
	Detail string // Human-readable detail message (e.g. "unterminated string: \"abc")
	Pos    int    // 0-based character position in source
	Row    int    // 1-based line number
	Col    int    // 1-based column number
	Src    string // Source fragment at the error (the token text)
	Hint   string // Additional explanatory text for this error code

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

// makeTabnasError creates a TabnasError with the proper Detail message.
// Message and hint templates come from cfg (which merges Options.Error /
// Options.Hint over the package defaults); cfg also supplies the colour
// palette, errmsg name/suffix/link, and instance tag. A nil cfg uses the
// package defaults with colour disabled. Template {key} placeholders are
// injected with StrInject, matching TS errinject (ts/src/error.ts).
func makeTabnasError(code, src, fullSource string, pos, row, col int, cfg *LexConfig) *TabnasError {
	msgs := errorMessages
	hints := defaultHints
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
	// errinject: code, src, and location details.
	ref := map[string]any{
		"code": code,
		"src":  src,
		"pos":  pos,
		"row":  row,
		"col":  col,
	}

	je.Detail = StrInject(tmpl, ref)
	if hint, ok := hints[code]; ok {
		je.Hint = StrInject(hint, ref)
	}

	return je
}
