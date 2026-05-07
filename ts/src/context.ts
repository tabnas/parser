/* Copyright (c) 2021-2026 Richard Rodger and other contributors, MIT License */

/*  context.ts
 *  Per-parse Context — the state object passed to every rule action
 *  and lex matcher. Defined as a class so the legacy `t0` / `t1` /
 *  `v1` / `v2` aliases live on the prototype as accessors instead of
 *  being installed by hand at parse start, and so `mark()` / `rewind()`
 *  are real methods rather than functions attached to a literal.
 */

import type {
  Amagama,
  Config,
  LexSub,
  AmagamaOptions,
  Plugin,
  Rule,
  RuleSpec,
  RuleSub,
  Tin,
  Token,
} from './types'


// Fields the parser supplies up front when starting a parse. The
// remaining Context fields (rule, NORULE, log, lex …) get filled in
// after construction.
export type ContextInit = {
  opts: AmagamaOptions
  cfg: Config
  meta: Record<string, any>
  src: () => string
  root: () => any
  plgn: () => Plugin[]
  inst: () => Amagama
  sub: { lex?: LexSub[]; rule?: RuleSub[] }
  rsm: { [name: string]: RuleSpec }
  F: (s: any) => string
  NOTOKEN: Token
  NORULE: Rule
}


export class Context {
  // Plugin / rule decoration — kept open so plugins can stash custom
  // state without TypeScript complaints.
  [key: string]: any

  uI = 0           // Rule index.
  opts!: AmagamaOptions   // Amagama instance options.
  cfg!: Config     // Amagama instance config.
  meta!: Record<string, any>       // Parse meta parameters.
  src!: () => string
  root!: () => any
  plgn!: () => Plugin[]
  inst!: () => Amagama

  rule!: Rule      // Current rule instance — set by parser.start().
  sub!: { lex?: LexSub[]; rule?: RuleSub[] }

  xs: Tin = -1 as Tin // Lex state tin.

  // Consumed-token history. v1 / v2 (below) read from the top of
  // this stack. vAbs is the absolute count of pushed-not-rewound
  // tokens since parse start; mark() returns it so ring-buffer
  // eviction of old entries doesn't invalidate outstanding marks.
  v: Token[] = []
  vAbs = 0

  // Lookahead buffer. Seeded with two NOTOKEN slots; grows as alts
  // request deeper positions via t[i].
  t!: Token[]

  tC = -2          // Prepare count for lookahead (two seeded slots).
  kI = -1          // Parser rule iteration count.
  rs: Rule[] = []  // Rule stack.
  rsI = 0
  rsm!: { [name: string]: RuleSpec }
  log?: (...rest: any) => void
  F!: (s: any) => string
  u: Record<string, any> = {}      // Custom meta data (for use by plugins).
  NOTOKEN!: Token
  NORULE!: Rule
  lex?: any        // Attached by parser.start() once the lexer exists.


  constructor(init: ContextInit) {
    this.opts = init.opts
    this.cfg = init.cfg
    this.meta = init.meta
    this.src = init.src
    this.root = init.root
    this.plgn = init.plgn
    this.inst = init.inst
    this.sub = init.sub
    this.rsm = init.rsm
    this.F = init.F
    this.NOTOKEN = init.NOTOKEN
    this.NORULE = init.NORULE
    this.rule = init.NORULE  // overwritten by parser.start once the
                             // root rule is built; safe placeholder.
    this.t = [init.NOTOKEN, init.NOTOKEN]
  }


  // Legacy aliases for the first two slots of the lookahead buffer.
  // Reading an unfetched slot yields NOTOKEN; writing seeds the slot.
  // @deprecated Use t[0] / t[1] directly in new grammar code.
  get t0(): Token {
    return this.t[0] ?? this.NOTOKEN
  }
  set t0(tkn: Token) {
    this.t[0] = tkn
  }

  get t1(): Token {
    return this.t[1] ?? this.NOTOKEN
  }
  set t1(tkn: Token) {
    this.t[1] = tkn
  }


  // Most-recently-consumed token (top of the v stack).
  // Setting v1 with an empty stack pushes; otherwise replaces top.
  get v1(): Token {
    return this.v[this.v.length - 1] ?? this.NOTOKEN
  }
  set v1(tkn: Token) {
    if (0 < this.v.length) this.v[this.v.length - 1] = tkn
    else this.v.push(tkn)
  }

  // Token before the previous one (one below the top).
  get v2(): Token {
    return this.v[this.v.length - 2] ?? this.NOTOKEN
  }
  set v2(tkn: Token) {
    const L = this.v.length
    if (1 < L) this.v[L - 2] = tkn
    else if (1 === L) this.v.unshift(tkn)
    else this.v.push(tkn)
  }


  // Save a rewind mark at the current parse position. The returned
  // value can be passed to rewind() to replay the tokens consumed
  // since the mark was taken, re-feeding them through the lexer's
  // pending-token queue.
  mark(): number {
    return this.vAbs
  }


  // Replay the tokens consumed since the given mark.
  //
  // Marks are absolute rather than array-relative so the ring-buffer
  // cap (options.rewind.history) can evict old tokens from the front
  // of v without invalidating mark values held by in-flight rule
  // actions. A rewind whose target has been evicted throws — the
  // caller's retained-history budget was too small for the grammar.
  rewind(mark: number): void {
    const k = this.vAbs - mark
    if (k <= 0) return
    if (k > this.v.length) {
      throw new Error(
        `amagama: ctx.rewind target ${mark} is outside the retained ` +
        `history window (oldest mark available is ${this.vAbs - this.v.length}, ` +
        `current is ${this.vAbs}); increase options.rewind.history.`,
      )
    }
    const queue: Token[] = this.lex.pnt.token
    const NOTOKEN = this.NOTOKEN

    // The lookahead buffer (this.t) holds tokens the lexer has already
    // produced past the current consumed position but that haven't
    // been committed to this.v yet. They advanced the lexer's sI — so
    // if we just invalidated the buffer, those source chars would be
    // lost. Preserve them by splicing into the front of the pending
    // queue in the order the lexer produced them, BEHIND the rewound
    // consumed tokens that come next.
    const pendingLookahead: Token[] = []
    for (let i = 0; i < this.t.length; i++) {
      const tkn = this.t[i]
      if (tkn && tkn !== NOTOKEN) pendingLookahead.push(tkn)
      this.t[i] = NOTOKEN
    }
    // Un-shift pre-lexed lookahead (oldest-first order at the queue
    // head), so the next lex.next() serves them in the same order
    // they were originally produced.
    for (let i = pendingLookahead.length - 1; i >= 0; i--) {
      queue.unshift(pendingLookahead[i])
    }
    // Then unshift the rewound consumed tokens — they go in FRONT of
    // the lookahead, so the next lex.next() serves the oldest rewound
    // consumed token first, then the rest in order.
    for (let i = 0; i < k; i++) {
      // Pop newest-first, unshift in that order — the first unshift
      // lands the newest at the queue's head; the next unshift slides
      // older tokens in front of it, so the queue reads oldest-first.
      queue.unshift(this.v.pop()!)
    }
    this.vAbs -= k
    // Clear the lexer's cached end-of-source token so lex.next serves
    // from the newly-replenished queue rather than short-circuiting
    // to #ZZ. (Once the lexer has produced the end token it pins it
    // to pnt.end; the rewound tokens would otherwise be unreachable.)
    this.lex.pnt.end = undefined
  }
}
