"""The Python binding, checked against the engine's own fixtures.

Run after building the library:

    cd go/clib && ./build.sh
    cd ../../py && python3 -m unittest -v

These are conformance tests, not smoke tests. The `.tsv` fixtures under
test/spec are the SAME files the TypeScript and Go suites run, so a
disagreement here is a disagreement between runtimes rather than a
binding bug — which is the property that makes a third language
trustworthy at all.
"""

import os
import unittest

import tabnas

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
FIXTURE = os.path.join(ROOT, "ts", "test", "json-builder.fixture.json")
SPEC_DIR = os.path.join(ROOT, "test", "spec")

# The fixtures carry \n, \r and \t as two-character escapes. Go's reader
# (go/spec_test.go preprocessEscapes) expands exactly those three and
# leaves every other backslash alone; anything more eager here would feed
# the engine a different string than the other runtimes see, which is the
# one thing a conformance test must not do.
_ESCAPES = {"n": "\n", "r": "\r", "t": "\t"}


def _unescape(s):
    out, i = [], 0
    while i < len(s):
        if s[i] == "\\" and i + 1 < len(s) and s[i + 1] in _ESCAPES:
            out.append(_ESCAPES[s[i + 1]])
            i += 2
        else:
            out.append(s[i])
            i += 1
    return "".join(out)


def load_grammar():
    with open(FIXTURE, "rb") as f:
        return tabnas.Grammar(f.read())


class TestSurface(unittest.TestCase):
    def test_version(self):
        self.assertRegex(tabnas.version(), r"^\d+\.\d+\.\d+")

    def test_accept_and_reject(self):
        with load_grammar() as g:
            self.assertTrue(g.accepts('{"a":1}'))
            self.assertTrue(g.accepts('{"a":1,"b":[1,2]}'))
            self.assertFalse(g.accepts('{"a":1,}'))
            self.assertFalse(g.accepts("{oops"))

    def test_rejection_is_an_answer_not_an_exception(self):
        with load_grammar() as g:
            v = g.check("{oops")
            self.assertFalse(v.accept)
            self.assertFalse(v)  # Verdict is falsy when rejected
            self.assertIn("message", v.error)
            self.assertNotIn("\n", v.error["message"])

    def test_call_errors_do_raise(self):
        with self.assertRaises(tabnas.TabnasError):
            tabnas.Grammar("{not a spec")
        g = load_grammar()
        g.close()
        with self.assertRaises(tabnas.TabnasError):
            g.check('{"a":1}')

    def test_bytes_input_is_not_truncated_at_nul(self):
        # The bug an FFI binding gets for free if it passes C strings.
        with load_grammar() as g:
            self.assertFalse(g.accepts(b'{"a":1}\x00trailing'))

    def test_explicit_path_is_remembered(self):
        # The documented load(path=...) then Grammar(spec) sequence. If
        # only auto-discovered libraries were cached, the second call
        # would go back to discovery and raise for anyone whose library
        # is not on the default search path.
        lib = tabnas._default_lib_path()
        tabnas._lib = None
        try:
            tabnas.load(lib)
            with load_grammar() as g:
                self.assertTrue(g.accepts('{"a":1}'))
        finally:
            tabnas._lib = None

    def test_unicode_round_trips(self):
        with load_grammar() as g:
            self.assertTrue(g.accepts('{"a":"é日\U0001F600"}'))


class TestSharedFixtures(unittest.TestCase):
    """The engine's own strict-JSON fixtures, driven through Python.

    Each .tsv row is `input <TAB> expected`, where an expected value of
    ERROR:<code> means the input must be rejected. Only the accept /
    reject direction is asserted here — the value the engine builds is
    the other runtimes' business, and this binding does not expose it.
    """

    def _rows(self, name):
        path = os.path.join(SPEC_DIR, name)
        if not os.path.exists(path):
            self.skipTest(f"fixture {name} not present")
        with open(path, encoding="utf-8") as f:
            for n, line in enumerate(f, 1):
                if n == 1:
                    continue  # header row, as go/spec_test.go loadTSV skips it
                line = line.rstrip("\n")
                if not line or line.startswith("#") or "\t" not in line:
                    continue
                src, expected = line.split("\t", 1)
                yield n, _unescape(src), expected

    def test_include_json(self):
        checked = 0
        with load_grammar() as g:
            for n, src, expected in self._rows("include-json.tsv"):
                want_reject = expected.startswith("ERROR:")
                got = g.accepts(src)

                # BOTH directions, or this proves nothing: a binding that
                # rejected every input would satisfy the reject rows alone
                # and pass a test advertised as conformance.
                if want_reject and got:
                    self.fail(f"line {n}: {src!r} was accepted, expected {expected}")
                if not want_reject and not got:
                    self.fail(f"line {n}: {src!r} was rejected, expected accept")
                checked += 1
        self.assertGreater(checked, 0, "no fixture rows were checked")


if __name__ == "__main__":
    unittest.main()
