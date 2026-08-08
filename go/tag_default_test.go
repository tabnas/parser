/* Copyright (c) 2013-2026 Richard Rodger, MIT License */

package tabnas

import (
	"strings"
	"testing"
)

// The instance tag is user-visible in three places: the instance id, the
// "--internal: tag=..." suffix on every formatted error, and the debug
// plugin's `tag:` line (which prints Options().Tag verbatim). TS defaults
// it to "-" (ts/src/defaults.ts), so an unset Go tag has to resolve to
// the same string or every Go diagnostic reads differently from the TS
// one for the same grammar.

func TestTagDefaultsToDash(t *testing.T) {
	j := Make()
	if got := j.Options().Tag; got != DefaultTag {
		t.Errorf("unset tag: expected %q, got %q", DefaultTag, got)
	}
	if id := j.String(); !strings.HasSuffix(id, "/"+DefaultTag) {
		t.Errorf("instance id should carry the default tag, got %q", id)
	}
}

func TestTagExplicitIsKept(t *testing.T) {
	j := Make(Options{Tag: "mygrammar"})
	if got := j.Options().Tag; got != "mygrammar" {
		t.Errorf("explicit tag: expected %q, got %q", "mygrammar", got)
	}
	if id := j.String(); !strings.HasSuffix(id, "/mygrammar") {
		t.Errorf("instance id should carry the explicit tag, got %q", id)
	}
	// A later SetOptions that says nothing about the tag must not reset it.
	j.SetOptions(Options{})
	if got := j.Options().Tag; got != "mygrammar" {
		t.Errorf("tag after SetOptions{}: expected %q, got %q", "mygrammar", got)
	}
}

// The default tag reaches the error suffix, which is what makes this
// engine-level rather than a debug-plugin concern.
func TestTagDefaultAppearsInErrorSuffix(t *testing.T) {
	j := Make(Options{Rule: &RuleOptions{Start: "top", Exclude: "tabnas,imp"}})
	j.Rule("top", func(rs *RuleSpec, _ *Parser) {
		rs.AddOpen(&AltSpec{S: [][]Tin{{TinTX}}})
		rs.AddClose(&AltSpec{S: [][]Tin{{TinZZ}}})
	})
	_, err := j.Parse("{")
	if err == nil {
		t.Fatalf("expected a parse error")
	}
	if !strings.Contains(err.Error(), "--internal: tag=-;") {
		t.Errorf("error suffix should carry tag=-, got:\n%s", err.Error())
	}
}

// Merge identifies each side's grammar by its tag, so the default tag
// must be rejected exactly as an empty one is (TS merge.ts tagOf treats
// null, "" and "-" alike).
func TestMergeRejectsDefaultTag(t *testing.T) {
	_, err := Make().Merge(Make())
	if err == nil {
		t.Fatalf("merging two untagged instances should fail")
	}
	if !strings.Contains(err.Error(), "the first instance needs a Tag option") {
		t.Errorf("unexpected merge error: %v", err)
	}

	_, err = Make(Options{Tag: "a"}).Merge(Make())
	if err == nil || !strings.Contains(err.Error(),
		"the second instance needs a Tag option") {
		t.Errorf("second untagged instance should be rejected, got: %v", err)
	}

	// Two genuinely tagged instances still merge.
	if _, err = Make(Options{Tag: "a"}).Merge(Make(Options{Tag: "b"})); err != nil {
		t.Errorf("tagged merge should succeed, got: %v", err)
	}
}
