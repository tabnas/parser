/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

import "testing"

// Per-parse error list: ctx.Errs, the Go mirror of TS ctx.errs
// (ts/test/errs.test.js). Errors record themselves at their
// CONSTRUCTION site, so the error Parse returns is also the last
// recorded entry. Groundwork for opt-in multi-error recovery.
//
// Pinned here, matching the TS suite case for case:
//  1. fail-fast behaviour is unchanged — Parse still returns an error;
//  2. the returned error IS the recorded error (same pointer);
//  3. Errs is per-parse — a fresh parse starts empty;
//  4. a clean parse records nothing;
//  5. recording never panics on a context-less lexer.

// ctxOf grabs the parse Context via a rule subscriber, which is the
// only handle a caller has on it after Parse returns.
func ctxOf(j *Tabnas) func() *Context {
	var seen *Context
	j.Sub(nil, func(r *Rule, ctx *Context) { seen = ctx })
	return func() *Context { return seen }
}

func TestErrsRecordsReturnedError(t *testing.T) {
	j := makeJSON()
	get := ctxOf(j)

	_, err := j.Parse(`{"a":1,}`)
	if err == nil {
		t.Fatal("parse should fail")
	}
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("want *TabnasError, got %T", err)
	}
	ctx := get()
	if ctx == nil {
		t.Fatal("no context captured")
	}
	if 1 != len(ctx.Errs) {
		t.Fatalf("want 1 recorded error, got %d", len(ctx.Errs))
	}
	if ctx.Errs[0] != je {
		t.Fatal("returned error is not the recorded error")
	}
}

func TestErrsIsPerParse(t *testing.T) {
	j := makeJSON()
	get := ctxOf(j)

	if _, err := j.Parse(`[1,`); err == nil {
		t.Fatal("parse should fail")
	}
	first := get()

	if _, err := j.Parse(`{"b":`); err == nil {
		t.Fatal("parse should fail")
	}
	second := get()

	if first == second {
		t.Fatal("second parse reused the first parse's context")
	}
	if 1 != len(second.Errs) {
		t.Fatalf("second parse: want 1 recorded error, got %d", len(second.Errs))
	}
}

func TestErrsEmptyOnCleanParse(t *testing.T) {
	j := makeJSON()
	get := ctxOf(j)

	if _, err := j.Parse(`{"a":[1,2]}`); err != nil {
		t.Fatalf("clean parse failed: %v", err)
	}
	ctx := get()
	if ctx == nil {
		t.Fatal("no context captured")
	}
	if 0 != len(ctx.Errs) {
		t.Fatalf("clean parse recorded %d errors", len(ctx.Errs))
	}
}

func TestErrsRecordsLexerErrors(t *testing.T) {
	// An unterminated string is raised by the lexer, not the parser —
	// a different construction site, which must record too.
	j := makeJSON()
	get := ctxOf(j)

	_, err := j.Parse(`{"a":"unterminated`)
	if err == nil {
		t.Fatal("parse should fail")
	}
	ctx := get()
	if ctx == nil || 0 == len(ctx.Errs) {
		t.Fatal("lexer error was not recorded")
	}
	if ctx.Errs[len(ctx.Errs)-1] != err {
		t.Fatal("returned lexer error is not the last recorded error")
	}
}

func TestRecordErrIsNilSafe(t *testing.T) {
	// A Lex built without a Context (NewLex) has no list to record
	// into; recording must never be the thing that breaks a parse.
	var ctx *Context
	je := &TabnasError{Code: "unexpected"}
	if got := ctx.recordErr(je); got != je {
		t.Fatal("recordErr on a nil context must return its argument")
	}
	real := &Context{}
	if got := real.recordErr(nil); got != nil {
		t.Fatal("recordErr(nil) must return nil")
	}
	if 0 != len(real.Errs) {
		t.Fatal("recordErr(nil) must not append")
	}
}
