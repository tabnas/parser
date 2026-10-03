/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// A write through Config() lasts only until the next SetOptions, which
// rebuilds the config from the merged options, copies the result over the
// live one and carries forward only the per-instance state the options
// cannot rebuild. The routes that survive a rebuild are the Options
// fields, Options.Ender and Property.ConfigModify, the Go form of
// TypeScript config.modify, which runs on every build. Grammar plugins
// kept writing hooks onto the live config and losing them to a later
// SetOptions (tabnas/ini#76, tabnas/yaml#81), because nothing said so and
// no engine test covered ConfigModify (#238).

import "testing"

// clHook is a text check that answers every text run with the value
// "hooked", so a parse shows whether the hook is installed.
func clHook(lex *Lex) *LexCheckResult {
	tkn := lex.Token("#TX", TinTX, "hooked", lex.Fwd(1))
	p := lex.Cursor()
	p.SI = p.Len
	return &LexCheckResult{Done: true, Token: tkn}
}

func TestConfigWritesLastUntilSetOptions(t *testing.T) {
	j := pmTopVal()
	cfg := j.Config()
	cfg.TextCheck = clHook
	cfg.EnderChars = map[rune]bool{'!': true}
	cfg.ParseBudgetN = 7
	if v, err := j.Parse("abc"); err != nil || "hooked" != v {
		t.Fatalf("hook written through Config(): got %v, %v; want hooked", v, err)
	}

	j.SetOptions(Options{})
	if cfg != j.Config() {
		t.Fatalf("SetOptions replaced the config pointer the grammar closures hold")
	}
	if cfg.TextCheck != nil || cfg.EnderChars['!'] || 0 != cfg.ParseBudgetN {
		t.Errorf("a write through Config() survived SetOptions: check %v, ender %v, budget %d",
			cfg.TextCheck != nil, cfg.EnderChars['!'], cfg.ParseBudgetN)
	}
	if v, err := j.Parse("abc"); err != nil || "abc" != v {
		t.Fatalf("after SetOptions: got %v, %v; want abc", v, err)
	}
}

// The routes that survive: a config modifier runs on every rebuild, and an
// option field is part of what every rebuild reads.
func TestConfigModifyRunsOnEveryRebuild(t *testing.T) {
	builds := 0
	j := pmTopVal()
	j.SetOptions(Options{
		Ender: []string{"!"},
		Property: &PropertyOptions{ConfigModify: map[string]ConfigModifier{
			"hook": func(cfg *LexConfig, _ *Options) {
				builds++
				cfg.TextCheck = clHook
			},
		}},
	})
	if 1 != builds {
		t.Fatalf("modifier ran %d times on the call that installed it, want 1", builds)
	}
	for i := 1; i <= 3; i++ {
		before := builds
		j.SetOptions(Options{})
		if builds <= before {
			t.Fatalf("empty SetOptions call %d did not run the modifier", i)
		}
	}
	cfg := j.Config()
	if cfg.TextCheck == nil || !cfg.EnderChars['!'] {
		t.Errorf("after three empty SetOptions calls: check %v, ender %v; want both",
			cfg.TextCheck != nil, cfg.EnderChars['!'])
	}
	if v, err := j.Parse("abc"); err != nil || "hooked" != v {
		t.Fatalf("hook installed by ConfigModify: got %v, %v; want hooked", v, err)
	}
}

// Two config fields share their storage with the options: StringReplace is
// the String.Replace map and TextModify the Text.Modify slice. A write into
// the map through Config() changes the options, so it outlasts the
// rebuild; assigning a new map to the field changes only the config, and
// the rebuild puts the options' map back.
func TestConfigWriteIntoAnOptionMapOutlivesTheRebuild(t *testing.T) {
	j := Make(Options{String: &StringOptions{Replace: map[rune]string{'a': "A"}}})
	j.Config().StringReplace['a'] = "B"
	j.SetOptions(Options{})
	if got := j.Config().StringReplace['a']; "B" != got {
		t.Errorf("a write into StringReplace answers %q after SetOptions, want B", got)
	}
	j.Config().StringReplace = map[rune]string{'a': "C"}
	j.SetOptions(Options{})
	if got := j.Config().StringReplace['a']; "B" != got {
		t.Errorf("a new StringReplace map answers %q after SetOptions, want the options' B", got)
	}
}

// ConfigModify runs inside the build, before the token sets are installed
// from Options.TokenSet and before SetOptions carries the live config's
// state over, so a modifier's write to a token set never takes, neither
// at construction nor after a rebuild.
func TestConfigModifyCannotSetTheTokenSets(t *testing.T) {
	j := Make(Options{Property: &PropertyOptions{ConfigModify: map[string]ConfigModifier{
		"vals": func(cfg *LexConfig, _ *Options) { cfg.ValSet = []Tin{TinZZ} },
	}}})
	for _, when := range []string{"after Make", "after SetOptions"} {
		if vs := j.Config().ValSet; 1 == len(vs) && TinZZ == vs[0] {
			t.Errorf("%s: the modifier's ValSet took: %v", when, vs)
		}
		j.SetOptions(Options{})
	}
}
