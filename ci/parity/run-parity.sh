#!/usr/bin/env bash
# run-parity.sh — differential token-stream parity: each available runtime's
# lexer must emit byte-identical consumed-token streams for the same input.
#
# Value-level TSV comparison (the existing suites) cannot distinguish
# '123abc' lexed as one #TX from #NR+#TX recombined, or a position drift
# that only surfaces in error messages. This runner feeds every input
# column of every shared TSV fixture through the TS and Go dumpers (plus Rust
# for the function-free strict-JSON grammar) and diffs the streams, localizing
# any parity break to the exact token.
#
# Usage: ci/parity/run-parity.sh [grammar] [spec-dir] [unescape|raw]
#   grammar:  jsonic (default) | json
#   spec-dir: default test/spec in this repo
#   unescape: decode the shared fixture codec (\n, \r, \t, and \\).
#             This is the default for every tabnas TSV corpus because
#             @tabnas/support's SpecRow.unesc is the canonical loader.
#             raw remains available for an explicitly non-standard input.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"
ROOT="${TABNAS_ROOT:-$(cd "$PARSER_ROOT/.." && pwd)}"
GRAMMAR="${1:-jsonic}"
SPEC="${2:-$PARSER_ROOT/test/spec}"
MODE="${3:-unescape}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "=== build gotokdump ==="
GOWORK_DIR="$(mktemp -d)"
( cd "$GOWORK_DIR" && go work init \
    "$DIR/gotokdump" "$PARSER_ROOT/go" "$ROOT/json/go" "$ROOT/jsonic/go" >/dev/null )
( cd "$DIR/gotokdump" && GOWORK="$GOWORK_DIR/go.work" go build -o "$WORK/gotokdump" . )
rm -rf "$GOWORK_DIR"

if [ "$GRAMMAR" = json ]; then
  echo "=== build parity_tokdump ==="
  # Built FROM rs/, not with --manifest-path from wherever this script was
  # invoked. `--manifest-path` does not set the directory cargo resolves
  # from, and a relative `CARGO_TARGET_DIR` is taken relative to that
  # directory: built from the repository root it lands in <root>/<dir>, and
  # from rs/ in rs/<dir>. `target-dir.js` asks from rs/, so the build has to
  # as well or the two name different paths and this gate dies with "No such
  # file or directory" on a build that just succeeded.
  ( cd "$PARSER_ROOT/rs" && cargo build --quiet --bin parity_tokdump )
  # Ask cargo where it wrote it. `CARGO_TARGET_DIR`, or a `[build] target-dir`
  # in an applicable cargo config, moves the directory away from rs/target,
  # and hardcoding that path made this gate die the same way.
  RS_TOKDUMP="$(node "$DIR/../rust/target-dir.js" parity_tokdump)"
fi

# Extract input columns from the TSV fixtures (skip header). Unescape mode
# mirrors @tabnas/support exactly: \n, \r, \t, and \\ decode; unknown escape
# sequences and a trailing backslash remain literal.
node -e '
const fs = require("fs"), path = require("path")
const [specDir, outDir, mode] = process.argv.slice(1)
function unescapeCell(src) {
  let out = ""
  for (let i = 0; i < src.length; i++) {
    const c = src[i]
    if ("\\" === c && i + 1 < src.length) {
      const n = src[i + 1]
      if ("n" === n) { out += "\n"; i++; continue }
      if ("r" === n) { out += "\r"; i++; continue }
      if ("t" === n) { out += "\t"; i++; continue }
      if ("\\" === n) { out += "\\"; i++; continue }
    }
    out += c
  }
  return out
}
let n = 0
for (const f of fs.readdirSync(specDir).filter((f) => f.endsWith(".tsv")).sort()) {
  const lines = fs.readFileSync(path.join(specDir, f), "utf8").split(/\r?\n/)
  lines.slice(1).filter((line) => line && !(line.startsWith("#") && !line.includes("\t"))).forEach((line, i) => {
    let input = line.split("\t")[0]
    if ("unescape" === mode) {
      input = unescapeCell(input)
    }
    fs.writeFileSync(path.join(outDir, `${String(n++).padStart(5, "0")}-${f.replace(/\.tsv$/, "")}-r${i + 1}.in`), input)
  })
}
console.log(`extracted ${n} inputs (${mode})`)
' "$SPEC" "$WORK" "$MODE"

# One process per runtime over the whole input directory (per-file
# sections delimited by "== <name>" lines), then a single diff.
node "$DIR/tokdump.js" "$GRAMMAR" "$WORK" > "$WORK/ts.tok"
"$WORK/gotokdump" "$GRAMMAR" "$WORK" > "$WORK/go.tok"
if [ "$GRAMMAR" = json ]; then
  "$RS_TOKDUMP" "$GRAMMAR" "$WORK" > "$WORK/rs.tok"
fi

total=$(grep -c '^== ' "$WORK/ts.tok")
if cmp -s "$WORK/ts.tok" "$WORK/go.tok" && \
    { [ "$GRAMMAR" != json ] || cmp -s "$WORK/ts.tok" "$WORK/rs.tok"; }; then
  if [ "$GRAMMAR" = json ]; then runtimes="all three runtimes"; else runtimes="both runtimes"; fi
  echo "parity($GRAMMAR): $total inputs, $runtimes identical"
  exit 0
fi

echo "parity($GRAMMAR): DIVERGENT (over $total inputs); first differences:"
if ! cmp -s "$WORK/ts.tok" "$WORK/go.tok"; then
  echo "--- TypeScript vs Go"
  diff "$WORK/ts.tok" "$WORK/go.tok" | head -30
fi
if [ "$GRAMMAR" = json ] && ! cmp -s "$WORK/ts.tok" "$WORK/rs.tok"; then
  echo "--- TypeScript vs Rust"
  diff "$WORK/ts.tok" "$WORK/rs.tok" | head -30
fi
exit 1
