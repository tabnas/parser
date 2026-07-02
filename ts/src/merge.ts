/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  merge.ts
 *  Merge two Tabnas instances into a new instance combining both
 *  grammars. The originals are not modified, and the operation is
 *  commutative: a.merge(b) and b.merge(a) produce instances with the
 *  same options, rule alternates (in the same order), and parse
 *  behavior. See Tabnas.merge (tabnas.ts) for the public entry point.
 */

import type {
  AltSpec,
  Config,
  Tabnas,
  TabnasOptions,
  Tin,
} from './types'

import { defaults } from './defaults'


// A rule alternate made independent of its source instance: token
// entries in `s` are names (not instance-specific Tins), and the
// computed match tables (t/S/sN) are dropped so the merged instance
// re-normalizes in its own Tin space. Sort metadata is carried
// alongside so the interleave comparator never needs the source again.
type PortableAlt = {
  alt: Record<string, any>   // Portable AltSpec clone.
  keys: string[]             // Canonical per-position token-name keys.
  complexity: number[]       // Presence vector, see altComplexity.
  gkey: string               // Joined (pre-sorted) group tags.
  tag: string                // Source instance tag (final tie-break).
}

// Portable form of one rule from one source instance.
type RuleRecord = {
  fnref: Record<string, Function>
  bo: Function[]
  ao: Function[]
  bc: Function[]
  ac: Function[]
  open: PortableAlt[]
  close: PortableAlt[]
}


// Plain-object check for the options walk. Option trees are built by
// deep() from literals (and occasionally Object.create(null)), so both
// Object-constructed and null-prototype objects count.
function isplain(v: any): boolean {
  if (null == v || 'object' !== typeof v || Array.isArray(v)) {
    return false
  }
  const proto = Object.getPrototypeOf(v)
  return null === proto || proto === Object.prototype
}


// Leaf equality for option values: functions by reference, RegExp by
// source+flags, arrays element-wise, plain objects key-wise (for
// object-valued array elements), everything else by Object.is (so NaN
// equals NaN, matching values like result.fail entries).
function leafeq(u: any, v: any): boolean {
  if (Object.is(u, v)) {
    return true
  }
  if (u instanceof RegExp && v instanceof RegExp) {
    return u.source === v.source && u.flags === v.flags
  }
  if (Array.isArray(u) && Array.isArray(v)) {
    return u.length === v.length && u.every((e, i) => leafeq(e, v[i]))
  }
  if (isplain(u) && isplain(v)) {
    const uk = Object.keys(u)
    const vk = Object.keys(v)
    return (
      uk.length === vk.length && uk.every((k) => leafeq(u[k], v[k]))
    )
  }
  return false
}


// Commutative option merge: values present in only one tree, or equal
// in both, merge cleanly; where the trees disagree, the side that
// still equals the shared default loses; both sides non-default and
// unequal is a genuine grammar conflict and throws naming the path.
function mergeOptionTrees(
  av: any,
  bv: any,
  dv: any,
  path: string[],
): any {
  if (undefined === av) {
    return bv
  }
  if (undefined === bv) {
    return av
  }

  if (isplain(av) && isplain(bv)) {
    const out: Record<string, any> = {}
    const bkeys = Object.keys(bv)
    const union = [
      ...Object.keys(av),
      ...bkeys.filter((k) => !(k in av)),
    ]
    for (const k of union) {
      const merged = mergeOptionTrees(
        av[k],
        bv[k],
        isplain(dv) ? dv[k] : undefined,
        [...path, k],
      )
      if (undefined !== merged) {
        out[k] = merged
      }
    }
    return out
  }

  if (leafeq(av, bv)) {
    return av
  }
  if (leafeq(av, dv)) {
    return bv
  }
  if (leafeq(bv, dv)) {
    return av
  }

  throw new Error(
    'merge: conflicting option values at ' + path.join('.'),
  )
}


// Two fixed-token names claiming one source string would make
// configure() silently drop one of them — refuse instead.
function checkFixedTokenSources(options: Record<string, any>): void {
  const token = options.fixed?.token
  if (null == token) {
    return
  }
  const bysrc: Record<string, string> = {}
  for (const name of Object.keys(token)) {
    const src = token[name]
    if (null != bysrc[src] && bysrc[src] !== name) {
      throw new Error(
        'merge: fixed tokens ' + bysrc[src] + ' and ' + name +
        ' both claim source ' + JSON.stringify(src),
      )
    }
    bysrc[src] = name
  }
}


// De-share match.token matcher objects. configure() annotates each
// matcher (RegExp or function) in place with the instance's `tin$`
// (utility.ts) — a matcher object shared between the merged options
// and a source instance would have its annotation overwritten when
// the merged instance configures, silently corrupting the source's
// lexer. RegExps are cloned (keeping the user-set `eager$` opt-out);
// function-form matchers get a delegating wrapper.
function deshareMatchTokens(options: Record<string, any>): void {
  const token = options.match?.token
  if (null == token) {
    return
  }
  const fresh: Record<string, any> = {}
  for (const name of Object.keys(token)) {
    const m = token[name]
    if (m instanceof RegExp) {
      const re: any = new RegExp(m.source, m.flags)
      if ((m as any).eager$) {
        re.eager$ = true
      }
      fresh[name] = re
    } else if ('function' === typeof m) {
      const wrapped: any = (lex: any, rule: any, tI?: number) =>
        m(lex, rule, tI)
      if ((m as any).eager$) {
        wrapped.eager$ = true
      }
      fresh[name] = wrapped
    } else {
      fresh[name] = m
    }
  }
  options.match.token = fresh
}


// Rebuild the lex matcher registry with keys inserted in (order, name)
// order. configure() sorts matchers by `order` with a stable sort, so
// insertion order decides ties — this makes the merged matcher
// sequence deterministic regardless of merge direction.
function orderLexMatch(options: Record<string, any>): void {
  const match = options.lex?.match
  if (null == match) {
    return
  }
  const names = Object.keys(match).sort((a, b) => {
    const oa = match[a]?.order ?? 0
    const ob = match[b]?.order ?? 0
    return oa !== ob ? oa - ob : a < b ? -1 : a > b ? 1 : 0
  })
  const ordered: Record<string, any> = {}
  for (const name of names) {
    ordered[name] = match[name]
  }
  options.lex.match = ordered
}


// Translate one `s` entry from the source Tin space to token names.
// String entries (token or tokenset names) pass through; the merged
// instance re-resolves them at normalization time.
function untin(entry: any, cfg: Config): any {
  const name = (tin: Tin) => {
    const n = (cfg.t as any)[tin]
    if (null == n) {
      throw new Error('merge: unknown token tin: ' + tin)
    }
    return n
  }
  if ('number' === typeof entry) {
    return name(entry as Tin)
  }
  if (Array.isArray(entry)) {
    return entry.map((e) => ('number' === typeof e ? name(e as Tin) : e))
  }
  return entry
}


// Presence vector deciding "complexity" order between alts whose
// token sequences are identical: more complex first, compared
// element-wise in this fixed field order.
function altComplexity(alt: any): number[] {
  return [
    alt.c ? 1 : 0,
    alt.e ? 1 : 0,
    alt.h ? 1 : 0,
    alt.b ? 1 : 0,
    alt.n ? Object.keys(alt.n).length : 0,
    alt.a ? 1 : 0,
    alt.u ? 1 : 0,
    alt.k ? 1 : 0,
    alt.p ? 1 : 0,
    alt.r ? 1 : 0,
  ]
}


// Build the portable form of a normalized alt. The canonical
// per-position key is the sorted, space-joined token-name expansion of
// that position (uniform over single tokens, tokensets, and Tin-array
// subsets), computed from the source-resolved alt.t tables.
function portable(alt: any, cfg: Config, tag: string): PortableAlt {
  const t: Tin[][] = alt.t || []
  const keys = t.map((tins) =>
    tins
      .map((tin) => (cfg.t as any)[tin])
      .sort()
      .join(' '),
  )

  const clone: Record<string, any> = { ...alt }
  delete clone.t
  delete clone.S
  delete clone.sN

  clone.s =
    alt.s && alt.s.length
      ? alt.s.map((entry: any) => untin(entry, cfg))
      : null
  clone.g = [...(alt.g || [])]
  if (alt.n) {
    clone.n = { ...alt.n }
  }
  if (alt.u) {
    clone.u = { ...alt.u }
  }
  if (alt.k) {
    clone.k = { ...alt.k }
  }

  return {
    alt: clone,
    keys,
    complexity: altComplexity(alt),
    gkey: (alt.g || []).join(','),
    tag,
  }
}


// The interleave comparator (<0 means a first):
// 1. first differing position: lexicographic token-name order;
// 2. one sequence a prefix of the other: longer first (so empty-s
//    catch-all alts sort last);
// 3. identical sequences: more complex first, then group tags;
// 4. finally the source tag — never equal across the two parsers, so
//    the order is total and independent of merge direction.
function compareAlts(a: PortableAlt, b: PortableAlt): number {
  const n = Math.min(a.keys.length, b.keys.length)
  for (let i = 0; i < n; i++) {
    if (a.keys[i] !== b.keys[i]) {
      return a.keys[i] < b.keys[i] ? -1 : 1
    }
  }
  if (a.keys.length !== b.keys.length) {
    return b.keys.length - a.keys.length
  }
  for (let i = 0; i < a.complexity.length; i++) {
    const d = b.complexity[i] - a.complexity[i]
    if (0 !== d) {
      return d
    }
  }
  if (a.gkey !== b.gkey) {
    return a.gkey < b.gkey ? -1 : 1
  }
  return a.tag < b.tag ? -1 : a.tag > b.tag ? 1 : 0
}


// Function-field equality for dedupe: reference identity, or — since
// every plugin run creates fresh closures, so two instances that
// installed the same plugin never share references — identical source
// text. Source equality is blind to captured environments, which is
// why the callers below restrict it to cases where that cannot change
// parse behavior.
function fneq(u: any, v: any): boolean {
  if (u === v) {
    return true
  }
  return (
    'function' === typeof u &&
    'function' === typeof v &&
    u.toString() === v.toString()
  )
}


// Identical alts (same token keys and group tags, same behavior
// fields, same data props by value) are emitted once — the
// shared-base-plugin case, where both instances installed the same
// grammar plugin. Fields compare by reference or by function source;
// for the source path the alts must be unconditioned (or share one
// condition reference): with no condition the first of two
// identical-sequence alts always wins the match and the second is
// unreachable, so the dedupe cannot change behavior even if the
// source-equal closures captured different environments.
function identicalAlts(a: PortableAlt, b: PortableAlt): boolean {
  if (
    a.keys.length !== b.keys.length ||
    !a.keys.every((k, i) => k === b.keys[i]) ||
    a.gkey !== b.gkey
  ) {
    return false
  }
  if (a.alt.c !== b.alt.c && (a.alt.c || b.alt.c)) {
    return false
  }
  return (
    fneq(a.alt.a, b.alt.a) &&
    fneq(a.alt.h, b.alt.h) &&
    fneq(a.alt.e, b.alt.e) &&
    (a.alt.b === b.alt.b || fneq(a.alt.b, b.alt.b)) &&
    (a.alt.p === b.alt.p || fneq(a.alt.p, b.alt.p)) &&
    (a.alt.r === b.alt.r || fneq(a.alt.r, b.alt.r)) &&
    leafeq(a.alt.n, b.alt.n) &&
    leafeq(a.alt.u, b.alt.u) &&
    leafeq(a.alt.k, b.alt.k)
  )
}


// Standard two-pointer merge: each source list stays internally
// stable (original alt order is preserved relative to itself), the
// comparator only decides interleaving across the two lists. Dedupe
// runs first, position-independently: an alt of y identical to ANY
// alt of x is dropped (x is the canonical smaller-tag side, so the
// outcome is direction-independent) — head-only comparison would miss
// shared-base alts shifted by each side's own additions.
function interleave(
  xs: PortableAlt[],
  ys: PortableAlt[],
): PortableAlt[] {
  const yy = ys.filter((y) => !xs.some((x) => identicalAlts(x, y)))
  const out: PortableAlt[] = []
  let i = 0
  let j = 0
  while (i < xs.length && j < yy.length) {
    if (compareAlts(xs[i], yy[j]) <= 0) {
      out.push(xs[i++])
    } else {
      out.push(yy[j++])
    }
  }
  while (i < xs.length) {
    out.push(xs[i++])
  }
  while (j < yy.length) {
    out.push(yy[j++])
  }
  return out
}


// Extract the portable rule records of one instance. fnref keys are
// renamed '@x' -> '@<tag>:x' so the two parsers' named actions cannot
// collide; the '<tag>:' infix can never match the reserved
// '@<rulename>-<phase>' lifecycle pattern, so renamed entries never
// auto-install (the already-installed bo/ao/bc/ac arrays are carried
// verbatim instead). '$'-refs are engine builtins — identical
// references on both sides — and stay unprefixed.
function ruleRecords(
  tn: Tabnas,
  tag: string,
): Record<string, RuleRecord> {
  const internal = tn.internal()
  const cfg = internal.config
  const rsm: Record<string, any> = internal.parser.rule() as any

  const records: Record<string, RuleRecord> = {}
  for (const name of Object.keys(rsm)) {
    const def = rsm[name].def
    const fnref: Record<string, Function> = {}
    for (const key of Object.keys(def.fnref)) {
      if (key.includes('$')) {
        fnref[key] = def.fnref[key]
      } else {
        fnref['@' + tag + ':' + key.substring(1)] = def.fnref[key]
      }
    }
    records[name] = {
      fnref,
      bo: [...def.bo],
      ao: [...def.ao],
      bc: [...def.bc],
      ac: [...def.ac],
      open: def.open.map((alt: AltSpec) => portable(alt, cfg, tag)),
      close: def.close.map((alt: AltSpec) => portable(alt, cfg, tag)),
    }
  }
  return records
}


// Concatenate lifecycle actions in canonical (tag) order, deduping by
// function identity or source text so a handler contributed by a
// plugin both instances installed (fresh closures per plugin run, same
// source) installs once — otherwise e.g. a shared base grammar's
// element handler would append every element twice. Source equality
// cannot see captured environments; a handler whose behavior differs
// only via its closure environment dedupes to the smaller-tag side's
// copy (documented).
function concatActions(xs: Function[], ys: Function[]): Function[] {
  const out = [...xs]
  for (const fn of ys) {
    if (!out.some((existing) => fneq(existing, fn))) {
      out.push(fn)
    }
  }
  return out
}


// Fresh alt clone per plugin invocation: normalt mutates alts in
// place (match tables, sorted g), and the synthetic plugin re-runs on
// make()-derived children, which must not share alt objects.
function cloneAlt(alt: Record<string, any>): AltSpec {
  const out: Record<string, any> = { ...alt }
  out.s = Array.isArray(alt.s) ? [...alt.s] : alt.s
  out.g = [...(alt.g || [])]
  if (alt.n) {
    out.n = { ...alt.n }
  }
  if (alt.u) {
    out.u = { ...alt.u }
  }
  if (alt.k) {
    out.k = { ...alt.k }
  }
  return out as AltSpec
}


// Merge two instances into a new one. `makeInstance` is injected by
// tabnas.ts (avoids an import cycle) and builds a fresh instance from
// the merged options — deliberately not make() from either side, so
// the result cannot depend on argument order.
function mergeInstances(
  a: Tabnas,
  b: Tabnas,
  makeInstance: (options: TabnasOptions) => Tabnas,
): Tabnas {
  const tagOf = (tn: Tabnas, which: string): string => {
    const tag = tn.internal().merged.tag
    if (null == tag || '' === tag || '-' === tag) {
      throw new Error(
        'merge: the ' + which + ' instance needs a tag option ' +
        '(used to prefix its named actions)',
      )
    }
    return String(tag)
  }

  const tagA = tagOf(a, 'first')
  const tagB = tagOf(b, 'second')
  if (tagA === tagB) {
    throw new Error(
      'merge: instance tags must differ, both are ' +
      JSON.stringify(tagA),
    )
  }

  // Canonical order by tag: everything below is independent of which
  // instance was the merge receiver.
  const [x, y] = tagA < tagB ? [a, b] : [b, a]
  const [tagX, tagY] = tagA < tagB ? [tagA, tagB] : [tagB, tagA]

  // `tag` is computed for the result and `plugins` is handled by the
  // synthetic plugin below — neither takes part in the conflict walk.
  const strip = (tree: Record<string, any>) => {
    const { tag, plugins, ...rest } = tree
    return rest
  }
  const mergedOptions: Record<string, any> = mergeOptionTrees(
    strip(x.internal().merged),
    strip(y.internal().merged),
    defaults,
    [],
  )
  mergedOptions.tag = tagX + '~' + tagY

  checkFixedTokenSources(mergedOptions)
  deshareMatchTokens(mergedOptions)
  orderLexMatch(mergedOptions)

  const xRecords = ruleRecords(x, tagX)
  const yRecords = ruleRecords(y, tagY)
  const ruleNames = [
    ...Object.keys(xRecords),
    ...Object.keys(yRecords).filter((n) => !(n in xRecords)),
  ].sort()

  const empty: RuleRecord = {
    fnref: {}, bo: [], ao: [], bc: [], ac: [], open: [], close: [],
  }
  const records: Record<string, RuleRecord> = {}
  for (const name of ruleNames) {
    const xr = xRecords[name] || empty
    const yr = yRecords[name] || empty
    records[name] = {
      fnref: { ...xr.fnref, ...yr.fnref },
      bo: concatActions(xr.bo, yr.bo),
      ao: concatActions(xr.ao, yr.ao),
      bc: concatActions(xr.bc, yr.bc),
      ac: concatActions(xr.ac, yr.ac),
      open: interleave(xr.open, yr.open),
      close: interleave(xr.close, yr.close),
    }
  }

  // Synthetic plugin carrying the merged grammar. Installing rules
  // via a plugin (rather than directly) keeps make() derivation
  // working: children rebuild their rules by re-running plugins.
  // fnref entries are assigned directly (not via rs.fnref()) so the
  // renamed keys never trigger lifecycle auto-install; the lifecycle
  // arrays carry the already-installed handlers.
  const mergedGrammar = function (tn: Tabnas) {
    for (const name of ruleNames) {
      const rec = records[name]
      tn.rule(name, (rs: any) => {
        Object.assign(rs.def.fnref, rec.fnref)
        rs.def.bo.push(...rec.bo)
        rs.def.ao.push(...rec.ao)
        rs.def.bc.push(...rec.bc)
        rs.def.ac.push(...rec.ac)
        if (0 < rec.open.length) {
          rs.open(rec.open.map((p) => cloneAlt(p.alt)), { append: true })
        }
        if (0 < rec.close.length) {
          rs.close(rec.close.map((p) => cloneAlt(p.alt)), { append: true })
        }
      })
    }
  }
  Object.defineProperty(mergedGrammar, 'name', { value: 'merged' })

  const out = makeInstance(mergedOptions as TabnasOptions)
  out.use(mergedGrammar as any)

  // Event subscribers carry over too, in canonical order.
  const sub = out.internal().sub
  const xSub = x.internal().sub
  const ySub = y.internal().sub
  for (const kind of ['lex', 'rule'] as const) {
    const combined = [...(xSub[kind] || []), ...(ySub[kind] || [])]
    if (0 < combined.length) {
      sub[kind] = [...(sub[kind] || []), ...combined] as any
    }
  }

  return out
}


export { mergeInstances }
