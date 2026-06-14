package tabnas

// Strict-escape mode (string.EscapeStrict) and escape-map removal.
// Mirrors ts/test/strict-escape.test.js — both runtimes must reject the
// same inputs with the same error codes, since the downstream strict-JSON
// grammar plugins assert on those shared codes.

import "testing"

// strParser builds a one-string grammar over the bare engine with the
// given string options, so each case exercises only escape handling.
func strParser(so *StringOptions) *Tabnas {
	j := Make(Options{Rule: &RuleOptions{Start: "top"}, String: so})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinST}}, A: func(r *Rule, ctx *Context) {
			r.Node = r.O0.ResolveVal(r, ctx)
		}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	return j
}

// parseResult returns ("", code) on error or (value, "") on success.
func parseResult(t *testing.T, j *Tabnas, src string) (string, string) {
	t.Helper()
	out, err := j.Parse(src)
	if err != nil {
		if te, ok := err.(*TabnasError); ok {
			return "", te.Code
		}
		t.Fatalf("Parse(%q): non-TabnasError %T", src, err)
	}
	s, _ := out.(string)
	return s, ""
}

func strictStringOpts() *StringOptions {
	f := false
	tr := true
	return &StringOptions{
		AllowUnknown: &f,
		EscapeStrict: &tr,
		Escape:       map[string]string{"v": "", "'": "", "`": ""},
	}
}

func TestStrictEscapeRejectsNonStandard(t *testing.T) {
	j := strParser(strictStringOpts())
	cases := map[string]string{
		`"\x41"`:   "unexpected",      // \x disabled → unknown escape
		`"\u{41}"`: "invalid_unicode", // braced disabled → '{' not hex
		`"\v"`:     "unexpected",      // removed built-in
		`"\'"`:     "unexpected",      // removed built-in
		"\"\\`\"":  "unexpected",      // removed built-in
	}
	for src, wantCode := range cases {
		if _, code := parseResult(t, j, src); code != wantCode {
			t.Errorf("Parse(%q): code = %q, want %q", src, code, wantCode)
		}
	}
}

func TestStrictEscapeAcceptsStandard(t *testing.T) {
	j := strParser(strictStringOpts())
	if v, code := parseResult(t, j, `"😀"`); code != "" || v != "😀" {
		t.Errorf(`Parse("😀"): v=%q code=%q`, v, code)
	}
	if v, code := parseResult(t, j, `"\n\t\"\\\/\b\f\r"`); code != "" || v != "\n\t\"\\/\b\f\r" {
		t.Errorf("standard escapes: v=%q code=%q", v, code)
	}
	if v, code := parseResult(t, j, `"A"`); code != "" || v != "A" {
		t.Errorf(`Parse("A"): v=%q code=%q`, v, code)
	}
}

func TestStrictEscapeDefaultUnchanged(t *testing.T) {
	j := strParser(nil) // engine defaults
	cases := map[string]string{
		`"\x41"`:   "A",
		`"\u{41}"`: "A",
		`"\v"`:     "\v",
		`"\'"`:     "'",
		"\"\\`\"":  "`",
	}
	for src, want := range cases {
		if v, code := parseResult(t, j, src); code != "" || v != want {
			t.Errorf("default Parse(%q): v=%q code=%q, want %q", src, v, code, want)
		}
	}
}

func TestStrictEscapeMapRemovalWithoutStrict(t *testing.T) {
	// Dropping \v via the escape map ("" sentinel) rejects it even with
	// strict mode off, as long as unknown escapes are disallowed.
	f := false
	j := strParser(&StringOptions{AllowUnknown: &f, Escape: map[string]string{"v": ""}})
	if _, code := parseResult(t, j, `"\v"`); code != "unexpected" {
		t.Errorf(`Parse("\v") with v removed: code=%q, want unexpected`, code)
	}
	// \x stays enabled (strict off).
	if v, code := parseResult(t, j, `"\x41"`); code != "" || v != "A" {
		t.Errorf(`Parse("\x41") strict off: v=%q code=%q`, v, code)
	}
}
