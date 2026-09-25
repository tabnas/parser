# Build, test and publish the TypeScript (ts/), Go (go/) and Rust (rs/)
# implementations. ts/ is canonical; go/ and rs/ track it.
#
# These targets assume nothing about the machine beyond the toolchain each
# one invokes: a plain checkout with no sibling @tabnas repos linked in
# builds and tests fine. Pointing this checkout at unpublished siblings is
# local wiring you add deliberately and must not commit -- see AGENTS.md,
# "Never commit the local wiring".

.PHONY: all build test clean build-ts build-go build-rs test-ts test-go test-rs \
        clean-ts clean-go clean-rs deps deps-test publish-ts publish-go tags-go \
        reset prose prose-counts

all: build test

build: build-ts build-go build-rs

test: deps deps-test test-ts test-go test-rs

clean: clean-ts clean-go clean-rs

# --- TypeScript (package in ts/) ---
build-ts:
	cd ts && npm run build

test-ts:
	cd ts && npm test

clean-ts:
	rm -rf ts/dist ts/dist-test

# Publish the TypeScript package at its current package.json version.
publish-ts: test-ts
	cd ts && npm publish --access public

# --- Go (module in go/) ---
build-go:
	cd go && go build ./...

test-go:
	cd go && go test -v ./...

clean-go:
	cd go && go clean

# --- Rust (crate in rs/) ---
build-rs:
	cd rs && cargo build --all-targets

test-rs:
	cd rs && cargo test --all-targets
	cd rs && cargo clippy --all-targets --all-features -- -D warnings

clean-rs:
	cd rs && cargo clean

# --- Dependency sources (repo-wide) ---

# A committed dependency names a published package or a GitHub reference.
# Local wiring -- file:, a sibling path, a go replace leaving the tree, a
# packed archive -- is how a change is tested before its dependency is
# released, and it is correct right up to the commit; this is what stops it
# arriving in one. It judges what git TRACKS, so the wiring stays legal
# until it is staged, and it needs nothing but node. The same check runs
# inside `make test-ts` as ts/test/deps.test.js, so `npm test` carries it
# too.
deps:
	node tools/dep-gate.cjs

# The gate's own suite: every rule is driven by a case that breaks exactly
# one thing and requires that rule to go red.
deps-test:
	node --test tools/dep-gate.test.cjs

# Publish the Go module: make publish-go V=x.y.z
# Injects V into the Go `Version` const, commits, tags go/vX.Y.Z, and
# (when gh is available) creates a GitHub release.
publish-go: test-go
	@test -n "$(V)" || (echo "Usage: make publish-go V=x.y.z" && exit 1)
	@grep -q '^const VERSION = ' go/tabnas.go || \
	  (echo "publish-go: no 'const VERSION = ' in go/tabnas.go — refusing to tag a release with an unbumped constant" && exit 1)
	sed -i.bak 's/^const VERSION = ".*"/const VERSION = "$(V)"/' go/tabnas.go
	rm -f go/tabnas.go.bak
	git add go/tabnas.go
	git commit -m "go: v$(V)"
	git tag go/v$(V)
	git push origin main go/v$(V)
	@command -v gh >/dev/null 2>&1 && gh release create go/v$(V) --title "go/v$(V)" --notes "Go module release v$(V)" || true

# List published Go module tags, newest first.
tags-go:
	git tag -l 'go/v*' --sort=-version:refname

reset:
	cd ts && npm run reset
	cd go && go clean -cache && go build ./... && go test -v ./...
	cd rs && cargo clean && cargo build --all-targets && cargo test --all-targets

# The prose gate (see doc/STYLE-GUIDE.md). Vale over the reader-facing
# pages, at the levels set in .vale.ini, on the same file list
# ts/test/docs.test.js reads. Warnings are advisory, errors fail.
#
# Needs `vale` on PATH and one `vale sync` to fetch the pinned Google
# package -- neither is present on every machine, and where vale is
# missing this target fails rather than reporting a pass it did not earn.
# The fast half of the same gate is ts/test/docs.test.js, which runs in
# `make test-ts` and needs nothing but node. The workflow form of the
# Vale half is .github/workflows/docs.yml, which CI runs.
prose:
	vale --minAlertLevel=error $$(node ts/scripts/gated-docs.cjs)
	node ts/scripts/vale-counts.cjs

# Re-measure the counts .vale.ini and doc/STYLE-GUIDE.md record, after
# a change to the pages or the rules moves them.
prose-counts:
	node ts/scripts/vale-counts.cjs --write
