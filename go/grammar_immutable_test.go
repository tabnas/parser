// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// Grammar must not modify the *GrammarSpec it is given.
//
// Go has always behaved this way, but nothing pinned it. The TS runtime
// did the opposite until 2026-08-07: normalisation ran directly on the
// caller's object, so `@name$` ref strings were replaced by resolved
// functions and a declarative grammar came back full of code. Serialising
// after installing then produced a spec with every action missing, which
// still installed, still parsed, and returned nothing.
//
// The shared .tsv fixtures pin how grammars parse, not what the engine API
// does to its arguments, which is exactly why that divergence survived so
// long. These tests close that hole on the Go side; the TS counterpart is
// ts/test/grammar-immutable.test.js.

import (
	"encoding/json"
	"testing"
)

func immutableSpec() *GrammarSpec {
	return &GrammarSpec{
		OptionsMap: map[string]any{
			"fixed": map[string]any{"token": map[string]any{"#EQ": "="}},
			"rule":  map[string]any{"start": "pair"},
		},
		Rule: map[string]*GrammarRuleSpec{
			"pair": {
				Open:  []*GrammarAltSpec{{S: "#TX #EQ", P: "val", A: []any{"@object$", "@key$"}, G: "zulu,alpha"}},
				Close: []*GrammarAltSpec{{A: "@setval$"}},
			},
			"val": {
				Open:  []*GrammarAltSpec{{S: "#NR", A: "@value$"}},
				Close: []*GrammarAltSpec{{}},
			},
		},
	}
}

func parsedPort(t *testing.T, j *Tabnas) string {
	t.Helper()
	out, err := j.Parse("port = 8080")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// The caller's spec reads the same after Grammar as before it.
func TestGrammarLeavesCallerSpecIntact(t *testing.T) {
	gs := immutableSpec()

	openAlts, ok := gs.Rule["pair"].Open.([]*GrammarAltSpec)
	if !ok {
		t.Fatal("open alts not []*GrammarAltSpec")
	}
	alt := openAlts[0]

	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := parsedPort(t, j); `{"port":8080}` != got {
		t.Fatalf("parse = %s, want {\"port\":8080}", got)
	}

	// Ref strings stay strings — not resolved functions written back.
	refs, ok := alt.A.([]any)
	if !ok {
		t.Fatalf("A = %#v, want []any of ref strings", alt.A)
	}
	if 2 != len(refs) || "@object$" != refs[0] || "@key$" != refs[1] {
		t.Fatalf("A = %#v, want [@object$ @key$]", refs)
	}

	// G is not rewritten into a sorted slice on the caller's value.
	if "zulu,alpha" != alt.G {
		t.Fatalf("G = %q, want %q", alt.G, "zulu,alpha")
	}
}

// Installing the same spec value twice must give the same grammar both
// times. This is the case that failed silently in TS.
func TestGrammarInstallsSameSpecTwice(t *testing.T) {
	gs := immutableSpec()

	first := Make()
	if err := first.Grammar(gs); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if got := parsedPort(t, first); `{"port":8080}` != got {
		t.Fatalf("first parse = %s", got)
	}

	second := Make()
	if err := second.Grammar(gs); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if got := parsedPort(t, second); `{"port":8080}` != got {
		t.Fatalf("second parse = %s, want {\"port\":8080} — spec was consumed by the first install", got)
	}
}
