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

import type { AmagamaOptions } from './amagama'

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


// Consume a run of line characters from `src[sI..]`, counting how
// many were row-incrementing (`rowBitmap` / `rowChars`). The bitmap
// is the ASCII fast path; the `Chars` object catches non-ASCII
// members. When `single` is true the walk stops as soon as the same
// character would repeat (so callers emit one #LN per newline).
//
// Used by the comment matcher's `eatline` tails. The hot matchers
// (space, line) inline this pattern by hand to dodge the per-call
// allocation of the {sI, rows} return object — the bench showed
// the call-site regression was almost entirely that allocation.
function runLineChars(
  src: string,
  sI: number,
  bitmap: Uint8Array,
  chars: Record<string, any>,
  rowBitmap: Uint8Array,
  rowChars: Record<string, any>,
  single: boolean,
): { sI: number; rows: number } {
  let cc
  let rows = 0
  let counts: Record<string, number> | undefined = single ? {} : undefined
  while ((cc = src.charCodeAt(sI)) < 256 ? bitmap[cc] : chars[src[sI]]) {
    if (counts) {
      const c = src[sI]
      const n = (counts[c] || 0) + 1
      counts[c] = n
      if (n > 1) break
    }
    if (cc < 256 ? rowBitmap[cc] : rowChars[src[sI]]) rows++
    sI++
  }
  return { sI, rows }
}


let makeFixedMatcher: MakeLexMatcher = (cfg: Config, _opts: AmagamaOptions) => {
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

let makeMatchMatcher: MakeLexMatcher = (cfg: Config, _opts: AmagamaOptions) => {
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

let makeCommentMatcher: MakeLexMatcher = (cfg: Config, opts: AmagamaOptions) => {
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
          // a line char (not from a suffix match).
          const r = runLineChars(
            fwd, fI, lineBM, lineChars, rowBM, rowChars, false,
          )
          rI += r.rows
          fI = r.sI
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
            const r = runLineChars(
              fwd, fI, lineBM, lineChars, rowBM, rowChars, false,
            )
            rI += r.rows
            fI = r.sI
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
let makeTextMatcher: MakeLexMatcher = (cfg: Config, opts: AmagamaOptions) => {
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

let makeNumberMatcher: MakeLexMatcher = (cfg: Config, _opts: AmagamaOptions) => {
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

let makeStringMatcher: MakeLexMatcher = (cfg: Config, opts: AmagamaOptions) => {
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

  return guardedMatcher(cfg.string, function stringBody(lex) {
    const mcfg = cfg.string
    let {
      quoteMap,
      quoteBitmap,
      escMap,
      escBitmap,
      escChar,
      escCharCode,
      multiChars,
      multiBitmap,
      allowUnknown,
      replaceCodeMap,
      hasReplace,
    } = mcfg

    let { pnt, src } = lex
    let { sI, rI, cI } = pnt
    let srclen = src.length
    let qcc = src.charCodeAt(sI)

    if (qcc < 256 ? quoteBitmap[qcc] : quoteMap[src[sI]]) {
      const q = src[sI] // Quote character
      const qI = sI
      const qrI = rI
      const isMultiLine =
        qcc < 256 ? multiBitmap[qcc] : multiChars[q]
      ++sI
      ++cI

      let s: string[] = []
      let rs: string | undefined

      for (sI; sI < srclen; sI++) {
        cI++
        let c = src[sI]
        rs = undefined

        // Quote char.
        if (q === c) {
          sI++
          break // String finished.
        }

        // Escape char.
        else if (escChar === c) {
          sI++
          cI++

          let es = escMap[src[sI]]

          if (null != es) {
            s.push(es)
          }

          // ASCII escape \x**
          else if ('x' === src[sI]) {
            sI++
            let cc = parseInt(src.substring(sI, sI + 2), 16)

            if (isNaN(cc)) {
              if (mcfg.abandon) {
                return undefined
              }
              sI = sI - 2
              cI -= 2
              pnt.sI = sI
              pnt.cI = cI
              return lex.bad(S.invalid_ascii, sI, sI + 4)
            }

            let us = String.fromCharCode(cc)

            s.push(us)
            sI += 1 // Loop increments sI.
            cI += 2
          }

          // Unicode escape \u**** and \u{*****}.
          else if ('u' === src[sI]) {
            sI++
            let ux = '{' === src[sI] ? (sI++, 1) : 0
            let ulen = ux ? 6 : 4

            let cc = parseInt(src.substring(sI, sI + ulen), 16)

            if (isNaN(cc)) {
              if (mcfg.abandon) {
                return undefined
              }
              sI = sI - 2 - ux
              cI -= 2

              pnt.sI = sI
              pnt.cI = cI
              return lex.bad(S.invalid_unicode, sI, sI + ulen + 2 + 2 * ux)
            }

            let us = String.fromCodePoint(cc)

            s.push(us)
            sI += ulen - 1 + ux // Loop increments sI.
            cI += ulen + ux
          } else if (allowUnknown) {
            s.push(src[sI])
          } else {
            if (mcfg.abandon) {
              return undefined
            }
            pnt.sI = sI
            pnt.cI = cI - 1
            return lex.bad(S.unexpected, sI, sI + 1)
          }
        } else if (
          hasReplace &&
          undefined !== (rs = replaceCodeMap[src.charCodeAt(sI)])
        ) {
          s.push(rs)
          cI++
        }

        // Body part of string.
        else {
          let bI = sI

          // TODO: move to cfgx
          let qc = q.charCodeAt(0)
          let cc = src.charCodeAt(sI)

          while (
            (!hasReplace || undefined === (rs = replaceCodeMap[cc])) &&
            sI < srclen &&
            32 <= cc &&
            qc !== cc &&
            escCharCode !== cc
          ) {
            cc = src.charCodeAt(++sI)
            cI++
          }
          cI--

          if (undefined === rs && cc < 32) {
            // TODO: move up - allow c < 32 to be a line char
            // cc < 32 always so the bitmap is exhaustive here.
            if (isMultiLine && cfg.line.charsBitmap[cc]) {
              if (cfg.line.rowCharsBitmap[cc]) {
                pnt.rI = ++rI
              }

              cI = 1
              s.push(src.substring(bI, sI + 1))
            } else {
              if (mcfg.abandon) {
                return undefined
              }
              pnt.sI = sI
              pnt.cI = cI
              return lex.bad(S.unprintable, sI, sI + 1)
            }
          } else {
            s.push(src.substring(bI, sI))
            sI--
          }
        }
      }

      if (src[sI - 1] !== q || pnt.sI === sI - 1) {
        if (mcfg.abandon) {
          return undefined
        }
        pnt.rI = qrI
        return lex.bad(S.unterminated_string, qI, sI)
      }

      const tkn = lex.token(
        '#ST',
        s.join(EMPTY),
        src.substring(pnt.sI, sI),
        pnt,
      )

      pnt.sI = sI
      pnt.rI = rI
      pnt.cI = cI
      return tkn
    }
  })
}

// Line ending matcher.
//
// Spec (matches `runLineChars`, inlined here for speed since this is
// a per-newline hot path): "Consume a run of `cfg.line.chars`,
// counting `rowChars` to advance the row counter. If `single`, stop
// as soon as the same character would repeat (so each newline emits
// its own #LN)."
let makeLineMatcher: MakeLexMatcher = (cfg: Config, _opts: AmagamaOptions) =>
  guardedMatcher(cfg.line, function lineBody(lex) {
    const { pnt, src } = lex
    const bm = cfg.line.charsBitmap
    const rbm = cfg.line.rowCharsBitmap
    const chars = cfg.line.chars
    const rowChars = cfg.line.rowChars
    const single = cfg.line.single

    let sI = pnt.sI
    let rI = pnt.rI
    let cc
    const counts: Record<string, number> | undefined = single ? {} : undefined

    while ((cc = src.charCodeAt(sI)) < 256 ? bm[cc] : chars[src[sI]]) {
      if (counts) {
        const c = src[sI]
        const n = (counts[c] || 0) + 1
        counts[c] = n
        if (n > 1) break
      }
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
  })

// Space matcher.
//
// Spec (matches `runChars`, inlined here for speed since this is a
// per-whitespace hot path): "Consume a run of `cfg.space.chars`;
// emit one #SP token covering the run."
let makeSpaceMatcher: MakeLexMatcher = (cfg: Config, _opts: AmagamaOptions) =>
  guardedMatcher(cfg.space, function spaceBody(lex) {
    const { pnt, src } = lex
    const bm = cfg.space.charsBitmap
    const chars = cfg.space.chars
    const startSI = pnt.sI
    let sI = startSI
    let cc
    while ((cc = src.charCodeAt(sI)) < 256 ? bm[cc] : chars[src[sI]]) {
      sI++
    }

    if (startSI < sI) {
      const msrc = src.substring(startSI, sI)
      const tkn = lex.token('#SP', undefined, msrc, pnt)
      pnt.sI = sI
      pnt.cI += sI - startSI
      return tkn
    }
  })

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
}
