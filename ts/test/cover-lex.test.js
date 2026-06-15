/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// Coverage tests for lexer matcher branches: match matchers (regexp
// and function forms for both values and tokens), check hooks,
// string escapes and abandon mode, comment suffixes and eatline,
// number/value interactions, line `single` mode, and the scan
// primitives' non-ASCII fallbacks.

const { describe, it } = require('node:test')
const assert = require('node:assert')
const Util = require('util')

const { Tabnas, makeLex, makeToken, makePoint } = require('..')
const tn = new Tabnas()

// Collect all tokens of `src` lexed with options `opts`.
function tokens(opts, src) {
  let j = tn.make(opts)
  let t = j.token
  let lex = makeLex({
    src: () => src,
    cfg: j.internal().config,
    opts: j.options,
    sub: {},
  })
  let out = []
  let tkn
  do {
    tkn = lex.next()
    out.push(tkn)
  } while (t.ZZ !== tkn.tin && t.BD !== tkn.tin)
  return out
}

// Compact form: NAME or NAME:val
function summary(opts, src) {
  return tokens(opts, src)
    .map((t) => t.name + (undefined === t.val ? '' : ':' + t.val))
    .join(' ')
}

describe('cover-lex', () => {
  it('token-bad-details-and-inspect', () => {
    let t0 = makeToken('a', 1, 'v', 'vs', makePoint(2, 0, 1, 1), { x: 1 }, 'W')
    t0.bad('badness', { y: 2 })
    assert.equal(t0.err, 'badness')
    assert.deepEqual(t0.use, { x: 1, y: 2 })
    // Node inspection delegates to toString.
    assert.equal(Util.inspect(t0), '' + t0)
  })

  it('lex-refwd-and-tokenize', () => {
    let j = tn.make()
    let lex = makeLex({
      src: () => 'abcd',
      cfg: j.internal().config,
      opts: j.options,
      sub: {},
    })
    lex.pnt.sI = 2
    assert.equal(lex.refwd(), 'cd')
    assert.equal(lex.tokenize('#SP'), j.token.SP)
    assert.equal(lex.tokenize(j.token.SP), '#SP')
  })

  it('check-hooks-short-circuit', () => {
    // The guardedMatcher check hook can replace a matcher's output.
    const mkcheck = (c, val) => (lex) => {
      if (c === lex.src[lex.pnt.sI]) {
        const tkn = lex.token('#VL', val, c, lex.pnt)
        lex.pnt.sI += 1
        lex.pnt.cI += 1
        return { done: true, token: tkn }
      }
      return undefined
    }

    // number.check fires before the number matcher body.
    assert.equal(
      summary({ number: { check: mkcheck('1', 'one') } }, '1'),
      '#VL:one #ZZ',
    )

    // fixed.check
    assert.equal(
      summary({ fixed: { check: mkcheck('{', 'ob') } }, '{'),
      '#VL:ob #ZZ',
    )

    // space.check
    assert.equal(
      summary({ space: { check: mkcheck(' ', 'sp') } }, ' '),
      '#VL:sp #ZZ',
    )

    // line.check
    assert.equal(
      summary({ line: { check: mkcheck('\n', 'ln') } }, '\n'),
      '#VL:ln #ZZ',
    )

    // text.check (textMatcher has its own check logic)
    assert.equal(
      summary({ text: { check: mkcheck('q', 'tx') } }, 'q'),
      '#VL:tx #ZZ',
    )

    // A check that does not fire falls through to the matcher body.
    assert.equal(
      summary({ number: { check: mkcheck('Z', 'zed') } }, '7'),
      '#NR:7 #ZZ',
    )
  })

  it('check-hook-throws-becomes-bad-token', () => {
    // A throwing matcher is caught by Lex.next and produces a #BD
    // token carrying the error; err.code becomes the why.
    let coded = tokens(
      {
        space: {
          check: () => {
            throw Object.assign(new Error('boom'), { code: 'mycode' })
          },
        },
      },
      ' x',
    )
    assert.equal(coded[0].name, '#BD')
    assert.equal(coded[0].why, 'mycode')
    assert.ok(coded[0].use.err)

    // No code on the error: why falls back to 'unexpected'.
    let uncoded = tokens(
      {
        space: {
          check: () => {
            throw new Error('boom')
          },
        },
      },
      ' x',
    )
    assert.equal(uncoded[0].name, '#BD')
    assert.equal(uncoded[0].why, 'unexpected')
  })

  it('match-matchers-value-and-token-forms', () => {
    const j = new Tabnas({
      rule: { start: 'top' },
      match: {
        lex: true,
        value: {
          // RegExp value matcher with val transform.
          pct: { match: /^%[0-9]+/, val: (m) => +m[0].substring(1) },
          // RegExp value matcher without val (uses match source).
          raw: { match: /^&\w+/ },
          // Function (LexMatcher) value matcher.
          qm: {
            match: (lex) => {
              if ('?' === lex.src[lex.pnt.sI]) {
                const tkn = lex.token('#VL', 'Q', '?', lex.pnt)
                lex.pnt.sI += 1
                lex.pnt.cI += 1
                return tkn
              }
            },
          },
        },
        token: {
          // RegExp token matcher, gated by alt tins.
          '#T1': /^!a/,
          // RegExp token matcher NOT present in any alt: gated out.
          '#T2': /^=b/,
          // Function token matcher.
          '#T4': (lex) => {
            if (lex.src.startsWith('!d', lex.pnt.sI)) {
              const tkn = lex.token('#T4', '!d', '!d', lex.pnt)
              lex.pnt.sI += 2
              lex.pnt.cI += 2
              return tkn
            }
          },
          // Eager regexp matcher skips tcol gating.
          '#T5': Object.assign(/^!e/, { eager$: true }),
        },
      },
    })

    j.rule('top', (rs) =>
      rs
        .open([
          { s: ['#T1'], a: (r) => (r.node = 'T1') },
          { s: ['#T4'], a: (r) => (r.node = 'T4') },
          { s: ['#T5'], a: (r) => (r.node = 'T5') },
          { s: ['#VL'], a: (r) => (r.node = r.o0.val) },
          { s: ['#TX'], a: (r) => (r.node = 'TX:' + r.o0.val) },
        ])
        .close([{ s: ['#ZZ'] }]),
    )

    assert.equal(j.parse('%42'), 42)
    assert.equal(j.parse('&abc'), '&abc')
    assert.equal(j.parse('?'), 'Q')
    assert.equal(j.parse('!a'), 'T1')
    assert.equal(j.parse('!d'), 'T4')
    assert.equal(j.parse('!e'), 'T5')
    // '#T2' is gated out (not in any alt), so '=b' lexes as text.
    assert.equal(j.parse('=b'), 'TX:=b')
  })

  it('string-escapes-and-replace', () => {
    // Valid \x ascii escape.
    assert.equal(summary({}, '"\\x41"'), '#ST:A #ZZ')

    // Replace map overrides chars inside strings.
    assert.equal(
      summary({ string: { replace: { b: 'X' } } }, '"abc"'),
      '#ST:aXc #ZZ',
    )

    // Multiline string: row-advancing and non-row line chars.
    let multi = tokens({}, '`a\nb\rc`')
    assert.equal(multi[0].name, '#ST')
    assert.equal(multi[0].val, 'a\nb\rc')

    // Non-ASCII chars are plain string body chars.
    assert.equal(summary({}, '"　\u{1F600}"'), '#ST:　\u{1F600} #ZZ')

    // allowUnknown=false rejects unknown escapes.
    let bad = tokens({ string: { allowUnknown: false } }, '"\\q"')
    assert.equal(bad[0].name, '#BD')
    assert.equal(bad[0].why, 'unexpected')

    // Unprintable control char inside a non-multiline string.
    let unp = tokens({}, '"a\nb"')
    assert.equal(unp[0].name, '#BD')
    assert.equal(unp[0].why, 'unprintable')
  })

  it('string-abandon-falls-through', () => {
    // abandon:true makes the string matcher give up silently so later
    // matchers (text) can try.
    let opts = { string: { abandon: true } }
    assert.equal(summary(opts, '"abc'), '#TX:"abc #ZZ') // unterminated
    assert.equal(summary(opts, '"\\xQQ"'), '#TX:"\\xQQ" #ZZ') // bad ascii
    assert.equal(summary(opts, '"\\uQQQQ"'), '#TX:"\\uQQQQ" #ZZ') // bad unicode

    // Unknown escape with allowUnknown false + abandon.
    assert.equal(
      summary({ string: { abandon: true, allowUnknown: false } }, '"\\q"'),
      '#TX:"\\q" #ZZ',
    )

    // Control char + abandon.
    let t = tokens(opts, '"a\nb"')
    assert.equal(t[0].name, '#TX')
  })

  it('string-non-ascii-quote-and-lazy-spec', () => {
    // Non-ASCII quote chars go through the fallback classifier for
    // both opening and closing quotes, so the string terminates.
    let t = tokens({ string: { chars: '"「' } }, '「ab「')
    assert.equal(t[0].name, '#ST')
    assert.equal(t[0].val, 'ab')

    // A config modifier that adds a quote char after the matchers are
    // built triggers the lazy body-spec construction.
    let lazy = tokens(
      {
        config: {
          modify: {
            addq: (cfg) => {
              cfg.string.quoteMap['^'] = 94
              cfg.string.quoteBitmap[94] = 1
            },
          },
        },
      },
      '^ab^',
    )
    assert.equal(lazy[0].name, '#ST')
    assert.equal(lazy[0].val, 'ab')
  })

  it('line-matchers-non-ascii-and-single', () => {
    // Non-ASCII line chars exercise the scan fallback path.
    let opts = { line: { chars: '\r\n ', rowChars: '\n ' } }
    let t = tokens(opts, 'a 　b')
    assert.equal(t[0].name, '#TX')
    assert.equal(t[1].name, '#LN')
    assert.equal(t[1].src, ' ')
    assert.equal(t[2].rI, 2)

    // single mode: stop at the first repeated line char.
    let s = tokens({ line: { single: true } }, '\r\n\r\nx')
    assert.equal(s[0].name, '#LN')
    assert.equal(s[0].src, '\r\n')
    assert.equal(s[1].name, '#LN')
    assert.equal(s[1].src, '\r\n')
    assert.equal(s[2].name, '#TX')

    // single mode with non-ASCII line chars.
    let s2 = tokens(
      { line: { single: true, chars: '\r\n ', rowChars: '\n ' } },
      ' x',
    )
    assert.equal(s2[0].name, '#LN')
    assert.equal(s2[0].src, ' ')
    assert.equal(s2[1].name, '#TX')
  })

  it('space-matcher-non-ascii', () => {
    let t = tokens({ space: { chars: ' 　' } }, '　 x')
    assert.equal(t[0].name, '#SP')
    assert.equal(t[0].src, '　 ')
  })

  it('comment-suffix-string-array-fn', () => {
    // String suffix terminates a line comment early.
    let def = { hash: { line: true, start: '#', lex: true, suffix: ';' } }
    let t = tokens({ comment: { def } }, 'a#bc;d')
    assert.equal(t[1].name, '#CM')
    assert.equal(t[1].src, '#bc;')
    assert.equal(t[2].name, '#TX')
    assert.equal(t[2].val, 'd')

    // Array suffix (multiple, length sorted).
    def = { hash: { line: true, start: '#', lex: true, suffix: [';', ';;'] } }
    t = tokens({ comment: { def } }, 'a#b;;c')
    assert.equal(t[1].name, '#CM')
    assert.equal(t[1].src, '#b;;')

    // Function (LexMatcher) suffix.
    def = {
      hash: {
        line: true,
        start: '#',
        lex: true,
        suffix: (lex) => {
          if ('!' === lex.src[lex.pnt.sI]) {
            return lex.token('#CM', undefined, '!', lex.pnt)
          }
          return undefined
        },
      },
    }
    t = tokens({ comment: { def } }, 'a#b!c')
    assert.equal(t[1].name, '#CM')
    assert.equal(t[1].src, '#b!')
    assert.equal(t[2].val, 'c')

    // Block comment with a suffix that includes a newline.
    def = {
      multi: { line: false, start: '/*', end: '*/', lex: true, suffix: ';\n' },
    }
    t = tokens({ comment: { def } }, 'a/*b;\nc*/d')
    assert.equal(t[1].name, '#CM')
    assert.equal(t[1].src, '/*b;\n')
    assert.equal(t[1].rI, 1)

    // Block comment with a function suffix.
    def = {
      multi: {
        line: false,
        start: '/*',
        end: '*/',
        lex: true,
        suffix: (lex) => {
          if ('!' === lex.src[lex.pnt.sI]) {
            return lex.token('#CM', undefined, '!', lex.pnt)
          }
          return undefined
        },
      },
    }
    t = tokens({ comment: { def } }, 'a/*b!c*/d')
    assert.equal(t[1].name, '#CM')
    assert.equal(t[1].src, '/*b!')
  })

  it('comment-eatline-and-removal', () => {
    // eatline on a line comment absorbs trailing newlines.
    let def = { hash: { line: true, start: '#', lex: true, eatline: true } }
    let t = tokens({ comment: { def } }, 'a#b\nc')
    assert.equal(t[1].name, '#CM')
    assert.equal(t[1].src, '#b\n')
    assert.equal(t[2].name, '#TX')
    assert.equal(t[2].val, 'c')

    // eatline on a block comment.
    def = {
      multi: {
        line: false,
        start: '/*',
        end: '*/',
        lex: true,
        eatline: true,
      },
    }
    t = tokens({ comment: { def } }, 'a/*b*/c')
    assert.equal(t[1].name, '#CM')

    // Setting a comment def to null / false removes the marker, so
    // '#' is no longer a comment start (nor a text ender).
    t = tokens({ comment: { def: { hash: null, slash: false } } }, 'a#b')
    assert.equal(t[0].name, '#TX')
    assert.equal(t[0].val, 'a#b')
  })

  it('number-value-def-and-sub-fixed', () => {
    // A number-shaped value definition wins over the number matcher.
    let j = tn.make({ value: { def: { 42: { val: 'forty-two' } } } })
    let lex = makeLex({
      src: () => '42',
      cfg: j.internal().config,
      opts: j.options,
      sub: {},
    })
    let tkn = lex.next()
    assert.equal(tkn.name, '#VL')
    assert.equal(tkn.val, 'forty-two')

    // A number-like prefix that fails numeric conversion but is
    // followed by a fixed token: subMatchFixed with a null first.
    let t = tokens({}, '0b_,')
    assert.equal(t[0].name, '#CA')
  })

  it('text-value-regexps-and-modify', () => {
    // Full-match value regexp without val.
    assert.equal(
      summary({ value: { def: { pic: { match: /^[a-z]+~\d+$/ } } } }, 'ab~12'),
      '#VL:ab~12 #ZZ',
    )

    // Full-match value regexp with val transform.
    assert.equal(
      summary(
        {
          value: {
            def: { px: { match: /^(\d+)px$/, val: (res) => +res[1] } },
          },
        },
        '42px',
      ),
      '#VL:42 #ZZ',
    )

    // Consuming value regexp matches the forward source directly.
    assert.equal(
      summary(
        { value: { def: { at: { match: /^@\w+/, consume: true } } } },
        '@abc def',
      ),
      '#VL:@abc #SP #TX:def #ZZ',
    )

    // text.modify pipeline transforms text values.
    assert.equal(
      summary({ text: { modify: (val) => val + '!' } }, 'abc'),
      '#TX:abc! #ZZ',
    )
    assert.equal(
      summary(
        { text: { modify: [(val) => val + '?', (val) => val + '!'] } },
        'xyz',
      ),
      '#TX:xyz?! #ZZ',
    )
  })

  it('lex-sub-callback', () => {
    let j = new Tabnas({
      rule: { start: 'top' },
      fixed: { token: { Ta: 'a' } },
    })
    let Ta = j.token.Ta
    j.rule('top', (rs) => rs.open([{ s: [Ta] }]).close([{ s: ['#ZZ'] }]))

    let seen = []
    j.sub({ lex: (tkn) => seen.push(tkn.name) })
    j.parse('a')
    assert.ok(seen.includes('Ta'))
    assert.ok(seen.includes('#ZZ'))
  })
})
