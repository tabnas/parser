
const Fs = require('node:fs')
const Path = require('node:path')

const REPO = Path.join(__dirname, '..', '..')

// The language-neutral pages under doc/. The Rust-port series and the
// feasibility reports are working documents for contributors, and are
// deliberately out: see "The published set" in doc/STYLE-GUIDE.md.
const NEUTRAL = [
  'syntax.md',
  'architecture.md',
  'value-builtins.md',
]

// The four Diataxis kinds, per runtime. Both ports carry the same set
// so that a page missing from one is visible as a missing gate.
const PER_RUNTIME = [
  'tutorial.md',
  'guide.md',
  'api.md',
  'options.md',
  'plugins.md',
  'concepts.md',
]

// Pages only one runtime has.
const EXTRA = ['go/doc/differences.md', 'go/doc/syntax.md']

// npm and pkg.go.dev render these to somebody who has the package and
// not the repository, so they are held to the same bar as the docs.
const READMES = ['README.md', 'ts/README.md', 'go/README.md', 'rs/README.md']


function exists(rel) {
  return Fs.existsSync(Path.join(REPO, rel))
}


// Repo-relative, sorted within each group. A declared page that is not
// on disk THROWS.
//
// This filtered instead, and the comment here claimed the filter made a
// renamed page "fail as a missing gate". It did the opposite: the page
// left the list, both halves of the gate carried on over what remained,
// and the coverage test passed because it only counts what the list
// returned. Deleting a page was the one way to stop it being checked.
function gatedDocs() {
  const neutral = NEUTRAL.map((f) => `doc/${f}`)
  const ts = PER_RUNTIME.map((f) => `ts/doc/${f}`)
  const go = PER_RUNTIME.map((f) => `go/doc/${f}`)

  const declared = [...neutral, ...ts, ...go, ...EXTRA, ...READMES]
  const gone = declared.filter((f) => !exists(f))
  if (0 < gone.length) {
    throw new Error(
      'gated-docs.cjs: declared but not on disk: ' + gone.join(', ') +
      '. Rename it here, or delete the entry deliberately.')
  }
  return declared
}


// Tutorials are the only pages where "we" is allowed, so the style
// gate has to know which page is which.
function tutorials() {
  return ['ts/doc/tutorial.md', 'go/doc/tutorial.md'].filter(exists)
}


module.exports = { gatedDocs, tutorials, NEUTRAL, PER_RUNTIME, READMES }

if (require.main === module) {
  process.stdout.write(gatedDocs().join('\n') + '\n')
}
