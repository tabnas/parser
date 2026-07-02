// Copyright (c) 2013-2026 Richard Rodger, MIT License

package tabnas

import (
	"fmt"
	"sort"
	"strings"
)

// Plugin modifies a Tabnas instance (tokens, matchers, rules); errors on init failure.
type Plugin func(j *Tabnas, opts map[string]any) error

// LexMatcher is a custom lexer matcher: returns a Token (advancing the cursor) or nil to pass.
// The rule parameter allows context-sensitive lexing (e.g. rule.K, rule.U, rule.N, rule.State).
type LexMatcher func(lex *Lex, rule *Rule) *Token

// MakeLexMatcher is a factory that builds a LexMatcher from the lex config and options.
type MakeLexMatcher func(cfg *LexConfig, opts *Options) LexMatcher

// MatchSpec is a custom matcher registered via options (TS: { order, make }).
type MatchSpec struct {
	Order int            // Priority order (lower runs first).
	Make  MakeLexMatcher // Factory that builds the matcher.
}

// MatcherEntry is a named custom matcher ordered by priority (lower runs first).
// Built-ins: fixed=2e6, space=3e6, line=4e6, string=5e6, comment=6e6, number=7e6, text=8e6.
type MatcherEntry struct {
	Name     string     // Matcher name (unique key).
	Priority int        // Ordering priority; < 2e6 runs before all built-ins.
	Match    LexMatcher // The matcher function.
}

// RuleDefiner modifies a RuleSpec, adding alternates, actions, or conditions to a grammar rule.
type RuleDefiner func(rs *RuleSpec, p *Parser)

// LexSub is a subscriber callback invoked after each token is lexed.
type LexSub func(tkn *Token, rule *Rule, ctx *Context)

// RuleSub is a subscriber callback invoked after each rule step.
type RuleSub func(rule *Rule, ctx *Context)

// pluginEntry stores a registered plugin and its options.
type pluginEntry struct {
	plugin Plugin         // The registered plugin function.
	opts   map[string]any // Options passed to the plugin.
}

// internalError converts a recovered panic value into an "internal"-code
// *TabnasError, so error-returning public APIs uphold the no-panic
// guarantee: any panic (including from plugin callbacks or grammar specs)
// surfaces as a returned error rather than crashing the caller.
func (j *Tabnas) internalError(api string, r any) error {
	return j.parser.makeError("internal", fmt.Sprintf("%s: %v", api, r), "", 0, 1, 1)
}

// Use registers and invokes a plugin on this Tabnas instance.
// The plugin function is called with the Tabnas instance and optional options.
// Returns an error if the plugin fails to initialize.
//
// Example:
//
//	j := tabnas.Make()
//	err := j.Use(myPlugin, map[string]any{"key": "value"})
func (j *Tabnas) Use(plugin Plugin, opts ...map[string]any) (err error) {
	// Plugin code is arbitrary; a panic in it becomes an "internal" error.
	defer func() {
		if r := recover(); r != nil {
			err = j.internalError("Use", r)
		}
	}()

	var pluginOpts map[string]any
	if len(opts) > 0 && opts[0] != nil {
		pluginOpts = opts[0]
	}

	j.plugins = append(j.plugins, pluginEntry{plugin: plugin, opts: pluginOpts})
	return plugin(j, pluginOpts)
}

// UseDefaults registers and invokes a plugin, merging default options with
// user-provided options before calling the plugin. This matches the TS pattern
// where plugins have a .defaults property:
//
//	deep({}, plugin.defaults || {}, plugin_options || {})
//
// Example:
//
//	j := tabnas.Make()
//	err := j.UseDefaults(hoover.Hoover, hoover.Defaults, map[string]any{...})
func (j *Tabnas) UseDefaults(plugin Plugin, defaults map[string]any, opts ...map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = j.internalError("UseDefaults", r)
		}
	}()

	pluginOpts := Deep(map[string]any{}, defaults).(map[string]any)
	if len(opts) > 0 && opts[0] != nil {
		pluginOpts = Deep(pluginOpts, opts[0]).(map[string]any)
	}

	j.plugins = append(j.plugins, pluginEntry{plugin: plugin, opts: pluginOpts})
	return plugin(j, pluginOpts)
}

// Rule modifies or creates a grammar rule by name.
// The definer callback receives the RuleSpec and can modify its Open/Close
// alternates, and BO/BC/AO/AC state actions.
//
// If the rule does not exist, a new empty RuleSpec is created.
// A nil definer deletes the rule from the rule-spec map, if present
// (mirrors the TypeScript rule(name, null) idiom). Deleting an absent
// rule is a no-op.
// Returns the Tabnas instance for chaining.
//
// Example:
//
//	j.Rule("val", func(rs *RuleSpec, p *Parser) {
//	    rs.open = append([]*AltSpec{{
//	        S: [][]Tin{{myToken}},
//	        A: func(r *Rule, ctx *Context) { r.Node = "custom" },
//	    }}, rs.open...)
//	})
func (j *Tabnas) Rule(name string, definer RuleDefiner) *Tabnas {
	// A nil definer deletes the rule (mirrors TS rule(name, null)).
	if definer == nil {
		delete(j.parser.RSM, name)
		return j
	}
	rs := j.parser.RSM[name]
	if rs == nil {
		rs = &RuleSpec{Name: name}
		j.parser.RSM[name] = rs
	}
	definer(rs, j.parser)
	return j
}

// Token registers a new token type or looks up an existing one.
// With just a name, it returns the Tin for an existing token.
// With a name and source character(s), it registers a new fixed token.
//
// Returns the Tin (token identification number) for the token.
//
// Example:
//
//	// Register a new fixed token
//	TT := j.Token("#TL", "~")
//
//	// Look up existing token
//	OB := j.Token("#OB", "")
func (j *Tabnas) Token(name string, src ...string) Tin {
	// Look up existing token by name.
	if tin, ok := j.tinByName[name]; ok {
		// If src provided, update the fixed token mapping.
		if len(src) > 0 && src[0] != "" {
			if j.parser.Config.FixedTokens == nil {
				j.parser.Config.FixedTokens = make(map[string]Tin)
			}
			j.parser.Config.FixedTokens[src[0]] = tin
			j.parser.Config.SortFixedTokens()
		}
		return tin
	}

	// Allocate a new Tin.
	tin := j.nextTin
	j.nextTin++

	j.tinByName[name] = tin
	j.nameByTin[tin] = name

	// Also store in the config's TinNames for lexer access.
	if j.parser.Config.TinNames == nil {
		j.parser.Config.TinNames = make(map[Tin]string)
	}
	j.parser.Config.TinNames[tin] = name

	// Register as fixed token if src provided.
	if len(src) > 0 && src[0] != "" {
		if j.parser.Config.FixedTokens == nil {
			j.parser.Config.FixedTokens = make(map[string]Tin)
		}
		j.parser.Config.FixedTokens[src[0]] = tin
		j.parser.Config.SortFixedTokens()
	}

	return tin
}

// FixedSrc returns the Tin for a fixed token source string (e.g. "{" → TinOB).
// Returns 0 if the source string is not a fixed token.
// Matches TS `tabnas.fixed('b')`.
func (j *Tabnas) FixedSrc(src string) Tin {
	if tin, ok := j.parser.Config.FixedTokens[src]; ok {
		return tin
	}
	return 0
}

// FixedTin returns the source string for a fixed token Tin (e.g. TinOB → "{").
// Returns "" if the Tin is not a fixed token.
// Matches TS `tabnas.fixed(18)`.
func (j *Tabnas) FixedTin(tin Tin) string {
	for src, t := range j.parser.Config.FixedTokens {
		if t == tin {
			return src
		}
	}
	return ""
}

// registerMatchSpecs materializes the factories in opts.Lex.Match into
// LexMatchers and appends them to Config.CustomMatchers, keeping the slice
// sorted by priority (lower first). Built-in matcher priorities:
//
//	fixed=2000000, space=3000000, line=4000000, string=5000000,
//	comment=6000000, number=7000000, text=8000000
//
// Use Order < 2000000 to run before all built-ins (matching TS match behavior).
func (j *Tabnas) registerMatchSpecs(opts *Options) {
	if opts.Lex == nil || opts.Lex.Match == nil {
		return
	}
	byName := make(map[string]int, len(j.parser.Config.CustomMatchers))
	for i, m := range j.parser.Config.CustomMatchers {
		byName[m.Name] = i
	}
	for name, spec := range opts.Lex.Match {
		if spec == nil || spec.Make == nil {
			continue
		}
		matcher := spec.Make(j.parser.Config, j.options)
		if matcher == nil {
			continue
		}
		entry := &MatcherEntry{Name: name, Priority: spec.Order, Match: matcher}
		if idx, ok := byName[name]; ok {
			j.parser.Config.CustomMatchers[idx] = entry
		} else {
			j.parser.Config.CustomMatchers = append(j.parser.Config.CustomMatchers, entry)
			byName[name] = len(j.parser.Config.CustomMatchers) - 1
		}
	}
	// Tie-break equal priorities by name: map iteration order is random,
	// so without this the order of same-priority matchers would vary
	// between runs (and between merge directions in (*Tabnas).Merge).
	sort.SliceStable(j.parser.Config.CustomMatchers, func(i, k int) bool {
		mi, mk := j.parser.Config.CustomMatchers[i], j.parser.Config.CustomMatchers[k]
		if mi.Priority != mk.Priority {
			return mi.Priority < mk.Priority
		}
		return mi.Name < mk.Name
	})
}

// applyMatchTokens resolves match.token / match.tokenFn names to Tins and
// registers the matchers (TS match.token: RegExp | LexMatcher). Called at
// construction (Make) and on SetOptions.
func (j *Tabnas) applyMatchTokens(opts *Options) {
	if opts.Match == nil || (opts.Match.Token == nil && opts.Match.TokenFn == nil) {
		return
	}
	cfg := j.parser.Config
	// Allocate Tins in a deterministic order so the lexer's Tin-ascending
	// match-token iteration reflects a stable precedence (overlapping eager
	// tokens — e.g. a range regex vs a single-char case-insensitive literal
	// — otherwise resolve by random map-iteration order). TokenOrder names
	// come first in caller order; remaining keys follow, sorted by name.
	var order []string
	seen := map[string]bool{}
	for _, name := range opts.Match.TokenOrder {
		if _, ok := opts.Match.Token[name]; ok && !seen[name] {
			order = append(order, name)
			seen[name] = true
		}
	}
	rest := make([]string, 0, len(opts.Match.Token))
	for name := range opts.Match.Token {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)
	for _, name := range order {
		re := opts.Match.Token[name]
		tin := j.Token(name)
		cfg.MatchTokens[tin] = re
		if opts.Match.TokenEager[name] {
			if cfg.MatchTokensEager == nil {
				cfg.MatchTokensEager = make(map[Tin]bool)
			}
			cfg.MatchTokensEager[tin] = true
		}
	}
	for name, fn := range opts.Match.TokenFn {
		tin := j.Token(name)
		if cfg.MatchTokenFns == nil {
			cfg.MatchTokenFns = make(map[Tin]LexMatcher)
		}
		cfg.MatchTokenFns[tin] = fn
	}
	// Project maps → sorted slice so lex-time iteration is deterministic.
	cfg.RebuildMatchTokensSorted()
}

// applyFixedTokens updates the lexer's fixed-token table from opts.Fixed.Token.
// Keys are token names, values are pointers to the intended source string:
//   - non-nil: remove any existing src→tin mapping for that name, then set
//     *srcPtr → tin. Unknown names are registered as new tokens.
//   - nil: remove any existing src→tin mapping(s) for that name.
//
// Mirrors the TS `options.fixed.token` semantics (StrMap with null = delete).
func (j *Tabnas) applyFixedTokens(opts *Options) {
	if opts.Fixed == nil || opts.Fixed.Token == nil {
		return
	}
	if j.parser.Config.FixedTokens == nil {
		j.parser.Config.FixedTokens = make(map[string]Tin)
	}
	changed := false
	for name, srcPtr := range opts.Fixed.Token {
		tin, ok := j.tinByName[name]
		if !ok {
			if srcPtr == nil {
				continue // nothing to delete for an unknown name
			}
			tin = j.Token(name) // allocate
		}
		for src, t := range j.parser.Config.FixedTokens {
			if t == tin {
				delete(j.parser.Config.FixedTokens, src)
				changed = true
			}
		}
		if srcPtr != nil && *srcPtr != "" {
			j.parser.Config.FixedTokens[*srcPtr] = tin
			changed = true
		}
	}
	if changed {
		j.parser.Config.SortFixedTokens()
	}
}

// Plugins returns the list of installed plugins (for introspection).
func (j *Tabnas) Plugins() []Plugin {
	out := make([]Plugin, len(j.plugins))
	for i, pe := range j.plugins {
		out[i] = pe.plugin
	}
	return out
}

// Config returns the parser's LexConfig for direct inspection or modification.
// Use with care — prefer Token(), Rule(), and options.lex.match for most work.
func (j *Tabnas) Config() *LexConfig {
	return j.parser.Config
}

// RSM returns the rule spec map for direct inspection or modification.
func (j *Tabnas) RSM() map[string]*RuleSpec {
	return j.parser.RSM
}

// TinName returns the name for a Tin value, checking both built-in and custom tokens.
func (j *Tabnas) TinName(tin Tin) string {
	if name, ok := j.nameByTin[tin]; ok {
		return name
	}
	return tinName(tin)
}

// TokenSet returns a named set of Tin values.
// Built-in sets: "IGNORE" (space, line, comment), "VAL" (text, number, string, value),
// "KEY" (text, number, string, value).
// Custom sets can be added via SetTokenSet.
// Returns nil if the set name is not recognized.
func (j *Tabnas) TokenSet(name string) []Tin {
	// Check custom sets first.
	if j.customTokenSets != nil {
		if tins, ok := j.customTokenSets[name]; ok {
			result := make([]Tin, len(tins))
			copy(result, tins)
			return result
		}
	}
	switch name {
	case "IGNORE":
		ignoreSet := j.parser.Config.IgnoreSet
		tins := make([]Tin, 0, len(ignoreSet))
		for tin := range ignoreSet {
			tins = append(tins, tin)
		}
		return tins
	case "VAL":
		valSet := j.parser.Config.ValSet
		result := make([]Tin, len(valSet))
		copy(result, valSet)
		return result
	case "KEY":
		keySet := j.parser.Config.KeySet
		result := make([]Tin, len(keySet))
		copy(result, keySet)
		return result
	default:
		return nil
	}
}

// SetTokenSet registers a custom named token set.
// Matches TS options.tokenSet.
// Also updates the per-instance config sets (IgnoreSet, ValSet, KeySet)
// to keep them in sync, matching TS cfg.tokenSetTins behavior.
func (j *Tabnas) SetTokenSet(name string, tins []Tin) {
	if j.customTokenSets == nil {
		j.customTokenSets = make(map[string][]Tin)
	}
	j.customTokenSets[name] = tins

	// Keep per-instance config sets in sync (matching TS cfg.tokenSetTins).
	switch name {
	case "IGNORE":
		ignoreSet := make(map[Tin]bool, len(tins))
		for _, tin := range tins {
			ignoreSet[tin] = true
		}
		j.parser.Config.IgnoreSet = ignoreSet
	case "VAL":
		copied := make([]Tin, len(tins))
		copy(copied, tins)
		j.parser.Config.ValSet = copied
	case "KEY":
		copied := make([]Tin, len(tins))
		copy(copied, tins)
		j.parser.Config.KeySet = copied
	}
}

// Sub subscribes to lex and/or rule events.
// LexSub fires after each non-ignored token is lexed.
// RuleSub fires after each rule processing step.
// Returns the Tabnas instance for chaining.
func (j *Tabnas) Sub(lexSub LexSub, ruleSub RuleSub) *Tabnas {
	if lexSub != nil {
		j.lexSubs = append(j.lexSubs, lexSub)
	}
	if ruleSub != nil {
		j.ruleSubs = append(j.ruleSubs, ruleSub)
	}
	return j
}

// Derive creates a new Tabnas instance inheriting this instance's config,
// rules, plugins, and custom tokens. Changes to the child do not affect the parent.
// This matches TypeScript's tabnas.make(options, parent).
func (j *Tabnas) Derive(opts ...Options) (result *Tabnas, err error) {
	// Re-applies plugins (arbitrary code) and deep-merges options; a panic
	// in either becomes an "internal" error rather than crashing.
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = j.internalError("Derive", r)
		}
	}()

	// Inherit the parent's accumulated options, with the derive options
	// merged on top (derive options win). Mirrors TS make(), where the
	// child is built from deep(parent.merged, opts) — so settings like
	// rule.start, number/string/comment config, etc. carry to the child
	// without each plugin having to re-apply them.
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if j.options != nil {
		o = Deep(*j.options, o).(Options)
	}
	child := Make(o)

	// Copy parent's custom fixed tokens.
	for k, v := range j.parser.Config.FixedTokens {
		child.parser.Config.FixedTokens[k] = v
	}
	child.parser.Config.SortFixedTokens()

	// Copy parent's custom token names.
	for k, v := range j.tinByName {
		child.tinByName[k] = v
	}
	for k, v := range j.nameByTin {
		child.nameByTin[k] = v
	}
	if child.nextTin < j.nextTin {
		child.nextTin = j.nextTin
	}

	// Copy TinNames into child config.
	if j.parser.Config.TinNames != nil {
		if child.parser.Config.TinNames == nil {
			child.parser.Config.TinNames = make(map[Tin]string)
		}
		for k, v := range j.parser.Config.TinNames {
			child.parser.Config.TinNames[k] = v
		}
	}

	// Copy parent's custom matchers.
	for _, m := range j.parser.Config.CustomMatchers {
		child.parser.Config.CustomMatchers = append(child.parser.Config.CustomMatchers, m)
	}

	// Copy parent's ender chars.
	if j.parser.Config.EnderChars != nil {
		if child.parser.Config.EnderChars == nil {
			child.parser.Config.EnderChars = make(map[rune]bool)
		}
		for k, v := range j.parser.Config.EnderChars {
			child.parser.Config.EnderChars[k] = v
		}
	}

	// Copy parent's escape map.
	if j.parser.Config.EscapeMap != nil {
		if child.parser.Config.EscapeMap == nil {
			child.parser.Config.EscapeMap = make(map[string]string)
		}
		for k, v := range j.parser.Config.EscapeMap {
			child.parser.Config.EscapeMap[k] = v
		}
	}

	// Copy custom token sets.
	if j.customTokenSets != nil {
		if child.customTokenSets == nil {
			child.customTokenSets = make(map[string][]Tin)
		}
		for name, tins := range j.customTokenSets {
			copied := make([]Tin, len(tins))
			copy(copied, tins)
			child.customTokenSets[name] = copied
		}
	}

	// Copy per-instance token sets (matching TS cfg.tokenSetTins inheritance).
	if j.parser.Config.IgnoreSet != nil {
		child.parser.Config.IgnoreSet = make(map[Tin]bool, len(j.parser.Config.IgnoreSet))
		for k, v := range j.parser.Config.IgnoreSet {
			child.parser.Config.IgnoreSet[k] = v
		}
	}
	if j.parser.Config.ValSet != nil {
		child.parser.Config.ValSet = make([]Tin, len(j.parser.Config.ValSet))
		copy(child.parser.Config.ValSet, j.parser.Config.ValSet)
	}
	if j.parser.Config.KeySet != nil {
		child.parser.Config.KeySet = make([]Tin, len(j.parser.Config.KeySet))
		copy(child.parser.Config.KeySet, j.parser.Config.KeySet)
	}

	// Re-apply parent's plugins on the child.
	for _, pe := range j.plugins {
		child.plugins = append(child.plugins, pe)
		if err := pe.plugin(child, pe.opts); err != nil {
			// Mirrors TS make(), where a plugin throwing during child
			// derivation propagates to the caller.
			return nil, fmt.Errorf("tabnas: plugin error during Derive: %w", err)
		}
	}

	// Copy subscriptions.
	child.lexSubs = append(child.lexSubs, j.lexSubs...)
	child.ruleSubs = append(child.ruleSubs, j.ruleSubs...)

	// Copy decorations (TS: parent properties inherited by child).
	if j.decorations != nil {
		if child.decorations == nil {
			child.decorations = make(map[string]any)
		}
		for k, v := range j.decorations {
			child.decorations[k] = v
		}
	}

	// Copy plugin options namespace.
	if j.pluginOpts != nil {
		if child.pluginOpts == nil {
			child.pluginOpts = make(map[string]map[string]any)
		}
		for name, opts := range j.pluginOpts {
			copied := make(map[string]any, len(opts))
			for k, v := range opts {
				copied[k] = v
			}
			child.pluginOpts[name] = copied
		}
	}

	return child, nil
}

// SetOptions deep-merges new options into this instance and rebuilds the
// config. Existing grammar rules (including plugin modifications) are
// preserved — matching the TypeScript clone/inherit pattern where
// options() does not rebuild the grammar.
// When called from within a plugin (during re-apply), skips plugin
// re-application to avoid infinite recursion.
// Returns the instance for chaining.
func (j *Tabnas) SetOptions(opts Options) *Tabnas {
	merged := Deep(*j.options, opts).(Options)
	j.options = &merged

	// Rebuild config from merged options.
	cfg := buildConfig(j.options)

	// Preserve per-instance state.
	cfg.FixedTokens = j.parser.Config.FixedTokens
	cfg.FixedSorted = j.parser.Config.FixedSorted
	cfg.TinNames = j.parser.Config.TinNames
	cfg.CustomMatchers = j.parser.Config.CustomMatchers
	// Preserve token sets. buildConfig unconditionally resets IgnoreSet/
	// ValSet/KeySet to defaults, which would clobber SetTokenSet mutations
	// made by plugins or prior calls. opts.TokenSet below reapplies any
	// explicit override from the incoming options.
	if j.parser.Config.IgnoreSet != nil {
		cfg.IgnoreSet = j.parser.Config.IgnoreSet
	}
	if j.parser.Config.ValSet != nil {
		cfg.ValSet = j.parser.Config.ValSet
	}
	if j.parser.Config.KeySet != nil {
		cfg.KeySet = j.parser.Config.KeySet
	}
	// Preserve match token/value entries added by prior SetOptions/Grammar calls.
	if len(j.parser.Config.MatchTokens) > 0 {
		for k, v := range j.parser.Config.MatchTokens {
			cfg.MatchTokens[k] = v
		}
	}
	// Preserve the per-tin eager flags alongside the tokens, so an eager
	// match token registered at Make() (or an earlier SetOptions) keeps
	// firing at non-leading slots after a later SetOptions rebuilds cfg.
	if len(j.parser.Config.MatchTokensEager) > 0 {
		if cfg.MatchTokensEager == nil {
			cfg.MatchTokensEager = make(map[Tin]bool, len(j.parser.Config.MatchTokensEager))
		}
		for k, v := range j.parser.Config.MatchTokensEager {
			cfg.MatchTokensEager[k] = v
		}
	}
	if len(j.parser.Config.MatchTokenFns) > 0 {
		for k, v := range j.parser.Config.MatchTokenFns {
			cfg.MatchTokenFns[k] = v
		}
	}
	if len(j.parser.Config.MatchValues) > 0 {
		cfg.MatchValues = append(cfg.MatchValues, j.parser.Config.MatchValues...)
		// Re-sort: the preserved entries may break the name-ascending order
		// built by buildConfig. Keep lex-time iteration deterministic.
		sort.Slice(cfg.MatchValues, func(i, k int) bool {
			return cfg.MatchValues[i].Name < cfg.MatchValues[k].Name
		})
	}
	// Re-project the merged MatchTokens map to its sorted view.
	cfg.RebuildMatchTokensSorted()

	// Update config in-place to preserve pointer identity.
	// Grammar closures capture the original *LexConfig pointer, so updating
	// the object they point to (rather than replacing it) ensures they see
	// the new config values. This matches TS behavior where configure()
	// mutates the existing config and parser.clone() inherits the rules.
	*j.parser.Config = *cfg

	// Do NOT rebuild grammar — preserve existing RSM with user rule
	// modifications. This matches TS where options() calls parser.clone()
	// which inherits existing rules rather than rebuilding from scratch.
	//
	// Do NOT re-apply plugins here either: TS #setOptions does not re-run
	// plugins (only the make()/derive path does, on the child). Re-running
	// them caused a plugin whose body calls SetOptions to be applied twice
	// from a single Use(). Plugin re-application lives only in Derive.

	// Apply lex.match specs: create matchers from MakeLexMatcher factories.
	j.registerMatchSpecs(&opts)

	// Apply fixed.token overrides (add, swap, or delete fixed-token mappings).
	j.applyFixedTokens(&opts)

	// Apply match.token / match.tokenFn: resolve token names to Tins and
	// register regexp or function matchers.
	j.applyMatchTokens(&opts)

	// Apply tokenSet: resolve token names and update per-instance sets.
	if opts.TokenSet != nil {
		for setName, names := range opts.TokenSet {
			var tins []Tin
			for _, name := range names {
				if name == "" {
					continue
				}
				resolved := j.resolveTokenName(name)
				tins = append(tins, resolved...)
			}
			j.SetTokenSet(setName, tins)
		}
	}

	// Re-alias the parser error fields to the rebuilt config maps.
	// buildConfig resolved Error/Hint/ErrMsg from the merged options.
	j.parser.ErrorMessages = j.parser.Config.ErrorMessages
	j.parser.Hints = j.parser.Config.Hints
	j.parser.ErrTag = j.parser.Config.ErrTag
	j.hints = j.parser.Config.Hints

	// Apply lex options (empty source handling).
	// Uses merged options so that values set at Make() or via prior
	// SetOptions/Grammar calls are preserved.
	if j.options.Lex != nil {
		if j.options.Lex.Empty != nil {
			j.emptyAllow = *j.options.Lex.Empty
		}
		j.emptyResult = j.options.Lex.EmptyResult
	}

	// Apply custom parser start.
	if j.options.Parser != nil && j.options.Parser.Start != nil {
		j.parserStart = j.options.Parser.Start
	}

	// Apply rule include first, then exclude. Only apply from the
	// incoming opts (not merged) to avoid re-filtering groups that
	// earlier SetOptions/Grammar calls already handled.
	if opts.Rule != nil && opts.Rule.Include != "" {
		j.include(opts.Rule.Include)
	}
	if opts.Rule != nil && opts.Rule.Exclude != "" {
		j.exclude(opts.Rule.Exclude)
	}

	return j
}

// textParser parses tabnas-format text for the text-form convenience
// APIs (SetOptionsText, GrammarText). The engine ships no grammar
// (matching the TS package), so a grammar package must register a
// parser via RegisterTextParser, in the manner of database/sql drivers.
var textParser func(src string) (any, error)

// RegisterTextParser sets the parser used by SetOptionsText and
// GrammarText. Grammar packages call this from init(); the last
// registration wins.
func RegisterTextParser(p func(src string) (any, error)) {
	textParser = p
}

// parseText runs the registered text parser, or errors when none is
// registered (e.g. the engine is used without a grammar package).
func parseText(api, text string) (any, error) {
	if textParser == nil {
		return nil, fmt.Errorf(
			"%s: no text parser registered — call RegisterTextParser with a grammar package's parser", api)
	}
	return textParser(text)
}

// SetOptionsText parses a tabnas-format options string, converts it to an
// Options struct via MapToOptions, and applies it via SetOptions.
// Returns the instance for chaining and any parse error encountered.
// Complement to SetOptions that accepts a textual specification of the
// desired options tree. Requires a registered text parser (see
// RegisterTextParser).
func (j *Tabnas) SetOptionsText(text string) (result *Tabnas, err error) {
	// Text parsing + MapToOptions + SetOptions can panic on malformed
	// input; convert any panic into an "internal" error.
	defer func() {
		if r := recover(); r != nil {
			result = j
			err = j.internalError("SetOptionsText", r)
		}
	}()

	if text == "" {
		return j, nil
	}
	parsed, err := parseText("SetOptionsText", text)
	if err != nil {
		return j, err
	}
	if parsed == nil {
		return j, nil
	}
	m, ok := parsed.(map[string]any)
	if !ok {
		return j, fmt.Errorf("SetOptionsText: expected map, got %T", parsed)
	}
	j.SetOptions(MapToOptions(m))
	return j, nil
}

// include keeps only grammar alternates whose G tags intersect the
// given group names. Group names are comma-separated in AltSpec.G
// fields and in the supplied arguments. Alts with no G tags are
// dropped whenever the include set is non-empty.
// Use rule.include option to opt alts into the grammar (e.g. "json"
// for strict-JSON mode where plugins pre-tag their alts with "json").
func (j *Tabnas) include(groups ...string) *Tabnas {
	includeSet := buildTagSet(groups)
	if len(includeSet) == 0 {
		return j
	}
	for _, rs := range j.parser.RSM {
		rs.open = filterAltsInclude(rs.open, includeSet)
		rs.close = filterAltsInclude(rs.close, includeSet)
	}
	return j
}

// exclude removes grammar alternates tagged with any of the given group names.
// Group names are comma-separated in AltSpec.G fields.
// Use rule.exclude option to strip groups (e.g. "tabnas" for strict JSON).
func (j *Tabnas) exclude(groups ...string) *Tabnas {
	excludeSet := buildTagSet(groups)

	for _, rs := range j.parser.RSM {
		rs.open = filterAlts(rs.open, excludeSet)
		rs.close = filterAlts(rs.close, excludeSet)
	}
	return j
}

// buildTagSet parses one or more comma-separated tag strings into a set.
func buildTagSet(groups []string) map[string]bool {
	out := make(map[string]bool)
	for _, g := range groups {
		for _, part := range strings.Split(g, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out[part] = true
			}
		}
	}
	return out
}

// filterAlts removes alternates whose G tags overlap with the exclude set.
func filterAlts(alts []*AltSpec, excludeSet map[string]bool) []*AltSpec {
	result := make([]*AltSpec, 0, len(alts))
	for _, alt := range alts {
		if alt.G == "" {
			result = append(result, alt)
			continue
		}
		excluded := false
		for _, tag := range strings.Split(alt.G, ",") {
			tag = strings.TrimSpace(tag)
			if excludeSet[tag] {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, alt)
		}
	}
	return result
}

// filterAltsInclude keeps only alternates whose G tags intersect the
// include set. Alts with no G tags are dropped when includeSet is
// non-empty; callers should short-circuit for the empty-set case.
func filterAltsInclude(alts []*AltSpec, includeSet map[string]bool) []*AltSpec {
	result := make([]*AltSpec, 0, len(alts))
	for _, alt := range alts {
		if alt.G == "" {
			continue
		}
		kept := false
		for _, tag := range strings.Split(alt.G, ",") {
			tag = strings.TrimSpace(tag)
			if includeSet[tag] {
				kept = true
				break
			}
		}
		if kept {
			result = append(result, alt)
		}
	}
	return result
}

// ParseMeta parses a tabnas string with metadata passed through to the parse context.
// The meta map is accessible in rule actions/conditions via ctx.Meta.
func (j *Tabnas) ParseMeta(src string, meta map[string]any) (any, error) {
	return j.parseInternal(src, meta)
}
