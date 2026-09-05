/* Copyright (c) 2026 Richard Rodger, MIT License */

// The executable cross-port divergence register — Go side.
//
// test/spec/divergent.tsv records each KNOWN split as the value each port
// actually produces. This runner asserts the `go` column; the TypeScript
// and Rust runners assert their columns from the same file.
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
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

const divergentCols = 8
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
		return jsNumberString(t)
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

// jsNumberString renders a float64 exactly as JavaScript's String(number)
// does — ECMAScript Number::toString, base 10.
//
// The obvious spelling, %v, is wrong, and wrong in a way that would make
// this renderer MANUFACTURE divergences: measured, Go's %v gives `1e+20`
// where JavaScript gives `100000000000000000000`, and `1e-07` where
// JavaScript gives `1e-7`. Two ports agreeing on the IEEE-754 value would
// then be recorded as disagreeing, which is the precise failure the
// canonical form exists to prevent. An earlier "just use %d when it is an
// integer" guard did not help either: int64(1e20) overflows, so the guard
// declined and fell through to %v.
//
// The spec's own variable names are kept so the cases can be checked
// against it: s is the shortest decimal digit string, k its length, and n
// the position of the decimal point, so that s x 10^(n-k) == x.
//
// Pinned against the real String() by TestJSNumberStringMatchesJavaScript
// here and its twin in ts/test/divergent.test.js; the two tables must
// stay in step.
func jsNumberString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x == 0:
		return "0" // String(-0) is "0" in JavaScript, not "-0"
	}

	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}

	// FormatFloat with 'e' and precision -1 gives the SHORTEST digit
	// string that round-trips, which is exactly the spec's s and n.
	e := strconv.FormatFloat(x, 'e', -1, 64)
	mant, expPart, _ := strings.Cut(e, "e")
	exp, err := strconv.Atoi(expPart)
	if err != nil {
		return sign + e
	}
	digits := strings.Replace(mant, ".", "", 1)
	k := len(digits)
	n := exp + 1

	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}

	// Exponential. JavaScript writes the exponent with a sign and NO
	// zero padding, where Go's 'e' pads to two digits.
	expSign := "+"
	ev := n - 1
	if ev < 0 {
		expSign = "-"
		ev = -ev
	}
	if k == 1 {
		return sign + digits + "e" + expSign + strconv.Itoa(ev)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + expSign + strconv.Itoa(ev)
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
//
// The cap counts RETAINED tokens rather than Next() calls, matching the
// TypeScript twin, which additionally drops the #SP tokens this port
// never produces. Capping calls instead would reach the limit after half
// as many real tokens there: measured, a `find` target 40 tokens in was
// NOT-FOUND in TypeScript and found at column 79 here, under an
// identical cap — manufacturing the very sequence-dependent difference
// selecting one token is meant to avoid.
func divergentProbeLex(arg map[string]any, input string) (string, error) {
	j := Make(divergentOpts(arg))
	lex := NewLex(input, j.Config())

	var tokens []*Token
	for guard := 0; len(tokens) < divergentMaxTokens && guard < 4*divergentMaxTokens; guard++ {
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
		// Never produced here today; skipped anyway so the two runners
		// retain the same list if that ever changes.
		if tk.Name == "#SP" {
			continue
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
				"rust justification), got %d", row.lineNo, divergentCols, len(row.cols))
			continue
		}

		// Decode escapes in EVERY column, as ts/test/utility.js::loadTSV
		// does. The two loaders must read the same file the same way, or
		// sharing it is worse than not sharing it (test/AGENTS.md).
		cols := make([]string, len(row.cols))
		for i, c := range row.cols {
			cols[i] = preprocessEscapes(c)
		}
		name, probe, argRaw, input, want, why := cols[0], cols[1], cols[2], cols[3], cols[4], cols[7]

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

// TestJSNumberStringMatchesJavaScript pins jsNumberString against the
// real String(number), value by value.
//
// The expectations were TAKEN from `node -e 'console.log(String(v))'`,
// not written from the spec by hand — a shared-rendering claim that is
// only asserted is exactly what cost this PR a review round. The twin
// table is in ts/test/divergent.test.js; the two must stay in step, and
// a value added to one belongs in the other.
func TestJSNumberStringMatchesJavaScript(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{3, "3"},
		{0.1, "0.1"},
		{-0.1, "-0.1"},
		{0.5, "0.5"},
		{1.5, "1.5"},
		{100, "100"},
		{1e6, "1000000"},
		// The two %v got wrong, and the reason this function exists.
		{1e20, "100000000000000000000"},
		{1e-7, "1e-7"},
		// Either side of each threshold in the spec's case split.
		{1e21, "1e+21"},
		{1e-6, "0.000001"},
		{1e-21, "1e-21"},
		{123456789012345680000, "123456789012345680000"},
		{5e-324, "5e-324"},
		{1.7976931348623157e308, "1.7976931348623157e+308"},
		{0.30000000000000004, "0.30000000000000004"},
		{2.0 / 3.0, "0.6666666666666666"},
		{math.Copysign(0, -1), "0"}, // String(-0) is "0", not "-0"
		{9007199254740993, "9007199254740992"},
		{1e100, "1e+100"},
		{1.25e-10, "1.25e-10"},
		{255, "255"},
		{1e-3, "0.001"},
	} {
		if got := jsNumberString(c.in); got != c.want {
			t.Errorf("jsNumberString(%v) = %q, want %q (what JavaScript's "+
				"String() produces)", c.in, got, c.want)
		}
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
	// Walk in file order so each row is attributed to the group it sits
	// under, and record whether that group still holds a row where the
	// two ports actually DISAGREE.
	//
	// The marker alone is not registration. Without this, repairing a
	// divergence and deleting its divergent row — or simply editing both
	// expected columns to the same value — leaves the marker, the control
	// row, both runners and this gate all green, and the DIVERGENCE.md
	// entry outlives the executable evidence that is the entire point of
	// ADR-14. The gate would then be asserting that prose exists, which
	// prose is quite capable of doing by itself.
	registered := map[string]bool{}
	divergentRow := map[string]bool{}
	group := ""
	for _, row := range rows {
		if len(row.cols) == 1 {
			if strings.HasPrefix(row.cols[0], "# @divergence: ") {
				group = strings.TrimSpace(
					strings.TrimPrefix(row.cols[0], "# @divergence: "))
				registered[group] = true
			}
			continue
		}
		if len(row.cols) != divergentCols {
			continue // the runner reports a malformed row; not this gate's job
		}
		if group == "" {
			t.Errorf("line %d: row %q sits above every `# @divergence:` marker, "+
				"so it is attributed to no DIVERGENCE.md entry",
				row.lineNo, row.cols[0])
			continue
		}
		if row.cols[4] != row.cols[5] {
			divergentRow[group] = true
		}
	}

	for g := range registered {
		if !divergentRow[g] {
			t.Errorf("register group %q has no row where the `go` and `ts` "+
				"columns differ. Either the divergence was repaired — in which "+
				"case delete the group AND its DIVERGENCE.md entry, which is "+
				"what the repair is for — or its divergent row was lost and the "+
				"control rows are now pinning agreement under a heading that "+
				"claims disagreement", g)
		}
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
