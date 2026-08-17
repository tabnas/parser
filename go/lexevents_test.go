/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

import (
	"testing"
)

// lexevents_test.go — the Go mirror of ts/test/lexevents.test.js.
//
// The lex subscriber stream is a trace of lexer ACTIVITY, not a token
// list: under negotiated lexing a speculative recut fires an event
// that a later failure retracts by re-announcing the RESTORED token.
// The documented consumer contract (ts/doc/api.md, tn.sub) is:
// process events in order, keep the newest per source position, and
// let each kept token's span shadow older events inside
// [SI, SI+len). This suite pins that the retraction happens, which is
// what makes the reconstruction land on the token the parse used.
//
// Note the position unit: Go Token.SI is a BYTE offset where TS
// Token.sI counts UTF-16 units. The contract is identical; only the
// unit differs, so the shadowing arithmetic below uses byte lengths.

// reconcile applies the documented contract to a recorded event list.
func reconcile(events []*Token) map[int]*Token {
	out := map[int]*Token{}
	claimed := [][2]int{}
	for i := len(events) - 1; 0 <= i; i-- {
		t := events[i]
		span := len(t.Src)
		if span < 1 {
			span = 1
		}
		shadowed := false
		for _, c := range claimed {
			if t.SI >= c[0] && t.SI < c[1] {
				shadowed = true
				break
			}
		}
		if shadowed {
			continue
		}
		if _, seen := out[t.SI]; seen {
			continue
		}
		claimed = append(claimed, [2]int{t.SI, t.SI + span})
		out[t.SI] = t
	}
	return out
}

// A grammar over two overlapping fixed literals: #LONG ("ab") wins the
// first cut by longest-match, but an alternate may want the shorter
// #SHORT ("a"). Mirrors the TS suite's fixture.
func overlapGrammar(t *testing.T, wantSecond string) (*Tabnas, *[]*Token) {
	t.Helper()
	long, short, tb, tc := "ab", "a", "b", "c"
	relex := true
	j := Make(Options{
		Lex: &LexOptions{Relex: &relex},
		Fixed: &FixedOptions{Token: map[string]*string{
			"#LONG": &long, "#SHORT": &short, "#TB": &tb, "#TC": &tc,
		}},
	})

	j.Rule("val", func(rs *RuleSpec, p *Parser) {
		rs.ClearOpen()
		// First alternate recuts 'ab' -> #SHORT, then demands a second
		// token; when that is not there the recut must be undone.
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{j.Token("#SHORT")}, {j.Token(wantSecond)}}},
			&AltSpec{S: [][]Tin{{j.Token("#AA")}}},
		)
	})

	events := []*Token{}
	j.Sub(func(tkn *Token, r *Rule, ctx *Context) {
		if 0 <= tkn.SI {
			cp := *tkn
			events = append(events, &cp)
		}
	}, nil)

	return j, &events
}

func TestLexEventsAbandonedRecutIsRetracted(t *testing.T) {
	// The recut alternate wants #TC after #SHORT; 'ab' has no 'c', so
	// it fails and the fallback takes the original #LONG cut. The
	// reconstruction must show #LONG at position 0 — the token the
	// parse proceeded with — not the abandoned #SHORT.
	j, events := overlapGrammar(t, "#TC")
	long := j.Token("#LONG")

	if _, err := j.Parse("ab"); err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	final := reconcile(*events)
	at0, ok := final[0]
	if !ok {
		t.Fatal("no token recorded at position 0")
	}
	if at0.Tin != long {
		t.Fatalf("want restored #LONG at 0, got %s", at0.Name)
	}
	// Span shadowing: any stale event fired at an interior position
	// during the abandoned speculation is covered by #LONG's span.
	if _, leaked := final[1]; leaked {
		t.Fatal("interior stale event was not shadowed")
	}
}

func TestLexEventsCommittedRecutWins(t *testing.T) {
	// Here the recut alternate succeeds (#SHORT then #TB), so the
	// reconstruction must show the recut token, not the original cut.
	j, events := overlapGrammar(t, "#TB")
	short := j.Token("#SHORT")

	if _, err := j.Parse("ab"); err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	final := reconcile(*events)
	at0, ok := final[0]
	if !ok {
		t.Fatal("no token recorded at position 0")
	}
	if at0.Tin != short {
		t.Fatalf("want committed #SHORT at 0, got %s", at0.Name)
	}
}

// Guard: with relex off nothing is ever recut, so no retraction can
// fire — every position is announced exactly once and the
// reconciliation is the identity over the fetched tokens.
func TestLexEventsNoRetractionWithoutRelex(t *testing.T) {
	j := makeJSON()
	events := []*Token{}
	j.Sub(func(tkn *Token, r *Rule, ctx *Context) {
		if 0 <= tkn.SI {
			cp := *tkn
			events = append(events, &cp)
		}
	}, nil)

	if _, err := j.Parse(`{"a":1}`); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("expected the document's tokens, got %d events", len(events))
	}

	// No position is announced twice with a DIFFERENT identity: that
	// only happens when a recut is committed or retracted.
	byPos := map[int]string{}
	for _, e := range events {
		if prev, seen := byPos[e.SI]; seen && prev != e.Name {
			t.Fatalf("position %d announced as %s then %s without relex", e.SI, prev, e.Name)
		}
		byPos[e.SI] = e.Name
	}
}
