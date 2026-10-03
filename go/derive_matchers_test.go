package tabnas

import "testing"

// Issue #242: Derive built the child with Make(o) from the merged options,
// which already registers every lex.match matcher, and then appended the
// parent's CustomMatchers again. TypeScript rebuilds cfg.lex.match from the
// merged options alone, so a matcher appears once per name in every
// generation. The count must stay constant across generations.
func TestDeriveCarriesEachCustomMatcherOnce(t *testing.T) {
	mk := func(cfg *LexConfig, opts *Options) LexMatcher {
		return func(lex *Lex, rule *Rule) *Token { return nil }
	}
	m := Make(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"mine":  {Order: 1, Make: mk},
		"later": {Order: 9000000, Make: mk},
	}}})
	c1, err := m.Derive()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := c1.Derive()
	if err != nil {
		t.Fatal(err)
	}
	for i, j := range []*Tabnas{m, c1, c2} {
		ms := j.Config().CustomMatchers
		if len(ms) != 2 {
			t.Errorf("generation %d: %d custom matchers, want 2", i, len(ms))
		}
		seen := map[string]bool{}
		for k, e := range ms {
			if seen[e.Name] {
				t.Errorf("generation %d: matcher %q registered twice", i, e.Name)
			}
			seen[e.Name] = true
			if 0 < k && ms[k-1].Priority > e.Priority {
				t.Errorf("generation %d: matchers out of priority order", i)
			}
		}
	}
}

// A matcher registered after construction through SetOptions is inherited
// by the child exactly once too: it is in the parent's merged options.
func TestDeriveCarriesSetOptionsMatcherOnce(t *testing.T) {
	mk := func(cfg *LexConfig, opts *Options) LexMatcher {
		return func(lex *Lex, rule *Rule) *Token { return nil }
	}
	m := Make()
	m.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"pm": {Order: 100, Make: mk},
	}}})
	c1, err := m.Derive()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := c1.Derive()
	if err != nil {
		t.Fatal(err)
	}
	for i, j := range []*Tabnas{m, c1, c2} {
		ms := j.Config().CustomMatchers
		if len(ms) != 1 || ms[0].Name != "pm" {
			t.Errorf("generation %d: custom matchers %v, want exactly [pm]", i, matcherNames(ms))
		}
	}
}

func matcherNames(ms []*MatcherEntry) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

// A matcher a plugin placed on the parent's Config directly, outside the
// options, is not in the merged options the child is built from. The child
// still inherits it, once, and the slice stays in priority order.
func TestDeriveInheritsDirectConfigMatcherOnceInOrder(t *testing.T) {
	mk := func(cfg *LexConfig, opts *Options) LexMatcher {
		return func(lex *Lex, rule *Rule) *Token { return nil }
	}
	m := Make(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"opt": {Order: 500, Make: mk},
	}}})
	m.Config().CustomMatchers = append(m.Config().CustomMatchers,
		&MatcherEntry{Name: "direct", Priority: 1, Match: mk(nil, nil)})
	c1, err := m.Derive()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := c1.Derive()
	if err != nil {
		t.Fatal(err)
	}
	for i, j := range []*Tabnas{c1, c2} {
		got := matcherNames(j.Config().CustomMatchers)
		if len(got) != 2 || got[0] != "direct" || got[1] != "opt" {
			t.Errorf("generation %d: custom matchers %v, want [direct opt]", i+1, got)
		}
	}
}
