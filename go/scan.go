// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import "unicode/utf8"

// scan.go — declarative single-byte state machine driver for the simpler
// matchers. Port of the TS scan-spec design (ts/src/lexer.ts): the space,
// line, comment-eatline, and string-body walks all have the shape "walk
// bytes, dispatch on (state, byte-class), emit position-tracking actions,
// stop when told". The driver below centralises that shape, and the spec
// builders are exposed via the util bag so plugin authors can build their
// own matchers on it.
//
// Each spec declares:
//   - InitialState          which state the walk starts in
//   - NClasses              how many byte-classes the spec uses
//   - ClassOf ([256]uint8)  per-byte class index
//   - Table   ([]int32)     action keyed on state*NClasses + class
//
// An action is a packed int32 — ScanStateMask bits hold the next state,
// plus three single-bit flags. The driver applies ScanConsume / ScanIsRow
// first, then transitions, then ScanStop. That ordering makes "consume the
// char that ends the match" express as ScanConsume|ScanStop, while "stop
// without consuming" is just ScanStop.
//
// UTF-8: ASCII bytes classify via the ClassOf table (fast path). Lead
// bytes >= 0x80 decode a full rune and classify it via Fallback — the
// analogue of the TS fallback class function for UTF-16 code units >=
// 256 — consuming the whole rune as one column. When Fallback is nil the
// driver runs in pure byte mode (continuation bytes classify via
// ClassOf), which is correct for specs whose stop classes are all ASCII.

// Scan action flags and state mask (TS: CONSUME, IS_ROW, CI_RESET, STOP,
// STATE_MASK).
const (
	ScanConsume   int32 = 1 << 16 // Consume the char and advance position.
	ScanIsRow     int32 = 1 << 17 // rI++ and cI = 1
	ScanCIReset   int32 = 1 << 18 // cI = 1 without rI++ (line chars in multi-line strings)
	ScanStop      int32 = 1 << 19 // End the walk after applying this action.
	ScanStateMask int32 = 0xffff  // Bits holding the next state.
)

// ScanSpec is a declarative byte-walk specification (TS: ScanSpec).
type ScanSpec struct {
	InitialState int        // State the walk starts in.
	NClasses     int        // Number of byte-classes the spec uses.
	ClassOf      [256]uint8 // Per-ASCII-byte class index.
	Table        []int32    // Packed action keyed on state*NClasses + class.

	// Fallback classifies non-ASCII runes (lead byte >= 0x80); when set, the
	// driver decodes the full UTF-8 rune and consumes it as one column. Nil
	// means pure byte mode. Mirrors the TS fallback class fn for codes >= 256.
	Fallback func(r rune) uint8
}

// ScanOut is caller-owned scratch holding the position a Scan ended at (TS: ScanOut).
type ScanOut struct {
	SI int // Source byte index reached.
	RI int // Row index reached.
	CI int // Column index reached.
}

// Scan walks src from (startSI, startRI, startCI) according to spec,
// writing the reached positions into out. Reports whether any byte was
// consumed (TS: scan).
//
// Takes raw position numbers rather than a Point because some callers
// (notably the comment matcher) track positions as locals against a
// sliced fwd string rather than on the lex's pnt.
func Scan(src string, startSI, startRI, startCI int, spec *ScanSpec, out *ScanOut) bool {
	sI := startSI
	rI := startRI
	cI := startCI
	srclen := len(src)
	ncls := spec.NClasses
	table := spec.Table
	state := spec.InitialState

	for sI < srclen {
		var cls int
		size := 1
		if b := src[sI]; b < 0x80 || spec.Fallback == nil {
			cls = int(spec.ClassOf[b])
		} else {
			r, rsize := utf8.DecodeRuneInString(src[sI:])
			size = rsize
			cls = int(spec.Fallback(r))
		}
		action := table[state*ncls+cls]

		if action&ScanConsume != 0 {
			sI += size
			if action&ScanIsRow != 0 {
				rI++
				cI = 1
			} else if action&ScanCIReset != 0 {
				cI = 1
			} else {
				cI++
			}
		}
		state = int(action & ScanStateMask)
		if action&ScanStop != 0 {
			break
		}
	}

	out.SI = sI
	out.RI = rI
	out.CI = cI
	return startSI < sI
}

// (state=0, class=NOT_LINE) -> stop
// (state=0, class=LINE)     -> consume, stay in 0
// (state=0, class=LINE+ROW) -> consume + row, stay in 0
var lineRunTable = []int32{
	ScanStop,
	ScanConsume,
	ScanConsume | ScanIsRow,
}

// BuildLineRunSpec builds a 3-class line-run spec from line/row char sets.
// Class 0 = not a line char, class 1 = line char, class 2 = line char that
// also advances the row counter. Used by the line matcher (when not in
// `single` mode) and by the comment matcher's eatline tails
// (TS: buildLineRunSpec).
func BuildLineRunSpec(lineChars, rowChars map[rune]bool) *ScanSpec {
	spec := &ScanSpec{InitialState: 0, NClasses: 3, Table: lineRunTable}
	for cc := 0; cc < 128; cc++ {
		if lineChars[rune(cc)] {
			if rowChars[rune(cc)] {
				spec.ClassOf[cc] = 2
			} else {
				spec.ClassOf[cc] = 1
			}
		}
	}
	spec.Fallback = func(r rune) uint8 {
		if lineChars[r] {
			if rowChars[r] {
				return 2
			}
			return 1
		}
		return 0
	}
	return spec
}

// (state=0, class=OUT) -> stop
// (state=0, class=IN)  -> consume col, stay in 0
var charRunTable = []int32{
	ScanStop,
	ScanConsume,
}

// BuildCharRunSpec builds a 2-class run spec from a char set. Class 0 =
// not in set, class 1 = in set. Used by the space matcher
// (TS: buildCharRunSpec).
func BuildCharRunSpec(chars map[rune]bool) *ScanSpec {
	spec := &ScanSpec{InitialState: 0, NClasses: 2, Table: charRunTable}
	for cc := 0; cc < 128; cc++ {
		if chars[rune(cc)] {
			spec.ClassOf[cc] = 1
		}
	}
	spec.Fallback = func(r rune) uint8 {
		if chars[r] {
			return 1
		}
		return 0
	}
	return spec
}

// (s=0, BODY)         -> consume + col
// (s=0, STOP)         -> stop, caller dispatches on src[sI]
// (s=0, LINE_NONROW)  -> consume + cI=1 (multi-line)
// (s=0, LINE_ROW)     -> consume + rI++; cI=1 (multi-line)
var stringBodyTable = []int32{
	ScanConsume,
	ScanStop,
	ScanConsume | ScanCIReset,
	ScanConsume | ScanIsRow,
}

// BuildStringBodySpec builds a string-body scan spec for one quote char.
// Class 0 = BODY (consume, advance col); class 1 = STOP (caller decides
// what to do); class 2 = LINE (multi-line strings only — consume, reset
// col); class 3 = LINE+ROW (multi-line — consume, reset col, advance
// row). The opening/closing quote, the escape char, the replace chars,
// and any control char that can't be consumed in the current quote
// context all map to class 1.
//
// One spec per quote char because the quote char is encoded in the class
// table; the string matcher caches them per config
// (TS: buildStringBodySpec).
func BuildStringBodySpec(cfg *LexConfig, q rune) *ScanSpec {
	isMultiLine := cfg.MultiChars[q]
	classify := func(r rune) uint8 {
		switch {
		case r == q:
			return 1
		case r == cfg.EscapeChar:
			return 1
		case hasKey(cfg.StringReplace, r):
			return 1
		case r < 32:
			if isMultiLine && cfg.LineChars[r] {
				if cfg.RowChars[r] {
					return 3
				}
				return 2
			}
			return 1
		}
		return 0 // BODY
	}
	spec := &ScanSpec{InitialState: 0, NClasses: 4, Table: stringBodyTable}
	for cc := 0; cc < 128; cc++ {
		spec.ClassOf[cc] = classify(rune(cc))
	}
	spec.Fallback = classify
	return spec
}

// hasKey reports whether r is a replacement key (including empty-string
// replacements, which delete the char and must still stop the body run).
func hasKey(m map[rune]string, r rune) bool {
	_, ok := m[r]
	return ok
}

// Lazily built per-config scan specs. SetOptions overwrites the LexConfig
// contents in place (*j.parser.Config = *cfg) without preserving these
// caches, so they rebuild against the updated option values.

func (cfg *LexConfig) spaceRunSpec() *ScanSpec {
	if cfg.spaceSpec == nil {
		cfg.spaceSpec = BuildCharRunSpec(cfg.SpaceChars)
	}
	return cfg.spaceSpec
}

func (cfg *LexConfig) lineRunSpec() *ScanSpec {
	if cfg.lineSpec == nil {
		cfg.lineSpec = BuildLineRunSpec(cfg.LineChars, cfg.RowChars)
	}
	return cfg.lineSpec
}

func (cfg *LexConfig) stringBodySpec(q rune) *ScanSpec {
	if cfg.stringSpecs == nil {
		cfg.stringSpecs = make(map[rune]*ScanSpec, len(cfg.StringChars))
	}
	if spec, ok := cfg.stringSpecs[q]; ok {
		return spec
	}
	spec := BuildStringBodySpec(cfg, q)
	cfg.stringSpecs[q] = spec
	return spec
}

// buildScanSpecs eagerly populates the per-config scan specs (space run,
// line run, one string-body spec per configured quote char). Called at
// config build time so the shared LexConfig is read-only while parsing —
// left to the lazy getters above, the specs are built during the FIRST
// parse, which races when concurrent parses share one instance. The lazy
// getters remain as a fallback for hand-built configs.
func (cfg *LexConfig) buildScanSpecs() {
	cfg.spaceSpec = BuildCharRunSpec(cfg.SpaceChars)
	cfg.lineSpec = BuildLineRunSpec(cfg.LineChars, cfg.RowChars)
	cfg.stringSpecs = make(map[rune]*ScanSpec, len(cfg.StringChars))
	for q := range cfg.StringChars {
		cfg.stringSpecs[q] = BuildStringBodySpec(cfg, q)
	}
}
