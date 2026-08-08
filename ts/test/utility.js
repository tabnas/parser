const { readFileSync, existsSync } = require('fs')
const { join } = require('path')

// Fixture fields escape \n, \r and \t (a raw tab would be a column
// separator). Must stay in step with the Go loader's preprocessEscapes
// (go/spec_test.go) so a shared fixture means the same thing in both
// runtimes.
function unescape(str) {
  return str.replace(/\\r\\n|\\n|\\r|\\t/g, (m) => {
    if (m === '\\r\\n') return '\r\n'
    if (m === '\\n') return '\n'
    if (m === '\\r') return '\r'
    if (m === '\\t') return '\t'
    return m
  })
}

// Spec TSVs live at the repo root (../../test/spec/) so the Go and TS
// implementations share them. Tests under ts/test/ read them via this
// helper.
function loadTSV(name) {
  const specPath = join(__dirname, '..', '..', 'test', 'spec', name + '.tsv')

  if (!existsSync(specPath)) {
    throw new Error('spec file not found: ' + specPath)
  }

  const lines = readFileSync(specPath, 'utf8').split(/\r?\n/).filter(Boolean)
  return lines.slice(1).map((line, i) => {
    const cols = line.split('\t').map(unescape)
    return { cols, row: i + 1 }
  })
}

module.exports = { loadTSV }
