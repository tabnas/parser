// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
)

// groupTagRe is the regex every g tag must match: a lowercase letter
// followed by one or more lowercase letters, digits, or hyphens.
// Validated by NormAlt (and, transitively, by Grammar/GrammarText).
var groupTagRe = regexp.MustCompile(`^[a-z][a-z0-9-]+$`)

// ValidateGroupTags returns an error if any tag in the supplied
// comma-separated string fails the group-tag regex.
func ValidateGroupTags(g string) error {
	if g == "" {
		return nil
	}
	for _, tag := range strings.Split(g, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if !groupTagRe.MatchString(tag) {
			return fmt.Errorf("Grammar: invalid group tag %q — must match %s", tag, groupTagRe)
		}
	}
	return nil
}

// RuleState represents whether a rule is in open or close state.
type RuleState = string

const (
	OPEN  RuleState = "o"
	CLOSE RuleState = "c"
)

// undefinedType is the type of the Undefined sentinel, distinguishing "no value" from nil/null.
type undefinedType struct{}

var Undefined any = &undefinedType{}

// IsUndefined checks if a value is the Undefined sentinel.
func IsUndefined(v any) bool {
	_, ok := v.(*undefinedType)
	return ok
}

// skipType is the type of the Skip sentinel, which preserves the base value in deep merge ("@SKIP" in grammar options).
type skipType struct{}

var Skip any = &skipType{}

// IsSkip checks if a value is the Skip sentinel.
func IsSkip(v any) bool {
	_, ok := v.(*skipType)
	return ok
}

// UnwrapUndefined converts Undefined sentinels to nil in the result.
func UnwrapUndefined(v any) any {
	if IsUndefined(v) {
		return nil
	}
	switch val := v.(type) {
	case *OrderedMap:
		for _, k := range val.Keys {
			val.Vals[k] = UnwrapUndefined(val.Vals[k])
		}
		return val
	case map[string]any:
		for k, vv := range val {
			val[k] = UnwrapUndefined(vv)
		}
		return val
	case []any:
		for i, vv := range val {
			val[i] = UnwrapUndefined(vv)
		}
		return val
	}
	return v
}

// AltCond is a condition function for an alternate.
type AltCond func(r *Rule, ctx *Context) bool

// AltAction is an action function for an alternate.
type AltAction func(r *Rule, ctx *Context)

// AltError is an error function for an alternate.
type AltError func(r *Rule, ctx *Context) *Token

// AltModifier can modify an alt match result. Returns the (possibly modified) AltSpec.
type AltModifier func(alt *AltSpec, r *Rule, ctx *Context) *AltSpec

// StateAction is a before/after action on a rule state transition.
type StateAction func(r *Rule, ctx *Context)

// CondOp is a declarative comparison (operator + value) used in AltSpec.CD, e.g. { 'n.pk': { $lte: 0 } }.
//
// Val is `any` rather than `int` so a condition can compare what the TS port
// can: a counter or depth (numbers), but also a token id, a rule name, or a
// value parked in U/K (strings, bools). Existing callers are unaffected —
// CLte(0) still compiles, the literal just widens to any.
type CondOp struct {
	Op  string // Comparison operator ($eq, $ne, $lt, $lte, $gt, $gte, $exist).
	Val any    // Value to compare the rule property against ($exist: bool).
}

// Comparison operator constructors for declarative conditions (AltSpec.CD field).
func CEq(val any) CondOp  { return CondOp{Op: "$eq", Val: val} }
func CNe(val any) CondOp  { return CondOp{Op: "$ne", Val: val} }
func CLt(val any) CondOp  { return CondOp{Op: "$lt", Val: val} }
func CLte(val any) CondOp { return CondOp{Op: "$lte", Val: val} }
func CGt(val any) CondOp  { return CondOp{Op: "$gt", Val: val} }
func CGte(val any) CondOp { return CondOp{Op: "$gte", Val: val} }

// CExist matches on whether the counter was SET, regardless of its value.
// The comparisons read an unset counter as 0, so they cannot tell "never
// counted" from "counted zero"; this can. Mirrors the TS `$exist`.
func CExist(val bool) CondOp {
	if val {
		return CondOp{Op: "$exist", Val: 1}
	}
	return CondOp{Op: "$exist", Val: 0}
}

// AltSpec defines a parse alternate specification.
type AltSpec struct {
	S  [][]Tin                            // Per-position Tin sets to match: S[i] for lookahead token i (empty = wildcard)
	P  string                             // Push rule name (create child)
	R  string                             // Replace rule name (create sibling)
	B  int                                // Move token pointer backward (backtrack)
	C  AltCond                            // Custom condition (function)
	CD map[string]any                     // Declarative condition (converted to C by NormAlt)
	N  map[string]int                     // Counter increments
	A  AltAction                          // Match action
	U  map[string]any                     // Custom props added to Rule.U
	K  map[string]any                     // Custom props added to Rule.K (propagated)
	G  string                             // Named group tags (comma-separated)
	H  AltModifier                        // Alt modifier (called after match to potentially modify the alt)
	E  AltError                           // Error generation
	PF func(r *Rule, ctx *Context) string // Dynamic push rule name
	RF func(r *Rule, ctx *Context) string // Dynamic replace rule name
	BF func(r *Rule, ctx *Context) int    // Dynamic backtrack

	// SNames records the token NAMES declared for each S position, when the
	// alt was built from a name-based spec (Grammar / GrammarText /
	// ResolveGrammarAltStatic). Positions that name a token SET (#KEY, #VAL,
	// #IGNORE, or a custom set) are re-resolved against the instance doing
	// the parsing, so a per-instance options.tokenSet override takes effect
	// even for alts resolved before — or independently of — that override.
	// This is TS parity: rules.ts resolves `r.ji.tokenSet(n) ?? r.ji.token(n)`
	// at rule-normalisation time rather than freezing tins at registration.
	// nil when the alt was built from raw Tins (S is then authoritative).
	SNames [][]string
}

// ruleDefCounter stamps every RuleSpec with a monotonically increasing
// definition index. It is process-global (a RuleSpec can be built before it
// is attached to an instance), which makes the absolute values meaningless
// but the relative order within one instance exactly the registration order.
// Atomic so grammars built concurrently on separate instances stay race-free.
var ruleDefCounter atomic.Int64

// nextRuleDef returns the next definition index (1-based; 0 means "unstamped").
func nextRuleDef() int {
	return int(ruleDefCounter.Add(1))
}

// RuleSpec is the specification for a parsing rule; its alternate and action lists are unexported and mutated only via methods (mirroring the TS RuleSpec).
type RuleSpec struct {
	Name string // Rule name (key in the rule spec map).

	// Def is the rule's declaration order: a monotonically increasing index
	// stamped when the spec is first created ((*Tabnas).Rule, Grammar,
	// GrammarText, MakeRuleSpec). Rule specs sort by Def into the order the
	// grammar declared them — the Go engine's answer to the insertion order
	// a TS object literal keeps for free. Use (*Tabnas).RuleNames or
	// (*Tabnas).Rules rather than reading this directly. Zero means the spec
	// was built as a bare struct literal and carries no order.
	Def int

	open  []*AltSpec    // Open-phase alternates, tried in order.
	close []*AltSpec    // Close-phase alternates, tried in order.
	bo    []StateAction // Before-open actions.
	bc    []StateAction // Before-close actions.
	ao    []StateAction // After-open actions.
	ac    []StateAction // After-close actions.

	// fnrefInstalled tracks which StateAction functions are already wired into
	// each phase via wireStateActions, deduped by function pointer, so repeated
	// Grammar() calls don't stack duplicate handlers on a reserved slot.
	fnrefInstalled map[string]map[uintptr]bool

	// fnrefReplaced records phases an `@<rulename>-<phase>/replace` fnref has
	// taken ownership of; thereafter the plain/prepend/append fnrefs for that
	// phase are ignored so older handlers are not re-installed.
	fnrefReplaced map[string]bool
}

// Clear removes all alternates and state actions from this RuleSpec.
func (rs *RuleSpec) Clear() *RuleSpec {
	rs.open = rs.open[:0]
	rs.close = rs.close[:0]
	rs.bo = rs.bo[:0]
	rs.bc = rs.bc[:0]
	rs.ao = rs.ao[:0]
	rs.ac = rs.ac[:0]
	return rs
}

// AddOpen appends alternates to the open list (at the end).
func (rs *RuleSpec) AddOpen(alts ...*AltSpec) *RuleSpec {
	rs.open = append(rs.open, alts...)
	return rs
}

// AddClose appends alternates to the close list (at the end).
func (rs *RuleSpec) AddClose(alts ...*AltSpec) *RuleSpec {
	rs.close = append(rs.close, alts...)
	return rs
}

// PrependOpen inserts alternates at the beginning of the open list.
func (rs *RuleSpec) PrependOpen(alts ...*AltSpec) *RuleSpec {
	rs.open = append(alts, rs.open...)
	return rs
}

// PrependClose inserts alternates at the beginning of the close list.
func (rs *RuleSpec) PrependClose(alts ...*AltSpec) *RuleSpec {
	rs.close = append(alts, rs.close...)
	return rs
}

// AltModListOpts configures modifications to a RuleSpec alternate list (TS ListMods).
type AltModListOpts struct {
	Clear  bool                             // Empty the existing list before applying.
	Delete []int                            // Indices to delete (supports negative).
	Move   []int                            // Pairs: [from, to, from, to, ...].
	Custom func(list []*AltSpec) []*AltSpec // Custom modification callback.
}

// ModifyOpen applies delete/move/custom modifications to the open alternates list.
// Matches TS `rs.open(alts, mods)` where mods has delete/move/custom.
func (rs *RuleSpec) ModifyOpen(mods *AltModListOpts) *RuleSpec {
	rs.open = modifyAltList(rs.open, mods)
	return rs
}

// ModifyClose applies delete/move/custom modifications to the close alternates list.
func (rs *RuleSpec) ModifyClose(mods *AltModListOpts) *RuleSpec {
	rs.close = modifyAltList(rs.close, mods)
	return rs
}

func modifyAltList(list []*AltSpec, mods *AltModListOpts) []*AltSpec {
	if mods == nil {
		return list
	}
	// Clear empties the existing alternates before delete/move/custom, so a
	// later plugin can replace a rule's alternates outright.
	if mods.Clear {
		list = nil
	}
	if list == nil && mods.Custom == nil {
		return list
	}
	// Convert to []any, apply ModList, convert back.
	anyList := make([]any, len(list))
	for i, v := range list {
		anyList[i] = v
	}
	anyList = ModList(anyList, &ModListOpts{
		Delete: mods.Delete,
		Move:   mods.Move,
	})
	result := make([]*AltSpec, len(anyList))
	for i, v := range anyList {
		result[i] = v.(*AltSpec)
	}
	if mods.Custom != nil {
		if newList := mods.Custom(result); newList != nil {
			result = newList
		}
	}
	return result
}

// AddBO appends a before-open action.
func (rs *RuleSpec) AddBO(action StateAction) *RuleSpec {
	rs.bo = append(rs.bo, action)
	return rs
}

// AddAO appends an after-open action.
func (rs *RuleSpec) AddAO(action StateAction) *RuleSpec {
	rs.ao = append(rs.ao, action)
	return rs
}

// AddBC appends a before-close action.
func (rs *RuleSpec) AddBC(action StateAction) *RuleSpec {
	rs.bc = append(rs.bc, action)
	return rs
}

// AddAC appends an after-close action.
func (rs *RuleSpec) AddAC(action StateAction) *RuleSpec {
	rs.ac = append(rs.ac, action)
	return rs
}

// ClearOpen removes this rule's open alternates without touching close or
// the lifecycle actions. A later plugin can call this, then AddOpen, to
// replace the open alternates contributed by earlier plugins.
func (rs *RuleSpec) ClearOpen() *RuleSpec {
	rs.open = nil
	return rs
}

// ClearClose removes this rule's close alternates (see ClearOpen).
func (rs *RuleSpec) ClearClose() *RuleSpec {
	rs.close = nil
	return rs
}

// ClearActions removes the registered lifecycle actions for the named
// phases (any of "bo", "ao", "bc", "ac"); with no arguments, all four are
// cleared. The fnref dedup/replace bookkeeping for those phases is reset
// too, so a subsequent wireStateActions re-installs cleanly. Alternates
// are untouched.
func (rs *RuleSpec) ClearActions(phases ...string) *RuleSpec {
	all := phases
	if len(all) == 0 {
		all = []string{"bo", "ao", "bc", "ac"}
	}
	for _, p := range all {
		switch p {
		case "bo":
			rs.bo = nil
		case "ao":
			rs.ao = nil
		case "bc":
			rs.bc = nil
		case "ac":
			rs.ac = nil
		}
		base := "@" + rs.Name + "-" + p
		delete(rs.fnrefInstalled, base)
		delete(rs.fnrefReplaced, base)
	}
	return rs
}

// Fnref installs lifecycle state actions from a funcref map, using the
// reserved `@<rule>-<phase>` naming (with the optional `/prepend`,
// `/append`, `/replace` suffixes). Mirrors the TS `rs.fnref(frm)` method,
// giving append-by-funcref parity for code-built grammars without going
// through Grammar(). Returns the RuleSpec for chaining.
func (rs *RuleSpec) Fnref(ref map[FuncRef]any) *RuleSpec {
	wireStateActions(rs, ref)
	return rs
}

// PrependBO inserts a before-open action at the front (runs first).
func (rs *RuleSpec) PrependBO(action StateAction) *RuleSpec {
	rs.bo = append([]StateAction{action}, rs.bo...)
	return rs
}

// PrependAO inserts an after-open action at the front.
func (rs *RuleSpec) PrependAO(action StateAction) *RuleSpec {
	rs.ao = append([]StateAction{action}, rs.ao...)
	return rs
}

// PrependBC inserts a before-close action at the front.
func (rs *RuleSpec) PrependBC(action StateAction) *RuleSpec {
	rs.bc = append([]StateAction{action}, rs.bc...)
	return rs
}

// PrependAC inserts an after-close action at the front.
func (rs *RuleSpec) PrependAC(action StateAction) *RuleSpec {
	rs.ac = append([]StateAction{action}, rs.ac...)
	return rs
}

// OpenAlts returns this rule's open alternates. The returned slice is the
// live backing slice — read-only by convention; mutate via the Add/Modify/
// Clear methods. (Read accessor; the lists themselves are unexported, as in
// the TS RuleSpec.)
func (rs *RuleSpec) OpenAlts() []*AltSpec { return rs.open }

// CloseAlts returns this rule's close alternates (see OpenAlts).
func (rs *RuleSpec) CloseAlts() []*AltSpec { return rs.close }

// Actions returns the registered lifecycle actions for a phase ("bo",
// "ao", "bc", "ac"); an unknown phase returns nil.
func (rs *RuleSpec) Actions(phase string) []StateAction {
	switch phase {
	case "bo":
		return rs.bo
	case "ao":
		return rs.ao
	case "bc":
		return rs.bc
	case "ac":
		return rs.ac
	}
	return nil
}

// HasBO reports whether any before-open action is registered (mirrors the
// TS RuleSpec.bo boolean presence flag); likewise HasAO/HasBC/HasAC.
func (rs *RuleSpec) HasBO() bool { return len(rs.bo) > 0 }

// HasAO reports whether any after-open action is registered.
func (rs *RuleSpec) HasAO() bool { return len(rs.ao) > 0 }

// HasBC reports whether any before-close action is registered.
func (rs *RuleSpec) HasBC() bool { return len(rs.bc) > 0 }

// HasAC reports whether any after-close action is registered.
func (rs *RuleSpec) HasAC() bool { return len(rs.ac) > 0 }

// getRuleProp accesses a rule property by path (e.g. "d", "n.pk").
// Returns the integer value and whether it was found.
// Matches the TypeScript getRuleProp(r, prop, subprop) function.
func getRuleProp(r *Rule, prop string, subprop string) (int, bool) {
	if r == nil {
		return 0, false
	}
	switch prop {
	case "d":
		return r.D, true
	case "n":
		if subprop != "" {
			val, ok := r.N[subprop]
			return val, ok
		}
	}
	return 0, false
}

// resolveRuleProp reads a condition path, distinguishing an unset COUNTER
// from a path that does not resolve at all.
//
// A named counter always resolves: one that was never incremented reads as 0,
// because it has counted nothing. That keeps counter comparisons total —
// exactly one of <, =, > holds — which is what stops $lt and $gt from both
// being true at once.
//
// A nil rule, an unknown prop, or "n" with no counter named does NOT resolve.
// That is genuine absence rather than zero, so callers stay permissive there
// instead of inventing a value the rule cannot supply.
func resolveRuleProp(r *Rule, prop string, subprop string) (int, bool) {
	if r == nil {
		return 0, false
	}
	switch prop {
	case "d":
		return r.D, true
	case "n":
		if subprop != "" {
			return r.N[subprop], true
		}
	}
	return 0, false
}

// MakeRuleCond creates an AltCond function from a comparison operator, property path, and value.
// Matches the TypeScript makeRuleCond(co, prop, subprop, val) function.
// When the property is not set (missing), the condition returns true.
// condNum coerces a resolved value to a float for ordered comparison.
// Only numbers are orderable as numbers; anything else is not.
func condNum(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// condOrder compares two resolved values, mirroring the TS port: numbers
// compare numerically, strings lexicographically, and anything else is not
// orderable. Returns (-1, 0, 1) and whether the pair was comparable at all.
func condOrder(a, b any) (int, bool) {
	if an, ok := condNum(a); ok {
		if bn, ok := condNum(b); ok {
			switch {
			case an < bn:
				return -1, true
			case an > bn:
				return 1, true
			}
			return 0, true
		}
		return 0, false
	}
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return strings.Compare(as, bs), true
		}
	}
	return 0, false
}

// condEqual is equality across the value shapes a condition can meet.
// Numbers compare by value regardless of int/float shape, so `n.pk` (int)
// equals a float64 1 the way it does in TS, where both are just numbers.
func condEqual(a, b any) bool {
	if an, ok := condNum(a); ok {
		if bn, ok := condNum(b); ok {
			return an == bn
		}
		return false
	}
	return a == b
}

// MakeRuleCond builds the condition function for one declarative comparison.
//
// A named COUNTER that was never set resolves to 0 — it has counted nothing —
// so counter comparisons stay total (exactly one of <, =, > holds). These
// used to short-circuit to true on a missing counter, so $lt and $gt were both
// true at once and a "past the limit" guard fired on the first token.
//
// A path that does not resolve at all (nil rule, unknown property, a `u`/`k`
// key that was never set) is NOT a zero, and keeps the permissive
// short-circuit: it answers a question the rule cannot answer. $exist is the
// explicit set/unset test and never coerces.
//
// `subprop` carries the remainder of a dotted path, so `n.a` arrives as
// ("n", "a") and deeper paths like `parent.n.x` as ("parent", "n.x").
func MakeRuleCond(op string, prop string, subprop string, val any) (AltCond, error) {
	path := []string{prop}
	if subprop != "" {
		path = append(path, strings.Split(subprop, ".")...)
	}

	switch op {
	// $eq fails CLOSED on a path that does not resolve: "equals x" cannot be
	// satisfied by a value that is not there. The ordered operators below fail
	// OPEN instead, because they answer a question the rule cannot answer.
	// (This asymmetry is the TS port's documented behaviour; Go used to fail
	// open here too, so `{ 'u.never': { $eq: 'x' } }` matched everything.)
	case "$eq":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := resolveRulePath(r, path)
			return ok && condEqual(rval, val)
		}, nil
	case "$ne":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := resolveRulePath(r, path)
			return !ok || !condEqual(rval, val)
		}, nil
	case "$lt":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := resolveRulePath(r, path)
			if !ok {
				return true
			}
			cmp, comparable := condOrder(rval, val)
			return !comparable || cmp < 0
		}, nil
	case "$lte":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := resolveRulePath(r, path)
			if !ok {
				return true
			}
			cmp, comparable := condOrder(rval, val)
			return !comparable || cmp <= 0
		}, nil
	case "$gt":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := resolveRulePath(r, path)
			if !ok {
				return true
			}
			cmp, comparable := condOrder(rval, val)
			return !comparable || cmp > 0
		}, nil
	case "$gte":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := resolveRulePath(r, path)
			if !ok {
				return true
			}
			cmp, comparable := condOrder(rval, val)
			return !comparable || cmp >= 0
		}, nil
	// $exist asks whether the path was SET, so it reads presence via
	// getRuleProp/existence rather than the resolving walk (which reads an
	// unset counter as 0).
	case "$exist":
		want := true
		switch v := val.(type) {
		case bool:
			want = v
		case int:
			want = v != 0
		}
		return func(r *Rule, ctx *Context) bool {
			return condPathExists(r, path) == want
		}, nil
	default:
		return nil, fmt.Errorf("MakeRuleCond: unknown comparison operator: %s", op)
	}
}

// condPathExists reports whether a path was actually SET, which the resolving
// walk cannot: it reads an unset counter as 0, so a counter set to 0 and one
// never set look identical to every comparison.
func condPathExists(r *Rule, path []string) bool {
	if r == nil || len(path) == 0 {
		return false
	}
	if path[0] == "n" && len(path) == 2 {
		_, ok := r.N[path[1]]
		return ok
	}
	_, ok := resolveRulePath(r, path)
	return ok
}

// condPathRoots are the rule properties a declarative condition can read.
// Matches the TS port's set, so the same declarative grammar is expressible in
// either runtime. A path rooted anywhere else can NEVER resolve, and the
// ordered operators would fail open on it forever — the guard silently doing
// nothing — so it is rejected while the grammar is built.
var condPathRoots = map[string]bool{
	"n": true, "u": true, "k": true, // counters, user data, kept data
	"d": true, "i": true, "name": true, "state": true, // identity / position
	"node": true, "oN": true, "cN": true,
	"o": true, "c": true, "o0": true, "o1": true, "c0": true, "c1": true, // tokens
	"parent": true, "child": true, "prev": true, "next": true, // rule graph
	"spec": true,
}

// resolveRulePath reads a dotted condition path off a rule, mirroring the TS
// port's generic property walk so the same declarative grammar behaves the
// same in either runtime.
//
// Returns (value, resolved). A named COUNTER always resolves: one never
// incremented reads as 0, because it has counted nothing — that is what keeps
// counter comparisons total. Anything the path cannot reach does not resolve,
// and callers stay permissive there rather than inventing a value.
func resolveRulePath(r *Rule, path []string) (any, bool) {
	if r == nil || len(path) == 0 {
		return nil, false
	}

	rest := path[1:]

	switch path[0] {
	case "n":
		if len(rest) == 1 {
			return r.N[rest[0]], true // unset counter reads as 0
		}
	case "u":
		if len(rest) == 1 {
			v, ok := r.U[rest[0]]
			return v, ok
		}
	case "k":
		if len(rest) == 1 {
			v, ok := r.K[rest[0]]
			return v, ok
		}
	case "d":
		return leaf(r.D, rest)
	case "i":
		return leaf(r.I, rest)
	case "name":
		return leaf(r.Name, rest)
	case "state":
		return leaf(r.State, rest)
	case "node":
		return leaf(r.Node, rest)
	case "oN":
		return leaf(r.ON, rest)
	case "cN":
		return leaf(r.CN, rest)
	case "o0":
		return tokenPath(r.O0, rest)
	case "o1":
		return tokenPath(r.O1, rest)
	case "c0":
		return tokenPath(r.C0, rest)
	case "c1":
		return tokenPath(r.C1, rest)
	case "parent":
		return resolveRulePath(r.Parent, rest)
	case "child":
		return resolveRulePath(r.Child, rest)
	case "prev":
		return resolveRulePath(r.Prev, rest)
	case "next":
		return resolveRulePath(r.Next, rest)
	}

	return nil, false
}

// leaf returns v when the path ends here, and nothing when it goes deeper
// than the value can (TS walks into undefined and yields undefined).
func leaf(v any, rest []string) (any, bool) {
	if len(rest) == 0 {
		return v, true
	}
	return nil, false
}

// tokenPath reads a field off a matched token, e.g. `o0.tin`.
func tokenPath(t *Token, rest []string) (any, bool) {
	if t == nil {
		return nil, false
	}
	if len(rest) == 0 {
		return t, true
	}
	if len(rest) != 1 {
		return nil, false
	}
	switch rest[0] {
	case "tin":
		return t.Tin, true
	case "name":
		return t.Name, true
	case "src":
		return t.Src, true
	case "val":
		return t.Val, true
	case "why":
		return t.Why, true
	}
	return nil, false
}

// condProblems reports everything wrong with one declarative condition entry.
// Pure: it returns messages instead of an error so a whole grammar can be
// checked and every problem listed at once.
func condProblems(propdef string, pspec any) []string {
	var out []string

	parts := strings.SplitN(propdef, ".", 2)
	if !condPathRoots[parts[0]] {
		roots := make([]string, 0, len(condPathRoots))
		for root := range condPathRoots {
			roots = append(roots, root)
		}
		sort.Strings(roots)
		out = append(out, fmt.Sprintf(
			"unknown condition path: %q (no rule property %q); known roots: %s",
			propdef, parts[0], strings.Join(roots, ", ")))
	}

	switch v := pspec.(type) {
	case int, int64, float64, string, bool:
		// Plain value: shorthand for $eq, as in the TS port.
	case CondOp:
		if _, err := MakeRuleCond(v.Op, "d", "", 0); err != nil {
			out = append(out, fmt.Sprintf(
				"unknown condition operator: %s (on %q)", v.Op, propdef))
		}
	default:
		// Anything else was silently ignored, leaving the alternate with one
		// fewer condition than it reads as having — or none at all.
		out = append(out, fmt.Sprintf(
			"unusable condition value on %q: want int or CondOp, got %T", propdef, v))
	}

	return out
}

// ValidateAlt reports every problem in an alternate's DECLARATIVE parts.
//
// Pure: it reports instead of erroring, so a whole grammar can be checked and
// every problem listed at once. NormAlt calls the same checks while the
// grammar is built and returns an error on what it finds, which is why a bad
// declarative spec cannot surface during a parse — but a grammar held as data
// (the Grammar / GrammarText path, a generator, an editor) can be checked with
// this directly, before any parser exists.
//
// Only declarative fields are checkable: a condition given as a function is
// opaque, and P/R rule names may legitimately be defined later.
func ValidateAlt(alt *AltSpec) []string {
	var out []string

	if alt == nil {
		return out
	}

	for propdef, pspec := range alt.CD {
		out = append(out, condProblems(propdef, pspec)...)
	}

	if err := ValidateGroupTags(alt.G); err != nil {
		out = append(out, err.Error())
	}

	sort.Strings(out) // map iteration is random; keep reports stable
	return out
}

// ValidateAlts reports problems across a list of alternates, each prefixed
// with where it is. label names the list, e.g. "val.open".
func ValidateAlts(alts []*AltSpec, label string) []string {
	var out []string

	at := ""
	if label != "" {
		at = label + " "
	}

	for index, alt := range alts {
		for _, problem := range ValidateAlt(alt) {
			out = append(out, fmt.Sprintf("%salt[%d]: %s", at, index, problem))
		}
	}

	return out
}

// NormAlt normalizes an AltSpec by converting a declarative CD condition
// into a C function and validating the G tag format.  Returns a non-nil
// error if any G tag fails the group-tag regex; callers must check the
// return value and surface the error (no panics).
func NormAlt(alt *AltSpec) error {
	if alt == nil {
		return nil
	}

	if err := ValidateGroupTags(alt.G); err != nil {
		return err
	}

	if alt.CD == nil || alt.C != nil {
		return nil
	}

	// Validate the whole declarative condition BEFORE building any of it, so
	// an unusable entry is reported rather than skipped. Skipping left the
	// alternate with fewer conditions than it reads as having — or none, in
	// which case it matched everything. This runs while the grammar is built,
	// never during a parse.
	if problems := ValidateAlt(alt); len(problems) > 0 {
		return fmt.Errorf("tabnas: %s", strings.Join(problems, "; "))
	}

	var conds []AltCond
	for propdef, pspec := range alt.CD {
		parts := strings.SplitN(propdef, ".", 2)
		prop := parts[0]
		subprop := ""
		if len(parts) == 2 {
			subprop = parts[1]
		}

		switch v := pspec.(type) {
		case int:
			cond, err := MakeRuleCond("$eq", prop, subprop, v)
			if err != nil {
				return err
			}
			conds = append(conds, cond)
		case CondOp:
			cond, err := MakeRuleCond(v.Op, prop, subprop, v.Val)
			if err != nil {
				return err
			}
			conds = append(conds, cond)
		}
	}

	if len(conds) == 1 {
		alt.C = conds[0]
	} else if len(conds) > 1 {
		alt.C = func(r *Rule, ctx *Context) bool {
			for _, cond := range conds {
				if !cond(r, ctx) {
					return false
				}
			}
			return true
		}
	}

	return nil
}

// NormAlts normalizes all alternates in a RuleSpec.  Returns the first
// validation error encountered, if any.
func NormAlts(spec *RuleSpec) error {
	for _, alt := range spec.open {
		if err := NormAlt(alt); err != nil {
			return err
		}
	}
	for _, alt := range spec.close {
		if err := NormAlt(alt); err != nil {
			return err
		}
	}
	return nil
}

// Rule is a rule instance created during parsing (the runtime counterpart of a RuleSpec).
type Rule struct {
	I      int       // Unique rule id within this parse run.
	Name   string    // Rule name (matches its RuleSpec).
	Spec   *RuleSpec // The RuleSpec this rule applies.
	Node   any       // Value node this rule is building.
	State  RuleState // Current phase: open ("o") or close ("c").
	D      int       // Stack depth at which this rule was pushed.
	Child  *Rule     // Rule pushed by this rule (NoRule if none).
	Parent *Rule     // Rule that pushed this rule (NoRule if none).
	Prev   *Rule     // Rule this one replaced (NoRule if none).
	Next   *Rule     // Rule to process after this one.

	// Generalized per-position matched tokens. O[i] holds the token
	// matched at the i-th lookahead position during OPEN (mirroring C
	// for CLOSE). ON / CN give the number of matched positions. This
	// supersedes the legacy O0/O1/OS (and C0/C1/CS) two-slot fields,
	// which are still maintained below for backward compatibility.
	O  []*Token // Tokens matched in the open phase, by position.
	ON int      // Count of tokens matched in the open phase.
	C  []*Token // Tokens matched in the close phase, by position.
	CN int      // Count of tokens matched in the close phase.

	// Legacy two-slot aliases. Kept in sync with O[0..1] / C[0..1] by
	// ParseAlts so existing grammar code and plugins that read r.O0,
	// r.O1, r.C0, r.C1, r.OS, r.CS continue to work unchanged.
	O0 *Token // Open token at position 0 (alias of O[0]).
	O1 *Token // Open token at position 1 (alias of O[1]).
	C0 *Token // Close token at position 0 (alias of C[0]).
	C1 *Token // Close token at position 1 (alias of C[1]).
	OS int    // Open match count (alias of ON).
	CS int    // Close match count (alias of CN).

	// N/U/K are allocated lazily: nil until first written (a Go nil-map
	// READ is safe and returns the zero value, so read-only access needs
	// no guard). Plugins writing to a fresh rule must go through
	// EnsureN/EnsureU/EnsureK (or nil-guard themselves) — before v0.3
	// these maps were always allocated, costing three heap allocations
	// per rule instance whether or not the grammar used them.
	N   map[string]int // Named counters tracked across the rule.
	U   map[string]any // Custom user props (not propagated to children).
	K   map[string]any // Custom keep props (propagated via push/replace).
	Why string         // Internal tracing field; set when a rule fails.
}

// EnsureN returns the rule's named-counter map, allocating it on first
// use. Required before writing r.N on a fresh rule (nil until written).
func (r *Rule) EnsureN() map[string]int {
	if r.N == nil {
		r.N = make(map[string]int, 4)
	}
	return r.N
}

// EnsureU returns the rule's user-prop map, allocating it on first use.
// Required before writing r.U on a fresh rule (nil until written).
func (r *Rule) EnsureU() map[string]any {
	if r.U == nil {
		r.U = make(map[string]any, 4)
	}
	return r.U
}

// EnsureK returns the rule's keep-prop map, allocating it on first use.
// Required before writing r.K on a fresh rule (nil until written).
func (r *Rule) EnsureK() map[string]any {
	if r.K == nil {
		r.K = make(map[string]any, 4)
	}
	return r.K
}

// NoRule is the sentinel "no rule" value; its Node is Undefined (TS NORULE.node === undefined).
var NoRule *Rule

func init() {
	NoRule = &Rule{Name: "norule", I: -1, State: OPEN, Node: Undefined,
		N: make(map[string]int), U: make(map[string]any), K: make(map[string]any)}
}

// An unset counter reads as 0: a counter that has never been incremented has
// counted nothing. These previously short-circuited to true when the counter
// was missing, which made Lt(k,n) and Gt(k,n) BOTH true — a predicate and its
// own negation — breaking trichotomy and firing "past the limit" guards on the
// first token. Use Exist to ask whether a counter was set at all.

// Eq checks if counter equals limit (unset counter reads as 0).
func (r *Rule) Eq(counter string, limit int) bool {
	return r.N[counter] == limit
}

// Lt checks if counter < limit (unset counter reads as 0).
func (r *Rule) Lt(counter string, limit int) bool {
	return r.N[counter] < limit
}

// Gt checks if counter > limit (unset counter reads as 0).
func (r *Rule) Gt(counter string, limit int) bool {
	return r.N[counter] > limit
}

// Lte checks if counter <= limit (unset counter reads as 0).
func (r *Rule) Lte(counter string, limit int) bool {
	return r.N[counter] <= limit
}

// Gte checks if counter >= limit (unset counter reads as 0).
func (r *Rule) Gte(counter string, limit int) bool {
	return r.N[counter] >= limit
}

// Exist reports whether the counter was set at all. The comparison helpers
// read an unset counter as 0, so they cannot tell "never counted" from
// "counted zero"; this can. (Declarative equivalent: $exist.)
func (r *Rule) Exist(counter string) bool {
	_, ok := r.N[counter]
	return ok
}

// MakeRule creates a new Rule from a RuleSpec.
func MakeRule(spec *RuleSpec, ctx *Context, node any) *Rule {
	// N/U/K stay nil until first written (see the field docs / Ensure
	// helpers) — most rules in value-building grammars never touch them.
	r := &Rule{
		I: ctx.UI, Name: spec.Name, Spec: spec, Node: node,
		State: OPEN, D: ctx.RSI,
		Child: NoRule, Parent: NoRule, Prev: NoRule, Next: NoRule,
		O: nil, ON: 0, C: nil, CN: 0,
		O0: NoToken, O1: NoToken, C0: NoToken, C1: NoToken,
	}
	ctx.UI++
	return r
}

// Process processes this rule, returning the next rule to process.
func (r *Rule) Process(ctx *Context, lex *Lex) *Rule {
	isOpen := r.State == OPEN
	var next *Rule
	if isOpen {
		next = r
	} else {
		next = NoRule
	}

	def := r.Spec
	var alts []*AltSpec
	if isOpen {
		alts = def.open
	} else {
		alts = def.close
	}

	// Before actions
	if isOpen && len(def.bo) > 0 {
		for _, action := range def.bo {
			action(r, ctx)
		}
	} else if !isOpen && len(def.bc) > 0 {
		for _, action := range def.bc {
			action(r, ctx)
		}
	}

	// Match alternates
	alt, _ := ParseAlts(isOpen, alts, lex, r, ctx)

	// No alternate matched: immediate parse error (matching TS parse_alts behavior).
	// In TS, when alts exist but none match, out.e = ctx.t0 which triggers this.bad().
	if alt == nil && len(alts) > 0 {
		ctx.ParseErr = ctx.T0
		ctx.parseErrDiag = captureDiag(ctx)
		return next
	}

	// Alt modifier
	if alt != nil && alt.H != nil {
		alt = alt.H(alt, r, ctx)
	}

	// Error check: if alt.E returns a token, signal a parse error.
	// The diagnostic context is snapshotted HERE, not when the parser
	// loop later builds the error: Process keeps running (counters,
	// pushes, pops, the state flip) after this point, while the TS
	// engine throws immediately at the raise site — reading RS/RSI and
	// the rule state after Process returns reports the post-mutation
	// world and diverges from TS.
	if alt != nil && alt.E != nil {
		errTkn := alt.E(r, ctx)
		if errTkn != nil {
			ctx.ParseErr = errTkn
			ctx.parseErrDiag = captureDiag(ctx)
		}
	}

	// Update counters
	if alt != nil && alt.N != nil && 0 < len(alt.N) {
		rn := r.EnsureN()
		for cn, cv := range alt.N {
			if cv == 0 {
				rn[cn] = 0
			} else {
				if _, ok := rn[cn]; !ok {
					rn[cn] = 0
				}
				rn[cn] += cv
			}
		}
	}

	// Set custom properties
	if alt != nil && alt.U != nil && 0 < len(alt.U) {
		ru := r.EnsureU()
		for k, v := range alt.U {
			ru[k] = v
		}
	}
	if alt != nil && alt.K != nil && 0 < len(alt.K) {
		rk := r.EnsureK()
		for k, v := range alt.K {
			rk[k] = v
		}
	}

	// Compute how many tokens this alt consumes (matched minus
	// backtrack) once, and record them on the rewind history BEFORE the
	// action runs, so a ctx.Rewind() call inside the action sees the
	// just-matched tokens. The same count drives the lookahead-buffer
	// shift below. Mirrors the TS rules.ts ordering.
	consumed := 0
	if alt != nil {
		backtrack := alt.B
		if alt.BF != nil {
			backtrack = alt.BF(r, ctx)
		}
		if isOpen {
			consumed = r.ON - backtrack
		} else {
			consumed = r.CN - backtrack
		}
		if consumed < 0 {
			consumed = 0
		}
		ctx.recordConsumed(consumed)
	}

	// Action callback
	if alt != nil && alt.A != nil {
		alt.A(r, ctx)
	}

	// Push / Replace / Pop
	if alt != nil {
		// Resolve push rule name (static or dynamic)
		pushName := alt.P
		if alt.PF != nil {
			pushName = alt.PF(r, ctx)
		}
		// Resolve replace rule name (static or dynamic)
		replaceName := alt.R
		if alt.RF != nil {
			replaceName = alt.RF(r, ctx)
		}

		if pushName != "" {
			rulespec, ok := ctx.RSM[pushName]
			if ok {
				if ctx.RSI < len(ctx.RS) {
					ctx.RS[ctx.RSI] = r
				} else {
					ctx.RS = append(ctx.RS, r)
				}
				ctx.RSI++
				next = MakeRule(rulespec, ctx, r.Node)
				r.Child = next
				next.Parent = r
				if len(r.N) > 0 {
					nn := next.EnsureN()
					for k, v := range r.N {
						nn[k] = v
					}
				}
				if len(r.K) > 0 {
					nk := next.EnsureK()
					for k, v := range r.K {
						nk[k] = v
					}
				}
			} else {
				// Unknown rule name: raise unknown_rule instead of
				// silently ignoring the push (TS parity — rules.ts
				// throws via unknownRule + bad when the alt fires).
				markUnknownRule(ctx, pushName)
				return next
			}
		} else if replaceName != "" {
			rulespec, ok := ctx.RSM[replaceName]
			if ok {
				next = MakeRule(rulespec, ctx, r.Node)
				next.Parent = r.Parent
				next.Prev = r
				if len(r.N) > 0 {
					nn := next.EnsureN()
					for k, v := range r.N {
						nn[k] = v
					}
				}
				if len(r.K) > 0 {
					nk := next.EnsureK()
					for k, v := range r.K {
						nk[k] = v
					}
				}
			} else {
				markUnknownRule(ctx, replaceName)
				return next
			}
		} else if !isOpen {
			// Pop
			if ctx.RSI > 0 {
				ctx.RSI--
				next = ctx.RS[ctx.RSI]
			} else {
				next = NoRule
			}
		}
	} else if !isOpen {
		// No alt matched AND we're closing → pop
		if ctx.RSI > 0 {
			ctx.RSI--
			next = ctx.RS[ctx.RSI]
		} else {
			next = NoRule
		}
	}

	r.Next = next

	// After actions
	if isOpen && len(def.ao) > 0 {
		for _, action := range def.ao {
			action(r, ctx)
		}
	} else if !isOpen && len(def.ac) > 0 {
		for _, action := range def.ac {
			action(r, ctx)
		}
	}

	// State transition
	if r.State == OPEN {
		r.State = CLOSE
	}

	// Token consumption with backtrack (only when an alt matched).
	// `consumed` was computed above (and recorded on the rewind history)
	// before the action ran; reuse it here for the lookahead-buffer
	// shift. Generalized from the previous 2-slot shift to any number of
	// consumed positions, to match the N-token lookahead support in
	// ParseAlts.
	if alt != nil {
		if consumed > 0 {
			// V1 / V2 were set in recordConsumed before the action (the
			// consumed tbuf slots are already cleared to NoToken here).
			// Compact the lookahead buffer: shift left by `consumed`,
			// filling vacated tail positions with NoToken so later alts
			// re-fetch from the lexer. If a ctx.Rewind() ran in the
			// action it already cleared/re-queued T, so this is a no-op.
			L := len(ctx.T)
			for i := 0; i < L-consumed; i++ {
				ctx.T[i] = ctx.T[i+consumed]
			}
			start := L - consumed
			if start < 0 {
				start = 0
			}
			for i := start; i < L; i++ {
				ctx.T[i] = NoToken
			}

			// Sync legacy T0 / T1 aliases.
			if len(ctx.T) >= 1 {
				ctx.T0 = ctx.T[0]
			} else {
				ctx.T0 = NoToken
			}
			if len(ctx.T) >= 2 {
				ctx.T1 = ctx.T[1]
			} else {
				ctx.T1 = NoToken
			}

			ctx.TC += consumed
		}
	}

	return next
}

// ParseAlts attempts to match one of the alternates.
//
// Supports arbitrary N-token lookahead: an alt's S slice may declare
// any number of positions (previously capped at 2). Tokens are fetched
// lazily - position i is only requested after position i-1 matches.
// markUnknownRule flags an unknown push/replace rule name as a parse
// error carrying the unknown_rule code and the offending name (TS
// parity: rules.ts unknownRule token + bad throw). The current
// lookahead token supplies the source location; it is copied, not
// mutated, since it may be the shared NoToken sentinel.
func markUnknownRule(ctx *Context, name string) {
	at := ctx.T0
	if at == nil {
		at = NoToken
	}
	ctx.ParseErr = &Token{
		Name: at.Name, Tin: at.Tin, Val: at.Val, Src: at.Src,
		SI: at.SI, RI: at.RI, CI: at.CI,
		Err: "unknown_rule",
		Use: map[string]any{"rulename": name},
	}
	ctx.parseErrDiag = captureDiag(ctx)
}

func ParseAlts(isOpen bool, alts []*AltSpec, lex *Lex, rule *Rule, ctx *Context) (*AltSpec, bool) {
	if len(alts) == 0 {
		return nil, false
	}

	// Negotiated lexing (LexConfig.Relex): a token fetched under one rule
	// context keeps its identity in the pushback buffer, but a character
	// claimable by several matchers may legitimately be a DIFFERENT token
	// for a different alternate. When enabled, a tin mismatch is not
	// final: the alternate may re-cut the span under its own token list.
	// See Lex.Relex.
	relex := ctx.Cfg != nil && ctx.Cfg.Relex

	// Undo state for a recut this alternate commits. The token buffer is
	// shared with every later alternate AND with later rules, so a cut
	// chosen for an alternate that then fails would otherwise be inherited
	// as if it had been chosen for them — which is how a renegotiation
	// could turn a working parse into a failing one. Only the FIRST recut
	// of an alternate is recorded: restoring to before it undoes any later
	// ones too.
	unI := -1
	var unTkn *Token
	var unSaved relexPoint

	for _, alt := range alts {
		matched := 0
		cond := true
		unI = -1

		// Token-set positions are re-resolved against the parsing instance
		// (identity when it has no tokenSet override).
		altS := ctx.altS(alt)
		sN := len(altS)
		for i := 0; i < sN; i++ {
			// Grow the lookahead buffer on demand.
			for len(ctx.T) <= i {
				ctx.T = append(ctx.T, NoToken)
			}

			// Lazy fetch: only pull a new token from the lexer if this
			// slot has not been populated by a previous alt / fetch.
			if ctx.T[i].IsNoToken() {
				tkn := lex.Next(rule)
				ctx.T[i] = tkn
				// Keep the legacy T0 / T1 aliases in sync so existing
				// grammar / plugin code that reads them observes the
				// same values as ctx.T[0] / ctx.T[1].
				if i == 0 {
					ctx.T0 = tkn
				} else if i == 1 {
					ctx.T1 = tkn
				}
				// Lex subscribers are notified inside Lex.Next (for every
				// raw token, ignored ones included), matching the TS lexer.
			}

			// A bad token never satisfies a position. Under negotiated
			// lexing its immediate throw is deferred (Lex.Next hands it
			// over instead) so an alternate can try to re-cut the span
			// into something it names — but that is ALL the deferral buys
			// it. Without this test a wildcard position would accept it
			// outright: an empty slot skips the check, and #AA makes
			// tinMatch true for every tin including #BD, so malformed
			// input that Relex:false rejects would parse.
			isBad := relex && ctx.T[i].Tin == TinBD

			// Empty alt.S[i] means "no Tin constraint at this position"
			// (wildcard) - the token is still fetched and consumed but
			// the match check is skipped. This prevents silently
			// dropping the check at a later required position.
			if isBad || len(altS[i]) != 0 {
				hit := false
				if !isBad && len(altS[i]) != 0 {
					hit = tinMatch(ctx.T[i].Tin, altS[i])
				}
				if !hit {
					// Before failing this alternate, ask the lexer whether
					// the same span cuts to a tin this alternate wants.
					// Bounded: at most one recut per (alternate, position),
					// and the recut's tin is in this alt's set by
					// construction — Relex returns nil otherwise — so this
					// cannot accept a token the position does not name.
					var recut *Token
					if relex && 0 < len(ctx.T[i].Src) && len(altS[i]) != 0 {
						recut = lex.Relex(ctx.T[i], altS[i], rule)
					}
					if recut == nil {
						cond = false
						break
					}
					// First recut of this alternate: remember how to undo.
					if unI == -1 {
						unI = i
						unTkn = ctx.T[i]
						unSaved = lex.RelexUndo()
					}
					// The recut replaces the buffered token; anything
					// fetched beyond it was lexed from positions that may
					// no longer exist, so it is dropped and re-fetched on
					// demand.
					ctx.setT(i, recut)
					ctx.dropT(i + 1)
				}
			}
			matched = i + 1
		}

		// Record the matched tokens on the rule only when the tin
		// positions matched — failed alts left partial recordings that
		// nothing could observe (custom conditions only run on tin
		// success, and the next candidate overwrote them). Recording
		// stays BEFORE the alt.C condition call so conditions observe
		// the candidate's tokens. Both the generalized O / ON (or C /
		// CN) slice form and the legacy O0 / O1 / OS (or C0 / C1 / CS)
		// two-slot form are populated.
		if cond {
			if isOpen {
				if cap(rule.O) < matched {
					rule.O = make([]*Token, matched)
				} else {
					rule.O = rule.O[:matched]
				}
				for i := 0; i < matched; i++ {
					rule.O[i] = ctx.T[i]
				}
				rule.ON = matched
				rule.OS = matched
				if matched >= 1 {
					rule.O0 = rule.O[0]
				} else {
					rule.O0 = NoToken
				}
				if matched >= 2 {
					rule.O1 = rule.O[1]
				} else {
					rule.O1 = NoToken
				}
			} else {
				if cap(rule.C) < matched {
					rule.C = make([]*Token, matched)
				} else {
					rule.C = rule.C[:matched]
				}
				for i := 0; i < matched; i++ {
					rule.C[i] = ctx.T[i]
				}
				rule.CN = matched
				rule.CS = matched
				if matched >= 1 {
					rule.C0 = rule.C[0]
				} else {
					rule.C0 = NoToken
				}
				if matched >= 2 {
					rule.C1 = rule.C[1]
				} else {
					rule.C1 = NoToken
				}
			}

			if alt.C != nil {
				cond = alt.C(rule, ctx)
			}

			if cond {
				return alt, true
			}
		}

		// This alternate renegotiated a token and then failed anyway — put
		// the cut back, so the alternates and rules that follow see the
		// buffer as it was before this one touched it.
		if unI != -1 {
			lex.Unrelex(unSaved)
			ctx.setT(unI, unTkn)
			ctx.dropT(unI + 1)
			// Re-announce the RESTORED token to lex subscribers. The
			// recut fired an event when it was cut; without a matching
			// event for the undo, a position-keyed consumer would keep
			// the abandoned recut. After this, the newest event per
			// source position (with each kept token's span shadowing
			// older events inside it) is always the token the parse
			// proceeded with. Mirrors ts/src/rules.ts; contract in
			// ts/doc/api.md under tn.sub.
			if ctx != nil && 0 < len(ctx.LexSubs) {
				for _, sub := range ctx.LexSubs {
					sub(unTkn, rule, ctx)
				}
			}
			unI = -1
		}
	}

	// No alternate could use the token and it is a bad one: raise the
	// lexer's own error, exactly as the non-negotiated path does at fetch
	// time. Deferring that throw is what let the alternates try to re-cut
	// it; now that all of them have declined, the specific diagnostic is
	// the useful one.
	if relex && 0 < len(ctx.T) && ctx.T[0] != nil && ctx.T[0].Tin == TinBD &&
		lex.Err == nil {
		bad := ctx.T[0]
		je := makeTabnasError(bad.Why, bad.Src, lex.Src, bad.SI, bad.RI, bad.CI, lex.Config)
		lex.attachErrContext(je, rule, bad.Name, bad.Why)
		ctx.recordErr(je)
		lex.Err = je
	}

	return nil, false
}

// tinMatch reports whether tin is accepted by the tins list of an alt
// slot. The #AA (ANY) tin is a wildcard: a slot that lists TinAA accepts
// every token. This mirrors the TS engine, where normalt converts an
// `s:` entry containing #AA into the "no constraint" sentinel so the
// match check is skipped entirely (see ts/test/aa-wildcard.test.js).
func tinMatch(tin Tin, tins []Tin) bool {
	for _, t := range tins {
		if tin == t || t == TinAA {
			return true
		}
	}
	return false
}
