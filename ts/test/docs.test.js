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

  test('the-gated-set-covers-the-readmes-and-both-ports', () => {
    const files = paths().map((p) => p.file)
    Assert.ok(15 < files.length, `gated set is ${files.length} files`)
    for (const r of ['README.md', 'ts/README.md', 'go/README.md']) {
      Assert.ok(files.includes(r), `${r} is gated`)
    }
    Assert.ok(
      files.some((f) => f.startsWith('ts/doc/')), 'the TS docs are gated')
    Assert.ok(
      files.some((f) => f.startsWith('go/doc/')), 'the Go docs are gated')
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


  test('first-person-singular-appears-nowhere', () => {
    const hits = []
    for (const { file, abs } of paths()) {
      prose(Fs.readFileSync(abs, 'utf8'))
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


  // A whole category added to reject.txt and missed by the guide leaves
  // the reader's summary silently incomplete.
  test('the-guide-summarises-every-banned-category', () => {
    const guide = Fs.readFileSync(GUIDE, 'utf8').toLowerCase()
    const missing = lf(Fs.readFileSync(REJECT, 'utf8'))
      .split('\n')
      .map((l) => l.match(/^#\s*-{2,}\s*(.+?)\s*-{2,}\s*$/))
      .filter(Boolean)
      .map((m) => m[1].trim())
      .filter((cat) => !guide.includes(cat.toLowerCase()))
    Assert.deepEqual(missing, [],
      `reject.txt categories with no summary in the guide: ${
        missing.join(', ')}`)
  })

})
