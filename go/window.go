// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

// A parse from a reader, a window of the source at a time (admin ADR-30
// and ADR-33, doc/chunked-source.md), as the Rust engine reads one
// (rs/src/lexer.rs, `Window`) and the TypeScript engine does
// (ts/src/lexer.ts, `SourceWindow`).
//
// The lexer keeps the text from the oldest token the parser may cut again
// (its lookahead, the tokens a rewind replays and its consumed history) to
// what it has read. Before each token it fills the window to a margin past
// the cursor, and a token whose cut read to the window's end while the
// source had more is cut again over a window twice as far ahead, so that it
// is the token the whole text gives. While a token is cut, Lex.Src is the
// window's text and the cursor counts from the window's start, so the
// matchers read the window as they read a whole text; every token the cut
// makes then takes the source's positions, and its own copy of its text.

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// windowSizes are a window's sizes: the margin ahead of the cursor and the
// dead text gathered before it is dropped, in bytes, and a read's length.
// Each is 64 KiB (admin ADR-30), and none is an option: only the
// differential test makes them a few bytes (parseReaderWithSizes), so that
// every matcher meets the window's edge.
type windowSizes struct {
	margin int
	block  int
	chunk  int
}

var defaultWindowSizes = windowSizes{margin: 64 << 10, block: 64 << 10, chunk: 64 << 10}

// notUTF8 is the reason a source is not UTF-8, as Rust's read_to_string
// gives it (rs/src/lexer.rs NOT_UTF8), so that the three runtimes say the
// same.
const notUTF8 = "stream did not contain valid UTF-8"

// maxEmptyReads is how many reads in a row may give neither a byte nor an
// error before the reader counts as failed: io.Reader discourages such
// reads, and bufio.Reader gives up on them after as many.
const maxEmptyReads = 100

// unreadable is a source that could not be read to its end: why, and the
// position (bytes), row and column (runes) after the last character read,
// where the parse fails.
type unreadable struct {
	reason string
	pos    int
	row    int
	col    int
}

// utf8Prefix returns the length of the longest prefix of b that is
// well-formed UTF-8 (as Rust's str::from_utf8 reads it), and whether what
// follows it can never be: an ill-formed sequence, or, once the source has
// ended (final), a character it ends inside. A character split at the end
// of b otherwise waits for its last bytes.
func utf8Prefix(b []byte, final bool) (int, bool) {
	i := 0
	for i < len(b) {
		if b[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			if !final && !utf8.FullRune(b[i:]) {
				return i, false
			}
			return i, true
		}
		i += size
	}
	return i, false
}

// positionAfter is where text ends, as a failure's position counts it: its
// bytes, and the row and column after its last character, a line feed
// starting a row.
func positionAfter(text string) (int, int, int) {
	row := 1 + strings.Count(text, "\n")
	col := 1 + utf8.RuneCountInString(text[strings.LastIndexByte(text, '\n')+1:])
	return len(text), row, col
}

// readWhole reads r to its end, as a grammar that reads its source whole
// reads it (rs/src/parser.rs read_whole): the text, and why the source
// ended early if it did, a read that failed or bytes that are not UTF-8,
// at the character after the longest prefix that decodes. A failed read is
// the reason even where bytes before it do not decode, as in Rust.
func readWhole(r io.Reader, chunk int) (string, *unreadable) {
	if chunk < 1 {
		chunk = 1
	}
	buf := make([]byte, chunk)
	var all []byte
	failed := false
	reason := ""
	empties := 0
	for {
		n, err := r.Read(buf)
		if n < 0 || n > len(buf) {
			failed = true
			reason = fmt.Sprintf("the reader read %d bytes into %d", n, len(buf))
			break
		}
		all = append(all, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			failed = true
			reason = err.Error()
			break
		}
		if n == 0 {
			if empties++; maxEmptyReads <= empties {
				failed = true
				reason = io.ErrNoProgress.Error()
				break
			}
		} else {
			empties = 0
		}
	}
	valid, bad := utf8Prefix(all, true)
	if !failed && bad {
		failed = true
		reason = notUTF8
	}
	text := string(all[:valid])
	if !failed {
		return text, nil
	}
	pos, row, col := positionAfter(text)
	return text, &unreadable{reason: reason, pos: pos, row: row, col: col}
}

// sourceWindow is the window over a source read from a reader: the text
// from low, the oldest position the parser may still ask for, to what has
// been read. Positions are the source's, in bytes from its start.
type sourceWindow struct {
	r    io.Reader
	text string // The text read and kept, from low on.
	low  int    // The source position of text[0].

	// The runes before low, so that a diagnostic's pos, which counts
	// runes, stays exact once the window has moved on.
	lowRunes int

	// The row and column of text[0], counted as an error's excerpt counts
	// them: a line feed starts a row, and every rune is a column.
	lowRow int
	lowCol int

	// The row and column after the last character read, counted the same
	// way, which place a failure.
	readRow int
	readCol int

	// The UTF-16 units read, which the parser's step budget is measured
	// against as it grows.
	utf16 int

	exhausted bool        // The reader has said there is no more.
	failure   *unreadable // Why the source ended early, and where.
	margin    int         // How far past the cursor the window reaches.
	block     int         // Dead text gathered before it is dropped.
	buf       []byte      // What a read fills, kept from read to read.
	pending   []byte      // Bytes read and not yet decoded.
	empties   int         // Reads in a row that gave nothing.
}

func newSourceWindow(r io.Reader, sizes windowSizes) *sourceWindow {
	if sizes.margin < 1 {
		sizes.margin = 1
	}
	if sizes.block < 1 {
		sizes.block = 1
	}
	if sizes.chunk < 1 {
		sizes.chunk = 1
	}
	return &sourceWindow{
		r:       r,
		lowRow:  1,
		lowCol:  1,
		readRow: 1,
		readCol: 1,
		margin:  sizes.margin,
		block:   sizes.block,
		buf:     make([]byte, sizes.chunk),
	}
}

// end is the source position after the last character read.
func (w *sourceWindow) end() int {
	return w.low + len(w.text)
}

// read makes one read. Its bytes are decoded as far as they are whole
// characters, and a character the read split waits for the next; the bytes
// come first, as io.Reader has it, whatever the read also says. A read
// that fails or answers a count it cannot have, and bytes that are not
// UTF-8, end the source where they stand, and the parse fails there.
func (w *sourceWindow) read() {
	n, err := w.r.Read(w.buf)
	if n < 0 || n > len(w.buf) {
		w.fail(fmt.Sprintf("the reader read %d bytes into %d", n, len(w.buf)))
		return
	}
	if n == 0 && err == nil {
		if w.empties++; maxEmptyReads <= w.empties {
			w.fail(io.ErrNoProgress.Error())
		}
		return
	}
	w.empties = 0
	eof := err == io.EOF
	w.take(w.buf[:n], eof)
	if err != nil && !eof {
		w.fail(err.Error())
	} else if eof {
		w.exhausted = true
	}
}

// take decodes the bytes a read gave after those still pending.
func (w *sourceWindow) take(b []byte, final bool) {
	if 0 < len(w.pending) {
		b = append(w.pending, b...)
		w.pending = nil
	}
	valid, bad := utf8Prefix(b, final)
	if 0 < valid {
		w.append(string(b[:valid]))
	}
	if bad {
		w.fail(notUTF8)
	} else if valid < len(b) {
		w.pending = append([]byte(nil), b[valid:]...)
	}
}

// append adds decoded text to the window, counting the row and column
// after it.
func (w *sourceWindow) append(s string) {
	if last := strings.LastIndexByte(s, '\n'); last < 0 {
		w.readCol += utf8.RuneCountInString(s)
	} else {
		w.readRow += strings.Count(s, "\n")
		w.readCol = 1 + utf8.RuneCountInString(s[last+1:])
	}
	w.utf16 += utf16Len(s)
	w.text += s
}

// fail ends the source here: the parse fails with reason at the character
// after the last one read, at the next token.
func (w *sourceWindow) fail(reason string) {
	w.exhausted = true
	if w.failure == nil {
		w.failure = &unreadable{reason: reason, pos: w.end(), row: w.readRow, col: w.readCol}
	}
}

// drop lets go of the text before the source position keep, keeping the
// row, column and runes the window then begins at.
func (w *sourceWindow) drop(keep int) {
	n := keep - w.low
	if n <= 0 {
		return
	}
	dropped := w.text[:n]
	if last := strings.LastIndexByte(dropped, '\n'); last < 0 {
		w.lowCol += utf8.RuneCountInString(dropped)
	} else {
		w.lowRow += strings.Count(dropped, "\n")
		w.lowCol = 1 + utf8.RuneCountInString(dropped[last+1:])
	}
	w.lowRunes += utf8.RuneCountInString(dropped)
	w.text = w.text[n:]
	w.low = keep
}

// newWindowLex creates a lexer that reads a window of the source w reads.
func newWindowLex(w *sourceWindow, cfg *LexConfig) *Lex {
	l := NewLex("", cfg)
	l.win = w
	l.Src = w.text
	l.pnt.Len = w.end()
	return l
}

// Windowed reports whether the lexer reads a window of a source read from
// a reader (Tabnas.ParseReader).
func (l *Lex) Windowed() bool {
	return l.win != nil
}

// windowOpen reports whether the lexer reads a window of a source that has
// more to give, so that a read at the window's end may not be the
// source's.
func (l *Lex) windowOpen() bool {
	return l.win != nil && !l.win.exhausted
}

// touchEnd is the built-in matchers' note that a read reached the end of
// the window while the source has more to give: the token being cut may be
// cut short, and is cut again over a wider window. Nothing in a whole
// text.
func (l *Lex) touchEnd() {
	if l.win != nil && !l.win.exhausted {
		l.touched = true
	}
}

// Window returns the text from the cursor to the end of what the lexer
// holds: in a parse from a reader, the end of the window. A matcher in a
// grammar that declares lex.window reads the source through this and Fill.
func (l *Lex) Window() string {
	return l.restAt(l.pnt.SI)
}

// Fill reads the source until ahead bytes lie past the cursor, or it ends,
// and reports whether they do. A whole source is all read, so this only
// answers. Text is never dropped while a token is being cut, and a source
// that fails to read ends where it failed, the parse failing with it at
// the next token. Src is the grown window's text after a fill.
func (l *Lex) Fill(ahead int) bool {
	if l.win != nil {
		l.grow(ahead)
	}
	return ahead <= l.pnt.Len-l.pnt.SI
}

// restAt returns the text the lexer holds from the source position si on:
// outside a cut over a window the cursor counts from the source's start,
// and the window's text from its own.
func (l *Lex) restAt(si int) string {
	at := si
	if l.win != nil && !l.cutting {
		at -= l.win.low
	}
	if at < 0 || at > len(l.Src) {
		return ""
	}
	return l.Src[at:]
}

// lengthRead is how much of the source has been read, in UTF-16 units: the
// whole text's length, or what the window has read, which the parser's
// step budget is measured against as it grows.
func (l *Lex) lengthRead() int {
	if l.win == nil {
		return utf16Len(l.Src)
	}
	return l.win.utf16
}

// isEmpty reports whether the source turns out empty: the window is
// filled until it holds a character or the reader is done. A source that
// fails before its first character is not empty: its failure is the
// parse's error.
func (w *sourceWindow) isEmpty() bool {
	for !w.exhausted && w.end() == 0 {
		w.read()
	}
	return w.end() == 0 && w.failure == nil
}

// grow fills the window until ahead bytes lie past the cursor or the
// source is exhausted.
func (l *Lex) grow(ahead int) {
	w := l.win
	at := l.pnt.SI
	if l.cutting {
		at += l.off
	}
	if w.exhausted || ahead <= w.end()-at {
		return
	}
	for !w.exhausted && w.end()-at < ahead {
		w.read()
	}
	l.synced()
}

// synced follows a change to the window's text: Src and the cursor's
// length follow it.
func (l *Lex) synced() {
	l.Src = l.win.text
	l.pnt.Len = l.win.end()
	if l.cutting {
		l.pnt.Len -= l.off
	}
}

// oldestHeld is the oldest source position among tokens older than keep,
// or keep: how far back the window must still reach for them. Sentinel and
// unset slots are skipped, and the scan stops once the answer falls below
// floor, when nothing can be dropped anyway.
func oldestHeld(tokens []*Token, keep, floor int) int {
	for i := 0; i < len(tokens) && floor <= keep; i++ {
		t := tokens[i]
		if t != nil && t != NoToken && 0 <= t.Tin && 0 <= t.SI && t.SI < keep {
			keep = t.SI
		}
	}
	return keep
}

// release drops the text before the oldest token the parser holds, once a
// block of it has gathered behind the cursor: the parser's lookahead, the
// tokens a rewind replays, and its consumed history, which a rewind may
// replay and a re-cut then read again. The space, lines and comments after
// the last token the parser does not ignore are kept, as the Rust engine
// keeps them until the parser fetches its next lookahead. Never during a
// negotiated cut, which sets the replay queue aside (Relex).
func (l *Lex) release() {
	w := l.win
	floor := w.low + w.block
	if l.pnt.SI < floor || l.want != nil {
		return
	}
	keep := l.pnt.SI
	if l.keepFrom < keep {
		keep = l.keepFrom
	}
	if ctx := l.Ctx; ctx != nil {
		keep = oldestHeld(ctx.T, keep, floor)
		keep = oldestHeld(ctx.V, keep, floor)
		keep = oldestHeld([]*Token{ctx.T0, ctx.T1, ctx.V1, ctx.V2}, keep, floor)
	}
	keep = oldestHeld(l.tokens, keep, floor)
	if keep < floor {
		return
	}
	w.drop(keep)
	l.synced()
}

// enderReach is how far past the point it is tried at an ender of the text
// and number matchers can read, in bytes: the longest of the fixed tokens,
// the multi-character enders and the comment markers, or a character. A
// cut over a window that could read past the window's end may differ from
// the whole text's, and is made again over a wider window.
func (l *Lex) enderReach() int {
	if 0 < l.reach {
		return l.reach
	}
	reach := utf8.UTFMax
	longest := func(s string) {
		if reach < len(s) {
			reach = len(s)
		}
	}
	for _, fs := range l.Config.FixedSorted {
		longest(fs)
	}
	for fs := range l.Config.FixedTokens {
		longest(fs)
	}
	for _, es := range l.Config.EnderSeqs {
		longest(es)
	}
	for _, cs := range l.Config.CommentLine {
		longest(cs)
	}
	for _, cb := range l.Config.CommentBlock {
		longest(cb[0])
	}
	l.reach = reach
	return reach
}

// isNumberish reports whether a byte can be part of a number: a digit, a
// letter (a base marker, a hex digit or an exponent), a sign, a point, or
// the separator.
func (l *Lex) isNumberish(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') ||
		c == '.' || c == '-' || c == '+' ||
		(l.Config.NumberSep != 0 && rune(c) == l.Config.NumberSep)
}

// settle gives a token whose cut over the window stands the source's
// positions, and its own copy of its text: the window moves on and drops
// what lies behind it, and a token, or a string value it carries, must not
// keep the window's text alive. A value the parse interned keeps the
// interned copy.
func (l *Lex) settle(tkn *Token, base int) {
	if tkn == nil {
		return
	}
	tkn.SI += base
	tkn.Src = ownString(tkn.Src)
	if s, ok := tkn.Val.(string); ok {
		if pooled, ok := l.intern[s]; ok {
			tkn.Val = pooled
		} else {
			tkn.Val = strings.Clone(s)
		}
	}
}

// ownString is a copy of s that shares no storage with the window.
func ownString(s string) string {
	if len(s) == 1 && s[0] < utf8.RuneSelf {
		return asciiByteStr[s[0]]
	}
	return strings.Clone(s)
}

// cutWindow cuts one token from the window. The window is filled to its
// margin past the cursor first, having dropped what no token the parser
// holds can reach; the matchers then run over it as over a whole text, the
// cursor counting from the window's start. A cut that read to the window's
// end while the source had more is undone and made again over a window
// twice as far ahead, so that it is the cut the whole text gives. A cut
// that stands gives its tokens the source's positions and their own text.
// It returns nil where no matcher claimed the position, as nextRaw does.
func (l *Lex) cutWindow(rule *Rule) *Token {
	if l.cutting {
		// A matcher that asks the lexer for a token while one is being
		// cut gets one cut in the window's coordinates, which the cut it
		// runs in settles: nothing moves or is dropped under that cut.
		// At the window's end it is the source's end only once the
		// source is.
		if l.pnt.SI >= l.pnt.Len {
			l.touchEnd()
			if l.win.exhausted {
				return l.Token("#ZZ", TinZZ, Undefined, "")
			}
			return nil
		}
		return l.cutRaw(rule)
	}
	w := l.win
	ahead := w.margin
	for {
		l.release()
		l.grow(ahead)
		if w.failure != nil {
			// Raised at once, as Rust's lexer returns its error and
			// TypeScript's throws it: the parse ends here, with no
			// passes after (startParse recovers the panic as this error).
			panic(l.unreadableError(rule))
		}
		base := w.low
		saved := l.pnt
		queue := l.tokens
		qN := len(queue)
		end := l.end
		l.cutting = true
		l.off = base
		l.touched = false
		l.pnt.SI -= base
		l.pnt.Len = len(w.text)
		var tkn *Token
		func() {
			defer func() {
				l.cutting = false
				l.off = 0
				l.pnt.SI += base
				l.synced()
			}()
			if l.pnt.SI >= l.pnt.Len {
				// The source is exhausted at the cursor.
				l.end = l.Token("#ZZ", TinZZ, Undefined, "")
				tkn = l.end
			} else {
				tkn = l.cutRaw(rule)
			}
		}()
		if w.exhausted || (!l.touched && l.pnt.SI < l.pnt.Len) {
			l.settle(tkn, base)
			made := l.tokens
			from := qN
			if len(made) < qN || (0 < qN && &made[0] != &queue[0]) {
				from = 0
			}
			for _, t := range made[from:] {
				if t != tkn {
					l.settle(t, base)
				}
			}
			if l.end != nil && l.end != end && l.end != tkn {
				l.settle(l.end, base)
			}
			return tkn
		}
		l.pnt = saved
		l.tokens = queue[:qN]
		l.end = end
		ahead *= 2
	}
}

// unreadableError is the error a parse fails with when its source could
// not be read to its end: unreadable, the reason as its source text, at
// the character after the last one read (Rust's Unreadable::error).
func (l *Lex) unreadableError(r *Rule) *TabnasError {
	f := l.win.failure
	je := makeTabnasError("unreadable", f.reason, "", f.pos, f.row, f.col, l.Config)
	l.attachErrContext(je, r, "#BD", "unreadable")
	return je
}

// excerpt is the text an error shows its line from, the row and column it
// starts at, and the source position and the runes before it: in a parse
// from a reader the window, read on first to the end of its last line, no
// further than a margin, so that the line an error is on shows as a parse
// of the whole text shows it, but for a line longer than the window.
func (l *Lex) excerpt() (string, int, int, int, int) {
	w := l.win
	start := len(w.text)
	cut := start
	if !strings.HasSuffix(w.text, "\n") {
		nl := -1
		for {
			if i := strings.IndexByte(w.text[start:], '\n'); 0 <= i {
				nl = start + i
			}
			if 0 <= nl || w.exhausted || start+w.margin <= len(w.text) {
				break
			}
			w.read()
		}
		cut = len(w.text)
		if 0 <= nl {
			cut = nl + 1
		}
		if start+w.margin < cut {
			cut = start + w.margin
			// Back to the start of a character the margin splits.
			for start < cut && !utf8.RuneStart(w.text[cut]) {
				cut--
			}
		}
		l.synced()
	}
	return w.text[:cut], w.lowRow, w.lowCol, w.low, w.lowRunes
}

// attachExcerpt gives an error raised in a parse from a reader the text
// its line is shown from: the window's (TabnasError.setExcerpt). Once.
func (l *Lex) attachExcerpt(je *TabnasError) {
	if l == nil || l.win == nil || je == nil || 0 < je.srcRow {
		return
	}
	text, row, col, off, runes := l.excerpt()
	je.setExcerpt(text, row, col, off, runes)
}

// ParseReader parses a source read from r, its bytes decoded as UTF-8
// (admin ADR-30 and ADR-33, doc/chunked-source.md).
//
// A grammar that declares lex.window (Options.Lex.Window) is read a window
// at a time: the parse holds the text from the oldest token it may cut
// again to 64 KiB or so past its cursor, rather than all of it, and gives
// the same value, rule passes and errors as Parse of the same text, but for
// an error's excerpt, which shows a line only as far back as the window
// reaches. Any other grammar, and one with a Parser.Start or error
// recovery, reads the reader whole first and parses the text.
//
// In a parse a window at a time, Context.Src is empty, and a matcher reads
// the window: Lex.Src is its text and the cursor (Lex.Cursor) counts from
// its start while a token is cut, and Lex.Window and Lex.Fill read on. A
// reader that fails, or bytes that are not UTF-8, fail the parse with the
// code unreadable, where the source failed, error recovery or not.
func (j *Tabnas) ParseReader(r io.Reader) (any, error) {
	return j.parseReaderWithSizes(r, defaultWindowSizes, nil)
}

// ParseReaderMeta is ParseReader with metadata passed through to the parse
// context, as ParseMeta is Parse with it.
func (j *Tabnas) ParseReaderMeta(r io.Reader, meta map[string]any) (any, error) {
	return j.parseReaderWithSizes(r, defaultWindowSizes, meta)
}

// parseReaderWithSizes is ParseReader with the window's sizes given. Not an
// option: the sizes are fixed (admin ADR-30), and only the differential
// test makes them a few bytes (rs/src/lib.rs parse_reader_with_sizes).
func (j *Tabnas) parseReaderWithSizes(r io.Reader, sizes windowSizes, meta map[string]any) (result any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			result = nil
			err = j.internalError("ParseReader", rec)
		}
	}()

	if r == nil {
		je := j.parser.makeError("unreadable", "no reader", "", 0, 1, 1)
		je.tokenName = "#BD"
		je.why = "unreadable"
		return nil, j.attachHint(je)
	}

	if !j.parser.Config.Window || j.parserStart != nil || j.parser.Config.Recover.Enabled {
		text, failure := readWhole(r, sizes.chunk)
		if failure != nil {
			je := j.parser.makeError("unreadable", failure.reason, text, failure.pos, failure.row, failure.col)
			je.tokenName = "#BD"
			je.why = "unreadable"
			if fn, ok := meta["fileName"].(string); ok {
				je.fileName = fn
			}
			return nil, j.attachHint(je)
		}
		if meta == nil {
			return j.Parse(text)
		}
		return j.ParseMeta(text, meta)
	}

	w := newSourceWindow(r, sizes)

	// A source read from a reader is empty only once a read says so.
	if w.isEmpty() {
		if !j.emptyAllow {
			return nil, j.parser.makeError("unexpected", "", "", 0, 1, 1)
		}
		return j.emptyResult, nil
	}

	out, perr := j.parser.startParseFrom("", w, meta, j.lexSubs, j.ruleSubs, j)
	return out, j.attachHint(perr)
}
