/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

/*  defaults.ts
 *  Default option values.
 */

import { TabnasOptions } from './tabnas'

// Functions that create token matching lexers.
// The `make*Matcher` functions may optionally initialise
// and validate Config properties specific to their lexing.
import {
  makeMatchMatcher,
  makeFixedMatcher,
  makeSpaceMatcher,
  makeLineMatcher,
  makeStringMatcher,
  makeCommentMatcher,
  makeNumberMatcher,
  makeTextMatcher,
} from './lexer'


const defaults: TabnasOptions = {
  // Prevent prototype pollution
  safe: {
    key: true,
  },

  // Default tag - set your own!
  tag: '-',

  // Fixed token lexing.
  fixed: {
    // Recognize fixed tokens in the Lexer.
    lex: true,

    // Token names.
    token: {
      '#OB': '{',
      '#CB': '}',
      '#OS': '[',
      '#CS': ']',
      '#CL': ':',
      '#CA': ',',
    },
  },

  match: {
    lex: true,
    token: {},
  },

  // Token sets.
  tokenSet: {
    IGNORE: ['#SP', '#LN', '#CM'],
    VAL: ['#TX', '#NR', '#ST', '#VL'],
    KEY: ['#TX', '#NR', '#ST', '#VL'],
  },

  // Recognize space characters in the lexer.
  space: {
    // Recognize space in the Lexer.
    lex: true,

    // Space characters are kept to a minimal set.
    // Add more from https://en.wikipedia.org/wiki/Whitespace_character as needed.
    chars: ' \t',
  },

  // Line lexing.
  line: {
    // Recognize lines in the Lexer.
    lex: true,

    // Line characters.
    chars: '\r\n',

    // Increments row (aka line) counter.
    rowChars: '\n',

    // Generate separate lexer tokens for each newline.
    // Note: '\r\n' counts as one newline.
    single: false,
  },

  // Text formats.
  text: {
    // Recognize text (non-quoted strings) in the Lexer.
    lex: true,
  },

  // Control number formats.
  number: {
    // Recognize numbers in the Lexer.
    lex: true,

    // Recognize hex numbers (eg. 10 === 0x0a).
    hex: true,

    // Recognize octal numbers (eg. 10 === 0o12).
    oct: true,

    // Recognize binary numbers (eg. 10 === 0b1010).
    bin: true,

    // All possible number chars. |+-|0|xob|0-9a-fA-F|.e|+-|0-9a-fA-F|
    // digital: '-1023456789._xoeEaAbBcCdDfF+',

    // Allow embedded separator. `null` to disable.
    sep: '_',

    // Exclude number strings matching this RegExp
    exclude: undefined,
  },

  // Comment markers.
  // <mark-char>: true -> single line comments
  // <mark-start>: <mark-end> -> multiline comments
  comment: {
    // Recognize comments in the Lexer.
    lex: true,

    // TODO: plugin
    // Balance multiline comments.
    // balance: true,

    // Comment markers.
    def: {
      hash: { line: true, start: '#', lex: true, eatline: false },
      slash: { line: true, start: '//', lex: true, eatline: false },
      multi: {
        line: false,
        start: '/' + '*',
        end: '*' + '/',
        lex: true,
        eatline: false,
      },
    },
  },

  // String formats.
  string: {
    // Recognize strings in the Lexer.
    lex: true,

    // Quote characters
    chars: '\'"`',

    // Multiline quote chars.
    multiChars: '`',

    // Escape character.
    escapeChar: '\\',

    // String escape chars.
    // Denoting char (follows escape char) => actual char.
    escape: {
      b: '\b',
      f: '\f',
      n: '\n',
      r: '\r',
      t: '\t',
      v: '\v',

      // These preserve standard escapes when allowUnknown=false.
      '"': '"',
      "'": "'",
      '`': '`',
      '\\': '\\',
      '/': '/',
    },

    // Allow unknown escape characters - they are copied to output: '\w' -> 'w'.
    allowUnknown: true,

    // Restrict escapes to the standard set: disable the non-standard
    // structural escapes \xHH and \u{...} (plain \uXXXX stays).
    escapeStrict: false,

    // Allow raw control characters (code point < 0x20) inside a string
    // body instead of erroring with `unprintable`. Line-end characters
    // are NOT covered by this: they stay governed by `multiChars`, so a
    // raw newline in a single-line string is still an error. Grammars
    // whose spec admits raw control chars (e.g. JSON5, whose
    // JSON5SourceCharacter permits a literal tab) set this true.
    // Default false keeps the strict behaviour.
    allowControl: false,

    // If string lexing fails, instead of error, allow other matchers to try.
    abandon: false,
  },

  // Object formats.
  map: {
    // TODO: or trigger error?
    // Later duplicates extend earlier ones, rather than replacing them.
    extend: true,

    // Custom merge function for duplicates (optional).
    // TODO: needs function signature
    merge: undefined,

    // Allow bare colon `:value` in maps, stored as `child$` property.
    child: false,
  },

  // Array formats.
  list: {
    // Allow arrays to have properties: `[a:9,0,1]`
    property: true,

    // Parse pairs as object elements: `[a:1]` -> `[{"a":1}]`
    // Takes precedence over list.property when true.
    pair: false,

    // Parse bare colon as child$ property: `[:1]` -> [] with child$=1
    // Multiple child values merge.
    child: false,
  },

  // Metadata info markers. When enabled, a non-enumerable marker property
  // is attached to parsed nodes with metadata (implicit flag, meta bag, etc.).
  info: {
    // Attach marker to map nodes.
    map: false,
    // Attach marker to list nodes.
    list: false,
    // Wrap string values as String objects with marker (quote info).
    text: false,
    // Property name for the marker.
    marker: '__info__',
  },

  // Keyword values.
  value: {
    lex: true,
    def: {
      true: { val: true },
      false: { val: false },
      null: { val: null },
    },
  },

  // Additional text ending characters
  ender: [],

  // Plugin custom options, (namespace by plugin name).
  plugin: {},

  // Debug settings
  debug: {
    // Default console for logging.
    get_console: () => console,

    // Max length of parse value to print.
    maxlen: 99,

    // Print internal structures
    print: {
      // Print config built from options.
      config: false,

      // Custom string formatter for src and node values.
      src: undefined,
    },
  },

  // Error messages.
  error: {
    unknown: 'unknown error: {code}',
    unexpected: 'unexpected character(s): {src}',
    invalid_unicode: 'invalid unicode escape: {src}',
    invalid_ascii: 'invalid ascii escape: {src}',
    unprintable: 'unprintable character: {src}',
    unterminated_string: 'unterminated string: {src}',
    unterminated_comment: 'unterminated comment: {src}',
    unknown_rule: 'unknown rule: {rulename}',
    end_of_source: 'unexpected end of source',
    cancel: 'parse cancelled',
  },

  errmsg: {
    name: 'tabnas',
    suffix: true
  },

  // Error hints: {error-code: hint-text}.
  hint: {
    unknown: `
Unknown error code: {code}
Details:
{details}`,

    unexpected: `
The character(s) {src} do not match any rule alternative active at
this position.`,

    invalid_unicode: `
The escape sequence {src} does not encode a valid unicode code point.`,

    invalid_ascii: `
The escape sequence {src} does not encode a valid ASCII character.`,

    unprintable: `
The character {src} (code point below 32) is not allowed inside a
string literal.`,

    unterminated_string: `
This string has no end quote.`,

    unterminated_comment: `
This comment is never closed.`,

    unknown_rule: `
No rule named {rulename} is defined.`,

    end_of_source: `
Unexpected end of source.`,

    cancel: `
The parse was cancelled by the caller's parse.budget.onCheck callback
(or exceeded its configured budget) before completing.`,
  },

  // Lexer
  lex: {
    match: {
      match: { order: 1e6, make: makeMatchMatcher },
      fixed: { order: 2e6, make: makeFixedMatcher },
      space: { order: 3e6, make: makeSpaceMatcher },
      line: { order: 4e6, make: makeLineMatcher },
      string: { order: 5e6, make: makeStringMatcher },
      comment: { order: 6e6, make: makeCommentMatcher },
      number: { order: 7e6, make: makeNumberMatcher },
      text: { order: 8e6, make: makeTextMatcher },
    },

    // Empty string is allowed and returns undefined
    empty: true,
    emptyResult: undefined,

    // Negotiated lexing off: grammars written for a tokenising lexer
    // never need it. Scannerless front-ends (GBNF) opt in.
    relex: false,
  },

  // Parser
  parse: {
    // Plugin custom functions to prepare parser context.
    prepare: {},

    // Opt-in error recovery (multi-error collection). Off by default:
    // fail-fast consumers are untouched. When enabled, parse() returns
    // `{ value, errors }` instead of throwing on the first error, and
    // the parser skips to a sync point derived from the live rule
    // stack (close-alternate `g` group tags) after each error.
    // See ts/doc/lsp-feasibility.md for the design.
    recover: {
      enabled: false,

      // AltSpec.g tags that mark close alternates as sync edges.
      // null = the engine default ['close','comma','end']. Provide an
      // array to REPLACE the set entirely (include the defaults
      // yourself if you want them kept) — the option layer merges
      // arrays index-wise, so a non-null default here would splice
      // user values over defaults instead of replacing them.
      syncGroups: null,

      // Explicit extra sync token names (e.g. ['#CA']).
      syncTokens: [],

      // Pop the rule stack until a rule accepts the sync token in its
      // close state (else pop exactly one rule).
      popUntilValid: true,

      // Cap forward token skip per recovery.
      maxSkip: 64,

      // Cap recorded errors per parse; the parse gives up beyond this.
      maxRecoveries: 32,

      // Errors within this many consumed tokens of the previous
      // recovery are dropped as cascades.
      suppress: 4,
    },

    // Opt-in parse budget / cancellation. Off by default (checkEveryN
    // 0). When set, the main rule loop invokes onCheck every N
    // iterations; a false return cancels the parse with a `cancel`
    // error. Long-lived hosts (language servers) use this to enforce
    // deadlines or observe an abort flag set from another thread.
    budget: {
      checkEveryN: 0,
      onCheck: null,
    },
  },

  // Parser rule options.
  rule: {
    // Name of the starting rule.
    start: 'val',

    // Automatically close remaining structures at EOF.
    finish: true,

    // Multiplier to increase the maximum number of rule occurrences.
    maxmul: 3,

    // Include only those alts with matching group tags (comma sep).
    // NOTE: applies universally, thus also for subsequent rules.
    include: '',

    // Exclude alts with matching group tags (comma sep).
    // NOTE: applies universally, thus also for subsequent rules.
    exclude: '',
  },

  // Result value options.
  result: {
    // Fail if result matches any of these.
    fail: [],
  },

  // Token-rewind options. `history` bounds how many consumed tokens
  // are retained on ctx.v for ctx.rewind(). The default of 64 keeps
  // parse-time memory bounded for large inputs; raise it if a
  // grammar needs to rewind further, or set to Infinity to retain
  // every consumed token. ctx.rewind(mark) throws if `mark` falls
  // outside the retained window.
  rewind: {
    history: 64,
  },

  // Configuration options.
  config: {
    // Configuration modifiers.
    modify: {},
  },

  // Provide a custom parser.
  parser: {
    start: undefined,
  },
}

export { defaults }
