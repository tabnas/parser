# tabnas for Python

The tabnas parsing engine, via its C ABI. This is a `ctypes` binding
over [`go/clib`](../go/clib/) — nothing here reimplements the engine, so
what Python accepts is exactly what every other tabnas runtime accepts.
The library is `libtabnasparser`, and it exports the uniform tabnas C ABI:
`tabnas_version`, `tabnas_grammar`, `tabnas_parse`, `tabnas_grammar_free`
and `tabnas_free`.

```sh
cd ../go/clib && ./build.sh        # build the library first
cd ../../py && python3 -m unittest -v
```

```python
import json, tabnas

with open("json-grammar.json") as f:
    grammar = tabnas.Grammar(json.load(f))

grammar.accepts('{"a": 1}')      # True
grammar.accepts('{"a": 1,}')     # False

v = grammar.check('{"a": 1,}')
v.accept                          # False
v.error["code"]                   # 'unexpected'
v.error["message"]                # why

grammar.check('{"a": 1}').value   # {'a': 1}: the parse result

tabnas.version()                  # 'v3': the C ABI template revision
```

Supply a **serialized GrammarSpec** — the pure-data form a front-end
compiler emits (`@tabnas/gbnf` for llama.cpp GBNF, `@tabnas/abnf` for
RFC 5234 ABNF). Compile the grammar wherever a front-end lives, then run
it here.

A rejection is an answer, not an exception: `check()` returns a
`Verdict`, whose `error` is the engine's structured diagnostic (`code`,
`message`, `row`, `col`, `token`, … as in
[`schema/diagnostic.schema.json`](../schema/diagnostic.schema.json)).
`TabnasError` is raised only when the *call* fails: a bad spec (including
one that installs no start rule), a closed grammar, an engine-internal
fault.

`tabnas.version()` returns the `template` member of the library's
version document, the revision of the C ABI it implements. It is not the
engine version, which the version document does not carry: that is the
`go/v…` release tag the library was built from, and every rejection
reports it as `error["version"]`.

Set `TABNAS_LIB` to the shared library, or pass `path=`, if it is not
next to this file (as `libtabnasparser.so`, `.dylib` or `.dll`) or in
`go/clib/dist` (as `libtabnasparser-<os>-<arch>.so` and so on).

**Processes.** The library carries a Go runtime, which does not survive
`os.fork()` intact. With `multiprocessing`, use the `spawn` or
`forkserver` start method rather than `fork`.

Not packaged as a wheel yet — build the library, or take a prebuilt
`libtabnasparser` from the GitHub Release on a `go/v…` tag (its
`manifest.json` lists the targets), and put this module on your path.
The wheel matrix (manylinux/musllinux/macOS/Windows via `cibuildwheel`)
is the next step, and needs a macOS runner because darwin cannot be
cross-compiled with zig.
