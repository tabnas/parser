/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */

package tabnas

// What ends a text run, and what does NOT.
//
// The answer is `Config.LineChars` (plus space chars, ender chars, fixed
// tokens and comment starters) and nothing else. Two pieces of code
// decide it here and both read the config: `buildLexTables` classifies
// each byte, and `textStopBase` re-checks the ones it classified as
// `textVerify`. There is no third source.
//
// Which of the two answers a given input is not obvious, and getting it
// wrong makes this file vacuous rather than failing. Injecting a stop
// into `textStopBase` alone changed nothing, because with no non-ASCII
// stop configured `buildLexTables` never sets `wide` and every byte
// above 0x7F is `textContinue` — `textStopBase` is not consulted at all.
// The cases below were checked by injecting the defect at BOTH layers
// and watching them go red:
//
//   wide = true + U+2028/9 in textStopBase  ->  rows 1 and 2 fail
//   l.text['\n'] = textStop                 ->  rows 3 and 7 fail
//
// The TypeScript half did have one. It built its text-ender regex without
// the `s` flag whenever line lexing was on, so the JS regex dialect's own
// line-terminator set — U+000A, U+000D, U+2028, U+2029 — ended a text run
// whether or not any option named those characters. Audit item P4.
//
// ts/test/text-ender.test.js asserts the same eight cases with the same
// expected spans; that pairing is the test. Before P4 the first three
// rows below were `#BD"a"` in TypeScript and exactly what they are here
// in Go — so this file, on its own, would have gone on passing while the
// two ports disagreed. Nothing in the shared fixtures used a non-ASCII
// separator or a retargeted LineChars, which is why 3,693 rows saw none
// of it.

import (
	"reflect"
	"testing"
)

func TestTextEnderUsesTheConfigAndNothingElse(t *testing.T) {
	const LS = "\u2028" // LINE SEPARATOR
	const PS = "\u2029" // PARAGRAPH SEPARATOR

	no := false

	// The sources of the #TX tokens, in order. Only text spans are
	// compared: TypeScript emits an #LN token between them and this
	// lexer does not, a separate and long-standing shape difference
	// rather than the property under test.
	textSpans := func(opts Options, src string) []string {
		j := Make(opts)
		lex := MakeLex(src, j.Config())
		out := []string{}
		for i := 0; i < 12; i++ {
			tk := lex.Next()
			if tk == nil {
				break
			}
			if tk.Name == "#TX" {
				out = append(out, tk.Src)
			}
			// A bad token is the failure shape TypeScript had here, so
			// surface it rather than ending the loop on it silently.
			if tk.Name == "#BD" {
				out = append(out, "#BD:"+tk.Src)
			}
			if tk.Name == "#ZZ" || tk.Name == "#BD" {
				break
			}
		}
		return out
	}

	for _, c := range []struct {
		label string
		opts  Options
		src   string
		want  []string
	}{
		// No option names U+2028 or U+2029, so neither ends a text run.
		// TypeScript produced `#BD:a` for both before P4.
		{"LS is not an ender", Options{}, "a" + LS + "b", []string{"a" + LS + "b"}},
		{"PS is not an ender", Options{}, "a" + PS + "b", []string{"a" + PS + "b"}},

		// LineChars no longer contains a newline, so a newline is
		// ordinary text. `#BD:a` in TypeScript before P4.
		{"LF after LineChars is retargeted",
			Options{Line: &LineOptions{Chars: ";"}}, "a\nb", []string{"a\nb"}},

		// Controls. Each already agreed across the ports, and each would
		// catch a repair that went too far — dropping the ender-char
		// class instead of the dialect's extra set.
		{"a configured line char still ends the run",
			Options{}, "a\nb", []string{"a", "b"}},
		{"and so does the other default one",
			Options{}, "a\rb", []string{"a", "b"}},
		{"a retargeted line char ends the run",
			Options{Line: &LineOptions{Chars: ";"}}, "a;b", []string{"a", "b"}},
		{"line lexing off means nothing line-ish ends it",
			Options{Line: &LineOptions{Lex: &no}}, "a\nb", []string{"a\nb"}},

		// Control, and the point of the whole change: naming U+2028 makes
		// it an ender, exactly as naming `;` does. go/utf8_content_test.go
		// relies on this same configuration for row counting.
		{"naming LS makes it an ender after all",
			Options{Line: &LineOptions{Chars: "\r\n" + LS, RowChars: "\n" + LS}},
			"a" + LS + "b", []string{"a", "b"}},
	} {
		if got := textSpans(c.opts, c.src); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %q -> %q, want %q", c.label, c.src, got, c.want)
		}
	}
}
