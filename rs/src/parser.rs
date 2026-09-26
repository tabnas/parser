// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::builtins::run_builtin_action_with_info;
use crate::context::{Context, ContextSeed, InstanceInfo};
use crate::error::TabnasError;
use crate::lexer::{compile_number_exclude, Lexer, RelexCheckpoint};
use crate::options::Options;
use crate::rule::{
    resolved_action_order, resolved_alt_action_order, ActionBinding, AltActionBinding, AltMatch,
    AltSpec, CompareOp, Condition, Rule, RuleDone, RuleDoneAlt, RuleName, RuleSnapshot, RuleSpec,
    RuleState, StateAction,
};
use crate::token::{Tin, Token, TIN_AA, TIN_BD, TIN_ZZ};
use crate::value::Value;
use crate::{
    Action, AltAction, ContextAction, LexSubscriber, RuleDoneSubscriber, RuleSubscriber,
    TokenSubscriber,
};
use indexmap::IndexMap;
use std::borrow::Cow;
use std::collections::{BTreeSet, HashMap};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::rc::Rc;
use std::sync::Arc;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Continuations {
    pub tins: Vec<Tin>,
    pub tokens: Vec<String>,
}

#[derive(Debug, Clone)]
pub struct ParseRecovery {
    pub value: Option<Value>,
    pub errors: Vec<TabnasError>,
    pub fatal: Option<TabnasError>,
}

#[derive(Default)]
struct ContinuationCapture {
    at_end: BTreeSet<Tin>,
    have_end: bool,
    failure: Vec<Tin>,
}

struct ParseMode<'a> {
    continuation: Option<&'a mut ContinuationCapture>,
    recovering: bool,
    errors: &'a mut Vec<TabnasError>,
    partial: Option<Value>,
}

struct RelexUndo {
    position: usize,
    token: Token,
    checkpoint: RelexCheckpoint,
    tokens: Vec<Token>,
}

#[derive(Clone, Copy)]
struct ParseSite<'a> {
    source: &'a str,
    stack: &'a [Rule],
    alts: &'a [AltSpec],
}

pub struct Parser {
    /// Shared with the lexer, which never writes to them. Cloning a
    /// whole `Options` was 22% of a small parse, and it was happening
    /// twice.
    pub options: Arc<Options>,
    ignore_tins: Vec<Tin>,
    /// `options.number.exclude` compiled once, here, and shared with
    /// the lexer of every parse instead of compiled by each of them.
    ///
    /// `options` is public, so this is a cache whose source has a second
    /// writer: a caller on the low-level API may replace the whole `Arc`
    /// between parses, and before this was cached each lexer compiled
    /// whatever pattern was in force. `exclude_pattern` is the pattern
    /// this regex was built from, and a parse whose options no longer
    /// carry it compiles the one they do carry instead -- the old cost,
    /// paid only by the callers who change the pattern under the parser.
    exclude_regex: Option<Arc<regex::Regex>>,
    exclude_pattern: Option<String>,
    /// Installed rules by name.
    ///
    /// Private, and read through [`Parser::rules`]. Two derived tables below
    /// are keyed by the same names and are written only by `add_rule`; while
    /// this was public an embedder could insert or replace a rule straight
    /// into it, leaving those tables describing the rule that used to be
    /// there. Lookahead would then gate the custom matchers on the previous
    /// rule's token identities, or on none at all for a name that was never
    /// installed, and a valid document could fail to parse. Before those
    /// identities were derived once per rule instead of once per lookahead
    /// there was nothing to go stale, so this hazard arrived with the table.
    rules: IndexMap<String, Arc<RuleSpec>>,
    /// The `rule.include` and `rule.exclude` every `PreparedAlt::groups`
    /// was worked out against, and every `expected_tins` row collated
    /// against. Both are public and a callback may write either between
    /// one step and the next, so the step compares these two strings
    /// before trusting the prepared answer, and `expected_match_tins`
    /// before trusting the row -- the same shape of guard the compiled
    /// `number.exclude` pattern carries, and for the same reason: a cache
    /// derived from a public mutable field is a cache with a second
    /// writer.
    prepared_include: String,
    prepared_exclude: String,
    /// The token identities each rule can accept at each lookahead slot,
    /// worked out once per installed rule rather than once per token, and
    /// positional in `rules` the way `names` is: a rule carries the slot
    /// it was installed at, so reaching its row costs an index rather than
    /// a hash of its name on every token fetch.
    expected_tins: Vec<ExpectedTins>,
    /// One shared copy of each installed rule's name, so pushing a rule
    /// clones a pointer rather than reallocating the name per push.
    ///
    /// Parallel to `rules`, indexed by position rather than keyed by the
    /// name again: every route already has to find the rule's spec, and
    /// `IndexMap::get_full` hands back that rule's index with it, so the
    /// shared name costs an array index instead of a second hash of the
    /// same bytes. `Parser::rules` is public and hands out the map itself,
    /// which is why the handle lives beside the map rather than in it.
    /// `add_rule` keeps the two in step; `IndexMap` gives a replaced key
    /// back its existing index, so a replacement overwrites in place.
    names: Vec<RuleName>,
    /// Per-rule state worked out once, when the grammar is installed.
    ///
    /// Parallel to `rules` and `names`, and written only by `add_rule`,
    /// which rebuilds the whole table -- a route may name a rule that is
    /// installed later, so an entry is only right once every rule is in.
    /// The loop reaches an entry by the `slot` the rule carries and checks
    /// it against the rule's own spec and name before trusting it, so a
    /// callback that rewrites either just falls back to the lookup.
    prepared: Vec<PreparedRule>,
    pub actions: HashMap<String, Action>,
    pub context_actions: HashMap<String, ContextAction>,
    pub matched_actions: HashMap<String, AltAction>,
    pub state_actions: HashMap<String, StateAction>,
    pub token_subscribers: Vec<TokenSubscriber>,
    pub lex_subscribers: Vec<LexSubscriber>,
    pub rule_subscribers: Vec<RuleSubscriber>,
    pub rule_done_subscribers: Vec<RuleDoneSubscriber>,
    pub instance: InstanceInfo,
}

/// One installed rule, with what the parse loop can work out about it
/// before a parse starts.
///
/// Routing, and the order the rule's actions run in. The order is a
/// question about the spec alone -- which of the named, callback and
/// state lists a binding came from, and where the three interleave -- and
/// the spec behind the `Arc` this record is checked against cannot change
/// while a parse runs, so it is answered once here instead of rebuilt into
/// a fresh `Vec` on every rule step. What a *named* binding resolves to is
/// a different question: it is looked up in `Parser::actions`,
/// `matched_actions` and `state_actions`, all of them public maps an
/// embedder may write after `add_rule` has run, so that lookup stays where
/// it was, on the step -- a table derived from a field with a second
/// writer goes stale in silence, which is the hazard `rules` was made
/// private to close.
struct PreparedRule {
    /// The rule's shared name handle and the spec it was installed with.
    ///
    /// `spec` is what the loop borrows in place of the rule's own; `name`
    /// is only ever compared. Together they are what says the rule in hand
    /// is still the rule this record describes.
    name: RuleName,
    spec: Arc<RuleSpec>,
    open: Vec<PreparedAlt>,
    close: Vec<PreparedAlt>,
    /// The alternates of each state indexed by the tin they can take at
    /// position 0, so a step tries the candidates for the first
    /// lookahead token rather than every alternate.
    open_first: AltIndex,
    close_first: AltIndex,
    /// The four lifecycle action orders, in the same order the rule runs
    /// them. Most grammars declare none of them, and an empty list here is
    /// what lets a step skip the whole phase rather than walk an empty
    /// one: `bind_spec` sets a rule's `bo`/`ao`/`bc`/`ac` true
    /// unconditionally, so those flags say the phase is *enabled*, never
    /// that it has anything to run.
    bo: Vec<ActionBinding>,
    ao: Vec<ActionBinding>,
    bc: Vec<ActionBinding>,
    ac: Vec<ActionBinding>,
}

impl PreparedRule {
    /// The first-token index of the open or close alternates.
    fn first(&self, is_open: bool) -> &AltIndex {
        if is_open {
            &self.open_first
        } else {
            &self.close_first
        }
    }

    fn alt(&self, is_open: bool, idx: usize) -> Option<&PreparedAlt> {
        if is_open {
            self.open.get(idx)
        } else {
            self.close.get(idx)
        }
    }

    /// The before-open or before-close action order.
    fn before(&self, is_open: bool) -> &[ActionBinding] {
        if is_open {
            &self.bo
        } else {
            &self.bc
        }
    }

    /// The after-open or after-close action order.
    fn after(&self, is_open: bool) -> &[ActionBinding] {
        if is_open {
            &self.ao
        } else {
            &self.ac
        }
    }
}

/// One alternate's two routing channels and its action order, resolved at
/// install time.
struct PreparedAlt {
    p: PreparedRoute,
    r: PreparedRoute,
    /// The alternate's action bindings in the order they run.
    actions: Vec<AltActionBinding>,
    /// Whether anything a step on this alternate runs can read the
    /// `AltMatch` record, and so see the action list the engine publishes
    /// into `matched.actions`.
    ///
    /// The list is published for the callbacks that receive the record --
    /// the `c`/`e`/`p`/`r`/`b` matched hooks, the matched modifier, and a
    /// matched action itself. When the alternate declares none of them
    /// nothing in the step can tell whether the field was filled, so the
    /// step runs these bindings straight from here: no `Vec` built, no
    /// second copy taken to iterate, no reference counts touched. When it
    /// declares any of them the step fills the record exactly as before.
    ///
    /// A `Named` binding may also resolve to a matched action, and that
    /// cannot be settled here -- `add_matched_action` may be called after
    /// `add_rule`, and `matched_actions` is public. `named` records only
    /// that the question arises; the step asks it of the map it is asking
    /// anyway.
    observed: bool,
    named: bool,
    /// Whether `rule.include` and `rule.exclude` leave this alternate
    /// active, worked out when the rule was installed.
    ///
    /// The question is about `alt.g` against two option strings, and the
    /// answer cannot change during a parse unless a callback rewrites the
    /// options -- so `prepared_include`/`prepared_exclude` on the parser
    /// record what this was computed against, and the step checks those
    /// two strings ONCE rather than re-deriving the answer per alternate.
    groups: bool,
    /// `alt.g` split into the tags the step publishes into `matched.g`,
    /// split when the rule was installed rather than per rule step.
    ///
    /// Unlike `groups` this is derived from the alternate alone, so no
    /// option string can stale it; the only alternate it cannot answer for
    /// is one a modifier rewrote, which the step detects the same way it
    /// does for the action order.
    group_tags: Vec<String>,
}

enum PreparedRoute {
    /// Nothing was resolved: the alternate declares no route on this
    /// channel, or names a rule that is not installed, or routes through a
    /// callback. All three take the same path they took before the table --
    /// one lookup by name, which is also what raises `unknown_rule` for a
    /// route naming no rule.
    ByName,
    Static {
        slot: usize,
        name: RuleName,
        spec: Arc<RuleSpec>,
    },
}

impl PreparedRoute {
    /// The prepared answer, if the route the parse is actually taking is
    /// still the one that was prepared.
    ///
    /// The test is byte-equality of the name, unconditionally, and not a
    /// pointer comparison of the alternate: `h` rewrites the whole
    /// `AltSpec` per step, `p_fn`/`r_fn` and `p_match`/`r_match` produce
    /// the name at run time, and a matched action may write `matched.p` or
    /// `matched.r` outright -- all supported routing channels. A route that
    /// arrives at the same name resolves to the same rule whichever of them
    /// produced it, so the bytes are the whole question.
    fn resolved(&self, name: &str) -> Option<(usize, RuleName, &Arc<RuleSpec>)> {
        match self {
            PreparedRoute::Static {
                slot,
                name: prepared,
                spec,
            } if prepared.as_str() == name => Some((*slot, prepared.clone(), spec)),
            _ => None,
        }
    }
}

/// Per-slot accepted token identities for one rule, in each state.
///
/// This used to be derived on every lookahead: a map lookup, a `BTreeSet`
/// built from the alternates, and a `Vec` collected out of it, once per
/// token. None of it can change while a parse runs, so it is derived once
/// when the rule is installed.
/// What the names on one slot resolve to against `options` now: a slot
/// naming a token set takes the set's current members, and a slot naming a
/// token takes that token. `None` leaves the slot with the tins it has, and
/// is the answer for a slot with no names (an alternate built from tins
/// directly, or a slot set by hand before a merge carried it, whose names
/// the merge drops), a slot whose `s` was set by hand since it was
/// installed (it no longer equals what the names last resolved to,
/// `s_bound`: the edit wins over the names, as it did before the names
/// were kept), and a slot naming something the options do not know (a
/// parser build must not mint a token).
pub(crate) fn resolved_slot(alt: &AltSpec, slot: usize, options: &Options) -> Option<Vec<Tin>> {
    let names = alt.s_names.get(slot)?;
    if names.is_empty() || alt.s.get(slot) != alt.s_bound.get(slot) {
        return None;
    }
    let mut tins = Vec::with_capacity(names.len());
    for name in names {
        if let Some(set) = options.token_set.get(name.trim_start_matches('#')) {
            tins.extend(set.iter().copied());
        } else {
            tins.push(options.token(name)?);
        }
    }
    Some(tins)
}

/// Resolve every slot that was declared by name against `options`
/// (`resolved_slot`).
fn resolve_slot_names(spec: &mut RuleSpec, options: &Options) {
    for alt in spec.open.iter_mut().chain(spec.close.iter_mut()) {
        for slot in 0..alt.s.len() {
            if let Some(tins) = resolved_slot(alt, slot, options) {
                alt.s[slot] = tins;
            }
        }
    }
}

/// Alternates indexed by the tin they can take at position 0.
///
/// `by_tin` maps each tin some alternate names at position 0 to the
/// ascending indices of the alternates naming it; `wild` holds the
/// alternates that constrain nothing at position 0 (an empty sequence,
/// an empty slot, or a slot naming `#AA`, exactly the slots
/// `slot_matches` accepts every tin for), which are candidates for every
/// tin. The step walks the two lists together in index order, so the
/// candidates for a tin are the original scan with the alternates that
/// cannot take it left out, and first-match-wins is preserved. The lists
/// stay separate rather than being merged per tin: a rule with W
/// wildcard alternates and T distinct first tins would otherwise cost
/// W×T entries.
#[derive(Debug, Default)]
struct AltIndex {
    by_tin: HashMap<Tin, Vec<usize>>,
    wild: Vec<usize>,
}

const NO_ALTS: &[usize] = &[];

impl AltIndex {
    fn of(alts: &[AltSpec]) -> Self {
        let mut by_tin: HashMap<Tin, Vec<usize>> = HashMap::new();
        let mut wild = Vec::new();
        for (idx, alt) in alts.iter().enumerate() {
            match alt.s.first() {
                Some(slot) if !slot.is_empty() && !slot.contains(&TIN_AA) => {
                    for tin in slot {
                        let list = by_tin.entry(*tin).or_default();
                        // A slot naming one tin twice must not try the
                        // alternate twice: a condition could observe it.
                        if list.last() != Some(&idx) {
                            list.push(idx);
                        }
                    }
                }
                _ => wild.push(idx),
            }
        }
        AltIndex { by_tin, wild }
    }

    /// The ascending alternates naming `tin` at position 0; the
    /// wildcards are `wild`, walked beside them.
    fn named(&self, tin: Tin) -> &[usize] {
        self.by_tin.get(&tin).map_or(NO_ALTS, Vec::as_slice)
    }
}

#[derive(Debug, Default)]
struct ExpectedTins {
    open: Vec<Vec<Tin>>,
    close: Vec<Vec<Tin>>,
}

impl ExpectedTins {
    /// Collated over the alternates `options` enable, as TypeScript's
    /// `tcol` is: `filterRules` has removed the others from the spec
    /// before `norm()` collates it. An excluded alternate is never tried,
    /// so it must not decide which matchers run in the position-expected
    /// pass either. It did until 0.12.4, and a grammar that excludes a
    /// group while redefining a set that group's alternates name had the
    /// set's members expected where the grammar never takes them (toml
    /// excludes jsonic and sets `KEY` to `#ST #ID`, and its `val` came to
    /// expect `#ID`, whose matcher then claimed every number).
    ///
    /// The filters are the ones in force at install, which the parser
    /// records as `prepared_include` and `prepared_exclude`. Both are
    /// public options a caller may rewrite afterwards, and the step then
    /// chooses among the alternates the live filters enable, so
    /// `Parser::expected_match_tins` reads this table only while the live
    /// filters are still the recorded ones, and collates from them with
    /// `live` otherwise.
    fn of(spec: &RuleSpec, options: &Options) -> Self {
        Self {
            open: Self::by_slot(&spec.open, options),
            close: Self::by_slot(&spec.close, options),
        }
    }

    fn by_slot(alts: &[AltSpec], options: &Options) -> Vec<Vec<Tin>> {
        let alts: Vec<&AltSpec> = alts
            .iter()
            .filter(|alt| groups_enabled(alt, options))
            .collect();
        let slots = alts.iter().map(|alt| alt.s.len()).max().unwrap_or(0);
        (0..slots)
            .map(|slot| Self::collate(alts.iter().copied(), slot))
            .collect()
    }

    /// One slot's row, collated from the alternates `options` enable now
    /// rather than the ones they enabled at install.
    fn live(alts: &[AltSpec], options: &Options, slot: usize) -> Vec<Tin> {
        Self::collate(alts.iter().filter(|alt| groups_enabled(alt, options)), slot)
    }

    fn collate<'a>(alts: impl Iterator<Item = &'a AltSpec>, slot: usize) -> Vec<Tin> {
        let mut expected = BTreeSet::new();
        for alt in alts {
            if let Some(tins) = alt.s.get(slot) {
                expected.extend(tins.iter().copied());
            }
        }
        expected.into_iter().collect()
    }

    fn at(&self, is_open: bool, slot: usize) -> &[Tin] {
        let slots = if is_open { &self.open } else { &self.close };
        slots.get(slot).map(Vec::as_slice).unwrap_or_default()
    }
}

impl Parser {
    pub fn new(options: Options) -> Self {
        let mut options = options;
        options.sort_for_lexing();
        Self::from_shared(Arc::new(options))
    }

    /// Parse against a configuration that is already prepared and
    /// ordered, shared with every other parse of the same grammar.
    pub fn from_shared(options: Arc<Options>) -> Self {
        Parser {
            ignore_tins: options.ignore_tins(),
            exclude_regex: compile_number_exclude(&options),
            exclude_pattern: options.number.exclude.clone(),
            options,
            rules: IndexMap::new(),
            prepared_include: String::new(),
            prepared_exclude: String::new(),
            expected_tins: Vec::new(),
            names: Vec::new(),
            prepared: Vec::new(),
            actions: HashMap::new(),
            context_actions: HashMap::new(),
            matched_actions: HashMap::new(),
            state_actions: HashMap::new(),
            token_subscribers: Vec::new(),
            lex_subscribers: Vec::new(),
            rule_subscribers: Vec::new(),
            rule_done_subscribers: Vec::new(),
            instance: InstanceInfo::default(),
        }
    }

    /// The installed rules, in declaration order.
    ///
    /// Read-only: every write goes through [`Parser::add_rule`], which is
    /// what keeps the derived tables in step with it.
    pub fn rules(&self) -> &IndexMap<String, Arc<RuleSpec>> {
        &self.rules
    }

    pub fn add_rule(&mut self, spec: RuleSpec) {
        // A slot declared by name is resolved against the options this
        // parser is built with, so a token set overridden after the rule
        // was installed reaches it (tabnas/parser#217). The parser is
        // rebuilt whenever the options change, which is what makes this
        // the late binding TypeScript's `norm()` and Go's `altS` provide.
        let mut spec = spec;
        resolve_slot_names(&mut spec, &self.options);
        // `rebuild_prepared` below records the live filters as the ones
        // every accepted-token row was collated against, so a row collated
        // under filters rewritten since then is collated again first. Only
        // a rewrite between two installs reaches this.
        if self.options.rule.include != self.prepared_include
            || self.options.rule.exclude != self.prepared_exclude
        {
            for (row, installed) in self.expected_tins.iter_mut().zip(self.rules.values()) {
                *row = ExpectedTins::of(installed, &self.options);
            }
        }
        let expected = ExpectedTins::of(&spec, &self.options);
        let shared = RuleName::from(spec.name.as_str());
        let (index, _) = self.rules.insert_full(spec.name.clone(), Arc::new(spec));
        // A replacement keeps the key's index, so it overwrites its own
        // name handle; a new rule is appended and takes the next slot. The
        // accepted-token table is positional for the same reason and is
        // written here, not in `rebuild_prepared`: it is derived from one
        // rule's spec alone, so rebuilding it for every rule on every
        // install would be quadratic for no gain.
        match self.names.get_mut(index) {
            Some(existing) => *existing = shared,
            None => self.names.push(shared),
        }
        match self.expected_tins.get_mut(index) {
            Some(existing) => *existing = expected,
            None => self.expected_tins.push(expected),
        }
        self.rebuild_prepared();
    }

    /// Rebuild the prepared table for every installed rule.
    ///
    /// Every rule, not just the one just installed: routes point across the
    /// grammar, so a rule installed now can be the destination an earlier
    /// rule named and could not resolve, and a replaced rule is a new
    /// `Arc<RuleSpec>` that every route into it has to start handing out.
    /// That makes installing a grammar quadratic in its size, which for the
    /// grammars this engine runs -- tens of rules, installed once -- is
    /// nothing next to a single parse, and it is the only shape that stays
    /// right for a `Parser` that has rules added to it after it has already
    /// parsed something.
    fn rebuild_prepared(&mut self) {
        let mut prepared = Vec::with_capacity(self.rules.len());
        for (index, spec) in self.rules.values().enumerate() {
            prepared.push(PreparedRule {
                name: self.names[index].clone(),
                spec: Arc::clone(spec),
                open: Self::prepared_alts(&spec.open, &self.rules, &self.names, &self.options),
                close: Self::prepared_alts(&spec.close, &self.rules, &self.names, &self.options),
                open_first: AltIndex::of(&spec.open),
                close_first: AltIndex::of(&spec.close),
                bo: resolved_action_order(
                    &spec.bo,
                    &spec.bo_fns,
                    &spec.bo_state_fns,
                    &spec.bo_order,
                ),
                ao: resolved_action_order(
                    &spec.ao,
                    &spec.ao_fns,
                    &spec.ao_state_fns,
                    &spec.ao_order,
                ),
                bc: resolved_action_order(
                    &spec.bc,
                    &spec.bc_fns,
                    &spec.bc_state_fns,
                    &spec.bc_order,
                ),
                ac: resolved_action_order(
                    &spec.ac,
                    &spec.ac_fns,
                    &spec.ac_state_fns,
                    &spec.ac_order,
                ),
            });
        }
        self.prepared = prepared;
        self.prepared_include.clone_from(&self.options.rule.include);
        self.prepared_exclude.clone_from(&self.options.rule.exclude);
    }

    fn prepared_alts(
        alts: &[AltSpec],
        rules: &IndexMap<String, Arc<RuleSpec>>,
        names: &[RuleName],
        options: &Options,
    ) -> Vec<PreparedAlt> {
        alts.iter()
            .map(|alt| {
                // The same normalisation the step used to run: a grammar
                // that appended to `a` or `action_fns` after the builder
                // last touched `action_order` gets the fallback chain built
                // for it here instead of there.
                let actions = resolved_alt_action_order(
                    &alt.a,
                    &alt.action_fns,
                    &alt.matched_action_fns,
                    &alt.action_order,
                );
                let observed = alt.c_match.is_some()
                    || alt.c_lex_match.is_some()
                    || alt.e_match.is_some()
                    || alt.p_match.is_some()
                    || alt.r_match.is_some()
                    || alt.b_match.is_some()
                    || alt.h_match.is_some()
                    || actions
                        .iter()
                        .any(|binding| matches!(binding, AltActionBinding::Matched(_)));
                let named = actions
                    .iter()
                    .any(|binding| matches!(binding, AltActionBinding::Named(_)));
                PreparedAlt {
                    p: Self::prepared_route(alt.p.as_deref(), rules, names),
                    r: Self::prepared_route(alt.r.as_deref(), rules, names),
                    actions,
                    observed,
                    named,
                    groups: groups_enabled(alt, options),
                    group_tags: listed(&alt.g).map(str::to_owned).collect(),
                }
            })
            .collect()
    }

    fn prepared_route(
        route: Option<&str>,
        rules: &IndexMap<String, Arc<RuleSpec>>,
        names: &[RuleName],
    ) -> PreparedRoute {
        // An empty route name is "no route" at the transition, so it is not
        // one here either.
        let Some(route) = route.filter(|route| !route.is_empty()) else {
            return PreparedRoute::ByName;
        };
        match rules.get_full(route) {
            Some((slot, _, spec)) => PreparedRoute::Static {
                slot,
                name: names[slot].clone(),
                spec: Arc::clone(spec),
            },
            None => PreparedRoute::ByName,
        }
    }

    /// Resolve a rule name to the shared name handle and the installed
    /// spec in one lookup.
    ///
    /// Every route into a rule needs both, and used to hash the same
    /// three-byte name three times over to get them: once to ask whether
    /// the rule existed, once for the shared handle, once for the spec.
    /// `get_full` answers all three at once -- absence, the index the
    /// handle sits at, and the spec. The index is also the rule's slot in
    /// the prepared table, which the rule carries so that the next step can
    /// find its state without asking again.
    fn installed(&self, name: &str) -> Option<(usize, RuleName, &Arc<RuleSpec>)> {
        let (index, _, spec) = self.rules.get_full(name)?;
        Some((index, self.names[index].clone(), spec))
    }

    pub fn add_action(&mut self, name: String, action: Action) {
        self.actions.insert(name, action);
    }

    pub fn add_context_action(&mut self, name: String, action: ContextAction) {
        self.context_actions.insert(name, action);
    }

    pub fn add_matched_action(&mut self, name: String, action: AltAction) {
        self.matched_actions.insert(name, action);
    }

    pub fn add_state_action(&mut self, name: String, action: StateAction) {
        self.state_actions.insert(name, action);
    }

    pub fn add_token_subscriber(&mut self, subscriber: TokenSubscriber) {
        self.token_subscribers.push(subscriber);
    }

    pub fn add_lex_subscriber(&mut self, subscriber: LexSubscriber) {
        self.lex_subscribers.push(subscriber);
    }

    pub fn add_rule_subscriber(&mut self, subscriber: RuleSubscriber) {
        self.rule_subscribers.push(subscriber);
    }

    pub fn add_rule_done_subscriber(&mut self, subscriber: RuleDoneSubscriber) {
        self.rule_done_subscribers.push(subscriber);
    }

    pub fn set_instance_info(&mut self, instance: InstanceInfo) {
        self.instance = instance;
    }

    fn run_action(
        &self,
        name: &str,
        rule: &mut Rule,
        context: &mut Context,
    ) -> Result<(), TabnasError> {
        self.run_action_with_config(name, rule, context, None)
    }

    fn run_after_actions(
        &self,
        spec: &RuleSpec,
        prepared: Option<&PreparedRule>,
        is_open: bool,
        rule: &mut Rule,
        context: &mut Context,
        site: ParseSite<'_>,
    ) -> Result<(), TabnasError> {
        if (is_open && !rule.ao) || (!is_open && !rule.ac) {
            return Ok(());
        }
        // `ao`/`ac` above are run control, not presence: a rule carries
        // them set whether or not it declares an after action. The order
        // itself says whether there is anything to run, and when there is
        // not the phase costs nothing -- not the list, and not the
        // reference count on the next rule the state callbacks would have
        // been handed.
        let by_spec;
        let bindings: &[ActionBinding] = match prepared {
            Some(prepared) => prepared.after(is_open),
            None => {
                let (actions, callbacks, states, order) = if is_open {
                    (&spec.ao, &spec.ao_fns, &spec.ao_state_fns, &spec.ao_order)
                } else {
                    (&spec.ac, &spec.ac_fns, &spec.ac_state_fns, &spec.ac_order)
                };
                by_spec = resolved_action_order(actions, callbacks, states, order);
                &by_spec
            }
        };
        if bindings.is_empty() {
            return Ok(());
        }
        let next = rule.next_rule.clone();
        let mut output = None;
        for binding in bindings {
            output = match binding {
                ActionBinding::Named(action) => {
                    if let Some(callback) = self.state_actions.get(action) {
                        self.run_state_callback(
                            "named lifecycle after action",
                            callback,
                            rule,
                            context,
                            next.as_deref(),
                            output,
                        )
                        .map_err(|error| {
                            self.attach_action_error(
                                error,
                                site.source,
                                rule,
                                site.stack,
                                site.alts,
                            )
                        })?
                    } else {
                        self.run_action(action, rule, context).map_err(|error| {
                            self.attach_action_error(
                                error,
                                site.source,
                                rule,
                                site.stack,
                                site.alts,
                            )
                        })?;
                        None
                    }
                }
                ActionBinding::Callback(callback) => {
                    self.run_context_callback("lifecycle after action", callback, rule, context)
                        .map_err(|error| {
                            self.attach_action_error(
                                error,
                                site.source,
                                rule,
                                site.stack,
                                site.alts,
                            )
                        })?;
                    None
                }
                ActionBinding::State(callback) => self
                    .run_state_callback(
                        "lifecycle after action",
                        callback,
                        rule,
                        context,
                        next.as_deref(),
                        output,
                    )
                    .map_err(|error| {
                        self.attach_action_error(error, site.source, rule, site.stack, site.alts)
                    })?,
            };
            output = self.check_lifecycle_output(output, rule, site)?;
        }
        Ok(())
    }

    fn run_context_callback(
        &self,
        label: &str,
        callback: &ContextAction,
        rule: &mut Rule,
        context: &mut Context,
    ) -> Result<(), TabnasError> {
        context.set_rule(rule);
        match catch_unwind(AssertUnwindSafe(|| callback(rule, context))) {
            Ok(result) => result.map_err(|action_error| {
                let token = match rule.state {
                    RuleState::Open => rule.o0().or_else(|| rule.c0()),
                    RuleState::Close => rule.c0().or_else(|| rule.o0()),
                };
                let mut error = TabnasError::new(
                    action_error.code,
                    token.map_or("", |value| value.src.as_str()),
                    "",
                    token.map_or(0, |value| value.site.pos),
                    token.map_or(1, |value| value.site.ri),
                    token.map_or(1, |value| value.site.ci),
                );
                error.detail = action_error.detail;
                error
            }),
            Err(payload) => Err(self.action_panic(payload, label, rule)),
        }
    }

    fn run_state_callback(
        &self,
        label: &str,
        callback: &StateAction,
        rule: &mut Rule,
        context: &mut Context,
        next: Option<&RuleSnapshot>,
        out: Option<Token>,
    ) -> Result<Option<Token>, TabnasError> {
        context.set_rule(rule);
        match catch_unwind(AssertUnwindSafe(|| callback(rule, context, next, out))) {
            Ok(result) => result.map_err(|action_error| {
                let token = match rule.state {
                    RuleState::Open => rule.o0().or_else(|| rule.c0()),
                    RuleState::Close => rule.c0().or_else(|| rule.o0()),
                };
                let mut error = TabnasError::new(
                    action_error.code,
                    token.map_or("", |value| value.src.as_str()),
                    "",
                    token.map_or(0, |value| value.site.pos),
                    token.map_or(1, |value| value.site.ri),
                    token.map_or(1, |value| value.site.ci),
                );
                error.detail = action_error.detail;
                error
            }),
            Err(payload) => Err(self.action_panic(payload, label, rule)),
        }
    }

    fn check_lifecycle_output(
        &self,
        output: Option<Token>,
        rule: &Rule,
        site: ParseSite<'_>,
    ) -> Result<Option<Token>, TabnasError> {
        let Some(token) = output.as_ref().filter(|token| !token.err.is_empty()) else {
            return Ok(output);
        };
        Err(self.raised_token_error(token, rule, site))
    }

    fn raised_token_error(&self, token: &Token, rule: &Rule, site: ParseSite<'_>) -> TabnasError {
        let error = TabnasError::new(
            raised_error_code(token),
            token.src.clone(),
            site.source,
            token.site.pos,
            token.site.ri,
            token.site.ci,
        );
        self.attach_error(error, rule, site.stack, site.alts, Some(token))
    }

    fn attach_action_error(
        &self,
        mut error: TabnasError,
        src: &str,
        rule: &Rule,
        stack: &[Rule],
        alts: &[AltSpec],
    ) -> TabnasError {
        error.full_source = src.into();
        let token = match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        };
        self.attach_error(error, rule, stack, alts, token)
    }

    fn run_action_with_config(
        &self,
        name: &str,
        rule: &mut Rule,
        context: &mut Context,
        config: Option<&Value>,
    ) -> Result<(), TabnasError> {
        context.set_rule(rule);
        match catch_unwind(AssertUnwindSafe(|| {
            run_builtin_action_with_info(name, rule, context, config, &self.options.info)
        })) {
            Ok(true) => return Ok(()),
            Ok(false) => {}
            Err(payload) => return Err(self.action_panic(payload, name, rule)),
        }
        if let Some(action) = self.actions.get(name) {
            return match catch_unwind(AssertUnwindSafe(|| action(rule))) {
                Ok(()) => Ok(()),
                Err(payload) => Err(self.action_panic(payload, name, rule)),
            };
        }
        if let Some(action) = self.context_actions.get(name) {
            return self.run_context_callback(name, action, rule, context);
        }
        let token = match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        };
        let mut error = TabnasError::new(
            "unknown",
            name,
            "",
            token.map_or(0, |value| value.site.pos),
            token.map_or(1, |value| value.site.ri),
            token.map_or(1, |value| value.site.ci),
        );
        error.detail = format!("unknown action: {name}");
        Err(error)
    }

    fn action_panic(
        &self,
        payload: Box<dyn std::any::Any + Send>,
        name: &str,
        rule: &Rule,
    ) -> TabnasError {
        let token = match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        };
        TabnasError::from_panic(
            payload,
            &format!("action {name}"),
            "",
            token.map_or(0, |value| value.site.pos),
            token.map_or(1, |value| value.site.ri),
            token.map_or(1, |value| value.site.ci),
            &self.options,
        )
    }

    fn attach_error(
        &self,
        mut error: TabnasError,
        rule: &Rule,
        stack: &[Rule],
        alts: &[AltSpec],
        token: Option<&Token>,
    ) -> TabnasError {
        let mut rule_stack: Vec<String> = stack.iter().map(|item| item.name.to_string()).collect();
        rule_stack.push(rule.name.to_string());
        let expected = alts
            .iter()
            .filter_map(|alt| alt.s.first())
            .flat_map(|tins| tins.iter().copied())
            .map(|tin| self.options.token_name(tin))
            .collect();
        error.attach_context(
            &rule.name,
            if rule.state == RuleState::Open {
                "o"
            } else {
                "c"
            },
            rule_stack,
            token,
            expected,
        );
        self.decorate_error(&mut error);
        error
    }

    fn decorate_error(&self, error: &mut TabnasError) {
        error.apply_options(&self.options);
        error.plugins = self.instance.plugins.clone();
    }

    fn catch_callback<T>(
        &self,
        api: &str,
        src: &str,
        callback: impl FnOnce() -> T,
    ) -> Result<T, TabnasError> {
        catch_unwind(AssertUnwindSafe(callback))
            .map_err(|payload| TabnasError::from_panic(payload, api, src, 0, 1, 1, &self.options))
    }

    fn attach_active_error(
        &self,
        mut error: TabnasError,
        rule: &Rule,
        stack: &[Rule],
        token: Option<&Token>,
    ) -> TabnasError {
        if let Some(spec) = self.rules.get(&*rule.name) {
            let alts = if rule.state == RuleState::Open {
                &spec.open
            } else {
                &spec.close
            };
            self.attach_error(error, rule, stack, alts, token)
        } else {
            self.decorate_error(&mut error);
            error
        }
    }

    fn phase_token(rule: &Rule) -> Option<&Token> {
        match rule.state {
            RuleState::Open => rule.o0().or_else(|| rule.c0()),
            RuleState::Close => rule.c0().or_else(|| rule.o0()),
        }
    }

    fn ancestors_for<'a>(rule: &Rule, stack: &'a [Rule]) -> &'a [Rule] {
        if stack.last().is_some_and(|ancestor| ancestor.i == rule.i) {
            &stack[..stack.len() - 1]
        } else {
            stack
        }
    }

    /// A copy of the rule that just finished, when something will read it.
    ///
    /// [`Parser::notify_rule_done`] is its only reader, and that returns
    /// without looking when no `ruleDone` subscriber is installed. Cloning
    /// a `Rule` copies the value tree it has built with it, which is 3.3%
    /// of a 1 MB parse spent for nobody in a grammar that subscribes to
    /// nothing. This is the treatment `RuleDoneAlt` already gets at the
    /// transition arms, for the same reason.
    fn rule_done_copy(&self, rule: &Rule) -> Option<Rule> {
        (!self.rule_done_subscribers.is_empty()).then(|| rule.clone())
    }

    fn notify_rule_done(
        &self,
        rule: &Rule,
        context: &Context,
        state: RuleState,
        alt: Option<RuleDoneAlt>,
        src: &str,
        stack: &[Rule],
    ) -> Result<(), TabnasError> {
        if self.rule_done_subscribers.is_empty() {
            return Ok(());
        }
        let done = RuleDone {
            state,
            alt,
            forced: false,
        };
        let mut site_rule = rule.clone();
        site_rule.state = state;
        for subscriber in &self.rule_done_subscribers {
            let result = self.catch_callback("ruleDone subscriber", src, || {
                subscriber(rule, context, &done)
            });
            result.map_err(|error| {
                self.attach_active_error(
                    error,
                    &site_rule,
                    Self::ancestors_for(&site_rule, stack),
                    Self::phase_token(&site_rule),
                )
            })?;
        }
        Ok(())
    }

    fn notify_forced_close(
        &self,
        rule: &Rule,
        context: &Context,
        src: &str,
        stack: &[Rule],
    ) -> Result<(), TabnasError> {
        if self.rule_done_subscribers.is_empty() {
            return Ok(());
        }
        let done = RuleDone {
            state: RuleState::Close,
            alt: None,
            forced: true,
        };
        let mut site_rule = rule.clone();
        site_rule.state = RuleState::Close;
        for subscriber in &self.rule_done_subscribers {
            let result = self.catch_callback("ruleDone subscriber", src, || {
                subscriber(rule, context, &done)
            });
            result.map_err(|error| {
                self.attach_active_error(
                    error,
                    &site_rule,
                    Self::ancestors_for(&site_rule, stack),
                    Self::phase_token(&site_rule),
                )
            })?;
        }
        Ok(())
    }

    fn attempt_recover(
        &self,
        mut error: TabnasError,
        current_rule: &mut Rule,
        stack: &mut Vec<Rule>,
        context: &mut Context,
        lexer: &mut Lexer,
        mode: &mut ParseMode<'_>,
    ) -> Result<bool, TabnasError> {
        let src = error.full_source.clone();
        let recover = &self.options.parse.recover;
        if mode.errors.len() >= recover.max_recoveries {
            return Ok(false);
        }

        let suppressed = context
            .recover_at
            .is_some_and(|at| context.v_abs.saturating_sub(at) < recover.suppress);
        let no_progress = context.recover_at == Some(context.v_abs);
        let last_si = context.recover_si;
        context.recover_at = Some(context.v_abs);

        let sync = compute_sync_tins(current_rule, stack, &self.rules, &self.options);
        let mut pending: std::collections::VecDeque<Token> = std::mem::take(&mut context.t).into();
        let mut skipped = 0usize;

        let candidate = loop {
            let next = if let Some(token) = pending.pop_front() {
                Some(token)
            } else {
                loop {
                    let next_raw = self.catch_callback("lexer callback", &src, || {
                        lexer.next_raw_for_rule(current_rule, context)
                    });
                    let next_raw = next_raw.map_err(|error| {
                        self.attach_active_error(
                            error,
                            current_rule,
                            stack,
                            Self::phase_token(current_rule),
                        )
                    })?;
                    match next_raw {
                        Ok(mut token) => {
                            for subscriber in &self.lex_subscribers {
                                let result = self.catch_callback("lex subscriber", &src, || {
                                    subscriber(&mut token, current_rule, context)
                                });
                                result.map_err(|error| {
                                    self.attach_active_error(
                                        error,
                                        current_rule,
                                        stack,
                                        Some(&token),
                                    )
                                })?;
                            }
                            if self.ignore_tins.contains(&token.tin) {
                                continue;
                            }
                            for subscriber in &self.token_subscribers {
                                let result = self.catch_callback("token subscriber", &src, || {
                                    subscriber(&token)
                                });
                                result.map_err(|error| {
                                    self.attach_active_error(
                                        error,
                                        current_rule,
                                        stack,
                                        Some(&token),
                                    )
                                })?;
                            }
                            break Some(token);
                        }
                        Err(lex_error) => {
                            let mut token = error_token(&lex_error);
                            for subscriber in &self.lex_subscribers {
                                let result = self.catch_callback("lex subscriber", &src, || {
                                    subscriber(&mut token, current_rule, context)
                                });
                                result.map_err(|error| {
                                    self.attach_active_error(
                                        error,
                                        current_rule,
                                        stack,
                                        Some(&token),
                                    )
                                })?;
                            }
                            lexer.recover_after_error(mid_construct(&lex_error.code));
                            if skipped >= recover.max_skip {
                                break None;
                            }
                            skipped += 1;
                        }
                    }
                }
            };
            let Some(token) = next else {
                return Ok(false);
            };
            if token.tin == TIN_ZZ
                || (sync.contains(&token.tin)
                    && !(no_progress && last_si.is_some_and(|si| token.site.pos <= si)))
            {
                break token;
            }
            if skipped >= recover.max_skip {
                return Ok(false);
            }
            skipped += 1;
        };

        if candidate.tin == TIN_ZZ
            && no_progress
            && last_si.is_some_and(|si| candidate.site.pos <= si)
        {
            return Ok(false);
        }
        context.recover_si = Some(candidate.site.pos);
        error.recovered = Some(crate::RecoveredAt {
            skipped,
            sync: Some(candidate.tin),
            bad: false,
        });
        if !suppressed {
            mode.errors.push(error.clone());
            context.errs.push(error);
        }

        context.t.push(candidate.clone());
        context.t.extend(pending);
        context.bad_to = None;
        context.bad_error = None;

        if !recover.pop_until_valid {
            // The only pop that resumes a parent WITHOUT accepting a
            // child node over the top of it, so the only one that would
            // see a parked `child_node`. `Rule::park_child_node` is
            // skipped for the whole parse when this option is off, so
            // nothing is parked to see.
            if let Some(parent) = stack.pop() {
                *current_rule = parent;
                return Ok(true);
            }
            return Ok(false);
        }

        if accepts_close(current_rule, candidate.tin, &self.rules, &self.options) {
            if current_rule.state == RuleState::Open {
                current_rule.state = RuleState::Close;
            } else {
                current_rule.skip_befores = true;
            }
            return Ok(true);
        }

        self.notify_forced_close(current_rule, context, &src, stack)?;
        while let Some(mut parent) = stack.pop() {
            parent.accept_child(current_rule);
            if accepts_close(&parent, candidate.tin, &self.rules, &self.options) {
                *current_rule = parent;
                return Ok(true);
            }
            self.notify_forced_close(&parent, context, &src, stack)?;
            *current_rule = parent;
        }
        Ok(false)
    }

    #[allow(clippy::too_many_arguments)]
    fn recover_error_pass(
        &self,
        error: TabnasError,
        state: RuleState,
        mut alt: Option<RuleDoneAlt>,
        fallback_error_token: bool,
        src: &str,
        current_rule: &mut Rule,
        stack: &mut Vec<Rule>,
        context: &mut Context,
        lexer: &mut Lexer,
        mode: &mut ParseMode<'_>,
    ) -> Result<(), TabnasError> {
        // TypeScript's RuleSpec.bad performs recovery inside the rule pass;
        // the ordinary ruleDone event is dispatched only after that pass
        // returns. Preserve that ordering so any synthesized forced-close
        // events precede this final attempted-pass event.
        let event_rule = current_rule.clone();
        let recovered = mode.recovering
            && self.attempt_recover(error.clone(), current_rule, stack, context, lexer, mode)?;
        if !recovered && fallback_error_token {
            if let Some(alt) = alt.as_mut().filter(|alt| alt.err.is_none()) {
                let tin = self.options.token(&error.token.name).unwrap_or(TIN_BD);
                let mut token = Token::new(
                    error.token.name.clone(),
                    tin,
                    Value::Undefined,
                    error.token.src.clone(),
                    crate::Point {
                        len: error.len,
                        site: crate::Site {
                            si: error.pos,
                            pos: error.pos,
                            ri: error.row,
                            ci: error.col,
                        },
                    },
                );
                token.bad(&error.code);
                alt.err = Some(token);
            }
        }
        self.notify_rule_done(&event_rule, context, state, alt, src, stack)?;
        if recovered {
            Ok(())
        } else {
            Err(error)
        }
    }

    #[allow(clippy::too_many_arguments)]
    fn recover_after_actions(
        &self,
        result: Result<(), TabnasError>,
        state: RuleState,
        alt: Option<RuleDoneAlt>,
        src: &str,
        current_rule: &mut Rule,
        stack: &mut Vec<Rule>,
        context: &mut Context,
        lexer: &mut Lexer,
        mode: &mut ParseMode<'_>,
    ) -> Result<bool, TabnasError> {
        let Err(error) = result else {
            return Ok(false);
        };
        self.recover_error_pass(
            error,
            state,
            alt,
            true,
            src,
            current_rule,
            stack,
            context,
            lexer,
            mode,
        )?;
        Ok(true)
    }

    pub fn parse(&self, src: &str) -> Result<Value, TabnasError> {
        self.parse_with_meta(src, Value::Undefined)
    }

    pub fn parse_with_meta(&self, src: &str, meta: Value) -> Result<Value, TabnasError> {
        self.parse_with_owner(src, meta, None, None)
    }

    pub(crate) fn parse_for(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
    ) -> Result<Value, TabnasError> {
        self.parse_with_owner(src, meta, Some(owner), None)
    }

    pub(crate) fn parse_for_with_context(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
        parent: &ContextSeed,
    ) -> Result<Value, TabnasError> {
        self.parse_with_owner(src, meta, Some(owner), Some(parent))
    }

    fn parse_with_owner(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> Result<Value, TabnasError> {
        match catch_unwind(AssertUnwindSafe(|| {
            self.parse_uncaught(src, meta, owner, parent)
        })) {
            Ok(result) => result,
            Err(payload) => {
                let mut error =
                    TabnasError::from_panic(payload, "Parser::parse", src, 0, 1, 1, &self.options);
                self.decorate_error(&mut error);
                Err(error)
            }
        }
    }

    fn parse_uncaught(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> Result<Value, TabnasError> {
        if let Some(result) = self.run_parser_start(src, &meta, owner, parent) {
            return result.map_err(|mut error| {
                self.decorate_error(&mut error);
                error
            });
        }
        let mut errors = Vec::new();
        let recovering = self.options.parse.recover.enabled;
        let mut mode = ParseMode {
            continuation: None,
            recovering,
            errors: &mut errors,
            partial: None,
        };
        let result = self
            .parse_inner(src, meta, owner, parent, &mut mode)
            .map_err(|mut error| {
                self.decorate_error(&mut error);
                error
            });
        match result {
            Err(_) if recovering => Ok(mode.partial.unwrap_or(Value::Undefined)),
            other => other,
        }
    }

    pub fn parse_recover(&self, src: &str) -> ParseRecovery {
        self.parse_recover_with_meta(src, Value::Undefined)
    }

    pub fn parse_recover_with_meta(&self, src: &str, meta: Value) -> ParseRecovery {
        self.parse_recover_with_owner(src, meta, None, None)
    }

    pub(crate) fn parse_recover_for(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
    ) -> ParseRecovery {
        self.parse_recover_with_owner(src, meta, Some(owner), None)
    }

    pub(crate) fn parse_recover_for_with_context(
        &self,
        owner: &crate::Tabnas,
        src: &str,
        meta: Value,
        parent: &ContextSeed,
    ) -> ParseRecovery {
        self.parse_recover_with_owner(src, meta, Some(owner), Some(parent))
    }

    fn parse_recover_with_owner(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> ParseRecovery {
        match catch_unwind(AssertUnwindSafe(|| {
            self.parse_recover_uncaught(src, meta, owner, parent)
        })) {
            Ok(result) => result,
            Err(payload) => {
                let mut error = TabnasError::from_panic(
                    payload,
                    "Parser::parse_recover",
                    src,
                    0,
                    1,
                    1,
                    &self.options,
                );
                self.decorate_error(&mut error);
                ParseRecovery {
                    value: None,
                    errors: Vec::new(),
                    fatal: Some(error),
                }
            }
        }
    }

    fn parse_recover_uncaught(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> ParseRecovery {
        if let Some(result) = self.run_parser_start(src, &meta, owner, parent) {
            return match result {
                Ok(value) => ParseRecovery {
                    value: Some(value),
                    errors: Vec::new(),
                    fatal: None,
                },
                Err(mut error) => {
                    self.decorate_error(&mut error);
                    ParseRecovery {
                        value: None,
                        errors: Vec::new(),
                        fatal: Some(error),
                    }
                }
            };
        }
        let mut errors = Vec::new();
        let recovering = self.options.parse.recover.enabled;
        let (result, partial) = {
            let mut mode = ParseMode {
                continuation: None,
                recovering,
                errors: &mut errors,
                partial: None,
            };
            let result = self
                .parse_inner(src, meta, owner, parent, &mut mode)
                .map_err(|mut error| {
                    self.decorate_error(&mut error);
                    error
                });
            (result, mode.partial)
        };
        for error in &mut errors {
            self.decorate_error(error);
        }
        match result {
            Ok(value) => ParseRecovery {
                value: Some(value),
                errors,
                fatal: None,
            },
            Err(error) => {
                if errors.last() != Some(&error) {
                    errors.push(error.clone());
                }
                ParseRecovery {
                    value: recovering.then_some(partial).flatten(),
                    errors,
                    fatal: (!recovering).then_some(error),
                }
            }
        }
    }

    fn run_parser_start(
        &self,
        src: &str,
        meta: &Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
    ) -> Option<Result<Value, TabnasError>> {
        let result = if let Some(start) = self.options.parser.start_with_context.as_ref() {
            let Some(owner) = owner else {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail =
                    "parser.start requires an owning Tabnas instance; call Tabnas::parse".into();
                self.decorate_error(&mut error);
                return Some(Err(error));
            };
            catch_unwind(AssertUnwindSafe(|| start(src, owner, meta, parent)))
        } else if let Some(start) = self.options.parser.start_with_instance.as_ref() {
            let Some(owner) = owner else {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail =
                    "parser.start requires an owning Tabnas instance; call Tabnas::parse".into();
                self.decorate_error(&mut error);
                return Some(Err(error));
            };
            catch_unwind(AssertUnwindSafe(|| start(src, owner, meta)))
        } else {
            let start = self.options.parser.start.as_ref()?;
            catch_unwind(AssertUnwindSafe(|| start(src)))
        };
        Some(match result {
            Ok(result) => result.map_err(|error| *error),
            Err(payload) => Err(TabnasError::from_panic(
                payload,
                "parser.start",
                src,
                0,
                1,
                1,
                &self.options,
            )),
        })
    }

    /// Return the token kinds that can legally follow `src` when it is
    /// treated as a prefix. The result is an intentional over-approximation:
    /// runtime conditions and counters may still reject a listed token.
    pub fn continuations(&self, src: &str) -> Continuations {
        self.continuations_with_owner(src, None)
    }

    pub(crate) fn continuations_for(&self, owner: &crate::Tabnas, src: &str) -> Continuations {
        self.continuations_with_owner(src, Some(owner))
    }

    fn continuations_with_owner(&self, src: &str, owner: Option<&crate::Tabnas>) -> Continuations {
        catch_unwind(AssertUnwindSafe(|| self.continuations_uncaught(src, owner)))
            .unwrap_or_else(|_| self.start_continuations())
    }

    fn continuations_uncaught(&self, src: &str, owner: Option<&crate::Tabnas>) -> Continuations {
        let mut capture = ContinuationCapture::default();
        let mut errors = Vec::new();
        let result = {
            let mut mode = ParseMode {
                continuation: Some(&mut capture),
                recovering: false,
                errors: &mut errors,
                partial: None,
            };
            self.parse_inner(src, Value::Undefined, owner, None, &mut mode)
        };
        let mut tins = if result.is_ok() {
            if capture.have_end {
                capture.at_end.insert(TIN_ZZ);
                capture.at_end.into_iter().collect()
            } else {
                self.start_openers()
            }
        } else if capture.failure.is_empty() {
            self.start_openers()
        } else {
            capture.failure
        };
        tins.sort_unstable();
        tins.dedup();
        let tokens = tins
            .iter()
            .map(|tin| self.options.token_name(*tin))
            .collect();
        Continuations { tins, tokens }
    }

    fn start_continuations(&self) -> Continuations {
        let tins = self.start_openers();
        let tokens = tins
            .iter()
            .map(|tin| self.options.token_name(*tin))
            .collect();
        Continuations { tins, tokens }
    }

    fn start_openers(&self) -> Vec<Tin> {
        let start = self.rules.get(&self.options.rule.start);
        let mut out = BTreeSet::new();
        if let Some(spec) = start {
            for alt in &spec.open {
                if groups_enabled(alt, &self.options) {
                    if let Some(slot) = alt.s.first() {
                        out.extend(completion_tins(slot));
                    }
                }
            }
        }
        out.into_iter().collect()
    }

    /// The tins the rule can accept at `slot`, as the lexer's custom-matcher
    /// gate wants them, or nothing when there is no custom matcher to gate.
    ///
    /// Both consumers (the `fix_len` filter and the expected-first pass in
    /// `Lexer::next_raw_inner`) only read this list to decide WHICH of
    /// `options.match_tokens` may fire; with no match tokens the answer is
    /// the same for any list, and the walk over an empty table yields
    /// nothing either way. The empty slice is therefore exact, and it
    /// spares every token fetch the rule's accepted-token row entirely --
    /// the last work that sat on the fetch path of a grammar with no
    /// custom matcher at all. TS `makeMatchMatcher`
    /// returns null on an empty table and never asks (ts/src/lexer.ts).
    ///
    /// `options` is the one `Arc<Options>` the lexer reads too, so this
    /// guard consults the field its consumers consult and cannot drift
    /// from it.
    ///
    /// The table was collated over the alternates the group filters
    /// enabled at install. `rule.include` and `rule.exclude` are public and
    /// may have been rewritten since, and the step then chooses among the
    /// alternates the live filters enable (its `groups_prepared` guard),
    /// so the row is collated from the live filters too, rather than
    /// letting an alternate the step will not try decide which matcher
    /// runs first.
    fn expected_match_tins(&self, rule: &Rule, slot: usize) -> Cow<'_, [Tin]> {
        if self.options.match_tokens.is_empty() {
            return Cow::Borrowed(&[]);
        }
        // The table is the rule's, chosen by what the rule is called --
        // exactly as the name-keyed map this replaced chose it. The slot
        // is only a hint that skips the hash: `name` is public and a
        // callback may have written it, so the slot is trusted only while
        // the name installed there is still the rule's own, and anything
        // else falls back to the lookup by name. A slot never answers for
        // a name that is not at it.
        let index = match self.names.get(rule.slot) {
            Some(installed) if *installed == rule.name => rule.slot,
            _ => match self.rules.get_index_of(&*rule.name) {
                Some(index) => index,
                None => return Cow::Borrowed(&[]),
            },
        };
        let is_open = rule.state == RuleState::Open;
        if self.options.rule.include == self.prepared_include
            && self.options.rule.exclude == self.prepared_exclude
        {
            return Cow::Borrowed(self.expected_tins[index].at(is_open, slot));
        }
        let installed = &self.rules[index];
        let alts = if is_open {
            &installed.open
        } else {
            &installed.close
        };
        Cow::Owned(ExpectedTins::live(alts, &self.options, slot))
    }

    fn ensure_lookahead(
        &self,
        lexer: &mut Lexer,
        context: &mut Context,
        rule: &mut Rule,
        count: usize,
        mode: &mut ParseMode<'_>,
        site: ParseSite<'_>,
    ) -> Result<(), TabnasError> {
        while context.t.len() < count {
            if context.t.last().is_some_and(|token| token.tin == TIN_ZZ) {
                break;
            }
            let expected_match_tins = self.expected_match_tins(rule, context.t.len());
            let token = loop {
                let next = match context.next_replay() {
                    Some(token) => Ok(token),
                    None => {
                        let result = self.catch_callback("lexer callback", site.source, || {
                            lexer.next_rule_token(&expected_match_tins, rule, context)
                        });
                        result.map_err(|error| {
                            self.attach_active_error(
                                error,
                                rule,
                                site.stack,
                                Self::phase_token(rule),
                            )
                        })?
                    }
                };
                let mut token = match next {
                    Ok(token) => token,
                    Err(error) => {
                        let recovery_error = self
                            .rules
                            .get(&*rule.name)
                            .map(|spec| {
                                let alts = if rule.state == RuleState::Open {
                                    &spec.open
                                } else {
                                    &spec.close
                                };
                                self.attach_error((*error).clone(), rule, site.stack, alts, None)
                            })
                            .unwrap_or_else(|| (*error).clone());
                        if self.options.lex.relex {
                            let mut token = error_token(&recovery_error);
                            for subscriber in &self.lex_subscribers {
                                let result =
                                    self.catch_callback("lex subscriber", site.source, || {
                                        subscriber(&mut token, rule, context)
                                    });
                                result.map_err(|error| {
                                    self.attach_active_error(error, rule, site.stack, Some(&token))
                                })?;
                            }
                            break token;
                        }
                        if mode.recovering
                            && absorb_lex_error(
                                &recovery_error,
                                context,
                                &self.options,
                                mode.errors,
                            )
                        {
                            let mut token = error_token(&recovery_error);
                            for subscriber in &self.lex_subscribers {
                                let result =
                                    self.catch_callback("lex subscriber", site.source, || {
                                        subscriber(&mut token, rule, context)
                                    });
                                result.map_err(|error| {
                                    self.attach_active_error(error, rule, site.stack, Some(&token))
                                })?;
                            }
                            lexer.recover_after_error(mid_construct(&recovery_error.code));
                            continue;
                        }
                        if let Some(capture) = mode.continuation.as_deref_mut() {
                            capture.failure = continuation_tins(
                                context,
                                rule,
                                site.stack,
                                &self.rules,
                                &self.options,
                                context.t.len(),
                                None,
                            );
                        }
                        // The lexer boxes its error internally; this is
                        // the boundary back to the parser's own result.
                        return Err(*error);
                    }
                };
                for subscriber in &self.lex_subscribers {
                    let result = self.catch_callback("lex subscriber", site.source, || {
                        subscriber(&mut token, rule, context)
                    });
                    result.map_err(|error| {
                        self.attach_active_error(error, rule, site.stack, Some(&token))
                    })?;
                }
                if token.tin == TIN_ZZ {
                    if let Some(capture) = mode.continuation.as_deref_mut() {
                        capture.have_end = true;
                        capture.at_end.extend(continuation_tins(
                            context,
                            rule,
                            site.stack,
                            &self.rules,
                            &self.options,
                            context.t.len(),
                            None,
                        ));
                    }
                }
                if !self.ignore_tins.contains(&token.tin) {
                    break token;
                }
            };
            for subscriber in &self.token_subscribers {
                let result =
                    self.catch_callback("token subscriber", site.source, || subscriber(&token));
                result.map_err(|error| {
                    self.attach_active_error(error, rule, site.stack, Some(&token))
                })?;
            }
            let is_end = token.tin == TIN_ZZ;
            context.t.push(token);
            if is_end {
                break;
            }
        }
        Ok(())
    }

    fn parse_inner(
        &self,
        src: &str,
        meta: Value,
        owner: Option<&crate::Tabnas>,
        parent: Option<&ContextSeed>,
        mode: &mut ParseMode<'_>,
    ) -> Result<Value, TabnasError> {
        let input_meta = meta.clone();
        let mut context = Context::new(
            self.options.rewind.history,
            src,
            meta,
            Arc::clone(&self.options),
            self.instance.clone(),
        );
        if let Some(parent) = parent {
            context.apply_seed(parent);
        }
        for prepare in &self.options.parse.prepare {
            let outcome = self.catch_callback("parse.prepare", src, || {
                prepare.run(owner, &mut context, &input_meta)
            })?;
            if let Err(detail) = outcome {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail = detail.into();
                return Err(error);
            }
        }
        for prepare in self.options.parse.named_prepare.values() {
            let outcome = self.catch_callback("parse.prepare", src, || {
                prepare.run(owner, &mut context, &input_meta)
            })?;
            if let Err(detail) = outcome {
                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                error.detail = detail.into();
                return Err(error);
            }
        }

        if src.is_empty() {
            return if self.options.lex.empty {
                Ok(self.options.lex.empty_result.clone())
            } else {
                Err(TabnasError::new("unexpected", "", src, 0, 1, 1))
            };
        }

        // The cached regex is the one the parser was built with; use it
        // only while the options still carry the pattern it came from.
        let exclude_regex = if self.exclude_pattern == self.options.number.exclude {
            self.exclude_regex.clone()
        } else {
            compile_number_exclude(&self.options)
        };
        let mut lexer = Lexer::with_shared(src, Arc::clone(&self.options), exclude_regex);

        // One lookup for the start rule: whether it exists, its shared
        // name handle and the spec to bind, which used to be three.
        let start_name = self.options.rule.start.as_str();
        let Some((start_slot, start_shared, start_spec)) = self.installed(start_name) else {
            return Ok(Value::Undefined);
        };

        let mut current_rule = Rule::new(start_shared.clone(), Value::Undefined);
        current_rule.bind_spec(start_spec, start_shared, start_slot);
        current_rule.i = 0;
        let root_node = current_rule.node.clone();
        context.set_root(root_node.clone());
        let mut stack: Vec<Rule> = Vec::new();
        // Whether a pushed rule may let go of `child_node` while it is
        // buried. Read once: `self.options` is reached through `&self`,
        // so this cannot change under the parse, and the rule that parks
        // is the rule `attempt_recover` will resume.
        let park_child_nodes = self.options.parse.recover.pop_until_valid;
        let mut next_rule_id = 1;
        #[allow(unused_assignments)]
        let mut final_value = None;

        let mut iterations = 0usize;
        let maxmul = if self.options.rule.maxmul == 0 {
            3
        } else {
            self.options.rule.maxmul
        };
        let max_iterations = self
            .rules
            .len()
            .saturating_mul(src.encode_utf16().count())
            .saturating_mul(4)
            .saturating_mul(maxmul)
            .max(100);
        let budget = &self.options.parse.budget;

        // One match record for the whole parse, reset at the head of each
        // rule step. It used to be born twice per step -- a seed and a
        // per-alternate candidate -- and then moved twice more, at 320
        // bytes a move, to end up holding what one record could have held
        // all along. TypeScript keeps exactly one per context
        // (`ctx._palt`); a nested parse runs `parse_inner` again and so
        // gets its own, which is why this is a local and not a field.
        let mut matched = AltMatch::default();

        'parse: loop {
            context.set_active(&current_rule, &stack);
            update_partial(mode, &root_node, &current_rule, &stack);
            iterations += 1;
            if iterations > max_iterations {
                let pnt = context
                    .t
                    .first()
                    .map(|t| (t.site.pos, t.site.ri, t.site.ci))
                    .unwrap_or((0, 1, 1));
                return Err(TabnasError::new("unexpected", "", src, pnt.0, pnt.1, pnt.2));
            }
            context.iteration = iterations - 1;
            if budget.check_every_n > 0
                && context.iteration > 0
                && context.iteration % budget.check_every_n == 0
            {
                if let Some(check) = &budget.on_check {
                    let result =
                        self.catch_callback("parse.budget.onCheck", src, || check(&context));
                    let keep_going = result.map_err(|error| {
                        self.attach_active_error(
                            error,
                            &current_rule,
                            &stack,
                            Self::phase_token(&current_rule).or_else(|| context.t.first()),
                        )
                    })?;
                    if !keep_going {
                        let token = context.t.first();
                        let pnt = token
                            .map(|token| {
                                (
                                    token.src.as_str(),
                                    token.site.pos,
                                    token.site.ri,
                                    token.site.ci,
                                )
                            })
                            .unwrap_or(("", 0, 1, 1));
                        let error = TabnasError::new("cancel", pnt.0, src, pnt.1, pnt.2, pnt.3);
                        return Err(self.attach_active_error(error, &current_rule, &stack, token));
                    }
                }
            }

            // The rule was bound to its prepared state when it was routed
            // to, and carries the slot that state sits at. Taking the spec
            // from there rather than from the rule costs an array index in
            // place of comparing the two names byte by byte, and hands out a
            // borrow of the parser instead of a reference count on the spec
            // -- the clone existed only to release the borrow on the rule
            // that the callbacks below need mutably, and the parser is not
            // mutable here at all.
            //
            // The record is a hint. `spec` and `name` are both public on a
            // rule and reachable from any callback, so the slot is trusted
            // only while the record still describes the rule in hand: same
            // spec by pointer, same name. Anything else falls back to the
            // rule's own spec, and to the lookup by name as the guard for a
            // rule that was never bound.
            let by_name;
            let (prepared, spec) = match self.prepared.get(current_rule.slot).filter(|prepared| {
                Arc::ptr_eq(&prepared.spec, &current_rule.spec)
                    && prepared.name == current_rule.name
            }) {
                Some(prepared) => (Some(prepared), &prepared.spec),
                None => {
                    by_name = if current_rule.spec.name == *current_rule.name {
                        Arc::clone(&current_rule.spec)
                    } else {
                        match self.rules.get(&*current_rule.name) {
                            Some(s) => s.clone(),
                            None => {
                                let pnt = context
                                    .t
                                    .first()
                                    .map(|t| (t.site.pos, t.site.ri, t.site.ci))
                                    .unwrap_or((0, 1, 1));
                                return Err(TabnasError::new(
                                    "unknown_rule",
                                    &*current_rule.name,
                                    src,
                                    pnt.0,
                                    pnt.1,
                                    pnt.2,
                                ));
                            }
                        }
                    };
                    (None, &by_name)
                }
            };

            let is_open = current_rule.state == RuleState::Open;
            let alts = if is_open { &spec.open } else { &spec.close };

            for subscriber in &self.rule_subscribers {
                let result = self.catch_callback("rule subscriber", src, || {
                    subscriber(&mut current_rule, &mut context)
                });
                result.map_err(|error| {
                    self.attach_error(
                        error,
                        &current_rule,
                        &stack,
                        alts,
                        Self::phase_token(&current_rule).or_else(|| context.t.first()),
                    )
                })?;
            }
            update_partial(mode, &root_node, &current_rule, &stack);

            // 1. Run before-actions. Recovery can retry a failed close pass;
            // its before-close actions have already run and must not replay.
            let skip_befores = current_rule.skip_befores;
            current_rule.skip_befores = false;
            let before_enabled = if is_open {
                current_rule.bo
            } else {
                current_rule.bc
            };
            // `bo`/`bc` are run control, not presence -- see
            // `run_after_actions`. An empty order is the phase having
            // nothing to run, and skipping it here also skips the
            // snapshot the state callbacks would have been handed.
            let by_spec;
            let before_bindings: &[ActionBinding] = if skip_befores || !before_enabled {
                &[]
            } else {
                match prepared {
                    Some(prepared) => prepared.before(is_open),
                    None => {
                        let (actions, callbacks, states, order) = if is_open {
                            (&spec.bo, &spec.bo_fns, &spec.bo_state_fns, &spec.bo_order)
                        } else {
                            (&spec.bc, &spec.bc_fns, &spec.bc_state_fns, &spec.bc_order)
                        };
                        by_spec = resolved_action_order(actions, callbacks, states, order);
                        &by_spec
                    }
                }
            };
            if !before_bindings.is_empty() {
                let label = if is_open {
                    "before-open action"
                } else {
                    "before-close action"
                };
                let next = is_open.then(|| current_rule.snapshot());
                let site = ParseSite {
                    source: src,
                    stack: &stack,
                    alts,
                };
                let mut output = None;
                let mut lifecycle_error = None;
                for binding in before_bindings {
                    output = match binding {
                        ActionBinding::Named(action) => {
                            if let Some(callback) = self.state_actions.get(action) {
                                self.run_state_callback(
                                    label,
                                    callback,
                                    &mut current_rule,
                                    &mut context,
                                    next.as_deref(),
                                    output,
                                )
                                .map_err(|error| {
                                    self.attach_action_error(
                                        error,
                                        src,
                                        &current_rule,
                                        &stack,
                                        alts,
                                    )
                                })?
                            } else {
                                self.run_action(action, &mut current_rule, &mut context)
                                    .map_err(|error| {
                                        self.attach_action_error(
                                            error,
                                            src,
                                            &current_rule,
                                            &stack,
                                            alts,
                                        )
                                    })?;
                                None
                            }
                        }
                        ActionBinding::Callback(callback) => {
                            self.run_context_callback(
                                label,
                                callback,
                                &mut current_rule,
                                &mut context,
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?;
                            None
                        }
                        ActionBinding::State(callback) => self
                            .run_state_callback(
                                label,
                                callback,
                                &mut current_rule,
                                &mut context,
                                next.as_deref(),
                                output,
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?,
                    };
                    match self.check_lifecycle_output(output, &current_rule, site) {
                        Ok(next_output) => output = next_output,
                        Err(error) => {
                            lifecycle_error = Some(error);
                            break;
                        }
                    }
                }
                if let Some(error) = lifecycle_error {
                    update_partial(mode, &root_node, &current_rule, &stack);
                    self.recover_error_pass(
                        error,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        None,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue 'parse;
                }
            }
            update_partial(mode, &root_node, &current_rule, &stack);

            // 2. Select alternates
            let mut matched_alt_idx: Option<usize> = None;
            let mut matched_count = 0;
            // Nothing has read the record since the last step ended, so
            // this is the only point it has to be clean by.
            matched.reset();
            // Set once either of the two condition callbacks that receive
            // the record has written into it, so a rejected alternate's
            // writes are wiped before the next alternate is tried. Rust
            // keeps the alternates isolated from each other here; only the
            // candidates that were actually offered a record pay for it.
            let mut record_written = false;
            // The winning alternate's matched tokens, when they are known to
            // still describe `context.t`. See the assignment below.
            let mut matched_tokens: Option<Rc<Vec<Token>>> = None;

            // Asked once per step rather than once per alternate. When
            // the options still carry what the prepared answers were
            // worked out against, every alternate below reads a `bool`;
            // when a callback has rewritten either list, every alternate
            // falls back to deriving it, exactly as before.
            let groups_prepared = self.options.rule.include == self.prepared_include
                && self.options.rule.exclude == self.prepared_exclude;

            // First-token index. Once the first lookahead token is in
            // hand, and the lexer is not renegotiating token identity
            // (under relex an alternate may re-cut a token it does not
            // name, so every alternate stays a candidate), only the
            // alternates that can take that token at position 0 are
            // tried, in their original order. Until the first fetch,
            // alternates are tried in order as before: the first
            // alternate with a sequence fetches the token.
            let first_index = if self.options.lex.relex {
                None
            } else {
                prepared.map(|prepared| prepared.first(is_open))
            };
            // The two candidate lists (alternates naming the first tin,
            // and the wildcards), walked together in index order once
            // selected, and the tin they were selected for.
            let mut lists: Option<(&[usize], &[usize])> = None;
            let mut key_tin = TIN_BD;
            let (mut ni, mut wi) = (0, 0);
            let mut next_idx = 0;
            loop {
                if lists.is_none() {
                    if let (Some(index), Some(t0)) = (first_index, context.t.first()) {
                        if t0.tin != TIN_BD {
                            key_tin = t0.tin;
                            let named = index.named(key_tin);
                            let wild = index.wild.as_slice();
                            (ni, wi) = (0, 0);
                            while ni < named.len() && named[ni] < next_idx {
                                ni += 1;
                            }
                            while wi < wild.len() && wild[wi] < next_idx {
                                wi += 1;
                            }
                            lists = Some((named, wild));
                        }
                    }
                }
                let idx = match lists {
                    Some((named, wild)) => {
                        let n = named.get(ni).copied().unwrap_or(alts.len());
                        let w = wild.get(wi).copied().unwrap_or(alts.len());
                        if alts.len() <= n && alts.len() <= w {
                            // No remaining alternate can take the first token.
                            break;
                        }
                        if n < w {
                            ni += 1;
                            n
                        } else {
                            wi += 1;
                            w
                        }
                    }
                    None => {
                        if alts.len() <= next_idx {
                            break;
                        }
                        next_idx += 1;
                        next_idx - 1
                    }
                };
                let alt = &alts[idx];
                let enabled = match prepared
                    .filter(|_| groups_prepared)
                    .and_then(|prepared| prepared.alt(is_open, idx))
                {
                    Some(prepared_alt) => prepared_alt.groups,
                    None => groups_enabled(alt, &self.options),
                };
                if !enabled {
                    continue;
                }
                let s_len = alt.s.len();
                let mut alt_matches = true;
                if record_written {
                    matched.reset();
                    record_written = false;
                }
                let mut relex_undo: Option<RelexUndo> = None;
                for (pos, pos_tins) in alt.s.iter().enumerate() {
                    if let Err(error) = self.ensure_lookahead(
                        &mut lexer,
                        &mut context,
                        &mut current_rule,
                        pos + 1,
                        mode,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    ) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    let Some(token) = context.t.get(pos).cloned() else {
                        alt_matches = false;
                        break;
                    };
                    if !slot_matches(pos_tins, token.tin) {
                        let recut = if self.options.lex.relex
                            && !token.src.is_empty()
                            && !pos_tins.is_empty()
                        {
                            let result = self.catch_callback("lexer relex callback", src, || {
                                lexer.relex(&token, pos_tins, &mut current_rule, &mut context)
                            });
                            result.map_err(|error| {
                                self.attach_error(error, &current_rule, &stack, alts, Some(&token))
                            })?
                        } else {
                            None
                        };
                        let Some((mut recut, checkpoint)) = recut else {
                            alt_matches = false;
                            break;
                        };
                        for subscriber in &self.lex_subscribers {
                            let result = self.catch_callback("lex subscriber", src, || {
                                subscriber(&mut recut, &mut current_rule, &mut context)
                            });
                            result.map_err(|error| {
                                self.attach_error(error, &current_rule, &stack, alts, Some(&recut))
                            })?;
                        }
                        if !pos_tins.contains(&recut.tin) {
                            lexer.unrelex(checkpoint, &mut context);
                            alt_matches = false;
                            break;
                        }
                        if relex_undo.is_none() {
                            relex_undo = Some(RelexUndo {
                                position: pos,
                                token,
                                checkpoint,
                                tokens: context.t.clone(),
                            });
                        }
                        context.t[pos] = recut;
                        context.t.truncate(pos + 1);
                    }
                }

                if alt_matches {
                    let tokens: Rc<Vec<Token>> =
                        Rc::new(context.t.iter().take(s_len).cloned().collect());
                    // The declarative conditions are the only readers of a
                    // candidate rule; the callback tiers below run against
                    // `current_rule` itself, after its matched tokens are in
                    // place. Most alternates declare no declarative
                    // condition, and cloning a whole rule to answer a
                    // question nobody asks was the parse loop's largest
                    // single copy.
                    if alt.c_ref.is_some() || !alt.c.is_empty() {
                        let mut candidate = current_rule.clone();
                        if is_open {
                            candidate.o = Rc::clone(&tokens);
                        } else {
                            candidate.c = Rc::clone(&tokens);
                        }
                        if !builtin_condition_matches(alt.c_ref.as_deref(), &candidate)
                            || !conditions_match(&alt.c, &candidate, &stack)
                        {
                            alt_matches = false;
                        }
                    }
                    if alt_matches {
                        if is_open {
                            current_rule.o = Rc::clone(&tokens);
                        } else {
                            current_rule.c = Rc::clone(&tokens);
                        }
                        if let Some(condition) = &alt.c_fn {
                            context.set_rule(&current_rule);
                            let result = self.catch_callback("alternate condition", src, || {
                                condition(&mut current_rule, &mut context)
                            });
                            alt_matches = result.map_err(|error| {
                                self.attach_error(
                                    error,
                                    &current_rule,
                                    &stack,
                                    alts,
                                    Self::phase_token(&current_rule),
                                )
                            })?;
                        }
                        if alt_matches {
                            if let Some(condition) = &alt.c_match {
                                context.set_rule(&current_rule);
                                record_written = true;
                                let result =
                                    self.catch_callback("matched alternate condition", src, || {
                                        condition(&mut current_rule, &mut context, &mut matched)
                                    });
                                alt_matches = result.map_err(|error| {
                                    self.attach_error(
                                        error,
                                        &current_rule,
                                        &stack,
                                        alts,
                                        Self::phase_token(&current_rule),
                                    )
                                })?;
                            }
                        }
                        if alt_matches {
                            if let Some(condition) = &alt.c_lex_match {
                                context.set_rule(&current_rule);
                                record_written = true;
                                let result = self.catch_callback(
                                    "matched alternate lexer condition",
                                    src,
                                    || {
                                        condition(
                                            &mut current_rule,
                                            &mut context,
                                            &mut matched,
                                            &mut lexer,
                                        )
                                    },
                                );
                                alt_matches = result.map_err(|error| {
                                    self.attach_error(
                                        error,
                                        &current_rule,
                                        &stack,
                                        alts,
                                        Self::phase_token(&current_rule),
                                    )
                                })?;
                            }
                        }
                        if alt_matches {
                            if let Some(condition) = &alt.c_lex {
                                context.set_rule(&current_rule);
                                let result =
                                    self.catch_callback("alternate lexer condition", src, || {
                                        condition(&mut current_rule, &mut context, &mut lexer)
                                    });
                                alt_matches = result.map_err(|error| {
                                    self.attach_error(
                                        error,
                                        &current_rule,
                                        &stack,
                                        alts,
                                        Self::phase_token(&current_rule),
                                    )
                                })?;
                            }
                        }
                    }
                    if alt_matches {
                        matched_alt_idx = Some(idx);
                        matched_count = s_len;
                        // `tokens` is `context.t[..s_len]`, which is what the
                        // matched-token copy after this loop rebuilds from
                        // the same buffer. Between building it and here, the
                        // only things holding a `&mut Context` are the two
                        // condition callbacks, so without them the rebuild
                        // cannot differ and the vector below is reused
                        // instead of allocated and cloned a second time.
                        // `break` leaves the loop before the relex undo, so
                        // that cannot restore `context.t` underneath either.
                        if alt.c_fn.is_none() && alt.c_match.is_none() {
                            matched_tokens = Some(Rc::clone(&tokens));
                        }
                        break;
                    }
                }
                if let Some(undo) = relex_undo {
                    lexer.unrelex(undo.checkpoint, &mut context);
                    context.t = undo.tokens;
                    for subscriber in &self.lex_subscribers {
                        let mut restored = undo.token.clone();
                        let result = self.catch_callback("lex subscriber", src, || {
                            subscriber(&mut restored, &mut current_rule, &mut context)
                        });
                        result.map_err(|error| {
                            self.attach_error(error, &current_rule, &stack, alts, Some(&restored))
                        })?;
                    }
                    debug_assert_eq!(context.t.get(undo.position), Some(&undo.token));
                }
                // A condition can retag the first token, or replace it,
                // and then reject. The lists were selected for a tin the
                // token no longer has, and the plain scan would test every
                // later alternate against the token as it is now: resume
                // after this alternate, and select again at the top.
                if lists.is_some() && context.t.first().map(|t0| t0.tin) != Some(key_tin) {
                    lists = None;
                    next_idx = idx + 1;
                }
            }

            if let Some(idx) = matched_alt_idx {
                // Copy matched tokens
                let matched_tokens = matched_tokens.unwrap_or_else(|| {
                    Rc::new(context.t.iter().take(matched_count).cloned().collect())
                });
                if is_open {
                    current_rule.o = matched_tokens;
                } else {
                    current_rule.c = matched_tokens;
                }

                // Compatibility modifier for the original two-argument Rust
                // callback tier. It rewrites the source spec before dynamic
                // fields are resolved. The full `h_match` callback below runs
                // at the canonical point over the resolved AltMatch.
                //
                // Rewriting is the only thing here that needs an alternate of
                // its own; everything below reads one. A grammar that
                // declares no modifier — which is most of them, and both
                // benchmark grammars — now borrows the installed alternate
                // instead of copying it once per rule step.
                let rewritten: AltSpec;
                let alt: &AltSpec = if let Some(modifier) = alts[idx].h.clone() {
                    context.set_rule(&current_rule);
                    let source = alts[idx].clone();
                    let result = self.catch_callback("alternate modifier", src, || {
                        modifier(source, &mut current_rule, &mut context)
                    });
                    rewritten = result.map_err(|error| {
                        self.attach_error(
                            error,
                            &current_rule,
                            &stack,
                            alts,
                            Self::phase_token(&current_rule),
                        )
                    })?;
                    &rewritten
                } else {
                    &alts[idx]
                };

                matched.h = alt.h_match.clone();
                if !alt.n.is_empty() {
                    matched.n = alt.n.clone();
                }
                if !alt.u.is_empty() {
                    matched.u = alt.u.clone();
                }
                if !alt.k.is_empty() {
                    matched.k = alt.k.clone();
                }
                // The alternate's group tags and action order, both worked
                // out when the rule was installed. A modifier rewrites the
                // whole `AltSpec` per step, so a rule that carries one is
                // resolved from what the modifier produced, not from the
                // record.
                let prepared_alt = if alts[idx].h.is_some() {
                    None
                } else {
                    prepared.and_then(|prepared| prepared.alt(is_open, idx))
                };
                match prepared_alt {
                    // `clone_from` writes into the list the last step left
                    // behind -- `AltMatch::reset` clears it without giving
                    // up its capacity -- rather than growing a fresh one.
                    Some(prepared_alt) => matched.g.clone_from(&prepared_alt.group_tags),
                    None if !alt.g.is_empty() => {
                        matched.g = listed(&alt.g).map(str::to_owned).collect();
                    }
                    None => {}
                }
                // Publishing the order into the record is only observable
                // when something this step runs receives the record. When
                // nothing does -- no matched condition, error, route,
                // backtrack, modifier or action, and no named binding that
                // could resolve to a matched action -- the step runs the
                // prepared order in place and the record keeps the empty
                // list it was born with. `matched_actions` is public and
                // may be written after `add_rule`, so whether a name can
                // reach it is asked here, per step, never cached.
                let prepared_alt = prepared_alt.filter(|prepared_alt| {
                    !prepared_alt.observed
                        && (!prepared_alt.named || self.matched_actions.is_empty())
                });
                if prepared_alt.is_none() {
                    let actions = resolved_alt_action_order(
                        &alt.a,
                        &alt.action_fns,
                        &alt.matched_action_fns,
                        &alt.action_order,
                    );
                    if !actions.is_empty() {
                        matched.actions = actions;
                    }
                }
                if !alt.action_configs.is_empty() {
                    matched.action_configs = alt.action_configs.clone();
                }

                if let Some(route) = &alt.p_fn {
                    context.set_rule(&current_rule);
                    matched.p = self
                        .catch_callback("alternate push", src, || {
                            route(&mut current_rule, &mut context)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                }
                if let Some(route) = &alt.p_match {
                    context.set_rule(&current_rule);
                    matched.p = self
                        .catch_callback("matched alternate push", src, || {
                            route(&mut current_rule, &mut context, &mut matched)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                } else if alt.p_fn.is_none() {
                    if let Some(route) = alt.p.clone() {
                        matched.p = (!route.is_empty()).then_some(route);
                    }
                }
                if let Some(route) = &alt.r_fn {
                    context.set_rule(&current_rule);
                    matched.r = self
                        .catch_callback("alternate replace", src, || {
                            route(&mut current_rule, &mut context)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                }
                if let Some(route) = &alt.r_match {
                    context.set_rule(&current_rule);
                    matched.r = self
                        .catch_callback("matched alternate replace", src, || {
                            route(&mut current_rule, &mut context, &mut matched)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .filter(|name| !name.is_empty());
                } else if alt.r_fn.is_none() {
                    if let Some(route) = alt.r.clone() {
                        matched.r = (!route.is_empty()).then_some(route);
                    }
                }
                if let Some(backtrack) = &alt.b_fn {
                    context.set_rule(&current_rule);
                    matched.b = self
                        .catch_callback("alternate backtrack", src, || {
                            backtrack(&mut current_rule, &mut context)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?;
                }
                if let Some(backtrack) = &alt.b_match {
                    context.set_rule(&current_rule);
                    matched.b = self
                        .catch_callback("matched alternate backtrack", src, || {
                            backtrack(&mut current_rule, &mut context, &mut matched)
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?;
                } else if alt.b_fn.is_none() && alt.b != 0 {
                    matched.b = alt.b;
                }

                if let Some(modifier) = alt.h_match.clone() {
                    context.set_rule(&current_rule);
                    let next = is_open.then(|| current_rule.snapshot());
                    matched = self
                        .catch_callback("matched alternate modifier", src, || {
                            modifier(
                                std::mem::take(&mut matched),
                                &mut current_rule,
                                &mut context,
                                next.as_deref(),
                            )
                        })
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?;
                }

                // The alternate's error hook runs AFTER the routing forms
                // have resolved and after both modifiers, and sees what the
                // modifier produced: routing, then modify, then check, in
                // every runtime (#154). It ran before the routing forms and
                // between the two modifiers here, which no grammar could
                // observe and no other port did.
                if let Some(error_hook) = alt.e.clone() {
                    context.set_rule(&current_rule);
                    let result = self.catch_callback("alternate error", src, || {
                        error_hook(&mut current_rule, &mut context)
                    });
                    matched.e = result
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .map(Box::new);
                }
                if let Some(error_hook) = alt.e_match.clone() {
                    context.set_rule(&current_rule);
                    let result = self.catch_callback("matched alternate error", src, || {
                        error_hook(&mut current_rule, &mut context, &mut matched)
                    });
                    matched.e = result
                        .map_err(|error| {
                            self.attach_error(
                                error,
                                &current_rule,
                                &stack,
                                alts,
                                Self::phase_token(&current_rule),
                            )
                        })?
                        .map(Box::new);
                }
                // Function-valued alternate errors are raised at the match
                // site, before counters, actions and consumption.
                if let Some(token) = matched.e.clone() {
                    let code = raised_error_code(&token);
                    let error = TabnasError::new(
                        code,
                        token.src.clone(),
                        src,
                        token.site.pos,
                        token.site.ri,
                        token.site.ci,
                    );
                    let done_alt = (!self.rule_done_subscribers.is_empty()).then(|| RuleDoneAlt {
                        b: matched.b,
                        g: matched.g.clone(),
                        p: matched.p.clone().unwrap_or_default(),
                        r: matched.r.clone().unwrap_or_default(),
                        err: Some((*token).clone()),
                    });
                    let error = self.attach_error(error, &current_rule, &stack, alts, Some(&token));
                    self.recover_error_pass(
                        error,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue;
                }

                // Update counters n
                for (k, v) in &matched.n {
                    if *v == 0 {
                        current_rule.n_mut().insert(k.clone(), 0);
                    } else {
                        *current_rule.n_mut().entry(k.clone()).or_insert(0) += *v;
                    }
                }

                // Update user props u
                for (k, v) in &matched.u {
                    current_rule.u_mut().insert(k.clone(), v.clone());
                }

                // Update keep props k
                for (k, v) in &matched.k {
                    current_rule.k_mut().insert(k.clone(), v.clone());
                }

                let backtrack = matched.b;
                let consumed = matched_count.saturating_sub(backtrack);
                context.record_consumed(consumed);

                // Run action. A bad token returned by a canonical action is
                // raised through the same recovery path as alt.e and
                // lifecycle actions; later actions must not run.
                let mut matched_action_error = None;
                let mut matched_action_token = None;
                // The published list has to be copied to be walked -- an
                // action may write the record it is being read out of, and
                // appending to `matched.actions` from inside this loop has
                // never reached it. The prepared list is not the record, so
                // it is walked where it lies.
                let published;
                let bindings: &[AltActionBinding] = match prepared_alt {
                    Some(prepared_alt) => &prepared_alt.actions,
                    None => {
                        published = matched.actions.clone();
                        &published
                    }
                };
                for binding in bindings {
                    let act_name = match binding {
                        AltActionBinding::Context(callback) => {
                            self.run_context_callback(
                                "alternate action",
                                callback,
                                &mut current_rule,
                                &mut context,
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?;
                            continue;
                        }
                        AltActionBinding::Matched(callback) => {
                            context.set_rule(&current_rule);
                            let result = self
                                .catch_callback("matched alternate action", src, || {
                                    callback(&mut current_rule, &mut context, &mut matched)
                                })
                                .map_err(|error| {
                                    self.attach_action_error(
                                        error,
                                        src,
                                        &current_rule,
                                        &stack,
                                        alts,
                                    )
                                })?;
                            let token = result.map_err(|action_error| {
                                self.attach_action_error(
                                    action_error.into(),
                                    src,
                                    &current_rule,
                                    &stack,
                                    alts,
                                )
                            })?;
                            if let Some(token) = token.filter(|token| !token.err.is_empty()) {
                                matched_action_error = Some(self.raised_token_error(
                                    &token,
                                    &current_rule,
                                    ParseSite {
                                        source: src,
                                        stack: &stack,
                                        alts,
                                    },
                                ));
                                matched_action_token = Some(token);
                                break;
                            }
                            continue;
                        }
                        AltActionBinding::Named(name) => name,
                    };
                    if let Some(callback) = self.matched_actions.get(act_name) {
                        context.set_rule(&current_rule);
                        let result = self
                            .catch_callback("named matched alternate action", src, || {
                                callback(&mut current_rule, &mut context, &mut matched)
                            })
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?;
                        let token = result.map_err(|action_error| {
                            self.attach_action_error(
                                action_error.into(),
                                src,
                                &current_rule,
                                &stack,
                                alts,
                            )
                        })?;
                        if let Some(token) = token.filter(|token| !token.err.is_empty()) {
                            matched_action_error = Some(self.raised_token_error(
                                &token,
                                &current_rule,
                                ParseSite {
                                    source: src,
                                    stack: &stack,
                                    alts,
                                },
                            ));
                            matched_action_token = Some(token);
                            break;
                        }
                        continue;
                    }
                    match act_name.as_str() {
                        "@probeInit$" => {
                            current_rule
                                .k_mut()
                                .insert("pd_phase".into(), Value::Number(0.0));
                            let mark = Value::Number(context.mark() as f64);
                            current_rule.k_mut().insert("pd_mark".into(), mark);
                        }
                        "@probeDecide$" => {
                            let mark = current_rule.k.get("pd_mark").and_then(|value| {
                                if let Value::Number(mark) = value {
                                    usize::try_from(*mark as u64).ok()
                                } else {
                                    None
                                }
                            });
                            let Some(mark) = mark.filter(|mark| *mark <= context.v_abs) else {
                                let mut error = TabnasError::new("internal", "", src, 0, 1, 1);
                                error.detail =
                                    "@probeDecide$: phase-0 @probeInit$ did not record a valid mark"
                                        .into();
                                return Err(error);
                            };
                            if let Err(error) = self.ensure_lookahead(
                                &mut lexer,
                                &mut context,
                                &mut current_rule,
                                1,
                                mode,
                                ParseSite {
                                    source: src,
                                    stack: &stack,
                                    alts,
                                },
                            ) {
                                return Err(self.attach_error(
                                    error,
                                    &current_rule,
                                    &stack,
                                    alts,
                                    None,
                                ));
                            }
                            let disambiguator =
                                current_rule.k.get("pd_d").and_then(|value| match value {
                                    Value::String(name) => Some(name.as_str()),
                                    _ => None,
                                });
                            let phase = if context
                                .t
                                .first()
                                .is_some_and(|token| Some(token.name.as_str()) == disambiguator)
                            {
                                1.0
                            } else {
                                2.0
                            };
                            context.rewind(mark)?;
                            current_rule
                                .k_mut()
                                .insert("pd_phase".into(), Value::Number(phase));
                        }
                        _ => self
                            .run_action_with_config(
                                act_name,
                                &mut current_rule,
                                &mut context,
                                matched.action_configs.get(act_name),
                            )
                            .map_err(|error| {
                                self.attach_action_error(error, src, &current_rule, &stack, alts)
                            })?,
                    }
                }
                if let Some(error) = matched_action_error {
                    let recovered_alt = Some(RuleDoneAlt {
                        b: matched.b,
                        g: matched.g.clone(),
                        p: matched.p.clone().unwrap_or_default(),
                        r: matched.r.clone().unwrap_or_default(),
                        err: None,
                    });
                    self.recover_error_pass(
                        error,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        recovered_alt,
                        matched_action_token.is_some(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    update_partial(mode, &root_node, &current_rule, &stack);
                    continue 'parse;
                }
                update_partial(mode, &root_node, &current_rule, &stack);

                // The canonical action receives the live match record. Its
                // post-action p/r writes are a supported routing channel, so
                // resolve the transition only after the action sequence.
                // Nothing below reads `matched.p` or `matched.r` again: the
                // record's remaining readers take `b` and `g`. So the names
                // move out of it rather than being copied, which is the
                // second `String` each of them cost per rule step.
                let push_name = matched.p.take();
                let replace_name = matched.r.take();
                // Only a ruleDone subscriber ever reads this, and it is
                // cloned again at each of the transition arms below. A
                // grammar with no subscriber was building and copying it
                // several times per matched alternate for nobody.
                let done_alt = (!self.rule_done_subscribers.is_empty()).then(|| RuleDoneAlt {
                    b: matched.b,
                    g: matched.g.clone(),
                    p: push_name.clone().unwrap_or_default(),
                    r: replace_name.clone().unwrap_or_default(),
                    err: None,
                });

                // Callback routes and action mutations cannot be validated at
                // grammar-install time. Reject an unknown destination at the
                // canonical point: after the matched action, but before any
                // lifecycle after-action or transition.
                //
                // Resolving it here also settles the transition below: the
                // arms take the shared name and the spec out of this one
                // lookup rather than hashing the same name twice more.
                // `push` wins over `replace` in the arms below, so this
                // resolves whichever of the two the parse will take.
                //
                // A route the grammar declared on this alternate was already
                // resolved when the rule was installed; all that is left of
                // it here is checking that the name the step is actually
                // routing to is still that name, which is a byte compare
                // against a handle rather than a hash of the same three
                // bytes for the tens of thousands of steps that route where
                // the grammar said they would.
                let mut route = match push_name.as_deref().or(replace_name.as_deref()) {
                    Some(name) => match prepared
                        .and_then(|prepared| prepared.alt(is_open, idx))
                        .map(|alt| if push_name.is_some() { &alt.p } else { &alt.r })
                        .and_then(|route| route.resolved(name))
                        .or_else(|| self.installed(name))
                    {
                        Some(resolved) => Some(resolved),
                        None => {
                            let mut token = Self::phase_token(&current_rule)
                                .cloned()
                                .or_else(|| context.t.first().cloned())
                                .unwrap_or_else(Token::no_token);
                            token.bad("unknown_rule");
                            token
                                .use_data_mut()
                                .insert("rulename".into(), Value::String(name.to_string()));
                            let error = self.raised_token_error(
                                &token,
                                &current_rule,
                                ParseSite {
                                    source: src,
                                    stack: &stack,
                                    alts,
                                },
                            );
                            self.recover_error_pass(
                                error,
                                if is_open {
                                    RuleState::Open
                                } else {
                                    RuleState::Close
                                },
                                done_alt,
                                false,
                                src,
                                &mut current_rule,
                                &mut stack,
                                &mut context,
                                &mut lexer,
                                mode,
                            )?;
                            update_partial(mode, &root_node, &current_rule, &stack);
                            continue 'parse;
                        }
                    },
                    None => None,
                };

                // Resolve the transition before running lifecycle after-actions,
                // so they can inspect rule.next just like the canonical engine.
                // The action still belongs to the rule whose alternate matched.
                let completed_rule: Option<Rule>;
                let mut completed_value = None;
                if push_name.is_some() {
                    let (push_slot, push_shared, push_spec) =
                        route.take().expect("a push route was resolved above");
                    let mut child = Rule::bound(
                        push_shared.clone(),
                        current_rule.node.clone(),
                        Some(push_spec),
                        push_slot,
                    );
                    child.i = next_rule_id;
                    next_rule_id += 1;
                    child.d = stack.len() + 1;
                    child.parent_node = Some(current_rule.node.clone());
                    child.n = Rc::clone(&current_rule.n);
                    child.k = Rc::clone(&current_rule.k);
                    child.parent_rule = Some(current_rule.snapshot());
                    current_rule.next_rule_name = Some(push_shared);
                    current_rule.child_rule = Some(child.snapshot());
                    current_rule.next_rule = current_rule.child_rule.clone();
                    current_rule.note_child_push(&child);
                    let after = self.run_after_actions(
                        spec,
                        prepared,
                        is_open,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    if is_open {
                        current_rule.state = RuleState::Close;
                    }
                    child.parent_rule = Some(current_rule.snapshot());
                    completed_rule = self.rule_done_copy(&current_rule);
                    // The child about to run shares this rule's node cell,
                    // and `child_node` may be a second handle on the very
                    // container it will write into. Let go of it for the
                    // duration -- see `Rule::park_child_node`. Taken after
                    // `rule_done_copy`, so a ruleDone subscriber still sees
                    // the rule exactly as it stood, and only when every pop
                    // that can resume this rule overwrites the field first
                    // (see `park_child_node` and `attempt_recover`).
                    if park_child_nodes {
                        current_rule.park_child_node();
                    }
                    stack.push(current_rule);
                    current_rule = child;
                } else if replace_name.is_some() {
                    let (replace_slot, replace_shared, replace_spec) =
                        route.take().expect("a replace route was resolved above");
                    let mut next = Rule::bound(
                        replace_shared.clone(),
                        current_rule.node.clone(),
                        Some(replace_spec),
                        replace_slot,
                    );
                    next.i = next_rule_id;
                    next_rule_id += 1;
                    next.d = current_rule.d;
                    next.parent_node = current_rule.parent_node.clone();
                    next.parent_rule = current_rule.parent_rule.clone();
                    next.n = Rc::clone(&current_rule.n);
                    next.k = Rc::clone(&current_rule.k);
                    current_rule.next_rule_name = Some(replace_shared);
                    current_rule.next_rule = Some(next.snapshot());
                    let after = self.run_after_actions(
                        spec,
                        prepared,
                        is_open,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    if is_open {
                        current_rule.state = RuleState::Close;
                    }
                    next.prev_rule = Some(current_rule.snapshot());
                    // The rule being replaced stops existing here. If it is
                    // the one its parent PUSHED, the parent's `child` link
                    // stays on it -- TypeScript never relinks `rule.child`
                    // (rules.ts:665) and neither does Go (rule.go:1280) --
                    // so freeze the node cell and the record it ended on
                    // before it goes.
                    if let Some(parent) = stack.last_mut() {
                        parent.freeze_child(&current_rule);
                    }
                    // Moved rather than cloned: this arm hands the
                    // finished rule over instead of copying it, so the
                    // gate above has nothing to save here.
                    completed_rule = Some(current_rule);
                    current_rule = next;
                } else if is_open {
                    current_rule.next_rule_name = Some(current_rule.name.clone());
                    current_rule.next_rule = Some(current_rule.snapshot());
                    let after = self.run_after_actions(
                        spec,
                        prepared,
                        true,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Open,
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    current_rule.state = RuleState::Close;
                    completed_rule = self.rule_done_copy(&current_rule);
                } else {
                    // Close phase pop
                    current_rule.next_rule_name = stack.last().map(|rule| rule.name.clone());
                    current_rule.next_rule = stack.last().map(Rule::snapshot);
                    let after = self.run_after_actions(
                        spec,
                        prepared,
                        false,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Close,
                        done_alt.clone(),
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    let parent = stack.pop();
                    completed_rule = self.rule_done_copy(&current_rule);
                    if let Some(mut parent) = parent {
                        parent.accept_child(&current_rule);
                        current_rule = parent;
                    } else {
                        // Root rule popped! Done.
                        completed_value = Some(current_rule.node.borrow().clone());
                    }
                }
                if let Some(completed_rule) = &completed_rule {
                    self.notify_rule_done(
                        completed_rule,
                        &context,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        done_alt,
                        src,
                        &stack,
                    )?;
                }
                if let Some(value) = completed_value {
                    final_value = Some(value);
                    break;
                }
                update_partial(mode, &root_node, &current_rule, &stack);
            } else if alts.is_empty() {
                // A state with no alternatives performs an implicit empty
                // pass. It still resolves next and runs lifecycle after-actions.
                let completed_rule: Option<Rule>;
                let mut completed_value = None;
                if is_open {
                    current_rule.next_rule_name = Some(current_rule.name.clone());
                    current_rule.next_rule = Some(current_rule.snapshot());
                    let after = self.run_after_actions(
                        spec,
                        prepared,
                        true,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Open,
                        None,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    current_rule.state = RuleState::Close;
                    completed_rule = self.rule_done_copy(&current_rule);
                } else {
                    current_rule.next_rule_name = stack.last().map(|rule| rule.name.clone());
                    current_rule.next_rule = stack.last().map(Rule::snapshot);
                    let after = self.run_after_actions(
                        spec,
                        prepared,
                        false,
                        &mut current_rule,
                        &mut context,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    );
                    update_partial(mode, &root_node, &current_rule, &stack);
                    if self.recover_after_actions(
                        after,
                        RuleState::Close,
                        None,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )? {
                        continue 'parse;
                    }
                    let parent = stack.pop();
                    completed_rule = self.rule_done_copy(&current_rule);
                    if let Some(mut parent) = parent {
                        parent.accept_child(&current_rule);
                        current_rule = parent;
                    } else {
                        completed_value = Some(current_rule.node.borrow().clone());
                    }
                }
                if let Some(completed_rule) = &completed_rule {
                    self.notify_rule_done(
                        completed_rule,
                        &context,
                        if is_open {
                            RuleState::Open
                        } else {
                            RuleState::Close
                        },
                        None,
                        src,
                        &stack,
                    )?;
                }
                if let Some(value) = completed_value {
                    final_value = Some(value);
                    break;
                }
                update_partial(mode, &root_node, &current_rule, &stack);
            } else {
                // Declared alternatives exist, but none matched.
                if is_open {
                    if let Err(error) = self.ensure_lookahead(
                        &mut lexer,
                        &mut context,
                        &mut current_rule,
                        1,
                        mode,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    ) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    if let Some(capture) = mode.continuation.as_deref_mut() {
                        let base = failed_alt_tins(&context, alts, &self.options);
                        capture.failure = continuation_tins(
                            &context,
                            &current_rule,
                            &stack,
                            &self.rules,
                            &self.options,
                            0,
                            Some(&base),
                        );
                    }
                    let t0 = context.t.first().cloned();
                    let (src_token, si, ri, ci) = if let Some(t) = t0.as_ref() {
                        (t.src.to_string(), t.site.pos, t.site.ri, t.site.ci)
                    } else {
                        (String::new(), src.len(), 1, 1)
                    };
                    let code = t0.as_ref().map_or("unexpected", deferred_error_code);
                    let error = TabnasError::new(code, src_token, src, si, ri, ci);
                    let done_alt = (!alts.is_empty() && !self.rule_done_subscribers.is_empty())
                        .then(|| RuleDoneAlt {
                            b: 0,
                            g: Vec::new(),
                            p: String::new(),
                            r: String::new(),
                            err: t0.clone(),
                        });
                    let error = self.attach_error(error, &current_rule, &stack, alts, t0.as_ref());
                    self.recover_error_pass(
                        error,
                        RuleState::Open,
                        done_alt,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue;
                } else {
                    if let Err(error) = self.ensure_lookahead(
                        &mut lexer,
                        &mut context,
                        &mut current_rule,
                        1,
                        mode,
                        ParseSite {
                            source: src,
                            stack: &stack,
                            alts,
                        },
                    ) {
                        return Err(self.attach_error(error, &current_rule, &stack, alts, None));
                    }
                    if let Some(capture) = mode.continuation.as_deref_mut() {
                        let base = failed_alt_tins(&context, alts, &self.options);
                        capture.failure = continuation_tins(
                            &context,
                            &current_rule,
                            &stack,
                            &self.rules,
                            &self.options,
                            0,
                            Some(&base),
                        );
                    }
                    let token = context.t.first().cloned();
                    let (source, pos, row, col) = token.as_ref().map_or_else(
                        || (String::new(), src.chars().count(), 1, 1),
                        |value| {
                            (
                                value.src.to_string(),
                                value.site.pos,
                                value.site.ri,
                                value.site.ci,
                            )
                        },
                    );
                    let code = token.as_ref().map_or("unexpected", deferred_error_code);
                    let error = TabnasError::new(code, source, src, pos, row, col);
                    let done_alt = Some(RuleDoneAlt {
                        b: 0,
                        g: Vec::new(),
                        p: String::new(),
                        r: String::new(),
                        err: token.clone(),
                    });
                    let error =
                        self.attach_error(error, &current_rule, &stack, alts, token.as_ref());
                    self.recover_error_pass(
                        error,
                        RuleState::Close,
                        done_alt,
                        false,
                        src,
                        &mut current_rule,
                        &mut stack,
                        &mut context,
                        &mut lexer,
                        mode,
                    )?;
                    continue;
                }
            }
        }

        let res = final_value.unwrap_or(Value::Null).unwrap_undefined();
        if mode.recovering {
            mode.partial = Some(res.clone());
        }

        // Post-loop check: ensure no unexpected trailing tokens. Recovery
        // keeps the completed value and reports the trailing fault.
        if let Err(error) = self.ensure_lookahead(
            &mut lexer,
            &mut context,
            &mut current_rule,
            1,
            mode,
            ParseSite {
                source: src,
                stack: &stack,
                alts: &[],
            },
        ) {
            let error = self.attach_error(error, &current_rule, &stack, &[], None);
            if mode.recovering {
                if mode.errors.last() != Some(&error) {
                    mode.errors.push(error.clone());
                    context.errs.push(error);
                }
                return Ok(res);
            }
            return Err(error);
        }
        if let Some(t0) = context.t.first() {
            if t0.tin != TIN_ZZ {
                let code = if t0.tin == TIN_BD && !t0.why.is_empty() {
                    t0.why.as_str()
                } else {
                    "unexpected"
                };
                let error =
                    TabnasError::new(code, &*t0.src, src, t0.site.pos, t0.site.ri, t0.site.ci);
                let error = self.attach_error(
                    error,
                    &current_rule,
                    &stack,
                    &[AltSpec {
                        s: vec![vec![TIN_ZZ]],
                        ..Default::default()
                    }],
                    Some(t0),
                );
                if mode.recovering {
                    if mode.errors.last() != Some(&error) {
                        mode.errors.push(error.clone());
                        context.errs.push(error);
                    }
                    return Ok(res);
                }
                return Err(error);
            }
        }
        if self
            .options
            .result
            .fail
            .iter()
            .any(|failed| failed.deep_equal(&res))
        {
            let token = context.t.first();
            let error = token.map_or_else(
                || TabnasError::new("unexpected", "", src, 0, 1, 1),
                |token| {
                    TabnasError::new(
                        "unexpected",
                        &*token.src,
                        src,
                        token.site.pos,
                        token.site.ri,
                        token.site.ci,
                    )
                },
            );
            if mode.recovering {
                mode.errors.push(error.clone());
                context.errs.push(error);
                return Ok(res);
            }
            return Err(error);
        }
        Ok(res)
    }
}

/// Keep the best partial result the recovery path would return.
///
/// Ten sites in the parse loop call this, twelve times per input construct
/// on the benchmark grammars, and outside recovery every one of them is a
/// load and a branch wrapped in a call. The guard is inline so the call
/// goes away; the search behind it stays out of line, because a parse that
/// is recovering is not the one being measured.
#[inline]
fn update_partial(
    mode: &mut ParseMode<'_>,
    root_node: &std::rc::Rc<std::cell::RefCell<Value>>,
    current_rule: &Rule,
    stack: &[Rule],
) {
    if mode.recovering {
        mode.partial = best_partial_value(root_node, current_rule, stack);
    }
}

#[inline(never)]
fn best_partial_value(
    root_node: &std::rc::Rc<std::cell::RefCell<Value>>,
    current_rule: &Rule,
    stack: &[Rule],
) -> Option<Value> {
    let usable = |value: Value| (!matches!(value, Value::Undefined | Value::Null)).then_some(value);

    usable(root_node.borrow().clone())
        .or_else(|| {
            stack
                .iter()
                .find_map(|rule| usable(rule.node.borrow().clone()))
        })
        .or_else(|| usable(current_rule.node.borrow().clone()))
        .map(Value::unwrap_undefined)
}

fn error_token(error: &TabnasError) -> Token {
    let byte_position = error
        .full_source
        .char_indices()
        .nth(error.pos)
        .map_or(error.full_source.len(), |(index, _)| index);
    let mut token = Token::new(
        "#BD",
        TIN_BD,
        Value::Undefined,
        error.src.clone(),
        crate::Point {
            len: error.len,
            site: crate::Site {
                si: byte_position,
                pos: error.pos,
                ri: error.row,
                ci: error.col,
            },
        },
    );
    token.err = crate::TokenCode::from(error.code.as_str());
    token.why = token.err.clone();
    token
}

fn deferred_error_code(token: &Token) -> &str {
    if token.tin != TIN_BD {
        "unexpected"
    } else if !token.why.is_empty() {
        &token.why
    } else if !token.err.is_empty() {
        &token.err
    } else {
        "unexpected"
    }
}

fn raised_error_code(token: &Token) -> &str {
    if !token.err.is_empty() {
        &token.err
    } else if !token.why.is_empty() {
        &token.why
    } else {
        "unexpected"
    }
}

fn mid_construct(code: &str) -> bool {
    matches!(code, "unprintable" | "invalid_unicode" | "invalid_ascii")
}

fn absorb_lex_error(
    error: &TabnasError,
    context: &mut Context,
    options: &Options,
    errors: &mut Vec<TabnasError>,
) -> bool {
    let recover = &options.parse.recover;
    if errors.len() >= recover.max_recoveries {
        return false;
    }

    let end = error.pos.saturating_add(error.len.max(1));
    if context.bad_to.is_some_and(|bad_to| error.pos <= bad_to) {
        if let Some(index) = context.bad_error {
            if let Some(previous) = errors.get_mut(index) {
                let recovered = previous.recovered.get_or_insert(crate::RecoveredAt {
                    skipped: 0,
                    sync: None,
                    bad: true,
                });
                recovered.skipped = recovered.skipped.saturating_add(1);
                let skipped = recovered.skipped;
                if recover.max_skip < skipped {
                    return false;
                }
                if let Some(context_previous) = context.errs.get_mut(index) {
                    *context_previous = previous.clone();
                }
                context.bad_to = Some(end.max(context.bad_to.unwrap_or_default()));
                return true;
            }
        }
    }

    let suppressed = context
        .recover_at
        .is_some_and(|at| context.v_abs.saturating_sub(at) < recover.suppress);
    if suppressed {
        context.bad_error = None;
        context.bad_to = Some(end);
        return true;
    }

    let mut recorded = error.clone();
    recorded.recovered = Some(crate::RecoveredAt {
        skipped: 1,
        sync: None,
        bad: true,
    });
    errors.push(recorded.clone());
    context.errs.push(recorded);
    context.bad_error = Some(errors.len() - 1);
    context.bad_to = Some(end);
    context.recover_at = Some(context.v_abs);
    true
}

fn slot_matches(slot: &[Tin], tin: Tin) -> bool {
    tin != TIN_BD && (slot.is_empty() || slot.contains(&tin) || slot.contains(&TIN_AA))
}

fn alt_match_depth(alt: &AltSpec, context: &Context) -> usize {
    let mut depth = 0;
    while depth < alt.s.len() {
        let Some(token) = context.t.get(depth) else {
            break;
        };
        if !slot_matches(&alt.s[depth], token.tin) {
            break;
        }
        depth += 1;
    }
    depth
}

fn completion_tins(slot: &[Tin]) -> impl Iterator<Item = Tin> + '_ {
    slot.iter()
        .copied()
        .chain(slot.is_empty().then_some(TIN_AA))
}

fn failed_alt_tins(context: &Context, alts: &[AltSpec], options: &Options) -> Vec<Tin> {
    let mut out = BTreeSet::new();
    for alt in alts {
        if !groups_enabled(alt, options) {
            continue;
        }
        let depth = alt_match_depth(alt, context);
        if let Some(slot) = alt.s.get(depth) {
            out.extend(completion_tins(slot));
        }
    }
    out.into_iter().collect()
}

fn lead_tins(alts: &[AltSpec], options: &Options, out: &mut BTreeSet<Tin>) {
    for alt in alts {
        if !groups_enabled(alt, options) {
            continue;
        }
        if let Some(slot) = alt.s.first() {
            out.extend(completion_tins(slot));
        }
    }
}

fn has_empty_close(spec: &RuleSpec, options: &Options) -> bool {
    spec.close
        .iter()
        .any(|alt| groups_enabled(alt, options) && alt.s.is_empty())
}

fn alt_has_sync_group(alt: &AltSpec, sync_groups: &[String]) -> bool {
    alt.g
        .split(',')
        .map(str::trim)
        .any(|tag| sync_groups.iter().any(|wanted| wanted == tag))
}

fn add_close_tins(
    rule: &Rule,
    rules: &IndexMap<String, Arc<RuleSpec>>,
    options: &Options,
    tagged_only: bool,
    out: &mut BTreeSet<Tin>,
) {
    let Some(spec) = rules.get(&*rule.name) else {
        return;
    };
    for alt in &spec.close {
        if !groups_enabled(alt, options)
            || (tagged_only && !alt_has_sync_group(alt, &options.parse.recover.sync_groups))
        {
            continue;
        }
        if let Some(slot) = alt.s.first() {
            out.extend(completion_tins(slot));
        }
    }
}

fn compute_sync_tins(
    rule: &Rule,
    stack: &[Rule],
    rules: &IndexMap<String, Arc<RuleSpec>>,
    options: &Options,
) -> BTreeSet<Tin> {
    let mut out = BTreeSet::new();
    add_close_tins(rule, rules, options, true, &mut out);
    for parent in stack.iter().rev() {
        add_close_tins(parent, rules, options, true, &mut out);
    }
    if out.is_empty() {
        add_close_tins(rule, rules, options, false, &mut out);
        for parent in stack.iter().rev() {
            add_close_tins(parent, rules, options, false, &mut out);
        }
    }
    for name in &options.parse.recover.sync_tokens {
        if let Some(tin) = options.token(name) {
            out.insert(tin);
        }
        if let Some(tins) = options.token_set.get(name.trim_start_matches('#')) {
            out.extend(tins.iter().copied());
        }
    }
    out
}

fn accepts_close(
    rule: &Rule,
    tin: Tin,
    rules: &IndexMap<String, Arc<RuleSpec>>,
    options: &Options,
) -> bool {
    rules.get(&*rule.name).is_some_and(|spec| {
        spec.close.iter().any(|alt| {
            groups_enabled(alt, options)
                && (alt.s.is_empty() || alt.s.first().is_some_and(|slot| slot_matches(slot, tin)))
        })
    })
}

fn add_openers(
    name: &str,
    rules: &IndexMap<String, Arc<RuleSpec>>,
    options: &Options,
    opened: &mut BTreeSet<String>,
    out: &mut BTreeSet<Tin>,
) {
    if name.is_empty() || !opened.insert(name.to_owned()) {
        return;
    }
    let Some(spec) = rules.get(name) else {
        return;
    };
    lead_tins(&spec.open, options, out);
    for alt in &spec.open {
        if !groups_enabled(alt, options) || !alt.s.is_empty() {
            continue;
        }
        if let Some(push) = alt.p.as_deref() {
            add_openers(push, rules, options, opened, out);
        }
        if let Some(replace) = alt.r.as_deref() {
            add_openers(replace, rules, options, opened, out);
        }
    }
}

fn continuation_tins(
    context: &Context,
    rule: &Rule,
    stack: &[Rule],
    rules: &IndexMap<String, Arc<RuleSpec>>,
    options: &Options,
    query_pos: usize,
    failed: Option<&[Tin]>,
) -> Vec<Tin> {
    let Some(spec) = rules.get(&*rule.name) else {
        return Vec::new();
    };
    let state_alts = if rule.state == RuleState::Open {
        &spec.open
    } else {
        &spec.close
    };
    let mut out = BTreeSet::new();

    if let Some(failed) = failed.filter(|tins| !tins.is_empty()) {
        out.extend(failed.iter().copied());
    } else {
        for alt in state_alts {
            if !groups_enabled(alt, options) {
                continue;
            }
            let depth = alt_match_depth(alt, context);
            if depth == query_pos {
                if let Some(slot) = alt.s.get(depth) {
                    out.extend(completion_tins(slot));
                }
            }
        }
    }

    // If the current rule can close without consuming a token, closing
    // tokens accepted by each parent are legal at the same point too.
    let mut close_rule = rule;
    let mut parent_index = stack.len();
    while let Some(close_spec) = rules.get(&*close_rule.name) {
        if !has_empty_close(close_spec, options) || parent_index == 0 {
            break;
        }
        parent_index -= 1;
        let parent = &stack[parent_index];
        if let Some(parent_spec) = rules.get(&*parent.name) {
            lead_tins(&parent_spec.close, options, &mut out);
        }
        close_rule = parent;
    }

    // A fully matched alternate can immediately hand control to a pushed or
    // replacement rule. Follow empty opening hand-offs transitively.
    let mut opened = BTreeSet::new();
    for alt in state_alts {
        if !groups_enabled(alt, options) || alt_match_depth(alt, context) != alt.s.len() {
            continue;
        }
        // A callback backtrack is only knowable while executing the match.
        // Do not speculate that its static default is the handover point.
        if alt.b_fn.is_some() {
            continue;
        }
        if alt.s.len().checked_sub(alt.b) != Some(query_pos) {
            continue;
        }
        if let Some(push) = alt.p.as_deref() {
            add_openers(push, rules, options, &mut opened, &mut out);
        }
        if let Some(replace) = alt.r.as_deref() {
            add_openers(replace, rules, options, &mut opened, &mut out);
        }
    }

    out.into_iter().collect()
}

/// The group tags a comma-separated group list declares.
///
/// One definition, because two places ask: the include/exclude filter in
/// `groups_enabled`, and the list the engine publishes into `matched.g`.
/// They have to agree on what a tag is -- trimmed, and never empty.
fn listed(list: &str) -> impl Iterator<Item = &str> {
    list.split(',')
        .map(str::trim)
        .filter(|entry| !entry.is_empty())
}

pub(crate) fn groups_enabled(alt: &AltSpec, options: &Options) -> bool {
    // With neither an include nor an exclude list there is nothing to
    // test against, so every alternate is enabled whatever groups it
    // declares. That is the usual case, and it is asked once per
    // alternate per iteration.
    let include = options.rule.include.as_str();
    let exclude = options.rule.exclude.as_str();
    if include.is_empty() && exclude.is_empty() {
        return true;
    }
    // Iterators rather than three collected `Vec<&str>`. The question is
    // the same one and the answer is the same answer; the three heap
    // allocations per call were not part of either. The shipped JSON
    // preset sets `rule.include` to "json", so the early return above
    // never fires for it and every alternate of every rule step paid
    // them.
    let declares = |wanted: &str| listed(&alt.g).any(|group| group == wanted);
    let included = listed(include).next().is_none() || listed(include).any(&declares);
    included && !listed(exclude).any(&declares)
}

fn builtin_condition_matches(reference: Option<&str>, rule: &Rule) -> bool {
    let phase = match rule.k.get("pd_phase") {
        Some(Value::Number(value)) => *value as i32,
        _ => 0,
    };
    match reference {
        None => true,
        Some("@probePhase0$") => phase == 0,
        Some("@probePhase1$") => phase == 1,
        Some("@probePhase2$") => phase == 2,
        Some(_) => false,
    }
}

fn conditions_match(conditions: &[Condition], rule: &Rule, ancestors: &[Rule]) -> bool {
    conditions.iter().all(|condition| {
        let resolved = resolve_condition_path(rule, ancestors, &condition.path);
        if condition.op == CompareOp::Exist {
            let exists = condition_exists(rule, ancestors, &condition.path);
            let wanted = matches!(condition.value, Value::Bool(true));
            return exists == wanted;
        }
        let Some(actual) = resolved else {
            return !matches!(condition.op, CompareOp::Eq);
        };
        match condition.op {
            CompareOp::Eq => actual.deep_equal(&condition.value),
            CompareOp::Ne => !actual.deep_equal(&condition.value),
            CompareOp::Lt => ordered(&actual, &condition.value).is_none_or(|value| value < 0),
            CompareOp::Lte => ordered(&actual, &condition.value).is_none_or(|value| value <= 0),
            CompareOp::Gt => ordered(&actual, &condition.value).is_none_or(|value| value > 0),
            CompareOp::Gte => ordered(&actual, &condition.value).is_none_or(|value| value >= 0),
            CompareOp::Exist => unreachable!("handled above"),
        }
    })
}

fn ordered(left: &Value, right: &Value) -> Option<i8> {
    match (left, right) {
        (Value::Number(left), Value::Number(right)) => Some(if left < right {
            -1
        } else if left > right {
            1
        } else {
            0
        }),
        (Value::String(left), Value::String(right)) => Some(match left.cmp(right) {
            std::cmp::Ordering::Less => -1,
            std::cmp::Ordering::Equal => 0,
            std::cmp::Ordering::Greater => 1,
        }),
        _ => None,
    }
}

fn condition_exists(rule: &Rule, ancestors: &[Rule], path: &[String]) -> bool {
    if path.first().map(String::as_str) == Some("n") && path.len() == 2 {
        rule.n.contains_key(&path[1])
    } else if path.first().map(String::as_str) == Some("parent") {
        ancestors
            .split_last()
            .is_some_and(|(parent, parent_ancestors)| {
                condition_exists(parent, parent_ancestors, &path[1..])
            })
    } else if path.first().map(String::as_str) == Some("child") {
        rule.child_rule
            .as_deref()
            .is_some_and(|child| snapshot_condition_exists(child, &path[1..]))
    } else if path.first().map(String::as_str) == Some("prev") {
        rule.prev_rule
            .as_deref()
            .is_some_and(|prev| snapshot_condition_exists(prev, &path[1..]))
    } else if path.first().map(String::as_str) == Some("next") {
        if let Some(next) = rule.next_rule.as_deref() {
            snapshot_condition_exists(next, &path[1..])
        } else if rule.next_rule_name.as_deref() == Some(&*rule.name) {
            condition_exists(rule, ancestors, &path[1..])
        } else {
            false
        }
    } else {
        resolve_condition_path(rule, ancestors, path).is_some()
    }
}

fn resolve_condition_path(rule: &Rule, ancestors: &[Rule], path: &[String]) -> Option<Value> {
    let root = path.first()?.as_str();
    let rest = &path[1..];
    match root {
        "n" if rest.len() == 1 => Some(Value::Number(*rule.n.get(&rest[0]).unwrap_or(&0) as f64)),
        "u" => map_path(&rule.u, rest),
        "k" => map_path(&rule.k, rest),
        "d" if rest.is_empty() => Some(Value::Number(rule.d as f64)),
        "i" if rest.is_empty() => Some(Value::Number(rule.i as f64)),
        "name" if rest.is_empty() => Some(Value::String(rule.name.to_string())),
        "state" if rest.is_empty() => Some(Value::String(
            match rule.state {
                RuleState::Open => "o",
                RuleState::Close => "c",
            }
            .into(),
        )),
        "node" => value_path(rule.node.borrow().clone(), rest),
        "need" if rest.is_empty() => Some(Value::Number(rule.need as f64)),
        "oN" if rest.is_empty() => Some(Value::Number(rule.o.len() as f64)),
        "cN" if rest.is_empty() => Some(Value::Number(rule.c.len() as f64)),
        "o" => token_list_path(&rule.o, rest),
        "c" => token_list_path(&rule.c, rest),
        "o0" => token_path(rule.o.first(), rest),
        "o1" => token_path(rule.o.get(1), rest),
        "c0" => token_path(rule.c.first(), rest),
        "c1" => token_path(rule.c.get(1), rest),
        "parent" => {
            let (parent, parent_ancestors) = ancestors.split_last()?;
            resolve_condition_path(parent, parent_ancestors, rest)
        }
        "child" => resolve_snapshot_path(rule.child_rule.as_deref()?, rest),
        "prev" => resolve_snapshot_path(rule.prev_rule.as_deref()?, rest),
        "next" => {
            if let Some(next) = rule.next_rule.as_deref() {
                resolve_snapshot_path(next, rest)
            } else if rule.next_rule_name.as_deref() == Some(&*rule.name) {
                resolve_condition_path(rule, ancestors, rest)
            } else {
                None
            }
        }
        "spec" if rest == ["name"] => Some(Value::String(rule.name.to_string())),
        _ => None,
    }
}

fn snapshot_condition_exists(rule: &RuleSnapshot, path: &[String]) -> bool {
    if path.first().map(String::as_str) == Some("n") && path.len() == 2 {
        rule.n.contains_key(&path[1])
    } else if path.first().map(String::as_str) == Some("parent") {
        rule.parent_rule
            .as_deref()
            .is_some_and(|parent| snapshot_condition_exists(parent, &path[1..]))
    } else if path.first().map(String::as_str) == Some("child") {
        rule.child_rule
            .as_deref()
            .is_some_and(|child| snapshot_condition_exists(child, &path[1..]))
    } else if path.first().map(String::as_str) == Some("prev") {
        rule.prev_rule
            .as_deref()
            .is_some_and(|prev| snapshot_condition_exists(prev, &path[1..]))
    } else if path.first().map(String::as_str) == Some("next") {
        if let Some(next) = rule.next_rule.as_deref() {
            snapshot_condition_exists(next, &path[1..])
        } else if rule.next_rule_name.as_deref() == Some(&*rule.name) {
            snapshot_condition_exists(rule, &path[1..])
        } else {
            false
        }
    } else {
        resolve_snapshot_path(rule, path).is_some()
    }
}

fn resolve_snapshot_path(rule: &RuleSnapshot, path: &[String]) -> Option<Value> {
    let root = path.first()?.as_str();
    let rest = &path[1..];
    match root {
        "n" if rest.len() == 1 => Some(Value::Number(*rule.n.get(&rest[0]).unwrap_or(&0) as f64)),
        "u" => map_path(&rule.u, rest),
        "k" => map_path(&rule.k, rest),
        "d" if rest.is_empty() => Some(Value::Number(rule.d as f64)),
        "i" if rest.is_empty() => Some(Value::Number(rule.i as f64)),
        "name" if rest.is_empty() => Some(Value::String(rule.name.to_string())),
        "state" if rest.is_empty() => Some(Value::String(
            match rule.state {
                RuleState::Open => "o",
                RuleState::Close => "c",
            }
            .into(),
        )),
        "node" => value_path(rule.node.borrow().clone(), rest),
        "need" if rest.is_empty() => Some(Value::Number(rule.need as f64)),
        "oN" if rest.is_empty() => Some(Value::Number(rule.o.len() as f64)),
        "cN" if rest.is_empty() => Some(Value::Number(rule.c.len() as f64)),
        "o" => token_list_path(&rule.o, rest),
        "c" => token_list_path(&rule.c, rest),
        "o0" => token_path(rule.o.first(), rest),
        "o1" => token_path(rule.o.get(1), rest),
        "c0" => token_path(rule.c.first(), rest),
        "c1" => token_path(rule.c.get(1), rest),
        "parent" => resolve_snapshot_path(rule.parent_rule.as_deref()?, rest),
        "child" => resolve_snapshot_path(rule.child_rule.as_deref()?, rest),
        "prev" => resolve_snapshot_path(rule.prev_rule.as_deref()?, rest),
        "next" => {
            if let Some(next) = rule.next_rule.as_deref() {
                resolve_snapshot_path(next, rest)
            } else if rule.next_rule_name.as_deref() == Some(&*rule.name) {
                resolve_snapshot_path(rule, rest)
            } else {
                None
            }
        }
        "spec" if rest == ["name"] => Some(Value::String(rule.name.to_string())),
        _ => None,
    }
}

fn map_path(map: &HashMap<String, Value>, path: &[String]) -> Option<Value> {
    let (name, rest) = path.split_first()?;
    value_path(map.get(name)?.clone(), rest)
}

fn value_path(mut value: Value, path: &[String]) -> Option<Value> {
    for part in path {
        value = match value {
            Value::Object(map) => map.get(part)?.clone(),
            Value::Array(items) => items.get(part.parse::<usize>().ok()?)?.clone(),
            _ => return None,
        };
    }
    Some(value)
}

fn token_list_path(tokens: &[Token], path: &[String]) -> Option<Value> {
    let (index, rest) = path.split_first()?;
    token_path(tokens.get(index.parse::<usize>().ok()?), rest)
}

fn token_path(token: Option<&Token>, path: &[String]) -> Option<Value> {
    let token = token?;
    if path.is_empty() {
        let mut value = IndexMap::new();
        value.insert("tin".into(), Value::Number(token.tin as f64));
        value.insert("name".into(), Value::String(token.name.to_string()));
        value.insert("src".into(), Value::String(token.src.to_string()));
        value.insert("val".into(), token.val.clone());
        value.insert("why".into(), Value::String(token.why.to_string()));
        return Some(Value::object(value));
    }
    let (field, rest) = path.split_first()?;
    let value = match field.as_str() {
        "tin" => Value::Number(token.tin as f64),
        "name" => Value::String(token.name.to_string()),
        "src" => Value::String(token.src.to_string()),
        "val" => token.val.clone(),
        "why" => Value::String(token.why.to_string()),
        _ => return None,
    };
    value_path(value, rest)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// `expected_tins`, `names` and `prepared` are derived from `rules` and
    /// positional in it, and `add_rule` is the only thing that writes any
    /// of the four. That is the whole reason `rules` is private: while it was
    /// public, an embedder inserting or replacing a rule straight into it
    /// left the derived tables describing the rule that used to be there,
    /// and lookahead went on gating the custom matchers on that rule's token
    /// identities. This asserts the invariant a second write path would
    /// break; making `add_rule` keep an existing entry instead of replacing
    /// it fails the second assertion.
    #[test]
    fn replacing_a_rule_replaces_the_tables_derived_from_it() {
        let mut parser = Parser::new(crate::Options::default());

        let mut first = RuleSpec::new("val");
        first.bo.push("@enter".into());
        first.open.push(AltSpec {
            s: vec![vec![crate::TIN_NR]],
            a: vec!["@act".into()],
            ..Default::default()
        });
        parser.add_rule(first);
        let slot = parser
            .rules()
            .get_index_of("val")
            .expect("the rule was just installed");
        assert_eq!(parser.expected_tins[slot].at(true, 0), [crate::TIN_NR]);
        assert_eq!(&*parser.names[slot], "val");
        assert_eq!(parser.prepared[slot].before(true).len(), 1);
        assert_eq!(parser.prepared[slot].open[0].actions.len(), 1);

        let mut second = RuleSpec::new("val");
        second.open.push(AltSpec {
            s: vec![vec![crate::TIN_ST]],
            ..Default::default()
        });
        parser.add_rule(second);

        assert_eq!(parser.rules().len(), 1, "a replacement, not an addition");
        assert_eq!(
            parser.expected_tins[slot].at(true, 0),
            [crate::TIN_ST],
            "lookahead would still expect the replaced rule's tokens"
        );
        // The name table is positional now, so a replacement has to land on
        // the index the map kept for the key rather than append beside it:
        // an appended handle would leave `names` describing the wrong rule
        // at every index past this one.
        assert_eq!(parser.names.len(), 1, "one rule, one shared name");
        assert_eq!(parser.rules().get_index_of("val"), Some(slot));
        assert_eq!(&*parser.names[slot], "val");
        // The prepared table is positional for the same reason, and its
        // record has to describe the rule that is installed now: the parse
        // loop trusts a record only while its spec is the one the rule in
        // hand holds, so a record left pointing at the replaced spec is a
        // record the loop would stop using at all.
        assert_eq!(parser.prepared.len(), 1);
        assert_eq!(&*parser.prepared[slot].name, "val");
        assert!(Arc::ptr_eq(
            &parser.prepared[slot].spec,
            &parser.rules()["val"]
        ));
        // The action orders are derived from the same spec and go stale the
        // same way. The replacement declares neither the lifecycle action
        // nor the alternate action the first rule did, and an order left
        // over from that rule is one the loop would run for it.
        assert!(parser.prepared[slot].before(true).is_empty());
        assert!(parser.prepared[slot].open[0].actions.is_empty());
        assert!(!parser.prepared[slot].open[0].named);
    }

    /// The accepted-token table is positional now, and the slot a rule
    /// carries is a hint rather than an authority: `name` is public and a
    /// callback may have written it between one token fetch and the next.
    /// What the rule is called is what decides which row answers, exactly
    /// as the name-keyed map this replaced decided it, and a name that is
    /// installed nowhere expects nothing rather than whatever happens to
    /// sit at its slot.
    #[test]
    fn expected_match_tins_follows_the_rule_name_when_the_slot_goes_stale() {
        fn rule_named(name: &str, tin: Tin) -> RuleSpec {
            let mut spec = RuleSpec::new(name);
            spec.open.push(AltSpec {
                s: vec![vec![tin]],
                ..Default::default()
            });
            spec
        }

        let mut options = crate::Options::default();
        let tin = options.register_token("#QQ");
        options.match_tokens.insert(
            "#QQ".into(),
            crate::options::MatchToken {
                name: "#QQ".into(),
                tin,
                matcher: crate::options::MatchTokenMatcher::Regex(regex::Regex::new("^q").unwrap()),
                eager: false,
            },
        );
        let mut parser = Parser::new(options);
        parser.add_rule(rule_named("val", crate::TIN_NR));
        parser.add_rule(rule_named("other", crate::TIN_ST));

        let val_slot = parser.rules().get_index_of("val").expect("installed");
        let other_slot = parser.rules().get_index_of("other").expect("installed");
        assert_ne!(val_slot, other_slot);

        // Bound the way the parse loop binds it: slot, name and spec all
        // describe the same installed rule.
        let mut rule = Rule::new("val", Value::Undefined);
        rule.bind_spec(
            &parser.rules()["val"],
            parser.names[val_slot].clone(),
            val_slot,
        );
        assert_eq!(&*parser.expected_match_tins(&rule, 0), [crate::TIN_NR]);

        // A callback renames the rule to another installed rule. The slot
        // still points at `val`, so the slot alone would answer with
        // `val`'s row; the name is what makes it `other`'s, which is what
        // the lookup by name always gave.
        rule.name = parser.names[other_slot].clone();
        assert_eq!(
            &*parser.expected_match_tins(&rule, 0),
            [crate::TIN_ST],
            "the rule is called `other` now, so `other`'s row answers"
        );

        // A callback swaps the spec and leaves the name. Nothing about
        // which row answers depends on the spec, because the row is
        // derived from the name the rule was installed under.
        rule.name = parser.names[val_slot].clone();
        rule.spec = Arc::clone(&parser.rules()["other"]);
        assert_eq!(
            &*parser.expected_match_tins(&rule, 0),
            [crate::TIN_NR],
            "still called `val`, so still gated by `val`'s row"
        );

        // A name installed nowhere expects nothing. Its slot is
        // `usize::MAX`, so nothing is in range to answer by accident.
        let ghost = Rule::new("ghost", Value::Undefined);
        assert_eq!(
            &*parser.expected_match_tins(&ghost, 0),
            &[] as &[Tin],
            "an uninstalled name has no row, not another rule's"
        );
    }

    /// `expected_match_tins` exists to gate the custom matchers, so with no
    /// custom matcher registered it answers "nothing" without consulting
    /// the table -- the table is still built and still says what it said,
    /// and the moment a match token appears the same rule at the same slot
    /// gets the table's row again. The first assertion is what pins the
    /// short-circuit: were it dropped, the empty-matcher parser would
    /// return `[TIN_NR]` and the assertion would fail.
    #[test]
    fn expected_match_tins_is_empty_until_a_match_token_exists() {
        fn val_rule() -> RuleSpec {
            let mut spec = RuleSpec::new("val");
            spec.open.push(AltSpec {
                s: vec![vec![crate::TIN_NR]],
                ..Default::default()
            });
            spec
        }
        let rule = Rule::new("val", Value::Undefined);

        let mut without = Parser::new(crate::Options::default());
        without.add_rule(val_rule());
        assert!(without.options.match_tokens.is_empty());
        assert_eq!(without.expected_tins[0].at(true, 0), [crate::TIN_NR]);
        assert_eq!(
            &*without.expected_match_tins(&rule, 0),
            &[] as &[Tin],
            "no custom matcher: nothing to gate, so nothing to look up"
        );

        let mut options = crate::Options::default();
        let tin = options.register_token("#QQ");
        options.match_tokens.insert(
            "#QQ".into(),
            crate::options::MatchToken {
                name: "#QQ".into(),
                tin,
                matcher: crate::options::MatchTokenMatcher::Regex(regex::Regex::new("^q").unwrap()),
                eager: false,
            },
        );
        let mut with = Parser::new(options);
        with.add_rule(val_rule());
        assert_eq!(
            &*with.expected_match_tins(&rule, 0),
            [crate::TIN_NR],
            "a custom matcher is present, so the slot's tins gate it"
        );
        assert_eq!(
            &*with.expected_match_tins(&rule, 1),
            &[] as &[Tin],
            "a slot the rule never fills expects nothing"
        );
    }
}
