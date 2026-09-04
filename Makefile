# Build, test and publish both the TypeScript (ts/) and Go (go/)
# implementations. ts/ is canonical; go/ tracks it.
#
# Local build/test resolve the unpublished @tabnas siblings via the
# repo-set go.work + node_modules symlinks (admin/scripts/link.sh).

.PHONY: all build test clean build-ts build-go build-rs test-ts test-go test-rs \
        clean-ts clean-go clean-rs publish-ts publish-go tags-go reset

all: build test

build: build-ts build-go build-rs

test: test-ts test-go test-rs

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
