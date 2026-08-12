// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// GrammarSpecFromJSON is the cross-runtime door: every non-Go binding
// reaches the engine through it. A field it drops or coerces is not a
// cosmetic loss — it is this runtime accepting a language the canonical
// TypeScript runtime does not. These tests pin the fields where that
// divergence is possible.

import (
	"strings"
	"testing"
)

// `clear` wipes rules and fixed-token bindings before the rest of the
// spec applies. Dropping it leaves the engine's default bindings in
// place, which changes how the grammar tokenises — a silently different
// language, the one thing the house rule forbids.
func TestFromJSONPreservesClear(t *testing.T) {
	for _, c := range []struct {
		spec string
		want bool
	}{
		{`{"clear":true}`, true},
		{`{"clear":false}`, false},
		{`{}`, false},
		// TS compares against literal true (`true === gs.clear`), so a
		// non-boolean is not a clear request. Matching that exactly
		// matters more than being lenient here.
		{`{"clear":"yes"}`, false},
		{`{"clear":1}`, false},
	} {
		gs, err := GrammarSpecFromJSON([]byte(c.spec))
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		if gs.Clear != c.want {
			t.Errorf("%s: Clear = %v, want %v", c.spec, gs.Clear, c.want)
		}
	}
}

// A malformed version must be refused, not coerced. "999" silently
// becoming 0 disables the version gate altogether — the spec then loads
// on an engine that may not implement its schema at all.
func TestFromJSONRejectsMalformedVersion(t *testing.T) {
	for _, spec := range []string{
		`{"v":"999"}`, // not a number: would coerce to 0, gate skipped
		`{"v":2.5}`,   // not an integer: would truncate to a different schema
		`{"v":0}`,     // present but not positive; TS rejects
		`{"v":-1}`,
		`{"v":null}`,
		`{"v":true}`,
	} {
		gs, err := GrammarSpecFromJSON([]byte(spec))
		if err == nil {
			t.Errorf("%s: accepted, decoded V=%d; want a refusal", spec, gs.V)
			continue
		}
		if !strings.Contains(err.Error(), "invalid builtin schema version") {
			t.Errorf("%s: unhelpful error %q", spec, err)
		}
	}
}

func TestFromJSONKeepsValidVersion(t *testing.T) {
	// Absent means "current" and must stay 0, the engine's own sentinel.
	gs, err := GrammarSpecFromJSON([]byte(`{}`))
	if err != nil || gs.V != 0 {
		t.Fatalf("absent v: V=%d err=%v, want 0 and no error", gs.V, err)
	}

	gs, err = GrammarSpecFromJSON([]byte(`{"v":1}`))
	if err != nil || gs.V != 1 {
		t.Fatalf("v:1: V=%d err=%v", gs.V, err)
	}

	// A version this engine cannot serve still decodes; refusing it is
	// Grammar()'s job, and its message is the one users already know.
	gs, err = GrammarSpecFromJSON([]byte(`{"v":999}`))
	if err != nil {
		t.Fatalf("v:999 should decode, then be refused by Grammar: %v", err)
	}
	if err := Make().Grammar(gs); err == nil ||
		!strings.Contains(err.Error(), "supports up to") {
		t.Errorf("Grammar() should refuse v:999, got %v", err)
	}
}
