// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

// srcappend.go — appending to a tree node's "src" in amortized constant
// time.
//
// The tree builders grow a node's src once per item: @capture$ appends
// each child's src, @fold$ each iteration's src and separator, @node$
// each matched terminal. A repetition is a same-depth replace loop that
// carries ONE node through all of its items, so that node takes an
// append per item. Go strings are immutable, and `n["src"] = ns + cs`
// allocated a new string and copied the whole accumulated src into it on
// every append: a loop of n items cost O(n²) time and allocated O(n²)
// bytes, where TypeScript (V8 concatenates into ropes) and Rust
// (`String::push_str`) are linear.
//
// appendSrc keeps, per parse, the growable byte buffer behind each src
// string it builds, keyed by the address of the buffer's first byte, and
// appends in place when the src being extended is the WHOLE of a buffer
// it owns. The strings it returns alias that buffer, which is sound for
// the reason strings.Builder is: bytes below a buffer's length are never
// written again, so every string already handed out keeps its content;
// an append writes only past the end, and grows by copying into a new
// buffer when the capacity runs out.
//
// Nothing observable changes. Every src is byte-identical to the plain
// concatenation, the node maps and kids slices are the same objects, and
// a src the table does not recognise — one a user action assigned, a
// prefix of a longer buffer, a token's text — is extended by copying, as
// before. Only the time and the allocations differ.

import "unsafe"

// srcTailMin is the length below which appendSrc concatenates plainly:
// copying fewer bytes than this costs about what the table lookup does,
// and short srcs, the common case, never enter the table. A node pays at
// most srcTailMin bytes per append before its src crosses the threshold,
// so the bound stays linear.
const srcTailMin = 256

// appendSrc returns s + x, where s is the current src of a tree node and
// x the text being appended to it. ctx may be nil (a builtin invoked
// directly, outside a parse), which concatenates plainly.
func appendSrc(ctx *Context, s, x string) string {
	switch {
	case x == "":
		return s
	case s == "":
		// What `"" + x` returns: x itself, uncopied.
		return x
	case ctx == nil || len(s)+len(x) < srcTailMin:
		return s + x
	}
	p := unsafe.StringData(s)
	if buf, ok := ctx.srcTails[p]; ok && len(buf) == len(s) {
		// s is the whole of a buffer this parse owns: nothing handed out
		// reaches past its end, so append in place.
		buf = append(buf, x...)
		if q := unsafe.SliceData(buf); q != p {
			// The append outgrew the capacity and moved. Strings already
			// handed out keep the old buffer alive and unchanged; it can
			// no longer be extended in place, so forget it.
			delete(ctx.srcTails, p)
			ctx.srcTails[q] = buf
		} else {
			ctx.srcTails[p] = buf
		}
		return unsafe.String(unsafe.SliceData(buf), len(buf))
	}
	// Anything else — a src this parse did not build, or a prefix of a
	// buffer that has since grown — starts a buffer of its own. It is
	// allocated at exactly the length a concatenation would be, so a node
	// appended to once wastes nothing; the next append grows it
	// geometrically.
	if ctx.srcTails == nil {
		ctx.srcTails = make(map[*byte][]byte)
	}
	buf := make([]byte, 0, len(s)+len(x))
	buf = append(append(buf, s...), x...)
	ctx.srcTails[unsafe.SliceData(buf)] = buf
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}
