#!/bin/sh
# Build libtabnasparser, the C-ABI shared library, for one or more targets.
#
# tabnas-clib-template: v3 (stamped by admin tasks/adopt-clib.sh;
# edit the template and re-stamp, not this file).
#
#   ./build.sh                 # host only, into ./dist
#   ./build.sh all             # every target this host can reach
#   ./build.sh linux/arm64 …   # named targets
#
# CROSS-COMPILING USES ZIG. The library needs cgo, and cgo needs a C
# toolchain per target. `zig cc` is a cross compiler for all of them, so
# one Linux box can produce Linux and Windows artifacts:
#
#   ZIG=/path/to/zig ./build.sh all
#
# macOS is the exception and cannot be cross-compiled this way: linking
# needs Apple's SDK, which zig cannot redistribute. Build darwin
# artifacts on a macOS host; releases do not block on them (ADR-12
# clause 5 — the darwin lane is best-effort and additive).
set -eu

ZIG="${ZIG:-zig}"
OUT="${OUT:-dist}"
PKG="."
LIB="libtabnasparser"

# GOHOSTOS/GOHOSTARCH, not GOOS/GOARCH: the latter name the TARGET when
# a caller exports them for cross-compilation, and mistaking a target
# for the physical host selects the wrong toolchain branch.
host_os=$(go env GOHOSTOS)
host_arch=$(go env GOHOSTARCH)

# A target NAMED on the command line is a requirement, not a wish: if it
# cannot be built the script fails, so release automation cannot mistake
# an incomplete artifact set for a successful build. `all` stays
# best-effort, because its whole point is "whatever this host can reach".
targets=""
explicit=0
case "${1:-host}" in
  host) targets="$host_os/$host_arch" ;;
  all)  targets="linux/amd64 linux/arm64 windows/amd64 windows/arm64"
        [ "$host_os" = "darwin" ] && targets="$targets darwin/amd64 darwin/arm64" ;;
  *)    targets="$*"; explicit=1 ;;
esac

skip_or_fail() {
  echo "$1" >&2
  [ "$explicit" = "1" ] && exit 1
  return 0
}

zig_target() {
  case "$1/$2" in
    linux/amd64)   echo "x86_64-linux-gnu" ;;
    linux/arm64)   echo "aarch64-linux-gnu" ;;
    windows/amd64) echo "x86_64-windows-gnu" ;;
    windows/arm64) echo "aarch64-windows-gnu" ;;
    *)             echo "" ;;
  esac
}

lib_ext() {
  case "$1" in
    windows) echo ".dll" ;;
    darwin)  echo ".dylib" ;;
    *)       echo ".so" ;;
  esac
}

mkdir -p "$OUT"

for t in $targets; do
  os=${t%%/*}
  arch=${t##*/}
  ext=$(lib_ext "$os")
  out="$OUT/$LIB-$os-$arch$ext"
  # A skipped target must leave no stale artifact from an earlier run —
  # packaging would otherwise publish an old binary as current.
  rm -f "$out"

  if [ "$os" = "$host_os" ] && [ "$arch" = "$host_arch" ]; then
    CGO_ENABLED=1 GOOS="$os" GOARCH="$arch" \
      go build -buildmode=c-shared -o "$out" "$PKG"
    # Canonical basename for the host: release artifacts carry the
    # target suffix, but the linker (and the pkg-config Libs line)
    # wants lib$LIB$ext — installers copy or link this name into libdir.
    ln -sf "$(basename "$out")" "$OUT/$LIB$ext"
  else
    if [ "$os" = "darwin" ]; then
      skip_or_fail "skip $t: darwin cannot be cross-compiled (needs Apple's SDK); build on a macOS host"
      continue
    fi
    zt=$(zig_target "$os" "$arch")
    if [ -z "$zt" ]; then
      skip_or_fail "skip $t: no zig target mapping"
      continue
    fi
    if ! command -v "$ZIG" >/dev/null 2>&1 && [ ! -x "$ZIG" ]; then
      skip_or_fail "skip $t: zig not found (set ZIG=/path/to/zig)"
      continue
    fi
    cc="$OUT/.zigcc-$os-$arch"
    printf '#!/bin/sh\nexec "%s" cc -target %s "$@"\n' "$ZIG" "$zt" > "$cc"
    chmod +x "$cc"
    CGO_ENABLED=1 GOOS="$os" GOARCH="$arch" CC="$(cd "$(dirname "$cc")" && pwd)/$(basename "$cc")" \
      go build -buildmode=c-shared -o "$out" "$PKG"
    rm -f "$cc"
  fi

  echo "built $out"
done
