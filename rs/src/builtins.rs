// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::rule::Rule;
use crate::value::Value;
use indexmap::IndexMap;
use std::cell::RefCell;
use std::rc::Rc;

pub fn run_builtin_action(name: &str, rule: &mut Rule) -> bool {
    match name {
        "@val-bo" => {
            rule.node = Rc::new(RefCell::new(Value::Undefined));
        }
        "@val-bc" => {
            let is_undef = rule.node.borrow().is_undefined();
            if is_undef {
                if !rule.child_node.is_undefined() {
                    rule.node = Rc::new(RefCell::new(rule.child_node.clone()));
                } else if rule.os() > 0 {
                    if let Some(t0) = rule.o0() {
                        rule.node = Rc::new(RefCell::new(t0.val.clone()));
                    }
                }
            }
        }
        "@value$" => {
            if let Some(token) = rule.o0() {
                rule.node = Rc::new(RefCell::new(token.val.clone()));
            } else if !rule.child_node.is_undefined() {
                rule.node = Rc::new(RefCell::new(rule.child_node.clone()));
            }
        }
        "@map-bo" => {
            rule.node = Rc::new(RefCell::new(Value::Object(IndexMap::new())));
        }
        "@list-bo" => {
            rule.node = Rc::new(RefCell::new(Value::Array(Vec::new())));
        }
        "@pairkey" => {
            if let Some(t0) = rule.o0() {
                let key = match &t0.val {
                    Value::String(s) => s.clone(),
                    _ => t0.src.clone(),
                };
                rule.u.insert("key".to_string(), Value::String(key));
            }
        }
        "@pair-bc" => {
            if let Some(Value::String(key)) = rule.u.get("key").cloned() {
                let mut node = rule.node.borrow_mut();
                if let Value::Object(ref mut map) = *node {
                    map.insert(key, rule.child_node.clone());
                }
            }
        }
        "@elem-bc" if !rule.child_node.is_undefined() => {
            let mut node = rule.node.borrow_mut();
            if let Value::Array(ref mut arr) = *node {
                arr.push(rule.child_node.clone());
            }
        }
        _ => return false,
    }
    true
}
