// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::value::unwrap_arc;
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

/// One location in the source text: the index reached, and the row and
/// column that index sits at. It is the same data TypeScript carries as
/// the loose `sI`/`rI`/`cI` fields on both `Point` and `Token`
/// (ts/src/lexer.ts), and as the `ScanOut` record its scan driver writes
/// back — naming it once is what keeps those uses in step.
///
/// The extra field is `pos`, and it is not extra data: TypeScript indexes
/// source by UTF-16 code unit and reports that same number in a
/// diagnostic, while Rust slices by UTF-8 byte. So `si` is the offset
/// this port slices with and `pos` is the offset it REPORTS, and the two
/// together say what TypeScript's single `sI` says. See
/// `go/doc/differences.md` and DIVERGENCE.md "Column positions for astral
/// characters" for what that costs at the edges.
///
/// Deliberately NOT carrying a length: `Point::len` is the length of the
/// whole source and `Token::len` is the length of that token's matched
/// text, so the two mean different things and only the position is shared.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Site {
    pub si: usize,  // 0-based UTF-8 byte position used for source slicing
    pub pos: usize, // 0-based Unicode-scalar position used by diagnostics
    pub ri: usize,  // 1-based row
    pub ci: usize,  // 1-based column
}

impl Default for Site {
    fn default() -> Self {
        Site {
            si: 0,
            pos: 0,
            ri: 1,
            ci: 1,
        }
    }
}

impl fmt::Display for Site {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{},{},{}", self.si, self.ri, self.ci)
    }
}

/// Cursor position within the source text.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Point {
    pub len: usize, // Total UTF-8 byte length of the source.
    pub site: Site, // Where the cursor currently sits.
}

impl fmt::Display for Point {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            formatter,
            "Point[{}/{},{},{}]",
            self.site.si, self.len, self.site.ri, self.site.ci
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
#[derive(Clone, Default)]
pub struct TokenText(crate::text::InlineText);

impl TokenText {
    pub fn as_str(&self) -> &str {
        self.0.as_str()
    }

    pub fn is_empty(&self) -> bool {
        self.as_str().is_empty()
    }

    fn build(text: &str) -> Self {
        TokenText(crate::text::InlineText::new(text))
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
        TokenText(crate::text::InlineText::shared(text))
    }
}

impl From<TokenText> for String {
    fn from(text: TokenText) -> Self {
        text.as_str().to_string()
    }
}

/// A token's diagnostic code, held behind a pointer.
///
/// `err` and `why` are empty on virtually every token of every parse, and a
/// `Token` is moved and cloned several times per input construct. Carried
/// inline as `TokenText` the pair took 48 of the token's 248 bytes. Padding
/// `Token` by those 32 bytes measured 1.2% to 3.8% across the benchmark
/// rows, which is what carrying them inline was costing; behind a pointer
/// the pair costs 16 bytes and one allocation on the error path, which is
/// already the expensive one.
///
/// The surface matches `TokenText`, so reading code does not change: it
/// derefs, compares and prints as `str`, and an absent code reads as the
/// empty string. Setting one to `""` stores nothing, so `is_empty` answers
/// the same question either way.
#[derive(Clone, Default)]
pub struct TokenCode(Option<Box<TokenText>>);

impl TokenCode {
    pub fn as_str(&self) -> &str {
        match &self.0 {
            Some(text) => text.as_str(),
            None => "",
        }
    }

    pub fn is_empty(&self) -> bool {
        self.0.is_none()
    }

    fn build(text: &str) -> Self {
        TokenCode((!text.is_empty()).then(|| Box::new(TokenText::from(text))))
    }
}

impl Eq for TokenCode {}

impl PartialEq for TokenCode {
    fn eq(&self, other: &Self) -> bool {
        self.as_str() == other.as_str()
    }
}

impl std::hash::Hash for TokenCode {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.as_str().hash(state)
    }
}

impl PartialOrd for TokenCode {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for TokenCode {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.as_str().cmp(other.as_str())
    }
}

impl std::ops::Deref for TokenCode {
    type Target = str;

    fn deref(&self) -> &str {
        self.as_str()
    }
}

impl AsRef<str> for TokenCode {
    fn as_ref(&self) -> &str {
        self.as_str()
    }
}

impl std::borrow::Borrow<str> for TokenCode {
    fn borrow(&self) -> &str {
        self.as_str()
    }
}

impl fmt::Display for TokenCode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Printed as the bare text, like `TokenText`.
impl fmt::Debug for TokenCode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        fmt::Debug::fmt(self.as_str(), f)
    }
}

impl PartialEq<str> for TokenCode {
    fn eq(&self, other: &str) -> bool {
        self.as_str() == other
    }
}

impl PartialEq<&str> for TokenCode {
    fn eq(&self, other: &&str) -> bool {
        self.as_str() == *other
    }
}

impl PartialEq<String> for TokenCode {
    fn eq(&self, other: &String) -> bool {
        self.as_str() == other.as_str()
    }
}

impl PartialEq<TokenCode> for str {
    fn eq(&self, other: &TokenCode) -> bool {
        self == other.as_str()
    }
}

impl PartialEq<TokenCode> for &str {
    fn eq(&self, other: &TokenCode) -> bool {
        *self == other.as_str()
    }
}

impl PartialEq<TokenCode> for String {
    fn eq(&self, other: &TokenCode) -> bool {
        self.as_str() == other.as_str()
    }
}

impl From<&str> for TokenCode {
    fn from(text: &str) -> Self {
        TokenCode::build(text)
    }
}

impl From<String> for TokenCode {
    fn from(text: String) -> Self {
        TokenCode::build(text.as_str())
    }
}

impl From<&String> for TokenCode {
    fn from(text: &String) -> Self {
        TokenCode::build(text.as_str())
    }
}

impl From<TokenText> for TokenCode {
    fn from(text: TokenText) -> Self {
        TokenCode((!text.is_empty()).then(|| Box::new(text)))
    }
}

impl From<TokenCode> for String {
    fn from(text: TokenCode) -> Self {
        text.as_str().to_string()
    }
}

/// A single lexical token produced by the lexer.
#[derive(Debug, Clone, PartialEq)]
pub struct Token {
    pub name: TokenText,
    pub tin: Tin,
    pub val: Value,
    pub src: TokenText,
    /// UTF-8 byte length of `src`, paired with the byte offset `site.si`.
    pub len: usize,
    /// Where in the source this token starts.
    pub site: Site,
    pub err: TokenCode,
    pub why: TokenCode,
    /// Plugin diagnostic details, boxed and absent until something
    /// writes one. Prefer `use_data()` and `use_data_mut()` to reaching
    /// through the `Option`; the shape is public only so that a `Token`
    /// can still be built with a struct literal. It is boxed because a `HashMap` is 48 bytes inline, a token is
    /// cloned about six times per input construct, and almost no token
    /// ever carries a detail. Measured by padding `Token`, 96 bytes of
    /// it is worth 3% to 12% depending on the grammar.
    pub use_data: Option<Box<HashMap<String, Value>>>,
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
            site: Site::default(),
            err: TokenCode::default(),
            why: TokenCode::default(),
            use_data: None,
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
            name: TokenText::from(name.as_ref()),
            tin,
            val,
            src,
            len,
            site: pnt.site,
            err: TokenCode::default(),
            why: TokenCode::default(),
            use_data: None,
            ignored: None,
            val_fn: None,
        }
    }

    /// Plugin diagnostic details. Empty unless something wrote one.
    pub fn use_data(&self) -> &HashMap<String, Value> {
        static EMPTY: std::sync::OnceLock<HashMap<String, Value>> = std::sync::OnceLock::new();
        match &self.use_data {
            Some(details) => details,
            None => EMPTY.get_or_init(HashMap::new),
        }
    }

    /// Mutable access, allocating the bag on first write.
    pub fn use_data_mut(&mut self) -> &mut HashMap<String, Value> {
        self.use_data.get_or_insert_with(Box::default)
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
            site: Site::default(),
            err: TokenCode::default(),
            why: TokenCode::default(),
            use_data: None,
            ignored: None,
            val_fn: None,
        }
    }

    pub fn is_no_token(&self) -> bool {
        self.tin == -1
    }

    pub fn bad(&mut self, err: &str) -> &mut Self {
        self.err = TokenCode::from(err);
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
        self.err = TokenCode::from(err);
        for (key, value) in details {
            let details = self.use_data_mut();
            let previous = details.remove(&key).unwrap_or(Value::Undefined);
            details.insert(key, merge_detail(previous, value));
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
        write!(formatter, " {}", self.site)?;
        if !self.use_data().is_empty() {
            let mut entries = self.use_data().iter().collect::<Vec<_>>();
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
        (Value::Object(base), Value::Object(overlay)) => {
            let mut base = unwrap_arc(base);
            for (key, value) in unwrap_arc(overlay) {
                let previous = base.shift_remove(&key).unwrap_or(Value::Undefined);
                base.insert(key, merge_detail(previous, value));
            }
            Value::object(base)
        }
        (Value::Array(base), Value::Array(overlay)) => {
            let mut base = unwrap_arc(base);
            let overlay = unwrap_arc(overlay);
            if base.len() < overlay.len() {
                base.resize(overlay.len(), Value::Undefined);
            }
            for (index, value) in overlay.into_iter().enumerate() {
                let previous = std::mem::replace(&mut base[index], Value::Undefined);
                base[index] = merge_detail(previous, value);
            }
            Value::array(base)
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
        Value::ListRef(list) => detail_json(&Value::array(list.value.clone())),
        Value::MapRef(map) => detail_json(&Value::object(map.value.clone())),
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
