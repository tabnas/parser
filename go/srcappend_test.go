// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

// The tree builders grow a node's "src" once per item of a repetition:
// @capture$ appends each child's src, @fold$ appends each iteration's
// src and separator to the parent, and @node$ appends matched terminals.
// A repetition is a same-depth replace loop (AGENTS.md, "Repetition is
// replacement, never a push chain"), so one node takes every append of
// the loop. Go strings are immutable, and `n["src"] = ns + cs` copied
// the whole accumulated src on every item: a loop of n items cost O(n²)
// time here while TypeScript (V8 ropes) and Rust (`push_str`) are
// linear. appendSrc (srcappend.go) makes each append amortized O(1);
// these tests pin the time and the values.

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// srcLoopCase is one builtin's replace loop: a grammar, an input of n
// items, and the JSON the parse must produce for it.
type srcLoopCase struct {
	name    string
	grammar func(t testing.TB) *Tabnas
	input   func(n int) string
	want    func(n int) string
}

func srcLoopWord(i int) string { return fmt.Sprintf("w%023d", i) }
func srcLoopNum(i int) string  { return fmt.Sprintf("1%017d", i) }

func srcLoopGrammar(t testing.TB, spec *GrammarSpec) *Tabnas {
	t.Helper()
	tn := Make()
	if err := tn.Grammar(spec); err != nil {
		t.Fatal(err)
	}
	return tn
}

var srcLoopCases = []srcLoopCase{
	{
		// `list` pushes an item and, on close, captures it and replaces
		// itself while another word follows: every item lands in the one
		// node the loop carries.
		name: "capture",
		grammar: func(t testing.TB) *Tabnas {
			capture := map[string]any{"capture$": map[string]any{
				"rule": "list", "kind": "user"}}
			return srcLoopGrammar(t, &GrammarSpec{
				OptionsMap: map[string]any{
					"rule": map[string]any{"start": "list"},
				},
				Rule: map[string]*GrammarRuleSpec{
					"list": {
						Open: []*GrammarAltSpec{{P: "item"}},
						Close: []*GrammarAltSpec{
							{S: []string{"#TX"}, B: 1, R: "list",
								A: "@capture$", K: capture},
							{A: "@capture$", K: capture},
						},
					},
					"item": {
						Open: []*GrammarAltSpec{{S: []string{"#TX"},
							A: "@node$", K: map[string]any{"node$": map[string]any{
								"init": true, "rule": "item", "kind": "user",
								"nterms": 1}}}},
						Close: []*GrammarAltSpec{{}},
					},
				},
			})
		},
		input: func(n int) string {
			words := make([]string, n)
			for i := range words {
				words[i] = srcLoopWord(i)
			}
			return strings.Join(words, " ")
		},
		want: func(n int) string {
			var src, kids strings.Builder
			for i := 0; i < n; i++ {
				w := srcLoopWord(i)
				src.WriteString(w)
				if i > 0 {
					kids.WriteByte(',')
				}
				kids.WriteString(`{"kids":[],"rule":"item","src":"` + w + `"}`)
			}
			return `{"kids":[` + kids.String() + `],"rule":"list","src":"` +
				src.String() + `"}`
		},
	},
	{
		// The tail-repeat shape TestBuiltinFoldTailRepeat pins: each
		// `add` folds its own node, then the `+` it closed on, into
		// `val`'s node.
		name: "fold",
		grammar: func(t testing.TB) *Tabnas {
			return srcLoopGrammar(t, &GrammarSpec{
				OptionsMap: map[string]any{
					"rule":  map[string]any{"start": "val"},
					"fixed": map[string]any{"token": map[string]any{"#PL": "+"}},
				},
				Rule: map[string]*GrammarRuleSpec{
					"val": {
						Open: []*GrammarAltSpec{{P: "add", A: "@node$",
							K: map[string]any{"node$": map[string]any{
								"init": true, "rule": "val", "kind": "user"}}}},
						Close: []*GrammarAltSpec{{A: "@capture$",
							K: map[string]any{"capture$": map[string]any{
								"rule": "val", "kind": "user"}}}},
					},
					"add": {
						Open: []*GrammarAltSpec{{S: []string{"#NR"}, A: "@node$",
							K: map[string]any{"node$": map[string]any{
								"init": true, "rule": "add", "kind": "user",
								"nterms": 1}}}},
						Close: []*GrammarAltSpec{
							{S: []string{"#PL"}, R: "add", A: "@fold$",
								K: map[string]any{"fold$": map[string]any{"cN": 1}}},
							{A: "@fold$"},
						},
					},
				},
			})
		},
		input: func(n int) string {
			nums := make([]string, n)
			for i := range nums {
				nums[i] = srcLoopNum(i)
			}
			return strings.Join(nums, "+")
		},
		want: func(n int) string {
			var src, kids strings.Builder
			for i := 0; i < n; i++ {
				d := srcLoopNum(i)
				if i > 0 {
					src.WriteByte('+')
					kids.WriteByte(',')
				}
				src.WriteString(d)
				kids.WriteString(`{"kids":[],"rule":"add","src":"` + d + `"}`)
			}
			return `{"kids":[` + kids.String() + `],"rule":"val","src":"` +
				src.String() + `"}`
		},
	},
	{
		// `top` allocates the node and pushes `list`, which inherits it
		// and appends one matched word per iteration of its replace loop.
		name: "node",
		grammar: func(t testing.TB) *Tabnas {
			return srcLoopGrammar(t, &GrammarSpec{
				OptionsMap: map[string]any{
					"rule": map[string]any{"start": "top"},
				},
				Rule: map[string]*GrammarRuleSpec{
					"top": {
						Open: []*GrammarAltSpec{{P: "list", A: "@node$",
							K: map[string]any{"node$": map[string]any{
								"init": true, "rule": "top", "kind": "user"}}}},
						Close: []*GrammarAltSpec{{}},
					},
					"list": {
						Open: []*GrammarAltSpec{{S: []string{"#TX"}, A: "@node$",
							K: map[string]any{"node$": map[string]any{
								"nterms": 1}}}},
						Close: []*GrammarAltSpec{
							{S: []string{"#TX"}, B: 1, R: "list"},
							{},
						},
					},
				},
			})
		},
		input: func(n int) string {
			words := make([]string, n)
			for i := range words {
				words[i] = srcLoopWord(i)
			}
			return strings.Join(words, " ")
		},
		want: func(n int) string {
			var src strings.Builder
			for i := 0; i < n; i++ {
				src.WriteString(srcLoopWord(i))
			}
			return `{"kids":[],"rule":"top","src":"` + src.String() + `"}`
		},
	},
}

func parseJSON(t testing.TB, tn *Tabnas, src string) string {
	t.Helper()
	out, err := tn.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// The values the loops build, at the sizes the timing test uses and at
// the edges: one item, two, and a run that crosses appendSrc's
// threshold several times over.
func TestBuiltinSrcLoopValues(t *testing.T) {
	for _, c := range srcLoopCases {
		t.Run(c.name, func(t *testing.T) {
			tn := c.grammar(t)
			for _, n := range []int{1, 2, 3, 17, 100, 1000, 10000} {
				if got, want := parseJSON(t, tn, c.input(n)), c.want(n); got != want {
					t.Fatalf("%d items: the value differs from the plain "+
						"concatenation (got %d bytes, want %d)",
						n, len(got), len(want))
				}
			}
		})
	}
}

// appendSrc hands out strings that alias a buffer it keeps extending, so
// the thing to prove is that no string it has handed out ever changes,
// whoever appends next and to which of them.
func TestAppendSrcKeepsEveryStringItHandedOut(t *testing.T) {
	ctx := &Context{}
	long := strings.Repeat("x", srcTailMin)

	a := appendSrc(ctx, long, "a") // a new buffer
	ab := appendSrc(ctx, a, "b")   // a is its whole: may extend in place
	ac := appendSrc(ctx, a, "c")   // a is now a prefix: must copy
	abd := appendSrc(ctx, ab, "d") // ab is the whole again: in place
	acE := appendSrc(ctx, ac, "E")
	abe := appendSrc(ctx, ab, "e") // ab is a prefix of abd now: copy

	for _, c := range []struct{ got, want string }{
		{a, long + "a"}, {ab, long + "ab"}, {ac, long + "ac"},
		{abd, long + "abd"}, {acE, long + "acE"}, {abe, long + "abe"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got[srcTailMin:], c.want[srcTailMin:])
		}
	}

	// A src the parse did not build, however long, is copied, not written
	// through; a slice of a built src is a new string, never a tail.
	foreign := strings.Repeat("f", 4*srcTailMin)
	if got := appendSrc(ctx, foreign, "g"); got != foreign+"g" ||
		foreign != strings.Repeat("f", 4*srcTailMin) {
		t.Errorf("foreign src: got a %d-byte result", len(got))
	}
	cut := abd[:len(abd)-1]
	if got := appendSrc(ctx, cut, "z"); got != long+"abz" || abd != long+"abd" {
		t.Errorf("prefix of a tail: got %q, abd now %q",
			got[srcTailMin:], abd[srcTailMin:])
	}

	// The two identities concatenation has, and the short and nil-context
	// paths, which never touch the table.
	if appendSrc(ctx, long, "") != long || appendSrc(ctx, "", long) != long {
		t.Error("appending or prepending nothing changed the src")
	}
	if appendSrc(ctx, "ab", "cd") != "abcd" || appendSrc(nil, long, "!") != long+"!" {
		t.Error("the plain concatenation paths")
	}
}

// Ten thousand appends to one src allocate a buffer a few dozen times,
// as the capacity grows geometrically; concatenation allocated ten
// thousand. Counted, not timed, so no runner's clock can blur it.
func TestAppendSrcAllocatesLogarithmically(t *testing.T) {
	item := strings.Repeat("y", 24)
	var got string
	allocs := testing.AllocsPerRun(1, func() {
		ctx := &Context{}
		src := ""
		for i := 0; i < 10000; i++ {
			src = appendSrc(ctx, src, item)
		}
		got = src
	})
	if got != strings.Repeat(item, 10000) {
		t.Fatalf("the src is not the concatenation (%d bytes)", len(got))
	}
	t.Logf("10,000 appends: %.0f allocations", allocs)
	if allocs > 200 {
		t.Errorf("10,000 appends allocated %.0f times; geometric growth "+
			"needs a few dozen", allocs)
	}
}

// allocatedBytes is what one parse of src allocates on the heap.
func allocatedBytes(t testing.TB, tn *Tabnas, src string) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := tn.Parse(src); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// The same bound as the timing test below, on the bytes a parse
// allocates rather than the time it takes: those do not depend on the
// runner's clock or load, so this half runs in -short mode too. A
// linear parse allocates about ten times as much for ten times the
// items; the quadratic copy allocated the sum of every prefix of the
// src, 88 to 94 times as much where this was measured.
func TestBuiltinSrcLoopAllocatesLinearly(t *testing.T) {
	for _, c := range srcLoopCases {
		t.Run(c.name, func(t *testing.T) {
			tn := c.grammar(t)
			small := allocatedBytes(t, tn, c.input(1000))
			large := allocatedBytes(t, tn, c.input(10000))
			ratio := float64(large) / float64(small)
			t.Logf("%s: 1,000 items %d bytes, 10,000 items %d bytes, ratio %.1f",
				c.name, small, large, ratio)
			if ratio > 30 {
				t.Errorf("%s: 10,000 items allocated %.1f times as much as "+
					"1,000 (%d bytes against %d); linear is about 10",
					c.name, ratio, large, small)
			}
		})
	}
}

// perParse is the wall time of one parse of src, measured over a batch
// that runs until it has used at least 150 ms, so a coarse clock
// (Windows advances in 15.6 ms steps) cannot read a fast parse as zero.
// The best of three batches is kept, which discards a batch a scheduler
// hiccup or a collection landed in.
func perParse(t testing.TB, tn *Tabnas, src string) time.Duration {
	t.Helper()
	const minBatch = 150 * time.Millisecond
	best := time.Duration(0)
	for sample := 0; sample < 3; sample++ {
		start := time.Now()
		runs := 0
		for {
			if _, err := tn.Parse(src); err != nil {
				t.Fatal(err)
			}
			runs++
			if elapsed := time.Since(start); elapsed >= minBatch {
				per := elapsed / time.Duration(runs)
				if best == 0 || per < best {
					best = per
				}
				break
			}
		}
	}
	return best
}

// A replace loop of 10,000 items costs about ten times one of 1,000.
// Linear time reads as a ratio near 10 (9 to 14 where this was
// measured); the quadratic copy this repairs read as 57 to 88 on the
// same machine. The bound, 30, leaves a slow or noisy runner three times
// the headroom a linear parse needs and still fails the quadratic one by
// a wide margin. Each item is 24 bytes, long enough that the copy, not
// the rest of the parse, is what a quadratic run spends its time on.
func TestBuiltinSrcAppendIsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	for _, c := range srcLoopCases {
		t.Run(c.name, func(t *testing.T) {
			tn := c.grammar(t)
			small := perParse(t, tn, c.input(1000))
			large := perParse(t, tn, c.input(10000))
			ratio := float64(large) / float64(small)
			t.Logf("%s: 1,000 items %v, 10,000 items %v, ratio %.1f",
				c.name, small, large, ratio)
			if ratio > 30 {
				t.Errorf("%s: 10,000 items took %.1f times as long as 1,000 "+
					"(%v against %v); linear is about 10", c.name, ratio,
					large, small)
			}
		})
	}
}

// go test -run '^$' -bench BuiltinSrcLoop -benchmem
func BenchmarkBuiltinSrcLoop(b *testing.B) {
	for _, c := range srcLoopCases {
		tn := c.grammar(b)
		for _, n := range []int{1000, 10000} {
			src := c.input(n)
			b.Run(fmt.Sprintf("%s/%d", c.name, n), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(src)))
				for i := 0; i < b.N; i++ {
					if _, err := tn.Parse(src); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
