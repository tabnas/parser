/* Copyright (c) 2026 Richard Rodger, MIT License */
'use strict'

// A parse from a reader, a window of the source at a time, against the
// same text parsed whole (admin ADR-30, doc/chunked-source.md, "Proving
// it"), as rs/tests/window_test.rs holds the Rust engine to it and
// go/window_test.go the Go one. Every input of every parsing file in the
// spec register is parsed by the grammar-free engine and the strict-JSON
// grammar, and the probe and rule-history rows by their own specs, each
// with `lex.window` set: once whole, and from a reader that hands the text
// over in pieces of 1, 2, 3, 7, 64 and 4096 bytes, splitting characters
// past ASCII between pieces, under windows whose margin and block are a
// few units, so that every matcher meets the window's edge. The value,
// the tokens the lexer gives, the rule passes and an error's code,
// position, row, column and token must be the same. An error's excerpt
// must be the same while the window holds the text, and a cut of it where
// the window has dropped the line's start: the one difference a parse
// from a reader shows.

const { describe, it } = require('node:test')
const assert = require('node:assert')
const Fs = require('node:fs')
const Path = require('node:path')

const { Tabnas } = require('..')
const { json } = require('../dist-test/json-plugin')

const SPEC_DIR = Path.join(__dirname, '..', '..', 'test', 'spec')

// A reader that hands its bytes over at most `size` at a time.
function pieces(bytes, size) {
  let at = 0
  return {
    read(buf) {
      const n = Math.min(size, buf.length, bytes.length - at)
      buf.set(bytes.subarray(at, at + n))
      at += n
      return n
    },
  }
}

// A reader that hands over `good` bytes, a few at a time, and then fails.
function failing(bytes, good) {
  let at = 0
  return {
    read(buf) {
      if (good <= at) {
        throw new Error('the disk went away')
      }
      const n = Math.min(3, buf.length, good - at)
      buf.set(bytes.subarray(at, at + n))
      at += n
      return n
    },
  }
}

const utf8 = (text) => Buffer.from(text, 'utf8')

// The spec register's escapes, as its runners decode them.
function decode(field) {
  return field
    .replace(/\\r\\n/g, '\r\n')
    .replace(/\\n/g, '\n')
    .replace(/\\r/g, '\r')
    .replace(/\\t/g, '\t')
}

// A spec file's rows: the line, the `input` column decoded, and the named
// spec document the row's `spec` column asks for, if any.
function rows(file) {
  const text = Fs.readFileSync(Path.join(SPEC_DIR, file), 'utf8')
  const lines = text.split(/\r?\n/)
  const specs = {}
  for (const line of lines) {
    if (line.startsWith('# @spec ')) {
      const rest = line.slice('# @spec '.length)
      const sp = rest.indexOf(' ')
      specs[rest.slice(0, sp)] = rest.slice(sp + 1)
    }
  }
  const header = lines[0].split('\t')
  const input = header.indexOf('input')
  assert.ok(0 <= input, file + ' has no input column')
  const specCol = header.indexOf('spec')
  const out = []
  lines.forEach((line, index) => {
    if (0 === index || '' === line.trim() || line.startsWith('#')) return
    const cols = line.split('\t')
    let doc
    if (0 <= specCol) {
      try {
        const name = JSON.parse(cols[specCol]).spec
        if ('string' === typeof name) {
          doc = specs[name]
          assert.ok(undefined !== doc, file + ': no # @spec ' + name)
        }
      } catch (e) {
        if (e instanceof assert.AssertionError) throw e
      }
    }
    out.push({ line: index + 1, input: decode(cols[input]), doc })
  })
  return out
}

// Every file of the register whose rows are parsed.
const PARSED = [
  'bad-token.tsv',
  'diagnostic.tsv',
  'divergent.tsv',
  'happy.tsv',
  'include-json.tsv',
  'include-json-errors.tsv',
  'include-json-utf8.tsv',
  'include-json-utf8-errors.tsv',
  'lex-ender-array.tsv',
  'lex-string-control.tsv',
  'lex-text-line-terminator.tsv',
  'lex-text-quote.tsv',
  'probe.tsv',
  'repaired.tsv',
  'rule-history.tsv',
]

// The pieces a reader hands its bytes over in.
const PIECES = [1, 2, 3, 7, 64, 4096]

// Windows whose margin and block are a few units, and one that holds every
// input of the register whole.
const WINDOWS = [[1, 1], [3, 2], [64, 64], [4096, 4096]]

// `tn` with `lex.window` set, recording the tokens and rule passes of each
// parse into `log`.
function windowed(tn, log) {
  tn.options({ lex: { window: true } })
  tn.sub({
    lex: (tkn) => {
      log.tokens.push(
        tkn.tin + ' ' + JSON.stringify(tkn.src) + ' ' +
        JSON.stringify(tkn.val) + ' ' + tkn.sI + ' ' + tkn.rI + ':' + tkn.cI,
      )
    },
    ruleDone: (rule, _ctx, done) => {
      log.rules.push(rule.name + ' ' + done.state + ' ' + done.probe)
    },
  })
  return tn
}

const newLog = () => ({ tokens: [], rules: [] })

// What a parse showed: its value, or its error without the excerpt; its
// tokens and passes, taken from `log`, which is empty again after; and the
// error's excerpt, its line and its rendering.
function seen(log, parse) {
  let outcome
  let line
  let rendered
  try {
    const value = parse()
    outcome = 'ok ' + (undefined === value ? 'undefined' : JSON.stringify(value))
  } catch (e) {
    if (null == e || 'function' !== typeof e.toJSON) {
      outcome = 'throw ' + (e && e.name) + ' ' + (e && e.message)
    } else {
      const d = e.toJSON()
      outcome =
        'err ' + d.code + ' at ' + d.pos + ' ' + d.row + ':' + d.col + ' ' +
        JSON.stringify(d.token.src) + ' ' + d.len + ' ' + d.token.name + ' ' +
        d.rule + ' ' + JSON.stringify(d.expected)
      line = d.src
      rendered = e.message
    }
  }
  const tokens = log.tokens
  const rules = log.rules
  log.tokens = []
  log.rules = []
  return { outcome, tokens, rules, line, rendered }
}

// Every input parsed whole and from a reader, under every pieces and
// window size; the count of reader parses compared.
function differential(file, line, input, tn, log) {
  const whole = seen(log, () => tn.parse(input))
  const bytes = utf8(input)
  let checked = 0
  for (const size of PIECES) {
    for (const [margin, block] of WINDOWS) {
      const sizes = { margin, block, chunk: size }
      const read = seen(log, () =>
        tn.parseReaderWithSizes(pieces(bytes, size), sizes))
      const at =
        file + ':' + line + ' ' + JSON.stringify(input) + ' in pieces of ' +
        size + ', margin ' + margin + ', block ' + block
      assert.equal(read.outcome, whole.outcome, at)
      assert.deepEqual(read.tokens, whole.tokens, at)
      assert.deepEqual(read.rules, whole.rules, at)
      if (input.length <= margin) {
        // The window holds the text whole: the same excerpt.
        assert.equal(read.line, whole.line, at)
        assert.equal(read.rendered, whole.rendered, at)
      } else if (undefined !== read.line && undefined !== whole.line) {
        // Cut where the window begins, and read on past its end no
        // further than its margin.
        assert.ok(
          whole.line.includes(read.line),
          at + ': ' + JSON.stringify(read.line) + ' is no cut of ' +
          JSON.stringify(whole.line),
        )
      }
      checked++
    }
  }
  return checked
}

// A grammar that takes every token the lexer gives, with every built-in
// matcher on and fixed tokens of one, two and three characters, under
// `options`: a token stream, so that each matcher's cut, at a window's
// edge too, is what the test compares.
function stream(options) {
  const tn = new Tabnas(options)
  tn.grammar({
    options: {
      rule: { start: 'top' },
      fixed: { token: { '#AR': '=>', '#EL': '...', '#DT': '.', '#EQ': '=' } },
    },
    rule: { top: { open: [{ s: '#ZZ' }, { s: '#AA', r: 'top' }] } },
  })
  return tn
}

// Inputs that reach the matchers the strict-JSON grammar has off:
// comments with suffixes and eaten lines, escapes cut short, numbers in
// every base, multi-character fixed tokens, value keywords, line runs.
const CRAFTED = [
  'a # comment\n b // c\n /* block\n comment */ d',
  '"\\u{1F600}" "\\x41" "\\u0041" \'single\' `multi\nline`',
  '0x1F 0o17 0b101 1_000 1.5e3 -2 +3 .5 0xFF.5 1e 1e+ 0x',
  'a=>b...c.d=e',
  'true false null truex nul',
  'x\r\ny\n\n\nz\r\r\n',
  '/* unterminated',
  '"unterminated',
  '"bad \\q escape"',
  '"\\u{110000}"',
  '"\\u{12"',
  '"\\x4"',
  '"\\u12"',
  'ñ ü € 𝄞 "𝄞" é=>ö',
  'abc --x--> def -- y - z (* c\n *) w',
  '   \t  \n  ',
  '{a:1,b:[2,3]}',
  'a\u2028b',
]

// A long text of records, each line one, past ASCII when `name` is.
function records(count, name, sep) {
  const out = []
  for (let i = 0; i < count; i++) {
    out.push('{"id":' + i + ',"' + name + '":"v' + i + '","tags":["a","b"]}')
  }
  return '[' + out.join(sep) + ']'
}

describe('window', () => {
  it('every parsed row parses the same from a reader', () => {
    const log = newLog()
    const plain = windowed(new Tabnas(), log)
    const strict = windowed(new Tabnas({ plugins: [json] }), log)
    let checked = 0
    PARSED.forEach((file, index) => {
      for (const { line, input, doc } of rows(file)) {
        if (undefined !== doc) {
          const tn = new Tabnas()
          try {
            tn.grammar(JSON.parse(doc))
          } catch (e) {
            // A spec the register expects to refuse installing
            // (ERROR:install) has no parse to compare.
            continue
          }
          checked += differential(file, line, input, windowed(tn, log), log)
        } else {
          checked += differential(file, line, input, plain, log)
          checked += differential(file, line, input, strict, log)
        }
      }
      console.log(
        'window differential: ' + (index + 1) + ' of ' + PARSED.length +
        ' files (' + Math.floor(((index + 1) * 100) / PARSED.length) +
        '%), ' + checked + ' reader parses',
      )
    })
    assert.ok(10000 < checked, '' + checked)
  })

  it('every matcher meets the window edge as a whole text has it', () => {
    // Beyond the register's grammars, whose lexers leave text, comments
    // and most of the fixed tokens off: a token stream over every
    // built-in matcher, with lines cut singly in one and strings
    // abandoned and comments with suffixes in another.
    const log = newLog()
    const grammars = [
      stream(),
      stream({ line: { single: true } }),
      stream({
        string: { abandon: true },
        comment: {
          def: {
            dash: { line: true, start: '--', lex: true, suffix: ['-->', '-'] },
            block: { line: false, start: '(*', end: '*)', lex: true, eatline: true },
          },
        },
      }),
    ].map((tn) => windowed(tn, log))
    const inputs = CRAFTED.slice()
    for (const file of PARSED) {
      for (const { input } of rows(file)) inputs.push(input)
    }
    let checked = 0
    grammars.forEach((tn, index) => {
      inputs.forEach((input, line) => {
        checked += differential('stream ' + index, line, input, tn, log)
      })
      console.log(
        'window stream: ' + (index + 1) + ' of ' + grammars.length +
        ' grammars (' + Math.floor(((index + 1) * 100) / grammars.length) +
        '%), ' + checked + ' reader parses',
      )
    })
    assert.ok(20000 < checked, '' + checked)
  })

  it('a long source parses the same from a reader', () => {
    const log = newLog()
    const tn = windowed(new Tabnas({ plugins: [json] }), log)
    // 5,000 records, some 250 KB: several windows of 64 KiB, at a cost
    // this suite can carry, where rs/tests/window_test.rs takes 20,000.
    for (const name of ['n', 'ñ']) {
      const text = records(5000, name, ',\n')
      const bytes = utf8(text)
      const whole = seen(log, () => tn.parse(text))
      assert.ok(whole.outcome.startsWith('ok '), whole.outcome.slice(0, 80))
      const read = seen(log, () => tn.parseReader(pieces(bytes, 1 << 20)))
      assert.deepEqual(read, whole, name)
      const small = { margin: 16, block: 16, chunk: 7 }
      const cut = seen(log, () =>
        tn.parseReaderWithSizes(pieces(bytes, 5), small))
      assert.equal(cut.outcome, whole.outcome, name)
      assert.deepEqual(cut.tokens, whole.tokens, name)
      assert.deepEqual(cut.rules, whole.rules, name)
    }
  })

  it('a parse from a reader holds a bounded window', () => {
    // A block behind the cursor and a margin or two ahead of it, however
    // long the source, flat from 2,000 records to 20,000: the window's
    // text is what the lexer holds between tokens, `lex.src`.
    const tn = new Tabnas({ plugins: [json] })
    tn.options({ lex: { window: true } })
    let peak = 0
    tn.sub({
      lex: (_tkn, _rule, ctx) => {
        if (peak < ctx.lex.src.length) peak = ctx.lex.src.length
      },
    })
    const sizes = { margin: 1024, block: 1024, chunk: 1024 }
    const peaks = []
    for (const count of [2000, 20000]) {
      const text = records(count, 'name', ',')
      peak = 0
      tn.parseReaderWithSizes(pieces(utf8(text), 1 << 20), sizes)
      assert.ok(peak <= 4 * 1024, count + ' records held ' + peak + ' units')
      peaks.push(peak)
    }
    assert.ok(peaks[1] <= peaks[0] + 256, JSON.stringify(peaks))

    // A grammar that does not declare lex.window reads the reader whole.
    const whole = new Tabnas({ plugins: [json] })
    let held = 0
    whole.sub({
      lex: (_tkn, _rule, ctx) => {
        if (held < ctx.lex.src.length) held = ctx.lex.src.length
      },
    })
    const text = records(2000, 'name', ',')
    whole.parseReaderWithSizes(pieces(utf8(text), 1 << 20), sizes)
    assert.equal(held, text.length)
  })

  it("a long line's excerpt is cut where the window begins", () => {
    // An error on a line longer than the window shows the line from where
    // the window begins, and its caret stays under the error.
    const tn = new Tabnas({ plugins: [json] })
    tn.options({ lex: { window: true }, color: { active: false } })
    const text = '[' + '1,'.repeat(200) + 'x]'
    let whole
    try {
      tn.parse(text)
    } catch (e) {
      whole = e
    }
    let read
    try {
      tn.parseReaderWithSizes(pieces(utf8(text), 4), {
        margin: 8,
        block: 8,
        chunk: 4,
      })
    } catch (e) {
      read = e
    }
    assert.ok(whole && read)
    const w = whole.toJSON()
    const r = read.toJSON()
    assert.deepEqual(
      [r.code, r.pos, r.row, r.col],
      [w.code, w.pos, w.row, w.col],
    )
    assert.equal(w.src, text)
    assert.ok(r.src.length < w.src.length, r.src)
    assert.ok(w.src.endsWith(r.src), r.src)
    // The caret sits under the `x`, on the line as the window shows it.
    const lines = read.message.split('\n')
    const shown = lines.findIndex((l) => l.endsWith(r.src))
    assert.ok(0 <= shown, read.message)
    const x = lines[shown].length - r.src.length + r.src.indexOf('x')
    assert.equal(lines[shown + 1].indexOf('^'), x, read.message)
  })

  it('an unreadable source fails where it fails', () => {
    // A reader that fails, and bytes that are not UTF-8, fail the parse
    // with `unreadable` where the source failed, whether the grammar reads
    // a window at a time or the reader whole.
    const window = new Tabnas({ plugins: [json] })
    window.options({ lex: { window: true } })
    const whole = new Tabnas({ plugins: [json] })
    const fail = (fn) => {
      try {
        fn()
      } catch (e) {
        return e
      }
      assert.fail('the parse should fail')
    }
    const text = utf8('[1,\n2,\n3333,\n4]')
    for (const tn of [window, whole]) {
      const failed = fail(() => tn.parseReader(failing(text, 9)))
      assert.equal(failed.code, 'unreadable')
      const f = failed.toJSON()
      assert.deepEqual([f.pos, f.row, f.col], [9, 3, 3])
      assert.ok(failed.message.includes('the disk went away'), failed.message)
      assert.equal(f.token.src, 'the disk went away')

      const bytes = Buffer.from([...utf8('[1,\n"a'), 0xff, ...utf8('b"]')])
      const invalid = fail(() =>
        tn.parseReaderWithSizes(pieces(bytes, 2), {
          margin: 2,
          block: 2,
          chunk: 2,
        }))
      assert.equal(invalid.code, 'unreadable')
      const i = invalid.toJSON()
      assert.deepEqual([i.pos, i.row, i.col], [6, 2, 3])
      assert.equal(i.token.src, 'stream did not contain valid UTF-8')
      assert.equal(
        i.message,
        'the source could not be read: stream did not contain valid UTF-8',
      )

      // A character the source ends inside.
      const cut = utf8('["ñ').subarray(0, 3)
      const ended = fail(() => tn.parseReader(pieces(cut, 4096)))
      assert.equal(ended.code, 'unreadable')
      const c = ended.toJSON()
      assert.deepEqual([c.pos, c.row, c.col], [2, 1, 3])
    }
  })

  it('a source that fails before its first character is not empty', () => {
    // An empty source parses as an empty one; one that fails at once
    // fails with `unreadable`, at the start.
    const tn = new Tabnas({ plugins: [json] })
    tn.options({ lex: { window: true, empty: true, emptyResult: 'E' } })
    assert.equal(tn.parseReader(pieces(new Uint8Array(0), 8)), 'E')
    let failed
    try {
      tn.parseReader(failing(new Uint8Array(0), 0))
    } catch (e) {
      failed = e
    }
    assert.equal(failed.code, 'unreadable')
    assert.deepEqual(
      [failed.toJSON().pos, failed.toJSON().row, failed.toJSON().col],
      [0, 1, 1],
    )
  })

  it('a reader is a thing with a read method', () => {
    const tn = new Tabnas({ plugins: [json] })
    assert.throws(() => tn.parseReader('[1]'), TypeError)
    assert.throws(() => tn.parseReader(null), TypeError)
    // A count a read cannot have ends the source there, as a failed read.
    tn.options({ lex: { window: true } })
    let failed
    try {
      tn.parseReader({ read: () => -1 })
    } catch (e) {
      failed = e
    }
    assert.equal(failed.code, 'unreadable')
  })

  it('a byte order mark is the source\'s own character', () => {
    // As Rust's UTF-8 decoding and a parse of the text keep it.
    const tn = new Tabnas({ plugins: [json] })
    tn.options({ lex: { window: true } })
    const text = '﻿[1]'
    const outcome = (fn) => {
      try {
        return 'ok ' + JSON.stringify(fn())
      } catch (e) {
        return 'err ' + e.code + ' ' + e.toJSON().pos
      }
    }
    const whole = outcome(() => tn.parse(text))
    assert.equal(outcome(() => tn.parseReader(pieces(utf8(text), 1))), whole)
    assert.equal(
      outcome(() => new Tabnas({ plugins: [json] }).parseReader(
        pieces(utf8(text), 1))),
      whole,
    )
  })

  it('lex.window survives a merge', () => {
    const tagged = (tag, window) =>
      new Tabnas({ plugins: [json], tag, lex: { window } })
    const merged = (a, b) => a.merge(b).internal().config.lex.window
    assert.equal(merged(tagged('A', true), tagged('B', false)), true)
    assert.equal(merged(tagged('A', false), tagged('B', true)), true)
    assert.equal(merged(tagged('A', false), tagged('B', false)), false)
  })

  it("a matcher reads the window, counting from the window's start", () => {
    // In a parse from a reader a window at a time, a matcher sees the
    // window: `lex.src` is its text, and the point, its length included,
    // counts from its start, so that `lex.src[lex.pnt.sI]` is the
    // character under the cursor as in a parse of the whole text, where
    // the length is the source's (DIVERGENCE.md, "What a parse from a
    // reader shows of its source"). The tokens it makes carry the
    // source's positions.
    const probed = (log) => {
      const tn = new Tabnas()
      tn.grammar({
        clear: true,
        options: {
          lex: {
            window: true,
            match: {
              probe: {
                order: 1,
                make: () => (lex) => {
                  log.push({
                    len: lex.pnt.len,
                    held: lex.src.length,
                    at: lex.src[lex.pnt.sI],
                  })
                  return undefined
                },
              },
            },
          },
          rule: { start: 'top' },
        },
        rule: { top: { open: [{ s: '#TX', r: 'top' }, { s: '#ZZ' }] } },
      })
      return tn
    }
    const text = 'a '.repeat(200)
    const read = []
    const tokens = []
    const tn = probed(read)
    tn.sub({ lex: (tkn) => tokens.push(tkn.sI) })
    tn.parseReaderWithSizes(pieces(utf8(text), 4), {
      margin: 4,
      block: 4,
      chunk: 4,
    })
    assert.ok(0 < read.length)
    assert.ok(read.every((r) => r.len === r.held), JSON.stringify(read[0]))
    assert.ok(read.every((r) => r.held < text.length))
    assert.ok(read.every((r) => 'a' === r.at || ' ' === r.at))
    const wholeTokens = []
    const whole = []
    const tw = probed(whole)
    tw.sub({ lex: (tkn) => wholeTokens.push(tkn.sI) })
    tw.parse(text)
    assert.ok(whole.every((r) => r.len === text.length))
    assert.deepEqual(tokens, wholeTokens)
  })

  it("a windowed context's src() is empty", () => {
    // Where a parse of the whole text gives the source: a grammar that
    // declares lex.window reads the text through the lexer's window.
    const sources = []
    const tn = new Tabnas({ plugins: [json] })
    tn.options({ lex: { window: true } })
    tn.sub({ rule: (_rule, ctx) => sources.push(ctx.src()) })
    const text = '{"a":[1,2,{"b":null}]}'
    tn.parseReader(pieces(utf8(text), 4096))
    assert.ok(0 < sources.length)
    assert.ok(sources.every((s) => '' === s), JSON.stringify(sources))
    sources.length = 0
    tn.parse(text)
    assert.ok(sources.every((s) => text === s))
  })

  it('a grammar that reads its source whole reads a reader whole', () => {
    // No lex.window, error recovery, or a parser.start: the reader is read
    // to its end first, and the text parsed.
    const text = '{"a":[1,2,{"b":null}]}'
    const value = JSON.parse(text)
    assert.deepEqual(
      new Tabnas({ plugins: [json] }).parseReader(pieces(utf8(text), 3)),
      value,
    )
    const recovering = new Tabnas({ plugins: [json] })
    recovering.options({ lex: { window: true }, parse: { recover: { enabled: true } } })
    assert.deepEqual(
      recovering.parseReader(pieces(utf8(text), 3)),
      recovering.parse(text),
    )
    const started = new Tabnas({
      lex: { window: true },
      parser: { start: (src) => 'parsed:' + src },
    })
    assert.equal(started.parseReader(pieces(utf8(text), 3)), 'parsed:' + text)
  })
})
