# tsdeps: the TypeScript peer-dependency page

Regenerates the page of every tabnas TypeScript package's `@tabnas` peers,
read from each repository's `main`: the layered graph, the deepest chains,
the packages that need the engine, what changed since the previous read,
and npm's own manifests for the same versions. The published copy is the
artifact <https://claude.ai/artifact/R5c6Hag7eawFfUZrJitjaU>, private to
the maintainer's account, updated in place on each redraw. (The earlier
copy, `EbqCuLdZ4mh1wSGQdTfjKZ`, could not be reached from the maintainer's
machine on 2026-10-05, so the redraw was published afresh.) It was last
read at 2026-10-06 07:05 UTC (`last-read/`), after the shared-types pull
requests, the 0.2.0 releases, and multisource 0.6.0, which dropped jsonic
(tabnas/multisource#73, #74).

| File | What it does |
|---|---|
| `parse.js` | reads every `<repo>@<path>_package.json` copy in the working directory into `graph.json` (packages, and `@tabnas` edges as dependency, peer or optional) |
| `gen.js` | `graph.json`, `shas.txt`, `read-at.txt`, `npm-manifests.json`, `changes.json` and `prev/` into `site/tabnas-peers.html`, through `template.html`, and prints the layers and findings |
| `template.html` | the page, with `{{...}}` placeholders `gen.js` fills |
| `last-read/` | `graph.json`, `shas.txt` and `read-at.txt` of the last read: copy them into `prev/` so the next page can say what changed |

## Steps

Run from a working directory holding `parse.js`, `gen.js` and
`template.html`, with `prev/` filled from `last-read/`. These ran as
separate commands in the session, each printing its progress, not as
one tested script.

1. Read every `package.json` on each repository's `main`, and record the
   commit and the time:

   ```bash
   date -u +%Y-%m-%dT%H:%M:%SZ > read-at.txt
   repos="parser support json debug path hoover directive railroad jsonic bnf abnf ebnf gbnf css csv expr json5 jsonc jsonl markdown toml xml yaml zon proto semver c ini feed alchemy transduce render alchemy-cli multisource jsonic-cli mcp lsp chess web skills status"
   total=$(echo $repos | wc -w); n=0; : > shas.txt
   for r in $repos; do n=$((n+1))
     [ -d $r ] || timeout 120 git clone -q --depth 1 --filter=blob:none --no-checkout https://github.com/tabnas/$r $r 2>/dev/null
     timeout 60 git -C $r fetch -q --depth 1 origin main 2>/dev/null
     sha=$(git -C $r rev-parse --short origin/main 2>/dev/null)
     files=$(git -C $r ls-tree -r --name-only origin/main | grep -E '(^|/)package\.json$' | grep -v node_modules | tr '\n' ' ')
     for f in $files; do git -C $r show origin/main:$f > "$r@$(echo $f | tr / _)"; done
     echo "$r $sha" >> shas.txt
     echo "progress: $n/$total ($((n*100/total))%) $r @ $sha: ${files:-no package.json}"
   done
   ```

   Add a repository to `repos` when the fleet gains one.

2. `node parse.js > parse.out`, which writes `graph.json`.

3. npm's manifests for the same versions, so the page can show where
   `main` is ahead of the registry:

   ```bash
   node -e '
   const g = require("./graph.json")
   for (const p of g.packages) if (p.name && p.name.startsWith("@tabnas/") && !p.private) console.log(p.name + "@" + p.version)
   ' > npm-query.txt; total=$(wc -l < npm-query.txt); n=0; echo "{" > npm-manifests.json.tmp; first=1
   while read spec; do n=$((n+1))
     out=$(timeout 60 npm view "$spec" peerDependencies peerDependenciesMeta dependencies optionalDependencies version --json 2>/dev/null)
     [ -z "$out" ] && out='null'
     [ $first = 1 ] || echo "," >> npm-manifests.json.tmp; first=0
     printf '%s: %s\n' "\"$spec\"" "$out" >> npm-manifests.json.tmp
     echo "npm: $n/$total ($((n*100/total))%) $spec"
   done < npm-query.txt; echo "}" >> npm-manifests.json.tmp
   node -e 'JSON.parse(require("fs").readFileSync("npm-manifests.json.tmp","utf8"))' && mv npm-manifests.json.tmp npm-manifests.json
   ```

4. The commits on each `main` since the previous read, into
   `changes.json`. The clones are shallow, so deepen one first when its
   previous commit is out of reach (`git -C <repo> fetch --deepen=20
   origin main`):

   ```bash
   node -e '
   const fs = require("fs"), cp = require("child_process")
   const parse = (f) => Object.fromEntries(fs.readFileSync(f, "utf8").trim().split("\n").map((l) => l.split(" ")))
   const prev = parse("prev/shas.txt"), now = parse("shas.txt")
   const out = {}
   for (const r of Object.keys(now)) {
     if (prev[r] === now[r]) continue
     const log = cp.execSync(`git -C ${r} log --reverse --format=%h%x09%s ${prev[r]}..origin/main`, { encoding: "utf8" }).trim().split("\n")
     out[r] = { from: prev[r], to: now[r], commits: log.map((l) => { const [sha, subject] = l.split("\t"); const m = subject.match(/\(#(\d+)\)$/); return { sha, subject, pr: m ? Number(m[1]) : null } }) }
   }
   fs.writeFileSync("changes.json", JSON.stringify(out, null, 1))
   '
   ```

5. `mkdir -p site && node gen.js`, then publish `site/tabnas-peers.html`
   to the same artifact URL, so the link stays the same.

Before the next read, move this read into `prev/`
(`cp graph.json shas.txt read-at.txt prev/`).
