package jsonic

import (
	tabnas "github.com/tabnas/parser/go"
)

// buildGrammar populates the default tabnas grammar rules using declarative GrammarAltSpec.
// This is a faithful port of grammar.ts, matching the exact alternate orderings
// produced by the JSON phase followed by the Tabnas extension phase.
func buildGrammar(rsm map[string]*tabnas.RuleSpec, cfg *tabnas.LexConfig) {
	// Named function references for the grammar.
	// These closures capture cfg for runtime configuration access.
	ref := map[tabnas.FuncRef]any{
		"@finish": tabnas.AltError(func(r *tabnas.Rule, ctx *tabnas.Context) *tabnas.Token {
			if !cfg.FinishRule {
				return ctx.T0
			}
			return nil
		}),

		"@pairkey": tabnas.AltAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			keyToken := r.O0
			var key string
			if keyToken.Tin == tabnas.TinST || keyToken.Tin == tabnas.TinTX {
				key, _ = keyToken.Val.(string)
			} else {
				key = keyToken.Src
			}
			r.U["key"] = key
		}),

		"@pairval": tabnas.AltAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			key, _ := r.U["key"].(string)
			val := r.Child.Node
			if tabnas.IsUndefined(val) {
				val = nil
			}
			if cfg.SafeKey && r.U["list"] == true {
				if key == "__proto__" || key == "constructor" {
					return
				}
			}
			// Drop keys that match the info marker to preserve metadata.
			if cfg.InfoMarker != "" && key == cfg.InfoMarker {
				return
			}
			prev := r.U["prev"]
			if prev == nil {
				nodeMapSet(r.Node, key, val)
			} else if cfg.MapMerge != nil {
				nodeMapSet(r.Node, key, cfg.MapMerge(prev, val, r, ctx))
			} else if cfg.MapExtend {
				nodeMapSet(r.Node, key, tabnas.Deep(prev, val))
			} else {
				nodeMapSet(r.Node, key, val)
			}
		}),

		// val rule state actions
		"@val-bo": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			r.Node = tabnas.Undefined
		}),

		"@val-bc": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if tabnas.IsUndefined(r.Node) {
				if tabnas.IsUndefined(r.Child.Node) {
					if r.OS == 0 {
						r.Node = tabnas.Undefined
					} else {
						val := r.O0.ResolveVal(r, ctx)
						// A nil value from a non-value token (e.g. #CS, #CB)
						// means "no value", not "null". Keep Undefined to match
						// TS where resolveVal returns undefined for such tokens.
						if val == nil && r.O0.Tin != tabnas.TinVL {
							r.Node = tabnas.Undefined
						} else {
							if cfg.TextInfo && (r.O0.Tin == tabnas.TinST || r.O0.Tin == tabnas.TinTX) {
								quote := ""
								if r.O0.Tin == tabnas.TinST && len(r.O0.Src) > 0 {
									quote = string(r.O0.Src[0])
								}
								str, _ := val.(string)
								val = tabnas.Text{Quote: quote, Str: str}
							}
							r.Node = val
						}
					}
				} else {
					r.Node = r.Child.Node
				}
			}
		}),

		// map rule state actions
		"@map-bo": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if cfg.MapRef {
				r.Node = tabnas.MapRef{
					Val:  make(map[string]any),
					Meta: make(map[string]any),
				}
			} else {
				r.Node = make(map[string]any)
			}
		}),

		"@map-bo-tabnas": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if v, ok := r.N["dmap"]; ok {
				r.N["dmap"] = v + 1
			} else {
				r.N["dmap"] = 1
			}
		}),

		"@map-bc": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if cfg.MapRef {
				implicit := !(r.O0 != tabnas.NoToken && r.O0.Tin == tabnas.TinOB)
				if mr, ok := r.Node.(tabnas.MapRef); ok {
					mr.Implicit = implicit
					r.Node = mr
				}
			}
		}),

		// list rule state actions
		"@list-bo": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if cfg.ListRef {
				r.Node = tabnas.ListRef{
					Val:  make([]any, 0),
					Meta: make(map[string]any),
				}
			} else {
				r.Node = make([]any, 0)
			}
		}),

		"@list-bo-tabnas": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if v, ok := r.N["dlist"]; ok {
				r.N["dlist"] = v + 1
			} else {
				r.N["dlist"] = 1
			}
			if r.Prev != tabnas.NoRule && r.Prev != nil {
				if implist, ok := r.Prev.U["implist"]; ok && implist == true {
					prevNode := r.Prev.Node
					if tabnas.IsUndefined(prevNode) {
						prevNode = nil
					}
					r.Node = nodeListAppend(r.Node, prevNode)
					r.Prev.Node = r.Node
				}
			}
		}),

		"@list-bc": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if cfg.ListRef {
				implicit := !(r.O0 != tabnas.NoToken && r.O0.Tin == tabnas.TinOS)
				if lr, ok := r.Node.(tabnas.ListRef); ok {
					lr.Implicit = implicit
					if c, ok := r.U["child$"]; ok {
						lr.Child = c
					}
					r.Node = lr
				}
			}
		}),

		// pair rule state actions
		"@pair-bc-json": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			if _, ok := r.U["pair"]; ok {
				key, _ := r.U["key"].(string)
				if cfg.SafeKey && r.U["list"] == true && (key == "__proto__" || key == "constructor") {
					return
				}
				// Drop keys that match the info marker to preserve metadata.
				if cfg.InfoMarker != "" && key == cfg.InfoMarker {
					return
				}
				r.U["prev"] = nodeMapGetVal(r.Node, r.U["key"])
				nodeMapSet(r.Node, key, r.Child.Node)
			}
		}),

		"@pair-bc-tabnas": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			if _, ok := r.U["pair"]; ok {
				key, _ := r.U["key"].(string)
				val := r.Child.Node
				if tabnas.IsUndefined(val) {
					val = nil
				}
				if cfg.SafeKey && r.U["list"] == true {
					if key == "__proto__" || key == "constructor" {
						return
					}
				}
				// Drop keys that match the info marker to preserve metadata.
				if cfg.InfoMarker != "" && key == cfg.InfoMarker {
					return
				}
				prev := r.U["prev"]
				if prev == nil {
					nodeMapSet(r.Node, key, val)
				} else if cfg.MapMerge != nil {
					nodeMapSet(r.Node, key, cfg.MapMerge(prev, val, r, ctx))
				} else if cfg.MapExtend {
					nodeMapSet(r.Node, key, tabnas.Deep(prev, val))
				} else {
					nodeMapSet(r.Node, key, val)
				}
			}
		}),

		"@pair-bc-child": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			if childFlag, ok := r.U["child"]; !ok || childFlag != true {
				return
			}
			val := r.Child.Node
			if tabnas.IsUndefined(val) {
				val = nil
			}
			prev, hasPrev := nodeMapGet(r.Node, "child$")
			if !hasPrev {
				nodeMapSet(r.Node, "child$", val)
			} else if cfg.MapMerge != nil {
				nodeMapSet(r.Node, "child$", cfg.MapMerge(prev, val, r, ctx))
			} else if cfg.MapExtend {
				nodeMapSet(r.Node, "child$", tabnas.Deep(prev, val))
			} else {
				nodeMapSet(r.Node, "child$", val)
			}
		}),

		// elem rule state actions
		"@elem-bc-json": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			done, _ := r.U["done"].(bool)
			if !done && !tabnas.IsUndefined(r.Child.Node) {
				if _, ok := nodeListVal(r.Node); ok {
					r.Node = nodeListAppend(r.Node, r.Child.Node)
					if r.Parent != tabnas.NoRule && r.Parent != nil {
						r.Parent.Node = r.Node
					}
				}
			}
		}),

		"@elem-bc-pair": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			if pair, ok := r.U["pair"]; !ok || pair != true {
				return
			}
			if cfg.ListPair {
				key := r.U["key"].(string)
				val := r.Child.Node
				if tabnas.IsUndefined(val) {
					val = nil
				}
				pairObj := map[string]any{key: val}
				if _, ok := nodeListVal(r.Node); ok {
					r.Node = nodeListAppend(r.Node, pairObj)
					if r.Parent != tabnas.NoRule && r.Parent != nil {
						r.Parent.Node = r.Node
					}
				}
			} else {
				r.U["prev"] = nodeMapGetVal(r.Node, r.U["key"])
				key, _ := r.U["key"].(string)
				val := r.Child.Node
				if tabnas.IsUndefined(val) {
					val = nil
				}
				if cfg.SafeKey && r.U["list"] == true {
					if key == "__proto__" || key == "constructor" {
						return
					}
				}
				// Drop keys that match the info marker to preserve metadata.
				if cfg.InfoMarker != "" && key == cfg.InfoMarker {
					return
				}
				prev := r.U["prev"]
				if prev == nil {
					nodeMapSet(r.Node, key, val)
				} else if cfg.MapMerge != nil {
					nodeMapSet(r.Node, key, cfg.MapMerge(prev, val, r, ctx))
				} else if cfg.MapExtend {
					nodeMapSet(r.Node, key, tabnas.Deep(prev, val))
				} else {
					nodeMapSet(r.Node, key, val)
				}
			}
		}),

		"@elem-bc-child": tabnas.StateAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if childFlag, ok := r.U["child"]; !ok || childFlag != true {
				return
			}
			val := r.Child.Node
			if tabnas.IsUndefined(val) {
				val = nil
			}
			if r.Parent != tabnas.NoRule && r.Parent != nil {
				prev, hasPrev := r.Parent.U["child$"]
				if !hasPrev {
					r.Parent.U["child$"] = val
				} else if cfg.MapExtend {
					r.Parent.U["child$"] = tabnas.Deep(prev, val)
				} else {
					r.Parent.U["child$"] = val
				}
			}
		}),

		// Inline actions used in alts
		"@val-close-err": tabnas.AltError(func(r *tabnas.Rule, ctx *tabnas.Context) *tabnas.Token {
			if r.D == 0 {
				return ctx.T0
			}
			return nil
		}),

		"@implist-cond": tabnas.AltCond(func(r *tabnas.Rule, ctx *tabnas.Context) bool {
			return r.Prev != tabnas.NoRule && r.Prev != nil && r.Prev.U["implist"] == true
		}),

		"@elem-double-comma": tabnas.AltAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if _, ok := nodeListVal(r.Node); ok {
				r.Node = nodeListAppend(r.Node, nil)
				if r.Parent != tabnas.NoRule && r.Parent != nil {
					r.Parent.Node = r.Node
				}
			}
		}),

		"@elem-single-comma": tabnas.AltAction(func(r *tabnas.Rule, ctx *tabnas.Context) {
			_ = ctx
			if _, ok := nodeListVal(r.Node); ok {
				r.Node = nodeListAppend(r.Node, nil)
				if r.Parent != tabnas.NoRule && r.Parent != nil {
					r.Parent.Node = r.Node
				}
			}
		}),

		"@elem-pair-err": tabnas.AltError(func(r *tabnas.Rule, ctx *tabnas.Context) *tabnas.Token {
			if cfg.ListProperty || cfg.ListPair {
				return nil
			}
			return ctx.T0
		}),

		"@elem-close-err": tabnas.AltError(func(r *tabnas.Rule, ctx *tabnas.Context) *tabnas.Token {
			return r.C0
		}),
	}

	// Helper to resolve a GrammarAltSpec slice to []*AltSpec.
	resolve := func(gas []*tabnas.GrammarAltSpec) []*tabnas.AltSpec {
		alts := make([]*tabnas.AltSpec, len(gas))
		for i, ga := range gas {
			alts[i] = tabnas.ResolveGrammarAltStatic(ga, ref)
		}
		return alts
	}

	// ====== VAL rule ======
	valSpec := &tabnas.RuleSpec{Name: "val"}

	valSpec.BO = []tabnas.StateAction{ref["@val-bo"].(tabnas.StateAction)}
	valSpec.BC = []tabnas.StateAction{ref["@val-bc"].(tabnas.StateAction)}

	valOpen := resolve([]*tabnas.GrammarAltSpec{
		{S: "#OB", P: "map", B: 1, G: "map,json"},
		{S: "#OS", P: "list", B: 1, G: "list,json"},
		{S: "#KEY #CL", C: map[string]any{"d": 0}, P: "map", B: 2, G: "pair,tabnas,top"},
		{S: "#KEY #CL", P: "map", B: 2, N: map[string]int{"pk": 1}, G: "pair,tabnas"},
		{S: "#VAL", G: "val,json"},
	})
	// CB|CS in single slot:
	valOpen = append(valOpen, tabnas.ResolveGrammarAltStatic(
		&tabnas.GrammarAltSpec{S: []string{"#CB #CS"}, C: map[string]any{"d": tabnas.CGt(0)}, B: 1, G: "val,imp,null,tabnas"}, ref))
	valOpen = append(valOpen, resolve([]*tabnas.GrammarAltSpec{
		{S: "#CA", C: map[string]any{"d": 0}, P: "list", B: 1, G: "list,imp,tabnas"},
		{S: "#CA", B: 1, G: "list,val,imp,null,tabnas"},
		{S: "#ZZ", G: "tabnas"},
	})...)
	valSpec.Open = valOpen

	valClose := resolve([]*tabnas.GrammarAltSpec{
		{S: "#ZZ", G: "end,json"},
	})
	// CB|CS in single slot:
	valClose = append(valClose, tabnas.ResolveGrammarAltStatic(
		&tabnas.GrammarAltSpec{S: []string{"#CB #CS"}, B: 1, E: "@val-close-err", G: "val,json,close"}, ref))
	valClose = append(valClose, resolve([]*tabnas.GrammarAltSpec{
		{S: "#CA", C: map[string]any{"n.dlist": tabnas.CLte(0), "n.dmap": tabnas.CLte(0)},
			R: "list", U: map[string]any{"implist": true}, G: "list,val,imp,comma,tabnas"},
		{C: map[string]any{"n.dlist": tabnas.CLte(0), "n.dmap": tabnas.CLte(0)},
			R: "list", U: map[string]any{"implist": true}, B: 1, G: "list,val,imp,space,tabnas"},
		{S: "#ZZ", G: "tabnas"},
		{B: 1, G: "more,json"},
	})...)
	valSpec.Close = valClose

	// ====== MAP rule ======
	mapSpec := &tabnas.RuleSpec{Name: "map"}

	mapSpec.BO = []tabnas.StateAction{
		ref["@map-bo"].(tabnas.StateAction),
		ref["@map-bo-tabnas"].(tabnas.StateAction),
	}
	mapSpec.BC = []tabnas.StateAction{ref["@map-bc"].(tabnas.StateAction)}

	mapSpec.Open = resolve([]*tabnas.GrammarAltSpec{
		{S: "#OB #ZZ", B: 1, E: "@finish", G: "end,tabnas"},
		{S: "#OB #CB", B: 1, N: map[string]int{"pk": 0}, G: "map,json"},
		{S: "#OB", P: "pair", N: map[string]int{"pk": 0}, G: "map,json,pair"},
		{S: "#KEY #CL", P: "pair", B: 2, G: "pair,list,val,imp,tabnas"},
	})

	// For map.Close, we need to merge token sets for the third alt.
	// "#CA #CS" + VAL tokens in a single slot → need raw AltSpec for that one.
	mapClose := resolve([]*tabnas.GrammarAltSpec{
		{S: "#CB", C: map[string]any{"n.pk": tabnas.CLte(0)}, G: "end,json"},
		{S: "#CB", B: 1, G: "path,tabnas"},
		// slot 0 = merge(CA, CS, VAL) — handled below
	})
	// Third alt: CA|CS|VAL tokens in single slot
	mapClose = append(mapClose, tabnas.ResolveGrammarAltStatic(
		&tabnas.GrammarAltSpec{S: []string{"#CA #CS #VAL"}, B: 1, G: "end,path,tabnas"}, ref))
	mapClose = append(mapClose, tabnas.ResolveGrammarAltStatic(
		&tabnas.GrammarAltSpec{S: "#ZZ", E: "@finish", G: "end,tabnas"}, ref))
	mapSpec.Close = mapClose

	// ====== LIST rule ======
	listSpec := &tabnas.RuleSpec{Name: "list"}

	listSpec.BO = []tabnas.StateAction{
		ref["@list-bo"].(tabnas.StateAction),
		ref["@list-bo-tabnas"].(tabnas.StateAction),
	}
	listSpec.BC = []tabnas.StateAction{ref["@list-bc"].(tabnas.StateAction)}

	// First alt uses a condition function directly (not declarative).
	listOpen := []*tabnas.AltSpec{
		tabnas.ResolveGrammarAltStatic(&tabnas.GrammarAltSpec{C: "@implist-cond", P: "elem"}, ref),
	}
	listOpen = append(listOpen, resolve([]*tabnas.GrammarAltSpec{
		{S: "#OS #CS", B: 1, G: "list,json"},
		{S: "#OS", P: "elem", G: "list,elem,json"},
		{S: "#CA", P: "elem", B: 1, G: "list,elem,val,imp,tabnas"},
		{P: "elem", G: "list,elem,tabnas"},
	})...)
	listSpec.Open = listOpen

	listSpec.Close = resolve([]*tabnas.GrammarAltSpec{
		{S: "#CS", G: "end,json"},
		{S: "#ZZ", E: "@finish", G: "end,tabnas"},
	})

	// ====== PAIR rule ======
	pairSpec := &tabnas.RuleSpec{Name: "pair"}

	pairSpec.BC = []tabnas.StateAction{
		ref["@pair-bc-json"].(tabnas.StateAction),
		ref["@pair-bc-tabnas"].(tabnas.StateAction),
		ref["@pair-bc-child"].(tabnas.StateAction),
	}

	pairOpen := resolve([]*tabnas.GrammarAltSpec{
		{S: "#KEY #CL", P: "val", U: map[string]any{"pair": true}, A: "@pairkey", G: "map,pair,key,json"},
		{S: "#CA", G: "map,pair,comma,tabnas"},
	})
	if cfg.MapChild {
		pairOpen = append(pairOpen, tabnas.ResolveGrammarAltStatic(
			&tabnas.GrammarAltSpec{S: "#CL", P: "val",
				U: map[string]any{"done": true, "child": true}}, ref))
	}
	pairSpec.Open = pairOpen

	pairSpec.Close = resolve([]*tabnas.GrammarAltSpec{
		{S: "#CB", C: map[string]any{"n.pk": tabnas.CLte(0)}, B: 1, G: "map,pair,json"},
		{S: "#CA #CB", C: map[string]any{"n.pk": tabnas.CLte(0)}, B: 1, G: "map,pair,comma,tabnas"},
		{S: "#CA #ZZ", G: "end,tabnas"},
		{S: "#CA", C: map[string]any{"n.pk": tabnas.CLte(0)}, R: "pair", G: "map,pair,json"},
		{S: "#CA", C: map[string]any{"n.dmap": tabnas.CLte(1)}, R: "pair", G: "map,pair,tabnas"},
		{S: "#KEY", C: map[string]any{"n.dmap": tabnas.CLte(1)}, R: "pair", B: 1, G: "map,pair,imp,tabnas"},
	})

	// CB|CA|CS|KEY in single slot
	pairSpec.Close = append(pairSpec.Close, tabnas.ResolveGrammarAltStatic(
		&tabnas.GrammarAltSpec{S: []string{"#CB #CA #CS #KEY"}, C: map[string]any{"n.pk": tabnas.CGt(0)},
			B: 1, G: "map,pair,imp,path,tabnas"}, ref))
	// Remaining pair close alts.
	pairSpec.Close = append(pairSpec.Close, resolve([]*tabnas.GrammarAltSpec{
		{S: "#CS", E: "@elem-close-err", G: "end,tabnas"},
		{S: "#ZZ", E: "@finish", G: "map,pair,json"},
		{R: "pair", B: 1, G: "map,pair,imp,tabnas"},
	})...)

	// ====== ELEM rule ======
	elemSpec := &tabnas.RuleSpec{Name: "elem"}

	elemSpec.BC = []tabnas.StateAction{
		ref["@elem-bc-json"].(tabnas.StateAction),
		ref["@elem-bc-pair"].(tabnas.StateAction),
		ref["@elem-bc-child"].(tabnas.StateAction),
	}

	elemOpen := resolve([]*tabnas.GrammarAltSpec{
		{S: "#CA #CA", B: 2, U: map[string]any{"done": true}, A: "@elem-double-comma",
			G: "list,elem,imp,null,tabnas"},
		{S: "#CA", U: map[string]any{"done": true}, A: "@elem-single-comma",
			G: "list,elem,imp,null,tabnas"},
		{S: "#KEY #CL", P: "val",
			N: map[string]int{"pk": 1, "dmap": 1},
			U: map[string]any{"done": true, "pair": true, "list": true},
			A: "@pairkey", E: "@elem-pair-err", G: "elem,pair,tabnas"},
	})
	if cfg.ListChild {
		elemOpen = append(elemOpen, tabnas.ResolveGrammarAltStatic(
			&tabnas.GrammarAltSpec{S: "#CL", P: "val",
				U: map[string]any{"done": true, "child": true, "list": true},
				G: "elem,child,tabnas"}, ref))
	}
	elemOpen = append(elemOpen, tabnas.ResolveGrammarAltStatic(
		&tabnas.GrammarAltSpec{P: "val", G: "list,elem,val,json"}, ref))
	elemSpec.Open = elemOpen

	elemClose := []*tabnas.AltSpec{
		// CA in slot 0, CS|ZZ in slot 1:
		tabnas.ResolveGrammarAltStatic(&tabnas.GrammarAltSpec{S: []string{"#CA", "#CS #ZZ"}, B: 1, G: "list,elem,comma,tabnas"}, ref),
	}
	elemClose = append(elemClose, resolve([]*tabnas.GrammarAltSpec{
		{S: "#CA", R: "elem", G: "list,elem,json"},
		{S: "#CS", B: 1, G: "list,elem,json"},
		{S: "#ZZ", E: "@finish", G: "list,elem,json"},
		{S: "#CB", E: "@elem-close-err", G: "end,tabnas"},
		{R: "elem", B: 1, G: "list,elem,imp,tabnas"},
	})...)
	elemSpec.Close = elemClose

	rsm["val"] = valSpec
	rsm["map"] = mapSpec
	rsm["list"] = listSpec
	rsm["pair"] = pairSpec
	rsm["elem"] = elemSpec
}

// nodeListAppend appends a value to a list node (plain []any or ListRef).
func nodeListAppend(node any, val any) any {
	if lr, ok := node.(tabnas.ListRef); ok {
		lr.Val = append(lr.Val, val)
		return lr
	}
	if arr, ok := node.([]any); ok {
		return append(arr, val)
	}
	return node
}

// nodeListVal extracts the []any from a list node.
func nodeListVal(node any) ([]any, bool) {
	if lr, ok := node.(tabnas.ListRef); ok {
		return lr.Val, true
	}
	if arr, ok := node.([]any); ok {
		return arr, true
	}
	return nil, false
}

// nodeMapSet sets a key on a map node.
func nodeMapSet(node any, key any, val any) {
	k, _ := key.(string)
	if m, ok := node.(map[string]any); ok {
		m[k] = val
	} else if mr, ok := node.(tabnas.MapRef); ok {
		mr.Val[k] = val
	}
}

// nodeMapGet gets a value from a map node.
func nodeMapGet(node any, key any) (any, bool) {
	k, _ := key.(string)
	if m, ok := node.(map[string]any); ok {
		v, exists := m[k]
		return v, exists
	}
	if mr, ok := node.(tabnas.MapRef); ok {
		v, exists := mr.Val[k]
		return v, exists
	}
	return nil, false
}

// nodeMapGetVal returns the value or nil.
func nodeMapGetVal(node any, key any) any {
	v, _ := nodeMapGet(node, key)
	return v
}
