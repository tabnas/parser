// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::value::Value;
use crate::{Context, Rule};
use std::collections::HashMap;
use std::fmt;
use std::sync::Arc;

type TokenValCallback = dyn Fn(&mut Rule, &mut Context) -> Value + Send + Sync;

/// Tin is a token identification number.
pub type Tin = i32;

pub const TIN_BD: Tin = 1; // #BD - BAD
pub const TIN_ZZ: Tin = 2; // #ZZ - END
pub const TIN_UK: Tin = 3; // #UK - UNKNOWN
pub const TIN_AA: Tin = 4; // #AA - ANY
pub const TIN_SP: Tin = 5; // #SP - SPACE
pub const TIN_LN: Tin = 6; // #LN - LINE
pub const TIN_CM: Tin = 7; // #CM - COMMENT
pub const TIN_NR: Tin = 8; // #NR - NUMBER
pub const TIN_ST: Tin = 9; // #ST - STRING
pub const TIN_TX: Tin = 10; // #TX - TEXT
pub const TIN_VL: Tin = 11; // #VL - VALUE (true, false, null)
pub const TIN_OB: Tin = 12; // #OB - Open Brace {
pub const TIN_CB: Tin = 13; // #CB - Close Brace }
pub const TIN_OS: Tin = 14; // #OS - Open Square [
pub const TIN_CS: Tin = 15; // #CS - Close Square ]
pub const TIN_CL: Tin = 16; // #CL - Colon :
pub const TIN_CA: Tin = 17; // #CA - Comma ,
pub const TIN_MAX: Tin = 18;

pub fn tin_name(tin: Tin) -> &'static str {
    match tin {
        TIN_BD => "#BD",
        TIN_ZZ => "#ZZ",
        TIN_UK => "#UK",
        TIN_AA => "#AA",
        TIN_SP => "#SP",
        TIN_LN => "#LN",
        TIN_CM => "#CM",
        TIN_NR => "#NR",
        TIN_ST => "#ST",
        TIN_TX => "#TX",
        TIN_VL => "#VL",
        TIN_OB => "#OB",
        TIN_CB => "#CB",
        TIN_OS => "#OS",
        TIN_CS => "#CS",
        TIN_CL => "#CL",
        TIN_CA => "#CA",
        _ => "#UNKNOWN",
    }
}

pub fn name_to_tin(name: &str) -> Option<Tin> {
    match name {
        "#BD" | "BD" => Some(TIN_BD),
        "#ZZ" | "ZZ" => Some(TIN_ZZ),
        "#UK" | "UK" => Some(TIN_UK),
        "#AA" | "AA" => Some(TIN_AA),
        "#SP" | "SP" => Some(TIN_SP),
        "#LN" | "LN" => Some(TIN_LN),
        "#CM" | "CM" => Some(TIN_CM),
        "#NR" | "NR" => Some(TIN_NR),
        "#ST" | "ST" => Some(TIN_ST),
        "#TX" | "TX" => Some(TIN_TX),
        "#VL" | "VL" => Some(TIN_VL),
        "#OB" | "OB" => Some(TIN_OB),
        "#CB" | "CB" => Some(TIN_CB),
        "#OS" | "OS" => Some(TIN_OS),
        "#CS" | "CS" => Some(TIN_CS),
        "#CL" | "CL" => Some(TIN_CL),
        "#CA" | "CA" => Some(TIN_CA),
        _ => None,
    }
}

/// Cursor position within the source text.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Point {
    pub len: usize, // Total UTF-8 byte length of the source.
    pub si: usize,  // 0-based UTF-8 byte position used for source slicing
    pub pos: usize, // 0-based Unicode-scalar position used by diagnostics
    pub ri: usize,  // 1-based row
    pub ci: usize,  // 1-based column
}

impl Default for Point {
    fn default() -> Self {
        Point {
            len: 0,
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
        }
    }
}

impl fmt::Display for Point {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "Point[{}/{},{},{}]",
            self.si, self.len, self.ri, self.ci
        )
    }
}

/// Lazy token value callback, evaluated only when a parser action asks for
/// the token's semantic value. This is the Rust counterpart of the canonical
/// `TokenValFunc` `(rule, context) => value` extension point.
#[derive(Clone)]
pub struct TokenValFunc {
    callback: Arc<TokenValCallback>,
}

impl TokenValFunc {
    pub fn new(
        callback: impl Fn(&mut Rule, &mut Context) -> Value + Send + Sync + 'static,
    ) -> Self {
        Self {
            callback: Arc::new(callback),
        }
    }

    pub fn call(&self, rule: &mut Rule, context: &mut Context) -> Value {
        (self.callback)(rule, context)
    }

    pub(crate) fn same_callback(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.callback, &other.callback)
    }
}

impl fmt::Debug for TokenValFunc {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("TokenValFunc(<function>)")
    }
}

impl PartialEq for TokenValFunc {
    fn eq(&self, other: &Self) -> bool {
        self.same_callback(other)
    }
}

impl Eq for TokenValFunc {}

/// A single lexical token produced by the lexer.
#[derive(Debug, Clone, PartialEq)]
pub struct Token {
    pub name: String,
    pub tin: Tin,
    pub val: Value,
    pub src: String,
    /// UTF-8 byte length of `src`, paired with the byte offset `si`.
    pub len: usize,
    pub si: usize,
    pub pos: usize,
    pub ri: usize,
    pub ci: usize,
    pub err: String,
    pub why: String,
    pub use_data: HashMap<String, Value>,
    /// Optional ignored trivia associated with this token. Negotiated
    /// re-lexing carries it to the replacement token.
    pub ignored: Option<Box<Token>>,
    /// Optional semantic value callback. `val` remains the eagerly produced
    /// fallback and the value shown by raw token inspection.
    pub val_fn: Option<TokenValFunc>,
}

impl Default for Token {
    fn default() -> Self {
        Token {
            name: String::new(),
            tin: -1,
            val: Value::Undefined,
            src: String::new(),
            len: 0,
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
            err: String::new(),
            why: String::new(),
            use_data: HashMap::new(),
            ignored: None,
            val_fn: None,
        }
    }
}

impl Token {
    pub fn new(
        name: impl Into<String>,
        tin: Tin,
        val: Value,
        src: impl Into<String>,
        pnt: Point,
    ) -> Self {
        let src = src.into();
        let len = src.len();
        Token {
            name: name.into(),
            tin,
            val,
            src,
            len,
            si: pnt.si,
            pos: pnt.pos,
            ri: pnt.ri,
            ci: pnt.ci,
            err: String::new(),
            why: String::new(),
            use_data: HashMap::new(),
            ignored: None,
            val_fn: None,
        }
    }

    pub fn no_token() -> Self {
        Token {
            // The canonical sentinel has no public token name; identity is
            // carried by tin -1 rather than a synthetic grammar token.
            name: String::new(),
            tin: -1,
            val: Value::Undefined,
            src: String::new(),
            len: 0,
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
            err: String::new(),
            why: String::new(),
            use_data: HashMap::new(),
            ignored: None,
            val_fn: None,
        }
    }

    pub fn is_no_token(&self) -> bool {
        self.tin == -1
    }

    pub fn bad(&mut self, err: &str) -> &mut Self {
        self.err = err.to_string();
        self
    }

    /// Mark this token bad and deep-merge plugin diagnostic details into its
    /// existing `use_data` bag. This is the typed Rust form of
    /// `token.bad(code, details)`.
    pub fn bad_with_details(
        &mut self,
        err: &str,
        details: impl IntoIterator<Item = (String, Value)>,
    ) -> &mut Self {
        self.err = err.to_string();
        for (key, value) in details {
            let previous = self.use_data.remove(&key).unwrap_or(Value::Undefined);
            self.use_data.insert(key, merge_detail(previous, value));
        }
        self
    }

    /// Attach an already shared lazy value callback.
    pub fn with_val_func(mut self, callback: TokenValFunc) -> Self {
        self.val_fn = Some(callback);
        self
    }

    /// Attach a lazy value callback without constructing a wrapper first.
    pub fn with_lazy_value(
        self,
        callback: impl Fn(&mut Rule, &mut Context) -> Value + Send + Sync + 'static,
    ) -> Self {
        self.with_val_func(TokenValFunc::new(callback))
    }

    /// Resolve the semantic token value against the live parse state.
    pub fn resolve_val(&self, rule: &mut Rule, context: &mut Context) -> Value {
        self.val_fn
            .as_ref()
            .map_or_else(|| self.val.clone(), |callback| callback.call(rule, context))
    }
}

impl fmt::Display for Token {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "Token[{}={} {}",
            self.name,
            self.tin,
            snip(&self.src, 5)
        )?;
        if !self.val.is_undefined() && !matches!(self.name.as_str(), "#ST" | "#TX") {
            write!(formatter, "={}", snip(&value_text(&self.val), 5))?;
        }
        write!(formatter, " {},{},{}", self.si, self.ri, self.ci)?;
        if !self.use_data.is_empty() {
            let mut entries = self.use_data.iter().collect::<Vec<_>>();
            entries.sort_by_key(|(key, _)| *key);
            let details = entries
                .into_iter()
                .map(|(key, value)| format!("{key}:{}", detail_json(value)))
                .collect::<Vec<_>>()
                .join(",");
            write!(
                formatter,
                " {}",
                snip(&format!("{{{details}}}").replace('"', ""), 22)
            )?;
        }
        if !self.err.is_empty() {
            write!(formatter, " {}", self.err)?;
        }
        if !self.why.is_empty() {
            write!(formatter, " {}", snip(&self.why, 22))?;
        }
        formatter.write_str("]")
    }
}

fn merge_detail(base: Value, overlay: Value) -> Value {
    match (base, overlay) {
        (base, Value::Undefined) => base,
        (Value::Object(mut base), Value::Object(overlay)) => {
            for (key, value) in overlay {
                let previous = base.shift_remove(&key).unwrap_or(Value::Undefined);
                base.insert(key, merge_detail(previous, value));
            }
            Value::Object(base)
        }
        (Value::Array(mut base), Value::Array(overlay)) => {
            if base.len() < overlay.len() {
                base.resize(overlay.len(), Value::Undefined);
            }
            for (index, value) in overlay.into_iter().enumerate() {
                let previous = std::mem::replace(&mut base[index], Value::Undefined);
                base[index] = merge_detail(previous, value);
            }
            Value::Array(base)
        }
        (_, overlay) => overlay,
    }
}

fn value_text(value: &Value) -> String {
    match value {
        Value::Undefined => String::new(),
        Value::Null => "null".into(),
        Value::Bool(value) => value.to_string(),
        Value::Number(value) => Value::Number(*value).to_string(),
        Value::String(value) | Value::Text(crate::Text { string: value, .. }) => value.clone(),
        Value::Array(values) => values.iter().map(value_text).collect::<Vec<_>>().join(","),
        Value::ListRef(list) => list
            .value
            .iter()
            .map(value_text)
            .collect::<Vec<_>>()
            .join(","),
        Value::Object(_) | Value::MapRef(_) => "[object Object]".into(),
    }
}

fn detail_json(value: &Value) -> String {
    match value {
        Value::Undefined | Value::Null => "null".into(),
        Value::Bool(value) => value.to_string(),
        Value::Number(value) => Value::Number(*value).to_string(),
        Value::String(value) | Value::Text(crate::Text { string: value, .. }) => {
            serde_json::to_string(value).unwrap_or_default()
        }
        Value::Array(values) => format!(
            "[{}]",
            values.iter().map(detail_json).collect::<Vec<_>>().join(",")
        ),
        Value::Object(values) => format!(
            "{{{}}}",
            values
                .iter()
                // JSON.stringify omits undefined-valued object properties
                // (while array slots below render as null).
                .filter(|(_, value)| !value.is_undefined())
                .map(|(key, value)| format!(
                    "{}:{}",
                    serde_json::to_string(key).unwrap_or_default(),
                    detail_json(value)
                ))
                .collect::<Vec<_>>()
                .join(",")
        ),
        Value::ListRef(list) => detail_json(&Value::Array(list.value.clone())),
        Value::MapRef(map) => detail_json(&Value::Object(map.value.clone())),
    }
}

fn snip(value: &str, max_len: usize) -> String {
    value
        .chars()
        .take(max_len)
        .map(|character| match character {
            '\r' | '\n' | '\t' => '.',
            character => character,
        })
        .collect()
}
