package jsonic

// UTF-8 handling tests: multi-byte characters (2/3/4-byte sequences) in
// keys, values, strings, comments, and escapes; configured non-ASCII
// matcher chars; and the no-panic guarantee on arbitrary byte input.

import (
	"strings"
	"testing"

	tabnas "github.com/tabnas/parser/go"
)

func TestUTF8SharedFixtures(t *testing.T) {
	runParserTSV(t, "alignment-utf8.tsv", Make())
}

func TestUTF8IncludeJSON(t *testing.T) {
	j := Make(tabnas.Options{Rule: &tabnas.RuleOptions{Include: "json"}})
	runParserTSV(t, "include-json-utf8.tsv", j)
}

func TestUTF8IncludeJSONErrors(t *testing.T) {
	j := Make(tabnas.Options{Rule: &tabnas.RuleOptions{Include: "json"}})
	runErrorTSV(t, "include-json-utf8-errors.tsv", j)
}

// --- Surrogate pairs (the JSON encoding of astral characters) ---

func TestUTF8SurrogatePairs(t *testing.T) {
	out, err := Parse(`"\ud83d\ude00"`)
	if err != nil || out != "😀" {
		t.Errorf(`surrogate pair: got %q, %v`, out, err)
	}
	// Case-insensitive hex.
	out, err = Parse(`"\uD83D\uDE00"`)
	if err != nil || out != "😀" {
		t.Errorf(`upper-case surrogate pair: got %q, %v`, out, err)
	}
	// A lone surrogate becomes U+FFFD (matching encoding/json).
	out, err = Parse(`"\ud83d"`)
	if err != nil || out != "�" {
		t.Errorf(`lone high surrogate: got %q, %v`, out, err)
	}
	out, err = Parse(`"\ude00x"`)
	if err != nil || out != "�x" {
		t.Errorf(`lone low surrogate: got %q, %v`, out, err)
	}
	// High surrogate not followed by a low one stays lone (U+FFFD).
	out, err = Parse(`"\ud83dA"`)
	if err != nil || out != "�A" {
		t.Errorf(`unpaired high + BMP escape: got %q, %v`, out, err)
	}
}

// --- Braced escapes: variable length, range-checked ---

func TestUTF8BracedEscapes(t *testing.T) {
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`"\u{41}"`, "A"},
		{`"\u{e9}"`, "é"},
		{`"\u{4E2D}"`, "中"},
		{`"\u{1F600}"`, "😀"},
		{`"\u{10FFFF}"`, "\U0010FFFF"},
		{`"\u{41}B"`, "AB"},
	} {
		out, err := Parse(tt.src)
		if err != nil || out != tt.want {
			t.Errorf("%s: got %q, %v", tt.src, out, err)
		}
	}
	for _, src := range []string{
		`"\u{110000}"`, `"\u{}"`, `"\u{GG}"`, `"\u{1234567}"`, `"\u{41"`,
	} {
		_, err := Parse(src)
		je, ok := err.(*tabnas.TabnasError)
		if !ok || je.Code != "invalid_unicode" {
			t.Errorf("%s: expected invalid_unicode, got %v", src, err)
		}
	}
}

// --- Configured non-ASCII matcher chars (TS supports any BMP char via
// its fallback classifier; Go matches any rune) ---

func TestUTF8ConfiguredSpaceChars(t *testing.T) {
	// NBSP (U+00A0, 2-byte) and ideographic space (U+3000, 3-byte).
	j := Make(tabnas.Options{Space: &tabnas.SpaceOptions{Chars: " \t 　"}})
	out, err := j.Parse("a: 1　b: 2")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) || m["b"] != float64(2) {
		t.Errorf("non-ASCII space chars: got %#v", out)
	}
}

func TestUTF8ConfiguredLineChars(t *testing.T) {
	// U+2028 LINE SEPARATOR (3-byte) as a line + row char.
	j := Make(tabnas.Options{Line: &tabnas.LineOptions{
		Chars: "\r\n ", RowChars: "\n ",
	}})
	out, err := j.Parse("a:1 b:2")
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["a"] != float64(1) || m["b"] != float64(2) {
		t.Errorf("U+2028 line char: got %#v", out)
	}
	// Row counting: an error after two U+2028 separators reports row 3.
	_, err = j.Parse("a:1 b:2 c:\"x")
	je, ok := err.(*tabnas.TabnasError)
	if !ok || je.Row != 3 {
		t.Errorf("U+2028 row counting: got %+v", err)
	}
}

func TestUTF8ConfiguredQuoteChars(t *testing.T) {
	// Curly double quotes (U+201C/U+201D, 3-byte) as string delimiters.
	j := Make(tabnas.Options{String: &tabnas.StringOptions{Chars: "\"'`“"}})
	out, err := j.Parse("“hello world“")
	if err != nil || out != "hello world" {
		t.Errorf("curly quote string: got %q, %v", out, err)
	}
	// Unterminated curly-quoted string errors cleanly.
	_, err = j.Parse("“unterminated")
	je, ok := err.(*tabnas.TabnasError)
	if !ok || je.Code != "unterminated_string" {
		t.Errorf("unterminated curly quote: got %v", err)
	}
}

func TestUTF8MultilineString(t *testing.T) {
	out, err := Parse("`line1\nличность 😀`")
	if err != nil || out != "line1\nличность 😀" {
		t.Errorf("multiline multibyte: got %q, %v", out, err)
	}
}

// --- Column positions count runes (one column per character) ---

func TestUTF8ColumnsAreRunes(t *testing.T) {
	// "é中😀" is 9 bytes but 3 runes. With rune-counted columns the
	// unterminated quote sits at column 5: é(1) 中(2) 😀(3) :(4) "(5).
	// Byte-counted columns would report 11.
	_, err := Make().Parse("é中😀:\"x")
	je, ok := err.(*tabnas.TabnasError)
	if !ok {
		t.Fatalf("expected TabnasError, got %v", err)
	}
	if je.Code != "unterminated_string" || je.Col != 5 {
		t.Errorf("expected unterminated_string at rune column 5, got %s col %d", je.Code, je.Col)
	}
}

// --- Invalid UTF-8 bytes: never panic, never corrupt adjacent input ---

func TestUTF8InvalidBytes(t *testing.T) {
	for _, src := range []string{
		"a:\xff\xfe1", "\"\xc3(\"", "\xf0\x9f", "a:\x80", "\xed\xa0\x80",
		"\"trunc\xe2\x82", "k\xff:1",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse(%q) panicked: %v", src, r)
				}
			}()
			_, _ = Parse(src)
		}()
	}
}

// --- Fuzz: parsing arbitrary bytes never panics ---

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"a:1", `{"a":[1,2,{"b":"x"}]}`, "é:😀", "\"\\u{1F600}\"",
		"\"\\ud83d\\ude00\"", "a:\xff\xfe", "\xed\xa0\x80", "`ab\ncd`",
		"# comment\n[1,2,]", "/*", "\"unterminated", "{a:{b:{c:",
		"\\", "\"\\u{", "0x", "-", "  ",
	} {
		f.Add(seed)
	}
	j := Make()
	f.Fuzz(func(t *testing.T, src string) {
		// Errors are fine; panics are not (the engine converts internal
		// panics to "internal" TabnasErrors, so any panic escaping here
		// is a real bug).
		_, _ = j.Parse(src)
	})
}

// --- The recover guard converts plugin/matcher panics to errors ---

func TestPanicConvertedToInternalError(t *testing.T) {
	j := Make()
	boom := func(lex *tabnas.Lex, rule *tabnas.Rule) *tabnas.Token {
		if strings.HasPrefix(lex.Fwd(2), "@@") {
			panic("matcher exploded")
		}
		return nil
	}
	j.SetOptions(tabnas.Options{Match: &tabnas.MatchOptions{
		Value: map[string]*tabnas.MatchValueSpec{"boom": {Fn: boom}},
	}})

	_, err := j.Parse("a:@@")
	je, ok := err.(*tabnas.TabnasError)
	if !ok || je.Code != "internal" {
		t.Fatalf("expected internal TabnasError, got %v", err)
	}
	if !strings.Contains(je.Detail, "matcher exploded") {
		t.Errorf("internal error should carry panic message, got %q", je.Detail)
	}
}
