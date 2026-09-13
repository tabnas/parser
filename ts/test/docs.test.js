/* Copyright (c) 2026 Richard Rodger and other contributors, MIT License */

// The fast half of the prose gate (docs/STYLE-GUIDE.md).
//
// Vale is the other half and runs in .github/workflows/docs.yml. The two
// read ONE file list (ts/scripts/gated-docs.cjs) and ONE banned list
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
const GUIDE = Path.join(REPO, 'docs', 'STYLE-GUIDE.md')


function lf(s) {
  return s.replace(/\r\n/g, '\n')
}


// The banned list, read from the file Vale reads. Comments and blank
// lines out; every other line is a regex, matched case-insensitively on
// word boundaries, exactly as Vale.Avoid matches it.
function loadBanned() {
  return lf(Fs.readFileSync(REJECT, 'utf8'))
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => '' !== l && !l.startsWith('#'))
    .map((src) => [new RegExp(`\\b(?:${src})\\b`, 'gi'), src])
}


const BANNED = loadBanned()

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


function prose(md) {
  return fenceless(md)
    .replace(/^---\n[\s\S]*?\n---\n/, '')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/`[^`\n]*`/g, '')
}


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
    const piece = line.trim().replace(/\s+/g, ' ')
    starts.push(at)
    lines.push(i + 1)
    pieces.push(piece)
    at += piece.length + 1
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
      `banned phrases (docs/STYLE-GUIDE.md):\n${hits.join('\n')}`)
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
      `em dashes in prose (docs/STYLE-GUIDE.md):\n${hits.join('\n')}`)
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
  test('first-person-singular-appears-nowhere', () => {
    const hits = []
    for (const { file, abs } of paths()) {
      prose(Fs.readFileSync(abs, 'utf8'))
        .replace(/\*\*[A-Z]{1,2}\*\*/g, '')
        .split('\n')
        .forEach((line, i) => {
          if (/\b(I|I'\w+|me|my|mine)\b/.test(line)) {
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
      // A sentence-ending mark, not every `!` byte: `!=` is an
      // operator and `![alt](src)` is an image.
      const n = (prose(Fs.readFileSync(abs, 'utf8'))
        .match(/\w!(?=\s|$)/g) || []).length
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
      `exclamation marks (docs/STYLE-GUIDE.md):\n${hits.join('\n')}`)
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
          if (/[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]/u.test(line)) {
            hits.push(`${file}:${i + 1}: ${line.trim()}`)
          }
        })
    }
    Assert.deepEqual(hits, [],
      `emoji in documentation (docs/STYLE-GUIDE.md):\n${hits.join('\n')}`)
  })


  // The guide claims two gates. If either name stops appearing the
  // claim has gone stale, and a reader following it lands nowhere.
  test('the-style-guide-names-both-gates', () => {
    const guide = Fs.readFileSync(GUIDE, 'utf8')
    for (const name of [
      'make prose', 'ts/test/docs.test.js', 'ts/scripts/gated-docs.cjs',
      '.vale.ini', 'reject.txt',
    ]) {
      Assert.ok(guide.includes(name), `the guide names ${name}`)
    }
  })


  // Every pattern here is summarised in the guide. Checked by its
  // literal prefix, the part before the first regex metacharacter, so
  // `leverag(?:e|es|ed|ing)` is satisfied by "leverage" in the prose.
  test('the-guide-covers-every-banned-pattern', () => {
    const guide = Fs.readFileSync(GUIDE, 'utf8').toLowerCase()
    const missing = BANNED
      .map(([, src]) => src.split(/[([\\.?*+|]/)[0].trim())
      .filter((stem) => 2 < stem.length && !guide.includes(stem.toLowerCase()))
    Assert.deepEqual([...new Set(missing)], [],
      `banned patterns with no summary in the guide: ${missing.join(', ')}`)
  })

})
