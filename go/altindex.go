package tabnas

// altIndex is a parse-scoped index over one rule state's alternates.
//
// byTin maps each tin some alternate names at position 0 to the
// ascending indices of the alternates naming it; wild holds the
// alternates that constrain nothing at position 0 (an empty sequence, or
// a wildcard slot), which are candidates for every tin. ParseAlts walks
// the two lists together in index order, so the candidates for a tin are
// exactly the original scan with the alternates that cannot take it left
// out, and first-match-wins is preserved. The lists stay separate rather
// than being merged per tin: a rule with W wildcard alternates and T
// distinct first tins would otherwise cost W×T entries, per parse.
//
// cols is the lexer's gate: cols[slot][tin] is true when some alternate
// names tin at that lookahead slot (exact membership, as the gate has
// always tested; #AA is a plain tin here, since a wildcard says the
// PARSER will take any tin, not that the lexer should invent one). It
// replaces a walk of every alternate's slot, with a Context.altS lookup
// each, for every candidate match token on every lex attempt, which was
// most of the parse time on a grammar with hundreds of alternates.
//
// The index is built from the slots as the parsing instance resolves
// them (Context.altS), so a token set overridden on the instance is
// honoured, and it lives on the Context: one build per rule state per
// parse, no invalidation and no lock, for the same reason altSlots is
// per-Context. gen is the RuleSpec's mutation count when the index was
// built: a list changed by an action during a parse (an append, a
// prepend, a reorder through ModifyOpen) moves it, and the next step
// through the rule rebuilds.
type altIndex struct {
	gen   uint64
	n     int
	byTin map[Tin][]int32
	wild  []int32
	cols  [][]bool
}

type altIndexKey struct {
	spec *RuleSpec
	open bool
}

// altIndex returns the index for one rule state, building it on first
// use in this parse.
func (ctx *Context) altIndex(spec *RuleSpec, isOpen bool, alts []*AltSpec) *altIndex {
	key := altIndexKey{spec: spec, open: isOpen}
	if idx, ok := ctx.altIdx[key]; ok && idx.gen == spec.gen && idx.n == len(alts) {
		return idx
	}
	idx := buildAltIndex(ctx, alts)
	idx.gen = spec.gen
	if ctx.altIdx == nil {
		ctx.altIdx = make(map[altIndexKey]*altIndex)
	}
	ctx.altIdx[key] = idx
	return idx
}

func buildAltIndex(ctx *Context, alts []*AltSpec) *altIndex {
	idx := &altIndex{
		n:     len(alts),
		byTin: make(map[Tin][]int32),
		wild:  make([]int32, 0),
	}
	maxTin := Tin(-1)
	maxSlots := 0
	for _, alt := range alts {
		altS := ctx.altS(alt)
		if maxSlots < len(altS) {
			maxSlots = len(altS)
		}
		for _, slot := range altS {
			for _, tin := range slot {
				if maxTin < tin {
					maxTin = tin
				}
			}
		}
	}
	idx.cols = make([][]bool, maxSlots)
	for s := range idx.cols {
		idx.cols[s] = make([]bool, maxTin+1)
	}
	for aI, alt := range alts {
		altS := ctx.altS(alt)
		for s, slot := range altS {
			for _, tin := range slot {
				if 0 <= tin {
					idx.cols[s][tin] = true
				}
			}
		}
		// No position-0 constraint: an empty sequence, an empty slot, or
		// a slot naming #AA (tinMatch accepts every tin for it).
		if len(altS) == 0 || len(altS[0]) == 0 || hasTin(altS[0], TinAA) {
			idx.wild = append(idx.wild, int32(aI))
			continue
		}
		for _, tin := range altS[0] {
			list := idx.byTin[tin]
			// A slot naming one tin twice must not try the alternate
			// twice: a condition function could observe it.
			if 0 < len(list) && list[len(list)-1] == int32(aI) {
				continue
			}
			idx.byTin[tin] = append(list, int32(aI))
		}
	}
	return idx
}

// named is the ascending list of alternates naming tin at position 0,
// never nil; the wildcards are idx.wild, walked beside it.
func (idx *altIndex) named(tin Tin) []int32 {
	if list, ok := idx.byTin[tin]; ok {
		return list
	}
	return noAlts
}

var noAlts = []int32{}

// expects reports whether some alternate names tin at the lookahead
// slot: the match-token gate.
func (idx *altIndex) expects(slot int, tin Tin) bool {
	if slot < 0 || len(idx.cols) <= slot {
		return false
	}
	col := idx.cols[slot]
	return 0 <= tin && tin < len(col) && col[tin]
}

func hasTin(tins []Tin, want Tin) bool {
	for _, t := range tins {
		if t == want {
			return true
		}
	}
	return false
}
