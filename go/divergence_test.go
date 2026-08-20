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
	"encoding/json"
	"reflect"
	"testing"
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

// String errors are positioned ON the offending construct, in both ports.
//
// This was a divergence pin: Go left the lex point at the opening quote
// and spanned from it, so every error from this matcher reported 1:1 and
// carried the whole string-so-far as its token src, while TypeScript
// moved the point onto the escape (or the offending character) and
// spanned just that. Repaired by mirroring TS's `pnt.sI = sI; pnt.cI =
// cI` at all five raise sites; swept 19 inputs across the family, 0
// diverge.
//
// Kept as a PARITY test rather than deleted: the point-moving is easy to
// drop when editing this matcher, and dropping it is silent — the codes
// stay right and only the positions move. ts/test/divergence.test.js
// asserts the same inputs.
func TestStringErrorsPointAtTheConstruct(t *testing.T) {
	for _, c := range []struct {
		src  string
		code string
		col  int
		tsrc string
	}{
		// Escape errors sit on the BACKSLASH and span the escape.
		{`"\uZZZZ"`, "invalid_unicode", 2, `\uZZZZ`},
		{`"\xZZ"`, "invalid_ascii", 2, `\xZZ`},
		{`"\u{GG}"`, "invalid_unicode", 2, `\u{GG}`},
		// An unknown escape sits on the escape CHARACTER, as TS does.
		{`"\q"`, "unexpected", 3, `q`},
		// A control char sits on the character itself.
		{"\"a\nb\"", "unprintable", 3, "\n"},
		// A truncated escape at EOF spans the partial digits too — `sI`
		// is where they START, so ending the span there drops them.
		{`"\x4`, "invalid_ascii", 2, `\x4`},
		{`"\u41`, "invalid_unicode", 2, `\u41`},
		{`"\u{42`, "invalid_unicode", 2, `\u{42`},
	} {
		// allowUnknown off, or `\q` is simply accepted and there is no
		// error to position. The TypeScript twin sets the same option.
		no := false
		j := Make(Options{String: &StringOptions{AllowUnknown: &no}})
		lex := NewLex(c.src, j.Config())
		lex.Next()
		je, ok := lex.Err.(*TabnasError)
		if !ok || je == nil {
			t.Fatalf("%s: expected a *TabnasError, got %v", c.src, lex.Err)
		}
		if je.Code != c.code {
			t.Errorf("%s: code = %q, want %q", c.src, je.Code, c.code)
		}
		if je.Col != c.col {
			t.Errorf("%s: col = %d, want %d — the point must sit on the "+
				"construct, not the opening quote", c.src, je.Col, c.col)
		}
		if je.Src != c.tsrc {
			t.Errorf("%s: token src = %q, want %q — the span must cover the "+
				"construct, not the string so far", c.src, je.Src, c.tsrc)
		}
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

// The escape-removed and strict-\x branches raise from a DIFFERENT place
// than the default unknown-escape branch, and need the same positioning.
// They are reachable only under those options, so the sweep over default
// options could not see them.
//
// The span is the whole RUNE: an escaped non-ASCII character is more than
// one byte, and half of one leaves invalid UTF-8 in the diagnostic.
// ts/test/divergence.test.js asserts the same three.
func TestStringErrorsPointAtTheConstructUnderOptions(t *testing.T) {
	no, yes := false, true
	for _, c := range []struct {
		label string
		src   string
		opts  Options
		col   int
		tsrc  string
	}{
		{"strict disables \\x", `"\x41"`,
			Options{String: &StringOptions{EscapeStrict: &yes, AllowUnknown: &no}}, 3, "x"},
		{"escape map removes \\v", `"\v"`,
			Options{String: &StringOptions{Escape: map[string]string{"v": ""}, AllowUnknown: &no}}, 3, "v"},
		{"non-ASCII escape char", "\"\\é\"",
			Options{String: &StringOptions{AllowUnknown: &no}}, 3, "é"},
	} {
		j := Make(c.opts)
		lex := NewLex(c.src, j.Config())
		lex.Next()
		je, ok := lex.Err.(*TabnasError)
		if !ok || je == nil {
			t.Fatalf("%s: expected a *TabnasError, got %v", c.label, lex.Err)
		}
		if je.Code != "unexpected" {
			t.Errorf("%s: code = %q, want unexpected", c.label, je.Code)
		}
		if je.Col != c.col {
			t.Errorf("%s: col = %d, want %d", c.label, je.Col, c.col)
		}
		if je.Src != c.tsrc {
			t.Errorf("%s: token src = %q, want %q", c.label, je.Src, c.tsrc)
		}
	}
}

// `pos` is emitted in RUNES, so it agrees with TypeScript throughout the
// BMP and carries only the astral divergence — the same class as `col`.
//
// It used to be a BYTE offset, which diverged for every character above
// U+007F while both DIVERGENCE.md and schema/diagnostic.schema.json
// described runes. Audit item P5.
//
// ts/test/divergence.test.js asserts the TypeScript numbers for the same
// four inputs; the astral row is the only one where they differ.
func TestDiagnosticPosCountsRunes(t *testing.T) {
	for _, c := range []struct {
		src string
		pos int
		ts  int
	}{
		{`"ab" 1`, 5, 5}, // pure ASCII
		{`"é" 1`, 4, 4},  // 2 bytes, 1 rune, 1 UTF-16 unit
		{`"€" 1`, 4, 4},  // 3 bytes, 1 rune, 1 UTF-16 unit
		{`"😀" 1`, 4, 5},  // 4 bytes, 1 rune, TWO UTF-16 units
	} {
		j := Make(Options{Rule: &RuleOptions{Start: "top", Exclude: "tabnas,imp"}})
		j.Rule("top", func(rs *RuleSpec, p *Parser) {
			rs.AddOpen(&AltSpec{S: [][]Tin{{TinST}},
				A: func(r *Rule, ctx *Context) { r.Node = r.O0.Val }})
			rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
		})
		_, err := j.Parse(c.src)
		if err == nil {
			t.Fatalf("%s: expected a parse error", c.src)
		}
		b, mErr := json.Marshal(err)
		if mErr != nil {
			t.Fatalf("%s: marshal: %v", c.src, mErr)
		}
		var o struct {
			Pos int `json:"pos"`
		}
		if uErr := json.Unmarshal(b, &o); uErr != nil {
			t.Fatalf("%s: unmarshal: %v", c.src, uErr)
		}
		if o.Pos != c.pos {
			t.Errorf("%s: pos = %d, want %d (runes). A byte offset would "+
				"report the character's UTF-8 width instead.", c.src, o.Pos, c.pos)
		}
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
