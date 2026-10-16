/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// RuleDone.Probe: a pass made while a probe is open (Context.ProbeOpen,
// which @probeInit$ calls) is the probe's, not the document's, since the
// grammar rewinds past it. The shared fixture test/spec/probe.tsv pins
// which passes they are, and every runtime runs it.

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestSharedProbeContract(t *testing.T) {
	rows, err := loadTSV(filepath.Join(specDir(), "probe.tsv"))
	if err != nil {
		t.Fatalf("cannot load probe.tsv: %v", err)
	}
	specs := divergentSpecs(rows)
	ran := 0
	for _, row := range rows {
		if len(row.cols) == 1 && strings.HasPrefix(row.cols[0], "#") {
			continue
		}
		if len(row.cols) != 4 {
			t.Fatalf("probe.tsv:%d: expected 4 columns, got %d", row.lineNo, len(row.cols))
		}
		cols := make([]string, len(row.cols))
		for i, column := range row.cols {
			cols[i] = preprocessEscapes(column)
		}
		var args map[string]string
		if err := json.Unmarshal([]byte(cols[1]), &args); err != nil {
			t.Fatalf("%s: bad arguments: %v", cols[0], err)
		}
		def, ok := specs[args["spec"]]
		if !ok {
			t.Fatalf("%s: row names an undefined `# @spec`: %s", cols[0], args["spec"])
		}
		gs, err := GrammarSpecFromJSON([]byte(def))
		if err != nil {
			t.Fatalf("%s: spec does not parse as a GrammarSpec: %v", cols[0], err)
		}
		j := Make()
		if err := j.Grammar(gs); err != nil {
			t.Fatalf("%s: install: %v", cols[0], err)
		}
		probe := []string{}
		kept := 0
		j.SubRuleDone(func(rule *Rule, _ *Context, done RuleDone) {
			if done.Probe {
				state := "open"
				if done.State == CLOSE {
					state = "close"
				}
				probe = append(probe, rule.Name+":"+state)
			} else {
				kept++
			}
		})
		// A spec with recovery on parses through ParseRecover, as TS's
		// parse does when its options enable it.
		if strings.Contains(def, `"recover":{"enabled":true`) {
			if _, _, err := j.ParseRecover(cols[2]); err != nil {
				t.Fatalf("%s: parse: %v", cols[0], err)
			}
		} else if _, err := j.Parse(cols[2]); err != nil {
			t.Fatalf("%s: parse: %v", cols[0], err)
		}
		sort.Strings(probe)
		got, _ := json.Marshal(map[string]any{"probe": probe, "kept": kept})
		// encoding/json sorts map keys; compare as values, not text.
		var gotV, wantV any
		_ = json.Unmarshal(got, &gotV)
		if err := json.Unmarshal([]byte(cols[3]), &wantV); err != nil {
			t.Fatalf("%s: bad expected column: %v", cols[0], err)
		}
		gb, _ := json.Marshal(gotV)
		wb, _ := json.Marshal(wantV)
		if string(gb) != string(wb) {
			t.Errorf("%s: got %s, want %s", cols[0], gb, wb)
		}
		ran++
	}
	if ran == 0 {
		t.Fatal("probe.tsv ran no cases")
	}
}

func TestProbeContextNesting(t *testing.T) {
	ctx := &Context{}
	if ctx.probeTake() {
		t.Error("no probe open, yet the pass is flagged")
	}
	ctx.ProbeOpen(&Rule{D: 2})
	if !ctx.probeTake() {
		t.Error("the pass that opens a probe is the probe's")
	}
	ctx.ProbeOpen(&Rule{D: 4})
	ctx.ProbeClose()
	if !ctx.probeTake() {
		t.Error("still inside the outer probe")
	}
	ctx.ProbeClose()
	if ctx.ProbeDepth() != 0 {
		t.Errorf("ProbeDepth=%d after two closes, want 0", ctx.ProbeDepth())
	}
	if !ctx.probeTake() {
		t.Error("the pass that closes the outer probe is the probe's")
	}
	if ctx.probeTake() {
		t.Error("the next pass is the document again")
	}
	ctx.ProbeClose()
	if ctx.ProbeDepth() != 0 {
		t.Errorf("an unmatched close went negative: ProbeDepth=%d", ctx.ProbeDepth())
	}
}

// The twin of the TypeScript test: recovery abandoning a rule closes the
// probes it opened, and only those.
func TestProbeAbandonClosesTheRulesProbes(t *testing.T) {
	ctx := &Context{}
	ctx.ProbeOpen(&Rule{D: 2})
	ctx.ProbeOpen(&Rule{D: 4})
	ctx.probeTake()
	steps := []struct {
		d     int
		probe bool
		depth int
	}{
		{5, true, 2}, // inside both probes, opened neither
		{4, true, 1}, // the inner probe goes with its rule
		{3, true, 1}, // a rule above it does not own the outer
		{2, true, 0},
	}
	for _, step := range steps {
		if got := ctx.probeAbandon(&Rule{D: step.d}); got != step.probe {
			t.Errorf("abandon d=%d: probe=%v, want %v", step.d, got, step.probe)
		}
		if ctx.ProbeDepth() != step.depth {
			t.Errorf("abandon d=%d: ProbeDepth=%d, want %d", step.d, ctx.ProbeDepth(), step.depth)
		}
	}
	// Nothing leaks to the next pass: the parse loop flags the pass that
	// failed inside the probe because it began inside one.
	if ctx.probeTake() {
		t.Error("the next pass is the document again")
	}
	if ctx.probeAbandon(&Rule{D: 1}) {
		t.Error("a rule outside every probe is not the probe's")
	}
}

func TestProbeNeverFlagsAGrammarThatNeverProbes(t *testing.T) {
	j := Make()
	if err := j.Use(jsonPlugin); err != nil {
		t.Fatal(err)
	}
	passes := 0
	j.SubRuleDone(func(_ *Rule, _ *Context, done RuleDone) {
		passes++
		if done.Probe {
			t.Error("a pass of a grammar with no probe is flagged")
		}
	})
	if _, err := j.Parse(`{"a":[1,2,{"b":null}]}`); err != nil {
		t.Fatal(err)
	}
	if passes == 0 {
		t.Fatal("no ruleDone event fired")
	}
}
