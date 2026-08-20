/* Copyright (c) 2013-2026 Richard Rodger and other contributors, MIT License */

package tabnas

// The Go half of audit item P9: a fixed token must not be queued at a
// point the lexer never reached.
//
// A CONSUMING value regexp matches against the forward source rather
// than against `msrc`, so it may take a shorter prefix than the text run
// the ender found. This port returns straight after such a match and
// re-enters the matcher, so the fixed token that follows is found at a
// point that actually exists.
//
// TypeScript did not: `subMatchFixed` built its token at the POINT while
// `tsrc` came from the end of `msrc` — two different offsets whenever a
// consuming regexp stopped short. It fabricated a delimiter where the
// source had something else, swallowed that something, and emitted the
// real delimiter a second time.
//
// This half asserts behaviour that was already correct, so the only
// question about it is whether it is connected to anything. It is: every
// row below is this port's stream, measured, and
// ts/test/fixed-token-point.test.js asserts the same table with the same
// values. A change to either port that reintroduces the offset mix-up
// separates them.
//
// Found by review on the P4 change (parser#140) and reproduced on `main`
// with no line terminator involved, so it predates P4 and is independent
// of how P4 is settled. The two P4-dependent rows from that PR are NOT
// here on purpose — what a text run does at a line terminator is P4's
// question, and pinning it here would couple this file to that decision.

import (
	"reflect"
	"regexp"
	"testing"
)

func TestConsumingValueDoesNotQueueADistantFixedToken(t *testing.T) {
	spans := func(src string) []string {
		j := Make(Options{Value: &ValueOptions{Def: map[string]*ValueDef{
			"at": {
				Match:   regexp.MustCompile(`^@\w+`),
				Consume: true,
				ValFunc: func(m []string) any { return map[string]any{"at": m[0]} },
			},
		}}})
		lex := MakeLex(src, j.Config())
		out := []string{}
		for i := 0; i < 14; i++ {
			tk := lex.Next()
			if tk == nil {
				break
			}
			// #SP is skipped: TypeScript emits a space token here and
			// this port does not, a separate shape difference.
			if tk.Name != "#SP" {
				out = append(out, tk.Name+":"+tk.Src)
			}
			if tk.Name == "#ZZ" || tk.Name == "#BD" {
				break
			}
		}
		return out
	}

	for _, c := range []struct {
		src  string
		want []string
	}{
		// The regexp takes `@abc`; the ender is the comma, further on.
		// The characters in between must survive as text. TypeScript
		// emitted `#CA:,` at offset 4 — where the source holds `-` —
		// then re-emitted the real comma.
		{"@abc-rest,", []string{"#VL:@abc", "#TX:-rest", "#CA:,", "#ZZ:"}},
		{"@abc-r,x", []string{"#VL:@abc", "#TX:-r", "#CA:,", "#TX:x", "#ZZ:"}},
		{"@abc--,", []string{"#VL:@abc", "#TX:--", "#CA:,", "#ZZ:"}},

		// Controls: when the run IS fully consumed, the fixed token must
		// still be queued in the same call. A guard that disabled
		// subMatchFixed outright would pass the three rows above and
		// fail these two.
		{"@abc,rest", []string{"#VL:@abc", "#CA:,", "#TX:rest", "#ZZ:"}},
		{"@abc rest,", []string{"#VL:@abc", "#TX:rest", "#CA:,", "#ZZ:"}},
	} {
		if got := spans(c.src); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q -> %q, want %q", c.src, got, c.want)
		}
	}
}
