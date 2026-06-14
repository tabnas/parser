package tabnas

// Token-rewind primitives on Context: Mark() captures the current parse
// position; Rewind(m) replays every token consumed since that mark by
// re-feeding the active lexer's pending-token queue. Mirrors
// ts/test/rewind.test.js. Go reports an out-of-window rewind as an error
// (TS throws) to preserve the no-panic guarantee.

import (
	"strings"
	"testing"
)

// rewindParser builds a bare-engine instance with the given fixed tokens
// and rewind options, then lets the caller install rules.
func rewindParser(t *testing.T, fixed map[string]string, history *int) (*Tabnas, map[string]Tin) {
	t.Helper()
	tok := make(map[string]*string, len(fixed))
	for name, src := range fixed {
		s := src
		tok[name] = &s
	}
	opts := Options{
		Rule:  &RuleOptions{Start: "top"},
		Fixed: &FixedOptions{Token: tok},
	}
	if history != nil {
		opts.Rewind = &RewindOptions{History: history}
	}
	j := Make(opts)
	tins := make(map[string]Tin, len(fixed))
	for name := range fixed {
		tins[name] = j.Token(name)
	}
	return j, tins
}

func srcs(toks []*Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Src
	}
	return out
}

func TestRewindRecordsConsumed(t *testing.T) {
	j, tn := rewindParser(t, map[string]string{"Ta": "a", "Tb": "b", "Tc": "c"}, nil)
	var recorded []string
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: [][]Tin{{tn["Ta"]}, {tn["Tb"]}, {tn["Tc"]}}}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}, A: func(r *Rule, ctx *Context) {
			recorded = srcs(ctx.V)
		}}}
	})
	if _, err := j.Parse("abc"); err != nil {
		t.Fatal(err)
	}
	// a, b, c, then the end sentinel (src "").
	want := []string{"a", "b", "c", ""}
	if strings.Join(recorded, ",") != strings.Join(want, ",") {
		t.Errorf("ctx.V = %v, want %v", recorded, want)
	}
}

func TestRewindReplays(t *testing.T) {
	j, tn := rewindParser(t, map[string]string{"Ta": "a", "Tb": "b", "Tc": "c"}, nil)
	var trace []string
	abc := [][]Tin{{tn["Ta"]}, {tn["Tb"]}, {tn["Tc"]}}
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: abc, P: "again", A: func(r *Rule, ctx *Context) {
			trace = append(trace, "first:"+strings.Join(srcs(ctx.V[len(ctx.V)-3:]), ""))
			if err := ctx.Rewind(0); err != nil {
				t.Errorf("rewind: %v", err)
			}
			trace = append(trace, "after-rewind-v-len:"+itoa(len(ctx.V)))
		}}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	j.Rule("again", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: abc, A: func(r *Rule, ctx *Context) {
			trace = append(trace, "second:"+strings.Join(srcs(ctx.V[len(ctx.V)-3:]), ""))
		}}}
	})
	if _, err := j.Parse("abc"); err != nil {
		t.Fatal(err)
	}
	want := "first:abc|after-rewind-v-len:0|second:abc"
	if strings.Join(trace, "|") != want {
		t.Errorf("trace = %q, want %q", strings.Join(trace, "|"), want)
	}
}

func TestRewindPartial(t *testing.T) {
	j, tn := rewindParser(t, map[string]string{"Ta": "a", "Tb": "b", "Tc": "c", "Td": "d"}, nil)
	var second string
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{
			S: [][]Tin{{tn["Ta"]}, {tn["Tb"]}, {tn["Tc"]}, {tn["Td"]}},
			P: "tail",
			A: func(r *Rule, ctx *Context) {
				// Mark right after 'a' (absolute index 1); rewind undoes bcd.
				if err := ctx.Rewind(1); err != nil {
					t.Errorf("rewind: %v", err)
				}
			},
		}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	j.Rule("tail", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: [][]Tin{{tn["Tb"]}, {tn["Tc"]}, {tn["Td"]}}, A: func(r *Rule, ctx *Context) {
			second = strings.Join(srcs(ctx.V), "")
		}}}
	})
	if _, err := j.Parse("abcd"); err != nil {
		t.Fatal(err)
	}
	if second != "abcd" {
		t.Errorf("after partial rewind, ctx.V = %q, want abcd", second)
	}
}

func TestRewindNoOp(t *testing.T) {
	j, tn := rewindParser(t, map[string]string{"Ta": "a"}, nil)
	ok := false
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: [][]Tin{{tn["Ta"]}}, A: func(r *Rule, ctx *Context) {
			mark := ctx.Mark()
			if err := ctx.Rewind(mark); err != nil {
				t.Errorf("rewind: %v", err)
			}
			ok = ctx.VAbs == mark
		}}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("no-op rewind changed VAbs")
	}
}

func TestRewindHistoryCap(t *testing.T) {
	cap4 := 4
	j, tn := rewindParser(t, map[string]string{"Ta": "a"}, &cap4)
	maxSeen := 0
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: [][]Tin{{tn["Ta"]}}, A: func(r *Rule, ctx *Context) {
			if len(ctx.V) > maxSeen {
				maxSeen = len(ctx.V)
			}
		}}}
		rs.close = []*AltSpec{
			{S: [][]Tin{{tn["Ta"]}}, B: 1, R: "top"},
			{S: [][]Tin{{TinZZ}}},
		}
	})
	if _, err := j.Parse("a a a a a a a a a a a a a a a a a a a a"); err != nil {
		t.Fatal(err)
	}
	if maxSeen > 2*4 {
		t.Errorf("ctx.V grew to %d, expected <= %d", maxSeen, 2*4)
	}
	if maxSeen < 4 {
		t.Errorf("ctx.V only reached %d, expected >= 4", maxSeen)
	}
}

func TestRewindPastWindowErrors(t *testing.T) {
	cap2 := 2
	j, tn := rewindParser(t, map[string]string{"Ta": "a"}, &cap2)
	var rewindErr error
	five := [][]Tin{{tn["Ta"]}, {tn["Ta"]}, {tn["Ta"]}, {tn["Ta"]}, {tn["Ta"]}, {tn["Ta"]}}
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: five, A: func(r *Rule, ctx *Context) {
			rewindErr = ctx.Rewind(0)
		}}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if _, err := j.Parse("a a a a a a"); err != nil {
		t.Fatal(err)
	}
	if rewindErr == nil || !strings.Contains(rewindErr.Error(), "outside the retained history") {
		t.Errorf("expected out-of-window error, got %v", rewindErr)
	}
}

func TestRewindDefaultHistory64(t *testing.T) {
	j, tn := rewindParser(t, map[string]string{"Ta": "a"}, nil)
	finalV := 0
	ten := make([][]Tin, 10)
	for i := range ten {
		ten[i] = []Tin{tn["Ta"]}
	}
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: ten, A: func(r *Rule, ctx *Context) {
			finalV = len(ctx.V)
		}}}
		rs.close = []*AltSpec{{S: [][]Tin{{TinZZ}}}}
	})
	if _, err := j.Parse("a a a a a a a a a a"); err != nil {
		t.Fatal(err)
	}
	// Under the default cap of 64, a 10-token parse retains every token.
	if finalV != 10 {
		t.Errorf("ctx.V = %d, want 10", finalV)
	}
}

func TestRewindUnbounded(t *testing.T) {
	zero := 0 // non-positive history → unbounded
	j, tn := rewindParser(t, map[string]string{"Ta": "a"}, &zero)
	maxV := 0
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.open = []*AltSpec{{S: [][]Tin{{tn["Ta"]}}, A: func(r *Rule, ctx *Context) {
			if len(ctx.V) > maxV {
				maxV = len(ctx.V)
			}
		}}}
		rs.close = []*AltSpec{
			{S: [][]Tin{{tn["Ta"]}}, B: 1, R: "top"},
			{S: [][]Tin{{TinZZ}}},
		}
	})
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteByte('a')
	}
	if _, err := j.Parse(sb.String()); err != nil {
		t.Fatal(err)
	}
	if maxV < 200 {
		t.Errorf("unbounded history retained only %d, expected >= 200", maxV)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
