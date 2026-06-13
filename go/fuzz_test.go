package tabnas

// Fuzz the no-panic guarantee: public Parse must never panic, even on
// arbitrary malformed input. The engine converts internal panics to
// "internal" TabnasErrors, so any panic escaping here is a real bug.
// Run with: go test -run x -fuzz FuzzParse .

import "testing"

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		`{"a":[1,2,{"b":"x"}]}`, `"é😀"`, "\"\\u{1F600}\"",
		"\"\\ud83d\\ude00\"", "a:\xff\xfe", "\xed\xa0\x80",
		"[1,2,]", "{", "\"unterminated", "{\"a\":{\"b\":{\"c\":",
		"\\", "\"\\u{", "0x", "-", "  ", "",
	} {
		f.Add(seed)
	}
	j := makeJSON()
	f.Fuzz(func(t *testing.T, src string) {
		_, _ = j.Parse(src)
	})
}
