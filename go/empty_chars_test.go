// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package tabnas

import "testing"

// TestEmptyCharsMeansUnset pins what StringOptions.Chars can and cannot
// say, because the documentation for it is otherwise the only record and
// prose cannot be executed.
//
// Chars is a plain string, so "" is its zero value and an explicitly
// empty Chars is indistinguishable from an unset one. The default quote
// characters stay in force. TypeScript CAN say it — `{string:{chars:”}}`
// distinguishes ” from undefined — so a plugin ported field-for-field
// diverges silently, which is what happened to @tabnas/css (see #24
// there, and StringOptions.Chars here).
//
// This asserts the CURRENT behaviour, deliberately. If Chars becomes a
// *string, as the comment on it proposes, this test fails — which is the
// signal to delete it and the comment together, not a nuisance.
func TestEmptyCharsMeansUnset(t *testing.T) {
	cfg := Make(Options{String: &StringOptions{Chars: ""}}).Config()

	for _, q := range []rune{'\'', '"', '`'} {
		if !cfg.StringChars[q] {
			t.Errorf("quote %q was dropped: an empty Chars now means "+
				"NONE rather than UNSET, so StringOptions.Chars's "+
				"comment and this test are both out of date", q)
		}
	}

	// And the documented way to actually turn it off still works, so the
	// comment's advice is executable rather than merely stated.
	off := false
	cfgOff := Make(Options{String: &StringOptions{Lex: &off}}).Config()
	if cfgOff.StringLex {
		t.Error("Lex:false did not disable string lexing, so the " +
			"workaround the docs point at is not a workaround")
	}
}
