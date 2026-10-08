# Handover: engine 0.12.11 and the fleet released on it, 2026-10-08

This session ran in a cloud container on 2026-10-08, from the
maintainer's instructions "Review the current state of the tabnas parser
system, and the aless utility", then "Implement fixes; ask me as needed;
add tabnas repos pushable as needed. Aim for full clean uptodate
publishing once work done." It began from the previous page, now
[`2026-10-07.md`](2026-10-07.md). What is still open from that page is
under "Carried over".

## The maintainer's rulings (2026-10-08)

1. **parser #287 lands**: jsonic's ledger row first and a jsonic patch
   release, then the Go reorder in the engine, then engine 0.12.11.
2. **A full fleet cascade**: every Go module that required the engine
   at 0.12.10 moves to 0.12.11 in its own PR, and every changed package
   is released, in dependency order.
3. **aless stays on git pins**, moved to the latest releases; #28 gets a
   comment on what still blocks crates.io and stays open.
4. **aless prints a path the way jq takes it**: `.[0].name`.

## What landed

**The engine, 0.12.11** (parser#303, released `d6ae424`, with the C
library Release). It carries:

- #300: Rust starts the matchers after a custom one where that one left
  the cursor, and a lone U+FEFF no longer panics. The previous page's
  "found, not fixed" panic is fixed by this.
- #302 (closes #287): Go reads `string.replace` before the escape and
  control checks, as TypeScript and Rust do. jsonic#111 deleted its
  ledger row ahead of it, and jsonic 0.7.7 (jsonic#112) shipped that;
  jsonic 0.7.8 (jsonic#113) asserts the repaired answer.
- #301: documentation and register structure only.

**aless#46**: the agent interface's errors, paths and docs agree with the
code; paths print as jq takes them (ruling 4).

**The cascade.** Each repository moved to engine 0.12.11 in its own PR,
made by admin `publish.sh`'s step-2 block and verified against the
published dependencies (clean TS install and `npm test`, `GOWORK=off` Go
tests, `ci/rust/run.sh` on the MSRV toolchain). Each was released by a
`release.yml` dispatch on `main` after `main` CI went green, and checked
the way parser's AGENTS.md says: npm's `gitHead` against every tag and the
recorded commit, the Go proxy, crates.io and, where `clib-release.yml`
exists, the published C library Release.

In release order. Each row's commit is what npm's `gitHead`, every tag and
the Go proxy name. Where a row lists a C library, its Release is published
with `manifest.json`.

| Repo | Version | PR | Release commit | Published |
|---|---|---|---|---|
| parser | 0.12.11 | parser#303 | `d6ae424` | npm, Go, crate, C library |
| chess | 0.1.13 | chess#58 | `8d9c454` | npm, Go, crate, C library |
| support | 0.3.9 | support#52 | `9bf14f4` | npm, Go, crate |
| lsp | 0.1.7 | lsp#42 | `eae04e6` | npm, Go, crate |
| bnf | 0.1.28 | bnf#107 | `3927b59` | npm, Go, crate, C library |
| directive | 0.5.13 | directive#74 | `dfe3824` | npm, Go, crate, C library |
| debug | 0.3.13 | debug#80 | `860b549` | npm, Go, crate, C library |
| hoover | 0.3.14 | hoover#70 | `4aa6210` | npm, Go, crate, C library |
| json | 0.5.16 | json#106 | `cdc663e` | npm, Go, crate, C library |
| markdown | 0.7.11 | markdown#93 | `c439ae0` | npm, Go, crate, C library |
| mcp | 0.1.20 | mcp#35 | `292320c` | npm |
| semver | 0.0.9 | semver#40 | `380e6e9` | npm, Go, crate, C library |
| jsonic | 0.7.8 | jsonic#113 | `62bb63a` | npm, Go, crate, C library |
| railroad | 0.3.12 | railroad#67 | `be5934a` | npm, Go, crate |
| path | 0.3.13 | path#72 | `f4d2ec6` | npm, Go, crate, C library |
| jsonl | 0.1.15 | jsonl#48 | `f1ac542` | npm, Go, crate, C library |
| ebnf | 0.1.14 | ebnf#61 | `68f0fac` | npm, Go, crate, C library |
| expr | 0.5.15 | expr#91 | `cc94602` | npm, Go, crate, C library |
| xml | 0.7.15 | xml#93 | `533569b` | npm, Go, crate, C library |
| abnf | 0.4.20 | abnf#116 | `7352bc7` | npm, Go, crate, C library |
| csv | 0.6.5 | csv#101 | `e68f319` | npm, Go, crate, C library |
| yaml | 0.5.22 | yaml#125 | `8fa1c29` | npm, Go, crate, C library |
| toml | 0.5.15 | toml#107 | `a8814e6` | npm, Go, crate, C library |
| ini | 0.5.17 | ini#103 | `5a37b7b` | npm, Go, crate, C library |
| json5 | 0.5.14 | json5#97 | `96b4838` | npm, Go, crate, C library |
| jsonc | 0.5.13 | jsonc#88 | `c637340` | npm, Go, crate, C library |
| zon | 0.5.15 | zon#98 | `3bf99a7` | npm, Go, crate, C library |
| css | 0.5.13 | css#66 | `81ccc3f` | npm, Go, crate, C library |
| jsonic-cli | 0.5.14 | jsonic-cli#54 | `993a4a5` | npm, Go, crate |
| multisource | 0.6.3 | multisource#84 | `024d5cf` | npm, Go, crate, C library |
| feed | 0.6.15 | feed#83 | `d6783e3` | npm, Go, crate, C library |
| alchemy | 0.2.3 | alchemy#54 | `bdf73a7` | npm, Go, crate, C library |
| gbnf | 0.1.17 | gbnf#64 | `5e67674` | npm, Go, crate, C library |
| proto | 0.6.6 | proto#66 | `e227197` | npm, Go, crate, C library |
| c | 0.5.13 | c#67 | `b91cb5b` | npm, Go, crate, C library |
| transduce | 0.2.4 | transduce#42 | `c14f813` | npm, Go, crate |
| render | 0.2.3 | render#21 | `4a4cc86` | npm, Go, crate |
| alchemy-cli | 0.1.5 | alchemy-cli#14 | `6e96ef1` | npm, Go, crate |

**Beyond the version sites**, what the repositories' own rules asked
for, most of it caught in review:

- **Peer floors** follow `go/go.mod` in abnf, ebnf, gbnf, proto and
  semver: `@tabnas/parser` `>=0.12.11`, and `@tabnas/bnf` `>=0.1.28`
  where it is a peer. `publish.sh` leaves peer ranges alone, so this is
  a hand step for those five on every release.
- **gbnf**: the llama.cpp corpora moved to the latest release tag,
  b11497 (`ff30363a0e3e`), from b11200, as its release step 1 asks.
  Upstream changed neither corpus in between (same blob ids), so only
  the pin moved and the Rust oracle regenerated byte for byte. The demo
  bundle `docs/gbnf-demo.js` was rebuilt on engine 0.12.11 (it carried
  0.12.8), and the dev floors moved to abnf `^0.4.20`, bnf `^0.1.28`.
- **feed**: xml 0.7.15 is the first release whose Go port gives
  attributes as a `*tabnas.OrderedMap` (xml#91), so the `atom.tsv` row
  that pins XHTML attribute order in all three ports landed with feed
  0.6.15, as feed's AGENTS.md said it should.
- **jsonic**: the #287 follow-up described above.

**Also landed**:

- skills#16 and admin#135: the plugin pins `@tabnas/mcp@0.1.20` and is
  released as **0.3.3** (the pin syncs since 0.3.2 had skipped the bump).
  `CLAUDE_CODE_VERSION` is 2.1.285, the current `stable` dist-tag, with
  admin's rollout template mirrored.
- lsp#43: a test line clippy on current stable rejected
  (`cloned_ref_to_slice_refs`); the MSRV gate never saw it.
- json#107: the `ts/package-lock.json` pins of the dev-only debug and
  railroad at their latest, the only stale tracked `@tabnas` lockfile
  pins in the fleet outside web.

**After the cascade**:

- aless#47: `Cargo.lock`'s 21 tabnas git pins are at the commits of
  those releases (`cargo update -p … --precise`, not branch heads), with
  every aless gate green (ruling 3). The #28 comment records a crates.io
  trial on the same releases. All 19 crates resolve from the registry
  (175 packages, none from git), `cargo check` is clean, and every test
  passes except `tests/yaml_render.rs`, which reads tabnas-yaml's own
  fixtures. The published crate does not ship them. The comment lists
  three ways out; nothing is decided.
- web#58 moves the site's eight exact pins (the engine 0.12.11 among
  them), the fallback versions of 38 packages in `src/consts.ts`, and the
  generated `error-codes.json` and `skills.json`. Every check in
  `npm run check` passes, and so does the Vale gate with its recorded
  counts. Since the merge, tabnas.dev's `versions.json` names exactly
  the pinned versions, and every check on the merge commit is green.
- admin#136: `scripts/verify.sh`'s workflow-template check compared
  nothing in a container whose `gh` is installed but not logged in.
  Every template's repository read as nonexistent, and the check printed
  "all 0 deployed copies current". It now falls back to the local
  checkouts, and compares all 224.
- admin: `docs/deps` is fresh (`node tasks/port-deps.js --fresh`), since
  the cascade moved versions and no dependency edges.

## No red checks

With web#58 and admin#136 merged, `scripts/verify.sh` reports no problem
in any of the 41 tabnas repositories cloned here. 38 are ok and 3 are
pending, as expected:

- alchemy, whose test-only requires stay one release behind (see
  "Carried over");
- json and lsp, each one commit past its release (json#107 and lsp#43),
  neither of which changes a published artifact.

parser, ok at 0.12.11, becomes the fourth pending when this page merges,
one documentation commit past its release.

Every workflow run on the head of `main` is green in all 41 of them and
in aless. One had been red: parser's scheduled `fleet` run at 10:32 UTC
failed in feed's Go suite alone. It cloned feed 0.6.14, the latest feed
then, with xml 0.7.15, released about twenty minutes before. xml 0.7.15
gives attributes to Go as a `*tabnas.OrderedMap`, and feed reads that
form only from feed#81, first released in 0.6.15. So every attribute
read as absent: an Atom link's `href` came out empty, and
`<rss version="0.91">` was detected as RSS 2.0. Re-run once feed 0.6.15
was out, the same job ended `FLEET PASS`. A Go program that requires xml
0.7.15 or later and feed 0.6.14 or earlier gets the same wrong answer, and
feed 0.6.15 is its fix.

## Pushed and open

Nothing. Every pull request this session opened is merged, and this
page goes up in its own.

## Decisions waiting on the maintainer

Unchanged from [`2026-10-07.md`](2026-10-07.md): the engine's loose
TypeScript types, xml's loose embed check, alchemy's wording
inconsistencies, and the census's `ERROR:<code>@row:col` cells.

## Carried over

- From [`2026-10-04.md`](2026-10-04.md), "Carried over", unchanged:
  Engine 3, the jsonic/go alignment, the Engine 1 and 2 follow-ups, the
  css citations in this repository, and css's linear selector-group scan.
- **Open issues**: parser #258. aless #28 has a comment on what blocks
  the move to crates.io (see aless above). aless #29 waits on its owner:
  this session's credentials cannot delete branches.
- **Found, not fixed** on the previous page and still open: two engines
  from `tabnas_json::make()` cannot be merged in Rust. (The U+FEFF panic,
  feed's attribute order, and ini's and toml's stale AGENTS.md lines are
  fixed.)
- **chess-view**: the chess repository's `web/` package is released by a
  `web/v*` tag, which only the maintainer can push.
- **alchemy's test-only requires** on transduce and render, and their
  `rs/Cargo.lock` entries, stay one release behind until alchemy's next
  release (the cycle admin#114 recorded).

## Working method that held up

- **Read each repository's release steps before bumping it.** Several
  ask for more than `publish.sh` does: the peer floors (abnf, ebnf, gbnf,
  proto, semver), gbnf's corpus refresh and demo bundle, and feed's
  fixture once xml moved. Codex caught the ones this session missed;
  reading AGENTS.md first is cheaper than a review round.
- **A dependency map from `go.mod` alone misses Rust path dependencies.**
  lsp takes 15 grammars by path and was released in the first wave. Its
  gate runs without `--locked` for exactly this reason, and its crate
  release rewrites each path into a caret requirement on the newest
  version, so nothing broke; `publish.sh`'s ORDER still puts lsp last,
  and that is the order to keep.
- **alchemy's `ci / ts (windows-latest)` takes about 25 minutes**, nearly
  all of it building its 17 siblings. It is slow, not stuck. This session
  cancelled and re-ran it once believing otherwise; the re-run took 25
  minutes and passed.
- **Never ask the Go proxy for a version before its tag exists.**
  proxy.golang.org caches the not-found answer: this session's waiter
  polled `alchemy/go/@v/v0.2.3.info` while the release was still
  publishing to npm, and that path kept answering "unknown revision
  go/v0.2.3" for about half an hour after `go/v0.2.3` was pushed, while `@v/list`,
  `@latest`, `.mod`, `.zip` and a real `go get` already served v0.2.3.
  Wait on npm and `git ls-remote` for the tag; ask the proxy last, as
  `relcheck`-style verification does.
- **A fleet run during a cascade can pair releases that never ship
  together.** It clones every package at its latest release, so between
  two releases of a chain it tests the new one against the old. Read the
  versions it took (the `--update-lock` artifact records them) before
  calling a red run a regression.
- **The disk allowance runs out across a fleet of Rust builds.** One
  shared `CARGO_TARGET_DIR` for every prep reached 23G and left 1.1G
  free; it is rebuildable, so clear it between levels.
- **Read-only subagents watched CI**, one line per PR or commit, which
  kept the fleet's check-run JSON out of the main context. A watcher that
  waits for Codex must not wait for a re-review after a push: Codex
  reviews when a PR opens, not on every push.

## Session notes

- This session's merged head branches are still on GitHub, since its
  credentials cannot delete branches: `claude/release-<repo>-<version>`
  in each repository of the table above, and
  - parser: `claude/doc-drift-2026-10-08`, `claude/fix-287-replace-order`
    and this page's `claude/handover-2026-10-08`;
  - jsonic: `claude/ledger-ahead-of-parser-287` and
    `claude/release-jsonic-patch` (0.7.7);
  - json: `claude/json-devdeps-debug-railroad`;
  - lsp: `claude/lsp-clippy-from-ref`;
  - skills: `claude/skills-0.3.3`;
  - web: `claude/web-cascade-0.12.11`;
  - admin: `claude/skills-claude-code-2.1.285` and
    `claude/verify-org-list-fallback`;
  - aless: `claude/agent-contract-fixes` and `claude/aless-pins-0.12.11`,
    which add to #29's list.
- `scripts/verify.sh` takes every git checkout beside admin for a tabnas
  repository. In a container whose `/home/user` holds aless too, it
  reports aless as a PROBLEM for having no `.tabnas-kind`; name the
  tabnas repositories on its command line there.
- No subscriptions or check-ins carry over.
