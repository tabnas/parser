/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

import (
	"encoding/json"
	"testing"
)

// Opt-in parse budget / cancellation: Options.Parse.Budget, the Go
// mirror of ts/test/budget.test.js. The main rule loop calls OnCheck
// every CheckEveryN iterations; a false return cancels the parse with
// a "cancel" error. Off by default.
//
// One signature difference from TS, and it is deliberate: TS allows
// `boolean | void`, so a checker that only observes can return
// nothing. Go has no undefined, so an observer returns true.

const budgetSrc = `{"a":[1,2,3,{"b":[4,5,6]},7],"c":{"d":[8,9]}}`

// budgetJSON builds the JSON fixture with a budget hook attached.
func budgetJSON(everyN int, onCheck func(ctx *Context) bool) *Tabnas {
	return makeJSON(Options{
		Parse: &ParseOptions{
			Budget: &BudgetOptions{CheckEveryN: everyN, OnCheck: onCheck},
		},
	})
}

// roundTrips is the "the parse was not disturbed" assertion: the
// fixture's own source is canonical JSON, so a clean parse re-encodes
// to exactly it.
func roundTrips(t *testing.T, out any) {
	t.Helper()
	enc, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("result does not encode: %v", err)
	}
	if budgetSrc != string(enc) {
		t.Fatalf("want %s, got %s", budgetSrc, string(enc))
	}
}

func TestBudgetOffByDefault(t *testing.T) {
	out, err := makeJSON().Parse(budgetSrc)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	roundTrips(t, out)
}

func TestBudgetInvokesOnCheckPeriodically(t *testing.T) {
	calls := 0
	j := budgetJSON(2, func(ctx *Context) bool {
		calls++
		return true
	})

	out, err := j.Parse(budgetSrc)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	roundTrips(t, out)
	if calls < 1 {
		t.Fatalf("OnCheck called at least once, got %d", calls)
	}
}

func TestBudgetCancelsOnFalse(t *testing.T) {
	calls := 0
	j := budgetJSON(2, func(ctx *Context) bool {
		calls++
		return calls < 3
	})

	if _, err := j.Parse(budgetSrc); err == nil {
		t.Fatal("parse should cancel")
	} else if je, ok := err.(*TabnasError); !ok {
		t.Fatalf("want *TabnasError, got %T", err)
	} else if "cancel" != je.Code {
		t.Fatalf("want cancel code, got %s", je.Code)
	}
}

func TestBudgetCancelIsRecorded(t *testing.T) {
	// A cancellation is an error of this parse like any other, so it
	// lands in ctx.Errs as the last entry (the A1 invariant).
	calls := 0
	j := budgetJSON(2, func(ctx *Context) bool {
		calls++
		return calls < 3
	})
	get := ctxOf(j)

	_, err := j.Parse(budgetSrc)
	if err == nil {
		t.Fatal("parse should cancel")
	}
	ctx := get()
	if ctx == nil {
		t.Fatal("no context captured")
	}
	if 0 == len(ctx.Errs) {
		t.Fatal("cancellation was not recorded in ctx.Errs")
	}
	if ctx.Errs[len(ctx.Errs)-1] != err {
		t.Fatal("returned error is not the last recorded error")
	}
}

func TestBudgetNeedsBothHalves(t *testing.T) {
	// An interval with no checker, or a checker with no interval, is
	// not a half-enabled budget — it is off. Otherwise a config typo
	// would silently cancel or silently never fire.
	calls := 0
	observer := func(ctx *Context) bool {
		calls++
		return false // would cancel immediately if it ever ran
	}

	for _, tc := range []struct {
		name   string
		everyN int
		fn     func(ctx *Context) bool
	}{
		{"interval without checker", 2, nil},
		{"checker without interval", 0, observer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls = 0
			out, err := budgetJSON(tc.everyN, tc.fn).Parse(budgetSrc)
			if err != nil {
				t.Fatalf("parse should be unaffected: %v", err)
			}
			roundTrips(t, out)
			if 0 != calls {
				t.Fatalf("checker ran %d times with the hook off", calls)
			}
		})
	}
}

func TestBudgetNotCalledBeforeAnyWork(t *testing.T) {
	// Interval N means "every N iterations", not "immediately and then
	// every N": a deadline checker must not be asked before the parse
	// has done anything, or a zero-length budget could never start.
	firstKI := -1
	j := budgetJSON(1, func(ctx *Context) bool {
		if firstKI < 0 {
			firstKI = ctx.KI
		}
		return true
	})

	if _, err := j.Parse(budgetSrc); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if firstKI < 1 {
		t.Fatalf("first check at iteration %d, want >= 1", firstKI)
	}
}

func TestBudgetDeadlineStyleCheck(t *testing.T) {
	// The shape a language server actually uses: closure state read on
	// every iteration, here a flag another goroutine could flip.
	abort := false
	j := budgetJSON(1, func(ctx *Context) bool { return !abort })

	out, err := j.Parse(budgetSrc)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	roundTrips(t, out)

	abort = true
	if _, err := j.Parse(budgetSrc); err == nil {
		t.Fatal("parse should cancel once the flag is set")
	}
}

func TestBudgetCtxIsTheLiveParseContext(t *testing.T) {
	// OnCheck receives the parse context, not a copy: a host needs the
	// rule stack and iteration count to decide whether to keep going.
	var seenKI []int
	var sawRule bool
	j := budgetJSON(3, func(ctx *Context) bool {
		seenKI = append(seenKI, ctx.KI)
		if ctx.Rule != nil && ctx.Rule != NoRule {
			sawRule = true
		}
		return true
	})

	if _, err := j.Parse(budgetSrc); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(seenKI) < 2 {
		t.Fatalf("want several checks, got %d", len(seenKI))
	}
	for i := 1; i < len(seenKI); i++ {
		if seenKI[i] <= seenKI[i-1] {
			t.Fatalf("KI did not advance: %v", seenKI)
		}
	}
	if !sawRule {
		t.Fatal("ctx.Rule was never a live rule")
	}
}
