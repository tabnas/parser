// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package tabnas

import "testing"

// TestActionErrorKeepsItsCode pins that a *TabnasError raised from a
// grammar action reaches the caller with its own code.
//
// It did not. startParse wrapped every recovered panic into "internal",
// so a plugin author diagnosing a defect in the INPUT — a TOML key
// conflict, a duplicate section — had no way to say so: the diagnosis
// came back labelled as an engine bug. @tabnas/toml's key-conflict check
// exists in the TypeScript port and not in the Go one for exactly this
// reason.
//
// TypeScript has always allowed it (tabnas.ts: `if (e instanceof
// TabnasError) err = e; else throw e`), so this was a divergence in what
// the two engines let a grammar SAY, not merely in what they do.
func TestActionErrorKeepsItsCode(t *testing.T) {
	j := makeJSON()
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddAO(func(r *Rule, ctx *Context) {
			// Constructed the way a PLUGIN can: exported fields only.
			// The engine's own constructors are unexported, so this is
			// the surface actually available outside the package, and
			// the fix is worth nothing if it does not carry this.
			panic(&TabnasError{
				Code:   "demo_key_conflict",
				Detail: "demo",
				Row:    1,
				Col:    1,
			})
		})
	})

	_, err := j.Parse(`{"a":1}`)
	if err == nil {
		t.Fatal("the action raised an error, so the parse must fail")
	}
	te, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("want *TabnasError, got %T", err)
	}
	if "demo_key_conflict" != te.Code {
		t.Errorf("code = %q, want demo_key_conflict — a coded error raised "+
			"from an action is being relabelled, so a plugin cannot "+
			"diagnose its own input", te.Code)
	}
}

// TestNonErrorPanicStaysInternal is the other half, and it is not
// decorative: the fix above must not turn an engine bug into something
// that reads like a grammar's considered diagnosis. Anything that is not
// a *TabnasError still becomes "internal".
//
// This is where the two ports deliberately differ. TypeScript re-throws a
// non-TabnasError; Go cannot, because this package promises never to
// panic on a caller — see nopanic_test.go. Go keeps the stronger
// guarantee and the ports agree on the case that carries meaning.
func TestNonErrorPanicStaysInternal(t *testing.T) {
	j := makeJSON()
	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.AddAO(func(r *Rule, ctx *Context) { panic("not an error value") })
	})

	_, err := j.Parse(`{"a":1}`)
	te, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("want *TabnasError, got %T", err)
	}
	if "internal" != te.Code {
		t.Errorf("code = %q, want internal — a bare panic is an engine "+
			"bug and must not be dressed up as a diagnosis", te.Code)
	}
}
