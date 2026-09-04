// Copyright (c) 2013-2026 Richard Rodger, MIT License

use crate::token::{Tin, TIN_NR, TIN_ST, TIN_TX, TIN_VL};
use std::collections::HashMap;

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
}

impl Default for RuleOptions {
    fn default() -> Self {
        RuleOptions {
            finish: true,
            include: String::new(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct Options {
    pub text: TextOptions,
    pub number: NumberOptions,
    pub string: StringOptions,
    pub line: LineOptions,
    pub comment: CommentOptions,
    pub map: MapOptions,
    pub lex: LexOptions,
    pub rule: RuleOptions,
    pub token_set: HashMap<String, Vec<Tin>>,
    pub tag: String,
}

impl Default for Options {
    fn default() -> Self {
        let mut token_set = HashMap::new();
        token_set.insert("VAL".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);
        token_set.insert("KEY".to_string(), vec![TIN_TX, TIN_NR, TIN_ST, TIN_VL]);

        Options {
            text: TextOptions::default(),
            number: NumberOptions::default(),
            string: StringOptions::default(),
            line: LineOptions::default(),
            comment: CommentOptions::default(),
            map: MapOptions::default(),
            lex: LexOptions::default(),
            rule: RuleOptions::default(),
            token_set,
            tag: "-".to_string(),
        }
    }
}
