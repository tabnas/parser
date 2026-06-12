package jsonic

import tabnas "github.com/tabnas/parser/go"

// Tests for the TS-aligned error message system: errmsg.name / suffix /
// link, template injection ({key} placeholders via StrInject), default
// hints, meta fileName, and the "--internal: ..." suffix block. These
// mirror the TS errdesc/errmsg behavior in ts/src/error.ts.

import (
	"strings"
	"testing"
)

// --- errmsg.link ---

func TestErrMsgLinkInSuffix(t *testing.T) {
	off := false
	j := Make(tabnas.Options{
		ErrMsg: &tabnas.ErrMsgOptions{Link: "https://example.org/docs/errors"},
		Color:  &tabnas.ColorOptions{Active: &off},
	})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "\n  https://example.org/docs/errors") {
		t.Errorf("expected link line in suffix, got:\n%s", msg)
	}
	// The link precedes the internal diagnostics line.
	if strings.Index(msg, "https://example.org") > strings.Index(msg, "--internal:") {
		t.Errorf("link should precede --internal line, got:\n%s", msg)
	}
}

func TestErrMsgLinkSuppressedWithSuffixFalse(t *testing.T) {
	j := Make(tabnas.Options{
		ErrMsg: &tabnas.ErrMsgOptions{Suffix: false, Link: "https://example.org/x"},
	})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "example.org") || strings.Contains(msg, "--internal:") {
		t.Errorf("suffix=false should suppress link and internal block, got:\n%s", msg)
	}
}

func TestErrMsgLinkTextForm(t *testing.T) {
	j := Make()
	_, err := setOptionsFromText(j, `errmsg: { link: 'https://example.org/text-form' }`)
	if err != nil {
		t.Fatal(err)
	}
	_, perr := j.Parse("}")
	if perr == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(perr.Error(), "https://example.org/text-form") {
		t.Errorf("text-form errmsg.link not applied, got:\n%s", perr.Error())
	}
}

// --- errmsg.suffix ---

func TestErrMsgSuffixDefaultInternal(t *testing.T) {
	// Default (suffix unset → true, matching TS) renders the internal
	// diagnostics line with rule and token context.
	_, err := Make().Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--internal: tag=") {
		t.Errorf("default suffix should include --internal line, got:\n%s", msg)
	}
	if !strings.Contains(msg, "rule=val~o") {
		t.Errorf("internal line should carry rule context, got:\n%s", msg)
	}
	if !strings.Contains(msg, "token=#CB") {
		t.Errorf("internal line should carry token context, got:\n%s", msg)
	}
}

func TestErrMsgSuffixString(t *testing.T) {
	j := Make(tabnas.Options{ErrMsg: &tabnas.ErrMsgOptions{Suffix: "see the manual"}})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "see the manual") {
		t.Errorf("string suffix should be appended, got:\n%s", msg)
	}
	if strings.Contains(msg, "--internal:") {
		t.Errorf("string suffix should replace internal block, got:\n%s", msg)
	}
}

func TestErrMsgSuffixFunc(t *testing.T) {
	j := Make(tabnas.Options{ErrMsg: &tabnas.ErrMsgOptions{
		Suffix: func(code, src string) string { return "code=" + code + " src=" + src },
	}})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "code=unexpected src=}") {
		t.Errorf("func suffix should be computed, got:\n%s", err.Error())
	}
}

func TestErrMsgSuffixInternalTag(t *testing.T) {
	// options.tag (not errmsg.name) feeds the internal line, per TS errdesc.
	j := Make(tabnas.Options{Tag: "mytag"})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--internal: tag=mytag;") {
		t.Errorf("internal line should show instance tag, got:\n%s", err.Error())
	}
}

func TestErrMsgSuffixPlugins(t *testing.T) {
	called := false
	plugin := func(j *tabnas.Tabnas, opts map[string]any) error {
		called = true
		return nil
	}
	j := Make()
	if err := j.Use(plugin); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("plugin not invoked")
	}
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "plugins=") {
		t.Errorf("internal line should carry plugins segment, got:\n%s", err.Error())
	}
}

// --- Template injection (TS errinject / strinject) ---

func TestErrorTemplateInjection(t *testing.T) {
	j := Make(tabnas.Options{
		Error: map[string]string{
			"unexpected": "boom at {row}:{col} near {src}",
		},
	})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	je := err.(*tabnas.TabnasError)
	if je.Detail != "boom at 1:1 near }" {
		t.Errorf("template injection failed, got %q", je.Detail)
	}
}

func TestDefaultErrorMessageInjection(t *testing.T) {
	_, err := Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	je := err.(*tabnas.TabnasError)
	// TS default: "unexpected character(s): {src}"
	if je.Detail != "unexpected character(s): }" {
		t.Errorf("default message injection failed, got %q", je.Detail)
	}
}

func TestHintTemplateInjection(t *testing.T) {
	j := Make(tabnas.Options{
		Hint: map[string]string{
			"unexpected": "Cannot use {src} here.",
		},
	})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	je := err.(*tabnas.TabnasError)
	if je.Hint != "Cannot use } here." {
		t.Errorf("hint injection failed, got %q", je.Hint)
	}
}

// --- Default hints (TS defaults.ts hint texts) ---

func TestDefaultHints(t *testing.T) {
	_, err := Parse(`"unclosed`)
	if err == nil {
		t.Fatal("expected error")
	}
	je := err.(*tabnas.TabnasError)
	if je.Hint != "This string has no end quote." {
		t.Errorf("default unterminated_string hint missing, got %q", je.Hint)
	}
	if !strings.Contains(je.Error(), "\n\n  This string has no end quote.") {
		t.Errorf("hint should render indented after a blank line, got:\n%s", je.Error())
	}
}

func TestHintOverrideMergesOverDefaults(t *testing.T) {
	j := Make(tabnas.Options{
		Hint: map[string]string{"unexpected": "custom"},
	})
	// Overridden code uses the custom hint...
	_, err := j.Parse("}")
	if je := err.(*tabnas.TabnasError); je.Hint != "custom" {
		t.Errorf("override failed, got %q", je.Hint)
	}
	// ...other codes keep their default hints.
	_, err = j.Parse(`"unclosed`)
	if je := err.(*tabnas.TabnasError); je.Hint != "This string has no end quote." {
		t.Errorf("default hint lost after override, got %q", je.Hint)
	}
}

// --- meta fileName (TS meta.fileName) ---

func TestErrorFileName(t *testing.T) {
	off := false
	j := Make(tabnas.Options{Color: &tabnas.ColorOptions{Active: &off}})
	_, err := j.ParseMeta("}", map[string]any{"fileName": "conf/app.tabnas"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--> conf/app.tabnas:1:1") {
		t.Errorf("fileName should appear in location line, got:\n%s", err.Error())
	}
}

func TestErrorNoFileName(t *testing.T) {
	off := false
	_, err := Make(tabnas.Options{Color: &tabnas.ColorOptions{Active: &off}}).Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--> <no-file>:1:1") {
		t.Errorf("unnamed source should show <no-file>, got:\n%s", err.Error())
	}
}

// --- errmsg.name still sets the header tag ---

func TestErrMsgNameHeader(t *testing.T) {
	j := Make(tabnas.Options{ErrMsg: &tabnas.ErrMsgOptions{Name: "myapp"}})
	_, err := j.Parse("}")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "[myapp/unexpected]:") {
		t.Errorf("errmsg.name should set the header tag, got:\n%s", err.Error())
	}
}

// --- Lexer-path errors carry the same configuration ---

func TestLexErrorCarriesErrMsgConfig(t *testing.T) {
	j := Make(tabnas.Options{
		ErrMsg: &tabnas.ErrMsgOptions{Name: "myapp", Link: "https://example.org/lex"},
	})
	_, err := j.Parse(`"unclosed`)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "[myapp/unterminated_string]:") {
		t.Errorf("lex error should carry errmsg.name, got:\n%s", msg)
	}
	if !strings.Contains(msg, "https://example.org/lex") {
		t.Errorf("lex error should carry errmsg.link, got:\n%s", msg)
	}
}
