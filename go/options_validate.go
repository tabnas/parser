// Copyright (c) 2026 Richard Rodger, MIT License

package tabnas

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// validateOptionsMap checks a serialized options map, with its FuncRefs
// already resolved, against the shape of Options by reflection, and
// reports every leaf whose value cannot be what the field holds.
//
// An ill-typed leaf used to be dropped in silence here, where the
// canonical runtime threw a raw TypeError from deep inside its
// configuration; the same spec loaded in one runtime and crashed in the
// other (#143). The ruling is that it is a load fault with the leaf
// named, in every runtime, and this is the Go half. A function in a slot
// that holds data is a FuncRef resolved where only data is declared and
// is the same fault, which is what restricts refs to declared code
// slots; a function of the wrong type in a code slot is reported as
// such rather than ignored. Keys that name no field are a plugin's own
// and are not checked.
//
// The value model is the serialized one: a JSON number is a float64, an
// array is []any, an object is map[string]any, and null (a Go nil) means
// "remove" or "default" at any leaf.
func validateOptionsMap(m map[string]any) error {
	var errs []string
	validateStruct(reflect.TypeOf(Options{}), m, "options", &errs)
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("tabnas: options: %s", strings.Join(errs, "; "))
	}
	return nil
}

// optionsKeyNames is the serialized spelling of a Go field where
// lowering the first letter does not give it (the TS option names).
var optionsKeyNames = map[string]string{
	"maxmul":  "MaxMul",
	"errmsg":  "ErrMsg",
	"eatline": "EatLine",
}

func optionsField(t reflect.Type, key string) (reflect.StructField, bool) {
	if name, ok := optionsKeyNames[key]; ok {
		return t.FieldByName(name)
	}
	if key == "" {
		return reflect.StructField{}, false
	}
	return t.FieldByName(strings.ToUpper(key[:1]) + key[1:])
}

func validateStruct(t reflect.Type, m map[string]any, path string, errs *[]string) {
	for key, val := range m {
		f, ok := optionsField(t, key)
		if !ok || !f.IsExported() {
			continue
		}
		validateLeaf(f.Type, val, path+"."+key, errs)
	}
}

var (
	regexpType      = reflect.TypeOf((*regexp.Regexp)(nil))
	eagerRegexpType = reflect.TypeOf((*EagerRegexp)(nil))
)

// validateLeaf checks one value against the type of the field that will
// hold it.
func validateLeaf(t reflect.Type, val any, path string, errs *[]string) {
	if val == nil || IsSkip(val) {
		return
	}
	// The one leaf with a spelling of its own: rewind.history takes false
	// for unbounded (#142).
	if path == "options.rewind.history" {
		switch v := val.(type) {
		case bool:
			if v {
				*errs = append(*errs, path+": true is not a cap; use false for unbounded")
			}
			return
		}
	}
	vt := reflect.TypeOf(val)
	isFunc := vt != nil && vt.Kind() == reflect.Func
	// Three slots take more than one shape, as their TS types do: a
	// match token is a regex or a matcher function, a match value is a
	// regex, a function or a {match, val} object, and number.exclude is a
	// function or a regex.
	switch {
	case strings.HasPrefix(path, "options.match.token."):
		if vt != regexpType && vt != eagerRegexpType && !isFunc {
			if _, ok := val.(string); !ok {
				bad(errs, path, "a serialized regex or a matcher function", val)
			}
		}
		return
	case strings.HasPrefix(path, "options.match.value.") && strings.Count(path, ".") == 3:
		if vt == regexpType || vt == eagerRegexpType || isFunc {
			return
		}
		if _, ok := val.(map[string]any); !ok {
			bad(errs, path, "a serialized regex, a matcher function or an object", val)
			return
		}
	case path == "options.number.exclude":
		if vt != regexpType && !isFunc {
			bad(errs, path, "a function or a serialized regex", val)
		}
		return
	}
	switch t.Kind() {
	case reflect.Ptr:
		if t == regexpType || t == eagerRegexpType {
			if vt != regexpType && vt != eagerRegexpType {
				bad(errs, path, "a serialized regex", val)
			}
			return
		}
		if t.Elem().Kind() == reflect.Struct {
			sub, ok := val.(map[string]any)
			if !ok {
				bad(errs, path, "object", val)
				return
			}
			validateStruct(t.Elem(), sub, path, errs)
			return
		}
		validateLeaf(t.Elem(), val, path, errs)
	case reflect.Bool:
		if _, ok := val.(bool); !ok {
			bad(errs, path, "boolean", val)
		}
	case reflect.String:
		if _, ok := val.(string); !ok {
			bad(errs, path, "string", val)
		}
	case reflect.Int, reflect.Int32, reflect.Int64:
		if _, ok := mapInt(val); !ok {
			bad(errs, path, "number", val)
		}
	case reflect.Interface:
		// `any`: anything goes (lex.emptyResult, errmsg.suffix, a keyword's val).
	case reflect.Func:
		if vt == nil || vt.Kind() != reflect.Func {
			bad(errs, path, "function", val)
			return
		}
		if !vt.AssignableTo(t) && !vt.ConvertibleTo(t) {
			*errs = append(*errs, fmt.Sprintf(
				"%s: the function reference has the wrong type (%s, want %s)", path, vt, t))
		}
	case reflect.Slice:
		arr, ok := val.([]any)
		if !ok {
			if vt != nil && vt.Kind() == reflect.Slice {
				return // a Go slice built in-process
			}
			bad(errs, path, "array", val)
			return
		}
		// A string list takes strings and the null that removes a position
		// (#151); any other element kind is validated as one.
		for i, item := range arr {
			if item == nil {
				continue
			}
			if t.Elem().Kind() == reflect.String {
				if _, ok := item.(string); !ok {
					bad(errs, fmt.Sprintf("%s[%d]", path, i), "string", item)
				}
				continue
			}
			validateLeaf(t.Elem(), item, fmt.Sprintf("%s[%d]", path, i), errs)
		}
	case reflect.Map:
		sub, ok := val.(map[string]any)
		if !ok {
			if vt != nil && vt.Kind() == reflect.Map {
				return // a Go map built in-process
			}
			bad(errs, path, "object", val)
			return
		}
		for name, entry := range sub {
			// A false entry removes a definition (value.def, comment.def),
			// as null does.
			if b, ok := entry.(bool); ok && !b && t.Elem().Kind() == reflect.Ptr {
				continue
			}
			validateLeaf(t.Elem(), entry, path+"."+name, errs)
		}
	case reflect.Struct:
		sub, ok := val.(map[string]any)
		if !ok {
			bad(errs, path, "object", val)
			return
		}
		validateStruct(t, sub, path, errs)
	}
}

func bad(errs *[]string, path, want string, val any) {
	got := fmt.Sprintf("%T", val)
	if vt := reflect.TypeOf(val); vt != nil && vt.Kind() == reflect.Func {
		got = "a function reference, which is only allowed in a declared code slot"
	} else {
		switch val.(type) {
		case map[string]any:
			got = "object"
		case []any:
			got = "array"
		case float64:
			got = "number"
		case bool:
			got = "boolean"
		case string:
			got = "string"
		}
	}
	*errs = append(*errs, fmt.Sprintf("%s: expected %s, got %s", path, want, got))
}
