package tabnas

import (
	"fmt"
	"regexp"
	"strings"
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

// Undefined is a sentinel value distinguishing "no value" from nil (null).
// In TypeScript, undefined !== null. In Go, we use this sentinel.
type undefinedType struct{}

var Undefined any = &undefinedType{}

// IsUndefined checks if a value is the Undefined sentinel.
func IsUndefined(v any) bool {
	_, ok := v.(*undefinedType)
	return ok
}

// Skip is a sentinel value that acts as undefined in deep merge — the base
// value is preserved. Represented as "@SKIP" in grammar options.
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

// CondOp represents a comparison operator with a value for declarative conditions.
// Used in the CD field of AltSpec to define conditions declaratively,
// matching the TypeScript c: { 'n.pk': { $lte: 0 } } syntax.
type CondOp struct {
	Op  string
	Val int
}

// Comparison operator constructors for declarative conditions (AltSpec.CD field).
func CEq(val int) CondOp  { return CondOp{Op: "$eq", Val: val} }
func CNe(val int) CondOp  { return CondOp{Op: "$ne", Val: val} }
func CLt(val int) CondOp  { return CondOp{Op: "$lt", Val: val} }
func CLte(val int) CondOp { return CondOp{Op: "$lte", Val: val} }
func CGt(val int) CondOp  { return CondOp{Op: "$gt", Val: val} }
func CGte(val int) CondOp { return CondOp{Op: "$gte", Val: val} }

// AltSpec defines a parse alternate specification.
type AltSpec struct {
	S  [][]Tin                            // Token Tin sequences to match: s[0] for t0, s[1] for t1
	P  string                             // Push rule name (create child)
	R  string                             // Replace rule name (create sibling)
	B  int                                // Move token pointer backward (backtrack)
	C  AltCond                            // Custom condition (function)
	CD map[string]any                     // Declarative condition (converted to C by NormAlt)
	N  map[string]int                     // Counter increments
	A  AltAction                          // Match action
	U  map[string]any                     // Custom props added to Rule.u
	K  map[string]any                     // Custom props added to Rule.k (propagated)
	G  string                             // Named group tags (comma-separated)
	H  AltModifier                        // Alt modifier (called after match to potentially modify the alt)
	E  AltError                           // Error generation
	PF func(r *Rule, ctx *Context) string // Dynamic push rule name
	RF func(r *Rule, ctx *Context) string // Dynamic replace rule name
	BF func(r *Rule, ctx *Context) int    // Dynamic backtrack
}

// RuleSpec defines the specification for a parsing rule.
//
// The alternate lists (open/close) and lifecycle action lists (bo/ao/bc/ac)
// are unexported: like the TypeScript RuleSpec, they are mutated only
// through methods (AddOpen/PrependOpen/ModifyOpen/ClearOpen, AddBO/PrependBO/
// ClearActions, Fnref, …) and read through getters (OpenAlts/CloseAlts,
// Actions, HasBO/…). This keeps the engine's exposed API aligned with TS,
// where direct array assignment is not possible.
type RuleSpec struct {
	Name  string
	open  []*AltSpec
	close []*AltSpec
	bo    []StateAction // Before-open actions
	bc    []StateAction // Before-close actions
	ao    []StateAction // After-open actions
	ac    []StateAction // After-close actions

	// fnrefInstalled tracks which StateAction functions have already
	// been wired into each phase via wireStateActions, deduped by
	// function pointer. Prevents multiple Grammar() calls from stacking
	// duplicate state actions when they re-register the same handler
	// for the same reserved `@<rulename>-<phase>` slot.
	fnrefInstalled map[string]map[uintptr]bool

	// fnrefReplaced records phases an `@<rulename>-<phase>/replace` fnref
	// has taken ownership of. Once replaced, the plain/prepend/append
	// fnrefs for that phase are ignored so older handlers are not
	// re-installed on subsequent wireStateActions calls or re-derivation.
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

// AltModListOpts configures modifications for RuleSpec alternate lists.
// Matches the TS ListMods parameter to rs.open(alts, mods)/rs.close(alts, mods).
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

// MakeRuleCond creates an AltCond function from a comparison operator, property path, and value.
// Matches the TypeScript makeRuleCond(co, prop, subprop, val) function.
// When the property is not set (missing), the condition returns true.
func MakeRuleCond(op string, prop string, subprop string, val int) (AltCond, error) {
	switch op {
	case "$eq":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := getRuleProp(r, prop, subprop)
			return !ok || rval == val
		}, nil
	case "$ne":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := getRuleProp(r, prop, subprop)
			return !ok || rval != val
		}, nil
	case "$lt":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := getRuleProp(r, prop, subprop)
			return !ok || rval < val
		}, nil
	case "$lte":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := getRuleProp(r, prop, subprop)
			return !ok || rval <= val
		}, nil
	case "$gt":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := getRuleProp(r, prop, subprop)
			return !ok || rval > val
		}, nil
	case "$gte":
		return func(r *Rule, ctx *Context) bool {
			rval, ok := getRuleProp(r, prop, subprop)
			return !ok || rval >= val
		}, nil
	default:
		return nil, fmt.Errorf("MakeRuleCond: unknown comparison operator: %s", op)
	}
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

// Rule represents a rule instance during parsing.
type Rule struct {
	I      int
	Name   string
	Spec   *RuleSpec
	Node   any
	State  RuleState
	D      int
	Child  *Rule
	Parent *Rule
	Prev   *Rule
	Next   *Rule

	// Generalized per-position matched tokens. O[i] holds the token
	// matched at the i-th lookahead position during OPEN (mirroring C
	// for CLOSE). ON / CN give the number of matched positions. This
	// supersedes the legacy O0/O1/OS (and C0/C1/CS) two-slot fields,
	// which are still maintained below for backward compatibility.
	O  []*Token
	ON int
	C  []*Token
	CN int

	// Legacy two-slot aliases. Kept in sync with O[0..1] / C[0..1] by
	// ParseAlts so existing grammar code and plugins that read r.O0,
	// r.O1, r.C0, r.C1, r.OS, r.CS continue to work unchanged.
	O0 *Token
	O1 *Token
	C0 *Token
	C1 *Token
	OS int
	CS int

	N   map[string]int
	U   map[string]any
	K   map[string]any
	Why string
}

// NoRule is a sentinel rule.
// Node is Undefined (like TS where NORULE.node = undefined).
var NoRule *Rule

func init() {
	NoRule = &Rule{Name: "norule", I: -1, State: OPEN, Node: Undefined,
		N: make(map[string]int), U: make(map[string]any), K: make(map[string]any)}
}

// Eq checks if counter equals limit (nil/missing → true).
func (r *Rule) Eq(counter string, limit int) bool {
	val, ok := r.N[counter]
	return !ok || val == limit
}

// Lt checks if counter < limit (nil/missing → true).
func (r *Rule) Lt(counter string, limit int) bool {
	val, ok := r.N[counter]
	return !ok || val < limit
}

// Gt checks if counter > limit (nil/missing → true).
func (r *Rule) Gt(counter string, limit int) bool {
	val, ok := r.N[counter]
	return !ok || val > limit
}

// Lte checks if counter <= limit (nil/missing → true).
func (r *Rule) Lte(counter string, limit int) bool {
	val, ok := r.N[counter]
	return !ok || val <= limit
}

// Gte checks if counter >= limit (nil/missing → true).
func (r *Rule) Gte(counter string, limit int) bool {
	val, ok := r.N[counter]
	return !ok || val >= limit
}

// MakeRule creates a new Rule from a RuleSpec.
func MakeRule(spec *RuleSpec, ctx *Context, node any) *Rule {
	r := &Rule{
		I: ctx.UI, Name: spec.Name, Spec: spec, Node: node,
		State: OPEN, D: ctx.RSI,
		Child: NoRule, Parent: NoRule, Prev: NoRule, Next: NoRule,
		O: nil, ON: 0, C: nil, CN: 0,
		O0: NoToken, O1: NoToken, C0: NoToken, C1: NoToken,
		N: make(map[string]int), U: make(map[string]any), K: make(map[string]any),
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
		return next
	}

	// Alt modifier
	if alt != nil && alt.H != nil {
		alt = alt.H(alt, r, ctx)
	}

	// Error check: if alt.E returns a token, signal a parse error.
	if alt != nil && alt.E != nil {
		errTkn := alt.E(r, ctx)
		if errTkn != nil {
			ctx.ParseErr = errTkn
		}
	}

	// Update counters
	if alt != nil && alt.N != nil {
		for cn, cv := range alt.N {
			if cv == 0 {
				r.N[cn] = 0
			} else {
				if _, ok := r.N[cn]; !ok {
					r.N[cn] = 0
				}
				r.N[cn] += cv
			}
		}
	}

	// Set custom properties
	if alt != nil && alt.U != nil {
		for k, v := range alt.U {
			r.U[k] = v
		}
	}
	if alt != nil && alt.K != nil {
		for k, v := range alt.K {
			r.K[k] = v
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
				for k, v := range r.N {
					next.N[k] = v
				}
				if len(r.K) > 0 {
					for k, v := range r.K {
						next.K[k] = v
					}
				}
			}
		} else if replaceName != "" {
			rulespec, ok := ctx.RSM[replaceName]
			if ok {
				next = MakeRule(rulespec, ctx, r.Node)
				next.Parent = r.Parent
				next.Prev = r
				for k, v := range r.N {
					next.N[k] = v
				}
				if len(r.K) > 0 {
					for k, v := range r.K {
						next.K[k] = v
					}
				}
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
func ParseAlts(isOpen bool, alts []*AltSpec, lex *Lex, rule *Rule, ctx *Context) (*AltSpec, bool) {
	if len(alts) == 0 {
		return nil, false
	}

	for _, alt := range alts {
		matched := 0
		cond := true

		sN := len(alt.S)
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
				if len(ctx.LexSubs) > 0 {
					for _, sub := range ctx.LexSubs {
						sub(tkn, rule, ctx)
					}
				}
			}

			// Empty alt.S[i] means "no Tin constraint at this position"
			// (wildcard) - the token is still fetched and consumed but
			// the match check is skipped. This prevents silently
			// dropping the check at a later required position.
			if len(alt.S[i]) != 0 {
				if !tinMatch(ctx.T[i].Tin, alt.S[i]) {
					cond = false
					break
				}
			}
			matched = i + 1
		}

		// Record the matched tokens on the rule. Both the generalized
		// O / ON (or C / CN) slice form and the legacy O0 / O1 / OS
		// (or C0 / C1 / CS) two-slot form are populated.
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

		if cond && alt.C != nil {
			cond = alt.C(rule, ctx)
		}

		if cond {
			return alt, true
		}
	}

	return nil, false
}

func tinMatch(tin Tin, tins []Tin) bool {
	for _, t := range tins {
		if tin == t {
			return true
		}
	}
	return false
}
