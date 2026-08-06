package tabnas

import (
	"strings"
	"testing"
)

// Matcher-owned tokens cannot be bound to a fixed literal: doing so adds a
// second producer for the same tin instead of replacing the matcher, and
// the value silently vanishes. Mirrors the TS coverage test.
func TestMatcherTokensCannotBeBoundToLiteral(t *testing.T) {
	names := []string{
		"#BD", "#ZZ", "#UK", "#AA", "#SP", "#LN", "#CM",
		"#NR", "#ST", "#TX", "#VL",
	}
	for _, name := range names {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%s: expected panic, got none", name)
					return
				}
				if msg, ok := r.(string); !ok ||
					!strings.Contains(msg, "produced by a lexer matcher") {
					t.Errorf("%s: unexpected panic: %v", name, r)
				}
			}()
			src := "lit"
			Make(Options{Fixed: &FixedOptions{
				Token: map[string]*string{name: &src},
			}})
		}()
	}
}

// The fixed punctuation tokens are literals by definition, so rebinding
// them stays supported — @tabnas/csv relies on it for field.separation.
func TestFixedPunctuationTokensStayRebindable(t *testing.T) {
	for _, name := range []string{"#OB", "#CB", "#OS", "#CS", "#CL", "#CA"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: unexpected panic: %v", name, r)
				}
			}()
			src := ";"
			Make(Options{Fixed: &FixedOptions{
				Token: map[string]*string{name: &src},
			}})
		}()
	}
}

// A nil value removes a fixed binding rather than creating one, so it is
// allowed even for a matcher-owned name (where it is a no-op).
func TestNilFixedTokenValueIsAllowed(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic: %v", r)
		}
	}()
	Make(Options{Fixed: &FixedOptions{
		Token: map[string]*string{"#TX": nil, "#OB": nil},
	}})
}
