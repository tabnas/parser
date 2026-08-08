/* Copyright (c) 2026 Richard Rodger, MIT License */

// The baked-in VERSION must equal the package's declared version.
//
// This is the CI check for version drift. It exists because the constant HAS
// drifted in practice: jsonic-cli shipped 0.4.1 and 0.4.2 while its const sat
// at 0.4.0 (the release orchestrator's file discovery missed it), and
// @tabnas/json shipped a TS `Version` export reading 1.0.0 for several
// releases because nothing ever rewrote it. Both were invisible until someone
// read the file. A release that bumps package.json and forgets the constant
// now fails here instead of shipping a lie.

package tabnas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionMatchesPackageJSON(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "ts", "package.json"))
	if err != nil {
		// Deliberately fatal, never skipped: a version check that silently
		// does not run is the failure mode this test exists to prevent.
		t.Fatalf("cannot read ts/package.json, so VERSION cannot be checked: %v", err)
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("ts/package.json is not readable JSON: %v", err)
	}
	if pkg.Version == "" {
		t.Fatal("ts/package.json has no version field")
	}
	if VERSION != pkg.Version {
		t.Errorf("VERSION drift: go VERSION = %q but %s package.json = %q.\n"+
			"Both are rewritten by admin/publish.sh at release; if you bumped one by "+
			"hand, bump the other.", VERSION, pkg.Name, pkg.Version)
	}
}
