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

Tabnas repos resolve their unpublished `@tabnas/*` siblings from
**side-by-side checkouts**, so clone this repo's tabnas dependencies into the
same parent directory. Check `.github/workflows/` for the exact list.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/) — release
automation derives versions and changelogs from them, so this is required:

```
feat: add lax mode for trailing commas
fix: handle CRLF inside block scalars
docs: clarify plugin ordering
```

Use `feat!:` / `fix!:` (or a `BREAKING CHANGE:` footer) for breaking changes.

## Pull requests

1. Open an issue first for anything larger than a small fix.
2. Branch from `main`; keep the PR focused on one change.
3. `make test` must pass for **both** implementations.
4. PR titles follow Conventional Commits — PRs are squash-merged, so the
   title becomes the commit message.
5. CI must be green before merge.

## Security issues

Never open a public issue for a vulnerability — see [SECURITY.md](SECURITY.md).

## Code of conduct

Participation is covered by the org
[Code of Conduct](https://github.com/tabnas/.github/blob/main/CODE_OF_CONDUCT.md).
