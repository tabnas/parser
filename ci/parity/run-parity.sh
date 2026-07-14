#!/usr/bin/env bash
# run-parity.sh — differential token-stream parity: both runtimes' lexers
# must emit byte-identical consumed-token streams for the same input.
#
# Value-level TSV comparison (the existing suites) cannot distinguish
# '123abc' lexed as one #TX from #NR+#TX recombined, or a position drift
# that only surfaces in error messages. This runner feeds every input
# column of every shared TSV fixture through the TS and Go dumpers and
# diffs the streams, localizing any parity break to the exact token.
#
# Usage: ci/parity/run-parity.sh [grammar] [spec-dir] [unescape|raw]
#   grammar:  jsonic (default) | json
#   spec-dir: default test/spec in this repo
#   unescape: whether input cells carry \n \r escapes. The parser and
#             jsonic corpora do (their loadTSV unescapes); the json
#             corpus does not (its loader is raw). Default follows the
#             grammar: jsonic->unescape, json->raw.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
PARSER_ROOT="$(cd "$DIR/../.." && pwd)"
ROOT="${TABNAS_ROOT:-$(cd "$PARSER_ROOT/.." && pwd)}"
GRAMMAR="${1:-jsonic}"
SPEC="${2:-$PARSER_ROOT/test/spec}"
if [ "$GRAMMAR" = json ]; then DEFMODE=raw; else DEFMODE=unescape; fi
MODE="${3:-$DEFMODE}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "=== build gotokdump ==="
GOWORK_DIR="$(mktemp -d)"
( cd "$GOWORK_DIR" && go work init \
    "$DIR/gotokdump" "$PARSER_ROOT/go" "$ROOT/json/go" "$ROOT/jsonic/go" >/dev/null )
( cd "$DIR/gotokdump" && GOWORK="$GOWORK_DIR/go.work" go build -o "$WORK/gotokdump" . )
rm -rf "$GOWORK_DIR"

# Extract input columns from the TSV fixtures (skip header). In
# unescape mode, \n \r \r\n become real chars, exactly like the parser
# and jsonic repos' loadTSV helpers; raw mode matches json's loader.
node -e '
const fs = require("fs"), path = require("path")
const [specDir, outDir, mode] = process.argv.slice(1)
let n = 0
for (const f of fs.readdirSync(specDir).filter((f) => f.endsWith(".tsv")).sort()) {
  const lines = fs.readFileSync(path.join(specDir, f), "utf8").split(/\r?\n/).filter(Boolean)
  lines.slice(1).forEach((line, i) => {
    let input = line.split("\t")[0]
    if ("unescape" === mode) {
      input = input.replace(/\\r\\n|\\n|\\r/g,
        (m) => ("\\r\\n" === m ? "\r\n" : "\\n" === m ? "\n" : "\r"))
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

total=$(grep -c '^== ' "$WORK/ts.tok")
if cmp -s "$WORK/ts.tok" "$WORK/go.tok"; then
  echo "parity($GRAMMAR): $total inputs, all token streams identical"
  exit 0
fi

fail=$(diff "$WORK/ts.tok" "$WORK/go.tok" | grep -c '^[<>] == ' || true)
echo "parity($GRAMMAR): DIVERGENT (over $total inputs); first differences:"
diff "$WORK/ts.tok" "$WORK/go.tok" | head -30
exit 1
