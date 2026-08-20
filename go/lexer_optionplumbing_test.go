// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Lexer option-plumbing regressions:
//
//   - string.allowControl: raw control chars inside a string body.
//     Pinned cross-runtime by the shared fixture test/spec/lex-string-control.tsv
//     (the TS counterpart is 'string-allow-control-spec' in ts/test/lex.test.js).
//   - string.check / comment.check: the TS runtime declared these hooks and
//     consulted them at lex time but never copied them out of the options, so
//     they were dead there. Go already copied all eight; these tests pin that,
//     so the two runtimes cannot drift apart again.
//   - MapToOptions: the grammar-text converter silently dropped
//     StringOptions.EscapeStrict.
//   - Deep: an opaque value (a *regexp.Regexp) on the `over` side must replace
//     the base, not be "merged" field-by-field into a corrupt zero value.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// lexOne lexes src with cfg and renders the first token the way the shared
// lex-string-control fixture spells it: "ERROR:<code>" or "<name>:<value>".
//
// Note the runtime difference this has to paper over: on a bad token the Go
// Lex.Next stores the error and substitutes #ZZ, where TS delivers the #BD
// token itself. The cross-runtime contract is the error CODE, so read it off
// lex.Err rather than off the returned token.
func lexOne(src string, cfg *LexConfig) string {
	cfg.IgnoreSet = map[Tin]bool{}
	lex := NewLex(src, cfg)
	tkn := lex.Next()

	if lex.Err != nil {
		if je, ok := lex.Err.(*TabnasError); ok {
			return "ERROR:" + je.Code
		}
		return "ERROR:" + lex.Err.Error()
	}

	// Not a string assertion: the number matcher's Val is numeric, and a
	// blank there would have made every #NR row in a fixture compare equal
	// to every other. TS builds the same cell by string-concatenating
	// tkn.val, which is what %v reproduces.
	if str, ok := tkn.Val.(string); ok {
		return tkn.Name + ":" + str
	}
	if nil == tkn.Val {
		return tkn.Name + ":"
	}
	return tkn.Name + ":" + fmt.Sprintf("%v", tkn.Val)
}

// TestSpecLexTextLineTerminator runs the shared
// lex-text-line-terminator.tsv fixture (the TS counterpart is
// 'text-line-terminator-spec' in ts/test/lex.test.js). Columns:
// lineLex | fixedSep | input | expected, same ERROR:<code> / <name>:<value>
// contract as lex-string-control. `fixedSep` registers U+2028 as a fixed
// token, the case where the separator DOES get an ender alternative and the
// run ends normally instead of failing.
//
// The NUMBER matcher is covered here too, and it is the trap this port walks
// into: one predicate (textStopBase, via isFollowingText) answers both "can
// text continue?" and "can a number end here?". Teaching THAT about U+2028
// makes Go emit `#NR:1` for `1<U+2028>b` where TS reports `unexpected` — the
// TS number ender regex has its own alternatives and does not contain the
// separators either. `1\nb` is the control that keeps the rule from being
// read as "reject everything".
//
// The rule under test comes from the REGEX DIALECT, not the ender set: the
// TS ender is built as `cfg.line.lex ? 'y' : 'ys'`, so with line lexing on
// `.` cannot cross a JS line terminator. \n and \r are also enders, so they
// END the run; U+2028 and U+2029 are not, so the match FAILS and no text
// token is produced. RE2's `.` excludes only \n, so Go ran straight through
// both and made `a<U+2028>b` one text token.
//
// The U+2028/U+2029 cells hold the RAW code point — the shared escape codec
// is deliberately minimal (\n \r \t \\ only) and has no \u form. The guard
// below is why that is safe to rely on: a tool that normalised them away
// would otherwise leave these rows passing while testing nothing.
func TestSpecLexTextLineTerminator(t *testing.T) {
	rows := loadSpecTSV(t, "lex-text-line-terminator")

	all := ""
	for _, row := range rows {
		for _, c := range row.cols {
			all += c
		}
	}
	if !strings.ContainsRune(all, 0x2028) || !strings.ContainsRune(all, 0x2029) {
		t.Fatal("lex-text-line-terminator.tsv no longer contains U+2028/U+2029 — " +
			"the raw code points were normalised away and these rows now test nothing")
	}

	for _, row := range rows {
		lineLex := tsvCol(row.cols, 0) == "true"
		fixedSep := tsvCol(row.cols, 1) == "true"
		src := preprocessEscapes(tsvCol(row.cols, 2))
		want := preprocessEscapes(tsvCol(row.cols, 3))

		cfg := buildConfig(&Options{Line: &LineOptions{Lex: &lineLex}})
		if fixedSep {
			// U+2028 as a fixed token: TS's rePart.ender gains an
			// alternative that matches it, so the run ENDS there instead of
			// failing, and the text before it is emitted.
			sep := string(rune(0x2028))
			cfg.FixedTokens = map[string]Tin{sep: Tin(500)}
			cfg.TinNames = map[Tin]string{Tin(500): "#SEP"}
			cfg.SortFixedTokens()
		}

		if got := lexOne(src, cfg); got != want {
			t.Errorf("lex-text-line-terminator line %d: lineLex=%v fixedSep=%v input=%q: got %q, want %q",
				row.lineNo, lineLex, fixedSep, src, got, want)
		}
	}
}

// TestSpecLexStringControl runs the shared lex-string-control.tsv fixture.
// Columns: allowControl | input | expected, where expected is either
// ERROR:<code> or #ST:<string value>. \t \n \r are escaped in BOTH the input
// and expected columns (a raw tab would be a column separator), so both are
// run through preprocessEscapes — matching the TS loadTSV, which unescapes at
// load time.
func TestSpecLexStringControl(t *testing.T) {
	for _, row := range loadSpecTSV(t, "lex-string-control") {
		allowControl := tsvCol(row.cols, 0) == "true"
		src := preprocessEscapes(tsvCol(row.cols, 1))
		want := preprocessEscapes(tsvCol(row.cols, 2))

		cfg := buildConfig(&Options{
			String: &StringOptions{AllowControl: &allowControl},
		})

		got := lexOne(src, cfg)

		if got != want {
			t.Errorf("lex-string-control line %d: allowControl=%v input=%q: got %q, want %q",
				row.lineNo, allowControl, src, got, want)
		}
	}
}

// The option must default to the strict (pre-existing) behaviour, and must
// reach the LexConfig from the Options.
func TestStringAllowControlDefaultsStrict(t *testing.T) {
	if cfg := buildConfig(&Options{}); cfg.AllowControl {
		t.Fatal("AllowControl must default to false (strict)")
	}
	if cfg := buildConfig(&Options{String: &StringOptions{}}); cfg.AllowControl {
		t.Fatal("AllowControl must stay false when string options omit it")
	}
	on := true
	if cfg := buildConfig(&Options{
		String: &StringOptions{AllowControl: &on},
	}); !cfg.AllowControl {
		t.Fatal("string.AllowControl was not copied into the LexConfig")
	}
}

// Control chars outside the fixture's \t \n \r set (which the TSV loaders
// cannot carry) behave the same way: admitted when allowControl is set.
func TestStringAllowControlOtherControlChars(t *testing.T) {
	on := true

	for _, c := range []rune{0x00, 0x07, 0x0b, 0x0c, 0x1f} {
		src := "\"x" + string(c) + "y\""

		if got := lexOne(src, buildConfig(&Options{})); got != "ERROR:unprintable" {
			t.Errorf("strict U+%04X: got %q, want ERROR:unprintable", c, got)
		}

		got := lexOne(src, buildConfig(&Options{
			String: &StringOptions{AllowControl: &on},
		}))
		if want := "#ST:x" + string(c) + "y"; got != want {
			t.Errorf("allowControl U+%04X: got %q, want %q", c, got, want)
		}
	}
}

// All eight check hooks must survive the Options -> LexConfig conversion.
// string and comment are the two that were dead in the TS runtime.
func TestOptionsCheckHooksReachConfig(t *testing.T) {
	hook := func(lex *Lex) *LexCheckResult { return nil }

	cfg := buildConfig(&Options{
		Fixed:   &FixedOptions{Check: hook},
		Match:   &MatchOptions{Check: hook},
		Space:   &SpaceOptions{Check: hook},
		Line:    &LineOptions{Check: hook},
		Text:    &TextOptions{Check: hook},
		Number:  &NumberOptions{Check: hook},
		Comment: &CommentOptions{Check: hook},
		String:  &StringOptions{Check: hook},
	})

	for name, got := range map[string]LexCheck{
		"fixed":   cfg.FixedCheck,
		"match":   cfg.MatchCheck,
		"space":   cfg.SpaceCheck,
		"line":    cfg.LineCheck,
		"text":    cfg.TextCheck,
		"number":  cfg.NumberCheck,
		"comment": cfg.CommentCheck,
		"string":  cfg.StringCheck,
	} {
		if got == nil {
			t.Errorf("options.%s.check was not copied into the LexConfig", name)
		}
	}
}

// A string/comment check hook must actually run and be able to claim the match.
func TestStringCommentCheckHooksRun(t *testing.T) {
	cases := []struct {
		which string
		src   string
		set   func(*LexConfig, LexCheck)
	}{
		{"string", `"abc"`, func(c *LexConfig, h LexCheck) { c.StringCheck = h }},
		{"comment", "# c", func(c *LexConfig, h LexCheck) { c.CommentCheck = h }},
	}

	for _, c := range cases {
		called := false
		cfg := buildConfig(&Options{})
		cfg.IgnoreSet = map[Tin]bool{}
		c.set(cfg, func(lex *Lex) *LexCheckResult {
			called = true
			tkn := lex.Token("#VL", TinVL, "claimed", lex.Fwd(1))
			p := lex.Cursor()
			p.SI = p.Len
			return &LexCheckResult{Done: true, Token: tkn}
		})

		tkn := NewLex(c.src, cfg).Next()
		if !called {
			t.Errorf("%s check hook was never called", c.which)
		}
		if tkn.Val != "claimed" {
			t.Errorf("%s check hook did not claim the match: got %v (%s)",
				c.which, tkn.Val, tkn.Name)
		}
	}
}

// MapToOptions is the grammar-text -> Options converter. It handled every
// other StringOptions field but silently dropped escapeStrict.
func TestMapToOptionsStringFields(t *testing.T) {
	opts := MapToOptions(map[string]any{
		"string": map[string]any{
			"lex":          true,
			"chars":        `"`,
			"multiChars":   "`",
			"escapeChar":   `\`,
			"allowUnknown": false,
			"escapeStrict": true,
			"allowControl": true,
			"abandon":      true,
			"escape":       map[string]any{"n": "\n", "v": nil},
			"replace":      map[string]any{"~": "!"},
		},
	})

	if opts.String == nil {
		t.Fatal("string options not converted")
	}
	if opts.String.EscapeStrict == nil || !*opts.String.EscapeStrict {
		t.Error("string.escapeStrict was dropped by MapToOptions")
	}
	if opts.String.AllowControl == nil || !*opts.String.AllowControl {
		t.Error("string.allowControl was dropped by MapToOptions")
	}

	// The fields that already worked must keep working.
	if opts.String.Lex == nil || !*opts.String.Lex {
		t.Error("string.lex dropped")
	}
	if opts.String.Chars != `"` {
		t.Errorf("string.chars = %q", opts.String.Chars)
	}
	if opts.String.MultiChars != "`" {
		t.Errorf("string.multiChars = %q", opts.String.MultiChars)
	}
	if opts.String.EscapeChar != `\` {
		t.Errorf("string.escapeChar = %q", opts.String.EscapeChar)
	}
	if opts.String.AllowUnknown == nil || *opts.String.AllowUnknown {
		t.Error("string.allowUnknown dropped")
	}
	if opts.String.Abandon == nil || !*opts.String.Abandon {
		t.Error("string.abandon dropped")
	}
	if opts.String.Escape["n"] != "\n" || opts.String.Escape["v"] != "" {
		t.Errorf("string.escape = %#v", opts.String.Escape)
	}
	if opts.String.Replace['~'] != "!" {
		t.Errorf("string.replace = %#v", opts.String.Replace)
	}

	// ...and escapeStrict must reach the LexConfig, not just the Options.
	if cfg := buildConfig(&opts); !cfg.EscapeStrict || !cfg.AllowControl {
		t.Errorf("buildConfig: EscapeStrict=%v AllowControl=%v, want both true",
			cfg.EscapeStrict, cfg.AllowControl)
	}
}

// escapeStrict from grammar text must change lexing, not just sit in a struct:
// with it set, \x41 is no longer the structural hex escape.
func TestMapToOptionsEscapeStrictAffectsLexing(t *testing.T) {
	build := func(strict bool) *LexConfig {
		opts := MapToOptions(map[string]any{
			"string": map[string]any{
				"escapeStrict": strict,
				"allowUnknown": false,
			},
		})
		return buildConfig(&opts)
	}

	if got := lexOne(`"\x41"`, build(false)); got != "#ST:A" {
		t.Errorf(`escapeStrict=false: "\x41" got %q, want "#ST:A"`, got)
	}
	if got := lexOne(`"\x41"`, build(true)); got != "ERROR:unexpected" {
		t.Errorf(`escapeStrict=true: "\x41" got %q, want "ERROR:unexpected"`, got)
	}
}

// Deep must let an opaque value (no exported fields — *regexp.Regexp,
// time.Time, ...) on the `over` side replace the base. The old field-by-field
// struct merge produced a freshly allocated zero value instead, i.e. a regexp
// with the empty pattern: both operands silently destroyed.
func TestDeepOpaqueStructReplaces(t *testing.T) {
	a := regexp.MustCompile(`^00+`)
	b := regexp.MustCompile(`newRe`)

	got, ok := Deep(a, b).(*regexp.Regexp)
	if !ok {
		t.Fatalf("Deep(a,b) type = %T, want *regexp.Regexp", Deep(a, b))
	}
	if got != b {
		t.Errorf("Deep(a,b) = %q, want the over regexp %q", got.String(), b.String())
	}

	// Same through a map, which is how an option map carries one.
	m := Deep(
		map[string]any{"exclude": a},
		map[string]any{"exclude": b},
	).(map[string]any)
	if re := m["exclude"].(*regexp.Regexp); re != b {
		t.Errorf("map merge exclude = %q, want %q", re.String(), b.String())
	}

	// A nil over side still loses, and a struct WITH exported fields still
	// merges field-by-field (zero over fields preserve base).
	if got := Deep(a, nil); got != nil {
		t.Errorf("Deep(a,nil) = %v, want nil", got)
	}
	onTrue := true
	merged := Deep(
		&StringOptions{Chars: `"`, AllowUnknown: &onTrue},
		&StringOptions{MultiChars: "`"},
	).(*StringOptions)
	if merged.Chars != `"` || merged.MultiChars != "`" || merged.AllowUnknown == nil {
		t.Errorf("exported-field struct merge broken: %#v", merged)
	}
}

// A string body containing a raw control char must round-trip through the
// public parse path too, not just the lexer, when allowControl is set.
func TestStringAllowControlThroughParse(t *testing.T) {
	on := true
	j := makeJSON(Options{String: &StringOptions{AllowControl: &on}})

	out, err := j.Parse("{\"a\":\"x\ty\"}")
	if err != nil {
		t.Fatalf("parse with allowControl failed: %v", err)
	}
	got, ok := Plainify(out).(map[string]any)
	if !ok || got["a"] != "x\ty" {
		t.Errorf("parsed %#v, want a = %q", out, "x\ty")
	}

	// Default (strict) still rejects it.
	strict := makeJSON(Options{})
	if _, err := strict.Parse("{\"a\":\"x\ty\"}"); err == nil ||
		!strings.Contains(err.Error(), "unprintable") {
		t.Errorf("strict parse should fail with unprintable, got %v", err)
	}
}
