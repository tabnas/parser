/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  parser.ts
 *  Parser implementation, converts the lexer tokens into parsed data.
 */

import type {
  Config,
  Options,
  ParsePrepare,
  Rule,
  RuleDefiner,
  RuleSpec,
  RuleSpecMap,
  Amagama,
} from './types'

import { EMPTY } from './types'

import { Context } from './context'

import {
  S,
  badlex,
  deep,
  filterRules,
  keys,
  srcfmt,
  tokenize,
  // log_stack,
  values,
} from './utility'

import { AmagamaError } from './error'

import { makeNoToken, makeLex, makePoint, makeToken } from './lexer'

import { makeRule, makeNoRule, makeRuleSpec } from './rules'


// Top-level rule-driven parser. Holds the rule map, the current
// config, and the Amagama instance the rules belong to. `start()`
// runs a parse from scratch; `clone()` produces a sibling for child
// instances (Amagama#make).
class Parser {
  options: Options
  cfg: Config
  rsm: RuleSpecMap = {}
  ji: Amagama

  constructor(options: Options, cfg: Config, j: Amagama) {
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

      // Ensures amagama.rule can chain
      return undefined
    }

    return rs
  }

  start(src: string, amagama: any, meta?: any, parent_ctx?: any): any {
    let root: Rule

    let endtkn = makeToken(
      '#ZZ',
      tokenize('#ZZ', this.cfg),
      undefined,
      EMPTY,
      makePoint(-1),
    )

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
      plgn: () => amagama.internal().plugins,
      inst: () => amagama,
      sub: amagama.internal().sub,
      rsm: this.rsm,
      F: srcfmt(this.cfg),
      NOTOKEN: notoken,
      NORULE: {} as Rule,
    })

    // Merge in any caller-supplied parent_ctx (plugin tests use this
    // to seed `meta` or other fields). `deep` mutates the class
    // instance in place, so getters/setters and methods survive.
    deep(ctx, parent_ctx)

    let norule = makeNoRule(this.ji, ctx)
    ctx.NORULE = norule
    ctx.rule = norule

    // makelog(ctx, meta)
    if (meta && S.function === typeof meta.log) {
      ctx.log = meta.log
    }

    this.cfg.parse.prepare.forEach((prep: ParsePrepare) =>
      prep(amagama, ctx, meta),
    )

    // Special case - avoids extra per-token tests in main parser rules.
    if ('' === src) {
      if (this.cfg.lex.empty) {
        return this.cfg.lex.emptyResult
      } else {
        throw new AmagamaError(S.unexpected, { src }, ctx.t0, norule, ctx)
      }
    }

    let lex = badlex(makeLex(ctx), tokenize('#BD', this.cfg), ctx)

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
    let maxr =
      2 * keys(this.rsm).length * lex.src.length * 2 * ctx.cfg.rule.maxmul

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
    if (endtkn.tin !== lex.next(rule).tin) {
      throw new AmagamaError(S.unexpected, {}, ctx.t0, norule, ctx)
    }

    // NOTE: by returning root, we get implicit closing of maps and lists.
    const result = ctx.root().node

    if (this.cfg.result.fail.includes(result)) {
      throw new AmagamaError(S.unexpected, {}, ctx.t0, norule, ctx)
    }

    return result
  }

  clone(options: Options, config: Config, j: Amagama) {
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
