/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */
'use strict'

// A binary-format grammar built directly on the bare tabnas engine — the
// worked example in doc/guide.md, and the canonical counterpart to the
// Go go/binarygrammar_test.go and Rust rs/tests/binary_grammar_test.rs
// suites (both of which name this file as their mirror).
//
// The engine is byte-agnostic: a JS string is an array of 16-bit code
// units, and latin1 maps bytes 0..255 onto code units 0..255 one-for-one,
// so `buf.toString('latin1')` hands the lexer the bytes unchanged. Every
// position the engine tracks (Point.sI, Token.sI, Token.len) is then a
// byte offset.
//
// The format below (magic, count, sentinel-terminated records) exercises
// each field shape a binary grammar needs:
//
//   magic    "TBN1"               fixed-width literal
//   count    u16 big-endian       fixed-width integer
//   records  until the sentinel   {
//     id     u8                   fixed-width integer
//     name   NUL-terminated       TERMINATION MARKER
//     len    LEB128 varint        self-terminating variable width
//     data   `len` bytes          VARIABLE LENGTH SUPPLIED BY THE RULE
//   }
//   end      0x00 0xFF            two-byte sentinel
//
// The three ports build byte-identical input and assert byte-identical
// output; keep them in step.

const { describe, it } = require('node:test')
const assert = require('node:assert')

const { Tabnas } = require('..')

// --- byte helpers ---------------------------------------------------

const hex = (s) =>
  Array.from(s, (c) => c.charCodeAt(0).toString(16).padStart(2, '0')).join('')

// LEB128, the self-terminating varint: 7 bits per byte, high bit set on
// every byte but the last.
const varint = (n) => {
  let out = ''
  do {
    let b = n & 0x7f
    n = Math.floor(n / 128)
    if (0 < n) b |= 0x80
    out += String.fromCharCode(b)
  } while (0 < n)
  return out
}

const u16be = (n) => String.fromCharCode((n >>> 8) & 0xff, n & 0xff)

const record = (id, name, data) =>
  String.fromCharCode(id) + name + '\u0000' + varint(data.length) + data

const SENTINEL = '\u0000\u00ff'

// The widest LEB128 encoding a JS number holds exactly: 10 groups of 7
// bits, matching the Go and Rust mirrors.
const VARINT_MAX_BYTES = 10

const file = (records) =>
  'TBN1' + u16be(records.length) + records.join('') + SENTINEL

// --- the grammar ----------------------------------------------------

function binaryPlugin(tn) {
  tn.options({
    rule: { start: 'file', exclude: 'tabnas,imp' },

    // Nothing in a binary stream is ignorable: a byte that means
    // nothing here means something at the next offset.
    tokenSet: { IGNORE: [null, null, null] },

    // Switch off every text-oriented builtin, AND drop it from the
    // matcher pipeline. `lex: false` alone leaves the matcher installed
    // and called once per token only to decline.
    fixed: { lex: false }, space: { lex: false }, line: { lex: false },
    text: { lex: false }, number: { lex: false }, comment: { lex: false },
    string: { lex: false }, value: { lex: false },
    lex: {
      match: {
        fixed: null, space: null, line: null,
        string: null, comment: null, number: null, text: null,
      },
    },

    // Byte offsets, not rows and columns: a binary stream has no lines.
    error: { unexpected: 'no field matches at byte offset {sI}' },

    // Matchers go under `match.token`, NOT `lex.match`. Only the former
    // is gated on the rule's expected-token column, and that gating is
    // what makes binary parsable at all: the bytes do not say what they
    // are, so the grammar has to. Declaration order fixes tin order,
    // which is the tie-break within a column — #END before #U8 so the
    // sentinel is tried before the catch-all byte.
    match: {
      lex: true,
      token: {
        // Fixed-width literal.
        '#MAGIC': function magic(lex) {
          const pnt = lex.pnt
          if (!lex.src.startsWith('TBN1', pnt.sI)) return undefined
          const tkn = lex.token('#MAGIC', 'TBN1', 'TBN1', pnt)
          pnt.sI += 4
          pnt.cI += 4
          return tkn
        },

        // Two-byte sentinel. Lower tin than #U8, so it wins the column.
        '#END': function end(lex) {
          const pnt = lex.pnt
          if (pnt.len < pnt.sI + 2) return undefined
          if (0x00 !== lex.src.charCodeAt(pnt.sI)) return undefined
          if (0xff !== lex.src.charCodeAt(pnt.sI + 1)) return undefined
          const tkn = lex.token('#END', true, undefined, pnt,
                                undefined, undefined, 2)
          pnt.sI += 2
          pnt.cI += 2
          return tkn
        },

        // Fixed-width integers.
        '#U16': function u16(lex) {
          const pnt = lex.pnt
          if (pnt.len < pnt.sI + 2) return undefined
          const val =
            (lex.src.charCodeAt(pnt.sI) << 8) | lex.src.charCodeAt(pnt.sI + 1)
          const tkn = lex.token('#U16', val, undefined, pnt,
                                undefined, undefined, 2)
          pnt.sI += 2
          pnt.cI += 2
          return tkn
        },

        '#U8': function u8(lex) {
          const pnt = lex.pnt
          if (pnt.len <= pnt.sI) return undefined
          const tkn = lex.token('#U8', lex.src.charCodeAt(pnt.sI), undefined,
                                pnt, undefined, undefined, 1)
          pnt.sI += 1
          pnt.cI += 1
          return tkn
        },

        // Self-terminating variable width: the high bit says "more".
        //
        // A malformed length declines like any other unmatched field
        // rather than producing a garbage value: past VARINT_MAX_BYTES
        // the encoding exceeds what a JS number holds exactly. The Go
        // and Rust mirrors carry the same bound, where the same input
        // truncates silently and panics respectively.
        '#VARINT': function vi(lex) {
          const pnt = lex.pnt
          let i = pnt.sI
          let val = 0
          let shift = 0
          let b
          do {
            if (pnt.len <= i || VARINT_MAX_BYTES <= i - pnt.sI) return undefined
            b = lex.src.charCodeAt(i++)
            val += (b & 0x7f) * Math.pow(2, shift)
            if (!Number.isSafeInteger(val)) return undefined
            shift += 7
          } while (0 !== (b & 0x80))
          const tkn = lex.token('#VARINT', val, undefined, pnt,
                                undefined, undefined, i - pnt.sI)
          pnt.cI += tkn.len
          pnt.sI = i
          return tkn
        },

        // Termination marker: run up to and including the next NUL. The
        // token's `len` covers the terminator; its `val` does not.
        '#CSTR': function cstr(lex) {
          const pnt = lex.pnt
          const end = lex.src.indexOf('\u0000', pnt.sI)
          if (end < 0) return undefined
          const tkn = lex.token('#CSTR', lex.src.substring(pnt.sI, end),
                                undefined, pnt, undefined, undefined,
                                end - pnt.sI + 1)
          pnt.cI += tkn.len
          pnt.sI = end + 1
          return tkn
        },

        // Variable length supplied by the RULE, not by the bytes. `k`
        // propagates from the rule that lexed the length; `u` would not.
        // No `val` and no `src`: the token is a bare (sI, len) span, so
        // nothing is copied until someone reads it.
        '#DATA': function data(lex, rule) {
          const keep = rule && rule.rawk()
          const n = keep ? keep.datalen : undefined
          if (null == n || n < 0) return undefined
          const pnt = lex.pnt
          if (pnt.len < pnt.sI + n) return undefined
          const tkn = lex.token('#DATA', undefined, undefined, pnt,
                                undefined, undefined, n)
          pnt.sI += n
          pnt.cI += n
          return tkn
        },
      },
    },
  })

  const { MAGIC, END, U16, U8, VARINT, CSTR, DATA, ZZ } = tn.token

  tn.rule('file', (rs) =>
    rs
      .open([
        {
          s: [MAGIC, U16],
          a: (r) => {
            r.node = { magic: r.o[0].val, count: r.o[1].val, records: [] }
          },
          p: 'records',
        },
      ])
      .close([{ s: [ZZ] }]),
  )

  // An alternate that leads to a fetch must NAME the token it expects,
  // or column gating locks every matcher out and the fetch yields #BD.
  // `{ p: 'rec' }` alone names nothing; `{ s: [U8], b: 1, p: 'rec' }`
  // matches the byte, pushes it back, and hands it to the child.
  const loop = (rs) =>
    rs
      .open([{ s: [END] }, { s: [U8], b: 1, p: 'rec' }])
      .close([{ s: [END] }, { s: [U8], b: 1, r: 'records' }])
      .bc((r) => {
        if (r.child && null != r.child.node) r.node.records.push(r.child.node)
      })

  tn.rule('records', loop)

  tn.rule('rec', (rs) =>
    rs
      .open([
        {
          s: [U8, CSTR, VARINT],
          a: (r) => {
            r.node = { id: r.o[0].val, name: r.o[1].val, len: r.o[2].val }
            r.k.datalen = r.o[2].val
          },
          p: 'data',
        },
      ])
      .close([{ a: (r) => (r.node.data = r.child.node) }]),
  )

  tn.rule('data', (rs) =>
    rs.open([{ s: [DATA], a: (r) => (r.node = hex(r.o[0].src)) }]),
  )
}

// --- the shared corpus ----------------------------------------------
//
// Byte-identical in all three ports. `alpha` carries an embedded NUL and
// an 0xFF inside its payload, which is what proves the stream is read as
// bytes and not as text; the third record forces a two-byte LEB128 length.

const CORPUS = file([
  record(1, 'alpha', '\u0000\u0001\u00ff'),
  record(2, '', ''),
  record(200, 'x'.repeat(200), 'y'.repeat(200)),
])

const EXPECTED = {
  magic: 'TBN1',
  count: 3,
  records: [
    { id: 1, name: 'alpha', len: 3, data: '0001ff' },
    { id: 2, name: '', len: 0, data: '' },
    { id: 200, name: 'x'.repeat(200), len: 200, data: '79'.repeat(200) },
  ],
}

describe('binary-grammar', function () {
  it('parses the shared corpus', () => {
    const tn = new Tabnas({ plugins: [binaryPlugin] })
    assert.deepEqual(tn.parse(CORPUS), EXPECTED)
  })

  it('reads every byte value transparently', () => {
    // All 256 byte values survive the latin1 crossing unchanged, and a
    // payload containing every one of them round-trips through a token
    // span. 0x00 is excluded from the NAME (it terminates it) but not
    // from the payload.
    const all = Array.from({ length: 256 }, (_, i) =>
      String.fromCharCode(i)).join('')
    const tn = new Tabnas({ plugins: [binaryPlugin] })
    const out = tn.parse(file([record(7, 'all', all)]))
    assert.equal(out.records.length, 1)
    assert.equal(out.records[0].len, 256)
    assert.equal(out.records[0].data, hex(all))
    // And the bytes really are 0x00..0xFF.
    assert.equal(out.records[0].data.slice(0, 6), '000102')
    assert.equal(out.records[0].data.slice(-6), 'fdfeff')
  })

  it('carries the length from the rule to the lexer', () => {
    // The payload matcher has no way to know where the field ends: the
    // length lives in a token the parser has already consumed. Change
    // only the declared length and the same bytes cut differently.
    const tn = new Tabnas({ plugins: [binaryPlugin] })
    const body = 'abcdef'
    for (const n of [0, 1, 3, 6]) {
      const src = 'TBN1' + u16be(1) + '\u0001' + 'n' + '\u0000' +
        varint(n) + body.slice(0, n) + SENTINEL
      const out = tn.parse(src)
      assert.equal(out.records[0].len, n)
      assert.equal(out.records[0].data, hex(body.slice(0, n)))
    }
  })

  it('token spans are byte offsets into the source', () => {
    const tn = new Tabnas({ plugins: [binaryPlugin] })
    const seen = []
    tn.sub({ lex: (tkn) => seen.push([tkn.name, tkn.sI, tkn.len]) })
    tn.parse(file([record(1, 'ab', '\u00ff\u00fe')]))
    // TBN1(0..4) count(4..6) id(6) name "ab\0"(7..10) len(10) data(11..13)
    const byName = (n) => seen.filter((s) => s[0] === n)
    assert.deepEqual(byName('#MAGIC')[0], ['#MAGIC', 0, 4])
    assert.deepEqual(byName('#U16')[0], ['#U16', 4, 2])
    assert.deepEqual(byName('#U8')[0], ['#U8', 6, 1])
    assert.deepEqual(byName('#CSTR')[0], ['#CSTR', 7, 3])
    assert.deepEqual(byName('#VARINT')[0], ['#VARINT', 10, 1])
    assert.deepEqual(byName('#DATA')[0], ['#DATA', 11, 2])
  })

  it('reports byte offsets on malformed input', () => {
    const tn = new Tabnas({ plugins: [binaryPlugin] })

    // Bad magic: nothing matches at offset 0.
    assert.throws(() => tn.parse('XXXX' + u16be(0) + SENTINEL), (e) => {
      assert.equal(e.code, 'unexpected')
      assert.match(e.message, /byte offset 0/)
      return true
    })

    // Truncated payload: the length says 9, only 3 bytes remain. The
    // #DATA matcher declines and the column admits nothing else.
    const short = 'TBN1' + u16be(1) + '\u0001' + 'n' + '\u0000' +
      varint(9) + 'abc'
    assert.throws(() => tn.parse(short), (e) => {
      assert.equal(e.code, 'unexpected')
      return true
    })

    // Unterminated name: no NUL anywhere after it.
    const noterm = 'TBN1' + u16be(1) + '\u0001' + 'name-with-no-nul'
    assert.throws(() => tn.parse(noterm), (e) => {
      assert.equal(e.code, 'unexpected')
      return true
    })
  })

  it('structured diagnostics carry pos and the expected column', () => {
    const tn = new Tabnas({ plugins: [binaryPlugin] })
    let diag
    try {
      tn.parse('XXXX' + u16be(0) + SENTINEL)
    } catch (e) {
      diag = JSON.parse(JSON.stringify(e))
    }
    assert.equal(diag.code, 'unexpected')
    assert.equal(diag.pos, 0)          // byte offset, not a character index
    assert.equal(diag.rule, 'file')
    assert.deepEqual(diag.expected, ['#MAGIC'])
  })

  it('declines an oversized length instead of reading a garbage one', () => {
    // Sixteen continuation bytes: past VARINT_MAX_BYTES and past what a
    // JS number holds exactly. The matcher declines, the column admits
    // nothing else, and the parse fails as a format error.
    const tn = new Tabnas({ plugins: [binaryPlugin] })
    const src = 'TBN1' + u16be(1) + '\u0001' + 'n' + '\u0000' +
      '\u00ff'.repeat(16) + '\u0000'
    assert.throws(() => tn.parse(src), (e) => {
      assert.equal(e.code, 'unexpected')
      return true
    })

    // The bound is not so tight that a legitimate wide length fails.
    assert.equal(varint(0x10000000).length, 5)
    const wide = 'TBN1' + u16be(1) + '\u0001' + 'n' + '\u0000' +
      varint(200) + 'z'.repeat(200) + SENTINEL
    assert.equal(tn.parse(wide).records[0].len, 200)
  })

  it('leaves the built-in matchers out of the pipeline', () => {
    const tn = new Tabnas({ plugins: [binaryPlugin] })
    const pipeline = tn
      .internal()
      .config.lex.match.map((m) => m.matcher)
    assert.deepEqual(pipeline, ['match'])
  })
})

// --- sub-byte fields -------------------------------------------------
//
// The engine has no bit cursor: Point.sI counts bytes. A bitfield
// grammar keeps the bit offset on ctx.u and emits tokens whose `len`
// counts only the bytes the field fully crossed, so a field that sits
// inside a byte already consumed produces a zero-length token.

function bitfieldPlugin(tn) {
  tn.options({
    rule: { start: 'ipv4', exclude: 'tabnas,imp' },
    tokenSet: { IGNORE: [null, null, null] },
    fixed: { lex: false }, space: { lex: false }, line: { lex: false },
    text: { lex: false }, number: { lex: false }, comment: { lex: false },
    string: { lex: false }, value: { lex: false },
    lex: {
      match: {
        fixed: null, space: null, line: null,
        string: null, comment: null, number: null, text: null,
      },
    },
    match: {
      lex: true,
      token: {
        '#BITS': function bits(lex, rule) {
          const keep = rule && rule.rawk()
          const width = keep ? keep.bits : undefined
          if (null == width) return undefined
          const ctx = lex.ctx
          const pnt = lex.pnt
          let bit = ctx.u.bit || 0        // bits already taken from src[sI]
          let need = width
          let val = 0
          let i = pnt.sI
          while (0 < need) {
            if (pnt.len <= i) return undefined
            const avail = 8 - bit
            const take = avail < need ? avail : need
            const byte = lex.src.charCodeAt(i)
            val = (val << take) | ((byte >> (avail - take)) & ((1 << take) - 1))
            bit += take
            need -= take
            if (8 === bit) {
              bit = 0
              i++
            }
          }
          ctx.u.bit = bit
          const tkn = lex.token('#BITS', val, undefined, pnt,
                                undefined, undefined, i - pnt.sI)
          pnt.cI += tkn.len
          pnt.sI = i
          return tkn
        },
      },
    },
  })

  const { BITS, ZZ } = tn.token

  // version(4) ihl(4) dscp(6) ecn(2) — the first two bytes of an IPv4
  // header, four fields across two bytes.
  const field = (name, width, next) =>
    tn.rule(name, (rs) =>
      rs
        .bo((r) => (r.k.bits = width))
        .open([
          {
            s: [BITS],
            a: (r) => {
              r.node = r.parent.node
              r.node[name] = r.o[0].val
            },
          },
        ])
        .close([next ? { r: next } : { s: [ZZ] }]),
    )

  tn.rule('ipv4', (rs) =>
    rs.bo((r) => (r.node = {})).open([{ p: 'version' }]).close([{ s: [ZZ] }]))

  field('version', 4, 'ihl')
  field('ihl', 4, 'dscp')
  field('dscp', 6, 'ecn')
  field('ecn', 2, null)
}

describe('binary-grammar-bitfields', function () {
  it('reads sub-byte fields across a byte boundary', () => {
    const tn = new Tabnas({ plugins: [bitfieldPlugin] })
    // 0x45 0xb8 = 0100 0101 1011 1000
    //   version 0100 = 4, ihl 0101 = 5, dscp 101110 = 46, ecn 00 = 0
    assert.deepEqual(tn.parse('\u0045\u00b8'), {
      version: 4, ihl: 5, dscp: 46, ecn: 0,
    })
    // 0x60 0x0f = 0110 0000 0000 1111
    //   version 6, ihl 0, dscp 000011 = 3, ecn 11 = 3
    assert.deepEqual(tn.parse('\u0060\u000f'), {
      version: 6, ihl: 0, dscp: 3, ecn: 3,
    })
  })
})
