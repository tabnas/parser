#!/bin/sh
# Build libtabnas, the C-ABI shared library, for one or more targets.
#
#   ./build.sh                 # host only, into ./dist
#   ./build.sh all             # every target this host can reach
#   ./build.sh linux/arm64 …   # named targets
#
# CROSS-COMPILING USES ZIG. The library needs cgo, and cgo needs a C
# toolchain per target — which is what normally forces a CI matrix of
# native runners. `zig cc` is a cross compiler for all of them, so one
# Linux box can produce Linux and Windows artifacts. Point ZIG at the
# binary, or have it on PATH:
#
#   curl -L https://ziglang.org/download/<ver>/zig-<host>-<ver>.tar.xz | tar xJ
#   ZIG=./zig-<host>-<ver>/zig ./build.sh all
#
# macOS is the exception and cannot be cross-compiled this way: linking
# needs Apple's SDK (CoreFoundation, libresolv), which zig cannot
# redistribute. Build darwin artifacts on a macOS host, where the plain
# system toolchain works and ZIG is not needed.
set -eu

ZIG="${ZIG:-zig}"
OUT="${OUT:-dist}"
PKG="."

host_os=$(go env GOOS)
host_arch=$(go env GOARCH)

targets=""
case "${1:-host}" in
  host) targets="$host_os/$host_arch" ;;
  all)  targets="linux/amd64 linux/arm64 windows/amd64"
        # Only offer darwin when we are ON darwin; see the note above.
        [ "$host_os" = "darwin" ] && targets="$targets darwin/amd64 darwin/arm64" ;;
  *)    targets="$*" ;;
esac

# zig's target triple and the shared-library extension for a Go target.
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
  out="$OUT/libtabnas-$os-$arch$ext"

  if [ "$os" = "$host_os" ] && [ "$arch" = "$host_arch" ]; then
    # Native: the system toolchain is already correct, and on macOS it is
    # the only one that can link.
    CGO_ENABLED=1 GOOS="$os" GOARCH="$arch" \
      go build -buildmode=c-shared -o "$out" "$PKG"
  else
    if [ "$os" = "darwin" ]; then
      echo "skip $t: darwin cannot be cross-compiled (needs Apple's SDK); build on a macOS host" >&2
      continue
    fi
    zt=$(zig_target "$os" "$arch")
    if [ -z "$zt" ]; then
      echo "skip $t: no zig target mapping" >&2
      continue
    fi
    if ! command -v "$ZIG" >/dev/null 2>&1 && [ ! -x "$ZIG" ]; then
      echo "skip $t: zig not found (set ZIG=/path/to/zig)" >&2
      continue
    fi
    # go passes the compiler as one word, so the -target flag needs a
    # wrapper rather than being appended to CC.
    cc="$OUT/.zigcc-$os-$arch"
    printf '#!/bin/sh\nexec %s cc -target %s "$@"\n' "$ZIG" "$zt" > "$cc"
    chmod +x "$cc"
    CGO_ENABLED=1 GOOS="$os" GOARCH="$arch" CC="$(cd "$(dirname "$cc")" && pwd)/$(basename "$cc")" \
      go build -buildmode=c-shared -o "$out" "$PKG"
    rm -f "$cc"
  fi

  echo "built $out"
done
