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
		t.Errorf("code: got %q, want invalid_unicode (the code agrees for "+
			"THIS input — see TestDivergenceTruncatedEscapeCodeDiverges "+
			"for the shapes where it does not)", je.Code)
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

// Escape decoding is strict in BOTH ports, and this asserts it here.
//
// This was a divergence pin. TypeScript decoded with `parseInt`, which
// succeeds on any hex prefix, so it accepted `"\x4Z"` as U+0004 —
// discarding the `Z` — and reported truncated escapes as
// unterminated_string where Go named the escape. Swept then: 32 cases,
// 16 diverged. The repair (P3) made TypeScript require the full
// fixed-width hex run, which is what Go already did; swept after: 0.
//
// Kept as a PARITY test rather than deleted, because the boundary is
// easy to relax again by accident — a plain `parseInt` is the obvious
// way to write this and it is the wrong way. ts/test/divergence.test.js
// asserts the same inputs.
func TestEscapeDecodeIsStrict(t *testing.T) {
	// A junk-terminated escape is not a valid escape.
	assertLexCode(t, `"\x4Z"`, "invalid_ascii",
		"the trailing Z must not be swallowed as part of the escape")
	assertLexCode(t, `"\u414Z"`, "invalid_unicode",
		"the trailing Z must not be swallowed as part of the escape")

	// Nor is a truncated one, with or without a closing quote.
	for _, c := range []struct{ src, code string }{
		{`"\x4`, "invalid_ascii"},
		{`"\x4"`, "invalid_ascii"},
		{`"\u4`, "invalid_unicode"},
		{`"\u41"`, "invalid_unicode"},
		{`"\u414Z`, "invalid_unicode"},
	} {
		assertLexCode(t, c.src, c.code,
			"a short hex run is not a complete escape")
	}

	// No valid hex prefix at all — unchanged by the repair.
	assertLexCode(t, `"\xZZ"`, "invalid_ascii", "no hex prefix")
	assertLexCode(t, `"\uZZZZ"`, "invalid_unicode", "no hex prefix")
}

func assertLexCode(t *testing.T, src, want, note string) {
	t.Helper()
	j := Make(Options{})
	lex := NewLex(src, j.Config())
	lex.Next()

	je, ok := lex.Err.(*TabnasError)
	if !ok || je == nil {
		t.Fatalf("%s: expected a *TabnasError lex error, got %v — %s",
			src, lex.Err, note)
	}
	if je.Code != want {
		t.Errorf("%s: code = %q, want %q (%s). Both ports decode escapes "+
			"strictly since P3; a plain parseInt-style decode reintroduces "+
			"the defect. See DIVERGENCE.md.", src, je.Code, want, note)
	}
}
