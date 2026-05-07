/* Copyright (c) 2021-2022 Richard Rodger, MIT License */

/*  types.ts
 *  Type and constant definitions.
 */

export const OPEN: RuleState = 'o'
export const CLOSE: RuleState = 'c'
export const BEFORE: RuleStep = 'b'
export const AFTER: RuleStep = 'a'
export const EMPTY = ''
export const INSPECT = Symbol.for('nodejs.util.inspect.custom')

// Sentinel value that acts as `undefined` in deep merge — the base value
// is preserved.  Represented as "@SKIP" in grammar options.
export const SKIP: unique symbol = Symbol.for('amagama.SKIP')

// Empty rule used as a no-value placeholder.
// export const NONE = ({ name: 'none', state: OPEN } as Rule)

export const STRING = 'string'

// Parse function signature. Plugins occasionally type a parse callback
// without holding a class instance; export the shape for them.
export type AmagamaParse = (src: any, meta?: any, parent_ctx?: any) => any

// BNF converter options. Re-declared here rather than imported to keep
// types.ts free of circular cross-file references.
export type BnfConvertOptions = {
  start?: string
  tag?: string
}


// Additional settings applied when processing a grammar spec.
// If `rule.alt.g` is defined, its value(s) are appended to every
// rule-alt `g` property in the grammar before the alts are installed.
export type GrammarSetting = {
  rule?: {
    alt?: {
      g?: string | string[]
    }
  }
}


// Internal state held by every Amagama instance. Exposed via
// `instance.internal()` for parser, plugins, and debug code.
export type AmagamaInternal = {
  parser: Parser
  config: Config
  plugins: Plugin[]
  sub: { lex?: LexSub[]; rule?: RuleSub[] }
  mark: number
  merged: Bag
}


// Amagama is a runtime class defined in src/amagama.ts. The type-only
// re-export here lets every other type definition in this file (and
// the rest of the codebase that imports from `./types`) reference
// `Amagama` as a type without pulling the class file in directly.
import type { Amagama } from './amagama'
export type { Amagama }


// Define a plugin to extend the provided Amagama instance.
export type Plugin = ((
  amagama: Amagama,
  plugin_options?: any,
) => void | Amagama) & {
  defaults?: Bag
  options?: Bag // TODO: InstalledPlugin.options is always defined ?
}

// Parsing options. See defaults.ts for commentary on individual fields.
//
// This is the canonical option shape passed to `new Amagama(...)` and
// `am.make(...)`. It also covers the result of `am.options()` and the
// argument to `am.options(change)`.
export type AmagamaOptions = {
  // Plugins to apply at construction time. `new Amagama({ plugins:
  // [jsonic] })` is sugar for `am.use(jsonic)` after construction —
  // children inherit the parent's plugin list and re-run them with
  // the merged options.
  plugins?: Plugin[]

  safe?: {
    key: boolean
  }
  tag?: string
  fixed?: {
    lex?: boolean
    token?: StrMap
    check?: LexCheck
  }
  match?: {
    lex?: boolean
    token?: { [name: string]: RegExp | LexMatcher }
    value?: {
      [name: string]: {
        match: RegExp | LexMatcher
        val?: any
      }
    }
    check?: LexCheck
  }
  tokenSet?: {
    [name: string]: string[]
  }
  space?: {
    lex?: boolean
    chars?: string
    check?: LexCheck
  }
  line?: {
    lex?: boolean
    chars?: string
    rowChars?: string
    single?: boolean
    check?: LexCheck
  }
  text?: {
    lex?: boolean
    modify?: ValModifier | ValModifier[]
    check?: LexCheck
  }
  number?: {
    lex?: boolean
    hex?: boolean
    oct?: boolean
    bin?: boolean
    sep?: string | null
    exclude?: RegExp
    check?: LexCheck
  }
  comment?: {
    lex?: boolean
    def?: {
      [name: string]:
      | {
        line?: boolean
        start?: string
        end?: string
        lex?: boolean
        suffix?: string | string[] | LexMatcher
        eatline: boolean
      }
      | null
      | undefined
      | false
    }
    check?: LexCheck
  }
  string?: {
    lex?: boolean
    chars?: string
    multiChars?: string
    escapeChar?: string
    escape?: {
      [char: string]: string | null
    }
    allowUnknown?: boolean
    replace?: { [char: string]: string | null }
    abandon?: boolean
    check?: LexCheck
  }
  map?: {
    extend?: boolean
    merge?: (prev: any, curr: any) => any
    child?: boolean
  }
  list?: {
    property: boolean
    pair: boolean
    child: boolean
  }
  info?: {
    map?: boolean
    list?: boolean
    text?: boolean
    marker?: string
  }
  value?: {
    lex?: boolean
    def?: {
      [src: string]:
      | undefined
      | null
      | false
      | {
        val: any

        // RegExp values will always have lower priority than pure tokens
        // as they are matched by the TextMatcher. For higher priority
        // use the `match` option.
        match?: RegExp
        consume?: boolean
      }
    }
  }
  ender?: string | string[]
  plugin?: Bag
  debug?: {
    get_console?: () => any
    maxlen?: number
    print?: {
      config?: boolean
      src?: (x: any) => string
    }
  }
  error?: { [code: string]: string }
  errmsg?: {
    name?: string
    suffix?: boolean | string | ((color?: any, spec?: any) => string)
  }
  hint?: any
  lex?: {
    empty?: boolean
    emptyResult?: any
    match: {
      [name: string]: {
        order: number
        make: MakeLexMatcher
      }
    }
  }
  parse?: {
    prepare?: { [name: string]: ParsePrepare }
  }
  rule?: {
    start?: string
    finish?: boolean
    maxmul?: number
    include?: string
    exclude?: string
  }
  result?: {
    fail: any[]
  }
  rewind?: {
    // Maximum number of consumed tokens retained in ctx.v for
    // ctx.rewind(). Defaults to Infinity (unbounded). Set a finite
    // value to cap parse-time memory; ctx.rewind(mark) will throw
    // if the mark has been evicted from the retained window.
    history?: number
  }
  config?: {
    modify?: {
      [plugin_name: string]: (config: Config, options: AmagamaOptions) => void
    }
  }
  parser?: {
    start?: (
      lexer: any,
      src: string,
      amagama: Amagama,
      meta?: any,
      parent_ctx?: any,
    ) => any
  }
  standard$?: boolean
  defaults$?: boolean
  grammar$?: boolean

  color?: {
    active?: boolean
    reset?: string
    hi?: string
    lo?: string
    line?: string
  }

}

// Parsing rule specification. The rule OPEN and CLOSE state token
// RuleSpec / Rule are runtime classes defined in src/rules.ts. The
// type-only re-export here lets other type definitions in this file
// (and the rest of the codebase that imports from `./types`) reference
// them as types without pulling the implementation in.
import type { Rule, RuleSpec } from './rules'
export type { Rule, RuleSpec }

// The current parse state and associated context.
// Per-parse Context. The runtime class is in src/context.ts; this
// type-only import + re-export lets other type definitions in this
// file (and the rest of the codebase that imports from `./types`)
// keep referencing `Context` without pulling in the class itself.
import type { Context } from './context'
export type { Context }

// Lex is a runtime class defined in src/lexer.ts. The type-only re-
// export here keeps all consumers that import from `./types` working.
import type { Lex } from './lexer'
export type { Lex }

export type NextToken = (rule: Rule) => Token

// Internal clean configuration built from options by
// `utility.configure` and LexMatchers.
export type Config = {
  safe: {
    key: boolean
  }
  lex: {
    match: LexMatcher[]
    empty: boolean
    emptyResult: any
  }

  parse: {
    prepare: ParsePrepare[]
  }

  rule: {
    start: string
    maxmul: number
    finish: boolean
    include: string[]
    exclude: string[]
  }

  // Fixed tokens (punctuation, operators, keywords, etc.)
  fixed: {
    lex: boolean
    token: TokenMap
    ref: Record<string | Tin, Tin | string>
    check?: LexCheck
  }

  // Matched tokens and values (regexp, custom function)
  match: {
    lex: boolean
    // Values have priority.
    value: {
      [name: string]: {
        // NOTE: RegExp must begin with `^`.
        match: RegExp | LexMatcher
        val?: any
      }
    }
    token: MatchMap
    check?: LexCheck
  }

  // Token set derived config.
  tokenSet: TokenSetMap

  // Token set derived config.
  tokenSetTins: {
    [name: string]: { [tin: number]: boolean }
  }

  // Space characters.
  space: {
    lex: boolean
    chars: Chars
    check?: LexCheck
  }

  // Line end characters.
  line: {
    lex: boolean
    chars: Chars
    rowChars: Chars // Row counting characters.
    single: boolean
    check?: LexCheck
  }

  // Unquoted text
  text: {
    lex: boolean
    modify: ValModifier[]
    check?: LexCheck
  }

  // Numbers
  number: {
    lex: boolean
    hex: boolean
    oct: boolean
    bin: boolean
    sep: boolean
    exclude?: RegExp
    sepChar?: string | null
    check?: LexCheck
  }

  // String quote characters.
  string: {
    lex: boolean
    quoteMap: Chars
    escMap: Bag
    escChar?: string
    escCharCode?: number
    multiChars: Chars
    allowUnknown: boolean
    replaceCodeMap: { [charCode: number]: string }
    hasReplace: boolean
    abandon: boolean
    check?: LexCheck
  }

  // Literal values
  value: {
    lex: boolean

    // Fixed values
    def: {
      [src: string]: {
        val: any
      }
    }

    // Regexp processed values, pre-sorted by name at configure time so
    // iteration during lexing is deterministic across runtimes.
    defre: {
      name: string
      val: (res: any) => any
      match: RegExp
      consume: boolean
    }[]
  }

  // Comment markers
  comment: {
    lex: boolean
    def: {
      [name: string]: {
        name: string
        line: boolean
        start: string
        end?: string
        lex: boolean
        eatline: boolean
        // Normalized suffix terminators. Matches are consumed as the
        // final part of the comment body.  Sorted longest-first.
        suffixes?: string[]
        // Optional function-form suffix probe. A non-nil return signals
        // termination; len(token.src) characters are consumed.
        suffixFn?: LexMatcher
      }
    }
    check?: LexCheck
  }

  map: {
    extend: boolean
    merge?: (prev: any, curr: any, rule: Rule, ctx: Context) => any
    child: boolean
  }

  list: {
    property: boolean
    pair: boolean
    child: boolean
  }

  info: {
    map: boolean
    list: boolean
    text: boolean
    marker: string
  }

  debug: {
    get_console: () => any
    maxlen: number
    print: {
      config: boolean
      src?: (x: any) => string
    }
  }

  result: {
    fail: any[]
  }

  rewind: {
    history: number
  }

  error: { [code: string]: string }
  errmsg: {
    name: string
    suffix: boolean | string | ((color?: any, spec?: any) => string)
  }

  hint: any

  rePart: any
  re: any

  tI: number // Token identifier index.
  t: Record<string, Tin> // Token index map.

  color: {
    active: boolean
    reset: string
    hi: string
    lo: string
    line: string
  }
}

// Point and Token are runtime classes defined in src/lexer.ts. Type-
// only re-export keeps consumers that import from `./types` working.
import type { Point, Token } from './lexer'
export type { Point, Token }

// Specification for a parse-alternate within a Rule state.
// Represent a possible token match (2-token lookahead)
export interface AltSpec {
  // Token Tin sequence to match (0,1,2 Tins, or a subset of Tins; nulls filterd out).
  s?: (Tin | Tin[] | null | undefined | string)[] | null | string

  // Push named Rule onto stack (create child).
  p?: string | AltNext | null | false | FuncRef

  // Replace current rule with named Rule on stack (create sibling).
  r?: string | AltNext | null | false | FuncRef

  // Move token pointer back by indicated number of steps.
  b?: number | AltBack | null | false | FuncRef

  // Condition function, return true to match alternate.
  // NOTE: Token sequence (s) must also match.
  c?: AltCond | null

  n?: Counters // Increment counters by specified amounts.
  a?: AltAction | FuncRef | null // Perform an action if this alternate matches.
  h?: AltModifier | null // Modify current Alt to customize parser.
  u?: Bag // Key-value custom data.
  k?: Bag // Key-value custom data (propagated).

  g?:
  | string // Named group tags for the alternate (allows filtering).
  | string[] // - comma separated or string array

  e?: AltError | FuncRef | null// Generate an error token (alternate is not allowed).
}

// Allow AltSpecs to be "empty" and thus ignored.
export type AltSpecish = AltSpec | undefined | null | false | 0 | typeof NaN

// List modifications
export type ListMods = {
  append?: boolean // if `true` apppend new entries, otherwise prepend.
  move?: number[] // [from,to,  from,to,  ...]
  delete?: number[] // [index0, index1, ...]
  custom?: (alts: AltSpec[]) => null | AltSpec[]
}

// Parse-alternate match. The runtime class lives in src/rules.ts.
import type { AltMatch } from './rules'
export type { AltMatch }

// General container of named items.
export type Bag = { [key: string]: any }

export type FuncRef = `@${string}`

// Named function references.
export type FuncRefMap<FT> = Record<FuncRef, FT>

// A set of named counters.
export type Counters = { [key: string]: number }

// Unique token identification number (aka "tin").
export type Tin = number

// Map token name ('#' prefix removed) to Token index (Tin).
export type TokenMap = { [name: string]: Tin }

// Map token name ('#' prefix removed) to Token index (Tin) set.
export type TokenSetMap = { [name: string]: Tin[] }

// Map Token index (Tin) to token name ('#' prefix removed).
export type TinMap = { [ref: number]: string }

// Map Token index (Tin) to token set name ('#' prefix removed).
export type TinSetMap = { [ref: number]: string }

// Map token name to matcher.
export type MatchMap = { [name: string]: RegExp | LexMatcher }

// Map character to code value.
export type Chars = { [char: string]: number }

// Map string to string value.
export type StrMap = { [name: string]: string }

// After rule stack push, Rules are in state OPEN ('o'),
// after first process, awaiting pop, Rules are in state CLOSE ('c').
export type RuleState = 'o' | 'c'

// When executing a Rule state (attempting a match), an action can be
// executed BEFORE ('b') or AFTER ('a') the match.
export type RuleStep = 'b' | 'a'

// A lexing function that attempts to match tokens.
export type LexMatcher = (
  lex: Lex,
  rule: Rule,
  tI?: number,
) => Token | undefined

// Construct a lexing function based on configuration.
export type MakeLexMatcher = (
  cfg: Config,
  opts: AmagamaOptions,
) => LexMatcher | null | undefined | false

export type LexCheck = (
  lex: Lex,
) => void | undefined | { done: boolean; token: Token | undefined }

export type ParsePrepare = (amagama: Amagama, ctx: Context, meta?: any) => void

export type RuleSpecMap = { [name: string]: RuleSpec }

export type RuleDefiner = (rs: RuleSpec, p: Parser) => void | RuleSpec

// Normalized parse-alternate.
export interface NormAltSpec extends AltSpec {
  s: (Tin | Tin[] | null | undefined)[]
  p: string | AltNext | null | false
  r: string | AltNext | null | false
  b: number | AltBack | null | false
  // Per-position bit-field match tables. S[i] is the bit-packed Tin set
  // for the i-th token in `s` (null if position matches any token).
  S: (number[] | null)[] | null
  // Per-position resolved Tin arrays (used for tcol collation).
  t: Tin[][]
  // Cached length of `s` (number of lookahead positions this alt requires).
  sN: number
  c: NormAltCond | null | undefined // Convenience definition reduce to function for processing.
  g: string[] // Named group tags
  a: AltAction | null | undefined // Generate an error token (alternate is not allowed).
  e: AltError | null | undefined // Generate an error token (alternate is not allowed).
}

// Conditionally pass an alternate.
export type AltCond =
  ((rule: Rule, ctx: Context, alt: AltMatch) => boolean)
  | Record<string, any>

export type NormAltCond =
  ((rule: Rule, ctx: Context, alt: AltMatch) => boolean)


// Arbitrarily modify an alternate to customize parser.
export type AltModifier = (
  rule: Rule,
  ctx: Context,
  alt: AltMatch,
  next: Rule,
) => AltMatch

// Execute an action when alternate matches.
export type AltAction = (rule: Rule, ctx: Context, alt: AltMatch) => any

// Determine next rule name (for AltSpec r or p properties).
export type AltNext = (
  rule: Rule,
  ctx: Context,
  alt: AltMatch,
) => string | null | false | 0

// Determine token push back.
export type AltBack = (
  rule: Rule,
  ctx: Context,
  alt: AltMatch,
) => number | null | false

// Execute an action for a given Rule state and step:
// bo: BEFORE OPEN, ao: AFTER OPEN, bc: BEFORE CLOSE, ac: AFTER CLOSE.
export type StateAction = (
  rule: Rule,
  ctx: Context,
  next: Rule,
  out?: Token | void, // TODO: why void?
) => Token | void

// Generate an error token (with an appropriate code).
// NOTE: errors are specified using tokens in order to capture file row and col.
export type AltError = (
  rule: Rule,
  ctx: Context,
  alt: AltMatch,
) => Token | undefined

export type ValModifier = (
  val: any,
  lex: Lex,
  cfg: Config,
  opts: AmagamaOptions,
) => string

export type LexSub = (tkn: Token, rule: Rule, ctx: Context) => void
export type RuleSub = (rule: Rule, ctx: Context) => void

// Parser is a runtime class defined in src/parser.ts.
import type { Parser } from './parser'
export type { Parser }


export type GrammarSpec = {
  ref?: Record<FuncRef, Function>

  // JSON-serializable options. Function-valued fields use FuncRef strings
  // that are resolved against `ref` before being applied.
  options?: Bag

  rule?: Record<string, {
    open?: GrammarAltSpec[] |
    {
      alts: GrammarAltSpec[],
      inject: { append?: boolean, delete?: number[], move?: number[] }
    }
    close?: GrammarAltSpec[] |
    {
      alts: GrammarAltSpec[],
      inject: { append?: boolean, delete?: number[], move?: number[] }
    }

  }>

}


export type GrammarAltSpec = {
  s?: string | string[],
  b?: FuncRef | number,
  p?: FuncRef | string,
  r?: FuncRef | string,
  a?: FuncRef,
  e?: FuncRef,
  h?: FuncRef,
  c?: FuncRef | Record<string, any>,
  n?: Record<string, number>,
  u?: Record<string, any>
  k?: Record<string, any>
  g?: string | string[],
}
