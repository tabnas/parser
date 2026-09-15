// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::value::Value;
use crate::{Context, Rule};
use std::cell::RefCell;
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

/// A token's name or matched source text.
///
/// Tokens are cloned about six times per input construct, and each
/// clone used to copy both of these strings. Sharing them makes a
/// clone a pointer copy; names go further and are interned per token
/// identity, so lexing one costs nothing at all. It behaves like the
/// `String` it replaced: compare it with a literal, print it, index
/// it, or take a `&str` from it.
#[derive(Clone)]
pub struct TokenText(TextRepr);

/// Most tokens are a handful of characters — a digit, a comma, a
/// brace — so the text is held inline and a clone is a register copy.
/// Sharing the long ones keeps their clones cheap too, but through an
/// `Arc`, whose atomic refcount is what makes it the slower choice for
/// the short ones: glibc serves a small allocation from a fast bin in
/// less than an atomic read-modify-write costs.
#[derive(Clone)]
enum TextRepr {
    Inline {
        len: u8,
        bytes: [u8; INLINE_CAPACITY],
    },
    Shared(Arc<str>),
}

const INLINE_CAPACITY: usize = 22;

impl TokenText {
    pub fn as_str(&self) -> &str {
        match &self.0 {
            // SAFETY: `bytes[..len]` is only ever written from a `&str`,
            // so it is whole, valid UTF-8.
            TextRepr::Inline { len, bytes } => unsafe {
                std::str::from_utf8_unchecked(&bytes[..*len as usize])
            },
            TextRepr::Shared(text) => text,
        }
    }

    pub fn is_empty(&self) -> bool {
        self.as_str().is_empty()
    }

    fn build(text: &str) -> Self {
        if text.len() <= INLINE_CAPACITY {
            let mut bytes = [0u8; INLINE_CAPACITY];
            bytes[..text.len()].copy_from_slice(text.as_bytes());
            TokenText(TextRepr::Inline {
                len: text.len() as u8,
                bytes,
            })
        } else {
            TokenText(TextRepr::Shared(Arc::from(text)))
        }
    }
}

impl Default for TokenText {
    fn default() -> Self {
        TokenText(TextRepr::Inline {
            len: 0,
            bytes: [0u8; INLINE_CAPACITY],
        })
    }
}

impl Eq for TokenText {}

impl PartialEq for TokenText {
    fn eq(&self, other: &Self) -> bool {
        self.as_str() == other.as_str()
    }
}

impl std::hash::Hash for TokenText {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.as_str().hash(state)
    }
}

impl PartialOrd for TokenText {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for TokenText {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.as_str().cmp(other.as_str())
    }
}

impl std::ops::Deref for TokenText {
    type Target = str;

    fn deref(&self) -> &str {
        self.as_str()
    }
}

impl AsRef<str> for TokenText {
    fn as_ref(&self) -> &str {
        self.as_str()
    }
}

impl std::borrow::Borrow<str> for TokenText {
    fn borrow(&self) -> &str {
        self.as_str()
    }
}

impl fmt::Display for TokenText {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Printed as the bare text, so a `{:?}` of a token reads the way it
/// did when these were `String`s.
impl fmt::Debug for TokenText {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        fmt::Debug::fmt(self.as_str(), f)
    }
}

impl PartialEq<str> for TokenText {
    fn eq(&self, other: &str) -> bool {
        self.as_str() == other
    }
}

impl PartialEq<&str> for TokenText {
    fn eq(&self, other: &&str) -> bool {
        self.as_str() == *other
    }
}

impl PartialEq<String> for TokenText {
    fn eq(&self, other: &String) -> bool {
        self.as_str() == other.as_str()
    }
}

impl PartialEq<TokenText> for str {
    fn eq(&self, other: &TokenText) -> bool {
        self == other.as_str()
    }
}

impl PartialEq<TokenText> for &str {
    fn eq(&self, other: &TokenText) -> bool {
        *self == other.as_str()
    }
}

impl PartialEq<TokenText> for String {
    fn eq(&self, other: &TokenText) -> bool {
        self.as_str() == other.as_str()
    }
}

impl From<&str> for TokenText {
    fn from(text: &str) -> Self {
        TokenText::build(text)
    }
}

impl From<String> for TokenText {
    fn from(text: String) -> Self {
        TokenText::build(text.as_str())
    }
}

impl From<&String> for TokenText {
    fn from(text: &String) -> Self {
        TokenText::build(text.as_str())
    }
}

impl From<Arc<str>> for TokenText {
    fn from(text: Arc<str>) -> Self {
        TokenText(TextRepr::Shared(text))
    }
}

impl From<TokenText> for String {
    fn from(text: TokenText) -> Self {
        text.as_str().to_string()
    }
}

/// One shared handle per token identity. Token names come from a fixed
/// registry, so the same half-dozen strings were being allocated once
/// per token in the input. The cache is keyed by `tin` and checked
/// against the name asked for, because a grammar may bind its own name
/// to a tin; a mismatch just allocates, as before.
pub(crate) fn interned_token_name(name: &str, tin: Tin) -> TokenText {
    thread_local! {
        static NAMES: RefCell<HashMap<Tin, TokenText>> = RefCell::new(HashMap::new());
    }
    NAMES.with(|cache| {
        let mut cache = cache.borrow_mut();
        match cache.get(&tin) {
            Some(shared) if *shared == name => shared.clone(),
            _ => {
                let shared = TokenText::from(name);
                cache.insert(tin, shared.clone());
                shared
            }
        }
    })
}

/// A single lexical token produced by the lexer.
#[derive(Debug, Clone, PartialEq)]
pub struct Token {
    pub name: TokenText,
    pub tin: Tin,
    pub val: Value,
    pub src: TokenText,
    /// UTF-8 byte length of `src`, paired with the byte offset `si`.
    pub len: usize,
    pub si: usize,
    pub pos: usize,
    pub ri: usize,
    pub ci: usize,
    pub err: TokenText,
    pub why: TokenText,
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
            name: TokenText::default(),
            tin: -1,
            val: Value::Undefined,
            src: TokenText::default(),
            len: 0,
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
            err: TokenText::default(),
            why: TokenText::default(),
            use_data: HashMap::new(),
            ignored: None,
            val_fn: None,
        }
    }
}

impl Token {
    pub fn new(
        name: impl AsRef<str>,
        tin: Tin,
        val: Value,
        src: impl Into<TokenText>,
        pnt: Point,
    ) -> Self {
        let src: TokenText = src.into();
        let len = src.len();
        Token {
            name: interned_token_name(name.as_ref(), tin),
            tin,
            val,
            src,
            len,
            si: pnt.si,
            pos: pnt.pos,
            ri: pnt.ri,
            ci: pnt.ci,
            err: TokenText::default(),
            why: TokenText::default(),
            use_data: HashMap::new(),
            ignored: None,
            val_fn: None,
        }
    }

    pub fn no_token() -> Self {
        Token {
            // The canonical sentinel has no public token name; identity is
            // carried by tin -1 rather than a synthetic grammar token.
            name: TokenText::default(),
            tin: -1,
            val: Value::Undefined,
            src: TokenText::default(),
            len: 0,
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
            err: TokenText::default(),
            why: TokenText::default(),
            use_data: HashMap::new(),
            ignored: None,
            val_fn: None,
        }
    }

    pub fn is_no_token(&self) -> bool {
        self.tin == -1
    }

    pub fn bad(&mut self, err: &str) -> &mut Self {
        self.err = TokenText::from(err);
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
        self.err = TokenText::from(err);
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
