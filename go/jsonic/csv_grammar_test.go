package jsonic

import tabnas "github.com/tabnas/parser/go"

import (
	"reflect"
	"testing"
)

// Minimal CSV grammar built directly on the tabnas parser API.
// Mirrors test/csv-grammar.test.js — the grammar treats comma-separated
// values as cells, newline-separated rows as records, and single tokens
// (text, number, string, keyword) as cell values. Empty cells become "";
// empty rows are dropped.
func makeCSV() *tabnas.Tabnas {
	j := Make()

	// pushBack keeps the replaced-rule chain in sync so the parent rule
	// always sees the latest slice through r.Parent.Child.Node. Go's
	// append may reallocate, which means a bare tail-call replacement
	// would leave the original row.Node stale — unlike JS arrays.
	pushBack := func(r *tabnas.Rule) {
		if r.Parent != tabnas.NoRule && r.Parent != nil &&
			r.Parent.Child != tabnas.NoRule && r.Parent.Child != nil {
			r.Parent.Child.Node = r.Node
		}
	}

	appendCell := func(r *tabnas.Rule, val any) {
		arr, _ := r.Node.([]any)
		r.Node = append(arr, val)
		pushBack(r)
	}

	// csv: outer list of rows. Fresh bo resets Node for each new parse.
	j.Rule("csv", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.BO = []tabnas.StateAction{func(r *tabnas.Rule, ctx *tabnas.Context) {
			r.Node = []any{}
		}}
		rs.Open = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
			{P: "row"},
		}
		rs.Close = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{{tabnas.TinLN}, {tabnas.TinZZ}}},
			{S: [][]tabnas.Tin{{tabnas.TinLN}}, R: "csvcont"},
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
		}
		rs.BC = []tabnas.StateAction{func(r *tabnas.Rule, ctx *tabnas.Context) {
			if cells, ok := r.Child.Node.([]any); ok && len(cells) > 0 {
				outer, _ := r.Node.([]any)
				r.Node = append(outer, cells)
			}
		}}
	})

	// csvcont: tail-call sibling of csv. Inherits the outer-list node so
	// the replace chain carries the rows accumulated so far.
	j.Rule("csvcont", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.Open = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
			{P: "row"},
		}
		rs.Close = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{{tabnas.TinLN}, {tabnas.TinZZ}}},
			{S: [][]tabnas.Tin{{tabnas.TinLN}}, R: "csvcont"},
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}},
		}
		rs.BC = []tabnas.StateAction{func(r *tabnas.Rule, ctx *tabnas.Context) {
			if cells, ok := r.Child.Node.([]any); ok && len(cells) > 0 {
				outer, _ := r.Node.([]any)
				r.Node = append(outer, cells)
			}
		}}
	})

	// row: handles the first cell (initialising the row slice) then hands
	// the continuation to rowcont. Row-ending tokens at open produce an
	// empty row, which csv.bc drops.
	j.Rule("row", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.Open = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{tabnas.TinSetVAL}, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				r.Node = []any{r.O[0].Val}
			}},
			{S: [][]tabnas.Tin{{tabnas.TinCA}}, B: 1, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				r.Node = []any{""}
			}},
			{S: [][]tabnas.Tin{{tabnas.TinLN}}, B: 1, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				r.Node = []any{}
			}},
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}, B: 1, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				r.Node = []any{}
			}},
		}
		rs.Close = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{{tabnas.TinCA}}, R: "rowcont"},
			{S: [][]tabnas.Tin{{tabnas.TinLN}}, B: 1},
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}, B: 1},
		}
	})

	// rowcont: continues appending cells into the row slice. pushBack
	// keeps row.Node (the node parent rule reads) synced after append.
	j.Rule("rowcont", func(rs *tabnas.RuleSpec, _ *tabnas.Parser) {
		rs.Open = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{tabnas.TinSetVAL}, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				appendCell(r, r.O[0].Val)
			}},
			{S: [][]tabnas.Tin{{tabnas.TinCA}}, B: 1, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				appendCell(r, "")
			}},
			{S: [][]tabnas.Tin{{tabnas.TinLN}}, B: 1, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				appendCell(r, "")
			}},
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}, B: 1, A: func(r *tabnas.Rule, ctx *tabnas.Context) {
				appendCell(r, "")
			}},
		}
		rs.Close = []*tabnas.AltSpec{
			{S: [][]tabnas.Tin{{tabnas.TinCA}}, R: "rowcont"},
			{S: [][]tabnas.Tin{{tabnas.TinLN}}, B: 1},
			{S: [][]tabnas.Tin{{tabnas.TinZZ}}, B: 1},
		}
	})

	// Select the custom start rule and drop tabnas-only extensions.
	// Keep #SP and #CM in IGNORE but let #LN reach the parser.
	j.SetOptions(tabnas.Options{
		Rule: &tabnas.RuleOptions{Start: "csv", Exclude: "tabnas,imp"},
		Lex:  &tabnas.LexOptions{EmptyResult: []any{}},
		TokenSet: map[string][]string{
			"IGNORE": {"#SP", "#CM"},
		},
	})

	return j
}

func runCSV(t *testing.T, name, src string, want []any) {
	t.Helper()
	j := makeCSV()
	got, err := j.Parse(src)
	if err != nil {
		t.Fatalf("%s: Parse(%q) error: %v", name, src, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: Parse(%q)\n  got:      %#v\n  expected: %#v",
			name, src, got, want)
	}
}

func TestCSVEmptyInput(t *testing.T) {
	runCSV(t, "empty-input", "", []any{})
}

func TestCSVSingleRow(t *testing.T) {
	runCSV(t, "single-row", "a,b,c",
		[]any{[]any{"a", "b", "c"}})
}

func TestCSVMultipleRows(t *testing.T) {
	runCSV(t, "multiple-rows", "a,b\nc,d",
		[]any{[]any{"a", "b"}, []any{"c", "d"}})
}

func TestCSVTrailingNewline(t *testing.T) {
	runCSV(t, "trailing-newline", "a,b,c\n",
		[]any{[]any{"a", "b", "c"}})
}

func TestCSVBlankLinesSkipped(t *testing.T) {
	runCSV(t, "blank-lines", "a,b\n\nc,d\n",
		[]any{[]any{"a", "b"}, []any{"c", "d"}})
}

func TestCSVNumbersParsed(t *testing.T) {
	runCSV(t, "numbers", "1,2,3",
		[]any{[]any{float64(1), float64(2), float64(3)}})
}

func TestCSVQuotedStrings(t *testing.T) {
	runCSV(t, "quoted", `"hello","world"`,
		[]any{[]any{"hello", "world"}})
}

func TestCSVMixedTypes(t *testing.T) {
	runCSV(t, "mixed", `a,1,"x",true`,
		[]any{[]any{"a", float64(1), "x", true}})
}

func TestCSVEmptyLeadingField(t *testing.T) {
	runCSV(t, "leading-empty", ",a,b",
		[]any{[]any{"", "a", "b"}})
}

func TestCSVEmptyMiddleField(t *testing.T) {
	runCSV(t, "middle-empty", "a,,b",
		[]any{[]any{"a", "", "b"}})
}

func TestCSVEmptyTrailingField(t *testing.T) {
	runCSV(t, "trailing-empty", "a,b,",
		[]any{[]any{"a", "b", ""}})
}

func TestCSVSingleCellRow(t *testing.T) {
	runCSV(t, "single-cell", "x\ny",
		[]any{[]any{"x"}, []any{"y"}})
}

func TestCSVKeywords(t *testing.T) {
	runCSV(t, "keywords", "true,false,null",
		[]any{[]any{true, false, nil}})
}
