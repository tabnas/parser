# Handover: engine 0.12.11, the fleet released on it, alchemy's cycle broken, and aless ready to release, 2026-10-08 and 2026-10-09

This session ran in a cloud container on 2026-10-08, from the
maintainer's instructions "Review the current state of the tabnas parser
system, and the aless utility", then "Implement fixes; ask me as needed;
add tabnas repos pushable as needed. Aim for full clean uptodate
publishing once work done." It began from the previous page, now
[`2026-10-07.md`](2026-10-07.md). What is still open from that page is
under "Carried over".

The first version of this page went up in #304. The maintainer then
answered the open questions, and a second round of work followed. Its
rulings are 5 to 10 below, and its work is under "The second round". A
third round, on 2026-10-09, made aless ready to release as a tool of its
own. Its work is under "The third round", and the steps that only the
maintainer can take are under "Decisions waiting".

## The maintainer's rulings (2026-10-08)

1. **parser #287 lands**: jsonic's ledger row first and a jsonic patch
   release, then the Go reorder in the engine, then engine 0.12.11.
2. **A full fleet cascade**: every Go module that required the engine
   at 0.12.10 moves to 0.12.11 in its own PR, and every changed package
   is released, in dependency order.
3. **aless stays on git pins**, moved to the latest releases; #28 gets a
   comment on what still blocks crates.io and stays open. Ruling 5
   replaced this one.
4. **aless prints a path the way jq takes it**: `.[0].name`.

From the open questions, in the second round:

5. **aless moves to crates.io** (#28). CI clones tabnas/yaml at the tag of
   the tabnas-yaml that `Cargo.lock` pins. `tests/yaml_render.rs` reads
   that checkout through `TABNAS_YAML_DIR`, and fails rather than skips
   without it. The git pins and the `[patch]` tables go.
6. **No release for a commit that ships nothing.** parser's handover page,
   json's lockfile pins and lsp's test line stay unreleased.
7. **chess-view 0.1.6**: the maintainer pushes the `web/v0.1.6` tag.
8. **Fix now**: xml's embed check, the census's located cells, and
   alchemy's cycle, the last "as designed". alchemy's tests that run
   programs move to alchemy-cli, and alchemy stops depending on transduce
   and render.
9. **alchemy-cli's test-only dependencies are approved.**
   - Rust: path dev-dependencies on support, the engine, ini, json5,
     jsonc, jsonl, markdown, toml, xml, yaml and zon.
   - Go: the same modules at their newest releases, with hoover indirect.
   - TypeScript: `"*"` devDependencies on csv, ini, json5, jsonc, jsonic,
     jsonl, markdown, support, toml, xml, yaml and zon.
10. **Windows CI**: the shared polyglot workflow makes real links, and
    takes released siblings from npm.

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

### The second round

- **chess-view 0.1.6** (ruling 7). chess#59 moved `web/` to 0.1.6, which
  bundles `@tabnas/chess` 0.1.13 and engine 0.12.11. The maintainer then
  pushed `web/v0.1.6`, and npm serves 0.1.6 with `gitHead` `714db96`, the
  merge commit. chess#60 moved the CDN examples in `README.md`,
  `web/README.md` and `web/demo.html` to 0.1.6.
- **aless#48** (closes #28, ruling 5). Every tabnas crate comes from
  crates.io, and the `[patch]` tables are gone. `scripts/yaml-fixtures.sh`
  clones tabnas/yaml at `go/v<version>` (else `ts/v<version>`) of the
  tabnas-yaml that `Cargo.lock` pins, into
  `target/yaml-fixtures/<version>`, and prints the export line. CI runs it
  before the tests. `tests/yaml_render.rs` fails when `TABNAS_YAML_DIR` is
  unset, or names a checkout of another version.
- **xml's embed check** needed nothing more. xml#89 had already made embed
  mode identify a jsonic host by jsonic's own alternates, in every port,
  and xml 0.7.15 shipped it.
- **The census.** admin#131 already read the code in an
  `ERROR:<code>@row:col` cell. alchemy#55 dropped the eight bare rows that
  alchemy's `check.tsv` carried only for the census.
- **alchemy's cycle** (ruling 8). admin#137 records it as ADR-26.
  - alchemy-cli#15 took over alchemy's tests that run programs, in all
    three runtimes: `run.tsv` (natively and interpreted), the catalogue
    test over all four fixture files, the lowering, events, linked
    sources, the translation parts, and the standard library's
    differential test.
  - alchemy#56 then dropped transduce and render from alchemy's
    manifests, in every runtime. Each port compiles a program without the
    stages, on its own copy of render's number formatter. The TypeScript
    API keeps `shortestNumber(renderers, value)` and
    `numberText(renderers, value, lexeme)` as deprecated overloads.
  - alchemy's new `downstream.yml` runs alchemy-cli's two gates against
    every alchemy change, at the same branch name where alchemy-cli has
    one.
  - With four dependencies left, alchemy's `ci / ts (windows-latest)`
    takes about a minute.
- **The releases on it**, made and checked as in the first round, in
  `publish.sh` order:

  | Repo | Version | PR | Release commit | Published |
  |---|---|---|---|---|
  | alchemy | 0.2.4 | alchemy#57 | `2ddd50b` | npm, Go, crate, C library |
  | transduce | 0.2.5 | transduce#44 | `66d5242` | npm, Go, crate |
  | render | 0.2.4 | render#22 | `46c535c` | npm, Go, crate |
  | alchemy-cli | 0.1.6 | alchemy-cli#16 | `fbe55cf` | npm, Go, crate |

- **aless#49**: `Cargo.toml`'s floors and `Cargo.lock` take alchemy
  0.2.4, transduce 0.2.5 and render 0.2.4. `cargo update` also moved seven
  transitive crates to their newest compatible releases, each declaring a
  `rust-version` at or below aless's 1.88. Every gate passed, locally and
  in CI, the `msrv` job included.
- **web#59** pins `@tabnas/chess-view` at 0.1.6, and moves the `/releases`
  fallback versions of chess-view, alchemy, transduce, render and
  alchemy-cli. Every other pin and version was already npm's latest, and
  the generated site data was current. `npm run check` and the Vale gate
  pass.
- **Windows CI** (ruling 10, admin#138). The shared `polyglot-ci.yml`
  template now makes real links on Windows. Git Bash's plain `ln -s`
  deep-copies a tree and exits 0, and on alchemy's 18 dependencies the
  copies took 1,075 s of a 1,224 s job. It now makes an NTFS symlink, else
  a junction, else a copy, and logs which. It also takes a dependency from
  npm when its `ts/` is what its latest release was built from, it
  declares nothing the run builds from source, and the repo under test
  does not read its build by path. transduce#43 and debug#81 ran it as
  trials, closed unmerged afterwards. transduce's Windows TS job took
  71 s with the defaults and 163 s with every dependency built from
  source, against 952 s on `main`. The template is merged in admin; it
  reaches `tabnas/.github` only when the maintainer applies it (see
  "Decisions waiting").
- **admin#139**: with `gh` logged out, `verify.sh` now asks GitHub, with
  `git ls-remote`, about a template's repository that has no checkout
  beside admin, where it used to take it for absent (see "No red checks").
  `apply-workflows.sh`'s scope check, which ended silently when no one was
  logged in, now says to log in with the `workflow` scope.

### The third round: aless as a released tool, 2026-10-09

The maintainer then asked: "Report on the status of aless. Can it now be
prepared as a standalone cmd line utility in rust?"; "Prepare to match the
distribution options of jless.io; then search for best modern practices
for releasing a cross platform command line tool and tui like this"; and
"ensure that the help provided by the aless utility is full and
comprehensive so that agents can easily drive it, then do everything to
prepare up to the point that you need manual actions by me".

- **aless#50**: [dist](https://github.com/axodotdev/cargo-dist) 0.33.0
  builds and publishes aless. `dist-workspace.toml` is the configuration,
  and dist generates `.github/workflows/release.yml` from it; a pull
  request's `plan` job fails when the two disagree.
  - A release is a `workflow_dispatch` with the tag to make (`dry-run`
    by default). The `host` job creates the GitHub Release, and with it
    the tag, so a session never pushes one.
  - Eight targets, each built on a native runner: Linux x86_64 and
    aarch64 (glibc 2.35, and static musl), macOS x86_64 and aarch64,
    Windows x86_64 and aarch64.
  - Shell and PowerShell installers, a Homebrew formula pushed to
    `rjrodger/homebrew-tap`, and crates.io through
    `.github/workflows/publish-crates.yml`, by trusted publishing.
  - Build-provenance attestations, a CycloneDX SBOM, and cargo-auditable
    binaries. Every action is pinned by commit.
  - RELEASING.md, PACKAGING.md and CHANGELOG.md say how, and what the
    owner does once.
- **aless#51**: every option is in one table, `cli::OPTIONS`, and the
  parser refuses any option the table does not list. `--help` is the
  whole reference an agent needs: every option, what each output prints,
  every error kind with its fields, the exit statuses, paths, positions,
  formats and limits. `-h` is a summary. `--generate` writes the man page,
  completions for bash, zsh, fish and PowerShell, and the Agent Skill,
  from the same table. `man/aless.1` and `completions/` are committed,
  checked current by `tests/agent.rs`, and carried by every archive and
  by the crate.
- **aless#52**: the changelog's section is `[0.1.0] - 2026-10-09`, which
  dist takes as the Release's notes, and the man page takes its date.
  The README's Install section leads with the release channels: the
  archives and crates.io ship the release commit's README, as Codex's
  review pointed out. Its merge is `44c0a91`.
- **A dry run** of `release.yml` on `main` at `eae2e4f`, #51's merge
  (run 37916812440), built all eight archives without publishing, and
  passed. Each archive carries `man/aless.1` and `completions/`, the
  Windows zips with LF, as `.gitattributes` pins them. The released
  Linux binary writes the same man page and completions it ships.

The maintainer then asked whether the tap is a GitHub repository, for a
compliant crate description, for the community practice on the
dependencies' licences, whether jless is still the base code, and for
the man page and the completions to follow community practice.

- **aless#53**:
  - The tap formula installs the man page in `man1` and the bash, zsh,
    fish and PowerShell completions where Homebrew's formulas put theirs,
    with a test for `brew test`. `publish-homebrew.yml`, aless's own, replaces
    dist's Homebrew job: it runs `scripts/homebrew-formula.py` over the
    formula dist writes, and pushes the result to the tap. Checked with
    Homebrew itself, from a local tap: `brew test` passed, and
    `brew audit --strict` reported nothing.
  - The zsh completion also loads from `~/.zshrc`
    (`eval "$(aless --generate complete-zsh)"`), as bash's and fish's
    do from theirs.
  - A unit test holds `Cargo.toml`'s description to Homebrew's rules for
    a formula's `desc`, which it already kept.
  - THIRD_PARTY_NOTICES.md credits jless alone. aless is an independent
    implementation of jless's interface, and none of jless's code is in
    it. The OpenAPI example in `examples/` is one written for aless.
- **No licence bundle.** The comparable tools ship none: the release
  archives of ripgrep 14.1.1, bat 0.24.0, fd 10.2.0, jless 0.9.0, delta
  0.18.2, zoxide 0.9.6, uv 0.4.30 and starship 1.21.1 carry at most their
  own licence, and none of their dependencies' licence texts. aless's
  archives carry its licence and jless's notice, and its CycloneDX SBOM
  names every crate and its licence. If a bundle is ever wanted,
  [cargo-about](https://github.com/EmbarkStudios/cargo-about) writes one.

## No red checks

At the end of the second round, `scripts/verify.sh` finds no problem in
any of the 41 tabnas repositories cloned here. 37 are ok, and 4 are
pending, each past its release only by commits that ship nothing, which
ruling 6 leaves unreleased:

- chess, two commits past `ts/v0.1.13`: chess#59 changed `web/`, which
  ships as chess-view under its own tag, and chess#60 is documentation;
- json and lsp, one commit each (json#107 and lsp#43);
- parser, this page, whose second version is one more documentation
  commit.

alchemy is ok at 0.2.4: ADR-26 removed the test-only requires that kept
it pending after the first round.

At the fleet level `verify.sh` reports one problem, `staged-workflows`.
The shared `polyglot-ci.yml` template from admin#138 is ahead of the copy
deployed in `tabnas/.github`, and stays so until the maintainer applies
it (see "Decisions waiting"). A session in a container has seen this only
since admin#139. Before it, with `gh` logged out, `verify.sh` took
`.github`, `measure` and `status` for repositories that do not exist, and
compared none of their nine templates.

Every check run on the head of `main` is green in all 41 repositories and
in aless: each of the 42 heads has check runs, none failed or still
running, and no commit status is failing.

After the third round, aless's `main` is green at `44c0a91`, the
release commit: `ci` on Linux, macOS and Windows, and the minimum Rust,
1.88. The dry run of the release ran at `eae2e4f`, which differs from it
only in the changelog's heading, the man page's date and the README's
Install section.

In the first round one run had been red: parser's scheduled `fleet` run
at 10:32 UTC failed in feed's Go suite alone. It cloned feed 0.6.14, the
latest feed then, with xml 0.7.15, released about twenty minutes before.
xml 0.7.15 gives attributes to Go as a `*tabnas.OrderedMap`, and feed
reads that form only from feed#81, first released in 0.6.15. So every
attribute read as absent: an Atom link's `href` came out empty, and
`<rss version="0.91">` was detected as RSS 2.0. Re-run once feed 0.6.15
was out, the same job ended `FLEET PASS`. A Go program that requires xml
0.7.15 or later and feed 0.6.14 or earlier gets the same wrong answer, and
feed 0.6.15 is its fix.

## Pushed and open

Nothing. Every pull request of the three rounds is merged or, for the two
trials, closed; this page's fourth version goes up in its own.

## Decisions waiting on the maintainer

- **Release aless 0.1.0.** `main`, at `f66bd6a`, aless#53's merge, is
  the release commit. RELEASING.md says how, and these steps need the
  owner's accounts:
  1. On GitHub, create the public repository `rjrodger/homebrew-tap`,
     with a README so that it has a first commit; Homebrew reads
     `rjrodger/tap` as that repository. Make a fine-grained token whose
     only repository is that one, with *Contents: Read and write*, and
     store it in aless as the Actions secret `HOMEBREW_TAP_TOKEN`.
  2. Publish 0.1.0 to crates.io by hand: crates.io takes a trusted
     publisher only for a crate that exists. From a clean checkout of
     `main`, `cargo publish --locked`, with a token limited to the crate
     `aless`, the `publish-new` and `publish-update` scopes, and a day's
     expiry. Then add the trusted publisher (GitHub, `rjrodger`, `aless`,
     workflow `release.yml`, environment `release`), require trusted
     publishing, and revoke the token. `publish-crates.yml` then finds
     0.1.0 there and skips.
  3. Recommended: turn on release immutability, and give the `release`
     environment a required reviewer.
  4. Dispatch `release.yml` on `main` with the tag `v0.1.0`, and check
     the Release as RELEASING.md's step 4 says.

  After it: the starting points in PACKAGING.md for the AUR, a Scoop
  bucket and winget, and homebrew-core once aless meets its acceptance
  policy.
- **Apply admin#138 to `tabnas/.github`.** A session cannot attach that
  repository, so the shared workflow moves only when the maintainer
  applies the template. From admin, read the dry run's diff first:

  ```bash
  rollout/apply-workflows.sh --only 'dot-github__polyglot-ci.yml'
  rollout/apply-workflows.sh --apply --only 'dot-github__polyglot-ci.yml'
  ```

  Until then, `scripts/verify.sh` reports `staged-workflows`, with the
  template ahead of the deployed copy.
- **The engine's loose TypeScript types**, unchanged from
  [`2026-10-07.md`](2026-10-07.md). The other three items on that list
  are done: xml's embed check (xml#89), alchemy's wording (alchemy#51)
  and the census's located cells (admin#131, with alchemy#55).

## Carried over

- From [`2026-10-04.md`](2026-10-04.md), "Carried over", unchanged:
  Engine 3, the jsonic/go alignment, the Engine 1 and 2 follow-ups, the
  css citations in this repository, and css's linear selector-group scan.
- **Open issues**: parser #258. aless #29 waits on its owner: this
  session's credentials cannot delete branches. aless#48 closed #28.
- **Found, not fixed** on the previous page and still open: two engines
  from `tabnas_json::make()` cannot be merged in Rust. (The U+FEFF panic,
  feed's attribute order, and ini's and toml's stale AGENTS.md lines are
  fixed.)
- **chess-view** is released by a `web/v*` tag, which only the
  maintainer can push. 0.1.6 is out; the next release needs the same
  step.

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
- **A long `ci / ts (windows-latest)` was copying trees, not stuck.**
  alchemy's took about 25 minutes with 18 siblings, and this session
  cancelled and re-ran it once believing otherwise. Since alchemy#56 it
  takes about a minute. transduce's, with 18 as well, took 8 to 16
  minutes in this session, and will until admin#138 reaches
  `tabnas/.github`.
- **The shared Go job's workspace takes every `go.mod` it finds**,
  nested test modules included. json's `go/debugtest` replaces
  `github.com/tabnas/debug/go` with `../../../debug/go`, so a caller whose
  `deps` clones json must clone debug too, or its Go jobs fail. alchemy-cli
  found it.
- **Read a SHA; never type one.** A merge with a typed full SHA got a 409.
  Take it from `git rev-parse` or `git ls-remote`.
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
  free; it is rebuildable, so clear it between levels. In the second
  round six per-task target directories, from work already merged, held
  15 GB, and a release prep's link failed with `No space left on device`.
  Delete each one when its work merges.
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

  And from the second round, merged or closed:
  - `claude/release-<repo>-<version>` for the second table's four
    releases;
  - chess: `claude/chess-view-0.1.6` and `claude/chess-view-cdn-0.1.6`;
  - aless: `claude/aless-crates-io` and `claude/aless-alchemy-stack`;
  - alchemy: `claude/census-rows-located` and `claude/alchemy-tests-to-cli`;
  - alchemy-cli: `claude/alchemy-tests-to-cli`;
  - admin: `claude/alchemy-no-stage-deps`,
    `claude/polyglot-windows-links` and
    `claude/verify-unchecked-template-repos`;
  - transduce and debug: `claude/try-polyglot-links`, the closed trials;
  - web: `claude/web-alchemy-stack-2026-10-08`;
  - parser: `claude/handover-2026-10-08-second-round`, this page's
    second version.

  And from the third round, all merged:
  - aless: `claude/release-distribution`, `claude/help-and-release-prep`
    and `claude/release-0.1.0`;
  - parser: `claude/handover-2026-10-09`, this page's third version.
- `scripts/verify.sh` takes every git checkout beside admin for a tabnas
  repository. In a container whose `/home/user` holds aless too, it
  reports aless as a PROBLEM for having no `.tabnas-kind`; name the
  tabnas repositories on its command line there.
- No subscriptions or check-ins carry over.
