/* Copyright (c) 2013-2022 Richard Rodger, MIT License */

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

// Position tracking inside the source string. The lexer threads a
// single Point through the parse — sI advances as characters are
// consumed; rI/cI track the human-readable row/column for error
// messages; token is the pending-token queue (rewind feeds it).
class Point {
  len = -1
  sI = 0
  rI = 1
  cI = 1
  token: Token[] = []
  end?: Token

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

// Tokens from the lexer.
// A single lexed token. `tin` is the numeric token id (a Tin); `val`
// is the JS-typed value (e.g. a number for #NR); `src` is the raw
// matching source text. Match positions are kept on `pnt` for error
// reporting and rewind.
class Token {
  isToken = true
  name = EMPTY
  tin: Tin = -1 as Tin
  val: any = undefined
  src = EMPTY
  sI = -1
  rI = -1
  cI = -1
  len = -1
  use?: Record<string, any>
  err?: string
  why?: string
  ignored?: Token

  constructor(
    name: string,
    tin: Tin,
    val: any,
    src: string,
    pnt: Point,
    use?: any,
    why?: string,
  ) {
    this.name = name
    this.tin = tin
    this.src = src
    this.val = val
    this.sI = pnt.sI
    this.rI = pnt.rI
    this.cI = pnt.cI
    this.use = use
    this.why = why

    this.len = null == src ? 0 : src.length
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
// factories are re-invoked on every `am.make()` clone (via
// `configure()`), so a stale closure can never outlive the cfg
// snapshot it was built from.
function guardedMatcher(
  mcfg: { lex: boolean; check?: LexCheck | undefined },
  body: LexMatcher,
): LexMatcher {
  return function guarded(lex, rule, tI) {
    if (!mcfg.lex) return undefined
    if (mcfg.check) {
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

type ScanSpec = {
  readonly initialState: number
  readonly nclasses: number
  readonly classOf: Uint8Array
  readonly fallback: (c: string) => number
  readonly table: Int32Array
}

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

  // Bytes >= 256 are always plain body chars (no special meaning).
  const fallback = (_c: string): number => 0

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



let makeFixedMatcher: MakeLexMatcher = (cfg: Config, _opts: TabnasOptions) => {
  let fixed = regexp(null, '^(', cfg.rePart.fixed, ')')

  return guardedMatcher(cfg.fixed, function fixedBody(lex) {
    const mcfg = cfg.fixed
    let pnt = lex.pnt
    let fwd = lex.fwd

    let m = fwd.match(fixed)
    if (m) {
      let msrc = m[1]
      let mlen = msrc.length
      if (0 < mlen) {
        let tkn: Token | undefined = undefined

        let tin = mcfg.token[msrc]
        if (null != tin) {
          tkn = lex.token(tin, undefined, msrc, pnt)

          pnt.sI += mlen
          pnt.cI += mlen
        }

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
    let fwd = lex.fwd

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

  return guardedMatcher(cfg.comment, function commentBody(lex) {
    let pnt = lex.pnt
    let fwd = lex.fwd

    let rI = pnt.rI
    let cI = pnt.cI

    // Single line comment.

    const lineBM = cfg.line.charsBitmap
    const rowBM = cfg.line.rowCharsBitmap
    const lineChars = cfg.line.chars
    const rowChars = cfg.line.rowChars

    for (let mc of lineComments) {
      if (fwd.startsWith(mc.start)) {
        let fwdlen = fwd.length
        let fI = mc.start.length
        cI += mc.start.length
        let suffixLen = 0
        let cc
        while (
          fI < fwdlen &&
          !((cc = fwd.charCodeAt(fI)) < 256
            ? lineBM[cc]
            : lineChars[fwd[fI]])
        ) {
          let n = commentSuffixMatch(fwd, fI, mc.suffixes)
          if (n > 0) { suffixLen = n; break }
          n = commentSuffixFnMatch(lex, fI, mc.suffixFn)
          if (n > 0) { suffixLen = n; break }
          cI++
          fI++
        }

        if (suffixLen > 0) {
          // Consume the suffix as the tail of the comment body.
          fI += suffixLen
          cI += suffixLen
        }
        else if (mc.eatline) {
          // Only absorb trailing line chars when termination came from
          // a line char (not from a suffix match). cI is intentionally
          // NOT pulled from scanOut — current semantics leave the
          // pnt.cI at end-of-comment-body even after eating newlines.
          scan(fwd, fI, rI, cI, lineRunSpec, scanOut)
          rI = scanOut.rI
          fI = scanOut.sI
        }

        let csrc = fwd.substring(0, fI)
        let tkn = lex.token('#CM', undefined, csrc, pnt)

        pnt.sI += csrc.length
        pnt.cI = cI
        pnt.rI = rI

        return tkn
      }
    }

    // Multiline comment.

    for (let mc of blockComments) {
      if (fwd.startsWith(mc.start)) {
        let fwdlen = fwd.length
        let fI = mc.start.length
        let end = mc.end as string
        cI += mc.start.length
        let suffixLen = 0
        let cc
        while (fI < fwdlen && !fwd.startsWith(end, fI)) {
          let n = commentSuffixMatch(fwd, fI, mc.suffixes)
          if (n > 0) { suffixLen = n; break }
          n = commentSuffixFnMatch(lex, fI, mc.suffixFn)
          if (n > 0) { suffixLen = n; break }
          cc = fwd.charCodeAt(fI)
          if (cc < 256 ? rowBM[cc] : rowChars[fwd[fI]]) {
            rI++
            cI = 0
          }

          cI++
          fI++
        }

        if (suffixLen > 0) {
          // Advance through the consumed suffix, tracking newlines.
          for (let k = 0; k < suffixLen; k++) {
            cc = fwd.charCodeAt(fI + k)
            if (cc < 256 ? rowBM[cc] : rowChars[fwd[fI + k]]) {
              rI++
              cI = 0
            }
            cI++
          }
          let csrc = fwd.substring(0, fI + suffixLen)
          let tkn = lex.token('#CM', undefined, csrc, pnt)
          pnt.sI += csrc.length
          pnt.rI = rI
          pnt.cI = cI
          return tkn
        }

        if (fwd.startsWith(end, fI)) {
          cI += end.length

          if (mc.eatline) {
            scan(fwd, fI, rI, cI, lineRunSpec, scanOut)
            rI = scanOut.rI
            fI = scanOut.sI
          }

          let csrc = fwd.substring(0, fI + end.length)
          let tkn = lex.token('#CM', undefined, csrc, pnt)

          pnt.sI += csrc.length
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
// fwd[fI:] or 0 if none matches. Suffixes are pre-sorted longest-first,
// so the first match is the best match.
function commentSuffixMatch(fwd: string, fI: number, suffixes?: string[]): number {
  if (!suffixes || 0 === suffixes.length) return 0
  for (let s of suffixes) {
    if (fwd.substring(fI, fI + s.length) === s) return s.length
  }
  return 0
}

// commentSuffixFnMatch probes the LexMatcher-form suffix terminator at
// offset fI. Returns the length of the returned token's src (to be
// consumed) or 0 if no termination. The lex point is snapshotted and
// restored so a misbehaving matcher can't advance the stream itself.
function commentSuffixFnMatch(lex: Lex, fI: number, fn?: LexMatcher): number {
  if (!fn) return 0
  let pnt = lex.pnt
  let savedSI = pnt.sI
  let savedRI = pnt.rI
  let savedCI = pnt.cI
  pnt.sI = savedSI + fI
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
  let ender = regexp(cfg.line.lex ? null : 's', '^(.*?)', ...cfg.rePart.ender)

  return function textMatcher(lex: Lex) {
    if (cfg.text.check) {
      let check = cfg.text.check(lex)
      if (check && check.done) {
        return check.token
      }
    }

    let mcfg = cfg.text
    let pnt = lex.pnt
    let fwd = lex.fwd
    let def = cfg.value.def
    let defre = cfg.value.defre

    let m = fwd.match(ender)

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
                  // If consume, assume regexp starts with ^.
                  let res = vspec.match.exec(vspec.consume ? fwd : msrc)

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

  let ender = regexp(
    null,
    [
      '^([-+]?(0(',
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

  return guardedMatcher(cfg.number, function numberBody(lex) {
    mcfg = cfg.number
    let pnt = lex.pnt
    let fwd = lex.fwd
    let valdef = cfg.value.def

    let m = fwd.match(ender)
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
            let nstr = numberSep ? msrc.replace(numberSep, '') : msrc
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
    hasReplace: false,
    abandon: !!os.abandon,
  })

  cfg.string.escMap = clean(cfg.string.escMap)
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
      allowUnknown, replaceCodeMap, hasReplace,
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

    const buf: string[] = []

    while (sI < srclen) {
      // Body scan: consume body chars (and multi-line newlines)
      // until something interesting (quote, escape, control, replace).
      const bodyStart = sI
      scan(src, sI, rI, cI, bodySpec, scanOut)
      sI = scanOut.sI
      rI = scanOut.rI
      cI = scanOut.cI
      if (bodyStart < sI) buf.push(src.substring(bodyStart, sI))

      if (sI >= srclen) break

      const cc = src.charCodeAt(sI)

      // Closing quote — string done.
      if (cc === qcc) {
        sI++
        const tkn = lex.token('#ST', buf.join(EMPTY),
          src.substring(startSI, sI), pnt)
        pnt.sI = sI
        pnt.rI = rI
        pnt.cI = cI + 1
        return tkn
      }

      // Replace map override.
      if (hasReplace) {
        const rs = replaceCodeMap[cc]
        if (rs !== undefined) {
          buf.push(rs)
          sI++
          cI++
          continue
        }
      }

      // Escape sequence.
      if (cc === escCharCode) {
        sI++
        cI++
        if (sI >= srclen) break // unterminated
        const ec = src[sI]
        const es = escMap[ec]
        if (es != null) {
          buf.push(es)
          sI++
          cI++
        } else if ('x' === ec) {
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
          const ux = '{' === src[sI] ? (sI++, 1) : 0
          const ulen = ux ? 6 : 4
          const uu = parseInt(src.substring(sI, sI + ulen), 16)
          if (isNaN(uu)) {
            if (mcfg.abandon) return undefined
            sI = sI - 2 - ux
            cI -= 1
            pnt.sI = sI
            pnt.cI = cI
            return lex.bad(S.invalid_unicode, sI, sI + ulen + 2 + 2 * ux)
          }
          buf.push(String.fromCodePoint(uu))
          sI += ulen + ux
          cI += ulen + 1 + ux
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
        const msrc = src.substring(pnt.sI, sI)
        const tkn = lex.token('#LN', undefined, msrc, pnt)
        pnt.sI = sI
        pnt.rI = rI
        pnt.cI = 1
        return tkn
      }
      return
    }

    if (scan(src, pnt.sI, pnt.rI, pnt.cI, spec, out)) {
      const msrc = src.substring(pnt.sI, out.sI)
      const tkn = lex.token('#LN', undefined, msrc, pnt)
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
      const msrc = src.substring(pnt.sI, out.sI)
      const tkn = lex.token('#SP', undefined, msrc, pnt)
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

// The lexer driver. Holds a Point for the current scan position and
// runs the configured matchers in order. `next()` advances; `peek()`
// looks ahead without consuming.
class Lex {
  src = EMPTY
  ctx = {} as Context
  cfg = {} as Config
  pnt = makePoint(-1)
  fwd = EMPTY as string

  refwd(): string {
    this.fwd = this.src.substring(this.pnt.sI) as string
    return this.fwd
  }

  constructor(ctx: Context) {
    this.ctx = ctx
    this.src = ctx.src()
    this.cfg = ctx.cfg
    this.pnt = makePoint(this.src.length)
  }

  token(
    ref: Tin | string,
    val: any,
    src: string,
    pnt?: Point,
    use?: any,
    why?: string,
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

    let tkn = makeToken(name, tin, val, src, pnt || this.pnt, use, why)

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
      this.fwd = this.src.substring(pnt.sI) as string
      try {
        for (let mat of this.cfg.lex.match) {
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
