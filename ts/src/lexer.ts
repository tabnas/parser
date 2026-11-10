/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  lexer.ts
 *  Lexer implementation, converts source text into tokens for the parser.
 */

import type {
  Tin,
  Rule,
  Config,
  Context,
  LexCheck,
  LexMatcher,
  MakeLexMatcher,
  NormAltSpec,
} from './types'

import { EMPTY, INSPECT } from './types'

import type { TabnasOptions } from './tabnas'

import {
  S,
  charset,
  charsBitmap,
  clean,
  deep,
  escre,
  keys,
  omap,
  regexp,
  snip,
  tokenize,
  entries,
  values,
} from './utility'

import { TabnasError } from './error'

// Scan position threaded through the parse: source index, row/column, and pending-token queue.
class Point {
  len = -1                 // Total source length.
  sI = 0                   // Source index (chars consumed so far).
  rI = 1                   // Row (1-based, for error messages).
  cI = 1                   // Column (1-based, for error messages).
  token: Token[] = []      // Pending-token queue (lookahead / rewind feed it).
  end?: Token              // Cached end-of-source (#ZZ) token, once reached.

  constructor(len: number, sI?: number, rI?: number, cI?: number) {
    this.len = len
    if (null != sI) {
      this.sI = sI
    }
    if (null != rI) {
      this.rI = rI
    }
    if (null != cI) {
      this.cI = cI
    }
  }

  toString() {
    return (
      'Point[' +
      [this.sI + '/' + this.len, this.rI, this.cI] +
      (0 < this.token.length ? ' ' + this.token : '') +
      ']'
    )
  }

  [INSPECT]() {
    return this.toString()
  }
}

const makePoint = (...params: ConstructorParameters<typeof Point>) =>
  new Point(...params)

const STRING_TYPE = 'string'

// Assigned in Token's static block, which alone can reach its private
// fields: give a token whose cut over a window stands the source's
// positions and its own text.
let settleToken: (tkn: Token, base: number) => void

// A copy of `s` that shares no storage with the string it was cut from.
// A substring of a long string is a view of it in V8 (a sliced string),
// so a token's text cut from a window would keep the whole window alive;
// concatenating flattens into a fresh string, and the slice of that holds
// only the copy.
function ownText(s: string): string {
  return 0 === s.length ? s : (' ' + s).slice(1)
}


// A single lexed token: numeric token id, JS-typed value, raw source text, and match position.
//
// NOTE: `src` is a prototype accessor over a lazily-materialized backing
// field — matchers that only know the token's span (space, line,
// comment, quoted-string raw text) defer the substring until someone
// actually reads it, so ignored tokens never allocate one. Reading
// tkn.src always yields the correct string; the one observable
// difference from a plain data property is that `src` is not an OWN
// enumerable property, so Object.keys(tkn), {...tkn}, and
// JSON.stringify(tkn) do not include it.
class Token {
  isToken = true             // Marker discriminating Tokens from other values.
  name = EMPTY               // Token name (e.g. '#NR', '#ST').
  tin: Tin = -1 as Tin       // Numeric token id corresponding to name.
  val: any = undefined       // JS-typed value (e.g. a number for #NR).
  sI = -1                    // Source index where the match started.
  rI = -1                    // Row where the match started.
  cI = -1                    // Column where the match started.
  len = -1                   // Length of src.
  use?: Record<string, any>  // Arbitrary user/plugin data attached to the token.
  err?: string               // Error code, if this token is bad.
  why?: string               // Diagnostic note explaining how the token arose.
  ignored?: Token            // Optional attached ignored token (e.g. space/line/comment).

  #src: string | null | undefined  // Materialized source text (undefined = not yet).
  #ref?: string                    // Full source backing the [sI, sI+len) span.

  constructor(
    name: string,
    tin: Tin,
    val: any,
    src: string | undefined,
    pnt: Point,
    use?: any,
    why?: string,
    ref?: string,
    len?: number,
  ) {
    this.name = name
    this.tin = tin
    this.#src = src
    this.#ref = ref
    this.val = val
    this.sI = pnt.sI
    this.rI = pnt.rI
    this.cI = pnt.cI
    this.use = use
    this.why = why

    this.len = null != len ? len : null == src ? 0 : src.length
  }

  get src(): string {
    let s = this.#src
    if (undefined === s) {
      const ref = this.#ref
      s = this.#src =
        undefined === ref ? EMPTY : ref.substring(this.sI, this.sI + this.len)
    }
    return s as string
  }

  set src(s: string) {
    this.#src = s
  }

  resolveVal(rule: Rule, ctx: Context): any {
    let out =
      'function' === typeof this.val ? (this.val as any)(rule, ctx) : this.val
    return out
  }

  bad(err: string, details?: any): Token {
    this.err = err
    if (null != details) {
      this.use = deep(this.use || {}, details)
    }
    return this
  }

  // A token cut from a window (admin ADR-30) is cut at positions counted
  // from the window's start, and may hold its text as a span of the
  // window. Once its cut stands it takes the source's positions and a copy
  // of its own text: the window moves on and drops what lies behind it,
  // and a token, or a string value it carries, must not keep the window's
  // text alive (`settleToken`, declared above the class).
  static {
    settleToken = (tkn: Token, base: number): void => {
      const src = tkn.src
      const own = ownText(src)
      tkn.#src = own
      tkn.#ref = undefined
      const val = tkn.val
      if (val === src) {
        tkn.val = own
      } else if (STRING_TYPE === typeof val) {
        tkn.val = ownText(val)
      }
      tkn.sI += base
    }
  }

  toString() {
    return (
      'Token[' +
      this.name +
      '=' +
      this.tin +
      ' ' +
      snip(this.src) +
      (undefined === this.val || '#ST' === this.name || '#TX' === this.name
        ? ''
        : '=' + snip(this.val)) +
      ' ' +
      [this.sI, this.rI, this.cI] +
      (null == this.use
        ? ''
        : ' ' + snip('' + JSON.stringify(this.use).replace(/"/g, ''), 22)) +
      (null == this.err ? '' : ' ' + this.err) +
      (null == this.why ? '' : ' ' + snip('' + this.why, 22)) +
      ']'
    )
  }

  [INSPECT]() {
    return this.toString()
  }
}

const makeToken = (...params: ConstructorParameters<typeof Token>) =>
  new Token(...params)

const makeNoToken = () => makeToken('', -1 as Tin, undefined, EMPTY, makePoint(-1))


// ---------------------------------------------------------------------------
// A source read from a reader, a window of it at a time (admin ADR-30,
// doc/chunked-source.md).
// ---------------------------------------------------------------------------

// A synchronous pull reader of bytes: `read(buf)` writes up to
// `buf.length` bytes into `buf` from its start and returns how many it
// wrote, 0 at the end of the input. Node's `fs.readSync(fd, buf)` has this
// shape; a read that fails throws.
type Reader = { read(buf: Uint8Array): number }

// A window's sizes: the margin ahead of the cursor and the dead text
// gathered before it is dropped, in UTF-16 units, and a read's length in
// bytes. Each is 64 KiB (admin ADR-30), and none is an option: only the
// differential test makes them a few units (`parseReaderWithSizes`), so
// that every matcher meets the window's edge.
type WindowSizes = { margin: number; block: number; chunk: number }

const WINDOW_SIZES: WindowSizes = Object.freeze({
  margin: 64 * 1024,
  block: 64 * 1024,
  chunk: 64 * 1024,
})

// The reason a source is not UTF-8, as Rust's `read_to_string` gives it
// (rs/src/lexer.rs NOT_UTF8), so that the three runtimes say the same.
const NOT_UTF8 = 'stream did not contain valid UTF-8'

// A source that could not be read to its end: why, and the position, row
// and column after the last character read, where the parse fails.
type Unreadable = { reason: string; pos: number; row: number; col: number }

// Why a read failed: the thrown error's message, or what was thrown.
function readFailure(err: any): string {
  return null != err && STRING_TYPE === typeof err.message
    ? err.message
    : String(err)
}

// Why a read that answered a count it cannot have failed.
function readCount(n: any, len: number): string {
  return 'the reader read ' + String(n) + ' bytes into ' + len
}

// The length of the longest prefix of b[0, n) that is well-formed UTF-8
// (Unicode Table 3-7, as Rust's `str::from_utf8` reads it), and in
// `scratch.bad` whether what follows it can never be: an ill-formed
// sequence, or, once the source has `ended`, a character it ends inside. A
// character split at n otherwise waits for its last bytes.
function utf8Prefix(
  b: Uint8Array,
  n: number,
  ended: boolean,
  scratch: { bad: boolean },
): number {
  scratch.bad = false
  let i = 0
  while (i < n) {
    const c = b[i]
    if (c < 0x80) {
      i++
      continue
    }
    let need = 0
    let lo = 0x80
    let hi = 0xbf
    if (0xc2 <= c && c <= 0xdf) {
      need = 1
    } else if (0xe0 === c) {
      need = 2
      lo = 0xa0
    } else if ((0xe1 <= c && c <= 0xec) || 0xee === c || 0xef === c) {
      need = 2
    } else if (0xed === c) {
      need = 2
      hi = 0x9f
    } else if (0xf0 === c) {
      need = 3
      lo = 0x90
    } else if (0xf1 <= c && c <= 0xf3) {
      need = 3
    } else if (0xf4 === c) {
      need = 3
      hi = 0x8f
    } else {
      scratch.bad = true
      return i
    }
    for (let k = 1; k <= need; k++) {
      if (n <= i + k) {
        scratch.bad = ended
        return i
      }
      const d = b[i + k]
      if (1 === k ? d < lo || hi < d : d < 0x80 || 0xbf < d) {
        scratch.bad = true
        return i
      }
    }
    i += need + 1
  }
  return n
}

// Where `text` ends, as a failure's position counts it: its UTF-16 units,
// and the row and column after its last one, a line feed starting a row.
function positionAfter(text: string): { pos: number; row: number; col: number } {
  let row = 1
  for (let i = text.indexOf('\n'); -1 !== i; i = text.indexOf('\n', i + 1)) {
    row++
  }
  return { pos: text.length, row, col: text.length - text.lastIndexOf('\n') }
}

// A reader read to its end, as a grammar that reads its source whole
// reads it (rs/src/parser.rs `read_whole`): the text, and why the source
// ended early if it did, a read that failed or bytes that are not UTF-8,
// at the character after the longest prefix that decodes. A failed read
// is the reason even where bytes before it do not decode, as in Rust.
function readWhole(
  reader: Reader,
  chunk: number,
): { text: string; failure?: Unreadable } {
  const buffer = new Uint8Array(Math.max(1, chunk | 0))
  const parts: Uint8Array[] = []
  let total = 0
  let failed: string | undefined = undefined
  for (;;) {
    let n: number
    try {
      n = reader.read(buffer)
    } catch (err: any) {
      failed = readFailure(err)
      break
    }
    if (!(Number.isInteger(n) && 0 <= n && n <= buffer.length)) {
      failed = readCount(n, buffer.length)
      break
    }
    if (0 === n) break
    parts.push(buffer.slice(0, n))
    total += n
  }
  const bytes = new Uint8Array(total)
  let at = 0
  for (const part of parts) {
    bytes.set(part, at)
    at += part.length
  }
  const scratch = { bad: false }
  const valid = utf8Prefix(bytes, total, true, scratch)
  const text = new TextDecoder('utf-8', { ignoreBOM: true }).decode(
    bytes.subarray(0, valid),
  )
  const reason =
    undefined !== failed ? failed : scratch.bad ? NOT_UTF8 : undefined
  if (undefined === reason) {
    return { text }
  }
  return { text, failure: { reason, ...positionAfter(text) } }
}

// The oldest source position among `tokens` older than `keep`, or `keep`:
// how far back the window must still reach for them. Sentinel and unset
// slots are skipped, and the scan stops once the answer falls below
// `floor`, when nothing can be dropped anyway.
function oldestHeld(
  tokens: (Token | undefined)[] | undefined,
  keep: number,
  floor: number,
  notoken: Token,
): number {
  if (null == tokens) return keep
  for (let i = 0; i < tokens.length && floor <= keep; i++) {
    const t = tokens[i]
    if (null != t && notoken !== t && 0 <= t.tin && 0 <= t.sI && t.sI < keep) {
      keep = t.sI
    }
  }
  return keep
}

// The window over a source read from a reader: the text from `low`, the
// oldest position the parser may still ask for, to what has been read.
// Positions are the source's, in UTF-16 units from its start; the text
// is `text`, indexed from `low`.
class SourceWindow {
  reader: Reader
  text = EMPTY              // The text read and kept, from `low` on.
  low = 0                   // The source position of text[0].
  // The row and column of text[0], counted as an error's excerpt counts
  // them: a line feed starts a row, and every UTF-16 unit is a column.
  lowRow = 1
  lowCol = 1
  // The row and column after the last character read, counted the same
  // way, which place a failure.
  readRow = 1
  readCol = 1
  exhausted = false         // The reader has said there is no more.
  failure?: Unreadable      // Why the source ended early, and where.
  margin: number            // How far past the cursor the window reaches.
  block: number             // Dead text gathered before it is dropped.
  buffer: Uint8Array        // What a read fills, kept from read to read.
  pending?: Uint8Array      // Bytes read and not yet decoded.
  #decoder: TextDecoder
  #scratch = { bad: false }

  constructor(reader: Reader, sizes: WindowSizes) {
    this.reader = reader
    this.margin = Math.max(1, sizes.margin | 0)
    this.block = Math.max(1, sizes.block | 0)
    this.buffer = new Uint8Array(Math.max(1, sizes.chunk | 0))
    // ignoreBOM: a byte order mark is the source's own character U+FEFF,
    // as Rust's UTF-8 decoding and a parse of the text keep it.
    this.#decoder = new TextDecoder('utf-8', { ignoreBOM: true })
  }

  // The source position after the last character read.
  get end(): number {
    return this.low + this.text.length
  }

  // One read. Its bytes are decoded as far as they are whole characters,
  // and a character the read split waits for the next. A read that throws
  // or answers a count it cannot have, and bytes that are not UTF-8, end
  // the source where they stand, and the parse fails there.
  read(): void {
    const buffer = this.buffer
    let n: number
    try {
      n = this.reader.read(buffer)
    } catch (err: any) {
      this.fail(readFailure(err))
      return
    }
    if (!(Number.isInteger(n) && 0 <= n && n <= buffer.length)) {
      this.fail(readCount(n, buffer.length))
      return
    }
    if (0 === n) {
      this.exhausted = true
    }
    let bytes = buffer
    let len = n
    const pending = this.pending
    if (undefined !== pending) {
      bytes = new Uint8Array(pending.length + n)
      bytes.set(pending)
      bytes.set(buffer.subarray(0, n), pending.length)
      len = bytes.length
      this.pending = undefined
    }
    const scratch = this.#scratch
    const valid = utf8Prefix(bytes, len, this.exhausted, scratch)
    if (0 < valid) {
      this.append(this.#decoder.decode(bytes.subarray(0, valid)))
    }
    if (scratch.bad) {
      this.fail(NOT_UTF8)
    } else if (valid < len) {
      this.pending = bytes.slice(valid, len)
    }
  }

  // Add decoded text to the window, counting the row and column after it.
  append(s: string): void {
    const last = s.lastIndexOf('\n')
    if (-1 === last) {
      this.readCol += s.length
    } else {
      let rows = 0
      for (let i = s.indexOf('\n'); -1 !== i; i = s.indexOf('\n', i + 1)) {
        rows++
      }
      this.readRow += rows
      this.readCol = s.length - last
    }
    this.text += s
  }

  // The source ends here: the parse fails with `reason` at the character
  // after the last one read, at the next token.
  fail(reason: string): void {
    this.exhausted = true
    if (undefined === this.failure) {
      this.failure = {
        reason,
        pos: this.end,
        row: this.readRow,
        col: this.readCol,
      }
    }
  }

  // Drop the text before the source position `keep`, keeping the row and
  // column the window then begins at.
  drop(keep: number): void {
    const n = keep - this.low
    if (n <= 0) return
    const text = this.text
    const last = text.lastIndexOf('\n', n - 1)
    if (-1 === last) {
      this.lowCol += n
    } else {
      let rows = 0
      for (let i = text.indexOf('\n'); -1 !== i && i < n; i = text.indexOf('\n', i + 1)) {
        rows++
      }
      this.lowRow += rows
      this.lowCol = n - last
    }
    this.text = text.substring(n)
    this.low = keep
  }
}


// Wrap a matcher body in the standard entry guards: skip when
// `mcfg.lex` is false, and consult an optional `check` hook that
// may short-circuit by returning `{ done: true, token }`.
//
// `mcfg` is captured once at matcher-build time. The matcher
// factories are re-invoked on every `tn.make()` clone (via
// `configure()`), so a stale closure can never outlive the cfg
// snapshot it was built from.
function guardedMatcher(
  mcfg: { lex: boolean; check?: LexCheck | undefined },
  body: LexMatcher,
): LexMatcher {
  return function guarded(lex, rule, tI) {
    if (!mcfg.lex) return undefined
    if (mcfg.check) {
      // Check hooks are user code and may read lex.fwd directly.
      lex.refwd()
      const r = mcfg.check(lex)
      if (r && r.done) return r.token
    }
    return body(lex, rule, tI)
  }
}


// ---------------------------------------------------------------------------
// Declarative single-character state machine driver.
//
// The simpler matchers (space, line, comment-eatline tails) all
// have the shape "walk bytes, dispatch on (state, char-class), emit
// position-tracking actions, stop when told". The driver below
// centralises that shape.
//
// Each spec declares:
//   - `initialState`           which state the walk starts in
//   - `nclasses`               how many byte-classes the spec uses
//   - `classOf` (Uint8Array)   per-byte class index (ASCII fast path)
//   - `fallback`               class for non-ASCII bytes
//   - `table`   (Int32Array)   action keyed on `state * nclasses + class`
//
// An action is a packed Int32 — `STATE_MASK` bits hold the next
// state, plus three single-bit flags below. The driver applies
// CONSUME / IS_ROW first, then transitions, then STOP. That ordering
// makes "consume the char that ends the match" express as
// `CONSUME | STOP`, while "stop without consuming" is just `STOP`.
//
// Performance-wise the loop is uniform: one Uint8Array index, one
// Int32Array index, three bit tests, no function calls per byte.
// ---------------------------------------------------------------------------

const CONSUME = 1 << 16
const IS_ROW = 1 << 17
const CI_RESET = 1 << 18 // cI = 1 without rI++ (line chars in multi-line strings)
const STOP = 1 << 19
const STATE_MASK = 0xffff

// Immutable spec driving the single-character scan state machine (see driver above).
type ScanSpec = {
  readonly initialState: number          // State the walk starts in.
  readonly nclasses: number              // Number of byte-classes the spec uses.
  readonly classOf: Uint8Array           // Per-byte class index (ASCII fast path).
  readonly fallback: (c: string) => number  // Class for non-ASCII bytes.
  readonly table: Int32Array             // Packed action keyed on state * nclasses + class.
}

// Caller-owned scratch holding the position the scan ended at (no per-call allocation).
type ScanOut = { sI: number; rI: number; cI: number }

// Walk `src` from `(startSI, startRI, startCI)` according to `spec`.
// Position fields are written into `out` (a caller-owned scratch
// object — no allocation per call). Returns true if any char was
// consumed.
//
// Takes raw position numbers rather than a Point because some
// callers (notably the comment matcher) track positions as locals
// against a sliced `fwd` string rather than on the lex's pnt.
function scan(
  src: string,
  startSI: number,
  startRI: number,
  startCI: number,
  spec: ScanSpec,
  out: ScanOut,
): boolean {
  let sI = startSI
  let rI = startRI
  let cI = startCI
  const len = src.length
  const ncls = spec.nclasses
  const classOf = spec.classOf
  const table = spec.table
  let state = spec.initialState

  while (sI < len) {
    const cc = src.charCodeAt(sI)
    const cls = cc < 256 ? classOf[cc] : spec.fallback(src[sI])
    const action = table[state * ncls + cls]

    if (action & CONSUME) {
      sI++
      if (action & IS_ROW) {
        rI++
        cI = 1
      } else if (action & CI_RESET) {
        cI = 1
      } else {
        cI++
      }
    }
    state = action & STATE_MASK
    if (action & STOP) break
  }

  out.sI = sI
  out.rI = rI
  out.cI = cI
  return startSI < sI
}


// Build a 3-class line-run spec from cfg.line. Class 0 = not a line
// char, class 1 = line char, class 2 = line char that also advances
// the row counter. Used by the line matcher (when not in `single`
// mode) and by the comment matcher's `eatline` tails.
function buildLineRunSpec(cfgLine: Config['line']): ScanSpec {
  const classOf = new Uint8Array(256)
  for (let cc = 0; cc < 256; cc++) {
    if (cfgLine.charsBitmap[cc]) {
      classOf[cc] = cfgLine.rowCharsBitmap[cc] ? 2 : 1
    }
  }
  const lineChars = cfgLine.chars
  const rowChars = cfgLine.rowChars
  const fallback = (c: string): number => {
    if (lineChars[c]) return rowChars[c] ? 2 : 1
    return 0
  }
  return {
    initialState: 0,
    nclasses: 3,
    classOf,
    fallback,
    table: LINE_RUN_TABLE,
  }
}

// (state=0, class=NOT_LINE) -> stop
// (state=0, class=LINE)     -> consume, stay in 0
// (state=0, class=LINE+ROW) -> consume + row, stay in 0
const LINE_RUN_TABLE = new Int32Array([
  STOP,
  CONSUME,
  CONSUME | IS_ROW,
])


// Build a 2-class run spec from a chars / charsBitmap pair. Class 0
// = not in set, class 1 = in set. Used by the space matcher.
function buildCharRunSpec(
  charsBitmap: Uint8Array,
  chars: Record<string, any>,
): ScanSpec {
  const fallback = (c: string): number => (chars[c] ? 1 : 0)
  return {
    initialState: 0,
    nclasses: 2,
    classOf: charsBitmap, // already 0/1
    fallback,
    table: CHAR_RUN_TABLE,
  }
}

// (state=0, class=OUT) -> stop
// (state=0, class=IN)  -> consume col, stay in 0
const CHAR_RUN_TABLE = new Int32Array([
  STOP,
  CONSUME,
])


// Build a string-body scan spec for one quote character. Class 0 =
// BODY (consume, advance col); class 1 = STOP (caller decides what
// to do); class 2 = LINE (multi-line strings only — consume, reset
// col); class 3 = LINE+ROW (multi-line — consume, reset col,
// advance row). The opening / closing quote, the escape char, the
// replace chars and any control char that can't be consumed in
// the current quote context all map to class 1.
//
// One spec per quote char because the quote char is encoded in the
// class table. For a typical config (1-3 quote chars) this is
// cheap; the matcher caches them per make.
function buildStringBodySpec(cfg: Config, qchar: string): ScanSpec {
  const qcc = qchar.charCodeAt(0)
  const escCharCode = cfg.string.escCharCode
  const replaceCodeMap = cfg.string.replaceCodeMap
  const hasReplace = cfg.string.hasReplace
  const isMultiLine = !!cfg.string.multiBitmap[qcc]
  const allowControl = cfg.string.allowControl
  const lineBM = cfg.line.charsBitmap
  const rowBM = cfg.line.rowCharsBitmap

  const classOf = new Uint8Array(256)
  for (let cc = 0; cc < 256; cc++) {
    if (cc === qcc) {
      classOf[cc] = 1
    } else if (cc === escCharCode) {
      classOf[cc] = 1
    } else if (hasReplace && replaceCodeMap[cc] !== undefined) {
      classOf[cc] = 1
    } else if (isMultiLine && lineBM[cc]) {
      // A line char inside a multi-line string, at any code point in
      // the table, as the fallback below already classified one past
      // it. This test sat under `cc < 32`, so a Latin-1 line char
      // (U+0085, configured as one) was plain body inside a multi-line
      // string while U+2028 counted a row (tabnas/parser#263).
      classOf[cc] = rowBM[cc] ? 3 : 2
    } else if (cc < 32) {
      if (allowControl && !lineBM[cc]) {
        // Raw control char admitted verbatim: plain BODY. Line chars
        // are deliberately excluded — inside a single-line string they
        // must still stop and error.
        classOf[cc] = 0
      } else {
        classOf[cc] = 1
      }
    }
    // else BODY (class 0)
  }

  // Char codes >= 256 classify like the table: the quote itself, the
  // escape char, replace chars, and (multi-line) line chars are special;
  // everything else is plain body. Without this, non-Latin-1 quote chars
  // could open a string but never close it.
  const lineChars = cfg.line.chars
  const rowChars = cfg.line.rowChars
  const fallback = (c: string): number => {
    const cc = c.charCodeAt(0)
    if (c === qchar) return 1
    if (cc === escCharCode) return 1
    if (hasReplace && replaceCodeMap[cc] !== undefined) return 1
    if (isMultiLine && lineChars[c]) return rowChars[c] ? 3 : 2
    return 0
  }

  return {
    initialState: 0,
    nclasses: 4,
    classOf,
    fallback,
    table: STRING_BODY_TABLE,
  }
}

// (s=0, BODY)         -> consume + col
// (s=0, STOP)         -> stop, caller dispatches on src[sI]
// (s=0, LINE_NONROW)  -> consume + cI=1 (multi-line)
// (s=0, LINE_ROW)     -> consume + rI++; cI=1 (multi-line)
const STRING_BODY_TABLE = new Int32Array([
  CONSUME,
  STOP,
  CONSUME | CI_RESET,
  CONSUME | IS_ROW,
])



// One fixed-token candidate: the token source, its length, and its tin.
type FixedCand = { src: string; len: number; tin: Tin }

let makeFixedMatcher: MakeLexMatcher = (cfg: Config, _opts: TabnasOptions) => {
  // First-char dispatch table replacing the old anchored alternation
  // regex: match cost per position becomes one charCode index plus (for
  // multi-char tokens) a startsWith verify, with no match-array or
  // capture substring allocation. Candidates sharing a first char are
  // ordered longest-first, preserving the regex's longest-match-wins
  // ordering (utility.ts sorts the alternation the same way).
  const table: (FixedCand[] | undefined)[] = new Array(256)
  const wide: FixedCand[] = [] // Tokens whose first char is >= U+0100.
  const byLenDesc = (a: FixedCand, b: FixedCand) =>
    b.len - a.len || (a.src < b.src ? -1 : a.src > b.src ? 1 : 0)
  for (const fsrc of keys(cfg.fixed.token)) {
    const tin = cfg.fixed.token[fsrc]
    if (null == tin || 0 === fsrc.length) continue
    const cand: FixedCand = { src: fsrc, len: fsrc.length, tin }
    const cc = fsrc.charCodeAt(0)
    if (cc < 256) {
      ;(table[cc] = table[cc] || []).push(cand)
    } else {
      wide.push(cand)
    }
  }
  for (const cands of table) {
    if (cands) cands.sort(byLenDesc)
  }
  wide.sort(byLenDesc)

  return guardedMatcher(cfg.fixed, function fixedBody(lex) {
    const pnt = lex.pnt
    const src = lex.src
    const cc = src.charCodeAt(pnt.sI)
    const cands = cc < 256 ? table[cc] : wide

    if (undefined === cands) return undefined

    // Negotiated lexing: only candidates the requesting alternate wants
    // may match. Longest-match-wins still holds among those, so a
    // shorter wanted token can win over a longer unwanted one — which
    // is the point: the alternate knows which cut it needs.
    const want = lex.want

    for (const cand of cands) {
      if (null != want && !want.includes(cand.tin)) continue
      // A candidate longer than what is left of a window may yet match
      // once it has grown.
      if (src.length < pnt.sI + cand.len) lex.touchEnd()
      // Single-char Latin-1 candidates already matched via the table
      // index; longer (or wide-char) candidates verify in place.
      if ((1 === cand.len && cc < 256) || src.startsWith(cand.src, pnt.sI)) {
        const tkn = lex.token(cand.tin, undefined, cand.src, pnt)
        pnt.sI += cand.len
        pnt.cI += cand.len
        return tkn
      }
    }
  })
}

let makeMatchMatcher: MakeLexMatcher = (cfg: Config, _opts: TabnasOptions) => {
  // Pre-sort both matcher lists at configure time so lexing iterates in
  // a deterministic order regardless of how the config object was built.
  // Value matchers: sort by user-supplied name (ascending).
  // Token matchers: sort by attached tin$ (ascending), set in utility.ts.
  let valueMatchers = entries(cfg.match.value)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([, spec]) => spec)
  let tokenMatchers = values(cfg.match.token).sort(
    (a: any, b: any) => (a.tin$ || 0) - (b.tin$ || 0),
  )

  // Don't add a matcher if there's nothing to do.
  if (0 === valueMatchers.length && 0 === tokenMatchers.length) {
    return null
  }

  return guardedMatcher(cfg.match, function matchBody(lex, rule, tI = 0) {
    let pnt = lex.pnt
    // Value/token matcher regexes are documented to run against the
    // remainder string, so materialize it (memoized per position).
    let fwd = lex.refwd()

    // A standalone lexer (`makeLex` + `lex.next()` with no rule, which
    // the exported lexer API and its tests use) has no rule and so no
    // token column to gate on. Nothing constrains what the caller can
    // use there, so every matcher is eligible and the two passes below
    // collapse into the first. Reading `rule.spec` unconditionally threw
    // instead, and `Lex.next` turned that into a `#BD` token — which is
    // what a standalone lexer with any match token got, before this
    // change as well as after it.
    const rspec = null == rule ? undefined : (rule as Rule).spec
    const gated = null != rspec
    let oc = gated && 'o' === (rule as Rule).state ? 0 : 1

    // Under a negotiated-lexing constraint, value matchers are skipped
    // outright: they produce value tokens (#VL) by content, not by the
    // requesting alternate's tin list, so they cannot serve a want.
    const want = lex.want

    if (null == want)
    for (let valueMatcher of valueMatchers) {
      if (valueMatcher.match instanceof RegExp) {
        // TODO: only match VL if present in rule

        let m = fwd.match(valueMatcher.match)

        if (m) {
          let msrc = m[0]
          let mlen = msrc.length
          if (0 < mlen) {
            let tkn: Token | undefined = undefined

            let val = valueMatcher.val ? valueMatcher.val(m) : msrc
            tkn = lex.token('#VL', val, msrc, pnt)

            pnt.sI += mlen
            pnt.cI += mlen

            return tkn
          }
        }
      } else {
        let tkn: any = valueMatcher.match(lex, rule)
        if (null != tkn) {
          return tkn
        }
      }
    }

    // Two passes over the token matchers, in tin order within each, as
    // go/lexer.go matchMatch has always made: first the matchers whose
    // tin the rule expects at this slot, then the `eager$` ones it does
    // not. One pass, with eagerness merely bypassing the gate, let an
    // eager matcher EARLIER in tin order win over an expected one later:
    // `s = p *d [t]` with `p = %x31-39`, `d = %x30-39` read the `2` of
    // `12` as `p`, and the `*d` loop, which wanted `d`, failed on a token
    // it never expected. Preferring what the parser expects at the slot
    // is what eagerness was for: a token that must still fire where the
    // rule's collated column is narrower than the grammar (a case-
    // insensitive literal; a character class at a lookahead slot the
    // column does not cover), never one that steals an expected cut.
    //
    // A matcher with no `tin$` (a function matcher registered without a
    // token) has no gate to fail and runs in the first pass, as before.
    //
    // Under a negotiated-lexing `want` the alternate's own tin list
    // replaces the column and is the sharper gate; one filtered pass is
    // the whole search, as in Go.
    const col =
      null == want && gated ? (rspec as any).def.tcol[oc][tI] : undefined

    // The longest FIXED literal the slot expects that matches here, or
    // 0. Computed once per lex, and only when the eager pass is reached:
    // pass 0 is the match tokens the slot expects, and between a token
    // the slot expects and a literal it expects, the existing matcher
    // order decides. -1 means "not computed yet".
    //
    // In the eager pass, a literal the slot names beats an eager-only
    // matcher that cuts no further than it does. Without this, a
    // character class that CONTAINS a literal the grammar also uses
    // swallows it wherever the class is eager: `num = "0" / posdigit
    // *digit` beside `digit = %x30-39` lexed every `0` as the class, the
    // fixed `#0` was never produced, and no alternate of `num` could
    // match — Go rejected `0.0.0` for a grammar that plainly accepts it,
    // and TS did too once the bnf emitter marked classes eager.
    // Eagerness is for firing where the column is narrower than the
    // grammar, never for outbidding what the column names.
    //
    // LENGTH decides, not mere existence: an eager matcher that cuts
    // FURTHER than the literal still wins, so a keyword literal cannot
    // truncate a longer word (`#IF` = "if" beside an eager `#ID` =
    // /^[a-z]+/ leaves `iffy` to `#ID`, and takes `if` itself). Ties go
    // to the literal, which is the case this exists for.
    //
    // Read from `cfg.fixed.token` at lex time, never snapshotted when
    // the matcher was built: a grammar adds its own literals after the
    // matchers exist, and a snapshot would hold only the defaults —
    // which is exactly the case this has to see (`#0` is the grammar's).
    // Go reads `Config.FixedTokens` the same way, in expectedFixedLen.
    let fixLen = -1
    const expectedFixedLen = (): number => {
      if (-1 !== fixLen) return fixLen
      fixLen = 0
      if (!cfg.fixed.lex || null == col || 0 === col.length) return fixLen
      const src = lex.src
      const sI = pnt.sI
      const ftoken = cfg.fixed.token
      for (const fsrc of keys(ftoken)) {
        const ftin = ftoken[fsrc]
        if (null != ftin && fsrc.length > fixLen && col.includes(ftin)) {
          // A literal longer than what is left of a window may yet be
          // there once it has grown.
          if (src.length < sI + fsrc.length) lex.touchEnd()
          if (src.startsWith(fsrc, sI)) {
            fixLen = fsrc.length
          }
        }
      }
      return fixLen
    }

    for (let pass = 0; pass < 2; pass++) {
      for (let tokenMatcher of tokenMatchers) {
        const tin = (tokenMatcher as any).tin$

        if (null != want) {
          if (!tin || !want.includes(tin)) {
            continue
          }
        } else {
          // Ungated (no rule): everything is expected, so pass 0 runs
          // every matcher in tin order and pass 1 finds nothing left.
          const expected =
            !tin || !gated || (null != col && col.includes(tin))
          if (0 === pass ? !expected : expected || !(tokenMatcher as any).eager$) {
            continue
          }
        }

        if (tokenMatcher instanceof RegExp) {
          let m = fwd.match(tokenMatcher)

          if (m) {
            let msrc = m[0]
            let mlen = msrc.length
            // The eager pass yields to an expected literal it cannot
            // out-cut; the fixed matcher (order 2e6) runs next and takes
            // it. See `expectedFixedLen` above.
            if (1 === pass && mlen <= expectedFixedLen()) {
              continue
            }
            if (0 < mlen) {
              let tkn: Token | undefined = undefined

              tkn = lex.token(tin, msrc, msrc, pnt)

              pnt.sI += mlen
              pnt.cI += mlen

              return tkn
            }
          }
        } else {
          let tkn: any = tokenMatcher(lex, rule)
          if (null != tkn) {
            return tkn
          }
        }
      }

      if (null != want) break
    }
  })
}

// NOTE 1: matchers return arbitrary tokens and describe lexing using
// code, rather than a grammar. Thus, for example, some matchers below
// will check (using subMatchFixed) if their source text actually represents
// a fixed value.

// NOTE 2: a matcher can queue additional tokens onto the Point's pending
// queue (pnt.token) as well as returning one. Queued tokens are served, in
// order, before the lexer scans any further source. subMatchFixed below is
// the usual case: it returns the leading token and queues a trailing fixed
// one. The queue is a FIFO of no fixed size — parser lookahead depth is set
// by the grammar (see alt.sN), not by this.

// Resolved definition of one comment marker, extracted from the Config comment def map.
type CommentDef = Config['comment']['def'] extends { [_: string]: infer T }
  ? T
  : never

let makeCommentMatcher: MakeLexMatcher = (cfg: Config, opts: TabnasOptions) => {
  let oc = opts.comment

  cfg.comment = {
    lex: oc ? !!oc.lex : false,
    def: (oc?.def ? entries(oc.def) : []).reduce(
      (def: any, [name, om]: [string, any]) => {
        // Set comment marker to null to remove
        if (null == om || false === om) {
          return def
        }

        let { suffixes, suffixFn } = normalizeCommentSuffix(om.suffix)

        let cm: CommentDef = {
          name,
          start: om.start as string,
          end: om.end,
          line: !!om.line,
          lex: !!om.lex,
          eatline: !!om.eatline,
          suffixes,
          suffixFn,
        }

        def[name] = cm
        return def
      },
      {} as any,
    ),

    // The check hook is honoured by guardedMatcher below and by
    // buildLexDispatch (a comment matcher with a check must be a
    // candidate for every first char, not just the marker starts).
    check: oc?.check,
  }

  // Pre-sort by start length (longest first) so that a longer marker
  // shadows any shorter marker it contains (e.g. '##' wins over '#'),
  // regardless of the insertion order of cfg.comment.def. Ties break by
  // name for deterministic iteration across runtimes.
  let byStartLenDesc = (a: CommentDef, b: CommentDef) =>
    b.start.length - a.start.length ||
    (a.name < b.name ? -1 : a.name > b.name ? 1 : 0)
  let lineComments = cfg.comment.lex
    ? values(cfg.comment.def).filter((c) => c.lex && c.line).sort(byStartLenDesc)
    : []
  let blockComments = cfg.comment.lex
    ? values(cfg.comment.def).filter((c) => c.lex && !c.line).sort(byStartLenDesc)
    : []

  // Eatline tail: a 3-class line-run state machine, same shape as
  // the line matcher's no-single path. Built once per matcher build
  // so the table and class array are reused across comment matches.
  const lineRunSpec = buildLineRunSpec(cfg.line)
  const scanOut: ScanOut = { sI: 0, rI: 0, cI: 0 }

  // The body walks lex.src with absolute indices (aI) — no remainder
  // slice is materialized per position.
  return guardedMatcher(cfg.comment, function commentBody(lex) {
    let pnt = lex.pnt
    let src = lex.src

    let rI = pnt.rI
    let cI = pnt.cI

    // Single line comment.

    const lineBM = cfg.line.charsBitmap
    const rowBM = cfg.line.rowCharsBitmap
    const lineChars = cfg.line.chars
    const rowChars = cfg.line.rowChars

    for (let mc of lineComments) {
      // A marker longer than what is left of a window may yet start a
      // comment once it has grown.
      if (src.length < pnt.sI + mc.start.length) lex.touchEnd()
      if (src.startsWith(mc.start, pnt.sI)) {
        let srclen = src.length
        let aI = pnt.sI + mc.start.length
        cI += mc.start.length
        let suffixLen = 0
        let cc
        while (
          aI < srclen &&
          !((cc = src.charCodeAt(aI)) < 256
            ? lineBM[cc]
            : lineChars[src[aI]])
        ) {
          let n = commentSuffixMatch(src, aI, mc.suffixes, lex)
          if (n > 0) { suffixLen = n; break }
          n = commentSuffixFnMatch(lex, aI, mc.suffixFn)
          if (n > 0) { suffixLen = n; break }
          cI++
          aI++
        }
        // The body ran to a window's end, which need not be its own.
        if (srclen <= aI) lex.touchEnd()

        if (suffixLen > 0) {
          // Consume the suffix as the tail of the comment body.
          aI += suffixLen
          cI += suffixLen
        }
        else if (mc.eatline) {
          // Only absorb trailing line chars when termination came from
          // a line char (not from a suffix match). cI is intentionally
          // NOT pulled from scanOut — current semantics leave the
          // pnt.cI at end-of-comment-body even after eating newlines.
          scan(src, aI, rI, cI, lineRunSpec, scanOut)
          rI = scanOut.rI
          aI = scanOut.sI
          if (srclen <= aI) lex.touchEnd()
        }

        let tkn = lex.token(
          '#CM', undefined, undefined, pnt,
          undefined, undefined, aI - pnt.sI)

        pnt.sI = aI
        pnt.cI = cI
        pnt.rI = rI

        return tkn
      }
    }

    // Multiline comment.

    for (let mc of blockComments) {
      if (src.length < pnt.sI + mc.start.length) lex.touchEnd()
      if (src.startsWith(mc.start, pnt.sI)) {
        let srclen = src.length
        let aI = pnt.sI + mc.start.length
        let end = mc.end as string
        cI += mc.start.length
        let suffixLen = 0
        let cc
        while (aI < srclen && !src.startsWith(end, aI)) {
          let n = commentSuffixMatch(src, aI, mc.suffixes, lex)
          if (n > 0) { suffixLen = n; break }
          n = commentSuffixFnMatch(lex, aI, mc.suffixFn)
          if (n > 0) { suffixLen = n; break }
          cc = src.charCodeAt(aI)
          if (cc < 256 ? rowBM[cc] : rowChars[src[aI]]) {
            rI++
            cI = 0
          }

          cI++
          aI++
        }
        // The body ran to a window's end, or its end marker may straddle
        // it: the comment may yet close once the window has grown.
        if (srclen < aI + end.length && 0 === suffixLen) lex.touchEnd()

        if (suffixLen > 0) {
          // Advance through the consumed suffix, tracking newlines.
          for (let k = 0; k < suffixLen; k++) {
            cc = src.charCodeAt(aI + k)
            if (cc < 256 ? rowBM[cc] : rowChars[src[aI + k]]) {
              rI++
              cI = 0
            }
            cI++
          }
          let tkn = lex.token(
            '#CM', undefined, undefined, pnt,
            undefined, undefined, aI + suffixLen - pnt.sI)
          pnt.sI = aI + suffixLen
          pnt.rI = rI
          pnt.cI = cI
          return tkn
        }

        if (src.startsWith(end, aI)) {
          cI += end.length

          if (mc.eatline) {
            scan(src, aI, rI, cI, lineRunSpec, scanOut)
            rI = scanOut.rI
            aI = scanOut.sI
            if (srclen <= aI) lex.touchEnd()
          }

          let tkn = lex.token(
            '#CM', undefined, undefined, pnt,
            undefined, undefined, aI + end.length - pnt.sI)

          pnt.sI = aI + end.length
          pnt.rI = rI
          pnt.cI = cI

          return tkn
        } else {
          return lex.bad(
            S.unterminated_comment,
            pnt.sI,
            pnt.sI + 9 * mc.start.length,
          )
        }
      }
    }
  })
}

// normalizeCommentSuffix splits the polymorphic suffix option value
// (string | string[] | LexMatcher) into a length-sorted string list and
// an optional LexMatcher probe. Empty/absent input yields empty outputs.
function normalizeCommentSuffix(raw: any):
  { suffixes: string[] | undefined, suffixFn: LexMatcher | undefined } {
  if (null == raw) {
    return { suffixes: undefined, suffixFn: undefined }
  }
  if ('function' === typeof raw) {
    return { suffixes: undefined, suffixFn: raw as LexMatcher }
  }
  let list: string[] = []
  if ('string' === typeof raw) {
    if ('' !== raw) list.push(raw)
  } else if (Array.isArray(raw)) {
    for (let s of raw) {
      if ('string' === typeof s && '' !== s) list.push(s)
    }
  }
  if (list.length > 1) {
    list.sort((a, b) => b.length - a.length || (a < b ? -1 : a > b ? 1 : 0))
  }
  return {
    suffixes: 0 === list.length ? undefined : list,
    suffixFn: undefined,
  }
}

// commentSuffixMatch returns the length of the best suffix match at
// src[aI:] or 0 if none matches. Suffixes are pre-sorted longest-first,
// so the first match is the best match.
function commentSuffixMatch(
  src: string,
  aI: number,
  suffixes?: string[],
  lex?: Lex,
): number {
  if (!suffixes || 0 === suffixes.length) return 0
  for (let s of suffixes) {
    // A suffix longer than what is left of a window may yet match once
    // it has grown, and outrank a shorter one that matches now.
    if (undefined !== lex && src.length < aI + s.length) lex.touchEnd()
    if (src.startsWith(s, aI)) return s.length
  }
  return 0
}

// commentSuffixFnMatch probes the LexMatcher-form suffix terminator at
// absolute source offset aI. Returns the length of the returned token's
// src (to be consumed) or 0 if no termination. The lex point is
// snapshotted and restored so a misbehaving matcher can't advance the
// stream itself.
function commentSuffixFnMatch(lex: Lex, aI: number, fn?: LexMatcher): number {
  if (!fn) return 0
  let pnt = lex.pnt
  let savedSI = pnt.sI
  let savedRI = pnt.rI
  let savedCI = pnt.cI
  pnt.sI = aI
  let tkn: any
  try {
    tkn = fn(lex, undefined as any)
  } finally {
    pnt.sI = savedSI
    pnt.rI = savedRI
    pnt.cI = savedCI
  }
  if (null == tkn) return 0
  return ('string' === typeof tkn.src) ? tkn.src.length : 0
}

// How far past the point it is tried at an ender of the text and number
// patterns can read, in UTF-16 units: the longest of the fixed tokens,
// the grammar's own enders, the comment markers, and a space or line
// character (`cfg.rePart.ender`, built by configure in utility.ts). A cut
// over a window whose pattern could read past the window's end may differ
// from the whole text's, and is made again over a wider window.
function enderReach(cfg: Config, opts: TabnasOptions): number {
  let reach = 2
  for (const fsrc of keys(cfg.fixed.token)) {
    if (reach < fsrc.length) reach = fsrc.length
  }
  const ender: any = (opts as any).ender
  if (Array.isArray(ender)) {
    for (const e of ender) {
      if ('string' === typeof e && reach < e.length) reach = e.length
    }
  }
  const defs: any = opts.comment?.lex ? opts.comment.def : undefined
  if (null != defs) {
    for (const c of values(defs) as any[]) {
      if (c && c.lex && 'string' === typeof c.start && reach < c.start.length) {
        reach = c.start.length
      }
    }
  }
  return reach
}

// Match text, checking for literal values, optionally followed by a fixed token.
// Text strings are terminated by end markers.
let makeTextMatcher: MakeLexMatcher = (cfg: Config, opts: TabnasOptions) => {
  // Sticky ('y') so the regex runs against the full source at an offset
  // (ender.lastIndex = pnt.sI) instead of an allocated remainder slice.
  let ender = regexp(cfg.line.lex ? 'y' : 'ys', '(.*?)', ...cfg.rePart.ender)
  const reach = enderReach(cfg, opts)

  return function textMatcher(lex: Lex) {
    if (cfg.text.check) {
      // Check hooks are user code and may read lex.fwd directly.
      lex.refwd()
      let check = cfg.text.check(lex)
      if (check && check.done) {
        return check.token
      }
    }

    let mcfg = cfg.text
    let pnt = lex.pnt
    let def = cfg.value.def
    let defre = cfg.value.defre

    // Where this match starts. `tsrc` below was found at the END of
    // `msrc`, so it is only the token that follows the point if the
    // point actually got there — see the guard on subMatchFixed.
    let msI = pnt.sI

    ender.lastIndex = pnt.sI
    let m = ender.exec(lex.src)

    if (m) {
      let msrc = m[1]
      let tsrc = m[2]

      // The text stops where an ender is first found, and the enders tried
      // there read on as far as the longest: if that passes a window's
      // end, the text may run on, or end in a longer ender, once the
      // window has grown.
      if (lex.src.length < msI + (null == msrc ? 0 : msrc.length) + reach) {
        lex.touchEnd()
      }

      let out: Token | undefined = undefined

      if (null != msrc) {
        let mlen = msrc.length
        if (0 < mlen) {
          // Check for values first.
          let vs = undefined
          if (cfg.value.lex) {
            // Fixed values (e.g true, false, null).
            if (undefined !== (vs = def[msrc])) {
              out = lex.token('#VL', vs.val, msrc, pnt)
              pnt.sI += mlen
              pnt.cI += mlen
            }

            // Regexp processed values.
            else {
              // defre is a name-sorted array (see cfg.value.defre build in
              // utility.ts) — iteration order is deterministic.
              for (let vspec of defre) {
                if (vspec.match) {
                  // If consume, assume regexp starts with ^. Consuming
                  // value regexes are user-supplied and match against the
                  // remainder string (materialized lazily, memoized).
                  let res = vspec.match.exec(vspec.consume ? lex.refwd() : msrc)

                  // Must match entire text.
                  if (res && (vspec.consume || res[0].length === msrc.length)) {
                    let remsrc = res[0]

                    if (null == vspec.val) {
                      out = lex.token('#VL', remsrc, remsrc, pnt)
                    } else {
                      // `val` may be a FUNCTION of the match, or a plain
                      // value. A serialized grammar can only carry the
                      // second: JSON has no functions, and `@REF` reaches
                      // only names the host registered. Calling a string
                      // threw a TypeError, which the matcher loop turns
                      // into a #BD bad token — so a legal shared spec
                      // reported `unexpected` on VALID input, naming the
                      // character rather than the option. go/lexer.go has
                      // carried both shapes all along (`ValFunc` and
                      // `Val`). Audit item P10.
                      let val =
                        'function' === typeof vspec.val
                          ? vspec.val(res)
                          : vspec.val
                      out = lex.token('#VL', val, remsrc, pnt)
                    }

                    pnt.sI += remsrc.length
                    pnt.cI += remsrc.length
                  }
                }
              }
            }
          }

          // Not a value, so plain text.
          // NOTEL if !text.lex then only values are matched.
          if (null == out && mcfg.lex) {
            out = lex.token('#TX', msrc, msrc, pnt)
            pnt.sI += mlen
            pnt.cI += mlen
          }
        }
      }

      // A following fixed token can only match if there was already a
      // valid text or value match — AND if the point reached the place
      // `tsrc` was found.
      //
      // A CONSUMING value regexp matches against the forward source
      // rather than against `msrc`, so it may take a shorter prefix and
      // leave the point mid-run. `subMatchFixed` builds its token at the
      // point, not at the offset `tsrc` came from, then advances by that
      // token's length. With the point short of the ender, that
      // fabricated a delimiter at the wrong offset, swallowed whatever
      // stood there, and left the real delimiter to be emitted again:
      //
      //   value.def {at: {match: /^@\w+/, consume: true}}, src `@abc-rest,`
      //     was:  #VL"@abc"  #CA","@4  #TX"rest"  #CA","@9   (`-` gone)
      //     now:  #VL"@abc"  #TX"-rest"  #CA","@9
      //
      // go/lexer.go returns straight after a consuming value match and
      // re-enters the matcher, which is why it never had this; the token
      // stream above is Go's, measured. Audit item P9.
      if (out && pnt.sI === msI + (null == msrc ? 0 : msrc.length)) {
        out = subMatchFixed(lex, out, tsrc)
      }

      if (out && 0 < cfg.text.modify.length) {
        const modify = cfg.text.modify
        for (let mI = 0; mI < modify.length; mI++) {
          out.val = modify[mI](out.val, lex, cfg, opts)
        }
      }

      return out
    }
  }
}

let makeNumberMatcher: MakeLexMatcher = (cfg: Config, _opts: TabnasOptions) => {
  let mcfg = cfg.number

  // Sticky ('y') so the regex runs against the full source at an offset
  // (ender.lastIndex = pnt.sI) instead of an allocated remainder slice.
  // A base-prefixed run (0x…, 0o…, 0b…) is a COMPLETE integer: no fraction
  // and no exponent may attach to it. Only the decimal alternative carries
  // that tail.
  //
  // Both groups used to sit outside the alternation, so they attached to
  // base-prefixed runs too. `0xFF.5` then produced msrc `0xFF.5`, which
  // coerces to NaN, so the matcher declined the whole span and the lenient
  // text matcher took `0xFF` as #TX — while Go, whose base-prefix branch has
  // no tail at all, read #NR("0xFF") and left `.5` to following matchers.
  //
  // The follow character decided whether that was visible: `0xFF.a` agreed
  // by accident, because `.a` cannot open a fraction, so the regex
  // backtracked to `0xFF` and `.` served as the ender. Any test probing only
  // `0xFF.x` or `0xFF.5x` passes against the broken code — see the parity
  // fixtures, which deliberately carry both a diverging and an agreeing
  // follow character for each of 0x/0o/0b.
  //
  // Fraction digits, when present, must START with a digit, as the integer
  // and exponent runs already require: an earlier `\.[0-9]?(...)` let a
  // separator open the run, so `1._5` parsed as 1.5 — separators are legal
  // only BETWEEN digits.
  //
  // Every inner group is now non-capturing, so the number contributes
  // exactly ONE capture (msrc) and the ender's first group is m[2] rather
  // than m[9]. That retires "count parens in numberEnder!" as a hazard.
  // The marker letter is case-insensitive, as it is in Go's matchNumber
  // (src[sI+1] == 'x' || src[sI+1] == 'X', and likewise o/O and b/B) and as
  // JS `Number()` already is — +"0XFF" is 255, so the coercion below needed
  // no change. TS accepted only the lowercase marker, so `0XFF` lexed as
  // #TX here and #NR there.
  //
  // Widening TS to match Go, rather than narrowing Go to match TS: Go is the
  // reference port, and narrowing would break any Go consumer relying on
  // 0XFF being 255. It is also the internally consistent direction — hex
  // DIGITS are already case-insensitive in both ports (0xAB == 0xab), so
  // only the marker was asymmetric.
  let baseAlts = [
    mcfg.hex ? '[xX][0-9a-fA-F_]+' : null,
    mcfg.oct ? '[oO][0-7_]+' : null,
    mcfg.bin ? '[bB][01_]+' : null,
  ]
    .filter((s) => null != s)
    .join('|')

  let decimalRe =
    '\\.?[0-9]+(?:[0-9_]*[0-9])?' +
    '(?:\\.(?:[0-9](?:[0-9_]*[0-9])?)?)?' +
    '(?:[eE][-+]?[0-9]+(?:[0-9_]*[0-9])?)?'

  let ender = regexp(
    'y',
    (
      '([-+]?' +
      // With every base form disabled there is no prefix branch at all,
      // rather than a degenerate `0(?:)` that matches a bare `0`.
      ('' === baseAlts
        ? decimalRe
        : '(?:0(?:' + baseAlts + ')|' + decimalRe + ')')
    ).replace(/_/g, mcfg.sep ? escre(mcfg.sepChar as string) : ''),
    ')',
    ...cfg.rePart.ender,
  )

  let numberSep = mcfg.sep
    ? regexp('g', escre(mcfg.sepChar as string))
    : undefined

  // The characters a number is written in, the separator among them: a
  // run of them is as far as the number pattern can read before an ender,
  // which is how a matcher over a window knows whether the window's end
  // could have changed what it read.
  let numberish = regexp(
    'y',
    '[-+.0-9a-zA-Z' + (mcfg.sep ? escre(mcfg.sepChar as string) : '') + ']*',
  )
  const reach = enderReach(cfg, _opts)
  let numberSepChar = mcfg.sep ? (mcfg.sepChar as string) : undefined

  return guardedMatcher(cfg.number, function numberBody(lex) {
    mcfg = cfg.number
    let pnt = lex.pnt
    let valdef = cfg.value.def

    ender.lastIndex = pnt.sI
    let m = ender.exec(lex.src)
    // The pattern reads no further than the run of number characters at
    // the point, and an ender after where it stops: if that passes a
    // window's end, the number may be longer, or another, or follow on to
    // a longer ender, once the window has grown.
    if (lex.windowOpen) {
      numberish.lastIndex = pnt.sI
      numberish.test(lex.src)
      if (lex.src.length < numberish.lastIndex + reach) {
        lex.touchEnd()
      }
    }
    if (m) {
      let msrc = m[1]
      // m[2], not m[9]: the number contributes exactly one capture (msrc),
      // so the ender's first group follows it directly. Every inner group in
      // the number pattern is non-capturing precisely so this index cannot
      // drift again.
      let tsrc = m[2]

      let out: Token | undefined = undefined
      let included = true

      if (
        null != msrc &&
        (included = !cfg.number.exclude || !msrc.match(cfg.number.exclude))
      ) {
        let mlen = msrc.length
        if (0 < mlen) {
          let vs = undefined
          if (cfg.value.lex && undefined !== (vs = valdef[msrc])) {
            out = lex.token('#VL', vs.val, msrc, pnt)
          } else {
            // Strip separators only when one is actually present — the
            // common separator-free number skips the regex replace.
            let nstr =
              numberSep && numberSepChar && -1 < msrc.indexOf(numberSepChar)
                ? msrc.replace(numberSep, '')
                : msrc
            let num = +nstr

            // Special case: +- prefix of 0x... format
            if (isNaN(num)) {
              let first = nstr[0]
              if ('-' === first || '+' === first) {
                num = ('-' === first ? -1 : 1) * +nstr.substring(1)
              }
            }

            if (!isNaN(num)) {
              out = lex.token('#NR', num, msrc, pnt)
              pnt.sI += mlen
              pnt.cI += mlen
            }
            // Else let later matchers try.
          }
        }
      }

      // Emit a fixed token from the ender ONLY when the number itself was
      // consumed. The sticky regexp spans number+ender, so tsrc sits at
      // sI+mlen — but the point is only advanced past the number when a
      // token was produced. Emitting the ender while declining the number
      // (e.g. `0xFF.5`, where the run matches the shape but coerces to
      // NaN) advanced the point one fixed-token length INSIDE the
      // unconsumed run: the grammar saw a `,` or `]` that was never at
      // that position, fabricating null/empty elements, and the run's
      // first character was destroyed. `[0xFF.5]` parsed as
      // [[],"xFF.5"]. Declining must decline the WHOLE span, so the
      // lenient text matcher takes the run intact, as the Go port does.
      if (included && null != out) {
        out = subMatchFixed(lex, out, tsrc)
      }

      return out
    }
  })
}

// Fixed-width hex runs for `\xHH` and `\uHHHH`. Anchored and exact:
// a short or junk-terminated run is not a valid escape.
const HEX2 = /^[0-9a-fA-F]{2}$/
const HEX4 = /^[0-9a-fA-F]{4}$/

let makeStringMatcher: MakeLexMatcher = (cfg: Config, opts: TabnasOptions) => {
  // TODO: does `clean` make sense here?

  let os = opts.string || {}
  cfg.string = cfg.string || ({} as any)

  // Replace map-shaped fields outright rather than deep-merging — when
  // an instance reconfigures with stricter options (e.g. JSON's
  // `chars: '"'`), the old quote/multi-char/escape entries from the
  // permissive default would otherwise linger.
  cfg.string.quoteMap = charset(os.chars)
  cfg.string.quoteBitmap = charsBitmap(os.chars)
  cfg.string.multiChars = charset(os.multiChars)
  cfg.string.multiBitmap = charsBitmap(os.multiChars)
  cfg.string.escMap = { ...os.escape }
  cfg.string.replaceCodeMap = omap(
    clean({ ...os.replace }),
    ([c, r]: [string, any]) => [c.charCodeAt(0), r],
  )

  cfg.string = deep(cfg.string, {
    lex: !!os?.lex,
    escChar: os.escapeChar,
    escCharCode:
      null == os.escapeChar ? undefined : os.escapeChar.charCodeAt(0),
    allowUnknown: !!os.allowUnknown,
    // Strict escapes: disable the non-standard structural escapes \xHH
    // and \u{...} (plain \uXXXX stays). Combined with escape-map removals
    // and allowUnknown:false this yields JSON.parse-conformant handling.
    escapeStrict: !!os.escapeStrict,
    // Permit raw control chars (< 0x20) in a string body. Line chars
    // are excluded — they stay governed by multiChars.
    allowControl: !!os.allowControl,
    hasReplace: false,
    abandon: !!os.abandon,
  })

  // Assigned outside the deep() merge so that dropping the hook from
  // the options actually clears it (deep() ignores undefined). The
  // hook is honoured by guardedMatcher below and by buildLexDispatch
  // (a string matcher with a check must be a candidate for every first
  // char, not just the quote chars).
  cfg.string.check = os.check

  cfg.string.escMap = clean(cfg.string.escMap)
  // An escape mapped to '' (or null/undefined, removed by clean above)
  // is treated as removed, so a built-in escape such as \v can be
  // dropped via `string.escape: { v: '' }` — parity with the Go runtime.
  for (const k of keys(cfg.string.escMap)) {
    if ('' === cfg.string.escMap[k]) delete cfg.string.escMap[k]
  }
  cfg.string.escBitmap = charsBitmap(cfg.string.escMap)
  cfg.string.hasReplace = 0 < keys(cfg.string.replaceCodeMap).length

  // Pre-build a body-scan ScanSpec per quote character. The spec
  // classifies every byte 0..255 against THAT quote's role table
  // (the quote char, the escape char, replace chars, and control /
  // line chars depending on whether the quote allows multiline).
  // The body-scan loop below is then just a scan + dispatch.
  const bodySpecs = new Map<number, ScanSpec>()
  for (const qc of Object.keys(cfg.string.quoteMap)) {
    bodySpecs.set(qc.charCodeAt(0), buildStringBodySpec(cfg, qc))
  }
  const scanOut: ScanOut = { sI: 0, rI: 0, cI: 0 }

  return guardedMatcher(cfg.string, function stringBody(lex) {
    const mcfg = cfg.string
    const {
      quoteMap, quoteBitmap, escMap, escCharCode,
      multiChars, multiBitmap,
      allowUnknown, replaceCodeMap, hasReplace, escapeStrict,
    } = mcfg

    const { pnt, src } = lex
    const startSI = pnt.sI
    const startRI = pnt.rI
    const srclen = src.length
    const qcc = src.charCodeAt(startSI)

    // Is this byte an opening quote?
    if (!(qcc < 256 ? quoteBitmap[qcc] : quoteMap[src[startSI]])) return undefined

    const q = src[startSI]
    const isMultiLine =
      qcc < 256 ? !!multiBitmap[qcc] : !!multiChars[q]
    const bodySpec =
      bodySpecs.get(qcc) ||
      (() => {
        const spec = buildStringBodySpec(cfg, q)
        bodySpecs.set(qcc, spec)
        return spec
      })()

    let sI = startSI + 1
    let rI = startRI
    let cI = pnt.cI + 1

    // Escape-free fast path: until the first escape or replace is
    // processed the value is the contiguous source run after the opening
    // quote, so no segment buffer is needed — a clean quoted string costs
    // one substring instead of a buffer, per-segment pushes, and a join.
    let buf: string[] | undefined = undefined

    while (sI < srclen) {
      // Body scan: consume body chars (and multi-line newlines)
      // until something interesting (quote, escape, control, replace).
      const bodyStart = sI
      scan(src, sI, rI, cI, bodySpec, scanOut)
      sI = scanOut.sI
      rI = scanOut.rI
      cI = scanOut.cI
      if (undefined !== buf && bodyStart < sI)
        buf.push(src.substring(bodyStart, sI))

      if (sI >= srclen) break

      const cc = src.charCodeAt(sI)

      // Closing quote — string done.
      if (cc === qcc) {
        const val =
          undefined === buf ? src.substring(startSI + 1, sI) : buf.join(EMPTY)
        sI++
        const tkn = lex.token('#ST', val,
          undefined, pnt, undefined, undefined, sI - startSI)
        pnt.sI = sI
        pnt.rI = rI
        pnt.cI = cI + 1
        return tkn
      }

      // Replace map override.
      if (hasReplace) {
        const rs = replaceCodeMap[cc]
        if (rs !== undefined) {
          if (undefined === buf) {
            buf = startSI + 1 < sI ? [src.substring(startSI + 1, sI)] : []
          }
          buf.push(rs)
          sI++
          cI++
          continue
        }
      }

      // Escape sequence.
      if (cc === escCharCode) {
        if (undefined === buf) {
          buf = startSI + 1 < sI ? [src.substring(startSI + 1, sI)] : []
        }
        sI++
        cI++
        if (sI >= srclen) break // unterminated
        const ec = src[sI]
        const es = escMap[ec]
        if (es != null) {
          buf.push(es)
          sI++
          cI++
        } else if ('x' === ec && !escapeStrict) {
          sI++ // past 'x'
          if (srclen < sI + 2) lex.touchEnd()
          // Exactly two hex digits. `parseInt` stops at the first
          // non-hex character and succeeds on whatever prefix it found,
          // so `\x4Z` decoded as U+0004 and consumed the `Z` — emitting
          // a character the input never specified and silently deleting
          // one it did. At end of input the same leniency read `\x4` as
          // a complete escape, overshot, and reported the string as
          // unterminated. The braced form below already validates this
          // way; this makes the three escape forms agree.
          const xs = src.substring(sI, sI + 2)
          const xx = HEX2.test(xs) ? parseInt(xs, 16) : NaN
          if (isNaN(xx)) {
            if (mcfg.abandon) return undefined
            sI -= 2
            cI -= 1
            pnt.sI = sI
            pnt.cI = cI
            return lex.bad(S.invalid_ascii, sI, sI + 4)
          }
          buf.push(String.fromCharCode(xx))
          sI += 2
          cI += 3
        } else if ('u' === ec) {
          sI++ // past 'u'
          if (srclen <= sI) lex.touchEnd()
          if ('{' === src[sI] && !escapeStrict) {
            // Braced form \u{H...H}: 1-6 hex digits, any code point.
            const endI = src.indexOf('}', sI + 1)
            // The closing brace may lie past a window's end.
            if (-1 === endI) lex.touchEnd()
            const digits = -1 === endI ? '' : src.substring(sI + 1, endI)
            const uu =
              0 < digits.length && digits.length <= 6 && /^[0-9a-fA-F]+$/.test(digits)
                ? parseInt(digits, 16)
                : NaN
            if (isNaN(uu) || 0x10ffff < uu) {
              if (mcfg.abandon) return undefined
              sI = sI - 2
              pnt.sI = sI
              pnt.cI = cI - 1
              return lex.bad(S.invalid_unicode, sI, -1 === endI ? srclen : endI + 1)
            }
            buf.push(String.fromCodePoint(uu))
            cI += endI + 1 - sI + 1
            sI = endI + 1
          } else {
            // Exactly four hex digits — same reason as `\x` above.
            // `\u414Z` decoded as U+0414 and swallowed the `Z`.
            if (srclen < sI + 4) lex.touchEnd()
            const us = src.substring(sI, sI + 4)
            const uu = HEX4.test(us) ? parseInt(us, 16) : NaN
            if (isNaN(uu)) {
              if (mcfg.abandon) return undefined
              sI = sI - 2
              cI -= 1
              pnt.sI = sI
              pnt.cI = cI
              return lex.bad(S.invalid_unicode, sI, sI + 6)
            }
            buf.push(String.fromCharCode(uu))
            sI += 4
            cI += 5
          }
        } else if (allowUnknown) {
          buf.push(ec)
          sI++
          cI++
        } else {
          if (mcfg.abandon) return undefined
          pnt.sI = sI
          pnt.cI = cI
          return lex.bad(S.unexpected, sI, sI + 1)
        }
        continue
      }

      // Stopped on a control char (cc < 32) that wasn't a multi-line
      // newline (those are consumed by the spec). Either an embedded
      // line char in a non-multi-line string or a real unprintable.
      // Both are errors. Under string.allowControl the non-line control
      // chars are classified as plain body by buildStringBodySpec and
      // never reach here, so only the embedded-line case remains.
      if (cc < 32) {
        if (mcfg.abandon) return undefined
        pnt.sI = sI
        pnt.cI = cI
        return lex.bad(S.unprintable, sI, sI + 1)
      }

      // Unreachable — the spec only stops on classes handled above.
      break
    }

    // Hit EOF without closing quote. A window's end need not be the
    // source's: the string may yet close once the window has grown.
    lex.touchEnd()
    if (mcfg.abandon) return undefined
    pnt.rI = startRI
    return lex.bad(S.unterminated_string, startSI, sI)
  })
}

// Line ending matcher.
//
// The non-single path is the 3-class line-run state machine
// (LINE_RUN_TABLE). `single` mode tracks "has this exact char been
// seen yet" — that's not bounded by a small state count so it stays
// inline.
let makeLineMatcher: MakeLexMatcher = (cfg: Config, _opts: TabnasOptions) => {
  const spec = buildLineRunSpec(cfg.line)
  const out: ScanOut = { sI: 0, rI: 0, cI: 0 }

  return guardedMatcher(cfg.line, function lineBody(lex) {
    const { pnt, src } = lex

    if (cfg.line.single) {
      // Inline: tracks per-char counts to stop at the first repeat.
      const bm = cfg.line.charsBitmap
      const rbm = cfg.line.rowCharsBitmap
      const chars = cfg.line.chars
      const rowChars = cfg.line.rowChars
      let sI = pnt.sI
      let rI = pnt.rI
      let cc
      const counts: Record<string, number> = {}
      while ((cc = src.charCodeAt(sI)) < 256 ? bm[cc] : chars[src[sI]]) {
        const c = src[sI]
        const n = (counts[c] || 0) + 1
        counts[c] = n
        if (n > 1) break
        if (cc < 256 ? rbm[cc] : rowChars[src[sI]]) rI++
        sI++
      }
      // The run reached a window's end, which need not be its own.
      if (src.length <= sI) lex.touchEnd()
      if (pnt.sI < sI) {
        const tkn = lex.token(
          '#LN', undefined, undefined, pnt,
          undefined, undefined, sI - pnt.sI)
        pnt.sI = sI
        pnt.rI = rI
        pnt.cI = 1
        return tkn
      }
      return
    }

    const lined = scan(src, pnt.sI, pnt.rI, pnt.cI, spec, out)
    if (src.length <= out.sI) lex.touchEnd()
    if (lined) {
      const tkn = lex.token(
        '#LN', undefined, undefined, pnt,
        undefined, undefined, out.sI - pnt.sI)
      pnt.sI = out.sI
      pnt.rI = out.rI
      pnt.cI = 1
      return tkn
    }
  })
}

// Space matcher.
//
// Spec: walk a run of `cfg.space.chars`. Class 0 = not a space,
// class 1 = a space. The driver does the rest.
let makeSpaceMatcher: MakeLexMatcher = (cfg: Config, _opts: TabnasOptions) => {
  const spec = buildCharRunSpec(cfg.space.charsBitmap, cfg.space.chars)
  const out: ScanOut = { sI: 0, rI: 0, cI: 0 }

  return guardedMatcher(cfg.space, function spaceBody(lex) {
    const { pnt, src } = lex
    const spaced = scan(src, pnt.sI, pnt.rI, pnt.cI, spec, out)
    // The run reached a window's end, which need not be its own.
    if (src.length <= out.sI) lex.touchEnd()
    if (spaced) {
      const tkn = lex.token(
        '#SP', undefined, undefined, pnt,
        undefined, undefined, out.sI - pnt.sI)
      pnt.sI = out.sI
      pnt.rI = out.rI
      pnt.cI = out.cI
      return tkn
    }
  })
}

function subMatchFixed(
  lex: Lex,
  first: Token | undefined,
  tsrc: string | undefined,
): Token | undefined {
  let pnt = lex.pnt
  let out = first

  if (lex.cfg.fixed.lex && null != tsrc) {
    let tknlen = tsrc.length
    if (0 < tknlen) {
      let tkn: Token | undefined = undefined

      let tin = lex.cfg.fixed.token[tsrc]
      if (null != tin) {
        tkn = lex.token(tin, undefined, tsrc, pnt)
      }

      if (null != tkn) {
        pnt.sI += tkn.src.length
        pnt.cI += tkn.src.length

        if (null == first) {
          out = tkn
        } else {
          pnt.token.push(tkn)
        }
      }
    }
  }
  return out
}

// Built-in matcher names (the `matcher` annotation set in configure()).
// These matchers scan lex.src at pnt offsets or call refwd() themselves;
// anything else in the matcher list gets a fresh lex.fwd before running.
const BUILTIN_MATCHER = {
  match: 1, fixed: 1, space: 1, line: 1,
  string: 1, comment: 1, number: 1, text: 1,
} as Record<string, 1 | undefined>

// Lexer driver: holds the scan Point and runs the configured matchers in order via next().
class Lex {
  src = EMPTY              // Full source text being lexed.
  ctx = {} as Context     // Parse context (config, logging, subscribers).
  cfg = {} as Config      // Resolved configuration.
  pnt = makePoint(-1)     // Current scan position.
  fwd = EMPTY as string   // Source from pnt.sI onward (the unconsumed remainder).
  fwdSI = -1              // pnt.sI the current fwd slice was taken at (memo key).

  // A parse from a reader a window at a time (admin ADR-30): the window
  // over the source. While a token is cut from it (`#cutWindow`) `src` is
  // the window's text and the point counts from the window's start, `#off`
  // being the source position of that start, so the matchers read the
  // window as they read a whole text; outside a cut the point counts from
  // the source's start, as every token and error does.
  #win: SourceWindow | undefined = undefined
  #cutting = false
  #off = 0
  // A read reached the window's end while the source had more, so the
  // token being cut may be cut short (`touchEnd`).
  #touched = false
  // The matcher that made the last token cut, for the lex log.
  #match: LexMatcher | undefined = undefined
  // Where the last token the lexer gave that the parser does not ignore
  // ends: the window keeps the space, lines and comments after it, as the
  // Rust engine keeps them until the parser fetches its next lookahead.
  #keepFrom = 0

  // Negotiated-lexing constraint (cfg.lex.relex). When non-null, only
  // matchers that can produce one of these tins run, and tcol gating is
  // bypassed — the requesting alternate's own token list is a sharper
  // gate than the rule-level union. Set only inside relex().
  want = null as Tin[] | null

  // Where the point stood immediately before the last recut `relex`
  // COMMITTED. One reusable record: the parser copies out of it at the
  // moment of the commit (see parse_alts), because a later recut
  // overwrites it.
  relexUndo = {
    sI: -1,
    rI: -1,
    cI: -1,
    token: [] as Token[],
    end: undefined as Token | undefined,
  }

  // Slice the remainder lazily and memoize on the position — the slice
  // is only materialized for consumers that genuinely need a remainder
  // string (custom matchers, value.defre regexes), never once per token.
  refwd(): string {
    if (this.fwdSI !== this.pnt.sI) {
      // Outside a cut over a window the point counts from the source's
      // start, and the window's text from its own.
      const w = this.#win
      const at =
        undefined === w || this.#cutting ? this.pnt.sI : this.pnt.sI - w.low
      this.fwd = this.src.substring(at) as string
      this.fwdSI = this.pnt.sI
    }
    return this.fwd
  }

  // Negotiated lexing (cfg.lex.relex): re-cut the source span of an
  // already-lexed token, constrained to the tins the requesting
  // alternate wants. Returns the recut token — the cursor then sits
  // after it and any pending lookahead is discarded — or undefined,
  // with all lexer state restored.
  //
  // Why this exists: one character can be claimable by several
  // matchers ('"' by an escape class and by a fixed quote; '\n' by a
  // whitespace class and by a fixed literal). The first fetch commits
  // to one identity by matcher order, and the pushback buffer makes
  // that identity sticky across rule boundaries — so an alternate
  // wanting the other, equally valid, identity fails. Scannerless
  // notations (GBNF) hit this constantly; the parser knows which tins
  // it wants, so it is allowed to renegotiate the cut.
  relex(from: Token, want: Tin[], rule: Rule): Token | undefined {
    if (null == from || from.len <= 0 || from.sI < 0) {
      return undefined
    }

    const pnt = this.pnt
    const sI = pnt.sI
    const rI = pnt.rI
    const cI = pnt.cI
    const queue = pnt.token
    const end = pnt.end

    pnt.sI = from.sI
    pnt.rI = from.rI
    pnt.cI = from.cI
    pnt.token = []
    pnt.end = undefined

    this.want = want
    let tkn: Token | undefined = undefined
    try {
      tkn = this.next(rule)
    } finally {
      this.want = null
    }

    if (null == tkn || !want.includes(tkn.tin)) {
      pnt.sI = sI
      pnt.rI = rI
      pnt.cI = cI
      pnt.token = queue
      pnt.end = end
      return undefined
    }

    // Commit. The cursor now sits after the recut token; queued
    // lookahead derived from the old cut is stale and stays dropped.
    // Preserve any attached ignored token (it precedes the span and is
    // unaffected by the recut).
    if (null != from.ignored) {
      tkn.ignored = from.ignored
    }

    // Record where the point was, so the caller can undo this commit if
    // the alternate that asked for it goes on to fail. See `unrelex`.
    const undo = this.relexUndo
    undo.sI = sI
    undo.rI = rI
    undo.cI = cI
    undo.token = queue
    undo.end = end

    return tkn
  }


  // Undo a committed recut: put the point back where `relex` found it.
  // The caller passes its own copy of the snapshot rather than reading
  // `relexUndo` directly, because that field is overwritten by any
  // further recut and the undo must reach back to the FIRST one.
  unrelex(
    sI: number,
    rI: number,
    cI: number,
    token: Token[],
    end: Token | undefined,
  ): void {
    const pnt = this.pnt
    pnt.sI = sI
    pnt.rI = rI
    pnt.cI = cI
    pnt.token = token
    pnt.end = end
  }


  // Run one matcher under a `want` constraint, keeping its token only
  // when the tin is wanted and restoring the point otherwise. Used for
  // custom matchers during negotiated lexing, where the produced tin is
  // not knowable in advance. A matcher is free to move the cursor and
  // queue tokens, so the restore covers the same fields `relex` saves.
  speculate(
    mat: LexMatcher,
    rule: Rule,
    tI?: number,
  ): Token | undefined {
    const pnt = this.pnt
    const sI = pnt.sI
    const rI = pnt.rI
    const cI = pnt.cI
    const qN = pnt.token.length
    const end = pnt.end

    if (undefined === BUILTIN_MATCHER[(mat as any).matcher]) {
      this.refwd()
    }
    const tkn = mat(this, rule, tI)
    if (null != tkn && (this.want as Tin[]).includes(tkn.tin)) {
      return tkn
    }

    pnt.sI = sI
    pnt.rI = rI
    pnt.cI = cI
    pnt.token.length = qN
    pnt.end = end
    return undefined
  }

  constructor(ctx: Context, win?: SourceWindow) {
    this.ctx = ctx
    this.cfg = ctx.cfg
    if (undefined === win) {
      this.src = ctx.src()
      this.pnt = makePoint(this.src.length)
    } else {
      this.#win = win
      this.src = win.text
      this.pnt = makePoint(win.end)
    }
  }

  // Whether the lexer reads a window of a source read from a reader.
  get windowed(): boolean {
    return undefined !== this.#win
  }

  // Whether the lexer reads a window of a source that has more to give,
  // so that a read at the window's end may not be the source's.
  get windowOpen(): boolean {
    const w = this.#win
    return undefined !== w && !w.exhausted
  }

  // The built-in matchers' note that a read reached the end of the window
  // while the source has more to give: the token being cut may be cut
  // short, and is cut again over a wider window. Nothing in a whole text.
  touchEnd(): void {
    const w = this.#win
    if (undefined !== w && !w.exhausted) {
      this.#touched = true
    }
  }

  // The text from the cursor to the end of what the lexer holds: in a
  // parse from a reader, the end of the window. A matcher in a grammar that
  // declares `lex.window` reads the source through this and `fill`.
  window(): string {
    return this.refwd()
  }

  // Read the source until `ahead` UTF-16 units lie past the cursor, or it
  // ends, and say whether they do. A whole source is all read, so this
  // only answers. Text is never dropped while a token is being cut, and a
  // source that fails to read ends where it failed, the parse failing with
  // it at the next token. `src` is the grown window's text after a fill.
  fill(ahead: number): boolean {
    if (undefined !== this.#win) {
      this.#grow(ahead)
    }
    return ahead <= this.pnt.len - this.pnt.sI
  }

  // How much of the source has been read, in UTF-16 units: the whole
  // text's length, or what the window has read, which the parser's step
  // budget is measured against as it grows.
  lengthRead(): number {
    const w = this.#win
    return undefined === w ? this.src.length : w.end
  }

  // Whether a source read from a reader turns out empty: the window is
  // filled until it holds a character or the reader is done. A source that
  // fails before its first character is not empty: its failure is the
  // parse's error.
  isEmptySource(): boolean {
    const w = this.#win
    if (undefined === w) {
      return 0 === this.src.length
    }
    this.#grow(1)
    return 0 === w.end && undefined === w.failure
  }

  // The text an error shows its line from, the row and column that text
  // starts at, and the source position it starts at: the whole source,
  // from row 1 and column 1, or in a parse from a reader the window, whose
  // first line is cut where the window begins. The window is read on first
  // to the end of its last line, no further than a margin, so that the
  // line an error is on shows as a parse of the whole text shows it, but
  // for a line longer than the window.
  excerpt(): { text: string; row: number; col: number; off: number } {
    const w = this.#win
    if (undefined === w) {
      return { text: this.src, row: 1, col: 1, off: 0 }
    }
    const start = w.text.length
    let cut = start
    if (!w.text.endsWith('\n')) {
      let nl = -1
      for (;;) {
        nl = w.text.indexOf('\n', start)
        if (-1 !== nl || w.exhausted || start + w.margin <= w.text.length) {
          break
        }
        w.read()
      }
      cut = Math.min(-1 === nl ? w.text.length : nl + 1, start + w.margin)
      this.#synced()
    }
    return {
      text: w.text.substring(0, cut),
      row: w.lowRow,
      col: w.lowCol,
      off: w.low,
    }
  }

  // Fill the window until `ahead` units lie past the cursor or the source
  // is exhausted.
  #grow(ahead: number): void {
    const w = this.#win as SourceWindow
    const at = this.pnt.sI + (this.#cutting ? this.#off : 0)
    if (w.exhausted || ahead <= w.end - at) {
      return
    }
    while (!w.exhausted && w.end - at < ahead) {
      w.read()
    }
    this.#synced()
  }

  // The window's text changed: `src`, the point's length and the
  // remainder follow it.
  #synced(): void {
    const w = this.#win as SourceWindow
    this.src = w.text
    this.pnt.len = w.end - (this.#cutting ? this.#off : 0)
    this.fwdSI = -1
    this.fwd = EMPTY
  }

  // Drop the text before the oldest token the parser holds, once a block
  // of it has gathered behind the cursor: the parser's lookahead, the
  // tokens a rewind replays, and its consumed history, which a rewind may
  // replay and a re-cut then read again. Never during a negotiated cut,
  // which sets the parser's replay queue aside (`relex`).
  #release(): void {
    const w = this.#win as SourceWindow
    const pnt = this.pnt
    const floor = w.low + w.block
    if (pnt.sI < floor || null != this.want) {
      return
    }
    const ctx = this.ctx
    const notoken = ctx.NOTOKEN
    let keep = oldestHeld(ctx.t, Math.min(pnt.sI, this.#keepFrom), floor, notoken)
    keep = oldestHeld(pnt.token, keep, floor, notoken)
    keep = oldestHeld(ctx.v, keep, floor, notoken)
    if (keep < floor) {
      return
    }
    w.drop(keep)
    this.#synced()
  }

  // Cut one token from the window. The window is filled to its margin past
  // the cursor first, having dropped what no token the parser holds can
  // reach; the matchers then run over it as over a whole text, the point
  // counting from the window's start. A cut that read to the window's end
  // while the source had more is undone and made again over a window
  // twice as far ahead, so that it is the cut the whole text gives. A cut
  // that stands gives its tokens the source's positions and their own
  // text (`settleToken`).
  #cutWindow(rule: Rule, tI?: number): Token {
    if (this.#cutting) {
      // A matcher that asks the lexer for a token while one is being cut
      // gets one cut in the window's coordinates, which the cut it runs
      // in settles: nothing moves or is dropped under that cut. At the
      // window's end it is the source's end only once the source is.
      const p = this.pnt
      if (p.len <= p.sI) {
        this.touchEnd()
        this.#match = undefined
        return (this.#win as SourceWindow).exhausted
          ? this.token('#ZZ', undefined, '', p)
          : this.token('#BD', undefined, EMPTY, p, undefined, S.unexpected)
      }
      return this.#cut(rule, tI)
    }
    const w = this.#win as SourceWindow
    const pnt = this.pnt
    let ahead = w.margin
    for (;;) {
      this.#release()
      this.#grow(ahead)
      if (undefined !== w.failure) {
        throw this.#unreadable(rule)
      }
      const base = w.low
      const sI = pnt.sI
      const rI = pnt.rI
      const cI = pnt.cI
      const queue = pnt.token
      const qN = queue.length
      const end = pnt.end
      this.#cutting = true
      this.#off = base
      this.#touched = false
      pnt.sI = sI - base
      pnt.len = w.text.length
      this.fwdSI = -1
      let tkn: Token
      try {
        if (pnt.len <= pnt.sI) {
          // The source is exhausted at the cursor.
          this.#match = undefined
          tkn = pnt.end = this.token('#ZZ', undefined, '', pnt)
        } else {
          tkn = this.#cut(rule, tI)
        }
      } finally {
        this.#cutting = false
        this.#off = 0
        pnt.sI += base
        this.#synced()
      }
      // The cut stands when nothing read to the window's end while the
      // source had more, and it ended inside the window or the source is
      // all read. A read that touched the end decided on a window shorter
      // than the source, even where a later matcher's fill has since read
      // the source to its end: made again, over the window that fill left,
      // it touches nothing and stands (#326).
      if (!this.#touched && (w.exhausted || pnt.sI < pnt.len)) {
        settleToken(tkn, base)
        const made = pnt.token
        for (let i = made === queue ? qN : 0; i < made.length; i++) {
          if (made[i] !== tkn) {
            settleToken(made[i], base)
          }
        }
        if (undefined !== pnt.end && end !== pnt.end && tkn !== pnt.end) {
          settleToken(pnt.end, base)
        }
        return tkn
      }
      pnt.sI = sI
      pnt.rI = rI
      pnt.cI = cI
      queue.length = qN
      pnt.token = queue
      pnt.end = end
      ahead *= 2
    }
  }

  // The error a parse fails with when its source could not be read to its
  // end: `unreadable`, the reason as its token's text, at the character
  // after the last one read (Rust's `Unreadable::error`).
  #unreadable(rule: Rule): TabnasError {
    const f = (this.#win as SourceWindow).failure as Unreadable
    const tkn = makeToken(
      '#BD',
      tokenize('#BD', this.cfg),
      undefined,
      f.reason,
      makePoint(f.pos, f.pos, f.row, f.col),
      undefined,
      S.unreadable,
    )
    return new TabnasError(S.unreadable, {}, tkn, rule, this.ctx)
  }

  // Create a token. Pass `src` when the matcher already holds the
  // matched text; pass src=undefined with an explicit `len` to defer
  // the substring to first read (a span over this lexer's source).
  token(
    ref: Tin | string,
    val: any,
    src: string | undefined,
    pnt?: Point,
    use?: any,
    why?: string,
    len?: number,
  ): Token {
    let tin: Tin
    let name: string
    if ('string' === typeof ref) {
      name = ref
      tin = tokenize(name, this.cfg)
    } else {
      tin = ref
      name = tokenize(ref, this.cfg)
    }

    const p = pnt || this.pnt
    const w = this.#win
    if (undefined !== w && !this.#cutting) {
      // Made outside a cut over the window, where the point counts from
      // the source's start: the token takes its own text at once, since
      // the window moves on.
      if (undefined === src) {
        const at = p.sI - w.low
        src = this.src.substring(at, at + (len || 0))
      }
      return makeToken(name, tin, val, ownText(src), p, use, why, undefined, len)
    }

    let tkn = makeToken(
      name, tin, val, src, p, use, why, this.src, len,
    )

    return tkn
  }

  next(rule: Rule, alt?: NormAltSpec, altI?: number, tI?: number): Token {
    let tkn: Token | undefined
    let pnt = this.pnt
    let sI = pnt.sI
    let match: LexMatcher | undefined = undefined

    if (pnt.end) {
      tkn = pnt.end
    } else if (0 < pnt.token.length) {
      tkn = pnt.token.shift() as Token
    } else if (undefined !== this.#win) {
      tkn = this.#cutWindow(rule, tI)
      match = this.#match
    } else if (pnt.len <= pnt.sI) {
      pnt.end = this.token('#ZZ', undefined, '', pnt)

      tkn = pnt.end
    } else {
      tkn = this.#cut(rule, tI)
      match = this.#match
    }

    if (undefined !== this.#win) {
      const ignore = this.cfg.tokenSetTins.IGNORE
      if (null == ignore || !ignore[tkn.tin]) {
        this.#keepFrom = tkn.sI + Math.max(0, tkn.len)
      }
    }

    this.ctx.log &&
      this.ctx.log(
        S.lex,
        this.ctx,
        rule,
        this,
        pnt,
        sI,
        match,
        tkn,
        alt,
        altI,
        tI,
      )

    // this.ctx.log &&
    //   log_lex(this.ctx, rule, this, pnt, sI, match, tkn, alt, altI, tI)

    if (this.ctx.sub.lex) {
      this.ctx.sub.lex.map((sub) => sub(tkn as Token, rule, this.ctx))
    }

    return tkn
  }

  // Cut one token at the cursor: run the matchers that could make one
  // starting with its first character, in order, and take the first token
  // one makes, or a bad token if none does. The matcher that made it is
  // left in `#match` for the lex log.
  #cut(rule: Rule, tI?: number): Token {
    const pnt = this.pnt
    let tkn: Token | undefined
    let match: LexMatcher | undefined = undefined

    // First-char dispatch: run only the matchers that could produce a
    // token starting with this char (non-Latin-1 chars and configs
    // without a table run the full pipeline).
    const dispatch = this.cfg.lex.dispatch
    const cc = this.src.charCodeAt(pnt.sI)
    const mats =
      undefined === dispatch
        ? this.cfg.lex.match
        : dispatch[cc < 256 ? cc : 256]
    try {
      for (let mat of mats) {
        // Negotiated lexing: a single-token builtin whose tin is not
        // wanted cannot serve the request; skip it. match/fixed filter
        // internally, per candidate.
        if (null != this.want) {
          const nm = (mat as any).matcher
          if ('match' !== nm && 'fixed' !== nm) {
            const t: any = this.cfg.t
            const btin =
              'space' === nm ? t.SP :
                'line' === nm ? t.LN :
                  'string' === nm ? t.ST :
                    'comment' === nm ? t.CM :
                      'number' === nm ? t.NR :
                        'text' === nm ? t.TX : -1
            if (-1 === btin) {
              // A custom `lex.match` matcher. Its token identity is
              // opaque — the only way to learn whether it can serve
              // the request is to run it — so run it speculatively and
              // put the cursor back when what it produced is not
              // wanted. Skipping it outright would deny custom tokens
              // the renegotiation that match and fixed tokens get.
              const cand = this.speculate(mat, rule, tI)
              if (undefined === cand) continue
              tkn = cand
              match = mat
              break
            }
            if (!this.want.includes(btin)) {
              continue
            }
          }
        }

        if (undefined === BUILTIN_MATCHER[(mat as any).matcher]) {
          this.refwd()
        }
        if ((tkn = mat(this, rule, tI))) {
          match = mat
          break
        }
      }
    } catch (err: any) {
      tkn =
        tkn ||
        this.token(
          '#BD',
          undefined,
          this.src[pnt.sI],
          pnt,
          { err },
          err.code || S.unexpected,
        )
    }

    this.#match = match
    return (
      tkn ||
      this.token(
        '#BD',
        undefined,
        this.src[pnt.sI],
        pnt,
        undefined,
        S.unexpected,
      )
    )
  }

  tokenize<R extends string | Tin, T extends R extends Tin ? string : Tin>(
    ref: R,
  ): T {
    return tokenize(ref, this.cfg)
  }

  bad(why: string, pstart: number, pend: number) {
    if (this.src.length < pend) {
      // A span cut at a window's end may be cut short.
      this.touchEnd()
    }
    return this.token(
      '#BD',
      undefined,
      0 <= pstart && pstart <= pend
        ? this.src.substring(pstart, pend)
        : this.src[this.pnt.sI],
      undefined,
      undefined,
      why,
    )
  }
}

const makeLex = (...params: ConstructorParameters<typeof Lex>) =>
  new Lex(...params)

export {
  Lex,
  Point,
  Token,
  makeNoToken,
  makeLex,
  makePoint,
  makeToken,
  makeMatchMatcher,
  makeFixedMatcher,
  makeSpaceMatcher,
  makeLineMatcher,
  makeStringMatcher,
  makeCommentMatcher,
  makeNumberMatcher,
  makeTextMatcher,
  // Lex scan primitives — exposed so plugin authors can build their
  // own matchers on the same state-machine driver.
  guardedMatcher,
  scan,
  buildCharRunSpec,
  buildLineRunSpec,
  buildStringBodySpec,
  CONSUME,
  IS_ROW,
  CI_RESET,
  STOP,
  STATE_MASK,
  // A parse from a reader, a window at a time (admin ADR-30).
  SourceWindow,
  WINDOW_SIZES,
  NOT_UTF8,
  utf8Prefix,
  readWhole,
}

export type { ScanSpec, ScanOut, Reader, WindowSizes, Unreadable }
