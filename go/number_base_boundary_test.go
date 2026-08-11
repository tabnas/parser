/* Copyright (c) 2026 tabnas, MIT License */

package tabnas

import (
	"fmt"
	"testing"
)

// A base-prefixed run (0x…, 0o…, 0b…) is a COMPLETE integer. It ends at a
// registered fixed token, and no fraction or exponent tail may attach to it.
//
// Go already behaved this way — matchNumber's base-prefix branch consumes the
// prefix plus its digits and returns, with no tail — so nothing in lexer.go
// changed for this. These cases exist to PIN that, because the TS port had the
// fraction and exponent groups outside its regex alternation and so declined
// the whole span for `0xFF.5` (NaN), yielding #TX where Go yields #NR. The two
// ports disagreed on the token stream for identical input.
//
// The follow character decides whether that is observable:
//
//	0xFF.5   TS: fraction attaches -> NaN -> declined -> #TX   DIVERGED
//	0xFF.a   `.a` cannot open a fraction, so TS backtracked
//	         to `0xFF` and `.` served as the ender      -> #NR  AGREED
//
// A suite probing only `0xFF.x` or `0xFF.5x` passes against the broken port —
// which is how this survived: a downstream corpus covered only agreeing
// shapes. Every prefix below therefore carries at least one DIVERGING follow
// and at least one AGREEING one. Keep both halves.
//
// Mirrored by ts/test/number-base-boundary.test.js over the same table.

// stream renders the token stream as NAME("src") per token — the two ports are
// compared on the STREAM, not just decoded values, so a divergence that
// changes only the split stays visible.
func lexStream(t *testing.T, src string) string {
	t.Helper()
	j := Make(Options{})
	j.Token("#DT", ".")
	lex := NewLex(src, j.Config())
	out := ""
	for i := 0; i < 20; i++ {
		tkn := lex.Next()
		if tkn == nil || tkn.Tin == TinZZ || tkn.Tin == TinBD {
			break
		}
		if out != "" {
			out += " "
		}
		out += fmt.Sprintf("%s(%q)", tkn.Name, tkn.Src)
	}
	return out
}

func TestNumberBaseBoundaryDivergingFollows(t *testing.T) {
	// The shapes that used to decline in TS and lex here.
	cases := [][2]string{
		{"0xFF.5", `#NR("0xFF") #DT(".") #NR("5")`},
		{"0xFF.0", `#NR("0xFF") #DT(".") #NR("0")`},
		{"0xFF.", `#NR("0xFF") #DT(".")`},
		{"0xFF.e5", `#NR("0xFF") #DT(".") #TX("e5")`},
		{"0xFF.5e3", `#NR("0xFF") #DT(".") #NR("5e3")`},
		{"0x1_F.5", `#NR("0x1_F") #DT(".") #NR("5")`},
		{"0xFF.5_0", `#NR("0xFF") #DT(".") #NR("5_0")`},

		{"0o17.5", `#NR("0o17") #DT(".") #NR("5")`},
		{"0o17.0", `#NR("0o17") #DT(".") #NR("0")`},
		{"0o17.", `#NR("0o17") #DT(".")`},
		{"0o17.e5", `#NR("0o17") #DT(".") #TX("e5")`},

		{"0b101.5", `#NR("0b101") #DT(".") #NR("5")`},
		{"0b101.0", `#NR("0b101") #DT(".") #NR("0")`},
		{"0b101.", `#NR("0b101") #DT(".")`},
		{"0b101.e5", `#NR("0b101") #DT(".") #TX("e5")`},
	}
	for _, c := range cases {
		if got := lexStream(t, c[0]); got != c[1] {
			t.Errorf("diverging follow %q:\n got  %s\n want %s", c[0], got, c[1])
		}
	}
}

func TestNumberBaseBoundaryAgreeingFollows(t *testing.T) {
	// These agreed across ports even with the TS bug present, by regex
	// backtracking rather than by intent. They pin that nothing changed —
	// on their own they are NOT evidence the divergence is fixed.
	cases := [][2]string{
		{"0xFF.a", `#NR("0xFF") #DT(".") #TX("a")`},
		{"0xFF.F", `#NR("0xFF") #DT(".") #TX("F")`},
		{"0xFF.x", `#NR("0xFF") #DT(".") #TX("x")`},
		{"0xFF.z", `#NR("0xFF") #DT(".") #TX("z")`},
		{"0xFF.5x", `#NR("0xFF") #DT(".") #TX("5x")`},
		{"0xFF.a5", `#NR("0xFF") #DT(".") #TX("a5")`},

		{"0o17.x", `#NR("0o17") #DT(".") #TX("x")`},
		{"0o17.5x", `#NR("0o17") #DT(".") #TX("5x")`},

		{"0b101.x", `#NR("0b101") #DT(".") #TX("x")`},
		{"0b101.5x", `#NR("0b101") #DT(".") #TX("5x")`},
	}
	for _, c := range cases {
		if got := lexStream(t, c[0]); got != c[1] {
			t.Errorf("agreeing follow %q:\n got  %s\n want %s", c[0], got, c[1])
		}
	}
}

func TestNumberBaseBoundaryDecimalTailIntact(t *testing.T) {
	// Scoping the tail to the decimal alternative must not scope it away
	// entirely: `.` is a fixed token here, so a lost fraction group would
	// split these into three tokens.
	cases := [][2]string{
		{"1.5", `#NR("1.5")`},
		{"1.5e3", `#NR("1.5e3")`},
		{"00.5", `#NR("00.5")`},
		{"1.", `#NR("1.")`},
		{"1e3", `#NR("1e3")`},
		{"1_0.5", `#NR("1_0.5")`},
		{"-1.5", `#NR("-1.5")`},
		{"+1.5e-3", `#NR("+1.5e-3")`},
	}
	for _, c := range cases {
		if got := lexStream(t, c[0]); got != c[1] {
			t.Errorf("decimal tail %q:\n got  %s\n want %s", c[0], got, c[1])
		}
	}
}

func TestNumberBaseBoundaryNonNumbersStayText(t *testing.T) {
	// `0x` with no digits is not a number and must stay text in both ports,
	// as must a run that merely looks prefix-like.
	cases := [][2]string{
		{"0x.5", `#TX("0x") #DT(".") #NR("5")`},
		{"0z.5", `#TX("0z") #DT(".") #NR("5")`},
		{"x0.5", `#TX("x0") #DT(".") #NR("5")`},
		{"abc.def", `#TX("abc") #DT(".") #TX("def")`},
		{"0xFF", `#NR("0xFF")`},
	}
	for _, c := range cases {
		if got := lexStream(t, c[0]); got != c[1] {
			t.Errorf("text boundary %q:\n got  %s\n want %s", c[0], got, c[1])
		}
	}
}
