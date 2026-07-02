// Copyright (c) 2013-2026 Richard Rodger, MIT License

// Tests for (*Tabnas).Merge, mirroring ts/test/merge.test.js.

package tabnas

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// mergeAltKeys renders a rule's open alts as token-name sequences via
// the instance's own tin-name map — the visible interleave order.
func mergeAltKeys(j *Tabnas, rulename string) []string {
	rs := j.RSM()[rulename]
	if rs == nil {
		return nil
	}
	out := make([]string, 0, len(rs.OpenAlts()))
	for _, alt := range rs.OpenAlts() {
		pos := make([]string, len(alt.S))
		for i, tins := range alt.S {
			names := make([]string, len(tins))
			for k, tin := range tins {
				names[k] = j.TinName(tin)
			}
			pos[i] = strings.Join(names, "|")
		}
		out = append(out, strings.Join(pos, " "))
	}
	return out
}

// mergeGrammarA is grammar A of the plan's example: token AT=@ and a
// val rule matching TX AT.
func mergeGrammarA() *Tabnas {
	j := Make(Options{Tag: "A"})
	at := j.Token("#AT", "@")
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}, {at}},
			A: func(r *Rule, ctx *Context) {
				r.Node = fmt.Sprintf("%v@", r.O0.ResolveVal(r, ctx))
			},
			G: "ga",
		})
	})
	return j
}

// mergeGrammarB is grammar B: token PC=% and a val rule matching TX PC.
func mergeGrammarB() *Tabnas {
	j := Make(Options{Tag: "B"})
	pc := j.Token("#PC", "%")
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}, {pc}},
			A: func(r *Rule, ctx *Context) {
				r.Node = fmt.Sprintf("%v%%", r.O0.ResolveVal(r, ctx))
			},
			G: "gb",
		})
	})
	return j
}

func mustParse(t *testing.T, j *Tabnas, src string, want any) {
	t.Helper()
	got, err := j.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	if got != want {
		t.Fatalf("Parse(%q) = %v, want %v", src, got, want)
	}
}

func TestMergeCombinesGrammarsCommutatively(t *testing.T) {
	a := mergeGrammarA()
	b := mergeGrammarB()

	ab, err := a.Merge(b)
	if err != nil {
		t.Fatalf("a.Merge(b) error: %v", err)
	}
	ba, err := b.Merge(a)
	if err != nil {
		t.Fatalf("b.Merge(a) error: %v", err)
	}

	// Both merged instances parse both grammars' forms.
	for _, m := range []*Tabnas{ab, ba} {
		mustParse(t, m, "x@", "x@")
		mustParse(t, m, "y%", "y%")
	}

	// Deterministic interleave: TX AT before TX PC (token-name order
	// at the differing position), in both merge directions.
	want := []string{"#TX #AT", "#TX #PC"}
	for _, m := range []*Tabnas{ab, ba} {
		keys := mergeAltKeys(m, "val")
		if len(keys) != 2 || keys[0] != want[0] || keys[1] != want[1] {
			t.Fatalf("val open alts = %v, want %v", keys, want)
		}
	}

	// Identity is direction-independent.
	if ab.Options().Tag != "A~B" || ba.Options().Tag != "A~B" {
		t.Fatalf("merged tags = %q, %q, want A~B",
			ab.Options().Tag, ba.Options().Tag)
	}

	// Originals unmodified.
	if len(a.RSM()["val"].OpenAlts()) != 1 || len(b.RSM()["val"].OpenAlts()) != 1 {
		t.Fatal("original rule alts modified by merge")
	}
	mustParse(t, a, "x@", "x@")
	mustParse(t, b, "y%", "y%")
	if _, err := a.Parse("y%"); err == nil {
		t.Fatal("grammar A should not parse grammar B's form")
	}
	if _, err := b.Parse("x@"); err == nil {
		t.Fatal("grammar B should not parse grammar A's form")
	}
	if a.Options().Tag != "A" || b.Options().Tag != "B" {
		t.Fatal("original tags modified by merge")
	}
}

func TestMergeRequiresDistinctTags(t *testing.T) {
	if _, err := Make().Merge(mergeGrammarB()); err == nil ||
		!strings.Contains(err.Error(), "first instance needs a Tag") {
		t.Fatalf("expected first-tag error, got %v", err)
	}
	if _, err := mergeGrammarA().Merge(Make()); err == nil ||
		!strings.Contains(err.Error(), "second instance needs a Tag") {
		t.Fatalf("expected second-tag error, got %v", err)
	}
	if _, err := mergeGrammarA().Merge(mergeGrammarA()); err == nil ||
		!strings.Contains(err.Error(), "tags must differ") {
		t.Fatalf("expected equal-tag error, got %v", err)
	}
	// Merge returns errors, never panics — nil other included.
	if _, err := mergeGrammarA().Merge(nil); err == nil {
		t.Fatal("expected error for nil other")
	}
}

func TestMergeOptionConflict(t *testing.T) {
	five, seven := 5, 7
	a := Make(Options{Tag: "A", Rule: &RuleOptions{MaxMul: &five}})
	b := Make(Options{Tag: "B", Rule: &RuleOptions{MaxMul: &seven}})
	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		_, err := pair[0].Merge(pair[1])
		if err == nil || !strings.Contains(err.Error(), "rule.maxmul") {
			t.Fatalf("expected rule.maxmul conflict, got %v", err)
		}
	}

	// Set vs unset (default) is not a conflict: the set value wins in
	// either direction.
	c := Make(Options{Tag: "A", Rule: &RuleOptions{MaxMul: &five}})
	d := Make(Options{Tag: "B"})
	for _, pair := range [][2]*Tabnas{{c, d}, {d, c}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		if m.Options().Rule == nil || m.Options().Rule.MaxMul == nil ||
			*m.Options().Rule.MaxMul != 5 {
			t.Fatalf("merged MaxMul != 5")
		}
	}
}

func TestMergeFixedTokens(t *testing.T) {
	m, err := mergeGrammarA().Merge(mergeGrammarB())
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	at := m.FixedSrc("@")
	pc := m.FixedSrc("%")
	if at == 0 || m.TinName(at) != "#AT" {
		t.Fatalf("merged @ token = %d (%s)", at, m.TinName(at))
	}
	if pc == 0 || m.TinName(pc) != "#PC" {
		t.Fatalf("merged %% token = %d (%s)", pc, m.TinName(pc))
	}
}

func TestMergeLongerPrefixFirst(t *testing.T) {
	a := Make(Options{Tag: "A"})
	a.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}},
			A: func(r *Rule, ctx *Context) {
				r.Node = r.O0.ResolveVal(r, ctx)
			},
		})
	})
	b := mergeGrammarB()

	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		alts := m.RSM()["val"].OpenAlts()
		if len(alts) != 2 || len(alts[0].S) != 2 || len(alts[1].S) != 1 {
			t.Fatalf("expected longer alt first, got %v", mergeAltKeys(m, "val"))
		}
		// The longer alt must win the shared TX prefix, or "y%" would
		// strand the % after the shorter alt matched.
		mustParse(t, m, "y%", "y%")
		mustParse(t, m, "z", "z")
	}
}

func TestMergeComplexityThenGroupTags(t *testing.T) {
	// Same token sequence on both sides; A's alt carries a condition,
	// so it sorts first — its false condition falls through to B's
	// unconditioned alt at parse time.
	a := Make(Options{Tag: "A"})
	a.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}},
			C: func(r *Rule, ctx *Context) bool { return false },
			A: func(r *Rule, ctx *Context) { r.Node = "cond" },
		})
	})
	b := Make(Options{Tag: "B"})
	b.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}},
			A: func(r *Rule, ctx *Context) { r.Node = "plain" },
		})
	})
	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		alts := m.RSM()["val"].OpenAlts()
		if alts[0].C == nil || alts[1].C != nil {
			t.Fatal("conditioned alt should sort first")
		}
		mustParse(t, m, "x", "plain")
	}

	// Equal complexity: group tags decide.
	c := Make(Options{Tag: "A"})
	c.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}}, G: "zz",
			A: func(r *Rule, ctx *Context) { r.Node = "zz" },
		})
	})
	d := Make(Options{Tag: "B"})
	d.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}}, G: "aa",
			A: func(r *Rule, ctx *Context) { r.Node = "aa" },
		})
	})
	for _, pair := range [][2]*Tabnas{{c, d}, {d, c}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		alts := m.RSM()["val"].OpenAlts()
		if alts[0].G != "aa" || alts[1].G != "zz" {
			t.Fatalf("group-tag order wrong: %q, %q", alts[0].G, alts[1].G)
		}
		mustParse(t, m, "x", "aa")
	}
}

func TestMergeDisjointRules(t *testing.T) {
	a := mergeGrammarA()
	a.Rule("extra", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{a.Token("#AT")}}})
	})
	b := mergeGrammarB()
	b.Rule("other", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{b.Token("#PC")}}})
	})
	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		for _, rn := range []string{"val", "extra", "other"} {
			if m.RSM()[rn] == nil {
				t.Fatalf("merged rule %q missing", rn)
			}
		}
		if len(m.RSM()["extra"].OpenAlts()) != 1 ||
			len(m.RSM()["other"].OpenAlts()) != 1 {
			t.Fatal("disjoint rule alts wrong")
		}
	}
}

func TestMergeEmptySequenceLast(t *testing.T) {
	a := Make(Options{Tag: "A"})
	a.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			A: func(r *Rule, ctx *Context) { r.Node = "any" },
		})
	})
	b := Make(Options{Tag: "B"})
	b.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{TinTX}},
			A: func(r *Rule, ctx *Context) { r.Node = "text" },
		})
	})
	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		alts := m.RSM()["val"].OpenAlts()
		if len(alts[0].S) != 1 || len(alts[1].S) != 0 {
			t.Fatal("empty-sequence alt should sort last")
		}
		mustParse(t, m, "x", "text")
	}
}

func TestMergeFixedSourceCollision(t *testing.T) {
	a := mergeGrammarA()
	b := Make(Options{Tag: "B"})
	bt := b.Token("#BT", "@")
	b.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}, {bt}}})
	})
	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		_, err := pair[0].Merge(pair[1])
		if err == nil || !strings.Contains(err.Error(), `both claim source "@"`) {
			t.Fatalf("expected source collision error, got %v", err)
		}
	}
}

func TestMergeDerive(t *testing.T) {
	m, err := mergeGrammarA().Merge(mergeGrammarB())
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	child, err := m.Derive()
	if err != nil {
		t.Fatalf("Derive error: %v", err)
	}
	mustParse(t, child, "x@", "x@")
	mustParse(t, child, "y%", "y%")
	keys := mergeAltKeys(child, "val")
	if len(keys) != 2 || keys[0] != "#TX #AT" || keys[1] != "#TX #PC" {
		t.Fatalf("derived val open alts = %v", keys)
	}
}

func TestMergeSharedAncestorDedupe(t *testing.T) {
	// Both instances install the same base plugin (closures share one
	// code pointer per Use run), and each adds its own alt in front —
	// the shared alt still dedupes even though it sits at different
	// positions in the two lists, and the base lifecycle handler
	// installs once.
	count := 0
	base := func(j *Tabnas, _ map[string]any) error {
		j.Rule("val", func(rs *RuleSpec, p *Parser) {
			rs.AddBO(func(r *Rule, ctx *Context) { count++ })
			rs.AddOpen(&AltSpec{
				S: [][]Tin{{TinTX}},
				A: func(r *Rule, ctx *Context) {
					r.Node = r.O0.ResolveVal(r, ctx)
				},
			})
		})
		return nil
	}
	a := Make(Options{Tag: "A"})
	at := a.Token("#AT", "@")
	if err := a.Use(base); err != nil {
		t.Fatal(err)
	}
	a.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.PrependOpen(&AltSpec{
			S: [][]Tin{{at}},
			A: func(r *Rule, ctx *Context) { r.Node = "at" },
		})
	})
	b := Make(Options{Tag: "B"})
	pc := b.Token("#PC", "%")
	if err := b.Use(base); err != nil {
		t.Fatal(err)
	}
	b.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.PrependOpen(&AltSpec{
			S: [][]Tin{{pc}},
			A: func(r *Rule, ctx *Context) { r.Node = "pc" },
		})
	})
	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		if len(m.RSM()["val"].OpenAlts()) != 3 {
			t.Fatalf("shared alt should dedupe, got %d alts",
				len(m.RSM()["val"].OpenAlts()))
		}
		if len(m.RSM()["val"].Actions("bo")) != 1 {
			t.Fatalf("shared bo handler should install once, got %d",
				len(m.RSM()["val"].Actions("bo")))
		}
		count = 0
		mustParse(t, m, "x", "x")
		if count != 1 {
			t.Fatalf("bo handler fired %d times, want 1", count)
		}
		mustParse(t, m, "@", "at")
		mustParse(t, m, "%", "pc")
	}

	// Conditioned alts never dedupe: Go closures from one literal share
	// a code pointer even over different environments, so a condition
	// cannot be proven identical — both copies are kept (the condition
	// makes the second reachable).
	closing := func(allow bool) Plugin {
		return func(j *Tabnas, _ map[string]any) error {
			j.Rule("val", func(rs *RuleSpec, p *Parser) {
				rs.AddOpen(&AltSpec{
					S: [][]Tin{{TinTX}},
					C: func(r *Rule, ctx *Context) bool { return allow },
					A: func(r *Rule, ctx *Context) {
						r.Node = fmt.Sprintf("allow=%v", allow)
					},
				})
			})
			return nil
		}
	}
	c := Make(Options{Tag: "A"})
	if err := c.Use(closing(false)); err != nil {
		t.Fatal(err)
	}
	d := Make(Options{Tag: "B"})
	if err := d.Use(closing(true)); err != nil {
		t.Fatal(err)
	}
	m, err := c.Merge(d)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	if len(m.RSM()["val"].OpenAlts()) != 2 {
		t.Fatalf("conditioned closures should keep both alts, got %d",
			len(m.RSM()["val"].OpenAlts()))
	}
	// The false condition falls through to the env-differing twin —
	// which a pointer-based dedupe would have wrongly dropped.
	mustParse(t, m, "x", "allow=true")
}

func TestMergeLexMatchers(t *testing.T) {
	// Custom match tokens from each side both lex in the merged instance.
	a := Make(Options{Tag: "A", Match: &MatchOptions{
		Token: map[string]*regexp.Regexp{"#QQ": regexp.MustCompile(`^!+`)},
	}})
	a.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{a.Token("#QQ")}},
			A: func(r *Rule, ctx *Context) { r.Node = "bang" },
		})
	})
	b := Make(Options{Tag: "B", Match: &MatchOptions{
		Token: map[string]*regexp.Regexp{"#WW": regexp.MustCompile(`^\?+`)},
	}})
	b.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{b.Token("#WW")}},
			A: func(r *Rule, ctx *Context) { r.Node = "quest" },
		})
	})
	for _, pair := range [][2]*Tabnas{{a, b}, {b, a}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		mustParse(t, m, "!!", "bang")
		mustParse(t, m, "??", "quest")
	}

	// Registry entries with tied Order values run in name order in
	// both merge directions.
	probeMa := func(lex *Lex, rule *Rule) *Token { return nil }
	probeMb := func(lex *Lex, rule *Rule) *Token { return nil }
	c := Make(Options{Tag: "A", Lex: &LexOptions{Match: map[string]*MatchSpec{
		"mb": {Order: 1500000,
			Make: func(cfg *LexConfig, opts *Options) LexMatcher { return probeMb }},
	}}})
	d := Make(Options{Tag: "B", Lex: &LexOptions{Match: map[string]*MatchSpec{
		"ma": {Order: 1500000,
			Make: func(cfg *LexConfig, opts *Options) LexMatcher { return probeMa }},
	}}})
	for _, pair := range [][2]*Tabnas{{c, d}, {d, c}} {
		m, err := pair[0].Merge(pair[1])
		if err != nil {
			t.Fatalf("Merge error: %v", err)
		}
		var names []string
		for _, entry := range m.Config().CustomMatchers {
			if entry.Name == "ma" || entry.Name == "mb" {
				names = append(names, entry.Name)
			}
		}
		if len(names) != 2 || names[0] != "ma" || names[1] != "mb" {
			t.Fatalf("tied-order matchers = %v, want [ma mb]", names)
		}
	}

	// Same matcher name with a different factory is a conflict.
	e := Make(Options{Tag: "A", Lex: &LexOptions{Match: map[string]*MatchSpec{
		"same": {Order: 1500000,
			Make: func(cfg *LexConfig, opts *Options) LexMatcher { return probeMa }},
	}}})
	f := Make(Options{Tag: "B", Lex: &LexOptions{Match: map[string]*MatchSpec{
		"same": {Order: 1500000,
			Make: func(cfg *LexConfig, opts *Options) LexMatcher { return probeMa }},
	}}})
	if _, err := e.Merge(f); err == nil ||
		!strings.Contains(err.Error(), "lex.match.same.make") {
		t.Fatalf("expected lex.match.same.make conflict, got %v", err)
	}

	// The identical shared entry (same factory reference) merges to one.
	mkshared := func(cfg *LexConfig, opts *Options) LexMatcher { return probeMa }
	g := Make(Options{Tag: "A", Lex: &LexOptions{Match: map[string]*MatchSpec{
		"same": {Order: 1500000, Make: mkshared},
	}}})
	h := Make(Options{Tag: "B", Lex: &LexOptions{Match: map[string]*MatchSpec{
		"same": {Order: 1500000, Make: mkshared},
	}}})
	m, err := g.Merge(h)
	if err != nil {
		t.Fatalf("Merge error: %v", err)
	}
	count := 0
	for _, entry := range m.Config().CustomMatchers {
		if entry.Name == "same" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared matcher entry duplicated: %d", count)
	}
}

// --- Grammar-permutation scenario (mirrors ts/test/merge.test.js
// "merge-permutations"): four small real-world grammars — emails,
// urls, file paths, semvers — each lexing its form with a match token
// and building a structured map in its own pushed rule. Merging any
// 2, 3, or 4 of them must parse the same inputs to the same
// structured results regardless of merge order. ---

func TestMergeGrammarPermutations(t *testing.T) {
	// Shared by all four grammars (same code pointer, so merges dedupe
	// it): hoist the child rule's structured map into val.
	hoist := StateAction(func(r *Rule, ctx *Context) {
		if (r.Node == nil || IsUndefined(r.Node)) &&
			r.Child.Node != nil && !IsUndefined(r.Child.Node) {
			r.Node = r.Child.Node
		}
	})

	makeEmailG := func() *Tabnas {
		j := Make(Options{Tag: "email", Match: &MatchOptions{
			Token: map[string]*regexp.Regexp{
				"#EM": regexp.MustCompile(`^[a-z][a-z0-9._-]*@[a-z0-9.-]+\.[a-z]{2,}`),
			},
		}})
		em := j.Token("#EM")
		j.Rule("val", func(rs *RuleSpec, p *Parser) {
			rs.AddBC(hoist)
			rs.AddOpen(&AltSpec{S: [][]Tin{{em}}, B: 1, P: "email"})
		})
		j.Rule("email", func(rs *RuleSpec, p *Parser) {
			rs.AddBO(func(r *Rule, ctx *Context) {
				r.Node = map[string]any{"kind": "email"}
			})
			rs.AddOpen(&AltSpec{S: [][]Tin{{em}}, A: func(r *Rule, ctx *Context) {
				parts := strings.SplitN(r.O0.Src, "@", 2)
				m := r.Node.(map[string]any)
				m["user"] = parts[0]
				m["domain"] = parts[1]
			}})
		})
		return j
	}

	urlRe := regexp.MustCompile(`^([a-z][a-z0-9+.-]*)://([^/\s]+)(/[^\s]*)?`)
	makeUrlG := func() *Tabnas {
		j := Make(Options{Tag: "url", Match: &MatchOptions{
			Token: map[string]*regexp.Regexp{
				"#UR": regexp.MustCompile(`^[a-z][a-z0-9+.-]*://[^\s]+`),
			},
		}})
		ur := j.Token("#UR")
		j.Rule("val", func(rs *RuleSpec, p *Parser) {
			rs.AddBC(hoist)
			rs.AddOpen(&AltSpec{S: [][]Tin{{ur}}, B: 1, P: "url"})
		})
		j.Rule("url", func(rs *RuleSpec, p *Parser) {
			rs.AddBO(func(r *Rule, ctx *Context) {
				r.Node = map[string]any{"kind": "url"}
			})
			rs.AddOpen(&AltSpec{S: [][]Tin{{ur}}, A: func(r *Rule, ctx *Context) {
				g := urlRe.FindStringSubmatch(r.O0.Src)
				m := r.Node.(map[string]any)
				m["protocol"] = g[1]
				m["host"] = g[2]
				if g[3] == "" {
					m["path"] = "/"
				} else {
					m["path"] = g[3]
				}
			}})
		})
		return j
	}

	makePathG := func() *Tabnas {
		j := Make(Options{Tag: "path", Match: &MatchOptions{
			Token: map[string]*regexp.Regexp{
				"#FP": regexp.MustCompile(`^/[a-zA-Z0-9._-]+(?:/[a-zA-Z0-9._-]+)*`),
			},
		}})
		fp := j.Token("#FP")
		j.Rule("val", func(rs *RuleSpec, p *Parser) {
			rs.AddBC(hoist)
			rs.AddOpen(&AltSpec{S: [][]Tin{{fp}}, B: 1, P: "path"})
		})
		j.Rule("path", func(rs *RuleSpec, p *Parser) {
			rs.AddBO(func(r *Rule, ctx *Context) {
				r.Node = map[string]any{"kind": "path"}
			})
			rs.AddOpen(&AltSpec{S: [][]Tin{{fp}}, A: func(r *Rule, ctx *Context) {
				var segs []string
				for _, s := range strings.Split(r.O0.Src, "/") {
					if s != "" {
						segs = append(segs, s)
					}
				}
				m := r.Node.(map[string]any)
				m["base"] = segs[len(segs)-1]
				m["dir"] = "/" + strings.Join(segs[:len(segs)-1], "/")
			}})
		})
		return j
	}

	semverRe := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$`)
	makeSemverG := func() *Tabnas {
		j := Make(Options{Tag: "semver", Match: &MatchOptions{
			Token: map[string]*regexp.Regexp{
				"#SV": regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?`),
			},
		}})
		sv := j.Token("#SV")
		j.Rule("val", func(rs *RuleSpec, p *Parser) {
			rs.AddBC(hoist)
			rs.AddOpen(&AltSpec{S: [][]Tin{{sv}}, B: 1, P: "semver"})
		})
		j.Rule("semver", func(rs *RuleSpec, p *Parser) {
			rs.AddBO(func(r *Rule, ctx *Context) {
				r.Node = map[string]any{"kind": "semver"}
			})
			rs.AddOpen(&AltSpec{S: [][]Tin{{sv}}, A: func(r *Rule, ctx *Context) {
				g := semverRe.FindStringSubmatch(r.O0.Src)
				major, _ := strconv.Atoi(g[1])
				minor, _ := strconv.Atoi(g[2])
				patch, _ := strconv.Atoi(g[3])
				m := r.Node.(map[string]any)
				m["major"] = major
				m["minor"] = minor
				m["patch"] = patch
				m["prerelease"] = g[4]
			}})
		})
		return j
	}

	input := map[string]string{
		"email":  "alice@example.com",
		"url":    "https://example.com/a/b",
		"path":   "/usr/local/bin/node",
		"semver": "1.2.3-beta.1",
	}
	expected := map[string]map[string]any{
		"email": {"kind": "email", "user": "alice", "domain": "example.com"},
		"url": {"kind": "url", "protocol": "https",
			"host": "example.com", "path": "/a/b"},
		"path": {"kind": "path", "base": "node", "dir": "/usr/local/bin"},
		"semver": {"kind": "semver", "major": 1, "minor": 2, "patch": 3,
			"prerelease": "beta.1"},
	}

	var permute func(items []string) [][]string
	permute = func(items []string) [][]string {
		if len(items) <= 1 {
			return [][]string{append([]string{}, items...)}
		}
		var out [][]string
		for i := range items {
			rest := make([]string, 0, len(items)-1)
			rest = append(rest, items[:i]...)
			rest = append(rest, items[i+1:]...)
			for _, p := range permute(rest) {
				out = append(out, append([]string{items[i]}, p...))
			}
		}
		return out
	}
	var subsets func(items []string, k int) [][]string
	subsets = func(items []string, k int) [][]string {
		if k == 0 {
			return [][]string{{}}
		}
		if len(items) < k {
			return nil
		}
		var out [][]string
		for _, s := range subsets(items[1:], k-1) {
			out = append(out, append([]string{items[0]}, s...))
		}
		out = append(out, subsets(items[1:], k)...)
		return out
	}

	g := map[string]*Tabnas{
		"email": makeEmailG(), "url": makeUrlG(),
		"path": makePathG(), "semver": makeSemverG(),
	}
	names := []string{"email", "url", "path", "semver"}

	// Each singleton parses its own input.
	for _, n := range names {
		got, err := g[n].Parse(input[n])
		if err != nil {
			t.Fatalf("singleton %s: %v", n, err)
		}
		if !reflect.DeepEqual(got, map[string]any(expected[n])) {
			t.Fatalf("singleton %s = %v, want %v", n, got, expected[n])
		}
	}

	permCount := 0
	for k := 2; k <= len(names); k++ {
		for _, subset := range subsets(names, k) {
			refKeys := ""
			for _, perm := range permute(subset) {
				permCount++
				// Chained merge in this permutation's order. Merge never
				// modifies its operands, so the singletons are reusable
				// across all 60 permutations.
				merged := g[perm[0]]
				for _, n := range perm[1:] {
					var err error
					merged, err = merged.Merge(g[n])
					if err != nil {
						t.Fatalf("merge %v: %v", perm, err)
					}
				}

				// Every grammar in the subset parses to the same
				// structured map as its singleton.
				inSubset := map[string]bool{}
				for _, n := range subset {
					inSubset[n] = true
					got, err := merged.Parse(input[n])
					if err != nil {
						t.Fatalf("parse %s via %v: %v", n, perm, err)
					}
					if !reflect.DeepEqual(got, map[string]any(expected[n])) {
						t.Fatalf("parse %s via %v = %v, want %v",
							n, perm, got, expected[n])
					}
				}

				// Inputs from grammars outside the subset are rejected.
				for _, n := range names {
					if !inSubset[n] {
						if _, err := merged.Parse(input[n]); err == nil {
							t.Fatalf("input %s should not parse via %v", n, perm)
						}
					}
				}

				// The interleaved alt order is identical for every merge
				// order of the same grammar subset.
				keys := strings.Join(mergeAltKeys(merged, "val"), ";")
				if refKeys == "" {
					refKeys = keys
				} else if keys != refKeys {
					t.Fatalf("alt order differs for %v: %q vs %q",
						perm, keys, refKeys)
				}
			}
		}
	}

	// P(4,2) + P(4,3) + P(4,4) = 12 + 24 + 24.
	if permCount != 60 {
		t.Fatalf("permutation count = %d, want 60", permCount)
	}

	// The singletons are still intact after 60 merges.
	for _, n := range names {
		got, err := g[n].Parse(input[n])
		if err != nil {
			t.Fatalf("singleton %s corrupted by merges: %v", n, err)
		}
		if !reflect.DeepEqual(got, map[string]any(expected[n])) {
			t.Fatalf("singleton %s result changed after merges: %v", n, got)
		}
		if len(g[n].RSM()["val"].OpenAlts()) != 1 {
			t.Fatalf("singleton %s rule alts modified", n)
		}
	}
}
