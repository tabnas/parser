/* Copyright (c) 2013-2023 Richard Rodger, MIT License */

/*  amagama.ts
 *  Entry point and API.
 */

import type {
  AltAction,
  AltCond,
  AltError,
  AltMatch,
  AltModifier,
  AltSpec,
  Bag,
  Config,
  Context,
  Counters,
  FuncRef,
  AmagamaAPI,
  AmagamaParse,
  Lex,
  LexCheck,
  LexMatcher,
  LexSub,
  MakeLexMatcher,
  NormAltSpec,
  Options,
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
  GrammarSpec,
  GrammarSetting,
} from './types'

import { OPEN, CLOSE, BEFORE, AFTER, EMPTY, SKIP } from './types'

import {
  S,
  assign,
  badlex,
  deep,
  defprop,
  makelog,
  mesc,
  regexp,
  tokenize,
  findTokenSet,
  srcfmt,
  clone,
  charset,
  configure,
  escre,
  parserwrap,
  str,
  clean,

  resolveFuncRefs,

  // Exported with amagama.util
  omap,
  entries,
  values,
  keys,
} from './utility'

import {
  AmagamaError,
  errdesc,
  errinject,
  errsite,
  errmsg,
  trimstk,
  strinject,
  prop,
} from './error'

import { defaults } from './defaults'

import {
  makePoint,
  makeToken,
  makeLex,
  makeFixedMatcher,
  makeSpaceMatcher,
  makeLineMatcher,
  makeStringMatcher,
  makeCommentMatcher,
  makeNumberMatcher,
  makeTextMatcher,
} from './lexer'

import { makeRule, makeRuleSpec, makeParser } from './parser'

import { grammar, makeJSON } from './grammar'

import { bnf as bnfConvert } from './bnf'
import type { BnfConvertOptions } from './types'

// TODO: remove - too much for an API!
const util = {
  tokenize,
  srcfmt,
  clone,
  charset,
  trimstk,
  makelog,
  badlex,
  errsite,
  errinject,
  errdesc,
  configure,
  parserwrap,
  mesc,
  escre,
  regexp,
  prop,
  str,
  clean,
  errmsg,
  strinject,

  // TODO: validated to include in util API:
  deep,
  omap,
  keys,
  values,
  entries,
}

// The full library type.
// NOTE: redeclared here so it can be exported as a type and instance.
type Amagama = AmagamaParse & // A function that parses.
  AmagamaAPI & { [prop: string]: any } // A utility with API methods. // Extensible by plugin decoration.

function make(param_options?: Bag | string, parent?: Amagama): Amagama {
  let injectFullAPI = true
  if ('amagama' === param_options) {
    injectFullAPI = false
  } else if ('json' === param_options) {
    return makeJSON(root)
  }

  param_options = 'string' === typeof param_options ? {} : param_options

  let internal: {
    parser: Parser
    config: Config
    plugins: Plugin[]
    sub: {
      lex?: LexSub[]
      rule?: RuleSub[]
    }
    mark: number
  } = {
    parser: null as unknown as Parser,
    config: null as unknown as Config,
    plugins: [],
    sub: {
      lex: undefined,
      rule: undefined,
    },
    mark: Math.random(),
  }

  // Merge options.
  let merged_options = deep(
    {},
    parent
      ? { ...parent.options }
      : false === (param_options as Bag)?.defaults$
        ? {}
        : defaults,
    param_options ? param_options : {},
  )

  // Create primary parsing function
  let amagama: any = function Amagama(
    src: any,
    meta?: any,
    parent_ctx?: any,
  ): any {
    if (S.string === typeof src) {
      let internal = amagama.internal()
      let parser = optionsMethod.parser?.start
        ? parserwrap(optionsMethod.parser)
        : internal.parser
      return parser.start(src, amagama, meta, parent_ctx)
    }

    return src
  }

  // This lets you access options as direct properties,
  // and set them as a function call.
  // `change_options` can be a Bag object or a amagama-format string that
  // is parsed into a Bag before applying.
  let optionsMethod: any = (change_options?: Bag | string) => {
    if (null != change_options) {
      if (S.string === typeof change_options) {
        const parsed = make()(change_options as string)
        change_options =
          null != parsed && S.object === typeof parsed
            ? (parsed as Bag)
            : undefined
      }
      if (null != change_options && S.object === typeof change_options) {
        deep(merged_options, change_options)
        configure(amagama, internal.config, merged_options)
        let parser: Parser = amagama.internal().parser
        internal.parser = parser.clone(merged_options, internal.config, amagama)
      }
    }
    return { ...amagama.options }
  }

  // Define the API
  let api: AmagamaAPI = {
    token: ((ref: string | Tin) =>
      internal.config.fixed.token[ref] ??
      tokenize(ref, internal.config, amagama)) as unknown as AmagamaAPI['token'],

    tokenSet: ((ref: string | Tin) =>
      findTokenSet(ref, internal.config)) as unknown as AmagamaAPI['tokenSet'],

    fixed: ((ref: string | Tin) =>
      internal.config.fixed.ref[ref]) as unknown as AmagamaAPI['fixed'],

    options: deep(optionsMethod, merged_options),

    config: () => deep(internal.config),

    parse: amagama,

    // TODO: how to handle null plugin?
    use: function use(plugin: Plugin, plugin_options?: Bag): Amagama {
      if (S.function !== typeof plugin) {
        throw new Error(
          'Amagama.use: the first argument must be a function ' +
          'defining a plugin. See https://amagama.senecajs.org/plugin',
        )
      }

      // Plugin name keys in options.plugin are the lower-cased plugin function name.
      const plugin_name = plugin.name.toLowerCase()
      const full_plugin_options = deep(
        {},
        plugin.defaults || {},
        plugin_options || {},
      )

      amagama.options({
        plugin: {
          [plugin_name]: full_plugin_options,
        },
      })
      let merged_plugin_options = amagama.options.plugin[plugin_name]
      amagama.internal().plugins.push(plugin)
      plugin.options = merged_plugin_options

      return plugin(amagama, merged_plugin_options) || amagama
    },

    rule: (name?: string, define?: RuleDefiner | null) => {
      return (amagama.internal().parser as Parser).rule(name, define) || amagama
    },

    make: (options?: Options | string) => {
      return make(options, amagama)
    },

    empty: (options?: Options) =>
      make({
        defaults$: false,
        standard$: false,
        grammar$: false,
        ...(options || {}),
      }),

    id:
      'Amagama/' +
      Date.now() +
      '/' +
      ('' + Math.random()).substring(2, 8).padEnd(6, '0') +
      (null == optionsMethod.tag ? '' : '/' + optionsMethod.tag),

    toString: () => {
      return api.id
    },

    sub: (spec: { lex?: LexSub; rule?: RuleSub }) => {
      if (spec.lex) {
        internal.sub.lex = internal.sub.lex || []
        internal.sub.lex.push(spec.lex)
      }
      if (spec.rule) {
        internal.sub.rule = internal.sub.rule || []
        internal.sub.rule.push(spec.rule)
      }
      return amagama
    },

    util,


    grammar: (gs: GrammarSpec | string, setting?: GrammarSetting) => {
      if ('string' === typeof gs) {
        const parsed = make()(gs)
        if (null == parsed || 'object' !== typeof parsed) {
          return
        }
        gs = parsed as GrammarSpec
      }

      // Normalize the optional setting's rule.alt.g value to a string[] once.
      const altG = setting?.rule?.alt?.g
      const altGArr: string[] | null =
        null == altG
          ? null
          : Array.isArray(altG)
            ? [...altG]
            : String(altG).split(/\s*,\s*/).filter((s) => s.length > 0)

      // Append altGArr tags to each alt's g field without mutating the input alt.
      const applyG = (alts: any): any => {
        if (null == altGArr || 0 === altGArr.length || !Array.isArray(alts)) {
          return alts
        }
        return alts.map((a: any) => {
          if (null == a || 'object' !== typeof a) return a
          const existing: string[] =
            null == a.g
              ? []
              : Array.isArray(a.g)
                ? [...a.g]
                : String(a.g).split(/\s*,\s*/).filter((s) => s.length > 0)
          return { ...a, g: [...existing, ...altGArr] }
        })
      }

      if (gs.options) {
        const resolved = resolveFuncRefs(gs.options, gs.ref)
        ji.options(resolved)
      }

      if (gs.rule) {
        for (const rulename of Object.keys(gs.rule)) {
          const rulespec = gs.rule[rulename]
          ji.rule(rulename, (rs: RuleSpec) => {

            if (gs.ref) {
              rs.fnref(gs.ref)
            }

            if (rulespec.open) {
              const isarr = Array.isArray(rulespec.open)
              const alts = isarr ? rulespec.open : (rulespec.open as any).alts
              const inject = isarr ? {} : (rulespec.open as any).inject
              rs.open(applyG(alts), inject)
            }

            if (rulespec.close) {
              const isarr = Array.isArray(rulespec.close)
              const alts = isarr ? rulespec.close : (rulespec.close as any).alts
              const inject = isarr ? {} : (rulespec.close as any).inject
              rs.close(applyG(alts), inject)
            }

          })
        }
      }
    },


    // Convert a BNF grammar string into a amagama GrammarSpec and install
    // it on this instance. Returns the generated spec so callers can
    // inspect, serialise or diff it. Use `bnf.toSpec(src, opts)` to
    // build the spec without installing it.
    bnf: (() => {
      const impl = (src: string, opts?: BnfConvertOptions) => {
        const spec = bnfConvert(src, opts)
        ji.grammar(spec)
        return spec
      }
      impl.toSpec = (src: string, opts?: BnfConvertOptions) =>
        bnfConvert(src, opts)
      return impl
    })(),

  }

  // Has to be done indirectly as we are in a fuction named `make`.
  defprop(api.make, S.name, { value: S.make })

  let ji = amagama
  if (injectFullAPI) {
    // Add API methods to the core utility function.
    assign(amagama, api)
  } else {
    assign(amagama, {
      empty: api.empty,
      parse: api.parse,
      sub: api.sub,
      id: api.id,
      toString: api.toString,
    })
    ji = assign(Object.create(amagama), api)
  }

  // Hide internals where you can still find them.
  defprop(amagama, 'internal', { value: () => internal })

  if (parent) {
    // Transfer extra parent properties (preserves plugin decorations, etc).
    for (let k in parent) {
      if (undefined === amagama[k]) {
        amagama[k] = parent[k]
      }
    }

    amagama.parent = parent

    let parent_internal = parent.internal()
    internal.config = deep({}, parent_internal.config)

    configure(amagama, internal.config, merged_options)
    assign(amagama.token, internal.config.t)

    internal.plugins = [...parent_internal.plugins]
    internal.parser = parent_internal.parser.clone(
      merged_options,
      internal.config,
      ji,
    )
  }
  else {
    let rootWithAPI = { ...amagama, ...api }
    internal.config = configure(rootWithAPI, undefined, merged_options)
    internal.plugins = []
    internal.parser = makeParser(merged_options, internal.config, ji)

    if (false !== merged_options.grammar$) {
      grammar(rootWithAPI)
    }
  }

  return amagama
}


let root: any = undefined

// The global root Amagama instance parsing rules cannot be modified.
// use Amagama.make() to create a modifiable instance.
let Amagama: Amagama = (root = make('amagama'))

// Provide deconstruction export names
root.Amagama = root
root.AmagamaError = AmagamaError
root.makeLex = makeLex
root.makeParser = makeParser
root.makeToken = makeToken
root.makePoint = makePoint
root.makeRule = makeRule
root.makeRuleSpec = makeRuleSpec
root.makeFixedMatcher = makeFixedMatcher
root.makeSpaceMatcher = makeSpaceMatcher
root.makeLineMatcher = makeLineMatcher
root.makeStringMatcher = makeStringMatcher
root.makeCommentMatcher = makeCommentMatcher
root.makeNumberMatcher = makeNumberMatcher
root.makeTextMatcher = makeTextMatcher
root.OPEN = OPEN
root.CLOSE = CLOSE
root.BEFORE = BEFORE
root.AFTER = AFTER
root.EMPTY = EMPTY
root.SKIP = SKIP

root.util = util
root.make = make
root.S = S

// Export most of the types for use by plugins.
export type {
  AltAction,
  AltCond,
  AltError,
  AltMatch,
  AltModifier,
  AltSpec,
  Bag,
  Config,
  Context,
  Counters,
  FuncRef,
  Lex,
  LexCheck,
  LexMatcher,
  MakeLexMatcher,
  NormAltSpec,
  Options,
  Plugin,
  Point,
  Rule,
  RuleDefiner,
  RuleSpec,
  RuleSpecMap,
  RuleState,
  StateAction,
  Tin,
  Token,
}

export {
  // Amagama is both a type and a value.
  Amagama as Amagama,
  AmagamaError,
  Parser,
  util,
  make,
  makeToken,
  makePoint,
  makeRule,
  makeRuleSpec,
  makeLex,
  makeParser,
  makeFixedMatcher,
  makeSpaceMatcher,
  makeLineMatcher,
  makeStringMatcher,
  makeCommentMatcher,
  makeNumberMatcher,
  makeTextMatcher,
  OPEN,
  CLOSE,
  BEFORE,
  AFTER,
  EMPTY,
  SKIP,
  S,
  root,
}

export default Amagama

if ('undefined' !== typeof module) {
  module.exports = Amagama
}
