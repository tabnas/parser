// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package tabnas

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// A binary-format grammar built directly on the bare tabnas engine. This
// is the Go mirror of ts/test/binary-grammar.test.js (the canonical
// version) and of rs/tests/binary_grammar_test.rs; the three build
// byte-identical input and assert byte-identical output, so keep them in
// step.
//
// Go needs no transcoding step: a Go string IS a byte sequence, Src[i]
// is a byte, and Src[a:b] shares the backing array rather than copying.
// TypeScript reaches the same place through latin1 (byte <-> UTF-16 code
// unit) and Rust through a byte-to-char mapping; see doc/guide.md
// "Parse a binary format", doc/differences.md and ../rs/README.md.
//
// The format, identical in all three ports:
//
//	magic    "TBN1"               fixed-width literal
//	count    u16 big-endian       fixed-width integer
//	records  until the sentinel   {
//	  id     u8                   fixed-width integer
//	  name   NUL-terminated       termination marker
//	  len    LEB128 varint        self-terminating variable width
//	  data   `len` bytes          variable length supplied by the rule
//	}
//	end      0x00 0xFF            two-byte sentinel

// --- byte helpers (mirror the TS ones) -------------------------------

func binVarint(n int) string {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n /= 128
		if 0 < n {
			b |= 0x80
		}
		out = append(out, b)
		if 0 == n {
			break
		}
	}
	return string(out)
}

func binU16be(n int) string {
	return string([]byte{byte((n >> 8) & 0xff), byte(n & 0xff)})
}

func binRecord(id int, name string, data string) string {
	return string([]byte{byte(id)}) + name + "\x00" +
		binVarint(len(data)) + data
}

const binSentinel = "\x00\xff"

func binFile(records ...string) string {
	return "TBN1" + binU16be(len(records)) + strings.Join(records, "") +
		binSentinel
}

// --- the grammar -----------------------------------------------------

func makeBinaryGrammar() *Tabnas {
	return makeBinaryGrammarWith(func(tkn *Token) any {
		return hex.EncodeToString([]byte(tkn.Src))
	})
}

// makeBinaryGrammarWith builds the grammar with a caller-chosen payload
// handler. The hex default above is what the corpus assertions compare;
// the allocation probe passes one that touches nothing, so what it
// measures is the engine and not the handler.
func makeBinaryGrammarWith(onData func(tkn *Token) any) *Tabnas {
	no := false

	j := Make(Options{
		Rule: &RuleOptions{Start: "file", Exclude: "tabnas,imp"},

		// Nothing in a binary stream is ignorable: a byte that means
		// nothing here means something at the next offset.
		TokenSet: map[string][]string{"IGNORE": {}},

		// Switch off every text-oriented built-in. Unlike TypeScript,
		// Go cannot remove them from the pipeline: the matcher order is
		// hard-coded in Lex.Next and each built-in is gated by its own
		// Config flag, so a disabled one costs a boolean test rather
		// than a call. See doc/differences.md "The matcher pipeline is a
		// list in TS and fixed in Go".
		Fixed:   &FixedOptions{Lex: &no},
		Space:   &SpaceOptions{Lex: &no},
		Line:    &LineOptions{Lex: &no},
		Text:    &TextOptions{Lex: &no},
		Number:  &NumberOptions{Lex: &no},
		Comment: &CommentOptions{Lex: &no},
		String:  &StringOptions{Lex: &no},
		Value:   &ValueOptions{Lex: &no},

		// Byte offsets, not rows and columns: a binary stream has no
		// lines. The placeholder is `{pos}` here and `{sI}` in
		// TypeScript: the two runtimes build the template reference bag
		// differently (Go has a fixed key set in errInjectRef, TS
		// spreads the token), and error message text is explicitly not
		// in parity. See go/doc/differences.md "Error template
		// placeholders" and ../DIVERGENCE.md "Not divergences".
		Error: map[string]string{
			"unexpected": "no field matches at byte offset {pos}",
		},
	})

	// Allocate the tins up front. Declaration order is tin order, which
	// is the tie-break within a rule's expected-token column, so #END
	// must come before #U8 or the catch-all byte matcher would take the
	// sentinel. Match.TokenOrder below pins the same order for the
	// function matchers themselves.
	tinMAGIC := j.Token("#MAGIC")
	tinEND := j.Token("#END")
	tinU16 := j.Token("#U16")
	tinU8 := j.Token("#U8")
	tinVARINT := j.Token("#VARINT")
	tinCSTR := j.Token("#CSTR")
	tinDATA := j.Token("#DATA")

	// Matchers go under Match.TokenFn, NOT Lex.Match. Only the former is
	// gated on the rule's expected-token column, and that gating is what
	// makes binary parsable at all: the bytes do not say what they are,
	// so the grammar has to.
	j.SetOptions(Options{Match: &MatchOptions{
		TokenOrder: []string{
			"#MAGIC", "#END", "#U16", "#U8", "#VARINT", "#CSTR", "#DATA",
		},
		TokenFn: map[string]LexMatcher{

			// Fixed-width literal.
			"#MAGIC": func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				if !strings.HasPrefix(lex.Src[pnt.SI:], "TBN1") {
					return nil
				}
				tkn := lex.Token("#MAGIC", tinMAGIC, "TBN1", "TBN1")
				pnt.SI += 4
				pnt.CI += 4
				return tkn
			},

			// Two-byte sentinel. Lower tin than #U8, so it wins the
			// column.
			"#END": func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				if pnt.Len < pnt.SI+2 ||
					0x00 != lex.Src[pnt.SI] || 0xff != lex.Src[pnt.SI+1] {
					return nil
				}
				tkn := lex.Token("#END", tinEND, true, lex.Src[pnt.SI:pnt.SI+2])
				pnt.SI += 2
				pnt.CI += 2
				return tkn
			},

			// Fixed-width integers.
			"#U16": func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				if pnt.Len < pnt.SI+2 {
					return nil
				}
				val := int(lex.Src[pnt.SI])<<8 | int(lex.Src[pnt.SI+1])
				tkn := lex.Token("#U16", tinU16, val, lex.Src[pnt.SI:pnt.SI+2])
				pnt.SI += 2
				pnt.CI += 2
				return tkn
			},

			"#U8": func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				if pnt.Len <= pnt.SI {
					return nil
				}
				val := int(lex.Src[pnt.SI])
				tkn := lex.Token("#U8", tinU8, val, lex.Src[pnt.SI:pnt.SI+1])
				pnt.SI++
				pnt.CI++
				return tkn
			},

			// Self-terminating variable width: the high bit says "more".
			"#VARINT": func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				i := pnt.SI
				val := 0
				shift := uint(0)
				for {
					if pnt.Len <= i {
						return nil
					}
					b := lex.Src[i]
					i++
					val += int(b&0x7f) << shift
					shift += 7
					if 0 == b&0x80 {
						break
					}
				}
				tkn := lex.Token("#VARINT", tinVARINT, val, lex.Src[pnt.SI:i])
				pnt.CI += i - pnt.SI
				pnt.SI = i
				return tkn
			},

			// Termination marker: run up to and including the next NUL.
			// The token's Src covers the terminator; its Val does not.
			"#CSTR": func(lex *Lex, rule *Rule) *Token {
				pnt := lex.Cursor()
				rel := strings.IndexByte(lex.Src[pnt.SI:], 0x00)
				if rel < 0 {
					return nil
				}
				end := pnt.SI + rel
				tkn := lex.Token("#CSTR", tinCSTR, lex.Src[pnt.SI:end],
					lex.Src[pnt.SI:end+1])
				pnt.CI += end + 1 - pnt.SI
				pnt.SI = end + 1
				return tkn
			},

			// Variable length supplied by the RULE, not by the bytes. K
			// propagates from the rule that lexed the length; U would
			// not. Src is a slice of the source string, so no bytes are
			// copied here.
			"#DATA": func(lex *Lex, rule *Rule) *Token {
				if rule == nil {
					return nil
				}
				raw, ok := rule.K["datalen"]
				if !ok {
					return nil
				}
				n, ok := raw.(int)
				if !ok || n < 0 {
					return nil
				}
				pnt := lex.Cursor()
				if pnt.Len < pnt.SI+n {
					return nil
				}
				tkn := lex.Token("#DATA", tinDATA, nil, lex.Src[pnt.SI:pnt.SI+n])
				pnt.SI += n
				pnt.CI += n
				return tkn
			},
		},
	}})

	j.Rule("file", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{tinMAGIC}, {tinU16}},
			A: func(r *Rule, ctx *Context) {
				r.Node = map[string]any{
					"magic":   r.O[0].Val,
					"count":   r.O[1].Val,
					"records": []any{},
				}
			},
			P: "records",
		})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})

	// An alternate that leads to a fetch must NAME the token it expects,
	// or column gating locks every matcher out and the fetch yields #BD.
	// `{P: "rec"}` alone names nothing; `{S: [][]Tin{{tinU8}}, B: 1, P:
	// "rec"}` matches the byte, pushes it back, and hands it to the
	// child.
	j.Rule("records", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(
			&AltSpec{S: [][]Tin{{tinEND}}},
			&AltSpec{S: [][]Tin{{tinU8}}, B: 1, P: "rec"},
		)
		rs.AddClose(
			&AltSpec{S: [][]Tin{{tinEND}}},
			&AltSpec{S: [][]Tin{{tinU8}}, B: 1, R: "records"},
		)
		rs.AddBC(func(r *Rule, ctx *Context) {
			if r.Child == nil || r.Child == NoRule || r.Child.Node == nil {
				return
			}
			node, ok := r.Node.(map[string]any)
			if !ok {
				return
			}
			node["records"] = append(node["records"].([]any), r.Child.Node)
		})
	})

	j.Rule("rec", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{tinU8}, {tinCSTR}, {tinVARINT}},
			A: func(r *Rule, ctx *Context) {
				r.Node = map[string]any{
					"id":   r.O[0].Val,
					"name": r.O[1].Val,
					"len":  r.O[2].Val,
				}
				r.EnsureK()["datalen"] = r.O[2].Val
			},
			P: "data",
		})
		rs.AddClose(&AltSpec{A: func(r *Rule, ctx *Context) {
			r.Node.(map[string]any)["data"] = r.Child.Node
		}})
	})

	j.Rule("data", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{
			S: [][]Tin{{tinDATA}},
			A: func(r *Rule, ctx *Context) {
				r.Node = onData(r.O[0])
			},
		})
	})

	return j
}

// --- the shared corpus -----------------------------------------------
//
// Byte-identical in all three ports. "alpha" carries an embedded NUL and
// an 0xFF inside its payload, which is what proves the stream is read as
// bytes and not as text; the third record forces a two-byte LEB128
// length.

func binCorpus() string {
	return binFile(
		binRecord(1, "alpha", "\x00\x01\xff"),
		binRecord(2, "", ""),
		binRecord(200, strings.Repeat("x", 200), strings.Repeat("y", 200)),
	)
}

func binExpected() map[string]any {
	return map[string]any{
		"magic": "TBN1",
		"count": 3,
		"records": []any{
			map[string]any{"id": 1, "name": "alpha", "len": 3, "data": "0001ff"},
			map[string]any{"id": 2, "name": "", "len": 0, "data": ""},
			map[string]any{
				"id":   200,
				"name": strings.Repeat("x", 200),
				"len":  200,
				"data": strings.Repeat("79", 200),
			},
		},
	}
}

func TestBinaryGrammarParsesSharedCorpus(t *testing.T) {
	j := makeBinaryGrammar()
	out, err := j.Parse(binCorpus())
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !reflect.DeepEqual(out, binExpected()) {
		t.Errorf("got %#v", out)
	}
}

func TestBinaryGrammarReadsEveryByteValue(t *testing.T) {
	// All 256 byte values reach the grammar unchanged. 0x00 is excluded
	// from the NAME (it terminates it) but not from the payload.
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	j := makeBinaryGrammar()
	out, err := j.Parse(binFile(binRecord(7, "all", string(all))))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	recs := out.(map[string]any)["records"].([]any)
	if 1 != len(recs) {
		t.Fatalf("got %d records", len(recs))
	}
	rec := recs[0].(map[string]any)
	if 256 != rec["len"] {
		t.Errorf("len: got %v", rec["len"])
	}
	if hex.EncodeToString(all) != rec["data"] {
		t.Errorf("data mismatch")
	}
	data := rec["data"].(string)
	if "000102" != data[:6] || "fdfeff" != data[len(data)-6:] {
		t.Errorf("byte range: %s ... %s", data[:6], data[len(data)-6:])
	}
}

func TestBinaryGrammarCarriesLengthFromRuleToLexer(t *testing.T) {
	// The payload matcher has no way to know where the field ends: the
	// length lives in a token the parser has already consumed. Change
	// only the declared length and the same bytes cut differently.
	j := makeBinaryGrammar()
	body := "abcdef"
	for _, n := range []int{0, 1, 3, 6} {
		src := "TBN1" + binU16be(1) + "\x01" + "n" + "\x00" +
			binVarint(n) + body[:n] + binSentinel
		out, err := j.Parse(src)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		rec := out.(map[string]any)["records"].([]any)[0].(map[string]any)
		if n != rec["len"] {
			t.Errorf("n=%d: len %v", n, rec["len"])
		}
		if hex.EncodeToString([]byte(body[:n])) != rec["data"] {
			t.Errorf("n=%d: data %v", n, rec["data"])
		}
	}
}

func TestBinaryGrammarTokenSpansAreByteOffsets(t *testing.T) {
	j := makeBinaryGrammar()
	type span struct {
		name string
		si   int
		n    int
	}
	var seen []span
	j.Sub(func(tkn *Token, rule *Rule, ctx *Context) {
		seen = append(seen, span{tkn.Name, tkn.SI, len(tkn.Src)})
	}, nil)
	if _, err := j.Parse(binFile(binRecord(1, "ab", "\xff\xfe"))); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	first := func(name string) span {
		for _, s := range seen {
			if s.name == name {
				return s
			}
		}
		return span{}
	}
	// TBN1(0..4) count(4..6) id(6) name "ab\0"(7..10) len(10) data(11..13)
	for _, want := range []span{
		{"#MAGIC", 0, 4}, {"#U16", 4, 2}, {"#U8", 6, 1},
		{"#CSTR", 7, 3}, {"#VARINT", 10, 1}, {"#DATA", 11, 2},
	} {
		if got := first(want.name); got != want {
			t.Errorf("%s: got %+v want %+v", want.name, got, want)
		}
	}
}

func TestBinaryGrammarReportsByteOffsets(t *testing.T) {
	j := makeBinaryGrammar()

	// Bad magic: nothing matches at offset 0.
	_, err := j.Parse("XXXX" + binU16be(0) + binSentinel)
	if err == nil {
		t.Fatal("bad magic: expected an error")
	}
	if !strings.Contains(err.Error(), "byte offset 0") {
		t.Errorf("bad magic: %v", err)
	}

	// Truncated payload: the length says 9, only 3 bytes remain.
	short := "TBN1" + binU16be(1) + "\x01" + "n" + "\x00" + binVarint(9) + "abc"
	if _, err := j.Parse(short); err == nil {
		t.Error("truncated: expected an error")
	}

	// Unterminated name: no NUL anywhere after it.
	noterm := "TBN1" + binU16be(1) + "\x01" + "name-with-no-nul"
	if _, err := j.Parse(noterm); err == nil {
		t.Error("unterminated name: expected an error")
	}
}

func TestBinaryGrammarDataTokenDoesNotCopyItsBytes(t *testing.T) {
	// Go substrings share the backing array, so a payload token costs no
	// copy however large the payload is. Measured rather than asserted:
	// if Src were a copy, the allocated bytes per parse would grow by
	// roughly the payload size. TypeScript reaches the same place with a
	// deferred (sI, len) span; Rust cannot, and pays the copy, because
	// its Token.src is an owned TokenText (../rs/README.md).
	bytesPerParse := func(payload int) int64 {
		// Length only: the handler allocates nothing that scales with
		// the payload, so any growth below is the engine copying.
		j := makeBinaryGrammarWith(func(tkn *Token) any { return len(tkn.Src) })
		src := binFile(binRecord(1, "n", strings.Repeat("z", payload)))
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := j.Parse(src); err != nil {
					b.Fatal(err)
				}
			}
		})
		return res.AllocedBytesPerOp()
	}

	small := bytesPerParse(64)
	large := bytesPerParse(64 * 1024)
	grew := large - small

	// A copying implementation would grow by at least the 64KiB payload.
	// Allow generous headroom for the engine's own per-parse garbage.
	if grew > 16*1024 {
		t.Errorf("payload token appears to copy: %d bytes -> %d bytes "+
			"(grew %d for a 64KiB payload)", small, large, grew)
	}
}

// --- sub-byte fields --------------------------------------------------
//
// The engine has no bit cursor: Point.SI counts bytes. A bitfield
// grammar keeps the bit offset on Ctx.U and emits tokens whose Src
// covers only the bytes the field fully crossed, so a field that sits
// inside a byte already consumed produces a zero-length token.

func makeBitfieldGrammar() *Tabnas {
	no := false
	j := Make(Options{
		Rule:     &RuleOptions{Start: "ipv4", Exclude: "tabnas,imp"},
		TokenSet: map[string][]string{"IGNORE": {}},
		Fixed:    &FixedOptions{Lex: &no},
		Space:    &SpaceOptions{Lex: &no},
		Line:     &LineOptions{Lex: &no},
		Text:     &TextOptions{Lex: &no},
		Number:   &NumberOptions{Lex: &no},
		Comment:  &CommentOptions{Lex: &no},
		String:   &StringOptions{Lex: &no},
		Value:    &ValueOptions{Lex: &no},
	})

	tinBITS := j.Token("#BITS")

	j.SetOptions(Options{Match: &MatchOptions{
		TokenFn: map[string]LexMatcher{
			"#BITS": func(lex *Lex, rule *Rule) *Token {
				if rule == nil {
					return nil
				}
				raw, ok := rule.K["bits"]
				if !ok {
					return nil
				}
				width, ok := raw.(int)
				if !ok {
					return nil
				}
				ctx := lex.Ctx
				pnt := lex.Cursor()
				bit := 0
				if b, ok := ctx.U["bit"].(int); ok {
					bit = b
				}
				need := width
				val := 0
				i := pnt.SI
				for 0 < need {
					if pnt.Len <= i {
						return nil
					}
					avail := 8 - bit
					take := avail
					if need < take {
						take = need
					}
					b := int(lex.Src[i])
					val = val<<take | (b>>(avail-take))&((1<<take)-1)
					bit += take
					need -= take
					if 8 == bit {
						bit = 0
						i++
					}
				}
				if ctx.U == nil {
					ctx.U = map[string]any{}
				}
				ctx.U["bit"] = bit
				tkn := lex.Token("#BITS", tinBITS, val, lex.Src[pnt.SI:i])
				pnt.CI += i - pnt.SI
				pnt.SI = i
				return tkn
			},
		},
	}})

	// version(4) ihl(4) dscp(6) ecn(2): the first two bytes of an IPv4
	// header, four fields across two bytes.
	field := func(name string, width int, next string) {
		j.Rule(name, func(rs *RuleSpec, _ *Parser) {
			rs.AddBO(func(r *Rule, ctx *Context) {
				r.EnsureK()["bits"] = width
			})
			rs.AddOpen(&AltSpec{
				S: [][]Tin{{tinBITS}},
				A: func(r *Rule, ctx *Context) {
					r.Node = r.Parent.Node
					r.Node.(map[string]any)[name] = r.O[0].Val
				},
			})
			if "" == next {
				rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
			} else {
				rs.AddClose(&AltSpec{R: next})
			}
		})
	}

	j.Rule("ipv4", func(rs *RuleSpec, _ *Parser) {
		rs.AddBO(func(r *Rule, ctx *Context) { r.Node = map[string]any{} })
		rs.AddOpen(&AltSpec{P: "version"})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})

	field("version", 4, "ihl")
	field("ihl", 4, "dscp")
	field("dscp", 6, "ecn")
	field("ecn", 2, "")

	return j
}

func TestBinaryGrammarSubByteFields(t *testing.T) {
	j := makeBitfieldGrammar()
	for _, tc := range []struct {
		src  string
		want map[string]any
	}{
		// 0x45 0xb8 = 0100 0101 1011 1000
		//   version 0100 = 4, ihl 0101 = 5, dscp 101110 = 46, ecn 00 = 0
		{"\x45\xb8", map[string]any{
			"version": 4, "ihl": 5, "dscp": 46, "ecn": 0}},
		// 0x60 0x0f = 0110 0000 0000 1111
		//   version 6, ihl 0, dscp 000011 = 3, ecn 11 = 3
		{"\x60\x0f", map[string]any{
			"version": 6, "ihl": 0, "dscp": 3, "ecn": 3}},
	} {
		out, err := j.Parse(tc.src)
		if err != nil {
			t.Fatalf("%s: %v", fmt.Sprintf("%x", tc.src), err)
		}
		if !reflect.DeepEqual(out, tc.want) {
			t.Errorf("%x: got %#v want %#v", tc.src, out, tc.want)
		}
	}
}
