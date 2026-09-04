// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, TIN_CM, TIN_LN, TIN_MAX, TIN_NR, TIN_SP, TIN_ST, TIN_TX, TIN_VL};
use indexmap::IndexMap;
use regex::Regex;
use std::collections::HashMap;
use std::fmt;
use std::sync::Arc;

use crate::context::Context;

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
        Self { lex: true, tokens }
    }
}

#[derive(Debug, Clone)]
pub struct TextOptions {
    pub lex: bool,
}

impl Default for TextOptions {
    fn default() -> Self {
        TextOptions { lex: true }
    }
}

#[derive(Debug, Clone)]
pub struct SpaceOptions {
    pub lex: bool,
    pub chars: String,
}

impl Default for SpaceOptions {
    fn default() -> Self {
        Self {
            lex: true,
            chars: " \t".into(),
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
        }
    }
}

#[derive(Debug, Clone)]
pub struct StringOptions {
    pub lex: bool,
    pub chars: String,
    pub multi_chars: String,
    pub allow_unknown: bool,
    pub escape_strict: bool,
    pub allow_control: bool,
}

impl Default for StringOptions {
    fn default() -> Self {
        StringOptions {
            lex: true,
            chars: "\"'`".to_string(),
            multi_chars: "".to_string(),
            allow_unknown: true,
            escape_strict: false,
            allow_control: false,
        }
    }
}

#[derive(Debug, Clone)]
pub struct LineOptions {
    pub lex: bool,
    pub fixed: Vec<char>,
}

impl Default for LineOptions {
    fn default() -> Self {
        Self {
            lex: true,
            fixed: Vec::new(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct CommentOptions {
    pub lex: bool,
}

#[derive(Debug, Clone)]
pub struct ValueOptions {
    pub lex: bool,
}

impl Default for ValueOptions {
    fn default() -> Self {
        Self { lex: true }
    }
}

impl Default for CommentOptions {
    fn default() -> Self {
        CommentOptions { lex: true }
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

#[derive(Debug, Clone)]
pub struct LexOptions {
    pub empty: bool,
    pub empty_result: crate::Value,
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
    pub regex: Regex,
    pub eager: bool,
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
    pub map: MapOptions,
    pub lex: LexOptions,
    pub rewind: RewindOptions,
    pub rule: RuleOptions,
    pub result: ResultOptions,
    pub parse: ParseOptions,
    pub token_set: HashMap<String, Vec<Tin>>,
    pub match_tokens: IndexMap<String, MatchToken>,
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
            map: MapOptions::default(),
            lex: LexOptions::default(),
            rewind: RewindOptions::default(),
            rule: RuleOptions::default(),
            result: ResultOptions::default(),
            parse: ParseOptions::default(),
            token_set,
            match_tokens: IndexMap::new(),
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
