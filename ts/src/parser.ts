/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  parser.ts
 *  Parser implementation, converts the lexer tokens into parsed data.
 */

import type {
  Config,
  TabnasOptions,
  ParsePrepare,
  Rule,
  RuleDefiner,
  RuleSpec,
  RuleSpecMap,
  Tabnas,
} from './types'

import { EMPTY } from './types'

import { Context } from './context'

import {
  S,
  deep,
  filterRules,
  srcfmt,
  tokenize,
  // log_stack,
  values,
} from './utility'

import { TabnasError } from './error'

import { makeNoToken, makeLex, makePoint, makeToken } from './lexer'

import { makeRule, makeRuleSpec } from './rules'


// Rule-driven parser: start() parses from scratch, clone() makes a child sibling.
class Parser {
  options: TabnasOptions    // Raw user options.
  cfg: Config               // Resolved configuration.
  rsm: RuleSpecMap = {}     // Rule specs keyed by rule name.
  ji: Tabnas                // Owning Tabnas instance.

  // Per-Parser caches for per-parse setup that depends only on cfg.
  // options()/make() build a fresh Parser (makeParser/clone), so these
  // never outlive the configuration they were built from.
  #nrspec?: RuleSpec              // Empty spec backing the NORULE sentinel.
  #srcfmt?: (s: any) => string    // Debug source formatter.

  constructor(options: TabnasOptions, cfg: Config, j: Tabnas) {
    this.options = options
    this.cfg = cfg
    this.ji = j
  }

  // TODO: ensure chains properly, both for create and extend rule
  // Multi-functional get/set for rules.
  rule(
    name?: string,
    define?: RuleDefiner | null,
  ): RuleSpec | RuleSpecMap | undefined {
    // If no name, get all the rules.
    if (null == name) {
      return this.rsm
    }

    // Else get a rule by name.
    let rs: RuleSpec = this.rsm[name]

    // Else delete a specific rule by name.
    if (null === define) {
      delete this.rsm[name]
    }

    // Else add or redefine a rule by name.
    else if (undefined !== define) {
      rs = this.rsm[name] = this.rsm[name] || makeRuleSpec(this.ji, this.cfg, {})
      rs.name = name
      rs = this.rsm[name] = define(this.rsm[name], this) || this.rsm[name]

      // Ensures tabnas.rule can chain
      return undefined
    }

    return rs
  }

  start(src: string, tabnas: Tabnas, meta?: any, parent_ctx?: any): any {
    let root: Rule

    let endtkn = makeToken(
      '#ZZ',
      tokenize('#ZZ', this.cfg),
      undefined,
      EMPTY,
      makePoint(-1),
    )

    let bdtin = tokenize('#BD', this.cfg)

    let notoken = makeNoToken()

    // Build the per-parse Context. NORULE is patched in once we have
    // the actual no-rule sentinel — the constructor uses NOTOKEN as a
    // placeholder so the class invariants (rule != null) hold.
    let ctx = new Context({
      opts: this.options,
      cfg: this.cfg,
      meta: meta || {},
      src: () => src, // Avoid printing src
      root: () => root,
      plgn: () => tabnas.internal().plugins,
      inst: () => tabnas,
      sub: tabnas.internal().sub,
      rsm: this.rsm,
      F: (this.#srcfmt ??= srcfmt(this.cfg)),
      NOTOKEN: notoken,
      NORULE: {} as Rule,
    })

    // Merge in any caller-supplied parent_ctx (plugin tests use this
    // to seed `meta` or other fields). `deep` mutates the class
    // instance in place, so getters/setters and methods survive.
    if (null != parent_ctx) {
      deep(ctx, parent_ctx)
    }

    // The no-rule sentinel Rule is per-parse, but its (empty) spec is
    // immutable and cfg-scoped, so build the spec once per Parser.
    let norule = makeRule((this.#nrspec ??= makeRuleSpec(this.ji, this.cfg, {})), ctx)
    ctx.NORULE = norule
    ctx.rule = norule

    // makelog(ctx, meta)
    if (meta && S.function === typeof meta.log) {
      ctx.log = meta.log
    }

    this.cfg.parse.prepare.forEach((prep: ParsePrepare) =>
      prep(tabnas, ctx, meta),
    )

    // Special case - avoids extra per-token tests in main parser rules.
    if ('' === src) {
      if (this.cfg.lex.empty) {
        return this.cfg.lex.emptyResult
      } else {
        throw new TabnasError(S.unexpected, { src }, ctx.t0, norule, ctx)
      }
    }

    // Bad tokens are converted to throws by the engine's own token
    // consumers (the parse_alts fetch loop, and the trailing-content
    // check below) rather than by wrapping lex.next in a closure — the
    // old badlex wrapper cost a bind + closure + Lex shape transition
    // per parse and an extra call frame per token. The exported badlex
    // helper remains for standalone-lexer users.
    let lex = makeLex(ctx)

    // Stash lex on ctx so ctx.rewind can push tokens back onto the
    // lexer's pending-token queue.
    ctx.lex = lex

    let startspec = this.rsm[this.cfg.rule.start]

    if (null == startspec) {
      return undefined
    }

    let rule = makeRule(startspec, ctx)

    root = rule

    // Maximum rule iterations (prevents infinite loops). Allow for
    // rule open and close, and for each rule on each char to be
    // virtual (like map, list), and double for safety margin (allows
    // lots of backtracking), and apply a multipler option as a get-out-of-jail.
    let rsmCount = 0
    for (let _rn in this.rsm) rsmCount++
    let maxr = 2 * rsmCount * lex.src.length * 2 * ctx.cfg.rule.maxmul

    // Process rules on tokens
    let kI = 0

    // This loop is the heart of the engine. Keep processing rule
    // occurrences until there's none left.
    while (norule !== rule && kI < maxr) {
      ctx.kI = kI
      ctx.rule = rule

      ctx.log && ctx.log(S.step, ctx.kI + ':')

      if (ctx.sub.rule) {
        ctx.sub.rule.map((sub) => sub(rule, ctx))
      }

      rule = rule.process(ctx, lex)

      ctx.log && ctx.log(S.stack, ctx, rule, lex)

      kI++
    }

    // TODO: option to allow trailing content
    const endtry = lex.next(rule)
    if (bdtin === endtry.tin) {
      // A bad token in trailing content carries its own error code
      // (e.g. unterminated_string), matching the old badlex wrapper.
      let details: any = {}
      if (null != endtry.use) {
        details.use = endtry.use
      }
      throw new TabnasError(endtry.why || S.unexpected, details, endtry, rule, ctx)
    }
    if (endtkn.tin !== endtry.tin) {
      throw new TabnasError(S.unexpected, {}, ctx.t0, norule, ctx)
    }

    // NOTE: by returning root, we get implicit closing of maps and lists.
    const result = ctx.root().node

    if (this.cfg.result.fail.includes(result)) {
      throw new TabnasError(S.unexpected, {}, ctx.t0, norule, ctx)
    }

    return result
  }

  clone(options: TabnasOptions, config: Config, j: Tabnas) {
    let parser = new Parser(options, config, j)

    // Inherit rules from parent, filtered by config.rule
    parser.rsm = Object.keys(this.rsm).reduce(
      (a, rn) => ((a[rn] = filterRules(this.rsm[rn], this.cfg)), a),
      {} as any,
    )

    parser.norm()

    return parser
  }

  norm() {
    values(this.rsm).map((rs: RuleSpec) => rs.norm())
  }
}

const makeParser = (...params: ConstructorParameters<typeof Parser>) =>
  new Parser(...params)

export { Parser, makeRule, makeRuleSpec, makeParser }
