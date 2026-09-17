// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, Token};
use crate::value::Value;
use crate::Lexer;
use crate::{ActionError, Context, ContextAction};
use std::cell::RefCell;
use std::collections::HashMap;
use std::fmt;
use std::rc::Rc;
use std::sync::Arc;

/// Function-valued alternate condition. The candidate's matched tokens have
/// already been copied onto `rule` when this callback runs.
pub type AltCondition = Arc<dyn Fn(&mut Rule, &mut Context) -> bool + Send + Sync>;

/// Canonical condition shape, including the effective match record being
/// assembled for this candidate.
pub type AltConditionWithMatch =
    Arc<dyn Fn(&mut Rule, &mut Context, &mut AltMatch) -> bool + Send + Sync>;

/// Condition with access to the live lexer. This is the imperative form used
/// by plugins that perform controlled lookahead from a condition.
pub type AltConditionWithLexer =
    Arc<dyn for<'source> Fn(&mut Rule, &mut Context, &mut Lexer<'source>) -> bool + Send + Sync>;

/// Complete canonical condition shape: the live effective match plus access
/// to the lexer for bounded, plugin-controlled lookahead.
pub type AltConditionWithLexerAndMatch = Arc<
    dyn for<'source> Fn(&mut Rule, &mut Context, &mut AltMatch, &mut Lexer<'source>) -> bool
        + Send
        + Sync,
>;

/// Function-valued push/replace route.
pub type AltNext = Arc<dyn Fn(&mut Rule, &mut Context) -> Option<String> + Send + Sync>;
pub type AltNextWithMatch =
    Arc<dyn Fn(&mut Rule, &mut Context, &mut AltMatch) -> Option<String> + Send + Sync>;

/// Function-valued token backtrack count.
pub type AltBack = Arc<dyn Fn(&mut Rule, &mut Context) -> usize + Send + Sync>;
pub type AltBackWithMatch =
    Arc<dyn Fn(&mut Rule, &mut Context, &mut AltMatch) -> usize + Send + Sync>;

/// Function-valued alternate error. Returning a token rejects the alternate
/// at its match site and uses the token's `err`/`why` field as the error code.
pub type AltError = Arc<dyn Fn(&mut Rule, &mut Context) -> Option<Token> + Send + Sync>;
pub type AltErrorWithMatch =
    Arc<dyn Fn(&mut Rule, &mut Context, &mut AltMatch) -> Option<Token> + Send + Sync>;

/// Post-match alternate modifier. It takes and returns the effective match so
/// replacement is explicit rather than mutating shared grammar state.
pub type AltModifier = Arc<dyn Fn(AltSpec, &mut Rule, &mut Context) -> AltSpec + Send + Sync>;

/// Full canonical alternate modifier. `next` is the pre-routing next-rule
/// view (`Some(current)` during open, `None` during close). The returned
/// match is the one used for erroring, state mutation, action, and routing.
pub type AltModifierWithMatch =
    Arc<dyn Fn(AltMatch, &mut Rule, &mut Context, Option<&RuleSnapshot>) -> AltMatch + Send + Sync>;

/// Full canonical alternate action. A returned error token aborts the pass;
/// other returned tokens are ignored by the mature engine and should be
/// expressed as `None` here.
pub type AltAction = Arc<
    dyn Fn(&mut Rule, &mut Context, &mut AltMatch) -> Result<Option<Token>, ActionError>
        + Send
        + Sync,
>;

#[derive(Clone)]
pub enum AltActionBinding {
    Named(String),
    Context(ContextAction),
    Matched(AltAction),
}

/// One executable action in its exact declaration position. The mature
/// engines allow named function references and direct callbacks to be mixed;
/// retaining this sequence avoids losing prepend/append order while keeping
/// the serialized `a`/`bo`/`ao`/`bc`/`ac` lists available for inspection.
#[derive(Clone)]
pub enum ActionBinding {
    Named(String),
    Callback(ContextAction),
    State(StateAction),
}

/// Canonical rule lifecycle callback (`bo`/`ao`/`bc`/`ac`). `next` is the
/// rule selected for the following pass, or `None` for the no-rule sentinel;
/// `out` is the previous handler's token result in the same phase.
pub type StateAction = Arc<
    dyn Fn(
            &mut Rule,
            &mut Context,
            Option<&RuleSnapshot>,
            Option<Token>,
        ) -> Result<Option<Token>, ActionError>
        + Send
        + Sync,
>;

/// Effective result of matching one alternate. Dynamic error/routing/backtrack
/// callbacks are resolved into this record before the modifier runs, matching
/// TypeScript's `AltMatch` contract.
#[derive(Clone, Default)]
pub struct AltMatch {
    pub p: Option<String>,
    pub r: Option<String>,
    pub b: usize,
    pub n: HashMap<String, i32>,
    pub u: HashMap<String, Value>,
    pub k: HashMap<String, Value>,
    pub g: Vec<String>,
    /// The alternate's error token, boxed. A `Token` is 248 bytes and this
    /// record is moved twice per rule step, so carrying one inline made
    /// `AltMatch` 560 bytes of which 248 were an error almost no alternate
    /// raises. The box costs an allocation only on the error path, which
    /// is already the expensive one.
    pub e: Option<Box<Token>>,
    /// Canonical post-match modifier attached to the selected alternate.
    /// It remains visible to the modifier itself and later actions even
    /// though changing it after selection does not rerun the phase.
    pub h: Option<AltModifierWithMatch>,
    pub actions: Vec<AltActionBinding>,
    pub action_configs: HashMap<String, Value>,
}

impl AltMatch {
    /// Return the record to the state `AltMatch::default()` would give it,
    /// without giving up the buffers it has already allocated.
    ///
    /// The parse loop keeps one record for the whole parse and resets it at
    /// the head of each rule step, the shape TypeScript's `ctx._palt` has
    /// (ts/src/rules.ts): building a fresh 320-byte record twice per step
    /// and moving it twice more cost more than the nine fields are worth.
    /// Every field an earlier step or a rejected alternate can leave behind
    /// has to be cleared here -- a step can leave by the error path with
    /// `e`, `p`, `r`, `b` and `g` still set -- and the maps and vectors are
    /// cleared rather than replaced so the next step writes into the
    /// capacity the last one left.
    pub(crate) fn reset(&mut self) {
        self.p = None;
        self.r = None;
        self.b = 0;
        self.e = None;
        self.h = None;
        if !self.n.is_empty() {
            self.n.clear();
        }
        if !self.u.is_empty() {
            self.u.clear();
        }
        if !self.k.is_empty() {
            self.k.clear();
        }
        if !self.g.is_empty() {
            self.g.clear();
        }
        if !self.actions.is_empty() {
            self.actions.clear();
        }
        if !self.action_configs.is_empty() {
            self.action_configs.clear();
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RuleState {
    Open,
    Close,
}

#[derive(Debug, Clone, PartialEq)]
pub struct RuleDoneAlt {
    pub b: usize,
    pub g: Vec<String>,
    pub p: String,
    pub r: String,
    pub err: Option<Token>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct RuleDone {
    /// Rule state before the completed pass.
    pub state: RuleState,
    /// `None` only when that state declared no alternatives.
    pub alt: Option<RuleDoneAlt>,
    /// True only for a close synthesized by recovery.
    pub forced: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CompareOp {
    Eq,
    Ne,
    Lt,
    Lte,
    Gt,
    Gte,
    Exist,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Condition {
    pub path: Vec<String>,
    pub op: CompareOp,
    pub value: Value,
}

#[derive(Clone, Default)]
pub struct AltSpec {
    pub s: Vec<Vec<Tin>>,
    pub p: Option<String>,
    pub p_fn: Option<AltNext>,
    pub p_match: Option<AltNextWithMatch>,
    pub r: Option<String>,
    pub r_fn: Option<AltNext>,
    pub r_match: Option<AltNextWithMatch>,
    pub b: usize,
    pub b_fn: Option<AltBack>,
    pub b_match: Option<AltBackWithMatch>,
    pub a: Vec<String>,
    /// Imperative actions installed directly by a native Rust plugin.
    /// Named actions in `a` remain the serialized grammar representation;
    /// `action_order` retains their exact interleaving with direct callbacks.
    pub action_fns: Vec<ContextAction>,
    pub action_order: Vec<AltActionBinding>,
    pub matched_action_fns: Vec<AltAction>,
    pub action_configs: HashMap<String, Value>,
    pub c: Vec<Condition>,
    pub c_ref: Option<String>,
    pub c_fn: Option<AltCondition>,
    pub c_match: Option<AltConditionWithMatch>,
    pub c_lex: Option<AltConditionWithLexer>,
    pub c_lex_match: Option<AltConditionWithLexerAndMatch>,
    pub n: HashMap<String, i32>,
    pub u: HashMap<String, Value>,
    pub k: HashMap<String, Value>,
    pub g: String,
    pub h: Option<AltModifier>,
    pub h_match: Option<AltModifierWithMatch>,
    pub e: Option<AltError>,
    pub e_match: Option<AltErrorWithMatch>,
}

impl AltSpec {
    pub fn new() -> Self {
        Self::default()
    }
}

#[derive(Clone)]
pub struct RuleSpec {
    pub name: String,
    pub open: Vec<AltSpec>,
    pub close: Vec<AltSpec>,
    pub bo: Vec<String>,
    pub ao: Vec<String>,
    pub bc: Vec<String>,
    pub ac: Vec<String>,
    /// Imperative lifecycle callbacks, parallel to the named serialized
    /// callback lists above.
    pub bo_fns: Vec<ContextAction>,
    pub ao_fns: Vec<ContextAction>,
    pub bc_fns: Vec<ContextAction>,
    pub ac_fns: Vec<ContextAction>,
    pub bo_state_fns: Vec<StateAction>,
    pub ao_state_fns: Vec<StateAction>,
    pub bc_state_fns: Vec<StateAction>,
    pub ac_state_fns: Vec<StateAction>,
    pub bo_order: Vec<ActionBinding>,
    pub ao_order: Vec<ActionBinding>,
    pub bc_order: Vec<ActionBinding>,
    pub ac_order: Vec<ActionBinding>,
}

impl fmt::Debug for RuleSpec {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("RuleSpec")
            .field("name", &self.name)
            .field("open", &self.open.len())
            .field("close", &self.close.len())
            .field("bo", &self.bo.len())
            .field("ao", &self.ao.len())
            .field("bc", &self.bc.len())
            .field("ac", &self.ac.len())
            .finish_non_exhaustive()
    }
}

impl RuleSpec {
    pub fn new(name: impl Into<String>) -> Self {
        RuleSpec {
            name: name.into(),
            open: Vec::new(),
            close: Vec::new(),
            bo: Vec::new(),
            ao: Vec::new(),
            bc: Vec::new(),
            ac: Vec::new(),
            bo_fns: Vec::new(),
            ao_fns: Vec::new(),
            bc_fns: Vec::new(),
            ac_fns: Vec::new(),
            bo_state_fns: Vec::new(),
            ao_state_fns: Vec::new(),
            bc_state_fns: Vec::new(),
            ac_state_fns: Vec::new(),
            bo_order: Vec::new(),
            ao_order: Vec::new(),
            bc_order: Vec::new(),
            ac_order: Vec::new(),
        }
    }

    /// Remove all alternates and lifecycle actions from this rule.
    pub fn clear(&mut self) -> &mut Self {
        self.open.clear();
        self.close.clear();
        self.clear_actions(&[]);
        self
    }

    pub fn add_open(&mut self, alt: AltSpec) -> &mut Self {
        self.open.push(alt);
        self
    }

    pub fn prepend_open(&mut self, alt: AltSpec) -> &mut Self {
        self.open.insert(0, alt);
        self
    }

    pub fn add_close(&mut self, alt: AltSpec) -> &mut Self {
        self.close.push(alt);
        self
    }

    pub fn prepend_close(&mut self, alt: AltSpec) -> &mut Self {
        self.close.insert(0, alt);
        self
    }

    pub fn clear_open(&mut self) -> &mut Self {
        self.open.clear();
        self
    }

    pub fn clear_close(&mut self) -> &mut Self {
        self.close.clear();
        self
    }

    /// Delete then move entries using the same signed-index rules as the
    /// serialized grammar `inject` object.
    pub fn modify_open(&mut self, mods: &crate::utility::ListMods<AltSpec>) -> &mut Self {
        self.open = crate::utility::modlist(std::mem::take(&mut self.open), Some(mods));
        self
    }

    pub fn modify_close(&mut self, mods: &crate::utility::ListMods<AltSpec>) -> &mut Self {
        self.close = crate::utility::modlist(std::mem::take(&mut self.close), Some(mods));
        self
    }

    pub fn add_bo(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.bo,
            &self.bo_fns,
            &self.bo_state_fns,
            &mut self.bo_order,
        );
        let action = infallible_action(action);
        self.bo_fns.push(action.clone());
        self.bo_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_bo(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.bo,
            &self.bo_fns,
            &self.bo_state_fns,
            &mut self.bo_order,
        );
        let action = infallible_action(action);
        self.bo_fns.insert(0, action.clone());
        self.bo_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_ao(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.ao,
            &self.ao_fns,
            &self.ao_state_fns,
            &mut self.ao_order,
        );
        let action = infallible_action(action);
        self.ao_fns.push(action.clone());
        self.ao_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_ao(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.ao,
            &self.ao_fns,
            &self.ao_state_fns,
            &mut self.ao_order,
        );
        let action = infallible_action(action);
        self.ao_fns.insert(0, action.clone());
        self.ao_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_bc(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.bc,
            &self.bc_fns,
            &self.bc_state_fns,
            &mut self.bc_order,
        );
        let action = infallible_action(action);
        self.bc_fns.push(action.clone());
        self.bc_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_bc(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.bc,
            &self.bc_fns,
            &self.bc_state_fns,
            &mut self.bc_order,
        );
        let action = infallible_action(action);
        self.bc_fns.insert(0, action.clone());
        self.bc_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_ac(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.ac,
            &self.ac_fns,
            &self.ac_state_fns,
            &mut self.ac_order,
        );
        let action = infallible_action(action);
        self.ac_fns.push(action.clone());
        self.ac_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_ac(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(
            &self.ac,
            &self.ac_fns,
            &self.ac_state_fns,
            &mut self.ac_order,
        );
        let action = infallible_action(action);
        self.ac_fns.insert(0, action.clone());
        self.ac_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_bo_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(
            &self.bo,
            &self.bo_fns,
            &self.bo_state_fns,
            &mut self.bo_order,
        );
        self.bo_fns.push(action.clone());
        self.bo_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_ao_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(
            &self.ao,
            &self.ao_fns,
            &self.ao_state_fns,
            &mut self.ao_order,
        );
        self.ao_fns.push(action.clone());
        self.ao_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_bc_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(
            &self.bc,
            &self.bc_fns,
            &self.bc_state_fns,
            &mut self.bc_order,
        );
        self.bc_fns.push(action.clone());
        self.bc_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_ac_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(
            &self.ac,
            &self.ac_fns,
            &self.ac_state_fns,
            &mut self.ac_order,
        );
        self.ac_fns.push(action.clone());
        self.ac_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_bo_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.bo,
            &self.bo_fns,
            &self.bo_state_fns,
            &mut self.bo_order,
        );
        self.bo_state_fns.push(action.clone());
        self.bo_order.push(ActionBinding::State(action));
        self
    }

    pub fn prepend_bo_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.bo,
            &self.bo_fns,
            &self.bo_state_fns,
            &mut self.bo_order,
        );
        self.bo_state_fns.insert(0, action.clone());
        self.bo_order.insert(0, ActionBinding::State(action));
        self
    }

    pub fn add_ao_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.ao,
            &self.ao_fns,
            &self.ao_state_fns,
            &mut self.ao_order,
        );
        self.ao_state_fns.push(action.clone());
        self.ao_order.push(ActionBinding::State(action));
        self
    }

    pub fn prepend_ao_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.ao,
            &self.ao_fns,
            &self.ao_state_fns,
            &mut self.ao_order,
        );
        self.ao_state_fns.insert(0, action.clone());
        self.ao_order.insert(0, ActionBinding::State(action));
        self
    }

    pub fn add_bc_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.bc,
            &self.bc_fns,
            &self.bc_state_fns,
            &mut self.bc_order,
        );
        self.bc_state_fns.push(action.clone());
        self.bc_order.push(ActionBinding::State(action));
        self
    }

    pub fn prepend_bc_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.bc,
            &self.bc_fns,
            &self.bc_state_fns,
            &mut self.bc_order,
        );
        self.bc_state_fns.insert(0, action.clone());
        self.bc_order.insert(0, ActionBinding::State(action));
        self
    }

    pub fn add_ac_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.ac,
            &self.ac_fns,
            &self.ac_state_fns,
            &mut self.ac_order,
        );
        self.ac_state_fns.push(action.clone());
        self.ac_order.push(ActionBinding::State(action));
        self
    }

    pub fn prepend_ac_with_state(&mut self, action: StateAction) -> &mut Self {
        prepare_order(
            &self.ac,
            &self.ac_fns,
            &self.ac_state_fns,
            &mut self.ac_order,
        );
        self.ac_state_fns.insert(0, action.clone());
        self.ac_order.insert(0, ActionBinding::State(action));
        self
    }

    pub fn add_bo_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.bo,
            &mut self.bo_fns,
            &mut self.bo_state_fns,
            &mut self.bo_order,
            action.into(),
            false,
        );
        self
    }

    pub fn prepend_bo_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.bo,
            &mut self.bo_fns,
            &mut self.bo_state_fns,
            &mut self.bo_order,
            action.into(),
            true,
        );
        self
    }

    pub fn add_ao_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.ao,
            &mut self.ao_fns,
            &mut self.ao_state_fns,
            &mut self.ao_order,
            action.into(),
            false,
        );
        self
    }

    pub fn prepend_ao_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.ao,
            &mut self.ao_fns,
            &mut self.ao_state_fns,
            &mut self.ao_order,
            action.into(),
            true,
        );
        self
    }

    pub fn add_bc_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.bc,
            &mut self.bc_fns,
            &mut self.bc_state_fns,
            &mut self.bc_order,
            action.into(),
            false,
        );
        self
    }

    pub fn prepend_bc_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.bc,
            &mut self.bc_fns,
            &mut self.bc_state_fns,
            &mut self.bc_order,
            action.into(),
            true,
        );
        self
    }

    pub fn add_ac_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.ac,
            &mut self.ac_fns,
            &mut self.ac_state_fns,
            &mut self.ac_order,
            action.into(),
            false,
        );
        self
    }

    pub fn prepend_ac_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.ac,
            &mut self.ac_fns,
            &mut self.ac_state_fns,
            &mut self.ac_order,
            action.into(),
            true,
        );
        self
    }

    /// Clear named and imperative lifecycle actions. An empty phase slice
    /// clears all four; otherwise accepted phase names are `bo`, `ao`, `bc`,
    /// and `ac`.
    pub fn clear_actions(&mut self, phases: &[&str]) -> &mut Self {
        let clear_all = phases.is_empty();
        for phase in ["bo", "ao", "bc", "ac"] {
            if clear_all || phases.contains(&phase) {
                match phase {
                    "bo" => {
                        self.bo.clear();
                        self.bo_fns.clear();
                        self.bo_state_fns.clear();
                        self.bo_order.clear();
                    }
                    "ao" => {
                        self.ao.clear();
                        self.ao_fns.clear();
                        self.ao_state_fns.clear();
                        self.ao_order.clear();
                    }
                    "bc" => {
                        self.bc.clear();
                        self.bc_fns.clear();
                        self.bc_state_fns.clear();
                        self.bc_order.clear();
                    }
                    "ac" => {
                        self.ac.clear();
                        self.ac_fns.clear();
                        self.ac_state_fns.clear();
                        self.ac_order.clear();
                    }
                    _ => unreachable!(),
                }
            }
        }
        self
    }
}

fn infallible_action(
    action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
) -> ContextAction {
    Arc::new(move |rule, context| {
        action(rule, context);
        Ok::<(), ActionError>(())
    })
}

impl AltSpec {
    /// Append an imperative Rust action to this alternate.
    pub fn add_action(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_alt_order(
            &self.a,
            &self.action_fns,
            &self.matched_action_fns,
            &mut self.action_order,
        );
        let action = infallible_action(action);
        self.action_fns.push(action.clone());
        self.action_order.push(AltActionBinding::Context(action));
        self
    }

    /// Prepend an imperative Rust action ahead of existing named or direct
    /// actions.
    pub fn prepend_action(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_alt_order(
            &self.a,
            &self.action_fns,
            &self.matched_action_fns,
            &mut self.action_order,
        );
        let action = infallible_action(action);
        self.action_fns.insert(0, action.clone());
        self.action_order
            .insert(0, AltActionBinding::Context(action));
        self
    }

    /// Append a fallible imperative Rust action to this alternate.
    pub fn add_action_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_alt_order(
            &self.a,
            &self.action_fns,
            &self.matched_action_fns,
            &mut self.action_order,
        );
        self.action_fns.push(action.clone());
        self.action_order.push(AltActionBinding::Context(action));
        self
    }

    pub fn prepend_action_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_alt_order(
            &self.a,
            &self.action_fns,
            &self.matched_action_fns,
            &mut self.action_order,
        );
        self.action_fns.insert(0, action.clone());
        self.action_order
            .insert(0, AltActionBinding::Context(action));
        self
    }

    /// Append an action with the complete matched-alternate argument.
    pub fn add_action_with_match(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context, &AltMatch) -> Option<Token> + Send + Sync + 'static,
    ) -> &mut Self {
        self.add_action_with_match_result(Arc::new(move |rule, context, matched| {
            Ok(action(rule, context, matched))
        }))
    }

    pub fn prepend_action_with_match(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context, &AltMatch) -> Option<Token> + Send + Sync + 'static,
    ) -> &mut Self {
        self.prepend_action_with_match_result(Arc::new(move |rule, context, matched| {
            Ok(action(rule, context, matched))
        }))
    }

    pub fn add_action_with_match_result(&mut self, action: AltAction) -> &mut Self {
        prepare_alt_order(
            &self.a,
            &self.action_fns,
            &self.matched_action_fns,
            &mut self.action_order,
        );
        self.matched_action_fns.push(action.clone());
        self.action_order.push(AltActionBinding::Matched(action));
        self
    }

    pub fn prepend_action_with_match_result(&mut self, action: AltAction) -> &mut Self {
        prepare_alt_order(
            &self.a,
            &self.action_fns,
            &self.matched_action_fns,
            &mut self.action_order,
        );
        self.matched_action_fns.insert(0, action.clone());
        self.action_order
            .insert(0, AltActionBinding::Matched(action));
        self
    }

    pub fn add_action_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_alt_named(
            &mut self.a,
            &mut self.action_fns,
            &mut self.matched_action_fns,
            &mut self.action_order,
            action.into(),
            false,
        );
        self
    }

    pub fn prepend_action_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_alt_named(
            &mut self.a,
            &mut self.action_fns,
            &mut self.matched_action_fns,
            &mut self.action_order,
            action.into(),
            true,
        );
        self
    }
}

fn alt_order_matches(
    named: &[String],
    callbacks: &[ContextAction],
    matched: &[AltAction],
    order: &[AltActionBinding],
) -> bool {
    if order.len() != named.len() + callbacks.len() + matched.len() {
        return false;
    }
    // One pass. Written as three filtered views compared against their
    // lists, this walked `order` five times over -- once per view plus a
    // second walk of two of them to count -- and the parse loop asks this
    // question once per rule step. Taking the next expected entry from
    // whichever list a binding names says the same thing in one walk.
    let (mut next_name, mut next_callback, mut next_matched) = (0, 0, 0);
    for binding in order {
        match binding {
            AltActionBinding::Named(name) => {
                if named.get(next_name) != Some(name) {
                    return false;
                }
                next_name += 1;
            }
            AltActionBinding::Context(callback) => {
                if !callbacks
                    .get(next_callback)
                    .is_some_and(|expected| Arc::ptr_eq(expected, callback))
                {
                    return false;
                }
                next_callback += 1;
            }
            AltActionBinding::Matched(callback) => {
                if !matched
                    .get(next_matched)
                    .is_some_and(|expected| Arc::ptr_eq(expected, callback))
                {
                    return false;
                }
                next_matched += 1;
            }
        }
    }
    next_name == named.len() && next_callback == callbacks.len() && next_matched == matched.len()
}

fn prepare_alt_order(
    named: &[String],
    callbacks: &[ContextAction],
    matched: &[AltAction],
    order: &mut Vec<AltActionBinding>,
) {
    if !alt_order_matches(named, callbacks, matched, order) {
        *order = named
            .iter()
            .cloned()
            .map(AltActionBinding::Named)
            .chain(callbacks.iter().cloned().map(AltActionBinding::Context))
            .chain(matched.iter().cloned().map(AltActionBinding::Matched))
            .collect();
    }
}

fn add_alt_named(
    named: &mut Vec<String>,
    callbacks: &mut Vec<ContextAction>,
    matched: &mut Vec<AltAction>,
    order: &mut Vec<AltActionBinding>,
    action: String,
    prepend: bool,
) {
    prepare_alt_order(named, callbacks, matched, order);
    if prepend {
        named.insert(0, action.clone());
        order.insert(0, AltActionBinding::Named(action));
    } else {
        named.push(action.clone());
        order.push(AltActionBinding::Named(action));
    }
}

pub(crate) fn resolved_alt_action_order(
    named: &[String],
    callbacks: &[ContextAction],
    matched: &[AltAction],
    order: &[AltActionBinding],
) -> Vec<AltActionBinding> {
    if alt_order_matches(named, callbacks, matched, order) {
        order.to_vec()
    } else {
        named
            .iter()
            .cloned()
            .map(AltActionBinding::Named)
            .chain(callbacks.iter().cloned().map(AltActionBinding::Context))
            .chain(matched.iter().cloned().map(AltActionBinding::Matched))
            .collect()
    }
}

fn order_matches(
    named: &[String],
    callbacks: &[ContextAction],
    states: &[StateAction],
    order: &[ActionBinding],
) -> bool {
    if order.len() != named.len() + callbacks.len() + states.len() {
        return false;
    }
    // One pass, for the reason given on `alt_order_matches`.
    let (mut next_name, mut next_callback, mut next_state) = (0, 0, 0);
    for binding in order {
        match binding {
            ActionBinding::Named(name) => {
                if named.get(next_name) != Some(name) {
                    return false;
                }
                next_name += 1;
            }
            ActionBinding::Callback(callback) => {
                if !callbacks
                    .get(next_callback)
                    .is_some_and(|expected| Arc::ptr_eq(expected, callback))
                {
                    return false;
                }
                next_callback += 1;
            }
            ActionBinding::State(callback) => {
                if !states
                    .get(next_state)
                    .is_some_and(|expected| Arc::ptr_eq(expected, callback))
                {
                    return false;
                }
                next_state += 1;
            }
        }
    }
    next_name == named.len() && next_callback == callbacks.len() && next_state == states.len()
}

fn prepare_order(
    named: &[String],
    callbacks: &[ContextAction],
    states: &[StateAction],
    order: &mut Vec<ActionBinding>,
) {
    if !order_matches(named, callbacks, states, order) {
        *order = named
            .iter()
            .cloned()
            .map(ActionBinding::Named)
            .chain(callbacks.iter().cloned().map(ActionBinding::Callback))
            .chain(states.iter().cloned().map(ActionBinding::State))
            .collect();
    }
}

fn add_named(
    named: &mut Vec<String>,
    callbacks: &mut Vec<ContextAction>,
    states: &mut Vec<StateAction>,
    order: &mut Vec<ActionBinding>,
    action: String,
    prepend: bool,
) {
    prepare_order(named, callbacks, states, order);
    if prepend {
        named.insert(0, action.clone());
        order.insert(0, ActionBinding::Named(action));
    } else {
        named.push(action.clone());
        order.push(ActionBinding::Named(action));
    }
}

pub(crate) fn resolved_action_order(
    named: &[String],
    callbacks: &[ContextAction],
    states: &[StateAction],
    order: &[ActionBinding],
) -> Vec<ActionBinding> {
    if order_matches(named, callbacks, states, order) {
        order.to_vec()
    } else {
        named
            .iter()
            .cloned()
            .map(ActionBinding::Named)
            .chain(callbacks.iter().cloned().map(ActionBinding::Callback))
            .chain(states.iter().cloned().map(ActionBinding::State))
            .collect()
    }
}

/// A rule's name.
///
/// Rules are pushed and popped for every construct in a parse, so the
/// name is shared between a rule, its snapshots and whatever the next
/// rule records, rather than being copied at each step. It still
/// behaves like the `String` it replaced: compare it with a literal,
/// print it, or take a `&str` from it.
#[derive(Clone, PartialEq, Eq, Hash, PartialOrd, Ord)]
pub struct RuleName(Arc<str>);

impl RuleName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl std::ops::Deref for RuleName {
    type Target = str;

    fn deref(&self) -> &str {
        &self.0
    }
}

impl AsRef<str> for RuleName {
    fn as_ref(&self) -> &str {
        &self.0
    }
}

/// Keyed lookups borrow the name as a `str`, so `Hash` and `Eq` have to
/// agree with `str`'s. Both reach `str` through the `Arc`, so they do.
impl std::borrow::Borrow<str> for RuleName {
    fn borrow(&self) -> &str {
        &self.0
    }
}

/// Printed as the bare name, so a `{:?}` of a rule or a snapshot reads
/// the way it did when this was a `String`.
impl fmt::Debug for RuleName {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        fmt::Debug::fmt(&*self.0, f)
    }
}

impl fmt::Display for RuleName {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl PartialEq<str> for RuleName {
    fn eq(&self, other: &str) -> bool {
        &*self.0 == other
    }
}

impl PartialEq<&str> for RuleName {
    fn eq(&self, other: &&str) -> bool {
        &*self.0 == *other
    }
}

impl PartialEq<String> for RuleName {
    fn eq(&self, other: &String) -> bool {
        &*self.0 == other.as_str()
    }
}

impl PartialEq<RuleName> for str {
    fn eq(&self, other: &RuleName) -> bool {
        self == &*other.0
    }
}

impl PartialEq<RuleName> for &str {
    fn eq(&self, other: &RuleName) -> bool {
        *self == &*other.0
    }
}

impl PartialEq<RuleName> for String {
    fn eq(&self, other: &RuleName) -> bool {
        self.as_str() == &*other.0
    }
}

impl From<&str> for RuleName {
    fn from(name: &str) -> Self {
        RuleName(Arc::from(name))
    }
}

impl From<String> for RuleName {
    fn from(name: String) -> Self {
        RuleName(Arc::from(name.as_str()))
    }
}

impl From<&String> for RuleName {
    fn from(name: &String) -> Self {
        RuleName(Arc::from(name.as_str()))
    }
}

impl From<Arc<str>> for RuleName {
    fn from(name: Arc<str>) -> Self {
        RuleName(name)
    }
}

impl From<RuleName> for String {
    fn from(name: RuleName) -> Self {
        name.0.to_string()
    }
}

/// A rule as the parse loop sees it.
///
/// Its state lives behind an `Rc` that it shares with every snapshot
/// taken of it, so `snapshot()` is a pointer copy. The copy happens
/// instead on the next write, and only while a snapshot is still
/// holding the current value — so a rule that is snapshotted several
/// times between writes pays for one copy, not several, and a rule
/// nobody snapshotted pays for none. Reads and writes both go through
/// `Deref`, so `rule.state` and `rule.state = ..` are unchanged at
/// every call site.
#[derive(Clone)]
pub struct Rule {
    shared: Rc<RuleSnapshot>,
    /// Not shared with snapshots, which do not carry it.
    pub parent_node: Option<Rc<RefCell<Value>>>,
    /// The completed child's node, likewise not shared. A `Value` is 72
    /// bytes, which was a third of a `RuleSnapshot`, and copy-on-write
    /// copied all of it on the next write to any field. Nothing reads
    /// this through a snapshot: every use in the engine and in the
    /// plugin repos is `rule.child_node` on a live rule.
    pub child_node: Value,
    pub(crate) skip_befores: bool,
    pub(crate) child_node_is_self: bool,
    /// Where this rule's prepared state sits in the parser's table, or
    /// `usize::MAX` for a rule the parser did not bind.
    ///
    /// A plain `usize` rather than a handle on the prepared record itself:
    /// the parse loop clones a `Rule` once per close and snapshots it
    /// several times per step, and an `Arc` here would make every one of
    /// those a pair of atomics. The table is reached through the parser's
    /// `&self` instead, and this only says where to look. It is a hint,
    /// not a fact -- a callback can write `spec` or `name` out from under
    /// it -- so the loop checks the record it finds against both before
    /// trusting it.
    pub(crate) slot: usize,
}

impl Drop for RuleSnapshot {
    /// Unlink iteratively, because the derived drop recurses and the links
    /// below form a chain as long as the input.
    ///
    /// `parent_rule`, `child_rule`, `prev_rule` and `next_rule` each own an
    /// `Rc<RuleSnapshot>`, so the generated glue walks a chain with the call
    /// stack: `drop_in_place<RuleSnapshot>` calls `Rc::drop_slow` calls
    /// `drop_in_place<RuleSnapshot>` again, one frame per link. A grammar
    /// that pushes or replaces a rule per input element builds one link per
    /// element, so a flat JSON array of 150,000 numbers -- nesting depth
    /// ONE, nothing recursive about the document -- overflowed the default
    /// 8 MiB main-thread stack and aborted the process.
    ///
    /// Fat LTO makes it worse rather than better: inlining the cycle into
    /// itself multiplies the per-link frame, so a default release build
    /// survived an input that a build in the configuration rs/README.md
    /// documents for shipping did not. That is the wrong way round, and it
    /// is why this is a `Drop` impl rather than advice about stack size.
    ///
    /// The scratch is a local rather than a thread-local. A thread-local
    /// is faster and was wrong twice over: its key can be destroyed before
    /// another thread-local holding a snapshot is, and touching a
    /// destroyed key panics from inside a destructor; and re-entering this
    /// function while its `RefCell` is borrowed panics too. A local is
    /// immune to both, and the early return below means most drops never
    /// reach it.
    fn drop(&mut self) {
        // The overwhelmingly common case, and the one on the parse loop's
        // hot path: nothing is linked, so there is nothing to walk. Four
        // loads and a branch, no scratch, no allocation.
        if self.parent_rule.is_none()
            && self.child_rule.is_none()
            && self.prev_rule.is_none()
            && self.next_rule.is_none()
        {
            return;
        }

        /// Move a snapshot's links out, preferring the single-slot
        /// `cursor` so that a pure chain -- one link per snapshot, which
        /// is what a rule replaced once per input element builds -- is
        /// walked without allocating anything at all. `pending` is only
        /// reached for a snapshot holding more than one link, and a `Vec`
        /// that is never pushed to never allocates.
        fn unlink(
            snapshot: &mut RuleSnapshot,
            cursor: &mut Option<Rc<RuleSnapshot>>,
            pending: &mut Vec<Rc<RuleSnapshot>>,
        ) {
            let mut hold = |held: Option<Rc<RuleSnapshot>>| {
                if let Some(held) = held {
                    if cursor.is_none() {
                        *cursor = Some(held);
                    } else {
                        pending.push(held);
                    }
                }
            };
            hold(snapshot.parent_rule.take());
            hold(snapshot.child_rule.take());
            hold(snapshot.prev_rule.take());
            hold(snapshot.next_rule.take());
        }

        let mut cursor: Option<Rc<RuleSnapshot>> = None;
        let mut pending: Vec<Rc<RuleSnapshot>> = Vec::new();
        unlink(self, &mut cursor, &mut pending);
        while let Some(mut link) = cursor.take().or_else(|| pending.pop()) {
            if Rc::weak_count(&link) == 0 {
                // No weak observers, so `get_mut` answers "am I the last
                // handle" without moving anything. `RuleSnapshot` is a
                // twenty-field struct carrying eight reference-counted
                // handles, and this is the path the parse loop takes.
                if let Some(owned) = Rc::get_mut(&mut link) {
                    unlink(owned, &mut cursor, &mut pending);
                }
            } else if let Ok(mut owned) = Rc::try_unwrap(link) {
                // `Rule::snapshot` is public and `Context` hands out
                // `Rc<RuleSnapshot>`, so an embedder can hold a `Weak` to
                // one. `get_mut` refuses while any weak observer exists,
                // even when we ARE the last strong owner and dropping will
                // run the destructor; `try_unwrap` is the one that
                // distinguishes those. Getting this wrong left the links
                // in place and recursed after all.
                unlink(&mut owned, &mut cursor, &mut pending);
            }
            // Anything reached here drops with its links already taken, so
            // its own `drop` returns at the check above.
        }
    }
}

impl std::ops::Deref for Rule {
    type Target = RuleSnapshot;

    fn deref(&self) -> &RuleSnapshot {
        &self.shared
    }
}

/// Every write to a rule's shared state goes through here, which is
/// what makes the copy happen on write rather than on snapshot.
///
/// The obvious next step — stop copying at all, so a snapshot aliases
/// the live rule the way TypeScript's and Go's rule handles do — was
/// measured here by making this return an aliasing pointer. It is
/// faster where it works: a 512-character palindrome drops from 31.7M
/// to 24.6M instructions, and a 16K-term adder from 72.7 ms to 57.8 ms.
/// But `next_rule` and the parent and child links then point at rules
/// that point back, and an `Rc` cycle is never freed: peak memory for
/// the benchmark set goes from 71 MB to 1214 MB, and a 32K-character
/// palindrome slows from 111 ms to 270 ms once the leak outweighs the
/// saving. Doing it properly needs `Weak` on every back-reference and
/// an upgrade on every traversal, which spends some of the same 1.3x
/// it is chasing. Worth knowing before anyone tries it again.
impl std::ops::DerefMut for Rule {
    fn deref_mut(&mut self) -> &mut RuleSnapshot {
        Rc::make_mut(&mut self.shared)
    }
}

#[derive(Debug, Clone)]
pub struct RuleSnapshot {
    pub i: usize,
    pub d: usize,
    pub name: RuleName,
    pub spec: Arc<RuleSpec>,
    pub state: RuleState,
    pub bo: bool,
    pub ao: bool,
    pub bc: bool,
    pub ac: bool,
    pub need: i32,
    pub node: Rc<RefCell<Value>>,
    pub parent_rule: Option<Rc<RuleSnapshot>>,
    pub child_rule: Option<Rc<RuleSnapshot>>,
    pub prev_rule: Option<Rc<RuleSnapshot>>,
    pub next_rule: Option<Rc<RuleSnapshot>>,
    pub next_rule_name: Option<RuleName>,
    pub n: Rc<HashMap<String, i32>>,
    pub u: Rc<HashMap<String, Value>>,
    pub k: Rc<HashMap<String, Value>>,
    /// Matched open and close tokens. Shared rather than owned: the parse
    /// loop only ever replaces these wholesale, and a snapshot that copied
    /// them copied every `Token`'s name and source text with them.
    pub o: Rc<Vec<Token>>,
    pub c: Rc<Vec<Token>>,
}

/// One shared empty map per thread, so a rule that never writes to `n`,
/// `u` or `k` costs no allocation for them. `Rc::make_mut` copies on the
/// first write, which for an empty map is close to free.
fn empty_counters() -> Rc<HashMap<String, i32>> {
    thread_local! {
        static EMPTY: Rc<HashMap<String, i32>> = Rc::new(HashMap::new());
    }
    EMPTY.with(Rc::clone)
}

fn empty_values() -> Rc<HashMap<String, Value>> {
    thread_local! {
        static EMPTY: Rc<HashMap<String, Value>> = Rc::new(HashMap::new());
    }
    EMPTY.with(Rc::clone)
}

/// The matched-token lists start empty and are replaced wholesale when an
/// alternate matches, so every rule created allocated two `Rc` boxes for two
/// vectors that never grew. Shared like the counter and value bags above.
fn empty_tokens() -> Rc<Vec<Token>> {
    thread_local! {
        static EMPTY: Rc<Vec<Token>> = Rc::new(Vec::new());
    }
    EMPTY.with(Rc::clone)
}

impl Rule {
    /// Mutable access to the per-rule counters and state. Copies only when
    /// a snapshot is still holding the current value.
    pub fn n_mut(&mut self) -> &mut HashMap<String, i32> {
        Rc::make_mut(&mut self.n)
    }

    pub fn u_mut(&mut self) -> &mut HashMap<String, Value> {
        Rc::make_mut(&mut self.u)
    }

    pub fn k_mut(&mut self) -> &mut HashMap<String, Value> {
        Rc::make_mut(&mut self.k)
    }

    pub fn new(name: impl Into<RuleName>, initial_node: Value) -> Self {
        let name: RuleName = name.into();
        let spec = Arc::new(RuleSpec::new(name.as_str()));
        Rule {
            shared: Rc::new(RuleSnapshot {
                i: 0,
                d: 0,
                name,
                spec,
                state: RuleState::Open,
                bo: true,
                ao: true,
                bc: true,
                ac: true,
                need: 0,
                node: Rc::new(RefCell::new(initial_node)),
                parent_rule: None,
                child_rule: None,
                prev_rule: None,
                next_rule: None,
                next_rule_name: None,
                n: empty_counters(),
                u: empty_values(),
                k: empty_values(),
                o: empty_tokens(),
                c: empty_tokens(),
            }),
            parent_node: None,
            child_node: Value::Undefined,
            skip_befores: false,
            child_node_is_self: false,
            slot: usize::MAX,
        }
    }

    pub fn with_shared_node(name: impl Into<RuleName>, node: Rc<RefCell<Value>>) -> Self {
        Self::bound(name.into(), node, None, usize::MAX)
    }

    /// Build a rule already bound to its installed spec.
    ///
    /// An unbound rule reports its own name through `spec.name`, so building
    /// one with no spec has to invent a placeholder `RuleSpec` carrying that
    /// name: a `String` and an `Arc` box. Every push and replace in the parse
    /// loop then bound the installed spec straight over the placeholder, so
    /// both were allocated and freed once per rule step for nothing. The
    /// placeholder is still built for a name that names no installed rule,
    /// which is the case it exists for.
    pub(crate) fn bound(
        name: RuleName,
        node: Rc<RefCell<Value>>,
        installed: Option<&Arc<RuleSpec>>,
        slot: usize,
    ) -> Self {
        let spec = match installed {
            Some(spec) => Arc::clone(spec),
            None => Arc::new(RuleSpec::new(name.as_str())),
        };
        Rule {
            shared: Rc::new(RuleSnapshot {
                i: 0,
                d: 0,
                name,
                spec,
                state: RuleState::Open,
                bo: true,
                ao: true,
                bc: true,
                ac: true,
                need: 0,
                node,
                parent_rule: None,
                child_rule: None,
                prev_rule: None,
                next_rule: None,
                next_rule_name: None,
                n: empty_counters(),
                u: empty_values(),
                k: empty_values(),
                o: empty_tokens(),
                c: empty_tokens(),
            }),
            parent_node: None,
            child_node: Value::Undefined,
            skip_befores: false,
            child_node_is_self: false,
            slot,
        }
    }

    /// `name` arrives already shared: the parser interns one handle per
    /// installed rule, so binding copies a pointer rather than the text.
    ///
    /// `slot` is the rule's position in the parser's prepared table, which
    /// the same lookup that found the spec already returned.
    pub(crate) fn bind_spec(&mut self, spec: &Arc<RuleSpec>, name: RuleName, slot: usize) {
        self.name = name;
        self.spec = Arc::clone(spec);
        self.slot = slot;
        // Rust RuleSpec lifecycle lists are always present (possibly empty),
        // matching the canonical normalized definition's non-null defaults.
        self.bo = true;
        self.ao = true;
        self.bc = true;
        self.ac = true;
    }

    pub fn o0(&self) -> Option<&Token> {
        self.o.first()
    }

    pub fn o1(&self) -> Option<&Token> {
        self.o.get(1)
    }

    pub fn c0(&self) -> Option<&Token> {
        self.c.first()
    }

    pub fn c1(&self) -> Option<&Token> {
        self.c.get(1)
    }

    pub fn os(&self) -> usize {
        self.o.len()
    }

    pub fn cs(&self) -> usize {
        self.c.len()
    }

    /// Resolve a matched opening token's eager or lazy semantic value without
    /// exposing the temporary token clone needed by Rust's borrow rules.
    pub fn resolve_open_value(&mut self, index: usize, context: &mut Context) -> Value {
        self.o
            .get(index)
            .cloned()
            .map_or(Value::Undefined, |token| token.resolve_val(self, context))
    }

    /// Resolve a matched closing token's eager or lazy semantic value.
    pub fn resolve_close_value(&mut self, index: usize, context: &mut Context) -> Value {
        self.c
            .get(index)
            .cloned()
            .map_or(Value::Undefined, |token| token.resolve_val(self, context))
    }

    /// Counter comparisons use zero for an unset counter, matching the
    /// canonical engine. `exist` distinguishes unset from explicitly zero.
    pub fn eq(&self, counter: &str, limit: i32) -> bool {
        self.n.get(counter).copied().unwrap_or(0) == limit
    }

    pub fn lt(&self, counter: &str, limit: i32) -> bool {
        self.n.get(counter).copied().unwrap_or(0) < limit
    }

    pub fn gt(&self, counter: &str, limit: i32) -> bool {
        self.n.get(counter).copied().unwrap_or(0) > limit
    }

    pub fn lte(&self, counter: &str, limit: i32) -> bool {
        self.n.get(counter).copied().unwrap_or(0) <= limit
    }

    pub fn gte(&self, counter: &str, limit: i32) -> bool {
        self.n.get(counter).copied().unwrap_or(0) >= limit
    }

    pub fn exist(&self, counter: &str) -> bool {
        self.n.contains_key(counter)
    }

    /// The rule's state as it stands, shared rather than copied. The
    /// copy, if one is still needed, happens on the rule's next write.
    pub fn snapshot(&self) -> Rc<RuleSnapshot> {
        Rc::clone(&self.shared)
    }

    pub(crate) fn accept_child_node(&mut self, child: &Rule) {
        self.child_node_is_self = Rc::ptr_eq(&self.node, &child.node);
        self.child_node = child.node.borrow().clone();
    }
}

impl fmt::Display for Rule {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "[Rule {}~{}]", self.name, self.i)
    }
}
