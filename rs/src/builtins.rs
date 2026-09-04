// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::rule::Rule;
use crate::value::Value;
use indexmap::IndexMap;
use std::cell::RefCell;
use std::rc::Rc;

fn config_object(config: Option<&Value>) -> Option<&IndexMap<String, Value>> {
    config.and_then(|value| match value {
        Value::Object(map) => Some(map),
        _ => None,
    })
}

fn config_bool(config: Option<&Value>, name: &str) -> bool {
    matches!(
        config_object(config).and_then(|map| map.get(name)),
        Some(Value::Bool(true))
    )
}

fn config_string(config: Option<&Value>, name: &str) -> String {
    match config_object(config).and_then(|map| map.get(name)) {
        Some(Value::String(value)) => value.clone(),
        _ => String::new(),
    }
}

fn config_usize(config: Option<&Value>, name: &str) -> usize {
    match config_object(config).and_then(|map| map.get(name)) {
        Some(Value::Number(value)) if value.is_finite() && *value >= 0.0 => *value as usize,
        _ => 0,
    }
}

fn ast_node(rule: String, kind: String) -> Value {
    let mut node = IndexMap::new();
    if kind == "user" {
        node.insert("rule".into(), Value::String(rule));
    }
    node.insert("src".into(), Value::String(String::new()));
    node.insert("kids".into(), Value::Array(Vec::new()));
    Value::Object(node)
}

fn append_src(node: &mut IndexMap<String, Value>, source: &str) {
    if let Some(Value::String(current)) = node.get_mut("src") {
        current.push_str(source);
    }
}

fn append_kid(node: &mut IndexMap<String, Value>, child: Value) {
    if let Some(Value::Array(kids)) = node.get_mut("kids") {
        kids.push(child);
    }
}

fn capture_child(node: &mut IndexMap<String, Value>, child: Value) {
    if let Value::Object(child_map) = &child {
        if let Some(Value::String(source)) = child_map.get("src") {
            append_src(node, source);
            if child_map
                .get("rule")
                .is_some_and(|value| !matches!(value, Value::String(rule) if rule.is_empty()))
            {
                append_kid(node, child);
            } else if let Some(Value::Array(children)) = child_map.get("kids") {
                for nested in children {
                    append_kid(node, nested.clone());
                }
            }
            return;
        }
    }
    append_kid(node, child);
}

pub fn run_builtin_action(name: &str, rule: &mut Rule, config: Option<&Value>) -> bool {
    match name {
        "@node$" => {
            if config_bool(config, "init") {
                rule.node = Rc::new(RefCell::new(ast_node(
                    config_string(config, "rule"),
                    config_string(config, "kind"),
                )));
            }
            let nterms = config_usize(config, "nterms");
            if let Value::Object(node) = &mut *rule.node.borrow_mut() {
                for token in rule.o.iter().take(nterms) {
                    append_src(node, &token.src);
                }
            }
        }
        "@capture$" => {
            if rule.node.borrow().is_undefined() {
                rule.node = Rc::new(RefCell::new(ast_node(
                    config_string(config, "rule"),
                    config_string(config, "kind"),
                )));
            }
            if !rule.child_node.is_undefined() {
                if let Value::Object(node) = &mut *rule.node.borrow_mut() {
                    capture_child(node, rule.child_node.clone());
                }
            }
        }
        "@bubble$" => {
            if !rule.child_node.is_undefined() {
                rule.node = Rc::new(RefCell::new(rule.child_node.clone()));
            }
        }
        "@fold$" => {
            if let Some(parent_node) = &rule.parent_node {
                let same_node = Rc::ptr_eq(parent_node, &rule.node);
                let own = rule.node.borrow().clone();
                if let Value::Object(parent) = &mut *parent_node.borrow_mut() {
                    if !same_node {
                        capture_child(parent, own);
                    }
                    let close_count = config_usize(config, "cN");
                    for token in rule.c.iter().take(close_count) {
                        append_src(parent, &token.src);
                    }
                }
            }
            rule.node = Rc::new(RefCell::new(Value::Undefined));
        }
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
            if !rule.child_node.is_undefined() {
                rule.node = Rc::new(RefCell::new(rule.child_node.clone()));
            } else if let Some(token) = rule.o.get(config_usize(config, "from")) {
                rule.node = Rc::new(RefCell::new(token.val.clone()));
            }
        }
        "@object$" => {
            rule.node = Rc::new(RefCell::new(Value::Object(IndexMap::new())));
        }
        "@array$" => {
            rule.node = Rc::new(RefCell::new(Value::Array(Vec::new())));
        }
        "@reset$" => {
            rule.node = Rc::new(RefCell::new(Value::Undefined));
            rule.child_node = Value::Undefined;
        }
        "@key$" => {
            if let Some(token) = rule.o.get(config_usize(config, "from")) {
                let slot = {
                    let configured = config_string(config, "slot");
                    if configured.is_empty() {
                        "key".to_string()
                    } else {
                        configured
                    }
                };
                rule.u.insert(slot, token.val.clone());
            }
        }
        "@setval$" => {
            let slot = {
                let configured = config_string(config, "slot");
                if configured.is_empty() {
                    "key".to_string()
                } else {
                    configured
                }
            };
            if let Some(Value::String(key)) = rule.u.get(&slot).cloned() {
                if let Value::Object(map) = &mut *rule.node.borrow_mut() {
                    map.insert(key, rule.child_node.clone());
                }
            }
        }
        "@push$" => {
            if !rule.child_node.is_undefined() {
                if let Value::Array(array) = &mut *rule.node.borrow_mut() {
                    array.push(rule.child_node.clone());
                }
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
