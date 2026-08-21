/* Copyright (c) 2026 Richard Rodger, MIT License */

// The executable cross-port divergence register — Go side.
//
// test/spec/divergent.tsv records each KNOWN split as the value each port
// actually produces. This runner asserts the `go` column; the TypeScript
// runner (ts/test/divergent.test.js) asserts the `ts` column, from the
// same file.
//
// The property that matters: a divergence which gets FIXED fails here as
// loudly as one that regresses, forcing the row to be deleted. Prose
// cannot do that — the sibling repo's differences doc claimed 2.e3 and
// 1e999 still diverged long after they had been aligned, which is what
// moved jsonic to an executable ledger and this repo to ADR-14.
//
// This file also carries the COVERAGE gate (TestDivergenceRegisterCovers
// EveryEntry), which ties the register back to DIVERGENCE.md so an entry
// cannot be quietly dropped from it and an exemption cannot outlive the
// heading it exempts. It lives on this side for the same reason the
// fixture-registration gate does: one runtime reading the other's sources
// is how this repo keeps a two-sided contract honest from one place.

package tabnas

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"
)

const divergentCols = 7
const divergentMaxTokens = 64

// ---------------------------------------------------------------------
// Rendering.
//
// Deliberately NOT this runtime's own quoting or key order. %q and
// JavaScript's JSON.stringify escape different characters, and the two
// runtimes sort strings by different units — both cost this repo a round
// of review already (#156). Everything below renders the same bytes as
// its TypeScript twin by construction.

// divergentValHex renders a string as lowercase UTF-16 code units,
// dot-joined: the one rendering that shows the lone-surrogate split
// without either port having to spell a character it cannot hold. Go
// strings are UTF-8, so this encodes to UTF-16 first; the TS twin reads
// the units directly, because JS strings already are UTF-16.
func divergentValHex(v any) string {
	s, ok := v.(string)
	if !ok {
		return "NOT-A-STRING"
	}
	units := utf16.Encode([]rune(s))
	if len(units) == 0 {
		return "EMPTY"
	}
	parts := make([]string, len(units))
	for i, u := range units {
		parts[i] = fmt.Sprintf("%04x", u)
	}
	return strings.Join(parts, ".")
}

// divergentCanon renders a parsed value canonically, matching the TS
// twin. Strings render VERBATIM between quotes — a row must therefore not
// expect a value containing a tab, newline or carriage return — and map
// keys are sorted by UTF-16 code unit, because ADR-15 puts key order out
// of contract and the two runtimes do not agree on UTF-8 order (utf16Less
// is the same helper ValidateGrammar uses, for the same reason).
func divergentCanon(v any) string {
	switch t := stripRefs(v).(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return `"` + t + `"`
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = divergentCanon(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.SliceStable(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = `"` + k + `":` + divergentCanon(t[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		return "UNRENDERABLE"
	}
}

// ---------------------------------------------------------------------
// Probes. A closed set: an unknown probe is an error, never a skip, or a
// row could sit here asserting nothing while looking green.

func divergentOpts(arg map[string]any) Options {
	m, ok := arg["opts"].(map[string]any)
	if !ok {
		return Options{}
	}
	return MapToOptions(m)
}

func divergentTokenField(tk *Token, field string) (string, error) {
	switch field {
	case "name":
		return tk.Name, nil
	case "src":
		return tk.Src, nil
	case "si":
		return fmt.Sprintf("%d", tk.SI), nil
	case "ri":
		return fmt.Sprintf("%d", tk.RI), nil
	case "ci":
		return fmt.Sprintf("%d", tk.CI), nil
	case "valhex":
		return divergentValHex(tk.Val), nil
	default:
		return "", fmt.Errorf("unknown lex show field (extend both runners): %s", field)
	}
}

func divergentShow(arg map[string]any, dflt []string) ([]string, error) {
	raw, ok := arg["show"].([]any)
	if !ok {
		return dflt, nil
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("show must be an array of strings")
		}
		out[i] = s
	}
	return out, nil
}

// divergentProbeLex lexes input and selects ONE token. Never the whole
// stream: the two ports emit different token SEQUENCES for the same
// source (this port emits no #SP where TypeScript does), so a stream
// render would go red for a reason no row is about.
func divergentProbeLex(arg map[string]any, input string) (string, error) {
	j := Make(divergentOpts(arg))
	lex := NewLex(input, j.Config())

	var tokens []*Token
	for i := 0; i < divergentMaxTokens; i++ {
		tk := lex.Next()
		// A lex failure surfaces on lex.Err here and as a #BD token in
		// TypeScript. The register asserts the observable — code, column,
		// span — not the channel, which is a porting difference rather
		// than a divergence.
		if te, ok := lex.Err.(*TabnasError); ok && te != nil {
			return fmt.Sprintf("ERROR:%s:%d:%s", te.Code, te.Col, te.Src), nil
		}
		if tk == nil || tk.Name == "#ZZ" {
			break
		}
		tokens = append(tokens, tk)
	}

	var tk *Token
	if find, ok := arg["find"].(string); ok {
		for _, t := range tokens {
			if t.Src == find {
				tk = t
				break
			}
		}
	} else {
		at := 0
		if f, ok := arg["at"].(float64); ok {
			at = int(f)
		}
		if 0 <= at && at < len(tokens) {
			tk = tokens[at]
		}
	}
	if tk == nil {
		return "NOT-FOUND", nil
	}

	show, err := divergentShow(arg, []string{"name"})
	if err != nil {
		return "", err
	}
	parts := make([]string, len(show))
	for i, f := range show {
		if parts[i], err = divergentTokenField(tk, f); err != nil {
			return "", err
		}
	}
	return strings.Join(parts, ":"), nil
}

func divergentErrField(te *TabnasError, field string) (string, error) {
	switch field {
	case "code":
		return te.Code, nil
	case "pos":
		return fmt.Sprintf("%d", te.Pos), nil
	case "col":
		return fmt.Sprintf("%d", te.Col), nil
	case "row":
		return fmt.Sprintf("%d", te.Row), nil
	default:
		return "", fmt.Errorf("unknown spec show field (extend both runners): %s", field)
	}
}

// divergentProbeSpec installs a serialized GrammarSpec and parses. A spec
// is pure JSON, so the SAME text drives both ports — which is what lets a
// grammar-level row be registered here at all.
func divergentProbeSpec(
	arg map[string]any, input string, specs map[string]string,
) (string, error) {
	var specJSON string
	switch s := arg["spec"].(type) {
	case string:
		def, ok := specs[s]
		if !ok {
			return "", fmt.Errorf("row names an undefined `# @spec`: %s", s)
		}
		specJSON = def
	case map[string]any:
		b, err := json.Marshal(s)
		if err != nil {
			return "", err
		}
		specJSON = string(b)
	default:
		return "", fmt.Errorf("spec probe needs arg.spec (an object, or an `# @spec` name)")
	}

	gs, err := GrammarSpecFromJSON([]byte(specJSON))
	if err != nil {
		// A spec this port cannot even READ is not the same finding as one
		// it can read and will not load; say so rather than folding both
		// into INSTALL_ERROR.
		return "", fmt.Errorf("spec does not parse as a GrammarSpec: %w", err)
	}

	j := Make(divergentOpts(arg))
	if err := j.Grammar(gs); err != nil {
		return "INSTALL_ERROR", nil
	}

	_, hasShow := arg["show"]
	v, perr := j.Parse(input)
	if perr == nil {
		if hasShow {
			return "OK", nil
		}
		return "OK:" + divergentCanon(v), nil
	}

	te, ok := perr.(*TabnasError)
	if !ok {
		return "ERROR:" + perr.Error(), nil
	}
	if !hasShow {
		return "ERROR:" + te.Code, nil
	}
	fields, err := divergentShow(arg, nil)
	if err != nil {
		return "", err
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		if parts[i], err = divergentErrField(te, f); err != nil {
			return "", err
		}
	}
	return "ERROR:" + strings.Join(parts, ":"), nil
}

func divergentRunProbe(
	probe string, arg map[string]any, input string, specs map[string]string,
) (string, error) {
	switch probe {
	case "lex":
		return divergentProbeLex(arg, input)
	case "spec":
		return divergentProbeSpec(arg, input, specs)
	default:
		return "", fmt.Errorf("unknown probe (extend both runners): %s", probe)
	}
}

// ---------------------------------------------------------------------

// divergentSpecs collects the `# @spec <name> <json>` definitions, so the
// regex rows do not repeat a 160-character grammar five times.
func divergentSpecs(rows []tsvRow) map[string]string {
	specs := map[string]string{}
	for _, row := range rows {
		if len(row.cols) != 1 || !strings.HasPrefix(row.cols[0], "# @spec ") {
			continue
		}
		rest := strings.TrimPrefix(row.cols[0], "# @spec ")
		sp := strings.Index(rest, " ")
		if sp <= 0 {
			continue
		}
		specs[rest[:sp]] = rest[sp+1:]
	}
	return specs
}

func TestDivergentRegister(t *testing.T) {
	rows, lerr := loadTSV(filepath.Join(specDir(), "divergent.tsv"))
	if lerr != nil {
		t.Fatalf("cannot load divergent.tsv: %v", lerr)
	}
	if len(rows) == 0 {
		t.Fatal("divergent.tsv has no rows; if the register is empty, delete " +
			"the file and its runners rather than leaving an assertion that " +
			"asserts nothing")
	}

	specs := divergentSpecs(rows)
	seen := map[string]bool{}
	ran := 0

	for _, row := range rows {
		// A `#`-leading line with no tab is a comment or a directive.
		if len(row.cols) == 1 && strings.HasPrefix(row.cols[0], "#") {
			continue
		}
		if len(row.cols) != divergentCols {
			t.Errorf("line %d: want %d columns (name probe arg input go ts "+
				"justification), got %d", row.lineNo, divergentCols, len(row.cols))
			continue
		}

		// Decode escapes in EVERY column, as ts/test/utility.js::loadTSV
		// does. The two loaders must read the same file the same way, or
		// sharing it is worse than not sharing it (test/AGENTS.md).
		cols := make([]string, len(row.cols))
		for i, c := range row.cols {
			cols[i] = preprocessEscapes(c)
		}
		name, probe, argRaw, input, want, why := cols[0], cols[1], cols[2], cols[3], cols[4], cols[6]

		if seen[name] {
			t.Errorf("%s: duplicate row name", name)
		}
		seen[name] = true
		if strings.TrimSpace(why) == "" {
			t.Errorf("%s: a register row must carry a justification", name)
		}

		arg := map[string]any{}
		if argRaw != "-" && argRaw != "" {
			if err := json.Unmarshal([]byte(argRaw), &arg); err != nil {
				t.Errorf("%s: bad arg %q: %v", name, argRaw, err)
				continue
			}
		}

		got, err := divergentRunProbe(probe, arg, input, specs)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		ran++

		if got != want {
			t.Errorf("%s: the Go side of the register is stale.\n"+
				"  probe: %s %s\n  input: %q\n  got:   %s\n  want:  %s\n"+
				"If Go now AGREES with the ts column the divergence is "+
				"REPAIRED — delete the row, and the DIVERGENCE.md entry with "+
				"it. Do not edit the column to match.",
				name, probe, argRaw, input, got, want)
		}
	}

	// A register that silently ran nothing is the failure mode this file
	// exists to prevent.
	if ran == 0 {
		t.Error("divergent.tsv parsed but no data row ran")
	}
}

// notRegistered lists DIVERGENCE.md entries that are NOT yet rows in the
// register, with the reason and where they ARE pinned. An entry here is a
// gap being declared, not a gap being excused: the register is the
// direction of travel, and the probe set is meant to grow until this map
// is empty.
var notRegistered = map[string]string{
	"Rule-iteration budget: a fractional `rule.maxmul`": "needs a 61-element " +
		"input driven through a full value grammar to reach the runaway guard, " +
		"which no probe builds yet; pinned by ts/test/rule-budget.test.js ('a " +
		"fractional maxmul is expressible here and not in Go') and " +
		"go/rule_budget_test.go TestMaxMulSurvivesTheOptionsMap",
}

// TestDivergenceRegisterCoversEveryEntry ties the register to the prose.
//
// Without this, the two rot apart in the direction that hurts: an entry
// gets written in DIVERGENCE.md and never registered, and the file that
// RUNS quietly covers less and less of the file that CLAIMS. It fails in
// both directions — an unregistered heading with no exemption, and an
// exemption naming a heading that no longer exists.
func TestDivergenceRegisterCoversEveryEntry(t *testing.T) {
	md, err := os.ReadFile(filepath.Join("..", "DIVERGENCE.md"))
	if err != nil {
		t.Fatalf("cannot read DIVERGENCE.md: %v", err)
	}

	var headings []string
	for _, line := range strings.Split(string(md), "\n") {
		if strings.HasPrefix(line, "### ") {
			headings = append(headings, strings.TrimSpace(strings.TrimPrefix(line, "### ")))
		}
	}
	if len(headings) == 0 {
		t.Fatal("no `### ` headings in DIVERGENCE.md — this gate matches on " +
			"them, so a restructure of that file must update this test")
	}

	rows, lerr := loadTSV(filepath.Join(specDir(), "divergent.tsv"))
	if lerr != nil {
		t.Fatalf("cannot load divergent.tsv: %v", lerr)
	}
	registered := map[string]bool{}
	for _, row := range rows {
		if len(row.cols) != 1 || !strings.HasPrefix(row.cols[0], "# @divergence: ") {
			continue
		}
		registered[strings.TrimSpace(
			strings.TrimPrefix(row.cols[0], "# @divergence: "))] = true
	}

	known := map[string]bool{}
	for _, h := range headings {
		known[h] = true
		if registered[h] {
			if _, exempt := notRegistered[h]; exempt {
				t.Errorf("%q is BOTH a register group and a notRegistered "+
					"entry — delete the exemption", h)
			}
			continue
		}
		if _, exempt := notRegistered[h]; exempt {
			continue
		}
		t.Errorf("DIVERGENCE.md entry %q has no `# @divergence:` group in "+
			"test/spec/divergent.tsv and no notRegistered entry. Register it, "+
			"or declare why it cannot be — a divergence recorded only in prose "+
			"is the failure ADR-14 exists to prevent", h)
	}

	for h := range notRegistered {
		if !known[h] {
			t.Errorf("notRegistered names %q, which is no longer a `### ` "+
				"heading in DIVERGENCE.md — the exemption has outlived its "+
				"entry and should go", h)
		}
	}
	for h := range registered {
		if !known[h] {
			t.Errorf("test/spec/divergent.tsv registers %q, which is no longer "+
				"a `### ` heading in DIVERGENCE.md — either the heading was "+
				"renamed (update the group) or the entry was deleted while its "+
				"rows still pass, which means the rows now pin something the "+
				"prose no longer claims", h)
		}
	}
}
