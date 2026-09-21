/* Copyright (c) 2026 Richard Rodger, MIT License */

package tabnas

// The serialized-options surface is defined by TYPE, not by hand-list
// (#130): every leaf of Options that OptionsFromMap does not read falls
// silently out of any serialized GrammarSpec, and 65 of 92 leaves had.
// This gate walks Options by reflection, builds a map that sets EVERY
// leaf, runs it through OptionsFromMap, and fails on any leaf that did
// not arrive. A field added to Options without a branch here is a test
// failure, not a silent drop.
//
// Function-valued leaves are set with a function of the right type, the
// shape a resolved FuncRef takes, so they are covered by the same walk;
// a spec carrying only JSON cannot reach them, which is what makes them
// unportable rather than unread.

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// mapKeys is the serialized spelling of a Go field where lowerFirst
// does not give it: the TS option names these come from.
var mapKeys = map[string]string{
	"MaxMul":  "maxmul",
	"ErrMsg":  "errmsg",
	"EatLine": "eatline",
}

// notInMap lists the leaves the serialized surface does not carry, each
// with the reason. An entry here that OptionsFromMap DOES read fails,
// so an exclusion cannot outlive its justification.
var notInMap = map[string]string{
	// Derived from the @~/…/ ref form of match.token, never written
	// directly; TestMapToOptionsAllKeys covers the derivation.
	"Match.TokenEager": "derived from an eager @~/…/ match.token entry",
	// Split from match.token by value type: a LexMatcher there lands
	// here, which TestOptionsFromMapSplitsFunctionForms covers.
	"Match.TokenFn": "the function-form half of match.token",
	// Split from value.def[name].val by value type, covered by
	// TestMapToOptionsAllKeys.
	"Value.Def.ValFunc": "the function-form half of value.def[name].val",
	// The bare-function form of match.value[name], covered by
	// TestOptionsFromMapSplitsFunctionForms.
	"Match.Value.Fn": "read from match.value[name] when the entry is a function",
}

func optionKey(field reflect.StructField) string {
	if k, ok := mapKeys[field.Name]; ok {
		return k
	}
	return strings.ToLower(field.Name[:1]) + field.Name[1:]
}

// sample builds a non-zero value of type t, as a serialized map would
// carry it after ref resolution: JSON scalars for data, a callable for
// a function type, and a map with one entry for a map of structs.
func sample(t reflect.Type, path string) any {
	switch t.Kind() {
	case reflect.Ptr:
		if t.Elem().Kind() == reflect.Struct {
			return sampleMap(t.Elem(), path)
		}
		return sample(t.Elem(), path)
	case reflect.Bool:
		return true
	case reflect.Int:
		return float64(3)
	case reflect.String:
		if strings.HasSuffix(path, "Rule.Start") {
			return "top"
		}
		return "x"
	case reflect.Slice:
		return []any{sample(t.Elem(), path)}
	case reflect.Map:
		v := t.Elem()
		key := "k"
		if t.Key().Kind() == reflect.Int32 {
			key = "o" // string.replace keys a rune
		}
		if v == reflect.TypeOf((*regexp.Regexp)(nil)) {
			return map[string]any{key: regexp.MustCompile("^x")}
		}
		if v.Kind() == reflect.Ptr && v.Elem().Kind() == reflect.Struct {
			return map[string]any{key: sampleMap(v.Elem(), path)}
		}
		return map[string]any{key: sample(v, path)}
	case reflect.Func:
		return reflect.MakeFunc(t, func(args []reflect.Value) []reflect.Value {
			out := make([]reflect.Value, t.NumOut())
			for i := range out {
				out[i] = reflect.Zero(t.Out(i))
			}
			return out
		}).Interface()
	case reflect.Interface:
		return "any"
	case reflect.Struct:
		return sampleMap(t, path)
	}
	return nil
}

func sampleMap(t reflect.Type, path string) map[string]any {
	m := map[string]any{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		p := path + "." + f.Name
		if _, skip := notInMap[strings.TrimPrefix(p, "Options.")]; skip {
			continue
		}
		key := optionKey(f)
		// *regexp.Regexp leaves arrive already compiled from a @/…/ ref.
		if f.Type == reflect.TypeOf((*regexp.Regexp)(nil)) {
			m[key] = regexp.MustCompile("^x")
			continue
		}
		// value.def[name].val is data; its function form is ValFunc.
		if p == "Options.Value.Def.Val" {
			m[key] = "v"
			continue
		}
		m[key] = sample(f.Type, p)
	}
	return m
}

// unset walks a built Options value and reports every exported leaf
// still at its zero value. With skipExcluded the notInMap leaves are
// left out, which is the coverage walk; the honesty check below wants
// them reported.
func unset(v reflect.Value, path string, skipExcluded bool, out *[]string) {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			*out = append(*out, path)
			return
		}
		unset(v.Elem(), path, skipExcluded, out)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			p := path + "." + f.Name
			if _, skip := notInMap[strings.TrimPrefix(p, "Options.")]; skip && skipExcluded {
				continue
			}
			unset(v.Field(i), p, skipExcluded, out)
		}
	case reflect.Map:
		if v.Len() == 0 {
			*out = append(*out, path)
			return
		}
		for _, k := range v.MapKeys() {
			unset(v.MapIndex(k), path, skipExcluded, out)
		}
	case reflect.Slice:
		if v.Len() == 0 {
			*out = append(*out, path)
			return
		}
		unset(v.Index(0), path, skipExcluded, out)
	default:
		if v.IsZero() {
			*out = append(*out, path)
		}
	}
}

func TestOptionsFromMapCoversEveryLeaf(t *testing.T) {
	m := sampleMap(reflect.TypeOf(Options{}), "Options")
	opts, err := OptionsFromMap(m)
	if err != nil {
		t.Fatalf("OptionsFromMap: %v", err)
	}
	var missing []string
	unset(reflect.ValueOf(opts), "Options", true, &missing)
	if len(missing) > 0 {
		t.Errorf("OptionsFromMap does not read these Options leaves, so a "+
			"serialized spec setting them is silently ignored (add a branch, "+
			"or a notInMap entry with the reason):\n  %s",
			strings.Join(missing, "\n  "))
	}

	// The exclusions must stay honest: a leaf listed as not carried
	// that a spec CAN set fails here.
	for leaf := range notInMap {
		parts := strings.Split(leaf, ".")
		probe := map[string]any{}
		cur := probe
		for _, part := range parts[:len(parts)-1] {
			next := map[string]any{}
			cur[strings.ToLower(part[:1])+part[1:]] = next
			cur = next
		}
		cur[strings.ToLower(parts[len(parts)-1][:1])+parts[len(parts)-1][1:]] = true
		built, _ := OptionsFromMap(probe)
		var got []string
		unset(reflect.ValueOf(built), "Options", false, &got)
		stillUnset := false
		for _, g := range got {
			g = strings.TrimPrefix(g, "Options.")
			if g == leaf || strings.HasPrefix(leaf, g+".") {
				stillUnset = true
			}
		}
		if !stillUnset {
			t.Errorf("%s is excluded as %q but a map sets it: remove the exclusion",
				leaf, notInMap[leaf])
		}
	}
}

// The groups #130 measured as dropped, driven end to end: the same
// serialized spec sets them in TypeScript, and each is now observable
// on this side rather than silently defaulted.
func TestOptionsFromMapCarriesTheDroppedGroups(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{
		"rewind": map[string]any{"history": float64(7)},
		"result": map[string]any{"fail": []any{"x", nil}},
		"parse": map[string]any{
			"budget":  map[string]any{"checkEveryN": float64(5)},
			"recover": map[string]any{"enabled": true, "maxSkip": float64(9), "syncTokens": []any{"#CA"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Rewind == nil || opts.Rewind.History == nil || *opts.Rewind.History != 7 {
		t.Errorf("rewind.history dropped: %+v", opts.Rewind)
	}
	if opts.Result == nil || len(opts.Result.Fail) != 2 || opts.Result.Fail[0] != "x" {
		t.Errorf("result.fail dropped: %+v", opts.Result)
	}
	if opts.Parse == nil || opts.Parse.Budget == nil || opts.Parse.Budget.CheckEveryN != 5 {
		t.Errorf("parse.budget.checkEveryN dropped: %+v", opts.Parse)
	}
	r := opts.Parse.Recover
	if r == nil || !r.Enabled || r.MaxSkip == nil || *r.MaxSkip != 9 || len(r.SyncTokens) != 1 {
		t.Errorf("parse.recover dropped: %+v", r)
	}

	// The rewind spellings, as MapToOptions receives them from JSON.
	for _, c := range []struct {
		raw  any
		want int
	}{
		{nil, DefaultRewindHistory},
		{false, -1},
		{float64(0), 0},
		{float64(-4), 0},
		{float64(12), 12},
	} {
		o, err := OptionsFromMap(map[string]any{"rewind": map[string]any{"history": c.raw}})
		if err != nil || o.Rewind == nil || o.Rewind.History == nil || *o.Rewind.History != c.want {
			t.Errorf("rewind.history %v: got %+v (%v), want %d", c.raw, o.Rewind, err, c.want)
		}
	}
	if _, err := OptionsFromMap(map[string]any{"rewind": map[string]any{"history": true}}); err == nil {
		t.Error("rewind.history true accepted")
	}
	if _, err := OptionsFromMap(map[string]any{"rewind": map[string]any{"history": "many"}}); err == nil {
		t.Error("rewind.history string accepted")
	}
}

// The function-form halves that the type split hides from the walk.
func TestOptionsFromMapSplitsFunctionForms(t *testing.T) {
	matcher := LexMatcher(func(lex *Lex, rule *Rule) *Token { return nil })
	opts, err := OptionsFromMap(map[string]any{
		"match": map[string]any{
			"token": map[string]any{"#F": matcher},
			"value": map[string]any{"v": matcher, "w": map[string]any{"fn": matcher}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Match.TokenFn["#F"] == nil {
		t.Error("match.token function form not split into TokenFn")
	}
	if opts.Match.Value["v"] == nil || opts.Match.Value["v"].Fn == nil {
		t.Error("match.value bare function form not read")
	}
	if opts.Match.Value["w"] == nil || opts.Match.Value["w"].Fn == nil {
		t.Error("match.value {fn} form not read")
	}
}

// rewind.history set from a serialized spec is observable at parse time:
// this is the DoS bound AGENTS.md names, and it silently stayed at its
// default on this path (#130).
func TestRewindHistorySurvivesTheSerializedSpec(t *testing.T) {
	gs, err := GrammarSpecFromJSON([]byte(`{"clear":true,
		"options":{"rule":{"start":"top"},"rewind":{"history":0},
		           "fixed":{"token":{"#A":"a"}}},
		"rule":{"top":{"open":[{"s":["#A"],"a":"@count"}],"close":[{"s":["#ZZ"]}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	retained := -1
	gs.Ref = map[FuncRef]any{"@count": AltAction(func(r *Rule, ctx *Context) {
		retained = len(ctx.V)
	})}
	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("a"); err != nil {
		t.Fatal(err)
	}
	if retained != 0 {
		t.Errorf("rewind.history 0 from a spec retained %d tokens; the option was dropped", retained)
	}
}

// result.fail set from a serialized spec is observable too.
func TestResultFailSurvivesTheSerializedSpec(t *testing.T) {
	gs, err := GrammarSpecFromJSON([]byte(`{"clear":true,
		"options":{"rule":{"start":"top"},"result":{"fail":["x"]},
		           "fixed":{"token":{"#A":"a"}}},
		"rule":{"top":{"open":[{"s":["#A"],"a":"@x"}],"close":[{"s":["#ZZ"]}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	gs.Ref = map[FuncRef]any{"@x": AltAction(func(r *Rule, ctx *Context) { r.Node = "x" })}
	j := Make()
	if err := j.Grammar(gs); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Parse("a"); err == nil {
		t.Error("result.fail [\"x\"] from a spec did not fail the parse; the option was dropped")
	}
}
