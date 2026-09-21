/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// The engine's own config validators report a caller's mistake as an
// error, not a panic, on every door that has an error channel (#119).
// Two sites panicked: a serialized regex RE2 cannot compile, and a
// matcher-owned token bound to a fixed literal. MapToOptions and
// SetOptions panicked out to the caller; Grammar() recovered the panic
// and reported a user error as "internal".

import (
	"strings"
	"testing"
)

func TestOptionsFromMapReportsAnUncompilableRegex(t *testing.T) {
	m := map[string]any{"match": map[string]any{"token": map[string]any{"#RX": "@/^(?=a)b/"}}}
	resolved := ResolveFuncRefs(m, nil).(map[string]any)

	_, err := OptionsFromMap(resolved)
	if err == nil || !strings.Contains(err.Error(), "did not compile") {
		t.Fatalf("expected a compile error, got %v", err)
	}

	// The unchecked form returns, rather than panicking out to its
	// caller.
	done := make(chan bool)
	go func() {
		defer func() { done <- recover() == nil }()
		MapToOptions(resolved)
	}()
	if !<-done {
		t.Error("MapToOptions panicked")
	}
}

func TestGrammarReportsAnUncompilableRegexAsAUserError(t *testing.T) {
	gs, err := GrammarSpecFromJSON([]byte(`{"options":{"rule":{"start":"top"},
		"match":{"token":{"#RX":"@/^(?=a)b/"}}},
		"rule":{"top":{"open":[{"s":["#RX"]}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	err = Make().Grammar(gs)
	if err == nil {
		t.Fatal("grammar loaded with an uncompilable terminal")
	}
	if strings.Contains(err.Error(), "internal") {
		t.Errorf("a caller's regex is reported as an internal error: %v", err)
	}
	if !strings.Contains(err.Error(), "#RX") {
		t.Errorf("the error does not name the token: %v", err)
	}
}

func TestGrammarReportsAMatcherTokenBindingAsAUserError(t *testing.T) {
	x := "x"
	err := Make().Grammar(&GrammarSpec{Options: &Options{
		Fixed: &FixedOptions{Token: map[string]*string{"#ST": &x}},
	}})
	if err == nil || !strings.Contains(err.Error(), "produced by a lexer matcher") {
		t.Fatalf("expected the fixed-token error, got %v", err)
	}
	if strings.Contains(err.Error(), "internal") {
		t.Errorf("a caller's binding is reported as an internal error: %v", err)
	}

	gs, perr := GrammarSpecFromJSON([]byte(`{"options":{"fixed":{"token":{"#ST":"x"}}}}`))
	if perr != nil {
		t.Fatal(perr)
	}
	err = Make().Grammar(gs)
	if err == nil || strings.Contains(err.Error(), "internal") {
		t.Errorf("serialized form: got %v", err)
	}
}

func TestApplyOptionsReturnsTheValidationError(t *testing.T) {
	x := "x"
	j := Make()
	err := j.ApplyOptions(Options{Fixed: &FixedOptions{Token: map[string]*string{"#NR": &x}}})
	if err == nil || !strings.Contains(err.Error(), "produced by a lexer matcher") {
		t.Fatalf("expected the fixed-token error, got %v", err)
	}
	// Nothing was applied.
	if _, bound := j.parser.Config.FixedTokens["x"]; bound {
		t.Error("the refused binding was applied anyway")
	}
}

func TestSetOptionsTextReportsUserErrors(t *testing.T) {
	withStubTextParser(t, func(string) (any, error) {
		x := "x"
		return map[string]any{"fixed": map[string]any{"token": map[string]any{"#ST": x}}}, nil
	})
	_, err := Make().SetOptionsText("fixed:{token:{'#ST':'x'}}")
	if err == nil || !strings.Contains(err.Error(), "produced by a lexer matcher") {
		t.Fatalf("expected the fixed-token error, got %v", err)
	}
	if te, ok := err.(*TabnasError); ok && te.Code == "internal" {
		t.Errorf("a caller's binding is reported as internal: %v", err)
	}
}
