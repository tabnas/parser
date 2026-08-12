# libtabnas — the C ABI

The engine as a C shared library, so languages with no tabnas port can
still use it. Python via `ctypes` is the motivating case (see
[`../../py/`](../../py/)), but the surface is plain C and works from
anything with an FFI.

```sh
./build.sh            # host
ZIG=/path/to/zig ./build.sh all
```

## What it does and does not do

It is **grammar-agnostic**, because this repo ships no grammar. A caller
supplies a *serialized GrammarSpec* — the pure-data form a front-end
compiler emits — and the library parses input against it:

```
GBNF text ──@tabnas/gbnf──▶ GrammarSpec ──serialize──▶ JSON
                                                        │
                                          libtabnas ◀───┘  parse input
```

Compile the grammar wherever a front-end lives; run it anywhere. There
is deliberately **no text-form loader**: `GrammarText` needs a registered
text parser and a grammar-free engine ships none, so a text entry point
would be dead here.

Scope is validation — does this input parse against this grammar. The
AST does not cross the boundary.

## The contract

| Function | Returns |
|---|---|
| `tabnas_version()` | `{"ok":true,"version":"…"}` |
| `tabnas_grammar_json(spec, len)` | `{"ok":true,"handle":N}` |
| `tabnas_parse(handle, src, len)` | `{"ok":true,"accept":true}` or `{"ok":true,"accept":false,"error":{…}}` |
| `tabnas_grammar_free(handle)` | — |
| `tabnas_free(str)` | — |

Four rules, each load-bearing:

1. **Every call returns JSON.** A C ABI has one return value and no
   exceptions. Rather than out-params or a thread-local error slot, each
   entry point returns a document, so a binding in any language is
   *call, decode* and the error contract is identical everywhere.
2. **A rejection is an answer, not a failure.** Input outside the
   grammar's language returns `ok:true, accept:false`. `ok:false` is
   reserved for the call itself being wrong — an unknown handle, an
   unparseable spec — so a caller can tell "your input is not in the
   language" from "you called me wrong" without reading messages.
3. **Lengths are explicit.** Grammar and source arguments take a byte
   length and are *not* read as NUL-terminated C strings. Parser input is
   arbitrary bytes and may legitimately contain a zero byte; truncating
   there would answer a question the caller did not ask.

   A **NULL pointer is the empty buffer when — and only when — the
   length is zero**, which is how C conveys one:

   | call | result |
   |---|---|
   | `(NULL, 0)` | the empty buffer; a normal verdict follows |
   | `(NULL, n)`, `n > 0` | `ok:false`, code `usage` |
   | `(ptr, n)`, `n < 0` | `ok:false`, code `usage` |

   `(NULL, 0)` is a question, not a mistake: a grammar may accept empty
   input, and `lex.empty` decides what it returns when one does. A
   pointer and a length that disagree cannot be honoured, so they are
   refused rather than guessed at.
4. **The caller owns what it is given.** Every `char*` returned must be
   released with `tabnas_free` (it is `malloc`'d, so that is `free(3)` —
   do not use another allocator's). Every handle must be released with
   `tabnas_grammar_free`.

Handles are safe to use from several threads: each carries a mutex,
because a `*Tabnas` is not safe for concurrent `Parse` and an FFI caller
is under no obligation to serialise — CPython, for one, releases the GIL
for the duration of a `ctypes` call.

## Cross-compiling

cgo needs a C toolchain per target, which normally forces a matrix of
native CI runners. `zig cc` is a cross compiler for all of them, so one
Linux box produces Linux and Windows artifacts:

| target | how |
|---|---|
| `linux/amd64`, `linux/arm64` | zig, cross |
| `windows/amd64` | zig, cross |
| `darwin/*` | **native macOS host only** |

macOS is the exception: linking needs Apple's SDK (`CoreFoundation`,
`libresolv`), which zig cannot redistribute. `build.sh all` skips darwin
unless it is already running on it.

## Layout

- `core.go` — the behaviour, in plain Go.
- `tabnas_c.go` — the cgo shim: `(pointer, length)` in, `malloc`'d
  string out, nothing else.
- `core_test.go` — the contract.

The split is not decoration. Go does not support cgo in `_test.go`
files, so anything beside `import "C"` is unreachable from a test;
keeping the behaviour in `core.go` is what makes it testable.
