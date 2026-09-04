// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Loading and installing the JSON-serializable grammar interchange format.

use crate::rule::{AltSpec, RuleSpec};
use crate::token::name_to_tin;
use crate::{Tabnas, Value};
use indexmap::IndexMap;
use serde_json::{Map, Value as JsonValue};
use std::collections::HashMap;
use std::fmt;

/// Builtin wire schema implemented by this early Rust port.
pub const BUILTIN_SCHEMA_VERSION: u64 = 1;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GrammarError(pub String);

impl fmt::Display for GrammarError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for GrammarError {}

/// An ordered, immutable serialized grammar document.
#[derive(Debug, Clone)]
pub struct GrammarSpec {
    document: JsonValue,
    pub clear: bool,
    pub version: Option<u64>,
    pub meta: Option<IndexMap<String, JsonValue>>,
}

impl GrammarSpec {
    pub fn from_json(src: &str) -> Result<Self, GrammarError> {
        let document: JsonValue = serde_json::from_str(src)
            .map_err(|error| GrammarError(format!("Grammar: invalid JSON: {error}")))?;
        Self::from_value(document)
    }

    pub fn from_slice(src: &[u8]) -> Result<Self, GrammarError> {
        let document: JsonValue = serde_json::from_slice(src)
            .map_err(|error| GrammarError(format!("Grammar: invalid JSON: {error}")))?;
        Self::from_value(document)
    }

    pub fn from_value(document: JsonValue) -> Result<Self, GrammarError> {
        let root = object(&document, "document")?;
        let version = match root.get("v") {
            None => None,
            Some(JsonValue::Number(number)) => number.as_u64().filter(|value| *value > 0),
            Some(_) => None,
        };
        if root.contains_key("v") && version.is_none() {
            return Err(GrammarError(
                "Grammar: invalid builtin schema version (expected a positive integer)".into(),
            ));
        }
        if version.is_some_and(|value| value > BUILTIN_SCHEMA_VERSION) {
            return Err(GrammarError(format!(
                "Grammar: requires builtin schema version {}, but this engine supports up to {}",
                version.unwrap_or_default(),
                BUILTIN_SCHEMA_VERSION
            )));
        }
        let meta = root.get("meta").and_then(JsonValue::as_object).map(|map| {
            map.iter()
                .map(|(key, value)| (key.clone(), value.clone()))
                .collect()
        });
        Ok(Self {
            clear: root.get("clear") == Some(&JsonValue::Bool(true)),
            version,
            meta,
            document,
        })
    }
}

impl Tabnas {
    /// Install a serialized grammar without mutating the caller's document.
    pub fn grammar(&mut self, grammar: &GrammarSpec) -> Result<&mut Self, GrammarError> {
        let root = object(&grammar.document, "document")?;
        if grammar.clear {
            self.rules.clear();
        }
        if let Some(options) = root.get("options") {
            apply_options(&mut self.options, object(options, "options")?)?;
        }
        if let Some(rules) = root.get("rule") {
            for (name, value) in object(rules, "rule")? {
                if value.is_null() {
                    self.rules.shift_remove(name);
                    continue;
                }
                let value = object(value, &format!("rule.{name}"))?;
                let mut spec = self
                    .rules
                    .get(name)
                    .cloned()
                    .unwrap_or_else(|| RuleSpec::new(name));
                if let Some(open) = value.get("open") {
                    apply_alt_list(
                        &mut spec.open,
                        open,
                        &format!("{name}.open"),
                        &self.options.token_set,
                    )?;
                }
                if let Some(close) = value.get("close") {
                    apply_alt_list(
                        &mut spec.close,
                        close,
                        &format!("{name}.close"),
                        &self.options.token_set,
                    )?;
                }
                self.rules.insert(name.clone(), spec);
            }
        }
        Ok(self)
    }

    pub fn grammar_json(&mut self, src: &str) -> Result<&mut Self, GrammarError> {
        let grammar = GrammarSpec::from_json(src)?;
        self.grammar(&grammar)
    }
}

fn object<'a>(
    value: &'a JsonValue,
    label: &str,
) -> Result<&'a Map<String, JsonValue>, GrammarError> {
    value
        .as_object()
        .ok_or_else(|| GrammarError(format!("Grammar: {label} must be an object")))
}

fn apply_alt_list(
    target: &mut Vec<AltSpec>,
    value: &JsonValue,
    label: &str,
    token_sets: &HashMap<String, Vec<i32>>,
) -> Result<(), GrammarError> {
    let (alts, inject) = if let Some(array) = value.as_array() {
        (array, None)
    } else {
        let wrapper = object(value, label)?;
        let alts = wrapper
            .get("alts")
            .and_then(JsonValue::as_array)
            .ok_or_else(|| GrammarError(format!("Grammar: {label}.alts must be an array")))?;
        (alts, wrapper.get("inject").and_then(JsonValue::as_object))
    };
    if inject
        .and_then(|item| item.get("clear"))
        .and_then(JsonValue::as_bool)
        == Some(true)
    {
        target.clear();
    }
    let mut existing: Vec<Option<AltSpec>> = target.drain(..).map(Some).collect();
    if let Some(indices) = inject
        .and_then(|item| item.get("delete"))
        .and_then(JsonValue::as_array)
    {
        for raw in indices.iter().filter_map(JsonValue::as_i64) {
            let index = if raw < 0 {
                existing.len().checked_sub(raw.unsigned_abs() as usize)
            } else {
                Some(raw as usize)
            };
            if let Some(slot) = index.and_then(|index| existing.get_mut(index)) {
                *slot = None;
            }
        }
    }
    if let Some(moves) = inject
        .and_then(|item| item.get("move"))
        .and_then(JsonValue::as_array)
    {
        let moves: Vec<i64> = moves.iter().filter_map(JsonValue::as_i64).collect();
        for pair in moves.chunks_exact(2) {
            if existing.is_empty() {
                break;
            }
            let len = existing.len() as i64;
            let from = pair[0].rem_euclid(len) as usize;
            let to = pair[1].rem_euclid(len) as usize;
            let entry = existing.remove(from);
            existing.insert(to, entry);
        }
    }
    *target = existing.into_iter().flatten().collect();
    let parsed: Result<Vec<_>, _> = alts
        .iter()
        .enumerate()
        .map(|(index, alt)| parse_alt(alt, &format!("{label} alt[{index}]"), token_sets))
        .collect();
    let mut parsed = parsed?;
    if inject
        .and_then(|item| item.get("append"))
        .and_then(JsonValue::as_bool)
        == Some(true)
    {
        target.append(&mut parsed);
    } else {
        parsed.append(target);
        *target = parsed;
    }
    Ok(())
}

fn parse_alt(
    value: &JsonValue,
    label: &str,
    token_sets: &HashMap<String, Vec<i32>>,
) -> Result<AltSpec, GrammarError> {
    let map = object(value, label)?;
    for unsupported in ["e", "h", "c"] {
        if map.contains_key(unsupported) {
            return Err(GrammarError(format!(
                "Grammar: {label}.{unsupported} is not supported by the Rust engine"
            )));
        }
    }
    let mut alt = AltSpec::default();
    if let Some(spec) = map.get("s") {
        let slots: Vec<&str> = match spec {
            JsonValue::String(sequence) => sequence.split_whitespace().collect(),
            JsonValue::Array(sequence) => sequence
                .iter()
                .map(|slot| {
                    slot.as_str().ok_or_else(|| {
                        GrammarError(format!("Grammar: {label}.s entries must be strings"))
                    })
                })
                .collect::<Result<_, _>>()?,
            _ => {
                return Err(GrammarError(format!(
                    "Grammar: {label}.s must be a string or array"
                )))
            }
        };
        for slot in slots {
            let mut tins = Vec::new();
            for name in slot.split_whitespace() {
                if let Some(set) = token_sets.get(name.trim_start_matches('#')) {
                    tins.extend(set.iter().copied());
                } else if let Some(tin) = name_to_tin(name) {
                    tins.push(tin);
                } else {
                    return Err(GrammarError(format!(
                        "Grammar: {label}: unknown token {name}"
                    )));
                }
            }
            alt.s.push(tins);
        }
    }
    alt.b = map.get("b").map_or(Ok(0), |value| {
        value.as_u64().map(|v| v as usize).ok_or_else(|| {
            GrammarError(format!("Grammar: {label}.b must be a non-negative integer"))
        })
    })?;
    alt.p = string_field(map, "p", label)?;
    alt.r = string_field(map, "r", label)?;
    for (field, value) in [("p", &alt.p), ("r", &alt.r)] {
        if value.as_deref().is_some_and(|value| value.starts_with('@')) {
            return Err(GrammarError(format!(
                "Grammar: {label}.{field} dynamic references are not supported by the Rust engine"
            )));
        }
    }
    alt.a = match map.get("a") {
        None => Vec::new(),
        Some(JsonValue::String(action)) => vec![action.clone()],
        Some(JsonValue::Array(actions)) => actions
            .iter()
            .map(|action| {
                action.as_str().map(str::to_owned).ok_or_else(|| {
                    GrammarError(format!("Grammar: {label}.a entries must be strings"))
                })
            })
            .collect::<Result<_, _>>()?,
        Some(_) => {
            return Err(GrammarError(format!(
                "Grammar: {label}.a must be a string or array"
            )))
        }
    };
    alt.n = number_map(map.get("n"), label)?;
    alt.u = value_map(map.get("u"), label)?;
    alt.k = value_map(map.get("k"), label)?;
    alt.g = match map.get("g") {
        None => String::new(),
        Some(JsonValue::String(tags)) => tags.clone(),
        Some(JsonValue::Array(tags)) => tags
            .iter()
            .filter_map(JsonValue::as_str)
            .collect::<Vec<_>>()
            .join(","),
        Some(_) => {
            return Err(GrammarError(format!(
                "Grammar: {label}.g must be a string or array"
            )))
        }
    };
    Ok(alt)
}

fn string_field(
    map: &Map<String, JsonValue>,
    key: &str,
    label: &str,
) -> Result<Option<String>, GrammarError> {
    map.get(key)
        .map(|value| {
            value
                .as_str()
                .map(str::to_owned)
                .ok_or_else(|| GrammarError(format!("Grammar: {label}.{key} must be a string")))
        })
        .transpose()
}

fn number_map(
    value: Option<&JsonValue>,
    label: &str,
) -> Result<HashMap<String, i32>, GrammarError> {
    let Some(value) = value else {
        return Ok(HashMap::new());
    };
    object(value, &format!("{label}.n"))?
        .iter()
        .map(|(key, value)| {
            value
                .as_i64()
                .and_then(|v| i32::try_from(v).ok())
                .map(|v| (key.clone(), v))
                .ok_or_else(|| GrammarError(format!("Grammar: {label}.n.{key} must be an integer")))
        })
        .collect()
}

fn value_map(
    value: Option<&JsonValue>,
    label: &str,
) -> Result<HashMap<String, Value>, GrammarError> {
    let Some(value) = value else {
        return Ok(HashMap::new());
    };
    Ok(object(value, label)?
        .iter()
        .map(|(key, value)| (key.clone(), Value::from_json(value)))
        .collect())
}

fn apply_options(
    options: &mut crate::Options,
    map: &Map<String, JsonValue>,
) -> Result<(), GrammarError> {
    if let Some(tag) = map.get("tag").and_then(JsonValue::as_str) {
        options.tag = tag.into();
    }
    if let Some(rule) = map.get("rule") {
        let rule = object(rule, "options.rule")?;
        if let Some(start) = rule.get("start").and_then(JsonValue::as_str) {
            options.rule.start = start.into();
        }
        if let Some(finish) = rule.get("finish").and_then(JsonValue::as_bool) {
            options.rule.finish = finish;
        }
        if let Some(include) = rule.get("include").and_then(JsonValue::as_str) {
            options.rule.include = include.into();
        }
    }
    Ok(())
}

pub fn validate_grammar(rules: &IndexMap<String, RuleSpec>) -> Vec<String> {
    let mut problems = Vec::new();
    for (name, spec) in rules {
        for (state, alts) in [("open", &spec.open), ("close", &spec.close)] {
            for (index, alt) in alts.iter().enumerate() {
                for referred in [&alt.p, &alt.r].into_iter().flatten() {
                    if !referred.starts_with('@') && !rules.contains_key(referred) {
                        problems.push(format!(
                            "{name}.{state} alt[{index}]: unknown rule: {referred}"
                        ));
                    }
                }
            }
        }
    }
    problems.sort_by(|left, right| left.encode_utf16().cmp(right.encode_utf16()));
    problems
}
