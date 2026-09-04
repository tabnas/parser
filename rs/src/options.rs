// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, TIN_CM, TIN_LN, TIN_MAX, TIN_NR, TIN_SP, TIN_ST, TIN_TX, TIN_VL};
use indexmap::IndexMap;
use regex::Regex;
use std::collections::HashMap;
use std::fmt;
use std::sync::Arc;

use crate::context::Context;

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
}

impl LexCheckResult {
    pub fn token(token: LexCheckToken) -> Self {
        Self::Token(Box::new(token))
    }
}

#[derive(Clone)]
pub struct LexCheck {
    callback: Arc<dyn Fn(&str) -> LexCheckResult + Send + Sync>,
}

impl LexCheck {
    pub(crate) fn new(callback: impl Fn(&str) -> LexCheckResult + Send + Sync + 'static) -> Self {
        Self {
            callback: Arc::new(callback),
        }
    }

    pub(crate) fn run(&self, source: &str) -> LexCheckResult {
        (self.callback)(source)
    }
}

impl fmt::Debug for LexCheck {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("LexCheck(<function>)")
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

pub type TextModifier = Arc<dyn Fn(crate::Value) -> crate::Value + Send + Sync>;

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
    callback: Arc<CommentSuffixCallback>,
}

type CommentSuffixCallback = dyn Fn(&str) -> Option<String> + Send + Sync;

impl CommentSuffixMatcher {
    pub(crate) fn new(callback: impl Fn(&str) -> Option<String> + Send + Sync + 'static) -> Self {
        Self {
            callback: Arc::new(callback),
        }
    }

    pub(crate) fn run(&self, source: &str) -> Option<String> {
        (self.callback)(source)
    }
}

impl fmt::Debug for CommentSuffixMatcher {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("CommentSuffixMatcher(<function>)")
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
pub struct MapOptions {
    pub extend: bool,
}

impl Default for MapOptions {
    fn default() -> Self {
        MapOptions { extend: true }
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

pub type ParsePrepare = Arc<dyn Fn(&mut Context) + Send + Sync>;

#[derive(Clone, Default)]
pub struct ParseOptions {
    pub prepare: Vec<ParsePrepare>,
    pub budget: BudgetOptions,
    pub recover: RecoverOptions,
}

impl fmt::Debug for ParseOptions {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ParseOptions")
            .field("prepare", &self.prepare.len())
            .field("budget", &self.budget)
            .field("recover", &self.recover)
            .finish()
    }
}

#[derive(Debug, Clone)]
pub struct Options {
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
    pub info: InfoOptions,
    pub lex: LexOptions,
    pub rewind: RewindOptions,
    pub rule: RuleOptions,
    pub result: ResultOptions,
    pub parse: ParseOptions,
    pub token_set: HashMap<String, Vec<Tin>>,
    /// Enable serialized/custom regexp token matching (`options.match.lex`).
    pub match_lex: bool,
    pub match_check: Option<LexCheck>,
    pub match_tokens: IndexMap<String, MatchToken>,
    pub match_values: IndexMap<String, MatchValue>,
    pub tag: String,
}

impl Default for Options {
    fn default() -> Self {
        let mut token_set = HashMap::new();
        token_set.insert("IGNORE".to_string(), vec![TIN_SP, TIN_LN, TIN_CM]);
        token_set.insert("VAL".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);
        token_set.insert("KEY".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);

        Options {
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
            info: InfoOptions::default(),
            lex: LexOptions::default(),
            rewind: RewindOptions::default(),
            rule: RuleOptions::default(),
            result: ResultOptions::default(),
            parse: ParseOptions::default(),
            token_set,
            match_lex: true,
            match_check: None,
            match_tokens: IndexMap::new(),
            match_values: IndexMap::new(),
            tag: "-".to_string(),
        }
    }
}

impl Options {
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
            self.match_tokens
                .get(&name)
                .map(|matcher| matcher.tin)
                .or_else(|| self.fixed.tokens.get(&name).map(|token| token.tin))
        })
    }

    pub fn next_tin(&self) -> Tin {
        self.match_tokens
            .values()
            .map(|matcher| matcher.tin)
            .chain(self.fixed.tokens.values().map(|token| token.tin))
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
            .unwrap_or_else(|| crate::token::tin_name(tin).to_string())
    }
}
