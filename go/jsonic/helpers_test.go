package jsonic

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

// Shared test helpers for tests migrated from the engine package.

// makeBare returns an engine instance with the relaxed-JSON grammar
// installed directly (not registered as a plugin). jsonic.Make registers
// the grammar as a plugin, and SetOptions re-applies plugins, which
// rebuilds the rule specs; tests that mutate grammar rules and then call
// SetOptions rely on the pre-split semantics where SetOptions never
// reinstalled the grammar — makeBare preserves those semantics.
func makeBare(opts ...tabnas.Options) *tabnas.Tabnas {
	j := tabnas.Make(opts...)
	if err := Plugin(j, nil); err != nil {
		panic(err)
	}
	return j
}

// setOptionsFromText parses a textual options snippet with the jsonic
// grammar and applies it via SetOptions. The engine's SetOptionsText
// (and GrammarText) parse the text with the now grammar-free
// tabnas.Make(), whose Parse returns nil, so textual options silently
// no-op after the engine/grammar split. This helper preserves the
// intent of the text-form option tests: text parsed by the relaxed-JSON
// grammar, converted via MapToOptions, applied via SetOptions.
func setOptionsFromText(j *tabnas.Tabnas, text string) (*tabnas.Tabnas, error) {
	parsed, err := Parse(text)
	if err != nil {
		return j, err
	}
	if parsed == nil {
		return j, nil
	}
	m, ok := parsed.(map[string]any)
	if !ok {
		return j, fmt.Errorf("setOptionsFromText: expected map, got %T", parsed)
	}
	return j.SetOptions(tabnas.MapToOptions(m)), nil
}

// boolPtr is a helper to create a *bool (mirrors the engine-internal
// helper of the same name).
func boolPtr(b bool) *bool {
	return &b
}

// newGrammarParser returns a tabnas.NewParser() with the relaxed-JSON
// grammar installed, matching the pre-split NewParser() (which bundled
// the grammar).
func newGrammarParser() *tabnas.Parser {
	p := tabnas.NewParser()
	buildGrammar(p.RSM, p.Config)
	return p
}

// mustGrammar applies a grammar spec or fails the test (mirrors the
// helper of the same name in the engine test package).
func mustGrammar(t *testing.T, j *tabnas.Tabnas, gs *tabnas.GrammarSpec) {
	t.Helper()
	if err := j.Grammar(gs); err != nil {
		t.Fatal(err)
	}
}

// grammarText replicates the engine's (*Tabnas).GrammarText using the
// jsonic grammar to parse the text. The engine's GrammarText parses its
// input with the grammar-free tabnas.Make(), whose Parse now returns
// nil, so textual grammar declarations silently no-op after the
// engine/grammar split. The map-to-spec conversion below mirrors the
// engine's mapToGrammarRules / parseGrammarAltsOrSpec /
// mapToGrammarAltSpec helpers.
func grammarText(j *tabnas.Tabnas, text string, setting ...*tabnas.GrammarSetting) error {
	parsed, err := Parse(text)
	if err != nil {
		return err
	}
	if parsed == nil {
		return nil
	}
	gsMap, ok := parsed.(map[string]any)
	if !ok {
		return fmt.Errorf("GrammarText: expected map, got %T", parsed)
	}
	gs := &tabnas.GrammarSpec{}
	if optionsMap, ok := gsMap["options"].(map[string]any); ok {
		gs.OptionsMap = optionsMap
	} else if _, hasRule := gsMap["rule"]; !hasRule {
		// No "options" wrapper and no "rule" key — treat the entire map
		// as options.
		gs.OptionsMap = gsMap
	}
	if ruleMap, ok := gsMap["rule"].(map[string]any); ok {
		gs.Rule = mapToGrammarRulesT(ruleMap)
	}
	return j.Grammar(gs, setting...)
}

func mapToGrammarRulesT(ruleMap map[string]any) map[string]*tabnas.GrammarRuleSpec {
	rules := make(map[string]*tabnas.GrammarRuleSpec, len(ruleMap))
	for name, v := range ruleMap {
		rm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		spec := &tabnas.GrammarRuleSpec{}
		if open, ok := rm["open"]; ok {
			spec.Open = parseGrammarAltsOrSpecT(open)
		}
		if close_, ok := rm["close"]; ok {
			spec.Close = parseGrammarAltsOrSpecT(close_)
		}
		rules[name] = spec
	}
	return rules
}

func parseGrammarAltsOrSpecT(v any) any {
	if arr, ok := v.([]any); ok {
		return parseGrammarAltsT(arr)
	}
	if m, ok := v.(map[string]any); ok {
		altsRaw, hasAlts := m["alts"]
		if !hasAlts {
			return nil
		}
		altsArr, ok := altsRaw.([]any)
		if !ok {
			return nil
		}
		spec := &tabnas.GrammarAltListSpec{Alts: parseGrammarAltsT(altsArr)}
		if injectRaw, ok := m["inject"].(map[string]any); ok {
			spec.Inject = &tabnas.GrammarInjectSpec{}
			if append_, ok := injectRaw["append"].(bool); ok {
				spec.Inject.Append = append_
			}
			if del, ok := injectRaw["delete"].([]any); ok {
				for _, d := range del {
					if f, ok := d.(float64); ok {
						spec.Inject.Delete = append(spec.Inject.Delete, int(f))
					}
				}
			}
			if mv, ok := injectRaw["move"].([]any); ok {
				for _, mvv := range mv {
					if f, ok := mvv.(float64); ok {
						spec.Inject.Move = append(spec.Inject.Move, int(f))
					}
				}
			}
		}
		return spec
	}
	return nil
}

func parseGrammarAltsT(arr []any) []*tabnas.GrammarAltSpec {
	alts := make([]*tabnas.GrammarAltSpec, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		alts = append(alts, mapToGrammarAltSpecT(m))
	}
	return alts
}

func mapToGrammarAltSpecT(m map[string]any) *tabnas.GrammarAltSpec {
	alt := &tabnas.GrammarAltSpec{}
	if v, ok := m["s"]; ok {
		alt.S = v
	}
	if v, ok := m["b"]; ok {
		alt.B = v
	}
	if v, ok := m["p"].(string); ok {
		alt.P = v
	}
	if v, ok := m["r"].(string); ok {
		alt.R = v
	}
	if v, ok := m["a"].(string); ok {
		alt.A = v
	}
	if v, ok := m["e"].(string); ok {
		alt.E = v
	}
	if v, ok := m["h"].(string); ok {
		alt.H = v
	}
	if v, ok := m["c"]; ok {
		alt.C = v
	}
	if v, ok := m["n"].(map[string]any); ok {
		alt.N = make(map[string]int, len(v))
		for k, val := range v {
			if f, ok := val.(float64); ok {
				alt.N[k] = int(f)
			}
		}
	}
	if v, ok := m["u"].(map[string]any); ok {
		alt.U = v
	}
	if v, ok := m["k"].(map[string]any); ok {
		alt.K = v
	}
	if v, ok := m["g"].(string); ok {
		alt.G = v
	}
	return alt
}

// altGTags and containsTagSet mirror the helpers of the same name in
// the engine test package (splitGroupTags inlined).
func altGTags(t *testing.T, j *tabnas.Tabnas, rulename, state string) [][]string {
	t.Helper()
	rs, ok := j.RSM()[rulename]
	if !ok {
		t.Fatalf("rule %q not found", rulename)
	}
	var alts []*tabnas.AltSpec
	switch state {
	case "open":
		alts = rs.Open
	case "close":
		alts = rs.Close
	default:
		t.Fatalf("bad state %q", state)
	}
	out := make([][]string, 0, len(alts))
	for _, a := range alts {
		if a == nil {
			continue
		}
		var tags []string
		for _, p := range strings.Split(a.G, ",") {
			if p = strings.TrimSpace(p); p != "" {
				tags = append(tags, p)
			}
		}
		sort.Strings(tags)
		out = append(out, tags)
	}
	return out
}

func containsTagSet(tagSets [][]string, want []string) bool {
	sort.Strings(want)
	for _, ts := range tagSets {
		if len(ts) != len(want) {
			continue
		}
		eq := true
		for i := range ts {
			if ts[i] != want[i] {
				eq = false
				break
			}
		}
		if eq {
			return true
		}
	}
	return false
}

// preprocessEscapes converts literal \n, \r and \t escape sequences in
// TSV fixture fields into their real characters. Recreated here as a
// test helper after package-level preprocessEscapes was removed from
// the engine package.
func preprocessEscapes(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	var out []rune
	i := 0
	for i < len(runes) {
		if runes[i] == '\\' && i+1 < len(runes) {
			switch runes[i+1] {
			case 'n':
				out = append(out, '\n')
				i += 2
			case 'r':
				out = append(out, '\r')
				i += 2
			case 't':
				out = append(out, '\t')
				i += 2
			default:
				out = append(out, runes[i])
				i++
			}
		} else {
			out = append(out, runes[i])
			i++
		}
	}
	return string(out)
}
