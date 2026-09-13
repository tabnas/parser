
const Fs = require('node:fs')
const Path = require('node:path')

const REPO = Path.join(__dirname, '..', '..')

// The language-neutral pages under doc/. The Rust-port series and the
// feasibility reports are working documents for contributors, and are
// deliberately out: see "The published set" in docs/STYLE-GUIDE.md.
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
const READMES = ['README.md', 'ts/README.md', 'go/README.md']


function exists(rel) {
  return Fs.existsSync(Path.join(REPO, rel))
}


// Repo-relative, sorted within each group, and filtered to what is
// actually on disk so a renamed page fails as a missing gate rather
// than as a crash.
function gatedDocs() {
  const neutral = NEUTRAL.map((f) => `doc/${f}`)
  const ts = PER_RUNTIME.map((f) => `ts/doc/${f}`)
  const go = PER_RUNTIME.map((f) => `go/doc/${f}`)

  return [...neutral, ...ts, ...go, ...EXTRA, ...READMES].filter(exists)
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
