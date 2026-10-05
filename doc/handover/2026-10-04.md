# Handover: alchemy's shared types, and the fleet's open threads, 2026-10-04

This session ran from 2026-10-02 21:34 UTC until this page was written,
on 2026-10-04 at about 17:30 UTC, on the maintainer's instruction. It
began from the previous session's handover page in aless (aless #41,
closed unmerged by ruling 5 below). This page records what is open, the
rulings the maintainer gave, the work that exists nowhere else (the
`alchemy-cli-*` patches in `patches/`), and the order the open work has
to land in. The handover before this one, of 2026-09-30, is now
[`2026-09-30.md`](2026-09-30.md); what is still open from it is under
"Carried over".

## Pushed and open

The maintainer's instruction of 2026-10-04: "Transduce and render should
depend upon alchemy. Alchemy should have the shared types in all
languages. Rust, Go, and TypeScript." Rulings 11 and 12 below settled the
two questions it raised.

| PR | Branch head | State |
|---|---|---|
| tabnas/alchemy#40 | `96f1414` | Merge first. alchemy owns the shared types, takes its routers and renderers from the host, and the `alchemy` command leaves for tabnas/alchemy-cli. |
| tabnas/transduce#32 | `62fc24e` | Merge second. Takes the shared types from alchemy, re-exports them under their old names, and offers its stages as alchemy's `Routers`. |
| tabnas/render#13 | `b1cb614` | Merge third. Takes the shared types from alchemy instead of transduce, and offers its renderers as alchemy's `Renderers`. |

All three are on the branch `claude/alchemy-shared-types`. Each head is
the reviewed change plus one comment-only commit made while this page was
written: the header of `.github/workflows/release.yml` gave the old
release order, transduce and render before alchemy.

**CI.** The `rust` workflow takes every sibling from the branch of the
same name, so it tests the three together. It was green on all three
before the comment commit, and was running again on the new heads when
this page was written. The `ci` workflow, the org's shared polyglot one,
clones siblings at `main`, and its TypeScript and Go jobs fail until the
set is there:

- alchemy's tests import `routers` and `renderers`, which transduce's and
  render's `main` lack;
- transduce and render import `@tabnas/alchemy/shared`, which alchemy's
  `main` lacks.

No order turns every PR green before it merges. Merging alchemy#40 first
means it alone merges red. Each PR carries one comment saying so.

### The change in brief

- **The shared types** live in a unit that depends on nothing outside
  itself:
  - TypeScript: `ts/src/shared/`, published as `@tabnas/alchemy/shared`;
  - Go: the package `github.com/tabnas/alchemy/go/shared`;
  - Rust: `tabnas_alchemy::shared`, the crate with
    `default-features = false`, the language sitting behind the default
    `language` feature.

  It holds the event protocol moved from transduce (events, sinks, the
  table protocol, `Fail` and `Code`, limits, selectors, datums), the
  types alchemy's lowering hands on (transduce's captures, render's
  `TextOut` and its CSV and JSON options), and two interfaces, `Routers`
  and `Renderers`.
- **transduce implements `Routers`** (`routers`, `Routers()`, `routers()`)
  and **render implements `Renderers`** (`renderers`, `Renderers()`,
  `renderers()`). Both re-export every moved name at its old path, so
  each public API is unchanged apart from that one new name.
- **The compile API is the breaking change.** alchemy's runtime imports
  neither package and takes both implementations from its host:
  `compile(src, file, { routers, renderers })` in TypeScript,
  `Compile(src, file, routers, renderers)` in Go, and `compile(src, file,
  routers, renderers)` with `Arc<dyn Routers<Val>>` and
  `Arc<dyn Renderers>` in Rust.
- **Dependencies, as instructed.** transduce and render take alchemy (in
  Go and Rust, the shared unit alone). render drops transduce at run
  time, and alchemy drops both; each keeps them as development
  dependencies for its end-to-end tests.

## Pushed, no pull request yet

| Branch | Head | State |
|---|---|---|
| rjrodger/aless `claude/alchemy-injected` | `29934c8` | aless passes transduce's routers and render's renderers to `compile` and `compile_sources` from one place, `src/alchemy.rs`, and `Cargo.toml`'s patch tables follow the new manifests. `Cargo.lock` still pins the three crates' current `main`, so the branch does not build until the PRs above merge. Checked in a scratch copy pinned to their branches: clippy clean, 325 of 325 tests with every outcome identical to aless `main`, the pty smoke test green. aless CI runs only on `main` and on pull requests, so the pushed branch has no red run. |

## Local work, saved in `patches/`

tabnas/alchemy-cli could not be attached: at 16:38 UTC GitHub answered
that the repository does not exist or the Claude GitHub App cannot reach
it. These two patches are the only copy of it.

| Patch | Applies to | State |
|---|---|---|
| `alchemy-cli-1-license.patch` | tabnas/alchemy-cli, `main` | LICENSE alone, the repository's first commit. |
| `alchemy-cli-2-tree.patch` | tabnas/alchemy-cli, `claude/alchemy-shared-types` | Everything else, for its first PR: the TypeScript, Go and Rust commands, their tests, README, AGENTS.md with `CLAUDE.md` linked to it, CI, and `rs/Cargo.lock`. |

Once the maintainer has created the empty public repository and given the
app access:

```bash
mkdir alchemy-cli && cd alchemy-cli && git init -b main
git am ../parser/doc/handover/patches/alchemy-cli-1-license.patch
git remote add origin https://github.com/tabnas/alchemy-cli
git push -u origin main
git checkout -b claude/alchemy-shared-types
git am ../parser/doc/handover/patches/alchemy-cli-2-tree.patch
git push -u origin claude/alchemy-shared-types
```

Then open the PR, ready for review. The patches were applied to an empty
repository while this page was written, and the result was identical to
the source tree, the symbolic link included.

**Verified while this page was written**, against sibling checkouts of
alchemy `9d02378`, transduce `43b6c12` and render `7a8965a` (the PR heads
before the comment commit) and parser, json, jsonic and csv at `main`:

- `ci/rust/run.sh`: fmt, build, 19 tests, clippy, all `--locked`;
- `ci/polyglot/run.sh`: TypeScript 20 of 20, Go plain and with
  `-tags tabnas_nodecell`, and vet.

These are the CLI tests that left alchemy: 19 in Rust, 20 in TypeScript,
and Go's with `go/cmd/alchemy`.

Its CI clones siblings at `main`, like the others, so it goes green once
the three PRs are there. Watch one thing. When render#13's Go job looked
for a package missing from its module, Go walked the whole module graph
and stopped on json's `go/debugtest`, which replaces debug with
`../../../debug/go`. That stops once alchemy's `main` has `go/shared`. If
alchemy-cli's Go job meets it anyway, add `debug` to the deps in its
`ci.yml`.

The npm and crates.io setup for the new packages is the maintainer's: npm
registers a trusted publisher only for a package that exists, so the
first publish is by hand.

## Landing order

1. Merge alchemy#40.
2. Rerun `ci` on transduce#32. It should go green untouched; merge it.
3. Rerun `ci` on render#13, which needs transduce#32 on `main` because its
   tests take events from transduce's sources. Merge it.
4. Rerun the `ci` run that step 1 started on alchemy's `main`, red only for
   the ordering.
5. **admin, `publish.sh`:** move alchemy before transduce in `ORDER`.
   `... feed transduce render alchemy multisource ...` becomes
   `... feed alchemy transduce render multisource ...`. Rewrite the
   comment above it as well, which says render takes transduce and
   alchemy takes both. Check with `node tasks/dep-order.js --check`
   against checkouts at the merged mains.
6. **Release, in order:** alchemy first, then transduce, then render.
   - alchemy's release carries npm with `./shared`, the Go tag that
     carries `go/shared`, and the crate with the feature split.
   - Each goes through its repository's `release.yml`, dispatched on
     `main` after a reviewed bump PR. A session cannot push tags.
   - The compile API changed, so the version numbers are the maintainer's
     call.
   - Then move `github.com/tabnas/alchemy/go` in transduce's and render's
     `go.mod` from v0.1.3 to the new tag. Until then a `GOWORK=off` build
     cannot find `go/shared`.
7. **alchemy-cli:** apply the patches as above and open its PR.
8. **aless:** on `claude/alchemy-injected`, run
   `cargo update -p tabnas-alchemy -p tabnas-transduce -p tabnas-render`,
   then the four gates in its AGENTS.md, then open the PR.

## Rulings the maintainer gave in this session

1. **Parser 0.12.9: bump, dispatch, cascade.** Done. Released and
   verified on npm, crates.io, the Go proxy and the clib Release, with
   the `go.mod` bump cascaded through the fleet.
2. **parser #244: fix yaml's guard.** Done in yaml #113, with Go's derive
   order registered in parser #283.
3. **parser #250: implement `rule.finish` in Rust.** Built, then
   withdrawn. The fleet gate showed `@tabnas/json` failing 13 TypeScript
   tests and rows of its Go `errors.tsv`, because json and jsonic use the
   option in jsonic's auto-close sense. parser #284 instead documents
   `rule.finish` as the grammar's end-of-source switch in all three
   runtimes. It closed #250, which records the measurement.
4. **The transduce, render and alchemy release path: start with the
   defaults.** Done. transduce 0.1.2, render 0.1.2 and alchemy 0.1.3 are
   on npm, alchemy with its C library for five platforms.
5. **aless #41, the previous session's handover page: close unmerged.**
   Done.
6. **aless #29: delete the ten merged branches, and pin actions by commit
   SHA.** Done, the pinning in aless #44. The issue's other item, #19, is
   closed, so #29 can be closed.
7. **toml #89: refuse the four TOML-invalid documents.** Done, in all
   three ports.
8. **parser #259: fix it in Rust now.** Done.
9. **The dependency diagram reads `package.json`, and the unnecessary
   dependencies go** (2026-10-03 and 10-04). Done:
   - five unneeded tabnas peers dropped;
   - multisource's path peer made optional;
   - web's eight stale pins bumped;
   - transduce's csv and json peers made optional, loaded when a line
     source starts.
10. "These are components. The user is meant to compose them. They should
    not drag along unnecessary dependencies." (2026-10-04)
11. **The runtime takes its routers and renderers by injection** (it
    stays in alchemy, which defines the interfaces).
12. **The `alchemy` command moves to a new repository**,
    tabnas/alchemy-cli. Its npm and crates.io publishing setup is the
    maintainer's.

**Standing rules:**

- Dependencies change only on explicit instruction, and versions track
  the latest release.
- Pull requests open ready for review, never as drafts.
- A transient task prints a status line at least every 30 seconds.
- Releases go through the workflows: a session's credentials cannot push
  tags, and nothing is published from a local machine.
- web holds astro, @astrojs/mdx and @astrojs/cloudflare on majors 5, 4
  and 12.

## Decisions waiting on the maintainer

1. **The TypeScript install footprint.** transduce and render now peer on
   `@tabnas/alchemy`, so npm also installs its required peers,
   `@tabnas/parser` and `@tabnas/json`. Go and Rust avoid that through
   the subpackage and the feature. Making alchemy's two peers optional,
   loaded when a program is compiled, would remove it. It is a dependency
   change.
2. **Two Rust manifests, both dependency changes:**
   - render's `tabnas` dependency is used only by `tests/json_readback.rs`
     (move it to dev-dependencies?);
   - transduce's `indexmap` is unused, and its `serde_json` is used by
     tests only.
3. **The structural items from the dependency review** (2026-10-03),
   none started:
   - semver and proto ship their ABNF grammar compiled, so abnf and bnf
     become build-only dependencies;
   - css, csv, xml and zon (which exclude `jsonic,imp`) and ini and toml
     (which exclude `jsonic`) try json instead of jsonic: swap
     `use(jsonic)` for `use(json)` and run each suite;
   - an admin check that compares each `package.json`'s peers with what
     `ts/src` imports, failing in both directions, with an allowlist for
     host-only peers like expr's.

   The review's first item, callers supplying transduce's line-source
   parsers, is done as ruling 9's optional peers.
4. **Delete the duplicate diagram artifact `5UQXW2KkB9Gv7LSdW7fwhQ`?** The
   one kept is <https://claude.ai/artifact/EbqCuLdZ4mh1wSGQdTfjKZ>.
5. **The version numbers** for step 6's releases.

## Carried over

From [`2026-09-30.md`](2026-09-30.md), with what this session learned:

- **Engine 3, merge of a removed fixed token** (ruling 3 there). Not
  started; the `parser-3a` and `parser-3b` patches are as they were.
- **jsonic/go alignment** (ruling 4 there). Not landed: jsonic's `main`
  has no `test/grammar/alternates.json`. The csv, multisource and toml Go
  fixes still go first, then jsonic.
- **The Engine 1 and 2 follow-ups** that parser #274's body lists:
  - Rust's `unterminated_comment` span and cursor against TypeScript's;
  - the partial-value divergence for a key that never parsed;
  - a test that a give-up returns the node as a failing callback left it.
- **The css citations, now unblocked.** css#56 (`c952a32`) landed the Go
  css fix, replacing `Chars: ""` with `Lex: false`. In this repository:
  - The empty-`Chars` entry in `DIVERGENCE.md` still counts css among the
    call sites in its adoption-cost table (`css, csv, yaml | 1 each`, and
    "fifteen call sites"). It also says "of the four call sites in the
    fleet that set an empty value, only css's was live".
  - `go/options.go` says css was "Fixed there by also saying Lex:false",
    citing tabnas/css#24. The fix that landed is css#56, and it replaced
    `Chars: ""` rather than adding to it. `go/empty_chars_test.go` cites
    css#24 for the same history.
  - Drop css from the count, and cite css#56.
- **css TypeScript and Go: the linear selector-group scan.** Not started.

**Open issues:**

- **parser #287:** Go reads `string.replace` after the escape and control
  checks. The repair is written in the issue and held on jsonic's ledger.
  Delete `string-replace-control` from jsonic's `test/spec/divergent.tsv`,
  release jsonic, then land the reorder and close the register group.
- **parser #258:** the per-rule cost of the Rust port.
- **aless #28:** the move to crates.io versions waits on twelve crates
  releasing what their `main` carries. alchemy, transduce and render now
  need step 6's release as well.
- **aless #29:** done, and can be closed (ruling 6).

## Tools

`tools/tsdeps/` regenerates the TypeScript peer-dependency page linked in
decision 4 from every repository's `main`. Its README lists the steps,
and `last-read/` holds the read the published page shows, so the next
page can say what changed.

## Working method that held up

- **Check a cross-repository change in a scratch copy pinned to the
  branches.** For aless, the copy's git dependencies named the branch;
  the checkout itself never did.
- **Compare per-test outcomes with `main`, not only pass counts.** It is
  the check that says nothing was skipped or renamed away.
- **Before reading a failure, read where the job takes its siblings.**
  Here the `rust` workflows take same-named branches, and the shared `ci`
  takes `main`.
- **Reproduce a CI-only failure from a copy, not through symlinks.**
  Symlinked sibling checkouts (`/home/user/json` pointing into
  `/home/user/tabnas/json`) resolved differently from CI's clones until
  the tree was copied.
- **Work in small steps that each report.** On 2026-10-04 the maintainer
  called out 32 silent minutes spent reading `package.json` files, work a
  deterministic scan does in a few. Silence reads as a hang.

## Session notes

- This session's subscriptions to PR activity, and its check-ins, do not
  carry over. A successor subscribes its own session to the three PRs. No
  check-in is scheduled.
- The checkouts and scratch copies lived in this session's container and
  go with it. Everything above is pushed, or in this directory.
