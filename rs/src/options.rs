// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, TIN_MAX, TIN_NR, TIN_ST, TIN_TX, TIN_VL};
use indexmap::IndexMap;
use regex::Regex;
use std::collections::HashMap;

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
pub struct NumberOptions {
    pub hex: bool,
    pub oct: bool,
    pub bin: bool,
    pub sep: Option<String>,
    pub exclude: Option<String>, // regex string e.g. "^00+"
}

impl Default for NumberOptions {
    fn default() -> Self {
        NumberOptions {
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
    pub chars: String,
    pub multi_chars: String,
    pub allow_unknown: bool,
    pub allow_control: bool,
}

impl Default for StringOptions {
    fn default() -> Self {
        StringOptions {
            chars: "\"'`".to_string(),
            multi_chars: "".to_string(),
            allow_unknown: true,
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
}

impl Default for LexOptions {
    fn default() -> Self {
        LexOptions { empty: true }
    }
}

#[derive(Debug, Clone)]
pub struct RuleOptions {
    pub finish: bool,
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
            include: String::new(),
            exclude: String::new(),
            start: "val".to_string(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct Options {
    pub fixed: FixedOptions,
    pub text: TextOptions,
    pub number: NumberOptions,
    pub string: StringOptions,
    pub line: LineOptions,
    pub comment: CommentOptions,
    pub map: MapOptions,
    pub lex: LexOptions,
    pub rule: RuleOptions,
    pub token_set: HashMap<String, Vec<Tin>>,
    pub match_tokens: IndexMap<String, MatchToken>,
    pub tag: String,
}

impl Default for Options {
    fn default() -> Self {
        let mut token_set = HashMap::new();
        token_set.insert("VAL".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);
        token_set.insert("KEY".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);

        Options {
            fixed: FixedOptions::default(),
            text: TextOptions::default(),
            number: NumberOptions::default(),
            string: StringOptions::default(),
            line: LineOptions::default(),
            comment: CommentOptions::default(),
            map: MapOptions::default(),
            lex: LexOptions::default(),
            rule: RuleOptions::default(),
            token_set,
            match_tokens: IndexMap::new(),
            tag: "-".to_string(),
        }
    }
}

impl Options {
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
