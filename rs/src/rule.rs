// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, Token};
use crate::value::Value;
use crate::Context;
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;
use std::sync::Arc;

/// Function-valued alternate condition. The candidate's matched tokens have
/// already been copied onto `rule` when this callback runs.
pub type AltCondition = Arc<dyn Fn(&mut Rule, &mut Context) -> bool + Send + Sync>;

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
    pub action_configs: HashMap<String, Value>,
    pub c: Vec<Condition>,
    pub c_ref: Option<String>,
    pub c_fn: Option<AltCondition>,
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
        }
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

#[derive(Clone)]
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

    pub fn c0(&self) -> Option<&Token> {
        self.c.first()
    }

    pub fn os(&self) -> usize {
        self.o.len()
    }

    pub fn cs(&self) -> usize {
        self.c.len()
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
