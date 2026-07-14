// Copyright (c) 2026 Richard Rodger and other contributors, MIT License

package tabnas

// UTF-8 multibyte CONTENT and configured-chars scenarios, porting the
// `multibyte-content` and `configured-chars` cases of ts/test/utf8.test.js
// (the escape-sequence cases are already covered by lexer_edge_test.go
// and strict_escape_test.go). Uses the strict-JSON grammar fixture from
// jsonplugin_test.go, mirroring the TS tests' use of the json plugin.

import (
	"reflect"
	"testing"
)

func TestUTF8MultibyteContent(t *testing.T) {
	j := makeJSON()

	// 2-byte (é), 3-byte (中, ☃), and 4-byte (😀, 🎈, 𝄞) UTF-8
	// characters as map keys, map values, list elements, and string body.
	cases := []struct {
		src  string
		want any
	}{
		{`{"é":"中"}`, map[string]any{"é": "中"}},
		{`{"😀":"🎈"}`, map[string]any{"😀": "🎈"}},
		{`["é","中","😀","𝄞","☃"]`, []any{"é", "中", "😀", "𝄞", "☃"}},
		{`"héllo wörld"`, "héllo wörld"},
	}
	for _, c := range cases {
		out, err := j.Parse(c.src)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.src, err)
			continue
		}
		if !reflect.DeepEqual(out, c.want) {
			t.Errorf("Parse(%q) = %#v, want %#v", c.src, out, c.want)
		}
	}
}

func TestUTF8ConfiguredSpaceChars(t *testing.T) {
	// NBSP (U+00A0) and ideographic space (U+3000) as space chars — the
	// scan-spec fallback classifier handles any rune outside ASCII.
	j := makeJSON(Options{Space: &SpaceOptions{Chars: " \t 　"}})
	src := "{\"a\": 1,　\"b\": 2}"
	out, err := j.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	want := map[string]any{"a": float64(1), "b": float64(2)}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("Parse(%q) = %#v, want %#v", src, out, want)
	}
}

func TestUTF8ConfiguredLineChars(t *testing.T) {
	// U+2028 LINE SEPARATOR as a line + row char: parses as line
	// whitespace, and error rows count it.
	j := makeJSON(Options{Line: &LineOptions{
		Chars:    "\r\n ",
		RowChars: "\n ",
	}})

	src := "{\"a\":1, \"b\":2}"
	out, err := j.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	want := map[string]any{"a": float64(1), "b": float64(2)}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("Parse(%q) = %#v, want %#v", src, out, want)
	}

	// Unterminated string on the third row (after two U+2028 chars).
	bad := "{\"a\":1, \"b\": \"x"
	_, err = j.Parse(bad)
	if err == nil {
		t.Fatal("unterminated string should error")
	}
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T: %v", err, err)
	}
	if je.Code != "unterminated_string" {
		t.Errorf("expected unterminated_string, got %s", je.Code)
	}
	if je.Row != 3 {
		t.Errorf("U+2028 should advance the row count: Row=%d, want 3", je.Row)
	}
}

func TestUTF8ConfiguredStringQuoteChar(t *testing.T) {
	// Curly double quote (U+201C, a 3-byte UTF-8 char) as a string
	// delimiter.
	j := makeJSON(Options{String: &StringOptions{Chars: "\"\u201c"}})
	src := "\u201chello world\u201c"
	out, err := j.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	if out != "hello world" {
		t.Errorf("Parse(%q) = %#v, want %q", src, out, "hello world")
	}
}
