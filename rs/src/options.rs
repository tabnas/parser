// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, TIN_CM, TIN_LN, TIN_MAX, TIN_NR, TIN_SP, TIN_ST, TIN_TX, TIN_VL};
use indexmap::IndexMap;
use regex::Regex;
use std::collections::HashMap;
use std::fmt;
use std::sync::Arc;

use crate::context::Context;
use crate::lexer::Lexer;
use crate::rule::Rule;
use crate::token::Token;

type ConfigModifierCallback = dyn Fn(&mut Options, &Options) + Send + Sync;

#[derive(Clone)]
pub struct ConfigModifier {
    callback: Arc<ConfigModifierCallback>,
}

impl ConfigModifier {
    pub(crate) fn new(callback: impl Fn(&mut Options) + Send + Sync + 'static) -> Self {
        Self {
            callback: Arc::new(move |config, _options| callback(config)),
        }
    }

    pub(crate) fn with_options(
        callback: impl Fn(&mut Options, &Options) + Send + Sync + 'static,
    ) -> Self {
        Self {
            callback: Arc::new(callback),
        }
    }

    pub(crate) fn run(&self, config: &mut Options, options: &Options) {
        (self.callback)(config, options);
    }

    pub(crate) fn same_callback(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.callback, &other.callback)
    }
}

impl fmt::Debug for ConfigModifier {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("ConfigModifier(<function>)")
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ColorOptions {
    pub active: bool,
    pub reset: String,
    pub hi: String,
    pub lo: String,
    pub line: String,
}

impl ColorOptions {
    pub(crate) fn codes(&self) -> (&str, &str, &str, &str) {
        if self.active {
            (&self.hi, &self.lo, &self.line, &self.reset)
        } else {
            ("", "", "", "")
        }
    }
}

impl Default for ColorOptions {
    fn default() -> Self {
        Self {
            active: true,
            reset: "\x1b[0m".into(),
            hi: "\x1b[91m".into(),
            lo: "\x1b[2m".into(),
            line: "\x1b[34m".into(),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ErrorSuffixContext {
    pub code: String,
    pub source: String,
    pub message: String,
    pub hint: String,
    pub pos: usize,
    pub row: usize,
    pub col: usize,
    pub name: String,
    pub tag: String,
    pub rule: String,
    pub rule_state: String,
    pub token: String,
    pub why: String,
    pub plugins: Vec<String>,
    pub color: ColorOptions,
}

pub type ErrorSuffixCallback = Arc<dyn Fn(&ErrorSuffixContext) -> String + Send + Sync>;

#[derive(Clone)]
pub enum ErrorSuffix {
    Standard,
    Disabled,
    Text(String),
    Callback(ErrorSuffixCallback),
}

impl fmt::Debug for ErrorSuffix {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Standard => formatter.write_str("Standard"),
            Self::Disabled => formatter.write_str("Disabled"),
            Self::Text(text) => formatter.debug_tuple("Text").field(text).finish(),
            Self::Callback(_) => formatter.write_str("Callback(<function>)"),
        }
    }
}

impl PartialEq for ErrorSuffix {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Standard, Self::Standard) | (Self::Disabled, Self::Disabled) => true,
            (Self::Text(left), Self::Text(right)) => left == right,
            (Self::Callback(left), Self::Callback(right)) => Arc::ptr_eq(left, right),
            _ => false,
        }
    }
}

impl Eq for ErrorSuffix {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ErrMsgOptions {
    pub name: String,
    pub suffix: ErrorSuffix,
    pub link: String,
}

impl Default for ErrMsgOptions {
    fn default() -> Self {
        Self {
            name: "tabnas".into(),
            suffix: ErrorSuffix::Standard,
            link: String::new(),
        }
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct LexCheckToken {
    pub name: String,
    pub tin: Tin,
    pub source: String,
    pub value: crate::Value,
}

impl LexCheckToken {
    pub fn new(
        name: impl Into<String>,
        tin: Tin,
        source: impl Into<String>,
        value: crate::Value,
    ) -> Self {
        Self {
            name: name.into(),
            tin,
            source: source.into(),
            value,
        }
    }

    /// Build a token effect whose numeric identity is resolved from its name
    /// after the serialized grammar has declared that token.
    pub fn named(name: impl Into<String>, source: impl Into<String>, value: crate::Value) -> Self {
        let name = name.into();
        Self {
            name: if name.starts_with('#') {
                name
            } else {
                format!("#{name}")
            },
            tin: -1,
            source: source.into(),
            value,
        }
    }
}

/// Effect returned by a matcher-family preflight hook.
#[derive(Debug, Clone, PartialEq)]
pub enum LexCheckResult {
    /// Run the matcher's normal implementation.
    Continue,
    /// Skip this matcher and try the next matcher family.
    Skip,
    /// Emit an owned token while consuming its non-empty source prefix.
    Token(Box<LexCheckToken>),
    /// Return a token built by a live lexer callback. The callback owns all
    /// cursor movement, matching the canonical `LexCheck(lex)` contract.
    NativeToken(Box<Token>),
}

impl LexCheckResult {
    pub fn token(token: LexCheckToken) -> Self {
        Self::Token(Box::new(token))
    }

    pub fn native_token(token: Token) -> Self {
        Self::NativeToken(Box::new(token))
    }
}

pub type ImperativeLexCheck =
    Arc<dyn for<'source> Fn(&mut Lexer<'source>) -> LexCheckResult + Send + Sync>;

#[derive(Clone)]
pub struct LexCheck {
    callback: Option<Arc<LexCheckEffect>>,
    imperative: Option<ImperativeLexCheck>,
}

type LexCheckEffect = dyn Fn(&str) -> LexCheckResult + Send + Sync;

impl LexCheck {
    pub(crate) fn new(callback: impl Fn(&str) -> LexCheckResult + Send + Sync + 'static) -> Self {
        Self {
            callback: Some(Arc::new(callback)),
            imperative: None,
        }
    }

    pub(crate) fn new_imperative(
        callback: impl for<'source> Fn(&mut Lexer<'source>) -> LexCheckResult + Send + Sync + 'static,
    ) -> Self {
        Self {
            callback: None,
            imperative: Some(Arc::new(callback)),
        }
    }

    pub(crate) fn run(&self, source: &str) -> Option<LexCheckResult> {
        self.callback.as_ref().map(|callback| callback(source))
    }

    pub(crate) fn run_imperative(&self, lexer: &mut Lexer<'_>) -> Option<LexCheckResult> {
        self.imperative.as_ref().map(|callback| callback(lexer))
    }

    pub(crate) fn same_callback(&self, other: &Self) -> bool {
        match (
            &self.callback,
            &self.imperative,
            &other.callback,
            &other.imperative,
        ) {
            (Some(left), None, Some(right), None) => Arc::ptr_eq(left, right),
            (None, Some(left), None, Some(right)) => Arc::ptr_eq(left, right),
            _ => false,
        }
    }
}

impl fmt::Debug for LexCheck {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(if self.imperative.is_some() {
            "LexCheck(<live lexer function>)"
        } else {
            "LexCheck(<effect function>)"
        })
    }
}

#[derive(Debug, Clone)]
pub struct FixedToken {
    pub name: String,
    pub tin: Tin,
    pub source: String,
}

#[derive(Debug, Clone)]
pub struct FixedOptions {
    pub lex: bool,
    pub tokens: IndexMap<String, FixedToken>,
    pub check: Option<LexCheck>,
}

impl Default for FixedOptions {
    fn default() -> Self {
        let mut tokens = IndexMap::new();
        for (name, tin, source) in [
            ("#OB", crate::token::TIN_OB, "{"),
            ("#CB", crate::token::TIN_CB, "}"),
            ("#OS", crate::token::TIN_OS, "["),
            ("#CS", crate::token::TIN_CS, "]"),
            ("#CL", crate::token::TIN_CL, ":"),
            ("#CA", crate::token::TIN_CA, ","),
        ] {
            tokens.insert(
                name.to_string(),
                FixedToken {
                    name: name.to_string(),
                    tin,
                    source: source.to_string(),
                },
            );
        }
        Self {
            lex: true,
            tokens,
            check: None,
        }
    }
}

pub type ValueTextModifier = Arc<dyn Fn(crate::Value) -> crate::Value + Send + Sync>;
pub type ImperativeTextModifier = Arc<
    dyn for<'source> Fn(
            crate::Value,
            &mut Lexer<'source>,
            &mut Rule,
            &mut Context,
            &Options,
        ) -> crate::Value
        + Send
        + Sync,
>;

/// One unquoted-text modifier in declaration order. The value-only form is
/// convenient for pure serialized hooks; the imperative form exposes the
/// same live lexer/config state as the canonical `ValModifier` and also hands
/// Rust callers explicit rule/context references instead of hiding them on
/// the lexer object.
#[derive(Clone)]
pub enum TextModifier {
    Value(ValueTextModifier),
    Imperative(ImperativeTextModifier),
}

impl TextModifier {
    pub(crate) fn new(
        modifier: impl Fn(crate::Value) -> crate::Value + Send + Sync + 'static,
    ) -> Self {
        Self::Value(Arc::new(modifier))
    }

    pub(crate) fn new_imperative(
        modifier: impl for<'source> Fn(
                crate::Value,
                &mut Lexer<'source>,
                &mut Rule,
                &mut Context,
                &Options,
            ) -> crate::Value
            + Send
            + Sync
            + 'static,
    ) -> Self {
        Self::Imperative(Arc::new(modifier))
    }

    pub(crate) fn run(
        &self,
        value: crate::Value,
        lexer: &mut Lexer<'_>,
        rule: &mut Rule,
        context: &mut Context,
        options: &Options,
    ) -> crate::Value {
        match self {
            Self::Value(modifier) => modifier(value),
            Self::Imperative(modifier) => modifier(value, lexer, rule, context, options),
        }
    }

    pub(crate) fn same_callback(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Value(left), Self::Value(right)) => Arc::ptr_eq(left, right),
            (Self::Imperative(left), Self::Imperative(right)) => Arc::ptr_eq(left, right),
            _ => false,
        }
    }
}

impl fmt::Debug for TextModifier {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(match self {
            Self::Value(_) => "TextModifier::Value(<function>)",
            Self::Imperative(_) => "TextModifier::Imperative(<function>)",
        })
    }
}

#[derive(Clone)]
pub struct TextOptions {
    pub lex: bool,
    pub modify: Vec<TextModifier>,
    pub check: Option<LexCheck>,
}

impl fmt::Debug for TextOptions {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("TextOptions")
            .field("lex", &self.lex)
            .field("modify", &self.modify.len())
            .field("check", &self.check)
            .finish()
    }
}

impl Default for TextOptions {
    fn default() -> Self {
        TextOptions {
            lex: true,
            modify: Vec::new(),
            check: None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct SpaceOptions {
    pub lex: bool,
    pub chars: String,
    pub check: Option<LexCheck>,
}

impl Default for SpaceOptions {
    fn default() -> Self {
        Self {
            lex: true,
            chars: " \t".into(),
            check: None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct NumberOptions {
    pub lex: bool,
    pub hex: bool,
    pub oct: bool,
    pub bin: bool,
    pub sep: Option<String>,
    pub exclude: Option<String>, // regex string e.g. "^00+"
    pub check: Option<LexCheck>,
}

impl Default for NumberOptions {
    fn default() -> Self {
        NumberOptions {
            lex: true,
            hex: true,
            oct: true,
            bin: true,
            sep: Some("_".to_string()),
            exclude: None,
            check: None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct StringOptions {
    pub lex: bool,
    pub chars: String,
    pub multi_chars: String,
    pub escape_char: char,
    pub escape: HashMap<char, String>,
    pub replace: HashMap<char, String>,
    pub allow_unknown: bool,
    pub escape_strict: bool,
    pub allow_control: bool,
    pub abandon: bool,
    pub check: Option<LexCheck>,
}

impl Default for StringOptions {
    fn default() -> Self {
        let escape = [
            ('b', "\u{0008}"),
            ('f', "\u{000c}"),
            ('n', "\n"),
            ('r', "\r"),
            ('t', "\t"),
            ('v', "\u{000b}"),
            ('"', "\""),
            ('\'', "'"),
            ('`', "`"),
            ('\\', "\\"),
            ('/', "/"),
        ]
        .into_iter()
        .map(|(key, value)| (key, value.into()))
        .collect();
        StringOptions {
            lex: true,
            chars: "\"'`".to_string(),
            multi_chars: "`".to_string(),
            escape_char: '\\',
            escape,
            replace: HashMap::new(),
            allow_unknown: true,
            escape_strict: false,
            allow_control: false,
            abandon: false,
            check: None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct LineOptions {
    pub lex: bool,
    pub chars: String,
    pub row_chars: String,
    pub single: bool,
    /// Extra line terminators retained for compatibility with the first
    /// Rust slice. Serialized grammars should prefer `line.chars`.
    pub fixed: Vec<char>,
    pub check: Option<LexCheck>,
}

impl Default for LineOptions {
    fn default() -> Self {
        Self {
            lex: true,
            chars: "\r\n".into(),
            row_chars: "\n".into(),
            single: false,
            fixed: Vec::new(),
            check: None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct CommentDef {
    pub line: bool,
    pub start: String,
    pub end: String,
    pub lex: bool,
    pub suffixes: Vec<String>,
    pub suffix_matcher: Option<CommentSuffixMatcher>,
    pub eat_line: bool,
}

#[derive(Clone)]
pub struct CommentSuffixMatcher {
    callback: Option<Arc<CommentSuffixCallback>>,
    imperative: Option<ImperativeCommentSuffixMatcher>,
}

type CommentSuffixCallback = dyn Fn(&str) -> Option<String> + Send + Sync;
pub type ImperativeCommentSuffixMatcher =
    Arc<dyn for<'source> Fn(&mut Lexer<'source>) -> Option<Token> + Send + Sync>;

impl CommentSuffixMatcher {
    pub(crate) fn new(callback: impl Fn(&str) -> Option<String> + Send + Sync + 'static) -> Self {
        Self {
            callback: Some(Arc::new(callback)),
            imperative: None,
        }
    }

    pub(crate) fn new_imperative(
        callback: impl for<'source> Fn(&mut Lexer<'source>) -> Option<Token> + Send + Sync + 'static,
    ) -> Self {
        Self {
            callback: None,
            imperative: Some(Arc::new(callback)),
        }
    }

    pub(crate) fn run(&self, source: &str) -> Option<String> {
        self.callback.as_ref().and_then(|callback| callback(source))
    }

    pub(crate) fn run_imperative(&self, lexer: &mut Lexer<'_>) -> Option<Token> {
        self.imperative
            .as_ref()
            .and_then(|callback| callback(lexer))
    }

    pub(crate) fn same_callback(&self, other: &Self) -> bool {
        match (
            &self.callback,
            &self.imperative,
            &other.callback,
            &other.imperative,
        ) {
            (Some(left), None, Some(right), None) => Arc::ptr_eq(left, right),
            (None, Some(left), None, Some(right)) => Arc::ptr_eq(left, right),
            _ => false,
        }
    }
}

impl fmt::Debug for CommentSuffixMatcher {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(if self.imperative.is_some() {
            "CommentSuffixMatcher(<live lexer function>)"
        } else {
            "CommentSuffixMatcher(<effect function>)"
        })
    }
}

#[derive(Debug, Clone)]
pub struct CommentOptions {
    pub lex: bool,
    pub definitions: IndexMap<String, CommentDef>,
    pub check: Option<LexCheck>,
}

#[derive(Clone)]
pub struct ValueDef {
    pub val: Option<crate::Value>,
    pub matcher: Option<Regex>,
    pub transform: Option<ValueTransform>,
    pub consume: bool,
}

pub type ValueTransform = Arc<dyn Fn(&[String]) -> crate::Value + Send + Sync>;

impl fmt::Debug for ValueDef {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ValueDef")
            .field("val", &self.val)
            .field("matcher", &self.matcher)
            .field("transform", &self.transform.as_ref().map(|_| "<function>"))
            .field("consume", &self.consume)
            .finish()
    }
}

#[derive(Debug, Clone)]
pub struct ValueOptions {
    pub lex: bool,
    pub definitions: IndexMap<String, ValueDef>,
}

impl Default for ValueOptions {
    fn default() -> Self {
        let mut definitions = IndexMap::new();
        for (source, value) in [
            ("true", crate::Value::Bool(true)),
            ("false", crate::Value::Bool(false)),
            ("null", crate::Value::Null),
        ] {
            definitions.insert(
                source.into(),
                ValueDef {
                    val: Some(value),
                    matcher: None,
                    transform: None,
                    consume: false,
                },
            );
        }
        Self {
            lex: true,
            definitions,
        }
    }
}

impl Default for CommentOptions {
    fn default() -> Self {
        let mut definitions = IndexMap::new();
        for (name, line, start, end) in [
            ("hash", true, "#", ""),
            ("slash", true, "//", ""),
            ("multi", false, "/*", "*/"),
        ] {
            definitions.insert(
                name.into(),
                CommentDef {
                    line,
                    start: start.into(),
                    end: end.into(),
                    lex: true,
                    suffixes: Vec::new(),
                    suffix_matcher: None,
                    eat_line: false,
                },
            );
        }
        CommentOptions {
            lex: true,
            definitions,
            check: None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct SafeOptions {
    pub key: bool,
}

impl Default for SafeOptions {
    fn default() -> Self {
        Self { key: true }
    }
}

pub type MapMerge =
    Arc<dyn Fn(crate::Value, crate::Value, &mut Rule, &mut Context) -> crate::Value + Send + Sync>;

#[derive(Clone)]
pub struct MapOptions {
    pub extend: bool,
    pub merge: Option<MapMerge>,
    pub child: bool,
    /// Rust's `IndexMap` always retains insertion order. This flag is kept so
    /// serialized option overlays and plugin code observe the canonical
    /// option surface even though enabling it requires no representation
    /// change here.
    pub ordered: bool,
}

impl fmt::Debug for MapOptions {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("MapOptions")
            .field("extend", &self.extend)
            .field("merge", &self.merge.as_ref().map(|_| "<function>"))
            .field("child", &self.child)
            .field("ordered", &self.ordered)
            .finish()
    }
}

impl Default for MapOptions {
    fn default() -> Self {
        MapOptions {
            extend: true,
            merge: None,
            child: false,
            ordered: false,
        }
    }
}

#[derive(Debug, Clone)]
pub struct ListOptions {
    pub property: bool,
    pub pair: bool,
    pub child: bool,
}

impl Default for ListOptions {
    fn default() -> Self {
        Self {
            property: true,
            pair: false,
            child: false,
        }
    }
}

/// Controls typed metadata carriers on native parse results.
///
/// The wrappers serialize as their underlying JSON values, matching the
/// TypeScript implementation's non-enumerable metadata marker.
#[derive(Debug, Clone)]
pub struct InfoOptions {
    pub map: bool,
    pub list: bool,
    pub text: bool,
    pub marker: String,
}

impl Default for InfoOptions {
    fn default() -> Self {
        Self {
            map: false,
            list: false,
            text: false,
            marker: "__info__".into(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct LexOptions {
    pub empty: bool,
    pub empty_result: crate::Value,
    pub relex: bool,
    pub matchers: IndexMap<String, LexMatcher>,
}

#[derive(Debug, Clone)]
pub struct RewindOptions {
    /// Maximum retained consumed-token history. `None` and `Some(0)` are
    /// unbounded, matching the serialized `null` / non-positive forms.
    pub history: Option<usize>,
}

impl Default for RewindOptions {
    fn default() -> Self {
        Self { history: Some(64) }
    }
}

impl Default for LexOptions {
    fn default() -> Self {
        LexOptions {
            empty: true,
            empty_result: crate::Value::Undefined,
            relex: false,
            matchers: IndexMap::new(),
        }
    }
}

#[derive(Debug, Clone, Default)]
pub struct ResultOptions {
    pub fail: Vec<crate::Value>,
}

#[derive(Debug, Clone)]
pub struct RuleOptions {
    pub finish: bool,
    pub maxmul: usize,
    pub include: String,
    pub exclude: String,
    pub start: String,
}

#[derive(Debug, Clone)]
pub struct MatchToken {
    pub name: String,
    pub tin: Tin,
    pub matcher: MatchTokenMatcher,
    pub eager: bool,
}

#[derive(Clone)]
pub struct MatchValue {
    pub name: String,
    pub matcher: MatchTokenMatcher,
    pub val: Option<crate::Value>,
    pub transform: Option<ValueTransform>,
}

impl fmt::Debug for MatchValue {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("MatchValue")
            .field("name", &self.name)
            .field("matcher", &self.matcher)
            .field("val", &self.val)
            .field("transform", &self.transform.as_ref().map(|_| "<function>"))
            .finish()
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct MatchTokenResult {
    /// Non-empty source prefix consumed by the match.
    pub source: String,
    /// Token value exposed to rule actions.
    pub value: crate::Value,
}

impl MatchTokenResult {
    pub fn new(source: impl Into<String>, value: crate::Value) -> Self {
        Self {
            source: source.into(),
            value,
        }
    }
}

pub type MatchTokenCallback = Arc<dyn Fn(&str) -> Option<MatchTokenResult> + Send + Sync>;

/// Effect-based custom lexer matcher used by serialized `options.lex.match`
/// entries. The returned token must consume a non-empty prefix of the
/// remaining source.
pub type LexMatcherCallback = Arc<dyn Fn(&str) -> Option<LexCheckToken> + Send + Sync>;

/// Full native custom matcher. Unlike the serialized effect callback, this
/// form can inspect and mutate the live rule/context and advance the lexer.
pub type ImperativeLexMatcher = Arc<
    dyn for<'source> Fn(&mut Lexer<'source>, &mut Rule, &mut Context) -> Option<Token>
        + Send
        + Sync,
>;

/// Setup-time matcher constructor. Rust has one resolved typed option tree,
/// so it serves the roles of both canonical `cfg` and raw `opts` arguments.
pub type LexMatcherFactory = Arc<dyn Fn(&Options) -> Option<ImperativeLexMatcher> + Send + Sync>;

#[derive(Clone)]
pub struct LexMatcher {
    pub name: String,
    pub order: f64,
    pub matcher: Option<LexMatcherCallback>,
    pub imperative: Option<ImperativeLexMatcher>,
    pub factory: Option<LexMatcherFactory>,
}

impl fmt::Debug for LexMatcher {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("LexMatcher")
            .field("name", &self.name)
            .field("order", &self.order)
            .field("matcher", &self.matcher.as_ref().map(|_| "<function>"))
            .field(
                "imperative",
                &self.imperative.as_ref().map(|_| "<function>"),
            )
            .field("factory", &self.factory.as_ref().map(|_| "<function>"))
            .finish()
    }
}

#[derive(Clone)]
pub enum MatchTokenMatcher {
    Regex(Regex),
    Callback(MatchTokenCallback),
}

impl fmt::Debug for MatchTokenMatcher {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Regex(regex) => formatter.debug_tuple("Regex").field(regex).finish(),
            Self::Callback(_) => formatter.write_str("Callback(<function>)"),
        }
    }
}

impl Default for RuleOptions {
    fn default() -> Self {
        RuleOptions {
            finish: true,
            maxmul: 3,
            include: String::new(),
            exclude: String::new(),
            start: "val".to_string(),
        }
    }
}

pub type BudgetCheck = Arc<dyn Fn(&Context) -> bool + Send + Sync>;

#[derive(Clone, Default)]
pub struct BudgetOptions {
    pub check_every_n: usize,
    pub on_check: Option<BudgetCheck>,
}

impl fmt::Debug for BudgetOptions {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("BudgetOptions")
            .field("check_every_n", &self.check_every_n)
            .field("on_check", &self.on_check.as_ref().map(|_| "<callback>"))
            .finish()
    }
}

#[derive(Debug, Clone)]
pub struct RecoverOptions {
    pub enabled: bool,
    pub sync_groups: Vec<String>,
    pub sync_tokens: Vec<String>,
    pub pop_until_valid: bool,
    pub max_skip: usize,
    pub max_recoveries: usize,
    pub suppress: usize,
}

impl Default for RecoverOptions {
    fn default() -> Self {
        Self {
            enabled: false,
            sync_groups: vec!["close".into(), "comma".into(), "end".into()],
            sync_tokens: Vec::new(),
            pop_until_valid: true,
            max_skip: 64,
            max_recoveries: 32,
            suppress: 4,
        }
    }
}

pub type ContextParsePrepare = Arc<dyn Fn(&mut Context) + Send + Sync>;
pub type ParsePrepareWithInstance =
    Arc<dyn Fn(&crate::Tabnas, &mut Context, &crate::Value) + Send + Sync>;

/// Pre-parse hook. The context-only form preserves the original Rust API;
/// `WithInstance` exposes the owning parser and caller metadata carried by
/// the canonical callback contract.
#[derive(Clone)]
pub enum ParsePrepare {
    Context(ContextParsePrepare),
    WithInstance(ParsePrepareWithInstance),
}

impl ParsePrepare {
    pub(crate) fn same_callback(&self, other: &Self) -> bool {
        match (self, other) {
            (Self::Context(left), Self::Context(right)) => Arc::ptr_eq(left, right),
            (Self::WithInstance(left), Self::WithInstance(right)) => Arc::ptr_eq(left, right),
            _ => false,
        }
    }

    pub(crate) fn run(
        &self,
        owner: Option<&crate::Tabnas>,
        context: &mut Context,
        meta: &crate::Value,
    ) -> Result<(), &'static str> {
        match self {
            Self::Context(callback) => {
                callback(context);
                Ok(())
            }
            Self::WithInstance(callback) => {
                let owner = owner.ok_or(
                    "parse.prepare requires an owning Tabnas instance; call Tabnas::parse",
                )?;
                callback(owner, context, meta);
                Ok(())
            }
        }
    }
}

impl fmt::Debug for ParsePrepare {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Context(_) => formatter.write_str("ParsePrepare::Context(<function>)"),
            Self::WithInstance(_) => formatter.write_str("ParsePrepare::WithInstance(<function>)"),
        }
    }
}

pub type ParserStart =
    Arc<dyn Fn(&str) -> Result<crate::Value, Box<crate::TabnasError>> + Send + Sync>;
pub type ParserStartWithInstance = Arc<
    dyn Fn(&str, &crate::Tabnas, &crate::Value) -> Result<crate::Value, Box<crate::TabnasError>>
        + Send
        + Sync,
>;

#[derive(Clone, Default)]
pub struct ParserOptions {
    pub start: Option<ParserStart>,
    pub start_with_instance: Option<ParserStartWithInstance>,
}

impl fmt::Debug for ParserOptions {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ParserOptions")
            .field("start", &self.start.as_ref().map(|_| "<callback>"))
            .field(
                "start_with_instance",
                &self.start_with_instance.as_ref().map(|_| "<callback>"),
            )
            .finish()
    }
}

#[derive(Clone, Default)]
pub struct ParseOptions {
    pub prepare: Vec<ParsePrepare>,
    pub named_prepare: IndexMap<String, ParsePrepare>,
    pub budget: BudgetOptions,
    pub recover: RecoverOptions,
}

impl fmt::Debug for ParseOptions {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ParseOptions")
            .field("prepare", &self.prepare.len())
            .field("named_prepare", &self.named_prepare.len())
            .field("budget", &self.budget)
            .field("recover", &self.recover)
            .finish()
    }
}

#[derive(Debug, Clone)]
pub struct Options {
    pub safe: SafeOptions,
    pub fixed: FixedOptions,
    pub space: SpaceOptions,
    pub text: TextOptions,
    pub number: NumberOptions,
    pub string: StringOptions,
    pub line: LineOptions,
    pub comment: CommentOptions,
    pub value: ValueOptions,
    pub ender: Vec<String>,
    pub map: MapOptions,
    pub list: ListOptions,
    pub info: InfoOptions,
    pub lex: LexOptions,
    pub rewind: RewindOptions,
    pub rule: RuleOptions,
    pub result: ResultOptions,
    pub parse: ParseOptions,
    pub parser: ParserOptions,
    /// Per-plugin accumulated option bags (`options.plugin`).
    pub plugin: IndexMap<String, crate::Value>,
    pub token_set: HashMap<String, Vec<Tin>>,
    /// Named token identities without an attached built-in, fixed, or match
    /// producer. Serialized rule slots allocate these on first reference.
    pub tokens: IndexMap<String, Tin>,
    /// Enable serialized/custom regexp token matching (`options.match.lex`).
    pub match_lex: bool,
    pub match_check: Option<LexCheck>,
    pub match_tokens: IndexMap<String, MatchToken>,
    pub match_values: IndexMap<String, MatchValue>,
    pub error: HashMap<String, String>,
    pub hint: HashMap<String, String>,
    pub errmsg: ErrMsgOptions,
    pub color: ColorOptions,
    pub config_modify: IndexMap<String, ConfigModifier>,
    pub tag: String,
}

impl Default for Options {
    fn default() -> Self {
        let mut token_set = HashMap::new();
        token_set.insert("IGNORE".to_string(), vec![TIN_SP, TIN_LN, TIN_CM]);
        token_set.insert("VAL".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);
        token_set.insert("KEY".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);

        Options {
            safe: SafeOptions::default(),
            fixed: FixedOptions::default(),
            space: SpaceOptions::default(),
            text: TextOptions::default(),
            number: NumberOptions::default(),
            string: StringOptions::default(),
            line: LineOptions::default(),
            comment: CommentOptions::default(),
            value: ValueOptions::default(),
            ender: Vec::new(),
            map: MapOptions::default(),
            list: ListOptions::default(),
            info: InfoOptions::default(),
            lex: LexOptions::default(),
            rewind: RewindOptions::default(),
            rule: RuleOptions::default(),
            result: ResultOptions::default(),
            parse: ParseOptions::default(),
            parser: ParserOptions::default(),
            plugin: IndexMap::new(),
            token_set,
            tokens: IndexMap::new(),
            match_lex: true,
            match_check: None,
            match_tokens: IndexMap::new(),
            match_values: IndexMap::new(),
            error: crate::error::default_error_messages(),
            hint: crate::error::default_error_hints(),
            errmsg: ErrMsgOptions::default(),
            color: ColorOptions::default(),
            config_modify: IndexMap::new(),
            tag: "-".to_string(),
        }
    }
}

impl Options {
    /// Rebuild the resolved configuration callbacks from this option tree.
    /// Config modifiers run before matcher factories, matching canonical
    /// `configure`: factories must observe the modifier's final values.
    pub fn refresh_configuration(&mut self) -> Result<(), String> {
        let raw_options = self.clone();
        let modifiers: Vec<_> = self
            .config_modify
            .iter()
            .map(|(name, modifier)| (name.clone(), modifier.clone()))
            .collect();
        for (name, modifier) in modifiers {
            std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                modifier.run(self, &raw_options)
            }))
            .map_err(|_| format!("config modifier {name} panicked"))?;
        }
        self.refresh_lex_matchers()
    }

    /// Rebuild setup-time lexer matchers from the fully resolved option tree.
    /// Factories run once per configuration/derivation, not once per parse.
    pub fn refresh_lex_matchers(&mut self) -> Result<(), String> {
        let factories: Vec<_> = self
            .lex
            .matchers
            .iter()
            .filter_map(|(name, matcher)| {
                matcher
                    .factory
                    .as_ref()
                    .map(|factory| (name.clone(), factory.clone()))
            })
            .collect();
        for (name, factory) in factories {
            let matcher = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| factory(self)))
                .map_err(|_| format!("lexer matcher factory {name} panicked"))?;
            if let Some(entry) = self.lex.matchers.get_mut(&name) {
                entry.imperative = matcher;
            }
        }
        Ok(())
    }

    /// Baseline for `Tabnas::empty`: no standard token producers or token
    /// sets. Structural and diagnostic fields retain typed values so plugins
    /// can opt individual facilities back in without handling missing data.
    pub fn empty() -> Self {
        let mut options = Self::default();
        options.fixed.lex = false;
        options.fixed.tokens.clear();
        options.space.lex = false;
        options.text.lex = false;
        options.number.lex = false;
        options.string.lex = false;
        options.line.lex = false;
        options.comment.lex = false;
        options.comment.definitions.clear();
        options.value.lex = false;
        options.value.definitions.clear();
        options.match_lex = false;
        options.match_tokens.clear();
        options.match_values.clear();
        options.lex.matchers.clear();
        options.token_set.clear();
        options.tokens.clear();
        options
    }

    pub fn is_ignored(&self, tin: Tin) -> bool {
        self.token_set
            .get("IGNORE")
            .is_some_and(|ignored| ignored.contains(&tin))
    }

    pub fn token(&self, name: &str) -> Option<Tin> {
        crate::token::name_to_tin(name).or_else(|| {
            let name = if name.starts_with('#') {
                name.to_string()
            } else {
                format!("#{name}")
            };
            self.tokens
                .get(&name)
                .copied()
                .or_else(|| self.match_tokens.get(&name).map(|matcher| matcher.tin))
                .or_else(|| self.fixed.tokens.get(&name).map(|token| token.tin))
        })
    }

    pub fn register_token(&mut self, name: impl Into<String>) -> Tin {
        let name = name.into();
        let name = if name.starts_with('#') {
            name
        } else {
            format!("#{name}")
        };
        if let Some(tin) = self.token(&name) {
            return tin;
        }
        let tin = self.next_tin();
        self.tokens.insert(name, tin);
        tin
    }

    pub fn next_tin(&self) -> Tin {
        self.match_tokens
            .values()
            .map(|matcher| matcher.tin)
            .chain(self.fixed.tokens.values().map(|token| token.tin))
            .chain(self.tokens.values().copied())
            .max()
            .unwrap_or(TIN_MAX - 1)
            + 1
    }

    pub fn token_name(&self, tin: Tin) -> String {
        self.match_tokens
            .values()
            .find(|matcher| matcher.tin == tin)
            .map(|matcher| matcher.name.clone())
            .or_else(|| {
                self.fixed
                    .tokens
                    .values()
                    .find(|token| token.tin == tin)
                    .map(|token| token.name.clone())
            })
            .or_else(|| {
                self.tokens
                    .iter()
                    .find(|(_, token_tin)| **token_tin == tin)
                    .map(|(name, _)| name.clone())
            })
            .unwrap_or_else(|| crate::token::tin_name(tin).to_string())
    }
}
