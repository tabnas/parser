// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::options::InfoOptions;
use crate::rule::Rule;
use crate::token::{TIN_ST, TIN_TX};
use crate::value::{ListRef, MapRef, Text, Value};
use crate::Context;
use indexmap::IndexMap;
use std::cell::RefCell;
use std::rc::Rc;
use std::sync::Arc;

pub(crate) fn is_builtin_action(name: &str) -> bool {
    matches!(
        name,
        "@node$"
            | "@capture$"
            | "@bubble$"
            | "@fold$"
            | "@probeInit$"
            | "@probeDecide$"
            | "@object$"
            | "@array$"
            | "@reset$"
            | "@key$"
            | "@setval$"
            | "@push$"
            | "@value$"
            | "@map-bo"
            | "@list-bo"
            | "@pairkey"
            | "@pair-bc"
            | "@elem-bc"
            | "@val-bo"
            | "@val-bc"
    )
}

fn config_object(config: Option<&Value>) -> Option<&IndexMap<String, Value>> {
    config.and_then(|value| match value {
        Value::Object(map) => Some(map.as_ref()),
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

/// The config value at `name` only when it really is a string.
///
/// Distinct from `config_string`, which folds "absent" and "" into the
/// same empty String: `lit` needs them apart, because an empty string is
/// a real key. Matching the TS `typeof` guard and the Go type assertion,
/// anything that is not a string reads as absent, so the three ports
/// agree on every input and not merely on well-typed ones.
fn config_str_opt(config: Option<&Value>, name: &str) -> Option<String> {
    match config_object(config).and_then(|map| map.get(name)) {
        Some(Value::String(value)) => Some(value.clone()),
        _ => None,
    }
}

fn config_usize(config: Option<&Value>, name: &str) -> usize {
    match config_object(config).and_then(|map| map.get(name)) {
        Some(Value::Number(value)) if value.is_finite() && *value >= 0.0 => *value as usize,
        _ => 0,
    }
}

fn config_index(config: Option<&Value>, name: &str) -> Option<usize> {
    match config_object(config).and_then(|map| map.get(name)) {
        None => Some(0),
        Some(Value::Number(value))
            if value.is_finite()
                && value.fract() == 0.0
                && *value >= 0.0
                && *value <= usize::MAX as f64 =>
        {
            Some(*value as usize)
        }
        _ => None,
    }
}

fn ast_node(rule: String, kind: String) -> Value {
    let mut node = IndexMap::new();
    if kind == "user" {
        node.insert("rule".into(), Value::String(rule));
    }
    node.insert("src".into(), Value::String(String::new()));
    node.insert("kids".into(), Value::array(Vec::new()));
    Value::object(node)
}

/// The accumulated source text of a tree node -- the `{rule?, src, kids}`
/// shape `ast_node` builds -- or the node unchanged when it is not one.
///
/// "src" is how a member whose value IS its matched text gets that text:
/// the tree builders already accumulate it, and nothing else could read it
/// back out. A compiler emits "src" only where it already knows the member
/// is a scalar, so this never has to guess which it is: the fall-through
/// exists so asking for src where no tree node was built passes the value
/// along rather than erasing it.
fn src_val(node: Value) -> Value {
    if let Value::Object(map) = &node {
        if let Some(Value::String(source)) = map.get("src") {
            return Value::String(source.clone());
        }
    }
    node
}

fn append_src(node: &mut IndexMap<String, Value>, source: &str) {
    if let Some(Value::String(current)) = node.get_mut("src") {
        current.push_str(source);
    }
}

fn append_kid(node: &mut IndexMap<String, Value>, child: Value) {
    if let Some(kids) = node.get_mut("kids").and_then(Value::as_array_mut) {
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
                for nested in children.iter() {
                    append_kid(node, nested.clone());
                }
            }
            return;
        }
    }
    append_kid(node, child);
}

fn map_value(info: &InfoOptions, implicit: bool) -> Value {
    if info.map {
        Value::MapRef(Arc::new(MapRef {
            value: IndexMap::new(),
            implicit,
            meta: IndexMap::new(),
        }))
    } else {
        Value::object(IndexMap::new())
    }
}

fn list_value(info: &InfoOptions, implicit: bool) -> Value {
    if info.list {
        Value::ListRef(Arc::new(ListRef {
            value: Vec::new(),
            implicit,
            child: None,
            meta: IndexMap::new(),
        }))
    } else {
        Value::array(Vec::new())
    }
}

fn token_value(rule: &mut Rule, context: &mut Context, index: usize, info: &InfoOptions) -> Value {
    let Some(token) = rule.o.get(index).cloned() else {
        return Value::Undefined;
    };
    let value = token.resolve_val(rule, context);
    if info.text && matches!(token.tin, TIN_ST | TIN_TX) {
        let string = match &value {
            Value::String(value) => value.clone(),
            Value::Text(value) => value.string.clone(),
            _ => return value,
        };
        let quote = if token.tin == TIN_ST {
            token
                .src
                .chars()
                .next()
                .map_or_else(String::new, |ch| ch.to_string())
        } else {
            String::new()
        };
        Value::Text(Text { quote, string })
    } else {
        value
    }
}

fn map_insert(node: &mut Value, key: String, value: Value) {
    match node {
        Value::Object(map) => {
            Arc::make_mut(map).insert(key, value);
        }
        Value::MapRef(map) => {
            Arc::make_mut(map).value.insert(key, value);
        }
        _ => {}
    }
}

fn list_push(node: &mut Value, value: Value) {
    match node {
        Value::Array(array) => Arc::make_mut(array).push(value),
        Value::ListRef(list) => Arc::make_mut(list).value.push(value),
        _ => {}
    }
}

/// Run a built-in with metadata wrappers disabled.
///
/// This keeps the original public helper stable for embedders. Parser-owned
/// execution uses [`run_builtin_action_with_info`] with its configured options.
pub fn run_builtin_action(name: &str, rule: &mut Rule, config: Option<&Value>) -> bool {
    let options = std::sync::Arc::new(crate::Options::default());
    let mut context = Context::new(
        options.rewind.history,
        "",
        Value::Undefined,
        std::sync::Arc::clone(&options),
        crate::InstanceInfo::default(),
    );
    run_builtin_action_with_info(name, rule, &mut context, config, &InfoOptions::default())
}

/// Run a builtin against an explicit live parse context. Embedders that use
/// lazy token values should prefer this over [`run_builtin_action`].
pub fn run_builtin_action_with_context(
    name: &str,
    rule: &mut Rule,
    context: &mut Context,
    config: Option<&Value>,
) -> bool {
    let info = context.options.info.clone();
    run_builtin_action_with_info(name, rule, context, config, &info)
}

pub(crate) fn run_builtin_action_with_info(
    name: &str,
    rule: &mut Rule,
    context: &mut Context,
    config: Option<&Value>,
    info: &InfoOptions,
) -> bool {
    match name {
        "@node$" => {
            if config_bool(config, "init") {
                rule.node = Rc::new(RefCell::new(ast_node(
                    config_string(config, "rule"),
                    config_string(config, "kind"),
                )));
            }
            let nterms = config_usize(config, "nterms");
            if let Some(node) = rule.node.borrow_mut().as_object_mut() {
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
            if !rule.child_node.is_undefined() && !rule.child_node_is_self {
                if let Some(node) = rule.node.borrow_mut().as_object_mut() {
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
                if let Some(parent) = parent_node.borrow_mut().as_object_mut() {
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
                    rule.node = Rc::new(RefCell::new(token_value(rule, context, 0, info)));
                }
            }
        }
        "@value$" => {
            if !rule.child_node.is_undefined() {
                rule.node = Rc::new(RefCell::new(rule.child_node.clone()));
            } else {
                let value = config_index(config, "from").map_or(Value::Undefined, |index| {
                    token_value(rule, context, index, info)
                });
                rule.node = Rc::new(RefCell::new(value));
            }
        }
        "@object$" => {
            rule.node = Rc::new(RefCell::new(map_value(
                info,
                config_bool(config, "implicit"),
            )));
        }
        "@array$" => {
            rule.node = Rc::new(RefCell::new(list_value(
                info,
                config_bool(config, "implicit"),
            )));
        }
        "@reset$" => {
            rule.node = Rc::new(RefCell::new(Value::Undefined));
            rule.child_node = Value::Undefined;
            rule.child_node_is_self = false;
        }
        "@key$" => {
            let slot = {
                let configured = config_string(config, "slot");
                if configured.is_empty() {
                    "key".to_string()
                } else {
                    configured
                }
            };
            // `lit` supplies the key as a CONSTANT instead of reading it
            // from a token, and wins outright when set. A grammar whose
            // structure is declared rather than delimited -- `ver = maj
            // "." min`, where `maj` names a part but no token carries the
            // text "maj" -- has no token for @key$ to read, so without
            // this the key side of @setval$ is unreachable for it.
            if let Some(lit) = config_str_opt(config, "lit") {
                rule.u_mut().insert(slot, Value::String(lit));
            } else if let Some(value) = config_index(config, "from")
                .and_then(|index| rule.o.get(index))
                .map(|token| token.val.clone())
            {
                rule.u_mut().insert(slot, value);
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
                let mut val = rule.child_node.clone();
                if config_bool(config, "src") {
                    val = src_val(val);
                }
                map_insert(&mut rule.node.borrow_mut(), key, val);
            }
        }
        "@push$" => {
            if !rule.child_node.is_undefined() {
                let mut val = rule.child_node.clone();
                if config_bool(config, "src") {
                    val = src_val(val);
                }
                list_push(&mut rule.node.borrow_mut(), val);
            }
        }
        "@map-bo" => {
            rule.node = Rc::new(RefCell::new(map_value(info, false)));
        }
        "@list-bo" => {
            rule.node = Rc::new(RefCell::new(list_value(info, false)));
        }
        // The captured key belongs only to this pair rule and is consumed by
        // @pair-bc on that same rule. Store it in `u`, the non-propagating
        // scratch bag, so it cannot leak into child rules as `n` or `k` would.
        "@pairkey" => {
            if let Some(t0) = rule.o0() {
                let key = match &t0.val {
                    Value::String(s) => s.clone(),
                    _ => t0.src.to_string(),
                };
                rule.u_mut().insert("key".to_string(), Value::String(key));
            }
        }
        "@pair-bc" => {
            if let Some(Value::String(key)) = rule.u.get("key").cloned() {
                let mut node = rule.node.borrow_mut();
                map_insert(&mut node, key, rule.child_node.clone());
            }
        }
        "@elem-bc" if !rule.child_node.is_undefined() => {
            let mut node = rule.node.borrow_mut();
            list_push(&mut node, rule.child_node.clone());
        }
        _ => return false,
    }
    true
}
