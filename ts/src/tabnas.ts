/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  tabnas.ts
 *  The Tabnas class — core parsing engine. The package ships no
 *  grammar of its own: every grammar arrives via a plugin (the BNF
 *  plugin in this repo, plus whatever a consumer brings).
 */

import type {
  AltAction,
  AltCond,
  AltError,
  AltMatch,
  AltModifier,
  AltSpec,
  Config,
  Context,
  Counters,
  EagerRegExp,
  FuncRef,
  GrammarSetting,
  GrammarSpec,
  Lex,
  LexCheck,
  LexMatcher,
  LexSub,
  MakeLexMatcher,
  NormAltSpec,
  TabnasOptions,
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

import { BUILTIN_REFS, BUILTIN_SCHEMA_VERSION } from './builtins'

import {
  TabnasError,
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
  // Lex scan primitives — re-exposed via util for plugin authors.
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
} from './lexer'

import { makeParser, makeRule, makeRuleSpec } from './parser'


// Utility bag re-exported on Tabnas.util for plugin convenience.
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

  // Lex scan primitives. Plugin authors writing custom matchers can
  // reuse the same state-machine driver and spec builders the core
  // matchers use. See the matchers in src/lexer.ts for examples.
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


// Internal state held by every Tabnas instance.
type Internal = {
  parser:  Parser                                // Live parser instance.
  config:  Config                                // Resolved configuration.
  plugins: Plugin[]                              // Plugins applied, in order.
  sub:     { lex?: LexSub[]; rule?: RuleSub[] }  // Event subscribers, by kind.
  mark:    number                                // Random per-instance stamp.
  merged:  Record<string, any>                   // Merged option tree.
}


// Construction options now live in types.ts as TabnasOptions —
// including the optional `plugins` array. Nothing extra is added here.


// Core parsing engine; grammar arrives only via plugins.
class Tabnas {
  // Index signature exposes plugin-attached properties and methods to TS.
  [key: string]: any

  // Public API surface — see types.ts for documentation.
  token!: ((ref: string | Tin) => any) & { [k: string]: any }    // Token lookup/create, also a map.
  tokenSet!: ((ref: string | Tin) => any) & { [k: string]: any } // Token-set lookup, also a map.
  fixed!: ((ref: string | Tin) => any) & { [k: string]: any }    // Fixed-token lookup, also a map.
  // `options` is both callable (set/get) and an indexable map of the merged
  // option tree: read settings via `tn.options.<name>`, apply via `tn.options({...})`.
  options!: ((change?: Record<string, any>) => Record<string, any>) & Record<string, any>
  id!: string                                                    // Stamped per-instance identifier.
  parent?: Tabnas                                                // Parent instance, if forked.

  // Hash-private internal state, invisible to for...in / Object.keys /
  // JSON.stringify / tests. Read it through the public `internal()` method.
  #internal!: Internal

  // Static utility / constants for plugin code that holds the class.
  static util = util      // Shared utility bag.
  static S = S            // Interned string constants.
  static OPEN = OPEN      // Rule-state: open phase.
  static CLOSE = CLOSE    // Rule-state: close phase.
  static BEFORE = BEFORE  // Rule-step: before the match.
  static AFTER = AFTER    // Rule-step: after the match.
  static EMPTY = EMPTY    // Empty-string constant.
  static SKIP = SKIP      // Skip marker (Symbol).


  constructor(options?: TabnasOptions, parent?: Tabnas) {
    let plugins: Plugin[] = []
    let opts: TabnasOptions = {}

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
      'Tabnas/' +
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
    const optionsFn = ((change?: Record<string, any>): Record<string, any> => {
      return this.#setOptions(change)
    }) as ((change?: Record<string, any>) => Record<string, any>) & Record<string, any>
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
      // strip (e.g. `make({ rule: { exclude: 'tabnas' } })`).
      const rsm: RuleSpecMap = internal.parser.rule() as RuleSpecMap
      const filtered: RuleSpecMap = {}
      for (const rn of Object.keys(rsm)) {
        filtered[rn] = filterRules(rsm[rn], internal.config) as RuleSpec
      }
      ; (internal.parser as any).rsm = filtered
        ; (internal.parser as any).norm()
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
  #setOptions(change?: Record<string, any>): Record<string, any> {
    if (null != change) {
      deep(this.#internal.merged, change)
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


  // Register and apply a plugin. Plugin is `(tn, opts) => void | tn`.
  // If the plugin returns a Tabnas-like value (e.g. a Proxy wrapping
  // the instance), that's what `use()` returns — matches the upstream
  // contract and lets plugins decorate or wrap the instance.
  use(plugin: Plugin, plugin_options?: Record<string, any>): Tabnas {
    if (S.function !== typeof plugin) {
      throw new Error(
        'Tabnas.use: the first argument must be a function ' +
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

    return (plugin(this, merged_plugin_options) || this) as Tabnas
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
  make(options?: TabnasOptions): Tabnas {
    return new Tabnas(options, this)
  }


  // Create a fresh standalone instance (no parent) with no defaults, no
  // standard tokens, and no grammar — for tests and for plugins that build
  // everything from scratch.
  empty(options?: TabnasOptions): Tabnas {
    return new Tabnas({
      defaults$: false,
      standard$: false,
      grammar$: false,
      ...(options || {}),
    } as TabnasOptions)
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
  grammar(gs: GrammarSpec, setting?: GrammarSetting): this {

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

    // Refuse a grammar that requires a newer builtin config-schema than
    // this engine implements (absent ⇒ current). The version field is a
    // forward-compatibility hatch for the `$`-builtin wire format, so it
    // must be a well-formed positive integer (a relational compare alone
    // would silently accept a malformed/NaN/string `v`).
    if (null != gs.v) {
      if ('number' !== typeof gs.v || !Number.isInteger(gs.v) || gs.v < 1) {
        throw new Error(
          `Grammar: invalid builtin schema version: ${gs.v} ` +
          `(expected a positive integer)`)
      }
      if (gs.v > BUILTIN_SCHEMA_VERSION) {
        throw new Error(
          `Grammar: requires builtin schema version ${gs.v}, but this ` +
          `engine supports up to ${BUILTIN_SCHEMA_VERSION}`)
      }
    }

    // The `$` ref-namespace is reserved for engine builtins; a user
    // ref key may not contain `$` (it would silently shadow, or be
    // shadowed by, a builtin in the merge below).
    if (gs.ref) {
      for (const key of Object.keys(gs.ref)) {
        if (key.includes('$')) {
          throw new Error(
            `Grammar: '$' is reserved for engine builtins; user ref ` +
            `key '${key}' may not contain '$'`)
        }
      }
    }

    // Merge the standard `$`-suffixed builtins UNDER any spec-supplied
    // refs (the spec wins on collision, though `$` is reserved above).
    // This lets a serialized, function-free GrammarSpec reference engine
    // builtins (e.g. `@probeInit$`) by name. See builtins.ts.
    const ref: Record<string, any> =
      Object.assign(Object.create(null), BUILTIN_REFS, gs.ref || {})

    if (gs.options) {
      const resolved = resolveFuncRefs(gs.options, ref)
      this.options(resolved)
    }

    if (gs.rule) {
      for (const rulename of Object.keys(gs.rule)) {
        const rulespec = gs.rule[rulename]
        this.rule(rulename, (rs: RuleSpec) => {
          rs.fnref(ref)
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
  Config,
  Context,
  Counters,
  EagerRegExp,
  FuncRef,
  GrammarSetting,
  GrammarSpec,
  Lex,
  LexCheck,
  LexMatcher,
  LexSub,
  MakeLexMatcher,
  NormAltSpec,
  TabnasOptions,
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

export type { ScanSpec, ScanOut } from './lexer'

export type { BuiltinRef } from './builtins'

export {
  Tabnas,
  TabnasError,
  BUILTIN_REFS,
  BUILTIN_SCHEMA_VERSION,
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
