// Copyright (c) 2013-2026 Richard Rodger, MIT License

//! Cross-runtime utility primitives used by grammar and option handling.

use serde_json::{Map, Value};

const DANGEROUS_KEYS: [&str; 3] = ["__proto__", "constructor", "prototype"];

/// Recursively merge JSON data. Objects merge by key, arrays by index, and
/// unlike container kinds or scalar values are replaced by the overlay.
pub fn deep(mut base: Value, overlays: impl IntoIterator<Item = Value>) -> Value {
    for overlay in overlays {
        base = deep_one(base, overlay);
    }
    base
}

fn deep_one(base: Value, overlay: Value) -> Value {
    match (base, overlay) {
        (Value::Object(mut base), Value::Object(overlay)) => {
            for (key, value) in overlay {
                if DANGEROUS_KEYS.contains(&key.as_str()) {
                    continue;
                }
                let merged = match base.shift_remove(&key) {
                    Some(previous) => deep_one(previous, value),
                    None => clone_safe(value),
                };
                base.insert(key, merged);
            }
            Value::Object(base)
        }
        (Value::Array(base), Value::Array(overlay)) => {
            let length = base.len().max(overlay.len());
            let mut base = base.into_iter();
            let mut overlay = overlay.into_iter();
            Value::Array(
                (0..length)
                    .map(|_| match (base.next(), overlay.next()) {
                        (Some(base), Some(overlay)) => deep_one(base, overlay),
                        (Some(base), None) => clone_safe(base),
                        (None, Some(overlay)) => clone_safe(overlay),
                        (None, None) => unreachable!("length is derived from both iterators"),
                    })
                    .collect(),
            )
        }
        (_, overlay) => clone_safe(overlay),
    }
}

fn clone_safe(value: Value) -> Value {
    match value {
        Value::Object(map) => Value::Object(
            map.into_iter()
                .filter(|(key, _)| !DANGEROUS_KEYS.contains(&key.as_str()))
                .map(|(key, value)| (key, clone_safe(value)))
                .collect(),
        ),
        Value::Array(values) => Value::Array(values.into_iter().map(clone_safe).collect()),
        scalar => scalar,
    }
}

/// Render JSON-like data to at most `max_len` characters, marking truncation
/// with dots using the canonical Tabnas behavior.
pub fn str_value(value: &Value, max_len: i64) -> String {
    if max_len <= 0 {
        return String::new();
    }
    let rendered = match value {
        Value::String(value) => value.clone(),
        _ => serde_json::to_string(value).unwrap_or_default(),
    };
    let chars: Vec<char> = rendered.chars().collect();
    let max_len = max_len as usize;
    let output = if chars.len() > max_len {
        if max_len >= 4 {
            chars[..max_len - 3].iter().collect::<String>() + "..."
        } else {
            ".".repeat(max_len)
        }
    } else {
        rendered
    };
    output
        .chars()
        .take(max_len)
        .map(|value| match value {
            '\r' | '\n' | '\t' => '.',
            value => value,
        })
        .collect()
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ListMods {
    pub delete: Vec<isize>,
    pub move_items: Vec<isize>,
}

/// Apply canonical delete-then-move list modifications.
pub fn modlist<T>(list: Vec<T>, mods: Option<&ListMods>) -> Vec<T> {
    let Some(mods) = mods else { return list };
    let mut list: Vec<Option<T>> = list.into_iter().map(Some).collect();
    for raw in &mods.delete {
        let index = if *raw < 0 {
            list.len().checked_sub(raw.unsigned_abs())
        } else {
            Some(*raw as usize)
        };
        if let Some(item) = index.and_then(|index| list.get_mut(index)) {
            *item = None;
        }
    }
    for pair in mods.move_items.chunks_exact(2) {
        if list.is_empty() {
            break;
        }
        let length = list.len() as isize;
        let from = pair[0].rem_euclid(length) as usize;
        let to = pair[1].rem_euclid(length) as usize;
        let item = list.remove(from);
        list.insert(to, item);
    }
    list.into_iter().flatten().collect()
}

/// Substitute `{a.b.0}` paths from an object or array. Missing paths retain
/// their placeholder, matching the TypeScript and Go utilities.
pub fn str_inject(template: &str, values: Option<&Value>) -> String {
    let Some(values @ (Value::Object(_) | Value::Array(_))) = values else {
        return template.to_string();
    };
    let mut output = String::with_capacity(template.len());
    let mut rest = template;
    while let Some(open) = rest.find('{') {
        output.push_str(&rest[..open]);
        let after_open = &rest[open + 1..];
        let Some(close) = after_open.find('}') else {
            output.push_str(&rest[open..]);
            return output;
        };
        let path = &after_open[..close];
        if let Some(value) = resolve_path(values, path) {
            output.push_str(&compact(value));
        } else {
            output.push_str(&rest[open..open + close + 2]);
        }
        rest = &after_open[close + 1..];
    }
    output.push_str(rest);
    output
}

fn resolve_path<'a>(mut value: &'a Value, path: &str) -> Option<&'a Value> {
    for part in path.split('.') {
        value = match value {
            Value::Object(map) => map.get(part)?,
            Value::Array(array) => array.get(part.parse::<usize>().ok()?)?,
            _ => return None,
        };
    }
    Some(value)
}

fn compact(value: &Value) -> String {
    match value {
        Value::Null => "null".into(),
        Value::Bool(value) => value.to_string(),
        Value::Number(value) => value.to_string(),
        Value::String(value) => value.clone(),
        Value::Array(values) => {
            format!(
                "[{}]",
                values.iter().map(compact).collect::<Vec<_>>().join(",")
            )
        }
        Value::Object(values) => compact_object(values),
    }
}

fn compact_object(values: &Map<String, Value>) -> String {
    let fields = values
        .iter()
        .map(|(key, value)| format!("{key}:{}", compact(value)))
        .collect::<Vec<_>>()
        .join(",");
    format!("{{{fields}}}")
}
