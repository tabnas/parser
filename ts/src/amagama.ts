/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  amagama.ts
 *  The Amagama class — core parsing engine. Grammar is provided by
 *  separate plugins (see src/plugins/json.ts and src/plugins/jsonic.ts).
 */

import type {
  AltAction,
  AltCond,
  AltError,
  AltMatch,
  AltModifier,
  AltSpec,
  BnfConvertOptions,
  Config,
  Context,
  Counters,
  FuncRef,
  GrammarSetting,
  GrammarSpec,
  Lex,
  LexCheck,
  LexMatcher,
  LexSub,
  MakeLexMatcher,
  NormAltSpec,
  AmagamaOptions,
  Parser,
  Plugin,
  Point,
  Rule,
  RuleDefiner,
  RuleSpec,
  RuleSpecMap,
  RuleState,
  RuleSub,
  StateAction,
  Tin,
  Token,
} from './types'

import { OPEN, CLOSE, BEFORE, AFTER, EMPTY, SKIP } from './types'

import {
  S,
  assign,
  badlex,
  charset,
  clean,
  clone,
  configure,
  deep,
  defprop,
  entries,
  escre,
  filterRules,
  findTokenSet,
  keys,
  makelog,
  mesc,
  omap,
  parserwrap,
  regexp,
  resolveFuncRefs,
  srcfmt,
  str,
  tokenize,
  values,
} from './utility'

import {
  AmagamaError,
  errdesc,
  errinject,
  errmsg,
  errsite,
  prop,
  strinject,
  trimstk,
} from './error'

import { defaults } from './defaults'

import {
  makeCommentMatcher,
  makeFixedMatcher,
  makeLex,
  makeLineMatcher,
  makeNumberMatcher,
  makePoint,
  makeSpaceMatcher,
  makeStringMatcher,
  makeTextMatcher,
  makeToken,
} from './lexer'

import { makeParser, makeRule, makeRuleSpec } from './parser'


// Utility bag re-exported on Amagama.util for plugin convenience.
const util: Record<string, any> = {
  badlex,
  charset,
  clean,
  clone,
  configure,
  deep,
  entries,
  errdesc,
  errinject,
  errmsg,
  errsite,
  escre,
  keys,
  makelog,
  mesc,
  omap,
  parserwrap,
  prop,
  regexp,
  srcfmt,
  str,
  strinject,
  tokenize,
  trimstk,
  values,
}


// Internal state held by every Amagama instance.
type Internal = {
  parser: Parser
  config: Config
  plugins: Plugin[]
  sub: { lex?: LexSub[]; rule?: RuleSub[] }
  mark: number
  merged: Record<string, any>
}


// Construction options now live in types.ts as AmagamaOptions —
// including the optional `plugins` array. Nothing extra is added here.


class Amagama {
  // Methods like parse/use/rule are declared with the class. Plugins may
  // attach extra properties; the index signature exposes that to TS.
  [key: string]: any

  // Public API surface — see types.ts for documentation.
  token!: ((ref: string | Tin) => any) & { [k: string]: any }
  tokenSet!: ((ref: string | Tin) => any) & { [k: string]: any }
  fixed!: ((ref: string | Tin) => any) & { [k: string]: any }
  // `options` is both a callable (set/get) and an indexable map of the
  // merged option tree. Plugins may read individual settings via
  // `am.options.<name>` and apply changes via `am.options({ ... })`.
  options!: ((change?: Record<string, any> | string) => Record<string, any>) & Record<string, any>
  id!: string
  parent?: Amagama

  // Truly-private (ECMAScript hash-private) internal state. Inaccessible
  // outside the class — for...in, Object.keys, JSON.stringify, and
  // tests all see the instance as if this field didn't exist. Read it
  // through the public `internal()` method.
  #internal!: Internal

  // Static utility / constants for plugin code that holds the class.
  static util = util
  static S = S
  static OPEN = OPEN
  static CLOSE = CLOSE
  static BEFORE = BEFORE
  static AFTER = AFTER
  static EMPTY = EMPTY
  static SKIP = SKIP


  constructor(options?: AmagamaOptions, parent?: Amagama) {
    let plugins: Plugin[] = []
    let opts: AmagamaOptions = {}

    if (options) {
      if (Array.isArray((options as any).plugins)) {
        plugins = (options as any).plugins
        const { plugins: _ignored, ...rest } = options as any
        opts = rest
      } else {
        opts = options
      }
    }

    this.parent = parent

    const internal: Internal = {
      parser: undefined as unknown as Parser,
      config: undefined as unknown as Config,
      plugins: [],
      sub: { lex: undefined, rule: undefined },
      mark: Math.random(),
      merged: undefined as unknown as Record<string, any>,
    }
    this.#internal = internal

    const merged_options = deep(
      {},
      parent
        ? { ...parent.#internal.merged }
        : false === (opts as any).defaults$
          ? {}
          : defaults,
      opts || {},
    )
    internal.merged = merged_options

    // Stamped identifier (carries through child instances via tag).
    this.id =
      'Amagama/' +
      Date.now() +
      '/' +
      ('' + Math.random()).substring(2, 8).padEnd(6, '0') +
      (null == merged_options.tag ? '' : '/' + merged_options.tag)

    // token / tokenSet / fixed are dual-shape: callable for lookup-or-create
    // and indexable as a map. The map portion is filled by configure().
    this.token = ((ref: string | Tin) =>
      internal.config.fixed.token[ref as any] ??
      tokenize(ref, internal.config, this)) as any

    this.tokenSet = ((ref: string | Tin) =>
      findTokenSet(ref, internal.config)) as any

    this.fixed = ((ref: string | Tin) =>
      internal.config.fixed.ref[ref as any]) as any

    // Build a callable+indexable `options` member up front so use()
    // and any plugin code below can rely on `this.options` already
    // existing and working.
    const optionsFn = ((change?: Record<string, any> | string): Record<string, any> => {
      return this.#setOptions(change)
    }) as ((change?: Record<string, any> | string) => Record<string, any>) & Record<string, any>
    deep(optionsFn, internal.merged)
    defprop(this, 'options', {
      value: optionsFn,
      writable: true,
      enumerable: true,
      configurable: true,
    })

    if (parent) {
      // Inherit config + carry parent properties (plugin decorations
      // etc), build a fresh parser, then re-run parent plugins on this
      // instance so option-conditional rule alts (e.g. `list.child`)
      // get re-evaluated against the child's merged options.
      const parentInternal = parent.#internal
      internal.config = configure(this, undefined, merged_options)
      assign(this.token, internal.config.t)

      for (const k of Object.keys(parent)) {
        if (undefined === (this as any)[k]) {
          (this as any)[k] = (parent as any)[k]
        }
      }

      internal.parser = makeParser(merged_options, internal.config, this)
      const inherited = parentInternal.plugins
      internal.plugins = []
      for (const plugin of inherited) {
        this.use(plugin)
      }
      // After plugins re-register their rules with the child's
      // options, apply rule.include / rule.exclude filtering. The
      // alts we re-evaluated may include groups the user wanted to
      // strip (e.g. `make({ rule: { exclude: 'amagama' } })`).
      const rsm: RuleSpecMap = internal.parser.rule() as RuleSpecMap
      const filtered: RuleSpecMap = {}
      for (const rn of Object.keys(rsm)) {
        filtered[rn] = filterRules(rsm[rn], internal.config) as RuleSpec
      }
      ;(internal.parser as any).rsm = filtered
      ;(internal.parser as any).norm()
    } else {
      internal.config = configure(this, undefined, merged_options)
      internal.parser = makeParser(merged_options, internal.config, this)
      assign(this.token, internal.config.t)
    }

    for (const plugin of plugins) {
      this.use(plugin)
    }
  }


  // Hash-private options setter. Public callers go through `options(change)`.
  #setOptions(change?: Record<string, any> | string): Record<string, any> {
    if (null != change) {
      let actualChange: Record<string, any> | undefined
      if ('string' === typeof change) {
        // Lazy-parse via a fresh jsonic instance.
        // eslint-disable-next-line @typescript-eslint/no-var-requires
        const { jsonic } = require('./plugins/jsonic') as { jsonic: Plugin }
        const tmp = new Amagama({ plugins: [jsonic] })
        const parsed = tmp.parse(change)
        if (null != parsed && 'object' === typeof parsed) {
          actualChange = parsed as Record<string, any>
        }
      } else {
        actualChange = change
      }

      if (null != actualChange) {
        deep(this.#internal.merged, actualChange)
        configure(this, this.#internal.config, this.#internal.merged)
        this.#internal.parser = this.#internal.parser.clone(
          this.#internal.merged,
          this.#internal.config,
          this,
        )
        // Refresh the indexable view on `options` so subsequent
        // property reads see the latest merged tree.
        deep(this.options, this.#internal.merged)
      }
    }
    return { ...this.#internal.merged }
  }


  // Parse `src` and return the resulting JS value. Strings are parsed;
  // non-string inputs are returned as-is (matches the upstream contract).
  parse(src: any, meta?: any, parent_ctx?: any): any {
    if (S.string === typeof src) {
      const internalParser = this.#internal.parser
      const optsParser: any = (this.#internal.merged as any).parser
      const parser = optsParser?.start
        ? parserwrap(optsParser)
        : internalParser
      return parser.start(src, this, meta, parent_ctx)
    }
    return src
  }


  config(): Config {
    return deep(this.#internal.config)
  }


  // Register and apply a plugin. Plugin is `(am, opts) => void | am`.
  // If the plugin returns an Amagama-like value (e.g. a Proxy wrapping
  // the instance), that's what `use()` returns — matches the upstream
  // contract and lets plugins decorate or wrap the instance.
  use(plugin: Plugin, plugin_options?: Record<string, any>): Amagama {
    if (S.function !== typeof plugin) {
      throw new Error(
        'Amagama.use: the first argument must be a function ' +
          'defining a plugin.',
      )
    }

    const plugin_name = plugin.name.toLowerCase()
    const full_options = deep(
      {},
      plugin.defaults || {},
      plugin_options || {},
    )

    this.options({
      plugin: {
        [plugin_name]: full_options,
      },
    })

    const merged_plugin_options =
      (this.#internal.merged as any).plugin[plugin_name]
    this.#internal.plugins.push(plugin)
    plugin.options = merged_plugin_options

    return (plugin(this, merged_plugin_options) || this) as Amagama
  }


  // Get the rule map (no args), get/define a rule by name, or delete a
  // rule (define === null).
  rule(
    name?: string,
    define?: RuleDefiner | null,
  ): RuleSpec | RuleSpecMap | this | undefined {
    const result = this.#internal.parser.rule(name, define)
    return result === undefined ? this : result
  }


  // Create a child instance that inherits config, plugins, and rules
  // from this instance. Use to fork and customize without touching the
  // parent.
  make(options?: AmagamaOptions): Amagama {
    return new Amagama(options, this)
  }


  // Create a sibling instance with no defaults, no standard tokens, and
  // no grammar — for tests and for plugins that build everything from
  // scratch.
  empty(options?: AmagamaOptions): Amagama {
    return new Amagama({
      defaults$: false,
      standard$: false,
      grammar$: false,
      ...(options || {}),
    } as AmagamaOptions)
  }


  toString(): string {
    return this.id
  }


  // Subscribe to lexer / rule events. Multiple subscriptions are allowed
  // and fire in registration order.
  sub(spec: { lex?: any; rule?: any }): this {
    if (spec.lex) {
      this.#internal.sub.lex = this.#internal.sub.lex || []
      this.#internal.sub.lex.push(spec.lex)
    }
    if (spec.rule) {
      this.#internal.sub.rule = this.#internal.sub.rule || []
      this.#internal.sub.rule.push(spec.rule)
    }
    return this
  }


  // Internal accessor used by parser, plugins, and debug code.
  internal(): Internal {
    return this.#internal
  }


  // Apply a GrammarSpec (declarative rule definition) to this instance.
  grammar(gsIn: GrammarSpec | string, setting?: GrammarSetting): this {
    let gs: GrammarSpec
    if ('string' === typeof gsIn) {
      const tmp = new Amagama()
      // Lazy require to avoid circular import; jsonic plugin is needed
      // to parse the grammar string itself.
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      const { jsonic } = require('./plugins/jsonic') as { jsonic: Plugin }
      tmp.use(jsonic)
      const parsed = tmp.parse(gsIn)
      if (null == parsed || 'object' !== typeof parsed) {
        return this
      }
      gs = parsed as GrammarSpec
    } else {
      gs = gsIn
    }

    const altG = setting?.rule?.alt?.g
    const altGArr: string[] | null =
      null == altG
        ? null
        : Array.isArray(altG)
          ? [...altG]
          : String(altG)
              .split(/\s*,\s*/)
              .filter((s) => s.length > 0)

    const applyG = (alts: any): any => {
      if (null == altGArr || 0 === altGArr.length || !Array.isArray(alts)) {
        return alts
      }
      return alts.map((a: any) => {
        if (null == a || S.object !== typeof a) return a
        const existing: string[] =
          null == a.g
            ? []
            : Array.isArray(a.g)
              ? [...a.g]
              : String(a.g)
                  .split(/\s*,\s*/)
                  .filter((s: string) => s.length > 0)
        return { ...a, g: [...existing, ...altGArr] }
      })
    }

    if (gs.options) {
      const resolved = resolveFuncRefs(gs.options, gs.ref)
      this.options(resolved)
    }

    if (gs.rule) {
      for (const rulename of Object.keys(gs.rule)) {
        const rulespec = gs.rule[rulename]
        this.rule(rulename, (rs: RuleSpec) => {
          if (gs.ref) {
            rs.fnref(gs.ref)
          }
          if (rulespec.open) {
            const isarr = Array.isArray(rulespec.open)
            const alts = isarr
              ? rulespec.open
              : (rulespec.open as any).alts
            const inject = isarr ? {} : (rulespec.open as any).inject
            rs.open(applyG(alts), inject)
          }
          if (rulespec.close) {
            const isarr = Array.isArray(rulespec.close)
            const alts = isarr
              ? rulespec.close
              : (rulespec.close as any).alts
            const inject = isarr ? {} : (rulespec.close as any).inject
            rs.close(applyG(alts), inject)
          }
        })
      }
    }
    return this
  }


  // Convenience: util bag accessible per-instance too.
  get util(): Record<string, any> {
    return util
  }
}


// Re-export everything plugins might need.
export type {
  AltAction,
  AltCond,
  AltError,
  AltMatch,
  AltModifier,
  AltSpec,
  BnfConvertOptions,
  Config,
  Context,
  Counters,
  FuncRef,
  GrammarSetting,
  GrammarSpec,
  Lex,
  LexCheck,
  LexMatcher,
  LexSub,
  MakeLexMatcher,
  NormAltSpec,
  AmagamaOptions,
  Parser,
  Plugin,
  Point,
  Rule,
  RuleDefiner,
  RuleSpec,
  RuleSpecMap,
  RuleState,
  RuleSub,
  StateAction,
  Tin,
  Token,
}

export {
  Amagama,
  AmagamaError,
  OPEN,
  CLOSE,
  BEFORE,
  AFTER,
  EMPTY,
  SKIP,
  S,
  util,
  makeCommentMatcher,
  makeFixedMatcher,
  makeLex,
  makeLineMatcher,
  makeNumberMatcher,
  makeParser,
  makePoint,
  makeRule,
  makeRuleSpec,
  makeSpaceMatcher,
  makeStringMatcher,
  makeTextMatcher,
  makeToken,
}

// Re-export the json + jsonic plugins for ergonomic usage:
//   const { Amagama, json, jsonic } = require('amagama')
// Plugins are loaded from sibling folders so callers can do
// `const { Amagama, jsonic } = require('amagama')`.
export { json } from './plugins/json'
export { jsonic } from './plugins/jsonic'
export { bnf } from './plugins/bnf'
export { Debug } from './plugins/debug'
