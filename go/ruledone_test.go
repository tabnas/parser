/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

import (
	"encoding/json"
	"strings"
	"testing"
)

// Post-process rule event: Tabnas.SubRuleDone, the Go mirror of
// ts/test/ruledone.test.js. It fires AFTER each rule pass, with the
// matched tokens recorded on the rule — unlike the pre-process
// RuleSub, which sees a rule that has not matched anything yet. That
// is the span-bearing structural stream an LSP outline is built from.
//
// Two shape differences from TS, both forced and both documented in
// doc/differences.md: the subscription is its own method rather than a
// third argument to Sub (whose positional signature the sibling repos
// all call), and Forced is always false until Go gains recovery (A2),
// since only recovery synthesizes a close.

type doneEvent struct {
	I     int
	Name  string
	State RuleState
	Alt   *RuleDoneAlt
	O0    int
	C0    int
	ON    int
	CN    int
}

// collectDone subscribes and returns the accumulating event list.
func collectDone(j *Tabnas) *[]doneEvent {
	events := []doneEvent{}
	j.SubRuleDone(func(rule *Rule, ctx *Context, done RuleDone) {
		e := doneEvent{
			I: rule.I, Name: rule.Name, State: done.State,
			Alt: done.Alt, O0: -1, C0: -1, ON: rule.ON, CN: rule.CN,
		}
		if rule.O0 != nil && !rule.O0.IsNoToken() {
			e.O0 = rule.O0.SI
		}
		if rule.C0 != nil && !rule.C0.IsNoToken() {
			e.C0 = rule.C0.SI
		}
		events = append(events, e)
	})
	return &events
}

func findDone(events []doneEvent, name string, state RuleState) *doneEvent {
	for i := range events {
		if events[i].Name == name && events[i].State == state {
			return &events[i]
		}
	}
	return nil
}

func TestRuleDoneFiresWithMatchedTokens(t *testing.T) {
	j := makeJSON()
	events := collectDone(j)
	src := `{"a":1}`

	if _, err := j.Parse(src); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if 0 == len(*events) {
		t.Fatal("no events")
	}

	// The map rule's open pass matched the brace at index 0 and its
	// close pass the brace at the end — the exact spans an outline
	// needs, and precisely what the pre-process event cannot see.
	mapOpen := findDone(*events, "map", OPEN)
	mapClose := findDone(*events, "map", CLOSE)
	if mapOpen == nil {
		t.Fatal("no map open event")
	}
	if mapClose == nil {
		t.Fatal("no map close event")
	}
	if want := strings.Index(src, "{"); mapOpen.O0 != want {
		t.Fatalf("map open at %d, want %d", mapOpen.O0, want)
	}
	if want := strings.Index(src, "}"); mapClose.C0 != want {
		t.Fatalf("map close at %d, want %d", mapClose.C0, want)
	}
	if mapOpen.I != mapClose.I {
		t.Fatalf("different rule instances: %d vs %d", mapOpen.I, mapClose.I)
	}
}

func TestRuleDoneReconstructsNestedSpans(t *testing.T) {
	j := makeJSON()
	events := collectDone(j)
	src := `{"a":[1,{"b":2}]}`

	if _, err := j.Parse(src); err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Pair open and close events by rule instance id, exactly as an
	// outline provider would.
	type span struct {
		name       string
		start, end int
	}
	opens := map[int]doneEvent{}
	spans := []span{}
	for _, e := range *events {
		if "map" != e.Name && "list" != e.Name {
			continue
		}
		if OPEN == e.State && 0 < e.ON {
			opens[e.I] = e
		}
		if CLOSE == e.State && 0 < e.CN {
			if open, ok := opens[e.I]; ok {
				spans = append(spans, span{e.Name, open.O0, e.C0})
			}
		}
	}

	want := []span{
		{"map", 0, len(src) - 1},
		{"list", strings.Index(src, "["), strings.Index(src, "]")},
		{"map", strings.Index(src[1:], "{") + 1, strings.Index(src[1:], "}") + 1},
	}
	if len(spans) != len(want) {
		t.Fatalf("want %d spans, got %d: %+v", len(want), len(spans), spans)
	}
	// Sort by start, matching the TS suite's comparison.
	for i := 0; i < len(spans); i++ {
		for k := i + 1; k < len(spans); k++ {
			if spans[k].start < spans[i].start {
				spans[i], spans[k] = spans[k], spans[i]
			}
		}
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("span %d: got %+v, want %+v", i, spans[i], want[i])
		}
	}
}

func TestRuleDoneExposesRouting(t *testing.T) {
	j := makeJSON()
	events := collectDone(j)

	if _, err := j.Parse(`{"a":1,"b":2}`); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	routed := false
	for _, e := range *events {
		if e.Alt != nil && ("" != e.Alt.P || "" != e.Alt.R) {
			routed = true
			break
		}
	}
	if !routed {
		t.Fatal("no push/replace routing observed")
	}
}

func TestRuleDoneFiresOnTheFailurePath(t *testing.T) {
	// A failed pass is still a completed pass: the event fires, and its
	// Alt carries the token that could not be matched. Without this an
	// outline stops silently at the first error.
	j := makeJSON()
	events := collectDone(j)

	if _, err := j.Parse(`{"a":1,,}`); err == nil {
		t.Fatal("parse should fail")
	}
	if 0 == len(*events) {
		t.Fatal("no events on the failure path")
	}
	sawErr := false
	for _, e := range *events {
		if e.Alt != nil && e.Alt.Err != nil {
			sawErr = true
			break
		}
	}
	if !sawErr {
		t.Fatal("no failure token observed")
	}
}

func TestRuleDoneAltIsNilOnlyWhenNoAlternates(t *testing.T) {
	// Alt nil means "this state had no alternates at all"; a state
	// whose alternates all failed reports a non-nil Alt carrying Err.
	// Collapsing the two would make a grammar hole look like a syntax
	// error to a consumer.
	j := makeJSON()
	var withErr, withRouting int
	j.SubRuleDone(func(rule *Rule, ctx *Context, done RuleDone) {
		if done.Alt == nil {
			return
		}
		if done.Alt.Err != nil {
			withErr++
		}
		if "" != done.Alt.P || "" != done.Alt.R {
			withRouting++
		}
	})

	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if 0 != withErr {
		t.Fatalf("clean parse reported %d failure alts", withErr)
	}
	if 0 == withRouting {
		t.Fatal("clean parse reported no routing")
	}
}

func TestRuleDoneGroupTagsAreSnapshots(t *testing.T) {
	// AltSpec.G is live grammar configuration. The event hands out a
	// fresh slice, so a consumer that mutates what it receives cannot
	// corrupt the grammar for the next parse.
	j := makeJSON()
	j.SubRuleDone(func(rule *Rule, ctx *Context, done RuleDone) {
		if done.Alt != nil && 0 < len(done.Alt.G) {
			done.Alt.G[0] = "HACK"
			done.Alt.G = append(done.Alt.G, "HACK")
		}
	})

	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	out, err := j.Parse(`{"b":2}`)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	enc, _ := json.Marshal(out)
	if `{"b":2}` != string(enc) {
		t.Fatalf("mutation corrupted the grammar: %s", string(enc))
	}
}

func TestRuleDoneNotFiredWhenNotSubscribed(t *testing.T) {
	j := makeJSON()
	out, err := j.Parse(`{"a":[1,2]}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	enc, _ := json.Marshal(out)
	if `{"a":[1,2]}` != string(enc) {
		t.Fatalf("want {\"a\":[1,2]}, got %s", string(enc))
	}
}

func TestRuleDoneSubIsInheritedByDerive(t *testing.T) {
	// Derive is Go's tn.make(): a child instance parses the same
	// language, so it must carry the parent's subscribers like it
	// already carries lex and rule ones.
	//
	// The grammar goes in as a PLUGIN here, not through registerJSON
	// directly, because Derive re-runs plugins and does not copy rules
	// installed by the Rule() API — the same documented make() rule
	// that forces continuations() to transplant rule specs in TS. A
	// makeJSON() child has zero rules and parses to nil, which would
	// make this test pass for the wrong reason.
	j := Make(jsonOptions())
	if err := j.Use(func(t *Tabnas, opts map[string]any) error {
		return registerJSONGrammar(t)
	}); err != nil {
		t.Fatalf("plugin failed: %v", err)
	}

	fired := 0
	j.SubRuleDone(func(rule *Rule, ctx *Context, done RuleDone) { fired++ })

	child, err := j.Derive()
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}
	if 0 == len(child.parser.RSM) {
		t.Fatal("child has no rules: the grammar did not survive Derive")
	}
	if 0 == len(child.ruleDoneSubs) {
		t.Fatal("child did not inherit the subscriber list")
	}

	out, err := child.Parse(`{"x":1}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	enc, _ := json.Marshal(out)
	if `{"x":1}` != string(enc) {
		t.Fatalf("child parsed to %s", string(enc))
	}
	if 0 == fired {
		t.Fatal("subscriber did not fire on the derived instance")
	}
}

func TestRuleDoneStateIsPrePassState(t *testing.T) {
	// State is the rule's state BEFORE the pass. Reading it after the
	// transition would report every event as a close, which is what
	// makes open/close pairing possible at all.
	j := makeJSON()
	events := collectDone(j)

	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var opens, closes int
	for _, e := range *events {
		switch e.State {
		case OPEN:
			opens++
		case CLOSE:
			closes++
		default:
			t.Fatalf("unexpected state %q", e.State)
		}
	}
	if 0 == opens {
		t.Fatal("no open-state events: State was read after the transition")
	}
	if 0 == closes {
		t.Fatal("no close-state events")
	}
}
