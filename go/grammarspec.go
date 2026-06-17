// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"fmt"
	"reflect"
	"strings"
)

// FuncRef is a string starting with "@" that references a function in a Ref map.
type FuncRef = string

// Declarative grammar specification; mirrors the TypeScript GrammarSpec type.
type GrammarSpec struct {
	Ref        map[FuncRef]any             // Maps FuncRef strings (e.g. "@finish") to Go functions.
	Options    *Options                    // Typed options merged in (via SetOptions) before rules are processed.
	OptionsMap map[string]any              // Map-form options; FuncRef values resolved via Ref before applying.
	Rule       map[string]*GrammarRuleSpec // Open/close alternates keyed by rule name.
	V          int                         // Builtin config-schema version; engine refuses V > BUILTIN_SCHEMA_VERSION. Zero ⇒ 1.
}

// Open and close alternates for a single rule (each: []*GrammarAltSpec or *GrammarAltListSpec).
type GrammarRuleSpec struct {
	Open  any // Opening alternates: []*GrammarAltSpec or *GrammarAltListSpec.
	Close any // Closing alternates: []*GrammarAltSpec or *GrammarAltListSpec.
}

// Alt specs paired with injection modifiers controlling how they merge into a rule.
type GrammarAltListSpec struct {
	Alts   []*GrammarAltSpec  // Alternates to install.
	Inject *GrammarInjectSpec // How to merge Alts into existing alternates (nil = default prepend).
}

// GrammarInjectSpec controls how alts are merged into existing rule alternates.
type GrammarInjectSpec struct {
	Append bool  // If true, append; if false, prepend (default).
	Clear  bool  // If true, empty the existing alternates before inserting.
	Delete []int // Indices to delete (supports negative).
	Move   []int // Pairs: [from, to, from, to, ...].
}

// Optional settings applied when a grammar spec is installed; its Rule.Alt.G tags are appended to every rule-alt G.
type GrammarSetting struct {
	Rule *GrammarSettingRule // Rule-level settings to apply across the grammar.
}

// Rule-level grammar settings, wrapping alt-level settings.
type GrammarSettingRule struct {
	Alt *GrammarSettingAlt // Per-alt settings applied to every rule-alt.
}

// Per-alt grammar settings (currently only group tags).
type GrammarSettingAlt struct {
	G any // Group tag(s) appended to each alt's G: string (comma-separated) or []string.
}

// Declarative alternate spec: token fields use "#NAME" strings, function fields use "@name" FuncRefs.
type GrammarAltSpec struct {
	S any            // Token spec: string ("#KEY #CL", each name a slot) or []string (per-element alternatives).
	B any            // Backtrack: int or FuncRef string.
	P string         // Push rule name or FuncRef.
	R string         // Replace rule name or FuncRef.
	A any            // Action: FuncRef string, or []any of refs/AltActions run in order.
	E FuncRef        // Error function ref.
	H FuncRef        // Modifier function ref.
	C any            // Condition: FuncRef string or map[string]any for declarative.
	N map[string]int // Counter increments.
	U map[string]any // Custom props.
	K map[string]any // Propagated custom props.
	G string         // Group tags (comma-separated).
}

// Grammar applies a declarative grammar specification to this Tabnas instance.
// Options are applied first, then rules are processed.
// An optional *GrammarSetting may be supplied to append a tag (or tags) to
// every rule-alt G property in the spec.
// Returns an error if any FuncRef is missing or has the wrong type.
func (j *Tabnas) Grammar(gs *GrammarSpec, setting ...*GrammarSetting) (err error) {
	// Recover guard: malformed specs must produce errors, never panics.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Grammar: internal error: %v", r)
		}
	}()
	// Refuse a grammar that requires a newer builtin config-schema than
	// this engine implements (zero ⇒ current). Mirrors the TS version gate.
	if gs.V != 0 {
		if gs.V < 0 {
			return fmt.Errorf("Grammar: invalid builtin schema version: %d (expected a positive integer)", gs.V)
		}
		if gs.V > BUILTIN_SCHEMA_VERSION {
			return fmt.Errorf("Grammar: requires builtin schema version %d, but this engine supports up to %d", gs.V, BUILTIN_SCHEMA_VERSION)
		}
	}

	// The `$` ref-namespace is reserved for engine builtins; a user ref
	// key may not contain `$` (it would shadow, or be shadowed by, a
	// builtin in the merge below).
	for key := range gs.Ref {
		if strings.Contains(key, "$") {
			return fmt.Errorf("Grammar: '$' is reserved for engine builtins; user ref key %q may not contain '$'", key)
		}
	}

	// Merge the standard `$`-suffixed builtins under any spec-supplied
	// refs (spec wins). Lets a serialized, function-free GrammarSpec
	// reference engine builtins (e.g. @probeInit$) by name. See builtins.go.
	ref := mergeBuiltinRefs(gs.Ref)

	// Apply typed Options directly.
	if gs.Options != nil {
		j.SetOptions(*gs.Options)
	}

	// Apply OptionsMap with FuncRef resolution.
	if gs.OptionsMap != nil {
		resolved := ResolveFuncRefs(gs.OptionsMap, ref)
		if resolvedMap, ok := resolved.(map[string]any); ok {
			opts := MapToOptions(resolvedMap)
			j.SetOptions(opts)
		}
	}

	// Resolve the optional grammar setting's alt.g tags once.
	altGTags := extractSettingAltG(setting)

	if gs.Rule != nil {
		for rulename, rulespec := range gs.Rule {
			var resolveErr error
			j.Rule(rulename, func(rs *RuleSpec, _ *Parser) {
				// Process Open alts.
				if rulespec.Open != nil {
					if err := applyGrammarAlts(j, rs, rulespec.Open, ref, true, altGTags); err != nil {
						resolveErr = err
						return
					}
				}

				// Process Close alts.
				if rulespec.Close != nil {
					if err := applyGrammarAlts(j, rs, rulespec.Close, ref, false, altGTags); err != nil {
						resolveErr = err
						return
					}
				}

				// Auto-wire reserved FuncRef names for state actions.
				if ref != nil {
					wireStateActions(rs, ref)
				}
			})
			if resolveErr != nil {
				return resolveErr
			}
		}
	}

	return nil
}

// extractSettingAltG returns the list of tag strings from the variadic
// setting slice (first non-nil entry wins).  Returns nil when no tags
// are supplied.  Accepts string (comma-separated) or []string.
func extractSettingAltG(setting []*GrammarSetting) []string {
	for _, s := range setting {
		if s == nil || s.Rule == nil || s.Rule.Alt == nil || s.Rule.Alt.G == nil {
			continue
		}
		switch v := s.Rule.Alt.G.(type) {
		case string:
			return splitGroupTags(v)
		case []string:
			out := make([]string, 0, len(v))
			for _, t := range v {
				t = strings.TrimSpace(t)
				if t != "" {
					out = append(out, t)
				}
			}
			return out
		}
	}
	return nil
}

// splitGroupTags splits a comma-separated tag string into a []string,
// trimming surrounding whitespace and discarding empty entries.
func splitGroupTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// mergeG combines the existing g tag string with the supplied extra tags,
// returning a single comma-separated string.
func mergeG(existing string, extra []string) string {
	if len(extra) == 0 {
		return existing
	}
	tags := splitGroupTags(existing)
	tags = append(tags, extra...)
	return strings.Join(tags, ",")
}

// GrammarText parses a tabnas grammar text string into a GrammarSpec
// and applies it. The text is parsed using a default Tabnas instance,
// and the resulting map is used as the OptionsMap of a GrammarSpec.
// An optional *GrammarSetting may be supplied to append a tag (or tags)
// to every rule-alt G property in the spec.
// This is a convenience that replaces:
//
//	gs := tabnas.Make()
//	parsed, _ := gs.Parse(text)
//	j.Grammar(&GrammarSpec{OptionsMap: parsed.(map[string]any)})
func (j *Tabnas) GrammarText(text string, setting ...*GrammarSetting) (err error) {
	// The map→spec conversion below can panic on malformed input; the
	// nested Grammar() has its own guard, but wrap the whole body so the
	// conversion is covered too.
	defer func() {
		if r := recover(); r != nil {
			err = j.internalError("GrammarText", r)
		}
	}()

	parsed, perr := parseText("GrammarText", text)
	if perr != nil {
		return perr
	}
	if parsed == nil {
		return nil
	}
	gsMap, ok := parsed.(map[string]any)
	if !ok {
		return fmt.Errorf("GrammarText: expected map, got %T", parsed)
	}
	gs := &GrammarSpec{}
	if optionsMap, ok := gsMap["options"].(map[string]any); ok {
		gs.OptionsMap = optionsMap
	} else if _, hasRule := gsMap["rule"]; !hasRule {
		// No "options" wrapper and no "rule" key — treat the entire map as options.
		gs.OptionsMap = gsMap
	}
	if ruleMap, ok := gsMap["rule"].(map[string]any); ok {
		gs.Rule = mapToGrammarRules(ruleMap)
	}
	// Builtin config-schema version (parsed numbers arrive as float64).
	if v, ok := gsMap["v"]; ok {
		gs.V = cfgInt(v)
	}
	return j.Grammar(gs, setting...)
}

// mapToGrammarRules converts a parsed rule map into typed GrammarRuleSpec map.
func mapToGrammarRules(ruleMap map[string]any) map[string]*GrammarRuleSpec {
	rules := make(map[string]*GrammarRuleSpec, len(ruleMap))
	for name, v := range ruleMap {
		rm, ok := v.(map[string]any)
		if !ok {
			continue
		}
		spec := &GrammarRuleSpec{}
		if open, ok := rm["open"]; ok {
			spec.Open = parseGrammarAltsOrSpec(open)
		}
		if close, ok := rm["close"]; ok {
			spec.Close = parseGrammarAltsOrSpec(close)
		}
		rules[name] = spec
	}
	return rules
}

// parseGrammarAltsOrSpec handles both forms:
//   - []any (plain alt array) → []*GrammarAltSpec
//   - map[string]any with "alts" and "inject" → *GrammarAltListSpec
func parseGrammarAltsOrSpec(v any) any {
	// Plain array form.
	if arr, ok := v.([]any); ok {
		return parseGrammarAlts(arr)
	}
	// Map form with alts + inject.
	if m, ok := v.(map[string]any); ok {
		altsRaw, hasAlts := m["alts"]
		if !hasAlts {
			return nil
		}
		altsArr, ok := altsRaw.([]any)
		if !ok {
			return nil
		}
		alts := parseGrammarAlts(altsArr)
		spec := &GrammarAltListSpec{Alts: alts}
		if injectRaw, ok := m["inject"].(map[string]any); ok {
			spec.Inject = &GrammarInjectSpec{}
			if append_, ok := injectRaw["append"].(bool); ok {
				spec.Inject.Append = append_
			}
			if del, ok := injectRaw["delete"].([]any); ok {
				for _, d := range del {
					if f, ok := d.(float64); ok {
						spec.Inject.Delete = append(spec.Inject.Delete, int(f))
					}
				}
			}
			if mv, ok := injectRaw["move"].([]any); ok {
				for _, m := range mv {
					if f, ok := m.(float64); ok {
						spec.Inject.Move = append(spec.Inject.Move, int(f))
					}
				}
			}
		}
		return spec
	}
	return nil
}

// parseGrammarAlts converts a parsed alt array ([]any of maps) to []*GrammarAltSpec.
func parseGrammarAlts(arr []any) []*GrammarAltSpec {
	alts := make([]*GrammarAltSpec, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		alt := mapToGrammarAltSpec(m)
		alts = append(alts, alt)
	}
	return alts
}

// mapToGrammarAltSpec converts a parsed map to a GrammarAltSpec.
func mapToGrammarAltSpec(m map[string]any) *GrammarAltSpec {
	alt := &GrammarAltSpec{}
	if v, ok := m["s"]; ok {
		alt.S = v // string or []string ([]any of strings)
	}
	if v, ok := m["b"]; ok {
		alt.B = v // int (float64 from parse) or FuncRef string
	}
	if v, ok := m["p"].(string); ok {
		alt.P = v
	}
	if v, ok := m["r"].(string); ok {
		alt.R = v
	}
	if v, ok := m["a"]; ok {
		alt.A = v // FuncRef string or []any of refs (array-`a`)
	}
	if v, ok := m["e"].(string); ok {
		alt.E = v
	}
	if v, ok := m["h"].(string); ok {
		alt.H = v
	}
	if v, ok := m["c"]; ok {
		alt.C = v // FuncRef string or map[string]any
	}
	if v, ok := m["n"].(map[string]any); ok {
		alt.N = make(map[string]int, len(v))
		for k, val := range v {
			if f, ok := val.(float64); ok {
				alt.N[k] = int(f)
			}
		}
	}
	if v, ok := m["u"].(map[string]any); ok {
		alt.U = v
	}
	if v, ok := m["k"].(map[string]any); ok {
		alt.K = v
	}
	if v, ok := m["g"].(string); ok {
		alt.G = v
	}
	return alt
}

// applyGrammarAlts resolves and applies grammar alts to a rule spec.
// Handles both plain []*GrammarAltSpec and *GrammarAltListSpec with inject.
// When extraG is non-empty, those tags are appended to every alt's G field.
func applyGrammarAlts(j *Tabnas, rs *RuleSpec, spec any, ref map[FuncRef]any, isOpen bool, extraG []string) error {
	var gas []*GrammarAltSpec
	var inject *GrammarInjectSpec

	switch v := spec.(type) {
	case []*GrammarAltSpec:
		gas = v
	case *GrammarAltListSpec:
		gas = v.Alts
		inject = v.Inject
	default:
		return nil
	}

	// Append the setting's alt-g tags to each alt's G prior to resolution.
	if len(extraG) > 0 {
		merged := make([]*GrammarAltSpec, len(gas))
		for i, ga := range gas {
			if ga == nil {
				merged[i] = nil
				continue
			}
			cp := *ga
			cp.G = mergeG(ga.G, extraG)
			merged[i] = &cp
		}
		gas = merged
	}

	resolved, err := j.resolveGrammarAlts(gas, ref)
	if err != nil {
		return err
	}

	dest := &rs.close
	if isOpen {
		dest = &rs.open
	}

	// Apply inject modifiers (clear, delete, move) to existing alts first.
	if inject != nil && (inject.Clear || len(inject.Delete) > 0 || len(inject.Move) > 0) {
		*dest = modifyAltList(*dest, &AltModListOpts{
			Clear:  inject.Clear,
			Delete: inject.Delete,
			Move:   inject.Move,
		})
	}

	// Insert resolved alts: append or prepend (default: prepend).
	if inject != nil && inject.Append {
		*dest = append(*dest, resolved...)
	} else {
		*dest = append(resolved, *dest...)
	}

	return nil
}

// resolveGrammarAlts converts a slice of GrammarAltSpec to concrete AltSpec.
func (j *Tabnas) resolveGrammarAlts(gas []*GrammarAltSpec, ref map[FuncRef]any) ([]*AltSpec, error) {
	alts := make([]*AltSpec, 0, len(gas))
	for _, ga := range gas {
		alt, err := j.resolveGrammarAlt(ga, ref)
		if err != nil {
			return nil, err
		}
		alts = append(alts, alt)
	}
	return alts, nil
}

// resolveGrammarAlt converts a single GrammarAltSpec to a concrete AltSpec.
// resolveActionField resolves a GrammarAltSpec.A value — a FuncRef
// string, an AltAction, or an array of either — into a single AltAction
// (or nil for none). An array is collapsed to one ordered call that
// short-circuits when a prior action sets ctx.ParseErr (the engine's
// equivalent of the TS error-token short-circuit).
func resolveActionField(a any, ref map[FuncRef]any) (AltAction, error) {
	switch av := a.(type) {
	case nil:
		return nil, nil
	case string:
		if av == "" {
			return nil, nil
		}
		return requireAction(ref, av)
	case AltAction:
		return av, nil
	case func(*Rule, *Context):
		return AltAction(av), nil
	case []any:
		return resolveActionList(av, ref)
	case []string:
		items := make([]any, len(av))
		for i, s := range av {
			items[i] = s
		}
		return resolveActionList(items, ref)
	}
	return nil, fmt.Errorf("Grammar: invalid action field type %T", a)
}

func requireAction(ref map[FuncRef]any, name string) (AltAction, error) {
	fn, err := RequireRef(ref, name, "action")
	if err != nil {
		return nil, err
	}
	af, ok := fn.(AltAction)
	if !ok {
		if raw, ok2 := fn.(func(*Rule, *Context)); ok2 {
			return AltAction(raw), nil
		}
		return nil, fmt.Errorf("Grammar: ref %q is not an AltAction", name)
	}
	return af, nil
}

func resolveActionList(items []any, ref map[FuncRef]any) (AltAction, error) {
	fns := make([]AltAction, 0, len(items))
	for _, el := range items {
		switch ev := el.(type) {
		case string:
			af, err := requireAction(ref, ev)
			if err != nil {
				return nil, err
			}
			fns = append(fns, af)
		case AltAction:
			fns = append(fns, ev)
		case func(*Rule, *Context):
			fns = append(fns, AltAction(ev))
		default:
			return nil, fmt.Errorf("Grammar: invalid action list element type %T", el)
		}
	}
	if len(fns) == 0 {
		return nil, nil
	}
	if len(fns) == 1 {
		return fns[0], nil
	}
	return func(r *Rule, ctx *Context) {
		for _, f := range fns {
			f(r, ctx)
			if ctx.ParseErr != nil {
				return
			}
		}
	}, nil
}

func (j *Tabnas) resolveGrammarAlt(ga *GrammarAltSpec, ref map[FuncRef]any) (*AltSpec, error) {
	alt := &AltSpec{}

	// Resolve S (token spec: string or []string → [][]Tin)
	if ga.S != nil {
		alt.S = j.resolveTokenField(ga.S)
	}

	// Resolve B (backtrack: int or FuncRef)
	switch v := ga.B.(type) {
	case int:
		alt.B = v
	case float64:
		alt.B = int(v)
	case string:
		fn, err := RequireRef(ref, v, "backtrack")
		if err != nil {
			return nil, err
		}
		if bf, ok := fn.(func(*Rule, *Context) int); ok {
			alt.BF = bf
		} else {
			return nil, fmt.Errorf("Grammar: ref %q is not a backtrack function", v)
		}
	}

	// Resolve P (push: rule name or FuncRef)
	if ga.P != "" {
		if IsFuncRef(ga.P) {
			fn, err := RequireRef(ref, ga.P, "push")
			if err != nil {
				return nil, err
			}
			if pf, ok := fn.(func(*Rule, *Context) string); ok {
				alt.PF = pf
			} else {
				return nil, fmt.Errorf("Grammar: ref %q is not a push function", ga.P)
			}
		} else {
			alt.P = ga.P
		}
	}

	// Resolve R (replace: rule name or FuncRef)
	if ga.R != "" {
		if IsFuncRef(ga.R) {
			fn, err := RequireRef(ref, ga.R, "replace")
			if err != nil {
				return nil, err
			}
			if rf, ok := fn.(func(*Rule, *Context) string); ok {
				alt.RF = rf
			} else {
				return nil, fmt.Errorf("Grammar: ref %q is not a replace function", ga.R)
			}
		} else {
			alt.R = ga.R
		}
	}

	// Resolve A (action): a FuncRef string, or an array of refs/AltActions
	// run in order (matched alt's own action first, then composed user
	// actions), short-circuiting if a prior action sets ctx.ParseErr.
	if action, err := resolveActionField(ga.A, ref); err != nil {
		return nil, err
	} else if action != nil {
		alt.A = action
	}

	// Resolve E (error)
	if ga.E != "" {
		fn, err := RequireRef(ref, ga.E, "error")
		if err != nil {
			return nil, err
		}
		if ef, ok := fn.(AltError); ok {
			alt.E = ef
		} else {
			return nil, fmt.Errorf("Grammar: ref %q is not an AltError", ga.E)
		}
	}

	// Resolve H (modifier)
	if ga.H != "" {
		fn, err := RequireRef(ref, ga.H, "modifier")
		if err != nil {
			return nil, err
		}
		if hf, ok := fn.(AltModifier); ok {
			alt.H = hf
		} else {
			return nil, fmt.Errorf("Grammar: ref %q is not an AltModifier", ga.H)
		}
	}

	// Resolve C (condition: FuncRef or declarative map)
	switch cv := ga.C.(type) {
	case string:
		fn, err := RequireRef(ref, cv, "condition")
		if err != nil {
			return nil, err
		}
		if cf, ok := fn.(AltCond); ok {
			alt.C = cf
		} else {
			return nil, fmt.Errorf("Grammar: ref %q is not an AltCond", cv)
		}
	case map[string]any:
		alt.CD = cv
	}

	// Copy simple fields
	if ga.N != nil {
		alt.N = make(map[string]int, len(ga.N))
		for k, v := range ga.N {
			alt.N[k] = v
		}
	}
	if ga.U != nil {
		alt.U = make(map[string]any, len(ga.U))
		for k, v := range ga.U {
			alt.U[k] = v
		}
	}
	if ga.K != nil {
		alt.K = make(map[string]any, len(ga.K))
		for k, v := range ga.K {
			alt.K[k] = v
		}
	}
	alt.G = ga.G

	// Normalize declarative conditions and validate group tags.
	if err := NormAlt(alt); err != nil {
		return nil, err
	}

	return alt, nil
}

// resolveTokenField resolves the S field of a GrammarAltSpec.
// Accepts string or []string.
//
//	string:     "#KEY #CL" — each space-separated name is a separate slot.
//	[]string:   ["#CB #CS"] — each element is a slot; within an element,
//	            space-separated names are alternatives for that slot.
func (j *Tabnas) resolveTokenField(s any) [][]Tin {
	switch v := s.(type) {
	case string:
		if v == "" {
			return nil
		}
		return j.resolveTokenSpec(v)
	case []string:
		result := make([][]Tin, len(v))
		for i, slot := range v {
			var tins []Tin
			for _, name := range strings.Fields(slot) {
				tins = append(tins, j.resolveTokenName(name)...)
			}
			result[i] = tins
		}
		return result
	}
	return nil
}

// resolveTokenSpec resolves a token spec string into [][]Tin.
// Each space-separated name becomes a separate slot.
func (j *Tabnas) resolveTokenSpec(s string) [][]Tin {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return nil
	}
	result := make([][]Tin, len(parts))
	for i, part := range parts {
		result[i] = j.resolveTokenName(part)
	}
	return result
}

// resolveTokenName resolves a single token name (like "#OB" or "#KEY") to a []Tin.
func (j *Tabnas) resolveTokenName(name string) []Tin {
	setName := strings.TrimPrefix(name, "#")
	if tins := j.TokenSet(setName); tins != nil {
		return tins
	}
	tin := j.Token(name)
	return []Tin{tin}
}

// wireStateActions auto-wires reserved FuncRef names to state action slices.
// Names: @{rulename}-bo, @{rulename}-ao, @{rulename}-bc, @{rulename}-ac
// Variants: /prepend prepends, /append or plain appends.
//
// Dedupe by function identity per phase: registering the same StateAction
// twice (directly, or via a later Grammar() call that also passes it)
// installs only one action, but distinct functions for the same phase all
// install. Mirrors the TypeScript fnref behaviour — accommodates grammars
// that layer handlers while preventing unrelated Grammar() calls from
// re-stacking previously-registered reserved handlers.
func wireStateActions(rs *RuleSpec, ref map[FuncRef]any) {
	type target struct {
		suffix string
		dest   *[]StateAction
	}
	targets := []target{
		{"bo", &rs.bo},
		{"ao", &rs.ao},
		{"bc", &rs.bc},
		{"ac", &rs.ac},
	}
	if rs.fnrefInstalled == nil {
		rs.fnrefInstalled = map[string]map[uintptr]bool{}
	}
	if rs.fnrefReplaced == nil {
		rs.fnrefReplaced = map[string]bool{}
	}
	for _, t := range targets {
		base := "@" + rs.Name + "-" + t.suffix
		phaseSet, ok := rs.fnrefInstalled[base]
		if !ok {
			phaseSet = map[uintptr]bool{}
			rs.fnrefInstalled[base] = phaseSet
		}

		// `/replace` clears all prior actions for this phase (from any
		// plugin) and installs the replacement, then owns the phase: once
		// replaced, the plain/prepend/append fnrefs for it are ignored.
		if rf, present := ref[base+"/replace"]; present && rf != nil {
			if sa, ok := rf.(StateAction); ok {
				if !rs.fnrefReplaced[base] {
					rs.fnrefReplaced[base] = true
					*t.dest = nil
					phaseSet = map[uintptr]bool{}
					rs.fnrefInstalled[base] = phaseSet
					phaseSet[reflect.ValueOf(sa).Pointer()] = true
					*t.dest = append(*t.dest, sa)
				}
				continue
			}
		}
		if rs.fnrefReplaced[base] {
			continue
		}

		// Install /prepend, then /append-or-plain. Matching TS
		// (`fr[base+'/append'] ?? fr[base]`), `/append` and the plain name
		// are the SAME slot: providing both installs one (the /append
		// entry wins). Dedupe by function pointer so the same StateAction
		// isn't wired twice across Grammar() calls. (Go has no per-closure
		// identity, so distinct closures sharing a code pointer dedupe as
		// one — reuse stable ref values across calls, as grammars do.)
		install := func(key string, doAppend bool) {
			fn, present := ref[key]
			if !present || fn == nil {
				return
			}
			sa, ok := fn.(StateAction)
			if !ok {
				return
			}
			ptr := reflect.ValueOf(sa).Pointer()
			if phaseSet[ptr] {
				return
			}
			phaseSet[ptr] = true
			if doAppend {
				*t.dest = append(*t.dest, sa)
			} else {
				*t.dest = append([]StateAction{sa}, *t.dest...)
			}
		}

		install(base+"/prepend", false)
		appendKey := base + "/append"
		if _, ok := ref[appendKey]; !ok {
			appendKey = base // '/append' ?? plain — one slot
		}
		install(appendKey, true)
	}
}

// builtinTins maps standard token names to their Tin values.
var builtinTins = map[string]Tin{
	"#BD": TinBD, "#ZZ": TinZZ, "#UK": TinUK, "#AA": TinAA,
	"#SP": TinSP, "#LN": TinLN, "#CM": TinCM, "#NR": TinNR,
	"#ST": TinST, "#TX": TinTX, "#VL": TinVL, "#OB": TinOB,
	"#CB": TinCB, "#OS": TinOS, "#CS": TinCS, "#CL": TinCL,
	"#CA": TinCA,
}

// builtinTokenSets maps standard token set names to their Tin slices.
var builtinTokenSets = map[string][]Tin{
	"VAL": TinSetVAL,
	"KEY": TinSetKEY,
}

// resolveTokenFieldStatic resolves a string or []string S field using built-in tokens.
func resolveTokenFieldStatic(s any) [][]Tin {
	switch v := s.(type) {
	case string:
		if v == "" {
			return nil
		}
		return resolveTokenSpecStatic(v)
	case []string:
		result := make([][]Tin, len(v))
		for i, slot := range v {
			var tins []Tin
			for _, name := range strings.Fields(slot) {
				tins = append(tins, resolveTokenNameStatic(name)...)
			}
			result[i] = tins
		}
		return result
	}
	return nil
}

// resolveTokenSpecStatic resolves a token spec string using built-in tokens only.
func resolveTokenSpecStatic(s string) [][]Tin {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return nil
	}
	result := make([][]Tin, len(parts))
	for i, part := range parts {
		result[i] = resolveTokenNameStatic(part)
	}
	return result
}

func resolveTokenNameStatic(name string) []Tin {
	setName := strings.TrimPrefix(name, "#")
	if tins, ok := builtinTokenSets[setName]; ok {
		result := make([]Tin, len(tins))
		copy(result, tins)
		return result
	}
	if tin, ok := builtinTins[name]; ok {
		return []Tin{tin}
	}
	// Unknown tokens in static context are programming errors in the internal grammar.
	// Return empty slice rather than panicking.
	return nil
}

// ResolveGrammarAltStatic converts a GrammarAltSpec to a concrete AltSpec
// using only built-in token resolution. Used by the internal Grammar().
// Errors cause the returned alt to have nil fields (best-effort).
func ResolveGrammarAltStatic(ga *GrammarAltSpec, ref map[FuncRef]any) *AltSpec {
	alt := &AltSpec{}

	if ga.S != nil {
		alt.S = resolveTokenFieldStatic(ga.S)
	}

	switch v := ga.B.(type) {
	case int:
		alt.B = v
	case float64:
		alt.B = int(v)
	case string:
		if fn := LookupRef(ref, v); fn != nil {
			if bf, ok := fn.(func(*Rule, *Context) int); ok {
				alt.BF = bf
			}
		}
	}

	if ga.P != "" {
		if IsFuncRef(ga.P) {
			if fn := LookupRef(ref, ga.P); fn != nil {
				if pf, ok := fn.(func(*Rule, *Context) string); ok {
					alt.PF = pf
				}
			}
		} else {
			alt.P = ga.P
		}
	}

	if ga.R != "" {
		if IsFuncRef(ga.R) {
			if fn := LookupRef(ref, ga.R); fn != nil {
				if rf, ok := fn.(func(*Rule, *Context) string); ok {
					alt.RF = rf
				}
			}
		} else {
			alt.R = ga.R
		}
	}

	if action, err := resolveActionField(ga.A, ref); err == nil && action != nil {
		alt.A = action
	}
	if ga.E != "" {
		if fn := LookupRef(ref, ga.E); fn != nil {
			alt.E = fn.(AltError)
		}
	}
	if ga.H != "" {
		if fn := LookupRef(ref, ga.H); fn != nil {
			alt.H = fn.(AltModifier)
		}
	}

	switch cv := ga.C.(type) {
	case string:
		if fn := LookupRef(ref, cv); fn != nil {
			alt.C = fn.(AltCond)
		}
	case map[string]any:
		alt.CD = cv
	}

	if ga.N != nil {
		alt.N = ga.N
	}
	if ga.U != nil {
		alt.U = ga.U
	}
	if ga.K != nil {
		alt.K = ga.K
	}
	alt.G = ga.G

	// Internal grammar is constructed from trusted tag literals, so a
	// validation error here indicates a programming bug rather than bad
	// user input. Ignore the error — it will surface if a library author
	// ever adds a malformed tag, via the regular Grammar/GrammarText path.
	_ = NormAlt(alt)
	return alt
}
