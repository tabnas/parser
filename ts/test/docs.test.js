/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

// The fast half of the prose gate (doc/STYLE-GUIDE.md).
//
// Vale is the other half, run by `make prose` wherever vale is installed.
// The two read ONE file list (ts/scripts/gated-docs.cjs) and ONE banned list
// (.vale/styles/config/vocabularies/Tabnas/reject.txt) so neither can
// drift from the other.
//
// What lives here rather than in Vale, and why:
//
//   - The banned list is matched ACROSS A LINE WRAP. These pages wrap
//     near 72 columns and most of the list is multi-word, so a phrase
//     split over two lines is invisible to Vale, which matches within a
//     line. Paragraphs are joined before matching here.
//   - The em dash ban applies to prose only. Literal code and quoted
//     output keep their punctuation, so the check runs after stripping
//     fences and code spans -- something a Vale rule cannot express.
//   - "We" is allowed in tutorials only, and "I" nowhere. Vale cannot
//     say "only in tutorials"; this file knows which page is which.

const Fs = require('node:fs')
const Path = require('node:path')
const Assert = require('node:assert')
const { describe, test } = require('node:test')

const { gatedDocs, tutorials } = require('../scripts/gated-docs.cjs')

const REPO = Path.join(__dirname, '..', '..')
// Patterns only, one per line, no comments. Vale has NO comment syntax
// in a vocabulary file: a line beginning with `#` is compiled as a
// pattern and then matches a bare `#` in prose, so `tabnas/bnf#13` was
// reported as a banned phrase. The commentary lives in the style guide,
// which `the-guide-covers-every-banned-pattern` holds to this file.
const REJECT = Path.join(
  REPO, '.vale', 'styles', 'config', 'vocabularies', 'Tabnas', 'reject.txt')
const GUIDE = Path.join(REPO, 'doc', 'STYLE-GUIDE.md')


function lf(s) {
  return s.replace(/\r\n/g, '\n')
}


// The banned list, read from the file Vale reads. Every line is a regex,
// matched case-insensitively on word boundaries, exactly as Vale.Avoid
// matches it.
//
// A `#` line is REFUSED rather than skipped. Vale has no comment syntax
// in a vocabulary file, so it reads one as a pattern: a lone `#` became
// a banned phrase and reported `tabnas/bnf#13` as an error. Skipping it
// on this side only would leave the two halves banning different things,
// which is the one thing sharing the file is for.
function loadBanned() {
  const lines = lf(Fs.readFileSync(REJECT, 'utf8'))
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => '' !== l)
  const comments = lines.filter((l) => l.startsWith('#'))
  if (0 < comments.length) {
    throw new Error(
      `${REJECT} has comment lines, and Vale reads them as patterns: ` +
      comments.join(' / '))
  }
  return lines.map((src) => [new RegExp(`\\b(?:${src})\\b`, 'gi'), src])
}


const BANNED = loadBanned()

// An emptied reject.txt would leave `no-banned-phrases-in-prose` and
// `the-guide-covers-every-banned-pattern` iterating nothing and passing.
// A gate that checks nothing has to say so.
if (0 === BANNED.length) {
  throw new Error(`${REJECT} loaded no patterns; the phrase gate is off`)
}

// A code span's delimiter is a RUN of backticks, and the run length
// decides where it ends. Stripping pairs of single backticks left the
// contents of ``a `b` c`` in the prose stream, so a literal could fail
// the pronoun or banned-phrase checks the guide exempts it from.
//
// It stops at a newline. Without that, an unpaired backtick runs to the
// next one however many lines away and takes every newline between them
// with it, gluing the page into one line that every per-line check then
// reports.
//
// Both runs must be MAXIMAL. `(`+)` backtracks, so four opening
// backticks against three closing ones shrank to a run of three and ate
// the fourth as content: ````not just``` is prose by CommonMark, and
// the phrase in it vanished from the prose stream.
const CODE_SPAN = /(?<!`)(`+)(?!`)(?:[^`\n]|(?!\1)`)*(?<!`)\1(?!`)/g

// Emoji, not "any symbol in these blocks". The old range was wrong both
// ways: it flagged the text-presentation symbols documentation uses
// (the bare warning sign, a check mark, an arrow) and it missed every
// emoji built from a variation selector, a keycap, or a flag's regional
// indicators, none of which sit in it.
// A span that wraps once. CODE_SPAN stops at a newline, so that one was
// left in the prose stream whole: the boolean NOT inside one counted as
// an exclamation mark. One newline is the bound that keeps an unpaired
// backtick from running away, and the newline itself is kept so a
// reported line still points at the author's line.
const CODE_WRAP =
  /(?<!`)(`+)(?!`)(?:[^`\n]|(?!\1)`)*\n(?:[^`\n]|(?!\1)`)*(?<!`)\1(?!`)/g

const EMOJI = /\p{Emoji_Presentation}|\uFE0F|\u20E3|[\u{1F1E6}-\u{1F1FF}]/u

// `I` is a pronoun only capitalised, because a lone lowercase `i` is
// the one in `i.e.` or an index. `me`, `my` and `mine` are pronouns
// however they fall, the start of a sentence or a heading included,
// which a single case-sensitive pattern missed. `I/O` is neither.
const FIRST_I = /\b(?:I(?!\/)|I'\w+)\b/
const FIRST_MY = /\b(?:me|my|mine)\b/i

// Every exclamation mark except the two that are punctuation for
// something else: the `!=` of an operator and the `!` that opens an
// image. Asking for a word character before the mark, as this did,
// scored `Really?!`, `Great!!` and `Voilà!` at nothing.
const EXCLAMATION = /!(?![=[])/g

function firstSingular(line) {
  return FIRST_I.test(line) || FIRST_MY.test(line)
}


// A bold LABEL opening a line or a list item is a heading, so `**I**`
// there is an initial rather than a pronoun. The exemption used to
// strip every bold one- or two-letter capital anywhere, which also
// removed the pronoun from `**I** configured the parser`.
function label(line) {
  return line.replace(/^(\s*(?:[-*+]\s+|\d+[.)]\s+)?)\*\*[A-Z]{1,2}\*\*/, '$1')
}

const FENCE_OPEN = /^(\s{0,3})(`{3,}|~{3,})[ \t]*([^`\s]*)[^`]*$/

function fenceCloser(fence) {
  return new RegExp(`^\\s{0,3}${fence[0]}{${fence.length},}\\s*$`)
}


// Blank out every fenced block, keeping the line count so a reported
// line number still opens on the offending line.
function fenceless(md) {
  const lines = lf(md).split('\n')
  const out = [...lines]

  for (let i = 0; i < lines.length; i++) {
    const fm = lines[i].match(FENCE_OPEN)
    if (!fm) {
      continue
    }
    const closer = fenceCloser(fm[2])
    out[i] = ''
    let j = i + 1
    for (; j < lines.length && !closer.test(lines[j]); j++) {
      out[j] = ''
    }
    if (j < lines.length) {
      out[j] = ''
    }
    i = j
  }

  return out.join('\n')
}


// A link TARGET is not prose. `](https://.../en-US/docs/...)` put the
// letters `US` between word boundaries, and the first-person-plural
// check read them as the pronoun. Vale skips link targets; so does this
// now, in the bracketed, angled and bare forms. The link TEXT stays,
// because that is prose a reader sees.
function prose(md) {
  return fenceless(md)
    .replace(/^---\n[\s\S]*?\n---\n/, '')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(CODE_SPAN, '')
    .replace(CODE_WRAP, (m) => m.replace(/[^\n]/g, ''))
    .replace(/\]\([^)\s]*/g, '](')
    .replace(/^\[[^\]]+\]:\s*\S+/gm, '')
    .replace(/<https?:\/\/[^\s<>]*>/g, '')
    .replace(/\bhttps?:\/\/[^\s<>)\]]*[^\s<>)\]!.,;:?*_"']/g, '')
}


// A line that OPENS a block is not a continuation of the line above it,
// even with no blank line between them. Markdown puts list items and
// table rows on adjacent lines, and joining those invented phrases that
// span a cell boundary: a row ending in "worth" ahead of one opening
// "noting" read as `worth noting`.
const BLOCK = /^\s*(?:[-*+] |\d+[.)] |#{1,6} |>|\||`{3,}|~{3,})/


// A line that OPENS a block is not a continuation of the line above it,
// and Markdown needs no blank line between the two. `## Something worth`
// followed by `noting this` joined into one string and reported
// `worth noting`, a phrase neither line contains.
//
// A list item or a blockquote keeps its wrapped continuation lines. A
// heading, a table row and a rule are one line each, so they close as
// well as open.
const OPENS = /^\s*(?:[-*+] |\d+[.)] |#{1,6} |>|\|[^|]*\||`{3,}|~{3,})/
const CLOSES = /^\s*(?:#{1,6} |\|[^|]*\||(?:[-*_] *){3,}$)/


// A paragraph, joined for matching, with each piece's physical line
// kept so a hit can be reported where the author will find it.
function logical(text) {
  const out = []
  let pieces = []
  let starts = []
  let lines = []
  let at = 0

  const flush = () => {
    if (0 < pieces.length) {
      out.push({ text: pieces.join(' '), starts, lines })
      pieces = []
      starts = []
      lines = []
      at = 0
    }
  }

  lf(text).split('\n').forEach((line, i) => {
    if ('' === line.trim()) {
      flush()
      return
    }
    if (BLOCK.test(line)) {
      flush()
    }
    if (OPENS.test(line)) {
      flush()
    }
    const piece = line.trim().replace(/\s+/g, ' ')
    starts.push(at)
    lines.push(i + 1)
    pieces.push(piece)
    at += piece.length + 1
    if (CLOSES.test(line)) {
      flush()
    }
  })
  flush()

  return out
}


function lineAt(para, index) {
  let k = 0
  for (let i = 0; i < para.starts.length; i++) {
    if (para.starts[i] <= index) {
      k = i
    }
  }
  return { line: para.lines[k] }
}


// Where a repository keeps code a page can quote. A short list rather
// than a walk of the tree: node_modules and dist hold copies, and their
// text is the source of nothing.
const SOURCE_ROOTS = [
  'ts/src', 'ts/scripts', 'src', 'lib', 'go', 'rs/src', 'scripts',
]
const SOURCE_EXT = /\.(ts|go|js|mjs|cjs|rs|abnf)$/
// A test file is not what the program prints. hoover's tutorial walks
// through the same mini-grammar its Go test does, comment for comment,
// and that overlap is not a quotation of anything.
const SOURCE_TEST = /(^|[._-])(test|spec)\.[^.]+$|_test\.[^.]+$/


function sourceFiles(dir, out) {
  if (undefined === dir) {
    const all = []
    for (const r of SOURCE_ROOTS) {
      const abs = Path.join(REPO, r)
      if (Fs.existsSync(abs) && Fs.statSync(abs).isDirectory()) {
        sourceFiles(abs, all)
      }
    }
    for (const n of Fs.readdirSync(REPO)) {
      if (n.endsWith('.abnf')) {
        all.push(Path.join(REPO, n))
      }
    }
    return all
  }
  for (const e of Fs.readdirSync(dir, { withFileTypes: true })) {
    const abs = Path.join(dir, e.name)
    if (e.isDirectory()) {
      if (!e.name.startsWith('.') && 'node_modules' !== e.name &&
          'dist' !== e.name && 'dist-test' !== e.name && 'vendor' !== e.name &&
          'test' !== e.name && 'tests' !== e.name) {
        sourceFiles(abs, out)
      }
    }
    else if (SOURCE_EXT.test(e.name) && !SOURCE_TEST.test(e.name)) {
      out.push(abs)
    }
  }
  return out
}


// One line, and no space around the dash, on both sides of the
// comparison: the source holds the message on one line, the page wraps
// it near 72 columns, and a wrap is not a difference in the text.
function flat(s) {
  return lf(s).replace(/\s+/g, ' ').replace(/ ?— ?/g, '—')
}


// Adjacent string literals are joined the way the compiler joins them,
// so a message split over `'...' +` lines reads as the one string the
// program actually emits.
function sourceText(src) {
  return flat(src.replace(/["'`]\s*\+\s*["'`]/g, '').replace(/\\n/g, ' '))
}


function paths() {
  return gatedDocs().map((file) => ({ file, abs: Path.join(REPO, file) }))
}


describe('docs-style', () => {

  // Portable across the fleet: the repos differ in whether they carry a
  // per-runtime doc set or document themselves in the READMEs alone, so
  // this asserts what is true of any of them rather than one layout.
  test('the-gated-set-is-reader-facing-and-covers-the-readmes', () => {
    const files = paths().map((p) => p.file)
    Assert.ok(0 < files.length, 'the gated set is not empty')

    for (const r of ['README.md', 'ts/README.md', 'go/README.md']) {
      if (Fs.existsSync(Path.join(REPO, r))) {
        Assert.ok(files.includes(r), `${r} exists and is gated`)
      }
    }

    // Working documents are out by rule, not by accident.
    const working = files.filter((f) => /feasibility|rust-port|BUGS|REVIEW|DIVERGENCE|STYLE-GUIDE|design/.test(f))
    Assert.deepEqual(working, [],
      `working documents must not be gated: ${working.join(', ')}`)
  })


  test('no-banned-phrases-in-prose', () => {
    const hits = []
    for (const { file, abs } of paths()) {
      for (const para of logical(prose(Fs.readFileSync(abs, 'utf8')))) {
        for (const [re, name] of BANNED) {
          for (const m of para.text.matchAll(re)) {
            if (null == m.index) {
              continue
            }
            const { line } = lineAt(para, m.index)
            const hit = `${file}:${line} "${name}"`
            if (!hits.includes(hit)) {
              hits.push(hit)
            }
          }
        }
      }
    }
    Assert.deepEqual(hits, [],
      `banned phrases (doc/STYLE-GUIDE.md):\n${hits.join('\n')}`)
  })


  // Literal code and quoted output keep their punctuation. The rule
  // applies to prose, using the same stripper as the phrase gate.
  test('no-em-dashes-in-prose', () => {
    const hits = []
    for (const { file, abs } of paths()) {
      prose(Fs.readFileSync(abs, 'utf8'))
        .split('\n')
        .forEach((line, i) => {
          if (line.includes('—')) {
            hits.push(`${file}:${i + 1}: ${line.trim()}`)
          }
        })
    }
    Assert.deepEqual(hits, [],
      `em dashes in prose (doc/STYLE-GUIDE.md):\n${hits.join('\n')}`)
  })


  // A fenced block is not prose, so both halves of the gate strip it
  // before looking: `no-em-dashes-in-prose` runs over `prose()`, and
  // Vale skips code blocks. An edit INSIDE one is therefore invisible
  // to every other check here, and the pass that removed the em dashes
  // rewrote three repositories' quoted error messages with the whole
  // gate green. A page that shows what the program prints has to print
  // what the program prints.
  //
  // Matched on BOTH sides of the dash. Prose that merely shares an
  // opening phrase with a literal (lsp's README shares one with a
  // generated banner) carries on differently, so its tail does not
  // match and it is not a hit.
  test('quoted-output-keeps-the-source-punctuation', () => {
    const faults = []
    const docs = paths().map((p) => (
      { file: p.file, text: flat(Fs.readFileSync(p.abs, 'utf8')) }))

    for (const abs of sourceFiles()) {
      const text = sourceText(Fs.readFileSync(abs, 'utf8'))
      for (let at = text.indexOf('—'); -1 !== at;
        at = text.indexOf('—', at + 1)) {
        const key = text.slice(Math.max(0, at - 40), at)
        const tail = text.slice(at + 1, at + 41)
        if (15 > key.length || 15 > tail.length) {
          continue
        }
        for (const doc of docs) {
          if (doc.text.includes(key + '—' + tail) ||
              !doc.text.includes(key) || !doc.text.includes(tail)) {
            continue
          }
          faults.push(`${doc.file}: quotes ${Path.relative(REPO, abs)}` +
            ` without its em dash: ...${key.slice(-34)} — ${tail.slice(0, 34)}...`)
        }
      }
    }

    Assert.deepEqual(faults, [],
      `quoted output rewritten (doc/STYLE-GUIDE.md):\n${faults.join('\n')}`)
  })

  test('we-appears-only-in-tutorials', () => {
    const allowed = tutorials()
    const hits = []
    for (const { file, abs } of paths()) {
      if (allowed.includes(file)) {
        continue
      }
      prose(Fs.readFileSync(abs, 'utf8'))
        .split('\n')
        .forEach((line, i) => {
          if (/\b(we|we'\w+|us|our|ours)\b/i.test(line)) {
            hits.push(`${file}:${i + 1}: ${line.trim()}`)
          }
        })
    }
    Assert.deepEqual(hits, [],
      `first-person plural outside a tutorial (voice rule 7):\n${
        hits.join('\n')}`)
  })


  // A bold single-letter label is not a pronoun: c/README.md numbers its
  // grammar sections `**A**` ... `**I**`, and the ninth is not first
  // person. Labels are stripped before matching.
  //
  // Nor is the I of `I/O`. A slash is a word boundary, so a plain \bI\b
  // reads "disk I/O" as a pronoun; the lookahead excludes it.
  test('first-person-singular-appears-nowhere', () => {
    const hits = []
    for (const { file, abs } of paths()) {
      prose(Fs.readFileSync(abs, 'utf8'))
        .split('\n')
        .forEach((line, i) => {
          if (firstSingular(label(line))) {
            hits.push(`${file}:${i + 1}: ${line.trim()}`)
          }
        })
    }
    Assert.deepEqual(hits, [],
      `first-person singular (voice rule 7):\n${hits.join('\n')}`)
  })


  // At most one per page, in tutorials only, on a genuine payoff.
  test('exclamation-marks-are-rationed', () => {
    const allowed = tutorials()
    const hits = []
    for (const { file, abs } of paths()) {
      // Not every `!` byte: `!=` is an operator and `![alt](src)` is
      // an image.
      const n = (prose(Fs.readFileSync(abs, 'utf8'))
        .match(EXCLAMATION) || []).length
      if (0 === n) {
        continue
      }
      if (!allowed.includes(file)) {
        hits.push(`${file}: ${n} outside a tutorial`)
      }
      else if (1 < n) {
        hits.push(`${file}: ${n}, the ration is one`)
      }
    }
    Assert.deepEqual(hits, [],
      `exclamation marks (doc/STYLE-GUIDE.md):\n${hits.join('\n')}`)
  })


  // Explicit ranges rather than \p{Extended_Pictographic}, which also
  // covers technical symbols: `↔` (U+2194) is how the comparison pages
  // write "TypeScript versus Go", and it is not decoration.
  //
  // Reads prose, not the raw file, so an emoji inside a code span stays:
  // go/doc/differences.md tabulates how each runtime matches a literal
  // `😀`, which is test data exactly as quoted output is.
  test('no-emoji', () => {
    const hits = []
    for (const { file, abs } of paths()) {
      prose(Fs.readFileSync(abs, 'utf8'))
        .split('\n')
        .forEach((line, i) => {
          if (EMOJI.test(line)) {
            hits.push(`${file}:${i + 1}: ${line.trim()}`)
          }
        })
    }
    Assert.deepEqual(hits, [],
      `emoji in documentation (doc/STYLE-GUIDE.md):\n${hits.join('\n')}`)
  })


  // The guide claims two gates. If either name stops appearing the
  // claim has gone stale, and a reader following it lands nowhere.
  // A check is a claim about what it rejects, and a clean run over
  // well-written pages cannot tell a working rule from a broken one.
  // Every case here is a defect a review found in these rules after
  // they were installed in every repository in the fleet.
  test('the-checks-catch-what-they-claim', () => {
    const faults = []
    const claim = (ok, what) => {
      if (!ok) {
        faults.push(what)
      }
    }

    // A code span's delimiter is a RUN of backticks.
    claim('' === '``a `b` c``'.replace(CODE_SPAN, ''), 'multi-backtick span')
    claim('x  y' === 'x `my` y'.replace(CODE_SPAN, ''), 'single-backtick span')
    claim('````not just```' === '````not just```'.replace(CODE_SPAN, ''),
      'a shorter closing run is not a code span')
    claim('a \n b' === 'a `x !(y ==\nz)` b'.replace(CODE_SPAN, '')
      .replace(CODE_WRAP, (m) => m.replace(/[^\n]/g, '')).replace(/ +/g, ' '),
      'a span that wraps once is still a span')
    claim('an odd ` mark\nand my line' ===
      'an odd ` mark\nand my line'.replace(CODE_SPAN, ''),
      'an unpaired backtick does not swallow the next line')

    // Emoji, not "symbol in these blocks".
    for (const text of ['\u26A0', '\u2713', '\u2194', '\u2020']) {
      claim(!EMOJI.test(text), `text-presentation symbol ${text} is not emoji`)
    }
    for (const text of ['\u{1F680}', '1\uFE0F\u20E3', '\u{1F1EC}\u{1F1E7}',
      '\u00A9\uFE0F', '\u2197\uFE0F']) {
      claim(EMOJI.test(text), `${text} is emoji`)
    }

    // First person, wherever it falls, and what merely looks like it.
    claim(firstSingular('My parser is fast.'), 'My at a sentence start')
    claim(firstSingular('Mine is faster.'), 'Mine at a sentence start')
    claim(!firstSingular('The disk I/O is buffered.'), 'I/O is not a pronoun')
    claim(!firstSingular('A lexer, i.e. a tokeniser.'), 'the i of i.e.')
    claim(firstSingular('Then I ran it.'), 'a capital I is a pronoun')
    claim(!firstSingular(label('**I** the identifier column')),
      'a bold label is a label')
    claim(firstSingular(label('Then **I** configured it.')),
      'a bold pronoun in prose is a pronoun')

    // Every mark, and the two that are not one.
    const bang = (s) => (s.match(EXCLAMATION) || []).length
    claim(1 === bang('It works **now!** Next'), 'mark before bold close')
    claim(1 === bang('He said "Done!" then'), 'mark before a quote')
    claim(1 === bang('It works! Next'), 'plain mark')
    claim(1 === bang('Really?!'), 'mark after another mark')
    claim(1 === bang('Voil\u00e0!'), 'mark after a non-ASCII letter')
    claim(2 === bang('Great!!'), 'two marks are two marks')
    claim(0 === bang('if (a != b)'), '!= is an operator')
    claim(0 === bang('![alt](src)'), 'an image is not a mark')
    claim(0 === bang(prose('Read <https://host/a!b>.')),
      'an autolink is not prose')
    claim(0 === bang(prose('Read https://host/a!b today.')), 'nor a bare URL')
    claim(1 === bang(prose('Read https://host/a!')),
      'the mark ending the sentence after one still counts')
    claim(0 === bang(prose('Text ``a ` !\nb`` more.')),
      'a wrapped span holding a shorter run is still a span')

    // A typographic apostrophe is what a word processor, a website and
    // most of these pages produce. `let'?s` matched `lets` and `let's`
    // and walked straight past `let\u2019s`.
    const banned = (text) => BANNED.some(([re]) => {
      re.lastIndex = 0
      return re.test(text)
    })
    claim(!/\*\*I\*\*/.test(label('1) **I** the column')),
      'a parenthesised list label is a label too')
    claim(/\*\*I\*\*/.test(label('Then **I** ran it')),
      'a bold pronoun mid-sentence is not a label')
    claim(banned('so let\u2019s break it down'), 'a curly apostrophe')
    claim(banned("so let's break it down"), 'a straight apostrophe')

    // A block opener ends the paragraph above it.
    const joined = logical('| a worth |\n| noting b |').map((p) => p.text)
    claim(!joined.some((t) => /worth noting/.test(t)),
      'a table row is not the row above wrapping')
    claim(logical('worth\nnoting').some((t) => /worth noting/.test(t.text)),
      'a wrapped paragraph still joins')

    // A block opener is not the line above it wrapping, and a heading or
    // a table row is one line whatever follows it.
    const joins = (md, phrase) =>
      logical(md).some((p) => p.text.includes(phrase))
    claim(!joins('## Something worth\nnoting this', 'worth noting'),
      'a heading is not the paragraph under it')
    claim(!joins('| a | worth |\n| noting | b |', 'worth | | noting'),
      'a table row is not the row above it')
    claim(!joins('- one worth\n- noting two', 'worth - noting'),
      'a list item is not the item above it')
    claim(joins('| This explanation is worth\nnoting here', 'worth noting'),
      'one pipe is a sentence, not a table row')
    claim(joins('a sentence worth\nnoting here', 'worth noting'),
      'a wrapped paragraph still joins')
    claim(joins('- an item worth\n  noting here', 'worth noting'),
      'a wrapped list item still joins')

    Assert.deepEqual(faults, [],
      `these rules no longer catch what they claim:\n${faults.join('\n')}`)
  })

  // The guide names the command that runs the Vale half, and the check
  // is that the command EXISTS. `make prose` was in every copy of this
  // list, including the repository that has no Makefile and runs its
  // gate from npm.
  test('the-style-guide-names-both-gates', () => {
    const guide = Fs.readFileSync(GUIDE, 'utf8')
    for (const name of [
      'ts/test/docs.test.js', 'ts/scripts/gated-docs.cjs',
      '.vale.ini', 'reject.txt',
    ]) {
      Assert.ok(guide.includes(name), `the guide names ${name}`)
    }

    // Naming a command that exists is not the claim. The claim is that
    // running it runs Vale, and an emptied or repointed recipe still
    // has the name.
    const make = Path.join(REPO, 'Makefile')
    const pkg = Path.join(REPO, 'ts', 'package.json')
    const target = Fs.existsSync(make)
      ? (/^prose:[^\n]*\n((?:[ \t][^\n]*\n|\n)*)/m
        .exec(Fs.readFileSync(make, 'utf8')) || [])[1]
      : null
    const script = Fs.existsSync(pkg)
      ? (JSON.parse(Fs.readFileSync(pkg, 'utf8')).scripts || {}).prose
      : null
    const hasMake = null != target && /vale/i.test(target)
    const hasNpm = null != script && /vale/i.test(script)
    Assert.ok(hasMake || hasNpm,
      'neither a Makefile `prose` target nor an npm `prose` script runs Vale')
    const command = hasMake ? 'make prose' : 'npm run prose'
    Assert.ok(guide.includes(command),
      `the guide does not name ${command}, which is what runs Vale here`)
  })


  // Every pattern here is summarised in the guide, checked by running
  // the pattern's OWN regex over it.
  //
  // This used to compare a literal stem: the part of the source before
  // its first metacharacter, discarded when shorter than three
  // characters. So every pattern BEGINNING with a group left the check
  // without a word: `(?:hits|lands|strikes) hardest` produced an empty
  // stem and was dropped, along with `(?:hit|struck) a nerve`. Asking
  // the regex is both simpler and exact, since a guide that quotes the
  // phrase is quoting something the gate would catch.
  test('the-guide-covers-every-banned-pattern', () => {
    const guide = Fs.readFileSync(GUIDE, 'utf8')
    const missing = BANNED
      .filter(([re]) => !new RegExp(re.source, 'i').test(guide))
      .map(([, src]) => src)
    Assert.deepEqual([...new Set(missing)], [],
      `banned patterns with no summary in the guide: ${missing.join(', ')}`)
  })

})
