// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Commutative composition of two configured parser instances.
//!
//! Token numbers belong to one `Tabnas` instance.  Merge therefore moves
//! grammar slots and token sets through token *names*, allocates a fresh
//! deterministic token space, and only then materializes the combined rules.

use crate::options::{
    CommentDef, ConfigModifier, FixedToken, LexCheck, LexMatcher, MatchToken, MatchTokenMatcher,
    MatchValue, TextModifier, ValueDef,
};
use crate::{
    Action, ActionBinding, AltSpec, ContextAction, LexSubscriber, Options, Plugin, PluginError,
    RuleDoneSubscriber, RuleSpec, RuleSubscriber, Tabnas, TokenSubscriber, Value,
};
use indexmap::IndexMap;
use std::collections::{BTreeSet, HashMap};
use std::fmt;
use std::sync::Arc;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MergeError(pub String);

impl fmt::Display for MergeError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl std::error::Error for MergeError {}

fn conflict(path: &str) -> MergeError {
    MergeError(format!("merge: conflicting option values at {path}"))
}

fn pick<T: Clone>(
    left: &T,
    right: &T,
    default: &T,
    path: &str,
    equal: &dyn Fn(&T, &T) -> bool,
) -> Result<T, MergeError> {
    if equal(left, right) {
        Ok(left.clone())
    } else if equal(left, default) {
        Ok(right.clone())
    } else if equal(right, default) {
        Ok(left.clone())
    } else {
        Err(conflict(path))
    }
}

fn pick_without_default<T: Clone>(
    left: &T,
    right: &T,
    path: &str,
    equal: &dyn Fn(&T, &T) -> bool,
) -> Result<T, MergeError> {
    equal(left, right)
        .then(|| left.clone())
        .ok_or_else(|| conflict(path))
}

fn eq_value(left: &Value, right: &Value) -> bool {
    left.deep_equal(right)
}

fn eq_f64(left: &f64, right: &f64) -> bool {
    (left.is_nan() && right.is_nan()) || left.to_bits() == right.to_bits()
}

fn eq_option_arc<T: ?Sized>(left: &Option<Arc<T>>, right: &Option<Arc<T>>) -> bool {
    match (left, right) {
        (None, None) => true,
        (Some(left), Some(right)) => Arc::ptr_eq(left, right),
        _ => false,
    }
}

fn eq_arc_vec<T: ?Sized>(left: &[Arc<T>], right: &[Arc<T>]) -> bool {
    left.len() == right.len()
        && left
            .iter()
            .zip(right)
            .all(|(left, right)| Arc::ptr_eq(left, right))
}

fn eq_text_modifier_vec(left: &[TextModifier], right: &[TextModifier]) -> bool {
    left.len() == right.len()
        && left
            .iter()
            .zip(right)
            .all(|(left, right)| left.same_callback(right))
}

fn eq_prepare_vec(left: &[crate::ParsePrepare], right: &[crate::ParsePrepare]) -> bool {
    left.len() == right.len()
        && left
            .iter()
            .zip(right)
            .all(|(left, right)| left.same_callback(right))
}

fn eq_option_check(left: &Option<LexCheck>, right: &Option<LexCheck>) -> bool {
    match (left, right) {
        (None, None) => true,
        (Some(left), Some(right)) => left.same_callback(right),
        _ => false,
    }
}

fn ordered_keys<T>(left: &IndexMap<String, T>, right: &IndexMap<String, T>) -> Vec<String> {
    let mut keys = left.keys().cloned().collect::<Vec<_>>();
    keys.extend(right.keys().filter(|key| !left.contains_key(*key)).cloned());
    keys
}

fn sorted_hash_keys<T>(left: &HashMap<String, T>, right: &HashMap<String, T>) -> Vec<String> {
    let mut keys = left
        .keys()
        .chain(right.keys())
        .cloned()
        .collect::<BTreeSet<_>>()
        .into_iter()
        .collect::<Vec<_>>();
    keys.shrink_to_fit();
    keys
}

fn merge_index_map<T: Clone>(
    left: &IndexMap<String, T>,
    right: &IndexMap<String, T>,
    default: &IndexMap<String, T>,
    prefix: &str,
    equal: &dyn Fn(&T, &T) -> bool,
) -> Result<IndexMap<String, T>, MergeError> {
    let mut out = IndexMap::new();
    for key in ordered_keys(left, right) {
        let value = match (left.get(&key), right.get(&key), default.get(&key)) {
            (Some(left), Some(right), Some(default)) => Some(pick(
                left,
                right,
                default,
                &format!("{prefix}.{key}"),
                equal,
            )?),
            (Some(left), Some(right), None) => Some(pick_without_default(
                left,
                right,
                &format!("{prefix}.{key}"),
                equal,
            )?),
            // A missing resolved default entry represents an explicit
            // deletion. It wins against the unchanged default, while a
            // deletion and a different replacement are both non-default.
            (Some(value), None, Some(default)) | (None, Some(value), Some(default)) => {
                if equal(value, default) {
                    None
                } else {
                    return Err(conflict(&format!("{prefix}.{key}")));
                }
            }
            (Some(value), None, None) | (None, Some(value), None) => Some(value.clone()),
            (None, None, _) => unreachable!(),
        };
        if let Some(value) = value {
            out.insert(key, value);
        }
    }
    Ok(out)
}

fn merge_hash_map<T: Clone>(
    left: &HashMap<String, T>,
    right: &HashMap<String, T>,
    default: &HashMap<String, T>,
    prefix: &str,
    equal: &dyn Fn(&T, &T) -> bool,
) -> Result<HashMap<String, T>, MergeError> {
    let mut out = HashMap::new();
    for key in sorted_hash_keys(left, right) {
        let value = match (left.get(&key), right.get(&key), default.get(&key)) {
            (Some(left), Some(right), Some(default)) => Some(pick(
                left,
                right,
                default,
                &format!("{prefix}.{key}"),
                equal,
            )?),
            (Some(left), Some(right), None) => Some(pick_without_default(
                left,
                right,
                &format!("{prefix}.{key}"),
                equal,
            )?),
            (Some(value), None, Some(default)) | (None, Some(value), Some(default)) => {
                if equal(value, default) {
                    None
                } else {
                    return Err(conflict(&format!("{prefix}.{key}")));
                }
            }
            (Some(value), None, None) | (None, Some(value), None) => Some(value.clone()),
            (None, None, _) => unreachable!(),
        };
        if let Some(value) = value {
            out.insert(key, value);
        }
    }
    Ok(out)
}

fn merge_char_map(
    left: &HashMap<char, String>,
    right: &HashMap<char, String>,
    default: &HashMap<char, String>,
    prefix: &str,
) -> Result<HashMap<char, String>, MergeError> {
    let keys = left
        .keys()
        .chain(right.keys())
        .copied()
        .collect::<BTreeSet<_>>();
    let mut out = HashMap::new();
    for key in keys {
        let path = format!("{prefix}.{key}");
        let value = match (left.get(&key), right.get(&key), default.get(&key)) {
            (Some(left), Some(right), Some(default)) => {
                Some(pick(left, right, default, &path, &PartialEq::eq)?)
            }
            (Some(left), Some(right), None) => {
                Some(pick_without_default(left, right, &path, &PartialEq::eq)?)
            }
            (Some(value), None, Some(default)) | (None, Some(value), Some(default)) => {
                if value == default {
                    None
                } else {
                    return Err(conflict(&path));
                }
            }
            (Some(value), None, None) | (None, Some(value), None) => Some(value.clone()),
            (None, None, _) => unreachable!(),
        };
        if let Some(value) = value {
            out.insert(key, value);
        }
    }
    Ok(out)
}

fn eq_matcher(left: &MatchTokenMatcher, right: &MatchTokenMatcher) -> bool {
    match (left, right) {
        (MatchTokenMatcher::Regex(left), MatchTokenMatcher::Regex(right)) => {
            left.as_str() == right.as_str()
        }
        (MatchTokenMatcher::Callback(left), MatchTokenMatcher::Callback(right)) => {
            Arc::ptr_eq(left, right)
        }
        _ => false,
    }
}

fn eq_fixed_token(left: &FixedToken, right: &FixedToken) -> bool {
    left.name == right.name && left.source == right.source
}

fn eq_match_token(left: &MatchToken, right: &MatchToken) -> bool {
    left.name == right.name
        && left.eager == right.eager
        && eq_matcher(&left.matcher, &right.matcher)
}

fn eq_match_value(left: &MatchValue, right: &MatchValue) -> bool {
    left.name == right.name
        && eq_matcher(&left.matcher, &right.matcher)
        && match (&left.val, &right.val) {
            (None, None) => true,
            (Some(left), Some(right)) => left.deep_equal(right),
            _ => false,
        }
        && eq_option_arc(&left.transform, &right.transform)
}

fn eq_value_def(left: &ValueDef, right: &ValueDef) -> bool {
    match (&left.val, &right.val) {
        (None, None) => {}
        (Some(left), Some(right)) if left.deep_equal(right) => {}
        _ => return false,
    }
    match (&left.matcher, &right.matcher) {
        (None, None) => {}
        (Some(left), Some(right)) if left.as_str() == right.as_str() => {}
        _ => return false,
    }
    left.consume == right.consume && eq_option_arc(&left.transform, &right.transform)
}

fn eq_comment_def(left: &CommentDef, right: &CommentDef) -> bool {
    left.line == right.line
        && left.start == right.start
        && left.end == right.end
        && left.lex == right.lex
        && left.suffixes == right.suffixes
        && left.eat_line == right.eat_line
        && match (&left.suffix_matcher, &right.suffix_matcher) {
            (None, None) => true,
            (Some(left), Some(right)) => left.same_callback(right),
            _ => false,
        }
}

fn eq_lex_matcher(left: &LexMatcher, right: &LexMatcher) -> bool {
    left.name == right.name
        && eq_f64(&left.order, &right.order)
        && eq_option_arc(&left.matcher, &right.matcher)
        && eq_option_arc(&left.imperative, &right.imperative)
        && eq_option_arc(&left.factory, &right.factory)
}

fn merge_options(left: &Options, right: &Options) -> Result<Options, MergeError> {
    let default = Options::default();
    let mut out = default.clone();

    out.safe.key = pick(
        &left.safe.key,
        &right.safe.key,
        &default.safe.key,
        "safe.key",
        &PartialEq::eq,
    )?;

    out.fixed.lex = pick(
        &left.fixed.lex,
        &right.fixed.lex,
        &default.fixed.lex,
        "fixed.lex",
        &PartialEq::eq,
    )?;
    out.fixed.check = pick(
        &left.fixed.check,
        &right.fixed.check,
        &default.fixed.check,
        "fixed.check",
        &eq_option_check,
    )?;
    out.fixed.tokens = merge_index_map(
        &left.fixed.tokens,
        &right.fixed.tokens,
        &default.fixed.tokens,
        "fixed.token",
        &eq_fixed_token,
    )?;

    out.space.lex = pick(
        &left.space.lex,
        &right.space.lex,
        &default.space.lex,
        "space.lex",
        &PartialEq::eq,
    )?;
    out.space.chars = pick(
        &left.space.chars,
        &right.space.chars,
        &default.space.chars,
        "space.chars",
        &PartialEq::eq,
    )?;
    out.space.check = pick(
        &left.space.check,
        &right.space.check,
        &default.space.check,
        "space.check",
        &eq_option_check,
    )?;

    out.text.lex = pick(
        &left.text.lex,
        &right.text.lex,
        &default.text.lex,
        "text.lex",
        &PartialEq::eq,
    )?;
    out.text.modify = pick(
        &left.text.modify,
        &right.text.modify,
        &default.text.modify,
        "text.modify",
        &|left, right| eq_text_modifier_vec(left, right),
    )?;
    out.text.check = pick(
        &left.text.check,
        &right.text.check,
        &default.text.check,
        "text.check",
        &eq_option_check,
    )?;

    out.number.lex = pick(
        &left.number.lex,
        &right.number.lex,
        &default.number.lex,
        "number.lex",
        &PartialEq::eq,
    )?;
    out.number.hex = pick(
        &left.number.hex,
        &right.number.hex,
        &default.number.hex,
        "number.hex",
        &PartialEq::eq,
    )?;
    out.number.oct = pick(
        &left.number.oct,
        &right.number.oct,
        &default.number.oct,
        "number.oct",
        &PartialEq::eq,
    )?;
    out.number.bin = pick(
        &left.number.bin,
        &right.number.bin,
        &default.number.bin,
        "number.bin",
        &PartialEq::eq,
    )?;
    out.number.sep = pick(
        &left.number.sep,
        &right.number.sep,
        &default.number.sep,
        "number.sep",
        &PartialEq::eq,
    )?;
    out.number.exclude = pick(
        &left.number.exclude,
        &right.number.exclude,
        &default.number.exclude,
        "number.exclude",
        &PartialEq::eq,
    )?;
    out.number.check = pick(
        &left.number.check,
        &right.number.check,
        &default.number.check,
        "number.check",
        &eq_option_check,
    )?;

    out.string.lex = pick(
        &left.string.lex,
        &right.string.lex,
        &default.string.lex,
        "string.lex",
        &PartialEq::eq,
    )?;
    out.string.chars = pick(
        &left.string.chars,
        &right.string.chars,
        &default.string.chars,
        "string.chars",
        &PartialEq::eq,
    )?;
    out.string.multi_chars = pick(
        &left.string.multi_chars,
        &right.string.multi_chars,
        &default.string.multi_chars,
        "string.multiChars",
        &PartialEq::eq,
    )?;
    out.string.escape_char = pick(
        &left.string.escape_char,
        &right.string.escape_char,
        &default.string.escape_char,
        "string.escapeChar",
        &PartialEq::eq,
    )?;
    out.string.escape = merge_char_map(
        &left.string.escape,
        &right.string.escape,
        &default.string.escape,
        "string.escape",
    )?;
    out.string.replace = merge_char_map(
        &left.string.replace,
        &right.string.replace,
        &default.string.replace,
        "string.replace",
    )?;
    out.string.allow_unknown = pick(
        &left.string.allow_unknown,
        &right.string.allow_unknown,
        &default.string.allow_unknown,
        "string.allowUnknown",
        &PartialEq::eq,
    )?;
    out.string.escape_strict = pick(
        &left.string.escape_strict,
        &right.string.escape_strict,
        &default.string.escape_strict,
        "string.escapeStrict",
        &PartialEq::eq,
    )?;
    out.string.allow_control = pick(
        &left.string.allow_control,
        &right.string.allow_control,
        &default.string.allow_control,
        "string.allowControl",
        &PartialEq::eq,
    )?;
    out.string.abandon = pick(
        &left.string.abandon,
        &right.string.abandon,
        &default.string.abandon,
        "string.abandon",
        &PartialEq::eq,
    )?;
    out.string.check = pick(
        &left.string.check,
        &right.string.check,
        &default.string.check,
        "string.check",
        &eq_option_check,
    )?;

    out.line.lex = pick(
        &left.line.lex,
        &right.line.lex,
        &default.line.lex,
        "line.lex",
        &PartialEq::eq,
    )?;
    out.line.chars = pick(
        &left.line.chars,
        &right.line.chars,
        &default.line.chars,
        "line.chars",
        &PartialEq::eq,
    )?;
    out.line.row_chars = pick(
        &left.line.row_chars,
        &right.line.row_chars,
        &default.line.row_chars,
        "line.rowChars",
        &PartialEq::eq,
    )?;
    out.line.single = pick(
        &left.line.single,
        &right.line.single,
        &default.line.single,
        "line.single",
        &PartialEq::eq,
    )?;
    out.line.fixed = pick(
        &left.line.fixed,
        &right.line.fixed,
        &default.line.fixed,
        "line.fixed",
        &PartialEq::eq,
    )?;
    out.line.check = pick(
        &left.line.check,
        &right.line.check,
        &default.line.check,
        "line.check",
        &eq_option_check,
    )?;

    out.comment.lex = pick(
        &left.comment.lex,
        &right.comment.lex,
        &default.comment.lex,
        "comment.lex",
        &PartialEq::eq,
    )?;
    out.comment.check = pick(
        &left.comment.check,
        &right.comment.check,
        &default.comment.check,
        "comment.check",
        &eq_option_check,
    )?;
    out.comment.definitions = merge_index_map(
        &left.comment.definitions,
        &right.comment.definitions,
        &default.comment.definitions,
        "comment.def",
        &eq_comment_def,
    )?;

    out.value.lex = pick(
        &left.value.lex,
        &right.value.lex,
        &default.value.lex,
        "value.lex",
        &PartialEq::eq,
    )?;
    out.value.definitions = merge_index_map(
        &left.value.definitions,
        &right.value.definitions,
        &default.value.definitions,
        "value.def",
        &eq_value_def,
    )?;
    out.ender = pick(
        &left.ender,
        &right.ender,
        &default.ender,
        "ender",
        &PartialEq::eq,
    )?;

    out.map.extend = pick(
        &left.map.extend,
        &right.map.extend,
        &default.map.extend,
        "map.extend",
        &PartialEq::eq,
    )?;
    out.map.merge = pick(
        &left.map.merge,
        &right.map.merge,
        &default.map.merge,
        "map.merge",
        &eq_option_arc,
    )?;
    out.map.child = pick(
        &left.map.child,
        &right.map.child,
        &default.map.child,
        "map.child",
        &PartialEq::eq,
    )?;
    out.map.ordered = pick(
        &left.map.ordered,
        &right.map.ordered,
        &default.map.ordered,
        "map.ordered",
        &PartialEq::eq,
    )?;

    out.list.property = pick(
        &left.list.property,
        &right.list.property,
        &default.list.property,
        "list.property",
        &PartialEq::eq,
    )?;
    out.list.pair = pick(
        &left.list.pair,
        &right.list.pair,
        &default.list.pair,
        "list.pair",
        &PartialEq::eq,
    )?;
    out.list.child = pick(
        &left.list.child,
        &right.list.child,
        &default.list.child,
        "list.child",
        &PartialEq::eq,
    )?;

    out.info.map = pick(
        &left.info.map,
        &right.info.map,
        &default.info.map,
        "info.map",
        &PartialEq::eq,
    )?;
    out.info.list = pick(
        &left.info.list,
        &right.info.list,
        &default.info.list,
        "info.list",
        &PartialEq::eq,
    )?;
    out.info.text = pick(
        &left.info.text,
        &right.info.text,
        &default.info.text,
        "info.text",
        &PartialEq::eq,
    )?;
    out.info.marker = pick(
        &left.info.marker,
        &right.info.marker,
        &default.info.marker,
        "info.marker",
        &PartialEq::eq,
    )?;

    out.lex.empty = pick(
        &left.lex.empty,
        &right.lex.empty,
        &default.lex.empty,
        "lex.empty",
        &PartialEq::eq,
    )?;
    out.lex.empty_result = pick(
        &left.lex.empty_result,
        &right.lex.empty_result,
        &default.lex.empty_result,
        "lex.emptyResult",
        &eq_value,
    )?;
    out.lex.relex = pick(
        &left.lex.relex,
        &right.lex.relex,
        &default.lex.relex,
        "lex.relex",
        &PartialEq::eq,
    )?;
    out.lex.matchers = merge_index_map(
        &left.lex.matchers,
        &right.lex.matchers,
        &default.lex.matchers,
        "lex.match",
        &eq_lex_matcher,
    )?;
    out.lex.matchers.sort_by(|name_a, left, name_b, right| {
        left.order
            .total_cmp(&right.order)
            .then_with(|| name_a.cmp(name_b))
    });

    out.rewind.history = pick(
        &left.rewind.history,
        &right.rewind.history,
        &default.rewind.history,
        "rewind.history",
        &PartialEq::eq,
    )?;
    out.rule.finish = pick(
        &left.rule.finish,
        &right.rule.finish,
        &default.rule.finish,
        "rule.finish",
        &PartialEq::eq,
    )?;
    out.rule.maxmul = pick(
        &left.rule.maxmul,
        &right.rule.maxmul,
        &default.rule.maxmul,
        "rule.maxmul",
        &PartialEq::eq,
    )?;
    out.rule.include = pick(
        &left.rule.include,
        &right.rule.include,
        &default.rule.include,
        "rule.include",
        &PartialEq::eq,
    )?;
    out.rule.exclude = pick(
        &left.rule.exclude,
        &right.rule.exclude,
        &default.rule.exclude,
        "rule.exclude",
        &PartialEq::eq,
    )?;
    out.rule.start = pick(
        &left.rule.start,
        &right.rule.start,
        &default.rule.start,
        "rule.start",
        &PartialEq::eq,
    )?;
    out.result.fail = pick(
        &left.result.fail,
        &right.result.fail,
        &default.result.fail,
        "result.fail",
        &|left, right| {
            left.len() == right.len()
                && left
                    .iter()
                    .zip(right)
                    .all(|(left, right)| left.deep_equal(right))
        },
    )?;
    out.plugin = merge_index_map(
        &left.plugin,
        &right.plugin,
        &default.plugin,
        "plugin",
        &Value::deep_equal,
    )?;

    out.parse.prepare = pick(
        &left.parse.prepare,
        &right.parse.prepare,
        &default.parse.prepare,
        "parse.prepare",
        &|left, right| eq_prepare_vec(left, right),
    )?;
    out.parse.named_prepare = merge_index_map(
        &left.parse.named_prepare,
        &right.parse.named_prepare,
        &default.parse.named_prepare,
        "parse.prepare",
        &crate::ParsePrepare::same_callback,
    )?;
    out.parse.budget.check_every_n = pick(
        &left.parse.budget.check_every_n,
        &right.parse.budget.check_every_n,
        &default.parse.budget.check_every_n,
        "parse.budget.checkEveryN",
        &PartialEq::eq,
    )?;
    out.parse.budget.on_check = pick(
        &left.parse.budget.on_check,
        &right.parse.budget.on_check,
        &default.parse.budget.on_check,
        "parse.budget.onCheck",
        &eq_option_arc,
    )?;
    out.parse.recover.enabled = pick(
        &left.parse.recover.enabled,
        &right.parse.recover.enabled,
        &default.parse.recover.enabled,
        "parse.recover.enabled",
        &PartialEq::eq,
    )?;
    out.parse.recover.sync_groups = pick(
        &left.parse.recover.sync_groups,
        &right.parse.recover.sync_groups,
        &default.parse.recover.sync_groups,
        "parse.recover.syncGroups",
        &PartialEq::eq,
    )?;
    out.parse.recover.sync_tokens = pick(
        &left.parse.recover.sync_tokens,
        &right.parse.recover.sync_tokens,
        &default.parse.recover.sync_tokens,
        "parse.recover.syncTokens",
        &PartialEq::eq,
    )?;
    out.parse.recover.pop_until_valid = pick(
        &left.parse.recover.pop_until_valid,
        &right.parse.recover.pop_until_valid,
        &default.parse.recover.pop_until_valid,
        "parse.recover.popUntilValid",
        &PartialEq::eq,
    )?;
    out.parse.recover.max_skip = pick(
        &left.parse.recover.max_skip,
        &right.parse.recover.max_skip,
        &default.parse.recover.max_skip,
        "parse.recover.maxSkip",
        &PartialEq::eq,
    )?;
    out.parse.recover.max_recoveries = pick(
        &left.parse.recover.max_recoveries,
        &right.parse.recover.max_recoveries,
        &default.parse.recover.max_recoveries,
        "parse.recover.maxRecoveries",
        &PartialEq::eq,
    )?;
    out.parse.recover.suppress = pick(
        &left.parse.recover.suppress,
        &right.parse.recover.suppress,
        &default.parse.recover.suppress,
        "parse.recover.suppress",
        &PartialEq::eq,
    )?;
    out.parser.start = pick(
        &left.parser.start,
        &right.parser.start,
        &default.parser.start,
        "parser.start",
        &eq_option_arc,
    )?;
    out.parser.start_with_instance = pick(
        &left.parser.start_with_instance,
        &right.parser.start_with_instance,
        &default.parser.start_with_instance,
        "parser.start",
        &eq_option_arc,
    )?;

    out.match_lex = pick(
        &left.match_lex,
        &right.match_lex,
        &default.match_lex,
        "match.lex",
        &PartialEq::eq,
    )?;
    out.match_check = pick(
        &left.match_check,
        &right.match_check,
        &default.match_check,
        "match.check",
        &eq_option_check,
    )?;
    out.match_tokens = merge_index_map(
        &left.match_tokens,
        &right.match_tokens,
        &default.match_tokens,
        "match.token",
        &eq_match_token,
    )?;
    out.match_values = merge_index_map(
        &left.match_values,
        &right.match_values,
        &default.match_values,
        "match.value",
        &eq_match_value,
    )?;
    out.error = merge_hash_map(
        &left.error,
        &right.error,
        &default.error,
        "error",
        &PartialEq::eq,
    )?;
    out.hint = merge_hash_map(
        &left.hint,
        &right.hint,
        &default.hint,
        "hint",
        &PartialEq::eq,
    )?;

    out.errmsg.name = pick(
        &left.errmsg.name,
        &right.errmsg.name,
        &default.errmsg.name,
        "errmsg.name",
        &PartialEq::eq,
    )?;
    out.errmsg.suffix = pick(
        &left.errmsg.suffix,
        &right.errmsg.suffix,
        &default.errmsg.suffix,
        "errmsg.suffix",
        &PartialEq::eq,
    )?;
    out.errmsg.link = pick(
        &left.errmsg.link,
        &right.errmsg.link,
        &default.errmsg.link,
        "errmsg.link",
        &PartialEq::eq,
    )?;
    out.color.active = pick(
        &left.color.active,
        &right.color.active,
        &default.color.active,
        "color.active",
        &PartialEq::eq,
    )?;
    out.color.reset = pick(
        &left.color.reset,
        &right.color.reset,
        &default.color.reset,
        "color.reset",
        &PartialEq::eq,
    )?;
    out.color.hi = pick(
        &left.color.hi,
        &right.color.hi,
        &default.color.hi,
        "color.hi",
        &PartialEq::eq,
    )?;
    out.color.lo = pick(
        &left.color.lo,
        &right.color.lo,
        &default.color.lo,
        "color.lo",
        &PartialEq::eq,
    )?;
    out.color.line = pick(
        &left.color.line,
        &right.color.line,
        &default.color.line,
        "color.line",
        &PartialEq::eq,
    )?;
    out.config_modify = merge_index_map(
        &left.config_modify,
        &right.config_modify,
        &default.config_modify,
        "config.modify",
        &ConfigModifier::same_callback,
    )?;

    merge_token_space(left, right, &default, &mut out)?;
    Ok(out)
}

fn token_set_names(options: &Options) -> HashMap<String, Vec<String>> {
    options
        .token_set
        .iter()
        .map(|(name, members)| {
            (
                name.clone(),
                members.iter().map(|tin| options.token_name(*tin)).collect(),
            )
        })
        .collect()
}

fn merge_token_space(
    left: &Options,
    right: &Options,
    default: &Options,
    out: &mut Options,
) -> Result<(), MergeError> {
    let left_sets = token_set_names(left);
    let right_sets = token_set_names(right);
    let default_sets = token_set_names(default);
    let sets = merge_hash_map(
        &left_sets,
        &right_sets,
        &default_sets,
        "tokenSet",
        &PartialEq::eq,
    )?;

    let mut plain_names = left
        .tokens
        .keys()
        .chain(right.tokens.keys())
        .cloned()
        .collect::<BTreeSet<_>>();
    for name in out.fixed.tokens.keys().chain(out.match_tokens.keys()) {
        plain_names.insert(name.clone());
    }
    for members in sets.values() {
        plain_names.extend(members.iter().cloned());
    }

    let mut next_tin = crate::token::TIN_MAX;
    let mut tins = HashMap::new();
    for name in &plain_names {
        let tin = crate::token::name_to_tin(name).unwrap_or_else(|| {
            let allocated = next_tin;
            next_tin += 1;
            allocated
        });
        tins.insert(name.clone(), tin);
    }

    for (name, token) in &mut out.fixed.tokens {
        token.tin = *tins.get(name).expect("fixed token was allocated");
    }
    for (name, token) in &mut out.match_tokens {
        token.tin = *tins.get(name).expect("match token was allocated");
    }
    out.tokens = plain_names
        .iter()
        .map(|name| (name.clone(), tins[name]))
        .collect();
    out.token_set = sets
        .into_iter()
        .map(|(name, members)| {
            let members = members.into_iter().map(|member| tins[&member]).collect();
            (name, members)
        })
        .collect();

    let mut by_source: HashMap<&str, &str> = HashMap::new();
    for (name, token) in &out.fixed.tokens {
        if let Some(existing) = by_source.insert(&token.source, name) {
            if existing != name {
                return Err(MergeError(format!(
                    "merge: fixed tokens {existing} and {name} both claim source {:?}",
                    token.source
                )));
            }
        }
    }
    Ok(())
}

#[derive(Clone, Debug, PartialEq, Eq)]
enum ActionIdentity {
    Simple(usize),
    Context(usize),
    Builtin(String),
    Missing(String),
}

fn erased_ptr<T: ?Sized>(value: &Arc<T>) -> usize {
    Arc::as_ptr(value) as *const () as usize
}

fn action_identity(tabnas: &Tabnas, name: &str) -> ActionIdentity {
    if name.contains('$') {
        ActionIdentity::Builtin(name.to_string())
    } else if let Some(action) = tabnas.actions.get(name) {
        ActionIdentity::Simple(erased_ptr(action))
    } else if let Some(action) = tabnas.context_actions.get(name) {
        ActionIdentity::Context(erased_ptr(action))
    } else {
        ActionIdentity::Missing(name.to_string())
    }
}

fn rename_ref(name: &str, tag: &str) -> String {
    if name.contains('$') {
        name.to_string()
    } else if let Some(name) = name.strip_prefix('@') {
        format!("@{tag}:{name}")
    } else {
        format!("{tag}:{name}")
    }
}

#[derive(Clone)]
struct PortableAlt {
    alt: AltSpec,
    slots: Vec<Vec<String>>,
    keys: Vec<String>,
    complexity: [usize; 10],
    group: String,
    tag: String,
    action_identity: Vec<ActionIdentity>,
}

fn portable_alt(tabnas: &Tabnas, alt: &AltSpec, tag: &str) -> PortableAlt {
    let slots = alt
        .s
        .iter()
        .map(|slot| {
            slot.iter()
                .map(|tin| tabnas.options.token_name(*tin))
                .collect::<Vec<_>>()
        })
        .collect::<Vec<_>>();
    let keys = slots
        .iter()
        .map(|slot| {
            let mut names = slot.clone();
            names.sort();
            names.join(" ")
        })
        .collect();
    let mut cloned = alt.clone();
    let ordered_actions =
        crate::rule::resolved_action_order(&alt.a, &alt.action_fns, &alt.action_order);
    let action_identity = ordered_actions
        .iter()
        .map(|binding| match binding {
            ActionBinding::Named(name) => action_identity(tabnas, name),
            ActionBinding::Callback(callback) => ActionIdentity::Context(erased_ptr(callback)),
        })
        .collect();
    cloned.a = alt.a.iter().map(|name| rename_ref(name, tag)).collect();
    cloned.action_configs = alt
        .action_configs
        .iter()
        .map(|(name, value)| (rename_ref(name, tag), value.clone()))
        .collect();
    cloned.c_ref = alt.c_ref.as_ref().map(|name| rename_ref(name, tag));
    cloned.action_order = ordered_actions
        .into_iter()
        .map(|binding| match binding {
            ActionBinding::Named(name) => ActionBinding::Named(rename_ref(&name, tag)),
            ActionBinding::Callback(callback) => ActionBinding::Callback(callback),
        })
        .collect();
    cloned.s.clear();
    PortableAlt {
        alt: cloned,
        slots,
        keys,
        complexity: [
            usize::from(!alt.c.is_empty() || alt.c_fn.is_some() || alt.c_lex.is_some()),
            usize::from(alt.e.is_some()),
            usize::from(alt.h.is_some()),
            usize::from(alt.b != 0 || alt.b_fn.is_some()),
            alt.n.len(),
            usize::from(!alt.a.is_empty() || !alt.action_fns.is_empty()),
            usize::from(!alt.u.is_empty()),
            usize::from(!alt.k.is_empty()),
            usize::from(alt.p.is_some() || alt.p_fn.is_some()),
            usize::from(alt.r.is_some() || alt.r_fn.is_some()),
        ],
        group: alt.g.clone(),
        tag: tag.to_string(),
        action_identity,
    }
}

fn compare_alts(left: &PortableAlt, right: &PortableAlt) -> std::cmp::Ordering {
    for (left, right) in left.keys.iter().zip(&right.keys) {
        let ordering = left.cmp(right);
        if !ordering.is_eq() {
            return ordering;
        }
    }
    if left.keys.len() != right.keys.len() {
        return right.keys.len().cmp(&left.keys.len());
    }
    for (left, right) in left.complexity.iter().zip(right.complexity) {
        let ordering = right.cmp(left);
        if !ordering.is_eq() {
            return ordering;
        }
    }
    left.group
        .cmp(&right.group)
        .then_with(|| left.tag.cmp(&right.tag))
}

fn eq_value_hash(left: &HashMap<String, Value>, right: &HashMap<String, Value>) -> bool {
    left.len() == right.len()
        && left
            .iter()
            .all(|(key, value)| right.get(key).is_some_and(|other| value.deep_equal(other)))
}

fn eq_conditions(left: &[crate::Condition], right: &[crate::Condition]) -> bool {
    left.len() == right.len()
        && left.iter().zip(right).all(|(left, right)| {
            left.path == right.path && left.op == right.op && left.value.deep_equal(&right.value)
        })
}

fn identical_alts(left: &PortableAlt, right: &PortableAlt) -> bool {
    let left_alt = &left.alt;
    let right_alt = &right.alt;
    left.keys == right.keys
        && left.group == right.group
        && left.action_identity == right.action_identity
        && eq_arc_vec(&left_alt.action_fns, &right_alt.action_fns)
        && eq_conditions(&left_alt.c, &right_alt.c)
        && eq_option_arc(&left_alt.c_fn, &right_alt.c_fn)
        && eq_option_arc(&left_alt.c_lex, &right_alt.c_lex)
        && eq_option_arc(&left_alt.h, &right_alt.h)
        && eq_option_arc(&left_alt.e, &right_alt.e)
        && left_alt.b == right_alt.b
        && eq_option_arc(&left_alt.b_fn, &right_alt.b_fn)
        && left_alt.p == right_alt.p
        && eq_option_arc(&left_alt.p_fn, &right_alt.p_fn)
        && left_alt.r == right_alt.r
        && eq_option_arc(&left_alt.r_fn, &right_alt.r_fn)
        && left_alt.n == right_alt.n
        && eq_value_hash(&left_alt.u, &right_alt.u)
        && eq_value_hash(&left_alt.k, &right_alt.k)
        && eq_value_hash(&left_alt.action_configs, &right_alt.action_configs)
}

fn interleave(left: Vec<PortableAlt>, right: Vec<PortableAlt>) -> Vec<PortableAlt> {
    let right = right
        .into_iter()
        .filter(|candidate| {
            !left
                .iter()
                .any(|existing| identical_alts(existing, candidate))
        })
        .collect::<Vec<_>>();
    let mut out = Vec::with_capacity(left.len() + right.len());
    let (mut left_i, mut right_i) = (0, 0);
    while left_i < left.len() && right_i < right.len() {
        if compare_alts(&left[left_i], &right[right_i]).is_le() {
            out.push(left[left_i].clone());
            left_i += 1;
        } else {
            out.push(right[right_i].clone());
            right_i += 1;
        }
    }
    out.extend(left[left_i..].iter().cloned());
    out.extend(right[right_i..].iter().cloned());
    out
}

fn append_callbacks<T: ?Sized>(left: &[Arc<T>], right: &[Arc<T>]) -> Vec<Arc<T>> {
    left.iter().chain(right).cloned().collect()
}

#[derive(Clone)]
struct MergedRule {
    name: String,
    open: Vec<PortableAlt>,
    close: Vec<PortableAlt>,
    bo: Vec<String>,
    ao: Vec<String>,
    bc: Vec<String>,
    ac: Vec<String>,
    bo_fns: Vec<ContextAction>,
    ao_fns: Vec<ContextAction>,
    bc_fns: Vec<ContextAction>,
    ac_fns: Vec<ContextAction>,
    bo_order: Vec<ActionBinding>,
    ao_order: Vec<ActionBinding>,
    bc_order: Vec<ActionBinding>,
    ac_order: Vec<ActionBinding>,
}

impl MergedRule {
    fn materialize(&self, options: &mut Options) -> RuleSpec {
        fn alts(alts: &[PortableAlt], options: &mut Options) -> Vec<AltSpec> {
            alts.iter()
                .map(|portable| {
                    let mut alt = portable.alt.clone();
                    alt.s = portable
                        .slots
                        .iter()
                        .map(|slot| {
                            slot.iter()
                                .map(|name| options.register_token(name))
                                .collect()
                        })
                        .collect();
                    alt
                })
                .collect()
        }
        RuleSpec {
            name: self.name.clone(),
            open: alts(&self.open, options),
            close: alts(&self.close, options),
            bo: self.bo.clone(),
            ao: self.ao.clone(),
            bc: self.bc.clone(),
            ac: self.ac.clone(),
            bo_fns: self.bo_fns.clone(),
            ao_fns: self.ao_fns.clone(),
            bc_fns: self.bc_fns.clone(),
            ac_fns: self.ac_fns.clone(),
            bo_order: self.bo_order.clone(),
            ao_order: self.ao_order.clone(),
            bc_order: self.bc_order.clone(),
            ac_order: self.ac_order.clone(),
        }
    }
}

struct PhaseSource<'a> {
    tabnas: &'a Tabnas,
    named: &'a [String],
    callbacks: &'a [ContextAction],
    order: &'a [ActionBinding],
    tag: &'a str,
}

fn merge_phase(
    left: PhaseSource<'_>,
    right: PhaseSource<'_>,
) -> (Vec<String>, Vec<ContextAction>, Vec<ActionBinding>) {
    fn portable(
        tabnas: &Tabnas,
        named: &[String],
        callbacks: &[ContextAction],
        order: &[ActionBinding],
        tag: &str,
    ) -> Vec<(ActionBinding, ActionIdentity)> {
        crate::rule::resolved_action_order(named, callbacks, order)
            .into_iter()
            .map(|binding| match binding {
                ActionBinding::Named(name) => (
                    ActionBinding::Named(rename_ref(&name, tag)),
                    action_identity(tabnas, &name),
                ),
                ActionBinding::Callback(callback) => {
                    let identity = ActionIdentity::Context(erased_ptr(&callback));
                    (ActionBinding::Callback(callback), identity)
                }
            })
            .collect()
    }

    let mut order = portable(
        left.tabnas,
        left.named,
        left.callbacks,
        left.order,
        left.tag,
    );
    for candidate in portable(
        right.tabnas,
        right.named,
        right.callbacks,
        right.order,
        right.tag,
    ) {
        if !order.iter().any(|(_, identity)| *identity == candidate.1) {
            order.push(candidate);
        }
    }
    let order = order
        .into_iter()
        .map(|(binding, _)| binding)
        .collect::<Vec<_>>();
    let named = order
        .iter()
        .filter_map(|binding| match binding {
            ActionBinding::Named(name) => Some(name.clone()),
            ActionBinding::Callback(_) => None,
        })
        .collect();
    let callbacks = order
        .iter()
        .filter_map(|binding| match binding {
            ActionBinding::Named(_) => None,
            ActionBinding::Callback(callback) => Some(callback.clone()),
        })
        .collect();
    (named, callbacks, order)
}

fn merged_rules(left: &Tabnas, left_tag: &str, right: &Tabnas, right_tag: &str) -> Vec<MergedRule> {
    let names = left
        .rules
        .keys()
        .chain(right.rules.keys())
        .cloned()
        .collect::<BTreeSet<_>>();
    names
        .into_iter()
        .map(|name| {
            let left_rule = left.rules.get(&name);
            let right_rule = right.rules.get(&name);
            let empty_named = Vec::new();
            let empty_callbacks = Vec::new();
            let empty_order = Vec::new();
            let left_open = left_rule
                .map(|rule| {
                    rule.open
                        .iter()
                        .map(|alt| portable_alt(left, alt, left_tag))
                        .collect()
                })
                .unwrap_or_default();
            let right_open = right_rule
                .map(|rule| {
                    rule.open
                        .iter()
                        .map(|alt| portable_alt(right, alt, right_tag))
                        .collect()
                })
                .unwrap_or_default();
            let left_close = left_rule
                .map(|rule| {
                    rule.close
                        .iter()
                        .map(|alt| portable_alt(left, alt, left_tag))
                        .collect()
                })
                .unwrap_or_default();
            let right_close = right_rule
                .map(|rule| {
                    rule.close
                        .iter()
                        .map(|alt| portable_alt(right, alt, right_tag))
                        .collect()
                })
                .unwrap_or_default();
            let phase =
                |select_named: fn(&RuleSpec) -> &Vec<String>,
                 select_callbacks: fn(&RuleSpec) -> &Vec<ContextAction>,
                 select_order: fn(&RuleSpec) -> &Vec<ActionBinding>| {
                    merge_phase(
                        PhaseSource {
                            tabnas: left,
                            named: left_rule.map(select_named).unwrap_or(&empty_named),
                            callbacks: left_rule.map(select_callbacks).unwrap_or(&empty_callbacks),
                            order: left_rule.map(select_order).unwrap_or(&empty_order),
                            tag: left_tag,
                        },
                        PhaseSource {
                            tabnas: right,
                            named: right_rule.map(select_named).unwrap_or(&empty_named),
                            callbacks: right_rule.map(select_callbacks).unwrap_or(&empty_callbacks),
                            order: right_rule.map(select_order).unwrap_or(&empty_order),
                            tag: right_tag,
                        },
                    )
                };
            let (bo, bo_fns, bo_order) =
                phase(|rule| &rule.bo, |rule| &rule.bo_fns, |rule| &rule.bo_order);
            let (ao, ao_fns, ao_order) =
                phase(|rule| &rule.ao, |rule| &rule.ao_fns, |rule| &rule.ao_order);
            let (bc, bc_fns, bc_order) =
                phase(|rule| &rule.bc, |rule| &rule.bc_fns, |rule| &rule.bc_order);
            let (ac, ac_fns, ac_order) =
                phase(|rule| &rule.ac, |rule| &rule.ac_fns, |rule| &rule.ac_order);
            MergedRule {
                name,
                open: interleave(left_open, right_open),
                close: interleave(left_close, right_close),
                bo,
                ao,
                bc,
                ac,
                bo_fns,
                ao_fns,
                bc_fns,
                ac_fns,
                bo_order,
                ao_order,
                bc_order,
                ac_order,
            }
        })
        .collect()
}

fn prefix_hash_map<T: Clone>(source: &HashMap<String, T>, tag: &str) -> HashMap<String, T> {
    source
        .iter()
        .map(|(name, value)| (rename_ref(name, tag), value.clone()))
        .collect()
}

fn combine_prefixed<T: Clone>(
    left: &HashMap<String, T>,
    left_tag: &str,
    right: &HashMap<String, T>,
    right_tag: &str,
) -> HashMap<String, T> {
    let mut out = prefix_hash_map(left, left_tag);
    out.extend(prefix_hash_map(right, right_tag));
    out
}

#[derive(Clone)]
struct MergedInstall {
    rules: Vec<MergedRule>,
    actions: HashMap<String, Action>,
    context_actions: HashMap<String, ContextAction>,
    token_subscribers: Vec<TokenSubscriber>,
    lex_subscribers: Vec<LexSubscriber>,
    rule_subscribers: Vec<RuleSubscriber>,
    rule_done_subscribers: Vec<RuleDoneSubscriber>,
    alt_conditions: HashMap<String, crate::AltCondition>,
    alt_lexer_conditions: HashMap<String, crate::AltConditionWithLexer>,
    alt_modifiers: HashMap<String, crate::AltModifier>,
    alt_errors: HashMap<String, crate::AltError>,
    alt_pushes: HashMap<String, crate::AltNext>,
    alt_replaces: HashMap<String, crate::AltNext>,
    alt_backtracks: HashMap<String, crate::AltBack>,
    match_token_refs: HashMap<String, (crate::MatchTokenCallback, bool)>,
    value_transform_refs: HashMap<String, crate::ValueTransform>,
    text_modifier_refs: HashMap<String, crate::TextModifier>,
    lex_check_refs: HashMap<String, crate::LexCheck>,
    comment_suffix_refs: HashMap<String, crate::CommentSuffixMatcher>,
    match_value_refs: HashMap<String, crate::MatchTokenCallback>,
    parse_prepare_refs: HashMap<String, crate::ParsePrepare>,
    budget_check_refs: HashMap<String, crate::BudgetCheck>,
    lex_match_refs: HashMap<String, crate::LexMatcherCallback>,
    imperative_lex_match_refs: HashMap<String, crate::ImperativeLexMatcher>,
    lex_match_factory_refs: HashMap<String, crate::LexMatcherFactory>,
    error_suffix_refs: HashMap<String, crate::ErrorSuffixCallback>,
    config_modifier_refs: HashMap<String, crate::ConfigModifier>,
    parser_start_refs: HashMap<String, crate::ParserStart>,
    parser_start_instance_refs: HashMap<String, crate::ParserStartWithInstance>,
    map_merge_refs: HashMap<String, crate::MapMerge>,
}

impl MergedInstall {
    fn install(&self, tabnas: &mut Tabnas) {
        tabnas.rules = self
            .rules
            .iter()
            .map(|rule| {
                let rule = rule.materialize(&mut tabnas.options);
                (rule.name.clone(), rule)
            })
            .collect();
        tabnas.actions = self.actions.clone();
        tabnas.context_actions = self.context_actions.clone();
        tabnas.token_subscribers = self.token_subscribers.clone();
        tabnas.lex_subscribers = self.lex_subscribers.clone();
        tabnas.rule_subscribers = self.rule_subscribers.clone();
        tabnas.rule_done_subscribers = self.rule_done_subscribers.clone();
        tabnas.alt_conditions = self.alt_conditions.clone();
        tabnas.alt_lexer_conditions = self.alt_lexer_conditions.clone();
        tabnas.alt_modifiers = self.alt_modifiers.clone();
        tabnas.alt_errors = self.alt_errors.clone();
        tabnas.alt_pushes = self.alt_pushes.clone();
        tabnas.alt_replaces = self.alt_replaces.clone();
        tabnas.alt_backtracks = self.alt_backtracks.clone();
        tabnas.match_token_refs = self.match_token_refs.clone();
        tabnas.value_transform_refs = self.value_transform_refs.clone();
        tabnas.text_modifier_refs = self.text_modifier_refs.clone();
        tabnas.lex_check_refs = self.lex_check_refs.clone();
        tabnas.comment_suffix_refs = self.comment_suffix_refs.clone();
        tabnas.match_value_refs = self.match_value_refs.clone();
        tabnas.parse_prepare_refs = self.parse_prepare_refs.clone();
        tabnas.budget_check_refs = self.budget_check_refs.clone();
        tabnas.lex_match_refs = self.lex_match_refs.clone();
        tabnas.imperative_lex_match_refs = self.imperative_lex_match_refs.clone();
        tabnas.lex_match_factory_refs = self.lex_match_factory_refs.clone();
        tabnas.error_suffix_refs = self.error_suffix_refs.clone();
        tabnas.config_modifier_refs = self.config_modifier_refs.clone();
        tabnas.parser_start_refs = self.parser_start_refs.clone();
        tabnas.parser_start_instance_refs = self.parser_start_instance_refs.clone();
        tabnas.map_merge_refs = self.map_merge_refs.clone();
    }
}

fn merge_plugin_value(left: &Value, right: &Value, path: &str) -> Result<Value, MergeError> {
    match (left, right) {
        (Value::Undefined, value) | (value, Value::Undefined) => Ok(value.clone()),
        (Value::Object(left), Value::Object(right)) => {
            let mut out = IndexMap::new();
            for key in ordered_keys(left, right) {
                let value = match (left.get(&key), right.get(&key)) {
                    (Some(left), Some(right)) => {
                        merge_plugin_value(left, right, &format!("{path}.{key}"))?
                    }
                    (Some(value), None) | (None, Some(value)) => value.clone(),
                    (None, None) => unreachable!(),
                };
                out.insert(key, value);
            }
            Ok(Value::Object(out))
        }
        _ if left.deep_equal(right) => Ok(left.clone()),
        _ => Err(conflict(path)),
    }
}

pub(crate) fn merge(left: &Tabnas, right: &Tabnas) -> Result<Tabnas, MergeError> {
    fn tag<'a>(tabnas: &'a Tabnas, which: &str) -> Result<&'a str, MergeError> {
        let tag = tabnas.options.tag.as_str();
        if tag.is_empty() || tag == "-" {
            Err(MergeError(format!(
                "merge: the {which} instance needs a tag option (used to prefix its named actions)"
            )))
        } else {
            Ok(tag)
        }
    }

    let left_tag = tag(left, "first")?;
    let right_tag = tag(right, "second")?;
    if left_tag == right_tag {
        return Err(MergeError(format!(
            "merge: instance tags must differ, both are {left_tag:?}"
        )));
    }
    let (left, left_tag, right, right_tag) = if left_tag < right_tag {
        (left, left_tag, right, right_tag)
    } else {
        (right, right_tag, left, left_tag)
    };

    let mut options = merge_options(&left.options, &right.options)?;
    options.tag = format!("{left_tag}~{right_tag}");
    let plugin_options = merge_plugin_value(
        &Value::Object(left.plugin_options.clone()),
        &Value::Object(right.plugin_options.clone()),
        "plugin",
    )?;
    let Value::Object(plugin_options) = plugin_options else {
        unreachable!()
    };

    let install = MergedInstall {
        rules: merged_rules(left, left_tag, right, right_tag),
        actions: combine_prefixed(&left.actions, left_tag, &right.actions, right_tag),
        context_actions: combine_prefixed(
            &left.context_actions,
            left_tag,
            &right.context_actions,
            right_tag,
        ),
        token_subscribers: append_callbacks(&left.token_subscribers, &right.token_subscribers),
        lex_subscribers: append_callbacks(&left.lex_subscribers, &right.lex_subscribers),
        rule_subscribers: append_callbacks(&left.rule_subscribers, &right.rule_subscribers),
        rule_done_subscribers: append_callbacks(
            &left.rule_done_subscribers,
            &right.rule_done_subscribers,
        ),
        alt_conditions: combine_prefixed(
            &left.alt_conditions,
            left_tag,
            &right.alt_conditions,
            right_tag,
        ),
        alt_lexer_conditions: combine_prefixed(
            &left.alt_lexer_conditions,
            left_tag,
            &right.alt_lexer_conditions,
            right_tag,
        ),
        alt_modifiers: combine_prefixed(
            &left.alt_modifiers,
            left_tag,
            &right.alt_modifiers,
            right_tag,
        ),
        alt_errors: combine_prefixed(&left.alt_errors, left_tag, &right.alt_errors, right_tag),
        alt_pushes: combine_prefixed(&left.alt_pushes, left_tag, &right.alt_pushes, right_tag),
        alt_replaces: combine_prefixed(
            &left.alt_replaces,
            left_tag,
            &right.alt_replaces,
            right_tag,
        ),
        alt_backtracks: combine_prefixed(
            &left.alt_backtracks,
            left_tag,
            &right.alt_backtracks,
            right_tag,
        ),
        match_token_refs: combine_prefixed(
            &left.match_token_refs,
            left_tag,
            &right.match_token_refs,
            right_tag,
        ),
        value_transform_refs: combine_prefixed(
            &left.value_transform_refs,
            left_tag,
            &right.value_transform_refs,
            right_tag,
        ),
        text_modifier_refs: combine_prefixed(
            &left.text_modifier_refs,
            left_tag,
            &right.text_modifier_refs,
            right_tag,
        ),
        lex_check_refs: combine_prefixed(
            &left.lex_check_refs,
            left_tag,
            &right.lex_check_refs,
            right_tag,
        ),
        comment_suffix_refs: combine_prefixed(
            &left.comment_suffix_refs,
            left_tag,
            &right.comment_suffix_refs,
            right_tag,
        ),
        match_value_refs: combine_prefixed(
            &left.match_value_refs,
            left_tag,
            &right.match_value_refs,
            right_tag,
        ),
        parse_prepare_refs: combine_prefixed(
            &left.parse_prepare_refs,
            left_tag,
            &right.parse_prepare_refs,
            right_tag,
        ),
        budget_check_refs: combine_prefixed(
            &left.budget_check_refs,
            left_tag,
            &right.budget_check_refs,
            right_tag,
        ),
        lex_match_refs: combine_prefixed(
            &left.lex_match_refs,
            left_tag,
            &right.lex_match_refs,
            right_tag,
        ),
        imperative_lex_match_refs: combine_prefixed(
            &left.imperative_lex_match_refs,
            left_tag,
            &right.imperative_lex_match_refs,
            right_tag,
        ),
        lex_match_factory_refs: combine_prefixed(
            &left.lex_match_factory_refs,
            left_tag,
            &right.lex_match_factory_refs,
            right_tag,
        ),
        error_suffix_refs: combine_prefixed(
            &left.error_suffix_refs,
            left_tag,
            &right.error_suffix_refs,
            right_tag,
        ),
        config_modifier_refs: combine_prefixed(
            &left.config_modifier_refs,
            left_tag,
            &right.config_modifier_refs,
            right_tag,
        ),
        parser_start_refs: combine_prefixed(
            &left.parser_start_refs,
            left_tag,
            &right.parser_start_refs,
            right_tag,
        ),
        parser_start_instance_refs: combine_prefixed(
            &left.parser_start_instance_refs,
            left_tag,
            &right.parser_start_instance_refs,
            right_tag,
        ),
        map_merge_refs: combine_prefixed(
            &left.map_merge_refs,
            left_tag,
            &right.map_merge_refs,
            right_tag,
        ),
    };

    let mut out = Tabnas::with_options(options);
    out.plugin_options = plugin_options;
    let install_callback = install.clone();
    out.use_plugin(
        Plugin::new("merged", move |tabnas, _| {
            install_callback.install(tabnas);
            Ok(())
        }),
        None,
    )
    .map_err(|PluginError(message)| MergeError(format!("merge: {message}")))?;
    Ok(out)
}
