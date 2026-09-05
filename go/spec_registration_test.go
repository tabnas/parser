/* Copyright (c) 2026 Richard Rodger, MIT License */

// Every shared parity fixture must be executed by all three runtimes.
//
// The shared corpus only means anything if both ports actually run it. This
// repo has been on the wrong side of that: before the ownership split, most
// of test/spec/ was referenced by neither runner — files that looked like a
// cross-port corpus and measured nothing, reporting green throughout. The
// sibling gate in jsonic found 39 such fixtures there.
//
// This makes it a build failure: add a .tsv without wiring it into both
// ports and this test says so. Fixtures that are deliberately not parity
// fixtures go in nonParity with the reason, and that list is asserted to
// stay honest — an entry that IS referenced by all runners fails too, so
// an exemption cannot outlive its justification.

package tabnas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures that exist for a purpose other than cross-port parity.
var nonParity = map[string]string{
	"happy": "loader smoke-test fixture for ts/test/spec.test.js — this engine " +
		"ships no grammar, so its rows cannot be parsed here at all",
}

func TestSpecFixturesRunByAllRuntimes(t *testing.T) {
	specs, err := filepath.Glob(filepath.Join(specDir(), "*.tsv"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("cannot list %s: %v", specDir(), err)
	}

	goSrc := concatSources(t, ".", ".go")
	tsSrc := concatSources(t, filepath.Join("..", "ts", "test"), ".js")
	rustSrc := concatSources(t, filepath.Join("..", "rs", "tests"), ".rs")

	for _, p := range specs {
		name := strings.TrimSuffix(filepath.Base(p), ".tsv")
		inGo := strings.Contains(goSrc, `"`+name+`"`) || strings.Contains(goSrc, name+`.tsv`)
		inTS := strings.Contains(tsSrc, `'`+name+`'`) || strings.Contains(tsSrc, `"`+name+`"`) || strings.Contains(tsSrc, name+`.tsv`)
		inRust := strings.Contains(rustSrc, name+`.tsv`)

		if reason, ok := nonParity[name]; ok {
			if inGo && inTS {
				t.Errorf("%s is exempt as %q but IS referenced by both runners — "+
					"remove the exemption", name, reason)
			}
			continue
		}
		if !inGo {
			t.Errorf("%s.tsv is not referenced by any Go test "+
				"(wire it, or add a nonParity entry saying why not)", name)
		}
		if !inTS {
			t.Errorf("%s.tsv is not referenced by any TS test — a fixture only "+
				"one port runs is not a parity fixture", name)
		}
		if !inRust {
			t.Errorf("%s.tsv is not referenced by any Rust test — a fixture only " +
				"the mature ports run is not a three-runtime parity fixture", name)
		}
	}
}

// concatSources reads every file with the given suffix in dir. Reading the
// sources is deliberate: it catches a fixture referenced from ANY test, not
// just one registry function, so the gate does not have to know how each
// runner is organised.
func concatSources(t *testing.T, dir, suffix string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		// Skip this file: it names fixtures in nonParity, and counting
		// those as "references" would make every exemption self-justifying.
		if e.Name() == "spec_registration_test.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("cannot read %s: %v", e.Name(), err)
		}
		b.Write(data)
	}
	return b.String()
}
