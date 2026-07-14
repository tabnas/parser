/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

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
export const SKIP: unique symbol = Symbol.for('tabnas.SKIP')

// Empty rule used as a no-value placeholder.
// export const NONE = ({ name: 'none', state: OPEN } as Rule)

export const STRING = 'string'

// Standalone parse function signature, for plugins that type a parse callback.
export type TabnasParse = (src: any, meta?: any, parent_ctx?: any) => any


// Extra settings applied when installing a grammar spec; `rule.alt.g` is appended to every rule-alt `g`.
export type GrammarSetting = {
  rule?: {                          // Rule-level settings.
    alt?: {                         // Alternate-level settings.
      g?: string | string[]         // Group tag(s) appended to every alt's `g`.
    }
  }
}


// Internal state of a Tabnas instance, exposed via `instance.internal()`.
export type TabnasInternal = {
  parser: Parser                              // Live parser instance.
  config: Config                              // Resolved configuration.
  plugins: Plugin[]                           // Plugins applied, in order.
  sub: { lex?: LexSub[]; rule?: RuleSub[] }   // Lex and rule subscriber callbacks.
  mark: number                                // Random marker identifying this instance.
  merged: Record<string, any>                 // Accumulated merged options.
}


// Type-only re-export of the Tabnas runtime class (src/tabnas.ts).
import type { Tabnas } from './tabnas'
export type { Tabnas }


// A plugin: a function that extends a Tabnas instance, with optional metadata.
export type Plugin = ((
  tabnas: Tabnas,                 // Instance to extend.
  plugin_options?: any,           // Options passed to the plugin.
) => void | Tabnas) & {
  defaults?: Record<string, any>  // Default option values for the plugin.
  options?: Record<string, any>   // Resolved plugin options. TODO: InstalledPlugin.options is always defined ?
}

// Canonical parsing options passed to `new Tabnas(...)`, `tn.make(...)`, and `tn.options(...)`.
// See defaults.ts for commentary on individual fields.
export type TabnasOptions = {
  // Plugins to apply at construction time. `new Tabnas({ plugins:
  // [bnf] })` is sugar for `tn.use(bnf)` after construction —
  // children inherit the parent's plugin list and re-run them with
  // the merged options.
  plugins?: Plugin[]

  safe?: {                          // Safety guards.
    key: boolean                    // Reject unsafe (prototype-polluting) keys.
  }
  tag?: string                      // Label for this instance (used in debug output).
  fixed?: {                         // Fixed (literal punctuation/operator) tokens.
    lex?: boolean                   // Enable fixed-token lexing.
    token?: StrMap                  // Map of token name to literal source string.
    check?: LexCheck                // Post-match validation hook.
  }
  match?: {                         // Tokens and values matched by regexp/function.
    lex?: boolean                   // Enable match lexing.
    token?: { [name: string]: RegExp | LexMatcher }   // Matched token definitions.
    value?: {                                         // Matched value definitions.
      [name: string]: {
        match: RegExp | LexMatcher  // Matcher producing the value.
        val?: any                   // Fixed value, or transform when omitted.
      }
    }
    check?: LexCheck                // Post-match validation hook.
  }
  tokenSet?: {                      // Named groups of token names.
    [name: string]: string[]
  }
  space?: {                         // Whitespace (non-line) handling.
    lex?: boolean                   // Enable space lexing.
    chars?: string                  // Characters treated as space.
    check?: LexCheck                // Post-match validation hook.
  }
  line?: {                          // Line-end handling.
    lex?: boolean                   // Enable line lexing.
    chars?: string                  // Characters treated as line ends.
    rowChars?: string               // Characters that increment the row counter.
    single?: boolean                // Collapse runs into a single line token.
    check?: LexCheck                // Post-match validation hook.
  }
  text?: {                          // Unquoted text handling.
    lex?: boolean                   // Enable text lexing.
    modify?: ValModifier | ValModifier[]   // Text value modifier(s).
    check?: LexCheck                // Post-match validation hook.
  }
  number?: {                        // Numeric literal handling.
    lex?: boolean                   // Enable number lexing.
    hex?: boolean                   // Allow 0x hexadecimal literals.
    oct?: boolean                   // Allow 0o octal literals.
    bin?: boolean                   // Allow 0b binary literals.
    sep?: string | null             // Digit separator character (e.g. '_').
    exclude?: RegExp                // Sources matching this are not numbers.
    check?: LexCheck                // Post-match validation hook.
  }
  comment?: {                       // Comment handling.
    lex?: boolean                   // Enable comment lexing.
    def?: {                         // Comment marker definitions by name.
      [name: string]:
      | {
        line?: boolean             // True for line comments, false for block.
        start?: string             // Opening marker.
        end?: string               // Closing marker (block comments).
        lex?: boolean              // Enable this definition.
        suffix?: string | string[] | LexMatcher   // Terminating suffix(es).
        eatline: boolean           // Consume the trailing line end.
      }
      | null
      | undefined
      | false
    }
    check?: LexCheck                // Post-match validation hook.
  }
  string?: {                        // Quoted string handling.
    lex?: boolean                   // Enable string lexing.
    chars?: string                  // Single-line quote characters.
    multiChars?: string             // Multi-line quote characters.
    escapeChar?: string             // Escape character (e.g. '\').
    escape?: {                      // Escape sequence map.
      [char: string]: string | null
    }
    allowUnknown?: boolean          // Pass through unknown escapes verbatim.
    // Restrict escapes to the standard set: disable the non-standard
    // \xHH and \u{...} structural escapes (plain \uXXXX stays). Default
    // false. Combine with escape-map removals + allowUnknown:false for
    // JSON.parse-conformant escape handling.
    escapeStrict?: boolean
    replace?: { [char: string]: string | null }   // Post-parse character replacements.
    abandon?: boolean               // Abandon (re-lex) on unterminated string.
    check?: LexCheck                // Post-match validation hook.
  }
  map?: {                           // Object/map construction.
    extend?: boolean                // Deep-merge duplicate keys.
    merge?: (prev: any, curr: any) => any   // Custom duplicate-key merger.
    child?: boolean                 // Wrap values in child objects.
  }
  list?: {                          // Array/list construction.
    property: boolean               // Allow properties in lists.
    pair: boolean                   // Allow key:value pairs in lists.
    child: boolean                  // Wrap entries in child objects.
  }
  info?: {                          // Source-info annotations on results.
    map?: boolean                   // Annotate maps.
    list?: boolean                  // Annotate lists.
    text?: boolean                  // Annotate text values.
    marker?: string                 // Property name for the annotation.
  }
  value?: {                         // Literal value definitions (e.g. true, null).
    lex?: boolean                   // Enable value lexing.
    def?: {                         // Value definitions by source string.
      [src: string]:
      | undefined
      | null
      | false
      | {
        val: any                   // The literal value produced.

        // RegExp values will always have lower priority than pure tokens
        // as they are matched by the TextMatcher. For higher priority
        // use the `match` option.
        match?: RegExp
        consume?: boolean          // Consume the matched source.
      }
    }
  }
  ender?: string | string[]         // Characters that end the current value.
  plugin?: Record<string, any>      // Per-plugin option storage.
  debug?: {                         // Debug output settings.
    get_console?: () => any         // Console provider.
    maxlen?: number                 // Max length of printed values.
    print?: {                       // Print toggles.
      config?: boolean              // Print resolved config.
      src?: (x: any) => string      // Source stringifier.
    }
  }
  error?: { [code: string]: string }   // Error message templates by code.
  errmsg?: {                        // Error message formatting.
    name?: string                   // Error name shown in messages.
    suffix?: boolean | string | ((color?: any, spec?: any) => string)   // Trailing diagnostics line.
    // Optional "see also" line appended above the internal-diagnostics
    // line when `suffix` is `true` (e.g. a docs URL).
    link?: string
  }
  hint?: any                        // Hint text shown with errors.
  lex?: {                           // Lexer matcher registry.
    empty?: boolean                 // Allow empty source.
    emptyResult?: any               // Result for empty source.
    match: {                        // Matcher constructors by name.
      [name: string]: {
        order: number               // Match priority (lower runs first).
        make: MakeLexMatcher        // Matcher constructor.
      }
    }
  }
  parse?: {                         // Parse-phase hooks.
    prepare?: { [name: string]: ParsePrepare }   // Pre-parse setup callbacks.
  }
  rule?: {                          // Rule engine settings.
    start?: string                  // Name of the start rule.
    finish?: boolean                // Require input to be fully consumed.
    maxmul?: number                 // Max rule multiplier (loop guard).
    include?: string                // Group tags to include.
    exclude?: string                // Group tags to exclude.
  }
  result?: {                        // Result handling.
    fail: any[]                     // Values that mark a failed parse.
  }
  rewind?: {                        // Token rewind history.
    // Maximum number of consumed tokens retained in ctx.v for
    // ctx.rewind(). Defaults to 64 (see defaults.ts) to keep parse-time
    // memory bounded; set to Infinity for unbounded retention, or a
    // larger finite value as needed. ctx.rewind(mark) throws if the
    // mark has been evicted from the retained window — so a grammar
    // that probes/rewinds across a long span (e.g. the `$`-builtin
    // probe dispatcher scanning a long optional prefix) must raise
    // this above the longest prefix it can encounter, or it will throw
    // on otherwise-valid input.
    history?: number
  }
  config?: {                        // Config post-processing.
    modify?: {                      // Per-plugin config mutators.
      [plugin_name: string]: (config: Config, options: TabnasOptions) => void
    }
  }
  parser?: {                        // Parser override.
    start?: (                       // Custom parse entry point.
      lexer: any,
      src: string,
      tabnas: Tabnas,
      meta?: any,
      parent_ctx?: any,
    ) => any
  }
  standard$?: boolean               // Internal flag: standard option set.
  defaults$?: boolean               // Internal flag: defaults option set.
  grammar$?: boolean                // Internal flag: grammar-derived option set.

  color?: {                         // ANSI color settings for output.
    active?: boolean                // Enable coloring.
    reset?: string                  // Reset escape sequence.
    hi?: string                     // Highlight escape sequence.
    lo?: string                     // Dim escape sequence.
    line?: string                   // Line-marker escape sequence.
  }

}

// Type-only re-export of the Rule and RuleSpec runtime classes (src/rules.ts).
import type { Rule, RuleSpec } from './rules'
export type { Rule, RuleSpec }

// Type-only re-export of the per-parse Context runtime class (src/context.ts).
import type { Context } from './context'
export type { Context }

// Type-only re-export of the Lex runtime class (src/lexer.ts).
import type { Lex } from './lexer'
export type { Lex }

// Get the next token for the given rule.
export type NextToken = (rule: Rule) => Token

// Resolved configuration built from options by `utility.configure` and LexMatchers.
export type Config = {
  safe: {                           // Safety guards.
    key: boolean                    // Reject unsafe keys.
  }
  lex: {                            // Lexer state.
    match: LexMatcher[]             // Active matchers, in priority order.
    // First-char dispatch: dispatch[charCode] (or [256] for non-Latin-1
    // positions) lists the matchers, in priority order, that could
    // produce a token starting with that char. Built by configure();
    // when absent the full match list runs at every position.
    dispatch?: LexMatcher[][]
    empty: boolean                  // Allow empty source.
    emptyResult: any                // Result for empty source.
  }

  parse: {                          // Parse-phase hooks.
    prepare: ParsePrepare[]         // Pre-parse setup callbacks.
  }

  rule: {                           // Rule engine settings.
    start: string                   // Name of the start rule.
    maxmul: number                  // Max rule multiplier (loop guard).
    finish: boolean                 // Require input to be fully consumed.
    include: string[]               // Group tags to include.
    exclude: string[]               // Group tags to exclude.
  }

  // Fixed tokens (punctuation, operators, keywords, etc.)
  fixed: {
    lex: boolean                    // Enable fixed-token lexing.
    token: TokenMap                 // Token name to Tin.
    ref: Record<string | Tin, Tin | string>   // Bidirectional name/Tin/source map.
    check?: LexCheck                // Post-match validation hook.
  }

  // Matched tokens and values (regexp, custom function)
  match: {
    lex: boolean                    // Enable match lexing.
    // Values have priority.
    value: {
      [name: string]: {
        // NOTE: RegExp must begin with `^`.
        match: RegExp | LexMatcher  // Matcher producing the value.
        val?: any                   // Fixed value, or transform when omitted.
      }
    }
    token: MatchMap                 // Matched token definitions.
    check?: LexCheck                // Post-match validation hook.
  }

  // Token set name to member Tins.
  tokenSet: TokenSetMap

  // Token set name to membership lookup (Tin -> true).
  tokenSetTins: {
    [name: string]: { [tin: number]: boolean }
  }

  // Space characters.
  space: {
    lex: boolean                    // Enable space lexing.
    chars: Chars                    // Space character to code.
    charsBitmap: Uint8Array         // 256-byte fast-path lookup for chars
    check?: LexCheck                // Post-match validation hook.
  }

  // Line end characters.
  line: {
    lex: boolean                    // Enable line lexing.
    chars: Chars                    // Line-end character to code.
    charsBitmap: Uint8Array         // 256-byte fast-path lookup for chars
    rowChars: Chars                 // Row counting characters.
    rowCharsBitmap: Uint8Array      // 256-byte fast-path lookup for rowChars
    single: boolean                 // Collapse runs into a single line token.
    check?: LexCheck                // Post-match validation hook.
  }

  // Unquoted text
  text: {
    lex: boolean                    // Enable text lexing.
    modify: ValModifier[]           // Text value modifiers.
    check?: LexCheck                // Post-match validation hook.
  }

  // Numbers
  number: {
    lex: boolean                    // Enable number lexing.
    hex: boolean                    // Allow 0x hexadecimal literals.
    oct: boolean                    // Allow 0o octal literals.
    bin: boolean                    // Allow 0b binary literals.
    sep: boolean                    // Digit separators enabled.
    exclude?: RegExp                // Sources matching this are not numbers.
    sepChar?: string | null         // Digit separator character.
    check?: LexCheck                // Post-match validation hook.
  }

  // String quote characters.
  string: {
    lex: boolean                    // Enable string lexing.
    quoteMap: Chars                 // Single-line quote character to code.
    quoteBitmap: Uint8Array         // 256-byte fast-path lookup for quoteMap
    escMap: Record<string, any>     // Escape sequence map.
    escBitmap: Uint8Array           // 256-byte fast-path lookup for escMap keys
    escChar?: string                // Escape character.
    escCharCode?: number            // Escape character code.
    multiChars: Chars               // Multi-line quote character to code.
    multiBitmap: Uint8Array         // 256-byte fast-path lookup for multiChars
    allowUnknown: boolean           // Pass through unknown escapes verbatim.
    escapeStrict: boolean           // Disable non-standard structural escapes.
    replaceCodeMap: { [charCode: number]: string }   // Char-code replacement map.
    hasReplace: boolean             // True if replaceCodeMap is non-empty.
    abandon: boolean                // Abandon (re-lex) on unterminated string.
    check?: LexCheck                // Post-match validation hook.
  }

  // Literal values
  value: {
    lex: boolean                    // Enable value lexing.

    // Fixed values
    def: {
      [src: string]: {
        val: any                    // The literal value produced.
      }
    }

    // Regexp processed values, pre-sorted by name at configure time so
    // iteration during lexing is deterministic across runtimes.
    defre: {
      name: string                  // Value definition name.
      val: (res: any) => any        // Transform from match result to value.
      match: RegExp                 // Matcher (must begin with `^`).
      consume: boolean              // Consume the matched source.
    }[]
  }

  // Comment markers
  comment: {
    lex: boolean                    // Enable comment lexing.
    def: {
      [name: string]: {
        name: string               // Definition name.
        line: boolean              // True for line comments, false for block.
        start: string              // Opening marker.
        end?: string               // Closing marker (block comments).
        lex: boolean               // Enable this definition.
        eatline: boolean           // Consume the trailing line end.
        // Normalized suffix terminators. Matches are consumed as the
        // final part of the comment body.  Sorted longest-first.
        suffixes?: string[]
        // Optional function-form suffix probe. A non-nil return signals
        // termination; len(token.src) characters are consumed.
        suffixFn?: LexMatcher
      }
    }
    check?: LexCheck                // Post-match validation hook.
  }

  map: {                            // Object/map construction.
    extend: boolean                 // Deep-merge duplicate keys.
    merge?: (prev: any, curr: any, rule: Rule, ctx: Context) => any   // Custom duplicate-key merger.
    child: boolean                  // Wrap values in child objects.
  }

  list: {                           // Array/list construction.
    property: boolean               // Allow properties in lists.
    pair: boolean                   // Allow key:value pairs in lists.
    child: boolean                  // Wrap entries in child objects.
  }

  info: {                           // Source-info annotations on results.
    map: boolean                    // Annotate maps.
    list: boolean                   // Annotate lists.
    text: boolean                   // Annotate text values.
    marker: string                  // Property name for the annotation.
  }

  debug: {                          // Debug output settings.
    get_console: () => any          // Console provider.
    maxlen: number                  // Max length of printed values.
    print: {                        // Print toggles.
      config: boolean               // Print resolved config.
      src?: (x: any) => string      // Source stringifier.
    }
  }

  result: {                         // Result handling.
    fail: any[]                     // Values that mark a failed parse.
  }

  rewind: {                         // Token rewind history.
    history: number                 // Max consumed tokens retained for ctx.rewind().
  }

  error: { [code: string]: string }   // Error message templates by code.
  errmsg: {                         // Error message formatting.
    name: string                    // Error name shown in messages.
    suffix: boolean | string | ((color?: any, spec?: any) => string)   // Trailing diagnostics line.
    link?: string                   // "See also" link line.
  }

  hint: any                         // Hint text shown with errors.

  rePart: any                       // Shared regexp fragments.
  re: any                           // Compiled regexps built from rePart.

  tI: number                        // Token identifier index (next Tin counter).
  t: Record<string, Tin>            // Token name to Tin index map.

  color: {                          // ANSI color settings for output.
    active: boolean                 // Enable coloring.
    reset: string                   // Reset escape sequence.
    hi: string                      // Highlight escape sequence.
    lo: string                      // Dim escape sequence.
    line: string                    // Line-marker escape sequence.
  }
}

// Type-only re-export of the Point and Token runtime classes (src/lexer.ts).
import type { Point, Token } from './lexer'
export type { Point, Token }

// A parse-alternate within a Rule state: a possible token match (up to 2-token lookahead).
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

  n?: Counters                    // Increment counters by specified amounts.
  // Action(s) on match. An array runs each in order (the matched alt's
  // own action first, then composed user actions); each element is a
  // function or a FuncRef string. The array is collapsed to a single
  // function at normalization time, so the normalized form stays scalar.
  a?: AltAction | FuncRef | (AltAction | FuncRef)[] | null
  h?: AltModifier | null          // Modify current Alt to customize parser.
  u?: Record<string, any>         // Key-value custom data.
  k?: Record<string, any>         // Key-value custom data (propagated).

  g?:
  | string                        // Named group tags for the alternate (allows filtering).
  | string[]                      // - comma separated or string array

  e?: AltError | FuncRef | null   // Generate an error token (alternate is not allowed).
}

// An AltSpec or a falsy placeholder (empty alternates are ignored).
export type AltSpecish = AltSpec | undefined | null | false | 0 | typeof NaN

// Modifications applied to a rule's list of alternates.
export type ListMods = {
  append?: boolean                          // if `true` apppend new entries, otherwise prepend.
  clear?: boolean                           // if `true`, empty the existing list before adding new entries.
  move?: number[]                           // [from,to,  from,to,  ...]
  delete?: number[]                         // [index0, index1, ...]
  custom?: (alts: AltSpec[]) => null | AltSpec[]   // Arbitrary transform of the alternates.
}

// Type-only re-export of the AltMatch runtime class (src/rules.ts).
import type { AltMatch } from './rules'
export type { AltMatch }

// A named function reference of the form `@name`.
export type FuncRef = `@${string}`

// Map of named function references to their functions.
export type FuncRefMap<FT> = Record<FuncRef, FT>

// A set of named counters.
export type Counters = { [key: string]: number }

// Unique token identification number (aka "tin"). Branded `number` so
// arbitrary integers can't be passed where a real token id is wanted —
// callers go through `tokenize()` (utility.ts) or assign via the
// internal `cfg.tI++` counter.
export type Tin = number & { readonly __brand: 'Tin' }

// Convenience cast for the few code paths that legitimately need to
// construct a Tin from a raw counter (tokenize, the test scaffold).
export const asTin = (n: number): Tin => n as Tin

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

// Rule lifecycle state: OPEN ('o') after push, CLOSE ('c') after first process (awaiting pop).
export type RuleState = 'o' | 'c'

// Rule state step: an action runs BEFORE ('b') or AFTER ('a') the match.
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
  opts: TabnasOptions,
) => LexMatcher | null | undefined | false

// Optional post-match validation hook called by a lexer matcher.
export type LexCheck = (
  lex: Lex,
) => void | undefined | { done: boolean; token: Token | undefined }

// Pre-parse setup callback run before parsing begins.
export type ParsePrepare = (tabnas: Tabnas, ctx: Context, meta?: any) => void

// Map rule name to its RuleSpec.
export type RuleSpecMap = { [name: string]: RuleSpec }

// Define or modify a RuleSpec for the parser.
export type RuleDefiner = (rs: RuleSpec, p: Parser) => void | RuleSpec

// Normalized parse-alternate, with required fields and precomputed match tables.
export interface NormAltSpec extends AltSpec {
  s: (Tin | Tin[] | null | undefined)[]   // Normalized token sequence to match.
  p: string | AltNext | null | false      // Push child rule.
  r: string | AltNext | null | false      // Replace with sibling rule.
  b: number | AltBack | null | false       // Token push back count.
  // Per-position bit-field match tables. S[i] is the bit-packed Tin set
  // for the i-th token in `s` (null if position matches any token).
  S: (number[] | null)[] | null
  // Per-position resolved Tin arrays (used for tcol collation).
  t: Tin[][]
  // Cached length of `s` (number of lookahead positions this alt requires).
  sN: number
  c: NormAltCond | null | undefined        // Match condition, reduced to a function.
  g: string[]                              // Named group tags.
  a: AltAction | null | undefined          // Action run when this alternate matches.
  e: AltError | null | undefined           // Error token generator (alternate is not allowed).
}

// Condition deciding whether an alternate matches (function or value-equality spec).
export type AltCond =
  ((rule: Rule, ctx: Context, alt: AltMatch) => boolean)
  | Record<string, any>

// Normalized (function-form) alternate condition.
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

// A RegExp token matcher with the engine's matcher annotations. `tin$`
// is the matcher's own token id; `eager$` opts the matcher out of the
// lexer's tcol gating (it fires whenever its regex matches, regardless
// of the active rule's token column). Serialized grammars carry the
// eager flag via the `@~/pattern/flags` ref form (see resolveFuncRefs).
export type EagerRegExp = RegExp & { tin$?: number; eager$?: boolean }

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

// Transform a lexed value (used for text and number modification).
export type ValModifier = (
  val: any,
  lex: Lex,
  cfg: Config,
  opts: TabnasOptions,
) => string

// Subscriber callback invoked for each lexed token.
export type LexSub = (tkn: Token, rule: Rule, ctx: Context) => void
// Subscriber callback invoked for each processed rule.
export type RuleSub = (rule: Rule, ctx: Context) => void

// Type-only re-export of the Parser runtime class (src/parser.ts).
import type { Parser } from './parser'
export type { Parser }


// JSON-serializable grammar definition: options plus per-rule alternates.
export type GrammarSpec = {
  ref?: Record<FuncRef, Function>           // Functions resolved from FuncRef strings.

  // Builtin config-schema version this grammar was compiled against. The
  // engine refuses a grammar whose `v` exceeds the schema it implements
  // (see BUILTIN_SCHEMA_VERSION). Absent ⇒ version 1.
  v?: number

  // JSON-serializable options. Function-valued fields use FuncRef strings
  // that are resolved against `ref` before being applied.
  options?: Record<string, any>

  rule?: Record<string, {                   // Per-rule alternate definitions.
    open?: GrammarAltSpec[] |               // OPEN-state alternates (or alts + inject ops).
    {
      alts: GrammarAltSpec[],
      inject: { append?: boolean, delete?: number[], move?: number[] }
    }
    close?: GrammarAltSpec[] |              // CLOSE-state alternates (or alts + inject ops).
    {
      alts: GrammarAltSpec[],
      inject: { append?: boolean, delete?: number[], move?: number[] }
    }

  }>

}


// JSON-serializable AltSpec: like AltSpec but function fields use FuncRef strings.
export type GrammarAltSpec = {
  s?: string | string[],                    // Token sequence to match.
  b?: FuncRef | number,                     // Token push back count.
  p?: FuncRef | string,                     // Push child rule.
  r?: FuncRef | string,                     // Replace with sibling rule.
  a?: FuncRef | FuncRef[],                  // Action(s) on match (array runs in order).
  e?: FuncRef,                              // Error token generator.
  h?: FuncRef,                              // Alternate modifier.
  c?: FuncRef | Record<string, any>,        // Match condition.
  n?: Record<string, number>,               // Counter increments.
  u?: Record<string, any>                   // Custom data.
  k?: Record<string, any>                   // Custom data (propagated).
  g?: string | string[],                    // Group tags.
}
