// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, Token};
use crate::value::Value;
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RuleState {
    Open,
    Close,
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
    pub r: Option<String>,
    pub b: usize,
    pub a: Vec<String>,
    pub c: Vec<Condition>,
    pub n: HashMap<String, i32>,
    pub u: HashMap<String, Value>,
    pub k: HashMap<String, Value>,
    pub g: String,
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
    pub node: Rc<RefCell<Value>>,
    pub child_node: Value,
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
            node: Rc::new(RefCell::new(initial_node)),
            child_node: Value::Undefined,
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
            node,
            child_node: Value::Undefined,
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
}
