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
    } else if (cc < 32) {
      if (isMultiLine && lineBM[cc]) {
        classOf[cc] = rowBM[cc] ? 3 : 2
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

    for (const cand of cands) {
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

    let oc = 'o' === (rule as Rule).state ? 0 : 1

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

    for (let tokenMatcher of tokenMatchers) {
      // Only match Token if present in Rule sequence.
      // Exception: an `eager$` flag on the matcher opts out of
      // tcol gating — the matcher fires whenever its regex matches
      // and the downstream parser rejects tokens it doesn't expect
      // at the current position. This is what ABNF's
      // case-insensitive literals need: the lexer has to emit the
      // literal's own tin even when the current rule's tcol is
      // narrower, so the next rule up the stack can see the token
      // as its proper type rather than falling through to #TX.

      if (
        (tokenMatcher as any).tin$ &&
        !(tokenMatcher as any).eager$ &&
        !rule.spec.def.tcol[oc][tI].includes((tokenMatcher as any).tin$)
      ) {
        continue
      }

      if (tokenMatcher instanceof RegExp) {
        let m = fwd.match(tokenMatcher)

        if (m) {
          let msrc = m[0]
          let mlen = msrc.length
          if (0 < mlen) {
            let tkn: Token | undefined = undefined

            let tin = (tokenMatcher as any).tin$
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
  })
}

// NOTE 1: matchers return arbitrary tokens and describe lexing using
// code, rather than a grammar. Thus, for example, some matchers below
// will check (using subMatchFixed) if their source text actually represents
// a fixed value.

// NOTE 2: matchers can place a second token onto the Point tokens,
// supporting two token lookahead.

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
          let n = commentSuffixMatch(src, aI, mc.suffixes)
          if (n > 0) { suffixLen = n; break }
          n = commentSuffixFnMatch(lex, aI, mc.suffixFn)
          if (n > 0) { suffixLen = n; break }
          cI++
          aI++
        }

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
      if (src.startsWith(mc.start, pnt.sI)) {
        let srclen = src.length
        let aI = pnt.sI + mc.start.length
        let end = mc.end as string
        cI += mc.start.length
        let suffixLen = 0
        let cc
        while (aI < srclen && !src.startsWith(end, aI)) {
          let n = commentSuffixMatch(src, aI, mc.suffixes)
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
function commentSuffixMatch(src: string, aI: number, suffixes?: string[]): number {
  if (!suffixes || 0 === suffixes.length) return 0
  for (let s of suffixes) {
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

// Match text, checking for literal values, optionally followed by a fixed token.
// Text strings are terminated by end markers.
let makeTextMatcher: MakeLexMatcher = (cfg: Config, opts: TabnasOptions) => {
  // Sticky ('y') so the regex runs against the full source at an offset
  // (ender.lastIndex = pnt.sI) instead of an allocated remainder slice.
  let ender = regexp(cfg.line.lex ? 'y' : 'ys', '(.*?)', ...cfg.rePart.ender)

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

    ender.lastIndex = pnt.sI
    let m = ender.exec(lex.src)

    if (m) {
      let msrc = m[1]
      let tsrc = m[2]

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
                      let val = vspec.val(res)
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
      // valid text or value match.
      if (out) {
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
  let ender = regexp(
    'y',
    [
      '([-+]?(0(',
      [
        mcfg.hex ? 'x[0-9a-fA-F_]+' : null,
        mcfg.oct ? 'o[0-7_]+' : null,
        mcfg.bin ? 'b[01_]+' : null,
      ]
        .filter((s) => null != s)
        .join('|'),
      // ')|[.0-9]+([0-9_]*[0-9])?)',
      ')|\\.?[0-9]+([0-9_]*[0-9])?)',
      '(\\.[0-9]?([0-9_]*[0-9])?)?',
      '([eE][-+]?[0-9]+([0-9_]*[0-9])?)?',
    ]
      .join('')
      .replace(/_/g, mcfg.sep ? escre(mcfg.sepChar as string) : ''),
    ')',
    ...cfg.rePart.ender,
  )

  let numberSep = mcfg.sep
    ? regexp('g', escre(mcfg.sepChar as string))
    : undefined
  let numberSepChar = mcfg.sep ? (mcfg.sepChar as string) : undefined

  return guardedMatcher(cfg.number, function numberBody(lex) {
    mcfg = cfg.number
    let pnt = lex.pnt
    let valdef = cfg.value.def

    ender.lastIndex = pnt.sI
    let m = ender.exec(lex.src)
    if (m) {
      let msrc = m[1]
      let tsrc = m[9] // NOTE: count parens in numberEnder!

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

      if (included) {
        out = subMatchFixed(lex, out, tsrc)
      }

      return out
    }
  })
}

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
    hasReplace: false,
    abandon: !!os.abandon,
  })

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
          const xx = parseInt(src.substring(sI, sI + 2), 16)
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
          if ('{' === src[sI] && !escapeStrict) {
            // Braced form \u{H...H}: 1-6 hex digits, any code point.
            const endI = src.indexOf('}', sI + 1)
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
            const uu = parseInt(src.substring(sI, sI + 4), 16)
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
      // Both are errors.
      if (cc < 32) {
        if (mcfg.abandon) return undefined
        pnt.sI = sI
        pnt.cI = cI
        return lex.bad(S.unprintable, sI, sI + 1)
      }

      // Unreachable — the spec only stops on classes handled above.
      break
    }

    // Hit EOF without closing quote.
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

    if (scan(src, pnt.sI, pnt.rI, pnt.cI, spec, out)) {
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
    if (scan(src, pnt.sI, pnt.rI, pnt.cI, spec, out)) {
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

  // Slice the remainder lazily and memoize on the position — the slice
  // is only materialized for consumers that genuinely need a remainder
  // string (custom matchers, value.defre regexes), never once per token.
  refwd(): string {
    if (this.fwdSI !== this.pnt.sI) {
      this.fwd = this.src.substring(this.pnt.sI) as string
      this.fwdSI = this.pnt.sI
    }
    return this.fwd
  }

  constructor(ctx: Context) {
    this.ctx = ctx
    this.src = ctx.src()
    this.cfg = ctx.cfg
    this.pnt = makePoint(this.src.length)
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

    let tkn = makeToken(
      name, tin, val, src, pnt || this.pnt, use, why, this.src, len,
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
    } else if (pnt.len <= pnt.sI) {
      pnt.end = this.token('#ZZ', undefined, '', pnt)

      tkn = pnt.end
    } else {
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

      tkn =
        tkn ||
        this.token(
          '#BD',
          undefined,
          this.src[pnt.sI],
          pnt,
          undefined,
          S.unexpected,
        )
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

  tokenize<R extends string | Tin, T extends R extends Tin ? string : Tin>(
    ref: R,
  ): T {
    return tokenize(ref, this.cfg)
  }

  bad(why: string, pstart: number, pend: number) {
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
}

export type { ScanSpec, ScanOut }
