/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// test/spec/bad-token.tsv, the Go runner: what the parser does with a bad
// token the lexer hands it, fail-fast, under recovery and under relexing,
// and the error a recovering parse ends on. The TypeScript
// (ts/test/bad-token.test.js) and Rust (rs/tests/bad_token_spec_test.rs)
// runners assert the same rows; the fixture's header says what every column
// means, and what the custom matcher all three build must do.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// badTokenMatcher is the custom matcher the fixture header specifies: a
// bad token at `?`, one running to the end of the source at `%`, and one
// at `!` with only its Err set, none moving the cursor.
func badTokenMatcher(cfg *LexConfig, opts *Options) LexMatcher {
	return func(lex *Lex, rule *Rule) *Token {
		pnt := lex.Cursor()
		if pnt.SI >= len(lex.Src) {
			return nil
		}
		switch lex.Src[pnt.SI] {
		case '?':
			tkn := lex.Token("#BD", TinBD, nil, "?")
			tkn.Why = "custom_bad"
			return tkn
		case '%':
			tkn := lex.Token("#BD", TinBD, nil, lex.Src[pnt.SI:])
			tkn.Why = "custom_unterminated"
			return tkn
		case '!':
			tkn := lex.Token("#BD", TinBD, nil, "!")
			tkn.Err = "custom_err"
			return tkn
		}
		return nil
	}
}

func badTokenParser(t *testing.T, grammar string, grammars map[string]string, opts string) *Tabnas {
	t.Helper()
	j := Make(Options{Lex: &LexOptions{Match: map[string]*MatchSpec{
		"bad": {Order: 1500000, Make: badTokenMatcher},
	}}})
	install := func(gs *GrammarSpec) {
		t.Helper()
		if err := j.Grammar(gs); err != nil {
			t.Fatalf("install %s: %v", grammar, err)
		}
	}
	optionsOnly := func(text string) *GrammarSpec {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("options %s: %v", text, err)
		}
		return &GrammarSpec{OptionsMap: m}
	}
	if "json" == grammar {
		install(fixtureSpec(t, "json-builder.fixture.json"))
		install(optionsOnly(`{"rule":{"start":"val"}}`))
	} else {
		text, ok := grammars[grammar]
		if !ok {
			t.Fatalf("no @grammar named %s", grammar)
		}
		gs, err := GrammarSpecFromJSON([]byte(text))
		if err != nil {
			t.Fatalf("grammar %s: %v", grammar, err)
		}
		install(gs)
	}
	if "-" != opts && badTokenContinuations != opts {
		install(optionsOnly(opts))
	}
	return j
}

// badTokenContinuations is the `opts` cell that asks for Continuations(input)
// instead of a parse.
const badTokenContinuations = "continuations"

// badTokenCanon is the shared canonical value: sorted keys, integral
// numbers without a fraction, and an absent value as null.
func badTokenCanon(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, badTokenCanon(e))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case *OrderedMap:
		keys := append([]string(nil), x.Keys...)
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			kb, _ := json.Marshal(k)
			parts = append(parts, string(kb)+":"+badTokenCanon(x.Vals[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case map[string]any:
		om := &OrderedMap{Vals: x}
		for k := range x {
			om.Keys = append(om.Keys, k)
		}
		return badTokenCanon(om)
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		b, _ := json.Marshal(x)
		return string(b)
	case int64:
		return fmt.Sprintf("%d", x)
	case int:
		return fmt.Sprintf("%d", x)
	}
	if IsUndefined(v) {
		return "null"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func badTokenError(e *TabnasError) string {
	meta := ""
	if r := e.Recovered; r != nil {
		if r.Bad {
			meta = fmt.Sprintf("+bad%d", r.Skipped)
		} else {
			meta = fmt.Sprintf("+skip%d", r.Skipped)
		}
	}
	return fmt.Sprintf("%s@%d:%d%s", e.Code, e.Row, e.Col, meta)
}

func TestBadTokenSpec(t *testing.T) {
	rows, err := loadTSV(filepath.Join(specDir(), "bad-token.tsv"))
	if err != nil {
		t.Fatalf("load bad-token.tsv: %v", err)
	}
	at := regexp.MustCompile(`^#\s*@grammar\s+(\S+)\s+(.*)$`)
	grammars := map[string]string{}
	ran := 0
	for _, row := range rows {
		cols := row.cols
		if m := at.FindStringSubmatch(cols[0]); m != nil {
			grammars[m[1]] = m[2]
			continue
		}
		if strings.HasPrefix(cols[0], "#") || len(cols) < 5 {
			continue
		}
		for i := range cols {
			cols[i] = preprocessEscapes(cols[i])
		}
		grammar, opts, input, value, errors := cols[0], cols[1], cols[2], cols[3], cols[4]
		j := badTokenParser(t, grammar, grammars, opts)
		var gotValue, gotErrors string
		if badTokenContinuations == opts {
			_, names := j.Continuations(input)
			gotValue, gotErrors = strings.Join(names, ","), "-"
		} else if j.Config().Recover.Enabled {
			v, errs, perr := j.ParseRecover(input)
			if perr != nil {
				t.Errorf("row %d %q %s: recovery returned an error: %v", row.lineNo, input, opts, perr)
				continue
			}
			parts := make([]string, 0, len(errs))
			for _, e := range errs {
				parts = append(parts, badTokenError(e))
			}
			gotValue, gotErrors = badTokenCanon(v), strings.Join(parts, ",")
			if "" == gotErrors {
				gotErrors = "-"
			}
		} else {
			v, perr := j.Parse(input)
			if perr != nil {
				je, ok := perr.(*TabnasError)
				if !ok {
					t.Errorf("row %d %q: not a TabnasError: %v", row.lineNo, input, perr)
					continue
				}
				gotValue, gotErrors = "-", badTokenError(je)
			} else {
				gotValue, gotErrors = badTokenCanon(v), "-"
			}
		}
		if gotValue != value || gotErrors != errors {
			t.Errorf("row %d %q %s\n  value:  got %s, want %s\n  errors: got %s, want %s",
				row.lineNo, input, opts, gotValue, value, gotErrors, errors)
		}
		ran++
	}
	if ran <= 20 {
		t.Fatalf("bad-token.tsv ran only %d rows", ran)
	}
}

// Under relexing, a bad token no alternate re-cut is raised with the code
// its fetch would have given it: its Why, else `unexpected`, and never its
// Err (ts/src/rules.ts, `bad.why || UNEXPECTED`). Pinned here rather than
// in the shared fixture, which the Rust port runs too: that port reads err
// at this one site, so the row would not yet hold in all three runtimes.
func TestBadTokenErrOnlyUnderRelexFailFast(t *testing.T) {
	j := badTokenParser(t, "json", nil, `{"lex":{"relex":true}}`)
	_, err := j.Parse("[1,!]")
	je, ok := err.(*TabnasError)
	if !ok {
		t.Fatalf("expected *TabnasError, got %T (%v)", err, err)
	}
	if got := badTokenError(je); "unexpected@1:4" != got {
		t.Errorf("relex fail-fast: got %s, want unexpected@1:4", got)
	}
}
