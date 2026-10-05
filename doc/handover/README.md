# Handover: the fleet's PRs merged, and alchemy, transduce and render at 0.2.0, 2026-10-05

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

## Pushed and open

Nothing. rjrodger/aless#45 (step 8 of the previous page) is merged:
`claude/alchemy-injected` plus one commit putting every tabnas pin in
`Cargo.lock` at its repository's current main, 21 crates. The three alone
could not move: every tabnas crate shares one engine and one json, and the
other pins sat on revisions that require parser 0.12.8. Only tabnas
packages changed; 325 of 325 tests, and CI on all three platforms.

## One red check, and why

**alchemy's `ci / ts`** (the org's shared `polyglot-ci.yml`) is red on
every OS, and nothing in alchemy's code causes it. alchemy's tests import
transduce and render, which the shared workflow builds from `main` as
siblings. Each of those has its own `node_modules/@tabnas/alchemy`, the
published copy, because the workflow links siblings into the repository
under test but never the repository under test into its siblings. So
alchemy's tests see two copies of the shared classes, and TypeScript
refuses to assign one to the other (`AbortFlag`, `Routers`). Before the
release the same gap showed as `Cannot find module '@tabnas/alchemy/shared'`.

The `rust` workflow's `ci/polyglot/run.sh` builds alchemy first and links
it into transduce and render, and it is green: that is alchemy's
TypeScript coverage today (Linux). The fix is the maintainer's to choose
(decision 1 below).

## Decisions waiting on the maintainer

1. **alchemy's shared `ci / ts`.** Options:
   - teach `tabnas/.github` `polyglot-ci.yml` to link the repository under
     test into the siblings that installed it, and give alchemy a
     `build-order` with alchemy before transduce and render. That fixes
     Linux and macOS. Windows copies instead of linking, so the two-copy
     problem stays there;
   - set `run-ts: false` in alchemy's `ci.yml` and rely on the `rust`
     workflow's polyglot gate (Linux only);
   - make the shared types compare structurally (no private members on
     `AbortFlag` and the like), so that two copies agree.
2. **The 34 port-deps register entries** each end "Undecided: awaits the
   maintainer's ruling" (ADR-24). `docs/deps/README.md` in admin lists
   them with their reasons and repairs.
3. From the previous page, still open: the TypeScript install footprint
   (transduce and render peer on alchemy, whose required peers npm then
   installs); render's `tabnas` and transduce's `indexmap` and
   `serde_json` in Rust; the structural items from the dependency review;
   deleting the duplicate diagram artifact `5UQXW2KkB9Gv7LSdW7fwhQ`.
   Version numbers for the releases are settled (0.2.0).

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

## Session notes

- Worktrees for this session's PRs are under
  `~/Projects/tabnas-worktrees/release-0.2.0/` on the maintainer's
  machine; every branch in them is merged.
- The TypeScript peer-dependency page (`tools/tsdeps/`) was redrawn on
  2026-10-05 after the shared-types merges, the 0.2.0 releases and
  multisource dropping jsonic and json (multisource#73, admin#116), and
  published afresh at <https://claude.ai/artifact/R5c6Hag7eawFfUZrJitjaU>:
  the earlier copy could not be reached from the maintainer's machine.
  admin's `docs/deps` (ADR-24) already showed the new edges.
- No subscriptions or check-ins carry over.
