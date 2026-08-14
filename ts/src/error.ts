/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  error.ts
 *  Error handling functions and classes.
 */

import type {
  Config,
  Context,
  Rule,
  Token,
} from './types'

import { EMPTY, OPEN, STRING } from './types'
import { makeToken, makePoint } from './lexer'
import { assign, deep, entries, keys, escre, tokenize } from './utility'

const S = {
  function: 'function',
  object: 'object',
  string: 'string',
  unexpected: 'unexpected',
  Object: 'Object',
  Array: 'Array',
  gap: '  ',
  no_re_flags: EMPTY,
}


// Structured diagnostic shape emitted by TabnasError.toJSON (and by
// json.Marshal on the Go TabnasError). Documented in
// schema/diagnostic.schema.json; only `code` is contractual across
// runtimes — message/hint/src are informative text.
type TabnasErrorJSON = {
  status: 'failure'
  code: string
  message: string
  hint: string
  row: number
  col: number
  pos: number
  len: number
  rule: string
  ruleStack: string[]
  token: { name: string; src: string }
  expected: string[]
  src: string
  plugins: string[]
  version: string
}


// Tabnas errors with nice formatting.
class TabnasError extends SyntaxError {
  // Assigned by `assign(this, desc)` below; declared (emit-free) so
  // toJSON can read them under strict typing. errdesc has a catch path
  // that returns {} (see below), so both may be undefined.
  declare code?: string
  declare txts?: () => { msg?: string; hint?: string; site?: string }

  // Raw error materials, stashed non-enumerably by the constructor —
  // errdesc's own `internal` is silently dropped today by the
  // `{...Object.create(desc)}` spread (prototype props are not own
  // props), and making it enumerable would change Object.keys and
  // JSON output shape, and would let JSON.stringify recurse into ctx.
  declare internal?: { token: Token; rule: Rule; ctx: Context }

  constructor(
    code: string,
    details: Record<string, any>,
    token: Token,
    rule: Rule,
    ctx: Context,
  ) {
    details = deep({}, details)
    let desc = errdesc(code, details, token, rule, ctx)
    super(desc.message)
    assign(this, desc)
    Object.defineProperty(this, 'internal', {
      value: { token, rule, ctx },
      enumerable: false,
    })
  }

  // Structured diagnostic for JSON.stringify. Mirrors the Go
  // TabnasError.MarshalJSON output (see schema/diagnostic.schema.json).
  // Must never throw: errdesc can fail and return {} (its catch path),
  // leaving code/txts undefined, and plugin code can construct errors
  // with degenerate token/rule/ctx values — every read is guarded and
  // the whole derivation is wrapped, falling back to inert defaults.
  toJSON(): TabnasErrorJSON {
    const out: TabnasErrorJSON = {
      status: 'failure',
      code: STRING === typeof this.code ? (this.code as string) : 'unknown',
      message: EMPTY,
      hint: EMPTY,
      row: 1,
      col: 1,
      pos: 0,
      len: 0,
      rule: EMPTY,
      ruleStack: [],
      token: { name: EMPTY, src: EMPTY },
      expected: [],
      src: EMPTY,
      plugins: [],
      version: EMPTY,
    }

    try {
      const txts = S.function === typeof this.txts ? this.txts!() : undefined

      if (txts && 'string' === typeof txts.msg) {
        out.message = txts.msg
      }

      // The hint is stored display-ready: errdesc indents every line by
      // exactly two spaces. The diagnostic carries the raw text, so
      // strip exactly that indent, then trim.
      if (txts && 'string' === typeof txts.hint) {
        out.hint = txts.hint
          .split('\n')
          .map((s: string) => (S.gap === s.substring(0, 2) ? s.substring(2) : s))
          .join('\n')
          .trim()
      }
    } catch (e) {
      // Leave message/hint defaults.
    }

    try {
      const internal = this.internal
      const token: Token | undefined = internal?.token
      const ctx: Context | undefined = internal?.ctx

      if (null != token) {
        // Clamp unset locations (sentinel tokens carry -1) the same way
        // the Go error constructor does: rows and columns are 1-based
        // and pos is a 0-based offset, so anything below those floors
        // is "unknown", not a position.
        out.row = 1 <= token.rI ? token.rI : 1
        out.col = 1 <= token.cI ? token.cI : 1
        out.pos = 0 <= token.sI ? token.sI : 0

        // token.src is a prototype accessor (lazily materialized), so
        // spreads and stringify miss it — read it explicitly.
        const tsrc = STRING === typeof token.src ? token.src : EMPTY
        // len counts Unicode code points (NOT token.len, which is
        // UTF-16 units) so the counting unit never diverges between
        // runtimes. The token SPAN itself can still differ where the
        // lexers cut bad tokens differently — see DIVERGENCE.md
        // "Bad-token spans for invalid string escapes".
        out.len = Array.from(tsrc).length
        out.token = {
          name: STRING === typeof token.name ? token.name : EMPTY,
          src: tsrc,
        }
      }

      // The failing rule: prefer the raise-site rule; the parser sites
      // that raise with the NORULE sentinel fall back to the context's
      // current rule. If that is also the sentinel, the rule is unknown
      // and the stack carries no tail. Sentinel detection is by IDENTITY
      // first — a rule that IS ctx.NORULE is the sentinel whatever it is
      // named (parserwrap fabricates one named 'no-rule') — with the
      // name-'' test kept as the fallback for a missing NORULE; a
      // degenerate rule without a string name (parserwrap passes
      // `{} as Rule` as the raise-site rule) counts as the sentinel too.
      const named = (r: Rule | undefined): boolean =>
        null != r &&
        r !== ctx?.NORULE &&
        STRING === typeof r.name &&
        EMPTY !== r.name
      let failing: Rule | undefined = internal?.rule
      if (!named(failing)) {
        failing = ctx?.rule
      }
      if (!named(failing)) {
        failing = undefined
      }

      if (null != failing) {
        out.rule = failing.name
      }

      // Rule names root-first, including the failing rule. ctx.rs must
      // be sliced to ctx.rsI — pops move the index without truncating —
      // and sentinel entries are skipped. rsI goes NEGATIVE (-1) once
      // the root rule pops (rules.ts pop: `ctx.rs[--ctx.rsI]`), and a
      // negative slice end counts from the END of the array, which would
      // resurrect stale popped frames — clamp to 0.
      const stack: string[] = []
      if (null != ctx && Array.isArray(ctx.rs) && 'number' === typeof ctx.rsI) {
        for (const r of ctx.rs.slice(0, Math.max(0, ctx.rsI))) {
          if (named(r)) {
            stack.push(r.name)
          }
        }
      }
      if (null != failing) {
        stack.push(failing.name)
      }
      out.ruleStack = stack

      // Token names some alternate of the failing rule names at
      // lookahead position 0 for the current state. Deduplicated and
      // sorted so the two runtimes emit identical arrays (TS tcol
      // insertion order vs Go alt order would otherwise diverge).
      const tcol = failing?.spec?.def?.tcol
      if (null != tcol && null != ctx?.cfg) {
        const tins: number[] = tcol[OPEN === failing!.state ? 0 : 1]?.[0] ?? []
        out.expected = [
          ...new Set(tins.map((tin: number) => '' + tokenize(tin as any, ctx!.cfg))),
        ].sort()
      }

      // The full source LINE containing the error.
      const fullsrc = S.function === typeof ctx?.src ? ctx!.src() : undefined
      if (STRING === typeof fullsrc) {
        const line = (fullsrc as string).split('\n')[out.row - 1]
        if (undefined !== line) {
          out.src = line.endsWith('\r')
            ? line.substring(0, line.length - 1)
            : line
        }
      }

      if (S.function === typeof ctx?.plgn) {
        out.plugins = ctx!.plgn().map((p: any) => '' + p?.name)
      }
    } catch (e) {
      // Leave location/rule/expected defaults.
    }

    try {
      // Lazy read avoids a static import cycle: tabnas.ts imports
      // TabnasError from this module at init; by the time toJSON runs
      // the tabnas module (which owns VERSION) is fully loaded.
      out.version = require('./tabnas').VERSION
    } catch (e) {
      // Leave version default.
    }

    return out
  }
}


// Inject value text into an error message. The value is taken from
// the `details` parameter to TabnasError. If not defined, the value is
// determined heuristically from the Token and Context.
function errinject<T extends string | string[] | { [key: string]: string }>(
  s: T,
  code: string,
  details: Record<string, any>,
  token: Token,
  rule: Rule,
  ctx: Context,
): T {
  let ref: any = {
    ...(ctx || {}),
    ...(ctx.cfg || {}),
    ...(ctx.opts || {}),
    ...(token || {}),
    // Token.src is a prototype accessor (materialized lazily from the
    // token's span), so the spread above does not pick it up — inject
    // it explicitly so {src} in error templates is the token's source
    // text, not the ctx.src function.
    ...(null == token ? {} : { src: token.src }),
    ...(rule || {}),
    ...(ctx.meta || {}),
    ...(details || {}),
    ...{ code, details, token, rule, ctx },
  }
  return strinject(s, ref, { indent: '  ' })
}

// Remove Tabnas internal lines as spurious for caller.
function trimstk(err: Error) {
  if (err.stack) {
    err.stack = err.stack
      .split('\n')
      .filter((s) => !s.includes('tabnas/tabnas'))
      .map((s) => s.replace(/    at /, 'at '))
      .join('\n')
  }
}

// Extract error site in source text and mark error point.
function errsite(spec: {
  src: string
  sub?: string
  msg?: string
  row?: number
  col?: number
  pos?: number
  cline?: string
}) {
  let { src, sub, msg, cline, row, col, pos } = spec
  row = null != row && 0 < row ? row : 1
  col = null != col && 0 < col ? col : 1

  // An empty `cline` means colour is off, and errdesc passes '' (not
  // undefined) in that case — so test for a usable colour rather than for
  // null, or the reset codes are emitted with nothing to reset.
  const tint = cline ? cline : EMPTY
  const untint = cline ? '\x1b[0m' : EMPTY

  pos =
    null != pos && 0 < pos
      ? pos
      : null == src
        ? 0
        : src
          .split('\n')
          .reduce(
            (pos, line, i) => (
              (pos +=
                i < row - 1 ? line.length + 1 : i === row - 1 ? col : 0),
              pos
            ),
            0,
          )

  let tsrc = null == sub ? EMPTY : sub
  let behind = src.substring(Math.max(0, pos - 333), pos).split('\n')
  let ahead = src.substring(pos, pos + 333).split('\n')

  let pad = 2 + (EMPTY + (row + 2)).length
  let rc = row < 3 ? 1 : row - 2
  let ln = (s: string) =>
    tint +
    (EMPTY + rc++).padStart(pad, ' ') +
    ' | ' +
    untint +
    (null == s ? EMPTY : s)

  let blen = behind.length

  let lines = [
    2 < blen ? ln(behind[blen - 3]) : null,
    1 < blen ? ln(behind[blen - 2]) : null,
    ln(behind[blen - 1] + ahead[0]),
    ' '.repeat(pad) +
    '   ' +
    ' '.repeat(col - 1) +
    tint +
    '^'.repeat(tsrc.length || 1) +
    ' ' +
    msg +
    untint,
    ln(ahead[1]),
    ln(ahead[2]),
  ]
    .filter((line: any) => null != line)
    .join('\n')

  return lines
}


function errmsg(spec: {
  code?: string
  name?: string

  txts?: {
    msg?: string
    hint?: string
    site?: string
  }

  smsg?: string
  src?: string
  file?: string
  row?: number
  col?: number
  pos?: number
  site?: string
  sub?: string
  prefix?: string | Function
  suffix?: string | Function
  color?: { active?: boolean, reset?: string; hi?: string; lo?: string; line?: string }
}) {

  const color = {
    active: false,
    reset: '',
    hi: '',
    lo: '',
    line: '',
  }

  if (spec.color && spec.color.active) {
    Object.assign(color, spec.color)
  }

  const txts = {
    msg: null,
    hint: null,
    site: null,
    ...(spec.txts || {})
  }

  let message = [
    null == spec.prefix
      ? null
      : 'function' === typeof spec.prefix
        ? spec.prefix(color, spec)
        : '' + spec.prefix,

    (null == spec.code
      ? ''
      : color.hi +
      '[' +
      (null == spec.name ? '' : spec.name + '/') +
      spec.code +
      ']:') +
    color.reset +
    ' ' +
    // (null == spec.msg ? '' : spec.msg),
    (null == txts.msg ? '' : txts.msg),

    (null != spec.row && null != spec.col) || null != spec.file
      ? '  ' +
      color.line +
      '-->' +
      color.reset +
      ' ' +
      (null == spec.file ? '<no-file>' : spec.file) +
      (null == spec.row || null == spec.col
        ? ''
        : ':' + spec.row + ':' + spec.col)
      : null,

    null == spec.src
      ? ''
      : (null == txts.site ? '' : errsite({
        src: spec.src,
        sub: spec.sub,
        msg: spec.smsg || spec.txts?.msg,
        cline: color.line,
        row: spec.row,
        col: spec.col,
        pos: spec.pos,
      })),

    '',

    // null == spec.hint ? null : spec.hint,
    // txts.hint,
    (null == txts.hint ? '' : txts.hint),

    null == spec.suffix
      ? null
      : 'function' === typeof spec.suffix
        ? spec.suffix(color, spec)
        : '' + spec.suffix,
  ]
    .filter((n) => null != n)
    .join('\n')

  return message
}


function errdesc(
  code: string,
  details: Record<string, any>,
  token: Token,
  rule: Rule,
  ctx: Context,
): Record<string, any> {
  try {
    const src = ctx.src()
    const cfg = ctx.cfg
    const meta = ctx.meta
    const txts = errinject(
      {
        msg:
          cfg.error[code] ||
          (details?.use?.err &&
            (details.use.err.code || details.use.err.message)) ||
          cfg.error.unknown,

        hint: (
          cfg.hint[code] ||
          details.use?.err?.message ||
          cfg.hint.unknown ||
          ''
        )
          .trim()
          .split('\n')
          .map((s: string) => '  ' + s)
          .join('\n'),
        site: '',
      },
      code,
      details,
      token,
      rule,
      ctx,
    )


    txts.site = errsite({
      src,
      msg: txts.msg,
      cline: cfg.color.active ? cfg.color.line : '',
      row: token.rI,
      col: token.cI,
      pos: token.sI,
      sub: token.src,

    })

    const suffix =
      true === cfg.errmsg.suffix ? (color: any) =>
        [
          '',
          // Optional "see also" line (e.g. a docs URL). Purely
          // cosmetic; consumers opt in via `errmsg.link`.
          ...(cfg.errmsg.link
            ? ['  ' + color.lo + cfg.errmsg.link + color.reset]
            : []),
          '  ' +
          color.lo +
          '--internal: tag=' +
          (ctx.opts.tag || '') +
          '; rule=' +
          rule.name +
          '~' +
          rule.state +
          '; token=' +
          tokenize(token.tin, ctx.cfg) +
          (null == token.why ? '' : '~' + token.why) +
          '; plugins=' +
          ctx
            .plgn()
            .map((p: any) => p.name)
            .join(',') +
          '--' +
          color.reset,
        ].join('\n') :
        ('string' === typeof cfg.errmsg.suffix || 'function' === typeof cfg.errmsg.suffix) ?
          cfg.errmsg.suffix :
          undefined

    let message = errmsg({
      code,
      // name: 'tabnas',
      name: cfg.errmsg.name,
      txts,
      src,
      file: meta ? meta.fileName : undefined,
      row: token.rI,
      col: token.cI,
      pos: token.sI,
      sub: token.src,
      color: cfg.color,
      suffix,
    })

    let desc: any = {
      internal: {
        token,
        ctx,
      },
    }

    desc = {
      ...Object.create(desc),
      message,
      code,
      details,
      meta,
      fileName: meta ? meta.fileName : undefined,
      lineNumber: token.rI,
      columnNumber: token.cI,
      txts: () => txts
    }

    return desc
  }
  catch (e) {
    // TODO: fix
    console.log(e)
    return {}
  }
}


// Inject value into text by key using "{key}" syntax.
function strinject<T extends string | string[] | { [key: string]: string }>(
  s: T,
  m: Record<string, any>,
  f?: { indent?: string },
): T {
  let st = typeof s
  let t = Array.isArray(s)
    ? 'array'
    : null == s
      ? 'string'
      : 'object' === st
        ? st
        : 'string'
  let so =
    'object' === t
      ? s
      : 'array' === t
        ? (s as string[]).reduce((a: any, n, i) => ((a[i] = n), a), {})
        : { _: s }
  let mo = null == m ? {} : m

  Object.entries(so).map(
    (n: any[]) =>
    (so[n[0]] =
      null == n[1]
        ? ''
        : ('' + n[1]).replace(
          /\{([\w_0-9.]+)}/g,
          (match: any, keypath: string) => {
            let inject = prop(mo, keypath)
            inject = undefined === inject ? match : inject

            if ('object' === typeof inject) {
              let cn = inject?.constructor?.name
              if ('Object' === cn || 'Array' === cn) {
                inject = JSON.stringify(inject).replace(/([^"])"/g, '$1')
              } else {
                inject = inject.toString()
              }
            } else {
              inject = '' + inject
            }

            if (f) {
              if ('string' === typeof f.indent) {
                inject = inject.replace(/\n/g, '\n' + f.indent)
              }
            }

            return inject
          },
        )),
  )

  return ('string' === t ? so._ : 'array' === t ? Object.values(so) : so) as T
}

function prop(obj: any, path: string, val?: any): any {
  let root = obj
  try {
    let parts = path.split('.')
    let pn: any
    for (let pI = 0; pI < parts.length; pI++) {
      pn = parts[pI]
      if ('__proto__' === pn) {
        throw new Error(pn)
      }
      if (pI < parts.length - 1) {
        obj = obj[pn] = obj[pn] || {}
      }
    }
    if (undefined !== val) {
      if ('__proto__' === pn) {
        throw new Error(pn)
      }
      obj[pn] = val
    }
    return obj[pn]
  } catch (e: any) {
    throw new Error(
      'Cannot ' +
      (undefined === val ? 'get' : 'set') +
      ' path ' +
      path +
      ' on object: ' +
      str(root) +
      (undefined === val ? '' : ' to value: ' + str(val, 22)),
    )
  }
}

function str(o: any, len: number = 44) {
  let s
  try {
    s = 'object' === typeof o ? JSON.stringify(o) : '' + o
  } catch (e: any) {
    s = '' + o
  }
  return snip(len < s.length ? s.substring(0, len - 3) + '...' : s, len)
}

function snip(s: any, len: number = 5) {
  return undefined === s
    ? ''
    : ('' + s).substring(0, len).replace(/[\r\n\t]/g, '.')
}

export type {
  TabnasErrorJSON,
}

export {
  TabnasError,
  errdesc,
  errinject,
  errsite,
  errmsg,
  trimstk,
  strinject,
  prop,
}
