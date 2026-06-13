package tabnas

// Strict JSON grammar — kept here as a test fixture, the Go counterpart
// to ts/test/json-plugin.ts.
//
// The tabnas engine ships no grammar; consumers bring their own grammar
// plugin. This fixture exists so the engine has a non-trivial grammar to
// test against (shared .tsv conformance fixtures, lexer/parser mechanics,
// and the Go-only MapRef/ListRef/Text introspection features). It mirrors
// JSON.parse: only quoted-string keys, double-quoted strings, plain
// decimal numbers, the true/false/null keywords, no comments, no trailing
// commas, no implicit structures.

import "testing"

// jsonNode helpers — set/append into either a plain map/slice or the
// MapRef/ListRef wrappers used when the info options are enabled.

func jsonMapSet(node any, key string, val any) any {
	if mr, ok := node.(MapRef); ok {
		mr.Val[key] = val
		return mr
	}
	m, _ := node.(map[string]any)
	m[key] = val
	return m
}

func jsonListAppend(node any, val any) any {
	if lr, ok := node.(ListRef); ok {
		lr.Val = append(lr.Val, val)
		return lr
	}
	s, _ := node.([]any)
	return append(s, val)
}

// jsonOptions restricts the engine to strict JSON. Mirrors JSON_OPTIONS
// in ts/test/json-plugin.ts.
func jsonOptions() Options {
	f := false
	return Options{
		Text: &TextOptions{Lex: &f},
		Number: &NumberOptions{
			Hex: &f, Oct: &f, Bin: &f,
			Sep: "",
			Exclude: func(s string) bool {
				return len(s) >= 2 && s[0] == '0' && s[1] == '0'
			},
		},
		String: &StringOptions{
			Chars:        `"`,
			MultiChars:   "",
			AllowUnknown: &f,
		},
		Comment: &CommentOptions{Lex: &f},
		Map:     &MapOptions{Extend: &f},
		Lex:     &LexOptions{Empty: &f},
		Rule:    &RuleOptions{Finish: &f},
		// Strict JSON keys are quoted strings only.
		TokenSet: map[string][]string{"KEY": {"#ST"}},
	}
}

// registerJSONGrammar installs the strict JSON rule set (val / map /
// list / pair / elem) on j. Exposed separately from the options so other
// fixtures can layer on the JSON core. cfg is read for the info
// (MapRef / ListRef / Text) and finish settings.
func registerJSONGrammar(j *Tabnas) {
	cfg := j.Config()

	// val: a value is a map, a list, or a plain scalar token.
	j.Rule("val", func(rs *RuleSpec, _ *Parser) {
		rs.BO = []StateAction{func(r *Rule, ctx *Context) {
			r.Node = Undefined
		}}
		rs.BC = []StateAction{func(r *Rule, ctx *Context) {
			if !IsUndefined(r.Node) {
				return
			}
			if !IsUndefined(r.Child.Node) {
				r.Node = r.Child.Node
				return
			}
			if r.OS == 0 {
				r.Node = Undefined
				return
			}
			val := r.O0.ResolveVal(r, ctx)
			if val == nil && r.O0.Tin != TinVL {
				r.Node = Undefined
				return
			}
			if cfg.TextInfo && (r.O0.Tin == TinST || r.O0.Tin == TinTX) {
				quote := ""
				if r.O0.Tin == TinST && len(r.O0.Src) > 0 {
					quote = string(r.O0.Src[0])
				}
				str, _ := val.(string)
				val = Text{Quote: quote, Str: str}
			}
			r.Node = val
		}}
		rs.Open = []*AltSpec{
			{S: [][]Tin{{TinOB}}, P: "map", B: 1, G: "map,json"},
			{S: [][]Tin{{TinOS}}, P: "list", B: 1, G: "list,json"},
			{S: [][]Tin{TinSetVAL}, G: "val,json"},
		}
		rs.Close = []*AltSpec{
			{S: [][]Tin{{TinZZ}}, G: "end,json"},
			{B: 1, G: "more,json"},
		}
	})

	// map: an object. bo creates the (possibly wrapped) node; bc marks
	// the wrapper's implicit flag.
	j.Rule("map", func(rs *RuleSpec, _ *Parser) {
		rs.BO = []StateAction{func(r *Rule, ctx *Context) {
			if cfg.MapRef {
				r.Node = MapRef{Val: make(map[string]any), Meta: make(map[string]any)}
			} else {
				r.Node = make(map[string]any)
			}
		}}
		rs.BC = []StateAction{func(r *Rule, ctx *Context) {
			if cfg.MapRef {
				if mr, ok := r.Node.(MapRef); ok {
					mr.Implicit = !(r.O0 != NoToken && r.O0.Tin == TinOB)
					r.Node = mr
				}
			}
		}}
		rs.Open = []*AltSpec{
			{S: [][]Tin{{TinOB}, {TinCB}}, B: 1, N: map[string]int{"pk": 0}, G: "map,json"},
			{S: [][]Tin{{TinOB}}, P: "pair", N: map[string]int{"pk": 0}, G: "map,json,pair"},
		}
		rs.Close = []*AltSpec{
			{S: [][]Tin{{TinCB}}, G: "end,json"},
		}
	})

	// list: an array.
	j.Rule("list", func(rs *RuleSpec, _ *Parser) {
		rs.BO = []StateAction{func(r *Rule, ctx *Context) {
			if cfg.ListRef {
				r.Node = ListRef{Val: make([]any, 0), Meta: make(map[string]any)}
			} else {
				r.Node = make([]any, 0)
			}
		}}
		rs.BC = []StateAction{func(r *Rule, ctx *Context) {
			if cfg.ListRef {
				if lr, ok := r.Node.(ListRef); ok {
					lr.Implicit = !(r.O0 != NoToken && r.O0.Tin == TinOS)
					r.Node = lr
				}
			}
		}}
		rs.Open = []*AltSpec{
			{S: [][]Tin{{TinOS}, {TinCS}}, B: 1, G: "list,json"},
			{S: [][]Tin{{TinOS}}, P: "elem", G: "list,elem,json"},
		}
		rs.Close = []*AltSpec{
			{S: [][]Tin{{TinCS}}, G: "end,json"},
		}
	})

	// pair: a key:value entry inside a map.
	j.Rule("pair", func(rs *RuleSpec, _ *Parser) {
		rs.BC = []StateAction{func(r *Rule, ctx *Context) {
			if _, ok := r.U["pair"]; !ok {
				return
			}
			key, _ := r.U["key"].(string)
			val := r.Child.Node
			if IsUndefined(val) {
				val = nil
			}
			r.Node = jsonMapSet(r.Node, key, val)
		}}
		rs.Open = []*AltSpec{
			{
				S: [][]Tin{{TinST}, {TinCL}},
				P: "val",
				U: map[string]any{"pair": true},
				G: "map,pair,key,json",
				A: func(r *Rule, ctx *Context) {
					keyToken := r.O0
					if keyToken.Tin == TinST || keyToken.Tin == TinTX {
						r.U["key"], _ = keyToken.Val.(string)
					} else {
						r.U["key"] = keyToken.Src
					}
				},
			},
		}
		rs.Close = []*AltSpec{
			{S: [][]Tin{{TinCA}}, R: "pair", G: "map,pair,json"},
			{S: [][]Tin{{TinCB}}, B: 1, G: "map,pair,json"},
		}
	})

	// elem: a value inside a list.
	j.Rule("elem", func(rs *RuleSpec, _ *Parser) {
		rs.BC = []StateAction{func(r *Rule, ctx *Context) {
			if IsUndefined(r.Child.Node) {
				return
			}
			r.Node = jsonListAppend(r.Node, r.Child.Node)
			if r.Parent != NoRule && r.Parent != nil {
				r.Parent.Node = r.Node
			}
		}}
		rs.Open = []*AltSpec{
			{P: "val", G: "list,elem,val,json"},
		}
		rs.Close = []*AltSpec{
			{S: [][]Tin{{TinCA}}, R: "elem", G: "list,elem,json"},
			{S: [][]Tin{{TinCS}}, B: 1, G: "list,elem,json"},
		}
	})
}

// jsonPlugin is the standard plugin form: apply strict options, then
// register the grammar.
func jsonPlugin(j *Tabnas, opts map[string]any) error {
	j.SetOptions(jsonOptions())
	registerJSONGrammar(j)
	return nil
}

// makeJSON builds a strict-JSON parser, optionally layering extra options
// (e.g. info.Map/List/Text for the introspection tests) over the base
// strict configuration.
func makeJSON(extra ...Options) *Tabnas {
	j := Make(jsonOptions())
	registerJSONGrammar(j)
	// Extra options are applied after the grammar exists so that rule
	// include/exclude filters operate on the installed alternates (and
	// info options reach the config the grammar closures captured).
	for _, o := range extra {
		j.SetOptions(o)
	}
	return j
}

// jsonParse is a parse-and-fatal-on-error helper for tests.
func jsonParse(t *testing.T, src string) any {
	t.Helper()
	out, err := makeJSON().Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return out
}
