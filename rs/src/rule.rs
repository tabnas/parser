// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, Token};
use crate::value::Value;
use crate::Lexer;
use crate::{ActionError, Context, ContextAction};
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;
use std::sync::Arc;

/// Function-valued alternate condition. The candidate's matched tokens have
/// already been copied onto `rule` when this callback runs.
pub type AltCondition = Arc<dyn Fn(&mut Rule, &mut Context) -> bool + Send + Sync>;

/// Condition with access to the live lexer. This is the imperative form used
/// by plugins that perform controlled lookahead from a condition.
pub type AltConditionWithLexer =
    Arc<dyn for<'source> Fn(&mut Rule, &mut Context, &mut Lexer<'source>) -> bool + Send + Sync>;

/// Function-valued push/replace route.
pub type AltNext = Arc<dyn Fn(&mut Rule, &mut Context) -> Option<String> + Send + Sync>;

/// Function-valued token backtrack count.
pub type AltBack = Arc<dyn Fn(&mut Rule, &mut Context) -> usize + Send + Sync>;

/// Function-valued alternate error. Returning a token rejects the alternate
/// at its match site and uses the token's `err`/`why` field as the error code.
pub type AltError = Arc<dyn Fn(&mut Rule, &mut Context) -> Option<Token> + Send + Sync>;

/// Post-match alternate modifier. It takes and returns the effective match so
/// replacement is explicit rather than mutating shared grammar state.
pub type AltModifier = Arc<dyn Fn(AltSpec, &mut Rule, &mut Context) -> AltSpec + Send + Sync>;

/// One executable action in its exact declaration position. The mature
/// engines allow named function references and direct callbacks to be mixed;
/// retaining this sequence avoids losing prepend/append order while keeping
/// the serialized `a`/`bo`/`ao`/`bc`/`ac` lists available for inspection.
#[derive(Clone)]
pub enum ActionBinding {
    Named(String),
    Callback(ContextAction),
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
    pub r: Option<String>,
    pub r_fn: Option<AltNext>,
    pub b: usize,
    pub b_fn: Option<AltBack>,
    pub a: Vec<String>,
    /// Imperative actions installed directly by a native Rust plugin.
    /// Named actions in `a` remain the serialized grammar representation;
    /// `action_order` retains their exact interleaving with direct callbacks.
    pub action_fns: Vec<ContextAction>,
    pub action_order: Vec<ActionBinding>,
    pub action_configs: HashMap<String, Value>,
    pub c: Vec<Condition>,
    pub c_ref: Option<String>,
    pub c_fn: Option<AltCondition>,
    pub c_lex: Option<AltConditionWithLexer>,
    pub n: HashMap<String, i32>,
    pub u: HashMap<String, Value>,
    pub k: HashMap<String, Value>,
    pub g: String,
    pub h: Option<AltModifier>,
    pub e: Option<AltError>,
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
    pub bo_order: Vec<ActionBinding>,
    pub ao_order: Vec<ActionBinding>,
    pub bc_order: Vec<ActionBinding>,
    pub ac_order: Vec<ActionBinding>,
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
        prepare_order(&self.bo, &self.bo_fns, &mut self.bo_order);
        let action = infallible_action(action);
        self.bo_fns.push(action.clone());
        self.bo_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_bo(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.bo, &self.bo_fns, &mut self.bo_order);
        let action = infallible_action(action);
        self.bo_fns.insert(0, action.clone());
        self.bo_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_ao(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.ao, &self.ao_fns, &mut self.ao_order);
        let action = infallible_action(action);
        self.ao_fns.push(action.clone());
        self.ao_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_ao(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.ao, &self.ao_fns, &mut self.ao_order);
        let action = infallible_action(action);
        self.ao_fns.insert(0, action.clone());
        self.ao_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_bc(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.bc, &self.bc_fns, &mut self.bc_order);
        let action = infallible_action(action);
        self.bc_fns.push(action.clone());
        self.bc_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_bc(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.bc, &self.bc_fns, &mut self.bc_order);
        let action = infallible_action(action);
        self.bc_fns.insert(0, action.clone());
        self.bc_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_ac(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.ac, &self.ac_fns, &mut self.ac_order);
        let action = infallible_action(action);
        self.ac_fns.push(action.clone());
        self.ac_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_ac(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.ac, &self.ac_fns, &mut self.ac_order);
        let action = infallible_action(action);
        self.ac_fns.insert(0, action.clone());
        self.ac_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_bo_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(&self.bo, &self.bo_fns, &mut self.bo_order);
        self.bo_fns.push(action.clone());
        self.bo_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_ao_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(&self.ao, &self.ao_fns, &mut self.ao_order);
        self.ao_fns.push(action.clone());
        self.ao_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_bc_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(&self.bc, &self.bc_fns, &mut self.bc_order);
        self.bc_fns.push(action.clone());
        self.bc_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_ac_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(&self.ac, &self.ac_fns, &mut self.ac_order);
        self.ac_fns.push(action.clone());
        self.ac_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn add_bo_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.bo,
            &mut self.bo_fns,
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
                        self.bo_order.clear();
                    }
                    "ao" => {
                        self.ao.clear();
                        self.ao_fns.clear();
                        self.ao_order.clear();
                    }
                    "bc" => {
                        self.bc.clear();
                        self.bc_fns.clear();
                        self.bc_order.clear();
                    }
                    "ac" => {
                        self.ac.clear();
                        self.ac_fns.clear();
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
        prepare_order(&self.a, &self.action_fns, &mut self.action_order);
        let action = infallible_action(action);
        self.action_fns.push(action.clone());
        self.action_order.push(ActionBinding::Callback(action));
        self
    }

    /// Prepend an imperative Rust action ahead of existing named or direct
    /// actions.
    pub fn prepend_action(
        &mut self,
        action: impl Fn(&mut Rule, &mut Context) + Send + Sync + 'static,
    ) -> &mut Self {
        prepare_order(&self.a, &self.action_fns, &mut self.action_order);
        let action = infallible_action(action);
        self.action_fns.insert(0, action.clone());
        self.action_order.insert(0, ActionBinding::Callback(action));
        self
    }

    /// Append a fallible imperative Rust action to this alternate.
    pub fn add_action_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(&self.a, &self.action_fns, &mut self.action_order);
        self.action_fns.push(action.clone());
        self.action_order.push(ActionBinding::Callback(action));
        self
    }

    pub fn prepend_action_result(&mut self, action: ContextAction) -> &mut Self {
        prepare_order(&self.a, &self.action_fns, &mut self.action_order);
        self.action_fns.insert(0, action.clone());
        self.action_order.insert(0, ActionBinding::Callback(action));
        self
    }

    pub fn add_action_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.a,
            &mut self.action_fns,
            &mut self.action_order,
            action.into(),
            false,
        );
        self
    }

    pub fn prepend_action_ref(&mut self, action: impl Into<String>) -> &mut Self {
        add_named(
            &mut self.a,
            &mut self.action_fns,
            &mut self.action_order,
            action.into(),
            true,
        );
        self
    }
}

fn order_matches(named: &[String], callbacks: &[ContextAction], order: &[ActionBinding]) -> bool {
    if order.len() != named.len() + callbacks.len() {
        return false;
    }
    let ordered_names = order.iter().filter_map(|binding| match binding {
        ActionBinding::Named(name) => Some(name),
        ActionBinding::Callback(_) => None,
    });
    let ordered_callbacks = order.iter().filter_map(|binding| match binding {
        ActionBinding::Named(_) => None,
        ActionBinding::Callback(callback) => Some(callback),
    });
    named.iter().eq(ordered_names)
        && callbacks.len() == ordered_callbacks.clone().count()
        && callbacks
            .iter()
            .zip(ordered_callbacks)
            .all(|(left, right)| Arc::ptr_eq(left, right))
}

fn prepare_order(named: &[String], callbacks: &[ContextAction], order: &mut Vec<ActionBinding>) {
    if !order_matches(named, callbacks, order) {
        *order = named
            .iter()
            .cloned()
            .map(ActionBinding::Named)
            .chain(callbacks.iter().cloned().map(ActionBinding::Callback))
            .collect();
    }
}

fn add_named(
    named: &mut Vec<String>,
    callbacks: &mut Vec<ContextAction>,
    order: &mut Vec<ActionBinding>,
    action: String,
    prepend: bool,
) {
    prepare_order(named, callbacks, order);
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
    order: &[ActionBinding],
) -> Vec<ActionBinding> {
    if order_matches(named, callbacks, order) {
        order.to_vec()
    } else {
        named
            .iter()
            .cloned()
            .map(ActionBinding::Named)
            .chain(callbacks.iter().cloned().map(ActionBinding::Callback))
            .collect()
    }
}

#[derive(Clone)]
pub struct Rule {
    pub i: usize,
    pub d: usize,
    pub name: String,
    pub state: RuleState,
    pub(crate) skip_befores: bool,
    pub need: i32,
    pub node: Rc<RefCell<Value>>,
    pub parent_node: Option<Rc<RefCell<Value>>>,
    pub child_node: Value,
    pub(crate) child_node_is_self: bool,
    pub parent_rule: Option<Rc<RuleSnapshot>>,
    pub child_rule: Option<Rc<RuleSnapshot>>,
    pub prev_rule: Option<Rc<RuleSnapshot>>,
    pub next_rule: Option<Rc<RuleSnapshot>>,
    pub next_rule_name: Option<String>,
    pub n: HashMap<String, i32>,
    pub u: HashMap<String, Value>,
    pub k: HashMap<String, Value>,
    pub o: Vec<Token>,
    pub c: Vec<Token>,
}

#[derive(Debug, Clone)]
pub struct RuleSnapshot {
    pub i: usize,
    pub d: usize,
    pub name: String,
    pub state: RuleState,
    pub need: i32,
    pub node: Rc<RefCell<Value>>,
    pub child_node: Value,
    pub parent_rule: Option<Rc<RuleSnapshot>>,
    pub child_rule: Option<Rc<RuleSnapshot>>,
    pub prev_rule: Option<Rc<RuleSnapshot>>,
    pub next_rule: Option<Rc<RuleSnapshot>>,
    pub next_rule_name: Option<String>,
    pub n: HashMap<String, i32>,
    pub u: HashMap<String, Value>,
    pub k: HashMap<String, Value>,
    pub o: Vec<Token>,
    pub c: Vec<Token>,
}

impl Rule {
    pub fn new(name: impl Into<String>, initial_node: Value) -> Self {
        Rule {
            i: 0,
            d: 0,
            name: name.into(),
            state: RuleState::Open,
            skip_befores: false,
            need: 0,
            node: Rc::new(RefCell::new(initial_node)),
            parent_node: None,
            child_node: Value::Undefined,
            child_node_is_self: false,
            parent_rule: None,
            child_rule: None,
            prev_rule: None,
            next_rule: None,
            next_rule_name: None,
            n: HashMap::new(),
            u: HashMap::new(),
            k: HashMap::new(),
            o: Vec::new(),
            c: Vec::new(),
        }
    }

    pub fn with_shared_node(name: impl Into<String>, node: Rc<RefCell<Value>>) -> Self {
        Rule {
            i: 0,
            d: 0,
            name: name.into(),
            state: RuleState::Open,
            skip_befores: false,
            need: 0,
            node,
            parent_node: None,
            child_node: Value::Undefined,
            child_node_is_self: false,
            parent_rule: None,
            child_rule: None,
            prev_rule: None,
            next_rule: None,
            next_rule_name: None,
            n: HashMap::new(),
            u: HashMap::new(),
            k: HashMap::new(),
            o: Vec::new(),
            c: Vec::new(),
        }
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

    pub fn snapshot(&self) -> Rc<RuleSnapshot> {
        Rc::new(RuleSnapshot {
            i: self.i,
            d: self.d,
            name: self.name.clone(),
            state: self.state,
            need: self.need,
            node: self.node.clone(),
            child_node: self.child_node.clone(),
            parent_rule: self.parent_rule.clone(),
            child_rule: self.child_rule.clone(),
            prev_rule: self.prev_rule.clone(),
            next_rule: self.next_rule.clone(),
            next_rule_name: self.next_rule_name.clone(),
            n: self.n.clone(),
            u: self.u.clone(),
            k: self.k.clone(),
            o: self.o.clone(),
            c: self.c.clone(),
        })
    }

    pub(crate) fn accept_child_node(&mut self, child: &Rule) {
        self.child_node_is_self = Rc::ptr_eq(&self.node, &child.node);
        self.child_node = child.node.borrow().clone();
    }
}
