// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Loading and installing the JSON-serializable grammar interchange format.

use crate::rule::{AltSpec, CompareOp, Condition, RuleSpec};
use crate::utility::{modlist, ListMods};
use crate::{
    builtins::is_builtin_action, AltBack, AltCondition, AltError, AltModifier, AltNext,
    BudgetCheck, CommentSuffixMatcher, ConfigModifier, ErrorSuffixCallback, LexCheck,
    LexMatcherCallback, MatchTokenCallback, ParsePrepare, Tabnas, TextModifier, Value,
    ValueTransform,
};
use indexmap::IndexMap;
use regex::RegexBuilder;
use serde_json::{Map, Value as JsonValue};
use std::collections::HashMap;
use std::fmt;

#[derive(Clone)]
struct AltRefs {
    conditions: HashMap<String, AltCondition>,
    modifiers: HashMap<String, AltModifier>,
    errors: HashMap<String, AltError>,
    pushes: HashMap<String, AltNext>,
    replaces: HashMap<String, AltNext>,
    backtracks: HashMap<String, AltBack>,
    match_tokens: HashMap<String, (MatchTokenCallback, bool)>,
    value_transforms: HashMap<String, ValueTransform>,
    text_modifiers: HashMap<String, TextModifier>,
    lex_checks: HashMap<String, LexCheck>,
    comment_suffixes: HashMap<String, CommentSuffixMatcher>,
    match_values: HashMap<String, MatchTokenCallback>,
    parse_prepares: HashMap<String, ParsePrepare>,
    budget_checks: HashMap<String, BudgetCheck>,
    lex_matches: HashMap<String, LexMatcherCallback>,
    error_suffixes: HashMap<String, ErrorSuffixCallback>,
    config_modifiers: HashMap<String, ConfigModifier>,
    parser_starts: HashMap<String, crate::ParserStart>,
}

impl From<&Tabnas> for AltRefs {
    fn from(tabnas: &Tabnas) -> Self {
        Self {
            conditions: tabnas.alt_conditions.clone(),
            modifiers: tabnas.alt_modifiers.clone(),
            errors: tabnas.alt_errors.clone(),
            pushes: tabnas.alt_pushes.clone(),
            replaces: tabnas.alt_replaces.clone(),
            backtracks: tabnas.alt_backtracks.clone(),
            match_tokens: tabnas.match_token_refs.clone(),
            value_transforms: tabnas.value_transform_refs.clone(),
            text_modifiers: tabnas.text_modifier_refs.clone(),
            lex_checks: tabnas.lex_check_refs.clone(),
            comment_suffixes: tabnas.comment_suffix_refs.clone(),
            match_values: tabnas.match_value_refs.clone(),
            parse_prepares: tabnas.parse_prepare_refs.clone(),
            budget_checks: tabnas.budget_check_refs.clone(),
            lex_matches: tabnas.lex_match_refs.clone(),
            error_suffixes: tabnas.error_suffix_refs.clone(),
            config_modifiers: tabnas.config_modifier_refs.clone(),
            parser_starts: tabnas.parser_start_refs.clone(),
        }
    }
}

/// Builtin wire schema implemented by the Rust port. Schema v3 adds the
/// `@fold$` tree action used by current BNF-family compiler output.
pub const BUILTIN_SCHEMA_VERSION: u64 = 3;

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
    /// Installation is transactional: an invalid option, rule, reference, or
    /// builtin payload leaves the existing parser unchanged.
    pub fn grammar(&mut self, grammar: &GrammarSpec) -> Result<&mut Self, GrammarError> {
        let mut staged = self.clone();
        staged.install_grammar(grammar)?;
        *self = staged;
        Ok(self)
    }

    fn install_grammar(&mut self, grammar: &GrammarSpec) -> Result<(), GrammarError> {
        let root = object(&grammar.document, "document")?;
        let refs = AltRefs::from(&*self);
        if grammar.clear {
            self.rules.clear();
            self.options.fixed.tokens.clear();
        }
        if let Some(options) = root.get("options") {
            apply_options(&mut self.options, object(options, "options")?, &refs)?;
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
                        &mut self.options,
                        &refs,
                    )?;
                }
                if let Some(close) = value.get("close") {
                    apply_alt_list(
                        &mut spec.close,
                        close,
                        &format!("{name}.close"),
                        &mut self.options,
                        &refs,
                    )?;
                }
                wire_state_actions(self, &mut spec);
                validate_action_references(self, &spec)?;
                self.rules.insert(name.clone(), spec);
            }
        }
        Ok(())
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
    options: &mut crate::Options,
    refs: &AltRefs,
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
    let mods = inject.map(|inject| ListMods {
        delete: integer_list(inject.get("delete")),
        move_items: integer_list(inject.get("move")),
    });
    *target = modlist(std::mem::take(target), mods.as_ref());
    let parsed: Result<Vec<_>, _> = alts
        .iter()
        .enumerate()
        .map(|(index, alt)| parse_alt(alt, &format!("{label} alt[{index}]"), options, refs))
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

fn integer_list(value: Option<&JsonValue>) -> Vec<isize> {
    value
        .and_then(JsonValue::as_array)
        .into_iter()
        .flatten()
        .filter_map(JsonValue::as_i64)
        .filter_map(|value| isize::try_from(value).ok())
        .collect()
}

fn parse_alt(
    value: &JsonValue,
    label: &str,
    options: &mut crate::Options,
    refs: &AltRefs,
) -> Result<AltSpec, GrammarError> {
    let map = object(value, label)?;
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
                if let Some(set) = options.token_set.get(name.trim_start_matches('#')) {
                    tins.extend(set.iter().copied());
                } else {
                    tins.push(
                        options
                            .token(name)
                            .unwrap_or_else(|| options.register_token(name)),
                    );
                }
            }
            alt.s.push(tins);
        }
    }
    match map.get("b") {
        None | Some(JsonValue::Null) | Some(JsonValue::Bool(false)) => {}
        Some(JsonValue::String(reference)) if reference.starts_with('@') => {
            alt.b_fn = Some(refs.backtracks.get(reference).cloned().ok_or_else(|| {
                GrammarError(format!(
                    "Grammar: unknown backtrack function reference: {reference}"
                ))
            })?);
        }
        Some(value) => {
            alt.b = value.as_u64().map(|v| v as usize).ok_or_else(|| {
                GrammarError(format!("Grammar: {label}.b must be a non-negative integer"))
            })?;
        }
    }
    alt.p = string_field(map, "p", label)?;
    alt.r = string_field(map, "r", label)?;
    if let Some(reference) = alt.p.as_deref().filter(|value| value.starts_with('@')) {
        alt.p_fn = Some(refs.pushes.get(reference).cloned().ok_or_else(|| {
            GrammarError(format!(
                "Grammar: unknown push function reference: {reference}"
            ))
        })?);
        alt.p = None;
    }
    if let Some(reference) = alt.r.as_deref().filter(|value| value.starts_with('@')) {
        alt.r_fn = Some(refs.replaces.get(reference).cloned().ok_or_else(|| {
            GrammarError(format!(
                "Grammar: unknown replace function reference: {reference}"
            ))
        })?);
        alt.r = None;
    }
    alt.a = match map.get("a") {
        None | Some(JsonValue::Null) | Some(JsonValue::Bool(false)) => Vec::new(),
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
    match map.get("c") {
        Some(JsonValue::String(reference)) => {
            if matches!(
                reference.as_str(),
                "@probePhase0$" | "@probePhase1$" | "@probePhase2$"
            ) {
                alt.c_ref = Some(reference.clone());
            } else if let Some(condition) = refs.conditions.get(reference) {
                alt.c_fn = Some(condition.clone());
            } else {
                return Err(GrammarError(format!(
                    "Grammar: unknown condition function reference: {reference}"
                )));
            }
        }
        value => alt.c = parse_conditions(value, label)?,
    }
    if let Some(reference) = optional_ref_field(map, "h", label)? {
        alt.h = Some(refs.modifiers.get(&reference).cloned().ok_or_else(|| {
            GrammarError(format!(
                "Grammar: unknown modifier function reference: {reference}"
            ))
        })?);
    }
    if let Some(reference) = optional_ref_field(map, "e", label)? {
        alt.e = Some(refs.errors.get(&reference).cloned().ok_or_else(|| {
            GrammarError(format!(
                "Grammar: unknown error function reference: {reference}"
            ))
        })?);
    }
    alt.n = number_map(map.get("n"), label)?;
    alt.u = value_map(map.get("u"), label)?;
    alt.k = value_map(map.get("k"), label)?;
    for action in &alt.a {
        if matches!(
            action.as_str(),
            "@node$"
                | "@capture$"
                | "@fold$"
                | "@object$"
                | "@array$"
                | "@key$"
                | "@setval$"
                | "@value$"
        ) {
            let key = action.trim_start_matches('@');
            if let Some(config) = alt.k.remove(key) {
                validate_builtin_config(action, &config, label)?;
                alt.action_configs.insert(action.clone(), config);
            }
        }
    }
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
    validate_group_tags(&alt.g, label)?;
    Ok(alt)
}

fn optional_ref_field(
    map: &Map<String, JsonValue>,
    key: &str,
    label: &str,
) -> Result<Option<String>, GrammarError> {
    match map.get(key) {
        None | Some(JsonValue::Null) => Ok(None),
        Some(JsonValue::String(reference)) if reference.starts_with('@') => {
            Ok(Some(reference.clone()))
        }
        Some(_) => Err(GrammarError(format!(
            "Grammar: {label}.{key} must be a function reference"
        ))),
    }
}

fn validate_action_references(tabnas: &Tabnas, spec: &RuleSpec) -> Result<(), GrammarError> {
    for (state, alts) in [("open", &spec.open), ("close", &spec.close)] {
        for (index, alt) in alts.iter().enumerate() {
            for action in &alt.a {
                if !is_builtin_action(action)
                    && !tabnas.actions.contains_key(action)
                    && !tabnas.context_actions.contains_key(action)
                {
                    return Err(GrammarError(format!(
                        "Grammar: {}.{state} alt[{index}]: unknown action function reference: {action}",
                        spec.name
                    )));
                }
            }
        }
    }
    for (phase, actions) in [
        ("bo", &spec.bo),
        ("ao", &spec.ao),
        ("bc", &spec.bc),
        ("ac", &spec.ac),
    ] {
        for action in actions {
            if !is_builtin_action(action)
                && !tabnas.actions.contains_key(action)
                && !tabnas.context_actions.contains_key(action)
            {
                return Err(GrammarError(format!(
                    "Grammar: {}.{phase}: unknown state action function reference: {action}",
                    spec.name
                )));
            }
        }
    }
    Ok(())
}

fn wire_state_actions(tabnas: &Tabnas, spec: &mut RuleSpec) {
    let rule_name = spec.name.clone();
    for (phase, actions) in [
        ("bo", &mut spec.bo),
        ("ao", &mut spec.ao),
        ("bc", &mut spec.bc),
        ("ac", &mut spec.ac),
    ] {
        let base = format!("@{rule_name}-{phase}");
        let replacement = format!("{base}/replace");
        if tabnas.context_actions.contains_key(&replacement) {
            actions.clear();
            actions.push(replacement);
            continue;
        }

        let prepend = format!("{base}/prepend");
        if tabnas.context_actions.contains_key(&prepend) && !actions.contains(&prepend) {
            actions.insert(0, prepend);
        }

        let explicit_append = format!("{base}/append");
        let append = tabnas
            .context_actions
            .contains_key(&explicit_append)
            .then_some(explicit_append)
            .or_else(|| tabnas.context_actions.contains_key(&base).then_some(base));
        if let Some(append) = append.filter(|append| !actions.contains(append)) {
            actions.push(append);
        }
    }
}

fn validate_builtin_config(action: &str, config: &Value, label: &str) -> Result<(), GrammarError> {
    let Value::Object(config) = config else {
        return Err(GrammarError(format!(
            "Grammar: {label}.k.{} must be an object",
            action.trim_start_matches('@')
        )));
    };
    let fields: &[(&str, &str)] = match action {
        "@node$" => &[
            ("init", "boolean"),
            ("rule", "string"),
            ("kind", "string"),
            ("nterms", "non-negative integer"),
        ],
        "@capture$" => &[("rule", "string"), ("kind", "string")],
        "@fold$" => &[("cN", "non-negative integer")],
        "@object$" | "@array$" => &[("implicit", "boolean")],
        "@key$" => &[("slot", "string"), ("from", "integer")],
        "@setval$" => &[("slot", "string")],
        "@value$" => &[("from", "integer")],
        _ => return Ok(()),
    };
    for key in config.keys() {
        if !fields.iter().any(|(field, _)| key == field) {
            return Err(GrammarError(format!(
                "Grammar: {label}.k.{} has unknown field {key}",
                action.trim_start_matches('@')
            )));
        }
    }
    for (field, expected) in fields {
        let Some(value) = config.get(*field) else {
            continue;
        };
        let valid = match *expected {
            "boolean" => matches!(value, Value::Bool(_)),
            "string" => matches!(value, Value::String(_)),
            "integer" => {
                matches!(value, Value::Number(number) if number.is_finite() && number.fract() == 0.0)
            }
            "non-negative integer" => {
                matches!(value, Value::Number(number) if number.is_finite() && *number >= 0.0 && number.fract() == 0.0)
            }
            _ => false,
        };
        if !valid {
            return Err(GrammarError(format!(
                "Grammar: {label}.k.{}.{} must be a {expected}",
                action.trim_start_matches('@'),
                field
            )));
        }
    }
    Ok(())
}

fn parse_conditions(
    value: Option<&JsonValue>,
    label: &str,
) -> Result<Vec<Condition>, GrammarError> {
    let Some(value) = value else {
        return Ok(Vec::new());
    };
    if value.is_null() {
        return Ok(Vec::new());
    }
    let conditions = object(value, &format!("{label}.c"))?;
    let roots = [
        "n", "u", "k", "d", "i", "name", "state", "node", "need", "oN", "cN", "o", "c", "o0", "o1",
        "c0", "c1", "parent", "child", "prev", "next", "spec",
    ];
    let mut output = Vec::new();
    for (path, definition) in conditions {
        if definition.is_null() {
            continue;
        }
        let parts: Vec<String> = path.split('.').map(str::to_owned).collect();
        if !roots.contains(&parts[0].as_str()) {
            return Err(GrammarError(format!(
                "{label}: unknown condition path: \"{path}\""
            )));
        }
        if let Some(operators) = definition.as_object() {
            for (operator, value) in operators {
                let op = match operator.as_str() {
                    "$eq" => CompareOp::Eq,
                    "$ne" => CompareOp::Ne,
                    "$lt" => CompareOp::Lt,
                    "$lte" => CompareOp::Lte,
                    "$gt" => CompareOp::Gt,
                    "$gte" => CompareOp::Gte,
                    "$exist" => CompareOp::Exist,
                    _ => {
                        return Err(GrammarError(format!(
                            "{label}: unknown condition operator: {operator}"
                        )))
                    }
                };
                output.push(Condition {
                    path: parts.clone(),
                    op,
                    value: Value::from_json(value),
                });
            }
        } else {
            output.push(Condition {
                path: parts,
                op: CompareOp::Eq,
                value: Value::from_json(definition),
            });
        }
    }
    Ok(output)
}

fn validate_group_tags(tags: &str, label: &str) -> Result<(), GrammarError> {
    let valid = regex::Regex::new("^[a-z][a-z0-9-]+$").expect("static group regex");
    for tag in tags.split(',').map(str::trim).filter(|tag| !tag.is_empty()) {
        if !valid.is_match(tag) {
            return Err(GrammarError(format!(
                "{label}: invalid group tag: \"{tag}\""
            )));
        }
    }
    Ok(())
}

fn string_field(
    map: &Map<String, JsonValue>,
    key: &str,
    label: &str,
) -> Result<Option<String>, GrammarError> {
    match map.get(key) {
        None | Some(JsonValue::Null) | Some(JsonValue::Bool(false)) => Ok(None),
        Some(JsonValue::String(value)) => Ok(Some(value.clone())),
        Some(_) => Err(GrammarError(format!(
            "Grammar: {label}.{key} must be a string"
        ))),
    }
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

fn apply_lex_check(
    map: &Map<String, JsonValue>,
    label: &str,
    target: &mut Option<LexCheck>,
    refs: &AltRefs,
) -> Result<(), GrammarError> {
    let Some(value) = map.get("check") else {
        return Ok(());
    };
    if value.is_null() || value == &JsonValue::Bool(false) {
        *target = None;
        return Ok(());
    }
    let reference = value.as_str().ok_or_else(|| {
        GrammarError(format!(
            "Grammar: {label}.check must be a function reference or null"
        ))
    })?;
    *target = Some(refs.lex_checks.get(reference).cloned().ok_or_else(|| {
        GrammarError(format!(
            "Grammar: unknown lexer check function reference: {reference}"
        ))
    })?);
    Ok(())
}

fn resolve_value_producer(
    value: Option<&JsonValue>,
    refs: &AltRefs,
) -> (Option<Value>, Option<ValueTransform>) {
    match value.filter(|value| !value.is_null()) {
        Some(JsonValue::String(reference)) if refs.value_transforms.contains_key(reference) => {
            (None, refs.value_transforms.get(reference).cloned())
        }
        Some(JsonValue::String(reference)) if reference.starts_with("@@") => {
            (Some(Value::String(reference[1..].to_string())), None)
        }
        Some(value) => (Some(Value::from_json(value)), None),
        None => (None, None),
    }
}

fn apply_options(
    options: &mut crate::Options,
    map: &Map<String, JsonValue>,
    refs: &AltRefs,
) -> Result<(), GrammarError> {
    if let Some(tag) = map.get("tag").and_then(JsonValue::as_str) {
        options.tag = tag.into();
    }
    for (field, target) in [("error", &mut options.error), ("hint", &mut options.hint)] {
        if let Some(overrides) = map.get(field) {
            for (code, template) in object(overrides, &format!("options.{field}"))? {
                if template.is_null() || template == &JsonValue::Bool(false) {
                    target.remove(code);
                } else {
                    target.insert(
                        code.clone(),
                        template
                            .as_str()
                            .ok_or_else(|| {
                                GrammarError(format!(
                                    "Grammar: options.{field}.{code} must be a string or null"
                                ))
                            })?
                            .into(),
                    );
                }
            }
        }
    }
    if let Some(errmsg) = map.get("errmsg") {
        let errmsg = object(errmsg, "options.errmsg")?;
        if let Some(name) = errmsg.get("name") {
            options.errmsg.name = name
                .as_str()
                .ok_or_else(|| {
                    GrammarError("Grammar: options.errmsg.name must be a string".into())
                })?
                .into();
        }
        if let Some(suffix) = errmsg.get("suffix") {
            options.errmsg.suffix = match suffix {
                JsonValue::Null | JsonValue::Bool(false) => crate::ErrorSuffix::Disabled,
                JsonValue::Bool(true) => crate::ErrorSuffix::Standard,
                JsonValue::String(reference) => {
                    refs.error_suffixes.get(reference).cloned().map_or_else(
                        || crate::ErrorSuffix::Text(reference.clone()),
                        crate::ErrorSuffix::Callback,
                    )
                }
                _ => {
                    return Err(GrammarError(
                        "Grammar: options.errmsg.suffix must be a boolean, string, or null".into(),
                    ))
                }
            };
        }
        if let Some(link) = errmsg.get("link") {
            options.errmsg.link = if link.is_null() {
                String::new()
            } else {
                link.as_str()
                    .ok_or_else(|| {
                        GrammarError("Grammar: options.errmsg.link must be a string or null".into())
                    })?
                    .into()
            };
        }
    }
    if let Some(color) = map.get("color") {
        let color = object(color, "options.color")?;
        set_bool(color, "active", &mut options.color.active);
        for (field, target) in [
            ("reset", &mut options.color.reset),
            ("hi", &mut options.color.hi),
            ("lo", &mut options.color.lo),
            ("line", &mut options.color.line),
        ] {
            if let Some(value) = color.get(field) {
                *target = value
                    .as_str()
                    .ok_or_else(|| {
                        GrammarError(format!("Grammar: options.color.{field} must be a string"))
                    })?
                    .into();
            }
        }
    }
    if let Some(text) = map.get("text") {
        let text = object(text, "options.text")?;
        set_bool(text, "lex", &mut options.text.lex);
        apply_lex_check(text, "options.text", &mut options.text.check, refs)?;
        if let Some(modify) = text.get("modify") {
            let references =
                match modify {
                    JsonValue::Null | JsonValue::Bool(false) => Vec::new(),
                    JsonValue::String(reference) => vec![reference.as_str()],
                    JsonValue::Array(references) => references
                        .iter()
                        .map(|reference| {
                            reference.as_str().ok_or_else(|| {
                                GrammarError(
                                "Grammar: options.text.modify entries must be function references"
                                    .into(),
                            )
                            })
                        })
                        .collect::<Result<Vec<_>, _>>()?,
                    _ => return Err(GrammarError(
                        "Grammar: options.text.modify must be a function reference, array, or null"
                            .into(),
                    )),
                };
            options.text.modify = references
                .into_iter()
                .map(|reference| {
                    refs.text_modifiers.get(reference).cloned().ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: unknown text modifier function reference: {reference}"
                        ))
                    })
                })
                .collect::<Result<_, _>>()?;
        }
    }
    if let Some(space) = map.get("space") {
        let space = object(space, "options.space")?;
        set_bool(space, "lex", &mut options.space.lex);
        apply_lex_check(space, "options.space", &mut options.space.check, refs)?;
        if let Some(chars) = space.get("chars") {
            options.space.chars = chars
                .as_str()
                .ok_or_else(|| {
                    GrammarError("Grammar: options.space.chars must be a string".into())
                })?
                .into();
        }
    }
    if let Some(number) = map.get("number") {
        let number = object(number, "options.number")?;
        set_bool(number, "lex", &mut options.number.lex);
        set_bool(number, "hex", &mut options.number.hex);
        set_bool(number, "oct", &mut options.number.oct);
        set_bool(number, "bin", &mut options.number.bin);
        apply_lex_check(number, "options.number", &mut options.number.check, refs)?;
        if let Some(separator) = number.get("sep") {
            options.number.sep = separator.as_str().map(str::to_owned);
        }
        if let Some(exclude) = number.get("exclude") {
            options.number.exclude = exclude.as_str().map(str::to_owned);
        }
    }
    if let Some(string) = map.get("string") {
        let string = object(string, "options.string")?;
        set_bool(string, "lex", &mut options.string.lex);
        apply_lex_check(string, "options.string", &mut options.string.check, refs)?;
        if let Some(chars) = string.get("chars").and_then(JsonValue::as_str) {
            options.string.chars = chars.into();
        }
        if let Some(chars) = string
            .get("multiChars")
            .or_else(|| string.get("multi_chars"))
            .and_then(JsonValue::as_str)
        {
            options.string.multi_chars = chars.into();
        }
        if let Some(escape_char) = string.get("escapeChar") {
            let escape_char = escape_char.as_str().ok_or_else(|| {
                GrammarError("Grammar: options.string.escapeChar must be a string".into())
            })?;
            let mut chars = escape_char.chars();
            options.string.escape_char = chars.next().ok_or_else(|| {
                GrammarError("Grammar: options.string.escapeChar must not be empty".into())
            })?;
            if chars.next().is_some() {
                return Err(GrammarError(
                    "Grammar: options.string.escapeChar must contain one character".into(),
                ));
            }
        }
        for (field, target) in [
            ("escape", &mut options.string.escape),
            ("replace", &mut options.string.replace),
        ] {
            if let Some(entries) = string.get(field) {
                for (key, value) in object(entries, &format!("options.string.{field}"))? {
                    let mut chars = key.chars();
                    let character = chars.next().ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: options.string.{field} keys must not be empty"
                        ))
                    })?;
                    if chars.next().is_some() {
                        return Err(GrammarError(format!(
                            "Grammar: options.string.{field} keys must contain one character"
                        )));
                    }
                    if value.is_null() {
                        target.remove(&character);
                    } else {
                        target.insert(
                            character,
                            value
                                .as_str()
                                .ok_or_else(|| {
                                    GrammarError(format!(
                                        "Grammar: options.string.{field}.{key} must be a string or null"
                                    ))
                                })?
                                .into(),
                        );
                    }
                }
            }
        }
        if let Some(value) = string
            .get("allowUnknown")
            .or_else(|| string.get("allow_unknown"))
            .and_then(JsonValue::as_bool)
        {
            options.string.allow_unknown = value;
        }
        if let Some(value) = string
            .get("escapeStrict")
            .or_else(|| string.get("escape_strict"))
            .and_then(JsonValue::as_bool)
        {
            options.string.escape_strict = value;
        }
        if let Some(value) = string
            .get("allowControl")
            .or_else(|| string.get("allow_control"))
            .and_then(JsonValue::as_bool)
        {
            options.string.allow_control = value;
        }
        if let Some(value) = string.get("abandon").and_then(JsonValue::as_bool) {
            options.string.abandon = value;
        }
    }
    if let Some(line) = map.get("line") {
        let line = object(line, "options.line")?;
        set_bool(line, "lex", &mut options.line.lex);
        set_bool(line, "single", &mut options.line.single);
        apply_lex_check(line, "options.line", &mut options.line.check, refs)?;
        if let Some(chars) = line.get("chars") {
            options.line.chars = chars
                .as_str()
                .ok_or_else(|| GrammarError("Grammar: options.line.chars must be a string".into()))?
                .into();
        }
        if let Some(chars) = line.get("rowChars") {
            options.line.row_chars = chars
                .as_str()
                .ok_or_else(|| {
                    GrammarError("Grammar: options.line.rowChars must be a string".into())
                })?
                .into();
        }
        if let Some(chars) = line.get("fixed").and_then(JsonValue::as_str) {
            options.line.fixed = chars.chars().collect();
        }
    }
    if let Some(comment) = map.get("comment") {
        let comment = object(comment, "options.comment")?;
        set_bool(comment, "lex", &mut options.comment.lex);
        apply_lex_check(comment, "options.comment", &mut options.comment.check, refs)?;
        if let Some(definitions) = comment.get("def") {
            for (name, value) in object(definitions, "options.comment.def")? {
                if value.is_null() || value == &JsonValue::Bool(false) {
                    options.comment.definitions.shift_remove(name);
                    continue;
                }
                let value = object(value, &format!("options.comment.def.{name}"))?;
                let definition =
                    options
                        .comment
                        .definitions
                        .entry(name.clone())
                        .or_insert(crate::CommentDef {
                            line: false,
                            start: String::new(),
                            end: String::new(),
                            lex: false,
                            suffixes: Vec::new(),
                            suffix_matcher: None,
                            eat_line: false,
                        });
                set_bool(value, "line", &mut definition.line);
                set_bool(value, "lex", &mut definition.lex);
                set_bool(value, "eatline", &mut definition.eat_line);
                for (field, target) in [
                    ("start", &mut definition.start),
                    ("end", &mut definition.end),
                ] {
                    if let Some(text) = value.get(field) {
                        *target = text
                            .as_str()
                            .ok_or_else(|| {
                                GrammarError(format!(
                                    "Grammar: options.comment.def.{name}.{field} must be a string"
                                ))
                            })?
                            .into();
                    }
                }
                if let Some(suffix) = value.get("suffix") {
                    definition.suffix_matcher = None;
                    definition.suffixes = match suffix {
                        JsonValue::Null => Vec::new(),
                        JsonValue::String(reference)
                            if refs.comment_suffixes.contains_key(reference) =>
                        {
                            definition.suffix_matcher =
                                refs.comment_suffixes.get(reference).cloned();
                            Vec::new()
                        }
                        JsonValue::String(suffix) => {
                            if suffix.is_empty() {
                                Vec::new()
                            } else {
                                vec![suffix.clone()]
                            }
                        }
                        JsonValue::Array(suffixes) => suffixes
                            .iter()
                            .map(|suffix| {
                                suffix.as_str().map(str::to_owned).ok_or_else(|| {
                                    GrammarError(format!(
                                        "Grammar: options.comment.def.{name}.suffix entries must be strings"
                                    ))
                                })
                            })
                            .collect::<Result<Vec<_>, _>>()?,
                        _ => {
                            return Err(GrammarError(format!(
                                "Grammar: options.comment.def.{name}.suffix must be a string, array, or null"
                            )))
                        }
                    };
                    definition.suffixes.retain(|suffix| !suffix.is_empty());
                    definition
                        .suffixes
                        .sort_by(|a, b| b.len().cmp(&a.len()).then_with(|| a.cmp(b)));
                }
            }
        }
    }
    if let Some(value) = map.get("value") {
        let value = object(value, "options.value")?;
        set_bool(value, "lex", &mut options.value.lex);
        if let Some(definitions) = value.get("def") {
            for (name, value) in object(definitions, "options.value.def")? {
                if value.is_null() || value == &JsonValue::Bool(false) {
                    options.value.definitions.shift_remove(name);
                    continue;
                }
                let value = object(value, &format!("options.value.def.{name}"))?;
                let (val, transform) = resolve_value_producer(value.get("val"), refs);
                let matcher = value
                    .get("match")
                    .map(|matcher| {
                        let source = matcher.as_str().ok_or_else(|| {
                            GrammarError(format!(
                                "Grammar: options.value.def.{name}.match must be a serialized regex"
                            ))
                        })?;
                        compile_serialized_regex(
                            source,
                            &format!("options.value.def.{name}.match"),
                            true,
                        )
                        .map(|(regex, _)| regex)
                    })
                    .transpose()?;
                let consume = match value.get("consume") {
                    Some(consume) => consume.as_bool().ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: options.value.def.{name}.consume must be a boolean"
                        ))
                    })?,
                    None => false,
                };
                options.value.definitions.insert(
                    name.clone(),
                    crate::ValueDef {
                        val,
                        matcher,
                        transform,
                        consume,
                    },
                );
            }
        }
    }
    if let Some(ender) = map.get("ender") {
        options.ender = match ender {
            JsonValue::Null => Vec::new(),
            JsonValue::String(ender) => vec![ender.clone()],
            JsonValue::Array(enders) => enders
                .iter()
                .map(|ender| {
                    ender.as_str().map(str::to_owned).ok_or_else(|| {
                        GrammarError("Grammar: options.ender entries must be strings".into())
                    })
                })
                .collect::<Result<_, _>>()?,
            _ => {
                return Err(GrammarError(
                    "Grammar: options.ender must be a string, array, or null".into(),
                ))
            }
        };
        options.ender.retain(|ender| !ender.is_empty());
    }
    if let Some(map_options) = map.get("map") {
        set_bool(
            object(map_options, "options.map")?,
            "extend",
            &mut options.map.extend,
        );
    }
    if let Some(info) = map.get("info") {
        let info = object(info, "options.info")?;
        set_bool(info, "map", &mut options.info.map);
        set_bool(info, "list", &mut options.info.list);
        set_bool(info, "text", &mut options.info.text);
        if let Some(marker) = info.get("marker") {
            options.info.marker = marker
                .as_str()
                .ok_or_else(|| {
                    GrammarError("Grammar: options.info.marker must be a string".into())
                })?
                .to_string();
        }
    }
    if let Some(lex) = map.get("lex") {
        let lex = object(lex, "options.lex")?;
        set_bool(lex, "empty", &mut options.lex.empty);
        set_bool(lex, "relex", &mut options.lex.relex);
        if let Some(value) = lex.get("emptyResult") {
            options.lex.empty_result = Value::from_json(value);
        }
        if let Some(matchers) = lex.get("match") {
            for (name, spec) in object(matchers, "options.lex.match")? {
                if spec.is_null() || spec == &JsonValue::Bool(false) {
                    options.lex.matchers.shift_remove(name);
                    continue;
                }
                let spec = object(spec, &format!("options.lex.match.{name}"))?;
                let order = spec
                    .get("order")
                    .and_then(JsonValue::as_f64)
                    .filter(|order| order.is_finite())
                    .ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: options.lex.match.{name}.order must be a finite number"
                        ))
                    })?;
                if order < 0.0 {
                    options.lex.matchers.shift_remove(name);
                    continue;
                }
                let reference = spec
                    .get("make")
                    .and_then(JsonValue::as_str)
                    .ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: options.lex.match.{name}.make must be a function reference"
                        ))
                    })?;
                let matcher = refs.lex_matches.get(reference).cloned().ok_or_else(|| {
                    GrammarError(format!(
                        "Grammar: unknown custom lexer matcher function reference: {reference}"
                    ))
                })?;
                options.lex.matchers.insert(
                    name.clone(),
                    crate::LexMatcher {
                        name: name.clone(),
                        order,
                        matcher,
                    },
                );
            }
            options.lex.matchers.sort_by(|name_a, left, name_b, right| {
                left.order
                    .total_cmp(&right.order)
                    .then_with(|| name_a.cmp(name_b))
            });
        }
    }
    if let Some(rewind) = map.get("rewind") {
        let rewind = object(rewind, "options.rewind")?;
        if let Some(history) = rewind.get("history") {
            options.rewind.history = match history {
                JsonValue::Null => None,
                JsonValue::Number(number) => match number.as_i64() {
                    Some(value) if value <= 0 => None,
                    Some(value) => Some(usize::try_from(value).map_err(|_| {
                        GrammarError(
                            "Grammar: options.rewind.history is outside the supported range".into(),
                        )
                    })?),
                    None => {
                        return Err(GrammarError(
                            "Grammar: options.rewind.history must be an integer or null".into(),
                        ))
                    }
                },
                _ => {
                    return Err(GrammarError(
                        "Grammar: options.rewind.history must be an integer or null".into(),
                    ))
                }
            };
        }
    }
    if let Some(rule) = map.get("rule") {
        let rule = object(rule, "options.rule")?;
        if let Some(start) = rule.get("start").and_then(JsonValue::as_str) {
            options.rule.start = start.into();
        }
        if let Some(finish) = rule.get("finish").and_then(JsonValue::as_bool) {
            options.rule.finish = finish;
        }
        if let Some(maxmul) = rule.get("maxmul") {
            options.rule.maxmul = match maxmul.as_i64() {
                Some(value) if value <= 0 => 3,
                Some(value) => usize::try_from(value).map_err(|_| {
                    GrammarError(
                        "Grammar: options.rule.maxmul is outside the supported range".into(),
                    )
                })?,
                None => {
                    return Err(GrammarError(
                        "Grammar: options.rule.maxmul must be an integer".into(),
                    ))
                }
            };
        }
        if let Some(include) = rule.get("include").and_then(JsonValue::as_str) {
            options.rule.include = include.into();
        }
        if let Some(exclude) = rule.get("exclude").and_then(JsonValue::as_str) {
            options.rule.exclude = exclude.into();
        }
    }
    if let Some(parse) = map.get("parse") {
        let parse = object(parse, "options.parse")?;
        if let Some(prepare) = parse.get("prepare") {
            let prepare = object(prepare, "options.parse.prepare")?;
            for (name, reference) in prepare {
                if reference.is_null() || reference == &JsonValue::Bool(false) {
                    options.parse.named_prepare.shift_remove(name);
                    continue;
                }
                let reference = reference.as_str().ok_or_else(|| {
                    GrammarError(format!(
                        "Grammar: options.parse.prepare.{name} must be a function reference or null"
                    ))
                })?;
                let callback = refs.parse_prepares.get(reference).cloned().ok_or_else(|| {
                    GrammarError(format!(
                        "Grammar: unknown parse prepare function reference: {reference}"
                    ))
                })?;
                options.parse.named_prepare.insert(name.clone(), callback);
            }
            options.parse.named_prepare.sort_keys();
        }
        if let Some(budget) = parse.get("budget") {
            let budget = object(budget, "options.parse.budget")?;
            if let Some(interval) = budget
                .get("checkEveryN")
                .or_else(|| budget.get("check_every_n"))
            {
                options.parse.budget.check_every_n = interval
                    .as_u64()
                    .and_then(|value| usize::try_from(value).ok())
                    .ok_or_else(|| {
                        GrammarError(
                            "Grammar: options.parse.budget.checkEveryN must be a non-negative integer"
                                .into(),
                        )
                })?;
            }
            if let Some(reference) = budget.get("onCheck").or_else(|| budget.get("on_check")) {
                options.parse.budget.on_check = match reference {
                    JsonValue::Null | JsonValue::Bool(false) => None,
                    JsonValue::String(reference) => Some(
                        refs.budget_checks
                            .get(reference)
                            .cloned()
                            .ok_or_else(|| {
                                GrammarError(format!(
                                    "Grammar: unknown parse budget function reference: {reference}"
                                ))
                            })?,
                    ),
                    _ => {
                        return Err(GrammarError(
                            "Grammar: options.parse.budget.onCheck must be a function reference or null"
                                .into(),
                        ))
                    }
                };
            }
        }
        if let Some(recover) = parse.get("recover") {
            let recover = object(recover, "options.parse.recover")?;
            set_bool(recover, "enabled", &mut options.parse.recover.enabled);
            set_bool(
                recover,
                "popUntilValid",
                &mut options.parse.recover.pop_until_valid,
            );
            if let Some(groups) = recover.get("syncGroups") {
                options.parse.recover.sync_groups = match groups {
                    JsonValue::Null => vec!["close".into(), "comma".into(), "end".into()],
                    JsonValue::Array(groups) => groups
                        .iter()
                        .map(|group| {
                            group.as_str().map(str::to_owned).ok_or_else(|| {
                                GrammarError(
                                    "Grammar: options.parse.recover.syncGroups entries must be strings"
                                        .into(),
                                )
                            })
                        })
                        .collect::<Result<_, _>>()?,
                    _ => {
                        return Err(GrammarError(
                            "Grammar: options.parse.recover.syncGroups must be an array or null"
                                .into(),
                        ))
                    }
                };
            }
            if let Some(tokens) = recover.get("syncTokens") {
                options.parse.recover.sync_tokens = tokens
                    .as_array()
                    .ok_or_else(|| {
                        GrammarError(
                            "Grammar: options.parse.recover.syncTokens must be an array".into(),
                        )
                    })?
                    .iter()
                    .map(|token| {
                        token.as_str().map(str::to_owned).ok_or_else(|| {
                            GrammarError(
                                "Grammar: options.parse.recover.syncTokens entries must be strings"
                                    .into(),
                            )
                        })
                    })
                    .collect::<Result<_, _>>()?;
            }
            for (name, target) in [
                ("maxSkip", &mut options.parse.recover.max_skip),
                ("maxRecoveries", &mut options.parse.recover.max_recoveries),
                ("suppress", &mut options.parse.recover.suppress),
            ] {
                if let Some(value) = recover.get(name) {
                    *target = value
                        .as_u64()
                        .and_then(|value| usize::try_from(value).ok())
                        .ok_or_else(|| {
                            GrammarError(format!(
                                "Grammar: options.parse.recover.{name} must be a non-negative integer"
                            ))
                        })?;
                }
            }
        }
    }
    if let Some(result) = map.get("result") {
        let result = object(result, "options.result")?;
        if let Some(fail) = result.get("fail") {
            options.result.fail = fail
                .as_array()
                .ok_or_else(|| {
                    GrammarError("Grammar: options.result.fail must be an array".into())
                })?
                .iter()
                .map(Value::from_json)
                .collect();
        }
    }
    if let Some(parser) = map.get("parser") {
        let parser = object(parser, "options.parser")?;
        if let Some(reference) = parser.get("start") {
            options.parser.start = match reference {
                JsonValue::Null | JsonValue::Bool(false) => None,
                JsonValue::String(reference) => {
                    Some(refs.parser_starts.get(reference).cloned().ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: unknown parser start function reference: {reference}"
                        ))
                    })?)
                }
                _ => {
                    return Err(GrammarError(
                        "Grammar: options.parser.start must be a function reference or null".into(),
                    ))
                }
            };
        }
    }
    if let Some(fixed_options) = map.get("fixed") {
        let fixed_options = object(fixed_options, "options.fixed")?;
        set_bool(fixed_options, "lex", &mut options.fixed.lex);
        apply_lex_check(
            fixed_options,
            "options.fixed",
            &mut options.fixed.check,
            refs,
        )?;
        if let Some(tokens) = fixed_options.get("token") {
            for (name, source) in object(tokens, "options.fixed.token")? {
                let name = if name.starts_with('#') {
                    name.clone()
                } else {
                    format!("#{name}")
                };
                if source.is_null() {
                    options.fixed.tokens.shift_remove(&name);
                    continue;
                }
                let source = source.as_str().ok_or_else(|| {
                    GrammarError(format!(
                        "Grammar: options.fixed.token.{name} must be a string or null"
                    ))
                })?;
                if matches!(
                    name.as_str(),
                    "#BD"
                        | "#ZZ"
                        | "#UK"
                        | "#AA"
                        | "#SP"
                        | "#LN"
                        | "#CM"
                        | "#NR"
                        | "#ST"
                        | "#TX"
                        | "#VL"
                ) {
                    return Err(GrammarError(format!(
                        "Grammar: {name} is produced by a lexer matcher and cannot be bound to a fixed literal"
                    )));
                }
                let tin = options
                    .fixed
                    .tokens
                    .get(&name)
                    .map(|token| token.tin)
                    .or_else(|| options.token(&name))
                    .unwrap_or_else(|| options.next_tin());
                options.fixed.tokens.insert(
                    name.clone(),
                    crate::options::FixedToken {
                        name,
                        tin,
                        source: source.to_string(),
                    },
                );
            }
        }
    }
    if let Some(match_options) = map.get("match") {
        let match_options = object(match_options, "options.match")?;
        set_bool(match_options, "lex", &mut options.match_lex);
        apply_lex_check(
            match_options,
            "options.match",
            &mut options.match_check,
            refs,
        )?;
        if let Some(tokens) = match_options.get("token") {
            for (name, source) in object(tokens, "options.match.token")? {
                let name = if name.starts_with('#') {
                    name.clone()
                } else {
                    format!("#{name}")
                };
                if source.is_null() {
                    options.match_tokens.shift_remove(&name);
                    continue;
                }
                let source = source.as_str().ok_or_else(|| {
                    GrammarError(format!(
                        "Grammar: options.match.token.{name} must be a serialized regex"
                    ))
                })?;
                let matcher = if serialized_regex(source).is_some() {
                    let (regex, eager) = compile_serialized_regex(
                        source,
                        &format!("options.match.token.{name}"),
                        true,
                    )?;
                    (crate::MatchTokenMatcher::Regex(regex), eager)
                } else if let Some((callback, eager)) = refs.match_tokens.get(source) {
                    (crate::MatchTokenMatcher::Callback(callback.clone()), *eager)
                } else if source.starts_with('@') {
                    return Err(GrammarError(format!(
                        "Grammar: unknown token matcher function reference: {source}"
                    )));
                } else {
                    return Err(GrammarError(format!(
                        "Grammar: options.match.token.{name} must use @/pattern/flags or a registered function reference"
                    )));
                };
                let tin = options
                    .match_tokens
                    .get(&name)
                    .map(|matcher| matcher.tin)
                    .or_else(|| options.token(&name))
                    .unwrap_or_else(|| options.next_tin());
                options.match_tokens.insert(
                    name.clone(),
                    crate::options::MatchToken {
                        name,
                        tin,
                        matcher: matcher.0,
                        eager: matcher.1,
                    },
                );
            }
        }
        if let Some(values) = match_options.get("value") {
            for (name, value) in object(values, "options.match.value")? {
                if value.is_null() || value == &JsonValue::Bool(false) {
                    options.match_values.shift_remove(name);
                    continue;
                }
                let value = object(value, &format!("options.match.value.{name}"))?;
                let source = value
                    .get("match")
                    .and_then(JsonValue::as_str)
                    .ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: options.match.value.{name}.match must be a serialized regex or function reference"
                        ))
                    })?;
                let matcher = if serialized_regex(source).is_some() {
                    let (regex, _) = compile_serialized_regex(
                        source,
                        &format!("options.match.value.{name}.match"),
                        true,
                    )?;
                    crate::MatchTokenMatcher::Regex(regex)
                } else if let Some(callback) = refs.match_values.get(source) {
                    crate::MatchTokenMatcher::Callback(callback.clone())
                } else if source.starts_with('@') {
                    return Err(GrammarError(format!(
                        "Grammar: unknown value matcher function reference: {source}"
                    )));
                } else {
                    return Err(GrammarError(format!(
                        "Grammar: options.match.value.{name}.match must use @/pattern/flags or a registered function reference"
                    )));
                };
                let (val, transform) = resolve_value_producer(value.get("val"), refs);
                options.match_values.insert(
                    name.clone(),
                    crate::MatchValue {
                        name: name.clone(),
                        matcher,
                        val,
                        transform,
                    },
                );
            }
            options.match_values.sort_keys();
        }
    }
    if let Some(token_sets) = map.get("tokenSet").or_else(|| map.get("token_set")) {
        for (name, members) in object(token_sets, "options.tokenSet")? {
            if members.is_null() {
                options.token_set.remove(name.trim_start_matches('#'));
                continue;
            }
            let members = members.as_array().ok_or_else(|| {
                GrammarError(format!("Grammar: options.tokenSet.{name} must be an array"))
            })?;
            let mut tins = Vec::with_capacity(members.len());
            for member in members {
                let member = member.as_str().ok_or_else(|| {
                    GrammarError(format!(
                        "Grammar: options.tokenSet.{name} entries must be strings"
                    ))
                })?;
                let tin = options
                    .token(member)
                    .unwrap_or_else(|| options.register_token(member));
                tins.push(tin);
            }
            options
                .token_set
                .insert(name.trim_start_matches('#').to_string(), tins);
        }
    }

    if let Some(config) = map.get("config") {
        let config = object(config, "options.config")?;
        if let Some(modifiers) = config.get("modify") {
            for (name, reference) in object(modifiers, "options.config.modify")? {
                if reference.is_null() || reference == &JsonValue::Bool(false) {
                    options.config_modify.shift_remove(name);
                    continue;
                }
                let reference = reference.as_str().ok_or_else(|| {
                    GrammarError(format!(
                        "Grammar: options.config.modify.{name} must be a function reference or null"
                    ))
                })?;
                let modifier = refs
                    .config_modifiers
                    .get(reference)
                    .cloned()
                    .ok_or_else(|| {
                        GrammarError(format!(
                            "Grammar: unknown config modifier function reference: {reference}"
                        ))
                    })?;
                options.config_modify.insert(name.clone(), modifier);
            }
        }
    }

    let modifiers: Vec<_> = options
        .config_modify
        .iter()
        .map(|(name, modifier)| (name.clone(), modifier.clone()))
        .collect();
    for (name, modifier) in modifiers {
        std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| modifier.run(options)))
            .map_err(|_| GrammarError(format!("Grammar: config modifier {name} panicked")))?;
    }
    Ok(())
}

fn set_bool(map: &Map<String, JsonValue>, key: &str, target: &mut bool) {
    if let Some(value) = map.get(key).and_then(JsonValue::as_bool) {
        *target = value;
    }
}

fn serialized_regex(source: &str) -> Option<(&str, &str, bool)> {
    let (body, eager) = source
        .strip_prefix("@~/")
        .map(|body| (body, true))
        .or_else(|| source.strip_prefix("@/").map(|body| (body, false)))?;
    let slash = body.rfind('/')?;
    Some((&body[..slash], &body[slash + 1..], eager))
}

fn compile_serialized_regex(
    source: &str,
    label: &str,
    reject_empty: bool,
) -> Result<(regex::Regex, bool), GrammarError> {
    let (pattern, flags, eager) = serialized_regex(source)
        .ok_or_else(|| GrammarError(format!("Grammar: {label} must use @/pattern/flags")))?;
    if flags.contains('v')
        || flags
            .chars()
            .any(|flag| !matches!(flag, 'i' | 'm' | 's' | 'u' | 'g' | 'y' | 'd'))
    {
        return Err(GrammarError(format!(
            "Grammar: unsupported regex flags: {flags}"
        )));
    }
    let mut builder = RegexBuilder::new(pattern);
    builder
        .case_insensitive(flags.contains('i'))
        .multi_line(flags.contains('m'))
        .dot_matches_new_line(flags.contains('s'));
    let regex = builder
        .build()
        .map_err(|error| GrammarError(format!("Grammar: invalid regex for {label}: {error}")))?;
    if reject_empty && regex.is_match("") {
        return Err(GrammarError(format!(
            "Grammar: regex for {label} must not match empty input"
        )));
    }
    Ok((regex, eager))
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
