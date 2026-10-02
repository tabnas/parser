// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import "testing"

// Number options without a Sep keep the `_` digit separator (#241).
//
// TypeScript deep-merges `number: {...}` over the defaults, so
// `make({number: {lex: true}})` keeps `sep: '_'` and reads `1_000` as
// 1000; only `sep: null` (or `”`) switches the separator off. The Go
// overlay reset NumberSep to none whenever Number was non-nil and Sep
// was empty, so turning number lexing on, or installing a number
// check, silently disabled `1_000` in Go only. Measured against
// ts/dist with `new Tabnas({number: {lex: true}})('a:1_000,b:2')`
// => {"a":1000,"b":2}.
func TestNumberOptionsWithoutSepKeepSeparator(t *testing.T) {
	tr := true
	cases := []struct {
		name string
		opts Options
	}{
		{"lex", Options{Number: &NumberOptions{Lex: &tr}}},
		{"check", Options{Number: &NumberOptions{Check: func(lex *Lex) *LexCheckResult { return nil }}}},
		{"hex-false", Options{Number: &NumberOptions{Hex: Bool(false)}}},
		{"exclude", Options{Number: &NumberOptions{Exclude: func(s string) bool { return false }}}},
	}
	for _, c := range cases {
		t.Run("make/"+c.name, func(t *testing.T) {
			assertNumberSep(t, Make(c.opts), true)
		})
		t.Run("setoptions/"+c.name, func(t *testing.T) {
			assertNumberSep(t, Make().SetOptions(c.opts), true)
		})
	}
}

// An explicit empty separator still switches `_` off, the spelling the
// strict-JSON grammars use (TS `sep: null`), and a later unrelated
// Number option keeps that base value, as TS keeps a merged `sep: null`.
func TestNumberOptionsSepOffStaysOff(t *testing.T) {
	tr := true
	j := Make(Options{Number: &NumberOptions{Sep: String("")}})
	assertNumberSep(t, j, false)
	j.SetOptions(Options{Number: &NumberOptions{Lex: &tr}})
	assertNumberSep(t, j, false)
}

// A separator other than `_` is honoured, and a later option without
// a Sep keeps it.
func TestNumberOptionsSepCustom(t *testing.T) {
	tr := true
	j := Make(Options{Number: &NumberOptions{Sep: String("~")}})
	if tok := NewLex("1~000 ", j.parser.Config).Next(); tok.Tin != TinNR || tok.Val != float64(1000) {
		t.Errorf("custom separator: got %s %#v, want #NR 1000", tok.Name, tok.Val)
	}
	assertNumberSep(t, j, false) // `_` is then text, as in TS
	j.SetOptions(Options{Number: &NumberOptions{Lex: &tr}})
	if j.parser.Config.NumberSep != '~' {
		t.Errorf("NumberSep after an unrelated SetOptions = %q, want '~'", j.parser.Config.NumberSep)
	}
}

// The serialized door: `{"number":{"lex":true}}` keeps the separator,
// `{"number":{"sep":null}}` switches it off, as TS `sep: null` does.
func TestMapToOptionsNumberSep(t *testing.T) {
	keep := MapToOptions(map[string]any{"number": map[string]any{"lex": true}})
	if keep.Number == nil || keep.Number.Sep != nil {
		t.Errorf("lex alone: Sep = %v, want nil (not supplied)", keep.Number.Sep)
	}
	assertNumberSep(t, Make(keep), true)

	off := MapToOptions(map[string]any{"number": map[string]any{"sep": nil}})
	if off.Number == nil || off.Number.Sep == nil || *off.Number.Sep != "" {
		t.Errorf("sep null: Sep = %v, want pointer to \"\"", off.Number.Sep)
	}
	assertNumberSep(t, Make(off), false)

	on := MapToOptions(map[string]any{"number": map[string]any{"sep": "'"}})
	if on.Number == nil || on.Number.Sep == nil || *on.Number.Sep != "'" {
		t.Errorf("sep string: Sep = %v, want pointer to \"'\"", on.Number.Sep)
	}
	if cfg := buildConfig(&on); cfg.NumberSep != '\'' {
		t.Errorf("sep string: NumberSep = %q", cfg.NumberSep)
	}
}

func assertNumberSep(t *testing.T, j *Tabnas, on bool) {
	t.Helper()
	assertLexNumberSep(t, j.parser.Config, on)
}

// The engine ships no grammar, so the lexer is driven directly, as
// ts/test/lex.test.js does: with the separator on `1_000` is one #NR
// 1000, with it off the run is #TX "1_000" in both runtimes.
func assertLexNumberSep(t *testing.T, cfg *LexConfig, on bool) {
	t.Helper()
	tok := NewLex("1_000 ", cfg).Next()
	if on {
		if tok.Tin != TinNR || tok.Val != float64(1000) {
			t.Errorf("got %s %#v, want #NR 1000 (NumberSep=%q)", tok.Name, tok.Val, cfg.NumberSep)
		}
	} else if tok.Tin != TinTX || tok.Val != "1_000" {
		t.Errorf("got %s %#v, want #TX \"1_000\" (NumberSep=%q)", tok.Name, tok.Val, cfg.NumberSep)
	}
}
