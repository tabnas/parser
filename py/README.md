# tabnas for Python

The tabnas parsing engine, via its C ABI. This is a `ctypes` binding
over [`go/clib`](../go/clib/) — nothing here reimplements the engine, so
what Python accepts is exactly what every other tabnas runtime accepts.

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
v.error["message"]                # why
```

Supply a **serialized GrammarSpec** — the pure-data form a front-end
compiler emits (`@tabnas/gbnf` for llama.cpp GBNF, `@tabnas/abnf` for
RFC 5234 ABNF). Compile the grammar wherever a front-end lives, then run
it here.

A rejection is an answer, not an exception: `check()` returns a
`Verdict`. `TabnasError` is raised only when the *call* is wrong — a bad
spec, a closed grammar.

Set `TABNAS_LIB` to the shared library, or pass `path=`, if it is not
next to this file or in `go/clib/dist`.

**Processes.** The library carries a Go runtime, which does not survive
`os.fork()` intact. With `multiprocessing`, use the `spawn` or
`forkserver` start method rather than `fork`.

Not packaged as a wheel yet — build the library and put this module on
your path. The wheel matrix (manylinux/musllinux/macOS/Windows via
`cibuildwheel`) is the next step, and needs a macOS runner because
darwin cannot be cross-compiled with zig.
