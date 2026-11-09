/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// A parse from a reader, a window of the source at a time, against the
// same text parsed whole (admin ADR-30, doc/chunked-source.md, "Proving
// it"), as rs/tests/window_test.rs holds the Rust engine to it and
// ts/test/window.test.js the TypeScript one. Every input of the parsing
// files of the spec register (windowParsed) is parsed by the grammar-free
// engine and the strict-JSON grammar, and the probe and rule-history rows
// by their own specs, each with lex.window set: once whole, and from a
// reader that
// hands the text over in pieces of 1, 2, 3, 7, 64 and 4096 bytes,
// splitting characters past ASCII between pieces, under windows whose
// margin and block are a few bytes, so that every matcher meets the
// window's edge. The value, the tokens the lexer gives, the rule passes
// and an error's code, position, row, column and token must be the same.
// An error's excerpt must be the same while the window holds the text, and
// a cut of it where the window has dropped the line's start: the one
// difference a parse from a reader shows.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// pieceReader hands its bytes over at most size at a time. With eofWith
// it gives io.EOF with its last bytes, as io.Reader allows.
type pieceReader struct {
	b       []byte
	at      int
	size    int
	eofWith bool
}

func (p *pieceReader) Read(buf []byte) (int, error) {
	if len(p.b) <= p.at {
		return 0, io.EOF
	}
	n := p.size
	if len(buf) < n {
		n = len(buf)
	}
	if len(p.b)-p.at < n {
		n = len(p.b) - p.at
	}
	copy(buf, p.b[p.at:p.at+n])
	p.at += n
	if p.eofWith && len(p.b) <= p.at {
		return n, io.EOF
	}
	return n, nil
}

func pieces(text string, size int) io.Reader {
	return &pieceReader{b: []byte(text), size: size, eofWith: 0 == size%2}
}

// failingReader hands over good bytes, a few at a time, and then fails.
type failingReader struct {
	b    []byte
	at   int
	good int
}

func (f *failingReader) Read(buf []byte) (int, error) {
	if f.good <= f.at {
		return 0, errors.New("the disk went away")
	}
	n := 3
	if len(buf) < n {
		n = len(buf)
	}
	if f.good-f.at < n {
		n = f.good - f.at
	}
	copy(buf, f.b[f.at:f.at+n])
	f.at += n
	return n, nil
}

// windowRow is a register row: its line, its input decoded, and the
// named spec document its spec column asks for, if any.
type windowRow struct {
	line  int
	input string
	doc   string
}

// windowRows reads a spec file's rows as rs/tests/window_test.rs does.
func windowRows(t *testing.T, file string) []windowRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(specDir(), file))
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	specs := map[string]string{}
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, "# @spec "); ok {
			if sp := strings.Index(rest, " "); 0 < sp {
				specs[rest[:sp]] = rest[sp+1:]
			}
		}
	}
	header := strings.Split(lines[0], "\t")
	input, spec := -1, -1
	for i, col := range header {
		switch col {
		case "input":
			input = i
		case "spec":
			spec = i
		}
	}
	if input < 0 {
		t.Fatalf("%s has no input column", file)
	}
	var out []windowRow
	for index, line := range lines {
		if index == 0 || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		row := windowRow{line: index + 1, input: preprocessEscapes(cols[input])}
		if 0 <= spec && spec < len(cols) {
			var args map[string]any
			if json.Unmarshal([]byte(cols[spec]), &args) == nil {
				if name, ok := args["spec"].(string); ok {
					doc, found := specs[name]
					if !found {
						t.Fatalf("%s: no # @spec %s", file, name)
					}
					row.doc = doc
				}
			}
		}
		out = append(out, row)
	}
	return out
}

// windowParsed are the register's files whose rows are parsed: those of
// rs/tests/window_test.rs but the jsonic loader's smoke test, which this
// engine does not run (nonParity, go/spec_registration_test.go: an exempt
// fixture is not wired into Go).
var windowParsed = []string{
	"bad-token.tsv",
	"diagnostic.tsv",
	"divergent.tsv",
	"include-json.tsv",
	"include-json-errors.tsv",
	"include-json-utf8.tsv",
	"include-json-utf8-errors.tsv",
	"lex-ender-array.tsv",
	"lex-string-control.tsv",
	"lex-text-line-terminator.tsv",
	"lex-text-quote.tsv",
	"probe.tsv",
	"repaired.tsv",
	"rule-history.tsv",
}

// The pieces a reader hands its bytes over in.
var windowPieces = []int{1, 2, 3, 7, 64, 4096}

// Windows whose margin and block are a few bytes, and one that holds every
// input of the register whole.
var windowWindows = [][2]int{{1, 1}, {3, 2}, {64, 64}, {4096, 4096}}

// windowLog records the tokens and rule passes of a parse.
type windowLog struct {
	tokens []string
	rules  []string
}

var windowOn = true

// windowed sets lex.window on j and records its tokens and rule passes
// into log.
func windowed(j *Tabnas, log *windowLog) *Tabnas {
	j.SetOptions(Options{Lex: &LexOptions{Window: &windowOn}})
	j.Sub(func(tkn *Token, _ *Rule, _ *Context) {
		log.tokens = append(log.tokens, fmt.Sprintf("%d %q %#v %d %d:%d",
			tkn.Tin, tkn.Src, tkn.Val, tkn.SI, tkn.RI, tkn.CI))
	}, nil)
	j.SubRuleDone(func(rule *Rule, _ *Context, done RuleDone) {
		log.rules = append(log.rules, fmt.Sprintf("%s %s %v", rule.Name, done.State, done.Probe))
	})
	return j
}

// windowSeen is what a parse showed: its value, or its error without the
// excerpt; its tokens and passes; and the error's excerpt, its line and
// its rendering.
type windowSeen struct {
	outcome  string
	tokens   []string
	rules    []string
	line     *string
	rendered string
}

type windowDiag struct {
	Code     string   `json:"code"`
	Pos      int      `json:"pos"`
	Row      int      `json:"row"`
	Col      int      `json:"col"`
	Len      int      `json:"len"`
	Rule     string   `json:"rule"`
	Expected []string `json:"expected"`
	Src      string   `json:"src"`
	Token    struct {
		Name string `json:"name"`
		Src  string `json:"src"`
	} `json:"token"`
}

func diagOf(t *testing.T, je *TabnasError) windowDiag {
	t.Helper()
	raw, err := json.Marshal(je)
	if err != nil {
		t.Fatalf("diagnostic: %v", err)
	}
	var d windowDiag
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("diagnostic: %v", err)
	}
	return d
}

func seenOf(t *testing.T, log *windowLog, parse func() (any, error)) windowSeen {
	t.Helper()
	var s windowSeen
	value, err := parse()
	var je *TabnasError
	switch {
	case err == nil:
		s.outcome = fmt.Sprintf("ok %#v", value)
	case errors.As(err, &je):
		d := diagOf(t, je)
		s.outcome = fmt.Sprintf("err %s at %d %d:%d %q %d %s %s %q",
			d.Code, d.Pos, d.Row, d.Col, d.Token.Src, d.Len, d.Token.Name, d.Rule, d.Expected)
		line := d.Src
		s.line = &line
		s.rendered = je.Error()
	default:
		s.outcome = "error " + err.Error()
	}
	s.tokens, s.rules = log.tokens, log.rules
	log.tokens, log.rules = nil, nil
	return s
}

// windowDifferential parses input whole and from a reader, under every
// pieces and window size, and returns the count of reader parses compared.
func windowDifferential(t *testing.T, at string, input string, j *Tabnas, log *windowLog) int {
	t.Helper()
	whole := seenOf(t, log, func() (any, error) { return j.Parse(input) })
	checked := 0
	for _, size := range windowPieces {
		for _, mb := range windowWindows {
			sizes := windowSizes{margin: mb[0], block: mb[1], chunk: size}
			read := seenOf(t, log, func() (any, error) {
				return j.parseReaderWithSizes(pieces(input, size), sizes, nil)
			})
			where := fmt.Sprintf("%s %q in pieces of %d, margin %d, block %d", at, input, size, mb[0], mb[1])
			if read.outcome != whole.outcome {
				t.Fatalf("%s:\n  read  %s\n  whole %s", where, read.outcome, whole.outcome)
			}
			if !reflect.DeepEqual(read.tokens, whole.tokens) {
				t.Fatalf("%s: tokens\n  read  %q\n  whole %q", where, read.tokens, whole.tokens)
			}
			if !reflect.DeepEqual(read.rules, whole.rules) {
				t.Fatalf("%s: rules\n  read  %q\n  whole %q", where, read.rules, whole.rules)
			}
			if len(input) <= mb[0] {
				// The window holds the text whole: the same excerpt.
				if !reflect.DeepEqual(read.line, whole.line) || read.rendered != whole.rendered {
					t.Fatalf("%s: excerpt\n  read  %q\n  whole %q", where, read.rendered, whole.rendered)
				}
			} else if read.line != nil && whole.line != nil && !strings.Contains(*whole.line, *read.line) {
				// Cut where the window begins, and read on past its end no
				// further than its margin.
				t.Fatalf("%s: %q is no cut of %q", where, *read.line, *whole.line)
			}
			checked++
		}
	}
	return checked
}

func TestWindowEveryParsedRowParsesTheSameFromAReader(t *testing.T) {
	log := &windowLog{}
	plain := windowed(Make(), log)
	strict := windowed(makeJSON(), log)
	checked := 0
	for index, file := range windowParsed {
		for _, row := range windowRows(t, file) {
			at := fmt.Sprintf("%s:%d", file, row.line)
			if row.doc != "" {
				gs, err := GrammarSpecFromJSON([]byte(row.doc))
				if err != nil {
					continue
				}
				j := Make()
				// A spec the register expects to refuse installing
				// (ERROR:install) has no parse to compare.
				if err := j.Grammar(gs); err != nil {
					continue
				}
				checked += windowDifferential(t, at, row.input, windowed(j, log), log)
				continue
			}
			checked += windowDifferential(t, at, row.input, plain, log)
			checked += windowDifferential(t, at, row.input, strict, log)
		}
		t.Logf("window differential: %d of %d files (%d%%), %d reader parses",
			index+1, len(windowParsed), (index+1)*100/len(windowParsed), checked)
	}
	if checked <= 10000 {
		t.Fatalf("only %d reader parses", checked)
	}
}

// windowStream is a grammar that takes every token the lexer gives, with
// every built-in matcher on and fixed tokens of one, two and three
// characters, under the options: a token stream, so that each matcher's
// cut, at a window's edge too, is what the test compares.
func windowStream(t *testing.T, options string) *Tabnas {
	t.Helper()
	doc := `{"options":{"rule":{"start":"top"},` +
		`"fixed":{"token":{"#AR":"=>","#EL":"...","#DT":".","#EQ":"="}}` + options + `},` +
		`"rule":{"top":{"open":[{"s":"#ZZ"},{"s":"#AA","r":"top"}]}}}`
	gs, err := GrammarSpecFromJSON([]byte(doc))
	if err != nil {
		t.Fatalf("stream grammar: %v", err)
	}
	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatalf("stream grammar: %v", err)
	}
	return j
}

// windowCrafted reaches the matchers the strict-JSON grammar has off:
// comments with suffixes and eaten lines, escapes cut short, numbers in
// every base, multi-character fixed tokens, value keywords, line runs.
var windowCrafted = []string{
	"a # comment\n b // c\n /* block\n comment */ d",
	`"\u{1F600}" "\x41" "\u0041" 'single' ` + "`multi\nline`",
	"0x1F 0o17 0b101 1_000 1.5e3 -2 +3 .5 0xFF.5 1e 1e+ 0x",
	"a=>b...c.d=e",
	"true false null truex nul",
	"x\r\ny\n\n\nz\r\r\n",
	"/* unterminated",
	`"unterminated`,
	`"bad \q escape"`,
	`"\u{110000}"`,
	`"\u{12"`,
	`"\x4"`,
	`"\u12"`,
	`ñ ü € 𝄞 "𝄞" é=>ö`,
	"abc --x--> def -- y - z (* c\n *) w",
	"   \t  \n  ",
	"{a:1,b:[2,3]}",
	"a\u2028b",
}

func TestWindowEveryMatcherMeetsTheWindowEdgeAsAWholeTextHasIt(t *testing.T) {
	// Beyond the register's grammars, whose lexers leave text, comments
	// and most of the fixed tokens off: a token stream over every
	// built-in matcher, with lines cut singly in one and strings
	// abandoned and comments with suffixes in another.
	log := &windowLog{}
	grammars := []*Tabnas{
		windowed(windowStream(t, ""), log),
		windowed(windowStream(t, `,"line":{"single":true}`), log),
		windowed(windowStream(t, `,"string":{"abandon":true},"comment":{"def":{`+
			`"dash":{"line":true,"start":"--","lex":true,"suffix":["-->","-"]},`+
			`"block":{"line":false,"start":"(*","end":"*)","lex":true,"eatline":true}}}`), log),
	}
	inputs := append([]string(nil), windowCrafted...)
	for _, file := range windowParsed {
		for _, row := range windowRows(t, file) {
			inputs = append(inputs, row.input)
		}
	}
	checked := 0
	for index, j := range grammars {
		for line, input := range inputs {
			checked += windowDifferential(t, fmt.Sprintf("stream %d:%d", index, line), input, j, log)
		}
		t.Logf("window stream: %d of %d grammars (%d%%), %d reader parses",
			index+1, len(grammars), (index+1)*100/len(grammars), checked)
	}
	if checked <= 20000 {
		t.Fatalf("only %d reader parses", checked)
	}
}

// windowRecords is a long text of records, one a line, past ASCII when
// name is.
func windowRecords(count int, name, sep string) string {
	out := make([]string, count)
	for i := range out {
		out[i] = fmt.Sprintf(`{"id":%d,"%s":"v%d","tags":["a","b"]}`, i, name, i)
	}
	return "[" + strings.Join(out, sep) + "]"
}

func TestWindowALongSourceParsesTheSameFromAReader(t *testing.T) {
	log := &windowLog{}
	j := windowed(makeJSON(), log)
	// 5,000 records, some 250 KB: several windows of 64 KiB, at a cost
	// this suite can carry, where rs/tests/window_test.rs takes 20,000.
	for _, name := range []string{"n", "ñ"} {
		text := windowRecords(5000, name, ",\n")
		whole := seenOf(t, log, func() (any, error) { return j.Parse(text) })
		if !strings.HasPrefix(whole.outcome, "ok ") {
			t.Fatalf("%s: %.80s", name, whole.outcome)
		}
		read := seenOf(t, log, func() (any, error) { return j.ParseReader(strings.NewReader(text)) })
		if !reflect.DeepEqual(read, whole) {
			t.Fatalf("%s: a parse from a reader differs from the text's", name)
		}
		small := windowSizes{margin: 16, block: 16, chunk: 7}
		cut := seenOf(t, log, func() (any, error) {
			return j.parseReaderWithSizes(pieces(text, 5), small, nil)
		})
		if cut.outcome != whole.outcome || !reflect.DeepEqual(cut.tokens, whole.tokens) ||
			!reflect.DeepEqual(cut.rules, whole.rules) {
			t.Fatalf("%s: a parse from a reader in a small window differs from the text's", name)
		}
	}
}

func TestWindowAParseFromAReaderHoldsABoundedWindow(t *testing.T) {
	// A block behind the cursor and a margin or two ahead of it, however
	// long the source, flat from 2,000 records to 20,000: the window's
	// text is what the lexer holds between tokens, Lex.Src.
	j := makeJSON(Options{Lex: &LexOptions{Window: &windowOn}})
	peak := 0
	j.Sub(func(_ *Token, _ *Rule, ctx *Context) {
		if peak < len(ctx.Lex.Src) {
			peak = len(ctx.Lex.Src)
		}
	}, nil)
	sizes := windowSizes{margin: 1024, block: 1024, chunk: 1024}
	var peaks []int
	for _, count := range []int{2000, 20000} {
		text := windowRecords(count, "name", ",")
		peak = 0
		if _, err := j.parseReaderWithSizes(strings.NewReader(text), sizes, nil); err != nil {
			t.Fatal(err)
		}
		if 4*1024 < peak {
			t.Fatalf("%d records held %d bytes", count, peak)
		}
		peaks = append(peaks, peak)
	}
	if peaks[0]+256 < peaks[1] {
		t.Fatalf("the window grew with the source: %v", peaks)
	}

	// A grammar that does not declare lex.window reads the reader whole.
	whole := makeJSON()
	held := 0
	whole.Sub(func(_ *Token, _ *Rule, ctx *Context) {
		if held < len(ctx.Lex.Src) {
			held = len(ctx.Lex.Src)
		}
	}, nil)
	text := windowRecords(2000, "name", ",")
	if _, err := whole.parseReaderWithSizes(strings.NewReader(text), sizes, nil); err != nil {
		t.Fatal(err)
	}
	if held != len(text) {
		t.Fatalf("a whole read held %d of %d bytes", held, len(text))
	}
}

// plainText is text without its terminal colour codes.
func plainText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestWindowALongLinesExcerptIsCutWhereTheWindowBegins(t *testing.T) {
	// An error on a line longer than the window shows the line from where
	// the window begins, and its caret stays under the error.
	j := makeJSON(Options{Lex: &LexOptions{Window: &windowOn}})
	text := "[" + strings.Repeat("1,", 200) + "x]"
	_, werr := j.Parse(text)
	_, rerr := j.parseReaderWithSizes(pieces(text, 4), windowSizes{margin: 8, block: 8, chunk: 4}, nil)
	var whole, read *TabnasError
	if !errors.As(werr, &whole) || !errors.As(rerr, &read) {
		t.Fatalf("both parses should fail: %v / %v", werr, rerr)
	}
	w, r := diagOf(t, whole), diagOf(t, read)
	if [4]any{r.Code, r.Pos, r.Row, r.Col} != [4]any{w.Code, w.Pos, w.Row, w.Col} {
		t.Fatalf("read %v, whole %v", r, w)
	}
	if w.Src != text {
		t.Fatalf("the whole line: %q", w.Src)
	}
	if len(w.Src) <= len(r.Src) || !strings.HasSuffix(w.Src, r.Src) {
		t.Fatalf("the cut line: %q", r.Src)
	}
	// The caret sits under the `x`, on the line as the window shows it.
	lines := strings.Split(plainText(read.Error()), "\n")
	shown := -1
	for i, line := range lines {
		if strings.HasSuffix(line, r.Src) {
			shown = i
			break
		}
	}
	if shown < 0 {
		t.Fatalf("the cut line is not shown:\n%s", read.Error())
	}
	x := len(lines[shown]) - len(r.Src) + strings.Index(r.Src, "x")
	if caret := strings.Index(lines[shown+1], "^"); caret != x {
		t.Fatalf("the caret is at %d, the x at %d:\n%s", caret, x, plainText(read.Error()))
	}
}

func TestWindowAnUnreadableSourceFailsWhereItFails(t *testing.T) {
	// A reader that fails, and bytes that are not UTF-8, fail the parse
	// with unreadable where the source failed, whether the grammar reads a
	// window at a time or the reader whole.
	for _, j := range []*Tabnas{makeJSON(Options{Lex: &LexOptions{Window: &windowOn}}), makeJSON()} {
		fail := func(r io.Reader, sizes windowSizes) (*TabnasError, windowDiag) {
			t.Helper()
			_, err := j.parseReaderWithSizes(r, sizes, nil)
			var je *TabnasError
			if !errors.As(err, &je) {
				t.Fatalf("the parse should fail: %v", err)
			}
			return je, diagOf(t, je)
		}
		text := []byte("[1,\n2,\n3333,\n4]")
		failed, f := fail(&failingReader{b: text, good: 9}, defaultWindowSizes)
		if f.Code != "unreadable" || [3]int{f.Pos, f.Row, f.Col} != [3]int{9, 3, 3} {
			t.Fatalf("a failed read: %+v", f)
		}
		if !strings.Contains(failed.Error(), "the disk went away") || f.Token.Src != "the disk went away" {
			t.Fatalf("a failed read: %s", failed.Error())
		}

		bytes := append([]byte("[1,\n\"a"), 0xff)
		bytes = append(bytes, []byte("b\"]")...)
		invalid, i := fail(pieces(string(bytes), 2), windowSizes{margin: 2, block: 2, chunk: 2})
		if i.Code != "unreadable" || [3]int{i.Pos, i.Row, i.Col} != [3]int{6, 2, 3} {
			t.Fatalf("bytes that are not UTF-8: %+v", i)
		}
		if i.Token.Src != notUTF8 || invalid.Detail != "the source could not be read: "+notUTF8 {
			t.Fatalf("bytes that are not UTF-8: %q", invalid.Detail)
		}

		// A character the source ends inside.
		cut := []byte("[\"ñ")
		_, c := fail(pieces(string(cut[:len(cut)-1]), 4096), defaultWindowSizes)
		if c.Code != "unreadable" || [3]int{c.Pos, c.Row, c.Col} != [3]int{2, 1, 3} {
			t.Fatalf("a character the source ends inside: %+v", c)
		}
	}
}

func TestWindowASourceThatFailsBeforeItsFirstCharacterIsNotEmpty(t *testing.T) {
	// An empty source parses as an empty one; one that fails at once fails
	// with unreadable, at the start.
	empty := true
	j := makeJSON(Options{Lex: &LexOptions{Window: &windowOn, Empty: &empty, EmptyResult: "E"}})
	if out, err := j.ParseReader(strings.NewReader("")); err != nil || out != "E" {
		t.Fatalf("an empty source: %v, %v", out, err)
	}
	_, err := j.ParseReader(&failingReader{good: 0})
	var je *TabnasError
	if !errors.As(err, &je) {
		t.Fatalf("a source that fails at once: %v", err)
	}
	if d := diagOf(t, je); d.Code != "unreadable" || [3]int{d.Pos, d.Row, d.Col} != [3]int{0, 1, 1} {
		t.Fatalf("a source that fails at once: %+v", d)
	}
}

// stalledReader gives nothing, and no error, every time.
type stalledReader struct{}

func (stalledReader) Read([]byte) (int, error) { return 0, nil }

func TestWindowAReaderThatGivesNothingFails(t *testing.T) {
	// io.Reader discourages a read that gives neither bytes nor an error;
	// one that only does fails as bufio.Reader fails it, and a nil reader
	// is a source that cannot be read.
	for _, j := range []*Tabnas{makeJSON(Options{Lex: &LexOptions{Window: &windowOn}}), makeJSON()} {
		_, err := j.ParseReader(stalledReader{})
		var je *TabnasError
		if !errors.As(err, &je) || je.Code != "unreadable" || je.Src != io.ErrNoProgress.Error() {
			t.Fatalf("a reader that gives nothing: %v", err)
		}
		_, err = j.ParseReader(nil)
		if !errors.As(err, &je) || je.Code != "unreadable" {
			t.Fatalf("a nil reader: %v", err)
		}
	}
}

func TestWindowAByteOrderMarkIsTheSourcesOwnCharacter(t *testing.T) {
	// As Rust's UTF-8 decoding and a parse of the text keep it.
	text := "\uFEFF[1]"
	outcome := func(out any, err error) string { return fmt.Sprintf("%#v %v", out, err != nil) }
	j := makeJSON(Options{Lex: &LexOptions{Window: &windowOn}})
	whole := outcome(j.Parse(text))
	if read := outcome(j.ParseReader(pieces(text, 1))); read != whole {
		t.Fatalf("read %s, whole %s", read, whole)
	}
	if read := outcome(makeJSON().ParseReader(pieces(text, 1))); read != whole {
		t.Fatalf("read whole %s, whole %s", read, whole)
	}
}

func TestWindowLexWindowSurvivesAMerge(t *testing.T) {
	// From either side. The side left unset is the default, as Go's merge
	// reads an option set on neither, or one, side (go/merge.go).
	tagged := func(tag string, window bool) *Tabnas {
		opts := Options{Tag: tag}
		if window {
			opts.Lex = &LexOptions{Window: &windowOn}
		}
		return makeJSON(opts)
	}
	merged := func(a, b *Tabnas) bool {
		m, err := a.Merge(b)
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		return m.Config().Window
	}
	if !merged(tagged("A", true), tagged("B", false)) || !merged(tagged("A", false), tagged("B", true)) {
		t.Fatal("lex.window did not survive a merge")
	}
	if merged(tagged("A", false), tagged("B", false)) {
		t.Fatal("lex.window came from nowhere in a merge")
	}
}

func TestWindowAMatcherReadsTheWindowCountingFromItsStart(t *testing.T) {
	// In a parse from a reader a window at a time, a matcher sees the
	// window: Lex.Src is its text, and the cursor, its length included,
	// counts from its start, so that Lex.Src[cursor.SI] is the byte under
	// the cursor as in a parse of the whole text, where the length is the
	// source's (DIVERGENCE.md, "What a parse from a reader shows of its
	// source"). The tokens it makes carry the source's positions.
	type probe struct {
		len, held int
		at        byte
	}
	probed := func(log *[]probe, positions *[]int) *Tabnas {
		gs, err := GrammarSpecFromJSON([]byte(`{"clear":true,` +
			`"options":{"rule":{"start":"top"},"lex":{"window":true}},` +
			`"rule":{"top":{"open":[{"s":"#TX","r":"top"},{"s":"#ZZ"}]}}}`))
		if err != nil {
			t.Fatal(err)
		}
		j := Make()
		if err := j.Grammar(gs); err != nil {
			t.Fatal(err)
		}
		j.SetOptions(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
			"probe": {Order: 1, Make: func(_ *LexConfig, _ *Options) LexMatcher {
				return func(lex *Lex, _ *Rule) *Token {
					cur := lex.Cursor()
					*log = append(*log, probe{cur.Len, len(lex.Src), lex.Src[cur.SI]})
					return nil
				}
			}},
		}}})
		j.Sub(func(tkn *Token, _ *Rule, _ *Context) { *positions = append(*positions, tkn.SI) }, nil)
		return j
	}
	text := strings.Repeat("a ", 200)
	var read []probe
	var readAt []int
	if _, err := probed(&read, &readAt).parseReaderWithSizes(pieces(text, 4),
		windowSizes{margin: 4, block: 4, chunk: 4}, nil); err != nil {
		t.Fatal(err)
	}
	if len(read) == 0 {
		t.Fatal("the matcher never ran")
	}
	for _, p := range read {
		if p.len != p.held || len(text) <= p.held || (p.at != 'a' && p.at != ' ') {
			t.Fatalf("a matcher over a window saw %+v", p)
		}
	}
	var whole []probe
	var wholeAt []int
	if _, err := probed(&whole, &wholeAt).Parse(text); err != nil {
		t.Fatal(err)
	}
	for _, p := range whole {
		if p.len != len(text) {
			t.Fatalf("a matcher over the text saw %+v", p)
		}
	}
	if !reflect.DeepEqual(readAt, wholeAt) {
		t.Fatalf("token positions differ:\n  read  %v\n  whole %v", readAt, wholeAt)
	}
}

func TestWindowAWindowedContextsSrcIsEmpty(t *testing.T) {
	// Where a parse of the whole text gives the source: a grammar that
	// declares lex.window reads the text through the lexer's window.
	var sources []string
	j := makeJSON(Options{Lex: &LexOptions{Window: &windowOn}})
	j.Sub(nil, func(_ *Rule, ctx *Context) { sources = append(sources, ctx.Src) })
	text := `{"a":[1,2,{"b":null}]}`
	if _, err := j.ParseReader(strings.NewReader(text)); err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no rule ran")
	}
	for _, s := range sources {
		if s != "" {
			t.Fatalf("a windowed context's source: %q", s)
		}
	}
	sources = nil
	if _, err := j.Parse(text); err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if s != text {
			t.Fatalf("a context's source: %q", s)
		}
	}
}

func TestWindowAGrammarThatReadsItsSourceWholeReadsAReaderWhole(t *testing.T) {
	// No lex.window, error recovery, or a Parser.Start: the reader is read
	// to its end first, and the text parsed.
	text := `{"a":[1,2,{"b":null}]}`
	want, err := makeJSON().Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := makeJSON().ParseReader(pieces(text, 3)); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("no lex.window: %#v, %v", got, err)
	}
	recovering := makeJSON(Options{
		Lex:   &LexOptions{Window: &windowOn},
		Parse: &ParseOptions{Recover: &RecoverOptions{Enabled: true}},
	})
	got, gerr := recovering.ParseReader(pieces(text, 3))
	wantR, werr := recovering.Parse(text)
	if !reflect.DeepEqual(got, wantR) || (gerr == nil) != (werr == nil) {
		t.Fatalf("recovery: %#v, %v", got, gerr)
	}
	started := Make(Options{
		Lex: &LexOptions{Window: &windowOn},
		Parser: &ParserOptions{Start: func(src string, _ *Tabnas, _ map[string]any) (any, error) {
			return "parsed:" + src, nil
		}},
	})
	if got, err := started.ParseReader(pieces(text, 3)); err != nil || got != "parsed:"+text {
		t.Fatalf("parser.start: %#v, %v", got, err)
	}
}
