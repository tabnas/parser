// Copyright (c) 2013-2026 Richard Rodger, MIT License

// merge.go implements (*Tabnas).Merge: combining two parser instances
// into a new instance carrying both grammars. Mirrors ts/src/merge.ts
// (TS is canonical): options merge commutatively with conflict
// detection, custom tokens are translated by name into the merged
// instance's Tin space, shared rules interleave their alternates
// deterministically, and the operation is commutative — a.Merge(b)
// and b.Merge(a) produce instances with the same options, rule
// alternates (in the same order), and parse behavior.
//
// Go difference from TS: the Go RuleSpec persists no fnref map
// (Grammar() Ref maps are transient, only wiring state actions), so
// there are no named-action keys to rename with tag prefixes; the
// already-wired lifecycle action slices are carried instead. See
// doc/differences.md.

package tabnas

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

var regexpPtrType = reflect.TypeOf((*regexp.Regexp)(nil))

// funcAwareEqual compares two reflect values like reflect.DeepEqual,
// except functions compare by pointer identity (DeepEqual treats any
// non-nil functions as unequal) and regexps compare by pattern.
func funcAwareEqual(a, b reflect.Value) bool {
	if !a.IsValid() || !b.IsValid() {
		return a.IsValid() == b.IsValid()
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Func:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		return a.Pointer() == b.Pointer()
	case reflect.Ptr:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		if a.Type() == regexpPtrType {
			return a.Interface().(*regexp.Regexp).String() ==
				b.Interface().(*regexp.Regexp).String()
		}
		return funcAwareEqual(a.Elem(), b.Elem())
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		return funcAwareEqual(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !funcAwareEqual(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.Len() != b.Len() {
			return false
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() || !funcAwareEqual(a.MapIndex(k), bv) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if a.Kind() == reflect.Slice && (a.IsNil() != b.IsNil()) {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !funcAwareEqual(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.String:
		return a.String() == b.String()
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() == b.Float()
	default:
		return false
	}
}

// mergeReflect merges one option value pair; zero (nil pointer, empty
// string, etc.) means "default" per the Options convention, so a zero
// side always loses. Both sides non-zero and unequal is a conflict.
// Paths use lowercased field names ("rule.maxmul") plus map keys.
func mergeReflect(av, bv reflect.Value, path string) (reflect.Value, error) {
	if !av.IsValid() || av.IsZero() {
		return bv, nil
	}
	if !bv.IsValid() || bv.IsZero() {
		return av, nil
	}

	switch av.Kind() {
	case reflect.Ptr:
		if av.Type() != regexpPtrType && av.Type().Elem().Kind() == reflect.Struct {
			merged, err := mergeReflect(av.Elem(), bv.Elem(), path)
			if err != nil {
				return av, err
			}
			out := reflect.New(av.Type().Elem())
			out.Elem().Set(merged)
			return out, nil
		}

	case reflect.Struct:
		out := reflect.New(av.Type()).Elem()
		t := av.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				return av, fmt.Errorf(
					"merge: cannot merge option struct %s (unexported fields)", path)
			}
			fpath := strings.ToLower(f.Name)
			if path != "" {
				fpath = path + "." + fpath
			}
			merged, err := mergeReflect(av.Field(i), bv.Field(i), fpath)
			if err != nil {
				return av, err
			}
			if merged.IsValid() {
				out.Field(i).Set(merged)
			}
		}
		return out, nil

	case reflect.Map:
		out := reflect.MakeMap(av.Type())
		keys := av.MapKeys()
		for _, k := range bv.MapKeys() {
			if !av.MapIndex(k).IsValid() {
				keys = append(keys, k)
			}
		}
		for _, k := range keys {
			kpath := path + "." + fmt.Sprintf("%v", k.Interface())
			avv := av.MapIndex(k)
			bvv := bv.MapIndex(k)
			switch {
			case !avv.IsValid():
				out.SetMapIndex(k, bvv)
			case !bvv.IsValid():
				out.SetMapIndex(k, avv)
			default:
				merged, err := mergeReflect(avv, bvv, kpath)
				if err != nil {
					return av, err
				}
				out.SetMapIndex(k, merged)
			}
		}
		return out, nil
	}

	if funcAwareEqual(av, bv) {
		return av, nil
	}
	return av, fmt.Errorf("merge: conflicting option values at %s", path)
}

// mergeOptionsCommutative merges two Options values symmetrically:
// nil/zero fields mean "default" and always lose; fields set to
// different values on both sides error with the option path. Tag is
// excluded (the caller computes the merged tag).
func mergeOptionsCommutative(a, b *Options) (Options, error) {
	ac := *a
	bc := *b
	ac.Tag = ""
	bc.Tag = ""
	merged, err := mergeReflect(
		reflect.ValueOf(ac),
		reflect.ValueOf(bc),
		"",
	)
	if err != nil {
		return Options{}, err
	}
	return merged.Interface().(Options), nil
}

// portableAlt is a rule alternate made independent of its source
// instance: S is carried as token names, with sort metadata for the
// interleave comparator.
type portableAlt struct {
	alt   AltSpec    // Value copy with S nil'd (names carry the sequence).
	names [][]string // Per-position token names, original order.
	keys  []string   // Canonical per-position keys (sorted, joined names).
	comp  []int      // Complexity presence vector.
	gkey  string     // Sorted, joined group tags.
	tag   string     // Source instance tag (final tie-break).
}

// mergeRuleRecord is the portable form of one rule from one side.
type mergeRuleRecord struct {
	bo, ao, bc, ac []StateAction
	open, close    []portableAlt
}

func fnPtr(fn any) uintptr {
	v := reflect.ValueOf(fn)
	if !v.IsValid() || v.IsNil() {
		return 0
	}
	return v.Pointer()
}

// makePortable translates one alt out of its source Tin space.
func makePortable(alt *AltSpec, side *Tabnas, tag string) portableAlt {
	names := make([][]string, len(alt.S))
	keys := make([]string, len(alt.S))
	for i, tins := range alt.S {
		posNames := make([]string, len(tins))
		for k, tin := range tins {
			posNames[k] = side.TinName(tin)
		}
		names[i] = posNames
		sorted := append([]string{}, posNames...)
		sort.Strings(sorted)
		keys[i] = strings.Join(sorted, " ")
	}

	gtags := splitGroupTags(alt.G)
	sort.Strings(gtags)

	clone := *alt
	clone.S = nil
	clone.N = copyIntMap(alt.N)
	clone.U = copyAnyMap(alt.U)
	clone.K = copyAnyMap(alt.K)
	clone.CD = copyAnyMap(alt.CD)

	// Complexity presence vector: condition, error, modifier,
	// backtrack, counters, action, custom props, propagated props,
	// push, replace — more complex sorts first on identical sequences.
	comp := []int{
		b2i(alt.C != nil || alt.CD != nil),
		b2i(alt.E != nil),
		b2i(alt.H != nil),
		b2i(alt.B != 0 || alt.BF != nil),
		len(alt.N),
		b2i(alt.A != nil),
		b2i(len(alt.U) > 0),
		b2i(len(alt.K) > 0),
		b2i(alt.P != "" || alt.PF != nil),
		b2i(alt.R != "" || alt.RF != nil),
	}

	return portableAlt{
		alt: clone, names: names, keys: keys,
		comp: comp, gkey: strings.Join(gtags, ","), tag: tag,
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func copyIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// compareMergeAlts is the interleave comparator (<0 means a first):
// first differing position by token-name order; a shared prefix sorts
// the longer sequence first (empty-S catch-alls last); identical
// sequences order by complexity (more complex first) then group tags;
// the source tag breaks any remaining tie, so the order is total and
// direction-independent.
func compareMergeAlts(a, b portableAlt) int {
	n := len(a.keys)
	if len(b.keys) < n {
		n = len(b.keys)
	}
	for i := 0; i < n; i++ {
		if a.keys[i] != b.keys[i] {
			if a.keys[i] < b.keys[i] {
				return -1
			}
			return 1
		}
	}
	if len(a.keys) != len(b.keys) {
		return len(b.keys) - len(a.keys)
	}
	for i := 0; i < len(a.comp); i++ {
		if d := b.comp[i] - a.comp[i]; d != 0 {
			return d
		}
	}
	if a.gkey != b.gkey {
		if a.gkey < b.gkey {
			return -1
		}
		return 1
	}
	if a.tag != b.tag {
		if a.tag < b.tag {
			return -1
		}
		return 1
	}
	return 0
}

// identicalMergeAlts reports whether two alts from different sides are
// the same alt (shared-base-plugin case): same token keys and group
// tags, behavior fields equal by function identity, data props equal
// by value. Such pairs are emitted once.
//
// Unlike TS (where function reference identity is exact), Go closures
// built from the same literal share one code pointer even when they
// capture different environments — so pointer equality alone could
// conflate condition closures that behave differently. Dedupe is
// therefore restricted to unconditioned alts: with no condition, the
// first of two identical-sequence alts always wins the match and the
// second is unreachable, making the dedupe behavior-neutral.
func identicalMergeAlts(a, b portableAlt) bool {
	if a.alt.C != nil || a.alt.CD != nil || b.alt.C != nil || b.alt.CD != nil {
		return false
	}
	if len(a.keys) != len(b.keys) || a.gkey != b.gkey {
		return false
	}
	for i := range a.keys {
		if a.keys[i] != b.keys[i] {
			return false
		}
	}
	return fnPtr(a.alt.A) == fnPtr(b.alt.A) &&
		fnPtr(a.alt.H) == fnPtr(b.alt.H) &&
		fnPtr(a.alt.E) == fnPtr(b.alt.E) &&
		fnPtr(a.alt.PF) == fnPtr(b.alt.PF) &&
		fnPtr(a.alt.RF) == fnPtr(b.alt.RF) &&
		fnPtr(a.alt.BF) == fnPtr(b.alt.BF) &&
		a.alt.B == b.alt.B &&
		a.alt.P == b.alt.P &&
		a.alt.R == b.alt.R &&
		reflect.DeepEqual(a.alt.N, b.alt.N) &&
		reflect.DeepEqual(a.alt.U, b.alt.U) &&
		reflect.DeepEqual(a.alt.K, b.alt.K)
}

// interleaveAlts merges the two per-rule alt lists with a two-pointer
// walk: each list stays internally stable, the comparator only decides
// interleaving across lists. Dedupe runs first, position-independently:
// an alt of y identical to ANY alt of x is dropped (x is the canonical
// smaller-tag side, so the outcome is direction-independent) —
// head-only comparison would miss shared-base alts shifted by each
// side's own additions.
func interleaveAlts(xs, ys []portableAlt) []portableAlt {
	yy := make([]portableAlt, 0, len(ys))
	for _, y := range ys {
		dup := false
		for _, x := range xs {
			if identicalMergeAlts(x, y) {
				dup = true
				break
			}
		}
		if !dup {
			yy = append(yy, y)
		}
	}

	out := make([]portableAlt, 0, len(xs)+len(yy))
	i, j := 0, 0
	for i < len(xs) && j < len(yy) {
		if compareMergeAlts(xs[i], yy[j]) <= 0 {
			out = append(out, xs[i])
			i++
		} else {
			out = append(out, yy[j])
			j++
		}
	}
	out = append(out, xs[i:]...)
	out = append(out, yy[j:]...)
	return out
}

// concatStateActions concatenates lifecycle actions in canonical (tag)
// order, deduping by function identity so a handler shared via a
// common base plugin installs once.
func concatStateActions(xs, ys []StateAction) []StateAction {
	out := append([]StateAction{}, xs...)
	for _, fy := range ys {
		dup := false
		for _, fx := range out {
			if fnPtr(fx) == fnPtr(fy) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, fy)
		}
	}
	return out
}

// mergeRuleRecords extracts one side's rules in portable form.
func mergeRuleRecords(side *Tabnas, tag string) map[string]*mergeRuleRecord {
	out := make(map[string]*mergeRuleRecord, len(side.parser.RSM))
	for name, rs := range side.parser.RSM {
		rec := &mergeRuleRecord{
			bo: append([]StateAction{}, rs.bo...),
			ao: append([]StateAction{}, rs.ao...),
			bc: append([]StateAction{}, rs.bc...),
			ac: append([]StateAction{}, rs.ac...),
		}
		for _, alt := range rs.open {
			rec.open = append(rec.open, makePortable(alt, side, tag))
		}
		for _, alt := range rs.close {
			rec.close = append(rec.close, makePortable(alt, side, tag))
		}
		out[name] = rec
	}
	return out
}

// resolveMergeAlt rebuilds a portable alt inside the target instance,
// re-resolving token names in its Tin space. Fresh copies per call:
// the synthetic plugin re-runs on Derive children, which must not
// share alt structs with the parent.
func resolveMergeAlt(t *Tabnas, pa portableAlt) *AltSpec {
	alt := pa.alt
	alt.N = copyIntMap(pa.alt.N)
	alt.U = copyAnyMap(pa.alt.U)
	alt.K = copyAnyMap(pa.alt.K)
	alt.CD = copyAnyMap(pa.alt.CD)
	if len(pa.names) > 0 {
		S := make([][]Tin, len(pa.names))
		for i, posNames := range pa.names {
			tins := make([]Tin, len(posNames))
			for k, n := range posNames {
				tins[k] = t.Token(n)
			}
			S[i] = tins
		}
		alt.S = S
	}
	return &alt
}

// mergeCustomTokens unifies the two sides' custom token and fixed
// token state (which lives on the instance, not in Options) into the
// merged instance, translating by NAME with sorted iteration so Tin
// allocation is deterministic. A token name mapped to two different
// non-default sources, or one source claimed by two names, is an
// error.
func mergeCustomTokens(out, x, y *Tabnas) error {
	// Default fixed sources by name (the global builtin table).
	defaultByName := make(map[string]string, len(FixedTokens))
	for src, tin := range FixedTokens {
		defaultByName[tinName(tin)] = src
	}

	fixedByName := func(side *Tabnas) map[string]string {
		m := make(map[string]string)
		for src, tin := range side.parser.Config.FixedTokens {
			m[side.TinName(tin)] = src
		}
		return m
	}
	fx := fixedByName(x)
	fy := fixedByName(y)

	// Resolve the union name→src table, defaults losing to overrides.
	srcByName := make(map[string]string, len(fx)+len(fy))
	names := make([]string, 0, len(fx)+len(fy))
	for name := range fx {
		names = append(names, name)
	}
	for name := range fy {
		if _, ok := fx[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		sx, okx := fx[name]
		sy, oky := fy[name]
		switch {
		case okx && oky && sx != sy:
			def := defaultByName[name]
			switch {
			case sx == def:
				srcByName[name] = sy
			case sy == def:
				srcByName[name] = sx
			default:
				return fmt.Errorf(
					"merge: fixed token %s maps to both %q and %q",
					name, sx, sy)
			}
		case okx:
			srcByName[name] = sx
		default:
			srcByName[name] = sy
		}
	}

	// One source string may serve only one token name.
	nameBySrc := make(map[string]string, len(srcByName))
	for _, name := range names {
		src := srcByName[name]
		if prev, ok := nameBySrc[src]; ok && prev != name {
			return fmt.Errorf(
				"merge: fixed tokens %s and %s both claim source %q",
				prev, name, src)
		}
		nameBySrc[src] = name
	}

	// Allocate every custom (non-builtin) token name first, in sorted
	// order, so the merged Tin space is deterministic.
	customSet := make(map[string]bool)
	for _, side := range []*Tabnas{x, y} {
		for name, tin := range side.tinByName {
			if tin >= TinMAX {
				customSet[name] = true
			}
		}
	}
	customNames := make([]string, 0, len(customSet))
	for name := range customSet {
		customNames = append(customNames, name)
	}
	sort.Strings(customNames)
	for _, name := range customNames {
		out.Token(name)
	}

	// Apply the fixed source table: clear stale sources for each
	// remapped token, then bind the resolved source.
	cfg := out.parser.Config
	for _, name := range names {
		src := srcByName[name]
		tin := out.Token(name)
		for s, t := range cfg.FixedTokens {
			if t == tin && s != src {
				delete(cfg.FixedTokens, s)
			}
		}
		cfg.FixedTokens[src] = tin
	}
	cfg.SortFixedTokens()

	return nil
}

// mergeTokenSets translates both sides' custom token sets into the
// merged instance. Sets defined on both sides must agree (by token
// name); Options.TokenSet-driven sets land here too, since SetOptions
// materializes them via SetTokenSet.
func mergeTokenSets(out, x, y *Tabnas) error {
	setNames := make(map[string]bool)
	for name := range x.customTokenSets {
		setNames[name] = true
	}
	for name := range y.customTokenSets {
		setNames[name] = true
	}
	sorted := make([]string, 0, len(setNames))
	for name := range setNames {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	tinsToNames := func(side *Tabnas, tins []Tin) []string {
		names := make([]string, len(tins))
		for i, tin := range tins {
			names[i] = side.TinName(tin)
		}
		return names
	}

	for _, name := range sorted {
		xt, okx := x.customTokenSets[name]
		yt, oky := y.customTokenSets[name]
		var members []string
		switch {
		case okx && oky:
			xn := tinsToNames(x, xt)
			yn := tinsToNames(y, yt)
			xs := append([]string{}, xn...)
			ys := append([]string{}, yn...)
			sort.Strings(xs)
			sort.Strings(ys)
			if !reflect.DeepEqual(xs, ys) {
				return fmt.Errorf(
					"merge: conflicting token set %q: [%s] vs [%s]",
					name, strings.Join(xn, " "), strings.Join(yn, " "))
			}
			members = xn
		case okx:
			members = tinsToNames(x, xt)
		default:
			members = tinsToNames(y, yt)
		}
		tins := make([]Tin, len(members))
		for i, n := range members {
			tins[i] = out.Token(n)
		}
		out.SetTokenSet(name, tins)
	}
	return nil
}

// mergeNamedAny key-unions two name→value maps (decorations, plugin
// options), erroring when both sides bind the same name to different
// values.
func mergeNamedAny(kind string, xm, ym map[string]any) (map[string]any, error) {
	if xm == nil && ym == nil {
		return nil, nil
	}
	out := make(map[string]any, len(xm)+len(ym))
	for k, v := range xm {
		out[k] = v
	}
	for k, v := range ym {
		if prev, ok := out[k]; ok {
			if !funcAwareEqual(reflect.ValueOf(prev), reflect.ValueOf(v)) {
				return nil, fmt.Errorf(
					"merge: conflicting %s %q", kind, k)
			}
			continue
		}
		out[k] = v
	}
	return out, nil
}

// Merge combines this instance's grammar with another's, returning a
// new instance; both originals are unmodified. Commutative: options
// are conflict-checked rather than overridden, shared rules interleave
// their alternates deterministically (token-name order at the first
// differing position; a longer sequence beats its own prefix;
// identical sequences order by complexity then group tags), and
// custom tokens are unified by name. Both instances must have
// distinct, non-empty Tag options; the result's tag is the sorted
// join (e.g. "A~B").
func (j *Tabnas) Merge(other *Tabnas) (result *Tabnas, err error) {
	// Merge runs arbitrary plugin-adjacent machinery; a panic becomes
	// an "internal" error per the no-panic guarantee.
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = j.internalError("Merge", r)
		}
	}()

	if other == nil {
		return nil, fmt.Errorf("merge: the second instance is nil")
	}
	tagJ := j.options.Tag
	tagO := other.options.Tag
	if tagJ == "" {
		return nil, fmt.Errorf(
			"merge: the first instance needs a Tag option " +
				"(used to identify its grammar)")
	}
	if tagO == "" {
		return nil, fmt.Errorf(
			"merge: the second instance needs a Tag option " +
				"(used to identify its grammar)")
	}
	if tagJ == tagO {
		return nil, fmt.Errorf(
			"merge: instance tags must differ, both are %q", tagJ)
	}

	// Canonical order by tag: everything below is independent of
	// which instance was the receiver.
	x, y := j, other
	tagX, tagY := tagJ, tagO
	if tagY < tagX {
		x, y = y, x
		tagX, tagY = tagY, tagX
	}

	mergedOpts, err := mergeOptionsCommutative(x.options, y.options)
	if err != nil {
		return nil, err
	}
	mergedOpts.Tag = tagX + "~" + tagY

	out := Make(mergedOpts)

	if err = mergeCustomTokens(out, x, y); err != nil {
		return nil, err
	}
	if err = mergeTokenSets(out, x, y); err != nil {
		return nil, err
	}

	if out.decorations, err = mergeNamedAny(
		"decoration", x.decorations, y.decorations); err != nil {
		return nil, err
	}
	pluginOpts := make(map[string]map[string]any)
	pluginNames := make(map[string]bool)
	for name := range x.pluginOpts {
		pluginNames[name] = true
	}
	for name := range y.pluginOpts {
		pluginNames[name] = true
	}
	for name := range pluginNames {
		merged, perr := mergeNamedAny(
			"plugin option for "+name, x.pluginOpts[name], y.pluginOpts[name])
		if perr != nil {
			return nil, perr
		}
		pluginOpts[name] = merged
	}
	if len(pluginOpts) > 0 {
		out.pluginOpts = pluginOpts
	}

	// Portable rule records, interleaved per rule and phase.
	xRecords := mergeRuleRecords(x, tagX)
	yRecords := mergeRuleRecords(y, tagY)
	nameSet := make(map[string]bool, len(xRecords)+len(yRecords))
	for name := range xRecords {
		nameSet[name] = true
	}
	for name := range yRecords {
		nameSet[name] = true
	}
	ruleNames := make([]string, 0, len(nameSet))
	for name := range nameSet {
		ruleNames = append(ruleNames, name)
	}
	sort.Strings(ruleNames)

	emptyRec := &mergeRuleRecord{}
	records := make(map[string]*mergeRuleRecord, len(ruleNames))
	for _, name := range ruleNames {
		xr, yr := xRecords[name], yRecords[name]
		if xr == nil {
			xr = emptyRec
		}
		if yr == nil {
			yr = emptyRec
		}
		records[name] = &mergeRuleRecord{
			bo:    concatStateActions(xr.bo, yr.bo),
			ao:    concatStateActions(xr.ao, yr.ao),
			bc:    concatStateActions(xr.bc, yr.bc),
			ac:    concatStateActions(xr.ac, yr.ac),
			open:  interleaveAlts(xr.open, yr.open),
			close: interleaveAlts(xr.close, yr.close),
		}
	}

	// Synthetic plugin carrying the merged grammar. Installing rules
	// via a plugin keeps Derive working: children rebuild their rules
	// by re-running plugins, resolving token names afresh.
	mergedGrammar := func(t *Tabnas, _ map[string]any) error {
		for _, name := range ruleNames {
			rec := records[name]
			t.Rule(name, func(rs *RuleSpec, _ *Parser) {
				rs.bo = append(rs.bo, rec.bo...)
				rs.ao = append(rs.ao, rec.ao...)
				rs.bc = append(rs.bc, rec.bc...)
				rs.ac = append(rs.ac, rec.ac...)
				for _, pa := range rec.open {
					rs.open = append(rs.open, resolveMergeAlt(t, pa))
				}
				for _, pa := range rec.close {
					rs.close = append(rs.close, resolveMergeAlt(t, pa))
				}
			})
			if err := NormAlts(t.parser.RSM[name]); err != nil {
				return err
			}
		}
		return nil
	}
	if err = out.Use(mergedGrammar); err != nil {
		return nil, err
	}

	// Event subscribers carry over too, in canonical order.
	out.lexSubs = append(out.lexSubs, x.lexSubs...)
	out.lexSubs = append(out.lexSubs, y.lexSubs...)
	out.ruleSubs = append(out.ruleSubs, x.ruleSubs...)
	out.ruleSubs = append(out.ruleSubs, y.ruleSubs...)

	return out, nil
}
