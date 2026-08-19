// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// One test per entry in DIVERGENCE.md, the record of deliberate TS/Go
// non-parity. Mirrored by ts/test/divergence.test.js, which asserts the
// OPPOSITE where the ports differ — that is the point of the pair.
//
// WHY EVERY PERMITTED DIVERGENCE NEEDS A TEST
//
// A divergence that is written down but not executed can change in either
// direction without anyone noticing: it can regress further, or it can be
// quietly FIXED and leave the document lying. Both have happened in this
// project. jsonic's prose said `2.e3` and `1e999` still diverged for some
// time after they had been aligned, and said base-prefixed overflow was
// aligned before it was; that is what moved jsonic to an executable
// ledger. Prose rots silently because nothing runs it.
//
// So an entry in DIVERGENCE.md without a test here is an unbacked claim.
// If one of these tests fails, do not "fix" the test: either the
// divergence moved and the document is now wrong, or it was resolved and
// the entry should be deleted.
//
// Lone surrogates — the third entry — are pinned in
// surrogate_pairing_test.go (TestSurrogateLoneFoldsToReplacement) rather
// than duplicated here, next to the pairing cases they are easily confused
// with.

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

// TestDivergenceAstralColumnIsOneRune pins: "Error columns count UTF-16
// units in TypeScript (an astral character is 2) and runes in Go (any
// character is 1)."
//
// Measured on the token AFTER the character, which is where the difference
// becomes visible. The ports emit different token sequences for the same
// source (Go does not emit #SP here), so comparing "the third token" would
// compare different tokens — the ASCII control row is what keeps this
// honest, since both ports agree on it.
func TestDivergenceAstralColumnIsOneRune(t *testing.T) {
	col := func(src string) int {
		j := Make(Options{})
		lex := NewLex(src, j.Config())
		for i := 0; i < 6; i++ {
			tk := lex.Next()
			if tk == nil || tk.Tin == TinZZ {
				break
			}
			if tk.Src == "y" {
				return tk.CI
			}
		}
		t.Fatalf("no `y` token in %q", src)
		return -1
	}

	// Control: one ASCII char then a space puts `y` in column 3. Both ports
	// agree here, so a change to this row means something else broke.
	if got := col("x y"); got != 3 {
		t.Errorf("ascii control: y at column %d, want 3", got)
	}

	// The divergence: an astral character is ONE rune to Go, so `y` stays
	// in column 3. TypeScript counts two UTF-16 units and reports 4.
	if got := col("\U0001F600 y"); got != 3 {
		t.Errorf("astral: y at column %d, want 3 (Go counts runes); "+
			"TypeScript reports 4 — see DIVERGENCE.md", got)
	}
}

// TestDivergenceNoRelexOption pins: "Negotiated lexing (lex.relex) —
// TypeScript only".
//
// The divergence is a feature's ABSENCE, so the assertion is that nothing
// in the Go config surface accepts it. If Go ever gains relex, this fails
// and the DIVERGENCE.md entry must go — which is the intended signal, not
// a nuisance.
//
// Asserted structurally rather than behaviourally on purpose: the entry
// says the practical impact is confined to scannerless front-ends, so for
// every grammar in this fleet the two ports already behave identically.
// A behavioural test would therefore pass in both ports and pin nothing.
func TestDivergenceRelexOptionExists(t *testing.T) {
	j := Make(Options{})
	cfg := j.Config()

	// Guard first: a presence assertion is worthless if the detector sees
	// everything. Confirm hasField rejects a name that certainly does not
	// exist, so a reflection mistake cannot turn the check below into a
	// vacuous pass. (Its predecessor was an ABSENCE assertion, and nearly
	// went vacuous the other way — an earlier sanity check used the
	// TypeScript field names and "failed".)
	for _, known := range []string{"NumberLex", "StringLex", "EscapeChar"} {
		if !hasField(cfg, known) {
			t.Fatalf("hasField cannot see known LexConfig field %q", known)
		}
	}
	if hasField(cfg, "NoSuchFieldAtAll") {
		t.Fatal("hasField sees a field that does not exist, so the " +
			"assertion below would pass vacuously")
	}

	// Negotiated lexing is ported: LexConfig carries Relex, and it is
	// readable back off the resolved config, which is how @tabnas/gbnf
	// probes for support. Behaviour is covered by relex_test.go; this
	// keeps DIVERGENCE.md honest about the entry no longer applying.
	if !hasField(cfg, "Relex") {
		t.Error("LexConfig lost its Relex field: negotiated lexing is a " +
			"ported feature, not a divergence")
	}
}

// hasField reports whether v (or the struct it points to, recursively
// through embedded pointers one level) declares a field named name.
func hasField(v any, name string) bool {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	rt := rv.Type()
	if _, ok := rt.FieldByName(name); ok {
		return true
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rv.Field(i)
		if f.Kind() == reflect.Ptr && !f.IsNil() && f.Elem().Kind() == reflect.Struct {
			if _, ok := f.Elem().Type().FieldByName(name); ok {
				return true
			}
		}
	}
	return false
}

// TestDivergenceBadEscapeSpanIncludesQuote pins: "Bad-token spans for
// invalid string escapes". This port's string matcher reports the string
// from its opening quote up to the bad escape; TypeScript reports the
// offending escape sequence itself. Same code (the contract), different
// span — so the structured diagnostic's len/pos/col diverge on this path
// even for pure-ASCII input. ts/test/divergence.test.js asserts the
// OPPOSITE values on purpose.
func TestDivergenceBadEscapeSpanIncludesQuote(t *testing.T) {
	j := Make(Options{})
	lex := NewLex(`"\uZZZZ"`, j.Config())
	lex.Next()

	je, ok := lex.Err.(*TabnasError)
	if !ok || je == nil {
		t.Fatalf("expected a *TabnasError lex error, got %v", lex.Err)
	}
	if je.Code != "invalid_unicode" {
		t.Errorf("code: got %q, want invalid_unicode (the code is shared — "+
			"only the span diverges)", je.Code)
	}
	if je.Src != `"\uZZZZ` {
		t.Errorf("token src: got %q, want quote-to-escape — TS reports the "+
			"escape alone", je.Src)
	}
	if je.Pos != 0 {
		t.Errorf("pos: got %d, want 0 (the quote) — TS reports 1", je.Pos)
	}
	if je.Col != 1 {
		t.Errorf("col: got %d, want 1 (the quote) — TS reports 2", je.Col)
	}
	if n := utf8.RuneCountInString(je.Src); n != 7 {
		t.Errorf("diagnostic len (code points of token src): got %d, want 7 "+
			"— TS reports 6", n)
	}
}

// TestDivergenceRegexDialect pins the serialized-regex dialect gap, asserting
// the OPPOSITE of ts/test/divergence.test.js: that pairing IS the test. See
// DIVERGENCE.md, "Regex dialect in serialized terminals".
//
// This is the first PARSE-LEVEL reproduction of it. The gap was known at the
// regex-engine layer and recorded in go/doc/differences.md, but nothing drove
// it through a real GrammarSpec — so "a shared grammar that depends on either
// will differ" was a prediction, not a measurement. It goes both ways: Go
// REJECTS what TS accepts (`\s`) and ACCEPTS what TS rejects (`(?i)`).
func TestDivergenceRegexDialect(t *testing.T) {
	run := func(spec, src string) string {
		gs, err := GrammarSpecFromJSON([]byte(spec))
		if nil != err {
			t.Fatalf("spec: %v", err)
		}
		j := Make(Options{Rule: &RuleOptions{Start: "top"}})
		if err := j.Grammar(gs); nil != err {
			t.Fatalf("grammar: %v", err)
		}
		v, perr := j.Parse(src)
		if nil != perr {
			return "REJECTED"
		}
		s, _ := v.(string)
		return "ACCEPTED:" + s
	}

	const specWS = `{"options":{"rule":{"start":"top"},"match":{"token":{"#WS":"@/^\\s+/"}}},` +
		`"rule":{"top":{"open":[{"s":["#WS"],"a":"@value$"}],"close":[{}]}}}`
	const specK = `{"options":{"rule":{"start":"top"},"match":{"token":{"#K":"@/^k/i"}}},` +
		`"rule":{"top":{"open":[{"s":["#K"],"a":"@value$"}],"close":[{}]}}}`

	// Control first: the ASCII whitespace RE2 and JS agree on. A change here
	// means something other than the dialect gap broke.
	for _, cp := range []rune{0x20, 0x09} {
		if got := run(specWS, string(cp)); "ACCEPTED:"+string(cp) != got {
			t.Errorf("\\s control U+%04X: got %s, want ACCEPTED", cp, got)
		}
	}

	// RE2's `\s` is the Perl class [\t\n\f\r ]. JS's is Unicode-aware, so TS
	// ACCEPTS every one of these. Pinned by name so a future RE2 that widened
	// the class fails loudly rather than quietly aligning.
	for _, c := range []struct {
		n  string
		cp rune
	}{
		{"NBSP", 0x00A0}, {"LINE SEPARATOR", 0x2028}, {"EN QUAD", 0x2000},
		{"IDEOGRAPHIC SPACE", 0x3000}, {"ZERO WIDTH NO-BREAK SPACE", 0xFEFF},
	} {
		if got := run(specWS, string(c.cp)); "REJECTED" != got {
			t.Errorf("\\s U+%04X (%s): got %s, want REJECTED — TS accepts it; "+
				"if Go now accepts it too the divergence is GONE and the "+
				"DIVERGENCE.md entry should be deleted", c.cp, c.n, got)
		}
	}

	// The other direction. RE2 case-folds by Unicode rules, so `(?i)k` matches
	// U+212A KELVIN SIGN; JS `/i` without `u` does not, and TS rejects it.
	for _, s := range []string{"k", "K"} {
		if got := run(specK, s); "ACCEPTED:"+s != got {
			t.Errorf("(?i) control %q: got %s, want ACCEPTED", s, got)
		}
	}
	if got := run(specK, string(rune(0x212A))); "REJECTED" == got {
		t.Error("(?i) U+212A KELVIN SIGN: got REJECTED, want ACCEPTED — TS " +
			"rejects it; if Go now rejects it too the divergence is GONE")
	}

	// THE WORKAROUND, pinned rather than only written down. DIVERGENCE.md
	// recommends an explicit class instead of `\s`, and the recommendation is
	// worthless unless it works in BOTH runtimes: the first draft spelled it
	// the RE2 way (\x{00a0}), which is a SyntaxError in TypeScript. This is
	// the same assertion as its TS counterpart — not the opposite — because
	// the whole point is that the two agree here.
	const cls = `[\\t\\n\\v\\f\\r \\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000\\ufeff]+`
	specCls := `{"options":{"rule":{"start":"top"},"match":{"token":{"#WS":"@/^` + cls + `/"}}},` +
		`"rule":{"top":{"open":[{"s":["#WS"],"a":"@value$"}],"close":[{}]}}}`
	for _, cp := range []rune{0x20, 0x09, 0x00A0, 0x2028, 0x2000, 0x3000, 0xFEFF} {
		if got := run(specCls, string(cp)); "ACCEPTED:"+string(cp) != got {
			t.Errorf("workaround class U+%04X: got %s, want ACCEPTED — the "+
				"class DIVERGENCE.md recommends must work in both runtimes", cp, got)
		}
	}
	if got := run(specCls, "A"); "REJECTED" != got {
		t.Errorf("workaround class %q: got %s, want REJECTED", "A", got)
	}

	// A HARSHER KIND OF DIVERGENCE: not a different result, but a grammar
	// that will not load at all. RE2 implements neither lookahead nor
	// backreferences (both need backtracking), so a spec written and tested
	// against TypeScript can be unloadable here.
	for _, c := range []struct{ name, pattern string }{
		{"lookahead", `(?=x)x`},
		{"backreference", `(a)\\1`},
	} {
		spec := `{"options":{"rule":{"start":"top"},"match":{"token":{"#WS":"@/^` +
			c.pattern + `/"}}},` +
			`"rule":{"top":{"open":[{"s":["#WS"],"a":"@value$"}],"close":[{}]}}}`
		gs, err := GrammarSpecFromJSON([]byte(spec))
		if nil != err {
			t.Fatalf("%s spec: %v", c.name, err)
		}
		j := Make(Options{Rule: &RuleOptions{Start: "top"}})
		if err := j.Grammar(gs); nil == err {
			t.Errorf("%s (%s): installed, want an install error — TS installs "+
				"and matches it. If Go installs it too the divergence is GONE",
				c.name, c.pattern)
		}
	}
}
