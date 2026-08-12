"""tabnas — the parsing engine, via its C ABI.

The engine is written in Go and exposed as a C shared library
(``go/clib``); this module is a ctypes binding over it. Nothing here
reimplements the engine, so what Python accepts is exactly what every
other tabnas runtime accepts.

The engine ships no grammar, and neither does this module. Supply a
*serialized GrammarSpec* — the pure-data form a front-end compiler
emits (``@tabnas/gbnf`` for llama.cpp GBNF, ``@tabnas/abnf`` for RFC
5234 ABNF, and so on). Compile the grammar wherever a front-end lives,
then run it anywhere::

    import json, tabnas

    with open("json-grammar.json") as f:
        grammar = tabnas.Grammar(json.load(f))

    grammar.accepts('{"a": 1}')     # True
    grammar.accepts('{"a": 1,}')    # False

    verdict = grammar.check('{"a": 1,}')
    verdict.accept                  # False
    verdict.error["message"]        # why

Build the library first::

    cd go/clib && ./build.sh

Set ``TABNAS_LIB`` to point at it, or pass ``path=`` to ``load()``.

A NOTE ON PROCESSES. The library carries a Go runtime, and a Go runtime
does not survive ``os.fork()`` intact. If you use ``multiprocessing``,
choose the ``spawn`` or ``forkserver`` start method rather than
``fork``.
"""

from __future__ import annotations

import ctypes
import json
import os
import platform
from dataclasses import dataclass, field
from typing import Any, Optional

__all__ = ["Grammar", "Verdict", "TabnasError", "load", "version"]


class TabnasError(Exception):
    """The call itself was wrong — a bad spec, a released grammar.

    NOT raised when input fails to parse: that is an answer, not an
    error. See :class:`Verdict`.
    """


@dataclass(frozen=True)
class Verdict:
    """The result of checking one input against a grammar."""

    accept: bool
    error: Optional[dict] = field(default=None)

    def __bool__(self) -> bool:
        return self.accept


def _default_lib_path() -> str:
    if env := os.environ.get("TABNAS_LIB"):
        return env
    ext = {"Windows": ".dll", "Darwin": ".dylib"}.get(platform.system(), ".so")
    arch = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64",
            "arm64": "arm64"}.get(platform.machine(), platform.machine())
    goos = {"Windows": "windows", "Darwin": "darwin"}.get(
        platform.system(), "linux")
    here = os.path.dirname(os.path.abspath(__file__))
    for candidate in (
        os.path.join(here, f"libtabnas{ext}"),
        os.path.join(here, "..", "go", "clib", "dist",
                     f"libtabnas-{goos}-{arch}{ext}"),
    ):
        if os.path.exists(candidate):
            return candidate
    raise TabnasError(
        "cannot find the tabnas shared library. Build it with "
        "`cd go/clib && ./build.sh`, then set TABNAS_LIB to the result "
        "or pass path= to tabnas.load()."
    )


_lib = None


def load(path: Optional[str] = None):
    """Load the shared library. Called automatically on first use.

    An explicit ``path`` is remembered, so the documented
    ``tabnas.load(path=...)`` then ``tabnas.Grammar(spec)`` sequence
    works — caching only the auto-discovered library would send the
    second call back to discovery and raise for anyone whose library is
    not on the default search path.
    """
    global _lib
    if _lib is not None and path is None:
        return _lib

    lib = ctypes.CDLL(path or _default_lib_path())

    # Returned strings are ours to free, so they come back as void* —
    # ctypes would otherwise copy a c_char_p and lose the pointer we
    # have to hand to tabnas_free.
    lib.tabnas_version.restype = ctypes.c_void_p
    lib.tabnas_version.argtypes = []
    lib.tabnas_grammar_json.restype = ctypes.c_void_p
    lib.tabnas_grammar_json.argtypes = [ctypes.c_char_p, ctypes.c_int]
    lib.tabnas_parse.restype = ctypes.c_void_p
    lib.tabnas_parse.argtypes = [ctypes.c_longlong, ctypes.c_char_p,
                                 ctypes.c_int]
    lib.tabnas_grammar_free.restype = None
    lib.tabnas_grammar_free.argtypes = [ctypes.c_longlong]
    lib.tabnas_free.restype = None
    lib.tabnas_free.argtypes = [ctypes.c_void_p]

    _lib = lib
    return lib


def _call(lib, ptr) -> dict:
    """Decode a returned document and release it."""
    if not ptr:
        raise TabnasError("the library returned nothing")
    try:
        return json.loads(ctypes.string_at(ptr).decode("utf-8"))
    finally:
        lib.tabnas_free(ptr)


def version() -> str:
    """The engine version behind the loaded library."""
    lib = load()
    return _call(lib, lib.tabnas_version())["version"]


class Grammar:
    """A loaded grammar, ready to check inputs against.

    ``spec`` is a serialized GrammarSpec: a dict, or the JSON text of
    one. Use as a context manager, or call :meth:`close`, to release the
    underlying handle deterministically.
    """

    def __init__(self, spec: Any, *, path: Optional[str] = None):
        self._lib = load(path)
        if isinstance(spec, (dict, list)):
            spec = json.dumps(spec)
        if isinstance(spec, str):
            spec = spec.encode("utf-8")
        if not isinstance(spec, (bytes, bytearray)):
            raise TypeError("spec must be a dict, str or bytes")

        res = _call(self._lib, self._lib.tabnas_grammar_json(
            bytes(spec), len(spec)))
        if not res.get("ok"):
            raise TabnasError(res.get("error", {}).get("message", "load failed"))
        self._handle = int(res["handle"])

    def check(self, src: Any) -> Verdict:
        """Check one input. Returns a :class:`Verdict`; never raises for
        a rejection."""
        if self._handle is None:
            raise TabnasError("this grammar has been closed")
        if isinstance(src, str):
            src = src.encode("utf-8")
        if not isinstance(src, (bytes, bytearray)):
            raise TypeError("src must be str or bytes")
        src = bytes(src)

        # Length is passed explicitly: input is bytes and may contain a
        # zero byte, which a NUL-terminated read would silently truncate.
        res = _call(self._lib, self._lib.tabnas_parse(
            self._handle, src, len(src)))
        if not res.get("ok"):
            raise TabnasError(res.get("error", {}).get("message", "parse failed"))
        return Verdict(accept=bool(res.get("accept")), error=res.get("error"))

    def accepts(self, src: Any) -> bool:
        """True when src is in this grammar's language."""
        return self.check(src).accept

    def close(self) -> None:
        if getattr(self, "_handle", None) is not None:
            self._lib.tabnas_grammar_free(self._handle)
            self._handle = None

    def __enter__(self) -> "Grammar":
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    def __del__(self) -> None:
        try:
            self.close()
        except Exception:
            pass
