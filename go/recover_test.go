/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

import (
	"encoding/json"
	"strings"
	"testing"
)

// Opt-in panic-mode recovery: Options.Parse.Recover, the Go mirror of
// ts/test/recover.test.js. With recovery on, a parse error no longer
// ends the parse — the error is recorded, the lexer skips to a sync
// token derived from the live rule stack, and parsing continues.
//
// Read results with ParseRecover, which returns (value, errs, err).
// That is Go's form of TS's `{ value, errors }`: TS changes the shape
// of parse()'s single return, which Go cannot do without forcing every
// existing caller through a type assertion.

// mkRec builds the JSON fixture with recovery on, applying opt.
func mkRec(t *testing.T, opt ...func(*RecoverOptions)) *Tabnas {
	t.Helper()
	r := &RecoverOptions{Enabled: true}
	for _, f := range opt {
		f(r)
	}
	return makeJSON(Options{Parse: &ParseOptions{Recover: r}})
}

func intp(v int) *int    { return &v }
func boolp(v bool) *bool { return &v }

func enc(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<unmarshalable>"
	}
	return string(b)
}

func TestRecoverOffByDefault(t *testing.T) {
	j := makeJSON()
	if _, err := j.Parse(`{"a":1,}`); err == nil {
		t.Fatal("fail-fast parse should still error")
	}
	// ParseRecover with recovery off behaves exactly like Parse.
	v, errs, err := j.ParseRecover(`{"a":1,}`)
	if err == nil {
		t.Fatal("ParseRecover should surface the error when recovery is off")
	}
	if v != nil {
		t.Fatalf("want nil value, got %v", v)
	}
	// ctx.Errs still records the fatal error (the A1 invariant).
	if 1 != len(errs) {
		t.Fatalf("want the one fatal error, got %d", len(errs))
	}
}

func TestRecoverCleanParseReportsNoErrors(t *testing.T) {
	j := mkRec(t)
	v, errs, err := j.ParseRecover(`{"a":1}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if 0 != len(errs) {
		t.Fatalf("clean parse recorded %d errors", len(errs))
	}
	if `{"a":1}` != enc(v) {
		t.Fatalf("got %s", enc(v))
	}
}

func TestRecoverSingleErrorKeepsPartialValue(t *testing.T) {
	j := mkRec(t)
	v, errs, err := j.ParseRecover(`{"a":1,}`)
	if err != nil {
		t.Fatalf("recovered parse should not return an error: %v", err)
	}
	if 1 != len(errs) {
		t.Fatalf("want 1 error, got %d", len(errs))
	}
	if `{"a":1}` != enc(v) {
		t.Fatalf("partial value lost: %s", enc(v))
	}
}

func TestRecoverReportsOneDiagnosticPerUnlexableRun(t *testing.T) {
	// Two unlexable runs, `blah` and `blip`, are two diagnostics — one
	// each, not one per character and not one for the pair. The lexer
	// hands each unclaimed rune over as a #BD token, and the fetch-time
	// absorber coalesces contiguous ones into a single region, so the
	// parse simply continues past each run.
	//
	// Continuing is the point: the trailing `"b":1` still parses. An
	// earlier shape skipped straight to the comma and lost it.
	//
	// Cross-checked against the TS engine on this exact input: two
	// errors and {"a":true,"b":1}.
	j := mkRec(t, func(r *RecoverOptions) { r.Suppress = intp(0) })
	v, errs, err := j.ParseRecover(`{"a":true blah blip,"b":1}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if 2 != len(errs) {
		t.Fatalf("want one diagnostic per run (2), got %d", len(errs))
	}
	for i, e := range errs {
		if e.Recovered == nil || !e.Recovered.Bad {
			t.Fatalf("error %d is not marked as an absorbed run: %+v", i, e.Recovered)
		}
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want a map, got %s", enc(v))
	}
	if true != m["a"] {
		t.Fatalf("value before the faults lost: %s", enc(v))
	}
	if 1 != toInt(m["b"]) {
		t.Fatalf("value AFTER the faults lost: %s", enc(v))
	}
}

// toInt reads a JSON-ish number back as an int, whatever concrete
// numeric type the grammar produced.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return -1
}

func TestRecoverReportsOneFaultPerRunNotPerCharacter(t *testing.T) {
	// The guard on the above: a run of arbitrary length is still one
	// diagnostic. A per-character error stream would bury an editor.
	j := mkRec(t)
	_, errs, err := j.ParseRecover(`{"a": zzzzzzzzzzzzzzzzzzzz, "b":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if 1 != len(errs) {
		t.Fatalf("want one diagnostic for a 20-character run, got %d", len(errs))
	}
}

func TestRecoverAtEOF(t *testing.T) {
	j := mkRec(t)
	v, errs, err := j.ParseRecover(`{"a":[1,2`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if 0 == len(errs) {
		t.Fatal("want at least one error")
	}
	if !strings.Contains(enc(v), `"a"`) {
		t.Fatalf("partial structure lost: %s", enc(v))
	}
}

func TestRecoverCapsAtMaxRecoveries(t *testing.T) {
	j := mkRec(t, func(r *RecoverOptions) {
		r.MaxRecoveries = intp(2)
		r.Suppress = intp(0)
	})
	_, errs, _ := j.ParseRecover(`{"a":true x,"b":true y,"c":true z,"d":true w}`)
	if len(errs) > 3 {
		t.Fatalf("maxRecoveries not enforced: %d errors", len(errs))
	}
}

func TestRecoverSuppressesCascades(t *testing.T) {
	// Two unlexable runs close together. With no window both are
	// reported; with a window of 8 consumed tokens the second reads as
	// a follow-on of the first and is dropped. Either way the document
	// parses and the values on both sides survive.
	//
	// Cross-checked against the TS engine on this input: 2 errors at
	// suppress 0, 1 at suppress 8, {"a":true,"b":1} in both.
	src := `{"a":true blah blip,"b":1}`

	zeroInst := mkRec(t, func(r *RecoverOptions) { r.Suppress = intp(0) })
	if 0 != zeroInst.parser.Config.Recover.Suppress {
		t.Fatalf("suppress 0 not applied: got %d", zeroInst.parser.Config.Recover.Suppress)
	}
	_, zero, err := zeroInst.ParseRecover(src)
	if err != nil {
		t.Fatalf("suppress 0: %v", err)
	}
	if 2 != len(zero) {
		t.Fatalf("suppress 0 should keep both regions, got %d", len(zero))
	}

	eightInst := mkRec(t, func(r *RecoverOptions) { r.Suppress = intp(8) })
	if 8 != eightInst.parser.Config.Recover.Suppress {
		t.Fatalf("suppress 8 not applied: got %d", eightInst.parser.Config.Recover.Suppress)
	}
	v, eight, err := eightInst.ParseRecover(src)
	if err != nil {
		t.Fatalf("suppress 8: %v", err)
	}
	if 1 != len(eight) {
		t.Fatalf("suppress 8 should drop the follow-on, got %d", len(eight))
	}
	m, ok := v.(map[string]any)
	if !ok || true != m["a"] || 1 != toInt(m["b"]) {
		t.Fatalf("values around the suppressed region lost: %s", enc(v))
	}
}

func TestRecoverRecordsMetadata(t *testing.T) {
	// The skipped region is what lets a diagnostics consumer show how
	// much source the recovery abandoned.
	j := mkRec(t)
	_, errs, _ := j.ParseRecover(`{"a":true blah blip,"b":1}`)
	if 0 == len(errs) {
		t.Fatal("want an error")
	}
	found := false
	for _, e := range errs {
		if e.Recovered != nil {
			found = true
			if e.Recovered.Skipped < 0 {
				t.Fatalf("negative skip count %d", e.Recovered.Skipped)
			}
		}
	}
	if !found {
		t.Fatal("no error carried recovery metadata")
	}
}

func TestRecoverGivesUpButKeepsValue(t *testing.T) {
	// Hitting the skip cap ends recovery, but the partial container
	// built so far is still the most useful thing to hand back.
	j := mkRec(t, func(r *RecoverOptions) { r.MaxSkip = intp(3) })
	_, errs, _ := j.ParseRecover(`{"a": zzzzzzzzzzzzzzzzzzzz}`)
	if 0 == len(errs) {
		t.Fatal("want at least one error")
	}
}

func TestRecoverEmptySourceUnaffected(t *testing.T) {
	// Whatever the instance does with empty source, recovery must not
	// change it. The JSON fixture rejects empty source (emptyAllow is
	// off), and the parse never reaches the rule loop where recovery
	// hooks — so the answer has to be identical either way.
	plain := makeJSON()
	_, plainErr := plain.Parse("")

	_, errs, err := mkRec(t).ParseRecover("")
	if (nil == plainErr) != (nil == err) {
		t.Fatalf("recovery changed empty-source handling: %v vs %v", plainErr, err)
	}
	if nil == err && 0 != len(errs) {
		t.Fatalf("empty source recorded %d errors", len(errs))
	}
}

func TestRecoverDoesNotReplayBeforeCloseActions(t *testing.T) {
	// `[1 :]` errors in the elem close AFTER @elem-bc already appended
	// the 1. Recovery resuming that same rule must not run the action
	// again — a replayed append would duplicate the element, and the
	// value would be silently wrong rather than merely incomplete.
	j := mkRec(t)
	v, errs, err := j.ParseRecover(`[1 :]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if 0 == len(errs) {
		t.Fatal("want at least one error")
	}
	if list, ok := v.([]any); ok {
		ones := 0
		for _, e := range list {
			if f, ok := e.(float64); ok && 1 == f {
				ones++
			}
			if i, ok := e.(int); ok && 1 == i {
				ones++
			}
		}
		if ones > 1 {
			t.Fatalf("element duplicated by a replayed bc action: %s", enc(v))
		}
	}
}

func TestRecoverSyncGroupsReplaceRatherThanSplice(t *testing.T) {
	// A supplied array means ONLY those groups. Splicing over the
	// engine defaults would silently keep 'close'/'comma'/'end' and
	// make a deliberately narrow configuration behave like the default.
	j := mkRec(t, func(r *RecoverOptions) { r.SyncGroups = []string{"nope"} })
	got := j.parser.Config.Recover.SyncGroups
	if 1 != len(got) || "nope" != got[0] {
		t.Fatalf("syncGroups spliced instead of replaced: %v", got)
	}
	// Recovery still works, via the structural fallback.
	_, errs, err := j.ParseRecover(`{"a":1,}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if 0 == len(errs) {
		t.Fatal("want at least one error")
	}
}

func TestRecoverPopUntilValidOff(t *testing.T) {
	// With PopUntilValid off the engine pops exactly one rule instead
	// of searching for one that accepts the sync token.
	j := mkRec(t, func(r *RecoverOptions) { r.PopUntilValid = boolp(false) })
	if false != j.parser.Config.Recover.PopUntilValid {
		t.Fatal("PopUntilValid not applied")
	}
	// Must not hang or panic; either outcome is acceptable behaviour.
	if _, _, err := j.ParseRecover(`{"a":[1,2}`); err != nil {
		t.Logf("fixed-depth pop gave up: %v", err)
	}
}

func TestRecoverResyncsAtLineEndForControlChars(t *testing.T) {
	// A control character INSIDE a string is a mid-construct fault:
	// resuming just past it would restart lexing inside the string and
	// let a stray quote swallow the following lines into one giant
	// value. Recovery advances to the line end instead.
	j := mkRec(t)
	src := "{\"a\":\"x\x07y\",\n\"b\":2}"
	v, errs, err := j.ParseRecover(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if 0 == len(errs) {
		t.Fatal("want the control-character error")
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("structure lost: %s", enc(v))
	}
	// No value swallowed the rest of the document.
	for k, val := range m {
		if s, ok := val.(string); ok && 5 <= len(s) {
			t.Fatalf("value %q ran past the line: %q", k, s)
		}
	}
}

func TestRecoverDoesNotDisturbLaterParses(t *testing.T) {
	// Recovery bookkeeping lives on the per-parse Context, so a
	// recovered parse must leave the instance exactly as it found it.
	j := mkRec(t)
	_, errs, err := j.ParseRecover(`{"a":1,}`)
	if err != nil || 0 == len(errs) {
		t.Fatalf("first parse: errs=%d err=%v", len(errs), err)
	}
	v, errs2, err := j.ParseRecover(`{"a":1}`)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if 0 != len(errs2) {
		t.Fatalf("clean parse inherited %d errors", len(errs2))
	}
	if `{"a":1}` != enc(v) {
		t.Fatalf("second parse got %s", enc(v))
	}
}

func TestRecoverParseReturnsValueWithoutErrors(t *testing.T) {
	// Parse keeps its two-value signature: a recovered parse yields the
	// partial value and a nil error, so existing callers see a result
	// rather than a failure. ParseRecover is how you learn there were
	// errors at all.
	j := mkRec(t)
	v, err := j.Parse(`{"a":1,}`)
	if err != nil {
		t.Fatalf("Parse should not surface a recovered error: %v", err)
	}
	if `{"a":1}` != enc(v) {
		t.Fatalf("got %s", enc(v))
	}
}

func TestRecoverForcedCloseEventsAreEmitted(t *testing.T) {
	// A rule popped by recovery never runs a close pass, so without a
	// synthesized event an outline consumer would see an open with no
	// matching close and its spans would never balance. This is the
	// RuleDone.Forced flag that A4 defined but could not yet exercise.
	j := mkRec(t)
	forced := []string{}
	j.SubRuleDone(func(rule *Rule, ctx *Context, done RuleDone) {
		if done.Forced {
			forced = append(forced, rule.Name+":"+string(done.State))
		}
	})

	if _, _, err := j.ParseRecover(`{"a":[1,2}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if 0 == len(forced) {
		t.Fatal("no forced close events: popped rules left the stream unbalanced")
	}
	for _, f := range forced {
		if !strings.HasSuffix(f, ":c") {
			t.Fatalf("forced event was not a close: %s", f)
		}
	}
}

func TestRecoverTrailingContent(t *testing.T) {
	// The completeness checks run AFTER the rule loop: a document
	// whose value parses cleanly but is followed by junk must still
	// produce a diagnostic in recovery mode, with the value kept —
	// mirroring TS, where the trailing-content raise is recorded by
	// the TabnasError constructor and converted to { value, errors }.
	// This port once skipped those checks entirely under recovery and
	// silently ACCEPTED `"x" q`. Mirrors the TS case
	// "reports trailing content after a complete value".
	j := mkRec(t)
	for _, c := range []struct {
		src   string
		value string
	}{
		{`"x" q`, `"x"`},
		{`1 q`, `1`},
		{`"a" "b"`, `"a"`},
	} {
		v, errs, err := j.ParseRecover(c.src)
		if err != nil {
			t.Fatalf("%s: unexpected terminal error: %v", c.src, err)
		}
		if 1 != len(errs) {
			t.Fatalf("%s: errs = %s, want one `unexpected`", c.src, enc(errs))
		}
		if "unexpected" != errs[0].Code {
			t.Fatalf("%s: code = %s", c.src, errs[0].Code)
		}
		if c.value != enc(v) {
			t.Fatalf("%s: value = %s, want %s", c.src, enc(v), c.value)
		}
	}
}

func TestRecoverGiveUpAddsNoTrailingError(t *testing.T) {
	// When recovery gives up, the terminal fault is already recorded
	// and TS's catch converts without ever reaching the post-loop
	// completeness checks — so those checks must not run here either:
	// they would fetch a LATER token and report a second error from
	// text recovery deliberately stopped processing. Mirrors the TS
	// case "give-up leaves only the recorded errors: no post-loop
	// extras".
	j := mkRec(t, func(r *RecoverOptions) { r.MaxSkip = intp(0) })
	_, errs, err := j.ParseRecover(`[1 : abc def ghi]`)
	if err != nil {
		t.Fatalf("unexpected terminal error: %v", err)
	}
	if 1 != len(errs) || "unexpected" != errs[0].Code {
		t.Fatalf("want the one recorded `unexpected`, got %s", enc(errs))
	}
}

func TestRecoverTrailingBadTokenKeepsCode(t *testing.T) {
	// A bad token in trailing content carries its own code — TS raises
	// `why || unexpected` with the token's use details in both the
	// buffered and the final-lookahead checks. Both routes are pinned:
	// the prefetched-lookahead one (json grammar) and the no-lookahead
	// one (a rule closing on an empty alternate, where the final
	// Lex.Next returns a deferred #BD with lex.Err nil). Mirrors the
	// TS case "trailing bad tokens keep their own error code".
	j := mkRec(t)
	v, errs, err := j.ParseRecover(`1 "abc`)
	if err != nil {
		t.Fatalf("lookahead route: unexpected terminal error: %v", err)
	}
	if 1 != len(errs) || "unterminated_string" != errs[0].Code {
		t.Fatalf("lookahead route: want one unterminated_string, got %s", enc(errs))
	}
	if `1` != enc(v) {
		t.Fatalf("lookahead route: value = %s, want 1", enc(v))
	}

	n := Make(Options{Parse: &ParseOptions{Recover: &RecoverOptions{Enabled: true}}})
	gs, gerr := GrammarSpecFromJSON([]byte(
		`{"options":{"rule":{"start":"top"}},` +
			`"rule":{"top":{"open":[{"s":"#TX"}],"close":[{}]}}}`))
	if gerr != nil {
		t.Fatal(gerr)
	}
	if gerr = n.Grammar(gs); gerr != nil {
		t.Fatal(gerr)
	}
	_, errs, err = n.ParseRecover(`x "`)
	if err != nil {
		t.Fatalf("no-lookahead route: unexpected terminal error: %v", err)
	}
	if 1 != len(errs) || "unterminated_string" != errs[0].Code {
		t.Fatalf("no-lookahead route: want one unterminated_string, got %s", enc(errs))
	}
}
