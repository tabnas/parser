# Contributing to parser

Thanks for your interest in contributing! The organization-wide conventions
in [tabnas/.github](https://github.com/tabnas/.github/blob/main/CONTRIBUTING.md) are
canonical and apply here. This file adds what is specific to
**tabnas/parser**.

Start with [`AGENTS.md`](AGENTS.md) — it is the working guide to this
repository for humans and agents alike.

## Build & test

This repository is *polyglot*: `ts/`, `go/`, and `rs/` hold the canonical
engine and its ports. **`ts/` is canonical; ports track it** — a behaviour
change normally lands in each affected runtime, with shared fixtures where the
surface overlaps.

```bash
make build   # builds ts/, go/, and rs/
make test    # tests ts/, go/, and rs/

# or per stack:
cd ts && npm install && npm run build && npm test
cd go && go build ./... && go test ./...
cd rs && cargo build --all-targets && cargo test --all-targets
```

The TypeScript side installs its only `@tabnas` devDependencies,
`@tabnas/debug` and `@tabnas/railroad`, from the npm registry, and neither
the Go module nor the Rust crate depends on another tabnas package, so a
plain build needs no other checkout. One test does: the README's ABNF
example requires `@tabnas/abnf`, which `ts/package.json` does not declare,
so the doc-examples harness falls back to a sibling `../abnf/ts`, and
`npm test` needs a built `abnf` checkout beside this repository. CI clones
`bnf`, `debug` and `abnf` (the `deps` in `.github/workflows/ci.yml`) and
builds them against this engine. Other sibling checkouts are optional: to
work against unreleased siblings, clone them into the same parent
directory and run admin's `scripts/link.sh`, which links them over
`ts/node_modules/@tabnas/*` and writes a `go.work` one level up. Never
commit that wiring.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/) are required,
for commit messages and PR titles alike. PRs are squash-merged, so a PR's
title is its commit message, and the GitHub Release that each release creates
lists those titles in its generated notes. They do not set the version: a
release is its own version-bump pull request, then a `release.yml` dispatch
(see [`AGENTS.md`](AGENTS.md), "Releasing"). For example:

```
feat: add lax mode for trailing commas
fix: handle CRLF inside block scalars
docs: clarify plugin ordering
```

Use `feat!:` / `fix!:` (or a `BREAKING CHANGE:` footer) for breaking changes.

## Pull requests

1. Open an issue first for anything larger than a small fix.
2. Branch from `main`; keep the PR focused on one change.
3. `make test` must pass for **all three** implementations.
4. PR titles follow Conventional Commits — PRs are squash-merged, so the
   title becomes the commit message.
5. CI must be green before merge.

## Security issues

Never open a public issue for a vulnerability — see [SECURITY.md](SECURITY.md).

## Code of conduct

Participation is covered by the org
[Code of Conduct](https://github.com/tabnas/.github/blob/main/CODE_OF_CONDUCT.md).
