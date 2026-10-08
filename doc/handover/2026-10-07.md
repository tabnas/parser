# Handover: the fleet's PRs merged and released, 2026-10-05 to 2026-10-07

This session ran on the maintainer's machine on 2026-10-05, with `gh` as
the maintainer, from the maintainer's instruction "Check each tabnas repo -
are all clean PRs merged? Resolve all PRs", then "Merge all the PRs and get
everything onto main locally", then "continue". It began from the previous
page, now [`2026-10-04.md`](2026-10-04.md), whose landing order it
finished. What is still open from that page is under "Carried over".

## What landed

**Every open tabnas PR is merged.** None is open in the org.

- **ADR-24, the port-deps test** (admin#113 and 36 repo PRs, opened by
  an earlier session this morning): `tools/port-deps.cjs` and its test
  are in every three-port repo, alchemy-cli included (alchemy-cli#2),
  with the registers recording the 34 differences found. admin's
  `docs/deps` was regenerated from the merged mains before admin#113
  merged. Its gate, `.github/workflows/port-deps.yml`, is green.
- **The alchemy shared-types set**, in the landing order of the previous
  page: alchemy#40, transduce#32, render#13, alchemy-cli#1 (from the
  patches in [`patches/`](patches/), now applied; the repository exists
  and its PR is merged).
- **admin#114**: `publish.sh` ORDER now reads `... feed alchemy transduce
  render alchemy-cli ...`, and `tasks/dep-order.js` no longer reports
  that set as a cycle. A go.mod require only the tests use gives way where
  the required repo's own code needs this one (alchemy's go.mod requires
  transduce and render for its end-to-end tests); every other go.mod edge
  still counts.

**Released at 0.2.0, the maintainer's choice of version** (the compile API
changed), each through a reviewed bump PR and a `release.yml` dispatch on
`main`, and each verified on npm, the Go proxy and crates.io:

| Repo | Bump PR | Release |
|---|---|---|
| alchemy | alchemy#42 | npm with `./shared`, `go/v0.2.0`, crate, and the C library Release `go/v0.2.0` |
| transduce | transduce#34 (go.mod takes alchemy v0.2.0) | npm, `go/v0.2.0`, crate |
| render | render#15 (go.mod takes alchemy and transduce v0.2.0) | npm, `go/v0.2.0`, crate |

Then alchemy#43 and alchemy-cli#3 moved their go.mod requires to the three
releases, which cleared the `GOWORK=off` step in both.

**alchemy-cli is published and releases through its workflows.**
- **0.1.0, by hand:** the maintainer published it to npm, and to crates.io
  with `tasks/crates/publish-first.sh` from the `ts/v0.1.0` tag, then
  registered both trusted publishers, each naming `release.yml`.
- **The release setup, alchemy-cli#4:**
  - `.tabnas-kind` is `TOOL`;
  - five workflows come from transduce's (`release.yml`,
    `crates-release.yml`, `github-release.yml`, `notify-status.yml`,
    `scorecard.yml`), mirrored in admin (admin#115);
  - there is one `VERSION` per port, each held to `ts/package.json` by a
    test.
- **0.1.1, the test release** (alchemy-cli#5): a `release.yml` dispatch
  published npm and crates.io through the trusted publishers, and tagged
  `ts/v0.1.1` and `go/v0.1.1`. npm's `gitHead`, both tags, the Go proxy
  and the GitHub Release all name the release commit, `03f733d`.

## On 2026-10-06

- **multisource dropped jsonic, and json with it** (multisource#73), on the
  maintainer's instruction: it depends on no grammar in any port, ships
  no `json` processor (an application registers one), and its C library
  runs on the bare engine with the caller's GrammarSpec (admin#116).
  Released as **0.6.0** (multisource#74): npm, `go/v0.6.0`, the crate and
  the C library Release all name commit `92ed3f5`, and npm's 0.6.0
  declares no jsonic peer. A GitHub Actions outage (about an hour on
  2026-10-05) cancelled its PR checks twice; they were rerun.
- **Docs and website**: tabnas/web gains a Streaming tier (alchemy,
  transduce, render 0.2.0) and alchemy-cli 0.1.1 under Command line, with
  multisource 0.6.0 and its how-to's JSON section; alchemy-cli's README
  says how to install it in each runtime (each install was tested against
  the registries); alchemy's language guide points at alchemy-cli for the
  command; multisource's release steps name every version site. admin's
  `docs/deps` and the TypeScript peer-dependency page were redrawn.
- **Port parity** is unchanged otherwise: 18 repositories differ in 34
  dependencies, all registered (decision 2). Twelve of them (11 grammars
  and feed) are the shape multisource had, a Go port reaching the engine
  through jsonic's aliases.

### Later on 2026-10-06: the engine and three packages released

- **parser 0.12.10** (parser#293): the engine changes on `main` since
  0.12.9. npm, `ts/v`, `rs/v` and `go/v`, the Go proxy, crates.io and the
  C library Release all name `33f4419`.
- **The cascade**: every Go module that requires the engine moved to
  v0.12.10 in its own PR (33 repositories, `deps/parser-v0.12.10`). Four
  needed more than `go.mod`, as publish.sh's release step would have done:
  - support, json and lsp commit `ts/package-lock.json`, which their
    engine-pin tests compare with the built engine, so its `@tabnas`
    entries moved with `npm update` (json's debug and railroad too);
  - json's nested `go/debugtest` replaces the engine, but also replaces
    json with the parent module, so its require had to move as well;
  - mcp (no Go module) moved its lockfile, and its `data/` copies of the
    engine's registry and DIVERGENCE.md were regenerated (`npm run
    gen-data`).
- **css 0.5.11, feed 0.6.12, jsonic-cli 0.5.11**: each released with the
  engine bump, and verified on every surface, except that
  **`tabnas-jsonic-cli` 0.5.11 is not on crates.io**. Its crates job
  failed: no trusted publisher was ever registered for the hand-published
  crate (admin#120 adds it to the scripts). Register it, then rerun the
  failed crates job of jsonic-cli's release run.
- **Tidy-up**: admin's workflow copies refreshed from the live files
  (admin#118); expr's stamp made current without losing its hand-added
  test (admin#119, expr#82); the website pins the engine at 0.12.10
  (web#54); the TypeScript peer-dependency page shows npm matching `main`.
- **chess's TypeScript perf test is flaky on shared runners**: it requires
  reuse to beat rebuild-per-parse by more than 4x and measured 3.4x and
  4.0x. It has failed on chess `main` since 2026-10-05.

### Still later on 2026-10-06: the maintainer's list, and the release wave

The maintainer's instruction "do this work" named five items. Each is
done:

- **The engine release wave.** Every repository whose Go module required
  engine v0.12.10, and every one changed below, was released: 37 in all,
  in dependency order. Each went through a release PR (admin `publish.sh`'s
  own step 2, run in a worktree), a green `main`, and a `release.yml`
  dispatch. Each was verified: npm `gitHead`, the `ts/v` and `go/v` tags,
  the Go proxy, crates.io and the GitHub Release all name the release
  commit.
  - support 0.3.7, bnf 0.1.26, chess 0.1.11, lsp 0.1.5;
  - json 0.5.14, debug 0.3.11, directive 0.5.11, hoover 0.3.12, markdown
    0.7.9, path 0.3.11, abnf 0.4.18, ebnf 0.1.12, gbnf 0.1.15, mcp 0.1.18;
  - jsonic 0.7.5, railroad 0.3.10, jsonl 0.1.13, semver 0.0.7, proto
    0.6.4;
  - css 0.5.12, csv 0.6.3, expr 0.5.13, json5 0.5.12, jsonc 0.5.11, xml
    0.7.13, yaml 0.5.20, zon 0.5.13, ini 0.5.15, toml 0.5.13, jsonic-cli
    0.5.12, multisource 0.6.1;
  - c 0.5.11, feed 0.6.13, alchemy 0.2.1, transduce 0.2.1, render 0.2.1,
    alchemy-cli 0.1.2.

  parser was not re-released (only docs since 0.12.10). The skills pin
  follows mcp 0.1.18 (skills#13). web#55 moves the website's pins,
  fallback versions and skills data.
- **alchemy's red `ci / ts`: structural shared types** (the maintainer's
  choice). alchemy#46 removed every `private` member from
  `ts/src/shared`, so two installed copies compare by shape. The shared
  `isFail` stays `instanceof`-only by design; `src/fail.ts`'s is the
  tolerant one. CI stayed red until the structural classes were on npm.
  After alchemy 0.2.1, `main`'s `ci / ts` is green on Linux, macOS and
  Windows.
- **Port parity: the 11 grammars and feed.** Their Go ports now name the
  engine directly (`tabnas "github.com/tabnas/parser/go"`), as the other
  ports do: expr#84, feed#74, json5#90, jsonc#80, yaml#118, css#62,
  csv#94, xml#83, zon#91, ini#96, toml#100. jsonc#80 had dropped
  jsonc.go's jsonic import. But `Jsonc` reads its grammar with
  `GrammarText`, which needs the text parser only jsonic registers (in
  its `init`). A program importing only jsonc and the engine then failed
  with "no text parser registered". jsonc#81 restores the import (blank,
  with the reason) and adds a test that keeps it; it shipped in 0.5.11.
  admin's `docs/deps` recorded 23 differences across 8 repositories after
  these, down from 34 across 18, all registered (admin#122, admin#123).
  The C-library fix below adds one (24 across 9, admin#124).
- **The older decisions:**
  - **alchemy's install footprint:** its json and engine peers are
    optional (alchemy#46). alchemy-cli, which runs the language, declares
    the engine itself (alchemy-cli#8).
  - **Rust manifests:** render's engine is a dev-dependency (render#17).
    transduce drops `indexmap`, and `serde_json` is test-only
    (transduce#36).
  - **semver and proto ship their grammars precompiled** (semver#34,
    proto#60), so abnf and bnf are build-only in every port:
    - **semver** has one `semver-grammar.json`, shared by all three ports.
    - **proto** has one per port, because the TypeScript compiler writes
      the keyword guard as a lookahead, which Go's and Rust's regex
      engines cannot run.
    - **Staleness:** a test in each port recompiles and fails when the
      committed file is stale.
    - **What still parses the same:** every value, error code and
      position, over 111,111 semver strings and proto's 646 inputs.
    - **Visible changes:**
      - semver Go's diagnostics use the TypeScript names for three
        character-class tokens;
      - proto TS leaves no `tn.abnf` behind after `use(Proto)`;
      - its errors list `["Proto"]` as plugins.
  - **json instead of jsonic** for css, csv, xml, zon, ini and toml: tried
    in every port, and kept nowhere. Every port reads its
    `*-grammar.jsonic` with jsonic at run time, and json cannot read it
    (it stops at the first `#`). So the swap would add json rather than
    replace jsonic. With json as the base:
    - csv, ini and toml fail most of their suites: json closes a pair
      only on `,` or `}`, rejects empty input, and turns off text and
      comments;
    - xml and css fail fewer, on whitespace, embed mode and empty input;
    - **ZON came close.** Its tests pass on json if the grammar's six
      close rules take json's `@setval$`/`@push$` and NaN leaves json's
      `result.fail`. The grammar would then work only on json, and would
      have to reach the runtimes as JSON (decision 3 below).
  - **admin's TypeScript peer check** (admin#121, ADR-25):
    `tasks/ts-peers.js --check` holds each `ts/package.json`'s @tabnas
    declarations to what `ts/src` imports, in both directions. It runs in
    the `port-deps` workflow. Four exceptions are allowlisted with
    reasons: expr's jsonic, lsp's engine, multisource's path, and
    alchemy-cli's engine.
- **chess's timing test is robust** (chess#49): best of three rounds,
  rebuild more than 2x reuse.
- **The final audit.**
  - **`make dist`:** all 38 repositories match their checkouts on every
    surface. Its one disagreement, the site advertising mcp 0.1.17, cleared
    with web#55.
  - **`make verify`:** three findings.
    - **Pending, the known test-only cycle:** alchemy's go.mod requires
      render and transduce 0.2.0 for its end-to-end tests, and those were
      released after it.
    - **A CLIB error:** since feed#74 and xml#83, neither library imports
      jsonic, yet both C libraries were hosted on jsonic. That breaks ADR-22
      (a C library adds no dependency). admin#124 moves both hosts to the
      engine, as multisource's moved in admin#116, and feed#76 and xml#85
      restamp `go/clib/core.go`. A probe parsed every input under each
      repo's `test/` both ways:
      - xml: 3,420 inputs, all identical;
      - feed: 4,037 inputs, 5 different. The same 5 also differ between two
        jsonic-hosted parsers (see "Session notes").

      jsonic is now a runtime dependency of xml's and feed's other ports only:
      xml registers a new difference, and feed's narrows to Rust. The CLIB
      check is current for all 30 stamps. These re-hosts are on `main` and
      not released; they ride the next xml and feed releases.
    - **Five stale plugin descriptors** (`tabnas.plugin.json`, checked by
      `tasks/ax-descriptor.sh`): css, feed, proto, semver and toml.
      - **Caused today:** proto's names `@tabnas/abnf` as its base, which
        proto#60 made build-only; semver's has the same base change.
      - **Stale before today**, checked against each previous release's
        commit:
        - css since 2026-09-19 (a hand-added `rust` field the generator
          does not emit);
        - feed since feed#71 (xml became derivable as its base);
        - semver's field order and its `.abnf` grammar field;
        - toml's field order and error codes.
      - **Left alone:** regenerating would drop the hand-kept fields, and
        would change the plugin catalog bundled in mcp (a release) and on
        the website (decision 6 below).

## On 2026-10-07: the maintainer's rulings, and everything changed released

The maintainer answered every open decision directly. Each ruling, and what
it led to:

- **Plugin descriptors: fix the generator first.**
  - admin#125 taught `ax-descriptor` three things:
    - the `rust` field, from each `rs/Cargo.toml`, since every plugin has
      a published crate;
    - a root grammar source in any notation (semver's `.abnf`), never the
      compiled `.json`;
    - the schema's field order.
  - 27 descriptor PRs and multisource#80 followed, and every descriptor is
    current (28 of 28).
  - The catalogues built from them followed:
    - mcp's `data/plugins.json` (mcp#32);
    - lsp's bundled registry, last generated 2026-08-21 (lsp#39);
    - the website's `plugins.json` (web#56).
- **expr's kind is `grammar`**, as its descriptor has said since 2026-08-20.
  lsp's contradicting `modifier` override is gone (lsp#39).
- **feed's and xml's C-library re-host rides their next release**, which
  came the same day.
- **ZON stays on jsonic.** **semver's Rust port keeps abnf removed.**
  **The precompiled grammars stay regenerated by hand**, with their
  staleness tests.
- **The port-dependency register: every entry is now ruled.**
  - **Intentional, recorded in each register:** lsp (15 optional Rust
    grammars), jsonic-cli (json, parser), railroad (json), support
    (parser) and alchemy-cli (parser) — lsp#37, jsonic-cli#51,
    railroad#64, support#49, alchemy-cli#10.
  - **transduce:** "don't use any, use proper types". Its TypeScript
    names the engine's types (`ParserSource` takes a `Tabnas`), and
    `@tabnas/parser` is a required peer (transduce#38).
  - **multisource:** the optional `@tabnas/path` peer is dropped
    (multisource#78; admin#126 removes its ts-peers allowlist entry).
  - **feed:** Rust builds on the engine (feed#78).
  - **xml:** jsonic is removed in every port (xml#87). TypeScript
    installs its grammar from JSON, written by `npm run embed` and held by
    a staleness test, and Rust builds on the engine. Parity was identical
    on all 3,593 xml inputs and 4,105 feed inputs, in every port.
    - **Embed mode fails fast in every port** (the maintainer's ruling):
      installing with `embed: true` on a host without jsonic raises "embed
      mode needs a jsonic host", and Rust's `make_with` panics, documented.
    - **The embed step stays out of `npm run build`.**
    - **xml's Rust perf guard** is best of three rounds, more than 2x, as
      chess's is.
  - admin's `docs/deps` now records 20 differences across 5 repositories,
    every one ruled (admin#127, admin#128).
- **ax-codes: teach it what it missed, and alchemy declares its codes.**
  - admin#129 and admin#130: ax-codes reads a quoted `"error": {`
    catalogue (alchemy's), and treats `ERROR:install` as the fixture
    runners' marker for a grammar the engine refuses to install.
  - alchemy#49 declares its resolver, checker and runtime codes in its
    grammar's `error` block, 30 in all. A test in each port holds every
    raising site to its catalogue line, and 22 new fixture rows exercise
    every code.
  - `ax-codes --strict`: 65 declared, 0 uncovered, 0 orphan.
- **The engine's loose TypeScript types wait** (decision 1 below).

**Released, all verified** (npm `gitHead`, both tags, the Go proxy,
crates.io, the GitHub Release), in dependency order:
- 33 packages with commits since the first wave, plus mcp 0.1.19:
  - support 0.3.8, bnf 0.1.27, chess 0.1.12, lsp 0.1.6;
  - json 0.5.15, debug 0.3.12, directive 0.5.12, hoover 0.3.13,
    markdown 0.7.10, path 0.3.12, abnf 0.4.19, ebnf 0.1.13, gbnf 0.1.16,
    mcp 0.1.19;
  - jsonic 0.7.6, railroad 0.3.11, jsonl 0.1.14, semver 0.0.8, proto
    0.6.5;
  - csv 0.6.4, expr 0.5.14, json5 0.5.13, jsonc 0.5.12, xml 0.7.14, yaml
    0.5.21, zon 0.5.14, ini 0.5.16, toml 0.5.14, jsonic-cli 0.5.13,
    multisource 0.6.2;
  - c 0.5.12, feed 0.6.14, transduce 0.2.2, alchemy-cli 0.1.3.
- Then, after alchemy#49: alchemy 0.2.2, transduce 0.2.3, render 0.2.2,
  alchemy-cli 0.1.4.

Not released: parser, whose only commits are these handover pages (the
maintainer's choice), and css, which had no commit of its own.

The skills pin follows mcp 0.1.19 (skills#14). web#56 and web#57 cover
the website:
- the pins, the fallback versions and the catalogue;
- the skills data;
- Vale counts re-measured for the two new catalogue pages.

**Hitches on the way:**
- lsp#38's release found lsp's Rust fleet map stale after feed and xml
  dropped jsonic. lsp#39 fixed it, with the registry regeneration.
- transduce's and alchemy's `Cargo.lock` still listed jsonic under xml
  and feed. publish.sh's bump stamps version lines only, so those were
  refreshed by hand (in the transduce release PR, and in alchemy#48).
- mcp's MCP-registry publish hit a 504 and succeeded on a rerun. The
  registry then stopped answering for a few hours. Once it was back,
  `make dist` confirmed the 0.1.19 entry, and every surface of all 38
  repositories matched.
- npm sometimes took over 12 minutes to serve a new version's `gitHead`.

**`make verify` passes** (exit 0) for the first time in these sessions.
What stays pending is normal between releases:
- parser's docs;
- the skills pin commits;
- alchemy's test-only Go requires, which lag render and transduce by
  design;
- css's go.mod, which names the previous json, jsonic and support because
  css had nothing to release.

## Pushed and open

- **web#57**: the website's fallback versions for alchemy 0.2.2,
  transduce 0.2.3, render 0.2.2 and alchemy-cli 0.1.4. Merging it
  deploys the site.

## No red checks

alchemy's `ci / ts` is green on every OS since alchemy 0.2.1 put the
structural shared types on npm. Adding a `private` or `protected` member,
or a `#` field, to a class in alchemy's `ts/src/shared` brings it back: two
installed copies would compare nominally again. `ts/src/shared/index.ts`
says so.

## Decisions waiting on the maintainer

1. **The engine's loose TypeScript types** ("later", ruled 2026-10-07).
   Typing transduce found `any` in the engine's own types:
   - `Tabnas.options` and plugin options (`Record<string, any>`);
   - the `sub()` spec;
   - `token()` and `parse()` results;
   - `Rule.node` and `Rule.u`.

   Tightening them is a parser release, which moves every package's
   engine require.
2. **xml's embed check is loose.** It looks for any `val` rule, so a host
   whose `val` comes from another grammar (strict json) would pass it.
3. **alchemy's wording inconsistencies**, left alone because behaviour was
   frozen while its codes were declared (alchemy#49's PR body lists them):
   - `bad_let`, `bad_if` and `bad_match` are also raised as
     `DSL_TYPE_ERROR`;
   - resolver and desugarer word the same failures differently.

   Some codes stay undeclared because no single-source program reaches
   them (alchemy's AGENTS.md lists them).
4. **The census ignores `ERROR:<code>@row:col` cells.** If ax-codes read
   them, alchemy's 10 bare-code rows added for the census would not be
   needed.

## Carried over

Unchanged from [`2026-10-04.md`](2026-10-04.md), "Carried over": Engine 3,
the jsonic/go alignment, the Engine 1 and 2 follow-ups, the css citations
in this repository, and css's linear selector-group scan.

**Open issues:** parser #287 and #258 as before. aless #28 (crates.io
versions) no longer waits on alchemy, transduce and render. aless #29 can
be closed (ruling 6 of the previous page).

## Working method that held up

- **Read a red check before trusting a page that predicted it.** The
  previous page expected alchemy's `ci / ts` to go green after the merges;
  it went red for a different reason, which only the failure text showed.
- **A release that the orchestrator cannot verify goes by hand, in the
  orchestrator's steps.** `publish.sh` verifies a repo against its
  published dependencies, so alchemy (whose tests need transduce 0.2.0,
  which needs alchemy 0.2.0) could not pass it. The bump PRs did what its
  bump does (the four version sites, the path entries of `Cargo.lock`,
  every tabnas go.mod require at its latest release, `go mod tidy`) and
  the `rust` check, which takes siblings by branch, was the gate.
- **npm can take a minute or two to serve a version `npm publish` has
  accepted.** Poll the version document before calling a release missing.
- **A release wave as PRs.** Admin `publish.sh`'s helper functions and its
  step-2 block run cleanly in a worktree (`eval`ed, with `ROOT` at a
  folder of sibling checkouts). That gives every version site, the Go
  requires, nested modules and Rust locks without hand edits. Two gaps
  needed filling:
  - a committed lockfile's own `version` moves only when a dependency did;
  - mcp's `release.yml` takes no `go` input.
  Fast-forward every `main` checkout before each dependency level.
- **Generators and checks that enumerate siblings skip symlinks**
  (port-deps `--write`, web's gen-ax-data and check-ax, mcp's gen-data).
  Run them with `--root` at the real checkouts, or move the worktree among
  them with `git worktree move`.
- **npm's CDN can serve a 404 for a tarball minutes after publishing.**
  Two alchemy PR checks failed that way on ini 0.5.15. Rerun before
  reading anything into it.

## Session notes

- Every worktree of these sessions is removed. Every `main` checkout is
  clean and current.
- The TypeScript peer-dependency page was re-read after the second
  release and republished in place (version 7) at
  <https://claude.ai/artifact/R5c6Hag7eawFfUZrJitjaU>. The duplicate
  diagram artifact `5UQXW2KkB9Gv7LSdW7fwhQ` could not be found from the
  maintainer's account on 2026-10-07, so it is already gone or belongs to
  another account.
- Found, not fixed:
  - the Rust engine panics on a lone U+FEFF when the text matcher is off
    (`parser/rs/src/lexer.rs:1413`); a few xmlconf inputs reach it, and
    the engine reports `internal`;
  - feed's Go port writes XHTML attributes in Go map order, so 5 of the
    4,037 inputs under feed's `test/` can give different output on
    different runs;
  - two engines from `tabnas_json::make()` cannot be merged in Rust;
  - ini's and toml's AGENTS.md describe `">=2"` peers and `file:`
    devDependencies that `package.json` no longer has.
- No subscriptions or check-ins carry over.
