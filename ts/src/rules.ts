/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  rules.ts
 *  Parser rules.
 */

import type {
  AltAction,
  AltCond,
  AltModifier,
  AltSpec,
  AltSpecish,
  Config,
  Context,
  Counters,
  FuncRef,
  FuncRefMap,
  Tabnas,
  Lex,
  ListMods,
  NormAltCond,
  NormAltSpec,
  RuleState,
  RuleStep,
  StateAction,
  Tin,
  Token,
} from './types'

import { OPEN, CLOSE, BEFORE, AFTER, EMPTY, STRING } from './types'

import {
  S,
  defprop,
  getpath,
  isarr,
  modlist,
  tokenize,
} from './utility'

import { TabnasError } from './error'

// A single application of a RuleSpec on the parser stack; lives through an "open" then "close" pass.
class Rule {
  i = -1                                  // Unique rule id within this parse run.
  name = EMPTY                            // Rule name (matches its RuleSpec).
  node: any = null                        // Value node this rule is building.
  state: RuleState = OPEN                 // Current phase: open ('o') or close ('c').
  d = -1                                  // Stack depth at which this rule was pushed.
  bo = false                              // Has before-open actions.
  ao = false                              // Has after-open actions.
  bc = false                              // Has before-close actions.
  ac = false                              // Has after-close actions.

  oN = 0                                  // Count of tokens matched in the open phase.
  cN = 0                                  // Count of tokens matched in the close phase.

  spec: RuleSpec                          // The RuleSpec this rule applies.
  child: Rule                             // Rule pushed by this rule (NORULE if none).
  parent: Rule                            // Rule that pushed this rule (NORULE if none).
  prev: Rule                              // Rule this one replaced (NORULE if none).
  next: Rule                              // Rule to process after this one.

  // Canonical storage for matched tokens at each lookahead position.
  o: Token[]                              // Tokens matched in the open phase.
  c: Token[]                              // Tokens matched in the close phase.

  // Per-rule NOTOKEN reference (from ctx), used by legacy accessors.
  // Optional so the structural type stays compatible with consumers
  // that don't know about it.
  _NOTOKEN?: Token

  need = 0                                // Reserved counter for grammar/plugin use.

  // Counter (n), user-prop (u), and keep-prop (k) objects are created
  // lazily on first access — most rules in value-building grammars
  // never touch them, and three per-rule allocations were measurable
  // GC pressure. The public r.n / r.u / r.k API is unchanged (the
  // accessors materialize on demand); engine hot paths read the
  // non-materializing rawn()/rawu()/rawk() views instead.
  #n?: Counters
  #u?: Record<string, any>
  #k?: Record<string, any>

  get n(): Counters { return (this.#n ??= Object.create(null)) }
  set n(v: Counters) { this.#n = v }
  get u(): Record<string, any> { return (this.#u ??= Object.create(null)) }
  set u(v: Record<string, any>) { this.#u = v }
  get k(): Record<string, any> { return (this.#k ??= Object.create(null)) }
  set k(v: Record<string, any>) { this.#k = v }

  // Non-materializing views: undefined until the map has been created.
  rawn(): Counters | undefined { return this.#n }
  rawu(): Record<string, any> | undefined { return this.#u }
  rawk(): Record<string, any> | undefined { return this.#k }

  // Internal tracing field — set by the parser when a rule fails.
  why?: string

  constructor(spec: RuleSpec, ctx: Context, node?: any) {
    this.i = ctx.uI++ // Rule ids are unique only to the parse run.
    this.name = spec.name
    this.spec = spec

    this.child = ctx.NORULE
    this.parent = ctx.NORULE
    this.prev = ctx.NORULE
    this.next = ctx.NORULE

    this._NOTOKEN = ctx.NOTOKEN
    this.o = []
    this.c = []

    this.node = node
    this.d = ctx.rsI
    this.bo = null != spec.def.bo
    this.ao = null != spec.def.ao
    this.bc = null != spec.def.bc
    this.ac = null != spec.def.ac
  }

  // Legacy aliases for o[0], o[1], c[0], c[1] and the count fields.
  // Maintained so existing grammar/plugin code that reads r.o0/r.o1/r.os
  // (and r.c0/r.c1/r.cs) continues to work unchanged.
  get o0(): Token { return this.o[0] ?? (this._NOTOKEN as Token) }
  set o0(v: Token) { this.o[0] = v }
  get o1(): Token { return this.o[1] ?? (this._NOTOKEN as Token) }
  set o1(v: Token) { this.o[1] = v }
  get c0(): Token { return this.c[0] ?? (this._NOTOKEN as Token) }
  set c0(v: Token) { this.c[0] = v }
  get c1(): Token { return this.c[1] ?? (this._NOTOKEN as Token) }
  set c1(v: Token) { this.c[1] = v }
  get os(): number { return this.oN }
  set os(v: number) { this.oN = v }
  get cs(): number { return this.cN }
  set cs(v: number) { this.cN = v }

  process(ctx: Context, lex: Lex): Rule {
    let rule = this.spec.process(this, ctx, lex, this.state)
    return rule
  }

  // An unset counter reads as 0: a counter that has never been incremented
  // has counted nothing. Previously every helper short-circuited to `true`
  // when the counter was unset, which made `lt(k,n)` and `gt(k,n)` BOTH true
  // — a predicate and its own negation. That broke trichotomy (exactly one of
  // <, =, > must hold) and made the natural "refuse when past the limit"
  // guard fire on the very first token, before anything had been counted.
  // Reading unset as 0 keeps the permissive direction intact (`lt(k,n)` is
  // still true when unset, for n>0) while making the strict direction honest.
  // To ask whether a counter was set at all, use `exist()`.

  eq(counter: string, limit: number = 0): boolean {
    return (this.#n?.[counter] ?? 0) === limit
  }

  lt(counter: string, limit: number = 0): boolean {
    return (this.#n?.[counter] ?? 0) < limit
  }

  gt(counter: string, limit: number = 0): boolean {
    return (this.#n?.[counter] ?? 0) > limit
  }

  lte(counter: string, limit: number = 0): boolean {
    return (this.#n?.[counter] ?? 0) <= limit
  }

  gte(counter: string, limit: number = 0): boolean {
    return (this.#n?.[counter] ?? 0) >= limit
  }

  /** Has this counter been set at all? The comparison helpers read an unset
   * counter as 0, so they cannot distinguish "never counted" from "counted
   * zero"; this can. (Declarative equivalent: `{ 'n.k': { $exist: true } }`.) */
  exist(counter: string): boolean {
    return null != this.#n?.[counter]
  }

  toString() {
    return '[Rule ' + this.name + '~' + this.i + ']'
  }
}

const makeRule = (...params: ConstructorParameters<typeof Rule>) =>
  new Rule(...params)

const makeNoRule = (j: Tabnas, ctx: Context) => makeRule(makeRuleSpec(j, ctx.cfg, {}), ctx)

// Result of matching one parse alternate against the current tokens (built from current tokens and AltSpec).
class AltMatch {
  p: string | null | false | 0 = EMPTY  // Push rule (by name).
  r: string | null | false | 0 = EMPTY  // Replace rule (by name).
  b: number | null | false = 0          // Backtrack: move token position backward.
  c?: AltCond                           // Custom alt match condition.
  n?: Counters                          // Named counters to increment.
  a?: AltAction                         // Action to run on match.
  h?: AltModifier                       // Modifier for this alternate match.
  u?: Record<string, any>               // Custom props to add to Rule.u.
  k?: Record<string, any>               // Custom props to add to Rule.k (propagated via push/replace).
  g?: string[]                          // Named group tags (lets plugins find alts).
  e?: Token                             // Token the match errored on.
}

const makeAltMatch = (...params: ConstructorParameters<typeof AltMatch>) =>
  new AltMatch(...params)

const EMPTY_ALT = makeAltMatch()

// Reusable definition of a rule: its open/close alternates, lifecycle actions, and collated token lookups.
class RuleSpec {
  name = EMPTY                          // Rule name (set by Parser.rule).
  def = {
    open: [] as AltSpec[],              // Open-phase alternates.
    close: [] as AltSpec[],             // Close-phase alternates.
    bo: [] as StateAction[],            // Before-open actions.
    bc: [] as StateAction[],            // Before-close actions.
    ao: [] as StateAction[],            // After-open actions.
    ac: [] as StateAction[],            // After-close actions.
    tcol: [] as Tin[][][],              // Collated lookahead tins: [stateI][tokenI][tins].
    fnref: {} as FuncRefMap<Function>,  // Named function references (@name handlers).
  }
  cfg: Config                           // Resolved configuration.
  ji: Tabnas                            // Owning Tabnas instance.


  constructor(j: Tabnas, cfg: Config, def: any) {
    this.ji = j
    this.cfg = cfg
    this.def = Object.assign(this.def, def)

    // Null Alt entries are allowed and ignored as a convenience.
    this.def.open = (this.def.open || []).filter((alt: AltSpec) => null != alt)
    this.def.close = (this.def.close || []).filter(
      (alt: AltSpec) => null != alt,
    )

    for (let alt of this.def.open) {
      normalt(alt, OPEN, this)
    }

    for (let alt of this.def.close) {
      normalt(alt, CLOSE, this)
    }

    const anames = ['bo', 'ao', 'bc', 'ac']
    for (let an of anames) {
      for (let sa of ((this.def as any)[an] ?? [])) {
        if ('object' === typeof sa) {
          let sadef = sa as any
          (this as any)[an](sadef.append, sadef.action)
        }
      }
    }
  }

  // Convenience access to token Tins
  tin<R extends string | Tin, T extends R extends Tin ? string : Tin>(
    ref: R,
  ): T {
    return tokenize(ref, this.cfg)
  }


  fnref(frm: Record<FuncRef, Function>): RuleSpec {
    Object.assign(this.def.fnref, frm)

    // Auto-install reserved `@<rulename>-<phase>` handlers as state
    // actions. Dedupe by function identity per phase: registering the
    // same function twice (directly or via a later fnref() call that
    // also passes it) installs only one action, but distinct functions
    // for the same phase all install. This accommodates grammars that
    // layer handlers (e.g. core sets a basic @list-bo, then a richer
    // one replaces/augments it) while preventing the old bug where
    // iterating the accumulated fnref map re-installed every
    // previously-registered handler on every call.
    const rn = this.name
    const fr: any = this.def.fnref
    const installed: Map<string, WeakSet<Function>> =
      ((this.def as any).fnrefInstalled =
        (this.def as any).fnrefInstalled || new Map())
    // Phases an `@<rule>-<phase>/replace` fnref has taken ownership of.
    // Once a phase is replaced, the plain/prepend/append fnrefs for it are
    // ignored so older handlers (still lingering in the accumulated fnref
    // map) are not re-installed on subsequent fnref() calls or re-derivation.
    const replaced: Set<string> =
      ((this.def as any).fnrefReplaced =
        (this.def as any).fnrefReplaced || new Set())

    const reserved = [`@${rn}-bo`, `@${rn}-ao`, `@${rn}-bc`, `@${rn}-ac`]
    for (let base of reserved) {
      let phaseSet = installed.get(base)
      if (!phaseSet) installed.set(base, phaseSet = new WeakSet())

      const aname = base.replace(/^[^-]+-/, '')

      // `/replace` clears all prior actions for this phase (from any
      // plugin) and installs the replacement, then owns the phase.
      const replaceFn = fr[base + '/replace']
      if (replaceFn) {
        if (!replaced.has(base)) {
          replaced.add(base)
          ;(this.def as any)[aname].length = 0
          phaseSet = new WeakSet()
          installed.set(base, phaseSet)
          phaseSet.add(replaceFn)
          ;(this as any)[aname](true, replaceFn)
        }
        continue // phase owned by replace: skip prepend/append/plain
      }
      if (replaced.has(base)) continue

      const prependFn = fr[base + '/prepend']
      const appendFn = fr[base + '/append'] ?? fr[base]

      if (prependFn && !phaseSet.has(prependFn)) {
        phaseSet.add(prependFn)
          ; (this as any)[aname](false, prependFn)
      }
      if (appendFn && !phaseSet.has(appendFn)) {
        phaseSet.add(appendFn)
          ; (this as any)[aname](true, appendFn)
      }
    }

    return this
  }


  add(rs: RuleState, a: AltSpec | AltSpecish[], mods?: ListMods): RuleSpec {
    let inject = mods?.append ? 'push' : 'unshift'
    let aa = ((isarr(a) ? a : [a]) as AltSpec[])
      .filter((alt: AltSpec) => null != alt && 'object' === typeof alt)
      .map((a) => normalt(a, rs, this))
    let altState: 'open' | 'close' = 'o' === rs ? 'open' : 'close'
    let alts: any = this.def[altState]

    // `clear` empties the pre-existing alternates (from earlier plugins)
    // before the new ones are injected — a later plugin can thus replace
    // a rule's open/close alternates outright. Done before inject so the
    // new alternates survive.
    if (mods?.clear) {
      alts.length = 0
    }

    alts[inject](...aa)

    alts = this.def[altState] = modlist(alts, mods)

    // NOTE: an earlier version called filterRules(this, this.cfg) here
    // and discarded the result — filterRules clones the whole def, so
    // that was pure dead work on every open()/close() call. Rule
    // include/exclude filtering happens at clone/derive time
    // (Parser.clone), not during registration.

    this.norm()

    return this
  }


  open(a: AltSpec | AltSpecish[], mods?: ListMods): RuleSpec {
    return this.add('o', a, mods)
  }

  close(a: AltSpec | AltSpecish[], mods?: ListMods): RuleSpec {
    return this.add('c', a, mods)
  }

  action(
    append: boolean,
    step: RuleStep,
    state: RuleState,
    action: StateAction,
  ): RuleSpec {
    let actions = (this.def as any)[step + state]
    if (append) {
      actions.push(action)
    } else {
      actions.unshift(action)
    }
    return this
  }

  bo(append: StateAction | boolean | FuncRef, action?: StateAction): RuleSpec {
    return this.action(
      action ? !!append : true,
      BEFORE,
      OPEN,
      'string' === typeof append ? this.def.fnref[append as FuncRef] as StateAction :
        (action ?? (append as StateAction)),
    )
  }

  ao(append: StateAction | boolean, action?: StateAction): RuleSpec {
    return this.action(
      action ? !!append : true,
      AFTER,
      OPEN,
      'string' === typeof append ? this.def.fnref[append as FuncRef] as StateAction :
        (action ?? (append as StateAction)),
    )
  }

  bc(append: StateAction | boolean, action?: StateAction): RuleSpec {
    return this.action(
      action ? !!append : true,
      BEFORE,
      CLOSE,
      'string' === typeof append ? this.def.fnref[append as FuncRef] as StateAction :
        (action ?? (append as StateAction)),
    )
  }

  ac(append: StateAction | boolean, action?: StateAction): RuleSpec {
    return this.action(
      action ? !!append : true,
      AFTER,
      CLOSE,
      'string' === typeof append ? this.def.fnref[append as FuncRef] as StateAction :
        (action ?? (append as StateAction)),
    )
  }

  clear() {
    this.def.open.length = 0
    this.def.close.length = 0
    this.def.bo.length = 0
    this.def.ao.length = 0
    this.def.bc.length = 0
    this.def.ac.length = 0
    return this
  }

  // Remove this rule's open alternates without touching close or the
  // lifecycle actions. A later plugin can call this, then re-add, to
  // replace the open alternates contributed by earlier plugins.
  clearOpen() {
    this.def.open.length = 0
    return this
  }

  // Remove this rule's close alternates (see clearOpen).
  clearClose() {
    this.def.close.length = 0
    return this
  }

  // Remove the registered lifecycle actions for the named phases (any of
  // 'bo', 'ao', 'bc', 'ac'); with no arguments, clear all four. The
  // fnref dedup/replace bookkeeping for those phases is reset too, so a
  // subsequent fnref() re-installs cleanly. Alternates are untouched.
  clearActions(...phases: ('bo' | 'ao' | 'bc' | 'ac')[]) {
    const all = (0 < phases.length ? phases : ['bo', 'ao', 'bc', 'ac']) as (
      'bo' | 'ao' | 'bc' | 'ac'
    )[]
    const installed: Map<string, WeakSet<Function>> | undefined = (this.def as any)
      .fnrefInstalled
    const replaced: Set<string> | undefined = (this.def as any).fnrefReplaced
    for (const p of all) {
      ;(this.def as any)[p].length = 0
      const base = `@${this.name}-${p}`
      installed && installed.delete(base)
      replaced && replaced.delete(base)
    }
    return this
  }

  norm() {
    this.def.open.map((alt) => normalt(alt, OPEN, this))
    this.def.close.map((alt) => normalt(alt, CLOSE, this))

    // [stateI is o=0,c=1][tokenI is 0..maxS-1][tins]
    const columns: Tin[][][] = []

    // Compute max lookahead depth declared across this rule's alts,
    // per state. Generalizes the previous hard-coded 2-slot collation.
    const maxS = (alts: any[]): number =>
      alts.reduce((m: number, a: any) => Math.max(m, a.sN || 0), 0)
    const maxOpen = maxS(this.def.open)
    const maxClose = maxS(this.def.close)

    for (let tI = 0; tI < maxOpen; tI++) {
      this.def.open.reduce(...collate(0, tI, columns))
    }
    for (let tI = 0; tI < maxClose; tI++) {
      this.def.close.reduce(...collate(1, tI, columns))
    }

    // Ensure tcol[stateI] exists with enough slots so the lexer's
    // tcol gating can always index `tcol[oc][tI]` safely for any tI
    // the parser passes (bounded by this rule's own maxS).
    columns[0] = columns[0] || []
    columns[1] = columns[1] || []
    for (let tI = 0; tI < maxOpen; tI++) columns[0][tI] = columns[0][tI] || []
    for (let tI = 0; tI < maxClose; tI++) columns[1][tI] = columns[1][tI] || []

    this.def.tcol = columns

    function collate(
      stateI: number,
      tokenI: number,
      columns: Tin[][][],
    ): [any, any] {
      columns[stateI] = columns[stateI] || []
      let tins = (columns[stateI][tokenI] = columns[stateI][tokenI] || [])

      return [
        function(tins: any, alt: any) {
          let resolved = alt.t && alt.t[tokenI]
          if (resolved && 0 < resolved.length) {
            let newtins = [...new Set(tins.concat(resolved))]
            tins.length = 0
            tins.push(...newtins)
          }
          return tins
        },
        tins,
      ]
    }

    return this
  }


  process(rule: Rule, ctx: Context, lex: Lex, state: RuleState): Rule {
    ctx.log && ctx.log(S.rule, ctx, rule, lex)

    let is_open = state === 'o'
    let next = is_open ? rule : ctx.NORULE
    let why = is_open ? 'O' : 'C'
    let def = this.def

    // The `why` trace string is only appended to when logging is active:
    // the concatenations otherwise allocate rope fragments on every step
    // that nothing reads (error formatting uses token.why, not rule.why,
    // and the Go runtime never builds a per-step why at all).
    const logging = null != ctx.log

    // Match alternates for current state.
    let alts = (is_open ? def.open : def.close) as NormAltSpec[]

    // Handle "before" call.
    let befores = is_open ? (rule.bo ? def.bo : null) : rule.bc ? def.bc : null
    if (befores) {
      let bout: Token | void = undefined
      for (let bI = 0; bI < befores.length; bI++) {
        bout = befores[bI].call(this, rule, ctx, next, bout)
        if (bout?.isToken && bout?.err) {
          return this.bad(bout, rule, ctx, { is_open })
        }
      }
    }

    // Attempt to match one of the alts.
    let alt: AltMatch =
      0 < alts.length ? parse_alts(is_open, alts, lex, rule, ctx) : EMPTY_ALT

    // Expose the alternate this pass resolved to, for the parser's
    // post-process ruleDone event (a pointer store; snapshotting is
    // done only when a subscriber exists).
    ;(ctx as any)._dalt = alt

    // Custom alt handler.
    if (alt.h) {
      alt = alt.h(rule, ctx, alt, next) || alt
      if (logging) why += 'H'
    }

    // Unconditional error.
    if (alt.e) {
      return this.bad(alt.e, rule, ctx, { is_open })
    }

    // Update counters.
    if (alt.n) {
      const rn = rule.n
      for (let cn in alt.n) {
        rn[cn] =
          // 0 reverts counter to 0.
          0 === alt.n[cn]
            ? 0
            : // First seen, set to 0.
            (null == rn[cn]
              ? 0
              : // Increment counter.
              rn[cn]) + alt.n[cn]
      }
    }

    // Set custom properties
    if (alt.u) {
      rule.u = Object.assign(rule.u, alt.u)
    }
    if (alt.k) {
      rule.k = Object.assign(rule.k, alt.k)
    }

    // Record consumed tokens (matched minus backtrack) on the v
    // history BEFORE running alt actions, so an action that calls
    // ctx.rewind sees the just-matched tokens on top of the stack.
    // The lookahead-buffer shift itself still happens at the end of
    // process() so non-action paths behave identically.
    //
    // ctx.vAbs is an absolute monotonic counter used as the mark
    // value — it's decoupled from ctx.v.length so the ring-buffer
    // cap can evict old tokens from the front without invalidating
    // outstanding marks (marks older than the retained window will
    // simply fail at rewind time with a clear error).
    const _cons = rule[is_open ? 'oN' : 'cN'] - (alt.b || 0)
    if (0 < _cons) {
      // Move consumed tokens from ctx.t → ctx.v. Clear the tbuf slots
      // so a ctx.rewind call inside the subsequent alt action can
      // distinguish "token already in v" (NOTOKEN here; will be
      // replayed from v) from "pre-lexed lookahead past consumed"
      // (real token in tbuf; needs re-queuing to preserve state).
      const NOTOKEN = ctx.NOTOKEN
      for (let i = 0; i < _cons; i++) {
        ctx.v.push(ctx.t[i])
        ctx.t[i] = NOTOKEN
      }
      ;(ctx as any).vAbs += _cons
      // Amortised-O(1) ring-buffer cap: let v grow to twice the
      // capacity, then splice its front back down. Batch-eviction
      // makes each push O(1) on average even at the cap.
      const cap = ctx.cfg.rewind.history
      if (cap !== Infinity && ctx.v.length > 2 * cap) {
        ctx.v.splice(0, ctx.v.length - cap)
      }
    }

    // TODO: move after rule.next resolution
    // (breaks Expr! - fix first)
    // Action call.
    if (alt.a) {
      if (logging) why += 'A'
      let tout = alt.a(rule, ctx, alt)
      if (tout && tout.isToken && tout.err) {
        return this.bad(tout, rule, ctx, { is_open })
      }
    }

    // Push a new rule onto the stack...
    if (alt.p) {
      ctx.rs[ctx.rsI++] = rule
      let rulespec = ctx.rsm[alt.p]
      if (rulespec) {
        next = rule.child = makeRule(rulespec, ctx, rule.node)
        next.parent = rule
        // Copy counters/keeps through the non-materializing views: a
        // parent that never touched them costs the child nothing, and
        // the child's object is created only when there is content.
        const pn = rule.rawn()
        if (undefined !== pn) {
          let nn: Counters | undefined = undefined
          for (let cn in pn) (nn ??= next.n)[cn] = pn[cn]
        }
        const pk = rule.rawk()
        if (undefined !== pk) {
          let nk: Record<string, any> | undefined = undefined
          for (let kn in pk) (nk ??= next.k)[kn] = pk[kn]
        }
        if (logging) why += 'P`' + alt.p + '`'
      }
      else {
        return this.bad(this.unknownRule(ctx.t0, alt.p), rule, ctx, { is_open })
      }
    }

    // ...or replace with a new rule.
    else if (alt.r) {
      let rulespec = ctx.rsm[alt.r]
      if (rulespec) {
        next = makeRule(rulespec, ctx, rule.node)
        next.parent = rule.parent
        next.prev = rule
        const pn = rule.rawn()
        if (undefined !== pn) {
          let nn: Counters | undefined = undefined
          for (let cn in pn) (nn ??= next.n)[cn] = pn[cn]
        }
        const pk = rule.rawk()
        if (undefined !== pk) {
          let nk: Record<string, any> | undefined = undefined
          for (let kn in pk) (nk ??= next.k)[kn] = pk[kn]
        }
        if (logging) why += 'R`' + alt.r + '`'
      }
      else {
        return this.bad(this.unknownRule(ctx.t0, alt.r), rule, ctx, { is_open })
      }
    }

    // Pop closed rule off stack.
    else if (!is_open) {
      next = ctx.rs[--ctx.rsI] || ctx.NORULE
    }


    // TODO: move action call here (alt.a)
    // and set r.next = next, so that action has access to next

    rule.next = next


    // Handle "after" call.
    let afters = is_open ? (rule.ao ? def.ao : null) : rule.ac ? def.ac : null
    if (afters) {
      let aout: Token | void = undefined
      for (let aI = 0; aI < afters.length; aI++) {
        aout = afters[aI](rule, ctx, next, aout)
        if (aout?.isToken && aout?.err) {
          return this.bad(aout, rule, ctx, { is_open })
        }
      }
    }

    next.why = why

    ctx.log && ctx.log(S.node, ctx, rule, lex, next)

    // Must be last as state change is for next process call.
    if (OPEN === rule.state) {
      rule.state = CLOSE
    }

    // Backtrack reduces consumed token count.
    let consumed = rule[is_open ? 'oN' : 'cN'] - (alt.b || 0)
    if (consumed < 0) consumed = 0

    if (0 < consumed) {
      // Shift the lookahead buffer left by `consumed` slots, filling
      // vacated tail positions with NOTOKEN so later alts re-fetch.
      // (The corresponding v-history push ran before alt actions.)
      const L = ctx.t.length
      for (let i = 0; i < L - consumed; i++) ctx.t[i] = ctx.t[i + consumed]
      for (let i = Math.max(0, L - consumed); i < L; i++) ctx.t[i] = ctx.NOTOKEN
    }

    return next
  }

  bad(tkn: Token, rule: Rule, ctx: Context, parse: { is_open: boolean }): Rule {
    // Opt-in recovery: record the error and continue from a sync point
    // instead of throwing (options.parse.recover).
    const rec = ctx.cfg?.parse?.recover
    if (rec?.enabled) {
      const next = attemptRecover(tkn, rule, ctx, parse)
      if (null != next) return next

      // Recovery gave up (caps or exhausted stack). The error is
      // already recorded; throw the last recorded one so parser.start
      // can convert the parse to { value, errors }.
      const lastErr = ctx.errs?.[ctx.errs.length - 1]
      if (null != lastErr) throw lastErr
    }

    throw new TabnasError(
      tkn.err || S.unexpected,
      {
        ...tkn.use,
        state: parse.is_open ? S.open : S.close,
      },
      tkn,
      rule,
      ctx,
    )
  }

  unknownRule(tkn: Token, name: string): Token {
    tkn.err = 'unknown_rule'
    tkn.use = tkn.use || {}
    tkn.use.rulename = name
    return tkn
  }
}

const makeRuleSpec = (...params: ConstructorParameters<typeof RuleSpec>) =>
  new RuleSpec(...params)

// First match wins.
// NOTE: input AltSpecs are used to build the Alt output.
// ---------------------------------------------------------------------------
// Error recovery (opt-in via options.parse.recover; see
// ts/doc/lsp-feasibility.md). All of this is reached only from error
// paths, and only when the flag is on — the match hot path is untouched.

// Close-alternate token info per rule spec, cached on the def.close
// array identity. `len` and `sig` guard in-place mutation of the alts
// list and a changed syncGroups configuration respectively.
type CloseInfo = {
  len: number
  sig: string
  sync: Set<Tin>   // leading tins of close alts tagged with a sync group
  all: Set<Tin>    // leading tins of every close alt (structural fallback)
  any: boolean     // spec has an empty-s close alt (accepts any token)
}

const closeInfoCache = new WeakMap<object, CloseInfo>()

// A bad token is produced without advancing the lex point (the
// matchers declined it) — skipping one forward requires moving the
// point past its span by hand, tracking rows and columns.
function advanceLexPast(lex: Lex, t: Token): void {
  const pnt = (lex as any).pnt
  const src: string = (lex as any).src
  const target = Math.max(pnt.sI, t.sI + Math.max(1, t.len | 0))
  while (pnt.sI < target && pnt.sI < src.length) {
    if ('\n' === src[pnt.sI]) {
      pnt.rI++
      pnt.cI = 1
    } else {
      pnt.cI++
    }
    pnt.sI++
  }
}

function closeInfo(spec: RuleSpec, groups: string[], sig: string): CloseInfo {
  const close = ((spec as any).def?.close ?? []) as NormAltSpec[]
  let info = closeInfoCache.get(close)
  if (null == info || info.len !== close.length || info.sig !== sig) {
    const sync = new Set<Tin>()
    const all = new Set<Tin>()
    let any = false
    for (const alt of close) {
      if (0 === (alt.sN | 0)) {
        any = true
        continue
      }
      const lead = alt.t?.[0] ?? []
      for (const tin of lead) all.add(tin)
      const g = alt.g ?? []
      for (const tag of g) {
        if (groups.includes(tag)) {
          for (const tin of lead) sync.add(tin)
          break
        }
      }
    }
    info = { len: close.length, sig, sync, all, any }
    closeInfoCache.set(close, info)
  }
  return info
}

// The sync token set for the current error, computed from the live
// rule stack: leading tins of close alternates whose g tags intersect
// parse.recover.syncGroups, plus explicit syncTins. When the grammar
// is untagged (sync set empty) the normative structural fallback
// applies: every close-leading tin anywhere in the stack.
function computeSyncTins(ctx: Context, rule: Rule): Set<Tin> {
  const rec = ctx.cfg.parse.recover
  const sig = rec.syncGroups.join(',')
  const out = new Set<Tin>(rec.syncTins)

  const addSync = (r: Rule) => {
    if (null != r && r !== ctx.NORULE) {
      closeInfo(r.spec, rec.syncGroups, sig).sync.forEach((t) => out.add(t))
    }
  }
  addSync(rule)
  for (let d = ctx.rsI - 1; 0 <= d; d--) addSync(ctx.rs[d])

  if (0 === out.size) {
    const addAll = (r: Rule) => {
      if (null != r && r !== ctx.NORULE) {
        closeInfo(r.spec, rec.syncGroups, sig).all.forEach((t) => out.add(t))
      }
    }
    addAll(rule)
    for (let d = ctx.rsI - 1; 0 <= d; d--) addAll(ctx.rs[d])
  }

  return out
}

// Does this rule's close state accept the token? An empty-s close
// alternate accepts anything (over-approximation: conditions and
// counters may still reject — cascade suppression absorbs that).
function acceptsClose(spec: RuleSpec, tin: Tin, groups: string[], sig: string): boolean {
  const info = closeInfo(spec, groups, sig)
  return info.any || info.all.has(tin)
}

// Panic-mode recovery: record the error, skip forward to a sync token,
// pop the rule stack to a rule that can consume it, and return that
// rule so the main loop continues. Returns undefined to give up (the
// caller throws; parser.start converts to { value, errors } in
// recovery mode).
function attemptRecover(
  tkn: Token,
  rule: Rule,
  ctx: Context,
  parse: { is_open: boolean },
): Rule | undefined {
  const rec = ctx.cfg.parse.recover
  const lex = ctx.lex
  if (null == lex) return undefined

  // Record the error (the TabnasError constructor pushes to ctx.errs).
  const err = new TabnasError(
    tkn.err || tkn.why || S.unexpected,
    { ...tkn.use, state: parse.is_open ? S.open : S.close },
    tkn,
    rule,
    ctx,
  )

  // Cascade suppression: an error within `suppress` consumed tokens of
  // the previous recovery is dropped as a follow-on of the same fault.
  const lastAbs: number | undefined = (ctx as any)._recoverAt
  if (null != lastAbs && ctx.vAbs - lastAbs < rec.suppress) {
    if (ctx.errs[ctx.errs.length - 1] === err) ctx.errs.pop()
  }

  if (rec.maxRecoveries < ctx.errs.length) return undefined

  // Strict-progress guard: when nothing was consumed since the last
  // recovery, the previous resume point failed to advance the parse
  // (e.g. every accepting alternate's condition rejected). Requiring
  // the next sync candidate to sit strictly beyond the previous one
  // bounds total recoveries by source length, so a recovery can never
  // loop in place.
  const lastSI: number = (ctx as any)._recoverSI ?? -1
  const noProgress = null != lastAbs && lastAbs === ctx.vAbs
  ;(ctx as any)._recoverAt = ctx.vAbs

  const sync = computeSyncTins(ctx, rule)
  const ZZ = ctx.cfg.t.ZZ
  const BD = ctx.cfg.t.BD
  const IGNORE = ctx.cfg.tokenSetTins.IGNORE
  const NOTOKEN = ctx.NOTOKEN
  const tbuf = ctx.t

  const advancePast = (t: Token) => advanceLexPast(lex, t)

  // Skip forward: drain already-fetched lookahead first (those tokens
  // advanced the lexer and must not be lost), then pull fresh tokens.
  // Bad tokens are skipped without recording — they are part of the
  // same error region.
  const pending: Token[] = []
  for (let i = 0; i < tbuf.length; i++) {
    const t = tbuf[i]
    if (null != t && NOTOKEN !== t) pending.push(t)
    tbuf[i] = NOTOKEN
  }

  const fetch = (): Token => {
    let t: Token
    do {
      t = lex.next(rule)
    } while (IGNORE[t.tin])
    return t
  }

  let cand: Token = 0 < pending.length ? (pending.shift() as Token) : fetch()
  let skipped = 0
  while (
    ZZ !== cand.tin &&
    (!sync.has(cand.tin) || (noProgress && cand.sI <= lastSI))
  ) {
    if (BD === cand.tin) advancePast(cand)
    if (rec.maxSkip <= skipped++) return undefined
    cand = 0 < pending.length ? (pending.shift() as Token) : fetch()
  }

  // End of source with no progress since the last recovery: the
  // grammar has already had its chance to close on the end token —
  // give up rather than spin on the pinned end token.
  if (ZZ === cand.tin && noProgress && lastSI >= cand.sI) {
    return undefined
  }

  ;(ctx as any)._recoverSI = cand.sI

  // Restore the buffer: the sync token first, then any remaining
  // pre-fetched lookahead in original order.
  tbuf[0] = cand
  for (let i = 0; i < pending.length && i + 1 < tbuf.length; i++) {
    tbuf[i + 1] = pending[i]
  }

  // The skipped region, for diagnostics consumers (not part of the
  // structured diagnostic JSON shape).
  ;(err as any).recovered = { skipped, sync: cand.tin }

  const sig = rec.syncGroups.join(',')

  if (rec.popUntilValid) {
    // Resume with the erroring rule itself if its close state accepts
    // the sync token, else pop ancestors until one does.
    if (rule !== ctx.NORULE && acceptsClose(rule.spec, cand.tin, rec.syncGroups, sig)) {
      if (OPEN === rule.state) rule.state = CLOSE
      return rule
    }
    while (0 < ctx.rsI) {
      const r = ctx.rs[--ctx.rsI]
      if (null != r && acceptsClose(r.spec, cand.tin, rec.syncGroups, sig)) {
        return r
      }
      // Force-popped without a close pass: synthesize the close
      // notification so structural consumers (outline/folding) see a
      // balanced event stream even through recovery.
      if (null != r && ctx.sub.ruleDone) {
        const done = { state: CLOSE, alt: null, forced: true }
        ctx.sub.ruleDone.map((s) => s(r, ctx, done))
      }
    }
    return undefined
  }

  // Fixed-depth pop: one rule.
  if (0 < ctx.rsI) return ctx.rs[--ctx.rsI]
  return undefined
}

function parse_alts(
  is_open: boolean,
  alts: NormAltSpec[],
  lex: Lex,
  rule: Rule,
  ctx: Context,
): AltMatch {
  // One reusable scratch AltMatch per parse Context (allocated lazily on
  // first use). Scoping it to the Context — rather than a module global —
  // keeps nested parses (a plugin action parsing with another instance)
  // from clobbering each other's in-flight match state.
  let out: AltMatch = (ctx as any)._palt || ((ctx as any)._palt = makeAltMatch())
  out.b = 0 // Backtrack n tokens.
  out.p = EMPTY // Push named rule onto stack.
  out.r = EMPTY // Replace current rule with named rule.
  out.n = undefined // Increment named counters.
  out.h = undefined // Custom handler function.
  out.a = undefined // Rule action.
  out.u = undefined // Custom rule properties.
  out.k = undefined // Custom rule properties (propagated).
  out.e = undefined // Error token.

  let alt: NormAltSpec | null = null
  let altI = 0
  let t = ctx.cfg.t
  let cond: boolean = true
  let bitAA = 1 << (t.AA - 1)

  let IGNORE = ctx.cfg.tokenSetTins.IGNORE
  let BD = t.BD
  // S (the string table) is shadowed by alt.S inside the loop below.
  const UNEXPECTED = S.unexpected

  // TODO: replace with lookup map
  let len = alts.length
  const NOTOKEN = ctx.NOTOKEN
  const tbuf = ctx.t

  // Negotiated lexing (cfg.lex.relex): a token fetched under one
  // rule context keeps its identity in the pushback buffer, but a
  // character claimable by several matchers may legitimately be a
  // different token for a different alternate. When enabled, a tin
  // mismatch is not final: the alternate may re-cut the span under its
  // own token list. See Lex.relex.
  const RELEX = ctx.cfg.lex.relex

  // Undo state for a recut this alternate commits. The token buffer is
  // shared with every later alternate AND with later rules, so a cut
  // chosen for an alternate that then fails would otherwise be inherited
  // as if it had been chosen for them — which is how a renegotiation
  // could turn a working parse into a failing one. Only the FIRST recut
  // of an alternate is recorded: restoring to before it undoes any
  // later ones too. Plain locals, so the common no-recut path allocates
  // nothing.
  let unI = -1
  let unTkn: Token = NOTOKEN
  let unSI = 0
  let unRI = 0
  let unCI = 0
  let unQueue: Token[] | null = null
  let unEnd: Token | undefined = undefined

  for (altI = 0; altI < len; altI++) {
    alt = alts[altI] as NormAltSpec

    // Number of positions that matched in this alt. Tracked so the
    // rule can record exactly which tokens it consumed.
    let matched = 0
    cond = true
    unI = -1

    const S = alt.S
    const sN = alt.sN | 0

    // Iterate alt's lookahead positions. Each position is fetched
    // lazily and only when the previous position matched, preserving
    // the original 2-slot lazy behaviour for any N.
    //
    // A null entry in S[i] means "no Tin constraint at this position"
    // (wildcard) - the token is still fetched and consumed, but the
    // bit-field check is skipped. This matches the `s` docstring
    // ("null if position matches any token") and prevents silently
    // dropping the check at a later required position.
    for (let i = 0; i < sN; i++) {
      let tkn = tbuf[i]
      if (null == tkn || NOTOKEN === tkn) {
        // Fetch (skipping IGNORE tokens) inline — a nested function here
        // would allocate a closure on every parse_alts call.
        let refetch = false
        do {
          refetch = false
          tkn = lex.next(rule, alt, altI, i)
          ctx.tC++
          // Bad tokens abort the parse with their own error code
          // (formerly done by the badlex wrapper around lex.next).
          //
          // Under negotiated lexing a bad token is a soft failure
          // instead: the current rule's token column may simply be
          // unable to produce the character (a user rule whose only
          // viable alternate is empty, sitting before a class-matched
          // token, has nothing to gate the class in with). The bad
          // token stays buffered — an alternate here or in a later
          // rule renegotiates it via relex, and if nothing ever can,
          // the parse still fails at this exact token.
          if (BD === tkn.tin && !RELEX) {
            let details: any = {}
            if (null != tkn.use) {
              details.use = tkn.use
            }
            // Opt-in recovery: lexer soft mode. Record the bad token's
            // own error and skip it, keeping the fetch going; beyond
            // the recovery cap the parse gives up as usual.
            const rec = ctx.cfg.parse.recover
            if (rec.enabled) {
              const bderr = new TabnasError(
                tkn.why || UNEXPECTED,
                details,
                tkn,
                rule,
                ctx,
              )
              // Coalesce a contiguous run of bad tokens (e.g. each
              // character of an unlexable word) into one recorded
              // error whose region metadata grows with the run.
              const runEnd: number | undefined = (ctx as any)._badTo
              const runErr: any = (ctx as any)._badErr
              if (
                null != runEnd &&
                tkn.sI <= runEnd &&
                null != runErr &&
                ctx.errs[ctx.errs.length - 1] === bderr
              ) {
                ctx.errs.pop()
                runErr.recovered.skipped++
              } else {
                ;(bderr as any).recovered = { skipped: 1, bad: true }
                ;(ctx as any)._badErr = bderr
              }
              ;(ctx as any)._badTo = tkn.sI + Math.max(1, tkn.len | 0)
              if (rec.maxRecoveries < ctx.errs.length) {
                throw bderr
              }
              // The bad token did not advance the lex point — move
              // past it or the refetch would loop in place.
              advanceLexPast(lex, tkn)
              refetch = true
              continue
            }
            throw new TabnasError(
              tkn.why || UNEXPECTED,
              details,
              tkn,
              rule,
              ctx,
            )
          }
        } while (refetch || IGNORE[tkn.tin])
        tbuf[i] = tkn
      }

      const Si = S ? S[i] : null

      // A bad token never satisfies a position. Under negotiated lexing
      // its immediate throw is deferred (above) so an alternate can try
      // to re-cut the span into something it names — but that is ALL the
      // deferral buys it. Without this test a wildcard position would
      // accept it outright: `#AA` compiles to a null `Si`, and the
      // match-any bit would cover a `#BD` tin too, so malformed input
      // that `relex: false` rejects would parse.
      const isBad = BD === tkn.tin
      if (isBad || null != Si) {
        let hit = false
        if (!isBad && null != Si) {
          const tin = tkn.tin
          const part = (tin / 31) | 0
          // bitAA lives in partition 0 (tin=AA=4). ORing it into the
          // match mask for any partition other than 0 lets unrelated
          // tokens in higher partitions collide with alts that merely
          // set bit 3 of their own partition — a false positive. Apply
          // bitAA only when testing a partition-0 token.
          const aaBit = part === 0 ? bitAA : 0
          hit = 0 !== (Si[part] & ((1 << ((tin % 31) - 1)) | aaBit))
        }
        if (!hit) {
          // Negotiated lexing: before failing this alternate, ask the
          // lexer whether the same span cuts to a tin this alternate
          // wants. Bounded: at most one recut per (alternate, position),
          // and the recut's tin is in this alt's set by construction.
          let recut: Token | undefined = undefined
          if (RELEX && 0 < tkn.len) {
            const want = alt.t[i]
            if (null != want && 0 < want.length) {
              recut = lex.relex(tkn, want, rule)
            }
          }
          if (null == recut) {
            cond = false
            break
          }
          // First recut of this alternate: remember how to undo it.
          if (-1 === unI) {
            const u = lex.relexUndo
            unI = i
            unTkn = tkn
            unSI = u.sI
            unRI = u.rI
            unCI = u.cI
            unQueue = u.token
            unEnd = u.end
          }
          // The recut replaces the buffered token; anything fetched
          // beyond it was lexed from positions that may no longer
          // exist, so it is dropped and re-fetched on demand.
          tbuf[i] = recut
          for (let j = i + 1; j < tbuf.length; j++) {
            tbuf[j] = NOTOKEN
          }
        }
      }
      matched = i + 1
    }

    // Record matched tokens only when the tin positions matched —
    // failed alts left partial recordings that nothing could observe
    // (custom conditions only run on tin success, and the next
    // candidate overwrote them). Recording stays BEFORE the alt.c
    // condition call so conditions observe the candidate's tokens.
    if (cond) {
      if (is_open) {
        rule.oN = matched
        for (let i = 0; i < matched; i++) rule.o[i] = tbuf[i]
        // Clear trailing slots so stale matches from earlier alts are
        // not observed via rule.o[i] / rule.o0 / rule.o1 accessors.
        for (let i = matched; i < rule.o.length; i++) rule.o[i] = NOTOKEN
      } else {
        rule.cN = matched
        for (let i = 0; i < matched; i++) rule.c[i] = tbuf[i]
        for (let i = matched; i < rule.c.length; i++) rule.c[i] = NOTOKEN
      }

      // Optional custom condition
      if (alt.c) {
        cond = alt.c(rule, ctx, out)
      }
    }

    if (cond) {
      break
    }
    else {
      alt = null
      // This alternate renegotiated a token and then failed anyway —
      // put the cut back, so the alternates and rules that follow see
      // the buffer as it was before this one touched it.
      if (-1 !== unI) {
        lex.unrelex(unSI, unRI, unCI, unQueue as Token[], unEnd)
        tbuf[unI] = unTkn
        for (let j = unI + 1; j < tbuf.length; j++) {
          tbuf[j] = NOTOKEN
        }
        unI = -1
      }
    }
  }

  if (!cond) {
    const bad = tbuf[0]
    // No alternate could use the token and it is a bad one: raise the
    // lexer's own error, exactly as the non-negotiated path does at
    // fetch time. Deferring that throw is what let the alternates try to
    // re-cut it; now that all of them have declined, the specific
    // diagnostic is the useful one.
    if (RELEX && null != bad && BD === bad.tin && !ctx.cfg.parse.recover.enabled) {
      const details: any = {}
      if (null != bad.use) {
        details.use = bad.use
      }
      throw new TabnasError(bad.why || UNEXPECTED, details, bad, rule, ctx)
      // In recovery mode the bad token flows through out.e below into
      // RuleSpec.bad, which routes it into attemptRecover.
    }
    out.e = tbuf[0] ?? NOTOKEN
  }

  if (alt) {
    out.n = null != alt.n ? alt.n : out.n
    out.h = null != alt.h ? alt.h : out.h
    out.a = null != alt.a ? alt.a : out.a
    out.u = null != alt.u ? alt.u : out.u
    out.k = null != alt.k ? alt.k : out.k
    out.g = null != alt.g ? alt.g : out.g

    out.e = (alt.e && alt.e(rule, ctx, out)) || undefined

    out.p =
      null != alt.p && false !== alt.p
        ? 'string' === typeof alt.p
          ? alt.p
          : alt.p(rule, ctx, out)
        : out.p

    out.r =
      null != alt.r && false !== alt.r
        ? 'string' === typeof alt.r
          ? alt.r
          : alt.r(rule, ctx, out)
        : out.r

    out.b =
      null != alt.b && false !== alt.b
        ? 'number' === typeof alt.b
          ? alt.b
          : alt.b(rule, ctx, out)
        : out.b
  }

  let match = altI < alts.length

  ctx.log && ctx.log(S.parse, ctx, rule, lex, match, cond, altI, alt, out)

  return out
}


const partify = (tins: Tin[], part: number) =>
  tins.filter((tin) => 31 * part <= tin && tin < 31 * (part + 1))

const bitify = (s: Tin[], part: number) =>
  s.reduce(
    (bits: number, tin: Tin) => (1 << (tin - (31 * part + 1))) | bits,
    0,
  )


// Valid group-tag pattern: lowercase letter followed by one or more
// lowercase letters, digits, or hyphens. Enforced by normalt().
const GROUP_TAG_RE = /^[a-z][a-z0-9-]+$/

// Normalize AltSpec (mutates).
function normalt(a: AltSpec, rs: RuleState, r: RuleSpec): NormAltSpec {
  // Ensure groups are a string[]
  if (STRING === typeof a.g) {
    a.g = (a as any).g.split(/\s*,\s*/)
  } else if (null == a.g) {
    a.g = []
  }

  // Validate every group tag (reject empty and non-matching tags).
  for (let tag of (a.g as string[])) {
    if (!GROUP_TAG_RE.test(tag)) {
      throw new Error(
        `Grammar: invalid group tag "${tag}" ` +
        `in rule ${r.name} (${rs}) — must match ${GROUP_TAG_RE}`
      )
    }
  }

  a.g = (a as any).g.sort()

  const aa = a as any

  if (!a.s || 0 === a.s.length) {
    a.s = null
    aa.t = []
    aa.S = null
    aa.sN = 0
  }
  else {
    const tinsify = (s: any[]): Tin[] => {
      const tins = s
        .flat()
        .map((n) => 'string' === typeof n ? n.split(/\s* +\s*/) : n)
        .flat()
        .map((n) => 'string' === typeof n ? (r.ji.tokenSet(n) ?? r.ji.token(n)) : n)
        .flat()
        .filter((tin) => 'number' === typeof tin) as Tin[]
      return tins
    }


    if ('string' === typeof a.s) {
      a.s = a.s.split(/\s* +\s*/)
    }

    // Per-position resolved tins and bit-field match tables.
    // alt.t[i] holds the Tin[] for position i (used by tcol collation);
    // alt.S[i] holds the bit-packed lookup (null if position is empty,
    // which should not normally occur - tinsify filters nulls).
    const sN = a.s.length
    const t: Tin[][] = new Array(sN)
    const S: (number[] | null)[] = new Array(sN)

    for (let i = 0; i < sN; i++) {
      const tins: Tin[] = tinsify([a.s[i]])
      t[i] = tins
      // `#AA` is the ANY wildcard — a position whose tin list
      // includes it must match every lexed token regardless of
      // partition. Represent that by dropping to the existing
      // `S[i] = null` sentinel ("no constraint"), bypassing the
      // per-partition bitset check in parse_alts. The t[i] entry
      // keeps the raw tin list so tcol collation still reflects
      // what the user wrote.
      const aaTin = r.ji.token('#AA')
      if (aaTin != null && tins.includes(aaTin)) {
        S[i] = null
        continue
      }
      S[i] =
        0 < tins.length
          ? new Array(Math.max(...tins.map((tin) => (1 + tin / 31) | 0)))
            .fill(null)
            .map((_, j) => j)
            .map((part) => bitify(partify(tins, part), part))
          : null
    }

    aa.t = t
    aa.S = S
    aa.sN = sN
  }

  if (!a.p) {
    a.p = null
  }
  else {
    resolveFunctionRef('push', rs, r, a, 'p')
  }

  if (!a.r) {
    a.r = null
  }
  else {
    resolveFunctionRef('replace', rs, r, a, 'r')
  }

  if (!a.b) {
    a.b = null
  }
  else {
    resolveFunctionRef('back', rs, r, a, 'b')
  }

  if (!a.a) {
    a.a = null
  }
  else {
    resolveFunctionRef('action', rs, r, a, 'a')
  }

  if (!a.h) {
    a.h = null
  }
  else {
    resolveFunctionRef('modify', rs, r, a, 'h')
  }

  if (!a.e) {
    a.e = null
  }
  else {
    resolveFunctionRef('error', rs, r, a, 'e')
  }


  if (!a.c) {
    a.c = null
  }
  else {
    const ct = typeof a.c

    if ('string' === ct) {
      resolveFunctionRef('condition', rs, r, a, 'c')
    }
    else if ('function' === ct) {
      if ('c' === a.c.name) {
        defprop(a.c, 'name', { value: 'ruleCond' })
      }
    }
    else if ('object' === ct) {
      const ac: Record<string, any> = a.c
      const conds: NormAltCond[] = []
      const ruleprops = Object.keys(a.c)
      for (let prop of ruleprops) {
        const pspec = ac[prop]
        if (null != pspec) {
          // Validate BEFORE building: a bad operator or an unresolvable path
          // used to be skipped, leaving `conds` empty so `c` was deleted and
          // the alternate became UNCONDITIONAL — a typo turned a guard into a
          // match-everything, silently. This runs while the grammar is built,
          // so it can never surface during a parse.
          const problems = condProblems(prop, pspec)
          if (0 < problems.length) {
            // Name the rule and phase: in a grammar of any size, "unknown
            // condition path" on its own is a needle in a haystack.
            const where = (r?.name ?? '?') + '.' + (OPEN === rs ? 'open' : 'close')
            throw new Error('tabnas: ' + where + ': ' + problems.join('; '))
          }

          if ('object' === typeof pspec) {
            for (let co of Object.keys(pspec)) {
              conds.push(makeRuleCond(co, prop, pspec[co]))
            }
          }
          else {
            conds.push(makeRuleCond('$eq', prop, pspec))
          }
        }
      }

      if (0 === conds.length) {
        delete a.c
      }
      else if (1 === conds.length) {
        a.c = conds[0]
      }
      else {
        a.c = function conjunctCond(r: Rule, c: Context, a: AltMatch) {
          for (let cond of conds) {
            let pass = cond(r, c, a)
            if (false == pass) {
              return false
            }
          }
          return true
        }
      }
    }
    else {
      throw new Error('Grammar: invalid condition: ' + a.c)
    }
  }

  return a as NormAltSpec
}


function isfnref(v: any) {
  return 'string' === typeof v && v.startsWith('@')
}


function resolveFunctionRef(
  fkind: string,
  rs: RuleState,
  r: RuleSpec,
  a: AltSpec,
  k: keyof AltSpec
) {
  const val = a[k]

  // An action (`a`) may be a list of refs/functions, run in order when
  // the alt matches: the matched alt's own action first, then composed
  // user actions. This lets a serialized, function-free grammar carry
  // `a: ['@node$', '@my-user-action']` and resolve each by name. The
  // array is collapsed here to a single function so `process()` keeps
  // invoking one `alt.a`. Gated to `a` only — the other fields are
  // single-valued.
  if ('a' === k && Array.isArray(val)) {
    if (0 === val.length) {
      a[k] = null as any
      return
    }
    const fns: AltAction[] = val.map((v: any) => {
      if (isfnref(v)) {
        const f = r.def.fnref[v as FuncRef] as AltAction
        if (null == f) {
          throw new Error(`Grammar: unknown ${fkind} function reference: ` + v +
            ` for rule ${r.name} (${rs}) and alt ${a.s} (${a.g})`)
        }
        return f
      }
      return v as AltAction
    })
    a[k] = function composedAction(rule: Rule, ctx: Context, alt: AltMatch) {
      let out: any
      for (const f of fns) {
        out = f(rule, ctx, alt)
        // Preserve the error-token short-circuit semantics of process().
        if (out && out.isToken && out.err) return out
      }
      return out
    } as any
    return
  }

  if (isfnref(val)) {
    const func = r.def.fnref[val as FuncRef] as Function
    if (null == func) {
      throw new Error(`Grammar: unknown ${fkind} function reference: ` + val +
        ` for rule ${r.name} (${rs}) and alt ${a.s} (${a.g})`)
    }
    a[k] = func as any
  }
}


// Operators accepted in a declarative condition. An operator missing from
// this table used to be SILENTLY DROPPED — and if it was the only one, the
// alternate lost `c` entirely and became unconditional, which is worse than
// failing. $exist was implemented in makeRuleCond but never listed here, so
// `{ 'n.k': { $exist: true } }` quietly matched everything.
const COND_OPS: Record<string, number> = {
  $eq: 1,
  $ne: 1,
  $lt: 1,
  $lte: 1,
  $gt: 1,
  $gte: 1,
  $exist: 1,
}

// Roots a declarative condition path may start from: the Rule members a
// condition can read. A path rooted anywhere else can NEVER resolve, so the
// ordered operators fail open on it forever and the guard silently does
// nothing — the same class of bug as an unknown operator, and just as quiet.
// `n`/`u`/`k` carry arbitrary user keys below the root, so only the root is
// checked.
const COND_PATH_ROOTS: Record<string, number> = {
  n: 1, u: 1, k: 1,                       // counters, user data, kept data
  d: 1, i: 1, name: 1, state: 1,          // identity / position
  node: 1, need: 1, oN: 1, cN: 1,
  o: 1, c: 1, o0: 1, o1: 1, c0: 1, c1: 1, // matched tokens
  parent: 1, child: 1, prev: 1, next: 1,  // rule graph
  spec: 1,
}

/** Every problem in an alternate's DECLARATIVE parts, as messages.
 *
 * Pure: it reports instead of throwing, so a whole grammar can be checked and
 * every problem listed at once. `normalt` calls it while the grammar is being
 * built and raises on what it finds, which is why a bad declarative spec can
 * never surface during a parse — but a grammar held as data (the Grammar /
 * GrammarText path, a generator, an editor) can be checked with this directly,
 * before any parser exists.
 *
 * Only declarative fields are checkable: a condition given as a function is
 * opaque, and `p`/`r` rule names may legitimately be defined later. */
export function validateAlt(alt: any): string[] {
  const out: string[] = []

  if (null == alt || 'object' !== typeof alt) {
    return out
  }

  // Condition, object form only — a function condition is opaque.
  if (null != alt.c && 'object' === typeof alt.c && 'function' !== typeof alt.c) {
    for (const prop of Object.keys(alt.c)) {
      const pspec = alt.c[prop]
      if (null != pspec) {
        out.push(...condProblems(prop, pspec))
      }
    }
  }

  // Group tags: same rule normalt enforces.
  if (null != alt.g) {
    const tags = 'string' === typeof alt.g ? alt.g.split(',') : alt.g
    if (Array.isArray(tags)) {
      for (const tag of tags) {
        if ('string' === typeof tag && !GROUP_TAG_RE.test(tag.trim())) {
          out.push('invalid group tag: "' + tag + '"')
        }
      }
    }
  }

  return out
}

/** Every problem in a list of alternates, each prefixed with where it is.
 * `label` names the list, e.g. `"val.open"`. */
export function validateAlts(alts: any[], label: string = ''): string[] {
  const out: string[] = []
  const at = label ? label + ' ' : ''

  if (!Array.isArray(alts)) {
    return out
  }

  for (let index = 0; index < alts.length; index++) {
    for (const problem of validateAlt(alts[index])) {
      out.push(at + 'alt[' + index + ']: ' + problem)
    }
  }

  return out
}

/** Problems with one declarative condition entry: `prop` against `pspec`.
 * Pure — returns messages instead of throwing, so a validation pass can
 * collect every problem in a grammar rather than stopping at the first. */
function condProblems(prop: string, pspec: any): string[] {
  const out: string[] = []

  const root = prop.split('.')[0]
  if (1 !== COND_PATH_ROOTS[root]) {
    out.push('unknown condition path: "' + prop + '" (no rule property "' +
      root + '"); known roots: ' + Object.keys(COND_PATH_ROOTS).join(', '))
  }

  if (null != pspec && 'object' === typeof pspec) {
    for (const co of Object.keys(pspec)) {
      if (1 !== COND_OPS[co]) {
        out.push('unknown condition operator: ' + co + ' (on "' + prop +
          '"); known operators: ' + Object.keys(COND_OPS).join(', '))
      }
    }
  }

  return out
}


function makeRuleCond(co: string, prop: string, val: any) {
  const path = prop.split('.')

  // A COUNTER path (`n.<name>`) compared against a number reads as 0 when the
  // counter was never set: it has counted nothing, so the comparison stays
  // total (exactly one of <, =, > holds). Previously an unset counter made
  // every operator true, so `$lt` and `$gt` both passed and a "past the limit"
  // guard fired on the first token.
  //
  // This applies ONLY to counters. Any other path that fails to resolve — an
  // absent `o0`, a `u.*` you never set — is genuine absence, not zero, and
  // inventing a 0 there would silently answer a question the rule cannot
  // answer; those keep the permissive short-circuit below. `$exist` is the
  // explicit set/unset test and never coerces.
  const iscounter = 'n' === path[0] && 2 === path.length && 'number' === typeof val
  const read = (r: Rule) => {
    const rval = getpath(r, path)
    return (null == rval && iscounter) ? 0 : rval
  }

  if ('$eq' === co) {
    return function ruleCond(r: Rule, _c: Context, _a: AltMatch) {
      return read(r) === val
    }
  }
  else if ('$ne' === co) {
    return function ruleCond(r: Rule, _c: Context, _a: AltMatch) {
      return read(r) != val
    }
  }
  else if ('$lt' === co) {
    return function ruleCond(r: Rule, _c: Context, _a: AltMatch) {
      const rval = read(r)
      return null == rval || rval < val
    }
  }
  else if ('$lte' === co) {
    return function ruleCond(r: Rule, _c: Context, _a: AltMatch) {
      const rval = read(r)
      return null == rval || rval <= val
    }
  }

  else if ('$gt' === co) {
    return function ruleCond(r: Rule, _c: Context, _a: AltMatch) {
      const rval = read(r)
      return null == rval || rval > val
    }
  }
  else if ('$gte' === co) {
    return function ruleCond(r: Rule, _c: Context, _a: AltMatch) {
      const rval = read(r)
      return null == rval || rval >= val
    }
  }
  else if ('$exist' === co) {
    return function ruleCond(r: Rule, _c: Context, _a: AltMatch) {
      const rval = getpath(r, path)
      return true === val ? null != rval : null == rval
    }
  }
  else {
    throw new Error('Grammer: unknown comparison operator: ' + co)
  }
}



export { Rule, RuleSpec, AltMatch, makeRule, makeNoRule, makeRuleSpec }
